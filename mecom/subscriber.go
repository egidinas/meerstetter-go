package mecom

import (
	"context"
	"math"
	"time"

	"github.com/egidinas/meerstetter-go/tmtc"
)

// SubscriberConfig configures one push-style telemetry stream.
type SubscriberConfig struct {
	// TargetID identifies the device in emitted Telemetry. Required.
	TargetID string

	// SessionID is copied verbatim into every emitted Telemetry, useful for
	// correlating with a higher-level run or campaign. Optional.
	SessionID string

	// Parameters lists the signals to poll on every interval. Each parameter
	// produces one Telemetry per cycle.
	Parameters []Parameter

	// Interval between full poll cycles. Defaults to 1s when zero.
	Interval time.Duration

	// Buffer is the capacity of the output channel. Defaults to len(Parameters)
	// so one full cycle fits without blocking.
	Buffer int
}

// Subscriber polls a ReadClient on a fixed interval and emits one
// tmtc.Telemetry per parameter per cycle on its output channel. On read
// failure it emits Telemetry with Quality set to "unreachable" but keeps
// streaming; transient failures do not terminate the subscription.
//
// The channel is owned by the Subscriber. It is closed when Run returns.
type Subscriber struct {
	cfg    SubscriberConfig
	client ReadClient
	out    chan tmtc.Telemetry
}

// NewSubscriber returns a Subscriber ready to Run. The output channel can
// be retrieved with C() and ranged over.
func NewSubscriber(client ReadClient, cfg SubscriberConfig) *Subscriber {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	buf := cfg.Buffer
	if buf <= 0 {
		buf = len(cfg.Parameters)
		if buf <= 0 {
			buf = 1
		}
	}
	return &Subscriber{
		cfg:    cfg,
		client: client,
		out:    make(chan tmtc.Telemetry, buf),
	}
}

// C returns the read-only telemetry channel. Always consume in a separate
// goroutine; if the consumer falls behind by more than Buffer items, samples
// are dropped from the head of the buffer (oldest first).
func (s *Subscriber) C() <-chan tmtc.Telemetry { return s.out }

// Run polls until ctx is cancelled. It always closes the output channel
// before returning. The returned error is the context cancellation cause,
// or nil for clean shutdown.
func (s *Subscriber) Run(ctx context.Context) error {
	defer close(s.out)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()

	// First cycle fires immediately; subsequent ones on the ticker.
	s.pollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.pollOnce(ctx)
		}
	}
}

func (s *Subscriber) pollOnce(ctx context.Context) {
	now := time.Now()
	values, err := s.client.ReadBulk(ctx, s.cfg.Parameters)
	for i, param := range s.cfg.Parameters {
		tm := tmtc.Telemetry{
			ID:        s.cfg.TargetID + ":" + ParameterKey(param) + ":" + now.Format(time.RFC3339Nano),
			TargetID:  s.cfg.TargetID,
			SessionID: s.cfg.SessionID,
			Time:      now,
			Name:      param.Name,
			Unit:      param.Unit,
		}
		switch {
		case err != nil:
			tm.Value = nil
			tm.Quality = "unreachable"
			tm.Metadata = map[string]string{"error": err.Error()}
		case i >= len(values):
			tm.Value = nil
			tm.Quality = "missing"
		case math.IsNaN(values[i]):
			tm.Value = nil
			tm.Quality = "nan"
		default:
			tm.Value = values[i]
			tm.Quality = "ok"
		}
		s.emit(ctx, tm)
		if ctx.Err() != nil {
			return
		}
	}
}

// emit pushes one Telemetry. Under back-pressure it drops the oldest queued
// item rather than blocking the poll loop, so a stalled consumer never
// stalls the device side.
func (s *Subscriber) emit(ctx context.Context, tm tmtc.Telemetry) {
	for {
		select {
		case <-ctx.Done():
			return
		case s.out <- tm:
			return
		default:
		}
		// Drop one stale item to make room, then retry.
		select {
		case <-s.out:
		default:
		}
	}
}
