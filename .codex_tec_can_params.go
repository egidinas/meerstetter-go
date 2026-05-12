package mecom

var TEC_CAN_PARAMETERS = []ParameterDef{
	{ID: 104, Name: "Device Status", Format: "INT32"},
	{ID: 108, Name: "Save Data to Flash", Format: "INT32"},
	{ID: 109, Name: "Flash Status", Format: "INT32"},
	{ID: 111, Name: "Device Reset", Format: "INT32"},
	{ID: 2051, Name: "Device Address", Format: "INT32"},
	{ID: 2070, Name: "CANopen Node ID", Format: "INT32"},
	{ID: 2071, Name: "CANopen Bit Rate", Format: "INT32"},
	{ID: 2072, Name: "CAN1", Format: "INT32"},
	{ID: 2080, Name: "CAN1 Auto Operational", Format: "INT32"},
}
