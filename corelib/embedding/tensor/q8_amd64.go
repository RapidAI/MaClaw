//go:build amd64

package tensor

import "unsafe"

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
	if hasAVX2andFMA && nBlocks > 0 {
		dequantRowScaledAVX2(dst, data, scales, rowOff, nBlocks)
		return
	}
	sc := unsafe.Slice(scales, nBlocks)
	dequantRowScaledScalar(dst, data, sc, rowOff, nBlocks)
}

func dequantRowScaledDual(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int) {
	if hasAVX2andFMA && nBlocks > 0 && len(dst0) >= nBlocks*q8BlockSize && len(dst1) >= nBlocks*q8BlockSize {
		dequantRowScaledDualAVX2(dst0, dst1, data, scales0, scales1, rowOff0, rowOff1, nBlocks)
		return
	}
	dequantRowScaledASM(dst0, data, scales0, rowOff0, nBlocks)
	dequantRowScaledASM(dst1, data, scales1, rowOff1, nBlocks)
}
