package mecom

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRingReaderClient struct {
	configs    [][]RingCaptureParameter
	syncs      int
	pointers   []uint32
	pointerErr error
	chunk      RingReadResponse
	chunkErr   error
	chunks     int
	chunkMax   []uint16
}

func (f *fakeRingReaderClient) ConfigureRingCapture(_ context.Context, _ uint16, params []RingCaptureParameter) error {
	f.configs = append(f.configs, append([]RingCaptureParameter(nil), params...))
	return nil
}

func (f *fakeRingReaderClient) TriggerRingSync(context.Context) error {
	f.syncs++
	return nil
}

func (f *fakeRingReaderClient) ReadRingPointer(context.Context) (uint32, error) {
	if f.pointerErr != nil {
		return 0, f.pointerErr
	}
	if len(f.pointers) == 0 {
		return 0, nil
	}
	ptr := f.pointers[0]
	f.pointers = f.pointers[1:]
	return ptr, nil
}

func (f *fakeRingReaderClient) ReadRingChunk(_ context.Context, _ uint32, maxBytes uint16) (RingReadResponse, error) {
	f.chunks++
	f.chunkMax = append(f.chunkMax, maxBytes)
	if f.chunkErr != nil {
		return RingReadResponse{}, f.chunkErr
	}
	return f.chunk, nil
}

func TestRingReaderDisablesAfterRepeatedPointerFailures(t *testing.T) {
	client := &fakeRingReaderClient{pointerErr: errors.New(`mecom: invalid ring pointer payload "05"`)}
	reader := newRingReader(client, time.Second, nil, nil)

	for i := 0; i < ringReaderConsecutiveFailureLimit; i++ {
		reader.fetch()
	}

	if !reader.Disabled() {
		t.Fatal("reader should disable itself after repeated pointer failures")
	}
	if got := reader.DisabledReason(); got == nil {
		t.Fatal("disabled reason is nil")
	}
	if client.chunks != 0 {
		t.Fatalf("chunks read = %d, want 0", client.chunks)
	}
}

func TestRingReaderDisablesAfterRepeatedCursorResets(t *testing.T) {
	client := &fakeRingReaderClient{
		pointers: []uint32{90, 80, 70, 60},
	}
	reader := newRingReader(client, time.Second, nil, nil)
	reader.lastPointer = 100

	for i := 0; i < ringReaderConsecutiveResetLimit; i++ {
		reader.fetch()
	}

	if !reader.Disabled() {
		t.Fatal("reader should disable itself after repeated cursor resets")
	}
	if client.chunks != 0 {
		t.Fatalf("chunks read = %d, want 0", client.chunks)
	}
}

func TestRingReaderDisablesAfterFrequentCursorResetsInWindow(t *testing.T) {
	pointers := make([]uint32, 0, ringReaderResetWindowLimit*2)
	for i := 1; i <= ringReaderResetWindowLimit; i++ {
		ptr := uint32(100 + i*10)
		pointers = append(pointers, ptr, ptr)
	}
	client := &fakeRingReaderClient{
		pointers: pointers,
		chunk: RingReadResponse{
			BytesAdded: 0,
			Status:     RingStatusOverlap,
		},
	}
	reader := newRingReader(client, time.Second, nil, nil)
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	reader.now = func() time.Time { return now }
	reader.lastPointer = 100

	for i := 0; i < ringReaderResetWindowLimit*2; i++ {
		reader.fetch()
		now = now.Add(time.Second)
	}

	if !reader.Disabled() {
		t.Fatal("reader should disable itself after frequent cursor resets in a short window")
	}
	if client.chunks != ringReaderResetWindowLimit {
		t.Fatalf("chunks read = %d, want %d", client.chunks, ringReaderResetWindowLimit)
	}
}

func TestRingReaderUsesControllerReadLimit(t *testing.T) {
	client := &fakeRingReaderClient{
		pointers: []uint32{1024},
		chunk: RingReadResponse{
			BytesAdded: MaxRingReadMaxBytes,
			Status:     RingStatusHasMoreData,
		},
	}
	reader := newRingReader(client, time.Second, nil, nil)

	reader.fetch()

	if client.chunks == 0 {
		t.Fatal("expected ring chunks to be read")
	}
	for _, maxBytes := range client.chunkMax {
		if maxBytes > MaxRingReadMaxBytes {
			t.Fatalf("ring read max = %d, want <= %d", maxBytes, MaxRingReadMaxBytes)
		}
	}
}

func TestRingReaderResetsWhenBacklogExceedsDeviceRing(t *testing.T) {
	client := &fakeRingReaderClient{
		pointers: []uint32{RingBufferSizeBytes + 1},
	}
	reader := newRingReader(client, time.Second, nil, nil)

	reader.fetch()

	if client.chunks != 0 {
		t.Fatalf("chunks read = %d, want 0 when backlog already exceeded device ring", client.chunks)
	}
	if reader.lastPointer != RingBufferSizeBytes+1 {
		t.Fatalf("last pointer = %d, want reset to producer pointer", reader.lastPointer)
	}
}

func TestStampRingFrameSamplesUsesFrameOrderAndTimestampWrap(t *testing.T) {
	observedAt := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)
	frames := []RingFrame{
		{
			Timestamp10us: 65000,
			Samples: []RingSample{
				{ConfigIndex: 0, Type: DataTypeFloat32, Value: 21.0},
			},
		},
		{
			Timestamp10us: 10,
			Samples: []RingSample{
				{ConfigIndex: 1, Type: DataTypeFloat32, Value: 22.0},
			},
		},
	}

	samples := stampRingFrameSamples(frames, observedAt)
	if len(samples) != 2 {
		t.Fatalf("samples len = %d, want 2", len(samples))
	}
	if !samples[1].At.Equal(observedAt) {
		t.Fatalf("newest sample At = %s, want %s", samples[1].At, observedAt)
	}
	wantOlderAt := observedAt.Add(-546 * 10 * time.Microsecond)
	if !samples[0].At.Equal(wantOlderAt) {
		t.Fatalf("older sample At = %s, want %s", samples[0].At, wantOlderAt)
	}
}
