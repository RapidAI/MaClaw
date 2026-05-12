package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// KnowledgeStore is the interface required by the agent executor for knowledge operations.
// Satisfied by *knowledge.SQLiteStore.
type KnowledgeStore interface {
	Search(ctx context.Context, opts knowledge.SearchOptions) ([]knowledge.SearchResult, error)
	ContextPack(ctx context.Context, opts knowledge.ContextPackOptions) (knowledge.ContextPackResult, error)
	SaveURL(ctx context.Context, req knowledge.URLSaveRequest) (knowledge.Source, error)
	SaveText(ctx context.Context, req knowledge.TextSaveRequest) (knowledge.Source, error)
	Stats(ctx context.Context) (knowledge.Stats, error)
}

// SetKnowledgeStore wires the knowledge store into the executor.
// Must be called before Execute() to enable knowledge tools and auto-recall.
func (e *CoreAgentExecutor) SetKnowledgeStore(store KnowledgeStore) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.knowledgeStore = store
}

// knowledgeAutoRecallMaxQueryRunes limits the user message length used for auto-recall.
const knowledgeAutoRecallMaxQueryRunes = 200

// knowledgeAutoRecallScoreThreshold is the minimum score for injection.
const knowledgeAutoRecallScoreThreshold = 0.3

// appendKnowledgeAutoRecall searches the knowledge base and injects relevant
// results into the system prompt. Mirrors gui/im_knowledge_auto_recall.go logic.
func (c *coreAgentCallbacks) appendKnowledgeAutoRecall(b *strings.Builder, userMsg string) {
	if c.knowledgeStore == nil || userMsg == "" {
		return
	}

	query := userMsg
	if utf8.RuneCountInString(query) > knowledgeAutoRecallMaxQueryRunes {
		runes := []rune(query)
		query = string(runes[:knowledgeAutoRecallMaxQueryRunes])
	}

	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
	defer cancel()

	results, err := c.knowledgeStore.Search(ctx, knowledge.SearchOptions{
		Query:    query,
		OwnerID:  c.principal.UserID,
		TenantID: c.principal.TenantID,
		Limit:    5,
	})
	if err != nil {
		log.Printf("[knowledge_auto_recall] search error: %v", err)
		return
	}
	if len(results) == 0 {
		return
	}

	topScore := results[0].Score
	var maxInject int
	switch {
	case topScore >= 3.0:
		maxInject = 3
	case topScore >= 1.0:
		maxInject = 2
	case topScore >= knowledgeAutoRecallScoreThreshold:
		maxInject = 1
	default:
		return
	}

	b.WriteString("\n## 知识库参考（自动检索）\n")
	b.WriteString("以下内容来自知识库，与当前问题可能相关。请自然引用相关内容；不相关则忽略。\n")
	b.WriteString("如需更多信息，可调用 knowledge_search 或 knowledge_context_pack 深入检索。\n\n")

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
		text := knowledgeSnippet(r)
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

// knowledgeSnippet extracts the best display text from a search result.
func knowledgeSnippet(r knowledge.SearchResult) string {
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

// --- Knowledge tool execution ---

func (c *coreAgentCallbacks) executeKnowledgeSearch(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	opts := buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	results, err := c.knowledgeStore.Search(ctx, opts)
	if err != nil {
		return fmt.Sprintf("Error: knowledge search failed: %v", err)
	}
	if len(results) == 0 {
		return "No results found in knowledge base."
	}
	return formatSearchResults(results)
}

func (c *coreAgentCallbacks) executeKnowledgeContextPack(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	searchOpts := buildSearchOptions(args, c.principal.TenantID, c.principal.UserID)
	maxItems := intArg(args, "max_items", 10)
	maxChars := intArg(args, "max_chars", 4000)

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	result, err := c.knowledgeStore.ContextPack(ctx, knowledge.ContextPackOptions{
		SearchOptions: searchOpts,
		MaxItems:      maxItems,
		MaxChars:      maxChars,
	})
	if err != nil {
		return fmt.Sprintf("Error: knowledge context pack failed: %v", err)
	}
	if len(result.Items) == 0 {
		return "No relevant knowledge found for context pack."
	}
	// Format as structured text
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Knowledge context pack (%d items, %d chars):\n\n", result.Count, result.CharacterCount))
	for i, item := range result.Items {
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, item.ResultType, item.Text))
		if item.Citation != "" {
			b.WriteString(fmt.Sprintf("   Citation: %s\n", item.Citation))
		}
	}
	return b.String()
}

func (c *coreAgentCallbacks) executeKnowledgeSaveURL(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	url := stringArg(args, "url")
	if url == "" {
		return "Error: url parameter is required"
	}
	title := stringArg(args, "title")
	topicHint := stringArg(args, "topic_hint")

	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	source, err := c.knowledgeStore.SaveURL(ctx, knowledge.URLSaveRequest{
		URL:       url,
		OwnerID:   c.principal.UserID,
		TenantID:  c.principal.TenantID,
		TopicHint: topicHint,
	})
	if err != nil {
		return fmt.Sprintf("Error: failed to save URL: %v", err)
	}
	result := fmt.Sprintf("URL saved to knowledge base. Source ID: %s", source.ID)
	if title != "" || source.Title != "" {
		t := source.Title
		if t == "" {
			t = title
		}
		result += fmt.Sprintf(", Title: %s", t)
	}
	return result
}

func (c *coreAgentCallbacks) executeKnowledgeSaveText(args map[string]interface{}) string {
	if c.knowledgeStore == nil {
		return "Error: knowledge base is not configured"
	}
	text := stringArg(args, "text")
	if text == "" {
		text = stringArg(args, "content")
	}
	if text == "" {
		return "Error: text parameter is required"
	}
	title := stringArg(args, "title")
	topicHint := stringArg(args, "topic_hint")

	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()

	source, err := c.knowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
		Text:      text,
		Title:     title,
		OwnerID:   c.principal.UserID,
		TenantID:  c.principal.TenantID,
		TopicHint: topicHint,
	})
	if err != nil {
		return fmt.Sprintf("Error: failed to save text: %v", err)
	}
	result := fmt.Sprintf("Text saved to knowledge base. Source ID: %s", source.ID)
	if source.Title != "" {
		result += fmt.Sprintf(", Title: %s", source.Title)
	}
	return result
}

// --- Helpers ---

func buildSearchOptions(args map[string]interface{}, tenantID, userID string) knowledge.SearchOptions {
	opts := knowledge.SearchOptions{
		Query:       stringArg(args, "query"),
		OwnerID:     userID,
		TenantID:    tenantID,
		SearchScope: stringArg(args, "search_scope"),
		TopicHint:   stringArg(args, "topic_hint"),
		Domain:      stringArg(args, "domain"),
		ProjectPath: stringArg(args, "project_path"),
		Limit:       intArg(args, "limit", 8),
	}
	if opts.Limit > 50 {
		opts.Limit = 50
	}
	if v, ok := args["context_terms"]; ok {
		opts.ContextTerms = toStringSlice(v)
	}
	if v, ok := args["result_types"]; ok {
		opts.ResultTypes = toStringSlice(v)
	}
	if v, ok := args["source_kinds"]; ok {
		opts.SourceKinds = toStringSlice(v)
	}
	if v, ok := args["source_ids"]; ok {
		opts.SourceIDs = toStringSlice(v)
	}
	if v, ok := args["labels"]; ok {
		opts.Labels = toStringSlice(v)
	}
	if v, ok := args["include_disabled"]; ok {
		if b, ok2 := v.(bool); ok2 {
			opts.IncludeDisabled = b
		}
	}
	return opts
}

func formatSearchResults(results []knowledge.SearchResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	for i, r := range results {
		b.WriteString(fmt.Sprintf("### Result %d (score: %.2f, type: %s)\n", i+1, r.Score, r.ResultType))
		if r.CardTitle != "" {
			b.WriteString(fmt.Sprintf("**Title**: %s\n", r.CardTitle))
		}
		if r.Claim != "" {
			b.WriteString(fmt.Sprintf("**Claim**: %s\n", r.Claim))
		}
		if r.Summary != "" {
			b.WriteString(fmt.Sprintf("**Summary**: %s\n", r.Summary))
		}
		if r.Snippet != "" && r.Snippet != r.Claim && r.Snippet != r.Summary {
			b.WriteString(fmt.Sprintf("**Snippet**: %s\n", r.Snippet))
		}
		if r.Subject != "" {
			b.WriteString(fmt.Sprintf("**Fact**: %s %s %s\n", r.Subject, r.Predicate, r.Object))
		}
		if r.Citation != "" {
			b.WriteString(fmt.Sprintf("**Citation**: %s\n", r.Citation))
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.URI
		}
		if source != "" {
			b.WriteString(fmt.Sprintf("**Source**: %s\n", source))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok2 := v.(string); ok2 {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func intArg(args map[string]interface{}, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

func toStringSlice(v interface{}) []string {
	if arr, ok := v.([]interface{}); ok {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok2 := item.(string); ok2 {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}
