package mecom

const (
	RMM1182USBMeComAddress = 0
	RMM1182USBSerialBaud   = 57600

	RMM1182HR1Instance = 1
	RMM1182VC1Instance = 1
)

const (
	RMM1182ParamHRRawADC          = 3000
	RMM1182ParamHRResistance      = 3001
	RMM1182ParamHRVoltage         = 3002
	RMM1182ParamHRMeasurementType = 3100
	RMM1182ParamHRADCRs           = 3101
	RMM1182ParamVCResult          = 4000
	RMM1182ParamVCSurveilled      = 4001
	RMM1182ParamVCResultType      = 4011
	RMM1182ParamVCConversionType  = 4012
)

var defaultRMM1182HR1Pt100Parameters = []Parameter{
	{ID: RMM1182ParamHRRawADC, Instance: RMM1182HR1Instance, Name: "RMM-1182 HR1 raw ADC", Unit: "count", Type: DataTypeFloat32, Role: "measurement", Kind: "continuous"},
	{ID: RMM1182ParamHRResistance, Instance: RMM1182HR1Instance, Name: "RMM-1182 HR1 resistance", Unit: "Ohm", Type: DataTypeFloat32, Role: "measurement", Kind: "continuous"},
	{ID: RMM1182ParamHRVoltage, Instance: RMM1182HR1Instance, Name: "RMM-1182 HR1 voltage", Unit: "V", Type: DataTypeFloat32, Role: "measurement", Kind: "continuous"},
	{ID: RMM1182ParamHRMeasurementType, Instance: RMM1182HR1Instance, Name: "RMM-1182 HR1 measurement type", Type: DataTypeInt32, Role: "configuration", Kind: "state"},
	{ID: RMM1182ParamHRADCRs, Instance: RMM1182HR1Instance, Name: "RMM-1182 HR1 ADC Rs", Unit: "Ohm", Type: DataTypeFloat32, Role: "configuration", Kind: "state"},
	{ID: RMM1182ParamVCResult, Instance: RMM1182VC1Instance, Name: "RMM-1182 VC1 result", Unit: "C", Type: DataTypeFloat32, Role: "measurement", Kind: "continuous"},
	{ID: RMM1182ParamVCSurveilled, Instance: RMM1182VC1Instance, Name: "RMM-1182 VC1 surveilled result", Type: DataTypeFloat32, Role: "measurement", Kind: "continuous"},
	{ID: RMM1182ParamVCResultType, Instance: RMM1182VC1Instance, Name: "RMM-1182 VC1 result type", Type: DataTypeInt32, Role: "configuration", Kind: "state"},
	{ID: RMM1182ParamVCConversionType, Instance: RMM1182VC1Instance, Name: "RMM-1182 VC1 conversion type", Type: DataTypeInt32, Role: "configuration", Kind: "state"},
}

// DefaultRMM1182HR1Pt100Parameters returns the read-only USB MeCom parameters
// proven on one RMM-1182 with a Pt100 sensor connected to HR1/VC1.
func DefaultRMM1182HR1Pt100Parameters() []Parameter {
	out := make([]Parameter, len(defaultRMM1182HR1Pt100Parameters))
	copy(out, defaultRMM1182HR1Pt100Parameters)
	return out
}

func DefaultRMM1182HR1Pt100ReadoutParameters() []ReadoutParameter {
	params := DefaultRMM1182HR1Pt100Parameters()
	out := make([]ReadoutParameter, 0, len(params))
	for _, param := range params {
		out = append(out, ReadoutParameter{
			Parameter:    param,
			Sensor:       "mecom.rmm_1182.hr1." + rmm1182HR1Pt100SensorSuffix(param.ID),
			HighPriority: false,
		})
	}
	return out
}

func rmm1182HR1Pt100SensorSuffix(id int) string {
	switch id {
	case RMM1182ParamHRRawADC:
		return "raw_adc"
	case RMM1182ParamHRResistance:
		return "resistance_ohm"
	case RMM1182ParamHRVoltage:
		return "voltage_v"
	case RMM1182ParamHRMeasurementType:
		return "measurement_type"
	case RMM1182ParamHRADCRs:
		return "adc_rs_ohm"
	case RMM1182ParamVCResult:
		return "vc1_result_c"
	case RMM1182ParamVCSurveilled:
		return "vc1_surveilled_result"
	case RMM1182ParamVCResultType:
		return "vc1_result_type"
	case RMM1182ParamVCConversionType:
		return "vc1_conversion_type"
	default:
		return "param"
	}
}
