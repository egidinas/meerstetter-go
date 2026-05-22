package mecom

import (
	"encoding/json"
	"fmt"
	"sort"
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
	MeComDefinitionSystem       = "mecom"
	MeerstetterDefinitionFamily = "meerstetter"
	MeerstetterSubFamilyTEC     = "tec"
	MeerstetterSubFamilyLDD     = "ldd"
	MeerstetterSubFamilyDAQ     = "daq"
	MeerstetterVariantLDD130x   = "ldd_130x"
	MeerstetterVariantLDD1321   = "ldd_1321"

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

type MeComCatalogueConfig struct {
	Definition        MeComCatalogueDefinition
	SourceID          string
	DisplayName       string
	ChannelCount      int
	SourceSubject     string
	ControllerAddress int
	FixtureProvenance string
}

type MeComCatalogueDefinition struct {
	DefinitionID    string
	System          string
	Family          string
	SubFamily       string
	Variant         string
	Version         string
	SourceFamily    graphsem.SourceFamily
	DefaultSourceID string
	DisplayName     string
	TracePrefix     string

	readoutParameters []mecomTECParameter
	writeParameters   []mecomTECParameter
	derivedParameters []mecomTECDerivedParameter
}

func DefaultTECCatalogueDefinition() MeComCatalogueDefinition {
	return MeComCatalogueDefinition{
		DefinitionID:      "meerstetter.tec.v631",
		System:            MeComDefinitionSystem,
		Family:            MeerstetterDefinitionFamily,
		SubFamily:         MeerstetterSubFamilyTEC,
		Variant:           MeerstetterSubFamilyTEC,
		Version:           "v631",
		SourceFamily:      graphsem.SourceFamily(DefaultMeComTECCatalogueSourceID),
		DefaultSourceID:   DefaultMeComTECCatalogueSourceID,
		DisplayName:       "MeCom TEC controller bank",
		TracePrefix:       "mecom.tec",
		readoutParameters: mecomTECParameters(),
		writeParameters:   mecomTECWriteParameters(),
		derivedParameters: mecomTECDerivedParameters(),
	}
}

func DefaultLDDCatalogueDefinition() MeComCatalogueDefinition {
	return MeComCatalogueDefinition{
		DefinitionID:    "meerstetter.ldd.v1",
		System:          MeComDefinitionSystem,
		Family:          MeerstetterDefinitionFamily,
		SubFamily:       MeerstetterSubFamilyLDD,
		Variant:         MeerstetterSubFamilyLDD,
		SourceFamily:    graphsem.SourceFamily("mecom_ldd"),
		DefaultSourceID: "mecom_ldd",
		DisplayName:     "MeCom LDD controller bank",
		TracePrefix:     "mecom.ldd",
	}
}

func DefaultLDD130xCatalogueDefinition() MeComCatalogueDefinition {
	definition := DefaultLDDCatalogueDefinition()
	definition.DefinitionID = "meerstetter.ldd_130x.v221"
	definition.Variant = MeerstetterVariantLDD130x
	definition.Version = "v221"
	definition.SourceFamily = graphsem.SourceFamily("mecom_ldd_130x")
	definition.DefaultSourceID = "mecom_ldd_130x"
	definition.DisplayName = "MeCom LDD-130x controller bank"
	definition.TracePrefix = "mecom.ldd_130x"
	return definition
}

func DefaultDAQCatalogueDefinition() MeComCatalogueDefinition {
	return MeComCatalogueDefinition{
		DefinitionID:    "meerstetter.daq.v1",
		System:          MeComDefinitionSystem,
		Family:          MeerstetterDefinitionFamily,
		SubFamily:       MeerstetterSubFamilyDAQ,
		Variant:         MeerstetterSubFamilyDAQ,
		SourceFamily:    graphsem.SourceFamily("mecom_daq"),
		DefaultSourceID: "mecom_daq",
		DisplayName:     "MeCom DAQ controller bank",
		TracePrefix:     "mecom.daq",
	}
}

func ResolveMeComCatalogueDefinition(system, family, subFamily string) (MeComCatalogueDefinition, bool) {
	system = normalizeDefinitionToken(system)
	family = normalizeDefinitionToken(family)
	subFamily = normalizeDefinitionToken(subFamily)
	if system == "" {
		system = MeComDefinitionSystem
	}
	if family == "" {
		family = MeerstetterDefinitionFamily
	}
	if subFamily == "" {
		subFamily = MeerstetterSubFamilyTEC
	}
	if system != MeComDefinitionSystem || family != MeerstetterDefinitionFamily {
		return MeComCatalogueDefinition{}, false
	}
	switch subFamily {
	case MeerstetterSubFamilyTEC:
		return DefaultTECCatalogueDefinition(), true
	case MeerstetterSubFamilyLDD:
		return DefaultLDDCatalogueDefinition(), true
	case "ldd_130x", "ldd130x":
		return DefaultLDD130xCatalogueDefinition(), true
	case "ldd_1321", "ldd1321":
		definition := DefaultLDDCatalogueDefinition()
		definition.DefinitionID = "meerstetter.ldd_1321.v1"
		definition.Variant = MeerstetterVariantLDD1321
		definition.SourceFamily = graphsem.SourceFamily("mecom_ldd_1321")
		definition.DefaultSourceID = "mecom_ldd_1321"
		definition.DisplayName = "MeCom LDD-1321 controller bank"
		definition.TracePrefix = "mecom.ldd_1321"
		return definition, true
	case MeerstetterSubFamilyDAQ:
		return DefaultDAQCatalogueDefinition(), true
	default:
		return MeComCatalogueDefinition{}, false
	}
}

func normalizeDefinitionToken(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (definition MeComCatalogueDefinition) normalized() MeComCatalogueDefinition {
	if definition.DefinitionID == "" && definition.System == "" && definition.Family == "" && definition.SubFamily == "" && definition.TracePrefix == "" && definition.SourceFamily == "" {
		return DefaultTECCatalogueDefinition()
	}
	if definition.System == "" {
		definition.System = MeComDefinitionSystem
	}
	if definition.Family == "" {
		definition.Family = MeerstetterDefinitionFamily
	}
	if definition.SubFamily == "" {
		definition.SubFamily = MeerstetterSubFamilyTEC
	}
	if definition.DefinitionID == "" {
		definition.DefinitionID = strings.Join(compactStrings([]string{definition.Family, definition.SubFamily, definition.Variant, definition.Version}), ".")
	}
	if definition.SourceFamily == "" {
		definition.SourceFamily = graphsem.SourceFamily(strings.Join(compactStrings([]string{definition.System, definition.SubFamily}), "_"))
	}
	if definition.DefaultSourceID == "" {
		definition.DefaultSourceID = string(definition.SourceFamily)
	}
	if definition.DisplayName == "" {
		definition.DisplayName = strings.Join(compactStrings([]string{definition.Family, definition.SubFamily, definition.Variant}), " ")
	}
	if definition.TracePrefix == "" {
		definition.TracePrefix = strings.Join(compactStrings([]string{definition.System, definition.SubFamily}), ".")
	}
	return definition
}

func (definition MeComCatalogueDefinition) TraceID(instance int, suffix string) string {
	definition = definition.normalized()
	prefix := strings.TrimRight(definition.TracePrefix, ".")
	if prefix == "" {
		prefix = "mecom.tec"
	}
	return fmt.Sprintf("%s_%02d.%s", prefix, instance, suffix)
}

func (definition MeComCatalogueDefinition) Metadata() map[string]string {
	definition = definition.normalized()
	metadata := map[string]string{
		"definition_ref":        definition.DefinitionID,
		"definition_system":     definition.System,
		"definition_family":     definition.Family,
		"definition_sub_family": definition.SubFamily,
	}
	if definition.Variant != "" {
		metadata["definition_variant"] = definition.Variant
	}
	if definition.Version != "" {
		metadata["definition_version"] = definition.Version
	}
	return metadata
}

func addMeComDefinitionMetadata(metadata map[string]string, definition MeComCatalogueDefinition) {
	for key, value := range definition.Metadata() {
		if value != "" {
			metadata[key] = value
		}
	}
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
	SourceStatus        string                 `json:"source_status,omitempty"`
	SourceEvidence      []string               `json:"source_evidence,omitempty"`
	ReadoutPriority     string                 `json:"readout_priority,omitempty"`
	PreferredReadout    string                 `json:"preferred_readout,omitempty"`
	Command             string                 `json:"command,omitempty"`
	ApplicableModes     []string               `json:"applicableModes,omitempty"`
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

func DefaultMeComCatalogueEntries(definition MeComCatalogueDefinition, channels int) []TECCatalogueEntry {
	definition = definition.normalized()
	switch definition.SubFamily {
	case MeerstetterSubFamilyLDD:
		if definition.Variant == MeerstetterVariantLDD130x || strings.Contains(definition.DefinitionID, "ldd_130x") {
			return DefaultLDD130xCatalogueEntries(channels)
		}
		return nil
	case MeerstetterSubFamilyDAQ:
		return nil
	default:
		return DefaultTECCatalogueEntries(channels)
	}
}

func DefaultLDD130xCatalogueEntries(channels int) []TECCatalogueEntry {
	if channels <= 0 {
		channels = 1
	}
	if channels > 1 {
		channels = 1
	}
	keys := ldd130xCatalogueKeys()
	used := map[int]struct{}{}
	synthetic := 930000
	out := make([]TECCatalogueEntry, 0, len(keys)+1)
	for _, key := range keys {
		ctx := defaultLDD130xUIMetadata.ParameterContexts[key]
		defaultParam, hasDefault := defaultLDD130xConfig.Parameters[key]
		if ctx.Key == "" {
			ctx = ldd130xContextFromDefault(key, defaultParam)
		}
		var defaultParamPtr *ldd130xDefaultParam
		if hasDefault {
			defaultParamPtr = &defaultParam
		}
		id := ldd130xCatalogueID(ctx, used, &synthetic)
		out = append(out, ldd130xCatalogueEntryFromContext(1, id, key, ctx, defaultParamPtr))
	}
	if feature := ldd130xCrossCheckByID("feature_unlock_license_metadata"); feature.ID != "" {
		if _, exists := used[54000]; !exists {
			used[54000] = struct{}{}
			out = append(out, ldd130xFeatureKeyStoreEntry(feature))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		if out[i].Subgroup != out[j].Subgroup {
			return out[i].Subgroup < out[j].Subgroup
		}
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].RawName < out[j].RawName
	})
	return out
}

func ldd130xCatalogueKeys() []string {
	seen := map[string]struct{}{}
	for key := range defaultLDD130xConfig.Parameters {
		seen[key] = struct{}{}
	}
	for key := range defaultLDD130xUIMetadata.ParameterContexts {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ldd130xContextFromDefault(key string, param ldd130xDefaultParam) ldd130xUIContext {
	label := strings.TrimSpace(param.Key)
	if label == "" {
		label = key
	}
	return ldd130xUIContext{
		Key:                     key,
		LabelKey:                param.LabelKey,
		PrimaryDisplayCandidate: titleFromKey(label),
		ContextStack:            compactStrings([]string{"Default Config", param.Group}),
		ProtocolStatus:          "default_config_only",
		SourceEvidence:          append([]string(nil), param.SourceEvidence...),
	}
}

func ldd130xCatalogueID(ctx ldd130xUIContext, used map[int]struct{}, synthetic *int) int {
	for _, raw := range ctx.ProtocolIDs {
		id, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && id > 0 {
			if _, exists := used[id]; !exists {
				used[id] = struct{}{}
				return id
			}
		}
	}
	for _, raw := range ctx.CANopenIndices {
		id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "0x"), "0X"), 16, 64)
		if err == nil && id > 0 {
			candidate := int(id)
			if _, exists := used[candidate]; !exists {
				used[candidate] = struct{}{}
				return candidate
			}
		}
	}
	for {
		id := *synthetic
		(*synthetic)++
		if _, exists := used[id]; !exists {
			used[id] = struct{}{}
			return id
		}
	}
}

func ldd130xCatalogueEntryFromContext(instance, id int, key string, ctx ldd130xUIContext, defaultParam *ldd130xDefaultParam) TECCatalogueEntry {
	definition := DefaultLDD130xCatalogueDefinition()
	label := firstNonEmpty(ctx.PrimaryDisplayCandidate, titleFromKey(key))
	status := firstNonEmpty(ctx.ProtocolStatus, "software_label")
	check := ldd130xCrossCheckByKey(key)
	help := ldd130xHelp(label, ctx, defaultParam, check)
	treePath := append([]string{"Meerstetter", "LDD", "LDD-130x"}, ctx.ContextStack...)
	treePath = compactStrings(append(treePath, label))
	treePaths := []TECCatalogueTreePath{{
		ID:      "ldd-130x-ui",
		Label:   "LDD-130x",
		Path:    treePath,
		Default: true,
	}, {
		ID:    "protocol",
		Label: "LDD protocol",
		Path:  compactStrings([]string{"LDD protocol", firstNonEmpty(firstString(ctx.ProtocolIDs), "software metadata"), label}),
	}}
	metadata := map[string]string{
		"ldd_key":                  key,
		"label_key":                firstNonEmpty(ctx.LabelKey, ldd130xDefaultLabelKey(defaultParam)),
		"protocol_status":          status,
		"source_text_encoding":     defaultLDD130xUIMetadata.StringsOutputEncoding,
		"resource_string_encoding": defaultLDD130xUIMetadata.ResourceStringEncoding,
		"preferred_readout":        "definition_catalogue",
		"readout_priority":         "metadata",
	}
	addMeComDefinitionMetadata(metadata, definition)
	if len(ctx.ProtocolIDs) > 0 {
		metadata["protocol_ids"] = strings.Join(ctx.ProtocolIDs, ",")
	}
	if len(ctx.CANopenIndices) > 0 {
		metadata["canopen_indices"] = strings.Join(ldd130xCANopenHexList(ctx.CANopenIndices), ",")
	}
	if len(ctx.ContextStack) > 0 {
		metadata["context_stack"] = strings.Join(compactStrings(ctx.ContextStack), " / ")
	}
	if defaultParam != nil && defaultParam.DefaultValueText != "" {
		metadata["default_value"] = defaultParam.DefaultValueText
	}
	if check.ID != "" {
		metadata["documentation_cross_check"] = check.ID
		metadata["documentation_cross_check_status"] = check.Status
	}
	if raw, err := json.Marshal(treePaths); err == nil {
		metadata["tree_paths"] = string(raw)
	}
	return TECCatalogueEntry{
		ID:                  id,
		Instance:            instance,
		DisplayName:         label,
		RawName:             key,
		Unit:                ldd130xUnit(label),
		Type:                ldd130xValueType(defaultParam, ctx),
		Role:                ldd130xRole(defaultParam, ctx),
		Group:               "LDD-130x",
		Subgroup:            firstNonEmpty(firstString(ctx.ContextStack), "Definition metadata"),
		Category:            "auxiliary",
		Kind:                ldd130xKind(defaultParam, ctx),
		Access:              "metadata",
		SemanticRole:        strings.ToLower(strings.ReplaceAll(key, "_", ".")),
		SourceParameterName: key,
		Help:                help,
		Visibility:          ldd130xVisibility(defaultParam),
		SafetyNote:          ldd130xSafetyNote(defaultParam, ctx),
		SourceStatus:        status,
		SourceEvidence:      appendLDDEvidence(defaultParam, ctx, check),
		ReadoutPriority:     "metadata",
		PreferredReadout:    "definition_catalogue",
		DefaultValue:        ldd130xDefaultValue(defaultParam),
		ApplicableModes:     ldd130xApplicableModes(ctx),
		TransportSupport:    []string{"mecom_serial", "mecom_can", "canopen"},
		TreePath:            treePath,
		TreePaths:           treePaths,
		Metadata:            metadata,
	}
}

func ldd130xFeatureKeyStoreEntry(check ldd130xCrossCheck) TECCatalogueEntry {
	definition := DefaultLDD130xCatalogueDefinition()
	metadata := map[string]string{
		"protocol_status":                  "protocol_documented",
		"documentation_cross_check":        check.ID,
		"documentation_cross_check_status": check.Status,
		"source_text_encoding":             defaultLDD130xUIMetadata.StringsOutputEncoding,
		"resource_string_encoding":         defaultLDD130xUIMetadata.ResourceStringEncoding,
		"preferred_readout":                "definition_catalogue",
		"readout_priority":                 "metadata",
	}
	addMeComDefinitionMetadata(metadata, definition)
	return TECCatalogueEntry{
		ID:                  54000,
		Instance:            1,
		DisplayName:         firstNonEmpty(check.ProtocolName, "Feature Key Store"),
		RawName:             "FEATURE_KEY_STORE",
		Type:                "string",
		Role:                "metadata",
		Group:               "LDD-130x",
		Subgroup:            "Feature Licenses",
		Category:            "auxiliary",
		Kind:                "metadata",
		Access:              "metadata",
		SemanticRole:        "feature.key.store",
		SourceParameterName: "FEATURE_KEY_STORE",
		Help:                check.Summary,
		Visibility:          "advanced",
		SourceStatus:        "protocol_documented",
		SourceEvidence:      append([]string(nil), check.SourceEvidence...),
		ReadoutPriority:     "metadata",
		PreferredReadout:    "definition_catalogue",
		ApplicableModes:     []string{"ldd"},
		TransportSupport:    []string{"mecom_serial", "mecom_can", "canopen"},
		TreePath:            []string{"Meerstetter", "LDD", "LDD-130x", "Feature Licenses", "Feature Key Store"},
		TreePaths: []TECCatalogueTreePath{{
			ID:      "ldd-130x-ui",
			Label:   "LDD-130x",
			Path:    []string{"Meerstetter", "LDD", "LDD-130x", "Feature Licenses", "Feature Key Store"},
			Default: true,
		}},
		Metadata: metadata,
	}
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ldd130xDefaultLabelKey(param *ldd130xDefaultParam) string {
	if param == nil {
		return ""
	}
	return param.LabelKey
}

func ldd130xUnit(label string) string {
	start := strings.LastIndex(label, "[")
	end := strings.LastIndex(label, "]")
	if start < 0 || end <= start {
		return ""
	}
	unit := strings.TrimSpace(label[start+1 : end])
	switch unit {
	case "\u00b0C", "degC":
		return "degC"
	default:
		return unit
	}
}

func titleFromKey(value string) string {
	words := strings.Fields(strings.ToLower(strings.ReplaceAll(value, "_", " ")))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func ldd130xValueType(param *ldd130xDefaultParam, ctx ldd130xUIContext) string {
	if param != nil {
		switch strings.ToLower(strings.TrimSpace(param.ValueKind)) {
		case "integer", "bool", "boolean":
			return "int32"
		case "number", "float":
			return "float32"
		case "text":
			return "string"
		}
	}
	if ldd130xUnit(ctx.PrimaryDisplayCandidate) != "" {
		return "float32"
	}
	return "string"
}

func ldd130xRole(param *ldd130xDefaultParam, ctx ldd130xUIContext) string {
	if ctx.ProtocolStatus == "service_software_only" {
		return "metadata"
	}
	if param != nil && param.Visibility == "operator" {
		return "control"
	}
	return "monitor"
}

func ldd130xKind(param *ldd130xDefaultParam, ctx ldd130xUIContext) string {
	if ctx.ProtocolStatus == "service_software_only" || ldd130xValueType(param, ctx) == "string" {
		return "metadata"
	}
	return "continuous"
}

func ldd130xVisibility(param *ldd130xDefaultParam) string {
	if param != nil && strings.TrimSpace(param.Visibility) != "" {
		return param.Visibility
	}
	return "advanced"
}

func ldd130xSafetyNote(param *ldd130xDefaultParam, ctx ldd130xUIContext) string {
	if param != nil && strings.TrimSpace(param.SafetyNote) != "" {
		return param.SafetyNote
	}
	if ctx.ProtocolStatus == "service_software_only" {
		return defaultLDD130xMetadataIndex.SafetyPolicy
	}
	return ""
}

func ldd130xDefaultValue(param *ldd130xDefaultParam) *string {
	if param == nil || strings.TrimSpace(param.DefaultValueText) == "" {
		return nil
	}
	value := param.DefaultValueText
	return &value
}

func ldd130xApplicableModes(ctx ldd130xUIContext) []string {
	if ctx.ProtocolStatus == "service_software_only" {
		return []string{"metadata"}
	}
	return []string{"ldd"}
}

func appendLDDEvidence(param *ldd130xDefaultParam, ctx ldd130xUIContext, check ldd130xCrossCheck) []string {
	var out []string
	if param != nil {
		out = append(out, param.SourceEvidence...)
	}
	out = append(out, ctx.SourceEvidence...)
	out = append(out, check.SourceEvidence...)
	return compactStrings(out)
}

func ldd130xHelp(label string, ctx ldd130xUIContext, param *ldd130xDefaultParam, check ldd130xCrossCheck) string {
	var parts []string
	if label != "" {
		parts = append(parts, label)
	}
	if len(ctx.ContextStack) > 0 {
		parts = append(parts, "Context: "+strings.Join(compactStrings(ctx.ContextStack), " / "))
	}
	if ctx.ProtocolStatus != "" {
		parts = append(parts, "Protocol status: "+ctx.ProtocolStatus)
	}
	var ids []string
	if len(ctx.ProtocolIDs) > 0 {
		ids = append(ids, "MeCom "+strings.Join(ctx.ProtocolIDs, ", "))
	}
	if len(ctx.CANopenIndices) > 0 {
		ids = append(ids, "CANopen "+strings.Join(ldd130xCANopenHexList(ctx.CANopenIndices), ", "))
	}
	if len(ids) > 0 {
		parts = append(parts, strings.Join(ids, "; "))
	}
	if check.Summary != "" {
		parts = append(parts, check.Summary)
	}
	if note := ldd130xSafetyNote(param, ctx); note != "" {
		parts = append(parts, note)
	}
	return strings.Join(parts, " ")
}

func ldd130xCrossCheckByKey(key string) ldd130xCrossCheck {
	for _, check := range defaultLDD130xMetadataIndex.DocumentationCrossChecks.Checks {
		if check.DefaultKey == key || check.UIKey == key {
			return check
		}
	}
	return ldd130xCrossCheck{}
}

func ldd130xCrossCheckByID(id string) ldd130xCrossCheck {
	for _, check := range defaultLDD130xMetadataIndex.DocumentationCrossChecks.Checks {
		if check.ID == id {
			return check
		}
	}
	return ldd130xCrossCheck{}
}

func ldd130xCANopenHexList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(strings.ToUpper(value))
		if value == "" {
			continue
		}
		value = strings.TrimPrefix(value, "0X")
		out = append(out, "0x"+value)
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
	addMeComDefinitionMetadata(metadata, DefaultTECCatalogueDefinition())
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
		SourceStatus:        "definition_catalogue",
		SourceEvidence:      append([]string(nil), help.SourceEvidence...),
		ReadoutPriority:     param.readoutPriority(),
		PreferredReadout:    param.effectivePreferredReadout(),
		DefaultValue:        cloneStringPointer(param.DefaultValue),
		Command:             param.Command,
		ApplicableModes:     append([]string(nil), param.ApplicableModes...),
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

//go:embed catalogues/sources/ldd_130x_default_config_5261h.v221.json
var defaultLDD130xConfigJSON []byte

var defaultLDD130xConfig = mustLoadLDD130xConfigJSON(defaultLDD130xConfigJSON)

//go:embed catalogues/sources/ldd_130x_ui_metadata.v221.json
var defaultLDD130xUIMetadataJSON []byte

var defaultLDD130xUIMetadata = mustLoadLDD130xUIMetadataJSON(defaultLDD130xUIMetadataJSON)

//go:embed catalogues/sources/ldd_130x_metadata_index.v221.json
var defaultLDD130xMetadataIndexJSON []byte

var defaultLDD130xMetadataIndex = mustLoadLDD130xMetadataIndexJSON(defaultLDD130xMetadataIndexJSON)

type ldd130xDefinitionSource struct {
	DefinitionRef string `json:"definition_ref"`
	System        string `json:"system"`
	Family        string `json:"family"`
	SubFamily     string `json:"sub_family"`
	Variant       string `json:"variant"`
	Version       string `json:"version"`
}

type ldd130xDefaultConfigSource struct {
	SchemaVersion string                         `json:"schema_version"`
	Definition    ldd130xDefinitionSource        `json:"definition"`
	Source        any                            `json:"source"`
	Parameters    map[string]ldd130xDefaultParam `json:"parameters"`
}

type ldd130xDefaultParam struct {
	Key              string   `json:"key"`
	LabelKey         string   `json:"label_key"`
	Group            string   `json:"group"`
	Visibility       string   `json:"visibility"`
	DefaultValue     any      `json:"default_value"`
	DefaultValueText string   `json:"default_value_text"`
	ValueKind        string   `json:"value_kind"`
	SafetyNote       string   `json:"safety_note"`
	SourceEvidence   []string `json:"source_evidence"`
}

type ldd130xUIMetadataSource struct {
	SchemaVersion          string                      `json:"schema_version"`
	Definition             ldd130xDefinitionSource     `json:"definition"`
	ResourceStringEncoding string                      `json:"resource_string_encoding"`
	StringsOutputEncoding  string                      `json:"strings_output_encoding"`
	Source                 any                         `json:"source"`
	ParameterContexts      map[string]ldd130xUIContext `json:"parameter_contexts"`
}

type ldd130xUIContext struct {
	Key                     string   `json:"key"`
	LabelKey                string   `json:"label_key"`
	PrimaryDisplayCandidate string   `json:"primary_display_candidate"`
	DisplayCandidates       []string `json:"display_candidates"`
	ContextStack            []string `json:"context_stack"`
	NeighborControls        []string `json:"neighbor_controls"`
	SourceEvidence          []string `json:"source_evidence"`
	ProtocolStatus          string   `json:"protocol_status"`
	ProtocolIDs             []string `json:"protocol_ids"`
	CANopenIndices          []string `json:"canopen_indices"`
}

type ldd130xMetadataIndexSource struct {
	SchemaVersion            string                    `json:"schema_version"`
	Definition               ldd130xDefinitionSource   `json:"definition"`
	SafetyPolicy             string                    `json:"safety_policy"`
	DocumentationCrossChecks ldd130xDocumentationIndex `json:"documentation_cross_checks"`
}

type ldd130xDocumentationIndex struct {
	ProtocolDocument ldd130xDocument     `json:"protocol_document"`
	ManualDocument   ldd130xDocument     `json:"manual_document"`
	Checks           []ldd130xCrossCheck `json:"checks"`
}

type ldd130xDocument struct {
	Source       string `json:"source"`
	Document     string `json:"document"`
	ReleaseDate  string `json:"release_date"`
	TextEncoding string `json:"text_encoding"`
}

type ldd130xCrossCheck struct {
	ID             string   `json:"id"`
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	SourceEvidence []string `json:"source_evidence"`
	DefaultKey     string   `json:"default_key"`
	UIKey          string   `json:"ui_key"`
	MeComID        string   `json:"mecom_id"`
	CANopenIndex   string   `json:"canopen_index"`
	UIPath         string   `json:"ui_path"`
	ProtocolName   string   `json:"protocol_name"`
}

func mustLoadTECCatalogueJSON(raw []byte) tecCatalogueJSON {
	catalogue, err := loadTECCatalogueJSON(raw)
	if err != nil {
		panic(err)
	}
	return catalogue
}

func mustLoadLDD130xConfigJSON(raw []byte) ldd130xDefaultConfigSource {
	source, err := loadLDD130xConfigJSON(raw)
	if err != nil {
		panic(err)
	}
	return source
}

func loadLDD130xConfigJSON(raw []byte) (ldd130xDefaultConfigSource, error) {
	var source ldd130xDefaultConfigSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return ldd130xDefaultConfigSource{}, err
	}
	if source.SchemaVersion == "" {
		return ldd130xDefaultConfigSource{}, fmt.Errorf("missing LDD default config schema_version")
	}
	if source.Definition.DefinitionRef == "" {
		return ldd130xDefaultConfigSource{}, fmt.Errorf("missing LDD default config definition_ref")
	}
	if len(source.Parameters) == 0 {
		return ldd130xDefaultConfigSource{}, fmt.Errorf("LDD default config parameters must not be empty")
	}
	return source, nil
}

func mustLoadLDD130xUIMetadataJSON(raw []byte) ldd130xUIMetadataSource {
	source, err := loadLDD130xUIMetadataJSON(raw)
	if err != nil {
		panic(err)
	}
	return source
}

func loadLDD130xUIMetadataJSON(raw []byte) (ldd130xUIMetadataSource, error) {
	var source ldd130xUIMetadataSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return ldd130xUIMetadataSource{}, err
	}
	if source.SchemaVersion == "" {
		return ldd130xUIMetadataSource{}, fmt.Errorf("missing LDD UI metadata schema_version")
	}
	if source.Definition.DefinitionRef == "" {
		return ldd130xUIMetadataSource{}, fmt.Errorf("missing LDD UI metadata definition_ref")
	}
	if len(source.ParameterContexts) == 0 {
		return ldd130xUIMetadataSource{}, fmt.Errorf("LDD UI parameter_contexts must not be empty")
	}
	return source, nil
}

func mustLoadLDD130xMetadataIndexJSON(raw []byte) ldd130xMetadataIndexSource {
	source, err := loadLDD130xMetadataIndexJSON(raw)
	if err != nil {
		panic(err)
	}
	return source
}

func loadLDD130xMetadataIndexJSON(raw []byte) (ldd130xMetadataIndexSource, error) {
	var source ldd130xMetadataIndexSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return ldd130xMetadataIndexSource{}, err
	}
	if source.SchemaVersion == "" {
		return ldd130xMetadataIndexSource{}, fmt.Errorf("missing LDD metadata index schema_version")
	}
	if source.Definition.DefinitionRef == "" {
		return ldd130xMetadataIndexSource{}, fmt.Errorf("missing LDD metadata index definition_ref")
	}
	return source, nil
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
	return DefaultMeComReadoutParameters(DefaultTECCatalogueDefinition(), channels)
}

func DefaultTECTraceID(instance int, suffix string) string {
	return DefaultTECCatalogueDefinition().TraceID(instance, suffix)
}

func DefaultMeComReadoutParameters(definition MeComCatalogueDefinition, channels int) []ReadoutParameter {
	definition = definition.normalized()
	if channels <= 0 {
		channels = 8
	}
	out := make([]ReadoutParameter, 0, channels*len(definition.readoutParameters))
	for ch := 1; ch <= channels; ch++ {
		for _, param := range definition.readoutParameters {
			out = append(out, ReadoutParameter{
				Parameter: Parameter{
					ID:       param.ID,
					Instance: ch,
					Name:     param.Suffix,
					Unit:     param.Unit,
					Type:     param.dataType(),
					Writable: param.Writable,
				},
				Sensor:       definition.TraceID(ch, param.Suffix),
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
	return DefaultMeComSignalNames(DefaultTECCatalogueDefinition(), channels)
}

func DefaultMeComSignalNames(definition MeComCatalogueDefinition, channels int) []string {
	definition = definition.normalized()
	params := DefaultMeComReadoutParameters(definition, channels)
	out := make([]string, 0, len(params)+channels*len(definition.derivedParameters))
	for _, param := range params {
		out = append(out, param.Sensor)
	}
	for ch := 1; ch <= channels; ch++ {
		for _, param := range definition.derivedParameters {
			out = append(out, definition.TraceID(ch, param.Suffix))
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
	return BuildMeComCatalogue(MeComCatalogueConfig{
		Definition:        DefaultTECCatalogueDefinition(),
		SourceID:          cfg.SourceID,
		DisplayName:       cfg.DisplayName,
		ChannelCount:      cfg.ChannelCount,
		SourceSubject:     cfg.SourceSubject,
		ControllerAddress: cfg.ControllerAddress,
		FixtureProvenance: cfg.FixtureProvenance,
	})
}

func BuildMeComCatalogue(cfg MeComCatalogueConfig) graphsem.SourceCatalogue {
	definition := cfg.Definition.normalized()
	if cfg.SourceID == "" {
		cfg.SourceID = definition.DefaultSourceID
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = definition.DisplayName
	}
	if cfg.ChannelCount <= 0 {
		cfg.ChannelCount = 8
	}
	entries := make([]graphsem.SourceCatalogueRow, 0, cfg.ChannelCount*(len(definition.readoutParameters)+len(definition.derivedParameters)))
	for ch := 1; ch <= cfg.ChannelCount; ch++ {
		for _, param := range definition.readoutParameters {
			entries = append(entries, mecomTECRow(cfg, definition, ch, param))
		}
		for _, param := range definition.derivedParameters {
			entries = append(entries, mecomTECDerivedRow(cfg, definition, ch, param))
		}
	}
	return graphsem.SourceCatalogue{
		SchemaVersion: graphsem.CurrentSourceCatalogueSchemaVersion,
		SourceID:      cfg.SourceID,
		SourceFamily:  definition.SourceFamily,
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
			PoliteAccessStatement: "MeCom catalogue rows prefer CRTVStream ring-buffer readout for high-priority values, reduced by mean/stddev windows to the consumer-requested rate; background values use ?VX round-robin chunks and raw single reads remain compatibility fallback.",
		},
	}
}

func mecomTECDerivedRow(cfg MeComCatalogueConfig, definition MeComCatalogueDefinition, ch int, param mecomTECDerivedParameter) graphsem.SourceCatalogueRow {
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
	addMeComDefinitionMetadata(metadata, definition)
	if cfg.ControllerAddress > 0 {
		metadata["controller_address"] = strconv.Itoa(cfg.ControllerAddress)
	}
	if cfg.FixtureProvenance != "" {
		metadata["fixture_provenance"] = cfg.FixtureProvenance
		metadata["dut_id"] = ohID
	}
	addCatalogueTreePathMetadata(metadata, param.effectiveTreePaths())
	return graphsem.SourceCatalogueRow{
		TraceID:        definition.TraceID(ch, param.Suffix),
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

func mecomTECRow(cfg MeComCatalogueConfig, definition MeComCatalogueDefinition, ch int, param mecomTECParameter) graphsem.SourceCatalogueRow {
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
	addMeComDefinitionMetadata(metadata, definition)
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
		TraceID:        definition.TraceID(ch, param.Suffix),
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
