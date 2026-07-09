package asr

import (
	"math"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

type kvCache struct {
	selfK  [][]float32 // [layer] pre-allocated, grows with step*dim
	selfV  [][]float32
	crossK [][]float32 // [layer][encFrames * dim], computed once
	crossV [][]float32
}

// decoderBufs holds reusable scratch buffers for decoder steps.
type decoderBufs struct {
	x, residual, q, kNew, vNew, cq      []float32
	projOut, crossProj, downOut, fc1Out []float32
	logits                              []float32
	selfScores                          []float32 // [maxSeqLen] reusable
	crossScores                         []float32 // [maxEncFrames] reusable
	rotaryDim                           int       // cached
	ropeHalfRot                         int       // halfRot for lazy RoPE growth
	ropeFreqs                           []float32 // [halfRot] precomputed freq table
	// RoPE precomputed tables: lazily grown, [pos][halfRotDim]
	ropeCos [][]float32
	ropeSin [][]float32
}

func newDecoderBufs(dim, ffDim2x, vocabSize, maxSeqLen, maxEncFrames int, hp HParams) *decoderBufs {
	headDim := hp.DecoderHDim
	rotaryDim := int(float32(headDim) * hp.PartialRot)
	rotaryDim -= rotaryDim % 2
	halfRot := rotaryDim / 2

	// Pre-allocate RoPE tables for a reasonable initial capacity.
	// Tables grow lazily via ensureRoPE if decoding exceeds this.
	initCap := 64
	if initCap > maxSeqLen {
		initCap = maxSeqLen
	}

	// Precompute frequency table once (only depends on dimension index)
	ropeFreqs := make([]float32, halfRot)
	for i := 0; i < halfRot; i++ {
		ropeFreqs[i] = 1.0 / float32(math.Pow(float64(hp.RopeTheta), float64(2*i)/float64(headDim)))
	}

	b := &decoderBufs{
		x: make([]float32, dim), residual: make([]float32, dim),
		q: make([]float32, dim), kNew: make([]float32, dim), vNew: make([]float32, dim),
		cq: make([]float32, dim), projOut: make([]float32, dim),
		crossProj: make([]float32, dim), downOut: make([]float32, dim),
		fc1Out: make([]float32, ffDim2x), logits: make([]float32, vocabSize),
		selfScores:  make([]float32, maxSeqLen),
		crossScores: make([]float32, maxEncFrames),
		rotaryDim:   rotaryDim,
		ropeHalfRot: halfRot,
		ropeFreqs:   ropeFreqs,
		ropeCos:     make([][]float32, 0, maxSeqLen),
		ropeSin:     make([][]float32, 0, maxSeqLen),
	}
	b.ensureRoPE(initCap)
	return b
}

// ensureRoPE grows the precomputed RoPE tables to cover at least pos positions.
func (b *decoderBufs) ensureRoPE(pos int) {
	for len(b.ropeCos) < pos {
		p := len(b.ropeCos)
		cosRow := make([]float32, b.ropeHalfRot)
		sinRow := make([]float32, b.ropeHalfRot)
		for i := 0; i < b.ropeHalfRot; i++ {
			angle := float32(p) * b.ropeFreqs[i]
			cosRow[i] = float32(math.Cos(float64(angle)))
			sinRow[i] = float32(math.Sin(float64(angle)))
		}
		b.ropeCos = append(b.ropeCos, cosRow)
		b.ropeSin = append(b.ropeSin, sinRow)
	}
}

func matMulLinear(out, a []float32, w linearWeight, M, N, K int) {
	if w.f32 != nil {
		tensor.MatMul(out, a, w.f32, M, N, K)
		return
	}
	tensor.MatMulQ8(out, a, w.q8, M, N, K)
}

// matMulLinearBias is matMulLinear + row-broadcast bias (one write pass over out).
func matMulLinearBias(out, a []float32, w linearWeight, bias []float32, M, N, K int) {
	if w.f32 != nil {
		tensor.MatMulBias(out, a, w.f32, bias, M, N, K)
		return
	}
	tensor.MatMulQ8Bias(out, a, w.q8, bias, M, N, K)
}

// matMulLinearBiasReLU is matMul + bias + ReLU (FFN up-projection).
func matMulLinearBiasReLU(out, a []float32, w linearWeight, bias []float32, M, N, K int) {
	if w.f32 != nil {
		tensor.MatMulBiasReLU(out, a, w.f32, bias, M, N, K)
		return
	}
	tensor.MatMulQ8BiasReLU(out, a, w.q8, bias, M, N, K)
}

// shouldStopNearEOS handles small Go-vs-ggml numeric differences at utterance end.
func shouldStopNearEOS(logits []float32, eosID int, bestVal float32, generated int) bool {
	const minGeneratedTokens = 8
	const eosMargin = 3.0
	if generated < minGeneratedTokens || eosID < 0 || eosID >= len(logits) {
		return false
	}
	return logits[eosID] >= bestVal-eosMargin
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
		matMulLinear(cache.crossK[li], encOut, l.crossKW, encFrames, dim, dim)
		matMulLinear(cache.crossV[li], encOut, l.crossVW, encFrames, dim, dim)
	}

	ffDim2x := m.w.decLayers[0].ffUpW.Rows()
	bufs := newDecoderBufs(dim, ffDim2x, hp.VocabSize, hp.MaxSeqLen, encFrames, hp)

	vocabN := m.activeVocabSize()
	tokens := []int{hp.BOSID}

	// Anti-loop: track consecutive repetitions and n-gram occurrences
	const maxConsecutiveRepeat = 3 // force EOS after same token repeats N times
	const repPenalty float32 = 1.5 // penalize recently generated tokens
	const repWindow = 16           // look-back window for repetition penalty
	const bigramBlockAfter = 1     // block a bigram after it has appeared N times

	consecutiveCount := 0
	lastToken := -1

	for step := 0; step < hp.MaxSeqLen; step++ {
		m.decoderStep(cache, bufs, step, tokens[len(tokens)-1], encFrames, vocabN)

		// Suppress BOS/padding
		bufs.logits[0] = float32(math.Inf(-1))
		if hp.BOSID >= 0 && hp.BOSID < vocabN {
			bufs.logits[hp.BOSID] = float32(math.Inf(-1))
		}

		// Repetition penalty: penalize tokens that appeared recently
		start := len(tokens) - repWindow
		if start < 0 {
			start = 0
		}
		for _, tid := range tokens[start:] {
			if tid >= 0 && tid < vocabN && tid != hp.EOSID {
				if bufs.logits[tid] > 0 {
					bufs.logits[tid] /= repPenalty
				} else {
					bufs.logits[tid] *= repPenalty
				}
			}
		}

		// Bigram blocking: if the previous token + candidate forms a bigram
		// that already appeared in the sequence, suppress the candidate.
		// This prevents short-phrase repetition like "太阳太阳".
		// Only check top candidates (logit > bestLogit - 5.0) to avoid O(vocab*seq) cost.
		if len(tokens) >= 2 {
			prevToken := tokens[len(tokens)-1]
			// Find threshold: only check candidates that are competitive
			var topLogit float32
			for i := 0; i < vocabN; i++ {
				if bufs.logits[i] > topLogit {
					topLogit = bufs.logits[i]
				}
			}
			bigramThreshold := topLogit - 5.0
			for candidate := 0; candidate < vocabN; candidate++ {
				if bufs.logits[candidate] <= bigramThreshold {
					continue
				}
				if bigramCount(tokens, prevToken, candidate) >= bigramBlockAfter {
					bufs.logits[candidate] = float32(math.Inf(-1))
				}
			}
		}

		bestID := 0
		bestVal := bufs.logits[0]
		for i := 1; i < vocabN; i++ {
			if bufs.logits[i] > bestVal {
				bestVal = bufs.logits[i]
				bestID = i
			}
		}
		if bestID == hp.EOSID || shouldStopNearEOS(bufs.logits, hp.EOSID, bestVal, len(tokens)-1) {
			break
		}

		// Consecutive repeat guard
		if bestID == lastToken {
			consecutiveCount++
			if consecutiveCount >= maxConsecutiveRepeat {
				break
			}
		} else {
			consecutiveCount = 0
		}
		lastToken = bestID

		tokens = append(tokens, bestID)
	}
	return tokens, nil
}

func (m *MoonshineModel) decoderStep(cache *kvCache, b *decoderBufs, step, curToken, encFrames, vocabN int) {
	hp := m.hp
	dim := hp.DecoderDim
	nHeads := hp.DecoderHeads
	headDim := hp.DecoderHDim
	rotaryDim := b.rotaryDim

	x := b.x
	tokenRows := len(m.w.tokenEmb) / dim
	if curToken >= 0 && curToken < tokenRows {
		copy(x, m.w.tokenEmb[curToken*dim:(curToken+1)*dim])
	} else {
		for i := range x {
			x[i] = 0
		}
	}

	for li := 0; li < hp.DecoderDepth; li++ {
		l := &m.w.decLayers[li]

		// Self-attention
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.selfNormW, 1e-5)
		matMulLinear(b.q, x, l.selfQW, 1, dim, dim)
		matMulLinear(b.kNew, x, l.selfKW, 1, dim, dim)
		matMulLinear(b.vNew, x, l.selfVW, 1, dim, dim)

		// RoPE with precomputed tables (lazy-grow if needed)
		if step+1 > len(b.ropeCos) {
			b.ensureRoPE(step + 1)
		}
		ropeInterleavedPrecomp(b.q, nHeads, headDim, rotaryDim, b.ropeCos[step], b.ropeSin[step])
		ropeInterleavedPrecomp(b.kNew, nHeads, headDim, rotaryDim, b.ropeCos[step], b.ropeSin[step])

		cache.selfK[li] = append(cache.selfK[li], b.kNew...)
		cache.selfV[li] = append(cache.selfV[li], b.vNew...)
		seqK := step + 1

		sdpaSingleOpt(b.q, cache.selfK[li], cache.selfV[li], x, b.selfScores[:seqK], seqK, nHeads, headDim)
		matMulLinear(b.projOut, x, l.selfOutW, 1, dim, dim)
		tensor.Add(x, b.residual, b.projOut)

		// Cross-attention
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.crossNormW, 1e-5)
		matMulLinear(b.cq, x, l.crossQW, 1, dim, dim)
		sdpaSingleOpt(b.cq, cache.crossK[li], cache.crossV[li], x, b.crossScores[:encFrames], encFrames, nHeads, headDim)
		matMulLinear(b.crossProj, x, l.crossOutW, 1, dim, dim)
		tensor.Add(x, b.residual, b.crossProj)

		// SwiGLU FFN: fused SiLU+Mul
		copy(b.residual, x)
		tensor.LayerNorm(x, x, l.ffNormW, 1e-5)
		ffDim2x := l.ffUpW.Rows()
		matMulLinear(b.fc1Out[:ffDim2x], x, l.ffUpW, 1, ffDim2x, dim)
		if l.ffUpB != nil {
			tensor.Add(b.fc1Out[:ffDim2x], b.fc1Out[:ffDim2x], l.ffUpB)
		}
		intermediate := ffDim2x / 2
		gatePart := b.fc1Out[intermediate:ffDim2x]
		valuePart := b.fc1Out[:intermediate]
		// Fused SiLU(gate) * value in place
		tensor.SiLUMul(gatePart, valuePart)
		matMulLinear(b.downOut, gatePart, l.ffDownW, 1, dim, intermediate)
		if l.ffDownB != nil {
			tensor.Add(b.downOut, b.downOut, l.ffDownB)
		}
		tensor.Add(x, b.residual, b.downOut)
	}

	tensor.LayerNorm(x, x, m.w.decFinalNormW, 1e-5)
	if m.w.lmHeadW != nil {
		tensor.MatMulQ8(b.logits[:vocabN], x, m.w.lmHeadW, 1, vocabN, dim)
	} else {
		tensor.MatMul(b.logits[:vocabN], x, m.w.lmHeadF32, 1, vocabN, dim)
	}
}

// sdpaSingleOpt: optimized single-query attention with caller-provided scores buffer.
// Eliminates per-call allocation. q is [dim], k/v are [seqK*dim], out is [dim].
func sdpaSingleOpt(q, k, v, out, scores []float32, seqK, nHeads, headDim int) {
	dim := nHeads * headDim
	scale := 1.0 / float32(math.Sqrt(float64(headDim)))

	for h := 0; h < nHeads; h++ {
		hOff := h * headDim
		qVec := q[hOff : hOff+headDim]
		for sk := 0; sk < seqK; sk++ {
			scores[sk] = vek32.Dot(qVec, k[sk*dim+hOff:sk*dim+hOff+headDim]) * scale
		}
		outSlice := out[hOff : hOff+headDim]
		tensor.SoftmaxWeightedSumStrided(outSlice, scores[:seqK], v[hOff:], seqK, dim, headDim)
	}
}

// bigramCount counts how many times the bigram (a, b) appears in tokens.
// Used by bigram blocking to prevent short-phrase repetition.
func bigramCount(tokens []int, a, b int) int {
	count := 0
	for i := 0; i < len(tokens)-1; i++ {
		if tokens[i] == a && tokens[i+1] == b {
			count++
		}
	}
	return count
}
