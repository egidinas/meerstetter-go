package mecom

import (
	"math"
	"testing"

	"github.com/egidinas/meerstetter-go/canopen"
)

func TestDecodeBinaryCANFrame(t *testing.T) {
	// Test case 1: Valid float32 response (!VR)
	// Temp 25.5 degC = 0x41CC0000 in bits
	f := canopen.Frame{
		ID:  0x401, // Response from address 1
		DLC: 8,
		Data: [8]byte{
			0x81, // Control: Response bit set, Seq=1
			0x01, // Address 1
			0x00, 0x01, // Command: !VR
			0x41, 0xCC, 0x00, 0x00, // Value: 25.5
		},
	}
	val, err := DecodeBinaryCANFrame(f, DataTypeFloat32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(val-25.5) > 0.001 {
		t.Errorf("got %f, want 25.5", val)
	}

	// Test case 2: Valid int32 response
	f.Data[4] = 0x00
	f.Data[5] = 0x00
	f.Data[6] = 0x00
	f.Data[7] = 0x01 // Value: 1
	val, err = DecodeBinaryCANFrame(f, DataTypeInt32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 1.0 {
		t.Errorf("got %f, want 1.0", val)
	}

	// Test case 3: Binary NACK (Error response)
	f.Data[2] = 0x00
	f.Data[3] = 0x06 // Command: ResponseError
	f.Data[4] = 0x05 // Code 5: PAR_NOT_AVAILABLE
	_, err = DecodeBinaryCANFrame(f, DataTypeFloat32)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "mecom: binary nack 05 (PAR_NOT_AVAILABLE)" {
		t.Errorf("unexpected error message: %v", err)
	}
}
