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
	"net/http"
	"os"
	"os/signal"
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
	ID           string          `json:"id"`
	Endpoint     string          `json:"endpoint"`
	Address      byte            `json:"address"`
	Label        string          `json:"label,omitempty"`
	ChannelCount int             `json:"channel_count,omitempty"`
	Routes       []RouteConfig   `json:"routes,omitempty"`
	Channels     []ChannelConfig `json:"channels,omitempty"`
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
	devices         map[string]*deviceBinding
	leases          *writelease.Registry
	defaultLeaseTTL time.Duration
	channelCount    int
	defaultDeviceID string
	commandLogMu    sync.Mutex
	commandLog      []gatewayCommandActivity
	graphHistoryMu      sync.Mutex
	graphHistoryRaw     map[string]*graphTileHistory
	graphHistoryDerived map[string]*graphTileHistory
	logger              *log.Logger
	uiDir           string
	allowedOrigins  []string
	accessToken     string
	proxyBasePort   int
}

type deviceBinding struct {
	cfg       DeviceConfig
	mu        sync.Mutex
	client    mecom.DeviceClient
	commander *mecom.Commander
	lastErr   error
	proxy     *mecom.ProxyServer
	ring      *mecom.RingReader
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
	}
	for _, dc := range cfg.Devices {
		if dc.ChannelCount <= 0 {
			dc.ChannelCount = channelCount
		}
		s.devices[dc.ID] = &deviceBinding{cfg: dc}
	}
	return s
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
	ep, ok := mecom.ParseEndpoint(b.cfg.Endpoint)
	if !ok {
		return deviceBindingSnapshot{}, fmt.Errorf("device %q: invalid endpoint %q", b.cfg.ID, b.cfg.Endpoint)
	}
	client, err := mecom.NewForEndpoint(context.Background(), ep, mecom.ClientConfig{
		Address: b.cfg.Address,
		Timeout: 2 * time.Second,
	}, socketCANDialer)
	if err != nil {
		b.lastErr = err
		return deviceBindingSnapshot{}, err
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
	if b.ring == nil {
		// Example: auto-oversample critical parameters if they exist
		// For now, just a placeholder for future dynamic config
		// b.ring = mecom.NewRingReader(client.(*mecom.Client), criticalParams, s.rawSamplesChan)
		// b.ring.Start(context.Background())
	}

	return deviceBindingSnapshot{binding: b, client: b.client, commander: b.commander}, nil
}

func (s *server) deviceIndex(id string) int {
	// This should be stable based on config order
	// For now, just find it in devices map (need a stable way though)
	// I'll add device_index to deviceBinding in a future step if needed.
	return 0 
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
