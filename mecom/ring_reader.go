package mecom

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	ringReaderPollInterval            = 200 * time.Millisecond
	ringReaderMaxChunksFetch          = 16
	ringReaderConsecutiveFailureLimit = 3
	ringReaderConsecutiveResetLimit   = 4
	ringReaderResetWindow             = 30 * time.Second
	ringReaderResetWindowLimit        = 8
	ringReaderDefaultRequestTimeout   = 2 * time.Second
	ringTimestampStep                 = 10 * time.Microsecond
)

type ringReaderClient interface {
	ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error
	TriggerRingSync(context.Context) error
	ReadRingPointer(context.Context) (uint32, error)
	ReadRingChunk(context.Context, uint32, uint16) (RingReadResponse, error)
}

// RingReader automates oversampling by reading the device's internal CRTVStream buffer.
type RingReader struct {
	Client  *Client
	Config  []RingCaptureParameter
	Samples chan<- RingSample

	client              ringReaderClient
	requestTimeout      time.Duration
	now                 func() time.Time
	mu                  sync.Mutex
	lastPointer         uint32
	tail                []byte
	consecutiveFailures int
	consecutiveResets   int
	resetWindowStarted  time.Time
	resetWindowResets   int
	disabled            bool
	disabledReason      error
	quit                chan struct{}
	stopOnce            sync.Once
}

func NewRingReader(client *Client, config []RingCaptureParameter, samples chan<- RingSample) *RingReader {
	return newRingReader(client, clientTimeout(client), config, samples)
}

func newRingReader(client ringReaderClient, timeout time.Duration, config []RingCaptureParameter, samples chan<- RingSample) *RingReader {
	if timeout <= 0 {
		timeout = ringReaderDefaultRequestTimeout
	}
	var concrete *Client
	if c, ok := client.(*Client); ok {
		concrete = c
	}
	return &RingReader{
		Client:         concrete,
		client:         client,
		Config:         config,
		Samples:        samples,
		requestTimeout: timeout,
		now:            time.Now,
		quit:           make(chan struct{}),
	}
}

func (r *RingReader) Start(ctx context.Context) error {
	client := r.ringClient()
	if client == nil {
		return fmt.Errorf("ring: client is nil")
	}
	if capable, ok := client.(RingReadoutCapability); ok && !capable.SupportsRingReadout() {
		return fmt.Errorf("ring: %w", ErrTransportNotSupported)
	}
	// 1. Configure the device's ring buffer capture
	if err := client.ConfigureRingCapture(ctx, 1, r.Config); err != nil {
		return fmt.Errorf("ring: config failed: %w", err)
	}

	// 2. Trigger sync to start capture
	if err := client.TriggerRingSync(ctx); err != nil {
		return fmt.Errorf("ring: sync failed: %w", err)
	}

	// 3. Get initial pointer
	ptr, err := client.ReadRingPointer(ctx)
	if err != nil {
		return fmt.Errorf("ring: initial pointer failed: %w", err)
	}
	r.mu.Lock()
	r.lastPointer = ptr
	r.tail = nil
	r.consecutiveFailures = 0
	r.consecutiveResets = 0
	r.resetWindowStarted = time.Time{}
	r.resetWindowResets = 0
	r.disabled = false
	r.disabledReason = nil
	r.mu.Unlock()

	go r.poll()
	return nil
}

func (r *RingReader) Stop() {
	r.stopOnce.Do(func() {
		close(r.quit)
	})
}

func (r *RingReader) Disabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disabled
}

func (r *RingReader) DisabledReason() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disabledReason
}

func (r *RingReader) poll() {
	ticker := time.NewTicker(ringReaderPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.quit:
			return
		case <-ticker.C:
			r.fetch()
		}
	}
}

func (r *RingReader) fetch() {
	if r.Disabled() {
		return
	}

	client := r.ringClient()
	if client == nil {
		r.disable(fmt.Errorf("ring client is nil"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout())
	defer cancel()

	// Get current pointer
	ptr, err := client.ReadRingPointer(ctx)
	if err != nil {
		r.noteFailure("pointer fetch", err)
		return
	}

	if ptr == r.lastPointer {
		r.noteHealthy()
		return
	}

	remaining, ok := ringPointerDistance(r.lastPointer, ptr)
	if !ok {
		if !r.noteCursorReset(fmt.Sprintf("pointer moved backwards from %d to %d", r.lastPointer, ptr)) {
			return
		}
		r.lastPointer = ptr
		r.tail = nil
		return
	}
	if remaining == 0 {
		return
	}
	if remaining > RingBufferSizeBytes {
		if !r.noteCursorReset(fmt.Sprintf("reader is %d bytes behind the %d-byte device ring", remaining, RingBufferSizeBytes)) {
			return
		}
		r.lastPointer = ptr
		r.tail = nil
		return
	}

	for chunks := 0; remaining > 0 && chunks < ringReaderMaxChunksFetch; chunks++ {
		maxBytes := uint16(MaxRingReadMaxBytes)
		if remaining < uint32(maxBytes) {
			maxBytes = uint16(remaining)
		}
		resp, err := client.ReadRingChunk(ctx, r.lastPointer, maxBytes)
		if err != nil {
			r.noteFailure("chunk read", err)
			return
		}
		if resp.Status == RingStatusOverlap {
			if !r.noteCursorReset(fmt.Sprintf("device reported overlap at pointer %d while producer pointer is %d", r.lastPointer, ptr)) {
				return
			}
			r.lastPointer = ptr
			r.tail = nil
			return
		}
		if resp.BytesAdded == 0 {
			r.noteHealthy()
			return
		}

		data := append(append([]byte(nil), r.tail...), resp.Data...)
		frames, tail, err := ParseRingFrames(data, r.Config)
		if err != nil {
			r.noteFailure("parse", err)
			r.tail = nil
		} else {
			r.noteHealthy()
			r.tail = tail
			for _, s := range stampRingFrameSamples(frames, r.nowTime().UTC()) {
				if r.Samples == nil {
					continue
				}
				select {
				case <-r.quit:
					return
				case r.Samples <- s:
				}
			}
		}
		r.lastPointer += uint32(resp.BytesAdded)
		if resp.Status == RingStatusAllDataRead {
			return
		}
		if nextRemaining, ok := ringPointerDistance(r.lastPointer, ptr); ok {
			remaining = nextRemaining
		} else {
			remaining = 0
		}
		if remaining == 0 && resp.Status == RingStatusHasMoreData {
			remaining = uint32(MaxRingReadMaxBytes)
		}
	}
}

func ringPointerDistance(from, to uint32) (uint32, bool) {
	if to >= from {
		return to - from, true
	}
	distance := uint64(to) + (uint64(1) << 32) - uint64(from)
	if distance <= RingBufferSizeBytes {
		return uint32(distance), true
	}
	return 0, false
}

func (r *RingReader) ringClient() ringReaderClient {
	if r == nil {
		return nil
	}
	if r.client != nil {
		return r.client
	}
	return r.Client
}

func (r *RingReader) timeout() time.Duration {
	if r != nil && r.requestTimeout > 0 {
		return r.requestTimeout
	}
	if r == nil {
		return ringReaderDefaultRequestTimeout
	}
	return clientTimeout(r.Client)
}

func clientTimeout(client *Client) time.Duration {
	if client != nil && client.timeout > 0 {
		return client.timeout
	}
	return ringReaderDefaultRequestTimeout
}

func (r *RingReader) noteFailure(operation string, err error) {
	r.mu.Lock()
	r.consecutiveFailures++
	r.consecutiveResets = 0
	failures := r.consecutiveFailures
	r.mu.Unlock()

	if failures >= ringReaderConsecutiveFailureLimit {
		r.disable(fmt.Errorf("%d consecutive %s failures: %w", failures, operation, err))
		return
	}
	log.Printf("ring: %s failed (%d/%d): %v", operation, failures, ringReaderConsecutiveFailureLimit, err)
}

func (r *RingReader) noteCursorReset(reason string) bool {
	now := r.nowTime()
	r.mu.Lock()
	r.consecutiveFailures = 0
	r.consecutiveResets++
	resets := r.consecutiveResets
	if r.resetWindowStarted.IsZero() || now.Sub(r.resetWindowStarted) > ringReaderResetWindow {
		r.resetWindowStarted = now
		r.resetWindowResets = 0
	}
	r.resetWindowResets++
	windowResets := r.resetWindowResets
	r.mu.Unlock()

	if resets >= ringReaderConsecutiveResetLimit {
		r.disable(fmt.Errorf("%d consecutive cursor resets: %s", resets, reason))
		return false
	}
	if windowResets >= ringReaderResetWindowLimit {
		r.disable(fmt.Errorf("%d cursor resets within %s: %s", windowResets, ringReaderResetWindow, reason))
		return false
	}
	log.Printf(
		"ring: %s; resetting reader cursor (%d/%d consecutive, %d/%d within %s)",
		reason,
		resets,
		ringReaderConsecutiveResetLimit,
		windowResets,
		ringReaderResetWindowLimit,
		ringReaderResetWindow,
	)
	return true
}

func (r *RingReader) noteHealthy() {
	r.mu.Lock()
	r.consecutiveFailures = 0
	r.consecutiveResets = 0
	r.mu.Unlock()
}

func (r *RingReader) nowTime() time.Time {
	if r != nil && r.now != nil {
		return r.now()
	}
	return time.Now()
}

func (r *RingReader) disable(reason error) {
	r.mu.Lock()
	if r.disabled {
		r.mu.Unlock()
		return
	}
	r.disabled = true
	r.disabledReason = reason
	r.mu.Unlock()

	log.Printf("ring: disabling reader: %v", reason)
	r.Stop()
}

func stampRingFrameSamples(frames []RingFrame, observedAt time.Time) []RingSample {
	var newest *RingFrame
	for i := range frames {
		if len(frames[i].Samples) == 0 {
			continue
		}
		newest = &frames[i]
	}
	if newest == nil {
		return nil
	}

	out := make([]RingSample, 0)
	for _, frame := range frames {
		if len(frame.Samples) == 0 {
			continue
		}
		at := observedAt.Add(-time.Duration(ringTimestampDelta(newest.Timestamp10us, frame.Timestamp10us)) * ringTimestampStep)
		for _, sample := range frame.Samples {
			sample.At = at
			out = append(out, sample)
		}
	}
	return out
}

func ringTimestampDelta(newest, older uint16) uint32 {
	if newest >= older {
		return uint32(newest - older)
	}
	return uint32(newest) + 1<<16 - uint32(older)
}
