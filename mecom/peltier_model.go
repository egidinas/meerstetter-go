package mecom

import "math"

type ChannelDriveMode string

const (
	ChannelModeUnknown       ChannelDriveMode = "unknown"
	ChannelModeResistor      ChannelDriveMode = "resistor"
	ChannelModePowerSupply   ChannelDriveMode = "power_supply"
	ChannelModePeltierDriver ChannelDriveMode = "peltier_driver"
)

type ChannelModeResolver interface {
	ModeFor(controllerAddress int, channel int) ChannelDriveMode
}

type DerivedValue struct {
	Value      float64
	Valid      bool
	Confidence float64
	Source     string
}

type PeltierModulePoint struct {
	DeltaT         float64
	CurrentAmpere  float64
	VoltageVolt    float64
	HeatPumpedWatt float64
}

type PeltierModuleData struct {
	ModuleID string
	Source   string
	Points   []PeltierModulePoint
}

type PeltierChannelInput struct {
	ControllerAddress int
	Channel           int
	DriveMode         ChannelDriveMode
	ColdTemperatureC  float64
	HotTemperatureC   float64
	CurrentAmpere     float64
	VoltageVolt       float64
}

type PeltierChannelEstimate struct {
	ControllerAddress      int
	Channel                int
	DriveMode              ChannelDriveMode
	DeltaTC                float64
	ElectricalInputWatt    float64
	HeatPumpedFromItemWatt DerivedValue
	ResistiveHeatWatt      DerivedValue
	HotSideDissipatedWatt  DerivedValue
	Confidence             float64
	Source                 string
}

type PeltierGlobalEstimate struct {
	ChannelCount             int
	ElectricalInputWatt      float64
	HeatPumpedFromItemWatt   float64
	ResistiveHeatWatt        float64
	HotSideDissipatedWatt    float64
	ValidPeltierChannels     int
	ValidResistorChannels    int
	PowerSupplyChannels      int
	UnknownModeChannels      int
	MinimumThermalConfidence float64
}

type PeltierEstimator struct {
	moduleData map[int]PeltierModuleData
}

func NewPeltierEstimator(moduleData map[int]PeltierModuleData) *PeltierEstimator {
	copied := make(map[int]PeltierModuleData, len(moduleData))
	for address, data := range moduleData {
		points := make([]PeltierModulePoint, len(data.Points))
		copy(points, data.Points)
		data.Points = points
		copied[address] = data
	}
	return &PeltierEstimator{moduleData: copied}
}

func NormalizeChannelDriveMode(mode ChannelDriveMode) ChannelDriveMode {
	switch mode {
	case ChannelModeResistor, ChannelModePowerSupply, ChannelModePeltierDriver:
		return mode
	default:
		return ChannelModeUnknown
	}
}

func (e *PeltierEstimator) EstimateChannel(input PeltierChannelInput) PeltierChannelEstimate {
	mode := NormalizeChannelDriveMode(input.DriveMode)
	electrical := math.Abs(input.CurrentAmpere * input.VoltageVolt)
	estimate := PeltierChannelEstimate{
		ControllerAddress:   input.ControllerAddress,
		Channel:             input.Channel,
		DriveMode:           mode,
		DeltaTC:             input.HotTemperatureC - input.ColdTemperatureC,
		ElectricalInputWatt: electrical,
		Source:              "electrical_measurement",
	}

	switch mode {
	case ChannelModeResistor:
		estimate.ResistiveHeatWatt = DerivedValue{
			Value:      electrical,
			Valid:      true,
			Confidence: 1,
			Source:     "resistor_mode_electrical_input",
		}
		estimate.Confidence = 1
		estimate.Source = estimate.ResistiveHeatWatt.Source
	case ChannelModePowerSupply:
		estimate.Source = "power_supply_mode_electrical_only"
	case ChannelModePeltierDriver:
		data, ok := e.moduleData[input.ControllerAddress]
		if !ok || len(data.Points) == 0 {
			estimate.Source = "peltier_mode_missing_module_data"
			return estimate
		}
		point := nearestPeltierPoint(data.Points, estimate.DeltaTC, math.Abs(input.CurrentAmpere))
		confidence := peltierPointConfidence(point, input)
		source := data.Source
		if source == "" {
			source = "peltier_module_data"
		}
		estimate.HeatPumpedFromItemWatt = DerivedValue{
			Value:      point.HeatPumpedWatt,
			Valid:      true,
			Confidence: confidence,
			Source:     source,
		}
		estimate.HotSideDissipatedWatt = DerivedValue{
			Value:      electrical + point.HeatPumpedWatt,
			Valid:      true,
			Confidence: confidence,
			Source:     source,
		}
		estimate.Confidence = confidence
		estimate.Source = source
	default:
		estimate.Source = "unknown_mode_electrical_only"
	}

	return estimate
}

func EstimatePeltierGlobal(estimates []PeltierChannelEstimate) PeltierGlobalEstimate {
	global := PeltierGlobalEstimate{
		ChannelCount:             len(estimates),
		MinimumThermalConfidence: 1,
	}
	var thermalConfidenceSeen bool
	for _, estimate := range estimates {
		global.ElectricalInputWatt += estimate.ElectricalInputWatt
		switch estimate.DriveMode {
		case ChannelModePeltierDriver:
			if estimate.HeatPumpedFromItemWatt.Valid {
				global.HeatPumpedFromItemWatt += estimate.HeatPumpedFromItemWatt.Value
			}
			if estimate.HotSideDissipatedWatt.Valid {
				global.HotSideDissipatedWatt += estimate.HotSideDissipatedWatt.Value
				global.ValidPeltierChannels++
				global.MinimumThermalConfidence = math.Min(global.MinimumThermalConfidence, estimate.HotSideDissipatedWatt.Confidence)
				thermalConfidenceSeen = true
			}
		case ChannelModeResistor:
			if estimate.ResistiveHeatWatt.Valid {
				global.ResistiveHeatWatt += estimate.ResistiveHeatWatt.Value
				global.ValidResistorChannels++
				global.MinimumThermalConfidence = math.Min(global.MinimumThermalConfidence, estimate.ResistiveHeatWatt.Confidence)
				thermalConfidenceSeen = true
			}
		case ChannelModePowerSupply:
			global.PowerSupplyChannels++
		default:
			global.UnknownModeChannels++
		}
	}
	if !thermalConfidenceSeen {
		global.MinimumThermalConfidence = 0
	}
	return global
}

func nearestPeltierPoint(points []PeltierModulePoint, deltaT float64, currentAmpere float64) PeltierModulePoint {
	best := points[0]
	bestDistance := peltierPointDistance(best, deltaT, currentAmpere)
	for _, point := range points[1:] {
		distance := peltierPointDistance(point, deltaT, currentAmpere)
		if distance < bestDistance {
			best = point
			bestDistance = distance
		}
	}
	return best
}

func peltierPointDistance(point PeltierModulePoint, deltaT float64, currentAmpere float64) float64 {
	deltaTScale := math.Max(math.Abs(point.DeltaT), 1)
	currentScale := math.Max(math.Abs(point.CurrentAmpere), 1)
	dt := (point.DeltaT - deltaT) / deltaTScale
	current := (math.Abs(point.CurrentAmpere) - currentAmpere) / currentScale
	return dt*dt + current*current
}

func peltierPointConfidence(point PeltierModulePoint, input PeltierChannelInput) float64 {
	confidence := 0.75
	if point.VoltageVolt != 0 && input.VoltageVolt != 0 {
		relativeVoltageError := math.Abs(math.Abs(input.VoltageVolt)-math.Abs(point.VoltageVolt)) / math.Abs(point.VoltageVolt)
		switch {
		case relativeVoltageError <= 0.1:
			confidence = 0.95
		case relativeVoltageError <= 0.25:
			confidence = 0.75
		case relativeVoltageError <= 0.5:
			confidence = 0.5
		default:
			confidence = 0.25
		}
	}
	return confidence
}
