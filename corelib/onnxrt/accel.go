package onnxrt

import (
	"runtime"
	"strings"
)

// Accel reports the inference backends this process will use.
//
// "avx512" or "avx2" selects the amd64 GEMM (MatMul, and Conv after im2col).
// "neon" selects the arm64 GEMM. "metal" is the darwin Conv and elementwise
// path; MatMul and decoder GEMVs stay on the CPU SIMD kernels, because an
// untested Metal GEMM would share the self-check that disables every Metal
// op. "scalar" means none of those paths are selected. A machine with more
// than one backend joins them with "+", for example "metal+neon".
func Accel() string {
	var parts []string
	if metalSupported() {
		parts = append(parts, "metal")
	}
	switch {
	case hasAVX512ZMM:
		parts = append(parts, "avx512")
	case hasAVX2FMA:
		parts = append(parts, "avx2")
	}
	if runtime.GOARCH == "arm64" {
		parts = append(parts, "neon")
	}
	if len(parts) == 0 {
		return "scalar"
	}
	return strings.Join(parts, "+")
}
