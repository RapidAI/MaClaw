//go:build amd64

package tensor

import "unsafe"

//go:noescape
func q4Q8BlockVNNIASM(out *int32, aQ, q4 *byte)

// q4Q8BlockVNNI returns eight four-element integer partial sums. aQ must be
// unsigned (the FFN-down ReLU activation representation), and q4 uses the
// canonical split-nibble Q4_0 layout.
func q4Q8BlockVNNI(out *[8]int32, aQ, q4 []byte) {
	if len(aQ) < q4BlockSize || len(q4) < q4BlockBytes {
		return
	}
	if hasAVX512VNNI {
		q4Q8BlockVNNIASM(&out[0], &aQ[0], &q4[0])
		return
	}
	for i, p := range q4[:q4BlockBytes] {
		out[i/4] += int32(int(p&0x0f)-8) * int32(aQ[i])
		out[4+i/4] += int32(int(p>>4)-8) * int32(aQ[q4BlockBytes+i])
	}
}

//go:noescape
func q4Q8BlocksVNNIASM(out *int32, aQ, q4 *byte, nBlocks int)

func q4Q8BlocksVNNI(out []int32, aQ, q4 []byte, nBlocks int) {
	if nBlocks <= 0 || len(out) < nBlocks*8 || len(aQ) < nBlocks*q4BlockSize || len(q4) < nBlocks*q4BlockBytes {
		return
	}
	if hasAVX512VNNI {
		q4Q8BlocksVNNIASM(&out[0], &aQ[0], &q4[0], nBlocks)
		return
	}
	for b := 0; b < nBlocks; b++ {
		var partial [8]int32
		q4Q8BlockVNNI(&partial, aQ[b*q4BlockSize:], q4[b*q4BlockBytes:])
		copy(out[b*8:], partial[:])
	}
}

//go:noescape
func q4Q8BlocksVNNIStrideASM(out *int32, aQ, q4 *byte, nBlocks, stride int)

// q4Q8BlocksVNNIStride reads one activation row directly from a block-major
// panel. SenseVoice's 8-row panel uses stride=256 bytes per Q8 block.
func q4Q8BlocksVNNIStride(out []int32, aQ, q4 []byte, nBlocks, stride int) {
	if nBlocks <= 0 || stride < q4BlockSize || len(out) < nBlocks*8 || len(aQ) < (nBlocks-1)*stride+q4BlockSize || len(q4) < nBlocks*q4BlockBytes {
		return
	}
	if hasAVX512VNNI {
		q4Q8BlocksVNNIStrideASM(&out[0], &aQ[0], &q4[0], nBlocks, stride)
		return
	}
	for b := 0; b < nBlocks; b++ {
		var partial [8]int32
		q4Q8BlockVNNI(&partial, aQ[b*stride:], q4[b*q4BlockBytes:])
		copy(out[b*8:], partial[:])
	}
}

//go:noescape
func q4Q8BlocksDualVNNIStrideASM(out0, out1 *int32, aQ, q40, q41 *byte, nBlocks, stride int)

// q4Q8BlocksDualVNNIStride evaluates two Q4 rows while loading the strided
// Q8 activation block only once per iteration.
func q4Q8BlocksDualVNNIStride(out0, out1 []int32, aQ, q40, q41 []byte, nBlocks, stride int) {
	if nBlocks <= 0 || stride < q4BlockSize || len(out0) < nBlocks*8 || len(out1) < nBlocks*8 || len(aQ) < (nBlocks-1)*stride+q4BlockSize || len(q40) < nBlocks*q4BlockBytes || len(q41) < nBlocks*q4BlockBytes {
		return
	}
	if hasAVX512VNNI {
		q4Q8BlocksDualVNNIStrideASM(&out0[0], &out1[0], &aQ[0], &q40[0], &q41[0], nBlocks, stride)
		return
	}
	q4Q8BlocksVNNIStride(out0, aQ, q40, nBlocks, stride)
	q4Q8BlocksVNNIStride(out1, aQ, q41, nBlocks, stride)
}

//go:noescape
func q4Q8Dual8AccumVNNIASM(out *float32, aQ *int8, aS *float32, q40, q41 *byte, s0, s1 *float32, m, n, N int, b0, b1 float32)

// q4Q8Dual8AccumVNNI is the FFN-down 8x2 epilogue.  It keeps the VNNI
// partial reductions, Q4/Q8 scales, bias, residual accumulation, and stores
// in assembly; the caller only schedules adjacent weight rows.
func q4Q8Dual8AccumVNNI(out []float32, ap *q8APanel8, w *Q4Tensor, m, n, N int, b0, b1 float32) bool {
	if !hasAVX512VNNI || ap == nil || w == nil || N <= n+1 || len(out) < (m+8)*N || n < 0 || n+1 >= w.Rows || w.Cols != 2048 || len(w.Scales) < (n+2)*64 || len(w.Data) < (n+2)*64*q4BlockBytes {
		return false
	}
	q40 := &w.Data[n*64*q4BlockBytes]
	q41 := &w.Data[(n+1)*64*q4BlockBytes]
	q4Q8Dual8AccumVNNIASM(&out[0], &ap.q[0], &ap.s[0], q40, q41, &w.Scales[n*64], &w.Scales[(n+1)*64], m, n, N, b0, b1)
	return true
}

// dotQ4Q8Panel8VNNI evaluates one Q4 row against all eight rows of a
// q8APanel8. The panel is block-major, so each activation row is stride 256.
func dotQ4Q8Panel8VNNI(out *[8]float32, w *Q4Tensor, weightRow int, ap *q8APanel8) bool {
	if w == nil || ap == nil || weightRow < 0 || weightRow >= w.Rows || w.Cols != 2048 || len(w.Scales) < (weightRow+1)*64 || len(w.Data) < (weightRow+1)*64*q4BlockBytes {
		return false
	}
	q4 := w.Data[weightRow*64*q4BlockBytes:]
	var partial [64 * 8]int32
	for r := 0; r < 8; r++ {
		aQ := unsafe.Slice((*byte)(unsafe.Pointer(&ap.q[r*32])), len(ap.q)-r*32)
		q4Q8BlocksVNNIStride(partial[:], aQ, q4, 64, 256)
		var sum float32
		for b := 0; b < 64; b++ {
			p := partial[b*8 : b*8+8]
			dot := p[0] + p[1] + p[2] + p[3] + p[4] + p[5] + p[6] + p[7]
			sum += float32(dot) * w.Scales[weightRow*64+b] * ap.s[b*8+r]
		}
		out[r] = sum
	}
	return true
}

// dotQ4Q8Panel8DualVNNI evaluates two adjacent Q4 rows over all eight
// activation rows. out0/out1 map to weightRow and weightRow+1 respectively.
func dotQ4Q8Panel8DualVNNI(out0, out1 *[8]float32, w *Q4Tensor, weightRow int, ap *q8APanel8) bool {
	if w == nil || ap == nil || weightRow < 0 || weightRow+1 >= w.Rows || w.Cols != 2048 || len(w.Scales) < (weightRow+2)*64 || len(w.Data) < (weightRow+2)*64*q4BlockBytes {
		return false
	}
	q40 := w.Data[weightRow*64*q4BlockBytes:]
	q41 := w.Data[(weightRow+1)*64*q4BlockBytes:]
	var partial0, partial1 [64 * 8]int32
	for r := 0; r < 8; r++ {
		aQ := unsafe.Slice((*byte)(unsafe.Pointer(&ap.q[r*32])), len(ap.q)-r*32)
		q4Q8BlocksDualVNNIStride(partial0[:], partial1[:], aQ, q40, q41, 64, 256)
		var sum0, sum1 float32
		for b := 0; b < 64; b++ {
			p0, p1 := partial0[b*8:b*8+8], partial1[b*8:b*8+8]
			d0 := p0[0] + p0[1] + p0[2] + p0[3] + p0[4] + p0[5] + p0[6] + p0[7]
			d1 := p1[0] + p1[1] + p1[2] + p1[3] + p1[4] + p1[5] + p1[6] + p1[7]
			s := ap.s[b*8+r]
			sum0 += float32(d0) * w.Scales[weightRow*64+b] * s
			sum1 += float32(d1) * w.Scales[(weightRow+1)*64+b] * s
		}
		out0[r], out1[r] = sum0, sum1
	}
	return true
}
