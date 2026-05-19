package memory

import (
	"strings"
	"unicode"
)

// ConversationSummaryUpsertOptions describes a generated conversation-summary
// write. It keeps GUI/TUI/server summary records on one shared path.
type ConversationSummaryUpsertOptions struct {
	Title            string
	Content          string
	Tags             []string
	IdentityTagCount int
	OwnerID          string
	SourceType       string
	EvidenceIDs      []string
}

// UpsertConversationSummary creates or updates a project-scoped conversation
// summary. Use this for session expiry summaries, archived task summaries, and
// similar conversation-derived records with a stable generated identity.
func (s *Store) UpsertConversationSummary(opts ConversationSummaryUpsertOptions) (UpsertResult, error) {
	if s == nil {
		return UpsertResult{}, nil
	}
	sourceType := opts.SourceType
	if sourceType == "" {
		sourceType = "conversation_summary"
	}
	boundary := generatedRecordBoundary(opts.Tags, opts.OwnerID, sourceType)
	return s.UpsertEntryByTags(UpsertByTagsOptions{
		Title:            opts.Title,
		Content:          opts.Content,
		Category:         CategoryConversationSummary,
		Tags:             opts.Tags,
		IdentityTagCount: opts.IdentityTagCount,
		Scope:            ScopeProject,
		OwnerID:          opts.OwnerID,
		SourceType:       sourceType,
		EvidenceIDs:      opts.EvidenceIDs,
		DerivedKind:      "summary",
		Boundary:         boundary,
	})
}

// IsEmptyConversationSummary reports whether an LLM fallback summary is an
// explicit no-op answer rather than durable memory content.
func IsEmptyConversationSummary(summary string) bool {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return true
	}
	normalized := strings.TrimFunc(trimmed, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	normalized = strings.ToLower(strings.TrimSpace(normalized))
	switch normalized {
	case "none", "no", "n/a", "na", "nothing", "nothing worth remembering", "no memory", "no memories", "not worth remembering", "\u65e0", "\u6ca1\u6709", "\u65e0\u5185\u5bb9", "\u65e0\u9700\u8bb0\u5f55", "\u65e0\u503c\u5f97\u8bb0\u5f55\u7684\u4fe1\u606f", "\u6ca1\u6709\u503c\u5f97\u8bb0\u5f55\u7684\u4fe1\u606f":
		return true
	default:
		return false
	}
}

func generatedRecordBoundary(tags []string, ownerID, sourceType string) *MemoryBoundary {
	boundary := InferMemoryBoundary([]Entry{{
		Tags:       tags,
		Scope:      ScopeProject,
		OwnerID:    ownerID,
		SourceType: sourceType,
	}})
	return &boundary
}
