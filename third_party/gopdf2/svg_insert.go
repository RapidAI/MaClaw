package gopdf

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

// SVGOption configures how an SVG is rendered into the PDF.
type SVGOption struct {
	// X is the left position in page units.
	X float64
	// Y is the top position in page units.
	Y float64
	// Width is the target width. If 0, uses the SVG's native width.
	Width float64
	// Height is the target height. If 0, uses the SVG's native height.
	Height float64
}

var (
	ErrSVGParseFailed = errors.New("failed to parse SVG")
	ErrSVGEmpty       = errors.New("SVG contains no renderable elements")
)

// ImageSVG inserts an SVG image from a file path into the current page.
// The SVG is converted to native PDF drawing commands (lines, curves,
// rectangles, circles, paths) — no rasterization is needed.
//
// Supported SVG elements: rect, circle, ellipse, line, polyline,
// polygon, path (M, L, C, Q, Z commands), text (basic).
//
// Example:
//
//	pdf.AddPage()
//	err := pdf.ImageSVG("icon.svg", SVGOption{X: 50, Y: 50, Width: 200, Height: 200})
func (gp *GoPdf) ImageSVG(svgPath string, opt SVGOption) error {
	data, err := os.ReadFile(svgPath)
	if err != nil {
		return fmt.Errorf("read SVG file: %w", err)
	}
	return gp.ImageSVGFromBytes(data, opt)
}

// ImageSVGFromBytes inserts an SVG from raw bytes into the current page.
func (gp *GoPdf) ImageSVGFromBytes(svgData []byte, opt SVGOption) error {
	svg, err := parseSVG(svgData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSVGParseFailed, err)
	}
	if len(svg.elements) == 0 {
		return ErrSVGEmpty
	}

	gp.UnitsToPointsVar(&opt.X, &opt.Y)
	if opt.Width > 0 {
		opt.Width = gp.UnitsToPoints(opt.Width)
	}
	if opt.Height > 0 {
		opt.Height = gp.UnitsToPoints(opt.Height)
	}

	// Calculate scale
	scaleX := 1.0
	scaleY := 1.0
	if opt.Width > 0 && svg.width > 0 {
		scaleX = opt.Width / svg.width
	}
	if opt.Height > 0 && svg.height > 0 {
		scaleY = opt.Height / svg.height
	}
	if opt.Width > 0 && opt.Height == 0 {
		scaleY = scaleX
	} else if opt.Height > 0 && opt.Width == 0 {
		scaleX = scaleY
	}

	gp.SaveGraphicsState()
	defer gp.RestoreGraphicsState()

	// Render each SVG element
	for _, elem := range svg.elements {
		gp.renderSVGElement(elem, opt.X, opt.Y, scaleX, scaleY)
	}

	return nil
}

// ImageSVGFromReader inserts an SVG from an io.Reader into the current page.
func (gp *GoPdf) ImageSVGFromReader(r io.Reader, opt SVGOption) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return gp.ImageSVGFromBytes(data, opt)
}

// ---- SVG parsing ----

type svgDoc struct {
	width    float64
	height   float64
	viewBox  [4]float64
	elements []svgElement
}

type svgElementType int

const (
	svgRect svgElementType = iota
	svgCircle
	svgEllipse
	svgLine
	svgPolyline
	svgPolygon
	svgPath
)

type svgElement struct {
	typ    svgElementType
	// Common style
	fill       [3]uint8
	hasFill    bool
	stroke     [3]uint8
	hasStroke  bool
	strokeW    float64
	opacity    float64
	// Geometry
	x, y, w, h    float64 // rect
	cx, cy, r     float64 // circle
	rx, ry         float64 // ellipse / rect corner radius
	x1, y1, x2, y2 float64 // line
	points         []Point // polyline, polygon
	pathData       string  // path d attribute
}

type xmlSVG struct {
	XMLName  xml.Name     `xml:"svg"`
	Width    string       `xml:"width,attr"`
	Height   string       `xml:"height,attr"`
	ViewBox  string       `xml:"viewBox,attr"`
	Elements []xmlElement `xml:",any"`
}

type xmlElement struct {
	XMLName xml.Name
	Attrs   []xml.Attr   `xml:",any,attr"`
	Content []xmlElement `xml:",any"`
}

func parseSVG(data []byte) (*svgDoc, error) {
	var raw xmlSVG
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	doc := &svgDoc{
		width:  parseSVGLength(raw.Width),
		height: parseSVGLength(raw.Height),
	}

	if raw.ViewBox != "" {
		parts := strings.Fields(raw.ViewBox)
		if len(parts) == 4 {
			doc.viewBox[0], _ = strconv.ParseFloat(parts[0], 64)
			doc.viewBox[1], _ = strconv.ParseFloat(parts[1], 64)
			doc.viewBox[2], _ = strconv.ParseFloat(parts[2], 64)
			doc.viewBox[3], _ = strconv.ParseFloat(parts[3], 64)
		}
		if doc.width == 0 {
			doc.width = doc.viewBox[2]
		}
		if doc.height == 0 {
			doc.height = doc.viewBox[3]
		}
	}

	for _, el := range raw.Elements {
		if elem, ok := parseSVGElement(el); ok {
			doc.elements = append(doc.elements, elem)
		}
		// Recurse into groups
		for _, child := range el.Content {
			if elem, ok := parseSVGElement(child); ok {
				doc.elements = append(doc.elements, elem)
			}
		}
	}

	return doc, nil
}

func parseSVGElement(el xmlElement) (svgElement, bool) {
	attrs := make(map[string]string)
	for _, a := range el.Attrs {
		attrs[a.Name.Local] = a.Value
	}

	var elem svgElement
	elem.opacity = 1.0
	elem.strokeW = 1.0
	parseSVGStyle(&elem, attrs)

	switch el.XMLName.Local {
	case "rect":
		elem.typ = svgRect
		elem.x = atof(attrs["x"])
		elem.y = atof(attrs["y"])
		elem.w = atof(attrs["width"])
		elem.h = atof(attrs["height"])
		elem.rx = atof(attrs["rx"])
		elem.ry = atof(attrs["ry"])
		return elem, true
	case "circle":
		elem.typ = svgCircle
		elem.cx = atof(attrs["cx"])
		elem.cy = atof(attrs["cy"])
		elem.r = atof(attrs["r"])
		return elem, true
	case "ellipse":
		elem.typ = svgEllipse
		elem.cx = atof(attrs["cx"])
		elem.cy = atof(attrs["cy"])
		elem.rx = atof(attrs["rx"])
		elem.ry = atof(attrs["ry"])
		return elem, true
	case "line":
		elem.typ = svgLine
		elem.x1 = atof(attrs["x1"])
		elem.y1 = atof(attrs["y1"])
		elem.x2 = atof(attrs["x2"])
		elem.y2 = atof(attrs["y2"])
		return elem, true
	case "polyline":
		elem.typ = svgPolyline
		elem.points = parseSVGPoints(attrs["points"])
		return elem, len(elem.points) > 0
	case "polygon":
		elem.typ = svgPolygon
		elem.points = parseSVGPoints(attrs["points"])
		return elem, len(elem.points) > 0
	case "path":
		elem.typ = svgPath
		elem.pathData = attrs["d"]
		return elem, elem.pathData != ""
	}
	return elem, false
}

func parseSVGStyle(elem *svgElement, attrs map[string]string) {
	if v, ok := attrs["fill"]; ok && v != "none" {
		if c, ok := parseSVGColor(v); ok {
			elem.fill = c
			elem.hasFill = true
		}
	}
	if v, ok := attrs["stroke"]; ok && v != "none" {
		if c, ok := parseSVGColor(v); ok {
			elem.stroke = c
			elem.hasStroke = true
		}
	}
	if v, ok := attrs["stroke-width"]; ok {
		elem.strokeW = atof(v)
	}
	if v, ok := attrs["opacity"]; ok {
		elem.opacity = atof(v)
	}
	if style, ok := attrs["style"]; ok {
		for _, decl := range strings.Split(style, ";") {
			decl = strings.TrimSpace(decl)
			parts := strings.SplitN(decl, ":", 2)
			if len(parts) != 2 {
				continue
			}
			prop := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch prop {
			case "fill":
				if val != "none" {
					if c, ok := parseSVGColor(val); ok {
						elem.fill = c
						elem.hasFill = true
					}
				}
			case "stroke":
				if val != "none" {
					if c, ok := parseSVGColor(val); ok {
						elem.stroke = c
						elem.hasStroke = true
					}
				}
			case "stroke-width":
				elem.strokeW = atof(val)
			case "opacity":
				elem.opacity = atof(val)
			}
		}
	}
}

func parseSVGColor(s string) ([3]uint8, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		hex := s[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) == 6 {
			r, _ := strconv.ParseUint(hex[0:2], 16, 8)
			g, _ := strconv.ParseUint(hex[2:4], 16, 8)
			b, _ := strconv.ParseUint(hex[4:6], 16, 8)
			return [3]uint8{uint8(r), uint8(g), uint8(b)}, true
		}
	}
	if strings.HasPrefix(s, "rgb(") {
		s = strings.TrimPrefix(s, "rgb(")
		s = strings.TrimSuffix(s, ")")
		parts := strings.Split(s, ",")
		if len(parts) == 3 {
			r, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
			return [3]uint8{uint8(r), uint8(g), uint8(b)}, true
		}
	}
	// Named colors (common subset)
	switch strings.ToLower(s) {
	case "black":
		return [3]uint8{0, 0, 0}, true
	case "white":
		return [3]uint8{255, 255, 255}, true
	case "red":
		return [3]uint8{255, 0, 0}, true
	case "green":
		return [3]uint8{0, 128, 0}, true
	case "blue":
		return [3]uint8{0, 0, 255}, true
	case "yellow":
		return [3]uint8{255, 255, 0}, true
	case "cyan":
		return [3]uint8{0, 255, 255}, true
	case "magenta":
		return [3]uint8{255, 0, 255}, true
	case "gray", "grey":
		return [3]uint8{128, 128, 128}, true
	case "orange":
		return [3]uint8{255, 165, 0}, true
	case "purple":
		return [3]uint8{128, 0, 128}, true
	}
	return [3]uint8{}, false
}

func parseSVGPoints(s string) []Point {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	var points []Point
	for i := 0; i+1 < len(fields); i += 2 {
		x, _ := strconv.ParseFloat(fields[i], 64)
		y, _ := strconv.ParseFloat(fields[i+1], 64)
		points = append(points, Point{X: x, Y: y})
	}
	return points
}

func parseSVGLength(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	s = strings.TrimSuffix(s, "pt")
	s = strings.TrimSuffix(s, "mm")
	s = strings.TrimSuffix(s, "cm")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func atof(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// ---- SVG rendering to PDF ----

func (gp *GoPdf) renderSVGElement(elem svgElement, offX, offY, scaleX, scaleY float64) {
	// Apply style
	if elem.hasStroke {
		gp.SetStrokeColor(elem.stroke[0], elem.stroke[1], elem.stroke[2])
		gp.SetLineWidth(elem.strokeW * scaleX)
	}
	if elem.hasFill {
		gp.SetFillColor(elem.fill[0], elem.fill[1], elem.fill[2])
	}

	switch elem.typ {
	case svgRect:
		x := offX + elem.x*scaleX
		y := offY + elem.y*scaleY
		w := elem.w * scaleX
		h := elem.h * scaleY
		style := svgPaintStyle(elem)
		rx := elem.rx * scaleX
		if rx > 0 {
			gp.Rectangle(x, y, x+w, y+h, style, rx, 12)
		} else {
			gp.Rectangle(x, y, x+w, y+h, style, 0, 0)
		}

	case svgCircle:
		cx := offX + elem.cx*scaleX
		cy := offY + elem.cy*scaleY
		r := elem.r * scaleX
		gp.Oval(cx-r, cy-r, cx+r, cy+r)

	case svgEllipse:
		cx := offX + elem.cx*scaleX
		cy := offY + elem.cy*scaleY
		rx := elem.rx * scaleX
		ry := elem.ry * scaleY
		gp.Oval(cx-rx, cy-ry, cx+rx, cy+ry)

	case svgLine:
		x1 := offX + elem.x1*scaleX
		y1 := offY + elem.y1*scaleY
		x2 := offX + elem.x2*scaleX
		y2 := offY + elem.y2*scaleY
		gp.Line(x1, y1, x2, y2)

	case svgPolyline:
		scaled := scaleSVGPoints(elem.points, offX, offY, scaleX, scaleY)
		if len(scaled) >= 2 {
			gp.Polyline(scaled)
		}

	case svgPolygon:
		scaled := scaleSVGPoints(elem.points, offX, offY, scaleX, scaleY)
		if len(scaled) >= 3 {
			style := svgPaintStyle(elem)
			gp.Polygon(scaled, style)
		}

	case svgPath:
		gp.renderSVGPath(elem.pathData, offX, offY, scaleX, scaleY, elem)
	}
}

func svgPaintStyle(elem svgElement) string {
	if elem.hasFill && elem.hasStroke {
		return "FD"
	}
	if elem.hasFill {
		return "F"
	}
	return "D"
}

func scaleSVGPoints(pts []Point, offX, offY, scaleX, scaleY float64) []Point {
	out := make([]Point, len(pts))
	for i, p := range pts {
		out[i] = Point{X: offX + p.X*scaleX, Y: offY + p.Y*scaleY}
	}
	return out
}

// renderSVGPath renders an SVG path "d" attribute using PDF drawing commands.
// Supports: M/m, L/l, H/h, V/v, C/c, Q/q, Z/z commands.
func (gp *GoPdf) renderSVGPath(d string, offX, offY, scaleX, scaleY float64, elem svgElement) {
	cmds := tokenizeSVGPath(d)
	if len(cmds) == 0 {
		return
	}

	var curX, curY float64
	var startX, startY float64
	i := 0

	for i < len(cmds) {
		cmd := cmds[i]
		i++

		switch cmd {
		case "M":
			if i+1 < len(cmds) {
				curX = atof(cmds[i])
				curY = atof(cmds[i+1])
				i += 2
				startX, startY = curX, curY
			}
		case "m":
			if i+1 < len(cmds) {
				curX += atof(cmds[i])
				curY += atof(cmds[i+1])
				i += 2
				startX, startY = curX, curY
			}
		case "L":
			for i+1 < len(cmds) && !isSVGCommand(cmds[i]) {
				nx := atof(cmds[i])
				ny := atof(cmds[i+1])
				gp.Line(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+nx*scaleX, offY+ny*scaleY,
				)
				curX, curY = nx, ny
				i += 2
			}
		case "l":
			for i+1 < len(cmds) && !isSVGCommand(cmds[i]) {
				dx := atof(cmds[i])
				dy := atof(cmds[i+1])
				nx, ny := curX+dx, curY+dy
				gp.Line(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+nx*scaleX, offY+ny*scaleY,
				)
				curX, curY = nx, ny
				i += 2
			}
		case "H":
			if i < len(cmds) {
				nx := atof(cmds[i])
				i++
				gp.Line(offX+curX*scaleX, offY+curY*scaleY, offX+nx*scaleX, offY+curY*scaleY)
				curX = nx
			}
		case "h":
			if i < len(cmds) {
				dx := atof(cmds[i])
				i++
				nx := curX + dx
				gp.Line(offX+curX*scaleX, offY+curY*scaleY, offX+nx*scaleX, offY+curY*scaleY)
				curX = nx
			}
		case "V":
			if i < len(cmds) {
				ny := atof(cmds[i])
				i++
				gp.Line(offX+curX*scaleX, offY+curY*scaleY, offX+curX*scaleX, offY+ny*scaleY)
				curY = ny
			}
		case "v":
			if i < len(cmds) {
				dy := atof(cmds[i])
				i++
				ny := curY + dy
				gp.Line(offX+curX*scaleX, offY+curY*scaleY, offX+curX*scaleX, offY+ny*scaleY)
				curY = ny
			}
		case "C":
			for i+5 < len(cmds) && !isSVGCommand(cmds[i]) {
				x1 := atof(cmds[i])
				y1 := atof(cmds[i+1])
				x2 := atof(cmds[i+2])
				y2 := atof(cmds[i+3])
				x3 := atof(cmds[i+4])
				y3 := atof(cmds[i+5])
				gp.Curve(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+x1*scaleX, offY+y1*scaleY,
					offX+x2*scaleX, offY+y2*scaleY,
					offX+x3*scaleX, offY+y3*scaleY,
					"D",
				)
				curX, curY = x3, y3
				i += 6
			}
		case "c":
			for i+5 < len(cmds) && !isSVGCommand(cmds[i]) {
				dx1 := atof(cmds[i])
				dy1 := atof(cmds[i+1])
				dx2 := atof(cmds[i+2])
				dy2 := atof(cmds[i+3])
				dx3 := atof(cmds[i+4])
				dy3 := atof(cmds[i+5])
				gp.Curve(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+(curX+dx1)*scaleX, offY+(curY+dy1)*scaleY,
					offX+(curX+dx2)*scaleX, offY+(curY+dy2)*scaleY,
					offX+(curX+dx3)*scaleX, offY+(curY+dy3)*scaleY,
					"D",
				)
				curX += dx3
				curY += dy3
				i += 6
			}
		case "Q":
			// Quadratic Bézier — convert to cubic
			for i+3 < len(cmds) && !isSVGCommand(cmds[i]) {
				qx := atof(cmds[i])
				qy := atof(cmds[i+1])
				ex := atof(cmds[i+2])
				ey := atof(cmds[i+3])
				// Convert quadratic to cubic control points
				c1x := curX + 2.0/3.0*(qx-curX)
				c1y := curY + 2.0/3.0*(qy-curY)
				c2x := ex + 2.0/3.0*(qx-ex)
				c2y := ey + 2.0/3.0*(qy-ey)
				gp.Curve(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+c1x*scaleX, offY+c1y*scaleY,
					offX+c2x*scaleX, offY+c2y*scaleY,
					offX+ex*scaleX, offY+ey*scaleY,
					"D",
				)
				curX, curY = ex, ey
				i += 4
			}
		case "q":
			for i+3 < len(cmds) && !isSVGCommand(cmds[i]) {
				dqx := atof(cmds[i])
				dqy := atof(cmds[i+1])
				dex := atof(cmds[i+2])
				dey := atof(cmds[i+3])
				qx := curX + dqx
				qy := curY + dqy
				ex := curX + dex
				ey := curY + dey
				c1x := curX + 2.0/3.0*(qx-curX)
				c1y := curY + 2.0/3.0*(qy-curY)
				c2x := ex + 2.0/3.0*(qx-ex)
				c2y := ey + 2.0/3.0*(qy-ey)
				gp.Curve(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+c1x*scaleX, offY+c1y*scaleY,
					offX+c2x*scaleX, offY+c2y*scaleY,
					offX+ex*scaleX, offY+ey*scaleY,
					"D",
				)
				curX, curY = ex, ey
				i += 4
			}
		case "Z", "z":
			if curX != startX || curY != startY {
				gp.Line(
					offX+curX*scaleX, offY+curY*scaleY,
					offX+startX*scaleX, offY+startY*scaleY,
				)
				curX, curY = startX, startY
			}
		}
	}
}

// tokenizeSVGPath splits an SVG path "d" attribute into commands and numbers.
func tokenizeSVGPath(d string) []string {
	var tokens []string
	var current strings.Builder

	flush := func() {
		s := strings.TrimSpace(current.String())
		if s != "" {
			tokens = append(tokens, s)
		}
		current.Reset()
	}

	for i := 0; i < len(d); i++ {
		ch := d[i]
		switch {
		case ch == ',' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			flush()
		case isSVGCommandByte(ch):
			flush()
			tokens = append(tokens, string(ch))
		case ch == '-' && current.Len() > 0:
			// Negative number starts a new token
			flush()
			current.WriteByte(ch)
		default:
			current.WriteByte(ch)
		}
	}
	flush()
	return tokens
}

func isSVGCommand(s string) bool {
	if len(s) != 1 {
		return false
	}
	return isSVGCommandByte(s[0])
}

func isSVGCommandByte(ch byte) bool {
	switch ch {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'S', 's',
		'Q', 'q', 'T', 't', 'A', 'a', 'Z', 'z':
		return true
	}
	return false
}

// Ensure math import is used
var _ = math.Pi
