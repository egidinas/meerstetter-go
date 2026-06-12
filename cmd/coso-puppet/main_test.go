package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egidinas/meerstetter-go/mecom"
)

func TestParseTraceLineExtractsMetadataError(t *testing.T) {
	line := "[2026-05-21 10:40:22.083] - ERROR   - ComInterface.ReadMetaDataFromDevice - Exception: Get Meta Data failed: Address: ; ID: 1045; Inst: 1; Detail: Specified argument was out of the range of valid values."

	event, ok := parseTraceLine(line)
	if !ok {
		t.Fatal("parseTraceLine returned ok=false")
	}
	if event.Timestamp != "2026-05-21 10:40:22.083" {
		t.Fatalf("Timestamp = %q", event.Timestamp)
	}
	if event.Level != "ERROR" || event.Source != "ComInterface.ReadMetaDataFromDevice" {
		t.Fatalf("level/source = %q/%q", event.Level, event.Source)
	}
	if event.ParameterID != 1045 || event.Instance != 1 {
		t.Fatalf("parameter = %d.%d, want 1045.1", event.ParameterID, event.Instance)
	}
	if event.Detail != "Specified argument was out of the range of valid values." {
		t.Fatalf("Detail = %q", event.Detail)
	}
}

func TestParseTraceLineExtractsMeParID(t *testing.T) {
	line := "[2026-05-21 09:47:21.048] - WARN    - ProgressIndicator.UpdateWithChangedActualValue - crashed for 'Auto Tuning [CH1] / Progress'. MeParID 51021.1. Detail 'NaN' is not a valid value for property 'Value'."

	event, ok := parseTraceLine(line)
	if !ok {
		t.Fatal("parseTraceLine returned ok=false")
	}
	if event.Level != "WARN" || event.ParameterID != 51021 || event.Instance != 1 {
		t.Fatalf("event = %+v", event)
	}
	if !strings.Contains(event.Detail, "NaN") {
		t.Fatalf("Detail = %q, want NaN detail", event.Detail)
	}
}

func TestBuildMeComFrame(t *testing.T) {
	got := string(buildMeComFrame(0x4c, 0x35, "?VM041501"))
	prefix := "#4C0035?VM041501"
	want := fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator)
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestParseMeComResponse(t *testing.T) {
	prefix := "!4C003500010400000001FF8000007F80000000000000"
	raw := []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))

	resp, err := parseMeComResponse(raw, nil)
	if err != nil {
		t.Fatalf("parseMeComResponse returned error: %v", err)
	}
	if resp.Address != 0x4c || resp.Sequence != 0x35 || resp.Status != "data" {
		t.Fatalf("response envelope = %+v", resp)
	}
	if resp.Payload != "00010400000001FF8000007F80000000000000" {
		t.Fatalf("Payload = %q", resp.Payload)
	}
	meta, ok := decodeMetadataPayload(resp.Payload)
	if !ok {
		t.Fatal("decodeMetadataPayload returned ok=false")
	}
	if meta.Type != "FLOAT32" || meta.Flags != 1 || meta.MaxInstances != 4 || meta.MaxElements != 1 {
		t.Fatalf("metadata = %+v", meta)
	}
}

func TestParseMeComResponseRejectsMismatchedEnvelope(t *testing.T) {
	request := buildMeComFrame(0x4b, 0x4b, "?VI")
	prefix := "!4C004C027802E8"
	raw := []byte(fmt.Sprintf("%s%04X%c", prefix, mecom.CRC16([]byte(prefix)), mecom.FrameTerminator))

	if _, err := parseMeComResponse(raw, request); err == nil || !strings.Contains(err.Error(), "address/sequence mismatch") {
		t.Fatalf("parseMeComResponse error = %v, want mismatch", err)
	}
}

func TestParseMeComResponseAcceptsBareRequestCRCAck(t *testing.T) {
	request := buildMeComFrame(0, 1, "VS08160100000038")
	raw := []byte(fmt.Sprintf("!000001%s\r", string(request[len(request)-5:len(request)-1])))

	resp, err := parseMeComResponse(raw, request)
	if err != nil {
		t.Fatalf("parseMeComResponse returned error: %v", err)
	}
	if resp.Address != 0 || resp.Sequence != 1 || resp.Status != "ok" || resp.Payload != "" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestParseMeComResponseRejectsUnmatchedBareRequestCRCAck(t *testing.T) {
	request := buildMeComFrame(0, 1, "VS08160100000038")
	raw := []byte("!0000010000\r")

	if _, err := parseMeComResponse(raw, request); err == nil {
		t.Fatal("parseMeComResponse accepted unmatched bare ACK")
	}
}
