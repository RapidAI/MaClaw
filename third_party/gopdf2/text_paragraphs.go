package gopdf

import (
	"os"
	"strings"
)

// ============================================================
// Paragraph reconstruction — merges reading-order text lines back into
// paragraphs. Handles the classic wrapped-text patterns: line breaks
// inside sentences, hyphenated splits ("transduc-/tion"), first-line
// indents and paragraph spacing.
// ============================================================

// TextParagraph is a reconstructed paragraph.
type TextParagraph struct {
	// X, Y are the top-left coordinates of the paragraph.
	X, Y float64
	// Width, Height span all lines of the paragraph.
	Width, Height float64
	// Lines are the merged lines in reading order.
	Lines []TextLine
	// Text is the paragraph text: lines joined with spaces, hyphenated
	// splits joined without the hyphen.
	Text string
}

// paragraphMaxGap is the largest vertical gap (× line height) between two
// lines of the same paragraph. Intra-paragraph leading adds 0.1-0.3 em of
// space between line boxes; paragraph spacing is typically ≥ 0.6 em.
const paragraphMaxGap = 0.55

// paragraphIndentFactor: a line starting this much further right (× height)
// than the paragraph's left edge starts a new (indented) paragraph.
const paragraphIndentFactor = 1.2

// lineBoxHeight returns the visual height of a line (max word height).
func lineBoxHeight(ln TextLine) float64 {
	h := 0.0
	for _, w := range ln.Words {
		if w.Height > h {
			h = w.Height
		}
	}
	if h <= 0 {
		h = 12
	}
	return h
}

// lineX returns the line's left edge.
func lineX(ln TextLine) float64 {
	if len(ln.Words) > 0 {
		return ln.Words[0].X
	}
	return 0
}

// isTerminalPunct reports whether text ends a sentence/paragraph.
func isTerminalPunct(text string) bool {
	t := strings.TrimRight(text, "\"'”’)]»")
	if t == "" {
		return false
	}
	switch t[len(t)-1] {
	case '.', '!', '?', ':', ';':
		// "..", "..." count as terminal; decimals like "3.14" usually do
		// not end a line anyway — keep it simple
		return true
	}
	return false
}

// isHyphenSplit reports whether the previous line ends with a hyphen that
// merges with a lowercase continuation ("transduc-" + "tion").
func isHyphenSplit(prev, next string) bool {
	if !strings.HasSuffix(prev, "-") || strings.HasSuffix(prev, "--") {
		return false
	}
	t := strings.TrimRight(prev, "-")
	if t == "" {
		return false
	}
	last := t[len(t)-1]
	if last < 'a' || last > 'z' {
		return false
	}
	if next == "" {
		return false
	}
	first := next[0]
	return first >= 'a' && first <= 'z'
}

// ParagraphsFromLines merges reading-order lines into paragraphs.
// Lines merge when they continue the same paragraph: vertical spacing
// within paragraph leading, x start at the block's left edge (first-line
// indents break), no terminal punctuation on the previous line. Hyphenated
// splits are joined without the hyphen.
func ParagraphsFromLines(lines []TextLine) []TextParagraph {
	var out []TextParagraph
	var cur *TextParagraph
	var curText strings.Builder

	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(curText.String())
			// bounds from the merged lines
			minX, maxX := 0.0, 0.0
			first := true
			for _, ln := range cur.Lines {
				x0, w := lineX(ln), lineWidth(ln)
				if first {
					minX, maxX = x0, x0+w
					first = false
				} else {
					if x0 < minX {
						minX = x0
					}
					if x0+w > maxX {
						maxX = x0 + w
					}
				}
				cur.Height = ln.Y + lineBoxHeight(ln) - cur.Y
			}
			cur.X = minX
			cur.Width = maxX - minX
			out = append(out, *cur)
			cur = nil
			curText.Reset()
		}
	}

	for _, ln := range lines {
		if len(ln.Words) == 0 {
			continue
		}
		h := lineBoxHeight(ln)
		x := lineX(ln)

		if cur == nil {
			cur = &TextParagraph{
				X:     x,
				Y:     ln.Y,
				Lines: []TextLine{ln},
			}
			curText.WriteString(ln.Text)
			continue
		}

		prev := cur.Lines[len(cur.Lines)-1]
		prevH := lineBoxHeight(prev)
		gap := ln.Y - (prev.Y + prevH)
		prevText := curText.String()

		merge := false
		switch {
		case gap > paragraphMaxGap*prevH:
			// paragraph spacing (or a column jump): new paragraph
		case isTerminalPunct(prevText):
			// previous line ended the sentence: new paragraph
		case isHyphenSplit(prevText, ln.Text):
			// hyphenated wrap: drop the hyphen and join directly
			hyphenless := strings.TrimSuffix(curText.String(), "-")
			curText.Reset()
			curText.WriteString(hyphenless)
			curText.WriteString(strings.TrimSuffix(ln.Text, " "))
			cur.Lines = append(cur.Lines, ln)
			merge = true
		case ln.Words[0].X-cur.X > paragraphIndentFactor*h:
			// first-line indent: new paragraph
		default:
			// continuation line
			curText.WriteString(" ")
			curText.WriteString(ln.Text)
			cur.Lines = append(cur.Lines, ln)
			merge = true
		}

		if !merge {
			flush()
			cur = &TextParagraph{
				X:     x,
				Y:     ln.Y,
				Lines: []TextLine{ln},
			}
			curText.WriteString(ln.Text)
		}
	}
	flush()
	return out
}

// lineWidth returns the line's ink width (last word end − first word start).
func lineWidth(ln TextLine) float64 {
	if len(ln.Words) == 0 {
		return 0
	}
	first, last := ln.Words[0], ln.Words[len(ln.Words)-1]
	w := last.X + last.Width - first.X
	if w < 0 {
		w = 0
	}
	return w
}

// ExtractPageParagraphs extracts a page's text as reconstructed paragraphs.
// This is the highest-level text API: reading-order lines merged into
// paragraphs with hyphenation joins (e.g. "transduc-/tion" → "transduction").
func ExtractPageParagraphs(pdfData []byte, pageIndex int) ([]TextParagraph, error) {
	lines, err := ExtractPageLines(pdfData, pageIndex)
	if err != nil {
		return nil, err
	}
	return ParagraphsFromLines(lines), nil
}

// ExtractAllPagesParagraphs reconstructs paragraphs for every page.
// The PDF is parsed once — prefer this over calling ExtractPageParagraphs
// in a loop.
func ExtractAllPagesParagraphs(pdfData []byte) (map[int][]TextParagraph, error) {
	pages, err := ExtractAllPagesLines(pdfData)
	if err != nil {
		return nil, err
	}
	out := make(map[int][]TextParagraph, len(pages))
	for i, lines := range pages {
		out[i] = ParagraphsFromLines(lines)
	}
	return out, nil
}

// ExtractPageParagraphsWithPassword extracts paragraphs from an encrypted
// page using the given user or owner password.
func ExtractPageParagraphsWithPassword(pdfData []byte, pageIndex int, password string) ([]TextParagraph, error) {
	runs, err := ExtractTextFromPageWithPassword(pdfData, pageIndex, password)
	if err != nil {
		return nil, err
	}
	return ParagraphsFromLines(GroupIntoTextLines(runs)), nil
}

// ExtractPageParagraphsFromFile reads a PDF file and reconstructs the
// paragraphs of one page.
func ExtractPageParagraphsFromFile(path string, pageIndex int) ([]TextParagraph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ExtractPageParagraphs(data, pageIndex)
}
