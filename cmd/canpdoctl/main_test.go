//go:build linux

package main

import (
	"math"
	"testing"

	"github.com/egidinas/meerstetter-go/canmap"
)

func TestParseObjectSpec(t *testing.T) {
	obj, err := parseObjectSpec("0x4200:1")
	if err != nil {
		t.Fatal(err)
	}
	if obj.index != 0x4200 || obj.subIndex != 1 {
		t.Fatalf("object = 0x%04X:%02X, want 0x4200:01", obj.index, obj.subIndex)
	}
	if _, err := parseObjectSpec("0x4200"); err == nil {
		t.Fatal("expected missing subindex error")
	}
}

func TestParseNodeRejectsBroadcast(t *testing.T) {
	if _, err := parseNode("0"); err == nil {
		t.Fatal("expected broadcast node rejection")
	}
	if node, err := parseNode("0x7f"); err != nil || node != 0x7f {
		t.Fatalf("node 0x7f = 0x%02X, %v", node, err)
	}
}

func TestEncodeValue(t *testing.T) {
	got, err := encodeValue("float32", "25.5")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || math.Float32frombits(uint32(got[0])|uint32(got[1])<<8|uint32(got[2])<<16|uint32(got[3])<<24) != 25.5 {
		t.Fatalf("float32 payload = % X", got)
	}
	got, err = encodeValue("byte", "1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("byte payload = % X, want 01", got)
	}
	got, err = encodeValue("uint16", "0x1234")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 0x34 || got[1] != 0x12 {
		t.Fatalf("uint16 payload = % X, want 34 12", got)
	}
}

func TestFormatValueSupportsUint16(t *testing.T) {
	got := formatValue("uint16", []byte{0x34, 0x12})
	if got != "0x1234" {
		t.Fatalf("uint16 format = %q, want 0x1234", got)
	}
}

func TestValidateValueLength(t *testing.T) {
	if err := validateValueLength("uint16", []byte{0x34, 0x12}); err != nil {
		t.Fatal(err)
	}
	if err := validateValueLength("uint16", []byte{0x34}); err == nil {
		t.Fatal("expected short uint16 rejection")
	}
	if err := validateValueLength("hex", []byte{0}); err == nil {
		t.Fatal("expected unknown type rejection")
	}
}

func TestParseRawPayload(t *testing.T) {
	got, err := parseRawPayload("00 00 da 41 00 00 e4 41")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x00, 0xda, 0x41, 0x00, 0x00, 0xe4, 0x41}
	if len(got) != len(want) {
		t.Fatalf("payload length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("payload[%d] = 0x%02X, want 0x%02X", i, got[i], want[i])
		}
	}
	if _, err := parseRawPayload("00 11 22 33 44 55 66 77 88"); err == nil {
		t.Fatal("expected max DLC error")
	}
}

func TestParseMappingList(t *testing.T) {
	got, err := parseMappingList("0x4200:1:32,0x4201:1:32")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Raw() != 0x42000120 || got[1].Raw() != 0x42010120 {
		t.Fatalf("mapping = %+v", got)
	}
	empty, err := parseMappingList("none")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("none mapping = %+v, want empty", empty)
	}
}

func TestBuildPDOPlanRejectsOutOfRangePDONumber(t *testing.T) {
	_, err := buildPDOPlan(pdoPlanInput{
		dir:              "rpdo",
		nodeID:           0x51,
		pdoNumber:        65537,
		cobID:            0x251,
		transmissionType: 0xFE,
		mapping:          nil,
	})
	if err == nil {
		t.Fatal("expected out-of-range PDO rejection")
	}
}

func TestBuildPDOPlanDisablesEmptyMapping(t *testing.T) {
	plan, err := buildPDOPlan(pdoPlanInput{
		dir:              "rpdo",
		nodeID:           0x51,
		pdoNumber:        1,
		cobID:            0x251,
		transmissionType: 0xFE,
		mapping:          nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantFinal := "sdo-write 0x1400:01 uint32 0x80000251"
	if got := plan.steps[len(plan.steps)-2].String(); got != wantFinal {
		t.Fatalf("final COB-ID step = %q, want %q", got, wantFinal)
	}
}

func TestBuildPDOPlanRejectsSubindexZeroMappingByDefault(t *testing.T) {
	input := pdoPlanInput{
		dir:              "rpdo",
		nodeID:           0x51,
		pdoNumber:        1,
		cobID:            0x251,
		transmissionType: 0xFE,
		mapping:          []canmap.MapEntry{{Index: 0x4200, SubIndex: 0, Bits: 32}},
	}
	if _, err := buildPDOPlan(input); err == nil {
		t.Fatal("expected subindex-zero mapping rejection")
	}
	input.allowSub0Mapping = true
	if _, err := buildPDOPlan(input); err != nil {
		t.Fatalf("explicit subindex-zero mapping override rejected: %v", err)
	}
}

func TestBuildTPDOPlanCanSetEventTimerAndRemainDisabled(t *testing.T) {
	plan, err := buildPDOPlan(pdoPlanInput{
		dir:              "tpdo",
		nodeID:           0x54,
		pdoNumber:        1,
		cobID:            0x1D4,
		transmissionType: 0xFE,
		setEventTimer:    true,
		eventTimerMS:     100,
		leaveDisabled:    true,
		mapping:          []canmap.MapEntry{{Index: 0x2100, SubIndex: 1, Bits: 32}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"nmt-preop",
		"sdo-write 0x1800:01 uint32 0x800001D4",
		"sdo-write 0x1A00:00 byte 0",
		"sdo-write 0x1A00:01 uint32 0x21000120",
		"sdo-write 0x1800:02 byte 254",
		"sdo-write 0x1800:05 uint16 0x0064",
		"sdo-write 0x1A00:00 byte 1",
		"sdo-write 0x1800:01 uint32 0x800001D4",
		"nmt-start",
	}
	if len(plan.steps) != len(want) {
		t.Fatalf("steps=%d, want %d: %+v", len(plan.steps), len(want), plan.steps)
	}
	for i, step := range plan.steps {
		if step.String() != want[i] {
			t.Fatalf("step %d = %q, want %q", i, step.String(), want[i])
		}
	}
}

func TestBuildPDOPlanRejectsEventTimerForRPDO(t *testing.T) {
	_, err := buildPDOPlan(pdoPlanInput{
		dir:              "rpdo",
		nodeID:           0x51,
		pdoNumber:        1,
		cobID:            0x251,
		transmissionType: 0xFE,
		setEventTimer:    true,
		eventTimerMS:     100,
		mapping:          nil,
	})
	if err == nil {
		t.Fatal("expected RPDO event timer rejection")
	}
}

func TestRunPDOSendRejectsUnsafePeriods(t *testing.T) {
	if err := runPDOSend([]string{"-cob-id", "0x2A0", "-type", "float32", "-value", "1", "-count", "2", "-period", "-1ms"}); err == nil {
		t.Fatal("expected negative period rejection")
	}
	if err := runPDOSend([]string{"-cob-id", "0x2A0", "-type", "float32", "-value", "1", "-count", "2", "-period", "0"}); err == nil {
		t.Fatal("expected zero period rejection for repeated send")
	}
	if err := runPDOSend([]string{"-cob-id", "0x2A0", "-type", "float32", "-value", "1", "-count", "1", "-period", "0"}); err != nil {
		t.Fatalf("single zero-period dry run rejected: %v", err)
	}
}

func TestBuildPDOPlanOrdering(t *testing.T) {
	plan, err := buildPDOPlan(pdoPlanInput{
		dir:              "rpdo",
		nodeID:           0x51,
		pdoNumber:        1,
		cobID:            0x1B8,
		transmissionType: 0xFE,
		mapping:          []canmap.MapEntry{{Index: 0x4200, SubIndex: 1, Bits: 32}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"nmt-preop",
		"sdo-write 0x1400:01 uint32 0x800001B8",
		"sdo-write 0x1600:00 byte 0",
		"sdo-write 0x1600:01 uint32 0x42000120",
		"sdo-write 0x1400:02 byte 254",
		"sdo-write 0x1600:00 byte 1",
		"sdo-write 0x1400:01 uint32 0x000001B8",
		"nmt-start",
	}
	if len(plan.steps) != len(want) {
		t.Fatalf("steps=%d, want %d: %+v", len(plan.steps), len(want), plan.steps)
	}
	for i, step := range plan.steps {
		if step.String() != want[i] {
			t.Fatalf("step %d = %q, want %q", i, step.String(), want[i])
		}
	}
}
