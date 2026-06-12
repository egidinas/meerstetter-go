//go:build linux

package main

import (
	"errors"
	"testing"

	"github.com/egidinas/meerstetter-go/canopen"
	"github.com/egidinas/meerstetter-go/mecom"
)

func TestSDOResponseMatchesProbeRequiresObjectIdentity(t *testing.T) {
	probe := sdoProbe{Index: 0x2030, SubIndex: 1}
	if !sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2030, SubIndex: 1}, probe) {
		t.Fatal("matching SDO response was rejected")
	}
	if sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2031, SubIndex: 1}, probe) {
		t.Fatal("mismatched SDO index was accepted")
	}
	if sdoResponseMatchesProbe(canopen.SDOUploadResponse{Index: 0x2030, SubIndex: 2}, probe) {
		t.Fatal("mismatched SDO subindex was accepted")
	}
}

func TestSDOParseErrorMatchesProbeUsesAbortObjectIdentity(t *testing.T) {
	probe := sdoProbe{Index: 0x2030, SubIndex: 1}
	if !sdoParseErrorMatchesProbe(canopen.SDOAbortError{Index: 0x2030, SubIndex: 1}, probe) {
		t.Fatal("matching SDO abort was rejected")
	}
	if sdoParseErrorMatchesProbe(canopen.SDOAbortError{Index: 0x2031, SubIndex: 1}, probe) {
		t.Fatal("mismatched SDO abort index was accepted")
	}
	if sdoParseErrorMatchesProbe(canopen.SDOAbortError{Index: 0x2030, SubIndex: 2}, probe) {
		t.Fatal("mismatched SDO abort subindex was accepted")
	}
	if !sdoParseErrorMatchesProbe(errors.New("malformed SDO frame"), probe) {
		t.Fatal("non-abort parse error was ignored")
	}
}

func TestParseMeComDataType(t *testing.T) {
	tests := map[string]mecom.DataType{
		"float32": mecom.DataTypeFloat32,
		"FLOAT32": mecom.DataTypeFloat32,
		"int32":   mecom.DataTypeInt32,
		" int32 ": mecom.DataTypeInt32,
	}
	for input, want := range tests {
		got, err := parseMeComDataType(input)
		if err != nil {
			t.Fatalf("parseMeComDataType(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("parseMeComDataType(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := parseMeComDataType("latin1"); err == nil {
		t.Fatal("parseMeComDataType(latin1) returned nil error")
	}
}

func TestMeComResponseMatchesRequestRequiresAddressAndSequence(t *testing.T) {
	frame := canopen.Frame{
		ID:  0x44b,
		DLC: 8,
		Data: [8]byte{
			0x81,
			0x4b,
			0x00, 0x01,
		},
	}
	if !mecomResponseMatchesRequest(frame, 0x4b, 1, mecom.BinaryCmdQueryValue) {
		t.Fatal("matching MeCom response was rejected")
	}
	frame.Data[1] = 0x4c
	if mecomResponseMatchesRequest(frame, 0x4b, 1, mecom.BinaryCmdQueryValue) {
		t.Fatal("mismatched MeCom address was accepted")
	}
	frame.Data[1] = 0x4b
	frame.Data[0] = 0x82
	if mecomResponseMatchesRequest(frame, 0x4b, 1, mecom.BinaryCmdQueryValue) {
		t.Fatal("mismatched MeCom sequence was accepted")
	}
	frame.Data[0] = 0x01
	if mecomResponseMatchesRequest(frame, 0x4b, 1, mecom.BinaryCmdQueryValue) {
		t.Fatal("request frame was accepted as response")
	}
	frame.Data[0] = 0x81
	frame.Data[3] = byte(mecom.BinaryCmdSetValue)
	if mecomResponseMatchesRequest(frame, 0x4b, 1, mecom.BinaryCmdQueryValue) {
		t.Fatal("mismatched MeCom command was accepted")
	}
}

func TestNextMeComSequenceSkipsZeroAfterWrap(t *testing.T) {
	if got := nextMeComSequence(0); got != 1 {
		t.Fatalf("next sequence from zero = %d, want 1", got)
	}
	if got := nextMeComSequence(126); got != 127 {
		t.Fatalf("next sequence before wrap = %d, want 127", got)
	}
	if got := nextMeComSequence(127); got != 1 {
		t.Fatalf("next sequence after wrap = %d, want 1", got)
	}
}
