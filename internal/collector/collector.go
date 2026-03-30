package collector

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
