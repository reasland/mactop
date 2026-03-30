package platform

/*
#include <sys/types.h>
#include <sys/sysctl.h>
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// SysctlUint64 reads a uint64 value from the named sysctl.
func SysctlUint64(name string) (uint64, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var val C.uint64_t
	valLen := C.size_t(unsafe.Sizeof(val))

	rc := C.sysctlbyname(cName, unsafe.Pointer(&val), &valLen, nil, 0)
	if rc != 0 {
		return 0, fmt.Errorf("sysctlbyname(%s) failed: %d", name, rc)
	}
	return uint64(val), nil
}

// SysctlUint32 reads a uint32 value from the named sysctl.
func SysctlUint32(name string) (uint32, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var val C.uint32_t
	valLen := C.size_t(unsafe.Sizeof(val))

	rc := C.sysctlbyname(cName, unsafe.Pointer(&val), &valLen, nil, 0)
	if rc != 0 {
		return 0, fmt.Errorf("sysctlbyname(%s) failed: %d", name, rc)
	}
	return uint32(val), nil
}

// SysctlString reads a string value from the named sysctl.
func SysctlString(name string) (string, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	// First call to get the required buffer size.
	var bufLen C.size_t
	rc := C.sysctlbyname(cName, nil, &bufLen, nil, 0)
	if rc != 0 {
		return "", fmt.Errorf("sysctlbyname(%s) size query failed: %d", name, rc)
	}

	buf := make([]byte, bufLen)
	rc = C.sysctlbyname(cName, unsafe.Pointer(&buf[0]), &bufLen, nil, 0)
	if rc != 0 {
		return "", fmt.Errorf("sysctlbyname(%s) failed: %d", name, rc)
	}

	// Trim the null terminator.
	if bufLen > 0 && buf[bufLen-1] == 0 {
		bufLen--
	}
	return string(buf[:bufLen]), nil
}

// SwapUsage holds swap file statistics.
type SwapUsage struct {
	Total uint64
	Used  uint64
	Avail uint64
}

// GetSwapUsage reads swap usage via sysctl("vm.swapusage").
func GetSwapUsage() (*SwapUsage, error) {
	mib := [2]C.int{C.CTL_VM, 55} // 55 = VM_SWAPUSAGE
	var xsw C.struct_xsw_usage
	xswLen := C.size_t(unsafe.Sizeof(xsw))

	rc := C.sysctl(&mib[0], 2, unsafe.Pointer(&xsw), &xswLen, nil, 0)
	if rc != 0 {
		return &SwapUsage{}, nil // non-fatal
	}
	return &SwapUsage{
		Total: uint64(xsw.xsu_total),
		Avail: uint64(xsw.xsu_avail),
		Used:  uint64(xsw.xsu_used),
	}, nil
}
