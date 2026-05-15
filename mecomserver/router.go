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
	Routes         []Route
	RequestTimeout time.Duration
	ReconnectDelay time.Duration
	Logger         *log.Logger
}

// ListenAndServeRouter listens on listenAddr and routes MeCom request frames
// by their device address byte.
func ListenAndServeRouter(ctx context.Context, listenAddr string, cfg RouterConfig) error {
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
func ServeRouter(ctx context.Context, ln net.Listener, cfg RouterConfig) error {
	if ln == nil {
		return fmt.Errorf("mecomserver: listener required")
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
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
		go handleRoutedClient(ctx, conn, routes, cfg)
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

func prepareRoutes(ctx context.Context, cfg RouterConfig) (map[byte]chan request, error) {
	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("mecomserver: at least one route required")
	}
	routes := make(map[byte]chan request, len(cfg.Routes))
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
		routeCfg := Config{
			Downstream:     route.Downstream,
			RequestTimeout: cfg.RequestTimeout,
			ReconnectDelay: cfg.ReconnectDelay,
			Logger:         cfg.Logger,
		}
		go runBroker(ctx, routeCfg, requests)
	}
	return routes, nil
}

func handleRoutedClient(ctx context.Context, conn net.Conn, routes map[byte]chan request, cfg RouterConfig) {
	handleClientWithSelector(ctx, conn, cfg.Logger, func(frame []byte) (chan<- request, error) {
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
