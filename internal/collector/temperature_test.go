package collector

import (
	"strings"
	"testing"

	"github.com/rileyeasland/mactop/internal/platform"
)

func TestBuildHIDMetrics_EmptySensorList(t *testing.T) {
	result := buildHIDMetrics(nil)
	if result.Available {
		t.Error("expected Available=false for empty sensor list")
	}
	if len(result.Sensors) != 0 {
		t.Errorf("expected 0 sensors, got %d", len(result.Sensors))
	}
}

func TestBuildHIDMetrics_EmptySlice(t *testing.T) {
	result := buildHIDMetrics([]platform.HIDTempSensor{})
	if result.Available {
		t.Error("expected Available=false for empty slice")
	}
}

func TestBuildHIDMetrics_OnlyTdieSensors(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tdie0", Value: 40.0},
		{Name: "PMU tdie1", Value: 50.0},
		{Name: "PMU tdie2", Value: 60.0},
	}
	result := buildHIDMetrics(sensors)

	if !result.Available {
		t.Fatal("expected Available=true")
	}
	if len(result.Sensors) != 2 {
		t.Fatalf("expected 2 sensors (Die Avg + Die Max), got %d", len(result.Sensors))
	}

	// Die Avg should be (40+50+60)/3 = 50.0
	avg := result.Sensors[0]
	if !strings.HasPrefix(avg.Name, "CPU Die Avg") {
		t.Errorf("first sensor name = %q, want prefix 'CPU Die Avg'", avg.Name)
	}
	if !strings.Contains(avg.Name, "(3)") {
		t.Errorf("first sensor name = %q, should contain count '(3)'", avg.Name)
	}
	if avg.Value != 50.0 {
		t.Errorf("Die Avg value = %f, want 50.0", avg.Value)
	}

	// Die Max should be 60.0
	max := result.Sensors[1]
	if max.Name != "CPU Die Max" {
		t.Errorf("second sensor name = %q, want 'CPU Die Max'", max.Name)
	}
	if max.Value != 60.0 {
		t.Errorf("Die Max value = %f, want 60.0", max.Value)
	}
}

func TestBuildHIDMetrics_MixedSensors(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tdie0", Value: 45.0},
		{Name: "PMU tdie1", Value: 55.0},
		{Name: "gas gauge battery", Value: 30.0},
		{Name: "NAND CH0 temp", Value: 35.0},
		{Name: "PMU tcal", Value: 42.0},
	}
	result := buildHIDMetrics(sensors)

	if !result.Available {
		t.Fatal("expected Available=true")
	}

	// Expect: Die Avg, Die Max, Battery, SSD, CPU Calibrated = 5 sensors
	if len(result.Sensors) != 5 {
		t.Fatalf("expected 5 sensors, got %d", len(result.Sensors))
	}

	// Die sensors should come first
	if !strings.HasPrefix(result.Sensors[0].Name, "CPU Die Avg") {
		t.Errorf("first sensor should be Die Avg, got %q", result.Sensors[0].Name)
	}
	if result.Sensors[1].Name != "CPU Die Max" {
		t.Errorf("second sensor should be Die Max, got %q", result.Sensors[1].Name)
	}

	// Check remaining sensors by name
	names := make(map[string]float64)
	for _, s := range result.Sensors {
		names[s.Name] = s.Value
	}
	if v, ok := names["Battery"]; !ok || v != 30.0 {
		t.Errorf("Battery sensor missing or wrong value: ok=%v, v=%f", ok, v)
	}
	if v, ok := names["SSD"]; !ok || v != 35.0 {
		t.Errorf("SSD sensor missing or wrong value: ok=%v, v=%f", ok, v)
	}
	if v, ok := names["CPU Calibrated"]; !ok || v != 42.0 {
		t.Errorf("CPU Calibrated sensor missing or wrong value: ok=%v, v=%f", ok, v)
	}
}

func TestBuildHIDMetrics_BatteryDeduplication(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "gas gauge battery", Value: 30.0},
		{Name: "gas gauge battery", Value: 31.0},
		{Name: "gas gauge battery", Value: 32.0},
	}
	result := buildHIDMetrics(sensors)

	if !result.Available {
		t.Fatal("expected Available=true")
	}
	if len(result.Sensors) != 1 {
		t.Fatalf("expected 1 sensor (deduplicated battery), got %d", len(result.Sensors))
	}
	if result.Sensors[0].Value != 30.0 {
		t.Errorf("should keep first battery value (30.0), got %f", result.Sensors[0].Value)
	}
}

func TestBuildHIDMetrics_TcalDeduplication(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tcal", Value: 40.0},
		{Name: "PMU tcal", Value: 41.0},
	}
	result := buildHIDMetrics(sensors)

	if len(result.Sensors) != 1 {
		t.Fatalf("expected 1 sensor (deduplicated tcal), got %d", len(result.Sensors))
	}
	if result.Sensors[0].Name != "CPU Calibrated" {
		t.Errorf("expected 'CPU Calibrated', got %q", result.Sensors[0].Name)
	}
	if result.Sensors[0].Value != 40.0 {
		t.Errorf("should keep first tcal value (40.0), got %f", result.Sensors[0].Value)
	}
}

func TestBuildHIDMetrics_SSDDeduplication(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "NAND CH0 temp sensor1", Value: 35.0},
		{Name: "NAND CH0 temp sensor2", Value: 36.0},
	}
	result := buildHIDMetrics(sensors)

	if len(result.Sensors) != 1 {
		t.Fatalf("expected 1 sensor (deduplicated SSD), got %d", len(result.Sensors))
	}
	if result.Sensors[0].Name != "SSD" {
		t.Errorf("expected 'SSD', got %q", result.Sensors[0].Name)
	}
}

func TestBuildHIDMetrics_SingleDieSensor(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tdie0", Value: 42.5},
	}
	result := buildHIDMetrics(sensors)

	if len(result.Sensors) != 2 {
		t.Fatalf("expected 2 sensors (avg + max), got %d", len(result.Sensors))
	}
	// With a single die sensor, avg == max == the single value
	if result.Sensors[0].Value != 42.5 {
		t.Errorf("Die Avg with single sensor = %f, want 42.5", result.Sensors[0].Value)
	}
	if result.Sensors[1].Value != 42.5 {
		t.Errorf("Die Max with single sensor = %f, want 42.5", result.Sensors[1].Value)
	}
	if !strings.Contains(result.Sensors[0].Name, "(1)") {
		t.Errorf("Die Avg name should show count (1), got %q", result.Sensors[0].Name)
	}
}

func TestBuildHIDMetrics_AllZeroDieTemps(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tdie0", Value: 0.0},
		{Name: "PMU tdie1", Value: 0.0},
	}
	result := buildHIDMetrics(sensors)

	if !result.Available {
		t.Fatal("expected Available=true even with zero temps")
	}
	if result.Sensors[0].Value != 0.0 {
		t.Errorf("Die Avg = %f, want 0.0", result.Sensors[0].Value)
	}
	if result.Sensors[1].Value != 0.0 {
		t.Errorf("Die Max = %f, want 0.0", result.Sensors[1].Value)
	}
}

func TestBuildHIDMetrics_DieMaxCorrectWithNegatives(t *testing.T) {
	// max is initialized to dieTemps[0], so negative temps are handled correctly.
	sensors := []platform.HIDTempSensor{
		{Name: "PMU tdie0", Value: -5.0},
		{Name: "PMU tdie1", Value: -3.0},
	}
	result := buildHIDMetrics(sensors)

	if result.Sensors[1].Value != -3.0 {
		t.Errorf("Die Max with all negative temps = %f, want -3.0", result.Sensors[1].Value)
	}
	// Avg: (-5 + -3) / 2 = -4.0
	if result.Sensors[0].Value != -4.0 {
		t.Errorf("Die Avg = %f, want -4.0", result.Sensors[0].Value)
	}
}

func TestBuildHIDMetrics_UnrecognizedSensorsSkipped(t *testing.T) {
	sensors := []platform.HIDTempSensor{
		{Name: "some random sensor", Value: 42.0},
		{Name: "another unknown", Value: 43.0},
	}
	result := buildHIDMetrics(sensors)

	if result.Available {
		t.Error("expected Available=false when no sensors match known patterns")
	}
	if len(result.Sensors) != 0 {
		t.Errorf("expected 0 sensors for unrecognized inputs, got %d", len(result.Sensors))
	}
}

func TestBuildHIDMetrics_DieSensorsOrderedFirst(t *testing.T) {
	// Battery comes first in input, but die sensors should be prepended in output.
	sensors := []platform.HIDTempSensor{
		{Name: "gas gauge battery", Value: 30.0},
		{Name: "PMU tdie0", Value: 50.0},
	}
	result := buildHIDMetrics(sensors)

	if len(result.Sensors) != 3 {
		t.Fatalf("expected 3 sensors, got %d", len(result.Sensors))
	}
	if !strings.HasPrefix(result.Sensors[0].Name, "CPU Die Avg") {
		t.Errorf("first sensor should be Die Avg, got %q", result.Sensors[0].Name)
	}
	if result.Sensors[1].Name != "CPU Die Max" {
		t.Errorf("second sensor should be Die Max, got %q", result.Sensors[1].Name)
	}
	if result.Sensors[2].Name != "Battery" {
		t.Errorf("third sensor should be Battery, got %q", result.Sensors[2].Name)
	}
}

func TestBuildHIDMetrics_CaseInsensitiveMatching(t *testing.T) {
	// Verify that matching is case-insensitive for known patterns.
	sensors := []platform.HIDTempSensor{
		{Name: "PMU TDIE0", Value: 45.0},
		{Name: "Gas Gauge Battery", Value: 30.0},
		{Name: "NAND ch0 TEMP something", Value: 35.0},
		{Name: "PMU TCAL", Value: 42.0},
	}
	result := buildHIDMetrics(sensors)

	if !result.Available {
		t.Fatal("expected Available=true with case-variant sensor names")
	}

	names := make(map[string]bool)
	for _, s := range result.Sensors {
		names[s.Name] = true
	}
	for _, expected := range []string{"Battery", "SSD", "CPU Calibrated"} {
		if !names[expected] {
			t.Errorf("expected sensor %q to be present", expected)
		}
	}
}
