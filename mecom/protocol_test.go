package mecom

import (
	"bytes"
	"context"
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
