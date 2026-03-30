package collector

import (
	"errors"

	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
)

// errGPUFound is a sentinel used to stop IOKit iteration after the first
// successful GPU match.
var errGPUFound = errors.New("gpu found")

// GPUCollector gathers GPU utilization via IOKit IOAccelerator.
type GPUCollector struct {
	Data metrics.GPUMetrics
}

func NewGPUCollector() *GPUCollector {
	return &GPUCollector{}
}

func (c *GPUCollector) Name() string { return "gpu" }

func (c *GPUCollector) Collect() error {
	c.Data = metrics.GPUMetrics{Available: false}

	err := platform.IOKitIterateMatching("IOAccelerator", func(props *platform.CFDict) error {
		// Look for PerformanceStatistics sub-dictionary.
		perfStats, ok := props.GetDict("PerformanceStatistics")
		if !ok {
			return nil
		}

		if util, ok := perfStats.GetInt64("Device Utilization %"); ok {
			c.Data.Utilization = float64(util)
			c.Data.Available = true
		}

		if inUse, ok := perfStats.GetInt64("In use system memory"); ok {
			c.Data.InUseMemory = uint64(inUse)
		}

		if alloc, ok := perfStats.GetInt64("Allocated system memory"); ok {
			c.Data.AllocatedMemory = uint64(alloc)
		}

		// Only process the first matching IOAccelerator entry.
		if c.Data.Available {
			return errGPUFound
		}
		return nil
	})

	if err != nil && err != errGPUFound {
		return err
	}

	return nil
}
