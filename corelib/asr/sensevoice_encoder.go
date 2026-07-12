// corelib/asr/sensevoice_encoder.go — SAN-M encoder forward pass with FSMN.
//
// SANM (Self-Attention Network with Memory) block:
//
//	LayerNorm1 → QKV linear → Multi-Head Attention → FSMN(V) → sum + residual
//	→ LayerNorm2 → FFN(ReLU) → residual
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
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// encodeHidden runs SAN-M encoder; returns [totalFrames, hidden] features (bufs-owned).
func (m *SenseVoiceModel) encodeHidden(lfrFeats []float32, lfrFrames int) (cur []float32, totalFrames int) {
	hp := m.hp
	hidden := hp.HiddenSize
	featsDim := hp.FeatsDim
	totalFrames = 4 + lfrFrames

	bufs := m.ensureEncBufs(totalFrames)

	// Build input sequence into featsBuf: [4 prompt embeddings ; LFR features].
	// Avoid full-buffer clear — only zero missing prompt rows; LFR is fully overwritten.
	featsN := totalFrames * featsDim
	xFeats := bufs.featsBuf[:featsN]
	promptIDs := [4]int{0, 1, 2, 14}
	if m.w.embedding != nil {
		embRows := len(m.w.embedding) / featsDim
		for i, pid := range promptIDs {
			dst := xFeats[i*featsDim : (i+1)*featsDim]
			if pid < embRows {
				copy(dst, m.w.embedding[pid*featsDim:(pid+1)*featsDim])
			} else {
				clear(dst)
			}
		}
	} else {
		clear(xFeats[:4*featsDim])
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
	return cur, totalFrames
}

// encode runs the full SenseVoice encoder.
// Input: lfrFeats [lfrFrames, svFeatsDim], Output: [totalFrames, vocabSize] logits.
func (m *SenseVoiceModel) encode(lfrFeats []float32, lfrFrames int) []float32 {
	cur, totalFrames := m.encodeHidden(lfrFeats, lfrFrames)
	bufs := m.encBufs
	vocab := m.hp.VocabSize
	hidden := m.hp.HiddenSize
	// CTC head — only the diagnostic/full-logit path needs this large buffer.
	// Transcribe uses encodeArgmax, so allocating it eagerly would retain
	// maxFrames*vocab float32s for every loaded model without ever reading them.
	logitN := totalFrames * vocab
	if cap(bufs.logits) < logitN {
		bufs.logits = make([]float32, logitN)
	}
	logits := bufs.logits[:logitN]
	matMulLinearBias(logits, cur, m.w.ctcW, m.w.ctcB, totalFrames, vocab, hidden)
	return logits
}

// encodeArgmax runs encoder + fused CTC argmax (no full logits buffer).
// Returns per-frame token IDs into a reusable scratch slice owned by bufs.
func (m *SenseVoiceModel) encodeArgmax(lfrFeats []float32, lfrFrames int) []int {
	cur, totalFrames := m.encodeHidden(lfrFeats, lfrFrames)
	bufs := m.encBufs
	vocab := m.hp.VocabSize
	hidden := m.hp.HiddenSize
	if cap(bufs.ctcIDs) < totalFrames {
		bufs.ctcIDs = make([]int, totalFrames)
	}
	ids := bufs.ctcIDs[:totalFrames]
	matMulLinearArgmax(ids, cur, m.w.ctcW, m.w.ctcB, totalFrames, vocab, hidden)
	return ids
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

	// Attention+proj and FSMN(V) are independent after QKV.
	// Target: out = attn_proj + FSMN(V) [+ x], without a separate projOut buffer:
	//   sameInOut: seed out=x, BiasAdd proj, then += fsmn
	//   entry:     Bias proj into out, then += fsmn
	// FSMN still overlaps attention (+ proj for sameInOut path).
	attnOut := bufs.attnOut[:n]
	fsmnOut := bufs.fsmnOut[:n]
	kFSMN := m.hp.FSMNKernel

	// Overlap FSMN with attention (+ proj) via matmul worker pool (no bare go).
	overlapFSMN := nFrames >= 12
	var fsmnWG *sync.WaitGroup
	var fsmnTask *svFSMNTask
	if overlapFSMN {
		fsmnWG = svWaitGroupPool.Get().(*sync.WaitGroup)
		fsmnTask = svFSMNTaskPool.Get().(*svFSMNTask)
		fsmnTask.model, fsmnTask.out, fsmnTask.qkv, fsmnTask.v = m, fsmnOut, qkv, v
		fsmnTask.frames, fsmnTask.hidden, fsmnTask.kernel = nFrames, hidden, kFSMN
		fsmnTask.layer, fsmnTask.bufs, fsmnTask.fused = l, bufs, useFused
		tensor.RunAsyncTask(fsmnTask, fsmnWG)
	}

	if useFused {
		svMultiHeadAttentionFused(attnOut, qkv, nFrames, nHeads, headDim, hidden, bufs)
	} else {
		svMultiHeadAttentionInto(attnOut, q, k, v, nFrames, nHeads, headDim, bufs)
	}

	// out = attn_proj (pure store — still overlaps remaining FSMN).
	matMulLinearBias(out[:n], attnOut, l.outW, l.outB.f32, nFrames, hidden, hidden)
	if overlapFSMN {
		fsmnWG.Wait()
		svWaitGroupPool.Put(fsmnWG)
		fsmnTask.model, fsmnTask.out, fsmnTask.qkv, fsmnTask.v = nil, nil, nil, nil
		fsmnTask.layer, fsmnTask.bufs = nil, nil
		svFSMNTaskPool.Put(fsmnTask)
	} else {
		// Short utterances do not overlap work, so avoid creating a closure per
		// SANM layer just to immediately invoke it.
		m.svRunFSMN(fsmnOut, qkv, v, nFrames, hidden, kFSMN, l, bufs, useFused)
	}

	// Fuse residual (out += fsmn [+ x]) with LN2 into residual2:
	//   was 3 memory sweeps (add + LN stats + LN affine) → 2 (add+stats, affine).
	if hidden == 512 && l.norm2B != nil {
		if sameInOut {
			svFuseAdd2AndLN512(out[:n], fsmnOut, x[:n], bufs.residual2[:n], l.norm2W, l.norm2B, nFrames)
		} else {
			svFuseAdd1AndLN512(out[:n], fsmnOut, bufs.residual2[:n], l.norm2W, l.norm2B, nFrames)
		}
	} else {
		if sameInOut {
			svAdd2Inplace(out[:n], fsmnOut, x[:n])
		} else {
			vek32.Add_Inplace(out[:n], fsmnOut)
		}
		svLayerNormBiasInto(bufs.residual2[:n], out[:n], nFrames, hidden, l.norm2W, l.norm2B)
	}

	// FFN: residual stays in out; LN result in residual2; out += ff2.
	ffDim := l.ff1W.Rows()
	ffOut := bufs.ffOut[:nFrames*ffDim]
	matMulLinearBiasReLU(ffOut, bufs.residual2[:n], l.ff1W, l.ff1B.f32, nFrames, ffDim, hidden)
	matMulLinearBiasAdd(out[:n], ffOut, l.ff2W, l.ff2B.f32, nFrames, hidden, ffDim)
}

type svFSMNTask struct {
	model                  *SenseVoiceModel
	out, qkv, v            []float32
	frames, hidden, kernel int
	layer                  *svLayerWeights
	bufs                   *svEncoderBufs
	fused                  bool
}

var svFSMNTaskPool = sync.Pool{New: func() any { return new(svFSMNTask) }}

var svWaitGroupPool = sync.Pool{New: func() any { return new(sync.WaitGroup) }}

func (t *svFSMNTask) RunAsyncTask() {
	t.model.svRunFSMN(t.out, t.qkv, t.v, t.frames, t.hidden, t.kernel, t.layer, t.bufs, t.fused)
}

// svRunFSMN produces the FSMN contribution for one SANM layer.
func (m *SenseVoiceModel) svRunFSMN(fsmnOut, qkv, v []float32, nFrames, hidden, kernelSize int, l *svLayerWeights, bufs *svEncoderBufs, fused bool) {
	if fused {
		// V is the third plane of interleaved QKV [Q|K|V].
		svFSMNStridedAddV(fsmnOut, qkv, 2*hidden, 3*hidden, nFrames, hidden, l.fsmnW, kernelSize)
		return
	}
	svFSMNInto(fsmnOut, v, l.fsmnW, nFrames, hidden, kernelSize, bufs)
	vek32.Add_Inplace(fsmnOut, v)
}

// svAdd2Inplace: out[i] += a[i] + b[i] (one RMW pass; attention residual).
func svAdd2Inplace(out, a, b []float32) {
	tensor.Add2Into(out, a, b)
}

// svAdd3: out[i] = a[i] + b[i] + c[i] (one write pass).
func svAdd3(out, a, b, c []float32) {
	n := len(out)
	i := 0
	for ; i+15 < n; i += 16 {
		out[i] = a[i] + b[i] + c[i]
		out[i+1] = a[i+1] + b[i+1] + c[i+1]
		out[i+2] = a[i+2] + b[i+2] + c[i+2]
		out[i+3] = a[i+3] + b[i+3] + c[i+3]
		out[i+4] = a[i+4] + b[i+4] + c[i+4]
		out[i+5] = a[i+5] + b[i+5] + c[i+5]
		out[i+6] = a[i+6] + b[i+6] + c[i+6]
		out[i+7] = a[i+7] + b[i+7] + c[i+7]
		out[i+8] = a[i+8] + b[i+8] + c[i+8]
		out[i+9] = a[i+9] + b[i+9] + c[i+9]
		out[i+10] = a[i+10] + b[i+10] + c[i+10]
		out[i+11] = a[i+11] + b[i+11] + c[i+11]
		out[i+12] = a[i+12] + b[i+12] + c[i+12]
		out[i+13] = a[i+13] + b[i+13] + c[i+13]
		out[i+14] = a[i+14] + b[i+14] + c[i+14]
		out[i+15] = a[i+15] + b[i+15] + c[i+15]
	}
	for ; i+7 < n; i += 8 {
		out[i] = a[i] + b[i] + c[i]
		out[i+1] = a[i+1] + b[i+1] + c[i+1]
		out[i+2] = a[i+2] + b[i+2] + c[i+2]
		out[i+3] = a[i+3] + b[i+3] + c[i+3]
		out[i+4] = a[i+4] + b[i+4] + c[i+4]
		out[i+5] = a[i+5] + b[i+5] + c[i+5]
		out[i+6] = a[i+6] + b[i+6] + c[i+6]
		out[i+7] = a[i+7] + b[i+7] + c[i+7]
	}
	for ; i < n; i++ {
		out[i] = a[i] + b[i] + c[i]
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
// Packs Q/K/V for all heads once (contiguous [nHeads][nFrames][headDim]).
// Scores use packed Q (no per-tile Q copy); softmax@V uses packed V (stride=headDim).
// Parallelizes across heads via the tensor worker pool when T is large.
func svMultiHeadAttentionFused(out, qkv []float32, nFrames, nHeads, headDim, hidden int, bufs *svEncoderBufs) {
	// headDim=128 → 1/sqrt(128) ≈ 0.08838834764831843 (avoid Sqrt every layer)
	scale := float32(0.08838834764831843)
	if headDim != 128 {
		scale = 1.0 / float32(math.Sqrt(float64(headDim)))
	}

	// Pack Q/K/V; fuse attention scale into Q while packing (dense mul on the
	// pack stream). Score writes then skip per-score *scale (was T² muls).
	packQKVHeads(bufs.q, bufs.k, bufs.v, qkv, nFrames, nHeads, headDim, hidden, scale)

	if nFrames >= 16 && nHeads > 1 && nHeads <= 12 {
		// Reuse structured pool tasks — avoids a captured range closure per layer.
		wg := svWaitGroupPool.Get().(*sync.WaitGroup)
		var tasks [12]*svAttnTask
		for h := 0; h < nHeads; h++ {
			t := svAttnTaskPool.Get().(*svAttnTask)
			t.out, t.bufs = out, bufs
			t.frames, t.heads, t.headDim, t.hidden, t.head = nFrames, nHeads, headDim, hidden, h
			tasks[h] = t
			tensor.RunAsyncTask(t, wg)
		}
		wg.Wait()
		svWaitGroupPool.Put(wg)
		for h := 0; h < nHeads; h++ {
			t := tasks[h]
			t.out, t.bufs = nil, nil
			svAttnTaskPool.Put(t)
		}
		return
	}
	if nFrames >= 16 && nHeads > 1 {
		// General fallback for unusual models with more heads than the task stack.
		tensor.ParallelRanges(nHeads, func(s, e int) {
			for h := s; h < e; h++ {
				svFusedAttnHead(out, bufs, nFrames, nHeads, headDim, hidden, h)
			}
		})
		return
	}
	for h := 0; h < nHeads; h++ {
		svFusedAttnHead(out, bufs, nFrames, nHeads, headDim, hidden, h)
	}
}

type svAttnTask struct {
	out                            []float32
	bufs                           *svEncoderBufs
	frames, heads, headDim, hidden int
	head                           int
}

var svAttnTaskPool = sync.Pool{New: func() any { return new(svAttnTask) }}

func (t *svAttnTask) RunAsyncTask() {
	svFusedAttnHead(t.out, t.bufs, t.frames, t.heads, t.headDim, t.hidden, t.head)
}

// svFusedAttnHead evaluates one packed attention head without a captured closure.
func svFusedAttnHead(out []float32, bufs *svEncoderBufs, nFrames, nHeads, headDim, hidden, h int) {
	hOff := h * headDim
	base := h * nFrames * headDim
	qPack := bufs.q[base : base+nFrames*headDim]
	kPack := bufs.k[base : base+nFrames*headDim]
	vPack := bufs.v[base : base+nFrames*headDim]
	scores, _ := ensureAttnScratchHead(bufs, nFrames, headDim, h, nHeads)
	// Scale is already in Q; packed V stride=headDim.
	svAttnScoresPackedQ(out, qPack, kPack, vPack, scores, nFrames, hidden, headDim, hOff, 0, headDim, 1)
}

// packQKVHeads packs fused QKV into contiguous [nHeads][nFrames][headDim] for Q, K, V.
// qScale is fused into Q (attention 1/sqrt(headDim)); K/V are plain copies.
// Frame-major over heads keeps each qkv row hot in L1.
// Kept serial: FSMN often runs concurrently and competing BW thrashing hurts more than pack parallel helps.
func packQKVHeads(qDst, kDst, vDst, qkv []float32, nFrames, nHeads, headDim, hidden int, qScale float32) {
	qkvStride := 3 * hidden
	if headDim == 128 {
		// nHeads=4 (SenseVoice): PackQKV4Heads128 packs all heads per frame
		// (one call keeps the [Q|K|V] row hot vs 4×PackQKV128).
		f := 0
		if nHeads == 4 {
			for ; f < nFrames; f++ {
				tensor.PackQKV4Heads128(qDst, kDst, vDst, qkv[f*qkvStride:], nFrames, f, qScale)
			}
			return
		}
		for ; f+1 < nFrames; f += 2 {
			row0 := qkv[f*qkvStride:]
			row1 := qkv[(f+1)*qkvStride:]
			for h := 0; h < nHeads; h++ {
				hOff := h * 128
				d0 := (h*nFrames + f) * 128
				d1 := d0 + 128
				tensor.PackQKV128(
					qDst[d0:d0+128], kDst[d0:d0+128], vDst[d0:d0+128],
					row0[hOff:hOff+128], row0[hidden+hOff:hidden+hOff+128], row0[2*hidden+hOff:2*hidden+hOff+128],
					qScale,
				)
				tensor.PackQKV128(
					qDst[d1:d1+128], kDst[d1:d1+128], vDst[d1:d1+128],
					row1[hOff:hOff+128], row1[hidden+hOff:hidden+hOff+128], row1[2*hidden+hOff:2*hidden+hOff+128],
					qScale,
				)
			}
		}
		for ; f < nFrames; f++ {
			row := qkv[f*qkvStride:]
			for h := 0; h < nHeads; h++ {
				hOff := h * 128
				dstOff := (h*nFrames + f) * 128
				tensor.PackQKV128(
					qDst[dstOff:dstOff+128], kDst[dstOff:dstOff+128], vDst[dstOff:dstOff+128],
					row[hOff:hOff+128], row[hidden+hOff:hidden+hOff+128], row[2*hidden+hOff:2*hidden+hOff+128],
					qScale,
				)
			}
		}
		return
	}
	for f := 0; f < nFrames; f++ {
		row := qkv[f*qkvStride:]
		for h := 0; h < nHeads; h++ {
			hOff := h * headDim
			dstOff := (h*nFrames + f) * headDim
			q := qDst[dstOff : dstOff+headDim]
			copy(q, row[hOff:hOff+headDim])
			if qScale != 1 {
				vek32.MulNumber_Inplace(q, qScale)
			}
			copy(kDst[dstOff:dstOff+headDim], row[hidden+hOff:hidden+hOff+headDim])
			copy(vDst[dstOff:dstOff+headDim], row[2*hidden+hOff:2*hidden+hOff+headDim])
		}
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

// ensureAttnScratch returns score rows [8*nFrames] and Q panel [8*headDim] (head 0).
func ensureAttnScratch(bufs *svEncoderBufs, nFrames, headDim int) (scores, qPanel []float32) {
	return ensureAttnScratchHead(bufs, nFrames, headDim, 0, 1)
}

// ensureAttnScratchHead returns per-head score/Q-panel slices for parallel attention.
func ensureAttnScratchHead(bufs *svEncoderBufs, nFrames, headDim, h, nHeads int) (scores, qPanel []float32) {
	perHead := 8*nFrames + 8*headDim
	need := nHeads * perHead
	if cap(bufs.scoresScratch) < need {
		bufs.scoresScratch = make([]float32, need)
	}
	base := h * perHead
	scores = bufs.scoresScratch[base : base+8*nFrames]
	qPanel = bufs.scoresScratch[base+8*nFrames : base+perHead]
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
	svAttnScoresTiled(out, qkv, kPack, qkv, scores, qPanel, nFrames, hidden, qkvStride, hOff, 2*hidden+hOff, headDim, scale, true)
}

// Score write helpers: dual-4 layout d[0:4]=K0, d[4:8]=K1; triple adds d[8:12]=K2.
func svWriteScores8x3(scores []float32, nFrames, kf int, d0, d1 *[12]float32, scale float32, noScale bool) {
	if noScale {
		scores[0*nFrames+kf], scores[1*nFrames+kf] = d0[0], d0[1]
		scores[2*nFrames+kf], scores[3*nFrames+kf] = d0[2], d0[3]
		scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d0[4], d0[5]
		scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d0[6], d0[7]
		scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d0[8], d0[9]
		scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d0[10], d0[11]
		scores[4*nFrames+kf], scores[5*nFrames+kf] = d1[0], d1[1]
		scores[6*nFrames+kf], scores[7*nFrames+kf] = d1[2], d1[3]
		scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = d1[4], d1[5]
		scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = d1[6], d1[7]
		scores[4*nFrames+kf+2], scores[5*nFrames+kf+2] = d1[8], d1[9]
		scores[6*nFrames+kf+2], scores[7*nFrames+kf+2] = d1[10], d1[11]
		return
	}
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d0[0]*scale, d0[1]*scale
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d0[2]*scale, d0[3]*scale
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d0[4]*scale, d0[5]*scale
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d0[6]*scale, d0[7]*scale
	scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d0[8]*scale, d0[9]*scale
	scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d0[10]*scale, d0[11]*scale
	scores[4*nFrames+kf], scores[5*nFrames+kf] = d1[0]*scale, d1[1]*scale
	scores[6*nFrames+kf], scores[7*nFrames+kf] = d1[2]*scale, d1[3]*scale
	scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = d1[4]*scale, d1[5]*scale
	scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = d1[6]*scale, d1[7]*scale
	scores[4*nFrames+kf+2], scores[5*nFrames+kf+2] = d1[8]*scale, d1[9]*scale
	scores[6*nFrames+kf+2], scores[7*nFrames+kf+2] = d1[10]*scale, d1[11]*scale
}

func svWriteScores8x2(scores []float32, nFrames, kf int, dLo, dHi *[8]float32, scale float32, noScale bool) {
	if noScale {
		scores[0*nFrames+kf], scores[1*nFrames+kf] = dLo[0], dLo[1]
		scores[2*nFrames+kf], scores[3*nFrames+kf] = dLo[2], dLo[3]
		scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = dLo[4], dLo[5]
		scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = dLo[6], dLo[7]
		scores[4*nFrames+kf], scores[5*nFrames+kf] = dHi[0], dHi[1]
		scores[6*nFrames+kf], scores[7*nFrames+kf] = dHi[2], dHi[3]
		scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = dHi[4], dHi[5]
		scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = dHi[6], dHi[7]
		return
	}
	scores[0*nFrames+kf], scores[1*nFrames+kf] = dLo[0]*scale, dLo[1]*scale
	scores[2*nFrames+kf], scores[3*nFrames+kf] = dLo[2]*scale, dLo[3]*scale
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = dLo[4]*scale, dLo[5]*scale
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = dLo[6]*scale, dLo[7]*scale
	scores[4*nFrames+kf], scores[5*nFrames+kf] = dHi[0]*scale, dHi[1]*scale
	scores[6*nFrames+kf], scores[7*nFrames+kf] = dHi[2]*scale, dHi[3]*scale
	scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = dHi[4]*scale, dHi[5]*scale
	scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = dHi[6]*scale, dHi[7]*scale
}

func svWriteScores8x1(scores []float32, nFrames, kf int, d8 *[8]float32, scale float32, noScale bool) {
	if noScale {
		for t := 0; t < 8; t++ {
			scores[t*nFrames+kf] = d8[t]
		}
		return
	}
	for t := 0; t < 8; t++ {
		scores[t*nFrames+kf] = d8[t] * scale
	}
}

func svWriteScores4x3(scores []float32, nFrames, kf int, d *[12]float32, scale float32, noScale bool) {
	if noScale {
		scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0], d[1]
		scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2], d[3]
		scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4], d[5]
		scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6], d[7]
		scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d[8], d[9]
		scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d[10], d[11]
		return
	}
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0]*scale, d[1]*scale
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2]*scale, d[3]*scale
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4]*scale, d[5]*scale
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6]*scale, d[7]*scale
	scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d[8]*scale, d[9]*scale
	scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d[10]*scale, d[11]*scale
}

func svWriteScores4x2(scores []float32, nFrames, kf int, d *[8]float32, scale float32, noScale bool) {
	if noScale {
		scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0], d[1]
		scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2], d[3]
		scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4], d[5]
		scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6], d[7]
		return
	}
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0]*scale, d[1]*scale
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2]*scale, d[3]*scale
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4]*scale, d[5]*scale
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6]*scale, d[7]*scale
}

func svWriteScores4x1(scores []float32, nFrames, kf int, d *[4]float32, scale float32, noScale bool) {
	if noScale {
		for t := 0; t < 4; t++ {
			scores[t*nFrames+kf] = d[t]
		}
		return
	}
	for t := 0; t < 4; t++ {
		scores[t*nFrames+kf] = d[t] * scale
	}
}

// svAttnScoresPackedQ: Q and K are both contiguous [nFrames][headDim].
// Q is expected pre-scaled by 1/sqrt(headDim) at pack time (scale arg kept for
// legacy callers; scale==1 is the fused hot path).
// V is typically packed (vStride=headDim); softmax@V batches 8/4 queries to share V loads.
func svAttnScoresPackedQ(out, qPack, kPack, vSrc, scores []float32, nFrames, hidden, vStride, hOff, vBaseOff, headDim int, scale float32) {
	// SenseVoice fused path: headDim=128, Q pre-scaled, packed V.
	if headDim == 128 && scale == 1 && vStride == 128 {
		svAttnScoresPackedQ128NS(out, qPack, kPack, vSrc[vBaseOff:], scores, nFrames, hidden, hOff)
		return
	}
	vBase := vSrc[vBaseOff:]
	noScale := scale == 1
	qf := 0
	for ; qf+7 < nFrames; qf += 8 {
		aPanel := qPack[qf*headDim : (qf+8)*headDim]
		aLo := aPanel[:4*headDim]
		aHi := aPanel[4*headDim:]
		var dTri0, dTri1 [12]float32
		var dLo, dHi [8]float32
		kf := 0
		if noScale {
			for ; kf+2 < nFrames; kf += 3 {
				k0 := kPack[kf*headDim : (kf+1)*headDim]
				k1 := kPack[(kf+1)*headDim : (kf+2)*headDim]
				k2 := kPack[(kf+2)*headDim : (kf+3)*headDim]
				tensor.MultiDot4TripleB(&dTri0, aLo, k0, k1, k2, headDim)
				tensor.MultiDot4TripleB(&dTri1, aHi, k0, k1, k2, headDim)
				svWriteScores8x3NS(scores, nFrames, kf, &dTri0, &dTri1)
			}
			for ; kf+1 < nFrames; kf += 2 {
				k0 := kPack[kf*headDim : (kf+1)*headDim]
				k1 := kPack[(kf+1)*headDim : (kf+2)*headDim]
				tensor.MultiDot8DualB(&dLo, &dHi, aPanel, k0, k1, headDim)
				svWriteScores8x2NS(scores, nFrames, kf, &dLo, &dHi)
			}
			if kf < nFrames {
				var d8 [8]float32
				tensor.MultiDot8(&d8, aPanel, kPack[kf*headDim:(kf+1)*headDim], headDim)
				svWriteScores8x1NS(scores, nFrames, kf, &d8)
			}
		} else {
			for ; kf+2 < nFrames; kf += 3 {
				k0 := kPack[kf*headDim : (kf+1)*headDim]
				k1 := kPack[(kf+1)*headDim : (kf+2)*headDim]
				k2 := kPack[(kf+2)*headDim : (kf+3)*headDim]
				tensor.MultiDot4TripleB(&dTri0, aLo, k0, k1, k2, headDim)
				tensor.MultiDot4TripleB(&dTri1, aHi, k0, k1, k2, headDim)
				svWriteScores8x3(scores, nFrames, kf, &dTri0, &dTri1, scale, false)
			}
			for ; kf+1 < nFrames; kf += 2 {
				k0 := kPack[kf*headDim : (kf+1)*headDim]
				k1 := kPack[(kf+1)*headDim : (kf+2)*headDim]
				tensor.MultiDot8DualB(&dLo, &dHi, aPanel, k0, k1, headDim)
				svWriteScores8x2(scores, nFrames, kf, &dLo, &dHi, scale, false)
			}
			if kf < nFrames {
				var d8 [8]float32
				tensor.MultiDot8(&d8, aPanel, kPack[kf*headDim:(kf+1)*headDim], headDim)
				svWriteScores8x1(scores, nFrames, kf, &d8, scale, false)
			}
		}
		tensor.SoftmaxWeightedSumBatched(out, scores, vBase, 8, nFrames, vStride, headDim, hidden, hOff, qf)
	}
	for ; qf+3 < nFrames; qf += 4 {
		aPanel := qPack[qf*headDim : (qf+4)*headDim]
		var dTri [12]float32
		var dDual [8]float32
		kf := 0
		if noScale {
			for ; kf+2 < nFrames; kf += 3 {
				tensor.MultiDot4TripleB(&dTri, aPanel,
					kPack[kf*headDim:(kf+1)*headDim],
					kPack[(kf+1)*headDim:(kf+2)*headDim],
					kPack[(kf+2)*headDim:(kf+3)*headDim], headDim)
				svWriteScores4x3NS(scores, nFrames, kf, &dTri)
			}
			for ; kf+1 < nFrames; kf += 2 {
				tensor.MultiDot4DualB(&dDual, aPanel,
					kPack[kf*headDim:(kf+1)*headDim],
					kPack[(kf+1)*headDim:(kf+2)*headDim], headDim)
				svWriteScores4x2NS(scores, nFrames, kf, &dDual)
			}
			if kf < nFrames {
				var d4 [4]float32
				tensor.MultiDot4(&d4, aPanel, kPack[kf*headDim:(kf+1)*headDim], headDim)
				svWriteScores4x1NS(scores, nFrames, kf, &d4)
			}
		} else {
			for ; kf+2 < nFrames; kf += 3 {
				tensor.MultiDot4TripleB(&dTri, aPanel,
					kPack[kf*headDim:(kf+1)*headDim],
					kPack[(kf+1)*headDim:(kf+2)*headDim],
					kPack[(kf+2)*headDim:(kf+3)*headDim], headDim)
				svWriteScores4x3(scores, nFrames, kf, &dTri, scale, false)
			}
			for ; kf+1 < nFrames; kf += 2 {
				tensor.MultiDot4DualB(&dDual, aPanel,
					kPack[kf*headDim:(kf+1)*headDim],
					kPack[(kf+1)*headDim:(kf+2)*headDim], headDim)
				svWriteScores4x2(scores, nFrames, kf, &dDual, scale, false)
			}
			if kf < nFrames {
				var d4 [4]float32
				tensor.MultiDot4(&d4, aPanel, kPack[kf*headDim:(kf+1)*headDim], headDim)
				svWriteScores4x1(scores, nFrames, kf, &d4, scale, false)
			}
		}
		tensor.SoftmaxWeightedSumBatched(out, scores, vBase, 4, nFrames, vStride, headDim, hidden, hOff, qf)
	}
	for ; qf < nFrames; qf++ {
		qVec := qPack[qf*headDim : (qf+1)*headDim]
		sc := scores[:nFrames]
		if noScale {
			for kf := 0; kf < nFrames; kf++ {
				sc[kf] = vek32.Dot(qVec, kPack[kf*headDim:(kf+1)*headDim])
			}
		} else {
			for kf := 0; kf < nFrames; kf++ {
				sc[kf] = vek32.Dot(qVec, kPack[kf*headDim:(kf+1)*headDim]) * scale
			}
		}
		oOff := qf*hidden + hOff
		tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+headDim], sc, vBase, nFrames, vStride, headDim)
	}
}

// svAttnScoresPackedQ128NS: SenseVoice fused attention hot path.
// headDim=128, Q pre-scaled, V contiguous; no per-score scale multiplies.
func svAttnScoresPackedQ128NS(out, qPack, kPack, vBase, scores []float32, nFrames, hidden, hOff int) {
	const headDim = 128
	qf := 0
	for ; qf+7 < nFrames; qf += 8 {
		aPanel := qPack[qf*128 : (qf+8)*128]
		var dTri0, dTri1 [12]float32
		var dLo, dHi [8]float32
		kf := 0
		for ; kf+2 < nFrames; kf += 3 {
			k0 := kPack[kf*128 : (kf+1)*128]
			k1 := kPack[(kf+1)*128 : (kf+2)*128]
			k2 := kPack[(kf+2)*128 : (kf+3)*128]
			// multiDot8Triple: two triple-4 with K=128 asm; B stays hot in L1.
			tensor.MultiDot8TripleB(&dTri0, &dTri1, aPanel, k0, k1, k2, 128)
			svWriteScores8x3NS(scores, nFrames, kf, &dTri0, &dTri1)
		}
		for ; kf+1 < nFrames; kf += 2 {
			k0 := kPack[kf*128 : (kf+1)*128]
			k1 := kPack[(kf+1)*128 : (kf+2)*128]
			tensor.MultiDot8DualB(&dLo, &dHi, aPanel, k0, k1, 128)
			svWriteScores8x2NS(scores, nFrames, kf, &dLo, &dHi)
		}
		if kf < nFrames {
			var d8 [8]float32
			tensor.MultiDot8(&d8, aPanel, kPack[kf*128:(kf+1)*128], 128)
			svWriteScores8x1NS(scores, nFrames, kf, &d8)
		}
		tensor.SoftmaxWeightedSumBatched(out, scores, vBase, 8, nFrames, 128, 128, hidden, hOff, qf)
	}
	for ; qf+3 < nFrames; qf += 4 {
		aPanel := qPack[qf*128 : (qf+4)*128]
		var dTri [12]float32
		var dDual [8]float32
		kf := 0
		for ; kf+2 < nFrames; kf += 3 {
			tensor.MultiDot4TripleB(&dTri, aPanel,
				kPack[kf*128:(kf+1)*128],
				kPack[(kf+1)*128:(kf+2)*128],
				kPack[(kf+2)*128:(kf+3)*128], 128)
			svWriteScores4x3NS(scores, nFrames, kf, &dTri)
		}
		for ; kf+1 < nFrames; kf += 2 {
			tensor.MultiDot4DualB(&dDual, aPanel,
				kPack[kf*128:(kf+1)*128],
				kPack[(kf+1)*128:(kf+2)*128], 128)
			svWriteScores4x2NS(scores, nFrames, kf, &dDual)
		}
		if kf < nFrames {
			var d4 [4]float32
			tensor.MultiDot4(&d4, aPanel, kPack[kf*128:(kf+1)*128], 128)
			svWriteScores4x1NS(scores, nFrames, kf, &d4)
		}
		tensor.SoftmaxWeightedSumBatched(out, scores, vBase, 4, nFrames, 128, 128, hidden, hOff, qf)
	}
	for ; qf < nFrames; qf++ {
		qVec := qPack[qf*128 : (qf+1)*128]
		sc := scores[:nFrames]
		kf := 0
		for ; kf+1 < nFrames; kf += 2 {
			sc[kf] = vek32.Dot(qVec, kPack[kf*128:(kf+1)*128])
			sc[kf+1] = vek32.Dot(qVec, kPack[(kf+1)*128:(kf+2)*128])
		}
		for ; kf < nFrames; kf++ {
			sc[kf] = vek32.Dot(qVec, kPack[kf*128:(kf+1)*128])
		}
		oOff := qf*hidden + hOff
		tensor.SoftmaxWeightedSumStrided(out[oOff:oOff+128], sc, vBase, nFrames, 128, 128)
	}
}

// noScale score writers (Q pre-scaled) — no branch, no multiply.
func svWriteScores8x3NS(scores []float32, nFrames, kf int, d0, d1 *[12]float32) {
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d0[0], d0[1]
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d0[2], d0[3]
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d0[4], d0[5]
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d0[6], d0[7]
	scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d0[8], d0[9]
	scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d0[10], d0[11]
	scores[4*nFrames+kf], scores[5*nFrames+kf] = d1[0], d1[1]
	scores[6*nFrames+kf], scores[7*nFrames+kf] = d1[2], d1[3]
	scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = d1[4], d1[5]
	scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = d1[6], d1[7]
	scores[4*nFrames+kf+2], scores[5*nFrames+kf+2] = d1[8], d1[9]
	scores[6*nFrames+kf+2], scores[7*nFrames+kf+2] = d1[10], d1[11]
}

func svWriteScores8x2NS(scores []float32, nFrames, kf int, dLo, dHi *[8]float32) {
	scores[0*nFrames+kf], scores[1*nFrames+kf] = dLo[0], dLo[1]
	scores[2*nFrames+kf], scores[3*nFrames+kf] = dLo[2], dLo[3]
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = dLo[4], dLo[5]
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = dLo[6], dLo[7]
	scores[4*nFrames+kf], scores[5*nFrames+kf] = dHi[0], dHi[1]
	scores[6*nFrames+kf], scores[7*nFrames+kf] = dHi[2], dHi[3]
	scores[4*nFrames+kf+1], scores[5*nFrames+kf+1] = dHi[4], dHi[5]
	scores[6*nFrames+kf+1], scores[7*nFrames+kf+1] = dHi[6], dHi[7]
}

func svWriteScores8x1NS(scores []float32, nFrames, kf int, d8 *[8]float32) {
	for t := 0; t < 8; t++ {
		scores[t*nFrames+kf] = d8[t]
	}
}

func svWriteScores4x3NS(scores []float32, nFrames, kf int, d *[12]float32) {
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0], d[1]
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2], d[3]
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4], d[5]
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6], d[7]
	scores[0*nFrames+kf+2], scores[1*nFrames+kf+2] = d[8], d[9]
	scores[2*nFrames+kf+2], scores[3*nFrames+kf+2] = d[10], d[11]
}

func svWriteScores4x2NS(scores []float32, nFrames, kf int, d *[8]float32) {
	scores[0*nFrames+kf], scores[1*nFrames+kf] = d[0], d[1]
	scores[2*nFrames+kf], scores[3*nFrames+kf] = d[2], d[3]
	scores[0*nFrames+kf+1], scores[1*nFrames+kf+1] = d[4], d[5]
	scores[2*nFrames+kf+1], scores[3*nFrames+kf+1] = d[6], d[7]
}

func svWriteScores4x1NS(scores []float32, nFrames, kf int, d *[4]float32) {
	for t := 0; t < 4; t++ {
		scores[t*nFrames+kf] = d[t]
	}
}

// svAttnScoresTiled: legacy path for non-packed Q (non-fused attention).
func svAttnScoresTiled(out, qSrc, kPack, vSrc, scores, qPanel []float32, nFrames, hidden, qStride, hOff, vBaseOff, headDim int, scale float32, fused bool) {
	vStride := hidden
	if fused {
		vStride = qStride
	}
	// Fallback: pack-on-the-fly still works; prefer PackedQ when possible.
	_ = fused
	qf := 0
	for ; qf+7 < nFrames; qf += 8 {
		for t := 0; t < 8; t++ {
			src := qSrc[(qf+t)*qStride+hOff : (qf+t)*qStride+hOff+headDim]
			copy(qPanel[t*headDim:(t+1)*headDim], src)
		}
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
			for t := 0; t < 8; t++ {
				scores[t*nFrames+kf] = d8[t] * scale
			}
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
			for t := 0; t < 4; t++ {
				scores[t*nFrames+kf] = d4[t] * scale
			}
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
func svFSMNStrided(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, kernelSize int, bufs *svEncoderBufs) {
	svFSMNStridedOpt(out, v, baseOff, stride, nFrames, hidden, kernel, kernelSize, false)
}

// svFSMNStridedAddV = FSMN(V) + V in one pass (saves a full memory sweep).
func svFSMNStridedAddV(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, kernelSize int) {
	svFSMNStridedOpt(out, v, baseOff, stride, nFrames, hidden, kernel, kernelSize, true)
}

func svFSMNStridedOpt(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, kernelSize int, addV bool) {
	if kernel == nil {
		if addV {
			// out = V
			for f := 0; f < nFrames; f++ {
				copy(out[f*hidden:(f+1)*hidden], v[f*stride+baseOff:f*stride+baseOff+hidden])
			}
		} else {
			clear(out[:nFrames*hidden])
		}
		return
	}
	if kernelSize == 11 && hidden >= 8 {
		svFSMNKernel11(out, v, baseOff, stride, nFrames, hidden, kernel, addV)
		return
	}
	pad := (kernelSize - 1) / 2
	for f := 0; f < nFrames; f++ {
		svFSMNOneFrame(out[f*hidden:(f+1)*hidden], v, baseOff, stride, nFrames, hidden, kernel, kernelSize, pad, f)
		if addV {
			s := v[f*stride+baseOff : f*stride+baseOff+hidden]
			d := out[f*hidden : (f+1)*hidden]
			i := 0
			for ; i+7 < hidden; i += 8 {
				d[i] += s[i]
				d[i+1] += s[i+1]
				d[i+2] += s[i+2]
				d[i+3] += s[i+3]
				d[i+4] += s[i+4]
				d[i+5] += s[i+5]
				d[i+6] += s[i+6]
				d[i+7] += s[i+7]
			}
			for ; i < hidden; i++ {
				d[i] += s[i]
			}
		}
	}
}

// svFSMNKernel11 is the hot FSMN path (kernel_size=11, pad=5).
// When addV, residual V is folded into the center kernel term (ki==pad):
//
//	out += V*(k_center+1)  instead of  out += V*k_center; out += V
//
// addV is specialized (SenseVoice fused path always folds V) — no per-ki branch.
func svFSMNKernel11(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, addV bool) {
	if addV {
		svFSMNKernel11AddV(out, v, baseOff, stride, nFrames, hidden, kernel)
		return
	}
	svFSMNKernel11Plain(out, v, baseOff, stride, nFrames, hidden, kernel)
}

func svFSMNKernel11AddV(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32) {
	const pad, ksz = 5, 11
	for f := 0; f < pad && f < nFrames; f++ {
		row := out[f*hidden : (f+1)*hidden]
		svFSMNOneFrame(row, v, baseOff, stride, nFrames, hidden, kernel, ksz, pad, f)
		svAddRow(row, v[f*stride+baseOff:f*stride+baseOff+hidden])
	}
	end := nFrames - pad
	if end < pad {
		end = pad
	}
	f := pad
	// Quad-frame: ki loop split around pad — center uses PlusOne without branch.
	for ; f+3 < end; f += 4 {
		out0 := out[f*hidden : (f+1)*hidden]
		out1 := out[(f+1)*hidden : (f+2)*hidden]
		out2 := out[(f+2)*hidden : (f+3)*hidden]
		out3 := out[(f+3)*hidden : (f+4)*hidden]
		src0 := (f-pad)*stride + baseOff
		src1 := src0 + stride
		src2 := src1 + stride
		src3 := src2 + stride
		tensor.Mul4Into(out0, out1, out2, out3,
			v[src0:src0+hidden], v[src1:src1+hidden], v[src2:src2+hidden], v[src3:src3+hidden], kernel[:hidden])
		for ki := 1; ki < pad; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd4Into(out0, out1, out2, out3,
				v[s0:s0+hidden], v[s0+stride:s0+stride+hidden],
				v[s0+2*stride:s0+2*stride+hidden], v[s0+3*stride:s0+3*stride+hidden], kRow)
		}
		{
			kRow := kernel[pad*hidden : (pad+1)*hidden]
			s0 := (f-pad+pad)*stride + baseOff // = f*stride+baseOff
			tensor.FmaddPlusOne4Into(out0, out1, out2, out3,
				v[s0:s0+hidden], v[s0+stride:s0+stride+hidden],
				v[s0+2*stride:s0+2*stride+hidden], v[s0+3*stride:s0+3*stride+hidden], kRow)
		}
		for ki := pad + 1; ki < ksz; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd4Into(out0, out1, out2, out3,
				v[s0:s0+hidden], v[s0+stride:s0+stride+hidden],
				v[s0+2*stride:s0+2*stride+hidden], v[s0+3*stride:s0+3*stride+hidden], kRow)
		}
	}
	// Dual-frame remainder: Mul2/Fmadd2 share kernel loads.
	for ; f+1 < end; f += 2 {
		out0 := out[f*hidden : (f+1)*hidden]
		out1 := out[(f+1)*hidden : (f+2)*hidden]
		src0 := (f-pad)*stride + baseOff
		src1 := src0 + stride
		tensor.Mul2Into(out0, out1, v[src0:src0+hidden], v[src1:src1+hidden], kernel[:hidden])
		for ki := 1; ki < pad; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd2Into(out0, out1, v[s0:s0+hidden], v[s0+stride:s0+stride+hidden], kRow)
		}
		{
			kRow := kernel[pad*hidden : (pad+1)*hidden]
			s0 := f*stride + baseOff
			tensor.FmaddPlusOne2Into(out0, out1, v[s0:s0+hidden], v[s0+stride:s0+stride+hidden], kRow)
		}
		for ki := pad + 1; ki < ksz; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd2Into(out0, out1, v[s0:s0+hidden], v[s0+stride:s0+stride+hidden], kRow)
		}
	}
	for ; f < end; f++ {
		outRow := out[f*hidden : (f+1)*hidden]
		src0 := (f-pad)*stride + baseOff
		vek32.Mul_Into(outRow, v[src0:src0+hidden], kernel[:hidden])
		for ki := 1; ki < pad; ki++ {
			src := (f-pad+ki)*stride + baseOff
			tensor.FmaddInto(outRow, v[src:src+hidden], kernel[ki*hidden:(ki+1)*hidden])
		}
		srcC := f*stride + baseOff
		tensor.FmaddPlusOneInto(outRow, v[srcC:srcC+hidden], kernel[pad*hidden:(pad+1)*hidden])
		for ki := pad + 1; ki < ksz; ki++ {
			src := (f-pad+ki)*stride + baseOff
			tensor.FmaddInto(outRow, v[src:src+hidden], kernel[ki*hidden:(ki+1)*hidden])
		}
	}
	for f := end; f < nFrames; f++ {
		row := out[f*hidden : (f+1)*hidden]
		svFSMNOneFrame(row, v, baseOff, stride, nFrames, hidden, kernel, ksz, pad, f)
		svAddRow(row, v[f*stride+baseOff:f*stride+baseOff+hidden])
	}
}

func svFSMNKernel11Plain(out, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32) {
	const pad, ksz = 5, 11
	for f := 0; f < pad && f < nFrames; f++ {
		svFSMNOneFrame(out[f*hidden:(f+1)*hidden], v, baseOff, stride, nFrames, hidden, kernel, ksz, pad, f)
	}
	end := nFrames - pad
	if end < pad {
		end = pad
	}
	f := pad
	for ; f+3 < end; f += 4 {
		out0 := out[f*hidden : (f+1)*hidden]
		out1 := out[(f+1)*hidden : (f+2)*hidden]
		out2 := out[(f+2)*hidden : (f+3)*hidden]
		out3 := out[(f+3)*hidden : (f+4)*hidden]
		src0 := (f-pad)*stride + baseOff
		tensor.Mul4Into(out0, out1, out2, out3,
			v[src0:src0+hidden], v[src0+stride:src0+stride+hidden],
			v[src0+2*stride:src0+2*stride+hidden], v[src0+3*stride:src0+3*stride+hidden], kernel[:hidden])
		for ki := 1; ki < ksz; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd4Into(out0, out1, out2, out3,
				v[s0:s0+hidden], v[s0+stride:s0+stride+hidden],
				v[s0+2*stride:s0+2*stride+hidden], v[s0+3*stride:s0+3*stride+hidden], kRow)
		}
	}
	for ; f+1 < end; f += 2 {
		out0 := out[f*hidden : (f+1)*hidden]
		out1 := out[(f+1)*hidden : (f+2)*hidden]
		src0 := (f-pad)*stride + baseOff
		tensor.Mul2Into(out0, out1, v[src0:src0+hidden], v[src0+stride:src0+stride+hidden], kernel[:hidden])
		for ki := 1; ki < ksz; ki++ {
			kRow := kernel[ki*hidden : (ki+1)*hidden]
			s0 := (f-pad+ki)*stride + baseOff
			tensor.Fmadd2Into(out0, out1, v[s0:s0+hidden], v[s0+stride:s0+stride+hidden], kRow)
		}
	}
	for ; f < end; f++ {
		outRow := out[f*hidden : (f+1)*hidden]
		src0 := (f-pad)*stride + baseOff
		vek32.Mul_Into(outRow, v[src0:src0+hidden], kernel[:hidden])
		for ki := 1; ki < ksz; ki++ {
			src := (f-pad+ki)*stride + baseOff
			tensor.FmaddInto(outRow, v[src:src+hidden], kernel[ki*hidden:(ki+1)*hidden])
		}
	}
	for f := end; f < nFrames; f++ {
		svFSMNOneFrame(out[f*hidden:(f+1)*hidden], v, baseOff, stride, nFrames, hidden, kernel, ksz, pad, f)
	}
}

func svAddRow(d, s []float32) {
	tensor.AddInto(d, s)
}

func svFSMNOneFrame(outRow, v []float32, baseOff, stride, nFrames, hidden int, kernel []float32, kernelSize, pad, f int) {
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
	// AVX2: x = x*scale + pe (entry featsDim=560 × T).
	tensor.ScaleAdd(x[:need], x[:need], pe, scale)
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
// Parallelizes across frames when T is large (~70 layers × 2 LNs).
func svLayerNormBiasInto(dst, src []float32, nFrames, dim int, w, b []float32) {
	if w == nil || dim == 0 {
		return
	}
	// Hot path: SenseVoice hidden=512 with bias (almost all LNs).
	if dim == 512 && b != nil {
		svLayerNormBiasInto512(dst, src, nFrames, w, b)
		return
	}
	// Entry LN dim=560 (and other multiples of 16) via AVX2 tensor path.
	if b != nil && dim&15 == 0 {
		for f := 0; f < nFrames; f++ {
			off := f * dim
			tensor.LayerNormBias(dst[off:off+dim], src[off:off+dim], w, b, dim)
		}
		return
	}
	const eps = 1e-5
	invDim := 1.0 / float32(dim)
	run := func(fs, fe int) {
		for f := fs; f < fe; f++ {
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
				for ; i+15 < dim; i += 16 {
					drow[i] = (srow[i]-mean)*invStd*w[i] + b[i]
					drow[i+1] = (srow[i+1]-mean)*invStd*w[i+1] + b[i+1]
					drow[i+2] = (srow[i+2]-mean)*invStd*w[i+2] + b[i+2]
					drow[i+3] = (srow[i+3]-mean)*invStd*w[i+3] + b[i+3]
					drow[i+4] = (srow[i+4]-mean)*invStd*w[i+4] + b[i+4]
					drow[i+5] = (srow[i+5]-mean)*invStd*w[i+5] + b[i+5]
					drow[i+6] = (srow[i+6]-mean)*invStd*w[i+6] + b[i+6]
					drow[i+7] = (srow[i+7]-mean)*invStd*w[i+7] + b[i+7]
					drow[i+8] = (srow[i+8]-mean)*invStd*w[i+8] + b[i+8]
					drow[i+9] = (srow[i+9]-mean)*invStd*w[i+9] + b[i+9]
					drow[i+10] = (srow[i+10]-mean)*invStd*w[i+10] + b[i+10]
					drow[i+11] = (srow[i+11]-mean)*invStd*w[i+11] + b[i+11]
					drow[i+12] = (srow[i+12]-mean)*invStd*w[i+12] + b[i+12]
					drow[i+13] = (srow[i+13]-mean)*invStd*w[i+13] + b[i+13]
					drow[i+14] = (srow[i+14]-mean)*invStd*w[i+14] + b[i+14]
					drow[i+15] = (srow[i+15]-mean)*invStd*w[i+15] + b[i+15]
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
	if nFrames >= 40 && dim >= 256 {
		svParallelFrames(nFrames, run)
		return
	}
	run(0, nFrames)
}

// svFuseAdd2AndLN512: out += a + b (in-place), dst = LN(out). AVX2 via tensor.
// Dual-frame shares w/bias loads in the affine pass.
func svFuseAdd2AndLN512(out, a, b, dst, w, bias []float32, nFrames int) {
	const dim = 512
	f := 0
	for ; f+1 < nFrames; f += 2 {
		o0, o1 := f*dim, (f+1)*dim
		tensor.FuseAdd2AndLN512Dual(
			out[o0:o0+dim], a[o0:o0+dim], b[o0:o0+dim], dst[o0:o0+dim],
			out[o1:o1+dim], a[o1:o1+dim], b[o1:o1+dim], dst[o1:o1+dim],
			w, bias,
		)
	}
	if f < nFrames {
		off := f * dim
		tensor.FuseAdd2AndLN512(
			out[off:off+dim], a[off:off+dim], b[off:off+dim],
			dst[off:off+dim], w, bias,
		)
	}
}

// svFuseAdd1AndLN512: out += a; dst = LN(out). Entry-layer residual (no x).
func svFuseAdd1AndLN512(out, a, dst, w, bias []float32, nFrames int) {
	const dim = 512
	f := 0
	for ; f+1 < nFrames; f += 2 {
		o0, o1 := f*dim, (f+1)*dim
		tensor.FuseAdd1AndLN512Dual(
			out[o0:o0+dim], a[o0:o0+dim], dst[o0:o0+dim],
			out[o1:o1+dim], a[o1:o1+dim], dst[o1:o1+dim],
			w, bias,
		)
	}
	if f < nFrames {
		off := f * dim
		tensor.FuseAdd1AndLN512(
			out[off:off+dim], a[off:off+dim],
			dst[off:off+dim], w, bias,
		)
	}
}

// svParallelFrames: LN / residual-LN is bandwidth-bound and sits right before
// FFN matmul. Parallelizing thrashs L3; serial keeps the working set hot for FF1.
func svParallelFrames(nFrames int, fn func(fs, fe int)) {
	fn(0, nFrames)
}

// svLayerNormBiasInto512: fixed dim=512 + bias (SenseVoice encoder hot path).
// Dual-frame affine shares w/b loads across consecutive frames.
func svLayerNormBiasInto512(dst, src []float32, nFrames int, w, b []float32) {
	const dim = 512
	f := 0
	for ; f+1 < nFrames; f += 2 {
		o0, o1 := f*dim, (f+1)*dim
		tensor.LayerNormBias512Dual(
			dst[o0:o0+dim], src[o0:o0+dim],
			dst[o1:o1+dim], src[o1:o1+dim],
			w, b,
		)
	}
	if f < nFrames {
		off := f * dim
		tensor.LayerNormBias512(dst[off:off+dim], src[off:off+dim], w, b)
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
	bufs := &svEncoderBufs{}
	svFSMNInto(out, v, kernel, nFrames, hidden, kernelSize, bufs)
	return out
}

func svAddPosEncoding(x []float32, nFrames, dim int) {
	bufs := &svEncoderBufs{}
	svAddPosEncodingCached(x, nFrames, dim, bufs)
}
