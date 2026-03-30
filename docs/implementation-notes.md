# Implementation Notes

## What was implemented

Complete `mactop` CLI tool -- a real-time macOS system monitor TUI with 7 collectors.

### File structure

- **`cmd/mactop/main.go`** -- Entrypoint with CLI flags (`-i`, `-v`, `--version`, `--no-color`). Clamps minimum interval to 250ms.
- **`internal/metrics/types.go`** -- All metric data structs per architecture doc section 5.
- **`internal/platform/cgo.go`** -- CGo linker flags (`-framework IOKit -framework CoreFoundation`).
- **`internal/platform/mach.go`** -- `HostProcessorInfo()` for CPU ticks, `HostVMInfo64()` for memory stats. Both use Mach kernel calls via CGo.
- **`internal/platform/sysctl.go`** -- `SysctlUint64()`, `SysctlUint32()`, `SysctlString()`, `GetSwapUsage()`.
- **`internal/platform/iokit.go`** -- IOKit helpers: `IOKitGetMatchingService`, `IOKitIterateMatching`, `CFDict` wrapper with `GetInt64`/`GetDict`/`GetBool`/`GetString` methods. All CF type conversions happen in C helper functions to avoid CGo type-casting issues.
- **`internal/smc/smc.go`** -- SMC connection open/close, `ReadFloat` (handles `flt ` float32 and `sp78` fixed-point), `ReadKey` for raw bytes. Uses `IOConnectCallStructMethod` with selectors 5 (read) and 9 (get key info).
- **`internal/smc/keys.go`** -- Apple Silicon sensor key registry (13 known keys).
- **`internal/collector/collector.go`** -- `Collector` interface.
- **`internal/collector/cpu.go`** -- CPU collector with P/E core detection via `hw.perflevel{0,1}.logicalcpu` sysctl, delta tick computation.
- **`internal/collector/gpu.go`** -- GPU collector reading `IOAccelerator` -> `PerformanceStatistics` -> `Device Utilization %`.
- **`internal/collector/memory.go`** -- Memory collector via `host_statistics64` + sysctl swap.
- **`internal/collector/network.go`** -- Network collector parsing `NET_RT_IFLIST2` sysctl buffer, delta computation for rates, top-5 by traffic, loopback filtered.
- **`internal/collector/disk.go`** -- Disk collector: capacity via `syscall.Statfs`, I/O via `IOBlockStorageDriver` Statistics, delta for throughput.
- **`internal/collector/power.go`** -- Power collector via `IOPSCopyPowerSourcesInfo` (done in C helper to avoid CF type issues) + `AppleSmartBattery` for voltage/amperage/wattage.
- **`internal/collector/temperature.go`** -- Temperature collector with lazy SMC connection, reads all known sensor keys and skips unavailable ones.
- **`internal/ui/app.go`** -- Bubbletea Model with tick-based refresh, help toggle, graceful quit with SMC cleanup.
- **`internal/ui/styles.go`** -- Lipgloss adaptive styles (light/dark), progress bar renderer.
- **`internal/ui/panels.go`** -- Panel render functions for each metric category.
- **`internal/ui/layout.go`** -- Two-column (>=100 cols) / single-column (<100 cols) adaptive layout.
- **`internal/ui/help.go`** -- Help overlay toggled with `?`.

### Key functions

- `platform.HostProcessorInfo()` -- Returns `[]CPUTicks` from Mach `host_processor_info()`.
- `platform.IOKitIterateMatching(className, fn)` -- Iterates IOKit services, calls `fn(*CFDict)` for each.
- `smc.Connection.ReadFloat(key)` -- Reads a temperature value from SMC by 4-char key.
- `collector.CPUCollector.Collect()` -- Computes per-core utilization via tick deltas.
- `ui.NewModel(interval, version, verbose)` -- Creates the bubbletea Model with all collectors.

## Deviations from architecture doc

1. **`IOKitIterateMatching` callback signature**: The architecture doc shows callbacks receiving raw `io_registry_entry_t`. Because CGo types are package-scoped (each package has its own `C.io_registry_entry_t`), the callback instead receives `*platform.CFDict` with properties already read. This avoids cross-package CGo type leakage.

2. **`CFDict` wrapper type**: The architecture doc uses raw `CFDictionaryRef`/`CFMutableDictionaryRef` across packages. The implementation wraps these in a Go `CFDict` struct with methods, and pushes all CF type casting into C helper functions. This is necessary because CGo on modern macOS sometimes represents CF types as pointers and sometimes as uintptrs, making direct Go-level casting unreliable.

3. **`mach_task_self()` wrapper**: The Mach macro `mach_task_self()` is not directly accessible from CGo. A static C function `get_mach_task_self()` wraps it in both `platform/mach.go` and `smc/smc.go`.

4. **Power collector C helper**: The `get_power_source_info()` function is implemented entirely in C (in the CGo preamble of `power.go`) rather than mixing Go and C calls. This avoids CF type casting issues with `IOPSCopyPowerSourcesInfo`/`IOPSCopyPowerSourcesList` which return CF types that would need cross-type casts in Go.

5. **`lipgloss.SetColorProfile(0)`** is used for `--no-color` instead of `lipgloss.SetHasDarkBackground` which the doc did not specify. This is the standard lipgloss approach to disable color.

## Assumptions

- **Apple Silicon layout**: P-cores are indexed before E-cores in the `host_processor_info` output. This matches observed behavior on M1-M4 chips.
- **`vm.swapusage` sysctl MIB**: Uses `{CTL_VM, 55}` where 55 is `VM_SWAPUSAGE`. This is a stable value on macOS but is technically not in public headers.
- **Network interface limit**: Capped at 64 interfaces max, top 5 shown by total traffic.
- **SMC struct layout**: The `SMCKeyData_t` struct layout matches the reverse-engineered format used by osx-cpu-temp and similar tools. A 2-byte padding field is included after `dataType` to match the kernel driver's expected alignment.

## Known limitations

- **No peak value tracking**: The `r` (reset peaks) keybinding is wired but peak tracking is not yet implemented.
- **No `--all-interfaces` flag**: Architecture doc mentions this; not implemented. Top 5 interfaces by traffic are shown.
- **No `powermetrics` fallback**: If SMC access fails or GPU stats are unavailable, the tool shows "N/A" but does not fall back to parsing `powermetrics` output (which requires root).

## Build requirements

- Go 1.21+ (tested with 1.24)
- macOS with Xcode command-line tools (`xcode-select --install`)
- CGO_ENABLED=1 (required for IOKit/CoreFoundation access)

## How to build and run

```bash
# Build
make build
# or directly:
CGO_ENABLED=1 go build -o bin/mactop ./cmd/mactop

# Run
./bin/mactop

# With options
./bin/mactop -i 500ms      # 500ms refresh
./bin/mactop --no-color     # disable colors
./bin/mactop --version      # print version
./bin/mactop -v             # enable verbose logging to mactop-debug.log
```

---

## Review Fixes (2026-03-27)

Fixes applied from code review, security review, and performance review.

### CRITICAL / HIGH Priority

1. **Mach port leak** (`internal/platform/mach.go`) -- Cached `mach_host_self()` in a package-level `hostPort` variable initialized once in `init()`. Both `HostProcessorInfo()` and `HostVMInfo64()` now use the cached port.

2. **Network/Disk delta wraparound** (`internal/collector/network.go`, `internal/collector/disk.go`) -- Rewrote delta computation to check `current >= prev` before subtracting, eliminating reliance on unsigned wraparound behavior.

3. **CPU tick wraparound** (`internal/collector/cpu.go`) -- Added guard that skips the delta computation for a core if any tick counter has wrapped (current < prev).

4. **SMC memcpy bounds** (`internal/smc/smc.go`, C function `smcReadKey`) -- Added `if (dataSize > sizeof(output.bytes)) dataSize = sizeof(output.bytes);` before `memcpy`.

5. **Network buffer parser bounds checks** (`internal/collector/network.go`, C function `parse_iflist2`) -- Added three bounds checks: remaining buffer >= `sizeof(struct if_msghdr)`, `ifm_msglen >= sizeof(struct if_msghdr)`, and `ifm_msglen >= sizeof(struct if_msghdr2)` before the RTM_IFINFO2 cast.

6. **SMC key info caching** (`internal/smc/smc.go`) -- Added `keyInfoCache` struct and `keyCache` map on `Connection`. After first successful `smcGetKeyInfo`, subsequent reads use cached type/size. Also added pure-Go `encodeKey()` to eliminate CString + smcKeyEncode CGo calls. Reduces per-sensor CGo calls from 5 to 1.

### MEDIUM Priority

7. **Cache hw.memsize** (`internal/collector/memory.go`) -- Read once in `NewMemoryCollector()`, stored on struct.

8. **Makefile GOARCH** (`Makefile`) -- Changed to `GOARCH ?= $(shell go env GOARCH)`.

9. **SwapUsage struct** (`internal/platform/sysctl.go`) -- Now uses `C.struct_xsw_usage` with proper field access (`xsu_total`, `xsu_avail`, `xsu_used`).

10. **SMC cleanup** (`cmd/mactop/main.go`, `internal/ui/app.go`) -- Added `Model.Close()` method and `defer model.Close()` in main for cleanup on any exit path.

11. **Verbose flag** (`internal/ui/app.go`) -- Implemented with `tea.LogToFile("mactop-debug.log", "mactop")`. Collector errors are logged via `log.Logger` when `-v` is set. Also cached `intervalStr` to avoid `fmt.Sprintf` per render.

### LOW Priority

12. **Degree symbol** (`internal/ui/panels.go`) -- Changed `"%.1f C"` to `"%.1f \u00b0C"`.

13. **Pre-allocate sensors** (`internal/collector/temperature.go`) -- `make([]metrics.TempSensor, 0, len(smc.AppleSiliconSensors))`.

14. **strings.Repeat** (`internal/ui/styles.go`) -- Replaced custom `repeatChar` with `strings.Repeat`. Removed dead function.

15. **GPU multi-match** (`internal/collector/gpu.go`) -- Returns `errGPUFound` sentinel after first successful match; sentinel is filtered out in `Collect()`.

### Deviations from task instructions

- **GPU break**: Used a sentinel error pattern instead of a loop break, since `IOKitIterateMatching` uses a callback. The sentinel (`errGPUFound`) is a package-level `errors.New` value compared by identity.

### Assumptions

- `mach_host_self()` returns a port valid for the process lifetime (guaranteed by macOS).
- SMC key info (type/size) is immutable per key per connection.
- `C.struct_xsw_usage` is available on all supported macOS versions.

### Limitations introduced

- `tea.LogToFile` opens a file that is not explicitly closed (OS reclaims on exit). Acceptable for a debug log.

---

## Bug Fixes (2026-03-27) -- Network Collector and Temperature Collector

### Bug 1: Network collector loop exits early (`internal/collector/network.go`)

**Root cause**: The `parse_iflist2` C function checked `ifm->ifm_msglen < sizeof(struct if_msghdr)` and broke the entire loop when it encountered non-IFINFO routing messages (e.g., `RTM_NEWADDR` with `ifa_msghdr`, which is ~20 bytes vs ~200 for `if_msghdr`). This skipped all subsequent IFINFO2 messages after the first non-IFINFO message.

**Fix**: Changed the loop to read only the common 4-byte routing message header (`u_short msglen`, `u_char version`, `u_char type`) before deciding what to do. The `sizeof(struct if_msghdr2)` check now only applies when `msgtype == RTM_IFINFO2`. Non-IFINFO messages are skipped by advancing `ptr += msglen` without breaking.

### Bug 2: SMC struct size mismatch (`internal/smc/smc.go`)

**Root cause**: The `SMCKeyData_t` struct was 52 bytes but the macOS kernel driver expects 80 bytes. The flat struct layout did not match the kernel's sub-struct layout with alignment padding, causing `IOConnectCallStructMethod` to fail with `kIOReturnIPCError` (0xe00002c7).

**Fix**: Replaced the flat struct with the correct sub-struct layout matching the kernel driver:
- `SMCKeyData_vers_t` (6 bytes)
- `SMCKeyData_pLimitData_t` (16 bytes)
- `SMCKeyData_keyInfo_t` (12 bytes with padding)
- Total `SMCKeyData_t`: 80 bytes

Changed `dataSize` from `uint8` to `uint32` throughout (C functions, Go `keyInfoCache` struct, and all call sites) since `keyInfo.dataSize` is `unsigned int` in the correct struct.

Added two new C functions: `smcGetKeyCount` (selector 7) and `smcGetKeyAtIndex` (selector 8) for key enumeration.

### Temperature sensor discovery (`internal/smc/smc.go`, `internal/collector/temperature.go`)

Added `DiscoverTempSensors()` method on `Connection` that enumerates all SMC keys starting with 'T' that return float/sp78 values in the 0-150C range. The `TempCollector` now tries auto-discovery on first collect; falls back to the hardcoded `AppleSiliconSensors` list if enumeration is not available (e.g., macOS 26+ restricts SMC key access).

### Expanded sensor key list (`internal/smc/keys.go`)

Added 15 additional sensor keys covering more Apple Silicon models: extra P-cores (5-6), extra E-cores (3-4), CPU die/proximity variants, extra GPU sensors, SSD 2, ambient temperature, and NAND.

### Files changed

- `/internal/collector/network.go` -- Fixed C `parse_iflist2` loop
- `/internal/smc/smc.go` -- Replaced C struct, updated Go types, added `DiscoverTempSensors`
- `/internal/smc/keys.go` -- Expanded sensor key list to 28 keys
- `/internal/collector/temperature.go` -- Added discovery-first logic with hardcoded fallback

### Assumptions

- The 4-byte routing message header layout (`u_short msglen` at offset 0, `u_char type` at offset 3) is stable across macOS versions.
- `SMCKeyData_t` at 80 bytes with sub-struct alignment matches the kernel driver on macOS 13-26.
- On macOS 26+, `smcGetKeyCount` may return 0 due to new OS-level SMC restrictions; the hardcoded fallback handles this gracefully.

---

## IOHIDEventSystem Temperature Source (2026-03-27)

Added IOHIDEventSystem-based temperature reading as the primary temperature source, with SMC as fallback. The IOHIDEventSystem private API works on macOS 26 where direct SMC access is blocked.

### Files changed

- **`internal/platform/iohid.go`** (new) -- Wraps the IOHIDEventSystem private API via CGo. Exports `HIDThermalReader` with `NewHIDThermalReader()`, `ReadTemperatures()`, and `Close()`. All private API calls are wrapped in static C functions in the CGo preamble to keep Go code clean. Deduplicates sensors by name (keeps first occurrence) and filters to 0-150C range.

- **`internal/collector/temperature.go`** (rewritten) -- Now tries HID first, SMC second. On first `Collect()`, creates `HIDThermalReader` and reads sensors. If sensors are found, locks to HID source (`useHID` flag). If no sensors, falls back to SMC (existing behavior). Subsequent calls skip source detection. Includes `buildHIDMetrics()` which aggregates raw HID sensors into display-friendly output:
  - Average and max of all "PMU tdie*" sensors shown as "CPU Die Avg (N)" and "CPU Die Max"
  - "gas gauge battery" mapped to "Battery" (first only)
  - "NAND CH0 temp" mapped to "SSD" (first only)
  - "PMU tcal" mapped to "CPU Calibrated" (first only)

### Key functions

- `platform.NewHIDThermalReader()` -- Creates IOHIDEventSystemClient once; reused across calls.
- `platform.HIDThermalReader.ReadTemperatures()` -- Sets matching filter, copies services, reads temperature events. Returns `[]HIDTempSensor`.
- `collector.buildHIDMetrics()` -- Transforms raw HID sensor list into aggregated `TemperatureMetrics`.

### Deviations from spec

- None.

### Assumptions

- The IOHIDEventSystem private API symbols (`IOHIDEventSystemClientCreate`, `IOHIDEventSystemClientCopyServices`, etc.) are present in IOKit on macOS 15+. They have no public headers but are stable private API used by system utilities.
- `CFRelease` works on the opaque `IOHIDEventSystemClientRef` pointer (it is a CF-based object).
- Temperature sensors with "PMU tdie" prefix are CPU die sensors across all Apple Silicon chips.

### Limitations

- The private API declarations rely on symbols being present in the IOKit framework at link time. If Apple removes these symbols in a future release, the build will fail at link time (not silently at runtime).
- The sensor name mapping is based on observed names from a single test machine. Other hardware may report different names; unmapped names are currently not shown in the aggregated view.

---

## Review Round 2 Fixes (2026-03-27)

Fixes from code-review-2, security-review-2, and performance-review-2.

### 1. Fix `buildHIDMetrics` max initialization (`internal/collector/temperature.go:200-206`)

`max` was initialized to `0.0`, which would produce an incorrect max if all die temps were negative. Now initialized to `dieTemps[0]` and the loop iterates over `dieTemps[1:]`. Updated the corresponding test (`TestBuildHIDMetrics_DieMaxCorrectWithNegatives`) to expect the correct value (`-3.0`) instead of the old buggy value (`0.0`).

### 2. SMC struct size compile-time assertion (`internal/smc/smc.go`)

Added `_Static_assert(sizeof(SMCKeyData_t) == 80, ...)` in the CGo preamble after the `SMCKeyData_t` typedef. This catches struct layout regressions at compile time rather than at runtime.

### 3. Move `hidSetTempMatching` to init (`internal/platform/iohid.go`)

Moved the `C.hidSetTempMatching(r.client)` call from `ReadTemperatures()` to `NewHIDThermalReader()`. The matching filter only needs to be set once; calling it every tick unnecessarily created and released CF objects (two `CFNumberCreate` + `CFDictionaryCreate` + three `CFRelease`) on every read cycle.

### 4. Cache HID sensor names (`internal/platform/iohid.go`)

Added `cachedNames []string` field to `HIDThermalReader`. On the first read, sensor names are populated via `hidGetProductName` CGo calls and stored. On subsequent reads, if the service count matches the cache length, names are reused from cache, skipping ~30 malloc+GoString+free CGo crossings per tick. If the count changes (unlikely but possible), names are re-read from scratch.

### 5. Remove dead `initialized` field (`internal/collector/temperature.go`)

Removed the unused `initialized bool` field from `TempCollector` and the `c.initialized = true` assignment.

### 6. Replace custom `errorString` with `errors.New` (`internal/platform/iohid.go`)

Replaced the custom `errorString` type and its `Error()` method with `errors.New("IOHIDEventSystem unavailable")`. Added `"errors"` import.

### 7. Add comment on Model struct (`internal/ui/app.go`)

Added documentation comment explaining the pointer-receiver pattern: all mutable collector fields must be pointers so value-receiver copies in bubbletea's Update loop share state with the original model.

### Files changed

- `internal/collector/temperature.go` -- Fixes 1, 5
- `internal/collector/temperature_test.go` -- Updated test for fix 1
- `internal/smc/smc.go` -- Fix 2
- `internal/platform/iohid.go` -- Fixes 3, 4, 6
- `internal/ui/app.go` -- Fix 7

---

## Time-Series Sparkline Graphs (2026-03-27)

Added braille-character sparkline graphs that display metric history over time, toggled with the `g` key. When enabled, graphs appear below the content of CPU, GPU, Memory, and Network panels.

### New files

- **`internal/ui/history.go`** -- `RingBuffer` (fixed-capacity circular buffer of float64), `MetricHistory` (holds ring buffers for CPU, GPU, Memory, NetIn, NetOut), and `MetricID` constants.
- **`internal/ui/history_test.go`** -- Unit tests for ring buffer push/wrap/values and MetricHistory recording.
- **`internal/ui/sparkline.go`** -- `RenderSparkline` function that maps data points to a braille dot grid (2 dots wide x 4 dots tall per terminal cell), with vertical line interpolation between consecutive points to avoid visual gaps.
- **`internal/ui/sparkline_test.go`** -- Unit tests for sparkline edge cases (empty data, zero dimensions, equal min/max, braille character output).

### Modified files

- **`internal/ui/app.go`** -- Added `history *MetricHistory` and `showGraphs bool` fields to `Model`. History is initialized in `NewModel` with capacity 256. `collectAll()` calls `history.Record(m.metrics)` after assembling the snapshot. Added `g` key handler in `Update()`.
- **`internal/ui/layout.go`** -- Updated `renderDashboard`, `renderWideLayout`, and `renderNarrowLayout` signatures to accept `*MetricHistory` and `showGraphs bool`. Status bar now includes `g: graphs`. When `showGraphs` is true, calls `*WithGraph` panel renderers.
- **`internal/ui/panels.go`** -- Added `renderCPUPanelWithGraph`, `renderGPUPanelWithGraph`, `renderMemoryPanelWithGraph`, `renderNetworkPanelWithGraph` wrapper functions. Added `appendSparkline` helper, `autoScaleMax`, and `formatBytesRate` utilities. Added `lipgloss` import.
- **`internal/ui/styles.go`** -- Added sparkline color variables: `sparkCPUColor` (39/cyan), `sparkGPUColor` (171/magenta), `sparkMemColor` (220/yellow), `sparkNetInColor` (40/green), `sparkNetOutColor` (208/orange).
- **`internal/ui/help.go`** -- Added `g` key to help text.
- **`internal/ui/layout_test.go`** -- Updated all `renderDashboard` calls to pass the two new arguments.

### Key functions

- `RingBuffer.Push(v)` / `RingBuffer.Values()` -- Circular buffer with O(1) push and O(n) chronological read.
- `MetricHistory.Record(m)` -- Extracts CPU aggregate, GPU utilization, memory %, net in/out totals and pushes to respective buffers.
- `RenderSparkline(data, opts)` -- Renders data as braille characters with line interpolation between consecutive points.
- `appendSparkline(base, label, data, ...)` -- Composes a labeled sparkline below existing panel content.

### Deviations from design doc

- The design doc used `barFilledColor` for all sparklines. Per the user requirement for distinct colors, each metric uses its own ANSI 256 color defined in `styles.go`.
- Graph wrapper functions accept a `graphHeight` parameter rather than hardcoding it, so layout functions pass 4 (wide) or 3 (narrow).
- The sparkline renderer fills vertical gaps between consecutive data points (line interpolation) to produce smoother-looking graphs. The design doc described only plotting individual dots.

### Assumptions

- The `g` key toggle defaults to OFF (graphs hidden), matching the design doc recommendation.
- Network graph labels use a compact format (`1.2M/s`) rather than the full `formatBytes` style to fit within the 12-character label width.
- The graph label uses `dimStyle` foreground color for visual consistency.

### Limitations

- Ring buffer capacity is fixed at 256. Terminals wider than 128 columns will not fill the full sparkline width until enough samples accumulate.
- Network graphs use linear auto-scaling. Bursty traffic may cause the graph to look jumpy as the scale adjusts.

---

## Review Round 3 Fixes -- Time-Series Graph Hardening (2026-03-27)

Eight fixes from code review of the sparkline/history feature.

### 1. Guard NewRingBuffer(0) (`internal/ui/history.go`)

Capacity <= 0 is now clamped to 1 to prevent panics from zero-length slice allocation (divide-by-zero on modulus in Push/Values).

### 2. Bounds-check MetricHistory.Get() (`internal/ui/history.go`)

Returns nil instead of panicking when MetricID is negative or >= metricCount.

### 3. Add math.IsInf check in sparkline (`internal/ui/sparkline.go`)

The NaN guard now also skips +Inf and -Inf values via `math.IsInf(v, 0)`.

### 4. Reduce allocation in Values() (`internal/ui/history.go`)

Replaced the per-element modulo loop with two `copy()` calls for the tail and head segments of the circular buffer. Added `ValuesInto(buf []float64) []float64` method that reuses a caller-provided buffer when its capacity is sufficient.

### 5. Flatten 2D sparkline grid (`internal/ui/sparkline.go`)

Replaced `[][]uint8` (Height+1 allocations) with a single flat `[]uint8` of size `Height * Width`, using `row*width + col` indexing.

### 6. Named constant for history capacity (`internal/ui/app.go`)

Replaced magic number `256` with `historyCapacity` constant. Updated all references in layout_test.go.

### 7. Cache lipgloss styles (`internal/ui/styles.go`, `internal/ui/panels.go`, `internal/ui/sparkline.go`)

- Added `sparklineStyle` (base style for sparkline rendering) and `graphLabelStyle` (for graph labels) to the package-level style variables in `styles.go`.
- Added `barBaseStyle` for progress bar rendering, replacing two `lipgloss.NewStyle()` calls per progress bar tick.
- `appendSparkline` in `panels.go` now uses `graphLabelStyle` instead of creating a new style each call. Removed the now-unused `lipgloss` import from `panels.go`.
- `RenderSparkline` in `sparkline.go` uses `sparklineStyle.Foreground(opts.Color)` instead of `lipgloss.NewStyle().Foreground(opts.Color)`.

### 8. Add showGraphs=true test path (`internal/ui/layout_test.go`)

Added `TestRenderDashboard_ShowGraphsWide` and `TestRenderDashboard_ShowGraphsNarrow` tests that call `renderDashboard` with `showGraphs=true` and a populated `MetricHistory` (20 samples across all metrics). Added `populatedHistory()` helper.

### Files changed

- `internal/ui/history.go` -- Fixes 1, 2, 4
- `internal/ui/sparkline.go` -- Fixes 3, 5, 7
- `internal/ui/app.go` -- Fix 6
- `internal/ui/styles.go` -- Fix 7
- `internal/ui/panels.go` -- Fix 7
- `internal/ui/layout_test.go` -- Fixes 6, 8

---

## Graph Layout Fixes and Temperature Graph (2026-03-27)

Three issues addressed: garbled network graphs, fragile side-by-side label+graph layout, and missing temperature graph.

### Issue 1: Network graph layout (`internal/ui/panels.go`)

Rewrote `renderNetworkPanelWithGraph` to not use `appendSparkline`. Instead it directly appends label lines above sparklines. Reduced network graph height to 2 rows each (from the full `graphHeight` of 3-4) to keep the panel compact. The graph width is now `width - 2` (full panel width minus small indent) instead of `width - graphLabelWidth`.

### Issue 2: Stacked label layout in `appendSparkline` (`internal/ui/panels.go`)

Replaced the side-by-side label+graph join logic (which split on newlines and concatenated line-by-line) with a simple stacked layout: label on its own line above the sparkline. This eliminates all ANSI escape sequence alignment issues. The `graphLabelWidth` constant was removed as it is no longer needed. CPU, GPU, and Memory graph widths updated from `width - graphLabelWidth` to `width - 2`.

### Issue 3: Temperature graph

**`internal/ui/history.go`** -- Added `MetricTemp` to the `MetricID` constants (before `metricCount`). Updated `Record()` to compute the max sensor temperature and push it.

**`internal/ui/styles.go`** -- Added `sparkTempColor` (ANSI 196 dark / 160 light, red/warm).

**`internal/ui/panels.go`** -- Added `renderTemperaturePanelWithGraph` function. Uses auto-scaling with `max * 1.2` and a floor of 30.0 degrees to avoid tiny scales. Label format: `"  Max 72.1 C"`.

**`internal/ui/layout.go`** -- Updated both `renderWideLayout` and `renderNarrowLayout` to call `renderTemperaturePanelWithGraph` when `showGraphs` is true.

### Files changed

- `internal/ui/panels.go` -- Rewrote `appendSparkline` and `renderNetworkPanelWithGraph`, added `renderTemperaturePanelWithGraph`, removed `graphLabelWidth`
- `internal/ui/history.go` -- Added `MetricTemp`, updated `Record()`
- `internal/ui/styles.go` -- Added `sparkTempColor`
- `internal/ui/layout.go` -- Wired temperature graph in both layouts

### Assumptions

- A floor of 30.0 degrees for temperature auto-scaling prevents the graph from having an excessively large scale when temperatures are very low.
- Temperature history records 0.0 when `Temperature.Available` is false or no sensors exist.

---

## Small Fixes -- Label, Unused Param, Dedup (2026-03-27)

Three small fixes to `internal/ui/panels.go` and `internal/ui/layout.go`.

### Fix 1: Temperature graph label says "Max" but shows latest value

The label `"  Max %.1f C"` was misleading. The value `data[len(data)-1]` is the latest reading (which represents the max across sensors at that tick), not the historical max. Changed to `"  Temp %.1f C"`.

- `internal/ui/panels.go` line 385

### Fix 2: Remove unused `graphHeight` parameter from `renderNetworkPanelWithGraph`

The function accepted `graphHeight int` but hardcoded `netHeight := 2`. Removed the parameter from the signature and updated all callers.

- `internal/ui/panels.go` -- function signature
- `internal/ui/layout.go` -- two call sites (wide and narrow layouts)
- `internal/ui/panels_test.go` -- three test call sites

### Fix 3: Deduplicate max-finding + scaling in temperature graph

The temperature graph had inline max-finding and scaling logic identical to `autoScaleMax` except for the floor value (30.0 vs 1024). Replaced `autoScaleMax` with `autoScaleMaxWithFloor(data []float64, floor float64)` and used it in both network (floor 1024) and temperature (floor 30.0) graphs.

- `internal/ui/panels.go` -- renamed function, added floor parameter, refactored temperature graph to call it
- `internal/ui/panels_test.go` -- updated all `autoScaleMax` test calls to `autoScaleMaxWithFloor(..., 1024)`
