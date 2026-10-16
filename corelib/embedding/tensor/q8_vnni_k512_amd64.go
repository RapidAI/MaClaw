//go:build amd64

package tensor

import (
	"math"
	"sync"
	"sync/atomic"
)

// K=512 signed-A VNNI path (Zen4+ VPDPBUSD) for the SenseVoice encoder hot
// layers: QKV projection (N=1536 plain), attention out-projection (N=512
// residual accum), FFN up-projection (N=2048 ReLU) and the CTC argmax head.
// Mirrors the K=2048 FFN-down VNNI design (q8APanel8 / tryFusedAccumVNNI),
// but activations are signed (LayerNorm output), so A is quantized as
// unsigned with a +128 bias and each per-block int accumulator is compensated
// in-kernel (VPDPBUSD against 0x80, subtracted before the scale FMA).

// enableFusedK512VNNI gates the K=512 int8 VNNI encoder paths (Q8Z/Q8R/YMM
// per-block variants for QKV/out/FFN-up) via the per-layer gates below.
// Precision note: int8 activation quantization shifts encoder logits by
// ~1e-3, and the reference model has a CTC top-2 near-tie with margin 1.3e-3
// (frame 90 of the reference fixture), so enabling ALL three layers flips that
// token (drops "，"). Bisect result: any single layer or any PAIR of layers
// keeps the reference transcription bit-identical to the F32 paths, so the
// production default enables the two largest layers (QKV+FFN-up) and leaves
// out-proj off until the model is recalibrated (target: min CTC top-2 margin
// > ~1e-2 absolute, measurable via SetDebugArgmaxMarginForTest).
var enableFusedK512VNNI = true

// SetFusedK512VNNIForTest toggles the per-block K=512 VNNI path.
func SetFusedK512VNNIForTest(enabled bool) { enableFusedK512VNNI = enabled }

// enableArgmaxK512VNNI gates the two-stage exact-pruned CTC argmax: stage 1
// runs the VNNI kernel over all columns writing approx logits to a buffer;
// stage 2 re-scores exactly only the per-row candidates within 2*err of the
// approx row max. The bound err makes the exact argmax provably a member of
// the candidate set, so the result is identical to the full F32 path.
var enableArgmaxK512VNNI = true

// SetArgmaxK512VNNIForTest toggles the two-stage CTC argmax (tests compare
// against the pure F32 path with it disabled).
func SetArgmaxK512VNNIForTest(enabled bool) { enableArgmaxK512VNNI = enabled }

// q8ArgmaxPruneErr is the safety bound for |approx - exact| CTC logits. It is
// calibrated by TestQ8K512ArgmaxPruneError (measured max ≈0.15; analytic bound
// ≈ 1024·16·sBmax ≈ 0.4 worst-case). Candidates = columns whose approx logit
// is within 2*err of the row max.
const q8ArgmaxPruneErr = 0.40

// q8ArgmaxMaxCands caps the per-row candidate count; rows exceeding it (near-
// flat logit tops) take the exact full-row fallback.
const q8ArgmaxMaxCands = 512

// q8ArgmaxFallbacks counts rows that hit the candidate cap (diagnostic).
var q8ArgmaxFallbacks int64

// Q8ArgmaxFallbackCount returns the candidate-cap fallback counter (diagnostic).
func Q8ArgmaxFallbackCount() int64 { return atomic.LoadInt64(&q8ArgmaxFallbacks) }

// Q8ArgmaxResetFallbackCount zeroes the fallback counter (diagnostic).
func Q8ArgmaxResetFallbackCount() { atomic.StoreInt64(&q8ArgmaxFallbacks, 0) }

//go:noescape
func q8uQ8sDual8LogitsVNNIK512(vals *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8rDual8LogitsVNNIK512(vals *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

var q8ArgmaxValsPool = sync.Pool{New: func() any {
	s := make([]float32, 0, 1<<14)
	return &s
}}

// q8APanel8K512 holds 8 rows of K=512 prequantized as +128-biased u8 Q8.
// Layout matches the FFN-down panel, block-major for VNNI L1 locality:
//
//	q[b*256 + r*32 + i]  — 8 rows of one block = 256 contiguous bytes
//	s[b*8 + r]           — 8 scales of one block = 32 contiguous bytes
type q8APanel8K512 struct {
	q [8 * 512]int8 // bytes = round(a*127/amax)+128 ∈ [1,255]
	s [8 * 16]float32
}

var q8APanel8K512Pool = sync.Pool{New: func() any { return new(q8APanel8K512) }}

//go:noescape
func quantizePanel8Q8SK512AVX512(q *int8, s *float32, a *float32)

func quantizePanel8Q8SK512(ap *q8APanel8K512, a []float32) {
	quantizePanel8Q8SK512AVX512(&ap.q[0], &ap.s[0], &a[0])
}

//go:noescape
func q8uQ8sDual8AccumVNNIK512N512(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8uQ8sDual8PlainVNNIK512N1536(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8uQ8sDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8uQ8sDual8ArgmaxVNNIK512(bestV *float32, bestI *int, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, n int, bn0, bn1 float32)

// q8K512Dual8Store is the per-epilogue unchecked dual-B call for one output
// pair (n, n+1) of an 8-row panel.
type q8K512Dual8Store func(out []float32, ap *q8APanel8K512, t *Q8Tensor, m, n int, bn0, bn1 float32)

const k512RowBytes = 16 * q8BlockBytes // one Q8_0 row of K=512 = 544 bytes

func q8K512AccumDual8(out []float32, ap *q8APanel8K512, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8uQ8sDual8AccumVNNIK512N512(&out[0], &ap.q[0], &ap.s[0], &t.Data[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], n*k512RowBytes, (n+1)*k512RowBytes, m, n, bn0, bn1)
}

func q8K512PlainDual8(out []float32, ap *q8APanel8K512, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8uQ8sDual8PlainVNNIK512N1536(&out[0], &ap.q[0], &ap.s[0], &t.Data[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], n*k512RowBytes, (n+1)*k512RowBytes, m, n, bn0, bn1)
}

func q8K512ReLUDual8(out []float32, ap *q8APanel8K512, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8uQ8sDual8ReLUVNNIK512N2048(&out[0], &ap.q[0], &ap.s[0], &t.Data[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], n*k512RowBytes, (n+1)*k512RowBytes, m, n, bn0, bn1)
}

// tryFusedK512VNNI handles the K=512 SenseVoice layers when AVX512-VNNI is
// available: N=1536 plain (QKV), N=512 residual accum (out-proj), N=2048
// ReLU (FFN up). Returns false for any other shape/flag combination so the
// caller falls through to the existing F32 paths unchanged.
func tryFusedK512VNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool) bool {
	if !enableFusedK512VNNI || !hasAVX512VNNI || K != 512 || M < 8 {
		return false
	}
	var kern q8K512Dual8Store
	switch {
	case relu && !accum && N == 2048:
		if !enableQ8ZK512FFNUp {
			return false
		}
		kern = q8K512ReLUDual8
	case !relu && !accum && N == 1536:
		if !enableQ8ZK512QKV {
			return false
		}
		kern = q8K512PlainDual8
	case !relu && accum && N == 512:
		if !enableQ8ZK512Out {
			return false
		}
		kern = q8K512AccumDual8
	default:
		return false
	}
	if len(a) < M*512 || len(b.Scales) < b.Rows*16 || len(out) < M*N || len(bias) < ne {
		return false
	}

	ap0 := q8APanel8K512Pool.Get().(*q8APanel8K512)
	var ap1 *q8APanel8K512
	if M >= 16 {
		ap1 = q8APanel8K512Pool.Get().(*q8APanel8K512)
	}
	var dDual0 [8]float32
	var d8 [8]float32
	m := 0
	// 16-row outer: two prequant panels share each B pair (B stays hot).
	for ; m+15 < M; m += 16 {
		a0 := a[m*512 : (m+8)*512]
		a1 := a[(m+8)*512 : (m+16)*512]
		quantizePanel8Q8SK512(ap0, a0)
		quantizePanel8Q8SK512(ap1, a1)
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := bias[n], bias[n+1]
			kern(out, ap0, b, m, n, bn0, bn1)
			kern(out, ap1, b, m+8, n, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := bias[n]
			q8MultiDot8T(&d8, a0, b, n, 16, 512)
			storeDot8(out, m, n, N, &d8, bn, relu, accum)
			q8MultiDot8T(&d8, a1, b, n, 16, 512)
			storeDot8(out, m+8, n, N, &d8, bn, relu, accum)
		}
	}
	for ; m+7 < M; m += 8 {
		aPanel := a[m*512 : (m+8)*512]
		quantizePanel8Q8SK512(ap0, aPanel)
		n := ns
		for ; n+1 < ne; n += 2 {
			kern(out, ap0, b, m, n, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			q8MultiDot8T(&d8, aPanel, b, n, 16, 512)
			storeDot8(out, m, n, N, &d8, bias[n], relu, accum)
		}
	}
	// Remainder rows < 8: existing float dual/row paths.
	for ; m+3 < M; m += 4 {
		aPanel := a[m*512 : (m+4)*512]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, 16, 512)
			storeDual4(out, m, n, N, &dDual0, bias[n], bias[n+1], relu, accum)
		}
		for ; n < ne; n++ {
			var d4 [4]float32
			q8MultiDot4T(&d4, aPanel, b, n, 16, 512)
			storeDot4(out, m, n, N, &d4, bias[n], relu, accum)
		}
	}
	for ; m < M; m++ {
		aRow := a[m*512 : m*512+512]
		n := ns
		for ; n+1 < ne; n += 2 {
			s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
			storeDot2(out, m, n, N, s0+bias[n], s1+bias[n+1], relu, accum)
		}
		for ; n < ne; n++ {
			storeDot1(out, m, n, N, DotQ8RowScaled(aRow, b, n)+bias[n], relu, accum)
		}
	}
	q8APanel8K512Pool.Put(ap0)
	if ap1 != nil {
		q8APanel8K512Pool.Put(ap1)
	}
	return true
}

func storeDot2(out []float32, m, n, N int, v0, v1 float32, relu, accum bool) {
	if relu {
		if v0 < 0 {
			v0 = 0
		}
		if v1 < 0 {
			v1 = 0
		}
	}
	if accum {
		out[m*N+n] += v0
		out[m*N+n+1] += v1
	} else {
		out[m*N+n] = v0
		out[m*N+n+1] = v1
	}
}

func storeDot1(out []float32, m, n, N int, v float32, relu, accum bool) {
	if relu && v < 0 {
		v = 0
	}
	if accum {
		out[m*N+n] += v
	} else {
		out[m*N+n] = v
	}
}

// tryArgmaxK512VNNI handles the CTC argmax head (K=512, hasScales+bias) with
// the two-stage exact-pruned path (see enableArgmaxK512VNNI).
func tryArgmaxK512VNNI(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, ns, ne int) bool {
	if !enableArgmaxK512VNNI || !hasAVX512VNNI || M < 8 {
		return false
	}
	return argmaxK512VNNI(bestV, bestI, a, b, bias, M, ns, ne)
}

// argmaxK512VNNI is the unchecked two-stage VNNI argmax (tests exercise it
// directly; production callers go through the gated tryArgmaxK512VNNI).
// Stage 1 quantizes 8-row panels and runs the VNNI logits kernel over all
// column pairs into vals; stage 2 collects per-row candidates within 2*err of
// the approx row max and re-scores them exactly. The error bound makes the
// exact argmax provably a candidate, so the result equals the full F32 scan.
func argmaxK512VNNI(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, M, ns, ne int) bool {
	if !hasAVX512VNNI || M < 8 {
		return false
	}
	if len(a) < M*512 || len(b.Scales) < b.Rows*16 || len(bestV) < M || len(bestI) < M || len(bias) < ne {
		return false
	}

	pairs := (ne - ns) / 2
	vp := q8ArgmaxValsPool.Get().(*[]float32)
	vals := *vp
	if cap(vals) < pairs*16 {
		vals = make([]float32, pairs*16)
		*vp = vals
	}
	vals = vals[:pairs*16]

	thr := float32(2 * q8ArgmaxPruneErr)
	oddCol := (ne-ns)%2 == 1
	candBuf := make([]int, 0, 16)

	rs := q8RowScaleFor(b, 16)
	ap0 := q8APanel8K512RowPool.Get().(*q8APanel8K512Row)
	var ap1 *q8APanel8K512Row
	if M >= 16 {
		ap1 = q8APanel8K512RowPool.Get().(*q8APanel8K512Row)
	}
	m := 0
	for ; m+15 < M; m += 16 {
		quantizePanel8Q8SRowK512(ap0, a[m*512:(m+8)*512])
		quantizePanel8Q8SRowK512(ap1, a[(m+8)*512:(m+16)*512])
		logitsK512Dual8Range(vals, ap0, rs, b, bias, ns, pairs)
		argmaxK512Stage2(bestV, bestI, a, b, bias, vals, m, ns, ne, pairs, thr, oddCol, candBuf)
		logitsK512Dual8Range(vals, ap1, rs, b, bias, ns, pairs)
		argmaxK512Stage2(bestV, bestI, a, b, bias, vals, m+8, ns, ne, pairs, thr, oddCol, candBuf)
	}
	for ; m+7 < M; m += 8 {
		quantizePanel8Q8SRowK512(ap0, a[m*512:(m+8)*512])
		logitsK512Dual8Range(vals, ap0, rs, b, bias, ns, pairs)
		argmaxK512Stage2(bestV, bestI, a, b, bias, vals, m, ns, ne, pairs, thr, oddCol, candBuf)
	}
	for ; m < M; m++ {
		argmaxK512RowExact(bestV, bestI, a, b, bias, m, ns, ne)
	}
	q8APanel8K512RowPool.Put(ap0)
	if ap1 != nil {
		q8APanel8K512RowPool.Put(ap1)
	}
	q8ArgmaxValsPool.Put(vp)
	return true
}

// argmaxK512Stage2 collects per-row candidates within 2*err of the approx row
// max from vals (panel-local rows 0..7) and re-scores them exactly. The error
// bound makes the exact argmax provably a candidate, so the result equals the
// full F32 scan (ascending columns, leftmost-max ties).
func argmaxK512Stage2(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias, vals []float32, m, ns, ne, pairs int, thr float32, oddCol bool, candBuf []int) {
	for lr := 0; lr < 8; lr++ {
		base := lr * 2
		vmax := float32(-math.MaxFloat32)
		for p := 0; p < pairs; p++ {
			if v := vals[p*16+base]; v > vmax {
				vmax = v
			}
			if v := vals[p*16+base+1]; v > vmax {
				vmax = v
			}
		}
		cut := vmax - thr
		candBuf = candBuf[:0]
		for p := 0; p < pairs; p++ {
			n0 := ns + 2*p
			if vals[p*16+base] >= cut {
				candBuf = append(candBuf, n0)
			}
			if vals[p*16+base+1] >= cut {
				candBuf = append(candBuf, n0+1)
			}
		}
		if oddCol {
			candBuf = append(candBuf, ne-1)
		}
		if len(candBuf) > q8ArgmaxMaxCands {
			atomic.AddInt64(&q8ArgmaxFallbacks, 1)
			argmaxK512RowExact(bestV, bestI, a, b, bias, m+lr, ns, ne)
			continue
		}
		bv, bi := bestV[m+lr], bestI[m+lr]
		aRow := a[(m+lr)*512 : (m+lr)*512+512]
		var second float32 = -math.MaxFloat32
		for _, c := range candBuf {
			v := DotQ8RowScaled(aRow, b, c) + bias[c]
			if v > bv {
				second = bv
				bv, bi = v, c
			} else if v > second {
				second = v
			}
		}
		if debugArgmaxMargin != nil {
			debugArgmaxMargin(m+lr, bv, second, len(candBuf))
		}
		bestV[m+lr], bestI[m+lr] = bv, bi
	}
}

// debugArgmaxMargin, when non-nil, receives per-row exact winner/runner-up
// logit values from the CTC prune stage 2 (temporary calibration analysis).
var debugArgmaxMargin func(row int, winner, runnerUp float32, nCand int)

// SetDebugArgmaxMarginForTest installs the margin hook (temporary calibration
// analysis; pass nil to remove).
func SetDebugArgmaxMarginForTest(fn func(row int, winner, runnerUp float32, nCand int)) {
	debugArgmaxMargin = fn
}

// logitsK512Dual8Range runs the stage-1 Q8R logits kernel over the pairs of
// [ns, ne), writing per-call 16-float blocks into vals (indexed by pair).
// The kernel consumes the REPACKED per-row-scale B (rs.q), not the strip.
func logitsK512Dual8Range(vals []float32, ap *q8APanel8K512Row, rs *q8RowScale, b *Q8Tensor, bias []float32, ns, pairs int) {
	for p := 0; p < pairs; p++ {
		n := ns + 2*p
		q8rDual8LogitsVNNIK512(&vals[p*16], &ap.q[0], &ap.s[0], &rs.q[0],
			&rs.scale[n], &rs.scale[n+1], &rs.sum128[n], &rs.sum128[n+1],
			n*k512Q8RRowBytes, (n+1)*k512Q8RRowBytes, 0, n, bias[n], bias[n+1])
	}
}

// argmaxK512RowExact computes one row's exact argmax over [ns,ne) by row dots
// (F32 fallback for panel remainders and capped candidate rows).
func argmaxK512RowExact(bestV []float32, bestI []int, a []float32, b *Q8Tensor, bias []float32, m, ns, ne int) {
	aRow := a[m*512 : m*512+512]
	bv, bi := bestV[m], bestI[m]
	for n := ns; n < ne; n++ {
		s := DotQ8RowScaled(aRow, b, n) + bias[n]
		if s > bv {
			bv, bi = s, n
		}
	}
	bestV[m], bestI[m] = bv, bi
}


