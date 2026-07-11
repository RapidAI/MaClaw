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

func dequantRowScaledDual(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int) {
	dequantRowScaledASM(dst0, data, scales0, rowOff0, nBlocks)
	dequantRowScaledASM(dst1, data, scales1, rowOff1, nBlocks)
}

func dequantRowScaledTriple(dst0, dst1, dst2 []float32, data []byte, scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2, nBlocks int) {
	dequantRowScaledDual(dst0, dst1, data, scales0, scales1, rowOff0, rowOff1, nBlocks)
	dequantRowScaledASM(dst2, data, scales2, rowOff2, nBlocks)
}

func prepareScalesBulk(dst []float32, data []byte) bool { return false }
