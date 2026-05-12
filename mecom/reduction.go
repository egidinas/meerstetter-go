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
	reduced := ReducedRingSample{
		ConfigIndex: configIndex,
		Policy:      RingReductionMeanStdDev,
		Min:         math.Inf(1),
		Max:         math.Inf(-1),
	}
	var m2 float64
	for _, frame := range frames {
		for _, sample := range frame.Samples {
			if sample.ConfigIndex != configIndex || math.IsNaN(sample.Value) {
				continue
			}
			if reduced.Count == 0 {
				reduced.Type = sample.Type
				reduced.FirstTimestamp10us = frame.Timestamp10us
			}
			reduced.Count++
			reduced.LastTimestamp10us = frame.Timestamp10us
			if sample.Value < reduced.Min {
				reduced.Min = sample.Value
			}
			if sample.Value > reduced.Max {
				reduced.Max = sample.Value
			}
			delta := sample.Value - reduced.Mean
			reduced.Mean += delta / float64(reduced.Count)
			m2 += delta * (sample.Value - reduced.Mean)
		}
	}
	if reduced.Count == 0 {
		return ReducedRingSample{}, false
	}
	if reduced.Count > 1 {
		reduced.StdDev = math.Sqrt(m2 / float64(reduced.Count-1))
	}
	return reduced, true
}
