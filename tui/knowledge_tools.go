package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// toolKnowledgeSearch performs an FTS search against the knowledge store.
func (app *TUIApp) toolKnowledgeSearch(args map[string]interface{}) string {
	if app.knowledgeStore == nil {
		return "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	opts := knowledge.SearchOptions{
		Query: query,
		Limit: 8,
	}
	if scope, ok := args["search_scope"].(string); ok && scope != "" {
		opts.SearchScope = scope
	}
	if limit, ok := args["limit"].(float64); ok && limit > 0 {
		opts.Limit = int(limit)
		if opts.Limit > 50 {
			opts.Limit = 50
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := app.knowledgeStore.Search(ctx, opts)
	if err != nil {
		return fmt.Sprintf("Error: knowledge search failed: %v", err)
	}
	if len(results) == 0 {
		return "No results found in knowledge base."
	}
	// Pass asset base dir for inline image thumbnail embedding.
	assetDir := filepath.Join(commands.ResolveDataDir(), "knowledge_assets")
	return formatKnowledgeSearchResults(results, assetDir)
}

// toolKnowledgeContextPack builds a citation-backed context bundle under a character budget.
func (app *TUIApp) toolKnowledgeContextPack(args map[string]interface{}) string {
	if app.knowledgeStore == nil {
		return "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"
	}
	query, _ := args["query"].(string)
	if query == "" {
		return "Error: query parameter is required"
	}

	searchOpts := knowledge.SearchOptions{
		Query: query,
		Limit: 20,
	}
	if scope, ok := args["search_scope"].(string); ok && scope != "" {
		searchOpts.SearchScope = scope
	}

	maxItems := 8
	if mi, ok := args["max_items"].(float64); ok && mi > 0 {
		maxItems = int(mi)
		if maxItems > 30 {
			maxItems = 30
		}
	}
	maxChars := 6000
	if mc, ok := args["max_chars"].(float64); ok && mc > 0 {
		maxChars = int(mc)
		if maxChars > 20000 {
			maxChars = 20000
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := app.knowledgeStore.ContextPack(ctx, knowledge.ContextPackOptions{
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

// toolKnowledgeSaveText persists the provided text as a new source in the knowledge store.
func (app *TUIApp) toolKnowledgeSaveText(args map[string]interface{}) string {
	if app.knowledgeStore == nil {
		return "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"
	}
	text, _ := args["text"].(string)
	if text == "" {
		// Try "content" as alias.
		text, _ = args["content"].(string)
	}
	if text == "" {
		return "Error: text parameter is required"
	}

	title, _ := args["title"].(string)
	topicHint, _ := args["topic_hint"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	source, err := app.knowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
		Text:      text,
		Title:     title,
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

// toolKnowledgeSaveURL fetches the URL content and persists it as a new source.
func (app *TUIApp) toolKnowledgeSaveURL(args map[string]interface{}) string {
	if app.knowledgeStore == nil {
		return "Error: knowledge base is not configured. Import documents first with: maclaw-tui knowledge import <path>"
	}
	url, _ := args["url"].(string)
	if url == "" {
		return "Error: url parameter is required"
	}

	topicHint, _ := args["topic_hint"].(string)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	source, err := app.knowledgeStore.SaveURL(ctx, knowledge.URLSaveRequest{
		URL:       url,
		TopicHint: topicHint,
	})
	if err != nil {
		return fmt.Sprintf("Error: failed to save URL: %v", err)
	}
	result := fmt.Sprintf("URL saved to knowledge base. Source ID: %s", source.ID)
	if source.Title != "" {
		result += fmt.Sprintf(", Title: %s", source.Title)
	}
	return result
}

// formatKnowledgeSearchResults formats search results for LLM consumption.
// When assetBaseDir is non-empty, image results include inline thumbnail markers
// that the frontend can parse and render as images.
func formatKnowledgeSearchResults(results []knowledge.SearchResult, assetBaseDir ...string) string {
	baseDir := ""
	if len(assetBaseDir) > 0 {
		baseDir = assetBaseDir[0]
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d results:\n\n", len(results)))
	for i, r := range results {
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := tuiKnowledgeSnippet(r)
		if text == "" {
			text = "(no snippet)"
		}

		// Image result: annotate with [图片] prefix + inline thumbnail
		if r.NodeType == knowledge.NodeTypeImage || r.Source.Kind == knowledge.SourceKindImage {
			prefix := "[图片] "
			b.WriteString(fmt.Sprintf("%d. [%.2f] %s%s\n   %s\n", i+1, r.Score, prefix, source, text))
			// Embed inline thumbnail marker (frontend renders as image)
			if baseDir != "" {
				if embed := knowledge.EmbedImageThumbForSearchResult(r, baseDir); embed != nil {
					b.WriteString("   ")
					b.WriteString(knowledge.FormatKBImageMarker(embed))
					b.WriteString("\n")
				}
			}
			if r.Citation != "" {
				b.WriteString(fmt.Sprintf("   图片路径: %s\n", r.Citation))
			}
			b.WriteString("   提示: 使用 send_file 工具可将此图片发送给用户，或图片已通过上方标记显示给用户\n")
		} else {
			b.WriteString(fmt.Sprintf("%d. [%.2f] %s\n   %s\n", i+1, r.Score, source, text))
		}
	}
	return b.String()
}
