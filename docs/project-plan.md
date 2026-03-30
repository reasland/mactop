# Project Plan: mactop — macOS System Monitor CLI

## Overview
A CLI program that displays real-time macOS system metrics including CPU, GPU, memory, network, disk usage, power, and temperatures.

## Tasks

| # | Task | Owner | Status |
|---|------|-------|--------|
| 1 | Architecture & Design | architect | ✅ Complete |
| 2 | Project scaffold & build setup | implementer | ✅ Complete |
| 3 | CPU monitoring module | implementer | ✅ Complete |
| 4 | GPU monitoring module | implementer | ✅ Complete |
| 5 | Memory monitoring module | implementer | ✅ Complete |
| 6 | Network monitoring module | implementer | ✅ Complete |
| 7 | Disk monitoring module | implementer | ✅ Complete |
| 8 | Power/Battery module | implementer | ✅ Complete |
| 9 | Temperature monitoring module | implementer | ✅ Complete |
| 10 | TUI/Display layer | implementer | ✅ Complete |
| 11 | Code review | code-reviewer | ✅ Complete |
| 12 | Security review | security-engineer | ✅ Complete |
| 13 | Performance review | performance-engineer | ✅ Complete |
| 14 | Review fixes | implementer | ✅ Complete |
| 15 | QA & testing | qa-engineer | ✅ Complete |

## Phase 2: Time-Series Graph Feature

| # | Task | Owner | Status |
|---|------|-------|--------|
| 16 | Design time-series graph feature | architect | ✅ Complete |
| 17 | Implement history buffer + graph renderer | implementer | ✅ Complete |
| 18 | Code review | code-reviewer | ✅ Approved |
| 19 | Security review | security-engineer | ✅ Approved |
| 20 | Performance review | performance-engineer | ✅ Approved |
| 21 | Review fixes | implementer | ✅ Complete |
| 22 | Re-review (code + security + perf) | all reviewers | ✅ Approved |
| 23 | QA & testing | qa-engineer | ✅ Complete (97 tests pass) |

## Status: Phase 2 ✅ COMPLETE

## Phase 3: GitHub Actions Build & Release Pipeline

| # | Task | Owner | Status | Dependencies |
|---|------|-------|--------|--------------|
| 24 | Design pipeline architecture | architect | ✅ Complete | - |
| 25 | Implement workflow YAML | implementer | ✅ Complete | 24 |
| 26 | Code review | code-reviewer | ✅ Approved | 25 |
| 27 | Security review | security-engineer | ✅ Approved | 26 |
| 28 | Review fixes applied | implementer | ✅ Complete | 26, 27 |
| 29 | Re-review | code-reviewer | ✅ Approved | 28 |

### Constraints
- GitHub-hosted `macos-latest` runners (CGO + macOS frameworks required)
- Trigger only on version tags (`v*`) to minimize runner minute usage
- GitHub spending limit at $0 — no surprise charges
- Build arm64 and amd64 binaries
- Publish binaries as GitHub Release assets
