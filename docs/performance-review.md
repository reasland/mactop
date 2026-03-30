# Performance Review: mactop

## Review Date: 2026-03-27

**Scope**: Full codebase review of all 7 collectors (`internal/collector/`), platform CGo bindings (`internal/platform/`), SMC connection management (`internal/smc/`), and TUI rendering (`internal/ui/`).

**Focus**: Per-tick overhead in a 1-second refresh loop. Each tick calls 7 collectors sequentially and then renders the full dashboard.

---

## Findings

### Finding 1: IOKit connections opened and closed every tick for Disk and GPU collectors

- **Impact**: HIGH
- **Location**: `internal/platform/iokit.go:217-249` (`IOKitIterateMatching`), called from `internal/collector/disk.go:48` and `internal/collector/gpu.go:22`
- **Issue**: `IOKitIterateMatching` is called every tick by both the Disk and GPU collectors. Each call performs the following CGo/IOKit operations:
  1. `C.CString` allocation + `C.free` (1 CGo call each)
  2. `C.iokit_get_matching_services` -- creates a matching dictionary, queries the IOKit registry
  3. `C.IOIteratorNext` in a loop (1 CGo call per matching service)
  4. `C.iokit_get_properties` per service (creates a full CFDictionary copy of all properties)
  5. `C.IOObjectRelease` per service entry
  6. `C.cf_release_dict` per property dictionary
  7. Dictionary key lookups via `GetInt64`/`GetDict` -- each calls `C.CString`, `C.free`, and `C.dict_get_int64` (3 CGo calls per key lookup)
  8. `C.IOObjectRelease` for the iterator

  For the Disk collector iterating over `IOBlockStorageDriver` entries, with N matching services and 2 key lookups per service (plus 1 `GetDict` call), this is roughly: **2 + N*(1 + 1 + 1) + N*(3*3) = 2 + 12N CGo calls per tick** just for disk. The GPU collector similarly does about **2 + 15N CGo calls** (3 key lookups on the `PerformanceStatistics` sub-dict plus the `GetDict` call). With even 2-3 matching services, this totals 30-50 CGo calls per collector per tick.

  Additionally, `IORegistryEntryCreateCFProperties` is an expensive IOKit operation that copies the entire property dictionary from the kernel into userspace on every tick.

- **Current cost**: ~60-100 CGo crossings per tick across both collectors, plus IOKit kernel round-trips and CF dictionary allocations/deallocations.
- **Recommendation**: Cache the `io_connect_t` or service handles and reuse them across ticks. For properties that change (like I/O counters and GPU utilization), use `IORegistryEntryCreateCFProperty` (singular) to fetch only the specific key needed rather than copying the entire property dictionary. Pre-encode the CFString keys once at init time rather than creating/freeing CStrings on every lookup.
- **Trade-offs**: Requires managing connection lifecycle and handling stale service handles (services can disappear). Adds complexity to error recovery.

---

### Finding 2: SMC ReadFloat makes 2 CGo round-trips per sensor key (GetKeyInfo + ReadKey)

- **Impact**: HIGH
- **Location**: `internal/smc/smc.go:140-194` (`ReadFloat`), called from `internal/collector/temperature.go:33-48`
- **Issue**: `ReadFloat` is called once per sensor in `AppleSiliconSensors` (13 sensors defined in `keys.go`). Each call:
  1. `C.CString` + `C.free` for the key string (2 CGo calls)
  2. `C.smcKeyEncode` (1 CGo call)
  3. `C.smcGetKeyInfo` -- IOKit `IOConnectCallStructMethod` kernel round-trip (1 CGo call)
  4. `C.smcReadKey` -- IOKit `IOConnectCallStructMethod` kernel round-trip (1 CGo call)
  5. `make([]byte, dataSize)` -- heap allocation per key
  6. Byte-by-byte copy loop from C array to Go slice

  This totals **5 CGo calls + 1 heap allocation per sensor**, and **65 CGo calls + 13 heap allocations per tick** for the temperature collector alone. The `smcGetKeyInfo` call is particularly wasteful because the key info (data type and size) never changes for a given SMC key -- it could be cached after the first call.

- **Current cost**: 65 CGo crossings and 13 allocations per tick for temperature collection. Each `IOConnectCallStructMethod` is a Mach IPC kernel trap, far more expensive than the ~100ns CGo overhead.
- **Recommendation**: Cache key info (data type and size) in a `map[string]keyInfo` after the first successful `smcGetKeyInfo` call. This cuts the per-sensor CGo calls from 5 to 3 (CString, free, and ReadKey). Also pre-encode the 4-char keys to `uint32` at init time using Go code (trivial bit-shifting) to eliminate `smcKeyEncode` CGo calls and the CString allocation entirely, bringing it down to 1 CGo call per sensor (just `smcReadKey`). For the byte copy, use `C.GoBytes` or copy directly from the C array without a `make`.
- **Trade-offs**: Cached key info must be invalidated if the SMC connection is re-established. Minimal complexity increase.

---

### Finding 3: Slice re-creation every tick in CPUCollector

- **Impact**: MEDIUM
- **Location**: `internal/collector/cpu.go:49` and `internal/collector/cpu.go:86`
- **Issue**: Two slices are allocated every tick:
  1. `cores := make([]metrics.CoreUsage, numCPU)` (line 49) -- a slice of structs, each 56 bytes (5 float64s + 1 int + 1 bool). For a 12-core M2 Pro, this is ~672 bytes.
  2. `c.prevTicks = make([]cpuTickSample, numCPU)` (line 86) -- a new slice for delta state that could be reused.

  The `prevTicks` slice is the more significant issue: it has the same length every tick and is replaced wholesale rather than updated in-place.

- **Current cost**: 2 heap allocations per tick, ~1-2 KB.
- **Recommendation**: Pre-allocate `prevTicks` in `NewCPUCollector` and reuse it. For `cores`, either pre-allocate on the `CPUCollector` struct or write directly into `c.Data.Cores` after ensuring it has the right length.
- **Trade-offs**: Minor. The struct becomes slightly more stateful but the pattern is straightforward.

---

### Finding 4: Network collector allocates a new sysctl buffer every tick

- **Impact**: MEDIUM
- **Location**: `internal/collector/network.go:101`
- **Issue**: `buf := make([]byte, bufLen)` allocates a new byte slice every tick to hold the `NET_RT_IFLIST2` sysctl output. The buffer size is queried first (line 96-98) and then allocated. The interface list size is relatively stable between ticks (interfaces rarely appear/disappear), so this buffer could be reused.

  Additionally, `result` (line 116) uses `append` from a nil slice, causing 1-5 grow-and-copy operations. And `newPrev` map (line 160) is re-created every tick rather than cleared and reused.

- **Current cost**: 1 buffer allocation (~4-16 KB depending on interface count) + 1 map allocation + slice growth allocations per tick.
- **Recommendation**: Store the buffer on the `NetworkCollector` struct and grow it only when `bufLen` exceeds the current capacity. Pre-allocate the `result` slice with capacity matching the previous tick's count. Reuse the `prev` map by deleting stale entries rather than creating a new one.
- **Trade-offs**: The collector struct holds onto the buffer between ticks, consuming memory even when not collecting. Negligible for this buffer size.

---

### Finding 5: CString allocations in IOKit dictionary lookups on every call

- **Impact**: MEDIUM
- **Location**: `internal/platform/iokit.go:165-174` (`GetInt64`), `internal/platform/iokit.go:178-187` (`GetDict`), and all similar `Get*` methods
- **Issue**: Every `GetInt64`, `GetDict`, `GetBool`, and `GetString` call creates a `C.CString` and frees it. These are called with the same string keys on every tick (e.g., `"Statistics"`, `"Bytes (Read)"`, `"Bytes (Write)"`, `"Device Utilization %"`, `"PerformanceStatistics"`, etc.). Each `C.CString` call is a CGo crossing that allocates memory in C heap and copies the string, followed by `C.free`.

  Across the Disk collector (3 key lookups per service, 2+ services) and GPU collector (4 key lookups per service), this adds roughly **14-28 CGo calls** per tick just for CString creation and freeing.

- **Current cost**: ~14-28 unnecessary CGo crossings per tick for repeated string conversions.
- **Recommendation**: Create a `CFStringRef` cache: convert the known key strings to `CFStringRef` once at init time (using `CFStringCreateWithCString`) and store them in a map. Pass pre-created `CFStringRef` values to the dict lookup helpers instead of converting from Go strings each time.
- **Trade-offs**: The cached `CFStringRef` values must be released on shutdown. Adds a small global cache, but the set of keys is fixed and small.

---

### Finding 6: Power collector opens and closes IOKit service handle every tick for AppleSmartBattery

- **Impact**: MEDIUM
- **Location**: `internal/collector/power.go:116-140` (`readSmartBattery`)
- **Issue**: `readSmartBattery` calls `platform.IOKitGetMatchingService("AppleSmartBattery")` every tick. This:
  1. Allocates a CString for "AppleSmartBattery"
  2. Creates a matching dictionary via `IOServiceMatching`
  3. Queries the IOKit registry via `IOServiceGetMatchingService`
  4. Opens registry entry properties via `IORegistryEntryCreateCFProperties`
  5. Performs 2 dict lookups (Voltage, Amperage) -- 4 more CGo calls for CStrings
  6. Releases the service and properties

  The `CFTypeRef info` and `CFArrayRef list` from the power source query (lines 26-29 of the C code in `power.go`) are also created and released every tick via `IOPSCopyPowerSourcesInfo` and `IOPSCopyPowerSourcesList`.

- **Current cost**: ~12 CGo crossings plus IOKit registry queries per tick.
- **Recommendation**: Cache the `io_service_t` for AppleSmartBattery similarly to how the SMC connection is cached in the TempCollector. Open it lazily and reuse across ticks. For the battery percent/charging info from IOPowerSources, there is no way to avoid the query, but it could be collected at a lower frequency (e.g., every 5 ticks) since battery state changes slowly.
- **Trade-offs**: Must handle the case where the service disappears (e.g., Thunderbolt battery accessories). Reduced frequency means slightly stale battery data (acceptable for a 5-second lag).

---

### Finding 7: MemoryCollector calls SysctlUint64("hw.memsize") every tick

- **Impact**: LOW
- **Location**: `internal/collector/memory.go:27`
- **Issue**: `hw.memsize` is a static value -- total physical memory never changes during runtime. Each call involves `C.CString` + `C.free` + `C.sysctlbyname` (3 CGo crossings).
- **Current cost**: 3 unnecessary CGo crossings per tick.
- **Recommendation**: Read `hw.memsize` once in `NewMemoryCollector` and cache the value.
- **Trade-offs**: None.

---

### Finding 8: progressBar allocates a byte slice and creates 2 new lipgloss styles per call

- **Impact**: LOW
- **Location**: `internal/ui/styles.go:54-80` (`progressBar`) and `internal/ui/styles.go:82-91` (`repeatChar`)
- **Issue**: `progressBar` is called once per CPU core, once for CPU aggregate, once for GPU, once for memory, and once per disk volume -- roughly 15-20 times per tick. Each call:
  1. Creates 2 new `lipgloss.NewStyle()` objects (lines 72-77)
  2. Calls `repeatChar` twice, each allocating a `[]byte` via `make` and converting to string
  3. Concatenates the two styled strings

  `lipgloss.NewStyle()` itself allocates. Over 20 calls, this produces ~60 small allocations per render.

- **Current cost**: ~60 small heap allocations per render.
- **Recommendation**: Pre-create the filled and empty bar styles as package-level variables (they only vary between normal and warning colors -- 2 filled styles and 1 empty style). Use `strings.Repeat` instead of the custom `repeatChar` (it uses `strings.Builder` internally and is marginally more efficient for single-byte patterns). Alternatively, pre-build bar strings for common widths.
- **Trade-offs**: Minimal. The styles are already nearly package-level; just needs a small refactor.

---

### Finding 9: View() calls fmt.Sprintf for interval string every render

- **Impact**: LOW
- **Location**: `internal/ui/app.go:122`
- **Issue**: `fmt.Sprintf("%v", m.interval)` is called every time `View()` is invoked (every tick). The interval never changes.
- **Current cost**: 1 unnecessary `fmt.Sprintf` allocation per tick.
- **Recommendation**: Cache the interval string in the `Model` struct at init time.
- **Trade-offs**: None.

---

### Finding 10: Temperature collector re-creates sensors slice every tick

- **Impact**: LOW
- **Location**: `internal/collector/temperature.go:32-49`
- **Issue**: `var sensors []metrics.TempSensor` starts as nil and uses `append` to grow. With 13 sensor definitions and most succeeding, this causes multiple grow-and-copy cycles (typically 3-4 allocations: cap 1, 2, 4, 8, 16). The number of sensors is bounded and known.
- **Current cost**: 3-4 slice growth allocations per tick.
- **Recommendation**: Pre-allocate with `make([]metrics.TempSensor, 0, len(smc.AppleSiliconSensors))`.
- **Trade-offs**: None.

---

### Finding 11: mach_host_self() called without caching the port

- **Impact**: LOW
- **Location**: `internal/platform/mach.go:34` and `internal/platform/mach.go:85`
- **Issue**: `C.mach_host_self()` is called in both `HostProcessorInfo` and `HostVMInfo64` on every tick. While `mach_host_self()` is cheap (returns a send right to the host port), it does increment a reference count that should technically be deallocated. More importantly, the return value never changes.
- **Current cost**: 2 CGo crossings per tick, plus minor Mach port reference count leaks.
- **Recommendation**: Cache the host port at package init time. Call `mach_host_self()` once and reuse.
- **Trade-offs**: The cached port must remain valid for the process lifetime (it always does for `mach_host_self`).

---

### Finding 12: Disk collector creates single-element VolumeInfo slice every tick

- **Impact**: LOW
- **Location**: `internal/collector/disk.go:35-42`
- **Issue**: `c.Data.Volumes = []metrics.VolumeInfo{...}` creates a new single-element slice literal every tick. This is a minor allocation that could be avoided by reusing the existing slice.
- **Current cost**: 1 small allocation per tick.
- **Recommendation**: Pre-allocate `Volumes` with capacity 1 and reuse via `c.Data.Volumes = c.Data.Volumes[:1]` then update in place.
- **Trade-offs**: None.

---

## CGo Call Count Summary (per tick)

| Collector       | Estimated CGo Calls | Notes |
|----------------|--------------------:|-------|
| CPU            | 3                   | host_processor_info + vm_deallocate + mach_host_self |
| Memory         | 9                   | host_statistics64 + mach_host_self + sysctl*3 (memsize) + sysctl (swap) |
| Disk           | 30-50               | IOKit iterate + property reads + CString allocs |
| GPU            | 25-40               | IOKit iterate + property reads + CString allocs |
| Network        | 4-5                 | sysctl*2 + parse (C-side) + if_indextoname per interface |
| Power          | 15-25               | IOPowerSources + AppleSmartBattery IOKit + CString allocs |
| Temperature    | 65+                 | 5 CGo calls x 13 sensors |
| **Total**      | **~150-195**        | Per tick, 1-second interval |

The temperature and IOKit-based collectors (Disk, GPU, Power) dominate CGo overhead.

---

## Summary

**Overall performance posture**: The codebase is well-structured and functionally correct. For a 1-second refresh interval, the current overhead is likely acceptable on modern Apple Silicon hardware (total per-tick cost is probably under 5ms). However, there is significant room to reduce CGo crossings and heap allocations, which would matter if users set shorter refresh intervals (the minimum is 250ms) or if the tool is used on resource-constrained systems.

**Top 3 priorities**:

1. **Cache SMC key info in the temperature collector** (Finding 2). This is the single largest source of unnecessary CGo calls (65+ per tick) and the easiest to fix. Caching key info and pre-encoding keys to uint32 in Go would cut this to ~13 CGo calls per tick.

2. **Optimize IOKit property reads for Disk and GPU collectors** (Finding 1). Replace `IORegistryEntryCreateCFProperties` (full dictionary copy) with `IORegistryEntryCreateCFProperty` (single key fetch) and pre-create CFString keys. This would eliminate ~40-60 CGo crossings and avoid copying large property dictionaries from the kernel.

3. **Cache the AppleSmartBattery service handle and hw.memsize** (Findings 6 and 7). Quick wins that eliminate ~15 unnecessary CGo crossings per tick with minimal code changes.

**Benchmarking recommendation**: Before implementing optimizations, add a benchmark test that calls `collectAll()` in a loop and measures time and allocations per iteration using `testing.B`. Use `go test -bench=. -benchmem` to establish a baseline. Key metrics to track:
- Wall time per `collectAll()` invocation
- Allocations per invocation (`allocs/op`)
- Bytes allocated per invocation (`B/op`)

After implementing changes, re-run to validate improvement. Consider also using `runtime/pprof` to capture a CPU profile during a 60-second run to identify any hotspots not visible in code review (e.g., lipgloss rendering overhead, GC pressure from per-tick allocations).

---
---

## Review Date: 2026-03-27 (Addendum)

**Scope**: Targeted review of the time-series sparkline graph feature -- `internal/ui/history.go`, `internal/ui/sparkline.go`, and graph-related functions in `internal/ui/panels.go`.

**Focus**: Per-tick allocation pressure and computational cost when graphs are enabled. With default settings, each tick triggers 5 `Push()` calls, 4-5 `Values()` calls returning up to 256 float64s each, and 4-5 `RenderSparkline()` calls that build braille grids.

---

## Findings

### Finding 13: RingBuffer.Values() allocates a new slice on every call

- **Impact**: Medium
- **Location**: `internal/ui/history.go:41-51`
- **Issue**: `Values()` calls `make([]float64, rb.count)` every time it is invoked. With 5 ring buffers at capacity 256 and graphs enabled, this produces 5 allocations of 2048 bytes (256 * 8 bytes per float64) per tick -- 10 KB of short-lived heap allocations per tick. At the minimum 250ms tick interval, this is 40 KB/s of allocation pressure feeding directly into GC work.

  The network panel is worse: `renderNetworkPanelWithGraph` calls `histIn.Values()` and `histOut.Values()`, and then immediately passes the returned slices to `autoScaleMax()` (which iterates them) and `appendSparkline()` (which passes them to `RenderSparkline()`, which may reslice them). The slices are dead after the render call returns. None of this requires ownership transfer -- a read-only view would suffice.

- **Current cost**: 5 slice allocations per tick (up to 10 KB total), all short-lived.
- **Recommendation**: Add a `ValuesInto(dst []float64) []float64` method that reuses a caller-provided buffer:
  ```go
  func (rb *RingBuffer) ValuesInto(dst []float64) []float64 {
      if rb.count == 0 {
          return dst[:0]
      }
      if cap(dst) < rb.count {
          dst = make([]float64, rb.count)
      } else {
          dst = dst[:rb.count]
      }
      start := (rb.head - rb.count + len(rb.data)) % len(rb.data)
      for i := 0; i < rb.count; i++ {
          dst[i] = rb.data[(start+i)%len(rb.data)]
      }
      return dst
  }
  ```
  Each `render*PanelWithGraph` function (or the layout function itself) can hold a single reusable `[]float64` buffer that gets passed to each `ValuesInto` call in sequence. Since the panels render sequentially, one buffer suffices for all 5 metrics.

  Alternatively, eliminate the copy entirely by exposing a two-segment read interface (since the ring buffer data is contiguous in at most two slices: `data[start:end]` or `data[start:] + data[:end]`). `RenderSparkline` could accept two slices, or a small iterator, avoiding any copy. This is a larger API change but eliminates the allocation entirely.
- **Trade-offs**: `ValuesInto` adds API complexity but is a standard Go pattern. The two-segment approach is more invasive and harder to use correctly.

---

### Finding 14: RenderSparkline allocates a 2D grid (Height * Width uint8s) on every call

- **Impact**: Medium
- **Location**: `internal/ui/sparkline.go:70-73`
- **Issue**: Each `RenderSparkline` call allocates `Height + 1` slices: one `[][]uint8` outer slice and `Height` inner `[]uint8` slices. With typical dimensions of Width=40-50, Height=3-4, this is 4-5 heap allocations per call totaling ~200-250 bytes. Across 4-5 sparklines per tick, this produces 16-25 small allocations per tick.

  The individual row slices (`make([]uint8, opts.Width)`) are the main concern: they are small, short-lived, and numerous. Small allocations stress the Go allocator's size-class system and fragment the heap.

- **Current cost**: ~20 small heap allocations per tick for grid construction.
- **Recommendation**: Flatten the grid into a single `[]uint8` allocation of size `Height * Width` and index it as `grid[row*Width + col]`. This replaces `Height + 1` allocations with exactly 1. The flat slice can also be pooled via `sync.Pool` keyed on `Height * Width`, or kept as a field on a reusable `SparklineRenderer` struct:
  ```go
  type SparklineRenderer struct {
      grid []uint8
  }

  func (sr *SparklineRenderer) Render(data []float64, opts SparklineOpts) string {
      needed := opts.Height * opts.Width
      if cap(sr.grid) < needed {
          sr.grid = make([]uint8, needed)
      } else {
          sr.grid = sr.grid[:needed]
          for i := range sr.grid {
              sr.grid[i] = 0
          }
      }
      // ... use sr.grid[row*opts.Width+col] instead of grid[row][col]
  }
  ```
  This also improves cache locality: the entire grid sits in one contiguous memory region rather than scattered across heap allocations.

- **Trade-offs**: Indexing math (`row*Width + col`) is slightly less readable than `grid[row][col]`. A reusable renderer struct adds state management. Both are minor.

---

### Finding 15: RenderSparkline creates a new lipgloss.Style on every call

- **Impact**: Medium
- **Location**: `internal/ui/sparkline.go:121`
- **Issue**: `lipgloss.NewStyle().Foreground(opts.Color)` is called inside `RenderSparkline`, which runs 4-5 times per tick. `lipgloss.NewStyle()` allocates a new style struct on the heap. The `Foreground()` method returns yet another copy. The style is then used in `style.Render(line.String())` which is called once per grid row (3-4 times per sparkline).

  Across 5 sparklines with 3-4 rows each, `style.Render()` is called 15-20 times per tick. Each `Render()` call internally builds ANSI escape sequences with string allocations.

  Additionally, `appendSparkline` at `panels.go:292` creates another `lipgloss.NewStyle()` for the label on every call.

- **Current cost**: 9-10 `lipgloss.NewStyle()` allocations per tick (5 sparkline styles + 4-5 label styles), plus 15-20 `Render()` string allocations.
- **Recommendation**: Pre-create a style per sparkline color at package init time or on the `SparklineOpts` struct. The set of colors is fixed and small (5 defined in `styles.go`). Replace the per-call `NewStyle` with a lookup:
  ```go
  var sparkStyles = map[lipgloss.TerminalColor]lipgloss.Style{}

  func getSparkStyle(color lipgloss.TerminalColor) lipgloss.Style {
      if s, ok := sparkStyles[color]; ok {
          return s
      }
      s := lipgloss.NewStyle().Foreground(color)
      sparkStyles[color] = s
      return s
  }
  ```
  For the label style in `appendSparkline`, extract it to a package-level variable since it always uses `dimStyle.GetForeground()` which is constant.

  A more impactful optimization: instead of calling `style.Render()` per row, build the entire grid as one string and call `style.Render()` once. This requires inserting newlines without breaking the ANSI escape sequence scope, which lipgloss handles -- a single `Render()` call with embedded newlines works correctly and avoids per-row ANSI open/close sequences.

- **Trade-offs**: Pre-created styles use a small amount of persistent memory. The single-Render approach produces slightly different ANSI output (one escape sequence wrapping everything vs. per-line sequences) but should render identically in all terminals.

---

### Finding 16: Inner strings.Builder in RenderSparkline creates a throwaway string per row

- **Impact**: Low
- **Location**: `internal/ui/sparkline.go:127-131`
- **Issue**: The rendering loop uses two `strings.Builder` instances: an outer `sb` for the full output and an inner `line` for each row. The inner builder is created fresh each iteration and `line.String()` is called to pass the result to `style.Render()`. This produces one temporary string allocation per row (3-4 bytes of braille runes per character, so roughly 120-200 bytes per row for a 40-50 column width).

  Since each braille character is a 3-byte UTF-8 encoding (U+2800-U+28FF are in the range E2 A0 80 to E2 A3 BF), a 50-column row produces a 150-byte string from `line.String()`, then `style.Render()` wraps it in ANSI codes producing another ~170-byte string. Both are dead after `sb.WriteString()`. This is 15-20 temporary string pairs per tick.

- **Current cost**: ~30-40 small string allocations per tick (two per row across all sparklines).
- **Recommendation**: Write braille runes directly into the outer builder, calling `style.Render()` once per sparkline (as noted in Finding 15). If per-row styling is needed, pre-allocate the inner builder outside the loop and reset it with `line.Reset()` each iteration. Also pre-grow `sb` with `sb.Grow(opts.Height * (opts.Width*3 + 20))` to avoid internal reallocation.
- **Trade-offs**: Minimal. `Reset()` reuse is a standard Go pattern.

---

### Finding 17: Modulo operations in RingBuffer.Values() hot loop

- **Impact**: Low
- **Location**: `internal/ui/history.go:48`
- **Issue**: The copy loop in `Values()` computes `(start+i) % len(rb.data)` on every iteration. Integer modulo is compiled to a division instruction on most architectures (roughly 20-40 cycles on ARM). For 256 iterations this is not significant in absolute terms, but the modulo can be replaced with a branch-free approach or a two-segment copy that avoids per-element arithmetic entirely.

  The ring buffer data is stored contiguously in memory and wraps at most once. The chronological data can always be expressed as at most two contiguous segments: `data[start:cap]` and `data[0:remainder]`. Using `copy()` for these two segments would be significantly faster for large buffers because `copy()` compiles to `memmove` which uses SIMD/NEON instructions on Apple Silicon.

- **Current cost**: 256 modulo operations per `Values()` call, 5 calls per tick = 1280 modulo operations per tick. Roughly 25-50 microseconds on M-series chips. Not a bottleneck but easily improvable.
- **Recommendation**: Replace the element-by-element loop with two `copy()` calls:
  ```go
  func (rb *RingBuffer) Values() []float64 {
      if rb.count == 0 {
          return nil
      }
      out := make([]float64, rb.count)
      start := (rb.head - rb.count + len(rb.data)) % len(rb.data)
      n := copy(out, rb.data[start:])
      if n < rb.count {
          copy(out[n:], rb.data[:rb.count-n])
      }
      return out
  }
  ```
  This is both faster (SIMD memcpy vs. per-element modulo) and more idiomatic Go.
- **Trade-offs**: None. Strictly better in performance and readability.

---

### Finding 18: history.Record() runs unconditionally even when graphs are disabled

- **Impact**: Low
- **Location**: `internal/ui/app.go:139`
- **Issue**: `m.history.Record(m.metrics)` is called on every tick regardless of whether `showGraphs` is true. When graphs are disabled, the recorded data is never consumed. The cost is 5 `Push()` calls per tick, each doing one array write and two arithmetic operations -- very cheap. The ring buffer memory (5 * 256 * 8 = 10 KB) is also held permanently.

  However, this is arguably a feature rather than a bug: when the user toggles graphs on with `g`, they immediately see historical data rather than a blank graph. The `Record()` cost is negligible (~50ns per tick for 5 writes).

- **Current cost**: ~50ns CPU and 10 KB memory when graphs are disabled.
- **Recommendation**: Keep the current behavior. The cost is trivial and the user experience benefit of instant graph population on toggle is worth it. If memory were a concern (it is not at 10 KB), recording could be gated on `showGraphs` at the cost of empty graphs on first toggle.
- **Trade-offs**: N/A -- no change recommended.

---

### Finding 19: autoScaleMax iterates the full data slice redundantly

- **Impact**: Low
- **Location**: `internal/ui/panels.go:387-399`
- **Issue**: `autoScaleMax()` iterates the entire `[]float64` (up to 256 elements) to find the maximum value. This is called twice for the network panel (once for in, once for out). The same data is then passed to `RenderSparkline()` which also iterates it. For percentage-based metrics (CPU, GPU, Memory), `autoScaleMax` is not called because the max is known (100).

  The redundant iteration is O(n) for n=256, costing roughly 1-2 microseconds. Not significant, but the max could be tracked incrementally in the ring buffer itself.

- **Current cost**: Two extra O(256) linear scans per tick.
- **Recommendation**: Optionally track a running max in the ring buffer. This is complex to do correctly with a circular buffer (the max may be evicted). A simpler approach: since `RenderSparkline` already iterates the data for scaling (lines 53-66), `autoScaleMax` could be folded into the renderer by passing `Max: 0` or `Max: math.NaN()` as a sentinel meaning "auto-scale", letting the renderer compute the max during its existing iteration.
- **Trade-offs**: Folding auto-scale into the renderer couples the concerns. Tracking running max adds complexity to the ring buffer. The current approach is clean and the cost is negligible.

---

### Finding 20: appendSparkline splits strings and re-joins them for side-by-side layout

- **Impact**: Low
- **Location**: `internal/ui/panels.go:296-309`
- **Issue**: `appendSparkline` calls `strings.Split(graph, "\n")` and `strings.Split(labelBlock, "\n")` to join the label and graph side by side. This creates two new string slices and their backing arrays. It then iterates and concatenates them into a `strings.Builder`.

  The label rendering via `labelStyle.Width(graphLabelWidth).Render(label)` may also produce multi-line output that gets split. Since the graph height is known (3-4 lines) and the label is always single-line (padded to fill), this split-and-join pattern produces 2 slice allocations and 6-8 string subslice headers per call, across 4-5 calls per tick.

- **Current cost**: ~10 small allocations per tick for string splitting.
- **Recommendation**: Build the side-by-side output without splitting. The graph renderer could return a `[]string` (one per row) instead of a single newline-joined string, avoiding the split entirely. The label can be generated as a fixed-width string without using lipgloss `Width()` rendering (just pad with spaces). This eliminates the split-join round-trip.
- **Trade-offs**: Changing the `RenderSparkline` return type from `string` to `[]string` is a larger API change. A simpler middle ground: have the sparkline renderer accept a callback or writer per row.

---

## Allocation Budget Summary (per tick, graphs enabled)

| Source | Allocations | Bytes | Notes |
|--------|------------:|------:|-------|
| RingBuffer.Values() x5 | 5 | ~10 KB | Finding 13 |
| Sparkline grid (2D) x5 | ~20 | ~1 KB | Finding 14 |
| lipgloss.NewStyle() x10 | ~10 | ~1 KB | Finding 15 |
| strings.Builder + String() | ~40 | ~5 KB | Finding 16 |
| appendSparkline splits x5 | ~10 | ~0.5 KB | Finding 20 |
| **Total (graph feature)** | **~85** | **~17.5 KB** | Per tick |

For comparison, at 250ms tick interval, this is ~340 allocations/sec and ~70 KB/sec of GC pressure from the graph feature alone.

---

## Summary

**Overall performance posture**: The sparkline graph feature is well-implemented and correct. The algorithmic complexity is appropriate -- O(n) work for n=256 data points per sparkline is entirely reasonable for a 1-second (or even 250ms) TUI refresh. There are no O(n^2) patterns, no unbounded growth, and the ring buffer design is sound. The main concern is allocation pressure: approximately 85 short-lived heap allocations totaling ~17.5 KB per tick, which is modest but improvable.

**Top 3 priorities for the graph feature**:

1. **Eliminate per-tick slice allocation in Values()** (Finding 13). This is the largest single source of allocation bytes (10 KB/tick). Switching to `ValuesInto()` with a reusable buffer, or exposing a two-segment read API, eliminates 5 allocations and 10 KB per tick with minimal API change.

2. **Flatten the sparkline grid and reuse it** (Finding 14). Replacing the 2D grid with a flat `[]uint8` buffer on a reusable renderer struct eliminates ~20 allocations per tick and improves cache locality.

3. **Replace the per-element modulo loop in Values() with two copy() calls** (Finding 17). This is a pure win with zero trade-offs -- faster, shorter, more idiomatic code. While the absolute savings are small (~25-50 microseconds), it is the kind of change that should be made simply because it is better.

**Benchmarking recommendation**: Add benchmark tests for the hot path:

```go
func BenchmarkRingBufferValues(b *testing.B) {
    rb := NewRingBuffer(256)
    for i := 0; i < 256; i++ {
        rb.Push(float64(i))
    }
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = rb.Values()
    }
}

func BenchmarkRenderSparkline(b *testing.B) {
    data := make([]float64, 256)
    for i := range data {
        data[i] = float64(i % 100)
    }
    opts := SparklineOpts{Width: 50, Height: 4, Min: 0, Max: 100}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = RenderSparkline(data, opts)
    }
}

func BenchmarkFullGraphRender(b *testing.B) {
    // Set up MetricHistory with 256 samples, render all panels with graphs
    // to measure the complete per-tick graph cost.
}
```

Run with `go test -bench=. -benchmem ./internal/ui/` before and after changes to quantify improvement. Target: reduce graph-feature allocations from ~85/tick to under 15/tick.

---
---

## Review Date: 2026-03-27 (Re-review after performance fixes)

**Scope**: Verification of fixes applied to Findings 13, 14, 15, and 17 from the previous sparkline graph review. Files: `internal/ui/history.go`, `internal/ui/sparkline.go`, `internal/ui/styles.go`, `internal/ui/panels.go`.

---

## Fix Verification

### Finding 13 (RingBuffer.Values() allocation) -- PARTIALLY FIXED

- **Status**: Fix applied to `Values()`, but callers not migrated to `ValuesInto()`.
- **Location**: `internal/ui/history.go:45-60` (Values), `internal/ui/history.go:64-83` (ValuesInto)
- **Assessment**: `Values()` now uses two `copy()` calls instead of the per-element modulo loop (this also resolves Finding 17). The implementation is correct: it computes the `tail` segment length, and either copies in one shot when the data does not wrap, or copies two segments when it does.

  However, `ValuesInto()` exists but is **never called**. All 5 call sites in `panels.go` (lines 319, 331, 347, 366, 374) still call `history.Values()`, which allocates a new `[]float64` on every invocation. The original allocation problem (5 slices, ~10 KB/tick) remains fully present. The `ValuesInto()` method is dead code.

- **Remaining cost**: 5 allocations, ~10 KB per tick (unchanged from before).
- **Recommendation**: Migrate the `render*PanelWithGraph` functions to use `ValuesInto()`. A single `[]float64` buffer can be held on the model or passed through the render pipeline. Since panels render sequentially, one shared buffer suffices. This would eliminate all 5 per-tick allocations.

---

### Finding 14 (2D sparkline grid) -- FIXED

- **Status**: Correctly implemented.
- **Location**: `internal/ui/sparkline.go:70`
- **Assessment**: The grid is now a single flat `[]uint8` allocation: `grid := make([]uint8, opts.Height*opts.Width)`. All indexing uses `row*opts.Width+col` (lines 89, 112). This replaces the previous `Height+1` allocations with exactly 1. The implementation is correct and the indexing is consistent throughout the function (both the point-plotting loop at line 89 and the vertical-connection loop at line 112 use the same formula).

- **Improvement**: ~20 allocations per tick reduced to ~5 (one per sparkline call). Cache locality improved.

---

### Finding 15 (lipgloss.NewStyle() per render) -- FIXED

- **Status**: Correctly implemented.
- **Location**: `internal/ui/styles.go:65` (sparklineStyle), `internal/ui/styles.go:68` (graphLabelStyle), `internal/ui/styles.go:55` (barBaseStyle)
- **Assessment**: `sparklineStyle`, `graphLabelStyle`, and `barBaseStyle` are now package-level variables initialized once. In `sparkline.go:118`, the style is derived via `sparklineStyle.Foreground(opts.Color)` which returns a new style value (lipgloss styles are value types), but this is a lightweight copy rather than a full `NewStyle()` allocation. In `panels.go:291`, `graphLabelStyle` is used directly. The `progressBar` function in `styles.go:91-94` uses `barBaseStyle.Foreground(...)` similarly.

  Note: `sparklineStyle.Foreground(opts.Color)` still creates a new style value on each call (lipgloss `Foreground()` returns a copy). Since there are only 5 distinct colors, these could be pre-computed as 5 cached styles to eliminate this entirely. This is a minor residual cost.

- **Improvement**: ~10 `NewStyle()` heap allocations per tick eliminated. Minor residual from `.Foreground()` copies.

---

### Finding 17 (Modulo in Values() loop) -- FIXED (merged into Finding 13 fix)

- **Status**: Correctly implemented.
- **Location**: `internal/ui/history.go:52-58`
- **Assessment**: The per-element `(start+i) % len(rb.data)` loop has been replaced with two `copy()` calls. The logic correctly handles both cases: when the data does not wrap around the buffer end (single copy), and when it does (two copies). The same pattern is correctly duplicated in `ValuesInto()`.

- **Improvement**: 1280 modulo operations per tick replaced with 5-10 `copy()` calls (compiled to NEON memcpy on Apple Silicon).

---

## New Finding

### Finding 21: ValuesInto() is dead code -- Values() still called at all 5 sites

- **Impact**: Medium
- **Location**: `internal/ui/panels.go:319,331,347,366,374`
- **Issue**: The `ValuesInto()` method was added to `RingBuffer` but no caller uses it. All 5 graph-rendering functions call `history.Values()`, which allocates a fresh `[]float64` per call. The intended optimization from Finding 13 (reusable buffer) was implemented in the data structure but not wired into the rendering pipeline.
- **Current cost**: 5 allocations totaling ~10 KB per tick, identical to the pre-fix state.
- **Recommendation**: Add a `[]float64` field (e.g., `valueBuf`) to the `Model` struct or to a render context passed through the panel functions. Each `render*PanelWithGraph` call would use `history.ValuesInto(m.valueBuf)` and reassign the returned slice back. Since panels render sequentially and the returned slice is only read (never stored), a single buffer is safe.
- **Trade-offs**: Adds a field to the model. Minimal complexity.

---

## Updated Allocation Budget (per tick, graphs enabled, post-fixes)

| Source | Before | After | Notes |
|--------|-------:|------:|-------|
| RingBuffer.Values() x5 | 5 allocs, ~10 KB | 5 allocs, ~10 KB | ValuesInto() not wired in |
| Sparkline grid x5 | ~20 allocs, ~1 KB | 5 allocs, ~1 KB | Flattened to single slice |
| lipgloss.NewStyle() x10 | ~10 allocs, ~1 KB | ~0 allocs | Cached at package level |
| strings.Builder + String() | ~40 allocs, ~5 KB | ~40 allocs, ~5 KB | Unchanged |
| appendSparkline splits x5 | ~10 allocs, ~0.5 KB | ~10 allocs, ~0.5 KB | Unchanged |
| **Total** | **~85 allocs, ~17.5 KB** | **~60 allocs, ~16.5 KB** | ~30% fewer allocs |

---

## Summary

Three of the four fixes are correctly applied and effective. The grid flattening (Finding 14) and style caching (Finding 15) are clean and correct. The `Values()` two-copy optimization (Finding 17) is also correct.

The main gap is that `ValuesInto()` exists as dead code -- the largest allocation source (10 KB/tick from `Values()`) remains unchanged. Wiring `ValuesInto()` into the panel render functions is the single highest-impact remaining change for the graph feature, and it is straightforward to implement.

**Priority**: Migrate callers from `Values()` to `ValuesInto()` with a shared buffer. This alone would drop per-tick graph allocations from ~60 to ~55 and per-tick bytes from ~16.5 KB to ~6.5 KB.

---

## Review Date: 2026-03-27 (Spot-check: sparkline simplification, network graph resize, temperature graph)

**Scope**: Targeted review of changes in `internal/ui/panels.go` (simplified `appendSparkline`, network graph height reduction, new `renderTemperaturePanelWithGraph`) and `internal/ui/history.go` (max-temp sensor loop in `Record()`).

### Finding 18: appendSparkline simplification -- allocation improvement confirmed

- **Impact**: Low (positive)
- **Location**: `internal/ui/panels.go:280-291`
- **Issue**: The previous side-by-side layout used `strings.Split` and `strings.Join` to interleave graph lines next to the base panel, which allocated intermediate string slices on every render. The new version is a simple concatenation: `base + "\n" + label + "\n" + graph`.
- **Assessment**: This is a clean win. The old approach created O(graphHeight) intermediate slices from Split/Join. The new stacked layout is pure append, producing no intermediate slice allocations. For a typical 4-row graph, this eliminates ~2 slice allocations and ~8 intermediate strings per sparkline render, across 6 graphs per tick.
- **Trade-offs**: None. The stacked layout is both simpler and faster.

### Finding 19: renderTemperaturePanelWithGraph scans history data twice

- **Impact**: Low
- **Location**: `internal/ui/panels.go:374-389`
- **Issue**: `renderTemperaturePanelWithGraph` calls `history.Values()` (which copies the ring buffer into a new slice), then iterates that slice to find `maxVal` for scale computation. The sparkline renderer (`RenderSparkline`) will then iterate the same slice again internally. This is two passes over the data plus one allocation.
- **Current cost**: O(n) extra pass over ~120-300 data points (typical history capacity). Negligible in absolute terms (~1 microsecond).
- **Recommendation**: Not worth changing. The extra pass is trivial compared to the braille grid computation and string rendering. If anything, the larger win remains the prior recommendation (Finding from previous review) to use `ValuesInto()` to avoid the slice allocation, which applies equally here.
- **Trade-offs**: N/A.

### Finding 20: renderTemperaturePanelWithGraph duplicates max-finding logic with autoScaleMax

- **Impact**: Low
- **Location**: `internal/ui/panels.go:375-384` vs `internal/ui/panels.go:393-405`
- **Issue**: The temperature graph function has an inline loop to find `maxVal` and then computes `maxVal * 1.2` with a floor of 30.0. The existing `autoScaleMax` helper does the same pattern (max * 1.2, floor of 1024). These are nearly identical except for the floor value. This is a code clarity issue more than a performance issue, but two near-identical scan loops is unnecessary cognitive overhead and a minor duplication of work if one were ever reused.
- **Recommendation**: Consider parameterizing `autoScaleMax` with a floor argument, or leave as-is since the floor values differ by domain (bytes vs degrees). No performance impact either way.

### Finding 21: Record() sensor iteration is negligible

- **Impact**: Low (no concern)
- **Location**: `internal/ui/history.go:126-133`
- **Issue**: The new max-temp loop iterates `m.Temperature.Sensors` on every tick. Apple Silicon machines typically expose 5-15 temperature sensors.
- **Assessment**: Iterating 5-15 float64 comparisons per tick (every 1-2 seconds) is unmeasurable. No allocation (the slice is already owned by the metrics struct). The `Available` guard correctly short-circuits when no sensor data exists. This is clean.

### Finding 22: Values() allocation remains the dominant cost for the new temperature graph

- **Impact**: Medium
- **Location**: `internal/ui/panels.go:374` -- `data := history.Values()`
- **Issue**: As noted in prior reviews, `Values()` allocates a new `[]float64` slice on every call. The temperature graph adds a 6th call to `Values()` per render tick (joining CPU, GPU, Memory, NetIn, NetOut). With a typical capacity of 300, that is an additional ~2.4 KB allocation per tick.
- **Current cost**: ~2.4 KB/tick for the temperature graph alone; ~14.4 KB/tick across all 6 graphs.
- **Recommendation**: Same as the outstanding recommendation from the prior review -- migrate all `Values()` callers to `ValuesInto()` with a reusable buffer. This is now slightly more impactful since there are 6 graphs instead of 5.
- **Trade-offs**: Requires threading a shared buffer through the render functions, adding a parameter.

### Finding 23: Network graph height reduction is a small positive

- **Impact**: Low (positive)
- **Location**: `internal/ui/panels.go:343` -- `netHeight := 2`
- **Assessment**: Reducing network graph height from the default (likely 3-4) to 2 rows reduces the braille grid from `width * 4 * height` cells to `width * 4 * 2`. For a 80-column terminal, this saves ~160-320 bytes of grid allocation per network graph (times 2 for in/out). Minor but directionally correct for a panel that already shows numeric throughput values.

## Summary

The `appendSparkline` simplification is a clear allocation win -- the old Split/Join interleaving was the most allocation-heavy part of graph rendering, and the new stacked approach eliminates it entirely. The new temperature graph code is clean and introduces no performance concerns; the sensor iteration in `Record()` is trivially cheap.

The single outstanding issue remains unchanged from prior reviews: **all 6 graph render paths call `Values()` which allocates a fresh slice per tick**. The `ValuesInto()` method already exists but has no callers. Wiring it in is now the top priority, and it is slightly more impactful with the addition of the temperature graph (6 allocations instead of 5).

No benchmarking is needed for this spot-check -- the changes are straightforward enough to assess by inspection.
