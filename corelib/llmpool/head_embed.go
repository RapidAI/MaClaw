package llmpool

// head_embed.go — which embedding model a ClassificationHead was trained on.
//
// A head is a linear softmax over the first HeadDim components of one
// embedding model's vectors; another model's vectors (e.g. Qwen3 after the
// single-model switch from EmbeddingGemma) are in an unrelated space, so the
// head must neither serve them nor be retrained on a mix of cached vectors
// from both. Heads and cached sample vectors written before this field
// existed were produced by EmbeddingGemma (the only hub embedder then).

import "strings"

// LegacyHeadEmbedModel is the family assumed for heads / cached sample
// vectors without an embed_model tag.
const LegacyHeadEmbedModel = "gemma"

// NormalizeHeadEmbedModel maps "" to LegacyHeadEmbedModel.
func NormalizeHeadEmbedModel(m string) string {
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return LegacyHeadEmbedModel
	}
	return m
}

// EmbedFamily is the embedding family the head was trained on.
func (h *ClassificationHead) EmbedFamily() string {
	if h == nil {
		return ""
	}
	return NormalizeHeadEmbedModel(h.EmbedModel)
}

// HeadEmbedCompatible reports whether h may score vectors from the current
// embedder family. current == "" means unknown (test embedder hooks) and is
// treated as compatible.
func HeadEmbedCompatible(h *ClassificationHead, current string) bool {
	if h == nil {
		return false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	return current == "" || h.EmbedFamily() == current
}

// SampleEmbeddingReusable reports whether a cached sample vector can be used
// to train a head for the current embedder family (else re-embed the preview).
func SampleEmbeddingReusable(sampleModel string, vecLen int, current string) bool {
	if vecLen < HeadDim {
		return false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	return current == "" || NormalizeHeadEmbedModel(sampleModel) == current
}
