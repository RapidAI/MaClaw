package yolo

// SIMD-accelerated operations for YOLO inference.
// Uses github.com/viterin/vek/vek32 for AVX2/NEON SIMD dot product.
// vek32 handles CPU feature detection internally (AVX2 → SSE → scalar).

import (
	"runtime"
	"sync"

	"github.com/viterin/vek/vek32"
)

// ── Buffer pool for reducing GC pressure ──

// getBuf and putBuf are simple wrappers that allocate fresh slices.
// The Go runtime's escape analysis and GC handle short-lived large slices
// efficiently. sync.Pool adds overhead that outweighs the benefit here
// because conv layers run sequentially (no concurrent reuse).
func getBuf(n int) []float32 {
	return make([]float32, n)
}

func putBuf(b []float32) {
	// no-op: let GC collect
}

// ── Matmul: the single hottest path (~85% of inference time) ──

// matmulConv computes the Conv2d output: C[m,n] = dot(W[m,:], col[:,n]) + bias[m]
// where W is [M, K] (weight, row-major) and col is [K, N] (im2col output, row-major).
//
// The challenge: col is [K, N] but vek32.Dot needs contiguous rows.
// Strategy: transpose col to [N, K] once, then use vek32.Dot for each (m, n) pair.
// The transpose is parallelized and the matmul is parallelized across M.
//
// For small matrices (1x1 conv), skip parallelism to avoid goroutine overhead.
func matmulConv(W []float32, col []float32, bias []float32, out []float32, M, K, N int) {
	nWorkers := runtime.NumCPU()
	if nWorkers > M {
		nWorkers = M
	}

	if M <= 4 || M*N < 1024 {
		// Small: transpose + sequential dot
		colT := make([]float32, N*K)
		for r := 0; r < K; r++ {
			srcOff := r * N
			for c := 0; c < N; c++ {
				colT[c*K+r] = col[srcOff+c]
			}
		}
		for m := 0; m < M; m++ {
			wRow := W[m*K : m*K+K]
			b := bias[m]
			for n := 0; n < N; n++ {
				out[m*N+n] = vek32.Dot(wRow, colT[n*K:n*K+K]) + b
			}
		}
		return
	}

	// Large: parallel transpose + parallel SIMD matmul.
	colT := make([]float32, N*K)
	if K > 64 {
		var twg sync.WaitGroup
		tWorkers := nWorkers
		if tWorkers > K {
			tWorkers = K
		}
		rPerW := (K + tWorkers - 1) / tWorkers
		for w := 0; w < tWorkers; w++ {
			rStart := w * rPerW
			rEnd := rStart + rPerW
			if rEnd > K {
				rEnd = K
			}
			if rStart >= rEnd {
				break
			}
			twg.Add(1)
			go func(rs, re int) {
				defer twg.Done()
				for r := rs; r < re; r++ {
					srcOff := r * N
					for c := 0; c < N; c++ {
						colT[c*K+r] = col[srcOff+c]
					}
				}
			}(rStart, rEnd)
		}
		twg.Wait()
	} else {
		for r := 0; r < K; r++ {
			srcOff := r * N
			for c := 0; c < N; c++ {
				colT[c*K+r] = col[srcOff+c]
			}
		}
	}

	var wg sync.WaitGroup
	rowsPerWorker := (M + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		mStart := w * rowsPerWorker
		mEnd := mStart + rowsPerWorker
		if mEnd > M {
			mEnd = M
		}
		if mStart >= mEnd {
			break
		}
		wg.Add(1)
		go func(ms, me int) {
			defer wg.Done()
			for m := ms; m < me; m++ {
				wRow := W[m*K : m*K+K]
				b := bias[m]
				outOff := m * N
				for n := 0; n < N; n++ {
					out[outOff+n] = vek32.Dot(wRow, colT[n*K:n*K+K]) + b
				}
			}
		}(mStart, mEnd)
	}
	wg.Wait()
}

// transposeParallel transposes a [rows, cols] matrix to [cols, rows].
// Allocates a new buffer. For hot paths, use transposeInto with a pooled buffer.
func transposeParallel(src []float32, rows, cols int) []float32 {
	dst := make([]float32, rows*cols)
	transposeInto(src, dst, rows, cols)
	return dst
}

// transposeInto transposes src [rows, cols] into pre-allocated dst [cols, rows].
// Parallelized across output columns for large matrices.
func transposeInto(src, dst []float32, rows, cols int) {
	// Simple sequential transpose — goroutine overhead for parallel transpose
	// exceeds the benefit for the matrix sizes in YOLO inference.
	for r := 0; r < rows; r++ {
		srcOff := r * cols
		for c := 0; c < cols; c++ {
			dst[c*rows+r] = src[srcOff+c]
		}
	}
}

// transposeBlock is unused — kept for reference.
func transposeBlock(src, dst []float32, rows, cols, cStart, cEnd int) {}

// ── Element-wise SIMD ops ──

// addInplace computes dst[i] += src[i] using SIMD.
func addInplace(dst, src []float32) {
	vek32.Add_Inplace(dst, src)
}

// ── Parallel im2col ──

// im2colParallel unfolds input patches with goroutine parallelism across channels.
func im2colParallel(input *Tensor, n, kh, kw, stride, padding, outH, outW int, dst []float32) {
	inC := input.Shape[1]
	inH := input.Shape[2]
	inW := input.Shape[3]
	batchOff := n * input.Stride[0]

	nWorkers := runtime.NumCPU()
	if nWorkers > inC {
		nWorkers = inC
	}
	if inC <= 4 {
		im2colDirect(input.Data, batchOff, input.Stride[1], inC, inH, inW, kh, kw, stride, padding, outH, outW, dst)
		return
	}

	var wg sync.WaitGroup
	chansPerWorker := (inC + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		cStart := w * chansPerWorker
		cEnd := cStart + chansPerWorker
		if cEnd > inC {
			cEnd = inC
		}
		if cStart >= cEnd {
			break
		}
		wg.Add(1)
		go func(cs, ce int) {
			defer wg.Done()
			dstOff := cs * kh * kw * outH * outW
			chanBase := batchOff + cs*input.Stride[1]
			im2colDirect(input.Data, chanBase, input.Stride[1], ce-cs, inH, inW, kh, kw, stride, padding, outH, outW, dst[dstOff:])
		}(cStart, cEnd)
	}
	wg.Wait()
}

// im2colDirect is the inner loop of im2col, operating on a contiguous range of channels.
// chanBase is the offset to the first channel in data (batchOff + startChan*chanStride).
func im2colDirect(data []float32, chanBase, chanStride, nChans, inH, inW, kh, kw, stride, padding, outH, outW int, dst []float32) {
	idx := 0
	for ic := 0; ic < nChans; ic++ {
		chanOff := chanBase + ic*chanStride
		for ky := 0; ky < kh; ky++ {
			for kx := 0; kx < kw; kx++ {
				for oh := 0; oh < outH; oh++ {
					ih := oh*stride - padding + ky
					if ih < 0 || ih >= inH {
						for ow := 0; ow < outW; ow++ {
							dst[idx] = 0
							idx++
						}
						continue
					}
					rowOff := chanOff + ih*inW
					for ow := 0; ow < outW; ow++ {
						iw := ow*stride - padding + kx
						if iw >= 0 && iw < inW {
							dst[idx] = data[rowOff+iw]
						} else {
							dst[idx] = 0
						}
						idx++
					}
				}
			}
		}
	}
}
