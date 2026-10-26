//go:build windows

package embedding

import (
	"syscall"
	"unsafe"
)

// relationProcessorCore is LOGICAL_PROCESSOR_RELATIONSHIP.RelationProcessorCore:
// one SYSTEM_LOGICAL_PROCESSOR_INFORMATION_EX entry per physical core, each
// carrying its SMT sibling mask.
const relationProcessorCore = 0

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procGetLogicalProcessorInfo = kernel32.NewProc("GetLogicalProcessorInformationEx")
)

// physicalCores counts RelationProcessorCore entries (8 on an 8C/16T Ryzen),
// which is the width the memory-bound embedding kernels can actually use.
// Returns 0 when the query fails, so callers fall back to runtime.NumCPU().
func physicalCores() int {
	var length uint32
	r, _, _ := procGetLogicalProcessorInfo.Call(
		uintptr(relationProcessorCore), 0, uintptr(unsafe.Pointer(&length)))
	if r != 0 || length == 0 {
		return 0
	}
	// The size probe above is expected to fail with ERROR_INSUFFICIENT_BUFFER
	// and report the required length; retry once in case it changed.
	for attempt := 0; attempt < 2; attempt++ {
		buf := make([]byte, length)
		r, _, _ = procGetLogicalProcessorInfo.Call(
			uintptr(relationProcessorCore),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&length)))
		if r == 0 {
			return 0
		}
		if n := countProcessorCoreEntries(buf, length); n > 0 {
			return n
		}
	}
	return 0
}

// countProcessorCoreEntries walks the variable-length record array:
//
//	struct { uint32 Relationship; uint32 Size; <payload>; }
func countProcessorCoreEntries(buf []byte, length uint32) int {
	n := 0
	for off := uint32(0); off+8 <= length; {
		rel := *(*uint32)(unsafe.Pointer(&buf[off]))
		size := *(*uint32)(unsafe.Pointer(&buf[off+4]))
		if size < 8 || off+size > length {
			return n
		}
		if rel == relationProcessorCore {
			n++
		}
		off += size
	}
	return n
}
