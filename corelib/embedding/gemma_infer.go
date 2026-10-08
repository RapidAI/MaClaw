package embedding

import (
	"fmt"
	"math"
	"os"
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
	fuse := g.useFusion()
	if seq > 0 && !g.skipPack && (seq <= 4 || (fuse && tensor.HasAVX512VNNI())) {
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

	for l := 0; l < nLayers; l++ {
		layer := &g.weights.layers[l]
		// Per-layer attention config.  Both the window width and the RoPE base
		// are layer-dependent in llama.cpp (for this model layers 5/11/17/23 are
		// global and rotate at 1e6 while the other 20 are windowed and rotate at
		// 1e4), so neither can be hoisted out of the loop.  The table choice is
		// gated on len()>0 as well as the pattern: an empty pair means the two
		// bases are equal and ropeCos/ropeSin are already correct.
		halfW := hp.HalfWindowFor(l)
		ropeCos, ropeSin := sc.ropeCos, sc.ropeSin
		if len(sc.ropeCosSwa) > 0 && hp.IsSwaLayer(l) {
			ropeCos, ropeSin = sc.ropeCosSwa, sc.ropeSinSwa
		}
		tensor.RMSNormRows(normed, x, layer.attnNormW, seq, dim, hp.RMSNormEps)
		if fuse && !fuseOffQKV {
			tensor.MatMulQ8PackedQKV(q, k, v, normed, &layer.attnQWeight, &layer.attnKWeight, &layer.attnVWeight, seq, maxWorkers)
		} else {
			tensor.MatMulQ8N(q, normed, &layer.attnQWeight, seq, dim, dim, maxWorkers)
			tensor.MatMulQ8N(k, normed, &layer.attnKWeight, seq, kvDim, dim, maxWorkers)
			tensor.MatMulQ8N(v, normed, &layer.attnVWeight, seq, kvDim, dim, maxWorkers)
		}
		tensor.RMSNormRoPESeq(q, layer.attnQNormW, ropeCos, ropeSin, seq, nHeads, headDim, hp.RMSNormEps)
		tensor.RMSNormRoPESeq(k, layer.attnKNormW, ropeCos, ropeSin, seq, nKVHeads, headDim, hp.RMSNormEps)
		g.gqaAttention(attnOut, q, k, v, seq, nHeads, nKVHeads, headDim, dim, kvDim, halfW)
		if fuse && !fuseOffAttRes {
			tensor.MatMulQ8RMSResidual(x, attnOut, sc.yTile, &layer.attnOutWeight, layer.postAttnNormW, seq, dim, dim, 8, maxWorkers, hp.RMSNormEps)
		} else {
			tensor.MatMulQ8N(projOut, attnOut, &layer.attnOutWeight, seq, dim, dim, maxWorkers)
			tensor.RMSNormRows(projOut, projOut, layer.postAttnNormW, seq, dim, hp.RMSNormEps)
			tensor.Add(x, x, projOut)
		}
		tensor.RMSNormRows(normed, x, layer.ffNormW, seq, dim, hp.RMSNormEps)
		if fuse && !fuseOffFFN {
			tensor.MatMulQ8DualOut(ffGate, ffUp, normed, &layer.ffGateWeight, &layer.ffUpWeight, seq, maxWorkers)
		} else {
			tensor.MatMulQ8N(ffGate, normed, &layer.ffGateWeight, seq, ffDim, dim, maxWorkers)
			tensor.MatMulQ8N(ffUp, normed, &layer.ffUpWeight, seq, ffDim, dim, maxWorkers)
			tensor.GeluMul(ffGate, ffUp)
		}
		if fuse && !fuseOffFFNRes {
			tensor.MatMulQ8RMSResidual(x, ffGate, sc.yTile, &layer.ffDownWeight, layer.postFFNNormW, seq, dim, ffDim, 8, maxWorkers, hp.RMSNormEps)
		} else {
			tensor.MatMulQ8N(ffDown, ffGate, &layer.ffDownWeight, seq, dim, ffDim, maxWorkers)
			tensor.RMSNormRows(ffDown, ffDown, layer.postFFNNormW, seq, dim, hp.RMSNormEps)
			tensor.Add(x, x, ffDown)
		}
	}
	tensor.RMSNormRows(x, x, g.weights.outputNorm, seq, dim, hp.RMSNormEps)
}

// swaRange is the visible key range [lo, hi) for query position p: llama.cpp's
// LLAMA_SWA_TYPE_SYMMETRIC mask, where `|p1 - p0| > n_swa/2` is the *masked*
// condition.  So the band is inclusive at both ends and 2*halfW+1 wide, and
// halfW < 0 means full attention.
//
// A free function rather than three lines inside gqaHead so the test can check
// it against the mask predicate itself.  The two ways to get this wrong -- using
// n_swa instead of n_swa/2, and an exclusive instead of inclusive far edge --
// are both off-by-a-constant and still produce a plausible cosine rather than a
// failure, so they need to be pinned by construction (TestSwaRange).
func swaRange(p, seq, halfW int) (int, int) {
	if halfW < 0 {
		return 0, seq
	}
	lo := p - halfW
	if lo < 0 {
		lo = 0
	}
	hi := p + halfW + 1
	if hi > seq {
		hi = seq
	}
	return lo, hi
}

// gqaAttention computes grouped-query attention using SIMD-accelerated dot products.
//
// `halfW` is the symmetric half-width of the attention band for this layer, or
// -1 for full attention (see GemmaHParams.HalfWindowFor).  The window is applied
// by softmaxing over the visible slice rather than by writing -inf into the
// masked lanes; those are the same thing mathematically, and it needs no new
// kernel and no branch inside the inner loop.
//
// For seq>=64 the per-head loops run in parallel goroutines (heads write
// disjoint out column ranges, each goroutine owns its score tile). For any
// seq the batched nQ=4 tile path is used — the old seq<=512 cutoff would
// otherwise dump every query of a long text into the single-query strided
// path, which is ~two orders of magnitude slower per token (measured: ~29%
// of a ~900-token Embed in weightedSumContigN alone).
func (g *GemmaEmbedder) gqaAttention(out, q, k, v []float32,
	seq, nHeads, nKVHeads, headDim, qStride, kvStride, halfW int) {

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	headsPerGroup := nHeads / nKVHeads
	nQ := 8
	if seq > 0 && seq < nQ {
		nQ = seq // seq=3 short Embed: one batched nQ=3 tile, not 3 leftover passes
	}
	if nHeads > 1 && seq >= 64 {
		var wg sync.WaitGroup
		for h := 0; h < nHeads; h++ {
			wg.Add(1)
			go func(h int) {
				defer wg.Done()
				tilep := gqaTilePool.Get().(*[]float32)
				tile := *tilep
				if cap(tile) < 8*seq {
					tile = make([]float32, 8*seq)
					*tilep = tile
				} else {
					tile = tile[:8*seq]
				}
				defer gqaTilePool.Put(tilep)
				g.gqaHead(tile, out, q, k, v, h, headsPerGroup, seq, nQ, scale, headDim, qStride, kvStride, halfW)
			}(h)
		}
		wg.Wait()
		return
	}
	tilep := gqaTilePool.Get().(*[]float32)
	tile := *tilep
	if cap(tile) < 8*seq {
		tile = make([]float32, 8*seq)
		*tilep = tile
	} else {
		tile = tile[:8*seq]
	}
	defer gqaTilePool.Put(tilep)
	for h := 0; h < nHeads; h++ {
		g.gqaHead(tile, out, q, k, v, h, headsPerGroup, seq, nQ, scale, headDim, qStride, kvStride, halfW)
	}
}

var gqaTilePool = sync.Pool{New: func() any { p := make([]float32, 8*512); return &p }}

// gqaHead computes attention for one head over all query positions. tile is
// scratch of at least 8*seq (batched score rows); the leftover single-query
// path reuses tile[:seq]. Bulk tiles run at nQ=8 (K/V loaded once per 8
// queries); a leftover 4..7 queries run as one nQ=4 tile before falling back
// to the single-query strided path.
func (g *GemmaEmbedder) gqaHead(tile, out, q, k, v []float32, h, headsPerGroup, seq, nQ int, scale float32, headDim, qStride, kvStride, halfW int) {
	kvH := h / headsPerGroup
	vBase := v[kvH*headDim:]
	hOff := h * headDim

	sq := 0
	for ; sq+nQ <= seq; sq += nQ {
		g.gqaTile(tile, out, q, k, vBase, sq, nQ, scale, headDim, hOff, seq, qStride, kvStride, kvH*headDim, halfW)
	}
	if rem := seq - sq; rem >= 4 {
		g.gqaTile(tile, out, q, k, vBase, sq, 4, scale, headDim, hOff, seq, qStride, kvStride, kvH*headDim, halfW)
		sq += 4
	}
	for ; sq < seq; sq++ {
		// Single-query leftover: its own visible range, both pointers offset and
		// the row shortened -- the port's design, and the reason the tile index
		// is not simply 0-based.
		lo, hi := swaRange(sq, seq, halfW)
		qVec := q[sq*qStride+hOff : sq*qStride+hOff+headDim]
		for sk := lo; sk < hi; sk++ {
			tile[sk-lo] = tensor.Dot(qVec, k[sk*kvStride+kvH*headDim:(sk*kvStride+kvH*headDim)+headDim]) * scale
		}
		tensor.SoftmaxWeightedSumStrided(out[sq*qStride+hOff:sq*qStride+hOff+headDim], tile[:hi-lo], vBase[lo*kvStride:], hi-lo, kvStride, headDim)
	}
}

// gqaTile computes Q·K scores and the softmax-weighted V sum for one tile of
// nQ queries (nQ∈{4,8}) starting at query row sq. kOff is the head's column
// offset within each K/V row (kvH*headDim).
//
// With a window, every query in the tile has its *own* visible range, so the
// scores are computed over the tile's union range [lo, hi) and each row is then
// softmaxed over its own sub-slice.  That keeps the batched dot path (K rows
// streamed once per nQ queries) intact -- a per-query fallback would lose it --
// and costs only the (nQ-1) extra columns the union spans beyond each row's own
// band.  SoftmaxWeightedSumBatched cannot be used here: it strides rows by
// `seq`, which is not the union width.
func (g *GemmaEmbedder) gqaTile(tile, out, q, k, vBase []float32, sq, nQ int, scale float32, headDim, hOff, seq, qStride, kvStride, kOff, halfW int) {
	lo, hi := 0, seq
	if halfW >= 0 {
		lo, _ = swaRange(sq, seq, halfW)
		_, hi = swaRange(sq+nQ-1, seq, halfW)
	}
	W := hi - lo
	rows := tile[:nQ*W]

	if (nQ == 8 || nQ == 4) && headDim <= 256 {
		// Q·K with K rows loaded once per nQ-query tile: q rows are strided
		// in q, so stage them contiguously, then multiDot4/8 streams each
		// K row a single time (was: nQ separate dots, nQ× K traffic).
		var qTile [8 * 256]float32
		for t := 0; t < nQ; t++ {
			copy(qTile[t*headDim:(t+1)*headDim], q[(sq+t)*qStride+hOff:(sq+t)*qStride+hOff+headDim])
		}
		aPanel := qTile[:nQ*headDim]
		if nQ == 8 {
			var d8 [8]float32
			for sk := lo; sk < hi; sk++ {
				kRow := k[sk*kvStride+kOff : sk*kvStride+kOff+headDim]
				tensor.MultiDot8(&d8, aPanel, kRow, headDim)
				for t := 0; t < 8; t++ {
					rows[t*W+sk-lo] = d8[t] * scale
				}
			}
		} else {
			var d4 [4]float32
			for sk := lo; sk < hi; sk++ {
				kRow := k[sk*kvStride+kOff : sk*kvStride+kOff+headDim]
				tensor.MultiDot4(&d4, aPanel, kRow, headDim)
				rows[sk-lo], rows[W+sk-lo], rows[2*W+sk-lo], rows[3*W+sk-lo] =
					d4[0]*scale, d4[1]*scale, d4[2]*scale, d4[3]*scale
			}
		}
	} else {
		for t := 0; t < nQ; t++ {
			qVec := q[(sq+t)*qStride+hOff : (sq+t)*qStride+hOff+headDim]
			row := rows[t*W : (t+1)*W]
			for sk := lo; sk < hi; sk++ {
				row[sk-lo] = tensor.Dot(qVec, k[sk*kvStride+kOff:sk*kvStride+kOff+headDim]) * scale
			}
		}
	}

	if halfW < 0 {
		tensor.SoftmaxWeightedSumBatched(out, tile[:nQ*seq], vBase, nQ, seq, kvStride, headDim, qStride, hOff, sq)
		return
	}
	for t := 0; t < nQ; t++ {
		rlo, rhi := swaRange(sq+t, seq, halfW)
		row := rows[t*W+(rlo-lo) : t*W+(rhi-lo)]
		o := out[(sq+t)*qStride+hOff : (sq+t)*qStride+hOff+headDim]
		tensor.SoftmaxWeightedSumStrided(o, row, vBase[rlo*kvStride:], rhi-rlo, kvStride, headDim)
	}
}

// Per-op fused-path diagnostics.
//
// MACLAW_EMBED_FUSION=0 turns the whole fused branch off, but that only says
// what fusion costs *in total*.  These four turn off one fused op each, which
// is what makes the cost attributable: measured against llama.cpp on the
// built-in corpus, the fused path is worth minCos 0.99891 and turning all four
// off gives 0.99989, with no single op dominating (FFNRES 6.0e-4, QKV 3.4e-4,
// FFN 2.6e-4, ATTRES -7.4e-5 -- i.e. one op *helps*).  See TestFusionOpCost.
//
// The non-fused equivalents are not a free replacement: measured interleaved,
// turning fusion off costs 1.72x single-text latency and 1.63x batch throughput
// (p50 9.79 -> 16.79 ms, 99.6 -> 61.1 t/s), so this is a real tradeoff and the
// default stays fused.
//
// Read once at init, so a normal forward pays nothing for them.
var (
	fuseOffQKV    = envFlag("MACLAW_NO_FUSE_QKV")
	fuseOffAttRes = envFlag("MACLAW_NO_FUSE_ATTRES")
	fuseOffFFN    = envFlag("MACLAW_NO_FUSE_FFN")
	fuseOffFFNRes = envFlag("MACLAW_NO_FUSE_FFNRES")
)

func envFlag(name string) bool {
	v := os.Getenv(name)
	return v != "" && v != "0"
}
