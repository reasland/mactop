package ui

import (
	"math"
	"strings"
	"testing"
)

func TestRenderSparkline_EmptyData(t *testing.T) {
	result := RenderSparkline(nil, SparklineOpts{Width: 10, Height: 4, Min: 0, Max: 100})
	if result != "" {
		t.Errorf("empty data should produce empty string, got %q", result)
	}
}

func TestRenderSparkline_ZeroWidth(t *testing.T) {
	result := RenderSparkline([]float64{50}, SparklineOpts{Width: 0, Height: 4, Min: 0, Max: 100})
	if result != "" {
		t.Errorf("zero width should produce empty string, got %q", result)
	}
}

func TestRenderSparkline_ZeroHeight(t *testing.T) {
	result := RenderSparkline([]float64{50}, SparklineOpts{Width: 10, Height: 0, Min: 0, Max: 100})
	if result != "" {
		t.Errorf("zero height should produce empty string, got %q", result)
	}
}

func TestRenderSparkline_SinglePoint(t *testing.T) {
	result := RenderSparkline([]float64{50}, SparklineOpts{Width: 5, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("single point should produce non-empty output")
	}
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
}

func TestRenderSparkline_RowCount(t *testing.T) {
	data := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	result := RenderSparkline(data, SparklineOpts{Width: 5, Height: 4, Min: 0, Max: 100})
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Errorf("expected 4 rows, got %d", len(lines))
	}
}

func TestRenderSparkline_AllZeros(t *testing.T) {
	data := make([]float64, 20)
	result := RenderSparkline(data, SparklineOpts{Width: 10, Height: 3, Min: 0, Max: 100})
	if result == "" {
		t.Error("all-zero data should still produce output")
	}
	// All dots should be at the bottom row.
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 rows, got %d", len(lines))
	}
}

func TestRenderSparkline_AllMax(t *testing.T) {
	data := []float64{100, 100, 100, 100}
	result := RenderSparkline(data, SparklineOpts{Width: 2, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("all-max data should produce non-empty output")
	}
}

func TestRenderSparkline_ContainsBrailleChars(t *testing.T) {
	data := []float64{10, 50, 90, 30}
	result := RenderSparkline(data, SparklineOpts{Width: 2, Height: 2, Min: 0, Max: 100})
	// Should contain braille characters in the range U+2800-U+28FF.
	hasBraille := false
	for _, r := range result {
		if r >= 0x2800 && r <= 0x28FF {
			hasBraille = true
			break
		}
	}
	if !hasBraille {
		t.Error("output should contain braille characters")
	}
}

func TestRenderSparkline_TruncatesExcessData(t *testing.T) {
	// Width=3 means 6 samples max. Provide 10.
	data := make([]float64, 10)
	for i := range data {
		data[i] = float64(i * 10)
	}
	result := RenderSparkline(data, SparklineOpts{Width: 3, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("should produce output even with excess data")
	}
}

func TestRenderSparkline_EqualMinMax(t *testing.T) {
	// Min == Max should not panic (division by zero guard).
	data := []float64{50, 50, 50}
	result := RenderSparkline(data, SparklineOpts{Width: 2, Height: 2, Min: 50, Max: 50})
	if result == "" {
		t.Error("equal min/max should still produce output")
	}
}

func TestRenderSparkline_NaNValuesSkipped(t *testing.T) {
	data := []float64{10, math.NaN(), 30, math.NaN()}
	result := RenderSparkline(data, SparklineOpts{Width: 4, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("data with NaN values should still produce output for valid points")
	}
	// Should have the expected number of rows.
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 rows, got %d", len(lines))
	}
}

func TestRenderSparkline_AllNaN(t *testing.T) {
	data := []float64{math.NaN(), math.NaN(), math.NaN()}
	result := RenderSparkline(data, SparklineOpts{Width: 3, Height: 2, Min: 0, Max: 100})
	// All points are NaN so all dotYs are -1; the grid should be all-zero braille (U+2800).
	// The renderer still produces output (empty braille cells).
	if result == "" {
		t.Error("all-NaN data should still produce output (blank braille cells)")
	}
}

func TestRenderSparkline_InfValuesSkipped(t *testing.T) {
	data := []float64{20, math.Inf(1), 50, math.Inf(-1)}
	result := RenderSparkline(data, SparklineOpts{Width: 4, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("data with Inf values should still produce output for valid points")
	}
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 rows, got %d", len(lines))
	}
}

func TestRenderSparkline_AllSameValues(t *testing.T) {
	data := []float64{42, 42, 42, 42, 42, 42}
	result := RenderSparkline(data, SparklineOpts{Width: 3, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("all same values should produce output")
	}
	// All points should be at the same vertical position; verify rows exist.
	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 rows, got %d", len(lines))
	}
}

func TestRenderSparkline_AllSameValuesEqualMinMax(t *testing.T) {
	// When all values are the same AND Min==Max (auto-scaled scenario).
	data := []float64{75, 75, 75}
	result := RenderSparkline(data, SparklineOpts{Width: 2, Height: 2, Min: 75, Max: 75})
	if result == "" {
		t.Error("all same values with Min==Max should produce output")
	}
}

func TestRenderSparkline_SingleDataPoint(t *testing.T) {
	result := RenderSparkline([]float64{80}, SparklineOpts{Width: 5, Height: 3, Min: 0, Max: 100})
	if result == "" {
		t.Error("single data point should produce non-empty output")
	}
	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 rows, got %d", len(lines))
	}
	// Verify at least one non-blank braille character exists.
	hasNonBlank := false
	for _, r := range result {
		if r > 0x2800 && r <= 0x28FF {
			hasNonBlank = true
			break
		}
	}
	if !hasNonBlank {
		t.Error("single data point should produce at least one non-blank braille character")
	}
}

func TestRenderSparkline_OnlyValidBrailleCharacters(t *testing.T) {
	data := []float64{0, 25, 50, 75, 100, 80, 60, 40, 20, 10}
	result := RenderSparkline(data, SparklineOpts{Width: 5, Height: 3, Min: 0, Max: 100})
	for _, r := range result {
		if r == '\n' {
			continue
		}
		// The renderer uses lipgloss which may add ANSI escape sequences.
		// Only check runes that are in the braille block or printable ASCII (ANSI).
		if r >= 0x2800 && r <= 0x28FF {
			// Valid braille character.
			continue
		}
		if r < 0x2800 {
			// ASCII / ANSI escape characters are OK.
			continue
		}
		// Anything else above 0x28FF and not braille is unexpected.
		if r > 0x28FF {
			t.Errorf("unexpected non-braille character: U+%04X", r)
		}
	}
}

func TestRenderSparkline_NegativeWidth(t *testing.T) {
	result := RenderSparkline([]float64{50}, SparklineOpts{Width: -1, Height: 2, Min: 0, Max: 100})
	if result != "" {
		t.Errorf("negative width should produce empty string, got %q", result)
	}
}

func TestRenderSparkline_NegativeHeight(t *testing.T) {
	result := RenderSparkline([]float64{50}, SparklineOpts{Width: 5, Height: -1, Min: 0, Max: 100})
	if result != "" {
		t.Errorf("negative height should produce empty string, got %q", result)
	}
}

func TestRenderSparkline_ValuesOutsideRange(t *testing.T) {
	// Values outside [Min, Max] should be clamped, not panic.
	data := []float64{-50, 200, 50}
	result := RenderSparkline(data, SparklineOpts{Width: 3, Height: 2, Min: 0, Max: 100})
	if result == "" {
		t.Error("values outside range should be clamped and produce output")
	}
}
