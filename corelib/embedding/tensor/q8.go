package tensor

import (
	"encoding/binary"
	"math"
	"runtime"
	"sync"
	"sync/atomic"

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

// matMulMaxParallel controls internal parallelism for MatMul operations.
// 0 = default (NumCPU). Set to 1 to force single-threaded (for batch-level parallelism).
var matMulMaxParallel int32

// SetMatMulMaxParallel sets the internal parallelism limit.
func SetMatMulMaxParallel(n int) { atomic.StoreInt32(&matMulMaxParallel, int32(n)) }

func getMatMulWorkers() int {
	n := int(atomic.LoadInt32(&matMulMaxParallel))
	if n <= 0 { return runtime.NumCPU() }
	return n
}

// MatMulQ8 computes out = A @ B^T where A is [M, K] float32 and B is Q8_0 [N, K].
// Result out is [M, N]. Each B row is dequantized into a temporary buffer, then
// vek32.Dot (AVX2/NEON SIMD) computes the dot product against the A row.
//
// Performance: dequant-then-SIMD-dot is faster than fused scalar dequant-dot
// because AVX2/NEON processes 8 floats per cycle. The dequant buffer is reused
// across iterations to avoid allocation overhead.
func MatMulQ8(out, a []float32, b *Q8Tensor, M, N, K int) {
	nCPU := getMatMulWorkers()
	if nCPU > 1 && N*K > 4096 {
		if M > 1 {
			matMulQ8Parallel(out, a, b, M, N, K)
			return
		}
		matMulQ8ParallelN(out, a, b, M, N, K)
		return
	}
	buf := make([]float32, K)
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
	nWorkers := getMatMulWorkers()
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

// matMulQ8ParallelN parallelizes across the N (output) dimension.
// This is critical when M=1 (single-token or short-sequence inference)
// where M-parallel would only use 1 goroutine.
func matMulQ8ParallelN(out, a []float32, b *Q8Tensor, M, N, K int) {
	nWorkers := getMatMulWorkers()
	if nWorkers > N {
		nWorkers = N
	}
	nBlocks := K / q8BlockSize
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
			buf := make([]float32, K)
			for m := 0; m < M; m++ {
				aRow := a[m*K : m*K+K]
				for n := ns; n < ne; n++ {
					dequantRowInto(b.Data, n, nBlocks, buf)
					out[m*N+n] = vek32.Dot(aRow, buf)
				}
			}
		}(nStart, nEnd)
	}
	wg.Wait()
}

// dequantRowInto dequantizes a Q8_0 row into dst (len >= nBlocks*32).
// This is the hot path — called once per dot product.
// Uses 8x unrolled loop to help the compiler auto-vectorize.
func dequantRowInto(data []byte, row int, nBlocks int, dst []float32) {
	rowOff := row * nBlocks * q8BlockBytes
	for b := 0; b < nBlocks; b++ {
		off := rowOff + b*q8BlockBytes
		scale := float16to32(binary.LittleEndian.Uint16(data[off:]))
		base := b * q8BlockSize
		qOff := off + 2
		// 8x unrolled for better ILP and auto-vectorization
		for i := 0; i < q8BlockSize; i += 8 {
			dst[base+i] = scale * float32(int8(data[qOff+i]))
			dst[base+i+1] = scale * float32(int8(data[qOff+i+1]))
			dst[base+i+2] = scale * float32(int8(data[qOff+i+2]))
			dst[base+i+3] = scale * float32(int8(data[qOff+i+3]))
			dst[base+i+4] = scale * float32(int8(data[qOff+i+4]))
			dst[base+i+5] = scale * float32(int8(data[qOff+i+5]))
			dst[base+i+6] = scale * float32(int8(data[qOff+i+6]))
			dst[base+i+7] = scale * float32(int8(data[qOff+i+7]))
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

// QuantizeToQ8 quantizes a float32 weight matrix [rows, cols] to Q8_0 format.
// cols must be a multiple of 32. Returns a Q8Tensor suitable for MatMulQ8.
// This enables runtime quantization of F32 weights for reduced memory and
// bandwidth during inference.
func QuantizeToQ8(data []float32, rows, cols int) *Q8Tensor {
	nBlocks := cols / q8BlockSize
	totalBytes := rows * nBlocks * q8BlockBytes
	raw := make([]byte, totalBytes)

	for r := 0; r < rows; r++ {
		rowData := data[r*cols : (r+1)*cols]
		rowOff := r * nBlocks * q8BlockBytes
		for b := 0; b < nBlocks; b++ {
			blockData := rowData[b*q8BlockSize : (b+1)*q8BlockSize]
			// Find absmax for scale
			var amax float32
			for _, v := range blockData {
				av := float32(math.Abs(float64(v)))
				if av > amax {
					amax = av
				}
			}
			scale := amax / 127.0
			off := rowOff + b*q8BlockBytes
			// Store scale as float16
			binary.LittleEndian.PutUint16(raw[off:], float32to16(scale))
			// Quantize values
			if scale > 0 {
				invScale := 127.0 / amax
				for i := 0; i < q8BlockSize; i++ {
					q := int(math.Round(float64(blockData[i] * invScale)))
					if q > 127 {
						q = 127
					} else if q < -128 {
						q = -128
					}
					raw[off+2+i] = byte(int8(q))
				}
			} else {
				// All zeros
				for i := 0; i < q8BlockSize; i++ {
					raw[off+2+i] = 0
				}
			}
		}
	}
	return &Q8Tensor{Data: raw, Rows: rows, Cols: cols}
}

// float32to16 converts float32 to IEEE 754 half-precision.
func float32to16(f float32) uint16 {
	bits := math.Float32bits(f)
	sign := uint16((bits >> 16) & 0x8000)
	exp := int((bits>>23)&0xff) - 127
	mant := bits & 0x7fffff

	switch {
	case exp > 15:
		return sign | 0x7c00 // infinity
	case exp < -14:
		return sign // zero (flush subnormals)
	default:
		return sign | uint16(exp+15)<<10 | uint16(mant>>13)
	}
}
