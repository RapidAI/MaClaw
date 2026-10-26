//go:build !windows

package vk

import (
	"errors"

	"github.com/ebitengine/purego"
)

// vkLoader wraps the Vulkan loader shared object (libvulkan.so.1 /
// libvulkan.1.dylib) through purego, which works with CGO_ENABLED=0.
type vkLoader struct {
	handle uintptr
}

func openLoader() (*vkLoader, error) {
	for _, name := range []string{"libvulkan.so.1", "libvulkan.so", "libvulkan.1.dylib", "libvulkan.dylib"} {
		if h, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL); err == nil {
			return &vkLoader{handle: h}, nil
		}
	}
	return nil, errors.New("vk: libvulkan not found")
}

func (l *vkLoader) sym(name string) uintptr {
	if l == nil || l.handle == 0 {
		return 0
	}
	p, err := purego.Dlsym(l.handle, name)
	if err != nil {
		return 0
	}
	return p
}

func (l *vkLoader) close() {
	if l != nil && l.handle != 0 {
		purego.Dlclose(l.handle)
		l.handle = 0
	}
}

// syscallN calls a C function with up to 12 pointer-sized integer arguments.
func syscallN(fn uintptr, args ...uintptr) uintptr {
	if fn == 0 {
		return 1
	}
	r, _, _ := purego.SyscallN(fn, args...)
	return r
}
