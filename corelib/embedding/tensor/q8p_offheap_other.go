//go:build !linux && !darwin && !freebsd && !windows

package tensor

import "unsafe"

func allocOffHeap(n int) ([]byte, func()) { return nil, nil }

func bytesAsF32(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}
