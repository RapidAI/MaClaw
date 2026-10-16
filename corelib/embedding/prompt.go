// corelib/embedding/prompt.go — EmbeddingGemma task-prompt templates.
//
// EmbeddingGemma is trained to receive a short task prefix that tells the model
// which vector space the text should be projected into. Retrieval needs an
// asymmetric pair (query vs document); similarity, classification, clustering
// and code retrieval each have their own template. Templates follow Google's
// model card "Prompt Instructions" section verbatim.
//
//	query     : "task: search result | query: {content}"
//	document  : "title: {title|none} | text: {content}"
//	similarity: "task: sentence similarity | query: {content}"
//	classify  : "task: classification | query: {content}"
//	cluster   : "task: clustering | query: {content}"
//	code      : "task: code retrieval | query: {content}"
//	qa        : "task: question answering | query: {content}"
//	factcheck : "task: fact checking | query: {content}"
//
// Documents keep the title slot at its "none" placeholder: the knowledge text
// builders already fold the title into the body, and moving it into the slot
// was measured to change nothing (see
// docs/design/embeddinggemma-task-prompt-zh.md).
//
// A prompt changes the vector space. Vectors produced with and without a prompt
// are not comparable, and neither are query-space and document-space vectors.
// Any consumer that thresholds on an absolute cosine — knowledge search
// (sim<0.25 / sim<0.3), the topic-switch detector, intent anchors — therefore
// has to be re-calibrated when the prompt regime flips. GemmaEmbedder.ModelID
// changes with the regime so stored vectors are invalidated rather than mixed.
package embedding

import (
	"os"
	"strings"
	"sync/atomic"
)

// Role selects which EmbeddingGemma task prompt is prepended to the input.
type Role int

const (
	// RoleNone sends the text through unchanged (pre-prompt behaviour).
	RoleNone Role = iota
	// RoleQuery is the query half of asymmetric retrieval.
	RoleQuery
	// RoleDocument is the document/passage half of asymmetric retrieval.
	RoleDocument
	// RoleSimilarity is for symmetric sentence-to-sentence comparison.
	RoleSimilarity
	// RoleClassification is for matching a text against label descriptions.
	RoleClassification
	// RoleClustering groups texts that carry the same meaning.
	RoleClustering
	// RoleCodeRetrieval is a natural-language query against code blocks.
	RoleCodeRetrieval
	// RoleQuestionAnswering is question-to-passage matching.
	RoleQuestionAnswering
	// RoleFactChecking is claim-to-evidence matching.
	RoleFactChecking
)

// Task prompt prefixes, verbatim from the EmbeddingGemma model card.
const (
	promptQuery          = "task: search result | query: "
	promptDocument       = "title: none | text: "
	promptSimilarity     = "task: sentence similarity | query: "
	promptClassification = "task: classification | query: "
	promptClustering     = "task: clustering | query: "
	promptCodeRetrieval  = "task: code retrieval | query: "
	promptQuestionAnswer = "task: question answering | query: "
	promptFactChecking   = "task: fact checking | query: "
)

// PromptPrefix returns the prompt template for role, or "" when the role has no
// template. It does not consult the enabled switch — use ApplyPrompt for that.
func PromptPrefix(role Role) string {
	switch role {
	case RoleQuery:
		return promptQuery
	case RoleDocument:
		return promptDocument
	case RoleSimilarity:
		return promptSimilarity
	case RoleClassification:
		return promptClassification
	case RoleClustering:
		return promptClustering
	case RoleCodeRetrieval:
		return promptCodeRetrieval
	case RoleQuestionAnswering:
		return promptQuestionAnswer
	case RoleFactChecking:
		return promptFactChecking
	default:
		return ""
	}
}

// NeedsPrompt reports whether ApplyPrompt would actually change text for role,
// i.e. the prompt switch is on and the role has a template.
//
// Batch callers use it as a fast path: when it is false the prompted result is
// byte-identical to the plain one, so they can skip copying the input slice and
// concatenating a prefix per text. The condition lives here so every embedder
// applies the same rule instead of restating it — a second copy that drifts
// would turn the fast path into a silent behaviour change.
func NeedsPrompt(role Role) bool {
	return PromptsEnabled() && PromptPrefix(role) != ""
}

// promptsEnabled is on unless MACLAW_EMBED_PROMPTS is set to 0/off/false.
// It is a process-wide switch because the prompt regime defines the vector
// space: flipping it mid-flight would silently mix two incompatible spaces.
var promptsEnabled atomic.Bool

func init() {
	promptsEnabled.Store(!promptsDisabledFromEnv())
}

func promptsDisabledFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_EMBED_PROMPTS"))) {
	case "0", "off", "false", "no":
		return true
	default:
		return false
	}
}

// PromptsEnabled reports whether task prompts are currently applied.
func PromptsEnabled() bool { return promptsEnabled.Load() }

// SetPromptsEnabled overrides the prompt switch for the current process.
// Intended for tests and admin tooling; callers that already stored vectors
// must re-embed them, because ModelID changes with the regime.
func SetPromptsEnabled(on bool) { promptsEnabled.Store(on) }

// ApplyPrompt prepends the task prompt for role. It returns text unchanged for
// RoleNone, for roles without a template, and whenever prompts are disabled.
func ApplyPrompt(text string, role Role) string {
	if role == RoleNone || !PromptsEnabled() {
		return text
	}
	prefix := PromptPrefix(role)
	if prefix == "" {
		return text
	}
	return prefix + text
}

// promptRecipeVersion must be bumped whenever a prompt template changes shape.
// It is part of a vector space identity, so editing a template invalidates
// stored vectors rather than mixing two recipes in one index.
const promptRecipeVersion = "prompt-v1"

// SpaceRecipe returns the prompt-regime component of a vector space identity:
// "raw" when prompts are disabled, otherwise the current recipe version.
//
// Embedders that wrap another embedder — server adapters, proxies — cannot
// always expose the wrapped model's own ModelID. They must fold this into
// theirs, otherwise flipping the prompt regime leaves their stored vectors in
// place and one index ends up holding two incompatible spaces.
func SpaceRecipe() string {
	if PromptsEnabled() {
		return promptRecipeVersion
	}
	return "raw"
}

// RoleEmbedder is implemented by embedders that understand task prompts.
//
// Callers must go through EmbedAs / EmbedBatchAs instead of asserting this
// interface directly, so embedders without prompt support — test doubles,
// remote embedding services, the noop embedder — keep working unchanged.
type RoleEmbedder interface {
	// EmbedWithRole returns the embedding of text under the given task prompt.
	EmbedWithRole(text string, role Role) ([]float32, error)
	// EmbedBatchWithRole returns embeddings for texts under the given task prompt.
	EmbedBatchWithRole(texts []string, role Role) ([][]float32, error)
}

// EmbedAs embeds text with the task prompt for role, falling back to a plain
// Embed when the embedder does not support prompts.
func EmbedAs(emb Embedder, text string, role Role) ([]float32, error) {
	if emb == nil || IsNoop(emb) {
		return nil, nil
	}
	if roleEmbedder, ok := emb.(RoleEmbedder); ok {
		return roleEmbedder.EmbedWithRole(text, role)
	}
	return emb.Embed(text)
}

// EmbedBatchAs embeds texts with the task prompt for role, falling back to a
// plain EmbedBatch when the embedder does not support prompts.
func EmbedBatchAs(emb Embedder, texts []string, role Role) ([][]float32, error) {
	if emb == nil || IsNoop(emb) {
		return nil, nil
	}
	if roleEmbedder, ok := emb.(RoleEmbedder); ok {
		return roleEmbedder.EmbedBatchWithRole(texts, role)
	}
	return emb.EmbedBatch(texts)
}
