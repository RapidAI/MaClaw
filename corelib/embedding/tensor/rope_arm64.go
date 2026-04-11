//go:build arm64

package tensor

// ropePrecomputedASM applies RoPE using pre-computed cos/sin tables with ARM64 NEON.
//
//go:noescape
func ropePrecomputedASM(x []float32, nHeads, headDim int, cosTable, sinTable []float32)
