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

// metalSelfCheck runs the Metal backend over several probe texts, packed into
// one forward, and requires every vector to match the CPU path. It returns the
// first failure with enough context to diagnose it.
func (q *Qwen3Embedder) metalSelfCheck(g *qwen3GPU) error {
	texts := []string{
		"Metal self-check: send this file to zhangsan",
		"查询 api2 服务器状态",
	}
	seqs := make([][]int, len(texts))
	for i, t := range texts {
		seqs[i] = q.Tokenize(t)
		if len(seqs[i]) == 0 {
			return fmt.Errorf("empty probe text %d", i)
		}
	}
	got := make([][]float32, len(seqs))
	for i := range got {
		got[i] = make([]float32, q.hp.Dim)
	}
	if err := g.forward(q, seqs, got); err != nil {
		return err
	}
	minCos, worst := 1.0, 0
	for i, ids := range seqs {
		if !allFinite(got[i]) {
			return fmt.Errorf("non-finite GPU output for probe text %d", i)
		}
		s := q.getScratch(len(ids) + 8)
		ref, err := q.forwardFrom(ids, nil, nil, s, EmbedThreads())
		q.putScratch(s)
		if err != nil {
			return err
		}
		// forwardFrom already applies the output norm, so normalise only the GPU
		// side; results are independent of the CPU worker count.
		gn := make([]float32, q.hp.Dim)
		tensor.RMSNorm(gn, got[i], q.outputNorm, q.hp.RMSNormEps)
		c := qwen3MetalCos(gn, ref[:q.hp.Dim])
		if math.IsNaN(c) || c < minCos {
			minCos, worst = c, i
		}
	}
	if os.Getenv("MACLAW_QWEN3_METAL_SKIP") != "" {
		return nil // timing experiments only; output is not trustworthy
	}
	if !(minCos >= qwen3MetalSelfCheckMinCos) {
		return fmt.Errorf("worst cos %.6f < %.4f (probe text %d)", minCos, qwen3MetalSelfCheckMinCos, worst)
	}
	return nil
}

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
		q.setGPUReason("disabled by MACLAW_QWEN3_METAL=0")
		return
	}
	if !HWAccelPreferred() {
		q.setGPUReason("hardware acceleration off")
		return
	}
	g, err := newQwen3GPU(q)
	if err != nil {
		q.setGPUReason(err.Error())
		return
	}
	// Correctness gate: the candidate backend must reproduce the CPU vectors
	// before it is allowed to serve requests. Every probe text is compared (not
	// just one) because the GPU packs its sequences, so a broken segment or mask
	// can corrupt some inputs while leaving others correct; the run also
	// exercises several sequence lengths.
	if err := q.metalSelfCheck(g); err != nil {
		g.release()
		q.setGPUReason("self-check: " + err.Error())
		log.Printf("qwen3: Metal disabled — %s", q.GPUReason())
		return
	}
	q.setGPUReason("")
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

// GPUBackend reports whether a GPU backend (Metal or Vulkan) is active and, if
// not, why.
func (q *Qwen3Embedder) GPUBackend() (active bool, reason string) {
	if q.vkGPU.Load() != nil {
		return true, ""
	}
	return q.gpu.Load() != nil, q.GPUReason()
}

// GPUMemoryBytes reports the GPU buffer footprint (weights, activations) for
// whichever backend is active.
func (q *Qwen3Embedder) GPUMemoryBytes() (weights, acts int) {
	if g := q.vkGPU.Load(); g != nil {
		return g.WeightBytes, g.ActBytes()
	}
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
		// Long sequences are slower on the GPU than on the CPU (see
		// qwen3VKMaxSeqTokens), so hand them back to the caller's CPU path.
		for _, s := range seqs {
			if len(s) > qwen3VKMaxSeqTokens {
				return nil, false
			}
		}
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
		vecs[i] = q.finishVector(n)
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
	// Keep the packed activation footprint bounded (Vulkan reserves buffers for
	// the peak row count; Metal shares system memory with the mapped weights).
	maxRows := qwen3MetalBatchRows
	if q.vkGPU.Load() != nil && maxRows > qwen3VKBatchRows() {
		maxRows = qwen3VKBatchRows()
	}
	res = make([][]float32, len(texts))
	// Long texts go to the CPU individually instead of failing the whole batch:
	// one long document in an import should not push every other text off the
	// GPU as well (see qwen3VKMaxSeqTokens for why long texts are CPU work).
	vkActive := q.vkGPU.Load() != nil
	pending := make([]int, 0, len(ids))
	for i := range ids {
		if vkActive && len(ids[i]) > qwen3VKMaxSeqTokens {
			v, err := q.embed(texts[i], 1)
			if err != nil {
				return nil, false
			}
			res[i] = v
			continue
		}
		pending = append(pending, i)
	}
	for p := 0; p < len(pending); {
		first := p
		rows := 0
		for p < len(pending) && (p == first || rows+len(ids[pending[p]]) <= maxRows) {
			rows += len(ids[pending[p]])
			p++
		}
		batch := pending[first:p]
		seqs := make([][]int, len(batch))
		for k, idx := range batch {
			seqs[k] = ids[idx]
		}
		v, ok := q.embedGPU(seqs)
		if !ok {
			return nil, false
		}
		for k, idx := range batch {
			res[idx] = v[k]
		}
	}
	return res, true
}
