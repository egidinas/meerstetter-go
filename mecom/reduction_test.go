package mecom

import (
	"math"
	"testing"
)

func TestReduceRingSamplesAveragesConsumerWindowAndKeepsNoiseContext(t *testing.T) {
	frames := []RingFrame{
		{Timestamp10us: 100, Samples: []RingSample{{ConfigIndex: 0, Type: DataTypeFloat32, Value: 10}}},
		{Timestamp10us: 200, Samples: []RingSample{{ConfigIndex: 0, Type: DataTypeFloat32, Value: 12}}},
		{Timestamp10us: 300, Samples: []RingSample{{ConfigIndex: 0, Type: DataTypeFloat32, Value: 14}}},
	}

	got, ok := ReduceRingSamples(frames, 0)
	if !ok {
		t.Fatal("expected reduced sample")
	}
	if got.Policy != RingReductionMeanStdDev || got.Count != 3 {
		t.Fatalf("reduction metadata = %#v", got)
	}
	if got.Mean != 12 || got.Min != 10 || got.Max != 14 {
		t.Fatalf("reduced values = %#v", got)
	}
	if math.Abs(got.StdDev-2) > 0.000001 {
		t.Fatalf("stddev = %v, want 2", got.StdDev)
	}
	if got.FirstTimestamp10us != 100 || got.LastTimestamp10us != 300 {
		t.Fatalf("window timestamps = %#v", got)
	}
}

func TestReduceRingSamplesReturnsFalseForMissingCaptureIndex(t *testing.T) {
	_, ok := ReduceRingSamples([]RingFrame{
		{Samples: []RingSample{{ConfigIndex: 1, Type: DataTypeFloat32, Value: 10}}},
	}, 0)
	if ok {
		t.Fatal("expected no reduced sample")
	}
}
