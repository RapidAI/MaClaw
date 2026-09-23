//go:build amd64

package yolo

import "golang.org/x/sys/cpu"

// hasAVX512 reports whether the CPU has the AVX512F/VL/DQ/BW feature set.
// The matmul dispatch uses it to select the register-blocked kernel path.
var hasAVX512 = cpu.X86.HasAVX512F && cpu.X86.HasAVX512VL &&
	cpu.X86.HasAVX512DQ && cpu.X86.HasAVX512BW

// gemmF32 computes out[M,N] = W[M,K] × B[K,N] + bias[M]. It dispatches to
// the register-blocked AVX2 kernel when the shape allows (no transpose of
// the [K,N] operand, bias applied in the kernel epilogue) and falls back to
// the portable dot-product path otherwise.
func gemmF32(out, W, B, bias []float32, M, K, N int) {
	matmulConvActiv(W, B, bias, out, M, K, N, N, false)
}
