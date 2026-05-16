package objectdict

import (
	"encoding/json"
	"testing"
)

func TestObjectDict(t *testing.T) {
	min := -20.0
	max := 120.0
	dict := Dictionary{
		SchemaVersion: 1,
		Protocol:      ProtocolCANopen,
		DefinitionID:  "tec-family-v1",
		Device: Device{
			Vendor:        "Meerstetter",
			VendorNumber:  "0x000000E2",
			Product:       "TEC",
			ProductNumber: "0x000003E8",
			Revision:      "1.2",
		},
		Objects: []Object{{
			ID:          "output-enable",
			Index:       0x2300,
			Name:        "Output enable",
			Description: "Controls TEC output state",
			Entries: []Entry{{
				ID:       "output-enable.command",
				Index:    0x2300,
				SubIndex: 1,
				Name:     "Command",
				Unit:     "state",
				DataType: "uint8",
				Kind:     ValueKindEnum,
				Access:   AccessReadWrite,
				Min:      &min,
				Max:      &max,
				Enum: map[int64]string{
					0: "off",
					1: "on",
				},
				Metadata: map[string]string{"family": "tec"},
			}},
			Metadata: map[string]string{"source": "eds"},
		}},
		Metadata: map[string]string{"generated_by": "test"},
	}

	raw, err := json.Marshal(dict)
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	if got["schema_version"] != float64(1) {
		t.Fatalf("schema_version = %v, want 1", got["schema_version"])
	}
	if got["protocol"] != string(ProtocolCANopen) {
		t.Fatalf("protocol = %v, want %q", got["protocol"], ProtocolCANopen)
	}
	if got["definition_id"] != "tec-family-v1" {
		t.Fatalf("definition_id = %v", got["definition_id"])
	}

	device := got["device"].(map[string]any)
	if device["vendor"] != "Meerstetter" || device["product_number"] != "0x000003E8" {
		t.Fatalf("device = %#v", device)
	}

	objects := got["objects"].([]any)
	object := objects[0].(map[string]any)
	if object["index"] != float64(0x2300) || object["description"] != "Controls TEC output state" {
		t.Fatalf("object = %#v", object)
	}

	entry := object["entries"].([]any)[0].(map[string]any)
	if entry["kind"] != string(ValueKindEnum) {
		t.Fatalf("kind = %v, want %q", entry["kind"], ValueKindEnum)
	}
	if entry["access"] != string(AccessReadWrite) {
		t.Fatalf("access = %v, want %q", entry["access"], AccessReadWrite)
	}
	if entry["min"] != min || entry["max"] != max {
		t.Fatalf("min/max = %v/%v, want %v/%v", entry["min"], entry["max"], min, max)
	}
	enum := entry["enum"].(map[string]any)
	if enum["0"] != "off" || enum["1"] != "on" {
		t.Fatalf("enum = %#v", enum)
	}
	if entry["metadata"].(map[string]any)["family"] != "tec" {
		t.Fatalf("metadata = %#v", entry["metadata"])
	}
}

func TestObjectDictOmitsZeroEntryFields(t *testing.T) {
	raw, err := json.Marshal(Dictionary{
		SchemaVersion: 1,
		Protocol:      ProtocolMeCom,
		Objects: []Object{{
			ID:   "temperature",
			Name: "Temperature",
			Entries: []Entry{{
				ID:     "temperature.value",
				Name:   "Value",
				Kind:   ValueKindContinuous,
				Access: AccessReadOnly,
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Objects []struct {
			Index   uint16 `json:"index,omitempty"`
			Entries []struct {
				Index    uint16            `json:"index,omitempty"`
				SubIndex uint8             `json:"sub_index,omitempty"`
				Min      *float64          `json:"min,omitempty"`
				Max      *float64          `json:"max,omitempty"`
				Enum     map[string]string `json:"enum,omitempty"`
				Metadata map[string]string `json:"metadata,omitempty"`
				Raw      map[string]any    `json:"-"`
			} `json:"entries"`
			Raw map[string]any `json:"-"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	object := decoded["objects"].([]any)[0].(map[string]any)
	entry := object["entries"].([]any)[0].(map[string]any)

	for _, key := range []string{"index"} {
		if _, ok := object[key]; ok {
			t.Fatalf("object unexpectedly includes %q in %s", key, raw)
		}
	}
	for _, key := range []string{"index", "sub_index", "min", "max", "enum", "metadata"} {
		if _, ok := entry[key]; ok {
			t.Fatalf("entry unexpectedly includes %q in %s", key, raw)
		}
	}
}
