// mecomgw is the HTTP/WebSocket gateway that fronts Meerstetter devices for
// browser UIs and other JSON consumers. It wraps mecomvseriald (or direct
// device endpoints) with a stable JSON surface:
//
//	GET    /api/healthz              - liveness probe
//	GET    /api/devices              - list devices + connection status
//	GET    /api/catalogue            - mecomdict parameter catalogue
//	GET    /api/commands             - recent write attempts and outcomes
//	POST   /api/devices/{id}/lease   - acquire a write lease
//	DELETE /api/devices/{id}/lease   - release a write lease
//	POST   /api/devices/{id}/write   - send a tmtc.Telecommand (requires lease)
//	GET    /api/devices/{id}/poll    - Server-Sent Events stream of Telemetry
//	GET    /api/devices/{id}/read    - one-shot bulk read of given params
//
// Devices are configured via a JSON file (-config). See
// deploy/example-gateway.json for the schema.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecom/writelease"
	tmtc "github.com/egidinas/signalforge/contracts"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	configPath := flag.String("config", "", "JSON config file with devices")
	defaultLeaseTTL := flag.Duration("default-lease-ttl", 5*time.Minute, "default lease TTL")
	uiDir := flag.String("ui-dir", "", "optional static UI directory served at /ui/")
	allowOrigin := flag.String("allow-origin", "", "comma-separated CORS origins for browser clients; use * only for isolated test gateways")
	proxyBasePort := flag.Int("proxy-base-port", 0, "base TCP port for per-device MeCom proxies (0 = disabled)")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mecomgw: %v\n", err)
		os.Exit(2)
	}
	if len(cfg.Devices) == 0 {
		fmt.Fprintln(os.Stderr, "mecomgw: no devices configured")
		os.Exit(2)
	}

	logger := log.New(os.Stderr, "mecomgw ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	srv := newServer(cfg, *defaultLeaseTTL, logger)
	srv.uiDir = *uiDir
	srv.allowedOrigins = parseCSV(*allowOrigin)
	srv.proxyBasePort = *proxyBasePort
	srv.accessToken = strings.TrimSpace(os.Getenv("MECOMGW_ACCESS_TOKEN"))
	if srv.accessToken != "" {
		logger.Printf("access token gate enabled")
	}

	go srv.derivationWorker(ctx)
	go srv.powerControlWorker(ctx)

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()
	logger.Printf("listen=%s devices=%d", *listen, len(cfg.Devices))
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("serve: %v", err)
	}
}

// Config is the on-disk JSON configuration.
type Config struct {
	Devices         []DeviceConfig `json:"devices"`
	ChannelCount    int            `json:"channel_count,omitempty"`
	DefaultDeviceID string         `json:"default_device_id,omitempty"`
}

// DeviceConfig describes one upstream device.
type DeviceConfig struct {
	ID                  string          `json:"id"`
	Endpoint            string          `json:"endpoint"`
	Address             byte            `json:"address"`
	Label               string          `json:"label,omitempty"`
	ChannelCount        int             `json:"channel_count,omitempty"`
	PowerControlEnabled bool            `json:"third_party_power_control_enabled,omitempty"`
	Routes              []RouteConfig   `json:"routes,omitempty"`
	Channels            []ChannelConfig `json:"channels,omitempty"`
}

// RouteConfig describes an alternate transport endpoint for a device.
type RouteConfig struct {
	Name      string `json:"name,omitempty"`
	Role      string `json:"role,omitempty"`
	Endpoint  string `json:"endpoint"`
	Transport string `json:"transport,omitempty"`
	State     string `json:"state,omitempty"`
}

// ChannelConfig carries operator-facing fixture metadata for one channel.
type ChannelConfig struct {
	Instance   int    `json:"instance"`
	Role       string `json:"role,omitempty"`
	Label      string `json:"label,omitempty"`
	UserNote   string `json:"user_note,omitempty"`
	HasCascade bool   `json:"has_cascade,omitempty"`
}

func deviceChannelConfig(cfg DeviceConfig, instance int) (ChannelConfig, bool) {
	for _, ch := range cfg.Channels {
		if ch.Instance == instance {
			return ch, true
		}
	}
	return ChannelConfig{}, false
}

func defaultMeerstetterChannelRole(instance int) string {
	if instance%2 == 0 {
		return "supply"
	}
	return "temp"
}

func effectiveDeviceChannelRole(cfg DeviceConfig, instance int) (string, string) {
	if ch, ok := deviceChannelConfig(cfg, instance); ok {
		if role := strings.ToLower(strings.TrimSpace(ch.Role)); role != "" {
			return role, "config"
		}
	}
	return defaultMeerstetterChannelRole(instance), "gateway-default"
}

func loadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, fmt.Errorf("-config required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if cfg.ChannelCount < 0 || cfg.ChannelCount > 255 {
		return Config{}, fmt.Errorf("channel_count must be 0/default or in range 1..255")
	}
	if cfg.ChannelCount == 0 {
		cfg.ChannelCount = 4
	}
	if cfg.DefaultDeviceID != "" {
		found := false
		for _, d := range cfg.Devices {
			if d.ID == cfg.DefaultDeviceID {
				found = true
				break
			}
		}
		if !found {
			return Config{}, fmt.Errorf("default_device_id %q not found in devices", cfg.DefaultDeviceID)
		}
	}
	seen := make(map[string]int, len(cfg.Devices))
	for i, d := range cfg.Devices {
		if d.ID == "" || d.Endpoint == "" || d.Address == 0 {
			return Config{}, fmt.Errorf("device[%d]: id, endpoint, address required", i)
		}
		if prev, ok := seen[d.ID]; ok {
			return Config{}, fmt.Errorf("device[%d]: duplicate id %q already used at device[%d]", i, d.ID, prev)
		}
		seen[d.ID] = i
		if d.ChannelCount < 0 || d.ChannelCount > 255 {
			return Config{}, fmt.Errorf("device[%d]: channel_count must be 0/default or in range 1..255", i)
		}
		if d.ChannelCount == 0 {
			cfg.Devices[i].ChannelCount = cfg.ChannelCount
		}
		if len(d.Routes) > 0 {
			seenRouteRoles := map[string]struct{}{}
			for j, route := range d.Routes {
				if strings.TrimSpace(route.Endpoint) == "" {
					return Config{}, fmt.Errorf("device[%d].routes[%d]: endpoint required", i, j)
				}
				if strings.TrimSpace(route.Role) == "" {
					return Config{}, fmt.Errorf("device[%d].routes[%d]: role required", i, j)
				}
				role := strings.ToLower(strings.TrimSpace(route.Role))
				switch role {
				case "hot", "warm", "fallback":
				default:
					return Config{}, fmt.Errorf("device[%d].routes[%d]: role must be hot, warm, or fallback", i, j)
				}
				if _, ok := seenRouteRoles[role]; ok {
					return Config{}, fmt.Errorf("device[%d].routes[%d]: duplicate role %q", i, j, role)
				}
				seenRouteRoles[role] = struct{}{}
				cfg.Devices[i].Routes[j].Role = role
				cfg.Devices[i].Routes[j].Name = strings.TrimSpace(route.Name)
				cfg.Devices[i].Routes[j].Endpoint = strings.TrimSpace(route.Endpoint)
				cfg.Devices[i].Routes[j].Transport = strings.TrimSpace(route.Transport)
				cfg.Devices[i].Routes[j].State = strings.TrimSpace(route.State)
			}
		}
		for j, ch := range d.Channels {
			if ch.Instance < 1 || ch.Instance > cfg.Devices[i].ChannelCount {
				return Config{}, fmt.Errorf("device[%d].channels[%d]: instance must be in range 1..channel_count", i, j)
			}
			switch strings.ToLower(strings.TrimSpace(ch.Role)) {
			case "", "temp", "supply":
				cfg.Devices[i].Channels[j].Role = strings.ToLower(strings.TrimSpace(ch.Role))
			default:
				return Config{}, fmt.Errorf("device[%d].channels[%d]: role must be temp or supply", i, j)
			}
		}
	}
	return cfg, nil
}

// server holds gateway runtime state. Device clients are opened lazily and
// reset after transport failures so the next request can reconnect.
type server struct {
	devices             map[string]*deviceBinding
	leases              *writelease.Registry
	defaultLeaseTTL     time.Duration
	channelCount        int
	defaultDeviceID     string
	commandLogMu        sync.Mutex
	commandLog          []gatewayCommandActivity
	graphHistoryMu      sync.Mutex
	graphHistoryRaw     map[string]*graphTileHistory
	graphHistoryDerived map[string]*graphTileHistory
	logger              *log.Logger
	uiDir               string
	allowedOrigins      []string
	accessToken         string
	proxyBasePort       int
	virtualParamsMu     sync.Mutex
	virtualParams       map[string]float64
}

type powerControlSample struct {
	v float64
	i float64
}

type deviceBinding struct {
	cfg          DeviceConfig
	index        int
	mu           sync.Mutex
	client       mecom.DeviceClient
	commander    *mecom.Commander
	lastErr      error
	proxy        *mecom.ProxyServer
	ring         *mecom.RingReader
	powerHistory map[int][]powerControlSample
	samplesChan  chan mecom.RingSample
	ringStop     chan struct{}
	latestV      map[int]float64
	latestI      map[int]float64
	latestVTime  map[int]time.Time
	latestITime  map[int]time.Time
}

type deviceBindingSnapshot struct {
	binding   *deviceBinding
	client    mecom.DeviceClient
	commander *mecom.Commander
}

var errUnknownGatewayDevice = errors.New("unknown device")

func newServer(cfg Config, defaultTTL time.Duration, logger *log.Logger) *server {
	channelCount := cfg.ChannelCount
	if channelCount <= 0 {
		channelCount = 4
	}
	s := &server{
		devices:             make(map[string]*deviceBinding, len(cfg.Devices)),
		leases:              writelease.NewRegistry(),
		defaultLeaseTTL:     defaultTTL,
		channelCount:        channelCount,
		defaultDeviceID:     cfg.DefaultDeviceID,
		graphHistoryRaw:     make(map[string]*graphTileHistory),
		graphHistoryDerived: make(map[string]*graphTileHistory),
		logger:              logger,
		virtualParams:       make(map[string]float64),
	}
	for _, dc := range cfg.Devices {
		if dc.ChannelCount <= 0 {
			dc.ChannelCount = channelCount
		}
		s.devices[dc.ID] = &deviceBinding{
			cfg:          dc,
			index:        len(s.devices),
			powerHistory: make(map[int][]powerControlSample),
			latestV:      make(map[int]float64),
			latestI:      make(map[int]float64),
			latestVTime:  make(map[int]time.Time),
			latestITime:  make(map[int]time.Time),
		}
	}
	return s
}

func (s *server) getVirtualParam(deviceID string, paramID, instance int, def float64) float64 {
	s.virtualParamsMu.Lock()
	defer s.virtualParamsMu.Unlock()
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	if val, ok := s.virtualParams[key]; ok {
		return val
	}
	return def
}

func (s *server) setVirtualParam(deviceID string, paramID, instance int, val float64) {
	s.virtualParamsMu.Lock()
	defer s.virtualParamsMu.Unlock()
	if s.virtualParams == nil {
		s.virtualParams = make(map[string]float64)
	}
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.virtualParams[key] = val
}

func (s *server) bind(id string) (deviceBindingSnapshot, error) {
	b, ok := s.devices[id]
	if !ok {
		return deviceBindingSnapshot{}, fmt.Errorf("%w %q", errUnknownGatewayDevice, id)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		return deviceBindingSnapshot{binding: b, client: b.client, commander: b.commander}, nil
	}
	endpointsToTry := append([]string{b.cfg.Endpoint}, func() []string {
		var list []string
		for _, r := range b.cfg.Routes {
			if r.Endpoint != "" {
				list = append(list, r.Endpoint)
			}
		}
		return list
	}()...)

	var client mecom.DeviceClient
	var err error
	var chosenEndpoint string

	for _, endpoint := range endpointsToTry {
		ep, ok := mecom.ParseEndpoint(endpoint)
		if !ok {
			err = fmt.Errorf("device %q: invalid endpoint %q", b.cfg.ID, endpoint)
			continue
		}
		client, err = mecom.NewForEndpoint(context.Background(), ep, mecom.ClientConfig{
			Address: b.cfg.Address,
			Timeout: 2 * time.Second,
		}, socketCANDialer)
		if err == nil && client != nil {
			chosenEndpoint = endpoint
			break
		}
	}

	if err != nil || client == nil {
		b.lastErr = err
		if err == nil {
			err = fmt.Errorf("failed to connect to all endpoints for device %q", id)
		}
		return deviceBindingSnapshot{}, err
	}

	if chosenEndpoint != b.cfg.Endpoint {
		s.logger.Printf("device %q: primary endpoint failed; fell back to redundant route %q", b.cfg.ID, chosenEndpoint)
	}

	b.client = client
	if writer, ok := client.(mecom.WriteClient); ok {
		cmdr := mecom.NewCommander(writer, 2*time.Second)
		cmdr.TargetID = b.cfg.ID
		cmdr.Authorizer = mecom.AuthorizerFunc(s.authorize)
		b.commander = cmdr
	}

	// Start Proxy if enabled
	if s.proxyBasePort > 0 && b.proxy == nil {
		if mac, ok := client.(mecom.MeComASCIIClient); ok {
			port := s.proxyBasePort + s.deviceIndex(b.cfg.ID)
			b.proxy = mecom.NewProxyServer(fmt.Sprintf(":%d", port), mac.MeComClient())
			if err := b.proxy.Start(); err != nil {
				s.logger.Printf("device %q: proxy start failed: %v", b.cfg.ID, err)
			} else {
				s.logger.Printf("device %q: proxy listening on :%d", b.cfg.ID, port)
			}
		}
	}

	// Start RingReader if not already started
	if b.ring == nil && b.cfg.PowerControlEnabled {
		if mac, ok := client.(mecom.MeComASCIIClient); ok {
			mc := mac.MeComClient()
			var criticalParams []mecom.RingCaptureParameter
			for ch := 1; ch <= b.cfg.ChannelCount; ch++ {
				criticalParams = append(criticalParams, mecom.RingCaptureParameter{
					Parameter: mecom.Parameter{ID: 1021, Instance: ch, Type: mecom.DataTypeFloat32},
				})
				criticalParams = append(criticalParams, mecom.RingCaptureParameter{
					Parameter: mecom.Parameter{ID: 1020, Instance: ch, Type: mecom.DataTypeFloat32},
				})
			}
			b.samplesChan = make(chan mecom.RingSample, 1000)
			b.ringStop = make(chan struct{})
			b.ring = mecom.NewRingReader(mc, criticalParams, b.samplesChan)
			if errRing := b.ring.Start(context.Background()); errRing != nil {
				s.logger.Printf("device %q: failed to start RingReader: %v", b.cfg.ID, errRing)
				b.ring = nil
				b.samplesChan = nil
				close(b.ringStop)
				b.ringStop = nil
			} else {
				s.logger.Printf("device %q: started RingReader for oversampling", b.cfg.ID)
				go s.runDeviceRingReceiver(b.cfg.ID, b, b.samplesChan, b.ringStop)
			}
		}
	}

	return deviceBindingSnapshot{binding: b, client: b.client, commander: b.commander}, nil
}

func (s *server) deviceIndex(id string) int {
	if b, ok := s.devices[id]; ok {
		return b.index
	}
	return 0
}

func (s *server) runDeviceRingReceiver(deviceID string, bound *deviceBinding, ch <-chan mecom.RingSample, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case sample, ok := <-ch:
			if !ok {
				return
			}
			bound.mu.Lock()
			if bound.ring == nil {
				bound.mu.Unlock()
				return
			}
			if sample.ConfigIndex < 0 || sample.ConfigIndex >= len(bound.ring.Config) {
				bound.mu.Unlock()
				continue
			}
			p := bound.ring.Config[sample.ConfigIndex].Parameter
			inst := p.Instance
			val := sample.Value

			if p.ID == 1021 {
				bound.latestV[inst] = val
				bound.latestVTime[inst] = time.Now()
			} else if p.ID == 1020 {
				bound.latestI[inst] = val
				bound.latestITime[inst] = time.Now()
			}

			tThreshold := 100 * time.Millisecond
			if vTime, okV := bound.latestVTime[inst]; okV {
				if iTime, okI := bound.latestITime[inst]; okI {
					timeDiff := vTime.Sub(iTime)
					if timeDiff < 0 {
						timeDiff = -timeDiff
					}
					if timeDiff < tThreshold {
						v := bound.latestV[inst]
						i := bound.latestI[inst]
						h := bound.powerHistory[inst]
						h = append(h, powerControlSample{v: v, i: i})
						if len(h) > 5 {
							h = h[1:]
						}
						bound.powerHistory[inst] = h
						delete(bound.latestVTime, inst)
						delete(bound.latestITime, inst)
					}
				}
			}
			bound.mu.Unlock()
		}
	}
}

func (s *server) resetDeviceBinding(id string, failed mecom.ReadClient, cause error) {
	b, ok := s.devices[id]
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if failed != nil && b.client != failed {
		return
	}
	if b.client != nil {
		_ = b.client.Close()
	}
	if b.proxy != nil {
		b.proxy.Stop()
		b.proxy = nil
	}
	if b.ring != nil {
		b.ring.Stop()
		b.ring = nil
	}
	if b.ringStop != nil {
		close(b.ringStop)
		b.ringStop = nil
	}
	b.samplesChan = nil
	b.powerHistory = make(map[int][]powerControlSample)
	b.latestV = make(map[int]float64)
	b.latestI = make(map[int]float64)
	b.latestVTime = make(map[int]time.Time)
	b.latestITime = make(map[int]time.Time)
	b.client = nil
	b.commander = nil
	b.lastErr = cause
}

func shouldResetDeviceBinding(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, mecom.ErrUnreachable), errors.Is(err, mecom.ErrTimeout):
		return true
	case errors.Is(err, mecom.ErrUnknownParameter),
		errors.Is(err, mecom.ErrParameterReadOnly),
		errors.Is(err, mecom.ErrWriteRejected),
		errors.Is(err, mecom.ErrTransportNotSupported),
		errors.Is(err, mecom.ErrBadAddress),
		errors.Is(err, mecom.ErrInvalidArgument):
		return false
	default:
		return true
	}
}

func (s *server) authorize(targetID string, tc tmtc.Telecommand) error {
	token := ""
	if tc.Metadata != nil {
		token = strings.TrimSpace(tc.Metadata["lease_token"])
	}
	if token == "" {
		return fmt.Errorf("write requires Metadata[\"lease_token\"]")
	}
	return s.leases.Validate(targetID, token)
}

func parseCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func (s *server) powerControlWorker(ctx context.Context) {
	for deviceID, bound := range s.devices {
		deviceID := deviceID
		bound := bound
		go s.powerControlDeviceLoop(ctx, deviceID, bound)
	}
}

func (s *server) powerControlDeviceLoop(ctx context.Context, deviceID string, bound *deviceBinding) {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("panic recovered in power control for device %s: %v", deviceID, r)
					}
				}()
				s.runDevicePowerControl(ctx, deviceID, bound)
			}()
		}
	}
}

func smoothSamples(h []powerControlSample) (float64, float64, bool) {
	n := len(h)
	if n == 0 {
		return 0, 0, false
	}
	if n < 3 {
		latest := h[n-1]
		return latest.v, latest.i, true
	}

	vSlices := make([]float64, n)
	iSlices := make([]float64, n)
	for idx, s := range h {
		vSlices[idx] = s.v
		iSlices[idx] = s.i
	}

	sort.Float64s(vSlices)
	sort.Float64s(iSlices)

	// Trimmed Mean: Discard the minimum and maximum samples, average the remaining middle samples
	vSum := 0.0
	iSum := 0.0
	for idx := 1; idx < n-1; idx++ {
		vSum += vSlices[idx]
		iSum += iSlices[idx]
	}
	divisor := float64(n - 2)

	return vSum / divisor, iSum / divisor, true
}

func (s *server) runDevicePowerControl(ctx context.Context, deviceID string, bound *deviceBinding) {
	if bound == nil || !bound.cfg.PowerControlEnabled {
		return
	}
	snap, err := s.bind(deviceID)
	if err != nil || snap.client == nil {
		return
	}
	if !snap.binding.cfg.PowerControlEnabled {
		return
	}
	writer, ok := snap.client.(mecom.WriteClient)
	if !ok {
		return
	}

	bound.mu.Lock()
	historyCopy := make(map[int][]powerControlSample)
	for inst, samples := range bound.powerHistory {
		historyCopy[inst] = append([]powerControlSample(nil), samples...)
	}
	bound.mu.Unlock()

	for inst := 1; inst <= snap.binding.cfg.ChannelCount; inst++ {
		role, _ := effectiveDeviceChannelRole(snap.binding.cfg, inst)
		if role != "supply" {
			continue
		}
		s.runChannelPowerControl(ctx, deviceID, inst, snap.client, writer, historyCopy[inst])
	}
}

func (s *server) runChannelPowerControl(ctx context.Context, deviceID string, instance int, client mecom.ReadClient, writer mecom.WriteClient, h []powerControlSample) {
	pTarget := s.getVirtualParam(deviceID, 2035, instance, 0.0)

	var vMeas, iMeas float64
	vMeas, iMeas, ok := smoothSamples(h)
	if !ok {
		// Fallback to one-shot read (for unit tests / initial ticks)
		params := []mecom.Parameter{
			{ID: 1021, Instance: instance},
			{ID: 1020, Instance: instance},
		}
		readCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
		vals, errRead := client.ReadBulk(readCtx, params)
		cancel()
		if errRead != nil || len(vals) != 2 {
			return
		}
		vMeas = vals[0]
		iMeas = vals[1]
	}

	params := []mecom.Parameter{
		{ID: 2010, Instance: instance},
		{ID: 2021, Instance: instance},
		{ID: 2030, Instance: instance},
	}

	readCtx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	vals, err := client.ReadBulk(readCtx, params)
	cancel()
	if err != nil || len(vals) != 3 {
		return
	}

	outEnable := vals[0]
	vCmd := vals[1]
	iLim := vals[2]

	if outEnable != 1.0 {
		s.setVirtualParam(deviceID, 2036, instance, 0.0)
		s.recordGraphSample(deviceID, 2036, instance, 0.0, gatewayQualityNaN, time.Now())
		s.setPowerControlStatus(deviceID, instance, 0.0, gatewayQualityOK)
		s.setVirtualParam(deviceID, 2043, instance, 0.0)
		return
	}

	if iMeas < 0.05 {
		s.setVirtualParam(deviceID, 2036, instance, 0.0)
		s.recordGraphSample(deviceID, 2036, instance, 0.0, gatewayQualityNaN, time.Now())
		s.setPowerControlStatus(deviceID, instance, 0.0, gatewayQualityOK)
		s.setVirtualParam(deviceID, 2043, instance, 0.0)
		return
	}

	rRaw := vMeas / iMeas
	rCandidate := rRaw

	rPrev := s.getVirtualParam(deviceID, 2037, instance, 0.0)
	if rPrev > 0.0 {
		// Low-pass filter (80% previous state, 20% raw sample)
		rCandidate = 0.8*rPrev + 0.2*rRaw

		// Deadband check: if change in resistance is less than 1.5%, reject as measurement noise
		if math.Abs(rCandidate-rPrev)/rPrev < 0.015 {
			rCandidate = rPrev
		}
	}

	isSampleNonOhmic := false
	if rRaw < 0.1 || rRaw > 1000.0 {
		isSampleNonOhmic = true
	} else if rPrev > 0.0 {
		ratio := rRaw / rPrev
		if ratio < 0.5 || ratio > 2.0 {
			isSampleNonOhmic = true
		}
	}

	nonOhmicCount := int(s.getVirtualParam(deviceID, 2043, instance, 0.0))
	if isSampleNonOhmic {
		nonOhmicCount++
		s.setVirtualParam(deviceID, 2043, instance, float64(nonOhmicCount))
		reportedR := rPrev
		if reportedR <= 0.0 {
			reportedR = rRaw
		}
		s.setVirtualParam(deviceID, 2036, instance, reportedR)
		s.recordGraphSample(deviceID, 2036, instance, reportedR, gatewayQualityNaN, time.Now())
	} else {
		nonOhmicCount = 0
		s.setVirtualParam(deviceID, 2043, instance, 0.0)
		s.setVirtualParam(deviceID, 2036, instance, rCandidate)
		s.recordGraphSample(deviceID, 2036, instance, rCandidate, gatewayQualityOK, time.Now())
		s.setVirtualParam(deviceID, 2037, instance, rCandidate)
	}

	var loopStatus float64 = 0.0
	if pTarget <= 0.0 {
		loopStatus = 0.0 // Standby
		s.setPowerControlStatus(deviceID, instance, loopStatus, gatewayQualityOK)
		return
	}

	if nonOhmicCount >= 3 {
		loopStatus = 2.0 // Fallback
		s.setPowerControlStatus(deviceID, instance, loopStatus, gatewayQualityOK)
		return
	}

	if isSampleNonOhmic {
		// Reject change (do not adjust control loop setpoints), but keep loop active with grace
		loopStatus = 1.0
		s.setPowerControlStatus(deviceID, instance, loopStatus, gatewayQualityOK)
		return
	}

	loopStatus = 1.0 // Active Closed Loop
	s.setPowerControlStatus(deviceID, instance, loopStatus, gatewayQualityOK)

	vTarget := math.Sqrt(pTarget * rCandidate)
	iTarget := math.Sqrt(pTarget / rCandidate)

	if vTarget > 80.0 {
		vTarget = 80.0
	}
	if vTarget < 0.0 {
		vTarget = 0.0
	}
	if iTarget > 25.0 {
		iTarget = 25.0
	}
	if iTarget < 0.0 {
		iTarget = 0.0
	}

	if math.IsNaN(vTarget) || math.IsNaN(iTarget) {
		return
	}

	// Verify if the last written targets match the device's actual setpoints
	vLastSent := s.getVirtualParam(deviceID, 2041, instance, vCmd)
	iLastSent := s.getVirtualParam(deviceID, 2042, instance, iLim)

	if math.Abs(vCmd-vLastSent) > 0.05 {
		s.logger.Printf("power control device %s ch %d: discrepancy detected (Vcmd actual %.3f V != expected %.3f V). Last write may have failed or was overridden. Re-aligning control tracking.", deviceID, instance, vCmd, vLastSent)
	}
	if math.Abs(iLim-iLastSent) > 0.05 {
		s.logger.Printf("power control device %s ch %d: discrepancy detected (Ilim actual %.3f A != expected %.3f A). Last write may have failed or was overridden. Re-aligning control tracking.", deviceID, instance, iLim, iLastSent)
	}

	alpha := 0.4
	vNext := vCmd + alpha*(vTarget-vCmd)
	vDiff := vNext - vCmd
	if vDiff > 5.0 {
		vNext = vCmd + 5.0
	} else if vDiff < -5.0 {
		vNext = vCmd - 5.0
	}

	iTargetWithHeadroom := iTarget * 1.2
	if iTargetWithHeadroom > 25.0 {
		iTargetWithHeadroom = 25.0
	}
	iNext := iLim + alpha*(iTargetWithHeadroom-iLim)
	iDiff := iNext - iLim
	if iDiff > 2.0 {
		iNext = iLim + 2.0
	} else if iDiff < -2.0 {
		iNext = iLim - 2.0
	}

	writeCtx, writeCancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer writeCancel()

	errV := writer.WriteFloat32(writeCtx, 2021, instance, float32(vNext))
	s.recordPowerControlWriteActivity(deviceID, instance, 2021, vNext, errV)
	if errV == nil {
		s.setVirtualParam(deviceID, 2041, instance, vNext)
	} else {
		s.logger.Printf("power control device %s ch %d: write target voltage failed: %v", deviceID, instance, errV)
	}

	errI := writer.WriteFloat32(writeCtx, 2030, instance, float32(iNext))
	s.recordPowerControlWriteActivity(deviceID, instance, 2030, iNext, errI)
	if errI == nil {
		s.setVirtualParam(deviceID, 2042, instance, iNext)
	} else {
		s.logger.Printf("power control device %s ch %d: write current limit failed: %v", deviceID, instance, errI)
	}
	if errV != nil || errI != nil {
		s.setPowerControlStatus(deviceID, instance, 2.0, gatewayQualityOK)
		if shouldResetDeviceBinding(errV) {
			s.resetDeviceBinding(deviceID, client, errV)
		} else if shouldResetDeviceBinding(errI) {
			s.resetDeviceBinding(deviceID, client, errI)
		}
	}
}

func (s *server) setPowerControlStatus(deviceID string, instance int, status float64, quality string) {
	s.setVirtualParam(deviceID, 2038, instance, status)
	s.recordGraphSample(deviceID, 2038, instance, status, quality, time.Now())
}

func (s *server) recordPowerControlWriteActivity(deviceID string, instance int, paramID int, value float64, err error) {
	activity := gatewayCommandActivity{
		Time:           time.Now(),
		TargetID:       deviceID,
		DeviceID:       deviceID,
		ParamID:        paramID,
		Instance:       instance,
		RequestedValue: value,
		Status:         "completed",
		Transport:      "mecom",
		LeaseHolder:    "third-party-power-control",
		HTTPStatus:     http.StatusOK,
	}
	if param, ok := gatewayParameterByID(paramID); ok {
		activity.SignalName = param.Name
		activity.SignalUnit = param.Unit
	}
	if err != nil {
		activity.Status = "failed"
		activity.Error = err.Error()
		activity.ErrorCategory = gatewayCommandErrorCategory(err)
		activity.HTTPStatus = http.StatusBadGateway
	}
	s.recordCommandActivity(activity)
}
