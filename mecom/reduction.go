package mecom

import "github.com/egidinas/signalforge/stats"

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
			if sample.ConfigIndex != configIndex {
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
	window  stats.Window
}

func newRingReductionAccumulator(configIndex int) *ringReductionAccumulator {
	return &ringReductionAccumulator{
		reduced: ReducedRingSample{
			ConfigIndex: configIndex,
			Policy:      RingReductionMeanStdDev,
		},
	}
}

func (acc *ringReductionAccumulator) add(frame RingFrame, sample RingSample) {
	if !acc.window.Add(sample.Value) {
		return
	}
	if acc.reduced.Count == 0 {
		acc.reduced.Type = sample.Type
		acc.reduced.FirstTimestamp10us = frame.Timestamp10us
	}
	acc.reduced.Count = acc.window.Count()
	acc.reduced.LastTimestamp10us = frame.Timestamp10us
}

func (acc *ringReductionAccumulator) result() (ReducedRingSample, bool) {
	summary, ok := acc.window.Snapshot()
	if !ok {
		return ReducedRingSample{}, false
	}
	reduced := acc.reduced
	reduced.Count = summary.Count
	reduced.Mean = summary.Mean
	reduced.Min = summary.Min
	reduced.Max = summary.Max
	reduced.StdDev = summary.StdDev
	return reduced, true
}
