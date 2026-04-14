package im

import (
	"strings"
	"unicode/utf8"
)

// FilterResult represents the outcome of the QuickFilter's message triage.
type FilterResult int

const (
	FilterCommand              FilterResult = iota // starts with /
	FilterActiveWorkflow                           // user has an active workflow
	FilterActiveUnderstanding                      // user has an active understanding session
	FilterSmallTalk                                // greeting / short chitchat
	FilterSimpleDirective                          // translate / format / summarize etc.
	FilterNeedsUnderstanding                       // complex task → enter intent understanding
)

// ActiveSessionChecker is used by QuickFilter to check whether a user has
// an active workflow or understanding session without importing WorkflowEngine.
type ActiveSessionChecker interface {
	HasActiveWorkflow(userID string) bool
	HasActiveUnderstanding(userID string) bool
}

// QuickFilter performs fast rule-based message triage (no I/O, <5ms).
type QuickFilter struct {
	checker ActiveSessionChecker
}

// NewQuickFilter creates a QuickFilter with the given session checker.
func NewQuickFilter(checker ActiveSessionChecker) *QuickFilter {
	return &QuickFilter{checker: checker}
}

// Filter classifies a user message into one of the FilterResult categories.
func (qf *QuickFilter) Filter(userID, text string) FilterResult {
	text = strings.TrimSpace(text)

	// 1. Command messages
	if strings.HasPrefix(text, "/") {
		return FilterCommand
	}

	// 2. Active workflow takes priority
	if qf.checker != nil && qf.checker.HasActiveWorkflow(userID) {
		return FilterActiveWorkflow
	}

	// 3. Active understanding session
	if qf.checker != nil && qf.checker.HasActiveUnderstanding(userID) {
		return FilterActiveUnderstanding
	}

	// 4. Small talk
	if isSmallTalk(text) {
		return FilterSmallTalk
	}

	// 5. Simple directive
	if isSimpleDirective(text) {
		return FilterSimpleDirective
	}

	// 6. Everything else needs understanding
	return FilterNeedsUnderstanding
}

// smallTalkWords are greetings and filler words that indicate small talk.
var smallTalkWords = []string{
	"你好", "您好", "谢谢", "感谢", "嗯", "哦", "好的", "好", "是的",
	"对", "行", "可以", "没问题", "了解", "明白", "收到",
	"hi", "hello", "hey", "thanks", "thank you", "ok", "okay",
	"yes", "no", "yeah", "yep", "nope", "bye", "goodbye",
	"早", "早上好", "晚上好", "下午好", "晚安",
}

// smallTalkSet is a pre-built set for O(1) lookup.
var smallTalkSet = func() map[string]bool {
	m := make(map[string]bool, len(smallTalkWords))
	for _, w := range smallTalkWords {
		m[w] = true
	}
	return m
}()

// isSmallTalk returns true for short greeting/filler messages.
// Only exact matches against the known word list are considered small talk.
func isSmallTalk(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	// Only short messages can be small talk — long messages are never greetings.
	if utf8.RuneCountInString(lower) > 15 {
		return false
	}
	return smallTalkSet[lower]
}

// simpleDirectivePrefixes are task prefixes that indicate a simple,
// single-step directive that doesn't need a multi-phase workflow.
var simpleDirectivePrefixes = []string{
	"翻译", "格式化", "总结", "整理", "转换",
	"解释", "计算", "查询", "搜索", "查找",
}

// isSimpleDirective returns true if the text starts with a known simple
// directive keyword, indicating a task that can be handled without a
// multi-phase workflow.
func isSimpleDirective(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range simpleDirectivePrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
