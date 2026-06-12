package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/canmap"
)

func sampleRegistryJSON(t *testing.T) []byte {
	t.Helper()
	reg := &canmap.Registry{
		SchemaVersion: canmap.SchemaVersion,
		Name:          "bench-a",
		Nodes: []canmap.Node{
			{Role: "rmm", NodeID: 0x10, Family: "rmm-1182"},
			{Role: "tec-a", NodeID: 0x4B, Family: "tec-v6.32"},
		},
		Signals: []canmap.Signal{{
			COBID: 0x1A1,
			Name:  "bench_a_object_temp",
			Producer: canmap.Producer{Role: "rmm", TPDO: 2, Mapping: []canmap.MapEntry{
				{Index: 0x4000, SubIndex: 2, Bits: 32},
			}},
			Consumers: []canmap.Consumer{{Role: "tec-a", RPDO: 1,
				Mapping:       []canmap.MapEntry{{Index: 0x4200, SubIndex: 1, Bits: 32}},
				SourceSelects: []canmap.SDOWrite{{Index: 0x3300, SubIndex: 1, Value: 7}}}},
			RateMS: 50,
		}},
	}
	data, err := reg.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return data
}

func canmapTestServer(t *testing.T, withFile bool) (*server, *httptest.Server) {
	t.Helper()
	s := newServer(testConfig(), time.Minute, log.New(io.Discard, "", 0))
	if withFile {
		path := filepath.Join(t.TempDir(), "canmap.json")
		if err := os.WriteFile(path, sampleRegistryJSON(t), 0o644); err != nil {
			t.Fatalf("seed registry: %v", err)
		}
		cm, err := loadCanmap(path)
		if err != nil {
			t.Fatalf("load canmap: %v", err)
		}
		s.canmap = cm
	} else {
		s.canmap = &canmapState{}
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts
}

func TestCanmapGetReturnsLoadedRegistry(t *testing.T) {
	_, ts := canmapTestServer(t, true)
	var got struct {
		Registry  *canmap.Registry `json:"registry"`
		IsPattern bool             `json:"is_pattern"`
	}
	getJSON(t, ts.URL+"/api/canmap", http.StatusOK, &got)
	if got.Registry == nil || got.Registry.Name != "bench-a" {
		t.Fatalf("unexpected registry: %+v", got.Registry)
	}
	if got.IsPattern {
		t.Fatal("concrete registry reported as pattern")
	}
	if got.Registry.Signals[0].COBID != 0x1A1 {
		t.Fatalf("cob-id lost in transit: %+v", got.Registry.Signals[0])
	}
}

func TestCanmapExportPatternStripsNodeIDs(t *testing.T) {
	_, ts := canmapTestServer(t, true)
	resp, err := http.Get(ts.URL + "/api/canmap/export?format=pattern")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Fatal("expected attachment Content-Disposition")
	}
	body, _ := io.ReadAll(resp.Body)
	pattern, err := canmap.ParseRegistry(body)
	if err != nil {
		t.Fatalf("parse exported pattern: %v", err)
	}
	if !pattern.IsPattern() {
		t.Fatalf("export format=pattern still carries node IDs: %+v", pattern.Nodes)
	}
}

func TestCanmapImportPatternInstantiatesForNewTestbed(t *testing.T) {
	s, ts := canmapTestServer(t, true)
	pattern := canmap.ExportPattern(s.canmap.registry)
	body, _ := json.Marshal(canmapImportRequest{
		Pattern: pattern,
		Name:    "bench-b",
		Bindings: []canmap.Binding{
			{Role: "rmm", NodeID: 0x20},
			{Role: "tec-a", NodeID: 0x51, Label: "bench B"},
		},
	})
	resp, err := http.Post(ts.URL+"/api/canmap/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, b)
	}
	// The new registry must be persisted and concrete.
	reloaded, err := loadCanmap(s.canmap.path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	n, ok := reloaded.registry.NodeByRole("tec-a")
	if !ok || n.NodeID != 0x51 || reloaded.registry.Name != "bench-b" {
		t.Fatalf("import not persisted correctly: %+v", reloaded.registry)
	}
}

func TestCanmapConcurrentImportAndReadAreRaceFree(t *testing.T) {
	// Exercise the registry swap under concurrent imports, exports and GETs.
	// With -race this fails if registry access is not serialized.
	s, ts := canmapTestServer(t, true)
	mkPattern := func(name string) []byte {
		body, _ := json.Marshal(canmapImportRequest{
			Pattern: canmap.ExportPattern(s.canmap.current()),
			Name:    name,
			Bindings: []canmap.Binding{
				{Role: "rmm", NodeID: 0x20},
				{Role: "tec-a", NodeID: 0x51},
			},
		})
		return body
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		name := "bench-" + string(rune('a'+i))
		go func() {
			defer wg.Done()
			http.Post(ts.URL+"/api/canmap/import", "application/json", bytes.NewReader(mkPattern(name)))
		}()
		go func() { defer wg.Done(); http.Get(ts.URL + "/api/canmap") }()
		go func() { defer wg.Done(); http.Get(ts.URL + "/api/canmap/export?format=pattern") }()
	}
	wg.Wait()

	// The registry must still be loadable and internally consistent afterward.
	reloaded, err := loadCanmap(s.canmap.path)
	if err != nil || reloaded.registry == nil {
		t.Fatalf("registry unusable after concurrent imports: %v", err)
	}
	if errs := reloaded.registry.Validate(); len(errs) > 0 {
		t.Fatalf("persisted registry invalid after concurrent imports: %v", errs)
	}
}

func TestCanmapImportRejectsBadSchemaVersion(t *testing.T) {
	s, ts := canmapTestServer(t, true)
	reg := s.canmap.registry
	reg.SchemaVersion = 999 // unsupported; loadCanmap would reject on restart
	body, _ := json.Marshal(canmapImportRequest{Registry: reg})
	resp, err := http.Post(ts.URL+"/api/canmap/import", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unsupported schema_version", resp.StatusCode)
	}
}

func TestCanmapImportRejectedWithoutFilePath(t *testing.T) {
	_, ts := canmapTestServer(t, false)
	resp, err := http.Post(ts.URL+"/api/canmap/import", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409 when no -canmap path", resp.StatusCode)
	}
}

func TestCanNodeIDParsesCanonicalEndpoint(t *testing.T) {
	cases := map[string]struct {
		want byte
		ok   bool
	}{
		"can:can0/0x4b":       {0x4B, true},
		"can:can0/75":         {75, true},
		"can:vcan0/0x10":      {0x10, true},
		"serial:/dev/ttyUSB0": {0, false},
		"tcp:127.0.0.1:50000": {0, false},
		"can:can0/0":          {0, false},
		"can:can0/200":        {0, false},
	}
	for endpoint, want := range cases {
		got, ok := canNodeID(endpoint)
		if ok != want.ok || got != want.want {
			t.Errorf("canNodeID(%q) = (0x%02X, %v), want (0x%02X, %v)", endpoint, got, ok, want.want, want.ok)
		}
	}
}
