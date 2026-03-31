package asr

import (
"math"
"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
"github.com/viterin/vek/vek32"
)

type kvCache struct {
selfK   [][]float32 // [layer] pre-allocated, len grows with selfLen*dim
selfV   [][]float32
selfLen int
crossK  [][]float32 // [layer][encFrames * dim], computed once
crossV  [][]float32
}

// decoderBufs holds reusable scratch buffers for decoder steps.
type decoderBufs struct {
x, residual, q, kNew, vNew, cq     []float32
projOut, crossProj, downOut, fc1Out []float32
logits                              []float32
}

func newDecoderBufs(dim, ffDim2x, vocabSize int) *decoderBufs {
return &decoderBufs{
x: make([]float32, dim), residual: make([]float32, dim),
q: make([]float32, dim), kNew: make([]float32, dim), vNew: make([]float32, dim),
cq: make([]float32, dim), projOut: make([]float32, dim),
crossProj: make([]float32, dim), downOut: make([]float32, dim),
fc1Out: make([]float32, ffDim2x), logits: make([]float32, vocabSize),
}
}

func (m *MoonshineModel) decode(encOut []float32, encFrames int) ([]int, error) {
hp := m.hp
dim := hp.DecoderDim
nLayers := hp.DecoderDepth
cache := &kvCache{
selfK: make([][]float32, nLayers), selfV: make([][]float32, nLayers),
crossK: make([][]float32, nLayers), crossV: make([][]float32, nLayers),
}
for li := 0; li < nLayers; li++ {
l := &m.w.decLayers[li]
cache.selfK[li] = make([]float32, 0, hp.MaxSeqLen*dim)
cache.selfV[li] = make([]float32, 0, hp.MaxSeqLen*dim)
cache.crossK[li] = make([]float32, encFrames*dim)
cache.crossV[li] = make([]float32, encFrames*dim)
tensor.MatMul(cache.crossK[li], encOut, l.crossKW, encFrames, dim, dim)
tensor.MatMul(cache.crossV[li], encOut, l.crossVW, encFrames, dim, dim)
}
ffDim2x := len(m.w.decLayers[0].ffUpW) / dim
bufs := newDecoderBufs(dim, ffDim2x, hp.VocabSize)
tokens := []int{hp.BOSID}
for step := 0; step < hp.MaxSeqLen; step++ {
m.decoderStep(cache, bufs, step, tokens[len(tokens)-1], encFrames)
bufs.logits[0] = float32(math.Inf(-1))
if hp.BOSID >= 0 && hp.BOSID < len(bufs.logits) {
bufs.logits[hp.BOSID] = float32(math.Inf(-1))
}
bestID := 0
bestVal := bufs.logits[0]
for i := 1; i < len(bufs.logits); i++ {
if bufs.logits[i] > bestVal { bestVal = bufs.logits[i]; bestID = i }
}
if bestID == hp.EOSID { break }
tokens = append(tokens, bestID)
}
return tokens, nil
}

func (m *MoonshineModel) decoderStep(cache *kvCache, b *decoderBufs, step, curToken, encFrames int) {
	hp := m.hp
	dim := hp.DecoderDim
	nHeads := hp.DecoderHeads
	headDim := hp.DecoderHDim
	x := b.x
	if curToken >= 0 && curToken < hp.VocabSize {
		copy(x, m.w.tokenEmb[curToken*dim:(curToken+1)*dim])
	} else {
		for i := range x { x[i] = 0 }
	}
	for li := 0; li < hp.DecoderDepth; li++ {
		l := &m.w.decLayers[li]
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.selfNormW, 1e-5)
		tensor.MatMul(b.q, x, l.selfQW, 1, dim, dim)
		tensor.MatMul(b.kNew, x, l.selfKW, 1, dim, dim)
		tensor.MatMul(b.vNew, x, l.selfVW, 1, dim, dim)
		tensor.RoPEInterleaved(b.q, nHeads, headDim, step, hp.RopeTheta, hp.PartialRot)
		tensor.RoPEInterleaved(b.kNew, nHeads, headDim, step, hp.RopeTheta, hp.PartialRot)
		cache.selfK[li] = append(cache.selfK[li], b.kNew...)
		cache.selfV[li] = append(cache.selfV[li], b.vNew...)
		seqK := step + 1
		sdpaSingle(b.q, cache.selfK[li], cache.selfV[li], x, seqK, nHeads, headDim)
		tensor.MatMul(b.projOut, x, l.selfOutW, 1, dim, dim)
		tensor.Add(x, b.residual, b.projOut)
		// Cross-attention
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.crossNormW, 1e-5)
		tensor.MatMul(b.cq, x, l.crossQW, 1, dim, dim)
		sdpaSingle(b.cq, cache.crossK[li], cache.crossV[li], x, encFrames, nHeads, headDim)
		tensor.MatMul(b.crossProj, x, l.crossOutW, 1, dim, dim)
		tensor.Add(x, b.residual, b.crossProj)
		// SwiGLU FFN
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.ffNormW, 1e-5)
		ffDim2x := len(l.ffUpW) / dim
		tensor.MatMul(b.fc1Out[:ffDim2x], x, l.ffUpW, 1, ffDim2x, dim)
		if l.ffUpB != nil { tensor.Add(b.fc1Out[:ffDim2x], b.fc1Out[:ffDim2x], l.ffUpB) }
		intermediate := ffDim2x / 2
		valuePart := b.fc1Out[:intermediate]
		gatePart := b.fc1Out[intermediate:ffDim2x]
		tensor.SiLU(gatePart)
		tensor.ElemMul(valuePart, gatePart, valuePart)
		tensor.MatMul(b.downOut, valuePart, l.ffDownW, 1, dim, intermediate)
		if l.ffDownB != nil { tensor.Add(b.downOut, b.downOut, l.ffDownB) }
		tensor.Add(x, b.residual, b.downOut)
	}
	tensor.LayerNorm(x, x, m.w.decFinalNormW, 1e-5)
	lmW := m.w.lmHeadW
	if lmW == nil { lmW = m.w.tokenEmb }
	tensor.MatMul(b.logits, x, lmW, 1, hp.VocabSize, dim)
}

// sdpaSingle: optimized single-query attention (seqQ=1) with SIMD dot.
// Writes result into out[dim]. q is [dim], k/v are [seqK*dim].
func sdpaSingle(q, k, v, out []float32, seqK, nHeads, headDim int) {
	dim := nHeads * headDim
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	scores := make([]float32, seqK)
	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		qVec := q[hOff : hOff+headDim]
		for sk := 0; sk < seqK; sk++ {
			scores[sk] = vek32.Dot(qVec, k[sk*dim+hOff:sk*dim+hOff+headDim]) * scale
		}
		tensor.Softmax(scores[:seqK])
		for i := 0; i < headDim; i++ { out[hOff+i] = 0 }
		for sk := 0; sk < seqK; sk++ {
			vOff := sk*dim + hOff
			w := scores[sk]
			for i := 0; i < headDim; i++ { out[hOff+i] += w * v[vOff+i] }
		}
	}
}
