package ui

import "github.com/rileyeasland/mactop/internal/metrics"

// MetricID identifies a tracked time-series metric.
type MetricID int

const (
	MetricCPU MetricID = iota
	MetricGPU
	MetricMemory
	MetricNetIn
	MetricNetOut
	MetricTemp
	metricCount // sentinel for iteration
)

// RingBuffer is a fixed-capacity circular buffer of float64 values.
type RingBuffer struct {
	data  []float64
	head  int // next write position
	count int // number of valid entries (<=cap)
}

// NewRingBuffer creates a buffer that holds capacity samples.
// If capacity <= 0, it is clamped to 1 to prevent panics.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &RingBuffer{
		data: make([]float64, capacity),
	}
}

// Push appends a value, overwriting the oldest if full.
func (rb *RingBuffer) Push(v float64) {
	rb.data[rb.head] = v
	rb.head = (rb.head + 1) % len(rb.data)
	if rb.count < len(rb.data) {
		rb.count++
	}
}

// Values returns the stored samples in chronological order (oldest first).
func (rb *RingBuffer) Values() []float64 {
	if rb.count == 0 {
		return nil
	}
	out := make([]float64, rb.count)
	start := (rb.head - rb.count + len(rb.data)) % len(rb.data)
	// Use two copy() calls for the tail and head segments of the circular buffer.
	tail := len(rb.data) - start
	if tail >= rb.count {
		copy(out, rb.data[start:start+rb.count])
	} else {
		copy(out, rb.data[start:])
		copy(out[tail:], rb.data[:rb.count-tail])
	}
	return out
}

// ValuesInto appends chronological samples into the provided buffer, reusing
// its capacity when possible. Returns the resulting slice.
func (rb *RingBuffer) ValuesInto(buf []float64) []float64 {
	if rb.count == 0 {
		return buf[:0]
	}
	buf = buf[:0]
	if cap(buf) >= rb.count {
		buf = buf[:rb.count]
	} else {
		buf = make([]float64, rb.count)
	}
	start := (rb.head - rb.count + len(rb.data)) % len(rb.data)
	tail := len(rb.data) - start
	if tail >= rb.count {
		copy(buf, rb.data[start:start+rb.count])
	} else {
		copy(buf, rb.data[start:])
		copy(buf[tail:], rb.data[:rb.count-tail])
	}
	return buf
}

// Len returns the number of valid samples.
func (rb *RingBuffer) Len() int {
	return rb.count
}

// MetricHistory holds ring buffers for all tracked metrics.
type MetricHistory struct {
	buffers [metricCount]*RingBuffer
}

// NewMetricHistory creates history buffers with the given sample capacity.
func NewMetricHistory(capacity int) *MetricHistory {
	h := &MetricHistory{}
	for i := 0; i < int(metricCount); i++ {
		h.buffers[i] = NewRingBuffer(capacity)
	}
	return h
}

// Record pushes the current metric snapshot into the appropriate buffers.
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

	// Record the maximum temperature across all sensors.
	var maxTemp float64
	if m.Temperature.Available {
		for _, s := range m.Temperature.Sensors {
			if s.Value > maxTemp {
				maxTemp = s.Value
			}
		}
	}
	h.buffers[MetricTemp].Push(maxTemp)
}

// Get returns the ring buffer for a given metric.
// Returns nil if id is out of range.
func (h *MetricHistory) Get(id MetricID) *RingBuffer {
	if id < 0 || id >= metricCount {
		return nil
	}
	return h.buffers[id]
}
