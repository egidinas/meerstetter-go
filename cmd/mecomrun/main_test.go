package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

func TestValidateRunAddressRejectsWrappedASCIIValues(t *testing.T) {
	ep := mecom.Endpoint{Network: "tcp", Address: "127.0.0.1:9000"}
	for _, address := range []int{-1, 256, 300} {
		if _, err := validateRunAddress(ep, address); err == nil {
			t.Fatalf("validateRunAddress accepted invalid ASCII address %d", address)
		}
	}
	for _, address := range []int{0, 255} {
		got, err := validateRunAddress(ep, address)
		if err != nil {
			t.Fatalf("validateRunAddress(%d) returned error: %v", address, err)
		}
		if got != byte(address) {
			t.Fatalf("validateRunAddress(%d) = %d", address, got)
		}
	}
}

func TestValidateRunAddressRejectsCanAddressFlag(t *testing.T) {
	ep := mecom.Endpoint{Network: "can", Address: "can0/0x4b"}
	if _, err := validateRunAddress(ep, 1); err == nil {
		t.Fatal("validateRunAddress accepted -address for CAN target")
	}
	if got, err := validateRunAddress(ep, 0); err != nil || got != 0 {
		t.Fatalf("validateRunAddress CAN default = %d, %v; want 0, nil", got, err)
	}
}

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
