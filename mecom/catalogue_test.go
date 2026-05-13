package mecom

import (
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
	if catalogue.SourceID != "bench_mecom" || catalogue.SourceFamily != graphsem.SourceFamilyMeComTec {
		t.Fatalf("catalogue identity = %q/%q", catalogue.SourceID, catalogue.SourceFamily)
	}
	if got, want := len(catalogue.Entries), 39; got != want {
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

	stable := findCatalogueEntry(t, catalogue, "mecom.tec_07.temperature_stable")
	if stable.ValueType != "int" ||
		stable.Metadata["readout_priority"] != "background" ||
		stable.Metadata["preferred_readout"] != "mecom_vx_round_robin_queue" {
		t.Fatalf("temperature stable row = %#v metadata=%#v", stable, stable.Metadata)
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
