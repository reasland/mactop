package collector

import (
	"fmt"
	"strings"

	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
	"github.com/rileyeasland/mactop/internal/smc"
)

// TempCollector gathers thermal sensor readings. It tries the IOHIDEventSystem
// API first (works on macOS 26+), falling back to direct SMC access.
type TempCollector struct {
	Data        metrics.TemperatureMetrics
	hidReader   *platform.HIDThermalReader
	conn        *smc.Connection
	sensorDefs  []smc.SensorDef // resolved on first SMC collect
	useHID bool // true once HID is confirmed working
	useSMC      bool  // true once SMC is confirmed working
	failed      bool  // true if both sources failed
}

func NewTempCollector() *TempCollector {
	return &TempCollector{}
}

func (c *TempCollector) Name() string { return "temperature" }

func (c *TempCollector) Collect() error {
	if c.failed {
		return nil
	}

	// Once a source is selected, use it directly.
	if c.useHID {
		return c.collectHID()
	}
	if c.useSMC {
		return c.collectSMC()
	}

	// First call: try HID, then SMC.
	if c.tryInitHID() {
		return nil
	}
	if c.tryInitSMC() {
		return nil
	}

	// Both failed.
	c.failed = true
	c.Data = metrics.TemperatureMetrics{Available: false}
	return nil
}

// tryInitHID attempts to create and use the HID thermal reader.
func (c *TempCollector) tryInitHID() bool {
	reader, err := platform.NewHIDThermalReader()
	if err != nil {
		return false
	}

	sensors, err := reader.ReadTemperatures()
	if err != nil || len(sensors) == 0 {
		reader.Close()
		return false
	}

	c.hidReader = reader
	c.useHID = true
	c.Data = buildHIDMetrics(sensors)
	return true
}

// tryInitSMC attempts to open SMC and read sensors (existing behavior).
func (c *TempCollector) tryInitSMC() bool {
	conn, err := smc.Open()
	if err != nil {
		return false
	}
	c.conn = conn

	discovered, err := conn.DiscoverTempSensors()
	if err == nil && len(discovered) > 0 {
		c.sensorDefs = discovered
	} else {
		c.sensorDefs = smc.AppleSiliconSensors
	}

	if err := c.collectSMCInner(); err != nil || !c.Data.Available {
		c.conn.Close()
		c.conn = nil
		return false
	}

	c.useSMC = true
	return true
}

func (c *TempCollector) collectHID() error {
	sensors, err := c.hidReader.ReadTemperatures()
	if err != nil {
		return err
	}
	c.Data = buildHIDMetrics(sensors)
	return nil
}

func (c *TempCollector) collectSMC() error {
	return c.collectSMCInner()
}

func (c *TempCollector) collectSMCInner() error {
	sensors := make([]metrics.TempSensor, 0, len(c.sensorDefs))

	for _, def := range c.sensorDefs {
		val, err := c.conn.ReadFloat(def.Key)
		if err != nil {
			continue
		}
		if val < -20 || val > 150 {
			continue
		}
		sensors = append(sensors, metrics.TempSensor{
			Name:   def.Name,
			SMCKey: def.Key,
			Value:  val,
		})
	}

	c.Data = metrics.TemperatureMetrics{
		Sensors:   sensors,
		Available: len(sensors) > 0,
	}
	return nil
}

// buildHIDMetrics transforms raw HID sensors into display-friendly metrics.
// It aggregates die sensors and picks the most relevant readings.
func buildHIDMetrics(raw []platform.HIDTempSensor) metrics.TemperatureMetrics {
	var result []metrics.TempSensor
	var dieTemps []float64

	var batteryDone, calDone, ssdDone bool

	for _, s := range raw {
		lower := strings.ToLower(s.Name)

		// Collect all tdie sensors for averaging.
		if strings.HasPrefix(lower, "pmu tdie") {
			dieTemps = append(dieTemps, s.Value)
			continue
		}

		// Battery: keep first only.
		if strings.Contains(lower, "gas gauge battery") {
			if batteryDone {
				continue
			}
			batteryDone = true
			result = append(result, metrics.TempSensor{
				Name:  "Battery",
				Value: s.Value,
			})
			continue
		}

		// SSD.
		if strings.HasPrefix(lower, "nand ch0 temp") {
			if ssdDone {
				continue
			}
			ssdDone = true
			result = append(result, metrics.TempSensor{
				Name:  "SSD",
				Value: s.Value,
			})
			continue
		}

		// CPU calibrated.
		if lower == "pmu tcal" {
			if calDone {
				continue
			}
			calDone = true
			result = append(result, metrics.TempSensor{
				Name:  "CPU Calibrated",
				Value: s.Value,
			})
			continue
		}
	}

	// Synthesize average and max from die sensors.
	if len(dieTemps) > 0 {
		max := dieTemps[0]
		sum := dieTemps[0]
		for _, t := range dieTemps[1:] {
			sum += t
			if t > max {
				max = t
			}
		}
		avg := sum / float64(len(dieTemps))

		// Prepend die metrics so they appear first.
		dieSensors := []metrics.TempSensor{
			{Name: fmt.Sprintf("CPU Die Avg (%d)", len(dieTemps)), Value: avg},
			{Name: "CPU Die Max", Value: max},
		}
		result = append(dieSensors, result...)
	}

	return metrics.TemperatureMetrics{
		Sensors:   result,
		Available: len(result) > 0,
	}
}

// Close releases the SMC connection and HID reader if open.
func (c *TempCollector) Close() {
	if c.hidReader != nil {
		c.hidReader.Close()
		c.hidReader = nil
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}
