package mecomserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Route maps one MeCom device address to one downstream target. Multiple
// routes for the same address form an ordered primary/fallback set.
type Route struct {
	Address    byte
	Target     string
	Downstream DownstreamDial
}

// RouteSelectionPolicy controls how duplicate routes for the same address are
// ordered before a request is sent.
type RouteSelectionPolicy string

const (
	// RouteSelectionFixedPreference keeps configured route order and only uses
	// later routes when an earlier read candidate fails.
	RouteSelectionFixedPreference RouteSelectionPolicy = "fixed-preference"
	// RouteSelectionDynamic may route read requests to the healthiest duplicate
	// route, while writes remain fixed-preference and non-replayed.
	RouteSelectionDynamic RouteSelectionPolicy = "dynamic"
)

type preparedRoutes map[byte][]*routeBroker

type routeBroker struct {
	address  byte
	target   string
	priority int
	requests chan request
	stats    *brokerStatsRecorder
}

// RouterConfig configures a single TCP listener that routes addressed MeCom
// request frames to per-device downstream brokers.
type RouterConfig struct {
	Routes            []Route
	DefaultAddress    byte
	AddressZeroOrder  []byte
	RequestTimeout    time.Duration
	ReconnectDelay    time.Duration
	ClientIdleTimeout time.Duration
	RouteSelection    RouteSelectionPolicy
	// CommandIdempotencyTTL caches exact non-read command frames for this
	// duration. Set a negative value to disable duplicate-command coalescing.
	CommandIdempotencyTTL time.Duration
	TraceFrames           bool
	Logger                *log.Logger

	// stats and routeStats are filled in by prepareRoutes so callers can
	// retrieve compatibility per-address snapshots and full per-route snapshots.
	stats      map[byte]*brokerStatsRecorder
	routeStats []*brokerStatsRecorder
}

// Stats returns the first configured route snapshot for each address.
// It is retained for compatibility with older callers that keyed health by
// MeCom address. Use RouteStats when duplicate serial/CAN routes matter.
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

// RouteStats returns a configured-order snapshot for every downstream route,
// including duplicate serial/CAN routes serving the same MeCom address.
// Returns nil before ServeRouter/ListenAndServeRouter has been called.
func (cfg *RouterConfig) RouteStats() []BrokerStats {
	if cfg == nil || cfg.routeStats == nil {
		return nil
	}
	out := make([]BrokerStats, 0, len(cfg.routeStats))
	for _, rec := range cfg.routeStats {
		out = append(out, rec.Snapshot())
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
	if cfg.CommandIdempotencyTTL == 0 {
		cfg.CommandIdempotencyTTL = defaultCommandIdempotencyTTL
	}

	routes, err := prepareRoutes(ctx, cfg)
	if err != nil {
		return err
	}
	addressZero, err := newAddressZeroSelector(cfg, routes)
	if err != nil {
		return err
	}
	commandCache := newCommandIdempotencyCache(cfg.CommandIdempotencyTTL)
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
		clientAddressZero, releaseAddressZero := addressZero.Lease(conn.RemoteAddr())
		go func() {
			defer releaseAddressZero()
			handleRoutedClient(ctx, conn, routes, *cfg, clientAddressZero, commandCache)
		}()
	}
}

// RequestAddress extracts the MeCom destination address from a request frame.
func RequestAddress(frame []byte) (byte, error) {
	frame = []byte(strings.TrimSpace(string(frame)))
	if len(frame) < 3 {
		return 0, fmt.Errorf("short MeCom frame")
	}
	if frame[0] != '#' {
		if frame[0] == '?' {
			// Route unaddressed MeCom requests like ?VR identification probes to
			// the default device so service tools can identify the host.
			return 0, nil
		}
		return 0, fmt.Errorf("not a MeCom request frame")
	}
	n, err := strconv.ParseUint(string(frame[1:3]), 16, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid MeCom address %q", string(frame[1:3]))
	}
	return byte(n), nil
}

func prepareRoutes(ctx context.Context, cfg *RouterConfig) (preparedRoutes, error) {
	if len(cfg.Routes) == 0 {
		return nil, fmt.Errorf("mecomserver: at least one route required")
	}
	policy, err := normalizeRouteSelectionPolicy(cfg.RouteSelection)
	if err != nil {
		return nil, err
	}
	cfg.RouteSelection = policy
	routes := make(preparedRoutes, len(cfg.Routes))
	cfg.stats = make(map[byte]*brokerStatsRecorder, len(cfg.Routes))
	cfg.routeStats = make([]*brokerStatsRecorder, 0, len(cfg.Routes))
	for _, route := range cfg.Routes {
		if route.Address == 0 {
			return nil, fmt.Errorf("mecomserver: route address 0 is reserved")
		}
		if route.Downstream == nil {
			dial, err := DialTarget(route.Target)
			if err != nil {
				return nil, fmt.Errorf("route 0x%02X: %w", route.Address, err)
			}
			route.Downstream = dial
		}
		requests := make(chan request, 256)
		priority := len(routes[route.Address])
		routeID := fmt.Sprintf("0x%02X:%d:%s", route.Address, priority, route.Target)
		recorder := newBrokerStatsRecorder(route.Address, route.Target, routeID, priority)
		if _, ok := cfg.stats[route.Address]; !ok {
			cfg.stats[route.Address] = recorder
		}
		cfg.routeStats = append(cfg.routeStats, recorder)
		broker := &routeBroker{
			address:  route.Address,
			target:   route.Target,
			priority: priority,
			requests: requests,
			stats:    recorder,
		}
		routes[route.Address] = append(routes[route.Address], broker)
		routeCfg := Config{
			Downstream:        route.Downstream,
			RequestTimeout:    cfg.RequestTimeout,
			ReconnectDelay:    cfg.ReconnectDelay,
			ClientIdleTimeout: cfg.ClientIdleTimeout,
			TraceFrames:       cfg.TraceFrames,
			Logger:            cfg.Logger,
			statsRecorder:     recorder,
		}
		go runBroker(ctx, routeCfg, requests)
	}
	if cfg.DefaultAddress != 0 {
		if len(routes[cfg.DefaultAddress]) == 0 {
			return nil, fmt.Errorf("mecomserver: default address 0x%02X has no configured route", cfg.DefaultAddress)
		}
	}
	for _, addr := range cfg.AddressZeroOrder {
		if addr == 0 {
			return nil, fmt.Errorf("mecomserver: address-zero route order cannot include address 0")
		}
		if len(routes[addr]) == 0 {
			return nil, fmt.Errorf("mecomserver: address-zero route 0x%02X has no configured route", addr)
		}
	}
	return routes, nil
}

func handleRoutedClient(ctx context.Context, conn net.Conn, routes preparedRoutes, cfg RouterConfig, addressZero byte, commandCache *commandIdempotencyCache) {
	if cfg.ClientIdleTimeout <= 0 {
		cfg.ClientIdleTimeout = defaultClientIdleTimeout
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	if cfg.Logger != nil {
		cfg.Logger.Printf("client connected remote=%s", conn.RemoteAddr())
		defer cfg.Logger.Printf("client disconnected remote=%s", conn.RemoteAddr())
	}

	reader := bufio.NewReader(conn)
	for {
		if cfg.ClientIdleTimeout > 0 {
			_ = conn.SetReadDeadline(time.Now().Add(cfg.ClientIdleTimeout))
		}
		frame, err := readBoundedFramePartial(reader, maxClientFrameBytes)
		if err != nil {
			if cfg.TraceFrames && cfg.Logger != nil {
				cfg.Logger.Printf("client read failed remote=%s bytes=%s err=%v", conn.RemoteAddr(), describeBytes(frame), err)
			}
			return
		}
		if len(frame) == 0 {
			continue
		}
		if cfg.TraceFrames && cfg.Logger != nil {
			cfg.Logger.Printf("client -> server remote=%s frame=%s", conn.RemoteAddr(), describeBytes(frame))
		}
		candidates, err := selectRouteCandidates(frame, routes, addressZero, cfg.RouteSelection)
		if err != nil {
			resp := deviceServerError(frame, err)
			if cfg.TraceFrames && cfg.Logger != nil {
				cfg.Logger.Printf("server -> client remote=%s frame=%s error=%v", conn.RemoteAddr(), describeBytes(resp), err)
			}
			if err := writeClientFrame(conn, resp, cfg.ClientIdleTimeout); err != nil {
				return
			}
			continue
		}
		route := func() ([]byte, error) {
			return routeFrame(ctx, frame, candidates, cfg.RequestTimeout, cfg.TraceFrames, cfg.Logger, conn.RemoteAddr().String())
		}
		var resp []byte
		var routeErr error
		if commandCache != nil && !isMeComReadFrame(frame) {
			resp, routeErr = commandCache.Do(frame, route)
		} else {
			resp, routeErr = route()
		}
		if routeErr != nil {
			if ctx.Err() != nil {
				return
			}
			resp = deviceServerError(frame, routeErr)
			if cfg.TraceFrames && cfg.Logger != nil {
				cfg.Logger.Printf("server -> client remote=%s frame=%s error=%v", conn.RemoteAddr(), describeBytes(resp), routeErr)
			}
		} else if cfg.TraceFrames && cfg.Logger != nil {
			cfg.Logger.Printf("server -> client remote=%s frame=%s", conn.RemoteAddr(), describeBytes(resp))
		}
		if err := writeClientFrame(conn, resp, cfg.ClientIdleTimeout); err != nil {
			return
		}
	}
}

type commandIdempotencyCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]*commandIdempotencyEntry
}

type commandIdempotencyEntry struct {
	done      chan struct{}
	completed bool
	expires   time.Time
	frame     []byte
	err       error
}

func newCommandIdempotencyCache(ttl time.Duration) *commandIdempotencyCache {
	if ttl < 0 {
		return nil
	}
	if ttl == 0 {
		ttl = defaultCommandIdempotencyTTL
	}
	return &commandIdempotencyCache{
		ttl:     ttl,
		entries: make(map[string]*commandIdempotencyEntry),
	}
}

func (c *commandIdempotencyCache) Do(frame []byte, route func() ([]byte, error)) ([]byte, error) {
	if c == nil {
		return route()
	}
	key := commandIdempotencyKey(frame)
	if key == "" {
		return route()
	}
	now := time.Now()

	c.mu.Lock()
	c.pruneLocked(now)
	if entry, ok := c.entries[key]; ok && (!entry.completed || now.Before(entry.expires)) {
		done := entry.done
		c.mu.Unlock()
		<-done
		c.mu.Lock()
		resp := append([]byte(nil), entry.frame...)
		err := entry.err
		c.mu.Unlock()
		return resp, err
	}
	entry := &commandIdempotencyEntry{done: make(chan struct{})}
	c.entries[key] = entry
	c.mu.Unlock()

	resp, err := route()

	c.mu.Lock()
	entry.frame = append([]byte(nil), resp...)
	entry.err = err
	entry.completed = true
	entry.expires = time.Now().Add(c.ttl)
	close(entry.done)
	c.mu.Unlock()
	return resp, err
}

func (c *commandIdempotencyCache) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if entry.completed && now.After(entry.expires) {
			delete(c.entries, key)
		}
	}
}

func commandIdempotencyKey(frame []byte) string {
	s := strings.TrimSpace(string(frame))
	if s == "" || isMeComReadFrame([]byte(s)) {
		return ""
	}
	return s
}

func selectRouteCandidates(frame []byte, routes preparedRoutes, addressZero byte, policy RouteSelectionPolicy) ([]*routeBroker, error) {
	addr, err := RequestAddress(frame)
	if err != nil {
		return nil, err
	}
	if addr == 0 && addressZero != 0 {
		addr = addressZero
	}
	candidates := routes[addr]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no downstream route for MeCom address 0x%02X", addr)
	}
	return orderRouteCandidates(candidates, policy, isMeComReadFrame(frame)), nil
}

func orderRouteCandidates(candidates []*routeBroker, policy RouteSelectionPolicy, readFrame bool) []*routeBroker {
	ordered := append([]*routeBroker(nil), candidates...)
	if len(ordered) <= 1 {
		return ordered
	}
	if policy != RouteSelectionDynamic || !readFrame {
		sort.SliceStable(ordered, func(i, j int) bool {
			return routePriority(ordered[i]) < routePriority(ordered[j])
		})
		return ordered
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return routeCandidateScore(ordered[i], readFrame) < routeCandidateScore(ordered[j], readFrame)
	})
	return ordered
}

func routePriority(candidate *routeBroker) int {
	if candidate == nil {
		return 1 << 30
	}
	return candidate.priority
}

func routeCandidateScore(candidate *routeBroker, readFrame bool) int {
	if candidate == nil {
		return 1 << 30
	}
	score := routePriority(candidate)*10 + len(candidate.requests)
	stats := candidate.stats.Snapshot()
	if stats.Connected {
		score -= 100
	}
	if !stats.Connected && stats.ErrorCount > 0 {
		score += 500
	}
	if readFrame {
		score += int(stats.ErrorCount) * 5
	} else {
		score += int(stats.ErrorCount) * 50
	}
	if candidate.priority == 0 {
		score -= 10
	}
	return score
}

func normalizeRouteSelectionPolicy(policy RouteSelectionPolicy) (RouteSelectionPolicy, error) {
	switch RouteSelectionPolicy(strings.ToLower(strings.TrimSpace(string(policy)))) {
	case "", "fixed":
		return RouteSelectionFixedPreference, nil
	case RouteSelectionFixedPreference:
		return RouteSelectionFixedPreference, nil
	case RouteSelectionDynamic:
		return RouteSelectionDynamic, nil
	default:
		return "", fmt.Errorf("mecomserver: invalid route selection policy %q: use %q or %q", policy, RouteSelectionFixedPreference, RouteSelectionDynamic)
	}
}

func routeFrame(ctx context.Context, frame []byte, candidates []*routeBroker, requestTimeout time.Duration, traceFrames bool, logger *log.Logger, remote string) ([]byte, error) {
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	readFrame := isMeComReadFrame(frame)
	var lastErr error
candidateLoop:
	for i, candidate := range candidates {
		attempts := 1
		if readFrame {
			attempts = 2
		}
		for attempt := 0; attempt < attempts; attempt++ {
			reqCtx, cancelReq := context.WithTimeout(ctx, requestTimeout)
			result := make(chan response, 1)
			select {
			case candidate.requests <- request{ctx: reqCtx, frame: append([]byte(nil), frame...), result: result}:
			case <-reqCtx.Done():
				lastErr = reqCtx.Err()
				cancelReq()
				if readFrame && i+1 < len(candidates) && ctx.Err() == nil {
					continue candidateLoop
				}
				return nil, lastErr
			case <-ctx.Done():
				cancelReq()
				return nil, ctx.Err()
			}
			select {
			case res := <-result:
				cancelReq()
				if res.err == nil {
					return res.frame, nil
				}
				lastErr = res.err
				if traceFrames && logger != nil {
					logger.Printf("route candidate failed remote=%s address=0x%02X target=%s read=%t err=%v", remote, candidate.address, candidate.target, readFrame, res.err)
				}
				if readFrame && isTransientRouteReadError(res.err) && attempt+1 < attempts && ctx.Err() == nil {
					continue
				}
				if readFrame && i+1 < len(candidates) {
					continue candidateLoop
				}
				return nil, res.err
			case <-reqCtx.Done():
				lastErr = reqCtx.Err()
				cancelReq()
				if readFrame && i+1 < len(candidates) && ctx.Err() == nil {
					continue candidateLoop
				}
				return nil, lastErr
			case <-ctx.Done():
				cancelReq()
				return nil, ctx.Err()
			}
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no downstream route candidates")
	}
	return nil, lastErr
}

func isTransientRouteReadError(err error) bool {
	return errors.Is(err, io.ErrClosedPipe) || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}

func isMeComReadFrame(frame []byte) bool {
	s := strings.TrimSpace(string(frame))
	if strings.HasPrefix(s, "?") {
		return true
	}
	if len(s) > 7 && s[0] == '#' {
		return s[7] == '?'
	}
	return false
}

type addressZeroSelector struct {
	fixed  byte
	order  []byte
	next   atomic.Uint64
	mu     sync.Mutex
	leases map[string]*addressZeroRemoteLease
}

type addressZeroRemoteLease struct {
	address byte
	refs    int
}

func newAddressZeroSelector(cfg *RouterConfig, routes preparedRoutes) (*addressZeroSelector, error) {
	if cfg.DefaultAddress != 0 && len(cfg.AddressZeroOrder) > 0 {
		return nil, fmt.Errorf("mecomserver: configure either fixed default address or address-zero route order, not both")
	}
	if cfg.DefaultAddress != 0 {
		return &addressZeroSelector{fixed: cfg.DefaultAddress}, nil
	}
	if len(cfg.AddressZeroOrder) == 0 {
		return &addressZeroSelector{}, nil
	}
	order := make([]byte, 0, len(cfg.AddressZeroOrder))
	for _, addr := range cfg.AddressZeroOrder {
		if len(routes[addr]) == 0 {
			return nil, fmt.Errorf("mecomserver: address-zero route 0x%02X has no configured route", addr)
		}
		order = append(order, addr)
	}
	return &addressZeroSelector{order: order}, nil
}

func (s *addressZeroSelector) Next() byte {
	if s == nil {
		return 0
	}
	if s.fixed != 0 {
		return s.fixed
	}
	if len(s.order) == 0 {
		return 0
	}
	i := s.next.Add(1) - 1
	return s.order[int(i%uint64(len(s.order)))]
}

func (s *addressZeroSelector) Lease(remote net.Addr) (byte, func()) {
	noRelease := func() {}
	if s == nil || s.fixed != 0 || len(s.order) == 0 {
		return s.Next(), noRelease
	}
	key := addressZeroRemoteKey(remote)
	if key == "" {
		return s.Next(), noRelease
	}

	s.mu.Lock()
	if s.leases == nil {
		s.leases = make(map[string]*addressZeroRemoteLease)
	}
	if lease := s.leases[key]; lease != nil {
		lease.refs++
		addr := lease.address
		s.mu.Unlock()
		return addr, s.releaseAddressZeroLeaseFunc(key)
	}
	addr := s.Next()
	s.leases[key] = &addressZeroRemoteLease{address: addr, refs: 1}
	s.mu.Unlock()
	return addr, s.releaseAddressZeroLeaseFunc(key)
}

func (s *addressZeroSelector) releaseAddressZeroLeaseFunc(key string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			lease := s.leases[key]
			if lease == nil {
				return
			}
			lease.refs--
			if lease.refs <= 0 {
				delete(s.leases, key)
			}
		})
	}
}

func addressZeroRemoteKey(remote net.Addr) string {
	if remote == nil {
		return ""
	}
	if tcp, ok := remote.(*net.TCPAddr); ok {
		if tcp.IP != nil {
			return tcp.IP.String()
		}
	}
	host, _, err := net.SplitHostPort(remote.String())
	if err == nil {
		return host
	}
	return remote.String()
}
