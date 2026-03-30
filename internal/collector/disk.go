package collector

import (
	"syscall"
	"time"

	"github.com/rileyeasland/mactop/internal/metrics"
	"github.com/rileyeasland/mactop/internal/platform"
)

// DiskCollector gathers disk capacity and I/O throughput.
type DiskCollector struct {
	Data           metrics.DiskMetrics
	prevReadBytes  uint64
	prevWriteBytes uint64
	prevTime       time.Time
}

func NewDiskCollector() *DiskCollector {
	return &DiskCollector{}
}

func (c *DiskCollector) Name() string { return "disk" }

func (c *DiskCollector) Collect() error {
	now := time.Now()

	// Disk capacity via Statfs on root volume.
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total := uint64(stat.Blocks) * uint64(stat.Bsize)
		avail := uint64(stat.Bavail) * uint64(stat.Bsize)
		used := total - avail

		c.Data.Volumes = []metrics.VolumeInfo{
			{
				MountPoint: "/",
				Total:      total,
				Used:       used,
				Available:  avail,
			},
		}
	}

	// Disk I/O via IOKit IOBlockStorageDriver.
	var totalRead, totalWrite uint64

	err := platform.IOKitIterateMatching("IOBlockStorageDriver", func(props *platform.CFDict) error {
		statsDict, ok := props.GetDict("Statistics")
		if !ok {
			return nil
		}

		if r, ok := statsDict.GetInt64("Bytes (Read)"); ok {
			totalRead += uint64(r)
		}
		if w, ok := statsDict.GetInt64("Bytes (Write)"); ok {
			totalWrite += uint64(w)
		}

		return nil
	})

	if err == nil {
		c.Data.ReadBytes = totalRead
		c.Data.WriteBytes = totalWrite

		// Compute throughput from delta.
		elapsed := now.Sub(c.prevTime).Seconds()
		if elapsed > 0 && !c.prevTime.IsZero() {
			// Check for counter wraparound before subtracting.
			var dRead uint64
			if totalRead >= c.prevReadBytes {
				dRead = totalRead - c.prevReadBytes
			}
			var dWrite uint64
			if totalWrite >= c.prevWriteBytes {
				dWrite = totalWrite - c.prevWriteBytes
			}

			c.Data.ReadBPS = float64(dRead) / elapsed
			c.Data.WriteBPS = float64(dWrite) / elapsed
		}

		c.prevReadBytes = totalRead
		c.prevWriteBytes = totalWrite
		c.prevTime = now
	}

	return nil
}
