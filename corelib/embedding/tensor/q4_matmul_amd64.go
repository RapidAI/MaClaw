//go:build amd64

package tensor

// matMulQ4BiasAddPanel handles SenseVoice FFN-down: K=2048, nonnegative
// activations, and output columns consumed in adjacent pairs.
func matMulQ4BiasAddPanel(out, a []float32, w *Q4Tensor, bias []float32, M, N, K int) bool {
	if !hasAVX512VNNI || K != 2048 || w.Cols != 2048 || N > w.Rows || M < 8 {
		return false
	}
	ap := q8APanelPool.Get().(*q8APanel8)
	defer q8APanelPool.Put(ap)
	m := 0
	for ; m+7 < M; m += 8 {
		quantizePanel8Q8U(ap, a[m*K:(m+8)*K])
		n := 0
		for ; n+1 < N; n += 2 {
			var b0, b1 float32
			if n < len(bias) {
				b0 = bias[n]
			}
			if n+1 < len(bias) {
				b1 = bias[n+1]
			}
			if !q4Q8Dual8AccumVNNI(out, ap, w, m, n, N, b0, b1) {
				return false
			}
		}
		for ; n < N; n++ {
			for r := 0; r < 8; r++ {
				v := dotQ4F32(a[(m+r)*K:(m+r+1)*K], w, n)
				if n < len(bias) {
					v += bias[n]
				}
				out[(m+r)*N+n] += v
			}
		}
	}
	// Complete a non-8-row tail here so the public operation is total.
	for ; m < M; m++ {
		for n := 0; n < N; n++ {
			v := dotQ4F32(a[m*K:(m+1)*K], w, n)
			if n < len(bias) {
				v += bias[n]
			}
			out[m*N+n] += v
		}
	}
	return true
}
