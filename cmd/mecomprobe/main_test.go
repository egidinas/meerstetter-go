package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func TestScanDialFailureRecordsEveryInstance(t *testing.T) {
	params := []parameterDef{
		{ID: 1000, Name: "temperature", Format: "FLOAT32"},
		{ID: 2000, Name: "status", Format: "INT32"},
	}
	got := scan(context.Background(), []string{"can:missing-adapter"}, 0, []int{1, 2}, params, "bulk", 8, time.Millisecond)
	if len(got) != 4 {
		t.Fatalf("results = %d, want one row per instance and parameter", len(got))
	}
	seen := map[int]int{}
	for _, res := range got {
		if res.Error == "" {
			t.Fatalf("result missing dial error: %#v", res)
		}
		seen[res.Instance]++
	}
	if seen[1] != 2 || seen[2] != 2 {
		t.Fatalf("instances = %#v, want two rows for each requested instance", seen)
	}
}

func TestValidateProbeOptionsRejectsInvalidModeChunkAndTimeout(t *testing.T) {
	cases := []struct {
		name string
		err  string
		args []any
	}{
		{name: "address low", err: "address", args: []any{-1, "bulk", 8, time.Millisecond}},
		{name: "address high", err: "address", args: []any{256, "bulk", 8, time.Millisecond}},
		{name: "mode", err: "mode", args: []any{0, "singel", 8, time.Millisecond}},
		{name: "chunk zero", err: "chunk", args: []any{0, "bulk", 0, time.Millisecond}},
		{name: "chunk high", err: "chunk", args: []any{0, "bulk", 256, time.Millisecond}},
		{name: "timeout", err: "timeout", args: []any{0, "bulk", 8, time.Duration(0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProbeOptions(tc.args[0].(int), tc.args[1].(string), tc.args[2].(int), tc.args[3].(time.Duration))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Fatalf("validateProbeOptions err = %v, want %q", err, tc.err)
			}
		})
	}
	if err := validateProbeOptions(255, "single", 255, time.Millisecond); err != nil {
		t.Fatalf("valid boundary rejected: %v", err)
	}
}

func TestDataTypeForFormatRejectsUnsupportedRegistryFormats(t *testing.T) {
	if _, ok := dataTypeForFormat("UINT16"); ok {
		t.Fatalf("UINT16 format accepted, want unsupported")
	}
	if _, ok := dataTypeForFormat("FLOAT32"); !ok {
		t.Fatalf("FLOAT32 format rejected")
	}
	if _, ok := dataTypeForFormat("INT32"); !ok {
		t.Fatalf("INT32 format rejected")
	}
}

func TestPresetParameterDefsIncludesRMMHR1Pt100(t *testing.T) {
	params, err := presetParameterDefs("RMM_1182_HR1_PT100")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(params), 9; got != want {
		t.Fatalf("RMM preset parameter count = %d, want %d", got, want)
	}
	if params[0].ID != 3000 || params[0].Format != "FLOAT32" {
		t.Fatalf("first RMM preset parameter = %#v", params[0])
	}
	if params[len(params)-1].ID != 4012 || params[len(params)-1].Format != "INT32" {
		t.Fatalf("last RMM preset parameter = %#v", params[len(params)-1])
	}
}

func TestPresetParameterDefsRejectsUnknownPreset(t *testing.T) {
	if _, err := presetParameterDefs("unknown"); err == nil || !strings.Contains(err.Error(), "unknown preset") {
		t.Fatalf("unknown preset err = %v, want unknown preset", err)
	}
}

func TestParameterDefsFromMeComParametersRejectsUnsupportedTypesAndInstances(t *testing.T) {
	if _, err := parameterDefsFromMeComParameters([]mecom.Parameter{{ID: 1, Instance: 1, Type: mecom.DataTypeLatin1}}); err == nil || !strings.Contains(err.Error(), "unsupported data type") {
		t.Fatalf("unsupported type err = %v", err)
	}
	if _, err := parameterDefsFromMeComParameters([]mecom.Parameter{{ID: 1, Instance: 2, Type: mecom.DataTypeFloat32}}); err == nil || !strings.Contains(err.Error(), "instance 2") {
		t.Fatalf("unsupported instance err = %v", err)
	}
}

func TestLoadParametersUsesRegistryLoaderAndRejectsEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	registryPath := filepath.Join(dir, "registry.go")
	if err := os.WriteFile(registryPath, []byte(`package main

var TECParameters = []ParameterDef {
	{ID: 1000, Name: "Object Temperature", Format: "FLOAT32"},
	{ID: 1200, Name: "Device Status", Format: "INT32"},
	{ID: 1000, Name: "Object Temperature Duplicate", Format: "FLOAT32"},
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	params, err := loadParameters(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(params) != 2 {
		t.Fatalf("params = %#v, want deduplicated entries", params)
	}
	if params[0].ID != 1000 || params[1].ID != 1200 || params[1].Format != "INT32" {
		t.Fatalf("params = %#v", params)
	}

	emptyPath := filepath.Join(dir, "empty.go")
	if err := os.WriteFile(emptyPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadParameters(emptyPath); err == nil || !strings.Contains(err.Error(), "no parameters") {
		t.Fatalf("empty registry err = %v, want no parameters", err)
	}
}
