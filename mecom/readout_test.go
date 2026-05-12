package mecom

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestReadoutUsesTECCatalogueRingForPriorityAndBulkForBackground(t *testing.T) {
	readout := NewReadout(ReadoutConfig{
		Parameters: DefaultTECReadoutParameters(1),
		BulkChunk:  8,
	})
	client := &fakeReadoutClient{
		pointer:    0x20,
		bulkValues: []float64{1},
	}

	first := readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(10, 0))

	if len(client.configured) != 1 {
		t.Fatalf("configured calls = %d, want 1", len(client.configured))
	}
	if got, want := len(client.configured[0]), 8; got != want {
		t.Fatalf("ring capture slots = %d, want high-priority slots %d", got, want)
	}
	for _, p := range client.configured[0] {
		if p.Name == "temperature_stable" {
			t.Fatalf("background parameter was configured into ring capture: %#v", p)
		}
	}
	if len(client.bulkParams) != 1 || len(client.bulkParams[0]) != 1 || client.bulkParams[0][0].Name != "temperature_stable" {
		t.Fatalf("bulk params = %#v, want only temperature_stable background read", client.bulkParams)
	}
	if value := readoutValueBySensor(first.Values, "mecom.tec_01.temperature_stable"); value == nil || value.Value != 1 {
		t.Fatalf("first poll values missing background temperature_stable: %#v", first.Values)
	}

	client.bulkParams = nil
	client.bulkValues = []float64{0}
	client.ringResponse = RingReadResponse{
		BytesAdded: 11,
		Status:     RingStatusAllDataRead,
		Data:       []byte{0x88, 0x00, 0x34, 0x12, 0x00, 0x00, 0x00, 0x40, 0x41, 0x88, 0x10},
	}

	second := readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(11, 0))

	if client.ringReads != 1 {
		t.Fatalf("ring reads = %d, want 1", client.ringReads)
	}
	if client.lastRingStart != 0x20 {
		t.Fatalf("ring read start = %#x, want configured pointer %#x", client.lastRingStart, 0x20)
	}
	if value := readoutValueBySensor(second.Values, "mecom.tec_01.object_temp_c"); value == nil || math.Abs(value.Value-12) > 0.0001 {
		t.Fatalf("second poll values missing reduced ring object_temp_c=12: %#v", second.Values)
	}
}

func TestReadoutWidensRingReadWindowUnderBacklog(t *testing.T) {
	readout := NewReadout(ReadoutConfig{
		Parameters: DefaultTECReadoutParameters(1),
		BulkChunk:  8,
	})
	client := &fakeReadoutClient{
		pointer:    0x40,
		bulkValues: []float64{1},
	}

	_ = readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(10, 0))
	client.ringResponse = RingReadResponse{
		BytesAdded: DefaultRingReadMaxBytes,
		Status:     RingStatusHasMoreData,
		Data:       []byte{0x88, 0x00, 0x34, 0x12, 0x00, 0x00, 0x00, 0x40, 0x41, 0x88, 0x10},
	}

	_ = readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(11, 0))

	if readout.RingReadMaxBytes() != DefaultRingReadMaxBytes*2 {
		t.Fatalf("ring max bytes = %d, want widened %d", readout.RingReadMaxBytes(), DefaultRingReadMaxBytes*2)
	}
}

func TestReadoutAddsChannelModeAwareDerivedValues(t *testing.T) {
	params := []ReadoutParameter{
		{Parameter: Parameter{ID: 1000, Instance: 1, Name: "object_temp_c", Unit: "degC", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.object_temp_c"},
		{Parameter: Parameter{ID: 1001, Instance: 1, Name: "sink_temp_c", Unit: "degC", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.sink_temp_c"},
		{Parameter: Parameter{ID: 1020, Instance: 1, Name: "output_current_a", Unit: "A", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.output_current_a"},
		{Parameter: Parameter{ID: 1021, Instance: 1, Name: "output_voltage_v", Unit: "V", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.output_voltage_v"},
	}
	readout := NewReadout(ReadoutConfig{
		Parameters: params,
		BulkChunk:  len(params),
		Derived: &DerivedReadoutConfig{
			ControllerAddress: 0x30,
			ChannelModes:      map[int]ChannelDriveMode{1: ChannelModePeltierDriver},
			Estimator: NewPeltierEstimator(map[int]PeltierModuleData{
				0x30: {
					ModuleID: "unit-module",
					Source:   "unit_module_curve",
					Points: []PeltierModulePoint{
						{DeltaT: 20, CurrentAmpere: 2, VoltageVolt: 12, HeatPumpedWatt: 8},
					},
				},
			}),
		},
	})
	client := &fakeReadoutClient{
		bulkValues: []float64{10, 30, 2, 12},
	}

	batch := readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(20, 0))

	if got, want := len(batch.DerivedEstimates), 1; got != want {
		t.Fatalf("derived estimate count = %d, want %d", got, want)
	}
	if value := readoutValueBySensor(batch.Values, "mecom.tec_01.electrical_input_w"); value == nil || math.Abs(value.Value-24) > 0.0001 {
		t.Fatalf("missing derived electrical input 24 W: %#v", batch.Values)
	}
	if value := readoutValueBySensor(batch.Values, "mecom.tec_01.heat_pumped_from_item_w"); value == nil || math.Abs(value.Value-8) > 0.0001 {
		t.Fatalf("missing derived heat pumped 8 W: %#v", batch.Values)
	}
	if value := readoutValueBySensor(batch.Values, "mecom.tec_01.hot_side_dissipated_w"); value == nil || math.Abs(value.Value-32) > 0.0001 {
		t.Fatalf("missing derived hot-side dissipated 32 W: %#v", batch.Values)
	}
}

func TestReadoutDoesNotInferPeltierThermalValuesForPowerSupplyMode(t *testing.T) {
	params := []ReadoutParameter{
		{Parameter: Parameter{ID: 1020, Instance: 1, Name: "output_current_a", Unit: "A", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.output_current_a"},
		{Parameter: Parameter{ID: 1021, Instance: 1, Name: "output_voltage_v", Unit: "V", Type: DataTypeFloat32}, Sensor: "mecom.tec_01.output_voltage_v"},
	}
	readout := NewReadout(ReadoutConfig{
		Parameters: params,
		BulkChunk:  len(params),
		Derived: &DerivedReadoutConfig{
			ControllerAddress: 0x30,
			ChannelModes:      map[int]ChannelDriveMode{1: ChannelModePowerSupply},
		},
	})
	client := &fakeReadoutClient{
		bulkValues: []float64{1.5, 10},
	}

	batch := readout.Poll(contextWithReadoutTestTimeout(t), client, time.Unix(21, 0))

	if value := readoutValueBySensor(batch.Values, "mecom.tec_01.electrical_input_w"); value == nil || math.Abs(value.Value-15) > 0.0001 {
		t.Fatalf("missing derived electrical input 15 W: %#v", batch.Values)
	}
	if value := readoutValueBySensor(batch.Values, "mecom.tec_01.heat_pumped_from_item_w"); value != nil {
		t.Fatalf("power supply mode must not infer peltier thermal output: %#v", value)
	}
}

type fakeReadoutClient struct {
	configured    [][]RingCaptureParameter
	pointer       uint32
	ringResponse  RingReadResponse
	ringReads     int
	lastRingStart uint32
	bulkParams    [][]Parameter
	bulkValues    []float64
}

func (f *fakeReadoutClient) ReadBulk(_ context.Context, params []Parameter) ([]float64, error) {
	copied := append([]Parameter(nil), params...)
	f.bulkParams = append(f.bulkParams, copied)
	values := make([]float64, len(params))
	for i := range values {
		values[i] = math.NaN()
	}
	for i, value := range f.bulkValues {
		if i >= len(values) {
			break
		}
		values[i] = value
	}
	return values, nil
}

func (f *fakeReadoutClient) ConfigureRingCapture(_ context.Context, _ uint16, params []RingCaptureParameter) error {
	copied := append([]RingCaptureParameter(nil), params...)
	f.configured = append(f.configured, copied)
	return nil
}

func (f *fakeReadoutClient) TriggerRingSync(context.Context) error { return nil }

func (f *fakeReadoutClient) ReadRingPointer(context.Context) (uint32, error) {
	return f.pointer, nil
}

func (f *fakeReadoutClient) ReadRingBuffer(_ context.Context, start uint32, _ uint16) (RingReadResponse, error) {
	f.ringReads++
	f.lastRingStart = start
	return f.ringResponse, nil
}

func contextWithReadoutTestTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func readoutValueBySensor(values []ReadoutValue, sensor string) *ReadoutValue {
	for i := range values {
		if values[i].Sensor == sensor {
			return &values[i]
		}
	}
	return nil
}
