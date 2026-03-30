# Security Review #2: mactop (Post-Private-API Integration)

**Date:** 2026-03-27
**Scope:** Second-pass audit focused on new IOHIDEventSystem private API integration (`internal/platform/iohid.go`), rewritten SMC structs (`internal/smc/smc.go`), modified network buffer parsing (`internal/collector/network.go`), and re-audit of all previously flagged areas.
**Reviewer:** Automated security review (Claude)
**Codebase version:** Post-HID integration (pre-release)

---

## Status of Findings from Security Review #1

### FINDING-05 (SMC memcpy bounds clamp) -- FIXED
The rewritten `smc.go` now includes the bounds clamp at line 104-105:
```c
uint32_t copySize = dataSize;
if (copySize > sizeof(output.bytes)) copySize = sizeof(output.bytes);
```
This correctly prevents over-read regardless of what the kernel returns for `dataSize`.

### FINDING-06 (Mach port leak from mach_host_self) -- FIXED
The `mach.go` file now caches `mach_host_self()` in a package-level `hostPort` variable initialized once in `init()` (line 26). Both `HostProcessorInfo` and `HostVMInfo64` use the cached port. No more per-call port right leak.

### FINDING-10 (SwapUsage struct size) -- FIXED
The `sysctl.go` `GetSwapUsage` function now uses `C.struct_xsw_usage` directly (line 81) with `unsafe.Sizeof(xsw)` for the buffer length (line 82). This matches the kernel struct exactly.

### FINDING-04 (SMC key encoding without null check) -- FIXED / NO LONGER APPLICABLE
The `smcKeyEncode` C function has been removed entirely. Key encoding is now done in pure Go via `encodeKey()` (line 163-165), which operates on Go string bytes with a `len(key) == 4` guard at every call site. No C string handling involved.

### FINDING-01 (SysctlString TOCTOU) -- NOT FIXED (still present)
The two-call pattern in `SysctlString` (lines 52-59) remains unchanged. As noted in the original review, this is low-impact because the kernel truncates rather than overflowing.

### FINDING-02 (Network buffer parsing bounds) -- PARTIALLY FIXED (see FINDING-2.1 below)
The `parse_iflist2` function has been rewritten with improved bounds checking, but a residual issue remains. See FINDING-2.1.

### FINDING-03 (get_if_name unused parameter) -- NOT FIXED (still present)
The `nameLen` parameter is still accepted but not used by `if_indextoname`. Low severity, unchanged.

### FINDING-09 (Hardcoded VM_SWAPUSAGE MIB) -- NOT FIXED (still present)
The hardcoded `55` remains at line 80. Low severity, unchanged.

### FINDING-16 (No dependency vulnerability scan) -- NOT FIXED (still present)
No `govulncheck` integration observed in the build pipeline. Low severity, unchanged.

---

## New Findings

### FINDING-2.1: Network parse_iflist2 -- Unaligned Pointer Dereference for msglen

- **Severity:** LOW
- **Location:** `internal/collector/network.go:38`
- **Issue:** The parser reads `msglen` via `*(const unsigned short *)ptr`, casting an arbitrary buffer offset to `unsigned short *`. While the initial buffer pointer from `sysctl` is naturally aligned, subsequent iterations advance `ptr` by `msglen` bytes, which could theoretically result in an odd-aligned pointer. On ARM64 (Apple Silicon), unaligned loads of `unsigned short` are architecturally permitted and handled in hardware, so this will not fault. On x86_64 it is also safe. However, the pattern is technically undefined behavior in C.
- **Impact:** No practical impact on either Apple Silicon or Intel Macs. The kernel guarantees message alignment in the routing socket buffer, so `ptr` will always be naturally aligned after advancing by `msglen`.
- **Remediation:** For strict correctness, use `memcpy` to read `msglen` and `msgtype`:
  ```c
  unsigned short msglen;
  memcpy(&msglen, ptr, sizeof(msglen));
  ```
  This is a defense-in-depth improvement, not a functional fix.
- **References:** CWE-188 (Reliance on Data/Memory Layout)

---

### FINDING-2.2: Network parse_iflist2 -- Minimum Header Size Check is 4 Bytes, Not sizeof(struct rt_msghdr)

- **Severity:** LOW
- **Location:** `internal/collector/network.go:36`
- **Issue:** The loop condition `ptr + 4 <= end` ensures at least 4 bytes remain to read `msglen` (2 bytes at offset 0) and `msgtype` (1 byte at offset 3). This is correct for reading those specific fields. However, the code then checks `msglen >= sizeof(struct if_msghdr2)` before casting to the full struct (line 44), which is the right defense. The overall bounds logic is sound. This is an informational note confirming the pattern is safe.
- **Impact:** None. The bounds checks are layered correctly: 4-byte minimum for header fields, then `ptr + msglen <= end` to ensure the full message is in-buffer, then `msglen >= sizeof(struct if_msghdr2)` before casting.
- **Remediation:** None needed.
- **References:** CWE-125 (Out-of-bounds Read)

---

### FINDING-2.3: IOHIDEventSystem -- Private API Stability Risk

- **Severity:** MEDIUM
- **Location:** `internal/platform/iohid.go:9-18` (extern declarations)
- **Issue:** The file declares six private IOKit symbols via `extern`:
  - `IOHIDEventSystemClientCreate`
  - `IOHIDEventSystemClientSetMatching`
  - `IOHIDEventSystemClientCopyServices`
  - `IOHIDServiceClientCopyProperty`
  - `IOHIDServiceClientCopyEvent`
  - `IOHIDEventGetFloatValue`

  These are private Apple APIs with no stability guarantee. If Apple changes the function signatures (e.g., adding or removing parameters, changing return types), the `extern` declarations would mismatch at link time or, worse, match at link time but produce memory corruption at runtime due to calling convention mismatches.

  Specific risks:
  1. **Parameter count change:** If a function gains a parameter, the caller pushes fewer arguments than expected. On ARM64, this results in reading garbage from a register for the missing parameter, which could cause the function to behave unpredictably or crash.
  2. **Return type change:** The `void*` typedefs for `IOHIDEventSystemClientRef`, `IOHIDServiceClientRef`, and `IOHIDEventRef` are opaque pointers. If Apple changes any of these to non-pointer types (e.g., a handle/index), the code would silently misinterpret values.
  3. **Removal:** If a symbol is removed from IOKit, the binary will fail to link (compile-time error, not a runtime vulnerability).

- **Impact:** On a future macOS version where these private APIs change signatures, the binary could crash (most likely outcome) or produce corrupted data. The temperature collector has a fallback to SMC, so a crash in HID initialization would be caught as a nil return, but a crash *during* `ReadTemperatures` after successful init would terminate the process.
- **Remediation:**
  1. Use `dlsym` with `RTLD_DEFAULT` to load symbols at runtime, allowing graceful fallback if they are missing.
  2. Add a build-time version guard or runtime macOS version check to disable HID on untested OS versions.
  3. Consider wrapping each extern call in a signal handler or recovery mechanism (not practical in Go/CGo).
  4. Document the specific macOS versions tested and known to work.
- **References:** CWE-477 (Use of Obsolete Function), Apple Private API Usage Policy

---

### FINDING-2.4: IOHIDEventSystem -- Type Safety of extern Declarations

- **Severity:** MEDIUM
- **Location:** `internal/platform/iohid.go:13-18`
- **Issue:** All IOHIDEventSystem types are declared as `typedef void*`:
  ```c
  typedef void* IOHIDEventSystemClientRef;
  typedef void* IOHIDServiceClientRef;
  typedef void* IOHIDEventRef;
  ```
  This erases type safety at the C level. If a function is called with the wrong opaque pointer type (e.g., passing an `IOHIDEventRef` where an `IOHIDServiceClientRef` is expected), the compiler will not catch the error. The current code does not make such mistakes, but future modifications could introduce them without compiler warnings.

  Additionally, `IOHIDServiceClientCopyEvent` is declared with three `int64_t` parameters (line 17), but the actual private API signature uses `int64_t eventType, int64_t fieldType, int64_t options` (or similar). If the actual parameter types differ (e.g., `int32_t` or `CFIndex`), there could be ABI mismatches on the calling convention. On ARM64, `int32_t` and `int64_t` use different register widths, so this could cause the function to read incorrect values.

- **Impact:** If the actual private API signatures use `int32_t` for any of the event type or field parameters (which some reverse-engineered headers suggest for `eventType`), passing `int64_t` would work correctly on ARM64 (the callee reads the lower 32 bits) but is technically an ABI violation. In practice, the values being passed (0x0F, 0x0F0000, 0, 0) all fit in 32 bits, so this is safe for current usage.
- **Remediation:** Cross-reference the extern signatures against multiple reverse-engineered header sources (e.g., Apple's open-source IOHIDFamily, or the `IOHIDEventTypes.h` from older public SDKs). Consider using `int32_t` for `eventType` and `int64_t` for the field value, matching the most commonly documented signatures.
- **References:** CWE-686 (Function Call With Incorrect Argument Type)

---

### FINDING-2.5: IOHIDEventSystem -- hidGetProductName malloc Without maxSize Validation

- **Severity:** LOW
- **Location:** `internal/platform/iohid.go:83-86`
- **Issue:** The function calls `CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1` and passes the result directly to `malloc`. `CFStringGetMaximumSizeForEncoding` returns `kCFNotFound` (which is -1, or `0x7FFFFFFFFFFFFFFF` as unsigned) if the encoding is not recognized or the length would overflow. For `kCFStringEncodingUTF8`, this does not happen in practice, but if it did, `maxSize` would be an enormous value, and `malloc` would fail (return NULL), and `CFStringGetCString` would be called with a NULL buffer, causing a crash.

  The code does not check if `maxSize` is reasonable or if `malloc` returned NULL.

- **Impact:** If `CFStringGetMaximumSizeForEncoding` returns `kCFNotFound` (theoretically impossible for UTF-8 encoding with a valid CFString length), `malloc` will return NULL and the subsequent `CFStringGetCString` call will write to address 0, causing a segfault. The probability is near zero in practice because `kCFStringEncodingUTF8` is always valid.
- **Remediation:** Add a NULL check after malloc:
  ```c
  char *buf = (char*)malloc(maxSize);
  if (buf == NULL) {
      CFRelease(prop);
      return NULL;
  }
  ```
  This is a one-line defensive addition.
- **References:** CWE-252 (Unchecked Return Value), CWE-476 (NULL Pointer Dereference)

---

### FINDING-2.6: IOHIDEventSystem -- HIDThermalReader Not Thread-Safe

- **Severity:** LOW
- **Location:** `internal/platform/iohid.go:128-199`
- **Issue:** The `HIDThermalReader` struct holds a `client` field that is read in `ReadTemperatures()` and set to `nil` in `Close()`. If `Close()` and `ReadTemperatures()` are called concurrently from different goroutines, there is a data race on the `client` field. The `TempCollector` that uses this reader is called from a single goroutine (the bubbletea update loop), so this is not a current issue.
- **Impact:** None with current usage. If the reader were shared across goroutines, concurrent access would be a data race (undefined behavior in Go).
- **Remediation:** No action needed for current single-goroutine usage. If concurrency is ever added, protect the `client` field with a mutex.
- **References:** CWE-362 (Race Condition)

---

### FINDING-2.7: SMC Struct Layout -- Alignment Verification Needed

- **Severity:** MEDIUM
- **Location:** `internal/smc/smc.go:33-65` (SMCKeyData_t struct definition)
- **Issue:** The C struct `SMCKeyData_t` is declared as 80 bytes (per the comment on line 65). The struct layout depends on compiler padding rules:
  - `key`: 4 bytes at offset 0
  - `vers` (6 bytes, 2-byte alignment): offset 4, spans bytes 4-9
  - `pLimitData` (16 bytes, 4-byte alignment): needs 4-byte alignment, so 2 bytes padding after `vers`. Offset 12, spans 12-27.
  - `keyInfo` (12 bytes with padding, 4-byte alignment): offset 28, spans 28-39.
  - `result` (char): offset 40
  - `status` (unsigned char): offset 41
  - `data8` (unsigned char): offset 42
  - Padding: 1 byte at offset 43 (for 4-byte alignment of `data32`)
  - `data32` (unsigned int): offset 44, spans 44-47
  - `bytes[32]`: offset 48, spans 48-79

  Total: 80 bytes. This matches the comment.

  However, this layout must match what the kernel's `AppleSMC.kext` driver expects. If the kernel was compiled with different struct packing or a different compiler version, the struct layout could differ. The previous version used a 52-byte struct which was incorrect. There is no compile-time assertion (e.g., `_Static_assert(sizeof(SMCKeyData_t) == 80, ...)`) to catch mismatches.

- **Impact:** If the struct size does not match the kernel driver's expectation, `IOConnectCallStructMethod` will either reject the call (if the kernel validates the input size) or misinterpret the fields (if it does not). The former produces an error return; the latter produces incorrect readings or could cause the kernel to read uninitialized memory from the user's buffer.
- **Remediation:** Add a compile-time size assertion:
  ```c
  _Static_assert(sizeof(SMCKeyData_t) == 80,
      "SMCKeyData_t size must match kernel driver (80 bytes)");
  ```
  This ensures the struct size is verified at build time and will produce a clear compiler error if the layout changes.
- **References:** CWE-131 (Incorrect Calculation of Buffer Size)

---

### FINDING-2.8: SMC Key Cache Grows Unboundedly During DiscoverTempSensors

- **Severity:** LOW
- **Location:** `internal/smc/smc.go:150-158, 206-219`
- **Issue:** The `keyCache` map in `Connection` grows each time a new key's info is queried. During `DiscoverTempSensors`, the code iterates all SMC keys (which can be 200+ on modern Macs) and queries `GetKeyInfo` for every key starting with 'T'. Each successful query adds an entry to the cache. The cache is never pruned.

  For the temperature-only use case, the cache will settle at approximately 30-60 entries (number of 'T' keys), consuming a few KB. This is negligible.

- **Impact:** No practical impact. The cache is bounded by the number of SMC keys (typically under 500), each entry is 8 bytes, total memory is under 4 KB.
- **Remediation:** No action needed. Document that the cache is intentionally unbounded since the key space is finite and small.
- **References:** CWE-400 (Uncontrolled Resource Consumption)

---

### FINDING-2.9: IOHIDEventSystem -- CFRelease on Opaque void* Pointer

- **Severity:** LOW
- **Location:** `internal/platform/iohid.go:107-109` (hidReleaseClient)
- **Issue:** The `hidReleaseClient` function calls `CFRelease(client)` on a `void*` pointer. `CFRelease` expects a `CFTypeRef` (which is also `void*`), so this compiles without warning. However, `IOHIDEventSystemClientRef` is a CF-based object only if Apple's implementation follows CF conventions. If a future version returns a non-CF object (e.g., a raw Mach port or C++ object), calling `CFRelease` on it would corrupt memory.

  The current implementation of `IOHIDEventSystemClientCreate` does return a CF-based object (confirmed by the fact that `CFRelease` works without crashing), but this is an implementation detail that could change.

- **Impact:** If Apple's private implementation changes the memory management model, `CFRelease` could double-free, corrupt the heap, or crash. This is extremely unlikely given the long-standing CF convention in IOKit.
- **Remediation:** No action needed. The CF-based convention is deeply embedded in IOKit's design and is unlikely to change.
- **References:** CWE-763 (Release of Invalid Pointer or Reference)

---

### FINDING-2.10: Debug Log File Created in Current Working Directory

- **Severity:** LOW
- **Location:** `internal/ui/app.go:61` (`tea.LogToFile("mactop-debug.log", "mactop")`)
- **Issue:** When verbose mode is enabled (`-v` flag), a debug log file `mactop-debug.log` is created in the current working directory. This file contains collector error messages which may include system details (sysctl names, IOKit error codes, etc.). The file is created with default permissions and is never explicitly closed or cleaned up.
- **Impact:** The log file could disclose system error details to other users on a shared system, or persist after the user intended to stop monitoring. The `-v` flag must be explicitly set, so this is opt-in.
- **Remediation:** Use `os.CreateTemp` for the log file (the bubbletea `LogToFile` function is a convenience wrapper). Alternatively, document that `-v` creates a log file in the CWD and recommend against using it in shared environments. Consider setting restrictive permissions (0600) on the file.
- **References:** CWE-532 (Insertion of Sensitive Information into Log File)

---

### FINDING-2.11: GPU Collector -- int64 to uint64 Cast for Memory Values

- **Severity:** LOW
- **Location:** `internal/collector/gpu.go:41-45`
- **Issue:** The GPU collector reads "In use system memory" and "Allocated system memory" as `int64` values via `GetInt64`, then casts them to `uint64`. If the IOKit property ever returns a negative value (which would indicate an error or sentinel), it would wrap to a very large unsigned value and display incorrect data.
- **Impact:** Cosmetic only -- incorrect memory display. No memory safety issue.
- **Remediation:** Add a negativity check before the cast:
  ```go
  if inUse, ok := perfStats.GetInt64("In use system memory"); ok && inUse >= 0 {
      c.Data.InUseMemory = uint64(inUse)
  }
  ```
- **References:** CWE-681 (Incorrect Conversion between Numeric Types)

---

## Executive Summary

### Overall Risk Posture: LOW

The codebase has improved since the first security review. Three of the four actionable medium/low findings from the first review (SMC memcpy bounds, Mach port leak, SwapUsage struct size) have been properly fixed. The SMC key encoding has been moved to pure Go, eliminating CString handling entirely.

The new IOHIDEventSystem integration introduces the primary new risk: reliance on private Apple APIs with `extern` declarations that cannot be validated at compile time against Apple's actual signatures. This is an inherent trade-off for supporting macOS 26+ where direct SMC access is blocked. The fallback architecture (HID -> SMC -> unavailable) is well-designed and limits blast radius.

### Findings Summary

| Severity      | Count | Finding IDs |
|---------------|-------|-------------|
| CRITICAL      | 0     | --          |
| HIGH          | 0     | --          |
| MEDIUM        | 3     | 2.3, 2.4, 2.7 |
| LOW           | 7     | 2.1, 2.5, 2.6, 2.8, 2.9, 2.10, 2.11 |
| INFORMATIONAL | 1     | 2.2 |

Plus 4 unfixed low-severity findings carried forward from Review #1 (FINDING-01, 03, 09, 16).

### Remediation Priority

1. **FINDING-2.7** (SMC struct size assertion) -- Add `_Static_assert(sizeof(SMCKeyData_t) == 80, ...)`. One line, zero risk, catches future breakage at compile time. Highest value fix.
2. **FINDING-2.5** (malloc NULL check in hidGetProductName) -- Add `if (buf == NULL)` check after malloc. One line, prevents theoretical NULL deref.
3. **FINDING-2.3** (Private API stability) -- Add runtime macOS version gating and document tested versions. Medium effort, important for long-term maintainability.
4. **FINDING-2.4** (extern signature accuracy) -- Cross-reference signatures against reverse-engineered headers. Research task.
5. **FINDING-2.10** (Debug log permissions) -- Set 0600 permissions on the log file. Low effort.
6. **FINDING-16 from Review #1** (govulncheck) -- Add to CI. Low effort, good practice.

### Positive Observations

- The HID -> SMC fallback architecture in `TempCollector` is well-designed. If HID fails, it falls back to SMC cleanly. If both fail, it marks temperature as unavailable rather than crashing.
- All CF object lifecycle management in the new `iohid.go` is correct: `services` array is released via `defer`, the `event` from `CopyEvent` is released after reading, and `prop` from `CopyProperty` is released on all code paths.
- The `hidGetProductName` function correctly type-checks the property (`CFGetTypeID != CFStringGetTypeID`) before casting, preventing type confusion.
- CString/free pairing remains correct throughout the codebase.
- The `seen` map deduplication in `ReadTemperatures` prevents duplicate sensor entries without leaking memory.
