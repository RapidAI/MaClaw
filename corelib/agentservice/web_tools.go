package agentservice

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// executeWebSearch handles the web_search tool.
func (c *coreAgentCallbacks) executeWebSearch(args map[string]interface{}) string {
	query := stringArg(args, "query")
	if query == "" {
		return "Error: missing query parameter"
	}
	maxResults := intArg(args, "max_results", 8)
	if maxResults > 20 {
		maxResults = 20
	}

	provider := c.resolveWebSearchProvider()
	results, err := websearch.SearchWithProvider(query, maxResults, provider)
	if err != nil {
		return fmt.Sprintf("Error: search failed: %v", err)
	}
	if len(results) == 0 {
		return "No results found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search \"%s\" — %d results:\n\n", query, len(results)))
	sb.WriteString("Use these search results as web evidence. Cite title/URL/snippet, and call web_fetch on a result URL before making precise claims that require page-level verification.\n\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// executeWebFetch handles the web_fetch tool.
func (c *coreAgentCallbacks) executeWebFetch(args map[string]interface{}) string {
	rawURL := stringArg(args, "url")
	if rawURL == "" {
		return "Error: missing url parameter"
	}

	offset := intArg(args, "offset", 0)
	maxChars := intArg(args, "max_chars", 16384)
	if maxChars <= 0 {
		maxChars = 16384
	}

	opts := &websearch.FetchOptions{
		Offset:   offset,
		MaxChars: maxChars,
		MaxBytes: 2 * 1024 * 1024, // 2MB
		TimeoutS: corelib.NormalizeAgentTimeoutSec(intArg(args, "timeout", corelib.DefaultAgentTimeoutSec)),
	}

	result, err := websearch.Fetch(rawURL, opts)
	if err != nil {
		return fmt.Sprintf("Error: fetch failed: %v", err)
	}

	start := offset
	if start < 0 {
		start = 0
	}
	end := start + len([]rune(result.Content))

	var sb strings.Builder
	sb.WriteString("Fetched web evidence. Cite the URL/title for any factual claim based on this content; if this page does not state the fact, say it is not confirmed by this source.\n")
	if result.Title != "" {
		sb.WriteString(fmt.Sprintf("Title: %s\n", result.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
	sb.WriteString(fmt.Sprintf("Type: %s | Size: %d bytes\n", result.ContentType, result.BytesRead))
	sb.WriteString(fmt.Sprintf("Read: %d-%d / %d chars\n", start, end, result.TotalChars))
	sb.WriteString(fmt.Sprintf("truncated: %t | has_more: %t | next_offset: %d\n\n", result.Truncated, result.HasMore, result.NextOffset))
	sb.WriteString(result.Content)
	if result.HasMore {
		sb.WriteString(fmt.Sprintf("\n\n--- Continuation ---\nhas_more: true\nnext_offset: %d\nPass offset=%d to continue reading.\n", result.NextOffset, result.NextOffset))
	}
	return sb.String()
}

// resolveWebSearchProvider finds the configured web search provider from AppConfig.
func (c *coreAgentCallbacks) resolveWebSearchProvider() corelib.WebSearchProvider {
	current := strings.TrimSpace(c.appCfg.WebSearchCurrentProvider)
	if current == "" {
		// Default to first available provider or empty.
		if len(c.appCfg.WebSearchProviders) > 0 {
			return c.appCfg.WebSearchProviders[0]
		}
		return corelib.WebSearchProvider{Type: "searxng"}
	}
	for _, p := range c.appCfg.WebSearchProviders {
		if strings.EqualFold(strings.TrimSpace(p.Type), current) {
			return p
		}
	}
	return corelib.WebSearchProvider{Type: current}
}
