package utility

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	discovery "github.com/egidinas/loom-gossamer-shared/go/discovery"
	graphwall "github.com/egidinas/loom-gossamer-shared/go/graphwall"
	sharedui "github.com/egidinas/loom-gossamer-shared/go/graphwallui"
	"github.com/egidinas/loom-gossamer-shared/go/tmtc"
	"github.com/egidinas/loom-gossamer-shared/go/tmtclog"
	"github.com/egidinas/loom-gossamer-shared/go/transport"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomdict"
	"github.com/egidinas/meerstetter-go/mecomserver"
	"github.com/egidinas/meerstetter-go/objectdict"
)

type graphWallAssignment = graphwall.Assignment[discovery.Target]

type Server struct {
	cfg       Config
	hub       mecomserver.HubConfig
	recorder  *tmtclog.Recorder
	targets   []discovery.Target
	graphWall []graphWallAssignment

	mu      sync.Mutex
	started bool
	stopFns []func()
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
	return &Server{
		cfg:       cfg,
		hub:       hub,
		recorder:  tmtclog.NewRecorder(tmtclog.New(maxRingRetention(hub)), nil),
		targets:   targets,
		graphWall: graphWall,
	}, nil
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
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/devices", s.handleDevices)
	mux.HandleFunc("/api/discovery/tree", s.handleDiscoveryTree)
	mux.HandleFunc("/api/graph-wall", s.handleGraphWall)
	mux.HandleFunc("/api/graph-wall/assign", s.handleGraphWallAssign)
	mux.HandleFunc("/api/tiles", s.handleTiles)
	mux.HandleFunc("/api/log/ring", s.handleRing)
	mux.HandleFunc("/api/events/swimlane", s.handleEvents)
	mux.HandleFunc("/api/target/read", s.handleTargetRead)
	mux.HandleFunc("/api/target/write", s.handleTargetWrite)
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
	client, closeFn, err := brokerClientForDevice(reqCtx, device)
	if err != nil {
		s.publishPollError(device, err.Error())
		return
	}
	defer closeFn()
	s.readChannelBaseline(reqCtx, client, device, 1, "hr", "HR")
	s.readChannelBaseline(reqCtx, client, device, 2, "lr", "LR")
}

func (s *Server) readChannelBaseline(ctx context.Context, client *mecom.Client, device mecomserver.DeviceConfig, instance int, suffix, label string) {
	objectTemp, err := client.ReadFloat32(ctx, 1000, instance)
	s.publishFloat(device, "temperature:"+suffix, label+" Temperature", "degC", objectTemp, err)
	current, currentErr := client.ReadFloat32(ctx, 1020, instance)
	voltage, voltageErr := client.ReadFloat32(ctx, 1021, instance)
	powerErr := currentErr
	if powerErr == nil {
		powerErr = voltageErr
	}
	s.publishFloat(device, "power:"+suffix, label+" Output Power", "W", current*voltage, powerErr)
	target, err := client.ReadFloat32(ctx, 3000, instance)
	s.publishFloat(device, "target:"+suffix, label+" Target Value", "degC", target, err)
}

func (s *Server) publishFloat(device mecomserver.DeviceConfig, suffix, name, unit string, value float64, err error) {
	quality := "ok"
	var jsonValue any = value
	metadata := map[string]string{
		"transport": device.Target,
		"protocol":  "mecom",
		"readout":   string(s.cfg.ReadPolicy.ModeFor("device:" + device.ID + ":" + suffix)),
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		jsonValue = nil
		metadata["raw_float"] = fmt.Sprintf("%g", value)
		metadata["value_state"] = "not_applicable"
	}
	tm := tmtc.Telemetry{
		ID:       device.ID + ":" + suffix,
		TargetID: "device:" + device.ID + ":" + suffix,
		Time:     time.Now().UTC(),
		Name:     name,
		Value:    jsonValue,
		Unit:     unit,
		Quality:  quality,
		Metadata: metadata,
	}
	if err != nil {
		tm.Quality = "read_error"
		tm.Metadata["error"] = err.Error()
	}
	if pubErr := s.recorder.PublishTelemetry(tm); pubErr != nil {
		log.Printf("utility: publish telemetry %s: %v", tm.TargetID, pubErr)
	}
}

func (s *Server) publishPollError(device mecomserver.DeviceConfig, detail string) {
	if err := s.recorder.PublishTelemetry(tmtc.Telemetry{
		ID:       device.ID + ":status",
		TargetID: "device:" + device.ID + ":status",
		Time:     time.Now().UTC(),
		Name:     "Status",
		Value:    "offline",
		Quality:  "read_error",
		Metadata: map[string]string{
			"transport": device.Target,
			"error":     detail,
		},
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
	})
}

func (s *Server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.hub.Devices)
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
	afterSeq := parseUintQuery(r, "after_seq")
	entries := s.recorder.ReplaySince(afterSeq)
	limit := parseIntQuery(r, "limit")
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	writeJSON(w, entries)
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
	event, err := s.writeTarget(r.Context(), target, req.Value)
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
	client, closeFn, err := brokerClientForDevice(reqCtx, device)
	if err != nil {
		return tmtc.Telemetry{}, err
	}
	defer closeFn()
	value, err := readParameter(reqCtx, client, target, paramID, instance)
	tm := tmtc.Telemetry{
		ID:       strings.TrimPrefix(target.ID, "device:"),
		TargetID: target.ID,
		Time:     time.Now().UTC(),
		Name:     target.Name,
		Value:    value,
		Unit:     target.Unit,
		Quality:  "ok",
		Metadata: map[string]string{
			"transport": device.Target,
			"protocol":  "mecom",
			"readout":   readoutForTarget(s.cfg, target.ID),
		},
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

func (s *Server) writeTarget(ctx context.Context, target discovery.Target, value any) (tmtc.CommandEvent, error) {
	event := tmtc.CommandEvent{
		ID:        fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		CommandID: target.ID,
		Time:      time.Now().UTC(),
		Status:    tmtc.CommandAcked,
		Transport: target.Transport,
		Metadata: map[string]string{
			"target_id": target.ID,
		},
	}
	if !targetWritable(target) {
		return event, fmt.Errorf("target %s is read-only", target.ID)
	}
	device, ok := s.deviceByID(target.NodeID)
	if !ok {
		return event, fmt.Errorf("device %q not found", target.NodeID)
	}
	paramID, instance, err := targetParameter(target)
	if err != nil {
		return event, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, device.Queue.RequestTimeout)
	defer cancel()
	client, closeFn, err := brokerClientForDevice(reqCtx, device)
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

func clientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (*mecom.Client, func(), error) {
	return clientForTarget(ctx, device, device.Target)
}

func brokerClientForDevice(ctx context.Context, device mecomserver.DeviceConfig) (*mecom.Client, func(), error) {
	target := device.Target
	if passthrough := localPassthroughTarget(device.PassthroughListen); passthrough != "" {
		target = passthrough
	}
	return clientForTarget(ctx, device, target)
}

func clientForTarget(ctx context.Context, device mecomserver.DeviceConfig, target string) (*mecom.Client, func(), error) {
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

func targetParameter(target discovery.Target) (int, int, error) {
	paramID, err := strconv.Atoi(target.Metadata["parameter_id"])
	if err != nil || paramID <= 0 {
		return 0, 0, fmt.Errorf("target %s is not a MeCom parameter", target.ID)
	}
	instance, err := strconv.Atoi(target.Metadata["instance"])
	if err != nil {
		return 0, 0, fmt.Errorf("target %s has invalid instance", target.ID)
	}
	return paramID, instance, nil
}

func readParameter(ctx context.Context, client *mecom.Client, target discovery.Target, paramID, instance int) (any, error) {
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
		targets = append(targets, baselineTargets(device, group)...)
		targets = append(targets, parameterTargets(device, params, instances, profile)...)
		targets = append(targets, discovery.Target{
			ID:        "device:" + device.ID + ":status",
			Name:      "Status",
			Group:     group,
			Direction: discovery.DirectionTM,
			Ownership: discovery.OwnershipLocalNode,
			Protocol:  string(objectdict.ProtocolMeCom),
			NodeID:    device.ID,
			Transport: device.Target,
			Metadata: map[string]string{
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
			},
		})
		targets = append(targets, discovery.Target{
			ID:        "device:" + device.ID + ":passthrough",
			Name:      "Original Software Passthrough",
			Group:     group,
			Direction: discovery.DirectionIO,
			Ownership: discovery.OwnershipShared,
			Protocol:  string(objectdict.ProtocolMeCom),
			NodeID:    device.ID,
			Transport: device.PassthroughListen,
			Metadata: map[string]string{
				"downstream": device.Target,
				"tc_demux":   "serialized_downstream",
			},
		})
	}
	return targets
}

func discoveryParametersForFamily(path, family string) []mecomdict.ParameterDef {
	params, err := mecomdict.LoadParameterRegistryForFamily(path, family)
	if err != nil {
		log.Printf("utility: MeCom parameter registry unavailable at %s: %v", path, err)
		return nil
	}
	return params
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

func readDeviceType(ctx context.Context, client *mecom.Client) (int32, error) {
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

func parameterResponds(ctx context.Context, client *mecom.Client, paramID, instance int, format string) bool {
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
				Metadata: map[string]string{
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
					"readout":           "configurable_single_or_ring_since_last_read",
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
				},
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
	case 1:
		return "Instance 1 HR"
	case 2:
		return "Instance 2 LR"
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
				"aggregate":     graphwall.AggregateTemperature,
				"graph_group":   graphwall.AggregateTemperature,
				"axis_policy":   graphwall.AxisTemperatureC,
				"source_filter": ":temperature:",
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
				"aggregate":     graphwall.AggregateTarget,
				"graph_group":   graphwall.AggregateTarget,
				"axis_policy":   graphwall.AxisTemperatureC,
				"source_filter": ":target:",
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
				"aggregate":     graphwall.AggregatePower,
				"graph_group":   graphwall.AggregatePower,
				"axis_policy":   graphwall.AxisPowerW,
				"source_filter": ":power:",
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

func baselineTargets(device mecomserver.DeviceConfig, group []string) []discovery.Target {
	return []discovery.Target{
		baselineTarget(device, group, "temperature:hr", "HR Temperature", "degC", objectdict.ValueKindContinuous, "ring_since_last_read"),
		baselineTarget(device, group, "temperature:lr", "LR Temperature", "degC", objectdict.ValueKindContinuous, "ring_since_last_read"),
		baselineTarget(device, group, "power:hr", "HR Output Power", "W", objectdict.ValueKindContinuous, "ring_since_last_read"),
		baselineTarget(device, group, "power:lr", "LR Output Power", "W", objectdict.ValueKindContinuous, "ring_since_last_read"),
		baselineTarget(device, group, "target:hr", "HR Target Value", "degC", objectdict.ValueKindContinuous, "single"),
		baselineTarget(device, group, "target:lr", "LR Target Value", "degC", objectdict.ValueKindContinuous, "single"),
	}
}

func baselineTarget(device mecomserver.DeviceConfig, group []string, suffix, name, unit string, kind objectdict.ValueKind, readout string) discovery.Target {
	graphGroup := graphwall.SemanticAggregate(graphwall.SemanticInput{
		ID:   "device:" + device.ID + ":" + suffix,
		Name: name,
		Unit: unit,
		Kind: string(kind),
	})
	return discovery.Target{
		ID:        "device:" + device.ID + ":" + suffix,
		Name:      name,
		Group:     append(append([]string(nil), group...), "Baseline"),
		Direction: discovery.DirectionTM,
		Ownership: discovery.OwnershipLocalNode,
		Protocol:  string(objectdict.ProtocolMeCom),
		NodeID:    device.ID,
		Transport: device.Target,
		Unit:      unit,
		Kind:      string(kind),
		Metadata: map[string]string{
			"baseline":    "true",
			"readout":     readout,
			"graph_group": graphGroup,
			"axis_policy": graphwall.AxisPolicyForAggregate(graphGroup, unit),
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
