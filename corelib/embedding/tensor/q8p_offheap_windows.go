//go:build windows

package tensor

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func allocOffHeap(n int) ([]byte, func()) {
	if n <= 0 {
		return nil, nil
	}
	addr, err := windows.VirtualAlloc(0, uintptr(n), windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil || addr == 0 {
		return nil, nil
	}
	// Not Go-managed memory: the pointer is stable until VirtualFree.
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	b := unsafe.Slice((*byte)(p), n)
	return b, func() { _ = windows.VirtualFree(addr, 0, windows.MEM_RELEASE) }
}

func bytesAsF32(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}
