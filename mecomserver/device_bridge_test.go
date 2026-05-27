package mecomserver

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egidinas/meerstetter-go/mecom"
)

type fakeParamKey struct {
	id       int
	instance int
}

type fakeSDOKey struct {
	index uint16
	sub   byte
}

type fakeSDORead struct {
	index uint16
	sub   byte
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

func deviceBridgeTestTransform(t *testing.T, id int, kind string) mecom.BridgeTransform {
	t.Helper()
	transform, ok := mecom.CANopenBridgeTransform(id)
	if !ok {
		t.Fatalf("missing bridge transform for MeCom ID %d", id)
	}
	if transform.Kind != kind {
		t.Fatalf("bridge transform %d kind = %q, want %q", id, transform.Kind, kind)
	}
	return transform
}

func deviceBridgeTestFrame(addr byte, seq uint16, payload string) []byte {
	body := []byte(fmt.Sprintf("#%02X%04X%s", addr, seq, payload))
	return []byte(fmt.Sprintf("%s%04X%c", body, mecom.CRC16(body), mecom.FrameTerminator))
}

func deviceBridgeInfoTestFrame(addr byte, seq uint16, payload string) []byte {
	body := []byte(fmt.Sprintf("!%02X%04X%s", addr, seq, payload))
	return []byte(fmt.Sprintf("%s%04X%c", body, mecom.CRC16(body), mecom.FrameTerminator))
}

func withDeviceBridgeCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	restore := SetDeviceBridgeCacheDir(dir)
	t.Cleanup(restore)
	return dir
}

func readSingleDeviceBridgeCacheSnapshot(t *testing.T, dir string) deviceBridgeCacheSnapshot {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, entry.Name())
		}
	}
	if len(files) != 1 {
		t.Fatalf("cache files = %v, want one JSON file", files)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0]))
	if err != nil {
		t.Fatalf("read cache file %s: %v", files[0], err)
	}
	var snap deviceBridgeCacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("decode cache file %s: %v", files[0], err)
	}
	return snap
}

func findDeviceBridgeCacheParam(t *testing.T, snap deviceBridgeCacheSnapshot, id, instance int) deviceBridgeCacheParameterSnapshot {
	t.Helper()
	for _, param := range snap.Parameters {
		if param.ID == id && param.Instance == instance {
			return param
		}
	}
	t.Fatalf("cache for %d.%d missing in %+v", id, instance, snap.Parameters)
	return deviceBridgeCacheParameterSnapshot{}
}

type fakeDeviceClient struct {
	floats     map[fakeParamKey]float64
	ints       map[fakeParamKey]int32
	floatErrs  map[fakeParamKey]error
	intErrs    map[fakeParamKey]error
	bulkValues map[fakeParamKey]float64
	bulkErr    error
	rawSDO     map[fakeSDOKey][]byte
	rawSDOErrs map[fakeSDOKey]error

	readFloats    []fakeParamKey
	readInts      []fakeParamKey
	bulkReads     [][]mecom.Parameter
	rawSDOReads   []fakeSDORead
	floatWrites   []fakeFloatWrite
	intWrites     []fakeIntWrite
	stringWrites  []fakeStringWrite
	bigDataWrites []fakeStringWrite
	closed        bool
}

func (f *fakeDeviceClient) ReadFloat32(_ context.Context, id, instance int) (float64, error) {
	key := fakeParamKey{id: id, instance: instance}
	f.readFloats = append(f.readFloats, key)
	if err := f.floatErrs[key]; err != nil {
		return 0, err
	}
	return f.floats[key], nil
}

func (f *fakeDeviceClient) ReadInt32(_ context.Context, id, instance int) (int32, error) {
	key := fakeParamKey{id: id, instance: instance}
	f.readInts = append(f.readInts, key)
	if err := f.intErrs[key]; err != nil {
		return 0, err
	}
	return f.ints[key], nil
}

func (f *fakeDeviceClient) ReadBulk(_ context.Context, params []mecom.Parameter) ([]float64, error) {
	f.bulkReads = append(f.bulkReads, append([]mecom.Parameter(nil), params...))
	if f.bulkErr != nil {
		return nil, f.bulkErr
	}
	values := make([]float64, len(params))
	for i, p := range params {
		key := fakeParamKey{id: p.ID, instance: p.Instance}
		if value, ok := f.bulkValues[key]; ok {
			values[i] = value
			continue
		}
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

func (f *fakeDeviceClient) ReadSDORaw(_ context.Context, index uint16, sub byte) ([]byte, error) {
	f.rawSDOReads = append(f.rawSDOReads, fakeSDORead{index: index, sub: sub})
	key := fakeSDOKey{index: index, sub: sub}
	if f.rawSDOErrs != nil {
		if err := f.rawSDOErrs[key]; err != nil {
			return nil, err
		}
	}
	if f.rawSDO != nil {
		if data, ok := f.rawSDO[key]; ok {
			return append([]byte(nil), data...), nil
		}
	}
	return nil, fmt.Errorf("%w: SDO 0x%04X:%02X", mecom.ErrUnknownParameter, index, sub)
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
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: 12.5},
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
	got, err := client.ReadFloat32(ctx, 1044, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if got != 12.5 {
		t.Fatalf("ReadFloat32 = %v, want 12.5", got)
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1044, instance: 1}) {
		t.Fatalf("readFloats = %+v", fake.readFloats)
	}
}

func TestDeviceClientBridgeRoutesMeasuredTemperatureReadFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1045, instance: 1}: 12.5},
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
	got, err := client.ReadFloat32(ctx, 1045, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if got != 12.5 {
		t.Fatalf("ReadFloat32 = %v, want 12.5", got)
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1045, instance: 1}) {
		t.Fatalf("readFloats = %+v", fake.readFloats)
	}
}

func TestDeviceClientBridgeRoutesLRMeasuredTemperatureReadFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: 12.5},
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
	got, err := client.ReadFloat32(ctx, 1044, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 returned error: %v", err)
	}
	if got != 12.5 {
		t.Fatalf("ReadFloat32 = %v, want 12.5", got)
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1044, instance: 1}) {
		t.Fatalf("readFloats = %+v", fake.readFloats)
	}
}

func TestDeviceClientBridgeRoutesMeasuredTemperatureInBulkReadFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1045, instance: 1}: 22.75},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4c, 0x44, "?VX01041501")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write bulk frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read bulk response: %v", err)
	}
	payload := fmt.Sprintf("%08X", math.Float32bits(float32(22.75)))
	want := deviceBridgeInfoTestFrame(0x4c, 0x44, payload)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("bulk response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.bulkReads) != 1 || len(fake.bulkReads[0]) != 1 {
		t.Fatalf("bulkReads = %+v", fake.bulkReads)
	}
	if got := fake.bulkReads[0][0]; got.ID != 1045 || got.Instance != 1 || got.Type != mecom.DataTypeFloat32 {
		t.Fatalf("bulk read param = %+v, want 1045.1 FLOAT32", got)
	}
}

func TestDeviceClientBridgeRoutesLRMeasuredTemperatureInBulkReadFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: 22.75},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4c, 0x44, "?VX01041401")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write bulk frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read bulk response: %v", err)
	}
	payload := fmt.Sprintf("%08X", math.Float32bits(float32(22.75)))
	want := deviceBridgeInfoTestFrame(0x4c, 0x44, payload)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("bulk response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.bulkReads) != 1 || len(fake.bulkReads[0]) != 1 {
		t.Fatalf("bulkReads = %+v", fake.bulkReads)
	}
	if got := fake.bulkReads[0][0]; got.ID != 1044 || got.Instance != 1 || got.Type != mecom.DataTypeFloat32 {
		t.Fatalf("bulk read param = %+v, want 1044.1 FLOAT32", got)
	}
}

func TestDeviceClientBridgeAnswersFirmwareIdentification(t *testing.T) {
	for _, payload := range []string{"?IF", "?VI"} {
		t.Run(payload, func(t *testing.T) {
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

			req := deviceBridgeTestFrame(0x4b, 1, payload)
			if _, err := conn.Write(req); err != nil {
				t.Fatalf("write %s frame: %v", payload, err)
			}
			buf := make([]byte, 128)
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("read %s response: %v", payload, err)
			}
			want := deviceBridgeInfoTestFrame(0x4b, 1, deviceBridgeFirmwareIdentification)
			if !bytes.Equal(buf[:n], want) {
				t.Fatalf("%s response = %q, want %q", payload, string(buf[:n]), string(want))
			}
			if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
				t.Fatalf("%s should not hit typed parameter reads: floats=%v ints=%v bulk=%v", payload, fake.readFloats, fake.readInts, fake.bulkReads)
			}
		})
	}
}

func TestDeviceClientBridgeAnswersBareFirmwareIdentification(t *testing.T) {
	for _, payload := range []string{"?IF", "?VI"} {
		t.Run(payload, func(t *testing.T) {
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

			if _, err := conn.Write([]byte(payload + "\r")); err != nil {
				t.Fatalf("write bare %s frame: %v", payload, err)
			}
			buf := make([]byte, 128)
			if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatalf("SetReadDeadline: %v", err)
			}
			n, err := conn.Read(buf)
			if err != nil {
				t.Fatalf("read bare %s response: %v", payload, err)
			}
			want := deviceBridgeInfoTestFrame(0, 0, deviceBridgeFirmwareIdentification)
			if !bytes.Equal(buf[:n], want) {
				t.Fatalf("bare %s response = %q, want %q", payload, string(buf[:n]), string(want))
			}
		})
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
		ints:   map[fakeParamKey]int32{{id: 100, instance: 1}: 8065},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4b, 0x07e1, "?VR006401")
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write single read frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read single read response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4b, 0x07e1, "00001F81")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("single read response = %q, want %q", string(buf[:n]), string(want))
	}
}

func TestDeviceClientBridgeAnswersCoSoSerialNumberFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 102, instance: 1}: 0x12345678},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	req := deviceBridgeTestFrame(0x4c, 0x07e2, "?VR006601")
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
	want := deviceBridgeInfoTestFrame(0x4c, 0x07e2, "12345678")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("serial number response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: 102, instance: 1}) {
		t.Fatalf("readInts = %+v, want serial number parameter read", fake.readInts)
	}
}

func TestDeviceClientBridgeAnswersCoSoHardwareVersionFromHealthySerialTrace(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 101, instance: 1}: 0x78},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadInt32(ctx, 101, 1)
	if err != nil {
		t.Fatalf("ReadInt32 hardware version returned error: %v", err)
	}
	if got != 0x78 {
		t.Fatalf("ReadInt32 hardware version = %#x, want 0x78", got)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: 101, instance: 1}) {
		t.Fatalf("readInts = %+v, want hardware version parameter read", fake.readInts)
	}

	const seq = 0x31a2
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VM006501")); err != nil {
		t.Fatalf("write metadata frame for CoSo hardware version: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo hardware version: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4c, seq, "01010100000001800000007FFFFFFF00000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo hardware version = %q, want %q", string(buf[:n]), string(want))
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatFromIntegerParam(t *testing.T) {
	ctx := context.Background()
	firmware := deviceBridgeTestTransform(t, 112, "synthesize_float32_from_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints: map[fakeParamKey]int32{
			{id: firmware.SourceMeComID, instance: 1}: 631,
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
	got, err := client.ReadFloat32(ctx, firmware.MeComID, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 firmware version returned error: %v", err)
	}
	if math.Abs(got-6.31) > 0.001 {
		t.Fatalf("ReadFloat32 firmware version = %v, want 6.31", got)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: firmware.SourceMeComID, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
	if len(fake.readFloats) != 0 {
		t.Fatalf("readFloats = %+v, want none", fake.readFloats)
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatInBulkRead(t *testing.T) {
	ctx := context.Background()
	firmware := deviceBridgeTestTransform(t, 112, "synthesize_float32_from_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: 12.5},
		ints: map[fakeParamKey]int32{
			{id: firmware.SourceMeComID, instance: 1}: 631,
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
		{ID: firmware.MeComID, Instance: 1, Type: mecom.DataTypeFloat32},
		{ID: 1044, Instance: 1, Type: mecom.DataTypeFloat32},
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
	if len(fake.bulkReads[0]) != 1 || fake.bulkReads[0][0].ID != 1044 {
		t.Fatalf("bulk read parameters = %+v", fake.bulkReads[0])
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: firmware.SourceMeComID, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
	if len(fake.readFloats) != 0 {
		t.Fatalf("readFloats = %+v, want none", fake.readFloats)
	}
}

func TestDeviceClientBridgeSynthesizesFirmwareFloatOnlyBulkRead(t *testing.T) {
	ctx := context.Background()
	firmware := deviceBridgeTestTransform(t, 112, "synthesize_float32_from_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints: map[fakeParamKey]int32{
			{id: firmware.SourceMeComID, instance: 1}: 631,
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
		{ID: firmware.MeComID, Instance: 1, Type: mecom.DataTypeFloat32},
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
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: firmware.SourceMeComID, instance: 1}) {
		t.Fatalf("readInts = %+v, want firmware int parameter read", fake.readInts)
	}
}

func TestDeviceClientBridgePassesSingleFloatNaN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: math.NaN()},
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
	got, err := client.ReadFloat32(ctx, 1044, 1)
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
		{name: "device status", id: 107, want: 1},
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

func TestDeviceClientBridgeMasksLargeRandomStartupValueForMeSoftCompatibility(t *testing.T) {
	ctx := context.Background()
	startup := deviceBridgeTestTransform(t, 115, "mask_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints: map[fakeParamKey]int32{
			{id: startup.MeComID, instance: 1}: 0x5296260A,
			{id: 2010, instance: 1}:            1,
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
	got, err := client.ReadInt32(ctx, startup.MeComID, 1)
	if err != nil {
		t.Fatalf("ReadInt32 random startup value returned error: %v", err)
	}
	wantMasked := int32(0x5296260A & startup.Int32Mask)
	if got != wantMasked {
		t.Fatalf("ReadInt32 random startup value = %08X, want %08X", uint32(got), uint32(wantMasked))
	}

	bulk, err := client.ReadBulk(ctx, []mecom.Parameter{
		{ID: startup.MeComID, Instance: 1, Type: mecom.DataTypeInt32},
		{ID: 2010, Instance: 1, Type: mecom.DataTypeInt32},
	})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(bulk) != 2 || int32(bulk[0]) != wantMasked || bulk[1] != 1 {
		t.Fatalf("ReadBulk = %v, want [%08X 1]", bulk, uint32(wantMasked))
	}
}

func TestDeviceClientBridgeUsesConstantInt32TransformForMissingCANStatus(t *testing.T) {
	ctx := context.Background()
	stable := deviceBridgeTestTransform(t, 1200, "constant_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: math.NaN()},
		ints:   map[fakeParamKey]int32{},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4b, Timeout: time.Second})
	got, err := client.ReadInt32(ctx, stable.MeComID, 3)
	if err != nil {
		t.Fatalf("ReadInt32 stable status returned error: %v", err)
	}
	if got != stable.Int32Value {
		t.Fatalf("ReadInt32 stable status = %d, want %d", got, stable.Int32Value)
	}
	if len(fake.readInts) != 0 || len(fake.readFloats) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("constant status should not hit CAN reads: ints=%v floats=%v bulk=%v", fake.readInts, fake.readFloats, fake.bulkReads)
	}

	bulk, err := client.ReadBulk(ctx, []mecom.Parameter{
		{ID: stable.MeComID, Instance: 4, Type: mecom.DataTypeInt32},
		{ID: 1044, Instance: 1, Type: mecom.DataTypeFloat32},
	})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(bulk) != 2 || int32(bulk[0]) != stable.Int32Value || !math.IsNaN(bulk[1]) {
		t.Fatalf("ReadBulk = %v, want [%d NaN]", bulk, stable.Int32Value)
	}
	if len(fake.bulkReads) != 1 {
		t.Fatalf("bulkReads = %d, want 1", len(fake.bulkReads))
	}
	if len(fake.bulkReads[0]) != 1 || fake.bulkReads[0][0].ID != 1044 {
		t.Fatalf("bulk read parameters = %+v, want only downstream float miss", fake.bulkReads[0])
	}
	if len(fake.readInts) != 0 || len(fake.readFloats) != 0 {
		t.Fatalf("constant status should not hit typed reads: ints=%v floats=%v", fake.readInts, fake.readFloats)
	}
}

func TestDeviceClientBridgeKeepsCoSoBulkAliveWhenSaveToFlashStatusIsRequested(t *testing.T) {
	ctx := context.Background()
	saveStatus := deviceBridgeTestTransform(t, 108, "constant_int32")
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 104, instance: 1}: 7},
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
		{ID: 104, Instance: 1, Type: mecom.DataTypeInt32},
		{ID: saveStatus.MeComID, Instance: 1, Type: mecom.DataTypeInt32},
	})
	if err != nil {
		t.Fatalf("ReadBulk for CoSo 104+108 status frame returned error: %v", err)
	}
	if len(got) != 2 || got[0] != 7 || int32(got[1]) != saveStatus.Int32Value {
		t.Fatalf("ReadBulk for CoSo 104+108 status frame = %v, want [7 %d]", got, saveStatus.Int32Value)
	}
	if len(fake.bulkReads) != 1 {
		t.Fatalf("bulkReads = %d, want one downstream read for real parameters", len(fake.bulkReads))
	}
	if len(fake.bulkReads[0]) != 1 || fake.bulkReads[0][0] != (mecom.Parameter{ID: 104, Instance: 1, Type: mecom.DataTypeInt32}) {
		t.Fatalf("bulk read parameters = %+v, want only device-status read", fake.bulkReads[0])
	}
	if len(fake.readInts) != 0 || len(fake.readFloats) != 0 {
		t.Fatalf("constant status should not hit typed reads: ints=%v floats=%v", fake.readInts, fake.readFloats)
	}
}

func TestDeviceClientBridgeAnswersBootloaderStatusNoopOnly(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:    map[fakeParamKey]float64{},
		ints:      map[fakeParamKey]int32{},
		floatErrs: map[fakeParamKey]error{{id: 52200, instance: 1}: mecom.ErrUnknownParameter},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	buf := make([]byte, 128)
	const noopSeq = 0x47
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, noopSeq, "?BC00000000")); err != nil {
		t.Fatalf("write bootloader noop status frame: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read bootloader noop status response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4c, noopSeq, "00000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("bootloader noop status response = %q, want %q", string(buf[:n]), string(want))
	}

	const mutatingSeq = 0x48
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, mutatingSeq, "?BC00000001")); err != nil {
		t.Fatalf("write bootloader mutating command frame: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read bootloader mutating command response: %v", err)
	}
	want = deviceBridgeNACK(0x4c, mutatingSeq, 0x01)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("bootloader mutating command response = %q, want %q", string(buf[:n]), string(want))
	}

	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("bootloader status should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeTranslatesBulkReadTypes(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: 25.25},
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
		{ID: 1044, Instance: 1, Type: mecom.DataTypeFloat32},
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
		floats: map[fakeParamKey]float64{{id: 1044, instance: 1}: math.NaN()},
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
	got, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 1044, Instance: 1, Type: mecom.DataTypeFloat32}})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 1 || !math.IsNaN(got[0]) {
		t.Fatalf("ReadBulk = %v, want [NaN]", got)
	}
}

func TestDeviceClientBridgeZeroFillsBulkIntNaN(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:     map[fakeParamKey]float64{},
		ints:       map[fakeParamKey]int32{},
		bulkValues: map[fakeParamKey]float64{{id: 6200, instance: 1}: math.NaN()},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 6200, Instance: 1, Type: mecom.DataTypeInt32}})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("ReadBulk = %v, want [0]", got)
	}
}

func TestDeviceClientBridgeFallsBackToSingleReadsWhenBulkFails(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:  map[fakeParamKey]float64{{id: 1044, instance: 1}: 25.25},
		ints:    map[fakeParamKey]int32{{id: 6200, instance: 1}: 7},
		bulkErr: errors.New("bulk unavailable"),
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadBulk(ctx, []mecom.Parameter{
		{ID: 1044, Instance: 1, Type: mecom.DataTypeFloat32},
		{ID: 6200, Instance: 1, Type: mecom.DataTypeInt32},
	})
	if err != nil {
		t.Fatalf("ReadBulk returned error: %v", err)
	}
	if len(got) != 2 || got[0] != 25.25 || got[1] != 7 {
		t.Fatalf("ReadBulk = %v, want [25.25 7]", got)
	}
	if len(fake.bulkReads) != 1 {
		t.Fatalf("bulkReads = %d, want 1", len(fake.bulkReads))
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1044, instance: 1}) {
		t.Fatalf("readFloats = %+v", fake.readFloats)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: 6200, instance: 1}) {
		t.Fatalf("readInts = %+v", fake.readInts)
	}
}

func TestDeviceClientBridgeAcceptsCoSoBulkCountMismatch(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{},
	}
	const payload = "?VX32041501041502041401041402041403041404042701CF1C010BB80107DA0103FC0103FD0103FE0103E80103E90104B0010BB80207DA0203FC0203FD0203FE0203E80203E90204B0020BB80307DA0303FC0303FD0303FE0303E80303E90304B0030BB80407DA0403FD0403FE0403E80403E90404B004183801183802044E01044E02CBE801CBE802CBE803CBE804042401042501"
	const actualPairs = 49
	got, err := deviceBridgeBulkRead(ctx, fake, payload)
	if err != nil {
		t.Fatalf("deviceBridgeBulkRead returned error: %v", err)
	}
	if len(got) != actualPairs*8 {
		t.Fatalf("bulk payload length = %d, want %d for %d actual pairs", len(got), actualPairs*8, actualPairs)
	}
}

func TestDeviceClientBridgeUsesFullCANopenCompatibilityCatalogue(t *testing.T) {
	types := mecom.CANopenCompatibilityParameterTypes()
	maxInstances := mecom.CANopenCompatibilityParameterMaxInstances()
	writability := mecom.CANopenCompatibilityParameterWritability()
	if len(types) == 0 {
		t.Fatal("CANopen compatibility catalogue is empty")
	}

	ids := make([]int, 0, len(types))
	for id := range types {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	for _, id := range ids {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			wantType := types[id]
			typ := deviceBridgeParameterType(id)
			if typ != wantType {
				t.Fatalf("router type for catalogue ID %d = %q, want %q", id, typ, wantType)
			}
			if typ != mecom.DataTypeByte && !deviceBridgeParameterTypeSupported(typ) {
				t.Fatalf("router type for catalogue ID %d = %q, want supported scalar bridge type", id, typ)
			}
			if wantWritable, ok := writability[id]; ok {
				if got := deviceBridgeParameterWritable(id); got != wantWritable {
					t.Fatalf("router writability for catalogue ID %d = %v, want catalogue value %v", id, got, wantWritable)
				}
			}
			if wantMax, ok := maxInstances[id]; ok && wantMax > 0 && wantMax <= 0xff {
				if got := deviceBridgeParameterMaxInstances(id); got != byte(wantMax) {
					t.Fatalf("router max instances for catalogue ID %d = %d, want catalogue value %d", id, got, wantMax)
				}
			}
		})
	}

	if got := deviceBridgeParameterType(2040); got != mecom.DataTypeInt32 {
		t.Fatalf("type for TEC General Operating Mode 2040 = %q, want int32", got)
	}
	want2040Writable, ok := writability[2040]
	if !ok {
		t.Fatal("CANopen compatibility catalogue missing writability for TEC General Operating Mode 2040")
	}
	if got := deviceBridgeParameterWritable(2040); got != want2040Writable {
		t.Fatalf("writability for TEC General Operating Mode 2040 = %v, want catalogue value %v", got, want2040Writable)
	}
	if got := deviceBridgeParameterMaxInstances(2040); got != byte(maxInstances[2040]) {
		t.Fatalf("max instances for TEC General Operating Mode 2040 = %d, want catalogue value %d", got, maxInstances[2040])
	}
	if got, err := deviceBridgeMetadata("?VM07F801"); err != nil {
		t.Fatalf("metadata for TEC General Operating Mode 2040 returned error: %v", err)
	} else if want := "01030100000001800000007FFFFFFF00000000"; got != want {
		t.Fatalf("metadata for TEC General Operating Mode 2040 = %q, want %q", got, want)
	}

	if typ := deviceBridgeParameterType(50000); typ != deviceBridgeUnsupportedParameterType {
		t.Fatalf("type for unsupported debug/flash parameter 50000 = %q, want unsupported", typ)
	}
	if deviceBridgeParameterWritable(50000) {
		t.Fatal("unsupported debug/flash parameter 50000 is writable")
	}
}

func TestDeviceClientBridgeExposesCANopenNVCByteConfigMetadataFromCatalogue(t *testing.T) {
	unsupported := mecom.CANopenUnsupportedParameterIDs()

	for _, tc := range []struct {
		id       int
		base     uint16
		elements int
		metadata string
	}{
		{id: 2150, base: 0x1400, elements: 80, metadata: "0403010000005000FF00"},
		{id: 2151, base: 0x1600, elements: 144, metadata: "0403010000009000FF00"},
		{id: 2152, base: 0x1800, elements: 144, metadata: "0403010000009000FF00"},
		{id: 2153, base: 0x1A00, elements: 144, metadata: "0403010000009000FF00"},
	} {
		id := tc.id
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			if _, ok := unsupported[id]; ok {
				t.Fatalf("CANopen catalogue declares MeParID %d unsupported; want BYTE big-data bridge transform", id)
			}
			if deviceBridgeUnsupportedSerialFallbackRead(id) {
				t.Fatalf("byte/PDO config MeParID %d is still marked for serial fallback reads", id)
			}
			if got := deviceBridgeParameterType(id); got != mecom.DataTypeByte {
				t.Fatalf("bridge type for MeParID %d = %q, want byte", id, got)
			}
			if deviceBridgeParameterWritable(id) {
				t.Fatalf("byte/PDO config MeParID %d is writable", id)
			}
			transform, ok := mecom.CANopenBridgeTransform(id)
			if !ok {
				t.Fatalf("missing CANopen bridge transform for MeParID %d", id)
			}
			if transform.Kind != deviceBridgeTransformCANopenPDOConfigBytes {
				t.Fatalf("bridge transform kind for MeParID %d = %q, want %q", id, transform.Kind, deviceBridgeTransformCANopenPDOConfigBytes)
			}
			if transform.Type != mecom.DataTypeByte || transform.MaxElements != tc.elements || transform.CANopenIndexBase != tc.base || transform.Writable || !transform.HasMetadataFlags || transform.MetadataFlags != 0x03 {
				t.Fatalf("bridge transform for MeParID %d = %+v, want type byte, base 0x%04X, max %d, readonly, metadata flags 0x03", id, transform, tc.base, tc.elements)
			}
			got, err := deviceBridgeMetadata(fmt.Sprintf("?VM%04X01", id))
			if err != nil {
				t.Fatalf("metadata for byte/PDO config MeParID %d returned error: %v", id, err)
			}
			if got != tc.metadata {
				t.Fatalf("metadata for byte/PDO config MeParID %d = %q, want %q", id, got, tc.metadata)
			}
		})
	}
}

func TestDeviceClientBridgeReadsCANopenNVCByteConfigFromPDOObjects(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		rawSDO: map[fakeSDOKey][]byte{
			{index: 0x1400, sub: 1}: []byte{0x4B, 0x02, 0x00, 0x00},
			{index: 0x1400, sub: 2}: []byte{0xFE},
		},
	}

	got, err := deviceBridgePayload(ctx, fake, "?VB086601000000000050")
	if err != nil {
		t.Fatalf("byte/PDO big-data read returned error: %v", err)
	}
	data := make([]byte, 80)
	copy(data, []byte{0x4B, 0x02, 0x00, 0x00, 0xFE})
	want := "005001" + strings.ToUpper(hex.EncodeToString(data))
	if got != want {
		t.Fatalf("byte/PDO big-data read = %q, want %q", got, want)
	}
	if len(fake.rawSDOReads) < 2 || fake.rawSDOReads[0] != (fakeSDORead{index: 0x1400, sub: 1}) || fake.rawSDOReads[1] != (fakeSDORead{index: 0x1400, sub: 2}) {
		t.Fatalf("raw SDO reads = %+v, want first reads 0x1400:01 and 0x1400:02", fake.rawSDOReads)
	}
}

func TestDeviceClientBridgeRejectsScalarReadForCANopenNVCByteConfig(t *testing.T) {
	ctx := context.Background()

	for _, id := range []int{2150, 2151, 2152, 2153} {
		t.Run(strconv.Itoa(id), func(t *testing.T) {
			fake := &fakeDeviceClient{}
			if got, err := deviceBridgeSingleRead(ctx, fake, fmt.Sprintf("?VR%04X01", id)); err == nil {
				t.Fatalf("single read for byte/PDO config MeParID %d returned %q, want error", id, got)
			}
			if len(fake.readFloats) != 0 || len(fake.readInts) != 0 {
				t.Fatalf("single read for byte/PDO config MeParID %d reached downstream: floats=%v ints=%v", id, fake.readFloats, fake.readInts)
			}

			fake = &fakeDeviceClient{}
			if got, err := deviceBridgeBulkRead(ctx, fake, fmt.Sprintf("?VX01%04X01", id)); err == nil {
				t.Fatalf("bulk read for byte/PDO config MeParID %d returned %q, want error", id, got)
			}
			if len(fake.bulkReads) != 0 {
				t.Fatalf("bulk read for byte/PDO config MeParID %d reached downstream: %v", id, fake.bulkReads)
			}
		})
	}
}

func TestDeviceClientBridgeRejectsUnsupportedDebugFlashMetadata(t *testing.T) {
	ctx := context.Background()
	unsupported := mecom.CANopenUnsupportedParameterIDs()
	if _, ok := unsupported[50000]; !ok {
		t.Fatal("CANopen catalogue does not declare debug/flash MeParID 50000 unsupported")
	}
	if got := mecom.CANopenUnsupportedParameterBridgeBehavior()[50000]; got != mecom.CANopenUnsupportedBridgeBehaviorNACKBulkRead {
		t.Fatalf("unsupported debug/flash bridge behavior = %q, want %q", got, mecom.CANopenUnsupportedBridgeBehaviorNACKBulkRead)
	}

	fake := &fakeDeviceClient{}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	const seq = 0x91
	if _, err := conn.Write(deviceBridgeTestFrame(0x4b, seq, "?VMC35001")); err != nil {
		t.Fatalf("write metadata frame for unsupported debug/flash parameter: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for unsupported debug/flash parameter: %v", err)
	}
	want := deviceBridgeNACK(0x4b, seq, 0x05)
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for unsupported debug/flash parameter = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("unsupported debug/flash metadata reached fake client: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeCompatibilityMetadataComesFromCatalogue(t *testing.T) {
	if got := deviceBridgeParameterType(104); got != mecom.DataTypeInt32 {
		t.Fatalf("type for CoSo profile parameter 104 = %q, want int32", got)
	}
	if got := deviceBridgeParameterType(1045); got != mecom.DataTypeFloat32 {
		t.Fatalf("type for CoSo profile parameter 1045 = %q, want float32", got)
	}
	if got := deviceBridgeParameterType(1063); got != mecom.DataTypeFloat32 {
		t.Fatalf("type for CoSo profile parameter 1063 = %q, want float32", got)
	}
	if got := deviceBridgeParameterType(1102); got != mecom.DataTypeFloat32 {
		t.Fatalf("type for CoSo profile parameter 1102 = %q, want float32", got)
	}
	if got := deviceBridgeParameterType(53020); got != mecom.DataTypeInt32 {
		t.Fatalf("type for CoSo profile parameter 53020 = %q, want int32", got)
	}
	if got := deviceBridgeParameterType(6200); got != mecom.DataTypeInt32 {
		t.Fatalf("type for CoSo profile parameter 6200 = %q, want int32", got)
	}
	if got := deviceBridgeParameterType(1000); got != mecom.DataTypeFloat32 {
		t.Fatalf("type for normal TEC readout parameter 1000 = %q, want float32", got)
	}
	if got := deviceBridgeParameterType(2033); got != mecom.DataTypeFloat32 {
		t.Fatalf("type for normal TEC write parameter 2033 = %q, want float32", got)
	}
	catalogueMaxInstances := mecom.CANopenCompatibilityParameterMaxInstances()
	for _, id := range []int{1045, 104, 1063, 1102, 53020, 6200, 1044, 1200, 1000} {
		want, ok := catalogueMaxInstances[id]
		if !ok {
			t.Fatalf("general catalogue has no max-instance entry for %d", id)
		}
		if got := int(deviceBridgeParameterMaxInstances(id)); got != want {
			t.Fatalf("max instances for compatibility parameter %d = %d, want general catalogue max %d", id, got, want)
		}
	}
	if !deviceBridgeParameterWritable(2010) {
		t.Fatal("writability for mapped compatibility write parameter 2010 = false, want true")
	}
	if !deviceBridgeParameterWritable(120) {
		t.Fatal("writability for User Notes transform parameter 120 = false, want true")
	}
	if deviceBridgeParameterWritable(1200) {
		t.Fatal("writability for read-only transform parameter 1200 = true, want false")
	}
	if deviceBridgeParameterWritable(1102) {
		t.Fatal("writability for CoSo fan monitor parameter 1102 = true, want false")
	}
	if !deviceBridgeParameterWritable(2033) {
		t.Fatal("writability for normal TEC write parameter 2033 = false, want true")
	}
	if !deviceBridgeParameterWritable(6200) {
		t.Fatal("writability for CoSo fan enable parameter 6200 = false, want true")
	}
	if !deviceBridgeParameterWritable(52200) {
		t.Fatal("writability for CoSo Object External Temperature parameter 52200 = false, want true")
	}
	if !deviceBridgeParameterWritable(52201) {
		t.Fatal("writability for CoSo Sink Fixed Temperature parameter 52201 = false, want true")
	}
}

func TestDeviceClientBridgeCoSoLooseConfigCompatibilitySurface(t *testing.T) {
	ctx := context.Background()
	deviceBridgeSoftParameters.reset()
	t.Cleanup(deviceBridgeSoftParameters.reset)

	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{{id: 203, instance: 1}: 1},
	}

	if got := deviceBridgeParameterType(203); got != mecom.DataTypeInt32 {
		t.Fatalf("type for loose CoSo config parameter 203 = %q, want int32", got)
	}
	if got := deviceBridgeParameterType(6024); got != mecom.DataTypeLatin1 {
		t.Fatalf("type for loose CoSo config parameter 6024 = %q, want LATIN1", got)
	}
	if got := deviceBridgeParameterMaxInstances(6024); got != 4 {
		t.Fatalf("max instances for loose CoSo config parameter 6024 = %d, want 4", got)
	}
	if got := deviceBridgeParameterMaxInstances(52002); got != 4 {
		t.Fatalf("max instances for loose CoSo config parameter 52002 = %d, want 4", got)
	}
	if deviceBridgeParameterWritable(203) {
		t.Fatal("loose-only CoSo config parameter 203 should be read-only unless the general catalogue marks it writable")
	}

	for _, tc := range []struct {
		payload string
		want    string
	}{
		{payload: "?VM00CB01", want: "01010100000001800000007FFFFFFF00000000"},
		{payload: "?VM178801", want: "03010400000100000000"},
		{payload: "?VM17D401", want: "01010A00000001800000007FFFFFFF00000000"},
		{payload: "?VMCB2204", want: "01010400000001800000007FFFFFFF00000000"},
	} {
		got, err := deviceBridgePayload(ctx, fake, tc.payload)
		if err != nil {
			t.Fatalf("deviceBridgePayload(%q) returned error: %v", tc.payload, err)
		}
		if got != tc.want {
			t.Fatalf("deviceBridgePayload(%q) = %q, want %q", tc.payload, got, tc.want)
		}
	}

	if _, err := deviceBridgePayload(ctx, fake, "VS00CB0100000001"); !errors.Is(err, mecom.ErrParameterReadOnly) {
		t.Fatalf("int32 loose config write error = %v, want ErrParameterReadOnly", err)
	}
	got, err := deviceBridgePayload(ctx, fake, "?VR00CB01")
	if err != nil {
		t.Fatalf("int32 soft config read returned error: %v", err)
	}
	if got != "00000001" {
		t.Fatalf("int32 loose config read = %q, want truthful downstream value 00000001", got)
	}

	if _, err := deviceBridgePayload(ctx, fake, "VB1788010000000000050148656C6C6F"); !errors.Is(err, mecom.ErrParameterReadOnly) {
		t.Fatalf("LATIN1 loose config big-data write error = %v, want ErrParameterReadOnly", err)
	}
	if _, err := deviceBridgePayload(ctx, fake, "SP"); err != nil {
		t.Fatalf("soft save-to-flash command returned error: %v", err)
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 1 || fake.readInts[0] != (fakeParamKey{id: 203, instance: 1}) || len(fake.bulkReads) != 0 ||
		len(fake.floatWrites) != 0 || len(fake.intWrites) != 0 || len(fake.stringWrites) != 0 || len(fake.bigDataWrites) != 0 {
		t.Fatalf("loose config facade should only truth read via downstream: floatReads=%v intReads=%v bulkReads=%v floatWrites=%v intWrites=%v stringWrites=%v bigDataWrites=%v",
			fake.readFloats, fake.readInts, fake.bulkReads, fake.floatWrites, fake.intWrites, fake.stringWrites, fake.bigDataWrites)
	}
}

func TestDeviceClientBridgeCachesLooseCoSoIntFromDownstreamBeforeZeroFallback(t *testing.T) {
	ctx := context.Background()
	deviceBridgeSoftParameters.reset()
	t.Cleanup(deviceBridgeSoftParameters.reset)

	key := fakeParamKey{id: 203, instance: 1}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{key: 7},
	}
	conn, _, err := DialDeviceClient("fake-can-cache", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadInt32(ctx, key.id, key.instance)
	if err != nil {
		t.Fatalf("first loose ReadInt32 returned error: %v", err)
	}
	if got != 7 {
		t.Fatalf("first loose ReadInt32 = %d, want truthful downstream value 7", got)
	}
	fake.ints[key] = 8
	got, err = client.ReadInt32(ctx, key.id, key.instance)
	if err != nil {
		t.Fatalf("second loose ReadInt32 returned error: %v", err)
	}
	if got != 8 {
		t.Fatalf("second loose ReadInt32 = %d, want refreshed downstream value 8", got)
	}
	if len(fake.readInts) != 2 || fake.readInts[0] != key || fake.readInts[1] != key {
		t.Fatalf("readInts = %+v, want two downstream refreshes for %v", fake.readInts, key)
	}

	if err := client.WriteInt32(ctx, key.id, key.instance, 9); err == nil || !strings.Contains(err.Error(), "PAR_NOT_WRITABLE") {
		t.Fatalf("loose WriteInt32 error = %v, want PAR_NOT_WRITABLE NACK", err)
	}
	fake.intErrs = map[fakeParamKey]error{key: errors.New("temporary CAN miss")}
	got, err = client.ReadInt32(ctx, key.id, key.instance)
	if err != nil {
		t.Fatalf("post-write loose ReadInt32 returned error: %v", err)
	}
	if got != 8 {
		t.Fatalf("post-write loose ReadInt32 = %d, want cached truthful downstream value 8", got)
	}
	if len(fake.readInts) != 3 {
		t.Fatalf("post-write readInts = %+v, want live retry plus cached fallback", fake.readInts)
	}
	if len(fake.intWrites) != 0 {
		t.Fatalf("loose WriteInt32 should update local cache only, got downstream writes %+v", fake.intWrites)
	}
}

func TestDeviceClientBridgeDoesNotCacheLooseCoSoReadFailure(t *testing.T) {
	ctx := context.Background()
	deviceBridgeSoftParameters.reset()
	t.Cleanup(deviceBridgeSoftParameters.reset)

	key := fakeParamKey{id: 203, instance: 1}
	fake := &fakeDeviceClient{
		floats:  map[fakeParamKey]float64{},
		ints:    map[fakeParamKey]int32{},
		intErrs: map[fakeParamKey]error{key: errors.New("temporary CAN miss")},
	}
	conn, _, err := DialDeviceClient("fake-can-cache", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadInt32(ctx, key.id, key.instance)
	if err != nil {
		t.Fatalf("loose ReadInt32 with downstream miss returned error: %v", err)
	}
	if got != 0 {
		t.Fatalf("loose ReadInt32 with downstream miss = %d, want safe placeholder 0", got)
	}

	delete(fake.intErrs, key)
	fake.ints[key] = 8
	got, err = client.ReadInt32(ctx, key.id, key.instance)
	if err != nil {
		t.Fatalf("loose ReadInt32 after downstream recovery returned error: %v", err)
	}
	if got != 8 {
		t.Fatalf("loose ReadInt32 after downstream recovery = %d, want truthful value 8", got)
	}
	if len(fake.readInts) != 2 {
		t.Fatalf("readInts = %+v, want miss plus retry because placeholder was not cached", fake.readInts)
	}
}

func TestDeviceClientBridgeTruthsLooseCoSoBulkNumericFromDownstream(t *testing.T) {
	ctx := context.Background()
	deviceBridgeSoftParameters.reset()
	t.Cleanup(deviceBridgeSoftParameters.reset)

	looseKey := fakeParamKey{id: 203, instance: 1}
	tempKey := fakeParamKey{id: 1045, instance: 1}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{tempKey: 22.75},
		ints:   map[fakeParamKey]int32{looseKey: 7},
	}
	conn, _, err := DialDeviceClient("fake-can-cache", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	params := []mecom.Parameter{
		{ID: looseKey.id, Instance: looseKey.instance, Type: mecom.DataTypeInt32},
		{ID: tempKey.id, Instance: tempKey.instance, Type: mecom.DataTypeFloat32},
	}
	got, err := client.ReadBulk(ctx, params)
	if err != nil {
		t.Fatalf("mixed loose/current ReadBulk returned error: %v", err)
	}
	if len(got) != 2 || got[0] != 7 || got[1] != 22.75 {
		t.Fatalf("mixed loose/current ReadBulk = %v, want [7 22.75]", got)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != looseKey {
		t.Fatalf("readInts = %+v, want one loose downstream refresh for %v", fake.readInts, looseKey)
	}
	if len(fake.bulkReads) != 1 || len(fake.bulkReads[0]) != 1 || fake.bulkReads[0][0].ID != tempKey.id {
		t.Fatalf("bulkReads = %+v, want only current non-loose parameter in downstream bulk", fake.bulkReads)
	}

	fake.ints[looseKey] = 8
	got, err = client.ReadBulk(ctx, params)
	if err != nil {
		t.Fatalf("second mixed loose/current ReadBulk returned error: %v", err)
	}
	if len(got) != 2 || got[0] != 8 || got[1] != 22.75 {
		t.Fatalf("second mixed loose/current ReadBulk = %v, want [8 22.75]", got)
	}
	if len(fake.readInts) != 2 {
		t.Fatalf("second readInts = %+v, want loose value refreshed downstream", fake.readInts)
	}
	if len(fake.bulkReads) != 2 {
		t.Fatalf("bulkReads = %+v, want current value refreshed each bulk read", fake.bulkReads)
	}
}

func TestDeviceClientBridgeWritesPerDeviceCacheJSON(t *testing.T) {
	ctx := context.Background()
	dir := withDeviceBridgeCacheDir(t)
	state := newDeviceBridgeState("can:can0/0x4b")
	key := fakeParamKey{id: 52002, instance: 3}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{key: 7},
	}

	got, err := deviceBridgeSingleReadWithState(ctx, state, fake, "?VRCB2203")
	if err != nil {
		t.Fatalf("deviceBridgeSingleReadWithState returned error: %v", err)
	}
	if got != "00000007" {
		t.Fatalf("deviceBridgeSingleReadWithState = %q, want 00000007", got)
	}

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	if snap.Device != "can:can0/0x4b" {
		t.Fatalf("snapshot device = %q, want can:can0/0x4b", snap.Device)
	}
	param := findDeviceBridgeCacheParam(t, snap, key.id, key.instance)
	if param.Int32 == nil || *param.Int32 != 7 {
		t.Fatalf("cached int32 = %v, want 7", param.Int32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

func TestDeviceClientBridgeUpdatesCacheJSONFromLiveBulk(t *testing.T) {
	ctx := context.Background()
	dir := withDeviceBridgeCacheDir(t)
	state := newDeviceBridgeState("can:can0/0x4b")
	key := fakeParamKey{id: 1045, instance: 1}
	fake := &fakeDeviceClient{
		floats:     map[fakeParamKey]float64{},
		ints:       map[fakeParamKey]int32{},
		bulkValues: map[fakeParamKey]float64{key: 22.75},
	}

	got, err := deviceBridgeBulkReadWithState(ctx, state, fake, "?VX01041501")
	if err != nil {
		t.Fatalf("deviceBridgeBulkReadWithState returned error: %v", err)
	}
	want := fmt.Sprintf("%08X", math.Float32bits(float32(22.75)))
	if got != want {
		t.Fatalf("deviceBridgeBulkReadWithState = %q, want %q", got, want)
	}

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	param := findDeviceBridgeCacheParam(t, snap, key.id, key.instance)
	if param.Float32 == nil || *param.Float32 != 22.75 {
		t.Fatalf("cached float32 = %v, want 22.75", param.Float32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

func TestDeviceClientBridgeBulkReadUsesLiveReadForMetadataLiveActualParameters(t *testing.T) {
	ctx := context.Background()
	dir := withDeviceBridgeCacheDir(t)
	state := newDeviceBridgeState("can:can0/0x4b")
	key := fakeParamKey{id: 2040, instance: 1}
	fake := &fakeDeviceClient{
		ints:       map[fakeParamKey]int32{key: 3},
		bulkValues: map[fakeParamKey]float64{key: 0},
	}

	got, err := deviceBridgeBulkReadWithState(ctx, state, fake, "?VX0107F801")
	if err != nil {
		t.Fatalf("deviceBridgeBulkReadWithState returned error: %v", err)
	}
	if got != "00000003" {
		t.Fatalf("operating-mode bulk read = %q, want live single-read value 00000003", got)
	}
	if len(fake.bulkReads) != 0 {
		t.Fatalf("bulkReads = %+v, want metadata-live-actual parameter to bypass stale bulk read", fake.bulkReads)
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != key {
		t.Fatalf("readInts = %+v, want one live operating-mode read for %v", fake.readInts, key)
	}

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	param := findDeviceBridgeCacheParam(t, snap, key.id, key.instance)
	if param.Int32 == nil || *param.Int32 != 3 {
		t.Fatalf("cached operating-mode int32 = %v, want 3", param.Int32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

func TestDeviceBridgeObserveSerialResponseRefreshesCacheJSON(t *testing.T) {
	dir := withDeviceBridgeCacheDir(t)
	state := newDeviceBridgeState("0x4B:0:serial:/dev/ttyUSB0@57600")
	req := deviceBridgeTestFrame(0x4b, 99, "?VRCB2203")
	resp := deviceBridgeInfoTestFrame(0x4b, 99, "00000007")

	deviceBridgeObserveFrame(state, req, resp)

	snap := readSingleDeviceBridgeCacheSnapshot(t, dir)
	param := findDeviceBridgeCacheParam(t, snap, 52002, 3)
	if param.Int32 == nil || *param.Int32 != 7 {
		t.Fatalf("observed cached int32 = %v, want 7", param.Int32)
	}
	if param.Source != deviceBridgeCacheSourceDownstream || !param.LiveRefresh || param.UpdatedAt == "" {
		t.Fatalf("cache metadata = source %q live %v updated %q, want downstream live timestamped", param.Source, param.LiveRefresh, param.UpdatedAt)
	}
}

func TestDeviceBridgeLoadsPerDeviceCacheJSON(t *testing.T) {
	_ = withDeviceBridgeCacheDir(t)
	state := newDeviceBridgeState("can:can0/0x4b")
	req := deviceBridgeTestFrame(0x4b, 99, "?VRCB2203")
	resp := deviceBridgeInfoTestFrame(0x4b, 99, "00000007")
	deviceBridgeObserveFrame(state, req, resp)

	loaded := newDeviceBridgeState("can:can0/0x4b")
	value, ok, err := loaded.softParameters.lookupParameter(52002, 3, mecom.DataTypeInt32)
	if err != nil {
		t.Fatalf("lookup cached parameter returned error: %v", err)
	}
	if !ok {
		t.Fatal("cached parameter missing after reload")
	}
	if value.int != 7 || value.source != deviceBridgeCacheSourceDownstream || !value.liveRefresh || value.updatedAt.IsZero() {
		t.Fatalf("loaded value = %+v, want downstream live int32 7 with timestamp", value)
	}
}

func TestDeviceClientBridgeMetadataUsesLiveOperatingModeActual(t *testing.T) {
	ctx := context.Background()
	key := fakeParamKey{id: 2040, instance: 1}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{key: 3},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	const seq = 0x7f
	if _, err := conn.Write(deviceBridgeTestFrame(0x4b, seq, "?VM07F801")); err != nil {
		t.Fatalf("write metadata frame for operating mode: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for operating mode: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4b, seq, "01030100000001800000007FFFFFFF00000003")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for operating mode = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readInts) != 1 || fake.readInts[0] != key {
		t.Fatalf("readInts = %+v, want one live operating-mode read", fake.readInts)
	}
}

func TestDeviceClientBridgeMetadataUsesCachedOperatingModeActual(t *testing.T) {
	ctx := context.Background()
	key := fakeParamKey{id: 2040, instance: 1}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{},
		ints:   map[fakeParamKey]int32{key: 3},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4b, Timeout: time.Second})
	value, err := client.ReadInt32(ctx, 2040, 1)
	if err != nil {
		t.Fatalf("initial operating-mode read returned error: %v", err)
	}
	if value != 3 {
		t.Fatalf("initial operating-mode read = %d, want 3", value)
	}
	fake.intErrs = map[fakeParamKey]error{key: errors.New("temporary CAN miss")}

	const seq = 0x80
	if _, err := conn.Write(deviceBridgeTestFrame(0x4b, seq, "?VM07F801")); err != nil {
		t.Fatalf("write metadata frame for cached operating mode: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for cached operating mode: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4b, seq, "01030100000001800000007FFFFFFF00000003")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for cached operating mode = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readInts) != 2 || fake.readInts[0] != key || fake.readInts[1] != key {
		t.Fatalf("readInts = %+v, want metadata to retry live operating mode then use cached value", fake.readInts)
	}
}

func TestDeviceClientBridgeAnswersCurrentCoSoTraceParametersFromCatalogue(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1063, instance: 1}: 33.25},
		ints:   map[fakeParamKey]int32{{id: 104, instance: 1}: 7},
		floatErrs: map[fakeParamKey]error{
			{id: 52200, instance: 1}: mecom.ErrUnknownParameter,
			{id: 52201, instance: 1}: mecom.ErrUnknownParameter,
		},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	got, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 104, Instance: 1, Type: mecom.DataTypeInt32}})
	if err != nil {
		t.Fatalf("ReadBulk for CoSo trace parameter 104 returned error: %v", err)
	}
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("ReadBulk for CoSo trace parameter 104 = %v, want [7]", got)
	}
	if len(fake.bulkReads) != 1 || len(fake.bulkReads[0]) != 1 {
		t.Fatalf("bulkReads = %+v, want one device-status read", fake.bulkReads)
	}
	if fake.bulkReads[0][0] != (mecom.Parameter{ID: 104, Instance: 1, Type: mecom.DataTypeInt32}) {
		t.Fatalf("bulk read parameter = %+v, want int32 device-status read", fake.bulkReads[0][0])
	}

	const seq = 0x37
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VM042701")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 1063: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 1063: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4c, seq, "00010100000001FF8000007F80000000000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 1063 = %q, want %q", string(buf[:n]), string(want))
	}

	const fanMonitorSeq = 0x3a
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, fanMonitorSeq, "?VM044E01")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 1102: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 1102: %v", err)
	}
	want = deviceBridgeInfoTestFrame(0x4c, fanMonitorSeq, "00010200000001FF8000007F80000000000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 1102 = %q, want %q", string(buf[:n]), string(want))
	}

	const licenseSeq = 0x38
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, licenseSeq, "?VMCF1C01")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 53020: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 53020: %v", err)
	}
	want = deviceBridgeInfoTestFrame(0x4c, licenseSeq, "01010100000001800000007FFFFFFF00000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 53020 = %q, want %q", string(buf[:n]), string(want))
	}

	const fanSeq = 0x39
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, fanSeq, "?VM183801")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 6200: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 6200: %v", err)
	}
	want = deviceBridgeInfoTestFrame(0x4c, fanSeq, "01030200000001800000007FFFFFFF00000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 6200 = %q, want %q", string(buf[:n]), string(want))
	}

	const objectExternalSeq = 0x3b
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, objectExternalSeq, "?VMCBE801")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 52200: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 52200: %v", err)
	}
	want = deviceBridgeInfoTestFrame(0x4c, objectExternalSeq, "00030100000001FF8000007F8000007FC00000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 52200 = %q, want %q", string(buf[:n]), string(want))
	}

	const sinkFixedSeq = 0x3c
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, sinkFixedSeq, "?VMCBE901")); err != nil {
		t.Fatalf("write metadata frame for CoSo trace parameter 52201: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response for CoSo trace parameter 52201: %v", err)
	}
	want = deviceBridgeInfoTestFrame(0x4c, sinkFixedSeq, "00030400000001FF8000007F80000041C80000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response for CoSo trace parameter 52201 = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readFloats) != 2 ||
		fake.readFloats[0] != (fakeParamKey{id: 52200, instance: 1}) ||
		fake.readFloats[1] != (fakeParamKey{id: 52201, instance: 1}) {
		t.Fatalf("metadata float reads = %v, want live virtual probes for 52200.1 and 52201.1", fake.readFloats)
	}
	if len(fake.readInts) != 0 {
		t.Fatalf("metadata int reads = %v, want none", fake.readInts)
	}
}

func TestDeviceClientBridgeRejectsUnknownCoSoTraceParameterTypes(t *testing.T) {
	ctx := context.Background()
	t.Run("metadata-fallback", func(t *testing.T) {
		fake := &fakeDeviceClient{
			floats: map[fakeParamKey]float64{{id: 1062, instance: 1}: 12.5},
			ints:   map[fakeParamKey]int32{},
		}
		conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
			return fake, nil
		}, 200*time.Millisecond)(ctx)
		if err != nil {
			t.Fatalf("DialDeviceClient returned error: %v", err)
		}
		defer conn.Close()

		const seq = 0x45
		if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VM042601")); err != nil {
			t.Fatalf("write metadata frame for unknown CoSo trace parameter: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read metadata response for unknown CoSo trace parameter: %v", err)
		}
		want := deviceBridgeInfoTestFrame(0x4c, seq, "00010100000001FF8000007F80000000000000")
		if !bytes.Equal(buf[:n], want) {
			t.Fatalf("metadata response for unknown CoSo trace parameter = %q, want %q", string(buf[:n]), string(want))
		}

		client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
		vals, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 1062, Instance: 1, Type: mecom.DataTypeFloat32}})
		if err != nil {
			t.Fatalf("ReadBulk for unknown CoSo trace parameter returned unexpected error: %v", err)
		}
		if len(vals) != 1 || vals[0] != 0 {
			t.Fatalf("unknown CoSo param in bulk: got %v, want [0]", vals)
		}
		if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
			t.Fatalf("unknown parameter reached fake client: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
		}
	})

	t.Run("single-read", func(t *testing.T) {
		fake := &fakeDeviceClient{
			floats: map[fakeParamKey]float64{{id: 65010, instance: 1}: 12.5},
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
		if _, err := client.ReadFloat32(ctx, 65010, 1); err == nil {
			t.Fatal("ReadFloat32 for unknown CoSo trace parameter returned nil error")
		}
		if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
			t.Fatalf("unknown parameter reached fake client: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
		}
	})

	t.Run("bulk-read", func(t *testing.T) {
		// Unknown-type params in a bulk read are zero-filled rather than
		// aborting the whole batch. The fake client must not be called.
		fake := &fakeDeviceClient{
			floats: map[fakeParamKey]float64{{id: 65010, instance: 1}: 12.5},
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
		vals, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 65010, Instance: 1, Type: mecom.DataTypeFloat32}})
		if err != nil {
			t.Fatalf("ReadBulk for unknown CoSo trace parameter returned unexpected error: %v", err)
		}
		if len(vals) != 1 || vals[0] != 0 {
			t.Fatalf("unknown CoSo param in bulk: got %v, want [0]", vals)
		}
		if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
			t.Fatalf("unknown parameter reached fake client: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
		}
	})

	t.Run("big-data-fallback", func(t *testing.T) {
		fake := &fakeDeviceClient{
			floats: map[fakeParamKey]float64{{id: 65010, instance: 1}: 12.5},
			ints:   map[fakeParamKey]int32{},
		}
		conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
			return fake, nil
		}, 200*time.Millisecond)(ctx)
		if err != nil {
			t.Fatalf("DialDeviceClient returned error: %v", err)
		}
		defer conn.Close()

		const seq = 0x46
		if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VBFDF20100000000FFFF")); err != nil {
			t.Fatalf("write big-data frame for unknown CoSo trace parameter: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		buf := make([]byte, 128)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("read big-data response for unknown CoSo trace parameter: %v", err)
		}
		want := deviceBridgeInfoTestFrame(0x4c, seq, "000000")
		if !bytes.Equal(buf[:n], want) {
			t.Fatalf("big-data response for unknown CoSo trace parameter = %q, want %q", string(buf[:n]), string(want))
		}
		if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
			t.Fatalf("unknown big-data parameter reached fake client: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
		}
	})
}

func TestDeviceClientBridgeReadsNormalTECCatalogueParametersInCoSoProfile(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{{id: 1000, instance: 1}: 12.5},
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
		t.Fatalf("ReadFloat32 for normal TEC catalogue parameter 1000 returned error: %v", err)
	}
	if got != 12.5 {
		t.Fatalf("ReadFloat32 for 1000 = %f, want 12.5", got)
	}
	if len(fake.readFloats) != 1 || fake.readFloats[0] != (fakeParamKey{id: 1000, instance: 1}) {
		t.Fatalf("readFloats = %v", fake.readFloats)
	}
	if len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("unexpected fake client calls: ints=%v bulk=%v", fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeTranslatesWrites(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:    map[fakeParamKey]float64{},
		ints:      map[fakeParamKey]int32{},
		floatErrs: map[fakeParamKey]error{{id: 52200, instance: 1}: mecom.ErrUnknownParameter},
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

func TestDeviceClientBridgeVirtualParameterStoresCoSoWriteSurface(t *testing.T) {
	deviceBridgeVirtualParameters.reset()

	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:    map[fakeParamKey]float64{},
		ints:      map[fakeParamKey]int32{},
		floatErrs: map[fakeParamKey]error{{id: 52200, instance: 1}: mecom.ErrUnknownParameter},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	initial, err := client.ReadFloat32(ctx, 52200, 1)
	if err != nil {
		t.Fatalf("initial ReadFloat32 for virtual CoSo parameter 52200 returned error: %v", err)
	}
	if !math.IsNaN(initial) {
		t.Fatalf("initial ReadFloat32 for virtual CoSo parameter 52200 = %v, want NaN", initial)
	}
	initialBulk, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 52200, Instance: 1, Type: mecom.DataTypeFloat32}})
	if err != nil {
		t.Fatalf("initial ReadBulk for virtual CoSo parameter 52200 returned error: %v", err)
	}
	if len(initialBulk) != 1 || !math.IsNaN(initialBulk[0]) {
		t.Fatalf("initial ReadBulk for virtual CoSo parameter 52200 = %v, want [NaN]", initialBulk)
	}
	if err := client.WriteFloat32(ctx, 52200, 1, 10); err != nil {
		t.Fatalf("WriteFloat32 for virtual CoSo parameter 52200 returned error: %v", err)
	}
	got, err := client.ReadFloat32(ctx, 52200, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 for virtual CoSo parameter 52200 returned error: %v", err)
	}
	if got != 10 {
		t.Fatalf("ReadFloat32 for virtual CoSo parameter 52200 = %v, want 10", got)
	}
	bulk, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 52200, Instance: 1, Type: mecom.DataTypeFloat32}})
	if err != nil {
		t.Fatalf("ReadBulk for virtual CoSo parameter 52200 returned error: %v", err)
	}
	if len(bulk) != 1 || bulk[0] != 10 {
		t.Fatalf("ReadBulk for virtual CoSo parameter 52200 = %v, want [10]", bulk)
	}
	if len(fake.readFloats) == 0 {
		t.Fatalf("virtual parameter should probe downstream before using compatibility cache")
	}
	if len(fake.readInts) != 0 || len(fake.bulkReads) != 0 || len(fake.floatWrites) != 0 || len(fake.intWrites) != 0 {
		t.Fatalf("virtual parameter should not write or bulk-read downstream: floatReads=%v intReads=%v bulkReads=%v floatWrites=%v intWrites=%v", fake.readFloats, fake.readInts, fake.bulkReads, fake.floatWrites, fake.intWrites)
	}
}

func TestDeviceClientBridgeVirtualParameterStoresCoSoSinkFixedTemperature(t *testing.T) {
	deviceBridgeVirtualParameters.reset()

	ctx := context.Background()
	fake := &fakeDeviceClient{
		floats:    map[fakeParamKey]float64{},
		ints:      map[fakeParamKey]int32{},
		floatErrs: map[fakeParamKey]error{{id: 52201, instance: 1}: mecom.ErrUnknownParameter},
	}
	conn, _, err := DialDeviceClient("fake-can", func(context.Context) (mecom.DeviceClient, error) {
		return fake, nil
	}, 200*time.Millisecond)(ctx)
	if err != nil {
		t.Fatalf("DialDeviceClient returned error: %v", err)
	}
	defer conn.Close()

	client := mecom.NewClient(conn, mecom.ClientConfig{Address: 0x4c, Timeout: time.Second})
	initial, err := client.ReadFloat32(ctx, 52201, 1)
	if err != nil {
		t.Fatalf("initial ReadFloat32 for virtual CoSo parameter 52201 returned error: %v", err)
	}
	if initial != 25 {
		t.Fatalf("initial ReadFloat32 for virtual CoSo parameter 52201 = %v, want 25", initial)
	}
	initialBulk, err := client.ReadBulk(ctx, []mecom.Parameter{{ID: 52201, Instance: 1, Type: mecom.DataTypeFloat32}})
	if err != nil {
		t.Fatalf("initial ReadBulk for virtual CoSo parameter 52201 returned error: %v", err)
	}
	if len(initialBulk) != 1 || initialBulk[0] != 25 {
		t.Fatalf("initial ReadBulk for virtual CoSo parameter 52201 = %v, want [25]", initialBulk)
	}
	if err := client.WriteFloat32(ctx, 52201, 1, 25); err != nil {
		t.Fatalf("WriteFloat32 for virtual CoSo parameter 52201 returned error: %v", err)
	}
	got, err := client.ReadFloat32(ctx, 52201, 1)
	if err != nil {
		t.Fatalf("ReadFloat32 for virtual CoSo parameter 52201 returned error: %v", err)
	}
	if got != 25 {
		t.Fatalf("ReadFloat32 for virtual CoSo parameter 52201 = %v, want 25", got)
	}
	if len(fake.readFloats) == 0 {
		t.Fatalf("virtual parameter should probe downstream before using compatibility cache")
	}
	if len(fake.readInts) != 0 || len(fake.bulkReads) != 0 || len(fake.floatWrites) != 0 || len(fake.intWrites) != 0 {
		t.Fatalf("virtual parameter should not write or bulk-read downstream: floatReads=%v intReads=%v bulkReads=%v floatWrites=%v intWrites=%v", fake.readFloats, fake.readInts, fake.bulkReads, fake.floatWrites, fake.intWrites)
	}
}

func TestDeviceClientBridgeVirtualParameterPrefersLiveValueAndCaches(t *testing.T) {
	ctx := context.Background()
	key := fakeParamKey{id: 52200, instance: 1}
	fake := &fakeDeviceClient{
		floats: map[fakeParamKey]float64{key: 12.5},
	}
	state := newDeviceBridgeState("controller-live")
	wantHex := fmt.Sprintf("%08X", math.Float32bits(float32(12.5)))

	metadata, err := deviceBridgeMetadataWithState(ctx, state, fake, "?VMCBE801")
	if err != nil {
		t.Fatalf("read live virtual metadata: %v", err)
	}
	if !strings.HasSuffix(metadata, wantHex) {
		t.Fatalf("live virtual metadata = %q, want actual suffix %q", metadata, wantHex)
	}

	fake.floats = map[fakeParamKey]float64{key: 18.25}
	wantHex = fmt.Sprintf("%08X", math.Float32bits(float32(18.25)))
	bulk, err := deviceBridgeBulkReadWithState(ctx, state, fake, "?VX01CBE801")
	if err != nil {
		t.Fatalf("bulk read live virtual parameter: %v", err)
	}
	if bulk != wantHex {
		t.Fatalf("live virtual bulk read = %q, want %q", bulk, wantHex)
	}

	fake.floats = map[fakeParamKey]float64{}
	fake.floatErrs = map[fakeParamKey]error{key: mecom.ErrUnknownParameter}
	got, err := deviceBridgeSingleReadWithState(ctx, state, fake, "?VRCBE801")
	if err != nil {
		t.Fatalf("read cached virtual parameter: %v", err)
	}
	if got != wantHex {
		t.Fatalf("cached virtual parameter read = %q, want %q", got, wantHex)
	}
	got, err = deviceBridgeBulkReadWithState(ctx, state, fake, "?VX01CBE801")
	if err != nil {
		t.Fatalf("bulk read cached virtual parameter: %v", err)
	}
	if got != wantHex {
		t.Fatalf("cached virtual bulk read = %q, want %q", got, wantHex)
	}
	if len(fake.readFloats) != 4 {
		t.Fatalf("readFloats = %+v, want live probe before cached fallback", fake.readFloats)
	}
	for _, got := range fake.readFloats {
		if got != key {
			t.Fatalf("readFloats = %+v, want only %+v", fake.readFloats, key)
		}
	}
}

func TestDeviceClientBridgeVirtualParameterStateIsPerController(t *testing.T) {
	deviceBridgeVirtualParameters.reset()

	ctx := context.Background()
	fake := &fakeDeviceClient{
		floatErrs: map[fakeParamKey]error{{id: 52201, instance: 1}: mecom.ErrUnknownParameter},
	}
	stateA := newDeviceBridgeState("controller-a")
	stateB := newDeviceBridgeState("controller-b")
	valueHex := fmt.Sprintf("%08X", math.Float32bits(float32(12.5)))
	defaultHex := fmt.Sprintf("%08X", math.Float32bits(float32(25)))

	if err := deviceBridgeWriteWithState(ctx, stateA, fake, "VSCBE901"+valueHex); err != nil {
		t.Fatalf("write controller A virtual parameter: %v", err)
	}
	gotA, err := deviceBridgeSingleReadWithState(ctx, stateA, fake, "?VRCBE901")
	if err != nil {
		t.Fatalf("read controller A virtual parameter: %v", err)
	}
	if gotA != valueHex {
		t.Fatalf("controller A virtual parameter = %q, want %q", gotA, valueHex)
	}
	gotB, err := deviceBridgeSingleReadWithState(ctx, stateB, fake, "?VRCBE901")
	if err != nil {
		t.Fatalf("read controller B virtual parameter: %v", err)
	}
	if gotB != defaultHex {
		t.Fatalf("controller B virtual parameter leaked controller A value: got %q, want %q", gotB, defaultHex)
	}
	bulkB, err := deviceBridgeBulkReadWithState(ctx, stateB, fake, "?VX01CBE901")
	if err != nil {
		t.Fatalf("bulk read controller B virtual parameter: %v", err)
	}
	if bulkB != defaultHex {
		t.Fatalf("controller B bulk virtual parameter leaked controller A value: got %q, want %q", bulkB, defaultHex)
	}
	if len(fake.readFloats) == 0 {
		t.Fatalf("virtual parameter should probe downstream before using compatibility cache")
	}
	if len(fake.readInts) != 0 || len(fake.bulkReads) != 0 || len(fake.floatWrites) != 0 || len(fake.intWrites) != 0 {
		t.Fatalf("virtual parameter should not write or bulk-read downstream: floatReads=%v intReads=%v bulkReads=%v floatWrites=%v intWrites=%v", fake.readFloats, fake.readInts, fake.bulkReads, fake.floatWrites, fake.intWrites)
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
			want:    "03030100000100000000",
		},
		{
			name:    "limits",
			seq:     0x24,
			payload: "?VL007801",
			want:    "030000",
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

func TestDeviceClientBridgeAnswersMetadataAndEmptyBigDataForFreeRTOSStatistics(t *testing.T) {
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
			seq:     0x52,
			payload: "?VM00D901",
			want:    "03010100000100000000",
		},
		{
			name:    "limits",
			seq:     0x53,
			payload: "?VL00D901",
			want:    "030000",
		},
		{
			name:    "big-data-read-empty",
			seq:     0x54,
			payload: "?VB00D90100000000FFFF",
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
		t.Fatalf("FreeRTOS metadata/big-data probes should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeAnswersCoSoMeasuredTemperatureMetadata(t *testing.T) {
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

	const seq = 0x35
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VM041501")); err != nil {
		t.Fatalf("write metadata frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4c, seq, "00010100000001FF8000007F80000000000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("metadata probe should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
	}
}

func TestDeviceClientBridgeAnswersCoSoLRMeasuredTemperatureMetadata(t *testing.T) {
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

	const seq = 0x36
	if _, err := conn.Write(deviceBridgeTestFrame(0x4c, seq, "?VM041401")); err != nil {
		t.Fatalf("write metadata frame: %v", err)
	}
	buf := make([]byte, 128)
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read metadata response: %v", err)
	}
	want := deviceBridgeInfoTestFrame(0x4c, seq, "00010400000001FF8000007F80000000000000")
	if !bytes.Equal(buf[:n], want) {
		t.Fatalf("metadata response = %q, want %q", string(buf[:n]), string(want))
	}
	if len(fake.readFloats) != 0 || len(fake.readInts) != 0 || len(fake.bulkReads) != 0 {
		t.Fatalf("metadata probe should not hit typed reads: floats=%v ints=%v bulk=%v", fake.readFloats, fake.readInts, fake.bulkReads)
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
		{name: "trigger-sync", seq: 0x33, payload: "?RS0003", wantData: "00"},
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

func TestDeviceClientBridgeParameterWritability(t *testing.T) {
	catalogueWritability := mecom.CANopenMappedParameterWritability()
	if writable, ok := catalogueWritability[1000]; !ok || writable {
		t.Fatalf("SDO catalog writability[1000] = %v/%v, want read-only mapping", writable, ok)
	}
	if writable, ok := catalogueWritability[2010]; !ok || !writable {
		t.Fatalf("SDO catalog writability[2010] = %v/%v, want writable mapping", writable, ok)
	}
	if writable, ok := catalogueWritability[2033]; !ok || !writable {
		t.Fatalf("SDO catalog writability[2033] = %v/%v, want writable mapping outside CoSo profile", writable, ok)
	}

	if deviceBridgeParameterWritable(1000) {
		t.Errorf("Parameter 1000 should be read-only")
	}

	if !deviceBridgeParameterWritable(2010) {
		t.Errorf("Parameter 2010 should be writable")
	}

	if !deviceBridgeParameterWritable(2033) {
		t.Errorf("Parameter 2033 should be writable through the CoSo router profile")
	}
}
