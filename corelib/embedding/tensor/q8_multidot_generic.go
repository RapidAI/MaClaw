//go:build !amd64 && !arm64

package tensor

import "unsafe"

func q8MultiDot4(out *[4]float32, a []float32, data []byte, row, nBlocks, K int) {
	buf := getQ8DequantBuf(K)
	dequantRowInto(data, row, nBlocks, buf)
	multiDot4(out, a, buf, K)
	putQ8DequantBuf(buf)
}

func q8MultiDot4T(out *[4]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	q8MultiDot4(out, a, t.Data, row, nBlocks, K)
}

func q8MultiDot8(out *[8]float32, a []float32, data []byte, row, nBlocks, K int) {
	var d0, d1 [4]float32
	q8MultiDot4(&d0, a[:4*K], data, row, nBlocks, K)
	q8MultiDot4(&d1, a[4*K:8*K], data, row, nBlocks, K)
	out[0], out[1], out[2], out[3] = d0[0], d0[1], d0[2], d0[3]
	out[4], out[5], out[6], out[7] = d1[0], d1[1], d1[2], d1[3]
}

func q8MultiDot8T(out *[8]float32, a []float32, t *Q8Tensor, row, nBlocks, K int) {
	q8MultiDot8(out, a, t.Data, row, nBlocks, K)
}

func q8DualMultiDot4(out *[8]float32, a []float32, data []byte, row0, row1, nBlocks, K int) {
	buf := getQ8DequantBuf(2 * K)
	dequantRowInto(data, row0, nBlocks, buf[:K])
	dequantRowInto(data, row1, nBlocks, buf[K:2*K])
	multiDot4DualB(out, a, buf[:K], buf[K:2*K], K)
	putQ8DequantBuf(buf)
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
