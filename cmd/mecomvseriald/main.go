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
	seen := make(map[byte]struct{}, len(r))
	for _, route := range r {
		if _, ok := seen[route.Address]; ok {
			continue
		}
		seen[route.Address] = struct{}{}
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
	if _, err := parseDaemonTarget(target); err != nil {
		return fmt.Errorf("invalid route target %q: %w", target, err)
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
	addressZeroFlag := flag.String("address-zero", "disabled", "route client requests addressed to 0: disabled, auto-first, route-order, or a configured fixed device address")
	routePolicyFlag := flag.String("route-policy", string(mecomserver.RouteSelectionFixedPreference), "duplicate-route policy: fixed-preference or dynamic")
	timeout := flag.Duration("timeout", 2*time.Second, "per-request downstream timeout")
	reconnectDelay := flag.Duration("reconnect-delay", 500*time.Millisecond, "delay after downstream dial failures")
	traceFrames := flag.Bool("trace-frames", false, "log client and downstream frame bytes for short diagnostic captures")
	deviceCacheDir := flag.String("device-cache-dir", os.Getenv("MECOM_DEVICE_CACHE_DIR"), "directory for per-device bridge cache JSON; empty disables")
	flag.Var(&routes, "route", "address=target route; repeatable, e.g. -route 75=serial:/dev/ttyUSB0@57600")
	flag.Parse()

	addressZero, err := parseAddressZeroMode(*addressZeroFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	routePolicy, err := parseRouteSelectionPolicy(*routePolicyFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	mode, err := selectServerMode(*target, routes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if (addressZero.fixed != 0 || addressZero.routeOrder || addressZero.autoFirst) && mode.target != "" {
		fmt.Fprintln(os.Stderr, "-address-zero requires routed -route mode")
		os.Exit(2)
	}

	logger := log.New(os.Stderr, "mecomvseriald ", log.LstdFlags|log.Lmicroseconds)
	restoreDeviceCacheDir := mecomserver.SetDeviceBridgeCacheDir(*deviceCacheDir)
	defer restoreDeviceCacheDir()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if mode.target != "" {
		downstream, err := downstreamForTarget(mode.target, *timeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		logger.Printf("listen=%s target=%s device-cache-dir=%s", *listen, mode.target, *deviceCacheDir)
		err = mecomserver.ListenAndServe(ctx, *listen, mecomserver.Config{
			Target:         mode.target,
			Downstream:     downstream,
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

	routes, err = prepareRouteDownstreams(routes, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defaultAddress, addressZeroOrder, err := resolveAddressZeroMode(addressZero, routes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	logger.Printf("listen=%s routes=%s address-zero=%s route-policy=%s device-cache-dir=%s", *listen, routes.String(), addressZero.String(), routePolicy, *deviceCacheDir)
	cfg := &mecomserver.RouterConfig{
		Routes:           routes,
		DefaultAddress:   defaultAddress,
		AddressZeroOrder: addressZeroOrder,
		RequestTimeout:   *timeout,
		ReconnectDelay:   *reconnectDelay,
		RouteSelection:   routePolicy,
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
		if _, err := parseDaemonTarget(target); err != nil {
			return serverMode{}, fmt.Errorf("invalid target %q: %w", target, err)
		}
		return serverMode{target: target}, nil
	}
	if len(routes) == 0 {
		return serverMode{}, fmt.Errorf("at least one -route or one -target is required")
	}
	return serverMode{routes: routes}, nil
}

func downstreamForTarget(target string, timeout time.Duration) (mecomserver.DownstreamDial, error) {
	ep, err := parseDaemonTarget(target)
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: %w", target, err)
	}
	if ep.Network != "can" {
		return nil, nil
	}
	return mecomserver.DialEndpointTarget(target, mecom.ClientConfig{Timeout: timeout}, socketCANDialer)
}

func prepareRouteDownstreams(routes routeFlags, timeout time.Duration) (routeFlags, error) {
	out := append(routeFlags(nil), routes...)
	for i := range out {
		downstream, err := downstreamForTarget(out[i].Target, timeout)
		if err != nil {
			return nil, err
		}
		if downstream != nil {
			out[i].Downstream = downstream
		}
	}
	return out, nil
}

func parseDaemonTarget(target string) (mecom.Endpoint, error) {
	ep, ok := mecom.ParseTarget(strings.TrimSpace(target))
	if !ok {
		return mecom.Endpoint{}, fmt.Errorf("empty endpoint")
	}
	if ep.Network == "can" && strings.TrimSpace(ep.Address) == "" {
		return mecom.Endpoint{}, fmt.Errorf("CAN endpoint requires interface/node address")
	}
	return ep, nil
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

func parseRouteSelectionPolicy(v string) (mecomserver.RouteSelectionPolicy, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "fixed", string(mecomserver.RouteSelectionFixedPreference):
		return mecomserver.RouteSelectionFixedPreference, nil
	case string(mecomserver.RouteSelectionDynamic):
		return mecomserver.RouteSelectionDynamic, nil
	default:
		return "", fmt.Errorf("invalid route policy %q: use %s or %s", v, mecomserver.RouteSelectionFixedPreference, mecomserver.RouteSelectionDynamic)
	}
}

type addressZeroMode struct {
	fixed      byte
	routeOrder bool
	autoFirst  bool
}

func (m addressZeroMode) String() string {
	if m.routeOrder {
		return "route-order"
	}
	if m.autoFirst {
		return "auto-first"
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
	case "auto-first", "auto", "first":
		return addressZeroMode{autoFirst: true}, nil
	case "route-order", "routes", "round-robin":
		return addressZeroMode{routeOrder: true}, nil
	}
	n, err := strconv.ParseUint(strings.TrimSpace(v), 0, 8)
	if err != nil {
		return addressZeroMode{}, fmt.Errorf("invalid address-zero mode %q: use disabled, auto-first, route-order, or a MeCom address: %w", v, err)
	}
	if n == 0 {
		return addressZeroMode{}, nil
	}
	if n > 254 {
		return addressZeroMode{}, fmt.Errorf("address-zero default address %d outside MeCom 0..254", n)
	}
	return addressZeroMode{fixed: byte(n)}, nil
}

func resolveAddressZeroMode(mode addressZeroMode, routes routeFlags) (byte, []byte, error) {
	switch {
	case mode.routeOrder:
		return 0, routes.addresses(), nil
	case mode.autoFirst:
		addresses := routes.addresses()
		if len(addresses) == 0 {
			return 0, nil, fmt.Errorf("address-zero auto-first requires at least one active route")
		}
		return addresses[0], nil, nil
	default:
		return mode.fixed, nil, nil
	}
}
