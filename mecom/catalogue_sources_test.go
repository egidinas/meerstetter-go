package mecom

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type tecDefaultConfigSource struct {
	SchemaVersion string `json:"schema_version"`
	Parameters    map[string]struct {
		MeParID       int `json:"mepar_id"`
		InstanceCount int `json:"instance_count"`
		Instances     map[string]struct {
			MeParInst        int      `json:"mepar_inst"`
			DefaultValue     any      `json:"default_value"`
			DefaultValueText string   `json:"default_value_text"`
			ValueKind        string   `json:"value_kind"`
			Path             string   `json:"path"`
			Tree             []string `json:"tree"`
		} `json:"instances"`
	} `json:"parameters"`
}

type tecCANopenEDSSource struct {
	SchemaVersion string            `json:"schema_version"`
	FileInfo      map[string]string `json:"file_info"`
	DeviceInfo    map[string]string `json:"device_info"`
	Objects       map[string]struct {
		Index      string `json:"index"`
		Name       string `json:"parameter_name"`
		AccessType string `json:"access_type"`
		DataType   string `json:"data_type"`
		Subobjects map[string]struct {
			Name       string `json:"parameter_name"`
			AccessType string `json:"access_type"`
			DataType   string `json:"data_type"`
		} `json:"subobjects"`
	} `json:"objects"`
}

type tecCANopenSDOMapSource struct {
	SchemaVersion string `json:"schema_version"`
	Mappings      []struct {
		MeComID   int    `json:"mecom_id"`
		Name      string `json:"name"`
		ValueType string `json:"value_type"`
		Access    string `json:"access"`
		Instances struct {
			Mode  string `json:"mode"`
			Min   int    `json:"min"`
			Max   int    `json:"max"`
			Fixed int    `json:"fixed"`
		} `json:"instances"`
		CANopen struct {
			Index        string `json:"index"`
			Subindex     string `json:"subindex"`
			SubindexMode string `json:"subindex_mode"`
			DataType     string `json:"data_type"`
		} `json:"canopen"`
		Aliases []struct {
			Space string `json:"space"`
			ID    any    `json:"id"`
		} `json:"aliases"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"mappings"`
	ProtocolInventory []struct {
		Command        string   `json:"command"`
		Status         string   `json:"status"`
		BridgeBehavior string   `json:"bridge_behavior"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"protocol_inventory"`
	BridgeTransforms []struct {
		MeComID   int    `json:"mecom_id"`
		Name      string `json:"name"`
		ValueType string `json:"value_type"`
		Trigger   string `json:"trigger"`
		Runtime   struct {
			Kind          string  `json:"kind"`
			SourceMeComID int     `json:"source_mecom_id"`
			Scale         float64 `json:"scale"`
			Int32Mask     string  `json:"int32_mask"`
			Int32Value    int32   `json:"int32_value"`
		} `json:"runtime"`
		BridgeBehavior string   `json:"bridge_behavior"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"bridge_transforms"`
	Unsupported []struct {
		ID             any      `json:"id"`
		Reason         string   `json:"reason"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"unsupported"`
}

type tecHelpSource struct {
	SchemaVersion string `json:"schema_version"`
	Parameters    map[string]struct {
		MeParID        int      `json:"mepar_id"`
		Name           string   `json:"name"`
		Group          string   `json:"group"`
		Visibility     string   `json:"visibility"`
		Help           string   `json:"help"`
		SourceEvidence []string `json:"source_evidence"`
		Access         string   `json:"access"`
		SafetyNote     string   `json:"safety_note"`
	} `json:"parameters"`
}

type tecMetadataIndexSource struct {
	SchemaVersion string `json:"schema_version"`
	Sources       []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Method string `json:"method"`
		Status string `json:"status"`
	} `json:"sources"`
	HiddenCandidates []struct {
		MeParID    int    `json:"mepar_id"`
		Group      string `json:"group"`
		Visibility string `json:"visibility"`
	} `json:"hidden_candidates"`
}

type lddDefaultConfigSource struct {
	SchemaVersion string            `json:"schema_version"`
	Definition    map[string]string `json:"definition"`
	Parameters    map[string]struct {
		Key              string   `json:"key"`
		LabelKey         string   `json:"label_key"`
		Group            string   `json:"group"`
		Visibility       string   `json:"visibility"`
		DefaultValue     any      `json:"default_value"`
		DefaultValueText string   `json:"default_value_text"`
		ValueKind        string   `json:"value_kind"`
		SafetyNote       string   `json:"safety_note"`
		SourceEvidence   []string `json:"source_evidence"`
	} `json:"parameters"`
}

type lddCANopenEDSSource struct {
	SchemaVersion string            `json:"schema_version"`
	Definition    map[string]string `json:"definition"`
	FileInfo      map[string]string `json:"file_info"`
	DeviceInfo    map[string]string `json:"device_info"`
	Objects       map[string]struct {
		Index      string `json:"index"`
		Name       string `json:"parameter_name"`
		AccessType string `json:"access_type"`
		DataType   string `json:"data_type"`
		Subobjects map[string]struct {
			Name       string `json:"parameter_name"`
			AccessType string `json:"access_type"`
			DataType   string `json:"data_type"`
		} `json:"subobjects"`
	} `json:"objects"`
}

type lddMetadataSourceRecord struct {
	ID                    string `json:"id"`
	Path                  string `json:"path"`
	Method                string `json:"method"`
	Status                string `json:"status"`
	TextEncoding          string `json:"text_encoding"`
	StringsOutputEncoding string `json:"strings_output_encoding"`
}

type lddMetadataIndexSource struct {
	SchemaVersion  string                    `json:"schema_version"`
	Definition     map[string]string         `json:"definition"`
	Sources        []lddMetadataSourceRecord `json:"sources"`
	SoftwareLabels []string                  `json:"software_labels"`
	ReleaseNotes   struct {
		CurrentVersion                string `json:"current_version"`
		CurrentReleaseDate            string `json:"current_release_date"`
		CurrentServiceSoftwareVersion string `json:"current_service_software_version"`
		CurrentFirmwareVersion        string `json:"current_firmware_version"`
		SupportedDevices              []struct {
			Device          string `json:"device"`
			HardwareVersion string `json:"hardware_version"`
		} `json:"supported_devices"`
		Versions []struct {
			Version        string   `json:"version"`
			NewFeatures    []string `json:"new_features"`
			ResolvedIssues []string `json:"resolved_issues"`
			KnownIssues    []string `json:"known_issues"`
		} `json:"versions"`
		RiskNotes []struct {
			ID             string   `json:"id"`
			Version        string   `json:"version"`
			Summary        string   `json:"summary"`
			SourceEvidence []string `json:"source_evidence"`
		} `json:"risk_notes"`
	} `json:"release_notes"`
	DocumentationCrossChecks struct {
		ProtocolDocument struct {
			Source       string `json:"source"`
			Document     string `json:"document"`
			ReleaseDate  string `json:"release_date"`
			TextEncoding string `json:"text_encoding"`
		} `json:"protocol_document"`
		ManualDocument struct {
			Source       string `json:"source"`
			Document     string `json:"document"`
			ReleaseDate  string `json:"release_date"`
			TextEncoding string `json:"text_encoding"`
		} `json:"manual_document"`
		Checks []struct {
			ID             string   `json:"id"`
			Status         string   `json:"status"`
			Summary        string   `json:"summary"`
			SourceEvidence []string `json:"source_evidence"`
			DefaultKey     string   `json:"default_key"`
			MeComID        string   `json:"mecom_id"`
			CANopenIndex   string   `json:"canopen_index"`
			UIPath         string   `json:"ui_path"`
			UIKey          string   `json:"ui_key"`
			ProtocolName   string   `json:"protocol_name"`
		} `json:"checks"`
	} `json:"documentation_cross_checks"`
	HiddenCandidates []struct {
		Key        string `json:"key"`
		LabelKey   string `json:"label_key"`
		Group      string `json:"group"`
		Visibility string `json:"visibility"`
	} `json:"hidden_candidates"`
}

type lddUIMetadataSource struct {
	SchemaVersion          string            `json:"schema_version"`
	Definition             map[string]string `json:"definition"`
	Source                 string            `json:"source"`
	ResourceStringEncoding string            `json:"resource_string_encoding"`
	StringsOutputEncoding  string            `json:"strings_output_encoding"`
	ParameterContexts      map[string]struct {
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
	} `json:"parameter_contexts"`
	ParameterPaths []struct {
		Path           string   `json:"path"`
		Tree           []string `json:"tree"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"parameter_paths"`
	UINotes []struct {
		ID             string   `json:"id"`
		Kind           string   `json:"kind"`
		Text           string   `json:"text"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"ui_notes"`
	UIStrings []struct {
		Text           string   `json:"text"`
		Kind           string   `json:"kind"`
		SourceEvidence []string `json:"source_evidence"`
	} `json:"ui_strings"`
}

func TestTECCatalogueSourceJSONFiles(t *testing.T) {
	var defaults tecDefaultConfigSource
	readCatalogueSourceJSON(t, "catalogues/sources/tec_default_config_5216o.v631.json", &defaults)
	if defaults.SchemaVersion != "mecom_tec_coso_defaults.v1" {
		t.Fatalf("defaults schema = %q", defaults.SchemaVersion)
	}
	if got, want := len(defaults.Parameters), 70; got < want {
		t.Fatalf("default parameter count = %d, want at least %d", got, want)
	}
	requireDefaultValue(t, defaults, "3000", "1", "25", "Target Temperature")
	requireDefaultValue(t, defaults, "2031", "1", "19", "Output Voltage Limit")
	requireDefaultValue(t, defaults, "3033", "2", "68", "Peltier Maximal Temperature Delta")

	var eds tecCANopenEDSSource
	readCatalogueSourceJSON(t, "catalogues/sources/canopen_eds.v631.json", &eds)
	if eds.SchemaVersion != "mecom_tec_canopen_eds.v1" {
		t.Fatalf("EDS schema = %q", eds.SchemaVersion)
	}
	if got, want := len(eds.Objects), 200; got < want {
		t.Fatalf("EDS object count = %d, want at least %d", got, want)
	}
	if got := eds.DeviceInfo["ProductName"]; !strings.Contains(strings.ToLower(got), "tec") {
		t.Fatalf("EDS ProductName = %q, want TEC-related device", got)
	}
	cascadeTarget, ok := eds.Objects["4423"]
	if !ok {
		t.Fatal("EDS missing object 0x4423 for cascade target")
	}
	if !strings.Contains(strings.ToLower(cascadeTarget.Name), "cascade") {
		t.Fatalf("EDS 0x4423 name = %q, want cascade-related", cascadeTarget.Name)
	}

	var sdoMap tecCANopenSDOMapSource
	readCatalogueSourceJSON(t, "catalogues/sources/tec_canopen_sdo_map.v631.json", &sdoMap)
	if sdoMap.SchemaVersion != "mecom_tec_canopen_sdo_map.v1" {
		t.Fatalf("SDO map schema = %q", sdoMap.SchemaVersion)
	}
	requireSDOMapAlias(t, sdoMap, 1044, "0x2144", 8516, "float32", 4)
	requireSDOMapAlias(t, sdoMap, 1045, "0x2145", 8517, "float32", 2)
	requireSDOMapAlias(t, sdoMap, 1012, "0x2112", 8466, "float32", 4)
	requireSDOMapAlias(t, sdoMap, 52002, "0x2F02", 12034, "int32", 4)
	requireSDOMapAlias(t, sdoMap, 52003, "0x2F03", 12035, "int32", 4)
	requireSDOMapAlias(t, sdoMap, 6000, "0x2C00", 11264, "int32", 2)
	requireSDOMapAlias(t, sdoMap, 6132, "0x3132", 12594, "float32", 4)
	requireSDOMapAlias(t, sdoMap, 53102, "0x4402", 17410, "int32", 2)
	requireSDOMapAlias(t, sdoMap, 53184, "0x4484", 17540, "float32", 2)
	requireUnsupportedSDOPath(t, sdoMap, 120, "metadata")
	requireUnsupportedSDOPath(t, sdoMap, 203, "does not expose")
	requireUnsupportedSDOPath(t, sdoMap, 217, "FreeRTOS")
	requireUnsupportedSDOPath(t, sdoMap, 6400, "no matching 0x3400")
	requireUnsupportedSDOPath(t, sdoMap, 51000, "firmware")
	requireUnsupportedSDOPath(t, sdoMap, 52201, "bridge_transform")
	requireUnsupportedSDOPath(t, sdoMap, 65100, "diagnostics")
	requireUnsupportedSDOPath(t, sdoMap, "?RS0000", "ring")
	requireProtocolInventoryCommand(t, sdoMap, "?VM", "metadata")
	requireProtocolInventoryCommand(t, sdoMap, "?VB", "big-data")
	requireProtocolInventoryCommand(t, sdoMap, "?RS0000", "ring")
	requireBridgeTransform(t, sdoMap, 112, "firmware")
	requireBridgeTransform(t, sdoMap, 115, "startup")
	requireBridgeTransform(t, sdoMap, 108, "CoSo")
	requireBridgeTransform(t, sdoMap, 217, "FreeRTOS")
	requireBridgeTransform(t, sdoMap, 52201, "sink-fixed")
	requireBridgeTransform(t, sdoMap, 1200, "stable")

	var help tecHelpSource
	readCatalogueSourceJSON(t, "catalogues/sources/tec_tooltips.v631.json", &help)
	if help.SchemaVersion != "mecom_tec_help.v1" {
		t.Fatalf("help schema = %q", help.SchemaVersion)
	}
	if got, want := len(help.Parameters), 30; got < want {
		t.Fatalf("help parameter count = %d, want at least %d", got, want)
	}
	requireHelp(t, help, "3030", "Output Current Limit")
	requireHelp(t, help, "53123", "cascade")
	requireHelp(t, help, "2051", "address")
	requireSafetyNote(t, help, "202", "current limiting")
	for id, param := range help.Parameters {
		if strings.Contains(param.Help, "\r") || strings.Contains(param.Help, "\n") {
			t.Fatalf("help %s contains raw multiline text", id)
		}
		if len(param.Help) > 320 {
			t.Fatalf("help %s is too long for reusable tooltip metadata: %d chars", id, len(param.Help))
		}
	}

	var index tecMetadataIndexSource
	readCatalogueSourceJSON(t, "catalogues/sources/tec_metadata_index.v631.json", &index)
	if index.SchemaVersion != "mecom_tec_metadata_index.v1" {
		t.Fatalf("metadata index schema = %q", index.SchemaVersion)
	}
	if got, want := len(index.Sources), 4; got < want {
		t.Fatalf("source count = %d, want at least %d", got, want)
	}
	if got, want := len(index.HiddenCandidates), 8; got < want {
		t.Fatalf("hidden candidate count = %d, want at least %d", got, want)
	}
	requireHiddenCandidate(t, index, 40000, "advanced")
	requireHiddenCandidate(t, index, 53010, "license")
	requireHiddenCandidate(t, index, 6320, "advanced")
}

func TestLDD130xCatalogueSourceJSONFiles(t *testing.T) {
	var defaults lddDefaultConfigSource
	readCatalogueSourceJSON(t, "catalogues/sources/ldd_130x_default_config_5261h.v221.json", &defaults)
	if defaults.SchemaVersion != "mecom_ldd_130x_defaults.v1" {
		t.Fatalf("defaults schema = %q", defaults.SchemaVersion)
	}
	requireDefinitionRef(t, defaults.Definition, "meerstetter.ldd_130x.v221", MeerstetterSubFamilyLDD, MeerstetterVariantLDD130x)
	if got, want := len(defaults.Parameters), 180; got < want {
		t.Fatalf("default parameter count = %d, want at least %d", got, want)
	}
	requireLDDDefaultValue(t, defaults, "LDD_OUTPUT_EN", "OFF", "ldd", "operator")
	requireLDDDefaultValue(t, defaults, "CANOPEN_NODEID", "127", "canopen", "advanced")
	requireLDDDefaultValue(t, defaults, "LPC_NOM_POWER_SOURCE", "Set Power", "lpc", "operator")
	requireLDDDefaultValue(t, defaults, "IVCURVE_ENABLE", "Disabled", "ivcurve", "advanced")

	var eds lddCANopenEDSSource
	readCatalogueSourceJSON(t, "catalogues/sources/ldd_130x_canopen_eds.v221.json", &eds)
	if eds.SchemaVersion != "mecom_ldd_130x_canopen_eds.v1" {
		t.Fatalf("EDS schema = %q", eds.SchemaVersion)
	}
	requireDefinitionRef(t, eds.Definition, "meerstetter.ldd_130x.v221", MeerstetterSubFamilyLDD, MeerstetterVariantLDD130x)
	if got, want := len(eds.Objects), 240; got < want {
		t.Fatalf("EDS object count = %d, want at least %d", got, want)
	}
	if got := eds.DeviceInfo["ProductName"]; got != "LDD-130x" {
		t.Fatalf("EDS ProductName = %q, want LDD-130x", got)
	}
	if got := eds.DeviceInfo["ProductNumber"]; got != "0x514" {
		t.Fatalf("EDS ProductNumber = %q, want 0x514", got)
	}
	requireEDSObjectName(t, eds.Objects, "2020", "Actual Output Current")
	requireEDSObjectName(t, eds.Objects, "2230", "Output Enable")
	requireEDSObjectName(t, eds.Objects, "2401", "Set Power")

	var index lddMetadataIndexSource
	readCatalogueSourceJSON(t, "catalogues/sources/ldd_130x_metadata_index.v221.json", &index)
	if index.SchemaVersion != "mecom_ldd_130x_metadata_index.v1" {
		t.Fatalf("metadata index schema = %q", index.SchemaVersion)
	}
	requireDefinitionRef(t, index.Definition, "meerstetter.ldd_130x.v221", MeerstetterSubFamilyLDD, MeerstetterVariantLDD130x)
	if got, want := len(index.Sources), 10; got < want {
		t.Fatalf("source count = %d, want at least %d", got, want)
	}
	requireLDDSourceEncodingPresent(t, index, "ldd_130x_default_config")
	requireLDDSourceEncodingPresent(t, index, "ldd_130x_canopen_eds")
	requireLDDSourceEncodingPresent(t, index, "ldd_130x_service_software")
	requireLDDSourceEncodingPresent(t, index, "ldd_130x_ui_metadata")
	if got, want := len(index.SoftwareLabels), 250; got < want {
		t.Fatalf("service software label count = %d, want at least %d", got, want)
	}
	if index.ReleaseNotes.CurrentVersion != "2.21" ||
		index.ReleaseNotes.CurrentReleaseDate != "2025-03-25" ||
		index.ReleaseNotes.CurrentServiceSoftwareVersion != "2.21" ||
		index.ReleaseNotes.CurrentFirmwareVersion != "2.21" {
		t.Fatalf("current release metadata = %+v", index.ReleaseNotes)
	}
	requireLDDReleaseDevice(t, index, "LDD-1301", "1.10 - 1.21")
	requireLDDReleaseDevice(t, index, "LDD-1303", "1.10 - 1.30")
	requireLDDReleaseRisk(t, index, "firmware_v200_canopen_damage_risk", "2.01", "damage")
	requireLDDReleaseRisk(t, index, "output_enable_import_behavior_fixed", "2.21", "ID 2100")
	requireLDDReleaseRisk(t, index, "feedforward_lpc_feature_unlock_added", "2.20", "Feedforward")
	requireLDDReleaseVersionIssue(t, index, "2.21", "resolved", "External Temperature Measurement Limits")
	requireLDDDocumentationCheck(t, index, "output_enable_protocol_mapping", "matched", "2100", "2230")
	requireLDDDocumentationCheck(t, index, "min_nominal_voltage_protocol_mapping", "matched", "2125", "2255")
	requireLDDDocumentationCheck(t, index, "feature_unlock_license_metadata", "matched", "54000", "")
	requireLDDDocumentationCheck(t, index, "bootloader_ignore_feature_fw_limit_service_only", "service_software_only", "", "")
	requireLDDSoftwareLabel(t, index, "label_PAR_LDD_CURRENT_ACT_AVG")
	requireLDDSoftwareLabel(t, index, "label_PAR_LPC_POWER_ACT_AVG")
	requireLDDSoftwareLabel(t, index, "label_PAR_PHOTODIODE_CURRENT")
	requireLDDHiddenCandidate(t, index, "CANOPEN_NODEID", "canopen")
	requireLDDHiddenCandidate(t, index, "LDD_CURRENT_CAL_A0", "ldd")
	requireLDDHiddenCandidate(t, index, "IVCURVE_ENABLE", "ivcurve")

	var ui lddUIMetadataSource
	readCatalogueSourceJSON(t, "catalogues/sources/ldd_130x_ui_metadata.v221.json", &ui)
	if ui.SchemaVersion != "mecom_ldd_130x_ui_metadata.v1" {
		t.Fatalf("UI metadata schema = %q", ui.SchemaVersion)
	}
	requireDefinitionRef(t, ui.Definition, "meerstetter.ldd_130x.v221", MeerstetterSubFamilyLDD, MeerstetterVariantLDD130x)
	if strings.TrimSpace(ui.ResourceStringEncoding) == "" || strings.TrimSpace(ui.StringsOutputEncoding) == "" {
		t.Fatalf("UI metadata encodings = %q/%q", ui.ResourceStringEncoding, ui.StringsOutputEncoding)
	}
	requireLDDSourceEncodingMatches(t, index, "ldd_130x_service_software", ui.ResourceStringEncoding)
	requireLDDSourceStringsOutputEncodingMatches(t, index, "ldd_130x_service_software", ui.StringsOutputEncoding)
	if got, want := len(ui.ParameterContexts), 250; got < want {
		t.Fatalf("UI parameter context count = %d, want at least %d", got, want)
	}
	if got, want := len(ui.ParameterPaths), 40; got < want {
		t.Fatalf("UI parameter path count = %d, want at least %d", got, want)
	}
	if got, want := len(ui.UINotes), 12; got < want {
		t.Fatalf("UI note count = %d, want at least %d", got, want)
	}
	if got, want := len(ui.UIStrings), 500; got < want {
		t.Fatalf("UI string count = %d, want at least %d", got, want)
	}
	requireLDDParameterContext(t, ui, "LDD_OUTPUT_EN", "Output Enable", "Operation")
	requireLDDParameterContext(t, ui, "LDD_VOLTAGE_LIMIT_MIN", "Min Nominal Voltage", "Output Limits")
	requireLDDParameterContext(t, ui, "IGNORE_FEATURE_FIRMW_LIM", "Ignore Feature FW Limit", "Bootloader")
	requireLDDParameterProtocol(t, ui, "LDD_OUTPUT_EN", "protocol_documented", "2100", "2230")
	requireLDDParameterProtocol(t, ui, "LDD_VOLTAGE_LIMIT_MIN", "protocol_documented", "2125", "2255")
	requireLDDParameterProtocol(t, ui, "IGNORE_FEATURE_FIRMW_LIM", "service_software_only", "", "")
	requireLDDUIPath(t, ui, "Operation.Input Source Selection.Output Enable")
	requireLDDUIPath(t, ui, "ME.Bootloader.Ignore Feature FW Limit")
	requireLDDUINote(t, ui, "limit", "minimal nominal voltage limit")
	requireLDDUINote(t, ui, "safety_warning", "Do not use this feature")
	requireLDDUINote(t, ui, "license", "bound to a specific device")
	requireLDDUINote(t, ui, "safety_warning", "DO NOT INTERRUPT POWER")
}

func readCatalogueSourceJSON(t *testing.T, path string, dst any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func requireDefinitionRef(t *testing.T, definition map[string]string, ref, subFamily, variant string) {
	t.Helper()
	if definition["definition_ref"] != ref ||
		definition["system"] != MeComDefinitionSystem ||
		definition["family"] != MeerstetterDefinitionFamily ||
		definition["sub_family"] != subFamily ||
		definition["variant"] != variant {
		t.Fatalf("definition = %#v", definition)
	}
}

func requireDefaultValue(t *testing.T, defaults tecDefaultConfigSource, id, inst, value, pathFragment string) {
	t.Helper()
	param, ok := defaults.Parameters[id]
	if !ok {
		t.Fatalf("defaults missing MeParID %s", id)
	}
	instance, ok := param.Instances[inst]
	if !ok {
		t.Fatalf("defaults missing MeParID %s instance %s", id, inst)
	}
	if instance.DefaultValueText != value {
		t.Fatalf("default %s.%s = %q, want %q", id, inst, instance.DefaultValueText, value)
	}
	if !strings.Contains(instance.Path, pathFragment) {
		t.Fatalf("default %s.%s path = %q, want fragment %q", id, inst, instance.Path, pathFragment)
	}
}

func requireLDDDefaultValue(t *testing.T, defaults lddDefaultConfigSource, key, value, group, visibility string) {
	t.Helper()
	param, ok := defaults.Parameters[key]
	if !ok {
		t.Fatalf("LDD defaults missing %s", key)
	}
	if param.DefaultValueText != value {
		t.Fatalf("LDD default %s = %q, want %q", key, param.DefaultValueText, value)
	}
	if param.Group != group || param.Visibility != visibility {
		t.Fatalf("LDD default %s group/visibility = %q/%q, want %q/%q", key, param.Group, param.Visibility, group, visibility)
	}
	if len(param.SourceEvidence) == 0 {
		t.Fatalf("LDD default %s missing source evidence", key)
	}
}

func requireEDSObjectName(t *testing.T, objects map[string]struct {
	Index      string `json:"index"`
	Name       string `json:"parameter_name"`
	AccessType string `json:"access_type"`
	DataType   string `json:"data_type"`
	Subobjects map[string]struct {
		Name       string `json:"parameter_name"`
		AccessType string `json:"access_type"`
		DataType   string `json:"data_type"`
	} `json:"subobjects"`
}, index, fragment string) {
	t.Helper()
	obj, ok := objects[index]
	if !ok {
		t.Fatalf("EDS missing object 0x%s", index)
	}
	if !strings.Contains(strings.ToLower(obj.Name), strings.ToLower(fragment)) {
		t.Fatalf("EDS 0x%s name = %q, want fragment %q", index, obj.Name, fragment)
	}
}

func requireLDDSoftwareLabel(t *testing.T, index lddMetadataIndexSource, label string) {
	t.Helper()
	for _, got := range index.SoftwareLabels {
		if got == label {
			return
		}
	}
	t.Fatalf("missing LDD service software label %s", label)
}

func requireLDDSourceEncodingPresent(t *testing.T, index lddMetadataIndexSource, id string) lddMetadataSourceRecord {
	t.Helper()
	for _, source := range index.Sources {
		if source.ID != id {
			continue
		}
		if strings.TrimSpace(source.TextEncoding) == "" {
			t.Fatalf("LDD source %s missing text encoding provenance", id)
		}
		return source
	}
	t.Fatalf("missing LDD source %s", id)
	return lddMetadataSourceRecord{}
}

func requireLDDSourceEncodingMatches(t *testing.T, index lddMetadataIndexSource, id, encoding string) {
	t.Helper()
	source := requireLDDSourceEncodingPresent(t, index, id)
	if source.TextEncoding != encoding {
		t.Fatalf("LDD source %s encoding = %q, want recorded UI resource encoding %q", id, source.TextEncoding, encoding)
	}
}

func requireLDDSourceStringsOutputEncodingMatches(t *testing.T, index lddMetadataIndexSource, id, encoding string) {
	t.Helper()
	source := requireLDDSourceEncodingPresent(t, index, id)
	if source.StringsOutputEncoding != encoding {
		t.Fatalf("LDD source %s strings output encoding = %q, want recorded UI strings encoding %q", id, source.StringsOutputEncoding, encoding)
	}
}

func requireLDDReleaseDevice(t *testing.T, index lddMetadataIndexSource, device, hardwareVersion string) {
	t.Helper()
	for _, got := range index.ReleaseNotes.SupportedDevices {
		if got.Device == device && got.HardwareVersion == hardwareVersion {
			return
		}
	}
	t.Fatalf("missing release-note supported device %s hardware %s", device, hardwareVersion)
}

func requireLDDReleaseRisk(t *testing.T, index lddMetadataIndexSource, id, version, summaryFragment string) {
	t.Helper()
	for _, note := range index.ReleaseNotes.RiskNotes {
		if note.ID != id {
			continue
		}
		if note.Version != version {
			t.Fatalf("risk %s version = %q, want %q", id, note.Version, version)
		}
		if !strings.Contains(strings.ToLower(note.Summary), strings.ToLower(summaryFragment)) {
			t.Fatalf("risk %s summary = %q, want fragment %q", id, note.Summary, summaryFragment)
		}
		if len(note.SourceEvidence) == 0 {
			t.Fatalf("risk %s missing source evidence", id)
		}
		return
	}
	t.Fatalf("missing release-note risk %s", id)
}

func requireLDDReleaseVersionIssue(t *testing.T, index lddMetadataIndexSource, version, section, fragment string) {
	t.Helper()
	for _, got := range index.ReleaseNotes.Versions {
		if got.Version != version {
			continue
		}
		var items []string
		switch section {
		case "new":
			items = got.NewFeatures
		case "resolved":
			items = got.ResolvedIssues
		case "known":
			items = got.KnownIssues
		default:
			t.Fatalf("unknown release-note section %q", section)
		}
		for _, item := range items {
			if strings.Contains(strings.ToLower(item), strings.ToLower(fragment)) {
				return
			}
		}
		t.Fatalf("release %s %s issues missing fragment %q", version, section, fragment)
	}
	t.Fatalf("missing release-note version %s", version)
}

func requireLDDDocumentationCheck(t *testing.T, index lddMetadataIndexSource, id, status, mecomID, canopenIndex string) {
	t.Helper()
	for _, check := range index.DocumentationCrossChecks.Checks {
		if check.ID != id {
			continue
		}
		if check.Status != status {
			t.Fatalf("documentation check %s status = %q, want %q", id, check.Status, status)
		}
		if mecomID != "" && check.MeComID != mecomID {
			t.Fatalf("documentation check %s MeCom ID = %q, want %q", id, check.MeComID, mecomID)
		}
		if canopenIndex != "" && check.CANopenIndex != canopenIndex {
			t.Fatalf("documentation check %s CANopen index = %q, want %q", id, check.CANopenIndex, canopenIndex)
		}
		if check.Summary == "" || len(check.SourceEvidence) == 0 {
			t.Fatalf("documentation check %s missing summary or source evidence", id)
		}
		return
	}
	t.Fatalf("missing documentation check %s", id)
}

func requireLDDHiddenCandidate(t *testing.T, index lddMetadataIndexSource, key, group string) {
	t.Helper()
	for _, candidate := range index.HiddenCandidates {
		if candidate.Key == key && candidate.Group == group && candidate.Visibility == "advanced" {
			return
		}
	}
	t.Fatalf("missing LDD hidden candidate %s in group %q", key, group)
}

func requireLDDParameterContext(t *testing.T, ui lddUIMetadataSource, key, labelFragment, contextFragment string) {
	t.Helper()
	context, ok := ui.ParameterContexts[key]
	if !ok {
		t.Fatalf("missing LDD UI parameter context %s", key)
	}
	if !containsStringFragment(context.DisplayCandidates, labelFragment) {
		t.Fatalf("LDD UI context %s display candidates = %v, want fragment %q", key, context.DisplayCandidates, labelFragment)
	}
	if !containsStringFragment(context.ContextStack, contextFragment) {
		t.Fatalf("LDD UI context %s stack = %v, want fragment %q", key, context.ContextStack, contextFragment)
	}
	if len(context.SourceEvidence) == 0 {
		t.Fatalf("LDD UI context %s missing source evidence", key)
	}
}

func requireLDDParameterProtocol(t *testing.T, ui lddUIMetadataSource, key, status, protocolID, canopenIndex string) {
	t.Helper()
	context, ok := ui.ParameterContexts[key]
	if !ok {
		t.Fatalf("missing LDD UI parameter context %s", key)
	}
	if context.ProtocolStatus != status {
		t.Fatalf("LDD UI context %s protocol status = %q, want %q", key, context.ProtocolStatus, status)
	}
	if protocolID != "" && !containsExactString(context.ProtocolIDs, protocolID) {
		t.Fatalf("LDD UI context %s protocol IDs = %v, want %q", key, context.ProtocolIDs, protocolID)
	}
	if canopenIndex != "" && !containsExactString(context.CANopenIndices, canopenIndex) {
		t.Fatalf("LDD UI context %s CANopen indices = %v, want %q", key, context.CANopenIndices, canopenIndex)
	}
	if protocolID == "" && len(context.ProtocolIDs) != 0 {
		t.Fatalf("LDD UI context %s protocol IDs = %v, want none", key, context.ProtocolIDs)
	}
	if canopenIndex == "" && len(context.CANopenIndices) != 0 {
		t.Fatalf("LDD UI context %s CANopen indices = %v, want none", key, context.CANopenIndices)
	}
	if context.PrimaryDisplayCandidate == "" {
		t.Fatalf("LDD UI context %s missing primary display candidate", key)
	}
}

func requireLDDUIPath(t *testing.T, ui lddUIMetadataSource, path string) {
	t.Helper()
	for _, got := range ui.ParameterPaths {
		if got.Path != path {
			continue
		}
		if len(got.Tree) == 0 {
			t.Fatalf("LDD UI path %s missing tree", path)
		}
		if len(got.SourceEvidence) == 0 {
			t.Fatalf("LDD UI path %s missing source evidence", path)
		}
		return
	}
	t.Fatalf("missing LDD UI path %s", path)
}

func requireLDDUINote(t *testing.T, ui lddUIMetadataSource, kind, fragment string) {
	t.Helper()
	for _, note := range ui.UINotes {
		if note.Kind != kind {
			continue
		}
		if !strings.Contains(strings.ToLower(note.Text), strings.ToLower(fragment)) {
			continue
		}
		if len(note.SourceEvidence) == 0 {
			t.Fatalf("LDD UI note %s missing source evidence", note.ID)
		}
		return
	}
	t.Fatalf("missing LDD UI note kind %q with fragment %q", kind, fragment)
}

func containsStringFragment(items []string, fragment string) bool {
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), strings.ToLower(fragment)) {
			return true
		}
	}
	return false
}

func containsExactString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func requireHelp(t *testing.T, help tecHelpSource, id, fragment string) {
	t.Helper()
	param, ok := help.Parameters[id]
	if !ok {
		t.Fatalf("help missing MeParID %s", id)
	}
	if !strings.Contains(strings.ToLower(param.Help), strings.ToLower(fragment)) {
		t.Fatalf("help %s = %q, want fragment %q", id, param.Help, fragment)
	}
}

func requireSafetyNote(t *testing.T, help tecHelpSource, id, fragment string) {
	t.Helper()
	param, ok := help.Parameters[id]
	if !ok {
		t.Fatalf("help missing MeParID %s", id)
	}
	if !strings.Contains(strings.ToLower(param.SafetyNote), strings.ToLower(fragment)) {
		t.Fatalf("safety note %s = %q, want fragment %q", id, param.SafetyNote, fragment)
	}
}

func requireHiddenCandidate(t *testing.T, index tecMetadataIndexSource, id int, visibility string) {
	t.Helper()
	for _, candidate := range index.HiddenCandidates {
		if candidate.MeParID == id && candidate.Visibility == visibility {
			return
		}
	}
	t.Fatalf("missing hidden candidate %d with visibility %q", id, visibility)
}

func requireSDOMapAlias(t *testing.T, sdoMap tecCANopenSDOMapSource, mecomID int, canopenIndex string, canopenObjectDecimal int, valueType string, maxInstances int) {
	t.Helper()
	for _, mapping := range sdoMap.Mappings {
		if mapping.MeComID != mecomID {
			continue
		}
		if mapping.CANopen.Index != canopenIndex {
			t.Fatalf("MeCom %d CANopen index = %q, want %q", mecomID, mapping.CANopen.Index, canopenIndex)
		}
		if mapping.ValueType != valueType {
			t.Fatalf("MeCom %d value_type = %q, want %q", mecomID, mapping.ValueType, valueType)
		}
		if mapping.Instances.Mode != "subindex" || mapping.Instances.Min != 1 || mapping.Instances.Max != maxInstances {
			t.Fatalf("MeCom %d instances = %+v, want subindex instances 1..%d", mecomID, mapping.Instances, maxInstances)
		}
		if !hasAlias(mapping.Aliases, "canopen_object_decimal", canopenObjectDecimal) {
			t.Fatalf("MeCom %d missing CANopen decimal alias %d", mecomID, canopenObjectDecimal)
		}
		if len(mapping.SourceEvidence) == 0 {
			t.Fatalf("MeCom %d missing source evidence", mecomID)
		}
		return
	}
	t.Fatalf("missing SDO mapping for MeCom %d", mecomID)
}

func requireUnsupportedSDOPath(t *testing.T, sdoMap tecCANopenSDOMapSource, id any, reasonFragment string) {
	t.Helper()
	for _, unsupported := range sdoMap.Unsupported {
		if !sameCatalogueSourceID(unsupported.ID, id) {
			continue
		}
		if !strings.Contains(strings.ToLower(unsupported.Reason), strings.ToLower(reasonFragment)) {
			t.Fatalf("unsupported %v reason = %q, want fragment %q", id, unsupported.Reason, reasonFragment)
		}
		if len(unsupported.SourceEvidence) == 0 {
			t.Fatalf("unsupported %v missing source evidence", id)
		}
		return
	}
	t.Fatalf("missing unsupported SDO path %v", id)
}

func requireProtocolInventoryCommand(t *testing.T, sdoMap tecCANopenSDOMapSource, command, behaviorFragment string) {
	t.Helper()
	for _, entry := range sdoMap.ProtocolInventory {
		if entry.Command != command {
			continue
		}
		if entry.Status == "" {
			t.Fatalf("protocol inventory %s missing status", command)
		}
		if !strings.Contains(strings.ToLower(entry.BridgeBehavior), strings.ToLower(behaviorFragment)) {
			t.Fatalf("protocol inventory %s bridge_behavior = %q, want fragment %q", command, entry.BridgeBehavior, behaviorFragment)
		}
		if len(entry.SourceEvidence) == 0 {
			t.Fatalf("protocol inventory %s missing source evidence", command)
		}
		return
	}
	t.Fatalf("missing protocol inventory command %s", command)
}

func requireBridgeTransform(t *testing.T, sdoMap tecCANopenSDOMapSource, id int, behaviorFragment string) {
	t.Helper()
	for _, entry := range sdoMap.BridgeTransforms {
		if entry.MeComID != id {
			continue
		}
		if entry.Name == "" || entry.Trigger == "" {
			t.Fatalf("bridge transform %d missing name or trigger", id)
		}
		if entry.ValueType == "" || entry.Runtime.Kind == "" {
			t.Fatalf("bridge transform %d missing value_type or runtime kind", id)
		}
		if !strings.Contains(strings.ToLower(entry.BridgeBehavior), strings.ToLower(behaviorFragment)) {
			t.Fatalf("bridge transform %d bridge_behavior = %q, want fragment %q", id, entry.BridgeBehavior, behaviorFragment)
		}
		if len(entry.SourceEvidence) == 0 {
			t.Fatalf("bridge transform %d missing source evidence", id)
		}
		return
	}
	t.Fatalf("missing bridge transform for MeCom ID %d", id)
}

func sameCatalogueSourceID(got, want any) bool {
	switch want := want.(type) {
	case int:
		switch got := got.(type) {
		case float64:
			return got == float64(want)
		case int:
			return got == want
		default:
			return false
		}
	case string:
		got, ok := got.(string)
		return ok && got == want
	default:
		return got == want
	}
}

func hasAlias(aliases []struct {
	Space string `json:"space"`
	ID    any    `json:"id"`
}, space string, id int) bool {
	for _, alias := range aliases {
		if alias.Space != space {
			continue
		}
		switch value := alias.ID.(type) {
		case float64:
			return int(value) == id
		case int:
			return value == id
		}
	}
	return false
}
