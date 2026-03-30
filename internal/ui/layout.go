package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyeasland/mactop/internal/metrics"
)

const wideThreshold = 100

// renderDashboard produces the full dashboard string for the given terminal size.
func renderDashboard(m metrics.SystemMetrics, width, height int, version string, intervalStr string,
	history *MetricHistory, showGraphs bool) string {
	// Title bar.
	title := titleStyle.Width(width - 2).Render("mactop " + version)

	// Status bar.
	status := statusStyle.Width(width - 2).Render(
		"  q: quit   r: reset peaks   g: graphs   ?: help   Refresh: " + intervalStr)

	if width >= wideThreshold {
		return renderWideLayout(m, width, title, status, history, showGraphs)
	}
	return renderNarrowLayout(m, width, title, status, history, showGraphs)
}

func renderWideLayout(m metrics.SystemMetrics, width int, title, status string,
	history *MetricHistory, showGraphs bool) string {
	leftWidth := width/2 - 2
	rightWidth := width - leftWidth - 4

	const wideGraphHeight = 4

	// Left column: CPU, GPU, Network.
	var cpuContent string
	if showGraphs {
		cpuContent = renderCPUPanelWithGraph(m.CPU, history.Get(MetricCPU), leftWidth, wideGraphHeight)
	} else {
		cpuContent = renderCPUPanel(m.CPU, leftWidth)
	}
	cpuPanel := panelStyle.Width(leftWidth).Render(cpuContent)

	var gpuContent string
	if showGraphs {
		gpuContent = renderGPUPanelWithGraph(m.GPU, history.Get(MetricGPU), leftWidth, wideGraphHeight)
	} else {
		gpuContent = renderGPUPanel(m.GPU, leftWidth)
	}
	gpuPanel := panelStyle.Width(leftWidth).Render(gpuContent)

	var netContent string
	if showGraphs {
		netContent = renderNetworkPanelWithGraph(m.Network, history.Get(MetricNetIn), history.Get(MetricNetOut), leftWidth)
	} else {
		netContent = renderNetworkPanel(m.Network, leftWidth)
	}
	netPanel := panelStyle.Width(leftWidth).Render(netContent)

	leftCol := lipgloss.JoinVertical(lipgloss.Left, cpuPanel, gpuPanel, netPanel)

	// Right column: Memory, Temperature, Power.
	var memContent string
	if showGraphs {
		memContent = renderMemoryPanelWithGraph(m.Memory, history.Get(MetricMemory), rightWidth, wideGraphHeight)
	} else {
		memContent = renderMemoryPanel(m.Memory, rightWidth)
	}
	memPanel := panelStyle.Width(rightWidth).Render(memContent)

	var tempContent string
	if showGraphs {
		tempContent = renderTemperaturePanelWithGraph(m.Temperature, history.Get(MetricTemp), rightWidth, wideGraphHeight)
	} else {
		tempContent = renderTemperaturePanel(m.Temperature)
	}
	tempPanel := panelStyle.Width(rightWidth).Render(tempContent)

	powerContent := renderPowerPanel(m.Power)
	powerPanel := panelStyle.Width(rightWidth).Render(powerContent)

	rightCol := lipgloss.JoinVertical(lipgloss.Left, memPanel, tempPanel, powerPanel)

	// Join columns.
	columns := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, rightCol)

	// Disk spans full width.
	diskContent := renderDiskPanel(m.Disk, width-4)
	diskPanel := panelStyle.Width(width - 2).Render(diskContent)

	return lipgloss.JoinVertical(lipgloss.Left, title, columns, diskPanel, status)
}

func renderNarrowLayout(m metrics.SystemMetrics, width int, title, status string,
	history *MetricHistory, showGraphs bool) string {
	pw := width - 4
	if pw < 10 {
		pw = 10
	}

	const narrowGraphHeight = 3

	var cpuContent, gpuContent, memContent, netContent string
	if showGraphs {
		cpuContent = renderCPUPanelWithGraph(m.CPU, history.Get(MetricCPU), pw, narrowGraphHeight)
		gpuContent = renderGPUPanelWithGraph(m.GPU, history.Get(MetricGPU), pw, narrowGraphHeight)
		memContent = renderMemoryPanelWithGraph(m.Memory, history.Get(MetricMemory), pw, narrowGraphHeight)
		netContent = renderNetworkPanelWithGraph(m.Network, history.Get(MetricNetIn), history.Get(MetricNetOut), pw)
	} else {
		cpuContent = renderCPUPanel(m.CPU, pw)
		gpuContent = renderGPUPanel(m.GPU, pw)
		memContent = renderMemoryPanel(m.Memory, pw)
		netContent = renderNetworkPanel(m.Network, pw)
	}

	cpuPanel := panelStyle.Width(pw).Render(cpuContent)
	gpuPanel := panelStyle.Width(pw).Render(gpuContent)
	memPanel := panelStyle.Width(pw).Render(memContent)
	netPanel := panelStyle.Width(pw).Render(netContent)
	diskPanel := panelStyle.Width(pw).Render(renderDiskPanel(m.Disk, pw))

	var tempContent string
	if showGraphs {
		tempContent = renderTemperaturePanelWithGraph(m.Temperature, history.Get(MetricTemp), pw, narrowGraphHeight)
	} else {
		tempContent = renderTemperaturePanel(m.Temperature)
	}
	tempPanel := panelStyle.Width(pw).Render(tempContent)
	powerPanel := panelStyle.Width(pw).Render(renderPowerPanel(m.Power))

	return lipgloss.JoinVertical(lipgloss.Left,
		title, cpuPanel, gpuPanel, memPanel, netPanel, diskPanel, tempPanel, powerPanel, status)
}
