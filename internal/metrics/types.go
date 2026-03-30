package metrics

import "time"

// SystemMetrics is the top-level container returned by the collector each tick.
type SystemMetrics struct {
	Timestamp   time.Time
	CPU         CPUMetrics
	GPU         GPUMetrics
	Memory      MemoryMetrics
	Network     []NetworkInterface
	Disk        DiskMetrics
	Power       PowerMetrics
	Temperature TemperatureMetrics
}

// CPUMetrics holds per-core and aggregate CPU utilization.
type CPUMetrics struct {
	Cores      []CoreUsage
	Aggregate  float64 // 0.0 - 100.0
	PCoreCount int
	ECoreCount int
}

// CoreUsage holds utilization data for a single CPU core.
type CoreUsage struct {
	ID      int
	User    float64 // percentage
	System  float64
	Idle    float64
	Nice    float64
	Total   float64 // user + system + nice
	IsECore bool
}

// GPUMetrics holds GPU utilization and memory.
type GPUMetrics struct {
	Utilization     float64 // 0.0 - 100.0
	InUseMemory     uint64  // bytes
	AllocatedMemory uint64  // bytes
	Available       bool    // false if we couldn't read GPU stats
}

// MemoryMetrics holds system memory usage.
type MemoryMetrics struct {
	Total      uint64 // bytes
	Used       uint64
	Free       uint64
	Active     uint64
	Inactive   uint64
	Wired      uint64
	Compressed uint64
	SwapTotal  uint64
	SwapUsed   uint64
	PageSize   uint64
}

// NetworkInterface holds per-interface network counters.
type NetworkInterface struct {
	Name       string
	BytesIn    uint64  // cumulative
	BytesOut   uint64
	BytesInPS  float64 // per second (computed)
	BytesOutPS float64
}

// DiskMetrics holds disk capacity and I/O stats.
type DiskMetrics struct {
	Volumes    []VolumeInfo
	ReadBytes  uint64  // cumulative
	WriteBytes uint64
	ReadBPS    float64 // bytes per second (computed)
	WriteBPS   float64
}

// VolumeInfo holds capacity information for a single mounted volume.
type VolumeInfo struct {
	MountPoint string
	Total      uint64
	Used       uint64
	Available  uint64
}

// PowerMetrics holds battery and power source info.
type PowerMetrics struct {
	HasBattery     bool
	BatteryPercent int     // 0-100
	IsCharging     bool
	PowerSource    string  // "AC Power" or "Battery Power"
	Voltage        float64 // volts
	Amperage       float64 // amps (negative = discharging)
	Wattage        float64 // watts (computed: |V * A|)
	TimeRemaining  int     // minutes, -1 if calculating
}

// TemperatureMetrics holds thermal sensor readings.
type TemperatureMetrics struct {
	Sensors   []TempSensor
	Available bool
}

// TempSensor holds a single temperature reading.
type TempSensor struct {
	Name   string  // human-readable: "CPU P-Core", "GPU", etc.
	SMCKey string  // e.g., "Tp0T"
	Value  float64 // degrees Celsius
}
