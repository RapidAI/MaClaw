//go:build !amd64

package tensor

func matMulQ4BiasAddPanel(out, a []float32, w *Q4Tensor, bias []float32, M, N, K int) bool {
	return false
}
