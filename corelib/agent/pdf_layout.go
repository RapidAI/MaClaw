package agent

import (
	"bytes"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
	"github.com/ledongthuc/pdf"
)

// pdfLayoutPage is one page rebuilt from glyph positions. Structured is set
// when the page gained a Markdown table or a column order the plain extractor
// does not already have. Callers keep the plain page unless that flag is set
// or the layout only removes CJK tracking spaces.
type pdfLayoutPage struct {
	text       string
	structured bool
}

// PDFPageBodies returns one body per page for the shared document reader.
// A page stays empty when neither the position layout nor the plain extractor
// produced text, so image-only pages still reach vision and OCR.
func PDFPageBodies(data []byte, numPages int) []string {
	if numPages <= 0 || len(data) == 0 {
		return nil
	}
	layouts, skipPlain := pdfLayoutPass(data, numPages)
	out := make([]string, numPages)
	for i := 0; i < numPages; i++ {
		var layout pdfLayoutPage
		if i < len(layouts) {
			layout = layouts[i]
		}
		text, err := pdfPageBody(data, i, layout, skipPlain)
		if err != nil {
			continue
		}
		out[i] = text
	}
	return out
}

// PDFPageBody is the single-page form used by GUI preview. It follows the
// same choice as PDFPageBodies.
func PDFPageBody(data []byte, pageIndex int) (string, error) {
	if pageIndex < 0 || len(data) == 0 {
		return "", nil
	}
	return choosePDFPageText(layoutPDFPage(data, pageIndex), plainPDFPage(data, pageIndex))
}

type nativeText struct {
	text string
	err  error
}

func plainPDFPage(data []byte, pageIndex int) nativeText {
	text, err := gopdf2.ExtractPageText(data, pageIndex)
	return nativeText{text: text, err: err}
}

func choosePDFPageText(layout pdfLayoutPage, native nativeText) (string, error) {
	layoutText := strings.TrimSpace(layout.text)
	nativeText := strings.TrimSpace(native.text)
	if layoutText == "" {
		return nativeText, native.err
	}
	if native.err != nil || nativeText == "" {
		if pdfTextGarbageRatio(layoutText) > 0.25 {
			return nativeText, native.err
		}
		return layoutText, nil
	}
	if pdfTextGarbageRatio(layoutText) > pdfTextGarbageRatio(nativeText)+0.02 {
		return nativeText, nil
	}
	if layout.structured || layoutRepairsTracking(layoutText, nativeText) {
		return layoutText, nil
	}
	return nativeText, nil
}

func layoutRepairsTracking(layout, native string) bool {
	collapsed := collapsePDFGlyphSpaces(native)
	if collapsed == layout && collapsed != native {
		return true
	}
	repaired := removeCJKTrackingSpaces(native)
	return repaired == layout && repaired != native
}

func removeCJKTrackingSpaces(s string) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range rs {
		if unicode.IsSpace(r) && i > 0 && i+1 < len(rs) &&
			unicode.Is(unicode.Han, rs[i-1]) && unicode.Is(unicode.Han, rs[i+1]) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func pdfTextGarbageRatio(text string) float64 {
	total, bad := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		if r == '\uFFFD' || unicode.IsControl(r) || (r >= 0xE000 && r <= 0xF8FF) {
			bad++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(bad) / float64(total)
}

func layoutPDFPages(data []byte, numPages int) (pages []pdfLayoutPage, complete bool) {
	pages = make([]pdfLayoutPage, numPages)
	defer func() { _ = recover() }()
	if numPages <= 0 || len(data) == 0 {
		return pages, false
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || reader == nil {
		return pages, false
	}
	limit := reader.NumPage()
	if limit < numPages {
		numPages = limit
	}
	for i := 0; i < numPages; i++ {
		pages[i] = safeLayoutPDFPage(reader.Page(i + 1))
	}
	return pages, true
}

// pdfLayoutPass reads every page once. skipPlain is set only when that pass
// finished, found no text, and the file has no classic page dictionary.
// ExtractPageText would otherwise open a new reader per page and then run
// the raw parser, which cannot see an object-stream page tree.
func pdfLayoutPass(data []byte, numPages int) (layouts []pdfLayoutPage, skipPlain bool) {
	layouts, complete := layoutPDFPages(data, numPages)
	if !complete || !pdfLayoutsAreEmpty(layouts) || plaintextHasPDFPageObject(data) {
		return layouts, false
	}
	return layouts, true
}

func pdfLayoutsAreEmpty(layouts []pdfLayoutPage) bool {
	if len(layouts) == 0 {
		return false
	}
	for _, layout := range layouts {
		if strings.TrimSpace(layout.text) != "" {
			return false
		}
	}
	return true
}

func pdfPageBody(data []byte, pageIndex int, layout pdfLayoutPage, skipPlain bool) (string, error) {
	if skipPlain {
		return "", nil
	}
	return choosePDFPageText(layout, plainPDFPage(data, pageIndex))
}

func layoutPDFPage(data []byte, pageIndex int) (page pdfLayoutPage) {
	defer func() { _ = recover() }()
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || reader == nil || pageIndex+1 > reader.NumPage() {
		return pdfLayoutPage{}
	}
	return safeLayoutPDFPage(reader.Page(pageIndex + 1))
}

func safeLayoutPDFPage(page pdf.Page) (out pdfLayoutPage) {
	defer func() { _ = recover() }()
	content := page.Content()
	lines := assemblePDFLayoutLines(content.Text)
	if len(lines) == 0 {
		return pdfLayoutPage{}
	}
	lines, table := foldPDFLayoutTables(lines)
	ordered, reordered := orderPDFLayoutColumns(lines)
	text := strings.TrimSpace(renderPDFLayoutLines(ordered))
	if text == "" {
		return pdfLayoutPage{}
	}
	return pdfLayoutPage{text: text, structured: table || reordered}
}

type pdfLayoutWord struct {
	x, y, w float64
	text    string
}

type pdfLayoutLine struct {
	y     float64
	minX  float64
	maxX  float64
	words []pdfLayoutWord
	text  string
	full  bool
}

const (
	pdfLayoutLineTolerance = 2.0
	pdfLayoutWordGapFactor = 0.3
	pdfLayoutGutterMin     = 24.0
	pdfLayoutColumnSlack   = 18.0
	pdfLayoutMinSideWords  = 4
)

func assemblePDFLayoutLines(glyphs []pdf.Text) []pdfLayoutLine {
	filtered := make([]pdf.Text, 0, len(glyphs))
	for _, g := range glyphs {
		if strings.TrimSpace(g.S) == "" || !isFinitePDFLayout(g.X) || !isFinitePDFLayout(g.Y) {
			continue
		}
		filtered = append(filtered, g)
	}
	if len(filtered) == 0 {
		return nil
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].Y < filtered[j].Y })
	var buckets [][]pdf.Text
	var cur []pdf.Text
	baseY := 0.0
	for _, g := range filtered {
		if len(cur) > 0 && math.Abs(g.Y-baseY) > pdfLayoutLineTolerance {
			buckets = append(buckets, cur)
			cur = nil
		}
		if len(cur) == 0 {
			baseY = g.Y
		}
		cur = append(cur, g)
	}
	if len(cur) > 0 {
		buckets = append(buckets, cur)
	}
	sort.SliceStable(buckets, func(i, j int) bool {
		return buckets[i][0].Y > buckets[j][0].Y
	})
	lines := make([]pdfLayoutLine, 0, len(buckets))
	for _, bucket := range buckets {
		sort.SliceStable(bucket, func(i, j int) bool { return bucket[i].X < bucket[j].X })
		line := joinPDFLayoutGlyphs(bucket)
		if strings.TrimSpace(line.text) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func joinPDFLayoutGlyphs(glyphs []pdf.Text) pdfLayoutLine {
	line := pdfLayoutLine{y: glyphs[0].Y, minX: glyphs[0].X}
	var word pdfLayoutWord
	open := false
	flush := func() {
		if !open {
			return
		}
		word.text = strings.TrimSpace(word.text)
		if word.text != "" {
			line.words = append(line.words, word)
		}
		open = false
	}
	for i, g := range glyphs {
		width := g.W
		if !isFinitePDFLayout(width) || width < 0 {
			width = 0
		}
		if i == 0 {
			word = pdfLayoutWord{x: g.X, y: g.Y, w: width, text: g.S}
			open = true
			line.text = g.S
			if g.X+width > line.maxX {
				line.maxX = g.X + width
			}
			continue
		}
		prev := glyphs[i-1]
		prevW := prev.W
		if !isFinitePDFLayout(prevW) || prevW < 0 {
			prevW = 0
		}
		gap := g.X - (prev.X + prevW)
		separated := gap > math.Max(prevW, width)*pdfLayoutWordGapFactor && gap > 0.8
		if separated {
			flush()
			if line.text != "" && !endsWithCJKLayout(line.text) && !startsWithCJKLayout(g.S) {
				line.text += " "
			}
			word = pdfLayoutWord{x: g.X, y: g.Y, w: width, text: g.S}
			open = true
		} else if open {
			word.text += g.S
			if g.X+width > word.x+word.w {
				word.w = g.X + width - word.x
			}
		}
		line.text += g.S
		if g.X < line.minX {
			line.minX = g.X
		}
		if g.X+width > line.maxX {
			line.maxX = g.X + width
		}
	}
	flush()
	line.text = strings.TrimSpace(line.text)
	return line
}

func endsWithCJKLayout(s string) bool {
	r, size := utf8.DecodeLastRuneInString(s)
	return size > 0 && isCJKLayout(r)
}

func startsWithCJKLayout(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	return size > 0 && isCJKLayout(r)
}

func isCJKLayout(r rune) bool {
	return unicode.Is(unicode.Han, r) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x20000 && r <= 0x3FFFF)
}

func isFinitePDFLayout(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func orderPDFLayoutColumns(lines []pdfLayoutLine) ([]pdfLayoutLine, bool) {
	gapL, gapR, ok := findPDFLayoutGutter(lines)
	if !ok {
		return lines, false
	}
	var out, leftBand, rightBand []pdfLayoutLine
	flush := func() {
		out = append(out, leftBand...)
		out = append(out, rightBand...)
		leftBand, rightBand = nil, nil
	}
	for _, ln := range lines {
		if ln.full {
			flush()
			out = append(out, ln)
			continue
		}
		left, right, split := splitPDFLayoutLine(ln, gapL, gapR)
		if split {
			leftBand = append(leftBand, left)
			rightBand = append(rightBand, right)
			continue
		}
		if layoutLineCrosses(ln, gapL, gapR) {
			flush()
			out = append(out, ln)
			continue
		}
		if lineCenter(ln) < (gapL+gapR)/2 {
			leftBand = append(leftBand, ln)
		} else {
			rightBand = append(rightBand, ln)
		}
	}
	flush()
	return out, layoutOrderChanged(lines, out)
}

func splitPDFLayoutLine(ln pdfLayoutLine, gapL, gapR float64) (left, right pdfLayoutLine, split bool) {
	if len(ln.words) < 2 {
		return ln, pdfLayoutLine{}, false
	}
	var lw, rw []pdfLayoutWord
	for _, w := range ln.words {
		end := w.x + math.Max(w.w, 0)
		if w.x < gapR && end > gapL && end-w.x > 1 && w.x < gapL && end > gapR {
			return ln, pdfLayoutLine{}, false
		}
		if w.x >= gapR {
			rw = append(rw, w)
		} else if end <= gapL || w.x < gapL {
			lw = append(lw, w)
		} else {
			return ln, pdfLayoutLine{}, false
		}
	}
	if len(lw) == 0 || len(rw) == 0 {
		return ln, pdfLayoutLine{}, false
	}
	return lineFromPDFLayoutWords(lw, ln.y), lineFromPDFLayoutWords(rw, ln.y), true
}

func lineFromPDFLayoutWords(words []pdfLayoutWord, y float64) pdfLayoutLine {
	ln := pdfLayoutLine{y: y, words: words, minX: words[0].x}
	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(w.text)
		if w.x < ln.minX {
			ln.minX = w.x
		}
		end := w.x + math.Max(w.w, 0)
		if end > ln.maxX {
			ln.maxX = end
		}
	}
	ln.text = b.String()
	return ln
}

func lineCenter(ln pdfLayoutLine) float64 {
	if len(ln.words) == 0 {
		return ln.minX
	}
	return ln.words[0].x
}

func layoutLineCrosses(ln pdfLayoutLine, gapL, gapR float64) bool {
	left, right := false, false
	for _, w := range ln.words {
		if w.x < gapL {
			left = true
		}
		if w.x >= gapR {
			right = true
		}
	}
	return left && right
}

func layoutOrderChanged(before, after []pdfLayoutLine) bool {
	if len(before) != len(after) {
		return true
	}
	for i := range before {
		if before[i].text != after[i].text {
			return true
		}
	}
	return false
}

type pdfLayoutGapVote struct {
	l, r float64
}

func findPDFLayoutGutter(lines []pdfLayoutLine) (gapL, gapR float64, ok bool) {
	var votes []pdfLayoutGapVote
	minX, maxX := 0.0, 0.0
	seenX := false
	for _, ln := range lines {
		if ln.full {
			continue
		}
		for _, w := range ln.words {
			a := w.x
			b := w.x + math.Max(w.w, 0)
			if !seenX || a < minX {
				minX = a
			}
			if !seenX || b > maxX {
				maxX = b
			}
			seenX = true
		}
		if len(ln.words) < 2 {
			continue
		}
		best := -1
		gap := 0.0
		for i := 0; i < len(ln.words)-1; i++ {
			l := ln.words[i].x + math.Max(ln.words[i].w, 0.5)
			r := ln.words[i+1].x
			if r-l > gap {
				gap = r - l
				best = i
			}
		}
		if best < 0 || gap < pdfLayoutGutterMin {
			continue
		}
		l := ln.words[best].x + math.Max(ln.words[best].w, 0.5)
		r := ln.words[best+1].x
		votes = append(votes, pdfLayoutGapVote{l, r})
	}
	if len(votes) < 3 || maxX-minX < 80 {
		return 0, 0, false
	}
	sort.Slice(votes, func(i, j int) bool {
		return votes[i].l+votes[i].r < votes[j].l+votes[j].r
	})
	bestN := 0
	bestL, bestR := 0.0, 0.0
	for i := range votes {
		n := 1
		l, r := votes[i].l, votes[i].r
		for j := i + 1; j < len(votes); j++ {
			if votes[j].l-votes[i].l > 36 {
				break
			}
			n++
			if votes[j].l > l {
				l = votes[j].l
			}
			if votes[j].r < r {
				r = votes[j].r
			}
		}
		if n > bestN && r-l >= pdfLayoutGutterMin {
			bestN = n
			bestL, bestR = l, r
		}
	}
	if bestN < 3 {
		return 0, 0, false
	}
	width := maxX - minX
	if bestL < minX+width*0.12 || bestR > maxX-width*0.12 {
		return 0, 0, false
	}
	mid := (bestL + bestR) / 2
	sideL, sideR := 0, 0
	for _, ln := range lines {
		for _, w := range ln.words {
			if w.x+math.Max(w.w, 0)/2 < mid {
				sideL++
			} else {
				sideR++
			}
		}
	}
	if sideL < pdfLayoutMinSideWords || sideR < pdfLayoutMinSideWords {
		return 0, 0, false
	}
	return bestL, bestR, true
}

func foldPDFLayoutTables(lines []pdfLayoutLine) ([]pdfLayoutLine, bool) {
	var out []pdfLayoutLine
	folded := false
	for i := 0; i < len(lines); {
		run := pdfLayoutTableRun(lines, i)
		centers := []float64(nil)
		if run >= 3 {
			centers = pdfLayoutColumnCenters(lines[i : i+run])
		}
		if pdfLayoutLooksLikeTable(lines[i:i+run], centers) {
			out = append(out, pdfLayoutLine{
				y:    lines[i].y,
				text: pdfLayoutMarkdownTable(lines[i:i+run], centers),
				full: true,
			})
			folded = true
			i += run
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return out, folded
}

func renderPDFLayoutLines(lines []pdfLayoutLine) string {
	var b strings.Builder
	for i, ln := range lines {
		if i > 0 {
			if ln.full || lines[i-1].full {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(ln.text)
	}
	return b.String()
}

func pdfLayoutTableRun(lines []pdfLayoutLine, start int) int {
	if start >= len(lines) || len(lines[start].words) < 2 {
		return 0
	}
	n := 1
	for start+n < len(lines) {
		ln := lines[start+n]
		if len(ln.words) < 2 {
			break
		}
		prev := lines[start+n-1]
		dy := math.Abs(prev.y - ln.y)
		if dy > 28 {
			break
		}
		n++
		if n == 12 {
			break
		}
	}
	return n
}

func pdfLayoutColumnCenters(lines []pdfLayoutLine) []float64 {
	var xs []float64
	for _, ln := range lines {
		for _, w := range ln.words {
			xs = append(xs, w.x)
		}
	}
	if len(xs) < 6 {
		return nil
	}
	sort.Float64s(xs)
	var clusters [][]float64
	var cur []float64
	for _, x := range xs {
		if len(cur) > 0 && x-cur[len(cur)-1] > pdfLayoutColumnSlack {
			clusters = append(clusters, cur)
			cur = nil
		}
		cur = append(cur, x)
	}
	if len(cur) > 0 {
		clusters = append(clusters, cur)
	}
	var centers []float64
	for _, c := range clusters {
		if len(c) < 2 || c[len(c)-1]-c[0] > 14 {
			continue
		}
		centers = append(centers, c[len(c)/2])
	}
	if len(centers) < 2 || len(centers) > 8 {
		return nil
	}
	return centers
}

func pdfLayoutLooksLikeTable(lines []pdfLayoutLine, centers []float64) bool {
	if len(lines) < 3 || len(centers) < 2 {
		return false
	}
	words := 0
	for _, ln := range lines {
		if len(ln.words) < 2 || len(ln.words) > len(centers)+3 {
			return false
		}
		if pdfLayoutColumnHits(ln, centers) < 2 {
			return false
		}
		words += len(ln.words)
		for _, w := range ln.words {
			if utf8.RuneCountInString(w.text) > 40 {
				return false
			}
		}
	}
	// A sentence packed onto each side of a two-column line has many more
	// tokens than a table cell. Those pages stay on the column reader.
	if float64(words)/float64(len(lines)) > float64(len(centers))*2 {
		return false
	}
	return true
}

func pdfLayoutColumnHits(ln pdfLayoutLine, centers []float64) int {
	hit := make([]bool, len(centers))
	for _, w := range ln.words {
		best := -1
		dist := math.MaxFloat64
		for i, c := range centers {
			d := math.Abs(w.x - c)
			if d < dist {
				dist = d
				best = i
			}
		}
		if best >= 0 && dist <= 14 {
			hit[best] = true
		}
	}
	n := 0
	for _, ok := range hit {
		if ok {
			n++
		}
	}
	return n
}

func pdfLayoutMarkdownTable(lines []pdfLayoutLine, centers []float64) string {
	var b strings.Builder
	for i, ln := range lines {
		cells := make([]string, len(centers))
		for _, w := range ln.words {
			best := 0
			dist := math.MaxFloat64
			for ci, c := range centers {
				d := math.Abs(w.x - c)
				if d < dist {
					dist = d
					best = ci
				}
			}
			if dist > 14 {
				continue
			}
			piece := strings.ReplaceAll(strings.ReplaceAll(w.text, "|", "\\|"), "\n", " ")
			if cells[best] != "" {
				cells[best] += " "
			}
			cells[best] += piece
		}
		b.WriteString("| ")
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString(" |\n")
		if i == 0 {
			b.WriteString("|")
			for range centers {
				b.WriteString(" --- |")
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
