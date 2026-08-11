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
//
// Loop order is N-outer so each B row stays hot while dotted against all M A
// rows (critical for encoder-style M≈frames, N≈hidden/ff).
func MatMul(out, a, b []float32, M, N, K int) {
	// Delegate to bias path with nil bias (same kernels, no second pass).
	MatMulBias(out, a, b, nil, M, N, K)
}

// MatMulBias is MatMul with optional bias fused into the store:
// out[m,n] = dot(A[m], B[n]) + bias[n].
//
// For M>1 uses M-tile outer (A panel hot); for M==1 parallelizes over N.
func MatMulBias(out, a, b, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if M == 1 {
		if shouldParallel(1, N, K) {
			matMulParallelN_M1(out, a, b, bias, N, K)
			return
		}
		for n := 0; n < N; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[n] = vek32.Dot(a[:K], b[n*K:n*K+K]) + bn
		}
		return
	}
	if shouldParallel(M, N, K) {
		matMulParallelN_MTile(out, a, b, bias, M, N, K)
		return
	}
	matMulSerialM(out, a, b, bias, M, N, K)
}

// MatMulBiasReLU computes out = max(0, A @ B^T + bias).
func MatMulBiasReLU(out, a, b, bias []float32, M, N, K int) {
	MatMulBias(out, a, b, bias, M, N, K)
	reluInplace(out[:M*N])
}

// MatMulBiasAdd: out[m,n] += A@B^T + bias (residual accumulate, float B).
func MatMulBiasAdd(out, a, b, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	if shouldParallel(M, N, K) {
		matMulF32ParallelN(out, a, b, bias, M, N, K, true)
		return
	}
	matMulRangeDualAdd(out, a, b, bias, M, N, K, 0, N)
}

// MatMulArgmax: per-row argmax of A @ B^T + bias (float B), no full matrix store.
func MatMulArgmax(outIDs []int, a, b, bias []float32, M, N, K int) {
	if M <= 0 || N <= 0 || K <= 0 {
		return
	}
	fn := func(ms, me int) {
		for m := ms; m < me; m++ {
			aRow := a[m*K : m*K+K]
			bestID := 0
			bestVal := float32(-1e38)
			for n := 0; n < N; n++ {
				s := vek32.Dot(aRow, b[n*K:n*K+K])
				if bias != nil {
					s += bias[n]
				}
				if s > bestVal {
					bestVal, bestID = s, n
				}
			}
			outIDs[m] = bestID
		}
	}
	if M > 1 && poolWorkers() > 1 && int64(M)*int64(N)*int64(K) >= flopThreshold {
		parallelRanges(M, fn)
		return
	}
	fn(0, M)
}

func matMulParallelN_M1(out, a, b, bias []float32, N, K int) {
	parallelRanges(N, func(ns, ne int) {
		for n := ns; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[n] = vek32.Dot(a[:K], b[n*K:n*K+K]) + bn
		}
	})
}

// matMulSerialM: M-tile outer keeps A panel hot while streaming B rows.
// Dual-B multiDot amortizes A loads across two B columns.
func matMulSerialM(out, a, b, bias []float32, M, N, K int) {
	matMulRangeDual(out, a, b, bias, M, N, K, 0, N)
}

// matMulParallelN_MTile: partition N via pool; M-tile outer inside each worker.
func matMulParallelN_MTile(out, a, b, bias []float32, M, N, K int) {
	matMulF32ParallelN(out, a, b, bias, M, N, K, false)
}

type f32RangeTask struct {
	out, a, b, bias []float32
	M, N, K         int
	add             bool
	wg              sync.WaitGroup
}

func (t *f32RangeTask) runRange(ns, ne int) {
	if t.add {
		matMulRangeDualAdd(t.out, t.a, t.b, t.bias, t.M, t.N, t.K, ns, ne)
		return
	}
	matMulRangeDual(t.out, t.a, t.b, t.bias, t.M, t.N, t.K, ns, ne)
}

var f32RangeTaskPool = sync.Pool{New: func() any { return new(f32RangeTask) }}

func matMulF32ParallelN(out, a, b, bias []float32, M, N, K int, add bool) {
	nw := rangeWorkers(N, matMulWorkersFor(M, N, K))
	if nw <= 1 {
		if add {
			matMulRangeDualAdd(out, a, b, bias, M, N, K, 0, N)
		} else {
			matMulRangeDual(out, a, b, bias, M, N, K, 0, N)
		}
		return
	}
	t := f32RangeTaskPool.Get().(*f32RangeTask)
	// A single task is shared by every worker. Assign and wait before it is
	// returned to the pool: assignment must happen before jobs are published
	// and fields must remain intact until all queued ranges finish.
	t.out, t.a, t.b, t.bias = out, a, b, bias
	t.M, t.N, t.K, t.add = M, N, K, add
	t.wg.Add(nw)
	chunk := (N + nw - 1) / nw
	ensureMatmulPool()
	for w := 0; w < nw; w++ {
		s, e := w*chunk, (w+1)*chunk
		if e > N {
			e = N
		}
		jobQueue <- matmulRangeJob{start: s, end: e, task: t, wg: &t.wg}
	}
	t.wg.Wait()
	t.out, t.a, t.b, t.bias = nil, nil, nil, nil
	f32RangeTaskPool.Put(t)
}

func matMulRangeDual(out, a, b, bias []float32, M, N, K, ns, ne int) {
	// For very wide column ranges the B panel (span*K floats) is re-streamed
	// from memory once per 8-row M tile. Block the range so each B sub-panel
	// stays cache resident across the M tiles. Each output element is still
	// produced by the same kernel over the same K order, so results are
	// bit-identical to the unblocked walk.
	const bPanelMaxElems = 1 << 17 // 512 KB of float32
	if span := ne - ns; M > 8 && span*K > bPanelMaxElems {
		nb := bPanelMaxElems / K
		nb &^= 1 // keep dual-B pairs inside a block
		if nb >= 2 && nb < span {
			for n0 := ns; n0 < ne; n0 += nb {
				n1 := n0 + nb
				if n1 > ne {
					n1 = ne
				}
				matMulRangeDual(out, a, b, bias, M, N, K, n0, n1)
			}
			return
		}
	}
	var d4 [4]float32
	var d2 [4]float32
	var d8 [8]float32
	var dDual0, dDual1 [8]float32
	m := 0
	for ; m+7 < M; m += 8 {
		aPanel := a[m*K : (m+8)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			b0 := b[n*K : n*K+K]
			b1 := b[(n+1)*K : (n+1)*K+K]
			multiDot8DualB(&dDual0, &dDual1, aPanel, b0, b1, K)
			storeF32Dual4(out, m, n, N, &dDual0, bn0, bn1)
			storeF32Dual4(out, m+4, n, N, &dDual1, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			multiDot8(&d8, aPanel, b[n*K:n*K+K], K)
			for t := 0; t < 8; t++ {
				out[(m+t)*N+n] = d8[t] + bn
			}
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			multiDot4DualB(&dDual0, aPanel, b[n*K:n*K+K], b[(n+1)*K:(n+1)*K+K], K)
			storeF32Dual4(out, m, n, N, &dDual0, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			multiDot4(&d4, aPanel, b[n*K:n*K+K], K)
			out[m*N+n] = d4[0] + bn
			out[(m+1)*N+n] = d4[1] + bn
			out[(m+2)*N+n] = d4[2] + bn
			out[(m+3)*N+n] = d4[3] + bn
		}
	}
	for ; m+1 < M; m += 2 {
		aPanel := a[m*K : (m+2)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			d2 = [4]float32{}
			multiDot2DualB(&d2, aPanel, b[n*K:n*K+K], b[(n+1)*K:(n+1)*K+K], K)
			base := m * N
			out[base+n], out[base+n+1] = d2[0]+bn0, d2[2]+bn1
			base += N
			out[base+n], out[base+n+1] = d2[1]+bn0, d2[3]+bn1
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[m*N+n] = vek32.Dot(aPanel[:K], b[n*K:n*K+K]) + bn
			out[(m+1)*N+n] = vek32.Dot(aPanel[K:], b[n*K:n*K+K]) + bn
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for n := ns; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[m*N+n] = vek32.Dot(aRow, b[n*K:n*K+K]) + bn
		}
	}
}

func storeF32Dual4(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32) {
	// Pair-adjacent (n, n+1) per row for better store locality.
	base := m * N
	out[base+n] = d[0] + bn0
	out[base+n+1] = d[4] + bn1
	base += N
	out[base+n] = d[1] + bn0
	out[base+n+1] = d[5] + bn1
	base += N
	out[base+n] = d[2] + bn0
	out[base+n+1] = d[6] + bn1
	base += N
	out[base+n] = d[3] + bn0
	out[base+n+1] = d[7] + bn1
}

func storeF32Dual4Add(out []float32, m, n, N int, d *[8]float32, bn0, bn1 float32) {
	base := m * N
	out[base+n] += d[0] + bn0
	out[base+n+1] += d[4] + bn1
	base += N
	out[base+n] += d[1] + bn0
	out[base+n+1] += d[5] + bn1
	base += N
	out[base+n] += d[2] + bn0
	out[base+n+1] += d[6] + bn1
	base += N
	out[base+n] += d[3] + bn0
	out[base+n+1] += d[7] + bn1
}

// matMulRangeDualAdd: out += A@B^T + bias over columns [ns,ne).
func matMulRangeDualAdd(out, a, b, bias []float32, M, N, K, ns, ne int) {
	var d4 [4]float32
	var d2 [4]float32
	var d8 [8]float32
	var dDual0, dDual1 [8]float32
	m := 0
	for ; m+7 < M; m += 8 {
		aPanel := a[m*K : (m+8)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			multiDot8DualB(&dDual0, &dDual1, aPanel, b[n*K:n*K+K], b[(n+1)*K:(n+1)*K+K], K)
			storeF32Dual4Add(out, m, n, N, &dDual0, bn0, bn1)
			storeF32Dual4Add(out, m+4, n, N, &dDual1, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			multiDot8(&d8, aPanel, b[n*K:n*K+K], K)
			for t := 0; t < 8; t++ {
				out[(m+t)*N+n] += d8[t] + bn
			}
		}
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			multiDot4DualB(&dDual0, aPanel, b[n*K:n*K+K], b[(n+1)*K:(n+1)*K+K], K)
			storeF32Dual4Add(out, m, n, N, &dDual0, bn0, bn1)
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			multiDot4(&d4, aPanel, b[n*K:n*K+K], K)
			out[m*N+n] += d4[0] + bn
			out[(m+1)*N+n] += d4[1] + bn
			out[(m+2)*N+n] += d4[2] + bn
			out[(m+3)*N+n] += d4[3] + bn
		}
	}
	for ; m+1 < M; m += 2 {
		aPanel := a[m*K : (m+2)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			bn0, bn1 := float32(0), float32(0)
			if bias != nil {
				bn0, bn1 = bias[n], bias[n+1]
			}
			d2 = [4]float32{}
			multiDot2DualB(&d2, aPanel, b[n*K:n*K+K], b[(n+1)*K:(n+1)*K+K], K)
			base := m * N
			out[base+n], out[base+n+1] = out[base+n]+d2[0]+bn0, out[base+n+1]+d2[2]+bn1
			base += N
			out[base+n], out[base+n+1] = out[base+n]+d2[1]+bn0, out[base+n+1]+d2[3]+bn1
		}
		for ; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[m*N+n] += vek32.Dot(aPanel[:K], b[n*K:n*K+K]) + bn
			out[(m+1)*N+n] += vek32.Dot(aPanel[K:], b[n*K:n*K+K]) + bn
		}
	}
	for ; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for n := ns; n < ne; n++ {
			bn := float32(0)
			if bias != nil {
				bn = bias[n]
			}
			out[m*N+n] += vek32.Dot(aRow, b[n*K:n*K+K]) + bn
		}
	}
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
// Uses SIMD for both the sum-of-squares and the fused scale*weight multiply.
func RMSNorm(out, x, weight []float32, eps float32) {
	n := len(x)
	ss := vek32.Dot(x, x) // sum of squares via SIMD dot product
	scale := 1.0 / float32(math.Sqrt(float64(ss/float32(n)+eps)))

	// Check if out aliases x (same backing array start).
	if len(out) > 0 && len(x) > 0 && &out[0] == &x[0] {
		// In-place: scale x first, then element-wise multiply with weight.
		vek32.MulNumber_Inplace(out[:n], scale)
		vek32.Mul_Inplace(out[:n], weight[:n])
	} else {
		// Non-alias: compute scaled_weight into out, then multiply with x.
		vek32.MulNumber_Into(out[:n], weight[:n], scale)
		vek32.Mul_Inplace(out[:n], x[:n])
	}
}

// fastExp computes an approximate exp(x) for float32 using the Schraudolph
// bit-trick. Max relative error ~0.06% in [-10, 10], which is sufficient for
// SiLU/Softmax in embedding inference where the final output is L2-normalized.
func fastExp(x float32) float32 {
	// exp(x) ≈ 2^(x / ln2) via IEEE 754 float bit manipulation.
	// Constant: 2^23 / ln(2) ≈ 12102203.16, bias: 127 * 2^23 = 1065353216.
	const (
		a = 12102203.0             // 2^23 / ln(2)
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
// On amd64, uses AVX2 SIMD acceleration.
func SiLUMul(gate, up []float32) {
	siluMulASM(gate, up)
}

// siluMulScalar is the pure Go fallback implementation.
// Uses fastExp and 8x unrolled loop for ILP.
func siluMulScalar(gate, up []float32) {
	n := len(gate)
	i := 0
	for ; i+7 < n; i += 8 {
		v0, v1, v2, v3 := gate[i], gate[i+1], gate[i+2], gate[i+3]
		v4, v5, v6, v7 := gate[i+4], gate[i+5], gate[i+6], gate[i+7]
		gate[i] = (v0 / (1.0 + fastExp(-v0))) * up[i]
		gate[i+1] = (v1 / (1.0 + fastExp(-v1))) * up[i+1]
		gate[i+2] = (v2 / (1.0 + fastExp(-v2))) * up[i+2]
		gate[i+3] = (v3 / (1.0 + fastExp(-v3))) * up[i+3]
		gate[i+4] = (v4 / (1.0 + fastExp(-v4))) * up[i+4]
		gate[i+5] = (v5 / (1.0 + fastExp(-v5))) * up[i+5]
		gate[i+6] = (v6 / (1.0 + fastExp(-v6))) * up[i+6]
		gate[i+7] = (v7 / (1.0 + fastExp(-v7))) * up[i+7]
	}
	for ; i < n; i++ {
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
// Fuses subtract-max and exp into a single pass to reduce memory traffic.
func Softmax(x []float32) {
	if len(x) == 0 {
		return
	}
	max := vek32.Max(x)
	// Fused: subtract max and compute exp in one pass
	var sum float32
	for i := range x {
		v := fastExp(x[i] - max)
		x[i] = v
		sum += v
	}
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
// On amd64, uses AVX2 SIMD acceleration.
func RoPEPrecomputed(x []float32, nHeads, headDim int, cosTable, sinTable []float32) {
	ropePrecomputedASM(x, nHeads, headDim, cosTable, sinTable)
}

// ropePrecomputedScalar is the pure Go fallback implementation.
// Uses 2x unrolled inner loop for better ILP.
func ropePrecomputedScalar(x []float32, nHeads, headDim int, cosTable, sinTable []float32) {
	halfDim := headDim / 2
	for h := 0; h < nHeads; h++ {
		off := h * headDim
		i := 0
		for ; i+1 < halfDim; i += 2 {
			cos0, sin0 := cosTable[i], sinTable[i]
			cos1, sin1 := cosTable[i+1], sinTable[i+1]
			x0a, x1a := x[off+i], x[off+i+halfDim]
			x0b, x1b := x[off+i+1], x[off+i+1+halfDim]
			x[off+i] = x0a*cos0 - x1a*sin0
			x[off+i+halfDim] = x0a*sin0 + x1a*cos0
			x[off+i+1] = x0b*cos1 - x1b*sin1
			x[off+i+1+halfDim] = x0b*sin1 + x1b*cos1
		}
		for ; i < halfDim; i++ {
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
	if n == 0 {
		return
	}
	mean := vek32.Sum(x) / float32(n)
	variance := vek32.Dot(x, x)/float32(n) - mean*mean
	if variance < 0 {
		variance = 0
	}
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	out = out[:n]
	if &out[0] != &x[0] {
		copy(out, x[:n])
	}
	vek32.AddNumber_Inplace(out, -mean)
	vek32.MulNumber_Inplace(out, scale)
	vek32.Mul_Inplace(out, weight[:n])
}

// GroupNorm1 computes GroupNorm with n_groups=1 over a [time, channels] tensor.
// Normalizes across all time*channels elements, then applies per-channel affine.
// data is row-major [time][channels]. weight and bias are [channels].
func GroupNorm1(data []float32, time, channels int, weight, bias []float32, eps float32) {
	n := time * channels
	if n == 0 {
		return
	}
	mean := vek32.Sum(data[:n]) / float32(n)
	variance := vek32.Dot(data[:n], data[:n])/float32(n) - mean*mean
	if variance < 0 {
		variance = 0
	}
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))
	for t := 0; t < time; t++ {
		off := t * channels
		row := data[off : off+channels]
		vek32.AddNumber_Inplace(row, -mean)
		vek32.MulNumber_Inplace(row, scale)
		if weight != nil {
			vek32.Mul_Inplace(row, weight[:channels])
		}
		if bias != nil {
			vek32.Add_Inplace(row, bias[:channels])
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

// AddBiasGELU fuses row-wise bias addition with GELU activation.
func AddBiasGELU(data []float32, rows, dim int, bias []float32) {
	const c = 0.7978845608 // sqrt(2/pi)
	for r := 0; r < rows; r++ {
		off := r * dim
		row := data[off : off+dim]
		for i := 0; i < dim; i++ {
			v := float64(row[i] + bias[i])
			row[i] = float32(0.5 * v * (1.0 + math.Tanh(c*(v+0.044715*v*v*v))))
		}
	}
}

// WeightedSumStrided computes out = sum_r weights[r] * values[r*stride:r*stride+dim].
// It is tuned for attention heads where each value row is embedded in a larger
// model-dimension stride.
func WeightedSumStrided(out, weights, values []float32, rows, stride, dim int) {
	if rows <= 0 || dim <= 0 {
		return
	}
	w0 := weights[0]
	v0 := values[:dim]
	i := 0
	for ; i+7 < dim; i += 8 {
		out[i] = w0 * v0[i]
		out[i+1] = w0 * v0[i+1]
		out[i+2] = w0 * v0[i+2]
		out[i+3] = w0 * v0[i+3]
		out[i+4] = w0 * v0[i+4]
		out[i+5] = w0 * v0[i+5]
		out[i+6] = w0 * v0[i+6]
		out[i+7] = w0 * v0[i+7]
	}
	for ; i < dim; i++ {
		out[i] = w0 * v0[i]
	}

	for r := 1; r < rows; r++ {
		w := weights[r]
		if w == 0 {
			continue
		}
		v := values[r*stride : r*stride+dim]
		i = 0
		for ; i+7 < dim; i += 8 {
			out[i] += w * v[i]
			out[i+1] += w * v[i+1]
			out[i+2] += w * v[i+2]
			out[i+3] += w * v[i+3]
			out[i+4] += w * v[i+4]
			out[i+5] += w * v[i+5]
			out[i+6] += w * v[i+6]
			out[i+7] += w * v[i+7]
		}
		for ; i < dim; i++ {
			out[i] += w * v[i]
		}
	}
}

// SoftmaxWeightedSumStrided computes out = softmax(scores) @ values without
// allocating normalized weights. scores is reused as exp(score-max) scratch,
// then scaled by invSum and fed to WeightedSumStrided.
func SoftmaxWeightedSumStrided(out, scores, values []float32, rows, stride, dim int) {
	if rows <= 0 || dim <= 0 {
		return
	}
	scores = scores[:rows]
	inv := softmaxInplaceInv(scores)
	if inv == 0 {
		clear(out[:dim])
		return
	}
	if dim == 128 {
		// Hot path for SenseVoice headDim=128: fuse invSum into weights on the fly.
		weightedSumStridedScaled(out, scores, values, rows, stride, dim, inv)
		return
	}
	vek32.MulNumber_Inplace(scores, inv)
	WeightedSumStrided(out, scores, values, rows, stride, dim)
}

// softmaxInplaceInv writes exp(x-max) into scores and returns 1/sum (0 if empty/zero).
// amd64: AVX2 max + Schraudolph exp vectorized for the attention hot path.
func softmaxInplaceInv(scores []float32) float32 {
	return softmaxInplaceInvASM(scores)
}

// softmaxInplaceInvDual: two equal-length rows (attention batch).
func softmaxInplaceInvDual(sc0, sc1 []float32) (float32, float32) {
	return softmaxInplaceInvDualASM(sc0, sc1)
}

// softmaxInplaceInvScalar is the pure-Go path (also used for short tails).
func softmaxInplaceInvScalar(scores []float32) float32 {
	rows := len(scores)
	if rows == 0 {
		return 0
	}
	// Unrolled max (avoids vek call overhead on short score rows T~60-100).
	max := scores[0]
	i := 1
	for ; i+3 < rows; i += 4 {
		v0, v1, v2, v3 := scores[i], scores[i+1], scores[i+2], scores[i+3]
		if v0 > max {
			max = v0
		}
		if v1 > max {
			max = v1
		}
		if v2 > max {
			max = v2
		}
		if v3 > max {
			max = v3
		}
	}
	for ; i < rows; i++ {
		if scores[i] > max {
			max = scores[i]
		}
	}
	var sum float32
	i = 0
	for ; i+3 < rows; i += 4 {
		v0 := fastExp(scores[i] - max)
		v1 := fastExp(scores[i+1] - max)
		v2 := fastExp(scores[i+2] - max)
		v3 := fastExp(scores[i+3] - max)
		scores[i], scores[i+1], scores[i+2], scores[i+3] = v0, v1, v2, v3
		sum += v0 + v1 + v2 + v3
	}
	for ; i < rows; i++ {
		v := fastExp(scores[i] - max)
		scores[i] = v
		sum += v
	}
	if sum == 0 {
		return 0
	}
	return 1.0 / sum
}

// SoftmaxWeightedSumBatched writes nQ attention outputs sharing one V stream.
// scores: [nQ][rows] row-major; values: strided rows of length dim starting at values[0].
// Each query t writes out[(qf+t)*outStride+hOff : +dim].
// Softmax writes exp(x-max) into scores; invSum is folded into V weights (no separate
// MulNumber pass over the score rows).
// nQ must be 4 or 8 (SenseVoice attention tiles).
func SoftmaxWeightedSumBatched(out, scores, values []float32, nQ, rows, vStride, dim, outStride, hOff, qf int) {
	if nQ <= 0 || rows <= 0 || dim <= 0 {
		return
	}
	if nQ > 8 {
		nQ = 8
	}
	var invs [8]float32
	// Dual-row softmax when possible (attention nQ=4/8 equal-length rows).
	t := 0
	for ; t+1 < nQ; t += 2 {
		sc0 := scores[t*rows : (t+1)*rows]
		sc1 := scores[(t+1)*rows : (t+2)*rows]
		inv0, inv1 := softmaxInplaceInvDual(sc0, sc1)
		if inv0 == 0 {
			clear(sc0)
		}
		if inv1 == 0 {
			clear(sc1)
		}
		invs[t], invs[t+1] = inv0, inv1
	}
	for ; t < nQ; t++ {
		sc := scores[t*rows : (t+1)*rows]
		inv := softmaxInplaceInv(sc)
		if inv == 0 {
			clear(sc)
		}
		invs[t] = inv
	}
	// Contiguous headDim=128 + small batch: specialized kernel.
	if dim == 128 && vStride == 128 && (nQ == 8 || nQ == 4) {
		weightedSumBatchedContig128(out, scores, values, nQ, rows, outStride, hOff, qf, invs)
		return
	}
	// Generic fallback: fold inv per query via scaled weighted sum.
	for t := 0; t < nQ; t++ {
		oOff := (qf+t)*outStride + hOff
		sc := scores[t*rows : (t+1)*rows]
		inv := invs[t]
		if inv == 0 {
			clear(out[oOff : oOff+dim])
			continue
		}
		weightedSumStridedScaled(out[oOff:oOff+dim], sc, values, rows, vStride, dim, inv)
	}
}

// weightedSumBatchedContig128: nQ∈{4,8} queries × contiguous V [rows][128].
// scores hold exp(x-max); invs[t] is 1/sum for query t (folded into weights).
// Outer loop over V rows so each 128-vector is loaded once (AVX2 FMA kernel).
// First V row seeds outputs (set); remaining rows accumulate — no clear pass.
func weightedSumBatchedContig128(out, scores, values []float32, nQ, rows, outStride, hOff, qf int, invs [8]float32) {
	const dim = 128
	if rows <= 0 {
		return
	}
	var o0, o1, o2, o3, o4, o5, o6, o7 []float32
	o0 = out[(qf+0)*outStride+hOff : (qf+0)*outStride+hOff+dim]
	o1 = out[(qf+1)*outStride+hOff : (qf+1)*outStride+hOff+dim]
	o2 = out[(qf+2)*outStride+hOff : (qf+2)*outStride+hOff+dim]
	o3 = out[(qf+3)*outStride+hOff : (qf+3)*outStride+hOff+dim]
	if nQ >= 8 {
		o4 = out[(qf+4)*outStride+hOff : (qf+4)*outStride+hOff+dim]
		o5 = out[(qf+5)*outStride+hOff : (qf+5)*outStride+hOff+dim]
		o6 = out[(qf+6)*outStride+hOff : (qf+6)*outStride+hOff+dim]
		o7 = out[(qf+7)*outStride+hOff : (qf+7)*outStride+hOff+dim]
	}

	// Seed: prefer set-add dual on V[0],V[1] (no out reload), then dual-add pairs.
	if nQ >= 8 {
		inv0, inv1, inv2, inv3 := invs[0], invs[1], invs[2], invs[3]
		inv4, inv5, inv6, inv7 := invs[4], invs[5], invs[6], invs[7]
		var wa, wb [8]float32
		r := 0
		if rows >= 2 {
			vA := values[:dim]
			vB := values[dim : 2*dim]
			wa[0], wa[1], wa[2], wa[3] = scores[0*rows]*inv0, scores[1*rows]*inv1, scores[2*rows]*inv2, scores[3*rows]*inv3
			wa[4], wa[5], wa[6], wa[7] = scores[4*rows]*inv4, scores[5*rows]*inv5, scores[6*rows]*inv6, scores[7*rows]*inv7
			wb[0], wb[1], wb[2], wb[3] = scores[0*rows+1]*inv0, scores[1*rows+1]*inv1, scores[2*rows+1]*inv2, scores[3*rows+1]*inv3
			wb[4], wb[5], wb[6], wb[7] = scores[4*rows+1]*inv4, scores[5*rows+1]*inv5, scores[6*rows+1]*inv6, scores[7*rows+1]*inv7
			wsumBatched8SetAdd128Dual(o0, o1, o2, o3, o4, o5, o6, o7, vA, vB, &wa, &wb)
			r = 2
		} else if rows == 1 {
			wsumBatched8Set128(
				o0, o1, o2, o3, o4, o5, o6, o7, values[:dim],
				scores[0*rows]*inv0, scores[1*rows]*inv1, scores[2*rows]*inv2, scores[3*rows]*inv3,
				scores[4*rows]*inv4, scores[5*rows]*inv5, scores[6*rows]*inv6, scores[7*rows]*inv7,
			)
			return
		}
		for ; r+1 < rows; r += 2 {
			vA := values[r*dim : (r+1)*dim]
			vB := values[(r+1)*dim : (r+2)*dim]
			wa[0], wa[1], wa[2], wa[3] = scores[0*rows+r]*inv0, scores[1*rows+r]*inv1, scores[2*rows+r]*inv2, scores[3*rows+r]*inv3
			wa[4], wa[5], wa[6], wa[7] = scores[4*rows+r]*inv4, scores[5*rows+r]*inv5, scores[6*rows+r]*inv6, scores[7*rows+r]*inv7
			wb[0], wb[1], wb[2], wb[3] = scores[0*rows+r+1]*inv0, scores[1*rows+r+1]*inv1, scores[2*rows+r+1]*inv2, scores[3*rows+r+1]*inv3
			wb[4], wb[5], wb[6], wb[7] = scores[4*rows+r+1]*inv4, scores[5*rows+r+1]*inv5, scores[6*rows+r+1]*inv6, scores[7*rows+r+1]*inv7
			wsumBatched8Add128Dual(o0, o1, o2, o3, o4, o5, o6, o7, vA, vB, &wa, &wb)
		}
		for ; r < rows; r++ {
			vrow := values[r*dim : (r+1)*dim]
			wsumBatched8Add128(
				o0, o1, o2, o3, o4, o5, o6, o7, vrow,
				scores[0*rows+r]*inv0, scores[1*rows+r]*inv1, scores[2*rows+r]*inv2, scores[3*rows+r]*inv3,
				scores[4*rows+r]*inv4, scores[5*rows+r]*inv5, scores[6*rows+r]*inv6, scores[7*rows+r]*inv7,
			)
		}
		return
	}
	inv0, inv1, inv2, inv3 := invs[0], invs[1], invs[2], invs[3]
	r := 0
	if rows >= 2 {
		wsumBatched4SetAdd128Dual(
			o0, o1, o2, o3, values[:dim], values[dim:2*dim],
			scores[0*rows]*inv0, scores[1*rows]*inv1, scores[2*rows]*inv2, scores[3*rows]*inv3,
			scores[0*rows+1]*inv0, scores[1*rows+1]*inv1, scores[2*rows+1]*inv2, scores[3*rows+1]*inv3,
		)
		r = 2
	} else if rows == 1 {
		wsumBatched4Set128(
			o0, o1, o2, o3, values[:dim],
			scores[0*rows]*inv0, scores[1*rows]*inv1, scores[2*rows]*inv2, scores[3*rows]*inv3,
		)
		return
	}
	for ; r+1 < rows; r += 2 {
		vA := values[r*dim : (r+1)*dim]
		vB := values[(r+1)*dim : (r+2)*dim]
		wsumBatched4Add128Dual(
			o0, o1, o2, o3, vA, vB,
			scores[0*rows+r]*inv0, scores[1*rows+r]*inv1, scores[2*rows+r]*inv2, scores[3*rows+r]*inv3,
			scores[0*rows+r+1]*inv0, scores[1*rows+r+1]*inv1, scores[2*rows+r+1]*inv2, scores[3*rows+r+1]*inv3,
		)
	}
	for ; r < rows; r++ {
		vrow := values[r*dim : (r+1)*dim]
		wsumBatched4Add128(
			o0, o1, o2, o3, vrow,
			scores[0*rows+r]*inv0, scores[1*rows+r]*inv1, scores[2*rows+r]*inv2, scores[3*rows+r]*inv3,
		)
	}
}

// weightedSumStridedScaled: out = sum_r (weights[r]*scale) * values[r*stride:].
func weightedSumStridedScaled(out, weights, values []float32, rows, stride, dim int, scale float32) {
	// Contiguous V rows (stride==dim): better locality after head packing.
	if stride == dim && dim == 128 {
		weightedSumContig128(out, weights, values, rows, scale)
		return
	}
	w0 := weights[0] * scale
	v0 := values[:dim]
	i := 0
	for ; i+7 < dim; i += 8 {
		out[i] = w0 * v0[i]
		out[i+1] = w0 * v0[i+1]
		out[i+2] = w0 * v0[i+2]
		out[i+3] = w0 * v0[i+3]
		out[i+4] = w0 * v0[i+4]
		out[i+5] = w0 * v0[i+5]
		out[i+6] = w0 * v0[i+6]
		out[i+7] = w0 * v0[i+7]
	}
	for ; i < dim; i++ {
		out[i] = w0 * v0[i]
	}
	for r := 1; r < rows; r++ {
		w := weights[r] * scale
		if w == 0 {
			continue
		}
		v := values[r*stride : r*stride+dim]
		i = 0
		for ; i+7 < dim; i += 8 {
			out[i] += w * v[i]
			out[i+1] += w * v[i+1]
			out[i+2] += w * v[i+2]
			out[i+3] += w * v[i+3]
			out[i+4] += w * v[i+4]
			out[i+5] += w * v[i+5]
			out[i+6] += w * v[i+6]
			out[i+7] += w * v[i+7]
		}
		for ; i < dim; i++ {
			out[i] += w * v[i]
		}
	}
}

// weightedSumContig128: out = sum_r (weights[r]*scale) * values[r][128].
// 16-wide unroll; contiguous rows stream well after packing V heads.
func weightedSumContig128(out, weights, values []float32, rows int, scale float32) {
	const dim = 128
	w0 := weights[0] * scale
	v0 := values[:dim]
	for i := 0; i < dim; i += 16 {
		out[i] = w0 * v0[i]
		out[i+1] = w0 * v0[i+1]
		out[i+2] = w0 * v0[i+2]
		out[i+3] = w0 * v0[i+3]
		out[i+4] = w0 * v0[i+4]
		out[i+5] = w0 * v0[i+5]
		out[i+6] = w0 * v0[i+6]
		out[i+7] = w0 * v0[i+7]
		out[i+8] = w0 * v0[i+8]
		out[i+9] = w0 * v0[i+9]
		out[i+10] = w0 * v0[i+10]
		out[i+11] = w0 * v0[i+11]
		out[i+12] = w0 * v0[i+12]
		out[i+13] = w0 * v0[i+13]
		out[i+14] = w0 * v0[i+14]
		out[i+15] = w0 * v0[i+15]
	}
	for r := 1; r < rows; r++ {
		w := weights[r] * scale
		if w == 0 {
			continue
		}
		v := values[r*dim : (r+1)*dim]
		for i := 0; i < dim; i += 16 {
			out[i] += w * v[i]
			out[i+1] += w * v[i+1]
			out[i+2] += w * v[i+2]
			out[i+3] += w * v[i+3]
			out[i+4] += w * v[i+4]
			out[i+5] += w * v[i+5]
			out[i+6] += w * v[i+6]
			out[i+7] += w * v[i+7]
			out[i+8] += w * v[i+8]
			out[i+9] += w * v[i+9]
			out[i+10] += w * v[i+10]
			out[i+11] += w * v[i+11]
			out[i+12] += w * v[i+12]
			out[i+13] += w * v[i+13]
			out[i+14] += w * v[i+14]
			out[i+15] += w * v[i+15]
		}
	}
}

// MultiDot4 computes 4 dots of consecutive A rows against the same B vector.
// a is [4][K] contiguous row-major; b is [K]; out receives 4 results.
// Uses AVX2/NEON multi-row kernels when available.
func MultiDot4(out *[4]float32, a, b []float32, K int) {
	multiDot4(out, a, b, K)
}

// MultiDot8 computes 8 dots of consecutive A rows against the same B vector.
func MultiDot8(out *[8]float32, a, b []float32, K int) {
	multiDot8(out, a, b, K)
}

// MultiDot4DualB computes 4 A rows × 2 B vectors.
// out[0:4] = dots with b0, out[4:8] = dots with b1.
// Loads each A chunk once (better bandwidth than two MultiDot4 calls).
func MultiDot4DualB(out *[8]float32, a, b0, b1 []float32, K int) {
	multiDot4DualB(out, a, b0, b1, K)
}

// MultiDot8DualB computes 8 A rows × 2 B vectors.
// out0/out1 each hold dual-4 layout: [0:4]=b0, [4:8]=b1 for rows 0-3 / 4-7.
// B stays hot across the two dual-4 micro-kernels.
func MultiDot8DualB(out0, out1 *[8]float32, a, b0, b1 []float32, K int) {
	multiDot8DualB(out0, out1, a, b0, b1, K)
}

// MultiDot8TripleB computes 8 A rows × 3 B vectors via two triple-4 kernels.
// out0/out1 each hold triple-4 layout: [0:4]=b0, [4:8]=b1, [8:12]=b2.
func MultiDot8TripleB(out0, out1 *[12]float32, a, b0, b1, b2 []float32, K int) {
	multiDot8TripleB(out0, out1, a, b0, b1, b2, K)
}

// FmaddInto computes out[i] += a[i] * b[i] (fused multiply-add, in-place on out).
// Dispatches to AVX2+FMA when available (FSMN hidden=512 hot path).
func FmaddInto(out, a, b []float32) {
	n := len(out)
	if n > len(a) {
		n = len(a)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmaddInto(out[:n], a[:n], b[:n])
}

// FmaddPlusOneInto computes out[i] += a[i]*(b[i]+1) = a[i]*b[i] + a[i].
// Used by FSMN to fold residual V into the center kernel term (ki==pad).
func FmaddPlusOneInto(out, a, b []float32) {
	n := len(out)
	if n > len(a) {
		n = len(a)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmaddPlusOneInto(out[:n], a[:n], b[:n])
}

// Mul2Into: o_r[i] = a_r[i]*b[i] for r=0..1 (FSMN dual-frame seed).
func Mul2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	mul2Into(o0[:n], o1[:n], a0[:n], a1[:n], b[:n])
}

// Fmadd2Into: o_r[i] += a_r[i]*b[i] for r=0..1.
func Fmadd2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmadd2Into(o0[:n], o1[:n], a0[:n], a1[:n], b[:n])
}

// FmaddPlusOne2Into: o_r[i] += a_r[i]*(b[i]+1) for r=0..1.
func FmaddPlusOne2Into(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmaddPlusOne2Into(o0[:n], o1[:n], a0[:n], a1[:n], b[:n])
}

// Mul4Into: o_r[i] = a_r[i]*b[i] for r=0..3 (FSMN seed; loads b once).
func Mul4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(o2) {
		n = len(o2)
	}
	if n > len(o3) {
		n = len(o3)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(a2) {
		n = len(a2)
	}
	if n > len(a3) {
		n = len(a3)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	mul4Into(o0[:n], o1[:n], o2[:n], o3[:n], a0[:n], a1[:n], a2[:n], a3[:n], b[:n])
}

// Fmadd4Into: o_r[i] += a_r[i]*b[i] for r=0..3 (FSMN quad; loads b once).
func Fmadd4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(o2) {
		n = len(o2)
	}
	if n > len(o3) {
		n = len(o3)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(a2) {
		n = len(a2)
	}
	if n > len(a3) {
		n = len(a3)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmadd4Into(o0[:n], o1[:n], o2[:n], o3[:n], a0[:n], a1[:n], a2[:n], a3[:n], b[:n])
}

// FmaddPlusOne4Into: o_r[i] += a_r[i]*(b[i]+1) for r=0..3 (FSMN center+V).
func FmaddPlusOne4Into(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	if n > len(o1) {
		n = len(o1)
	}
	if n > len(o2) {
		n = len(o2)
	}
	if n > len(o3) {
		n = len(o3)
	}
	if n > len(a0) {
		n = len(a0)
	}
	if n > len(a1) {
		n = len(a1)
	}
	if n > len(a2) {
		n = len(a2)
	}
	if n > len(a3) {
		n = len(a3)
	}
	if n > len(b) {
		n = len(b)
	}
	if n == 0 {
		return
	}
	fmaddPlusOne4Into(o0[:n], o1[:n], o2[:n], o3[:n], a0[:n], a1[:n], a2[:n], a3[:n], b[:n])
}

// Scalar fallbacks (used by generic/arm64 and amd64 non-AVX2).
func fmaddIntoScalar(out, a, b []float32) {
	n := len(out)
	i := 0
	for ; i+15 < n; i += 16 {
		out[i] += a[i] * b[i]
		out[i+1] += a[i+1] * b[i+1]
		out[i+2] += a[i+2] * b[i+2]
		out[i+3] += a[i+3] * b[i+3]
		out[i+4] += a[i+4] * b[i+4]
		out[i+5] += a[i+5] * b[i+5]
		out[i+6] += a[i+6] * b[i+6]
		out[i+7] += a[i+7] * b[i+7]
		out[i+8] += a[i+8] * b[i+8]
		out[i+9] += a[i+9] * b[i+9]
		out[i+10] += a[i+10] * b[i+10]
		out[i+11] += a[i+11] * b[i+11]
		out[i+12] += a[i+12] * b[i+12]
		out[i+13] += a[i+13] * b[i+13]
		out[i+14] += a[i+14] * b[i+14]
		out[i+15] += a[i+15] * b[i+15]
	}
	for ; i+7 < n; i += 8 {
		out[i] += a[i] * b[i]
		out[i+1] += a[i+1] * b[i+1]
		out[i+2] += a[i+2] * b[i+2]
		out[i+3] += a[i+3] * b[i+3]
		out[i+4] += a[i+4] * b[i+4]
		out[i+5] += a[i+5] * b[i+5]
		out[i+6] += a[i+6] * b[i+6]
		out[i+7] += a[i+7] * b[i+7]
	}
	for ; i < n; i++ {
		out[i] += a[i] * b[i]
	}
}

func mul2IntoScalar(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bi := b[i]
		o0[i] = a0[i] * bi
		o1[i] = a1[i] * bi
	}
}

func fmadd2IntoScalar(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bi := b[i]
		o0[i] += a0[i] * bi
		o1[i] += a1[i] * bi
	}
}

func fmaddPlusOne2IntoScalar(o0, o1, a0, a1, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bp1 := b[i] + 1
		o0[i] += a0[i] * bp1
		o1[i] += a1[i] * bp1
	}
}

func mul4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bi := b[i]
		o0[i] = a0[i] * bi
		o1[i] = a1[i] * bi
		o2[i] = a2[i] * bi
		o3[i] = a3[i] * bi
	}
}

func fmadd4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bi := b[i]
		o0[i] += a0[i] * bi
		o1[i] += a1[i] * bi
		o2[i] += a2[i] * bi
		o3[i] += a3[i] * bi
	}
}

func fmaddPlusOne4IntoScalar(o0, o1, o2, o3, a0, a1, a2, a3, b []float32) {
	n := len(o0)
	for i := 0; i < n; i++ {
		bp1 := b[i] + 1
		o0[i] += a0[i] * bp1
		o1[i] += a1[i] * bp1
		o2[i] += a2[i] * bp1
		o3[i] += a3[i] * bp1
	}
}

func fmaddPlusOneIntoScalar(out, a, b []float32) {
	n := len(out)
	i := 0
	for ; i+15 < n; i += 16 {
		out[i] += a[i]*b[i] + a[i]
		out[i+1] += a[i+1]*b[i+1] + a[i+1]
		out[i+2] += a[i+2]*b[i+2] + a[i+2]
		out[i+3] += a[i+3]*b[i+3] + a[i+3]
		out[i+4] += a[i+4]*b[i+4] + a[i+4]
		out[i+5] += a[i+5]*b[i+5] + a[i+5]
		out[i+6] += a[i+6]*b[i+6] + a[i+6]
		out[i+7] += a[i+7]*b[i+7] + a[i+7]
		out[i+8] += a[i+8]*b[i+8] + a[i+8]
		out[i+9] += a[i+9]*b[i+9] + a[i+9]
		out[i+10] += a[i+10]*b[i+10] + a[i+10]
		out[i+11] += a[i+11]*b[i+11] + a[i+11]
		out[i+12] += a[i+12]*b[i+12] + a[i+12]
		out[i+13] += a[i+13]*b[i+13] + a[i+13]
		out[i+14] += a[i+14]*b[i+14] + a[i+14]
		out[i+15] += a[i+15]*b[i+15] + a[i+15]
	}
	for ; i+7 < n; i += 8 {
		out[i] += a[i]*b[i] + a[i]
		out[i+1] += a[i+1]*b[i+1] + a[i+1]
		out[i+2] += a[i+2]*b[i+2] + a[i+2]
		out[i+3] += a[i+3]*b[i+3] + a[i+3]
		out[i+4] += a[i+4]*b[i+4] + a[i+4]
		out[i+5] += a[i+5]*b[i+5] + a[i+5]
		out[i+6] += a[i+6]*b[i+6] + a[i+6]
		out[i+7] += a[i+7]*b[i+7] + a[i+7]
	}
	for ; i < n; i++ {
		out[i] += a[i]*b[i] + a[i]
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
