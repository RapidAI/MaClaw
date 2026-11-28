//go:build android && cgo

package nnapi

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

static void* maclaw_dlopen(const char* name) {
    return dlopen(name, RTLD_NOW);
}

static void* maclaw_dlsym(void* handle, const char* name) {
    return dlsym(handle, name);
}

typedef uintptr_t (*maclaw_fn6)(uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t);

static uintptr_t maclaw_call(uintptr_t fn, uintptr_t a0, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4, uintptr_t a5) {
    return ((maclaw_fn6)fn)(a0, a1, a2, a3, a4, a5);
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// androidDLOpen loads a shared library without adding it to DT_NEEDED.
// libneuralnetworks is optional on older devices, so the voice .so must not link it.
func androidDLOpen(name string) (uintptr, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	h := C.maclaw_dlopen(cname)
	if h == nil {
		msg := "dlopen failed"
		if e := C.dlerror(); e != nil {
			msg = C.GoString(e)
		}
		return 0, fmt.Errorf("%s", msg)
	}
	return uintptr(h), nil
}

func androidDLSym(handle uintptr, name string) (uintptr, error) {
	if handle == 0 {
		return 0, fmt.Errorf("nil handle")
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	p := C.maclaw_dlsym(unsafe.Pointer(handle), cname)
	if p == nil {
		return 0, fmt.Errorf("missing %s", name)
	}
	return uintptr(p), nil
}

// androidCall invokes a C function of at most six integer arguments.
// NNAPI entry points used here are all within that limit. Extra registers
// are ignored by a callee that takes fewer arguments.
func androidCall(fn uintptr, args ...uintptr) uintptr {
	if fn == 0 {
		return ^uintptr(0)
	}
	if len(args) > 6 {
		return ^uintptr(0)
	}
	var a [6]uintptr
	copy(a[:], args)
	return uintptr(C.maclaw_call(
		C.uintptr_t(fn),
		C.uintptr_t(a[0]), C.uintptr_t(a[1]), C.uintptr_t(a[2]),
		C.uintptr_t(a[3]), C.uintptr_t(a[4]), C.uintptr_t(a[5]),
	))
}
