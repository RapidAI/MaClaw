package tts

// FlowReverseForward runs the TransformerCouplingBlock in reverse mode.
// z_p: [inter, T_mel] sampled latent
// g: [ginChannels, 1] speaker embedding
// Returns: z [inter, T_mel]
func FlowReverseForward(zP []float32, inter, tMel int,
	g []float32, ginCh int,
	flow *FlowWeights, hp HParams) []float32 {

	z := make([]float32, len(zP))
	copy(z, zP)

	// Reverse: iterate flows in reverse order.
	// Each coupling layer is at index i*2, Flip is at i*2+1.
	// In reverse: Flip first, then coupling layer reverse.
	nLayers := len(flow.Layers)
	for i := nLayers - 1; i >= 0; i-- {
		// Flip: reverse channel order
		FlipChannels(z, inter, tMel)

		// TransformerCouplingLayer reverse
		z = couplingLayerReverse(z, inter, tMel, g, ginCh, &flow.Layers[i], hp)
	}
	return z
}

// CouplingLayerReverseExported is an exported wrapper for testing.
func CouplingLayerReverseExported(z []float32, inter, tMel int,
	g []float32, ginCh int,
	layer *FlowCouplingLayer, hp HParams) []float32 {
	return couplingLayerReverse(z, inter, tMel, g, ginCh, layer, hp)
}

// couplingLayerReverse runs a single TransformerCouplingLayer in reverse.
// In forward: x0, x1 = split(x); m = enc(x0); x1 = x1 * exp(0) + m; x = concat(x0, x1)
// Since mean_only=True, logs=0, so exp(logs)=1.
// Reverse: x0, x1 = split(x); m = enc(x0); x1 = (x1 - m); x = concat(x0, x1)
func couplingLayerReverse(z []float32, inter, tMel int,
	g []float32, ginCh int,
	layer *FlowCouplingLayer, hp HParams) []float32 {

	halfCh := inter / 2

	// Split: x0 = z[:halfCh], x1 = z[halfCh:]
	x0 := z[:halfCh*tMel]
	x1 := z[halfCh*tMel:]

	// Pre-projection: [halfCh, T] → [hidden, T]
	hidden := hp.HiddenChannels
	h := Conv1D(x0, halfCh, tMel, layer.Pre.Weight, layer.Pre.KSize, hidden, 1,
		(layer.Pre.KSize-1)/2, layer.Pre.Bias)

	// FFT encoder layers (with speaker conditioning at layer 2)
	condLayerIdx := 2
	for j := range layer.Enc {
		if j == condLayerIdx && g != nil && ginCh > 0 && layer.SpkEmbLinear.Weight != nil {
			gInput := make([]float32, ginCh)
			copy(gInput, g[:ginCh])
			gProj := Conv1D(gInput, ginCh, 1, layer.SpkEmbLinear.Weight, 1, hidden, 1, 0, layer.SpkEmbLinear.Bias)
			for ch := 0; ch < hidden; ch++ {
				gVal := gProj[ch]
				for t := 0; t < tMel; t++ {
					h[ch*tMel+t] += gVal
				}
			}
		}
		h = encoderLayerForward(h, hidden, tMel, &layer.Enc[j], hp)
	}

	// Post-projection: [hidden, T] → [halfCh, T] (mean_only, so output is m)
	m := Conv1D(h, hidden, tMel, layer.Post.Weight, layer.Post.KSize, halfCh, 1,
		(layer.Post.KSize-1)/2, layer.Post.Bias)

	// Reverse affine: x1 = x1 - m (since mean_only=True, no scaling)
	for i := range x1 {
		x1[i] -= m[i]
	}

	// z is modified in-place (x0 unchanged, x1 updated)
	return z
}
