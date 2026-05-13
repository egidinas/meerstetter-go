package mecom

import (
	"math"
	"testing"
	"time"
)

func TestPollQueueRotatesNormalParameters(t *testing.T) {
	p1 := Parameter{ID: 1000, Instance: 1, Type: DataTypeFloat32}
	p2 := Parameter{ID: 1001, Instance: 1, Type: DataTypeFloat32}
	p3 := Parameter{ID: 1020, Instance: 1, Type: DataTypeFloat32}
	q := NewPollQueue([]Parameter{p1, p2, p3})

	first := q.NextChunk(2)
	second := q.NextChunk(2)

	if got := []int{first[0].ID, first[1].ID, second[0].ID, second[1].ID}; got[0] != 1000 || got[1] != 1001 || got[2] != 1020 || got[3] != 1000 {
		t.Fatalf("rotation = %#v", got)
	}
}

func TestPollQueueManualPollGoesToFront(t *testing.T) {
	normal := Parameter{ID: 1000, Instance: 1, Type: DataTypeFloat32}
	manual := Parameter{ID: 5000, Instance: 4, Type: DataTypeInt32}
	q := NewPollQueue([]Parameter{normal})

	q.EnqueueFront(manual)
	q.EnqueueFront(manual)
	chunk := q.NextChunk(3)

	if len(chunk) != 2 {
		t.Fatalf("chunk length = %d", len(chunk))
	}
	if chunk[0].ID != manual.ID || chunk[0].Instance != manual.Instance {
		t.Fatalf("manual parameter was not first: %#v", chunk)
	}
	if chunk[1].ID != normal.ID {
		t.Fatalf("normal parameter missing after manual: %#v", chunk)
	}
}

func TestPollQueueLatestReturnsValueWhenItComesAround(t *testing.T) {
	param := Parameter{ID: 1022, Instance: 2, Type: DataTypeFloat32}
	q := NewPollQueue([]Parameter{param})
	observedAt := time.Unix(100, 0)

	q.Record(PollResult{Parameter: param, Value: 12.5, ObservedAt: observedAt})
	got, ok := q.Latest(param)
	if !ok {
		t.Fatal("missing latest result")
	}
	if got.Value != 12.5 || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("latest = %#v", got)
	}
}

func TestPollQueueInitializesLatestForAllRoundRobinParameters(t *testing.T) {
	p1 := Parameter{ID: 1000, Instance: 1, Type: DataTypeFloat32}
	p2 := Parameter{ID: 3000, Instance: 4, Type: DataTypeFloat32}
	q := NewPollQueue([]Parameter{p1, p2})

	for _, param := range []Parameter{p1, p2} {
		got, ok := q.Latest(param)
		if !ok {
			t.Fatalf("missing initialized latest result for %#v", param)
		}
		if !math.IsNaN(got.Value) || got.Error != "not_sampled" {
			t.Fatalf("initialized latest = %#v, want NaN/not_sampled", got)
		}
	}
}
