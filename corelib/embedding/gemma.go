// corelib/embedding/gemma.go — Pure Go Gemma2 embedding model (GGUF).
//
// Architecture: Gemma2-style transformer with GQA, QK-norm, post-attn norm,
// post-FFN norm, SiLU-gated FFN, RoPE.
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
	"sync"

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
	hp        GemmaHParams
	weights   gemmaWeights
	tokenizer *Tokenizer
	dim       int // output dim (MRL truncation)
	mu        sync.Mutex
	mmap      *gguf.MmapFile // kept alive for the mmap backing
	scratch   *gemmaScratch  // reusable inference buffers (lazily initialized)
}

// gemmaScratch holds reusable scratch buffers for forward pass.
// Allocated once on first Embed call, reused across subsequent calls.
type gemmaScratch struct {
	normed  []float32
	q, k, v []float32
	attnOut []float32
	projOut []float32
	ffGate  []float32
	ffUp    []float32
	ffDown  []float32
	qNormed []float32
	kNormed []float32
	rowBuf  []float32
	scores  []float32
	seqCap  int // max seq length these buffers were allocated for
}

// NewGemmaEmbedder loads a Gemma2 embedding model from a GGUF file.
func NewGemmaEmbedder(modelPath string, dim int) (*GemmaEmbedder, error) {
	if dim <= 0 {
		dim = 256
	}

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

	embDim := gguf.GetMetaI32(mf.Meta, prefix+"embedding_length", 768)
	nHeads := gguf.GetMetaI32(mf.Meta, prefix+"attention.head_count", 3)
	nKVHeads := gguf.GetMetaI32(mf.Meta, prefix+"attention.head_count_kv", 1)
	headDim := embDim / nHeads

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
	tok := LoadTokenizerFromGGUF(tokens, scores)

	return &GemmaEmbedder{hp: hp, weights: *w, tokenizer: tok, dim: dim, mmap: mf}, nil
}

// Close releases the mmap and all resources.
// Safe to call concurrently with Embed (waits for in-flight inference).
func (g *GemmaEmbedder) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mmap != nil {
		g.mmap.CloseMmap()
		g.mmap = nil
	}
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
	}
	return w, nil
}
