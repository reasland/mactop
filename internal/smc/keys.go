package smc

// SensorDef maps a human-readable name to an SMC four-character key.
type SensorDef struct {
	Key  string
	Name string
}

// AppleSiliconSensors lists known thermal sensor keys for Apple Silicon Macs.
// The implementation tries each key; missing keys are silently skipped.
var AppleSiliconSensors = []SensorDef{
	// CPU Performance cores
	{"Tp0T", "CPU P-Core 1"},
	{"Tp0P", "CPU P-Core 2"},
	{"Tp0D", "CPU P-Core 3"},
	{"Tp0H", "CPU P-Core 4"},
	{"Tp0L", "CPU P-Core 5"},
	{"Tp0B", "CPU P-Core 6"},
	// CPU Efficiency cores
	{"Tp09", "CPU E-Core 1"},
	{"Tp01", "CPU E-Core 2"},
	{"Tp05", "CPU E-Core 3"},
	{"Tp0R", "CPU E-Core 4"},
	// CPU Die/Package
	{"Tc0c", "CPU Die"},
	{"Tc0p", "CPU Proximity"},
	{"TC0c", "CPU Die Alt"},
	{"TC0p", "CPU Prox Alt"},
	// GPU
	{"Tg0f", "GPU 1"},
	{"Tg0j", "GPU 2"},
	{"Tg0H", "GPU 3"},
	{"Tg0n", "GPU 4"},
	// Memory, SSD, other
	{"Tm0P", "Memory"},
	{"Ts0P", "SSD"},
	{"Ts1P", "SSD 2"},
	{"TaLP", "Airflow Left"},
	{"TaRP", "Airflow Right"},
	{"Ta0P", "Ambient"},
	{"TW0P", "WiFi Module"},
	{"TN0P", "NAND"},
}
