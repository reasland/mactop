# Performance Review (Second Pass): mactop

## Review Date: 2026-03-27

**Scope**: Second-pass review following optimizations applied after the first review. Covers all 7 collectors (`internal/collector/`), platform CGo bindings (`internal/platform/`), SMC module (`internal/smc/`), IOHIDEventSystem integration (`internal/platform/iohid.go`), and TUI rendering (`internal/ui/`).

**Focus**: Verify that previously flagged issues were addressed, evaluate the new IOHID temperature path, recalculate CGo call counts, and identify any new issues or regressions.

---

## Previously Flagged Issues: Status

### Finding 1 (IOKit connections every tick) -- NOT FIXED

- **Original**: `IOKitIterateMatching` opens/queries/closes IOKit service handles every tick for Disk and GPU collectors.
- **Status**: The code at `internal/platform/iokit.go:217-249` is unchanged. `IOKitIterateMatching` still performs the full cycle every tick: `CString` alloc, `IOServiceGetMatchingServices`, iterate with `IOIteratorNext`, `IORegistryEntryCreateCFProperties` per entry, property lookups via `CString`-based helpers, and cleanup.
- **Impact remains**: HIGH. Still ~30-50 CGo calls for Disk, ~25-40 for GPU per tick.

### Finding 2 (SMC key info not cached) -- FIXED

- **Status**: `internal/smc/smc.go:148-159` introduces `keyInfoCache` struct and a `map[uint32]keyInfoCache` on the `Connection` struct. `ReadFloat` (line 207) and `ReadKey` (line 313) both check the cache before calling `smcGetKeyInfo`. The `encodeKey` function (line 163) is now pure Go, eliminating both the `CString` allocation and the `smcKeyEncode` CGo call.
- **Result**: On the first tick, 13 sensors require `smcGetKeyInfo` (1 CGo each). On all subsequent ticks, key info is served from the Go-side cache. Per-sensor CGo calls drop from 5 to 1 (`smcReadKey` only) after warmup.
- **Revised cost (SMC path, after first tick)**: 13 CGo calls per tick (1 per sensor, just `smcReadKey`). This is a 4x reduction from the original 65.

### Finding 3 (CPU slice re-creation) -- NOT FIXED

- **Status**: `internal/collector/cpu.go:49` still does `cores := make([]metrics.CoreUsage, numCPU)` every tick. Line 95 still does `c.prevTicks = make([]cpuTickSample, numCPU)` every tick. Neither is pre-allocated or reused.
- **Impact remains**: LOW (2 allocations per tick, ~1-2 KB).

### Finding 4 (Network buffer allocation) -- NOT FIXED

- **Status**: `internal/collector/network.go:109` still does `buf := make([]byte, bufLen)` every tick. Line 167 still creates a new `newPrev` map every tick. Line 124 still uses `var result []metrics.NetworkInterface` (nil slice with append growth).
- **Impact remains**: MEDIUM (1 buffer + 1 map + slice growth per tick).

### Finding 5 (CString allocations in IOKit dict lookups) -- NOT FIXED

- **Status**: `internal/platform/iokit.go:165-212` -- all `GetInt64`, `GetDict`, `GetBool`, `GetString` methods still create a `CString` and free it per call. The C-side `dict_get_value` helper (line 56) additionally creates and releases a `CFStringRef` per lookup.
- **Impact remains**: MEDIUM (~14-28 CGo crossings per tick for repeated key lookups).

### Finding 6 (Power collector opens AppleSmartBattery every tick) -- NOT FIXED

- **Status**: `internal/collector/power.go:117` still calls `platform.IOKitGetMatchingService("AppleSmartBattery")` every tick via `readSmartBattery()`. The service handle is obtained, properties read, and both released within the same call.
- **Impact remains**: MEDIUM (~12 CGo crossings per tick).

### Finding 7 (hw.memsize caching) -- FIXED

- **Status**: `internal/collector/memory.go:11-19` -- `NewMemoryCollector` reads `hw.memsize` once at init time and stores it in `c.totalMem`. `Collect()` (line 31) uses the cached value, only falling back to a sysctl call if the init-time read failed.
- **Result**: Eliminates 3 CGo crossings per tick.

### Finding 8 (progressBar style allocation) -- PARTIALLY FIXED

- **Status**: `internal/ui/styles.go:52-54` pre-defines `barFilledColor`, `barEmptyColor`, and `barWarnColor` as package-level `lipgloss.AdaptiveColor` values. The `repeatChar` custom function has been replaced with `strings.Repeat` (lines 77, 80).
- **Still present**: Lines 76-81 still call `lipgloss.NewStyle().Foreground(...).Render(...)` twice per `progressBar` invocation, creating 2 new style objects per call. With ~15-20 calls per render, this is still ~30-40 style allocations per tick. The styles could be pre-created at package level (only 3 variants needed: filled-normal, filled-warn, empty).
- **Impact remains**: LOW (~30-40 small allocations per render).

### Finding 9 (interval string formatting every render) -- FIXED

- **Status**: `internal/ui/app.go:38` adds `intervalStr string` to the `Model` struct. `NewModel` (line 57) sets it once via `fmt.Sprintf("%v", interval)`. `View()` (line 141) passes the cached string to `renderDashboard`.
- **Result**: Eliminates 1 `fmt.Sprintf` per render.

### Finding 10 (Temperature sensor slice growth) -- FIXED

- **Status**: `internal/collector/temperature.go:118` now does `sensors := make([]metrics.TempSensor, 0, len(c.sensorDefs))`, pre-allocating with the known sensor count.
- **Result**: Eliminates 3-4 slice growth allocations per tick in the SMC path.

### Finding 11 (mach_host_self caching) -- FIXED

- **Status**: `internal/platform/mach.go:23-27` caches the host port at `init()` time. Both `HostProcessorInfo` (line 43) and `HostVMInfo64` (line 94) use the cached `hostPort` variable.
- **Result**: Eliminates 2 CGo crossings per tick and prevents Mach port reference leaks.

---

## New Findings

### Finding N1: IOHIDEventSystem matching dictionary re-created every tick

- **Impact**: MEDIUM
- **Location**: `internal/platform/iohid.go:33-54` (`hidSetTempMatching`), called from `ReadTemperatures` at line 150.
- **Issue**: `ReadTemperatures` calls `C.hidSetTempMatching(r.client)` on every tick. Inside the C function, this:
  1. Creates 2 `CFNumberRef` values via `CFNumberCreate` (heap allocations in CF)
  2. Creates a `CFDictionaryRef` via `CFDictionaryCreate`
  3. Calls `IOHIDEventSystemClientSetMatching` to set the filter
  4. Releases all 3 CF objects

  The matching criteria never change -- they are always `PrimaryUsagePage=0xFF00` and `PrimaryUsage=0x05`. The matching dictionary could be created once and reused, or `SetMatching` could be called once at init time since the filter does not change between ticks.

  Additionally, `CFSTR("PrimaryUsagePage")` and `CFSTR("PrimaryUsage")` are compile-time constants (not allocated/freed), so those are free. But the two `CFNumberRef` and the `CFDictionaryRef` are dynamically allocated and released every tick for no benefit.

- **Current cost**: 3 CF object allocations + releases + 1 `IOHIDEventSystemClientSetMatching` call per tick. While these are C-side calls batched into a single CGo crossing (the Go side calls `C.hidSetTempMatching` once), the CF allocations are unnecessary.
- **Recommendation**: Call `hidSetTempMatching` once inside `NewHIDThermalReader` instead of on every `ReadTemperatures` call. The matching filter is static and persists on the client object. This eliminates the CF object churn entirely.
- **Trade-offs**: None. The matching criteria are constant.

### Finding N2: Per-sensor string allocation via malloc in HID path

- **Impact**: MEDIUM
- **Location**: `internal/platform/iohid.go:75-93` (`hidGetProductName`), called per sensor per tick at line 166.
- **Issue**: For each temperature sensor discovered by IOHID (typically 20-40 sensors on Apple Silicon), `hidGetProductName` is called, which:
  1. Calls `IOHIDServiceClientCopyProperty` (CGo crossing)
  2. Calls `malloc` to allocate a C buffer for the name string
  3. Calls `CFStringGetCString` to copy the name
  4. Calls `CFRelease` on the property

  Then in Go (lines 170-171):
  5. `C.GoString(cName)` -- copies the C string into a Go string (heap allocation)
  6. `C.free(unsafe.Pointer(cName))` -- frees the C buffer (CGo crossing)

  This is 2 CGo crossings (hidGetProductName + free) plus 1 Go heap allocation per sensor per tick. With 30 sensors, that is 60 CGo crossings and 30 string allocations per tick just for names that never change.

  The deduplication map `seen` (line 160) is also re-created fresh every tick: `make(map[string]struct{}, count)`. Since the sensor set is stable, the deduplication result could be cached.

- **Current cost**: ~60 CGo crossings + ~30 Go string allocations + 1 map allocation per tick for sensor name retrieval.
- **Recommendation**: Cache the sensor names on first read. The sensor product names do not change between ticks. On the first call, build a mapping from service index to name (or cache the deduplication result). On subsequent ticks, skip `hidGetProductName` entirely and only call `hidReadTemperature` for the already-known sensors. This would reduce per-tick CGo cost from ~90 (names + temps + frees) to ~30 (temps only).
- **Trade-offs**: Must handle the case where the sensor set changes (e.g., a Thunderbolt device is connected/disconnected). Could re-scan names every N ticks (e.g., every 30 seconds) or when the service count changes.

### Finding N3: IOHIDEventSystemClientCopyServices called every tick copies the full service array

- **Impact**: MEDIUM
- **Location**: `internal/platform/iohid.go:58-60` (`hidCopyServices`), called at line 152.
- **Issue**: `IOHIDEventSystemClientCopyServices` returns a new `CFArrayRef` containing all matching service client references. This is a "Copy" function (CF ownership rules), meaning it allocates a new array each time. The array itself is released via `defer C.hidReleaseArray(services)`, but the allocation and population happen every tick.

  Combined with Finding N2, the per-tick flow for the HID path is:
  1. `hidSetTempMatching` -- 1 CGo crossing, 3 CF alloc/free cycles (Finding N1)
  2. `hidCopyServices` -- 1 CGo crossing, 1 CFArray allocation
  3. `hidServiceCount` -- 1 CGo crossing
  4. Per sensor (N times, typically 20-40):
     - `hidServiceAtIndex` -- 1 CGo crossing
     - `hidGetProductName` -- 1 CGo crossing + malloc
     - `C.GoString` + `C.free` -- 1 CGo crossing
     - `hidReadTemperature` -- 1 CGo crossing + CFRelease
  5. `hidReleaseArray` -- 1 CGo crossing

  Total: **4 + 4N CGo crossings per tick** where N is typically 20-40, giving **84-164 CGo crossings per tick**.

- **Current cost**: 84-164 CGo crossings per tick for the HID temperature path. This is comparable to or worse than the original SMC path (65 CGo crossings for 13 sensors) in raw CGo crossing count, though it reads more sensors.
- **Recommendation**: Combine with Finding N2's caching approach. On subsequent ticks, if the service count is unchanged, skip name retrieval and deduplication entirely. The `CopyServices` call is unavoidable (needed to get current service references for temperature reads), but the per-service name lookup is not.
- **Trade-offs**: Minimal. The `CopyServices` call remains necessary for fresh temperature values.

### Finding N4: buildHIDMetrics creates intermediate slices and does string operations every tick

- **Impact**: LOW
- **Location**: `internal/collector/temperature.go:144-222` (`buildHIDMetrics`)
- **Issue**: Every tick, `buildHIDMetrics`:
  1. Creates `var result []metrics.TempSensor` (nil, grows via append)
  2. Creates `var dieTemps []float64` (nil, grows via append)
  3. Calls `strings.ToLower(s.Name)` per sensor -- allocates a new lowercase string
  4. Calls `strings.HasPrefix` and `strings.Contains` on the lowered string per sensor
  5. Creates a `dieSensors` slice literal (line 211) and does `append(dieSensors, result...)` which copies the entire result slice

  With ~30 raw sensors, this is ~30 `strings.ToLower` allocations plus slice growth.

  The `append(dieSensors, result...)` at line 215 is an O(N) copy to prepend 2 elements. A more efficient pattern would be to pre-allocate result with capacity and insert die metrics at indices 0-1.

- **Current cost**: ~30 string allocations + ~5-8 slice growth allocations per tick.
- **Recommendation**:
  - Pre-allocate `result` with a reasonable capacity (e.g., 6: die avg, die max, battery, ssd, calibrated, plus margin).
  - Pre-allocate `dieTemps` with a capacity matching expected die sensor count (e.g., 8).
  - Use `strings.EqualFold` or manual ASCII lowering for simple prefix checks to avoid allocating a lowered copy.
  - Reserve slots 0-1 in `result` for die metrics, then fill in at the end, avoiding the prepend copy.
- **Trade-offs**: Slightly less readable code for the prefix/contains checks.

### Finding N5: SMC ReadFloat still allocates goBytes slice per sensor per tick

- **Impact**: LOW
- **Location**: `internal/smc/smc.go:230-233` (`ReadFloat`), `internal/smc/smc.go:334-337` (`ReadKey`)
- **Issue**: Both `ReadFloat` and `ReadKey` allocate `goBytes := make([]byte, int(dataSize))` per call. For `ReadFloat` with temperature sensors, `dataSize` is always 2 (sp78) or 4 (flt). This is a tiny allocation per call, but it happens 13 times per tick in the SMC path.

  The byte-by-byte copy loop from `rawBytes` to `goBytes` could also be eliminated by reading directly from the C array:
  ```
  bits := uint32(rawBytes[0]) | uint32(rawBytes[1])<<8 | uint32(rawBytes[2])<<16 | uint32(rawBytes[3])<<24
  ```

- **Current cost**: 13 small heap allocations per tick (SMC path).
- **Recommendation**: Use a stack-allocated `[32]byte` array instead of `make([]byte, dataSize)`, then slice it for the `binary` functions. Or decode directly from the C array without copying.
- **Trade-offs**: None.

### Finding N6: collectAll creates a collector interface slice every tick

- **Impact**: LOW
- **Location**: `internal/ui/app.go:109-111`
- **Issue**: Every tick, `collectAll` creates a new `[]collector.Collector` slice literal containing 7 elements. This is a small heap allocation (7 interface values = 112 bytes on 64-bit) that could be pre-allocated on the `Model` struct.
- **Current cost**: 1 small allocation per tick.
- **Recommendation**: Store the collector slice on the `Model` struct, initialized once in `NewModel`.
- **Trade-offs**: None.

---

## Updated CGo Call Count Table (per tick)

### SMC Temperature Path (macOS <= 15)

| Collector       | Before Optimization | After Optimization | Notes |
|----------------|--------------------:|-------------------:|-------|
| CPU            | 3                   | 2                  | host_processor_info + vm_deallocate (mach_host_self cached) |
| Memory         | 9                   | 5                  | host_statistics64 + sysctl (swap, 2 calls) + vm_kernel_page_size access (hw.memsize cached) |
| Disk           | 30-50               | 30-50              | **Unchanged** -- IOKit iterate + property reads + CString allocs |
| GPU            | 25-40               | 25-40              | **Unchanged** -- IOKit iterate + property reads + CString allocs |
| Network        | 4-5                 | 4-5                | sysctl*2 + parse + if_indextoname per interface |
| Power          | 15-25               | 15-25              | **Unchanged** -- IOPowerSources + AppleSmartBattery IOKit |
| Temperature (SMC) | 65+              | 13                 | 1 smcReadKey per sensor (key info cached, encodeKey pure Go) |
| **Total (SMC)**| **~150-195**        | **~94-140**        | ~35% reduction |

### HID Temperature Path (macOS 26+)

| Collector       | Estimated CGo Calls | Notes |
|----------------|--------------------:|-------|
| CPU            | 2                   | host_processor_info + vm_deallocate |
| Memory         | 5                   | host_statistics64 + sysctl (swap) |
| Disk           | 30-50               | IOKit iterate (unchanged) |
| GPU            | 25-40               | IOKit iterate (unchanged) |
| Network        | 4-5                 | sysctl + parse + if_indextoname |
| Power          | 15-25               | IOPowerSources + AppleSmartBattery (unchanged) |
| Temperature (HID) | 84-164           | hidSetTempMatching + CopyServices + 4N per sensor (N=20-40) |
| **Total (HID)**| **~165-291**        | HID path is MORE expensive than optimized SMC path |

**Key observation**: The new HID temperature path (84-164 CGo crossings) is significantly more expensive than the optimized SMC path (13 CGo crossings). This is expected given that HID reads ~30 sensors vs SMC's 13, and each HID sensor requires name retrieval. However, the name retrieval is the main source of overhead and is fully cacheable (Finding N2), which would bring the HID path down to ~34-44 CGo crossings per tick (CopyServices + serviceCount + N*readTemperature + releaseArray).

---

## Summary

**Overall performance posture**: The optimizations from the first review addressed 5 of 11 findings (Findings 2, 7, 9, 10, 11) and partially addressed 1 more (Finding 8). The most impactful fix was the SMC key info cache (Finding 2), which reduced the temperature collector from 65 to 13 CGo calls per tick -- a 5x improvement. The mach_host_self caching (Finding 11) also fixed a subtle Mach port reference leak.

However, 5 findings remain unaddressed (1, 3, 4, 5, 6), and the new HID temperature path introduces 3 new medium-impact issues (N1, N2, N3). The IOKit-based collectors (Disk, GPU, Power) remain the largest unfixed source of per-tick overhead.

**Top 3 priorities for next optimization pass**:

1. **Cache HID sensor names (Findings N1 + N2 + N3)**. The HID temperature path does 84-164 CGo crossings per tick, but ~60 of those are for name retrieval that produces the same results every tick. Moving `hidSetTempMatching` to init and caching names would reduce this to ~34-44 CGo calls -- a 2-4x improvement that makes the HID path competitive with the optimized SMC path.

2. **Optimize IOKit property reads for Disk and GPU (Findings 1 + 5)**. These remain the largest unfixed source of overhead at 55-90 CGo crossings per tick combined. The key optimization is replacing `IORegistryEntryCreateCFProperties` (copies entire property dictionary) with `IORegistryEntryCreateCFProperty` (fetches one key) and pre-creating CFString keys to eliminate per-lookup CString allocations.

3. **Cache AppleSmartBattery service handle (Finding 6)**. At ~12 CGo crossings per tick, this is a quick win. Battery data changes slowly enough that even reducing collection frequency (e.g., every 5 ticks) would be acceptable.

**Benchmarking recommendation**: The original benchmarking advice from the first review still applies. No benchmarks were found in the codebase. Before the next round of optimizations, establish baselines with:

```
go test -bench=BenchmarkCollectAll -benchmem -count=5
```

The HID vs SMC temperature path comparison is particularly important to benchmark, since the CGo call count analysis suggests the HID path may be 2-3x slower despite reading more sensors. Real-world timing would confirm whether the CGo crossing count is the dominant cost or whether the IOKit kernel round-trips (present in both paths) dominate instead.
