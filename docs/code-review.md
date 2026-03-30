# Code Review: mactop Full Codebase

**Date:** 2026-03-27
**Reviewer:** Claude Opus 4.6
**Scope:** All source files in the mactop project -- CGo bindings, collectors, SMC, UI, build system
**Verdict:** Approve with changes

---

## Summary

mactop is a well-structured macOS system monitor built on CGo, IOKit, Mach APIs, and the SMC. The architecture is clean: platform bindings are isolated from collection logic, which is isolated from the UI. The code is readable and follows Go conventions reasonably well. The CGo bindings are generally correct with proper memory management. However, there are several issues ranging from a Mach port leak on every call to some subtle correctness bugs in delta computations.

---

## CRITICAL (Must Fix)

### 1. Mach host port leaked on every call to `HostProcessorInfo` and `HostVMInfo64`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/platform/mach.go`, lines 34-35 and 85-86

`mach_host_self()` returns a send right to the host port. On macOS, each call to `mach_host_self()` increments the reference count on the port, and the caller is responsible for deallocating it with `mach_port_deallocate()`. Since `HostProcessorInfo()` and `HostVMInfo64()` are called every tick (default 1 second), this leaks a Mach port send right on every tick, eventually exhausting the process's port space.

**Suggested fix:** Cache the host port once at init time, or deallocate it after each use:
```go
host := C.mach_host_self()
defer C.mach_port_deallocate(C.get_mach_task_self(), host)
```

### 2. Network delta computed before wraparound check -- delta uses underflowed value

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/network.go`, lines 141-153

The code computes `dIn := bytesIn - prev.bytesIn` on line 141 as a `uint64` subtraction. If `bytesIn < prev.bytesIn` (counter wraparound), this silently wraps to a huge positive number. The wraparound check on lines 145-149 then sets `dIn = 0`, but only after the wrapped subtraction was already computed. Because these are `uint64` values, the subtraction does not panic -- it wraps. However, the logic is fragile and confusing. The same issue exists in `disk.go` lines 72-80.

While this technically works in Go (unsigned subtraction wraps), the code's intent would be clearer and safer if the comparison came first:
```go
var dIn uint64
if bytesIn >= prev.bytesIn {
    dIn = bytesIn - prev.bytesIn
}
```

This is marked CRITICAL because if the check order were ever rearranged or the types changed to signed, the bug would silently produce massive throughput spikes.

---

## MAJOR (Should Fix)

### 3. CPU tick delta uses uint32 subtraction which will silently wrap on overflow

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/cpu.go`, lines 64-67

`CPUTicks` fields are `uint32`. The delta computation `float64(ticks[i].User - prev.user)` performs `uint32` subtraction. If the tick counters wrap around (which they will after ~49 days at 100 ticks/sec), the unsigned subtraction wraps to a large positive number, producing a massive spike in the computed delta. There is no wraparound guard here, unlike the network and disk collectors.

**Suggested fix:** Either detect wraparound (if `current < prev`, set delta to 0 or skip the sample), or promote to `uint64` to delay the issue.

### 4. `collectAll` called via pointer receiver but `Update` uses value receiver

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/app.go`, lines 59 and 87

The `Update` method has a value receiver (`func (m Model) Update(...)`), and on line 80 it calls `m.collectAll()` which has a pointer receiver (`func (m *Model) collectAll()`). Go automatically takes the address of the value receiver to call the pointer method, so this works -- but it means `collectAll` modifies a **copy** of `m`, not the original. The modified `m` is then returned from `Update` on line 81, so the mutation is preserved. This is correct but fragile; if anyone adds another pointer-receiver call that expects to persist state without returning `m`, it will silently fail. Consider making `Update` a pointer receiver or documenting this pattern clearly.

### 5. SMC connection is never closed on normal exit paths

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/app.go`, lines 63-65
**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/temperature.go`, lines 59-65

`temp.Close()` is only called on `q` or `ctrl+c` key events. If the program exits via any other path (e.g., the terminal is closed, a signal is received, or `tea.Quit` is returned from some other condition), the SMC connection is leaked. The IOKit connection handle will be released when the process exits, so this is not catastrophic, but it is not clean resource management.

**Suggested fix:** Use a finalizer, `defer` in `main()`, or handle cleanup in a `tea.QuitMsg` handler.

### 6. `verbose` flag is accepted but does nothing

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/cmd/mactop/main.go`, line 20
**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/app.go`, lines 93-98

The `--v` flag is parsed and stored in the model, but the verbose branch on lines 94-98 just does `_ = err`. No logging is configured. This is misleading to users who pass `--v` expecting debug output.

**Suggested fix:** Either implement logging (e.g., `tea.LogToFile`) or remove the flag until it is functional.

### 7. Makefile hardcodes `GOARCH=arm64`, preventing Intel Mac builds

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/Makefile`, line 7

The build target hardcodes `GOARCH=arm64`. This means the project cannot be built on or for Intel Macs without manual override.

**Suggested fix:** Remove the `GOARCH` override to let Go auto-detect, or use a conditional:
```makefile
GOARCH ?= $(shell go env GOARCH)
```

### 8. `lipgloss.SetColorProfile(0)` uses a magic number

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/cmd/mactop/main.go`, line 35

The value `0` is not self-documenting. The lipgloss library defines `termenv.Ascii` for this purpose.

**Suggested fix:** Use `lipgloss.SetColorProfile(termenv.Ascii)` or at minimum add a comment explaining that 0 = no color.

---

## MINOR (Nice to Fix)

### 9. `GetSwapUsage` struct layout comment is incorrect

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/platform/sysctl.go`, lines 80-81

The comment says the struct layout is `total (8), avail (8), used (8)`, but the code reads `xsw[0]` as Total, `xsw[1]` as Avail, `xsw[2]` as Used. The actual `struct xsw_usage` on macOS is defined as `{ xsu_total, xsu_avail, xsu_used }`, so the code is correct but the comment could be misread. This is fine as-is but worth double-checking against the macOS headers if swap values ever look wrong.

### 10. GPU collector overwrites data on each IOAccelerator match

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/gpu.go`, lines 29-41

`IOKitIterateMatching("IOAccelerator", ...)` may match multiple IOAccelerator entries (e.g., on Macs with both integrated and discrete GPUs, or on Apple Silicon where there may be multiple accelerator entries). The callback overwrites `c.Data` fields each time, so only the last matching entry's values are kept. This happens to work on current Apple Silicon (one GPU) but would produce confusing results on multi-GPU Macs.

**Suggested fix:** Either break after the first match, or aggregate across GPUs.

### 11. Temperature panel uses `C` instead of degree symbol

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, line 234

The temperature is rendered as `"%.1f C"` rather than `"%.1f \u00b0C"`. This is a cosmetic issue but the degree symbol would look more polished.

### 12. `repeatChar` could use `strings.Repeat` or `bytes.Repeat`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/styles.go`, lines 82-91

The custom `repeatChar` function duplicates `strings.Repeat(string(ch), n)` from the standard library. Using the stdlib version would be more idiomatic.

### 13. Network collector silently returns nil on sysctl failure

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/network.go`, lines 97-104

When the sysctl call fails, the collector returns `nil` (no error), which means the UI will show stale data from the previous tick without any indication that collection failed. This is intentionally non-fatal per the `Collector` interface contract, but it means the `prev` map and `prevTime` are not updated, which will cause the next successful collection to compute deltas against a stale baseline, producing incorrect throughput numbers for one tick.

### 14. No tests in the project

There are zero test files (`*_test.go`) in the entire codebase. While the CGo-heavy code is harder to test in isolation, the pure Go logic (delta computations, `formatBytes`, `progressBar`, layout logic) is highly testable and would benefit from unit tests.

### 15. `SetColorProfile` is deprecated in newer lipgloss versions

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/cmd/mactop/main.go`, line 35

Depending on the exact lipgloss version, `SetColorProfile` may be deprecated in favor of using `lipgloss.NewRenderer` with a specific color profile. The current dependency is v1.1.0 which should still support it, but this is worth watching for future upgrades.

---

## Positive Observations

- **Clean separation of concerns:** The `platform` -> `collector` -> `ui` layering is well-thought-out. Platform code does not know about metrics types; collectors do not know about the UI.
- **CFDict wrapper pattern:** The `CFDict` type in `iokit.go` with typed accessor methods (`GetInt64`, `GetBool`, `GetString`, `GetDict`) is a good abstraction that keeps unsafe C operations in one place.
- **Proper vm_deallocate:** The `HostProcessorInfo` function correctly deallocates the processor info array via `vm_deallocate` with the right size calculation.
- **Proper IOKit cleanup in iterators:** `IOKitIterateMatching` correctly releases both the iterator and each entry's properties dictionary.
- **Defensive programming:** Division-by-zero guards are present in CPU delta computation (line 70 of cpu.go), memory percentage (line 118 of panels.go), and disk percentage (line 174 of panels.go).
- **SMC data type handling:** The `ReadFloat` method correctly handles both `flt ` (IEEE 754) and `sp78` (fixed-point) SMC data formats, with bounds checking on data size.
- **Adaptive color scheme:** The UI uses `lipgloss.AdaptiveColor` throughout, which correctly adapts to light and dark terminal backgrounds.

---

## Final Verdict

**Approve with changes.** The Mach port leak (issue #1) should be fixed before any release, as it will cause the process to degrade over time. The CPU tick wraparound (issue #3) should also be addressed. The remaining MAJOR issues are important for maintainability but do not cause immediate runtime failures. The MINOR issues are polish and best-practice improvements.

---
---

# Code Review: Time-Series Sparkline Graph Feature

**Date:** 2026-03-27
**Reviewer:** Claude Opus 4.6
**Scope:** New files: `internal/ui/history.go`, `history_test.go`, `sparkline.go`, `sparkline_test.go`. Modified files: `app.go`, `layout.go`, `panels.go`, `styles.go`, `help.go`, `layout_test.go`.
**Verdict:** Approve with changes

---

## Summary

This feature adds braille-dot sparkline graphs for CPU, GPU, memory, and network metrics, toggled with the `g` key. The implementation is clean and well-structured: a ring buffer stores metric history, a sparkline renderer converts data to braille characters, and wrapper functions in `panels.go` compose the graphs beneath existing panel content. The ring buffer and braille encoding are correct. The code integrates smoothly with the existing bubbletea architecture. There are no must-fix issues blocking merge, but several items should be addressed as follow-ups.

---

## Must Fix

No blocking issues found.

---

## Should Fix

### 1. `NewRingBuffer(0)` causes a division-by-zero panic in `Push`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go`, line 34

`Push` computes `rb.head = (rb.head + 1) % len(rb.data)`. If `NewRingBuffer(0)` is called, `len(rb.data)` is 0 and this panics with integer division by zero. While no current caller passes 0, the constructor is exported and has no guard. Similarly, `Values()` line 46 would also panic with `% len(rb.data)`.

**Suggested fix:** Add a guard at the top of `NewRingBuffer`:
```go
func NewRingBuffer(capacity int) *RingBuffer {
    if capacity <= 0 {
        capacity = 1
    }
    return &RingBuffer{
        data: make([]float64, capacity),
    }
}
```

### 2. `MetricHistory.Get` has no bounds check on the metric ID

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go`, line 95

`Get(id MetricID)` directly indexes `h.buffers[id]` with no bounds check. If an invalid `MetricID` is passed, this panics with an out-of-range index. Since `MetricID` is an exported `int` type, callers could pass any value.

**Suggested fix:** Either make `MetricID` unexported (since all callers are internal), or add a bounds check that returns nil for invalid IDs.

### 3. Vertical line interpolation fills only the current point's column, not the previous point's column

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/sparkline.go`, lines 97-118

The vertical connection loop between consecutive points (`y0` to `y1`) always uses the dot-column of data point `i` (the current point). When points `i-1` and `i` are in different dot-columns of the same braille cell, the vertical fill is drawn only in column `i`'s side. This means for a steep rise between two data points that land in the left and right halves of a single braille cell, the vertical interpolation dots are only on one side, leaving a visual gap on the other. This is a minor visual artifact rather than a correctness bug, but for a polished graph it would be better to fill both the previous and current dot-columns when they differ.

### 4. `history.Values()` allocates a new slice on every render tick

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go`, line 45
**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, lines 316-384

Each `*WithGraph` function calls `history.Values()`, which allocates a `[]float64` of up to 256 elements. With graphs enabled, this is called 5 times per tick (CPU, GPU, memory, net in, net out). At a 1-second refresh this is negligible, but at faster refresh rates or if the buffer capacity grows, consider providing a `ValuesInto(dst []float64) []float64` method that reuses a caller-provided buffer to reduce GC pressure.

### 5. Duplicate memory percentage calculation

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go`, lines 77-81
**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, lines 343-346

The memory percentage formula `float64(mem.Used) / float64(mem.Total) * 100` is duplicated in `MetricHistory.Record`, `renderMemoryPanel`, and `renderMemoryPanelWithGraph`. If the formula ever changes (e.g., to exclude wired memory), it would need to be updated in three places. Consider extracting a `memoryUsagePct(mem metrics.MemoryMetrics) float64` helper.

### 6. Layout tests do not exercise `showGraphs=true` path

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/layout_test.go`

All `renderDashboard` test calls pass `false` for `showGraphs`. The graph-enabled rendering paths in `renderWideLayout` and `renderNarrowLayout` are not tested at all. Since graphs add significant rendering logic (label padding, side-by-side joining), at least one test should verify the `showGraphs=true` path does not panic and produces output taller than the non-graph version.

**Suggested fix:** Add tests like:
```go
func TestRenderDashboard_WithGraphs(t *testing.T) {
    h := NewMetricHistory(256)
    // Push enough data so history.Len() >= 2
    for i := 0; i < 10; i++ {
        h.Record(emptyMetrics())
    }
    result := renderDashboard(emptyMetrics(), 120, 40, "v0.1.0", "1s", h, true)
    if result == "" {
        t.Error("graph-enabled dashboard should produce non-empty output")
    }
}
```

---

## Nits

### 7. `appendSparkline` shadows the package-level `labelStyle` variable

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, line 292

The local variable `labelStyle` in `appendSparkline` shadows the package-level `labelStyle` declared in `styles.go`. While this is not a bug (the local is intentionally different from the package-level one), it could confuse future readers. Consider naming it `graphLabelStyle` or `sparkLabelStyle`.

### 8. Magic number 256 for history capacity

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/app.go`, line 59

The history capacity of 256 is hardcoded. Consider extracting it as a named constant (e.g., `const defaultHistoryCapacity = 256`) for clarity and to make it easy to find and tune.

### 9. `formatBytesRate` duplicates logic from `formatBytes`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, lines 402-418

`formatBytesRate` is very similar to `formatBytes` but operates on `float64` and appends `/s`. Consider refactoring `formatBytes` to accept a `float64` or adding a `formatBytesF(b float64) string` helper that both functions call, to avoid the duplicated unit thresholds.

### 10. Braille bit table could use a doc reference

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/sparkline.go`, lines 10-18

The braille bit positions are correct per the Unicode Braille Patterns block (U+2800-U+28FF), but a brief reference comment (e.g., "See Unicode chart U+2800") would help future maintainers verify the mapping without having to look it up themselves.

### 11. `autoScaleMax` floor of 1024 is arbitrary

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, line 396

The 1024-byte floor for network auto-scaling means the graph will never zoom in below 1 KB/s. This seems reasonable but a brief comment explaining the rationale (e.g., "avoid noisy scaling for near-zero traffic") would help.

---

## Positive Observations

- **Ring buffer is correct.** The wrap-around arithmetic on line 46 (`(rb.head - rb.count + len(rb.data)) % len(rb.data)`) correctly handles the modular arithmetic for chronological ordering. The tests thoroughly verify partial fill, exact fill, and multi-wrap scenarios.
- **Braille encoding is correct.** The bit positions in `brailleBits` match the Unicode standard. The y-axis flip logic (`flippedY = (totalDots - 1) - dy`) correctly maps data-space coordinates (0=bottom) to screen-space (0=top).
- **Good edge case handling in sparkline.** The renderer guards against zero width, zero height, empty data, NaN values, equal min/max, and out-of-range values. The clamping on lines 59-65 prevents buffer overflows.
- **Clean integration pattern.** The `*WithGraph` wrapper functions compose well -- they call the existing panel renderer and append the graph below, avoiding any modification to the existing rendering logic.
- **Right-alignment for sparse data.** When there are fewer data points than the graph width allows, the sparkline is right-aligned so the graph fills from the right as data accumulates. This is a nice UX touch.
- **Adaptive colors for sparklines.** The sparkline colors use `lipgloss.AdaptiveColor` consistent with the rest of the codebase, ensuring readability on both light and dark terminals.

---

## Final Verdict

**Approve with changes.** The core data structures (ring buffer) and rendering (braille sparkline) are correct and well-tested. The integration is clean and non-disruptive to existing code. The should-fix items are primarily about defensive programming (#1, #2), visual polish (#3), and test coverage gaps (#6). None are blocking. The nits are standard code hygiene improvements. This is a solid feature addition.

---
---

# Re-Review: Time-Series Sparkline Graph Feature (Post-Fix)

**Date:** 2026-03-27
**Reviewer:** Claude Opus 4.6
**Scope:** Re-review of fixes applied to `internal/ui/history.go`, `sparkline.go`, `app.go`, `panels.go`, `styles.go`, `layout.go`, `layout_test.go`, `history_test.go`, `sparkline_test.go`.
**Verdict:** Approve

---

## Previous Findings -- Resolution Status

### 1. NewRingBuffer(0) panic -- RESOLVED
`history.go` lines 27-29: capacity is now clamped to 1 when <= 0. Correct fix.

### 2. MetricHistory.Get() bounds check -- RESOLVED
`history.go` lines 127-132: returns nil for out-of-range IDs. Correct fix.

### 3. math.IsInf check in sparkline -- RESOLVED
`sparkline.go` line 54: `math.IsInf(v, 0)` is checked alongside `math.IsNaN(v)`. Correct fix.

### 4. Values() allocation -- RESOLVED
`history.go` lines 45-60: now uses two `copy()` calls instead of per-element modulo. `ValuesInto()` method added on lines 62-83. Correct fix.

### 5. Flattened 2D sparkline grid -- RESOLVED
`sparkline.go` line 70: grid is now `[]uint8` of size `Height*Width` with flat indexing (`row*opts.Width+col`). Correct fix.

### 6. Named constant historyCapacity -- RESOLVED
`app.go` line 14: `const historyCapacity = 256` is defined and used on line 62. Correct fix.

### 7. Cached lipgloss styles -- RESOLVED
`styles.go` lines 55-69: `barBaseStyle`, `sparklineStyle`, and `graphLabelStyle` are declared as package-level variables. Correct fix.

### 8. showGraphs=true test paths -- RESOLVED
`layout_test.go` lines 79-121: `populatedHistory()` helper and two new tests (`TestRenderDashboard_ShowGraphsWide`, `TestRenderDashboard_ShowGraphsNarrow`) exercise the graph-enabled rendering path with meaningful data. Good coverage.

---

## New Issues Found

### Should Fix

**1. `ValuesInto` is dead code -- defined but never called**

File: `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go`, line 64

The `ValuesInto` method was added per the previous review's recommendation to reduce GC pressure, but no caller was updated to use it. All `*WithGraph` functions in `panels.go` still call `Values()`. Either the callers should be migrated to use `ValuesInto`, or the method should be removed until it is needed. Dead code adds maintenance burden and untested surface area.

**2. No tests for the two defensive fixes (NewRingBuffer clamp and Get bounds check)**

File: `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history_test.go`

The fixes for `NewRingBuffer(0)` and `Get()` with invalid IDs are not covered by any test. Adding tests for these edge cases would both document the intended behavior and prevent regressions. For example:

```go
func TestNewRingBuffer_ZeroCapacity(t *testing.T) {
    rb := NewRingBuffer(0)
    rb.Push(42)
    if rb.Len() != 1 {
        t.Errorf("Len() = %d, want 1", rb.Len())
    }
}

func TestMetricHistory_Get_InvalidID(t *testing.T) {
    h := NewMetricHistory(10)
    if h.Get(MetricID(-1)) != nil {
        t.Error("Get(-1) should return nil")
    }
    if h.Get(metricCount) != nil {
        t.Error("Get(metricCount) should return nil")
    }
}
```

**3. No test for `ValuesInto`**

File: `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history_test.go`

If `ValuesInto` is kept, it should have at least one test verifying it produces the same output as `Values()` and that it reuses the provided buffer's backing array when capacity is sufficient.

---

## Nits

**1. Duplicate memory percentage calculation remains**

Files: `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history.go` lines 109-112, `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go` lines 117-119 and 341-344

This was noted in the previous review (item #5) and was not addressed. The formula `float64(mem.Used) / float64(mem.Total) * 100` appears in three places. Not blocking, but extracting a helper would reduce the risk of inconsistency.

**2. `formatBytesRate` duplication remains**

File: `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go` lines 400-416

Also noted in the previous review (item #9) and not addressed. `formatBytesRate` and `formatBytes` share the same unit thresholds. Minor duplication.

---

## Final Verdict

**Approve.** All eight previous findings have been properly addressed with correct implementations. No regressions were introduced. The remaining items are minor: dead code (`ValuesInto`), missing tests for defensive edge cases, and previously-noted duplication that was deferred. None are blocking. The feature is ready to merge.

---
---

# Code Review: Stacked Layout + Temperature Graph Feature

**Date:** 2026-03-27
**Reviewer:** Claude Opus 4.6
**Scope:** Changes to `internal/ui/panels.go` (stacked sparkline layout, network graph rewrite, new temperature graph), `internal/ui/history.go` (MetricTemp), `internal/ui/styles.go` (sparkTempColor), `internal/ui/layout.go` (temperature graph integration).
**Verdict:** Approve with changes

---

## Summary

This changeset simplifies the sparkline layout from side-by-side (label + graph on one line) to stacked (label above graph), adds a temperature time-series graph with auto-scaling, and rewrites the network graph to use a compact 2-row height with inline label+graph composition. The stacked layout is a good simplification that removes the `graphLabelWidth` constant and the `lipgloss.JoinHorizontal` complexity. The temperature graph is well-integrated. A few issues below.

---

## Must Fix

No blocking issues found.

---

## Should Fix

### 1. Temperature auto-scale `Min: 0` produces a misleading graph for typical sensor ranges

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, line 387

The temperature sparkline uses `Min: 0` and a dynamic max (`maxVal * 1.2`, floored at 30). Typical CPU temperatures range from ~35-95 C. With `Min: 0`, a sensor fluctuating between 40-50 C will appear as a nearly flat line in the upper third of the graph, wasting two-thirds of the vertical resolution on a range (0-40 C) that is never reached.

Network graphs have the same `Min: 0` behavior, but that makes sense because network throughput genuinely starts at zero. Temperature does not.

**Suggested fix:** Set `Min` to something like `maxVal * 0.6` or `minVal - 5` (where `minVal` is the minimum in the data window), so the graph uses its vertical space to show actual variation. For example:

```go
minVal := data[0]
for _, v := range data {
    if v < minVal {
        minVal = v
    }
}
scaleMin := minVal * 0.8
if scaleMin < 0 {
    scaleMin = 0
}
```

### 2. No tests for `renderTemperaturePanelWithGraph` or `MetricTemp` recording

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels_test.go`
**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/history_test.go`

The existing `TestMetricHistory_RecordAndGet` and `TestMetricHistory_RecordAllFiveMetrics` tests do not include temperature data in their `SystemMetrics` fixture (the `Temperature` field is zero-valued). This means `MetricTemp` recording is untested. Similarly, there are no tests for `renderTemperaturePanelWithGraph`.

**Suggested fix:** Add test cases:

```go
// In history_test.go - extend existing test or add new one:
func TestMetricHistory_RecordTemperature(t *testing.T) {
    h := NewMetricHistory(5)
    h.Record(metrics.SystemMetrics{
        Temperature: metrics.TemperatureMetrics{
            Available: true,
            Sensors: []metrics.TempSensor{
                {Name: "CPU", Value: 55.0},
                {Name: "GPU", Value: 42.0},
            },
        },
    })
    vals := h.Get(MetricTemp).Values()
    if len(vals) != 1 || vals[0] != 55.0 {
        t.Errorf("MetricTemp = %v, want [55.0] (max of sensors)", vals)
    }
}

// Test that unavailable temperature records 0:
func TestMetricHistory_RecordTemperature_Unavailable(t *testing.T) {
    h := NewMetricHistory(5)
    h.Record(metrics.SystemMetrics{
        Temperature: metrics.TemperatureMetrics{Available: false},
    })
    vals := h.Get(MetricTemp).Values()
    if len(vals) != 1 || vals[0] != 0.0 {
        t.Errorf("MetricTemp unavailable = %v, want [0.0]", vals)
    }
}
```

### 3. `renderNetworkPanelWithGraph` ignores the `graphHeight` parameter

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, lines 341-342

The function accepts `graphHeight int` as a parameter but hardcodes `netHeight := 2` on line 342 and ignores `graphHeight` entirely. This means the caller's intent (e.g., `wideGraphHeight = 4` from layout.go line 31) is silently discarded.

**Suggested fix:** Either use `graphHeight` (perhaps clamped: `netHeight := min(graphHeight, 2)` if compact is the goal), or remove the `graphHeight` parameter from this function's signature and document that network graphs are always 2 rows. The current state is misleading -- the parameter suggests the caller controls the height, but they do not.

---

## Nits

### 4. Duplicate auto-scale logic between `renderTemperaturePanelWithGraph` and `autoScaleMax`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, lines 375-384 and 393-405

`renderTemperaturePanelWithGraph` manually iterates data to find max and applies `* 1.2` with a floor. `autoScaleMax` does the same thing (iterate, multiply by 1.2, apply floor). The only difference is the floor value (30.0 vs 1024). Consider parameterizing `autoScaleMax`:

```go
func autoScaleMax(data []float64, floor float64) float64 {
```

### 5. Temperature graph label shows current value, not actual max

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/panels.go`, line 385

The label says `"Max %.1f C"` but uses `data[len(data)-1]` (the most recent value), not the actual maximum across the history window. This is inconsistent with the label text. Either change the label to `"Temp %.1f C"` or use `maxVal` (computed on lines 375-379) for the label value.

---

## Positive Observations

- The stacked layout in `appendSparkline` (line 290: `base + "\n" + label + "\n" + graph`) is simpler and more readable than the previous side-by-side approach. Removing `graphLabelWidth` and the horizontal join logic is a good cleanup.
- The temperature recording in `Record()` correctly handles `Available: false` by leaving `maxTemp` at 0.0, which is a safe sentinel value.
- The `scaleMax` floor of 30.0 for temperature is a sensible choice -- it prevents the graph from becoming noisy when all sensors read near zero (e.g., on a cold start).
- The network graph's 2-row height is a good UX choice for keeping the panel compact while still showing trend direction.

---

## Final Verdict

**Approve with changes.** The stacked layout simplification is clean and the temperature graph integration is correct. The main issues are: the misleading graph label (#5 -- quick fix), the ignored `graphHeight` parameter (#3 -- interface consistency), the `Min: 0` scaling choice (#1 -- UX improvement), and missing test coverage (#2). None are blocking, but #3 and #5 should be addressed before this is considered complete.
