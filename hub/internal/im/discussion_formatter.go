package im

import (
	"fmt"
	"strings"
)

// FormatRoundSummary generates a brief one-line-per-device summary of a
// discussion round's results.
func FormatRoundSummary(round int, results []discussionRoundResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📝 第 %d 轮小结：\n", round)
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(&b, "• %s: ❌ 错误\n", r.Name)
		} else if r.Text == "" {
			fmt.Fprintf(&b, "• %s: ⏰ 超时\n", r.Name)
		} else {
			summary := r.Text
			runes := []rune(summary)
			if len(runes) > 80 {
				// Take first sentence or first 80 runes.
				if idx := strings.IndexAny(summary, "。.！!？?\n"); idx > 0 && idx < len(summary) {
					// Ensure the sentence boundary is within ~120 runes.
					sentRunes := []rune(summary[:idx+1])
					if len(sentRunes) <= 120 {
						summary = string(sentRunes)
					} else {
						summary = string(runes[:80]) + "…"
					}
				} else {
					summary = string(runes[:80]) + "…"
				}
			}
			fmt.Fprintf(&b, "• %s: %s\n", r.Name, summary)
		}
	}
	return b.String()
}

// FormatDiscussionSummary generates a structured discussion summary with
// consensus, divergence, and pending sections. This is used as the prompt
// guidance for the summarizer device.
func FormatDiscussionSummary(topic string, allRoundTexts []string) string {
	return fmt.Sprintf("话题: %s\n\n讨论记录:\n%s\n\n"+
		"请生成结构化总结，分为以下三部分：\n"+
		"【共识点】各参与者达成一致的观点\n"+
		"【分歧点】各参与者意见不同的地方\n"+
		"【待定事项】需要进一步确认或行动的事项\n\n"+
		"总结不超过 500 字。",
		topic, strings.Join(allRoundTexts, "\n\n"))
}
