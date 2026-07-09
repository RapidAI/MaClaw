//go:build !amd64 && !arm64

package tensor

// multiDot4 computes 4 dots of consecutive A rows against B.
// a is [4][K] packed (row-major), b is [K], results written to out[0..3].
func multiDot4(out *[4]float32, a, b []float32, K int) {
	var s0, s1, s2, s3 float32
	for k := 0; k < K; k++ {
		bk := b[k]
		s0 += a[k] * bk
		s1 += a[K+k] * bk
		s2 += a[2*K+k] * bk
		s3 += a[3*K+k] * bk
	}
	out[0], out[1], out[2], out[3] = s0, s1, s2, s3
}

func multiDot8(out *[8]float32, a, b []float32, K int) {
	var d0, d1 [4]float32
	multiDot4(&d0, a[:4*K], b, K)
	multiDot4(&d1, a[4*K:8*K], b, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func multiDot4DualB(out *[8]float32, a, b0, b1 []float32, K int) {
	var d0, d1 [4]float32
	multiDot4(&d0, a, b0, K)
	multiDot4(&d1, a, b1, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func multiDot8DualB(out0, out1 *[8]float32, a, b0, b1 []float32, K int) {
	multiDot4DualB(out0, a[:4*K], b0, b1, K)
	multiDot4DualB(out1, a[4*K:8*K], b0, b1, K)
}
