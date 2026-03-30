# Security Review: mactop

**Date:** 2026-03-27
**Scope:** Full codebase audit -- all Go source files, CGo bindings, IOKit/Mach/SMC interfaces, collectors, and UI
**Reviewer:** Automated security review (Claude)
**Codebase version:** v0.1.0 (pre-release)

---

## Findings

### FINDING-01: TOCTOU Race in SysctlString Buffer Allocation

- **Severity:** MEDIUM
- **Location:** `internal/platform/sysctl.go:47-68` (SysctlString function)
- **Issue:** The function performs a two-step sysctl call: first to query the buffer size, then to read the data. Between these two calls, the sysctl value can change (e.g., `kern.hostname` being updated), and the returned data size could exceed the originally allocated buffer. While `sysctlbyname` will not write beyond the `bufLen` passed in, it may return a truncated result or a non-zero error code with no handling specific to `ENOMEM`.
- **Impact:** In practice, the kernel truncates data to the buffer size, so this cannot cause a buffer overflow. However, the function may silently return truncated strings. This is a correctness issue more than a security vulnerability in this context.
- **Remediation:** Retry the size query and read in a loop if the second call fails with `ENOMEM`, or pre-allocate a generously sized buffer. The Go standard library's `syscall.Sysctl()` already handles this pattern correctly and could be used instead.
- **References:** CWE-367 (Time-of-check Time-of-use)

---

### FINDING-02: Network Collector -- Insufficient Bounds Validation in C Buffer Parser

- **Severity:** MEDIUM
- **Location:** `internal/collector/network.go:26-50` (parse_iflist2 C function)
- **Issue:** The `parse_iflist2` function casts raw buffer bytes to `struct if_msghdr` and reads `ifm_msglen` and `ifm_type` without verifying that there are enough remaining bytes in the buffer to safely read these struct fields. If `bufLen` is non-zero but smaller than `sizeof(struct if_msghdr)`, or if a malformed `ifm_msglen` points beyond the buffer boundary, the parser will read out-of-bounds memory. Additionally, when `ifm_type == RTM_IFINFO2`, the code casts to `struct if_msghdr2` (a larger struct) without verifying that `ifm_msglen >= sizeof(struct if_msghdr2)`.
- **Impact:** Out-of-bounds read from a Go-allocated buffer. Since the buffer comes from the kernel sysctl call and not from untrusted input, exploitation requires a compromised kernel or a kernel bug. The practical risk is low but the code pattern is unsafe.
- **Remediation:** Add bounds checks before each struct dereference:
  ```c
  if (ptr + sizeof(struct if_msghdr) > end) break;
  // ... and after reading ifm_msglen:
  if (ifm->ifm_msglen < sizeof(struct if_msghdr)) break;
  if (ifm->ifm_type == RTM_IFINFO2) {
      if (ifm->ifm_msglen < sizeof(struct if_msghdr2)) { ptr += ifm->ifm_msglen; continue; }
  }
  ```
- **References:** CWE-125 (Out-of-bounds Read), CWE-787 (Out-of-bounds Write)

---

### FINDING-03: get_if_name Buffer Size Mismatch

- **Severity:** LOW
- **Location:** `internal/collector/network.go:53-56` (get_if_name C function)
- **Issue:** The `get_if_name` function accepts a `nameLen` parameter but passes the buffer directly to `if_indextoname()`, which expects a buffer of at least `IFNAMSIZ` bytes and does not accept a length parameter. The caller does pass an `IFNAMSIZ`-sized buffer, so this is safe in practice, but the `nameLen` parameter creates a false sense that smaller buffers would be handled correctly.
- **Impact:** None currently. The parameter is misleading but the call site is correct.
- **Remediation:** Remove the unused `nameLen` parameter, or add an explicit check: `if (nameLen < IFNAMSIZ) return -1;`
- **References:** CWE-120 (Buffer Copy without Checking Size of Input)

---

### FINDING-04: SMC Key Encoding Reads Exactly 4 Bytes Without Null-Terminator Check

- **Severity:** LOW
- **Location:** `internal/smc/smc.go:43-48` (smcKeyEncode C function), lines 141 and 198 (Go callers)
- **Issue:** The `smcKeyEncode` C function reads `key[0]` through `key[3]` unconditionally. The Go callers validate `len(key) == 4` before calling, and `C.CString` always null-terminates, so the buffer will always have at least 5 bytes (4 chars + null). This is safe.
- **Impact:** None given current callers. If `smcKeyEncode` were ever called from C code with a shorter string, it would read uninitialized memory.
- **Remediation:** No action needed for current usage. Consider adding a defensive `strlen` check in the C function for future-proofing.
- **References:** CWE-125 (Out-of-bounds Read)

---

### FINDING-05: SMC memcpy Uses Kernel-Returned dataSize Without Upper-Bound Clamp

- **Severity:** MEDIUM
- **Location:** `internal/smc/smc.go:92` (smcReadKey C function: `memcpy(outBytes, output.bytes, dataSize)`)
- **Issue:** The `smcReadKey` function copies `dataSize` bytes from `output.bytes` (a 32-byte fixed array) into `outBytes`. The `dataSize` value comes from the kernel via `smcGetKeyInfo`. If the kernel returned a `dataSize` greater than 32, `memcpy` would read beyond the `output.bytes` array (stack buffer overread). On the Go side, the destination is also a `[32]C.uint8_t` array, so both source and destination are 32 bytes.
- **Impact:** If the kernel returns `dataSize > 32` (which would indicate a kernel bug or a compromised SMC driver), this would be a stack buffer over-read on the `output.bytes` array. The practical likelihood is extremely low since `dataSize` is a `uint8_t` (max 255) but the buffer is only 32 bytes.
- **Remediation:** Clamp `dataSize` before the memcpy:
  ```c
  if (dataSize > sizeof(output.bytes)) dataSize = sizeof(output.bytes);
  memcpy(outBytes, output.bytes, dataSize);
  ```
- **References:** CWE-120 (Buffer Copy without Checking Size of Input), CWE-125 (Out-of-bounds Read)

---

### FINDING-06: Mach Port Leak from mach_host_self()

- **Severity:** LOW
- **Location:** `internal/platform/mach.go:35` and `mach.go:85` (calls to `C.mach_host_self()`)
- **Issue:** `mach_host_self()` returns a send right to the host port. Each call increments the reference count on the port. The returned port is never deallocated via `mach_port_deallocate`. Over time (across many collection cycles), this leaks Mach port rights. On macOS, `mach_host_self()` typically returns the same port number, but the reference count still increases.
- **Impact:** Slow Mach port right leak. Each call to `HostProcessorInfo()` and `HostVMInfo64()` leaks one send right. At a 1-second refresh interval, this is ~86,400 leaked rights per day. The process would eventually hit the Mach port limit (though this typically requires millions of leaked rights).
- **Remediation:** Cache the host port at init time, or deallocate after each use:
  ```go
  hostPort := C.mach_host_self()
  defer C.mach_port_deallocate(C.get_mach_task_self(), hostPort)
  ```
  Alternatively, use a package-level variable initialized once.
- **References:** CWE-404 (Improper Resource Shutdown or Release)

---

### FINDING-07: Integer Overflow in Memory Page Count Multiplication

- **Severity:** LOW
- **Location:** `internal/collector/memory.go:32-36`
- **Issue:** The memory collector multiplies page counts (uint64) by page size (uint64). On current hardware, page counts are returned as 32-bit `natural_t` values from the kernel, promoted to uint64. Even at maximum (4 billion pages * 16384 page size), the result fits within uint64. Similarly, disk capacity calculations in `disk.go:31-33` multiply `stat.Blocks * stat.Bsize`, which are both `int64` on Darwin. No overflow is possible with current real-world values.
- **Impact:** No practical risk on current hardware. Theoretical only if page sizes or counts changed dramatically in future kernel versions.
- **Remediation:** No action needed. The uint64 arithmetic is correct for all realistic values.
- **References:** CWE-190 (Integer Overflow)

---

### FINDING-08: SMC Connection Not Closed on Abnormal Exit

- **Severity:** LOW
- **Location:** `internal/collector/temperature.go:22-29` (lazy open), `internal/ui/app.go:64` (close on quit)
- **Issue:** The SMC connection is opened lazily in `TempCollector.Collect()` and closed in `Model.Update()` only when the user presses 'q' or Ctrl+C. If the process is killed by a signal (SIGKILL, SIGTERM without handler), the `IOServiceClose` call is never made, leaving the kernel-side IOKit connection open until the Mach port is reclaimed.
- **Impact:** Minimal. macOS automatically cleans up IOKit connections and Mach ports when a process exits. The kernel driver handles unexpected disconnection gracefully.
- **Remediation:** Consider adding a `defer` or signal handler for graceful cleanup, though this is not strictly necessary on macOS.
- **References:** CWE-404 (Improper Resource Shutdown or Release)

---

### FINDING-09: SwapUsage Uses Hardcoded MIB Value

- **Severity:** LOW
- **Location:** `internal/platform/sysctl.go:83` (`55 = VM_SWAPUSAGE`)
- **Issue:** The MIB value 55 for `VM_SWAPUSAGE` is hardcoded as a magic number rather than using the defined constant. While this value has been stable across macOS versions, hardcoded MIB values are fragile and could break on future OS versions without compilation errors.
- **Impact:** If Apple changes the MIB number in a future macOS version, the sysctl call would silently fail (returning the zero-initialized struct due to the `non-fatal` error handling on line 89), or worse, read unrelated kernel data.
- **Remediation:** Use `sysctlbyname("vm.swapusage", ...)` instead, or reference the `VM_SWAPUSAGE` constant from the system headers if available.
- **References:** CWE-477 (Use of Obsolete Function)

---

### FINDING-10: SwapUsage Struct Layout Assumption

- **Severity:** LOW
- **Location:** `internal/platform/sysctl.go:81-86`
- **Issue:** The code comments state the layout of `struct xsw_usage` as `{total, avail, used}` but only reads 3 `uint64_t` values (24 bytes). The actual `struct xsw_usage` on macOS is `{xsu_total, xsu_avail, xsu_used, xsu_pagesize}` -- 4 fields totaling 32 bytes (three uint64 + one int, with padding). The `xswLen` passed is `sizeof([3]uint64)` = 24 bytes, which is smaller than the actual struct. The kernel may write beyond the provided buffer or return an error.
- **Impact:** The kernel's `sysctl` implementation truncates output to the provided buffer length, so no overflow occurs. However, the truncated read means the kernel might return an error on some versions, leading to silently zeroed swap data.
- **Remediation:** Allocate a buffer matching the full `struct xsw_usage` size, or use `C.struct_xsw_usage` directly:
  ```go
  var xsw C.struct_xsw_usage
  xswLen := C.size_t(unsafe.Sizeof(xsw))
  ```
- **References:** CWE-131 (Incorrect Calculation of Buffer Size)

---

### FINDING-11: CString Memory Management is Correct Throughout

- **Severity:** INFORMATIONAL
- **Location:** All files using `C.CString()`
- **Issue:** Every `C.CString()` allocation is paired with a `defer C.free(unsafe.Pointer(...))` in the same scope. This includes:
  - `internal/platform/iokit.go:124-125, 166-167, 179-180, 192-193, 202-203, 218-219`
  - `internal/platform/sysctl.go:18-19, 33-34, 48-49`
  - `internal/smc/smc.go:112-113, 145-146, 201-202`
- **Impact:** No memory leak. This is a positive finding.
- **Remediation:** None needed.

---

### FINDING-12: IOKit Object and CFDictionary Lifecycle Management is Correct

- **Severity:** INFORMATIONAL
- **Location:** `internal/platform/iokit.go` (entire file)
- **Issue:** All IOKit services, iterators, and CF dictionaries are properly released:
  - `IOKitGetMatchingService` returns a service that must be released by the caller (documented by the `Release()` method).
  - `IOKitIterateMatching` releases each entry after reading properties (line 236), releases the iterator via defer (line 226), and releases each property dict after the callback (line 243).
  - `GetDict` returns a non-owning reference with a clear doc comment (line 177).
  - The `cfStr` helper in `dict_get_value` creates and releases the key string within the same function (lines 57-61).
- **Impact:** No resource leaks in the IOKit layer. This is a positive finding.
- **Remediation:** None needed.

---

### FINDING-13: Information Disclosure -- Detailed System Metrics Exposed to Terminal

- **Severity:** LOW
- **Location:** All collector and UI files
- **Issue:** The tool displays detailed system information including: exact memory breakdown, per-core CPU utilization, GPU memory usage, network interface names and traffic rates, disk capacity and I/O rates, battery voltage/amperage/wattage, and thermal sensor readings. This information is rendered to the terminal and could be captured via screen recording, terminal logging, or shoulder surfing.
- **Impact:** An attacker with access to the terminal output could gain detailed knowledge of the system's hardware configuration, resource utilization patterns, and network activity. This is inherent to the tool's purpose as a system monitor. The tool does not expose passwords, encryption keys, or user data.
- **Remediation:** This is an accepted risk given the tool's purpose. Consider documenting that the tool should not be run in environments where terminal output may be captured by untrusted parties.
- **References:** CWE-200 (Exposure of Sensitive Information)

---

### FINDING-14: No Input Sanitization on Sysctl Names

- **Severity:** LOW
- **Location:** `internal/platform/sysctl.go:17-29, 32-44, 47-69`
- **Issue:** The `SysctlUint64`, `SysctlUint32`, and `SysctlString` functions accept arbitrary string names and pass them directly to `sysctlbyname`. There is no validation or allowlist of permitted sysctl names. However, all callers use hardcoded sysctl names (e.g., `"hw.memsize"`, `"hw.perflevel0.logicalcpu"`), and the functions are not exported in a way that external packages could call them with arbitrary input.
- **Impact:** No practical risk. The sysctl names are compile-time constants. If these functions were ever exposed as a public API or called with user-supplied input, an attacker could read arbitrary sysctl values (information disclosure) but not write them (the write parameters are always nil/0).
- **Remediation:** No action needed for current usage. If the API is ever made public, add an allowlist of permitted sysctl names.
- **References:** CWE-20 (Improper Input Validation)

---

### FINDING-15: Binary Ships with Symbols Stripped (-s -w)

- **Severity:** INFORMATIONAL
- **Location:** `Makefile:4`
- **Issue:** The build uses `-ldflags "-s -w"` which strips the symbol table and debug information. This is a positive security practice that makes reverse engineering marginally harder and reduces binary size.
- **Impact:** Positive finding.
- **Remediation:** None needed.

---

### FINDING-16: No Dependency Vulnerability Scan Configured

- **Severity:** LOW
- **Location:** `go.mod`, `Makefile`
- **Issue:** There is no `govulncheck` or equivalent vulnerability scanning step in the build process. The dependencies are relatively minimal (bubbletea TUI framework and transitive deps), but vulnerabilities in these packages would not be automatically detected.
- **Impact:** A vulnerability in a dependency (e.g., a terminal escape sequence injection in bubbletea) could go undetected.
- **Remediation:** Add `govulncheck ./...` to the Makefile or CI pipeline.
- **References:** CWE-1035 (Using Software with Known Vulnerabilities)

---

## Executive Summary

### Overall Risk Posture: LOW

mactop is a local-only, read-only system monitoring tool with no network listeners, no file I/O beyond system APIs, no user-supplied input (beyond CLI flags), and no privilege escalation. The attack surface is minimal.

### Findings Summary

| Severity      | Count | Finding IDs |
|---------------|-------|-------------|
| CRITICAL      | 0     | --          |
| HIGH          | 0     | --          |
| MEDIUM        | 3     | 01, 02, 05  |
| LOW           | 8     | 03, 04, 06, 07, 08, 09, 10, 13, 14, 16 |
| INFORMATIONAL | 3     | 11, 12, 15  |

### Key Observations

**Positive findings:**
- All `C.CString()` allocations are properly freed with `defer C.free()`.
- IOKit object lifecycle management is correct with proper release on all code paths.
- SMC key input is validated (must be exactly 4 characters).
- Counter wraparound is explicitly handled in disk and network delta computations.
- The tool makes no network connections and accepts no external input beyond CLI flags.
- Error handling is defensive throughout, with non-fatal fallbacks.

**Areas for improvement (prioritized):**
1. **FINDING-05** (SMC memcpy bounds) -- Add a `dataSize` clamp before `memcpy` to defend against a hypothetical kernel bug returning dataSize > 32. Simple one-line fix.
2. **FINDING-02** (Network buffer parsing) -- Add bounds checks in `parse_iflist2` before casting raw buffer bytes to struct pointers. Low probability but the fix is straightforward.
3. **FINDING-01** (Sysctl TOCTOU) -- Consider using Go's `syscall.Sysctl()` or adding a retry loop. Low impact in practice.
4. **FINDING-06** (Mach port leak) -- Cache `mach_host_self()` to avoid accumulating port rights over long-running sessions.
5. **FINDING-10** (SwapUsage struct size) -- Use the actual C struct type to ensure correct buffer sizing.
6. **FINDING-16** (Dependency scanning) -- Add `govulncheck` to the build/CI pipeline.
