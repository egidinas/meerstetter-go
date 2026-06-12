package mecom

import (
	"strings"
	"testing"

	"github.com/egidinas/signalforge/graphsem"
)

func TestBuildMeComTECCatalogueUsesChannelCountWithoutEightSpotAssumption(t *testing.T) {
	catalogue := BuildMeComTECCatalogue(MeComTECCatalogueConfig{
		SourceID:      "bench_mecom",
		ChannelCount:  3,
		SourceSubject: "telemetry.v4.local.lab.bench1.mecom.live",
	})

	if err := catalogue.Validate(); err != nil {
		t.Fatalf("catalogue did not validate: %v", err)
	}
	if catalogue.SourceID != "bench_mecom" || catalogue.SourceFamily != DefaultTECCatalogueDefinition().SourceFamily {
		t.Fatalf("catalogue identity = %q/%q", catalogue.SourceID, catalogue.SourceFamily)
	}
	if got, want := len(catalogue.Entries), 51; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
	for _, entry := range catalogue.Entries {
		if entry.TraceID == "mecom.tec_08.object_temp_c" {
			t.Fatalf("unexpected 8-spot hardcoded trace in 3-channel catalogue")
		}
		if entry.SourceSubject != "telemetry.v4.local.lab.bench1.mecom.live" {
			t.Fatalf("source subject drifted to %q", entry.SourceSubject)
		}
	}
}

func TestBuildMeComCatalogueCarriesFamilyAwareDefinitionMetadata(t *testing.T) {
	catalogue := BuildMeComCatalogue(MeComCatalogueConfig{
		Definition:   DefaultTECCatalogueDefinition(),
		ChannelCount: 1,
	})

	if err := catalogue.Validate(); err != nil {
		t.Fatalf("catalogue did not validate: %v", err)
	}
	row := findCatalogueEntry(t, catalogue, "mecom.tec_01.object_temp_c")
	if row.Metadata["definition_ref"] != "meerstetter.tec.v632" ||
		row.Metadata["definition_system"] != MeComDefinitionSystem ||
		row.Metadata["definition_family"] != MeerstetterDefinitionFamily ||
		row.Metadata["definition_sub_family"] != MeerstetterSubFamilyTEC ||
		row.Metadata["definition_version"] != "v632" {
		t.Fatalf("definition metadata = %#v", row.Metadata)
	}

	entries := DefaultTECCatalogueEntries(1)
	entry := findTECCatalogueProtocolEntry(t, entries, 1000, 1)
	if entry.Metadata["definition_ref"] != "meerstetter.tec.v632" {
		t.Fatalf("runtime catalogue definition metadata = %#v", entry.Metadata)
	}
}

func TestDefaultLDD130xCatalogueEntriesExposeProtocolCheckedMetadata(t *testing.T) {
	entries := DefaultLDD130xCatalogueEntries(4)
	if got, want := len(entries), 275; got < want {
		t.Fatalf("LDD catalogue entries = %d, want at least %d", got, want)
	}
	for _, entry := range entries {
		if entry.Instance != 1 {
			t.Fatalf("LDD entry %s instance = %d, want one device-level instance", entry.RawName, entry.Instance)
		}
		if entry.Metadata["definition_ref"] != "meerstetter.ldd_130x.v221" ||
			entry.Metadata["definition_family"] != MeerstetterDefinitionFamily ||
			entry.Metadata["definition_sub_family"] != MeerstetterSubFamilyLDD ||
			entry.Metadata["definition_variant"] != MeerstetterVariantLDD130x {
			t.Fatalf("LDD entry %s definition metadata = %#v", entry.RawName, entry.Metadata)
		}
	}

	outputEnable := findTECCatalogueRawName(t, entries, "LDD_OUTPUT_EN")
	if outputEnable.ID != 2100 ||
		outputEnable.Access != "metadata" ||
		outputEnable.Role != "control" ||
		outputEnable.PreferredReadout != "definition_catalogue" ||
		outputEnable.SourceStatus != "protocol_documented" {
		t.Fatalf("LDD output enable row = %#v", outputEnable)
	}
	if outputEnable.Metadata["protocol_ids"] != "2100" ||
		outputEnable.Metadata["canopen_indices"] != "0x2230" {
		t.Fatalf("LDD output enable protocol metadata = %#v", outputEnable.Metadata)
	}
	if !strings.Contains(outputEnable.Help, "Output Enable") || !containsString(outputEnable.ApplicableModes, "ldd") {
		t.Fatalf("LDD output enable help/modes = %q / %#v", outputEnable.Help, outputEnable.ApplicableModes)
	}

	minVoltage := findTECCatalogueRawName(t, entries, "LDD_VOLTAGE_LIMIT_MIN")
	if minVoltage.Unit != "V" ||
		minVoltage.Metadata["protocol_ids"] != "2125" ||
		minVoltage.Metadata["canopen_indices"] != "0x2255" ||
		minVoltage.Metadata["documentation_cross_check"] != "min_nominal_voltage_protocol_mapping" {
		t.Fatalf("LDD minimum-voltage metadata = unit:%q metadata:%#v", minVoltage.Unit, minVoltage.Metadata)
	}

	featureLimitBypass := findTECCatalogueRawName(t, entries, "IGNORE_FEATURE_FIRMW_LIM")
	if featureLimitBypass.SourceStatus != "service_software_only" ||
		featureLimitBypass.Role != "metadata" ||
		!containsString(featureLimitBypass.ApplicableModes, "metadata") ||
		!strings.Contains(featureLimitBypass.SafetyNote, "metadata candidates") {
		t.Fatalf("LDD service-only bypass row = %#v", featureLimitBypass)
	}

	featureKeyStore := findTECCatalogueRawName(t, entries, "FEATURE_KEY_STORE")
	if featureKeyStore.ID != 54000 ||
		featureKeyStore.SourceStatus != "protocol_documented" ||
		featureKeyStore.Metadata["documentation_cross_check"] != "feature_unlock_license_metadata" {
		t.Fatalf("LDD feature key store row = %#v metadata=%#v", featureKeyStore, featureKeyStore.Metadata)
	}
}

func TestBuildMeComCatalogueUsesDefinitionTracePrefix(t *testing.T) {
	definition := DefaultTECCatalogueDefinition()
	definition.TracePrefix = "fixture.meerstetter.tec"
	definition.SourceFamily = graphsem.SourceFamily("fixture_meerstetter")
	definition.DefaultSourceID = "fixture_meerstetter"
	definition.DefinitionID = "fixture.meerstetter.tec.v1"

	catalogue := BuildMeComCatalogue(MeComCatalogueConfig{
		Definition:   definition,
		ChannelCount: 1,
	})

	if catalogue.SourceFamily != graphsem.SourceFamily("fixture_meerstetter") {
		t.Fatalf("source family = %q", catalogue.SourceFamily)
	}
	row := findCatalogueEntry(t, catalogue, "fixture.meerstetter.tec_01.object_temp_c")
	if strings.HasPrefix(row.TraceID, "mecom.tec_") {
		t.Fatalf("trace id still uses hardcoded TEC prefix: %q", row.TraceID)
	}
	if row.Metadata["definition_ref"] != "fixture.meerstetter.tec.v1" {
		t.Fatalf("definition metadata = %#v", row.Metadata)
	}

	readouts := DefaultMeComReadoutParameters(definition, 1)
	if len(readouts) == 0 {
		t.Fatalf("readout sensor prefix missing")
	}
	if readouts[0].Sensor != "fixture.meerstetter.tec_01.object_temp_c" {
		t.Fatalf("readout sensor prefix = %q", readouts[0].Sensor)
	}
}

func TestResolveMeComCatalogueDefinitionRecognizesOpenSubFamilies(t *testing.T) {
	cases := []struct {
		subFamily        string
		wantSub          string
		wantVar          string
		wantSourceFamily graphsem.SourceFamily
	}{
		{subFamily: "", wantSub: MeerstetterSubFamilyTEC, wantVar: MeerstetterSubFamilyTEC, wantSourceFamily: graphsem.SourceFamily(DefaultMeComTECCatalogueSourceID)},
		{subFamily: "tec", wantSub: MeerstetterSubFamilyTEC, wantVar: MeerstetterSubFamilyTEC, wantSourceFamily: graphsem.SourceFamily(DefaultMeComTECCatalogueSourceID)},
		{subFamily: "ldd", wantSub: MeerstetterSubFamilyLDD, wantVar: MeerstetterSubFamilyLDD, wantSourceFamily: graphsem.SourceFamily("mecom_ldd")},
		{subFamily: "LDD-130x", wantSub: MeerstetterSubFamilyLDD, wantVar: MeerstetterVariantLDD130x, wantSourceFamily: graphsem.SourceFamily("mecom_ldd_130x")},
		{subFamily: "daq", wantSub: MeerstetterSubFamilyDAQ, wantVar: MeerstetterSubFamilyDAQ, wantSourceFamily: graphsem.SourceFamily("mecom_daq")},
		{subFamily: "rmm-1182", wantSub: MeerstetterSubFamilyRMM, wantVar: MeerstetterVariantRMM1182, wantSourceFamily: graphsem.SourceFamily("mecom_rmm_1182")},
	}
	for _, tc := range cases {
		definition, ok := ResolveMeComCatalogueDefinition("", "", tc.subFamily)
		if !ok {
			t.Fatalf("definition %q did not resolve", tc.subFamily)
		}
		if definition.System != MeComDefinitionSystem ||
			definition.Family != MeerstetterDefinitionFamily ||
			definition.SubFamily != tc.wantSub ||
			definition.Variant != tc.wantVar ||
			definition.SourceFamily != tc.wantSourceFamily {
			t.Fatalf("definition %q = %#v", tc.subFamily, definition)
		}
	}
	if _, ok := ResolveMeComCatalogueDefinition("", "", "unknown"); ok {
		t.Fatalf("unexpected resolution for unknown subfamily")
	}
}

func TestBuildMeComTECCatalogueIncludesDerivedThermalPowerRows(t *testing.T) {
	catalogue := BuildMeComTECCatalogue(MeComTECCatalogueConfig{
		ChannelCount:      4,
		ControllerAddress: 0x30,
	})

	electrical := findCatalogueEntry(t, catalogue, "mecom.tec_02.electrical_input_w")
	if electrical.Category != graphsem.CategoryPower || electrical.Unit != "W" {
		t.Fatalf("electrical derived row category/unit = %q/%q", electrical.Category, electrical.Unit)
	}
	if electrical.Metadata["derived_signal"] != "true" ||
		electrical.Metadata["preferred_readout"] != "mecom_derived_channel_model" ||
		electrical.Metadata["source_parameters"] != "1020,1021" ||
		electrical.Metadata["channel_mode_policy"] != "mode_aware_no_thermal_inference_for_power_supply" {
		t.Fatalf("electrical derived metadata = %#v", electrical.Metadata)
	}

	heatFromItem := findCatalogueEntry(t, catalogue, "mecom.tec_02.heat_pumped_from_item_w")
	if heatFromItem.Metadata["semantic_role"] != "heat_pumped_from_item" ||
		heatFromItem.Metadata["source_readout"] != "mecom_crtvstream_ring_buffer" ||
		heatFromItem.Metadata["ring_reduction"] != "mean_stddev_window_to_consumer_rate" {
		t.Fatalf("heat-from-item metadata = %#v", heatFromItem.Metadata)
	}

	hotSide := findCatalogueEntry(t, catalogue, "mecom.tec_02.hot_side_dissipated_w")
	if hotSide.Metadata["semantic_role"] != "hot_side_dissipated_heat" ||
		hotSide.Metadata["calculation_boundary"] != "edge_estimate_requires_channel_mode_and_optional_module_data" {
		t.Fatalf("hot-side metadata = %#v", hotSide.Metadata)
	}
}

func TestBuildMeComTECCatalogueDescribesDriveCascadeAndReadoutSemantics(t *testing.T) {
	catalogue := BuildMeComTECCatalogue(MeComTECCatalogueConfig{
		ChannelCount:      8,
		ControllerAddress: 80,
		FixtureProvenance: "thermal_fat_8spot_fixture",
	})

	objectTemp := findCatalogueEntry(t, catalogue, "mecom.tec_07.object_temp_c")
	if objectTemp.Metadata["semantic_role"] != "test_spot_temperature" {
		t.Fatalf("object temp semantic_role = %q", objectTemp.Metadata["semantic_role"])
	}
	if objectTemp.Metadata["physical_spot"] != "spot_07" || objectTemp.Metadata["dut_id"] != "OH7" {
		t.Fatalf("object temp spot metadata = %#v", objectTemp.Metadata)
	}
	if objectTemp.Metadata["controller_address"] != "80" {
		t.Fatalf("controller_address = %q", objectTemp.Metadata["controller_address"])
	}
	if objectTemp.Metadata["preferred_readout"] != "mecom_crtvstream_ring_buffer" ||
		objectTemp.Metadata["background_readout"] != "mecom_vx_round_robin_queue" ||
		objectTemp.Metadata["ring_reduction"] != "mean_stddev_window_to_consumer_rate" ||
		objectTemp.Metadata["consumer_rate_policy"] != "publish_reduced_windows_at_requested_rate" ||
		objectTemp.Metadata["manual_poll_policy"] != "enqueue_front_return_latest_when_polled" {
		t.Fatalf("readout metadata = %#v", objectTemp.Metadata)
	}
	if objectTemp.Metadata["ui_tree_path"] != "Thermal / Object / object temperature" ||
		objectTemp.Metadata["ui_tree_projection"] != "operator" ||
		!strings.Contains(objectTemp.Metadata["ui_tree_paths"], `"id":"protocol"`) ||
		!strings.Contains(objectTemp.Metadata["ui_tree_paths"], `Parameter 1000`) {
		t.Fatalf("generated object-temperature tree projections = %#v", objectTemp.Metadata)
	}

	cascade := findCatalogueEntry(t, catalogue, "mecom.tec_07.cascade_temp_c")
	if cascade.Metadata["semantic_role"] != "cascade_temperature" ||
		cascade.Metadata["source_parameter_name"] != "External Object Temperature" ||
		cascade.Metadata["preferred_readout"] != "mecom_crtvstream_ring_buffer" {
		t.Fatalf("cascade metadata = %#v", cascade.Metadata)
	}
	if cascade.Metadata["raw_numeric_fallback"] != "param_52200.instance_7" {
		t.Fatalf("cascade raw fallback = %q", cascade.Metadata["raw_numeric_fallback"])
	}

	outputVoltage := findCatalogueEntry(t, catalogue, "mecom.tec_07.output_voltage_v")
	if outputVoltage.Metadata["semantic_role"] != "drive_voltage" ||
		outputVoltage.Metadata["drive_component"] != "voltage" ||
		outputVoltage.Metadata["priority_group"] != "drive_telemetry" {
		t.Fatalf("output voltage metadata = %#v", outputVoltage.Metadata)
	}

	power := findCatalogueEntry(t, catalogue, "mecom.tec_07.output_power_w")
	if power.Category != graphsem.CategoryPower || power.Unit != "W" {
		t.Fatalf("OH power row category/unit = %q/%q", power.Category, power.Unit)
	}
	if power.Metadata["semantic_role"] != "drive_power" ||
		power.Metadata["drive_component"] != "watt" ||
		power.Metadata["preferred_readout"] != "mecom_crtvstream_ring_buffer" {
		t.Fatalf("power semantic_role = %q", power.Metadata["semantic_role"])
	}
	if power.Metadata["raw_numeric_fallback"] != "param_1022.instance_7" {
		t.Fatalf("raw fallback = %q", power.Metadata["raw_numeric_fallback"])
	}

	outputStageTemp := findCatalogueEntry(t, catalogue, "mecom.tec_07.output_stage_temp_c")
	if outputStageTemp.Metadata["semantic_role"] != "output_stage_temperature" ||
		outputStageTemp.Metadata["preferred_readout"] != "mecom_crtvstream_ring_buffer" ||
		outputStageTemp.Metadata["raw_numeric_fallback"] != "param_40000.instance_7" {
		t.Fatalf("output-stage temperature metadata = %#v", outputStageTemp.Metadata)
	}
	if outputStageTemp.Metadata["ui_tree_path"] != "Thermal / Output stage / Output Stage Temperature" {
		t.Fatalf("output-stage temperature tree metadata = %#v", outputStageTemp.Metadata)
	}
	if outputStageTemp.Metadata["ui_tree_projection"] != "operator" ||
		!strings.Contains(outputStageTemp.Metadata["ui_tree_paths"], `"id":"protocol"`) {
		t.Fatalf("output-stage temperature tree projection metadata = %#v", outputStageTemp.Metadata)
	}

	stable := findCatalogueEntry(t, catalogue, "mecom.tec_07.temperature_stable")
	if stable.ValueType != "int" ||
		stable.Metadata["readout_priority"] != "background" ||
		stable.Metadata["preferred_readout"] != "mecom_vx_round_robin_queue" {
		t.Fatalf("temperature stable row = %#v metadata=%#v", stable, stable.Metadata)
	}
}

func TestDefaultTECCatalogueEntriesCarryTelemetryCommandCounterparts(t *testing.T) {
	entries := DefaultTECCatalogueEntries(1)

	objectTemp := findTECCatalogueProtocolEntry(t, entries, 1000, 1)
	assertCounterpart(t, objectTemp, "setpoint", 3000)
	assertCounterpart(t, objectTemp, "object_external_input", 52200)
	assertCounterpart(t, objectTemp, "object_source_selection", 6300)

	sinkTemp := findTECCatalogueProtocolEntry(t, entries, 1001, 1)
	assertCounterpart(t, sinkTemp, "fixed_sink_input", 52201)
	assertCounterpart(t, sinkTemp, "sink_source_selection", 6304)

	targetTemp := findTECCatalogueProtocolEntry(t, entries, 3000, 1)
	assertCounterpart(t, targetTemp, "measured", 1000)

	externalObject := findTECCatalogueProtocolEntry(t, entries, 52200, 1)
	assertCounterpart(t, externalObject, "source_selection", 6300)
	assertCounterpart(t, externalObject, "measured", 1000)

	fixedSink := findTECCatalogueProtocolEntry(t, entries, 52201, 1)
	assertCounterpart(t, fixedSink, "source_selection", 6304)
	assertCounterpart(t, fixedSink, "measured", 1001)

	objectSource := findTECCatalogueProtocolEntry(t, entries, 6300, 1)
	assertCounterpart(t, objectSource, "external_value", 52200)
	assertCounterpart(t, objectSource, "measured", 1000)

	sinkSource := findTECCatalogueProtocolEntry(t, entries, 6304, 1)
	assertCounterpart(t, sinkSource, "fixed_value", 52201)
	assertCounterpart(t, sinkSource, "measured", 1001)

	outputPower := findTECCatalogueProtocolEntry(t, entries, 1022, 1)
	assertCounterpart(t, outputPower, "source", 1020, 1021)
	assertCounterpart(t, outputPower, "limit", 2030, 2031)

	voltageCommand := findTECCatalogueProtocolEntry(t, entries, 2021, 1)
	assertCounterpart(t, voltageCommand, "measured", 1021)
	assertCounterpart(t, voltageCommand, "limit", 2031)

	currentLimit := findTECCatalogueProtocolEntry(t, entries, 2030, 1)
	assertCounterpart(t, currentLimit, "measured", 1020)
	assertCounterpart(t, currentLimit, "command", 2020)
}

func TestTECCatalogueRuntimeMetadataCarriesHarvestedHelp(t *testing.T) {
	entries := DefaultTECCatalogueEntries(1)

	targetTemp := findTECCatalogueProtocolEntry(t, entries, 3000, 1)
	if !strings.Contains(strings.ToLower(targetTemp.Help), "target temperature") {
		t.Fatalf("3000 help = %q, want harvested target-temperature tooltip", targetTemp.Help)
	}
	if targetTemp.Metadata["help"] != targetTemp.Help {
		t.Fatalf("3000 metadata help = %q, want %q", targetTemp.Metadata["help"], targetTemp.Help)
	}
	if targetTemp.Visibility == "" || targetTemp.Metadata["visibility"] != targetTemp.Visibility {
		t.Fatalf("3000 visibility not carried through: entry=%q metadata=%#v", targetTemp.Visibility, targetTemp.Metadata)
	}
	if targetTemp.Metadata["source_access"] != "read_write" {
		t.Fatalf("3000 source access = %q, want harvested read/write access", targetTemp.Metadata["source_access"])
	}
	if !strings.Contains(targetTemp.Metadata["source_evidence"], "tectemperaturecontrollerwindow.baml") {
		t.Fatalf("3000 source evidence = %q, want CoSo BAML provenance", targetTemp.Metadata["source_evidence"])
	}

	outputEnable := findTECCatalogueProtocolEntry(t, entries, 2010, 1)
	if outputEnable.SafetyNote == "" || outputEnable.Metadata["safety_note"] != outputEnable.SafetyNote {
		t.Fatalf("2010 safety note not carried through: entry=%q metadata=%#v", outputEnable.SafetyNote, outputEnable.Metadata)
	}

	outputStageTemp := findTECCatalogueProtocolEntry(t, entries, 40000, 1)
	if outputStageTemp.Access != "read" {
		t.Fatalf("40000 access = %q, want hidden output-stage temperature to stay read-only", outputStageTemp.Access)
	}
	if outputStageTemp.Visibility != "advanced" || outputStageTemp.Metadata["visibility"] != "advanced" {
		t.Fatalf("40000 visibility not marked advanced: entry=%q metadata=%#v", outputStageTemp.Visibility, outputStageTemp.Metadata)
	}

	catalogue := BuildMeComTECCatalogue(MeComTECCatalogueConfig{ChannelCount: 1})
	row := findCatalogueEntry(t, catalogue, "mecom.tec_01.target_object_temp_c")
	if row.Metadata["help"] == "" || row.Metadata["visibility"] == "" {
		t.Fatalf("source catalogue row missing harvested help metadata: %#v", row.Metadata)
	}
}

func TestBuildMeComTECCatalogueUsesPublicGatewayEndpoint(t *testing.T) {
	catalogue := BuildMeComTECCatalogue(MeComTECCatalogueConfig{})
	endpoint := catalogue.Capabilities.SubscriptionEndpoint
	if endpoint != "/api/devices/{device_id}/poll" {
		t.Fatalf("subscription endpoint = %q, want public gateway poll route", endpoint)
	}
	if strings.Contains(strings.ToLower(endpoint), "loom") {
		t.Fatalf("subscription endpoint leaks private deployment naming: %q", endpoint)
	}
}

func findCatalogueEntry(t *testing.T, catalogue graphsem.SourceCatalogue, traceID string) graphsem.SourceCatalogueRow {
	t.Helper()
	for _, entry := range catalogue.Entries {
		if entry.TraceID == traceID {
			return entry
		}
	}
	t.Fatalf("missing catalogue entry %q", traceID)
	return graphsem.SourceCatalogueRow{}
}

func findTECCatalogueProtocolEntry(t *testing.T, entries []TECCatalogueEntry, id int, instance int) TECCatalogueEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.ID == id && entry.Instance == instance {
			return entry
		}
	}
	t.Fatalf("missing TEC catalogue entry %d:%d", id, instance)
	return TECCatalogueEntry{}
}

func findTECCatalogueRawName(t *testing.T, entries []TECCatalogueEntry, rawName string) TECCatalogueEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.RawName == rawName {
			return entry
		}
	}
	t.Fatalf("missing TEC catalogue entry raw name %q", rawName)
	return TECCatalogueEntry{}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertCounterpart(t *testing.T, entry TECCatalogueEntry, role string, ids ...int) {
	t.Helper()
	got := entry.Counterparts[role]
	if len(got) != len(ids) {
		t.Fatalf("entry %d:%d counterpart %q = %#v, want %#v", entry.ID, entry.Instance, role, got, ids)
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("entry %d:%d counterpart %q = %#v, want %#v", entry.ID, entry.Instance, role, got, ids)
		}
	}
}
