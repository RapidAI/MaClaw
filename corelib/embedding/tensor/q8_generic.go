//go:build !amd64 && !arm64

package tensor

import "unsafe"

// dotQ8RowASM is the scalar fallback for platforms without SIMD assembly.
// It delegates to the pure Go implementation.
func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32 {
	return dotQ8RowScalar(a, data, rowOff, nBlocks)
}

// dequantRowIntoASM is the scalar fallback for platforms without SIMD assembly.
func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int) {
	dequantRowIntoScalar(dst, data, rowOff, nBlocks)
}

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
