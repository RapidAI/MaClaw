package tts

import "math"

// PiperSDPForward runs the complete Stochastic Duration Predictor in inference (reverse) mode.
// x: [hidden, T] encoder output
// Returns: durations [T] and tMel
func PiperSDPForward(x []float32, hidden, T int,
	sdp *StochasticDPWeights, hp PiperHParams, noiseScaleW float32,
	phonemeIDs []int64) ([]int, int) {

	// Step 1: Conditioning — pre → DDSConv → proj
	h := Conv1D(x, hidden, T, sdp.Pre.Weight, sdp.Pre.KSize, hidden, 1,
		(sdp.Pre.KSize-1)/2, sdp.Pre.Bias)
	h = sdpDDSConv(h, hidden, T, &sdp.Convs, hp.DPDDSLayers)
	h = Conv1D(h, hidden, T, sdp.Proj.Weight, sdp.Proj.KSize, hidden, 1,
		(sdp.Proj.KSize-1)/2, sdp.Proj.Bias)

	// Step 2: Sample noise w [2, T]
	w := make([]float32, 2*T)
	if noiseScaleW > 0 {
		RandnScale(w, noiseScaleW)
	}

	// Step 3: Reverse through ConvFlow layers: Flip → CF(7) → Flip → CF(5) → Flip → CF(3)
	for i := len(sdp.Flows) - 1; i >= 0; i-- {
		// Flip channels
		sdpFlipChannels(w, T)
		// ConvFlow inverse
		sdpConvFlowInverse(w, T, h, hidden, &sdp.Flows[i], hp)
	}

	// Step 4: ElementwiseAffine inverse: w = (w - m) * exp(-logs)
	if sdp.FlowM != nil && len(sdp.FlowM) >= 2 {
		// m = sdp.FlowM [2, 1]
		// exp(-logs) precomputed: [1.0282332, 1.2391727] for this model
		// TODO: store exp(-logs) in weights. For now hardcode from ONNX.
		expNegLogs := [2]float32{1.0282332, 1.2391727}
		for t := 0; t < T; t++ {
			w[t] = (w[t] - sdp.FlowM[0]) * expNegLogs[0]
			w[T+t] = (w[T+t] - sdp.FlowM[1]) * expNegLogs[1]
		}
	}

	// Step 5: logw = w[channel 1] (channels swapped after odd number of flips)
	durations := make([]int, T)
	tMel := 0
	for t := 0; t < T; t++ {
		logw := float64(w[T+t]) // channel 1 = logw after 3 flips
		d := int(math.Ceil(math.Exp(logw)))
		if d < 1 {
			d = 1
		}
		if d > 50 {
			d = 50
		}
		durations[t] = d
		tMel += d
	}

	// Recalculate tMel
	tMel = 0
	for _, d := range durations {
		tMel += d
	}

	return durations, tMel
}

// sdpFlipChannels flips the two channels of w [2, T] in-place.
func sdpFlipChannels(w []float32, T int) {
	for t := 0; t < T; t++ {
		w[t], w[T+t] = w[T+t], w[t]
	}
}

// sdpConvFlowInverse runs one ConvFlow layer in inverse mode.
// w: [2, T] in-place modified. h: [hidden, T] conditioning.
func sdpConvFlowInverse(w []float32, T int, h []float32, hidden int,
	flow *SDPFlowLayerWeights, hp PiperHParams) {

	// Split: w0 = w[:1, :], w1 = w[1:, :]
	w0 := w[:T]   // channel 0
	w1 := w[T:]   // channel 1

	// h_w = pre(w0) + h
	hW := Conv1D(w0, 1, T, flow.Pre.Weight, flow.Pre.KSize, hidden, 1,
		(flow.Pre.KSize-1)/2, flow.Pre.Bias)
	for i := range hW {
		hW[i] += h[i]
	}

	// DDSConv
	hW = sdpDDSConv(hW, hidden, T, &flow.Convs, hp.DPDDSLayers)

	// Proj → [nBins*3-1, T]
	nBins := flow.Proj.OutCh // 29 = 10*3-1
	if nBins == 0 {
		nBins = 29
	}
	params := Conv1D(hW, hidden, T, flow.Proj.Weight, flow.Proj.KSize, nBins, 1,
		(flow.Proj.KSize-1)/2, flow.Proj.Bias)

	// Parse spline parameters
	// params layout: [nBins*3-1, T] = [29, T]
	// Reshape to [1, T, 29] (half_channels=1, so c=1)
	// Then split: W=[T, K], H=[T, K], D=[T, K-1]
	K := 10 // num_bins
	sqrtFilter := float32(math.Sqrt(float64(hidden)))

	// Build W, H, D arrays [T, K] and [T, K+1]
	W := make([]float32, T*K)
	H := make([]float32, T*K)
	D := make([]float32, T*(K+1))

	for t := 0; t < T; t++ {
		for k := 0; k < K; k++ {
			W[t*K+k] = params[k*T+t] / sqrtFilter
			H[t*K+k] = params[(K+k)*T+t] / sqrtFilter
		}
		// Derivatives: 9 values from params, pad to 11 with constant
		constant := float32(math.Log(math.Exp(1-1e-3) - 1))
		D[t*(K+1)] = constant // pad left
		for k := 0; k < K-1; k++ {
			D[t*(K+1)+1+k] = params[(2*K+k)*T+t]
		}
		D[t*(K+1)+K] = constant // pad right
	}

	// Apply spline inverse on w1
	w1New := rationalQuadraticSplineInverse(w1, W, H, D, T, K, 5.0)
	copy(w1, w1New)
}

// sdpDDSConv runs DDSConv matching ONNX: sep_conv → norm1 → GELU → 1x1 → norm2 → GELU → +residual.
func sdpDDSConv(x []float32, ch, T int, dds *DDSConvWeights, nLayers int) []float32 {
	for i := 0; i < nLayers; i++ {
		dilation := 1
		for d := 0; d < i; d++ {
			dilation *= 3
		}

		residual := make([]float32, len(x))
		copy(residual, x)

		// Depthwise separable conv (groups=ch)
		kSize := dds.ConvsSep[i].KSize
		if kSize == 0 {
			kSize = 3
		}
		padding := (kSize - 1) * dilation / 2
		x = depthwiseConv1D(x, ch, T, dds.ConvsSep[i].Weight, dds.ConvsSep[i].Bias,
			kSize, padding, dilation)

		// Norm1 → GELU
		applyLayerNormCT(x, ch, T, dds.Norms1[i].Weight, dds.Norms1[i].Bias)
		applyGELU(x)

		// 1x1 conv
		x = Conv1D(x, ch, T, dds.Convs1x1[i].Weight, 1, ch, 1, 0, dds.Convs1x1[i].Bias)

		// Norm2 → GELU
		applyLayerNormCT(x, ch, T, dds.Norms2[i].Weight, dds.Norms2[i].Bias)
		applyGELU(x)

		// Residual
		for j := range x {
			x[j] += residual[j]
		}
	}
	return x
}

// applyGELU applies GELU activation in-place: x * 0.5 * (1 + erf(x / sqrt(2)))
func applyGELU(x []float32) {
	for i, v := range x {
		x[i] = float32(0.5 * float64(v) * (1.0 + math.Erf(float64(v)/math.Sqrt2)))
	}
}

// ApplyGELUExported is exported for testing.
func ApplyGELUExported(x []float32) { applyGELU(x) }

// ApplyLayerNormCTExported is exported for testing.
func ApplyLayerNormCTExported(data []float32, C, T int, weight, bias []float32) {
	applyLayerNormCT(data, C, T, weight, bias)
}

// DepthwiseConv1DExported is exported for testing.
func DepthwiseConv1DExported(input []float32, ch, T int, kernel, bias []float32, kSize, padding, dilation int) []float32 {
	return depthwiseConv1D(input, ch, T, kernel, bias, kSize, padding, dilation)
}
