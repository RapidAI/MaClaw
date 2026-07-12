package tensor

import "math"

// Q4Tensor stores symmetric Q4_0 weights in the layout consumed by Q4×Q8
// SIMD kernels: each 16-byte block contains q[0:16] in low nibbles and
// q[16:32] in high nibbles. This matches GGML Q4_0 rather than packing
// adjacent values into each byte.
type Q4Tensor struct {
	Data   []byte
	Scales []float32
	Rows   int
	Cols   int
}

const q4BlockSize = 32
const q4BlockBytes = 16

// QuantizeToQ4 prepares canonical Q4_0 blocks for an ISA-specific Q4×Q8
// kernel. It is deliberately not dispatched by MatMul yet.
func QuantizeToQ4(data []float32, rows, cols int) *Q4Tensor {
	if rows <= 0 || cols <= 0 || cols%q4BlockSize != 0 || len(data) < rows*cols {
		return &Q4Tensor{Rows: rows, Cols: cols}
	}
	nBlocks := cols / q4BlockSize
	t := &Q4Tensor{Data: make([]byte, rows*nBlocks*q4BlockBytes), Scales: make([]float32, rows*nBlocks), Rows: rows, Cols: cols}
	for r := 0; r < rows; r++ {
		row := data[r*cols : (r+1)*cols]
		for b := 0; b < nBlocks; b++ {
			base := b * q4BlockSize
			maxAbs := float32(0)
			for i := 0; i < q4BlockSize; i++ {
				v := row[base+i]
				if v < 0 {
					v = -v
				}
				if v > maxAbs {
					maxAbs = v
				}
			}
			if maxAbs == 0 {
				continue
			}
			scale := maxAbs / 7
			idx := r*nBlocks + b
			t.Scales[idx] = scale
			inv, off := 1/scale, idx*q4BlockBytes
			for i := 0; i < q4BlockBytes; i++ {
				lo := int(math.Round(float64(row[base+i] * inv)))
				hi := int(math.Round(float64(row[base+q4BlockBytes+i] * inv)))
				if lo < -8 {
					lo = -8
				} else if lo > 7 {
					lo = 7
				}
				if hi < -8 {
					hi = -8
				} else if hi > 7 {
					hi = 7
				}
				t.Data[off+i] = byte(lo+8) | byte(hi+8)<<4
			}
		}
	}
	return t
}

// QuantizeQ8ToQ4 converts Q8_0 weights into canonical Q4_0 blocks at model
// load time without materializing a full F32 matrix.
func QuantizeQ8ToQ4(src *Q8Tensor) *Q4Tensor {
	if src == nil || src.Rows <= 0 || src.Cols <= 0 || src.Cols%q4BlockSize != 0 || len(src.Scales) < src.Rows*(src.Cols/q4BlockSize) || len(src.Data) < src.Rows*(src.Cols/q8BlockBytes) {
		return nil
	}
	nBlocks := src.Cols / q4BlockSize
	dst := &Q4Tensor{Data: make([]byte, src.Rows*nBlocks*q4BlockBytes), Scales: make([]float32, src.Rows*nBlocks), Rows: src.Rows, Cols: src.Cols}
	for r := 0; r < src.Rows; r++ {
		for b := 0; b < nBlocks; b++ {
			idx := r*nBlocks + b
			q8 := src.Data[idx*q8BlockBytes+2 : idx*q8BlockBytes+2+q4BlockSize]
			maxAbs := 0
			for _, v := range q8 {
				x := int(int8(v))
				if x < 0 {
					x = -x
				}
				if x > maxAbs {
					maxAbs = x
				}
			}
			if maxAbs == 0 {
				continue
			}
			scale := src.Scales[idx] * float32(maxAbs) / 7
			dst.Scales[idx] = scale
			for i := 0; i < q4BlockBytes; i++ {
				lo := int(math.Round(float64(float32(int8(q8[i])) * src.Scales[idx] / scale)))
				hi := int(math.Round(float64(float32(int8(q8[q4BlockBytes+i])) * src.Scales[idx] / scale)))
				if lo < -8 {
					lo = -8
				} else if lo > 7 {
					lo = 7
				}
				if hi < -8 {
					hi = -8
				} else if hi > 7 {
					hi = 7
				}
				dst.Data[idx*q4BlockBytes+i] = byte(lo+8) | byte(hi+8)<<4
			}
		}
	}
	return dst
}

// DotQ4Q8RowReference computes one Q4 weight row against one Q8 activation
// row. It is the numerical oracle for the forthcoming AVX Q4×Q8 kernel.
func DotQ4Q8RowReference(w *Q4Tensor, weightRow int, a *Q8Tensor, activationRow int) float32 {
	if w == nil || a == nil || weightRow < 0 || activationRow < 0 || weightRow >= w.Rows || activationRow >= a.Rows ||
		w.Cols != a.Cols || w.Cols%q4BlockSize != 0 {
		return 0
	}
	nBlocks := w.Cols / q4BlockSize
	if len(w.Scales) < (weightRow+1)*nBlocks || len(a.Scales) < (activationRow+1)*nBlocks ||
		len(w.Data) < (weightRow+1)*nBlocks*q4BlockBytes || len(a.Data) < (activationRow+1)*nBlocks*q8BlockBytes {
		return 0
	}
	var sum float32
	var dots [64]int32
	if nBlocks > len(dots) {
		return 0
	}
	dotQ4Q8Blocks(dots[:nBlocks], w.Data[weightRow*nBlocks*q4BlockBytes:], a.Data[activationRow*nBlocks*q8BlockBytes:], nBlocks)
	for b := 0; b < nBlocks; b++ {
		wi := weightRow*nBlocks + b
		ai := activationRow*nBlocks + b
		sum += float32(dots[b]) * w.Scales[wi] * a.Scales[ai]
	}
	return sum
}

// DotQ4Q8RowVNNI evaluates one Q4 row against a nonnegative Q8 activation
// row using the VNNI partial-sum kernel. It currently supports K<=2048, the
// FFN-down shape used by SenseVoice.
func DotQ4Q8RowVNNI(w *Q4Tensor, weightRow int, a *Q8Tensor, activationRow int) float32 {
	if w == nil || a == nil || weightRow < 0 || activationRow < 0 || weightRow >= w.Rows || activationRow >= a.Rows || w.Cols != a.Cols || w.Cols%q4BlockSize != 0 {
		return 0
	}
	nBlocks := w.Cols / q4BlockSize
	if nBlocks <= 0 || nBlocks > 64 || len(w.Scales) < (weightRow+1)*nBlocks || len(a.Scales) < (activationRow+1)*nBlocks || len(w.Data) < (weightRow+1)*nBlocks*q4BlockBytes || len(a.Data) < (activationRow+1)*nBlocks*q8BlockBytes {
		return 0
	}
	var q8Values [64 * q4BlockSize]byte
	for b := 0; b < nBlocks; b++ {
		src := a.Data[(activationRow*nBlocks+b)*q8BlockBytes+2:]
		copy(q8Values[b*q4BlockSize:(b+1)*q4BlockSize], src[:q4BlockSize])
	}
	var partials [64 * 8]int32
	q4 := w.Data[weightRow*nBlocks*q4BlockBytes:]
	q4Q8BlocksVNNI(partials[:nBlocks*8], q8Values[:nBlocks*q4BlockSize], q4, nBlocks)
	var sum float32
	for b := 0; b < nBlocks; b++ {
		p := partials[b*8 : b*8+8]
		blockSum := p[0] + p[1] + p[2] + p[3] + p[4] + p[5] + p[6] + p[7]
		sum += float32(blockSum) * w.Scales[weightRow*nBlocks+b] * a.Scales[activationRow*nBlocks+b]
	}
	return sum
}

// MatMulQ4BiasAdd computes out += A@Q4^T+bias. AMD64 routes production
// FFN-down panels to the Q4×Q8 VNNI kernel; other shapes use this exact
// scalar fallback.
func MatMulQ4BiasAdd(out, a []float32, w *Q4Tensor, bias []float32, M, N, K int) {
	if w == nil || M <= 0 || N <= 0 || K != w.Cols || N > w.Rows || len(a) < M*K || len(out) < M*N {
		return
	}
	if matMulQ4BiasAddPanel(out, a, w, bias, M, N, K) {
		return
	}
	for m := 0; m < M; m++ {
		for n := 0; n < N; n++ {
			v := dotQ4F32(a[m*K:(m+1)*K], w, n)
			if n < len(bias) {
				v += bias[n]
			}
			out[m*N+n] += v
		}
	}
}

func dotQ4F32(a []float32, w *Q4Tensor, row int) float32 {
	nBlocks := w.Cols / q4BlockSize
	var sum float32
	for b := 0; b < nBlocks; b++ {
		idx := row*nBlocks + b
		var dot float32
		for i := 0; i < q4BlockBytes; i++ {
			p := w.Data[idx*q4BlockBytes+i]
			dot += a[b*q4BlockSize+i] * float32(int(p&0x0f)-8)
			dot += a[b*q4BlockSize+q4BlockBytes+i] * float32(int(p>>4)-8)
		}
		sum += dot * w.Scales[idx]
	}
	return sum
}
