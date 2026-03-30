# Decisions Log

## 2026-03-27 — Project Kickoff
- **Decision**: Build a macOS system monitor CLI tool called "mactop"
- **Rationale**: User requested a CLI for CPU, GPU, memory, network, disk, power, and temperature monitoring on macOS
- **Made by**: User + PM

## 2026-03-27 — Architecture Decisions
- **Decision**: Use Go with bubbletea/lipgloss for TUI
- **Rationale**: Go has the best TUI ecosystem (bubbletea); macOS APIs accessible via CGo; produces single binary
- **Made by**: Architect

## 2026-03-27 — Design Questions Resolved
- **Root access**: Try unprivileged first; gracefully degrade with "N/A". No sudo required. (PM decision)
- **Module path**: `github.com/rileyeasland/mactop` (PM decision)
- **GPU memory**: Show in GPU panel with "(unified)" note (Architect recommendation, PM approved)
- **Color scheme**: ANSI 256 adaptive + `--no-color` flag (Architect recommendation, PM approved)
- **Logging**: stderr only with `-v` flag (Architect recommendation, PM approved)

## 2026-03-27 — Review Phase Decisions
- **Mach port caching**: Cache `mach_host_self()` at package init (all 3 reviews recommended)
- **SMC key info caching**: Cache after first lookup to reduce CGo calls from 65 to ~13/tick
- **Verbose logging**: Implemented with `tea.LogToFile` rather than removing the flag
- **SwapUsage**: Switched to `C.struct_xsw_usage` for correct struct size

## 2026-03-27 — QA Phase
- **Test strategy**: Unit tests on pure Go logic only (no CGo mocking needed)
- **Coverage**: 30 tests across styles, SMC encoding, and layout
- **Bug found**: Missing CGo LDFLAGS in smc package when tested in isolation

## 2026-03-27 — Bug Fixes: Network & Temperature
- **Network root cause**: Overzealous bounds check in `parse_iflist2` broke loop on non-IFINFO routing messages (RTM_NEWADDR uses smaller struct than if_msghdr). Fix: only validate against sizeof(if_msghdr2) for RTM_IFINFO2 messages.
- **Temperature root cause (1)**: SMCKeyData_t struct was 52 bytes, kernel expects 80 bytes. Sub-struct alignment padding was missing. Fix: use proper sub-structs matching kernel driver layout.
- **Temperature root cause (2)**: macOS 26 (Tahoe) blocks SMC key access entirely. IOConnectCallStructMethod returns kIOReturnIPCError. This is a new OS restriction. Fix: graceful degradation with "N/A".
- **Temperature mitigation**: Added SMC key auto-discovery (enumerate keys starting with 'T'), expanded hardcoded key list from 13 to 28 keys for broader Apple Silicon coverage.
- **Made by**: PM (diagnosis) + Implementer (fix)

## 2026-03-27 — IOHIDEventSystem for Temperature
- **Decision**: Use IOHIDEventSystem private API as primary temperature source, SMC as fallback
- **Rationale**: Investigated how github.com/macmade/Hot implements temps. Hot uses `IOHIDEventSystemClientCreate` + `IOHIDServiceClientCopyEvent` from the private IOHIDFamily API. This is accessible without entitlements on macOS 26. Verified: returns 46 sensors on test machine (M4 Pro, macOS 26.4) where SMC returns 0.
- **Architecture**: HID primary → SMC fallback → "N/A" last resort. Aggregates die sensors into Avg/Max for clean display.
- **Made by**: PM (research from Hot repo) + Implementer

## 2026-03-27 — Graph Fixes: Network Layout + Temperature Graph
- **Network issue**: Two stacked 4-row sparklines (In+Out) made the panel too tall and the side-by-side label+graph rendering was visually garbled
- **Fix approach**: Render network In/Out as two compact single sparklines (2 rows each) stacked vertically, with label on its own line above the graph instead of side-by-side
- **Temperature graph**: Add MetricTemp to history, track max CPU temp, add sparkline to temperature panel
- **Made by**: User (reported) + PM (diagnosis)

## 2026-03-27 — Time-Series Graph Feature Design
- **Decision**: Add braille-character sparkline graphs inline within panels, toggled with `g` key
- **Rationale**: User wants to see data over time; braille gives 2x4 dot resolution per cell for smooth curves; in-house renderer (~80 lines) avoids heavy dependencies
- **Design details**: 256-sample ring buffer (~4 min history), graphs for CPU/GPU/Memory/Network
- **Default state**: Graphs OFF by default (toggle with `g`)
- **Network scaling**: Linear auto-scale (start simple, iterate if needed)
- **Per-core CPU**: Aggregate only (per-core would be too dense)
- **Graph colors**: Distinct colors per metric (user requested)
- **Made by**: Architect (design) + User (color decision) + PM (defaults)

## 2026-03-27 — Review Round 2 Fixes
- **buildHIDMetrics max bug**: Fixed by initializing max from first element instead of 0.0 (QA found, PM approved)
- **SMC static_assert**: Added compile-time size check for 80-byte struct (Security recommendation)
- **HID matching at init**: Moved hidSetTempMatching to NewHIDThermalReader to avoid per-tick CF allocations (Performance)
- **HID name caching**: Cache sensor names after first read, skip CGo calls on subsequent ticks (Performance)
- **Dead code removal**: Removed unused `initialized` field (Code review)
- **Private API risks accepted**: IOHIDEventSystem extern declarations are inherently fragile; accepted as necessary for macOS 26 temp support

## 2026-03-30 — GitHub Actions: Use macos-latest runners
- **Decision**: Use GitHub-hosted `macos-latest` runners instead of self-hosted `arc-runner-set`
- **Rationale**: Project requires CGO with macOS frameworks (IOKit, CoreFoundation). Cross-compilation from Linux is impractical. macOS minutes cost 10x but free tier covers ~40-60 builds/month.
- **Made by**: User + PM

## 2026-03-30 — GitHub Actions: Implementation choices
- **Release action**: Use `softprops/action-gh-release@v2` — cleanest single-step for release creation + asset upload
- **Prerelease detection**: Skip — all tags are stable releases, keep it simple
- **Checksums**: Yes — generate SHA256 checksums.txt and attach to release (one-liner, standard practice)
- **amd64 build**: Single-job cross-compile on arm64 runner first; fallback to matrix with macos-13 only if it fails
- **Made by**: PM (user deferred to cleanest option)

## 2026-03-30 — GitHub Actions: Trigger on version tags only
- **Decision**: Pipeline triggers only on `v*` tags, not on every push
- **Rationale**: Minimizes macOS runner minute usage. Spending limit stays at $0 — no surprise charges.
- **Made by**: User + PM
