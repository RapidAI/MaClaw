//go:build !amd64

package tensor

// HasAVX512VNNI is always false off amd64 (no AVX-512 VNNI kernels there).
func HasAVX512VNNI() bool { return false }
