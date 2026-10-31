package browser

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Article bodies must arrive in paragraphs. Two things flattened them:
// the model wrote long bodies as one block or with single line breaks, and
// a Markdown editor (CSDN) renders lines that are not separated by a blank
// line as ONE paragraph. Paragraphize puts a blank line between prose
// lines (code fences, tables, lists and quotes keep their own layout), and
// ParagraphProblem refuses a long body that has no breaks at all, so the
// model rewrites it instead of posting a wall of text.

var (
	paraListRe  = regexp.MustCompile(`^\s*([-*+•]|\d+[.)、])\s+`)
	paraTableRe = regexp.MustCompile(`^\s*\|`)
)

func paraBlockLine(t string) bool {
	return paraListRe.MatchString(t) || paraTableRe.MatchString(t) || strings.HasPrefix(strings.TrimSpace(t), ">")
}

// Paragraphize separates prose lines and headings with a blank line.
func Paragraphize(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	lines := strings.Split(md, "\n")
	var out []string
	fence := false
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		out = append(out, ln)
		if fence || t == "" || i+1 >= len(lines) {
			continue
		}
		nt := strings.TrimSpace(lines[i+1])
		if nt == "" {
			continue
		}
		// keep runs of list items / table rows / quote lines together
		if paraBlockLine(ln) && paraBlockLine(lines[i+1]) {
			continue
		}
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// ParagraphProblem names a long article body with no paragraph breaks.
func ParagraphProblem(body string) string {
	b := strings.TrimSpace(imgAnchorRe.ReplaceAllString(body, ""))
	n := utf8.RuneCountInString(b)
	if n < 400 || strings.Contains(b, "\n") || strings.HasPrefix(b, "<") {
		return ""
	}
	return fmt.Sprintf("the body is %d characters in ONE paragraph (no line breaks); rewrite it as paragraphs of 2-4 sentences separated by a blank line (\\n\\n), with a ## heading per section, then call again (nothing was filled or posted)", n)
}

// paraCount counts non-empty blocks separated by blank lines.
func paraCount(s string) int {
	n := 0
	for _, b := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(s), -1) {
		if strings.TrimSpace(b) != "" {
			n++
		}
	}
	return n
}

// MarkdownHTML is the light Markdown body as editor HTML: <p> paragraphs,
// <h2>/<h3> headings, <strong>, lists and quotes.
func MarkdownHTML(md string) string { return richMarkdownHTML(Paragraphize(md)) }

// MarkdownPlain is the body for an editor that keeps no formatting (note
// sites such as Xiaohongshu): markers removed, headings on their own line,
// list items as "• ", a blank line between paragraphs.
func MarkdownPlain(md string) string {
	return richHTMLPlain(richMarkdownHTML(Paragraphize(md)))
}

var mdMarkRe = regexp.MustCompile(`(?m)^\s*#{1,6}\s+\S|\*\*[^*\n]+\*\*|__[^_\n]+__|^\s*[-*]\s+\S|^\s*>\s?\S`)

// HasMarkdown reports whether the text carries Markdown markers.
func HasMarkdown(s string) bool { return mdMarkRe.MatchString(s) }

// MarkdownText is the visible text a rich editor shows for MarkdownHTML.
func MarkdownText(md string) string { return richHTMLText(MarkdownHTML(md)) }
