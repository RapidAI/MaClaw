package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)



// appendKnowledgeAutoRecall searches the knowledge base and injects relevant
// results into the system prompt. Uses shared constants from corelib/agent/prompt_blocks.go
// to stay in sync with GUI and agentservice (maclawsrv).
func (app *TUIApp) appendKnowledgeAutoRecall(b *strings.Builder, userMsg string) {
	if app.knowledgeStore == nil || userMsg == "" {
		return
	}

	// Truncate user message to 200 runes for the FTS query.
	query := userMsg
	if utf8.RuneCountInString(query) > agent.KnowledgeAutoRecallMaxQueryRunes {
		runes := []rune(query)
		query = string(runes[:agent.KnowledgeAutoRecallMaxQueryRunes])
	}

	// Execute search with 3-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := app.knowledgeStore.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: agent.KnowledgeAutoRecallSearchLimit,
	})
	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v", err)
		return
	}
	if len(results) == 0 {
		// FTS returned nothing — could be empty KB or no match. Stay silent.
		return
	}

	// Determine max snippets to inject based on top score.
	topScore := results[0].Score
	maxInject := agent.KnowledgeAutoRecallMaxInject(topScore)
	if maxInject == 0 {
		// Results exist but scores too low — hint the LLM to try deeper search.
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	// Write section header (shared with GUI and agentservice).
	b.WriteString(agent.KnowledgeAutoRecallHeader)

	// Inject qualifying snippets.
	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < agent.KnowledgeAutoRecallScoreThreshold {
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
		if len([]rune(text)) > agent.KnowledgeAutoRecallSnippetMaxRunes {
			text = string([]rune(text)[:agent.KnowledgeAutoRecallSnippetMaxRunes]) + "..."
		}
		// Annotate image results with [图片] prefix and asset path hint
		if r.NodeType == knowledge.NodeTypeImage || r.Source.Kind == knowledge.SourceKindImage {
			b.WriteString(fmt.Sprintf("- [图片] [%s] %s\n", source, text))
			if r.Citation != "" {
				b.WriteString(fmt.Sprintf("  (图片路径: %s, 可用 send_file 发送给用户)\n", r.Citation))
			}
		} else {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
		}
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
