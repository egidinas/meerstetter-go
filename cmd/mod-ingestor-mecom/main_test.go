package main

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/loom-gossamer-shared/go/capabilitycatalog"
	"github.com/egidinas/loom-gossamer-shared/go/graphsem"
	"github.com/egidinas/loom-gossamer-shared/go/schema"
	"github.com/egidinas/meerstetter-go/mecom"
)

func TestMeComSourceOfferDeclaresChannelCatalogueAndDisabledWrites(t *testing.T) {
	subject := "telemetry.v4.local.lab.bench1.mecom.live"
	offer := mecomSourceOffer(emptyEnv, 2, subject, 500, 8)

	if offer.SourceFamily != string(graphsem.SourceFamilyMeComTec) {
		t.Fatalf("SourceFamily = %q, want mecom_tec", offer.SourceFamily)
	}
	if len(offer.Subjects) != 1 || offer.Subjects[0] != subject {
		t.Fatalf("Subjects = %#v, want only %q", offer.Subjects, subject)
	}
	if offer.Metadata["channel_count"] != "2" {
		t.Fatalf("channel_count = %q", offer.Metadata["channel_count"])
	}
	if offer.Metadata["command_policy"] != "read_only_discovery_writes_disabled_until_saga_lease" {
		t.Fatalf("command_policy = %q", offer.Metadata["command_policy"])
	}
	if offer.Metadata["semantic_mapping"] != "graphsem_mecom_tec_catalogue" {
		t.Fatalf("semantic_mapping = %q", offer.Metadata["semantic_mapping"])
	}
	if offer.Metadata["catalogue_entries"] != "26" {
		t.Fatalf("catalogue_entries = %q", offer.Metadata["catalogue_entries"])
	}
	if offer.Metadata["background_readout"] != "?VX_round_robin_queue" ||
		offer.Metadata["manual_poll_policy"] != "front_of_round_robin_queue_return_latest_when_polled" ||
		offer.Metadata["ring_reduction"] != "mean_stddev_window_to_consumer_rate" ||
		offer.Metadata["single_read_policy"] != "compatibility_only" ||
		offer.Metadata["derived_readout"] != "mecom_derived_channel_model" ||
		offer.Metadata["channel_mode_policy"] != "explicit_channel_modes_no_thermal_inference_for_power_supply" {
		t.Fatalf("readout metadata = %#v", offer.Metadata)
	}
	if !strings.Contains(offer.Metadata["semantic_roles"], "cascade_temperature") ||
		!strings.Contains(offer.Metadata["semantic_roles"], "vawc_voltage") ||
		!strings.Contains(offer.Metadata["semantic_roles"], "vawc_power") ||
		!strings.Contains(offer.Metadata["semantic_roles"], "heat_pumped_from_item") ||
		!strings.Contains(offer.Metadata["semantic_roles"], "hot_side_dissipated_heat") {
		t.Fatalf("semantic_roles missing required measured/derived roles: %q", offer.Metadata["semantic_roles"])
	}
	if offer.Metadata["priority_groups"] != "vawc,cascade,key_temperatures" {
		t.Fatalf("priority_groups = %q", offer.Metadata["priority_groups"])
	}

	catalogueOffer := capabilitycatalog.OfferFromGraphSource(offer, "unit_test", time.Unix(100, 0))
	if err := catalogueOffer.Validate(); err != nil {
		t.Fatalf("catalogue offer did not validate: %v", err)
	}
	if catalogueOffer.SemanticModel.ModelClass != "mecom_tec_controller_bank" {
		t.Fatalf("ModelClass = %q", catalogueOffer.SemanticModel.ModelClass)
	}
	if !containsString(catalogueOffer.SemanticModel.Properties, "mecom.tec_02.output_power_w") {
		t.Fatalf("properties missing channel signal: %#v", catalogueOffer.SemanticModel.Properties)
	}
	if !containsString(catalogueOffer.SemanticModel.Properties, "mecom.tec_02.cascade_temp_c") {
		t.Fatalf("properties missing cascade signal: %#v", catalogueOffer.SemanticModel.Properties)
	}
	if !containsString(catalogueOffer.SemanticModel.Properties, "mecom.tec_02.vawc_voltage_v") {
		t.Fatalf("properties missing VAWC voltage signal: %#v", catalogueOffer.SemanticModel.Properties)
	}
	if !containsString(catalogueOffer.SemanticModel.Properties, "mecom.tec_02.hot_side_dissipated_w") {
		t.Fatalf("properties missing derived hot-side dissipated heat signal: %#v", catalogueOffer.SemanticModel.Properties)
	}
	if !containsString(catalogueOffer.SemanticModel.Actions, "request_setpoint") {
		t.Fatalf("actions missing planned setpoint request: %#v", catalogueOffer.SemanticModel.Actions)
	}
	if !containsString(catalogueOffer.SemanticModel.Actions, "request_enable") {
		t.Fatalf("actions missing planned enable request: %#v", catalogueOffer.SemanticModel.Actions)
	}
	if !interactionSliceContains(catalogueOffer.Affordances.Actions, "request_setpoint") {
		t.Fatalf("affordance actions missing planned setpoint request: %#v", catalogueOffer.Affordances.Actions)
	}
	action := interactionByName(catalogueOffer.Affordances.Actions, "request_setpoint")
	if action == nil {
		t.Fatalf("missing request_setpoint action affordance: %#v", catalogueOffer.Affordances.Actions)
	}
	if action.State != "planned_disabled" || !action.ReadOnly || !action.LeaseRequired || !action.ReceiptRequired {
		t.Fatalf("request_setpoint action contract = %#v, want planned disabled lease/receipt affordance", *action)
	}
	if action.DisabledReason != "disabled_until_edge_owned_lease_and_originator_receipts" {
		t.Fatalf("request_setpoint disabled_reason = %q", action.DisabledReason)
	}
	if !bindingProtocolContains(catalogueOffer.Bindings, "originator_lease", "action") {
		t.Fatalf("bindings missing planned originator lease action: %#v", catalogueOffer.Bindings)
	}
	if !readOnlyBindingWithOperation(catalogueOffer.Bindings, "disabled_planned_control") {
		t.Fatalf("bindings missing read-only disabled planned action operation: %#v", catalogueOffer.Bindings)
	}
	if !catalogueOffer.ReadOnly || catalogueOffer.ControlPolicy.Mode != "read_only" || len(catalogueOffer.CommandSubjects) != 0 {
		t.Fatalf("control boundary = read_only:%v mode:%q commands:%#v", catalogueOffer.ReadOnly, catalogueOffer.ControlPolicy.Mode, catalogueOffer.CommandSubjects)
	}
	if catalogueOffer.ControlPolicy.Lease == "" || catalogueOffer.ControlPolicy.Audit == "" {
		t.Fatalf("control policy missing lease/audit requirements: %#v", catalogueOffer.ControlPolicy)
	}
}

func emptyEnv(string) string { return "" }

func TestChannelModesFromEnvAppliesDefaultAndOverrides(t *testing.T) {
	env := map[string]string{
		"LOOM_MECOM_DEFAULT_CHANNEL_MODE": "resistor",
		"LOOM_MECOM_CHANNEL_MODES":        "2=peltier_driver, 4:power_supply, 99=peltier",
	}

	modes := channelModesFromEnv(func(key string) string { return env[key] }, 4)

	if got := modes[1]; got != mecom.ChannelModeResistor {
		t.Fatalf("channel 1 mode = %q, want resistor", got)
	}
	if got := modes[2]; got != mecom.ChannelModePeltierDriver {
		t.Fatalf("channel 2 mode = %q, want peltier driver", got)
	}
	if got := modes[3]; got != mecom.ChannelModeResistor {
		t.Fatalf("channel 3 mode = %q, want resistor", got)
	}
	if got := modes[4]; got != mecom.ChannelModePowerSupply {
		t.Fatalf("channel 4 mode = %q, want power supply", got)
	}
	if _, ok := modes[99]; ok {
		t.Fatalf("out-of-range channel mode was retained: %#v", modes)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func interactionSliceContains(values []capabilitycatalog.InteractionAffordance, want string) bool {
	for _, value := range values {
		if value.Name == want || value.Title == want {
			return true
		}
	}
	return false
}

func interactionByName(values []capabilitycatalog.InteractionAffordance, want string) *capabilitycatalog.InteractionAffordance {
	for i := range values {
		if values[i].Name == want || values[i].Title == want {
			return &values[i]
		}
	}
	return nil
}

func bindingProtocolContains(values []capabilitycatalog.ProtocolBinding, protocol, kind string) bool {
	for _, value := range values {
		if value.Protocol == protocol && value.Kind == kind {
			return true
		}
	}
	return false
}

func readOnlyBindingWithOperation(values []capabilitycatalog.ProtocolBinding, operation string) bool {
	for _, value := range values {
		if value.Operation == operation && value.ReadOnly {
			return true
		}
	}
	return false
}

func TestDeviceReadoutUsesRingForHighPriorityAndBulkForBackground(t *testing.T) {
	ep, ok := mecom.ParseTarget("127.0.0.1:50000")
	if !ok {
		t.Fatal("failed to parse test endpoint")
	}
	readout := newDeviceReadout(target{addr: 0x30, endpoint: ep}, 1, "dut-1", "test-1", 8)
	client := &fakeMeComReadClient{
		pointer: 0x20,
		bulkValues: []float64{
			1,
		},
	}

	firstRows := readout.pollClient(contextWithTestTimeout(t), client)

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
	if row := rowBySensor(firstRows, "mecom.tec_01.temperature_stable"); row == nil || row.Value != 1 {
		t.Fatalf("first poll rows missing background temperature_stable: %#v", firstRows)
	}

	client.bulkParams = nil
	client.bulkValues = []float64{0}
	client.ringResponse = mecom.RingReadResponse{
		BytesAdded: 11,
		Status:     mecom.RingStatusAllDataRead,
		Data:       []byte{0x88, 0x00, 0x34, 0x12, 0x00, 0x00, 0x00, 0x40, 0x41, 0x88, 0x10},
	}

	secondRows := readout.pollClient(contextWithTestTimeout(t), client)

	if client.ringReads != 1 {
		t.Fatalf("ring reads = %d, want 1", client.ringReads)
	}
	if client.lastRingStart != 0x20 {
		t.Fatalf("ring read start = %#x, want configured pointer %#x", client.lastRingStart, 0x20)
	}
	if row := rowBySensor(secondRows, "mecom.tec_01.object_temp_c"); row == nil || math.Abs(row.Value-12) > 0.0001 {
		t.Fatalf("second poll rows missing reduced ring object_temp_c=12: %#v", secondRows)
	}
}

type fakeMeComReadClient struct {
	configured    [][]mecom.RingCaptureParameter
	pointer       uint32
	ringResponse  mecom.RingReadResponse
	ringReads     int
	lastRingStart uint32
	bulkParams    [][]mecom.Parameter
	bulkValues    []float64
}

func (f *fakeMeComReadClient) ReadBulk(_ context.Context, params []mecom.Parameter) ([]float64, error) {
	copied := append([]mecom.Parameter(nil), params...)
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

func (f *fakeMeComReadClient) ConfigureRingCapture(_ context.Context, _ uint16, params []mecom.RingCaptureParameter) error {
	copied := append([]mecom.RingCaptureParameter(nil), params...)
	f.configured = append(f.configured, copied)
	return nil
}

func (f *fakeMeComReadClient) TriggerRingSync(context.Context) error { return nil }

func (f *fakeMeComReadClient) ReadRingPointer(context.Context) (uint32, error) {
	return f.pointer, nil
}

func (f *fakeMeComReadClient) ReadRingBuffer(_ context.Context, start uint32, _ uint16) (mecom.RingReadResponse, error) {
	f.ringReads++
	f.lastRingStart = start
	return f.ringResponse, nil
}

func contextWithTestTimeout(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	return ctx
}

func rowBySensor(rows []schema.TelemetryRow, sensor string) *schema.TelemetryRow {
	for i := range rows {
		if rows[i].Sensor == sensor {
			return &rows[i]
		}
	}
	return nil
}
