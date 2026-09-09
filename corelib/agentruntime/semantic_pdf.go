package agentruntime

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

var semanticPDFReportDateRE = regexp.MustCompile(`^(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{4}年\d{1,2}月\d{1,2}日)$`)
var pdfReportTitleSuffixRE = regexp.MustCompile(`(?i)[,，、;；\s]*(?:请(?:帮我)?|帮我)?(?:并|and)?\s*(?:生成|generate)\s*(?:一份|a)?\s*pdf(?:\s*(?:报告|report))?[.。!！]*$`)

// NormalizePDFInvocationArgs closes the model-facing generate_pdf payload to
// the fields understood by the shared runtime.  content is required; title
// and doc_type remain when they are strings.  Decorative fields such as path,
// output and query are dropped only for substantive Markdown, while malformed
// or title-only payloads are returned unchanged so the normal schema gate can
// issue a correctable rejection instead of burning a one-shot grant.
func NormalizePDFInvocationArgs(argsJSON string) string {
	var parsed map[string]any
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return argsJSON
	}
	content := semanticPDFJSONString(parsed["content"])
	if content == "" || PDFLooksLikeTitleOnly(content) {
		return argsJSON
	}
	if date := semanticPDFReportDateString(parsed["date"]); date != "" && !strings.Contains(content, date) {
		content = "日期：" + date + "\n\n" + content
	}
	out := map[string]string{"content": content}
	if title := semanticPDFJSONString(parsed["title"]); title != "" {
		out["title"] = title
	}
	if docType := semanticPDFJSONString(parsed["doc_type"]); docType != "" {
		out["doc_type"] = docType
	}
	body, err := json.Marshal(out)
	if err != nil {
		return argsJSON
	}
	return string(body)
}

// PDFArgsTooThin reports whether a generate_pdf request contains only a title
// and no report body.  Hosts should reject this before consuming the grant.
func PDFArgsTooThin(argsJSON string) bool {
	var parsed map[string]any
	if json.Unmarshal([]byte(argsJSON), &parsed) != nil || parsed == nil {
		return false
	}
	content := semanticPDFJSONString(parsed["content"])
	if PDFLooksLikeTitleOnly(content) {
		return true
	}
	line, hasBody := PDFTitleLine(content)
	title := semanticPDFJSONString(parsed["title"])
	return !hasBody && title != "" && line != "" && strings.EqualFold(line, title) &&
		!PDFHasReportBodySignal(line)
}

// PDFTitleLine extracts the first Markdown line and reports whether any body
// text follows it.
func PDFTitleLine(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", false
	}
	line := content
	hasBody := false
	if idx := strings.Index(content, "\n"); idx >= 0 {
		line = strings.TrimSpace(content[:idx])
		hasBody = strings.TrimSpace(content[idx+1:]) != ""
	}
	if strings.HasPrefix(line, "#") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	}
	return line, hasBody
}

// PDFLooksLikeTitleOnly recognizes short report titles that smaller models
// emit instead of actual content.
func PDFLooksLikeTitleOnly(content string) bool {
	line, hasBody := PDFTitleLine(content)
	if hasBody || line == "" || PDFHasReportBodySignal(line) {
		return false
	}
	n := len([]rune(line))
	if n > 24 {
		return false
	}
	lower := strings.ToLower(line)
	return n <= 8 || strings.HasSuffix(line, "报告") || strings.HasSuffix(line, "纪要") ||
		strings.HasSuffix(line, "文档") || strings.HasSuffix(lower, "report") || strings.HasSuffix(lower, "pdf")
}

// PDFHasReportBodySignal detects punctuation, units, or numbers that make a
// short first line look like actual report content rather than a title.
func PDFHasReportBodySignal(content string) bool {
	return strings.ContainsAny(content, "。！？;；") || strings.Contains(content, "℃") ||
		strings.Contains(content, "°") || strings.ContainsAny(content, "0123456789")
}

// PDFReportDateString returns a bounded ISO or Chinese calendar date.  Other
// values are intentionally ignored so decorative model fields cannot be
// folded into report content.
func PDFReportDateString(value any) string {
	return semanticPDFReportDateString(value)
}

// HostOwnedPDFReportTitle derives a bounded title from the user's request
// after removing a trailing "generate PDF" instruction. It accepts already
// projected intent text so GUI and headless hosts can share the title policy
// without importing a host-specific message classifier.
func HostOwnedPDFReportTitle(intentText string) string {
	text := strings.TrimSpace(intentText)
	for {
		next := strings.TrimSpace(pdfReportTitleSuffixRE.ReplaceAllString(text, ""))
		next = strings.Trim(next, "，。,;； ")
		if next == text {
			break
		}
		text = next
	}
	if text == "" {
		return "报告"
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return text
}

// SubstantialPDFReportText keeps title-only or acknowledgement fragments out
// of host-owned PDF synthesis. The threshold is intentionally conservative;
// callers still run the full PDF content validator after this gate.
func SubstantialPDFReportText(text string) bool {
	return len([]rune(strings.TrimSpace(text))) >= 80
}

// HostOwnedPDFReportContent synthesizes the Markdown body for a host-owned
// PDF report. Trusted lookup evidence wins and is wrapped in a report heading;
// otherwise the assistant text (with deferred PDF promises and XML tool calls
// stripped) is used when it is substantial. Either candidate must still pass
// the shared PDF content validator, so a host can never emit a document the
// renderer would reject.
func HostOwnedPDFReportContent(assistantText, searchEvidence, title string) string {
	if evidence := TrustedLookupEvidence(searchEvidence); evidence != "" {
		heading := strings.TrimSpace(title)
		if heading == "" {
			heading = "报告"
		}
		body := "# " + heading + "\n\n" + evidence
		if swarm.ValidatePDFContent(body) == nil {
			return body
		}
	}
	cleaned := strings.TrimSpace(llm.StripXMLToolCalls(StripDeferredPDFPromise(assistantText)))
	if SubstantialPDFReportText(cleaned) && swarm.ValidatePDFContent(cleaned) == nil {
		return cleaned
	}
	return ""
}

func semanticPDFJSONString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func semanticPDFReportDateString(value any) string {
	text := semanticPDFJSONString(value)
	if text == "" || len([]rune(text)) > 32 || !semanticPDFReportDateRE.MatchString(text) {
		return ""
	}
	return text
}
