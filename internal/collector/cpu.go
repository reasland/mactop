package collector

import (
	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
)

// cpuTickSample stores a snapshot of ticks for delta computation.
type cpuTickSample struct {
	user   uint32
	system uint32
	idle   uint32
	nice   uint32
}

// CPUCollector gathers per-core and aggregate CPU utilization.
type CPUCollector struct {
	Data      metrics.CPUMetrics
	prevTicks []cpuTickSample
	pCoreCount int
	eCoreCount int
}

// NewCPUCollector creates a CPUCollector and detects P/E core counts.
func NewCPUCollector() *CPUCollector {
	c := &CPUCollector{}

	// Detect P-core and E-core counts on Apple Silicon.
	// perflevel0 = performance cores, perflevel1 = efficiency cores.
	if p, err := platform.SysctlUint32("hw.perflevel0.logicalcpu"); err == nil {
		c.pCoreCount = int(p)
	}
	if e, err := platform.SysctlUint32("hw.perflevel1.logicalcpu"); err == nil {
		c.eCoreCount = int(e)
	}

	return c
}

func (c *CPUCollector) Name() string { return "cpu" }

func (c *CPUCollector) Collect() error {
	ticks, err := platform.HostProcessorInfo()
	if err != nil {
		return err
	}

	numCPU := len(ticks)
	cores := make([]metrics.CoreUsage, numCPU)

	var totalBusy, totalAll float64

	for i := 0; i < numCPU; i++ {
		cu := metrics.CoreUsage{ID: i}

		// Determine if this is an E-core. On Apple Silicon with the
		// heterogeneous layout, P-cores come first, then E-cores.
		if c.pCoreCount > 0 && c.eCoreCount > 0 {
			cu.IsECore = i >= c.pCoreCount
		}

		if c.prevTicks != nil && i < len(c.prevTicks) {
			prev := c.prevTicks[i]

			// Guard against uint32 wraparound: if current < prev, skip
			// this sample rather than producing a wrapped delta.
			if ticks[i].User < prev.user || ticks[i].System < prev.system ||
				ticks[i].Idle < prev.idle || ticks[i].Nice < prev.nice {
				cores[i] = cu
				continue
			}

			dUser := float64(ticks[i].User - prev.user)
			dSystem := float64(ticks[i].System - prev.system)
			dIdle := float64(ticks[i].Idle - prev.idle)
			dNice := float64(ticks[i].Nice - prev.nice)
			dTotal := dUser + dSystem + dIdle + dNice

			if dTotal > 0 {
				cu.User = (dUser / dTotal) * 100
				cu.System = (dSystem / dTotal) * 100
				cu.Idle = (dIdle / dTotal) * 100
				cu.Nice = (dNice / dTotal) * 100
				cu.Total = cu.User + cu.System + cu.Nice
			}

			totalBusy += dUser + dSystem + dNice
			totalAll += dTotal
		}

		cores[i] = cu
	}

	// Store current ticks for next delta.
	c.prevTicks = make([]cpuTickSample, numCPU)
	for i := 0; i < numCPU; i++ {
		c.prevTicks[i] = cpuTickSample{
			user:   ticks[i].User,
			system: ticks[i].System,
			idle:   ticks[i].Idle,
			nice:   ticks[i].Nice,
		}
	}

	var aggregate float64
	if totalAll > 0 {
		aggregate = (totalBusy / totalAll) * 100
	}

	c.Data = metrics.CPUMetrics{
		Cores:      cores,
		Aggregate:  aggregate,
		PCoreCount: c.pCoreCount,
		ECoreCount: c.eCoreCount,
	}

	return nil
}
