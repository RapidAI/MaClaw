//go:build amd64

package tensor

import (
	"sync"
	"unsafe"
)

// --- AVX2+FMA assembly (Haswell+) ---

//go:noescape
func dotQ8RowAVX2(a []float32, data []byte, rowOff, nBlocks int) float32

//go:noescape
func dequantRowIntoAVX2(dst []float32, data []byte, rowOff, nBlocks int)

//go:noescape
func dequantRowScaledAVX2(dst []float32, data []byte, scales *float32, rowOff, nBlocks int)

//go:noescape
func dequantRowScaledDualAVX2(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

// --- AVX1 assembly (Sandy/Ivy Bridge) ---

//go:noescape
func dotQ8RowAVX1(a []float32, data []byte, rowOff, nBlocks int) float32

//go:noescape
func dequantRowIntoAVX1(dst []float32, data []byte, rowOff, nBlocks int)

// --- Runtime dispatch ---

func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32 {
	if hasAVX2andFMA {
		return dotQ8RowAVX2(a, data, rowOff, nBlocks)
	}
	if hasAVX {
		return dotQ8RowAVX1(a, data, rowOff, nBlocks)
	}
	return dotQ8RowScalar(a, data, rowOff, nBlocks)
}

func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int) {
	if hasAVX2andFMA {
		dequantRowIntoAVX2(dst, data, rowOff, nBlocks)
		return
	}
	if hasAVX {
		dequantRowIntoAVX1(dst, data, rowOff, nBlocks)
		return
	}
	dequantRowIntoScalar(dst, data, rowOff, nBlocks)
}

func dequantRowScaledASM(dst []float32, data []byte, scales *float32, rowOff, nBlocks int) {
	if nBlocks > 0 {
		if hasAVX512 {
			dequantRowScaledAVX512(dst, data, scales, rowOff, nBlocks)
			return
		}
		if hasAVX2andFMA {
			dequantRowScaledAVX2(dst, data, scales, rowOff, nBlocks)
			return
		}
	}
	sc := unsafe.Slice(scales, nBlocks)
	dequantRowScaledScalar(dst, data, sc, rowOff, nBlocks)
}

func dequantRowScaledDual(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int) {
	if nBlocks > 0 && len(dst0) >= nBlocks*q8BlockSize && len(dst1) >= nBlocks*q8BlockSize {
		if hasAVX512 {
			dequantRowScaledDualAVX512(dst0, dst1, data, scales0, scales1, rowOff0, rowOff1, nBlocks)
			return
		}
		if hasAVX2andFMA {
			dequantRowScaledDualAVX2(dst0, dst1, data, scales0, scales1, rowOff0, rowOff1, nBlocks)
			return
		}
	}
	dequantRowScaledASM(dst0, data, scales0, rowOff0, nBlocks)
	dequantRowScaledASM(dst1, data, scales1, rowOff1, nBlocks)
}

func dequantRowScaledTriple(dst0, dst1, dst2 []float32, data []byte, scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2, nBlocks int) {
	if nBlocks > 0 && len(dst0) >= nBlocks*q8BlockSize && len(dst1) >= nBlocks*q8BlockSize && len(dst2) >= nBlocks*q8BlockSize {
		if hasAVX512 {
			dequantRowScaledTripleAVX512(dst0, dst1, dst2, data, scales0, scales1, scales2, rowOff0, rowOff1, rowOff2, nBlocks)
			return
		}
	}
	dequantRowScaledDual(dst0, dst1, data, scales0, scales1, rowOff0, rowOff1, nBlocks)
	dequantRowScaledASM(dst2, data, scales2, rowOff2, nBlocks)
}

//go:noescape
func dequantRowScaledAVX512(dst []float32, data []byte, scales *float32, rowOff, nBlocks int)

//go:noescape
func dequantRowScaledDualAVX512(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)

//go:noescape
func dequantRowScaledTripleAVX512(dst0, dst1, dst2 []float32, data []byte, scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2, nBlocks int)

//go:noescape
func prepareScalesF16x8AVX2(dst *float32, data *byte, n int)

// prepareScalesBulk: strided f16→f32 via F16C (VCVTPH2PS). Returns false if
// SIMD unavailable or n too small (caller uses scalar path).
// Large tensors (FFN/CTC weights) split into parallel 16-aligned chunks —
// PrepareScales is load-time wall clock on SenseVoice (~5% of e2e profile).
func prepareScalesBulk(dst []float32, data []byte) bool {
	n := len(dst)
	if !hasAVX2andFMA || n < 8 || len(data) < n*q8BlockBytes {
		return false
	}
	n8 := n &^ 7
	// Parallelize when ≥8K scales (~16K Q8 blocks worth of work). Align chunks
	// to 16 so each worker hits the dual PINSRW/VCVTPH2PS main path.
	const parMin = 8192
	if n8 >= parMin {
		const workers = 4
		chunk := (n8 / workers) &^ 15
		if chunk >= 16 {
			var wg sync.WaitGroup
			for w := 0; w < workers; w++ {
				start := w * chunk
				end := start + chunk
				if w == workers-1 {
					end = n8 // last worker takes remainder of n8
				}
				if start >= end {
					continue
				}
				wg.Add(1)
				go func(s, e int) {
					defer wg.Done()
					prepareScalesF16x8AVX2(&dst[s], &data[s*q8BlockBytes], e-s)
				}(start, end)
			}
			wg.Wait()
			for i := n8; i < n; i++ {
				off := i * q8BlockBytes
				dst[i] = float16to32Fast(uint16(data[off]) | uint16(data[off+1])<<8)
			}
			return true
		}
	}
	prepareScalesF16x8AVX2(&dst[0], &data[0], n8)
	for i := n8; i < n; i++ {
		off := i * q8BlockBytes
		dst[i] = float16to32Fast(uint16(data[off]) | uint16(data[off+1])<<8)
	}
	return true
}
