//go:build arm64

package tensor

import "unsafe"

// q8MultiDot4 fuses Q8 dequant of one B row with 4 dots against consecutive A rows.
// a is [4][K] contiguous, K = nBlocks*32. Avoids materializing the dequant buffer.
func q8MultiDot4(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	if nBlocks > 0 && len(a) >= 4*K {
		rowOff := row * nBlocks * q8BlockBytes
		q8MultiDot4NEON(out, &a[0], K, &data[0], rowOff, nBlocks)
		return
	}
	q8MultiDot4Scalar(out, a, data, row, nBlocks, K)
}

func q8MultiDot4T(out *[4]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	q8MultiDot4(out, a, t.Data, row, nBlocks, K)
}

// q8MultiDot8 fuses Q8 dequant with 8 dots.
func q8MultiDot8(out *[8]float32, a []float32, data []byte, row, nBlocks, K int) {
	if nBlocks > 0 && len(a) >= 8*K {
		rowOff := row * nBlocks * q8BlockBytes
		q8MultiDot8NEON(out, &a[0], K, &data[0], rowOff, nBlocks)
		return
	}
	var d0, d1 [4]float32
	q8MultiDot4(&d0, a[:4*K], data, row, nBlocks, K)
	q8MultiDot4(&d1, a[4*K:8*K], data, row, nBlocks, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func q8MultiDot8T(out *[8]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	q8MultiDot8(out, a, t.Data, row, nBlocks, K)
}

//go:noescape
func q8MultiDot4NEON(out *[4]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

//go:noescape
func q8MultiDot8NEON(out *[8]float32, a *float32, K int, data *byte, rowOff, nBlocks int)

func q8MultiDot4Scalar(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	buf, bufPool := getQ8DequantBuf(K)
	dequantRowInto(data, row, nBlocks, buf)
	multiDot4(out, a, buf, K)
	putQ8DequantBuf(buf, bufPool)
}

// q8DualMultiDot4: dequant two B rows then dual F32 multiDot (NEON fused dual later).
func q8DualMultiDot4(out *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	buf, bufPool := getQ8DequantBuf(2 * K)
	dequantRowInto(data, row0, nBlocks, buf[:K])
	dequantRowInto(data, row1, nBlocks, buf[K:2*K])
	multiDot4DualB(out, a, buf[:K], buf[K:2*K], K)
	putQ8DequantBuf(buf, bufPool)
}

func q8DualMultiDot4T(out *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
	q8DualMultiDot4(out, a, t.Data, row0, row1, nBlocks, K)
}

func q8DualMultiDot8(out0, out1 *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	q8DualMultiDot4(out0, a[:4*K], data, row0, row1, nBlocks, K)
	q8DualMultiDot4(out1, a[4*K:8*K], data, row0, row1, nBlocks, K)
}

func q8DualMultiDot8T(out0, out1 *[8]float32, a []float32, t *Q8Tensor, row0, row1, nBlocks, K int) {
	q8DualMultiDot8(out0, out1, a, t.Data, row0, row1, nBlocks, K)
}

func q8TryDual8AccumN512(out, a []float32, t *Q8Tensor, m, n, nBlocks, K int, bn0, bn1 float32) bool {
	return false
}

func tryFusedAccumVNNI(out, a []float32, b *Q8Tensor, bias []float32, M, ns, ne, nBlocks int) bool {
	return false
}

func q8TripleMultiDot4T(out *[12]float32, a []float32, t *Q8Tensor, row0, row1, row2, nBlocks, K int) {
	var d8 [8]float32
	var d4 [4]float32
	q8DualMultiDot4T(&d8, a, t, row0, row1, nBlocks, K)
	q8MultiDot4T(&d4, a, t, row2, nBlocks, K)
	out[0], out[1], out[2], out[3] = d8[0], d8[1], d8[2], d8[3]
	out[4], out[5], out[6], out[7] = d8[4], d8[5], d8[6], d8[7]
	out[8], out[9], out[10], out[11] = d4[0], d4[1], d4[2], d4[3]
}

func q8TripleMultiDot8T(out0, out1 *[12]float32, a []float32, t *Q8Tensor, row0, row1, row2, nBlocks, K int) {
	q8TripleMultiDot4T(out0, a[:4*K], t, row0, row1, row2, nBlocks, K)
	q8TripleMultiDot4T(out1, a[4*K:8*K], t, row0, row1, row2, nBlocks, K)
}

func dotQ8RowDual(a []float32, data []byte, row0, row1, nBlocks int) (float32, float32) {
	return DotQ8Row(a, data, row0, nBlocks), DotQ8Row(a, data, row1, nBlocks)
}

func dotQ8RowScaled(a []float32, data []byte, scales *float32, rowOff, nBlocks int) float32 {
	return dotQ8RowScaledScalar(a, data, unsafe.Slice(scales, nBlocks), rowOff, nBlocks)
}

func dotQ8RowDualScaled(a []float32, data []byte, scales []float32, row0, row1, nBlocks int) (float32, float32) {
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
		for i := 0; i < q8BlockSize; i++ {
			blockSum += float32(int8(data[qOff+i])) * a[base+i]
		}
		sum += scale * blockSum
	}
	return sum
}
