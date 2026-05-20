package mecomserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

type fakeParamKey struct {
	id       int
	instance int
}

type fakeFloatWrite struct {
	key   fakeParamKey
	value float32
}

type fakeIntWrite struct {
	key   fakeParamKey
	value int32
}

type fakeStringWrite struct {
	key   fakeParamKey
	value string
}

func deviceBridgeTestFrame(addr byte, seq uint16, payload string) []byte {
	body := []byte(fmt.Sprintf("#%02X%04X%s", addr, seq, payload))
	return []byte(fmt.Sprintf("%s%04X%c", body, mecom.CRC16(body), mecom.FrameTerminator))
}

func deviceBridgeInfoTestFrame(addr byte, seq uint16, payload string) []byte {
	body := []byte(fmt.Sprintf("!%02X%04X%s", addr, seq, payload))
	return []byte(fmt.Sprintf("%s%04X%c", body, mecom.CRC16(body), mecom.FrameTerminator))
}

type fakeDeviceClient struct {
	floats map[fakeParamKey]float64
	ints   map[fakeParamKey]int32

	readFloats  []fakeParamKey
	readInts    []fakeParamKey
	bulkReads   [][]mecom.Parameter
	floatWrites []fakeFloatWrite
	intWrites   []fakeIntWrite
	stringWrites []fakeStringWrite
	bigDataWrites []fakeStringWrite
	closed      bool
}

func (f *fakeDeviceClient) ReadFloat32(_ context.Context, id, instance int) (float64, error) {
	key := fakeParamKey{id: id, instance: instance}
	f.readFloats = append(f.readFloats, key)
	return f.floats[key], nil
}

func (f *fakeDeviceClient) ReadInt32(_ context.Context, id, instance int) (int32, error) {
	key := fakeParamKey{id: id, instance: instance}
	f.readInts = append(f.readInts, key)
	return f.ints[key], nil
}

func (f *fakeDeviceClient) ReadBulk(_ context.Context, params []mecom.Parameter) ([]float64, error) {
	f.bulkReads = append(f.bulkReads, append([]mecom.Parameter(nil), params...))
	values := make([]float64, len(params))
	for i, p := range params {
		key := fakeParamKey{id: p.ID, instance: p.Instance}
		switch p.Type {
		case mecom.DataTypeInt32:
			values[i] = float64(f.ints[key])
		case mecom.DataTypeFloat32, "":
			values[i] = f.floats[key]
		default:
			values[i] = math.NaN()
		}
	}
	return values, nil
}

func (f *fakeDeviceClient) ConfigureRingCapture(context.Context, uint16, []mecom.RingCaptureParameter) error {
	return mecom.ErrTransportNotSupported
}

func (f *fakeDeviceClient) TriggerRingSync(context.Context) error {
	return mecom.ErrTransportNotSupported
}

func (f *fakeDeviceClient) ReadRingPointer(context.Context) (uint32, error) {
	return 0, mecom.ErrTransportNotSupported
}

func (f *fakeDeviceClient) ReadRingChunk(context.Context, uint32, uint16) (mecom.RingReadResponse, error) {
	return mecom.RingReadResponse{}, mecom.ErrTransportNotSupported
}

func (f *fakeDeviceClient) WriteFloat32(_ context.Context, id, instance int, value float32) error {
	f.floatWrites = append(f.floatWrites, fakeFloatWrite{key: fakeParamKey{id: id, instance: instance}, value: value})
	return nil
}

func (f *fakeDeviceClient) WriteInt32(_ context.Context, id, instance int, value int32) error {
	f.intWrites = append(f.intWrites, fakeIntWrite{key: fakeParamKey{id: id, instance: instance}, value: value})
	return nil
}

func (f *fakeDeviceClient) WriteString(_ context.Context, id, instance int, value string) error {
	f.stringWrites = append(f.stringWrites, fakeStringWrite{key: fakeParamKey{id: id, instance: instance}, value: value})
	return nil
}

func (f *fakeDeviceClient) WriteBigDataString(_ context.Context, id, instance int, value string) error {
	f.bigDataWrites = append(f.bigDataWrites, fakeStringWrite{key: fakeParamKey{id: id, instance: instance}, value: value})
	return nil
}

func (f *fakeDeviceClient) Close() error {
	f.closed = true
	return nil
}

func TestDeviceClientBridgeTranslatesSingleRead(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: 12.5},
		ints:   map[fakeParamKey]int32{},
	}
	conn, desc, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()
	if desc != "fake-can" {
		t.Fatalf("description = %q, want fake-can", desc)
	}

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadFloat32(ctx, 1000, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if got != 12.5 {
		t.Fatalf("ReadFloat32 = %v, want 12.5", got)
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1000, instance: 1}) {
		t.Fatalf("readFloats = %+v", fake.readFloats)
	}
}

func TestDeviceClientBridgeAnswersFirmwareIdentification(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4b, 1, "?IF")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write ?IF frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read ?IF response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4b, 1, deviceBridgeFirmwareIdentification)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("?IF response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("?IF should not hit typed parameter reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeAnswersBareFirmwareIdentification(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("?IF\r")); err != nil {
		t.Fatalf("write bare ?IF frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read bare ?IF response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0, 0, deviceBridgeFirmwareIdentification)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("bare ?IF response = %q, want %q", string(buf[:n]), string(want))
	}
}

func TestDeviceClientBridgeAnswersBoardIdentificationProbes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
		seq     uint16
	}{
		{name: "board-id", payload: "?BI", seq: 0xfe46},
		{name: "board-id-device", payload: "?BID", seq: 0xfe42},
		{name: "board-id-firmware", payload: "?BIF", seq: 0xfe44},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fake := &fakeDeviceClient{
				floats: map[fakeParamKey]float64{},
				ints:   map[fakeParamKey]int32{},
			}
			conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
				return fake, nil
			}, 200*time.Millisecond)(ctx)
			if err != nil {
				t.Fatalf("DialDeviceClient returned error: %v", err)
			}
			defer conn.Close()

			req := deviceBridgeTestFrame(0x4b, tc.seq, tc.payload)
			if _, err := conn.Write(req); err != nil {
				t.Fatalf("write %s frame: %v", tc.payload, err)
			}
			buf := make([]byte, 128)
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("read %s response: %v", tc.payload, err)
			}
			want := deviceBridgeInfoTestFrame(0x4b, tc.seq, deviceBridgeBoardIdentification)
			if !bytes.Equal(buf[:n], want) {
				t.Fatalf("%s response = %q, want %q", tc.payload, string(buf[:n]), string(want))
			}
			if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
				t.Fatalf("%s should not hit typed parameter reads: floats=%v ints=%v bulk=%v", tc.payload, fake.readFloats, fake.readInts, fake.bulkReads)
			}
		})
	}
}

func TestDeviceClientBridgeMatchesSerialSingleReadFraming(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 102, instance: 1}: 75},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4b, 0x07e1, "?VR006601")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write serial number read frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read serial number response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4b, 0x07e1, "0000004B")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("serial number response = %q, want %q", string(buf[:n]), string(want))
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatFromIntegerParam(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints: map[fakeParamKey]int32{
			{id: deviceBridgeFirmwareVersionIntParam, instance: 1}: 631,
		},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4b, Timeout: time.Second})
	got, err := client.ReadFloat32(ctx, deviceBridgeFirmwareVersionFloatParam, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 firmware version returned error: %v", err)
	}
	if math.Abs(got-6.31) > 0.001 {
		t.Fatalf("ReadFloat32 firmware version = %v, want 6.31", got)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: deviceBridgeFirmwareVersionIntParam, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
	if len(fake.readFloats) != 0 {
		t.Fatalf("readFloats = %+v, want none", fake.readFloats)
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatInBulkRead(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: 12.5},
		ints: map[fakeParamKey]int32{
			{id: deviceBridgeFirmwareVersionIntParam, instance: 1}: 631,
		},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4b, Timeout: time.Second})
	params := []mecom.Parameter{
		{ID: deviceBridgeFirmwareVersionFloatParam, Instance: 1, Type: mecom.DataTypeFloat32},
		{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32},
	}
	got, err := client.ReadBulk(ctx, params)
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 2 || math.Abs(got[0]-6.31) > 0.001 || got[1] != 12.5 {
		t.Fatalf("ReadBulk = %v, want [6.31 12.5]", got)
	}
	if len(fake.bulkReads) != 1 {
		t.Fatalf("bulkReads = %d, want 1", len(fake.bulkReads))
	}
	if len(fake.bulkReads[0]) != 1 || fake.bulkReads[0][0].ID != 1000 {
		t.Fatalf("bulk read parameters = %+v", fake.bulkReads[0])
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: deviceBridgeFirmwareVersionIntParam, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
	if len(fake.readFloats) != 0 {
		t.Fatalf("readFloats = %+v, want none", fake.readFloats)
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatOnlyBulkRead(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints: map[fakeParamKey]int32{
			{id: deviceBridgeFirmwareVersionIntParam, instance: 1}: 631,
		},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4b, Timeout: time.Second})
	got, err := client.ReadBulk(ctx, []mecom.Parameter{
		{ID: deviceBridgeFirmwareVersionFloatParam, Instance: 1, Type: mecom.DataTypeFloat32},
	})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 1 || math.Abs(got[0]-6.31) > 0.001 {
		t.Fatalf("ReadBulk = %v, want [6.31]", got)
	}
	if len(fake.bulkReads) != 0 {
		t.Fatalf("bulkReads = %+v, want none", fake.bulkReads)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: deviceBridgeFirmwareVersionIntParam, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
}

func TestDeviceClientBridgePassesSingleFloatNaN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: math.NaN()},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadFloat32(ctx, 1000, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if !math.IsNaN(got) {
		t.Fatalf("ReadFloat32 = %v, want NaN", got)
	}
}

func TestDeviceClientBridgeTranslatesSystemInt32Read(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		id   int
		want int32
	}{
		{name: "device type", id: 100, want: 8065},
		{name: "device status", id: 104, want: 1},
		{name: "random startup value", id: 115, want: 123456},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeDeviceClient{
				floats: map[fakeParamKey]float64{},
				ints:   map[fakeParamKey]int32{{id: tt.id, instance: 1}: tt.want},
			}
			conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
				return fake, nil
			}, 200*time.Millisecond)(ctx)
			if err != nil {
				t.Fatalf("DialDeviceClient returned error: %v", err)
			}
			defer conn.Close()

			client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
			got, err := client.ReadInt32(ctx, tt.id, 1)
			if err != nil {
				t.Fatalf("ReadInt32 returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ReadInt32 = %v, want %v", got, tt.want)
			}
			if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: tt.id, instance: 1}) {
				t.Fatalf("readInts = %+v", fake.readInts)
			}
			if len(fake.readFloats) != 0 {
				t.Fatalf("readFloats = %+v, want none", fake.readFloats)
			}
		})
	}
}

func TestDeviceClientBridgeTranslatesBulkReadTypes(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: 25.25},
		ints:   map[fakeParamKey]int32{{id: 2010, instance: 1}: 1},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	params := []mecom.Parameter{
		{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32},
		{ID: 2010, Instance: 1, Type: mecom.DataTypeInt32},
	}
	got, err := client.ReadBulk(ctx, params)
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 2 || got[0] != 25.25 || got[1] != 1 {
		t.Fatalf("ReadBulk = %v, want [25.25 1]", got)
	}
	if len(fake.bulkReads) != 1 {
		t.Fatalf("bulkReads = %d, want 1", len(fake.bulkReads))
	}
	if fake.bulkReads[0][0].Type != mecom.DataTypeFloat32 || fake.bulkReads[0][1].Type != mecom.DataTypeInt32 {
		t.Fatalf("bulk read parameter types = %+v", fake.bulkReads[0])
	}
}

func TestDeviceClientBridgePassesBulkFloatNaN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: math.NaN()},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 1000, Instance: 1, Type: mecom.DataTypeFloat32}})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 1 || !math.IsNaN(got[0]) {
		t.Fatalf("ReadBulk = %v, want [NaN]", got)
	}
}

func TestDeviceClientBridgeTranslatesWrites(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	if err := client.WriteFloat32(ctx, 2033, 1, 56); err != nil {
		t.Fatalf("WriteFloat32 returned error: %v", err)
	}
	if err := client.WriteInt32(ctx, 2010, 1, 0); err != nil {
		t.Fatalf("WriteInt32 returned error: %v", err)
	}
	if len(fake.floatWrites) != 1 || fake.floatWrites[0] != (fakeFloatWrite{key: fakeParamKey{id: 2033, instance: 1}, value: 56}) {
		t.Fatalf("floatWrites = %+v", fake.floatWrites)
	}
	if len(fake.intWrites) != 1 || fake.intWrites[0] != (fakeIntWrite{key: fakeParamKey{id: 2010, instance: 1}, value: 0}) {
		t.Fatalf("intWrites = %+v", fake.intWrites)
	}
}

func TestDeviceClientBridgeAnswersMetadataAndEmptyBigDataForUserNotes(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	for _, tc := range []struct {
		name    string
		seq     uint16
		payload string
		want    string
	}{
		{
			name:    "metadata",
			seq:     0x22,
			payload: "?VM007801",
			want:    "030301000001000000000000000000000000000000",
		},
		{
			name:    "big-data-read-empty",
			seq:     0x23,
			payload: "?VB00780100000000FFFF",
			want:    "000000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := conn.Write(deviceBridgeTestFrame(0x4b, tc.seq, tc.payload)); err != nil {
				t.Fatalf("write %s frame: %v", tc.payload, err)
			}
			buf := make([]byte, 128)
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("read %s response: %v", tc.payload, err)
			}
			want := deviceBridgeInfoTestFrame(0x4b, tc.seq, tc.want)
			if !bytes.Equal(buf[:n], want) {
				t.Fatalf("%s response = %q, want %q", tc.payload, string(buf[:n]), string(want))
			}
		})
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("metadata/big-data probes should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeAcceptsUserNotesBigDataWrite(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4b, 0x24, "VB00780100000000000501534E373600")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write VB frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read VB response: %v", err)
	}
	want := deviceBridgeOK(0x4b, 0x24, "")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("VB response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.bigDataWrites) != 1 || fake.bigDataWrites[0] != (fakeStringWrite{key: fakeParamKey{id: 120, instance: 1}, value: "SN76"}) {
		t.Fatalf("bigDataWrites = %+v, want SN76 write to ID 120 instance 1", fake.bigDataWrites)
	}
}

func TestDeviceClientBridgeEmulatesUnsupportedRingReadoutForCANBackedRoutes(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	for _, tc := range []struct {
		name     string
		seq      uint16
		payload  string
		wantData string
		wantOK   bool
	}{
		{name: "pointer", seq: 0x30, payload: "?RS0000", wantData: "00000000"},
		{name: "read-empty", seq: 0x31, payload: "?RS000100000000FFFF", wantData: "000000"},
		{name: "configure-empty", seq: 0x32, payload: "?RS0002000100", wantData: "00"},
		{name: "trigger-sync", seq: 0x33, payload: "?RS0003", wantOK: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := conn.Write(deviceBridgeTestFrame(0x4b, tc.seq, tc.payload)); err != nil {
				t.Fatalf("write %s frame: %v", tc.payload, err)
			}
			buf := make([]byte, 128)
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("read %s response: %v", tc.payload, err)
			}
			want := deviceBridgeInfoTestFrame(0x4b, tc.seq, tc.wantData)
			if tc.wantOK {
				want = deviceBridgeOK(0x4b, tc.seq, "")
			}
			if !bytes.Equal(buf[:n], want) {
				t.Fatalf("%s response = %q, want %q", tc.payload, string(buf[:n]), string(want))
			}
		})
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("ring probes should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeReturnsNACKForUnsupportedControl(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	err = client.Reset(ctx)
	if err == nil {
		t.Fatal("Reset returned nil error")
	}
	if !errors.Is(err, mecom.ErrTransportNotSupported) && err.Error() == "" {
		t.Fatalf("Reset returned unhelpful error: %v", err)
	}
}

func TestDeviceClientBridgeReturnsNACKForBadCRC(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("#4C0001?VR03E8010000\r")); err != nil {
		t.Fatalf("write bad frame: %v", err)
	}
	buf := make([]byte, 64)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read NACK: %v", err)
	}
	if n == 0 || buf[0] != '!' || !bytes.Contains(buf[:n], []byte("-")) {
		t.Fatalf("response = %q, want MeCom NACK", string(buf[:n]))
	}
	if len(fake.readFloats) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("bad CRC reached fake client: reads=%v bulk=%v", fake.readFloats, fake.bulkReads)
	}
}
