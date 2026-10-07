package embedding

// qwen3_q8p_fallback.go — Q8P (int8-activation) packing with a float
// fallback. The ISA gate (tensor.Q8PAvailable: AVX-512 VNNI / AVX-VNNI /
// AVX2 / arm64 SDOT, via x/sys/cpu which also checks OS register-state
// support) already routes CPUs without a kernel to the float Q8 path. This
// file hardens the remaining cases: a pack that fails (shape, arena) or
// panics, and a kernel that is present but numerically wrong on this host
// (self-check against the float Q8 matmul on two real weight matrices). Any
// of these leaves the instance on the float path instead of failing the load.

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Test hooks.
var (
	qwen3PackQ8PFn      = tensor.PackQ8PInto
	qwen3Q8PSelfCheckFn = qwen3Q8PSelfCheck
)

// qwen3Q8PSelfCheckMaxRelErr bounds ‖q8p−float‖/‖float‖ for the self-check.
// Single-pass int8 activations measure ≈0.5–1.5% on real Qwen3 weights; a
// broken kernel is off by O(1).
const qwen3Q8PSelfCheckMaxRelErr = 0.05

// Q8PFallbackReason is why this instance runs the float path although the CPU
// has a Q8P kernel ("" when Q8P is in use or was never attempted).
func (q *Qwen3Embedder) Q8PFallbackReason() string {
	if q == nil {
		return ""
	}
	return q.q8pFallback
}

// packQ8P packs every layer matrix into one arena, self-checks, and only then
// drops the float scales / mmap pages. On error nothing the float path needs
// has been touched and the arena is freed.
func (q *Qwen3Embedder) packQ8P(mf *gguf.MmapFile) (err error) {
	hp := &q.hp
	need := 0
	for i := range q.layers {
		l := &q.layers[i]
		for _, t := range []*tensor.Q8Tensor{&l.wq, &l.wk, &l.wv, &l.wo, &l.wGate, &l.wUp, &l.wDown} {
			need += tensor.Q8PPackedBytes(t.Rows, t.Cols)
		}
	}
	arena := tensor.NewQ8PArena(need)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("q8p pack panic: %v", r)
		}
		if err != nil {
			for i := range q.layers {
				l := &q.layers[i]
				l.pq, l.pk, l.pv, l.po, l.pg, l.pu, l.pd = nil, nil, nil, nil, nil, nil, nil
			}
			arena.Free()
		}
	}()
	for i := range q.layers {
		l := &q.layers[i]
		for _, pr := range []struct {
			dst **tensor.Q8Packed
			src *tensor.Q8Tensor
		}{{&l.pq, &l.wq}, {&l.pk, &l.wk}, {&l.pv, &l.wv}, {&l.po, &l.wo}, {&l.pg, &l.wGate}, {&l.pu, &l.wUp}, {&l.pd, &l.wDown}} {
			*pr.dst = qwen3PackQ8PFn(pr.src, arena)
			if *pr.dst == nil {
				return fmt.Errorf("q8p pack failed for layer %d (%dx%d)", i, pr.src.Rows, pr.src.Cols)
			}
		}
	}
	if len(q.layers) > 0 {
		l := &q.layers[0]
		if err := qwen3Q8PSelfCheckFn(&l.wq, l.pq); err != nil {
			return fmt.Errorf("q8p self-check attn_q: %w", err)
		}
		if err := qwen3Q8PSelfCheckFn(&l.wDown, l.pd); err != nil {
			return fmt.Errorf("q8p self-check ffn_down: %w", err)
		}
	}
	_ = hp
	// Committed: the float path is never used by this instance. Drop the
	// per-block float scales and release the mmap'd source pages (the packed
	// copy holds the same int8 values).
	for i := range q.layers {
		l := &q.layers[i]
		for _, t := range []*tensor.Q8Tensor{&l.wq, &l.wk, &l.wv, &l.wo, &l.wGate, &l.wUp, &l.wDown} {
			t.Scales = nil
			mf.Release(t.Data)
		}
	}
	q.q8pArena = arena
	q.q8p = true
	return nil
}

// qwen3Q8PSelfCheck compares the Q8P kernel with the float Q8 matmul on a
// deterministic 4-row activation.
func qwen3Q8PSelfCheck(w *tensor.Q8Tensor, p *tensor.Q8Packed) error {
	const M = 4
	N, K := w.Rows, w.Cols
	rng := rand.New(rand.NewSource(20261007))
	a := make([]float32, M*K)
	for i := range a {
		a[i] = float32(rng.NormFloat64())
	}
	ref := make([]float32, M*N)
	tensor.MatMulQ8N(ref, a, w, M, N, K, 1)
	act := tensor.QuantizeQ8Act(nil, a, M, K)
	got := make([]float32, act.Mpad*N)
	tensor.MatMulQ8P(got, act, p, false, 1)
	var num, den float64
	for i := 0; i < M*N; i++ {
		g, r := float64(got[i]), float64(ref[i])
		if math.IsNaN(g) || math.IsInf(g, 0) {
			return fmt.Errorf("non-finite output at %d", i)
		}
		num += (g - r) * (g - r)
		den += r * r
	}
	if den == 0 {
		return fmt.Errorf("zero reference output")
	}
	if rel := math.Sqrt(num / den); rel > qwen3Q8PSelfCheckMaxRelErr {
		return fmt.Errorf("relative error %.4f > %.2f", rel, qwen3Q8PSelfCheckMaxRelErr)
	}
	return nil
}
