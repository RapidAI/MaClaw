//go:build amd64

package tensor

import (
	"os"
	"sync"
)

// enableGemmaM8Maddwd gates the AVX2 VPMADDWD M8/M4 fast path
// (gemmaQuantizeS16 + gemmaMaddwdRowM8/M4N{24,36}AVX2) for AVX2-only
// machines, covering seq>=3: seq>=5 tiles M8 rows (3-4-row tails use the M4
// kernel), seq 3-4 run as a single M4 tile. It mirrors the VNNI M8
// architecture with s16 activations + VPMADDWD instead of u8 + VPDPBUSD
// (see the .s files for the overflow analysis). Correctness is pinned by
// TestGemmaMaddwdM8* kernel cross-validation plus the end-to-end
// TestFusionOffVsOnCosine gate in corelib/embedding (run with
// MACLAW_EMBED_NO_VNNI=1 to exercise this path on VNNI-capable machines).
// Set MACLAW_EMBED_NO_MADDWD=1 to disable (A/B benchmarking, bisecting
// numerics issues).
var enableGemmaM8Maddwd = os.Getenv("MACLAW_EMBED_NO_MADDWD") != "1"

//go:noescape
func gemmaQuantizeS16AVX2(q *int16, s *float32, a *float32, rows, nBlocks int)

//go:noescape
func gemmaMaddwdRowM8N24AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)

//go:noescape
func gemmaMaddwdRowM8N36AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)

//go:noescape
func gemmaMaddwdRowM4N24AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)

//go:noescape
func gemmaMaddwdRowM4N36AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)

// gemmaQuantizeS16 quantizes rows×K f32 activations into s16 payloads with
// one f32 scale per row (row-major, matching the gemmaMaddwd M8 kernel
// contract). dot = aS[r]·Σ_blk bS[n][blk]·Σ_i a16[r][i]·int8(w8[n][i]).
func gemmaQuantizeS16(q []int16, s []float32, a []float32, rows, K int) {
	nBlocks := K / q8BlockSize
	if rows <= 0 || nBlocks <= 0 {
		return
	}
	if hasAVX2andFMA && len(a) >= rows*K && len(q) >= rows*K && len(s) >= rows {
		gemmaQuantizeS16AVX2(&q[0], &s[0], &a[0], rows, nBlocks)
		return
	}
	gemmaQuantizeS16Scalar(q, s, a, rows, K)
}

// gemmaAS16 is a pooled quantized-activation panel (seq rows × K s16
// payloads, one scale per row for the row-scale M8 kernels).
type gemmaAS16 struct {
	q []int16
	s []float32
}

var gemmaAS16Pool = sync.Pool{New: func() any { return new(gemmaAS16) }}

func getGemmaAS16(rows, K int) *gemmaAS16 {
	p := gemmaAS16Pool.Get().(*gemmaAS16)
	if cap(p.q) < rows*K {
		p.q = make([]int16, rows*K)
	} else {
		p.q = p.q[:rows*K]
	}
	if cap(p.s) < rows {
		p.s = make([]float32, rows)
	} else {
		p.s = p.s[:rows]
	}
	return p
}

func putGemmaAS16(p *gemmaAS16) {
	if p != nil {
		gemmaAS16Pool.Put(p)
	}
}

// gemmaMaddwdWeightOK reports whether b can use the maddwd M8 kernels:
// weights are read straight from the Q8_0 Data blocks (no Packed needed),
// block scales from the f32 Scales cache.
func gemmaMaddwdWeightOK(b *Q8Tensor, K int) bool {
	if b == nil || (K != gemmaDim && K != gemmaFFDim) {
		return false
	}
	nBlocks := K / q8BlockSize
	return len(b.Data) >= b.Rows*nBlocks*q8BlockBytes && len(b.Scales) >= b.Rows*nBlocks
}

// gemmaMaddwdM8Cols runs the row-scale maddwd M8 kernel over [ns,ne) for one
// 8-row tile, handling odd column edges with the f32 tail path.
func gemmaMaddwdM8Cols(out []float32, aQ []int16, aS []float32, a []float32, b *Q8Tensor, N, K, ns, ne int) {
	n := ns
	if n&1 != 0 {
		gemmaStoreTailCol(out, a, b, 8, N, K, n)
		n++
	}
	if n < ne {
		if K == gemmaDim {
			gemmaMaddwdRowM8N24AVX2(&out[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		} else {
			gemmaMaddwdRowM8N36AVX2(&out[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		}
	}
	if (ne-n)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 8, N, K, ne-1)
	}
}

// gemmaMaddwdM4Cols runs the row-scale maddwd M4 kernel (4 rows × column
// pairs) over [ns,ne) for one 4-row tile, handling odd column edges with the
// f32 tail path.
func gemmaMaddwdM4Cols(out []float32, aQ []int16, aS []float32, a []float32, b *Q8Tensor, N, K, ns, ne int) {
	n := ns
	if n&1 != 0 {
		gemmaStoreTailCol(out, a, b, 4, N, K, n)
		n++
	}
	if n+1 < ne {
		if K == gemmaDim {
			gemmaMaddwdRowM4N24AVX2(&out[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		} else {
			gemmaMaddwdRowM4N36AVX2(&out[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		}
	}
	if (ne-n)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 4, N, K, ne-1)
	}
}

// gemmaMaddwdM4Range covers a 3- or 4-row M tail on [ns,ne) with the M4
// kernel (the caller guarantees M ∈ {3,4}). M=4 writes out directly; M=3 runs
// the 4-row kernel against the panel's zero pad row into a pooled 4×N
// scratch (row 4 of the kernel output is the pad dot = 0 and never copied),
// then copies the 3 valid rows out. Odd column edges use the f32 tail path
// with only the 3 valid A rows.
func gemmaMaddwdM4Range(out []float32, aQ []int16, aS []float32, a []float32, b *Q8Tensor, M, N, K, ns, ne int) {
	if M == 4 {
		gemmaMaddwdM4Cols(out, aQ, aS, a, b, N, K, ns, ne)
		return
	}
	// M == 3
	tp := gemmaMaddwdTailPool.Get().(*[]float32)
	scratch := *tp
	if cap(scratch) < 4*N {
		scratch = make([]float32, 4*N)
		*tp = scratch
	} else {
		scratch = scratch[:4*N]
	}
	defer gemmaMaddwdTailPool.Put(tp)
	n := ns
	if n&1 != 0 {
		gemmaStoreTailCol(scratch, a, b, 3, N, K, n)
		n++
	}
	if n+1 < ne {
		if K == gemmaDim {
			gemmaMaddwdRowM4N24AVX2(&scratch[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		} else {
			gemmaMaddwdRowM4N36AVX2(&scratch[0], &aQ[0], &aS[0], &b.Data[0], &b.Scales[0], N, n, ne)
		}
	}
	if (ne-n)&1 != 0 {
		gemmaStoreTailCol(scratch, a, b, 3, N, K, ne-1)
	}
	for r := 0; r < 3; r++ {
		copy(out[r*N+ns:r*N+ne], scratch[r*N+ns:r*N+ne])
	}
}

// gemmaMaddwdM8Range covers M rows on [ns,ne): maddwd M8 tiles; a 3-4-row M
// tail runs the cheaper M4 kernel directly, other tails (<8 rows) run the
// same M8 kernel on a zero-padded s16 panel into a pooled 8×N scratch, then
// copy the valid rows out. The padded a16 rows are zeros (entries zero their
// panel pad region after quantizing), so padded dots are 0 and never read
// past a's valid rows. This deliberately avoids the gemmaGemmM3/M4* kernels:
// those are AVX-512-only and would fault on the AVX2-only machines this path
// exists for.
func gemmaMaddwdM8Range(out []float32, aQ []int16, aS []float32, a []float32, b *Q8Tensor, M, N, K, ns, ne int) {
	m := 0
	for ; m+7 < M; m += 8 {
		gemmaMaddwdM8Cols(out[m*N:], aQ[m*K:], aS[m:], a[m*K:], b, N, K, ns, ne)
	}
	if m >= M {
		return
	}
	rows := M - m
	if rows == 3 || rows == 4 {
		gemmaMaddwdM4Range(out[m*N:], aQ[m*K:], aS[m:], a[m*K:], b, rows, N, K, ns, ne)
		return
	}
	tp := gemmaMaddwdTailPool.Get().(*[]float32)
	scratch := *tp
	if cap(scratch) < 8*N {
		scratch = make([]float32, 8*N)
		*tp = scratch
	} else {
		scratch = scratch[:8*N]
	}
	defer gemmaMaddwdTailPool.Put(tp)
	nn := ns
	if nn&1 != 0 {
		gemmaStoreTailCol(scratch, a[m*K:], b, rows, N, K, nn)
		nn++
	}
	if nn < ne {
		if K == gemmaDim {
			gemmaMaddwdRowM8N24AVX2(&scratch[0], &aQ[m*K], &aS[m], &b.Data[0], &b.Scales[0], N, nn, ne)
		} else {
			gemmaMaddwdRowM8N36AVX2(&scratch[0], &aQ[m*K], &aS[m], &b.Data[0], &b.Scales[0], N, nn, ne)
		}
	}
	if (ne-nn)&1 != 0 {
		gemmaStoreTailCol(scratch, a[m*K:], b, rows, N, K, ne-1)
	}
	for r := 0; r < rows; r++ {
		copy(out[(m+r)*N+ns:(m+r)*N+ne], scratch[r*N+ns:r*N+ne])
	}
}

var gemmaMaddwdTailPool = sync.Pool{New: func() any { p := make([]float32, 8*gemmaFFDim); return &p }}

// gemmaMaddwdPanel quantizes seq rows of a into a pooled s16 panel padded up
// to a multiple of 8 rows (pad rows are zero → padded dots are 0), so M8
// tiles and the padded tail tile can both read a full 8-row panel.
func gemmaMaddwdPanel(a []float32, seq, K int) *gemmaAS16 {
	seqPad := (seq + 7) &^ 7
	panel := getGemmaAS16(seqPad, K)
	gemmaQuantizeS16(panel.q, panel.s, a, seq, K)
	clear(panel.q[seq*K : seqPad*K])
	clear(panel.s[seq:seqPad])
	return panel
}

// gemmaMaddwdQKV replaces MatMulQ8PackedQKV for seq>=3 on AVX2-only
// machines: quantize A once (padded to a multiple of 8 rows), then split the
// virtual column space [0, Nq+Nkv+Nkv) so workers stay balanced
// (Q [0,768), K [768,1024), V [1024,1280)). seq 3-4 run as a single M4 tile.
func gemmaMaddwdQKV(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq, maxWorkers int) bool {
	const K, Nq, Nkv = gemmaDim, gemmaDim, gemmaKVDim
	if !enableGemmaM8Maddwd || !hasAVX2andFMA || seq < 3 {
		return false
	}
	if !gemmaMaddwdWeightOK(wq, K) || !gemmaMaddwdWeightOK(wk, K) || !gemmaMaddwdWeightOK(wv, K) {
		return false
	}
	if len(a) < seq*K || len(q) < seq*Nq || len(k) < seq*Nkv || len(v) < seq*Nkv {
		return false
	}
	panel := gemmaMaddwdPanel(a, seq, K)
	defer putGemmaAS16(panel)
	rangeFn := gemmaMaddwdM8Range
	if seq < 5 {
		// seq 3-4: an 8-row tile of padding would waste half the int8
		// work; run everything as one 4-row M4 tile instead.
		rangeFn = gemmaMaddwdM4Range
	}
	total := Nq + 2*Nkv
	run := func(cs, ce int) {
		if cs < Nq {
			ne := ce
			if ne > Nq {
				ne = Nq
			}
			rangeFn(q, panel.q, panel.s, a, wq, seq, Nq, K, cs, ne)
		}
		if ce > Nq && cs < Nq+Nkv {
			s := cs - Nq
			if s < 0 {
				s = 0
			}
			e := ce - Nq
			if e > Nkv {
				e = Nkv
			}
			if s < e {
				rangeFn(k, panel.q, panel.s, a, wk, seq, Nkv, K, s, e)
			}
		}
		if ce > Nq+Nkv {
			s := cs - Nq - Nkv
			if s < 0 {
				s = 0
			}
			e := ce - Nq - Nkv
			if e > Nkv {
				e = Nkv
			}
			if s < e {
				rangeFn(v, panel.q, panel.s, a, wv, seq, Nkv, K, s, e)
			}
		}
	}
	if maxWorkers == 1 || !shouldParallel(seq, Nq, K) {
		run(0, total)
	} else {
		parallelRangesWithWorkers(total, matMulWorkersFor(seq, Nq, K), run)
	}
	return true
}

// gemmaMaddwdDualOut replaces MatMulQ8DualOut for seq>=3: one quantization
// of A feeds both FFN weight streams; GELU is applied per range. seq 3-4 run
// as a single M4 tile.
func gemmaMaddwdDualOut(gate, up, a []float32, wG, wU *Q8Tensor, seq, maxWorkers int) bool {
	const K, N = gemmaDim, gemmaFFDim
	if !enableGemmaM8Maddwd || !hasAVX2andFMA || seq < 3 {
		return false
	}
	if !gemmaMaddwdWeightOK(wG, K) || !gemmaMaddwdWeightOK(wU, K) {
		return false
	}
	if len(a) < seq*K || len(gate) < seq*N || len(up) < seq*N {
		return false
	}
	panel := gemmaMaddwdPanel(a, seq, K)
	defer putGemmaAS16(panel)
	rangeFn := gemmaMaddwdM8Range
	if seq < 5 {
		rangeFn = gemmaMaddwdM4Range
	}
	run := func(ns, ne int) {
		rangeFn(gate, panel.q, panel.s, a, wG, seq, N, K, ns, ne)
		rangeFn(up, panel.q, panel.s, a, wU, seq, N, K, ns, ne)
		for r := 0; r < seq; r++ {
			off := r * N
			GeluMul(gate[off+ns:off+ne], up[off+ns:off+ne])
		}
	}
	if maxWorkers == 1 || !shouldParallel(seq, N, K) {
		run(0, N)
	} else {
		parallelRangesWithWorkers(N, matMulWorkersFor(seq, N, K), run)
	}
	return true
}

var gemmaMaddwdYPool = sync.Pool{New: func() any { return new([]float32) }}

// gemmaMaddwdRMSResidual replaces MatMulQ8RMSResidual for seq>=3:
// y = A@B^T into a pooled full-size buffer under one N-split join, then
// RMSNorm+residual. seq 3-4 run as a single M4 tile.
func gemmaMaddwdRMSResidual(x, a []float32, b *Q8Tensor, wRMS []float32, seq, N, K, maxWorkers int, eps float32) bool {
	if !enableGemmaM8Maddwd || !hasAVX2andFMA || seq < 3 {
		return false
	}
	if !gemmaMaddwdWeightOK(b, K) {
		return false
	}
	if len(a) < seq*K || len(x) < seq*N || len(wRMS) < N {
		return false
	}
	panel := gemmaMaddwdPanel(a, seq, K)
	defer putGemmaAS16(panel)
	rangeFn := gemmaMaddwdM8Range
	if seq < 5 {
		rangeFn = gemmaMaddwdM4Range
	}
	yp := gemmaMaddwdYPool.Get().(*[]float32)
	y := *yp
	if cap(y) < seq*N {
		y = make([]float32, seq*N)
		*yp = y
	} else {
		y = y[:seq*N]
	}
	defer gemmaMaddwdYPool.Put(yp)
	run := func(ns, ne int) {
		rangeFn(y, panel.q, panel.s, a, b, seq, N, K, ns, ne)
	}
	if maxWorkers == 1 || !shouldParallel(seq, N, K) {
		run(0, N)
	} else {
		parallelRangesWithWorkers(N, matMulWorkersFor(seq, N, K), run)
	}
	for r := 0; r < seq; r++ {
		row := y[r*N : (r+1)*N]
		RMSNorm(row, row, wRMS, eps)
		Add(x[r*N:(r+1)*N], x[r*N:(r+1)*N], row)
	}
	return true
}
