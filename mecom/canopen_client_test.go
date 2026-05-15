package mecom

import (
	"context"
	"encoding/binary"
	"errors"
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

func TestCANopenClientWriteFloat32SetsNominalTarget(t *testing.T) {
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x60, 0x00, 0x26, 0x01, 0, 0, 0, 0}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	if err := client.WriteFloat32(context.Background(), 3000, 1, 25.0); err != nil {
		t.Fatalf("WriteFloat32: %v", err)
	}
	if len(fake.sent) != 1 {
		t.Fatalf("sent=%d, want 1", len(fake.sent))
	}
	sent := fake.sent[0]
	if sent.ID != 0x64b {
		t.Fatalf("sent ID=0x%X, want 0x64B", sent.ID)
	}
	if sent.Data[0] != 0x23 {
		t.Fatalf("cmd byte=0x%02X, want 0x23", sent.Data[0])
	}
	if got := uint16(sent.Data[1]) | uint16(sent.Data[2])<<8; got != 0x2600 {
		t.Fatalf("index=0x%04X, want 0x2600", got)
	}
	if sent.Data[3] != 0x01 {
		t.Fatalf("subIndex=0x%02X, want 0x01", sent.Data[3])
	}
	gotValue := math.Float32frombits(binary.LittleEndian.Uint32(sent.Data[4:8]))
	if math.Abs(float64(gotValue)-25.0) > 0.001 {
		t.Fatalf("payload=%f, want 25.0", gotValue)
	}
}

func TestCANopenClientWriteRejectsReadOnlyParam(t *testing.T) {
	fake := &fakeCANTransceiver{}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	err := client.WriteFloat32(context.Background(), 1000, 1, 25.0)
	if err == nil {
		t.Fatal("expected error writing read-only param 1000")
	}
	if !errors.Is(err, ErrParameterReadOnly) {
		t.Fatalf("error = %v, want ErrParameterReadOnly", err)
	}
	if len(fake.sent) != 0 {
		t.Fatalf("sent=%d frames; should not transmit for read-only param", len(fake.sent))
	}
}

func TestCANopenClientWriteRejectsUnknownParam(t *testing.T) {
	fake := &fakeCANTransceiver{}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	err := client.WriteFloat32(context.Background(), 999999, 1, 25.0)
	if err == nil {
		t.Fatal("expected error writing unmapped param")
	}
	if !errors.Is(err, ErrUnknownParameter) {
		t.Fatalf("error = %v, want ErrUnknownParameter", err)
	}
	if len(fake.sent) != 0 {
		t.Fatalf("sent=%d frames; should not transmit for unmapped param", len(fake.sent))
	}
}

func TestCANopenClientWriteWrapsSDOAbort(t *testing.T) {
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x80, 0x00, 0x26, 0x01, 0x00, 0x00, 0x02, 0x06}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	err := client.WriteFloat32(context.Background(), 3000, 1, 25.0)
	if err == nil {
		t.Fatal("expected SDO abort")
	}
	if !errors.Is(err, ErrWriteRejected) {
		t.Fatalf("error = %v, want ErrWriteRejected", err)
	}
	var abort canopen.SDOAbortError
	if !errors.As(err, &abort) {
		t.Fatalf("error = %v, want SDOAbortError", err)
	}
}

func TestCANopenClientRingCaptureUnsupported(t *testing.T) {
	client := NewCANopenClient(&fakeCANTransceiver{}, ClientConfig{Address: 0x4b, Timeout: time.Second})
	if err := client.ConfigureRingCapture(context.Background(), 0, nil); !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("ConfigureRingCapture error = %v, want ErrTransportNotSupported", err)
	}
	if err := client.TriggerRingSync(context.Background()); !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("TriggerRingSync error = %v, want ErrTransportNotSupported", err)
	}
	if _, err := client.ReadRingPointer(context.Background()); !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("ReadRingPointer error = %v, want ErrTransportNotSupported", err)
	}
	if _, err := client.ReadRingChunk(context.Background(), 0, 1); !errors.Is(err, ErrTransportNotSupported) {
		t.Fatalf("ReadRingChunk error = %v, want ErrTransportNotSupported", err)
	}
}
