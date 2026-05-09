package canopen

import "testing"

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
