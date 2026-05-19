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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/meerstetter-go/mecom/writelease"
	tmtc "github.com/egidinas/signalforge/contracts"
)

const gatewayAccessCookie = "mecomgw_access"

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.handleHealth)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/devices", s.handleDevices)
	mux.HandleFunc("/api/catalogue", s.handleCatalogue)
	mux.HandleFunc("/api/commands", s.handleCommandsList)
	mux.HandleFunc("/api/graph/history/export", s.handleGraphHistoryExport)
	mux.HandleFunc("/api/graph/history/import", s.handleGraphHistoryImport)
	mux.HandleFunc("/api/graph/availability", s.handleGraphAvailability)
	mux.HandleFunc("/api/log/export", s.handleLogExport)
	mux.HandleFunc("/api/log/import", s.handleLogImport)
	mux.HandleFunc("/api/graph/sparklines", s.handleGraphSparklines)
	mux.HandleFunc("/api/graph/tiles/", s.handleGraphTileRoot)
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
	ID              string            `json:"id"`
	Label           string            `json:"label,omitempty"`
	Endpoint        string            `json:"endpoint"`
	ActiveRoute     deviceRouteView   `json:"active_route"`
	RouteCandidates []deviceRouteView `json:"route_candidates,omitempty"`
	Address         byte              `json:"address"`
	ChannelCount    int               `json:"channel_count"`
	Channels        []channelView     `json:"channels,omitempty"`
	Bound           bool              `json:"bound"`
	LastErr         string            `json:"last_error,omitempty"`
}

type deviceRouteView struct {
	Role      string `json:"role"`
	Name      string `json:"name,omitempty"`
	Endpoint  string `json:"endpoint"`
	Transport string `json:"transport,omitempty"`
	State     string `json:"state,omitempty"`
	Active    bool   `json:"active,omitempty"`
}

type gatewayCommandActivity struct {
	Time            time.Time `json:"time"`
	TargetID        string    `json:"target_id"`
	DeviceID        string    `json:"device_id"`
	ParamID         int       `json:"param_id,omitempty"`
	Instance        int       `json:"instance,omitempty"`
	SignalName      string    `json:"signal_name,omitempty"`
	SignalUnit      string    `json:"signal_unit,omitempty"`
	RequestedValue  any       `json:"requested_value,omitempty"`
	ConfirmedValue  any       `json:"confirmed_value,omitempty"`
	PrevValue       any       `json:"prev_value,omitempty"`
	ReadbackMatched *bool     `json:"readback_matched,omitempty"`
	Status          string    `json:"status"`
	Transport       string    `json:"transport,omitempty"`
	LeaseHolder     string    `json:"lease_holder,omitempty"`
	Error           string    `json:"error,omitempty"`
	ErrorCategory   string    `json:"error_category,omitempty"`
	HTTPStatus      int       `json:"http_status"`
	IdempotencyKey  string    `json:"idempotency_key,omitempty"`
}

type channelView struct {
	Instance   int    `json:"instance"`
	Role       string `json:"role,omitempty"`
	RoleSource string `json:"role_source,omitempty"`
	Label      string `json:"label,omitempty"`
	UserNote   string `json:"user_note,omitempty"`
	HasCascade bool   `json:"has_cascade,omitempty"`
}

func (s *server) handleDevices(w http.ResponseWriter, _ *http.Request) {
	views := make([]deviceView, 0, len(s.devices))
	for _, b := range s.orderedDeviceBindings() {
		b.mu.Lock()
		channelCount := effectiveChannelCount(b.cfg.ChannelCount, s.channelCount)
		v := deviceView{
			ID:              b.cfg.ID,
			Label:           b.cfg.Label,
			Endpoint:        b.cfg.Endpoint,
			ActiveRoute:     activeDeviceRouteView(b.cfg),
			RouteCandidates: deviceRouteCandidatesView(b.cfg),
			Address:         b.cfg.Address,
			ChannelCount:    channelCount,
			Channels:        deviceChannelsView(b.cfg, channelCount),
			Bound:           b.client != nil,
		}
		if b.lastErr != nil {
			v.LastErr = b.lastErr.Error()
		}
		b.mu.Unlock()
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": views})
}

func (s *server) orderedDeviceBindings() []*deviceBinding {
	views := make([]*deviceBinding, 0, len(s.devices))
	for _, b := range s.devices {
		views = append(views, b)
	}
	sort.SliceStable(views, func(i, j int) bool {
		li := deviceSortRank(s.defaultDeviceID, views[i].cfg.ID)
		lj := deviceSortRank(s.defaultDeviceID, views[j].cfg.ID)
		if li != lj {
			return li < lj
		}
		return views[i].cfg.ID < views[j].cfg.ID
	})
	return views
}

func deviceSortRank(defaultDeviceID, id string) int {
	if defaultDeviceID != "" && id == defaultDeviceID {
		return 0
	}
	if defaultDeviceID == "" && id == "tec-76" {
		return 0
	}
	return 1
}

func deviceChannelsView(cfg DeviceConfig, channelCount int) []channelView {
	out := make([]channelView, 0, channelCount)
	for instance := 1; instance <= channelCount; instance++ {
		ch, configured := deviceChannelConfig(cfg, instance)
		if configured && (ch.Instance < 1 || ch.Instance > channelCount) {
			continue
		}
		role, source := effectiveDeviceChannelRole(cfg, instance)
		v := channelView{
			Instance:   instance,
			Role:       role,
			RoleSource: source,
			Label:      ch.Label,
			UserNote:   ch.UserNote,
			HasCascade: ch.HasCascade,
		}
		out = append(out, v)
	}
	return out
}

func activeDeviceRouteView(cfg DeviceConfig) deviceRouteView {
	for _, route := range cfg.Routes {
		if strings.TrimSpace(route.Endpoint) == strings.TrimSpace(cfg.Endpoint) {
			return deviceRouteView{
				Role:      route.Role,
				Name:      route.Name,
				Endpoint:  route.Endpoint,
				Transport: firstNonEmpty(route.Transport, routeTransportFromEndpoint(route.Endpoint)),
				State:     "active",
				Active:    true,
			}
		}
	}
	return deviceRouteView{
		Role:      "hot",
		Name:      "configured",
		Endpoint:  cfg.Endpoint,
		Transport: routeTransportFromEndpoint(cfg.Endpoint),
		State:     "active",
		Active:    true,
	}
}

func deviceRouteCandidatesView(cfg DeviceConfig) []deviceRouteView {
	out := make([]deviceRouteView, 0, len(cfg.Routes)+1)
	active := activeDeviceRouteView(cfg)
	seenActive := false
	for _, route := range cfg.Routes {
		isActive := strings.TrimSpace(route.Endpoint) == strings.TrimSpace(active.Endpoint)
		if isActive {
			seenActive = true
		}
		out = append(out, deviceRouteView{
			Role:      route.Role,
			Name:      route.Name,
			Endpoint:  route.Endpoint,
			Transport: firstNonEmpty(route.Transport, routeTransportFromEndpoint(route.Endpoint)),
			State:     firstNonEmpty(activeState(isActive), route.State),
			Active:    isActive,
		})
	}
	if !seenActive {
		out = append([]deviceRouteView{active}, out...)
	}
	return out
}

func routeTransportFromEndpoint(endpoint string) string {
	switch {
	case strings.HasPrefix(endpoint, "tcp:"):
		return "tcp"
	case strings.HasPrefix(endpoint, "serial:"):
		return "serial"
	case strings.HasPrefix(endpoint, "serial+can:"):
		return "serial+can"
	case strings.HasPrefix(endpoint, "can:"):
		return "can"
	case strings.HasPrefix(endpoint, "canopen:"):
		return "canopen"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func activeState(active bool) string {
	if active {
		return "active"
	}
	return ""
}

func (s *server) handleCatalogue(w http.ResponseWriter, _ *http.Request) {
	channels := s.catalogueChannelCount()
	params := mecom.DefaultTECCatalogueEntries(channels)
	seen := make(map[string]struct{}, len(params)+channels*2)
	out := make([]gatewayCatalogueEntry, 0, len(params)+channels*2)
	for _, p := range params {
		e := gatewayCatalogueEntry{
			TECCatalogueEntry:       p,
			Writable:                gatewayCatalogueWritable(p.ID),
			RouteSupport:            gatewayCatalogueRouteSupport(p.TransportSupport),
			TelemetryCounterparts:   p.Counterparts,
			TelecommandCounterparts: p.Counterparts,
			WriteSemantics:          gatewayCatalogueWriteSemantics(p.Access, p.Command),
		}
		if p.ReadoutPriority == "high" {
			e.HighPri = true
		}
		e.Sensor = fmt.Sprintf("mecom.tec_%02d.%s", p.Instance, p.RawName)
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
	mecom.TECCatalogueEntry
	Sensor                  string           `json:"sensor,omitempty"`
	HighPri                 bool             `json:"high_priority"`
	Writable                bool             `json:"writable"`
	RouteSupport            []string         `json:"route_support,omitempty"`
	TelemetryCounterparts   map[string][]int `json:"telemetry_counterparts,omitempty"`
	TelecommandCounterparts map[string][]int `json:"telecommand_counterparts,omitempty"`
	WriteSemantics          string           `json:"write_semantics,omitempty"`
}

type gatewayReadValue struct {
	ID       int      `json:"id"`
	Instance int      `json:"instance"`
	Value    *float64 `json:"value"`
	Quality  string   `json:"quality"`
	At       string   `json:"at,omitempty"`
	AgeMS    *int64   `json:"age_ms,omitempty"`
}

func gatewayCatalogueKey(id, instance int) string {
	return fmt.Sprintf("%d:%d", id, instance)
}

func gatewayCatalogueWritable(id int) bool {
	return mecom.TECParameterWritable(id)
}

func gatewayWriteOnlyCatalogueEntries(channels int) []gatewayCatalogueEntry {
	params := mecom.DefaultTECWriteParameters(channels)
	out := make([]gatewayCatalogueEntry, 0, len(params))
	for _, p := range params {
		out = append(out, gatewayCatalogueEntry{
			TECCatalogueEntry: mecom.TECCatalogueEntry{
				ID:               p.ID,
				Instance:         p.Instance,
				DisplayName:      p.Name,
				RawName:          p.Name,
				Unit:             p.Unit,
				Type:             string(p.Type),
				Kind:             "continuous",
				Access:           "write",
				ReadoutPriority:  "background",
				PreferredReadout: "mecom_vx_round_robin_queue",
			},
			Sensor:         fmt.Sprintf("mecom.tec_%02d.%s", p.Instance, p.Name),
			Writable:       p.Writable,
			RouteSupport:   []string{"serial", "can", "serial+can", "canopen", "tcp"},
			WriteSemantics: "write",
		})
	}
	return out
}

func gatewayCatalogueRouteSupport(transportSupport []string) []string {
	if len(transportSupport) == 0 {
		return []string{"serial", "can", "serial+can", "canopen", "tcp"}
	}
	out := make([]string, 0, len(transportSupport))
	for _, support := range transportSupport {
		switch support {
		case "mecom_serial":
			out = append(out, "serial")
		case "mecom_can":
			out = append(out, "can")
		case "mecom_serial_can":
			out = append(out, "serial+can")
		case "mecom_tcp":
			out = append(out, "tcp")
		case "canopen":
			out = append(out, "canopen")
		}
	}
	if len(out) == 0 {
		return []string{"serial", "can", "serial+can", "canopen", "tcp"}
	}
	return out
}

func gatewayCatalogueWriteSemantics(access, command string) string {
	if access == "write" {
		if command != "" {
			return command
		}
		return "write"
	}
	return "read_only"
}

func (s *server) handleLeasesList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"leases": s.leases.List()})
}

func (s *server) handleCommandsList(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}
	s.commandLogMu.Lock()
	defer s.commandLogMu.Unlock()
	start := len(s.commandLog) - limit
	if start < 0 {
		start = 0
	}
	out := make([]gatewayCommandActivity, len(s.commandLog[start:]))
	copy(out, s.commandLog[start:])
	writeJSON(w, http.StatusOK, map[string]any{"commands": out})
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
	case "graph":
		s.handleDeviceGraphRoot(w, r, deviceID)
	default:
		http.NotFound(w, r)
	}
}

func (s *server) handleDeviceGraphRoot(w http.ResponseWriter, r *http.Request, deviceID string) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/devices/"+deviceID+"/graph/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	switch parts[0] {
	case "tiles":
		s.handleGraphTileRoot(w, r)
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
	activity := gatewayCommandActivity{
		Time:     time.Now(),
		DeviceID: deviceID,
		TargetID: deviceID,
		Status:   "failed",
	}
	bound, err := s.bind(deviceID)
	if err != nil {
		activity.Error = err.Error()
		activity.ErrorCategory = gatewayCommandErrorCategory(err)
		activity.HTTPStatus = bindStatusForError(err)
		s.recordCommandActivity(activity)
		http.Error(w, err.Error(), activity.HTTPStatus)
		return
	}
	if bound.commander == nil {
		activity.Error = "device transport does not support writes"
		activity.ErrorCategory = "transport"
		activity.HTTPStatus = http.StatusUnprocessableEntity
		activity.Transport = "unsupported"
		s.recordCommandActivity(activity)
		http.Error(w, activity.Error, activity.HTTPStatus)
		return
	}
	var req writeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		activity.Error = "invalid JSON: " + err.Error()
		activity.ErrorCategory = "request"
		activity.HTTPStatus = http.StatusBadRequest
		s.recordCommandActivity(activity)
		http.Error(w, activity.Error, activity.HTTPStatus)
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
	activity.Transport = commanderTransportName(bound.commander)
	activity.IdempotencyKey = tc.IdempotencyKey
	activity.LeaseHolder = s.leaseHolderForToken(token)
	activity.ParamID, activity.Instance, activity.SignalName, activity.SignalUnit, activity.RequestedValue = gatewayCommandTargetFromTelecommand(req)

	// Pre-check: Read current value before write for definitely validated status
	if bound.client != nil && activity.ParamID != 0 {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		if vals, err := bound.client.ReadBulk(ctx, []mecom.Parameter{{ID: activity.ParamID, Instance: activity.Instance}}); err == nil && len(vals) == 1 {
			activity.PrevValue = gatewayConfirmedWriteValue(mecom.Parameter{ID: activity.ParamID}, vals[0])
		}
		cancel()
	}

	ev, err := bound.commander.Send(tc)
	activity.HTTPStatus = http.StatusOK
	activity.Status = string(ev.Status)
	if errors.Is(err, mecom.ErrReadbackMismatch) {
		activity.Status = "readback_mismatch"
	}
	if ev.Error != "" {
		activity.Error = ev.Error
	}
	if ev.Status == tmtc.CommandCompleted {
		confirmed, matched, verifyErr := s.verifyGatewayWrite(r.Context(), bound.client, req)
		activity.ConfirmedValue = confirmed
		activity.ReadbackMatched = matched
		if verifyErr != nil {
			activity.Status = "verify_failed"
			activity.HTTPStatus = httpStatusForError(verifyErr)
			activity.Error = verifyErr.Error()
			activity.ErrorCategory = gatewayCommandErrorCategory(verifyErr)
			if shouldResetDeviceBinding(verifyErr) {
				s.resetDeviceBinding(deviceID, bound.client, verifyErr)
			}
			s.recordCommandActivity(activity)
			writeJSON(w, activity.HTTPStatus, map[string]any{"error": verifyErr.Error(), "event": ev, "confirmed_value": confirmed, "readback_matched": matched})
			return
		}
		if matched != nil && !*matched {
			activity.Status = "readback_mismatch"
			activity.HTTPStatus = http.StatusConflict
			activity.Error = fmt.Sprintf("write readback mismatch for parameter %d instance %d: requested %v confirmed %v", activity.ParamID, activity.Instance, activity.RequestedValue, confirmed)
			activity.ErrorCategory = "readback"
			s.recordCommandActivity(activity)
			writeJSON(w, http.StatusConflict, map[string]any{"error": activity.Error, "event": ev, "confirmed_value": confirmed, "readback_matched": false})
			return
		}
		if confirmed != nil || matched != nil || activity.PrevValue != nil {
			ev.Result = map[string]any{
				"confirmed_value":  confirmed,
				"readback_matched": matched,
				"prev_value":       activity.PrevValue,
			}
		}
		activity.HTTPStatus = http.StatusOK
		s.recordCommandActivity(activity)
		writeJSON(w, http.StatusOK, ev)
		return
	}
	activity.HTTPStatus = gatewayCommandHTTPStatus(ev.Status, err)
	activity.ErrorCategory = gatewayCommandErrorCategory(err)
	if err != nil {
		if shouldResetDeviceBinding(err) {
			s.resetDeviceBinding(deviceID, bound.client, err)
		}
		if errors.Is(err, writelease.ErrInvalidToken) || errors.Is(err, writelease.ErrUnknownDevice) || errors.Is(err, writelease.ErrExpired) {
			activity.HTTPStatus = http.StatusLocked
			s.recordCommandActivity(activity)
			writeJSON(w, http.StatusLocked, map[string]any{"error": err.Error(), "event": ev})
			return
		}
		s.recordCommandActivity(activity)
		writeJSON(w, activity.HTTPStatus, map[string]any{"error": err.Error(), "event": ev})
		return
	}
	s.recordCommandActivity(activity)
	writeJSON(w, http.StatusOK, ev)
}

func (s *server) recordCommandActivity(activity gatewayCommandActivity) {
	s.commandLogMu.Lock()
	defer s.commandLogMu.Unlock()
	s.commandLog = append(s.commandLog, activity)
	if len(s.commandLog) > 100 {
		s.commandLog = append([]gatewayCommandActivity(nil), s.commandLog[len(s.commandLog)-100:]...)
	}
}

func commanderTransportName(cmdr *mecom.Commander) string {
	if cmdr == nil {
		return ""
	}
	if cmdr.StringWriter != nil {
		return "string"
	}
	if cmdr.Writer != nil {
		return "mecom"
	}
	return ""
}

func gatewayCommandHTTPStatus(status tmtc.CommandStatus, err error) int {
	switch status {
	case tmtc.CommandCompleted:
		return http.StatusOK
	case tmtc.CommandRejected:
		if errors.Is(err, writelease.ErrInvalidToken) || errors.Is(err, writelease.ErrUnknownDevice) || errors.Is(err, writelease.ErrExpired) {
			return http.StatusLocked
		}
		return http.StatusBadRequest
	case tmtc.CommandFailed:
		if errors.Is(err, mecom.ErrReadbackMismatch) {
			return http.StatusConflict
		}
		return http.StatusBadRequest
	default:
		return http.StatusBadRequest
	}
}

func gatewayCommandErrorCategory(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, writelease.ErrInvalidToken), errors.Is(err, writelease.ErrUnknownDevice), errors.Is(err, writelease.ErrExpired):
		return "lease"
	case errors.Is(err, mecom.ErrUnreachable), errors.Is(err, mecom.ErrTimeout):
		return "transport"
	case errors.Is(err, mecom.ErrUnknownParameter), errors.Is(err, mecom.ErrParameterReadOnly), errors.Is(err, mecom.ErrWriteRejected), errors.Is(err, mecom.ErrTransportNotSupported), errors.Is(err, mecom.ErrBadAddress), errors.Is(err, mecom.ErrInvalidArgument):
		return "mecom"
	default:
		return "gateway"
	}
}

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}


func (s *server) leaseHolderForToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	for _, lease := range s.leases.List() {
		if lease.Token == token {
			return lease.Holder
		}
	}
	return ""
}

func gatewayCommandTargetFromTelecommand(req writeRequest) (int, int, string, string, any) {
	paramID := 0
	instance := 0
	if req.Arguments != nil {
		if v, ok := req.Arguments["param"]; ok {
			paramID = intFromAny(v)
		}
		if v, ok := req.Arguments["instance"]; ok {
			instance = intFromAny(v)
		}
	}
	signalName, signalUnit := "", ""
	if param, ok := gatewayParameterByID(paramID); ok {
		signalName = param.Name
		signalUnit = param.Unit
	}
	return paramID, instance, signalName, signalUnit, req.Arguments["value"]
}

func (s *server) verifyGatewayWrite(ctx context.Context, client mecom.ReadClient, req writeRequest) (any, *bool, error) {
	param, want, ok, err := gatewayWriteParameterFromRequest(req)
	if err != nil || !ok {
		return nil, nil, err
	}
	if client == nil {
		return nil, nil, fmt.Errorf("write readback unavailable: no read client")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := client.ReadBulk(ctx, []mecom.Parameter{param})
	if err != nil {
		return nil, nil, err
	}
	if len(values) != 1 {
		return nil, nil, fmt.Errorf("write readback returned %d values for parameter %d instance %d", len(values), param.ID, param.Instance)
	}
	confirmed := gatewayConfirmedWriteValue(param, values[0])
	matched := gatewayWriteValueMatches(param, values[0], want)
	return confirmed, boolPtr(matched), nil
}

func gatewayWriteParameterFromRequest(req writeRequest) (mecom.Parameter, float64, bool, error) {
	if req.Arguments == nil {
		return mecom.Parameter{}, 0, false, nil
	}
	paramID := intFromAny(req.Arguments["param"])
	instance := intFromAny(req.Arguments["instance"])
	if instance == 0 {
		instance = 1
	}
	rawValue, hasValue := req.Arguments["value"]
	if paramID == 0 || instance == 0 || !hasValue {
		return mecom.Parameter{}, 0, false, nil
	}
	want, ok := floatFromAny(rawValue)
	if !ok {
		return mecom.Parameter{}, 0, false, nil
	}
	param, ok := gatewayParameterByID(paramID)
	if !ok {
		return mecom.Parameter{}, 0, false, fmt.Errorf("write readback unavailable: unknown parameter %d", paramID)
	}
	param.Instance = instance
	switch {
	case strings.Contains(strings.ToLower(req.Name), "int32"):
		param.Type = mecom.DataTypeInt32
	case strings.Contains(strings.ToLower(req.Name), "float32"):
		param.Type = mecom.DataTypeFloat32
	}
	if param.Type != mecom.DataTypeInt32 && param.Type != mecom.DataTypeFloat32 {
		return mecom.Parameter{}, 0, false, nil
	}
	return param, want, true, nil
}

func gatewayConfirmedWriteValue(param mecom.Parameter, value float64) any {
	if param.Type == mecom.DataTypeInt32 {
		return int32(math.Round(value))
	}
	return value
}

func gatewayWriteValueMatches(param mecom.Parameter, got, want float64) bool {
	if param.Type == mecom.DataTypeInt32 {
		return int32(math.Round(got)) == int32(math.Round(want))
	}
	tolerance := math.Max(1e-4, math.Abs(want)*1e-5)
	return math.Abs(got-want) <= tolerance
}

func floatFromAny(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case float32:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(x))
		return i
	default:
		return 0
	}
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
	out, err := s.readGatewayValues(deviceID, ctx, bound.client, params)
	if err != nil {
		if shouldResetDeviceBinding(err) {
			s.resetDeviceBinding(deviceID, bound.client, err)
		}
		http.Error(w, err.Error(), httpStatusForError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": out})
}

func (s *server) readGatewayValues(deviceID string, ctx context.Context, client mecom.ReadClient, params []mecom.Parameter) ([]gatewayReadValue, error) {
	values, err := client.ReadBulk(ctx, params)
	if err == nil && len(values) == len(params) {
		readAt := time.Now().UTC()
		out := gatewayValuesFromFloats(params, values)
		stampGatewayReadValues(out, readAt, readAt)
		s.recordGatewayReadSamples(deviceID, out, readAt)
		return out, nil
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
		out[i] = gatewayReadValue{ID: p.ID, Instance: p.Instance, Quality: gatewayQualityMissing}
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
	readAt := time.Now().UTC()
	stampGatewayReadValues(out, readAt, readAt)
	s.recordGatewayReadSamples(deviceID, out, readAt)
	return out, nil
}

func (s *server) recordGatewayReadSamples(deviceID string, values []gatewayReadValue, at time.Time) {
	for _, value := range values {
		if value.Value == nil {
			continue
		}
		s.recordGraphSample(deviceID, value.ID, value.Instance, *value.Value, value.Quality, at)
	}
}

func stampGatewayReadValues(values []gatewayReadValue, at time.Time, now time.Time) {
	if at.IsZero() {
		return
	}
	age := now.Sub(at).Milliseconds()
	if age < 0 {
		age = 0
	}
	ts := at.UTC().Format(time.RFC3339Nano)
	for i := range values {
		if values[i].Value == nil {
			continue
		}
		ageMS := age
		values[i].At = ts
		values[i].AgeMS = &ageMS
	}
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
	out := gatewayReadValue{ID: p.ID, Instance: p.Instance, Quality: gatewayQualityOK}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		out.Quality = gatewayQualityNaN
		return out
	}
	v := value
	out.Value = &v
	out.Quality = gatewayQualityForFloat(p, value)
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
	if err == nil && len(values) == len(params) {
		c.server.recordGatewayReadSamples(c.deviceID, gatewayValuesFromFloats(params, values), time.Now().UTC())
	}
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

func (c gatewayResetReadClient) ReadFloat32(ctx context.Context, paramID, instance int) (float64, error) {
	v, err := c.client.ReadFloat32(ctx, paramID, instance)
	if err != nil && shouldResetDeviceBinding(err) {
		c.server.resetDeviceBinding(c.deviceID, c.client, err)
	}
	return v, err
}

func (c gatewayResetReadClient) ReadInt32(ctx context.Context, paramID, instance int) (int32, error) {
	v, err := c.client.ReadInt32(ctx, paramID, instance)
	if err != nil && shouldResetDeviceBinding(err) {
		c.server.resetDeviceBinding(c.deviceID, c.client, err)
	}
	return v, err
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
	params := mecom.DefaultTECCatalogueEntries(1)
	for _, p := range params {
		if p.ID == id {
			return mecom.Parameter{
				ID:       p.ID,
				Instance: p.Instance,
				Name:     p.RawName,
				Unit:     p.Unit,
				Type:     mecom.DataType(p.Type),
				Writable: gatewayCatalogueWritable(p.ID),
				Role:     p.Role,
				Kind:     p.Kind,
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
	case errors.Is(err, mecom.ErrReadbackMismatch):
		return http.StatusConflict
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
