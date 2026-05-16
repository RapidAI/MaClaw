package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// knowledgeAutoRecallMaxQueryRunes limits the user message length used for auto-recall.
const knowledgeAutoRecallMaxQueryRunes = 200

// knowledgeAutoRecallScoreThreshold is the minimum score for injection.
const knowledgeAutoRecallScoreThreshold = 0.3

// knowledgeAutoRecallMaxSnippets is the maximum number of snippets to inject.
const knowledgeAutoRecallMaxSnippets = 3

// appendKnowledgeAutoRecall searches the knowledge base and injects relevant
// results into the system prompt. Mirrors the logic in
// corelib/agentservice/knowledge_integration.go.
func (app *TUIApp) appendKnowledgeAutoRecall(b *strings.Builder, userMsg string) {
	if app.knowledgeStore == nil || userMsg == "" {
		return
	}

	// Truncate user message to 200 runes for the FTS query.
	query := userMsg
	if utf8.RuneCountInString(query) > knowledgeAutoRecallMaxQueryRunes {
		runes := []rune(query)
		query = string(runes[:knowledgeAutoRecallMaxQueryRunes])
	}

	// Execute search with 3-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := app.knowledgeStore.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: 5,
	})
	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v", err)
		return
	}
	if len(results) == 0 {
		return
	}

	// Determine max snippets to inject based on top score.
	topScore := results[0].Score
	var maxInject int
	switch {
	case topScore >= 3.0:
		maxInject = knowledgeAutoRecallMaxSnippets
	case topScore >= 1.0:
		maxInject = 2
	case topScore >= knowledgeAutoRecallScoreThreshold:
		maxInject = 1
	default:
		return
	}

	// Write section header.
	b.WriteString("\n## 知识库参考（自动检索）\n")
	b.WriteString("以下内容来自知识库，与当前问题可能相关。请自然引用相关内容；不相关则忽略。\n")
	b.WriteString("如需更多信息，可调用 knowledge_search 或 knowledge_context_pack 深入检索。\n\n")

	// Inject qualifying snippets.
	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < knowledgeAutoRecallScoreThreshold {
			break
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := tuiKnowledgeSnippet(r)
		if text == "" {
			continue
		}
		if len([]rune(text)) > 200 {
			text = string([]rune(text)[:200]) + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
		injected++
	}
}

// tuiKnowledgeSnippet extracts the best display text from a search result.
func tuiKnowledgeSnippet(r knowledge.SearchResult) string {
	if r.ResultType == "fact" {
		if r.Claim != "" {
			return r.Claim
		}
		if r.Summary != "" {
			return r.Summary
		}
		if r.Subject != "" && r.Predicate != "" {
			return r.Subject + " " + r.Predicate + " " + r.Object
		}
	}
	if r.Snippet != "" {
		return r.Snippet
	}
	if r.Summary != "" {
		return r.Summary
	}
	if r.Claim != "" {
		return r.Claim
	}
	if r.Subject != "" && r.Predicate != "" {
		return r.Subject + " " + r.Predicate + " " + r.Object
	}
	return ""
}
