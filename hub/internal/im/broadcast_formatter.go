package im

import (
	"fmt"
	"strings"
)

// FormatBroadcastReply formats multiple device replies into a structured
// text message with clear separators, deduplication, and summary stats.
func FormatBroadcastReply(replies []DeviceReply) string {
	var textParts []string
	var errorParts []string
	successCount := 0
	failCount := 0

	// Check for similar replies (first 100 chars).
	var goodReplies []DeviceReply
	for _, r := range replies {
		if r.Err == nil && r.Response != nil && r.Response.Body != "" {
			goodReplies = append(goodReplies, r)
		}
	}

	similar := areSimilarReplies(goodReplies)

	for _, r := range replies {
		if r.Err != nil {
			failCount++
			errorParts = append(errorParts, fmt.Sprintf("• %s: %v", r.Name, r.Err))
			continue
		}
		if r.Response == nil || r.Response.Body == "" {
			failCount++
			errorParts = append(errorParts, fmt.Sprintf("• %s: 超时未回复", r.Name))
			continue
		}

		successCount++

		if similar && successCount > 1 {
			// Skip duplicate replies.
			continue
		}

		textParts = append(textParts, fmt.Sprintf("━━ %s ━━\n%s", r.Name, r.Response.Body))
	}

	if similar && successCount > 1 {
		textParts = append(textParts, fmt.Sprintf("\n✅ 其他 %d 台设备观点一致", successCount-1))
	}

	// Append errors at the end.
	if len(errorParts) > 0 {
		textParts = append(textParts, fmt.Sprintf("⚠️ 异常设备:\n%s", strings.Join(errorParts, "\n")))
	}

	// Summary line.
	total := successCount + failCount
	summary := fmt.Sprintf("📊 参与: %d 台 | 成功: %d | 失败: %d", total, successCount, failCount)
	textParts = append(textParts, summary)

	return strings.Join(textParts, "\n\n")
}

func areSimilarReplies(replies []DeviceReply) bool {
	if len(replies) < 2 {
		return false
	}
	first := []rune(replies[0].Response.Body)
	prefixLen := 100
	if len(first) < prefixLen {
		prefixLen = len(first)
	}
	prefix := string(first[:prefixLen])
	for _, r := range replies[1:] {
		runes := []rune(r.Response.Body)
		cmpLen := prefixLen
		if len(runes) < cmpLen {
			cmpLen = len(runes)
		}
		if cmpLen != prefixLen || string(runes[:cmpLen]) != prefix {
			return false
		}
	}
	return true
}
