//go:build !android

package nnapi

import "fmt"

// Available reports whether an NNAPI accelerator can take a whole graph.
func Available() bool { return false }

// UnavailableReason explains why Available is false.
func UnavailableReason() string { return "nnapi: not android" }

// Compile builds an accelerator session. Non-Android builds always fail so
// the CPU kernels stay in use.
func Compile(b *Builder, cacheDir string, token [32]byte) (*Session, error) {
	return nil, fmt.Errorf("nnapi: accelerator compile is android-only")
}
