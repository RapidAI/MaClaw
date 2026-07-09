//go:build arm64

package tensor

import "unsafe"

// dotQ8RowASM computes the fused dequant-dot product of a float32 vector
// with a single Q8_0 row using ARM64 NEON SIMD instructions.
//
//go:noescape
func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32

// dequantRowIntoASM dequantizes a Q8_0 row into a float32 destination buffer
// using ARM64 NEON SIMD instructions.
//
//go:noescape
func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int)

// dequantRowScaledASM: f32 scale cache path (scalar unrolled; NEON dequant still
// does f16 convert on the non-scaled ASM entry).
func dequantRowScaledASM(dst []float32, data []byte, scales *float32, rowOff, nBlocks int) {
	sc := unsafe.Slice(scales, nBlocks)
	dequantRowScaledScalar(dst, data, sc, rowOff, nBlocks)
}
