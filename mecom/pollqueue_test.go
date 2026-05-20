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

func TestPollQueuePreservesNormalRoundRobinAcrossFrontPolls(t *testing.T) {
	p1 := Parameter{ID: 1000, Instance: 1, Type: DataTypeFloat32}
	p2 := Parameter{ID: 1001, Instance: 1, Type: DataTypeFloat32}
	p3 := Parameter{ID: 1002, Instance: 1, Type: DataTypeFloat32}
	manual := Parameter{ID: 5000, Instance: 4, Type: DataTypeInt32}
	q := NewPollQueue([]Parameter{p1, p2, p3})

	first := q.NextChunk(2)
	q.EnqueueFront(manual)
	second := q.NextChunk(3)
	third := q.NextChunk(2)

	got := []int{
		first[0].ID, first[1].ID,
		second[0].ID, second[1].ID, second[2].ID,
		third[0].ID, third[1].ID,
	}
	want := []int{1000, 1001, 5000, 1002, 1000, 1001, 1002}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round robin with front insertion = %#v, want %#v", got, want)
		}
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

func BenchmarkPollQueueNextChunkLarge(b *testing.B) {
	params := make([]Parameter, 4096)
	for i := range params {
		params[i] = Parameter{ID: 1000 + i, Instance: 1, Type: DataTypeFloat32}
	}
	q := NewPollQueue(params)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunk := q.NextChunk(len(params))
		if len(chunk) != len(params) {
			b.Fatalf("chunk length = %d, want %d", len(chunk), len(params))
		}
	}
}
