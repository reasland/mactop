# Code Review #2: mactop Post-Fix Review

**Date:** 2026-03-27
**Reviewer:** Claude Opus 4.6
**Scope:** All new and modified files after fixes from Review #1, plus full re-review of unchanged files
**Verdict:** Approve with changes (2 MAJOR, 5 MINOR issues remain)

---

## Review #1 Fix Verification

The first review identified 15 issues. Here is the status of each:

| # | Issue | Status | Notes |
|---|-------|--------|-------|
| 1 | Mach host port leak | **FIXED** | `mach.go:23-27` now caches `hostPort` at init time. Correct fix. |
| 2 | Network delta wraparound | **FIXED** | `network.go:149-157` now checks `>=` before subtracting. Correct. |
| 3 | CPU tick wraparound | **FIXED** | `cpu.go:66-71` now detects wraparound and skips the sample. Correct. |
| 4 | `collectAll` value vs pointer receiver | **NOT FIXED** | `Update` still uses a value receiver. See MAJOR #1 below. |
| 5 | SMC connection never closed on exit | **FIXED** | `main.go:39` has `defer model.Close()` and `app.go:104-106` implements `Close()`. |
| 6 | Verbose flag does nothing | **FIXED** | `app.go:60-65` now uses `tea.LogToFile` and `log.New`. Functional. |
| 7 | Makefile hardcodes GOARCH=arm64 | **FIXED** | `Makefile:4` now uses `GOARCH ?= $(shell go env GOARCH)`. Correct. |
| 8 | Magic number in SetColorProfile | **NOT FIXED** | `main.go:35` still uses `lipgloss.SetColorProfile(0)`. See MINOR #1 below. |
| 9 | SwapUsage struct layout | **FIXED** | `sysctl.go:79-93` now uses `C.struct_xsw_usage` directly instead of manual byte layout. Correct and much better. |
| 10 | GPU collector overwrites on multiple matches | **FIXED** | `gpu.go:49-51` now returns `errGPUFound` sentinel to stop after first match. Good pattern. |
| 11 | Temperature uses C instead of degree symbol | **FIXED** | `panels.go:234` now uses `\u00b0C`. |
| 12 | Custom `repeatChar` function | **FIXED** | `styles.go` now uses `strings.Repeat`. |
| 13 | Network collector returns nil on failure | **NOT FIXED** | Still returns `nil` on sysctl failure without updating prev state. Same as before. Acceptable per interface contract but noted. |
| 14 | No tests | **FIXED** | `smc_test.go`, `layout_test.go`, `styles_test.go` now exist with meaningful coverage. |
| 15 | SetColorProfile deprecated | **NOT FIXED** | Still using the same API. Low priority, acceptable. |

**Summary: 11 of 15 issues fixed. 2 carried forward as non-blocking (items 8, 15). 1 carried forward as design-accepted (item 13). 1 unresolved (item 4, see below).**

---

## New Files Review

### `internal/platform/iohid.go` (NEW)

This is a well-written CGo wrapper for the private IOHIDEventSystem API. The C code is clean with proper null checks and CFRelease calls on all paths.

**Positive observations:**
- `hidGetProductName` correctly handles both `NULL` return and type ID mismatch, releasing the property in all failure paths (lines 77-92)
- `hidReadTemperature` correctly releases the event via `CFRelease` (line 102)
- The sentinel value `-999.0` for read failure is filtered out by the Go side's `temp < 0` check (line 180)
- `hidSetTempMatching` releases both `CFNumberRef` values and the dictionary (lines 51-53)
- Go side properly `free()`s the C string from `hidGetProductName` (line 171)
- The `Close()` method nil-checks and nil-sets the client (lines 194-199), making it safe to call multiple times

**No issues found in this file.**

### `internal/smc/smc.go` (REWRITTEN)

The rewrite using proper sub-structs for the 80-byte `SMCKeyData_t` is a significant improvement over any manual byte-offset approach. The key info caching is a good optimization.

**Positive observations:**
- `smcReadKey` bounds-checks `copySize` against `sizeof(output.bytes)` (line 105)
- Key info caching in `keyCache` avoids redundant `kSMCGetKeyInfo` calls
- `ReadFloat` validates key length (line 198) and data size (lines 237, 244)
- `encodeKey` is pure Go, avoiding an unnecessary CGo call
- `DiscoverTempSensors` filters by first character 'T' before attempting a read, which is a good optimization for the ~1000+ SMC keys

### `internal/collector/temperature.go` (REWRITTEN)

The HID-primary / SMC-fallback pattern is well-designed.

**Positive observations:**
- The `failed` flag prevents repeated initialization attempts (line 32-33)
- `tryInitHID` cleans up the reader on failure (line 69)
- `tryInitSMC` cleans up the connection on failure (line 95-96)
- `buildHIDMetrics` correctly handles empty `dieTemps` slice (line 200)
- `Close()` nil-checks both resources (lines 225-233)

---

## MAJOR (Should Fix)

### 1. `Update` value receiver causes `model.Close()` in `main.go` defer to operate on a stale copy

**Files:**
- `/Users/rileyeasland/Documents/workarea/home/mactop/cmd/mactop/main.go:38-39`
- `/Users/rileyeasland/Documents/workarea/home/mactop/internal/ui/app.go:74,104`

`NewModel` returns a `Model` (value, not pointer) on line 38 of `main.go`. `defer model.Close()` on line 39 calls `Close()` on the **original** `model` variable. However, bubbletea's `Program.Run()` works with the `tea.Model` interface, and since `Update` has a value receiver (`func (m Model) Update(...) (tea.Model, tea.Cmd)`), bubbletea operates on copies. The `model` variable in `main()` is never updated after `p.Run()` begins.

This means `defer model.Close()` closes the `TempCollector` from the **original** model -- the one that was passed to `tea.NewProgram`. If `Collect()` has been called at least once (which it will have been, via `TickMsg`), the `TempCollector` inside the copy held by bubbletea may have a different `hidReader` or `conn` than the one in the original `model`. Specifically:

- `tryInitHID()` sets `c.hidReader` on the copy's `TempCollector`
- `tryInitSMC()` sets `c.conn` on the copy's `TempCollector`
- The original `model.temp` still has `nil` for these fields

Since `TempCollector` is a pointer (`*collector.TempCollector`), and `NewTempCollector()` returns a pointer, the pointer itself is copied by value into the bubbletea model. But because it is a **pointer**, both the original and the copy point to the **same** `TempCollector` struct. So `model.Close()` in the defer will actually close the correct resources.

**Conclusion: This works correctly by accident** -- because all collectors are stored as pointers in the `Model` struct. However, this is fragile and should be documented with a comment, or `NewModel` should return `*Model`. The `Update` value-receiver pattern also creates unnecessary allocations on every key press and tick. This was flagged in Review #1 (issue #4) and remains unaddressed.

**Suggested fix:** Add a comment on the `Model` struct explaining that all collector fields must be pointers for the value-receiver `Update` pattern to work correctly:

```go
// Model is the top-level bubbletea model. All mutable collector fields
// must be pointers so that value-receiver copies in bubbletea's Update
// loop share state with the original model passed to tea.NewProgram.
type Model struct {
```

### 2. `buildHIDMetrics` max calculation initializes max to 0, which is incorrect for negative temperatures

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/temperature.go:201-206`

```go
var sum, max float64
for _, t := range dieTemps {
    sum += t
    if t > max {
        max = t
    }
}
```

`max` starts at `0.0`. If all die temperatures are negative (which is physically implausible on a running Mac, but the HID filter at line 180 allows `temp >= 0`), `max` would incorrectly remain `0.0`. More practically, since the filter is `temp < 0 || temp > 150`, a temperature of exactly `0.0` would pass the filter and `max` would be `0.0` even if no sensor reported `0.0`.

This is not a high-likelihood bug in practice (CPUs are always above ambient), but it is a code correctness issue. The idiomatic fix is to initialize `max` to `dieTemps[0]` or `math.Inf(-1)`.

**Suggested fix:**
```go
max := dieTemps[0]
sum := dieTemps[0]
for _, t := range dieTemps[1:] {
    sum += t
    if t > max {
        max = t
    }
}
```

---

## MINOR (Nice to Fix)

### 1. Magic number `0` in `SetColorProfile` call

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/cmd/mactop/main.go:35`

```go
lipgloss.SetColorProfile(0)
```

This was noted in Review #1 (issue #8) and is still present. The value `0` corresponds to "no color" but is not self-documenting.

### 2. `hidSetTempMatching` is called on every `ReadTemperatures` call

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/platform/iohid.go:150`

```go
func (r *HIDThermalReader) ReadTemperatures() ([]HIDTempSensor, error) {
    C.hidSetTempMatching(r.client)
```

`hidSetTempMatching` creates two `CFNumberRef` objects and a `CFDictionaryRef` on every call (lines 42-53 in the C code), then releases them. The matching filter does not change between calls. This could be set once during `NewHIDThermalReader` instead of on every read, avoiding repeated allocations.

**Suggested fix:** Move the `hidSetTempMatching` call into `NewHIDThermalReader`, right after `hidCreateClient` succeeds.

### 3. `DiscoverTempSensors` reads every matching key's value during discovery

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/smc/smc.go:289`

```go
val, err := c.ReadFloat(keyStr)
```

During discovery, every key starting with 'T' is read via `ReadFloat`, which involves two IOConnectCallStructMethod calls (GetKeyInfo + ReadKey). On a system with many 'T' keys, this could be slow on the first tick. This is called once during initialization and the results are cached, so the performance impact is limited, but it is worth noting.

### 4. `errorString` type in `iohid.go` could use `errors.New`

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/platform/iohid.go:141-146`

```go
var errHIDUnavailable = errorString("IOHIDEventSystem unavailable")

type errorString string
func (e errorString) Error() string { return string(e) }
```

The comment says "simple error type to avoid importing errors package", but the `errors` package is trivially small and already used elsewhere in the project (e.g., `smc.go:142`). Using a custom error type just for this is unnecessary.

**Suggested fix:** Replace with `var errHIDUnavailable = errors.New("IOHIDEventSystem unavailable")` and import `"errors"`.

### 5. `TempCollector.initialized` field is set but never read

**File:** `/Users/rileyeasland/Documents/workarea/home/mactop/internal/collector/temperature.go:19,45`

```go
initialized bool   // line 19, declared
c.initialized = true  // line 45, set
```

The `initialized` field is set to `true` on line 45 but is never read anywhere. The initialization gating is done via `useHID`, `useSMC`, and `failed` flags, making `initialized` dead code.

**Suggested fix:** Remove the `initialized` field.

---

## Unchanged Files: Re-Review Notes

### `internal/collector/gpu.go`
The `errGPUFound` sentinel pattern is clean. The cast `uint64(inUse)` on line 41 and `uint64(alloc)` on line 45 convert from `int64` -- this is safe because memory values from IOKit are always non-negative. No issues.

### `internal/collector/power.go`
The C code correctly releases both `info` and `list` on all paths (lines 30-39, 64-65). The `readSmartBattery` properly uses defer for cleanup. No issues.

### `internal/platform/iokit.go`
`IOKitIterateMatching` correctly releases properties via `dict.Release()` even when `fn` returns an error (line 243). The `GetDict` comment on line 177 correctly notes that the returned sub-dict does NOT own its reference. No issues.

### `internal/platform/cgo.go`
Contains only the `#cgo LDFLAGS` directive. Correct and minimal. No issues.

### `internal/metrics/types.go`
Clean data types with appropriate comments. No issues.

### `internal/ui/styles.go`, `panels.go`, `layout.go`, `help.go`
The progress bar handles edge cases well (negative, >100%, very small width). The `formatBytes` function has tests covering all ranges including `maxUint64`. Layout handles both wide and narrow terminals. No issues.

### `Makefile`
Now uses `GOARCH ?=` as recommended. The `-s -w` ldflags strip debug info which is appropriate for a release build. No issues.

### `go.mod`
Uses Go 1.24.0 and reasonable dependency versions. No issues.

---

## Positive Observations (New in this Review)

- **HID/SMC fallback pattern:** The `TempCollector` gracefully degrades from HID to SMC to "unavailable", with proper cleanup on each failure path. This is good defensive design for cross-version macOS compatibility.
- **Key info caching in SMC:** The `keyCache` map avoids redundant `IOConnectCallStructMethod` calls for key metadata that never changes. Good optimization.
- **Deduplication in HID reader:** The `seen` map in `ReadTemperatures` (line 160) prevents duplicate sensors, and the sensor aggregation in `buildHIDMetrics` (die averaging, first-match for battery/SSD) produces a clean, readable output.
- **Proper sub-struct alignment in SMC:** Using C sub-structs for `SMCKeyData_t` instead of manual byte offsets ensures the compiler handles alignment correctly across architectures.
- **Tests added:** The `smc_test.go` tests verify key encoding against known C constants. The `styles_test.go` tests are thorough with edge cases. The `layout_test.go` tests verify no panics at various widths including boundary conditions.
- **Clean resource lifecycle:** `defer model.Close()` in `main.go` plus the `Close()` method in `app.go` and `temperature.go` form a proper cleanup chain.
- **Verbose logging:** The `tea.LogToFile` integration provides a real debugging mechanism now, not a dead flag.

---

## Final Verdict

**Approve with changes.** The codebase has improved significantly since Review #1. The critical Mach port leak and wraparound bugs are fixed correctly. The new HID thermal reader is well-implemented with proper CGo memory management. The two MAJOR issues (value-receiver documentation, max initialization) are low-risk in practice but should be addressed. The five MINOR issues are polish items. No blocking issues remain.
