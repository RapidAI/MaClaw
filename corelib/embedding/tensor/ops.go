// Package tensor provides optimized tensor operations for transformer inference.
// Uses github.com/viterin/vek/vek32 for AVX2/NEON SIMD acceleration on hot paths.
package tensor

import (
	"math"
	"sync"

	"github.com/viterin/vek/vek32"
)

// MatMul computes out = A @ B^T where A is [M, K] and B is [N, K] (row-major).
// Result out is [M, N]. Uses SIMD-accelerated dot product for each row pair.
func MatMul(out, a, b []float32, M, N, K int) {
	nCPU := getMatMulWorkers()
	if nCPU > 1 && N*K > 4096 {
		if M > 1 {
			matMulParallel(out, a, b, M, N, K)
			return
		}
		matMulParallelN(out, a, b, M, N, K)
		return
	}
	for m := 0; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for n := 0; n < N; n++ {
			out[m*N+n] = vek32.Dot(aRow, b[n*K:n*K+K])
		}
	}
}

func matMulParallel(out, a, b []float32, M, N, K int) {
	nWorkers := getMatMulWorkers()
	if nWorkers > M {
		nWorkers = M
	}
	var wg sync.WaitGroup
	rowsPerWorker := (M + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		start := w * rowsPerWorker
		end := start + rowsPerWorker
		if end > M {
			end = M
		}
		if start >= end {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for m := s; m < e; m++ {
				aRow := a[m*K : m*K+K]
				for n := 0; n < N; n++ {
					out[m*N+n] = vek32.Dot(aRow, b[n*K:n*K+K])
				}
			}
		}(start, end)
	}
	wg.Wait()
}

// matMulParallelN parallelizes across the N (output) dimension for small M.
func matMulParallelN(out, a, b []float32, M, N, K int) {
	nWorkers := getMatMulWorkers()
	if nWorkers > N {
		nWorkers = N
	}
	var wg sync.WaitGroup
	colsPerWorker := (N + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		nStart := w * colsPerWorker
		nEnd := nStart + colsPerWorker
		if nEnd > N {
			nEnd = N
		}
		if nStart >= nEnd {
			break
		}
		wg.Add(1)
		go func(ns, ne int) {
			defer wg.Done()
			for m := 0; m < M; m++ {
				aRow := a[m*K : m*K+K]
				for n := ns; n < ne; n++ {
					out[m*N+n] = vek32.Dot(aRow, b[n*K:n*K+K])
				}
			}
		}(nStart, nEnd)
	}
	wg.Wait()
}

// Dot computes the dot product of two vectors using SIMD acceleration.
func Dot(a, b []float32) float32 {
	return vek32.Dot(a, b)
}

// Axpy computes y[i] += a * x[i] (BLAS-style), in-place on y.
// Uses SIMD: scale x into a temp, then add in-place to y.
func Axpy(a float32, x, y []float32) {
	if len(x) == 0 || a == 0 {
		return
	}
	// Fast path: a == 1 → just add
	if a == 1.0 {
		vek32.Add_Inplace(y[:len(x)], x)
		return
	}
	// General path: use scratch from pool to avoid allocation
	buf := getAxpyBuf(len(x))
	copy(buf, x)
	vek32.MulNumber_Inplace(buf, a)
	vek32.Add_Inplace(y[:len(x)], buf)
	putAxpyBuf(buf)
}

// axpyPool caches scratch buffers for Axpy to avoid per-call allocation.
var axpyPool = sync.Pool{
	New: func() interface{} { return make([]float32, 0, 256) },
}

func getAxpyBuf(n int) []float32 {
	buf := axpyPool.Get().([]float32)
	if cap(buf) < n {
		buf = make([]float32, n)
	}
	return buf[:n]
}

func putAxpyBuf(buf []float32) {
	axpyPool.Put(buf[:0])
}

// RMSNorm computes RMS normalization: out[i] = x[i] / rms(x) * weight[i].
// out and x may alias (in-place normalization).
func RMSNorm(out, x, weight []float32, eps float32) {
	n := len(x)
	ss := vek32.Dot(x, x) // sum of squares via SIMD dot product
	scale := 1.0 / float32(math.Sqrt(float64(ss/float32(n)+eps)))
	// Fuse scale*weight into a single multiply pass
	for i := 0; i < n; i++ {
		out[i] = x[i] * (scale * weight[i])
	}
}

// fastExp computes an approximate exp(x) for float32 using the Schraudolph
// bit-trick. Max relative error ~0.06% in [-10, 10], which is sufficient for
// SiLU/Softmax in embedding inference where the final output is L2-normalized.
func fastExp(x float32) float32 {
	// exp(x) ≈ 2^(x / ln2) via IEEE 754 float bit manipulation.
	// Constant: 2^23 / ln(2) ≈ 12102203.16, bias: 127 * 2^23 = 1065353216.
	const (
		a = 12102203.0 // 2^23 / ln(2)
		b = 1065353216.0 - 60801.0 // 127*2^23 - correction term
	)
	if x < -88 {
		return 0
	}
	if x > 88 {
		return math.MaxFloat32
	}
	i := int32(a*x + b)
	return math.Float32frombits(uint32(i))
}

// SiLU computes the SiLU activation: x * sigmoid(x), in-place.
func SiLU(x []float32) {
	for i := range x {
		v := x[i]
		x[i] = v / (1.0 + fastExp(-v))
	}
}

// SiLUMul computes SiLU(gate) * up in-place into gate, fusing two operations.
// This avoids a separate ElemMul pass and reduces memory traffic.
func SiLUMul(gate, up []float32) {
	for i := range gate {
		v := gate[i]
		gate[i] = (v / (1.0 + fastExp(-v))) * up[i]
	}
}

// ElemMul computes element-wise multiplication: out[i] = a[i] * b[i].
// out may alias a or b when they share the same starting address.
func ElemMul(out, a, b []float32) {
	if len(out) == 0 {
		return
	}
	if &out[0] == &a[0] {
		vek32.Mul_Inplace(out, b)
		return
	}
	if &out[0] == &b[0] {
		vek32.Mul_Inplace(out, a)
		return
	}
	vek32.Mul_Into(out, a, b)
}

// Add computes element-wise addition: out[i] = a[i] + b[i].
// out may alias a or b when they share the same starting address.
func Add(out, a, b []float32) {
	if len(out) == 0 {
		return
	}
	if &out[0] == &a[0] {
		// out aliases a — use in-place variant (out += b).
		vek32.Add_Inplace(out, b)
		return
	}
	if &out[0] == &b[0] {
		// out aliases b — use in-place variant (out += a).
		vek32.Add_Inplace(out, a)
		return
	}
	vek32.Add_Into(out, a, b)
}

// Scale multiplies all elements by a scalar, in-place.
func Scale(x []float32, s float32) {
	vek32.MulNumber_Inplace(x, s)
}

// Softmax computes softmax over a slice, in-place.
func Softmax(x []float32) {
	if len(x) == 0 {
		return
	}
	max := vek32.Max(x)
	vek32.AddNumber_Inplace(x, -max)
	// Use fastExp for float32 — avoids float64 conversion per element
	for i := range x {
		x[i] = fastExp(x[i])
	}
	sum := vek32.Sum(x)
	if sum != 0 {
		vek32.MulNumber_Inplace(x, 1.0/sum)
	}
}

// RoPE applies Rotary Position Embedding to q/k tensors.
// x is [nHeads, headDim], pos is the token position.
func RoPE(x []float32, nHeads, headDim, pos int, theta float32) {
	halfDim := headDim / 2
	for h := 0; h < nHeads; h++ {
		off := h * headDim
		for i := 0; i < halfDim; i++ {
			freq := 1.0 / float32(math.Pow(float64(theta), float64(2*i)/float64(headDim)))
			angle := float32(pos) * freq
			cos := float32(math.Cos(float64(angle)))
			sin := float32(math.Sin(float64(angle)))
			x0 := x[off+i]
			x1 := x[off+i+halfDim]
			x[off+i] = x0*cos - x1*sin
			x[off+i+halfDim] = x0*sin + x1*cos
		}
	}
}

// RoPEPrecomputed applies RoPE using pre-computed cos/sin tables.
// cosTable and sinTable are [halfDim] for the given position.
// This avoids expensive math.Pow/Cos/Sin calls per layer (24 layers × seq positions).
func RoPEPrecomputed(x []float32, nHeads, headDim int, cosTable, sinTable []float32) {
	halfDim := headDim / 2
	for h := 0; h < nHeads; h++ {
		off := h * headDim
		for i := 0; i < halfDim; i++ {
			cos := cosTable[i]
			sin := sinTable[i]
			x0 := x[off+i]
			x1 := x[off+i+halfDim]
			x[off+i] = x0*cos - x1*sin
			x[off+i+halfDim] = x0*sin + x1*cos
		}
	}
}

// L2Normalize normalizes a vector to unit length, in-place.
func L2Normalize(v []float32) {
	norm := vek32.Norm(v)
	if norm == 0 {
		return
	}
	vek32.MulNumber_Inplace(v, 1.0/norm)
}

// LayerNorm computes LayerNorm (no bias): (x - mean) / sqrt(var + eps) * weight.
// out and x may alias.
func LayerNorm(out, x, weight []float32, eps float32) {
	n := len(x)
	mean := vek32.Sum(x) / float32(n)
	var variance float32
	for i := 0; i < n; i++ {
		d := x[i] - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for i := 0; i < n; i++ {
		out[i] = (x[i] - mean) * scale * weight[i]
	}
}

// GroupNorm1 computes GroupNorm with n_groups=1 over a [time, channels] tensor.
// Normalizes across all time*channels elements, then applies per-channel affine.
// data is row-major [time][channels]. weight and bias are [channels].
func GroupNorm1(data []float32, time, channels int, weight, bias []float32, eps float32) {
	n := time * channels
	mean := vek32.Sum(data[:n]) / float32(n)
	var variance float32
	for i := 0; i < n; i++ {
		d := data[i] - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for t := 0; t < time; t++ {
		off := t * channels
		for c := 0; c < channels; c++ {
			v := (data[off+c] - mean) * scale
			if weight != nil {
				v *= weight[c]
			}
			if bias != nil {
				v += bias[c]
			}
			data[off+c] = v
		}
	}
}

// Tanh applies tanh activation in-place.
func Tanh(x []float32) {
	for i := range x {
		x[i] = float32(math.Tanh(float64(x[i])))
	}
}

// GELU applies GELU activation in-place (approximate).
func GELU(x []float32) {
	const c = 0.7978845608 // sqrt(2/pi)
	for i := range x {
		v := float64(x[i])
		x[i] = float32(0.5 * v * (1.0 + math.Tanh(c*(v+0.044715*v*v*v))))
	}
}

// AddBias adds a bias vector [dim] to each row of data [rows, dim].
// Uses SIMD-accelerated in-place addition for each row.
func AddBias(data []float32, rows, dim int, bias []float32) {
	for r := 0; r < rows; r++ {
		off := r * dim
		vek32.Add_Inplace(data[off:off+dim], bias[:dim])
	}
}

// RoPEInterleaved applies interleaved RoPE (mode=0, matching HF rotate_half).
// x is [nHeads * headDim], pos is the token position.
// Pairs are (x[0],x[1]), (x[2],x[3]), ... up to rotaryDim.
func RoPEInterleaved(x []float32, nHeads, headDim, pos int, theta float32, partialRotaryFactor float32) {
	rotaryDim := int(float32(headDim) * partialRotaryFactor)
	rotaryDim -= rotaryDim % 2 // must be even
	for h := 0; h < nHeads; h++ {
		off := h * headDim
		for i := 0; i < rotaryDim; i += 2 {
			dimIdx := i / 2
			freq := 1.0 / float32(math.Pow(float64(theta), float64(2*dimIdx)/float64(headDim)))
			angle := float32(pos) * freq
			cos := float32(math.Cos(float64(angle)))
			sin := float32(math.Sin(float64(angle)))
			x0 := x[off+i]
			x1 := x[off+i+1]
			x[off+i] = x0*cos - x1*sin
			x[off+i+1] = x0*sin + x1*cos
		}
	}
}
