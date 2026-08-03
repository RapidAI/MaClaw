package agent

// tools_web.go implements web_search and web_fetch tool handlers as standalone
// functions. These use corelib/websearch which has no GUI dependencies.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// ToolWebSearch performs a web search using the configured provider.
func ToolWebSearch(provider corelib.WebSearchProvider, args map[string]interface{}) string {
	return ToolWebSearchCtx(context.Background(), provider, args)
}

// ToolWebSearchWithStrategy performs a web search using the configured engine
// order, including free-engine failover and the optional browser fallback.
func ToolWebSearchWithStrategy(strategy corelib.WebSearchStrategy, args map[string]interface{}) string {
	return ToolWebSearchWithStrategyCtx(context.Background(), strategy, args)
}

func ToolWebSearchWithStrategyCtx(ctx context.Context, strategy corelib.WebSearchStrategy, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	query := StringArg(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	response, err := websearch.SearchWithStrategyCtx(ctx, query, webSearchMaxResults(args), strategy)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	return formatWebSearchResults(response.Results)
}

func ToolWebSearchCtx(ctx context.Context, provider corelib.WebSearchProvider, args map[string]interface{}) string {
	if ctx == nil {
		ctx = context.Background()
	}
	query := StringArg(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	maxResults := webSearchMaxResults(args)

	results, err := websearch.SearchWithProviderCtx(ctx, query, maxResults, provider)
	if err != nil {
		return fmt.Sprintf("搜索失败: %v", err)
	}
	return formatWebSearchResults(results)
}

func webSearchMaxResults(args map[string]interface{}) int {
	maxResults := 8
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = int(n)
		if maxResults > 20 {
			maxResults = 20
		}
	}
	return maxResults
}

func formatWebSearchResults(results []websearch.SearchResult) string {
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

func ToolWebFetchCtx(ctx context.Context, args map[string]interface{}) string {
	return ToolWebFetchWithProviderCtx(ctx, args, corelib.WebSearchProvider{})
}

// ToolWebFetchWithProvider fetches content from a URL using a provider-aware
// fetch. When the provider has enhanced fetch capabilities (e.g. TinyFish),
// it uses the provider's API for better content extraction, falling back to
// standard fetch on failure.
func ToolWebFetchWithProvider(args map[string]interface{}, provider corelib.WebSearchProvider) string {
	return ToolWebFetchWithProviderCtx(context.Background(), args, provider)
}

func ToolWebFetchWithProviderCtx(ctx context.Context, args map[string]interface{}, provider corelib.WebSearchProvider) string {
	if ctx == nil {
		ctx = context.Background()
	}
	rawURL := StringArg(args, "url")
	if rawURL == "" {
		return "缺少 url 参数"
	}

	opts := &websearch.FetchOptions{TimeoutS: corelib.DefaultAgentTimeoutSec}
	if v, ok := args["render_js"].(bool); ok {
		opts.RenderJS = v
	}
	if v := StringArg(args, "save_path"); v != "" {
		opts.SavePath = ResolvePath(v)
	}
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		opts.TimeoutS = corelib.NormalizeAgentTimeoutSec(int(v))
	}
	if v, ok := args["offset"].(float64); ok {
		opts.Offset = int(v)
	}
	if v, ok := args["max_chars"].(float64); ok {
		opts.MaxChars = int(v)
	}
	// Optional request headers (anti-bot scenarios): `headers` object plus the
	// `cookie` shortcut, which wins on conflict.
	if raw, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok && strings.TrimSpace(k) != "" {
				if opts.Headers == nil {
					opts.Headers = map[string]string{}
				}
				opts.Headers[k] = s
			}
		}
	}
	if c := strings.TrimSpace(StringArg(args, "cookie")); c != "" {
		if opts.Headers == nil {
			opts.Headers = map[string]string{}
		}
		opts.Headers["Cookie"] = c
	}

	// use_browser_cookies: inject the live browser session's cookies/UA
	// (https only — plaintext URLs would leak session credentials).
	if useBrowser, _ := args["use_browser_cookies"].(bool); useBrowser {
		if u, err := url.Parse(rawURL); err != nil || !strings.EqualFold(u.Scheme, "https") {
			return "use_browser_cookies 仅支持 https URL（http 明文会泄漏浏览器会话 cookie）"
		}
		if BrowserAuthFunc == nil {
			return "use_browser_cookies 当前环境不可用（宿主未接入浏览器会话）"
		}
		bh, err := BrowserAuthFunc(ctx, rawURL)
		if err != nil {
			return fmt.Sprintf("use_browser_cookies 失败: %v（请先用 browser 工具打开目标网站完成验证后再试）", err)
		}
		if opts.Headers == nil {
			opts.Headers = map[string]string{}
		}
		for k, v := range bh {
			if _, exists := opts.Headers[k]; !exists {
				opts.Headers[k] = v // explicit headers/cookie args win
			}
		}
	}

	// via_browser: the managed browser itself downloads the URL
	// (Browser.setDownloadBehavior); used when HTTP-level fetching is blocked.
	if viaBrowser, _ := args["via_browser"].(bool); viaBrowser {
		if opts.SavePath == "" {
			return "via_browser 需要配合 save_path 使用（浏览器下载直接落盘，不返回文本）"
		}
		if BrowserDownloadFunc == nil {
			return "via_browser 当前环境不可用（宿主未接入浏览器下载）"
		}
		savedTo, size, err := BrowserDownloadFunc(ctx, rawURL, opts.SavePath, opts.TimeoutS)
		if err != nil {
			return fmt.Sprintf("浏览器下载失败: %v", err)
		}
		return fmt.Sprintf("已下载到: %s (%d bytes)", savedTo, size)
	}

	result, err := websearch.FetchWithProviderCtx(ctx, rawURL, opts, provider)
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
