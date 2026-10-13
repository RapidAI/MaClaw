// corelib/embedding/qwen3_prompt.go — role / instruct handling for Qwen3-Embedding.
//
// Qwen3-Embedding is instruction-aware: queries are embedded as
//
//	"Instruct: {task description}\nQuery: {text}"
//
// while documents/passages are embedded as plain text (model card: "no
// instruction on the document side"). Roles map onto task descriptions below.
// RoleClassification carries the intent-routing instruction used to train and
// evaluate the intent heads ("Classify the intent of this assistant user
// request"); intent Layer 2 is its only caller.
//
// As with Gemma, the prompt regime is part of the vector space identity:
// ModelID includes both SpaceRecipe() (prompts on/off) and the Qwen3 instruct
// recipe version, so cached vectors never mix regimes or models.
package embedding

import "fmt"

// qwen3PromptRecipe must be bumped when a task description below changes.
const qwen3PromptRecipe = "qwen3-instruct-v1"

// Qwen3TaskInstruction returns the instruct task description for role, or ""
// when the role embeds without an instruction (RoleNone, RoleDocument).
func Qwen3TaskInstruction(role Role) string {
	switch role {
	case RoleQuery:
		return "Given a web search query, retrieve relevant passages that answer the query"
	case RoleSimilarity:
		return "Retrieve semantically similar text"
	case RoleClassification:
		return "Classify the intent of this assistant user request"
	case RoleClustering:
		return "Identify the topic or theme of the given text"
	case RoleCodeRetrieval:
		return "Given a natural language query, retrieve relevant code snippets"
	case RoleQuestionAnswering:
		return "Given a question, retrieve passages that answer the question"
	case RoleFactChecking:
		return "Given a claim, retrieve documents that support or refute the claim"
	default:
		return ""
	}
}

// ApplyQwen3Prompt formats text for role (honours MACLAW_EMBED_PROMPTS=0).
func ApplyQwen3Prompt(text string, role Role) string {
	if role == RoleNone || !PromptsEnabled() {
		return text
	}
	task := Qwen3TaskInstruction(role)
	if task == "" {
		return text
	}
	return "Instruct: " + task + "\nQuery: " + text
}

func qwen3NeedsPrompt(role Role) bool {
	return PromptsEnabled() && Qwen3TaskInstruction(role) != ""
}

// EmbedWithRole embeds text with the Qwen3 instruct prefix for role.
func (q *Qwen3Embedder) EmbedWithRole(text string, role Role) ([]float32, error) {
	q.ensurePrefix(role)
	return q.Embed(ApplyQwen3Prompt(text, role))
}

// EmbedBatchWithRole embeds texts with the Qwen3 instruct prefix for role.
func (q *Qwen3Embedder) EmbedBatchWithRole(texts []string, role Role) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if !qwen3NeedsPrompt(role) {
		return q.EmbedBatch(texts)
	}
	q.ensurePrefix(role)
	prompted := make([]string, len(texts))
	for i, t := range texts {
		prompted[i] = ApplyQwen3Prompt(t, role)
	}
	return q.EmbedBatch(prompted)
}

// ModelID identifies the vector space: GGUF model name, output dim, prompt
// regime and Qwen3 instruct recipe. It never collides with Gemma's IDs (the
// GGUF names differ and the recipe suffix is Qwen3-specific).
func (q *Qwen3Embedder) ModelID() string {
	name := q.modelName
	if name == "" {
		name = "qwen3-embedding"
	}
	return fmt.Sprintf("%s:%d:%s:%s", name, q.dim, SpaceRecipe(), qwen3PromptRecipe)
}
