//go:build amd64

package tensor

// multiDot4 computes 4 dots of consecutive A rows against the same B vector.
// a layout: [4][K] row-major contiguous; b is [K]; out receives 4 results.
// On AVX2+FMA, B is loaded once per 8-wide chunk and reused across the 4 A rows.
func multiDot4(out *[4]float32, a, b []float32, K int) {
	if hasAVX2andFMA && K >= 8 && len(a) >= 4*K && len(b) >= K {
		multiDot4AVX2(out, &a[0], &b[0], K)
		return
	}
	multiDot4Scalar(out, a, b, K)
}

// multiDot8 is the 8-row micro-kernel (better B amortization when M is large).
func multiDot8(out *[8]float32, a, b []float32, K int) {
	if hasAVX2andFMA && K >= 8 && len(a) >= 8*K && len(b) >= K {
		multiDot8AVX2(out, &a[0], &b[0], K)
		return
	}
	var d0, d1 [4]float32
	multiDot4(&d0, a[:4*K], b, K)
	multiDot4(&d1, a[4*K:8*K], b, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

//go:noescape
func multiDot4AVX2(out *[4]float32, a, b *float32, K int)

//go:noescape
func multiDot8AVX2(out *[8]float32, a, b *float32, K int)

//go:noescape
func multiDot4DualBAVX2(out *[8]float32, a, b0, b1 *float32, K int)

// multiDot4DualB: 4 A rows × 2 B vectors.
// out[0:4] = dots with b0, out[4:8] = dots with b1.
// Loads each A chunk once and FMAs into both B accumulators.
func multiDot4DualB(out *[8]float32, a, b0, b1 []float32, K int) {
	if hasAVX2andFMA && K >= 8 && len(a) >= 4*K && len(b0) >= K && len(b1) >= K {
		multiDot4DualBAVX2(out, &a[0], &b0[0], &b1[0], K)
		return
	}
	multiDot4DualBScalar(out, a, b0, b1, K)
}

// multiDot8DualB: 8 A rows × 2 B vectors via two dual-4 kernels.
// B vectors stay hot in L1 across the two dual-4 calls.
func multiDot8DualB(out0, out1 *[8]float32, a, b0, b1 []float32, K int) {
	multiDot4DualB(out0, a[:4*K], b0, b1, K)
	multiDot4DualB(out1, a[4*K:8*K], b0, b1, K)
}

func multiDot4Scalar(out *[4]float32, a, b []float32, K int) {
	var s0, s1, s2, s3 float32
	k := 0
	for ; k+7 < K; k += 8 {
		for i := 0; i < 8; i++ {
			bk := b[k+i]
			s0 += a[k+i] * bk
			s1 += a[K+k+i] * bk
			s2 += a[2*K+k+i] * bk
			s3 += a[3*K+k+i] * bk
		}
	}
	for ; k < K; k++ {
		bk := b[k]
		s0 += a[k] * bk
		s1 += a[K+k] * bk
		s2 += a[2*K+k] * bk
		s3 += a[3*K+k] * bk
	}
	out[0], out[1], out[2], out[3] = s0, s1, s2, s3
}

func multiDot4DualBScalar(out *[8]float32, a, b0, b1 []float32, K int) {
	var s0, s1, s2, s3, t0, t1, t2, t3 float32
	k := 0
	for ; k+3 < K; k += 4 {
		for i := 0; i < 4; i++ {
			bk0 := b0[k+i]
			bk1 := b1[k+i]
			a0 := a[k+i]
			a1 := a[K+k+i]
			a2 := a[2*K+k+i]
			a3 := a[3*K+k+i]
			s0 += a0 * bk0
			s1 += a1 * bk0
			s2 += a2 * bk0
			s3 += a3 * bk0
			t0 += a0 * bk1
			t1 += a1 * bk1
			t2 += a2 * bk1
			t3 += a3 * bk1
		}
	}
	for ; k < K; k++ {
		bk0 := b0[k]
		bk1 := b1[k]
		a0 := a[k]
		a1 := a[K+k]
		a2 := a[2*K+k]
		a3 := a[3*K+k]
		s0 += a0 * bk0
		s1 += a1 * bk0
		s2 += a2 * bk0
		s3 += a3 * bk0
		t0 += a0 * bk1
		t1 += a1 * bk1
		t2 += a2 * bk1
		t3 += a3 * bk1
	}
	out[0], out[1], out[2], out[3] = s0, s1, s2, s3
	out[4], out[5], out[6], out[7] = t0, t1, t2, t3
}
