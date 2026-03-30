# Security Audit: Time-Series Sparkline Graph Feature

**Date:** 2026-03-27
**Auditor:** Claude Opus 4.6 (automated security review)
**Scope:** New sparkline graph feature -- `internal/ui/history.go`, `internal/ui/sparkline.go`, and modifications to `internal/ui/app.go`, `internal/ui/layout.go`, `internal/ui/panels.go`

---

## Findings

### Finding 1: Panic on Zero-Capacity RingBuffer

- **Severity:** Low
- **Location:** `internal/ui/history.go:33-34`
- **Issue:** `NewRingBuffer(0)` allocates a zero-length slice. The first call to `Push()` will panic with an index-out-of-range error on `rb.data[rb.head]`, and the modulus on line 34 (`% len(rb.data)`) will panic with integer divide by zero.
- **Impact:** If `NewMetricHistory` is ever called with `capacity <= 0`, the application will crash on the first metric collection tick. Currently the hardcoded value is 256 (line 59 of `app.go`), so this is not reachable in production, but the API is exported and unguarded.
- **Remediation:** Add a guard at the top of `NewRingBuffer`:
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
- **References:** CWE-369 (Divide By Zero), CWE-129 (Improper Validation of Array Index)

### Finding 2: Unchecked MetricID in MetricHistory.Get()

- **Severity:** Low
- **Location:** `internal/ui/history.go:94-96`
- **Issue:** `Get(id MetricID)` performs no bounds check on the `id` parameter before indexing into the fixed-size array `h.buffers[id]`. An out-of-range `MetricID` value would cause a panic (index out of range on a fixed array).
- **Impact:** Currently all call sites use the defined `MetricCPU`..`MetricNetOut` constants, so this is not reachable. However, the type is exported (`MetricID` is `int`), and any caller passing an invalid value would crash the program.
- **Remediation:** Either make the type unexported, or add a bounds check:
  ```go
  func (h *MetricHistory) Get(id MetricID) *RingBuffer {
      if id < 0 || int(id) >= len(h.buffers) {
          return NewRingBuffer(1) // return safe empty buffer
      }
      return h.buffers[id]
  }
  ```
- **References:** CWE-129 (Improper Validation of Array Index)

### Finding 3: Integer Overflow Potential in Sparkline Grid Dimensions

- **Severity:** Low
- **Location:** `internal/ui/sparkline.go:37-38`
- **Issue:** `samples := opts.Width * 2` and `totalDots := opts.Height * 4` are computed as plain `int` multiplications. With extremely large `Width` or `Height` values (greater than `math.MaxInt / 4`), these could theoretically overflow on 32-bit platforms, leading to negative values and unexpected behavior in the subsequent slice allocations and loop bounds.
- **Impact:** On 64-bit systems (the only platform mactop targets -- macOS on Apple Silicon), this is not practically reachable since terminal dimensions are bounded by the OS. On a hypothetical 32-bit build, a `Height` of ~536 million would trigger overflow, which is not a real terminal size.
- **Remediation:** No action required for the current target platform. If portability to 32-bit platforms is ever desired, add sanity caps:
  ```go
  if opts.Width > 1000 || opts.Height > 1000 {
      return ""
  }
  ```
- **References:** CWE-190 (Integer Overflow)

### Finding 4: Per-Render Allocations in Values() and RenderSparkline()

- **Severity:** Informational
- **Location:** `internal/ui/history.go:45`, `internal/ui/sparkline.go:52,70-73`
- **Issue:** Every call to `Values()` allocates a new slice of up to 256 float64s. `RenderSparkline` allocates an additional `dotYs` slice and a 2D grid. These are called on every render tick when graphs are enabled (potentially every 500ms-2s). With 5 metrics, this is ~10 slice allocations per tick.
- **Impact:** This is not a security vulnerability but a minor GC pressure concern. The allocations are small (a few KB total) and bounded, so there is no risk of unbounded memory growth or denial of service.
- **Remediation:** Optionally, pre-allocate reusable buffers in the `MetricHistory` or pass a scratch buffer to `RenderSparkline`. This is a performance optimization, not a security fix.
- **References:** N/A

### Finding 5: NaN Handling is Correct but Inf is Not Explicitly Handled

- **Severity:** Informational
- **Location:** `internal/ui/sparkline.go:54-57`
- **Issue:** The renderer correctly skips `NaN` values (line 54-56). However, `+Inf` or `-Inf` float64 values are not explicitly checked. An `Inf` value would pass the `IsNaN` check, and the expression `(v - opts.Min) / rng * float64(totalDots-1)` with `v = +Inf` would produce `+Inf`, which when cast to `int` yields a platform-dependent value. The subsequent clamping on lines 59-65 would catch positive overflow (`dy >= totalDots` clamps to `totalDots - 1`), but `int(+Inf)` in Go is undefined behavior per the spec.
- **Impact:** In practice, the metrics feeding this code (CPU%, GPU%, memory%) are bounded percentages and network throughput values from OS APIs. Inf values are not expected from the collectors. The clamping lines provide a safety net, but the `int(float64)` conversion of Inf is technically undefined.
- **Remediation:** Add an explicit Inf check alongside the NaN check:
  ```go
  if math.IsNaN(v) || math.IsInf(v, 0) {
      dotYs[i] = -1
      continue
  }
  ```
- **References:** CWE-681 (Incorrect Conversion between Numeric Types)

### Finding 6: No Concurrency Protection on RingBuffer or MetricHistory

- **Severity:** Informational
- **Location:** `internal/ui/history.go:18-22`, `internal/ui/history.go:58-61`
- **Issue:** The `RingBuffer` and `MetricHistory` structs have no mutex or synchronization. If `Push()` and `Values()` were ever called concurrently, data races would occur.
- **Impact:** In the current bubbletea architecture, the Model's `Update()` (which calls `collectAll` -> `Record`) and `View()` (which calls `Values()`) are invoked sequentially by the bubbletea runtime on the same goroutine, so there is no concurrent access. This is safe as-is but would become a bug if the architecture changed to use background goroutines for collection.
- **Remediation:** Document the single-goroutine assumption. Optionally, add a `sync.RWMutex` if future-proofing is desired.
- **References:** CWE-362 (Race Condition)

---

## Items Verified as Safe

1. **Division by zero in sparkline range calculation** (`sparkline.go:46-49`): Correctly guarded -- when `opts.Max - opts.Min <= 0`, the range is forced to 1.0.

2. **Division by zero in memory percentage** (`history.go:78`): Correctly guarded with `if m.Memory.Total > 0`.

3. **Braille bit manipulation** (`sparkline.go:13-18,92,115`): The `brailleBits` lookup table uses `uint8` values that are OR'd into `uint8` grid cells. The maximum possible value is `0x01|0x02|0x04|0x08|0x10|0x20|0x40|0x80 = 0xFF`, which fits in `uint8`. The braille base offset `0x2800 + int(bits)` yields a maximum of `0x28FF`, which is within the valid Unicode braille block. No overflow possible.

4. **Bounded memory usage**: The ring buffer capacity is fixed at 256 (hardcoded in `app.go:59`). Each buffer stores 256 float64s = 2KB. With 5 metrics, total history storage is ~10KB, permanently bounded.

5. **No injection vectors**: The code renders to a TUI via lipgloss/bubbletea. There is no shell execution, no file I/O, no network I/O, no SQL, no template rendering. The metric data comes from OS system calls, not user input.

6. **Grid bounds checks** (`sparkline.go:91,114`): All grid writes are guarded by explicit `row >= 0 && row < opts.Height && col >= 0 && col < opts.Width` bounds checks.

7. **Empty data paths**: `RenderSparkline` returns `""` for empty data, zero width, or zero height. The `*WithGraph` panel functions check `history.Len() < 2` before rendering. These prevent panics on startup before enough data has been collected.

8. **Test coverage**: Both `history_test.go` and `sparkline_test.go` cover key edge cases including empty buffers, wrap-around, zero dimensions, equal min/max, and excess data truncation.

---

## Executive Summary

**Overall Risk Posture: Low**

This feature has a small attack surface. It processes only internal system metrics (no external/user input), renders only to a local TUI (no network output), and performs no file or network I/O. The code is well-structured with appropriate defensive checks at critical points.

**Finding Summary:**
| Severity | Count |
|---|---|
| Critical | 0 |
| High | 0 |
| Medium | 0 |
| Low | 3 |
| Informational | 3 |

**Recommended Priority Order:**

1. **Finding 5** (Inf handling) -- Trivial one-line fix that eliminates undefined behavior from `int(+Inf)` conversion. Low effort, removes a theoretical correctness issue.
2. **Finding 1** (zero-capacity guard) -- Simple defensive check in `NewRingBuffer`. Prevents a panic if the API is ever misused.
3. **Finding 2** (MetricID bounds check) -- Simple guard on an exported API. Prevents panic from invalid input.
4. Findings 3, 4, 6 are informational and require no immediate action.

No blocking issues were identified. The feature is suitable for merge from a security perspective.

---

# Security Re-Audit: Time-Series Sparkline Graph (Post-Fix Verification)

**Date:** 2026-03-27
**Auditor:** Claude Opus 4.6 (automated security review)
**Scope:** Verification of three security fixes applied to `internal/ui/history.go` and `internal/ui/sparkline.go`

---

## Fix Verification

### Finding 1 (Original): Panic on Zero-Capacity RingBuffer -- FIXED

- **Location:** `internal/ui/history.go:26-29`
- **Status:** Verified fixed.
- **Assessment:** The guard `if capacity <= 0 { capacity = 1 }` is correctly placed at the top of `NewRingBuffer` before the `make` call. This eliminates both the index-out-of-range panic in `Push()` and the divide-by-zero in the modulus operation. The fix matches the recommended remediation exactly.

### Finding 2 (Original): Unchecked MetricID in Get() -- FIXED

- **Location:** `internal/ui/history.go:127-132`
- **Status:** Verified fixed.
- **Assessment:** The bounds check `if id < 0 || id >= metricCount` correctly guards against out-of-range access. Returning `nil` (rather than a safe empty buffer as originally suggested) is an acceptable choice -- callers must handle a nil return, but this is idiomatic Go and makes misuse obvious rather than silently returning empty data. No nil-dereference risk was found in existing call sites (the panel rendering functions check `history.Len() < 2` which would catch a nil buffer via a nil pointer dereference only if `Get()` returned nil -- however, all current call sites use valid MetricID constants, so this path is not reachable in practice).

### Finding 5 (Original): Undefined Behavior from int(+Inf) -- FIXED

- **Location:** `internal/ui/sparkline.go:54`
- **Status:** Verified fixed.
- **Assessment:** The check `math.IsNaN(v) || math.IsInf(v, 0)` on line 54 correctly guards against both NaN and Inf (positive and negative) before the float-to-int conversion on line 58. Values that match are assigned `dotYs[i] = -1` and skipped in all subsequent processing (lines 76-77 and 95-96 check `dy < 0`). This eliminates the undefined behavior from `int(+Inf)`.

---

## New Issues Identified

No new security issues were introduced by the fixes.

---

## Remaining Items from Original Audit (Unchanged)

The following informational findings from the original audit remain unaddressed but require no action:

- **Finding 3** (Integer overflow on 32-bit): Not applicable to the macOS/ARM64 target platform.
- **Finding 4** (Per-render allocations): Performance concern only, not security-relevant. Note that `ValuesInto` (lines 64-83 of history.go) was added as a buffer-reuse optimization, partially addressing this.
- **Finding 6** (No concurrency protection): Safe under bubbletea's single-goroutine model.

---

## Executive Summary

**Overall Risk Posture: Low (unchanged)**

All three recommended fixes have been correctly applied. The implementations match or improve upon the suggested remediations. No regressions or new vulnerabilities were introduced. The feature remains suitable for merge from a security perspective.
