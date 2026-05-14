package control

import "github.com/egidinas/signalforge/controlobserve"

type Sample = controlobserve.Sample
type TransitionDirection = controlobserve.TransitionDirection

const (
	TransitionRising      = controlobserve.TransitionRising
	TransitionFalling     = controlobserve.TransitionFalling
	TransitionDisturbance = controlobserve.TransitionDisturbance
)

type TransitionEvent = controlobserve.TransitionEvent
type TransitionDetectorConfig = controlobserve.TransitionDetectorConfig
type TransitionDetector = controlobserve.TransitionDetector

func NewTransitionDetector(cfg TransitionDetectorConfig) *TransitionDetector {
	return controlobserve.NewTransitionDetector(cfg)
}

type PIDAlgorithmBasis = controlobserve.AlgorithmBasis

const (
	AlgorithmStepResponse = controlobserve.AlgorithmStepResponse
)

type TransitionCharacterization = controlobserve.TransitionCharacterization
type PIDAdvisorConfig = controlobserve.PIDAdvisorConfig
type PIDAction = controlobserve.PIDAction

const (
	PIDActionRecommendOnly = controlobserve.PIDActionRecommendOnly
)

type PIDSafetyLevel = controlobserve.PIDSafetyLevel

const (
	PIDSafetyOperatorReview = controlobserve.PIDSafetyOperatorReview
)

type PIDScaleSuggestion = controlobserve.PIDScaleSuggestion
type PIDRecommendation = controlobserve.PIDRecommendation
type PIDAdvisor = controlobserve.PIDAdvisor

func NewPIDAdvisor(cfg PIDAdvisorConfig) *PIDAdvisor {
	return controlobserve.NewPIDAdvisor(cfg)
}
