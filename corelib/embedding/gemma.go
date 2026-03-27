// corelib/embedding/gemma.go — Pure Go Gemma2 embedding model (GGUF).
//
// Architecture: Gemma2-style transformer with GQA, QK-norm, post-attn norm,
// post-FFN norm, SiLU-gated FFN, RoPE.
// Output: mean-pooled hidden states → L2 normalized embedding.
// Supports MRL truncation (768 → 512/256/128).
package embedding

import (
	"fmt"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// GemmaHParams holds Gemma2 embedding model hyperparameters.
type GemmaHParams struct {
	Dim         int     // embedding_length (768)
	NLayers     int     // block_count (24)
	NHeads      int     // attention.head_count (3)
	NKVHeads    int     // attention.head_count_kv (1)
	HeadDim     int     // derived: Dim / NHeads (256)
	KVDim       int     // derived: HeadDim * NKVHeads (256)
	FFDim       int     // feed_forward_length (1152)
	VocabSize   int     // from token_embd tensor
	MaxSeqLen   int     // context_length (2048)
	RMSNormEps  float32 // attention.layer_norm_rms_epsilon
	RopeTheta   float32 // rope.freq_base
}

// gemmaLayer holds weights for one transformer block.
type gemmaLayer struct {
	// Attention
	attnNormW     []float32 // [dim] — input layernorm (pre-attention)
	attnQWeight   []float32 // [dim, dim]
	attnKWeight   []float32 // [dim, kvDim]
	attnVWeight   []float32 // [dim, kvDim]
	attnOutWeight []float32 // [dim, dim]
	attnQNormW    []float32 // [headDim] — QK norm
	attnKNormW    []float32 // [headDim]
	postAttnNormW []float32 // [dim] — post-attention norm

	// FFN
	ffNormW      []float32 // [dim] — pre-FFN norm
	ffGateWeight []float32 // [dim, ffDim]
	ffUpWeight   []float32 // [dim, ffDim]
	ffDownWeight []float32 // [ffDim, dim]
	postFFNNormW []float32 // [dim] — post-FFN norm
}

// gemmaWeights holds all model weights.
type gemmaWeights struct {
	tokenEmb   []float32 // [vocabSize, dim]
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
}

// NewGemmaEmbedder loads a Gemma2 embedding model from a GGUF file.
func NewGemmaEmbedder(modelPath string, dim int) (*GemmaEmbedder, error) {
	if dim <= 0 {
		dim = 256
	}

	gf, err := gguf.Open(modelPath)
	if err != nil {
		return nil, fmt.Errorf("gemma: open gguf: %w", err)
	}
	defer gf.Close()

	// Read hyperparameters (gemma-embedding.xxx keys)
	arch := gguf.GetMetaStr(gf.Meta, "general.architecture")
	if arch == "" {
		arch = "gemma-embedding"
	}
	prefix := arch + "."

	embDim := gguf.GetMetaI32(gf.Meta, prefix+"embedding_length", 768)
	nHeads := gguf.GetMetaI32(gf.Meta, prefix+"attention.head_count", 3)
	nKVHeads := gguf.GetMetaI32(gf.Meta, prefix+"attention.head_count_kv", 1)
	headDim := embDim / nHeads

	hp := GemmaHParams{
		Dim:        embDim,
		NLayers:    gguf.GetMetaI32(gf.Meta, prefix+"block_count", 24),
		NHeads:     nHeads,
		NKVHeads:   nKVHeads,
		HeadDim:    headDim,
		KVDim:      headDim * nKVHeads,
		FFDim:      gguf.GetMetaI32(gf.Meta, prefix+"feed_forward_length", 1152),
		MaxSeqLen:  gguf.GetMetaI32(gf.Meta, prefix+"context_length", 2048),
		RMSNormEps: gguf.GetMetaF32(gf.Meta, prefix+"attention.layer_norm_rms_epsilon", 1e-6),
		RopeTheta:  gguf.GetMetaF32(gf.Meta, prefix+"rope.freq_base", 1e6),
	}

	w, err := loadWeights(gf, hp)
	if err != nil {
		return nil, err
	}
	hp.VocabSize = len(w.tokenEmb) / hp.Dim

	// Load tokenizer
	tokens := gguf.GetMetaStrArr(gf.Meta, "tokenizer.ggml.tokens")
	if len(tokens) == 0 {
		return nil, fmt.Errorf("gemma: no tokenizer.ggml.tokens in GGUF")
	}
	var scores []float32
	if _, ok := gf.Meta["tokenizer.ggml.scores"]; ok {
		scores = gguf.LastF32Array()
	}
	tok := LoadTokenizerFromGGUF(tokens, scores)

	return &GemmaEmbedder{hp: hp, weights: *w, tokenizer: tok, dim: dim}, nil
}

func loadWeights(gf *gguf.File, hp GemmaHParams) (*gemmaWeights, error) {
	w := &gemmaWeights{}
	var err error

	w.tokenEmb, err = gf.ReadTensorF32("token_embd.weight")
	if err != nil {
		return nil, fmt.Errorf("gemma: %w", err)
	}

	w.outputNorm, err = gf.ReadTensorF32("output_norm.weight")
	if err != nil {
		return nil, fmt.Errorf("gemma: %w", err)
	}

	w.layers = make([]gemmaLayer, hp.NLayers)
	for i := 0; i < hp.NLayers; i++ {
		l := &w.layers[i]
		p := fmt.Sprintf("blk.%d.", i)

		if l.attnNormW, err = gf.ReadTensorF32(p + "attn_norm.weight"); err != nil {
			return nil, err
		}
		if l.attnQWeight, err = gf.ReadTensorF32(p + "attn_q.weight"); err != nil {
			return nil, err
		}
		if l.attnKWeight, err = gf.ReadTensorF32(p + "attn_k.weight"); err != nil {
			return nil, err
		}
		if l.attnVWeight, err = gf.ReadTensorF32(p + "attn_v.weight"); err != nil {
			return nil, err
		}
		if l.attnOutWeight, err = gf.ReadTensorF32(p + "attn_output.weight"); err != nil {
			return nil, err
		}
		if l.attnQNormW, err = gf.ReadTensorF32(p + "attn_q_norm.weight"); err != nil {
			return nil, err
		}
		if l.attnKNormW, err = gf.ReadTensorF32(p + "attn_k_norm.weight"); err != nil {
			return nil, err
		}
		if l.postAttnNormW, err = gf.ReadTensorF32(p + "post_attention_norm.weight"); err != nil {
			return nil, err
		}
		if l.ffNormW, err = gf.ReadTensorF32(p + "ffn_norm.weight"); err != nil {
			return nil, err
		}
		if l.ffGateWeight, err = gf.ReadTensorF32(p + "ffn_gate.weight"); err != nil {
			return nil, err
		}
		if l.ffUpWeight, err = gf.ReadTensorF32(p + "ffn_up.weight"); err != nil {
			return nil, err
		}
		if l.ffDownWeight, err = gf.ReadTensorF32(p + "ffn_down.weight"); err != nil {
			return nil, err
		}
		if l.postFFNNormW, err = gf.ReadTensorF32(p + "post_ffw_norm.weight"); err != nil {
			return nil, err
		}
	}
	return w, nil
}
