//go:build amd64

package tensor

// --- AVX2 assembly (Haswell+) ---

//go:noescape
func siluMulAVX2(gate, up []float32)

//go:noescape
func siluMulAVX512(gate, up []float32)

// --- AVX1 assembly (Sandy/Ivy Bridge) ---

//go:noescape
func siluMulAVX1(gate, up []float32)

// --- Runtime dispatch ---

//go:noescape
func vzeroupperASM()

func siluMulASM(gate, up []float32) {
	if hasAVX512 {
		siluMulAVX512(gate, up)
		return
	}
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
