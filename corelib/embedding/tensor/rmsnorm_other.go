//go:build !arm64

package tensor

func rmsNormAsm(out, x, weight []float32, eps float32) bool { return false }
