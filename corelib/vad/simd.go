// corelib/vad/simd.go — SIMD-accelerated operators for Silero VAD inference.
//
// Uses github.com/viterin/vek/vek32 for AVX2/NEON dot product acceleration.
// The three hot paths in VAD inference are all dot-product dominated:
//   1. STFT Conv1D: 258 dot products of length 256 each
//   2. Encoder Conv1D: multi-channel convolution = dot products per output element
//   3. LSTM gates: 512 dot products of length 128 each
//
// vek32.Dot handles CPU feature detection internally (AVX2 → SSE → scalar fallback).
package vad

import (
	"math"

	"github.com/viterin/vek/vek32"
)

// conv1dStrideSimd performs STFT convolution using SIMD dot products.
// Each output element is a dot product of a weight row and an input window.
// weight: [outCh, 1, kSize], input: [inputLen], dst: [outCh * outLen]
func conv1dStrideSimd(dst, input, weight []float32, outCh, kSize, stride int) {
	inputLen := len(input)
	outLen := (inputLen - kSize) / stride + 1
	if outLen <= 0 {
		return
	}
	for oc := 0; oc < outCh; oc++ {
		wRow := weight[oc*kSize : oc*kSize+kSize]
		dstOff := oc * outLen
		for t := 0; t < outLen; t++ {
			inOff := t * stride
			dst[dstOff+t] = vek32.Dot(wRow, input[inOff:inOff+kSize])
		}
	}
}

// conv1dPad1Simd performs multi-channel Conv1D with padding=1.
// Uses pre-transposed weights WT [outCh, kSize, inCh] so that for each (oc, k),
// the sum over inCh is a contiguous dot product — ideal for SIMD.
// inColsBuf is pre-allocated [T][maxInCh] gather buffers (from scratch).
func conv1dPad1Simd(dst, input []float32, inCh, inLen int, weight, bias []float32, outCh, wInCh, kSize int, wt []float32, inColsBuf [][]float32) {
	if wt == nil || kSize != 3 || inLen <= 0 {
		conv1dPad1Into(dst, input, inCh, inLen, weight, bias, outCh, wInCh, kSize)
		return
	}

	// Gather input columns: inCols[t] = in[:, t] contiguous [inCh]
	// Clear to inCh to prevent stale data from previous layers (buffer is sized for maxInCh=129)
	for t := 0; t < inLen; t++ {
		col := inColsBuf[t]
		for ic := 0; ic < inCh; ic++ {
			col[ic] = input[ic*inLen+t]
		}
		// Zero tail if buffer is larger than current inCh
		for ic := inCh; ic < wInCh; ic++ {
			col[ic] = 0
		}
	}

	for oc := 0; oc < outCh; oc++ {
		biasVal := float32(0)
		if bias != nil && oc < len(bias) {
			biasVal = bias[oc]
		}
		dstOff := oc * inLen
		wtBase := oc * kSize * wInCh

		for t := 0; t < inLen; t++ {
			var sum float32
			if t > 0 {
				sum += vek32.Dot(wt[wtBase:wtBase+wInCh], inColsBuf[t-1][:wInCh])
			}
			sum += vek32.Dot(wt[wtBase+wInCh:wtBase+2*wInCh], inColsBuf[t][:wInCh])
			if t < inLen-1 {
				sum += vek32.Dot(wt[wtBase+2*wInCh:wtBase+3*wInCh], inColsBuf[t+1][:wInCh])
			}
			dst[dstOff+t] = sum + biasVal
		}
	}
}

// lstmCellSimd performs LSTM cell computation with SIMD-accelerated gate computation.
// The gates computation is 4*hidden dot products of length inputSize (W_ih @ x)
// plus 4*hidden dot products of length hidden (W_hh @ h).
// For hidden=128, each dot product is 128 floats — good SIMD utilization.
func lstmCellSimd(x, h, c, gates []float32,
	wIH, wHH, bIH, bHH []float32,
	inputSize, hidden int) {

	h4 := 4 * hidden

	// Compute gates = W_ih @ x + W_hh @ h + b_ih + b_hh
	// Each gate[i] = dot(wIH[i*inputSize : (i+1)*inputSize], x) +
	//                dot(wHH[i*hidden : (i+1)*hidden], h) + bIH[i] + bHH[i]
	for i := 0; i < h4; i++ {
		sum := vek32.Dot(wIH[i*inputSize:i*inputSize+inputSize], x[:inputSize])
		sum += vek32.Dot(wHH[i*hidden:i*hidden+hidden], h[:hidden])
		if bIH != nil {
			sum += bIH[i]
		}
		if bHH != nil {
			sum += bHH[i]
		}
		gates[i] = sum
	}

	// Gate activations + state update (scalar — activation functions aren't SIMD-friendly)
	for j := 0; j < hidden; j++ {
		iGate := sigmoid(gates[j])
		fGate := sigmoid(gates[hidden+j])
		gGate := tanhf(gates[2*hidden+j])
		oGate := sigmoid(gates[3*hidden+j])
		c[j] = fGate*c[j] + iGate*gGate
		h[j] = oGate * tanhf(c[j])
	}
}

// magnitudeSimd computes magnitude spectrum from STFT output using SIMD.
// stftOut: [258, T] flattened (first 129 = real, next 129 = imag)
// mag: [129, T] output
// Uses vek32 for element-wise operations where beneficial.
func magnitudeSimd(mag, stftOut []float32, freqBins, T int) {
	// For each frequency bin, compute sqrt(re^2 + im^2 + eps)
	// With T=3, the inner loop is too short for SIMD on the T dimension.
	// Instead, we process all freq bins for each time step.
	for t := 0; t < T; t++ {
		for f := 0; f < freqBins; f++ {
			re := stftOut[f*T+t]
			im := stftOut[(freqBins+f)*T+t]
			mag[f*T+t] = fastsqrt(re*re + im*im + 1e-8)
		}
	}
}

// fastsqrt computes sqrt using math.Sqrt which Go compiles to a single
// SQRTSD instruction on x86-64 (hardware sqrt, full precision).
func fastsqrt(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
