//go:build amd64

package tensor

import "unsafe"

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
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 4*K && len(t.Scales) >= (row+1)*nBlocks {
		rowOff := row * nBlocks * q8BlockBytes
		// Fixed-geometry: K=512 remainder single-B (dual covers the main path).
		if nBlocks == 16 {
			q8MultiDot4ScaledAVX2N16(out, &a[0], &t.Data[0], &t.Scales[row*nBlocks], rowOff)
			return
		}
		q8MultiDot4ScaledAVX2(out, &a[0], K, &t.Data[0], &t.Scales[row*nBlocks], rowOff, nBlocks)
		return
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
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 8*K && len(t.Scales) >= (row+1)*nBlocks {
		var d0, d1 [4]float32
		q8MultiDot4T(&d0, a[:4*K], t, row, nBlocks, K)
		q8MultiDot4T(&d1, a[4*K:8*K], t, row, nBlocks, K)
		out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
		out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
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
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= 4*K && len(t.Scales) >= need {
		rowBytes := nBlocks * q8BlockBytes
		s0 := &t.Scales[row0*nBlocks]
		s1 := &t.Scales[row1*nBlocks]
		d := &t.Data[0]
		// Fixed-geometry kernels for SenseVoice shapes (no K/nBlocks bookkeeping).
		switch nBlocks {
		case 16: // K=512 encoder / proj
			q8DualMultiDot4ScaledAVX2N16(out, &a[0], d, s0, s1, row0*rowBytes, row1*rowBytes)
			return
		case 64: // K=2048 FFN down-proj (fused path)
			q8DualMultiDot4ScaledAVX2N64(out, &a[0], d, s0, s1, row0*rowBytes, row1*rowBytes)
			return
		}
		q8DualMultiDot4ScaledAVX2(out, &a[0], K, d, s0, s1, row0*rowBytes, row1*rowBytes, nBlocks)
		return
	}
	q8DualMultiDot4(out, a, t.Data, row0, row1, nBlocks, K)
}

// q8DualMultiDot8: 8 A rows × 2 B via two dual-4 kernels.
// dual-4×2 keeps all accums in YMM (no stack spill); measured faster than
// one-pass 8-row with B1 accums on stack for K=2048.
func q8DualMultiDot8(out0, out1 *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	q8DualMultiDot4(out0, a[:4*K], data, row0, row1, nBlocks, K)
	q8DualMultiDot4(out1, a[4*K:8*K], data, row0, row1, nBlocks, K)
}

func q8DualMultiDot8T(out0, out1 *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
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
func q8MultiDot8AVX2(out *[8]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

//go:noescape
func q8DualMultiDot4AVX2(out *[8]float32, a *float32, K int, data *byte, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualMultiDot4ScaledAVX2(out *[8]float32, a *float32, K int, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N16(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

//go:noescape
func q8DualMultiDot4ScaledAVX2N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)

func q8MultiDot4Scalar(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	// Fallback: dequant once then multiDot4
	buf := getQ8DequantBuf(K)
	dequantRowInto(data, row, nBlocks, buf)
	multiDot4(out, a, buf, K)
	putQ8DequantBuf(buf)
}

func q8DualMultiDot4Scalar(out *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	buf := getQ8DequantBuf(2 * K)
	dequantRowInto(data, row0, nBlocks, buf[:K])
	dequantRowInto(data, row1, nBlocks, buf[K:2*K])
	multiDot4DualB(out, a, buf[:K], buf[K:2*K], K)
	putQ8DequantBuf(buf)
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
func q8DualDot2ScaledAVX2(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

func dotQ8RowScaled(a []float32, data []byte, scales *float32, rowOff, nBlocks int) float32 {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= nBlocks*q8BlockSize {
		return dotQ8RowScaledAVX2(&a[0], &data[0], scales, rowOff, nBlocks)
	}
	return dotQ8RowScaledScalar(a, data, unsafe.Slice(scales, nBlocks), rowOff, nBlocks)
}

func dotQ8RowDualScaled(a []float32, data []byte, scales []float32, row0, row1, nBlocks int) (float32, float32) {
	if hasAVX2andFMA && nBlocks > 0 && len(a) >= nBlocks*q8BlockSize {
		rowBytes := nBlocks * q8BlockBytes
		var out [2]float32
		q8DualDot2ScaledAVX2(&out, &a[0], &data[0],
			&scales[row0*nBlocks], &scales[row1*nBlocks],
			row0*rowBytes, row1*rowBytes, nBlocks)
		return out[0], out[1]
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
