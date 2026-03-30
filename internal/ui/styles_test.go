package ui

import (
	"strings"
	"testing"
)

// countRunes returns the number of '|' and '-' runes in a string,
// ignoring any ANSI escape sequences from lipgloss styling.
func countRune(s string, r rune) int {
	count := 0
	for _, c := range s {
		if c == r {
			count++
		}
	}
	return count
}

func TestProgressBar_ZeroPercent(t *testing.T) {
	bar := progressBar(20, 0)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled != 0 {
		t.Errorf("expected 0 filled segments at 0%%, got %d", filled)
	}
	if empty != 20 {
		t.Errorf("expected 20 empty segments at 0%%, got %d", empty)
	}
}

func TestProgressBar_FullPercent(t *testing.T) {
	bar := progressBar(20, 100)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled != 20 {
		t.Errorf("expected 20 filled segments at 100%%, got %d", filled)
	}
	if empty != 0 {
		t.Errorf("expected 0 empty segments at 100%%, got %d", empty)
	}
}

func TestProgressBar_HalfPercent(t *testing.T) {
	bar := progressBar(20, 50)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled != 10 {
		t.Errorf("expected 10 filled segments at 50%%, got %d", filled)
	}
	if empty != 10 {
		t.Errorf("expected 10 empty segments at 50%%, got %d", empty)
	}
}

func TestProgressBar_NegativePercent(t *testing.T) {
	bar := progressBar(20, -10)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled != 0 {
		t.Errorf("negative percentage should produce 0 filled segments, got %d", filled)
	}
	if empty != 20 {
		t.Errorf("negative percentage should produce 20 empty segments, got %d", empty)
	}
}

func TestProgressBar_OverHundredPercent(t *testing.T) {
	bar := progressBar(20, 150)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled != 20 {
		t.Errorf("percentage over 100 should cap at width (20), got %d filled", filled)
	}
	if empty != 0 {
		t.Errorf("percentage over 100 should have 0 empty segments, got %d", empty)
	}
}

func TestProgressBar_VerySmallWidth(t *testing.T) {
	// Width < 3 is clamped to 3.
	bar := progressBar(1, 50)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	total := filled + empty
	if total != 3 {
		t.Errorf("width < 3 should be clamped to 3, got total segments = %d", total)
	}
}

func TestProgressBar_WarnColorAt90Percent(t *testing.T) {
	// At 90%+, the bar should still render correctly (filled + empty = width).
	bar := progressBar(20, 95)

	filled := countRune(bar, '|')
	empty := countRune(bar, '-')

	if filled+empty != 20 {
		t.Errorf("total segments should equal width (20), got %d", filled+empty)
	}
	if filled != 19 {
		t.Errorf("expected 19 filled segments at 95%%, got %d", filled)
	}
}

func TestProgressBar_TotalSegmentsEqualWidth(t *testing.T) {
	// Test several percentages to make sure total segments always equals width.
	widths := []int{5, 10, 20, 50, 80}
	pcts := []float64{0, 1, 25, 33.3, 50, 66.7, 75, 99, 100}

	for _, w := range widths {
		for _, p := range pcts {
			bar := progressBar(w, p)
			filled := countRune(bar, '|')
			empty := countRune(bar, '-')
			total := filled + empty
			if total != w {
				t.Errorf("progressBar(%d, %.1f): total segments = %d, want %d", w, p, total, w)
			}
		}
	}
}

func TestFormatBytes_Zero(t *testing.T) {
	result := formatBytes(0)
	if result != "0 B" {
		t.Errorf("formatBytes(0) = %q, want %q", result, "0 B")
	}
}

func TestFormatBytes_SubKilobyte(t *testing.T) {
	result := formatBytes(1023)
	if result != "1023 B" {
		t.Errorf("formatBytes(1023) = %q, want %q", result, "1023 B")
	}
}

func TestFormatBytes_ExactlyOneKB(t *testing.T) {
	result := formatBytes(1024)
	if result != "1.0 KB" {
		t.Errorf("formatBytes(1024) = %q, want %q", result, "1.0 KB")
	}
}

func TestFormatBytes_ExactlyOneMB(t *testing.T) {
	result := formatBytes(1024 * 1024)
	if result != "1.0 MB" {
		t.Errorf("formatBytes(1048576) = %q, want %q", result, "1.0 MB")
	}
}

func TestFormatBytes_ExactlyOneGB(t *testing.T) {
	result := formatBytes(1024 * 1024 * 1024)
	if result != "1.0 GB" {
		t.Errorf("formatBytes(1073741824) = %q, want %q", result, "1.0 GB")
	}
}

func TestFormatBytes_ExactlyOneTB(t *testing.T) {
	result := formatBytes(1024 * 1024 * 1024 * 1024)
	if result != "1.0 TB" {
		t.Errorf("formatBytes(1099511627776) = %q, want %q", result, "1.0 TB")
	}
}

func TestFormatBytes_LargeValue(t *testing.T) {
	// 16 GB
	val := uint64(16) * 1024 * 1024 * 1024
	result := formatBytes(val)
	if result != "16.0 GB" {
		t.Errorf("formatBytes(16 GB) = %q, want %q", result, "16.0 GB")
	}
}

func TestFormatBytes_FractionalKB(t *testing.T) {
	// 1.5 KB = 1536 bytes
	result := formatBytes(1536)
	if result != "1.5 KB" {
		t.Errorf("formatBytes(1536) = %q, want %q", result, "1.5 KB")
	}
}

func TestFormatBytes_FractionalGB(t *testing.T) {
	// 2.5 GB
	val := uint64(2.5 * 1024 * 1024 * 1024)
	result := formatBytes(val)
	if !strings.HasPrefix(result, "2.5 GB") {
		t.Errorf("formatBytes(2.5 GB) = %q, want prefix %q", result, "2.5 GB")
	}
}

func TestFormatBytes_MaxUint64(t *testing.T) {
	// Ensure no panic on max uint64.
	result := formatBytes(^uint64(0))
	if result == "" {
		t.Error("formatBytes(maxUint64) should not return empty string")
	}
	if !strings.Contains(result, "TB") {
		t.Errorf("formatBytes(maxUint64) = %q, expected TB unit", result)
	}
}
