//go:build amd64

package kokoro

//go:noescape
func dot3AVX2(a0, a1, a2, w0, w1, w2 []float32) float32

//go:noescape
func dot3FMA(a0, a1, a2, w0, w1, w2 []float32) float32

// Keep the fused conv knobs separate so AVX2-only and scalar fallback behavior
// can be tested without changing the global Kokoro SIMD setting.
var kokoroFusedConvASMEnabled = !envBool("KOKORO_DISABLE_FUSED_CONV_ASM")
var kokoroFusedConvFMAEnabled = cpuSupportsKokoroFMA() && !envBool("KOKORO_DISABLE_FUSED_CONV_FMA")

func useKokoroFusedConvASM() bool { return useKokoroSIMD() && kokoroFusedConvASMEnabled }

func useKokoroFusedConvFMA() bool { return useKokoroFusedConvASM() && kokoroFusedConvFMAEnabled }

func dot3Fused(a0, a1, a2, w0, w1, w2 []float32) float32 {
	if useKokoroFusedConvASM() && sameLen3Dot(a0, a1, a2, w0, w1, w2) {
		if useKokoroFusedConvFMA() {
			return dot3FMA(a0, a1, a2, w0, w1, w2)
		}
		return dot3AVX2(a0, a1, a2, w0, w1, w2)
	}
	return dot32(a0, w0) + dot32(a1, w1) + dot32(a2, w2)
}

func sameLen3Dot(a0, a1, a2, w0, w1, w2 []float32) bool {
	return len(a0) == len(w0) && len(a1) == len(w1) && len(a2) == len(w2) && len(a0) == len(a1) && len(a0) == len(a2)
}
