package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/signalforge/arrowtelemetry"
)

func (s *server) handleArrowExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.graphHistoryMu.Lock()
	keys := make([]string, 0, len(s.graphHistoryDerived))
	for k := range s.graphHistoryDerived {
		keys = append(keys, k)
	}
	s.graphHistoryMu.Unlock()

	w.Header().Set("Content-Type", arrowtelemetry.TransportMIME)
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

	campaignID := "meerstetter-export-" + time.Now().UTC().Format("20060102T150405Z")
	now := time.Now().UTC()

	for _, key := range keys {
		series, err := graphTileSeriesFromKey(key)
		if err != nil {
			continue
		}

		h := s.lookupGraphHistory(series.DeviceID, series.ParamID, series.Instance, int(defaultGraphHistoryRetention.Milliseconds()), now)
		param, _ := gatewayParameterByID(series.ParamID)
		
		sensorID := fmt.Sprintf("%s:%d:%d", series.DeviceID, series.ParamID, series.Instance)
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
			_ = sourceBuilder.AppendString(s.graphHistoryEndpoint(series.DeviceID))
			_ = roleBuilder.AppendString(mecom.RoleForParam(param.ID))
			_ = kindBuilder.AppendString(mecom.KindForParam(param.ID))
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

func (s *server) handleArrowImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reader, err := ipc.NewReader(r.Body, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		http.Error(w, "invalid Arrow IPC stream: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer reader.Release()

	imported := 0
	for reader.Next() {
		record := reader.Record()
		
		tsCol := record.Column(0).(*array.Int64)
		sensorCol := record.Column(1).(*array.Dictionary)
		valueCol := record.Column(2).(*array.Float64)
		qualityCol := record.Column(9).(*array.Dictionary)

		for i := 0; i < int(record.NumRows()); i++ {
			at := time.Unix(0, tsCol.Value(i)).UTC()
			sensorID := sensorCol.Dictionary().(*array.String).Value(sensorCol.GetValueIndex(i))
			val := valueCol.Value(i)
			quality := qualityCol.Dictionary().(*array.String).Value(qualityCol.GetValueIndex(i))

			// Parse sensorID back to device:param:instance
			parts := strings.Split(sensorID, ":")
			if len(parts) < 3 {
				continue
			}
			paramID, _ := strconv.Atoi(parts[len(parts)-2])
			instance, _ := strconv.Atoi(parts[len(parts)-1])
			deviceID := strings.Join(parts[:len(parts)-2], ":")

			if deviceID != "" && paramID != 0 {
				s.recordGraphSample(deviceID, paramID, instance, val, quality, at)
				imported++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported_samples": imported})
}
