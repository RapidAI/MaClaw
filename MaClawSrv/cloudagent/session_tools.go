package cloudagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// sessionNeedResolver is the process semantic resolver, narrowed to the
// fields a cloud turn already knows. Tests substitute it. Production wires
// the MaClawSrv service, which classifies through the shared embedder.
type sessionNeedResolver interface {
	ResolveSessionNeeds(ctx context.Context, tenantID, userID, taskID, userText string) (agentservice.DynamicCapabilityNeedResolution, error)
}

// SetSessionNeedResolver installs the semantic resolver consulted by every
// session and coding turn. Nil leaves the workspace tool surface in place.
func (rt *Runtime) SetSessionNeedResolver(resolver sessionNeedResolver) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.sessionNeeds = resolver
	rt.mu.Unlock()
}

func (rt *Runtime) sessionNeedResolver() sessionNeedResolver {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.sessionNeeds
}

// cloudRoutedSurface is the subset of a semantic resolution this jail can
// run. Browser, ssh, and office needs stay inactive so the turn keeps the
// workspace tools. artifact.deliver.current_channel is not a model tool:
// writing the PDF into the jail is the delivery.
type cloudRoutedSurface struct {
	active   bool
	search   bool
	fetch    bool
	generate bool
}

func cloudSurfaceFromResolution(res agentservice.DynamicCapabilityNeedResolution) cloudRoutedSurface {
	if !res.Managed {
		return cloudRoutedSurface{}
	}
	var surface cloudRoutedSurface
	for _, need := range res.Needs {
		switch need.Capability {
		case agentservice.CapabilityInformationSearchWeb:
			surface.search = true
		case agentservice.CapabilityWebFetch:
			surface.fetch = true
		case agentservice.CapabilityDocumentGenerate:
			surface.generate = true
		}
	}
	// A secondary document_generate is not copied into Needs. Stage it from
	// the classification the resolver kept, the same reading the desktop
	// would petition.
	if res.Classification.HasLabel(intent.LabelDocumentGenerate) {
		surface.generate = true
	}
	if res.Classification.Primary == intent.LabelLiveData || res.Classification.HasLabel(intent.LabelWebFetch) {
		surface.fetch = true
	}
	surface.active = surface.search || surface.fetch || surface.generate
	return surface
}

// ensureSessionRoute resolves the turn once. Empty text does not decide,
// because the prompt and the tool list can be built before the message is
// known. A resolver error or an unmanaged reading leaves the workspace
// surface in place.
func (h *agentHost) ensureSessionRoute(userText string) {
	if h == nil || h.routeDecided || strings.TrimSpace(userText) == "" {
		return
	}
	h.routeDecided = true
	// A workflow phase already names bash, read_file, and write_file in its
	// own prompt. The canned user text ("生成完整文档") classifies as
	// document_generate, and replacing those tools with generate_pdf makes
	// the phase's instructions fail every call.
	if strings.TrimSpace(h.phasePrompt) != "" {
		return
	}
	if h.rt == nil {
		return
	}
	resolver := h.rt.sessionNeedResolver()
	if resolver == nil || h.inst == nil {
		return
	}
	tenantID := strings.TrimSpace(h.inst.meta.TenantID)
	userID := strings.TrimSpace(h.inst.meta.OwnerUserID)
	if tenantID == "" || userID == "" {
		return
	}
	ctx := h.turn
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := resolver.ResolveSessionNeeds(ctx, tenantID, userID, h.task, userText)
	if err != nil {
		slog.Warn("cloudagent semantic route unavailable",
			slog.String("instance", h.inst.meta.ID),
			slog.String("task", h.task),
			slog.String("error", err.Error()))
		return
	}
	h.routed = cloudSurfaceFromResolution(res)
	if !h.routed.active {
		if res.Managed {
			slog.Info("cloudagent semantic route kept workspace tools",
				slog.String("instance", h.inst.meta.ID),
				slog.String("task", h.task),
				slog.String("primary", string(res.Classification.Primary)),
				slog.Int("needs", len(res.Needs)))
		}
		return
	}
	slog.Info("cloudagent semantic route",
		slog.String("instance", h.inst.meta.ID),
		slog.String("task", h.task),
		slog.String("primary", string(res.Classification.Primary)),
		slog.Bool("search", h.routed.search),
		slog.Bool("fetch", h.routed.fetch),
		slog.Bool("pdf", h.routed.generate))
}

func (h *agentHost) routedPrompt() string {
	if h == nil || !h.routed.active {
		return ""
	}
	var b strings.Builder
	b.WriteString("本轮可用工具由语义路由给出，只使用列出的工具。")
	if h.routed.search || h.routed.fetch {
		b.WriteString("当前事实通过列出的")
		switch {
		case h.routed.search && h.routed.fetch:
			b.WriteString(" web_search 和 web_fetch")
		case h.routed.search:
			b.WriteString(" web_search")
		default:
			b.WriteString(" web_fetch")
		}
		b.WriteString(" 获取。")
	}
	if h.routed.generate {
		if h.pdfListed() {
			b.WriteString("调用 generate_pdf，传入完整 Markdown，生成的文件就是本轮产物。")
		} else {
			b.WriteString("检索成功之后才会出现 generate_pdf，这一步先完成检索。")
		}
	}
	return b.String()
}

// pdfListed reports whether generate_pdf is on the current wave. A turn with
// no retrieval tool lists it immediately. Search and fetch are both
// retrieval: the first successful one unlocks the PDF.
func (h *agentHost) pdfListed() bool {
	if h == nil || !h.routed.generate {
		return false
	}
	if !h.routed.search && !h.routed.fetch {
		return true
	}
	return (h.routed.search && h.searchDone) || (h.routed.fetch && h.fetchDone)
}

func (h *agentHost) routedTools() []map[string]interface{} {
	if h == nil || !h.routed.active {
		return nil
	}
	tools := make([]map[string]interface{}, 0, 3)
	if h.routed.search {
		tools = append(tools, fnTool("web_search", "检索公开网页，用来核对当前事实。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []string{"query"},
		}))
	}
	if h.routed.fetch {
		tools = append(tools, fnTool("web_fetch", "读取一个公网 http(s) 页面正文。内网地址和带账号的地址不会打开。正文超过一页时结果带 next_offset，用 offset 继续读。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url":    map[string]interface{}{"type": "string"},
				"offset": map[string]interface{}{"type": "integer", "description": "从上次结果的 next_offset 继续读。省略时从正文开头读。"},
			},
			"required": []string{"url"},
		}))
	}
	if h.pdfListed() {
		tools = append(tools, fnTool("generate_pdf", "把完整 Markdown 渲染成 PDF，并作为本轮文件产物交给对话。", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":   map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"content"},
		}))
	}
	return tools
}

// routedToolDenied explains a call the current wave does not list. A PDF
// that is only waiting on retrieval must say so: the generic rejection
// reads as "this tool does not exist" and the model leaves the route.
func (h *agentHost) routedToolDenied(name string) string {
	if h != nil && strings.TrimSpace(name) == "generate_pdf" && h.routed.generate && !h.pdfListed() {
		return "检索成功之后才能调用 generate_pdf，先调用 " + h.nextLookupTool()
	}
	return "这个工具不在本轮路由结果里"
}

// nextLookupTool names a retrieval call that can still unlock the PDF.
// Search is the first wave. After that call has been tried, the hint moves
// to web_fetch.
func (h *agentHost) nextLookupTool() string {
	if h != nil && h.routed.search && !h.searchDone && !h.searchAttempted {
		return "web_search"
	}
	if h != nil && h.routed.fetch && !h.fetchDone && !h.fetchAttempted {
		return "web_fetch"
	}
	if h != nil && h.routed.search && !h.searchDone {
		return "web_search"
	}
	if h != nil && h.routed.fetch && !h.fetchDone {
		return "web_fetch"
	}
	if h != nil && h.routed.search {
		return "web_search"
	}
	return "web_fetch"
}

// cloudRoutedToolNudgeCap is how many times one owed tool is named after a
// tool-less stop. web_search, web_fetch, and generate_pdf each have their own
// count. A model that never calls still ends once the current tool is spent.
const cloudRoutedToolNudgeCap = 2

// RoutedWorkStopNudge implements agent.RoutedWorkStopGuard. A narration of
// the next lookup or of generate_pdf is not a finished cloud turn. The
// named tool is one that has not been called yet. A call that already
// reached the engine, including one that failed, is not asked for again
// as if it never happened.
func (h *agentHost) RoutedWorkStopNudge() string {
	if h == nil || !h.routed.active {
		return ""
	}
	if tool := h.owedRetrievalTool(); tool != "" {
		return h.routedStopNudge(tool, "[系统] 当前事实还没有检索到。请直接调用 "+tool+"，不要只说明将要查询。")
	}
	if h.owesGeneratedPDF() {
		prompt := "[系统] 请直接调用 generate_pdf，传入完整 Markdown。不要只说明将要生成 PDF。"
		if h.routed.search || h.routed.fetch {
			prompt = "[系统] 请直接调用 generate_pdf，把检索到的事实写成完整 Markdown 传给 content。不要只说明将要生成 PDF。"
		}
		return h.routedStopNudge("generate_pdf", prompt)
	}
	return ""
}

// UnresolvedRoutedWork implements agent.RoutedWorkStopOutcome. Reminders
// that were ignored do not turn the announcement into a finished answer.
// The sentence is only for a tool that was never called. A call that ran
// and failed keeps the model's report of that result.
func (h *agentHost) UnresolvedRoutedWork() string {
	if h == nil || !h.routed.active {
		return ""
	}
	if tool := h.owedRetrievalTool(); tool != "" {
		if !h.routedStopNudgeSpent(tool) || h.routedToolAttempted(tool) {
			return ""
		}
		msg := "这一步没有完成检索，还没有调用 " + tool + "。"
		if h.routed.generate {
			msg += "PDF 也还没有生成。"
		}
		return msg
	}
	if h.owesGeneratedPDF() && h.routedStopNudgeSpent("generate_pdf") && !h.pdfAttempted {
		return "这一步没有调用 generate_pdf，文件还没有生成。"
	}
	return ""
}

// routedToolAttempted reports a call that reached the engine. The stop
// sentence uses it so a failed call is not described as a call that never
// happened.
func (h *agentHost) routedToolAttempted(tool string) bool {
	if h == nil {
		return false
	}
	switch tool {
	case "web_search":
		return h.searchAttempted
	case "web_fetch":
		return h.fetchAttempted
	case "generate_pdf":
		return h.pdfAttempted
	default:
		return false
	}
}

func (h *agentHost) routedStopNudge(tool, prompt string) string {
	if h.routedStopNudgeSpent(tool) {
		return ""
	}
	if h.stopNudges == nil {
		h.stopNudges = map[string]int{}
	}
	h.stopNudges[tool]++
	return prompt
}

func (h *agentHost) routedStopNudgeSpent(tool string) bool {
	if h == nil || h.stopNudges == nil {
		return false
	}
	return h.stopNudges[tool] >= cloudRoutedToolNudgeCap
}

// owedRetrievalTool is the lookup that can still unlock this turn and has
// not been called. An already successful search or fetch clears the debt
// even when the other lookup was never called. A lookup that already ran
// and failed is not named again: the next unattempted lookup is, and when
// every listed lookup has run there is nothing left to demand.
func (h *agentHost) owedRetrievalTool() string {
	if h == nil || (!h.routed.search && !h.routed.fetch) {
		return ""
	}
	if (h.routed.search && h.searchDone) || (h.routed.fetch && h.fetchDone) {
		return ""
	}
	if h.routed.search && !h.searchAttempted {
		return "web_search"
	}
	if h.routed.fetch && !h.fetchAttempted {
		return "web_fetch"
	}
	return ""
}

// owesGeneratedPDF reports a listed generate_pdf that has not been called.
// A call that ran and failed is not still uncalled. pdfDone is the write.
func (h *agentHost) owesGeneratedPDF() bool {
	return h != nil && h.routed.generate && h.pdfListed() && !h.pdfDone && !h.pdfAttempted
}

func (h *agentHost) routedToolListed(name string) bool {
	if h == nil {
		return false
	}
	switch strings.TrimSpace(name) {
	case "web_search":
		return h.routed.search
	case "web_fetch":
		return h.routed.fetch
	case "generate_pdf":
		return h.pdfListed()
	default:
		return false
	}
}

func cloudLookupSucceeded(result string) bool {
	result = strings.TrimSpace(result)
	if result == "" {
		return false
	}
	for _, prefix := range []string{"Error:", "搜索失败", "缺少", "抓取失败", "获取失败"} {
		if strings.HasPrefix(result, prefix) {
			return false
		}
	}
	return true
}

var cloudWebSearch = func(ctx context.Context, query string) string {
	// Mainland search stays on the configured engines. Marking the context
	// public-only disables those engines, including Baidu. web_fetch is the
	// call that accepts a model-chosen URL, and that path refuses private hosts.
	return agent.ToolWebSearchWithStrategyCtx(ctx, websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland), map[string]interface{}{
		"query":       query,
		"max_results": float64(8),
	})
}

// cloudSearchBrowserFallback and cloudSearchBrowserSteer are the two halves
// of the note formatWebSearchResultsNote adds when a search fell back to a
// browser. This surface has no browser tool, so both halves are rewritten
// before the model sees them.
const (
	cloudSearchBrowserFallback = "已使用浏览器兜底"
	cloudSearchBrowserSteer    = "若结果仍不含目标站点，请用 browser 工具直接打开该网站。"
)

func cloudSteerSearchResult(h *agentHost, out string) string {
	if !strings.Contains(out, cloudSearchBrowserSteer) && !strings.Contains(out, cloudSearchBrowserFallback) {
		return out
	}
	next := "根据已返回的结果继续。"
	if h != nil && h.routed.fetch {
		next = "若结果仍不含目标站点，请用 web_fetch 打开结果里的 http(s) 页面。"
	}
	out = strings.ReplaceAll(out, cloudSearchBrowserFallback, "已改用备用搜索")
	return strings.ReplaceAll(out, cloudSearchBrowserSteer, next)
}

// cloudSearchHasHits reports a search that returned something the PDF can
// be written from. The formatter's empty sentence is a finished call with
// no facts, same as a fetch whose content is blank.
func cloudSearchHasHits(result string) bool {
	result = strings.TrimSpace(result)
	return result != "" && !strings.HasPrefix(result, "未找到相关结果")
}

// cloudPublicWebFetch reads one public page. ToolWebFetchCtx leaves
// PublicNetworkOnly unset, so a cloud turn could request loopback, RFC1918,
// or a metadata address. The desktop fetch path is unchanged.
func cloudPublicWebFetch(ctx context.Context, rawURL string, offset int) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if offset < 0 {
		offset = 0
	}
	result, err := websearch.FetchWithProviderCtx(ctx, rawURL, &websearch.FetchOptions{
		PublicNetworkOnly: true,
		TimeoutS:          30,
		Offset:            offset,
		// One rune cannot be smaller than one byte, so this cap never hides
		// text that would have fit in the model-visible page. The fitter
		// then shrinks the JSON to that byte budget.
		MaxChars: agent.WebFetchMaxToolResult,
	}, corelib.WebSearchProvider{})
	if err != nil || result == nil {
		if err == nil {
			err = errors.New("empty fetch")
		}
		return fmt.Sprintf("抓取失败: %v", err)
	}
	return cloudProjectFetchedPage(rawURL, result)
}

// cloudProjectFetchedPage windows one fetched page to the model-visible
// web_fetch budget. The loop's later preview keeps a head and a tail, which
// splits this JSON and drops the middle of the page. Fitting here leaves a
// valid document whose next_offset is the first rune that did not fit.
func cloudProjectFetchedPage(rawURL string, result *websearch.FetchResult) string {
	if result == nil {
		return "抓取失败: empty fetch"
	}
	page := *result
	return websearch.FitFetchPage(&page, agent.WebFetchMaxToolResult, func(page *websearch.FetchResult) string {
		return cloudFormatFetchedPage(rawURL, page)
	})
}

func cloudFormatFetchedPage(rawURL string, result *websearch.FetchResult) string {
	if result == nil {
		return "抓取失败: empty fetch"
	}
	resp := map[string]any{
		"title":   cloudBoundText(result.Title, 200),
		"url":     rawURL,
		"content": result.Content,
	}
	if strings.TrimSpace(result.Content) == "" {
		resp["note"] = "页面没有可读正文"
	}
	if result.HasMore {
		resp["has_more"] = true
		resp["next_offset"] = result.NextOffset
	} else if result.ResponseClipped {
		resp["response_clipped"] = true
		if _, ok := resp["note"]; !ok {
			resp["note"] = websearch.ResponseClippedNote
		}
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Sprintf("抓取失败: %v", err)
	}
	return string(data)
}

var cloudWebFetch = cloudPublicWebFetch

var cloudRenderPDF = defaultCloudRenderPDF

func defaultCloudRenderPDF(title, content string) ([]byte, error) {
	if err := swarm.ValidatePDFContent(content); err != nil {
		return nil, err
	}
	g := swarm.NewSwarmDocGenerator()
	if g == nil || !g.HasFont() {
		return nil, errors.New("这台云端主机没有可用的中文字体，不能生成 PDF")
	}
	// Bare hosts render with the embedded subset face; kick off the
	// background font download so later renders use a full font.
	maybeProvisionCJKFont()
	return g.GenerateSpecDoc("", title, content)
}

func (h *agentHost) execWebSearch(argsJSON string) (string, bool) {
	query := strings.TrimSpace(cloudJSONString(argsJSON, "query"))
	if query == "" {
		return "缺少 query 参数", true
	}
	ctx := h.turn
	if ctx == nil {
		ctx = context.Background()
	}
	h.searchAttempted = true
	out := cloudSteerSearchResult(h, cloudWebSearch(ctx, query))
	// No hits still returns to the model, and it does not count as a failed
	// call. It also does not unlock generate_pdf: there is nothing to put
	// in the document yet.
	if cloudLookupSucceeded(out) && cloudSearchHasHits(out) {
		h.searchDone = true
	}
	return out, !cloudLookupSucceeded(out)
}

func (h *agentHost) execWebFetch(argsJSON string) (string, bool) {
	rawURL := strings.TrimSpace(cloudJSONString(argsJSON, "url"))
	if rawURL == "" {
		return "缺少 url 参数", true
	}
	ctx := h.turn
	if ctx == nil {
		ctx = context.Background()
	}
	h.fetchAttempted = true
	out := cloudWebFetch(ctx, rawURL, cloudJSONInt(argsJSON, "offset"))
	// An empty body still returns to the model, and it does not count as a
	// failed call: the failure nudge would push the model off this route.
	// It also does not unlock generate_pdf.
	if cloudLookupSucceeded(out) && cloudFetchHasBody(out) {
		h.fetchDone = true
	}
	return out, !cloudLookupSucceeded(out)
}

func cloudFetchHasBody(result string) bool {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result)), &body); err != nil {
		return true
	}
	return strings.TrimSpace(body.Content) != ""
}

func cloudBoundText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func (h *agentHost) execGeneratePDF(argsJSON string) (string, bool) {
	content := cloudJSONString(argsJSON, "content")
	if strings.TrimSpace(content) == "" {
		return "缺少 content 参数", true
	}
	// The call carried content, so a later prose stop must not say
	// generate_pdf was never invoked. pdfDone stays false until the write.
	h.pdfAttempted = true
	title := strings.TrimSpace(cloudJSONString(argsJSON, "title"))
	if title == "" {
		title = "文档"
	}
	name := safePDFFileName(title)
	if strings.TrimSpace(h.root) == "" {
		return errJail.Error(), true
	}
	path, err := resolveInJail(h.root, name)
	if err != nil {
		return err.Error(), true
	}
	docTitle := strings.TrimSuffix(name, ".pdf")
	pdf, err := cloudRenderPDF(docTitle, content)
	if err != nil {
		return err.Error(), true
	}
	// serveFile refuses anything over its cap, and the Hub result that
	// carries the base64 is the same size. Writing the file anyway would
	// tell the model the PDF was delivered, then fail the download.
	if len(pdf) < 5 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return "生成的内容不是 PDF", true
	}
	if int64(len(pdf)) > serveFileMaxBytes {
		return fmt.Sprintf("生成的 PDF 有 %.1f MB，超过 %.0f MB 的回传上限，请缩短正文后再调用 generate_pdf。",
			float64(len(pdf))/(1<<20), float64(serveFileMaxBytes)/(1<<20)), true
	}
	created := true
	if _, statErr := os.Stat(path); statErr == nil {
		created = false
	}
	if err := os.WriteFile(path, pdf, 0o600); err != nil {
		return err.Error(), true
	}
	h.pdfDone = true
	h.noteFile(path, created)
	rel := canonicalArtifactRel(h.root, path)
	msg := "已生成 PDF：" + rel + "。文件会随本轮结果交给对话。"
	if note := cloudPDFMissingGlyphNote(title + "\n" + content); note != "" {
		msg += "\n" + note
	}
	return msg, false
}

// cloudPDFMissingGlyphNote warns the model when the render relied on the
// embedded subset face and the document uses characters it cannot print —
// they would come out as empty boxes, and silently delivering a flawed PDF
// is worse than telling the model to reword or inform the user. While a
// full font serves (system or downloaded), there is nothing to report.
// cloudPDFFontInFallback is indirect so tests can drive the note without
// manipulating docgen's process-wide detection state.
var cloudPDFFontInFallback = docgen.UsingEmbeddedFallback

// PDFMissingGlyphNote exposes the fallback missing-glyph warning to the
// other PDF render paths in this binary (hub bots render through
// bot_pdf.go) — a bot-delivered PDF with silent blank boxes is the same
// defect whether the turn was a cloud task or a bot request.
func PDFMissingGlyphNote(text string) string {
	return cloudPDFMissingGlyphNote(text)
}

func cloudPDFMissingGlyphNote(text string) string {
	if !cloudPDFFontInFallback() {
		return ""
	}
	seen := map[rune]bool{}
	var missing []rune
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || seen[r] {
			continue
		}
		seen[r] = true
		if !docgen.EmbeddedFallbackCovers(r) {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	shown := missing
	extra := ""
	if len(shown) > 20 {
		shown = shown[:20]
		extra = fmt.Sprintf(" 等 %d 个", len(missing))
	}
	return fmt.Sprintf("注意：当前主机使用中文字体兜底子集渲染，%s%s 缺少字形，在 PDF 里会显示为空白；请改用常见字重新生成，或向用户说明这一情况",
		string(shown), extra)
}

func cloudJSONString(argsJSON, key string) string {
	var body map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
		return ""
	}
	return stringArg(body, key)
}

// cloudMaxJSONInt is the largest offset a tool argument may request. A
// float past this does not fit a page cursor, and converting it to int is
// implementation-defined. Past the end of a page, fetch returns an empty body.
const cloudMaxJSONInt = 32 << 20

func cloudJSONInt(argsJSON, key string) int {
	var body map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil {
		return 0
	}
	return cloudNonNegInt(body[key])
}

// cloudNonNegInt accepts the encodings weak models use for an integer tool
// argument: a JSON number, or a numeric string copied from next_offset.
// Anything else, including a negative, reads from the start.
func cloudNonNegInt(v any) int {
	switch n := v.(type) {
	case float64:
		return cloudFiniteNonNegInt(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0
		}
		return cloudFiniteNonNegInt(f)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0
		}
		return cloudFiniteNonNegInt(f)
	default:
		return 0
	}
}

func cloudFiniteNonNegInt(f float64) int {
	if f < 0 || math.IsNaN(f) || math.IsInf(f, 0) || f > cloudMaxJSONInt {
		if f > cloudMaxJSONInt && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return cloudMaxJSONInt
		}
		return 0
	}
	return int(f)
}

// safePDFFileName keeps the PDF as one path segment inside the jail.
// Separators and Windows-reserved characters are dropped, the stem is at
// most 76 runes, and the result always ends in .pdf.
func safePDFFileName(title string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(title) {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			continue
		}
		b.WriteRune(r)
	}
	stem := strings.TrimLeft(strings.TrimSpace(b.String()), ".")
	stem = strings.Trim(stem, " ")
	if strings.HasSuffix(strings.ToLower(stem), ".pdf") {
		stem = strings.TrimLeft(strings.Trim(stem[:len(stem)-len(".pdf")], " "), ".")
	}
	if stem == "" {
		stem = "文档"
	}
	runes := []rune(stem)
	if len(runes) > 76 {
		stem = strings.TrimLeft(strings.TrimSpace(string(runes[:76])), ".")
		if stem == "" {
			stem = "文档"
		}
	}
	return stem + ".pdf"
}
