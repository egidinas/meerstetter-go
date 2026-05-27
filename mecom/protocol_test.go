package mecom

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func responseFrame(prefix string) []byte {
	return []byte(fmt.Sprintf("%s%04X%c", prefix, CRC16([]byte(prefix)), FrameTerminator))
}

func TestBuildSingleGetFrameFormat(t *testing.T) {
	frame := BuildSingleGetFrame(0x50, 1, 1000, 1)
	s := string(frame)
	if !strings.HasPrefix(s, "#") {
		t.Fatalf("frame must start with #: %q", s)
	}
	if s[len(s)-1] != '\r' {
		t.Fatalf("frame must end with CR: %q", s)
	}
	if !strings.Contains(s, "?VR03E801") {
		t.Fatalf("frame missing ?VR03E801: %q", s)
	}
	if crcHex := s[len(s)-5 : len(s)-1]; crcHex == "0000" {
		t.Fatalf("crc appears zero: %q", s)
	}
}

func TestReferenceCRCVector(t *testing.T) {
	payload := []byte("#0015AB?VR03E801")
	if got := CRC16(payload); got != 0xC21A {
		t.Fatalf("CRC16(%q) = %04X, want C21A", payload, got)
	}
}

func TestBuildSingleGetFrameReferenceVector(t *testing.T) {
	got := string(BuildSingleGetFrame(0x00, 0x15AB, 1000, 1))
	const want = "#0015AB?VR03E801C21A\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildSaveToFlashFrameReferenceVector(t *testing.T) {
	got := string(BuildSaveToFlashFrame(0x00, 0x0001))
	const want = "#000001SP69EE\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildResetFrameReferenceVector(t *testing.T) {
	got := string(BuildResetFrame(0x00, 0x0002))
	const want = "#000002RS33EC\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildSetBigDataStringFrameFormat(t *testing.T) {
	frame, err := BuildSetBigDataStringFrame(0x00, 0x0003, 120, 1, 0, "SN76 note", true)
	if err != nil {
		t.Fatalf("BuildSetBigDataStringFrame: %v", err)
	}
	got := string(frame)
	if !strings.HasPrefix(got, "#000003VB00780100000000000A01") {
		t.Fatalf("frame prefix = %q", got)
	}
	if !strings.Contains(got, "534E3736206E6F746500") {
		t.Fatalf("frame missing LATIN1 payload plus NUL: %q", got)
	}
	if got[len(got)-1] != '\r' {
		t.Fatalf("frame must end with CR: %q", got)
	}
}

func TestBuildSetBigDataStringFrameRejectsNonLatin1(t *testing.T) {
	_, err := BuildSetBigDataStringFrame(0x00, 0x0003, 120, 1, 0, "snowman ☃", true)
	if err == nil || !strings.Contains(err.Error(), "outside LATIN1") {
		t.Fatalf("err=%v, want LATIN1 rejection", err)
	}
}

func TestParseSingleResponseFloat(t *testing.T) {
	got, err := ParseSingleResponse(responseFrame("!500001+41CC0000"), DataTypeFloat32)
	if err != nil {
		t.Fatal(err)
	}
	if got != 25.5 {
		t.Fatalf("value = %v", got)
	}
}

func TestParseSingleResponseInt(t *testing.T) {
	got, err := ParseSingleResponse(responseFrame("!500001+FFFFFFFE"), DataTypeInt32)
	if err != nil {
		t.Fatal(err)
	}
	if got != -2 {
		t.Fatalf("value = %v", got)
	}
}

func TestParseNACK(t *testing.T) {
	_, err := ParseSingleResponse(responseFrame("!500001-05"), DataTypeFloat32)
	if err == nil || !strings.Contains(err.Error(), "PAR_NOT_AVAILABLE") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRejectsBadCRC(t *testing.T) {
	_, err := ParseSingleResponse([]byte("!500001+41CC0000ABCD\r"), DataTypeFloat32)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "crc") {
		t.Fatalf("ParseSingleResponse accepted bad CRC, err=%v", err)
	}
}

func TestClientReadFloat32(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBuffer(responseFrame("!500001+41CC0000"))}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
	got, err := client.ReadFloat32(context.Background(), 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != 25.5 {
		t.Fatalf("value = %v", got)
	}
	if !strings.Contains(rw.written.String(), "?VR03E801") {
		t.Fatalf("request = %q", rw.written.String())
	}
}

func TestClientWriteBigDataString(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBuffer(responseFrame("!500001+"))}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
	if err := client.WriteBigDataString(context.Background(), 120, 1, "SN76"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rw.written.String(), "VB00780100000000000501534E373600") {
		t.Fatalf("request = %q", rw.written.String())
	}
}

func TestClientConcurrentRequestsAllocateSequenceUnderLock(t *testing.T) {
	const requests = 32
	var responses bytes.Buffer
	for range requests {
		responses.Write(responseFrame("!500001+41CC0000"))
	}
	rw := &scriptedReadWriter{read: &responses}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})

	var wg sync.WaitGroup
	errs := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.ReadFloat32(context.Background(), 1000, 1)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	frames := SplitFrames(rw.written.Bytes())
	if len(frames) != requests {
		t.Fatalf("frames = %d, want %d", len(frames), requests)
	}
	seen := map[string]bool{}
	for _, frame := range frames {
		if len(frame) < 7 {
			t.Fatalf("short frame %q", frame)
		}
		seq := string(frame[3:7])
		if seen[seq] {
			t.Fatalf("duplicate sequence %s in %q", seq, rw.written.String())
		}
		seen[seq] = true
	}
	for i := 1; i <= requests; i++ {
		seq := fmt.Sprintf("%04X", i)
		if !seen[seq] {
			t.Fatalf("missing sequence %s in %q", seq, rw.written.String())
		}
	}
}

func TestBuildBulkGetFrameFormat(t *testing.T) {
	got := string(BuildBulkGetFrame(0x50, 1, []Parameter{
		{ID: 1000, Instance: 1},
		{ID: 1022, Instance: 4},
	}))
	if !strings.Contains(got, "?VX0203E80103FE04") {
		t.Fatalf("bulk frame = %q", got)
	}
}

func TestClientReadBulk(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBuffer(responseFrame("!50000141CC00000000002A"))}
	client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
	got, err := client.ReadBulk(context.Background(), []Parameter{
		{ID: 1000, Instance: 1, Type: DataTypeFloat32},
		{ID: 2000, Instance: 1, Type: DataTypeInt32},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != 25.5 || got[1] != 42 {
		t.Fatalf("values = %#v", got)
	}
	if !strings.Contains(rw.written.String(), "?VX0203E80107D001") {
		t.Fatalf("request = %q", rw.written.String())
	}
}

func TestClientRejectsOutOfRangeParameterAddressBeforeWrite(t *testing.T) {
	tests := []struct {
		name string
		op   func(*Client) error
	}{
		{
			name: "negative param",
			op: func(c *Client) error {
				_, err := c.ReadFloat32(context.Background(), -1, 1)
				return err
			},
		},
		{
			name: "too large param",
			op: func(c *Client) error {
				_, err := c.ReadFloat32(context.Background(), 0x10000, 1)
				return err
			},
		},
		{
			name: "zero instance",
			op: func(c *Client) error {
				_, err := c.ReadFloat32(context.Background(), 1000, 0)
				return err
			},
		},
		{
			name: "too large instance",
			op: func(c *Client) error {
				return c.WriteFloat32(context.Background(), 1000, 0x100, 1)
			},
		},
		{
			name: "invalid bulk member",
			op: func(c *Client) error {
				_, err := c.ReadBulk(context.Background(), []Parameter{
					{ID: 1000, Instance: 1, Type: DataTypeFloat32},
					{ID: -1, Instance: 1, Type: DataTypeFloat32},
				})
				return err
			},
		},
		{
			name: "invalid big-data instance",
			op: func(c *Client) error {
				return c.WriteBigDataString(context.Background(), 120, 0, "SN76")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rw := &rejectingReadWriter{}
			client := NewClient(rw, ClientConfig{Address: 0x50, Timeout: time.Second})
			err := tt.op(client)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err=%v, want ErrInvalidArgument", err)
			}
			if rw.writes != 0 {
				t.Fatalf("writes=%d, want 0", rw.writes)
			}
		})
	}
}

func TestClientRecoversAfterTimedOutRead(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	client := NewClient(clientConn, ClientConfig{Address: 0x50, Timeout: 20 * time.Millisecond})
	firstRead := make(chan struct{})
	go func() {
		defer close(firstRead)
		_, _ = readFrame(serverConn)
	}()
	if _, err := client.ReadFloat32(context.Background(), 1000, 1); err == nil {
		t.Fatal("expected first read to time out")
	}
	<-firstRead

	secondDone := make(chan error, 1)
	go func() {
		_, err := readFrame(serverConn)
		if err != nil {
			secondDone <- err
			return
		}
		_, err = serverConn.Write(responseFrame("!500002+41CC0000"))
		secondDone <- err
	}()
	got, err := client.ReadFloat32(context.Background(), 1000, 1)
	if err != nil {
		t.Fatalf("second read failed after timeout: %v", err)
	}
	if got != 25.5 {
		t.Fatalf("second value = %v", got)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("server side failed: %v", err)
	}
}

func TestBuildRingPointerFrameReferenceVector(t *testing.T) {
	got := string(BuildRingPointerFrame(0x00, 0x8532))
	const want = "#008532?RS00005984\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestBuildRingReadFrameReferenceVector(t *testing.T) {
	got := string(BuildRingReadFrame(0x00, 0x8564, 0x00000180, 0xFFFF))
	const want = "#008564?RS000100000180FFFF7FFA\r"
	if got != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestClientReadRingChunkClampsToControllerLimit(t *testing.T) {
	rw := &scriptedReadWriter{read: bytes.NewBuffer(responseFrame("!000001000000"))}
	client := NewClient(rw, ClientConfig{})

	resp, err := client.ReadRingChunk(context.Background(), 0x00000180, 0xFFFF)
	if err != nil {
		t.Fatalf("ReadRingChunk failed: %v", err)
	}
	if resp.BytesAdded != 0 || resp.Status != RingStatusAllDataRead {
		t.Fatalf("response = %#v", resp)
	}
	want := string(BuildRingReadFrame(0x00, 0x0001, 0x00000180, MaxRingReadMaxBytes))
	if got := rw.written.String(); got != want {
		t.Fatalf("written frame = %q, want clamped frame %q", got, want)
	}
}

func TestParseRingPointerResponse(t *testing.T) {
	got, err := ParseRingPointerResponse(responseFrame("!00853200000180"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 384 {
		t.Fatalf("pointer = %d", got)
	}
}

func TestParseRingReadResponseAndFrames(t *testing.T) {
	resp, err := ParseRingReadResponse(responseFrame("!0085640006008800EF3E8810"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.BytesAdded != 6 || resp.Status != RingStatusAllDataRead {
		t.Fatalf("response = %#v", resp)
	}
	frames, tail, err := ParseRingFrames(resp.Data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d tail=% X", len(frames), tail)
	}
	if frames[0].Timestamp10us != 0x3EEF || len(frames[0].Samples) != 0 {
		t.Fatalf("frame = %#v", frames[0])
	}
}

func TestParseRingFramesWithFloatAndIntSamples(t *testing.T) {
	raw := []byte{0x88, 0x00, 0x34, 0x12, 0x00, 0xD0, 0x83, 0xDA, 0x41, 0x85, 0x02, 0x39, 0x30, 0x00, 0x00, 0x88, 0x10}
	config := []RingCaptureParameter{
		{Parameter: Parameter{Type: DataTypeFloat32}},
		{}, {}, {}, {},
		{Parameter: Parameter{Type: DataTypeInt32}},
	}
	frames, tail, err := ParseRingFrames(raw, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) != 0 || len(frames) != 1 {
		t.Fatalf("frames=%d tail=% X", len(frames), tail)
	}
	frame := frames[0]
	if frame.Timestamp10us != 0x1234 || len(frame.Samples) != 2 {
		t.Fatalf("frame = %#v", frame)
	}
	if math.Abs(frame.Samples[0].Value-27.314362) > 0.00001 {
		t.Fatalf("float sample = %v", frame.Samples[0].Value)
	}
	if frame.Samples[1].ConfigIndex != 5 || frame.Samples[1].Value != 12345 {
		t.Fatalf("int sample = %#v", frame.Samples[1])
	}
}

type scriptedReadWriter struct {
	read    *bytes.Buffer
	written bytes.Buffer
}

func (s *scriptedReadWriter) Read(p []byte) (int, error) {
	return s.read.Read(p)
}

func (s *scriptedReadWriter) Write(p []byte) (int, error) {
	return s.written.Write(p)
}

type rejectingReadWriter struct {
	writes int
}

func (r *rejectingReadWriter) Read([]byte) (int, error) {
	return 0, errors.New("read should not happen")
}

func (r *rejectingReadWriter) Write([]byte) (int, error) {
	r.writes++
	return 0, errors.New("write should not happen")
}
