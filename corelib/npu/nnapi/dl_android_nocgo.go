//go:build android && !cgo

package nnapi

import "fmt"

// Without cgo there is no dlopen. The phone library is built with cgo; this
// file only keeps a CGO_ENABLED=0 cross-compile from failing closed.
func androidDLOpen(name string) (uintptr, error) {
	return 0, fmt.Errorf("cgo required to dlopen %s", name)
}

func androidDLSym(uintptr, string) (uintptr, error) {
	return 0, fmt.Errorf("cgo required")
}

func androidCall(uintptr, ...uintptr) uintptr {
	return ^uintptr(0)
}
