package mecom

import (
	"fmt"
	"math"
	"time"
)

const (
	TECParamObjectTemperature = 1000
	TECParamSinkTemperature   = 1001
	TECParamOutputCurrent     = 1020
	TECParamOutputVoltage     = 1021

	TECParamDerivedBase = 900000
)

const (
	derivedElectricalInputOffset      = 1
	derivedHeatPumpedFromItemOffset   = 2
	derivedResistiveHeatOffset        = 3
	derivedHotSideDissipatedOffset    = 4
	derivedElectricalInputName        = "electrical_input_w"
	derivedHeatPumpedFromItemName     = "heat_pumped_from_item_w"
	derivedResistiveHeatName          = "resistive_heat_w"
	derivedHotSideDissipatedName      = "hot_side_dissipated_w"
	derivedElectricalInputDisplayUnit = "W"
)

// DerivedReadoutConfig enables channel-mode-aware estimates from measured
// MeCom samples. Missing mode information deliberately falls back to electrical
// input only, so power-supply channels are not treated as thermal actuators.
type DerivedReadoutConfig struct {
	ControllerAddress int
	ChannelModes      map[int]ChannelDriveMode
	Estimator         *PeltierEstimator
}

type derivedReadout struct {
	controllerAddress int
	channelModes      map[int]ChannelDriveMode
	estimator         *PeltierEstimator
}

type derivedChannelInputs struct {
	channel int
	values  map[int]float64
}

func newDerivedReadout(cfg DerivedReadoutConfig) *derivedReadout {
	modes := make(map[int]ChannelDriveMode, len(cfg.ChannelModes))
	for channel, mode := range cfg.ChannelModes {
		modes[channel] = NormalizeChannelDriveMode(mode)
	}
	estimator := cfg.Estimator
	if estimator == nil {
		estimator = NewPeltierEstimator(nil)
	}
	return &derivedReadout{
		controllerAddress: cfg.ControllerAddress,
		channelModes:      modes,
		estimator:         estimator,
	}
}

func (d *derivedReadout) Append(observedAt time.Time, batch *ReadoutBatch) {
	if d == nil || batch == nil {
		return
	}
	inputs := map[int]*derivedChannelInputs{}
	for _, value := range batch.Values {
		if value.Parameter.Instance <= 0 {
			continue
		}
		switch value.Parameter.ID {
		case TECParamObjectTemperature, TECParamSinkTemperature, TECParamOutputCurrent, TECParamOutputVoltage:
		default:
			continue
		}
		if math.IsNaN(value.Value) {
			continue
		}
		channel := value.Parameter.Instance
		input := inputs[channel]
		if input == nil {
			input = &derivedChannelInputs{channel: channel, values: map[int]float64{}}
			inputs[channel] = input
		}
		input.values[value.Parameter.ID] = value.Value
	}
	for channel := 1; channel <= maxDerivedChannel(inputs); channel++ {
		input := inputs[channel]
		if input == nil {
			continue
		}
		current, hasCurrent := input.values[TECParamOutputCurrent]
		voltage, hasVoltage := input.values[TECParamOutputVoltage]
		if !hasCurrent || !hasVoltage {
			continue
		}
		estimate := d.estimator.EstimateChannel(PeltierChannelInput{
			ControllerAddress: d.controllerAddress,
			Channel:           channel,
			DriveMode:         d.channelModes[channel],
			ColdTemperatureC:  input.values[TECParamObjectTemperature],
			HotTemperatureC:   input.values[TECParamSinkTemperature],
			CurrentAmpere:     current,
			VoltageVolt:       voltage,
		})
		batch.DerivedEstimates = append(batch.DerivedEstimates, estimate)
		d.appendEstimateValues(observedAt, batch, estimate)
	}
}

func maxDerivedChannel(inputs map[int]*derivedChannelInputs) int {
	maxChannel := 0
	for channel := range inputs {
		if channel > maxChannel {
			maxChannel = channel
		}
	}
	return maxChannel
}

func (d *derivedReadout) appendEstimateValues(observedAt time.Time, batch *ReadoutBatch, estimate PeltierChannelEstimate) {
	d.appendValue(observedAt, batch, estimate.Channel, derivedElectricalInputOffset, derivedElectricalInputName, estimate.ElectricalInputWatt)
	if estimate.HeatPumpedFromItemWatt.Valid {
		d.appendValue(observedAt, batch, estimate.Channel, derivedHeatPumpedFromItemOffset, derivedHeatPumpedFromItemName, estimate.HeatPumpedFromItemWatt.Value)
	}
	if estimate.ResistiveHeatWatt.Valid {
		d.appendValue(observedAt, batch, estimate.Channel, derivedResistiveHeatOffset, derivedResistiveHeatName, estimate.ResistiveHeatWatt.Value)
	}
	if estimate.HotSideDissipatedWatt.Valid {
		d.appendValue(observedAt, batch, estimate.Channel, derivedHotSideDissipatedOffset, derivedHotSideDissipatedName, estimate.HotSideDissipatedWatt.Value)
	}
}

func (d *derivedReadout) appendValue(observedAt time.Time, batch *ReadoutBatch, channel int, offset int, name string, value float64) {
	sample := ReadoutValue{
		Parameter: Parameter{
			ID:       TECParamDerivedBase + channel*10 + offset,
			Instance: channel,
			Name:     name,
			Unit:     derivedElectricalInputDisplayUnit,
			Type:     DataTypeFloat32,
		},
		Sensor:     fmt.Sprintf("mecom.tec_%02d.%s", channel, name),
		Value:      value,
		ObservedAt: observedAt,
		Readout:    ReadoutDerivedChannelModel,
	}
	batch.DerivedValues = append(batch.DerivedValues, sample)
	batch.Values = append(batch.Values, sample)
}
