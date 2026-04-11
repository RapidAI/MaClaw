package tensor

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeFloat32(n int) []float32 {
	v := make([]float32, n)
	for i := range v {
		v[i] = rand.Float32()*2 - 1
	}
	return v
}

func makeQ8Data(rows, cols int) []byte {
	nBlocks := cols / q8BlockSize
	data := make([]byte, rows*nBlocks*q8BlockBytes)
	for r := 0; r < rows; r++ {
		for b := 0; b < nBlocks; b++ {
			off := (r*nBlocks + b) * q8BlockBytes
			// scale = 0.5 as float16
			binary.LittleEndian.PutUint16(data[off:], float32to16(0.5))
			for i := 0; i < q8BlockSize; i++ {
				data[off+2+i] = byte(int8(rand.Intn(255) - 128))
			}
		}
	}
	return data
}

// ---------------------------------------------------------------------------
// DotQ8Row benchmarks
// ---------------------------------------------------------------------------

func BenchmarkDotQ8Row_768(b *testing.B) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	a := makeFloat32(cols)
	data := makeQ8Data(1, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DotQ8Row(a, data, 0, nBlocks)
	}
}

func BenchmarkDotQ8RowScalar_768(b *testing.B) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	a := makeFloat32(cols)
	data := makeQ8Data(1, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dotQ8RowScalar(a, data, 0, nBlocks)
	}
}

func BenchmarkDotQ8Row_3072(b *testing.B) {
	const cols = 3072
	nBlocks := cols / q8BlockSize
	a := makeFloat32(cols)
	data := makeQ8Data(1, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DotQ8Row(a, data, 0, nBlocks)
	}
}

func BenchmarkDotQ8RowScalar_3072(b *testing.B) {
	const cols = 3072
	nBlocks := cols / q8BlockSize
	a := makeFloat32(cols)
	data := makeQ8Data(1, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dotQ8RowScalar(a, data, 0, nBlocks)
	}
}

// ---------------------------------------------------------------------------
// dequantRowInto benchmarks
// ---------------------------------------------------------------------------

func BenchmarkDequantRowInto_768(b *testing.B) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	data := makeQ8Data(1, cols)
	dst := make([]float32, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dequantRowInto(data, 0, nBlocks, dst)
	}
}

func BenchmarkDequantRowIntoScalar_768(b *testing.B) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	data := makeQ8Data(1, cols)
	dst := make([]float32, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dequantRowIntoScalar(dst, data, 0, nBlocks)
	}
}

// ---------------------------------------------------------------------------
// SiLUMul benchmarks
// ---------------------------------------------------------------------------

func BenchmarkSiLUMul_3072(b *testing.B) {
	gate := makeFloat32(3072)
	up := makeFloat32(3072)
	gateCopy := make([]float32, 3072)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(gateCopy, gate)
		SiLUMul(gateCopy, up)
	}
}

func BenchmarkSiLUMulScalar_3072(b *testing.B) {
	gate := makeFloat32(3072)
	up := makeFloat32(3072)
	gateCopy := make([]float32, 3072)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(gateCopy, gate)
		siluMulScalar(gateCopy, up)
	}
}

// ---------------------------------------------------------------------------
// RoPEPrecomputed benchmarks
// ---------------------------------------------------------------------------

func BenchmarkRoPEPrecomputed_12x64(b *testing.B) {
	nHeads := 12
	headDim := 64
	halfDim := headDim / 2
	x := makeFloat32(nHeads * headDim)
	cos := makeFloat32(halfDim)
	sin := makeFloat32(halfDim)
	xCopy := make([]float32, len(x))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(xCopy, x)
		RoPEPrecomputed(xCopy, nHeads, headDim, cos, sin)
	}
}

func BenchmarkRoPEPrecomputedScalar_12x64(b *testing.B) {
	nHeads := 12
	headDim := 64
	halfDim := headDim / 2
	x := makeFloat32(nHeads * headDim)
	cos := makeFloat32(halfDim)
	sin := makeFloat32(halfDim)
	xCopy := make([]float32, len(x))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(xCopy, x)
		ropePrecomputedScalar(xCopy, nHeads, headDim, cos, sin)
	}
}

// ---------------------------------------------------------------------------
// Correctness tests: ASM vs Scalar
// ---------------------------------------------------------------------------

func TestDotQ8Row_ASMvsScalar(t *testing.T) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	a := makeFloat32(cols)
	data := makeQ8Data(1, cols)

	got := DotQ8Row(a, data, 0, nBlocks)
	want := dotQ8RowScalar(a, data, 0, nBlocks)

	if diff := math.Abs(float64(got - want)); diff > 0.01*math.Abs(float64(want))+1e-6 {
		t.Errorf("DotQ8Row mismatch: ASM=%f Scalar=%f diff=%f", got, want, diff)
	}
}

func TestDequantRowInto_ASMvsScalar(t *testing.T) {
	const cols = 768
	nBlocks := cols / q8BlockSize
	data := makeQ8Data(1, cols)

	dstASM := make([]float32, cols)
	dstScalar := make([]float32, cols)

	dequantRowInto(data, 0, nBlocks, dstASM)
	dequantRowIntoScalar(dstScalar, data, 0, nBlocks)

	for i := range dstASM {
		if diff := math.Abs(float64(dstASM[i] - dstScalar[i])); diff > 1e-5 {
			t.Errorf("dequantRowInto[%d] mismatch: ASM=%f Scalar=%f", i, dstASM[i], dstScalar[i])
			break
		}
	}
}

func TestSiLUMul_ASMvsScalar(t *testing.T) {
	n := 3072
	gate := makeFloat32(n)
	up := makeFloat32(n)

	gateASM := make([]float32, n)
	gateScalar := make([]float32, n)
	copy(gateASM, gate)
	copy(gateScalar, gate)

	SiLUMul(gateASM, up)
	siluMulScalar(gateScalar, up)

	for i := range gateASM {
		diff := math.Abs(float64(gateASM[i] - gateScalar[i]))
		tol := 0.01*math.Abs(float64(gateScalar[i])) + 1e-5
		if diff > tol {
			t.Errorf("SiLUMul[%d] mismatch: ASM=%f Scalar=%f diff=%f", i, gateASM[i], gateScalar[i], diff)
			break
		}
	}
}

func TestRoPEPrecomputed_ASMvsScalar(t *testing.T) {
	nHeads := 12
	headDim := 64
	halfDim := headDim / 2
	x := makeFloat32(nHeads * headDim)
	cos := makeFloat32(halfDim)
	sin := makeFloat32(halfDim)

	xASM := make([]float32, len(x))
	xScalar := make([]float32, len(x))
	copy(xASM, x)
	copy(xScalar, x)

	RoPEPrecomputed(xASM, nHeads, headDim, cos, sin)
	ropePrecomputedScalar(xScalar, nHeads, headDim, cos, sin)

	for i := range xASM {
		if diff := math.Abs(float64(xASM[i] - xScalar[i])); diff > 1e-5 {
			t.Errorf("RoPEPrecomputed[%d] mismatch: ASM=%f Scalar=%f", i, xASM[i], xScalar[i])
			break
		}
	}
}

// Test with non-8-aligned sizes to verify tail handling
func TestSiLUMul_OddSize(t *testing.T) {
	for _, n := range []int{1, 3, 7, 9, 15, 17, 31, 33} {
		gate := makeFloat32(n)
		up := makeFloat32(n)

		gateASM := make([]float32, n)
		gateScalar := make([]float32, n)
		copy(gateASM, gate)
		copy(gateScalar, gate)

		SiLUMul(gateASM, up)
		siluMulScalar(gateScalar, up)

		for i := range gateASM {
			diff := math.Abs(float64(gateASM[i] - gateScalar[i]))
			tol := 0.01*math.Abs(float64(gateScalar[i])) + 1e-5
			if diff > tol {
				t.Errorf("SiLUMul n=%d [%d] mismatch: ASM=%f Scalar=%f", n, i, gateASM[i], gateScalar[i])
				break
			}
		}
	}
}

// Test RoPE with non-8-aligned halfDim
func TestRoPEPrecomputed_OddHalfDim(t *testing.T) {	for _, headDim := range []int{16, 24, 48, 64, 128} {
		nHeads := 4
		halfDim := headDim / 2
		x := makeFloat32(nHeads * headDim)
		cos := makeFloat32(halfDim)
		sin := makeFloat32(halfDim)

		xASM := make([]float32, len(x))
		xScalar := make([]float32, len(x))
		copy(xASM, x)
		copy(xScalar, x)

		RoPEPrecomputed(xASM, nHeads, headDim, cos, sin)
		ropePrecomputedScalar(xScalar, nHeads, headDim, cos, sin)

		for i := range xASM {
			if diff := math.Abs(float64(xASM[i] - xScalar[i])); diff > 1e-5 {
				t.Errorf("RoPE headDim=%d [%d] mismatch: ASM=%f Scalar=%f", headDim, i, xASM[i], xScalar[i])
				break
			}
		}
	}
}

// ---------------------------------------------------------------------------
// MatMulQ8 vs MatMulQ8Fused benchmarks (to validate fusedThreshold)
// ---------------------------------------------------------------------------

func BenchmarkMatMulQ8_1x768x768(b *testing.B) {
	M, N, K := 1, 768, 768
	a := makeFloat32(M * K)
	q8 := &Q8Tensor{Data: makeQ8Data(N, K), Rows: N, Cols: K}
	out := make([]float32, M*N)
	SetMatMulMaxParallel(1) // single-threaded for fair comparison
	defer SetMatMulMaxParallel(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MatMulQ8(out, a, q8, M, N, K)
	}
}

func BenchmarkMatMulQ8Fused_1x768x768(b *testing.B) {
	M, N, K := 1, 768, 768
	a := makeFloat32(M * K)
	q8 := &Q8Tensor{Data: makeQ8Data(N, K), Rows: N, Cols: K}
	out := make([]float32, M*N)
	SetMatMulMaxParallel(1)
	defer SetMatMulMaxParallel(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		MatMulQ8Fused(out, a, q8, M, N, K)
	}
}

// ---------------------------------------------------------------------------
// DequantRow benchmark (now uses ASM path)
// ---------------------------------------------------------------------------

func BenchmarkDequantRow_768(b *testing.B) {
	const cols = 768
	q8 := &Q8Tensor{Data: makeQ8Data(1, cols), Rows: 1, Cols: cols}
	dst := make([]float32, cols)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q8.DequantRow(0, dst)
	}
}
