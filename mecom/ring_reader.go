package mecom

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// RingReader automates oversampling by reading the device's internal CRTVStream buffer.
type RingReader struct {
	Client  *Client
	Config  []RingCaptureParameter
	Samples chan<- RingSample

	mu          sync.Mutex
	lastPointer uint32
	quit        chan struct{}
	stopOnce    sync.Once
}

func NewRingReader(client *Client, config []RingCaptureParameter, samples chan<- RingSample) *RingReader {
	return &RingReader{
		Client:  client,
		Config:  config,
		Samples: samples,
		quit:    make(chan struct{}),
	}
}

func (r *RingReader) Start(ctx context.Context) error {
	// 1. Configure the device's ring buffer capture
	if err := r.Client.ConfigureRingCapture(ctx, 1, r.Config); err != nil {
		return fmt.Errorf("ring: config failed: %w", err)
	}

	// 2. Trigger sync to start capture
	if err := r.Client.TriggerRingSync(ctx); err != nil {
		return fmt.Errorf("ring: sync failed: %w", err)
	}

	// 3. Get initial pointer
	ptr, err := r.Client.ReadRingPointer(ctx)
	if err != nil {
		return fmt.Errorf("ring: initial pointer failed: %w", err)
	}
	r.lastPointer = ptr

	go r.poll()
	return nil
}

func (r *RingReader) Stop() {
	r.stopOnce.Do(func() {
		close(r.quit)
	})
}

func (r *RingReader) poll() {
	ticker := time.NewTicker(200 * time.Millisecond)
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
	ctx, cancel := context.WithTimeout(context.Background(), r.Client.timeout)
	defer cancel()

	// Get current pointer
	ptr, err := r.Client.ReadRingPointer(ctx)
	if err != nil {
		log.Printf("ring: pointer fetch failed: %v", err)
		return
	}

	if ptr == r.lastPointer {
		return
	}

	// Calculate how much to read
	// Note: MeCom ring is circular, but for simplicity we assume it doesn't wrap
	// in 200ms. A more robust implementation would handle wrapping.
	diff := uint32(0)
	if ptr > r.lastPointer {
		diff = ptr - r.lastPointer
	} else {
		// Wrap around (assuming 32-bit pointer or max buffer size)
		// This needs exact buffer size knowledge from the device.
		// For now, reset to current.
		r.lastPointer = ptr
		return
	}

	if diff > 1024 {
		diff = 1024 // Limit chunk size
	}

	resp, err := r.Client.ReadRingChunk(ctx, r.lastPointer, uint16(diff))
	if err != nil {
		log.Printf("ring: chunk read failed: %v", err)
		return
	}

	if resp.BytesAdded > 0 {
		frames, _, err := ParseRingFrames(resp.Data, r.Config)
		if err != nil {
			log.Printf("ring: parse failed: %v", err)
		} else {
			for _, f := range frames {
				for _, s := range f.Samples {
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
		}
		r.lastPointer += uint32(resp.BytesAdded)
	}
}
