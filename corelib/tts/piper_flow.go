package tts

import "math"

// PiperFlowReverseForward runs the ResidualCouplingBlock flow decoder in reverse.
// z_p: [inter, T_mel] sampled latent
// g: [ginChannels] speaker embedding (unused in xiao_ya single-speaker, but kept for API compat)
// Returns: z [inter, T_mel]
func PiperFlowReverseForward(zP []float32, inter, tMel int,
	flow *PiperFlowWeights, hp PiperHParams) []float32 {

	z := make([]float32, len(zP))
	copy(z, zP)

	nLayers := len(flow.Layers)
	for i := nLayers - 1; i >= 0; i-- {
		FlipChannels(z, inter, tMel)
		z = residualCouplingReverse(z, inter, tMel, &flow.Layers[i], hp)

		// Debug: print RMS after each layer
		var rms float64
		for _, v := range z {
			rms += float64(v) * float64(v)
		}
		rms = math.Sqrt(rms / float64(len(z)))
		_ = rms
	}
	return z
}

// residualCouplingReverse runs one ResidualCouplingLayer in reverse.
// mean_only=True: x1 = x1 - m (no scaling)
func residualCouplingReverse(z []float32, inter, tMel int,
	layer *ResidualCouplingLayerWeights, hp PiperHParams) []float32 {

	halfCh := inter / 2
	hidden := hp.HiddenChannels

	// Split: x0 = z[:halfCh], x1 = z[halfCh:]
	x0 := z[:halfCh*tMel]
	x1 := z[halfCh*tMel:]

	// Pre-projection: [halfCh, T] → [hidden, T]
	h := Conv1D(x0, halfCh, tMel, layer.Pre.Weight, layer.Pre.KSize, hidden, 1,
		(layer.Pre.KSize-1)/2, layer.Pre.Bias)

	// WaveNet
	h = waveNetForward(h, hidden, tMel, layer.WN, hp)

	// Post-projection: [hidden, T] → [halfCh, T] (mean_only, output is m)
	m := Conv1D(h, hidden, tMel, layer.Post.Weight, layer.Post.KSize, halfCh, 1,
		(layer.Post.KSize-1)/2, layer.Post.Bias)

	// Reverse affine: x1 = x1 - m
	for i := range x1 {
		x1[i] -= m[i]
	}

	return z
}

// waveNetForward runs the WaveNet encoder inside a ResidualCouplingLayer.
// Uses dilated convolutions with gated activation (tanh * sigmoid).
// input: [hidden, T], output: [hidden, T]
func waveNetForward(x []float32, hidden, T int,
	layers []WaveNetLayerWeights, hp PiperHParams) []float32 {

	nLayers := len(layers)
	// Output accumulator for skip connections
	output := make([]float32, hidden*T)

	for i := 0; i < nLayers; i++ {
		wn := &layers[i]
		dilation := 1 // All WaveNet layers in this model use dilation=1

		// Dilated conv: [hidden, T] → [2*hidden, T]
		kSize := wn.InLayer.KSize
		if kSize == 0 {
			kSize = hp.WNKernelSize
		}
		padding := (kSize - 1) * dilation / 2

		acts := conv1DDilated(x, hidden, T, wn.InLayer.Weight, wn.InLayer.Bias,
			kSize, hidden*2, 1, padding, dilation)

		// Gated activation: tanh(acts[:hidden]) * sigmoid(acts[hidden:])
		gated := make([]float32, hidden*T)
		for c := 0; c < hidden; c++ {
			for t := 0; t < T; t++ {
				tVal := float32(math.Tanh(float64(acts[c*T+t])))
				sVal := float32(1.0 / (1.0 + math.Exp(float64(-acts[(hidden+c)*T+t]))))
				gated[c*T+t] = tVal * sVal
			}
		}

		// res_skip_layer: 1x1 conv
		rsOut := Conv1D(gated, hidden, T, wn.ResSkipLayer.Weight, 1,
			wn.ResSkipLayer.OutCh, 1, 0, wn.ResSkipLayer.Bias)

		if i < nLayers-1 {
			// Split into residual and skip
			// res_skip_layers output is [2*hidden, T] for non-last layers
			resCh := hidden
			skipCh := wn.ResSkipLayer.OutCh - hidden
			if skipCh <= 0 {
				skipCh = hidden
				resCh = 0
			}

			if wn.ResSkipLayer.OutCh == 2*hidden {
				// First half: residual, second half: skip
				for c := 0; c < hidden; c++ {
					for t := 0; t < T; t++ {
						x[c*T+t] += rsOut[c*T+t] // residual
					}
				}
				for c := 0; c < hidden; c++ {
					for t := 0; t < T; t++ {
						output[c*T+t] += rsOut[(hidden+c)*T+t] // skip
					}
				}
			} else {
				// All goes to residual
				_ = resCh
				for c := 0; c < hidden && c < wn.ResSkipLayer.OutCh; c++ {
					for t := 0; t < T; t++ {
						x[c*T+t] += rsOut[c*T+t]
					}
				}
			}
		} else {
			// Last layer: all goes to skip (output is [hidden, T])
			for c := 0; c < hidden && c < wn.ResSkipLayer.OutCh; c++ {
				for t := 0; t < T; t++ {
					output[c*T+t] += rsOut[c*T+t]
				}
			}
		}
	}

	return output
}
