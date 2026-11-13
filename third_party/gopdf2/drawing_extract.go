package gopdf

import (
	"fmt"
	"math"
	"strings"
)

// ============================================================
// Vector graphics extraction — PyMuPDF parity: page.get_drawings().
// Walks the content stream (and form XObjects) tracking the graphics
// state, and returns painted paths: lines, polylines, rects, curves.
// Coordinates are visual page points (top-down, honoring /Rotate).
// ============================================================

// Drawing is one painted vector path.
type Drawing struct {
	// Op is the paint operator: "s" stroke, "f" fill, "B" fill+stroke.
	Op string
	// Color is the stroke color (0-1 RGB).
	Color [3]float64
	// Fill is the fill color (0-1 RGB).
	Fill [3]float64
	// Width is the stroke width in points.
	Width float64
	// Points are the path vertices (polyline; bezier curves flattened).
	Points [][2]float64
	// IsRect marks paths that came from a "re" operator.
	IsRect bool
	// Rect is the rectangle [x y w h] for "re" paths (visual space).
	Rect [4]float64
}

// ExtractPageDrawings extracts the vector graphics of one page.
//
// Example:
//
//	drawings, err := gopdf.ExtractPageDrawings(data, 0)
//	for _, d := range drawings {
//	    if d.IsRect { fmt.Println("rect at", d.Rect) }
//	}
func ExtractPageDrawings(pdfData []byte, pageIndex int) ([]Drawing, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	return extractDrawingsWithParser(parser, pageIndex)
}

// extractDrawingsWithParser extracts vector graphics using an existing parser.
func extractDrawingsWithParser(parser *rawPDFParser, pageIndex int) ([]Drawing, error) {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(parser.pages)-1)
	}
	page := parser.pages[pageIndex]
	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return nil, nil
	}
	d := &drawingExtractor{
		parser: parser,
		xobjs:  page.resources.xobjs,
		rotate: page.rotate,
		mbW:    page.mediaBox[2] - page.mediaBox[0],
		mbH:    page.mediaBox[3] - page.mediaBox[1],
		ctmA:   1, ctmD: 1,
		stroke: [3]float64{0, 0, 0},
		width:  1,
	}
	d.walk(stream, 0)
	return d.out, nil
}

// ExtractDrawingsFromFile extracts the vector graphics of a page from a file.
func ExtractDrawingsFromFile(path string, pageIndex int) ([]Drawing, error) {
	data, err := readFileBytes(path)
	if err != nil {
		return nil, err
	}
	return ExtractPageDrawings(data, pageIndex)
}

// drawingState is one level of the q/Q graphics state stack.
type drawingState struct {
	ctmA, ctmD, ctmE, ctmF float64
	stroke, fill           [3]float64
	width                  float64
}

type drawingExtractor struct {
	parser *rawPDFParser
	xobjs  map[string]int
	rotate int
	mbW    float64
	mbH    float64

	out   []Drawing
	stack []drawingState

	ctmA, ctmD, ctmE, ctmF float64
	stroke, fill           [3]float64
	width                  float64

	// current path (user space, y-up points transformed to page space)
	path     [][2]float64
	pathUser [][2]float64
	subStart int // index of the current subpath start
	isRect   bool
	rect     [4]float64 // user-space rect
}

// toVisual maps page-space coordinates (y-up, after the CTM) to visual
// top-down points, honoring /Rotate. Rotating the page 90° clockwise maps
// the user bottom-left corner to the visual top-left, etc.
func (d *drawingExtractor) toVisual(x, y float64) [2]float64 {
	switch d.rotate {
	case 90:
		return [2]float64{y, x}
	case 180:
		return [2]float64{d.mbW - x, y}
	case 270:
		return [2]float64{d.mbH - y, d.mbW - x}
	}
	return [2]float64{x, d.mbH - y}
}

// transform maps a user-space point through the CTM to page space (y-up).
func (d *drawingExtractor) transform(x, y float64) [2]float64 {
	return [2]float64{d.ctmA*x + d.ctmE, d.ctmD*y + d.ctmF}
}

func (d *drawingExtractor) walk(stream []byte, depth int) {
	if depth > 4 {
		return
	}
	tokens := tokenizeBytes(stream)
	var stack []float64
	push := func(v float64) { stack = append(stack, v) }
	pop := func(n int) []float64 {
		if len(stack) < n {
			stack = stack[:0]
			return nil
		}
		vals := stack[len(stack)-n:]
		stack = stack[:len(stack)-n]
		return vals
	}

	for i := 0; i < len(tokens); i++ {
		if v, ok := fastParseFloat(tokens[i]); ok {
			push(v)
			continue
		}
		op := string(tokens[i])
		switch op {
		case "q":
			d.stack = append(d.stack, drawingState{d.ctmA, d.ctmD, d.ctmE, d.ctmF, d.stroke, d.fill, d.width})
		case "Q":
			if len(d.stack) > 0 {
				s := d.stack[len(d.stack)-1]
				d.stack = d.stack[:len(d.stack)-1]
				d.ctmA, d.ctmD, d.ctmE, d.ctmF = s.ctmA, s.ctmD, s.ctmE, s.ctmF
				d.stroke, d.fill, d.width = s.stroke, s.fill, s.width
			}
			d.path = d.path[:0]
			d.pathUser = d.pathUser[:0]
		case "cm":
			if a := pop(6); a != nil {
				// newCTM = cm x CTM
				na, nb, nc, nd := a[0], a[1], a[2], a[3]
				ne, nf := a[4], a[5]
				d.ctmE = ne*d.ctmA + nf*0 + d.ctmE
				d.ctmF = ne*0 + nf*d.ctmD + d.ctmF
				d.ctmA = na*d.ctmA + nb*0
				d.ctmD = nc*0 + nd*d.ctmD
			}
		case "w":
			if a := pop(1); a != nil && a[0] > 0 {
				d.width = a[0]
			}
		case "RG":
			if a := pop(3); a != nil {
				d.stroke = [3]float64{clamp01v(a[0]), clamp01v(a[1]), clamp01v(a[2])}
			}
		case "K":
			if a := pop(4); a != nil {
				r, g, b := cmykToRGB(a[0], a[1], a[2], a[3])
				d.stroke = [3]float64{r, g, b}
			}
		case "G":
			if a := pop(1); a != nil {
				v := clamp01v(a[0])
				d.stroke = [3]float64{v, v, v}
			}
		case "g":
			if a := pop(1); a != nil {
				v := clamp01v(a[0])
				d.fill = [3]float64{v, v, v}
			}
		case "rg":
			if a := pop(3); a != nil {
				d.fill = [3]float64{clamp01v(a[0]), clamp01v(a[1]), clamp01v(a[2])}
			}
		case "m":
			if a := pop(2); a != nil {
				d.pathUser = append(d.pathUser, [2]float64{a[0], a[1]})
				d.subStart = len(d.pathUser) - 1
			}
		case "l":
			if a := pop(2); a != nil {
				d.pathUser = append(d.pathUser, [2]float64{a[0], a[1]})
			}
		case "c":
			// cubic bezier: x1 y1 x2 y2 x3 y3
			if a := pop(6); a != nil && len(d.pathUser) > 0 {
				d.appendBezier(a[0], a[1], a[2], a[3], a[4], a[5])
			}
		case "v":
			// first control = current point
			if a := pop(4); a != nil && len(d.pathUser) > 0 {
				cur := d.pathUser[len(d.pathUser)-1]
				d.appendBezier(cur[0], cur[1], a[0], a[1], a[2], a[3])
			}
		case "y":
			if a := pop(4); a != nil && len(d.pathUser) > 0 {
				d.appendBezier(a[0], a[1], a[2], a[3], a[2], a[3])
			}
		case "h":
			if len(d.pathUser) > 0 {
				d.pathUser = append(d.pathUser, d.pathUser[d.subStart])
			}
		case "re":
			if a := pop(4); a != nil {
				d.isRect = true
				d.rect = [4]float64{a[0], a[1], a[2], a[3]}
				x, y, w, h := a[0], a[1], a[2], a[3]
				d.pathUser = append(d.pathUser,
					[2]float64{x, y}, [2]float64{x + w, y},
					[2]float64{x + w, y + h}, [2]float64{x, y + h},
					[2]float64{x, y})
			}
		case "S", "s", "f", "F", "f*", "B", "B*", "b", "b*":
			d.paint(op, depth)
			stack = stack[:0]
		case "n":
			d.path = d.path[:0]
			d.pathUser = d.pathUser[:0]
			d.isRect = false
			stack = stack[:0]
		case "W", "W*":
			// clipping — ignored
		case "Do":
			if i >= 1 && strings.HasPrefix(string(tokens[i-1]), "/") {
				name := string(tokens[i-1][1:])
				d.doForm(name, depth)
			}
		case "sh", "BI":
			// shadings and inline images — not vector paths
			stack = stack[:0]
		default:
			if !strings.HasPrefix(op, "/") {
				stack = stack[:0]
			}
		}
	}
}

// paint converts the accumulated user-space path to page/visual space and
// records the drawing.
func (d *drawingExtractor) paint(op string, depth int) {
	if len(d.pathUser) == 0 {
		return
	}
	d.path = d.path[:0]
	for _, p := range d.pathUser {
		tp := d.transform(p[0], p[1])
		d.path = append(d.path, d.toVisual(tp[0], tp[1]))
	}
	// copy: d.path's backing array is reused across paints
	pts := make([][2]float64, len(d.path))
	copy(pts, d.path)
	draw := Drawing{Op: "s", Color: d.stroke, Fill: d.fill, Width: d.width * math.Abs(d.ctmA), Points: pts}
	switch op {
	case "f", "F", "f*":
		draw.Op = "f"
	case "B", "B*", "b", "b*":
		draw.Op = "B"
	case "s":
		// close + stroke: append the subpath start (already done via h for
		// explicit "h"; "s" closes implicitly)
		if len(d.path) > 2 {
			draw.Points = append(draw.Points, d.path[d.subStart])
		}
	}
	if d.isRect {
		draw.IsRect = true
		// rect in visual space: transform two opposite corners
		c1 := d.transform(d.rect[0], d.rect[1])
		c2 := d.transform(d.rect[0]+d.rect[2], d.rect[1]+d.rect[3])
		p1 := d.toVisual(c1[0], c1[1])
		p2 := d.toVisual(c2[0], c2[1])
		x0, y0 := math.Min(p1[0], p2[0]), math.Min(p1[1], p2[1])
		x1, y1 := math.Max(p1[0], p2[0]), math.Max(p1[1], p2[1])
		draw.Rect = [4]float64{x0, y0, x1 - x0, y1 - y0}
	}
	d.out = append(d.out, draw)
	d.path = d.path[:0]
	d.pathUser = d.pathUser[:0]
	d.isRect = false
}

// appendBezier flattens a cubic bezier segment into 8 line segments.
func (d *drawingExtractor) appendBezier(x1, y1, x2, y2, x3, y3 float64) {
	const steps = 8
	start := d.pathUser[len(d.pathUser)-1]
	for i := 1; i <= steps; i++ {
		t := float64(i) / steps
		mt := 1 - t
		x := mt*mt*mt*start[0] + 3*mt*mt*t*x1 + 3*mt*t*t*x2 + t*t*t*x3
		y := mt*mt*mt*start[1] + 3*mt*mt*t*y1 + 3*mt*t*t*y2 + t*t*t*y3
		d.pathUser = append(d.pathUser, [2]float64{x, y})
	}
}

// doForm recurses into a form XObject, composing its matrix.
func (d *drawingExtractor) doForm(name string, depth int) {
	objNum, ok := d.xobjs[name]
	if !ok {
		return
	}
	obj, ok := d.parser.objects[objNum]
	if !ok || obj.stream == nil {
		return
	}
	if !strings.Contains(obj.dict, "/Subtype /Form") && !strings.Contains(obj.dict, "/Subtype/Form") {
		return
	}
	// save state
	saved := drawingState{d.ctmA, d.ctmD, d.ctmE, d.ctmF, d.stroke, d.fill, d.width}
	savedPath := append([][2]float64{}, d.pathUser...)
	savedRect := d.isRect
	// compose the form matrix if present
	if fm := extractNumberArray(obj.dict, "/Matrix"); len(fm) == 6 {
		d.ctmE = fm[4]*d.ctmA + d.ctmE
		d.ctmF = fm[5]*d.ctmD + d.ctmF
		d.ctmA = fm[0] * d.ctmA
		d.ctmD = fm[3] * d.ctmD
	}
	// nested xobjects
	savedXobjs := d.xobjs
	if nested := cachedFormXObjects(d.parser, objNum, obj.dict); len(nested) > 0 {
		merged := make(map[string]int, len(d.xobjs)+len(nested))
		for k, v := range d.xobjs {
			merged[k] = v
		}
		for k, v := range nested {
			merged[k] = v
		}
		d.xobjs = merged
	}
	d.walk(obj.stream, depth+1)
	d.xobjs = savedXobjs
	d.ctmA, d.ctmD, d.ctmE, d.ctmF = saved.ctmA, saved.ctmD, saved.ctmE, saved.ctmF
	d.stroke, d.fill, d.width = saved.stroke, saved.fill, saved.width
	d.pathUser = savedPath
	d.isRect = savedRect
}



func clamp01v(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
