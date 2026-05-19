package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/signalforge/hdf5"
)

func (s *server) handleLogExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	manifest := DefaultArchiveManifest()
	
	// For now, we only support NDJSON format as implemented.
	format := r.URL.Query().Get("format")
	if format == "arrow" {
		s.handleArrowExport(w, r)
		return
	}
	if format == "hdf5" {
		s.handleHDF5Export(w, r)
		return
	}
	if format != "" && format != "ndjson" {
		http.Error(w, "format not supported yet: "+format, http.StatusNotImplemented)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)

	// 1. Export Manifest
	_ = enc.Encode(map[string]any{"type": "manifest", "data": manifest})

	// 2. Export Object Dictionary Snapshots
	s.exportObjectDictionarySnapshots(enc)

	// 3. Export Command Events
	s.exportCommandEvents(enc)

	// 4. Export Telemetry Samples
	s.exportTelemetrySamples(enc)
}

func (s *server) handleLogImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ct := r.Header.Get("Content-Type")
	if ct == "application/vnd.apache.arrow.stream" || ct == "application/octet-stream" || strings.HasSuffix(r.URL.Path, ".arrow") {
		s.handleArrowImport(w, r)
		return
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)) // 32MB limit
	importedSamples := 0
	
	for dec.More() {
		var row struct {
			Stream    string  `json:"stream"`
			DeviceID  string  `json:"device_id"`
			TargetID  string  `json:"target_id"`
			Parameter string  `json:"parameter"`
			ParamID   int     `json:"param_id"`
			Instance  any     `json:"instance"`
			Time      string  `json:"time"`
			Value     float64 `json:"value"`
			Quality   string  `json:"quality"`
		}
		if err := dec.Decode(&row); err != nil {
			http.Error(w, "invalid NDJSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if row.Stream == "telemetry_samples" {
			at, err := parseGraphHistoryTimestamp(row.Time)
			if err != nil {
				continue
			}
			
			deviceID := firstNonEmpty(row.DeviceID, row.TargetID)
			instance := intFromAny(row.Instance)
			if instance == 0 {
				instance = 1
			}

			// Try to resolve ParamID from name if missing
			paramID := row.ParamID
			if paramID == 0 && row.Parameter != "" {
				// Simple heuristic for common params
				switch strings.ToLower(row.Parameter) {
				case "object temperature": paramID = 1000
				case "sink temperature": paramID = 1001
				case "target object temperature": paramID = 3000
				}
			}

			if deviceID != "" && paramID != 0 {
				s.recordGraphSample(deviceID, paramID, instance, row.Value, row.Quality, at)
				importedSamples++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported_samples": importedSamples})
}

func (s *server) exportObjectDictionarySnapshots(enc *json.Encoder) {
	now := time.Now().UTC()
	channels := s.catalogueChannelCount()
	params := mecom.DefaultTECCatalogueEntries(channels)
	
	for _, p := range params {
		for _, b := range s.devices {
			snapshot := map[string]any{
				"stream":       "object_dictionary_snapshots",
				"generated_at": now.Format(time.RFC3339Nano),
				"target_id":    b.cfg.ID,
				"device_id":    b.cfg.ID,
				"type":         p.Type,
				"subtype":      p.Kind,
				"parameter":    p.RawName,
				"instance":     fmt.Sprintf("%d", p.Instance),
				"read_path":    fmt.Sprintf("%d:%d", p.ID, p.Instance),
				"write_path":   fmt.Sprintf("%d:%d", p.ID, p.Instance),
				"writable":     gatewayCatalogueWritable(p.ID),
			}
			_ = enc.Encode(snapshot)
		}
	}
}

func (s *server) exportCommandEvents(enc *json.Encoder) {
	s.commandLogMu.Lock()
	logs := make([]gatewayCommandActivity, len(s.commandLog))
	copy(logs, s.commandLog)
	s.commandLogMu.Unlock()

	for i, log := range logs {
		event := map[string]any{
			"stream":     "command_events",
			"seq":        uint64(i + 1),
			"time":       log.Time.Format(time.RFC3339Nano),
			"target_id":  log.TargetID,
			"write_path": fmt.Sprintf("%d:%d", log.ParamID, log.Instance),
			"status":     log.Status,
			"owner":      log.LeaseHolder,
			"message":    log.Error,
		}
		_ = enc.Encode(event)
	}
}

func (s *server) exportTelemetrySamples(enc *json.Encoder) {
	s.graphHistoryMu.Lock()
	keys := make([]string, 0, len(s.graphHistoryDerived))
	for k := range s.graphHistoryDerived {
		keys = append(keys, k)
	}
	s.graphHistoryMu.Unlock()

	seq := uint64(1)
	for _, key := range keys {
		series, err := graphTileSeriesFromKey(key)
		if err != nil {
			continue
		}
		
		h := s.lookupGraphHistory(series.DeviceID, series.ParamID, series.Instance, int(defaultGraphHistoryRetention.Milliseconds()), time.Now().UTC())
		def, _ := gatewayParameterByID(series.ParamID)

		for i := range h.TS {
			sample := map[string]any{
				"stream":       "telemetry_samples",
				"seq":          seq,
				"time":         h.TS[i],
				"target_id":    series.DeviceID,
				"device_id":    series.DeviceID,
				"instance":     fmt.Sprintf("%d", series.Instance),
				"parameter":    def.Name,
				"value":        h.V[i],
				"quality":      gatewayQualityOK, // TODO: improve quality tracking in history
				"source_path":  s.graphHistoryEndpoint(series.DeviceID),
			}
			_ = enc.Encode(sample)
			seq++
		}
	}
}
func (s *server) handleHDF5Export(w http.ResponseWriter, r *http.Request) {
	tmpFile := "/home/svc_pmg_testbed_b/meerstetter-go/scratch/export_" + time.Now().Format("20060102_150405") + ".h5"
	writer, err := hdf5.NewHDF5Writer(tmpFile)
	if err != nil {
		http.Error(w, "HDF5 init failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writer.Create(); err != nil {
		http.Error(w, "HDF5 create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer writer.Close()

	// Export Telemetry
	if err := s.exportTelemetryToHDF5(writer); err != nil {
		http.Error(w, "HDF5 telemetry export failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writer.Close()

	w.Header().Set("Content-Type", "application/x-hdf5")
	w.Header().Set("Content-Disposition", "attachment; filename=\"export.h5\"")
	http.ServeFile(w, r, tmpFile)
	os.Remove(tmpFile)
}

func (s *server) exportTelemetryToHDF5(w *hdf5.HDF5Writer) error {
	s.graphHistoryMu.Lock()
	keys := make([]string, 0, len(s.graphHistoryDerived))
	for k := range s.graphHistoryDerived {
		keys = append(keys, k)
	}
	s.graphHistoryMu.Unlock()

	gid, err := w.CreateGroup("telemetry")
	if err != nil {
		return err
	}
	defer w.CloseGroup(gid)

	for _, key := range keys {
		series, err := graphTileSeriesFromKey(key)
		if err != nil {
			continue
		}
		
		h := s.lookupGraphHistory(series.DeviceID, series.ParamID, series.Instance, int(defaultGraphHistoryRetention.Milliseconds()), time.Now().UTC())
		
		groupName := fmt.Sprintf("%s_%d_%d", series.DeviceID, series.ParamID, series.Instance)
		sgid, err := w.CreateGroup("telemetry/" + groupName)
		if err != nil {
			continue
		}
		
		// Write TS as int64 (UnixNano)
		ts := make([]int64, len(h.TS))
		for i, tStr := range h.TS {
			t, err := time.Parse(time.RFC3339Nano, tStr)
			if err == nil {
				ts[i] = t.UnixNano()
			}
		}
		w.WriteInt64Dataset(sgid, "timestamps", ts)
		w.WriteFloat64Dataset(sgid, "values", h.V)
		
		w.CloseGroup(sgid)
	}
	return nil
}

func DefaultArchiveManifest() map[string]any {
	return map[string]any{
		"streams": []map[string]any{
			{"name": "telemetry_samples", "purpose": "Decoded MeCom/CAN values after catalogue mapping, reduction, and freshness tagging."},
			{"name": "can_frames", "purpose": "Raw SocketCAN receive evidence from the PiXtend route, reconciled across RAM and flash rings."},
			{"name": "command_events", "purpose": "Sequencer and write-path receipts, including lease-gated accepts and rejects."},
		},
	}
}
