// corelib/embedding/qwen3.go — Pure Go Qwen3-Embedding model (GGUF, Q8_0).
//
// Architecture (Qwen3-Embedding-0.6B: 28 layers, hidden 1024, GQA 16q/8kv,
// head_dim 128, FFN 3072, RoPE θ=1e6):
//
//	x = token_embd[ids]                       (no √dim scaling, unlike Gemma)
//	per layer:
//	  h = RMSNorm(x)·attn_norm
//	  q,k,v = h·Wq, h·Wk, h·Wv                 (q is 16×128 = 2048 wide ≠ hidden)
//	  q,k  = RoPE_neox(RMSNorm_head(q|k)·q_norm|k_norm)
//	  x   += CausalGQA(q,k,v)·Wo               (decoder: causal mask)
//	  x   += (SiLU(RMSNorm(x)·ffn_norm·Wg) ⊙ (…·Wu))·Wd
//	out = RMSNorm(x[last])·output_norm → MRL truncate → L2 normalise
//
// Pooling is last-token: the tokenizer appends <|endoftext|> and the hidden
// state of that token is the embedding. Queries take the instruct prefix
// "Instruct: {task}\nQuery: " (see qwen3_prompt.go); documents take none.
//
// Only kernels that already exist for EmbeddingGemma are used (Q8_0 MatMul,
// RMSNorm, fused per-head RMSNorm+RoPE, SiLU⊙, SIMD dot / softmax-weighted
// sum); the Gemma-shape fused kernels (dim 768 / ffn 1152) are not applicable.
package embedding

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// Qwen3ModelFilename is the conventional local filename of the official
// Qwen/Qwen3-Embedding-0.6B-GGUF Q8_0 build.
const Qwen3ModelFilename = "Qwen3-Embedding-0.6B-Q8_0.gguf"

// qwen3MaxTokens caps the sequence (incl. EOS); the model supports 32k but
// attention scratch grows quadratically and intent/knowledge texts are short.
const qwen3MaxTokens = 8192

// Qwen3HParams holds Qwen3 hyperparameters read from GGUF.
type Qwen3HParams struct {
	Dim        int // embedding_length (1024)
	NLayers    int
	NHeads     int
	NKVHeads   int
	HeadDim    int // attention.key_length (128)
	QDim       int // NHeads*HeadDim (2048)
	KVDim      int // NKVHeads*HeadDim (1024)
	FFDim      int
	VocabSize  int
	MaxSeqLen  int
	RMSNormEps float32
	RopeTheta  float32
}

type qwen3Layer struct {
	attnNormW, qNormW, kNormW, ffNormW []float32
	wq, wk, wv, wo                     tensor.Q8Tensor
	wGate, wUp, wDown                  tensor.Q8Tensor
}

// Qwen3Embedder is a pure Go Qwen3 text embedding model.
type Qwen3Embedder struct {
	hp         Qwen3HParams
	layers     []qwen3Layer
	tokenEmb   tensor.Q8Tensor
	outputNorm []float32
	tokenizer  *BPETokenizer
	dim        int
	modelName  string
	mu         sync.Mutex
	mmap       *gguf.MmapFile
	pools      [5]sync.Pool // scratch by bucket (16/64/256/512/other-unpooled)
	ropeMu     sync.Mutex
	ropeCos    []float32 // [ropeLen*half], grown on demand, read-only once published
	ropeSin    []float32
	ropeLen    int
}

type qwen3Scratch struct {
	seqCap                         int
	x, normed, q, k, v, attn, proj []float32
	gate, up                       []float32
	scores                         []float32
}

// NewQwen3Embedder loads a Qwen3 embedding GGUF. dim<=0 or dim>hidden keeps
// the full width; smaller values apply MRL truncation.
func NewQwen3Embedder(modelPath string, dim int) (*Qwen3Embedder, error) {
	waitMmapCloses()
	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		return nil, fmt.Errorf("qwen3: open mmap: %w", err)
	}
	// The header parse leaves the int32 token_type array in the reader's
	// "last array" slot; take it before anything else parses a file.
	tokenTypes := gguf.LastI32Array()
	_ = gguf.LastF32Array()
	arch := gguf.GetMetaStr(mf.Meta, "general.architecture")
	if arch != "qwen3" {
		mf.CloseMmap()
		return nil, fmt.Errorf("qwen3: unsupported architecture %q", arch)
	}
	p := arch + "."
	modelName := strings.TrimSpace(gguf.GetMetaStr(mf.Meta, "general.name"))
	if modelName == "" {
		modelName = "qwen3-embedding"
	}
	hp := Qwen3HParams{
		Dim:        gguf.GetMetaI32(mf.Meta, p+"embedding_length", 1024),
		NLayers:    gguf.GetMetaI32(mf.Meta, p+"block_count", 28),
		NHeads:     gguf.GetMetaI32(mf.Meta, p+"attention.head_count", 16),
		NKVHeads:   gguf.GetMetaI32(mf.Meta, p+"attention.head_count_kv", 8),
		FFDim:      gguf.GetMetaI32(mf.Meta, p+"feed_forward_length", 3072),
		MaxSeqLen:  gguf.GetMetaI32(mf.Meta, p+"context_length", 32768),
		RMSNormEps: gguf.GetMetaF32(mf.Meta, p+"attention.layer_norm_rms_epsilon", 1e-6),
		RopeTheta:  gguf.GetMetaF32(mf.Meta, p+"rope.freq_base", 1e6),
	}
	hp.HeadDim = gguf.GetMetaI32(mf.Meta, p+"attention.key_length", hp.Dim/hp.NHeads)
	hp.QDim = hp.NHeads * hp.HeadDim
	hp.KVDim = hp.NKVHeads * hp.HeadDim
	if hp.MaxSeqLen <= 0 || hp.MaxSeqLen > qwen3MaxTokens {
		hp.MaxSeqLen = qwen3MaxTokens
	}
	if hp.NKVHeads <= 0 || hp.NHeads%hp.NKVHeads != 0 || hp.HeadDim%2 != 0 {
		mf.CloseMmap()
		return nil, fmt.Errorf("qwen3: bad head config %d/%d/%d", hp.NHeads, hp.NKVHeads, hp.HeadDim)
	}
	if pt := gguf.GetMetaInt(mf.Meta, p+"pooling_type", 3); pt != 3 {
		mf.CloseMmap()
		return nil, fmt.Errorf("qwen3: pooling_type %d not supported (want 3 = last token)", pt)
	}
	if dim <= 0 || dim > hp.Dim {
		dim = hp.Dim
	}

	q := &Qwen3Embedder{hp: hp, dim: dim, modelName: modelName, mmap: mf}
	if err := q.loadWeights(mf); err != nil {
		mf.CloseMmap()
		return nil, err
	}

	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	merges := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.merges")
	if len(tokens) == 0 || len(merges) == 0 {
		mf.CloseMmap()
		return nil, fmt.Errorf("qwen3: GGUF lacks tokenizer.ggml.tokens/merges")
	}
	if m := gguf.GetMetaStr(mf.Meta, "tokenizer.ggml.model"); m != "gpt2" {
		mf.CloseMmap()
		return nil, fmt.Errorf("qwen3: tokenizer model %q not supported (want gpt2 byte-level BPE)", m)
	}
	eos := gguf.GetMetaInt(mf.Meta, "tokenizer.ggml.eos_token_id", 151643)
	bos := gguf.GetMetaInt(mf.Meta, "tokenizer.ggml.bos_token_id", eos)
	addEOS := gguf.GetMetaBool(mf.Meta, "tokenizer.ggml.add_eos_token", true)
	addBOS := gguf.GetMetaBool(mf.Meta, "tokenizer.ggml.add_bos_token", false)
	q.tokenizer = NewBPETokenizer(tokens, merges, tokenTypes, bos, eos, addBOS, addEOS)
	q.ensureRoPE(64)
	return q, nil
}

func (q *Qwen3Embedder) loadWeights(mf *gguf.MmapFile) error {
	hp := &q.hp
	raw, ti, err := mf.TensorRawBytes("token_embd.weight")
	if err != nil {
		return fmt.Errorf("qwen3: %w", err)
	}
	if ti.Type != gguf.TypeQ8_0 {
		return fmt.Errorf("qwen3: token_embd.weight is type %d, need Q8_0", ti.Type)
	}
	hp.VocabSize = int(ti.NumElements()) / hp.Dim
	q.tokenEmb = tensor.Q8Tensor{Data: raw, Rows: hp.VocabSize, Cols: hp.Dim}
	q.tokenEmb.PrepareScales()
	if q.outputNorm, err = mf.TensorF32("output_norm.weight"); err != nil {
		return fmt.Errorf("qwen3: %w", err)
	}
	q.layers = make([]qwen3Layer, hp.NLayers)
	for i := range q.layers {
		l := &q.layers[i]
		pf := fmt.Sprintf("blk.%d.", i)
		for _, nw := range []struct {
			dst  *[]float32
			name string
			n    int
		}{
			{&l.attnNormW, "attn_norm.weight", hp.Dim},
			{&l.qNormW, "attn_q_norm.weight", hp.HeadDim},
			{&l.kNormW, "attn_k_norm.weight", hp.HeadDim},
			{&l.ffNormW, "ffn_norm.weight", hp.Dim},
		} {
			v, err := mf.TensorF32(pf + nw.name)
			if err != nil {
				return fmt.Errorf("qwen3: %w", err)
			}
			if len(v) != nw.n {
				return fmt.Errorf("qwen3: %s%s has %d elems, want %d", pf, nw.name, len(v), nw.n)
			}
			*nw.dst = v
		}
		for _, mw := range []struct {
			dst        *tensor.Q8Tensor
			name       string
			rows, cols int
		}{
			{&l.wq, "attn_q.weight", hp.QDim, hp.Dim},
			{&l.wk, "attn_k.weight", hp.KVDim, hp.Dim},
			{&l.wv, "attn_v.weight", hp.KVDim, hp.Dim},
			{&l.wo, "attn_output.weight", hp.Dim, hp.QDim},
			{&l.wGate, "ffn_gate.weight", hp.FFDim, hp.Dim},
			{&l.wUp, "ffn_up.weight", hp.FFDim, hp.Dim},
			{&l.wDown, "ffn_down.weight", hp.Dim, hp.FFDim},
		} {
			t, err := readQ8Tensor(mf, pf+mw.name, mw.rows, mw.cols)
			if err != nil {
				return fmt.Errorf("qwen3: %w", err)
			}
			if len(t.Data) != mw.rows*mw.cols/32*34 {
				return fmt.Errorf("qwen3: %s%s size mismatch", pf, mw.name)
			}
			t.PrepareScales()
			*mw.dst = t
		}
	}
	for i := range q.layers {
		l := &q.layers[i]
		for _, t := range []*tensor.Q8Tensor{&l.wq, &l.wk, &l.wv, &l.wo, &l.wGate, &l.wUp, &l.wDown} {
			t.FaultIn()
		}
	}
	return nil
}

// ensureRoPE makes the shared cos/sin tables cover at least n positions.
// Tables only grow; a published slice is never written again, so readers
// holding an older (shorter) slice stay valid.
func (q *Qwen3Embedder) ensureRoPE(n int) ([]float32, []float32) {
	q.ropeMu.Lock()
	defer q.ropeMu.Unlock()
	if n <= q.ropeLen {
		return q.ropeCos, q.ropeSin
	}
	if n < 2*q.ropeLen {
		n = 2 * q.ropeLen
	}
	half := q.hp.HeadDim / 2
	cos := make([]float32, n*half)
	sin := make([]float32, n*half)
	for i := 0; i < half; i++ {
		// HF Qwen3: inv_freq = θ^(-2i/d), computed in float32 then angle = pos*inv_freq
		freq := 1.0 / math.Pow(float64(q.hp.RopeTheta), float64(2*i)/float64(q.hp.HeadDim))
		for pos := 0; pos < n; pos++ {
			a := float64(pos) * freq
			cos[pos*half+i] = float32(math.Cos(a))
			sin[pos*half+i] = float32(math.Sin(a))
		}
	}
	q.ropeCos, q.ropeSin, q.ropeLen = cos, sin, n
	return cos, sin
}

func qwen3Bucket(seq int) (int, int) {
	switch {
	case seq <= 16:
		return 16, 0
	case seq <= 64:
		return 64, 1
	case seq <= 256:
		return 256, 2
	case seq <= 512:
		return 512, 3
	default:
		return seq, -1
	}
}

func (q *Qwen3Embedder) getScratch(seq int) *qwen3Scratch {
	S, idx := qwen3Bucket(seq)
	if idx >= 0 {
		if v := q.pools[idx].Get(); v != nil {
			return v.(*qwen3Scratch)
		}
	}
	hp := q.hp
	s := &qwen3Scratch{seqCap: S}
	s.x = make([]float32, S*hp.Dim)
	s.normed = make([]float32, S*hp.Dim)
	s.q = make([]float32, S*hp.QDim)
	s.k = make([]float32, S*hp.KVDim)
	s.v = make([]float32, S*hp.KVDim)
	s.attn = make([]float32, S*hp.QDim)
	s.proj = make([]float32, S*hp.Dim)
	s.gate = make([]float32, S*hp.FFDim)
	s.up = make([]float32, S*hp.FFDim)
	s.scores = make([]float32, S)
	return s
}

func (q *Qwen3Embedder) putScratch(s *qwen3Scratch) {
	if s == nil {
		return
	}
	if _, idx := qwen3Bucket(s.seqCap); idx >= 0 {
		q.pools[idx].Put(s)
	}
}

// Tokenize returns the token ids Embed would feed the model (incl. EOS).
func (q *Qwen3Embedder) Tokenize(text string) []int {
	ids := q.tokenizer.Encode(text)
	if len(ids) > q.hp.MaxSeqLen {
		last := ids[len(ids)-1]
		ids = ids[:q.hp.MaxSeqLen]
		if q.tokenizer.addEOS {
			ids[len(ids)-1] = last // keep the pooled EOS token
		}
	}
	return ids
}

// Embed returns the L2-normalised embedding of text (no prompt applied).
// Matrix multiplies parallelise internally across cores.
func (q *Qwen3Embedder) Embed(text string) ([]float32, error) {
	return q.embed(text, 0)
}

// EmbedConcurrent is Embed with single-threaded kernels, for callers that
// parallelise across texts (EmbedBatch).
func (q *Qwen3Embedder) EmbedConcurrent(text string) ([]float32, error) {
	return q.embed(text, 1)
}

func (q *Qwen3Embedder) embed(text string, maxWorkers int) ([]float32, error) {
	ids := q.Tokenize(text)
	if len(ids) == 0 {
		return nil, fmt.Errorf("qwen3: empty token sequence")
	}
	s := q.getScratch(len(ids))
	defer q.putScratch(s)
	h, err := q.forward(ids, s, maxWorkers)
	if err != nil {
		return nil, err
	}
	out := make([]float32, q.dim)
	copy(out, h[:q.dim])
	tensor.L2Normalize(out)
	return out, nil
}

// EmbedBatch embeds texts in parallel (up to min(NumCPU, 8) goroutines,
// single-threaded kernels each).
func (q *Qwen3Embedder) EmbedBatch(texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	if len(texts) <= 1 {
		for i, t := range texts {
			v, err := q.Embed(t)
			if err != nil {
				return nil, fmt.Errorf("qwen3: batch item %d: %w", i, err)
			}
			results[i] = v
		}
		return results, nil
	}
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	if workers > len(texts) {
		workers = len(texts)
	}
	errs := make([]error, len(texts))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				results[i], errs[i] = q.EmbedConcurrent(texts[i])
			}
		}()
	}
	for i := range texts {
		next <- i
	}
	close(next)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("qwen3: batch item %d: %w", i, err)
		}
	}
	return results, nil
}

// Dim returns the output embedding dimension.
func (q *Qwen3Embedder) Dim() int { return q.dim }

// HParams returns the model hyperparameters.
func (q *Qwen3Embedder) HParams() Qwen3HParams { return q.hp }

// Close releases the mmap (after in-flight calls on the shared mutex finish).
func (q *Qwen3Embedder) Close() {
	q.mu.Lock()
	mf := q.mmap
	q.mmap = nil
	q.mu.Unlock()
	enqueueMmapClose(mf)
}

// forward runs the transformer and returns the final-normed hidden state of
// the last token (aliases scratch; caller copies).
func (q *Qwen3Embedder) forward(ids []int, s *qwen3Scratch, maxWorkers int) ([]float32, error) {
	hp := q.hp
	seq := len(ids)
	dim, qDim, kvDim, ffDim := hp.Dim, hp.QDim, hp.KVDim, hp.FFDim
	x := s.x[:seq*dim]
	for i, id := range ids {
		if id < 0 || id >= hp.VocabSize {
			return nil, fmt.Errorf("qwen3: token id %d out of range [0,%d)", id, hp.VocabSize)
		}
		q.tokenEmb.DequantRow(id, x[i*dim:(i+1)*dim])
	}
	cosT, sinT := q.ensureRoPE(seq)
	normed := s.normed[:seq*dim]
	qb := s.q[:seq*qDim]
	kb := s.k[:seq*kvDim]
	vb := s.v[:seq*kvDim]
	attn := s.attn[:seq*qDim]
	proj := s.proj[:seq*dim]
	gate := s.gate[:seq*ffDim]
	up := s.up[:seq*ffDim]
	eps := hp.RMSNormEps
	for li := range q.layers {
		l := &q.layers[li]
		tensor.RMSNormRows(normed, x, l.attnNormW, seq, dim, eps)
		tensor.MatMulQ8N(qb, normed, &l.wq, seq, qDim, dim, maxWorkers)
		tensor.MatMulQ8N(kb, normed, &l.wk, seq, kvDim, dim, maxWorkers)
		tensor.MatMulQ8N(vb, normed, &l.wv, seq, kvDim, dim, maxWorkers)
		tensor.RMSNormRoPESeq(qb, l.qNormW, cosT, sinT, seq, hp.NHeads, hp.HeadDim, eps)
		tensor.RMSNormRoPESeq(kb, l.kNormW, cosT, sinT, seq, hp.NKVHeads, hp.HeadDim, eps)
		qwen3CausalAttention(attn, qb, kb, vb, s.scores, seq, hp.NHeads, hp.NKVHeads, hp.HeadDim, maxWorkers)
		tensor.MatMulQ8N(proj, attn, &l.wo, seq, dim, qDim, maxWorkers)
		tensor.Add(x, x, proj)
		tensor.RMSNormRows(normed, x, l.ffNormW, seq, dim, eps)
		tensor.MatMulQ8N(gate, normed, &l.wGate, seq, ffDim, dim, maxWorkers)
		tensor.MatMulQ8N(up, normed, &l.wUp, seq, ffDim, dim, maxWorkers)
		tensor.SiLUMul(gate, up)
		tensor.MatMulQ8N(proj, gate, &l.wDown, seq, dim, ffDim, maxWorkers)
		tensor.Add(x, x, proj)
	}
	last := x[(seq-1)*dim : seq*dim]
	out := normed[:dim]
	tensor.RMSNorm(out, last, q.outputNorm, eps)
	return out, nil
}

// qwen3CausalAttention: out[t, h] = softmax_{s<=t}(q[t,h]·k[s,kvh]/√d) · v[s,kvh].
// scores is scratch of len >= seq (used by the serial path).
func qwen3CausalAttention(out, qb, kb, vb, scores []float32, seq, nHeads, nKV, headDim, maxWorkers int) {
	qStride := nHeads * headDim
	kvStride := nKV * headDim
	group := nHeads / nKV
	scale := float32(1.0 / math.Sqrt(float64(headDim)))
	head := func(h int, sc []float32) {
		kOff := (h / group) * headDim
		hOff := h * headDim
		vBase := vb[kOff:]
		for t := 0; t < seq; t++ {
			qv := qb[t*qStride+hOff : t*qStride+hOff+headDim]
			n := t + 1
			for s := 0; s < n; s++ {
				sc[s] = tensor.Dot(qv, kb[s*kvStride+kOff:s*kvStride+kOff+headDim]) * scale
			}
			tensor.SoftmaxWeightedSumStrided(out[t*qStride+hOff:t*qStride+hOff+headDim], sc[:n], vBase, n, kvStride, headDim)
		}
	}
	if maxWorkers == 1 || seq < 32 {
		for h := 0; h < nHeads; h++ {
			head(h, scores)
		}
		return
	}
	var wg sync.WaitGroup
	for h := 0; h < nHeads; h++ {
		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			head(h, make([]float32, seq))
		}(h)
	}
	wg.Wait()
}
