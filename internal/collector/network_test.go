package collector

import "testing"

func TestNewNetworkCollector_Initializes(t *testing.T) {
	c := NewNetworkCollector()
	if c == nil {
		t.Fatal("NewNetworkCollector() returned nil")
	}
	if c.prev == nil {
		t.Error("prev map should be initialized, not nil")
	}
	if len(c.prev) != 0 {
		t.Errorf("prev map should be empty, got %d entries", len(c.prev))
	}
	if c.Data != nil {
		t.Errorf("Data should be nil initially, got %v", c.Data)
	}
}

func TestNetworkCollector_Name(t *testing.T) {
	c := NewNetworkCollector()
	if c.Name() != "network" {
		t.Errorf("Name() = %q, want %q", c.Name(), "network")
	}
}

func TestNewTempCollector_Initializes(t *testing.T) {
	c := NewTempCollector()
	if c == nil {
		t.Fatal("NewTempCollector() returned nil")
	}
	if c.Name() != "temperature" {
		t.Errorf("Name() = %q, want %q", c.Name(), "temperature")
	}
}
