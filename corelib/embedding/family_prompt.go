package embedding

// family_prompt.go — prompt helpers keyed by model family, for adapters that
// format text themselves (e.g. MaClawSrv's batching adapter) instead of
// calling the model's own EmbedWithRole.

// FamilyPromptPrefix is the text prepended for role by family (FamilyQwen3
// instruct template, else the EmbeddingGemma template). "" = no prompt.
func FamilyPromptPrefix(family string, role Role) string {
	if family == FamilyQwen3 {
		if t := Qwen3TaskInstruction(role); t != "" {
			return "Instruct: " + t + "\nQuery: "
		}
		return ""
	}
	return PromptPrefix(role)
}

// ApplyFamilyPrompt formats text for role and family (honours MACLAW_EMBED_PROMPTS=0).
func ApplyFamilyPrompt(family, text string, role Role) string {
	if family == FamilyQwen3 {
		return ApplyQwen3Prompt(text, role)
	}
	return ApplyPrompt(text, role)
}

// FamilyNeedsPrompt reports whether ApplyFamilyPrompt changes text for role.
func FamilyNeedsPrompt(family string, role Role) bool {
	if family == FamilyQwen3 {
		return qwen3NeedsPrompt(role)
	}
	return NeedsPrompt(role)
}

// FamilySpaceRecipe is the prompt-regime part of a vector-space identity:
// SpaceRecipe() for Gemma, plus the instruct recipe for Qwen3 (matches the
// suffix of Qwen3Embedder.ModelID).
func FamilySpaceRecipe(family string) string {
	if family == FamilyQwen3 {
		return SpaceRecipe() + ":" + qwen3PromptRecipe
	}
	return SpaceRecipe()
}

// Qwen3EmbeddingDim is the full output width of Qwen3-Embedding-0.6B.
const Qwen3EmbeddingDim = 1024
