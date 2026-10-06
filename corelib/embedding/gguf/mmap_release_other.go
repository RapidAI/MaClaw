//go:build !linux

package gguf

// releasePages is a no-op off Linux (the OS trims clean file pages itself).
func releasePages(b []byte) {}
