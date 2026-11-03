package gopdf

import (
	"math"
	"sort"
	"strings"
)

// ============================================================
// Text run merging — turns the fragmentary runs produced by content-stream
// parsing (one run per Tj, often one per glyph) into usable words and lines.
// Position-aware: a run's width is derived from the X advance to the next
// run on the same baseline, falling back to a font-size heuristic.
// ============================================================

// mergedRun is an internal word built from consecutive ExtractedText runs.
type mergedRun struct {
	text  string
	x     float64 // left edge
	width float64 // estimated advance width
	y     float64 // baseline
	font  string
	size  float64
}

// mergeGapRatio: a gap larger than this fraction of the font size between two
// runs starts a new word (spaces ≈ 0.22-0.30 em, kerning noise < 0.05 em).
const mergeGapRatio = 0.15

// estimateRunWidth estimates the advance width of a run.
// nextX is the left edge of the following run on the same line (-1 if none).
func estimateRunWidth(e ExtractedText, nextX float64) float64 {
	runes := []rune(e.Text)
	if len(runes) == 0 {
		return 0
	}
	// Exact advance computed by the raw parser from font /Widths.
	if e.Width > 0 {
		return e.Width
	}
	if nextX >= 0 {
		w := nextX - e.X
		if w > 0 && w < float64(len(runes))*math.Max(e.FontSize, 1)*2.0 {
			return w
		}
	}
	// Heuristic: average glyph advance ≈ 0.55 em for mixed text.
	size := e.FontSize
	if size <= 0 {
		size = 12
	}
	return 0.55 * size * float64(len(runes))
}

// buildMergedRuns converts raw runs into word-level merged runs grouped per line.
func buildMergedRuns(runs []ExtractedText) [][]mergedRun {
	if len(runs) == 0 {
		return nil
	}
	// Group runs into lines by baseline Y (PDF Y grows downward here).
	type lineKey struct {
		y   int // Y bucketed to 2pt to absorb rounding noise
		ord int // original order bucket to avoid merging far-apart fragments
	}
	type line struct {
		y    float64
		runs []ExtractedText
	}
	const yTol = 2.0
	lines := make([]*line, 0, 64)
	lineIndex := map[int]int{} // bucket -> line idx
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		bucket := int(math.Round(r.Y/yTol) * yTol)
		idx, ok := lineIndex[bucket]
		if !ok {
			idx = len(lines)
			lineIndex[bucket] = idx
			lines = append(lines, &line{y: r.Y})
		}
		lines[idx].runs = append(lines[idx].runs, r)
	}

	// Sort lines in reading order: ExtractedText.Y is top-down
	// (pageHeight - pdfY), so smaller Y = higher on the page.
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].y < lines[j].y })

	var result [][]mergedRun
	for _, ln := range lines {
		// Sort runs left-to-right; stable keeps original order for ties.
		sort.SliceStable(ln.runs, func(i, j int) bool { return ln.runs[i].X < ln.runs[j].X })

		var words []mergedRun
		var cur strings.Builder
		wordX := math.NaN() // left edge of the word being built
		wordW := 0.0        // accumulated advance width of the word
		lastX := math.NaN() // X of the last run appended into the word
		lastW := 0.0        // width estimate of the last run appended
		curSize := 0.0
		curFont := ""
		spaceSeen := false

		flush := func() {
			if cur.Len() > 0 {
				words = append(words, mergedRun{
					text: cur.String(), x: wordX, width: wordW, y: ln.y,
					font: curFont, size: curSize,
				})
			}
			cur.Reset()
		}

		for i, r := range ln.runs {
			// Whitespace runs delimit words (raw backend keeps them; the
			// v2 backend drops them, so gaps handle that case below).
			if strings.TrimSpace(r.Text) == "" {
				if cur.Len() > 0 {
					flush()
				}
				spaceSeen = true
				continue
			}
			var nextX float64 = -1
			if i+1 < len(ln.runs) {
				nextX = ln.runs[i+1].X
			}
			w := estimateRunWidth(r, nextX)
			size := r.FontSize
			if size <= 0 {
				size = curSize
			}
			if size <= 0 {
				size = 12
			}
			if cur.Len() == 0 {
				cur.WriteString(r.Text)
				wordX = r.X
				wordW = w
				lastX = r.X
				lastW = w
				curSize = r.FontSize
				curFont = r.FontName
				spaceSeen = false
				continue
			}
			// gap between the previous run's estimated end and this run's start
			gap := r.X - (lastX + lastW)
			if !spaceSeen && gap <= mergeGapRatio*size {
				// same word: append without space
				cur.WriteString(r.Text)
				wordW += gap + w
				lastX = r.X
				lastW = w
			} else {
				flush()
				cur.WriteString(r.Text)
				wordX = r.X
				wordW = w
				lastX = r.X
				lastW = w
				curSize = r.FontSize
				curFont = r.FontName
			}
			spaceSeen = false
		}
		flush()
		if len(words) > 0 {
			result = append(result, words)
		}
	}
	return result
}

// MergeTextRuns merges fragmentary ExtractedText runs into word-level runs on
// the same baseline. Adjacent runs whose gap is small relative to the font
// size are joined without a space; larger gaps become spaces. This is the
// key step that turns per-glyph Tj streams into readable text.
func MergeTextRuns(runs []ExtractedText) []ExtractedText {
	lines := buildMergedRuns(runs)
	var out []ExtractedText
	for _, words := range lines {
		for _, w := range words {
			out = append(out, ExtractedText{
				Text: w.text, X: w.x, Y: w.y,
				FontName: w.font, FontSize: w.size,
				Width: w.width,
			})
		}
	}
	return out
}

// GroupIntoTextLines merges runs and groups them into lines with joined text.
func GroupIntoTextLines(runs []ExtractedText) []TextLine {
	lines := buildMergedRuns(runs)
	out := make([]TextLine, 0, len(lines))
	for _, words := range lines {
		tl := TextLine{Y: words[0].y, Words: make([]TextWord, len(words))}
		var sb strings.Builder
		for i, w := range words {
			tl.Words[i] = TextWord{
				X: w.x, Y: w.y,
				Width:    w.width,
				Height:   w.size,
				Text:     w.text,
				FontName: w.font,
				FontSize: w.size,
			}
			if i > 0 {
				// decide space by gap
				prev := words[i-1]
				gap := w.x - (prev.x + prev.width)
				size := prev.size
				if size <= 0 {
					size = 12
				}
				if gap > mergeGapRatio*size {
					sb.WriteByte(' ')
				}
			}
			sb.WriteString(w.text)
		}
		tl.Text = sb.String()
		out = append(out, tl)
	}
	return out
}

// ExtractPageLines extracts text from a page and returns it as merged,
// position-aware lines. This is the recommended API for reading text from
// complex real-world PDFs (per-glyph Tj streams, tables, academic papers).
func ExtractPageLines(pdfData []byte, pageIndex int) ([]TextLine, error) {
	texts, err := ExtractTextFromPage(pdfData, pageIndex)
	if err != nil {
		return nil, err
	}
	return GroupIntoTextLines(texts), nil
}

// ExtractAllPagesLines extracts text from every page and returns it as
// merged, position-aware lines. The PDF is parsed once — use this instead of
// calling ExtractPageLines in a loop (each call would re-parse the file).
func ExtractAllPagesLines(pdfData []byte) (map[int][]TextLine, error) {
	pages, err := ExtractTextFromAllPages(pdfData)
	if err != nil {
		return nil, err
	}
	out := make(map[int][]TextLine, len(pages))
	for i, runs := range pages {
		out[i] = GroupIntoTextLines(runs)
	}
	return out, nil
}
