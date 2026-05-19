package main

import "github.com/egidinas/meerstetter-go/mecom"

const (
	gatewayQualityOK       = "ok"
	gatewayQualityMissing  = "missing"
	gatewayQualityNaN      = "nan"
	gatewayQualityDetached = "detached"

	detachedTemperatureFloorC = -50.0
)

func gatewayQualityForFloat(p mecom.Parameter, value float64) string {
	if isDetachedMeasuredTemperature(p.ID, p.Unit, value) {
		return gatewayQualityDetached
	}
	return gatewayQualityOK
}

func isMeasuredTemperatureParam(paramID int) bool {
	switch paramID {
	case 1000, 1001, 40000:
		return true
	default:
		return false
	}
}

func isDetachedMeasuredTemperature(paramID int, unit string, value float64) bool {
	return isMeasuredTemperatureParam(paramID) && graphUnitIsTemperature(unit) && value < detachedTemperatureFloorC
}
