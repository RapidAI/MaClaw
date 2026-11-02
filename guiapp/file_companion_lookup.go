package guiapp

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	fileCompanionWebSearchMaxResults = 8
	// A literature query is a title or a short phrase. A longer string is the
	// open document being pasted into a third-party search engine.
	fileCompanionWebSearchMaxRunes = 300
	fileCompanionWebFetchMaxChars  = 16384
	fileCompanionWebFetchMaxURL    = 2048
)

func fileCompanionWebToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fileCompanionToolWebSearch,
				"description": "检索公开网页，用于文献比对、查找相关工作和核实出处。适用于当前打开的任何文档。query 只放题名、作者或短检索词，不要粘贴正文。返回标题、链接和摘要。不能访问内网，也不会打开浏览器登录态。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "检索词，例如论文题名、作者加年份，或「方法名 related work」。",
						},
						"max_results": map[string]interface{}{
							"type":        "integer",
							"description": "可选。返回条数，1 到 8，默认 8。",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fileCompanionToolWebFetch,
				"description": "打开 web_search 返回的一条公开 http 或 https 链接，提取正文，用于核对论文页面、摘要或相关工作。适用于当前打开的任何文档。不能下载文件，不能传 save_path、cookie 或请求头，不能渲染 JS，不能访问内网。长页面用 offset 续读。",
				"parameters": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"url": map[string]interface{}{
							"type":        "string",
							"description": "绝对的 http 或 https 地址。",
						},
						"offset": map[string]interface{}{
							"type":        "integer",
							"description": "可选。续读起点，字符偏移，默认 0。",
						},
						"max_chars": map[string]interface{}{
							"type":        "integer",
							"description": "可选。本次最多返回的字符数，最大 16384。",
						},
					},
					"required": []string{"url"},
				},
			},
		},
	}
}

// fileCompanionLiteratureSearchStrategy keeps the user's search engines and
// turns off the managed browser. A companion turn looks up public pages; it
// does not open the browser profile.
func fileCompanionLiteratureSearchStrategy(strategy corelib.WebSearchStrategy) corelib.WebSearchStrategy {
	strategy.BrowserFallbackEnabled = false
	strategy.BrowserHumanAssistEnabled = false
	strategy.Engines = append([]corelib.WebSearchEngineConfig(nil), strategy.Engines...)
	for i := range strategy.Engines {
		if strategy.Engines[i].Transport == corelib.WebSearchTransportBrowser {
			strategy.Engines[i].Enabled = false
		}
	}
	return strategy
}

func (h *IMMessageHandler) fileCompanionWebSearch(args map[string]interface{}) (string, bool) {
	query := strings.TrimSpace(stringVal(args, "query"))
	if query == "" {
		return "缺少 query 参数", false
	}
	if utf8.RuneCountInString(query) > fileCompanionWebSearchMaxRunes {
		return "query 过长。请改成题名、作者或短检索词，不要粘贴正文。", false
	}
	maxResults := intArg(args, "max_results", fileCompanionWebSearchMaxResults)
	if maxResults <= 0 || maxResults > fileCompanionWebSearchMaxResults {
		maxResults = fileCompanionWebSearchMaxResults
	}
	ctx, cancel := h.imToolContext()
	defer cancel()
	response, err := websearch.SearchWithStrategyCtx(ctx, query, maxResults, fileCompanionLiteratureSearchStrategy(h.getWebSearchStrategy()))
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error()), false
	}
	results := response.Results
	if len(results) == 0 {
		return "未找到相关结果", true
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "搜索 %q 找到 %d 条结果:\n\n", query, len(results))
	if response.Degraded {
		sb.WriteString("部分搜索引擎不可用。若结果不含目标论文，换一个更具体的题名或作者再搜，或用 web_fetch 打开已知的公开链接。\n\n")
	}
	for i, result := range results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n", i+1, strings.TrimSpace(result.Title), strings.TrimSpace(result.URL))
		if snippet := strings.TrimSpace(result.Snippet); snippet != "" {
			fmt.Fprintf(&sb, "   %s\n", snippet)
		}
		sb.WriteByte('\n')
	}
	return sb.String(), true
}

func fileCompanionWebFetchArgs(args map[string]interface{}) (map[string]interface{}, string) {
	rawURL := strings.TrimSpace(stringVal(args, "url"))
	if rawURL == "" {
		return nil, "缺少 url 参数"
	}
	if len(rawURL) > fileCompanionWebFetchMaxURL {
		return nil, "url 过长"
	}
	if _, err := websearch.ValidatePublicHTTPURL(rawURL); err != nil {
		return nil, "抓取失败: " + err.Error()
	}
	maxChars := intArg(args, "max_chars", fileCompanionWebFetchMaxChars)
	if maxChars <= 0 || maxChars > fileCompanionWebFetchMaxChars {
		maxChars = fileCompanionWebFetchMaxChars
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	// Rebuild the call. save_path, cookies, headers, via_browser and render_js
	// are dropped here, and the public-network flag blocks private hosts.
	narrowed := map[string]interface{}{
		"url":       rawURL,
		"offset":    offset,
		"max_chars": maxChars,
	}
	markLansengerGroupPublicWebToolArgs(narrowed)
	return narrowed, ""
}

func (h *IMMessageHandler) fileCompanionWebFetch(args map[string]interface{}) (string, bool) {
	narrowed, errText := fileCompanionWebFetchArgs(args)
	if errText != "" {
		return errText, false
	}
	text := h.toolWebFetch(narrowed)
	text = fileCompanionPresentWebFetch(text)
	if strings.HasPrefix(text, "抓取失败") || strings.HasPrefix(text, "缺少 url") {
		return text, false
	}
	return text, true
}

// fileCompanionPresentWebFetch rewrites the fetch layer's download advice.
// Companion web_fetch cannot take save_path, so a PDF or other binary reply
// must not tell the model to call a parameter it does not have.
func fileCompanionPresentWebFetch(text string) string {
	text = strings.ReplaceAll(text, "蓝信群聊的公开网络权限不允许使用浏览器、会话或 Cookie，请改用无需登录的公开来源", "伴读只能打开公开页面，不能使用浏览器或登录态。请改用无需登录的 HTML 页面")
	if strings.HasPrefix(strings.TrimSpace(text), "抓取失败") && (strings.Contains(text, "download_file") || strings.Contains(text, "via_browser") || strings.Contains(text, "browser 工具")) {
		return "抓取失败: 这个站点要求登录或人机验证。伴读不能打开浏览器，也不能下载文件。请改用无需登录的公开页面。"
	}
	if !strings.Contains(text, "save_path") {
		return text
	}
	const instead = "这个链接没有可提取的正文。伴读不能下载文件。请改打开 HTML 摘要页，或用 web_search 换一条结果。"
	var b strings.Builder
	for _, line := range strings.Split(text, "\n") {
		// Offset can slice off the leading "二进制" and still leave the
		// download instruction. A page that merely mentions the word stays.
		if strings.Contains(line, "save_path") && (strings.Contains(line, "二进制") || strings.Contains(line, "请使用 save_path") || strings.Contains(line, "如需下载")) {
			b.WriteString(instead)
			b.WriteByte('\n')
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
