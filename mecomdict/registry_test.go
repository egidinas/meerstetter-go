package mecomdict

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParameterRegistryForFamilyKeepsControllerListsSeparate(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "params.go")
	if err := os.WriteFile(registry, []byte(`package code_reference
var TEC_PARAMETERS = []ParameterDef{
	{ID: 1000, Name: "Object Temperature", Format: "FLOAT32"},
	{ID: 104, Name: "Device Status", Format: "INT32"},
}
var _PARAMETERS = []ParameterDef{
	{ID: 1016, Name: "Laser Diode Current", Format: "FLOAT32"},
}
var LDD_1321_PARAMETERS = []ParameterDef{
	{ID: 1104, Name: "Actual Anode Voltage", Format: "FLOAT32"},
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tec, err := LoadParameterRegistryForFamily(registry, FamilyTEC)
	if err != nil {
		t.Fatal(err)
	}
	if !hasParameter(tec, 1000, "Object Temperature") {
		t.Fatalf("TEC registry missing object temperature: %#v", tec)
	}
	if hasParameter(tec, 1016, "Laser Diode Current") || hasParameter(tec, 1104, "Actual Anode Voltage") {
		t.Fatalf("TEC registry includes LDD-only parameters: %#v", tec)
	}

	ldd112x, err := LoadParameterRegistryForFamily(registry, FamilyLDD112x)
	if err != nil {
		t.Fatal(err)
	}
	if !hasParameter(ldd112x, 1016, "Laser Diode Current") {
		t.Fatalf("LDD-112x registry missing laser current: %#v", ldd112x)
	}
	if hasParameter(ldd112x, 1000, "Object Temperature") {
		t.Fatalf("LDD-112x registry includes TEC-only parameters: %#v", ldd112x)
	}

	unknown, err := LoadParameterRegistryForFamily(registry, "UNKNOWN")
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown family returned mixed registry: %#v", unknown)
	}
}

func hasParameter(params []ParameterDef, id int, name string) bool {
	for _, param := range params {
		if param.ID == id && param.Name == name {
			return true
		}
	}
	return false
}
