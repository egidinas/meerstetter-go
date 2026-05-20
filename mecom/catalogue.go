package mecom

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	_ "embed"

	"github.com/egidinas/signalforge/graphsem"
)

func RoleForParam(id int) string {
	for _, p := range DefaultTECCatalogueEntries(1) {
		if p.ID == id {
			return p.Role
		}
	}
	return "aux"
}

func KindForParam(id int) string {
	for _, p := range DefaultTECCatalogueEntries(1) {
		if p.ID == id {
			return p.Kind
		}
	}
	return "continuous"
}

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
	ID                  int                     `json:"id"`
	Suffix              string                  `json:"sid"`
	RawName             string                  `json:"raw_name"`
	Display             string                  `json:"display"`
	Unit                string                  `json:"unit,omitempty"`
	ValueType           string                  `json:"type,omitempty"`
	Category            graphsem.SignalCategory `json:"category,omitempty"`
	Role                graphsem.SignalRole     `json:"role,omitempty"`
	SemanticRole        string                  `json:"semantic_role,omitempty"`
	SourceParameterName string                  `json:"source_parameter_name,omitempty"`
	Help                string                  `json:"help,omitempty"`
	Visibility          string                  `json:"visibility,omitempty"`
	SourceEvidence      []string                `json:"source_evidence,omitempty"`
	Priority            string                  `json:"readout_priority,omitempty"`
	PreferredReadout    string                  `json:"preferred_readout,omitempty"`
	PriorityComponent   string                  `json:"priority_component,omitempty"`
	Aliases             []string                `json:"aliases,omitempty"`
	Writable            bool                    `json:"writable,omitempty"`
	Access              string                  `json:"access,omitempty"`
	Group               string                  `json:"group,omitempty"`
	Subgroup            string                  `json:"subgroup,omitempty"`
	TreePath            []string                `json:"tree_path,omitempty"`
	TreePaths           []mecomTECTreePath      `json:"tree_paths,omitempty"`
	ApplicableModes     []string                `json:"applicable_modes,omitempty"`
	Command             string                  `json:"command,omitempty"`
	Minimum             *float64                `json:"min,omitempty"`
	Maximum             *float64                `json:"max,omitempty"`
	DefaultValue        *string                 `json:"default_value,omitempty"`
	Enum                map[string]string       `json:"enum,omitempty"`
	Dangerous           bool                    `json:"dangerous,omitempty"`
	TransportSupport    []string                `json:"transport_support,omitempty"`
	Counterparts        map[string][]int        `json:"counterparts,omitempty"`
}

type mecomTECDerivedParameter struct {
	Suffix           string             `json:"sid"`
	RawName          string             `json:"raw_name"`
	Display          string             `json:"display"`
	SemanticRole     string             `json:"semantic_role"`
	SourceParameters string             `json:"source_parameters"`
	Group            string             `json:"group,omitempty"`
	Subgroup         string             `json:"subgroup,omitempty"`
	TreePath         []string           `json:"tree_path,omitempty"`
	TreePaths        []mecomTECTreePath `json:"tree_paths,omitempty"`
}

type TECCatalogueTreePath struct {
	ID      string   `json:"id"`
	Label   string   `json:"label,omitempty"`
	Path    []string `json:"path"`
	Default bool     `json:"default,omitempty"`
}

type mecomTECTreePath = TECCatalogueTreePath

type TECCatalogueEntry struct {
	ID                  int                    `json:"id"`
	Instance            int                    `json:"instance"`
	DisplayName         string                 `json:"display_name,omitempty"`
	RawName             string                 `json:"raw_name,omitempty"`
	Unit                string                 `json:"unit,omitempty"`
	Type                string                 `json:"type,omitempty"`
	Role                string                 `json:"role,omitempty"`
	Group               string                 `json:"group,omitempty"`
	Subgroup            string                 `json:"subgroup,omitempty"`
	Category            string                 `json:"category,omitempty"`
	Kind                string                 `json:"kind,omitempty"`
	Access              string                 `json:"access,omitempty"`
	SemanticRole        string                 `json:"semantic_role,omitempty"`
	SourceParameterName string                 `json:"source_parameter_name,omitempty"`
	Help                string                 `json:"help,omitempty"`
	Visibility          string                 `json:"visibility,omitempty"`
	SafetyNote          string                 `json:"safety_note,omitempty"`
	SourceEvidence      []string               `json:"source_evidence,omitempty"`
	ReadoutPriority     string                 `json:"readout_priority,omitempty"`
	PreferredReadout    string                 `json:"preferred_readout,omitempty"`
	Command             string                 `json:"command,omitempty"`
	Min                 *float64               `json:"min,omitempty"`
	Max                 *float64               `json:"max,omitempty"`
	DefaultValue        *string                `json:"default_value,omitempty"`
	Enum                map[string]string      `json:"enum,omitempty"`
	Dangerous           bool                   `json:"dangerous,omitempty"`
	TransportSupport    []string               `json:"transport_support,omitempty"`
	Counterparts        map[string][]int       `json:"counterparts,omitempty"`
	TreePath            []string               `json:"tree_path,omitempty"`
	TreePaths           []TECCatalogueTreePath `json:"tree_paths,omitempty"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
}

func DefaultTECCatalogueEntries(channels int) []TECCatalogueEntry {
	if channels <= 0 {
		channels = 8
	}
	out := make([]TECCatalogueEntry, 0, channels*(len(mecomTECParameters())+len(mecomTECWriteParameters())))
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECParameters() {
			out = append(out, tecCatalogueEntryFromParameter(ch, param))
		}
		for _, param := range mecomTECWriteParameters() {
			out = append(out, tecCatalogueEntryFromParameter(ch, param))
		}
	}
	return out
}

func tecCatalogueEntryFromParameter(instance int, param mecomTECParameter) TECCatalogueEntry {
	help := tecHelpForParameter(param)
	metadata := map[string]string{
		"semantic_role":         param.SemanticRole,
		"source_parameter_name": param.SourceParameterName,
		"readout_priority":      param.readoutPriority(),
		"preferred_readout":     param.effectivePreferredReadout(),
	}
	addTECHelpMetadata(metadata, help)
	if param.Command != "" {
		metadata["command"] = param.Command
	}
	if param.DefaultValue != nil {
		metadata["default_value"] = *param.DefaultValue
	}
	if len(param.TreePath) > 0 {
		metadata["tree_path"] = catalogueTreePath(param.TreePath)
	}
	if raw, err := json.Marshal(param.TreePaths); err == nil && len(param.TreePaths) > 0 {
		metadata["tree_paths"] = string(raw)
	}
	if raw, err := json.Marshal(param.Counterparts); err == nil && len(param.Counterparts) > 0 {
		metadata["counterparts"] = string(raw)
	}
	return TECCatalogueEntry{
		ID:                  param.ID,
		Instance:            instance,
		DisplayName:         param.Display,
		RawName:             param.RawName,
		Unit:                param.Unit,
		Type:                param.effectiveValueType(),
		Role:                string(param.Role),
		Group:               param.Group,
		Subgroup:            param.Subgroup,
		Category:            string(param.Category),
		Kind:                "continuous",
		Access:              param.access(),
		SemanticRole:        param.SemanticRole,
		SourceParameterName: param.SourceParameterName,
		Help:                help.Help,
		Visibility:          help.Visibility,
		SafetyNote:          help.SafetyNote,
		SourceEvidence:      append([]string(nil), help.SourceEvidence...),
		ReadoutPriority:     param.readoutPriority(),
		PreferredReadout:    param.effectivePreferredReadout(),
		DefaultValue:        cloneStringPointer(param.DefaultValue),
		Command:             param.Command,
		Min:                 param.Minimum,
		Max:                 param.Maximum,
		Enum:                param.Enum,
		Dangerous:           param.Dangerous,
		TransportSupport:    append([]string(nil), param.TransportSupport...),
		Counterparts:        cloneCounterparts(param.Counterparts),
		TreePath:            append([]string(nil), param.TreePath...),
		TreePaths:           append([]TECCatalogueTreePath(nil), param.TreePaths...),
		Metadata:            metadata,
	}
}

type tecCatalogueJSON struct {
	SchemaVersion     string                     `json:"schema_version"`
	ReadoutParameters []mecomTECParameter        `json:"readout_parameters"`
	WriteParameters   []mecomTECParameter        `json:"write_parameters"`
	DerivedParameters []mecomTECDerivedParameter `json:"derived_parameters"`
}

type mecomTECHelpSourceJSON struct {
	SchemaVersion string                           `json:"schema_version"`
	Parameters    map[string]mecomTECHelpParameter `json:"parameters"`
}

type mecomTECHelpParameter struct {
	MeParID        int      `json:"mepar_id"`
	Name           string   `json:"name"`
	Group          string   `json:"group"`
	Visibility     string   `json:"visibility"`
	Help           string   `json:"help"`
	SourceEvidence []string `json:"source_evidence"`
	SourceAccess   string   `json:"access"`
	SafetyNote     string   `json:"safety_note"`
}

//go:embed catalogues/tec.json
var defaultTECCatalogueJSON []byte

var defaultTECCatalogue = mustLoadTECCatalogueJSON(defaultTECCatalogueJSON)

//go:embed catalogues/sources/tec_tooltips.v631.json
var defaultTECHelpJSON []byte

var defaultTECHelpSource = mustLoadTECHelpSourceJSON(defaultTECHelpJSON)

func mustLoadTECCatalogueJSON(raw []byte) tecCatalogueJSON {
	catalogue, err := loadTECCatalogueJSON(raw)
	if err != nil {
		panic(err)
	}
	return catalogue
}

func loadTECCatalogueJSON(raw []byte) (tecCatalogueJSON, error) {
	var catalogue tecCatalogueJSON
	if err := json.Unmarshal(raw, &catalogue); err != nil {
		return tecCatalogueJSON{}, err
	}
	if catalogue.SchemaVersion == "" {
		return tecCatalogueJSON{}, fmt.Errorf("missing schema_version")
	}
	if len(catalogue.ReadoutParameters) == 0 {
		return tecCatalogueJSON{}, fmt.Errorf("readout_parameters must not be empty")
	}
	if len(catalogue.DerivedParameters) == 0 {
		return tecCatalogueJSON{}, fmt.Errorf("derived_parameters must not be empty")
	}
	for _, param := range append(append([]mecomTECParameter{}, catalogue.ReadoutParameters...), catalogue.WriteParameters...) {
		if param.ID <= 0 {
			return tecCatalogueJSON{}, fmt.Errorf("catalogue parameter %q has invalid id %d", param.Suffix, param.ID)
		}
		if param.Suffix == "" {
			return tecCatalogueJSON{}, fmt.Errorf("catalogue parameter %d missing sid", param.ID)
		}
		if param.effectiveValueType() == "" {
			return tecCatalogueJSON{}, fmt.Errorf("catalogue parameter %d missing type", param.ID)
		}
		if param.dataType() == "" {
			return tecCatalogueJSON{}, fmt.Errorf("catalogue parameter %d has unsupported type %q", param.ID, param.ValueType)
		}
		if err := validateCatalogueTreePath(fmt.Sprintf("catalogue parameter %d", param.ID), param.TreePath); err != nil {
			return tecCatalogueJSON{}, err
		}
		if err := validateCatalogueTreePaths(fmt.Sprintf("catalogue parameter %d", param.ID), param.TreePaths); err != nil {
			return tecCatalogueJSON{}, err
		}
	}
	for _, param := range catalogue.DerivedParameters {
		if err := validateCatalogueTreePath(fmt.Sprintf("derived catalogue parameter %q", param.Suffix), param.TreePath); err != nil {
			return tecCatalogueJSON{}, err
		}
		if err := validateCatalogueTreePaths(fmt.Sprintf("derived catalogue parameter %q", param.Suffix), param.TreePaths); err != nil {
			return tecCatalogueJSON{}, err
		}
	}
	return catalogue, nil
}

func mustLoadTECHelpSourceJSON(raw []byte) mecomTECHelpSourceJSON {
	help, err := loadTECHelpSourceJSON(raw)
	if err != nil {
		panic(err)
	}
	return help
}

func loadTECHelpSourceJSON(raw []byte) (mecomTECHelpSourceJSON, error) {
	var help mecomTECHelpSourceJSON
	if err := json.Unmarshal(raw, &help); err != nil {
		return mecomTECHelpSourceJSON{}, err
	}
	if help.SchemaVersion == "" {
		return mecomTECHelpSourceJSON{}, fmt.Errorf("missing TEC help schema_version")
	}
	return help, nil
}

func tecHelpForParameter(param mecomTECParameter) mecomTECHelpParameter {
	help := mecomTECHelpParameter{
		MeParID:        param.ID,
		Help:           param.Help,
		Visibility:     param.Visibility,
		SourceEvidence: append([]string(nil), param.SourceEvidence...),
	}
	if source, ok := defaultTECHelpSource.Parameters[strconv.Itoa(param.ID)]; ok {
		if help.Help == "" {
			help.Help = source.Help
		}
		if help.Visibility == "" {
			help.Visibility = source.Visibility
		}
		if help.SourceAccess == "" {
			help.SourceAccess = source.SourceAccess
		}
		if help.SafetyNote == "" {
			help.SafetyNote = source.SafetyNote
		}
		if len(help.SourceEvidence) == 0 {
			help.SourceEvidence = append([]string(nil), source.SourceEvidence...)
		}
		if help.Name == "" {
			help.Name = source.Name
		}
		if help.Group == "" {
			help.Group = source.Group
		}
	}
	return help
}

func addTECHelpMetadata(metadata map[string]string, help mecomTECHelpParameter) {
	if metadata == nil {
		return
	}
	if help.Help != "" {
		metadata["help"] = help.Help
	}
	if help.Visibility != "" {
		metadata["visibility"] = help.Visibility
	}
	if help.SourceAccess != "" {
		metadata["source_access"] = help.SourceAccess
	}
	if help.SafetyNote != "" {
		metadata["safety_note"] = help.SafetyNote
	}
	if len(help.SourceEvidence) > 0 {
		metadata["source_evidence"] = strings.Join(help.SourceEvidence, ";")
	}
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

// DefaultTECReadoutParameters returns the reusable polling plan for TEC
// controllers. High-priority values are intended for CRTVStream ring-buffer
// capture; the remainder use the bulk round-robin queue.
func DefaultTECReadoutParameters(channels int) []ReadoutParameter {
	if channels <= 0 {
		channels = 8
	}
	out := make([]ReadoutParameter, 0, channels*len(mecomTECParameters()))
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECParameters() {
			out = append(out, ReadoutParameter{
				Parameter: Parameter{
					ID:       param.ID,
					Instance: ch,
					Name:     param.Suffix,
					Unit:     param.Unit,
					Type:     param.dataType(),
					Writable: param.Writable,
				},
				Sensor:       fmt.Sprintf("mecom.tec_%02d.%s", ch, param.Suffix),
				HighPriority: param.readoutPriority() == mecomReadoutPriorityHigh,
			})
		}
	}
	return out
}

// DefaultTECWriteParameters returns the editable TEC parameters described by
// the JSON catalogue. Some entries, such as 3000, are also read back as live
// telemetry; callers that build a catalogue should de-duplicate by ID/instance.
func DefaultTECWriteParameters(channels int) []Parameter {
	if channels <= 0 {
		channels = 8
	}
	out := make([]Parameter, 0, channels*len(mecomTECWriteParameters()))
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECWriteParameters() {
			out = append(out, Parameter{
				ID:       param.ID,
				Instance: ch,
				Name:     param.Suffix,
				Unit:     param.Unit,
				Type:     param.dataType(),
				Writable: true,
			})
		}
	}
	return out
}

// TECParameterWritable reports catalogue writability from the JSON source of
// truth instead of duplicating write switches in gateway code.
func TECParameterWritable(id int) bool {
	for _, param := range mecomTECParameters() {
		if param.ID == id {
			return param.Writable
		}
	}
	for _, param := range mecomTECWriteParameters() {
		if param.ID == id {
			return true
		}
	}
	return false
}

func DefaultTECSignalNames(channels int) []string {
	params := DefaultTECReadoutParameters(channels)
	out := make([]string, 0, len(params)+channels*len(mecomTECDerivedParameters()))
	for _, param := range params {
		out = append(out, param.Sensor)
	}
	for ch := 1; ch <= channels; ch++ {
		for _, param := range mecomTECDerivedParameters() {
			out = append(out, fmt.Sprintf("mecom.tec_%02d.%s", ch, param.Suffix))
		}
	}
	return out
}

func DefaultTECUnits() []string {
	seen := map[string]struct{}{}
	var units []string
	for _, param := range mecomTECParameters() {
		if param.Unit == "" {
			continue
		}
		if _, ok := seen[param.Unit]; ok {
			continue
		}
		seen[param.Unit] = struct{}{}
		units = append(units, param.Unit)
	}
	return units
}

func mecomTECParameters() []mecomTECParameter {
	return append([]mecomTECParameter(nil), defaultTECCatalogue.ReadoutParameters...)
}

func mecomTECWriteParameters() []mecomTECParameter {
	return append([]mecomTECParameter(nil), defaultTECCatalogue.WriteParameters...)
}

func mecomTECDerivedParameters() []mecomTECDerivedParameter {
	return append([]mecomTECDerivedParameter(nil), defaultTECCatalogue.DerivedParameters...)
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
	entries := make([]graphsem.SourceCatalogueRow, 0, cfg.ChannelCount*(len(mecomTECParameters())+len(mecomTECDerivedParameters())))
	for ch := 1; ch <= cfg.ChannelCount; ch++ {
		for _, param := range mecomTECParameters() {
			entries = append(entries, mecomTECRow(cfg, ch, param))
		}
		for _, param := range mecomTECDerivedParameters() {
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
		"semantic_role":        param.SemanticRole,
		"physical_spot":        spotID,
		"derived_signal":       "true",
		"mecom_instance":       strconv.Itoa(ch),
		"source_parameters":    param.SourceParameters,
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
	addCatalogueTreePathMetadata(metadata, param.effectiveTreePaths())
	return graphsem.SourceCatalogueRow{
		TraceID:        fmt.Sprintf("mecom.tec_%02d.%s", ch, param.Suffix),
		RawName:        fmt.Sprintf("TEC_CH%d_%s", ch, param.RawName),
		DisplayName:    fmt.Sprintf("Spot %02d / %s %s", ch, ohID, param.Display),
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
	help := tecHelpForParameter(param)
	metadata := map[string]string{
		"channel_index":         strconv.Itoa(ch),
		"semantic_role":         param.SemanticRole,
		"physical_spot":         spotID,
		"mecom_parameter_id":    strconv.Itoa(param.ID),
		"mecom_instance":        strconv.Itoa(ch),
		"raw_numeric_fallback":  fmt.Sprintf("param_%d.instance_%d", param.ID, ch),
		"source_parameter_name": param.SourceParameterName,
		"readout_priority":      param.readoutPriority(),
		"preferred_readout":     param.effectivePreferredReadout(),
		"background_readout":    mecomBackgroundReadout,
		"manual_poll_policy":    "enqueue_front_return_latest_when_polled",
		"single_read_policy":    "compatibility_only",
	}
	addTECHelpMetadata(metadata, help)
	if param.readoutPriority() == mecomReadoutPriorityHigh {
		metadata["ring_reduction"] = mecomRingReduction
		metadata["consumer_rate_policy"] = "publish_reduced_windows_at_requested_rate"
	} else {
		metadata["consumer_rate_policy"] = "publish_latest_when_round_robin_queue_updates"
	}
	if param.PriorityComponent != "" {
		metadata["priority_group"] = "drive_telemetry"
		metadata["drive_component"] = param.PriorityComponent
	}
	if len(param.Aliases) > 0 {
		metadata["aliases"] = strings.Join(param.Aliases, ",")
	}
	if raw, err := json.Marshal(param.Counterparts); err == nil && len(param.Counterparts) > 0 {
		metadata["counterparts"] = string(raw)
	}
	if cfg.ControllerAddress > 0 {
		metadata["controller_address"] = strconv.Itoa(cfg.ControllerAddress)
	}
	if cfg.FixtureProvenance != "" {
		metadata["fixture_provenance"] = cfg.FixtureProvenance
		metadata["dut_id"] = ohID
	}
	addCatalogueTreePathMetadata(metadata, param.effectiveTreePaths())
	return graphsem.SourceCatalogueRow{
		TraceID:        fmt.Sprintf("mecom.tec_%02d.%s", ch, param.Suffix),
		RawName:        fmt.Sprintf("TEC_CH%d_%s", ch, param.RawName),
		DisplayName:    fmt.Sprintf("Spot %02d / %s %s", ch, ohID, param.Display),
		Unit:           param.Unit,
		ValueType:      param.graphValueType(),
		Access:         "subscribe",
		GraphSource:    "nats_edge",
		GraphType:      "line",
		Category:       param.Category,
		Kind:           graphsem.KindContinuous,
		Role:           param.Role,
		DefaultHint:    graphsem.HintLine,
		SemanticStatus: "backend_semantic_mapping",
		SourceSubject:  cfg.SourceSubject,
		Metadata:       metadata,
	}
}

func (p mecomTECParameter) effectiveValueType() string {
	if p.ValueType != "" {
		return p.ValueType
	}
	return "float32"
}

func (p mecomTECParameter) dataType() DataType {
	switch p.effectiveValueType() {
	case "int", "int32":
		return DataTypeInt32
	case "latin1", "string":
		return DataTypeLatin1
	default:
		return DataTypeFloat32
	}
}

func (p mecomTECParameter) graphValueType() string {
	switch p.effectiveValueType() {
	case "int", "int32":
		return "int"
	case "latin1", "string":
		return "string"
	default:
		return "float"
	}
}

func (p mecomTECParameter) access() string {
	if p.Access != "" {
		return p.Access
	}
	if p.Writable {
		return "write"
	}
	return "read"
}

func (p mecomTECParameter) readoutPriority() string {
	if p.Priority != "" {
		return p.Priority
	}
	return mecomReadoutPriorityBackground
}

func (p mecomTECParameter) effectivePreferredReadout() string {
	if p.PreferredReadout != "" {
		return p.PreferredReadout
	}
	if p.readoutPriority() == mecomReadoutPriorityHigh {
		return mecomRingReadout
	}
	return mecomBackgroundReadout
}

func validateCatalogueTreePath(owner string, path []string) error {
	for i, part := range path {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("%s tree_path part %d is empty", owner, i)
		}
	}
	return nil
}

func validateCatalogueTreePaths(owner string, paths []mecomTECTreePath) error {
	seen := map[string]struct{}{}
	var defaults int
	for i, projection := range paths {
		id := strings.TrimSpace(projection.ID)
		if id == "" {
			return fmt.Errorf("%s tree_paths[%d] missing id", owner, i)
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("%s tree_paths duplicate id %q", owner, id)
		}
		seen[id] = struct{}{}
		if len(projection.Path) == 0 {
			return fmt.Errorf("%s tree_paths[%q] path must not be empty", owner, id)
		}
		if err := validateCatalogueTreePath(fmt.Sprintf("%s tree_paths[%q]", owner, id), projection.Path); err != nil {
			return err
		}
		if projection.Default {
			defaults++
		}
	}
	if defaults > 1 {
		return fmt.Errorf("%s tree_paths must have at most one default projection", owner)
	}
	return nil
}

func (p mecomTECParameter) effectiveTreePaths() []mecomTECTreePath {
	return effectiveCatalogueTreePaths(p.effectiveOperatorTreePath(), p.TreePaths, p.effectiveProtocolTreePath())
}

func (p mecomTECDerivedParameter) effectiveTreePaths() []mecomTECTreePath {
	return effectiveCatalogueTreePaths(p.effectiveOperatorTreePath(), p.TreePaths)
}

func (p mecomTECParameter) effectiveOperatorTreePath() []string {
	if len(p.TreePath) > 0 {
		return p.TreePath
	}
	return compactStrings([]string{p.Group, p.Subgroup, p.Display})
}

func (p mecomTECParameter) effectiveProtocolTreePath() mecomTECTreePath {
	name := p.RawName
	if name == "" {
		name = p.Suffix
	}
	return mecomTECTreePath{
		ID:    "protocol",
		Label: "MeCom protocol",
		Path:  compactStrings([]string{"MeCom protocol", fmt.Sprintf("Parameter %d", p.ID), name}),
	}
}

func (p mecomTECDerivedParameter) effectiveOperatorTreePath() []string {
	if len(p.TreePath) > 0 {
		return p.TreePath
	}
	return compactStrings([]string{p.Group, p.Subgroup, p.Display})
}

func effectiveCatalogueTreePaths(single []string, projections []mecomTECTreePath, generated ...mecomTECTreePath) []mecomTECTreePath {
	out := make([]mecomTECTreePath, 0, len(projections)+len(generated)+1)
	seen := map[string]struct{}{}
	for _, projection := range projections {
		if normalized, ok := normalizeCatalogueTreeProjection(projection); ok {
			out = append(out, normalized)
			seen[normalized.ID] = struct{}{}
		}
	}
	if len(out) == 0 && len(single) > 0 {
		if normalized, ok := normalizeCatalogueTreeProjection(mecomTECTreePath{
			ID:      "operator",
			Label:   "Operator",
			Path:    compactStrings(single),
			Default: true,
		}); ok {
			out = append(out, normalized)
			seen[normalized.ID] = struct{}{}
		}
		for _, projection := range generated {
			if normalized, ok := normalizeCatalogueTreeProjection(projection); ok {
				if _, exists := seen[normalized.ID]; exists {
					continue
				}
				out = append(out, normalized)
				seen[normalized.ID] = struct{}{}
			}
		}
	}
	return out
}

func normalizeCatalogueTreeProjection(projection mecomTECTreePath) (mecomTECTreePath, bool) {
	projection.ID = strings.TrimSpace(projection.ID)
	projection.Label = strings.TrimSpace(projection.Label)
	projection.Path = compactStrings(projection.Path)
	if projection.ID == "" || len(projection.Path) == 0 {
		return mecomTECTreePath{}, false
	}
	if projection.Label == "" {
		projection.Label = projection.ID
	}
	return projection, true
}

func addCatalogueTreePathMetadata(metadata map[string]string, paths []mecomTECTreePath) {
	if metadata == nil {
		return
	}
	if len(paths) == 0 {
		return
	}
	if raw, err := json.Marshal(paths); err == nil {
		metadata["ui_tree_paths"] = string(raw)
	}
	projection := paths[0]
	for _, candidate := range paths {
		if candidate.Default {
			projection = candidate
			break
		}
	}
	if projection.ID != "" {
		metadata["ui_tree_projection"] = projection.ID
	}
	if text := catalogueTreePath(projection.Path); text != "" {
		metadata["ui_tree_path"] = text
	}
}

func catalogueTreePath(path []string) string {
	parts := make([]string, 0, len(path))
	for _, part := range path {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " / ")
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

func cloneCounterparts(in map[string][]int) map[string][]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]int, len(in))
	for role, ids := range in {
		out[role] = append([]int(nil), ids...)
	}
	return out
}
