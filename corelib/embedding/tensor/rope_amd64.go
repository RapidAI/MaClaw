//go:build amd64

package tensor

// ropePrecomputedAVX applies RoPE using pre-computed cos/sin tables with AVX.
// RoPE assembly only needs AVX1, not AVX2.
//
//go:noescape
func ropePrecomputedAVX(x []float32, nHeads, headDim int, cosTable, sinTable []float32)

// ropePrecomputedASM dispatches to AVX assembly or scalar fallback based on CPU caps.
func ropePrecomputedASM(x []float32, nHeads, headDim int, cosTable, sinTable []float32) {
	if hasAVX {
		ropePrecomputedAVX(x, nHeads, headDim, cosTable, sinTable)
		return
	}
	ropePrecomputedScalar(x, nHeads, headDim, cosTable, sinTable)
}
