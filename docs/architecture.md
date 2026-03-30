# mactop Architecture & Design

## 1. Language Choice: Go

**Decision**: Use Go (not Swift).

**Rationale**:

| Factor | Go | Swift |
|--------|-----|-------|
| Single binary | `go build` produces one static binary | Requires bundled Swift runtime on some configs |
| TUI ecosystem | Mature: `bubbletea`, `lipgloss`, `tcell` | Very limited; no production TUI framework |
| CGo / IOKit access | CGo works well; can call any C API | Native, but TUI story is weak |
| Cross-team familiarity | Widely known | Narrower audience |
| Build speed | Fast | Slower (especially with frameworks) |
| macOS API access | Via CGo + IOKit/CoreFoundation C headers | Native, slight advantage |

The TUI ecosystem is the deciding factor. Go's `bubbletea` (Elm-architecture TUI framework) plus `lipgloss` (styling) is production-grade and well-maintained. Swift has nothing comparable. The macOS APIs we need (IOKit, mach kernel calls, sysctl) are all C APIs anyway, so Go's CGo interop covers them without friction.

**Trade-off**: CGo adds build complexity and means we need Xcode command-line tools installed at build time (but not at runtime). This is acceptable for a macOS-only tool.

---

## 2. Project Structure

```
mactop/
  cmd/
    mactop/
      main.go              -- entrypoint, CLI flag parsing, run loop
  internal/
    collector/
      collector.go         -- Collector interface, aggregate orchestrator
      cpu.go               -- CPU usage (per-core + aggregate)
      gpu.go               -- GPU utilization
      memory.go            -- Memory stats
      network.go           -- Network throughput
      disk.go              -- Disk capacity + I/O
      power.go             -- Battery / power source
      temperature.go       -- Thermal sensors
    metrics/
      types.go             -- All metric data structs (pure data, no logic)
    smc/
      smc.go               -- Low-level SMC (System Management Controller) access via IOKit
      keys.go              -- SMC key constants for Apple Silicon sensors
    ui/
      app.go               -- Bubbletea Model (top-level application)
      layout.go            -- Layout logic: grid of panels
      panels.go            -- Individual panel renderers (CPU, GPU, etc.)
      styles.go            -- Lipgloss style definitions
      help.go              -- Help overlay
    platform/
      mach.go              -- CGo bindings for mach kernel calls
      iokit.go             -- CGo bindings for IOKit
      sysctl.go            -- sysctl wrappers
  go.mod
  go.sum
  Makefile
  docs/
    architecture.md        -- this file
```

---

## 3. Data Sources Per Metric

This section specifies the exact macOS API or system call for each metric. All of these are C APIs accessed via CGo.

### 3.1 CPU Usage

**API**: `host_processor_info()` (Mach kernel)

```
kern_return_t host_processor_info(
    host_t host,
    processor_flavor_t flavor,          // PROCESSOR_CPU_LOAD_INFO
    natural_t *out_processor_count,
    processor_info_array_t *out_info,
    mach_msg_type_number_t *out_count
);
```

Returns per-CPU tick counts for `CPU_STATE_USER`, `CPU_STATE_SYSTEM`, `CPU_STATE_IDLE`, `CPU_STATE_NICE`. To compute utilization percentages, take two samples and divide delta-busy by delta-total.

**CGo sketch** (`internal/platform/mach.go`):

```go
/*
#include <mach/mach.h>
#include <mach/processor_info.h>
#include <mach/mach_host.h>
*/
import "C"

func HostProcessorInfo() ([]CPUTicks, error) {
    var count C.natural_t
    var info C.processor_info_array_t
    var msgCount C.mach_msg_type_number_t

    kr := C.host_processor_info(
        C.mach_host_self(),
        C.PROCESSOR_CPU_LOAD_INFO,
        &count,
        &info,
        &msgCount,
    )
    if kr != C.KERN_SUCCESS {
        return nil, fmt.Errorf("host_processor_info failed: %d", kr)
    }
    defer C.vm_deallocate(
        C.mach_task_self(),
        C.vm_address_t(uintptr(unsafe.Pointer(info))),
        C.vm_size_t(msgCount)*C.vm_size_t(unsafe.Sizeof(C.int(0))),
    )
    // Convert to []CPUTicks (4 values per core)
    // ...
}
```

Also use `sysctlbyname("hw.perflevel0.logicalcpu")` and `sysctlbyname("hw.perflevel1.logicalcpu")` to distinguish P-cores (performance) from E-cores (efficiency) on Apple Silicon.

### 3.2 GPU Usage

**Primary approach**: IOKit `IOServiceMatching("IOAccelerator")` to read GPU utilization.

The key properties on the `IOAccelerator` IOService object:

- `"PerformanceStatistics"` dictionary contains:
  - `"Device Utilization %"` (integer, 0-100) -- **this is the GPU busy percentage**
  - `"In use system memory"` (bytes)
  - `"Allocated system memory"` (bytes)

**Access pattern**:

```go
/*
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
*/
import "C"

func GPUUtilization() (int, error) {
    matching := C.IOServiceMatching(C.CString("IOAccelerator"))
    var iterator C.io_iterator_t
    kr := C.IOServiceGetMatchingServices(C.kIOMainPortDefault, matching, &iterator)
    // iterate, call IORegistryEntryCreateCFProperties
    // extract "PerformanceStatistics" -> "Device Utilization %"
}
```

**Fallback**: If `IOAccelerator` is unavailable or returns no data, parse output from `sudo powermetrics --samplers gpu_power -i 1000 -n 1`. Note: `powermetrics` requires root, so document this as a degraded mode.

**Linker flags**: `-framework IOKit -framework CoreFoundation`

### 3.3 Memory Usage

**API**: `host_statistics64()` with `HOST_VM_INFO64` (Mach kernel)

```go
/*
#include <mach/mach.h>
#include <mach/host_info.h>
*/
import "C"

func VMStats() (*vm_statistics64, error) {
    var stats C.vm_statistics64_data_t
    count := C.mach_msg_type_number_t(C.HOST_VM_INFO64_COUNT)
    kr := C.host_statistics64(
        C.mach_host_self(),
        C.HOST_VM_INFO64,
        (*C.integer_t)(unsafe.Pointer(&stats)),
        &count,
    )
    // stats.free_count, stats.active_count, stats.inactive_count,
    // stats.wire_count, stats.compressor_page_count, stats.internal_page_count
}
```

**Page size**: `C.vm_kernel_page_size` (typically 16384 on Apple Silicon).

**Swap**: `sysctl("vm.swapusage")` returns `struct xsw_usage { total, avail, used }`.

**Total physical RAM**: `sysctlbyname("hw.memsize")`.

### 3.4 Network Usage

**API**: `sysctl` with `CTL_NET` / `PF_ROUTE` / `NET_RT_IFLIST2`

This returns `struct if_msghdr2` per interface, containing:

- `ifm_data.ifi_ibytes` -- total bytes received (cumulative counter)
- `ifm_data.ifi_obytes` -- total bytes sent (cumulative counter)
- `ifm_data.ifi_ipackets`, `ifm_data.ifi_opackets`

To compute per-second rates: sample twice, divide delta by elapsed time.

```go
/*
#include <sys/sysctl.h>
#include <net/if.h>
#include <net/route.h>
*/
import "C"

func NetInterfaceStats() (map[string]NetCounters, error) {
    mib := [6]C.int{C.CTL_NET, C.PF_ROUTE, 0, 0, C.NET_RT_IFLIST2, 0}
    var bufLen C.size_t
    C.sysctl(&mib[0], 6, nil, &bufLen, nil, 0)
    buf := make([]byte, bufLen)
    C.sysctl(&mib[0], 6, unsafe.Pointer(&buf[0]), &bufLen, nil, 0)
    // Walk buffer parsing if_msghdr2 structs
}
```

Filter out loopback (`lo0`) and inactive interfaces from the display.

### 3.5 Disk Usage

**Capacity**: `syscall.Statfs()` (Go stdlib) on mount points, or `getmntinfo()` to enumerate all mounted filesystems.

```go
import "syscall"

func DiskCapacity(path string) (total, used, avail uint64) {
    var stat syscall.Statfs_t
    syscall.Statfs(path, &stat)
    total = stat.Blocks * uint64(stat.Bsize)
    avail = stat.Bavail * uint64(stat.Bsize)
    used = total - avail
    return
}
```

**I/O throughput**: IOKit `IOServiceMatching("IOBlockStorageDriver")`. Read `"Statistics"` dictionary:

- `"Bytes (Read)"` -- cumulative bytes read
- `"Bytes (Write)"` -- cumulative bytes written

Delta between samples gives throughput.

### 3.6 Power / Battery

**API**: `IOPSCopyPowerSourcesInfo()` and `IOPSCopyPowerSourcesList()` from `IOKit/ps/IOPowerSources.h`.

```go
/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>
*/
import "C"

func PowerSourceInfo() (*PowerMetrics, error) {
    info := C.IOPSCopyPowerSourcesInfo()
    defer C.CFRelease(info)
    list := C.IOPSCopyPowerSourcesList(info)
    defer C.CFRelease(list)
    // Iterate list, extract:
    //   kIOPSCurrentCapacityKey      -> battery %
    //   kIOPSIsChargingKey           -> bool
    //   kIOPSPowerSourceStateKey     -> "AC Power" | "Battery Power"
    //   kIOPSMaxCapacityKey          -> max capacity
}
```

**Wattage / power draw**: Not available from `IOPowerSources`. Options:

1. **IOKit `AppleSmartBattery`**: `IOServiceMatching("AppleSmartBattery")` exposes `Voltage`, `Amperage` (signed, mA). Power = voltage * |amperage| / 1e6 watts.
2. **Fallback**: parse `pmset -g batt` output (gives remaining time estimate but not wattage).

Use option 1 (IOKit `AppleSmartBattery`) as primary. On desktop Macs (Mac Mini, Mac Studio, Mac Pro) there is no battery; detect this gracefully and show "AC Power / No Battery".

### 3.7 Temperatures

**This is the hardest metric on Apple Silicon.**

Apple does not expose thermal sensors through any public API on Apple Silicon. The legacy `SMCReadKey` approach (used by tools like `iStats` on Intel Macs) works through the `AppleSMC` IOKit service, but the SMC key namespace changed significantly with Apple Silicon.

**Primary approach**: Direct IOKit SMC access.

```go
/*
#include <IOKit/IOKitLib.h>

// SMC command structure (reverse-engineered, used by open-source tools)
typedef struct {
    uint32_t key;
    uint8_t  vers[6];
    uint8_t  pLimitData;
    uint8_t  dataSize;
    uint32_t dataType;
    uint8_t  bytes[32];
} SMCVal_t;

// SMC kernel call selectors
enum {
    kSMCUserClientOpen  = 0,
    kSMCUserClientClose = 1,
    kSMCHandleYPCEvent  = 2,
    kSMCReadKey         = 5,
    kSMCGetKeyInfo      = 9,
};
*/
import "C"
```

**Known Apple Silicon SMC keys** (four-char codes):

| Key | Sensor | Type |
|-----|--------|------|
| `Tp09` | CPU efficiency core 1 temp | `flt ` (float32) |
| `Tp0T` | CPU performance core 1 temp | `flt ` |
| `Tg0f` | GPU temp | `flt ` |
| `TaLP` | Airflow left proximity | `flt ` |
| `TaRP` | Airflow right proximity | `flt ` |
| `Ts0P` | SSD/NAND temp | `flt ` |
| `TW0P` | Wireless module temp | `flt ` |
| `Tm0P` | Memory temp | `flt ` |

Note: exact keys vary by model (M1, M2, M3, M4). The implementation should attempt to read a set of known keys and gracefully skip any that return errors.

**Access pattern**:
1. `IOServiceOpen("AppleSMC", ...)` to get a connection
2. Use `IOConnectCallStructMethod` with selector `kSMCReadKey` (5) to read values
3. Parse the returned bytes according to the data type (`flt ` = IEEE 754 float32, `sp78` = signed fixed-point 7.8)

**Fallback**: Parse `sudo powermetrics --samplers smc -i 1000 -n 1` output. Requires root privileges. If the user is not root and SMC direct access fails, show "N/A - requires sudo" for temperature readings.

**Important**: SMC access does NOT require root on macOS when using `IOServiceOpen` from a user process, as long as no sandbox restricts it. This is how tools like `osx-cpu-temp` work without sudo.

---

## 4. TUI Approach

### Library: Bubbletea + Lipgloss

- **[bubbletea](https://github.com/charmbracelet/bubbletea)** -- Elm-architecture TUI framework. Handles input events, screen rendering, and the update loop.
- **[lipgloss](https://github.com/charmbracelet/lipgloss)** -- Styling (borders, colors, alignment, padding).

These are the dominant Go TUI libraries, actively maintained by Charm.

### Dashboard Layout

```
+------------------------------------------------------------------+
|                        mactop v0.1.0                             |
+----------------------------+-------------------------------------+
|  CPU Usage                 |  Memory                             |
|  ======== P-Cores ======== |  Used:  12.4 GB  ||||||||||||----   |
|  P0: ||||||||||||---- 78%  |  Wired:  4.2 GB                    |
|  P1: ||||||||||------ 62%  |  Compressed: 2.1 GB                |
|  P2: |||||||||------- 55%  |  Free:   3.3 GB                    |
|  P3: ||||||||-------- 48%  |  Swap:   0.0 GB                    |
|  ======== E-Cores ======== |  Total: 32.0 GB                    |
|  E0: ||||----------- 28%  +-------------------------------------+
|  E1: |||------------ 18%  |  Temperatures                       |
|  E2: ||------------- 12%  |  CPU P-Core: 62.3 C                 |
|  E3: |-------------- 08%  |  CPU E-Core: 48.1 C                 |
|  Avg: ||||||||------- 39%  |  GPU:        55.7 C                 |
+----------------------------+  SSD:        41.2 C                 |
|  GPU Usage                 |  Mem:        39.8 C                 |
|  ||||||||||||||------ 72%  +-------------------------------------+
|  VRAM: 8.2 / 16.0 GB      |  Power                              |
+----------------------------+  Battery: 87%  [|||||||||-]         |
|  Network                   |  Status:  Charging                  |
|  en0:  In: 2.4 MB/s        |  Source:  AC Power                  |
|        Out: 340 KB/s        |  Power:   14.2 W                   |
+----------------------------+-------------------------------------+
|  Disk                                                            |
|  /: 245.3 / 494.4 GB (49%) [||||||||||---------]                |
|  Read: 12.5 MB/s   Write: 3.2 MB/s                              |
+------------------------------------------------------------------+
|  q: quit   r: reset peaks   ?: help   Refresh: 1s               |
+------------------------------------------------------------------+
```

### Layout Strategy

The layout adapts to terminal width:
- **Wide** (>= 100 cols): two-column layout as shown above.
- **Narrow** (< 100 cols): single-column stack of panels.

Use `lipgloss.JoinHorizontal` and `lipgloss.JoinVertical` for composition. Each panel is a self-contained render function that takes the metrics struct and terminal width, returns a styled string.

---

## 5. Key Data Types

All metric types live in `internal/metrics/types.go`. These are pure data structs with no methods.

```go
package metrics

import "time"

// SystemMetrics is the top-level container returned by the collector each tick.
type SystemMetrics struct {
    Timestamp   time.Time
    CPU         CPUMetrics
    GPU         GPUMetrics
    Memory      MemoryMetrics
    Network     []NetworkInterface
    Disk        DiskMetrics
    Power       PowerMetrics
    Temperature TemperatureMetrics
}

// CPUMetrics holds per-core and aggregate CPU utilization.
type CPUMetrics struct {
    Cores       []CoreUsage
    Aggregate   float64 // 0.0 - 100.0
    PCoreCount  int
    ECoreCount  int
}

type CoreUsage struct {
    ID          int
    User        float64 // percentage
    System      float64
    Idle        float64
    Nice        float64
    Total       float64 // user + system + nice
    IsECore     bool
}

// GPUMetrics holds GPU utilization and memory.
type GPUMetrics struct {
    Utilization     float64 // 0.0 - 100.0
    InUseMemory     uint64  // bytes
    AllocatedMemory uint64  // bytes
    Available       bool    // false if we couldn't read GPU stats
}

// MemoryMetrics holds system memory usage.
type MemoryMetrics struct {
    Total      uint64 // bytes
    Used       uint64
    Free       uint64
    Active     uint64
    Inactive   uint64
    Wired      uint64
    Compressed uint64
    SwapTotal  uint64
    SwapUsed   uint64
    PageSize   uint64
}

// NetworkInterface holds per-interface network counters.
type NetworkInterface struct {
    Name        string
    BytesIn     uint64  // cumulative
    BytesOut    uint64
    BytesInPS   float64 // per second (computed)
    BytesOutPS  float64
}

// DiskMetrics holds disk capacity and I/O stats.
type DiskMetrics struct {
    Volumes     []VolumeInfo
    ReadBytes   uint64  // cumulative
    WriteBytes  uint64
    ReadBPS     float64 // bytes per second (computed)
    WriteBPS    float64
}

type VolumeInfo struct {
    MountPoint string
    Total      uint64
    Used       uint64
    Available  uint64
}

// PowerMetrics holds battery and power source info.
type PowerMetrics struct {
    HasBattery      bool
    BatteryPercent  int     // 0-100
    IsCharging      bool
    PowerSource     string  // "AC Power" or "Battery Power"
    Voltage         float64 // volts
    Amperage        float64 // amps (negative = discharging)
    Wattage         float64 // watts (computed: |V * A|)
    TimeRemaining   int     // minutes, -1 if calculating
}

// TemperatureMetrics holds thermal sensor readings.
type TemperatureMetrics struct {
    Sensors   []TempSensor
    Available bool
}

type TempSensor struct {
    Name     string  // human-readable: "CPU P-Core", "GPU", etc.
    SMCKey   string  // e.g., "Tp0T"
    Value    float64 // degrees Celsius
}
```

---

## 6. Collector Interface and Refresh Loop

### Collector Interface

Each metric module implements a common interface:

```go
package collector

import "github.com/rileyeasland/mactop/internal/metrics"

// Collector gathers one category of system metrics.
// Collect is called once per tick. Implementations hold
// previous-sample state internally for delta computations.
type Collector interface {
    // Collect gathers the current metrics. It must be safe
    // to call repeatedly. Errors are non-fatal; the UI will
    // display stale or "N/A" data.
    Collect() error

    // Name returns a human-readable name for logging.
    Name() string
}

// Specific collector types expose their data via typed fields.
// Example:
type CPUCollector struct {
    Data     metrics.CPUMetrics
    prevTicks []cpuTickSample
}

func (c *CPUCollector) Collect() error { /* ... */ }
func (c *CPUCollector) Name() string   { return "cpu" }
```

### Refresh Loop (Bubbletea Tick Model)

Bubbletea uses a message-passing architecture. The refresh loop works as follows:

```
                    +--------------------+
                    |   Bubbletea Init   |
                    +--------+-----------+
                             |
                             v
                    +--------+-----------+
                    |  Send TickMsg      |  <-- tea.Tick(interval)
                    +--------+-----------+
                             |
                             v
                +------------+-------------+
                |  Update(TickMsg)         |
                |  1. Call all collectors  |
                |  2. Store metrics        |
                |  3. Schedule next tick   |
                +------------+-------------+
                             |
                             v
                +------------+-------------+
                |  View()                  |
                |  Render dashboard from   |
                |  current metrics         |
                +------------+-------------+
                             |
                             v
                     (terminal output)
```

**Key design decisions**:

1. **Collection runs synchronously in Update()**: Each collector call takes microseconds to low milliseconds (they are reading kernel counters, not doing I/O). Running them synchronously keeps the code simple and avoids channel/mutex complexity.

2. **If a collector fails, skip it**: The UI shows the last known value or "N/A". No crashes on transient errors.

3. **Default interval: 1 second**. Configurable via `-i` flag (minimum 250ms).

```go
package ui

import (
    "time"
    tea "github.com/charmbracelet/bubbletea"
)

type TickMsg time.Time

func tickCmd(interval time.Duration) tea.Cmd {
    return tea.Tick(interval, func(t time.Time) tea.Msg {
        return TickMsg(t)
    })
}

type Model struct {
    metrics   metrics.SystemMetrics
    cpu       *collector.CPUCollector
    gpu       *collector.GPUCollector
    memory    *collector.MemoryCollector
    network   *collector.NetworkCollector
    disk      *collector.DiskCollector
    power     *collector.PowerCollector
    temp      *collector.TempCollector
    interval  time.Duration
    width     int
    height    int
}

func (m Model) Init() tea.Cmd {
    return tickCmd(m.interval)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "q" || msg.String() == "ctrl+c" {
            return m, tea.Quit
        }
    case tea.WindowSizeMsg:
        m.width = msg.Width
        m.height = msg.Height
    case TickMsg:
        m.collectAll()
        return m, tickCmd(m.interval)
    }
    return m, nil
}

func (m *Model) collectAll() {
    collectors := []collector.Collector{
        m.cpu, m.gpu, m.memory, m.network,
        m.disk, m.power, m.temp,
    }
    for _, c := range collectors {
        if err := c.Collect(); err != nil {
            // log.Debug, but do not crash
        }
    }
    m.metrics = metrics.SystemMetrics{
        Timestamp: time.Now(),
        CPU:       m.cpu.Data,
        GPU:       m.gpu.Data,
        Memory:    m.memory.Data,
        Network:   m.network.Data,
        Disk:      m.disk.Data,
        Power:     m.power.Data,
        Temperature: m.temp.Data,
    }
}
```

---

## 7. SMC Access Module

The SMC module deserves special attention because it uses undocumented IOKit interfaces.

### Connection Lifecycle

```go
package smc

// Connection holds an open handle to the AppleSMC IOKit service.
type Connection struct {
    conn C.io_connect_t
}

// Open establishes a connection to AppleSMC.
func Open() (*Connection, error) {
    service := C.IOServiceGetMatchingService(
        C.kIOMainPortDefault,
        C.IOServiceMatching(C.CString("AppleSMC")),
    )
    if service == 0 {
        return nil, errors.New("AppleSMC service not found")
    }
    defer C.IOObjectRelease(service)

    var conn C.io_connect_t
    kr := C.IOServiceOpen(service, C.mach_task_self(), 0, &conn)
    if kr != C.kIOReturnSuccess {
        return nil, fmt.Errorf("IOServiceOpen failed: 0x%x", kr)
    }
    return &Connection{conn: conn}, nil
}

// Close releases the SMC connection.
func (c *Connection) Close() error {
    return ioReturn(C.IOServiceClose(c.conn))
}

// ReadFloat reads a float32 temperature value for the given 4-char SMC key.
func (c *Connection) ReadFloat(key string) (float64, error) {
    // 1. Call kSMCGetKeyInfo to get data type and size
    // 2. Call kSMCReadKey to get raw bytes
    // 3. Interpret bytes as float32 (type "flt ") or fixed-point (type "sp78")
    // ...
}
```

### SMC Key Registry

```go
package smc

// SensorDef maps a human name to an SMC key.
type SensorDef struct {
    Key  string
    Name string
}

// AppleSiliconSensors lists known thermal sensor keys.
// The implementation tries each key; missing keys are silently skipped.
var AppleSiliconSensors = []SensorDef{
    {"Tp09", "CPU E-Core 1"},
    {"Tp01", "CPU E-Core 2"},
    {"Tp0T", "CPU P-Core 1"},
    {"Tp0P", "CPU P-Core 2"},
    {"Tp0D", "CPU P-Core 3"},
    {"Tp0H", "CPU P-Core 4"},
    {"Tg0f", "GPU 1"},
    {"Tg0j", "GPU 2"},
    {"Tm0P", "Memory"},
    {"Ts0P", "SSD"},
    {"TaLP", "Airflow Left"},
    {"TaRP", "Airflow Right"},
    {"TW0P", "WiFi Module"},
}
```

---

## 8. CGo Build Configuration

Each file using CGo needs appropriate LDFLAGS. Centralize in one file:

```go
// internal/platform/cgo.go
package platform

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
*/
import "C"
```

All CGo calls link against IOKit and CoreFoundation. No other frameworks are needed.

---

## 9. CLI Flags and Entrypoint

```go
package main

import (
    "flag"
    "fmt"
    "os"
    "time"

    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    interval := flag.Duration("i", 1*time.Second, "refresh interval")
    version := flag.Bool("version", false, "print version and exit")
    flag.Parse()

    if *version {
        fmt.Println("mactop v0.1.0")
        os.Exit(0)
    }

    model := ui.NewModel(*interval)
    p := tea.NewProgram(model, tea.WithAltScreen())
    if _, err := p.Run(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}
```

---

## 10. Build and Distribution

### Makefile

```makefile
.PHONY: build clean install

VERSION := 0.1.0
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	CGO_ENABLED=1 GOARCH=arm64 GOOS=darwin \
	  go build $(LDFLAGS) -o bin/mactop ./cmd/mactop

install: build
	cp bin/mactop /usr/local/bin/mactop

clean:
	rm -rf bin/
```

**Notes**:
- `CGO_ENABLED=1` is required (IOKit access needs CGo).
- `-s -w` strips debug symbols for a smaller binary.
- The resulting binary is ~5-10 MB, fully self-contained.
- No cross-compilation -- must build on macOS with Xcode command-line tools installed (`xcode-select --install`).

### Build Prerequisites

- Go 1.21+ (for bubbletea compatibility)
- Xcode command-line tools (for C headers and IOKit framework)
- macOS 13+ (Ventura) recommended for Apple Silicon API stability

---

## 11. Edge Cases and Failure Modes

| Scenario | Impact | Mitigation |
|----------|--------|------------|
| Desktop Mac (no battery) | PowerCollector returns empty | Check `HasBattery` flag; UI shows "N/A" for battery panel |
| Unknown SMC keys on new chip (M5, etc.) | Some temp sensors missing | Gracefully skip unknown keys; show only sensors that responded |
| Terminal too narrow | Layout breaks | Detect width via `WindowSizeMsg`; switch to single-column or show "terminal too small" |
| Process lacks IOKit permissions | SMC/GPU reads fail | Show "Permission denied" in relevant panel, rest of tool works |
| VM / Hackintosh | Many IOKit services missing | Each collector catches errors independently; tool degrades gracefully |
| Very fast refresh (<250ms) | High CPU usage from mactop itself | Enforce minimum 250ms interval with a clamp |
| Memory leak from Mach port calls | Slow leak over hours | Always call `vm_deallocate` on `processor_info_array_t` results; always `CFRelease` CF objects |
| Large number of network interfaces (VPNs, Docker) | Panel overflow | Show top 5 interfaces by traffic; add `--all-interfaces` flag for full list |
| Counter wraparound (network bytes) | Negative delta | Detect negative delta, treat as reset, skip that sample |

---

## 12. Dependency List

| Dependency | Version | Purpose |
|------------|---------|---------|
| `github.com/charmbracelet/bubbletea` | v1.x | TUI framework |
| `github.com/charmbracelet/lipgloss` | v1.x | TUI styling |
| `github.com/charmbracelet/bubbles` | v0.x | Reusable TUI components (progress bars) |

No other third-party dependencies. All macOS API access is via CGo and system headers.

---

## 13. Open Questions and Risks

**Questions for the team:**

1. **Root/sudo for temperatures** -- On some macOS versions, IOServiceOpen to AppleSMC works without root; on others it may be restricted by TCC. Should we document "run with sudo for full sensor access" or try to make everything work unprivileged? *Recommendation: try unprivileged first; if it fails, print a one-time warning suggesting sudo, but never crash.*

2. **Module name / Go module path** -- Should this be `github.com/rileyeasland/mactop` or something else? Affects import paths throughout the project. *Need to confirm the repo location before scaffolding.*

3. **GPU memory reporting** -- On Apple Silicon, GPU memory is unified (shared with system RAM). `InUseMemory` from IOAccelerator reports the GPU's allocation from the unified pool. Should we display this separately from system memory, or is that confusing? *Recommendation: show it in the GPU panel with a note "(unified memory)".*

4. **Color scheme** -- Should we support light/dark terminal themes, or pick one default? Lipgloss can use adaptive colors. *Recommendation: use ANSI 256 colors that look reasonable on both; add `--no-color` flag.*

5. **Logging** -- For debugging collector failures, should we log to stderr or to a file? Bubbletea captures stdout for the TUI, so stderr is available. *Recommendation: log to stderr only when `-v` (verbose) flag is set; otherwise silent.*

---

## 14. CI/CD: GitHub Actions Release Workflow

### 14.1 Overview

A single GitHub Actions workflow builds and releases `mactop` binaries for both macOS architectures whenever a version tag is pushed. Because the project requires CGO with macOS frameworks (IOKit, CoreFoundation), all builds must run on macOS runners -- Linux cross-compilation is not possible.

```
  git tag v1.2.3 && git push --tags
           |
           v
  +----------------------------+
  | GitHub Actions triggers    |
  | on: push tags "v*"        |
  +----------------------------+
           |
           v
  +----------------------------+
  | Job: release               |
  | runs-on: macos-latest      |
  +----------------------------+
           |
           v
  +--------+--------+
  |                 |
  v                 v
  Build arm64     Build amd64
  (GOARCH=arm64)  (GOARCH=amd64)
  |                 |
  v                 v
  mactop-darwin-   mactop-darwin-
  arm64            amd64
  |                 |
  +--------+--------+
           |
           v
  +----------------------------+
  | Create GitHub Release      |
  | Attach both binaries       |
  +----------------------------+
```

### 14.2 Workflow File

**Path**: `.github/workflows/release.yml`
**Name**: `Release`

### 14.3 Trigger

```
on:
  push:
    tags:
      - "v*"
```

This fires only when a tag matching `v*` is pushed (e.g., `v0.1.0`, `v1.2.3`, `v2.0.0-rc.1`). It does NOT trigger on branch pushes or pull requests.

### 14.4 Job Structure: Single Job, Sequential Builds

**Decision**: Use a single job with sequential build steps rather than a matrix strategy.

**Rationale**:
- A matrix would create two separate jobs, each needing its own checkout and Go setup -- overhead for a build that takes seconds.
- Both binaries must be collected into one release. With a matrix, this requires either a separate "create release" job with artifact passing, or a race condition if both jobs try to create the release. A single job avoids this entirely.
- The total build time for both architectures is under 2 minutes. The simplicity of a single job outweighs the ~30 seconds saved by parallel matrix builds.

**Trade-off**: If build times grow significantly (unlikely for a single-binary Go project), this can be converted to a matrix with a downstream release job. For now, simplicity wins.

### 14.5 Job Definition

**Job name**: `release`
**Runner**: `macos-latest` (GitHub-hosted; provides Xcode CLI tools, which supply the C headers and frameworks needed for CGO)
**Permissions**: `contents: write` (required to create releases and upload assets via the GitHub API)

### 14.6 Steps (in order)

#### Step 1: Checkout

- **Action**: `actions/checkout@v4`
- **Config**: `fetch-depth: 0` (full history, so tag metadata is available)
- **Purpose**: Clone the repo at the tagged commit.

#### Step 2: Set up Go

- **Action**: `actions/setup-go@v5`
- **Config**: Read Go version from `go.mod` (`go-version-file: go.mod`)
- **Purpose**: Install the correct Go version (currently 1.24). Reading from `go.mod` means the workflow never drifts from the project's Go version.

#### Step 3: Extract version from tag

- **Command**: Strip the `v` prefix from `GITHUB_REF_NAME` and store in `GITHUB_ENV`.
  ```
  echo "VERSION=${GITHUB_REF_NAME#v}" >> "$GITHUB_ENV"
  ```
- **Example**: Tag `v1.2.3` produces `VERSION=1.2.3`. This matches the Makefile convention where `main.version` is set to `1.2.3` (without the `v` prefix), and the application itself prepends `v` in its output (`mactop v1.2.3`).
- **Stored as**: Environment variable `VERSION`, available to all subsequent steps.

#### Step 4: Build arm64 binary

- **Command**:
  ```
  CGO_ENABLED=1 GOARCH=arm64 GOOS=darwin \
    go build -ldflags "-s -w -X main.version=$VERSION" \
    -o bin/mactop-darwin-arm64 ./cmd/mactop
  ```
- **Notes**:
  - `CGO_ENABLED=1` is mandatory (IOKit/CoreFoundation access).
  - `-s -w` strips debug info and DWARF symbols for smaller binaries.
  - `-X main.version=$VERSION` injects the version from Step 3.
  - Output goes to `bin/mactop-darwin-arm64`.
  - `macos-latest` runners are Apple Silicon (arm64), so this is a native build.

#### Step 5: Build amd64 binary

- **Command**:
  ```
  CGO_ENABLED=1 GOARCH=amd64 GOOS=darwin \
    go build -ldflags "-s -w -X main.version=$VERSION" \
    -o bin/mactop-darwin-amd64 ./cmd/mactop
  ```
- **Notes**:
  - This is a cross-architecture CGO build (arm64 host building for amd64). This works because Apple's toolchain supports both architectures natively via the `-arch x86_64` flag, and Go's CGO integration on macOS handles this transparently when `GOARCH=amd64` is set.
  - If this cross-compile fails in practice (unlikely but possible with specific CGO configurations), the fallback is to use a matrix with `macos-13` (Intel) for the amd64 build and `macos-latest` (Apple Silicon) for arm64. See Section 14.9.

#### Step 6: Create GitHub Release and upload assets

- **Action**: `softprops/action-gh-release@v2`
- **Config**:
  - `tag_name`: `${{ github.ref_name }}` (the full tag, e.g., `v1.2.3`)
  - `name`: `mactop ${{ github.ref_name }}` (release title, e.g., `mactop v1.2.3`)
  - `generate_release_notes`: `true` (auto-generates changelog from commits since last tag)
  - `files`: `bin/mactop-darwin-arm64` and `bin/mactop-darwin-amd64`
  - `draft`: `false`
  - `prerelease`: auto-detect based on tag (tags containing `-rc`, `-beta`, `-alpha` should be marked as prerelease; `softprops/action-gh-release` does not do this automatically, so either set `prerelease: ${{ contains(github.ref_name, '-') }}` or leave as `false` for simplicity)

**Why `softprops/action-gh-release`**: It is the most widely used release action, handles both release creation and asset upload in one step, and supports `generate_release_notes`. The alternative (`gh release create` via CLI) also works but requires more scripting. Either approach is fine; the action is slightly cleaner.

**Alternative approach using `gh` CLI**:
```
gh release create "$GITHUB_REF_NAME" \
  --title "mactop $GITHUB_REF_NAME" \
  --generate-notes \
  bin/mactop-darwin-arm64 \
  bin/mactop-darwin-amd64
```
This would use the `GITHUB_TOKEN` already available in the runner environment. The `gh` CLI approach has no external action dependency and is simpler to audit. If the team prefers minimal action dependencies, use this instead of `softprops/action-gh-release`.

### 14.7 Permissions and Tokens

- **`contents: write`**: Must be declared at the job or workflow level. Required for creating releases and uploading assets.
- **`GITHUB_TOKEN`**: Automatically provided by GitHub Actions. No personal access tokens or secrets needed.
- The `softprops/action-gh-release` action (or `gh` CLI) uses `GITHUB_TOKEN` by default.

### 14.8 Complete Step Summary

```
Job: release
  runs-on: macos-latest
  permissions: contents: write

  Steps:
  1. actions/checkout@v4          (fetch-depth: 0)
  2. actions/setup-go@v5          (go-version-file: go.mod)
  3. Extract VERSION from tag     (shell: echo)
  4. Build arm64 binary           (go build, GOARCH=arm64)
  5. Build amd64 binary           (go build, GOARCH=amd64)
  6. Create release + upload      (softprops/action-gh-release@v2 OR gh CLI)
```

### 14.9 Edge Cases and Failure Modes

| Scenario | Impact | Mitigation |
|----------|--------|------------|
| amd64 CGO cross-compile fails on arm64 runner | No Intel binary produced | Fallback: use a matrix with `macos-13` (Intel runner) for amd64. See note below. |
| Tag pushed without matching code changes | Empty or broken release | No mitigation needed -- this is a human process issue. The build either succeeds or fails cleanly. |
| `macos-latest` runner changes OS version | Build behavior may change | Pin to `macos-15` if stability is critical. Currently `macos-latest` maps to macOS 14 (Sonoma) / 15 (Sequoia). The project targets macOS 13+, so forward compatibility is expected. |
| Go version in `go.mod` not available in `setup-go` | Setup step fails | `setup-go@v5` supports Go 1.24. Only a risk if upgrading to a very new Go version before the action supports it. |
| `softprops/action-gh-release` has a breaking change | Release step fails | Pin to `@v2`. Alternatively, use `gh release create` which has no external dependency. |
| Tag format is not semver (e.g., `vabc`) | VERSION env var is `abc`, build succeeds but version string is wrong | Document that tags must follow `vX.Y.Z` format. No automated enforcement needed -- the build will still work, just with a non-standard version string. |
| Release already exists for tag (re-push) | `gh-release` action fails with conflict | Either delete the existing release first, or do not re-push tags. If re-releases are needed, delete the release and tag, then re-tag. |
| Xcode CLI tools missing on runner | CGO compilation fails (missing headers) | GitHub-hosted macOS runners always include Xcode CLI tools. Only a risk with self-hosted runners. |

**Fallback matrix strategy** (if amd64 cross-compile proves unreliable):

If Step 5 (amd64 build on an arm64 runner) fails due to CGO cross-compilation issues, restructure as:

```
Job: build (matrix)
  strategy:
    matrix:
      include:
        - goarch: arm64
          runner: macos-latest
        - goarch: amd64
          runner: macos-13
  Steps: checkout, setup-go, build, upload-artifact

Job: release (needs: build)
  Steps: download-artifacts, create-release
```

This adds complexity (artifact passing between jobs, two runners) but guarantees native builds for each architecture. Only adopt this if the simpler single-job approach fails.

### 14.10 What the Workflow Does NOT Cover

- **Testing**: No `go test` step is included because the project's tests require macOS-specific hardware access (SMC, IOKit). A separate CI workflow for testing on PRs could be added later if needed, but is out of scope for the release workflow.
- **Code signing / notarization**: The binaries are not signed or notarized. macOS Gatekeeper will flag them on first run. Users will need to right-click and Open, or run `xattr -cr mactop-darwin-*`. If distribution grows, add a signing step using a Developer ID certificate stored as a GitHub secret.
- **Homebrew formula**: No automatic Homebrew tap update. This can be added as a post-release step later (e.g., `mislav/bump-homebrew-formula-action`).
- **Checksums**: No SHA256 checksums file is generated. Add one if users request verification. (Simple addition: `shasum -a 256 bin/* > bin/checksums.txt` and attach to release.)

---

## Appendix A: Reference Projects

These open-source projects use similar techniques and serve as implementation references:

- **[github.com/context-labs/mactop](https://github.com/context-labs/mactop)** -- Go-based macOS monitor (uses powermetrics, requires sudo)
- **[github.com/exelban/stats](https://github.com/exelban/stats)** -- Swift macOS menu bar app (good reference for IOKit SMC access)
- **[github.com/lavoiesl/osx-cpu-temp](https://github.com/lavoiesl/osx-cpu-temp)** -- C tool for SMC temperature reading
- **[github.com/freedomtan/sensors](https://github.com/freedomtan/sensors)** -- Apple Silicon sensor exploration
