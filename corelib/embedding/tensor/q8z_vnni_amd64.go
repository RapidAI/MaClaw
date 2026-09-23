//go:build amd64

package tensor

import (
	"sync"
	"unsafe"
)

// Q8Z: ZMM per-block VNNI for the K=512 SenseVoice encoder layers (QKV N=1536,
// out-proj N=512, FFN up N=2048).
//
// Per-block scales on both sides (same numerics as the text-safe per-block YMM
// path), but VPDPBUSD runs on ZMM (2 blocks / 64B per instruction) and B is
// repacked scale-stripped (pure data movement — no requantization, identical
// values to Q8_0). Per 2-block tick per (row, B): pxor + vpdp + psubd + cvt +
// mulps(sA-vec) + fma(sB-vec) = 6 ops per 64 MACs, ~1.9× fewer instructions
// per MAC than the YMM per-block kernel.

const enableQ8ZK512VNNI = true

// q8zRowTail selects the 1×2 row-VNNI kernel for single-row M tails (false =
// padded dual8-into-scratch path; benchmarks A/B the two).
var q8zRowTail = true

// SetQ8ZRowTailForTest toggles the row-tail kernel (benchmarks only).
func SetQ8ZRowTailForTest(enabled bool) { q8zRowTail = enabled }

// Per-layer K=512 VNNI gates (precision bisection). Production defaults are
// chosen so the transcribed text is bit-identical to the F32 reference paths:
// see the model's near-tie CTC logit (enableFusedK512VNNI in
// q8_vnni_k512_amd64.go) — any int8 activation quantization perturbs encoder
// logits by ~1e-6..1e-3 and flips that token.
var (
	enableQ8ZK512FFNUp = true // ReLU N=2048 (safe in pair with QKV: bisected)
	enableQ8ZK512QKV   = true // plain N=1536 (safe in pair with FFN-up)
	enableQ8ZK512Out   = false // plain/accum N=512 — off: all-three-on flips the near-tie CTC token
)

// q8Stripped is the scale-stripped contiguous copy of a Q8Tensor's rows.
type q8Stripped struct {
	q []byte // N×K row-major
}

var q8StrippedCache sync.Map // *Q8Tensor → *q8Stripped

// q8StrippedBuildMu serializes cold builds so concurrent pool workers don't
// each build a full duplicate copy on first inference.
var q8StrippedBuildMu sync.Mutex

// q8StrippedFor returns the scale-stripped contiguous rows of t, building the
// copy lazily once per tensor. Values are identical to the Q8_0 blocks.
func q8StrippedFor(t *Q8Tensor, nBlocks int) *q8Stripped {
	if v, ok := q8StrippedCache.Load(t); ok {
		return v.(*q8Stripped)
	}
	q8StrippedBuildMu.Lock()
	defer q8StrippedBuildMu.Unlock()
	if v, ok := q8StrippedCache.Load(t); ok {
		return v.(*q8Stripped)
	}
	n, k := t.Rows, t.Cols
	s := &q8Stripped{q: make([]byte, n*k)}
	for row := 0; row < n; row++ {
		rowOff := row * nBlocks * q8BlockBytes
		for b := 0; b < nBlocks; b++ {
			src := rowOff + b*q8BlockBytes + 2
			dst := row*k + b*q8BlockSize
			dst32 := (*[q8BlockSize]byte)(unsafe.Pointer(&s.q[dst]))
			src32 := (*[q8BlockSize]byte)(unsafe.Pointer(&t.Data[src]))
			*dst32 = *src32
		}
	}
	q8StrippedCache.Store(t, s)
	return s
}

// PrewarmQ8Stripped builds the scale-stripped VNNI sidecar for q at model
// load, so the first inference frame does not pay the copy inline. No-op for
// shapes the VNNI path cannot use (Q8-aligned cols required) or when the
// sidecar already exists.
func PrewarmQ8Stripped(q *Q8Tensor) {
	if q == nil || q.Cols%q8BlockSize != 0 || len(q.Data) == 0 {
		return
	}
	nBlocks := q.Cols / q8BlockSize
	if len(q.Scales) < q.Rows*nBlocks {
		return
	}
	q8StrippedFor(q, nBlocks)
}

// q8APanel8K512Z holds 8 rows of K=512 prequantized as +128-biased u8 with
// per-block scales, in the ZMM interleaved layout:
//
//	q[g*512 + r*64 : +32] = block 2g of row r; q[+32:+64] = block 2g+1
//	s[b*8 + r]           — block-major scales (amax/127)
//	sA[g*128 + r*16]     — prearranged per-tick lane vec [s(2g,r)×8|s(2g+1,r)×8]
type q8APanel8K512Z struct {
	q     [8 * 512]int8
	s     [8 * 16]float32
	sAvec [8 * 8 * 16]float32
}

var q8APanel8K512ZPool = sync.Pool{New: func() any { return new(q8APanel8K512Z) }}

// q8zScratchOutPool holds the 8-row scratch output used by the zero-padded
// tail panel (the 8-row kernel always writes 8 rows; valid rows are copied).
var q8zScratchOutPool = sync.Pool{New: func() any {
	s := make([]float32, 8*2048)
	return &s
}}

//go:noescape
func quantizePanel8Q8SK512ZmmAVX512(q *int8, s *float32, sA *float32, a *float32)

func quantizePanel8Q8SK512Zmm(ap *q8APanel8K512Z, a []float32) {
	quantizePanel8Q8SK512ZmmAVX512(&ap.q[0], &ap.s[0], &ap.sAvec[0], &a[0])
}

//go:noescape
func q8zDual8PlainVNNIK512N512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8zDual8PlainVNNIK512N1536(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

//go:noescape
func q8zDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

// q8zRowDualVNNIK512 computes ONE A row × 2 B rows (adjacent n, n+1) from a
// q8APanel8K512Z panel; aQ/aS must be pre-offset to the row (aQ+r*64, aS+r).
//go:noescape
func q8zRowDualVNNIK512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1 int, bn0, bn1 float32, relu bool)

const k512ZRowBytes = 512 // one stripped Q8Z row of K=512

func q8zPlainDual8N512(out []float32, ap *q8APanel8K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8zDual8PlainVNNIK512N512(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, m, n, bn0, bn1)
}

func q8zPlainDual8N1536(out []float32, ap *q8APanel8K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8zDual8PlainVNNIK512N1536(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, m, n, bn0, bn1)
}

func q8zReLUDual8N2048(out []float32, ap *q8APanel8K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8zDual8ReLUVNNIK512N2048(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, m, n, bn0, bn1)
}

type q8zDual8Store func(out []float32, ap *q8APanel8K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1 float32)

// tryFusedK512ZmmVNNI handles the K=512 SenseVoice encoder layers with the ZMM
// per-block kernels. M-outer (16-row) × B-inner: two pooled 4.6KB A panels
// stay L1-resident across the B-pair sweep. (B-outer and 32-row panel groups
// both measured 1.7-2× slower interleaved: the per-pair panel re-read grows
// with the group's L1 footprint and cycling the 60KB panel array through L2
// per pair dwarfs the B re-reads it saves.)
// enableQ8ZK5124x4 selects the 4×4-tile kernels over the dual8 M-outer path.
// The 4×4 path now consumes one-shot prebuilt 4-row panels (prebuiltAFor,
// same hoisting as the dual8 8-row panels), which removed its original
// per-worker re-quantize penalty at multi-worker N splits. Interleaved A/B
// medians on the throttled 8745HS show parity with dual8 in the prebuilt
// config (±10% run-to-run noise, no consistent sign) and ~12-17% slower
// without prebuilt panels (4-row panels cost more per-worker quantize).
// Default OFF: no measured win to justify switching the live path.
// TestQ8Z4x4MatchesDual8 guards the numerics; SetQ8ZK5124x4ForTest toggles.
var enableQ8ZK5124x4 = false

// SetQ8ZK5124x4ForTest toggles the 4×4-tile path (interleaved benchmarks).
func SetQ8ZK5124x4ForTest(enabled bool) { enableQ8ZK5124x4 = enabled }

func tryFusedK512ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, pre *q8PrebuiltA) bool {
	var panels4 []*q8APanel4K512Z
	var panels8 []*q8APanel8K512Z
	if pre != nil {
		// Empty slices are non-nil (pooled containers are re-sliced to 0);
		// only hand non-empty panels down.
		if len(pre.k512x4) > 0 {
			panels4 = pre.k512x4
		}
		if len(pre.k512) > 0 {
			panels8 = pre.k512
		}
	}
	if enableQ8ZK5124x4 && tryFusedK512Zmm4x4(out, a, b, bias, M, N, K, ns, ne, relu, accum, panels4) {
		return true
	}
	return fusedK512ZmmVNNI(out, a, b, bias, M, N, K, ns, ne, relu, accum, panels8)
}

// q8K512ZPanelsFor quantizes every full/partial 8-row group of a [M,512] A
// into panels (the last panel is zero-padded). Caller returns the panels to
// q8APanel8K512ZPool. Used to hoist the panel quantize out of per-worker N
// ranges — each worker would otherwise re-quantize the whole A.
func q8K512ZPanelsFor(a []float32, M int) []*q8APanel8K512Z {
	n := (M + 7) / 8
	panels := make([]*q8APanel8K512Z, n)
	for i := 0; i < n; i++ {
		ap := q8APanel8K512ZPool.Get().(*q8APanel8K512Z)
		m0 := i * 8
		if m0+8 <= M {
			quantizePanel8Q8SK512Zmm(ap, a[m0*512:(m0+8)*512])
		} else {
			var aPad [8 * 512]float32
			copy(aPad[:(M-m0)*512], a[m0*512:M*512])
			clear(aPad[(M-m0)*512:])
			quantizePanel8Q8SK512Zmm(ap, aPad[:])
		}
		panels[i] = ap
	}
	return panels
}

func fusedK512ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, panels []*q8APanel8K512Z) bool {
	if !enableQ8ZK512VNNI || !enableFusedK512VNNI || !hasAVX512VNNI || K != 512 || M < 8 || accum {
		return false
	}
	var kern q8zDual8Store
	switch {
	case relu && N == 2048 && enableQ8ZK512FFNUp:
		kern = q8zReLUDual8N2048
	case !relu && N == 1536 && enableQ8ZK512QKV:
		kern = q8zPlainDual8N1536
	case !relu && N == 512 && enableQ8ZK512Out:
		kern = q8zPlainDual8N512
	default:
		return false
	}
	if len(a) < M*512 || len(b.Scales) < b.Rows*16 || len(out) < M*N || len(bias) < ne {
		return false
	}
	st := q8StrippedFor(b, 16)

	// With prebuilt panels the pooled panels serve only the (unused) tail
	// fallback below; fetch them lazily there instead of per call.
	var ap0, ap1 *q8APanel8K512Z
	panelAt := func(i int) *q8APanel8K512Z {
		if panels != nil {
			return panels[i]
		}
		if ap0 == nil {
			ap0 = q8APanel8K512ZPool.Get().(*q8APanel8K512Z)
			if M >= 16 {
				ap1 = q8APanel8K512ZPool.Get().(*q8APanel8K512Z)
			}
		}
		if i&1 == 0 || ap1 == nil {
			return ap0
		}
		return ap1
	}
	var d8 [8]float32
	m := 0
	for ; m+15 < M; m += 16 {
		a0 := a[m*512 : (m+8)*512]
		a1 := a[(m+8)*512 : (m+16)*512]
		p0, p1 := panelAt(m/8), panelAt(m/8+1)
		if panels == nil {
			quantizePanel8Q8SK512Zmm(p0, a0)
			quantizePanel8Q8SK512Zmm(p1, a1)
		}
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := bias[n], bias[n+1]
			kern(out, p0, st, b, m, n, bn0, bn1)
			kern(out, p1, st, b, m+8, n, bn0, bn1)
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
		p := panelAt(m / 8)
		if panels == nil {
			quantizePanel8Q8SK512Zmm(p, aPanel)
		}
		n := ns
		for ; n+1 < ne; n += 2 {
			kern(out, p, st, b, m, n, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			q8MultiDot8T(&d8, aPanel, b, n, 16, 512)
			storeDot8(out, m, n, N, &d8, bias[n], relu, accum)
		}
	}
	if m < M {
		// Zero-pad the tail to a full 8-row panel (prebuilt panels already
		// carry the padded tail at index M/8).
		rows := M - m
		if panels != nil {
			ap0 = panels[m/8]
		} else {
			ap0 = panelAt(0)
			var aPad [8 * 512]float32
			copy(aPad[:rows*512], a[m*512:M*512])
			clear(aPad[rows*512:])
			quantizePanel8Q8SK512Zmm(ap0, aPad[:])
		}
		n := ns
		if rows == 1 && q8zRowTail {
			// Single tail row: the 1×2 row kernel writes straight into out —
			// a full dual8 call + scratch copy would be ~4× the work.
			aQr := &ap0.q[0]
			aSr := &ap0.sAvec[0]
			for ; n+1 < ne; n += 2 {
				q8zRowDualVNNIK512(&out[m*N+n], aQr, aSr, &st.q[0],
					&b.Scales[n*16], &b.Scales[(n+1)*16],
					n*k512ZRowBytes, (n+1)*k512ZRowBytes, bias[n], bias[n+1], relu)
			}
			for ; n < ne; n++ {
				storeDot1(out, m, n, N, DotQ8RowScaled(a[m*512:m*512+512], b, n)+bias[n], relu, accum)
			}
		} else {
			// 2-7 tail rows: run the VNNI kernel into scratch (it always writes
			// 8 rows); valid rows are copied.
			sp := q8zScratchOutPool.Get().(*[]float32)
			scratch := *sp
			for ; n+1 < ne; n += 2 {
				kern(scratch, ap0, st, b, 0, n, bias[n], bias[n+1])
				for r := 0; r < rows; r++ {
					out[(m+r)*N+n] = scratch[r*N+n]
					out[(m+r)*N+n+1] = scratch[r*N+n+1]
				}
			}
			q8zScratchOutPool.Put(sp)
			for ; n < ne; n++ {
				for r := 0; r < rows; r++ {
					storeDot1(out, m+r, n, N, DotQ8RowScaled(a[(m+r)*512:(m+r)*512+512], b, n)+bias[n], relu, accum)
				}
			}
		}
	}
	if panels == nil {
		if ap0 != nil {
			q8APanel8K512ZPool.Put(ap0)
		}
		if ap1 != nil {
			q8APanel8K512ZPool.Put(ap1)
		}
	}
	return true
}

// --- K=2048 FFN down (ZMM per-block, u8 ReLU activations, no compensation) ---

// q8APanel8K2048Z holds 8 rows of K=2048 prequantized as u8 (0..127 ReLU A)
// with per-block scales, in the ZMM interleaved layout:
//
//	q[g*512 + r*64 : +32] = block 2g of row r; q[+32:+64] = block 2g+1
//	s[b*8 + r]           — block-major scales (amax/127)
type q8APanel8K2048Z struct {
	q [8 * 2048]int8
	s [8 * 64]float32
}

var q8APanel8K2048ZPool = sync.Pool{New: func() any { return new(q8APanel8K2048Z) }}

//go:noescape
func quantizePanel8Q8UZmmK2048AVX512(q *int8, s *float32, a *float32)

func quantizePanel8Q8UZmmK2048(ap *q8APanel8K2048Z, a []float32) {
	quantizePanel8Q8UZmmK2048AVX512(&ap.q[0], &ap.s[0], &a[0])
}

//go:noescape
func q8zDual8AccumVNNIK2048N512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

const k2048ZRowBytes = 2048 // one stripped Q8Z row of K=2048

func q8zAccumDual8(out []float32, ap *q8APanel8K2048Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	q8zDual8AccumVNNIK2048N512(&out[0], &ap.q[0], &ap.s[0], &st.q[0],
		&t.Scales[n*64], &t.Scales[(n+1)*64], n*k2048ZRowBytes, (n+1)*k2048ZRowBytes, m, n, bn0, bn1)
}

// tryFusedK2048ZmmVNNI: FFN down N=512 K=2048 via the ZMM per-block kernel.
// Same M-outer (16-row) structure as tryFusedAccumVNNI; activations are u8
// (ReLU) so no signed-A compensation is needed.
func tryFusedK2048ZmmVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, ns, ne int) bool {
	if !enableQ8ZK512VNNI || !enableFusedK512VNNI || !hasAVX512VNNI || M < 8 {
		return false
	}
	if len(a) < M*2048 || len(b.Scales) < b.Rows*64 || len(out) < M*512 || len(bias) < ne {
		return false
	}
	st := q8StrippedFor(b, 64)

	ap0 := q8APanel8K2048ZPool.Get().(*q8APanel8K2048Z)
	var ap1 *q8APanel8K2048Z
	if M >= 16 {
		ap1 = q8APanel8K2048ZPool.Get().(*q8APanel8K2048Z)
	}
	var d8 [8]float32
	m := 0
	for ; m+15 < M; m += 16 {
		a0 := a[m*2048 : (m+8)*2048]
		a1 := a[(m+8)*2048 : (m+16)*2048]
		quantizePanel8Q8UZmmK2048(ap0, a0)
		quantizePanel8Q8UZmmK2048(ap1, a1)
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := bias[n], bias[n+1]
			q8zAccumDual8(out, ap0, st, b, m, n, bn0, bn1)
			q8zAccumDual8(out, ap1, st, b, m+8, n, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := bias[n]
			q8MultiDot8T(&d8, a0, b, n, 64, 2048)
			storeDot8Accum(out, m, n, 512, &d8, bn)
			q8MultiDot8T(&d8, a1, b, n, 64, 2048)
			storeDot8Accum(out, m+8, n, 512, &d8, bn)
		}
	}
	for ; m+7 < M; m += 8 {
		aPanel := a[m*2048 : (m+8)*2048]
		quantizePanel8Q8UZmmK2048(ap0, aPanel)
		n := ns
		for ; n+1 < ne; n += 2 {
			q8zAccumDual8(out, ap0, st, b, m, n, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			q8MultiDot8T(&d8, aPanel, b, n, 64, 2048)
			storeDot8Accum(out, m, n, 512, &d8, bias[n])
		}
	}
	if m < M {
		// Zero-pad the tail to a full 8-row panel; the kernel accumulates into
		// scratch, so zero the 16 target cells per pair and add valid rows back.
		rows := M - m
		var aPad [8 * 2048]float32
		copy(aPad[:rows*2048], a[m*2048:M*2048])
		clear(aPad[rows*2048:])
		quantizePanel8Q8UZmmK2048(ap0, aPad[:])
		sp := q8AccumScratchPool.Get().(*[]float32)
		scratch := *sp
		n := ns
		for ; n+1 < ne; n += 2 {
			for r := 0; r < 8; r++ {
				scratch[r*512+n] = 0
				scratch[r*512+n+1] = 0
			}
			q8zAccumDual8(scratch, ap0, st, b, 0, n, bias[n], bias[n+1])
			for r := 0; r < rows; r++ {
				out[(m+r)*512+n] += scratch[r*512+n]
				out[(m+r)*512+n+1] += scratch[r*512+n+1]
			}
		}
		q8AccumScratchPool.Put(sp)
		for ; n < ne; n++ {
			for r := 0; r < rows; r++ {
				out[(m+r)*512+n] += DotQ8RowScaled(a[(m+r)*2048:(m+r)*2048+2048], b, n) + bias[n]
			}
		}
	}
	q8APanel8K2048ZPool.Put(ap0)
	if ap1 != nil {
		q8APanel8K2048ZPool.Put(ap1)
	}
	return true
}


// --- 4×4-tile kernels (4-row panels × 4-col groups) --------------------------
//
// Same per-block-scale math as the dual8 kernels (bit-identical accum lanes)
// but each call processes 4 rows × 4 cols: A loads and the per-(r,c) epilogue
// are shared across 4 columns instead of 2, halving per-MAC instruction count
// on the stall-bound encoder GEMMs.

// q8APanel4K512Z holds 4 rows of K=512 prequantized as +128-biased u8 with
// per-block scales, in the ZMM interleaved layout:
//
//	q[g*256 + r*64 : +32] = block 2g of row r; q[+32:+64] = block 2g+1
//	s[b*4 + r]           — block-major scales
//	sA[g*64 + r*16]      — prearranged per-tick lane vec [s(2g,r)×8|s(2g+1,r)×8]
type q8APanel4K512Z struct {
	q     [4 * 512]int8
	s     [16 * 4]float32
	sAvec [8 * 4 * 16]float32
}

var q8APanel4K512ZPool = sync.Pool{New: func() any { return new(q8APanel4K512Z) }}

//go:noescape
func quantizePanel4Q8SK512ZmmAVX512(q *int8, s *float32, sA *float32, a *float32)

func quantizePanel4Q8SK512Zmm(ap *q8APanel4K512Z, a []float32) {
	quantizePanel4Q8SK512ZmmAVX512(&ap.q[0], &ap.s[0], &ap.sAvec[0], &a[0])
}

// q8K512ZPanels4For quantizes every full/partial 4-row group of a [M,512] A
// into 4-row panels (the last panel is zero-padded). Caller returns the
// panels to q8APanel4K512ZPool. Used to hoist the panel quantize out of
// per-worker N ranges — each worker would otherwise re-quantize the full A.
func q8K512ZPanels4For(a []float32, M int) []*q8APanel4K512Z {
	n := (M + 3) / 4
	panels := make([]*q8APanel4K512Z, n)
	for i := 0; i < n; i++ {
		ap := q8APanel4K512ZPool.Get().(*q8APanel4K512Z)
		m0 := i * 4
		if m0+4 <= M {
			quantizePanel4Q8SK512Zmm(ap, a[m0*512:(m0+4)*512])
		} else {
			var aPad [4 * 512]float32
			copy(aPad[:(M-m0)*512], a[m0*512:M*512])
			clear(aPad[(M-m0)*512:])
			quantizePanel4Q8SK512Zmm(ap, aPad[:])
		}
		panels[i] = ap
	}
	return panels
}

//go:noescape
func q8zDual4x4VNNIK512N512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1, sB2, sB3 *float32, off0, off1, off2, off3, m, n int, bn0, bn1, bn2, bn3 float32)

//go:noescape
func q8zDual4x4VNNIK512N1536(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1, sB2, sB3 *float32, off0, off1, off2, off3, m, n int, bn0, bn1, bn2, bn3 float32)

//go:noescape
func q8zDual4x4VNNIK512N2048(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1, sB2, sB3 *float32, off0, off1, off2, off3, m, n int, bn0, bn1, bn2, bn3 float32)

type q8z4x4Store func(out []float32, ap *q8APanel4K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1, bn2, bn3 float32)

func q8z4x4N512(out []float32, ap *q8APanel4K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1, bn2, bn3 float32) {
	q8zDual4x4VNNIK512N512(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], &t.Scales[(n+2)*16], &t.Scales[(n+3)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, (n+2)*k512ZRowBytes, (n+3)*k512ZRowBytes,
		m, n, bn0, bn1, bn2, bn3)
}

func q8z4x4N1536(out []float32, ap *q8APanel4K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1, bn2, bn3 float32) {
	q8zDual4x4VNNIK512N1536(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], &t.Scales[(n+2)*16], &t.Scales[(n+3)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, (n+2)*k512ZRowBytes, (n+3)*k512ZRowBytes,
		m, n, bn0, bn1, bn2, bn3)
}

func q8z4x4N2048(out []float32, ap *q8APanel4K512Z, st *q8Stripped, t *Q8Tensor, m, n int, bn0, bn1, bn2, bn3 float32) {
	q8zDual4x4VNNIK512N2048(&out[0], &ap.q[0], &ap.sAvec[0], &st.q[0],
		&t.Scales[n*16], &t.Scales[(n+1)*16], &t.Scales[(n+2)*16], &t.Scales[(n+3)*16],
		n*k512ZRowBytes, (n+1)*k512ZRowBytes, (n+2)*k512ZRowBytes, (n+3)*k512ZRowBytes,
		m, n, bn0, bn1, bn2, bn3)
}

// tryFusedK512Zmm4x4 handles the K=512 encoder layers with the 4×4-tile
// kernels: 16-row super-tiles (4 prequantized 4-row panels) sweep 4-column
// groups; the <4 column tail takes exact per-row dots and the M%4 row tail is
// zero-padded through scratch (same numerics as the dual8 path). panels ==
// nil quantizes here (per call); prebuilt panels (one-shot quantize per GEMM,
// see prebuiltAFor) are what make the 4×4 path cheaper than dual8 at
// multi-worker N splits.
func tryFusedK512Zmm4x4(out, a []float32, b *Q8Tensor, bias []float32, M, N, K, ns, ne int, relu, accum bool, panels []*q8APanel4K512Z) bool {
	if !enableQ8ZK512VNNI || !enableFusedK512VNNI || !hasAVX512VNNI || K != 512 || M < 4 || accum {
		return false
	}
	var kern q8z4x4Store
	switch {
	case relu && N == 2048 && enableQ8ZK512FFNUp:
		kern = q8z4x4N2048
	case !relu && N == 1536 && enableQ8ZK512QKV:
		kern = q8z4x4N1536
	case !relu && N == 512 && enableQ8ZK512Out:
		kern = q8z4x4N512
	default:
		return false
	}
	if len(a) < M*512 || len(b.Scales) < b.Rows*16 || len(out) < M*N || len(bias) < ne {
		return false
	}
	st := q8StrippedFor(b, 16)

	ne4 := ns + ((ne-ns)/4)*4
	own := panels == nil
	if own {
		panels = q8K512ZPanels4For(a, M)
	}
	m := 0
	for ; m+15 < M; m += 16 {
		p := panels[m/4 : m/4+4]
		for n := ns; n < ne4; n += 4 {
			bn0, bn1, bn2, bn3 := bias[n], bias[n+1], bias[n+2], bias[n+3]
			for i := 0; i < 4; i++ {
				kern(out, p[i], st, b, m+4*i, n, bn0, bn1, bn2, bn3)
			}
		}
	}
	for ; m+3 < M; m += 4 {
		p0 := panels[m/4]
		for n := ns; n < ne4; n += 4 {
			kern(out, p0, st, b, m, n, bias[n], bias[n+1], bias[n+2], bias[n+3])
		}
	}
	if m < M {
		// The prebuilt/own tail panel at index m/4 is already zero-padded; the
		// kernel writes 4 rows to scratch and valid rows are copied (plain/relu
		// store, so no scratch pre-zeroing).
		rows := M - m
		p0 := panels[m/4]
		sp := q8zScratchOutPool.Get().(*[]float32)
		scratch := *sp
		for n := ns; n < ne4; n += 4 {
			kern(scratch, p0, st, b, 0, n, bias[n], bias[n+1], bias[n+2], bias[n+3])
			for r := 0; r < rows; r++ {
				for c := 0; c < 4; c++ {
					out[(m+r)*N+n+c] = scratch[r*N+n+c]
				}
			}
		}
		q8zScratchOutPool.Put(sp)
	}
	// Column tail [ne4, ne): ≤3 columns. Mirror the dual8 tail exactly
	// (q8MultiDot8T per 8-row group, per-row dots for the M%8 remainder) so
	// the whole output stays bit-identical to the dual8 path.
	var d8 [8]float32
	r := 0
	for ; r+7 < M; r += 8 {
		a8 := a[r*512 : (r+8)*512]
		for n := ne4; n < ne; n++ {
			q8MultiDot8T(&d8, a8, b, n, 16, 512)
			storeDot8(out, r, n, N, &d8, bias[n], relu, accum)
		}
	}
	for ; r < M; r++ {
		aRow := a[r*512 : r*512+512]
		for n := ne4; n < ne; n++ {
			storeDot1(out, r, n, N, DotQ8RowScaled(aRow, b, n)+bias[n], relu, accum)
		}
	}
	if own {
		for _, ap := range panels {
			q8APanel4K512ZPool.Put(ap)
		}
	}
	return true
}
