//go:build !amd64 && !arm64

package tensor

// siluMulASM is the scalar fallback for platforms without SIMD assembly.
func siluMulASM(gate, up []float32) {
	siluMulScalar(gate, up)
}

func vzeroupperASM() {}
