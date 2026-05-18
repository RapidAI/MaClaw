package agent

// tools_web.go implements web_search and web_fetch tool handlers as standalone
// functions. These use corelib/websearch which has no GUI dependencies.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// ToolWebSearch performs a web search using the configured provider.
func ToolWebSearch(provider corelib.WebSearchProvider, args map[string]interface{}) string {
	query := StringArg(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	maxResults := 8
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = int(n)
		if maxResults > 20 {
			maxResults = 20
		}
	}

	results, err := websearch.SearchWithProvider(query, maxResults, provider)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	if len(results) == 0 {
		return "未找到相关结果。"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条结果:\n\n", len(results))
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n   %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			snippet := r.Snippet
			if len([]rune(snippet)) > 200 {
				snippet = string([]rune(snippet)[:200]) + "…"
			}
			fmt.Fprintf(&b, "   %s\n", snippet)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ToolWebFetch fetches content from a URL.
func ToolWebFetch(args map[string]interface{}) string {
	return ToolWebFetchWithProvider(args, corelib.WebSearchProvider{})
}

// ToolWebFetchWithProvider fetches content from a URL using a provider-aware
// fetch. When the provider has enhanced fetch capabilities (e.g. TinyFish),
// it uses the provider's API for better content extraction, falling back to
// standard fetch on failure.
func ToolWebFetchWithProvider(args map[string]interface{}, provider corelib.WebSearchProvider) string {
	rawURL := StringArg(args, "url")
	if rawURL == "" {
		return "缺少 url 参数"
	}

	opts := &websearch.FetchOptions{}
	if v, ok := args["render_js"].(bool); ok {
		opts.RenderJS = v
	}
	if v := StringArg(args, "save_path"); v != "" {
		opts.SavePath = ResolvePath(v)
	}
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		opts.TimeoutS = int(v)
		if opts.TimeoutS > 120 {
			opts.TimeoutS = 120
		}
	}
	if v, ok := args["offset"].(float64); ok {
		opts.Offset = int(v)
	}
	if v, ok := args["max_chars"].(float64); ok {
		opts.MaxChars = int(v)
	}

	result, err := websearch.FetchWithProvider(rawURL, opts, provider)
	if err != nil {
		return fmt.Sprintf("抓取失败: %v", err)
	}

	if opts.SavePath != "" && result.SavedTo != "" {
		return fmt.Sprintf("已下载到: %s (%d bytes)", result.SavedTo, result.BytesRead)
	}

	resp := map[string]interface{}{
		"title":   result.Title,
		"url":     rawURL,
		"content": result.Content,
	}
	if result.HasMore {
		resp["has_more"] = true
		resp["next_offset"] = result.NextOffset
	}

	data, _ := json.Marshal(resp)
	return string(data)
}
