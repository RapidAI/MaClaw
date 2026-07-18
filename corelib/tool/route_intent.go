package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// RouteIntent is a structured rewrite of the user message used only for tool
// selection. The main agent loop still receives the original user text.
type RouteIntent struct {
	// Intent is a coarse label (start_recording, transcribe_audio, …).
	Intent string `json:"intent,omitempty"`
	// QueryForRoute is an expanded retrieval query for BM25/hybrid.
	QueryForRoute string `json:"query_for_route,omitempty"`
	// ToolFamilies maps to known tool groups (recording, browser, ssh, …).
	ToolFamilies []string `json:"tool_families,omitempty"`
	// MustInclude tool names to pin into this turn's tool list.
	MustInclude []string `json:"must_include,omitempty"`
	// MustExclude tool names to suppress this turn.
	MustExclude []string `json:"must_exclude,omitempty"`
	// Confidence in [0,1]. Below MinRouteIntentConfidence the intent is ignored.
	Confidence float64 `json:"confidence,omitempty"`
}

// RouteOptions customizes Router.RouteWithOptions.
type RouteOptions struct {
	// Intent is an optional LLM (or test) rewrite of the user message.
	Intent *RouteIntent
	// SkipUnifiedClassifier skips full UIC fusion (tree/LLM channels, multi-second).
	// Used by ACP Mode B so editor turns stay responsive; BM25/hybrid still run.
	SkipUnifiedClassifier bool
}

// MinRouteIntentConfidence is the floor below which a rewrite is ignored.
const MinRouteIntentConfidence = 0.45

// MinCandidateRouteScore skips zero/noise candidates instead of padding the
// budget with irrelevant tools (computer_*, etc.).
const MinCandidateRouteScore = 1e-6

// Known tool families → concrete tool names. Only names present in allTools
// are applied at route time.
var toolFamilyMembers = map[string][]string{
	"recording": {"record_audio", "asr", "send_file", "tts", "ask_user"},
	"audio":     {"record_audio", "asr", "tts"},
	"browser":   {"browser"},
	"ssh":       {"ssh"},
	"office":    {"office", "generate_pdf", "send_file"},
	"search":    {"web_search", "web_fetch", "download_file", "session_search", "memory"},
	"files":     {"read_file", "write_file", "edit_file", "list_directory", "bash", "download_file"},
	"memory":    {"memory"},
	"coding":    {"bash", "read_file", "write_file", "edit_file", "ripgrep", "Glob"},
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
	// Families are lowercase keys; tool names keep case (Glob, FileRead, …).
	intent.ToolFamilies = compactUniqueStrings(intent.ToolFamilies, true)
	intent.MustInclude = compactUniqueStrings(intent.MustInclude, false)
	intent.MustExclude = compactUniqueStrings(intent.MustExclude, false)
	if intent.Confidence < 0 {
		intent.Confidence = 0
	}
	if intent.Confidence > 1 {
		intent.Confidence = 1
	}
	// Empty useful payload → ignore.
	if intent.QueryForRoute == "" && len(intent.MustInclude) == 0 && len(intent.ToolFamilies) == 0 {
		return nil
	}
	// Models often omit confidence; treat missing (0) as mid-high trust when
	// they still produced a concrete rewrite payload.
	if intent.Confidence == 0 {
		intent.Confidence = 0.7
	}
	return intent
}

// compactUniqueStrings trims, optionally lowercases, and de-duplicates
// case-insensitively while preserving the first-seen spelling.
func compactUniqueStrings(in []string, lower bool) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if lower {
			s = strings.ToLower(s)
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

// Usable reports whether the intent should influence routing.
func (intent *RouteIntent) Usable() bool {
	if intent == nil {
		return false
	}
	if intent.Confidence < MinRouteIntentConfidence {
		return false
	}
	return intent.QueryForRoute != "" || len(intent.MustInclude) > 0 || len(intent.ToolFamilies) > 0
}

// HasStrongLocalRouteSignal is true when lexical detectors already pin tools
// with high confidence, so an LLM rewrite is unnecessary latency.
func HasStrongLocalRouteSignal(userMessage string) bool {
	return isExplicitRecordAudioRequest(userMessage) ||
		isExplicitScreenshotRequest(userMessage) ||
		isExplicitGitRequest(userMessage)
}

// SearchQuery returns the text to feed BM25/hybrid (falls back to userMessage).
func (intent *RouteIntent) SearchQuery(userMessage string) string {
	if intent != nil && intent.Usable() && intent.QueryForRoute != "" {
		return intent.QueryForRoute
	}
	return userMessage
}

// ExpandPins returns tool names to force-include this turn, intersected with
// availableTools when non-nil (case-insensitive match → canonical spelling).
func (intent *RouteIntent) ExpandPins(availableTools map[string]bool) []string {
	if intent == nil || !intent.Usable() {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = resolveAvailableToolName(name, availableTools)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, name := range intent.MustInclude {
		add(name)
	}
	for _, fam := range intent.ToolFamilies {
		for _, name := range toolFamilyMembers[fam] {
			add(name)
		}
		// Also allow family name itself as a tool (e.g. "browser", "office").
		add(fam)
	}
	// Intent label shortcuts when model omits must_include.
	switch intent.Intent {
	case "start_recording", "meeting_recording", "record_audio", "long_form_recording":
		add("record_audio")
	case "transcribe_audio", "asr":
		add("asr")
	case "browser":
		add("browser")
	case "ssh":
		add("ssh")
	}
	return out
}

// ExpandExcludes returns tool names to suppress this turn.
func (intent *RouteIntent) ExpandExcludes(availableTools map[string]bool) []string {
	if intent == nil || !intent.Usable() {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, name := range intent.MustExclude {
		name = resolveAvailableToolName(name, availableTools)
		if name == "" || seen[name] {
			continue
		}
		// Never exclude core safety/file surface via rewrite mistakes.
		if CoreToolNames[name] && name != "record_audio" && name != "asr" && name != "tts" {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// resolveAvailableToolName returns the canonical tool name present in
// availableTools (case-insensitive). If availableTools is nil, returns trimmed name.
func resolveAvailableToolName(name string, availableTools map[string]bool) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if availableTools == nil {
		return name
	}
	if availableTools[name] {
		return name
	}
	for k := range availableTools {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return ""
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
