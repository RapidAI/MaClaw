// corelib/embedding/gemma_prompt.go — task-prompt support for the Gemma embedder.
//
// GemmaEmbedder satisfies RoleEmbedder so callers can ask for a specific
// EmbeddingGemma task prompt. ModelID changes with the prompt regime, which is
// what makes the switch safe: the knowledge store persists the identifier next
// to every vector and only re-embeds rows whose identifier no longer matches,
// so enabling prompts triggers a one-time refresh instead of silently
// comparing vectors from two different spaces.
package embedding

import "fmt"

// EmbedWithRole embeds text under the task prompt for role.
func (g *GemmaEmbedder) EmbedWithRole(text string, role Role) ([]float32, error) {
	return g.Embed(ApplyPrompt(text, role))
}

// EmbedBatchWithRole embeds texts under the task prompt for role.
func (g *GemmaEmbedder) EmbedBatchWithRole(texts []string, role Role) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	// A no-op prompt must not cost an extra slice plus one concatenation per
	// text. When NeedsPrompt is false the prompted result is byte-identical to
	// the plain path, so take it directly.
	if !NeedsPrompt(role) {
		return g.EmbedBatch(texts)
	}
	prompted := make([]string, len(texts))
	for i, text := range texts {
		prompted[i] = ApplyPrompt(text, role)
	}
	return g.EmbedBatch(prompted)
}

// ModelID reports the identity of the vector space this embedder produces.
//
// It has three parts: the model identity declared in the GGUF header, the
// output dimension, and the prompt recipe. Any of the three changing means
// stored vectors are no longer comparable, so the knowledge store re-embeds
// them instead of silently mixing two spaces.
//
// The model name is what the generic "%T:%d" fallback cannot express: two
// different checkpoints that happen to share a dimension would otherwise be
// treated as one space, and retrieval would keep returning confident garbage
// after a model swap.
func (g *GemmaEmbedder) ModelID() string {
	name := g.modelName
	if name == "" {
		name = "gemma-embedding"
	}
	return fmt.Sprintf("%s:%d:%s", name, g.dim, SpaceRecipe())
}
