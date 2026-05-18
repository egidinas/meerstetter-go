package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/signalforge/tilehistory"
)

const (
	canonicalTileRenderer        = "signalforge.tile.uplot"
	defaultGraphHistoryRetention = 3 * 24 * time.Hour
)

type graphTileHistory struct {
	mu      sync.Mutex
	history *tilehistory.History[float64]
}

type graphTileRequestSeries struct {
	DeviceID string
	ParamID  int
	Instance int
}

type graphTileResponse struct {
	SchemaVersion  string          `json:"schema_version"`
	ID             string          `json:"id"`
	CardID         string          `json:"card_id"`
	Level          string          `json:"level"`
	T0             string          `json:"t0"`
	T1             string          `json:"t1"`
	GeneratedAt    string          `json:"generated_at"`
	Renderer       string          `json:"renderer"`
	Kind           string          `json:"kind"`
	TileID         string          `json:"tile_id"`
	Title          string          `json:"title"`
	TimeWindowMs   int             `json:"time_window_ms"`
	LatestEndpoint string          `json:"latest_endpoint"`
	TileEndpoint   string          `json:"tile_endpoint"`
	TileFiles      []tileFile      `json:"tile_files"`
	Axes           []tileAxis      `json:"axes"`
	Bands          []any           `json:"bands"`
	Markers        []any           `json:"markers"`
	Events         []any           `json:"events"`
	Diagnostics    graphTileDiag   `json:"diagnostics"`
	Provenance     map[string]any  `json:"provenance"`
	Series         []graphTileItem `json:"series"`
}

type tileFile struct {
	Level        string `json:"level"`
	TimeWindowMs int    `json:"time_window_ms"`
}

type tileAxis struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
	Unit  string `json:"unit"`
	Side  string `json:"side"`
}

type graphTileDiag struct {
	Status                         string `json:"status"`
	SeriesCount                    int    `json:"series_count"`
	PointCount                     int    `json:"point_count"`
	Decimation                     string `json:"decimation"`
	Renderer                       string `json:"renderer"`
	TileLevel                      string `json:"tile_level"`
	TileSource                     string `json:"tile_source"`
	OutlierPolicy                  string `json:"outlier_policy"`
	SuppressedOpenSensorPoints     int    `json:"suppressed_open_sensor_points,omitempty"`
	SuppressedInitialOutlierPoints int    `json:"suppressed_initial_outlier_points,omitempty"`
}

type graphTileItem struct {
	ID        string              `json:"id"`
	SeriesID  string              `json:"series_id"`
	TargetID  string              `json:"target_id,omitempty"`
	Label     string              `json:"label"`
	FullLabel string              `json:"full_label"`
	Color     string              `json:"color,omitempty"`
	Unit      string              `json:"unit"`
	History   graphTileHistorySet `json:"history"`
	Role      string              `json:"role,omitempty"`
	AxisID    string              `json:"axis_id"`
	RoleRank  int                 `json:"role_rank,omitempty"`
	SourceRef string              `json:"source_ref,omitempty"`
	Source    graphTileSource     `json:"source"`
	Points    []graphTilePoint    `json:"points"`
}

type graphTileHistorySet struct {
	TS []string  `json:"ts"`
	V  []float64 `json:"v"`
}

type graphTileSource struct {
	DeviceID string `json:"device_id"`
	Instance int    `json:"instance"`
	ParamID  int    `json:"param_id"`
	SignalID string `json:"signal_id"`
	Endpoint string `json:"endpoint,omitempty"`
}

type graphTilePoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

func (s *server) handleGraphTileRoot(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/graph/tiles/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	tileID := parts[0]
	level := "live"
	if len(parts) == 2 && parts[1] != "" {
		level = parts[1]
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	reqSeries, err := parseGraphTileSeriesQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tile, err := s.buildGraphTile(tileID, level, reqSeries)
	if err != nil {
		http.Error(w, err.Error(), httpStatusForError(err))
		return
	}
	writeJSON(w, http.StatusOK, tile)
}

func parseGraphTileSeriesQuery(q url.Values) ([]graphTileRequestSeries, error) {
	raw := append([]string(nil), q["series"]...)
	if len(raw) == 0 && q.Get("device") != "" && q.Get("param") != "" {
		raw = append(raw, fmt.Sprintf("%s:%s:%s", q.Get("device"), q.Get("param"), firstNonEmpty(q.Get("instance"), "1")))
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]graphTileRequestSeries, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		if len(parts) < 3 {
			return nil, fmt.Errorf("series %q must be device:param:instance", item)
		}
		paramID, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-2]))
		if err != nil {
			return nil, fmt.Errorf("series %q: invalid param", item)
		}
		instance, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
		if err != nil {
			return nil, fmt.Errorf("series %q: invalid instance", item)
		}
		deviceID := strings.TrimSpace(strings.Join(parts[:len(parts)-2], ":"))
		if deviceID == "" {
			return nil, fmt.Errorf("series %q: device required", item)
		}
		out = append(out, graphTileRequestSeries{DeviceID: deviceID, ParamID: paramID, Instance: instance})
	}
	return out, nil
}

func (s *server) buildGraphTile(tileID, level string, seriesReq []graphTileRequestSeries) (graphTileResponse, error) {
	now := time.Now().UTC()
	canonicalLevel, window, ok := graphTileLevel(level)
	if !ok {
		window = 90_000
		canonicalLevel = "live"
	}
	level = canonicalLevel
	series, units := s.graphTileSeries(seriesReq, window, now)
	pointCount := 0
	for _, item := range series {
		pointCount += len(item.Points)
	}
	title := tileID
	if title == "" {
		title = "graph tile"
	}
	return graphTileResponse{
		SchemaVersion:  "signalforge.graph_tile.v1",
		ID:             tileID,
		CardID:         tileID,
		Level:          level,
		T0:             now.Add(-time.Duration(window) * time.Millisecond).Format(time.RFC3339Nano),
		T1:             now.Format(time.RFC3339Nano),
		GeneratedAt:    now.Format(time.RFC3339Nano),
		Renderer:       canonicalTileRenderer,
		Kind:           "timeseries",
		TileID:         tileID,
		Title:          title,
		TimeWindowMs:   window,
		LatestEndpoint: fmt.Sprintf("/api/graph/tiles/%s/live", url.PathEscape(tileID)),
		TileEndpoint:   fmt.Sprintf("/api/graph/tiles/%s/%s", url.PathEscape(tileID), level),
		TileFiles:      graphTileFiles(),
		Axes:           axesFromUnits(units),
		Bands:          []any{},
		Markers:        []any{},
		Events:         []any{},
		Diagnostics:    graphTileDiag{Status: statusForSeries(len(series)), SeriesCount: len(series), PointCount: pointCount, Decimation: "1Hz mean", Renderer: canonicalTileRenderer, TileLevel: level, TileSource: sourceForLevel(level), OutlierPolicy: "drop_detached_degC_below_-50_and_initial_out_of_family"},
		Provenance:     map[string]any{"source": "meerstetter-go.graphwall.assignments", "generated_at": now.Format(time.RFC3339Nano)},
		Series:         series,
	}, nil
}

func graphTileLevel(level string) (string, int, bool) {
	switch level {
	case "live":
		return "live", 90_000, true
	case "minute":
		return "minute", 6 * 60_000, true
	case "hour":
		return "hour", 60 * 60_000, true
	case "three_hour", "3hour", "3h":
		return "three_hour", 3 * 60 * 60_000, true
	case "day":
		return "day", 24 * 60 * 60_000, true
	case "three_day", "3day":
		return "three_day", 3 * 24 * 60 * 60_000, true
	default:
		return "", 0, false
	}
}

func graphTileFiles() []tileFile {
	return []tileFile{
		{Level: "live", TimeWindowMs: 90_000},
		{Level: "minute", TimeWindowMs: 6 * 60_000},
		{Level: "hour", TimeWindowMs: 60 * 60_000},
		{Level: "three_hour", TimeWindowMs: 3 * 60 * 60_000},
		{Level: "day", TimeWindowMs: 24 * 60 * 60_000},
		{Level: "three_day", TimeWindowMs: 3 * 24 * 60 * 60_000},
	}
}

func (s *server) graphTileSeries(req []graphTileRequestSeries, windowMs int, now time.Time) ([]graphTileItem, []string) {
	if len(req) == 0 {
		req = s.defaultGraphTileSeries()
	}
	sorted := append([]graphTileRequestSeries(nil), req...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri := s.deviceSortRank(sorted[i].DeviceID)
		rj := s.deviceSortRank(sorted[j].DeviceID)
		if ri != rj {
			return ri < rj
		}
		if sorted[i].DeviceID != sorted[j].DeviceID {
			return sorted[i].DeviceID < sorted[j].DeviceID
		}
		if sorted[i].ParamID != sorted[j].ParamID {
			return sorted[i].ParamID < sorted[j].ParamID
		}
		return sorted[i].Instance < sorted[j].Instance
	})
	units := []string{}
	out := make([]graphTileItem, 0, len(sorted))
	for _, item := range sorted {
		def, _ := gatewayParameterByID(item.ParamID)
		unit := def.Unit
		if unit == "" {
			unit = "_"
		}
		if !containsString(units, unit) {
			units = append(units, unit)
		}
		history := s.lookupGraphHistory(item.DeviceID, item.ParamID, item.Instance, windowMs, now)
		points := pointsFromHistory(history)
		if len(points) == 0 {
			if liveValue, ok := s.readLiveSample(context.Background(), item.DeviceID, item.ParamID, item.Instance); ok {
				past := now.Add(-time.Second)
				points = []graphTilePoint{{Timestamp: past.Format(time.RFC3339Nano), Value: liveValue}, {Timestamp: now.Format(time.RFC3339Nano), Value: liveValue}}
				history = graphTileHistorySet{TS: []string{past.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)}, V: []float64{liveValue, liveValue}}
			}
		}
		if len(points) == 0 {
			points = []graphTilePoint{{Timestamp: now.Add(-time.Second).Format(time.RFC3339Nano), Value: 0}, {Timestamp: now.Format(time.RFC3339Nano), Value: 0}}
		} else if len(points) == 1 {
			points = append([]graphTilePoint{{Timestamp: now.Add(-time.Second).Format(time.RFC3339Nano), Value: points[0].Value}}, points...)
			history = graphTileHistorySet{TS: []string{points[0].Timestamp, points[1].Timestamp}, V: []float64{points[0].Value, points[1].Value}}
		}
		out = append(out, graphTileItem{
			ID:        tileSeriesKeyFromRequest(item),
			SeriesID:  tileSeriesKeyFromRequest(item),
			Label:     compactSeriesLabel(item.DeviceID, item.Instance, def),
			FullLabel: fmt.Sprintf("device %s · param %d · instance %d · %s", item.DeviceID, item.ParamID, item.Instance, seriesPathLabel(def)),
			Unit:      unit,
			History:   history,
			AxisID:    graphAxisIDForUnit(unit, ""),
			Source: graphTileSource{
				DeviceID: item.DeviceID,
				Instance: item.Instance,
				ParamID:  item.ParamID,
				SignalID: signalIDForParam(def),
			},
			Points: points,
		})
	}
	return out, units
}

func (s *server) defaultGraphTileSeries() []graphTileRequestSeries {
	out := make([]graphTileRequestSeries, 0, len(s.devices))
	for _, b := range s.orderedDeviceBindings() {
		out = append(out, graphTileRequestSeries{DeviceID: b.cfg.ID, ParamID: 1000, Instance: 1})
	}
	return out
}

func (s *server) deviceSortRank(id string) int {
	if s.defaultDeviceID != "" && id == s.defaultDeviceID {
		return 0
	}
	if s.defaultDeviceID == "" && id == "tec-76" {
		return 0
	}
	return 1
}

func tileSeriesKeyFromRequest(req graphTileRequestSeries) string {
	return fmt.Sprintf("%s:%d:%d", req.DeviceID, req.ParamID, req.Instance)
}

func seriesPathLabel(def mecom.Parameter) string {
	if def.Name == "" {
		return fmt.Sprintf("#%d", def.ID)
	}
	return def.Name
}

func compactSeriesLabel(deviceID string, instance int, def mecom.Parameter) string {
	return fmt.Sprintf("%s-ch%d %s", compactDeviceLabel(deviceID), instance, compactParamLabel(def))
}

func compactDeviceLabel(deviceID string) string {
	text := strings.TrimSpace(deviceID)
	if text == "" {
		return "device"
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	last := parts[len(parts)-1]
	if n, err := strconv.Atoi(last); err == nil {
		return fmt.Sprintf("SN%d", n)
	}
	return text
}

func compactParamLabel(def mecom.Parameter) string {
	name := strings.ToLower(strings.Join(strings.Fields(def.Name), " "))
	switch name {
	case "object temperature":
		return "OT"
	case "sink temperature":
		return "ST"
	case "target object temperature", "nominal object temperature":
		return "NOT"
	case "output power":
		return "OP"
	case "output voltage":
		return "OV"
	case "output current":
		return "OC"
	}
	if def.Name != "" {
		parts := strings.Fields(def.Name)
		initials := make([]string, 0, len(parts))
		for _, part := range parts {
			if part == "" {
				continue
			}
			initials = append(initials, strings.ToUpper(part[:1]))
		}
		if len(initials) > 0 {
			return strings.Join(initials, "")
		}
	}
	return fmt.Sprintf("#%d", def.ID)
}

func signalIDForParam(def mecom.Parameter) string {
	if def.Name == "" {
		return strconv.Itoa(def.ID)
	}
	return def.Name
}

func graphAxisIDForUnit(unit, role string) string {
	u := strings.ToLower(strings.TrimSpace(unit))
	switch {
	case u == "a":
		return "current_a"
	case u == "v":
		return "voltage_v"
	case u == "w":
		return "power_w"
	case u == "%" || u == "percent":
		return "percent"
	case u == "ms":
		return "bus_ms"
	case u == "s" || u == "sec" || u == "seconds":
		return "seconds"
	case strings.Contains(u, "deg") || u == "c" || u == "degc":
		return "temperature_c"
	case role == "counter":
		return "counter"
	default:
		return "generic_numeric"
	}
}

func axesFromUnits(units []string) []tileAxis {
	out := make([]tileAxis, 0, 2)
	for i, unit := range units {
		if i > 1 {
			break
		}
		side := "right"
		if i == 0 {
			side = "left"
		}
		out = append(out, tileAxis{ID: graphAxisIDForUnit(unit, ""), Label: axisLabelForUnit(unit), Unit: unit, Side: side})
	}
	return out
}

func axisLabelForUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "degc", "c":
		return "Temperature [°C]"
	case "v":
		return "Voltage [V]"
	case "a":
		return "Current [A]"
	case "w":
		return "Power [W]"
	case "%", "percent":
		return "Percent [%]"
	default:
		if strings.TrimSpace(unit) == "" || strings.TrimSpace(unit) == "_" {
			return "Value"
		}
		return fmt.Sprintf("Value [%s]", unit)
	}
}

func statusForSeries(seriesCount int) string {
	if seriesCount > 0 {
		return "ok"
	}
	return "empty"
}

func sourceForLevel(level string) string {
	if level == "live" {
		return "bounded-gateway-live-cache"
	}
	return "bounded-gateway-history-cache"
}

func pointsFromHistory(history graphTileHistorySet) []graphTilePoint {
	out := make([]graphTilePoint, 0, len(history.TS))
	for i := range history.TS {
		if i >= len(history.V) {
			break
		}
		out = append(out, graphTilePoint{Timestamp: history.TS[i], Value: history.V[i]})
	}
	return out
}

func (s *server) lookupGraphHistory(deviceID string, paramID, instance, windowMs int, now time.Time) graphTileHistorySet {
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistory[key]
	s.graphHistoryMu.Unlock()
	if h == nil {
		return graphTileHistorySet{}
	}
	cutoff := now.Add(-time.Duration(windowMs) * time.Millisecond)
	h.mu.Lock()
	snapshot := h.history.Snapshot()
	h.mu.Unlock()
	out := graphTileHistorySet{TS: make([]string, 0, len(snapshot.Buckets)), V: make([]float64, 0, len(snapshot.Buckets))}
	for _, bucket := range snapshot.Buckets {
		if bucket.IntervalStart.Before(cutoff) {
			continue
		}
		out.TS = append(out.TS, bucket.IntervalStart.Format(time.RFC3339Nano))
		out.V = append(out.V, bucket.Mean)
	}
	return out
}

func (s *server) recordGraphSample(deviceID string, paramID, instance int, value float64, quality string, at time.Time) {
	_ = quality
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistory[key]
	if h == nil {
		h = &graphTileHistory{history: tilehistory.New[float64](defaultGraphHistoryRetention)}
		s.graphHistory[key] = h
	}
	s.graphHistoryMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history.Add(tilehistory.Sample[float64]{Timestamp: at, Value: value})
}

func (s *server) readLiveSample(ctx context.Context, deviceID string, paramID, instance int) (float64, bool) {
	bound, err := s.bind(deviceID)
	if err != nil {
		return 0, false
	}
	def, ok := gatewayParameterByID(paramID)
	if !ok {
		return 0, false
	}
	def.Instance = instance
	values, err := bound.client.ReadBulk(ctx, []mecom.Parameter{def})
	if err != nil || len(values) != 1 || math.IsNaN(values[0]) || math.IsInf(values[0], 0) {
		return 0, false
	}
	s.recordGraphSample(deviceID, paramID, instance, values[0], "ok", time.Now().UTC())
	return values[0], true
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
