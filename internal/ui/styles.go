package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Panel border style.
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.AdaptiveColor{Light: "240", Dark: "60"}).
			Padding(0, 1)

	// Title bar style.
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"}).
			Align(lipgloss.Center)

	// Panel heading style.
	headingStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "117"})

	// Sub-heading style (e.g., "P-Cores", "E-Cores").
	subHeadingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})

	// Label style for metric names.
	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "236", Dark: "252"})

	// Value style for metric values.
	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "40"})

	// Warning style (high usage).
	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "196", Dark: "203"})

	// Dim style for N/A or secondary info.
	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"})

	// Status bar style.
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})

	// Progress bar colors and base style.
	barFilledColor = lipgloss.AdaptiveColor{Light: "40", Dark: "40"}
	barEmptyColor  = lipgloss.AdaptiveColor{Light: "252", Dark: "237"}
	barWarnColor   = lipgloss.AdaptiveColor{Light: "196", Dark: "203"}
	barBaseStyle   = lipgloss.NewStyle()

	// Sparkline colors per metric.
	sparkCPUColor    = lipgloss.AdaptiveColor{Light: "33", Dark: "39"}
	sparkGPUColor    = lipgloss.AdaptiveColor{Light: "129", Dark: "171"}
	sparkMemColor    = lipgloss.AdaptiveColor{Light: "178", Dark: "220"}
	sparkNetInColor  = lipgloss.AdaptiveColor{Light: "28", Dark: "40"}
	sparkNetOutColor = lipgloss.AdaptiveColor{Light: "166", Dark: "208"}
	sparkTempColor   = lipgloss.AdaptiveColor{Light: "160", Dark: "196"}

	// Base style for sparkline rendering (foreground color set per call).
	sparklineStyle = lipgloss.NewStyle()

	// Label style for graph labels (reuses dimStyle foreground).
	graphLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"})
)

// progressBar renders a text-based progress bar.
func progressBar(width int, pct float64) string {
	if width < 3 {
		width = 3
	}

	filled := int(pct / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	filledColor := barFilledColor
	if pct >= 90 {
		filledColor = barWarnColor
	}

	filledStr := barBaseStyle.Foreground(filledColor).Render(
		strings.Repeat("|", filled),
	)
	emptyStr := barBaseStyle.Foreground(barEmptyColor).Render(
		strings.Repeat("-", width-filled),
	)

	return filledStr + emptyStr
}

