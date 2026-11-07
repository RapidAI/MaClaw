package gopdf

import (
	"bytes"
	"image"
	"image/color"
	"math"
	"sort"
	"strings"
)

// ============================================================
// Text rasterization for RenderPageToImage.
// Resolves embedded TrueType fonts (/FontFile2), decodes text operations
// from the content stream and rasterizes glyph outlines with an even-odd
// scanline fill. Identity-H codes map directly to glyph IDs; simple-font
// bytes map through the font's cmap.
// ============================================================

// renderFont is a parsed embedded font for rasterization.
type renderFont struct {
	ttf      *minTTF
	isType0  bool // Identity-H: 2-byte codes are glyph IDs
	codeSize int
}

// renderTextState tracks the PDF text state while rendering.
type renderTextState struct {
	font        *renderFont
	fontSize    float64
	tmX, tmY    float64 // text matrix position (PDF space, y-up)
	tlmX        float64
	tlmY        float64
	leading     float64
	fill        color.RGBA
	charSpacing float64 // Tc: extra spacing per glyph
	wordSpacing float64 // Tw: extra spacing per space char
}

// resolveRenderFonts builds resource-name -> parsed embedded font mappings
// for the page (FontFile2 TrueType only).
func resolveRenderFonts(parser *rawPDFParser, page rawPDFPage) map[string]*renderFont {
	fonts := make(map[string]*renderFont, len(page.resources.fonts))
	for name, objNum := range page.resources.fonts {
		obj, ok := parser.objects[objNum]
		if !ok {
			continue
		}
		// FontDescriptor -> FontFile2. For Type0 fonts the descriptor lives
		// on the descendant CID font, not on the top-level dict.
		fdRef := extractRef(obj.dict, "/FontDescriptor")
		if fdRef <= 0 {
			if descRef := firstRefInArray(obj.dict, "/DescendantFonts"); descRef > 0 {
				if descObj, ok := parser.objects[descRef]; ok {
					fdRef = extractRef(descObj.dict, "/FontDescriptor")
				}
			}
		}
		if fdRef <= 0 {
			continue
		}
		fdObj, ok := parser.objects[fdRef]
		if !ok {
			continue
		}
		ff2Ref := extractRef(fdObj.dict, "/FontFile2")
		if ff2Ref <= 0 {
			continue
		}
		ff2Obj, ok := parser.objects[ff2Ref]
		if !ok || ff2Obj.stream == nil {
			continue
		}
		fontData := ff2Obj.stream
		m, err := parseMinTTF(fontData)
		if err != nil {
			continue
		}
		rf := &renderFont{ttf: m}
		rf.isType0 = strings.Contains(obj.dict, "/Type0") ||
			strings.Contains(obj.dict, "/Identity-H")
		if rf.isType0 {
			rf.codeSize = 2
		} else {
			rf.codeSize = 1
		}
		fonts[name] = rf
	}
	return fonts
}

// drawTextRun rasterizes one text run at the given pen position (device
// pixels), filling glyph outlines with the fill color.
func drawTextRun(img *image.RGBA, font *renderFont, codes []byte, fontSize float64, penX, penY float64, fill color.RGBA, csPx, wsPx float64) {
	if font == nil || font.ttf == nil || len(codes) == 0 {
		return
	}
	scale := fontSize / float64(font.ttf.unitsPerEm)

	for i := 0; i < len(codes); i += font.codeSize {
		var gid uint16
		var isSpace bool
		if font.codeSize == 2 && i+1 < len(codes) {
			gid = uint16(codes[i])<<8 | uint16(codes[i+1])
			isSpace = gid == 3 // common space GID in Identity-H subsets
		} else {
			gid = font.ttf.charToGlyph(int(codes[i]))
			isSpace = codes[i] == 32
		}
		if gid == 0 {
			continue
		}
		contours, err := font.ttf.glyphOutline(gid, 0)
		if err != nil || len(contours) == 0 {
			penX += font.ttf.glyphWidth(gid) * scale
			if isSpace {
				penX += wsPx
			}
			continue
		}
		// glyph origin at pen, y flipped (font y-up -> image y-down)
		drawGlyphContours(img, contours, scale, penX, penY, fill)
		penX += font.ttf.glyphWidth(gid)*scale + csPx
		if isSpace {
			penX += wsPx
		}
	}
}

// drawGlyphContours flattens quadratic outlines and fills them.
func drawGlyphContours(img *image.RGBA, contours [][]glyphPt, scale, penX, penY float64, fill color.RGBA) {
	var polys [][]image.Point
	for _, contour := range contours {
		if len(contour) < 3 {
			continue
		}
		// build the on/off curve point cycle
		pts := make([]glyphPt, 0, len(contour)*2)
		n := len(contour)
		for i := 0; i < n; i++ {
			cur := contour[i]
			next := contour[(i+1)%n]
			pts = append(pts, cur)
			if !cur.onCurve && !next.onCurve {
				// implicit on-curve midpoint between two control points
				pts = append(pts, glyphPt{
					x:       (cur.x + next.x) / 2,
					y:       (cur.y + next.y) / 2,
					onCurve: true,
				})
			}
		}
		// flatten quadratics: for each on-curve vertex with a following
		// off-curve point, sample the quad to the next on-curve point
		var poly []image.Point
		m := len(pts)
		for i := 0; i < m; i++ {
			cur := pts[i]
			px := penX + cur.x*scale
			py := penY - cur.y*scale
			poly = append(poly, image.Point{X: int(px), Y: int(py)})
			if !cur.onCurve {
				next := pts[(i+1)%m]
				if next.onCurve {
					// quadratic: cur (control) -> next (on)
					// sample 8 segments
					start := pts[(i-1+m)%m]
					if start.onCurve {
						sx := penX + start.x*scale
						sy := penY - start.y*scale
						ex := penX + next.x*scale
						ey := penY - next.y*scale
						cx := penX + cur.x*scale
						cy := penY - cur.y*scale
						for s := 1; s < 8; s++ {
							t := float64(s) / 8
							mt := 1 - t
							qx := mt*mt*sx + 2*mt*t*cx + t*t*ex
							qy := mt*mt*sy + 2*mt*t*cy + t*t*ey
							poly = append(poly, image.Point{X: int(qx), Y: int(qy)})
						}
					}
				}
			}
		}
		if len(poly) >= 3 {
			polys = append(polys, poly)
		}
	}
	fillPolygonEvenOdd(img, polys, fill)
}

// fillPolygonEvenOdd rasterizes polygons with the even-odd rule using a
// scanline crossing algorithm (covers holes in glyphs like 'o').
func fillPolygonEvenOdd(img *image.RGBA, polys [][]image.Point, fill color.RGBA) {
	if len(polys) == 0 {
		return
	}
	// collect all edges
	type edge struct{ x1, y1, x2, y2 float64 }
	var edges []edge
	bounds := img.Bounds()
	minY, maxY := bounds.Max.Y, bounds.Min.Y
	for _, poly := range polys {
		for i := 0; i < len(poly); i++ {
			a := poly[i]
			b := poly[(i+1)%len(poly)]
			if a.Y == b.Y {
				continue
			}
			edges = append(edges, edge{float64(a.X), float64(a.Y), float64(b.X), float64(b.Y)})
			if a.Y < minY {
				minY = a.Y
			}
			if a.Y > maxY {
				maxY = a.Y
			}
		}
	}
	if minY < bounds.Min.Y {
		minY = bounds.Min.Y
	}
	if maxY > bounds.Max.Y {
		maxY = bounds.Max.Y
	}

	for y := minY; y <= maxY; y++ {
		sy := float64(y) + 0.5
		var xs []float64
		for _, e := range edges {
			y1, y2 := e.y1, e.y2
			if (sy >= y1 && sy < y2) || (sy >= y2 && sy < y1) {
				t := (sy - y1) / (y2 - y1)
				x := e.x1 + t*(e.x2-e.x1)
				xs = append(xs, x)
			}
		}
		sort.Float64s(xs)
		// paint spans with per-pixel coverage for anti-aliased edges
		for i := 0; i+1 < len(xs); i += 2 {
			xStart := xs[i]
			xEnd := xs[i+1]
			pxStart := int(math.Floor(xStart))
			pxEnd := int(math.Floor(xEnd))
			for x := pxStart; x <= pxEnd; x++ {
				if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
					continue
				}
				// compute pixel coverage [0,1]
				coverL := math.Max(float64(x), xStart)
				coverR := math.Min(float64(x+1), xEnd)
				cov := coverR - coverL
				if cov <= 0 {
					continue
				}
				if cov >= 1 {
					img.SetRGBA(x, y, fill)
				} else {
					// blend: new = fill*cov + old*(1-cov)
					old := img.RGBAAt(x, y)
					blended := color.RGBA{
						R: uint8(float64(fill.R)*cov + float64(old.R)*(1-cov)),
						G: uint8(float64(fill.G)*cov + float64(old.G)*(1-cov)),
						B: uint8(float64(fill.B)*cov + float64(old.B)*(1-cov)),
						A: 255,
					}
					img.SetRGBA(x, y, blended)
				}
			}
		}
	}
}

// renderDrawText handles a show-text operator inside BT/ET during rendering.
func renderDrawText(img *image.RGBA, ts *renderTextState, rawToken []byte, pageH, scale float64, fontResources map[string]*renderFont) {
	if ts.font == nil {
		return
	}
	var data []byte
	if len(rawToken) >= 2 && rawToken[0] == '(' && rawToken[len(rawToken)-1] == ')' {
		data = []byte(unescapePDFString(string(rawToken[1 : len(rawToken)-1])))
	} else if len(rawToken) >= 2 && rawToken[0] == '<' && rawToken[len(rawToken)-1] == '>' {
		data = hexBytes(trimPDFWS(rawToken[1 : len(rawToken)-1]))
	} else {
		// raw code bytes passed directly (TJ items are pre-stripped)
		data = rawToken
	}
	penX := ts.tmX
	penY := pageH - ts.tmY
	drawTextRun(img, ts.font, data, ts.fontSize, penX*scale, penY*scale, ts.fill, ts.charSpacing*scale, ts.wordSpacing*scale)
}

// renderTf handles the Tf operator: select a font resource and size.
func renderTf(ts *renderTextState, fontResources map[string]*renderFont, name []byte, size float64) {
	if f, ok := fontResources[string(name)]; ok {
		ts.font = f
	}
	ts.fontSize = size
}

// renderTextOpBytes extracts the raw string token before a show-text op.
func renderTextOpBytes(tokens [][]byte, i int) []byte {
	if i >= 1 && isStringToken(tokens[i-1]) {
		return tokens[i-1]
	}
	return nil
}

// unused shims kept minimal
var _ = bytes.MinRead

// firstRefInArray extracts the first "N 0 R" reference following a key whose
// value is an array (e.g. /DescendantFonts [9 0 R]).
func firstRefInArray(dict, key string) int {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return 0
	}
	rest := dict[idx+len(key):]
	start := strings.Index(rest, "[")
	if start < 0 {
		return 0
	}
	end := strings.Index(rest[start:], "]")
	if end < 0 {
		return 0
	}
	m := reObjRef.FindStringSubmatch(rest[start : start+end])
	if m == nil {
		return 0
	}
	n := 0
	for _, ch := range m[1] {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// helper re-exported for the probe above

// pageFontsWithForm merges a form's own /Font resources with the page-level
// ones so text inside the form resolves its font correctly.
func pageFontsWithForm(parser *rawPDFParser, pageFonts map[string]int, formDict string) map[string]int {
	merged := make(map[string]int, len(pageFonts)+4)
	for n, o := range pageFonts {
		merged[n] = o
	}
	resDict := formDict
	if resRef := extractRef(formDict, "/Resources"); resRef > 0 {
		if obj, ok := parser.objects[resRef]; ok {
			resDict = obj.dict
		}
	}
	formFonts := make(map[string]int)
	parser.extractNamedRefs(resDict, "/Font", formFonts)
	for n, o := range formFonts {
		if _, clash := merged[n]; !clash {
			merged[n] = o
		}
	}
	return merged
}
