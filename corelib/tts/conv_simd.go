package tts

// SIMD-accelerated Conv1D operations for TTS inference.
//
// Strategy overview:
//
// 1. kSize=1 (4% of time): Transpose input [inCh, T] → [T, inCh] once,
//    then use vek32.Dot for each (oc, t) pair. 16x speedup.
//
// 2. kSize=3/5/7 stride=1 (25% of time): Transpose input once, then for each
//    output point, the kSize input patches across all channels are at known
//    offsets in the transposed buffer. Use vek32.Dot per kernel tap.
//
// 3. kSize=3/5/7 dilated (60% of time): Same transpose approach, but with
//    dilated offsets in the transposed buffer.
//
// The key insight: input transpose is O(inCh * T) and done once per conv call.
// The per-output-point work becomes kSize dot products of length inCh each,
// all on contiguous memory. This eliminates the strided gather that was the
// bottleneck in the previous approach.

import (
	"runtime"
	"sync"

	"github.com/viterin/vek/vek32"
)

// transposeInput converts input from [inCh, inLen] to [inLen, inCh] layout.
// After transpose, input[pos, ic] = inputT[pos*inCh + ic] — contiguous across channels.
func transposeInput(input []float32, inCh, inLen int) []float32 {
	inputT := make([]float32, inLen*inCh)
	for ic := 0; ic < inCh; ic++ {
		srcOff := ic * inLen
		for t := 0; t < inLen; t++ {
			inputT[t*inCh+ic] = input[srcOff+t]
		}
	}
	return inputT
}

// conv1DRangeStride1SIMD is the SIMD-optimized version of conv1DRangeStride1.
// Transposes input once, then uses vek32.Dot for contiguous channel accumulation.
// If transposed=true, kernel layout is [outCh, kSize, inCh] (pre-transposed).
// If transposed=false, kernel layout is [outCh, inCh, kSize] (original PyTorch).
func conv1DRangeStride1SIMD(input, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, padding int, transposed bool) {

	if inCh < 16 {
		conv1DRangeStride1(input, kernel, out, bias, ocStart, ocEnd, inCh, inLen, kSize, outLen, padding)
		return
	}
	inputT := transposeInput(input, inCh, inLen)
	conv1DRangeStride1SIMDWithTranspose(input, inputT, kernel, out, bias, ocStart, ocEnd, inCh, inLen, kSize, outLen, padding, transposed)
}

// conv1DRangeStride1SIMDWithTranspose is the core SIMD conv1d with pre-transposed input.
// inputT must be [inLen, inCh] layout (transposed from input [inCh, inLen]).
// input (original layout) is used only for boundary regions.
func conv1DRangeStride1SIMDWithTranspose(input, inputT, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, padding int, transposed bool) {

	midStart := padding
	midEnd := inLen - kSize + 1 + padding
	if midStart < 0 {
		midStart = 0
	}
	if midEnd > outLen {
		midEnd = outLen
	}
	if midEnd < midStart {
		midEnd = midStart
	}

	for oc := ocStart; oc < ocEnd; oc++ {
		var b float32
		if bias != nil {
			b = bias[oc]
		}
		outBase := oc * outLen

		// Get kernel tap slices (contiguous across inCh for each k)
		kernTaps := make([][]float32, kSize)
		if transposed {
			base := oc * kSize * inCh
			for k := 0; k < kSize; k++ {
				kernTaps[k] = kernel[base+k*inCh : base+(k+1)*inCh]
			}
		} else {
			ocKernBase := oc * inCh * kSize
			for k := 0; k < kSize; k++ {
				tap := make([]float32, inCh)
				for ic := 0; ic < inCh; ic++ {
					tap[ic] = kernel[ocKernBase+ic*kSize+k]
				}
				kernTaps[k] = tap
			}
		}

		// Left boundary (scalar)
		for o := 0; o < midStart; o++ {
			var sum float64
			inStart := o - padding
			for ic := 0; ic < inCh; ic++ {
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernTaps[k][ic])
					}
				}
			}
			out[outBase+o] = float32(sum) + b
		}

		// Middle: SIMD path using transposed input
		// inputT[pos*inCh : (pos+1)*inCh] gives all channel values at position pos
		for o := midStart; o < midEnd; o++ {
			inStart := o - padding
			var sum float32
			for k := 0; k < kSize; k++ {
				pos := inStart + k
				sum += vek32.Dot(kernTaps[k], inputT[pos*inCh:(pos+1)*inCh])
			}
			out[outBase+o] = sum + b
		}

		// Right boundary (scalar)
		for o := midEnd; o < outLen; o++ {
			var sum float64
			inStart := o - padding
			for ic := 0; ic < inCh; ic++ {
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernTaps[k][ic])
					}
				}
			}
			out[outBase+o] = float32(sum) + b
		}
	}
}

// conv1DDilatedRangeSIMD is the SIMD-optimized version of conv1DDilatedRange.
// Transposes input once, then uses vek32.Dot with dilated offsets.
func conv1DDilatedRangeSIMD(input, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd int,
	transposed bool) {

	if inCh < 16 {
		conv1DDilatedRange(input, kernel, out, bias, ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd)
		return
	}
	inputT := transposeInput(input, inCh, inLen)
	conv1DDilatedRangeSIMDWithTranspose(input, inputT, kernel, out, bias, ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd, transposed)
}

// conv1DDilatedRangeSIMDWithTranspose is the core SIMD dilated conv with pre-transposed input.
func conv1DDilatedRangeSIMDWithTranspose(input, inputT, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding, dilation, midStart, midEnd int,
	transposed bool) {

	for oc := ocStart; oc < ocEnd; oc++ {
		var b float32
		if bias != nil {
			b = bias[oc]
		}
		outBase := oc * outLen

		// Get kernel tap slices
		kernTaps := make([][]float32, kSize)
		if transposed {
			base := oc * kSize * inCh
			for k := 0; k < kSize; k++ {
				kernTaps[k] = kernel[base+k*inCh : base+(k+1)*inCh]
			}
		} else {
			ocKernBase := oc * inCh * kSize
			for k := 0; k < kSize; k++ {
				tap := make([]float32, inCh)
				for ic := 0; ic < inCh; ic++ {
					tap[ic] = kernel[ocKernBase+ic*kSize+k]
				}
				kernTaps[k] = tap
			}
		}

		// Left boundary (scalar)
		for o := 0; o < midStart; o++ {
			var sum float64
			inStart := o*stride - padding
			for ic := 0; ic < inCh; ic++ {
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k*dilation
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernTaps[k][ic])
					}
				}
			}
			out[outBase+o] = float32(sum) + b
		}

		// Middle: SIMD path using transposed input with dilated offsets
		for o := midStart; o < midEnd; o++ {
			inStart := o*stride - padding
			var sum float32
			for k := 0; k < kSize; k++ {
				pos := inStart + k*dilation
				sum += vek32.Dot(kernTaps[k], inputT[pos*inCh:(pos+1)*inCh])
			}
			out[outBase+o] = sum + b
		}

		// Right boundary (scalar)
		for o := midEnd; o < outLen; o++ {
			var sum float64
			inStart := o*stride - padding
			for ic := 0; ic < inCh; ic++ {
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k*dilation
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernTaps[k][ic])
					}
				}
			}
			out[outBase+o] = float32(sum) + b
		}
	}
}

// conv1DKernel1SIMD is the SIMD-optimized version of conv1DKernel1.
// For kSize=1: out[oc, t] = dot(kernel[oc, :], input[:, t]) + bias[oc]
// Transpose input once, then use vek32.Dot for contiguous channel vectors.
func conv1DKernel1SIMD(input, kernel, out []float32, bias []float32, outCh, inCh, T int) {
	inputT := transposeInput(input, inCh, T)

	nWorkers := runtime.NumCPU()
	if nWorkers > outCh {
		nWorkers = outCh
	}
	if nWorkers <= 1 || outCh < 4 {
		conv1DKernel1SIMDRange(inputT, kernel, out, bias, 0, outCh, inCh, T)
		return
	}

	var wg sync.WaitGroup
	chunk := (outCh + nWorkers - 1) / nWorkers
	for w := 0; w < nWorkers; w++ {
		s, e := w*chunk, (w+1)*chunk
		if e > outCh {
			e = outCh
		}
		if s >= e {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			conv1DKernel1SIMDRange(inputT, kernel, out, bias, s, e, inCh, T)
		}(s, e)
	}
	wg.Wait()
}

func conv1DKernel1SIMDRange(inputT, kernel, out []float32, bias []float32, ocStart, ocEnd, inCh, T int) {
	for oc := ocStart; oc < ocEnd; oc++ {
		var b float32
		if bias != nil {
			b = bias[oc]
		}
		kRow := kernel[oc*inCh : (oc+1)*inCh]
		oRow := out[oc*T : (oc+1)*T]
		for t := 0; t < T; t++ {
			oRow[t] = vek32.Dot(kRow, inputT[t*inCh:(t+1)*inCh]) + b
		}
	}
}

// convT1DByOutChSIMD is the SIMD-optimized version of convT1DByOutCh.
// Transposes input once, then uses vek32.Dot for channel accumulation.
func convT1DByOutChSIMD(input, kernel, out []float32,
	ocStart, ocEnd, totalOutCh, inCh, inLen, outLen, kSize, stride, padding int,
	transposed bool) {

	if inCh < 16 {
		convT1DByOutCh(input, kernel, out, ocStart, ocEnd, totalOutCh, inCh, inLen, outLen, kSize, stride, padding)
		return
	}
	inputT := transposeInput(input, inCh, inLen)
	convT1DByOutChSIMDWithTranspose(inputT, kernel, out, ocStart, ocEnd, totalOutCh, inCh, inLen, outLen, kSize, stride, padding, transposed)
}

// convT1DByOutChSIMDWithTranspose is the core SIMD ConvTranspose1D with pre-transposed input.
func convT1DByOutChSIMDWithTranspose(inputT, kernel, out []float32,
	ocStart, ocEnd, totalOutCh, inCh, inLen, outLen, kSize, stride, padding int,
	transposed bool) {

	for oc := ocStart; oc < ocEnd; oc++ {
		oRow := out[oc*outLen : (oc+1)*outLen]

		// Get kernel tap slices
		kernTaps := make([][]float32, kSize)
		if transposed {
			base := oc * kSize * inCh
			for k := 0; k < kSize; k++ {
				kernTaps[k] = kernel[base+k*inCh : base+(k+1)*inCh]
			}
		} else {
			for k := 0; k < kSize; k++ {
				tap := make([]float32, inCh)
				for ic := 0; ic < inCh; ic++ {
					tap[ic] = kernel[(ic*totalOutCh+oc)*kSize+k]
				}
				kernTaps[k] = tap
			}
		}

		// For each input position, use SIMD dot product across channels
		for i := 0; i < inLen; i++ {
			inVec := inputT[i*inCh : (i+1)*inCh]
			outStart := i*stride - padding
			for k := 0; k < kSize; k++ {
				outPos := outStart + k
				if outPos >= 0 && outPos < outLen {
					oRow[outPos] += vek32.Dot(inVec, kernTaps[k])
				}
			}
		}
	}
}
