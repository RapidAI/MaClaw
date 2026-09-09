package agentruntime

import (
	"strings"
)

// StripDeferredPDFPromise removes model text that only promises a future PDF
// generation (for example "请稍候，我将生成 PDF").  Hosts call this after a
// trusted artifact has actually been published so the visible response does
// not claim a pending action or expose a failed authorization attempt.
func StripDeferredPDFPromise(text string) string {
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		remainder, drop := stripDeferredPDFPromiseLine(line)
		if drop {
			if remainder == "" {
				continue
			}
			kept = append(kept, remainder)
			continue
		}
		kept = append(kept, raw)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func stripDeferredPDFPromiseLine(line string) (string, bool) {
	if line == "" {
		return "", false
	}
	if remainder, drop := stripFailedPDFAuthorizationExcuseLine(line); drop {
		if remainder == "" {
			return "", true
		}
		cleaned, _ := stripDeferredPDFPromiseLine(remainder)
		return cleaned, true
	}
	if WaitOnlyDeferredPDFLine(line) {
		return "", true
	}
	if idx := deferredPDFLeadInIndex(line); idx >= 0 {
		return strings.TrimSpace(strings.Trim(line[:idx], "，。,;； ")), true
	}
	if deferredPDFPromiseLine(line) {
		if idx := deferredWaitMarkerIndex(line); idx > 0 {
			prefix := strings.TrimSpace(strings.Trim(line[:idx], "，。,;； "))
			if prefix != "" && !deferredPDFPromiseLine(prefix) && !WaitOnlyDeferredPDFLine(prefix) {
				return prefix, true
			}
		}
		return "", true
	}
	return line, false
}

func deferredPDFLeadInIndex(line string) int {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "pdf") {
		return -1
	}
	best := -1
	for _, lead := range []string{"接下来我将", "接下来我会"} {
		idx := strings.Index(line, lead)
		if idx >= 0 && strings.Contains(lower[idx:], "pdf") && (best < 0 || idx < best) {
			best = idx
		}
	}
	for _, lead := range []string{"i will", "i'll", "i am going to", "let me generate"} {
		idx := strings.Index(lower, lead)
		if idx >= 0 && strings.Contains(lower[idx:], "pdf") && (best < 0 || idx < best) {
			best = idx
		}
	}
	return best
}

func deferredWaitMarkerIndex(line string) int {
	lower := strings.ToLower(line)
	best := -1
	for _, marker := range []string{"请稍候", "请稍后", "请稍等"} {
		if idx := strings.Index(line, marker); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if idx := strings.Index(lower, "please wait"); idx >= 0 && (best < 0 || idx < best) {
		best = idx
	}
	return best
}

func deferredPDFPromiseLine(line string) bool {
	if line == "" {
		return false
	}
	lower := strings.ToLower(line)
	mentionsPDF := strings.Contains(lower, "pdf")
	mentionsGenerateReport := strings.Contains(line, "生成") && strings.Contains(line, "报告")
	hasWait := strings.Contains(line, "请稍候") || strings.Contains(line, "请稍后") || strings.Contains(lower, "please wait")
	if hasWait && (mentionsPDF || mentionsGenerateReport) {
		return true
	}
	if strings.Contains(line, "接下来我将") && mentionsPDF {
		return true
	}
	return WaitOnlyDeferredPDFLine(line)
}

func stripFailedPDFAuthorizationExcuseLine(line string) (string, bool) {
	if !FailedPDFAuthorizationExcuseLine(line) {
		return line, false
	}
	var kept []string
	for _, sentence := range splitPDFExcuseSentences(line) {
		if !FailedPDFAuthorizationExcuseLine(sentence) {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "")), true
}

func splitPDFExcuseSentences(line string) []string {
	var out []string
	var current strings.Builder
	for _, r := range line {
		current.WriteRune(r)
		if r == '。' || r == '！' || r == '？' {
			out = append(out, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		out = append(out, current.String())
	}
	return out
}

// FailedPDFAuthorizationExcuseLine identifies text that reports a failed or
// unauthorized PDF call.  It is intentionally marker-based and provider
// agnostic so every host applies the same visible-text projection.
func FailedPDFAuthorizationExcuseLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(trimmed, "无法解析的工具调用") || strings.Contains(trimmed, "已拦截原始工具 XML") {
		return true
	}
	if strings.Contains(trimmed, "PDF生成失败") || strings.Contains(lower, "pdf generation failed") {
		return true
	}
	if strings.Contains(trimmed, "无法生成PDF") || strings.Contains(trimmed, "无法成功生成PDF") {
		return true
	}
	mentionsPDF := strings.Contains(lower, "generate_pdf") || strings.Contains(lower, "pdf")
	if mentionsPDF && (strings.Contains(trimmed, "无法直接生成") || strings.Contains(trimmed, "暂无法直接生成")) {
		return true
	}
	if mentionsPDF && (strings.Contains(trimmed, "工具列表中没有") || strings.Contains(trimmed, "没有 PDF 生成工具") ||
		strings.Contains(trimmed, "没有PDF生成工具") || strings.Contains(trimmed, "请重新发起生成") || strings.Contains(trimmed, "授权工具出现")) {
		return true
	}
	if strings.Contains(trimmed, "如需PDF报告") && (strings.Contains(trimmed, "授权") || strings.Contains(trimmed, "下一轮") ||
		strings.Contains(trimmed, "整理为") || strings.Contains(trimmed, "文本格式") || strings.Contains(trimmed, "其他方式") || strings.Contains(lower, "re-authorize")) {
		return true
	}
	if !mentionsPDF {
		return false
	}
	return strings.Contains(trimmed, "未授权") || strings.Contains(trimmed, "重新授权") || strings.Contains(trimmed, "工具调用失败") ||
		strings.Contains(trimmed, "参数格式无效") || strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "not allowed by the current execution policy")
}

// WaitOnlyDeferredPDFLine reports a line containing no user-facing content
// beyond a generic wait marker.
func WaitOnlyDeferredPDFLine(line string) bool {
	trimmed := strings.Trim(strings.TrimSpace(line), "。.~～…!！")
	switch strings.ToLower(trimmed) {
	case "请稍候", "请稍后", "请稍等", "稍候", "稍后", "稍等", "please wait", "wait a moment", "one moment":
		return true
	default:
		return false
	}
}
