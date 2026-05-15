// mecomgw is the HTTP/WebSocket gateway that fronts Meerstetter devices for
// browser UIs and other JSON consumers. It wraps mecomvseriald (or direct
// device endpoints) with a stable JSON surface:
//
//	GET    /api/healthz              - liveness probe
//	GET    /api/devices              - list devices + connection status
//	GET    /api/catalogue            - mecomdict parameter catalogue
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
	"github.com/egidinas/meerstetter-go/tmtc"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	configPath := flag.String("config", "", "JSON config file with devices")
	defaultLeaseTTL := flag.Duration("default-lease-ttl", 5*time.Minute, "default lease TTL")
	uiDir := flag.String("ui-dir", "", "optional static UI directory served at /ui/")
	allowOrigin := flag.String("allow-origin", "", "comma-separated CORS origins for browser clients; use * only for isolated test gateways")
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
	Devices []DeviceConfig `json:"devices"`
}

// DeviceConfig describes one upstream device.
type DeviceConfig struct {
	ID       string `json:"id"`
	Endpoint string `json:"endpoint"`
	Address  byte   `json:"address"`
	Label    string `json:"label,omitempty"`
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
	for i, d := range cfg.Devices {
		if d.ID == "" || d.Endpoint == "" || d.Address == 0 {
			return Config{}, fmt.Errorf("device[%d]: id, endpoint, address required", i)
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
	logger          *log.Logger
	uiDir           string
	allowedOrigins  []string
}

type deviceBinding struct {
	cfg       DeviceConfig
	mu        sync.Mutex
	client    mecom.DeviceClient
	commander *mecom.Commander
	lastErr   error
}

func newServer(cfg Config, defaultTTL time.Duration, logger *log.Logger) *server {
	s := &server{
		devices:         make(map[string]*deviceBinding, len(cfg.Devices)),
		leases:          writelease.NewRegistry(),
		defaultLeaseTTL: defaultTTL,
		logger:          logger,
	}
	for _, dc := range cfg.Devices {
		s.devices[dc.ID] = &deviceBinding{cfg: dc}
	}
	return s
}

func (s *server) bind(id string) (*deviceBinding, error) {
	b, ok := s.devices[id]
	if !ok {
		return nil, fmt.Errorf("unknown device %q", id)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		return b, nil
	}
	ep, ok := mecom.ParseEndpoint(b.cfg.Endpoint)
	if !ok {
		return nil, fmt.Errorf("device %q: invalid endpoint %q", b.cfg.ID, b.cfg.Endpoint)
	}
	client, err := mecom.NewForEndpoint(context.Background(), ep, mecom.ClientConfig{
		Address: b.cfg.Address,
		Timeout: 2 * time.Second,
	}, socketCANDialer)
	if err != nil {
		b.lastErr = err
		return nil, err
	}
	b.client = client
	if writer, ok := client.(mecom.WriteClient); ok {
		cmdr := mecom.NewCommander(writer, 2*time.Second)
		cmdr.TargetID = b.cfg.ID
		cmdr.Authorizer = mecom.AuthorizerFunc(s.authorize)
		b.commander = cmdr
	}
	return b, nil
}

func (s *server) resetDeviceBinding(id string, cause error) {
	b, ok := s.devices[id]
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
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
