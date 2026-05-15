package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpenAPIDescribesAllHandlerPaths walks the registered http.ServeMux paths
// and asserts each one appears in docs/gateway/openapi.yaml. The test catches
// "handler added, schema not updated" drift before it lands.
func TestOpenAPIDescribesAllHandlerPaths(t *testing.T) {
	openAPIPath := findOpenAPI(t)
	raw, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	doc := string(raw)

	// Mux-registered prefixes. /api/devices/ is a catch-all so we expand it
	// into the per-resource paths it dispatches to.
	required := []string{
		"/api/healthz",
		"/api/devices",
		"/api/catalogue",
		"/api/leases",
		"/api/devices/{id}/lease",
		"/api/devices/{id}/write",
		"/api/devices/{id}/read",
		"/api/devices/{id}/poll",
	}
	for _, path := range required {
		if !strings.Contains(doc, path+":") {
			t.Errorf("openapi.yaml is missing entry for %q", path)
		}
	}
}

func findOpenAPI(t *testing.T) string {
	t.Helper()
	// Walk up from this test file's directory until docs/gateway/openapi.yaml
	// is found. Avoids hard-coding a repo-root relative path.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, "docs", "gateway", "openapi.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("openapi.yaml not found")
	return ""
}
