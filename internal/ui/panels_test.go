package ui

import (
	"strings"
	"testing"

	"github.com/rileyeasland/mactop/internal/metrics"
)

// --- renderNetworkPanel tests ---

func TestRenderNetworkPanel_EmptyInterfaces(t *testing.T) {
	result := renderNetworkPanel(nil, 80)
	if !strings.Contains(result, "No active interfaces") {
		t.Error("empty interface list should show 'No active interfaces'")
	}
}

func TestRenderNetworkPanel_EmptySlice(t *testing.T) {
	result := renderNetworkPanel([]metrics.NetworkInterface{}, 80)
	if !strings.Contains(result, "No active interfaces") {
		t.Error("empty slice should show 'No active interfaces'")
	}
}

func TestRenderNetworkPanel_ZeroBytesSkipped(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 0, BytesOut: 0, BytesInPS: 0, BytesOutPS: 0},
	}
	result := renderNetworkPanel(ifaces, 80)
	// The interface should be skipped since BytesIn and BytesOut are both 0.
	if strings.Contains(result, "en0") {
		t.Error("interface with zero bytes should be skipped")
	}
}

func TestRenderNetworkPanel_ActiveInterface(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 1024, BytesOut: 2048, BytesInPS: 100, BytesOutPS: 200},
	}
	result := renderNetworkPanel(ifaces, 80)
	if !strings.Contains(result, "en0") {
		t.Error("active interface name should appear in output")
	}
	if !strings.Contains(result, "In:") {
		t.Error("should show In: rate")
	}
	if !strings.Contains(result, "Out:") {
		t.Error("should show Out: rate")
	}
}

func TestRenderNetworkPanel_MultipleInterfaces(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 1000, BytesOut: 500, BytesInPS: 100, BytesOutPS: 50},
		{Name: "en1", BytesIn: 2000, BytesOut: 1000, BytesInPS: 200, BytesOutPS: 100},
	}
	result := renderNetworkPanel(ifaces, 80)
	if !strings.Contains(result, "en0") {
		t.Error("en0 should appear")
	}
	if !strings.Contains(result, "en1") {
		t.Error("en1 should appear")
	}
}

func TestRenderNetworkPanel_ContainsHeading(t *testing.T) {
	result := renderNetworkPanel(nil, 80)
	if !strings.Contains(result, "Network") {
		t.Error("panel should contain 'Network' heading")
	}
}

// --- renderTemperaturePanel tests ---

func TestRenderTemperaturePanel_NotAvailable(t *testing.T) {
	temp := metrics.TemperatureMetrics{Available: false}
	result := renderTemperaturePanel(temp)
	if !strings.Contains(result, "N/A") {
		t.Error("unavailable temperature should show 'N/A'")
	}
}

func TestRenderTemperaturePanel_EmptySensors(t *testing.T) {
	temp := metrics.TemperatureMetrics{Available: true, Sensors: nil}
	result := renderTemperaturePanel(temp)
	if !strings.Contains(result, "N/A") {
		t.Error("empty sensors should show 'N/A'")
	}
}

func TestRenderTemperaturePanel_WithSensors(t *testing.T) {
	temp := metrics.TemperatureMetrics{
		Available: true,
		Sensors: []metrics.TempSensor{
			{Name: "CPU Die", Value: 45.5},
			{Name: "GPU", Value: 38.2},
		},
	}
	result := renderTemperaturePanel(temp)
	if !strings.Contains(result, "CPU Die") {
		t.Error("sensor name 'CPU Die' should appear")
	}
	if !strings.Contains(result, "45.5") {
		t.Error("sensor value 45.5 should appear")
	}
	if !strings.Contains(result, "GPU") {
		t.Error("sensor name 'GPU' should appear")
	}
}

func TestRenderTemperaturePanel_HighTempWarning(t *testing.T) {
	temp := metrics.TemperatureMetrics{
		Available: true,
		Sensors: []metrics.TempSensor{
			{Name: "Hot CPU", Value: 95.0},
		},
	}
	result := renderTemperaturePanel(temp)
	// The value should still appear (warn styling is applied but text is present).
	if !strings.Contains(result, "95.0") {
		t.Error("high temperature value should appear")
	}
}

func TestRenderTemperaturePanel_ContainsHeading(t *testing.T) {
	temp := metrics.TemperatureMetrics{Available: false}
	result := renderTemperaturePanel(temp)
	if !strings.Contains(result, "Temperatures") {
		t.Error("panel should contain 'Temperatures' heading")
	}
}

// --- renderPowerPanel tests ---

func TestRenderPowerPanel_NoBattery(t *testing.T) {
	power := metrics.PowerMetrics{HasBattery: false}
	result := renderPowerPanel(power)
	if !strings.Contains(result, "AC Power") {
		t.Error("no battery should show 'AC Power'")
	}
	if !strings.Contains(result, "No battery") {
		t.Error("no battery should show 'No battery'")
	}
}

func TestRenderPowerPanel_BatteryCharging(t *testing.T) {
	power := metrics.PowerMetrics{
		HasBattery:     true,
		BatteryPercent: 75,
		IsCharging:     true,
		PowerSource:    "AC Power",
	}
	result := renderPowerPanel(power)
	if !strings.Contains(result, "75%") {
		t.Error("battery percentage should appear")
	}
	if !strings.Contains(result, "Charging") {
		t.Error("charging status should appear")
	}
}

func TestRenderPowerPanel_BatteryDischarging(t *testing.T) {
	power := metrics.PowerMetrics{
		HasBattery:     true,
		BatteryPercent: 50,
		IsCharging:     false,
		PowerSource:    "Battery Power",
	}
	result := renderPowerPanel(power)
	if !strings.Contains(result, "Discharging") {
		t.Error("discharging status should appear")
	}
}

func TestRenderPowerPanel_WithWattage(t *testing.T) {
	power := metrics.PowerMetrics{
		HasBattery:     true,
		BatteryPercent: 80,
		IsCharging:     true,
		PowerSource:    "AC Power",
		Wattage:        30.5,
	}
	result := renderPowerPanel(power)
	if !strings.Contains(result, "30.5") {
		t.Error("wattage value should appear")
	}
	if !strings.Contains(result, "W") {
		t.Error("watts unit should appear")
	}
}

func TestRenderPowerPanel_ZeroWattageHidden(t *testing.T) {
	power := metrics.PowerMetrics{
		HasBattery:     true,
		BatteryPercent: 100,
		IsCharging:     false,
		PowerSource:    "Battery Power",
		Wattage:        0,
	}
	result := renderPowerPanel(power)
	if strings.Contains(result, "Power:") {
		t.Error("zero wattage should not show Power: line")
	}
}

func TestRenderPowerPanel_ContainsHeading(t *testing.T) {
	result := renderPowerPanel(metrics.PowerMetrics{})
	if !strings.Contains(result, "Power") {
		t.Error("panel should contain 'Power' heading")
	}
}

// --- renderGPUPanel tests ---

func TestRenderGPUPanel_NotAvailable(t *testing.T) {
	gpu := metrics.GPUMetrics{Available: false}
	result := renderGPUPanel(gpu, 80)
	if !strings.Contains(result, "N/A") {
		t.Error("unavailable GPU should show 'N/A'")
	}
}

func TestRenderGPUPanel_WithUtilization(t *testing.T) {
	gpu := metrics.GPUMetrics{
		Available:   true,
		Utilization: 55.0,
	}
	result := renderGPUPanel(gpu, 80)
	if !strings.Contains(result, "55%") {
		t.Error("GPU utilization percentage should appear")
	}
}

func TestRenderGPUPanel_WithMemory(t *testing.T) {
	gpu := metrics.GPUMetrics{
		Available:       true,
		Utilization:     30.0,
		InUseMemory:     1024 * 1024 * 512, // 512 MB
		AllocatedMemory: 1024 * 1024 * 1024, // 1 GB
	}
	result := renderGPUPanel(gpu, 80)
	if !strings.Contains(result, "VRAM") {
		t.Error("GPU memory info should show VRAM label")
	}
	if !strings.Contains(result, "unified") {
		t.Error("Apple Silicon GPU should show 'unified'")
	}
}

func TestRenderGPUPanel_ContainsHeading(t *testing.T) {
	result := renderGPUPanel(metrics.GPUMetrics{}, 80)
	if !strings.Contains(result, "GPU Usage") {
		t.Error("panel should contain 'GPU Usage' heading")
	}
}

// --- renderDiskPanel tests ---

func TestRenderDiskPanel_WithVolumes(t *testing.T) {
	disk := metrics.DiskMetrics{
		Volumes: []metrics.VolumeInfo{
			{MountPoint: "/", Total: 500 * 1024 * 1024 * 1024, Used: 250 * 1024 * 1024 * 1024},
		},
	}
	result := renderDiskPanel(disk, 80)
	if !strings.Contains(result, "/") {
		t.Error("mount point should appear")
	}
	if !strings.Contains(result, "50%") {
		t.Error("disk usage percentage should appear")
	}
}

func TestRenderDiskPanel_WithIOStats(t *testing.T) {
	disk := metrics.DiskMetrics{
		ReadBPS:  1024 * 1024,
		WriteBPS: 512 * 1024,
	}
	result := renderDiskPanel(disk, 80)
	if !strings.Contains(result, "Read:") {
		t.Error("read rate should appear")
	}
	if !strings.Contains(result, "Write:") {
		t.Error("write rate should appear")
	}
}

func TestRenderDiskPanel_EmptyVolumes(t *testing.T) {
	disk := metrics.DiskMetrics{}
	result := renderDiskPanel(disk, 80)
	if !strings.Contains(result, "Disk") {
		t.Error("panel should contain 'Disk' heading")
	}
	// Should still show Read/Write stats even with no volumes.
	if !strings.Contains(result, "Read:") {
		t.Error("should still show Read: rate")
	}
}

func TestRenderDiskPanel_ZeroTotalVolume(t *testing.T) {
	disk := metrics.DiskMetrics{
		Volumes: []metrics.VolumeInfo{
			{MountPoint: "/", Total: 0, Used: 0},
		},
	}
	result := renderDiskPanel(disk, 80)
	// Should not panic with division by zero; pct should be 0%.
	if !strings.Contains(result, "0%") {
		t.Error("zero total should show 0%")
	}
}

// --- formatBytes additional edge cases ---

func TestFormatBytes_One(t *testing.T) {
	result := formatBytes(1)
	if result != "1 B" {
		t.Errorf("formatBytes(1) = %q, want %q", result, "1 B")
	}
}

func TestFormatBytes_JustBelowKB(t *testing.T) {
	result := formatBytes(1023)
	if result != "1023 B" {
		t.Errorf("formatBytes(1023) = %q, want %q", result, "1023 B")
	}
}

func TestFormatBytes_JustAboveKB(t *testing.T) {
	result := formatBytes(1025)
	if !strings.HasPrefix(result, "1.0 KB") {
		t.Errorf("formatBytes(1025) = %q, want prefix '1.0 KB'", result)
	}
}

func TestFormatBytes_TwoGB(t *testing.T) {
	val := uint64(2) * 1024 * 1024 * 1024
	result := formatBytes(val)
	if result != "2.0 GB" {
		t.Errorf("formatBytes(2GB) = %q, want %q", result, "2.0 GB")
	}
}

// --- renderCPUPanelWithGraph tests ---

func TestRenderCPUPanelWithGraph_WithPopulatedHistory(t *testing.T) {
	cpu := metrics.CPUMetrics{Aggregate: 60.0}
	hist := NewRingBuffer(20)
	for i := 0; i < 10; i++ {
		hist.Push(float64(i * 10))
	}
	result := renderCPUPanelWithGraph(cpu, hist, 80, 3)
	if result == "" {
		t.Error("CPU panel with graph should produce non-empty output")
	}
	if !strings.Contains(result, "CPU Usage") {
		t.Error("should contain CPU Usage heading")
	}
}

func TestRenderCPUPanelWithGraph_InsufficientHistory(t *testing.T) {
	cpu := metrics.CPUMetrics{Aggregate: 50.0}
	hist := NewRingBuffer(10)
	hist.Push(50.0) // only 1 sample, need >= 2 for graph
	result := renderCPUPanelWithGraph(cpu, hist, 80, 3)
	// Should still show panel but without graph appended.
	if result == "" {
		t.Error("should produce non-empty output")
	}
	if !strings.Contains(result, "CPU Usage") {
		t.Error("should contain CPU Usage heading")
	}
}

func TestRenderCPUPanelWithGraph_NarrowWidth(t *testing.T) {
	cpu := metrics.CPUMetrics{Aggregate: 45.0}
	hist := NewRingBuffer(20)
	for i := 0; i < 5; i++ {
		hist.Push(float64(i * 20))
	}
	// Width small enough that graphWidth < minGraphWidth.
	result := renderCPUPanelWithGraph(cpu, hist, 15, 2)
	if result == "" {
		t.Error("should produce non-empty output even with narrow width")
	}
}

// --- renderNetworkPanelWithGraph tests ---

func TestRenderNetworkPanelWithGraph_WithAutoScaling(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 1000, BytesOut: 500, BytesInPS: 2048, BytesOutPS: 1024},
	}
	histIn := NewRingBuffer(20)
	histOut := NewRingBuffer(20)
	for i := 0; i < 10; i++ {
		histIn.Push(float64(1024 * (i + 1)))
		histOut.Push(float64(512 * (i + 1)))
	}
	result := renderNetworkPanelWithGraph(ifaces, histIn, histOut, 80)
	if result == "" {
		t.Error("network panel with graph should produce non-empty output")
	}
	if !strings.Contains(result, "Network") {
		t.Error("should contain Network heading")
	}
}

func TestRenderNetworkPanelWithGraph_InsufficientHistory(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 1000, BytesOut: 500, BytesInPS: 100, BytesOutPS: 50},
	}
	histIn := NewRingBuffer(10)
	histOut := NewRingBuffer(10)
	histIn.Push(100) // only 1 sample
	result := renderNetworkPanelWithGraph(ifaces, histIn, histOut, 80)
	if !strings.Contains(result, "Network") {
		t.Error("should contain Network heading")
	}
}

func TestRenderNetworkPanelWithGraph_NarrowWidth(t *testing.T) {
	ifaces := []metrics.NetworkInterface{
		{Name: "en0", BytesIn: 1000, BytesOut: 500, BytesInPS: 100, BytesOutPS: 50},
	}
	histIn := NewRingBuffer(10)
	histOut := NewRingBuffer(10)
	for i := 0; i < 5; i++ {
		histIn.Push(float64(i * 100))
		histOut.Push(float64(i * 50))
	}
	// Very narrow: graphWidth will be < minGraphWidth.
	result := renderNetworkPanelWithGraph(ifaces, histIn, histOut, 15)
	if result == "" {
		t.Error("should produce non-empty output even with narrow width")
	}
}

// --- autoScaleMax tests ---

func TestAutoScaleMax_EmptyData(t *testing.T) {
	result := autoScaleMaxWithFloor([]float64{}, 1024)
	if result != 1024 {
		t.Errorf("autoScaleMaxWithFloor(empty) = %f, want 1024", result)
	}
}

func TestAutoScaleMax_AllZeros(t *testing.T) {
	result := autoScaleMaxWithFloor([]float64{0, 0, 0}, 1024)
	if result != 1024 {
		t.Errorf("autoScaleMaxWithFloor(all zeros) = %f, want 1024 (floor)", result)
	}
}

func TestAutoScaleMax_NormalData(t *testing.T) {
	result := autoScaleMaxWithFloor([]float64{100, 500, 2000}, 1024)
	// max is 2000, * 1.2 = 2400, above floor of 1024.
	want := 2400.0
	if result != want {
		t.Errorf("autoScaleMaxWithFloor = %f, want %f", result, want)
	}
}

func TestAutoScaleMax_SmallData(t *testing.T) {
	result := autoScaleMaxWithFloor([]float64{10, 20, 30}, 1024)
	// max is 30, * 1.2 = 36, below floor of 1024.
	if result != 1024 {
		t.Errorf("autoScaleMaxWithFloor(small) = %f, want 1024 (floor)", result)
	}
}

func TestAutoScaleMax_SingleValue(t *testing.T) {
	result := autoScaleMaxWithFloor([]float64{5000}, 1024)
	want := 6000.0 // 5000 * 1.2
	if result != want {
		t.Errorf("autoScaleMaxWithFloor(single) = %f, want %f", result, want)
	}
}

// --- formatBytesRate tests ---

func TestFormatBytesRate_Bytes(t *testing.T) {
	result := formatBytesRate(500)
	if result != "500B/s" {
		t.Errorf("formatBytesRate(500) = %q, want %q", result, "500B/s")
	}
}

func TestFormatBytesRate_Zero(t *testing.T) {
	result := formatBytesRate(0)
	if result != "0B/s" {
		t.Errorf("formatBytesRate(0) = %q, want %q", result, "0B/s")
	}
}

func TestFormatBytesRate_Kilobytes(t *testing.T) {
	result := formatBytesRate(2048)
	if result != "2.0K/s" {
		t.Errorf("formatBytesRate(2048) = %q, want %q", result, "2.0K/s")
	}
}

func TestFormatBytesRate_Megabytes(t *testing.T) {
	result := formatBytesRate(1024 * 1024 * 5)
	if result != "5.0M/s" {
		t.Errorf("formatBytesRate(5MB) = %q, want %q", result, "5.0M/s")
	}
}

func TestFormatBytesRate_Gigabytes(t *testing.T) {
	result := formatBytesRate(1024 * 1024 * 1024 * 2)
	if result != "2.0G/s" {
		t.Errorf("formatBytesRate(2GB) = %q, want %q", result, "2.0G/s")
	}
}

func TestFormatBytesRate_JustBelowKB(t *testing.T) {
	result := formatBytesRate(1023)
	if result != "1023B/s" {
		t.Errorf("formatBytesRate(1023) = %q, want %q", result, "1023B/s")
	}
}

func TestFormatBytesRate_ExactKB(t *testing.T) {
	result := formatBytesRate(1024)
	if result != "1.0K/s" {
		t.Errorf("formatBytesRate(1024) = %q, want %q", result, "1.0K/s")
	}
}
