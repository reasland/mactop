# Time-Series Graph Feature Design

## Overview

Add sparkline/braille-dot graphs to the mactop TUI that plot metric history over
time. Users toggle graph visibility with the `g` key. The graphs render inline
within the existing panel layout using Unicode braille characters for high
resolution in minimal vertical space.

```
+------------------------------------------------------+
| mactop v0.x.x                                        |
+----------------------------+-------------------------+
| CPU Usage                  | Memory                  |
|   P0: |||||||------  70%   |   Used: 12.3 GB ... 76% |
|   ...                      |   ...                   |
|   Avg: |||||-------  50%   |   Total: 16.0 GB        |
|                            |                         |
|   [CPU %%  braille graph]  |   [MEM %% braille graph]|
|   ^^^^50%                  |   ^^^^76%               |
+----------------------------+-------------------------+
| GPU Usage                  | Temperatures            |
|   |||||||||||------  85%   |   CPU P-Core: 72.1 C    |
|                            |   ...                   |
|   [GPU %% braille graph]   |                         |
+----------------------------+-------------------------+
| Network                                              |
|   en0:  In: 1.2 MB/s  Out: 340 KB/s                 |
|   [Net In braille] [Net Out braille]                 |
+------------------------------------------------------+
```

When graphs are toggled OFF, the dashboard looks exactly as it does today.

---

## 1. Data Model: Ring Buffer

### New file: `internal/ui/history.go`

A fixed-size ring buffer stores the last N float64 samples for each tracked
metric. The buffer is generic over the metric name so we can reuse one type.

```go
// MetricID identifies a tracked time-series metric.
type MetricID int

const (
    MetricCPU MetricID = iota
    MetricGPU
    MetricMemory
    MetricNetIn
    MetricNetOut
    metricCount // sentinel for iteration
)

// RingBuffer is a fixed-capacity circular buffer of float64 values.
type RingBuffer struct {
    data  []float64
    head  int  // next write position
    count int  // number of valid entries (<=cap)
}

// NewRingBuffer creates a buffer that holds `capacity` samples.
func NewRingBuffer(capacity int) *RingBuffer

// Push appends a value, overwriting the oldest if full.
func (rb *RingBuffer) Push(v float64)

// Values returns the stored samples in chronological order (oldest first).
// The returned slice length equals rb.count (may be < capacity initially).
func (rb *RingBuffer) Values() []float64

// Len returns the number of valid samples.
func (rb *RingBuffer) Len() int

// MetricHistory holds ring buffers for all tracked metrics.
type MetricHistory struct {
    buffers [metricCount]*RingBuffer
}

// NewMetricHistory creates history buffers with the given sample capacity.
func NewMetricHistory(capacity int) *MetricHistory

// Record pushes the current metric snapshot into the appropriate buffers.
func (h *MetricHistory) Record(m metrics.SystemMetrics)

// Get returns the ring buffer for a given metric.
func (h *MetricHistory) Get(id MetricID) *RingBuffer
```

### Buffer capacity

The capacity should match the graph width in columns. Since the graph will be
rendered at the width of the panel it lives in, and braille characters encode 2
horizontal dots per character, a 60-column panel yields 120 data points. We
store a fixed maximum of **256 samples** -- more than enough for any reasonable
terminal width. When rendering, we take the rightmost `width * 2` samples (or
fewer if the buffer is not yet full).

With a default 1-second tick interval, 256 samples gives roughly 4 minutes of
history.

### Where it lives in the Model

```go
// In Model (app.go):
type Model struct {
    // ... existing fields ...
    history    *MetricHistory
    showGraphs bool
}
```

`history` is a pointer (like the collectors) so bubbletea's value-receiver
copies share state. It is initialized in `NewModel`:

```go
history: NewMetricHistory(256),
```

### Recording samples

At the end of `collectAll()`, add:

```go
m.history.Record(m.metrics)
```

The `Record` method extracts the relevant floats:

```go
func (h *MetricHistory) Record(m metrics.SystemMetrics) {
    h.buffers[MetricCPU].Push(m.CPU.Aggregate)
    h.buffers[MetricGPU].Push(m.GPU.Utilization)

    memPct := 0.0
    if m.Memory.Total > 0 {
        memPct = float64(m.Memory.Used) / float64(m.Memory.Total) * 100
    }
    h.buffers[MetricMemory].Push(memPct)

    // Sum throughput across all interfaces.
    var inTotal, outTotal float64
    for _, iface := range m.Network {
        inTotal += iface.BytesInPS
        outTotal += iface.BytesOutPS
    }
    h.buffers[MetricNetIn].Push(inTotal)
    h.buffers[MetricNetOut].Push(outTotal)
}
```

---

## 2. Graph Rendering

### New file: `internal/ui/sparkline.go`

A self-contained sparkline renderer with no external dependencies. Uses Unicode
braille characters (U+2800..U+28FF) for 2x4 dot resolution per cell, giving
smooth-looking line graphs in minimal terminal space.

### Braille character encoding

Each braille cell is 2 dots wide by 4 dots tall:

```
Dot positions:    Bit values:
  1  4              0x01  0x08
  2  5              0x02  0x10
  3  6              0x04  0x20
  7  8              0x40  0x80
```

Braille codepoint = 0x2800 + (sum of set dot bits).

### Renderer API

```go
// SparklineOpts controls sparkline rendering.
type SparklineOpts struct {
    Width  int     // number of terminal columns for the graph
    Height int     // number of terminal rows (each row = 4 braille dots)
    Min    float64 // y-axis minimum (use 0 for percentages)
    Max    float64 // y-axis maximum (use 100 for percentages)
    Color  lipgloss.TerminalColor // line color
}

// RenderSparkline renders a braille-dot sparkline graph from the given data.
// If len(data) > Width*2, only the rightmost Width*2 points are used.
// If len(data) < Width*2, the graph is right-aligned (left side is blank).
func RenderSparkline(data []float64, opts SparklineOpts) string
```

### Rendering algorithm

1. **Truncate/pad data**: Take at most `Width * 2` rightmost samples from the
   data slice. If fewer samples exist, left-pad with NaN (rendered as blank).

2. **Scale to dot grid**: The vertical resolution is `Height * 4` dots. Map
   each value `v` to a Y dot position:
   `dotY = int((v - Min) / (Max - Min) * float64(Height*4 - 1))`
   Clamp to `[0, Height*4-1]`.

3. **Plot into grid**: Create a `[Height][Width]` grid of uint8 (braille bit
   masks). For each pair of adjacent data points (x=0,1 -> col 0; x=2,3 ->
   col 1), set the corresponding dot bits in the appropriate row/column cell.

4. **Render to string**: Convert each cell to the braille character
   `rune(0x2800 + bits)`, join into rows separated by newlines, apply color via
   lipgloss.

### Scaling strategy for different metric types

| Metric    | Min | Max              | Notes                              |
|-----------|-----|------------------|------------------------------------|
| CPU %     | 0   | 100              | Fixed scale, always 0-100%         |
| GPU %     | 0   | 100              | Fixed scale, always 0-100%         |
| Memory %  | 0   | 100              | Fixed scale, always 0-100%         |
| Net In    | 0   | auto (max * 1.2) | Auto-scale to peak in the window   |
| Net Out   | 0   | auto (max * 1.2) | Auto-scale to peak in the window   |

For auto-scaled metrics, compute `Max = max(data...) * 1.2` with a floor of
1024 bytes/s to avoid division by zero or absurdly scaled graphs for idle
interfaces.

### Graph height

Default: **4 terminal rows** (= 16 braille dots of vertical resolution). This
is compact enough to fit below the panel content without dominating the screen.
For narrow layouts where vertical space is tighter, use 3 rows.

### Y-axis label

A minimal label showing the current value and scale is rendered to the left of
the graph. For percentage metrics: just the current value. For network: the
auto-scaled max in human-readable format.

```
  CPU  52%  [braille sparkline..........................]
  GPU  85%  [braille sparkline..........................]
  MEM  76%  [braille sparkline..........................]
  In 1.2M/s [braille sparkline.........................]
```

The label takes ~10 columns; the sparkline fills the rest.

---

## 3. UI Integration

### Toggle behavior

- Key: `g` toggles `m.showGraphs` (default: `false`, graphs hidden).
- When ON: each relevant panel gets a sparkline appended below its content.
- When OFF: panels render exactly as they do today (zero visual change).

### Key binding addition in `app.go` Update():

```go
case "g":
    m.showGraphs = !m.showGraphs
    return m, nil
```

### Status bar update in `layout.go`:

```go
"  q: quit   r: reset peaks   g: graphs   ?: help   Refresh: " + intervalStr
```

### Panel modifications in `panels.go`

Each render function gains an optional graph section. Rather than modifying the
existing function signatures (which would be a larger diff), add new wrapper
functions:

```go
// renderCPUPanelWithGraph appends a sparkline to the CPU panel content.
func renderCPUPanelWithGraph(cpu metrics.CPUMetrics, history *RingBuffer, width int) string {
    base := renderCPUPanel(cpu, width)
    if history.Len() < 2 {
        return base
    }
    graph := RenderSparkline(history.Values(), SparklineOpts{
        Width:  width - 12,
        Height: 4,
        Min:    0,
        Max:    100,
        Color:  barFilledColor,
    })
    label := fmt.Sprintf("  CPU %3.0f%%  ", cpu.Aggregate)
    return base + "\n" + labelStyle.Render(label) + graph
}
```

Same pattern for GPU, Memory, and Network panels.

### Layout integration in `layout.go`

The `renderWideLayout` and `renderNarrowLayout` functions receive the history
and showGraphs flag. When `showGraphs` is true, they call the `*WithGraph`
panel renderers; otherwise, they call the existing renderers unchanged.

**Updated function signatures:**

```go
func renderDashboard(m metrics.SystemMetrics, width, height int,
    version, intervalStr string, history *MetricHistory, showGraphs bool) string

func renderWideLayout(m metrics.SystemMetrics, width int, title, status string,
    history *MetricHistory, showGraphs bool) string

func renderNarrowLayout(m metrics.SystemMetrics, width int, title, status string,
    history *MetricHistory, showGraphs bool) string
```

The `View()` method in `app.go` passes the new arguments:

```go
return renderDashboard(m.metrics, m.width, m.height, m.version, m.intervalStr,
    m.history, m.showGraphs)
```

### Wide layout with graphs

Graphs appear **inside** each panel, below the existing content. The panel
border expands to accommodate the extra rows. This keeps the two-column layout
intact -- the graph simply makes each panel taller.

### Narrow layout with graphs

Same approach: graphs render inside panels, below the text. In narrow mode, the
graph width equals the panel width and the graph height drops to 3 rows to save
vertical space.

### Scroll/overflow

If the dashboard content exceeds the terminal height, it is simply truncated
(current behavior). Graphs are a modest addition (~5 rows per panel), and the
toggle lets users disable them if screen space is tight.

---

## 4. File Changes Summary

### New files

| File                          | Purpose                                    |
|-------------------------------|--------------------------------------------|
| `internal/ui/history.go`     | RingBuffer, MetricHistory, MetricID consts  |
| `internal/ui/history_test.go` | Unit tests for ring buffer                 |
| `internal/ui/sparkline.go`   | RenderSparkline, braille encoding           |
| `internal/ui/sparkline_test.go` | Unit tests for sparkline renderer        |

### Modified files

| File                      | Changes                                          |
|---------------------------|--------------------------------------------------|
| `internal/ui/app.go`     | Add `history`, `showGraphs` to Model; init in NewModel; record in collectAll; `g` key handler; pass to renderDashboard |
| `internal/ui/layout.go`  | Update renderDashboard/wide/narrow signatures to accept history+showGraphs; update status bar text; conditionally call graph panel renderers |
| `internal/ui/panels.go`  | Add `renderCPUPanelWithGraph`, `renderGPUPanelWithGraph`, `renderMemoryPanelWithGraph`, `renderNetworkPanelWithGraph` functions |
| `internal/ui/help.go`    | Add `g` key to help text                         |

### Files NOT changed

- `internal/metrics/types.go` -- no schema changes needed
- `internal/collector/*` -- collectors are unaffected
- `go.mod` -- no new dependencies

---

## 5. New Types and Interfaces Summary

```go
// internal/ui/history.go
type MetricID int
const MetricCPU, MetricGPU, MetricMemory, MetricNetIn, MetricNetOut MetricID

type RingBuffer struct { ... }
func NewRingBuffer(capacity int) *RingBuffer
func (*RingBuffer) Push(float64)
func (*RingBuffer) Values() []float64
func (*RingBuffer) Len() int

type MetricHistory struct { ... }
func NewMetricHistory(capacity int) *MetricHistory
func (*MetricHistory) Record(metrics.SystemMetrics)
func (*MetricHistory) Get(MetricID) *RingBuffer

// internal/ui/sparkline.go
type SparklineOpts struct {
    Width, Height int
    Min, Max      float64
    Color         lipgloss.TerminalColor
}
func RenderSparkline(data []float64, opts SparklineOpts) string
```

No new interfaces are introduced. The design uses concrete types to keep things
simple and avoid unnecessary abstraction.

---

## 6. Edge Cases and Failure Modes

### Insufficient data

When the history buffer has fewer than 2 samples, skip graph rendering and show
the panel as normal. The graph will gradually "fill in" from the right as
samples accumulate.

### Terminal too narrow

If the available graph width (panel width minus label) is less than 10 columns,
skip the graph for that panel. The label alone would be wider than the graph.

### Terminal too short

When graphs push content beyond the terminal height, bubbletea will truncate.
The toggle (`g`) lets users disable graphs to reclaim space. This is acceptable
-- the existing app already truncates if the terminal is too small.

### Zero/NaN values

GPU metrics may be unavailable (`Available: false`). If the GPU buffer contains
only zero values (or GPU is not available), skip the GPU graph. The ring buffer
always stores 0.0 for unavailable readings; this renders as a flat line at the
bottom, which is harmless but not useful.

### Memory overhead

256 float64 values per metric x 5 metrics = 10,240 bytes. Negligible.

### Braille character support

Braille characters (U+2800-U+28FF) are supported by virtually all modern
terminal emulators and monospace fonts. If a user's terminal cannot render them,
they will see boxes or question marks. This is an acceptable degradation -- the
underlying text panels remain fully functional.

### Concurrency

The Model is single-threaded (bubbletea's Update/View cycle). No locking is
needed for the ring buffer since pushes happen in Update (collectAll) and reads
happen in View, which bubbletea guarantees do not run concurrently.

---

## 7. Trade-offs

### Braille vs. block characters

**Chosen: Braille.** Each cell gives 2x4 dot resolution vs. block characters
which give 1x2 at best. This produces visibly smoother curves. The trade-off is
that braille characters may not render in extremely limited terminals (e.g.,
bare Linux console), but mactop is macOS-only, where Terminal.app, iTerm2, and
all common terminals support braille.

### In-house renderer vs. third-party library

**Chosen: In-house.** Libraries like `termdash` or `termui` are heavyweight and
bring their own rendering model that conflicts with bubbletea/lipgloss. A
braille sparkline renderer is ~80 lines of code. Not worth a dependency.

### Graphs inside panels vs. separate graph panel

**Chosen: Inside panels.** Putting graphs in a separate full-width panel would
require duplicating metric labels and would not visually associate each graph
with its metric. Embedding the graph below the panel content keeps the
association clear and avoids layout rework.

### Fixed capacity vs. dynamic

**Chosen: Fixed 256.** Simpler, predictable memory, no allocation churn. If the
terminal is wider than 128 columns (256 dots / 2 dots per column), the graph
still renders correctly -- it just does not fill the full width until the buffer
wraps around. This is acceptable since terminals wider than 128 columns are
uncommon and the graph fills in within minutes.

---

## 8. Open Questions

1. **Default state of graphs** -- Should graphs be ON or OFF by default? The
   current design defaults to OFF to preserve the existing appearance. If the
   team prefers graphs on by default, flip the initial value of `showGraphs` in
   `NewModel`.

2. **Network graph placement** -- Network throughput can vary by orders of
   magnitude (0 to 10+ Gbps). The auto-scaling approach works but can look
   jumpy. An alternative is a log-scale Y axis, at the cost of added
   complexity. Recommendation: start with linear auto-scale and revisit if user
   feedback says it is hard to read.

3. **Per-core CPU graphs** -- The current design only graphs aggregate CPU.
   Graphing all cores individually would be visually noisy and tall. If there is
   demand, a future iteration could add a separate "CPU detail" view (toggled
   with a different key) that shows per-core sparklines.

4. **Graph color** -- The design reuses `barFilledColor` (green). Each metric
   could have its own color for visual differentiation. This is a small cosmetic
   decision that can be made during implementation.
