package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/signalforge/arrowtelemetry"
	"github.com/egidinas/signalforge/tilehistory"
)

const (
	canonicalTileRenderer        = "signalforge.tile.uplot"
	defaultGraphTileLevel        = "three_day"
	graphHistoryHotRetention     = 15 * time.Minute
	defaultGraphHistoryRetention = 3 * 24 * time.Hour
	maxGraphHistoryImportSeries  = 512
	maxGraphHistoryImportSamples = 1_000_000
	maxGraphHistoryImportBytes   = 16 << 20
)

type graphTileRawSample struct {
	At    time.Time
	Value float64
}

type graphTileHistory struct {
	mu           sync.Mutex
	raw          []graphTileRawSample // 15m high-res (CRTVStream)
	pyramid      *tilehistory.Pyramid // Multi-level derived history (LOD)
	lastRawPurge time.Time
	hotLatest    time.Time
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
	ID               string              `json:"id"`
	SeriesID         string              `json:"series_id"`
	TargetID         string              `json:"target_id,omitempty"`
	Label            string              `json:"label"`
	FullLabel        string              `json:"full_label"`
	Color            string              `json:"color,omitempty"`
	Unit             string              `json:"unit"`
	History          graphTileHistorySet `json:"history"`
	Role             string              `json:"role,omitempty"`
	Kind             string              `json:"kind,omitempty"`
	AxisID           string              `json:"axis_id"`
	RoleRank         int                 `json:"role_rank,omitempty"`
	SourceRef        string              `json:"source_ref,omitempty"`
	Source           graphTileSource     `json:"source"`
	Quality          string              `json:"quality,omitempty"`
	DefaultVisible   bool                `json:"default_visible"`
	VisibilityReason string              `json:"visibility_reason,omitempty"`
	Diagnostics      graphTileItemDiag   `json:"diagnostics,omitempty"`
	Points           []graphTilePoint    `json:"points"`
}

type graphTileItemDiag struct {
	Status                     string `json:"status,omitempty"`
	HistoryPoints              int    `json:"history_points,omitempty"`
	LiveRead                   string `json:"live_read,omitempty"`
	Message                    string `json:"message,omitempty"`
	SuppressedOpenSensorPoints int    `json:"suppressed_open_sensor_points,omitempty"`
}

type graphTileHistorySet struct {
	TS []string  `json:"ts"`
	V  []float64 `json:"v"`
}

func normalizeGraphTileHistorySet(history graphTileHistorySet) graphTileHistorySet {
	if history.TS == nil {
		history.TS = []string{}
	}
	if history.V == nil {
		history.V = []float64{}
	}
	return history
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

type graphHistoryImportRequest struct {
	SchemaVersion string                     `json:"schema_version,omitempty"`
	Source        string                     `json:"source,omitempty"`
	Series        []graphHistoryImportSeries `json:"series"`
}

type graphHistoryImportSeries struct {
	DeviceID string              `json:"device_id,omitempty"`
	ParamID  int                 `json:"param_id,omitempty"`
	Instance int                 `json:"instance,omitempty"`
	Source   graphTileSource     `json:"source,omitempty"`
	History  graphTileHistorySet `json:"history,omitempty"`
	Points   []graphTilePoint    `json:"points,omitempty"`
	Quality  string              `json:"quality,omitempty"`
}

type graphHistoryImportResponse struct {
	Status          string `json:"status"`
	SeriesCount     int    `json:"series_count"`
	ImportedSamples int    `json:"imported_samples"`
	DroppedSamples  int    `json:"dropped_samples,omitempty"`
}

type graphHistoryExportResponse struct {
	SchemaVersion string                     `json:"schema_version"`
	Source        string                     `json:"source"`
	GeneratedAt   string                     `json:"generated_at"`
	Level         string                     `json:"level"`
	TimeWindowMs  int                        `json:"time_window_ms"`
	SeriesCount   int                        `json:"series_count"`
	SampleCount   int                        `json:"sample_count"`
	Series        []graphHistoryImportSeries `json:"series"`
}

type graphHistoryImportSample struct {
	At    time.Time
	Value float64
}

type graphHistoryImportSeriesPlan struct {
	DeviceID string
	ParamID  int
	Instance int
	Quality  string
	Samples  []graphHistoryImportSample
}

func (s *server) handleGraphHistoryImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req graphHistoryImportRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxGraphHistoryImportBytes))
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Series) == 0 {
		http.Error(w, "series required", http.StatusBadRequest)
		return
	}
	if len(req.Series) > maxGraphHistoryImportSeries {
		http.Error(w, "too many series", http.StatusRequestEntityTooLarge)
		return
	}

	plans := make([]graphHistoryImportSeriesPlan, 0, len(req.Series))
	imported := 0
	dropped := 0
	for _, series := range req.Series {
		deviceID, paramID, instance, err := resolveGraphHistoryImportIdentity(series)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, ok := s.devices[deviceID]; !ok {
			http.Error(w, "unknown device "+deviceID, http.StatusBadRequest)
			return
		}
		samples, sampleDropped, err := parseGraphHistoryImportSamples(series)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		dropped += sampleDropped
		if len(samples) == 0 {
			continue
		}
		if imported+len(samples) > maxGraphHistoryImportSamples {
			http.Error(w, "too many samples", http.StatusRequestEntityTooLarge)
			return
		}
		sort.Slice(samples, func(i, j int) bool {
			return samples[i].At.Before(samples[j].At)
		})
		imported += len(samples)
		plans = append(plans, graphHistoryImportSeriesPlan{
			DeviceID: deviceID,
			ParamID:  paramID,
			Instance: instance,
			Quality:  series.Quality,
			Samples:  samples,
		})
	}
	if imported == 0 {
		http.Error(w, "no valid samples to import", http.StatusBadRequest)
		return
	}
	for _, plan := range plans {
		for _, sample := range plan.Samples {
			s.recordGraphSample(plan.DeviceID, plan.ParamID, plan.Instance, sample.Value, plan.Quality, sample.At)
		}
	}
	writeJSON(w, http.StatusOK, graphHistoryImportResponse{Status: "ok", SeriesCount: len(req.Series), ImportedSamples: imported, DroppedSamples: dropped})
}

func (s *server) handleGraphHistoryExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	level := firstNonEmpty(r.URL.Query().Get("level"), "three_day")
	canonicalLevel, windowMs, ok := graphTileLevel(level)
	if !ok {
		http.Error(w, "invalid graph history level", http.StatusBadRequest)
		return
	}
	reqSeries, err := parseGraphTileSeriesQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(reqSeries) == 0 {
		reqSeries = s.allGraphHistorySeries()
	}

	now := time.Now().UTC()
	series := make([]graphHistoryImportSeries, 0, len(reqSeries))
	sampleCount := 0
	for _, req := range reqSeries {
		history := s.lookupGraphHistory(req.DeviceID, req.ParamID, req.Instance, windowMs, now)
		if len(history.TS) == 0 {
			continue
		}
		def, _ := gatewayParameterByID(req.ParamID)
		points := pointsFromHistory(history)
		quality := gatewayQualityOK
		if len(points) == 0 {
			quality = gatewayQualityMissing
		} else if countDetachedTemperaturePoints(req.ParamID, def.Unit, points) == len(points) {
			quality = gatewayQualityDetached
		}
		series = append(series, graphHistoryImportSeries{
			DeviceID: req.DeviceID,
			ParamID:  req.ParamID,
			Instance: req.Instance,
			Source: graphTileSource{
				DeviceID: req.DeviceID,
				Instance: req.Instance,
				ParamID:  req.ParamID,
				SignalID: signalIDForParam(def),
				Endpoint: s.graphHistoryEndpoint(req.DeviceID),
			},
			History: history,
			Quality: quality,
		})
		sampleCount += len(history.TS)
	}
	writeJSON(w, http.StatusOK, graphHistoryExportResponse{
		SchemaVersion: "signalforge.graph_tile.v1",
		Source:        "meerstetter-go.graph-history",
		GeneratedAt:   now.Format(time.RFC3339Nano),
		Level:         canonicalLevel,
		TimeWindowMs:  windowMs,
		SeriesCount:   len(series),
		SampleCount:   sampleCount,
		Series:        series,
	})
}

func resolveGraphHistoryImportIdentity(series graphHistoryImportSeries) (string, int, int, error) {
	topDeviceID := strings.TrimSpace(series.DeviceID)
	sourceDeviceID := strings.TrimSpace(series.Source.DeviceID)
	if topDeviceID != "" && sourceDeviceID != "" && topDeviceID != sourceDeviceID {
		return "", 0, 0, fmt.Errorf("device_id mismatch between series and source")
	}
	deviceID := firstNonEmpty(topDeviceID, sourceDeviceID)
	if deviceID == "" {
		return "", 0, 0, fmt.Errorf("device_id required")
	}

	paramID := series.ParamID
	if paramID != 0 && series.Source.ParamID != 0 && paramID != series.Source.ParamID {
		return "", 0, 0, fmt.Errorf("param_id mismatch between series and source")
	}
	if paramID == 0 {
		paramID = series.Source.ParamID
	}
	if paramID <= 0 {
		return "", 0, 0, fmt.Errorf("param_id required")
	}

	instance := series.Instance
	if instance != 0 && series.Source.Instance != 0 && instance != series.Source.Instance {
		return "", 0, 0, fmt.Errorf("instance mismatch between series and source")
	}
	if instance == 0 {
		instance = series.Source.Instance
	}
	if instance <= 0 {
		return "", 0, 0, fmt.Errorf("instance required")
	}
	return deviceID, paramID, instance, nil
}

func parseGraphHistoryImportSamples(series graphHistoryImportSeries) ([]graphHistoryImportSample, int, error) {
	if len(series.History.TS) != len(series.History.V) {
		return nil, 0, fmt.Errorf("history timestamp/value length mismatch")
	}
	out := make([]graphHistoryImportSample, 0, len(series.History.TS)+len(series.Points))
	dropped := 0
	for i, rawTS := range series.History.TS {
		at, err := parseGraphHistoryTimestamp(rawTS)
		if err != nil || !isFiniteFloat(series.History.V[i]) {
			dropped++
			continue
		}
		out = append(out, graphHistoryImportSample{At: at, Value: series.History.V[i]})
	}
	for _, point := range series.Points {
		at, err := parseGraphHistoryTimestamp(point.Timestamp)
		if err != nil || !isFiniteFloat(point.Value) {
			dropped++
			continue
		}
		out = append(out, graphHistoryImportSample{At: at, Value: point.Value})
	}
	return out, dropped, nil
}

func parseGraphHistoryTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("timestamp required")
	}
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid timestamp format: %q", raw)
}

func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *server) handleGraphTileRoot(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/graph/tiles/")
	if rest == "" {
		http.NotFound(w, r)
		return
	}
	parts := strings.SplitN(rest, "/", 2)
	tileID := parts[0]
	level := defaultGraphTileLevel
	if len(parts) == 2 && parts[1] != "" {
		level = parts[1]
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	reqSeries, err := parseGraphTileSeriesQuery(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	t0Raw := q.Get("t0")
	t1Raw := q.Get("t1")
	var t0, t1 time.Time
	customRange := false
	
	if t0Raw != "" && t1Raw != "" {
		var err0, err1 error
		t0, err0 = parseGraphHistoryTimestamp(t0Raw)
		t1, err1 = parseGraphHistoryTimestamp(t1Raw)
		if err0 == nil && err1 == nil {
			customRange = true
		}
	}

	if r.Header.Get("X-Format") == "arrow" || q.Get("format") == "arrow" {
		if customRange {
			s.handleGraphTileArrowRange(w, r, tileID, t0, t1, reqSeries)
		} else {
			s.handleGraphTileArrow(w, r, tileID, level, reqSeries)
		}
		return
	}
	
	var tile graphTileResponse
	if customRange {
		tile, err = s.buildGraphTileRange(tileID, t0, t1, reqSeries)
	} else {
		tile, err = s.buildGraphTile(tileID, level, reqSeries)
	}
	
	if err != nil {
		http.Error(w, err.Error(), httpStatusForError(err))
		return
	}
	writeJSON(w, http.StatusOK, tile)
}

func (s *server) handleGraphAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	s.graphHistoryMu.Lock()
	defer s.graphHistoryMu.Unlock()
	
	type availabilityItem struct {
		ID       string    `json:"id"`
		Earliest time.Time `json:"earliest"`
		Latest   time.Time `json:"latest"`
		Count    int       `json:"count"`
	}
	
	out := make([]availabilityItem, 0, len(s.graphHistoryDerived))
	for key, h := range s.graphHistoryDerived {
		h.mu.Lock()
		earliest := h.pyramid.Earliest()
		latest := h.pyramid.Latest()
		h.mu.Unlock()
		
		if earliest.IsZero() {
			continue
		}
		
		out = append(out, availabilityItem{
			ID:       key,
			Earliest: earliest,
			Latest:   latest,
			Count:    0, // Not tracked at top level
		})
	}
	
	writeJSON(w, http.StatusOK, map[string]any{"availability": out})
}

func (s *server) handleGraphTileArrow(w http.ResponseWriter, r *http.Request, tileID, level string, seriesReq []graphTileRequestSeries) {
	now := time.Now().UTC()
	canonicalLevel, window, ok := graphTileLevel(level)
	if !ok {
		canonicalLevel, window, _ = graphTileLevel(defaultGraphTileLevel)
	}
	level = canonicalLevel

	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	w.WriteHeader(http.StatusOK)

	writer := ipc.NewWriter(w, ipc.WithSchema(arrowtelemetry.TelemetrySchema))
	defer writer.Close()

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowtelemetry.TelemetrySchema)
	defer builder.Release()

	tsBuilder := builder.Field(0).(*array.Int64Builder)
	sensorBuilder := builder.Field(1).(*array.BinaryDictionaryBuilder)
	valueBuilder := builder.Field(2).(*array.Float64Builder)
	unitBuilder := builder.Field(3).(*array.BinaryDictionaryBuilder)
	campaignBuilder := builder.Field(4).(*array.BinaryDictionaryBuilder)
	sourceBuilder := builder.Field(5).(*array.BinaryDictionaryBuilder)
	roleBuilder := builder.Field(6).(*array.BinaryDictionaryBuilder)
	kindBuilder := builder.Field(7).(*array.BinaryDictionaryBuilder)
	familyBuilder := builder.Field(8).(*array.BinaryDictionaryBuilder)
	qualityBuilder := builder.Field(9).(*array.BinaryDictionaryBuilder)
	stateBuilder := builder.Field(10).(*array.BinaryDictionaryBuilder)

	campaignID := fmt.Sprintf("tile-%s-%s-%d", tileID, level, now.Unix())

	if len(seriesReq) == 0 {
		seriesReq = s.defaultGraphTileSeries(tileID)
	}

	for _, req := range seriesReq {
		h := s.lookupGraphHistory(req.DeviceID, req.ParamID, req.Instance, window, now)
		param, _ := gatewayParameterByID(req.ParamID)
		
		sensorID := fmt.Sprintf("%s:%d:%d", req.DeviceID, req.ParamID, req.Instance)
		unit := param.Unit
		if unit == "" {
			unit = "_"
		}

		for i := range h.TS {
			at, _ := parseGraphHistoryTimestamp(h.TS[i])
			
			tsBuilder.Append(at.UnixNano())
			_ = sensorBuilder.AppendString(sensorID)
			valueBuilder.Append(h.V[i])
			_ = unitBuilder.AppendString(unit)
			_ = campaignBuilder.AppendString(campaignID)
			_ = sourceBuilder.AppendString(s.graphHistoryEndpoint(req.DeviceID))
			_ = roleBuilder.AppendString(param.Role)
			_ = kindBuilder.AppendString(param.Kind)
			_ = familyBuilder.AppendString("mecom")
			_ = qualityBuilder.AppendString(gatewayQualityOK)
			stateBuilder.AppendNull()
		}

		if tsBuilder.Len() >= arrowtelemetry.BatchSize {
			record := builder.NewRecord()
			if err := writer.Write(record); err != nil {
				record.Release()
				return
			}
			record.Release()
		}
	}

	if tsBuilder.Len() > 0 {
		record := builder.NewRecord()
		_ = writer.Write(record)
		record.Release()
	}
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
		canonicalLevel, window, _ = graphTileLevel(defaultGraphTileLevel)
	}
	level = canonicalLevel
	series, units := s.graphTileSeries(tileID, seriesReq, level, window, now)
	pointCount := 0
	suppressedOpenSensorPoints := 0
	for _, item := range series {
		pointCount += len(item.Points)
		suppressedOpenSensorPoints += item.Diagnostics.SuppressedOpenSensorPoints
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
		Diagnostics:    graphTileDiag{Status: statusForSeries(len(series)), SeriesCount: len(series), PointCount: pointCount, Decimation: decimationForLevel(level), Renderer: canonicalTileRenderer, TileLevel: level, TileSource: sourceForLevel(level), OutlierPolicy: "drop_detached_degC_below_-50_and_initial_out_of_family", SuppressedOpenSensorPoints: suppressedOpenSensorPoints},
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

func (s *server) graphTileSeries(tileID string, req []graphTileRequestSeries, level string, windowMs int, now time.Time) ([]graphTileItem, []string) {
	if len(req) == 0 {
		req = s.defaultGraphTileSeries(tileID)
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
		itemDiag := graphTileItemDiag{Status: "ok", HistoryPoints: len(points)}
		if len(points) == 0 && level == "live" {
			if liveValue, ok := s.readLiveSample(context.Background(), item.DeviceID, item.ParamID, item.Instance); ok {
				past := now.Add(-time.Second)
				points = []graphTilePoint{{Timestamp: past.Format(time.RFC3339Nano), Value: liveValue}, {Timestamp: now.Format(time.RFC3339Nano), Value: liveValue}}
				history = graphTileHistorySet{TS: []string{past.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)}, V: []float64{liveValue, liveValue}}
				itemDiag.LiveRead = "ok"
				itemDiag.HistoryPoints = len(points)
			}
		}
		if len(points) == 0 {
			itemDiag.Status = "missing"
			if level == "live" {
				itemDiag.LiveRead = "unavailable"
				itemDiag.Message = "no history and live read unavailable"
			} else {
				itemDiag.LiveRead = "not_attempted"
				itemDiag.Message = "no archive history for selected tile level"
			}
		} else if len(points) == 1 {
			before, point := duplicateSingleGraphPoint(points[0], now)
			points = []graphTilePoint{before, point}
			history = graphTileHistorySet{TS: []string{points[0].Timestamp, points[1].Timestamp}, V: []float64{points[0].Value, points[1].Value}}
			itemDiag.HistoryPoints = len(points)
		}
		quality := gatewayQualityOK
		if len(points) == 0 {
			quality = gatewayQualityMissing
		} else {
			detachedPoints := countDetachedTemperaturePoints(item.ParamID, unit, points)
			if detachedPoints > 0 {
				itemDiag.SuppressedOpenSensorPoints = detachedPoints
			}
			if seriesLatestPointIsDetached(item.ParamID, unit, points) || detachedPoints == len(points) {
				quality = gatewayQualityDetached
				itemDiag.Status = gatewayQualityDetached
				itemDiag.Message = "measured temperature is below the detached-sensor floor"
			}
		}
		defaultVisible, visibilityReason := graphTileSeriesDefaultVisibility(quality, itemDiag)
		out = append(out, graphTileItem{
			ID:               tileSeriesKeyFromRequest(item),
			SeriesID:         tileSeriesKeyFromRequest(item),
			Label:            compactSeriesLabel(item.DeviceID, item.Instance, def),
			FullLabel:        fmt.Sprintf("device %s · param %d · instance %d · %s", item.DeviceID, item.ParamID, item.Instance, seriesPathLabel(def)),
			Unit:             unit,
			History:          normalizeGraphTileHistorySet(history),
			AxisID:           graphAxisIDForUnit(unit, ""),
			DefaultVisible:   defaultVisible,
			VisibilityReason: visibilityReason,
			Source: graphTileSource{
				DeviceID: item.DeviceID,
				Instance: item.Instance,
				ParamID:  item.ParamID,
				SignalID: signalIDForParam(def),
			},
			Quality:     quality,
			Diagnostics: itemDiag,
			Points:      points,
		})
	}
	return out, units
}

func duplicateSingleGraphPoint(point graphTilePoint, now time.Time) (graphTilePoint, graphTilePoint) {
	at, err := parseGraphHistoryTimestamp(point.Timestamp)
	if err != nil || at.IsZero() {
		at = now.UTC()
		point.Timestamp = at.Format(time.RFC3339Nano)
	}
	return graphTilePoint{Timestamp: at.Add(-time.Second).Format(time.RFC3339Nano), Value: point.Value}, point
}

func graphTileSeriesDefaultVisibility(quality string, diag graphTileItemDiag) (bool, string) {
	switch quality {
	case "", gatewayQualityOK:
		return true, ""
	case gatewayQualityMissing:
		return false, "hidden by default because no live value or history is available"
	case gatewayQualityDetached:
		if diag.Message != "" {
			return false, "hidden by default because " + diag.Message
		}
		return false, "hidden by default because the measured temperature is below the detached-sensor floor"
	default:
		return false, fmt.Sprintf("hidden by default because source quality is %s", quality)
	}
}

func (s *server) defaultGraphTileSeries(tileID string) []graphTileRequestSeries {
	paramID := 1000
	role := "temp"
	switch defaultGraphTileKind(tileID) {
	case "power":
		paramID = 1022
		role = "supply"
	case "voltage":
		paramID = 1021
		role = "supply"
	case "current":
		paramID = 1020
		role = "supply"
	}
	out := make([]graphTileRequestSeries, 0, len(s.devices))
	for _, b := range s.orderedDeviceBindings() {
		for _, instance := range defaultGraphTileInstances(b.cfg, s.channelCount, role) {
			out = append(out, graphTileRequestSeries{DeviceID: b.cfg.ID, ParamID: paramID, Instance: instance})
		}
	}
	return out
}

func defaultGraphTileKind(tileID string) string {
	id := strings.ToLower(strings.TrimSpace(tileID))
	switch {
	case strings.Contains(id, "voltage"):
		return "voltage"
	case strings.Contains(id, "current"):
		return "current"
	case strings.Contains(id, "supply") || strings.Contains(id, "power"):
		return "power"
	default:
		return "temperature"
	}
}

func defaultGraphTileInstances(cfg DeviceConfig, fallbackCount int, role string) []int {
	count := effectiveChannelCount(cfg.ChannelCount, fallbackCount)
	instances := make([]int, 0, count)
	for i := 1; i <= count; i++ {
		if role != "" {
			channelRole, _ := effectiveDeviceChannelRole(cfg, i)
			if channelRole != role {
				continue
			}
		}
		instances = append(instances, i)
	}
	return instances
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
	switch def.ID {
	case 1000:
		return "OT"
	case 1001:
		return "ST"
	case 3000:
		return "NOT"
	case 1022:
		return "OP"
	case 1021:
		return "OV"
	case 1020:
		return "OC"
	}
	name := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer("_", " ", "-", " ", "/", " ").Replace(def.Name)), " "))
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
	switch {
	case strings.Contains(name, "object temp") && !strings.Contains(name, "target") && !strings.Contains(name, "nominal"):
		return "OT"
	case strings.Contains(name, "sink temp"):
		return "ST"
	case strings.Contains(name, "target") && strings.Contains(name, "temp"):
		return "NOT"
	case strings.Contains(name, "nominal") && strings.Contains(name, "temp"):
		return "NOT"
	case strings.Contains(name, "output power"):
		return "OP"
	case strings.Contains(name, "output voltage"):
		return "OV"
	case strings.Contains(name, "output current"):
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

func graphUnitIsTemperature(unit string) bool {
	u := strings.ToLower(strings.TrimSpace(unit))
	return strings.Contains(u, "deg") || strings.Contains(u, "°") || strings.Contains(u, "celsius") || u == "c" || u == "degc"
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
	if graphTileLevelUsesHotBuffer(level) {
		return "bounded-gateway-hot-buffer"
	}
	return "bounded-gateway-history-cache"
}

func decimationForLevel(level string) string {
	if graphTileLevelUsesHotBuffer(level) {
		return "raw hot buffer"
	}
	return "1Hz mean"
}

func graphTileLevelUsesHotBuffer(level string) bool {
	switch level {
	case "live", "minute":
		return true
	default:
		return false
	}
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
	t0 := now.Add(-time.Duration(windowMs) * time.Millisecond)
	return s.lookupGraphRange(deviceID, paramID, instance, t0, now)
}

func (s *server) lookupGraphRange(deviceID string, paramID, instance int, t0, t1 time.Time) graphTileHistorySet {
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistoryDerived[key]
	if h == nil {
		h = s.graphHistoryRaw[key]
	}
	s.graphHistoryMu.Unlock()
	if h == nil {
		return normalizeGraphTileHistorySet(graphTileHistorySet{})
	}
	
	h.mu.Lock()
	defer h.mu.Unlock()

	window := t1.Sub(t0)
	if window <= 15*time.Minute && len(h.raw) > 0 {
		return h.hotHistoryLocked(t0)
	}

	// 1000 points target for UI performance
	targetInterval := window / 1000
	snapshot := h.pyramid.Snapshot(targetInterval)
	out := graphTileHistorySet{TS: make([]string, 0, len(snapshot.Buckets)), V: make([]float64, 0, len(snapshot.Buckets))}
	for _, bucket := range snapshot.Buckets {
		if bucket.IntervalStart.Before(t0) || bucket.IntervalStart.After(t1) {
			continue
		}
		out.TS = append(out.TS, bucket.IntervalStart.Format(time.RFC3339Nano))
		out.V = append(out.V, bucket.Mean)
	}
	return out
}

func newGraphTileHistory() *graphTileHistory {
	return &graphTileHistory{
		pyramid: tilehistory.NewPyramid(
			tilehistory.LevelSpec{Interval: 2 * time.Second, Retention: 3 * 24 * time.Hour},
			tilehistory.LevelSpec{Interval: 10 * time.Second, Retention: 7 * 24 * time.Hour},
			tilehistory.LevelSpec{Interval: time.Minute, Retention: 30 * 24 * time.Hour},
		),
	}
}

func (h *graphTileHistory) addSampleLocked(at time.Time, value float64, isRaw bool) {
	if !isFiniteFloat(value) {
		return
	}
	at = at.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if at.After(h.hotLatest) {
		h.hotLatest = at
	}
	if isRaw {
		h.raw = append(h.raw, graphTileRawSample{At: at, Value: value})
		h.trimRawLocked()
	} else {
		h.pyramid.Add(at, value)
	}
}

func (h *graphTileHistory) trimRawLocked() {
	if h.hotLatest.IsZero() || len(h.raw) == 0 {
		return
	}
	cutoff := h.hotLatest.Add(-15 * time.Minute)
	keep := h.raw[:0]
	for _, sample := range h.raw {
		if sample.At.Before(cutoff) {
			continue
		}
		keep = append(keep, sample)
	}
	h.raw = keep
}

func (h *graphTileHistory) hotHistoryLocked(cutoff time.Time) graphTileHistorySet {
	samples := make([]graphTileRawSample, 0, len(h.raw))
	for _, sample := range h.raw {
		if sample.At.Before(cutoff) {
			continue
		}
		samples = append(samples, sample)
	}
	sort.SliceStable(samples, func(i, j int) bool {
		return samples[i].At.Before(samples[j].At)
	})
	out := graphTileHistorySet{TS: make([]string, 0, len(samples)), V: make([]float64, 0, len(samples))}
	for _, sample := range samples {
		out.TS = append(out.TS, sample.At.Format(time.RFC3339Nano))
		out.V = append(out.V, sample.Value)
	}
	return out
}

func (s *server) allGraphHistorySeries() []graphTileRequestSeries {
	s.graphHistoryMu.Lock()
	keys := make(map[string]struct{})
	for key := range s.graphHistoryDerived {
		keys[key] = struct{}{}
	}
	for key := range s.graphHistoryRaw {
		keys[key] = struct{}{}
	}
	s.graphHistoryMu.Unlock()
	out := make([]graphTileRequestSeries, 0, len(keys))
	for key := range keys {
		series, err := graphTileSeriesFromKey(key)
		if err != nil {
			continue
		}
		out = append(out, series)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := s.deviceSortRank(out[i].DeviceID)
		rj := s.deviceSortRank(out[j].DeviceID)
		if ri != rj {
			return ri < rj
		}
		if out[i].DeviceID != out[j].DeviceID {
			return out[i].DeviceID < out[j].DeviceID
		}
		if out[i].ParamID != out[j].ParamID {
			return out[i].ParamID < out[j].ParamID
		}
		return out[i].Instance < out[j].Instance
	})
	return out
}

func graphTileSeriesFromKey(key string) (graphTileRequestSeries, error) {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return graphTileRequestSeries{}, fmt.Errorf("invalid graph history key %q", key)
	}
	paramID, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-2]))
	if err != nil {
		return graphTileRequestSeries{}, err
	}
	instance, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return graphTileRequestSeries{}, err
	}
	deviceID := strings.TrimSpace(strings.Join(parts[:len(parts)-2], ":"))
	if deviceID == "" {
		return graphTileRequestSeries{}, fmt.Errorf("device required")
	}
	return graphTileRequestSeries{DeviceID: deviceID, ParamID: paramID, Instance: instance}, nil
}

func (s *server) graphHistoryEndpoint(deviceID string) string {
	b, ok := s.devices[deviceID]
	if !ok {
		return ""
	}
	return b.cfg.Endpoint
}

func (s *server) recordGraphSample(deviceID string, paramID, instance int, value float64, quality string, at time.Time) {
	_ = quality
	if !isFiniteFloat(value) {
		return
	}
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistoryDerived[key]
	if h == nil {
		h = newGraphTileHistory()
		s.graphHistoryDerived[key] = h
	}
	s.graphHistoryMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addSampleLocked(at, value, false)
}

func (s *server) recordGraphRawSample(deviceID string, paramID, instance int, value float64, at time.Time) {
	if !isFiniteFloat(value) {
		return
	}
	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistoryRaw[key]
	if h == nil {
		h = newGraphTileHistory()
		s.graphHistoryRaw[key] = h
	}
	s.graphHistoryMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.addSampleLocked(at, value, true)
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

func countDetachedTemperaturePoints(paramID int, unit string, points []graphTilePoint) int {
	count := 0
	for _, point := range points {
		if isDetachedMeasuredTemperature(paramID, unit, point.Value) {
			count++
		}
	}
	return count
}

func (s *server) handleGraphTileArrowRange(w http.ResponseWriter, r *http.Request, tileID string, t0, t1 time.Time, seriesReq []graphTileRequestSeries) {
	w.Header().Set("Content-Type", "application/vnd.apache.arrow.stream")
	w.WriteHeader(http.StatusOK)
	s.writeGraphTileArrowRange(w, tileID, t0, t1, seriesReq)
}

func (s *server) writeGraphTileArrowRange(w io.Writer, tileID string, t0, t1 time.Time, seriesReq []graphTileRequestSeries) {
	writer := ipc.NewWriter(w, ipc.WithSchema(arrowtelemetry.TelemetrySchema))
	defer writer.Close()

	builder := array.NewRecordBuilder(memory.DefaultAllocator, arrowtelemetry.TelemetrySchema)
	defer builder.Release()

	tsBuilder := builder.Field(0).(*array.Int64Builder)
	sensorBuilder := builder.Field(1).(*array.BinaryDictionaryBuilder)
	valueBuilder := builder.Field(2).(*array.Float64Builder)
	unitBuilder := builder.Field(3).(*array.BinaryDictionaryBuilder)
	campaignBuilder := builder.Field(4).(*array.BinaryDictionaryBuilder)
	sourceBuilder := builder.Field(5).(*array.BinaryDictionaryBuilder)
	roleBuilder := builder.Field(6).(*array.BinaryDictionaryBuilder)
	kindBuilder := builder.Field(7).(*array.BinaryDictionaryBuilder)
	familyBuilder := builder.Field(8).(*array.BinaryDictionaryBuilder)
	qualityBuilder := builder.Field(9).(*array.BinaryDictionaryBuilder)
	stateBuilder := builder.Field(10).(*array.BinaryDictionaryBuilder)

	campaignID := fmt.Sprintf("tile-%s-range-%d", tileID, time.Now().Unix())

	if len(seriesReq) == 0 {
		seriesReq = s.defaultGraphTileSeries(tileID)
	}

	for _, req := range seriesReq {
		h := s.lookupGraphRange(req.DeviceID, req.ParamID, req.Instance, t0, t1)
		param, _ := gatewayParameterByID(req.ParamID)
		sensorID := fmt.Sprintf("%s:%d:%d", req.DeviceID, req.ParamID, req.Instance)
		unit := firstNonEmpty(param.Unit, "_")

		for i := range h.TS {
			at, _ := parseGraphHistoryTimestamp(h.TS[i])
			tsBuilder.Append(at.UnixNano())
			_ = sensorBuilder.AppendString(sensorID)
			valueBuilder.Append(h.V[i])
			_ = unitBuilder.AppendString(unit)
			_ = campaignBuilder.AppendString(campaignID)
			_ = sourceBuilder.AppendString(s.graphHistoryEndpoint(req.DeviceID))
			_ = roleBuilder.AppendString(param.Role)
			_ = kindBuilder.AppendString(param.Kind)
			_ = familyBuilder.AppendString("mecom")
			_ = qualityBuilder.AppendString(gatewayQualityOK)
			stateBuilder.AppendNull()
		}

		if tsBuilder.Len() >= arrowtelemetry.BatchSize {
			record := builder.NewRecord()
			_ = writer.Write(record)
			record.Release()
		}
	}

	if tsBuilder.Len() > 0 {
		record := builder.NewRecord()
		_ = writer.Write(record)
		record.Release()
	}
}

func (s *server) buildGraphTileRange(tileID string, t0, t1 time.Time, req []graphTileRequestSeries) (graphTileResponse, error) {
	now := time.Now().UTC()
	series, levelNames := s.graphTileSeriesRange(tileID, req, t0, t1)
	
	pointCount := 0
	for _, item := range series {
		pointCount += len(item.History.TS)
	}

	return graphTileResponse{
		SchemaVersion: "signalforge.graph_tile.v1",
		ID:            tileID,
		CardID:        tileID,
		Level:         "range",
		T0:            t0.Format(time.RFC3339Nano),
		T1:            t1.Format(time.RFC3339Nano),
		GeneratedAt:   now.Format(time.RFC3339Nano),
		Renderer:      canonicalTileRenderer,
		Kind:          "timeseries",
		TileID:        tileID,
		Title:         s.graphTileTitle(tileID),
		TimeWindowMs:  int(t1.Sub(t0).Milliseconds()),
		Axes:          s.graphTileAxes(tileID, series),
		Series:        series,
		Diagnostics: graphTileDiag{
			Status:      "ok",
			SeriesCount: len(series),
			PointCount:  pointCount,
			Renderer:    canonicalTileRenderer,
			TileLevel:   "range",
			TileSource:  strings.Join(levelNames, ","),
		},
		Provenance: map[string]any{
			"source":       "meerstetter-go.graph-history",
			"generated_at": now.Format(time.RFC3339Nano),
		},
	}, nil
}

func (s *server) graphTileSeriesRange(tileID string, req []graphTileRequestSeries, t0, t1 time.Time) ([]graphTileItem, []string) {
	if len(req) == 0 {
		req = s.defaultGraphTileSeries(tileID)
	}
	out := make([]graphTileItem, 0, len(req))
	sources := make(map[string]struct{})
	for _, r := range req {
		h := s.lookupGraphRange(r.DeviceID, r.ParamID, r.Instance, t0, t1)
		param, _ := gatewayParameterByID(r.ParamID)
		
		item := graphTileItem{
			ID:        fmt.Sprintf("%s:%d:%d", r.DeviceID, r.ParamID, r.Instance),
			SeriesID:  fmt.Sprintf("%s:%d:%d", r.DeviceID, r.ParamID, r.Instance),
			TargetID:  r.DeviceID,
			Label:     param.Name,
			FullLabel: fmt.Sprintf("%s / %s", r.DeviceID, param.Name),
			Color:     gatewayParameterColor(r.ParamID),
			Unit:      param.Unit,
			History:   h,
			Role:      mecom.RoleForParam(param.ID),
			Kind:      mecom.KindForParam(param.ID),
			AxisID:    gatewayParameterAxisID(param.Unit),
			Source: graphTileSource{
				DeviceID: r.DeviceID,
				Instance: r.Instance,
				ParamID:  r.ParamID,
				SignalID: signalIDForParam(param),
				Endpoint: s.graphHistoryEndpoint(r.DeviceID),
			},
			DefaultVisible: true,
		}
		out = append(out, item)
		sources["history"] = struct{}{}
	}
	srcs := make([]string, 0, len(sources))
	for s := range sources {
		srcs = append(srcs, s)
	}
	return out, srcs
}

func (s *server) graphTileTitle(tileID string) string {
	return "Telemetry: " + tileID
}

func (s *server) graphTileAxes(tileID string, series []graphTileItem) []tileAxis {
	units := make(map[string]struct{})
	for _, item := range series {
		units[item.Unit] = struct{}{}
	}
	axes := make([]tileAxis, 0, len(units))
	for unit := range units {
		axes = append(axes, tileAxis{
			ID:    gatewayParameterAxisID(unit),
			Unit:  unit,
			Side:  "left",
		})
	}
	if len(axes) == 0 {
		axes = append(axes, tileAxis{ID: "default", Unit: "", Side: "left"})
	}
	return axes
}

func gatewayParameterColor(paramID int) string {
	colors := []string{"#3b82f6", "#ef4444", "#10b981", "#f59e0b", "#8b5cf6", "#ec4899"}
	return colors[paramID%len(colors)]
}

func gatewayParameterAxisID(unit string) string {
	if unit == "" || unit == "_" {
		return "default"
	}
	return "axis-" + unit
}


func seriesLatestPointIsDetached(paramID int, unit string, points []graphTilePoint) bool {
	if len(points) == 0 {
		return false
	}
	if unit != "degC" {
		return false
	}
	return points[len(points)-1].Value < -100
}
func (s *server) handleGraphSparklines(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	deviceID := q.Get("device")
	paramID, _ := strconv.Atoi(q.Get("param"))
	instance, _ := strconv.Atoi(firstNonEmpty(q.Get("instance"), "1"))

	if deviceID == "" || paramID == 0 {
		http.Error(w, "device and param required", http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("%s:%d:%d", deviceID, paramID, instance)
	s.graphHistoryMu.Lock()
	h := s.graphHistoryRaw[key]
	if h == nil {
		h = s.graphHistoryDerived[key]
	}
	s.graphHistoryMu.Unlock()

	if h == nil {
		writeJSON(w, http.StatusOK, map[string]any{"values": []float64{}})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Return last 30 samples for sparkline
	var values []float64
	if len(h.raw) > 0 {
		start := len(h.raw) - 30
		if start < 0 {
			start = 0
		}
		for _, s := range h.raw[start:] {
			values = append(values, s.Value)
		}
	} else {
		snap := h.pyramid.Snapshot(2 * time.Second)
		start := len(snap.Buckets) - 30
		if start < 0 {
			start = 0
		}
		for _, b := range snap.Buckets[start:] {
			values = append(values, b.Mean)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"values": values})
}
func (s *server) derivationWorker(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processDerivations()
		}
	}
}

func (s *server) processDerivations() {
	s.graphHistoryMu.Lock()
	// Get all keys from raw history
	keys := make([]string, 0, len(s.graphHistoryRaw))
	for k := range s.graphHistoryRaw {
		keys = append(keys, k)
	}
	s.graphHistoryMu.Unlock()

	for _, k := range keys {
		s.graphHistoryMu.Lock()
		rawH := s.graphHistoryRaw[k]
		derivedH := s.graphHistoryDerived[k]
		if derivedH == nil {
			derivedH = newGraphTileHistory()
			s.graphHistoryDerived[k] = derivedH
		}
		s.graphHistoryMu.Unlock()

		rawH.mu.Lock()
		if len(rawH.raw) == 0 {
			rawH.mu.Unlock()
			continue
		}
		
		// Take samples from the last 2 seconds
		cutoff := time.Now().Add(-2 * time.Second)
		var sum float64
		var count int
		var latestAt time.Time
		for _, sample := range rawH.raw {
			if sample.At.After(cutoff) {
				sum += sample.Value
				count++
				if sample.At.After(latestAt) {
					latestAt = sample.At
				}
			}
		}
		rawH.mu.Unlock()

		if count > 0 {
			mean := sum / float64(count)
			derivedH.mu.Lock()
			derivedH.addSampleLocked(latestAt, mean, false)
			derivedH.mu.Unlock()
		}
	}
}
