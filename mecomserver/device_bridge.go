package mecomserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// DeviceClientOpener opens one typed MeCom device client for a downstream
// bridge. It is intentionally transport-opaque: serial/TCP can still use raw
// streams, while CAN routes can translate ASCII requests into typed calls.
type DeviceClientOpener func(context.Context) (mecom.DeviceClient, error)

// DialEndpointTarget returns a downstream dialer for any supported endpoint.
// Serial/TCP targets use the existing raw stream dialer. CAN targets are
// bridged through mecom.DeviceClient so the TCP-facing server can still speak
// normal ASCII MeCom frames to upstream tools.
func DialEndpointTarget(target string, cfg mecom.ClientConfig, dialCAN mecom.CANDialer) (DownstreamDial, error) {
	ep, ok := mecom.ParseTarget(target)
	if !ok {
		return nil, fmt.Errorf("mecomserver: invalid target %q", target)
	}
	if ep.Network != "can" {
		return DialTarget(target)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultRequestTimeout
	}
	return DialDeviceClient(ep.String(), func(ctx context.Context) (mecom.DeviceClient, error) {
		return mecom.NewForEndpoint(ctx, ep, cfg, dialCAN)
	}, cfg.Timeout), nil
}

// DialDeviceClient adapts a typed device client into the net.Conn-shaped
// DownstreamDial expected by the existing broker. The returned net.Conn is a
// private pipe; the goroutine on the other side translates one ASCII MeCom
// frame at a time and replies with an ASCII MeCom response.
func DialDeviceClient(description string, opener DeviceClientOpener, timeout time.Duration) DownstreamDial {
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return func(ctx context.Context) (net.Conn, string, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		client, err := opener(ctx)
		if err != nil {
			return nil, "", err
		}
		upstream, downstream := net.Pipe()
		go runDeviceClientBridge(downstream, client, timeout, newDeviceBridgeState(description))
		return upstream, description, nil
	}
}

func runDeviceClientBridge(conn net.Conn, client mecom.DeviceClient, timeout time.Duration, state *deviceBridgeState) {
	defer conn.Close()
	defer client.Close()

	reader := bufio.NewReader(conn)
	for {
		frame, err := readBoundedFramePartial(reader, maxClientFrameBytes)
		if err != nil {
			return
		}
		resp := handleDeviceClientFrameWithState(state, client, frame, timeout)
		if len(resp) == 0 {
			resp = deviceServerError(frame, fmt.Errorf("mecomserver: empty device bridge response"))
		}
		if timeout > 0 {
			_ = conn.SetWriteDeadline(time.Now().Add(timeout))
		}
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

type deviceBridgeVirtualOnlyClient struct{}

type deviceBridgeRawSDOReader interface {
	ReadSDORaw(context.Context, uint16, byte) ([]byte, error)
}

func (deviceBridgeVirtualOnlyClient) ReadFloat32(context.Context, int, int) (float64, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) ReadInt32(context.Context, int, int) (int32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) ReadBulk(context.Context, []mecom.Parameter) ([]float64, error) {
	return nil, mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) TriggerRingSync(context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (deviceBridgeVirtualOnlyClient) Close() error {
	return nil
}

type deviceBridgeRequest struct {
	raw     []byte
	address byte
	seq     uint16
	payload string
}

const (
	deviceBridgeFirmwareIdentification = "8065-TEC SW G01     "
	deviceBridgeBoardIdentification    = "00000000"

	deviceBridgeMetadataReadFlag   = 0x01
	deviceBridgeMetadataWriteFlag  = 0x02
	deviceBridgeBigDataMaxElements = 0x100

	deviceBridgeTransformSynthesizeFloat32FromInt32 = "synthesize_float32_from_int32"
	deviceBridgeTransformMaskInt32                  = "mask_int32"
	deviceBridgeTransformConstantInt32              = "constant_int32"
	deviceBridgeTransformVirtualParameter           = "virtual_parameter"
	deviceBridgeTransformCANopenPDOConfigBytes      = "canopen_pdo_config_bytes"
	deviceBridgeUnsupportedParameterType            = mecom.DataType("unsupported")

	deviceBridgeCacheSourceLocal       = "local"
	deviceBridgeCacheSourceDownstream  = "downstream"
	deviceBridgeCacheSourcePlaceholder = "placeholder"
)

type deviceBridgeVirtualParameterKey struct {
	id       int
	instance int
}

type deviceBridgeVirtualParameterValue struct {
	typ         mecom.DataType
	float       float64
	int         int32
	latin1      string
	source      string
	liveRefresh bool
	updatedAt   time.Time
}

type deviceBridgeVirtualParameterStore struct {
	mu     sync.Mutex
	values map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue
}

type deviceBridgeState struct {
	device            string
	cachePath         string
	virtualParameters *deviceBridgeVirtualParameterStore
	softParameters    *deviceBridgeVirtualParameterStore
}

type deviceBridgeCacheSnapshot struct {
	Device     string                               `json:"device"`
	Parameters []deviceBridgeCacheParameterSnapshot `json:"parameters"`
}

type deviceBridgeCacheParameterSnapshot struct {
	ID          int      `json:"id"`
	Instance    int      `json:"instance"`
	Type        string   `json:"type"`
	Source      string   `json:"source,omitempty"`
	LiveRefresh bool     `json:"live_refresh"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Int32       *int32   `json:"int32,omitempty"`
	Float32     *float64 `json:"float32,omitempty"`
	Latin1      string   `json:"latin1,omitempty"`
}

var deviceBridgeVirtualParameters = &deviceBridgeVirtualParameterStore{
	values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
}

var deviceBridgeSoftParameters = &deviceBridgeVirtualParameterStore{
	values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
}

var deviceBridgeCacheDirState = struct {
	sync.RWMutex
	dir string
}{}

func SetDeviceBridgeCacheDir(dir string) func() {
	dir = strings.TrimSpace(dir)
	deviceBridgeCacheDirState.Lock()
	previous := deviceBridgeCacheDirState.dir
	deviceBridgeCacheDirState.dir = dir
	deviceBridgeCacheDirState.Unlock()
	return func() {
		deviceBridgeCacheDirState.Lock()
		deviceBridgeCacheDirState.dir = previous
		deviceBridgeCacheDirState.Unlock()
	}
}

func deviceBridgeCurrentCacheDir() string {
	deviceBridgeCacheDirState.RLock()
	defer deviceBridgeCacheDirState.RUnlock()
	return deviceBridgeCacheDirState.dir
}

func deviceBridgeCachePath(device string) string {
	dir := deviceBridgeCurrentCacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, deviceBridgeCacheFileName(device))
}

func deviceBridgeCacheFileName(device string) string {
	device = strings.TrimSpace(device)
	if device == "" {
		device = "unknown"
	}
	var out strings.Builder
	lastUnderscore := false
	for _, r := range device {
		ok := (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '-' || r == '_'
		if ok {
			out.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			out.WriteByte('_')
			lastUnderscore = true
		}
	}
	name := strings.Trim(out.String(), "_")
	if name == "" {
		name = "device"
	}
	return name + ".json"
}

func newDeviceBridgeState(device string) *deviceBridgeState {
	device = strings.TrimSpace(device)
	if device == "" {
		device = "unknown"
	}
	state := &deviceBridgeState{
		device:    device,
		cachePath: deviceBridgeCachePath(device),
		virtualParameters: &deviceBridgeVirtualParameterStore{
			values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
		},
		softParameters: &deviceBridgeVirtualParameterStore{
			values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
		},
	}
	state.loadCacheBestEffort()
	return state
}

func defaultDeviceBridgeState() *deviceBridgeState {
	return &deviceBridgeState{
		device:            "default",
		virtualParameters: deviceBridgeVirtualParameters,
		softParameters:    deviceBridgeSoftParameters,
	}
}

func (s *deviceBridgeState) virtualParameterStore() *deviceBridgeVirtualParameterStore {
	if s == nil {
		return deviceBridgeVirtualParameters
	}
	if s.virtualParameters == nil {
		s.virtualParameters = &deviceBridgeVirtualParameterStore{
			values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
		}
	}
	return s.virtualParameters
}

func (s *deviceBridgeState) softParameterStore() *deviceBridgeVirtualParameterStore {
	if s == nil {
		return deviceBridgeSoftParameters
	}
	if s.softParameters == nil {
		s.softParameters = &deviceBridgeVirtualParameterStore{
			values: map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{},
		}
	}
	return s.softParameters
}

func (s *deviceBridgeState) snapshot() deviceBridgeCacheSnapshot {
	if s == nil {
		s = defaultDeviceBridgeState()
	}
	store := s.softParameterStore()

	store.mu.Lock()
	entries := make([]struct {
		key   deviceBridgeVirtualParameterKey
		value deviceBridgeVirtualParameterValue
	}, 0, len(store.values))
	for key, value := range store.values {
		entries = append(entries, struct {
			key   deviceBridgeVirtualParameterKey
			value deviceBridgeVirtualParameterValue
		}{key: key, value: value})
	}
	store.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key.id != entries[j].key.id {
			return entries[i].key.id < entries[j].key.id
		}
		return entries[i].key.instance < entries[j].key.instance
	})

	out := deviceBridgeCacheSnapshot{
		Device:     s.device,
		Parameters: make([]deviceBridgeCacheParameterSnapshot, 0, len(entries)),
	}
	for _, entry := range entries {
		value := entry.value
		param := deviceBridgeCacheParameterSnapshot{
			ID:          entry.key.id,
			Instance:    entry.key.instance,
			Type:        string(value.typ),
			Source:      value.source,
			LiveRefresh: value.liveRefresh,
		}
		if !value.updatedAt.IsZero() {
			param.UpdatedAt = value.updatedAt.UTC().Format(time.RFC3339Nano)
		}
		switch value.typ {
		case mecom.DataTypeInt32:
			v := value.int
			param.Int32 = &v
		case mecom.DataTypeFloat32:
			if math.IsNaN(value.float) || math.IsInf(value.float, 0) {
				continue
			}
			v := value.float
			param.Float32 = &v
		case mecom.DataTypeLatin1:
			param.Latin1 = value.latin1
		}
		out.Parameters = append(out.Parameters, param)
	}
	return out
}

func (s *deviceBridgeState) cacheJSON() ([]byte, error) {
	return json.MarshalIndent(s.snapshot(), "", "  ")
}

func (s *deviceBridgeState) loadCacheBestEffort() {
	if s == nil || s.cachePath == "" {
		return
	}
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		return
	}
	var snap deviceBridgeCacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return
	}
	store := s.softParameterStore()
	for _, param := range snap.Parameters {
		if param.Source == deviceBridgeCacheSourcePlaceholder {
			continue
		}
		value := deviceBridgeVirtualParameterValue{
			typ:         mecom.DataType(param.Type),
			source:      param.Source,
			liveRefresh: param.LiveRefresh,
		}
		if value.source == "" {
			value.source = deviceBridgeCacheSourceDownstream
		}
		if param.UpdatedAt != "" {
			if updatedAt, err := time.Parse(time.RFC3339Nano, param.UpdatedAt); err == nil {
				value.updatedAt = updatedAt
			}
		}
		switch value.typ {
		case mecom.DataTypeInt32:
			if param.Int32 == nil {
				continue
			}
			value.int = *param.Int32
		case mecom.DataTypeFloat32:
			if param.Float32 == nil {
				continue
			}
			if math.IsNaN(*param.Float32) || math.IsInf(*param.Float32, 0) {
				continue
			}
			value.float = *param.Float32
		case mecom.DataTypeLatin1:
			value.latin1 = param.Latin1
		default:
			continue
		}
		_ = store.storeParameterValue(param.ID, param.Instance, value)
	}
}

func (s *deviceBridgeState) flushCacheBestEffort() {
	if s == nil || s.cachePath == "" {
		return
	}
	data, err := s.cacheJSON()
	if err != nil {
		return
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(s.cachePath), 0o755); err != nil {
		return
	}
	tmpPath := s.cachePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, s.cachePath)
}

func (s *deviceBridgeState) storeParameterValue(id, instance int, value deviceBridgeVirtualParameterValue) error {
	if s == nil {
		s = defaultDeviceBridgeState()
	}
	store := s.softParameterStore()
	if value.source != deviceBridgeCacheSourcePlaceholder && value.updatedAt.IsZero() {
		value.updatedAt = time.Now().UTC()
	}
	if err := store.storeParameterValue(id, instance, value); err != nil {
		return err
	}
	if value.source != deviceBridgeCacheSourcePlaceholder {
		s.flushCacheBestEffort()
	}
	return nil
}

func (s *deviceBridgeState) writeParameter(id, instance int, typ mecom.DataType, valueHex string) error {
	if s == nil {
		s = defaultDeviceBridgeState()
	}
	store := s.softParameterStore()
	if err := store.writeParameter(id, instance, typ, valueHex); err != nil {
		return err
	}
	s.flushCacheBestEffort()
	return nil
}

func (s *deviceBridgeVirtualParameterStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values = map[deviceBridgeVirtualParameterKey]deviceBridgeVirtualParameterValue{}
}

func (s *deviceBridgeVirtualParameterStore) readNumeric(transform mecom.BridgeTransform, instance int) (float64, error) {
	if err := deviceBridgeValidateTransformInstance(transform, instance); err != nil {
		return 0, err
	}
	switch transform.Type {
	case mecom.DataTypeFloat32, mecom.DataTypeInt32:
	default:
		return 0, fmt.Errorf("%w: virtual single read of %s parameter %d", mecom.ErrTransportNotSupported, transform.Type, transform.MeComID)
	}

	key := deviceBridgeVirtualParameterKey{id: transform.MeComID, instance: instance}
	s.mu.Lock()
	value, ok := s.values[key]
	s.mu.Unlock()
	if !ok {
		return 0, nil
	}
	if value.typ != "" && value.typ != transform.Type {
		return 0, fmt.Errorf("%w: virtual parameter %d instance %d stored as %s, read as %s", mecom.ErrInvalidArgument, transform.MeComID, instance, value.typ, transform.Type)
	}
	if transform.Type == mecom.DataTypeInt32 {
		return float64(value.int), nil
	}
	return value.float, nil
}

func (s *deviceBridgeVirtualParameterStore) write(transform mecom.BridgeTransform, instance int, valueHex string) error {
	if err := deviceBridgeValidateTransformInstance(transform, instance); err != nil {
		return err
	}
	if !transform.Writable {
		return fmt.Errorf("%w: virtual parameter %d instance %d", mecom.ErrParameterReadOnly, transform.MeComID, instance)
	}

	value := deviceBridgeVirtualParameterValue{typ: transform.Type}
	switch transform.Type {
	case mecom.DataTypeFloat32, "":
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: float32 value for virtual parameter %d has %d hex chars", mecom.ErrInvalidArgument, transform.MeComID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid float32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		value.typ = mecom.DataTypeFloat32
		value.float = float64(math.Float32frombits(uint32(bits)))
	case mecom.DataTypeInt32:
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: int32 value for virtual parameter %d has %d hex chars", mecom.ErrInvalidArgument, transform.MeComID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid int32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		value.int = int32(uint32(bits))
	case mecom.DataTypeLatin1:
		data, err := hex.DecodeString(valueHex)
		if err != nil {
			return fmt.Errorf("%w: invalid LATIN1 value for virtual parameter %d: %v", mecom.ErrInvalidArgument, transform.MeComID, err)
		}
		value.latin1 = string(bytes.TrimRight(data, "\x00"))
	default:
		return fmt.Errorf("%w: write of virtual %s parameter %d", mecom.ErrTransportNotSupported, transform.Type, transform.MeComID)
	}

	key := deviceBridgeVirtualParameterKey{id: transform.MeComID, instance: instance}
	value.source = deviceBridgeCacheSourceLocal
	value.updatedAt = time.Now().UTC()
	s.mu.Lock()
	s.values[key] = value
	s.mu.Unlock()
	return nil
}

func (s *deviceBridgeVirtualParameterStore) lookupParameter(id, instance int, typ mecom.DataType) (deviceBridgeVirtualParameterValue, bool, error) {
	if err := deviceBridgeValidateParameterInstance(id, instance); err != nil {
		return deviceBridgeVirtualParameterValue{}, false, err
	}
	switch typ {
	case mecom.DataTypeFloat32, mecom.DataTypeInt32, mecom.DataTypeLatin1:
	default:
		return deviceBridgeVirtualParameterValue{}, false, fmt.Errorf("%w: soft read of %s parameter %d", mecom.ErrTransportNotSupported, typ, id)
	}

	key := deviceBridgeVirtualParameterKey{id: id, instance: instance}
	s.mu.Lock()
	value, ok := s.values[key]
	s.mu.Unlock()
	if !ok {
		return deviceBridgeVirtualParameterValue{}, false, nil
	}
	if value.typ != "" && value.typ != typ {
		return deviceBridgeVirtualParameterValue{}, false, fmt.Errorf("%w: soft parameter %d instance %d stored as %s, read as %s", mecom.ErrInvalidArgument, id, instance, value.typ, typ)
	}
	return value, true, nil
}

func (s *deviceBridgeVirtualParameterStore) storeParameterValue(id, instance int, value deviceBridgeVirtualParameterValue) error {
	if err := deviceBridgeValidateParameterInstance(id, instance); err != nil {
		return err
	}
	switch value.typ {
	case mecom.DataTypeFloat32, mecom.DataTypeInt32, mecom.DataTypeLatin1:
	default:
		return fmt.Errorf("%w: soft write of %s parameter %d", mecom.ErrTransportNotSupported, value.typ, id)
	}

	key := deviceBridgeVirtualParameterKey{id: id, instance: instance}
	s.mu.Lock()
	s.values[key] = value
	s.mu.Unlock()
	return nil
}

func (s *deviceBridgeVirtualParameterStore) readParameter(id, instance int, typ mecom.DataType) (deviceBridgeVirtualParameterValue, error) {
	value, ok, err := s.lookupParameter(id, instance, typ)
	if err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	}
	if !ok {
		return deviceBridgeVirtualParameterValue{typ: typ, source: deviceBridgeCacheSourcePlaceholder}, nil
	}
	return value, nil
}

func (s *deviceBridgeVirtualParameterStore) writeParameter(id, instance int, typ mecom.DataType, valueHex string) error {
	if err := deviceBridgeValidateParameterInstance(id, instance); err != nil {
		return err
	}

	value := deviceBridgeVirtualParameterValue{typ: typ}
	switch typ {
	case mecom.DataTypeFloat32:
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: float32 value for soft parameter %d has %d hex chars", mecom.ErrInvalidArgument, id, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid float32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		value.float = float64(math.Float32frombits(uint32(bits)))
	case mecom.DataTypeInt32:
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: int32 value for soft parameter %d has %d hex chars", mecom.ErrInvalidArgument, id, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid int32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		value.int = int32(uint32(bits))
	case mecom.DataTypeLatin1:
		data, err := hex.DecodeString(valueHex)
		if err != nil {
			return fmt.Errorf("%w: invalid LATIN1 value for soft parameter %d: %v", mecom.ErrInvalidArgument, id, err)
		}
		value.latin1 = string(bytes.TrimRight(data, "\x00"))
	default:
		return fmt.Errorf("%w: write of soft %s parameter %d", mecom.ErrTransportNotSupported, typ, id)
	}

	key := deviceBridgeVirtualParameterKey{id: id, instance: instance}
	value.source = deviceBridgeCacheSourceLocal
	value.updatedAt = time.Now().UTC()
	s.mu.Lock()
	s.values[key] = value
	s.mu.Unlock()
	return nil
}

func deviceBridgeValidateTransformInstance(transform mecom.BridgeTransform, instance int) error {
	minInstance := transform.MinInstance
	if minInstance <= 0 {
		minInstance = 1
	}
	maxInstance := transform.MaxInstance
	if maxInstance <= 0 {
		maxInstance = minInstance
	}
	if instance < minInstance || instance > maxInstance {
		return fmt.Errorf("%w: virtual parameter %d instance %d outside %d..%d", mecom.ErrInvalidArgument, transform.MeComID, instance, minInstance, maxInstance)
	}
	return nil
}

func deviceBridgeValidateParameterInstance(id, instance int) error {
	maxInstance := int(deviceBridgeParameterMaxInstances(id))
	if maxInstance <= 0 {
		maxInstance = 1
	}
	if instance < 1 || instance > maxInstance {
		return fmt.Errorf("%w: parameter %d instance %d outside 1..%d", mecom.ErrInvalidArgument, id, instance, maxInstance)
	}
	return nil
}

func handleDeviceClientFrame(client mecom.DeviceClient, raw []byte, timeout time.Duration) []byte {
	return handleDeviceClientFrameWithState(defaultDeviceBridgeState(), client, raw, timeout)
}

func handleDeviceClientFrameWithState(state *deviceBridgeState, client mecom.DeviceClient, raw []byte, timeout time.Duration) []byte {
	req, err := parseDeviceBridgeRequest(raw)
	if err != nil {
		return deviceServerError(raw, err)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	payload, err := deviceBridgePayloadWithState(ctx, state, client, req.payload)
	if err != nil {
		return deviceBridgeNACK(req.address, req.seq, nackCodeForError(err))
	}
	if isDeviceBridgeInfoProbe(req.payload) {
		return deviceBridgeInfo(req.address, req.seq, payload)
	}
	if isDeviceBridgeRead(req.payload) {
		return deviceBridgeData(req.address, req.seq, payload)
	}
	return deviceBridgeOK(req.address, req.seq, payload)
}

func handleDeviceBridgeRouterLocalFrame(state *deviceBridgeState, raw []byte, timeout time.Duration) ([]byte, bool) {
	req, err := parseDeviceBridgeRequest(raw)
	if err != nil {
		return nil, false
	}
	if !deviceBridgePayloadUsesRouterLocalVirtualWrite(req.payload) {
		return nil, false
	}
	return handleDeviceClientFrameWithState(state, deviceBridgeVirtualOnlyClient{}, raw, timeout), true
}

func deviceBridgePayloadUsesRouterLocalVirtualWrite(payload string) bool {
	switch {
	case strings.HasPrefix(payload, "VB"):
		if len(payload) < len("VB00000000000000000000") {
			return false
		}
		return deviceBridgeEncodedParameterIsVirtual(payload[2:8])
	case strings.HasPrefix(payload, "VS"):
		if len(payload) < len("VS000000") {
			return false
		}
		return deviceBridgeEncodedParameterIsVirtual(payload[2:8])
	default:
		return false
	}
}

func deviceBridgeEncodedParameterIsVirtual(encoded string) bool {
	paramID, _, err := parseDeviceBridgeParameter(encoded)
	if err != nil {
		return false
	}
	_, ok := deviceBridgeTransform(paramID, deviceBridgeTransformVirtualParameter)
	return ok
}

func parseDeviceBridgeRequest(raw []byte) (deviceBridgeRequest, error) {
	frame := bytes.TrimSuffix(bytes.TrimSpace(raw), []byte{mecom.FrameTerminator})
	if len(frame) >= 2 && frame[0] == '?' {
		return deviceBridgeRequest{
			raw:     append([]byte(nil), raw...),
			address: 0,
			seq:     0,
			payload: strings.ToUpper(string(frame)),
		}, nil
	}
	if len(frame) < 11 || frame[0] != '#' {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request frame %q", string(frame))
	}
	if err := verifyDeviceBridgeCRC(frame); err != nil {
		return deviceBridgeRequest{}, err
	}
	addr, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request address %q: %w", string(frame[1:3]), err)
	}
	seq, err := strconv.ParseUint(string(frame[3:7]), 16, 16)
	if err != nil {
		return deviceBridgeRequest{}, fmt.Errorf("mecomserver: invalid request sequence %q: %w", string(frame[3:7]), err)
	}
	return deviceBridgeRequest{
		raw:     append([]byte(nil), raw...),
		address: byte(addr),
		seq:     uint16(seq),
		payload: strings.ToUpper(string(frame[7 : len(frame)-4])),
	}, nil
}

func verifyDeviceBridgeCRC(frame []byte) error {
	payloadEnd := len(frame) - 4
	if payloadEnd <= 0 {
		return fmt.Errorf("mecomserver: frame too short for CRC %q", string(frame))
	}
	got, err := strconv.ParseUint(string(frame[payloadEnd:]), 16, 16)
	if err != nil {
		return fmt.Errorf("mecomserver: invalid request CRC %q: %w", string(frame[payloadEnd:]), err)
	}
	want := mecom.CRC16(frame[:payloadEnd])
	if uint16(got) != want {
		return fmt.Errorf("mecomserver: CRC mismatch got %04X want %04X", got, want)
	}
	return nil
}

func deviceBridgeObserveFrame(state *deviceBridgeState, requestFrame, responseFrame []byte) {
	if state == nil {
		return
	}
	req, err := parseDeviceBridgeRequest(requestFrame)
	if err != nil {
		return
	}
	responsePayload, ok := parseDeviceBridgeResponsePayload(req, responseFrame)
	if !ok {
		return
	}
	deviceBridgeObservePayload(state, req.payload, responsePayload)
}

func parseDeviceBridgeResponsePayload(req deviceBridgeRequest, raw []byte) (string, bool) {
	frame := bytes.TrimSuffix(bytes.TrimSpace(raw), []byte{mecom.FrameTerminator})
	if len(frame) < 11 || frame[0] != '!' {
		return "", false
	}
	if err := verifyDeviceBridgeCRC(frame); err != nil {
		return "", false
	}
	addr, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return "", false
	}
	seq, err := strconv.ParseUint(string(frame[3:7]), 16, 16)
	if err != nil {
		return "", false
	}
	if req.address != 0 && byte(addr) != req.address {
		return "", false
	}
	if uint16(seq) != req.seq {
		return "", false
	}
	payloadStart := 7
	switch frame[payloadStart] {
	case '-':
		return "", false
	case '+':
		payloadStart++
	}
	payloadEnd := len(frame) - 4
	if payloadStart > payloadEnd {
		return "", false
	}
	return strings.ToUpper(string(frame[payloadStart:payloadEnd])), true
}

func deviceBridgeObservePayload(state *deviceBridgeState, requestPayload, responsePayload string) {
	requestPayload = strings.ToUpper(strings.TrimSpace(requestPayload))
	responsePayload = strings.ToUpper(strings.TrimSpace(responsePayload))
	switch {
	case strings.HasPrefix(requestPayload, "?VM"):
		if len(requestPayload) != len("?VM000000") || len(responsePayload) < 38 {
			return
		}
		paramID, instance, err := parseDeviceBridgeParameter(requestPayload[3:])
		if err != nil || !deviceBridgeMetadataLiveActualParameter(paramID) {
			return
		}
		deviceBridgeObserveHexValue(state, paramID, instance, deviceBridgeParameterType(paramID), responsePayload[len(responsePayload)-8:], deviceBridgeCacheSourceDownstream, true)
	case strings.HasPrefix(requestPayload, "?VR"):
		if len(requestPayload) != len("?VR000000") || len(responsePayload) < 8 {
			return
		}
		paramID, instance, err := parseDeviceBridgeParameter(requestPayload[3:])
		if err != nil {
			return
		}
		deviceBridgeObserveHexValue(state, paramID, instance, deviceBridgeParameterType(paramID), responsePayload[:8], deviceBridgeCacheSourceDownstream, true)
	case strings.HasPrefix(requestPayload, "?VX"):
		if len(requestPayload) < len("?VX00") {
			return
		}
		count64, err := strconv.ParseUint(requestPayload[3:5], 16, 8)
		if err != nil {
			return
		}
		count := int(count64)
		if len(requestPayload) != 5+count*6 || len(responsePayload) < count*8 {
			return
		}
		for i := 0; i < count; i++ {
			paramID, instance, err := parseDeviceBridgeParameter(requestPayload[5+i*6 : 11+i*6])
			if err != nil {
				continue
			}
			deviceBridgeObserveHexValue(state, paramID, instance, deviceBridgeParameterType(paramID), responsePayload[i*8:i*8+8], deviceBridgeCacheSourceDownstream, true)
		}
	case strings.HasPrefix(requestPayload, "?VB"):
		if len(requestPayload) != len("?VB000000000000000000") {
			return
		}
		paramID, instance, err := parseDeviceBridgeParameter(requestPayload[3:9])
		if err != nil {
			return
		}
		deviceBridgeObserveBigDataReadValue(state, paramID, instance, responsePayload)
	case strings.HasPrefix(requestPayload, "VS"):
		if len(requestPayload) < len("VS00000000000000") {
			return
		}
		paramID, instance, err := parseDeviceBridgeParameter(requestPayload[2:8])
		if err != nil {
			return
		}
		deviceBridgeObserveHexValue(state, paramID, instance, deviceBridgeParameterType(paramID), requestPayload[8:], deviceBridgeCacheSourceLocal, false)
	case strings.HasPrefix(requestPayload, "VB"):
		if len(requestPayload) < len("VB00000000000000000000") {
			return
		}
		paramID, instance, err := parseDeviceBridgeParameter(requestPayload[2:8])
		if err != nil || deviceBridgeParameterType(paramID) != mecom.DataTypeLatin1 {
			return
		}
		count64, err := parseDeviceBridgeHexUint(requestPayload[16:20], "big-data write element count", 16)
		if err != nil {
			return
		}
		valueHex := requestPayload[22:]
		if len(valueHex) != int(count64)*2 {
			return
		}
		deviceBridgeObserveHexValue(state, paramID, instance, mecom.DataTypeLatin1, valueHex, deviceBridgeCacheSourceLocal, false)
	}
}

func deviceBridgeObserveHexValue(state *deviceBridgeState, id, instance int, typ mecom.DataType, valueHex string, source string, liveRefresh bool) {
	if state == nil {
		return
	}
	if typ == "" {
		typ = mecom.DataTypeFloat32
	}
	if !deviceBridgeParameterTypeSupported(typ) {
		return
	}
	value := deviceBridgeVirtualParameterValue{
		typ:         typ,
		source:      source,
		liveRefresh: liveRefresh,
		updatedAt:   time.Now().UTC(),
	}
	switch typ {
	case mecom.DataTypeInt32:
		if len(valueHex) != 8 {
			return
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return
		}
		value.int = deviceBridgeCompatibleInt32Value(id, int32(uint32(bits)))
	case mecom.DataTypeFloat32:
		if len(valueHex) != 8 {
			return
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return
		}
		value.float = float64(math.Float32frombits(uint32(bits)))
		if math.IsNaN(value.float) {
			return
		}
	case mecom.DataTypeLatin1:
		data, err := hex.DecodeString(valueHex)
		if err != nil {
			return
		}
		value.latin1 = string(bytes.TrimRight(data, "\x00"))
	default:
		return
	}
	_ = state.storeParameterValue(id, instance, value)
}

func deviceBridgeObserveBigDataReadValue(state *deviceBridgeState, id, instance int, responsePayload string) {
	if state == nil || deviceBridgeParameterType(id) != mecom.DataTypeLatin1 || len(responsePayload) < 6 {
		return
	}
	count64, err := parseDeviceBridgeHexUint(responsePayload[:4], "big-data read element count", 16)
	if err != nil || count64 == 0 {
		return
	}
	valueHex := responsePayload[6:]
	if len(valueHex) < int(count64)*2 {
		return
	}
	deviceBridgeObserveHexValue(state, id, instance, mecom.DataTypeLatin1, valueHex[:int(count64)*2], deviceBridgeCacheSourceDownstream, true)
}

func deviceBridgeStoreLiveNumericValue(state *deviceBridgeState, id, instance int, typ mecom.DataType, value float64) {
	if state == nil || math.IsNaN(value) {
		return
	}
	cacheValue := deviceBridgeVirtualParameterValue{
		typ:         typ,
		source:      deviceBridgeCacheSourceDownstream,
		liveRefresh: true,
		updatedAt:   time.Now().UTC(),
	}
	switch typ {
	case mecom.DataTypeInt32:
		cacheValue.int = deviceBridgeCompatibleInt32Value(id, int32(value))
	case mecom.DataTypeFloat32:
		cacheValue.float = value
	case "":
		cacheValue.typ = mecom.DataTypeFloat32
		cacheValue.float = value
	default:
		return
	}
	_ = state.storeParameterValue(id, instance, cacheValue)
}

func deviceBridgePayload(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	return deviceBridgePayloadWithState(ctx, defaultDeviceBridgeState(), client, payload)
}

func deviceBridgePayloadWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	switch {
	case payload == "?IF" || payload == "?VI":
		return deviceBridgeFirmwareIdentification, nil
	case payload == "?BI" || payload == "?BID" || payload == "?BIF":
		return deviceBridgeBoardIdentification, nil
	case strings.HasPrefix(payload, "?VM"):
		return deviceBridgeMetadataWithState(ctx, state, client, payload)
	case strings.HasPrefix(payload, "?VB"):
		return deviceBridgeBigDataReadWithState(ctx, state, client, payload)
	case strings.HasPrefix(payload, "?VL"):
		return deviceBridgeLimits(payload)
	case strings.HasPrefix(payload, "?BC"):
		return deviceBridgeBootloaderStatus(payload)
	case strings.HasPrefix(payload, "?RS"):
		return deviceBridgeRingPayload(ctx, client, payload)
	case strings.HasPrefix(payload, "?VR"):
		return deviceBridgeSingleReadWithState(ctx, state, client, payload)
	case strings.HasPrefix(payload, "?VX"):
		return deviceBridgeBulkReadWithState(ctx, state, client, payload)
	case strings.HasPrefix(payload, "VB"):
		return "", deviceBridgeBigDataWriteWithState(ctx, state, client, payload)
	case strings.HasPrefix(payload, "VS"):
		return "", deviceBridgeWriteWithState(ctx, state, client, payload)
	case payload == "SP":
		control, ok := client.(mecom.ControlClient)
		if !ok {
			return "", nil
		}
		return "", control.SaveToFlash(ctx)
	case payload == "RS":
		control, ok := client.(mecom.ControlClient)
		if !ok {
			return "", mecom.ErrTransportNotSupported
		}
		return "", control.Reset(ctx)
	default:
		return "", fmt.Errorf("%w: ASCII command %q", mecom.ErrTransportNotSupported, payloadCommand(payload))
	}
}

func isDeviceBridgeInfoProbe(payload string) bool {
	return payload == "?IF" || payload == "?VI" || payload == "?BI" || payload == "?BID" || payload == "?BIF"
}

func isDeviceBridgeRead(payload string) bool {
	return strings.HasPrefix(payload, "?VR") ||
		strings.HasPrefix(payload, "?VX") ||
		strings.HasPrefix(payload, "?VM") ||
		strings.HasPrefix(payload, "?VB") ||
		strings.HasPrefix(payload, "?VL") ||
		strings.HasPrefix(payload, "?RS") ||
		strings.HasPrefix(payload, "?BC")
}

func deviceBridgeMetadata(payload string) (string, error) {
	return deviceBridgeMetadataWithActual(context.Background(), nil, nil, payload)
}

func deviceBridgeMetadataWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	return deviceBridgeMetadataWithActual(ctx, state, client, payload)
}

func deviceBridgeMetadataWithActual(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) != len("?VM000000") {
		return "", fmt.Errorf("%w: invalid ?VM payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	meParType, err := deviceBridgeMeParType(typ)
	if err != nil {
		if deviceBridgeUnsupportedScalarNACK(paramID) {
			return "", fmt.Errorf("%w: unsupported parameter %d metadata is not scalar-readable", mecom.ErrUnknownParameter, paramID)
		}
		return deviceBridgeFallbackMetadata(instance), nil
	}
	flags := byte(deviceBridgeMetadataReadFlag)
	if deviceBridgeParameterWritable(paramID) {
		flags |= deviceBridgeMetadataWriteFlag
	}
	if transform, ok := mecom.CANopenBridgeTransform(paramID); ok && transform.HasMetadataFlags {
		flags = transform.MetadataFlags
	}
	minimum, maximum, actual, err := deviceBridgeParameterBounds(typ)
	if err != nil {
		return "", err
	}
	if deviceBridgeMetadataLiveActualParameter(paramID) {
		if liveActual, ok := deviceBridgeLiveMetadataActual(ctx, state, client, paramID, instance, typ); ok {
			actual = liveActual
		} else if cachedActual, ok := deviceBridgeCachedMetadataActual(state, paramID, instance, typ); ok {
			actual = cachedActual
		}
	}
	return fmt.Sprintf("%02X%02X%02X%08X%s%s%s",
		meParType,
		flags,
		deviceBridgeParameterMaxInstances(paramID),
		deviceBridgeParameterMaxElements(paramID, typ),
		minimum,
		maximum,
		actual,
	), nil
}

func deviceBridgeMetadataLiveActualParameter(id int) bool {
	if defaultDeviceBridgeCacheBehaviors[id] == mecom.CANopenCompatibilityCacheBehaviorMetadataLiveActual {
		return true
	}
	_, ok := deviceBridgeTransform(id, deviceBridgeTransformVirtualParameter)
	return ok
}

func deviceBridgeLiveMetadataActual(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, id, instance int, typ mecom.DataType) (string, bool) {
	if client == nil {
		return "", false
	}
	switch typ {
	case mecom.DataTypeInt32, mecom.DataTypeFloat32, "":
	default:
		return "", false
	}
	payload := fmt.Sprintf("?VR%04X%02X", id, instance)
	actual, err := deviceBridgeSingleReadWithState(ctx, state, client, payload)
	if err != nil || len(actual) != 8 {
		return "", false
	}
	return actual, true
}

func deviceBridgeCachedMetadataActual(state *deviceBridgeState, id, instance int, typ mecom.DataType) (string, bool) {
	if state == nil {
		return "", false
	}
	if typ == "" {
		typ = mecom.DataTypeFloat32
	}
	value, ok, err := state.softParameterStore().lookupParameter(id, instance, typ)
	if err != nil || !ok || value.source == deviceBridgeCacheSourcePlaceholder {
		return "", false
	}
	switch typ {
	case mecom.DataTypeInt32:
		return fmt.Sprintf("%08X", uint32(value.int)), true
	case mecom.DataTypeFloat32:
		return fmt.Sprintf("%08X", math.Float32bits(float32(value.float))), true
	default:
		return "", false
	}
}

func deviceBridgeFallbackMetadata(instance int) string {
	maxInstances := byte(1)
	if instance > 0 && instance <= 0xff {
		maxInstances = byte(instance)
	}
	return fmt.Sprintf("%02X%02X%02X%08X%s%s%s",
		byte(0),
		byte(deviceBridgeMetadataReadFlag),
		maxInstances,
		uint32(1),
		"FF800000",
		"7F800000",
		"00000000",
	)
}

func deviceBridgeLimits(payload string) (string, error) {
	if len(payload) != len("?VL000000") {
		return "", fmt.Errorf("%w: invalid ?VL payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, _, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	meParType, err := deviceBridgeMeParType(typ)
	if err != nil {
		return "", err
	}
	minimum, maximum, _, err := deviceBridgeParameterBounds(typ)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%02X%s%s", meParType, minimum, maximum), nil
}

func deviceBridgeBigDataRead(payload string) (string, error) {
	return deviceBridgeBigDataReadWithState(context.Background(), defaultDeviceBridgeState(), nil, payload)
}

func deviceBridgeBigDataReadWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) != len("?VB000000000000000000") {
		return "", fmt.Errorf("%w: invalid ?VB payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[3:9])
	if err != nil {
		return "", err
	}
	start, err := parseDeviceBridgeHexUint(payload[9:17], "big-data read start", 32)
	if err != nil {
		return "", err
	}
	maxElements, err := parseDeviceBridgeHexUint(payload[17:21], "big-data max elements", 16)
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	switch typ {
	case mecom.DataTypeLatin1:
	case mecom.DataTypeByte:
	default:
		return "000000", nil
	}
	if typ == mecom.DataTypeByte {
		transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformCANopenPDOConfigBytes)
		if !ok {
			return "000000", nil
		}
		data, err := deviceBridgeCANopenPDOConfigBytes(ctx, client, transform)
		if err != nil {
			return "", err
		}
		return deviceBridgeBigDataResponse(data, start, maxElements)
	}
	if state == nil {
		state = defaultDeviceBridgeState()
	}
	store := state.softParameterStore()
	if value, ok, err := store.lookupParameter(paramID, instance, mecom.DataTypeLatin1); err != nil {
		return "", err
	} else if ok && value.latin1 != "" {
		// CoSo tolerates empty big-data payloads during load, but if we have a
		// locally written string, expose it back instead of replacing truth with
		// an empty placeholder.
		data := []byte(value.latin1)
		return deviceBridgeBigDataResponse(data, start, maxElements)
	}
	return "000000", nil
}

func deviceBridgeBigDataResponse(data []byte, start, maxElements uint64) (string, error) {
	if maxElements == 0 || start >= uint64(len(data)) {
		return "000000", nil
	}
	end := start + maxElements
	if end < start || end > uint64(len(data)) {
		end = uint64(len(data))
	}
	segment := data[start:end]
	continuation := byte(0)
	if len(segment) > 0 {
		continuation = 1
	}
	return fmt.Sprintf("%04X%02X%s", len(segment), continuation, strings.ToUpper(hex.EncodeToString(segment))), nil
}

func deviceBridgeCANopenPDOConfigBytes(ctx context.Context, client mecom.DeviceClient, transform mecom.BridgeTransform) ([]byte, error) {
	reader, ok := client.(deviceBridgeRawSDOReader)
	if !ok {
		return nil, fmt.Errorf("%w: raw CANopen SDO reads unavailable for parameter %d", mecom.ErrTransportNotSupported, transform.MeComID)
	}
	if transform.CANopenIndexBase == 0 || transform.MaxElements <= 0 {
		return nil, fmt.Errorf("%w: incomplete PDO config transform for parameter %d", mecom.ErrInvalidArgument, transform.MeComID)
	}

	out := make([]byte, 0, transform.MaxElements)
	var lastErr error
	successes := 0
	for n := 0; n < 16; n++ {
		index := transform.CANopenIndexBase + uint16(n)
		switch transform.MeComID {
		case 2150:
			var ok bool
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 1, 4)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 2, 1)
			if ok {
				successes++
			}
		case 2151, 2153:
			var ok bool
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 0, 1)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 1, 4)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 2, 4)
			if ok {
				successes++
			}
		case 2152:
			var ok bool
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 1, 4)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 2, 1)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 3, 2)
			if ok {
				successes++
			}
			out, ok, lastErr = deviceBridgeAppendSDOBytes(ctx, reader, out, index, 5, 2)
			if ok {
				successes++
			}
		default:
			return nil, fmt.Errorf("%w: unsupported PDO config transform for parameter %d", mecom.ErrInvalidArgument, transform.MeComID)
		}
	}
	if successes == 0 && lastErr != nil {
		return nil, lastErr
	}
	for len(out) < transform.MaxElements {
		out = append(out, 0)
	}
	if len(out) > transform.MaxElements {
		out = out[:transform.MaxElements]
	}
	return out, nil
}

func deviceBridgeAppendSDOBytes(ctx context.Context, reader deviceBridgeRawSDOReader, out []byte, index uint16, subIndex byte, width int) ([]byte, bool, error) {
	data, err := reader.ReadSDORaw(ctx, index, subIndex)
	if err != nil {
		return append(out, bytes.Repeat([]byte{0}, width)...), false, err
	}
	for len(data) < width {
		data = append(data, 0)
	}
	return append(out, data[:width]...), true, nil
}

func deviceBridgeBootloaderStatus(payload string) (string, error) {
	if len(payload) != len("?BC00000000") {
		return "", fmt.Errorf("%w: invalid ?BC payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	command, err := parseDeviceBridgeHexUint(payload[3:], "bootloader command", 32)
	if err != nil {
		return "", err
	}
	if command != 0 {
		return "", fmt.Errorf("%w: bootloader command 0x%08X", mecom.ErrTransportNotSupported, command)
	}
	return "00000000", nil
}

func deviceBridgeSingleRead(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	return deviceBridgeSingleReadWithState(ctx, defaultDeviceBridgeState(), client, payload)
}

func deviceBridgeSingleReadWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) != len("?VR000000") {
		return "", fmt.Errorf("%w: invalid ?VR payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[3:])
	if err != nil {
		return "", err
	}
	typ := deviceBridgeParameterType(paramID)
	if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformVirtualParameter); ok {
		value, err := deviceBridgeVirtualParameterValueWithState(ctx, state, client, transform, instance)
		if err != nil {
			return "", err
		}
		switch transform.Type {
		case mecom.DataTypeInt32:
			return fmt.Sprintf("%08X", uint32(value.int)), nil
		case mecom.DataTypeFloat32, "":
			return fmt.Sprintf("%08X", math.Float32bits(float32(value.float))), nil
		default:
			return "", fmt.Errorf("%w: single read of virtual %s parameter %d", mecom.ErrTransportNotSupported, transform.Type, paramID)
		}
	}
	if deviceBridgeLooseFallbackParameter(paramID) {
		value, err := deviceBridgeSoftParameterValue(ctx, state, client, paramID, instance, typ)
		if err != nil {
			return "", err
		}
		switch typ {
		case mecom.DataTypeInt32:
			return fmt.Sprintf("%08X", uint32(value.int)), nil
		case mecom.DataTypeFloat32, "":
			return fmt.Sprintf("%08X", math.Float32bits(float32(value.float))), nil
		case mecom.DataTypeLatin1:
			return "00000000", nil
		default:
			return "", fmt.Errorf("%w: single read of soft %s parameter %d", mecom.ErrTransportNotSupported, typ, paramID)
		}
	}
	switch typ {
	case mecom.DataTypeInt32:
		if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformConstantInt32); ok {
			return fmt.Sprintf("%08X", uint32(transform.Int32Value)), nil
		}
		value, err := client.ReadInt32(ctx, paramID, instance)
		if err != nil {
			return "", err
		}
		value = deviceBridgeCompatibleInt32Value(paramID, value)
		deviceBridgeStoreLiveNumericValue(state, paramID, instance, typ, float64(value))
		return fmt.Sprintf("%08X", uint32(value)), nil
	case mecom.DataTypeFloat32, "":
		if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformSynthesizeFloat32FromInt32); ok {
			value, err := deviceBridgeSynthesizedFloat32(ctx, client, transform, instance)
			if err != nil {
				return "", err
			}
			deviceBridgeStoreLiveNumericValue(state, paramID, instance, mecom.DataTypeFloat32, value)
			return fmt.Sprintf("%08X", math.Float32bits(float32(value))), nil
		}
		value, err := client.ReadFloat32(ctx, paramID, instance)
		if err != nil {
			return "", err
		}
		deviceBridgeStoreLiveNumericValue(state, paramID, instance, typ, value)
		return fmt.Sprintf("%08X", math.Float32bits(float32(value))), nil
	default:
		return "", fmt.Errorf("%w: single read of %s parameter %d", mecom.ErrTransportNotSupported, typ, paramID)
	}
}

func deviceBridgeSoftParameterValue(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, id, instance int, typ mecom.DataType) (deviceBridgeVirtualParameterValue, error) {
	if state == nil {
		state = defaultDeviceBridgeState()
	}
	store := state.softParameterStore()

	if err := deviceBridgeValidateParameterInstance(id, instance); err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	}

	value := deviceBridgeVirtualParameterValue{
		typ:    typ,
		source: deviceBridgeCacheSourcePlaceholder,
	}
	switch typ {
	case mecom.DataTypeInt32:
		if client != nil {
			intValue, err := client.ReadInt32(ctx, id, instance)
			if err == nil {
				value.int = deviceBridgeCompatibleInt32Value(id, intValue)
				value.source = deviceBridgeCacheSourceDownstream
				value.liveRefresh = true
				value.updatedAt = time.Now().UTC()
				if err := state.storeParameterValue(id, instance, value); err != nil {
					return deviceBridgeVirtualParameterValue{}, err
				}
				return value, nil
			}
		}
	case mecom.DataTypeFloat32, "":
		if typ == "" {
			value.typ = mecom.DataTypeFloat32
		}
		if client != nil {
			floatValue, err := client.ReadFloat32(ctx, id, instance)
			if err == nil {
				value.float = floatValue
				value.source = deviceBridgeCacheSourceDownstream
				value.liveRefresh = true
				value.updatedAt = time.Now().UTC()
				if !math.IsNaN(floatValue) {
					if err := state.storeParameterValue(id, instance, value); err != nil {
						return deviceBridgeVirtualParameterValue{}, err
					}
				}
				return value, nil
			}
		}
	case mecom.DataTypeLatin1:
	default:
		return deviceBridgeVirtualParameterValue{}, fmt.Errorf("%w: soft read of %s parameter %d", mecom.ErrTransportNotSupported, typ, id)
	}

	if cached, ok, err := store.lookupParameter(id, instance, value.typ); err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	} else if ok {
		return cached, nil
	}
	return value, nil
}

func deviceBridgeVirtualParameterValueWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, transform mecom.BridgeTransform, instance int) (deviceBridgeVirtualParameterValue, error) {
	if err := deviceBridgeValidateTransformInstance(transform, instance); err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	}
	if state == nil {
		state = defaultDeviceBridgeState()
	}
	typ := transform.Type
	if typ == "" {
		typ = mecom.DataTypeFloat32
	}
	switch typ {
	case mecom.DataTypeInt32, mecom.DataTypeFloat32:
	default:
		return deviceBridgeVirtualParameterValue{}, fmt.Errorf("%w: virtual read of %s parameter %d", mecom.ErrTransportNotSupported, typ, transform.MeComID)
	}
	value, err := deviceBridgeSoftParameterValue(ctx, state, client, transform.MeComID, instance, typ)
	if err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	}
	if value.source != deviceBridgeCacheSourcePlaceholder {
		return value, nil
	}
	if cached, ok, err := state.virtualParameterStore().lookupParameter(transform.MeComID, instance, typ); err != nil {
		return deviceBridgeVirtualParameterValue{}, err
	} else if ok {
		return cached, nil
	}
	return deviceBridgeVirtualParameterDefault(transform, typ), nil
}

func deviceBridgeVirtualParameterDefault(transform mecom.BridgeTransform, typ mecom.DataType) deviceBridgeVirtualParameterValue {
	value := deviceBridgeVirtualParameterValue{
		typ:    typ,
		source: deviceBridgeCacheSourcePlaceholder,
	}
	switch typ {
	case mecom.DataTypeInt32:
		value.int = 0
	case mecom.DataTypeFloat32, "":
		value.typ = mecom.DataTypeFloat32
		switch transform.MeComID {
		case 52200:
			value.float = math.NaN()
		case 52201:
			value.float = 25
		default:
			value.float = 0
		}
	}
	return value
}

func deviceBridgeBulkRead(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	return deviceBridgeBulkReadWithState(ctx, defaultDeviceBridgeState(), client, payload)
}

func deviceBridgeBulkReadWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) (string, error) {
	if state == nil {
		state = defaultDeviceBridgeState()
	}
	if len(payload) < len("?VX00") {
		return "", fmt.Errorf("%w: invalid ?VX payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	count64, err := strconv.ParseUint(payload[3:5], 16, 8)
	if err != nil {
		return "", fmt.Errorf("%w: invalid ?VX count %q: %v", mecom.ErrInvalidArgument, payload[3:5], err)
	}
	count := int(count64)
	pairPayloadLen := len(payload) - 5
	if pairPayloadLen < 0 || pairPayloadLen%6 != 0 {
		return "", fmt.Errorf("%w: ?VX count %d does not match payload length %d", mecom.ErrInvalidArgument, count, len(payload))
	}
	actualCount := pairPayloadLen / 6
	if actualCount != count {
		count = actualCount
	}
	params := make([]mecom.Parameter, 0, count)
	// unsupportedSlots tracks indices whose type is unknown on this transport.
	// Their values stay 0 and are encoded as float32(0), matching the zero
	// value a real device would return for a param it holds at default.
	unsupportedSlots := make(map[int]struct{}, 0)
	for i := 0; i < count; i++ {
		paramID, instance, err := parseDeviceBridgeParameter(payload[5+i*6 : 11+i*6])
		if err != nil {
			return "", err
		}
		typ := deviceBridgeParameterType(paramID)
		if deviceBridgeLooseFallbackParameter(paramID) && typ == mecom.DataTypeLatin1 {
			unsupportedSlots[i] = struct{}{}
			params = append(params, mecom.Parameter{ID: paramID, Instance: instance, Type: mecom.DataTypeFloat32})
			continue
		}
		if !deviceBridgeParameterTypeSupported(typ) {
			if typ == mecom.DataTypeByte {
				return "", fmt.Errorf("%w: byte big-data parameter %d is not scalar bulk-readable", mecom.ErrTransportNotSupported, paramID)
			}
			if deviceBridgeUnsupportedBulkReadNACK(paramID) {
				return "", fmt.Errorf("%w: unsupported parameter %d is not scalar bulk-readable", mecom.ErrTransportNotSupported, paramID)
			}
			// Param has no mapping on this transport. Return zero for this slot
			// only when the catalogue has not marked the ID as unsafe to spoof,
			// so loose CoSo compatibility does not abort otherwise useful
			// batches.
			unsupportedSlots[i] = struct{}{}
			params = append(params, mecom.Parameter{ID: paramID, Instance: instance, Type: mecom.DataTypeFloat32})
			continue
		}
		params = append(params, mecom.Parameter{ID: paramID, Instance: instance, Type: typ})
	}
	values := make([]float64, len(params))
	liveSlots := make([]bool, len(params))
	downstreamParams := make([]mecom.Parameter, 0, len(params))
	downstreamIndexes := make([]int, 0, len(params))
	for i, param := range params {
		if _, skip := unsupportedSlots[i]; skip {
			// values[i] already zero; skip downstream read.
			continue
		}
		if transform, ok := deviceBridgeTransform(param.ID, deviceBridgeTransformVirtualParameter); ok {
			value, err := deviceBridgeVirtualParameterValueWithState(ctx, state, client, transform, param.Instance)
			if err != nil {
				values[i] = 0
				continue
			}
			if transform.Type == mecom.DataTypeInt32 {
				values[i] = float64(value.int)
			} else {
				values[i] = value.float
			}
			continue
		}
		if deviceBridgeLooseFallbackParameter(param.ID) {
			value, err := deviceBridgeSoftParameterValue(ctx, state, client, param.ID, param.Instance, param.Type)
			if err != nil {
				values[i] = 0
				continue
			}
			if param.Type == mecom.DataTypeInt32 {
				values[i] = float64(value.int)
			} else {
				values[i] = value.float
			}
			continue
		}
		if transform, ok := deviceBridgeTransform(param.ID, deviceBridgeTransformConstantInt32); ok {
			values[i] = float64(transform.Int32Value)
			continue
		}
		if transform, ok := deviceBridgeTransform(param.ID, deviceBridgeTransformSynthesizeFloat32FromInt32); ok {
			value, err := deviceBridgeSynthesizedFloat32(ctx, client, transform, param.Instance)
			if err != nil {
				values[i] = 0
				continue
			}
			values[i] = value
			liveSlots[i] = true
			continue
		}
		if deviceBridgeMetadataLiveActualParameter(param.ID) {
			if value, ok := deviceBridgeBulkReadSingleFallback(ctx, client, param); ok {
				values[i] = value
				liveSlots[i] = true
				continue
			}
			if value, ok := deviceBridgeCachedBulkReadValue(state, param); ok {
				values[i] = value
			}
			continue
		}
		downstreamIndexes = append(downstreamIndexes, i)
		downstreamParams = append(downstreamParams, param)
	}
	if len(downstreamParams) > 0 {
		downstreamValues, err := client.ReadBulk(ctx, downstreamParams)
		if err != nil || len(downstreamValues) != len(downstreamParams) {
			for i, param := range downstreamParams {
				value, ok := deviceBridgeBulkReadSingleFallback(ctx, client, param)
				values[downstreamIndexes[i]] = value
				liveSlots[downstreamIndexes[i]] = ok
			}
		} else {
			for i, value := range downstreamValues {
				values[downstreamIndexes[i]] = value
				liveSlots[downstreamIndexes[i]] = true
			}
		}
	}
	var out strings.Builder
	for i, value := range values {
		if liveSlots[i] {
			deviceBridgeStoreLiveNumericValue(state, params[i].ID, params[i].Instance, params[i].Type, value)
		}
		switch params[i].Type {
		case mecom.DataTypeInt32:
			if math.IsNaN(value) {
				value = 0
			}
			intValue := deviceBridgeCompatibleInt32Value(params[i].ID, int32(value))
			fmt.Fprintf(&out, "%08X", uint32(intValue))
		case mecom.DataTypeFloat32:
			fmt.Fprintf(&out, "%08X", math.Float32bits(float32(value)))
		default:
			return "", fmt.Errorf("%w: bulk read of %s parameter %d", mecom.ErrTransportNotSupported, params[i].Type, params[i].ID)
		}
	}
	return out.String(), nil
}

func deviceBridgeBulkReadSingleFallback(ctx context.Context, client mecom.DeviceClient, param mecom.Parameter) (float64, bool) {
	if client == nil {
		return 0, false
	}
	switch param.Type {
	case mecom.DataTypeInt32:
		value, err := client.ReadInt32(ctx, param.ID, param.Instance)
		if err != nil {
			return 0, false
		}
		return float64(value), true
	case mecom.DataTypeFloat32, "":
		value, err := client.ReadFloat32(ctx, param.ID, param.Instance)
		if err != nil {
			return 0, false
		}
		return value, true
	default:
		return 0, false
	}
}

func deviceBridgeCachedBulkReadValue(state *deviceBridgeState, param mecom.Parameter) (float64, bool) {
	if state == nil {
		return 0, false
	}
	value, ok, err := state.softParameterStore().lookupParameter(param.ID, param.Instance, param.Type)
	if err != nil || !ok || value.source == deviceBridgeCacheSourcePlaceholder {
		return 0, false
	}
	switch param.Type {
	case mecom.DataTypeInt32:
		return float64(value.int), true
	case mecom.DataTypeFloat32, "":
		return value.float, true
	default:
		return 0, false
	}
}

func deviceBridgeCompatibleInt32Value(paramID int, value int32) int32 {
	if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformMaskInt32); ok {
		return int32(uint32(value) & transform.Int32Mask)
	}
	return value
}

func deviceBridgeSynthesizedFloat32(ctx context.Context, client mecom.DeviceClient, transform mecom.BridgeTransform, instance int) (float64, error) {
	version, err := client.ReadInt32(ctx, transform.SourceMeComID, instance)
	if err != nil {
		return 0, err
	}
	return float64(version) * transform.Scale, nil
}

func deviceBridgeTransform(paramID int, kind string) (mecom.BridgeTransform, bool) {
	transform, ok := mecom.CANopenBridgeTransform(paramID)
	return transform, ok && transform.Kind == kind
}

func deviceBridgeBigDataWrite(ctx context.Context, client mecom.DeviceClient, payload string) error {
	return deviceBridgeBigDataWriteWithState(ctx, defaultDeviceBridgeState(), client, payload)
}

func deviceBridgeBigDataWriteWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) error {
	if len(payload) < len("VB00000000000000000000") {
		return fmt.Errorf("%w: invalid VB payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[2:8])
	if err != nil {
		return err
	}
	if !deviceBridgeParameterWritable(paramID) {
		return fmt.Errorf("%w: parameter %d instance %d", mecom.ErrParameterReadOnly, paramID, instance)
	}
	start64, err := parseDeviceBridgeHexUint(payload[8:16], "big-data write start", 32)
	if err != nil {
		return err
	}
	count64, err := parseDeviceBridgeHexUint(payload[16:20], "big-data element count", 16)
	if err != nil {
		return err
	}
	isLast64, err := parseDeviceBridgeHexUint(payload[20:22], "big-data final flag", 8)
	if err != nil {
		return err
	}
	valueHex := payload[22:]
	if len(valueHex) != int(count64)*2 {
		return fmt.Errorf("%w: VB element count %d does not match payload hex length %d", mecom.ErrInvalidArgument, count64, len(valueHex))
	}
	if deviceBridgeParameterType(paramID) != mecom.DataTypeLatin1 {
		return fmt.Errorf("%w: big-data write of non-LATIN1 parameter %d", mecom.ErrTransportNotSupported, paramID)
	}
	if start64 != 0 || isLast64 != 1 {
		return fmt.Errorf("%w: multi-package big-data writes are not supported", mecom.ErrTransportNotSupported)
	}
	if deviceBridgeLooseFallbackParameter(paramID) {
		if state == nil {
			state = defaultDeviceBridgeState()
		}
		store := state.softParameterStore()
		return store.writeParameter(paramID, instance, mecom.DataTypeLatin1, valueHex)
	}
	stringClient, ok := client.(mecom.StringWriteClient)
	if !ok {
		return mecom.ErrTransportNotSupported
	}
	data, err := hex.DecodeString(valueHex)
	if err != nil {
		return fmt.Errorf("%w: invalid LATIN1 big-data value for parameter %d: %v", mecom.ErrInvalidArgument, paramID, err)
	}
	return stringClient.WriteBigDataString(ctx, paramID, instance, string(bytes.TrimRight(data, "\x00")))
}

func deviceBridgeWrite(ctx context.Context, client mecom.DeviceClient, payload string) error {
	return deviceBridgeWriteWithState(ctx, defaultDeviceBridgeState(), client, payload)
}

func deviceBridgeWriteWithState(ctx context.Context, state *deviceBridgeState, client mecom.DeviceClient, payload string) error {
	if len(payload) < len("VS000000") {
		return fmt.Errorf("%w: invalid VS payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	paramID, instance, err := parseDeviceBridgeParameter(payload[2:8])
	if err != nil {
		return err
	}
	valueHex := payload[8:]
	if !deviceBridgeParameterWritable(paramID) {
		return fmt.Errorf("%w: parameter %d instance %d", mecom.ErrParameterReadOnly, paramID, instance)
	}
	if transform, ok := deviceBridgeTransform(paramID, deviceBridgeTransformVirtualParameter); ok {
		return deviceBridgeWriteVirtualParameter(state, transform, instance, valueHex)
	}
	if deviceBridgeLooseFallbackParameter(paramID) {
		typ := deviceBridgeParameterType(paramID)
		if state == nil {
			state = defaultDeviceBridgeState()
		}
		store := state.softParameterStore()
		return store.writeParameter(paramID, instance, typ, valueHex)
	}
	writer, ok := client.(mecom.WriteClient)
	if !ok {
		return mecom.ErrTransportNotSupported
	}
	typ := deviceBridgeParameterType(paramID)
	switch typ {
	case mecom.DataTypeInt32:
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: int32 value for parameter %d has %d hex chars", mecom.ErrInvalidArgument, paramID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid int32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		return writer.WriteInt32(ctx, paramID, instance, int32(uint32(bits)))
	case mecom.DataTypeFloat32, "":
		if len(valueHex) != 8 {
			return fmt.Errorf("%w: float32 value for parameter %d has %d hex chars", mecom.ErrInvalidArgument, paramID, len(valueHex))
		}
		bits, err := strconv.ParseUint(valueHex, 16, 32)
		if err != nil {
			return fmt.Errorf("%w: invalid float32 value %q: %v", mecom.ErrInvalidArgument, valueHex, err)
		}
		return writer.WriteFloat32(ctx, paramID, instance, math.Float32frombits(uint32(bits)))
	case mecom.DataTypeLatin1:
		stringClient, ok := client.(mecom.StringWriteClient)
		if !ok {
			return mecom.ErrTransportNotSupported
		}
		data, err := hex.DecodeString(valueHex)
		if err != nil {
			return fmt.Errorf("%w: invalid LATIN1 value for parameter %d: %v", mecom.ErrInvalidArgument, paramID, err)
		}
		return stringClient.WriteString(ctx, paramID, instance, string(bytes.TrimRight(data, "\x00")))
	default:
		return fmt.Errorf("%w: write of %s parameter %d", mecom.ErrTransportNotSupported, typ, paramID)
	}
}

func deviceBridgeWriteVirtualParameter(state *deviceBridgeState, transform mecom.BridgeTransform, instance int, valueHex string) error {
	if err := deviceBridgeValidateTransformInstance(transform, instance); err != nil {
		return err
	}
	if !transform.Writable {
		return fmt.Errorf("%w: virtual parameter %d instance %d", mecom.ErrParameterReadOnly, transform.MeComID, instance)
	}
	typ := transform.Type
	if typ == "" {
		typ = mecom.DataTypeFloat32
	}
	if state == nil {
		state = defaultDeviceBridgeState()
	}
	return state.writeParameter(transform.MeComID, instance, typ, valueHex)
}

func deviceBridgeRingPayload(ctx context.Context, client mecom.DeviceClient, payload string) (string, error) {
	if len(payload) < len("?RS0000") {
		return "", fmt.Errorf("%w: invalid ?RS payload length %d", mecom.ErrInvalidArgument, len(payload))
	}
	command := payload[3:7]
	switch command {
	case "0000":
		if len(payload) != len("?RS0000") {
			return "", fmt.Errorf("%w: invalid ring pointer payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		pointer, err := client.ReadRingPointer(ctx)
		if errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00000000", nil
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%08X", pointer), nil
	case "0001":
		if len(payload) != len("?RS0001000000000000") {
			return "", fmt.Errorf("%w: invalid ring read payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		start, err := parseDeviceBridgeHexUint(payload[7:15], "ring read start", 32)
		if err != nil {
			return "", err
		}
		maxBytes, err := parseDeviceBridgeHexUint(payload[15:19], "ring read max bytes", 16)
		if err != nil {
			return "", err
		}
		resp, err := client.ReadRingChunk(ctx, uint32(start), uint16(maxBytes))
		if errors.Is(err, mecom.ErrTransportNotSupported) {
			return "000000", nil
		}
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%04X%02X%s", resp.BytesAdded, resp.Status, strings.ToUpper(hex.EncodeToString(resp.Data))), nil
	case "0002":
		if len(payload) < len("?RS0002000000") {
			return "", fmt.Errorf("%w: invalid ring capture-config payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		captureID, err := parseDeviceBridgeHexUint(payload[7:11], "ring capture id", 16)
		if err != nil {
			return "", err
		}
		count64, err := parseDeviceBridgeHexUint(payload[11:13], "ring capture count", 8)
		if err != nil {
			return "", err
		}
		count := int(count64)
		if len(payload) != 13+count*10 {
			return "", fmt.Errorf("%w: ring capture count %d does not match payload length %d", mecom.ErrInvalidArgument, count, len(payload))
		}
		params := make([]mecom.RingCaptureParameter, 0, count)
		for i := 0; i < count; i++ {
			off := 13 + i*10
			paramID, instance, err := parseDeviceBridgeParameter(payload[off : off+6])
			if err != nil {
				return "", err
			}
			inhibit, err := parseDeviceBridgeHexUint(payload[off+6:off+10], "ring inhibit time", 16)
			if err != nil {
				return "", err
			}
			params = append(params, mecom.RingCaptureParameter{
				Parameter: mecom.Parameter{
					ID:       paramID,
					Instance: instance,
					Type:     deviceBridgeParameterType(paramID),
				},
				InhibitTime10us: uint16(inhibit),
			})
		}
		if err := client.ConfigureRingCapture(ctx, uint16(captureID), params); errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00", nil
		} else if err != nil {
			return "", err
		}
		return "00", nil
	case "0003":
		if len(payload) != len("?RS0003") {
			return "", fmt.Errorf("%w: invalid ring trigger-sync payload length %d", mecom.ErrInvalidArgument, len(payload))
		}
		if err := client.TriggerRingSync(ctx); errors.Is(err, mecom.ErrTransportNotSupported) {
			return "00", nil
		} else if err != nil {
			return "", err
		}
		return "00", nil
	default:
		return "", fmt.Errorf("%w: ring command %s", mecom.ErrTransportNotSupported, command)
	}
}

func parseDeviceBridgeParameter(payload string) (int, int, error) {
	if len(payload) != 6 {
		return 0, 0, fmt.Errorf("%w: parameter payload %q must be 6 hex chars", mecom.ErrInvalidArgument, payload)
	}
	paramID, err := strconv.ParseUint(payload[:4], 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid parameter id %q: %v", mecom.ErrInvalidArgument, payload[:4], err)
	}
	instance, err := strconv.ParseUint(payload[4:], 16, 8)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid parameter instance %q: %v", mecom.ErrInvalidArgument, payload[4:], err)
	}
	return int(paramID), int(instance), nil
}

func parseDeviceBridgeHexUint(value, name string, bitSize int) (uint64, error) {
	out, err := strconv.ParseUint(value, 16, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: invalid %s %q: %v", mecom.ErrInvalidArgument, name, value, err)
	}
	return out, nil
}

func deviceBridgeMeParType(typ mecom.DataType) (byte, error) {
	switch typ {
	case mecom.DataTypeFloat32, "":
		return 0, nil
	case mecom.DataTypeInt32:
		return 1, nil
	case mecom.DataTypeLatin1:
		return 3, nil
	case mecom.DataTypeByte:
		return 4, nil
	default:
		return 0, fmt.Errorf("%w: unsupported metadata type %s", mecom.ErrTransportNotSupported, typ)
	}
}

func deviceBridgeParameterBounds(typ mecom.DataType) (string, string, string, error) {
	switch typ {
	case mecom.DataTypeFloat32, "":
		return "FF800000", "7F800000", "00000000", nil
	case mecom.DataTypeInt32:
		return "80000000", "7FFFFFFF", "00000000", nil
	case mecom.DataTypeLatin1:
		return "00", "00", "00", nil
	case mecom.DataTypeByte:
		return "00", "FF", "00", nil
	default:
		return "", "", "", fmt.Errorf("%w: unsupported bounds type %s", mecom.ErrTransportNotSupported, typ)
	}
}

func deviceBridgeParameterMaxElements(id int, typ mecom.DataType) uint32 {
	if typ == mecom.DataTypeLatin1 {
		return deviceBridgeBigDataMaxElements
	}
	if typ == mecom.DataTypeByte {
		if transform, ok := deviceBridgeTransform(id, deviceBridgeTransformCANopenPDOConfigBytes); ok && transform.MaxElements > 0 {
			return uint32(transform.MaxElements)
		}
	}
	return 1
}

func deviceBridgeParameterMaxInstances(id int) byte {
	if mappedMax, ok := defaultDeviceBridgeParameterMaxInstances[id]; ok && mappedMax > 0 {
		return mappedMax
	}
	if mappedMax, ok := deviceBridgeCoSoLooseParameterMaxInstances[id]; ok && mappedMax > 0 {
		return mappedMax
	}
	return 1
}

func deviceBridgeParameterWritable(id int) bool {
	if writable, ok := defaultDeviceBridgeWritableParameters[id]; ok {
		return writable
	}
	return false
}

func deviceBridgeOK(addr byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("!%02X%04X+%s", addr, seq, strings.ToUpper(payload))
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func deviceBridgeInfo(addr byte, seq uint16, payload string) []byte {
	return deviceBridgeData(addr, seq, payload)
}

func deviceBridgeData(addr byte, seq uint16, payload string) []byte {
	prefix := fmt.Sprintf("!%02X%04X%s", addr, seq, payload)
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func deviceBridgeNACK(addr byte, seq uint16, code byte) []byte {
	prefix := fmt.Sprintf("!%02X%04X-%02X", addr, seq, code)
	return []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))
}

func nackCodeForError(err error) byte {
	switch {
	case errors.Is(err, mecom.ErrUnknownParameter):
		return 0x05
	case errors.Is(err, mecom.ErrParameterReadOnly):
		return 0x06
	case errors.Is(err, mecom.ErrInvalidArgument):
		return 0x04
	case errors.Is(err, mecom.ErrTransportNotSupported):
		return 0x01
	default:
		return 0x03
	}
}

func payloadCommand(payload string) string {
	if len(payload) < 2 {
		return payload
	}
	if payload[0] == '?' && len(payload) >= 3 {
		return payload[:3]
	}
	return payload[:2]
}

var defaultDeviceBridgeParameterTypes = buildDefaultDeviceBridgeParameterTypes()

var deviceBridgeCoSoLooseParameterTypes = map[int]mecom.DataType{
	200:   mecom.DataTypeInt32,
	203:   mecom.DataTypeInt32,
	204:   mecom.DataTypeInt32,
	209:   mecom.DataTypeInt32,
	210:   mecom.DataTypeInt32,
	213:   mecom.DataTypeInt32,
	214:   mecom.DataTypeInt32,
	1065:  mecom.DataTypeLatin1,
	1081:  mecom.DataTypeInt32,
	2072:  mecom.DataTypeInt32,
	2080:  mecom.DataTypeInt32,
	2100:  mecom.DataTypeInt32,
	3034:  mecom.DataTypeInt32,
	4034:  mecom.DataTypeInt32,
	6001:  mecom.DataTypeInt32,
	6020:  mecom.DataTypeInt32,
	6023:  mecom.DataTypeInt32,
	6024:  mecom.DataTypeLatin1,
	6025:  mecom.DataTypeLatin1,
	6026:  mecom.DataTypeLatin1,
	6051:  mecom.DataTypeInt32,
	6052:  mecom.DataTypeInt32,
	6100:  mecom.DataTypeInt32,
	6101:  mecom.DataTypeInt32,
	6102:  mecom.DataTypeInt32,
	6103:  mecom.DataTypeInt32,
	6120:  mecom.DataTypeInt32,
	6143:  mecom.DataTypeInt32,
	6330:  mecom.DataTypeInt32,
	51000: mecom.DataTypeInt32,
	51001: mecom.DataTypeInt32,
	51002: mecom.DataTypeInt32,
	51020: mecom.DataTypeInt32,
	52000: mecom.DataTypeInt32,
	52001: mecom.DataTypeInt32,
	52002: mecom.DataTypeInt32,
	52013: mecom.DataTypeInt32,
	52014: mecom.DataTypeInt32,
	53000: mecom.DataTypeLatin1,
	53100: mecom.DataTypeInt32,
	53101: mecom.DataTypeInt32,
	53126: mecom.DataTypeInt32,
	53132: mecom.DataTypeInt32,
	53160: mecom.DataTypeInt32,
	53181: mecom.DataTypeInt32,
	53182: mecom.DataTypeInt32,
}

var deviceBridgeCoSoLooseParameterMaxInstances = map[int]byte{
	3034:  4,
	4034:  2,
	6001:  2,
	6023:  4,
	6024:  4,
	6025:  4,
	6026:  4,
	6051:  2,
	6052:  2,
	6100:  10,
	6101:  10,
	6102:  10,
	6103:  10,
	6120:  2,
	6143:  4,
	51000: 4,
	51001: 4,
	51002: 4,
	51020: 4,
	52000: 4,
	52001: 4,
	52002: 4,
	52013: 4,
	52014: 4,
	53100: 2,
	53101: 2,
	53126: 2,
	53132: 2,
	53160: 2,
	53181: 2,
	53182: 2,
}

func deviceBridgeCoSoLooseParameter(id int) bool {
	_, ok := deviceBridgeCoSoLooseParameterTypes[id]
	return ok
}

func deviceBridgeLooseFallbackParameter(id int) bool {
	if !deviceBridgeCoSoLooseParameter(id) {
		return false
	}
	_, ok := defaultDeviceBridgeParameterTypes[id]
	return !ok
}

func buildDefaultDeviceBridgeParameterTypes() map[int]mecom.DataType {
	out := map[int]mecom.DataType{}
	for id, typ := range mecom.CANopenCompatibilityParameterTypes() {
		if typ != "" {
			out[id] = typ
		}
	}
	return out
}

var defaultDeviceBridgeUnsupportedParameterBridgeBehavior = buildDefaultDeviceBridgeUnsupportedParameterBridgeBehavior()

func buildDefaultDeviceBridgeUnsupportedParameterBridgeBehavior() map[int]string {
	out := map[int]string{}
	for id, behavior := range mecom.CANopenUnsupportedParameterBridgeBehavior() {
		behavior = strings.ToLower(strings.TrimSpace(behavior))
		if behavior != "" {
			out[id] = behavior
		}
	}
	return out
}

func deviceBridgeUnsupportedBulkReadNACK(id int) bool {
	return deviceBridgeUnsupportedReadNACK(id)
}

func deviceBridgeUnsupportedScalarNACK(id int) bool {
	return deviceBridgeUnsupportedReadNACK(id)
}

func deviceBridgeUnsupportedReadNACK(id int) bool {
	switch defaultDeviceBridgeUnsupportedParameterBridgeBehavior[id] {
	case mecom.CANopenUnsupportedBridgeBehaviorNACKBulkRead, mecom.CANopenUnsupportedBridgeBehaviorSerialFallbackRead:
		return true
	default:
		return false
	}
}

func deviceBridgeUnsupportedSerialFallbackRead(id int) bool {
	return defaultDeviceBridgeUnsupportedParameterBridgeBehavior[id] == mecom.CANopenUnsupportedBridgeBehaviorSerialFallbackRead
}

func deviceBridgeParameterType(id int) mecom.DataType {
	if typ := defaultDeviceBridgeParameterTypes[id]; typ != "" {
		return typ
	}
	if typ := deviceBridgeCoSoLooseParameterTypes[id]; typ != "" {
		return typ
	}
	return deviceBridgeUnsupportedParameterType
}

func deviceBridgeParameterTypeSupported(typ mecom.DataType) bool {
	switch typ {
	case mecom.DataTypeFloat32, mecom.DataTypeInt32, mecom.DataTypeLatin1:
		return true
	default:
		return false
	}
}

var defaultDeviceBridgeWritableParameters = buildDefaultDeviceBridgeWritableParameters()

func buildDefaultDeviceBridgeWritableParameters() map[int]bool {
	out := map[int]bool{}
	for id, writable := range mecom.CANopenCompatibilityParameterWritability() {
		out[id] = writable
	}
	return out
}

var defaultDeviceBridgeCacheBehaviors = buildDefaultDeviceBridgeCacheBehaviors()

func buildDefaultDeviceBridgeCacheBehaviors() map[int]string {
	out := map[int]string{}
	for id, behavior := range mecom.CANopenCompatibilityParameterCacheBehaviors() {
		behavior = strings.ToLower(strings.TrimSpace(behavior))
		if behavior != "" {
			out[id] = behavior
		}
	}
	return out
}

var defaultDeviceBridgeParameterMaxInstances = buildDefaultDeviceBridgeParameterMaxInstances()

func buildDefaultDeviceBridgeParameterMaxInstances() map[int]byte {
	out := map[int]byte{}
	for id, maxInstances := range mecom.CANopenCompatibilityParameterMaxInstances() {
		if maxInstances <= 0 || maxInstances > 0xff {
			continue
		}
		out[id] = byte(maxInstances)
	}
	return out
}
