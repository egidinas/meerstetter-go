package canopen

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestSDOUploadRequest(t *testing.T) {
	frame, err := SDOUploadRequest(0x2A, 0x3000, 0x01)
	if err != nil {
		t.Fatal(err)
	}
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

func TestSDOUploadRequestRejectsInvalidNodeID(t *testing.T) {
	for _, nodeID := range []byte{0, 0x80, 0xff} {
		t.Run(fmt.Sprintf("0x%02X", nodeID), func(t *testing.T) {
			if _, err := SDOUploadRequest(nodeID, 0x3000, 0x01); err == nil {
				t.Fatal("expected invalid node id error")
			}
		})
	}
}

func TestSDOUploadRequestAcceptsBoundaryNodeID(t *testing.T) {
	for _, nodeID := range []byte{1, 0x7f} {
		t.Run(fmt.Sprintf("0x%02X", nodeID), func(t *testing.T) {
			frame, err := SDOUploadRequest(nodeID, 0x3000, 0x01)
			if err != nil {
				t.Fatal(err)
			}
			if frame.ID != 0x600+uint32(nodeID) {
				t.Fatalf("id = 0x%X", frame.ID)
			}
		})
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

func TestNMTFrame(t *testing.T) {
	frame, err := NMTFrame(NMTEnterPreOperational, 0x4B)
	if err != nil {
		t.Fatal(err)
	}
	if frame.ID != 0 || frame.DLC != 2 {
		t.Fatalf("NMT frame id/dlc = 0x%X/%d, want 0/2", frame.ID, frame.DLC)
	}
	want := [8]byte{0x80, 0x4B}
	if frame.Data != want {
		t.Fatalf("NMT data = % X, want % X", frame.Data, want)
	}
}

func TestNMTFrameAcceptsBroadcastAndRejectsInvalidNode(t *testing.T) {
	frame, err := NMTFrame(NMTStartRemoteNode, 0)
	if err != nil {
		t.Fatalf("broadcast NMT should be accepted: %v", err)
	}
	if frame.Data[0] != 0x01 || frame.Data[1] != 0x00 {
		t.Fatalf("broadcast NMT data = % X", frame.Data)
	}
	if _, err := NMTFrame(NMTStartRemoteNode, 0x80); err == nil {
		t.Fatal("expected invalid node id error")
	}
}

func TestSDODownloadExpeditedRequestRejectsInvalidNodeID(t *testing.T) {
	for _, nodeID := range []byte{0, 0x80, 0xff} {
		t.Run(fmt.Sprintf("0x%02X", nodeID), func(t *testing.T) {
			if _, err := SDODownloadExpeditedRequest(nodeID, 0x3000, 0x01, []byte{0x01}); err == nil {
				t.Fatal("expected invalid node id error")
			}
		})
	}
}

func TestSDODownloadExpeditedRequestAcceptsBoundaryNodeID(t *testing.T) {
	for _, nodeID := range []byte{1, 0x7f} {
		t.Run(fmt.Sprintf("0x%02X", nodeID), func(t *testing.T) {
			frame, err := SDODownloadExpeditedRequest(nodeID, 0x3000, 0x01, []byte{0x01})
			if err != nil {
				t.Fatal(err)
			}
			if frame.ID != 0x600+uint32(nodeID) {
				t.Fatalf("id = 0x%X", frame.ID)
			}
		})
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

func TestParseSDODownloadResponseOK(t *testing.T) {
	resp, err := ParseSDODownloadResponse(Frame{
		ID:   0x5CB,
		DLC:  8,
		Data: [8]byte{0x60, 0x00, 0x30, 0x01, 0, 0, 0, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NodeID != 0x4B || resp.Index != 0x3000 || resp.SubIndex != 0x01 {
		t.Fatalf("decoded target = node 0x%02X 0x%04X:%02X", resp.NodeID, resp.Index, resp.SubIndex)
	}
}

func TestParseSDODownloadResponseAbort(t *testing.T) {
	_, err := ParseSDODownloadResponse(Frame{
		ID:   0x5CB,
		DLC:  8,
		Data: [8]byte{0x80, 0x00, 0x30, 0x01, 0x00, 0x00, 0x02, 0x06},
	})
	var abort SDOAbortError
	if !errors.As(err, &abort) {
		t.Fatalf("err = %v, want SDOAbortError", err)
	}
	if abort.Index != 0x3000 || abort.SubIndex != 0x01 || abort.Code != 0x06020000 {
		t.Fatalf("abort = 0x%04X:%02X 0x%08X", abort.Index, abort.SubIndex, abort.Code)
	}
}
