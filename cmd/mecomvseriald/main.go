// mecomvseriald exposes one LAN TCP listener for addressed MeCom serial
// devices and routes each request by the device address in the MeCom frame.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecomserver"
)

type routeFlags []mecomserver.Route

func (r *routeFlags) String() string {
	parts := make([]string, 0, len(*r))
	for _, route := range *r {
		parts = append(parts, fmt.Sprintf("0x%02X=%s", route.Address, route.Target))
	}
	return strings.Join(parts, ",")
}

func (r routeFlags) addresses() []byte {
	out := make([]byte, 0, len(r))
	for _, route := range r {
		out = append(out, route.Address)
	}
	return out
}

func (r *routeFlags) Set(v string) error {
	eq := strings.Index(v, "=")
	if eq < 0 {
		return fmt.Errorf("route must be address=target")
	}
	addrText := strings.TrimSpace(v[:eq])
	target := strings.TrimSpace(v[eq+1:])
	if target == "" {
		return fmt.Errorf("route target required")
	}
	ep, ok := mecom.ParseTarget(target)
	if !ok {
		return fmt.Errorf("invalid route target %q", target)
	}
	if ep.Network == "can" {
		return fmt.Errorf("route target %q uses CAN; mecomvseriald only routes serial/TCP downstreams and needs a typed CAN bridge adapter", target)
	}
	addr, err := parseAddress(addrText)
	if err != nil {
		return err
	}
	*r = append(*r, mecomserver.Route{Address: addr, Target: target})
	return nil
}

func main() {
	var routes routeFlags
	listen := flag.String("listen", "127.0.0.1:50000", "TCP listen address")
	target := flag.String("target", "", "single downstream target for address-agnostic passthrough, e.g. serial:/dev/ttyUSB0@57600")
	addressZeroFlag := flag.String("address-zero", "disabled", "route client requests addressed to 0: disabled, route-order, or a configured fixed device address")
	timeout := flag.Duration("timeout", 2*time.Second, "per-request downstream timeout")
	reconnectDelay := flag.Duration("reconnect-delay", 500*time.Millisecond, "delay after downstream dial failures")
	traceFrames := flag.Bool("trace-frames", false, "log client and downstream frame bytes for short diagnostic captures")
	flag.Var(&routes, "route", "address=target route; repeatable, e.g. -route 75=serial:/dev/ttyUSB0@57600")
	flag.Parse()

	addressZero, err := parseAddressZeroMode(*addressZeroFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	mode, err := selectServerMode(*target, routes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if (addressZero.fixed != 0 || addressZero.routeOrder) && mode.target != "" {
		fmt.Fprintln(os.Stderr, "-address-zero requires routed -route mode")
		os.Exit(2)
	}

	logger := log.New(os.Stderr, "mecomvseriald ", log.LstdFlags|log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if mode.target != "" {
		logger.Printf("listen=%s target=%s", *listen, mode.target)
		err = mecomserver.ListenAndServe(ctx, *listen, mecomserver.Config{
			Target:         mode.target,
			RequestTimeout: *timeout,
			ReconnectDelay: *reconnectDelay,
			TraceFrames:    *traceFrames,
			Logger:         logger,
		})
		if err != nil {
			logger.Printf("server failed: %v", err)
			os.Exit(1)
		}
		return
	}

	addressZeroOrder := []byte(nil)
	if addressZero.routeOrder {
		addressZeroOrder = routes.addresses()
	}
	logger.Printf("listen=%s routes=%s address-zero=%s", *listen, routes.String(), addressZero.String())
	cfg := &mecomserver.RouterConfig{
		Routes:           routes,
		DefaultAddress:   addressZero.fixed,
		AddressZeroOrder: addressZeroOrder,
		RequestTimeout:   *timeout,
		ReconnectDelay:   *reconnectDelay,
		TraceFrames:      *traceFrames,
		Logger:           logger,
	}
	err = mecomserver.ListenAndServeRouter(ctx, *listen, cfg)
	if err != nil {
		logger.Printf("server failed: %v", err)
		os.Exit(1)
	}
}

type serverMode struct {
	target string
	routes routeFlags
}

func selectServerMode(target string, routes routeFlags) (serverMode, error) {
	target = strings.TrimSpace(target)
	if target != "" {
		if len(routes) > 0 {
			return serverMode{}, fmt.Errorf("-target and -route are mutually exclusive")
		}
		ep, ok := mecom.ParseTarget(target)
		if !ok {
			return serverMode{}, fmt.Errorf("invalid target %q", target)
		}
		if ep.Network == "can" {
			return serverMode{}, fmt.Errorf("target %q uses CAN; mecomvseriald only exposes serial/TCP downstreams and needs a typed CAN bridge adapter", target)
		}
		return serverMode{target: target}, nil
	}
	if len(routes) == 0 {
		return serverMode{}, fmt.Errorf("at least one -route or one -target is required")
	}
	return serverMode{routes: routes}, nil
}

func parseAddress(v string) (byte, error) {
	n, err := strconv.ParseUint(v, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid address %q: %w", v, err)
	}
	if n == 0 {
		return 0, fmt.Errorf("address 0 is reserved")
	}
	if n > 254 {
		return 0, fmt.Errorf("address %d outside MeCom 1..254", n)
	}
	return byte(n), nil
}

type addressZeroMode struct {
	fixed      byte
	routeOrder bool
}

func (m addressZeroMode) String() string {
	if m.routeOrder {
		return "route-order"
	}
	if m.fixed != 0 {
		return fmt.Sprintf("0x%02X", m.fixed)
	}
	return "disabled"
}

func parseAddressZeroMode(v string) (addressZeroMode, error) {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "0", "disabled", "off", "none":
		return addressZeroMode{}, nil
	case "route-order", "routes", "round-robin":
		return addressZeroMode{routeOrder: true}, nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v), 0, 8)
	if err != nil {
		return addressZeroMode{}, fmt.Errorf("invalid address-zero mode %q: use disabled, route-order, or a MeCom address: %w", v, err)
	}
	if n == 0 {
		return addressZeroMode{}, nil
	}
	if n > 254 {
		return addressZeroMode{}, fmt.Errorf("address-zero default address %d outside MeCom 0..254", n)
	}
	return addressZeroMode{fixed: byte(n)}, nil
}
