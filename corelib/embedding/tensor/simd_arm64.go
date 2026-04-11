//go:build arm64

package tensor

// siluMulASM computes SiLU(gate) * up in-place into gate using ARM64 NEON.
//
//go:noescape
func siluMulASM(gate, up []float32)
