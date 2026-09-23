//go:build !amd64

package yolo

// hasAVX2FMA is false on platforms without the AVX2 GEMM kernel.
const hasAVX2FMA = false

// matmulConvActiv computes out[M,N] = W[M,K] × col[K,N] + bias[M]; the
// portable fallback applies no fused activation (returns false so callers
// run their separate activation pass). ldb is the row stride of col and is
// unused by the legacy packed-matrix path.
func matmulConvActiv(W, col, bias, out []float32, M, K, N, ldb int, silu bool) bool {
	matmulConvLegacy(W, col, bias, out, M, K, N)
	return false
}

// gemmF32 is the silu=false entry point used by the attention fast path
// (unreachable off amd64 — tryForwardFast is stubbed there).
func gemmF32(out, W, B, bias []float32, M, K, N int) {
	matmulConvLegacy(W, B, bias, out, M, K, N)
}

// axpySlice computes out[i] += w * x[i] over the overlap of out and x
// (portable).
func axpySlice(out, x []float32, w float32) {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	if n == 0 || w == 0 {
		return
	}
	for i := 0; i < n; i++ {
		out[i] += w * x[i]
	}
}

// siluSlice applies SiLU in place: x[i] = x[i] * sigmoid(x[i]) (portable).
func siluSlice(x []float32) {
	for i, v := range x {
		x[i] = v * sigmoid(v)
	}
}
