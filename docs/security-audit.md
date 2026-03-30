# Security Audit: Time-Series Sparkline Graph Feature

**Date:** 2026-03-27
**Auditor:** Claude Opus 4.6 (automated security review)
**Scope:** New sparkline graph feature -- `internal/ui/history.go`, `internal/ui/sparkline.go`, and modifications to `internal/ui/app.go`, `internal/ui/layout.go`, `internal/ui/panels.go`

---

## Findings

### Finding 1: Panic on Zero-Capacity RingBuffer

- **Severity:** Low
- **Location:** `internal/ui/history.go:33-34`
- **Issue:** `NewRingBuffer(0)` allocates a zero-length slice. The first call to `Push()` will panic with an index-out-of-range error on `rb.data[rb.head]`, and the modulus on line 34 (`% len(rb.data)`) will panic with integer divide by zero.
- **Impact:** If `NewMetricHistory` is ever called with `capacity <= 0`, the application will crash on the first metric collection tick. Currently the hardcoded value is 256 (line 59 of `app.go`), so this is not reachable in production, but the API is exported and unguarded.
- **Remediation:** Add a guard at the top of `NewRingBuffer`:
  ```go
  func NewRingBuffer(capacity int) *RingBuffer {
      if capacity <= 0 {
          capacity = 1
      }
      return &RingBuffer{
          data: make([]float64, capacity),
      }
  }
  ```
- **References:** CWE-369 (Divide By Zero), CWE-129 (Improper Validation of Array Index)

### Finding 2: Unchecked MetricID in MetricHistory.Get()

- **Severity:** Low
- **Location:** `internal/ui/history.go:94-96`
- **Issue:** `Get(id MetricID)` performs no bounds check on the `id` parameter before indexing into the fixed-size array `h.buffers[id]`. An out-of-range `MetricID` value would cause a panic (index out of range on a fixed array).
- **Impact:** Currently all call sites use the defined `MetricCPU`..`MetricNetOut` constants, so this is not reachable. However, the type is exported (`MetricID` is `int`), and any caller passing an invalid value would crash the program.
- **Remediation:** Either make the type unexported, or add a bounds check:
  ```go
  func (h *MetricHistory) Get(id MetricID) *RingBuffer {
      if id < 0 || int(id) >= len(h.buffers) {
          return NewRingBuffer(1) // return safe empty buffer
      }
      return h.buffers[id]
  }
  ```
- **References:** CWE-129 (Improper Validation of Array Index)

### Finding 3: Integer Overflow Potential in Sparkline Grid Dimensions

- **Severity:** Low
- **Location:** `internal/ui/sparkline.go:37-38`
- **Issue:** `samples := opts.Width * 2` and `totalDots := opts.Height * 4` are computed as plain `int` multiplications. With extremely large `Width` or `Height` values (greater than `math.MaxInt / 4`), these could theoretically overflow on 32-bit platforms, leading to negative values and unexpected behavior in the subsequent slice allocations and loop bounds.
- **Impact:** On 64-bit systems (the only platform mactop targets -- macOS on Apple Silicon), this is not practically reachable since terminal dimensions are bounded by the OS. On a hypothetical 32-bit build, a `Height` of ~536 million would trigger overflow, which is not a real terminal size.
- **Remediation:** No action required for the current target platform. If portability to 32-bit platforms is ever desired, add sanity caps:
  ```go
  if opts.Width > 1000 || opts.Height > 1000 {
      return ""
  }
  ```
- **References:** CWE-190 (Integer Overflow)

### Finding 4: Per-Render Allocations in Values() and RenderSparkline()

- **Severity:** Informational
- **Location:** `internal/ui/history.go:45`, `internal/ui/sparkline.go:52,70-73`
- **Issue:** Every call to `Values()` allocates a new slice of up to 256 float64s. `RenderSparkline` allocates an additional `dotYs` slice and a 2D grid. These are called on every render tick when graphs are enabled (potentially every 500ms-2s). With 5 metrics, this is ~10 slice allocations per tick.
- **Impact:** This is not a security vulnerability but a minor GC pressure concern. The allocations are small (a few KB total) and bounded, so there is no risk of unbounded memory growth or denial of service.
- **Remediation:** Optionally, pre-allocate reusable buffers in the `MetricHistory` or pass a scratch buffer to `RenderSparkline`. This is a performance optimization, not a security fix.
- **References:** N/A

### Finding 5: NaN Handling is Correct but Inf is Not Explicitly Handled

- **Severity:** Informational
- **Location:** `internal/ui/sparkline.go:54-57`
- **Issue:** The renderer correctly skips `NaN` values (line 54-56). However, `+Inf` or `-Inf` float64 values are not explicitly checked. An `Inf` value would pass the `IsNaN` check, and the expression `(v - opts.Min) / rng * float64(totalDots-1)` with `v = +Inf` would produce `+Inf`, which when cast to `int` yields a platform-dependent value. The subsequent clamping on lines 59-65 would catch positive overflow (`dy >= totalDots` clamps to `totalDots - 1`), but `int(+Inf)` in Go is undefined behavior per the spec.
- **Impact:** In practice, the metrics feeding this code (CPU%, GPU%, memory%) are bounded percentages and network throughput values from OS APIs. Inf values are not expected from the collectors. The clamping lines provide a safety net, but the `int(float64)` conversion of Inf is technically undefined.
- **Remediation:** Add an explicit Inf check alongside the NaN check:
  ```go
  if math.IsNaN(v) || math.IsInf(v, 0) {
      dotYs[i] = -1
      continue
  }
  ```
- **References:** CWE-681 (Incorrect Conversion between Numeric Types)

### Finding 6: No Concurrency Protection on RingBuffer or MetricHistory

- **Severity:** Informational
- **Location:** `internal/ui/history.go:18-22`, `internal/ui/history.go:58-61`
- **Issue:** The `RingBuffer` and `MetricHistory` structs have no mutex or synchronization. If `Push()` and `Values()` were ever called concurrently, data races would occur.
- **Impact:** In the current bubbletea architecture, the Model's `Update()` (which calls `collectAll` -> `Record`) and `View()` (which calls `Values()`) are invoked sequentially by the bubbletea runtime on the same goroutine, so there is no concurrent access. This is safe as-is but would become a bug if the architecture changed to use background goroutines for collection.
- **Remediation:** Document the single-goroutine assumption. Optionally, add a `sync.RWMutex` if future-proofing is desired.
- **References:** CWE-362 (Race Condition)

---

## Items Verified as Safe

1. **Division by zero in sparkline range calculation** (`sparkline.go:46-49`): Correctly guarded -- when `opts.Max - opts.Min <= 0`, the range is forced to 1.0.

2. **Division by zero in memory percentage** (`history.go:78`): Correctly guarded with `if m.Memory.Total > 0`.

3. **Braille bit manipulation** (`sparkline.go:13-18,92,115`): The `brailleBits` lookup table uses `uint8` values that are OR'd into `uint8` grid cells. The maximum possible value is `0x01|0x02|0x04|0x08|0x10|0x20|0x40|0x80 = 0xFF`, which fits in `uint8`. The braille base offset `0x2800 + int(bits)` yields a maximum of `0x28FF`, which is within the valid Unicode braille block. No overflow possible.

4. **Bounded memory usage**: The ring buffer capacity is fixed at 256 (hardcoded in `app.go:59`). Each buffer stores 256 float64s = 2KB. With 5 metrics, total history storage is ~10KB, permanently bounded.

5. **No injection vectors**: The code renders to a TUI via lipgloss/bubbletea. There is no shell execution, no file I/O, no network I/O, no SQL, no template rendering. The metric data comes from OS system calls, not user input.

6. **Grid bounds checks** (`sparkline.go:91,114`): All grid writes are guarded by explicit `row >= 0 && row < opts.Height && col >= 0 && col < opts.Width` bounds checks.

7. **Empty data paths**: `RenderSparkline` returns `""` for empty data, zero width, or zero height. The `*WithGraph` panel functions check `history.Len() < 2` before rendering. These prevent panics on startup before enough data has been collected.

8. **Test coverage**: Both `history_test.go` and `sparkline_test.go` cover key edge cases including empty buffers, wrap-around, zero dimensions, equal min/max, and excess data truncation.

---

## Executive Summary

**Overall Risk Posture: Low**

This feature has a small attack surface. It processes only internal system metrics (no external/user input), renders only to a local TUI (no network output), and performs no file or network I/O. The code is well-structured with appropriate defensive checks at critical points.

**Finding Summary:**
| Severity | Count |
|---|---|
| Critical | 0 |
| High | 0 |
| Medium | 0 |
| Low | 3 |
| Informational | 3 |

**Recommended Priority Order:**

1. **Finding 5** (Inf handling) -- Trivial one-line fix that eliminates undefined behavior from `int(+Inf)` conversion. Low effort, removes a theoretical correctness issue.
2. **Finding 1** (zero-capacity guard) -- Simple defensive check in `NewRingBuffer`. Prevents a panic if the API is ever misused.
3. **Finding 2** (MetricID bounds check) -- Simple guard on an exported API. Prevents panic from invalid input.
4. Findings 3, 4, 6 are informational and require no immediate action.

No blocking issues were identified. The feature is suitable for merge from a security perspective.

---

# Security Re-Audit: Time-Series Sparkline Graph (Post-Fix Verification)

**Date:** 2026-03-27
**Auditor:** Claude Opus 4.6 (automated security review)
**Scope:** Verification of three security fixes applied to `internal/ui/history.go` and `internal/ui/sparkline.go`

---

## Fix Verification

### Finding 1 (Original): Panic on Zero-Capacity RingBuffer -- FIXED

- **Location:** `internal/ui/history.go:26-29`
- **Status:** Verified fixed.
- **Assessment:** The guard `if capacity <= 0 { capacity = 1 }` is correctly placed at the top of `NewRingBuffer` before the `make` call. This eliminates both the index-out-of-range panic in `Push()` and the divide-by-zero in the modulus operation. The fix matches the recommended remediation exactly.

### Finding 2 (Original): Unchecked MetricID in Get() -- FIXED

- **Location:** `internal/ui/history.go:127-132`
- **Status:** Verified fixed.
- **Assessment:** The bounds check `if id < 0 || id >= metricCount` correctly guards against out-of-range access. Returning `nil` (rather than a safe empty buffer as originally suggested) is an acceptable choice -- callers must handle a nil return, but this is idiomatic Go and makes misuse obvious rather than silently returning empty data. No nil-dereference risk was found in existing call sites (the panel rendering functions check `history.Len() < 2` which would catch a nil buffer via a nil pointer dereference only if `Get()` returned nil -- however, all current call sites use valid MetricID constants, so this path is not reachable in practice).

### Finding 5 (Original): Undefined Behavior from int(+Inf) -- FIXED

- **Location:** `internal/ui/sparkline.go:54`
- **Status:** Verified fixed.
- **Assessment:** The check `math.IsNaN(v) || math.IsInf(v, 0)` on line 54 correctly guards against both NaN and Inf (positive and negative) before the float-to-int conversion on line 58. Values that match are assigned `dotYs[i] = -1` and skipped in all subsequent processing (lines 76-77 and 95-96 check `dy < 0`). This eliminates the undefined behavior from `int(+Inf)`.

---

## New Issues Identified

No new security issues were introduced by the fixes.

---

## Remaining Items from Original Audit (Unchanged)

The following informational findings from the original audit remain unaddressed but require no action:

- **Finding 3** (Integer overflow on 32-bit): Not applicable to the macOS/ARM64 target platform.
- **Finding 4** (Per-render allocations): Performance concern only, not security-relevant. Note that `ValuesInto` (lines 64-83 of history.go) was added as a buffer-reuse optimization, partially addressing this.
- **Finding 6** (No concurrency protection): Safe under bubbletea's single-goroutine model.

---

## Executive Summary

**Overall Risk Posture: Low (unchanged)**

All three recommended fixes have been correctly applied. The implementations match or improve upon the suggested remediations. No regressions or new vulnerabilities were introduced. The feature remains suitable for merge from a security perspective.

---

# Security Audit: GitHub Actions Release Workflow

**Date:** 2026-03-30
**Auditor:** Claude Opus 4.6 (automated security review)
**Scope:** `.github/workflows/release.yml` -- CI/CD release pipeline triggered on `v*` tag pushes, building Go binaries for macOS and creating GitHub Releases via `softprops/action-gh-release@v2`

---

## Findings

### Finding 1: Third-Party Actions Not Pinned to Commit SHAs

- **Severity:** High
- **Location:** `.github/workflows/release.yml:16,20,44`
- **Issue:** All three third-party actions are pinned to mutable version tags rather than immutable commit SHAs:
  - `actions/checkout@v4`
  - `actions/setup-go@v5`
  - `softprops/action-gh-release@v2`

  Git tags are mutable. If any of these repositories are compromised, an attacker can move the `v4`/`v5`/`v2` tag to point at malicious code. The workflow would then execute the attacker's code with `contents: write` permissions and access to `GITHUB_TOKEN`.
- **Impact:** A supply chain attack via a compromised action could: exfiltrate the `GITHUB_TOKEN`, tamper with built binaries before upload, inject malicious code into releases, or modify repository contents (given `contents: write`). The `softprops/action-gh-release` action is a community-maintained action (not a GitHub-official action), making it a higher-risk dependency.
- **Remediation:** Pin all actions to their full commit SHA. For example:
  ```yaml
  - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683 # v4.2.2
  - uses: actions/setup-go@d35c59abb061a4a6fb18e82ac0862c26744d6ab5 # v5.5.0
  - uses: softprops/action-gh-release@c95fe1489396fe8a9eb87c0abf8aa5b2ef267fda # v2.2.1
  ```
  Use Dependabot or Renovate with the `github-actions` ecosystem to receive automated PRs when pinned SHAs have newer releases. Include the version tag as a trailing comment for readability.
- **References:** CWE-829 (Inclusion of Functionality from Untrusted Control Sphere), OWASP CI/CD Top 10 -- CICD-3 (Dependency Chain Abuse), [GitHub docs: Security hardening for GitHub Actions](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions#using-third-party-actions)

### Finding 2: Permissions Granted at Workflow Level Instead of Job Level

- **Severity:** Medium
- **Location:** `.github/workflows/release.yml:8-9`
- **Issue:** The `contents: write` permission is declared at the top-level `permissions:` block (workflow level) rather than within the specific `jobs.release.permissions` block. While this workflow currently has only one job, workflow-level permissions apply to all current and future jobs. If a new job is added later (e.g., a test or lint job), it would inherit `contents: write` unnecessarily, violating the principle of least privilege.
- **Impact:** Any future job added to this workflow would silently inherit write access to repository contents. A compromised or misconfigured step in a non-release job could then modify repository contents or create releases.
- **Remediation:** Move the permissions block to the job level:
  ```yaml
  jobs:
    release:
      runs-on: macos-latest
      permissions:
        contents: write
      steps:
        ...
  ```
- **References:** CWE-250 (Execution with Unnecessary Privileges), [GitHub docs: Defining access for GITHUB_TOKEN](https://docs.github.com/en/actions/security-guides/automatic-token-authentication#modifying-the-permissions-for-the-github_token)

### Finding 3: Tag Push Trigger Allows Any Contributor with Push Access to Create Releases

- **Severity:** Medium
- **Location:** `.github/workflows/release.yml:3-6`
- **Issue:** The workflow triggers on any tag push matching `v*`. Any collaborator with write access to the repository can push a tag and trigger a full release build. There is no environment protection, no required reviewers, and no branch protection rule constraining which commits can be tagged. An attacker who compromises a collaborator account (or a malicious insider) could:
  1. Push a commit with modified source code to any branch.
  2. Tag that commit with `v1.0.0-malicious`.
  3. The workflow would build binaries from the attacker's code and publish a GitHub Release.

  The `fetch-depth: 0` on checkout (line 18) ensures the full history is available, but the build is performed from whatever commit the tag points to, which may not be on the `main` branch or reviewed via a pull request.
- **Impact:** A malicious release could distribute compromised binaries to users who download from GitHub Releases.
- **Remediation:** Consider one or more of the following mitigations:
  1. **Use a GitHub Environment** with required reviewers on the release job, so a human must approve before the release proceeds.
  2. **Add a verification step** that checks the tagged commit exists on the `main` branch:
     ```yaml
     - name: Verify tag is on main branch
       run: |
         git merge-base --is-ancestor "$GITHUB_SHA" origin/main || \
           { echo "ERROR: Tag does not point to a commit on main"; exit 1; }
     ```
  3. **Enable tag protection rules** in the repository settings to restrict who can create tags matching `v*`.
- **References:** CWE-284 (Improper Access Control), OWASP CI/CD Top 10 -- CICD-1 (Insufficient Flow Control Mechanisms)

### Finding 4: No Binary Integrity Verification Between Build and Upload

- **Severity:** Medium
- **Location:** `.github/workflows/release.yml:28-54`
- **Issue:** The binaries are built, checksums are generated, and then both are uploaded in subsequent steps within the same job. While the checksums provide post-hoc verification for end users, there is no signing, no SLSA provenance attestation, and no mechanism for users to verify that the binaries were actually produced by this workflow. The checksums file itself could be tampered with if any step between generation and upload is compromised (e.g., a compromised `softprops/action-gh-release` action could replace both binaries and checksums).
- **Impact:** Users downloading binaries have checksums for integrity (bit-rot, download corruption) but no cryptographic proof of provenance. If the build pipeline is compromised, both binaries and checksums would be attacker-controlled, making the checksums useless for supply chain security.
- **Remediation:**
  1. **Generate SLSA provenance** using `slsa-framework/slsa-github-generator` to create a verifiable attestation that ties the binary to this specific workflow, repository, and commit.
  2. Alternatively, use `actions/attest-build-provenance` (GitHub's native attestation) to create sigstore-backed attestations for each artifact.
  3. At minimum, use `gh attestation create` or GPG signing so users can verify artifacts were produced by this repository's CI.
- **References:** CWE-345 (Insufficient Verification of Data Authenticity), SLSA framework (https://slsa.dev), OWASP CI/CD Top 10 -- CICD-5 (Insufficient Artifact Integrity Validation)

### Finding 5: Runner Uses `macos-latest` Instead of a Pinned Version

- **Severity:** Low
- **Location:** `.github/workflows/release.yml:13`
- **Issue:** The workflow uses `runs-on: macos-latest`, which is a floating alias that GitHub periodically updates to point to a new macOS major version. This can cause:
  - Unexpected build breakage when the runner image changes.
  - Different toolchain versions (Xcode, SDK, system libraries) being linked into the binary without notice.
  - Difficulty reproducing a specific release build.
- **Impact:** This is primarily a reproducibility concern rather than a direct security vulnerability. However, non-reproducible builds undermine the ability to audit whether a given binary matches the expected source code.
- **Remediation:** Pin to a specific runner image version:
  ```yaml
  runs-on: macos-14  # Apple Silicon runner
  ```
  Update deliberately via PR when a new runner image is needed.
- **References:** CWE-1104 (Use of Unmaintained Third-Party Components)

### Finding 6: VERSION Extraction is Safe but Uses Environment File Injection Vector

- **Severity:** Low
- **Location:** `.github/workflows/release.yml:26`
- **Issue:** The version extraction:
  ```yaml
  run: echo "VERSION=${GITHUB_REF_NAME#v}" >> "$GITHUB_ENV"
  ```
  Uses `GITHUB_REF_NAME` which, for this trigger, is the tag name (e.g., `v1.2.3`). The value is derived from a git ref name, which has strict character restrictions (no spaces, no newlines, limited special characters per `git check-ref-format`). This makes multiline injection into `GITHUB_ENV` (which would require a newline to set additional variables) not feasible.

  However, the pattern `v*` is broad. A tag like `v$(malicious)` would not execute (it is parameter expansion, not command substitution, and is quoted on the right side of `>>`), but unusual characters in the tag name would flow into `VERSION` and then into the `-ldflags` string on lines 31 and 37. The `-X main.version=$VERSION` is unquoted in the shell, so a tag name containing spaces could cause unexpected ldflags parsing.
- **Impact:** Low. Git ref naming rules prevent most dangerous characters. A tag with spaces is invalid in git. The practical risk is negligible, but the pattern could be tightened.
- **Remediation:** Validate the version string format:
  ```yaml
  - name: Extract version from tag
    run: |
      VERSION="${GITHUB_REF_NAME#v}"
      if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
        echo "ERROR: Invalid version format: $VERSION"
        exit 1
      fi
      echo "VERSION=$VERSION" >> "$GITHUB_ENV"
  ```
  This ensures only semver-compatible strings are accepted. Also quote the variable in ldflags:
  ```yaml
  go build -ldflags "-s -w -X 'main.version=$VERSION'" ...
  ```
- **References:** CWE-78 (OS Command Injection), CWE-20 (Improper Input Validation), [GitHub docs: Security hardening -- script injections](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions#understanding-the-risk-of-script-injections)

### Finding 7: Checkout with fetch-depth: 0 Exposes Full Repository History

- **Severity:** Informational
- **Location:** `.github/workflows/release.yml:17-18`
- **Issue:** The checkout uses `fetch-depth: 0` which clones the complete repository history. This is likely present to support `generate_release_notes: true` in the release action (which needs commit history to generate changelogs). However, the full history is available to all subsequent steps, increasing the data surface if any step is compromised.
- **Impact:** Minimal for a public repository. For private repositories, a compromised action step would have access to the full commit history rather than just the tagged commit.
- **Remediation:** No change needed if `generate_release_notes` is desired. If changelog generation is not needed, use the default `fetch-depth: 1` for a shallow clone.
- **References:** CWE-200 (Exposure of Sensitive Information)

### Finding 8: Release Published Immediately as Non-Draft

- **Severity:** Informational
- **Location:** `.github/workflows/release.yml:49-50`
- **Issue:** The release is created with `draft: false` and `prerelease: false`, meaning it is immediately published and visible to all users as soon as the workflow completes. There is no human review gate between the build and the public release.
- **Impact:** If a flawed or malicious build completes successfully, it is immediately available for download. A draft-then-publish workflow would provide an opportunity for manual verification.
- **Remediation:** Consider creating releases as drafts initially:
  ```yaml
  draft: true
  ```
  Then manually publish after verifying the artifacts. This adds friction but provides a final human checkpoint.
- **References:** Defense in depth principle

---

## Items Verified as Safe

1. **GITHUB_TOKEN usage**: The token is not explicitly referenced in the workflow. It is passed implicitly by `softprops/action-gh-release@v2` via the default `github.token` context. The token is not exposed in logs, environment variables, or step outputs. The `contents: write` scope is the minimum required for creating releases and uploading assets (the release action needs write access to create the release object and upload files).

2. **No artifact persistence between jobs**: The workflow uses a single job, so there is no risk of artifact tampering during inter-job transfer (e.g., via `actions/upload-artifact` / `actions/download-artifact`).

3. **CGO_ENABLED=1 builds**: CGO is required for macOS system calls (IOKit framework bindings). This is expected and necessary for the application's functionality.

4. **Checksum generation**: The `shasum -a 256` command on line 41 generates SHA-256 checksums, which is the current standard. The `cd bin &&` pattern avoids including path prefixes in the checksum file, which is correct for user verification with `shasum -c`.

5. **No use of pull_request_target**: The workflow only triggers on `push` to tags, not on pull request events. There is no risk of the well-known `pull_request_target` vulnerability where untrusted PR code runs with write permissions.

6. **No secrets beyond GITHUB_TOKEN**: The workflow does not reference any repository secrets, PATs, or external service credentials. The attack surface from secret exfiltration is limited to the auto-generated `GITHUB_TOKEN`.

---

## Executive Summary

**Overall Risk Posture: Moderate**

The release workflow is functional and follows common patterns, but has several supply chain security gaps that are standard in the industry yet represent real risk for a project distributing compiled binaries.

**Finding Summary:**
| Severity | Count |
|---|---|
| Critical | 0 |
| High | 1 |
| Medium | 3 |
| Low | 2 |
| Informational | 2 |

**Recommended Priority Order:**

1. **Finding 1 -- Pin actions to commit SHAs** (High): This is the single most impactful improvement. Unpinned third-party actions are the most common supply chain attack vector in GitHub Actions. The `softprops/action-gh-release` action is community-maintained and has direct access to `GITHUB_TOKEN` with write permissions. Pin all three actions to specific commit SHAs and enable Dependabot for `github-actions` to stay current.

2. **Finding 3 -- Restrict tag-based release triggers** (Medium): Add verification that the tagged commit is on the main branch, or use GitHub Environments with required reviewers. This prevents a compromised collaborator from releasing arbitrary code.

3. **Finding 2 -- Scope permissions to job level** (Medium): A quick one-line change that improves defense in depth for future workflow modifications.

4. **Finding 4 -- Add build provenance attestation** (Medium): Implement SLSA provenance or GitHub artifact attestation so users can cryptographically verify that binaries were produced by this repository's CI pipeline. This is increasingly expected for security-conscious projects distributing compiled binaries.

5. **Finding 6 -- Validate version format and quote ldflags** (Low): A straightforward hardening measure.

6. **Finding 5 -- Pin runner image** (Low): Improves reproducibility.

7. **Findings 7-8** (Informational): Address based on project risk tolerance.

No critical vulnerabilities were identified. The workflow does not expose secrets, does not use dangerous triggers like `pull_request_target`, and uses the minimum required permission (`contents: write`). The primary risk is supply chain integrity -- the combination of unpinned actions, no commit verification on tags, and no artifact attestation means that a sophisticated attacker with access to either a collaborator account or a compromised upstream action could distribute malicious binaries through the release pipeline.
