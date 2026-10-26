//go:build windows

package vk

import (
	"syscall"
	"unsafe"
)

// vkLoader wraps the Vulkan loader DLL. Windows has no dlopen, so the Go
// syscall package loads it and resolves entry points by name.
type vkLoader struct {
	dll *syscall.DLL
}

func openLoader() (*vkLoader, error) {
	// The ICD loader ships with the graphics driver (System32\vulkan-1.dll).
	dll, err := syscall.LoadDLL("vulkan-1.dll")
	if err != nil {
		return nil, err
	}
	return &vkLoader{dll: dll}, nil
}

func (l *vkLoader) sym(name string) uintptr {
	p, err := l.dll.FindProc(name)
	if err != nil {
		return 0
	}
	return p.Addr()
}

func (l *vkLoader) close() {
	if l.dll != nil {
		l.dll.Release()
		l.dll = nil
	}
}

// syscallN calls a C function with up to 8 pointer-sized integer arguments
// through the Windows x64 / arm64 calling convention.
func syscallN(fn uintptr, args ...uintptr) uintptr {
	if fn == 0 {
		return 1
	}
	switch len(args) {
	case 0:
		r, _, _ := syscall.SyscallN(fn)
		return r
	case 1:
		r, _, _ := syscall.SyscallN(fn, args[0])
		return r
	case 2:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1])
		return r
	case 3:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2])
		return r
	case 4:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3])
		return r
	case 5:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4])
		return r
	case 6:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5])
		return r
	case 7:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6])
		return r
	case 8:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7])
		return r
	case 9:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8])
		return r
	case 10:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9])
		return r
	case 11:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9], args[10])
		return r
	case 12:
		r, _, _ := syscall.SyscallN(fn, args[0], args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9], args[10], args[11])
		return r
	default:
		return 1
	}
}

var _ = unsafe.Pointer(nil)
