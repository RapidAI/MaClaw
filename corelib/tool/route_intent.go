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
}

// MinRouteIntentConfidence is the floor below which a rewrite is ignored.
const MinRouteIntentConfidence = 0.45

// MinCandidateRouteScore skips zero/noise candidates instead of padding the
// budget with irrelevant tools (computer_*, etc.).
const MinCandidateRouteScore = 1e-6

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
