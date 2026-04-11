//go:build amd64

package tensor

// siluMulASM computes SiLU(gate) * up in-place into gate using AVX2.
// gate and up must have the same length. Length must be >= 0.
// Uses the Schraudolph fast exp approximation in SIMD.
//
//go:noescape
func siluMulASM(gate, up []float32)
