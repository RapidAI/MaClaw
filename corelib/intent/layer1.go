package intent

import "strings"

// classifyByKeywords is retained only for legacy test/build compatibility.
// Keyword-only classification is no longer an execution-route authority; all
// callers must use semantic channels (embedding/LLM fusion) or fail closed.
func classifyByKeywords(registry *KeywordRegistry, affinity *ToolAffinityRegistry, msg MessageContext) (ClassificationResult, bool) {
	_ = registry
	_ = affinity
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return ClassificationResult{
			Primary:    LabelUnknown,
			Confidence: 0,
			Layer:      1,
			Reason:     "empty message",
		}, false
	}
	return ClassificationResult{
		Primary:    LabelUnknown,
		Confidence: 0,
		Layer:      1,
		Reason:     "local lexical classification disabled; semantic classifier required",
		Degraded:   true,
	}, false
}
