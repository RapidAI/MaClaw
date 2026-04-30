package libopus

// linkname_compat.go provides compatibility shims for the cxgo/runtime/libc
// package which uses go:linkname to access runtime.findnull and runtime.findnullw.
// In Go 1.23+, go:linkname to unexported runtime symbols requires the target
// package to explicitly allow it. This file satisfies the linker by importing
// the unsafe package (which is required for go:linkname to work).
//
// If the linker still complains, build with: -ldflags="-checklinkname=0"

import _ "unsafe"
