//go:build amd64

package tensor

import "unsafe"

// multiDot4 computes 4 dots of consecutive A rows against the same B vector.
// a layout: [4][K] row-major contiguous; b is [K]; out receives 4 results.
// On AVX2+FMA, B is loaded once per 8-wide chunk and reused across the 4 A rows.
func multiDot4(out *[4]float32, a, b []float32, K int) {
	if hasAVX2andFMA && len(a) >= 4*K && len(b) >= K {
		switch K {
		case 128:
			multiDot4AVX2K128(out, &a[0], &b[0])
			return
		case 512:
			multiDot4AVX2K512(out, &a[0], &b[0])
			return
		case 560: // SenseVoice entry QKV (F32 weights)
			multiDot4AVX2K560(out, &a[0], &b[0])
			return
		}
		if K >= 8 {
			multiDot4AVX2(out, &a[0], &b[0], K)
			return
		}
	}
	multiDot4Scalar(out, a, b, K)
}

// multiDot8 is the 8-row micro-kernel (better B amortization when M is large).
func multiDot8(out *[8]float32, a, b []float32, K int) {
	if hasAVX2andFMA && len(a) >= 8*K && len(b) >= K {
		switch K {
		case 128:
			// Write directly into out halves — no intermediate [4] copy.
			multiDot4AVX2K128((*[4]float32)(unsafe.Pointer(&out[0])), &a[0], &b[0])
			multiDot4AVX2K128((*[4]float32)(unsafe.Pointer(&out[4])), &a[4*128], &b[0])
			return
		case 512:
			multiDot4AVX2K512((*[4]float32)(unsafe.Pointer(&out[0])), &a[0], &b[0])
			multiDot4AVX2K512((*[4]float32)(unsafe.Pointer(&out[4])), &a[4*512], &b[0])
			return
		case 560:
			multiDot4AVX2K560((*[4]float32)(unsafe.Pointer(&out[0])), &a[0], &b[0])
			multiDot4AVX2K560((*[4]float32)(unsafe.Pointer(&out[4])), &a[4*560], &b[0])
			return
		}
		if K >= 8 {
			multiDot8AVX2(out, &a[0], &b[0], K)
			return
		}
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
func multiDot4AVX2K128(out *[4]float32, a, b *float32)

//go:noescape
func multiDot4AVX2K512(out *[4]float32, a, b *float32)

//go:noescape
func multiDot4AVX2K560(out *[4]float32, a, b *float32)

//go:noescape
func multiDot8AVX2(out *[8]float32, a, b *float32, K int)

//go:noescape
func multiDot4DualBAVX2(out *[8]float32, a, b0, b1 *float32, K int)

// multiDot4DualB: 4 A rows × 2 B vectors.
// out[0:4] = dots with b0, out[4:8] = dots with b1.
// Loads each A chunk once and FMAs into both B accumulators.
func multiDot4DualB(out *[8]float32, a, b0, b1 []float32, K int) {
	if hasAVX2andFMA && len(a) >= 4*K && len(b0) >= K && len(b1) >= K {
		switch K {
		case 128: // SenseVoice headDim (attention scores)
			multiDot4DualBAVX2K128(out, &a[0], &b0[0], &b1[0])
			return
		case 512: // SenseVoice hidden (dequant-once encoder GEMM)
			multiDot4DualBAVX2K512(out, &a[0], &b0[0], &b1[0])
			return
		case 560: // SenseVoice entry QKV (F32, 560-dim)
			multiDot4DualBAVX2K560(out, &a[0], &b0[0], &b1[0])
			return
		case 2048: // FFN down-proj after dequant-once of dual B
			multiDot4DualBAVX2K2048(out, &a[0], &b0[0], &b1[0])
			return
		}
		if K >= 8 {
			multiDot4DualBAVX2(out, &a[0], &b0[0], &b1[0], K)
			return
		}
	}
	multiDot4DualBScalar(out, a, b0, b1, K)
}

// multiDot8DualB: 8 A rows × 2 B vectors via two dual-4 kernels.
// B vectors stay hot in L1 across the two dual-4 calls.
func multiDot8DualB(out0, out1 *[8]float32, a, b0, b1 []float32, K int) {
	if hasAVX2andFMA && len(a) >= 8*K && len(b0) >= K && len(b1) >= K {
		switch K {
		case 128:
			multiDot4DualBAVX2K128(out0, &a[0], &b0[0], &b1[0])
			multiDot4DualBAVX2K128(out1, &a[4*128], &b0[0], &b1[0])
			return
		case 512:
			multiDot4DualBAVX2K512(out0, &a[0], &b0[0], &b1[0])
			multiDot4DualBAVX2K512(out1, &a[4*512], &b0[0], &b1[0])
			return
		case 560:
			multiDot4DualBAVX2K560(out0, &a[0], &b0[0], &b1[0])
			multiDot4DualBAVX2K560(out1, &a[4*560], &b0[0], &b1[0])
			return
		case 2048:
			multiDot4DualBAVX2K2048(out0, &a[0], &b0[0], &b1[0])
			multiDot4DualBAVX2K2048(out1, &a[4*2048], &b0[0], &b1[0])
			return
		}
	}
	multiDot4DualB(out0, a[:4*K], b0, b1, K)
	multiDot4DualB(out1, a[4*K:8*K], b0, b1, K)
}

// multiDot4TripleB: 4 A rows × 3 B vectors.
// out[0:4]=b0, out[4:8]=b1, out[8:12]=b2.
// Loads each A chunk once for all three B (better than dual+single).
func multiDot4TripleB(out *[12]float32, a, b0, b1, b2 []float32, K int) {
	if hasAVX2andFMA && len(a) >= 4*K && len(b0) >= K && len(b1) >= K && len(b2) >= K {
		switch K {
		case 128:
			multiDot4TripleBAVX2K128(out, &a[0], &b0[0], &b1[0], &b2[0])
			return
		case 512:
			multiDot4TripleBAVX2K512(out, &a[0], &b0[0], &b1[0], &b2[0])
			return
		}
	}
	var d8 [8]float32
	var d4 [4]float32
	multiDot4DualB(&d8, a, b0, b1, K)
	multiDot4(&d4, a, b2, K)
	out[0], out[1], out[2], out[3] = d8[0], d8[1], d8[2], d8[3]
	out[4], out[5], out[6], out[7] = d8[4], d8[5], d8[6], d8[7]
	out[8], out[9], out[10], out[11] = d4[0], d4[1], d4[2], d4[3]
}

// MultiDot4TripleB is the public API for 4 A × 3 B micro-kernel.
func MultiDot4TripleB(out *[12]float32, a, b0, b1, b2 []float32, K int) {
	multiDot4TripleB(out, a, b0, b1, b2, K)
}

// multiDot8TripleB: 8 A × 3 B via two triple-4 kernels.
// Fixed-K paths call asm directly (no intermediate slice/dispatch).
func multiDot8TripleB(out0, out1 *[12]float32, a, b0, b1, b2 []float32, K int) {
	if hasAVX2andFMA && len(a) >= 8*K && len(b0) >= K && len(b1) >= K && len(b2) >= K {
		switch K {
		case 128:
			multiDot4TripleBAVX2K128(out0, &a[0], &b0[0], &b1[0], &b2[0])
			multiDot4TripleBAVX2K128(out1, &a[4*128], &b0[0], &b1[0], &b2[0])
			return
		case 512:
			multiDot4TripleBAVX2K512(out0, &a[0], &b0[0], &b1[0], &b2[0])
			multiDot4TripleBAVX2K512(out1, &a[4*512], &b0[0], &b1[0], &b2[0])
			return
		}
	}
	multiDot4TripleB(out0, a[:4*K], b0, b1, b2, K)
	multiDot4TripleB(out1, a[4*K:8*K], b0, b1, b2, K)
}

//go:noescape
func multiDot4DualBAVX2K128(out *[8]float32, a, b0, b1 *float32)

//go:noescape
func multiDot4DualBAVX2K512(out *[8]float32, a, b0, b1 *float32)

//go:noescape
func multiDot4DualBAVX2K560(out *[8]float32, a, b0, b1 *float32)

//go:noescape
func multiDot4DualBAVX2K2048(out *[8]float32, a, b0, b1 *float32)

//go:noescape
func multiDot4TripleBAVX2K128(out *[12]float32, a, b0, b1, b2 *float32)

//go:noescape
func multiDot4TripleBAVX2K512(out *[12]float32, a, b0, b1, b2 *float32)

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
