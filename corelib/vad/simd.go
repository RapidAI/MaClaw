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

// conv1dPad1Simd performs multi-channel Conv1D with padding=1 and stride.
// Uses pre-transposed weights WT [outCh, kSize, inCh] so that for each (oc, k),
// the sum over inCh is a contiguous dot product — ideal for SIMD.
// inColsBuf is pre-allocated [T][maxInCh] gather buffers (from scratch).
// outLen = (inLen + 2 - kSize) / stride + 1 (PyTorch conv1d k=3 pad=1 semantics).
func conv1dPad1Simd(dst, input []float32, inCh, inLen int, weight, bias []float32, outCh, wInCh, kSize, stride int, wt []float32, inColsBuf [][]float32) {
	if stride == 1 && (wt == nil || kSize != 3 || inLen <= 0) {
		conv1dPad1Into(dst, input, inCh, inLen, weight, bias, outCh, wInCh, kSize)
		return
	}
	outLen := (inLen-kSize+2)/stride + 1
	if outLen <= 0 {
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
		dstOff := oc * outLen
		wtBase := oc * kSize * wInCh

		for t := 0; t < outLen; t++ {
			c := t * stride
			var sum float32
			if c > 0 {
				sum += vek32.Dot(wt[wtBase:wtBase+wInCh], inColsBuf[c-1][:wInCh])
			}
			sum += vek32.Dot(wt[wtBase+wInCh:wtBase+2*wInCh], inColsBuf[c][:wInCh])
			if c+1 < inLen {
				sum += vek32.Dot(wt[wtBase+2*wInCh:wtBase+3*wInCh], inColsBuf[c+1][:wInCh])
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

// magnitudeFromSTFT computes the magnitude spectrum of stftOut frames
// [skip, skip+T). The reference v5 frontend drops the first STFT frame
// (it mostly covers the reflection-padded region), so skip=1 in practice.
// stftOut: [2*freqBins, T+skip] flattened (first freqBins rows = real, then imag)
// mag: [freqBins, T] output
func magnitudeFromSTFT(mag, stftOut []float32, freqBins, T, skip int) {
	totalT := T + skip
	for t := 0; t < T; t++ {
		for f := 0; f < freqBins; f++ {
			re := stftOut[f*totalT+t+skip]
			im := stftOut[(freqBins+f)*totalT+t+skip]
			mag[f*T+t] = fastsqrt(re*re + im*im)
		}
	}
}

// fastsqrt computes sqrt using math.Sqrt which Go compiles to a single
// SQRTSD instruction on x86-64 (hardware sqrt, full precision).
func fastsqrt(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}
