package mecom

import "math"

// RingReductionPolicy names the consumer-facing reduction applied to high-rate
// CRTVStream samples before publishing at the requested consumer rate.
type RingReductionPolicy string

const (
	RingReductionMeanStdDev RingReductionPolicy = "mean_stddev_window"
)

// ReducedRingSample is a SNR-improving aggregate for one capture slot over a
// consumer-rate window. Count and StdDev preserve enough context to judge noise.
type ReducedRingSample struct {
	ConfigIndex        int
	Type               DataType
	Policy             RingReductionPolicy
	Count              int
	Mean               float64
	Min                float64
	Max                float64
	StdDev             float64
	FirstTimestamp10us uint16
	LastTimestamp10us  uint16
}

// ReduceRingSamples folds all samples for one capture index across the supplied
// ring frames. Callers choose the window boundaries from the consumer rate.
func ReduceRingSamples(frames []RingFrame, configIndex int) (ReducedRingSample, bool) {
	acc := newRingReductionAccumulator(configIndex)
	for _, frame := range frames {
		for _, sample := range frame.Samples {
			if sample.ConfigIndex != configIndex || math.IsNaN(sample.Value) {
				continue
			}
			acc.add(frame, sample)
		}
	}
	return acc.result()
}

// ReduceRingSamplesForIndices folds samples for all requested capture indices in
// one frame pass. It is equivalent to calling ReduceRingSamples for each index,
// but avoids repeatedly scanning high-rate ring windows.
func ReduceRingSamplesForIndices(frames []RingFrame, configIndices []int) map[int]ReducedRingSample {
	accumulators := make(map[int]*ringReductionAccumulator, len(configIndices))
	for _, configIndex := range configIndices {
		if _, ok := accumulators[configIndex]; !ok {
			accumulators[configIndex] = newRingReductionAccumulator(configIndex)
		}
	}
	for _, frame := range frames {
		for _, sample := range frame.Samples {
			if math.IsNaN(sample.Value) {
				continue
			}
			acc, ok := accumulators[sample.ConfigIndex]
			if !ok {
				continue
			}
			acc.add(frame, sample)
		}
	}
	reduced := make(map[int]ReducedRingSample, len(accumulators))
	for configIndex, acc := range accumulators {
		if sample, ok := acc.result(); ok {
			reduced[configIndex] = sample
		}
	}
	return reduced
}

type ringReductionAccumulator struct {
	reduced ReducedRingSample
	m2      float64
}

func newRingReductionAccumulator(configIndex int) *ringReductionAccumulator {
	return &ringReductionAccumulator{
		reduced: ReducedRingSample{
			ConfigIndex: configIndex,
			Policy:      RingReductionMeanStdDev,
			Min:         math.Inf(1),
			Max:         math.Inf(-1),
		},
	}
}

func (acc *ringReductionAccumulator) add(frame RingFrame, sample RingSample) {
	if acc.reduced.Count == 0 {
		acc.reduced.Type = sample.Type
		acc.reduced.FirstTimestamp10us = frame.Timestamp10us
	}
	acc.reduced.Count++
	acc.reduced.LastTimestamp10us = frame.Timestamp10us
	if sample.Value < acc.reduced.Min {
		acc.reduced.Min = sample.Value
	}
	if sample.Value > acc.reduced.Max {
		acc.reduced.Max = sample.Value
	}
	delta := sample.Value - acc.reduced.Mean
	acc.reduced.Mean += delta / float64(acc.reduced.Count)
	acc.m2 += delta * (sample.Value - acc.reduced.Mean)
}

func (acc *ringReductionAccumulator) result() (ReducedRingSample, bool) {
	if acc.reduced.Count == 0 {
		return ReducedRingSample{}, false
	}
	reduced := acc.reduced
	if reduced.Count > 1 {
		reduced.StdDev = math.Sqrt(acc.m2 / float64(reduced.Count-1))
	}
	return reduced, true
}
