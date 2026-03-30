package platform

/*
#include <mach/mach.h>
#include <mach/processor_info.h>
#include <mach/mach_host.h>
#include <mach/host_info.h>

static mach_port_t get_mach_task_self(void) {
    return mach_task_self();
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// hostPort is the cached Mach host port, obtained once at init time.
// mach_host_self() returns a send right that increments a reference count
// on each call; caching it avoids leaking a port right every tick.
var hostPort C.mach_port_t

func init() {
	hostPort = C.mach_host_self()
}

// CPUTicks holds raw tick counts for a single CPU core.
type CPUTicks struct {
	User   uint32
	System uint32
	Idle   uint32
	Nice   uint32
}

// HostProcessorInfo returns raw CPU tick counts for every logical core.
func HostProcessorInfo() ([]CPUTicks, error) {
	var count C.natural_t
	var info C.processor_info_array_t
	var msgCount C.mach_msg_type_number_t

	kr := C.host_processor_info(
		hostPort,
		C.PROCESSOR_CPU_LOAD_INFO,
		&count,
		&info,
		&msgCount,
	)
	if kr != C.KERN_SUCCESS {
		return nil, fmt.Errorf("host_processor_info failed: %d", kr)
	}
	defer C.vm_deallocate(
		C.get_mach_task_self(),
		C.vm_address_t(uintptr(unsafe.Pointer(info))),
		C.vm_size_t(msgCount)*C.vm_size_t(unsafe.Sizeof(C.int(0))),
	)

	numCPU := int(count)
	ticks := make([]CPUTicks, numCPU)

	// The info array is laid out as numCPU groups of CPU_STATE_MAX (4) integers.
	infoSlice := unsafe.Slice((*C.int)(unsafe.Pointer(info)), numCPU*C.CPU_STATE_MAX)

	for i := 0; i < numCPU; i++ {
		base := i * C.CPU_STATE_MAX
		ticks[i] = CPUTicks{
			User:   uint32(infoSlice[base+C.CPU_STATE_USER]),
			System: uint32(infoSlice[base+C.CPU_STATE_SYSTEM]),
			Idle:   uint32(infoSlice[base+C.CPU_STATE_IDLE]),
			Nice:   uint32(infoSlice[base+C.CPU_STATE_NICE]),
		}
	}

	return ticks, nil
}

// VMStatistics64 holds selected fields from vm_statistics64.
type VMStatistics64 struct {
	FreeCount       uint64
	ActiveCount     uint64
	InactiveCount   uint64
	WireCount       uint64
	CompressorCount uint64
	InternalCount   uint64
	PageSize        uint64
}

// HostVMInfo64 returns virtual memory statistics via host_statistics64.
func HostVMInfo64() (*VMStatistics64, error) {
	var stats C.vm_statistics64_data_t
	count := C.mach_msg_type_number_t(C.HOST_VM_INFO64_COUNT)

	kr := C.host_statistics64(
		hostPort,
		C.HOST_VM_INFO64,
		(*C.integer_t)(unsafe.Pointer(&stats)),
		&count,
	)
	if kr != C.KERN_SUCCESS {
		return nil, fmt.Errorf("host_statistics64 failed: %d", kr)
	}

	return &VMStatistics64{
		FreeCount:       uint64(stats.free_count),
		ActiveCount:     uint64(stats.active_count),
		InactiveCount:   uint64(stats.inactive_count),
		WireCount:       uint64(stats.wire_count),
		CompressorCount: uint64(stats.compressor_page_count),
		InternalCount:   uint64(stats.internal_page_count),
		PageSize:        uint64(C.vm_kernel_page_size),
	}, nil
}
