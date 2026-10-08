//go:build amd64

package tensor

import (
	"os"
	"sync"
)

// enableGemmaM8VNNI gates the VNNI M8 medium/large-sequence fast path
// (gemmaQuantizeQ8U + gemmaVNNIM8N{24,36}PackedAVX512). Correctness is pinned
// by TestGemmaVNNIM8* kernel cross-validation plus the end-to-end
// TestFusionOffVsOnCosine gate in corelib/embedding. Set MACLAW_EMBED_NO_VNNI=1
// to force-disable (falls through to the AVX2 VPMADDWD M8 path on capable
// machines).
var enableGemmaM8VNNI = os.Getenv("MACLAW_EMBED_NO_VNNI") != "1"

//go:noescape
func gemmaVNNIRowM8N24PackedAVX512(out *float32, aQ *byte, aS *float32, packed *byte, bS *float32, N, ns, ne int)

//go:noescape
func gemmaVNNIRowM8N36PackedAVX512(out *float32, aQ *byte, aS *float32, packed *byte, bS *float32, N, ns, ne int)

//go:noescape
func gemmaQuantizeQ8UAVX512(q *byte, s *float32, a *float32, rows, nBlocks int)

//go:noescape
func gemmaQuantizeQ8URowAVX512(q *byte, s *float32, a *float32, rows, nBlocks int)

// gemmaQuantizeQ8U quantizes rows×K f32 activations into u8(+128) payloads and
// per-32-block scales (row-major, matching the gemmaVNNI M3 kernel contract).
func gemmaQuantizeQ8U(q []byte, s []float32, a []float32, rows, K int) {
	nBlocks := K / q8BlockSize
	if rows <= 0 || nBlocks <= 0 {
		return
	}
	if hasAVX512 && len(a) >= rows*K && len(q) >= rows*K && len(s) >= rows*nBlocks {
		gemmaQuantizeQ8UAVX512(&q[0], &s[0], &a[0], rows, nBlocks)
		return
	}
	gemmaQuantizeQ8UScalar(q, s, a, rows, K)
}

// gemmaQuantizeQ8URow quantizes rows×K f32 activations with one scale per row
// (s must hold rows floats), for the row-scale VNNI M8 kernels.
func gemmaQuantizeQ8URow(q []byte, s []float32, a []float32, rows, K int) {
	nBlocks := K / q8BlockSize
	if rows <= 0 || nBlocks <= 0 {
		return
	}
	if hasAVX512 && len(a) >= rows*K && len(q) >= rows*K && len(s) >= rows {
		gemmaQuantizeQ8URowAVX512(&q[0], &s[0], &a[0], rows, nBlocks)
		return
	}
	gemmaQuantizeQ8URowScalar(q, s, a, rows, K)
}

// gemmaAQ8 is a pooled quantized-activation panel (seq rows × K payloads,
// one scale per row for the row-scale M8 kernels).
type gemmaAQ8 struct {
	q []byte
	s []float32
}

var gemmaAQ8Pool = sync.Pool{New: func() any { return new(gemmaAQ8) }}

func getGemmaAQ8(rows, K int) *gemmaAQ8 {
	p := gemmaAQ8Pool.Get().(*gemmaAQ8)
	if cap(p.q) < rows*K {
		p.q = make([]byte, rows*K)
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

func putGemmaAQ8(p *gemmaAQ8) {
	if p != nil {
		gemmaAQ8Pool.Put(p)
	}
}

// gemmaVNNIWeightOK reports whether b can use the packed VNNI M8 kernels.
func gemmaVNNIWeightOK(b *Q8Tensor, K int) bool {
	if b == nil || (K != gemmaDim && K != gemmaFFDim) {
		return false
	}
	nBlocks := K / q8BlockSize
	return len(b.Packed) >= b.Rows*K && len(b.Scales) >= b.Rows*nBlocks && len(b.Data) >= b.Rows*nBlocks*q8BlockBytes
}

// gemmaVNNIM8Cols runs the row-scale VNNI M8 kernel over [ns,ne) for one
// 8-row tile, handling odd column edges with the f32 tail path.
func gemmaVNNIM8Cols(out []float32, aQ []byte, aS []float32, a []float32, b *Q8Tensor, N, K, ns, ne int) {
	n := ns
	if n&1 != 0 {
		gemmaStoreTailCol(out, a, b, 8, N, K, n)
		n++
	}
	if n+1 < ne {
		if K == gemmaDim {
			gemmaVNNIRowM8N24PackedAVX512(&out[0], &aQ[0], &aS[0], &b.Packed[0], &b.Scales[0], N, n, ne)
		} else {
			gemmaVNNIRowM8N36PackedAVX512(&out[0], &aQ[0], &aS[0], &b.Packed[0], &b.Scales[0], N, n, ne)
		}
	}
	if (ne-n)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 8, N, K, ne-1)
	}
}

// gemmaVNNIM8Range covers M rows on [ns,ne): VNNI M8 tiles plus existing f32
// kernels for the M tail (<8 rows). aS holds one scale per row.
func gemmaVNNIM8Range(out []float32, aQ []byte, aS []float32, a []float32, b *Q8Tensor, M, N, K, ns, ne int) {
	m := 0
	for ; m+7 < M; m += 8 {
		gemmaVNNIM8Cols(out[m*N:], aQ[m*K:], aS[m:], a[m*K:], b, N, K, ns, ne)
	}
	if m >= M {
		return
	}
	if K == gemmaDim {
		for ; m+3 < M; m += 4 {
			gemmaGemmM4N24(out[m*N:], a[m*K:], b, N, ns, ne)
		}
		if M-m == 3 {
			gemmaGemmM3N24(out[m*N:], a[m*K:], b, N, ns, ne)
		} else if m < M {
			gemmaGemmPad4(out[m*N:], a[m*K:], b, M-m, N, gemmaDim, ns, ne, gemmaGemmM4N24)
		}
		return
	}
	for ; m+3 < M; m += 4 {
		gemmaGemmM4N36(out[m*N:], a[m*K:], b, N, ns, ne)
	}
	if M-m == 3 {
		gemmaGemmM3N36(out[m*N:], a[m*K:], b, N, ns, ne)
	} else if m < M {
		gemmaGemmPad4(out[m*N:], a[m*K:], b, M-m, N, gemmaFFDim, ns, ne, gemmaGemmM4N36)
	}
}

// gemmaVNNIQKV replaces MatMulQ8PackedQKV for seq>=8: quantize A once, then
// split the virtual column space [0, Nq+Nkv+Nkv) so workers stay balanced
// (Q [0,768), K [768,1024), V [1024,1280)).
func gemmaVNNIQKV(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq, maxWorkers int) bool {
	const K, Nq, Nkv = gemmaDim, gemmaDim, gemmaKVDim
	if !enableGemmaM8VNNI || !hasAVX512VNNI || seq < 8 {
		return false
	}
	if !gemmaVNNIWeightOK(wq, K) || !gemmaVNNIWeightOK(wk, K) || !gemmaVNNIWeightOK(wv, K) {
		return false
	}
	if len(a) < seq*K || len(q) < seq*Nq || len(k) < seq*Nkv || len(v) < seq*Nkv {
		return false
	}
	panel := getGemmaAQ8(seq, K)
	defer putGemmaAQ8(panel)
	gemmaQuantizeQ8URow(panel.q, panel.s, a, seq, K)
	total := Nq + 2*Nkv
	run := func(cs, ce int) {
		if cs < Nq {
			ne := ce
			if ne > Nq {
				ne = Nq
			}
			gemmaVNNIM8Range(q, panel.q, panel.s, a, wq, seq, Nq, K, cs, ne)
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
				gemmaVNNIM8Range(k, panel.q, panel.s, a, wk, seq, Nkv, K, s, e)
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
				gemmaVNNIM8Range(v, panel.q, panel.s, a, wv, seq, Nkv, K, s, e)
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

// gemmaVNNIDualOut replaces MatMulQ8DualOut for seq>=8: one quantization of A
// feeds both FFN weight streams; GELU is applied per range (disjoint columns).
func gemmaVNNIDualOut(gate, up, a []float32, wG, wU *Q8Tensor, seq, maxWorkers int) bool {
	const K, N = gemmaDim, gemmaFFDim
	if !enableGemmaM8VNNI || !hasAVX512VNNI || seq < 8 {
		return false
	}
	if !gemmaVNNIWeightOK(wG, K) || !gemmaVNNIWeightOK(wU, K) {
		return false
	}
	if len(a) < seq*K || len(gate) < seq*N || len(up) < seq*N {
		return false
	}
	panel := getGemmaAQ8(seq, K)
	defer putGemmaAQ8(panel)
	gemmaQuantizeQ8URow(panel.q, panel.s, a, seq, K)
	run := func(ns, ne int) {
		gemmaVNNIM8Range(gate, panel.q, panel.s, a, wG, seq, N, K, ns, ne)
		gemmaVNNIM8Range(up, panel.q, panel.s, a, wU, seq, N, K, ns, ne)
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

var gemmaVNNIYPool = sync.Pool{New: func() any { return new([]float32) }}

// gemmaVNNIRMSResidual replaces MatMulQ8RMSResidual for seq>=8: y = A@B^T into
// a pooled full-size buffer under one N-split join, then RMSNorm+residual.
func gemmaVNNIRMSResidual(x, a []float32, b *Q8Tensor, wRMS []float32, seq, N, K, maxWorkers int, eps float32) bool {
	if !enableGemmaM8VNNI || !hasAVX512VNNI || seq < 8 {
		return false
	}
	if !gemmaVNNIWeightOK(b, K) {
		return false
	}
	if len(a) < seq*K || len(x) < seq*N || len(wRMS) < N {
		return false
	}
	panel := getGemmaAQ8(seq, K)
	defer putGemmaAQ8(panel)
	gemmaQuantizeQ8URow(panel.q, panel.s, a, seq, K)
	yp := gemmaVNNIYPool.Get().(*[]float32)
	y := *yp
	if cap(y) < seq*N {
		y = make([]float32, seq*N)
		*yp = y
	} else {
		y = y[:seq*N]
	}
	defer gemmaVNNIYPool.Put(yp)
	run := func(ns, ne int) {
		gemmaVNNIM8Range(y, panel.q, panel.s, a, b, seq, N, K, ns, ne)
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
