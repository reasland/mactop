package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Braille dot bit positions within a 2x4 cell.
// Column 0 (left): rows 0-3 map to bits 0x01, 0x02, 0x04, 0x40
// Column 1 (right): rows 0-3 map to bits 0x08, 0x10, 0x20, 0x80
var brailleBits = [4][2]uint8{
	{0x01, 0x08}, // row 0 (top)
	{0x02, 0x10}, // row 1
	{0x04, 0x20}, // row 2
	{0x40, 0x80}, // row 3 (bottom)
}

// SparklineOpts controls sparkline rendering.
type SparklineOpts struct {
	Width  int                     // number of terminal columns for the graph
	Height int                     // number of terminal rows (each row = 4 braille dots)
	Min    float64                 // y-axis minimum (use 0 for percentages)
	Max    float64                 // y-axis maximum (use 100 for percentages)
	Color  lipgloss.TerminalColor // line color
}

// RenderSparkline renders a braille-dot sparkline graph from the given data.
// If len(data) > Width*2, only the rightmost Width*2 points are used.
// If len(data) < Width*2, the graph is right-aligned (left side is blank).
func RenderSparkline(data []float64, opts SparklineOpts) string {
	if opts.Width <= 0 || opts.Height <= 0 || len(data) == 0 {
		return ""
	}

	samples := opts.Width * 2
	totalDots := opts.Height * 4

	// Take the rightmost samples.
	if len(data) > samples {
		data = data[len(data)-samples:]
	}

	// Prevent division by zero.
	rng := opts.Max - opts.Min
	if rng <= 0 {
		rng = 1
	}

	// Scale each value to a dot-row index (0 = bottom, totalDots-1 = top).
	dotYs := make([]int, len(data))
	for i, v := range data {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			dotYs[i] = -1 // skip
			continue
		}
		dy := int((v - opts.Min) / rng * float64(totalDots-1))
		if dy < 0 {
			dy = 0
		}
		if dy >= totalDots {
			dy = totalDots - 1
		}
		dotYs[i] = dy
	}

	// Build flat grid: grid[row*width+col] holds the braille bitmask for that cell.
	// Row 0 is the top of the graph, row Height-1 is the bottom.
	grid := make([]uint8, opts.Height*opts.Width)

	// Offset for right-alignment: data points start this many dot-columns from the left.
	offset := samples - len(data)

	for i, dy := range dotYs {
		if dy < 0 {
			continue
		}
		// Convert dot-Y (0=bottom) to grid row/dot-within-row.
		// The top of the graph is dotY = totalDots-1.
		flippedY := (totalDots - 1) - dy
		row := flippedY / 4
		dotRow := flippedY % 4

		col := (i + offset) / 2
		dotCol := (i + offset) % 2

		if row >= 0 && row < opts.Height && col >= 0 && col < opts.Width {
			grid[row*opts.Width+col] |= brailleBits[dotRow][dotCol]
		}
	}

	// Also draw vertical connections between consecutive points to avoid gaps.
	for i := 1; i < len(dotYs); i++ {
		if dotYs[i-1] < 0 || dotYs[i] < 0 {
			continue
		}
		y0, y1 := dotYs[i-1], dotYs[i]
		if y0 > y1 {
			y0, y1 = y1, y0
		}
		for y := y0; y <= y1; y++ {
			flippedY := (totalDots - 1) - y
			row := flippedY / 4
			dotRow := flippedY % 4

			// Use the dot-column of the current data point.
			col := (i + offset) / 2
			dotCol := (i + offset) % 2

			if row >= 0 && row < opts.Height && col >= 0 && col < opts.Width {
				grid[row*opts.Width+col] |= brailleBits[dotRow][dotCol]
			}
		}
	}

	// Render grid to string.
	style := sparklineStyle.Foreground(opts.Color)
	var sb strings.Builder
	for r := 0; r < opts.Height; r++ {
		if r > 0 {
			sb.WriteByte('\n')
		}
		var line strings.Builder
		rowStart := r * opts.Width
		for c := 0; c < opts.Width; c++ {
			line.WriteRune(rune(0x2800 + int(grid[rowStart+c])))
		}
		sb.WriteString(style.Render(line.String()))
	}

	return sb.String()
}
