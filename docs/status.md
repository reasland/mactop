# Status Log

## 2026-03-27 18:58 — Project Started
- **Completed**: Project plan created, docs initialized

## 2026-03-27 19:05 — Architecture Complete
- **Completed**: Architecture design

## 2026-03-27 19:14 — Implementation Complete
- **Completed**: All source files, build verified

## 2026-03-27 19:17 — Reviews Complete  
- **Completed**: Code, security, performance reviews

## 2026-03-27 19:24 — Review Fixes Applied
- **Completed**: All 15 review issues fixed

## 2026-03-27 19:28 — QA Complete
- **Completed**: 30 unit tests passing

## 2026-03-27 19:44 — Bug Fixes Applied
- **Fixed**: Network collector not showing data (parse_iflist2 bounds check)
- **Fixed**: Temperature SMC struct layout (52→80 bytes with proper sub-structs)
- **Known limitation**: macOS 26+ blocks SMC access — temperatures show "N/A" without elevated privileges
- **Status**: ✅ Build passing, tests passing

## 2026-03-27 19:51 — Temperature Fix via IOHIDEventSystem
- **Research**: Studied github.com/macmade/Hot implementation → uses IOHIDEventSystem private API
- **Implemented**: New `internal/platform/iohid.go` wrapping IOHIDEventSystemClient
- **Result**: 46 temperature sensors now accessible on macOS 26 without sudo/entitlements
- **Display**: Aggregated to CPU Die Avg, CPU Die Max, CPU Calibrated, Battery, SSD
- **Status**: ✅ All features working — CPU, GPU, Memory, Network, Disk, Power, Temperatures

## 2026-03-27 — Graph Fixes: Network Layout + Temperature Graph
- **Network fix**: Replaced broken side-by-side label+graph layout with stacked (label above graph). Reduced network graph height from 4 to 2 rows each.
- **All panels**: `appendSparkline` now uses stacked layout — simpler, fewer allocations, no ANSI alignment bugs.
- **Temperature graph**: Added `MetricTemp` to history (tracks max sensor value per tick). Red sparkline (color 196) with auto-scaling (floor 30°C).
- **Review nits fixed**: Corrected misleading "Max" label to "Temp", removed unused `graphHeight` param from network, deduplicated auto-scale logic with `autoScaleMaxWithFloor`.
- **Status**: ✅ Build + vet + tests all pass

## 2026-03-27 — Phase 2: Time-Series Graph Feature Complete
- **Design**: Braille sparkline graphs (U+2800-U+28FF) inline within panels, toggled with `g` key
- **New files**: `history.go` (ring buffer), `sparkline.go` (braille renderer), `panels_test.go`
- **Modified**: `app.go`, `layout.go`, `panels.go`, `styles.go`, `help.go`, test files
- **Review fixes applied**: zero-capacity guard, bounds checks, IsInf check, flattened grid, cached styles, copy() optimization, named constants
- **Tests**: 97 total (44 new), all passing, 0 bugs found
- **Distinct sparkline colors**: CPU=cyan(39), GPU=magenta(171), Memory=yellow(220), NetIn=green(40), NetOut=orange(208)
- **Status**: ✅ Build + vet + tests all pass

## 2026-03-27 20:09 — Review Round 2 Complete
- **Code Review**: Approve with changes — 2 MAJOR (fixed), 5 MINOR (4 fixed, 1 accepted)
- **Security Review**: 3 MEDIUM (1 fixed with static_assert, 2 accepted as inherent to private API), 7 LOW
- **Performance Review**: 3 NEW MEDIUM (2 fixed: HID matching moved to init, name caching added), 5 unfixed from R1
- **QA**: 60/60 tests passing (30 new), 1 bug found and fixed (max initialization)
- **All fixes applied**: build OK, tests OK, vet OK
- **Status**: ✅ All critical/major issues resolved

## 2026-03-30 — Phase 3: GitHub Actions Release Pipeline Complete
- **Design**: Architect designed single-job workflow on macos-latest, triggered on v* tags
- **Implementation**: `.github/workflows/release.yml` created
- **Code review**: Approved with 5 fixes (SHA pinning, job-level perms, remove defaults, explicit checksums, remove fetch-depth)
- **Security review**: High-severity SHA pinning fixed; medium items (tag-to-branch check, SLSA provenance) deferred as low risk for personal project
- **All fixes applied and re-reviewed**: ✅ Approved
- **Status**: ✅ Ready to use — push a `v*` tag to create a release
