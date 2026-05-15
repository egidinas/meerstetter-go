package mecom

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egidinas/signalforge/graphsem"
)

const DefaultMeComTECCatalogueSourceID = "mecom_tec"

const (
	mecomReadoutPriorityHigh       = "high"
	mecomReadoutPriorityBackground = "background"
	mecomRingReadout               = "mecom_crtvstream_ring_buffer"
	mecomBackgroundReadout         = "mecom_vx_round_robin_queue"
	mecomDerivedReadout            = "mecom_derived_channel_model"
	mecomRingReduction             = "mean_stddev_window_to_consumer_rate"
)

type MeComTECCatalogueConfig struct {
	SourceID          string
	DisplayName       string
	ChannelCount      int
	SourceSubject     string
	ControllerAddress int
	FixtureProvenance string
}

type mecomTECParameter struct {
	id                  int
	suffix              string
	rawName             string
	display             string
	unit                string
	valueType           string
	category            graphsem.SignalCategory
	role                graphsem.SignalRole
	semanticRole        string
	sourceParameterName string
	priority            string
	preferredReadout    string
	priorityComponent   string
	aliases             []string
}

type mecomTECDerivedParameter struct {
	suffix           string
	rawName          string
	display          string
	semanticRole     string
	sourceParameters string
}

// DefaultTECReadoutParameters returns the reusable polling plan for TEC
// controllers. High-priority values are intended for CRTVStream ring-buffer
// capture; the remainder use the bulk round-robin queue.
func DefaultTECReadoutParameters(channels int) []ReadoutParameter {
	if channels <= 0 {
		channels = 8
	}
	out := make([]ReadoutParameter, 0, channels*len(mecomTECParameters))
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECParameters {
			out = append(out, ReadoutParameter{
				Parameter: Parameter{
					ID:       param.id,
					Instance: ch,
					Name:     param.suffix,
					Unit:     param.unit,
					Type:     param.dataType(),
				},
				Sensor:       fmt.Sprintf("mecom.tec_%02d.%s", ch, param.suffix),
				HighPriority: param.readoutPriority() == mecomReadoutPriorityHigh,
			})
		}
	}
	return out
}

func DefaultTECSignalNames(channels int) []string {
	params := DefaultTECReadoutParameters(channels)
	out := make([]string, 0, len(params)+channels*len(mecomTECDerivedParameters))
	for _, param := range params {
		out = append(out, param.Sensor)
	}
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECDerivedParameters {
			out = append(out, fmt.Sprintf("mecom.tec_%02d.%s", ch, param.suffix))
		}
	}
	return out
}

func DefaultTECUnits() []string {
	seen := map[string]struct{}{}
	var units []string
	for _, param := range mecomTECParameters {
		if param.unit == "" {
			continue
		}
		if _, ok := seen[param.unit]; ok {
			continue
		}
		seen[param.unit] = struct{}{}
		units = append(units, param.unit)
	}
	return units
}

var mecomTECParameters = []mecomTECParameter{
	{id: 1000, suffix: "object_temp_c", rawName: "ObjectTemp", display: "object temperature", unit: "degC", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "test_spot_temperature", sourceParameterName: "Object Temperature", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "celsius"},
	{id: 1001, suffix: "sink_temp_c", rawName: "SinkTemp", display: "sink temperature", unit: "degC", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "tec_sink_temperature", sourceParameterName: "Sink Temperature", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "celsius"},
	{id: 52200, suffix: "cascade_temp_c", rawName: "CascadeTemp", display: "cascade temperature", unit: "degC", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "cascade_temperature", sourceParameterName: "External Object Temperature", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "celsius", aliases: []string{"external_object_temp_c"}},
	{id: 3000, suffix: "target_object_temp_c", rawName: "TargetObjectTemp", display: "target object temperature", unit: "degC", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "target_object_temperature", sourceParameterName: "Target Object Temperature", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "celsius"},
	{id: 1011, suffix: "ramp_object_temp_c", rawName: "RampObjectTemp", display: "ramp object temperature", unit: "degC", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "ramp_object_temperature", sourceParameterName: "Ramp Object Temperature", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "celsius"},
	{id: 1020, suffix: "output_current_a", rawName: "OutputCurrent", display: "output current", unit: "A", category: graphsem.CategoryElectrical, role: graphsem.RoleMonitor, semanticRole: "drive_current", sourceParameterName: "Actual Output Current", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "ampere"},
	{id: 1021, suffix: "output_voltage_v", rawName: "OutputVoltage", display: "output voltage", unit: "V", category: graphsem.CategoryElectrical, role: graphsem.RoleMonitor, semanticRole: "drive_voltage", sourceParameterName: "Actual Output Voltage", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "voltage"},
	{id: 1022, suffix: "output_power_w", rawName: "OutputPower", display: "output power", unit: "W", category: graphsem.CategoryPower, role: graphsem.RoleMonitor, semanticRole: "drive_power", sourceParameterName: "Actual Output Power", priority: mecomReadoutPriorityHigh, preferredReadout: mecomRingReadout, priorityComponent: "watt"},
	{id: 1200, suffix: "temperature_stable", rawName: "TemperatureStable", display: "temperature stable", valueType: "int", category: graphsem.CategoryThermal, role: graphsem.RoleMonitor, semanticRole: "temperature_stable", sourceParameterName: "Temperature is Stable", priority: mecomReadoutPriorityBackground, preferredReadout: mecomBackgroundReadout},
}

var mecomTECDerivedParameters = []mecomTECDerivedParameter{
	{suffix: derivedElectricalInputName, rawName: "ElectricalInput", display: "electrical input", semanticRole: "electrical_input_power", sourceParameters: "1020,1021"},
	{suffix: derivedHeatPumpedFromItemName, rawName: "HeatPumpedFromItem", display: "heat pumped from item", semanticRole: "heat_pumped_from_item", sourceParameters: "1000,1001,1020,1021"},
	{suffix: derivedResistiveHeatName, rawName: "ResistiveHeat", display: "resistive heat", semanticRole: "resistive_heat", sourceParameters: "1020,1021"},
	{suffix: derivedHotSideDissipatedName, rawName: "HotSideDissipated", display: "hot-side dissipated heat", semanticRole: "hot_side_dissipated_heat", sourceParameters: "1000,1001,1020,1021"},
}

func BuildMeComTECCatalogue(cfg MeComTECCatalogueConfig) graphsem.SourceCatalogue {
	if cfg.SourceID == "" {
		cfg.SourceID = DefaultMeComTECCatalogueSourceID
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = "MeCom TEC controller bank"
	}
	if cfg.ChannelCount <= 0 {
		cfg.ChannelCount = 8
	}
	entries := make([]graphsem.SourceCatalogueRow, 0, cfg.ChannelCount*(len(mecomTECParameters)+len(mecomTECDerivedParameters)))
	for ch := 1; ch <= cfg.ChannelCount; ch++ {
		for _, param := range mecomTECParameters {
			entries = append(entries, mecomTECRow(cfg, ch, param))
		}
		for _, param := range mecomTECDerivedParameters {
			entries = append(entries, mecomTECDerivedRow(cfg, ch, param))
		}
	}
	return graphsem.SourceCatalogue{
		SchemaVersion: graphsem.CurrentSourceCatalogueSchemaVersion,
		SourceID:      cfg.SourceID,
		SourceFamily:  graphsem.SourceFamilyMeComTec,
		DisplayName:   cfg.DisplayName,
		Entries:       entries,
		Capabilities: graphsem.SourceCapabilities{
			SupportsLive:          true,
			SupportsHistory:       true,
			SupportsMetadataOnly:  true,
			MaxSignals:            len(entries),
			DefaultRateHz:         1,
			RecommendedRateHz:     1,
			SubscriptionEndpoint:  "/api/devices/{device_id}/poll",
			LiveSubjects:          compactStrings([]string{cfg.SourceSubject}),
			SelectionRequired:     true,
			PoliteAccessStatement: "MeCom TEC rows prefer CRTVStream ring-buffer readout for high-priority values, reduced by mean/stddev windows to the consumer-requested rate; background values use ?VX round-robin chunks and raw single reads remain compatibility fallback.",
		},
	}
}

func mecomTECDerivedRow(cfg MeComTECCatalogueConfig, ch int, param mecomTECDerivedParameter) graphsem.SourceCatalogueRow {
	spotID := fmt.Sprintf("spot_%02d", ch)
	ohID := fmt.Sprintf("OH%d", ch)
	metadata := map[string]string{
		"channel_index":        strconv.Itoa(ch),
		"semantic_role":        param.semanticRole,
		"physical_spot":        spotID,
		"derived_signal":       "true",
		"mecom_instance":       strconv.Itoa(ch),
		"source_parameters":    param.sourceParameters,
		"readout_priority":     mecomReadoutPriorityHigh,
		"preferred_readout":    mecomDerivedReadout,
		"source_readout":       mecomRingReadout,
		"ring_reduction":       mecomRingReduction,
		"consumer_rate_policy": "publish_reduced_windows_at_requested_rate",
		"manual_poll_policy":   "source_parameters_enqueue_front_return_derived_when_polled",
		"single_read_policy":   "not_applicable_derived_from_measured_values",
		"channel_mode_policy":  "mode_aware_no_thermal_inference_for_power_supply",
		"calculation_boundary": "edge_estimate_requires_channel_mode_and_optional_module_data",
	}
	if cfg.ControllerAddress > 0 {
		metadata["controller_address"] = strconv.Itoa(cfg.ControllerAddress)
	}
	if cfg.FixtureProvenance != "" {
		metadata["fixture_provenance"] = cfg.FixtureProvenance
		metadata["dut_id"] = ohID
	}
	return graphsem.SourceCatalogueRow{
		TraceID:        fmt.Sprintf("mecom.tec_%02d.%s", ch, param.suffix),
		RawName:        fmt.Sprintf("TEC_CH%d_%s", ch, param.rawName),
		DisplayName:    fmt.Sprintf("Spot %02d / %s %s", ch, ohID, param.display),
		Unit:           "W",
		ValueType:      "float",
		Access:         "subscribe",
		GraphSource:    "nats_edge",
		GraphType:      "line",
		Category:       graphsem.CategoryPower,
		Kind:           graphsem.KindContinuous,
		Role:           graphsem.RoleMonitor,
		DefaultHint:    graphsem.HintLine,
		SemanticStatus: "backend_semantic_mapping",
		SourceSubject:  cfg.SourceSubject,
		Metadata:       metadata,
	}
}

func mecomTECRow(cfg MeComTECCatalogueConfig, ch int, param mecomTECParameter) graphsem.SourceCatalogueRow {
	spotID := fmt.Sprintf("spot_%02d", ch)
	ohID := fmt.Sprintf("OH%d", ch)
	metadata := map[string]string{
		"channel_index":         strconv.Itoa(ch),
		"semantic_role":         param.semanticRole,
		"physical_spot":         spotID,
		"mecom_parameter_id":    strconv.Itoa(param.id),
		"mecom_instance":        strconv.Itoa(ch),
		"raw_numeric_fallback":  fmt.Sprintf("param_%d.instance_%d", param.id, ch),
		"source_parameter_name": param.sourceParameterName,
		"readout_priority":      param.readoutPriority(),
		"preferred_readout":     param.effectivePreferredReadout(),
		"background_readout":    mecomBackgroundReadout,
		"manual_poll_policy":    "enqueue_front_return_latest_when_polled",
		"single_read_policy":    "compatibility_only",
	}
	if param.readoutPriority() == mecomReadoutPriorityHigh {
		metadata["ring_reduction"] = mecomRingReduction
		metadata["consumer_rate_policy"] = "publish_reduced_windows_at_requested_rate"
	} else {
		metadata["consumer_rate_policy"] = "publish_latest_when_round_robin_queue_updates"
	}
	if param.priorityComponent != "" {
		metadata["priority_group"] = "drive_telemetry"
		metadata["drive_component"] = param.priorityComponent
	}
	if len(param.aliases) > 0 {
		metadata["aliases"] = strings.Join(param.aliases, ",")
	}
	if cfg.ControllerAddress > 0 {
		metadata["controller_address"] = strconv.Itoa(cfg.ControllerAddress)
	}
	if cfg.FixtureProvenance != "" {
		metadata["fixture_provenance"] = cfg.FixtureProvenance
		metadata["dut_id"] = ohID
	}
	return graphsem.SourceCatalogueRow{
		TraceID:        fmt.Sprintf("mecom.tec_%02d.%s", ch, param.suffix),
		RawName:        fmt.Sprintf("TEC_CH%d_%s", ch, param.rawName),
		DisplayName:    fmt.Sprintf("Spot %02d / %s %s", ch, ohID, param.display),
		Unit:           param.unit,
		ValueType:      param.effectiveValueType(),
		Access:         "subscribe",
		GraphSource:    "nats_edge",
		GraphType:      "line",
		Category:       param.category,
		Kind:           graphsem.KindContinuous,
		Role:           param.role,
		DefaultHint:    graphsem.HintLine,
		SemanticStatus: "backend_semantic_mapping",
		SourceSubject:  cfg.SourceSubject,
		Metadata:       metadata,
	}
}

func (p mecomTECParameter) effectiveValueType() string {
	if p.valueType != "" {
		return p.valueType
	}
	return "float"
}

func (p mecomTECParameter) dataType() DataType {
	if p.effectiveValueType() == "int" {
		return DataTypeInt32
	}
	return DataTypeFloat32
}

func (p mecomTECParameter) readoutPriority() string {
	if p.priority != "" {
		return p.priority
	}
	return mecomReadoutPriorityBackground
}

func (p mecomTECParameter) effectivePreferredReadout() string {
	if p.preferredReadout != "" {
		return p.preferredReadout
	}
	if p.readoutPriority() == mecomReadoutPriorityHigh {
		return mecomRingReadout
	}
	return mecomBackgroundReadout
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
