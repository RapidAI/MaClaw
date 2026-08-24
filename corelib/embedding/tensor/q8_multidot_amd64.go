//go:build amd64

package tensor

import (
	"sync"
	"unsafe"
)

// q8MultiDot4 fuses Q8 dequant of one B row with 4 dots against consecutive A rows.
// a is [4][K] contiguous, K = nBlocks*32. Avoids materializing the dequant buffer.
func q8MultiDot4(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 4*K {
		rowOff := row * nBlocks * q8BlockBytes
		q8MultiDot4AVX2(out, &a[0], K, &data[0], rowOff, nBlocks)
		return
	}
	q8MultiDot4Scalar(out, a, data, row, nBlocks, K)
}

// q8MultiDot4T uses t.Scales when prepared (skips f16 convert in the inner loop).
func q8MultiDot4T(out *[4]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	if nBlocks > 0 && len(a) >= 4*K && len(t.Scales) >= (row+1)*nBlocks {
		rowOff := row * nBlocks * q8BlockBytes
		if hasAVX512 {
			switch nBlocks {
			case 24:
				q8MultiDot4ScaledAVX512N24(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			case 36:
				q8MultiDot4ScaledAVX512N36(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			case 64:
				q8MultiDot4ScaledAVX512N64(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			}
		}
		if hasAVX2andFMA {
			switch nBlocks {
			case 16:
				q8MultiDot4ScaledAVX2N16(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			case 24:
				q8MultiDot4ScaledAVX2N24(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			case 36:
				q8MultiDot4ScaledAVX2N36(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			case 64:
				q8MultiDot4ScaledAVX2N64(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
				return
			}
			q8MultiDot4ScaledAVX2(out, &a[0], K, &t.Data[0], &t.Scales[row*nBlocks], rowOff, nBlocks)
			return
		}
	}
	q8MultiDot4(out, a, t.Data, row, nBlocks, K)
}

// q8MultiDot8 fuses Q8 dequant with 8 dots.
func q8MultiDot8(out *[8]float32, a []float32, data []byte, row, nBlocks, K int) {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 8*K {
		rowOff := row * nBlocks * q8BlockBytes
		q8MultiDot8AVX2(out, &a[0], K, &data[0], rowOff, nBlocks)
		return
	}
	var d0, d1 [4]float32
	q8MultiDot4(&d0, a[:4*K], data, row, nBlocks, K)
	q8MultiDot4(&d1, a[4*K:8*K], data, row, nBlocks, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func q8MultiDot8T(out *[8]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	// Two multiDot4 into out halves — B row stays hot; no intermediate [4] copy.
	if nBlocks > 0 && len(a) >= 8*K && len(t.Scales) >= (row+1)*nBlocks && (hasAVX512 || hasAVX2andFMA) {
		q8MultiDot4T((*[4]float32)(unsafe.Pointer(&out[0])), a[:4*K], t, row, nBlocks, K)
		q8MultiDot4T((*[4]float32)(unsafe.Pointer(&out[4])), a[4*K:8*K], t, row, nBlocks, K)
		return
	}
	q8MultiDot8(out, a, t.Data, row, nBlocks, K)
}

// q8DualMultiDot4 fuses dequant of two consecutive B rows with 4 A-row dots.
// out[0:4] = dots with B[row0], out[4:8] = dots with B[row1].
// Each A chunk is loaded once and FMAd into both B chains.
func q8DualMultiDot4(out *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 4*K {
		rowBytes := nBlocks * q8BlockBytes
		q8DualMultiDot4AVX2(out, &a[0], K, &data[0], row0*rowBytes, row1*rowBytes, nBlocks)
		return
	}
	q8DualMultiDot4Scalar(out, a, data, row0, row1, nBlocks, K)
}

func q8DualMultiDot4T(out *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
	need := (row1 + 1) * nBlocks
	if nBlocks > 0 && len(a) >= 4*K && len(t.Scales) >= need {
		rowBytes := nBlocks * q8BlockBytes
		s0 := &t.Scales[row0*nBlocks]
		s1 := &t.Scales[row1*nBlocks]
		d := &t.Data[0]
		off0, off1 := row0*rowBytes, row1*rowBytes
		// AVX-512 first for FFN down (profile #2 hot kernel).
		if hasAVX512 {
			switch nBlocks {
			case 24:
				q8DualMultiDot4ScaledAVX512N24(out, &a[0], d, s0, s1, off0, off1)
				return
			case 36:
				q8DualMultiDot4ScaledAVX512N36(out, &a[0], d, s0, s1, off0, off1)
				return
			case 64:
				q8DualMultiDot4ScaledAVX512N64(out, &a[0], d, s0, s1, off0, off1)
				return
			}
		}
		if hasAVX2andFMA {
			switch nBlocks {
			case 16:
				q8DualMultiDot4ScaledAVX2N16(out, &a[0], d, s0, s1, off0, off1)
				return
			case 24:
				q8DualMultiDot4ScaledAVX2N24(out, &a[0], d, s0, s1, off0, off1)
				return
			case 36:
				q8DualMultiDot4ScaledAVX2N36(out, &a[0], d, s0, s1, off0, off1)
				return
			case 64:
				q8DualMultiDot4ScaledAVX2N64(out, &a[0], d, s0, s1, off0, off1)
				return
			}
			q8DualMultiDot4ScaledAVX2(out, &a[0], K, d, s0, s1, off0, off1, nBlocks)
			return
		}
	}
	q8DualMultiDot4(out, a, t.Data, row0, row1, nBlocks, K)
}

// q8DualMultiDot2T computes two A rows against two scaled Q8 rows. The fixed
// K=512 path is used by the M=6 CTC argmax tail and walks each Q8 B block once.
// out = [a0·b0, a1·b0, a0·b1, a1·b1].
func q8DualMultiDot2T(out *[4]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
	need := (row1 + 1) * nBlocks
	if K == 512 && nBlocks == 16 && len(a) >= 2*K && len(t.Scales) >= need && hasAVX2andFMA {
		rowBytes := nBlocks * q8BlockBytes
		q8DualMultiDot2ScaledAVX2K512(out, &a[0], &t.Data[0],
			&t.Scales[row0*nBlocks], &t.Scales[row1*nBlocks], row0*rowBytes, row1*rowBytes)
		return
	}
	s0, s1 := DotQ8RowDualScaled(a[:K], t, row0, row1)
	t0, t1 := DotQ8RowDualScaled(a[K:2*K], t, row0, row1)
	out[0], out[1], out[2], out[3] = s0, t0, s1, t1
}

// q8TripleMultiDot4T: 4 A × 3 B with f32 scales.
// out[0:4]=B0, out[4:8]=B1, out[8:12]=B2 (same layout as multiDot4TripleB / storeTriple4*).
func q8TripleMultiDot4T(out *[12]float32, a []float32, t *Q8Tensor, row0, row1, row2, nBlocks, K int) {
	need := (row2 + 1) * nBlocks
	if hasAVX2andFMA && nBlocks == 64 && len(a) >= 4*K && len(t.Scales) >= need {
		rowBytes := nBlocks * q8BlockBytes
		q8TripleMultiDot4ScaledAVX2N64(out, &a[0], &t.Data[0],
			&t.Scales[row0*nBlocks], &t.Scales[row1*nBlocks], &t.Scales[row2*nBlocks],
			row0*rowBytes, row1*rowBytes, row2*rowBytes)
		return
	}
	// Fallback: dual + single (A still warm for the single).
	var d8 [8]float32
	var d4 [4]float32
	q8DualMultiDot4T(&d8, a, t, row0, row1, nBlocks, K)
	q8MultiDot4T(&d4, a, t, row2, nBlocks, K)
	out[0], out[1], out[2], out[3] = d8[0], d8[1], d8[2], d8[3]
	out[4], out[5], out[6], out[7] = d8[4], d8[5], d8[6], d8[7]
	out[8], out[9], out[10], out[11] = d4[0], d4[1], d4[2], d4[3]
}

// q8TripleMultiDot8T: 8 A × 3 B via two triple-4 kernels (B stays hot).
func q8TripleMultiDot8T(out0, out1 *[12]float32, a []float32, t *Q8Tensor, row0, row1, row2, nBlocks, K int) {
	q8TripleMultiDot4T(out0, a[:4*K], t, row0, row1, row2, nBlocks, K)
	q8TripleMultiDot4T(out1, a[4*K:8*K], t, row0, row1, row2, nBlocks, K)
}

// q8DualMultiDot8: 8 A rows × 2 B via two dual-4 kernels.
// dual-4×2 keeps all accums in YMM (no stack spill); measured faster than
// one-pass 8-row with B1 accums on stack for K=2048.
func q8DualMultiDot8(out0, out1 *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	q8DualMultiDot4(out0, a[:4*K], data, row0, row1, nBlocks, K)
	q8DualMultiDot4(out1, a[4*K:8*K], data, row0, row1, nBlocks, K)
}

// q8DualMultiDot8T: fixed-geometry dual-4×2 with shared scale/data pointers so the
// second half reuses hot Q8 B without re-dispatch bookkeeping.
func q8DualMultiDot8T(out0, out1 *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
	need := (row1 + 1) * nBlocks
	if nBlocks > 0 && len(a) >= 8*K && len(t.Scales) >= need && (hasAVX512 || hasAVX2andFMA) {
		rowBytes := nBlocks * q8BlockBytes
		s0 := &t.Scales[row0*nBlocks]
		s1 := &t.Scales[row1*nBlocks]
		d := &t.Data[0]
		off0 := row0 * rowBytes
		off1 := row1 * rowBytes
		if hasAVX512 {
			switch nBlocks {
			case 24: // K=768 — 8A×2B one pass, B dequant once
				q8DualMultiDot8ScaledAVX512N24(out0, out1, &a[0], d, s0, s1, off0, off1)
				return
			case 36: // K=1152
				q8DualMultiDot8ScaledAVX512N36(out0, out1, &a[0], d, s0, s1, off0, off1)
				return
			case 64: // 8 A × 2 B one pass (scales in Z30/Z31; accums Z0-15 dual4 layout).
				q8DualMultiDot8ScaledAVX512N64(out0, out1, &a[0], d, s0, s1, off0, off1)
				return
			}
		}
		if hasAVX2andFMA {
			switch nBlocks {
			case 64: // K=2048 FFN down — B stays hot across both dual-4 halves
				q8DualMultiDot4ScaledAVX2N64(out0, &a[0], d, s0, s1, off0, off1)
				q8DualMultiDot4ScaledAVX2N64(out1, &a[4*K], d, s0, s1, off0, off1)
				return
			case 36: // K=1152
				q8DualMultiDot4ScaledAVX2N36(out0, &a[0], d, s0, s1, off0, off1)
				q8DualMultiDot4ScaledAVX2N36(out1, &a[4*K], d, s0, s1, off0, off1)
				return
			case 24: // K=768
				q8DualMultiDot4ScaledAVX2N24(out0, &a[0], d, s0, s1, off0, off1)
				q8DualMultiDot4ScaledAVX2N24(out1, &a[4*K], d, s0, s1, off0, off1)
				return
			case 16: // K=512
				q8DualMultiDot4ScaledAVX2N16(out0, &a[0], d, s0, s1, off0, off1)
				q8DualMultiDot4ScaledAVX2N16(out1, &a[4*K], d, s0, s1, off0, off1)
				return
			}
			q8DualMultiDot4ScaledAVX2(out0, &a[0], K, d, s0, s1, off0, off1, nBlocks)
			q8DualMultiDot4ScaledAVX2(out1, &a[4*K], K, d, s0, s1, off0, off1, nBlocks)
			return
		}
	}
	q8DualMultiDot4T(out0, a[:4*K], t, row0, row1, nBlocks, K)
	q8DualMultiDot4T(out1, a[4*K:8*K], t, row0, row1, nBlocks, K)
}

//go:noescape
func q8DualOut4ScaledAVX512N24(outG, outU *[8]float32, a *float32, dataG, dataU *byte, sG0, sG1, sU0, sU1 *float32, offG0, offG1, offU0, offU1 int)

func dualOutVNNIM4(gate, up, a []float32, wG, wU *Q8Tensor, N, ns, ne, nBlocks int) bool {
	return false
}

func q8DualOut4T(outG, outU *[8]float32, a []float32, wG, wU *Q8Tensor, row0, row1, nBlocks, K int) bool {
	if !hasAVX512 || nBlocks != 24 || wG == nil || wU == nil {
		return false
	}
	need := (row1 + 1) * nBlocks
	if len(a) < 4*K || len(wG.Scales) < need || len(wU.Scales) < need {
		return false
	}
	rowBytes := nBlocks * q8BlockBytes
	q8DualOut4ScaledAVX512N24(outG, outU, &a[0], &wG.Data[0], &wU.Data[0],
		&wG.Scales[row0*nBlocks], &wG.Scales[row1*nBlocks],
		&wU.Scales[row0*nBlocks], &wU.Scales[row1*nBlocks],
		row0*rowBytes, row1*rowBytes, row0*rowBytes, row1*rowBytes)
	return true
}

// tryGemmaFusedPlain is the serial full-N GEMM for EmbeddingGemma N24/N36.
// Embed() N-split (ns!=0) stays on DualDot2 / DualMultiDot4.
func tryGemmaFusedPlain(out, a []float32, b *Q8Tensor, M, N, K, ns, ne int) bool {
	if !hasAVX512 || M < 1 || ne-ns < 2 || b == nil {
		return false
	}
	if (ne-ns)&1 != 0 && M != 3 && M != 4 && M < 8 {
		return false
	}
	// M=3/4 short Embed: Dual3/Dual4 on N-split workers (not DualDot2).
	if M < 8 && (ns != 0 || ne != N) && M != 3 && M != 4 {
		return false
	}
	nBlocks := K / q8BlockSize
	if nBlocks != 24 && nBlocks != 36 {
		return false
	}
	if K != nBlocks*q8BlockSize || len(b.Scales) < b.Rows*nBlocks || len(a) < M*K || len(out) < M*N {
		return false
	}
	m := 0
	if nBlocks == 24 {
		for ; m+7 < M; m += 8 {
			gemmaGemmM8N24(out[m*N:], a[m*K:], b, N, ns, ne)
		}
		for ; m+3 < M; m += 4 {
			gemmaGemmM4N24(out[m*N:], a[m*K:], b, N, ns, ne)
		}
		if M-m == 3 {
			gemmaGemmM3N24(out[m*N:], a[m*K:], b, N, ns, ne)
		} else if m < M {
			gemmaGemmPad4(out[m*N:], a[m*K:], b, M-m, N, 768, ns, ne, gemmaGemmM4N24)
		}
	} else {
		for ; m+7 < M; m += 8 {
			gemmaGemmM8N36(out[m*N:], a[m*K:], b, N, ns, ne)
		}
		for ; m+3 < M; m += 4 {
			gemmaGemmM4N36(out[m*N:], a[m*K:], b, N, ns, ne)
		}
		if M-m == 3 {
			gemmaGemmM3N36(out[m*N:], a[m*K:], b, N, ns, ne)
		} else if m < M {
			gemmaGemmPad4(out[m*N:], a[m*K:], b, M-m, N, 1152, ns, ne, gemmaGemmM4N36)
		}
	}
	return true
}

var gemmaPadAPool = sync.Pool{New: func() any { p := make([]float32, 4*1152); return &p }}
var gemmaPadOPool = sync.Pool{New: func() any { p := make([]float32, 4*1152); return &p }}

func gemmaGemmPad4(out, a []float32, b *Q8Tensor, rows, N, K, ns, ne int, gemm func(out, a []float32, b *Q8Tensor, N, ns, ne int)) {
	if rows <= 0 {
		return
	}
	ap := gemmaPadAPool.Get().(*[]float32)
	op := gemmaPadOPool.Get().(*[]float32)
	aPad, oPad := *ap, *op
	if cap(aPad) < 4*K {
		aPad = make([]float32, 4*K)
		*ap = aPad
	} else {
		aPad = aPad[:4*K]
	}
	if cap(oPad) < 4*N {
		oPad = make([]float32, 4*N)
		*op = oPad
	} else {
		oPad = oPad[:4*N]
	}
	clear(aPad)
	copy(aPad, a[:rows*K])
	gemm(oPad, aPad, b, N, ns, ne)
	for r := 0; r < rows; r++ {
		copy(out[r*N+ns:r*N+ne], oPad[r*N+ns:r*N+ne])
	}
	gemmaPadAPool.Put(ap)
	gemmaPadOPool.Put(op)
}

func gemmaStoreM4(out []float32, n, stride int, d *[8]float32) {
	out[n] = d[0]
	out[n+1] = d[4]
	out[stride+n] = d[1]
	out[stride+n+1] = d[5]
	out[2*stride+n] = d[2]
	out[2*stride+n+1] = d[6]
	out[3*stride+n] = d[3]
	out[3*stride+n+1] = d[7]
}

func gemmaStoreM3(out []float32, n, stride int, d *[8]float32) {
	out[n] = d[0]
	out[n+1] = d[4]
	out[stride+n] = d[1]
	out[stride+n+1] = d[5]
	out[2*stride+n] = d[2]
	out[2*stride+n+1] = d[6]
}

func q8QuadMultiDot3(out *[12]float32, a []float32, t *Q8Tensor, row0, nBlocks, K int) bool {
	return false
}

func q8PackedOrData(b *Q8Tensor, nBlocks int) (data *byte, rowBytes int, packed bool) {
	rowPacked := nBlocks * q8BlockSize
	if b != nil && nBlocks > 0 && len(b.Packed) >= b.Rows*rowPacked {
		return &b.Packed[0], rowPacked, true
	}
	return &b.Data[0], nBlocks * q8BlockBytes, false
}

// enableGemmaM3VNNI gates the VNNI M3 short-sequence fast path
// (quantizeGemmaM3Q8U + gemmaVNNIM3N24PackedAVX512). It was disabled on
// 2026-08-24 because the kernel dropped every odd 32-byte block — an
// odd-block scale scratch register (X27) collided with the extracted
// high-half dot (Y27) — corrupting 3-token embeddings ("你好"
// fused-vs-reference cosine 0.26). The assembly now uses X25 for the
// odd-block scale; correctness is pinned by
// TestGemmaVNNIM3N24PackedMatchesScalar / TestGemmaVNNIM3N24PackedPerBlock
// plus the end-to-end TestFusionOffVsOnCosine.
const enableGemmaM3VNNI = true

// packedQKVGemmaShort: one N-split over Q's 768 cols; Dual3 workers covering
// [0,256) also write K/V so those GEMMs are not serial after Q's join.
func packedQKVGemmaShort(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq int) bool {
	const K, Nq, Nkv = 768, 768, 256
	if seq != 3 || !hasAVX512 || wq == nil || wk == nil || wv == nil {
		return false
	}
	if len(a) < 3*K || len(q) < 3*Nq || len(k) < 3*Nkv || len(v) < 3*Nkv {
		return false
	}
	var aq *gemmaM3AQ
	useVNNI := enableGemmaM3VNNI && hasAVX512VNNI &&
		len(wq.Packed) >= Nq*768 && len(wk.Packed) >= Nkv*768 && len(wv.Packed) >= Nkv*768
	if useVNNI {
		aq = gemmaM3AQPool.Get().(*gemmaM3AQ)
		quantizeGemmaM3Q8U(aq.q[:3*768], aq.s[:72], a, 768)
	}
	run := func(ns, ne int) {
		if aq != nil {
			gemmaVNNIPackedM3N24(q, aq.q[:3*768], aq.s[:72], a, wq, Nq, ns, ne)
			if ns < Nkv {
				ke := ne
				if ke > Nkv {
					ke = Nkv
				}
				if ns < ke {
					gemmaVNNIPackedM3N24(k, aq.q[:3*768], aq.s[:72], a, wk, Nkv, ns, ke)
					gemmaVNNIPackedM3N24(v, aq.q[:3*768], aq.s[:72], a, wv, Nkv, ns, ke)
				}
			}
		} else if len(wq.Packed) >= ne*768 && len(wk.Packed) >= Nkv*768 && len(wv.Packed) >= Nkv*768 {
			gemmaPackedM3N24(q, a, wq, Nq, ns, ne)
			if ns < Nkv {
				ke := ne
				if ke > Nkv {
					ke = Nkv
				}
				if ns < ke {
					gemmaPackedM3N24(k, a, wk, Nkv, ns, ke)
					gemmaPackedM3N24(v, a, wv, Nkv, ns, ke)
				}
			}
		} else {
			gemmaGemmM3N24(q, a, wq, Nq, ns, ne)
			if ns < Nkv {
				ke := ne
				if ke > Nkv {
					ke = Nkv
				}
				if ns < ke {
					gemmaGemmM3N24(k, a, wk, Nkv, ns, ke)
					gemmaGemmM3N24(v, a, wv, Nkv, ns, ke)
				}
			}
		}
		vzeroupperASM()
	}
	if !shouldParallel(seq, Nq, K) {
		run(0, Nq)
	} else {
		parallelRangesWithWorkers(Nq, matMulWorkersFor(seq, Nq, K), run)
	}
	if aq != nil {
		gemmaM3AQPool.Put(aq)
	}
	return true
}

// packedDualOutGemmaShort: Dual3 packed n-loop N-split + per-range SiLU
// (no extra full-array SiLUMul join after the GEMM wait).
func rmsResidualGemmaShort(x, a, y []float32, b *Q8Tensor, wRMS []float32, seq, N, K int, eps float32) bool {
	if seq != 3 || !hasAVX512 || b == nil || N != gemmaDim {
		return false
	}
	if len(x) < 3*N || len(a) < 3*K || len(y) < 3*N || len(wRMS) < N {
		return false
	}
	if K != 768 && K != 1152 {
		return false
	}
	var aq *gemmaM3AQ
	if enableGemmaM3VNNI && hasAVX512VNNI && K == 768 && len(b.Packed) >= N*768 {
		aq = gemmaM3AQPool.Get().(*gemmaM3AQ)
		quantizeGemmaM3Q8U(aq.q[:3*768], aq.s[:72], a, 768)
	}
	run := func(ns, ne int) {
		if aq != nil {
			gemmaVNNIPackedM3N24(y, aq.q[:3*768], aq.s[:72], a, b, N, ns, ne)
		} else if K == 768 {
			gemmaGemmM3N24(y, a, b, N, ns, ne)
		} else {
			gemmaGemmM3N36(y, a, b, N, ns, ne)
		}
	}
	if !shouldParallel(seq, N, K) {
		run(0, N)
	} else {
		parallelRangesWithWorkers(N, matMulWorkersFor(seq, N, K), run)
	}
	if aq != nil {
		gemmaM3AQPool.Put(aq)
	}
	for r := 0; r < 3; r++ {
		row := y[r*N : (r+1)*N]
		RMSNorm(row, row, wRMS, eps)
		Add(x[r*N:(r+1)*N], x[r*N:(r+1)*N], row)
	}
	return true
}

//go:noescape
func gemmaDualOutM3N24FusedPackedAVX512(gate, up, a *float32, packedG, packedU *byte, scalesG, scalesU *float32, N, ns, ne int)

func packedDualOutGemmaShort(gate, up, a []float32, wG, wU *Q8Tensor, seq int) bool {
	const K, N = 768, 1152
	if seq != 3 || !hasAVX512 || wG == nil || wU == nil {
		return false
	}
	if len(a) < 3*K || len(gate) < 3*N || len(up) < 3*N {
		return false
	}
	if len(wG.Scales) < N*24 || len(wU.Scales) < N*24 {
		return false
	}
	var aq *gemmaM3AQ
	useVNNI := enableGemmaM3VNNI && hasAVX512VNNI && len(wG.Packed) >= N*768 && len(wU.Packed) >= N*768
	if useVNNI {
		aq = gemmaM3AQPool.Get().(*gemmaM3AQ)
		quantizeGemmaM3Q8U(aq.q[:3*768], aq.s[:72], a, 768)
	}
	run := func(ns, ne int) {
		if aq != nil && ne > ns {
			gemmaVNNIPackedM3N24(gate, aq.q[:3*768], aq.s[:72], a, wG, N, ns, ne)
			gemmaVNNIPackedM3N24(up, aq.q[:3*768], aq.s[:72], a, wU, N, ns, ne)
		} else if ne > ns+1 && len(wG.Packed) >= ne*768 && len(wU.Packed) >= ne*768 {
			gemmaDualOutM3N24FusedPackedAVX512(&gate[0], &up[0], &a[0],
				&wG.Packed[0], &wU.Packed[0], &wG.Scales[0], &wU.Scales[0], N, ns, ne)
			if (ne-ns)&1 != 0 {
				gemmaStoreTailCol(gate, a, wG, 3, N, 768, ne-1)
				gemmaStoreTailCol(up, a, wU, 3, N, 768, ne-1)
			}
		} else if !gemmaDualOutM3N24(gate, up, a, wG, wU, N, ns, ne) {
			matMulQ8DualOutRange(gate, up, a, wG, wU, 3, N, K, ns, ne, 24)
		}
		for r := 0; r < 3; r++ {
			off := r*N + ns
			SiLUMul(gate[off:r*N+ne], up[off:r*N+ne])
		}
	}
	if !shouldParallel(seq, N, K) {
		run(0, N)
		return true
	}
	parallelRanges(N, run)
	return true
}

func gemmaDualOutM3N24(gate, up, a []float32, wG, wU *Q8Tensor, N, ns, ne int) bool {
	if !hasAVX512 || wG == nil || wU == nil || ne <= ns+1 {
		return false
	}
	if len(a) < 3*768 || len(wG.Scales) < ne*24 || len(wU.Scales) < ne*24 {
		return false
	}
	if len(gate) < 3*N || len(up) < 3*N {
		return false
	}
	gemmaGemmM3N24(gate, a, wG, N, ns, ne)
	gemmaGemmM3N24(up, a, wU, N, ns, ne)
	return true
}

type gemmaM3AQ struct {
	q [3 * 1152]byte
	s [3 * 36]float32
}

var gemmaM3AQPool = sync.Pool{New: func() any { return new(gemmaM3AQ) }}

func quantizeGemmaM3Q8U(q []byte, s []float32, a []float32, K int) {
	nBlocks := K / q8BlockSize
	for r := 0; r < 3; r++ {
		row := a[r*K : (r+1)*K]
		qs := q[r*K : (r+1)*K]
		ss := s[r*nBlocks : (r+1)*nBlocks]
		for b := 0; b < nBlocks; b++ {
			blk := row[b*32 : (b+1)*32]
			amax := float32(0)
			for i := 0; i < 32; i++ {
				v := blk[i]
				if v < 0 {
					v = -v
				}
				if v > amax {
					amax = v
				}
			}
			if amax <= 1e-8 {
				ss[b] = 0
				for i := 0; i < 32; i++ {
					qs[b*32+i] = 128
				}
				continue
			}
			ss[b] = amax / 127
			inv := 127 / amax
			dst := qs[b*32 : (b+1)*32]
			for i := 0; i < 32; i++ {
				x := blk[i] * inv
				qi := int(x + 0.5)
				if x < 0 {
					qi = int(x - 0.5)
				}
				if qi > 127 {
					qi = 127
				}
				if qi < -127 {
					qi = -127
				}
				dst[i] = byte(qi + 128)
			}
		}
	}
}

func gemmaVNNIPackedM3N24(out []float32, aQ []byte, aS []float32, a []float32, b *Q8Tensor, N, ns, ne int) {
	n := ns
	if n&1 != 0 {
		gemmaStoreTailCol(out, a, b, 3, N, 768, n)
		n++
	}
	if n+1 < ne {
		gemmaVNNIM3N24PackedAVX512(&out[0], &aQ[0], &aS[0], &b.Packed[0], &b.Scales[0], N, n, ne)
	}
	if (ne-n)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 3, N, 768, ne-1)
	}
}

func gemmaPackedM3N24(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	gemmaGemmM3N24PackedAVX512(&out[0], &a[0], &b.Packed[0], &b.Scales[0], N, ns, ne)
	if (ne-ns)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 3, N, 768, ne-1)
	}
}

//go:noescape
func gemmaVNNIM3N24PackedAVX512(out *float32, aQ *byte, aS *float32, packed *byte, bS *float32, N, ns, ne int)

func gemmaPackedM3N36(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	gemmaGemmM3N36PackedAVX512(&out[0], &a[0], &b.Packed[0], &b.Scales[0], N, ns, ne)
	if (ne-ns)&1 != 0 {
		gemmaStoreTailCol(out, a, b, 3, N, 1152, ne-1)
	}
}

func gemmaGemmM3N24(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	if ne > ns && len(out) >= 3*N && len(a) >= 3*768 && len(b.Scales) >= ne*24 {
		if len(b.Packed) >= ne*768 {
			gemmaPackedM3N24(out, a, b, N, ns, ne)
		} else if ne > ns+1 {
			gemmaGemmM3N24AVX512(&out[0], &a[0], &b.Data[0], &b.Scales[0], N, ns, ne)
			if (ne-ns)&1 != 0 {
				gemmaStoreTailCol(out, a, b, 3, N, 768, ne-1)
			}
		} else {
			gemmaStoreTailCol(out, a, b, 3, N, 768, ns)
		}
		vzeroupperASM()
		return
	}
	const nBlocks, rowBytes = 24, 24 * q8BlockBytes
	d := &b.Data[0]
	var acc [8]float32
	n := ns
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot3ScaledAVX512N24(&acc, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM3(out, n, N, &acc)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 3, N, 768, n)
	}
	vzeroupperASM()
}

//go:noescape
func gemmaGemmM3N24AVX512(out, a *float32, data *byte, scales *float32, N, ns, ne int)

//go:noescape
func gemmaGemmM3N36AVX512(out, a *float32, data *byte, scales *float32, N, ns, ne int)

//go:noescape
func gemmaGemmM3N24PackedAVX512(out, a *float32, packed *byte, scales *float32, N, ns, ne int)

//go:noescape
func gemmaGemmM3N36PackedAVX512(out, a *float32, packed *byte, scales *float32, N, ns, ne int)

func gemmaGemmM3N36(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	if ne > ns && len(out) >= 3*N && len(a) >= 3*1152 && len(b.Scales) >= ne*36 {
		if len(b.Packed) >= ne*1152 {
			gemmaPackedM3N36(out, a, b, N, ns, ne)
		} else if ne > ns+1 {
			gemmaGemmM3N36AVX512(&out[0], &a[0], &b.Data[0], &b.Scales[0], N, ns, ne)
			if (ne-ns)&1 != 0 {
				gemmaStoreTailCol(out, a, b, 3, N, 1152, ne-1)
			}
		} else {
			gemmaStoreTailCol(out, a, b, 3, N, 1152, ns)
		}
		vzeroupperASM()
		return
	}
	const nBlocks, rowBytes = 36, 36 * q8BlockBytes
	d := &b.Data[0]
	var acc [8]float32
	n := ns
	for ; n+3 < ne; n += 4 {
		q8DualMultiDot3ScaledAVX512N36(&acc, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM3(out, n, N, &acc)
		q8DualMultiDot3ScaledAVX512N36(&acc, &a[0], d,
			&b.Scales[(n+2)*nBlocks], &b.Scales[(n+3)*nBlocks],
			(n+2)*rowBytes, (n+3)*rowBytes)
		gemmaStoreM3(out, n+2, N, &acc)
	}
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot3ScaledAVX512N36(&acc, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM3(out, n, N, &acc)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 3, N, 1152, n)
	}
	vzeroupperASM()
}

func gemmaGemmM4N24(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	const nBlocks, rowBytes = 24, 24 * q8BlockBytes
	d := &b.Data[0]
	var acc [8]float32
	n := ns
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot4ScaledAVX512N24(&acc, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM4(out, n, N, &acc)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 4, N, 768, n)
	}
}

func dualOutGemmDual8(gate, up, a []float32, wG, wU *Q8Tensor, M, N, K, ns, ne, nBlocks int) int {
	if !hasAVX512 || M < 8 || wG == nil || wU == nil {
		return 0
	}
	if nBlocks != 24 && nBlocks != 36 {
		return 0
	}
	rowBytes := nBlocks * q8BlockBytes
	dG, dU := &wG.Data[0], &wU.Data[0]
	var d0, d1 [8]float32
	m := 0
	for ; m+15 < M; m += 16 {
		for n := ns; n+1 < ne; n += 2 {
			dualOutDual8Pair(&d0, &d1, gate, up, a, wG, wU, dG, dU, m, n, N, K, nBlocks, rowBytes)
			dualOutDual8Pair(&d0, &d1, gate, up, a, wG, wU, dG, dU, m+8, n, N, K, nBlocks, rowBytes)
		}
	}
	for ; m+7 < M; m += 8 {
		for n := ns; n+1 < ne; n += 2 {
			dualOutDual8Pair(&d0, &d1, gate, up, a, wG, wU, dG, dU, m, n, N, K, nBlocks, rowBytes)
		}
	}
	if m > 0 && (ne-ns)&1 != 0 {
		n := ne - 1
		for r := 0; r < m; r += 8 {
			gemmaStoreTailCol(gate[r*N:], a[r*K:], wG, 8, N, K, n)
			gemmaStoreTailCol(up[r*N:], a[r*K:], wU, 8, N, K, n)
		}
	}
	return m
}

func dualOutDual8Pair(d0, d1 *[8]float32, gate, up, a []float32, wG, wU *Q8Tensor, dG, dU *byte, m, n, N, K, nBlocks, rowBytes int) {
	off0, off1 := n*rowBytes, (n+1)*rowBytes
	ap := &a[m*K]
	gBase := gate[m*N:]
	uBase := up[m*N:]
	if nBlocks == 24 {
		q8DualMultiDot8ScaledAVX512N24(d0, d1, ap, dG, &wG.Scales[n*nBlocks], &wG.Scales[(n+1)*nBlocks], off0, off1)
		gemmaStoreM4(gBase, n, N, d0)
		gemmaStoreM4(gBase[4*N:], n, N, d1)
		q8DualMultiDot8ScaledAVX512N24(d0, d1, ap, dU, &wU.Scales[n*nBlocks], &wU.Scales[(n+1)*nBlocks], off0, off1)
		gemmaStoreM4(uBase, n, N, d0)
		gemmaStoreM4(uBase[4*N:], n, N, d1)
		return
	}
	q8DualMultiDot8ScaledAVX512N36(d0, d1, ap, dG, &wG.Scales[n*nBlocks], &wG.Scales[(n+1)*nBlocks], off0, off1)
	gemmaStoreM4(gBase, n, N, d0)
	gemmaStoreM4(gBase[4*N:], n, N, d1)
	q8DualMultiDot8ScaledAVX512N36(d0, d1, ap, dU, &wU.Scales[n*nBlocks], &wU.Scales[(n+1)*nBlocks], off0, off1)
	gemmaStoreM4(uBase, n, N, d0)
	gemmaStoreM4(uBase[4*N:], n, N, d1)
}

func q8DualMultiDot3T(out *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) bool {
	need := (row1 + 1) * nBlocks
	if !hasAVX512 || nBlocks <= 0 || len(a) < 3*K || t == nil || len(t.Scales) < need {
		return false
	}
	rowBytes := nBlocks * q8BlockBytes
	d := &t.Data[0]
	s0 := &t.Scales[row0*nBlocks]
	s1 := &t.Scales[row1*nBlocks]
	off0, off1 := row0*rowBytes, row1*rowBytes
	switch nBlocks {
	case 24:
		q8DualMultiDot3ScaledAVX512N24(out, &a[0], d, s0, s1, off0, off1)
		return true
	case 36:
		q8DualMultiDot3ScaledAVX512N36(out, &a[0], d, s0, s1, off0, off1)
		return true
	}
	return false
}

func gemmaGemmM8N24(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	const nBlocks, rowBytes = 24, 24 * q8BlockBytes
	d := &b.Data[0]
	var d0, d1 [8]float32
	n := ns
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot8ScaledAVX512N24(&d0, &d1, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM4(out, n, N, &d0)
		gemmaStoreM4(out[4*N:], n, N, &d1)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 8, N, 768, n)
	}
}

func gemmaGemmM4N36(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	const nBlocks, rowBytes = 36, 36 * q8BlockBytes
	d := &b.Data[0]
	var acc [8]float32
	n := ns
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot4ScaledAVX512N36(&acc, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM4(out, n, N, &acc)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 4, N, 1152, n)
	}
}

func gemmaGemmM8N36(out, a []float32, b *Q8Tensor, N, ns, ne int) {
	const nBlocks, rowBytes = 36, 36 * q8BlockBytes
	d := &b.Data[0]
	var d0, d1 [8]float32
	n := ns
	for ; n+1 < ne; n += 2 {
		q8DualMultiDot8ScaledAVX512N36(&d0, &d1, &a[0], d,
			&b.Scales[n*nBlocks], &b.Scales[(n+1)*nBlocks],
			n*rowBytes, (n+1)*rowBytes)
		gemmaStoreM4(out, n, N, &d0)
		gemmaStoreM4(out[4*N:], n, N, &d1)
	}
	if n < ne {
		gemmaStoreTailCol(out, a, b, 8, N, 1152, n)
	}
}

//go:noescape
func q8MultiDot4AVX2(out *[4]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

//go:noescape
func q8MultiDot4ScaledAVX2(out *[4]float32, a *float32, K int, data *byte, scales *float32, rowOff, nBlocks int)

//go:noescape
func q8MultiDot4ScaledAVX2N16(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8MultiDot4ScaledAVX2N24(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8MultiDot4ScaledAVX2N36(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8MultiDot4ScaledAVX2N64(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8TripleMultiDot4ScaledAVX2N64(out *[12]float32, a *float32, data *byte, scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2 int)

//go:noescape
func q8MultiDot8AVX2(out *[8]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

//go:noescape
func q8DualMultiDot4AVX2(out *[8]float32, a *float32, K int, data *byte, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualMultiDot4ScaledAVX2(out *[8]float32, a *float32, K int, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N16(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N24(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N36(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot2ScaledAVX2K512(out *[4]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX512N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX512N24(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot3ScaledAVX512N24(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot3ScaledAVX512N36(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX512N36(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot8ScaledAVX512N64(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot8ScaledAVX512N24(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot8ScaledAVX512N36(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

// q8DualMultiDot8ScaledAVX2N64 is the AVX2 fallback for K=2048 dual dots.
// Not yet wired to a Go caller; the declaration keeps the assembly's
// contract documented and vetted.
//
//go:noescape
func q8DualMultiDot8ScaledAVX2N64(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

// q8DualMultiDot8AccumAVX512N64: 8A×2B one-pass + residual/bias store for N=512 FFN down.
// out is &out[0]; writes rows m..m+7, cols n,n+1.
//
//go:noescape
func q8DualMultiDot8AccumAVX512N64(out *float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int, m, n int, bn0, bn1 float32)

// q8TryDual8AccumN512 fuses 8A×2B Q8 multiDot + residual/bias into out for N=512 FFN down.
// Returns true when the AVX-512 fused kernel handled the pair (n,n+1).
func q8TryDual8AccumN512(out, a []float32, t *Q8Tensor, m, n, nBlocks, K int, bn0, bn1 float32) bool {
	need := (n + 2) * nBlocks
	if !hasAVX512 || nBlocks != 64 || K != 2048 || len(a) < 8*K || len(t.Scales) < need || len(out) < (m+8)*512 {
		return false
	}
	rowBytes := nBlocks * q8BlockBytes
	s0 := &t.Scales[n*nBlocks]
	s1 := &t.Scales[(n+1)*nBlocks]
	q8DualMultiDot8AccumAVX512N64(&out[0], &a[0], &t.Data[0], s0, s1, n*rowBytes, (n+1)*rowBytes, m, n, bn0, bn1)
	return true
}

// q8APanel8 holds 8 rows of K=2048 prequantized as u8 Q8 (FFN-down ReLU A≥0).
// Both q and s are block-major for VNNI L1 locality:
//
//	q[b*256 + r*32 + i]  — 8 rows of one block = 256 contiguous bytes
//	s[b*8 + r]           — 8 scales of one block = 32 contiguous bytes
type q8APanel8 struct {
	q [8 * 2048]int8
	s [8 * 64]float32
}

var q8APanelPool = sync.Pool{New: func() any { return new(q8APanel8) }}

//go:noescape
func quantizePanel8Q8UAVX512(q *int8, s *float32, a *float32)

//go:noescape
func q8uQ8sDual8AccumVNNI(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)

// q8TryDual8AccumVNNI uses prequantized A_u8 × Q8 B via VPDPBUSD (Zen4+).
func q8TryDual8AccumVNNI(out []float32, ap *q8APanel8, t *Q8Tensor, m, n, nBlocks int, bn0, bn1 float32) bool {
	need := (n + 2) * nBlocks
	if !hasAVX512VNNI || nBlocks != 64 || len(t.Scales) < need || len(out) < (m+8)*512 {
		return false
	}
	q8Dual8AccumVNNIKnown(out, ap, t, m, n, bn0, bn1)
	return true
}

// q8Dual8AccumVNNIKnown is the unchecked VNNI call used after
// tryFusedAccumVNNI has validated the fixed N=512/K=2048 geometry. Keeping
// the checks outside the B-pair loop avoids repeating feature, shape, and
// slice-length checks for every output pair.
func q8Dual8AccumVNNIKnown(out []float32, ap *q8APanel8, t *Q8Tensor, m, n int, bn0, bn1 float32) {
	const rowBytes = 64 * q8BlockBytes
	q8uQ8sDual8AccumVNNI(&out[0], &ap.q[0], &ap.s[0], &t.Data[0],
		&t.Scales[n*64], &t.Scales[(n+1)*64],
		n*rowBytes, (n+1)*rowBytes, m, n, bn0, bn1)
}

func quantizePanel8Q8U(ap *q8APanel8, a []float32) {
	if hasAVX512 && len(a) >= 8*2048 {
		quantizePanel8Q8UAVX512(&ap.q[0], &ap.s[0], &a[0])
		return
	}
	// Scalar fallback (signed-safe clamp to 0..127); block-major q + s layout.
	const blocks = 64
	for r := 0; r < 8; r++ {
		row := a[r*2048 : (r+1)*2048]
		for b := 0; b < blocks; b++ {
			blk := row[b*32 : (b+1)*32]
			amax := float32(0)
			for i := 0; i < 32; i++ {
				if blk[i] > amax {
					amax = blk[i]
				}
			}
			base := b*256 + r*32 // block-major q
			sIdx := b*8 + r      // block-major s
			if amax <= 1e-7 {
				ap.s[sIdx] = 0
				for i := 0; i < 32; i++ {
					ap.q[base+i] = 0
				}
				continue
			}
			scale := amax / 127
			inv := 127 / amax
			ap.s[sIdx] = scale
			for i := 0; i < 32; i++ {
				v := blk[i] * inv
				if v > 127 {
					v = 127
				}
				if v < 0 {
					v = 0
				}
				ap.q[base+i] = int8(v + 0.5)
			}
		}
	}
}

// tryFusedAccumVNNI: FFN down N=512 K=2048 — prequant A, VNNI dual-B for all N.
// Dual-m 16: quantize two 8-row A panels, then for each B pair run both so Q8 B
// stays hot (mirrors float dual-m in matMulQ8RangeFusedAccumScaledBias).
const enableFusedAccumVNNI = true

func tryFusedAccumVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, ns, ne, nBlocks int) bool {
	if !enableFusedAccumVNNI || !hasAVX512VNNI || nBlocks != 64 || len(a) < M*2048 || len(b.Scales) < b.Rows*nBlocks {
		return false
	}
	// Panels are only consumed by 8-row blocks. Very short utterances take the
	// scalar remainder below, so avoid even the first pool round-trip for M < 8.
	var ap0 *q8APanel8
	if M >= 8 {
		ap0 = q8APanelPool.Get().(*q8APanel8)
	}
	// The second panel is used only by the 16-row loop below.
	var ap1 *q8APanel8
	if M >= 16 {
		ap1 = q8APanelPool.Get().(*q8APanel8)
	}
	m := 0
	// 16-row outer: two prequant panels share each B group.
	for ; m+15 < M; m += 16 {
		a0 := a[m*2048 : (m+8)*2048]
		a1 := a[(m+8)*2048 : (m+16)*2048]
		quantizePanel8Q8U(ap0, a0)
		quantizePanel8Q8U(ap1, a1)
		n := ns
		// Dual-B VNNI (vector float accums). Quad 4B variants measured slower /
		// incorrect under register pressure; keep dual only.
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := bias[n], bias[n+1]
			q8Dual8AccumVNNIKnown(out, ap0, b, m, n, bn0, bn1)
			q8Dual8AccumVNNIKnown(out, ap1, b, m+8, n, bn0, bn1)
		}
		for ; n < ne; n++ {
			var d8 [8]float32
			bn := bias[n]
			q8MultiDot8T(&d8, a0, b, n, nBlocks, 2048)
			storeDot8Accum(out, m, n, 512, &d8, bn)
			q8MultiDot8T(&d8, a1, b, n, nBlocks, 2048)
			storeDot8Accum(out, m+8, n, 512, &d8, bn)
		}
	}
	for ; m+7 < M; m += 8 {
		aPanel := a[m*2048 : (m+8)*2048]
		quantizePanel8Q8U(ap0, aPanel)
		n := ns
		for ; n+1 < ne; n += 2 {
			q8Dual8AccumVNNIKnown(out, ap0, b, m, n, bias[n], bias[n+1])
		}
		for ; n < ne; n++ {
			var d8 [8]float32
			q8MultiDot8T(&d8, aPanel, b, n, nBlocks, 2048)
			storeDot8Accum(out, m, n, 512, &d8, bias[n])
		}
	}
	// Remainder rows < 8: float dual path
	if m < M {
		var dDual0, dDual1 [8]float32
		var d4 [4]float32
		var d8 [8]float32
		for ; m+3 < M; m += 4 {
			aPanel := a[m*2048 : (m+4)*2048]
			n := ns
			for ; n+1 < ne; n += 2 {
				q8DualMultiDot4T(&dDual0, aPanel, b, n, n+1, nBlocks, 2048)
				storeDual4Accum(out, m, n, 512, &dDual0, bias[n], bias[n+1])
			}
			for ; n < ne; n++ {
				q8MultiDot4T(&d4, aPanel, b, n, nBlocks, 2048)
				storeDot4Accum(out, m, n, 512, &d4, bias[n])
			}
		}
		for ; m < M; m++ {
			aRow := a[m*2048 : m*2048+2048]
			n := ns
			for ; n+1 < ne; n += 2 {
				s0, s1 := DotQ8RowDualScaled(aRow, b, n, n+1)
				out[m*512+n] += s0 + bias[n]
				out[m*512+n+1] += s1 + bias[n+1]
			}
			for ; n < ne; n++ {
				out[m*512+n] += DotQ8RowScaled(aRow, b, n) + bias[n]
			}
		}
		_ = dDual1
		_ = d8
	}
	if ap0 != nil {
		q8APanelPool.Put(ap0)
	}
	if ap1 != nil {
		q8APanelPool.Put(ap1)
	}
	return true
}

//go:noescape
func q8MultiDot4ScaledAVX512N64(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8MultiDot4ScaledAVX512N24(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

//go:noescape
func q8MultiDot4ScaledAVX512N36(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

func q8MultiDot4Scalar(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	// Fallback: dequant once then multiDot4
	buf, bufPool := getQ8DequantBuf(K)
	dequantRowInto(data, row, nBlocks, buf)
	multiDot4(out, a, buf, K)
	putQ8DequantBuf(buf, bufPool)
}

func q8DualMultiDot4Scalar(out *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	buf, bufPool := getQ8DequantBuf(2 * K)
	dequantRowInto(data, row0, nBlocks, buf[:K])
	dequantRowInto(data, row1, nBlocks, buf[K:2*K])
	multiDot4DualB(out, a, buf[:K], buf[K:2*K], K)
	putQ8DequantBuf(buf, bufPool)
}

// dotQ8RowDual: one A × two B rows (fused dequant).
func dotQ8RowDual(a []float32, data []byte, row0, row1, nBlocks int) (float32, float32) {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= nBlocks*q8BlockSize {
		rowBytes := nBlocks * q8BlockBytes
		var out [2]float32
		q8DualDot2AVX2(&out, &a[0], &data[0], row0*rowBytes, row1*rowBytes, nBlocks)
		return out[0], out[1]
	}
	return DotQ8Row(a, data, row0, nBlocks), DotQ8Row(a, data, row1, nBlocks)
}

//go:noescape
func q8DualDot2AVX2(out *[2]float32, a *float32, data *byte, rowOff0, rowOff1, nBlocks int)

//go:noescape
func dotQ8RowScaledAVX2(a *float32, data *byte, scales *float32, rowOff, nBlocks int) float32

//go:noescape
func dotQ8RowScaledAVX512(a *float32, data *byte, scales *float32, rowOff, nBlocks int) float32

//go:noescape
func q8DualDot2ScaledAVX2(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualDot2ScaledAVX512(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualDot2ScaledAVX512N24(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualDot2ScaledAVX512N36(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

func dotQ8RowScaled(a []float32, data []byte, scales *float32, rowOff, nBlocks int) float32 {
	if nBlocks > 0 && len(a) >= nBlocks*q8BlockSize {
		if hasAVX512 {
			return dotQ8RowScaledAVX512(&a[0], &data[0], scales, rowOff, nBlocks)
		}
		if hasAVX2andFMA {
			return dotQ8RowScaledAVX2(&a[0], &data[0], scales, rowOff, nBlocks)
		}
	}
	return dotQ8RowScaledScalar(a, data, unsafe.Slice(scales, nBlocks), rowOff, nBlocks)
}

func dotQ8RowDualScaled(a []float32, data []byte, scales []float32, row0, row1, nBlocks int) (float32, float32) {
	if nBlocks > 0 && len(a) >= nBlocks*q8BlockSize {
		rowBytes := nBlocks * q8BlockBytes
		var out [2]float32
		if hasAVX512 {
			s0 := &scales[row0*nBlocks]
			s1 := &scales[row1*nBlocks]
			off0, off1 := row0*rowBytes, row1*rowBytes
			switch nBlocks {
			case 24:
				q8DualDot2ScaledAVX512N24(&out, &a[0], &data[0], s0, s1, off0, off1)
			case 36:
				q8DualDot2ScaledAVX512N36(&out, &a[0], &data[0], s0, s1, off0, off1)
			default:
				q8DualDot2ScaledAVX512(&out, &a[0], &data[0], s0, s1, off0, off1, nBlocks)
			}
			return out[0], out[1]
		}
		if hasAVX2andFMA {
			q8DualDot2ScaledAVX2(&out, &a[0], &data[0],
				&scales[row0*nBlocks], &scales[row1*nBlocks],
				row0*rowBytes, row1*rowBytes, nBlocks)
			return out[0], out[1]
		}
	}
	s0 := dotQ8RowScaledScalar(a, data, scales[row0*nBlocks:(row0+1)*nBlocks], row0*nBlocks*q8BlockBytes, nBlocks)
	s1 := dotQ8RowScaledScalar(a, data, scales[row1*nBlocks:(row1+1)*nBlocks], row1*nBlocks*q8BlockBytes, nBlocks)
	return s0, s1
}

func dotQ8RowScaledScalar(a []float32, data []byte, scales []float32, rowOff, nBlocks int) float32 {
	var sum float32
	for b := 0; b < nBlocks; b++ {
		scale := scales[b]
		base := b * q8BlockSize
		qOff := rowOff + b*q8BlockBytes + 2
		var blockSum float32
		for i := 0; i < q8BlockSize; i += 8 {
			blockSum += float32(int8(data[qOff+i])) * a[base+i]
			blockSum += float32(int8(data[qOff+i+1])) * a[base+i+1]
			blockSum += float32(int8(data[qOff+i+2])) * a[base+i+2]
			blockSum += float32(int8(data[qOff+i+3])) * a[base+i+3]
			blockSum += float32(int8(data[qOff+i+4])) * a[base+i+4]
			blockSum += float32(int8(data[qOff+i+5])) * a[base+i+5]
			blockSum += float32(int8(data[qOff+i+6])) * a[base+i+6]
			blockSum += float32(int8(data[qOff+i+7])) * a[base+i+7]
		}
		sum += scale * blockSum
	}
	return sum
}
