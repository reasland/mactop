package ui

import (
	"fmt"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rileyeasland/mactop/internal/collector"
	"github.com/rileyeasland/mactop/internal/metrics"
)

// historyCapacity is the number of samples retained per metric for time-series graphs.
const historyCapacity = 256

// TickMsg signals that a new collection cycle should run.
type TickMsg time.Time

func tickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// Model is the top-level bubbletea model for the mactop dashboard.
// All mutable collector fields MUST be pointers so that value-receiver
// copies in bubbletea's Update loop share state with the original
// model passed to tea.NewProgram.
type Model struct {
	metrics  metrics.SystemMetrics
	cpu      *collector.CPUCollector
	gpu      *collector.GPUCollector
	memory   *collector.MemoryCollector
	network  *collector.NetworkCollector
	disk     *collector.DiskCollector
	power    *collector.PowerCollector
	temp     *collector.TempCollector
	interval time.Duration
	width    int
	height   int
	history    *MetricHistory
	showHelp   bool
	showGraphs bool
	version    string
	verbose    bool
	intervalStr string
	logger      *log.Logger
}

// NewModel creates a new Model with all collectors initialized.
// If verbose is true, collector errors are logged to a temporary file
// (path printed to stderr on startup).
func NewModel(interval time.Duration, version string, verbose bool) Model {
	m := Model{
		cpu:         collector.NewCPUCollector(),
		gpu:         collector.NewGPUCollector(),
		memory:      collector.NewMemoryCollector(),
		network:     collector.NewNetworkCollector(),
		disk:        collector.NewDiskCollector(),
		power:       collector.NewPowerCollector(),
		temp:        collector.NewTempCollector(),
		history:     NewMetricHistory(historyCapacity),
		interval:    interval,
		version:     version,
		verbose:     verbose,
		intervalStr: fmt.Sprintf("%v", interval),
	}

	if verbose {
		f, err := tea.LogToFile("mactop-debug.log", "mactop")
		if err == nil {
			m.logger = log.New(f, "", log.LstdFlags)
		}
	}

	return m
}

func (m Model) Init() tea.Cmd {
	return tickCmd(m.interval)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.temp.Close()
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "r":
			// Reset is a no-op for now; could reset peak tracking.
			return m, nil
		case "g":
			m.showGraphs = !m.showGraphs
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case TickMsg:
		m.collectAll()
		return m, tickCmd(m.interval)
	}

	return m, nil
}

// Close releases resources held by the model's collectors (e.g., SMC connection).
// Safe to call multiple times.
func (m *Model) Close() {
	m.temp.Close()
}

func (m *Model) collectAll() {
	collectors := []collector.Collector{
		m.cpu, m.gpu, m.memory, m.network,
		m.disk, m.power, m.temp,
	}
	for _, c := range collectors {
		if err := c.Collect(); err != nil {
			if m.verbose && m.logger != nil {
				m.logger.Printf("collector %s: %v", c.Name(), err)
			}
		}
	}
	m.metrics = metrics.SystemMetrics{
		Timestamp:   time.Now(),
		CPU:         m.cpu.Data,
		GPU:         m.gpu.Data,
		Memory:      m.memory.Data,
		Network:     m.network.Data,
		Disk:        m.disk.Data,
		Power:       m.power.Data,
		Temperature: m.temp.Data,
	}
	m.history.Record(m.metrics)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing..."
	}

	if m.showHelp {
		return renderHelp(m.width, m.height)
	}

	return renderDashboard(m.metrics, m.width, m.height, m.version, m.intervalStr,
		m.history, m.showGraphs)
}
