//go:build !amd64

package kokoro

func useKokoroFusedConvASM() bool { return false }

func dot3Fused(a0, a1, a2, w0, w1, w2 []float32) float32 {
	return dot32(a0, w0) + dot32(a1, w1) + dot32(a2, w2)
}

func sameLen3Dot(a0, a1, a2, w0, w1, w2 []float32) bool {
	return len(a0) == len(w0) && len(a1) == len(w1) && len(a2) == len(w2) && len(a0) == len(a1) && len(a0) == len(a2)
}
