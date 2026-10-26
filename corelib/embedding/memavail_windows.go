//go:build windows

package embedding

import (
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var procGlobalMemoryStatusEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// availableMemory returns ullAvailPhys from GlobalMemoryStatusEx (no cgo).
func availableMemory() (uint64, bool) {
	if procGlobalMemoryStatusEx.Find() != nil {
		return 0, false
	}
	var st memoryStatusEx
	st.Length = uint32(unsafe.Sizeof(st))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&st)))
	if r == 0 {
		return 0, false
	}
	return st.AvailPhys, true
}

// AvailableMemory reports the physical memory the OS says is free, for
// diagnostics. ok=false when the platform cannot answer.
func AvailableMemory() (uint64, bool) { return availableMemory() }
