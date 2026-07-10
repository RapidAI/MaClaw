//go:build !amd64

package tensor

func softmaxInplaceInvASM(scores []float32) float32 {
	return softmaxInplaceInvScalar(scores)
}

func softmaxInplaceInvDualASM(sc0, sc1 []float32) (float32, float32) {
	return softmaxInplaceInvScalar(sc0), softmaxInplaceInvScalar(sc1)
}
