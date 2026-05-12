package mecom

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func TestBuildSingleGetFrameFormat(t *testing.T) {
	frame := BuildSingleGetFrame(0x50, 1, 1000, 1)
	s := string(frame)
	if !strings.HasPrefix(s, "#") {
		t.Fatalf("frame must start with #: %q", s)
	}
	if s[len(s)-1] != '\r' {
		t.Fatalf("frame must end with CR: %q", s)
	}
	if !strings.Contains(s, "?VR03E801") {
		t.Fatalf("frame missing ?VR03E801: %q", s)
	}
	if crcHex := s[len(s)-5 : len(s)-1]; crcHex == "0000" {
		t.Fatalf("crc appears zero: %q", s)
	}
}

func TestReferenceCRCVector(t *testing.T) {
	payload := []byte("#0015AB?VR03E801")
	if got := CRC16(payload); got != 0xC21A {
		t.Fatalf("CRC16(%q) = %04X, want C21A", payload, got)
	}
}

func TestBuildSingleGetFrameReferenceVector(t *testing.T) {
	got := string(BuildSingleGetFrame(0x00, 0x15AB, 1000, 1))
	const want = "#0015AB?VR03E801C21A\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildSaveToFlashFrameReferenceVector(t *testing.T) {
	got := string(BuildSaveToFlashFrame(0x00, 0x0001))
	const want = "#000001SP69EE\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildResetFrameReferenceVector(t *testing.T) {
	got := string(BuildResetFrame(0x00, 0x0002))
	const want = "#000002RS33EC\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestParseSingleResponseFloat(t *testing.T) {
	got, err := ParseSingleResponse([]byte("!500001+41CC0000ABCD\r"), DataTypeFloat32)
	if err != nil {
		t.Fatal(err)
	}
	if got != 25.5 {
		t.Fatalf("value = %v", got)
	}
}

func TestParseSingleResponseInt(t *testing.T) {
	got, err := ParseSingleResponse([]byte("!500001+FFFFFFFEABCD\r"), DataTypeInt32)
	if err != nil {
		t.Fatal(err)
	}
	if got != -2 {
		t.Fatalf("value = %v", got)
	}
}

func TestParseNACK(t *testing.T) {
	_, err := ParseSingleResponse([]byte("!500001-05ABCD\r"), DataTypeFloat32)
	if err == nil || !strings.Contains(err.Error(), "PAR_NOT_AVAILABLE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReadFloat32(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBufferString("!500001+41CC0000ABCD\r")}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
	got, err := client.ReadFloat32(context.Background(), 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 25.5 {
		t.Fatalf("value = %v", got)
	}
	if !strings.Contains(rw.written.String(), "?VR03E801") {
		t.Fatalf("request = %q", rw.written.String())
	}
}

func TestBuildBulkGetFrameFormat(t *testing.T) {
	got := string(BuildBulkGetFrame(0x50, 1, []Parameter{
		{ID: 1000, Instance: 1},
		{ID: 1022, Instance: 4},
	}))
	if !strings.Contains(got, "?VX0203E80103FE04") {
		t.Fatalf("bulk frame = %q", got)
	}
}

func TestClientReadBulk(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBufferString("!50000141CC00000000002AABCD\r")}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
	got, err := client.ReadBulk(context.Background(), []Parameter{
		{ID: 1000, Instance: 1, Type: DataTypeFloat32},
		{ID: 2000, Instance: 1, Type: DataTypeInt32},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 25.5 || got[1] != 42 {
		t.Fatalf("values = %#v", got)
	}
	if !strings.Contains(rw.written.String(), "?VX0203E80107D001") {
		t.Fatalf("request = %q", rw.written.String())
	}
}

func TestBuildRingPointerFrameReferenceVector(t *testing.T) {
	got := string(BuildRingPointerFrame(0x00, 0x8532))
	const want = "#008532?RS00005984\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildRingReadFrameReferenceVector(t *testing.T) {
	got := string(BuildRingReadFrame(0x00, 0x8564, 0x00000180, 0xFFFF))
	const want = "#008564?RS000100000180FFFF7FFA\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestParseRingPointerResponse(t *testing.T) {
	got, err := ParseRingPointerResponse([]byte("!00853200000180110C\r"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 384 {
		t.Fatalf("pointer = %d", got)
	}
}

func TestParseRingReadResponseAndFrames(t *testing.T) {
	resp, err := ParseRingReadResponse([]byte("!0085640006008800EF3E8810F58E\r"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.BytesAdded != 6 || resp.Status != RingStatusAllDataRead {
		t.Fatalf("response = %#v", resp)
	}
	frames, tail, err := ParseRingFrames(resp.Data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d tail=% X", len(frames), tail)
	}
	if frames[0].Timestamp10us != 0x3EEF || len(frames[0].Samples) != 0 {
		t.Fatalf("frame = %#v", frames[0])
	}
}

func TestParseRingFramesWithFloatAndIntSamples(t *testing.T) {
	raw := []byte{0x88, 0x00, 0x34, 0x12, 0x00, 0xD0, 0x83, 0xDA, 0x41, 0x85, 0x02, 0x39, 0x30, 0x00, 0x00, 0x88, 0x10}
	config := []RingCaptureParameter{
		{Parameter: Parameter{Type: DataTypeFloat32}},
		{}, {}, {}, {},
		{Parameter: Parameter{Type: DataTypeInt32}},
	}
	frames, tail, err := ParseRingFrames(raw, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d tail=% X", len(frames), tail)
	}
	frame := frames[0]
	if frame.Timestamp10us != 0x1234 || len(frame.Samples) != 2 {
		t.Fatalf("frame = %#v", frame)
	}
	if math.Abs(frame.Samples[0].Value-27.314362) > 0.00001 {
		t.Fatalf("float sample = %v", frame.Samples[0].Value)
	}
	if frame.Samples[1].ConfigIndex != 5 || frame.Samples[1].Value != 12345 {
		t.Fatalf("int sample = %#v", frame.Samples[1])
	}
}

type scriptedReadWriter struct {
	read    *bytes.Buffer
	written bytes.Buffer
}

func (s *scriptedReadWriter) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *scriptedReadWriter) Write(p []byte) (int, error) {
	return s.written.Write(p)
}
