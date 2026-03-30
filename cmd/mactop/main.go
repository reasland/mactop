package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rileyeasland/mactop/internal/ui"
)

// version is set via -ldflags at build time.
var version = "dev"

func main() {
	interval := flag.Duration("i", 1*time.Second, "refresh interval (minimum 250ms)")
	showVersion := flag.Bool("version", false, "print version and exit")
	verbose := flag.Bool("v", false, "verbose logging to stderr")
	noColor := flag.Bool("no-color", false, "disable color output")
	flag.Parse()

	if *showVersion {
		fmt.Printf("mactop v%s\n", version)
		os.Exit(0)
	}

	// Clamp minimum interval to 250ms.
	if *interval < 250*time.Millisecond {
		*interval = 250 * time.Millisecond
	}

	if *noColor {
		lipgloss.SetColorProfile(0)
	}

	model := ui.NewModel(*interval, "v"+version, *verbose)
	defer model.Close()

	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
