package embedding

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Embed returns the embedding vector for a single text string.
// Uses a shared scratch buffer protected by a mutex, suitable for sequential calls.
func (g *GemmaEmbedder) Embed(text string) ([]float32, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tokens := g.tokenizer.Encode(text)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("gemma: empty token sequence")
	}
	if len(tokens) > g.hp.MaxSeqLen {
		tokens = tokens[:g.hp.MaxSeqLen]
	}

	emb, err := g.forward(tokens)
	if err != nil {
		return nil, err
	}

	return g.truncateAndNormalize(emb), nil
}

// EmbedTokenStates returns per-token hidden states [seq, dim] without mean pooling.
// Each row is the contextualized embedding for one token.
// Used by TTS to provide per-phoneme BERT-like embeddings.
func (g *GemmaEmbedder) EmbedTokenStates(text string) (states []float32, seq, dim int, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	tokens := g.tokenizer.Encode(text)
	if len(tokens) == 0 {
		return nil, 0, 0, fmt.Errorf("gemma: empty token sequence")
	}
	if len(tokens) > g.hp.MaxSeqLen {
		tokens = tokens[:g.hp.MaxSeqLen]
	}

	sc := g.ensureScratch(len(tokens))
	states, err = g.forwardTokenStates(tokens, sc)
	if err != nil {
		return nil, 0, 0, err
	}
	return states, len(tokens), g.hp.Dim, nil
}

// forwardTokenStates runs the transformer and returns per-token hidden states
// instead of the mean-pooled output.
func (g *GemmaEmbedder) forwardTokenStates(tokenIDs []int, sc *gemmaScratch) ([]float32, error) {
	seq := len(tokenIDs)
	dim := g.hp.Dim
	if err := g.lookupTokens(tokenIDs, sc); err != nil {
		return nil, err
	}
	g.layerLoop(sc, seq, gemmaMatMulWorkers(seq))
	result := make([]float32, seq*dim)
	copy(result, sc.x[:seq*dim])
	return result, nil
}

// EmbedConcurrent returns the embedding vector for a single text string.
// Unlike Embed, it uses a pooled scratch buffer so multiple goroutines
// can run inference in parallel without contending on the shared mutex.
// Weights (mmap-backed, read-only) and tokenizer are safe to share.
func (g *GemmaEmbedder) EmbedConcurrent(text string) ([]float32, error) {
	tokens := g.tokenizer.Encode(text)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("gemma: empty token sequence")
	}
	if len(tokens) > g.hp.MaxSeqLen {
		tokens = tokens[:g.hp.MaxSeqLen]
	}

	s := g.getScratchFromPool(len(tokens))
	emb, err := g.forwardWithScratch(tokens, s, 1)
	if err != nil {
		g.putScratchToPool(s)
		return nil, err
	}
	out := g.truncateAndNormalize(emb)
	g.putScratchToPool(s)
	return out, nil
}

// truncateAndNormalize applies MRL dimension truncation and L2 normalization.
func (g *GemmaEmbedder) truncateAndNormalize(emb []float32) []float32 {
	outDim := g.dim
	if outDim > len(emb) {
		outDim = len(emb)
	}
	result := make([]float32, outDim)
	copy(result, emb[:outDim])
	tensor.L2Normalize(result)
	return result
}

// EmbedBatch returns embeddings for multiple texts using concurrent inference.
func (g *GemmaEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	if len(texts) <= 1 {
		results := make([][]float32, len(texts))
		for i, t := range texts {
			emb, err := g.Embed(t)
			if err != nil {
				return nil, fmt.Errorf("gemma: batch item %d: %w", i, err)
			}
			results[i] = emb
		}
		return results, nil
	}

	results := make([][]float32, len(texts))
	errs := make([]error, len(texts))

	maxWorkers := runtime.NumCPU()
	if maxWorkers > 8 {
		maxWorkers = 8
	}
	if maxWorkers > len(texts) {
		maxWorkers = len(texts)
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	for i, t := range texts {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, text string) {
			defer wg.Done()
			defer func() { <-sem }()
			emb, err := g.EmbedConcurrent(text)
			if err != nil {
				errs[idx] = err
			} else {
				results[idx] = emb
			}
		}(i, t)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("gemma: batch item %d: %w", i, err)
		}
	}
	return results, nil
}

// Dim returns the output embedding dimension.
func (g *GemmaEmbedder) Dim() int { return g.dim }

// ensureScratch returns scratch buffers large enough for the given seq length.
// Only used by the mutex-protected Embed path.
func (g *GemmaEmbedder) ensureScratch(seq int) *gemmaScratch {
	S := scratchBucket(seq)
	if g.scratch != nil && g.scratch.seqCap == S {
		return g.scratch
	}
	g.scratch = newGemmaScratch(g.hp, seq)
	return g.scratch
}

// forward runs the Gemma2 transformer using the shared scratch (mutex-protected path).
func (g *GemmaEmbedder) forward(tokenIDs []int) ([]float32, error) {
	sc := g.ensureScratch(len(tokenIDs))
	return g.forwardWithScratch(tokenIDs, sc, gemmaMatMulWorkers(len(tokenIDs)))
}

// gemmaMatMulWorkers: 0 uses shouldParallel so short M=3 Dual3 can N-split
// across cores (tryGemmaFusedPlain handles M=3 ranges). EmbedConcurrent
// still passes 1 to avoid nested pools.
func gemmaMatMulWorkers(seq int) int {
	_ = seq
	return 0
}

// forwardWithScratch runs the Gemma2 transformer with an externally provided
// scratch buffer. This is the core inference function, safe to call from
// multiple goroutines as long as each has its own scratch and weights are
// read-only (mmap-backed Q8 tensors).
func (g *GemmaEmbedder) forwardWithScratch(tokenIDs []int, sc *gemmaScratch, maxWorkers int) ([]float32, error) {
	hp := g.hp
	seq := len(tokenIDs)
	dim := hp.Dim
	if err := g.lookupTokens(tokenIDs, sc); err != nil {
		return nil, err
	}
	g.layerLoop(sc, seq, maxWorkers)

	out := sc.poolOut[:dim]
	for i := range out {
		out[i] = 0
	}
	x := sc.x[:seq*dim]
	for s := 0; s < seq; s++ {
		tensor.Add(out, out, x[s*dim:(s+1)*dim])
	}
	tensor.Scale(out, 1.0/float32(seq))
	// Alias of scratch poolOut; caller must copy (truncateAndNormalize)
	// before the scratch is reused.
	return out, nil
}

func (g *GemmaEmbedder) lookupTokens(tokenIDs []int, sc *gemmaScratch) error {
	hp := g.hp
	seq := len(tokenIDs)
	dim := hp.Dim
	x := sc.x[:seq*dim]
	embScale := float32(math.Sqrt(float64(dim)))
	for si, id := range tokenIDs {
		if id < 0 || id >= hp.VocabSize {
			return fmt.Errorf("gemma: token id %d out of range [0,%d)", id, hp.VocabSize)
		}
		dst := x[si*dim : (si+1)*dim]
		if cached := g.tokenCache.Get(id); cached != nil {
			copy(dst, cached)
		} else {
			g.weights.tokenEmb.DequantRow(id, dst)
			tensor.Scale(dst, embScale)
		}
	}
	return nil
}

func (g *GemmaEmbedder) useFusion() bool {
	return !g.fusionOff && g.hp.Dim == 768 && g.hp.FFDim == 1152 && g.hp.KVDim == 256 && g.hp.HeadDim == 256
}

func (g *GemmaEmbedder) ensurePackedQS() {
	if g == nil {
		return
	}
	g.packOnce.Do(func() {
		packGemmaLayerQS(&g.weights)
	})
}

func (g *GemmaEmbedder) layerLoop(sc *gemmaScratch, seq, maxWorkers int) {
	if seq > 0 && seq <= 4 && !g.skipPack {
		g.ensurePackedQS()
	}
	hp := g.hp
	dim := hp.Dim
	kvDim := hp.KVDim
	headDim := hp.HeadDim
	nHeads := hp.NHeads
	nKVHeads := hp.NKVHeads
	ffDim := hp.FFDim
	x := sc.x[:seq*dim]
	normed := sc.normed[:seq*dim]
	q := sc.q[:seq*dim]
	k := sc.k[:seq*kvDim]
	v := sc.v[:seq*kvDim]
	attnOut := sc.attnOut[:seq*dim]
	projOut := sc.projOut[:seq*dim]
	ffGate := sc.ffGate[:seq*ffDim]
	ffUp := sc.ffUp[:seq*ffDim]
	ffDown := sc.ffDown[:seq*dim]

	nLayers := hp.NLayers
	if ee := int(atomic.LoadInt32(&g.earlyExit)); ee > 0 && ee < nLayers {
		nLayers = ee
	}
	fuse := g.useFusion()

	for l := 0; l < nLayers; l++ {
		layer := &g.weights.layers[l]
		tensor.RMSNormRows(normed, x, layer.attnNormW, seq, dim, hp.RMSNormEps)
		if fuse {
			tensor.MatMulQ8PackedQKV(q, k, v, normed, &layer.attnQWeight, &layer.attnKWeight, &layer.attnVWeight, seq, maxWorkers)
			tensor.RMSNormRoPESeq(q, layer.attnQNormW, sc.ropeCos, sc.ropeSin, seq, nHeads, headDim, hp.RMSNormEps)
			tensor.RMSNormRoPESeq(k, layer.attnKNormW, sc.ropeCos, sc.ropeSin, seq, nKVHeads, headDim, hp.RMSNormEps)
		} else {
			tensor.MatMulQ8N(q, normed, &layer.attnQWeight, seq, dim, dim, maxWorkers)
			tensor.MatMulQ8N(k, normed, &layer.attnKWeight, seq, kvDim, dim, maxWorkers)
			tensor.MatMulQ8N(v, normed, &layer.attnVWeight, seq, kvDim, dim, maxWorkers)
			tensor.RMSNormRoPESeq(q, layer.attnQNormW, sc.ropeCos, sc.ropeSin, seq, nHeads, headDim, hp.RMSNormEps)
			tensor.RMSNormRoPESeq(k, layer.attnKNormW, sc.ropeCos, sc.ropeSin, seq, nKVHeads, headDim, hp.RMSNormEps)
		}
		g.gqaAttention(attnOut, q, k, v, seq, nHeads, nKVHeads, headDim, dim, kvDim, sc.scores[:seq])
		if fuse {
			tensor.MatMulQ8RMSResidual(x, attnOut, sc.yTile, &layer.attnOutWeight, layer.postAttnNormW, seq, dim, dim, 8, maxWorkers, hp.RMSNormEps)
			tensor.RMSNormRows(normed, x, layer.ffNormW, seq, dim, hp.RMSNormEps)
			tensor.MatMulQ8DualOut(ffGate, ffUp, normed, &layer.ffGateWeight, &layer.ffUpWeight, seq, maxWorkers)
			tensor.MatMulQ8RMSResidual(x, ffGate, sc.yTile, &layer.ffDownWeight, layer.postFFNNormW, seq, dim, ffDim, 8, maxWorkers, hp.RMSNormEps)
		} else {
			tensor.MatMulQ8N(projOut, attnOut, &layer.attnOutWeight, seq, dim, dim, maxWorkers)
			tensor.RMSNormRows(projOut, projOut, layer.postAttnNormW, seq, dim, hp.RMSNormEps)
			tensor.Add(x, x, projOut)
			tensor.RMSNormRows(normed, x, layer.ffNormW, seq, dim, hp.RMSNormEps)
			tensor.MatMulQ8N(ffGate, normed, &layer.ffGateWeight, seq, ffDim, dim, maxWorkers)
			tensor.MatMulQ8N(ffUp, normed, &layer.ffUpWeight, seq, ffDim, dim, maxWorkers)
			tensor.SiLUMul(ffGate, ffUp)
			tensor.MatMulQ8N(ffDown, ffGate, &layer.ffDownWeight, seq, dim, ffDim, maxWorkers)
			tensor.RMSNormRows(ffDown, ffDown, layer.postFFNNormW, seq, dim, hp.RMSNormEps)
			tensor.Add(x, x, ffDown)
		}
	}
	tensor.RMSNormRows(x, x, g.weights.outputNorm, seq, dim, hp.RMSNormEps)
}

// gqaAttention computes grouped-query attention using SIMD-accelerated dot products.
func (g *GemmaEmbedder) gqaAttention(out, q, k, v []float32,
	seq, nHeads, nKVHeads, headDim, qStride, kvStride int, scores []float32) {

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	headsPerGroup := nHeads / nKVHeads
	nQ := 4
	if seq > 0 && seq < nQ {
		nQ = seq // seq=3 short Embed: one batched nQ=3 tile, not 3 leftover passes
	}
	var scoreTile [4 * 512]float32

	for h := 0; h < nHeads; h++ {
		kvH := h / headsPerGroup
		vBase := v[kvH*headDim:]
		hOff := h * headDim

		sq := 0
		if seq <= 512 {
			for ; sq+nQ <= seq; sq += nQ {
				for t := 0; t < nQ; t++ {
					qVec := q[(sq+t)*qStride+hOff : (sq+t)*qStride+hOff+headDim]
					row := scoreTile[t*seq : (t+1)*seq]
					for sk := 0; sk < seq; sk++ {
						row[sk] = tensor.Dot(qVec, k[sk*kvStride+kvH*headDim:(sk*kvStride+kvH*headDim)+headDim]) * scale
					}
				}
				tensor.SoftmaxWeightedSumBatched(out, scoreTile[:nQ*seq], vBase, nQ, seq, kvStride, headDim, qStride, hOff, sq)
			}
		}
		for ; sq < seq; sq++ {
			qVec := q[sq*qStride+hOff : sq*qStride+hOff+headDim]
			for sk := 0; sk < seq; sk++ {
				scores[sk] = tensor.Dot(qVec, k[sk*kvStride+kvH*headDim:(sk*kvStride+kvH*headDim)+headDim]) * scale
			}
			tensor.SoftmaxWeightedSumStrided(out[sq*qStride+hOff:sq*qStride+hOff+headDim], scores[:seq], vBase, seq, kvStride, headDim)
		}
	}
}
