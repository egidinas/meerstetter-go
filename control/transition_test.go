package control

import (
	"testing"
	"time"
)

func TestEWMACUSUMDetectorFindsStepWithoutDeviceSpecificTypes(t *testing.T) {
	detector := NewTransitionDetector(TransitionDetectorConfig{
		Alpha:            0.2,
		MinimumBaseline:  8,
		MinimumMagnitude: 1.0,
		SigmaThreshold:   4,
	})

	start := time.Unix(100, 0)
	for i := 0; i < 16; i++ {
		event, ok := detector.Observe(Sample{
			Time:     start.Add(time.Duration(i) * time.Second),
			SystemID: "generic-thermal-loop",
			SignalID: "temperature.process",
			Value:    20 + float64(i%2)*0.05,
		})
		if ok {
			t.Fatalf("unexpected transition during baseline: %+v", event)
		}
	}

	var got TransitionEvent
	found := false
	for i := 16; i < 28; i++ {
		event, ok := detector.Observe(Sample{
			Time:     start.Add(time.Duration(i) * time.Second),
			SystemID: "generic-thermal-loop",
			SignalID: "temperature.process",
			Value:    25,
		})
		if ok {
			got = event
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected transition event")
	}
	if got.Direction != TransitionRising {
		t.Fatalf("direction = %q, want %q", got.Direction, TransitionRising)
	}
	if got.SystemID != "generic-thermal-loop" || got.SignalID != "temperature.process" {
		t.Fatalf("event identity = %q/%q", got.SystemID, got.SignalID)
	}
	if got.PeakDelta < 4 {
		t.Fatalf("peak delta = %.3f, want step-sized delta", got.PeakDelta)
	}
}

func TestPIDAdvisorReturnsConservativeObservationOnlyRecommendation(t *testing.T) {
	advisor := NewPIDAdvisor(PIDAdvisorConfig{
		MinimumEvents:       2,
		OvershootThreshold:  0.15,
		SettlingThreshold:   10 * time.Second,
		MinorAdjustmentGain: 0.05,
	})

	rec, ok := advisor.Recommend([]TransitionCharacterization{
		{
			SystemID:       "loop-a",
			SignalID:       "temperature.process",
			SetpointDelta:  10,
			PeakDelta:      12,
			SettlingTime:   20 * time.Second,
			ControlEffort:  0.9,
			Confidence:     0.8,
			AlgorithmBasis: AlgorithmStepResponse,
		},
		{
			SystemID:       "loop-a",
			SignalID:       "temperature.process",
			SetpointDelta:  10,
			PeakDelta:      11.8,
			SettlingTime:   22 * time.Second,
			ControlEffort:  0.95,
			Confidence:     0.75,
			AlgorithmBasis: AlgorithmStepResponse,
		},
	})
	if !ok {
		t.Fatal("expected recommendation")
	}
	if rec.Action != PIDActionRecommendOnly {
		t.Fatalf("action = %q, want observation-only recommendation", rec.Action)
	}
	if rec.SystemID != "loop-a" || rec.SignalID != "temperature.process" {
		t.Fatalf("recommendation identity = %q/%q", rec.SystemID, rec.SignalID)
	}
	if rec.Suggested.KpScale >= 1 {
		t.Fatalf("Kp scale = %.3f, want conservative reduction for overshoot", rec.Suggested.KpScale)
	}
	if rec.Safety != PIDSafetyOperatorReview {
		t.Fatalf("safety = %q, want %q", rec.Safety, PIDSafetyOperatorReview)
	}
	if len(rec.Reasons) == 0 {
		t.Fatal("expected evidence-backed reasons")
	}
}
