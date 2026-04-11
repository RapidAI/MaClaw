//go:build !amd64 && !arm64

package tensor

// dotQ8RowASM is the scalar fallback for platforms without SIMD assembly.
// It delegates to the pure Go implementation.
func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32 {
	return dotQ8RowScalar(a, data, rowOff, nBlocks)
}

// dequantRowIntoASM is the scalar fallback for platforms without SIMD assembly.
func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int) {
	dequantRowIntoScalar(dst, data, rowOff, nBlocks)
}
