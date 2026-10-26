package embedding

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Metal GPU backend selection (darwin only). MACLAW_QWEN3_METAL=0 disables it;
// any init or self-check failure leaves the CPU path in charge, and a runtime
// GPU error permanently falls back to the CPU for this embedder.

const qwen3MetalSelfCheckMinCos = 0.9999

// qwen3MetalBatchRows caps the rows packed into one GPU forward by EmbedBatch.
var qwen3MetalBatchRows = func() int {
	if v, err := strconv.Atoi(os.Getenv("MACLAW_QWEN3_METAL_BATCH_ROWS")); err == nil && v > 0 {
		return v
	}
	return 2048
}()

func (q *Qwen3Embedder) initGPU() {
	if runtime.GOOS != "darwin" {
		return
	}
	if os.Getenv("MACLAW_QWEN3_METAL") == "0" {
		q.gpuReason = "disabled by MACLAW_QWEN3_METAL=0"
		return
	}
	g, err := newQwen3GPU(q)
	if err != nil {
		q.gpuReason = err.Error()
		return
	}
	// Self-check against the CPU path on a fixed input.
	ids := q.Tokenize("Metal self-check: the quick brown fox jumps over the lazy dog 0123456789.")
	got := make([]float32, q.hp.Dim)
	if err := g.forward(q, [][]int{ids}, [][]float32{got}); err != nil {
		g.release()
		q.gpuReason = "self-check: " + err.Error()
		return
	}
	s := q.getScratch(len(ids) + 8)
	ref, err := q.forwardFrom(ids, nil, nil, s, 0)
	if err == nil && os.Getenv("MACLAW_QWEN3_METAL_SKIP") == "" {
		gn := make([]float32, q.hp.Dim)
		tensor.RMSNorm(gn, got, q.outputNorm, q.hp.RMSNormEps)
		if c := qwen3MetalCos(gn, ref[:q.hp.Dim]); !(c >= qwen3MetalSelfCheckMinCos) {
			err = fmt.Errorf("cos %.6f < %.4f", c, qwen3MetalSelfCheckMinCos)
		}
	}
	q.putScratch(s)
	if err != nil {
		g.release()
		q.gpuReason = "self-check: " + err.Error()
		return
	}
	q.gpu.Store(g)
	log.Printf("qwen3: Metal GPU backend active (%s, %d MB weights)", g.dev.Name, g.WeightBytes>>20)
}

func qwen3MetalCos(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / math.Sqrt(na*nb)
}

// GPUBackend reports whether the Metal backend is active and, if not, why.
func (q *Qwen3Embedder) GPUBackend() (active bool, reason string) {
	return q.gpu.Load() != nil, q.gpuReason
}

// GPUMemoryBytes reports the Metal buffer footprint (weights, activations).
func (q *Qwen3Embedder) GPUMemoryBytes() (weights, acts int) {
	if g := q.gpu.Load(); g != nil {
		return g.WeightBytes, g.ActBytes()
	}
	return 0, 0
}

// embedGPU embeds tokenised sequences on the GPU. ok=false means the GPU
// is not in use (or just failed and was disabled); the caller runs the CPU.
// Metal and Vulkan expose the same forward(q, seqs, outs) contract, so the
// backend choice is a single pointer check.
func (q *Qwen3Embedder) embedGPU(seqs [][]int) (vecs [][]float32, ok bool) {
	var hid [][]float32
	if vg := q.vkGPU.Load(); vg != nil {
		hid = make([][]float32, len(seqs))
		for i := range hid {
			hid[i] = make([]float32, q.hp.Dim)
		}
		if err := vg.forward(q, seqs, hid); err != nil {
			if q.vkGPU.CompareAndSwap(vg, nil) {
				log.Printf("qwen3: Vulkan backend failed, falling back to CPU: %v", err)
			}
			return nil, false
		}
		return q.poolVectors(hid), true
	}
	g := q.gpu.Load()
	if g == nil {
		return nil, false
	}
	hid = make([][]float32, len(seqs))
	for i := range hid {
		hid[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, seqs, hid); err != nil {
		if q.gpu.CompareAndSwap(g, nil) {
			log.Printf("qwen3: Metal backend failed, falling back to CPU: %v", err)
		}
		return nil, false
	}
	return q.poolVectors(hid), true
}

// poolVectors applies the output RMSNorm, MRL truncation, and L2 normalisation
// to the last hidden row of each sequence.
func (q *Qwen3Embedder) poolVectors(hid [][]float32) [][]float32 {
	vecs := make([][]float32, len(hid))
	for i, h := range hid {
		n := make([]float32, q.hp.Dim)
		tensor.RMSNorm(n, h, q.outputNorm, q.hp.RMSNormEps)
		out := make([]float32, q.dim)
		copy(out, n[:q.dim])
		tensor.L2Normalize(out)
		vecs[i] = out
	}
	return vecs
}

// embedBatchGPU packs texts into GPU forwards of up to qwen3MetalBatchRows rows.
func (q *Qwen3Embedder) embedBatchGPU(texts []string) (res [][]float32, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			res, ok = nil, false
		}
	}()
	if q == nil || q.tokenizer == nil || !q.inflight.acquire() {
		return nil, false
	}
	defer q.inflight.release()
	ids := make([][]int, len(texts))
	for i, t := range texts {
		ids[i] = q.Tokenize(t)
		if len(ids[i]) == 0 {
			return nil, false // let the CPU path report the error
		}
	}
	res = make([][]float32, len(texts))
	for i := 0; i < len(ids); {
		j, rows := i, 0
		for j < len(ids) && (j == i || rows+len(ids[j]) <= qwen3MetalBatchRows) {
			rows += len(ids[j])
			j++
		}
		v, ok := q.embedGPU(ids[i:j])
		if !ok {
			return nil, false
		}
		copy(res[i:j], v)
		i = j
	}
	return res, true
}
