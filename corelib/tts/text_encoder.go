package tts

import (
	"math"
)

// TextEncoderForward runs the MeloTTS text encoder.
// bert1024: [1024, T] or nil (for bert_proj)
// jaBert768: [768, T] or nil (for ja_bert_proj)
func TextEncoderForward(phonemeIDs, toneIDs, langIDs []int,
	g []float32, ginCh int,
	bert1024, jaBert768 []float32,
	te *TextEncoderWeights, hp HParams) (x, mP, logsP []float32, T int) {

	T = len(phonemeIDs)
	hidden := hp.HiddenChannels
	inter := hp.InterChannels
	sqrtH := float32(math.Sqrt(float64(hidden)))

	// Embedding lookup: emb(phoneme) + tone_emb(tone) + language_emb(lang)
	emb := make([]float32, T*hidden)
	for t := 0; t < T; t++ {
		pid := phonemeIDs[t]
		tid := toneIDs[t]
		lid := langIDs[t]
		for h := 0; h < hidden; h++ {
			var v float32
			if te.Emb != nil && pid*hidden+h < len(te.Emb) {
				v += te.Emb[pid*hidden+h]
			}
			if te.ToneEmb != nil && tid*hidden+h < len(te.ToneEmb) {
				v += te.ToneEmb[tid*hidden+h]
			}
			if te.LangEmb != nil && lid*hidden+h < len(te.LangEmb) {
				v += te.LangEmb[lid*hidden+h]
			}
			emb[t*hidden+h] = v * sqrtH
		}
	}

	// Add BERT projections
	// bert_proj: [1024, T] → [hidden, T]
	if bert1024 != nil && te.BertProj.Weight != nil {
		bertProj := Conv1D(bert1024, 1024, T, te.BertProj.Weight, 1, hidden, 1, 0, te.BertProj.Bias)
		// bertProj is [hidden, T], add to emb [T, hidden] after transpose
		for t := 0; t < T; t++ {
			for h := 0; h < hidden; h++ {
				emb[t*hidden+h] += bertProj[h*T+t]
			}
		}
	}

	// ja_bert_proj: [768, T] → [hidden, T]
	if jaBert768 != nil && te.JaBertProj.Weight != nil {
		jaBertProj := Conv1D(jaBert768, 768, T, te.JaBertProj.Weight, 1, hidden, 1, 0, te.JaBertProj.Bias)
		for t := 0; t < T; t++ {
			for h := 0; h < hidden; h++ {
				emb[t*hidden+h] += jaBertProj[h*T+t]
			}
		}
	}

	// Transpose to [hidden, T] for Conv1D operations
	x = make([]float32, hidden*T)
	for t := 0; t < T; t++ {
		for h := 0; h < hidden; h++ {
			x[h*T+t] = emb[t*hidden+h]
		}
	}

	// Encoder layers (post-norm architecture)
	// Speaker conditioning injected at cond_layer_idx=2
	condLayerIdx := 2

	for i := 0; i < hp.NLayers; i++ {
		// Speaker conditioning at the designated layer
		if i == condLayerIdx && g != nil && ginCh > 0 && te.SpkEmbLinear.Weight != nil {
			gInput := make([]float32, ginCh)
			copy(gInput, g[:ginCh])
			gProj := Conv1D(gInput, ginCh, 1, te.SpkEmbLinear.Weight, 1, hidden, 1, 0, te.SpkEmbLinear.Bias)
			for h := 0; h < hidden; h++ {
				gVal := gProj[h]
				for t := 0; t < T; t++ {
					x[h*T+t] += gVal
				}
			}
		}

		x = encoderLayerForward(x, hidden, T, &te.Layers[i], hp)
	}

	// Project to stats: [hidden, T] → [inter*2, T]
	stats := Conv1D(x, hidden, T, te.Proj.Weight, te.Proj.KSize, inter*2, 1,
		(te.Proj.KSize-1)/2, te.Proj.Bias)

	mP = stats[:inter*T]
	logsP = stats[inter*T:]

	return x, mP, logsP, T
}

// EncoderLayerForwardExported is an exported wrapper for testing.
func EncoderLayerForwardExported(x []float32, hidden, T int, layer *EncoderLayer, hp HParams) []float32 {
	return encoderLayerForward(x, hidden, T, layer, hp)
}

// encoderLayerForward runs one FFT encoder layer (POST-norm architecture).
// MeloTTS Encoder: y = attn(x); x = norm1(x + y); y = ffn(x); x = norm2(x + y)
func encoderLayerForward(x []float32, hidden, T int, layer *EncoderLayer, hp HParams) []float32 {

	nHeads := hp.NHeads
	headDim := hidden / nHeads

	// ── Self-Attention (on raw x, not normed) ──
	q := Conv1D(x, hidden, T, layer.Attn.ConvQ.Weight, 1, hidden, 1, 0, layer.Attn.ConvQ.Bias)
	k := Conv1D(x, hidden, T, layer.Attn.ConvK.Weight, 1, hidden, 1, 0, layer.Attn.ConvK.Bias)
	v := Conv1D(x, hidden, T, layer.Attn.ConvV.Weight, 1, hidden, 1, 0, layer.Attn.ConvV.Bias)

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))

	attnOut := make([]float32, hidden*T)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim

		// Compute standard attention scores: Q @ K^T / sqrt(d)
		scores := make([]float32, T*T)
		for tq := 0; tq < T; tq++ {
			for tk := 0; tk < T; tk++ {
				var dot float64 // float64 accumulator
				for d := 0; d < headDim; d++ {
					dot += float64(q[(hOff+d)*T+tq]) * float64(k[(hOff+d)*T+tk])
				}
				scores[tq*T+tk] = float32(dot * float64(scale))
			}
		}

		// Add relative position bias (window_size=4)
		if layer.Attn.EmbRelK != nil {
			addRelativePositionBias(scores, q, layer.Attn.EmbRelK,
				nHeads, h, headDim, T, 4, scale)
		}

		// Softmax per query (use precise exp)
		for tq := 0; tq < T; tq++ {
			row := scores[tq*T : (tq+1)*T]
			preciseSoftmax(row)
		}

		// Weighted sum of values
		for tq := 0; tq < T; tq++ {
			for d := 0; d < headDim; d++ {
				var sum float64 // float64 accumulator
				for tk := 0; tk < T; tk++ {
					sum += float64(scores[tq*T+tk]) * float64(v[(hOff+d)*T+tk])
				}
				attnOut[(hOff+d)*T+tq] = float32(sum)
			}
		}

		// Add relative position bias to values
		if layer.Attn.EmbRelV != nil {
			addRelativeValueBias(attnOut, scores, layer.Attn.EmbRelV,
				nHeads, h, headDim, T, 4)
		}
	}

	// Output projection
	y := Conv1D(attnOut, hidden, T, layer.Attn.ConvO.Weight, 1, hidden, 1, 0, layer.Attn.ConvO.Bias)

	// POST-norm: x = norm1(x + y)
	for i := range x {
		x[i] += y[i]
	}
	applyLayerNormCT(x, hidden, T, layer.Norm1.Weight, layer.Norm1.Bias)

	// ── FFN ──
	filter := layer.FFN.Conv1.OutCh
	if filter == 0 {
		filter = hp.FilterChannels
	}
	kSize := layer.FFN.Conv1.KSize
	if kSize == 0 {
		kSize = hp.KernelSize
	}

	ffn := Conv1D(x, hidden, T, layer.FFN.Conv1.Weight, kSize, filter, 1,
		(kSize-1)/2, layer.FFN.Conv1.Bias)
	ReLU(ffn)
	ffn = Conv1D(ffn, filter, T, layer.FFN.Conv2.Weight, kSize, hidden, 1,
		(kSize-1)/2, layer.FFN.Conv2.Bias)

	// POST-norm: x = norm2(x + y)
	for i := range x {
		x[i] += ffn[i]
	}
	applyLayerNormCT(x, hidden, T, layer.Norm2.Weight, layer.Norm2.Bias)

	return x
}

// addRelativePositionBias adds relative position bias to attention scores.
// emb_rel_k: [1, 2*window+1, headDim] (heads_share=True)
// scores: [T, T] for one head
func addRelativePositionBias(scores, q []float32, embRelK []float32,
	nHeads, headIdx, headDim, T, windowSize int, scale float32) {

	relLen := 2*windowSize + 1
	padLength := 0
	usedLen := 2*T - 1
	if T > windowSize+1 {
		padLength = T - (windowSize + 1)
	}

	sliceStart := 0
	if T <= windowSize+1 {
		sliceStart = windowSize + 1 - T
	}
	_ = sliceStart

	hOff := headIdx * headDim

	// Build padded relative embeddings
	goRelEmb := make([]float32, usedLen*headDim)
	for m := 0; m < usedLen; m++ {
		srcIdx := m - padLength
		for d := 0; d < headDim; d++ {
			if srcIdx >= 0 && srcIdx < relLen {
				goRelEmb[m*headDim+d] = embRelK[srcIdx*headDim+d]
			}
		}
	}

	// Q @ rel_K^T with float64 accumulator
	relLogits := make([]float32, T*usedLen)
	for tq := 0; tq < T; tq++ {
		for m := 0; m < usedLen; m++ {
			var dot float64
			for d := 0; d < headDim; d++ {
				dot += float64(q[(hOff+d)*T+tq]) * float64(goRelEmb[m*headDim+d])
			}
			relLogits[tq*usedLen+m] = float32(dot * float64(scale))
		}
	}

	// _relative_position_to_absolute_position
	padded := make([]float32, T*(usedLen+1))
	for tq := 0; tq < T; tq++ {
		copy(padded[tq*(usedLen+1):], relLogits[tq*usedLen:(tq+1)*usedLen])
	}
	flat := make([]float32, T*(usedLen+1)+T-1)
	copy(flat, padded)
	for tq := 0; tq < T; tq++ {
		for tk := 0; tk < T; tk++ {
			idx := tq*usedLen + (T - 1 + tk)
			if idx < len(flat) {
				scores[tq*T+tk] += flat[idx]
			}
		}
	}
}

// addRelativeValueBias adds relative position bias to attention output values.
// pAttn: [T, T] attention weights (after softmax) for one head
// embRelV: raw [1, 2*window+1, headDim] embedding
// attnOut: [hidden, T] — modified in place for this head
func addRelativeValueBias(attnOut []float32, pAttn []float32, embRelV []float32,
	nHeads, headIdx, headDim, T, windowSize int) {

	relLen := 2*windowSize + 1
	padLength := 0
	usedLen := 2*T - 1
	if T > windowSize+1 {
		padLength = T - (windowSize + 1)
	}

	// Build padded relative value embeddings [usedLen, headDim]
	relEmbV := make([]float32, usedLen*headDim)
	for m := 0; m < usedLen; m++ {
		srcIdx := m - padLength
		for d := 0; d < headDim; d++ {
			if srcIdx >= 0 && srcIdx < relLen {
				relEmbV[m*headDim+d] = embRelV[srcIdx*headDim+d]
			}
		}
	}

	hOff := headIdx * headDim

	// _absolute_position_to_relative_position: [T, T] → [T, 2T-1]
	// Step 1: Pad right with T-1 zeros: [T, T] → [T, T+T-1]
	padWidth := T - 1
	rowPadded := T + padWidth // T + T - 1 = 2T - 1

	// Step 2: Flatten to [T * (2T-1)]
	flatLen := T * rowPadded

	// Step 3: Pad left with T zeros → [T + T*(2T-1)]
	totalLen := T + flatLen

	flat := make([]float32, totalLen)
	// Fill: skip first T zeros, then interleave rows with padding
	for tq := 0; tq < T; tq++ {
		for tk := 0; tk < T; tk++ {
			flat[T+tq*rowPadded+tk] = pAttn[tq*T+tk]
		}
		// positions [T + tq*rowPadded + T .. T + (tq+1)*rowPadded - 1] are zero (padding)
	}

	// Step 4: Reshape to [T, 2T] and slice [:, 1:] → [T, 2T-1]
	reshapeRow := 2 * T
	for tq := 0; tq < T; tq++ {
		for m := 0; m < usedLen; m++ {
			idx := tq*reshapeRow + m + 1 // +1 for the [:, 1:] slice
			if idx >= totalLen {
				continue
			}
			w := flat[idx]
			if w == 0 {
				continue
			}
			for d := 0; d < headDim; d++ {
				attnOut[(hOff+d)*T+tq] += w * relEmbV[m*headDim+d]
			}
		}
	}
}

// preciseSoftmax computes softmax using math.Exp (not fastExp) for TTS precision.
func preciseSoftmax(x []float32) {
	if len(x) == 0 {
		return
	}
	max := x[0]
	for _, v := range x {
		if v > max {
			max = v
		}
	}
	var sum float32
	for i := range x {
		v := float32(math.Exp(float64(x[i] - max)))
		x[i] = v
		sum += v
	}
	if sum != 0 {
		inv := 1.0 / sum
		for i := range x {
			x[i] *= inv
		}
	}
}

// layerNormWithBias computes LayerNorm with bias.
func layerNormWithBias(x, weight, bias []float32, eps float32) {
	n := len(x)
	if n == 0 || weight == nil {
		return
	}
	var mean float32
	for _, v := range x {
		mean += v
	}
	mean /= float32(n)

	var variance float32
	for _, v := range x {
		d := v - mean
		variance += d * d
	}
	variance /= float32(n)
	scale := 1.0 / float32(math.Sqrt(float64(variance+eps)))

	for i := 0; i < n; i++ {
		v := (x[i] - mean) * scale
		if i < len(weight) {
			v *= weight[i]
		}
		if bias != nil && i < len(bias) {
			v += bias[i]
		}
		x[i] = v
	}
}

// applyLayerNormCT applies LayerNorm to a [C, T] tensor along C for each time step.
func applyLayerNormCT(data []float32, C, T int, weight, bias []float32) {
	if weight == nil {
		return
	}
	col := make([]float32, C)
	for t := 0; t < T; t++ {
		for c := 0; c < C; c++ {
			col[c] = data[c*T+t]
		}
		layerNormWithBias(col, weight, bias, 1e-5)
		for c := 0; c < C; c++ {
			data[c*T+t] = col[c]
		}
	}
}
