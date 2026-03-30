package ui

import (
	"testing"

	"github.com/rileyeasland/mactop/internal/metrics"
)

func emptyMetrics() metrics.SystemMetrics {
	return metrics.SystemMetrics{}
}

func TestRenderDashboard_WideLayout(t *testing.T) {
	// Width >= 100 should use the wide (two-column) layout.
	// We verify it does not panic and returns non-empty output.
	result := renderDashboard(emptyMetrics(), 120, 40, "v0.1.0", "1s", NewMetricHistory(historyCapacity), false)
	if result == "" {
		t.Error("renderDashboard with wide terminal should produce non-empty output")
	}
}

func TestRenderDashboard_NarrowLayout(t *testing.T) {
	// Width < 100 should use the narrow (single-column) layout.
	result := renderDashboard(emptyMetrics(), 80, 24, "v0.1.0", "1s", NewMetricHistory(historyCapacity), false)
	if result == "" {
		t.Error("renderDashboard with narrow terminal should produce non-empty output")
	}
}

func TestRenderDashboard_VeryNarrowLayout(t *testing.T) {
	// Very narrow terminal (e.g. 20 cols) should not panic.
	result := renderDashboard(emptyMetrics(), 20, 10, "v0.1.0", "1s", NewMetricHistory(historyCapacity), false)
	if result == "" {
		t.Error("renderDashboard with very narrow terminal should produce non-empty output")
	}
}

func TestRenderDashboard_ExactThreshold(t *testing.T) {
	// Width == 100 should use wide layout (>= threshold).
	result := renderDashboard(emptyMetrics(), 100, 30, "v0.1.0", "1s", NewMetricHistory(historyCapacity), false)
	if result == "" {
		t.Error("renderDashboard at exact threshold should produce non-empty output")
	}
}

func TestRenderDashboard_JustBelowThreshold(t *testing.T) {
	// Width == 99 should use narrow layout (< threshold).
	result := renderDashboard(emptyMetrics(), 99, 30, "v0.1.0", "1s", NewMetricHistory(historyCapacity), false)
	if result == "" {
		t.Error("renderDashboard just below threshold should produce non-empty output")
	}
}

func TestRenderDashboard_ContainsVersion(t *testing.T) {
	result := renderDashboard(emptyMetrics(), 120, 40, "v1.2.3", "500ms", NewMetricHistory(historyCapacity), false)
	if len(result) == 0 {
		t.Fatal("expected non-empty output")
	}
	// The title should contain the version string.
	// Note: lipgloss may add ANSI escapes, so we check the raw rune content.
	found := false
	for i := 0; i+len("v1.2.3") <= len(result); i++ {
		if result[i:i+len("v1.2.3")] == "v1.2.3" {
			found = true
			break
		}
	}
	if !found {
		t.Error("rendered dashboard should contain the version string")
	}
}

func TestWideThresholdConstant(t *testing.T) {
	if wideThreshold != 100 {
		t.Errorf("wideThreshold = %d, want 100", wideThreshold)
	}
}

// populatedHistory returns a MetricHistory with enough samples to trigger graph rendering.
func populatedHistory() *MetricHistory {
	h := NewMetricHistory(historyCapacity)
	for i := 0; i < 20; i++ {
		h.Record(metrics.SystemMetrics{
			CPU:    metrics.CPUMetrics{Aggregate: float64(20 + i*3)},
			GPU:    metrics.GPUMetrics{Utilization: float64(10 + i*2), Available: true},
			Memory: metrics.MemoryMetrics{Total: 16000, Used: uint64(8000 + i*100)},
			Network: []metrics.NetworkInterface{
				{BytesInPS: float64(1024 * (i + 1)), BytesOutPS: float64(512 * (i + 1))},
			},
		})
	}
	return h
}

func TestRenderDashboard_ShowGraphsWide(t *testing.T) {
	// Exercise the graph rendering path with showGraphs=true on a wide layout.
	result := renderDashboard(
		metrics.SystemMetrics{
			CPU:    metrics.CPUMetrics{Aggregate: 45.0},
			GPU:    metrics.GPUMetrics{Utilization: 30.0, Available: true},
			Memory: metrics.MemoryMetrics{Total: 16000, Used: 8000},
		},
		120, 60, "v0.1.0", "1s", populatedHistory(), true)
	if result == "" {
		t.Error("renderDashboard with showGraphs=true (wide) should produce non-empty output")
	}
}

func TestRenderDashboard_ShowGraphsNarrow(t *testing.T) {
	// Exercise the graph rendering path with showGraphs=true on a narrow layout.
	result := renderDashboard(
		metrics.SystemMetrics{
			CPU:    metrics.CPUMetrics{Aggregate: 45.0},
			GPU:    metrics.GPUMetrics{Utilization: 30.0, Available: true},
			Memory: metrics.MemoryMetrics{Total: 16000, Used: 8000},
		},
		80, 40, "v0.1.0", "1s", populatedHistory(), true)
	if result == "" {
		t.Error("renderDashboard with showGraphs=true (narrow) should produce non-empty output")
	}
}
