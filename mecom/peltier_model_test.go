package mecom

import "testing"

func TestPeltierModeUsesModuleDataForHeatFlow(t *testing.T) {
	estimator := NewPeltierEstimator(map[int]PeltierModuleData{
		31: {
			ModuleID: "sample-module",
			Source:   "datasheet",
			Points: []PeltierModulePoint{{
				DeltaT:         20,
				CurrentAmpere:  2,
				VoltageVolt:    12,
				HeatPumpedWatt: 8,
			}},
		},
	})

	got := estimator.EstimateChannel(PeltierChannelInput{
		ControllerAddress: 31,
		Channel:           1,
		DriveMode:         ChannelModePeltierDriver,
		ColdTemperatureC:  10,
		HotTemperatureC:   30,
		CurrentAmpere:     2,
		VoltageVolt:       12,
	})

	if got.ElectricalInputWatt != 24 {
		t.Fatalf("electrical input = %.3f, want 24", got.ElectricalInputWatt)
	}
	if !got.HeatPumpedFromItemWatt.Valid || got.HeatPumpedFromItemWatt.Value != 8 {
		t.Fatalf("heat pumped = %+v, want valid 8 W", got.HeatPumpedFromItemWatt)
	}
	if !got.HotSideDissipatedWatt.Valid || got.HotSideDissipatedWatt.Value != 32 {
		t.Fatalf("hot side heat = %+v, want valid 32 W", got.HotSideDissipatedWatt)
	}
	if got.ResistiveHeatWatt.Valid {
		t.Fatalf("resistive heat should be invalid in peltier mode: %+v", got.ResistiveHeatWatt)
	}
}

func TestResistorModeDoesNotRequireTemperatureGradientOrModuleData(t *testing.T) {
	got := NewPeltierEstimator(nil).EstimateChannel(PeltierChannelInput{
		ControllerAddress: 31,
		Channel:           2,
		DriveMode:         ChannelModeResistor,
		CurrentAmpere:     1.5,
		VoltageVolt:       10,
	})

	if got.ElectricalInputWatt != 15 {
		t.Fatalf("electrical input = %.3f, want 15", got.ElectricalInputWatt)
	}
	if !got.ResistiveHeatWatt.Valid || got.ResistiveHeatWatt.Value != 15 {
		t.Fatalf("resistive heat = %+v, want valid 15 W", got.ResistiveHeatWatt)
	}
	if got.HeatPumpedFromItemWatt.Valid || got.HotSideDissipatedWatt.Valid {
		t.Fatalf("resistor mode must not claim peltier heat flow: %+v", got)
	}
}

func TestPowerSupplyAndUnknownModesReportElectricalOnly(t *testing.T) {
	estimator := NewPeltierEstimator(nil)
	for _, tc := range []struct {
		name string
		mode ChannelDriveMode
	}{
		{name: "power supply", mode: ChannelModePowerSupply},
		{name: "unknown", mode: ChannelModeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := estimator.EstimateChannel(PeltierChannelInput{
				ControllerAddress: 32,
				Channel:           1,
				DriveMode:         tc.mode,
				CurrentAmpere:     -2,
				VoltageVolt:       12,
			})
			if got.ElectricalInputWatt != 24 {
				t.Fatalf("electrical input = %.3f, want abs(V*I)", got.ElectricalInputWatt)
			}
			if got.HeatPumpedFromItemWatt.Valid || got.ResistiveHeatWatt.Valid || got.HotSideDissipatedWatt.Valid {
				t.Fatalf("%s mode must not infer thermal coupling: %+v", tc.name, got)
			}
			if got.Confidence != 0 {
				t.Fatalf("confidence = %.3f, want 0 for non-thermal inference", got.Confidence)
			}
		})
	}
}

func TestPeltierGlobalEstimateAggregatesOnlyValidThermalValues(t *testing.T) {
	estimator := NewPeltierEstimator(map[int]PeltierModuleData{
		31: {
			ModuleID: "sample-module",
			Points: []PeltierModulePoint{{
				DeltaT:         20,
				CurrentAmpere:  2,
				VoltageVolt:    12,
				HeatPumpedWatt: 8,
			}},
		},
	})

	estimates := []PeltierChannelEstimate{
		estimator.EstimateChannel(PeltierChannelInput{ControllerAddress: 31, Channel: 1, DriveMode: ChannelModePeltierDriver, ColdTemperatureC: 10, HotTemperatureC: 30, CurrentAmpere: 2, VoltageVolt: 12}),
		estimator.EstimateChannel(PeltierChannelInput{ControllerAddress: 31, Channel: 2, DriveMode: ChannelModeResistor, CurrentAmpere: 1.5, VoltageVolt: 10}),
		estimator.EstimateChannel(PeltierChannelInput{ControllerAddress: 32, Channel: 1, DriveMode: ChannelModePowerSupply, CurrentAmpere: 2, VoltageVolt: 10}),
		estimator.EstimateChannel(PeltierChannelInput{ControllerAddress: 32, Channel: 2, DriveMode: ChannelModeUnknown, CurrentAmpere: 1, VoltageVolt: 5}),
	}

	got := EstimatePeltierGlobal(estimates)
	if got.ChannelCount != 4 || got.ValidPeltierChannels != 1 || got.ValidResistorChannels != 1 || got.PowerSupplyChannels != 1 || got.UnknownModeChannels != 1 {
		t.Fatalf("unexpected channel counts: %+v", got)
	}
	if got.ElectricalInputWatt != 64 {
		t.Fatalf("electrical total = %.3f, want 64", got.ElectricalInputWatt)
	}
	if got.HeatPumpedFromItemWatt != 8 || got.ResistiveHeatWatt != 15 || got.HotSideDissipatedWatt != 32 {
		t.Fatalf("unexpected thermal totals: %+v", got)
	}
}
