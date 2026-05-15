package mecomserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

// Route maps one MeCom device address to one downstream serial or TCP target.
type Route struct {
	Address    byte
	Target     string
	Downstream DownstreamDial
}

// RouterConfig configures a single TCP listener that routes addressed MeCom
// request frames to per-device downstream brokers.
type RouterConfig struct {
	Routes            []Route
	RequestTimeout    time.Duration
	ReconnectDelay    time.Duration
	ClientIdleTimeout time.Duration
	Logger            *log.Logger

	// stats is filled in by prepareRoutes so callers can retrieve a
	// connection-state snapshot per route via Stats().
	stats map[byte]*brokerStatsRecorder
}

// Stats returns a per-route snapshot of the broker connection state.
// Returns nil before ServeRouter/ListenAndServeRouter has been called.
func (cfg *RouterConfig) Stats() map[byte]BrokerStats {
	if cfg == nil || cfg.stats == nil {
		return nil
	}
	out := make(map[byte]BrokerStats, len(cfg.stats))
	for addr, rec := range cfg.stats {
		out[addr] = rec.Snapshot()
	}
	return out
}

// ListenAndServeRouter listens on listenAddr and routes MeCom request frames
// by their device address byte. The cfg pointer is mutated to expose
// per-route BrokerStats via cfg.Stats() after the call begins.
func ListenAndServeRouter(ctx context.Context, listenAddr string, cfg *RouterConfig) error {
	if cfg == nil {
		return fmt.Errorf("mecomserver: RouterConfig required")
	}
	if strings.TrimSpace(listenAddr) == "" {
		return fmt.Errorf("mecomserver: listen address required")
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	return ServeRouter(ctx, ln, cfg)
}

// ServeRouter accepts many TCP clients on one listener and dispatches each
// addressed request to the configured downstream for that MeCom address.
// cfg is mutated to publish per-route stats via cfg.Stats().
func ServeRouter(ctx context.Context, ln net.Listener, cfg *RouterConfig) error {
	if cfg == nil {
		return fmt.Errorf("mecomserver: RouterConfig required")
	}
	if ln == nil {
		return fmt.Errorf("mecomserver: listener required")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}

	routes, err := prepareRoutes(ctx, cfg)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if cfg.Logger != nil {
				cfg.Logger.Printf("accept failed: %v", err)
			}
			continue
		}
		go handleRoutedClient(ctx, conn, routes, *cfg)
	}
}

// RequestAddress extracts the MeCom destination address from a request frame.
func RequestAddress(frame []byte) (byte, error) {
	frame = []byte(strings.TrimSpace(string(frame)))
	if len(frame) < 3 {
		return 0, fmt.Errorf("short MeCom frame")
	}
	if frame[0] != '#' {
		return 0, fmt.Errorf("not a MeCom request frame")
	}
	n, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid MeCom address %q", string(frame[1:3]))
	}
	return byte(n), nil
}

func prepareRoutes(ctx context.Context, cfg *RouterConfig) (map[byte]chan request, error) {
	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("mecomserver: at least one route required")
	}
	routes := make(map[byte]chan request, len(cfg.Routes))
	cfg.stats = make(map[byte]*brokerStatsRecorder, len(cfg.Routes))
	for _, route := range cfg.Routes {
		if route.Address == 0 {
			return nil, fmt.Errorf("mecomserver: route address 0 is reserved")
		}
		if _, exists := routes[route.Address]; exists {
			return nil, fmt.Errorf("mecomserver: duplicate route for address 0x%02X", route.Address)
		}
		if route.Downstream == nil {
			dial, err := DialTarget(route.Target)
			if err != nil {
				return nil, fmt.Errorf("route 0x%02X: %w", route.Address, err)
			}
			route.Downstream = dial
		}
		requests := make(chan request, 256)
		routes[route.Address] = requests
		recorder := newBrokerStatsRecorder(route.Address, route.Target)
		cfg.stats[route.Address] = recorder
		routeCfg := Config{
			Downstream:        route.Downstream,
			RequestTimeout:    cfg.RequestTimeout,
			ReconnectDelay:    cfg.ReconnectDelay,
			ClientIdleTimeout: cfg.ClientIdleTimeout,
			Logger:            cfg.Logger,
			statsRecorder:     recorder,
		}
		go runBroker(ctx, routeCfg, requests)
	}
	return routes, nil
}

func handleRoutedClient(ctx context.Context, conn net.Conn, routes map[byte]chan request, cfg RouterConfig) {
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}
	handleClientWithSelector(ctx, conn, cfg.Logger, cfg.ClientIdleTimeout, func(frame []byte) (chan<- request, error) {
		addr, err := RequestAddress(frame)
		if err != nil {
			return nil, err
		}
		requests, ok := routes[addr]
		if !ok {
			return nil, fmt.Errorf("no downstream route for MeCom address 0x%02X", addr)
		}
		return requests, nil
	})
}
