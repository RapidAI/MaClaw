package agentruntime

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

var semanticPDFReportDateRE = regexp.MustCompile(`^\d{4}[-/.]\d{1,2}[-/.]\d{1,2}$`)

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
		content = date + "\n\n" + content
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

// PDFLooksLikeTitleOnly recognizes a short first line with no body. The
// line's words are not read. Punctuation, units, or digits mark it as content.
func PDFLooksLikeTitleOnly(content string) bool {
	line, hasBody := PDFTitleLine(content)
	if hasBody || line == "" || PDFHasReportBodySignal(line) {
		return false
	}
	return len([]rune(line)) <= 24
}

// PDFHasReportBodySignal detects a sentence ending, a unit, or a number.
// The line's words are not read. An ASCII full stop counts only at the end
// of a clause, so a dotted token such as a file name stays a title.
func PDFHasReportBodySignal(content string) bool {
	runes := []rune(content)
	for i, r := range runes {
		switch {
		case r >= '0' && r <= '9', r == '℃', r == '°':
			return true
		case r == '。' || r == '！' || r == '？' || r == '；' || r == '!' || r == '?' || r == ';':
			return true
		case r == '.' && (i+1 >= len(runes) || unicode.IsSpace(runes[i+1])):
			return true
		}
	}
	return false
}

// PDFReportDateString returns a numeric year-month-day.  The separators are
// '-', '/' or '.'.  Other values are ignored so a decorative field cannot
// be folded into report content.
func PDFReportDateString(value any) string {
	return semanticPDFReportDateString(value)
}

// HostOwnedPDFReportTitle is the first clause of the request, at most 40
// runes. The clause ends at the earliest break. A later clause is not read.
func HostOwnedPDFReportTitle(intentText string) string {
	text := strings.TrimSpace(intentText)
	if text == "" {
		return ""
	}
	if i := hostPDFClauseBreak(text); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	text = strings.Trim(text, "。.!！?？ ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return text
}

// hostPDFClauseBreak is the byte index of the earliest clause mark. Newlines,
// commas, and semicolons count. A later mark does not win because it was
// listed first.
func hostPDFClauseBreak(text string) int {
	for i, r := range text {
		if r == '\n' || r == '\r' || hostPDFClauseMark(r) {
			return i
		}
	}
	return -1
}

func hostPDFClauseMark(r rune) bool {
	switch r {
	case ',', ';',
		'，', '、', '；',
		'،', '؛':
		return true
	default:
		return false
	}
}

// SubstantialPDFReportText keeps title-only or acknowledgement fragments out
// of host-owned PDF synthesis. The threshold is intentionally conservative;
// callers still run the full PDF content validator after this gate.
func SubstantialPDFReportText(text string) bool {
	return len([]rune(strings.TrimSpace(text))) >= 80
}

// HostOwnedPDFReportContent synthesizes the Markdown body for a host-owned
// PDF report. Trusted lookup evidence wins and is wrapped in a report heading.
// Otherwise the assistant text is used when it is substantial, after XML tool
// calls and sentences that cite the host PDF tool are removed. Either
// candidate must still pass the shared PDF content validator.
func HostOwnedPDFReportContent(assistantText, searchEvidence, title string) string {
	if evidence := TrustedLookupEvidence(searchEvidence); evidence != "" {
		heading := strings.TrimSpace(title)
		body := evidence
		if heading != "" {
			body = "# " + heading + "\n\n" + evidence
		}
		if swarm.ValidatePDFContent(body) == nil {
			return body
		}
	}
	cleaned := strings.TrimSpace(llm.StripXMLToolCalls(OmitHostPDFToolStatus(assistantText)))
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
