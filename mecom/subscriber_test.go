package mecom

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type scriptedReadClient struct {
	values map[int]float64
	err    error
	calls  int
}

func (s *scriptedReadClient) ReadBulk(_ context.Context, params []Parameter) ([]float64, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]float64, 0, len(params))
	for _, p := range params {
		if v, ok := s.values[p.ID]; ok {
			out = append(out, v)
		} else {
			out = append(out, math.NaN())
		}
	}
	return out, nil
}

func (s *scriptedReadClient) ConfigureRingCapture(context.Context, uint16, []RingCaptureParameter) error {
	return ErrTransportNotSupported
}
func (s *scriptedReadClient) TriggerRingSync(context.Context) error              { return ErrTransportNotSupported }
func (s *scriptedReadClient) ReadRingPointer(context.Context) (uint32, error)    { return 0, ErrTransportNotSupported }
func (s *scriptedReadClient) ReadRingChunk(context.Context, uint32, uint16) (RingReadResponse, error) {
	return RingReadResponse{}, ErrTransportNotSupported
}

func TestSubscriberEmitsOneTelemetryPerParameter(t *testing.T) {
	src := &scriptedReadClient{values: map[int]float64{1000: 24.5, 3000: 25.0}}
	sub := NewSubscriber(src, SubscriberConfig{
		TargetID:   "tec-75",
		Parameters: []Parameter{{ID: 1000, Instance: 1, Name: "obj"}, {ID: 3000, Instance: 1, Name: "tgt"}},
		Interval:   10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	go sub.Run(ctx)

	received := make([]string, 0, 4)
	values := make([]float64, 0, 4)
	for tm := range sub.C() {
		received = append(received, tm.Name)
		if v, ok := tm.Value.(float64); ok {
			values = append(values, v)
		}
		if len(received) >= 4 {
			cancel()
		}
	}
	if len(received) < 2 {
		t.Fatalf("got %d Telemetry events, want at least 2", len(received))
	}
	if received[0] != "obj" || received[1] != "tgt" {
		t.Fatalf("order=%v, want [obj, tgt, ...]", received)
	}
	if len(values) < 2 || values[0] != 24.5 || values[1] != 25.0 {
		t.Fatalf("values=%v, want [24.5, 25.0, ...]", values)
	}
}

func TestSubscriberMarksUnreachableOnReadError(t *testing.T) {
	src := &scriptedReadClient{err: errors.New("boom")}
	sub := NewSubscriber(src, SubscriberConfig{
		TargetID:   "tec-75",
		Parameters: []Parameter{{ID: 1000, Instance: 1, Name: "obj"}},
		Interval:   10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	go sub.Run(ctx)

	var got string
	for tm := range sub.C() {
		got = tm.Quality
		cancel()
	}
	if got != "unreachable" {
		t.Fatalf("quality=%q, want unreachable", got)
	}
}

func TestSubscriberMarksNaN(t *testing.T) {
	src := &scriptedReadClient{values: map[int]float64{}} // ReadBulk returns NaN
	sub := NewSubscriber(src, SubscriberConfig{
		TargetID:   "tec-75",
		Parameters: []Parameter{{ID: 1000, Instance: 1, Name: "obj"}},
		Interval:   10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	go sub.Run(ctx)
	var got string
	for tm := range sub.C() {
		got = tm.Quality
		cancel()
	}
	if got != "nan" {
		t.Fatalf("quality=%q, want nan", got)
	}
}
