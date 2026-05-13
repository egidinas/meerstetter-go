package mecom

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	DefaultCANBitrateBitPerSecond      = 1000000
	DefaultConservativeCANFrameBits    = 160
	DefaultPollingMaxBusUtilization    = 0.50
	DefaultRingReadPeriod              = 100 * time.Millisecond
	DefaultBackgroundBulkPollHz        = 2
	DefaultBulkRequestFrames           = 1
	DefaultBulkResponseFramesPerParam  = 1
	DefaultRingRequestFrames           = 2
	DefaultRingResponseFramesPerSignal = 1
)

// PollingCapacityInput describes the reusable budget model for a controller
// bank. It is deliberately conservative: live measurements can tighten the
// frame counts and poll frequencies per adapter/backend later.
type PollingCapacityInput struct {
	Controllers             int
	InstancesPerController  int
	HighPriorityPerInstance int
	BackgroundPerInstance   int

	RingCaptureLimit int
	RingReadPeriod   time.Duration
	BulkChunk        int
	BulkPollHz       float64

	CANBitrateBitPerSecond int
	FrameBits              int
	MaxBusUtilization      float64

	BulkRequestFrames           int
	BulkResponseFramesPerParam  int
	RingRequestFrames           int
	RingResponseFramesPerSignal int
}

// PollingCapacityEstimate is the calculated operating envelope for one
// MeCom/TEC controller bank. It models controller ring readout as preferred for
// key signals and round-robin bulk reads as the background and overflow lane.
type PollingCapacityEstimate struct {
	Controllers            int
	InstancesPerController int

	HighPriorityPerController int
	BackgroundPerController   int
	TotalHighPriority         int
	TotalBackground           int

	RingCaptureSlotsPerController int
	RingCaptureSlotsTotal         int
	HighPriorityOverflow          int
	RoundRobinQueueTotal          int

	RingReadHz               float64
	RingValuesPerSecond      float64
	BulkParamsPerSecond      float64
	RoundRobinCycleSeconds   float64
	EstimatedFramesPerSecond float64
	EstimatedBitsPerSecond   float64
	EstimatedBusUtilization  float64

	CanKeepAllHighPriorityInRing bool
	CanSustainBudget             bool
	Recommendation               string
}

// DefaultTECPollingCapacityInput returns a four-controller-friendly baseline
// derived from the active TEC catalogue. The defaults are intentionally safe and
// should be replaced by measured adapter timings when available.
func DefaultTECPollingCapacityInput(controllers, instancesPerController int) PollingCapacityInput {
	if controllers <= 0 {
		controllers = 4
	}
	if instancesPerController <= 0 {
		instancesPerController = 4
	}
	high, background := countTECReadoutPriorities(instancesPerController)
	return PollingCapacityInput{
		Controllers:                 controllers,
		InstancesPerController:      instancesPerController,
		HighPriorityPerInstance:     high / instancesPerController,
		BackgroundPerInstance:       background / instancesPerController,
		RingCaptureLimit:            RingCaptureLimit,
		RingReadPeriod:              DefaultRingReadPeriod,
		BulkChunk:                   8,
		BulkPollHz:                  DefaultBackgroundBulkPollHz,
		CANBitrateBitPerSecond:      DefaultCANBitrateBitPerSecond,
		FrameBits:                   DefaultConservativeCANFrameBits,
		MaxBusUtilization:           DefaultPollingMaxBusUtilization,
		BulkRequestFrames:           DefaultBulkRequestFrames,
		BulkResponseFramesPerParam:  DefaultBulkResponseFramesPerParam,
		RingRequestFrames:           DefaultRingRequestFrames,
		RingResponseFramesPerSignal: DefaultRingResponseFramesPerSignal,
	}
}

func countTECReadoutPriorities(instances int) (high int, background int) {
	for _, param := range DefaultTECReadoutParameters(instances) {
		if param.HighPriority {
			high++
			continue
		}
		background++
	}
	return high, background
}

// EstimateTECPollingCapacity calculates whether the requested ring and
// round-robin polling envelope fits within the configured CAN budget.
func EstimateTECPollingCapacity(input PollingCapacityInput) PollingCapacityEstimate {
	input = normalizePollingCapacityInput(input)

	highPerController := input.HighPriorityPerInstance * input.InstancesPerController
	backgroundPerController := input.BackgroundPerInstance * input.InstancesPerController
	totalHigh := highPerController * input.Controllers
	totalBackground := backgroundPerController * input.Controllers

	ringSlotsPerController := minInt(input.RingCaptureLimit, highPerController)
	ringSlotsTotal := ringSlotsPerController * input.Controllers
	overflow := maxInt(0, totalHigh-ringSlotsTotal)
	roundRobinTotal := totalBackground + overflow

	ringHz := 1 / input.RingReadPeriod.Seconds()
	ringValuesPerSecond := float64(ringSlotsTotal) * ringHz
	bulkParamsPerSecond := float64(input.Controllers*input.BulkChunk) * input.BulkPollHz
	roundRobinCycleSeconds := math.Inf(1)
	if roundRobinTotal == 0 {
		roundRobinCycleSeconds = 0
	} else if bulkParamsPerSecond > 0 {
		roundRobinCycleSeconds = float64(roundRobinTotal) / bulkParamsPerSecond
	}

	ringFramesPerControllerRead := input.RingRequestFrames + ringSlotsPerController*input.RingResponseFramesPerSignal
	ringFramesPerSecond := float64(input.Controllers) * ringHz * float64(ringFramesPerControllerRead)
	bulkParamsPerController := minInt(input.BulkChunk, maxInt(0, roundRobinTotal/input.Controllers))
	bulkFramesPerControllerRead := input.BulkRequestFrames + bulkParamsPerController*input.BulkResponseFramesPerParam
	bulkFramesPerSecond := float64(input.Controllers) * input.BulkPollHz * float64(bulkFramesPerControllerRead)
	framesPerSecond := ringFramesPerSecond + bulkFramesPerSecond
	bitsPerSecond := framesPerSecond * float64(input.FrameBits)
	utilization := bitsPerSecond / float64(input.CANBitrateBitPerSecond)

	estimate := PollingCapacityEstimate{
		Controllers:                   input.Controllers,
		InstancesPerController:        input.InstancesPerController,
		HighPriorityPerController:     highPerController,
		BackgroundPerController:       backgroundPerController,
		TotalHighPriority:             totalHigh,
		TotalBackground:               totalBackground,
		RingCaptureSlotsPerController: ringSlotsPerController,
		RingCaptureSlotsTotal:         ringSlotsTotal,
		HighPriorityOverflow:          overflow,
		RoundRobinQueueTotal:          roundRobinTotal,
		RingReadHz:                    ringHz,
		RingValuesPerSecond:           ringValuesPerSecond,
		BulkParamsPerSecond:           bulkParamsPerSecond,
		RoundRobinCycleSeconds:        roundRobinCycleSeconds,
		EstimatedFramesPerSecond:      framesPerSecond,
		EstimatedBitsPerSecond:        bitsPerSecond,
		EstimatedBusUtilization:       utilization,
		CanKeepAllHighPriorityInRing:  overflow == 0,
		CanSustainBudget:              utilization <= input.MaxBusUtilization,
	}
	estimate.Recommendation = pollingCapacityRecommendation(estimate, input)
	return estimate
}

func normalizePollingCapacityInput(input PollingCapacityInput) PollingCapacityInput {
	if input.Controllers <= 0 {
		input.Controllers = 4
	}
	if input.InstancesPerController <= 0 {
		input.InstancesPerController = 4
	}
	if input.HighPriorityPerInstance <= 0 || input.BackgroundPerInstance <= 0 {
		high, background := countTECReadoutPriorities(input.InstancesPerController)
		if input.HighPriorityPerInstance <= 0 {
			input.HighPriorityPerInstance = high / input.InstancesPerController
		}
		if input.BackgroundPerInstance <= 0 {
			input.BackgroundPerInstance = background / input.InstancesPerController
		}
	}
	if input.RingCaptureLimit <= 0 || input.RingCaptureLimit > RingCaptureLimit {
		input.RingCaptureLimit = RingCaptureLimit
	}
	if input.RingReadPeriod <= 0 {
		input.RingReadPeriod = DefaultRingReadPeriod
	}
	if input.BulkChunk <= 0 {
		input.BulkChunk = 8
	}
	if input.BulkPollHz <= 0 {
		input.BulkPollHz = DefaultBackgroundBulkPollHz
	}
	if input.CANBitrateBitPerSecond <= 0 {
		input.CANBitrateBitPerSecond = DefaultCANBitrateBitPerSecond
	}
	if input.FrameBits <= 0 {
		input.FrameBits = DefaultConservativeCANFrameBits
	}
	if input.MaxBusUtilization <= 0 || input.MaxBusUtilization > 1 {
		input.MaxBusUtilization = DefaultPollingMaxBusUtilization
	}
	if input.BulkRequestFrames <= 0 {
		input.BulkRequestFrames = DefaultBulkRequestFrames
	}
	if input.BulkResponseFramesPerParam <= 0 {
		input.BulkResponseFramesPerParam = DefaultBulkResponseFramesPerParam
	}
	if input.RingRequestFrames <= 0 {
		input.RingRequestFrames = DefaultRingRequestFrames
	}
	if input.RingResponseFramesPerSignal <= 0 {
		input.RingResponseFramesPerSignal = DefaultRingResponseFramesPerSignal
	}
	return input
}

func pollingCapacityRecommendation(estimate PollingCapacityEstimate, input PollingCapacityInput) string {
	var parts []string
	if estimate.CanKeepAllHighPriorityInRing {
		parts = append(parts, "all high-priority catalogue values fit in the controller ring capture slots")
	} else {
		parts = append(parts, fmt.Sprintf("%d high-priority values overflow ring slots; keep the most important temperature/electrical signals in ring capture and send overflow through round-robin bulk reads", estimate.HighPriorityOverflow))
	}
	if estimate.CanSustainBudget {
		parts = append(parts, fmt.Sprintf("estimated CAN load %.1f%% is within the %.1f%% planning cap", estimate.EstimatedBusUtilization*100, input.MaxBusUtilization*100))
	} else {
		parts = append(parts, fmt.Sprintf("estimated CAN load %.1f%% exceeds the %.1f%% planning cap; reduce background poll rate first, then lengthen ring read period under congestion", estimate.EstimatedBusUtilization*100, input.MaxBusUtilization*100))
	}
	if estimate.RoundRobinCycleSeconds > 0 && !math.IsInf(estimate.RoundRobinCycleSeconds, 1) {
		parts = append(parts, fmt.Sprintf("round-robin queue cycle is %.2fs for %d queued values", estimate.RoundRobinCycleSeconds, estimate.RoundRobinQueueTotal))
	}
	return strings.Join(parts, "; ")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
