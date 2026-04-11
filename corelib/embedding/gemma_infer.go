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
// Uses a shared scratch buffer protected by a mutex — suitable for sequential calls.
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
	emb, err := g.forwardWithScratch(tokens, s)
	g.putScratchToPool(s)
	if err != nil {
		return nil, err
	}

	return g.truncateAndNormalize(emb), nil
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
	if maxWorkers > len(texts) {
		maxWorkers = len(texts)
	}

	// Disable internal MatMul parallelism — we're parallelizing at the batch
	// level instead. Each goroutine runs a full single-threaded inference,
	// which avoids nested goroutine contention and maximizes CPU utilization.
	tensor.SetMatMulMaxParallel(1)
	defer tensor.SetMatMulMaxParallel(0) // restore default

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

// newGemmaScratch allocates a fresh set of scratch buffers for the given
// hyperparameters and sequence length. Each concurrent inference goroutine
// gets its own scratch to avoid contention.
func newGemmaScratch(hp GemmaHParams, seq int) *gemmaScratch {
	dim := hp.Dim
	kvDim := hp.KVDim
	ffDim := hp.FFDim
	headDim := hp.HeadDim
	halfDim := headDim / 2

	// Pre-compute RoPE cos/sin tables for all positions
	ropeCos := make([]float32, seq*halfDim)
	ropeSin := make([]float32, seq*halfDim)
	for pos := 0; pos < seq; pos++ {
		for i := 0; i < halfDim; i++ {
			freq := 1.0 / float32(math.Pow(float64(hp.RopeTheta), float64(2*i)/float64(headDim)))
			angle := float32(pos) * freq
			ropeCos[pos*halfDim+i] = float32(math.Cos(float64(angle)))
			ropeSin[pos*halfDim+i] = float32(math.Sin(float64(angle)))
		}
	}

	return &gemmaScratch{
		x:       make([]float32, seq*dim),
		normed:  make([]float32, seq*dim),
		q:       make([]float32, seq*dim),
		k:       make([]float32, seq*kvDim),
		v:       make([]float32, seq*kvDim),
		attnOut: make([]float32, seq*dim),
		projOut: make([]float32, seq*dim),
		ffGate:  make([]float32, seq*ffDim),
		ffUp:    make([]float32, seq*ffDim),
		ffDown:  make([]float32, seq*dim),
		rowBuf:  make([]float32, dim),
		scores:  make([]float32, seq),
		poolOut: make([]float32, dim),
		ropeCos: ropeCos,
		ropeSin: ropeSin,
		seqCap:  seq,
		ropeSeq: seq,
	}
}

// getScratchFromPool retrieves a scratch buffer from the pool, or allocates
// a new one if the pooled buffer is too small for the given sequence length.
func (g *GemmaEmbedder) getScratchFromPool(seq int) *gemmaScratch {
	if v := g.scratchPool.Get(); v != nil {
		s := v.(*gemmaScratch)
		if s.seqCap >= seq {
			// Recompute RoPE tables only if seq changed (tables are position-dependent)
			g.recomputeRoPE(s, seq)
			return s
		}
		// Too small — discard and allocate fresh
	}
	return newGemmaScratch(g.hp, seq)
}

// putScratchToPool returns a scratch buffer to the pool for reuse.
func (g *GemmaEmbedder) putScratchToPool(s *gemmaScratch) {
	g.scratchPool.Put(s)
}

// recomputeRoPE updates the pre-computed RoPE cos/sin tables for a new seq length.
// Skips recomputation if the tables already cover the requested seq length.
func (g *GemmaEmbedder) recomputeRoPE(s *gemmaScratch, seq int) {
	if s.ropeSeq >= seq {
		return // tables already valid for this seq length
	}
	headDim := g.hp.HeadDim
	halfDim := headDim / 2
	for pos := 0; pos < seq; pos++ {
		for i := 0; i < halfDim; i++ {
			freq := 1.0 / float32(math.Pow(float64(g.hp.RopeTheta), float64(2*i)/float64(headDim)))
			angle := float32(pos) * freq
			s.ropeCos[pos*halfDim+i] = float32(math.Cos(float64(angle)))
			s.ropeSin[pos*halfDim+i] = float32(math.Sin(float64(angle)))
		}
	}
	s.ropeSeq = seq
}

// ensureScratch returns scratch buffers large enough for the given seq length.
// Buffers are allocated once and reused; reallocated only if seq exceeds previous capacity.
// Only used by the mutex-protected Embed path.
func (g *GemmaEmbedder) ensureScratch(seq int) *gemmaScratch {
	if g.scratch != nil && g.scratch.seqCap >= seq {
		return g.scratch
	}
	g.scratch = newGemmaScratch(g.hp, seq)
	return g.scratch
}

// forward runs the Gemma2 transformer using the shared scratch (mutex-protected path).
func (g *GemmaEmbedder) forward(tokenIDs []int) ([]float32, error) {
	sc := g.ensureScratch(len(tokenIDs))
	return g.forwardWithScratch(tokenIDs, sc)
}

// forwardWithScratch runs the Gemma2 transformer with an externally provided
// scratch buffer. This is the core inference function — safe to call from
// multiple goroutines as long as each has its own scratch and weights are
// read-only (mmap-backed Q8 tensors).
func (g *GemmaEmbedder) forwardWithScratch(tokenIDs []int, sc *gemmaScratch) ([]float32, error) {
	hp := g.hp
	seq := len(tokenIDs)
	dim := hp.Dim
	kvDim := hp.KVDim
	headDim := hp.HeadDim
	nHeads := hp.NHeads
	nKVHeads := hp.NKVHeads
	ffDim := hp.FFDim

	// Token embedding lookup: use cache for hot tokens, dequantize for the rest.
	x := sc.x[:seq*dim]
	embScale := float32(math.Sqrt(float64(dim)))
	for si, id := range tokenIDs {
		if id < 0 || id >= hp.VocabSize {
			return nil, fmt.Errorf("gemma: token id %d out of range [0,%d)", id, hp.VocabSize)
		}
		dst := x[si*dim : (si+1)*dim]
		if cached := g.tokenCache.Get(id); cached != nil {
			copy(dst, cached)
		} else {
			g.weights.tokenEmb.DequantRow(id, sc.rowBuf)
			copy(dst, sc.rowBuf)
		}
		tensor.Scale(dst, embScale)
	}

	normed := sc.normed[:seq*dim]
	q := sc.q[:seq*dim]
	k := sc.k[:seq*kvDim]
	v := sc.v[:seq*kvDim]
	attnOut := sc.attnOut[:seq*dim]
	projOut := sc.projOut[:seq*dim]
	ffGate := sc.ffGate[:seq*ffDim]
	ffUp := sc.ffUp[:seq*ffDim]
	ffDown := sc.ffDown[:seq*dim]
	halfDim := headDim / 2

	// Early exit: run fewer layers when output dim is small (MRL truncation).
	nLayers := hp.NLayers
	if ee := int(atomic.LoadInt32(&g.earlyExit)); ee > 0 && ee < nLayers {
		nLayers = ee
	}

	for l := 0; l < nLayers; l++ {
		layer := &g.weights.layers[l]

		// === Self-attention ===
		// Pre-attention RMSNorm
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.attnNormW, hp.RMSNormEps)
		}

		// Q, K, V projections — Q8 MatMul (normed is float32, weights are Q8)
		tensor.MatMulQ8(q, normed, &layer.attnQWeight, seq, dim, dim)
		tensor.MatMulQ8(k, normed, &layer.attnKWeight, seq, kvDim, dim)
		tensor.MatMulQ8(v, normed, &layer.attnVWeight, seq, kvDim, dim)

		// QK-norm + RoPE per position (using pre-computed cos/sin tables)
		for s := 0; s < seq; s++ {
			for h := 0; h < nHeads; h++ {
				off := s*dim + h*headDim
				// In-place RMSNorm — avoids copy through temp buffer.
				tensor.RMSNorm(q[off:off+headDim], q[off:off+headDim], layer.attnQNormW, hp.RMSNormEps)
			}
			for h := 0; h < nKVHeads; h++ {
				off := s*kvDim + h*headDim
				tensor.RMSNorm(k[off:off+headDim], k[off:off+headDim], layer.attnKNormW, hp.RMSNormEps)
			}
			cosTab := sc.ropeCos[s*halfDim : (s+1)*halfDim]
			sinTab := sc.ropeSin[s*halfDim : (s+1)*halfDim]
			tensor.RoPEPrecomputed(q[s*dim:(s+1)*dim], nHeads, headDim, cosTab, sinTab)
			tensor.RoPEPrecomputed(k[s*kvDim:(s+1)*kvDim], nKVHeads, headDim, cosTab, sinTab)
		}

		// GQA attention
		g.gqaAttention(attnOut, q, k, v, seq, nHeads, nKVHeads, headDim, dim, kvDim, sc.scores[:seq])

		// Output projection — Q8 MatMul
		tensor.MatMulQ8(projOut, attnOut, &layer.attnOutWeight, seq, dim, dim)

		// Post-attention norm + residual
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(projOut[s*dim:(s+1)*dim], projOut[s*dim:(s+1)*dim], layer.postAttnNormW, hp.RMSNormEps)
		}
		tensor.Add(x, x, projOut)

		// === FFN ===
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.ffNormW, hp.RMSNormEps)
		}

		// Gate + Up — Q8 MatMul
		tensor.MatMulQ8(ffGate, normed, &layer.ffGateWeight, seq, ffDim, dim)
		tensor.MatMulQ8(ffUp, normed, &layer.ffUpWeight, seq, ffDim, dim)
		// Fused SiLU(gate) * up — saves one full pass over ffDim*seq elements
		tensor.SiLUMul(ffGate, ffUp)

		// Down projection — Q8 MatMul
		tensor.MatMulQ8(ffDown, ffGate, &layer.ffDownWeight, seq, dim, ffDim)

		// Post-FFN norm + residual
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(ffDown[s*dim:(s+1)*dim], ffDown[s*dim:(s+1)*dim], layer.postFFNNormW, hp.RMSNormEps)
		}
		tensor.Add(x, x, ffDown)
	}

	// Final RMSNorm
	for s := 0; s < seq; s++ {
		tensor.RMSNorm(x[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], g.weights.outputNorm, hp.RMSNormEps)
	}

	// Mean pooling — use scratch buffer and SIMD-accelerated addition
	out := sc.poolOut[:dim]
	for i := range out {
		out[i] = 0
	}
	for s := 0; s < seq; s++ {
		tensor.Add(out, out, x[s*dim:(s+1)*dim])
	}
	tensor.Scale(out, 1.0/float32(seq))

	// Copy result out of scratch so caller owns the memory.
	result := make([]float32, dim)
	copy(result, out)
	return result, nil
}

// gqaAttention computes grouped-query attention using SIMD-accelerated dot products.
func (g *GemmaEmbedder) gqaAttention(out, q, k, v []float32,
	seq, nHeads, nKVHeads, headDim, qStride, kvStride int, scores []float32) {

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	headsPerGroup := nHeads / nKVHeads

	for h := 0; h < nHeads; h++ {
		kvH := h / headsPerGroup

		for sq := 0; sq < seq; sq++ {
			qOff := sq*qStride + h*headDim
			qVec := q[qOff : qOff+headDim]

			// Score computation: use SIMD dot product instead of scalar loop
			for sk := 0; sk < seq; sk++ {
				kOff := sk*kvStride + kvH*headDim
				scores[sk] = tensor.Dot(qVec, k[kOff:kOff+headDim]) * scale
			}

			tensor.Softmax(scores[:seq])

			// Weighted sum of V: fused scale-add without Axpy pool overhead.
			// For typical seq=40, headDim=256, this is ~10K FMAs — fast enough
			// as a scalar loop that the compiler can auto-vectorize.
			outOff := sq*qStride + h*headDim
			outSlice := out[outOff : outOff+headDim]
			for i := range outSlice {
				outSlice[i] = 0
			}
			for sk := 0; sk < seq; sk++ {
				s := scores[sk]
				if s == 0 {
					continue
				}
				vOff := sk*kvStride + kvH*headDim
				vSlice := v[vOff : vOff+headDim]
				for i := 0; i < headDim; i++ {
					outSlice[i] += s * vSlice[i]
				}
			}
		}
	}
}
