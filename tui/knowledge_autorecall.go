package main

import (
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// tuiKnowledgeSnippet extracts the best display text from a search result.
// Delegates to the shared knowledge.BestContentText for consistent priority across all platforms.
func tuiKnowledgeSnippet(r knowledge.SearchResult) string {
	return knowledge.BestContentText(r)
}
