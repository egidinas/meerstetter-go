package tmp

var Parameters = []struct {
	ID     int
	Name   string
	Format string
}{
	{ID: 2010, Name: "Output Stage Enable", Format: "INT32"},
	{ID: 2020, Name: "Fixed Output Current", Format: "FLOAT32"},
	{ID: 2021, Name: "Fixed Output Voltage", Format: "FLOAT32"},
	{ID: 2030, Name: "Output Current Limitation", Format: "FLOAT32"},
	{ID: 2031, Name: "Output Voltage Limitation", Format: "FLOAT32"},
	{ID: 2040, Name: "Output Operating Mode", Format: "INT32"},
	{ID: 1020, Name: "Actual Output Current", Format: "FLOAT32"},
	{ID: 1021, Name: "Actual Output Voltage", Format: "FLOAT32"},
	{ID: 1022, Name: "Actual Output Power", Format: "FLOAT32"},
}
