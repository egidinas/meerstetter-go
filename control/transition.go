package control

import (
	"math"
	"time"
)

type Sample struct {
	Time     time.Time
	SystemID string
	SignalID string
	Value    float64
	Unit     string
}

type TransitionDirection string

const (
	TransitionRising      TransitionDirection = "rising"
	TransitionFalling     TransitionDirection = "falling"
	TransitionDisturbance TransitionDirection = "disturbance"
)

type TransitionEvent struct {
	SystemID   string
	SignalID   string
	Start      time.Time
	Direction  TransitionDirection
	Baseline   float64
	PeakDelta  float64
	NoiseSigma float64
	Confidence float64
}

type TransitionDetectorConfig struct {
	Alpha            float64
	MinimumBaseline  int
	MinimumMagnitude float64
	SigmaThreshold   float64
}

type TransitionDetector struct {
	cfg       TransitionDetectorConfig
	count     int
	mean      float64
	variance  float64
	triggered bool
}

func NewTransitionDetector(cfg TransitionDetectorConfig) *TransitionDetector {
	if cfg.Alpha <= 0 || cfg.Alpha >= 1 {
		cfg.Alpha = 0.1
	}
	if cfg.MinimumBaseline <= 0 {
		cfg.MinimumBaseline = 20
	}
	if cfg.SigmaThreshold <= 0 {
		cfg.SigmaThreshold = 5
	}
	return &TransitionDetector{cfg: cfg}
}

func (d *TransitionDetector) Observe(sample Sample) (TransitionEvent, bool) {
	if math.IsNaN(sample.Value) {
		return TransitionEvent{}, false
	}
	if d.count == 0 {
		d.count = 1
		d.mean = sample.Value
		return TransitionEvent{}, false
	}

	sigma := math.Sqrt(math.Max(d.variance, 0))
	delta := sample.Value - d.mean
	threshold := math.Max(d.cfg.MinimumMagnitude, d.cfg.SigmaThreshold*sigma)
	if d.count >= d.cfg.MinimumBaseline && !d.triggered && math.Abs(delta) >= threshold {
		d.triggered = true
		direction := TransitionRising
		if delta < 0 {
			direction = TransitionFalling
		}
		confidence := 1.0
		if threshold > 0 {
			confidence = math.Min(1, math.Abs(delta)/math.Max(threshold*2, 1e-9))
		}
		return TransitionEvent{
			SystemID:   sample.SystemID,
			SignalID:   sample.SignalID,
			Start:      sample.Time,
			Direction:  direction,
			Baseline:   d.mean,
			PeakDelta:  math.Abs(delta),
			NoiseSigma: sigma,
			Confidence: confidence,
		}, true
	}

	alpha := d.cfg.Alpha
	nextMean := d.mean + alpha*delta
	nextDelta := sample.Value - nextMean
	d.variance = (1 - alpha) * (d.variance + alpha*delta*nextDelta)
	d.mean = nextMean
	d.count++
	return TransitionEvent{}, false
}

type PIDAlgorithmBasis string

const (
	AlgorithmStepResponse PIDAlgorithmBasis = "step_response_observation"
)

type TransitionCharacterization struct {
	SystemID       string
	SignalID       string
	SetpointDelta  float64
	PeakDelta      float64
	SettlingTime   time.Duration
	ControlEffort  float64
	Confidence     float64
	AlgorithmBasis PIDAlgorithmBasis
}

type PIDAdvisorConfig struct {
	MinimumEvents       int
	OvershootThreshold  float64
	SettlingThreshold   time.Duration
	MinorAdjustmentGain float64
}

type PIDAction string

const (
	PIDActionRecommendOnly PIDAction = "recommend_only"
)

type PIDSafetyLevel string

const (
	PIDSafetyOperatorReview PIDSafetyLevel = "operator_review"
)

type PIDScaleSuggestion struct {
	KpScale float64
	KiScale float64
	KdScale float64
}

type PIDRecommendation struct {
	SystemID       string
	SignalID       string
	Action         PIDAction
	Safety         PIDSafetyLevel
	Suggested      PIDScaleSuggestion
	Confidence     float64
	AlgorithmBasis PIDAlgorithmBasis
	Reasons        []string
}

type PIDAdvisor struct {
	cfg PIDAdvisorConfig
}

func NewPIDAdvisor(cfg PIDAdvisorConfig) *PIDAdvisor {
	if cfg.MinimumEvents <= 0 {
		cfg.MinimumEvents = 3
	}
	if cfg.OvershootThreshold <= 0 {
		cfg.OvershootThreshold = 0.2
	}
	if cfg.MinorAdjustmentGain <= 0 {
		cfg.MinorAdjustmentGain = 0.05
	}
	return &PIDAdvisor{cfg: cfg}
}

func (a *PIDAdvisor) Recommend(events []TransitionCharacterization) (PIDRecommendation, bool) {
	if len(events) < a.cfg.MinimumEvents {
		return PIDRecommendation{}, false
	}
	rec := PIDRecommendation{
		SystemID:       events[0].SystemID,
		SignalID:       events[0].SignalID,
		Action:         PIDActionRecommendOnly,
		Safety:         PIDSafetyOperatorReview,
		Suggested:      PIDScaleSuggestion{KpScale: 1, KiScale: 1, KdScale: 1},
		AlgorithmBasis: AlgorithmStepResponse,
	}

	var confidence float64
	var overshootCount int
	var slowCount int
	for _, event := range events {
		confidence += event.Confidence
		if event.AlgorithmBasis != "" {
			rec.AlgorithmBasis = event.AlgorithmBasis
		}
		if math.Abs(event.SetpointDelta) > 1e-9 {
			overshoot := (math.Abs(event.PeakDelta) - math.Abs(event.SetpointDelta)) / math.Abs(event.SetpointDelta)
			if overshoot > a.cfg.OvershootThreshold {
				overshootCount++
			}
		}
		if a.cfg.SettlingThreshold > 0 && event.SettlingTime > a.cfg.SettlingThreshold {
			slowCount++
		}
	}
	rec.Confidence = confidence / float64(len(events))
	if overshootCount > 0 {
		rec.Suggested.KpScale = 1 - a.cfg.MinorAdjustmentGain
		rec.Suggested.KiScale = 1 - a.cfg.MinorAdjustmentGain
		rec.Reasons = append(rec.Reasons, "repeated overshoot observed after setpoint transitions")
	}
	if slowCount > 0 {
		rec.Reasons = append(rec.Reasons, "settling time exceeded observation threshold")
	}
	if len(rec.Reasons) == 0 {
		rec.Reasons = append(rec.Reasons, "behavior is stable enough for continued observation")
	}
	return rec, true
}
