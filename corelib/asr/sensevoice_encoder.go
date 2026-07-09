// corelib/asr/sensevoice_encoder.go — SAN-M encoder forward pass with FSMN.
//
// SANM (Self-Attention Network with Memory) block:
//   LayerNorm1 → QKV linear → Multi-Head Attention → FSMN(V) → sum + residual
//   → LayerNorm2 → FFN(ReLU) → residual
//
// FSMN: depthwise 1D convolution on V with kernel_size=11, padding=5.
//
// Performance notes:
//   - MatMul / Q8 path already uses AVX2/NEON via corelib/embedding/tensor.
//   - LayerNorm, bias, residual, ReLU, attention dots, FSMN use vek32 SIMD.
//   - Per-block heap allocations eliminated via ping-pong encoder buffers.
//   - Positional encodings and fbank windows are cached.
package asr

import (
	"math"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// encode runs the full SenseVoice encoder.
// Input: lfrFeats [lfrFrames, svFeatsDim], Output: [totalFrames, vocabSize] logits.
func (m *SenseVoiceModel) encode(lfrFeats []float32, lfrFrames int) []float32 {
	hp := m.hp
	hidden := hp.HiddenSize
	featsDim := hp.FeatsDim
	totalFrames := 4 + lfrFrames

	bufs := m.ensureEncBufs(totalFrames)

	// Build input sequence into featsBuf: [4 prompt embeddings ; LFR features]
	featsN := totalFrames * featsDim
	xFeats := bufs.featsBuf[:featsN]
	// Zero first so unused embedding dims stay clean if embedding rows short
	clear(xFeats)

	if m.w.embedding != nil {
		promptIDs := [4]int{0, 1, 2, 14}
		embRows := len(m.w.embedding) / featsDim
		for i, pid := range promptIDs {
			if pid < embRows {
				copy(xFeats[i*featsDim:(i+1)*featsDim], m.w.embedding[pid*featsDim:(pid+1)*featsDim])
			}
		}
	}
	copy(xFeats[4*featsDim:], lfrFeats[:lfrFrames*featsDim])

	// Scale by sqrt(hidden) + add positional encoding in one pass.
	scale := float32(math.Sqrt(float64(hidden)))
	svScaleAddPosEncoding(xFeats, totalFrames, featsDim, scale, bufs)

	// Entry block: 560-dim → 512-dim into bufA
	x := bufs.bufA[:totalFrames*hidden]
	m.sanmBlockInto(xFeats, totalFrames, featsDim, &m.w.encoder0, x, bufs)

	// Main + TP blocks: ping-pong between bufA and bufB (no per-layer alloc)
	cur, nxt := x, bufs.bufB[:totalFrames*hidden]
	for i := range m.w.encoders {
		m.sanmBlockInto(cur, totalFrames, hidden, &m.w.encoders[i], nxt, bufs)
		cur, nxt = nxt, cur
	}

	svLayerNormBias(cur, totalFrames, hidden, m.w.afterNormW, m.w.afterNormB)

	for i := range m.w.tpEncoders {
		m.sanmBlockInto(cur, totalFrames, hidden, &m.w.tpEncoders[i], nxt, bufs)
		cur, nxt = nxt, cur
	}

	svLayerNormBias(cur, totalFrames, hidden, m.w.tpNormW, m.w.tpNormB)

	// CTC head — return buffer directly; caller holds m.mu and consumes before next encode.
	vocab := hp.VocabSize
	logits := bufs.logits[:totalFrames*vocab]
	matMulLinearBias(logits, cur, m.w.ctcW, m.w.ctcB, totalFrames, vocab, hidden)
	return logits
}

// sanmBlockInto executes one SAN-M encoder layer, writing [nFrames, hidden] into out.
// x is [nFrames, inDim]; out must have capacity nFrames*hidden.
// When sameInOut, x is preserved (ping-pong buffer) and used as residual without an extra copy.
func (m *SenseVoiceModel) sanmBlockInto(x []float32, nFrames, inDim int, l *svLayerWeights, out []float32, bufs *svEncoderBufs) {
	hidden := m.hp.HiddenSize
	nHeads := m.hp.NumHeads
	headDim := hidden / nHeads
	n := nFrames * hidden
	sameInOut := inDim == hidden

	// LayerNorm1: write into residual2 from x (no copy). Residual for sameInOut is x.
	var normSrc []float32
	if sameInOut {
		svLayerNormBiasInto(bufs.residual2[:n], x[:n], nFrames, inDim, l.norm1W, l.norm1B)
		normSrc = bufs.residual2[:n]
	} else {
		// Entry: mutate x (feats buffer) in place — residual not used.
		svLayerNormBias(x, nFrames, inDim, l.norm1W, l.norm1B)
		normSrc = x
	}

	// QKV: fused path keeps interleaved [Q|K|V] layout (no 3× copy split).
	// Separate path fills q/k/v buffers.
	useFused := l.fusedQKV && l.qW.Rows() > 0
	var q, k, v []float32
	var qkv []float32
	if useFused {
		qkv = bufs.qkv[:nFrames*3*hidden]
		matMulLinearBias(qkv, normSrc, l.qW, l.qB.f32, nFrames, 3*hidden, inDim)
	} else if l.qW.Rows() > 0 {
		q = bufs.q[:n]
		k = bufs.k[:n]
		v = bufs.v[:n]
		matMulLinearBias(q, normSrc, l.qW, l.qB.f32, nFrames, hidden, inDim)
		matMulLinearBias(k, normSrc, l.kW, l.kB.f32, nFrames, hidden, inDim)
		matMulLinearBias(v, normSrc, l.vW, l.vB.f32, nFrames, hidden, inDim)
	} else {
		if sameInOut {
			copy(out[:n], x[:n])
		} else {
			clear(out[:n])
		}
		return
	}

	// Attention → projOut
	attnOut := bufs.attnOut[:n]
	if useFused {
		svMultiHeadAttentionFused(attnOut, qkv, nFrames, nHeads, headDim, hidden, bufs)
	} else {
		svMultiHeadAttentionInto(attnOut, q, k, v, nFrames, nHeads, headDim, bufs)
	}

	projOut := bufs.projOut[:n]
	matMulLinearBias(projOut, attnOut, l.outW, l.outB.f32, nFrames, hidden, hidden)

	// FSMN(V) + V residual, then + projOut (+ input residual)
	fsmnOut := bufs.fsmnOut[:n]
	if useFused {
		// V is third plane of interleaved QKV [Q|K|V], stride = 3*hidden
		svFSMNStrided(fsmnOut, qkv, 2*hidden, 3*hidden, nFrames, hidden, l.fsmnW, m.hp.FSMNKernel, bufs)
		// residual V: add V plane into fsmnOut (strided)
		svAddStridedInplace(fsmnOut, qkv, nFrames, hidden, 2*hidden, 3*hidden)
	} else {
		svFSMNInto(fsmnOut, v, l.fsmnW, nFrames, hidden, m.hp.FSMNKernel, bufs)
		vek32.Add_Inplace(fsmnOut, v)
	}
	// out = projOut + fsmnOut [+ x residual] — this is the FFN residual.
	vek32.Add_Into(out[:n], projOut, fsmnOut)
	if sameInOut {
		vek32.Add_Inplace(out[:n], x[:n])
	}

	// FFN without residual copy:
	//   residual stays in out; LN writes residual2; ff2 writes projOut; out += projOut.
	// residual2 / projOut are free after attention residual.
	svLayerNormBiasInto(bufs.residual2[:n], out[:n], nFrames, hidden, l.norm2W, l.norm2B)

	ffDim := l.ff1W.Rows()
	ffOut := bufs.ffOut[:nFrames*ffDim]
	matMulLinearBiasReLU(ffOut, bufs.residual2[:n], l.ff1W, l.ff1B.f32, nFrames, ffDim, hidden)

	matMulLinearBias(projOut, ffOut, l.ff2W, l.ff2B.f32, nFrames, hidden, ffDim)
	vek32.Add_Inplace(out[:n], projOut)
}

// svAddStridedInplace: dst[f] += src[f*stride+baseOff] for f in [0,nFrames).
func svAddStridedInplace(dst, src []float32, nFrames, dim, baseOff, stride int) {
	for f := 0; f < nFrames; f++ {
		d := dst[f*dim : (f+1)*dim]
		s := src[f*stride+baseOff : f*stride+baseOff+dim]
		// Unrolled for auto-vectorization
		i := 0
		for ; i+7 < dim; i += 8 {
			d[i] += s[i]
			d[i+1] += s[i+1]
			d[i+2] += s[i+2]
			d[i+3] += s[i+3]
			d[i+4] += s[i+4]
			d[i+5] += s[i+5]
			d[i+6] += s[i+6]
			d[i+7] += s[i+7]
		}
		for ; i < dim; i++ {
			d[i] += s[i]
		}
	}
}

// svMultiHeadAttentionInto computes scaled dot-product attention into out.
// q,k,v,out: [nFrames, hidden] contiguous layout.
// Packs each head's K once, then multiDot4/8 Q-tiles against every K (B amortization).
func svMultiHeadAttentionInto(out, q, k, v []float32, nFrames, nHeads, headDim int, bufs *svEncoderBufs) {
	hidden := nHeads * headDim
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		// Pack K head into contiguous [nFrames, headDim] using residual2 scratch.
		kPack := bufs.residual2[:nFrames*headDim]
		packStridedHead(kPack, k, nFrames, hidden, hOff, headDim)
		svAttnHeadPacked(out, q, kPack, v, nFrames, hidden, hOff, headDim, scale, bufs)
	}
}

// svMultiHeadAttentionFused: qkv is [nFrames, 3*hidden] with layout [Q|K|V] per frame.
// Packs K per head for multiDot score kernels; V stays strided for weighted sum.
func svMultiHeadAttentionFused(out, qkv []float32, nFrames, nHeads, headDim, hidden int, bufs *svEncoderBufs) {
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	qkvStride := 3 * hidden
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		// Pack K (middle plane of QKV) — reuse unused k buffer.
		kPack := bufs.k[:nFrames*headDim]
		packStridedHead(kPack, qkv, nFrames, qkvStride, hidden+hOff, headDim)
		svAttnHeadPackedFused(out, qkv, kPack, nFrames, hidden, qkvStride, hOff, headDim, scale, bufs)
	}
}

// packStridedHead copies head slices from strided rows into contiguous dst [nFrames*headDim].
func packStridedHead(dst, src []float32, nFrames, stride, baseOff, headDim int) {
	// Unrolled copy for common headDim=128 (SenseVoice).
	if headDim == 128 {
		for f := 0; f < nFrames; f++ {
			s := src[f*stride+baseOff:]
			d := dst[f*128:]
			copy(d[:128], s[:128])
		}
		return
	}
	for f := 0; f < nFrames; f++ {
		off := f*stride + baseOff
		copy(dst[f*headDim:(f+1)*headDim], src[off:off+headDim])
	}
}

// ensureAttnScratch returns score rows [8*nFrames] and Q panel [8*headDim].
func ensureAttnScratch(bufs *svEncoderBufs, nFrames, headDim int) (scores, qPanel []float32) {
	need := 8*nFrames + 8*headDim
	if cap(bufs.scoresScratch) < need {
		bufs.scoresScratch = make([]float32, need)
	}
	scores = bufs.scoresScratch[:8*nFrames]
	qPanel = bufs.scoresScratch[8*nFrames : 8*nFrames+8*headDim]
	return
}

// svAttnHeadPacked: Q/V strided by hidden; K is packed contiguous [nFrames, headDim].
func svAttnHeadPacked(out, q, kPack, v []float32, nFrames, hidden, hOff, headDim int, scale float32, bufs *svEncoderBufs) {
	scores, qPanel := ensureAttnScratch(bufs, nFrames, headDim)
	// V head starts at hOff within each hidden-strided row.
	svAttnScoresTiled(out, q, kPack, v, scores, qPanel, nFrames, hidden, hidden, hOff, hOff, headDim, scale, false)
}

// svAttnHeadPackedFused: Q/V in interleaved QKV; K packed contiguous.
func svAttnHeadPackedFused(out, qkv, kPack []float32, nFrames, hidden, qkvStride, hOff, headDim int, scale float32, bufs *svEncoderBufs) {
	scores, qPanel := ensureAttnScratch(bufs, nFrames, headDim)
	// Fused path: q buffer is free — use it for denser Q panel locality.
	if cap(bufs.q) >= 8*headDim {
		qPanel = bufs.q[:8*headDim]
	}
	svAttnScoresTiled(out, qkv, kPack, qkv, scores, qPanel, nFrames, hidden, qkvStride, hOff, 2*hidden+hOff, headDim, scale, true)
}

// svAttnScoresTiled computes Q-tiles of 8/4 against packed K via MultiDot, then softmax@V.
// When fused=true, Q lives at base qSrc[f*qStride+hOff] and V at vSrc[f*vStride+vBaseOff].
// When fused=false, Q/V use the same hidden stride with offsets hOff / hOff.
func svAttnScoresTiled(out, qSrc, kPack, vSrc, scores, qPanel []float32, nFrames, hidden, qStride, hOff, vBaseOff, headDim int, scale float32, fused bool) {
	vStride := hidden
	if fused {
		vStride = qStride
	}
	qf := 0
	for ; qf+7 < nFrames; qf += 8 {
		for t := 0; t < 8; t++ {
			src := qSrc[(qf+t)*qStride+hOff : (qf+t)*qStride+hOff+headDim]
			copy(qPanel[t*headDim:(t+1)*headDim], src)
		}
		// Dual-K: MultiDot4DualB amortizes Q loads across two K vectors.
		// Process Q rows 0-3 and 4-7 separately (each dual×4).
		var dLo, dHi [8]float32
		kf := 0
		for ; kf+1 < nFrames; kf += 2 {
			k0 := kPack[kf*headDim : (kf+1)*headDim]
			k1 := kPack[(kf+1)*headDim : (kf+2)*headDim]
			tensor.MultiDot4DualB(&dLo, qPanel[:4*headDim], k0, k1, headDim)
			tensor.MultiDot4DualB(&dHi, qPanel[4*headDim:8*headDim], k0, k1, headDim)
			scores[0*nFrames+kf] = dLo[0] * scale
			scores[1*nFrames+kf] = dLo[1] * scale
			scores[2*nFrames+kf] = dLo[2] * scale
			scores[3*nFrames+kf] = dLo[3] * scale
			scores[0*nFrames+kf+1] = dLo[4] * scale
			scores[1*nFrames+kf+1] = dLo[5] * scale
			scores[2*nFrames+kf+1] = dLo[6] * scale
			scores[3*nFrames+kf+1] = dLo[7] * scale
			scores[4*nFrames+kf] = dHi[0] * scale
			scores[5*nFrames+kf] = dHi[1] * scale
			scores[6*nFrames+kf] = dHi[2] * scale
			scores[7*nFrames+kf] = dHi[3] * scale
			scores[4*nFrames+kf+1] = dHi[4] * scale
			scores[5*nFrames+kf+1] = dHi[5] * scale
			scores[6*nFrames+kf+1] = dHi[6] * scale
			scores[7*nFrames+kf+1] = dHi[7] * scale
		}
		if kf < nFrames {
			var d8 [8]float32
			tensor.MultiDot8(&d8, qPanel, kPack[kf*headDim:(kf+1)*headDim], headDim)
			scores[0*nFrames+kf] = d8[0] * scale
			scores[1*nFrames+kf] = d8[1] * scale
			scores[2*nFrames+kf] = d8[2] * scale
			scores[3*nFrames+kf] = d8[3] * scale
			scores[4*nFrames+kf] = d8[4] * scale
			scores[5*nFrames+kf] = d8[5] * scale
			scores[6*nFrames+kf] = d8[6] * scale
			scores[7*nFrames+kf] = d8[7] * scale
		}
		for t := 0; t < 8; t++ {
			oOff := (qf+t)*hidden + hOff
			sc := scores[t*nFrames : (t+1)*nFrames]
			tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+headDim], sc, vSrc[vBaseOff:], nFrames, vStride, headDim)
		}
	}
	for ; qf+3 < nFrames; qf += 4 {
		for t := 0; t < 4; t++ {
			src := qSrc[(qf+t)*qStride+hOff : (qf+t)*qStride+hOff+headDim]
			copy(qPanel[t*headDim:(t+1)*headDim], src)
		}
		var dDual [8]float32
		kf := 0
		for ; kf+1 < nFrames; kf += 2 {
			tensor.MultiDot4DualB(&dDual, qPanel[:4*headDim],
				kPack[kf*headDim:(kf+1)*headDim],
				kPack[(kf+1)*headDim:(kf+2)*headDim], headDim)
			scores[0*nFrames+kf] = dDual[0] * scale
			scores[1*nFrames+kf] = dDual[1] * scale
			scores[2*nFrames+kf] = dDual[2] * scale
			scores[3*nFrames+kf] = dDual[3] * scale
			scores[0*nFrames+kf+1] = dDual[4] * scale
			scores[1*nFrames+kf+1] = dDual[5] * scale
			scores[2*nFrames+kf+1] = dDual[6] * scale
			scores[3*nFrames+kf+1] = dDual[7] * scale
		}
		if kf < nFrames {
			var d4 [4]float32
			tensor.MultiDot4(&d4, qPanel[:4*headDim], kPack[kf*headDim:(kf+1)*headDim], headDim)
			scores[0*nFrames+kf] = d4[0] * scale
			scores[1*nFrames+kf] = d4[1] * scale
			scores[2*nFrames+kf] = d4[2] * scale
			scores[3*nFrames+kf] = d4[3] * scale
		}
		for t := 0; t < 4; t++ {
			oOff := (qf+t)*hidden + hOff
			sc := scores[t*nFrames : (t+1)*nFrames]
			tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+headDim], sc, vSrc[vBaseOff:], nFrames, vStride, headDim)
		}
	}
	for ; qf < nFrames; qf++ {
		qVec := qSrc[qf*qStride+hOff : qf*qStride+hOff+headDim]
		sc := scores[:nFrames]
		for kf := 0; kf < nFrames; kf++ {
			sc[kf] = vek32.Dot(qVec, kPack[kf*headDim:(kf+1)*headDim]) * scale
		}
		oOff := qf*hidden + hOff
		tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+headDim], sc, vSrc[vBaseOff:], nFrames, vStride, headDim)
	}
}

// svAttnHead is the legacy single-Q path (used by diagnostic wrappers).
func svAttnHead(out, q, k, v, scores []float32, nFrames, hidden, h, headDim int, scale float32) {
	hOff := h * headDim
	for qf := 0; qf < nFrames; qf++ {
		qVec := q[qf*hidden+hOff : qf*hidden+hOff+headDim]
		for kf := 0; kf < nFrames; kf++ {
			scores[kf] = vek32.Dot(qVec, k[kf*hidden+hOff:kf*hidden+hOff+headDim]) * scale
		}
		oOff := qf*hidden + hOff
		tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+headDim], scores[:nFrames], v[hOff:], nFrames, hidden, headDim)
	}
}

// svFSMNInto applies depthwise 1D conv (FSMN) into out.
// kernel layout: [ki][ch] = ki*hidden+ch (GGUF column-major).
// For each frame: out_f = sum_ki (v_{f+ki-pad} ⊙ kernel_ki)  — elementwise mul + add (SIMD).
func svFSMNInto(out, v, kernel []float32, nFrames, hidden, kernelSize int, bufs *svEncoderBufs) {
	svFSMNStrided(out, v, 0, hidden, nFrames, hidden, kernel, kernelSize, bufs)
}

// svFSMNStrided is FSMN over a strided V layout (e.g. fused QKV with V at baseOff).
// vFrame f starts at f*stride + baseOff and is contiguous for `hidden` floats.
// Uses fused out += v⊙k (no temporary mul buffer).
func svFSMNStrided(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, kernelSize int, bufs *svEncoderBufs) {
	if kernel == nil {
		clear(out[:nFrames*hidden])
		return
	}
	pad := (kernelSize - 1) / 2

	for f := 0; f < nFrames; f++ {
		outRow := out[f*hidden : (f+1)*hidden]
		seeded := false
		for ki := 0; ki < kernelSize; ki++ {
			srcFrame := f + ki - pad
			if srcFrame < 0 || srcFrame >= nFrames {
				continue
			}
			vOff := srcFrame*stride + baseOff
			vRow := v[vOff : vOff+hidden]
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			if !seeded {
				vek32.Mul_Into(outRow, vRow, kRow)
				seeded = true
				continue
			}
			tensor.FmaddInto(outRow, vRow, kRow)
		}
		if !seeded {
			clear(outRow)
		}
	}
}

// ensurePosEnc grows the sinusoidal PE cache to nFrames×dim.
func ensurePosEnc(nFrames, dim int, bufs *svEncoderBufs) {
	need := nFrames * dim
	if bufs.posEncDim != dim {
		bufs.posEnc = nil
		bufs.posEncDim = dim
	}
	oldFrames := 0
	if len(bufs.posEnc) > 0 {
		oldFrames = len(bufs.posEnc) / dim
	}
	if nFrames <= oldFrames {
		return
	}
	pe := make([]float32, need)
	if oldFrames > 0 {
		copy(pe, bufs.posEnc[:oldFrames*dim])
	}
	halfDim := dim / 2
	for pos := oldFrames; pos < nFrames; pos++ {
		off := pos * dim
		k := float64(pos + 1) // 1-indexed (match C++)
		for i := 0; i < halfDim; i++ {
			freq := math.Pow(10000.0, -2.0*float64(i)/float64(dim))
			angle := k * freq
			pe[off+i] = float32(math.Sin(angle))
			pe[off+i+halfDim] = float32(math.Cos(angle))
		}
	}
	bufs.posEnc = pe
}

// svAddPosEncodingCached adds sinusoidal PE using a lazily grown cache.
func svAddPosEncodingCached(x []float32, nFrames, dim int, bufs *svEncoderBufs) {
	ensurePosEnc(nFrames, dim, bufs)
	vek32.Add_Inplace(x[:nFrames*dim], bufs.posEnc[:nFrames*dim])
}

// svScaleAddPosEncoding computes x = x*scale + PE in one pass.
func svScaleAddPosEncoding(x []float32, nFrames, dim int, scale float32, bufs *svEncoderBufs) {
	ensurePosEnc(nFrames, dim, bufs)
	need := nFrames * dim
	pe := bufs.posEnc[:need]
	i := 0
	for ; i+3 < need; i += 4 {
		x[i] = x[i]*scale + pe[i]
		x[i+1] = x[i+1]*scale + pe[i+1]
		x[i+2] = x[i+2]*scale + pe[i+2]
		x[i+3] = x[i+3]*scale + pe[i+3]
	}
	for ; i < need; i++ {
		x[i] = x[i]*scale + pe[i]
	}
}

// fastInvSqrt32 approximates 1/sqrt(x) with one Newton step (Quake-style).
// Enough for LayerNorm (eps padded); avoids math.Sqrt float64 conversion.
func fastInvSqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	// Initial guess via bit hack
	bits := math.Float32bits(x)
	bits = 0x5f3759df - (bits >> 1)
	y := math.Float32frombits(bits)
	// One Newton iteration: y = y*(1.5 - 0.5*x*y*y)
	y = y * (1.5 - 0.5*x*y*y)
	// Second iteration for ~1e-5 relative accuracy on LayerNorm scales
	y = y * (1.5 - 0.5*x*y*y)
	return y
}

// svLayerNormBias applies LayerNorm with affine in-place.
func svLayerNormBias(x []float32, nFrames, dim int, w, b []float32) {
	svLayerNormBiasInto(x, x, nFrames, dim, w, b)
}

// svLayerNormBiasInto writes LayerNorm(src) into dst (may alias).
// Stats via SIMD Sum/Dot; normalize+affine fused into one store pass.
func svLayerNormBiasInto(dst, src []float32, nFrames, dim int, w, b []float32) {
	if w == nil || dim == 0 {
		return
	}
	const eps = 1e-5
	invDim := 1.0 / float32(dim)
	for f := 0; f < nFrames; f++ {
		srow := src[f*dim : (f+1)*dim]
		drow := dst[f*dim : (f+1)*dim]
		mean := vek32.Sum(srow) * invDim
		variance := vek32.Dot(srow, srow)*invDim - mean*mean
		if variance < 0 {
			variance = 0
		}
		invStd := fastInvSqrt32(variance + eps)
		if b != nil {
			i := 0
			for ; i+7 < dim; i += 8 {
				drow[i] = (srow[i]-mean)*invStd*w[i] + b[i]
				drow[i+1] = (srow[i+1]-mean)*invStd*w[i+1] + b[i+1]
				drow[i+2] = (srow[i+2]-mean)*invStd*w[i+2] + b[i+2]
				drow[i+3] = (srow[i+3]-mean)*invStd*w[i+3] + b[i+3]
				drow[i+4] = (srow[i+4]-mean)*invStd*w[i+4] + b[i+4]
				drow[i+5] = (srow[i+5]-mean)*invStd*w[i+5] + b[i+5]
				drow[i+6] = (srow[i+6]-mean)*invStd*w[i+6] + b[i+6]
				drow[i+7] = (srow[i+7]-mean)*invStd*w[i+7] + b[i+7]
			}
			for ; i < dim; i++ {
				drow[i] = (srow[i]-mean)*invStd*w[i] + b[i]
			}
		} else {
			i := 0
			for ; i+7 < dim; i += 8 {
				drow[i] = (srow[i] - mean) * invStd * w[i]
				drow[i+1] = (srow[i+1] - mean) * invStd * w[i+1]
				drow[i+2] = (srow[i+2] - mean) * invStd * w[i+2]
				drow[i+3] = (srow[i+3] - mean) * invStd * w[i+3]
				drow[i+4] = (srow[i+4] - mean) * invStd * w[i+4]
				drow[i+5] = (srow[i+5] - mean) * invStd * w[i+5]
				drow[i+6] = (srow[i+6] - mean) * invStd * w[i+6]
				drow[i+7] = (srow[i+7] - mean) * invStd * w[i+7]
			}
			for ; i < dim; i++ {
				drow[i] = (srow[i] - mean) * invStd * w[i]
			}
		}
	}
}

// svAddBias adds bias to each frame (kept for call sites / tests).
func svAddBias(x []float32, nFrames, dim int, bias []float32) {
	if bias == nil {
		return
	}
	tensor.AddBias(x, nFrames, dim, bias)
}

// svReLUInplace applies ReLU. Uses vek32.MaxNumber when available for SIMD max(0,x).
func svReLUInplace(x []float32) {
	if len(x) == 0 {
		return
	}
	// Branchless SIMD: max(x, 0)
	vek32.MaximumNumber_Inplace(x, 0)
}

// sanmBlock is a convenience wrapper that allocates the output (used by diagnostics).
func (m *SenseVoiceModel) sanmBlock(x []float32, nFrames, inDim int, l *svLayerWeights) []float32 {
	hidden := m.hp.HiddenSize
	out := make([]float32, nFrames*hidden)
	bufs := m.ensureEncBufs(nFrames)
	// Work on a copy of x so in-place LayerNorm on entry path doesn't mutate caller's buffer unexpectedly
	xin := make([]float32, nFrames*inDim)
	copy(xin, x[:nFrames*inDim])
	m.sanmBlockInto(xin, nFrames, inDim, l, out, bufs)
	return out
}

// Legacy wrappers used by diagnostic tests.
func svMultiHeadAttention(q, k, v []float32, nFrames, nHeads, headDim int) []float32 {
	hidden := nHeads * headDim
	out := make([]float32, nFrames*hidden)
	// Minimal local bufs for scores
	scores := make([]float32, nFrames)
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	for h := 0; h < nHeads; h++ {
		svAttnHead(out, q, k, v, scores, nFrames, hidden, h, headDim, scale)
	}
	return out
}

func svFSMN(v []float32, nFrames, hidden int, kernel []float32, kernelSize int) []float32 {
	out := make([]float32, nFrames*hidden)
	tmp := make([]float32, hidden)
	bufs := &svEncoderBufs{fsmnTmp: tmp}
	svFSMNInto(out, v, kernel, nFrames, hidden, kernelSize, bufs)
	return out
}

func svAddPosEncoding(x []float32, nFrames, dim int) {
	bufs := &svEncoderBufs{}
	svAddPosEncodingCached(x, nFrames, dim, bufs)
}
