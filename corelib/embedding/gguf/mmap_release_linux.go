//go:build linux

package gguf

import (
	"os"
	"syscall"
	"unsafe"
)

// releasePages drops the resident pages of b (a sub-slice of a read-only
// file mapping) with MADV_DONTNEED. Pages fault back in from the file if
// touched again, so this only lowers RSS; it never changes content.
func releasePages(b []byte) {
	ps := os.Getpagesize()
	start := uintptr(unsafe.Pointer(unsafe.SliceData(b)))
	end := start + uintptr(len(b))
	as := (start + uintptr(ps) - 1) &^ uintptr(ps-1)
	ae := end &^ uintptr(ps-1)
	if ae <= as {
		return
	}
	off := int(as - start)
	_ = syscall.Madvise(b[off:off+int(ae-as)], syscall.MADV_DONTNEED)
}
