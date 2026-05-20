package mecom

import (
	"os"
	"reflect"
	"testing"
)

func TestTECCatalogueDefinitionsLoadFromJSON(t *testing.T) {
	raw, err := os.ReadFile("catalogues/tec.json")
	if err != nil {
		t.Fatalf("read TEC catalogue JSON: %v", err)
	}

	catalogue, err := loadTECCatalogueJSON(raw)
	if err != nil {
		t.Fatalf("load TEC catalogue JSON: %v", err)
	}
	if got, want := len(catalogue.ReadoutParameters), 10; got < want {
		t.Fatalf("readout parameter count = %d, want at least %d", got, want)
	}
	if got, want := len(catalogue.WriteParameters), 14; got < want {
		t.Fatalf("write parameter count = %d, want at least %d", got, want)
	}
	if got, want := len(catalogue.DerivedParameters), 4; got < want {
		t.Fatalf("derived parameter count = %d, want at least %d", got, want)
	}
	if got, want := len(catalogue.ReadoutParameters)+len(catalogue.WriteParameters)+len(catalogue.DerivedParameters), 28; got < want {
		t.Fatalf("catalogue parameter total = %d, want at least %d", got, want)
	}

	outputStageTemp := findTECReadoutDefinition(t, catalogue.ReadoutParameters, 40000)
	if outputStageTemp.Writable {
		t.Fatal("40000 output-stage temperature must stay read-only")
	}
	if outputStageTemp.Suffix != "output_stage_temp_c" || outputStageTemp.Unit != "degC" || outputStageTemp.ValueType != "float32" {
		t.Fatalf("40000 metadata = %#v", outputStageTemp)
	}
	if want := []string{"Thermal", "Output stage", "Output Stage Temperature"}; !reflect.DeepEqual(outputStageTemp.TreePath, want) {
		t.Fatalf("40000 tree path = %#v, want %#v", outputStageTemp.TreePath, want)
	}
	if got, want := len(outputStageTemp.TreePaths), 3; got != want {
		t.Fatalf("40000 tree projections = %d, want %d", got, want)
	}
	if outputStageTemp.TreePaths[0].ID != "operator" || !outputStageTemp.TreePaths[0].Default {
		t.Fatalf("40000 default tree projection = %#v", outputStageTemp.TreePaths[0])
	}
	if want := []string{"Readout", "Parameter 40000", "OutputStageTemp"}; !reflect.DeepEqual(outputStageTemp.TreePaths[1].Path, want) {
		t.Fatalf("40000 protocol tree path = %#v, want %#v", outputStageTemp.TreePaths[1].Path, want)
	}

	actualVoltage := findTECReadoutDefinition(t, catalogue.ReadoutParameters, 1021)
	if actualVoltage.Writable {
		t.Fatal("1021 actual output voltage must stay read-only")
	}

	targetObjectTemp := findTECReadoutDefinition(t, catalogue.ReadoutParameters, 3000)
	if !targetObjectTemp.Writable {
		t.Fatal("3000 target object temperature should be writable")
	}

	voltageLimit := findTECWriteDefinition(t, catalogue.WriteParameters, 2031)
	if voltageLimit.Unit != "V" || voltageLimit.ValueType != "float32" {
		t.Fatalf("2031 voltage limit metadata = %#v", voltageLimit)
	}

	cascadeTarget := findTECWriteDefinition(t, catalogue.WriteParameters, 53123)
	if cascadeTarget.Unit != "degC" || cascadeTarget.ValueType != "float32" {
		t.Fatalf("53123 cascade target metadata = %#v", cascadeTarget)
	}
}

func findTECReadoutDefinition(t *testing.T, params []mecomTECParameter, id int) mecomTECParameter {
	t.Helper()
	for _, param := range params {
		if param.ID == id {
			return param
		}
	}
	t.Fatalf("missing readout definition %d", id)
	return mecomTECParameter{}
}

func findTECWriteDefinition(t *testing.T, params []mecomTECParameter, id int) mecomTECParameter {
	t.Helper()
	for _, param := range params {
		if param.ID == id {
			return param
		}
	}
	t.Fatalf("missing write definition %d", id)
	return mecomTECParameter{}
}
