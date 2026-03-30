package ui

import (
	"testing"

	"github.com/rileyeasland/mactop/internal/metrics"
)

func TestRingBuffer_PushAndLen(t *testing.T) {
	rb := NewRingBuffer(4)
	if rb.Len() != 0 {
		t.Errorf("new buffer Len() = %d, want 0", rb.Len())
	}

	rb.Push(1.0)
	rb.Push(2.0)
	if rb.Len() != 2 {
		t.Errorf("after 2 pushes Len() = %d, want 2", rb.Len())
	}
}

func TestRingBuffer_ValuesChronological(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Push(10)
	rb.Push(20)
	rb.Push(30)

	vals := rb.Values()
	if len(vals) != 3 {
		t.Fatalf("Values() len = %d, want 3", len(vals))
	}
	want := []float64{10, 20, 30}
	for i, v := range vals {
		if v != want[i] {
			t.Errorf("Values()[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestRingBuffer_OverwriteOldest(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	rb.Push(4) // overwrites 1

	vals := rb.Values()
	if len(vals) != 3 {
		t.Fatalf("Values() len = %d, want 3", len(vals))
	}
	want := []float64{2, 3, 4}
	for i, v := range vals {
		if v != want[i] {
			t.Errorf("Values()[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestRingBuffer_EmptyValues(t *testing.T) {
	rb := NewRingBuffer(5)
	vals := rb.Values()
	if vals != nil {
		t.Errorf("empty buffer Values() should be nil, got %v", vals)
	}
}

func TestRingBuffer_FullWrap(t *testing.T) {
	rb := NewRingBuffer(3)
	for i := 0; i < 10; i++ {
		rb.Push(float64(i))
	}
	if rb.Len() != 3 {
		t.Errorf("Len() = %d, want 3", rb.Len())
	}
	vals := rb.Values()
	want := []float64{7, 8, 9}
	for i, v := range vals {
		if v != want[i] {
			t.Errorf("Values()[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestMetricHistory_RecordAndGet(t *testing.T) {
	h := NewMetricHistory(10)

	m := metrics.SystemMetrics{
		CPU:    metrics.CPUMetrics{Aggregate: 55.5},
		GPU:    metrics.GPUMetrics{Utilization: 30.0, Available: true},
		Memory: metrics.MemoryMetrics{Total: 1000, Used: 750},
		Network: []metrics.NetworkInterface{
			{BytesInPS: 100, BytesOutPS: 200},
			{BytesInPS: 50, BytesOutPS: 25},
		},
	}

	h.Record(m)

	cpuVals := h.Get(MetricCPU).Values()
	if len(cpuVals) != 1 || cpuVals[0] != 55.5 {
		t.Errorf("CPU = %v, want [55.5]", cpuVals)
	}

	gpuVals := h.Get(MetricGPU).Values()
	if len(gpuVals) != 1 || gpuVals[0] != 30.0 {
		t.Errorf("GPU = %v, want [30.0]", gpuVals)
	}

	memVals := h.Get(MetricMemory).Values()
	if len(memVals) != 1 || memVals[0] != 75.0 {
		t.Errorf("Memory = %v, want [75.0]", memVals)
	}

	netInVals := h.Get(MetricNetIn).Values()
	if len(netInVals) != 1 || netInVals[0] != 150 {
		t.Errorf("NetIn = %v, want [150]", netInVals)
	}

	netOutVals := h.Get(MetricNetOut).Values()
	if len(netOutVals) != 1 || netOutVals[0] != 225 {
		t.Errorf("NetOut = %v, want [225]", netOutVals)
	}
}

func TestMetricHistory_ZeroMemoryTotal(t *testing.T) {
	h := NewMetricHistory(5)
	h.Record(metrics.SystemMetrics{
		Memory: metrics.MemoryMetrics{Total: 0, Used: 0},
	})
	memVals := h.Get(MetricMemory).Values()
	if len(memVals) != 1 || memVals[0] != 0.0 {
		t.Errorf("Memory with zero total = %v, want [0.0]", memVals)
	}
}

func TestNewRingBuffer_ZeroCapacity(t *testing.T) {
	// capacity 0 should be clamped to 1, not panic.
	rb := NewRingBuffer(0)
	if rb == nil {
		t.Fatal("NewRingBuffer(0) returned nil")
	}
	rb.Push(42)
	if rb.Len() != 1 {
		t.Errorf("Len() = %d, want 1", rb.Len())
	}
	vals := rb.Values()
	if len(vals) != 1 || vals[0] != 42 {
		t.Errorf("Values() = %v, want [42]", vals)
	}
}

func TestNewRingBuffer_NegativeCapacity(t *testing.T) {
	// negative capacity should be clamped to 1, not panic.
	rb := NewRingBuffer(-5)
	if rb == nil {
		t.Fatal("NewRingBuffer(-5) returned nil")
	}
	rb.Push(7)
	rb.Push(8) // overwrites 7
	if rb.Len() != 1 {
		t.Errorf("Len() = %d, want 1", rb.Len())
	}
	vals := rb.Values()
	if len(vals) != 1 || vals[0] != 8 {
		t.Errorf("Values() = %v, want [8]", vals)
	}
}

func TestMetricHistory_GetInvalidNegativeID(t *testing.T) {
	h := NewMetricHistory(5)
	if got := h.Get(MetricID(-1)); got != nil {
		t.Error("Get(-1) should return nil for negative MetricID")
	}
}

func TestMetricHistory_GetInvalidOutOfRangeID(t *testing.T) {
	h := NewMetricHistory(5)
	if got := h.Get(MetricID(999)); got != nil {
		t.Error("Get(999) should return nil for out-of-range MetricID")
	}
}

func TestMetricHistory_GetSentinelID(t *testing.T) {
	h := NewMetricHistory(5)
	if got := h.Get(metricCount); got != nil {
		t.Error("Get(metricCount) should return nil for sentinel value")
	}
}

func TestRingBuffer_ValuesInto_ReusesBuffer(t *testing.T) {
	rb := NewRingBuffer(5)
	rb.Push(10)
	rb.Push(20)
	rb.Push(30)

	// Provide a pre-allocated buffer with enough capacity.
	buf := make([]float64, 0, 10)
	result := rb.ValuesInto(buf)

	if len(result) != 3 {
		t.Fatalf("ValuesInto len = %d, want 3", len(result))
	}
	want := []float64{10, 20, 30}
	for i, v := range result {
		if v != want[i] {
			t.Errorf("ValuesInto()[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestRingBuffer_ValuesInto_EmptyBuffer(t *testing.T) {
	rb := NewRingBuffer(5)
	buf := make([]float64, 0, 10)
	result := rb.ValuesInto(buf)
	if len(result) != 0 {
		t.Errorf("ValuesInto on empty ring should return len 0, got %d", len(result))
	}
}

func TestRingBuffer_ValuesInto_InsufficientCapacity(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := 0; i < 5; i++ {
		rb.Push(float64(i))
	}

	// Provide a buffer with insufficient capacity; should allocate a new one.
	buf := make([]float64, 0, 2)
	result := rb.ValuesInto(buf)

	if len(result) != 5 {
		t.Fatalf("ValuesInto len = %d, want 5", len(result))
	}
	for i := 0; i < 5; i++ {
		if result[i] != float64(i) {
			t.Errorf("ValuesInto()[%d] = %f, want %f", i, result[i], float64(i))
		}
	}
}

func TestRingBuffer_ValuesInto_WrappedBuffer(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Push(1)
	rb.Push(2)
	rb.Push(3)
	rb.Push(4) // wraps: oldest is now 2

	buf := make([]float64, 0, 5)
	result := rb.ValuesInto(buf)

	want := []float64{2, 3, 4}
	if len(result) != 3 {
		t.Fatalf("ValuesInto len = %d, want 3", len(result))
	}
	for i, v := range result {
		if v != want[i] {
			t.Errorf("ValuesInto()[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestMetricHistory_RecordAllFiveMetrics(t *testing.T) {
	h := NewMetricHistory(10)
	m := metrics.SystemMetrics{
		CPU:    metrics.CPUMetrics{Aggregate: 42.5},
		GPU:    metrics.GPUMetrics{Utilization: 65.0, Available: true},
		Memory: metrics.MemoryMetrics{Total: 8000, Used: 6000},
		Network: []metrics.NetworkInterface{
			{BytesInPS: 500, BytesOutPS: 300},
			{BytesInPS: 200, BytesOutPS: 100},
		},
	}
	h.Record(m)

	tests := []struct {
		name string
		id   MetricID
		want float64
	}{
		{"CPU aggregate", MetricCPU, 42.5},
		{"GPU utilization", MetricGPU, 65.0},
		{"Memory percentage", MetricMemory, 75.0}, // 6000/8000 * 100
		{"Network in total", MetricNetIn, 700.0},   // 500 + 200
		{"Network out total", MetricNetOut, 400.0},  // 300 + 100
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vals := h.Get(tc.id).Values()
			if len(vals) != 1 {
				t.Fatalf("expected 1 value, got %d", len(vals))
			}
			if vals[0] != tc.want {
				t.Errorf("got %f, want %f", vals[0], tc.want)
			}
		})
	}
}

func TestMetricHistory_RecordMultipleSamples(t *testing.T) {
	h := NewMetricHistory(5)
	for i := 0; i < 3; i++ {
		h.Record(metrics.SystemMetrics{
			CPU: metrics.CPUMetrics{Aggregate: float64(10 * (i + 1))},
		})
	}
	vals := h.Get(MetricCPU).Values()
	if len(vals) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(vals))
	}
	want := []float64{10, 20, 30}
	for i, v := range vals {
		if v != want[i] {
			t.Errorf("sample[%d] = %f, want %f", i, v, want[i])
		}
	}
}

func TestMetricHistory_RecordNoNetworkInterfaces(t *testing.T) {
	h := NewMetricHistory(5)
	h.Record(metrics.SystemMetrics{
		CPU:     metrics.CPUMetrics{Aggregate: 10},
		Network: nil,
	})
	inVals := h.Get(MetricNetIn).Values()
	outVals := h.Get(MetricNetOut).Values()
	if len(inVals) != 1 || inVals[0] != 0.0 {
		t.Errorf("NetIn with no interfaces = %v, want [0]", inVals)
	}
	if len(outVals) != 1 || outVals[0] != 0.0 {
		t.Errorf("NetOut with no interfaces = %v, want [0]", outVals)
	}
}
