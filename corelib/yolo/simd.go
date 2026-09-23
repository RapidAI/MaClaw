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

// bufPools buckets recycled scratch buffers by capacity (powers of two,
// 64K floats and up). Conv layers run sequentially, so pooled buffers are
// reused immediately by the next layer of the same shape.
var bufPools [12]sync.Pool

func bufBucket(n int) int {
	b := 0
	for c := 1 << 16; c < n && b < len(bufPools)-1; c <<= 1 {
		b++
	}
	return b
}

// getBuf returns a zeroed scratch buffer of at least n floats.
func getBuf(n int) []float32 {
	if v := bufPools[bufBucket(n)].Get(); v != nil {
		s := v.([]float32)
		if cap(s) >= n {
			s = s[:n]
			clear(s)
			return s
		}
	}
	return make([]float32, n)
}

func putBuf(b []float32) {
	if cap(b) < 1<<16 {
		return // tiny buffers: not worth pooling
	}
	bufPools[bufBucket(cap(b))].Put(b[:0])
}

// getBufUnzeroed returns a pooled scratch buffer WITHOUT clearing it.
// Callers must overwrite every element before reading any (im2col writes
// its full output including explicit zeros, so the clear in getBuf is pure
// overhead there — the col buffer can be megabytes per conv).
func getBufUnzeroed(n int) []float32 {
	if v := bufPools[bufBucket(n)].Get(); v != nil {
		s := v.([]float32)
		if cap(s) >= n {
			return s[:n]
		}
	}
	return make([]float32, n)
}

// ── Matmul: the single hottest path (~85% of inference time) ──

// matmulConv computes the Conv2d output: C[m,n] = dot(W[m,:], col[:,n]) + bias[m]
// where W is [M, K] (weight, row-major) and col is [K, N] (im2col output, row-major).
//
// On AVX2 machines it dispatches to gemmF32, which reads col directly in
// [K,N] layout (no transpose) with a register-blocked 6x16 micro-kernel and a
// fused bias epilogue. Otherwise it falls back to the legacy path: transpose
// col to [N, K] once, then use vek32.Dot for each (m, n) pair.
func matmulConv(W []float32, col []float32, bias []float32, out []float32, M, K, N int) {
	if hasAVX2FMA {
		gemmF32(out, W, col, bias, M, K, N)
		return
	}
	matmulConvLegacy(W, col, bias, out, M, K, N)
}

// matmulConvLegacy is the portable (pre-AVX512) matmul: transpose col to
// [N, K] once, then use vek32.Dot for each (m, n) pair. The transpose is
// parallelized and the matmul is parallelized across M.
//
// For small matrices (1x1 conv), skip parallelism to avoid goroutine overhead.
func matmulConvLegacy(W []float32, col []float32, bias []float32, out []float32, M, K, N int) {
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
// im2colParallelLdb is im2colParallel with an explicit physical row stride
// (ldb >= outH*outW) for the [K][N] output; the tail of each row is left
// untouched (the GEMM never reads it).
func im2colParallelLdb(input *Tensor, n, kh, kw, stride, padding, outH, outW int, dst []float32, ldb int) {
	im2colParallelBody(input, n, kh, kw, stride, padding, outH, outW, dst, ldb)
}

func im2colParallel(input *Tensor, n, kh, kw, stride, padding, outH, outW int, dst []float32) {
	im2colParallelBody(input, n, kh, kw, stride, padding, outH, outW, dst, outH*outW)
}

func im2colParallelBody(input *Tensor, n, kh, kw, stride, padding, outH, outW int, dst []float32, ldb int) {
	inC := input.Shape[1]
	inH := input.Shape[2]
	inW := input.Shape[3]
	batchOff := n * input.Stride[0]

	nWorkers := runtime.NumCPU()
	if nWorkers > inC {
		nWorkers = inC
	}
	if inC <= 4 || ldb != outH*outW {
		im2colDirectLdb(input.Data, batchOff, input.Stride[1], inC, inH, inW, kh, kw, stride, padding, outH, outW, dst, ldb)
		return
	}

	var wg sync.WaitGroup
	chansPerWorker := (inC + nWorkers - 1) / nWorkers
	s2p3 := kh == 3 && kw == 3 && stride == 2 && padding == 1
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
			dstOff := cs * kh * kw * ldb
			chanBase := batchOff + cs*input.Stride[1]
			if s2p3 {
				im2colS2P3Ldb(input.Data, chanBase, input.Stride[1], ce-cs, inH, inW, outH, outW, dst[dstOff:], ldb)
				return
			}
			im2colDirectLdb(input.Data, chanBase, input.Stride[1], ce-cs, inH, inW, kh, kw, stride, padding, outH, outW, dst[dstOff:], ldb)
		}(cStart, cEnd)
	}
	wg.Wait()
}

// im2colDirectLdb is im2colDirect with an explicit physical row stride ldb
// for the [K][N] output matrix (tail of each row untouched).
func im2colDirectLdb(data []float32, chanBase, chanStride, nChans, inH, inW, kh, kw, stride, padding, outH, outW int, dst []float32, ldb int) {
	for ic := 0; ic < nChans; ic++ {
		chanOff := chanBase + ic*chanStride
		for ky := 0; ky < kh; ky++ {
			ohA := 0
			if v := padding - ky; v > 0 {
				ohA = (v + stride - 1) / stride
			}
			ohB := outH
			if v := inH - 1 + padding - ky; v >= 0 {
				if u := v/stride + 1; u < ohB {
					ohB = u
				}
			} else {
				ohB = 0
			}
			for kx := 0; kx < kw; kx++ {
				d := kx - padding
				owA := 0
				if v := -d; v > 0 {
					owA = (v + stride - 1) / stride
				}
				owB := outW
				if v := inW - 1 - d; v >= 0 {
					if u := v/stride + 1; u < owB {
						owB = u
					}
				} else {
					owB = 0
				}
				if owA > outW {
					owA = outW
				}
				if owB < owA {
					owB = owA
				}
				base := (ic*kh*kw + ky*kw + kx) * ldb
				for oh := 0; oh < outH; oh++ {
					row := dst[base+oh*outW : base+oh*outW+outW]
					if oh < ohA || oh >= ohB {
						clear(row)
						continue
					}
					src := data[chanOff+(oh*stride-padding+ky)*inW:]
					if owA > 0 {
						clear(row[:owA])
					}
					n := owB - owA
					if stride == 1 {
						copy(row[owA:owB], src[owA+d:owA+d+n])
					} else if stride == 2 {
						s0 := owA*stride + d
						deinterleave2Stride(row[owA:owB], src[s0:], n, inW-s0)
					} else {
						s0 := owA*stride + d
						for j := 0; j < n; j++ {
							row[owA+j] = src[s0+j*stride]
						}
					}
					if owB < outW {
						clear(row[owB:])
					}
				}
			}
		}
	}
}

// im2colS2P3Ldb is im2colS2P3 with an explicit physical row stride.
func im2colS2P3Ldb(data []float32, chanBase, chanStride, nChans, inH, inW, outH, outW int, dst []float32, ldb int) {
	evenLen := (inW + 1) / 2
	oddLen := inW / 2
	even := make([]float32, evenLen)
	odd := make([]float32, oddLen)
	rowFloats := 3 * 3 * ldb
	for ic := 0; ic < nChans; ic++ {
		chanOff := chanBase + ic*chanStride
		icOff := ic * rowFloats
		for ky := 0; ky < 3; ky++ {
			for oh := 0; oh < outH; oh++ {
				ih := 2*oh - 1 + ky
				base := icOff + (ky*3)*ldb + oh*outW
				if ih < 0 || ih >= inH {
					clear(dst[base : base+outW])
					clear(dst[base+ldb : base+ldb+outW])
					clear(dst[base+2*ldb : base+2*ldb+outW])
					continue
				}
				row := data[chanOff+ih*inW:]
				if oddLen > 0 {
					deinterleave2Stride(even, row, evenLen, inW)
					deinterleave2Stride(odd, row[1:], oddLen, inW-1)
				} else {
					even[0] = row[0]
				}
				r0 := dst[base : base+outW]
				r1 := dst[base+ldb : base+ldb+outW]
				r2 := dst[base+2*ldb : base+2*ldb+outW]
				copy(r1, even)
				copy(r2, odd)
				if oddLen < outW {
					clear(r2[oddLen:])
				}
				r0[0] = 0
				copy(r0[1:], odd)
			}
		}
	}
}

// im2colS2P3 is the specialized im2col for 3x3 stride-2 padding-1 convolutions
// (all downsampling convs in the model). Instead of nine per-element strided
// gathers per output row, each input row is deinterleaved ONCE into
// even/odd sub-rows (vectorized), and every output row becomes three
// contiguous copies with at most one leading zero.
//
// Bounds: with inW >= 1, outW = (inW+2-3)/2+1 guarantees
//   kx=0: dst[ow] = odd[ow-1] for ow >= 1, dst[0] = 0   (iw = 2*ow-1)
//   kx=1: dst[ow] = even[ow]                            (iw = 2*ow)
//   kx=2: dst[ow] = odd[ow]                             (iw = 2*ow+1)
// with even[j] = x[2j] (j < (inW+1)/2) and odd[j] = x[2j+1] (j < inW/2).
func im2colS2P3(data []float32, chanBase, chanStride, nChans, inH, inW, outH, outW int, dst []float32) {
	evenLen := (inW + 1) / 2
	oddLen := inW / 2
	even := make([]float32, evenLen)
	odd := make([]float32, oddLen)
	rowFloats := 3 * 3 * outH * outW
	for ic := 0; ic < nChans; ic++ {
		chanOff := chanBase + ic*chanStride
		icOff := ic * rowFloats
		for ky := 0; ky < 3; ky++ {
			for oh := 0; oh < outH; oh++ {
				ih := 2*oh - 1 + ky
				base := icOff + (ky*3)*outH*outW + oh*outW
				if ih < 0 || ih >= inH {
					clear(dst[base : base+outW])
					clear(dst[base+outH*outW : base+outH*outW+outW])
					clear(dst[base+2*outH*outW : base+2*outH*outW+outW])
					continue
				}
				row := data[chanOff+ih*inW:]
				if oddLen > 0 {
					deinterleave2Stride(even, row, evenLen, inW)
					deinterleave2Stride(odd, row[1:], oddLen, inW-1)
				} else {
					// inW == 1: only even[0] = x[0]
					even[0] = row[0]
				}
				r0 := dst[base : base+outW : base+outW]
				r1 := dst[base+outH*outW : base+outH*outW+outW]
				r2 := dst[base+2*outH*outW : base+2*outH*outW+outW]
				// kx = 1: evens
				copy(r1, even)
				// kx = 2: odds (odd inW leaves the last output zero-padded)
				copy(r2, odd)
				if oddLen < outW {
					clear(r2[oddLen:])
				}
				// kx = 0: [0, odds...]
				r0[0] = 0
				copy(r0[1:], odd)
			}
		}
	}
}

// im2colDirect is the inner loop of im2col, operating on a contiguous range of channels.
// chanBase is the offset to the first channel in data (batchOff + startChan*chanStride).
//
// The per-tap bounds checks are hoisted out of the pixel loop: for each
// (ky, kx) tap the valid output ranges [ohA, ohB) and [owA, owB) are computed
// once. Interior pixels then run branch-free: stride-1 taps are a contiguous
// memmove, stride-2 taps a strided gather (or an AVX512 VPERMT2PS
// deinterleave when available). Out-of-range pixels are explicit zeros.
func im2colDirect(data []float32, chanBase, chanStride, nChans, inH, inW, kh, kw, stride, padding, outH, outW int, dst []float32) {
	idx := 0
	for ic := 0; ic < nChans; ic++ {
		chanOff := chanBase + ic*chanStride
		for ky := 0; ky < kh; ky++ {
			// Valid output rows: ih = oh*stride - padding + ky ∈ [0, inH).
			ohA := 0
			if v := padding - ky; v > 0 {
				ohA = (v + stride - 1) / stride
			}
			ohB := outH
			if v := inH - 1 + padding - ky; v >= 0 {
				if u := v/stride + 1; u < ohB {
					ohB = u
				}
			} else {
				ohB = 0
			}
			for kx := 0; kx < kw; kx++ {
				// Valid output cols: iw = ow*stride + d ∈ [0, inW), d = kx - padding.
				d := kx - padding
				owA := 0
				if v := -d; v > 0 {
					owA = (v + stride - 1) / stride
				}
				owB := outW
				if v := inW - 1 - d; v >= 0 {
					if u := v/stride + 1; u < owB {
						owB = u
					}
				} else {
					owB = 0
				}
				if owA > outW {
					owA = outW
				}
				if owB < owA {
					owB = owA
				}
				for oh := 0; oh < outH; oh++ {
					row := dst[idx : idx+outW]
					idx += outW
					if oh < ohA || oh >= ohB {
						clear(row)
						continue
					}
					src := data[chanOff+(oh*stride-padding+ky)*inW:]
					if owA > 0 {
						clear(row[:owA])
					}
					n := owB - owA
					if stride == 1 {
						copy(row[owA:owB], src[owA+d:owA+d+n])
					} else if stride == 2 {
						s0 := owA*stride + d
						deinterleave2Stride(row[owA:owB], src[s0:], n, inW-s0)
					} else {
						s0 := owA*stride + d
						for j := 0; j < n; j++ {
							row[owA+j] = src[s0+j*stride]
						}
					}
					if owB < outW {
						clear(row[owB:])
					}
				}
			}
		}
	}
}
