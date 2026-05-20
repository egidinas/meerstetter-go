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
