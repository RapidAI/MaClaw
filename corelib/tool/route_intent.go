package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// RouteIntent is a structured rewrite of the user message used only for tool
// selection. The main agent loop still receives the original user text.
type RouteIntent struct {
	// Intent is a coarse label (start_recording, transcribe_audio, …).
	Intent string `json:"intent,omitempty"`
	// QueryForRoute is an expanded retrieval query for BM25/hybrid.
	QueryForRoute string `json:"query_for_route,omitempty"`
	// Confidence in [0,1]. Below MinRouteIntentConfidence the intent is ignored.
	Confidence float64 `json:"confidence,omitempty"`
}

// RouteOptions customizes Router.RouteWithOptions.
type RouteOptions struct {
	// Intent is an optional LLM (or test) rewrite of the user message.
	Intent *RouteIntent
	// SkipUnifiedClassifier skips live UIC fusion (embedding/tree/LLM). It must
	// not skip ClassifyCached: a fusion timeout stores a degraded unknown in
	// LoopContext, then the background tree writes the real verdict into the
	// cache before this turn's leftover router runs. Ignoring that cache is
	// how leftover BM25 hid web_search after a search tree landed (2026-08-29).
	SkipUnifiedClassifier bool
	// PreferEmbeddingOnly uses L2 only for optional tool affinity. It is for the
	// first-response path: an unavailable or inconclusive embedder falls back to
	// an already-cached full classification for the same message (a pure cache
	// read), and otherwise keeps conditional tools filtered instead of delaying
	// the main agent with L3. Explicit execution gates retain their own
	// stronger classification policy.
	PreferEmbeddingOnly bool
	// PreResolved carries the current turn's already-computed UIC
	// classification (e.g. RuntimeContext.SemanticIntent). A usable
	// (non-degraded) PreResolved is used directly. A degraded/unknown
	// PreResolved is not an authority: lookupRouteClassification consults
	// ClassifyCached before falling through, so a late tree verdict can still
	// activate tools on this turn.
	PreResolved *intent.ClassificationResult
	// CacheMessage is the ClassifyCached key. It must match the MessageContext
	// used by the turn's original Classify (text, user, recent history). Empty
	// Text falls back to the Route userMessage.
	CacheMessage intent.MessageContext
	// HostKeepTools are host-owned grants for this turn (for example a
	// configured SQL data source the user is asking to inspect). They are not
	// model-history pins. BM25 leftover ranking cannot drop them.
	HostKeepTools []string
}

// MinRouteIntentConfidence is the floor below which a rewrite is ignored.
const MinRouteIntentConfidence = 0.45

// MinCandidateRouteScore skips near-zero candidates instead of padding the
// budget. It must stay below typical #2 BM25/hybrid scores (weather+PDF
// web_search is ~0.26 fused-normalized) while still dropping exact zeros.
// Desktop computer_* tools are fail-closed separately; this gate is not
// their privilege boundary.
const MinCandidateRouteScore = 0.12

// QueryWantsMarkdownFile reports a .md file write, not a PDF conversion.
// Shared by leftover hide/HostKeep and the managed planner so "生成markdown"
// cannot mint generate_pdf.
func QueryWantsMarkdownFile(userMessage string) bool {
	return queryWantsMarkdownFile(userMessage)
}

func queryWantsMarkdownFile(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" {
		return false
	}
	// "把 markdown 转成 pdf" must keep generate_pdf. A negation ("不要 pdf，
	// 生成 markdown") is still a .md write: leftover HostKeep write_file
	// and hide generate_pdf, including when 继续 maps onto that history.
	if strings.Contains(stripMarkdownPDFNegations(q), "pdf") {
		return false
	}
	return queryMentionsMarkdownFile(q) && queryMentionsFileWrite(q)
}

func stripMarkdownPDFNegations(q string) string {
	for _, n := range []string{
		"不要 pdf", "不要pdf",
		"别生成 pdf", "别生成pdf",
		"不是 pdf", "不是pdf",
		"非 pdf", "非pdf",
		"not pdf", "no pdf",
	} {
		q = strings.ReplaceAll(q, n, " ")
	}
	return q
}

func queryMentionsMarkdownFile(q string) bool {
	if containsASCIIToken(q, "markdown") || hasDottedExt(q, ".md") {
		return true
	}
	for _, marker := range []string{"md文件", "md 文件", "成md", "为md", "成 md", "为 md"} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	return false
}

// hasDottedExt reports ext (including the leading dot) as a file suffix, not a
// longer token that only shares the prefix (.md vs .md5, .doc vs .document).
func hasDottedExt(q, ext string) bool {
	if ext == "" {
		return false
	}
	for start := 0; ; {
		idx := strings.Index(q[start:], ext)
		if idx < 0 {
			return false
		}
		after := start + idx + len(ext)
		if after == len(q) || !isASCIIAlphaNum(q[after]) {
			return true
		}
		start += idx + 1
	}
}

func queryMentionsFileWrite(q string) bool {
	for _, marker := range []string{
		"生成", "保存", "写成", "写出", "导出", "输出", "转成", "转为",
		"儲存", "寫成", "輸出", "轉成", "轉為",
	} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	for _, word := range []string{
		"create", "creates", "created", "creating",
		"write", "writes", "writing",
		"save", "saves", "saved", "saving",
		"export", "exports", "exported", "exporting",
	} {
		if containsASCIIToken(q, word) {
			return true
		}
	}
	return false
}

// QueryMentionsOfficeSource reports an existing Office/spreadsheet/slides file
// the turn needs to read. Plain "生成markdown" has none.
func QueryMentionsOfficeSource(userMessage string) bool {
	return queryMentionsOfficeSource(userMessage)
}

// queryMentionsOfficeSource reports an existing Office/spreadsheet/slides file
// the turn needs to read. Plain "生成markdown" has none; hiding office then
// prevents office(action=generate_pdf) from substituting for write_file.
func queryMentionsOfficeSource(userMessage string) bool {
	q := strings.ToLower(strings.TrimSpace(userMessage))
	if q == "" {
		return false
	}
	for _, ext := range []string{".xlsx", ".xls", ".csv", ".pptx", ".ppt", ".docx", ".doc"} {
		if hasDottedExt(q, ext) {
			return true
		}
	}
	for _, marker := range []string{
		"幻灯片", "演示文稿", "工作簿", "电子表格",
		"word文档", "word 文档", "word文件", "word 文件",
	} {
		if strings.Contains(q, marker) {
			return true
		}
	}
	// File-type tokens must be whole words: substring "xlsx" would match
	// "xlsxish", the same class of miss as excel⊂excellent.
	for _, word := range []string{"excel", "spreadsheet", "powerpoint", "xlsx", "pptx", "docx", "xls", "csv", "ppt"} {
		if containsASCIIToken(q, word) {
			return true
		}
	}
	return false
}

// applyMarkdownFileSurface hides PDF-producing tools for a .md file write and
// returns this-turn host keeps (write_file, and office when a workbook/slides
// source is named). HostKeep still wins: applyHostKeepTools runs afterward.
func applyMarkdownFileSurface(userMessage string, condKeep, suppressedTools map[string]bool) []string {
	if !queryWantsMarkdownFile(userMessage) {
		return nil
	}
	officeSource := queryMentionsOfficeSource(userMessage)
	if suppressedTools != nil {
		suppressedTools["generate_pdf"] = true
		// Production 2026-09-20 also ranked craft_generate_pdf via skill match
		// and exposed search_and_install_skill. A .md write does not need a
		// Skill install hop.
		suppressedTools["search_and_install_skill"] = true
		if !officeSource {
			suppressedTools["office"] = true
		}
	}
	if condKeep != nil {
		delete(condKeep, "generate_pdf")
		if !officeSource {
			delete(condKeep, "office")
		}
	}
	keep := []string{"write_file"}
	if officeSource {
		keep = append(keep, "office")
	}
	return keep
}

// ShouldAttemptRouteIntentRewrite reports whether a short/ambiguous message
// is worth a lightweight LLM rewrite before tool routing.
func ShouldAttemptRouteIntentRewrite(userMessage string) bool {
	msg := strings.TrimSpace(userMessage)
	if msg == "" {
		return false
	}
	// Pure acknowledgements / option digits add latency with no routing value.
	if isTrivialRouteMessage(msg) {
		return false
	}
	n := utf8.RuneCountInString(msg)
	// Short messages are the main failure mode (low BM25/hybrid signal).
	if n <= 48 {
		return true
	}
	// Medium-length but still vague / multi-intent.
	if n <= 96 {
		lower := strings.ToLower(msg)
		for _, marker := range []string{
			"录音", "錄音", "录制", "錄製", "会议", "會議", "纪要", "紀要",
			"record", "meeting", "minutes", "帮我", "幫我", "弄一下", "处理一下",
		} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}

// isTrivialRouteMessage is true for confirm/ack/option replies that should not
// burn an LLM rewrite (routing already has full history context later).
func isTrivialRouteMessage(msg string) bool {
	m := strings.ToLower(strings.TrimSpace(msg))
	// Strip common trailing punctuation.
	m = strings.TrimRight(m, "。.!！?？~～…")
	m = strings.TrimSpace(m)
	switch m {
	case "好", "好的", "嗯", "嗯嗯", "行", "可以", "中", "成", "收到",
		"ok", "okay", "yes", "y", "no", "n",
		"是", "否", "对", "對", "继续", "繼續", "确认", "確認", "取消",
		"谢谢", "謝謝", "感谢", "感謝",
		"你好", "你好呀", "你好啊", "哈喽", "hi", "hello", "hey",
		"1", "2", "3", "4", "5",
		"开工", "開工":
		return true
	default:
		return false
	}
}

// ParseRouteIntentJSON extracts a RouteIntent from model output (raw JSON or
// fenced ```json blocks). Returns nil on failure.
func ParseRouteIntentJSON(raw string) *RouteIntent {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Strip markdown fences if present.
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			raw = raw[i : j+1]
		}
	}
	var intent RouteIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return nil
	}
	return normalizeRouteIntent(&intent)
}

func normalizeRouteIntent(intent *RouteIntent) *RouteIntent {
	if intent == nil {
		return nil
	}
	intent.Intent = strings.TrimSpace(strings.ToLower(intent.Intent))
	intent.QueryForRoute = strings.TrimSpace(intent.QueryForRoute)
	if intent.Confidence < 0 {
		intent.Confidence = 0
	}
	if intent.Confidence > 1 {
		intent.Confidence = 1
	}
	// Empty useful payload → ignore.
	if intent.QueryForRoute == "" {
		return nil
	}
	// Models often omit confidence; treat missing (0) as mid-high trust when
	// they still produced a concrete rewrite payload.
	if intent.Confidence == 0 {
		intent.Confidence = 0.7
	}
	return intent
}

// Usable reports whether the intent should influence routing.
func (intent *RouteIntent) Usable() bool {
	if intent == nil {
		return false
	}
	if intent.Confidence < MinRouteIntentConfidence {
		return false
	}
	return intent.QueryForRoute != ""
}

// HasStrongLocalRouteSignal is retained for callers that can supply a trusted
// structured intent, but free-form wording is never a route authority.
func HasStrongLocalRouteSignal(userMessage string) bool {
	_ = userMessage
	return false
}

// SearchQuery returns the text to feed BM25/hybrid (falls back to userMessage).
func (intent *RouteIntent) SearchQuery(userMessage string) string {
	if intent != nil && intent.Usable() && intent.QueryForRoute != "" {
		return intent.QueryForRoute
	}
	return userMessage
}

// availableToolNameSet builds a set of tool names from OpenAI-style defs.
func availableToolNameSet(allTools []map[string]interface{}) map[string]bool {
	out := make(map[string]bool, len(allTools))
	for _, t := range allTools {
		if name := ExtractToolName(t); name != "" {
			out[name] = true
		}
	}
	return out
}
