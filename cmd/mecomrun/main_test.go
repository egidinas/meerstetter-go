package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoadScriptAcceptsDurationStrings(t *testing.T) {
	path := writeTempScript(t, `{
		"id":"sample",
		"timeout":"2s",
		"steps":[{"id":"wait","kind":"wait","duration":"50ms"}]
	}`)
	script, err := loadScript(path)
	if err != nil {
		t.Fatalf("loadScript returned error: %v", err)
	}
	if script.ID != "sample" || script.Timeout != 2*time.Second {
		t.Fatalf("script = %+v", script)
	}
	if script.Steps[0].Duration != 50*time.Millisecond {
		t.Fatalf("step duration = %s", script.Steps[0].Duration)
	}
}

func TestLoadScriptRequiresID(t *testing.T) {
	path := writeTempScript(t, `{"steps":[]}`)
	_, err := loadScript(path)
	if err == nil || !strings.Contains(err.Error(), "script.id") {
		t.Fatalf("loadScript error = %v, want script.id error", err)
	}
}

func writeTempScript(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "script-*.json")
	if err != nil {
		t.Fatalf("CreateTemp returned error: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString returned error: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	return f.Name()
}
