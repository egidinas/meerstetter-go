package mecom

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/canopen"
)

type fakeCANTransceiver struct {
	sent    []canopen.Frame
	replies []canopen.Frame
}

func (f *fakeCANTransceiver) Send(frame canopen.Frame) error {
	f.sent = append(f.sent, frame)
	return nil
}

func (f *fakeCANTransceiver) Recv(time.Duration) (canopen.Frame, error) {
	if len(f.replies) == 0 {
		return canopen.Frame{}, context.DeadlineExceeded
	}
	frame := f.replies[0]
	f.replies = f.replies[1:]
	return frame, nil
}

func TestCANClientReadFloat32SkipsEcho(t *testing.T) {
	var value [4]byte
	binary.BigEndian.PutUint32(value[:], math.Float32bits(25.5))
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x31f, DLC: 8, Data: [8]byte{0x01, 0x1f, 0x00, 0x01, 0x03, 0xe8, 0x01}},
			{ID: 0x41f, DLC: 8, Data: [8]byte{0x81, 0x1f, 0x00, 0x01, value[0], value[1], value[2], value[3]}},
		},
	}
	client := NewCANClient(fake, ClientConfig{Address: 0x1f, Timeout: time.Second})
	got, err := client.ReadFloat32(context.Background(), 1000, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if math.Abs(got-25.5) > 0.001 {
		t.Fatalf("ReadFloat32=%f, want 25.5", got)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("sent frames=%d, want 1", len(fake.sent))
	}
	sent := fake.sent[0]
	if sent.ID != 0x31f {
		t.Fatalf("sent ID=0x%X, want 0x31F", sent.ID)
	}
	want := BuildBinarySingleGetFrame(0x1f, 1, 1000, 1)
	for i := range want {
		if sent.Data[i] != want[i] {
			t.Fatalf("sent byte %d=0x%02X, want 0x%02X", i, sent.Data[i], want[i])
		}
	}
}
