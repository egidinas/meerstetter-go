//go:build linux

package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/egidinas/signalforge/hdf5"
)

func (s *server) handleHDF5Export(w http.ResponseWriter, r *http.Request) {
	tmpFile, err := newHDF5ExportTempFile()
	if err != nil {
		http.Error(w, "HDF5 temp file failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile)

	writer, err := hdf5.NewHDF5Writer(tmpFile)
	if err != nil {
		http.Error(w, "HDF5 init failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := writer.Create(); err != nil {
		_ = writer.Close()
		http.Error(w, "HDF5 create failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.exportTelemetryToHDF5(writer); err != nil {
		_ = writer.Close()
		http.Error(w, "HDF5 telemetry export failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writer.Close(); err != nil {
		http.Error(w, "HDF5 close failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-hdf5")
	w.Header().Set("Content-Disposition", "attachment; filename=\"export.h5\"")
	http.ServeFile(w, r, tmpFile)
}

func newHDF5ExportTempFile() (string, error) {
	file, err := os.CreateTemp("", "mecomgw-export-*.h5")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
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
