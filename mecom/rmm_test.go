package mecom

import (
	"strings"
	"testing"
)

func TestDefaultRMM1182HR1Pt100Parameters(t *testing.T) {
	params := DefaultRMM1182HR1Pt100Parameters()
	if got, want := len(params), 9; got != want {
		t.Fatalf("parameter count = %d, want %d", got, want)
	}
	requireRMMParameter(t, params, RMM1182ParamHRResistance, RMM1182HR1Instance, DataTypeFloat32, "Ohm")
	requireRMMParameter(t, params, RMM1182ParamVCResult, RMM1182VC1Instance, DataTypeFloat32, "C")
	requireRMMParameter(t, params, RMM1182ParamVCConversionType, RMM1182VC1Instance, DataTypeInt32, "")
	for _, param := range params {
		if param.Writable {
			t.Fatalf("RMM parameter %d marked writable, want read-only until live writes are proven", param.ID)
		}
	}
}

func TestDefaultRMM1182HR1Pt100ParametersReturnsCopy(t *testing.T) {
	params := DefaultRMM1182HR1Pt100Parameters()
	params[0].Name = "mutated"
	if DefaultRMM1182HR1Pt100Parameters()[0].Name == "mutated" {
		t.Fatal("RMM preset returned shared parameter backing storage")
	}
}

func TestDefaultRMM1182HR1Pt100ReadoutParameters(t *testing.T) {
	readouts := DefaultRMM1182HR1Pt100ReadoutParameters()
	if got, want := len(readouts), len(DefaultRMM1182HR1Pt100Parameters()); got != want {
		t.Fatalf("readout count = %d, want %d", got, want)
	}
	for _, readout := range readouts {
		if !strings.HasPrefix(readout.Sensor, "mecom.rmm_1182.hr1.") {
			t.Fatalf("sensor = %q, want RMM HR1 prefix", readout.Sensor)
		}
		if readout.HighPriority {
			t.Fatalf("sensor %q marked high priority before RMM ring/readout timing is proven", readout.Sensor)
		}
	}
}

func TestRMM1182DecimalMeParIDEncodesToObservedWireID(t *testing.T) {
	frame := string(BuildSingleGetFrame(RMM1182USBMeComAddress, 1, RMM1182ParamHRResistance, RMM1182HR1Instance))
	if !strings.Contains(frame, "?VR0BB901") {
		t.Fatalf("frame = %q, want decimal MeParID 3001 encoded as hex 0BB9 instance 01", frame)
	}
}

func requireRMMParameter(t *testing.T, params []Parameter, id, instance int, dataType DataType, unit string) {
	t.Helper()
	for _, param := range params {
		if param.ID == id && param.Instance == instance {
			if param.Type != dataType || param.Unit != unit {
				t.Fatalf("RMM parameter %d:%d = %#v, want type %s unit %q", id, instance, param, dataType, unit)
			}
			return
		}
	}
	t.Fatalf("missing RMM parameter %d:%d", id, instance)
}
