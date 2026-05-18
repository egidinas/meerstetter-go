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

func TestReduceRingSamplesForIndicesMatchesSingleIndexReduction(t *testing.T) {
	frames := []RingFrame{
		{Timestamp10us: 100, Samples: []RingSample{
			{ConfigIndex: 0, Type: DataTypeFloat32, Value: 10},
			{ConfigIndex: 1, Type: DataTypeFloat32, Value: 20},
			{ConfigIndex: 2, Type: DataTypeFloat32, Value: math.NaN()},
		}},
		{Timestamp10us: 200, Samples: []RingSample{
			{ConfigIndex: 0, Type: DataTypeFloat32, Value: 14},
			{ConfigIndex: 1, Type: DataTypeFloat32, Value: 22},
			{ConfigIndex: 99, Type: DataTypeFloat32, Value: 30},
		}},
	}

	reduced := ReduceRingSamplesForIndices(frames, []int{0, 1, 2})

	for _, configIndex := range []int{0, 1} {
		want, ok := ReduceRingSamples(frames, configIndex)
		if !ok {
			t.Fatalf("expected single-index reduction for %d", configIndex)
		}
		if got := reduced[configIndex]; got != want {
			t.Fatalf("multi-index reduction for %d = %#v, want %#v", configIndex, got, want)
		}
	}
	if _, ok := reduced[2]; ok {
		t.Fatalf("NaN-only capture index should not produce a reduced sample: %#v", reduced[2])
	}
	if _, ok := reduced[99]; ok {
		t.Fatalf("unrequested capture index should not produce a reduced sample: %#v", reduced[99])
	}
}
