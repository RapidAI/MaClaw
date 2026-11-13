package gopdf

import (
	"math"
	"sort"
	"strings"
)

// ============================================================
// Table structure detection — PyMuPDF parity: page.find_tables().
// Detects ruled tables by clustering the horizontal/vertical border lines
// found in the vector graphics, then fills cells with position-aware words.
// Coordinates are visual page points (top-down, honoring /Rotate).
// Borderless tables are not detected (no grid lines to cluster).
// ============================================================

// TableCell is one detected table cell.
type TableCell struct {
	X, Y, W, H float64
	Text       string
}

// Table is one detected table.
type Table struct {
	X, Y, W, H float64 // bounding box
	ColCount   int
	Rows       [][]TableCell
}

// ExtractTablesFromPage detects ruled tables on a page (0-based index).
//
// Example:
//
//	tables, err := gopdf.ExtractTablesFromPage(data, 0)
//	for _, t := range tables {
//	    for _, row := range t.Rows {
//	        for _, c := range row { fmt.Print(c.Text, " | ") }
//	        fmt.Println()
//	    }
//	}
func ExtractTablesFromPage(pdfData []byte, pageIndex int) ([]Table, error) {
	data := pdfData
	drawings, err := ExtractPageDrawings(data, pageIndex)
	if err != nil {
		return nil, err
	}
	lines, err := ExtractPageLines(data, pageIndex)
	if err != nil {
		return nil, err
	}
	return detectTable(drawings, lines), nil
}

// ExtractTablesFromFile detects tables on a page of a PDF file.
func ExtractTablesFromFile(path string, pageIndex int) ([]Table, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return ExtractTablesFromPage(data, pageIndex)
}

// lineSeg is one horizontal or vertical border segment.
// Horizontal: coord = y, from..to = x range. Vertical: coord = x, from..to = y range.
type lineSeg struct {
	coord    float64
	from, to float64
}

// detectTable finds ruled tables as connected components of crossing
// border lines, and fills each grid's cells with words. Returns the
// tables sorted by area (largest first).
func detectTable(drawings []Drawing, lines []TextLine) []Table {
	var hs, vs []lineSeg
	addRectEdges := func(r [4]float64) {
		x, y, w, h := r[0], r[1], r[2], r[3]
		if w <= 0 || h <= 0 {
			return
		}
		hs = append(hs, lineSeg{y, x, x + w}, lineSeg{y + h, x, x + w})
		vs = append(vs, lineSeg{x, y, y + h}, lineSeg{x + w, y, y + h})
	}
	for _, d := range drawings {
		if d.IsRect {
			if d.Op != "s" && d.Op != "f" && d.Op != "B" {
				continue
			}
			addRectEdges(d.Rect)
			continue
		}
		for i := 0; i+1 < len(d.Points); i++ {
			p0, p1 := d.Points[i], d.Points[i+1]
			switch {
			case math.Abs(p0[1]-p1[1]) <= 1 && math.Abs(p0[0]-p1[0]) > 1:
				a, b := p0[0], p1[0]
				if a > b {
					a, b = b, a
				}
				hs = append(hs, lineSeg{p0[1], a, b})
			case math.Abs(p0[0]-p1[0]) <= 1 && math.Abs(p0[1]-p1[1]) > 1:
				a, b := p0[1], p1[1]
				if a > b {
					a, b = b, a
				}
				vs = append(vs, lineSeg{p0[0], a, b})
			}
		}
	}
	// guard against pathological pages
	if len(hs) > 2000 {
		hs = hs[:2000]
	}
	if len(vs) > 2000 {
		vs = vs[:2000]
	}
	if len(hs) < 2 || len(vs) < 2 {
		return nil
	}

	// union-find over segments, linked by crossings (V.x inside H's x-range
	// AND H.y inside V's y-range): a table's lines mutually cross.
	nh, nv := len(hs), len(vs)
	parent := make([]int, nh+nv)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	const tol = 1.5
	for i := range hs {
		h := hs[i]
		for j := range vs {
			v := vs[j]
			if v.coord >= h.from-tol && v.coord <= h.to+tol &&
				h.coord >= v.from-tol && h.coord <= v.to+tol {
				union(i, nh+j)
			}
		}
	}

	// group segment indices per component
	type comp struct {
		hIdx, vIdx []int
	}
	comps := map[int]*comp{}
	for i := range hs {
		r := find(i)
		c := comps[r]
		if c == nil {
			c = &comp{}
			comps[r] = c
		}
		c.hIdx = append(c.hIdx, i)
	}
	for j := range vs {
		r := find(nh + j)
		c := comps[r]
		if c == nil {
			c = &comp{}
			comps[r] = c
		}
		c.vIdx = append(c.vIdx, j)
	}

	var tables []Table
	for _, c := range comps {
		if len(c.hIdx) < 2 || len(c.vIdx) < 2 {
			continue
		}
		cHS := make([]lineSeg, 0, len(c.hIdx))
		for _, i := range c.hIdx {
			cHS = append(cHS, hs[i])
		}
		cVS := make([]lineSeg, 0, len(c.vIdx))
		for _, j := range c.vIdx {
			cVS = append(cVS, vs[j])
		}
		rowYs := clusterCoords(cHS, 2.0)
		colXs := clusterCoords(cVS, 2.0)
		if len(rowYs) < 3 || len(colXs) < 2 {
			continue
		}
		// bounds and coverage filter
		yRange := rowYs[len(rowYs)-1] - rowYs[0]
		xRange := colXs[len(colXs)-1] - colXs[0]
		if yRange < 20 || xRange < 40 {
			continue
		}
		var keepRows, keepCols []float64
		for _, y := range rowYs {
			cov := 0.0
			for _, s := range cHS {
				if math.Abs(s.coord-y) <= 2 {
					cov += s.to - s.from
				}
			}
			if cov >= 0.25*xRange {
				keepRows = append(keepRows, y)
			}
		}
		for _, x := range colXs {
			cov := 0.0
			for _, s := range cVS {
				if math.Abs(s.coord-x) <= 2 {
					cov += s.to - s.from
				}
			}
			if cov >= 0.25*yRange {
				keepCols = append(keepCols, x)
			}
		}
		if len(keepCols) < 2 || len(keepRows) < 3 {
			continue
		}
		keepRows = trimEmptyRowBands(keepRows, keepCols, lines)
		if len(keepRows) < 3 {
			continue
		}
		t := Table{
			X:        keepCols[0],
			Y:        keepRows[0],
			W:        keepCols[len(keepCols)-1] - keepCols[0],
			H:        keepRows[len(keepRows)-1] - keepRows[0],
			ColCount: len(keepCols) - 1,
			Rows:     make([][]TableCell, 0, len(keepRows)-1),
		}
		for r := 0; r+1 < len(keepRows); r++ {
			row := make([]TableCell, 0, len(keepCols)-1)
			for c := 0; c+1 < len(keepCols); c++ {
				cell := TableCell{
					X: keepCols[c] + 1, Y: keepRows[r] + 1,
					W: keepCols[c+1] - keepCols[c] - 2, H: keepRows[r+1] - keepRows[r] - 2,
				}
				cell.Text = cellText(lines, cell)
				row = append(row, cell)
			}
			t.Rows = append(t.Rows, row)
		}
		tables = append(tables, t)
	}
	// largest first
	sort.Slice(tables, func(i, j int) bool { return tables[i].W*tables[i].H > tables[j].W*tables[j].H })
	return tables
}

// clusterCoords merges nearby segment coordinates into cluster centers. merges nearby segment coordinates into cluster centers.
func clusterCoords(segs []lineSeg, tol float64) []float64 {
	vals := make([]float64, 0, len(segs))
	for _, s := range segs {
		vals = append(vals, s.coord)
	}
	sort.Float64s(vals)
	var out []float64
	sum, n, start := vals[0], 1, vals[0]
	for i := 1; i < len(vals); i++ {
		if vals[i]-start <= tol {
			sum += vals[i]
			n++
			continue
		}
		out = append(out, sum/float64(n))
		sum, n, start = vals[i], 1, vals[i]
	}
	out = append(out, sum/float64(n))
	return out
}

// cellText joins the words whose center falls inside the cell.
func cellText(lines []TextLine, cell TableCell) string {
	type yx struct {
		y, x float64
		text string
	}
	var words []yx
	for _, ln := range lines {
		for _, w := range ln.Words {
			cx := w.X + w.Width/2
			cy := w.Y - w.Height/2
			if cx >= cell.X && cx <= cell.X+cell.W && cy >= cell.Y && cy <= cell.Y+cell.H {
				words = append(words, yx{w.Y, w.X, w.Text})
			}
		}
	}
	if len(words) == 0 {
		return ""
	}
	sort.Slice(words, func(i, j int) bool {
		if math.Abs(words[i].y-words[j].y) > 2 {
			return words[i].y < words[j].y
		}
		return words[i].x < words[j].x
	})
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, w.text)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// trimEmptyRowBands removes leading/trailing row boundaries whose band
// contains no text (common for pre-drawn empty table rows).
func trimEmptyRowBands(rows, cols []float64, lines []TextLine) []float64 {
	bandHasText := func(y0, y1 float64) bool {
		for _, ln := range lines {
			for _, w := range ln.Words {
				cy := w.Y - w.Height/2
				if cy >= y0 && cy <= y1 {
					return true
				}
			}
		}
		return false
	}
	start, end := 0, len(rows)-1
	for end-start >= 2 && !bandHasText(rows[start], rows[start+1]) {
		start++
	}
	for end-start >= 2 && !bandHasText(rows[end-1], rows[end]) {
		end--
	}
	return rows[start : end+1]
}

// ToCSV renders the table as CSV (comma-separated, quoted per RFC 4180).
func (t *Table) ToCSV() string {
	var sb strings.Builder
	for _, row := range t.Rows {
		cells := make([]string, 0, len(row))
		for _, c := range row {
			cells = append(cells, csvQuote(c.Text))
		}
		sb.WriteString(strings.Join(cells, ","))
		sb.WriteString("\n")
	}
	return sb.String()
}

// csvQuote quotes a CSV field when it contains separators, quotes or newlines.
func csvQuote(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}
