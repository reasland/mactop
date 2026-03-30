# mactop

A real-time macOS system monitor for the terminal.

mactop displays live metrics for CPU (per-core and aggregate), GPU utilization,
memory, network throughput, disk usage, power/battery, and temperatures. It
includes time-series sparkline graphs that can be toggled on and off.

Built with Go, [bubbletea](https://github.com/charmbracelet/bubbletea), and
[lipgloss](https://github.com/charmbracelet/lipgloss). Uses CGO to access macOS
system APIs (IOKit, CoreFoundation, mach kernel, sysctl) directly -- no sudo
required. Sensors that are unavailable degrade gracefully.

## Requirements

- macOS (Apple Silicon or Intel)
- Xcode command-line tools (for building from source)

## Installation

### Download a release binary

Pre-built binaries for arm64 and amd64 are available on the
[Releases](https://github.com/reasland/mactop/releases) page.

Binaries are unsigned, so on first run you may need:

```
xattr -cr mactop-darwin-*
```

### Build from source

```
git clone https://github.com/reasland/mactop.git
cd mactop
make build
```

The binary is written to `bin/mactop`. To install it to `/usr/local/bin`:

```
make install
```

## Usage

```
mactop [flags]
```

| Flag | Description |
|------|-------------|
| `-i <duration>` | Refresh interval (default `1s`, minimum `250ms`) |
| `-no-color` | Disable color output |
| `-v` | Verbose logging to stderr |
| `-version` | Print version and exit |

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `g` | Toggle time-series graphs |
| `q` / `ctrl+c` | Quit |

## Project structure

```
cmd/mactop/       entrypoint
internal/
  collector/      system metric collectors (CPU, GPU, memory, network, disk, power, temperature)
  metrics/        data types
  smc/            SMC (System Management Controller) access
  platform/       low-level macOS API bindings (IOKit, mach, sysctl, IOHIDEventSystem)
  ui/             TUI model, layout, panels, styles, sparkline graphs
```

## Releasing

Push a version tag to trigger the GitHub Actions release workflow:

```
git tag v0.1.0
git push origin v0.1.0
```

This builds arm64 and amd64 binaries and creates a GitHub release automatically.
