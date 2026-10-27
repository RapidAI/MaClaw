package guiapp

// coding_workbench_step_note.go — the checklist note is an annotation, not the
// step's report.
//
// A step result carries the SubAgent's whole final answer: report headings,
// tables, multi-paragraph prose. That payload belongs to the transcript and to
// the turn's final answer. The live plan checklist draws each step as one row
// above the composer, so its note must stay a short, single-line outcome.
// Writing the raw result there made one row swallow the report body — headings,
// bullet lists and emphasis markers included.
//
// Every writer of codingWorkbenchStepStatus.Summary projects through
// codingStepChecklistNote. The same projection runs when a step is serialized,
// when persisted steps are loaded, and when they leave the backend, so older
// snapshots that already hold a report body heal instead of keeping the
// polluted row.

import (
	"regexp"
	"strings"
)

// codingStepNoteMaxRunes bounds one row's note. 160 runes is roughly two
// wrapped CJK lines at checklist width; anything longer stops reading as a note
// and starts competing with the transcript.
const codingStepNoteMaxRunes = 160

var (
	// Paired emphasis spans only, and the span may not start with a path slash
	// or whitespace, nor directly follow an ASCII word character: a bare `**` is
	// a glob (`**/*.go`), a pointer (`int**`), or an exponent (`x**2`), and those
	// must survive. The leading boundary is captured so it is put back.
	codingStepNoteBoldPattern   = regexp.MustCompile(`(^|[^A-Za-z0-9])\*\*([^*/ \t](?:[^*]*[^ \t])?)\*\*`)
	codingStepNoteCodePattern   = regexp.MustCompile("`([^`]+)`")
	codingStepNoteStrikePattern = regexp.MustCompile(`~~([^~]+)~~`)
	codingStepNoteLinkPattern   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
)

// codingStepNoteCodePlaceholder protects a code span's body from emphasis
// stripping: `a**b` inside backticks is code, not markup.
const codingStepNoteCodePlaceholder = "\x00"

// codingStepChecklistNote projects a step result onto the note the checklist
// shows under the step title. The first line that states an outcome wins:
// report headings, rules, table rows and fenced code are structure, not a
// result. The note is single-line because the checklist row is one bounded
// annotation, and markdown is removed because the row renders plain text.
func codingStepChecklistNote(summary string) string {
	// A leading BOM is invisible in the transcript but would become the note's
	// first rune and shift the checklist row.
	summary = strings.TrimPrefix(strings.TrimSpace(summary), "\uFEFF")
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	inFence := false
	for _, rawLine := range strings.Split(summary, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		// A heading can sit behind a blockquote or a list marker; strip that
		// scaffolding before deciding whether the line states an outcome.
		line = codingStepNoteStripPrefix(line)
		if line == "" || codingStepNoteLineIsStructure(line) {
			continue
		}
		note := codingStepNoteStripInline(line)
		note = strings.Join(strings.Fields(note), " ")
		// A first line that ends with a colon is introducing the rest of the
		// report; the dangling punctuation has nothing to introduce on the row.
		note = strings.TrimRight(note, "：:")
		note = strings.TrimSpace(note)
		if note == "" {
			continue
		}
		return truncateCodingStepNote(note)
	}
	return ""
}

// codingStepNoteLineIsStructure reports markdown lines that frame a report
// rather than state what the step did. A report often opens with a heading or
// a table; showing that as the row note would say nothing about the outcome.
func codingStepNoteLineIsStructure(line string) bool {
	if strings.HasPrefix(line, "|") || strings.HasPrefix(line, "<!--") {
		return true
	}
	hashes := len(line) - len(strings.TrimLeft(line, "#"))
	if hashes >= 1 && hashes <= 6 {
		rest := line[hashes:]
		if rest == "" || strings.HasPrefix(rest, " ") || strings.HasPrefix(rest, "\t") {
			return true
		}
	}
	runes := []rune(line)
	if len(runes) < 3 {
		return false
	}
	for _, r := range runes {
		switch r {
		case '=', '-', '*', '_':
		default:
			return false
		}
	}
	// Thematic break or setext underline — both are decoration.
	return true
}

// codingStepNoteStripPrefix removes blockquote and list scaffolding. Step
// results are frequently written as a bullet list; the bullet character itself
// is not part of the note.
func codingStepNoteStripPrefix(line string) string {
	for {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, ">"):
			// `>=` and `>90%` are comparisons, not blockquotes; markdown
			// blockquotes put a space after the marker.
			rest := line[1:]
			if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
				return line
			}
			line = strings.TrimSpace(rest)
		case codingStepNoteHasListMarker(line):
			line = strings.TrimSpace(line[2:])
		default:
			if rest, ok := codingStepNoteStripCheckbox(line); ok {
				line = rest
				continue
			}
			if rest, ok := codingStepNoteStripOrderedMarker(line); ok {
				line = rest
				continue
			}
			return line
		}
	}
}

func codingStepNoteHasListMarker(line string) bool {
	if len(line) < 2 {
		return false
	}
	if line[0] != '-' && line[0] != '*' && line[0] != '+' {
		return false
	}
	return line[1] == ' ' || line[1] == '\t'
}

func codingStepNoteStripCheckbox(line string) (string, bool) {
	if len(line) < 3 || line[0] != '[' {
		return line, false
	}
	switch line[1] {
	case 'x', 'X', ' ':
	default:
		return line, false
	}
	if line[2] != ']' {
		return line, false
	}
	if len(line) > 3 && line[3] != ' ' && line[3] != '\t' {
		return line, false
	}
	return strings.TrimSpace(line[3:]), true
}

func codingStepNoteStripOrderedMarker(line string) (string, bool) {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	// Long runs are more likely a number than a list marker ("2026." is a year).
	if i == 0 || i > 2 || i >= len(line) {
		return line, false
	}
	// "1、" is the common Chinese ordered-list marker and carries no space.
	if strings.HasPrefix(line[i:], "、") {
		return strings.TrimSpace(line[i+len("、"):]), true
	}
	if line[i] != '.' && line[i] != ')' {
		return line, false
	}
	rest := line[i+1:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return line, false
	}
	return strings.TrimSpace(rest), true
}

func codingStepNoteStripInline(line string) string {
	var codeSpans []string
	line = codingStepNoteCodePattern.ReplaceAllStringFunc(line, func(match string) string {
		codeSpans = append(codeSpans, strings.Trim(match, "`"))
		return codingStepNoteCodePlaceholder
	})
	line = codingStepNoteLinkPattern.ReplaceAllString(line, "$1")
	line = codingStepNoteBoldPattern.ReplaceAllString(line, "$1$2")
	line = codingStepNoteStrikePattern.ReplaceAllString(line, "$1")
	for _, span := range codeSpans {
		line = strings.Replace(line, codingStepNoteCodePlaceholder, span, 1)
	}
	return line
}

// truncateCodingStepNote cuts at a sentence boundary when one falls in the
// second half, otherwise at the rune budget. The ellipsis is the only marker:
// the model-facing "…（已截断）" suffix would itself become report noise in a
// progress row.
func truncateCodingStepNote(note string) string {
	runes := []rune(note)
	if len(runes) <= codingStepNoteMaxRunes {
		return note
	}
	cut := runes[:codingStepNoteMaxRunes]
	for i := len(cut) - 1; i >= len(cut)/2; i-- {
		if codingStepNoteIsSentenceEnd(runes, i) {
			return string(cut[:i+1])
		}
	}
	return strings.TrimRight(string(cut), " \t,，、;；:：") + "…"
}

// codingStepNoteIsSentenceEnd accepts CJK terminators as written, but an ASCII
// terminator only when a word boundary follows it: "v2.5" and "a.b" are not
// sentence ends, so a note must not be cut in the middle of one.
func codingStepNoteIsSentenceEnd(runes []rune, i int) bool {
	switch runes[i] {
	case '。', '！', '？', '；':
		return true
	case '.', '!', '?', ';':
		return i+1 >= len(runes) || runes[i+1] == ' '
	}
	return false
}
