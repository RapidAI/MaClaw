package libc

import (
	"reflect"
	"unsafe"
)

// findnull returns the index of the first zero byte in a null-terminated string.
// Replaces the go:linkname to runtime.findnull which is restricted in Go 1.23+.
func findnull(str *byte) int {
	if str == nil {
		return 0
	}
	n := 0
	for p := str; *p != 0; p = (*byte)(unsafe.Add(unsafe.Pointer(p), 1)) {
		n++
	}
	return n
}

// findnullw returns the index of the first zero uint16 in a null-terminated wide string.
func findnullw(str *uint16) int {
	if str == nil {
		return 0
	}
	n := 0
	for p := str; *p != 0; p = (*uint16)(unsafe.Add(unsafe.Pointer(p), 2)) {
		n++
	}
	return n
}

// gobytes copies n bytes from p into a new byte slice.
func gobytes(p *byte, n int) []byte {
	if p == nil || n <= 0 {
		return nil
	}
	return unsafe.Slice(p, n)[:n:n]
}

// gostring converts a null-terminated C string to a Go string.
func gostring(p *byte) string {
	if p == nil {
		return ""
	}
	n := findnull(p)
	return string(unsafe.Slice(p, n))
}

// gostringnocopy converts a null-terminated C string to a Go string without copying.
func gostringnocopy(p *byte) string {
	if p == nil {
		return ""
	}
	n := findnull(p)
	return unsafe.String(p, n)
}

// gostringn converts a byte pointer and length to a Go string.
func gostringn(p *byte, l int) string {
	if p == nil || l <= 0 {
		return ""
	}
	return string(unsafe.Slice(p, l))
}

// gostringw converts a null-terminated wide (uint16) string to a Go string.
func gostringw(strw *uint16) string {
	if strw == nil {
		return ""
	}
	n := findnullw(strw)
	u16s := unsafe.Slice(strw, n)
	runes := make([]rune, n)
	for i, v := range u16s {
		runes[i] = rune(v)
	}
	return string(runes)
}

type rtype = unsafe.Pointer

type emptyInterface struct {
	typ  rtype
	word unsafe.Pointer
}

func typeof(i interface{}) rtype {
	eface := *(*emptyInterface)(unsafe.Pointer(&i))
	return eface.typ
}

func sizeof(t rtype) uintptr {
	if t == nil {
		return 0
	}
	return *(*uintptr)(t)
}

// unsafe_New allocates a new zero-value of the given type.
func unsafe_New(typ rtype) unsafe.Pointer {
	// Use reflect to create a new value of the type.
	rt := *(*reflect.Type)(unsafe.Pointer(&typ))
	v := reflect.New(rt)
	return v.UnsafePointer()
}

// unsafe_NewArray allocates a new array of the given type and size.
func unsafe_NewArray(typ rtype, size int) unsafe.Pointer {
	rt := *(*reflect.Type)(unsafe.Pointer(&typ))
	v := reflect.MakeSlice(reflect.SliceOf(rt), size, size)
	return v.UnsafePointer()
}
