package mecomserver

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

// RouterRuntime is the shared execution core behind the TCP router and
// metadata fanout helpers. Cancel the context passed to NewRouterRuntime to
// stop its downstream brokers.
type RouterRuntime struct {
	routes       preparedRoutes
	addressZero  *addressZeroSelector
	clientCfg    routedClientConfig
	commandCache *commandIdempotencyCache
}

// RouterDevice describes one logical MeCom device address and every configured
// route that can reach it.
type RouterDevice struct {
	Address byte          `json:"address"`
	Routes  []RouterRoute `json:"routes"`
}

// RouterRoute describes one configured downstream route for a logical device.
type RouterRoute struct {
	Address  byte        `json:"address"`
	RouteID  string      `json:"route_id,omitempty"`
	Priority int         `json:"priority"`
	Target   string      `json:"target"`
	Stats    BrokerStats `json:"stats"`
}

// FanoutWritePolicy controls whether meta fanout may send non-read frames.
type FanoutWritePolicy string

const (
	// FanoutWriteReject keeps fanout read-only. This is the default because
	// writes can have device side effects and must never be bus-broadcast by
	// accident.
	FanoutWriteReject FanoutWritePolicy = "reject"
	// FanoutWriteAllowAddressed permits addressed write fanout. Each target
	// device still uses the normal router write policy: preferred route only,
	// no fallback replay, and command idempotency caching.
	FanoutWriteAllowAddressed FanoutWritePolicy = "allow-addressed"
)

// FanoutRequest asks the router runtime to rewrite one addressed MeCom
// template frame for one or more configured device addresses, route each frame
// independently, and aggregate per-device replies.
type FanoutRequest struct {
	Frame          []byte               `json:"frame"`
	Addresses      []byte               `json:"addresses,omitempty"`
	RequestTimeout time.Duration        `json:"request_timeout,omitempty"`
	RouteSelection RouteSelectionPolicy `json:"route_selection,omitempty"`
	WritePolicy    FanoutWritePolicy    `json:"write_policy,omitempty"`
}

// FanoutResult is the aggregate reply for a router meta fanout operation.
type FanoutResult struct {
	Read      bool             `json:"read"`
	Responses []FanoutResponse `json:"responses"`
}

// FanoutResponse is one logical-device result. Errors are per-device so a
// partially offline device set can still return useful replies.
type FanoutResponse struct {
	Address       byte          `json:"address"`
	RequestFrame  string        `json:"request_frame"`
	ResponseFrame string        `json:"response_frame,omitempty"`
	Error         string        `json:"error,omitempty"`
	Routes        []RouterRoute `json:"routes,omitempty"`
}

// NewRouterRuntime starts the configured downstream route brokers without
// opening a TCP listener. ServeRouter uses the same runtime internally.
func NewRouterRuntime(ctx context.Context, cfg *RouterConfig) (*RouterRuntime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("mecomserver: RouterConfig required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	applyRouterDefaults(cfg)
	routes, err := prepareRoutes(ctx, cfg)
	if err != nil {
		return nil, err
	}
	addressZero, err := newAddressZeroSelector(cfg, routes)
	if err != nil {
		return nil, err
	}
	return &RouterRuntime{
		routes:      routes,
		addressZero: addressZero,
		clientCfg: routedClientConfig{
			RequestTimeout:    cfg.RequestTimeout,
			ClientIdleTimeout: cfg.ClientIdleTimeout,
			RouteSelection:    cfg.RouteSelection,
			TraceFrames:       cfg.TraceFrames,
			Logger:            cfg.Logger,
		},
		commandCache: newCommandIdempotencyCache(cfg.CommandIdempotencyTTL),
	}, nil
}

func applyRouterDefaults(cfg *RouterConfig) {
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}
	if cfg.CommandIdempotencyTTL == 0 {
		cfg.CommandIdempotencyTTL = defaultCommandIdempotencyTTL
	}
}

// Devices returns configured logical devices grouped by MeCom address.
func (rt *RouterRuntime) Devices() []RouterDevice {
	if rt == nil {
		return nil
	}
	addrs := rt.configuredAddresses()
	devices := make([]RouterDevice, 0, len(addrs))
	for _, addr := range addrs {
		devices = append(devices, RouterDevice{
			Address: addr,
			Routes:  rt.routeSnapshots(addr),
		})
	}
	return devices
}

// Fanout sends one addressed request per logical device and aggregates replies.
// With an empty address list, all configured non-zero device addresses are
// queried in ascending order. Write fanout is rejected unless explicitly
// enabled with FanoutWriteAllowAddressed.
func (rt *RouterRuntime) Fanout(ctx context.Context, req FanoutRequest) (FanoutResult, error) {
	if rt == nil {
		return FanoutResult{}, fmt.Errorf("mecomserver: RouterRuntime required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	template, err := parseFanoutTemplate(req.Frame)
	if err != nil {
		return FanoutResult{}, err
	}
	if !template.read && req.WritePolicy != FanoutWriteAllowAddressed {
		return FanoutResult{}, fmt.Errorf("mecomserver: write fanout rejected by default; set write policy %q for addressed writes", FanoutWriteAllowAddressed)
	}
	addrs, err := rt.fanoutAddresses(req.Addresses)
	if err != nil {
		return FanoutResult{}, err
	}
	policy := rt.clientCfg.RouteSelection
	if req.RouteSelection != "" {
		policy, err = normalizeRouteSelectionPolicy(req.RouteSelection)
		if err != nil {
			return FanoutResult{}, err
		}
	}
	timeout := req.RequestTimeout
	if timeout <= 0 {
		timeout = rt.clientCfg.RequestTimeout
	}

	result := FanoutResult{
		Read:      template.read,
		Responses: make([]FanoutResponse, len(addrs)),
	}
	var wg sync.WaitGroup
	for i, addr := range addrs {
		i, addr := i, addr
		wg.Add(1)
		go func() {
			defer wg.Done()
			frame := template.frameFor(addr)
			reply := FanoutResponse{
				Address:      addr,
				RequestFrame: frame,
				Routes:       rt.routeSnapshots(addr),
			}
			resp, routeErr := rt.routeAddressedFrame(ctx, []byte(frame), addr, timeout, policy, true, "fanout")
			if routeErr != nil {
				reply.Error = routeErr.Error()
			} else {
				reply.ResponseFrame = string(resp)
			}
			result.Responses[i] = reply
		}()
	}
	wg.Wait()
	return result, nil
}

func (rt *RouterRuntime) routeAddressedFrame(ctx context.Context, frame []byte, addr byte, timeout time.Duration, policy RouteSelectionPolicy, useCommandCache bool, remote string) ([]byte, error) {
	if rt == nil {
		return nil, fmt.Errorf("mecomserver: RouterRuntime required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if addr == 0 {
		return nil, fmt.Errorf("mecomserver: addressed route cannot use MeCom address 0")
	}
	candidates := rt.routes[addr]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no downstream route for MeCom address 0x%02X", addr)
	}
	if policy == "" {
		policy = rt.clientCfg.RouteSelection
	}
	normalized, err := normalizeRouteSelectionPolicy(policy)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = rt.clientCfg.RequestTimeout
	}
	readFrame := isMeComReadFrame(frame)
	ordered := orderRouteCandidates(candidates, normalized, readFrame)
	route := func() ([]byte, error) {
		return routeFrame(ctx, frame, ordered, timeout, rt.clientCfg.TraceFrames, rt.clientCfg.Logger, remote)
	}
	if useCommandCache && rt.commandCache != nil && !readFrame {
		return rt.commandCache.Do(frame, route)
	}
	return route()
}

func (rt *RouterRuntime) configuredAddresses() []byte {
	addrs := make([]int, 0, len(rt.routes))
	for addr := range rt.routes {
		addrs = append(addrs, int(addr))
	}
	sort.Ints(addrs)
	out := make([]byte, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, byte(addr))
	}
	return out
}

func (rt *RouterRuntime) fanoutAddresses(requested []byte) ([]byte, error) {
	if len(requested) == 0 {
		return rt.configuredAddresses(), nil
	}
	out := make([]byte, 0, len(requested))
	seen := make(map[byte]bool, len(requested))
	for _, addr := range requested {
		if addr == 0 {
			return nil, fmt.Errorf("mecomserver: fanout address 0 is reserved")
		}
		if len(rt.routes[addr]) == 0 {
			return nil, fmt.Errorf("mecomserver: no downstream route for MeCom address 0x%02X", addr)
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out, nil
}

func (rt *RouterRuntime) routeSnapshots(addr byte) []RouterRoute {
	routes := rt.routes[addr]
	out := make([]RouterRoute, 0, len(routes))
	for _, route := range routes {
		if route == nil {
			continue
		}
		stats := route.stats.Snapshot()
		out = append(out, RouterRoute{
			Address:  route.address,
			RouteID:  stats.RouteID,
			Priority: route.priority,
			Target:   route.target,
			Stats:    stats,
		})
	}
	return out
}

type fanoutTemplate struct {
	seq     string
	payload string
	read    bool
}

func parseFanoutTemplate(frame []byte) (fanoutTemplate, error) {
	s := strings.TrimSpace(string(frame))
	if len(s) == 0 {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: fanout frame required")
	}
	if strings.HasPrefix(s, "?") {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: fanout requires an addressed MeCom template frame, got bare request")
	}
	if len(s) < 11 || s[0] != '#' {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: invalid fanout template frame %q", s)
	}
	if _, err := strconv.ParseUint(s[1:3], 16, 8); err != nil {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: invalid fanout template address %q: %w", s[1:3], err)
	}
	if _, err := strconv.ParseUint(s[3:7], 16, 16); err != nil {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: invalid fanout template sequence %q: %w", s[3:7], err)
	}
	payloadEnd := len(s) - 4
	got, err := strconv.ParseUint(s[payloadEnd:], 16, 16)
	if err != nil {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: invalid fanout template CRC %q: %w", s[payloadEnd:], err)
	}
	if want := mecom.CRC16([]byte(s[:payloadEnd])); uint16(got) != want {
		return fanoutTemplate{}, fmt.Errorf("mecomserver: fanout template CRC mismatch got %04X want %04X", uint16(got), want)
	}
	payload := strings.ToUpper(s[7:payloadEnd])
	return fanoutTemplate{
		seq:     strings.ToUpper(s[3:7]),
		payload: payload,
		read:    strings.HasPrefix(payload, "?"),
	}, nil
}

func (t fanoutTemplate) frameFor(addr byte) string {
	prefix := fmt.Sprintf("#%02X%s%s", addr, t.seq, t.payload)
	return fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator)
}
