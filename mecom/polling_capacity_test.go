package mecom

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestDefaultTECPollingCapacityCountsFourControllerCatalogue(t *testing.T) {
	estimate := EstimateTECPollingCapacity(DefaultTECPollingCapacityInput(4, 4))

	if got, want := estimate.TotalHighPriority, 176; got != want {
		t.Fatalf("total high priority = %d, want %d", got, want)
	}
	if got, want := estimate.TotalBackground, 16; got != want {
		t.Fatalf("total background = %d, want %d", got, want)
	}
	if got, want := estimate.RingCaptureSlotsTotal, 64; got != want {
		t.Fatalf("ring capture slots = %d, want %d", got, want)
	}
	if got, want := estimate.HighPriorityOverflow, 112; got != want {
		t.Fatalf("high-priority overflow = %d, want %d", got, want)
	}
	if got, want := estimate.RoundRobinQueueTotal, 128; got != want {
		t.Fatalf("round-robin queue total = %d, want %d", got, want)
	}
	if got, want := estimate.RoundRobinCycleSeconds, 2.0; math.Abs(got-want) > 0.0001 {
		t.Fatalf("round-robin cycle = %.4f, want %.4f", got, want)
	}
	if estimate.CanKeepAllHighPriorityInRing {
		t.Fatal("four-controller/four-instance catalogue should exceed one 16-slot ring capture per controller")
	}
	if !strings.Contains(estimate.Recommendation, "overflow ring slots") {
		t.Fatalf("recommendation does not mention ring overflow: %q", estimate.Recommendation)
	}
}

func TestTECPollingCapacityFlagsOverBudgetSixteenControllerCase(t *testing.T) {
	input := DefaultTECPollingCapacityInput(16, 4)
	input.RingReadPeriod = 50 * time.Millisecond
	input.BulkPollHz = 4
	input.MaxBusUtilization = 0.25

	estimate := EstimateTECPollingCapacity(input)

	if estimate.CanSustainBudget {
		t.Fatalf("expected over-budget estimate, got utilization %.3f and recommendation %q", estimate.EstimatedBusUtilization, estimate.Recommendation)
	}
	if !strings.Contains(estimate.Recommendation, "exceeds") {
		t.Fatalf("recommendation does not call out over-budget load: %q", estimate.Recommendation)
	}
}

func TestTECPollingCapacityCanModelReducedPrioritySet(t *testing.T) {
	input := DefaultTECPollingCapacityInput(4, 2)
	input.HighPriorityPerInstance = 4
	input.BackgroundPerInstance = 5

	estimate := EstimateTECPollingCapacity(input)

	if !estimate.CanKeepAllHighPriorityInRing {
		t.Fatalf("reduced priority set should fit ring slots: %#v", estimate)
	}
	if got, want := estimate.RingCaptureSlotsTotal, 32; got != want {
		t.Fatalf("ring capture slots = %d, want %d", got, want)
	}
	if got, want := estimate.RoundRobinQueueTotal, 40; got != want {
		t.Fatalf("round-robin queue total = %d, want %d", got, want)
	}
}
