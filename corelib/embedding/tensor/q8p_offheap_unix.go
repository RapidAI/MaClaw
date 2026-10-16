//go:build linux || darwin || freebsd

package tensor

import (
	"syscall"
	"unsafe"
)

func allocOffHeap(n int) ([]byte, func()) {
	if n <= 0 {
		return nil, nil
	}
	b, err := syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return nil, nil
	}
	return b, func() { _ = syscall.Munmap(b) }
}

func bytesAsF32(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}
