package ui

import (
	"fmt"
	"strings"

	"github.com/rileyeasland/mactop/internal/metrics"
)

func renderCPUPanel(cpu metrics.CPUMetrics, width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("CPU Usage"))
	b.WriteByte('\n')

	barWidth := width - 22
	if barWidth < 10 {
		barWidth = 10
	}

	// Group P-cores and E-cores.
	if cpu.PCoreCount > 0 {
		b.WriteString(subHeadingStyle.Render("  P-Cores"))
		b.WriteByte('\n')
	}

	for _, core := range cpu.Cores {
		if core.IsECore {
			continue
		}
		label := fmt.Sprintf("  P%d:", core.ID)
		bar := progressBar(barWidth, core.Total)
		pct := fmt.Sprintf(" %4.0f%%", core.Total)
		b.WriteString(labelStyle.Render(label) + " " + bar + valueStyle.Render(pct))
		b.WriteByte('\n')
	}

	if cpu.ECoreCount > 0 {
		b.WriteString(subHeadingStyle.Render("  E-Cores"))
		b.WriteByte('\n')
		for _, core := range cpu.Cores {
			if !core.IsECore {
				continue
			}
			label := fmt.Sprintf("  E%d:", core.ID-cpu.PCoreCount)
			bar := progressBar(barWidth, core.Total)
			pct := fmt.Sprintf(" %4.0f%%", core.Total)
			b.WriteString(labelStyle.Render(label) + " " + bar + valueStyle.Render(pct))
			b.WriteByte('\n')
		}
	}

	// If no P/E detection, show all cores with C prefix.
	if cpu.PCoreCount == 0 && cpu.ECoreCount == 0 {
		for _, core := range cpu.Cores {
			label := fmt.Sprintf("  C%d:", core.ID)
			bar := progressBar(barWidth, core.Total)
			pct := fmt.Sprintf(" %4.0f%%", core.Total)
			b.WriteString(labelStyle.Render(label) + " " + bar + valueStyle.Render(pct))
			b.WriteByte('\n')
		}
	}

	avgLabel := "  Avg:"
	avgBar := progressBar(barWidth, cpu.Aggregate)
	avgPct := fmt.Sprintf(" %4.0f%%", cpu.Aggregate)
	s := avgLabel + " " + avgBar + avgPct
	if cpu.Aggregate >= 90 {
		s = warnStyle.Render(s)
	} else {
		s = valueStyle.Render(s)
	}
	b.WriteString(s)

	return b.String()
}

func renderGPUPanel(gpu metrics.GPUMetrics, width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("GPU Usage"))
	b.WriteByte('\n')

	if !gpu.Available {
		b.WriteString(dimStyle.Render("  N/A"))
		return b.String()
	}

	barWidth := width - 12
	if barWidth < 10 {
		barWidth = 10
	}

	bar := progressBar(barWidth, gpu.Utilization)
	pct := fmt.Sprintf(" %4.0f%%", gpu.Utilization)
	b.WriteString("  " + bar + valueStyle.Render(pct))
	b.WriteByte('\n')

	if gpu.AllocatedMemory > 0 {
		inUse := formatBytes(gpu.InUseMemory)
		alloc := formatBytes(gpu.AllocatedMemory)
		b.WriteString(labelStyle.Render("  VRAM: ") + valueStyle.Render(fmt.Sprintf("%s / %s", inUse, alloc)))
		b.WriteString(dimStyle.Render(" (unified)"))
	}

	return b.String()
}

func renderMemoryPanel(mem metrics.MemoryMetrics, width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Memory"))
	b.WriteByte('\n')

	barWidth := width - 24
	if barWidth < 10 {
		barWidth = 10
	}

	pct := 0.0
	if mem.Total > 0 {
		pct = float64(mem.Used) / float64(mem.Total) * 100
	}
	bar := progressBar(barWidth, pct)
	b.WriteString(fmt.Sprintf("  Used: %7s  %s %4.0f%%\n",
		formatBytes(mem.Used), bar, pct))
	b.WriteString(fmt.Sprintf("  Wired:      %s\n", valueStyle.Render(formatBytes(mem.Wired))))
	b.WriteString(fmt.Sprintf("  Compressed: %s\n", valueStyle.Render(formatBytes(mem.Compressed))))
	b.WriteString(fmt.Sprintf("  Free:       %s\n", valueStyle.Render(formatBytes(mem.Free))))
	b.WriteString(fmt.Sprintf("  Swap:       %s\n", valueStyle.Render(formatBytes(mem.SwapUsed))))
	b.WriteString(fmt.Sprintf("  Total:      %s", valueStyle.Render(formatBytes(mem.Total))))

	return b.String()
}

func renderNetworkPanel(ifaces []metrics.NetworkInterface, width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Network"))
	b.WriteByte('\n')

	if len(ifaces) == 0 {
		b.WriteString(dimStyle.Render("  No active interfaces"))
		return b.String()
	}

	for _, iface := range ifaces {
		// Only show interfaces with some traffic.
		if iface.BytesIn == 0 && iface.BytesOut == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s:  In: %s/s\n",
			labelStyle.Render(iface.Name),
			valueStyle.Render(formatBytes(uint64(iface.BytesInPS)))))
		b.WriteString(fmt.Sprintf("        Out: %s/s\n",
			valueStyle.Render(formatBytes(uint64(iface.BytesOutPS)))))
	}

	result := b.String()
	if strings.HasSuffix(result, "\n") {
		result = result[:len(result)-1]
	}
	return result
}

func renderDiskPanel(disk metrics.DiskMetrics, width int) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Disk"))
	b.WriteByte('\n')

	barWidth := width - 40
	if barWidth < 10 {
		barWidth = 10
	}

	for _, vol := range disk.Volumes {
		pct := 0.0
		if vol.Total > 0 {
			pct = float64(vol.Used) / float64(vol.Total) * 100
		}
		bar := progressBar(barWidth, pct)
		b.WriteString(fmt.Sprintf("  %s: %s / %s (%2.0f%%) %s\n",
			vol.MountPoint,
			formatBytes(vol.Used), formatBytes(vol.Total),
			pct, bar))
	}

	b.WriteString(fmt.Sprintf("  Read: %s/s   Write: %s/s",
		valueStyle.Render(formatBytes(uint64(disk.ReadBPS))),
		valueStyle.Render(formatBytes(uint64(disk.WriteBPS)))))

	return b.String()
}

func renderPowerPanel(power metrics.PowerMetrics) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Power"))
	b.WriteByte('\n')

	if !power.HasBattery {
		b.WriteString(labelStyle.Render("  Source: ") + valueStyle.Render("AC Power"))
		b.WriteByte('\n')
		b.WriteString(dimStyle.Render("  No battery"))
		return b.String()
	}

	// Battery bar.
	bar := progressBar(10, float64(power.BatteryPercent))
	b.WriteString(fmt.Sprintf("  Battery: %3d%%  [%s]\n", power.BatteryPercent, bar))

	status := "Discharging"
	if power.IsCharging {
		status = "Charging"
	}
	b.WriteString(labelStyle.Render("  Status:  ") + valueStyle.Render(status))
	b.WriteByte('\n')
	b.WriteString(labelStyle.Render("  Source:  ") + valueStyle.Render(power.PowerSource))

	if power.Wattage > 0 {
		b.WriteByte('\n')
		b.WriteString(labelStyle.Render("  Power:   ") + valueStyle.Render(fmt.Sprintf("%.1f W", power.Wattage)))
	}

	return b.String()
}

func renderTemperaturePanel(temp metrics.TemperatureMetrics) string {
	var b strings.Builder
	b.WriteString(headingStyle.Render("Temperatures"))
	b.WriteByte('\n')

	if !temp.Available || len(temp.Sensors) == 0 {
		b.WriteString(dimStyle.Render("  N/A"))
		return b.String()
	}

	for _, s := range temp.Sensors {
		valStr := fmt.Sprintf("%.1f \u00b0C", s.Value)
		if s.Value >= 90 {
			valStr = warnStyle.Render(valStr)
		} else {
			valStr = valueStyle.Render(valStr)
		}
		b.WriteString(fmt.Sprintf("  %-14s %s\n", labelStyle.Render(s.Name+":"), valStr))
	}

	result := b.String()
	if strings.HasSuffix(result, "\n") {
		result = result[:len(result)-1]
	}
	return result
}

// formatBytes converts bytes to a human-readable string.
func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// --- Graph wrapper functions ---

const (
	minGraphWidth = 10
)

// appendSparkline renders a labeled sparkline and appends it below base content.
// The label is placed on its own line above the sparkline graph.
func appendSparkline(base string, label string, data []float64, graphWidth, graphHeight int, opts SparklineOpts) string {
	if graphWidth < minGraphWidth {
		return base
	}
	opts.Width = graphWidth
	opts.Height = graphHeight
	graph := RenderSparkline(data, opts)
	if graph == "" {
		return base
	}
	return base + "\n" + graphLabelStyle.Render(label) + "\n" + graph
}

func renderCPUPanelWithGraph(cpu metrics.CPUMetrics, history *RingBuffer, width, graphHeight int) string {
	base := renderCPUPanel(cpu, width)
	if history.Len() < 2 {
		return base
	}
	label := fmt.Sprintf("  CPU %3.0f%%", cpu.Aggregate)
	gw := width - 2
	return appendSparkline(base, label, history.Values(), gw, graphHeight, SparklineOpts{
		Min: 0, Max: 100, Color: sparkCPUColor,
	})
}

func renderGPUPanelWithGraph(gpu metrics.GPUMetrics, history *RingBuffer, width, graphHeight int) string {
	base := renderGPUPanel(gpu, width)
	if history.Len() < 2 {
		return base
	}
	label := fmt.Sprintf("  GPU %3.0f%%", gpu.Utilization)
	gw := width - 2
	return appendSparkline(base, label, history.Values(), gw, graphHeight, SparklineOpts{
		Min: 0, Max: 100, Color: sparkGPUColor,
	})
}

func renderMemoryPanelWithGraph(mem metrics.MemoryMetrics, history *RingBuffer, width, graphHeight int) string {
	base := renderMemoryPanel(mem, width)
	if history.Len() < 2 {
		return base
	}
	pct := 0.0
	if mem.Total > 0 {
		pct = float64(mem.Used) / float64(mem.Total) * 100
	}
	label := fmt.Sprintf("  MEM %3.0f%%", pct)
	gw := width - 2
	return appendSparkline(base, label, history.Values(), gw, graphHeight, SparklineOpts{
		Min: 0, Max: 100, Color: sparkMemColor,
	})
}

func renderNetworkPanelWithGraph(ifaces []metrics.NetworkInterface, histIn, histOut *RingBuffer, width int) string {
	base := renderNetworkPanel(ifaces, width)

	gw := width - 2
	if gw < minGraphWidth {
		return base
	}

	// Use reduced height for network graphs to keep the panel compact.
	netHeight := 2

	if histIn.Len() >= 2 {
		inData := histIn.Values()
		maxIn := autoScaleMaxWithFloor(inData, 1024)
		label := fmt.Sprintf("  In  %s", formatBytesRate(inData[len(inData)-1]))
		graph := RenderSparkline(inData, SparklineOpts{
			Width: gw, Height: netHeight, Min: 0, Max: maxIn, Color: sparkNetInColor,
		})
		if graph != "" {
			base += "\n" + graphLabelStyle.Render(label) + "\n" + graph
		}
	}
	if histOut.Len() >= 2 {
		outData := histOut.Values()
		maxOut := autoScaleMaxWithFloor(outData, 1024)
		label := fmt.Sprintf("  Out %s", formatBytesRate(outData[len(outData)-1]))
		graph := RenderSparkline(outData, SparklineOpts{
			Width: gw, Height: netHeight, Min: 0, Max: maxOut, Color: sparkNetOutColor,
		})
		if graph != "" {
			base += "\n" + graphLabelStyle.Render(label) + "\n" + graph
		}
	}
	return base
}

func renderTemperaturePanelWithGraph(temp metrics.TemperatureMetrics, history *RingBuffer, width, graphHeight int) string {
	base := renderTemperaturePanel(temp)
	if history.Len() < 2 {
		return base
	}
	data := history.Values()
	scaleMax := autoScaleMaxWithFloor(data, 30.0)
	label := fmt.Sprintf("  Temp %.1f\u00b0C", data[len(data)-1])
	gw := width - 2
	return appendSparkline(base, label, data, gw, graphHeight, SparklineOpts{
		Min: 0, Max: scaleMax, Color: sparkTempColor,
	})
}

// autoScaleMaxWithFloor computes max(data) * 1.2 with the given floor.
func autoScaleMaxWithFloor(data []float64, floor float64) float64 {
	mx := 0.0
	for _, v := range data {
		if v > mx {
			mx = v
		}
	}
	mx *= 1.2
	if mx < floor {
		mx = floor
	}
	return mx
}

// formatBytesRate formats a bytes/s value as a compact human-readable string.
func formatBytesRate(bps float64) string {
	const (
		KB = 1024.0
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.1fG/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.1fM/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.1fK/s", bps/KB)
	default:
		return fmt.Sprintf("%.0fB/s", bps)
	}
}
