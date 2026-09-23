//go:build arm64

package kokoro

// dot3NEON is the operator-fused 3-tap dot product (see kernels_arm64.s).
// It fuses what would otherwise be three separate dot products per conv
// output point into one pass, mirroring the amd64 dot3AVX2/dot3FMA path.
//
//go:noescape
func dot3NEON(a0, a1, a2, w0, w1, w2 []float32) float32

var kokoroFusedConvASMEnabled = !envBool("KOKORO_DISABLE_FUSED_CONV_ASM")

func useKokoroFusedConvASM() bool { return useKokoroSIMD() && kokoroFusedConvASMEnabled }

func dot3Fused(a0, a1, a2, w0, w1, w2 []float32) float32 {
	if useKokoroFusedConvASM() && sameLen3Dot(a0, a1, a2, w0, w1, w2) {
		return dot3NEON(a0, a1, a2, w0, w1, w2)
	}
	return dot32(a0, w0) + dot32(a1, w1) + dot32(a2, w2)
}

func sameLen3Dot(a0, a1, a2, w0, w1, w2 []float32) bool {
	return len(a0) == len(w0) && len(a1) == len(w1) && len(a2) == len(w2) && len(a0) == len(a1) && len(a0) == len(a2)
}
