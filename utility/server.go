package utility

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	discovery "github.com/egidinas/loom-gossamer-shared/go/discovery"
	"github.com/egidinas/loom-gossamer-shared/go/graphsem"
	graphwall "github.com/egidinas/loom-gossamer-shared/go/graphwall"
	sharedui "github.com/egidinas/loom-gossamer-shared/go/graphwallui"
	"github.com/egidinas/loom-gossamer-shared/go/tmtc"
	"github.com/egidinas/loom-gossamer-shared/go/tmtclog"
	"github.com/egidinas/loom-gossamer-shared/go/transport"
	"github.com/egidinas/meerstetter-go/canring"
	archiveexport "github.com/egidinas/meerstetter-go/export"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomdict"
	"github.com/egidinas/meerstetter-go/mecomserver"
	"github.com/egidinas/meerstetter-go/objectdict"
)

type graphWallAssignment = graphwall.Assignment[discovery.Target]

type mecomScalarReader interface {
	ReadFloat32(context.Context, int, int) (float64, error)
	ReadInt32(context.Context, int, int) (int32, error)
}

type mecomReader interface {
	mecom.ReadClient
	mecomScalarReader
}

const readoutUnsupportedOnActiveTransport = "unsupported_on_active_transport"

const arrowIPCContentType = "application/vnd.apache.arrow.stream"

var telemetryArchiveArrowSchema = arrow.NewSchema([]arrow.Field{
	{Name: "seq", Type: arrow.PrimitiveTypes.Uint64},
	{Name: "time", Type: arrow.PrimitiveTypes.Int64},
	{Name: "target_id", Type: arrow.BinaryTypes.String},
	{Name: "device_id", Type: arrow.BinaryTypes.String},
	{Name: "device_alias", Type: arrow.BinaryTypes.String},
	{Name: "instance", Type: arrow.BinaryTypes.String},
	{Name: "parameter", Type: arrow.BinaryTypes.String},
	{Name: "type", Type: arrow.BinaryTypes.String},
	{Name: "subtype", Type: arrow.BinaryTypes.String},
	{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: true},
	{Name: "unit", Type: arrow.BinaryTypes.String},
	{Name: "quality", Type: arrow.BinaryTypes.String},
	{Name: "source_path", Type: arrow.BinaryTypes.String},
}, nil)

type Server struct {
	cfg       Config
	hub       mecomserver.HubConfig
	recorder  *tmtclog.Recorder
	targets   []discovery.Target
	graphWall []graphWallAssignment

	mu               sync.Mutex
	started          bool
	stopFns          []func()
	baselineReadouts map[string]*mecom.Readout
}

func NewServer(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	hub, err := cfg.HubConfig()
	if err != nil {
		return nil, err
	}
	targets := targetsFromHub(context.Background(), hub, cfg)
	graphConfig := cfg.GraphWall
	if len(graphConfig) == 0 {
		graphConfig = defaultGraphWall(targets)
	}
	graphWall, err := resolveGraphWall(graphConfig, targets)
	if err != nil {
		return nil, err
	}
	srv := &Server{
		cfg:              cfg,
		hub:              hub,
		recorder:         tmtclog.NewRecorder(tmtclog.New(maxRingRetention(hub)), nil),
		targets:          targets,
		graphWall:        graphWall,
		baselineReadouts: map[string]*mecom.Readout{},
	}
	srv.seedTECCatalogueTelemetry()
	return srv, nil
}

func (s *Server) Config() Config {
	return s.cfg
}

func (s *Server) HubConfig() mecomserver.HubConfig {
	return s.hub
}

func (s *Server) Recorder() *tmtclog.Recorder {
	return s.recorder
}

func (s *Server) Targets() []discovery.Target {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]discovery.Target(nil), s.targets...)
}

func (s *Server) GraphWall() []graphWallAssignment {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]graphWallAssignment(nil), s.graphWall...)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/assets/shared/graphwall/renderer.css", s.handleSharedGraphWallCSS)
	mux.HandleFunc("/assets/shared/graphwall/renderer.js", s.handleSharedGraphWallJS)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/operator/meerstettergo/health", s.handleHealth)
	mux.HandleFunc("/api/devices", s.handleDevices)
	mux.HandleFunc("/api/tec/catalogue", s.handleTECCatalogue)
	mux.HandleFunc("/api/loom/source-catalogue", s.handleLoomSourceCatalogue)
	mux.HandleFunc("/api/operator/meerstettergo/source-catalogue", s.handleLoomSourceCatalogue)
	mux.HandleFunc("/api/discovery/tree", s.handleDiscoveryTree)
	mux.HandleFunc("/api/loom/discovery-tree", s.handleDiscoveryTree)
	mux.HandleFunc("/api/loom/discovery/tree", s.handleDiscoveryTree)
	mux.HandleFunc("/api/operator/meerstettergo/discovery/tree", s.handleDiscoveryTree)
	mux.HandleFunc("/api/graph-wall", s.handleGraphWall)
	mux.HandleFunc("/api/operator/meerstettergo/graph-wall", s.handleGraphWall)
	mux.HandleFunc("/api/graph-wall/assign", s.handleGraphWallAssign)
	mux.HandleFunc("/api/tiles", s.handleTiles)
	mux.HandleFunc("/api/operator/meerstettergo/tiles", s.handleTiles)
	mux.HandleFunc("/api/log/ring", s.handleRing)
	mux.HandleFunc("/api/operator/meerstettergo/log/ring", s.handleRing)
	mux.HandleFunc("/api/log/export", s.handleLogExport)
	mux.HandleFunc("/api/log/archive/manifest", s.handleLogArchiveManifest)
	mux.HandleFunc("/api/operator/meerstettergo/log/archive/manifest", s.handleLogArchiveManifest)
	mux.HandleFunc("/api/log/import/review", s.handleLogImportReview)
	mux.HandleFunc("/api/log/review", s.handleLogReview)
	mux.HandleFunc("/api/can/ring", s.handleCANRing)
	mux.HandleFunc("/api/operator/meerstettergo/can/ring", s.handleCANRing)
	mux.HandleFunc("/api/polling/status", s.handlePollingStatus)
	mux.HandleFunc("/api/operator/meerstettergo/polling/status", s.handlePollingStatus)
	mux.HandleFunc("/api/events/swimlane", s.handleEvents)
	mux.HandleFunc("/api/target/read", s.handleTargetRead)
	mux.HandleFunc("/api/target/write", s.handleTargetWrite)
	mux.HandleFunc("/api/operator/meerstettergo/target/read", s.handleTargetRead)
	mux.HandleFunc("/api/operator/meerstettergo/target/write", s.handleTargetWrite)
	return s.redirectCloudflareHTTP(mux)
}

func (s *Server) redirectCloudflareHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "http") && r.Host != "" {
			u := *r.URL
			u.Scheme = "https"
			u.Host = r.Host
			http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if err := s.StartPassthrough(ctx); err != nil {
		return err
	}
	s.StartBaselinePolling(ctx)
	srv := &http.Server{Addr: s.cfg.HTTPListen, Handler: s.Handler()}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		s.StopPassthrough()
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) StartPassthrough(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	for _, device := range s.hub.Devices {
		if strings.TrimSpace(device.PassthroughListen) == "" {
			continue
		}
		ln, err := net.Listen("tcp", device.PassthroughListen)
		if err != nil {
			s.stopLocked()
			return fmt.Errorf("utility: listen passthrough %s for %s: %w", device.PassthroughListen, device.ID, err)
		}
		s.stopFns = append(s.stopFns, func() { _ = ln.Close() })
		cfg := device.ServerConfig()
		go func() {
			_ = mecomserver.Serve(ctx, ln, cfg)
		}()
	}
	s.started = true
	return nil
}

func (s *Server) StopPassthrough() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Server) StartBaselinePolling(ctx context.Context) {
	for _, device := range s.hub.Devices {
		device := device
		go s.pollBaseline(ctx, device)
	}
}

func (s *Server) pollBaseline(ctx context.Context, device mecomserver.DeviceConfig) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		s.pollBaselineOnce(ctx, device)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) pollBaselineOnce(ctx context.Context, device mecomserver.DeviceConfig) {
	reqCtx, cancel := context.WithTimeout(ctx, device.Queue.RequestTimeout)
	defer cancel()
	client, closeFn, activeDevice, err := activeBrokerClientForDevice(reqCtx, device)
	if err != nil {
		s.publishPollError(device, err.Error())
		return
	}
	defer closeFn()
	batch := s.pollBaselineClient(reqCtx, activeDevice, client, time.Now().UTC())
	for _, pollErr := range batch.Errors {
		s.publishPollError(activeDevice, pollErr.Error())
	}
}

const utilityDefaultTECChannels = 4

func utilityBaselineReadoutChannels(cfg Config) int {
	channels := utilityDefaultTECChannels
	for _, instance := range cfg.Instances {
		if instance > channels {
			channels = instance
		}
	}
	return channels
}

func utilityBaselineReadoutParameters(cfg Config) []mecom.ReadoutParameter {
	return mecom.DefaultTECReadoutParameters(utilityBaselineReadoutChannels(cfg))
}

func utilityReadoutForDevice(cfg Config, device mecomserver.DeviceConfig) *mecom.Readout {
	params := utilityBaselineReadoutParameters(cfg)
	disableRingReadout := isDirectCANTarget(device.Target)
	expanded := make([]mecom.ReadoutParameter, 0, len(params)*2)
	for _, param := range params {
		if disableRingReadout && param.HighPriority {
			param.HighPriority = false
		}
		expanded = append(expanded, param)
	}
	if !disableRingReadout {
		for _, param := range params {
			if !param.HighPriority {
				continue
			}
			param.HighPriority = false
			expanded = append(expanded, param)
		}
	}
	return mecom.NewReadout(mecom.ReadoutConfig{
		Parameters:     expanded,
		BulkChunk:      len(expanded),
		RequestTimeout: device.Queue.RequestTimeout,
		Derived: &mecom.DerivedReadoutConfig{
			ControllerAddress: utilityDeviceControllerAddress(device),
		},
	})
}

func utilityDeviceControllerAddress(device mecomserver.DeviceConfig) int {
	id := strings.TrimSpace(device.ID)
	for _, part := range strings.FieldsFunc(id, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		address, err := strconv.Atoi(part)
		if err == nil && address > 0 {
			return address
		}
	}
	return 0
}

func utilityProtocolForTarget(target string) string {
	if isCANopenTarget(target) {
		return "canopen"
	}
	return "mecom"
}

func isCANopenTarget(target string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.ToLower(target)), "canopen:")
}

func isDirectCANTarget(target string) bool {
	target = strings.TrimSpace(strings.ToLower(target))
	return strings.HasPrefix(target, "socketcan:") || strings.HasPrefix(target, "canopen:")
}

func applyTransportReadoutMetadata(metadata map[string]string, device mecomserver.DeviceConfig, preferredReadout string) {
	preferredReadout = strings.TrimSpace(preferredReadout)
	if preferredReadout == "" {
		preferredReadout = mecom.ReadoutVXRoundRobinQueue
	}
	if strings.TrimSpace(metadata["preferred_readout"]) == "" {
		metadata["preferred_readout"] = preferredReadout
	}
	metadata["active_readout"] = preferredReadout
	metadata["background_readout"] = mecom.ReadoutVXRoundRobinQueue
	metadata["ring_readout"] = mecom.ReadoutCRTVStreamRingBuffer
	metadata["controller_ring_status"] = "supported"
	metadata["controller_ring_gap_fill"] = "enabled_when_supported"
	metadata["ring_readout_transport"] = "serial,tcp"
	if !isDirectCANTarget(device.Target) {
		return
	}
	metadata["active_readout"] = mecom.ReadoutVXRoundRobinQueue
	metadata["ring_readout"] = readoutUnsupportedOnActiveTransport
	metadata["controller_ring_status"] = readoutUnsupportedOnActiveTransport
	metadata["controller_ring_gap_fill"] = "pi_ram_flash_ring_only_until_can_mapping_is_characterized"
}

func (s *Server) baselineReadout(device mecomserver.DeviceConfig) *mecom.Readout {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.baselineReadouts == nil {
		s.baselineReadouts = map[string]*mecom.Readout{}
	}
	if readout := s.baselineReadouts[device.ID]; readout != nil {
		return readout
	}
	readout := utilityReadoutForDevice(s.cfg, device)
	s.baselineReadouts[device.ID] = readout
	return readout
}

func (s *Server) pollBaselineClient(ctx context.Context, device mecomserver.DeviceConfig, client mecom.ReadClient, observedAt time.Time) mecom.ReadoutBatch {
	batch := s.baselineReadout(device).Poll(ctx, client, observedAt)
	for _, value := range batch.Values {
		s.publishReadoutValue(device, value)
	}
	return batch
}

func (s *Server) publishReadoutValue(device mecomserver.DeviceConfig, value mecom.ReadoutValue) {
	tm := s.telemetryForReadoutValue(device, value, "ok")
	if pubErr := s.recorder.PublishTelemetry(tm); pubErr != nil {
		log.Printf("utility: publish telemetry %s: %v", tm.TargetID, pubErr)
	}
}

func (s *Server) publishInitializedReadoutValue(device mecomserver.DeviceConfig, value mecom.ReadoutValue) {
	tm := s.telemetryForReadoutValue(device, value, "not_sampled")
	tm.Value = nil
	tm.Metadata["value_state"] = "not_sampled"
	if pubErr := s.recorder.PublishTelemetry(tm); pubErr != nil {
		log.Printf("utility: publish initialized telemetry %s: %v", tm.TargetID, pubErr)
	}
}

func (s *Server) telemetryForReadoutValue(device mecomserver.DeviceConfig, value mecom.ReadoutValue, quality string) tmtc.Telemetry {
	if value.ObservedAt.IsZero() {
		value.ObservedAt = time.Now().UTC()
	}
	targetID := utilitySignalTargetID(device, value.Sensor)
	writable := utilityReadoutParameterWritable(value.Parameter, value.Sensor)
	if strings.TrimSpace(quality) == "" {
		quality = "ok"
	}
	jsonValue := any(value.Value)
	category, subtype, parameterName := utilitySignalLabels(value.Sensor, value.Parameter.Unit)
	if strings.TrimSpace(value.Parameter.Name) != "" {
		parameterName = value.Parameter.Name
	}
	metadata := map[string]string{
		"transport":        device.Target,
		"protocol":         utilityProtocolForTarget(device.Target),
		"readout":          value.Readout,
		"catalogue_active": "true",
		"parameter_id":     strconv.Itoa(value.Parameter.ID),
		"parameter_name":   parameterName,
		"category":         category,
		"subtype":          subtype,
		"instance":         strconv.Itoa(value.Parameter.Instance),
		"format":           string(value.Parameter.Type),
		"value_type":       string(value.Parameter.Type),
		"sensor":           value.Sensor,
		"unit":             value.Parameter.Unit,
		"readable":         "true",
		"writable":         strconv.FormatBool(writable),
		"access":           readoutAccess(writable),
		"read_path":        "/api/target/read?id=" + url.QueryEscape(targetID),
		"graph_group":      graphGroupForSignal(value.Sensor, value.Parameter.Unit),
		"axis_policy":      graphwall.AxisPolicyForAggregate(graphGroupForSignal(value.Sensor, value.Parameter.Unit), value.Parameter.Unit),
	}
	applyTransportReadoutMetadata(metadata, device, value.Readout)
	addDeviceRedundancyMetadata(metadata, device)
	if writable {
		metadata["write_path"] = "/api/target/write"
	}
	if value.Reduction != nil {
		metadata["sample_count"] = strconv.Itoa(value.Reduction.Count)
		metadata["sample_stddev"] = fmt.Sprintf("%g", value.Reduction.StdDev)
		metadata["sample_min"] = fmt.Sprintf("%g", value.Reduction.Min)
		metadata["sample_max"] = fmt.Sprintf("%g", value.Reduction.Max)
		metadata["ring_reduction"] = "mean_stddev_window_to_consumer_rate"
	}
	if math.IsNaN(value.Value) || math.IsInf(value.Value, 0) {
		jsonValue = nil
		metadata["raw_float"] = fmt.Sprintf("%g", value.Value)
		metadata["value_state"] = "not_applicable"
	}
	return tmtc.Telemetry{
		ID:       strings.TrimPrefix(targetID, "device:"),
		TargetID: targetID,
		Time:     value.ObservedAt,
		Name:     parameterName,
		Value:    jsonValue,
		Unit:     value.Parameter.Unit,
		Quality:  quality,
		Metadata: metadata,
	}
}

func utilitySignalTargetID(device mecomserver.DeviceConfig, sensor string) string {
	return "device:" + device.ID + ":" + sensor
}

func utilitySignalSuffix(sensor string) string {
	parts := strings.Split(sensor, ".")
	if len(parts) == 0 {
		return sensor
	}
	return parts[len(parts)-1]
}

func utilitySensorInstance(sensor string) int {
	for _, part := range strings.Split(sensor, ".") {
		if !strings.HasPrefix(part, "tec_") {
			continue
		}
		instance, err := strconv.Atoi(strings.TrimPrefix(part, "tec_"))
		if err == nil {
			return instance
		}
	}
	return 0
}

func utilitySignalUnit(sensor string) string {
	switch utilitySignalSuffix(sensor) {
	case "object_temp_c", "sink_temp_c", "cascade_temp_c", "target_object_temp_c", "ramp_object_temp_c":
		return "degC"
	case "output_current_a":
		return "A"
	case "output_voltage_v":
		return "V"
	case "output_power_w", "electrical_input_w", "heat_pumped_from_item_w", "resistive_heat_w", "hot_side_dissipated_w":
		return "W"
	default:
		return ""
	}
}

func utilitySignalDisplayName(sensor string) string {
	_, _, parameter := utilitySignalLabels(sensor, utilitySignalUnit(sensor))
	return parameter
}

func utilitySignalLabels(sensor, unit string) (category, subtype, parameter string) {
	switch utilitySignalSuffix(sensor) {
	case "object_temp_c":
		return "Thermal", "Temperature", "Object Temperature"
	case "sink_temp_c":
		return "Thermal", "Temperature", "Sink Temperature"
	case "cascade_temp_c":
		return "Thermal", "Temperature", "Cascade Temperature"
	case "target_object_temp_c":
		return "Thermal", "Setpoint", "Target Object Temperature"
	case "ramp_object_temp_c":
		return "Thermal", "Setpoint", "Ramp Object Temperature"
	case "temperature_stable":
		return "Thermal", "State", "Temperature Stable"
	case "output_current_a":
		return "Electrical", "Drive", "Output Current"
	case "output_voltage_v":
		return "Electrical", "Drive", "Output Voltage"
	case "output_power_w":
		return "Power", "Output", "Output Power"
	case "electrical_input_w":
		return "Power", "Peltier Model", "Electrical Input Power"
	case "heat_pumped_from_item_w":
		return "Power", "Peltier Model", "Heat Pumped From Item"
	case "resistive_heat_w":
		return "Power", "Peltier Model", "Resistive Heat"
	case "hot_side_dissipated_w":
		return "Power", "Peltier Model", "Hot-Side Dissipated Heat"
	default:
		if unit == "degC" {
			category = "Thermal"
		} else if unit == "A" || unit == "V" {
			category = "Electrical"
		} else if unit == "W" {
			category = "Power"
		} else {
			category = "TEC"
		}
		return category, "Signal", titleFromSuffix(utilitySignalSuffix(sensor))
	}
}

func titleFromSuffix(suffix string) string {
	parts := strings.Split(strings.ReplaceAll(suffix, "_", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func readoutAccess(writable bool) string {
	if writable {
		return "read_write"
	}
	return "read"
}

func utilityReadoutParameterWritable(param mecom.Parameter, sensor string) bool {
	if param.Writable || param.ID == 3000 {
		return true
	}
	return strings.Contains(strings.ToLower(sensor), "target_object_temp_c")
}

func graphGroupForSignal(sensor, unit string) string {
	return graphwall.SemanticAggregate(graphwall.SemanticInput{
		ID:   sensor,
		Name: utilitySignalDisplayName(sensor),
		Unit: unit,
		Kind: string(objectdict.ValueKindContinuous),
	})
}

func (s *Server) seedTECCatalogueTelemetry() {
	observedAt := time.Now().UTC()
	for _, device := range s.hub.Devices {
		seen := map[string]struct{}{}
		for _, spec := range utilityBaselineReadoutParameters(s.cfg) {
			seen[spec.Sensor] = struct{}{}
			s.publishInitializedReadoutValue(device, mecom.ReadoutValue{
				Parameter:  spec.Parameter,
				Sensor:     spec.Sensor,
				Value:      math.NaN(),
				ObservedAt: observedAt,
				Readout:    mecom.ReadoutVXRoundRobinQueue,
			})
		}
		for _, sensor := range mecom.DefaultTECSignalNames(utilityBaselineReadoutChannels(s.cfg)) {
			if _, ok := seen[sensor]; ok {
				continue
			}
			s.publishInitializedReadoutValue(device, mecom.ReadoutValue{
				Parameter: mecom.Parameter{
					Name:     utilitySignalSuffix(sensor),
					Unit:     utilitySignalUnit(sensor),
					Type:     mecom.DataTypeFloat32,
					Instance: utilitySensorInstance(sensor),
				},
				Sensor:     sensor,
				Value:      math.NaN(),
				ObservedAt: observedAt,
				Readout:    mecom.ReadoutDerivedChannelModel,
			})
		}
	}
}

func (s *Server) publishPollError(device mecomserver.DeviceConfig, detail string) {
	metadata := map[string]string{
		"transport": device.Target,
		"error":     detail,
	}
	addDeviceRedundancyMetadata(metadata, device)
	if err := s.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       device.ID + ":status",
		TargetID: "device:" + device.ID + ":status",
		Time:     time.Now().UTC(),
		Name:     "Status",
		Value:    "offline",
		Quality:  "read_error",
		Metadata: metadata,
	}); err != nil {
		log.Printf("utility: publish status %s: %v", device.ID, err)
	}
}

func (s *Server) stopLocked() {
	for _, stop := range s.stopFns {
		stop()
	}
	s.stopFns = nil
	s.started = false
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.ReplaceAll(indexHTML, "__LISTEN_ADDR__", html.EscapeString(s.cfg.HTTPListen))))
}

func (s *Server) handleSharedGraphWallCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(sharedui.CSS()))
}

func (s *Server) handleSharedGraphWallJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	_, _ = w.Write([]byte(sharedui.JS()))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"ok":        true,
		"devices":   len(s.hub.Devices),
		"latestSeq": s.recorder.Ring().LatestSeq(),
		"can_ring":  s.canRingHealth(),
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.hub.Devices)
}

func (s *Server) handleTECCatalogue(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, mecom.BuildMeComTECCatalogue(mecom.MeComTECCatalogueConfig{
		ChannelCount:      utilityBaselineReadoutChannels(s.cfg),
		SourceSubject:     "utility://mecom/telemetry",
		FixtureProvenance: "utility_server",
	}))
}

func (s *Server) handleLoomSourceCatalogue(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.loomSourceCatalogue())
}

func (s *Server) loomSourceCatalogue() graphsem.GlobalSourceCatalogue {
	targets := s.Targets()
	rows := make([]graphsem.SourceCatalogueRow, 0, len(targets))
	writeTraceIDs := make([]string, 0)

	for _, target := range targets {
		if target.Metadata["catalogue_active"] != "true" {
			continue
		}
		if target.Metadata["parameter_id"] == "" && target.Metadata["mecom_parameter_id"] == "" {
			continue
		}
		row := loomCatalogueRowForTarget(target)
		rows = append(rows, row)
		if row.Access == "read_write" {
			writeTraceIDs = append(writeTraceIDs, row.TraceID)
		}
	}

	return graphsem.GlobalSourceCatalogue{
		SchemaVersion:   graphsem.CurrentSourceCatalogueSchemaVersion,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		SelectionOwner:  "loom.operator",
		Catalogues:      []graphsem.SourceCatalogue{loomTECCatalogue(rows, writeTraceIDs)},
		DiscoveryPolicy: loomTECDiscoveryPolicy(len(rows)),
		DiscoveredCatalogues: []graphsem.DiscoveredCatalogueRecord{
			{
				CatalogueID:    "meerstettergo.tec.pixtend",
				LinkedSourceID: "mecom_tec_bank_a",
				DisplayName:    "Meerstetter-Go TEC catalogue via PiXtend",
				SourceFamily:   graphsem.SourceFamily("mecom_tec"),
				Status:         "live_gateway",
				Owner:          "edge",
				EntryCount:     len(rows),
				RouteHint:      "/api/operator/meerstettergo/source-catalogue",
				ExplorerGroup:  "thermal plant",
				Priority:       "high",
				Notes: []string{
					"Rows are generated from the active Meerstetter-Go discovery targets.",
					"Writable rows expose /api/operator/meerstettergo/target/write and require a sequencer lease.",
				},
			},
		},
	}
}

func loomTECCatalogue(rows []graphsem.SourceCatalogueRow, writeTraceIDs []string) graphsem.SourceCatalogue {
	catalogue := graphsem.SourceCatalogue{
		SchemaVersion: graphsem.CurrentSourceCatalogueSchemaVersion,
		SourceID:      "mecom_tec_bank_a",
		SourceFamily:  graphsem.SourceFamily("mecom_tec"),
		DisplayName:   "Meerstetter-Go TEC bank via PiXtend",
		Page: graphsem.CataloguePage{
			Limit: len(rows),
			Total: len(rows),
		},
		Entries:      rows,
		Capabilities: loomTECDiscoveryPolicy(len(rows)),
	}
	if len(writeTraceIDs) > 0 {
		catalogue.CommandInputs = []graphsem.CommandInput{
			{
				CommandID:       "meerstettergo.tec.write",
				DisplayName:     "Write Meerstetter-Go TEC target",
				Group:           "thermal-control",
				CommandType:     "sequencer_write",
				Transport:       "mecom",
				Access:          "lease_required",
				OwnershipScope:  "device_target",
				SagaContract:    "sequencer_lease_and_meerstettergo_command_receipt_required",
				SourceSubject:   "command.v4.sequencer.meerstettergo.write",
				RelatedTraceIDs: writeTraceIDs,
				Parameters: []graphsem.CommandParameter{
					{Name: "target_id", Type: "string", Required: true},
					{Name: "value", Type: "float", Required: true},
					{Name: "lease_id", Type: "string", Required: true},
				},
			},
		}
	}
	return catalogue
}

func loomTECDiscoveryPolicy(maxSignals int) graphsem.SourceCapabilities {
	return graphsem.SourceCapabilities{
		SupportsLive:          true,
		SupportsMetadataOnly:  true,
		MaxSignals:            maxSignals,
		MaxBytesPerSecond:     65536,
		DefaultRateHz:         10,
		RecommendedRateHz:     10,
		DiscoveryCadenceSec:   30,
		SubscriptionEndpoint:  "/api/operator/meerstettergo/target/read",
		SelectionRequired:     true,
		PoliteAccessStatement: "Use ring-buffer polling for high-priority TEC values and round-robin reads for the rest; sequencer writes require an explicit lease.",
		RemoteRoutes: []graphsem.RemoteRoute{
			{
				RouteID:         "meerstettergo.pixtend.catalogue",
				RouteKind:       "source_catalogue",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/source-catalogue",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "live_discovery",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/loom/source-catalogue",
				Workflow:        "loom consumes Meerstetter-Go as the TEC data source",
			},
			{
				RouteID:         "meerstettergo.pixtend.health",
				RouteKind:       "health",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/health",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "live_status",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/health",
				Workflow:        "loom checks PiXtend edge readiness before attaching graph-wall consumers",
			},
			{
				RouteID:         "meerstettergo.pixtend.discovery_tree",
				RouteKind:       "discovery_tree",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/discovery/tree",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "live_discovery",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/discovery/tree",
				Workflow:        "loom builds the TEC signal tree from backend-owned typed discovery metadata",
			},
			{
				RouteID:         "meerstettergo.pixtend.graph_wall",
				RouteKind:       "graph_wall",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/graph-wall",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "live_discovery",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/graph-wall",
				Workflow:        "loom receives backend-authored graph-wall aggregate groups for the four-controller TEC bank",
			},
			{
				RouteID:         "meerstettergo.pixtend.tiles",
				RouteKind:       "history_tiles",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/tiles",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "ring_history",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/tiles",
				Workflow:        "loom graph wall reads reduced live and retained TEC history through the same PiXtend edge route",
			},
			{
				RouteID:         "meerstettergo.pixtend.log_ring",
				RouteKind:       "telemetry_ring",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/log/ring",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "ring_history",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/log/ring",
				Workflow:        "loom can bootstrap from the decoded in-memory telemetry ring after late attachment",
			},
			{
				RouteID:         "meerstettergo.pixtend.can_ring",
				RouteKind:       "raw_can_ring",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/can/ring",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "primary_ram_with_flash_fallback",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/can/ring",
				Workflow:        "loom can recover raw CAN evidence from RAM first and flash fallback when the owner connects late",
			},
			{
				RouteID:         "meerstettergo.pixtend.polling_status",
				RouteKind:       "polling_status",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/polling/status",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "live_status",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/polling/status",
				Workflow:        "loom verifies per-tier TEC polling freshness before treating PiXtend values as live",
			},
			{
				RouteID:         "meerstettergo.pixtend.read",
				RouteKind:       "target_read",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/target/read",
				AccessMode:      "read_only",
				State:           "configured",
				Freshness:       "on_demand",
				RouteBudget:     "2s request timeout",
				Provenance:      "meerstetter-go /api/target/read",
				Workflow:        "operator and sequencer read initialized TEC values by target id",
			},
			{
				RouteID:         "meerstettergo.pixtend.write",
				RouteKind:       "target_write",
				SourceHost:      "pixtend",
				GatewayEndpoint: "/api/operator/meerstettergo/target/write",
				AccessMode:      "lease_required",
				State:           "configured",
				Freshness:       "command_receipt",
				RouteBudget:     "2s request timeout",
				LeaseRequired:   true,
				ReceiptRequired: true,
				Provenance:      "meerstetter-go /api/target/write",
				Workflow:        "sequencer writes only through leased target commands",
			},
		},
		TransportPaths: []graphsem.TransportPath{
			{
				PathID:            "pixtend-socketcan-mecom",
				PathKind:          "edge_gateway",
				PhysicalTransport: "can",
				NetworkTransport:  "http",
				Endpoint:          "/api/loom/source-catalogue",
				State:             "configured",
				Workflow:          "PiXtend CAN to Meerstetter-Go catalogue to Loom source catalogue",
				Notes: []string{
					"USB serial, Kvaser USB, Kvaser DIN-rail, and Ethernet MeCom paths remain compatible transport targets behind the same catalogue shape.",
				},
			},
		},
	}
}

func loomCatalogueRowForTarget(target discovery.Target) graphsem.SourceCatalogueRow {
	metadata := cloneMetadata(target.Metadata)
	traceID := loomTraceIDForTarget(target)
	access := target.Metadata["access"]
	if access == "" {
		access = readoutAccess(target.Metadata["writable"] == "true")
	}
	if access == "read_only" {
		metadata["sequencer_write_path"] = ""
	} else if access == "read_write" {
		metadata["sequencer_write_path"] = "/api/operator/meerstettergo/target/write"
	}
	metadata["loom_read_path"] = "/api/operator/meerstettergo/target/read?id=" + url.QueryEscape(target.ID)
	metadata["target_id"] = target.ID
	metadata["source_id"] = "mecom_tec_bank_a"

	return graphsem.SourceCatalogueRow{
		TraceID:        traceID,
		RawName:        target.DictionaryEntry,
		DisplayName:    target.Name,
		Unit:           target.Unit,
		ValueType:      target.Metadata["value_type"],
		Access:         access,
		GraphSource:    "meerstettergo_edge",
		GraphType:      loomGraphType(target),
		Category:       graphsem.SignalCategory(target.Metadata["category"]),
		Kind:           graphsem.SignalKind(target.Kind),
		Role:           loomSignalRole(target),
		DefaultHint:    graphsem.GraphHint(loomDefaultHint(target)),
		SemanticStatus: "available_initialized",
		SourceSubject:  "telemetry.v4.local.pixtend.meerstettergo.live",
		RemoteRoute: &graphsem.RemoteRoute{
			RouteID:         "meerstettergo.pixtend." + traceID,
			RouteKind:       loomRouteKind(access),
			SourceHost:      "pixtend",
			GatewayEndpoint: metadata["loom_read_path"],
			AccessMode:      access,
			State:           "configured",
			Freshness:       target.Metadata["readout"],
			LeaseRequired:   access == "read_write",
			ReceiptRequired: access == "read_write",
			Provenance:      "meerstetter-go discovery target " + target.ID,
		},
		TargetID:       target.ID,
		TargetFormat:   target.Metadata["format"],
		TargetUse:      "telemetry_or_command",
		OwnerKind:      string(target.Ownership),
		OwnerNodeID:    target.NodeID,
		TargetMetadata: cloneMetadata(target.Metadata),
		Metadata:       metadata,
	}
}

func loomTraceIDForTarget(target discovery.Target) string {
	id := strings.TrimPrefix(target.ID, "device:")
	id = strings.ReplaceAll(id, ":", ".")
	id = strings.ReplaceAll(id, " ", "_")
	return id
}

func loomGraphType(target discovery.Target) string {
	switch strings.ToLower(target.Kind) {
	case "bool", "boolean", "state", "enum", "integer":
		return "state"
	default:
		return "line"
	}
}

func loomDefaultHint(target discovery.Target) string {
	if loomGraphType(target) == "state" {
		return "state"
	}
	return "line"
}

func loomSignalRole(target discovery.Target) graphsem.SignalRole {
	if target.Metadata["writable"] == "true" {
		return graphsem.SignalRole("reference")
	}
	return graphsem.SignalRole("monitor")
}

func loomRouteKind(access string) string {
	if access == "read_write" {
		return "target_read_write"
	}
	return "target_read"
}

func cloneMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+4)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Server) handleDiscoveryTree(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.refreshDiscovery(r.Context())
	}
	writeJSON(w, discovery.BuildTree(s.Targets()))
}

func (s *Server) handleGraphWall(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.GraphWall())
}

func (s *Server) handleGraphWallAssign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TargetID string `json:"target_id"`
		WallID   string `json:"wall_id"`
		NewWall  string `json:"new_wall_id"`
		Kind     string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target, ok := s.targetByID(req.TargetID)
	if !ok {
		http.Error(w, "unknown target", http.StatusNotFound)
		return
	}
	wallID := strings.TrimSpace(req.WallID)
	if strings.TrimSpace(req.NewWall) != "" {
		wallID = strings.TrimSpace(req.NewWall)
	}
	if wallID == "" {
		http.Error(w, "missing wall_id", http.StatusBadRequest)
		return
	}
	kind := graphwall.TileTrend
	if req.Kind != "" {
		kind = graphwall.TileKind(req.Kind)
	}
	target, kind, options := s.graphTargetForAssignment(target, kind)
	s.mu.Lock()
	for i := range s.graphWall {
		if s.graphWall[i].WallID == wallID && s.graphWall[i].Target.ID == target.ID {
			assignment := graphWallAssignment{
				WallID:   wallID,
				TileID:   s.graphWall[i].TileID,
				Kind:     kind,
				Target:   target,
				Position: s.graphWall[i].Position,
				Options:  mergeOptions(s.graphWall[i].Options, options),
			}
			s.graphWall[i] = assignment
			s.mu.Unlock()
			writeJSON(w, map[string]any{"ok": true, "assignment": assignment})
			return
		}
	}
	position := nextGraphPositionLocked(s.graphWall, wallID)
	assignment := graphWallAssignment{
		WallID:   wallID,
		TileID:   uniqueTileID(wallID + ":" + target.ID),
		Kind:     kind,
		Target:   target,
		Position: position,
		Options:  options,
	}
	s.graphWall = append(s.graphWall, assignment)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "assignment": assignment})
}

func (s *Server) handleRing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := parseIntQuery(r, "limit")
	entries := s.logEntries(parseUintQuery(r, "after_seq"), limit)
	if parseBoolQuery(r, "tail") {
		entries = s.tailLogEntries(limit)
	}
	writeJSON(w, entries)
}

type pollingStatusResponse struct {
	OK              bool                  `json:"ok"`
	Live            bool                  `json:"live"`
	GeneratedAt     time.Time             `json:"generated_at"`
	LatestSeq       uint64                `json:"latest_seq"`
	DeviceCount     int                   `json:"device_count"`
	TargetCount     int                   `json:"target_count"`
	FreshCount      int                   `json:"fresh_count"`
	StaleCount      int                   `json:"stale_count"`
	NotSampledCount int                   `json:"not_sampled_count"`
	ErrorCount      int                   `json:"error_count"`
	Devices         []pollingDeviceStatus `json:"devices"`
}

type pollingDeviceStatus struct {
	DeviceID           string               `json:"device_id"`
	Alias              string               `json:"alias,omitempty"`
	PreferredTransport string               `json:"preferred_transport,omitempty"`
	ActiveTransport    string               `json:"active_transport,omitempty"`
	TargetCount        int                  `json:"target_count"`
	FreshCount         int                  `json:"fresh_count"`
	StaleCount         int                  `json:"stale_count"`
	NotSampledCount    int                  `json:"not_sampled_count"`
	ErrorCount         int                  `json:"error_count"`
	Tiers              []pollingTierStatus  `json:"tiers"`
	Values             []pollingValueStatus `json:"values"`
}

type pollingTierStatus struct {
	Tier                   string  `json:"tier"`
	FreshnessBudgetSeconds int     `json:"freshness_budget_seconds"`
	TargetCount            int     `json:"target_count"`
	FreshCount             int     `json:"fresh_count"`
	StaleCount             int     `json:"stale_count"`
	NotSampledCount        int     `json:"not_sampled_count"`
	ErrorCount             int     `json:"error_count"`
	LatestObservedAt       string  `json:"latest_observed_at,omitempty"`
	OldestAgeSeconds       float64 `json:"oldest_age_seconds,omitempty"`
}

type pollingValueStatus struct {
	TargetID               string  `json:"target_id"`
	Sensor                 string  `json:"sensor,omitempty"`
	Parameter              string  `json:"parameter,omitempty"`
	Unit                   string  `json:"unit,omitempty"`
	Tier                   string  `json:"tier"`
	Quality                string  `json:"quality"`
	ValueState             string  `json:"value_state"`
	ObservedAt             string  `json:"observed_at,omitempty"`
	AgeSeconds             float64 `json:"age_seconds,omitempty"`
	FreshnessBudgetSeconds int     `json:"freshness_budget_seconds"`
	Fresh                  bool    `json:"fresh"`
	Readout                string  `json:"readout,omitempty"`
	Transport              string  `json:"transport,omitempty"`
	Protocol               string  `json:"protocol,omitempty"`
	Writable               bool    `json:"writable"`
}

type latestTelemetryEntry struct {
	tm       tmtc.Telemetry
	seq      uint64
	observed time.Time
}

func (s *Server) handlePollingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.pollingStatus(time.Now().UTC()))
}

func (s *Server) pollingStatus(now time.Time) pollingStatusResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	latest := latestTelemetryByTarget(s.recorder.Ring().Snapshot())
	response := pollingStatusResponse{
		OK:          true,
		GeneratedAt: now,
		LatestSeq:   s.recorder.Ring().LatestSeq(),
	}
	devices := map[string]*pollingDeviceStatus{}
	deviceOrder := []string{}

	for _, target := range s.Targets() {
		if target.Metadata["catalogue_active"] != "true" {
			continue
		}
		deviceID := firstNonEmpty(target.Metadata["device_id"], deviceIDFromTargetID(target.ID))
		if deviceID == "" {
			deviceID = "unknown"
		}
		device := devices[deviceID]
		if device == nil {
			device = &pollingDeviceStatus{
				DeviceID:           deviceID,
				Alias:              firstNonEmpty(target.Metadata["device_alias"], target.Metadata["alias"], deviceID),
				PreferredTransport: firstNonEmpty(target.Metadata["preferred_transport"], target.Metadata["primary_transport"], target.Transport),
				ActiveTransport:    firstNonEmpty(target.Metadata["active_transport"], target.Metadata["transport"], target.Transport),
			}
			devices[deviceID] = device
			deviceOrder = append(deviceOrder, deviceID)
		}
		value := pollingValueStatusForTarget(target, latest[target.ID], now)
		device.Values = append(device.Values, value)
		device.TargetCount++
		response.TargetCount++
		countPollingValue(&response.FreshCount, &response.StaleCount, &response.NotSampledCount, &response.ErrorCount, value)
		countPollingValue(&device.FreshCount, &device.StaleCount, &device.NotSampledCount, &device.ErrorCount, value)
	}

	sort.Strings(deviceOrder)
	for _, deviceID := range deviceOrder {
		device := devices[deviceID]
		sort.Slice(device.Values, func(i, j int) bool {
			if device.Values[i].Tier != device.Values[j].Tier {
				return pollingTierRank(device.Values[i].Tier) < pollingTierRank(device.Values[j].Tier)
			}
			return device.Values[i].TargetID < device.Values[j].TargetID
		})
		device.Tiers = pollingTiersFromValues(device.Values)
		response.Devices = append(response.Devices, *device)
	}
	response.DeviceCount = len(response.Devices)
	response.Live = response.FreshCount > 0
	return response
}

func latestTelemetryByTarget(entries []tmtclog.Entry) map[string]latestTelemetryEntry {
	latest := map[string]latestTelemetryEntry{}
	for _, entry := range entries {
		if entry.TM == nil || entry.TM.TargetID == "" {
			continue
		}
		observed := entry.TM.Time
		if observed.IsZero() {
			observed = entry.Time
		}
		current, ok := latest[entry.TM.TargetID]
		if !ok || entry.Seq >= current.seq {
			latest[entry.TM.TargetID] = latestTelemetryEntry{tm: *entry.TM, seq: entry.Seq, observed: observed}
		}
	}
	return latest
}

func pollingValueStatusForTarget(target discovery.Target, latest latestTelemetryEntry, now time.Time) pollingValueStatus {
	meta := cloneMetadata(target.Metadata)
	quality := "not_sampled"
	valueState := "not_sampled"
	observed := time.Time{}
	if latest.seq > 0 {
		for key, value := range latest.tm.Metadata {
			meta[key] = value
		}
		quality = firstNonEmpty(latest.tm.Quality, quality)
		valueState = firstNonEmpty(meta["value_state"], "sampled")
		observed = latest.observed
	}
	tier := pollingTier(meta, target)
	budget := pollingFreshnessBudgetSeconds(tier)
	ageSeconds := 0.0
	if !observed.IsZero() {
		ageSeconds = now.Sub(observed).Seconds()
		if ageSeconds < 0 {
			ageSeconds = 0
		}
	}
	errorState := pollingQualityIsError(quality)
	notSampled := latest.seq == 0 || quality == "not_sampled" || valueState == "not_sampled"
	stale := !notSampled && !errorState && ageSeconds > float64(budget)
	fresh := !notSampled && !errorState && !stale
	observedAt := ""
	if !observed.IsZero() {
		observedAt = observed.UTC().Format(time.RFC3339Nano)
	}
	return pollingValueStatus{
		TargetID:               target.ID,
		Sensor:                 firstNonEmpty(meta["sensor"], target.DictionaryEntry),
		Parameter:              firstNonEmpty(meta["parameter_name"], target.Name),
		Unit:                   firstNonEmpty(target.Unit, meta["unit"]),
		Tier:                   tier,
		Quality:                quality,
		ValueState:             valueState,
		ObservedAt:             observedAt,
		AgeSeconds:             ageSeconds,
		FreshnessBudgetSeconds: budget,
		Fresh:                  fresh,
		Readout:                firstNonEmpty(meta["readout"], meta["active_readout"], meta["preferred_readout"]),
		Transport:              firstNonEmpty(meta["active_transport"], meta["transport"], target.Transport),
		Protocol:               firstNonEmpty(meta["protocol"], target.Protocol),
		Writable:               meta["writable"] == "true",
	}
}

func countPollingValue(fresh, stale, notSampled, errors *int, value pollingValueStatus) {
	switch {
	case pollingQualityIsError(value.Quality):
		*errors += 1
	case value.ValueState == "not_sampled" || value.Quality == "not_sampled":
		*notSampled += 1
	case value.Fresh:
		*fresh += 1
	default:
		*stale += 1
	}
}

func pollingTiersFromValues(values []pollingValueStatus) []pollingTierStatus {
	tiers := map[string]*pollingTierStatus{}
	order := []string{}
	for _, value := range values {
		tier := tiers[value.Tier]
		if tier == nil {
			tier = &pollingTierStatus{
				Tier:                   value.Tier,
				FreshnessBudgetSeconds: value.FreshnessBudgetSeconds,
			}
			tiers[value.Tier] = tier
			order = append(order, value.Tier)
		}
		tier.TargetCount++
		switch {
		case pollingQualityIsError(value.Quality):
			tier.ErrorCount++
		case value.ValueState == "not_sampled" || value.Quality == "not_sampled":
			tier.NotSampledCount++
		case value.Fresh:
			tier.FreshCount++
		default:
			tier.StaleCount++
		}
		if value.ObservedAt != "" {
			if tier.LatestObservedAt == "" || value.ObservedAt > tier.LatestObservedAt {
				tier.LatestObservedAt = value.ObservedAt
			}
			if value.AgeSeconds > tier.OldestAgeSeconds {
				tier.OldestAgeSeconds = value.AgeSeconds
			}
		}
	}
	sort.Slice(order, func(i, j int) bool {
		return pollingTierRank(order[i]) < pollingTierRank(order[j])
	})
	out := make([]pollingTierStatus, 0, len(order))
	for _, name := range order {
		out = append(out, *tiers[name])
	}
	return out
}

func pollingTier(meta map[string]string, target discovery.Target) string {
	readout := firstNonEmpty(meta["readout"], meta["active_readout"], meta["preferred_readout"])
	sensor := firstNonEmpty(meta["sensor"], target.DictionaryEntry)
	switch {
	case readout == mecom.ReadoutDerivedChannelModel || meta["derived_signal"] == "true":
		return "derived_model"
	case meta["ring_reduction"] != "" || readout == mecom.ReadoutCRTVStreamRingBuffer || pollingSensorIsHighPriority(sensor):
		return "high_priority"
	default:
		return "round_robin"
	}
}

func pollingSensorIsHighPriority(sensor string) bool {
	switch utilitySignalSuffix(sensor) {
	case "object_temp_c", "sink_temp_c", "cascade_temp_c", "target_object_temp_c", "ramp_object_temp_c", "output_current_a", "output_voltage_v", "output_power_w":
		return true
	default:
		return false
	}
}

func pollingFreshnessBudgetSeconds(tier string) int {
	switch tier {
	case "high_priority":
		return 10
	case "derived_model":
		return 30
	default:
		return 60
	}
}

func pollingTierRank(tier string) int {
	switch tier {
	case "high_priority":
		return 0
	case "round_robin":
		return 1
	case "derived_model":
		return 2
	default:
		return 9
	}
}

func pollingQualityIsError(quality string) bool {
	quality = strings.ToLower(strings.TrimSpace(quality))
	return strings.Contains(quality, "error") || strings.Contains(quality, "failed")
}

func (s *Server) handleLogExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries := s.logEntriesFromRequest(r)
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	switch format {
	case "", "ndjson", "jsonl":
	case "arrow_ipc", "arrow", "ipc":
		var buf bytes.Buffer
		if err := writeTelemetryArrowIPC(&buf, entries); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", arrowIPCContentType)
		w.Header().Set("Content-Disposition", `attachment; filename="meerstettergo-telemetry.arrow"`)
		_, _ = w.Write(buf.Bytes())
		return
	default:
		http.Error(w, "unsupported export format", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="meerstettergo-log.ndjson"`)
	enc := json.NewEncoder(w)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func (s *Server) handleLogArchiveManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, archiveexport.DefaultArchiveManifest())
}

func writeTelemetryArrowIPC(w io.Writer, entries []tmtclog.Entry) error {
	builder := array.NewRecordBuilder(memory.DefaultAllocator, telemetryArchiveArrowSchema)
	defer builder.Release()

	seq := builder.Field(0).(*array.Uint64Builder)
	timestamp := builder.Field(1).(*array.Int64Builder)
	targetID := builder.Field(2).(*array.StringBuilder)
	deviceID := builder.Field(3).(*array.StringBuilder)
	deviceAlias := builder.Field(4).(*array.StringBuilder)
	instance := builder.Field(5).(*array.StringBuilder)
	parameter := builder.Field(6).(*array.StringBuilder)
	category := builder.Field(7).(*array.StringBuilder)
	subtype := builder.Field(8).(*array.StringBuilder)
	value := builder.Field(9).(*array.Float64Builder)
	unit := builder.Field(10).(*array.StringBuilder)
	quality := builder.Field(11).(*array.StringBuilder)
	sourcePath := builder.Field(12).(*array.StringBuilder)

	for _, entry := range entries {
		if entry.TM == nil {
			continue
		}
		tm := entry.TM
		meta := tm.Metadata
		observationTime := tm.Time
		if observationTime.IsZero() {
			observationTime = entry.Time
		}
		seq.Append(entry.Seq)
		timestamp.Append(observationTime.UnixNano())
		targetID.Append(tm.TargetID)
		deviceID.Append(firstNonEmpty(meta["device_id"], deviceIDFromTargetID(tm.TargetID)))
		deviceAlias.Append(firstNonEmpty(meta["device_alias"], meta["alias"], meta["device_id"], deviceIDFromTargetID(tm.TargetID)))
		instance.Append(firstNonEmpty(meta["instance"], meta["mecom_instance"]))
		parameter.Append(firstNonEmpty(meta["parameter_name"], meta["parameter"], tm.Name))
		category.Append(firstNonEmpty(meta["category"], meta["type"]))
		subtype.Append(meta["subtype"])
		if f, ok := telemetryFloat(tm.Value); ok {
			value.Append(f)
		} else {
			value.AppendNull()
		}
		unit.Append(firstNonEmpty(tm.Unit, meta["unit"]))
		quality.Append(tm.Quality)
		sourcePath.Append(firstNonEmpty(meta["source_path"], meta["preferred_transport"], meta["primary_transport"], meta["active_transport"], meta["transport"]))
	}

	record := builder.NewRecord()
	defer record.Release()

	writer := ipc.NewWriter(w, ipc.WithSchema(telemetryArchiveArrowSchema))
	if err := writer.Write(record); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func deviceIDFromTargetID(targetID string) string {
	if !strings.HasPrefix(targetID, "device:") {
		return ""
	}
	rest := strings.TrimPrefix(targetID, "device:")
	if idx := strings.IndexByte(rest, ':'); idx >= 0 {
		return rest[:idx]
	}
	return rest
}

func telemetryFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case nil:
		return 0, false
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func (s *Server) handleLogReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries := s.logEntriesFromRequest(r)
	writeJSON(w, logReviewSummary(entries))
}

func (s *Server) handleLogImportReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	entries, err := decodeLogNDJSON(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	summary := logReviewSummary(entries)
	summary["ok"] = true
	summary["mode"] = "review_only"
	summary["committed"] = false
	writeJSON(w, summary)
}

func (s *Server) logEntries(afterSeq uint64, limit int) []tmtclog.Entry {
	entries := s.recorder.ReplaySince(afterSeq)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func (s *Server) logEntriesFromRequest(r *http.Request) []tmtclog.Entry {
	limit := parseIntQuery(r, "limit")
	if parseBoolQuery(r, "tail") {
		return s.tailLogEntries(limit)
	}
	return s.logEntries(parseUintQuery(r, "after_seq"), limit)
}

func (s *Server) tailLogEntries(limit int) []tmtclog.Entry {
	entries := s.recorder.Ring().Snapshot()
	if limit > 0 && len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries
}

func decodeLogNDJSON(r io.Reader) ([]tmtclog.Entry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var entries []tmtclog.Entry
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var entry tmtclog.Entry
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			return nil, fmt.Errorf("decode log line %d: %w", line, err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func logReviewSummary(entries []tmtclog.Entry) map[string]any {
	summary := map[string]any{
		"ok":                  true,
		"entry_count":         len(entries),
		"by_kind":             map[string]int{},
		"targets":             []string{},
		"devices":             []string{},
		"qualities":           map[string]int{},
		"duplicate_seq_count": 0,
	}
	byKind := summary["by_kind"].(map[string]int)
	qualities := summary["qualities"].(map[string]int)
	targets := map[string]bool{}
	devices := map[string]bool{}
	seenSeq := map[uint64]bool{}
	var minSeq, maxSeq uint64
	var minTime, maxTime time.Time
	for _, entry := range entries {
		byKind[string(entry.Kind)]++
		if entry.Seq != 0 {
			if seenSeq[entry.Seq] {
				summary["duplicate_seq_count"] = summary["duplicate_seq_count"].(int) + 1
			}
			seenSeq[entry.Seq] = true
			if minSeq == 0 || entry.Seq < minSeq {
				minSeq = entry.Seq
			}
			if entry.Seq > maxSeq {
				maxSeq = entry.Seq
			}
		}
		if !entry.Time.IsZero() {
			if minTime.IsZero() || entry.Time.Before(minTime) {
				minTime = entry.Time
			}
			if maxTime.IsZero() || entry.Time.After(maxTime) {
				maxTime = entry.Time
			}
		}
		if entry.TM != nil {
			addReviewTarget(targets, devices, entry.TM.TargetID)
			if entry.TM.Quality != "" {
				qualities[entry.TM.Quality]++
			}
		}
		if entry.TC != nil {
			addReviewTarget(targets, devices, entry.TC.TargetID)
		}
	}
	if minSeq != 0 {
		summary["seq_min"] = minSeq
		summary["seq_max"] = maxSeq
	}
	if !minTime.IsZero() {
		summary["time_min"] = minTime.Format(time.RFC3339Nano)
		summary["time_max"] = maxTime.Format(time.RFC3339Nano)
	}
	summary["targets"] = sortedReviewKeys(targets)
	summary["devices"] = sortedReviewKeys(devices)
	return summary
}

func addReviewTarget(targets, devices map[string]bool, targetID string) {
	if targetID == "" {
		return
	}
	targets[targetID] = true
	if strings.HasPrefix(targetID, "device:") {
		parts := strings.SplitN(targetID, ":", 3)
		if len(parts) >= 2 && parts[1] != "" {
			devices[parts[1]] = true
		}
	}
}

func sortedReviewKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func (s *Server) handleCANRing(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit")
	if limit == 0 {
		limit = s.cfg.CANRingReplayLimit
	}
	if limit == 0 {
		limit = 1000
	}
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	var status map[string]any
	switch source {
	case "", "primary_ram":
		status = s.canRingStatus(limit)
	case "fallback_flash":
		status = s.canRingFallbackStatus(limit)
	case "merged", "reconciled":
		status = s.canRingMergedStatus(limit)
	default:
		http.Error(w, "unknown CAN ring source", http.StatusBadRequest)
		return
	}
	if ok, _ := status["ok"].(bool); !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	writeJSON(w, status)
}

func (s *Server) canRingStatus(limit int) map[string]any {
	primaryPath := strings.TrimSpace(s.cfg.CANRingPath)
	fallbackPath := strings.TrimSpace(s.cfg.CANRingFallbackPath)
	if primaryPath == "" {
		return map[string]any{"ok": false, "configured": false}
	}
	status, err := canRingSnapshotStatus(primaryPath, limit)
	if err != nil {
		if fallbackPath != "" {
			fallback, fallbackErr := canRingSnapshotStatus(fallbackPath, limit)
			if fallbackErr == nil {
				fallback["source"] = "fallback_flash"
				fallback["primary_path"] = primaryPath
				fallback["primary_error"] = err.Error()
				fallback["degraded"] = true
				return fallback
			}
			return map[string]any{
				"ok":             false,
				"configured":     true,
				"path":           primaryPath,
				"storage":        canRingStorageRole(primaryPath),
				"fallback_path":  fallbackPath,
				"fallback_error": fallbackErr.Error(),
				"error":          err.Error(),
			}
		}
		return map[string]any{
			"ok":         false,
			"configured": true,
			"path":       primaryPath,
			"storage":    canRingStorageRole(primaryPath),
			"error":      err.Error(),
		}
	}
	status["source"] = "primary_ram"
	status["fallback_path"] = fallbackPath
	return status
}

func (s *Server) canRingFallbackStatus(limit int) map[string]any {
	primaryPath := strings.TrimSpace(s.cfg.CANRingPath)
	fallbackPath := strings.TrimSpace(s.cfg.CANRingFallbackPath)
	if fallbackPath == "" {
		return map[string]any{
			"ok":           false,
			"configured":   false,
			"source":       "fallback_flash",
			"primary_path": primaryPath,
			"error":        "CAN ring fallback path is not configured",
		}
	}
	status, err := canRingSnapshotStatus(fallbackPath, limit)
	if err != nil {
		return map[string]any{
			"ok":           false,
			"configured":   true,
			"path":         fallbackPath,
			"storage":      canRingStorageRole(fallbackPath),
			"source":       "fallback_flash",
			"primary_path": primaryPath,
			"error":        err.Error(),
		}
	}
	status["source"] = "fallback_flash"
	status["primary_path"] = primaryPath
	return status
}

func (s *Server) canRingMergedStatus(limit int) map[string]any {
	primaryPath := strings.TrimSpace(s.cfg.CANRingPath)
	fallbackPath := strings.TrimSpace(s.cfg.CANRingFallbackPath)
	if primaryPath == "" && fallbackPath == "" {
		return map[string]any{
			"ok":         false,
			"configured": false,
			"source":     "merged",
			"error":      "no CAN ring paths are configured",
		}
	}

	var primarySnapshot canring.Snapshot
	var fallbackSnapshot canring.Snapshot
	var primaryRecords []canring.Record
	var fallbackRecords []canring.Record
	var primaryErr error
	var fallbackErr error
	if primaryPath != "" {
		primarySnapshot, primaryErr = canRingSnapshot(primaryPath, limit)
		if primaryErr == nil {
			primaryRecords = primarySnapshot.Records
		}
	}
	if fallbackPath != "" {
		fallbackSnapshot, fallbackErr = canRingSnapshot(fallbackPath, limit)
		if fallbackErr == nil {
			fallbackRecords = fallbackSnapshot.Records
		}
	}
	if primaryErr != nil && fallbackErr != nil {
		return map[string]any{
			"ok":             false,
			"configured":     true,
			"source":         "merged",
			"path":           primaryPath,
			"fallback_path":  fallbackPath,
			"primary_error":  primaryErr.Error(),
			"fallback_error": fallbackErr.Error(),
		}
	}
	if primaryErr != nil && fallbackPath == "" {
		return map[string]any{
			"ok":            false,
			"configured":    true,
			"source":        "merged",
			"path":          primaryPath,
			"primary_error": primaryErr.Error(),
		}
	}
	if fallbackErr != nil && primaryPath == "" {
		return map[string]any{
			"ok":             false,
			"configured":     true,
			"source":         "merged",
			"fallback_path":  fallbackPath,
			"fallback_error": fallbackErr.Error(),
		}
	}

	records := canring.MergeRecords(primaryRecords, fallbackRecords, limit)
	status := map[string]any{
		"ok":            true,
		"configured":    true,
		"source":        "merged",
		"storage":       "primary_ram+fallback_flash",
		"path":          primaryPath,
		"fallback_path": fallbackPath,
		"records":       canRingRecordsJSON(records),
	}
	if primaryErr == nil && primaryPath != "" {
		status["stats"] = primarySnapshot.Stats
		status["valid_chunks"] = primarySnapshot.ValidChunks
		status["primary"] = map[string]any{
			"path":         primaryPath,
			"storage":      canRingStorageRole(primaryPath),
			"stats":        primarySnapshot.Stats,
			"valid_chunks": primarySnapshot.ValidChunks,
			"records":      len(primaryRecords),
		}
	} else if primaryErr != nil {
		status["primary_error"] = primaryErr.Error()
		status["degraded"] = true
	}
	if fallbackErr == nil && fallbackPath != "" {
		status["fallback"] = map[string]any{
			"path":         fallbackPath,
			"storage":      canRingStorageRole(fallbackPath),
			"stats":        fallbackSnapshot.Stats,
			"valid_chunks": fallbackSnapshot.ValidChunks,
			"records":      len(fallbackRecords),
		}
	} else if fallbackErr != nil {
		status["fallback_error"] = fallbackErr.Error()
		status["degraded"] = true
	}
	return status
}

func canRingSnapshot(path string, limit int) (canring.Snapshot, error) {
	reader, err := canring.OpenReader(path)
	if err != nil {
		return canring.Snapshot{}, err
	}
	defer reader.Close()
	return reader.Snapshot(limit)
}

func canRingSnapshotStatus(path string, limit int) (map[string]any, error) {
	snapshot, err := canRingSnapshot(path, limit)
	if err != nil {
		return nil, err
	}
	records := canRingRecordsJSON(snapshot.Records)
	return map[string]any{
		"ok":           true,
		"configured":   true,
		"path":         path,
		"storage":      canRingStorageRole(path),
		"stats":        snapshot.Stats,
		"valid_chunks": snapshot.ValidChunks,
		"records":      records,
	}, nil
}

func canRingRecordsJSON(snapshotRecords []canring.Record) []map[string]any {
	records := make([]map[string]any, 0, len(snapshotRecords))
	for _, record := range snapshotRecords {
		dlc := int(record.DLC)
		if dlc > len(record.Data) {
			dlc = len(record.Data)
		}
		records = append(records, map[string]any{
			"seq":           record.Seq,
			"time":          record.Time.Format(time.RFC3339Nano),
			"elapsed_nanos": record.ElapsedNanos,
			"id":            record.ID,
			"id_hex":        fmt.Sprintf("%03X", record.ID),
			"dlc":           record.DLC,
			"data_hex":      strings.ToUpper(hex.EncodeToString(record.Data[:dlc])),
			"interface":     record.Interface,
			"chunk":         record.Chunk,
		})
	}
	return records
}

func (s *Server) canRingHealth() map[string]any {
	primaryPath := strings.TrimSpace(s.cfg.CANRingPath)
	fallbackPath := strings.TrimSpace(s.cfg.CANRingFallbackPath)
	if primaryPath == "" {
		return map[string]any{"ok": false, "configured": false}
	}
	primary, err := canRingStats(primaryPath)
	if err != nil {
		health := map[string]any{
			"ok":            false,
			"configured":    true,
			"path":          primaryPath,
			"storage":       canRingStorageRole(primaryPath),
			"fallback_path": fallbackPath,
			"error":         err.Error(),
		}
		if fallbackPath != "" {
			if fallback, fallbackErr := canRingStats(fallbackPath); fallbackErr == nil {
				health["ok"] = true
				health["degraded"] = true
				health["source"] = "fallback_flash"
				health["fallback"] = fallback
			} else {
				health["fallback_error"] = fallbackErr.Error()
			}
		}
		return health
	}
	health := map[string]any{
		"ok":            true,
		"configured":    true,
		"path":          primaryPath,
		"storage":       canRingStorageRole(primaryPath),
		"source":        "primary_ram",
		"fallback_path": fallbackPath,
		"stats":         primary,
	}
	if fallbackPath != "" {
		if fallback, fallbackErr := canRingStats(fallbackPath); fallbackErr == nil {
			health["fallback"] = fallback
		} else {
			health["fallback_error"] = fallbackErr.Error()
		}
	}
	return health
}

func canRingStats(path string) (canring.Stats, error) {
	reader, err := canring.OpenReader(path)
	if err != nil {
		return canring.Stats{}, err
	}
	defer reader.Close()
	return reader.Stats(), nil
}

func canRingStorageRole(path string) string {
	clean := strings.TrimSpace(path)
	if strings.HasPrefix(clean, "/run/") || strings.HasPrefix(clean, "/dev/shm/") {
		return "primary_ram"
	}
	if clean != "" {
		return "fallback_flash"
	}
	return "unconfigured"
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	afterSeq := parseUintQuery(r, "after_seq")
	writeJSON(w, swimlaneEvents(s.recorder.ReplaySince(afterSeq)))
}

func (s *Server) handleTargetRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target, ok := s.targetByID(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "unknown target", http.StatusNotFound)
		return
	}
	tm, err := s.readTarget(r.Context(), target)
	if err != nil {
		writeJSON(w, map[string]any{
			"ok":        false,
			"target_id": target.ID,
			"absent":    true,
			"error":     err.Error(),
		})
		return
	}
	_ = s.recorder.PublishTelemetry(tm)
	writeJSON(w, map[string]any{
		"ok":        true,
		"target_id": target.ID,
		"telemetry": tm,
	})
}

func (s *Server) handleTargetWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !writePrivilegedRequest(r) {
		http.Error(w, "write access requires local, LAN, or Tailnet direct access", http.StatusForbidden)
		return
	}
	var req struct {
		TargetID string `json:"target_id"`
		Value    any    `json:"value"`
		LeaseID  string `json:"lease_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.LeaseID) == "" {
		http.Error(w, "write access requires an explicit sequencer lease_id", http.StatusPreconditionRequired)
		return
	}
	target, ok := s.targetByID(req.TargetID)
	if !ok {
		http.Error(w, "unknown target", http.StatusNotFound)
		return
	}
	event, err := s.writeTarget(r.Context(), target, req.Value, req.LeaseID)
	if err != nil {
		event.Status = tmtc.CommandFailed
		event.Error = err.Error()
		_ = s.recorder.PublishCommandEvent(event)
		writeJSON(w, map[string]any{"ok": false, "event": event, "error": err.Error()})
		return
	}
	_ = s.recorder.PublishCommandEvent(event)
	if tm, readErr := s.readTarget(r.Context(), target); readErr == nil {
		_ = s.recorder.PublishTelemetry(tm)
	}
	writeJSON(w, map[string]any{"ok": true, "event": event})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) targetByID(id string) (discovery.Target, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, target := range s.targets {
		if target.ID == id {
			return target, true
		}
	}
	return discovery.Target{}, false
}

func nextGraphPositionLocked(assignments []graphWallAssignment, wallID string) graphwall.Position {
	maxY := 0
	for _, assignment := range assignments {
		if assignment.WallID != wallID {
			continue
		}
		if bottom := assignment.Position.Y + assignment.Position.H; bottom > maxY {
			maxY = bottom
		}
	}
	return graphwall.Position{X: 0, Y: maxY, W: 12, H: 4}
}

func uniqueTileID(targetID string) string {
	replacer := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return replacer.Replace(strings.TrimSpace(targetID))
}

func mergeOptions(existing, next map[string]any) map[string]any {
	if len(existing) == 0 {
		return next
	}
	merged := make(map[string]any, len(existing)+len(next))
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range next {
		merged[key] = value
	}
	return merged
}

func (s *Server) graphTargetForAssignment(target discovery.Target, requested graphwall.TileKind) (discovery.Target, graphwall.TileKind, map[string]any) {
	group := graphGroupForTarget(target)
	if group == graphwall.AggregateOther {
		return target, requested, map[string]any{"window_ms": 0}
	}
	if group == graphwall.AggregateState {
		group = graphwall.AggregateEvents
	}
	aggregate, ok := s.targetByID(aggregateTargetID(group))
	if !ok {
		return target, requested, map[string]any{"window_ms": 0}
	}
	kind := graphwall.TileTrend
	if group == graphwall.AggregateEvents {
		kind = graphwall.TileLog
	}
	options := map[string]any{
		"aggregate":   group,
		"axis_policy": graphwall.AxisPolicyForAggregate(group, aggregate.Unit),
		"window_ms":   0,
	}
	return aggregate, kind, options
}

func (s *Server) readTarget(ctx context.Context, target discovery.Target) (tmtc.Telemetry, error) {
	device, ok := s.deviceByID(target.NodeID)
	if !ok {
		return tmtc.Telemetry{}, fmt.Errorf("device %q not found", target.NodeID)
	}
	paramID, instance, err := targetParameter(target)
	if err != nil {
		return tmtc.Telemetry{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, device.Queue.RequestTimeout)
	defer cancel()
	client, closeFn, activeDevice, err := activeBrokerClientForDevice(reqCtx, device)
	if err != nil {
		return tmtc.Telemetry{}, err
	}
	defer closeFn()
	value, err := readParameter(reqCtx, client, target, paramID, instance)
	metadata := map[string]string{
		"transport": activeDevice.Target,
		"protocol":  utilityProtocolForTarget(activeDevice.Target),
		"readout":   readoutForTarget(s.cfg, target.ID),
	}
	addDeviceRedundancyMetadata(metadata, activeDevice)
	tm := tmtc.Telemetry{
		ID:       strings.TrimPrefix(target.ID, "device:"),
		TargetID: target.ID,
		Time:     time.Now().UTC(),
		Name:     target.Name,
		Value:    value,
		Unit:     target.Unit,
		Quality:  "ok",
		Metadata: metadata,
	}
	if err != nil {
		tm.Quality = "read_error"
		tm.Value = nil
		tm.Metadata["error"] = err.Error()
		return tm, err
	}
	if label := enumLabel(target, value); label != "" {
		tm.Metadata["value_label"] = label
	}
	if f, ok := value.(float64); ok && (math.IsNaN(f) || math.IsInf(f, 0)) {
		tm.Value = nil
		tm.Metadata["raw_float"] = fmt.Sprintf("%g", f)
		tm.Metadata["value_state"] = "not_applicable"
	}
	return tm, nil
}

func (s *Server) writeTarget(ctx context.Context, target discovery.Target, value any, leaseID string) (tmtc.CommandEvent, error) {
	event := tmtc.CommandEvent{
		ID:        fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		CommandID: target.ID,
		Time:      time.Now().UTC(),
		Status:    tmtc.CommandAcked,
		Transport: target.Transport,
		Metadata: map[string]string{
			"target_id": target.ID,
			"lease_id":  strings.TrimSpace(leaseID),
		},
	}
	if !targetWritable(target) {
		return event, fmt.Errorf("target %s is read-only", target.ID)
	}
	device, ok := s.deviceByID(target.NodeID)
	if !ok {
		return event, fmt.Errorf("device %q not found", target.NodeID)
	}
	addDeviceRedundancyMetadata(event.Metadata, device)
	paramID, instance, err := targetParameter(target)
	if err != nil {
		return event, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, device.Queue.RequestTimeout)
	defer cancel()
	client, closeFn, err := writeClientForDevice(reqCtx, device)
	if err != nil {
		return event, err
	}
	defer closeFn()
	if err := writeParameter(reqCtx, client, target, paramID, instance, value); err != nil {
		return event, err
	}
	event.Result = map[string]any{"target_id": target.ID, "value": value}
	return event, nil
}

func (s *Server) deviceByID(id string) (mecomserver.DeviceConfig, bool) {
	for _, device := range s.hub.Devices {
		if device.ID == id {
			return device, true
		}
	}
	return mecomserver.DeviceConfig{}, false
}

func clientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (mecomReader, func(), error) {
	client, closeFn, _, err := activeClientForDevice(ctx, device)
	return client, closeFn, err
}

func brokerClientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (mecomReader, func(), error) {
	client, closeFn, _, err := activeBrokerClientForDevice(ctx, device)
	return client, closeFn, err
}

func activeClientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (mecomReader, func(), mecomserver.DeviceConfig, error) {
	return activeClientForDeviceWithDialer(ctx, device, clientForTarget)
}

func activeBrokerClientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (mecomReader, func(), mecomserver.DeviceConfig, error) {
	return activeClientForDeviceWithDialer(ctx, device, brokerClientForTarget)
}

type targetDialer func(context.Context, mecomserver.DeviceConfig, string) (mecomReader, func(), error)

func activeClientForDeviceWithDialer(ctx context.Context, device mecomserver.DeviceConfig, dial targetDialer) (mecomReader, func(), mecomserver.DeviceConfig, error) {
	var failures []string
	for _, target := range deviceTargetCandidates(device) {
		activeDevice := deviceWithActiveTarget(device, target)
		client, closeFn, err := dial(ctx, device, target)
		if err == nil {
			return client, closeFn, activeDevice, nil
		}
		if closeFn != nil {
			closeFn()
		}
		failures = append(failures, fmt.Sprintf("%s: %v", target, err))
	}
	return nil, nil, device, fmt.Errorf("all transports failed for %s: %s", device.ID, strings.Join(failures, "; "))
}

func clientForTarget(ctx context.Context, device mecomserver.DeviceConfig, target string) (mecomReader, func(), error) {
	return readerForTarget(ctx, device, target)
}

func brokerClientForTarget(ctx context.Context, device mecomserver.DeviceConfig, target string) (mecomReader, func(), error) {
	dialTarget := target
	if target == device.Target && !isSocketCANTarget(target) {
		if passthrough := localPassthroughTarget(device.PassthroughListen); passthrough != "" {
			dialTarget = passthrough
		}
	}
	return readerForTarget(ctx, device, dialTarget)
}

func readerForTarget(ctx context.Context, device mecomserver.DeviceConfig, target string) (mecomReader, func(), error) {
	client, closeFn, err := socketCANReaderForTarget(ctx, device, target)
	if err != nil || client != nil {
		return client, closeFn, err
	}
	return serialClientForTarget(ctx, device, target)
}

func writeClientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (*mecom.Client, func(), error) {
	var failures []string
	for _, target := range deviceTargetCandidates(device) {
		if isSocketCANTarget(target) {
			failures = append(failures, target+": socketcan write path is not enabled until live MeCom-CAN write framing is proven")
			continue
		}
		dialTarget := target
		if target == device.Target {
			if passthrough := localPassthroughTarget(device.PassthroughListen); passthrough != "" {
				dialTarget = passthrough
			}
		}
		client, closeFn, err := serialClientForTarget(ctx, device, dialTarget)
		if err == nil {
			return client, closeFn, nil
		}
		if closeFn != nil {
			closeFn()
		}
		failures = append(failures, fmt.Sprintf("%s: %v", target, err))
	}
	return nil, nil, fmt.Errorf("no writable transport available for %s: %s", device.ID, strings.Join(failures, "; "))
}

func serialClientForTarget(ctx context.Context, device mecomserver.DeviceConfig, target string) (*mecom.Client, func(), error) {
	ep, ok := transport.ParseEndpoint(target)
	if !ok {
		return nil, nil, fmt.Errorf("invalid target")
	}
	conn, err := transport.Dial(ctx, ep, device.Queue.RequestTimeout)
	if err != nil {
		return nil, nil, err
	}
	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0, Timeout: device.Queue.RequestTimeout})
	return client, func() { _ = conn.Close() }, nil
}

func isSocketCANTarget(target string) bool {
	return isDirectCANTarget(target)
}

func localPassthroughTarget(listen string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil || port == "" {
		return ""
	}
	switch strings.Trim(host, "[]") {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "tcp:" + net.JoinHostPort(host, port)
}

func deviceTargetCandidates(device mecomserver.DeviceConfig) []string {
	candidates := make([]string, 0, 2+len(device.RedundantTargets))
	seen := map[string]struct{}{}
	add := func(target string) {
		target = strings.TrimSpace(target)
		if target == "" {
			return
		}
		if _, ok := seen[target]; ok {
			return
		}
		seen[target] = struct{}{}
		candidates = append(candidates, target)
	}
	add(device.Target)
	if passthrough := serialDeviceServerTarget(device); passthrough != "" {
		add(passthrough)
	}
	for _, target := range device.RedundantTargets {
		add(target)
	}
	return candidates
}

func deviceWithActiveTarget(device mecomserver.DeviceConfig, target string) mecomserver.DeviceConfig {
	device.Target = strings.TrimSpace(target)
	return device
}

func primaryTransport(device mecomserver.DeviceConfig) string {
	if device.Metadata != nil {
		if primary := strings.TrimSpace(device.Metadata["primary_transport"]); primary != "" {
			return primary
		}
	}
	return strings.TrimSpace(device.Target)
}

func redundantTargetsCSV(device mecomserver.DeviceConfig) string {
	if len(device.RedundantTargets) > 0 {
		return strings.Join(device.RedundantTargets, ",")
	}
	if device.Metadata != nil {
		return strings.TrimSpace(device.Metadata["redundant_targets"])
	}
	return ""
}

func serialDeviceServerTarget(device mecomserver.DeviceConfig) string {
	downstream := strings.TrimSpace(device.PassthroughTarget())
	if downstream == "" || isSocketCANTarget(downstream) {
		return ""
	}
	return localPassthroughTarget(device.PassthroughListen)
}

func availableTransportsCSV(device mecomserver.DeviceConfig) string {
	return strings.Join(deviceTargetCandidates(device), ",")
}

func addDeviceRedundancyMetadata(metadata map[string]string, device mecomserver.DeviceConfig) {
	if metadata == nil {
		return
	}
	preferred := primaryTransport(device)
	metadata["primary_transport"] = preferred
	metadata["preferred_transport"] = preferred
	metadata["active_transport"] = strings.TrimSpace(device.Target)
	metadata["redundant_targets"] = redundantTargetsCSV(device)
	metadata["available_transports"] = availableTransportsCSV(device)
	metadata["active_transport_policy"] = "preferred_then_available_candidates"
	if downstream := strings.TrimSpace(device.PassthroughTarget()); downstream != "" && !isSocketCANTarget(downstream) {
		metadata["passthrough_downstream"] = downstream
		metadata["physical_serial_target"] = downstream
	}
	if serialServer := serialDeviceServerTarget(device); serialServer != "" {
		metadata["serial_device_server"] = serialServer
	}
}

func targetParameter(target discovery.Target) (int, int, error) {
	paramValue := target.Metadata["parameter_id"]
	if strings.TrimSpace(paramValue) == "" {
		paramValue = target.Metadata["mecom_parameter_id"]
	}
	paramID, err := strconv.Atoi(paramValue)
	if err != nil || paramID <= 0 {
		return 0, 0, fmt.Errorf("target %s is not a MeCom parameter", target.ID)
	}
	instance, err := strconv.Atoi(target.Metadata["instance"])
	if err != nil {
		return 0, 0, fmt.Errorf("target %s has invalid instance", target.ID)
	}
	return paramID, instance, nil
}

func readParameter(ctx context.Context, client mecomScalarReader, target discovery.Target, paramID, instance int) (any, error) {
	switch strings.ToUpper(target.Metadata["format"]) {
	case "INT32", "UINT32":
		v, err := client.ReadInt32(ctx, paramID, instance)
		return v, err
	default:
		return client.ReadFloat32(ctx, paramID, instance)
	}
}

func enumLabel(target discovery.Target, value any) string {
	if len(target.Enum) == 0 {
		return ""
	}
	var key int64
	switch v := value.(type) {
	case int32:
		key = int64(v)
	case int:
		key = int64(v)
	case int64:
		key = v
	case float64:
		key = int64(v)
	default:
		return ""
	}
	return target.Enum[key]
}

func writeParameter(ctx context.Context, client *mecom.Client, target discovery.Target, paramID, instance int, value any) error {
	switch strings.ToUpper(target.Metadata["format"]) {
	case "INT32", "UINT32":
		v, err := coerceInt32(value)
		if err != nil {
			return err
		}
		return client.WriteInt32(ctx, paramID, instance, v)
	default:
		v, err := coerceFloat32(value)
		if err != nil {
			return err
		}
		return client.WriteFloat32(ctx, paramID, instance, v)
	}
}

func coerceFloat32(value any) (float32, error) {
	switch v := value.(type) {
	case float64:
		return float32(v), nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 32)
		return float32(f), err
	default:
		return 0, fmt.Errorf("expected numeric value")
	}
}

func coerceInt32(value any) (int32, error) {
	switch v := value.(type) {
	case float64:
		return int32(v), nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
		return int32(i), err
	default:
		return 0, fmt.Errorf("expected integer value")
	}
}

func readoutForTarget(cfg Config, targetID string) string {
	return string(cfg.ReadPolicy.ModeFor(targetID))
}

func targetsFromHub(ctx context.Context, hub mecomserver.HubConfig, cfg Config) []discovery.Target {
	instancesByDevice := discoverInstances(ctx, hub, cfg)
	profilesByDevice := discoverDeviceProfiles(ctx, hub, cfg)
	targets := make([]discovery.Target, 0, len(hub.Devices)*64)
	targets = append(targets, aggregateTargets()...)
	for _, device := range hub.Devices {
		instances := instancesByDevice[device.ID]
		profile := profilesByDevice[device.ID]
		params := discoveryParametersForFamily(cfg.ParameterRegistryPath, profile.Family)
		group := []string{"Devices", device.ID}
		targets = append(targets, tecCatalogueTargets(device, profile, utilityBaselineReadoutChannels(cfg))...)
		targets = append(targets, parameterTargets(device, params, instances, profile)...)
		statusMetadata := map[string]string{
			"readout":             "single",
			"graph_group":         graphwall.AggregateState,
			"axis_policy":         graphwall.AxisStateLane,
			"passthrough_listen":  device.PassthroughListen,
			"connection":          "transparent",
			"transparent_targets": "ethernet,serial,can",
			"controller_family":   profile.Family,
			"controller_model":    profile.Model,
			"controller_type":     profile.DeviceTypeString(),
			"discovery_quality":   profile.Quality,
			"discovery_reason":    profile.Reason,
		}
		addDeviceRedundancyMetadata(statusMetadata, device)
		targets = append(targets, discovery.Target{
			ID:        "device:" + device.ID + ":status",
			Name:      "Status",
			Group:     group,
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipLocalNode,
			Protocol:  string(objectdict.ProtocolMeCom),
			NodeID:    device.ID,
			Transport: device.Target,
			Metadata:  statusMetadata,
		})
		passthroughMetadata := map[string]string{
			"downstream": device.PassthroughTarget(),
			"tc_demux":   "serialized_downstream",
		}
		addDeviceRedundancyMetadata(passthroughMetadata, device)
		targets = append(targets, discovery.Target{
			ID:        "device:" + device.ID + ":passthrough",
			Name:      "Original Software Passthrough",
			Group:     group,
			Direction: discovery.DirectionIO,
			Ownership: discovery.OwnershipShared,
			Protocol:  string(objectdict.ProtocolMeCom),
			NodeID:    device.ID,
			Transport: device.PassthroughListen,
			Metadata:  passthroughMetadata,
		})
	}
	return sortDiscoveryTargets(targets)
}

func discoveryParametersForFamily(path, family string) []mecomdict.ParameterDef {
	params, err := mecomdict.LoadParameterRegistryForFamily(path, family)
	if err != nil {
		log.Printf("utility: MeCom parameter registry unavailable at %s: %v", path, err)
		return nil
	}
	return params
}

func tecCatalogueTargets(device mecomserver.DeviceConfig, profile deviceProfile, channels int) []discovery.Target {
	catalogue := mecom.BuildMeComTECCatalogue(mecom.MeComTECCatalogueConfig{
		ChannelCount:      channels,
		SourceSubject:     "utility://mecom/telemetry",
		FixtureProvenance: "utility_server",
	})
	deviceLabel := device.ID
	if strings.TrimSpace(profile.Model) != "" {
		deviceLabel = fmt.Sprintf("%s (%s)", device.ID, profile.Model)
	}
	targets := make([]discovery.Target, 0, len(catalogue.Entries))
	for _, row := range catalogue.Entries {
		targetID := utilitySignalTargetID(device, row.TraceID)
		category, subtype, parameter := utilitySignalLabels(row.TraceID, row.Unit)
		metadata := copyStringMap(row.Metadata)
		if sourceName := strings.TrimSpace(metadata["source_parameter_name"]); sourceName != "" {
			parameter = sourceName
			if utilitySignalSuffix(row.TraceID) == "output_voltage_v" {
				parameter = "Output Voltage"
			}
		}
		writable := metadata["mecom_parameter_id"] == "3000" || strings.HasSuffix(row.TraceID, ".target_object_temp_c")
		if metadata["derived_signal"] == "true" {
			writable = false
		}
		direction := discovery.DirectionTM
		if writable {
			direction = discovery.DirectionIO
		}
		readout := metadata["preferred_readout"]
		if readout == "" {
			readout = mecom.ReadoutVXRoundRobinQueue
		}
		instance := metadata["mecom_instance"]
		if instance == "" {
			instance = strconv.Itoa(utilitySensorInstance(row.TraceID))
		}
		if strings.TrimSpace(metadata["parameter_id"]) == "" && strings.TrimSpace(metadata["mecom_parameter_id"]) != "" {
			metadata["parameter_id"] = metadata["mecom_parameter_id"]
		}
		kind := string(row.Kind)
		if kind == "" {
			kind = string(objectdict.ValueKindContinuous)
		}
		graphGroup := graphGroupForSignal(row.TraceID, row.Unit)
		metadata["catalogue_active"] = "true"
		metadata["trace_id"] = row.TraceID
		metadata["category"] = category
		metadata["subtype"] = subtype
		metadata["parameter_name"] = parameter
		metadata["instance"] = instance
		metadata["format"] = row.ValueType
		metadata["value_type"] = row.ValueType
		metadata["sensor"] = row.TraceID
		metadata["unit"] = row.Unit
		metadata["readable"] = "true"
		metadata["writable"] = strconv.FormatBool(writable)
		metadata["access"] = readoutAccess(writable)
		metadata["read_path"] = "/api/target/read?id=" + url.QueryEscape(targetID)
		metadata["graph_group"] = graphGroup
		metadata["axis_policy"] = graphwall.AxisPolicyForAggregate(graphGroup, row.Unit)
		applyTransportReadoutMetadata(metadata, device, readout)
		metadata["readout"] = metadata["active_readout"]
		metadata["controller_family"] = profile.Family
		metadata["controller_model"] = profile.Model
		metadata["controller_type"] = profile.DeviceTypeString()
		metadata["discovery_quality"] = profile.Quality
		metadata["discovery_reason"] = profile.Reason
		if writable {
			metadata["write_path"] = "/api/target/write"
		}
		addDeviceRedundancyMetadata(metadata, device)
		targets = append(targets, discovery.Target{
			ID:              targetID,
			Name:            parameter,
			Group:           []string{"Signals", category, subtype, parameter, deviceLabel},
			Direction:       direction,
			Ownership:       discovery.OwnershipLocalNode,
			Protocol:        string(objectdict.ProtocolMeCom),
			NodeID:          device.ID,
			Transport:       device.Target,
			Address:         fmt.Sprintf("trace=%s instance=%s", row.TraceID, instance),
			Unit:            row.Unit,
			Kind:            kind,
			Dictionary:      "mecom-utility-tec-catalogue",
			DictionaryEntry: row.TraceID,
			Metadata:        metadata,
		})
	}
	return targets
}

func sortDiscoveryTargets(targets []discovery.Target) []discovery.Target {
	sort.SliceStable(targets, func(i, j int) bool {
		return discoveryTargetSortKey(targets[i]) < discoveryTargetSortKey(targets[j])
	})
	return targets
}

func discoveryTargetSortKey(target discovery.Target) string {
	category := ""
	subtype := ""
	parameter := target.Name
	instance := ""
	if target.Metadata != nil {
		category = target.Metadata["category"]
		subtype = target.Metadata["subtype"]
		if target.Metadata["parameter_name"] != "" {
			parameter = target.Metadata["parameter_name"]
		}
		instance = target.Metadata["instance"]
	}
	return fmt.Sprintf("%02d\x00%s\x00%s\x00%s\x00%s\x00%s", signalCategoryRank(category), category, subtype, parameter, target.NodeID, instance)
}

func signalCategoryRank(category string) int {
	switch category {
	case "Thermal", "Temperature":
		return 0
	case "Electrical":
		return 1
	case "Power":
		return 2
	case "State":
		return 3
	case "":
		return 8
	default:
		return 9
	}
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type deviceProfile struct {
	Family     string
	Model      string
	DeviceType int32
	Quality    string
	Reason     string
}

func (p deviceProfile) DeviceTypeString() string {
	if p.DeviceType == 0 {
		return ""
	}
	return strconv.FormatInt(int64(p.DeviceType), 10)
}

func fallbackDeviceProfile(reason string) deviceProfile {
	return deviceProfile{
		Family:  mecomdict.FamilyTEC,
		Model:   "TEC controller",
		Quality: "fallback",
		Reason:  reason,
	}
}

func discoverDeviceProfiles(ctx context.Context, hub mecomserver.HubConfig, cfg Config) map[string]deviceProfile {
	out := map[string]deviceProfile{}
	for _, device := range hub.Devices {
		out[device.ID] = fallbackDeviceProfile("default_tec_profile")
	}
	if !cfg.DiscoverInstances {
		return out
	}
	for _, device := range hub.Devices {
		profile, err := probeDeviceProfile(ctx, device)
		if err != nil {
			log.Printf("utility: controller profile discovery for %s failed, using TEC profile: %v", device.ID, err)
			out[device.ID] = fallbackDeviceProfile("profile_probe_failed")
			continue
		}
		out[device.ID] = profile
	}
	return out
}

func probeDeviceProfile(ctx context.Context, device mecomserver.DeviceConfig) (deviceProfile, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client, closeFn, err := clientForDevice(reqCtx, device)
	if err != nil {
		return deviceProfile{}, err
	}
	defer closeFn()
	if deviceType, err := readDeviceType(reqCtx, client); err == nil {
		if family := familyFromDeviceType(deviceType); family != "" {
			return deviceProfile{
				Family:     family,
				Model:      modelFromFamilyAndType(family, deviceType),
				DeviceType: deviceType,
				Quality:    "live",
				Reason:     "device_type_parameter",
			}, nil
		}
	}
	if parameterResponds(reqCtx, client, 1000, 1, "FLOAT32") {
		return deviceProfile{Family: mecomdict.FamilyTEC, Model: "TEC controller", Quality: "live", Reason: "object_temperature_probe"}, nil
	}
	if parameterResponds(reqCtx, client, 1104, 1, "FLOAT32") {
		return deviceProfile{Family: mecomdict.FamilyLDD1321, Model: "LDD-1321", Quality: "live", Reason: "anode_voltage_probe"}, nil
	}
	if parameterResponds(reqCtx, client, 1100, 1, "FLOAT32") {
		return deviceProfile{Family: mecomdict.FamilyLDD130x, Model: "LDD-130x", Quality: "live", Reason: "ldd_130x_probe"}, nil
	}
	if parameterResponds(reqCtx, client, 1016, 1, "FLOAT32") {
		return deviceProfile{Family: mecomdict.FamilyLDD112x, Model: "LDD-112x", Quality: "live", Reason: "laser_current_probe"}, nil
	}
	return deviceProfile{}, fmt.Errorf("no controller family probe responded")
}

func readDeviceType(ctx context.Context, client mecomScalarReader) (int32, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return client.ReadInt32(reqCtx, 100, 0)
}

func familyFromDeviceType(deviceType int32) string {
	switch {
	case deviceType == 1321:
		return mecomdict.FamilyLDD1321
	case deviceType >= 1300 && deviceType < 1310:
		return mecomdict.FamilyLDD130x
	case deviceType >= 1080 && deviceType < 1200:
		return mecomdict.FamilyTEC
	default:
		return ""
	}
}

func modelFromFamilyAndType(family string, deviceType int32) string {
	if deviceType == 0 {
		switch family {
		case mecomdict.FamilyTEC:
			return "TEC controller"
		case mecomdict.FamilyLDD112x:
			return "LDD-112x"
		case mecomdict.FamilyLDD130x:
			return "LDD-130x"
		case mecomdict.FamilyLDD1321:
			return "LDD-1321"
		default:
			return family
		}
	}
	switch family {
	case mecomdict.FamilyTEC:
		return fmt.Sprintf("TEC-%d", deviceType)
	case mecomdict.FamilyLDD112x, mecomdict.FamilyLDD130x, mecomdict.FamilyLDD1321:
		return fmt.Sprintf("LDD-%d", deviceType)
	default:
		return fmt.Sprintf("MeCom-%d", deviceType)
	}
}

func discoverInstances(ctx context.Context, hub mecomserver.HubConfig, cfg Config) map[string][]int {
	out := map[string][]int{}
	for _, device := range hub.Devices {
		out[device.ID] = discoveryInstances(cfg)
	}
	if !cfg.DiscoverInstances {
		return out
	}
	for _, device := range hub.Devices {
		instances, err := probeDeviceInstances(ctx, device, cfg)
		if err != nil {
			log.Printf("utility: instance discovery for %s failed, using configured instances: %v", device.ID, err)
			continue
		}
		if len(instances) > 0 {
			out[device.ID] = instances
		}
	}
	return out
}

func discoveryInstances(cfg Config) []int {
	if len(cfg.Instances) == 0 {
		return []int{0, 1, 2}
	}
	return append([]int(nil), cfg.Instances...)
}

func probeDeviceInstances(ctx context.Context, device mecomserver.DeviceConfig, cfg Config) ([]int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client, closeFn, err := clientForDevice(reqCtx, device)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	found := map[int]bool{}
	if parameterResponds(reqCtx, client, 104, 0, "INT32") {
		found[0] = true
	}
	maxInstance := cfg.InstanceScanMax
	if maxInstance == 0 {
		maxInstance = 16
	}
	for instance := 1; instance <= maxInstance; instance++ {
		if parameterResponds(reqCtx, client, 1000, instance, "FLOAT32") {
			found[instance] = true
		}
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no MeCom instances responded")
	}
	instances := make([]int, 0, len(found))
	for instance := range found {
		instances = append(instances, instance)
	}
	sort.Ints(instances)
	log.Printf("utility: discovered instances for %s: %v", device.ID, instances)
	return instances, nil
}

func parameterResponds(ctx context.Context, client mecomScalarReader, paramID, instance int, format string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	switch strings.ToUpper(format) {
	case "INT32", "UINT32":
		_, err := client.ReadInt32(reqCtx, paramID, instance)
		return err == nil
	default:
		_, err := client.ReadFloat32(reqCtx, paramID, instance)
		return err == nil
	}
}

func parameterTargets(device mecomserver.DeviceConfig, params []mecomdict.ParameterDef, instances []int, profile deviceProfile) []discovery.Target {
	if len(params) == 0 || len(instances) == 0 {
		return nil
	}
	targets := make([]discovery.Target, 0, len(params)*len(instances))
	for _, param := range params {
		paramID := fmt.Sprintf("%d", param.ID)
		category := mecomdict.CategoryForName(param.Name)
		for _, instance := range instances {
			instanceName := instanceDisplayName(instance)
			targetID := fmt.Sprintf("device:%s:mecom:%d:i%d", device.ID, param.ID, instance)
			direction := parameterDirection(param)
			unit := mecomdict.UnitForName(param.Name)
			format := strings.ToUpper(param.Format)
			kind := mecomdict.KindFor(param)
			graphGroup := graphwall.SemanticAggregate(graphwall.SemanticInput{
				ID:   targetID,
				Name: param.Name,
				Unit: unit,
				Kind: string(kind),
				Metadata: map[string]string{
					"category": category,
				},
			})
			metadata := map[string]string{
				"parameter_id":      paramID,
				"parameter_name":    param.Name,
				"instance":          fmt.Sprintf("%d", instance),
				"instance_name":     instanceName,
				"format":            format,
				"value_type":        format,
				"unit":              unit,
				"description":       mecomdict.DescriptionFor(param),
				"readable":          strconv.FormatBool(direction != discovery.DirectionTC),
				"writable":          strconv.FormatBool(direction != discovery.DirectionTM),
				"read_path":         "/api/target/read?id=" + url.QueryEscape(targetID),
				"category":          category,
				"graph_group":       graphGroup,
				"axis_policy":       graphwall.AxisPolicyForAggregate(graphGroup, unit),
				"resume":            "sequence",
				"loss_policy":       "lossless_within_retention_for_ring_targets",
				"controller_family": profile.Family,
				"controller_model":  profile.Model,
				"controller_type":   profile.DeviceTypeString(),
				"discovery_quality": profile.Quality,
				"discovery_reason":  profile.Reason,
				"parameter_source":  param.SourceList,
			}
			applyTransportReadoutMetadata(metadata, device, mecom.ReadoutVXRoundRobinQueue)
			metadata["readout"] = "configurable_single_or_ring_since_last_read"
			if isDirectCANTarget(device.Target) {
				metadata["readout"] = metadata["active_readout"]
			}
			if direction != discovery.DirectionTM {
				metadata["write_path"] = "/api/target/write"
			}
			addDeviceRedundancyMetadata(metadata, device)
			targets = append(targets, discovery.Target{
				ID:              targetID,
				Name:            instanceName,
				Group:           []string{"Signals", category, param.Name, device.ID},
				Direction:       direction,
				Ownership:       discovery.OwnershipLocalNode,
				Protocol:        string(objectdict.ProtocolMeCom),
				NodeID:          device.ID,
				Transport:       device.Target,
				Address:         fmt.Sprintf("parameter=%d instance=%d", param.ID, instance),
				Unit:            unit,
				Kind:            string(kind),
				Enum:            param.Enum,
				Dictionary:      "mecom-pymecom-parameters",
				DictionaryEntry: "mecom:" + paramID,
				Metadata:        metadata,
			})
		}
	}
	return targets
}

func parameterDirection(param mecomdict.ParameterDef) discovery.Direction {
	lower := strings.ToLower(param.Name)
	readOnlyHints := []string{
		"actual", "device type", "error", "event", "firmware", "hardware", "measured",
		"object temperature", "serial", "sink temperature", "state", "status", "temperature is stable",
		"version", "warning",
	}
	for _, hint := range readOnlyHints {
		if strings.Contains(lower, hint) {
			return discovery.DirectionTM
		}
	}
	writableHints := []string{
		"control", "current limit", "enable", "enabled", "gain", "kd", "ki", "kp",
		"limit", "mode", "offset", "output", "pid", "ramp", "save data to flash",
		"set point", "setpoint", "target", "voltage limit",
	}
	for _, hint := range writableHints {
		if strings.Contains(lower, hint) {
			return discovery.DirectionIO
		}
	}
	return discovery.DirectionTM
}

func targetWritable(target discovery.Target) bool {
	if target.Direction == discovery.DirectionTC || target.Direction == discovery.DirectionIO {
		return target.Metadata == nil || target.Metadata["writable"] == "" || target.Metadata["writable"] == "true"
	}
	return target.Metadata != nil && target.Metadata["writable"] == "true"
}

func writePrivilegedRequest(r *http.Request) bool {
	if cloudflareProxiedRequest(r) {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return writePrivilegedIP(net.ParseIP(strings.TrimSpace(host)))
}

func cloudflareProxiedRequest(r *http.Request) bool {
	if r.Header.Get("CF-Connecting-IP") != "" || r.Header.Get("Cf-Ray") != "" {
		return true
	}
	forwardedProto := strings.ToLower(r.Header.Get("X-Forwarded-Proto"))
	return strings.Contains(forwardedProto, "https") && strings.Contains(strings.ToLower(r.Header.Get("Server")), "cloudflare")
}

func writePrivilegedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	return tailnetIP(ip)
}

func tailnetIP(ip net.IP) bool {
	v4 := ip.To4()
	return v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 0x40
}

func instanceDisplayName(instance int) string {
	switch instance {
	case 0:
		return "Global"
	default:
		return fmt.Sprintf("Instance %d", instance)
	}
}

func graphGroupForTarget(target discovery.Target) string {
	return graphwall.SemanticAggregate(graphwall.SemanticInput{
		ID:       target.ID,
		Name:     target.Name,
		Unit:     target.Unit,
		Kind:     string(target.Kind),
		Metadata: target.Metadata,
	})
}

func aggregateTargetID(group string) string {
	switch group {
	case graphwall.AggregateTemperature:
		return "aggregate:temperatures"
	case graphwall.AggregateTarget:
		return "aggregate:targets"
	case graphwall.AggregatePower:
		return "aggregate:powers"
	case graphwall.AggregateEvents, graphwall.AggregateState:
		return "aggregate:events"
	default:
		return ""
	}
}

func aggregateTargets() []discovery.Target {
	return []discovery.Target{
		{
			ID:        "aggregate:temperatures",
			Name:      "All Temperatures",
			Group:     []string{"Graph Wall", "Aggregates"},
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipDerived,
			Protocol:  string(objectdict.ProtocolMeCom),
			Unit:      "degC",
			Kind:      string(objectdict.ValueKindContinuous),
			Metadata: map[string]string{
				"aggregate":       graphwall.AggregateTemperature,
				"graph_group":     graphwall.AggregateTemperature,
				"axis_policy":     graphwall.AxisTemperatureC,
				"source_filter":   "object_temp_c,sink_temp_c,cascade_temp_c",
				"source_suffixes": "object_temp_c,sink_temp_c,cascade_temp_c",
			},
		},
		{
			ID:        "aggregate:targets",
			Name:      "All Target Values",
			Group:     []string{"Graph Wall", "Aggregates"},
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipDerived,
			Protocol:  string(objectdict.ProtocolMeCom),
			Unit:      "degC",
			Kind:      string(objectdict.ValueKindContinuous),
			Metadata: map[string]string{
				"aggregate":       graphwall.AggregateTarget,
				"graph_group":     graphwall.AggregateTarget,
				"axis_policy":     graphwall.AxisTemperatureC,
				"source_filter":   "target_object_temp_c,ramp_object_temp_c",
				"source_suffixes": "target_object_temp_c,ramp_object_temp_c",
			},
		},
		{
			ID:        "aggregate:powers",
			Name:      "All Output Power",
			Group:     []string{"Graph Wall", "Aggregates"},
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipDerived,
			Protocol:  string(objectdict.ProtocolMeCom),
			Unit:      "W",
			Kind:      string(objectdict.ValueKindContinuous),
			Metadata: map[string]string{
				"aggregate":       graphwall.AggregatePower,
				"graph_group":     graphwall.AggregatePower,
				"axis_policy":     graphwall.AxisPowerW,
				"source_filter":   "output_power_w,electrical_input_w,heat_pumped_from_item_w,resistive_heat_w,hot_side_dissipated_w",
				"source_suffixes": "output_power_w,electrical_input_w,heat_pumped_from_item_w,resistive_heat_w,hot_side_dissipated_w",
			},
		},
		{
			ID:        "aggregate:events",
			Name:      "Event Swimlane",
			Group:     []string{"Graph Wall", "Aggregates"},
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipDerived,
			Protocol:  string(objectdict.ProtocolMeCom),
			Kind:      string(objectdict.ValueKindEnum),
			Metadata: map[string]string{
				"aggregate":   graphwall.AggregateEvents,
				"graph_group": graphwall.AggregateEvents,
				"axis_policy": graphwall.AxisEvent,
			},
		},
	}
}

func defaultGraphWall(_ []discovery.Target) []GraphTileConfig {
	return []GraphTileConfig{
		{
			WallID:   "baseline",
			TileID:   "all-temperatures",
			Kind:     graphwall.TileTrend,
			TargetID: "aggregate:temperatures",
			Position: graphwall.Position{X: 0, Y: 0, W: 12, H: 5},
			Options: map[string]any{
				"aggregate":         graphwall.AggregateTemperature,
				"axis_policy":       graphwall.AxisTemperatureC,
				"controller_layout": "tec_bank_4",
				"focus_signals": defaultTECBankFocusSignals(4,
					"object_temp_c",
					"sink_temp_c",
					"cascade_temp_c",
					"target_object_temp_c",
					"ramp_object_temp_c",
				),
				"reduction_policy": "mean_stddev_window_to_consumer_rate",
				"window_ms":        0,
			},
		},
		{
			WallID:   "baseline",
			TileID:   "all-targets",
			Kind:     graphwall.TileTrend,
			TargetID: "aggregate:targets",
			Position: graphwall.Position{X: 0, Y: 5, W: 12, H: 5},
			Options: map[string]any{
				"aggregate":         graphwall.AggregateTarget,
				"axis_policy":       graphwall.AxisTemperatureC,
				"controller_layout": "tec_bank_4",
				"focus_signals": defaultTECBankFocusSignals(4,
					"target_object_temp_c",
					"ramp_object_temp_c",
				),
				"window_ms": 0,
			},
		},
		{
			WallID:   "baseline",
			TileID:   "all-powers",
			Kind:     graphwall.TileTrend,
			TargetID: "aggregate:powers",
			Position: graphwall.Position{X: 0, Y: 10, W: 12, H: 5},
			Options: map[string]any{
				"aggregate":         graphwall.AggregatePower,
				"axis_policy":       graphwall.AxisPowerW,
				"controller_layout": "tec_bank_4",
				"focus_signals": defaultTECBankFocusSignals(4,
					"output_power_w",
					"electrical_input_w",
					"heat_pumped_from_item_w",
					"resistive_heat_w",
					"hot_side_dissipated_w",
				),
				"reduction_policy": "mean_stddev_window_to_consumer_rate",
				"window_ms":        0,
			},
		},
		{
			WallID:   "baseline",
			TileID:   "event-swimlane",
			Kind:     graphwall.TileLog,
			TargetID: "aggregate:events",
			Position: graphwall.Position{X: 0, Y: 15, W: 12, H: 4},
			Options: map[string]any{
				"aggregate":   graphwall.AggregateEvents,
				"axis_policy": graphwall.AxisEvent,
			},
		},
	}
}

func defaultTECBankFocusSignals(channels int, suffixes ...string) []string {
	signals := make([]string, 0, channels*len(suffixes))
	for channel := 1; channel <= channels; channel++ {
		for _, suffix := range suffixes {
			signals = append(signals, fmt.Sprintf("mecom.tec_%02d.%s", channel, suffix))
		}
	}
	return signals
}

func (s *Server) refreshDiscovery(ctx context.Context) {
	targets := targetsFromHub(ctx, s.hub, s.cfg)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = targets
	byID := map[string]discovery.Target{}
	for _, target := range targets {
		byID[target.ID] = target
	}
	filtered := s.graphWall[:0]
	for _, assignment := range s.graphWall {
		target, ok := byID[assignment.Target.ID]
		if !ok {
			continue
		}
		assignment.Target = target
		filtered = append(filtered, assignment)
	}
	s.graphWall = filtered
}

func resolveGraphWall(config []GraphTileConfig, targets []discovery.Target) ([]graphWallAssignment, error) {
	assignments, err := graphwall.ResolveAssignments(config, targets, func(target discovery.Target) string {
		return target.ID
	})
	if err != nil {
		return nil, fmt.Errorf("utility: %w", err)
	}
	return assignments, nil
}

func maxRingRetention(hub mecomserver.HubConfig) int {
	max := 1
	for _, device := range hub.Devices {
		if device.RingRetention > max {
			max = device.RingRetention
		}
	}
	return max
}
