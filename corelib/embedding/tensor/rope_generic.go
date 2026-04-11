//go:build !amd64 && !arm64

package tensor

// ropePrecomputedASM is the scalar fallback for platforms without SIMD assembly.
func ropePrecomputedASM(x []float32, nHeads, headDim int, cosTable, sinTable []float32) {
	ropePrecomputedScalar(x, nHeads, headDim, cosTable, sinTable)
}
