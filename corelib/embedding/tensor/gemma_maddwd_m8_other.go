//go:build !amd64

package tensor

// VPMADDWD M8 kernels are amd64-only; other architectures keep the f32 paths.

func gemmaMaddwdQKV(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq, maxWorkers int) bool {
	return false
}

func gemmaMaddwdDualOut(gate, up, a []float32, wG, wU *Q8Tensor, seq, maxWorkers int) bool {
	return false
}

func gemmaMaddwdRMSResidual(x, a []float32, b *Q8Tensor, wRMS []float32, seq, N, K, maxWorkers int, eps float32) bool {
	return false
}
