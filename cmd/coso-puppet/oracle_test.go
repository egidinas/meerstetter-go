package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoadCoSoOracleValidatesConnectionDance(t *testing.T) {
	oracle, err := loadCoSoOracle("testdata/coso_tec_v631_oracle.json")
	if err != nil {
		t.Fatalf("loadCoSoOracle returned error: %v", err)
	}
	if err := validateCoSoOracle(oracle); err != nil {
		t.Fatalf("validateCoSoOracle returned error: %v", err)
	}
	if oracle.SchemaVersion != "coso_connection_oracle.v1" {
		t.Fatalf("SchemaVersion = %q", oracle.SchemaVersion)
	}
	if oracle.Device.Definition != "meerstetter.tec.v631" {
		t.Fatalf("Device.Definition = %q", oracle.Device.Definition)
	}
	if !oracle.Transport.SingleFlight || oracle.Transport.TimeoutRetries != 3 {
		t.Fatalf("transport retry/serialization policy = %+v", oracle.Transport)
	}

	wantPhaseOrder := []string{
		"identity_gate",
		"firmware_gate",
		"metadata_load",
		"big_data_initial_read",
		"value_refresh",
		"status_poll",
		"crtv_stream_setup",
		"write_config",
	}
	for i, name := range wantPhaseOrder {
		phase := requireOraclePhase(t, oracle, name)
		if phase.Order != (i+1)*10 {
			t.Fatalf("phase %s order = %d", name, phase.Order)
		}
	}

	firmware := requireOraclePhase(t, oracle, "firmware_gate")
	requireOracleCommand(t, firmware, "?VI")
	fwVersion := requireOracleParameterCommand(t, firmware, "?VR", 112, 1)
	if fwVersion.ValueType != "float32" || fwVersion.Request.Payload != "?VR007001" {
		t.Fatalf("firmware version fallback = %+v", fwVersion)
	}
	fwRevision := requireOracleParameterCommand(t, firmware, "?VR", 113, 1)
	if fwRevision.ValueType != "int32" || fwRevision.Request.Payload != "?VR007101" {
		t.Fatalf("firmware revision fallback = %+v", fwRevision)
	}

	metadata := requireOracleCommand(t, requireOraclePhase(t, oracle, "metadata_load"), "?VM")
	if metadata.UnavailableErrorCode != 5 || metadata.UnavailableAction != "mark_parameter_unavailable_continue" {
		t.Fatalf("metadata unavailable policy = code %d action %q", metadata.UnavailableErrorCode, metadata.UnavailableAction)
	}
	operatingMode := requireOracleSample(t, metadata, 2040, 1)
	if operatingMode.ValueType != "int32" || operatingMode.CacheBehavior != "metadata_live_actual" || operatingMode.Request.Payload != "?VM07F801" {
		t.Fatalf("operating mode sample = %+v", operatingMode)
	}
	flashErase := requireOracleSample(t, metadata, 50000, 1)
	if flashErase.Support != "unsupported" || flashErase.Writable {
		t.Fatalf("50000 support/writability = %+v", flashErase)
	}

	status := requireOraclePhase(t, oracle, "status_poll")
	if status.UpdateIntervalMS != 300 {
		t.Fatalf("status poll interval = %d", status.UpdateIntervalMS)
	}
	if !requireOracleCommand(t, requireOraclePhase(t, oracle, "value_refresh"), "?VX").PreserveRequestOrder {
		t.Fatal("value refresh ?VX must preserve request order")
	}
	if !requireOracleCommand(t, requireOraclePhase(t, oracle, "write_config"), "MS").CompatibilityOnly {
		t.Fatal("extended MS write must be compatibility-only")
	}
}

func TestCoSoOraclePublicSafetyRejectsPrivateMarkers(t *testing.T) {
	oracle, err := loadCoSoOracle("testdata/coso_tec_v631_oracle.json")
	if err != nil {
		t.Fatalf("loadCoSoOracle returned error: %v", err)
	}
	if err := validateCoSoOraclePublicSafety(oracle); err != nil {
		t.Fatalf("validateCoSoOraclePublicSafety returned error: %v", err)
	}

	oracle.SourcePolicy += " C:\\Users\\operator\\Trace.txt"
	err = validateCoSoOraclePublicSafety(oracle)
	if err == nil {
		t.Fatal("validateCoSoOraclePublicSafety accepted a private Windows path")
	}
	if !strings.Contains(err.Error(), "forbidden token") {
		t.Fatalf("error = %v, want forbidden token", err)
	}
}

func TestCoSoOracleRequestPayloadsDeriveSmokeSequence(t *testing.T) {
	oracle, err := loadCoSoOracle("testdata/coso_tec_v631_oracle.json")
	if err != nil {
		t.Fatalf("loadCoSoOracle returned error: %v", err)
	}
	got := coSoOracleRequestPayloads(oracle)
	want := []string{
		"?IF",
		"?VR007001",
		"?VR007101",
		"?VM07F801",
		"?VMC35001",
		"?VB00780100000000FFFF",
		"?VX0107F801",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("payloads = %#v, want %#v", got, want)
	}
}

func TestRunOraclePrintsRequestPayloads(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"oracle", "-file", "testdata/coso_tec_v631_oracle.json", "-requests"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run returned %d, stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"oracle=meerstetter.tec.coso.v631.connection",
		"phase[30]=metadata_load commands=?VM",
		"request_payloads\n?IF\n?VR007001\n?VR007101\n?VM07F801",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func requireOraclePhase(t *testing.T, oracle coSoOracle, name string) coSoOraclePhase {
	t.Helper()
	for _, phase := range oracle.Phases {
		if phase.Name == name {
			return phase
		}
	}
	t.Fatalf("phase %q not found", name)
	return coSoOraclePhase{}
}

func requireOracleCommand(t *testing.T, phase coSoOraclePhase, command string) coSoOracleCommand {
	t.Helper()
	for _, cmd := range phase.Commands {
		if cmd.Command == command {
			return cmd
		}
	}
	t.Fatalf("command %q not found in phase %q", command, phase.Name)
	return coSoOracleCommand{}
}

func requireOracleParameterCommand(t *testing.T, phase coSoOraclePhase, command string, id, inst int) coSoOracleCommand {
	t.Helper()
	for _, cmd := range phase.Commands {
		if cmd.Command == command && cmd.ParameterID == id && cmd.Instance == inst {
			return cmd
		}
	}
	t.Fatalf("command %q for %d.%d not found in phase %q", command, id, inst, phase.Name)
	return coSoOracleCommand{}
}

func requireOracleSample(t *testing.T, cmd coSoOracleCommand, id, inst int) coSoOracleParameter {
	t.Helper()
	for _, sample := range cmd.SampleParameters {
		if sample.ParameterID == id && sample.Instance == inst {
			return sample
		}
	}
	t.Fatalf("sample %d.%d not found in command %q", id, inst, cmd.Command)
	return coSoOracleParameter{}
}
