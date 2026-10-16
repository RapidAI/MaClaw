//go:build !amd64

package tensor

// VNNI M8 kernels are amd64-only; other architectures keep the f32 paths.

func gemmaVNNIQKV(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq, maxWorkers int) bool {
	return false
}

func gemmaVNNIDualOut(gate, up, a []float32, wG, wU *Q8Tensor, seq, maxWorkers int) bool {
	return false
}

func gemmaVNNIRMSResidual(x, a []float32, b *Q8Tensor, wRMS []float32, seq, N, K, maxWorkers int, eps float32) bool {
	return false
}
