package gopdf

import (
	"math"
	"os"
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

// cjkMergeGapRatio is the tighter threshold for CJK runs, which are
// typically packed without inter-character spacing.
const cjkMergeGapRatio = 0.08

// isCJKRun reports whether the text contains CJK characters.
func isCJKRun(text string) bool {
	for _, r := range text {
		if (r >= 0x2E80 && r <= 0x9FFF) || (r >= 0xF900 && r <= 0xFAFF) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0x3000 && r <= 0x303F) {
			return true
		}
	}
	return false
}

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
		// Trust the gap-derived width only when the implied average glyph
		// advance is realistic (< 0.6 em/char, as in kerning-split
		// fragments). For a long run followed by a word space the gap
		// includes the space and would grossly overestimate the width,
		// which then swallows the space during merging.
		if w > 0 && e.FontSize > 0 && w < 0.6*e.FontSize*float64(len(runes)) {
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

// explodeRunSpaces splits a run containing embedded spaces into word sub-runs
// separated by synthetic single-space runs (which the line merger already
// treats as word delimiters). The run's advance width is distributed
// proportionally over its runes so each word keeps a plausible position and
// width. nil means "keep the run as is" — either there are no spaces, or
// the run width is unknown and exploding would collapse all words onto the
// same X (losing the spaces through gap-based merging).
func explodeRunSpaces(e ExtractedText) []ExtractedText {
	if !strings.Contains(e.Text, " ") || e.Width <= 0 {
		return nil
	}
	runes := []rune(e.Text)
	per := 0.0 // advance per rune; 0 when the run width is unknown
	if e.Width > 0 {
		per = e.Width / float64(len(runes))
	}
	var out []ExtractedText
	segStart := 0
	flushSeg := func(end int) {
		if end > segStart {
			out = append(out, ExtractedText{
				Text:     string(runes[segStart:end]),
				X:        e.X + float64(segStart)*per,
				Y:        e.Y,
				FontName: e.FontName,
				FontSize: e.FontSize,
				Width:    float64(end-segStart) * per,
			})
		}
	}
	for i, r := range runes {
		if r == ' ' {
			flushSeg(i)
			out = append(out, ExtractedText{
				Text: " ", X: e.X + float64(i)*per, Y: e.Y,
				FontName: e.FontName, FontSize: e.FontSize, Width: per,
			})
			segStart = i + 1
		}
	}
	flushSeg(len(runes))
	return out
}

// buildMergedRuns converts raw runs into word-level merged runs grouped per line.
func buildMergedRuns(runs []ExtractedText) [][]mergedRun {
	if len(runs) == 0 {
		return nil
	}
	// Split runs containing embedded spaces so word boundaries inside a
	// single show operation become separate words with proportionally
	// distributed positions and widths.
	expanded := make([]ExtractedText, 0, len(runs))
	for _, r := range runs {
		if subs := explodeRunSpaces(r); subs != nil {
			expanded = append(expanded, subs...)
		} else {
			expanded = append(expanded, r)
		}
	}
	runs = expanded
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
			// CJK characters are packed tightly without spaces — use a
			// tighter threshold so CJK glyphs merge into words correctly
			ratio := mergeGapRatio
			if isCJKRun(r.Text) {
				ratio = cjkMergeGapRatio
			}
			if !spaceSeen && gap <= ratio*size {
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
	lines := orderLineGroups(buildMergedRuns(runs))
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
// Lines are emitted in reading order: on two-column pages each column is read
// top-down before moving to the next; full-width lines (e.g. spanning titles)
// act as section separators. Single-column pages are ordered purely by Y.
func GroupIntoTextLines(runs []ExtractedText) []TextLine {
	lines := orderLineGroups(buildMergedRuns(runs))
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

// ExtractTextFromPageWithPassword extracts text from an encrypted PDF page
// using the given user or owner password.
func ExtractTextFromPageWithPassword(pdfData []byte, pageIndex int, password string) ([]ExtractedText, error) {
	dc, err := authenticate(pdfData, password)
	if err != nil {
		return nil, err
	}
	if dc == nil {
		// not encrypted — plain path
		return ExtractTextFromPage(pdfData, pageIndex)
	}
	decrypted := decryptPDF(pdfData, dc)
	return ExtractTextFromPage(decrypted, pageIndex)
}

// ExtractTextFromAllPagesWithPassword extracts text from an encrypted PDF
// using the given user or owner password. Non-encrypted input takes the
// plain path automatically.
func ExtractTextFromAllPagesWithPassword(pdfData []byte, password string) (map[int][]ExtractedText, error) {
	dc, err := authenticate(pdfData, password)
	if err != nil {
		return nil, err
	}
	if dc == nil {
		return ExtractTextFromAllPages(pdfData)
	}
	decrypted := decryptPDF(pdfData, dc)
	return ExtractTextFromAllPages(decrypted)
}

// ExtractPageTextWithPassword extracts a page as plain text from an
// encrypted PDF using the given password.
func ExtractPageTextWithPassword(pdfData []byte, pageIndex int, password string) (string, error) {
	lines, err := ExtractTextFromPageWithPassword(pdfData, pageIndex, password)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	lastY := 0.0
	first := true
	for _, ln := range GroupIntoTextLines(lines) {
		if !first {
			sb.WriteByte('\n')
		}
		sb.WriteString(ln.Text)
		lastY = ln.Y
		first = false
	}
	_ = lastY
	return sb.String(), nil
}

// ExtractTextFromFile extracts text from a PDF file on disk.
// Convenience wrapper that reads the file and calls ExtractTextFromAllPages.
func ExtractTextFromFile(path string) (map[int][]ExtractedText, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ExtractTextFromAllPages(data)
}

// ExtractPageLinesFromFile extracts merged text lines from a PDF file.
func ExtractPageLinesFromFile(path string, pageIndex int) ([]TextLine, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ExtractPageLines(data, pageIndex)
}

// ============================================================
// Column-aware reading order.
// Two-column pages (academic papers, newsletters) store lines in content
// order that alternates between columns; sorting purely by Y interleaves
// them. A vertical gutter free of text identifies the columns; full-width
// lines (titles spanning the gutter) split the page into sections that are
// each read column-by-column.
// ============================================================

// columnGutterMin is the narrowest vertical gap (in points) that may separate
// two text columns. Printed gutters are typically 20pt or more; the largest
// gaps inside a single column (justified-line word spacing, indents) stay
// well below this.
const columnGutterMin = 14.0

// columnMinLinesPerSide is the minimum number of lines required on each side
// of a candidate gutter before the split is trusted.
const columnMinLinesPerSide = 3

// lineSpan returns the horizontal extent of a merged line's words.
func lineSpan(words []mergedRun) (x0, x1 float64) {
	x0, x1 = math.MaxFloat64, -math.MaxFloat64
	for _, w := range words {
		if w.x < x0 {
			x0 = w.x
		}
		if w.x+w.width > x1 {
			x1 = w.x + w.width
		}
	}
	if x0 > x1 {
		return 0, 0
	}
	return x0, x1
}

// classifyWord returns where a word sits relative to the gutter:
// -1 left, +1 right, 0 overlapping the gutter interior.
func classifyWord(x0, x1, gl, gr float64) int {
	switch {
	case x1 <= gl+2:
		return -1
	case x0 >= gr-2:
		return 1
	default:
		return 0
	}
}

// detectColumnGutter looks for a vertical gutter splitting the text into
// two columns. It returns the gutter's left and right edges and whether a
// trustworthy split was found.
//
// Detection and validation work at word level: a baseline row may carry
// words on both sides of the gutter (as in two-column text typeset on a
// shared grid), and only words actually overlapping the gutter interior
// count as crossing it — a spanning title contributes those.
//
// A candidate is accepted only when:
//   - few words overlap the strip (<= 25% of all words),
//   - the clean part of the strip is at least columnGutterMin wide,
//   - at least columnMinLinesPerSide rows contribute words to each side,
//   - each side's rows span most (>=55%) of the page's vertical text range,
//     which rules out figures and ragged-right paragraphs.
func detectColumnGutter(lines [][]mergedRun) (gutterL, gutterR float64, ok bool) {
	if len(lines) < 2*columnMinLinesPerSide {
		return 0, 0, false
	}
	gmin, gmax := math.MaxFloat64, -math.MaxFloat64
	ymin, ymax := math.MaxFloat64, -math.MaxFloat64
	for _, ln := range lines {
		for _, w := range ln {
			if w.x < gmin {
				gmin = w.x
			}
			if w.x+w.width > gmax {
				gmax = w.x + w.width
			}
		}
		y := ln[0].y
		if y < ymin {
			ymin = y
		}
		if y > ymax {
			ymax = y
		}
	}
	ySpan := ymax - ymin
	if ySpan <= 0 || gmax-gmin < 200 {
		// too narrow to plausibly be two columns
		return 0, 0, false
	}

	// Count how many words cover each candidate x (2pt granularity).
	const step = 2.0
	n := int((gmax-gmin)/step) + 1
	cross := make([]int, n)
	for _, ln := range lines {
		for _, w := range ln {
			lo := int((w.x - gmin) / step)
			hi := int((w.x + w.width - gmin) / step)
			if lo < 0 {
				lo = 0
			}
			if hi >= n {
				hi = n - 1
			}
			for c := lo; c <= hi; c++ {
				cross[c]++
			}
		}
	}

	// A candidate can be a gutter when only crossing lines (full-width
	// titles) cover it. Words tile each line horizontally, so a crossing
	// line contributes ~1 word per candidate; column interiors add at
	// least columnMinLinesPerSide rows on top. Setting the threshold just
	// above the page's minimum coverage separates the two: the minimum
	// occurs in the gutter (or at sparse text edges, which are too narrow
	// to qualify once the width rule below applies).
	minCross := n
	for c := 0; c < n; c++ {
		if cross[c] < minCross {
			minCross = cross[c]
		}
	}
	maxCross := minCross + 2
	bestStart, bestEnd := -1, -1
	runStart := -1
	for c := 0; c < n; c++ {
		if cross[c] <= maxCross {
			if runStart < 0 {
				runStart = c
			}
			if c-runStart > bestEnd-bestStart {
				bestStart, bestEnd = runStart, c
			}
		} else {
			runStart = -1
		}
	}
	if bestStart < 0 || float64(bestEnd-bestStart+1)*step < columnGutterMin {
		return 0, 0, false
	}

	gl := gmin + float64(bestStart)*step
	gr := gmin + float64(bestEnd+1)*step

	// Validate the split: enough rows feeding each side, both sides
	// spanning most of the vertical range, few crossing rows.
	var leftN, rightN, crossN int
	leftYmin, leftYmax := math.MaxFloat64, -math.MaxFloat64
	rightYmin, rightYmax := math.MaxFloat64, -math.MaxFloat64
	for _, ln := range lines {
		var hasLeft, hasRight, interior bool
		for _, w := range ln {
			switch classifyWord(w.x, w.x+w.width, gl, gr) {
			case -1:
				hasLeft = true
			case 1:
				hasRight = true
			default:
				interior = true
			}
			if interior {
				break
			}
		}
		if interior {
			crossN++
			continue
		}
		if hasLeft {
			leftN++
			leftYmin = math.Min(leftYmin, ln[0].y)
			leftYmax = math.Max(leftYmax, ln[0].y)
		}
		if hasRight {
			rightN++
			rightYmin = math.Min(rightYmin, ln[0].y)
			rightYmax = math.Max(rightYmax, ln[0].y)
		}
	}
	if leftN < columnMinLinesPerSide || rightN < columnMinLinesPerSide {
		return 0, 0, false
	}
	if crossN > len(lines)/4 {
		return 0, 0, false
	}
	if leftYmax-leftYmin < 0.35*ySpan || rightYmax-rightYmin < 0.35*ySpan {
		return 0, 0, false
	}
	return gl, gr, true
}

// orderLineGroups reorders merged line groups into reading order. Without a
// column gutter the Y-sorted input passes through unchanged; with one, each
// section between full-width lines is emitted left column first, top-down.
// Rows are split at word granularity so a row carrying words on both sides
// of the gutter feeds both columns.
func orderLineGroups(lines [][]mergedRun) [][]mergedRun {
	if len(lines) < 2*columnMinLinesPerSide {
		return lines
	}
	gl, gr, ok := detectColumnGutter(lines)
	if !ok {
		return lines
	}
	var out [][]mergedRun
	var secL, secR [][]mergedRun
	flush := func() {
		out = append(out, secL...)
		out = append(out, secR...)
		secL, secR = nil, nil
	}
	for _, ln := range lines { // lines arrive Y-sorted from buildMergedRuns
		var leftWords, rightWords []mergedRun
		interior := false
		for _, w := range ln {
			switch classifyWord(w.x, w.x+w.width, gl, gr) {
			case -1:
				leftWords = append(leftWords, w)
			case 1:
				rightWords = append(rightWords, w)
			default:
				interior = true
			}
			if interior {
				break
			}
		}
		if interior {
			// full-width line: emit the section built so far, then itself
			flush()
			out = append(out, ln)
			continue
		}
		if len(leftWords) > 0 {
			secL = append(secL, leftWords)
		}
		if len(rightWords) > 0 {
			secR = append(secR, rightWords)
		}
	}
	flush()
	return out
}
