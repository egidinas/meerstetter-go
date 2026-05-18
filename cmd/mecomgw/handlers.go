package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecom/writelease"
	"github.com/egidinas/meerstetter-go/tmtc"
)

const gatewayAccessCookie = "mecomgw_access"

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.handleHealth)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/devices", s.handleDevices)
	mux.HandleFunc("/api/catalogue", s.handleCatalogue)
	mux.HandleFunc("/api/leases", s.handleLeasesList)
	mux.HandleFunc("/api/devices/", s.handleDeviceRoot)
	if s.uiDir != "" {
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir(s.uiDir))))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				http.Redirect(w, r, "/ui/", http.StatusFound)
				return
			}
			http.NotFound(w, r)
		})
	}
	return logRequests(s.logger, s.withCORS(s.withAccessToken(mux)))
}

// --- top-level handlers ---

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now()})
}

type deviceView struct {
	ID           string `json:"id"`
	Label        string `json:"label,omitempty"`
	Endpoint     string `json:"endpoint"`
	Address      byte   `json:"address"`
	ChannelCount int    `json:"channel_count"`
	Bound        bool   `json:"bound"`
	LastErr      string `json:"last_error,omitempty"`
}

func (s *server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	views := make([]deviceView, 0, len(s.devices))
	for _, b := range s.devices {
		b.mu.Lock()
		v := deviceView{
			ID:           b.cfg.ID,
			Label:        b.cfg.Label,
			Endpoint:     b.cfg.Endpoint,
			Address:      b.cfg.Address,
			ChannelCount: effectiveChannelCount(b.cfg.ChannelCount, s.channelCount),
			Bound:        b.client != nil,
		}
		if b.lastErr != nil {
			v.LastErr = b.lastErr.Error()
		}
		b.mu.Unlock()
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": views})
}

func (s *server) handleCatalogue(w http.ResponseWriter, _ *http.Request) {
	channels := s.catalogueChannelCount()
	params := mecom.DefaultTECReadoutParameters(channels)
	seen := make(map[string]struct{}, len(params)+channels*2)
	out := make([]gatewayCatalogueEntry, 0, len(params)+channels*2)
	for _, p := range params {
		e := gatewayCatalogueEntry{
			ID:       p.Parameter.ID,
			Instance: p.Parameter.Instance,
			Name:     p.Parameter.Name,
			Unit:     p.Parameter.Unit,
			Type:     string(p.Parameter.Type),
			Sensor:   p.Sensor,
			HighPri:  p.HighPriority,
			Writable: gatewayCatalogueWritable(p.Parameter.ID),
		}
		seen[gatewayCatalogueKey(e.ID, e.Instance)] = struct{}{}
		out = append(out, e)
	}
	for _, e := range gatewayWriteOnlyCatalogueEntries(channels) {
		key := gatewayCatalogueKey(e.ID, e.Instance)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"parameters": out})
}

func (s *server) catalogueChannelCount() int {
	maxChannels := s.channelCount
	for _, b := range s.devices {
		if b.cfg.ChannelCount > maxChannels {
			maxChannels = b.cfg.ChannelCount
		}
	}
	return effectiveChannelCount(maxChannels, 4)
}

func effectiveChannelCount(values ...int) int {
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return 4
}

type gatewayCatalogueEntry struct {
	ID       int    `json:"id"`
	Instance int    `json:"instance"`
	Name     string `json:"name"`
	Unit     string `json:"unit,omitempty"`
	Type     string `json:"type"`
	Sensor   string `json:"sensor,omitempty"`
	HighPri  bool   `json:"high_priority"`
	Writable bool   `json:"writable"`
}

type gatewayReadValue struct {
	ID       int      `json:"id"`
	Instance int      `json:"instance"`
	Value    *float64 `json:"value"`
	Quality  string   `json:"quality"`
}

func gatewayCatalogueKey(id, instance int) string {
	return fmt.Sprintf("%d:%d", id, instance)
}

func gatewayCatalogueWritable(id int) bool {
	switch id {
	case 2010, 2040, 3000:
		return true
	default:
		return false
	}
}

func gatewayWriteOnlyCatalogueEntries(channels int) []gatewayCatalogueEntry {
	if channels <= 0 {
		channels = 4
	}
	if channels > 255 {
		channels = 255
	}
	type writeOnlyParam struct {
		id   int
		name string
		typ  mecom.DataType
	}
	params := []writeOnlyParam{
		{id: 2010, name: "output_stage_enable", typ: mecom.DataTypeInt32},
		{id: 2040, name: "operating_mode", typ: mecom.DataTypeInt32},
	}
	out := make([]gatewayCatalogueEntry, 0, channels*len(params))
	for instance := 1; instance <= channels; instance++ {
		for _, p := range params {
			out = append(out, gatewayCatalogueEntry{
				ID:       p.id,
				Instance: instance,
				Name:     p.name,
				Type:     string(p.typ),
				Sensor:   fmt.Sprintf("mecom.tec_%02d.%s", instance, p.name),
				Writable: true,
			})
		}
	}
	return out
}

func (s *server) handleLeasesList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"leases": s.leases.List()})
}

// --- /api/devices/{id}/* dispatcher ---

func (s *server) handleDeviceRoot(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	deviceID := parts[0]
	sub := ""
	if len(parts) == 2 {
		sub = parts[1]
	}
	switch sub {
	case "lease":
		switch r.Method {
		case http.MethodPost:
			s.handleLeaseAcquire(w, r, deviceID)
		case http.MethodDelete:
			s.handleLeaseRelease(w, r, deviceID)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case "write":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleWrite(w, r, deviceID)
	case "read":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRead(w, r, deviceID)
	case "poll":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handlePollSSE(w, r, deviceID)
	default:
		http.NotFound(w, r)
	}
}

// --- leases ---

type leaseAcquireRequest struct {
	Holder string `json:"holder"`
	TTL    string `json:"ttl,omitempty"`
}

func (s *server) handleLeaseAcquire(w http.ResponseWriter, r *http.Request, deviceID string) {
	if _, ok := s.devices[deviceID]; !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	var req leaseAcquireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Holder == "" {
		http.Error(w, "holder required", http.StatusBadRequest)
		return
	}
	ttl := s.defaultLeaseTTL
	if req.TTL != "" {
		parsed, err := time.ParseDuration(req.TTL)
		if err != nil {
			http.Error(w, "invalid ttl: "+err.Error(), http.StatusBadRequest)
			return
		}
		ttl = parsed
	}
	lease, err := s.leases.Acquire(deviceID, req.Holder, ttl)
	if err != nil {
		if errors.Is(err, writelease.ErrLeaseHeld) {
			writeJSON(w, http.StatusLocked, map[string]any{"error": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, lease)
}

func (s *server) handleLeaseRelease(w http.ResponseWriter, r *http.Request, deviceID string) {
	if _, ok := s.devices[deviceID]; !ok {
		http.Error(w, "unknown device", http.StatusNotFound)
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Lease-Token"))
	if token == "" {
		http.Error(w, "X-Lease-Token header required", http.StatusBadRequest)
		return
	}
	if err := s.leases.Validate(deviceID, token); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := s.leases.Release(token); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- write ---

type writeRequest struct {
	Name      string            `json:"name"`
	Arguments map[string]any    `json:"arguments"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func (s *server) handleWrite(w http.ResponseWriter, r *http.Request, deviceID string) {
	bound, err := s.bind(deviceID)
	if err != nil {
		http.Error(w, err.Error(), bindStatusForError(err))
		return
	}
	if bound.commander == nil {
		http.Error(w, "device transport does not support writes", http.StatusUnprocessableEntity)
		return
	}
	var req writeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Lease-Token"))
	if req.Metadata == nil {
		req.Metadata = map[string]string{}
	}
	if token != "" {
		req.Metadata["lease_token"] = token
	}
	tc := tmtc.Telecommand{
		TargetID:  deviceID,
		Time:      time.Now(),
		Name:      req.Name,
		Arguments: req.Arguments,
		Metadata:  req.Metadata,
	}
	tc.EnsureIdempotencyKey()
	ev, err := bound.commander.Send(tc)
	if err != nil {
		if shouldResetDeviceBinding(err) {
			s.resetDeviceBinding(deviceID, bound.client, err)
		}
		if errors.Is(err, writelease.ErrInvalidToken) || errors.Is(err, writelease.ErrUnknownDevice) || errors.Is(err, writelease.ErrExpired) {
			writeJSON(w, http.StatusLocked, map[string]any{"error": err.Error(), "event": ev})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "event": ev})
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

// --- read ---

func (s *server) handleRead(w http.ResponseWriter, r *http.Request, deviceID string) {
	bound, err := s.bind(deviceID)
	if err != nil {
		http.Error(w, err.Error(), bindStatusForError(err))
		return
	}
	params, err := parseParamsQuery(r.URL.Query().Get("params"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out, err := readGatewayValues(ctx, bound.client, params)
	if err != nil {
		if shouldResetDeviceBinding(err) {
			s.resetDeviceBinding(deviceID, bound.client, err)
		}
		http.Error(w, err.Error(), httpStatusForError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": out})
}

func readGatewayValues(ctx context.Context, client mecom.ReadClient, params []mecom.Parameter) ([]gatewayReadValue, error) {
	values, err := client.ReadBulk(ctx, params)
	if err == nil && len(values) == len(params) {
		return gatewayValuesFromFloats(params, values), nil
	}
	bulkErr := err
	if bulkErr == nil {
		bulkErr = fmt.Errorf("read returned %d values for %d parameters", len(values), len(params))
	}
	if shouldResetDeviceBinding(bulkErr) {
		return nil, bulkErr
	}

	out := make([]gatewayReadValue, len(params))
	okCount := 0
	for i, p := range params {
		out[i] = gatewayReadValue{ID: p.ID, Instance: p.Instance, Quality: "missing"}
		single, singleErr := client.ReadBulk(ctx, []mecom.Parameter{p})
		if singleErr != nil || len(single) != 1 {
			continue
		}
		out[i] = gatewayValueFromFloat(p, single[0])
		okCount++
	}
	if okCount == 0 {
		return nil, bulkErr
	}
	return out, nil
}

func gatewayValuesFromFloats(params []mecom.Parameter, values []float64) []gatewayReadValue {
	if len(values) != len(params) {
		return nil
	}
	out := make([]gatewayReadValue, len(params))
	for i, p := range params {
		out[i] = gatewayValueFromFloat(p, values[i])
	}
	return out
}

func gatewayValueFromFloat(p mecom.Parameter, value float64) gatewayReadValue {
	out := gatewayReadValue{ID: p.ID, Instance: p.Instance, Quality: "ok"}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		out.Quality = "nan"
		return out
	}
	v := value
	out.Value = &v
	return out
}

// --- poll (SSE) ---

func (s *server) handlePollSSE(w http.ResponseWriter, r *http.Request, deviceID string) {
	bound, err := s.bind(deviceID)
	if err != nil {
		http.Error(w, err.Error(), bindStatusForError(err))
		return
	}
	params, err := parseParamsQuery(r.URL.Query().Get("params"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	intervalStr := r.URL.Query().Get("interval")
	interval := 2 * time.Second
	if intervalStr != "" {
		d, err := time.ParseDuration(intervalStr)
		if err != nil {
			http.Error(w, "invalid interval: "+err.Error(), http.StatusBadRequest)
			return
		}
		interval = d
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	sub := mecom.NewSubscriber(gatewayResetReadClient{
		server:   s,
		deviceID: deviceID,
		client:   bound.client,
	}, mecom.SubscriberConfig{
		TargetID:   deviceID,
		Parameters: params,
		Interval:   interval,
	})
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go sub.Run(ctx)

	enc := json.NewEncoder(w)
	for tm := range sub.C() {
		fmt.Fprint(w, "data: ")
		if err := enc.Encode(tm); err != nil {
			return
		}
		flusher.Flush()
	}
}

type gatewayResetReadClient struct {
	server   *server
	deviceID string
	client   mecom.ReadClient
}

func (c gatewayResetReadClient) ReadBulk(ctx context.Context, params []mecom.Parameter) ([]float64, error) {
	values, err := c.client.ReadBulk(ctx, params)
	if err != nil && shouldResetDeviceBinding(err) {
		c.server.resetDeviceBinding(c.deviceID, c.client, err)
	}
	return values, err
}

func (c gatewayResetReadClient) ConfigureRingCapture(ctx context.Context, captureID uint16, params []mecom.RingCaptureParameter) error {
	return c.client.ConfigureRingCapture(ctx, captureID, params)
}

func (c gatewayResetReadClient) TriggerRingSync(ctx context.Context) error {
	return c.client.TriggerRingSync(ctx)
}

func (c gatewayResetReadClient) ReadRingPointer(ctx context.Context) (uint32, error) {
	return c.client.ReadRingPointer(ctx)
}

func (c gatewayResetReadClient) ReadRingChunk(ctx context.Context, offset uint32, maxBytes uint16) (mecom.RingReadResponse, error) {
	return c.client.ReadRingChunk(ctx, offset, maxBytes)
}

// --- helpers ---

func parseParamsQuery(raw string) ([]mecom.Parameter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("params query is required, e.g. ?params=1000:1,3000:1")
	}
	var out []mecom.Parameter
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		colon := strings.IndexByte(part, ':')
		if colon < 0 {
			return nil, fmt.Errorf("param %q must be ID:INSTANCE", part)
		}
		id, err := strconv.Atoi(strings.TrimSpace(part[:colon]))
		if err != nil {
			return nil, fmt.Errorf("param %q: invalid id: %v", part, err)
		}
		instance, err := strconv.Atoi(strings.TrimSpace(part[colon+1:]))
		if err != nil {
			return nil, fmt.Errorf("param %q: invalid instance: %v", part, err)
		}
		if instance < 1 || instance > 255 {
			return nil, fmt.Errorf("param %q: instance must be in range 1..255", part)
		}
		param, ok := gatewayParameterByID(id)
		if !ok {
			return nil, fmt.Errorf("param %q: unknown id", part)
		}
		param.Instance = instance
		out = append(out, param)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no params parsed")
	}
	return out, nil
}

func gatewayParameterByID(id int) (mecom.Parameter, bool) {
	for _, readout := range mecom.DefaultTECReadoutParameters(1) {
		if readout.Parameter.ID == id {
			param := readout.Parameter
			return param, true
		}
	}
	for _, entry := range gatewayWriteOnlyCatalogueEntries(1) {
		if entry.ID == id {
			return mecom.Parameter{
				ID:       entry.ID,
				Instance: entry.Instance,
				Name:     entry.Name,
				Unit:     entry.Unit,
				Type:     mecom.DataType(entry.Type),
			}, true
		}
	}
	return mecom.Parameter{}, false
}

func httpStatusForError(err error) int {
	switch {
	case errors.Is(err, mecom.ErrUnreachable):
		return http.StatusServiceUnavailable
	case errors.Is(err, mecom.ErrTimeout):
		return http.StatusGatewayTimeout
	case errors.Is(err, mecom.ErrBadAddress), errors.Is(err, mecom.ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, mecom.ErrUnknownParameter):
		return http.StatusNotFound
	case errors.Is(err, mecom.ErrParameterReadOnly):
		return http.StatusForbidden
	case errors.Is(err, mecom.ErrWriteRejected):
		return http.StatusConflict
	case errors.Is(err, mecom.ErrTransportNotSupported):
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

func bindStatusForError(err error) int {
	if errors.Is(err, errUnknownGatewayDevice) {
		return http.StatusNotFound
	}
	return httpStatusForError(err)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		raw, _ = json.Marshal(map[string]string{"error": "JSON encode failed: " + err.Error()})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}

func logRequests(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *server) withAccessToken(next http.Handler) http.Handler {
	if s.accessToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !gatewayAccessRequired(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if token := gatewayAccessTokenFromQuery(r); token != "" {
			if s.validAccessToken(token) {
				s.setAccessCookie(w, r, token)
				if shouldCleanGatewayAccessToken(r) {
					http.Redirect(w, r, cleanGatewayAccessURL(r), http.StatusFound)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid gateway access token"})
			return
		}
		if s.validAccessToken(r.Header.Get("X-Gateway-Token")) || s.validAccessCookie(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "gateway access token required"})
	})
}

func gatewayAccessRequired(path string) bool {
	return path == "/" || strings.HasPrefix(path, "/ui/") || strings.HasPrefix(path, "/api/")
}

func gatewayAccessTokenFromQuery(r *http.Request) string {
	if token := strings.TrimSpace(r.URL.Query().Get("access_token")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("t"))
}

func shouldCleanGatewayAccessToken(r *http.Request) bool {
	return r.Method == http.MethodGet && (r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/ui/"))
}

func cleanGatewayAccessURL(r *http.Request) string {
	u := *r.URL
	q := u.Query()
	q.Del("access_token")
	q.Del("t")
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *server) validAccessToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.accessToken)) == 1
}

func (s *server) validAccessCookie(r *http.Request) bool {
	cookie, err := r.Cookie(gatewayAccessCookie)
	return err == nil && s.validAccessToken(cookie.Value)
}

func (s *server) setAccessCookie(w http.ResponseWriter, r *http.Request, token string) {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	http.SetCookie(w, &http.Cookie{
		Name:     gatewayAccessCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int((12 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

func (s *server) withCORS(next http.Handler) http.Handler {
	if len(s.allowedOrigins) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Lease-Token, X-Gateway-Token")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range s.allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}
