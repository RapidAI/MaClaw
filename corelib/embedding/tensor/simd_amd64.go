//go:build amd64

package tensor

// --- AVX2 assembly (Haswell+) ---

//go:noescape
func siluMulAVX2(gate, up []float32)

// --- AVX1 assembly (Sandy/Ivy Bridge) ---

//go:noescape
func siluMulAVX1(gate, up []float32)

// --- Runtime dispatch ---

func siluMulASM(gate, up []float32) {
	if hasAVX2andFMA {
		siluMulAVX2(gate, up)
		return
	}
	if hasAVX {
		siluMulAVX1(gate, up)
		return
	}
	siluMulScalar(gate, up)
}
