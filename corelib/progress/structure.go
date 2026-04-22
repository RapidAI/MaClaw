package progress

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// StructureSignal captures structural features of a message that are
// relevant for dispatch scheduling. These are syntactic/structural signals,
// not semantic — they don't require embedding or LLM calls.
type StructureSignal struct {
	Length      int  // rune count
	IsShort    bool // <5 runes
	IsMedium   bool // 5-30 runes
	IsLong     bool // >30 runes
	HasNegation bool // contains negation syntactic pattern
}

// AnalyzeStructure extracts structural signals from a message.
func AnalyzeStructure(text string) StructureSignal {
	text = strings.TrimSpace(text)
	runeCount := utf8.RuneCountInString(text)

	return StructureSignal{
		Length:      runeCount,
		IsShort:    runeCount < 5,
		IsMedium:   runeCount >= 5 && runeCount <= 30,
		IsLong:     runeCount > 30,
		HasNegation: DetectNegation(text),
	}
}

// DetectNegation checks whether the text contains Chinese or English negation
// syntactic patterns that indicate the user wants to stop/cancel/abandon.
//
// This is a SYNTACTIC check, not a SEMANTIC keyword list. Chinese negation
// has fixed grammatical structures (negation particle + sentence-final particle)
// that form a closed set. New expressions that follow these patterns are
// automatically covered.
//
// Patterns detected:
//   - Chinese negation + sentence-final: 不/别/没/算 + 了/吧/啦/呢
//   - Chinese negation imperative: 先不/暂时不/还是不/不要/不用/别再
//   - English negation: don't/stop/cancel/never mind/forget it
func DetectNegation(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}

	lower := strings.ToLower(text)

	// Chinese negation patterns.
	if chineseNegationRe.MatchString(text) {
		return true
	}

	// Chinese negation imperative prefixes.
	for _, prefix := range chineseNegationPrefixes {
		if strings.Contains(text, prefix) {
			return true
		}
	}

	// English negation patterns.
	for _, pattern := range englishNegationPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// chineseNegationRe matches the Chinese negation + sentence-final particle pattern.
// Structure: negation particle (不/别/没/算/莫/甭) + optional filler (0-4 chars) +
// sentence-final particle (了/吧/啦/呢/嘛/哦/噢).
//
// Examples matched:
//   - "算了" (forget it)
//   - "不做了" (not doing it anymore)
//   - "别搞了吧" (stop messing with it)
//   - "没必要了" (no need anymore)
//   - "不弄了" (not working on it)
//
// Examples NOT matched:
//   - "不错" (not bad — positive)
//   - "别人" (other people — not negation)
//   - "没有问题" (no problem — positive)
var chineseNegationRe = regexp.MustCompile(
	`[不别没算莫甭][^。？！\n]{0,4}[了吧啦呢嘛哦噢]`,
)

// chineseNegationPrefixes are imperative negation patterns.
// These are fixed grammatical constructions, not keywords.
//
// NOTE: These use strings.Contains (substring match), so short entries like
// "停" will match inside compound words ("停车场"). This is intentional —
// DetectNegation is a HIGH-RECALL signal detector, not a final decision maker.
// False positives are eliminated by the scheduler's multi-signal fusion
// (relevance + domain match override negation when the message is clearly
// a new task rather than a cancel command).
var chineseNegationPrefixes = []string{
	"先不", "暂时不", "还是不", "不要", "不用", "别再",
	"算了", "罢了", "得了", "行了",
	"取消", "停止", "中断", "放弃", "停",
}

// englishNegationPatterns are common English negation expressions.
var englishNegationPatterns = []string{
	"don't", "dont", "do not",
	"stop", "cancel", "abort",
	"never mind", "nevermind",
	"forget it", "forget about it",
	"no longer", "no need",
}
