package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseTargetsMixedCANAndTCP(t *testing.T) {
	targets, err := parseTargets("can:can0/0x4b=75,tcp:127.0.0.1:50000=76")
	if err != nil {
		t.Fatalf("parseTargets returned error: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	if targets[0].endpoint.Network != "can" || targets[0].address != 75 {
		t.Fatalf("first target = %+v", targets[0])
	}
	if targets[1].endpoint.Network != "tcp" || targets[1].address != 76 {
		t.Fatalf("second target = %+v", targets[1])
	}
}

func TestParseTargetsRejectsInvalidAddress(t *testing.T) {
	for _, input := range []string{"tcp:127.0.0.1:50000=0", "tcp:127.0.0.1:50000=255", "tcp:127.0.0.1:50000=nope"} {
		if _, err := parseTargets(input); err == nil {
			t.Fatalf("parseTargets(%q) returned nil error", input)
		}
	}
}

func TestParseTargetsRejectsMismatchedCANEndpointAndSuffix(t *testing.T) {
	if _, err := parseTargets("can:can0/0x4b=76"); err == nil {
		t.Fatal("parseTargets accepted mismatched CAN endpoint node and suffix")
	}
}

func TestValidateRuntimeFlagsRejectsNonPositiveContinuousInterval(t *testing.T) {
	if err := validateRuntimeFlags(0, time.Second, false); err == nil {
		t.Fatal("validateRuntimeFlags accepted zero continuous interval")
	}
	if err := validateRuntimeFlags(-time.Second, time.Second, false); err == nil {
		t.Fatal("validateRuntimeFlags accepted negative continuous interval")
	}
	if err := validateRuntimeFlags(0, time.Second, true); err != nil {
		t.Fatalf("validateRuntimeFlags rejected zero interval in once mode: %v", err)
	}
}

func TestValidateRuntimeFlagsRejectsNonPositiveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		if err := validateRuntimeFlags(time.Second, timeout, false); err == nil {
			t.Fatalf("validateRuntimeFlags accepted timeout %s in continuous mode", timeout)
		}
		if err := validateRuntimeFlags(0, timeout, true); err == nil {
			t.Fatalf("validateRuntimeFlags accepted timeout %s in once mode", timeout)
		}
	}
}

func TestPrintTableIncludesVoltageColumnsAndValues(t *testing.T) {
	out := captureStdout(t, func() {
		printTable([]cycleResult{{
			target: deviceTarget{raw: "can:can0/0x4b", address: 75},
			values: []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			at:     time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC),
		}})
	})
	for _, want := range []string{"U_ch1(V)", "U_ch2(V)", "9.000", "10.000"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestAnyErrors(t *testing.T) {
	if anyErrors([]cycleResult{{err: nil}}) {
		t.Fatal("anyErrors returned true for nil error")
	}
	if !anyErrors([]cycleResult{{err: errors.New("boom")}}) {
		t.Fatal("anyErrors returned false for non-nil error")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe returned error: %v", err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	return string(out)
}
