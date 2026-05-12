package canopen

import (
	"errors"
	"math"
	"testing"
)

func TestSDOUploadRequest(t *testing.T) {
	frame := SDOUploadRequest(0x2A, 0x3000, 0x01)
	if frame.ID != 0x62A {
		t.Fatalf("id = 0x%X", frame.ID)
	}
	want := [4]byte{0x40, 0x00, 0x30, 0x01}
	for i, v := range want {
		if frame.Data[i] != v {
			t.Fatalf("data[%d] = 0x%X, want 0x%X", i, frame.Data[i], v)
		}
	}
	if err := frame.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSDODownloadExpeditedRequest(t *testing.T) {
	frame, err := SDODownloadExpeditedRequest(0x2A, 0x3000, 0x01, []byte{0x00, 0x00, 0xCC, 0x41})
	if err != nil {
		t.Fatal(err)
	}
	if frame.Data[0] != 0x23 {
		t.Fatalf("command = 0x%X", frame.Data[0])
	}
	if frame.Data[4] != 0x00 || frame.Data[7] != 0x41 {
		t.Fatalf("value bytes = % X", frame.Data[4:8])
	}
}

func TestParseSDOUploadResponseUint32(t *testing.T) {
	resp, err := ParseSDOUploadResponse(Frame{
		ID:   0x5CB,
		DLC:  8,
		Data: [8]byte{0x43, 0x18, 0x10, 0x01, 0x47, 0x05, 0x00, 0x00},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NodeID != 0x4B || resp.Index != 0x1018 || resp.SubIndex != 0x01 {
		t.Fatalf("decoded target = node 0x%02X 0x%04X:%02X", resp.NodeID, resp.Index, resp.SubIndex)
	}
	v, err := resp.Uint32()
	if err != nil {
		t.Fatal(err)
	}
	if v != 0x00000547 {
		t.Fatalf("value = 0x%X, want 0x547", v)
	}
}

func TestParseSDOUploadResponseFloat32(t *testing.T) {
	bits := math.Float32bits(25.5)
	resp, err := ParseSDOUploadResponse(Frame{
		ID:  0x5D1,
		DLC: 8,
		Data: [8]byte{
			0x43, 0x00, 0x21, 0x01,
			byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	v, err := resp.Float32()
	if err != nil {
		t.Fatal(err)
	}
	if v != 25.5 {
		t.Fatalf("value = %f, want 25.5", v)
	}
}

func TestParseSDOUploadResponseAbort(t *testing.T) {
	_, err := ParseSDOUploadResponse(Frame{
		ID:   0x5D1,
		DLC:  8,
		Data: [8]byte{0x80, 0x00, 0x21, 0x01, 0x00, 0x00, 0x02, 0x06},
	})
	var abort SDOAbortError
	if !errors.As(err, &abort) {
		t.Fatalf("err = %v, want SDOAbortError", err)
	}
	if abort.Code != 0x06020000 {
		t.Fatalf("abort code = 0x%08X", abort.Code)
	}
}
