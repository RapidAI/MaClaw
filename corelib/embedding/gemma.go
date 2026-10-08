// corelib/embedding/gemma.go — Pure Go Gemma2 embedding model (GGUF).
//
// Architecture: Gemma2-style transformer with GQA, QK-norm, post-attn norm,
// post-FFN norm, GELU-gated FFN, RoPE.
// Output: mean-pooled hidden states → L2 normalized embedding.
// Supports MRL truncation (768 → 512/256/128).
//
// Memory optimization: weights are kept in Q8_0 format via mmap.
// Only small norm vectors are dequantized to float32.
// Large matrices (Q/K/V/O projections, FFN) stay quantized and are
// dequantized per-block during MatMul.
package embedding

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// GemmaHParams holds Gemma2 embedding model hyperparameters.
type GemmaHParams struct {
	Dim        int     // embedding_length (768)
	NLayers    int     // block_count (24)
	NHeads     int     // attention.head_count (3)
	NKVHeads   int     // attention.head_count_kv (1)
	HeadDim    int     // derived: Dim / NHeads (256)
	KVDim      int     // derived: HeadDim * NKVHeads (256)
	FFDim      int     // feed_forward_length (1152)
	VocabSize  int     // from token_embd tensor
	MaxSeqLen  int     // context_length (2048)
	RMSNormEps float32 // attention.layer_norm_rms_epsilon
	RopeTheta  float32 // rope.freq_base

	// Sliding-window attention, and the per-layer RoPE base that comes with it.
	//
	// llama.cpp's gemma-embedding loader (src/models/gemma-embedding.cpp:4-10):
	//     hparams.swa_type = LLAMA_SWA_TYPE_SYMMETRIC;
	//     load_swa_pattern(ml, 6);
	//     ml.get_key(LLM_KV_ROPE_FREQ_BASE_SWA, hparams.rope_freq_base_train_swa, false);
	//     ml.get_key(LLM_KV_ATTENTION_SLIDING_WINDOW, hparams.n_swa);
	// and llama-model.cpp:2358 then resolves the RoPE base per layer as
	//     is_swa(il) ? hparams.rope_freq_base_train_swa : cparams.rope_freq_base
	//
	// So the window and the RoPE base are both layer-dependent and both have to
	// be right together; getting either one wrong is silent. For this model the
	// window is 512 (a +-256 band, see HalfWindowFor) and 20 of the 24 layers
	// rotate at 1e4 while 5/11/17/23 rotate at 1e6.
	NSwa         int     // attention.sliding_window; 0 = none declared
	SwaPattern   int     // attention.sliding_window_pattern (scalar form); 0 = every layer
	RopeThetaSwa float32 // rope.freq_base_swa; absent from this GGUF -> 10000
	// Explicit per-layer flags, used verbatim when the GGUF carries
	// attention.sliding_window_pattern as an *array* (llama.cpp reads the array
	// first and only falls back to the scalar). Empty for this model.
	SwaLayers []uint8
}

// IsSwaLayer is llama.cpp's set_swa_pattern(n_pattern, dense_first=false):
//
//	is_swa_impl[il] = n_pattern == 0 || il % n_pattern < n_pattern - 1
//
// so with the default 6 the *global* layers are 5, 11, 17 and 23 of 24. Pure
// pattern query: the window width does not enter here, exactly as in llama.cpp,
// where is_swa() keys the RoPE-base choice off the pattern alone.
func (hp *GemmaHParams) IsSwaLayer(il int) bool {
	if len(hp.SwaLayers) > 0 {
		return il >= 0 && il < len(hp.SwaLayers) && hp.SwaLayers[il] != 0
	}
	if hp.SwaPattern <= 0 {
		return true
	}
	return (il % hp.SwaPattern) < (hp.SwaPattern - 1)
}

// HalfWindowFor returns the symmetric half-width of the attention band for layer
// il, or -1 for full attention.
//
// llama.cpp's is_masked_swa for LLAMA_SWA_TYPE_SYMMETRIC is
//
//	const int32_t half_n_swa = (int32_t) n_swa / 2;
//	masked iff |p1 - p0| > half_n_swa
//
// NOTE the halving: the declared 512 is a +-256 band, i.e. a 513-wide window,
// not +-512. Confirmed against llama.cpp's own mask dump at n_swa = 8, where
// row 0 is open for columns 0..4 and masked from 5.
func (hp *GemmaHParams) HalfWindowFor(il int) int {
	if hp.NSwa <= 0 || !hp.IsSwaLayer(il) {
		return -1
	}
	return hp.NSwa / 2
}

// gemmaLayer holds weights for one transformer block.
// Large projection matrices are kept as Q8Tensor (mmap-backed).
// Small norm vectors are dequantized to float32.
type gemmaLayer struct {
	// Attention — norm weights (small, float32)
	attnNormW     []float32 // [dim]
	attnQNormW    []float32 // [headDim]
	attnKNormW    []float32 // [headDim]
	postAttnNormW []float32 // [dim]

	// Attention — projection matrices (large, Q8 mmap)
	attnQWeight   tensor.Q8Tensor // [dim, dim]
	attnKWeight   tensor.Q8Tensor // [kvDim, dim]  (rows=kvDim)
	attnVWeight   tensor.Q8Tensor // [kvDim, dim]
	attnOutWeight tensor.Q8Tensor // [dim, dim]

	// FFN — norm weights (small, float32)
	ffNormW      []float32 // [dim]
	postFFNNormW []float32 // [dim]

	// FFN — projection matrices (large, Q8 mmap)
	ffGateWeight tensor.Q8Tensor // [ffDim, dim]
	ffUpWeight   tensor.Q8Tensor // [ffDim, dim]
	ffDownWeight tensor.Q8Tensor // [dim, ffDim]  (rows=dim, cols=ffDim)
}

// gemmaWeights holds all model weights.
type gemmaWeights struct {
	tokenEmb   tensor.Q8Tensor // [vocabSize, dim] — largest tensor, kept quantized
	layers     []gemmaLayer
	outputNorm []float32 // [dim]
}

// GemmaEmbedder is a pure Go Gemma2 text embedding model.
type GemmaEmbedder struct {
	hp           GemmaHParams
	weights      gemmaWeights
	tokenizer    *Tokenizer
	dim          int    // output dim (MRL truncation)
	modelName    string // identity from GGUF metadata; feeds ModelID
	mu           sync.Mutex
	mmap         *gguf.MmapFile // kept alive for the mmap backing
	scratch      *gemmaScratch  // reusable inference buffers (lazily initialized)
	tokenCache   *tokenEmbCache // cached float32 embeddings for hot tokens
	earlyExit    int32          // 0 = disabled; >0 = exit after this many layers (atomic)
	fusionOff    bool           // MACLAW_EMBED_FUSION=0
	accelInfo    AccelInfo
	scratchPools [4]sync.Pool
	packOnce     sync.Once
	dataOnce     sync.Once
	skipPack     bool // constructor Dual8 warmup: Dual3 mmap, no qs arena
}

// tokenEmbCache caches dequantized float32 embeddings for frequently-used tokens.
// Avoids repeated Q8→float32 dequantization for common tokens (articles, prepositions,
// punctuation, etc.) that appear in nearly every tool description.
type tokenEmbCache struct {
	cache map[int][]float32 // token_id → float32[dim]
}

const tokenCacheSize = 1024 // cache top-N most common tokens

// newTokenEmbCache pre-computes float32 embeddings for the most frequent tokens.
// Low-ID tokens in SentencePiece vocabularies tend to be the most common
// (single characters, common subwords), so we cache the first tokenCacheSize IDs.
func newTokenEmbCache(tokenEmb *tensor.Q8Tensor, dim int) *tokenEmbCache {
	tc := &tokenEmbCache{cache: make(map[int][]float32, tokenCacheSize)}
	n := tokenCacheSize
	if n > tokenEmb.Rows {
		n = tokenEmb.Rows
	}
	buf := make([]float32, dim)
	scale := float32(math.Sqrt(float64(dim)))
	for id := 0; id < n; id++ {
		emb := make([]float32, dim)
		tokenEmb.DequantRow(id, buf)
		copy(emb, buf)
		for i := range emb {
			emb[i] *= scale
		}
		tc.cache[id] = emb
	}
	return tc
}

// Get returns the cached float32 embedding for a token, or nil if not cached.
func (tc *tokenEmbCache) Get(id int) []float32 {
	return tc.cache[id]
}

// gemmaScratch holds reusable scratch buffers for forward pass.
// Allocated once on first Embed call, reused across subsequent calls.
type gemmaScratch struct {
	arena   []float32
	x       []float32 // hidden state [seq*dim]
	normed  []float32
	q, k, v []float32
	attnOut []float32
	projOut []float32
	ffGate  []float32
	ffUp    []float32
	ffDown  []float32
	yTile   []float32
	rowBuf  []float32
	scores  []float32
	poolOut []float32
	ropeCos []float32
	ropeSin []float32
	// Second RoPE table pair, for the SWA layers' different base.  Left nil when
	// the two bases are equal (then ropeCos/ropeSin serve every layer), so a nil
	// pair must never be selected -- see the len()>0 guard at the call sites,
	// which mirrors the port's !ropeCosSwa.empty().
	ropeCosSwa []float32
	ropeSinSwa []float32
	seqCap     int
	ropeSeq    int
}

// NewGemmaEmbedder loads a Gemma2 embedding model from a GGUF file.
func NewGemmaEmbedder(modelPath string, dim int) (*GemmaEmbedder, error) {
	if dim <= 0 {
		dim = 256
	}
	waitMmapCloses()

	mf, err := gguf.OpenMmap(modelPath)
	if err != nil {
		return nil, fmt.Errorf("gemma: open mmap: %w", err)
	}

	// Read hyperparameters
	arch := gguf.GetMetaStr(mf.Meta, "general.architecture")
	if arch == "" {
		arch = "gemma-embedding"
	}
	prefix := arch + "."

	// Model identity read straight from the GGUF header. ModelID folds it into
	// the stored vector identity, so replacing the file with a different
	// checkpoint invalidates the index even when the output dim is unchanged.
	modelName := strings.TrimSpace(gguf.GetMetaStr(mf.Meta, "general.name"))
	if modelName == "" {
		modelName = strings.TrimSpace(gguf.GetMetaStr(mf.Meta, "general.basename"))
	}
	if modelName == "" {
		modelName = arch
	}

	embDim := gguf.GetMetaI32(mf.Meta, prefix+"embedding_length", 768)
	// Clamp the requested output dim to what the forward pass can produce.
	// truncateAndNormalize() already clamps per call, but Dim() returned the
	// unclamped request, so Dim() disagreed with the length of the vector Embed()
	// returns -- and the golden writer declared a header dim that did not match
	// its payload (a file no reader can parse).  The C++ port had the same bug.
	if dim > embDim {
		dim = embDim
	}
	nHeads := gguf.GetMetaI32(mf.Meta, prefix+"attention.head_count", 3)
	nKVHeads := gguf.GetMetaI32(mf.Meta, prefix+"attention.head_count_kv", 1)
	headDim := embDim / nHeads

	// Sliding-window attention, and the per-layer RoPE base that comes with it.
	//
	// This GGUF carries the window but neither the pattern nor freq_base_swa, so
	// both take llama.cpp's defaults (load_swa_pattern(ml, 6) and the 1e4 that
	// get_key falls back to).  Both are read anyway: the whole point of reading
	// them rather than hardcoding is that a file which *does* declare them must
	// not be silently misread.
	nSwa := gguf.GetMetaI32(mf.Meta, prefix+"attention.sliding_window", 0)
	ropeThetaSwa := gguf.GetMetaF32(mf.Meta, prefix+"rope.freq_base_swa", 1e4)
	swaPattern := 6
	// llama.cpp's get_arr reads the per-layer array first and only falls back to
	// the scalar.  Element type matters: a BOOL array lands in I32s only because
	// readArray was taught to materialise it (it used to be skipped, which would
	// have made this a silent fallback to pattern 6).
	var swaLayers []uint8
	if arr := gguf.GetMetaI32Arr(mf.Meta, prefix+"attention.sliding_window_pattern"); len(arr) > 0 {
		swaLayers = make([]uint8, len(arr))
		for i, v := range arr {
			if v != 0 {
				swaLayers[i] = 1
			}
		}
	} else if p := gguf.GetMetaI32(mf.Meta, prefix+"attention.sliding_window_pattern", 0); p > 0 {
		swaPattern = p
	}

	hp := GemmaHParams{
		Dim:        embDim,
		NLayers:    gguf.GetMetaI32(mf.Meta, prefix+"block_count", 24),
		NHeads:     nHeads,
		NKVHeads:   nKVHeads,
		HeadDim:    headDim,
		KVDim:      headDim * nKVHeads,
		FFDim:      gguf.GetMetaI32(mf.Meta, prefix+"feed_forward_length", 1152),
		MaxSeqLen:  gguf.GetMetaI32(mf.Meta, prefix+"context_length", 2048),
		RMSNormEps: gguf.GetMetaF32(mf.Meta, prefix+"attention.layer_norm_rms_epsilon", 1e-6),
		RopeTheta:  gguf.GetMetaF32(mf.Meta, prefix+"rope.freq_base", 1e6),

		NSwa:         nSwa,
		SwaPattern:   swaPattern,
		RopeThetaSwa: ropeThetaSwa,
		SwaLayers:    swaLayers,
	}

	w, err := loadWeightsMmap(mf, hp)
	if err != nil {
		mf.CloseMmap()
		return nil, err
	}
	hp.VocabSize = w.tokenEmb.Rows

	// Load tokenizer
	tokens := gguf.GetMetaStrArr(mf.Meta, "tokenizer.ggml.tokens")
	if len(tokens) == 0 {
		mf.CloseMmap()
		return nil, fmt.Errorf("gemma: no tokenizer.ggml.tokens in GGUF")
	}
	var scores []float32
	if _, ok := mf.Meta["tokenizer.ggml.scores"]; ok {
		scores = gguf.LastF32Array()
	}
	tok := LoadTokenizerFromGGUF(tokens, scores,
		gguf.GetMetaI32Arr(mf.Meta, "tokenizer.ggml.token_type"),
		TokenizerOptionsFromGGUF(mf.Meta))

	g := &GemmaEmbedder{hp: hp, weights: *w, tokenizer: tok, dim: dim, mmap: mf,
		modelName:  modelName,
		tokenCache: newTokenEmbCache(&w.tokenEmb, hp.Dim),
		fusionOff:  fusionDisabledFromEnv(),
		accelInfo:  AccelInfo{Backend: BackendCPUSIMD, Reason: "cpu simd"},
	}
	g.ApplyAccel(HWAccelPreferred())

	// Early exit: for low-dim MRL outputs, skip later transformer layers.
	// Empirically, for dim<=256 the first 16/24 layers capture >95% of the
	// embedding quality. Set earlyExit=0 to disable (full model).
	if dim <= 128 && hp.NLayers > 12 {
		atomic.StoreInt32(&g.earlyExit, int32(hp.NLayers*2/3)) // ~16 of 24
	} else if dim <= 256 && hp.NLayers > 16 {
		atomic.StoreInt32(&g.earlyExit, int32(hp.NLayers*3/4)) // ~18 of 24
	}

	// Pre-fill concurrent scratch so the first EmbedBatch does not pay
	// arena+RoPE on the inference path (8 short UIC/batch workers).
	for i := 0; i < 8; i++ {
		g.putScratchToPool(newGemmaScratch(hp, scratchBucket16))
	}
	g.faultInWeights()
	if dim <= 256 {
		// Dual3 mmap + Dual8 warmup without PackQS. Allocating the packed
		// arena here left ~77MiB resident and slowed BenchmarkEmbed_Medium.
		g.skipPack = true
		for i := 0; i < 6; i++ {
			_, _ = g.Embed("hello world")
		}
		_, _ = g.EmbedBatch([]string{
			"hello there friend",
			"kernel warmup phrase",
			"short text embedding",
			"vector search query",
		})
		g.skipPack = false
	}
	// Pack weights at load (not on first inference) when the VNNI M8 path will
	// use them: keeps the one-time ~300MB copy + scale prep out of the first
	// Embed. Non-VNNI machines keep the lazy short-sequence trigger in layerLoop.
	if g.useFusion() && tensor.HasAVX512VNNI() {
		g.ensurePackedQS()
	}
	return g, nil
}

func (g *GemmaEmbedder) faultInWeights() {
	if g == nil {
		return
	}
	g.weights.tokenEmb.FaultIn()
	g.faultInLayerData()
}

func (g *GemmaEmbedder) faultInLayerData() {
	if g == nil {
		return
	}
	for i := range g.weights.layers {
		l := &g.weights.layers[i]
		l.attnQWeight.FaultIn()
		l.attnKWeight.FaultIn()
		l.attnVWeight.FaultIn()
		l.attnOutWeight.FaultIn()
		l.ffGateWeight.FaultIn()
		l.ffUpWeight.FaultIn()
		l.ffDownWeight.FaultIn()
	}
}

// Close releases the mmap and all resources.
// Safe to call concurrently with Embed (waits for in-flight inference).
var (
	mmapCloseOnce sync.Once
	mmapCloseCh   chan *gguf.MmapFile
	mmapCloseWG   sync.WaitGroup
)

func waitMmapCloses() { mmapCloseWG.Wait() }

func enqueueMmapClose(mf *gguf.MmapFile) {
	if mf == nil {
		return
	}
	mmapCloseOnce.Do(func() {
		mmapCloseCh = make(chan *gguf.MmapFile, 8)
		go func() {
			for m := range mmapCloseCh {
				m.CloseMmap()
				mmapCloseWG.Done()
			}
		}()
	})
	mmapCloseWG.Add(1)
	mmapCloseCh <- mf
}

func (g *GemmaEmbedder) Close() {
	g.mu.Lock()
	mf := g.mmap
	g.mmap = nil
	g.mu.Unlock()
	enqueueMmapClose(mf)
}

// SetEarlyExit overrides the automatic early-exit layer count.
// Set to 0 to disable early exit (run all layers).
// Set to n > 0 to exit after n layers (must be <= hp.NLayers).
// Safe to call concurrently with Embed/EmbedConcurrent.
func (g *GemmaEmbedder) SetEarlyExit(layers int) {
	if layers < 0 {
		layers = 0
	}
	if layers > g.hp.NLayers {
		layers = g.hp.NLayers
	}
	atomic.StoreInt32(&g.earlyExit, int32(layers))
}

// readQ8Tensor reads a tensor as Q8Tensor from the mmap file.
// The returned Q8Tensor.Data points directly into the mmap region.
// Only Q8_0 tensors are supported; other types will return an error.
func readQ8Tensor(mf *gguf.MmapFile, name string, rows, cols int) (tensor.Q8Tensor, error) {
	raw, ti, err := mf.TensorRawBytes(name)
	if err != nil {
		return tensor.Q8Tensor{}, err
	}
	if ti.Type != gguf.TypeQ8_0 {
		return tensor.Q8Tensor{}, fmt.Errorf("gemma: tensor %q is type %d, not Q8_0; mmap optimization requires a Q8_0 quantized model", name, ti.Type)
	}
	return tensor.Q8Tensor{Data: raw, Rows: rows, Cols: cols}, nil
}

func loadWeightsMmap(mf *gguf.MmapFile, hp GemmaHParams) (*gemmaWeights, error) {
	w := &gemmaWeights{}
	var err error

	// Token embedding — the biggest tensor. Keep as Q8.
	tokRaw, tokTI, err := mf.TensorRawBytes("token_embd.weight")
	if err != nil {
		return nil, fmt.Errorf("gemma: %w", err)
	}
	vocabSize := int(tokTI.NumElements()) / hp.Dim
	if tokTI.Type == gguf.TypeQ8_0 {
		w.tokenEmb = tensor.Q8Tensor{Data: tokRaw, Rows: vocabSize, Cols: hp.Dim}
	} else {
		return nil, fmt.Errorf("gemma: token_embd.weight is type %d, expected Q8_0; use a Q8_0 quantized model for mmap support", tokTI.Type)
	}

	// Output norm — small, dequant to float32
	w.outputNorm, err = mf.TensorF32("output_norm.weight")
	if err != nil {
		return nil, fmt.Errorf("gemma: %w", err)
	}

	w.layers = make([]gemmaLayer, hp.NLayers)
	for i := 0; i < hp.NLayers; i++ {
		l := &w.layers[i]
		p := fmt.Sprintf("blk.%d.", i)

		// Small norm weights → float32
		if l.attnNormW, err = mf.TensorF32(p + "attn_norm.weight"); err != nil {
			return nil, err
		}
		if l.attnQNormW, err = mf.TensorF32(p + "attn_q_norm.weight"); err != nil {
			return nil, err
		}
		if l.attnKNormW, err = mf.TensorF32(p + "attn_k_norm.weight"); err != nil {
			return nil, err
		}
		if l.postAttnNormW, err = mf.TensorF32(p + "post_attention_norm.weight"); err != nil {
			return nil, err
		}
		if l.ffNormW, err = mf.TensorF32(p + "ffn_norm.weight"); err != nil {
			return nil, err
		}
		if l.postFFNNormW, err = mf.TensorF32(p + "post_ffw_norm.weight"); err != nil {
			return nil, err
		}

		// Large projection matrices → Q8Tensor (mmap-backed)
		if l.attnQWeight, err = readQ8Tensor(mf, p+"attn_q.weight", hp.Dim, hp.Dim); err != nil {
			return nil, err
		}
		if l.attnKWeight, err = readQ8Tensor(mf, p+"attn_k.weight", hp.KVDim, hp.Dim); err != nil {
			return nil, err
		}
		if l.attnVWeight, err = readQ8Tensor(mf, p+"attn_v.weight", hp.KVDim, hp.Dim); err != nil {
			return nil, err
		}
		if l.attnOutWeight, err = readQ8Tensor(mf, p+"attn_output.weight", hp.Dim, hp.Dim); err != nil {
			return nil, err
		}
		if l.ffGateWeight, err = readQ8Tensor(mf, p+"ffn_gate.weight", hp.FFDim, hp.Dim); err != nil {
			return nil, err
		}
		if l.ffUpWeight, err = readQ8Tensor(mf, p+"ffn_up.weight", hp.FFDim, hp.Dim); err != nil {
			return nil, err
		}
		if l.ffDownWeight, err = readQ8Tensor(mf, p+"ffn_down.weight", hp.Dim, hp.FFDim); err != nil {
			return nil, err
		}
		l.attnQWeight.PrepareScales()
		l.attnKWeight.PrepareScales()
		l.attnVWeight.PrepareScales()
		l.attnOutWeight.PrepareScales()
		l.ffGateWeight.PrepareScales()
		l.ffUpWeight.PrepareScales()
		l.ffDownWeight.PrepareScales()
	}
	w.tokenEmb.PrepareScales()
	w.tokenEmb.FaultIn()
	return w, nil
}

// packGemmaLayerQS copies Dual3 projections into one 32-byte-block arena.
// Short Embed (seq<=4) calls this once; Medium Dual8 keeps mmap Data.
func packGemmaLayerQS(w *gemmaWeights) {
	if w == nil {
		return
	}
	need := 0
	for i := range w.layers {
		l := &w.layers[i]
		need += l.attnQWeight.Rows*l.attnQWeight.Cols +
			l.attnKWeight.Rows*l.attnKWeight.Cols +
			l.attnVWeight.Rows*l.attnVWeight.Cols +
			l.attnOutWeight.Rows*l.attnOutWeight.Cols +
			l.ffGateWeight.Rows*l.ffGateWeight.Cols +
			l.ffUpWeight.Rows*l.ffUpWeight.Cols +
			l.ffDownWeight.Rows*l.ffDownWeight.Cols
	}
	if need <= 0 {
		return
	}
	arena := make([]byte, need+64)
	off := int(uintptr(unsafe.Pointer(&arena[0])) % 64)
	if off != 0 {
		arena = arena[64-off:]
	}
	rest := arena
	for i := range w.layers {
		l := &w.layers[i]
		rest = l.attnQWeight.PackQSFrom(rest)
		rest = l.attnKWeight.PackQSFrom(rest)
		rest = l.attnVWeight.PackQSFrom(rest)
		rest = l.attnOutWeight.PackQSFrom(rest)
		rest = l.ffGateWeight.PackQSFrom(rest)
		rest = l.ffUpWeight.PackQSFrom(rest)
		rest = l.ffDownWeight.PackQSFrom(rest)
	}
	for i := 0; i < len(arena); i += 4096 {
		_ = arena[i]
	}
	if n := len(arena); n > 0 {
		_ = arena[n-1]
	}
}

func fusionDisabledFromEnv() bool {
	v := os.Getenv("MACLAW_EMBED_FUSION")
	return v == "0" || v == "off"
}
