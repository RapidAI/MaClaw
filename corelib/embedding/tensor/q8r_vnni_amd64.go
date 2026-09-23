//go:build amd64

package tensor

import (
	"math"
	"sync"
)

// Q8R: per-row-scale int-accumulate VNNI path for the K=512 SenseVoice
// encoder layers (QKV N=1536, out-proj N=512, FFN up N=2048).
//
// The per-block-epilogue kernels (q8_vnni_k512_amd64.go) pay cvt+scale+fma per
// (row, B, block), which caps them near the F32 multiDot rate. This path
// instead accumulates VPDPBUSD int32 across all 16 blocks per (row, B) and
// scales once per panel:
//
//	total(r,j) = sA_row[r] * sB_row[j] * (acc_int - 128*sumB[j])
//
// requiring per-row scales on both sides:
//   - B repacked from Q8_0 into contiguous s8 rows (Q + per-row Scale +
//     Sum128 = 128*sum(Q row), pre-multiplied for the +128 compensation).
//     Repack is exact-scale: sB_row = max(sB_block), q = round(s8*sB_block/sB_row).
//   - A quantized per row (amax over the full K=512 row) with +128 bias.

// q8RowScale is the repacked per-row-scale weight cache for one Q8Tensor.
type q8RowScale struct {
	q      []int8    // N×K row-major
	scale  []float32 // N: per-row amax/127
	sum128 []float32 // N: float32(128 * sum(q row)), post-hsum compensation
}

var q8RowScaleCache sync.Map // *Q8Tensor → *q8RowScale

// q8RowScaleFor returns the repacked per-row-scale cache for t, building it
// lazily once per tensor. Requires prepared f32 Scales and nBlocks = K/32.
func q8RowScaleFor(t *Q8Tensor, nBlocks int) *q8RowScale {
	if v, ok := q8RowScaleCache.Load(t); ok {
		return v.(*q8RowScale)
	}
	n, k := t.Rows, t.Cols
	rs := &q8RowScale{
		q:      make([]int8, n*k),
		scale:  make([]float32, n),
		sum128: make([]float32, n),
	}
	for row := 0; row < n; row++ {
		sb := t.Scales[row*nBlocks : (row+1)*nBlocks]
		smax := float32(0)
		for _, s := range sb {
			if s > smax {
				smax = s
			}
		}
		rs.scale[row] = smax
		rowOff := row * nBlocks * q8BlockBytes
		var sum int32
		for b := 0; b < nBlocks; b++ {
			ratio := float32(1)
			if smax > 0 {
				ratio = sb[b] / smax
			}
			base := b * q8BlockSize
			qOff := rowOff + b*q8BlockBytes + 2
			for i := 0; i < q8BlockSize; i++ {
				q := int8(math.Round(float64(int8(t.Data[qOff+i])) * float64(ratio)))
				rs.q[row*k+base+int(i)] = q
				sum += int32(q)
			}
		}
		rs.sum128[row] = float32(sum * 128)
	}
	q8RowScaleCache.Store(t, rs)
	return rs
}

// q8APanel8K512Row holds 8 rows of K=512 prequantized with per-row scales.
// q layout: block-major [16][8][32] (bytes = round(a*127/amax)+128); s: [8].
type q8APanel8K512Row struct {
	q [8 * 512]int8
	s [8]float32
}

var q8APanel8K512RowPool = sync.Pool{New: func() any { return new(q8APanel8K512Row) }}

//go:noescape
func quantizePanel8Q8SRowK512AVX512(q *int8, s *float32, a *float32)

func quantizePanel8Q8SRowK512(ap *q8APanel8K512Row, a []float32) {
	quantizePanel8Q8SRowK512AVX512(&ap.q[0], &ap.s[0], &a[0])
}

//go:noescape
func q8rDual8PlainVNNIK512N512(out *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8rDual8PlainVNNIK512N1536(out *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8rDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

// enableQ8RK512VNNI is a var so tests can toggle the Q8R path.
var enableQ8RK512VNNI = true

// SetQ8RK512ForTest toggles the Q8R int-accumulate path (temporary knob for
// precision analysis).
func SetQ8RK512ForTest(enabled bool) { enableQ8RK512VNNI = enabled }

// Per-layer Q8R gates (precision bisection); see the Q8Z gate comment.
var (
	enableQ8RK512FFNUp = true // ReLU N=2048 (mirrors the Q8Z gates)
	enableQ8RK512QKV   = true // plain N=1536
	enableQ8RK512Out   = false
)

// SetK512VNNILayerGatesForTest sets all per-layer K=512 VNNI gates (Q8Z and
// Q8R) at once for precision bisection; master switches are not affected.
func SetK512VNNILayerGatesForTest(qkv, out, ffnup bool) {
	enableQ8ZK512QKV, enableQ8RK512QKV = qkv, qkv
	enableQ8ZK512Out, enableQ8RK512Out = out, out
	enableQ8ZK512FFNUp, enableQ8RK512FFNUp = ffnup, ffnup
}

const k512Q8RRowBytes = 512 // one repacked Q8R row of K=512

func q8rPlainDual8N512(out []float32, ap *q8APanel8K512Row, rs *q8RowScale, m, n int, bn0, bn1 float32) {
	q8rDual8PlainVNNIK512N512(&out[0], &ap.q[0], &ap.s[0], &rs.q[0],
		&rs.scale[n], &rs.scale[n+1], &rs.sum128[n], &rs.sum128[n+1],
		n*k512Q8RRowBytes, (n+1)*k512Q8RRowBytes, m, n, bn0, bn1)
}

func q8rPlainDual8N1536(out []float32, ap *q8APanel8K512Row, rs *q8RowScale, m, n int, bn0, bn1 float32) {
	q8rDual8PlainVNNIK512N1536(&out[0], &ap.q[0], &ap.s[0], &rs.q[0],
		&rs.scale[n], &rs.scale[n+1], &rs.sum128[n], &rs.sum128[n+1],
		n*k512Q8RRowBytes, (n+1)*k512Q8RRowBytes, m, n, bn0, bn1)
}

func q8rReLUDual8N2048(out []float32, ap *q8APanel8K512Row, rs *q8RowScale, m, n int, bn0, bn1 float32) {
	q8rDual8ReLUVNNIK512N2048(&out[0], &ap.q[0], &ap.s[0], &rs.q[0],
		&rs.scale[n], &rs.scale[n+1], &rs.sum128[n], &rs.sum128[n+1],
		n*k512Q8RRowBytes, (n+1)*k512Q8RRowBytes, m, n, bn0, bn1)
}

type q8rDual8Store func(out []float32, ap *q8APanel8K512Row, rs *q8RowScale, m, n int, bn0, bn1 float32)

// tryFusedK512RowVNNI handles the K=512 SenseVoice encoder layers with the
// int-accumulate Q8R kernels. Returns false for other shapes/flags or when
// the repack is unavailable, so the caller falls through unchanged.
func tryFusedK512RowVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool) bool {
	if !enableQ8RK512VNNI || !enableFusedK512VNNI || !hasAVX512VNNI || K != 512 || M < 8 || accum {
		return false
	}
	var kern q8rDual8Store
	switch {
	case relu && N == 2048 && enableQ8RK512FFNUp:
		kern = q8rReLUDual8N2048
	case !relu && N == 1536 && enableQ8RK512QKV:
		kern = q8rPlainDual8N1536
	case !relu && N == 512 && enableQ8RK512Out:
		kern = q8rPlainDual8N512
	default:
		return false
	}
	if len(a) < M*512 || len(b.Scales) < b.Rows*16 || len(out) < M*N || len(bias) < ne {
		return false
	}
	rs := q8RowScaleFor(b, 16)

	ap0 := q8APanel8K512RowPool.Get().(*q8APanel8K512Row)
	var ap1 *q8APanel8K512Row
	if M >= 16 {
		ap1 = q8APanel8K512RowPool.Get().(*q8APanel8K512Row)
	}
	var dDual0 [8]float32
	var d8 [8]float32
	m := 0
	for ; m+15 < M; m += 16 {
		a0 := a[m*512 : (m+8)*512]
		a1 := a[(m+8)*512 : (m+16)*512]
		quantizePanel8Q8SRowK512(ap0, a0)
		quantizePanel8Q8SRowK512(ap1, a1)
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := bias[n], bias[n+1]
			kern(out, ap0, rs, m, n, bn0, bn1)
			kern(out, ap1, rs, m+8, n, bn0, bn1)
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
		quantizePanel8Q8SRowK512(ap0, aPanel)
		n := ns
		for ; n+1 < ne; n += 2 {
			kern(out, ap0, rs, m, n, bias[n], bias[n+1])
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
	q8APanel8K512RowPool.Put(ap0)
	if ap1 != nil {
		q8APanel8K512RowPool.Put(ap1)
	}
	return true
}
