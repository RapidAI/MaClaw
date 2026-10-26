package embedding

// qwen3_pool.go — r13 F1 intent features: last-token AND mean pooling from
// one forward pass, for the intent head only. Embed / EmbedWithRole (Layer 2,
// KB retrieval) are untouched: they still run forwardFrom with the last-layer
// prune and return the identical vector.
//
// Mean = L2-normalised average of the final-normed hidden states of every
// token of the prompted sequence (instruct prefix + text + EOS), matching
// HF last_hidden_state masked mean. With the prefix-KV cache the prefix rows
// are not recomputed; their summed final states are cached with the prefix
// (qwen3PrefixKV.rowSum). Metal: the GPU path has no per-row output, so the
// pooled embed always runs on the CPU kernels (CPU fallback).

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// PooledEmbedder returns last-token and mean-pooled vectors from one pass.
type PooledEmbedder interface {
	EmbedPooledWithRole(text string, role Role) (last, mean []float32, err error)
}

// finishVector truncates an output-normed hidden vector to the embedder's width
// and L2-normalises it. Every vector the embedder returns goes through here, so
// the MRL truncation and the normalisation cannot diverge between paths.
func (q *Qwen3Embedder) finishVector(h []float32) []float32 {
	out := make([]float32, q.dim)
	copy(out, h[:q.dim])
	tensor.L2Normalize(out)
	return out
}

// pooledGPU runs the pooled embedding on the Vulkan backend. ok=false means the
// caller must use the CPU path (backend failed or was released).
func (q *Qwen3Embedder) pooledGPU(g *qwen3VKGPU, ids []int) (last, mean []float32, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			last, mean, ok = nil, nil, false
		}
	}()
	dim := q.hp.Dim
	hidL := make([]float32, dim)
	hidM := make([]float32, dim)
	if err := g.forwardPooled(q, ids, hidL, hidM); err != nil {
		if q.vkGPU.CompareAndSwap(g, nil) {
			log.Printf("qwen3: Vulkan pooled backend failed, falling back to CPU: %v", err)
		}
		return nil, nil, false
	}
	return q.finishVector(hidL), q.finishVector(hidM), true
}

// EmbedPooledAs is EmbedAs for PooledEmbedder (error when unsupported).
func EmbedPooledAs(emb Embedder, text string, role Role) (last, mean []float32, err error) {
	pe, ok := emb.(PooledEmbedder)
	if !ok || emb == nil || IsNoop(emb) {
		return nil, nil, fmt.Errorf("embedding: %T has no mean pooling", emb)
	}
	return pe.EmbedPooledWithRole(text, role)
}

// SupportsPooling reports whether emb can produce mean-pooled vectors.
func SupportsPooling(emb Embedder) bool {
	_, ok := emb.(PooledEmbedder)
	return ok && emb != nil && !IsNoop(emb)
}

// EmbedPooledWithRole embeds ApplyQwen3Prompt(text, role) once and returns
// the L2-normalised last-token and mean-pooled vectors (full hidden width).
func (q *Qwen3Embedder) EmbedPooledWithRole(text string, role Role) (last, mean []float32, err error) {
	defer func() {
		if r := recover(); r != nil {
			last, mean, err = nil, nil, fmt.Errorf("qwen3: pooled embed panic: %v", r)
		}
	}()
	if q == nil || q.tokenizer == nil {
		return nil, nil, ErrEmbedderClosed
	}
	if !q.inflight.acquire() {
		return nil, nil, ErrEmbedderClosed
	}
	defer q.inflight.release()
	ids := q.Tokenize(ApplyQwen3Prompt(text, role))
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("qwen3: empty token sequence")
	}
	// The GPU computes the whole sequence and pools all rows, which is the same
	// mean the CPU split (cached prefix sum + suffix) produces. Long sequences
	// stay on the CPU, like every other GPU call.
	//
	// Wall time here is worse than the CPU path (embdoctor on the reference
	// machine: 100 ms vs 67 ms) because the CPU caches the prefix sum and only
	// forwards the suffix while the GPU redoes the whole sequence. It still runs
	// on the GPU on purpose: the Layer 2 intent path fires on every message and
	// the CPU path occupies four cores for 67 ms each time. MACLAW_VULKAN=0 is
	// the escape hatch when latency matters more than CPU load.
	if g := q.vkGPU.Load(); g != nil && len(ids) <= qwen3VKMaxSeqTokens {
		if last, mean, ok := q.pooledGPU(g, ids); ok {
			return last, mean, nil
		}
	}
	// Only the CPU path below consults the prefix cache, so warm it here rather
	// than before the GPU attempt (which would be a wasted CPU forward).
	q.ensurePrefix(role)
	pre := q.matchPrefix(ids)
	if pre != nil && pre.rowSum == nil {
		pre = nil
	}
	s := q.getScratch(len(ids) + 8)
	defer q.putScratch(s)
	dim := q.hp.Dim
	sum := make([]float32, dim)
	h, err := q.forwardPool(ids, pre, nil, s, EmbedThreads(), sum)
	if err != nil {
		return nil, nil, err
	}
	if pre != nil {
		for i, v := range pre.rowSum {
			sum[i] += v
		}
	}
	inv := 1 / float32(len(ids))
	for i := range sum {
		sum[i] *= inv
	}
	return q.finishVector(h), q.finishVector(sum), nil
}
