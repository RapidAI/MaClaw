package embedding

import (
	"fmt"
	"math"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Embed returns the embedding vector for a single text string.
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

	outDim := g.dim
	if outDim > len(emb) {
		outDim = len(emb)
	}
	result := make([]float32, outDim)
	copy(result, emb[:outDim])
	tensor.L2Normalize(result)
	return result, nil
}

// EmbedBatch returns embeddings for multiple texts.
func (g *GemmaEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
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

// Dim returns the output embedding dimension.
func (g *GemmaEmbedder) Dim() int { return g.dim }

// Close releases resources.
func (g *GemmaEmbedder) Close() {}

// forward runs the Gemma2 transformer and returns mean-pooled hidden states.
func (g *GemmaEmbedder) forward(tokenIDs []int) ([]float32, error) {
	hp := g.hp
	seq := len(tokenIDs)
	dim := hp.Dim
	kvDim := hp.KVDim
	headDim := hp.HeadDim
	nHeads := hp.NHeads
	nKVHeads := hp.NKVHeads
	ffDim := hp.FFDim

	// Token embedding lookup + scale by sqrt(dim)
	x := make([]float32, seq*dim)
	embScale := float32(math.Sqrt(float64(dim)))
	for s, id := range tokenIDs {
		if id < 0 || id >= hp.VocabSize {
			return nil, fmt.Errorf("gemma: token id %d out of range [0,%d)", id, hp.VocabSize)
		}
		copy(x[s*dim:(s+1)*dim], g.weights.tokenEmb[id*dim:(id+1)*dim])
		tensor.Scale(x[s*dim:(s+1)*dim], embScale)
	}

	// Scratch buffers
	normed := make([]float32, seq*dim)
	q := make([]float32, seq*dim)       // [seq, nHeads*headDim]
	k := make([]float32, seq*kvDim)     // [seq, nKVHeads*headDim]
	v := make([]float32, seq*kvDim)     // [seq, nKVHeads*headDim]
	attnOut := make([]float32, seq*dim) // after concat heads
	projOut := make([]float32, seq*dim)
	ffGate := make([]float32, seq*ffDim)
	ffUp := make([]float32, seq*ffDim)
	ffDown := make([]float32, seq*dim)
	qNormed := make([]float32, headDim)
	kNormed := make([]float32, headDim)

	for l := 0; l < hp.NLayers; l++ {
		layer := &g.weights.layers[l]

		// === Self-attention ===
		// Pre-attention RMSNorm
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.attnNormW, hp.RMSNormEps)
		}

		// Q [seq, dim], K [seq, kvDim], V [seq, kvDim]
		tensor.MatMul(q, normed, layer.attnQWeight, seq, dim, dim)
		tensor.MatMul(k, normed, layer.attnKWeight, seq, kvDim, dim)
		tensor.MatMul(v, normed, layer.attnVWeight, seq, kvDim, dim)

		// QK-norm + RoPE per position
		for s := 0; s < seq; s++ {
			// Q: nHeads heads of headDim each
			for h := 0; h < nHeads; h++ {
				off := s*dim + h*headDim
				tensor.RMSNorm(qNormed, q[off:off+headDim], layer.attnQNormW, hp.RMSNormEps)
				copy(q[off:off+headDim], qNormed)
			}
			// K: nKVHeads heads of headDim each
			for h := 0; h < nKVHeads; h++ {
				off := s*kvDim + h*headDim
				tensor.RMSNorm(kNormed, k[off:off+headDim], layer.attnKNormW, hp.RMSNormEps)
				copy(k[off:off+headDim], kNormed)
			}
			// RoPE
			tensor.RoPE(q[s*dim:(s+1)*dim], nHeads, headDim, s, hp.RopeTheta)
			tensor.RoPE(k[s*kvDim:(s+1)*kvDim], nKVHeads, headDim, s, hp.RopeTheta)
		}

		// GQA: grouped query attention
		g.gqaAttention(attnOut, q, k, v, seq, nHeads, nKVHeads, headDim, dim, kvDim)

		// Output projection
		tensor.MatMul(projOut, attnOut, layer.attnOutWeight, seq, dim, dim)

		// Post-attention norm + residual
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(projOut[s*dim:(s+1)*dim], projOut[s*dim:(s+1)*dim], layer.postAttnNormW, hp.RMSNormEps)
		}
		tensor.Add(x, x, projOut)

		// === FFN ===
		// Pre-FFN RMSNorm
		for s := 0; s < seq; s++ {
			tensor.RMSNorm(normed[s*dim:(s+1)*dim], x[s*dim:(s+1)*dim], layer.ffNormW, hp.RMSNormEps)
		}

		// Gate + Up
		tensor.MatMul(ffGate, normed, layer.ffGateWeight, seq, ffDim, dim)
		tensor.MatMul(ffUp, normed, layer.ffUpWeight, seq, ffDim, dim)
		tensor.SiLU(ffGate)
		tensor.ElemMul(ffGate, ffGate, ffUp)

		// Down projection
		tensor.MatMul(ffDown, ffGate, layer.ffDownWeight, seq, dim, ffDim)

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

	// Mean pooling
	out := make([]float32, dim)
	for s := 0; s < seq; s++ {
		for d := 0; d < dim; d++ {
			out[d] += x[s*dim+d]
		}
	}
	tensor.Scale(out, 1.0/float32(seq))
	return out, nil
}

// gqaAttention computes grouped-query attention.
// Q: [seq, nHeads*headDim], K: [seq, nKVHeads*headDim], V: [seq, nKVHeads*headDim]
// Each query head group shares one KV head.
func (g *GemmaEmbedder) gqaAttention(out, q, k, v []float32,
	seq, nHeads, nKVHeads, headDim, qStride, kvStride int) {

	scale := 1.0 / float32(math.Sqrt(float64(headDim)))
	headsPerGroup := nHeads / nKVHeads
	scores := make([]float32, seq)

	for h := 0; h < nHeads; h++ {
		kvH := h / headsPerGroup // which KV head this Q head uses

		for sq := 0; sq < seq; sq++ {
			qOff := sq*qStride + h*headDim
			qVec := q[qOff : qOff+headDim]

			for sk := 0; sk < seq; sk++ {
				kOff := sk*kvStride + kvH*headDim
				var d float32
				for i := 0; i < headDim; i++ {
					d += qVec[i] * k[kOff+i]
				}
				scores[sk] = d * scale
			}

			tensor.Softmax(scores[:seq])

			outOff := sq*qStride + h*headDim
			for i := 0; i < headDim; i++ {
				out[outOff+i] = 0
			}
			for sk := 0; sk < seq; sk++ {
				vOff := sk*kvStride + kvH*headDim
				w := scores[sk]
				for i := 0; i < headDim; i++ {
					out[outOff+i] += w * v[vOff+i]
				}
			}
		}
	}
}
