package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/egidinas/meerstetter-go/mecom"
	"github.com/egidinas/signalforge/arrowtelemetry"
)

var (
	arrowImportByteLimit   int64 = maxGraphHistoryImportBytes
	arrowImportSampleLimit       = maxGraphHistoryImportSamples

	errArrowSchemaMismatch = errors.New("Arrow schema does not match signalforge telemetry schema")
	errArrowTooManySamples = errors.New("Arrow import sample limit exceeded")
)

type arrowImportSample struct {
	deviceID string
	paramID  int
	instance int
	value    float64
	quality  string
	at       time.Time
}

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

	samples, err := readArrowImportSamples(http.MaxBytesReader(w, r.Body, arrowImportByteLimit), arrowImportSampleLimit)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errArrowTooManySamples) || isRequestBodyTooLarge(err) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, "invalid Arrow import: "+err.Error(), status)
		return
	}

	for _, sample := range samples {
		s.recordGraphSample(sample.deviceID, sample.paramID, sample.instance, sample.value, sample.quality, sample.at)
	}

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported_samples": len(samples)})
}

func readArrowImportSamples(body io.Reader, maxSamples int) ([]arrowImportSample, error) {
	reader, err := ipc.NewReader(body, ipc.WithAllocator(memory.DefaultAllocator))
	if err != nil {
		return nil, fmt.Errorf("Arrow IPC stream: %w", err)
	}
	defer reader.Release()

	if !reader.Schema().Equal(arrowtelemetry.TelemetrySchema) {
		return nil, errArrowSchemaMismatch
	}

	samples := make([]arrowImportSample, 0)
	for reader.Next() {
		record := reader.Record()
		if maxSamples > 0 && len(samples)+int(record.NumRows()) > maxSamples {
			return nil, errArrowTooManySamples
		}

		tsCol, sensorCol, valueCol, qualityCol, err := arrowImportColumns(record)
		if err != nil {
			return nil, err
		}

		for i := 0; i < int(record.NumRows()); i++ {
			if tsCol.IsNull(i) || sensorCol.IsNull(i) || valueCol.IsNull(i) || qualityCol.IsNull(i) {
				continue
			}
			at := time.Unix(0, tsCol.Value(i)).UTC()
			sensorID, err := arrowDictionaryString(sensorCol, i)
			if err != nil {
				return nil, err
			}
			val := valueCol.Value(i)
			quality, err := arrowDictionaryString(qualityCol, i)
			if err != nil {
				return nil, err
			}

			// Parse sensorID back to device:param:instance
			parts := strings.Split(sensorID, ":")
			if len(parts) < 3 {
				continue
			}
			paramID, err := strconv.Atoi(parts[len(parts)-2])
			if err != nil {
				continue
			}
			instance, err := strconv.Atoi(parts[len(parts)-1])
			if err != nil {
				continue
			}
			deviceID := strings.Join(parts[:len(parts)-2], ":")

			if deviceID != "" && paramID != 0 {
				samples = append(samples, arrowImportSample{
					deviceID: deviceID,
					paramID:  paramID,
					instance: instance,
					value:    val,
					quality:  quality,
					at:       at,
				})
			}
		}
	}
	if err := reader.Err(); err != nil {
		return nil, fmt.Errorf("Arrow IPC stream: %w", err)
	}

	return samples, nil
}

func arrowImportColumns(record arrow.Record) (*array.Int64, *array.Dictionary, *array.Float64, *array.Dictionary, error) {
	tsCol, ok := record.Column(0).(*array.Int64)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("timestamp_ns column has unexpected type %T", record.Column(0))
	}
	sensorCol, ok := record.Column(1).(*array.Dictionary)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("sensor column has unexpected type %T", record.Column(1))
	}
	valueCol, ok := record.Column(2).(*array.Float64)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("value column has unexpected type %T", record.Column(2))
	}
	qualityCol, ok := record.Column(9).(*array.Dictionary)
	if !ok {
		return nil, nil, nil, nil, fmt.Errorf("quality column has unexpected type %T", record.Column(9))
	}
	return tsCol, sensorCol, valueCol, qualityCol, nil
}

func arrowDictionaryString(col *array.Dictionary, row int) (string, error) {
	dict, ok := col.Dictionary().(*array.String)
	if !ok {
		return "", fmt.Errorf("dictionary column has unexpected value type %T", col.Dictionary())
	}
	return dict.Value(col.GetValueIndex(row)), nil
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr) || strings.Contains(err.Error(), "http: request body too large")
}
