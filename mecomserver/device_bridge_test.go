package mecomserver

import (
	"bytes"
	"context"
	"errors"
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

type fakeDeviceClient struct {
	floats map[fakeParamKey]float64
	ints   map[fakeParamKey]int32

	readFloats  []fakeParamKey
	readInts    []fakeParamKey
	bulkReads   [][]mecom.Parameter
	floatWrites []fakeFloatWrite
	intWrites   []fakeIntWrite
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
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 104, instance: 1}: 1},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadInt32(ctx, 104, 1)
	if err != nil {
		t.Fatalf("ReadInt32 returned error: %v", err)
	}
	if got != 1 {
		t.Fatalf("ReadInt32 = %v, want 1", got)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: 104, instance: 1}) {
		t.Fatalf("readInts = %+v", fake.readInts)
	}
	if len(fake.readFloats) != 0 {
		t.Fatalf("readFloats = %+v, want none", fake.readFloats)
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
