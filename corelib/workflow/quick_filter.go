package workflow

import (
	"strings"
	"unicode/utf8"
)

// WorkflowChecker is the minimal interface QuickFilter needs from the engine.
type WorkflowChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// QuickFilter classifies incoming user messages. Its ONLY job is to fast-reject
// messages that are definitively NOT workflow candidates (small talk, commands,
// empty). Everything else goes to LLM for accurate classification.
//
// Design principle: accuracy > speed. We'd rather spend 2-5s on an LLM call
// than misclassify a workflow request as a simple directive (or vice versa).
type QuickFilter struct {
	engine WorkflowChecker
}

// NewQuickFilter creates a QuickFilter with the given engine reference.
func NewQuickFilter(engine WorkflowChecker) *QuickFilter {
	return &QuickFilter{engine: engine}
}

// Classify determines the FilterResult for a user message.
//
// The classification is deliberately conservative: only small talk and
// active sessions are handled here. ALL other messages are routed to
// FilterNeedsUnderstanding so the LLM can make the final decision on
// whether this is a workflow task and which template to use.
//
// Priority (highest to lowest):
//  1. active_workflow       — user has an active workflow
//  2. active_understanding  — user has an active understanding session
//  3. small_talk            — short message with greeting/thanks/farewell words
//  4. needs_understanding   — everything else → LLM decides
func (f *QuickFilter) Classify(userID, text string) FilterResult {
	// 1. Active session checks (highest priority)
	if f.engine != nil {
		if f.engine.HasActiveWorkflow(userID) {
			return FilterActiveWorkflow
		}
		if f.engine.HasActiveUnderstanding(userID) {
			return FilterActiveUnderstanding
		}
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return FilterSimpleDirective
	}

	runeCount := utf8.RuneCountInString(trimmed)

	// 2. Small talk detection — short message + greeting/thanks/farewell words.
	// These are definitively NOT workflow requests. Fast-reject to avoid
	// wasting an LLM call on "你好" or "谢谢".
	if runeCount <= smallTalkMaxRunes && isSmallTalk(trimmed) {
		return FilterSmallTalk
	}

	// 3. Everything else → LLM decides.
	// The LLM will determine:
	//   a) Is this a workflow task or a simple directive?
	//   b) If workflow, which template?
	//   c) Structured intent extraction (goals, constraints, etc.)
	return FilterNeedsUnderstanding
}

// ---------------------------------------------------------------------------
// Small talk detection
// ---------------------------------------------------------------------------

const smallTalkMaxRunes = 15

// smallTalkExact are words that only count as small talk when the entire
// message matches (after lowercasing and trimming). Single-character words
// and short affirmations are too ambiguous for substring matching.
var smallTalkExact = []string{
	"好", "行", "嗯", "哦", "早", "拜",
	"好的", "ok", "okay",
}

// smallTalkWords are common Chinese greetings, time queries, thanks, farewells.
// These use substring matching (safe because they are specific enough to
// avoid false positives in short messages ≤15 runes).
var smallTalkWords = []string{
	// Greetings
	"你好", "您好", "嗨", "嘿", "哈喽", "hello", "hi", "hey",
	"早上好", "上午好", "中午好", "下午好", "晚上好", "晚安",
	"早安", "午安",
	// Thanks
	"谢谢", "感谢", "多谢", "谢了", "thanks", "thank you", "thx",
	// Time / weather
	"几点了", "几点", "什么时间", "现在几点", "今天天气", "天气怎么样", "天气如何",
	// Farewells
	"再见", "拜拜", "bye",
	// Fillers
	"在吗", "在不在",
}

func isSmallTalk(text string) bool {
	lower := strings.ToLower(text)
	// Exact match for single-character / ambiguous words
	for _, w := range smallTalkExact {
		if lower == w {
			return true
		}
	}
	// Substring match for multi-character specific words
	for _, w := range smallTalkWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers (kept for use by other modules)
// ---------------------------------------------------------------------------

// containsAny returns true if text contains any of the given substrings.
func containsAny(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// substantialInputMinLen is the minimum character count for user text to be
// considered "substantial" input (i.e., likely a pasted document or detailed
// description rather than a short command like "好的" or "开工").
const substantialInputMinLen = 50

// isSubstantialInput returns true if the text is likely a document upload
// or pasted content rather than a short conversational reply.
func isSubstantialInput(text string) bool {
	if len(text) >= substantialInputMinLen {
		return true
	}
	lower := strings.ToLower(text)
	fileIndicators := []string{".pdf", ".docx", ".doc", ".xlsx", ".xls", ".png", ".jpg", ".jpeg"}
	for _, ext := range fileIndicators {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	uploadIndicators := []string{"已上传", "上传了", "发给你", "文件在", "附件", "请查看", "请看"}
	for _, ind := range uploadIndicators {
		if strings.Contains(text, ind) {
			return true
		}
	}
	return false
}
