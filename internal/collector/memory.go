package collector

import (
	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
)

// MemoryCollector gathers system memory usage via host_statistics64 and sysctl.
type MemoryCollector struct {
	Data     metrics.MemoryMetrics
	totalMem uint64 // cached hw.memsize (never changes at runtime)
}

func NewMemoryCollector() *MemoryCollector {
	c := &MemoryCollector{}
	if total, err := platform.SysctlUint64("hw.memsize"); err == nil {
		c.totalMem = total
	}
	return c
}

func (c *MemoryCollector) Name() string { return "memory" }

func (c *MemoryCollector) Collect() error {
	vm, err := platform.HostVMInfo64()
	if err != nil {
		return err
	}

	pageSize := vm.PageSize
	total := c.totalMem
	if total == 0 {
		// Fallback if init-time read failed.
		var err error
		total, err = platform.SysctlUint64("hw.memsize")
		if err != nil {
			return err
		}
		c.totalMem = total
	}

	active := vm.ActiveCount * pageSize
	inactive := vm.InactiveCount * pageSize
	wired := vm.WireCount * pageSize
	compressed := vm.CompressorCount * pageSize
	free := vm.FreeCount * pageSize

	used := active + wired + compressed

	// Swap usage.
	swap, err := platform.GetSwapUsage()
	if err != nil {
		swap = &platform.SwapUsage{}
	}

	c.Data = metrics.MemoryMetrics{
		Total:      total,
		Used:       used,
		Free:       free,
		Active:     active,
		Inactive:   inactive,
		Wired:      wired,
		Compressed: compressed,
		SwapTotal:  swap.Total,
		SwapUsed:   swap.Used,
		PageSize:   pageSize,
	}

	return nil
}
