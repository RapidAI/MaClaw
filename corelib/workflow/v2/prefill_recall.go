package v2

import (
	"context"
	"strings"
)

// RecallResult represents a single result from memory or knowledge base recall.
// This is the common interface used by the prefill system — both memory entries
// and knowledge search results are mapped to this structure by the consumer layer.
type RecallResult struct {
	Content    string  // the text content of the memory/knowledge entry
	Category   string  // "user_fact" / "project_knowledge" / "task_artifact" / "knowledge_card" / "knowledge_fact" etc.
	Source     string  // provenance: "memory" or "knowledge"
	SourceID   string  // entry ID for traceability
	Score      float64 // relevance score from recall
	SourceDesc string  // human-readable source description (e.g. "来自知识库: AI论文.pdf")
}

// RecallProvider is the interface for retrieving information from memory and
// knowledge bases. The GUI/TUI layer implements this interface by delegating
// to memory.Store.RecallDynamic() and knowledge.SQLiteStore.Search().
//
// This abstraction exists because corelib/workflow/v2 cannot import corelib/memory
// or corelib/knowledge directly (layering constraint).
type RecallProvider interface {
	// RecallForField searches memory and knowledge base for information relevant
	// to the given field. The query is constructed from the field's semantics.
	// Returns results sorted by relevance score (highest first).
	// maxResults limits the number of results returned (typically 3-5).
	RecallForField(ctx context.Context, query string, maxResults int) []RecallResult
}

// PrefillFromRecall enriches the prefill map with values from memory and knowledge base.
// It only fills fields that are NOT already populated (by PrefillFromContext).
// Only values with clear provenance are used — no LLM inference.
//
// Parameters:
//   - schema: the phase's InputSchema defining expected fields
//   - existing: already-populated prefill values (from PrefillFromContext), may be nil
//   - provider: the recall provider implementation (memory + knowledge)
//   - ctx: context for cancellation
//
// Returns the enriched map (same map if existing is non-nil, new map otherwise).
func PrefillFromRecall(ctx context.Context, schema *PhaseInputSchema, existing map[string]*PrefilledValue, provider RecallProvider) map[string]*PrefilledValue {
	if schema == nil || len(schema.Fields) == 0 || provider == nil {
		return existing
	}

	if existing == nil {
		existing = make(map[string]*PrefilledValue)
	}

	for _, field := range schema.Fields {
		// Skip if already filled by context extraction
		if _, ok := existing[field.Name]; ok {
			continue
		}
		// Skip blocked fields
		if !ShouldPrefill(field.Name) {
			continue
		}
		// Skip textarea fields that are core creative content
		if field.Type == "textarea" && field.Required {
			continue
		}

		// Build a query from field semantics
		query := buildRecallQuery(field)
		if query == "" {
			continue
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			return existing
		default:
		}

		results := provider.RecallForField(ctx, query, 3)
		if len(results) == 0 {
			continue
		}

		// Try to extract a value from recall results
		if pv := extractValueFromRecallResults(field, results); pv != nil {
			existing[field.Name] = pv
		}
	}

	return existing
}

// buildRecallQuery constructs a search query from a field's metadata.
// Uses Label + Placeholder keywords for semantic relevance.
func buildRecallQuery(field PhaseInputField) string {
	var parts []string
	if field.Label != "" {
		parts = append(parts, field.Label)
	}
	// Extract key terms from placeholder (remove "如：" prefix and example formatting)
	if field.Placeholder != "" {
		ph := field.Placeholder
		// Strip common Chinese example prefixes
		for _, prefix := range []string{"如：", "如:", "例如：", "例如:", "比如："} {
			if len(ph) > len(prefix) && ph[:len(prefix)] == prefix {
				ph = ph[len(prefix):]
				break
			}
		}
		// Take first 30 runes max
		runes := []rune(ph)
		if len(runes) > 30 {
			runes = runes[:30]
		}
		parts = append(parts, string(runes))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

// extractValueFromRecallResults tries to find a suitable value for the field
// from the recall results. Uses rule-based extraction — no LLM inference.
func extractValueFromRecallResults(field PhaseInputField, results []RecallResult) *PrefilledValue {
	// For select fields: check if any option value appears in recall content
	if field.Type == "select" && len(field.Options) > 0 {
		for _, r := range results {
			for _, opt := range field.Options {
				if opt.Value != "" && containsWord(r.Content, opt.Value) {
					return &PrefilledValue{
						Value:        opt.Value,
						Source:       r.Source,
						SourceDetail: truncateSourceDesc(r.SourceDesc, 80),
						Confidence:   recallConfidence(r),
					}
				}
			}
		}
		return nil
	}

	// For text/other fields: use the label-anchor extraction on recall content
	for _, r := range results {
		if r.Content == "" {
			continue
		}
		// Try label-based extraction from the recall content
		pv := extractByLabelAnchor(field, r.Content)
		if pv != nil {
			// Override source to reflect recall provenance
			pv.Source = r.Source
			pv.SourceDetail = truncateSourceDesc(r.SourceDesc, 80)
			pv.Confidence = recallConfidence(r)
			return pv
		}
	}

	// For short factual fields (name, h_index, etc.): if recall returns a
	// short entry (≤50 runes) with high score and matching category, use it directly
	if isShortFactField(field.Name) {
		for _, r := range results {
			runes := []rune(r.Content)
			if len(runes) > 0 && len(runes) <= 50 && r.Score > 0.5 &&
				(r.Category == "user_fact" || r.Category == "preference") {
				// Use the entire content as the value (it's short enough to be a fact)
				return &PrefilledValue{
					Value:        r.Content,
					Source:       r.Source,
					SourceDetail: truncateSourceDesc(r.SourceDesc, 80),
					Confidence:   recallConfidence(r),
				}
			}
		}
	}

	return nil
}

// isShortFactField returns true for fields that typically hold short factual values
// (e.g. name, h_index, birth_date) where a recall entry's entire content could be the answer.
var shortFactFields = map[string]bool{
	"name": true, "h_index": true, "birth_date": true,
	"total_citations": true, "total_papers": true,
	"phd_year": true, "nationality": true,
	"discipline_code": true, "funding_amount": true,
	"duration": true,
}

func isShortFactField(name string) bool {
	return shortFactFields[name]
}

// recallConfidence maps recall result properties to a confidence score.
func recallConfidence(r RecallResult) float64 {
	switch {
	case r.Source == "knowledge" && r.Score > 0.8:
		return 0.90 // high-scoring knowledge base hit
	case r.Source == "knowledge":
		return 0.80 // any knowledge base hit
	case r.Category == "user_fact":
		return 0.85 // user facts are reliable
	case r.Category == "project_knowledge":
		return 0.80
	case r.Category == "task_artifact":
		return 0.75
	default:
		return 0.65
	}
}

// containsWord checks if text contains the word (simple substring for CJK).
func containsWord(text, word string) bool {
	return word != "" && strings.Contains(text, word)
}

func truncateSourceDesc(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
