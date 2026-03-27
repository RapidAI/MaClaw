// Package tensor provides optimized tensor operations for transformer inference.
// Uses github.com/viterin/vek/vek32 for AVX2/NEON SIMD acceleration on hot paths.
package tensor

import (
	"math"
	"runtime"
	"sync"

	"github.com/viterin/vek/vek32"
)

// MatMul computes out = A @ B^T where A is [M, K] and B is [N, K] (row-major).
// Result out is [M, N]. Uses SIMD-accelerated dot product for each row pair.
func MatMul(out, a, b []float32, M, N, K int) {
	if M > 1 && M*K > 4096 && runtime.NumCPU() > 1 {
		matMulParallel(out, a, b, M, N, K)
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
	nWorkers := runtime.NumCPU()
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

// RMSNorm computes RMS normalization: out[i] = x[i] / rms(x) * weight[i].
// out and x may alias (in-place normalization).
func RMSNorm(out, x, weight []float32, eps float32) {
	n := len(x)
	ss := vek32.Dot(x, x) // sum of squares via SIMD dot product
	scale := 1.0 / float32(math.Sqrt(float64(ss/float32(n)+eps)))
	for i := 0; i < n; i++ {
		out[i] = x[i] * scale * weight[i]
	}
}

// SiLU computes the SiLU activation: x * sigmoid(x), in-place.
func SiLU(x []float32) {
	for i := range x {
		x[i] = x[i] / (1.0 + float32(math.Exp(float64(-x[i]))))
	}
}

// ElemMul computes element-wise multiplication: out[i] = a[i] * b[i].
// out may alias a or b.
func ElemMul(out, a, b []float32) {
	for i := range out {
		out[i] = a[i] * b[i]
	}
}

// Add computes element-wise addition: out[i] = a[i] + b[i].
// out may alias a or b (safe for in-place residual add).
func Add(out, a, b []float32) {
	for i := range out {
		out[i] = a[i] + b[i]
	}
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
	// exp — vek32 has Exp for float32
	for i := range x {
		x[i] = float32(math.Exp(float64(x[i])))
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

// L2Normalize normalizes a vector to unit length, in-place.
func L2Normalize(v []float32) {
	norm := vek32.Norm(v)
	if norm == 0 {
		return
	}
	vek32.MulNumber_Inplace(v, 1.0/norm)
}
