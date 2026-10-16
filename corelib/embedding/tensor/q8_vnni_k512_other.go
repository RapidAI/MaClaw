//go:build !amd64

package tensor

// VNNI k512 kernels are amd64-only; other architectures fall back to the
// generic paths in q8.go.

func tryFusedK512VNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool) bool {
	return false
}

func tryArgmaxK512VNNI(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, ns, ne int) bool {
	return false
}
