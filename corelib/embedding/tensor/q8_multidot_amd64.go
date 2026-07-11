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
		if hasAVX512 && nBlocks == 64 {
			q8MultiDot4ScaledAVX512N64(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
			return
		}
		if hasAVX2andFMA {
			// Fixed-geometry: K=512 / K=2048 remainder single-B.
			switch nBlocks {
			case 16:
				q8MultiDot4ScaledAVX2N16(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
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
		if hasAVX512 && nBlocks == 64 {
			q8DualMultiDot4ScaledAVX512N64(out, &a[0], d, s0, s1, off0, off1)
			return
		}
		if hasAVX2andFMA {
			// Fixed-geometry kernels for SenseVoice shapes (no K/nBlocks bookkeeping).
			switch nBlocks {
			case 16: // K=512 encoder / proj
				q8DualMultiDot4ScaledAVX2N16(out, &a[0], d, s0, s1, off0, off1)
				return
			case 64: // K=2048 FFN down-proj (fused path)
				q8DualMultiDot4ScaledAVX2N64(out, &a[0], d, s0, s1, off0, off1)
				return
			}
			q8DualMultiDot4ScaledAVX2(out, &a[0], K, d, s0, s1, off0, off1, nBlocks)
			return
		}
	}
	q8DualMultiDot4(out, a, t.Data, row0, row1, nBlocks, K)
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
		if hasAVX512 && nBlocks == 64 {
			// 8 A × 2 B one pass (scales in Z30/Z31; accums Z0-15 dual4 layout).
			q8DualMultiDot8ScaledAVX512N64(out0, out1, &a[0], d, s0, s1, off0, off1)
			return
		}
		if hasAVX2andFMA {
			switch nBlocks {
			case 64: // K=2048 FFN down — B stays hot across both dual-4 halves
				q8DualMultiDot4ScaledAVX2N64(out0, &a[0], d, s0, s1, off0, off1)
				q8DualMultiDot4ScaledAVX2N64(out1, &a[4*K], d, s0, s1, off0, off1)
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
func q8MultiDot4AVX2(out *[4]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

//go:noescape
func q8MultiDot4ScaledAVX2(out *[4]float32, a *float32, K int, data *byte, scales *float32, rowOff, nBlocks int)

//go:noescape
func q8MultiDot4ScaledAVX2N16(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

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
func q8DualMultiDot4ScaledAVX2N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX512N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot8ScaledAVX512N64(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

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
	rowBytes := nBlocks * q8BlockBytes
	q8uQ8sDual8AccumVNNI(&out[0], &ap.q[0], &ap.s[0], &t.Data[0],
		&t.Scales[n*nBlocks], &t.Scales[(n+1)*nBlocks],
		n*rowBytes, (n+1)*rowBytes, m, n, bn0, bn1)
	return true
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
	ap0 := q8APanelPool.Get().(*q8APanel8)
	ap1 := q8APanelPool.Get().(*q8APanel8)
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
			if !q8TryDual8AccumVNNI(out, ap0, b, m, n, nBlocks, bn0, bn1) {
				q8TryDual8AccumN512(out, a0, b, m, n, nBlocks, 2048, bn0, bn1)
			}
			if !q8TryDual8AccumVNNI(out, ap1, b, m+8, n, nBlocks, bn0, bn1) {
				q8TryDual8AccumN512(out, a1, b, m+8, n, nBlocks, 2048, bn0, bn1)
			}
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
			if !q8TryDual8AccumVNNI(out, ap0, b, m, n, nBlocks, bias[n], bias[n+1]) {
				q8TryDual8AccumN512(out, aPanel, b, m, n, nBlocks, 2048, bias[n], bias[n+1])
			}
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
	q8APanelPool.Put(ap0)
	q8APanelPool.Put(ap1)
	return true
}

//go:noescape
func q8MultiDot4ScaledAVX512N64(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)

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
			q8DualDot2ScaledAVX512(&out, &a[0], &data[0],
				&scales[row0*nBlocks], &scales[row1*nBlocks],
				row0*rowBytes, row1*rowBytes, nBlocks)
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
