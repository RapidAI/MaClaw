package tensor

import (
	"encoding/binary"
	"math"
	"runtime"
	"sync"

	"github.com/viterin/vek/vek32"
)

// Q8Tensor holds a reference to Q8_0 quantized data (typically mmap-backed).
// Block layout: [scale:f16(2 bytes)][d0..d31:int8(32 bytes)] = 34 bytes per 32 elements.
// The data slice is NOT owned — it points into the mmap region.
type Q8Tensor struct {
	Data []byte // raw Q8_0 blocks
	Rows int    // number of rows (outer dimension)
	Cols int    // number of columns (inner dimension, must be multiple of 32)
}

const (
	q8BlockSize  = 32
	q8BlockBytes = 2 + q8BlockSize // 34 bytes per block
)

// DequantRow dequantizes a single row into dst (must be len >= t.Cols).
// This is used for token embedding lookup — only one row at a time.
func (t *Q8Tensor) DequantRow(row int, dst []float32) {
	cols := t.Cols
	nBlocks := cols / q8BlockSize
	rowOff := row * nBlocks * q8BlockBytes
	end := rowOff + nBlocks*q8BlockBytes
	if end > len(t.Data) {
		return // out of bounds — caller should validate row index
	}
	for b := 0; b < nBlocks; b++ {
		off := rowOff + b*q8BlockBytes
		scale := float16to32(binary.LittleEndian.Uint16(t.Data[off:]))
		base := b * q8BlockSize
		for i := 0; i < q8BlockSize; i++ {
			dst[base+i] = scale * float32(int8(t.Data[off+2+i]))
		}
	}
}

// MatMulQ8 computes out = A @ B^T where A is [M, K] float32 and B is Q8_0 [N, K].
// Result out is [M, N]. Each B row is dequantized into a temporary buffer, then
// vek32.Dot (AVX2/NEON SIMD) computes the dot product against the A row.
func MatMulQ8(out, a []float32, b *Q8Tensor, M, N, K int) {
	if M > 1 && M*K > 4096 && runtime.NumCPU() > 1 {
		matMulQ8Parallel(out, a, b, M, N, K)
		return
	}
	buf := make([]float32, K) // dequant buffer, reused across all N iterations
	nBlocks := K / q8BlockSize
	for m := 0; m < M; m++ {
		aRow := a[m*K : m*K+K]
		for n := 0; n < N; n++ {
			dequantRowInto(b.Data, n, nBlocks, buf)
			out[m*N+n] = vek32.Dot(aRow, buf)
		}
	}
}

func matMulQ8Parallel(out, a []float32, b *Q8Tensor, M, N, K int) {
	nWorkers := runtime.NumCPU()
	if nWorkers > M {
		nWorkers = M
	}
	nBlocks := K / q8BlockSize
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
			buf := make([]float32, K) // per-goroutine buffer
			for m := s; m < e; m++ {
				aRow := a[m*K : m*K+K]
				for n := 0; n < N; n++ {
					dequantRowInto(b.Data, n, nBlocks, buf)
					out[m*N+n] = vek32.Dot(aRow, buf)
				}
			}
		}(start, end)
	}
	wg.Wait()
}

// dequantRowInto dequantizes a Q8_0 row into dst (len >= nBlocks*32).
// This is the hot path — called once per dot product.
func dequantRowInto(data []byte, row int, nBlocks int, dst []float32) {
	rowOff := row * nBlocks * q8BlockBytes
	for b := 0; b < nBlocks; b++ {
		off := rowOff + b*q8BlockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		base := b * q8BlockSize
		qOff := off + 2
		// 4x unrolled dequant
		for i := 0; i < q8BlockSize; i += 4 {
			dst[base+i] = scale * float32(int8(data[qOff+i]))
			dst[base+i+1] = scale * float32(int8(data[qOff+i+1]))
			dst[base+i+2] = scale * float32(int8(data[qOff+i+2]))
			dst[base+i+3] = scale * float32(int8(data[qOff+i+3]))
		}
	}
}

// float16to32 converts IEEE 754 half-precision to float32.
func float16to32(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	mant := uint32(h) & 0x3ff
	switch {
	case exp == 0:
		if mant == 0 {
			return math.Float32frombits(sign)
		}
		for mant&0x400 == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x3ff
		fallthrough
	case exp < 31:
		return math.Float32frombits(sign | (exp+112)<<23 | mant<<13)
	default:
		return math.Float32frombits(sign | 0x7f800000 | mant<<13)
	}
}
