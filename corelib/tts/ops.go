// Package tts provides a pure Go MeloTTS (VITS) inference engine.
// No CGo, no ONNX, no Python dependency.
package tts

import (
	"math"
	"math/rand"
	"runtime"
	"sync"
)

// ── Element-wise activations ──

// LeakyReLU applies leaky ReLU in-place: max(x, slope*x).
func LeakyReLU(x []float32, slope float32) {
	for i, v := range x {
		if v < 0 {
			x[i] = v * slope
		}
	}
}

// ReLU applies ReLU in-place: max(x, 0).
func ReLU(x []float32) {
	for i, v := range x {
		if v < 0 {
			x[i] = 0
		}
	}
}

// Exp applies exp in-place.
func Exp(x []float32) {
	for i, v := range x {
		x[i] = float32(math.Exp(float64(v)))
	}
}

// Ceil applies ceil in-place.
func Ceil(x []float32) {
	for i, v := range x {
		x[i] = float32(math.Ceil(float64(v)))
	}
}

// Sigmoid applies sigmoid in-place.
func Sigmoid(x []float32) {
	for i, v := range x {
		x[i] = 1.0 / (1.0 + float32(math.Exp(float64(-v))))
	}
}

// Randn fills x with standard normal random values.
func Randn(x []float32) {
	for i := range x {
		x[i] = float32(rand.NormFloat64())
	}
}

// RandnScale fills x with normal random values scaled by s.
func RandnScale(x []float32, s float32) {
	for i := range x {
		x[i] = float32(rand.NormFloat64()) * s
	}
}

// ClampMin clamps all values to >= minVal.
func ClampMin(x []float32, minVal float32) {
	for i, v := range x {
		if v < minVal {
			x[i] = minVal
		}
	}
}

// ── Flip (for normalizing flow reverse) ──

// FlipChannels reverses the channel dimension of a [C, T] tensor in-place.
// After flip, channel 0 becomes channel C-1, etc.
func FlipChannels(data []float32, C, T int) {
	for i := 0; i < C/2; i++ {
		j := C - 1 - i
		rowI := data[i*T : (i+1)*T]
		rowJ := data[j*T : (j+1)*T]
		for k := 0; k < T; k++ {
			rowI[k], rowJ[k] = rowJ[k], rowI[k]
		}
	}
}

// ── Conv1D (layout: [outCh, inCh, kSize], input: [inCh, T]) ──

// Conv1D computes 1D convolution.
// input: [inCh, inLen] row-major
// kernel: [outCh, inCh, kSize] row-major (PyTorch weight layout)
// bias: [outCh] or nil
// Returns: [outCh, outLen] where outLen = (inLen + 2*padding - kSize) / stride + 1
func Conv1D(input []float32, inCh, inLen int,
	kernel []float32, kSize, outCh, stride, padding int,
	bias []float32) []float32 {

	paddedLen := inLen + 2*padding
	outLen := (paddedLen - kSize) / stride + 1
	if outLen <= 0 {
		return nil
	}
	out := make([]float32, outCh*outLen)

	// Special case: kSize=1, stride=1, padding=0 → matrix multiply
	if kSize == 1 && stride == 1 && padding == 0 {
		conv1DKernel1(input, kernel, out, bias, outCh, inCh, inLen)
		return out
	}

	nWorkers := runtime.NumCPU()
	if nWorkers > outCh {
		nWorkers = outCh
	}

	// For SIMD path: transpose input once before parallelization.
	// This avoids each goroutine redundantly transposing the same data.
	var inputT []float32
	useSIMD := inCh >= 16 && stride == 1 && kSize > 1
	if useSIMD {
		inputT = transposeInput(input, inCh, inLen)
	}

	if nWorkers <= 1 || outCh*outLen < 256 {
		if useSIMD {
			conv1DRangeStride1SIMDWithTranspose(input, inputT, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, padding, false)
		} else {
			conv1DRange(input, kernel, out, bias, 0, outCh, inCh, inLen, kSize, outLen, stride, padding)
		}
		return out
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
			if useSIMD {
				conv1DRangeStride1SIMDWithTranspose(input, inputT, kernel, out, bias, s, e, inCh, inLen, kSize, outLen, padding, false)
			} else {
				conv1DRange(input, kernel, out, bias, s, e, inCh, inLen, kSize, outLen, stride, padding)
			}
		}(s, e)
	}
	wg.Wait()
	return out
}

// conv1DKernel1 handles the kSize=1 case as a matrix multiply: out = kernel @ input + bias.
// kernel: [outCh, inCh], input: [inCh, T], out: [outCh, T]
func conv1DKernel1(input, kernel, out []float32, bias []float32, outCh, inCh, T int) {
	// SIMD path: transpose input once, then use vek32.Dot for each (oc, t) pair
	if inCh >= 16 {
		conv1DKernel1SIMD(input, kernel, out, bias, outCh, inCh, T)
		return
	}

	nWorkers := runtime.NumCPU()
	if nWorkers > outCh {
		nWorkers = outCh
	}
	if nWorkers <= 1 || outCh < 4 {
		conv1DKernel1Range(input, kernel, out, bias, 0, outCh, inCh, T)
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
			conv1DKernel1Range(input, kernel, out, bias, s, e, inCh, T)
		}(s, e)
	}
	wg.Wait()
}

func conv1DKernel1Range(input, kernel, out []float32, bias []float32, ocStart, ocEnd, inCh, T int) {
	for oc := ocStart; oc < ocEnd; oc++ {
		var b float64
		if bias != nil {
			b = float64(bias[oc])
		}
		kRow := kernel[oc*inCh : (oc+1)*inCh]
		oRow := out[oc*T : (oc+1)*T]

		for t := 0; t < T; t++ {
			var sum float64
			for ic := 0; ic < inCh; ic++ {
				sum += float64(kRow[ic]) * float64(input[ic*T+t])
			}
			oRow[t] = float32(sum + b)
		}
	}
}

func conv1DRange(input, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, stride, padding int) {

	// Fast path for stride=1: split into left-pad / middle / right-pad regions
	if stride == 1 && kSize > 1 {
		conv1DRangeStride1(input, kernel, out, bias, ocStart, ocEnd, inCh, inLen, kSize, outLen, padding)
		return
	}

	// General path (stride > 1 or kSize == 1)
	for oc := ocStart; oc < ocEnd; oc++ {
		var b float64
		if bias != nil {
			b = float64(bias[oc])
		}
		for o := 0; o < outLen; o++ {
			var sum float64
			inStart := o*stride - padding
			for ic := 0; ic < inCh; ic++ {
				kOff := (oc*inCh + ic) * kSize
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernel[kOff+k])
					}
				}
			}
			out[oc*outLen+o] = float32(sum + b)
		}
	}
}

// conv1DRangeStride1 is the optimized path for stride=1 convolutions.
// Splits output into three regions to eliminate boundary checks in the middle.
func conv1DRangeStride1(input, kernel, out []float32, bias []float32,
	ocStart, ocEnd, inCh, inLen, kSize, outLen, padding int) {

	// Middle region: output positions where all kernel taps are within input bounds
	// inStart = o - padding >= 0  →  o >= padding
	// inStart + kSize - 1 < inLen  →  o - padding + kSize - 1 < inLen  →  o < inLen - kSize + 1 + padding
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
		var b float64
		if bias != nil {
			b = float64(bias[oc])
		}
		ocKernBase := oc * inCh * kSize
		outBase := oc * outLen

		// Left boundary (with bounds checking)
		for o := 0; o < midStart; o++ {
			var sum float64
			inStart := o - padding
			for ic := 0; ic < inCh; ic++ {
				kOff := ocKernBase + ic*kSize
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernel[kOff+k])
					}
				}
			}
			out[outBase+o] = float32(sum + b)
		}

		// Middle (no bounds checking — hot path, kSize-specialized)
		switch kSize {
		case 3:
			for o := midStart; o < midEnd; o++ {
				var sum float64
				inStart := o - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*3
					iOff := ic*inLen + inStart
					sum += float64(input[iOff]) * float64(kernel[kOff])
					sum += float64(input[iOff+1]) * float64(kernel[kOff+1])
					sum += float64(input[iOff+2]) * float64(kernel[kOff+2])
				}
				out[outBase+o] = float32(sum + b)
			}
		case 5:
			for o := midStart; o < midEnd; o++ {
				var sum float64
				inStart := o - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*5
					iOff := ic*inLen + inStart
					sum += float64(input[iOff])*float64(kernel[kOff]) +
						float64(input[iOff+1])*float64(kernel[kOff+1]) +
						float64(input[iOff+2])*float64(kernel[kOff+2]) +
						float64(input[iOff+3])*float64(kernel[kOff+3]) +
						float64(input[iOff+4])*float64(kernel[kOff+4])
				}
				out[outBase+o] = float32(sum + b)
			}
		case 7:
			for o := midStart; o < midEnd; o++ {
				var sum float64
				inStart := o - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*7
					iOff := ic*inLen + inStart
					sum += float64(input[iOff])*float64(kernel[kOff]) +
						float64(input[iOff+1])*float64(kernel[kOff+1]) +
						float64(input[iOff+2])*float64(kernel[kOff+2]) +
						float64(input[iOff+3])*float64(kernel[kOff+3]) +
						float64(input[iOff+4])*float64(kernel[kOff+4]) +
						float64(input[iOff+5])*float64(kernel[kOff+5]) +
						float64(input[iOff+6])*float64(kernel[kOff+6])
				}
				out[outBase+o] = float32(sum + b)
			}
		default:
			for o := midStart; o < midEnd; o++ {
				var sum float64
				inStart := o - padding
				for ic := 0; ic < inCh; ic++ {
					kOff := ocKernBase + ic*kSize
					iOff := ic*inLen + inStart
					kSlice := kernel[kOff : kOff+kSize]
					iSlice := input[iOff : iOff+kSize]
					for k := 0; k < kSize; k++ {
						sum += float64(iSlice[k]) * float64(kSlice[k])
					}
				}
				out[outBase+o] = float32(sum + b)
			}
		}

		// Right boundary (with bounds checking)
		for o := midEnd; o < outLen; o++ {
			var sum float64
			inStart := o - padding
			for ic := 0; ic < inCh; ic++ {
				kOff := ocKernBase + ic*kSize
				iOff := ic * inLen
				for k := 0; k < kSize; k++ {
					inPos := inStart + k
					if inPos >= 0 && inPos < inLen {
						sum += float64(input[iOff+inPos]) * float64(kernel[kOff+k])
					}
				}
			}
			out[outBase+o] = float32(sum + b)
		}
	}
}

// ── ConvTranspose1D (the key new op for HiFi-GAN) ──

// ConvTranspose1D computes transposed 1D convolution (deconvolution).
// input: [inCh, inLen] row-major
// kernel: [inCh, outCh, kSize] row-major (PyTorch ConvTranspose1d weight layout)
// bias: [outCh] or nil
// Returns: [outCh, outLen] where outLen = (inLen - 1) * stride - 2*padding + kSize
func ConvTranspose1D(input []float32, inCh, inLen int,
	kernel []float32, kSize, outCh, stride, padding int,
	bias []float32) []float32 {

	outLen := (inLen-1)*stride - 2*padding + kSize
	if outLen <= 0 {
		return nil
	}
	out := make([]float32, outCh*outLen)

	// Initialize with bias
	if bias != nil {
		for oc := 0; oc < outCh; oc++ {
			b := bias[oc]
			row := out[oc*outLen : (oc+1)*outLen]
			for i := range row {
				row[i] = b
			}
		}
	}

	// Parallelize by output channel — each goroutine writes to its own rows.
	nWorkers := runtime.NumCPU()
	if nWorkers > outCh {
		nWorkers = outCh
	}

	// For SIMD: transpose input once before parallelization
	var inputT []float32
	useSIMD := inCh >= 16
	if useSIMD {
		inputT = transposeInput(input, inCh, inLen)
	}

	dispatchRange := func(s, e int) {
		if useSIMD {
			convT1DByOutChSIMDWithTranspose(inputT, kernel, out, s, e, outCh, inCh, inLen, outLen, kSize, stride, padding, false)
		} else {
			convT1DByOutCh(input, kernel, out, s, e, outCh, inCh, inLen, outLen, kSize, stride, padding)
		}
	}

	if nWorkers <= 1 || outCh < 4 {
		dispatchRange(0, outCh)
		return out
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
			dispatchRange(s, e)
		}(s, e)
	}
	wg.Wait()
	return out
}

func convT1DByOutCh(input, kernel, out []float32,
	ocStart, ocEnd, totalOutCh, inCh, inLen, outLen, kSize, stride, padding int) {
	for oc := ocStart; oc < ocEnd; oc++ {
		oRow := out[oc*outLen : (oc+1)*outLen]
		for ic := 0; ic < inCh; ic++ {
			kOff := (ic*totalOutCh + oc) * kSize
			iBase := ic * inLen
			// Process each input position
			for i := 0; i < inLen; i++ {
				inVal := input[iBase+i]
				if inVal == 0 {
					continue
				}
				outStart := i*stride - padding
				// Unrolled for common kSize values
				for k := 0; k < kSize; k++ {
					outPos := outStart + k
					if outPos >= 0 && outPos < outLen {
						oRow[outPos] += inVal * kernel[kOff+k]
					}
				}
			}
		}
	}
}

// ── generate_path: hard monotonic alignment from durations ──

// GeneratePath creates a hard alignment matrix from durations.
// durations: [T_text] integer durations (mel frames per phoneme)
// Returns: [T_mel, T_text] binary matrix (as float32 0/1)
// and T_mel (total mel frames).
func GeneratePath(durations []int) (path []float32, tMel int) {
	tText := len(durations)
	for _, d := range durations {
		tMel += d
	}
	if tMel == 0 {
		return nil, 0
	}
	path = make([]float32, tMel*tText)
	melPos := 0
	for t, dur := range durations {
		for d := 0; d < dur; d++ {
			if melPos < tMel {
				path[melPos*tText+t] = 1.0
			}
			melPos++
		}
	}
	return path, tMel
}

// ── Sequence mask ──

// SequenceMask creates a [1, 1, maxLen] mask where positions < length are 1.0.
func SequenceMask(length, maxLen int) []float32 {
	mask := make([]float32, maxLen)
	for i := 0; i < length && i < maxLen; i++ {
		mask[i] = 1.0
	}
	return mask
}
