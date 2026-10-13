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
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

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
	// wDown split along K (columns) into chunks the fast existing kernels
	// cover: the K=3072 FFN-down shape otherwise falls through to the generic
	// AVX2 fused loop, which profiled ~40× slower per MAC than the K=2048
	// AVX-512 N64 kernel and the K=1024 dequant-once path. Pure block copies
	// (no requantisation), summed after the matmuls.
	downParts []tensor.Q8Tensor
	downK0    []int
	// Q8P repacked weights (int8-activation GEMM, tensor/q8p.go); nil when
	// the kernel is unavailable (CPU ISA / MACLAW_Q8P_ISA / MACLAW_Q8P=0) or
	// MACLAW_QWEN3_Q8P=0, in which case the float-activation path above runs.
	// Activations: MACLAW_QWEN3_Q8P_2PASS unset → ISA-aware (avx512/avxvnni
	// two-pass; avx2/sdot single-pass). Use =none or =all to override.
	pq, pk, pv, po, pg, pu, pd *tensor.Q8Packed
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
	inflight   inflightGuard // Close waits for in-flight calls (inflight_guard.go)
	pools      [5]sync.Pool  // scratch by bucket (16/64/256/512/other-unpooled)
	ropeMu     sync.Mutex
	ropeCos    []float32 // [ropeLen*half], grown on demand, read-only once published
	ropeSin    []float32
	ropeLen    int

	// Instruct-prefix KV cache (qwen3_prefix_kv.go).
	prefixMu    sync.RWMutex
	prefixes    []*qwen3PrefixKV // immutable once published; bounded by qwen3MaxPrefixes
	prefixBuild sync.Mutex       // single-flight prefix computation

	q8p         bool             // layers carry Q8P packed weights (forwardFrom uses MatMulQ8P)
	q8pArena    *tensor.Q8PArena // off-heap storage of the packed weights (freed by Close)
	q8pFallback string           // why Q8P was wanted but not used; see Q8PFallbackReason
}

type qwen3Scratch struct {
	seqCap                         int
	x, normed, q, k, v, attn, proj []float32
	gate, up                       []float32
	scores                         []float32
	act, act2                      tensor.Q8Act // Q8P quantized activations (reused per matmul input)
	resid                          []float32    // Q8P two-pass residual scratch (lazily grown)
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
	if qwen3UseQ8P(hp) {
		if err := q.packQ8P(mf); err != nil {
			// Degrade, never fail the load: the float Q8 path needs nothing
			// the failed pack touched (scales/pages are only dropped after a
			// fully successful, self-checked pack).
			q.q8pFallback = err.Error()
			log.Printf("[qwen3] Q8P disabled, using float path: %v", err)
		} else {
			return nil
		}
	}
	for i := range q.layers {
		l := &q.layers[i]
		l.downParts, l.downK0 = splitQ8Cols(&l.wDown, qwen3DownChunks(hp.FFDim))
		ts := []*tensor.Q8Tensor{&l.wq, &l.wk, &l.wv, &l.wo, &l.wGate, &l.wUp}
		if len(l.downParts) == 0 {
			ts = append(ts, &l.wDown)
		}
		for _, t := range ts {
			t.FaultIn()
		}
	}
	return nil
}

// qwen3Q8PTwoPass selects the matmul classes whose activations use two int8
// terms (MACLAW_QWEN3_Q8P_2PASS). Default when unset is ISA-aware: two-pass
// on avx512/avxvnni (≈ float accuracy, faster than float); single-pass on
// avx2/arm64-sdot (where two-pass is slower than float). Opt into single-
// pass with "none"/"0"; force two-pass with "all"; or list qkv,o,gu,down.
var qwen3Q8PTwoPass = parseQwen3TwoPass(os.Getenv("MACLAW_QWEN3_Q8P_2PASS"))

type qwen3TwoPass struct{ qkv, o, gu, down bool }

func parseQwen3TwoPass(v string) qwen3TwoPass {
	v = strings.TrimSpace(v)
	if v == "" {
		// ISA-aware default (section 13 / 15): two-pass where it is faster
		// than float and matches float accuracy; single-pass on AVX2 / arm64
		// SDOT where two-pass is slower than float. Override with
		// MACLAW_QWEN3_Q8P_2PASS=all|none|qkv,o,gu,down.
		switch tensor.Q8PISAName() {
		case "avx2":
			return qwen3TwoPass{} // single-pass
		default: // avx512, avxvnni, sdot, i8mm (fused X2: forwardFrom skips the residual pass), off
			// sdot: single-pass int8 activations measured min cos 0.9992
			// vs float on Apple M4 (846 texts); two-pass is ≥0.999998.
			return qwen3TwoPass{true, true, true, true}
		}
	}

	if v == "none" || v == "0" {
		return qwen3TwoPass{}
	}
	var t qwen3TwoPass
	for _, f := range strings.Split(v, ",") {
		switch strings.TrimSpace(f) {
		case "qkv":
			t.qkv = true
		case "o":
			t.o = true
		case "gu":
			t.gu = true
		case "down":
			t.down = true
		case "all":
			t = qwen3TwoPass{true, true, true, true}
		case "none", "0":
			t = qwen3TwoPass{}
		}
	}
	return t
}

// qwen3UseQ8P reports whether the Q8P int8-activation kernel applies
// (amd64 AVX-512 VNNI, supported shapes, MACLAW_QWEN3_Q8P != "0").
func qwen3UseQ8P(hp *Qwen3HParams) bool {
	if !tensor.Q8PAvailable() || os.Getenv("MACLAW_QWEN3_Q8P") == "0" {
		return false
	}
	for _, nk := range [][2]int{{hp.QDim, hp.Dim}, {hp.KVDim, hp.Dim}, {hp.Dim, hp.QDim}, {hp.FFDim, hp.Dim}, {hp.Dim, hp.FFDim}} {
		if !tensor.Q8PSupported(nk[0], nk[1]) {
			return false
		}
	}
	return true
}

// qwen3DownChunks returns the K split for the FFN-down matmul: 2048-wide
// chunks (AVX-512 N64 kernel) plus a remainder, or nil when K needs no split.
func qwen3DownChunks(k int) []int {
	if k <= 2048 || k%32 != 0 {
		return nil
	}
	var c []int
	for k >= 2048 {
		c = append(c, 2048)
		k -= 2048
	}
	if k > 0 {
		c = append(c, k)
	}
	return c
}

// splitQ8Cols copies Q8_0 blocks of t into column chunks (heap-owned, scales
// prepared). Values are bit-identical to the source blocks.
func splitQ8Cols(t *tensor.Q8Tensor, widths []int) ([]tensor.Q8Tensor, []int) {
	if len(widths) < 2 {
		return nil, nil
	}
	const bb = 34
	srcBlocks := t.Cols / 32
	parts := make([]tensor.Q8Tensor, len(widths))
	k0s := make([]int, len(widths))
	k0 := 0
	for pi, w := range widths {
		nb := w / 32
		b0 := k0 / 32
		data := make([]byte, t.Rows*nb*bb)
		for r := 0; r < t.Rows; r++ {
			src := t.Data[(r*srcBlocks+b0)*bb : (r*srcBlocks+b0+nb)*bb]
			copy(data[r*nb*bb:(r+1)*nb*bb], src)
		}
		parts[pi] = tensor.Q8Tensor{Data: data, Rows: t.Rows, Cols: w}
		parts[pi].PrepareScales()
		k0s[pi] = k0
		k0 += w
	}
	return parts, k0s
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

func (q *Qwen3Embedder) embed(text string, maxWorkers int) (vec []float32, err error) {
	// Degrade, never crash: a kernel/tokenizer panic (bad file content that
	// passed the loader, an ISA quirk) becomes an error for this call. This
	// also covers EmbedBatch workers, where a panic would kill the process.
	defer func() {
		if r := recover(); r != nil {
			vec, err = nil, fmt.Errorf("qwen3: embed panic: %v", r)
		}
	}()
	if q == nil || q.tokenizer == nil {
		return nil, ErrEmbedderClosed
	}
	if !q.inflight.acquire() {
		return nil, ErrEmbedderClosed
	}
	defer q.inflight.release()
	ids := q.Tokenize(text)
	if len(ids) == 0 {
		return nil, fmt.Errorf("qwen3: empty token sequence")
	}
	pre := q.matchPrefix(ids)
	s := q.getScratch(len(ids) + 8) // +8: room for the 8-row matmul padding after a prefix
	defer q.putScratch(s)
	h, err := q.forwardFrom(ids, pre, nil, s, maxWorkers)
	if err != nil {
		return nil, err
	}
	out := make([]float32, q.dim)
	copy(out, h[:q.dim])
	tensor.L2Normalize(out)
	return out, nil
}

// EmbedBatch embeds texts in parallel (up to min(NumCPU, 8) goroutines,
// single-threaded kernels each; all cores on Apple Silicon, see qwen3BatchWorkers).
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
	workers := qwen3BatchWorkers()
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

// Close waits for in-flight calls, makes later calls fail with
// ErrEmbedderClosed, then releases the mmap.
func (q *Qwen3Embedder) Close() {
	q.inflight.close()
	q.mu.Lock()
	mf := q.mmap
	q.mmap = nil
	arena := q.q8pArena
	q.q8pArena = nil
	q.mu.Unlock()
	enqueueMmapClose(mf)
	// In-flight calls have drained (inflight.close waits) and later calls
	// fail before touching weights, so the packed weights can go now.
	arena.Free()
}

// forward runs the transformer over all of ids (no prefix cache) and returns
// the final-normed hidden state of the last token (aliases scratch).
func (q *Qwen3Embedder) forward(ids []int, s *qwen3Scratch, maxWorkers int) ([]float32, error) {
	return q.forwardFrom(ids, nil, nil, s, maxWorkers)
}

// forwardFrom runs the transformer over ids. With pre != nil, ids[:pre.n]
// must equal pre.ids: those positions are not recomputed — every layer's
// attention reads their cached (k_norm+RoPE'd) keys and values — and only the
// suffix rows ids[pre.n:] go through the network. Because attention is causal,
// prefix states never depend on later tokens, so this is the same function as
// the full forward (up to float reassociation in the matmul kernels).
// With capture != nil (and pre == nil), each layer's K/V rows are copied into
// capture. Returns the final-normed hidden state of the last token (aliases
// scratch; caller copies). s must hold len(ids)+8 rows.
func (q *Qwen3Embedder) forwardFrom(ids []int, pre, capture *qwen3PrefixKV, s *qwen3Scratch, maxWorkers int) ([]float32, error) {
	hp := q.hp
	P := 0
	if pre != nil {
		P = pre.n
	}
	total := len(ids)
	seq := total - P // rows computed here
	if seq <= 0 {
		return nil, fmt.Errorf("qwen3: empty suffix after cached prefix")
	}
	dim, qDim, kvDim, ffDim := hp.Dim, hp.QDim, hp.KVDim, hp.FFDim
	x := s.x[:seq*dim]
	for i, id := range ids[P:] {
		if id < 0 || id >= hp.VocabSize {
			return nil, fmt.Errorf("qwen3: token id %d out of range [0,%d)", id, hp.VocabSize)
		}
		q.tokenEmb.DequantRow(id, x[i*dim:(i+1)*dim])
	}
	cosAll, sinAll := q.ensureRoPE(total)
	half := hp.HeadDim / 2
	cosT, sinT := cosAll[P*half:], sinAll[P*half:] // suffix row r sits at position P+r
	normed := s.normed[:seq*dim]
	qb := s.q[:seq*qDim]
	// K/V hold all total positions: [0,P) from the cache, [P,total) computed.
	kAll := s.k[:total*kvDim]
	vAll := s.v[:total*kvDim]
	kb := s.k[P*kvDim : total*kvDim]
	vb := s.v[P*kvDim : total*kvDim]
	attn := s.attn[:seq*qDim]
	proj := s.proj[:seq*dim]
	gate := s.gate[:seq*ffDim]
	up := s.up[:seq*ffDim]
	eps := hp.RMSNormEps
	if q.q8p {
		act := &s.act
		// quant quantizes a[seq×K] into act and, when the matmul class is
		// configured for two-pass activations, its residual into act2.
		// mr is the number of rows still live: seq, except after the last
		// layer's attention, where only the final (pooled) row is needed.
		mr := seq
		quant := func(a []float32, K int, two bool) *tensor.Q8Act {
			seq := mr
			tensor.QuantizeQ8Act(act, a, seq, K)
			if !two {
				return nil
			}
			if cap(s.resid) < seq*K {
				s.resid = make([]float32, seq*K)
			}
			r := s.resid[:seq*K]
			tensor.Q8ActResidual(r, a, act)
			return tensor.QuantizeQ8Act(&s.act2, r, seq, K)
		}
		mm := func(out []float32, a2 *tensor.Q8Act, w *tensor.Q8Packed, accum bool) {
			tensor.MatMulQ8P(out, act, w, accum, maxWorkers)
			if a2 != nil {
				tensor.MatMulQ8P(out, a2, w, true, maxWorkers)
			}
		}
		tp := qwen3Q8PTwoPass
		if tensor.Q8PFusedTwoTerm() {
			tp = qwen3TwoPass{} // X2 activations are already two-term
		}
		// Row-parallel element-wise ops for long sequences (per-row results
		// are identical to the serial loop).
		rows := func(f func(r0, r1 int)) {
			if maxWorkers == 1 || mr < 64 {
				f(0, mr)
				return
			}
			tensor.ParallelRanges(mr, f)
		}
		half := hp.HeadDim / 2
		for li := range q.layers {
			l := &q.layers[li]
			rows(func(r0, r1 int) {
				tensor.RMSNormRows(normed[r0*dim:], x[r0*dim:], l.attnNormW, r1-r0, dim, eps)
			})
			a2 := quant(normed, dim, tp.qkv)
			if a2 == nil {
				tensor.MatMulQ8PMulti([][]float32{qb, kb, vb}, act, []*tensor.Q8Packed{l.pq, l.pk, l.pv}, maxWorkers)
			} else {
				mm(qb, a2, l.pq, false)
				mm(kb, a2, l.pk, false)
				mm(vb, a2, l.pv, false)
			}
			rows(func(r0, r1 int) {
				tensor.RMSNormRoPESeq(qb[r0*qDim:], l.qNormW, cosT[r0*half:], sinT[r0*half:], r1-r0, hp.NHeads, hp.HeadDim, eps)
				tensor.RMSNormRoPESeq(kb[r0*kvDim:], l.kNormW, cosT[r0*half:], sinT[r0*half:], r1-r0, hp.NKVHeads, hp.HeadDim, eps)
			})
			if pre != nil {
				copy(kAll[:P*kvDim], pre.k[li])
				copy(vAll[:P*kvDim], pre.v[li])
			}
			if capture != nil {
				capture.k[li] = append([]float32(nil), kb...)
				capture.v[li] = append([]float32(nil), vb...)
			}
			xl, attnL, normedL, gateL, upL := x, attn, normed, gate, up
			if li == len(q.layers)-1 && seq > 1 && qwen3PruneLast {
				// Last layer: K/V of every row are done; only the final row
				// feeds the pooled embedding, so attention, Wo and the FFN
				// run on that row alone (per-row results are unchanged).
				t := seq - 1
				qwen3CausalAttention(attn[:qDim], qb[t*qDim:], kAll, vAll, s.scores, 1, P+t, hp.NHeads, hp.NKVHeads, hp.HeadDim, maxWorkers)
				mr = 1
				xl = x[t*dim : seq*dim]
				attnL, normedL, gateL, upL = attn[:qDim], normed[:dim], gate[:ffDim], up[:ffDim]
			} else {
				qwen3CausalAttention(attn, qb, kAll, vAll, s.scores, seq, P, hp.NHeads, hp.NKVHeads, hp.HeadDim, maxWorkers)
			}
			x, attn, normed, gate, up := xl, attnL, normedL, gateL, upL
			a2 = quant(attn, qDim, tp.o)
			mm(x, a2, l.po, true) // x += attn·Wo
			rows(func(r0, r1 int) {
				tensor.RMSNormRows(normed[r0*dim:], x[r0*dim:], l.ffNormW, r1-r0, dim, eps)
			})
			a2 = quant(normed, dim, tp.gu)
			if a2 == nil {
				tensor.MatMulQ8PMulti([][]float32{gate, up}, act, []*tensor.Q8Packed{l.pg, l.pu}, maxWorkers)
			} else {
				mm(gate, a2, l.pg, false)
				mm(up, a2, l.pu, false)
			}
			rows(func(r0, r1 int) {
				tensor.SiLUMul(gate[r0*ffDim:r1*ffDim], up[r0*ffDim:r1*ffDim])
			})
			a2 = quant(gate, ffDim, tp.down)
			mm(x, a2, l.pd, true) // x += (silu(g)⊙u)·Wd
		}
		last := x[(seq-1)*dim : seq*dim]
		out := normed[:dim]
		tensor.RMSNorm(out, last, q.outputNorm, eps)
		return out, nil
	}
	// MatMulQ8N has a slow generic path for 4 <= M < 8 rows (the dequant-
	// once kernels need M >= 8); pad such sequences to 8 zero rows. Pad rows
	// of every matmul input stay exactly zero and are never read back.
	mr := qwen3MatMulRows(seq)
	if mr > seq {
		clear(s.normed[seq*dim : mr*dim])
		clear(s.attn[seq*qDim : mr*qDim])
	}
	normedM := s.normed[:mr*dim]
	qbM := s.q[:mr*qDim]
	kbM, vbM := s.k[P*kvDim:(P+mr)*kvDim], s.v[P*kvDim:(P+mr)*kvDim]
	attnM, projM := s.attn[:mr*qDim], s.proj[:mr*dim]
	gateM, upM := s.gate[:mr*ffDim], s.up[:mr*ffDim]
	for li := range q.layers {
		l := &q.layers[li]
		tensor.RMSNormRows(normed, x, l.attnNormW, seq, dim, eps)
		tensor.MatMulQ8N(qbM, normedM, &l.wq, mr, qDim, dim, maxWorkers)
		tensor.MatMulQ8N(kbM, normedM, &l.wk, mr, kvDim, dim, maxWorkers)
		tensor.MatMulQ8N(vbM, normedM, &l.wv, mr, kvDim, dim, maxWorkers)
		tensor.RMSNormRoPESeq(qb, l.qNormW, cosT, sinT, seq, hp.NHeads, hp.HeadDim, eps)
		tensor.RMSNormRoPESeq(kb, l.kNormW, cosT, sinT, seq, hp.NKVHeads, hp.HeadDim, eps)
		if pre != nil {
			copy(kAll[:P*kvDim], pre.k[li])
			copy(vAll[:P*kvDim], pre.v[li])
		}
		if capture != nil {
			capture.k[li] = append([]float32(nil), kb...)
			capture.v[li] = append([]float32(nil), vb...)
		}
		qwen3CausalAttention(attn, qb, kAll, vAll, s.scores, seq, P, hp.NHeads, hp.NKVHeads, hp.HeadDim, maxWorkers)
		tensor.MatMulQ8N(projM, attnM, &l.wo, mr, dim, qDim, maxWorkers)
		tensor.Add(x, x, proj)
		tensor.RMSNormRows(normed, x, l.ffNormW, seq, dim, eps)
		tensor.MatMulQ8N(gateM, normedM, &l.wGate, mr, ffDim, dim, maxWorkers)
		tensor.MatMulQ8N(upM, normedM, &l.wUp, mr, ffDim, dim, maxWorkers)
		tensor.SiLUMul(gate, up)
		if len(l.downParts) == 0 {
			tensor.MatMulQ8N(projM, gateM, &l.wDown, mr, dim, ffDim, maxWorkers)
			tensor.Add(x, x, proj)
			continue
		}
		// K-split down projection: copy each column chunk of gate into the
		// (now free) up buffer, matmul against the matching weight chunk, and
		// accumulate into the residual.
		off := 0
		for pi := range l.downParts {
			w := l.downParts[pi].Cols
			k0 := l.downK0[pi]
			a := s.up[off : off+mr*w]
			for r := 0; r < seq; r++ {
				copy(a[r*w:(r+1)*w], gate[r*ffDim+k0:r*ffDim+k0+w])
			}
			clear(a[seq*w:])
			off += mr * w
			tensor.MatMulQ8N(projM, a, &l.downParts[pi], mr, dim, w, maxWorkers)
			tensor.Add(x, x, proj)
		}
	}
	last := x[(seq-1)*dim : seq*dim]
	out := normed[:dim]
	tensor.RMSNorm(out, last, q.outputNorm, eps)
	return out, nil
}

// qwen3PadRows enables the M padding below (tests flip it to compare).
var qwen3PadRows = true

// qwen3MatMulRows returns the row count to feed MatMulQ8N for a sequence of
// seq tokens: 4..7 are padded to 8 so the fast dequant-once kernels apply.
func qwen3MatMulRows(seq int) int {
	if qwen3PadRows && seq >= 4 && seq < 8 {
		return 8
	}
	return seq
}

// qwen3CausalAttention: for the seq query rows at positions off..off+seq-1,
// out[t, h] = softmax_{s<=off+t}(q[t,h]·k[s,kvh]/√d) · v[s,kvh], where kb/vb
// hold all off+seq positions (off > 0 when a cached prefix precedes the rows).
// scores is scratch of len >= off+seq (used by the serial path).
func qwen3CausalAttention(out, qb, kb, vb, scores []float32, seq, off, nHeads, nKV, headDim, maxWorkers int) {
	qStride := nHeads * headDim
	kvStride := nKV * headDim
	group := nHeads / nKV
	scale := float32(1.0 / math.Sqrt(float64(headDim)))
	rowsT := func(h, t0, t1 int, sc []float32) {
		kOff := (h / group) * headDim
		hOff := h * headDim
		vBase := vb[kOff:]
		for t := t0; t < t1; t++ {
			qv := qb[t*qStride+hOff : t*qStride+hOff+headDim]
			n := off + t + 1
			if headDim == 128 {
				tensor.AttnDots128(sc[:n], qv, kb[kOff:], n, kvStride, scale)
			} else {
				for s := 0; s < n; s++ {
					sc[s] = tensor.Dot(qv, kb[s*kvStride+kOff:s*kvStride+kOff+headDim]) * scale
				}
			}
			tensor.SoftmaxWeightedSum128(out[t*qStride+hOff:t*qStride+hOff+headDim], sc[:n], vBase, n, kvStride, headDim)
		}
	}
	if headDim == 128 && qwen3AttnTile && tensor.AttnTile4Available() {
		// 4-query tiles (tensor/attn_tile.go); units = (head, 4-row block).
		nTB := (seq + 3) / 4
		units := nHeads * nTB
		unit := func(u int, sc []float32) {
			b := nTB - 1 - u/nHeads // largest (latest) blocks first
			h := u % nHeads
			t0, t1 := b*4, min(b*4+4, seq)
			var nk [4]int
			for t := t0; t < t1; t++ {
				nk[t-t0] = off + t + 1
			}
			kOff := (h / group) * headDim
			hOff := h * headDim
			tensor.AttnCausal4(out[t0*qStride+hOff:], qStride, qb[t0*qStride+hOff:], qStride,
				kb[kOff:], vb[kOff:], kvStride, nk[:t1-t0], scale, sc)
		}
		need := tensor.AttnScratch4(off + seq)
		if maxWorkers == 1 || seq < 32 {
			sc := make([]float32, need)
			for u := 0; u < units; u++ {
				unit(u, sc)
			}
			return
		}
		var next atomic.Int64
		tensor.ParallelRangesN(min(tensor.PoolWorkers(), units), func() {
			sc := make([]float32, need)
			for {
				u := int(next.Add(1)) - 1
				if u >= units {
					return
				}
				unit(u, sc)
			}
		})
		return
	}
	if maxWorkers == 1 || seq < 32 {
		for h := 0; h < nHeads; h++ {
			rowsT(h, 0, seq, scores)
		}
		return
	}
	if qwen3AttnPool {
		// (head, 16-row block) units handed out dynamically on the matmul
		// pool: balances causal row cost and P/E core speed.
		const tb = 16
		nTB := (seq + tb - 1) / tb
		units := nHeads * nTB
		nw := min(tensor.PoolWorkers(), units)
		var next atomic.Int64
		tensor.ParallelRangesN(nw, func() {
			sc := make([]float32, off+seq)
			for {
				u := int(next.Add(1)) - 1
				if u >= units {
					return
				}
				// Largest (latest) row blocks first.
				b := nTB - 1 - u/nHeads
				h := u % nHeads
				rowsT(h, b*tb, min(b*tb+tb, seq), sc)
			}
		})
		return
	}
	var wg sync.WaitGroup
	for h := 0; h < nHeads; h++ {
		wg.Add(1)
		go func(h int) {
			defer wg.Done()
			rowsT(h, 0, seq, make([]float32, off+seq))
		}(h)
	}
	wg.Wait()
}

// qwen3PruneLast: compute the last layer's attention output / FFN only for
// the pooled final row (exact). MACLAW_QWEN3_PRUNE_LAST=0 disables.
var qwen3PruneLast = os.Getenv("MACLAW_QWEN3_PRUNE_LAST") != "0"

// qwen3AttnTile: 4-query tiled attention where a kernel exists (arm64);
// MACLAW_QWEN3_ATTN_TILE=0 disables.
var qwen3AttnTile = os.Getenv("MACLAW_QWEN3_ATTN_TILE") != "0"

// qwen3AttnPool: dynamic pooled attention (default on arm64; set
// MACLAW_QWEN3_ATTN_POOL=1/0 to force).
var qwen3AttnPool = func() bool {
	switch os.Getenv("MACLAW_QWEN3_ATTN_POOL") {
	case "1":
		return true
	case "0":
		return false
	}
	return runtime.GOARCH == "arm64"
}()

// qwen3BatchWorkers: EmbedBatch parallelism (texts in flight, each with
// single-threaded kernels). MACLAW_QWEN3_BATCH_WORKERS overrides; default
// min(NumCPU, 8).
func qwen3BatchWorkers() int {
	if v, err := strconv.Atoi(os.Getenv("MACLAW_QWEN3_BATCH_WORKERS")); err == nil && v > 0 {
		return v
	}
	if runtime.GOARCH == "arm64" && runtime.GOOS == "darwin" {
		// Apple Silicon: E-cores add ~10% throughput on top of the P-cores
		// (M4 4P+6E: 8 workers 20.6 → 10 workers 22.8 items/s).
		return min(runtime.NumCPU(), 16)
	}
	return min(runtime.NumCPU(), 8)
}
