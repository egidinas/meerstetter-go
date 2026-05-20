package mecom

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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

func TestCANopenClientReadFloat32SkipsUnrelatedSDOAbort(t *testing.T) {
	var value [4]byte
	binary.LittleEndian.PutUint32(value[:], math.Float32bits(24.25))
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x80, 0x99, 0x99, 0x01, 0x00, 0x00, 0x02, 0x06}},
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

func TestCANopenClientWriteFloat32SkipsUnrelatedSDOAbort(t *testing.T) {
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x80, 0x99, 0x99, 0x01, 0x00, 0x00, 0x02, 0x06}},
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x60, 0x00, 0x26, 0x01, 0, 0, 0, 0}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	if err := client.WriteFloat32(context.Background(), 3000, 1, 25.0); err != nil {
		t.Fatalf("WriteFloat32 returned error: %v", err)
	}
}

func TestCANopenClientWriteInt32SetsCascadeEnable(t *testing.T) {
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{
			{ID: 0x5cb, DLC: 8, Data: [8]byte{0x60, 0x20, 0x44, 0x01, 0, 0, 0, 0}},
		},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	if err := client.WriteInt32(context.Background(), 53120, 1, 1); err != nil {
		t.Fatalf("WriteInt32: %v", err)
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
	if got := uint16(sent.Data[1]) | uint16(sent.Data[2])<<8; got != 0x4420 {
		t.Fatalf("index=0x%04X, want 0x4420", got)
	}
	if sent.Data[3] != 0x01 {
		t.Fatalf("subIndex=0x%02X, want 0x01", sent.Data[3])
	}
	gotValue := int32(binary.LittleEndian.Uint32(sent.Data[4:8]))
	if gotValue != 1 {
		t.Fatalf("payload=%d, want 1", gotValue)
	}
}

func TestCANopenClientReadsOutputStageActuals(t *testing.T) {
	tests := []struct {
		name      string
		paramID   int
		instance  int
		wantIndex uint16
		wantSub   byte
		wantValue float32
	}{
		{name: "current", paramID: 1020, instance: 2, wantIndex: 0x2120, wantSub: 0x02, wantValue: 0.125},
		{name: "voltage", paramID: 1021, instance: 2, wantIndex: 0x2121, wantSub: 0x02, wantValue: 28.0},
		{name: "power", paramID: 1022, instance: 2, wantIndex: 0x2122, wantSub: 0x02, wantValue: 3.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCANTransceiver{
				replies: []canopen.Frame{sdoFloatUploadReply(0x4b, tt.wantIndex, tt.wantSub, tt.wantValue)},
			}
			client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
			got, err := client.ReadFloat32(context.Background(), tt.paramID, tt.instance)
			if err != nil {
				t.Fatalf("ReadFloat32 returned error: %v", err)
			}
			if math.Abs(got-float64(tt.wantValue)) > 0.001 {
				t.Fatalf("ReadFloat32=%f, want %f", got, tt.wantValue)
			}
			assertSDOUploadRequest(t, fake.sent, 0x4b, tt.wantIndex, tt.wantSub)
		})
	}
}

func TestCANopenClientReadsChannelSpecificObjects(t *testing.T) {
	tests := []struct {
		name      string
		paramID   int
		instance  int
		wantIndex uint16
		wantSub   byte
		wantValue float32
	}{
		{name: "channel 1 object temperature", paramID: 1000, instance: 1, wantIndex: 0x2100, wantSub: 0x01, wantValue: 31.25},
		{name: "channel 3 object temperature", paramID: 1000, instance: 3, wantIndex: 0x2100, wantSub: 0x03, wantValue: 32.5},
		{name: "channel 2 output power", paramID: 1022, instance: 2, wantIndex: 0x2122, wantSub: 0x02, wantValue: 0.007},
		{name: "channel 4 output power", paramID: 1022, instance: 4, wantIndex: 0x2122, wantSub: 0x04, wantValue: 0.011},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCANTransceiver{
				replies: []canopen.Frame{sdoFloatUploadReply(0x4b, tt.wantIndex, tt.wantSub, tt.wantValue)},
			}
			client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
			got, err := client.ReadFloat32(context.Background(), tt.paramID, tt.instance)
			if err != nil {
				t.Fatalf("ReadFloat32 returned error: %v", err)
			}
			if math.Abs(got-float64(tt.wantValue)) > 0.001 {
				t.Fatalf("ReadFloat32=%f, want %f", got, tt.wantValue)
			}
			assertSDOUploadRequest(t, fake.sent, 0x4b, tt.wantIndex, tt.wantSub)
		})
	}
}

func TestCANopenClientRejectsInvalidInstanceWithoutChannelOneAlias(t *testing.T) {
	for _, tt := range []struct {
		name     string
		instance int
	}{
		{name: "zero", instance: 0},
		{name: "negative", instance: -1},
		{name: "too large", instance: 256},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCANTransceiver{}
			client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
			_, err := client.ReadFloat32(context.Background(), 1000, tt.instance)
			if err == nil {
				t.Fatalf("expected invalid instance %d to be rejected", tt.instance)
			}
			if !errors.Is(err, ErrUnknownParameter) {
				t.Fatalf("error = %v, want ErrUnknownParameter", err)
			}
			if len(fake.sent) != 0 {
				t.Fatalf("sent=%d frames; invalid instance must not alias to channel 1", len(fake.sent))
			}
		})
	}
}

func TestCANopenClientWritesOutputStageSetpointsAndLimits(t *testing.T) {
	tests := []struct {
		name      string
		paramID   int
		instance  int
		value     float32
		wantIndex uint16
		wantSub   byte
	}{
		{name: "fixed current", paramID: 2020, instance: 2, value: 0.75, wantIndex: 0x2420, wantSub: 0x02},
		{name: "fixed voltage", paramID: 2021, instance: 2, value: 28.0, wantIndex: 0x2421, wantSub: 0x02},
		{name: "current limitation", paramID: 2030, instance: 2, value: 1.5, wantIndex: 0x2430, wantSub: 0x02},
		{name: "voltage limitation", paramID: 2031, instance: 2, value: 30.0, wantIndex: 0x2431, wantSub: 0x02},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeCANTransceiver{
				replies: []canopen.Frame{sdoDownloadReply(0x4b, tt.wantIndex, tt.wantSub)},
			}
			client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
			if err := client.WriteFloat32(context.Background(), tt.paramID, tt.instance, tt.value); err != nil {
				t.Fatalf("WriteFloat32 returned error: %v", err)
			}
			assertSDOFloatDownload(t, fake.sent, 0x4b, tt.wantIndex, tt.wantSub, tt.value)
		})
	}
}

func TestCANopenClientWriteRejectsOutputStageActuals(t *testing.T) {
	for _, paramID := range []int{1020, 1021, 1022} {
		t.Run(fmt.Sprintf("%d", paramID), func(t *testing.T) {
			fake := &fakeCANTransceiver{}
			client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
			err := client.WriteFloat32(context.Background(), paramID, 2, 28.0)
			if err == nil {
				t.Fatalf("expected error writing read-only param %d", paramID)
			}
			if !errors.Is(err, ErrParameterReadOnly) {
				t.Fatalf("error = %v, want ErrParameterReadOnly", err)
			}
			if len(fake.sent) != 0 {
				t.Fatalf("sent=%d frames; should not transmit for read-only param", len(fake.sent))
			}
		})
	}
}

func TestCANopenClientWritesCascadeTarget(t *testing.T) {
	fake := &fakeCANTransceiver{
		replies: []canopen.Frame{sdoDownloadReply(0x4b, 0x4423, 0x01)},
	}
	client := NewCANopenClient(fake, ClientConfig{Address: 0x4b, Timeout: time.Second})
	if err := client.WriteFloat32(context.Background(), 53123, 1, 25.0); err != nil {
		t.Fatalf("WriteFloat32 returned error: %v", err)
	}
	assertSDOFloatDownload(t, fake.sent, 0x4b, 0x4423, 0x01, 25.0)
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

func sdoFloatUploadReply(node byte, index uint16, subIndex byte, value float32) canopen.Frame {
	var data [8]byte
	binary.LittleEndian.PutUint32(data[4:8], math.Float32bits(value))
	data[0] = 0x43
	data[1] = byte(index)
	data[2] = byte(index >> 8)
	data[3] = subIndex
	return canopen.Frame{ID: 0x580 + uint32(node), DLC: 8, Data: data}
}

func sdoDownloadReply(node byte, index uint16, subIndex byte) canopen.Frame {
	return canopen.Frame{
		ID:   0x580 + uint32(node),
		DLC:  8,
		Data: [8]byte{0x60, byte(index), byte(index >> 8), subIndex, 0, 0, 0, 0},
	}
}

func assertSDOUploadRequest(t *testing.T, sent []canopen.Frame, node byte, wantIndex uint16, wantSub byte) {
	t.Helper()
	if len(sent) != 1 {
		t.Fatalf("sent=%d, want 1", len(sent))
	}
	frame := sent[0]
	if frame.ID != 0x600+uint32(node) {
		t.Fatalf("sent ID=0x%X, want 0x%X", frame.ID, 0x600+uint32(node))
	}
	want := [8]byte{0x40, byte(wantIndex), byte(wantIndex >> 8), wantSub, 0, 0, 0, 0}
	if frame.Data != want {
		t.Fatalf("sent data=% X, want % X", frame.Data, want)
	}
}

func assertSDOFloatDownload(t *testing.T, sent []canopen.Frame, node byte, wantIndex uint16, wantSub byte, wantValue float32) {
	t.Helper()
	if len(sent) != 1 {
		t.Fatalf("sent=%d, want 1", len(sent))
	}
	frame := sent[0]
	if frame.ID != 0x600+uint32(node) {
		t.Fatalf("sent ID=0x%X, want 0x%X", frame.ID, 0x600+uint32(node))
	}
	if frame.Data[0] != 0x23 {
		t.Fatalf("cmd byte=0x%02X, want 0x23", frame.Data[0])
	}
	if got := uint16(frame.Data[1]) | uint16(frame.Data[2])<<8; got != wantIndex {
		t.Fatalf("index=0x%04X, want 0x%04X", got, wantIndex)
	}
	if frame.Data[3] != wantSub {
		t.Fatalf("subIndex=0x%02X, want 0x%02X", frame.Data[3], wantSub)
	}
	gotValue := math.Float32frombits(binary.LittleEndian.Uint32(frame.Data[4:8]))
	if math.Abs(float64(gotValue-wantValue)) > 0.001 {
		t.Fatalf("payload=%f, want %f", gotValue, wantValue)
	}
}
