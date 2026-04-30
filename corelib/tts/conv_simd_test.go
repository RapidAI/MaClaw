package tts

import (
	"math"
	"math/rand"
	"testing"
)

// maxAbsDiff returns the maximum absolute difference between two slices.
func maxAbsDiff(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.Inf(1))
	}
	var maxD float32
	for i := range a {
		d := float32(math.Abs(float64(a[i] - b[i])))
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

func TestConv1DKernel1SIMD(t *testing.T) {
	// Test kSize=1 SIMD path against scalar reference
	inCh := 64
	T := 32
	outCh := 32

	input := make([]float32, inCh*T)
	kernel := make([]float32, outCh*inCh)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()*2 - 1
	}
	for i := range kernel {
		kernel[i] = rand.Float32()*0.2 - 0.1
	}
	for i := range bias {
		bias[i] = rand.Float32()*0.1 - 0.05
	}

	// Scalar reference
	refOut := make([]float32, outCh*T)
	conv1DKernel1Range(input, kernel, refOut, bias, 0, outCh, inCh, T)

	// SIMD
	simdOut := make([]float32, outCh*T)
	conv1DKernel1SIMDRange(
		// First transpose input
		func() []float32 {
			inputT := make([]float32, T*inCh)
			for ic := 0; ic < inCh; ic++ {
				for t := 0; t < T; t++ {
					inputT[t*inCh+ic] = input[ic*T+t]
				}
			}
			return inputT
		}(),
		kernel, simdOut, bias, 0, outCh, inCh, T)

	d := maxAbsDiff(refOut, simdOut)
	t.Logf("kSize=1 SIMD vs scalar maxDiff=%.6f", d)
	if d > 0.001 {
		t.Errorf("kSize=1 SIMD diverged from scalar: maxDiff=%.6f", d)
	}
}

func TestConv1DStride1SIMD(t *testing.T) {
	// Test stride=1 SIMD path against scalar reference for various kSize
	for _, kSize := range []int{3, 5, 7} {
		t.Run("kSize="+string(rune('0'+kSize)), func(t *testing.T) {
			inCh := 64
			inLen := 48
			outCh := 32
			padding := (kSize - 1) / 2
			outLen := inLen // stride=1, same padding

			input := make([]float32, inCh*inLen)
			kernel := make([]float32, outCh*inCh*kSize)
			bias := make([]float32, outCh)
			for i := range input {
				input[i] = rand.Float32()*2 - 1
			}
			for i := range kernel {
				kernel[i] = rand.Float32()*0.2 - 0.1
			}
			for i := range bias {
				bias[i] = rand.Float32()*0.1 - 0.05
			}

			// Scalar reference
			refOut := make([]float32, outCh*outLen)
			conv1DRangeStride1(input, kernel, refOut, bias, 0, outCh, inCh, inLen, kSize, outLen, padding)

			// SIMD
			simdOut := make([]float32, outCh*outLen)
			conv1DRangeStride1SIMD(input, kernel, simdOut, bias, 0, outCh, inCh, inLen, kSize, outLen, padding, false)

			d := maxAbsDiff(refOut, simdOut)
			t.Logf("kSize=%d SIMD vs scalar maxDiff=%.6f", kSize, d)
			if d > 0.01 {
				t.Errorf("kSize=%d SIMD diverged from scalar: maxDiff=%.6f", kSize, d)
			}
		})
	}
}

func TestConv1DDilatedSIMD(t *testing.T) {
	// Test dilated SIMD path against scalar reference
	for _, tc := range []struct {
		kSize    int
		dilation int
	}{
		{3, 1}, {3, 2}, {3, 3}, {3, 12},
		{5, 2}, {7, 3},
	} {
		t.Run("", func(t *testing.T) {
			inCh := 64
			inLen := 64
			outCh := 32
			stride := 1
			effKSize := (tc.kSize-1)*tc.dilation + 1
			padding := (effKSize - 1) / 2
			paddedLen := inLen + 2*padding
			outLen := (paddedLen - effKSize) / stride + 1

			midStart := (padding + stride - 1) / stride
			midEnd := (inLen - 1 - (tc.kSize-1)*tc.dilation + padding) / stride + 1
			if midStart < 0 {
				midStart = 0
			}
			if midEnd > outLen {
				midEnd = outLen
			}
			if midEnd < midStart {
				midEnd = midStart
			}

			input := make([]float32, inCh*inLen)
			kernel := make([]float32, outCh*inCh*tc.kSize)
			bias := make([]float32, outCh)
			for i := range input {
				input[i] = rand.Float32()*2 - 1
			}
			for i := range kernel {
				kernel[i] = rand.Float32()*0.2 - 0.1
			}
			for i := range bias {
				bias[i] = rand.Float32()*0.1 - 0.05
			}

			// Scalar reference
			refOut := make([]float32, outCh*outLen)
			conv1DDilatedRange(input, kernel, refOut, bias, 0, outCh, inCh, inLen, tc.kSize, outLen, stride, padding, tc.dilation, midStart, midEnd)

			// SIMD
			simdOut := make([]float32, outCh*outLen)
			conv1DDilatedRangeSIMD(input, kernel, simdOut, bias, 0, outCh, inCh, inLen, tc.kSize, outLen, stride, padding, tc.dilation, midStart, midEnd, false)

			d := maxAbsDiff(refOut, simdOut)
			t.Logf("kSize=%d dil=%d SIMD vs scalar maxDiff=%.6f", tc.kSize, tc.dilation, d)
			if d > 0.01 {
				t.Errorf("kSize=%d dil=%d SIMD diverged from scalar: maxDiff=%.6f", tc.kSize, tc.dilation, d)
			}
		})
	}
}

func TestConvTranspose1DSIMD(t *testing.T) {
	// Test ConvTranspose1D SIMD path against scalar reference
	inCh := 64
	outCh := 32
	inLen := 16
	kSize := 16
	stride := 8
	padding := (kSize - stride) / 2
	outLen := (inLen-1)*stride - 2*padding + kSize

	input := make([]float32, inCh*inLen)
	kernel := make([]float32, inCh*outCh*kSize)
	for i := range input {
		input[i] = rand.Float32()*2 - 1
	}
	for i := range kernel {
		kernel[i] = rand.Float32()*0.2 - 0.1
	}

	// Scalar reference
	refOut := make([]float32, outCh*outLen)
	convT1DByOutCh(input, kernel, refOut, 0, outCh, outCh, inCh, inLen, outLen, kSize, stride, padding)

	// SIMD
	simdOut := make([]float32, outCh*outLen)
	convT1DByOutChSIMD(input, kernel, simdOut, 0, outCh, outCh, inCh, inLen, outLen, kSize, stride, padding, false)

	d := maxAbsDiff(refOut, simdOut)
	t.Logf("ConvT1D SIMD vs scalar maxDiff=%.6f", d)
	if d > 0.01 {
		t.Errorf("ConvT1D SIMD diverged from scalar: maxDiff=%.6f", d)
	}
}

// End of benchmarks

// Benchmarks

func BenchmarkConv1DStride1_Scalar(b *testing.B) {
	inCh := 128
	inLen := 256
	outCh := 128
	kSize := 3
	padding := 1
	outLen := inLen

	input := make([]float32, inCh*inLen)
	kernel := make([]float32, outCh*inCh*kSize)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	out := make([]float32, outCh*outLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DRangeStride1(input, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, padding)
	}
}

func BenchmarkConv1DStride1_SIMD(b *testing.B) {
	inCh := 128
	inLen := 256
	outCh := 128
	kSize := 3
	padding := 1
	outLen := inLen

	input := make([]float32, inCh*inLen)
	kernel := make([]float32, outCh*inCh*kSize)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	out := make([]float32, outCh*outLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DRangeStride1SIMD(input, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, padding, false)
	}
}

func BenchmarkConv1DDilated_Scalar(b *testing.B) {
	inCh := 128
	inLen := 256
	outCh := 128
	kSize := 3
	dilation := 2
	stride := 1
	effKSize := (kSize-1)*dilation + 1
	padding := (effKSize - 1) / 2
	paddedLen := inLen + 2*padding
	outLen := (paddedLen - effKSize) / stride + 1

	midStart := (padding + stride - 1) / stride
	midEnd := (inLen - 1 - (kSize-1)*dilation + padding) / stride + 1

	input := make([]float32, inCh*inLen)
	kernel := make([]float32, outCh*inCh*kSize)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	out := make([]float32, outCh*outLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DDilatedRange(input, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd)
	}
}

func BenchmarkConv1DDilated_SIMD(b *testing.B) {
	inCh := 128
	inLen := 256
	outCh := 128
	kSize := 3
	dilation := 2
	stride := 1
	effKSize := (kSize-1)*dilation + 1
	padding := (effKSize - 1) / 2
	paddedLen := inLen + 2*padding
	outLen := (paddedLen - effKSize) / stride + 1

	midStart := (padding + stride - 1) / stride
	midEnd := (inLen - 1 - (kSize-1)*dilation + padding) / stride + 1

	input := make([]float32, inCh*inLen)
	kernel := make([]float32, outCh*inCh*kSize)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	out := make([]float32, outCh*outLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DDilatedRangeSIMD(input, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd, false)
	}
}

func BenchmarkConv1DKernel1_Scalar(b *testing.B) {
	inCh := 192
	T := 256
	outCh := 192

	input := make([]float32, inCh*T)
	kernel := make([]float32, outCh*inCh)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	out := make([]float32, outCh*T)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DKernel1Range(input, kernel, out, bias, 0, outCh, inCh, T)
	}
}

func BenchmarkConv1DKernel1_SIMD(b *testing.B) {
	inCh := 192
	T := 256
	outCh := 192

	input := make([]float32, inCh*T)
	kernel := make([]float32, outCh*inCh)
	bias := make([]float32, outCh)
	for i := range input {
		input[i] = rand.Float32()
	}
	for i := range kernel {
		kernel[i] = rand.Float32() * 0.1
	}

	// Pre-transpose for SIMD
	inputT := make([]float32, T*inCh)
	for ic := 0; ic < inCh; ic++ {
		for t := 0; t < T; t++ {
			inputT[t*inCh+ic] = input[ic*T+t]
		}
	}

	out := make([]float32, outCh*T)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv1DKernel1SIMDRange(inputT, kernel, out, bias, 0, outCh, inCh, T)
	}
}

// End of benchmarks
