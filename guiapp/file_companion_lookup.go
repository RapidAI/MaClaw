package guiapp

import (
	"context"
	"fmt"
	"path/filepath"
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
				"description": "打开一条公开 http 或 https 链接。论文地址（arXiv、直接 PDF、论文落地页）会经论文下载保存到文档管理并打开，同时返回正文；普通网页只提取正文。不能传 save_path、cookie 或请求头，不能渲染 JS，不能访问内网，不能下载论文以外的文件。长文用 offset 续读。",
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

func (h *IMMessageHandler) fileCompanionWebFetch(userID string, args map[string]interface{}) (string, bool) {
	narrowed, errText := fileCompanionWebFetchArgs(args)
	if errText != "" {
		return errText, false
	}
	rawURL, _ := narrowed["url"].(string)
	// A participant note is untrusted. An explicit paper URL would become a
	// file on the host, so it is refused before any download. An HTML page
	// stays on the text fetch below.
	if fileCompanionShareDiscussionUserID(userID) && fileCompanionExplicitPaperURL(rawURL) {
		return "这个链接是论文 PDF。参与者批注不能下载文件。请改打开无需登录的 HTML 摘要页。", false
	}
	if h != nil && h.app != nil && !fileCompanionShareDiscussionUserID(userID) {
		if text, ok, handled := h.fileCompanionFetchPaper(rawURL, args); handled {
			return text, ok
		}
	}
	text := h.toolWebFetch(narrowed)
	text = fileCompanionPresentWebFetch(text)
	if strings.HasPrefix(text, "抓取失败") || strings.HasPrefix(text, "缺少 url") {
		return text, false
	}
	return text, true
}

// fileCompanionExplicitPaperURL reports a URL that is itself the paper, not
// an HTML page that might link one. A failed download of this URL is the
// answer. An HTML landing page can still fall through to its text.
func fileCompanionExplicitPaperURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if arxivIDFromInput(raw) != "" || paperURLPathPDF(raw) {
		return true
	}
	request := paperRequestURL(raw)
	for _, candidate := range paperWhitelistCandidates(raw) {
		if candidate == raw || candidate == request {
			return true
		}
	}
	return false
}

// fileCompanionFetchPaper runs the browser paper pipeline for the host chat.
// A page the probe already downloaded is returned from that body. handled is
// false only when the probe has nothing to read, so the text fetch can run.
func (h *IMMessageHandler) fileCompanionFetchPaper(rawURL string, args map[string]interface{}) (string, bool, bool) {
	parent := context.Background()
	if h != nil {
		ctx, cancel := h.imToolContext()
		defer cancel()
		parent = ctx
	}
	// One public fetch uses the same 60s budget as a later text fetch. A second
	// request after this probe times out cannot wait any longer.
	path, code, page, probeErr := resolveCompanionPaperWithin(parent, rawURL, companionPaperPDFTimeoutS)
	if code != "" && parent.Err() == context.Canceled {
		return "下载已取消。", false, true
	}
	switch code {
	case "":
		text, ok := h.fileCompanionPresentAcquiredPaper(rawURL, path, args)
		return text, ok, true
	case companionPaperCodeNoPDF:
		if text, ok, presented := h.fileCompanionPresentProbedPage(rawURL, page, args); presented {
			return text, ok, true
		}
		// The probe already stored this response. Fetching it again cannot
		// produce a body the first response did not have.
		if page != nil {
			return "这个链接没有可提取的正文。伴读不能下载文件。请改打开 HTML 摘要页，或用 web_search 换一条结果。", false, true
		}
		return "", false, false
	default:
		if fileCompanionExplicitPaperURL(rawURL) {
			return fileCompanionPaperFailure(code), false, true
		}
		// The probe already downloaded this page. A failed PDF candidate must
		// not throw that body away and request the URL again.
		if text, ok, presented := h.fileCompanionPresentProbedPage(rawURL, page, args); presented {
			return fileCompanionPaperFailure(code) + "\n\n" + text, ok, true
		}
		if code == companionPaperCodeTimeout && !fileCompanionExplicitPaperURL(rawURL) {
			return "页面获取超过 60 秒，没有读到正文。", false, true
		}
		if code == companionPaperCodeBadScheme || code == companionPaperCodeTooBig || code == companionPaperCodeTimeout {
			return fileCompanionPaperFailure(code), false, true
		}
		// The probe already failed this URL. A second text fetch would wait
		// on the same public request again.
		if probeErr != nil {
			return "抓取失败: " + probeErr.Error(), false, true
		}
		return "", false, false
	}
}

// fileCompanionPaperFailure is the sentence the companion window already shows
// for the same code. The agent and the window must not disagree.
func fileCompanionPaperFailure(code string) string {
	switch code {
	case companionPaperCodeBadScheme:
		return "只接受公网上的 http 和 https 链接，没有加入文档管理。"
	case companionPaperCodeNotPDF:
		return "下载到的内容不是 PDF，没有加入文档管理。"
	case companionPaperCodeTooBig:
		return "PDF 超过 25 MiB，没有保存。"
	case companionPaperCodeTimeout:
		return "下载超过 60 秒，没有保存。"
	default:
		return "保存失败，没有加入文档管理。"
	}
}

func fileCompanionPaperReadMaxChars(args map[string]interface{}) int {
	n := intArg(args, "max_chars", fileCompanionReadDocumentMaxChars)
	if n <= 0 || n > fileCompanionReadDocumentMaxChars {
		return fileCompanionReadDocumentMaxChars
	}
	return n
}

// fileCompanionPresentAcquiredPaper opens the PDF the paper pipeline saved and
// reads it with the same reader the open document uses. The current turn stays
// on the document that was already open; continuation uses web_fetch offset,
// which finds the saved file and does not download it again.
func (h *IMMessageHandler) fileCompanionPresentAcquiredPaper(rawURL, path string, args map[string]interface{}) (string, bool) {
	if h == nil || h.app == nil {
		return "论文已下载，但伴读宿主不可用，正文还没有打开。", false
	}
	canonical := path
	name := filepath.Base(path)
	if _, granted, err := h.app.fileCompanionGrant(path); err == nil && granted != "" {
		canonical = granted
		name = filepath.Base(granted)
	} else {
		doc, openErr := h.app.FileCompanionOpen(path)
		if openErr != nil || strings.TrimSpace(doc.Error) != "" {
			reason := ""
			if openErr != nil {
				reason = openErr.Error()
			} else {
				reason = doc.Error
			}
			return "论文已下载，但没有打开：" + reason, false
		}
		if strings.TrimSpace(doc.Path) != "" {
			h.app.enqueueFileCompanionOpen([]string{doc.Path}, false)
			canonical = doc.Path
		}
		if strings.TrimSpace(doc.Name) != "" {
			name = doc.Name
		}
	}
	body, readOK := h.fileCompanionReadDocument(canonical, map[string]interface{}{
		"offset":    intArg(args, "offset", 0),
		"max_chars": fileCompanionPaperReadMaxChars(args),
	})
	var b strings.Builder
	fmt.Fprintf(&b, "已下载论文并在伴读中打开：%s\n来源：%s\n", name, rawURL)
	if !readOK {
		b.WriteString("正文没有读出来。这份论文是新打开的文档；当前对话里的 read_document 和 pdf_read_pages 仍只读原来那一份。\n\n")
		b.WriteString(body)
		if strings.Contains(body, "扫描页") {
			b.WriteString("\n请到刚打开的论文里再读扫描页。")
		}
		return b.String(), true
	}
	b.WriteString("正文如下。还没读完时，按 continue 里的 web_fetch 续读。这份论文是新打开的文档；当前对话里的 read_document 和 pdf_read_pages 仍只读原来那一份。\n\n")
	b.WriteString(fileCompanionPaperContinue(body, rawURL))
	return b.String(), true
}

// fileCompanionPaperContinue points a further read at web_fetch. The document
// reader's own continue line names read_document, which in this turn still
// means the document that was already open.
func fileCompanionPaperContinue(body, rawURL string) string {
	inBody := false
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if !inBody && trim == "" {
			inBody = true
			b.WriteByte('\n')
			continue
		}
		if !inBody && strings.HasPrefix(trim, "# continue: read_document(") {
			offset := fileCompanionHeaderValue(trim, "offset=")
			maxChars := fileCompanionHeaderValue(trim, "max_chars=")
			fmt.Fprintf(&b, "# continue: web_fetch(url=%q, offset=%s, max_chars=%s)\n", rawURL, offset, maxChars)
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// fileCompanionPresentProbedPage turns the paper probe's body into the same
// text a web_fetch would return. presented is false when there is no body.
func (h *IMMessageHandler) fileCompanionPresentProbedPage(rawURL string, page *websearch.FetchResult, args map[string]interface{}) (string, bool, bool) {
	if page == nil || strings.TrimSpace(page.Content) == "" || paperContentTypePDF(page.ContentType) {
		return "", false, false
	}
	title, text, _ := websearch.ReadablePage(page.ContentType, []byte(page.Content))
	view := *page
	view.Title = title
	view.Content = text
	if strings.TrimSpace(view.URL) == "" {
		view.URL = rawURL
	}
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	maxChars := intArg(args, "max_chars", fileCompanionWebFetchMaxChars)
	if maxChars <= 0 || maxChars > fileCompanionWebFetchMaxChars {
		maxChars = fileCompanionWebFetchMaxChars
	}
	websearch.ApplyContentWindow(&view, offset, maxChars)
	shown := fileCompanionPresentWebFetch(projectLegacyWebFetchRequested(&view, offset, rawURL))
	if strings.TrimSpace(shown) == "" {
		return "", false, false
	}
	failed := strings.HasPrefix(shown, "抓取失败") || strings.HasPrefix(shown, "缺少 url")
	return shown, !failed, true
}

// fileCompanionPresentWebFetch rewrites the fetch layer's download advice.
// A paper PDF never reaches this function: the host chat saves it through the
// paper pipeline. What remains is a non-paper binary, which still cannot take
// save_path.
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
