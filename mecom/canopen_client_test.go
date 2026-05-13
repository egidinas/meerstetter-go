package mecom

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

func TestCANopenClientReadFloat32(t *testing.T) {
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], math.Float32bits(24.25))
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cc, DLC: 8, Data: [8]byte{0x43, 0x00, 0x21, 0x01, value[0], value[1], value[2], value[3]}},
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x43, 0x00, 0x21, 0x01, value[0], value[1], value[2], value[3]}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	got, err := client.ReadFloat32(context.Background(), 1000, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if math.Abs(got-24.25) > 0.001 {
		t.Fatalf("ReadFloat32=%f, want 24.25", got)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("sent frames=%d, want 1", len(fake.sent))
	}
	sent := fake.sent[0]
	if sent.ID != 0x64b {
		t.Fatalf("sent ID=0x%X, want 0x64B", sent.ID)
	}
	want := [8]byte{0x40, 0x00, 0x21, 0x01, 0, 0, 0, 0}
	if sent.Data != want {
		t.Fatalf("sent data=% X, want % X", sent.Data, want)
	}
}

func TestCANopenClientReadBulkKeepsUnsupportedSlots(t *testing.T) {
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], math.Float32bits(11.5))
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x43, 0x00, 0x21, 0x01, value[0], value[1], value[2], value[3]}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	values, err := client.ReadBulk(context.Background(), []Parameter{
		{ID: 1000, Instance: 1},
		{ID: 52200, Instance: 1},
	})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("ReadBulk returned %d values, want 2", len(values))
	}
	if math.Abs(values[0]-11.5) > 0.001 {
		t.Fatalf("values[0]=%f, want 11.5", values[0])
	}
	if !math.IsNaN(values[1]) {
		t.Fatalf("values[1]=%f, want NaN for unsupported parameter", values[1])
	}
}
