package agentruntime

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// hostPDFToolID is the adapter the host satisfies when it publishes a PDF.
// A sentence that cites this id is tool status. The id does not change with
// the language of the surrounding sentence.
const hostPDFToolID = "generate_pdf"

// OmitHostPDFToolStatus drops tool-status sentences from assistant text that
// may become a PDF body. Report sentences stay. The host's own malformed-tool
// notice is removed as a whole, because that string is host copy. Text that
// cites neither is returned unchanged, aside from trimming the ends, so a
// finished forecast is not rebuilt sentence by sentence.
func OmitHostPDFToolStatus(text string) string {
	if strings.Contains(text, llm.MalformedContentToolCallErrorMsg) {
		text = strings.ReplaceAll(text, llm.MalformedContentToolCallErrorMsg, "")
	}
	if !strings.Contains(strings.ToLower(text), hostPDFToolID) {
		return strings.TrimSpace(text)
	}
	return dropHostPDFToolSentences(text)
}

// ProjectHostPublishedPDFChat is the visible reply after the host has
// published the PDF. Sentences that cite the generate_pdf tool id are
// removed. Whatever the model wrote besides that is the reply, including a
// forecast. An empty result tells the caller to use its own receipt.
func ProjectHostPublishedPDFChat(assistantText string) string {
	return OmitHostPDFToolStatus(assistantText)
}

// dropHostPDFToolSentences removes sentences that cite the tool id. A blank
// line stays a paragraph break so the surrounding report is not glued together.
func dropHostPDFToolSentences(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		var parts []string
		for _, sentence := range splitHostPDFSentences(line) {
			if strings.TrimSpace(sentence) == "" || hostPDFSentenceCitesTool(sentence) {
				continue
			}
			parts = append(parts, strings.TrimSpace(sentence))
		}
		if len(parts) == 0 {
			continue
		}
		lines = append(lines, strings.Join(parts, ""))
	}
	return joinHostPDFLines(lines)
}

// joinHostPDFLines keeps a single blank line between paragraphs and drops
// blank lines at the ends.
func joinHostPDFLines(lines []string) string {
	var b strings.Builder
	blank := false
	started := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blank = started
			continue
		}
		if started {
			if blank {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		started = true
		blank = false
		b.WriteString(line)
	}
	return b.String()
}

func hostPDFSentenceCitesTool(sentence string) bool {
	return strings.Contains(strings.ToLower(sentence), hostPDFToolID)
}

func splitHostPDFSentences(line string) []string {
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
