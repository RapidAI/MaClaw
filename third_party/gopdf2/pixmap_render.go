package gopdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
)

// RenderOption configures page rendering to an image.
type RenderOption struct {
	// DPI is the resolution in dots per inch. Default: 72 (1:1 with PDF points).
	DPI float64

	// Background is the background color. Default: white.
	Background color.Color

	// Clip restricts rendering to a region of the page, in visual points
	// (top-down; honoring /Rotate). nil renders the full page.
	Clip *ClipRect
}

func (o *RenderOption) defaults() {
	if o.DPI <= 0 {
		o.DPI = 72
	}
	if o.Background == nil {
		o.Background = color.White
	}
}

// RenderPageToImage renders a page from raw PDF data to an image.Image.
// The pageIndex is 0-based. This provides basic rendering of text placeholders,
// lines, rectangles, and images embedded in the PDF.
//
// Note: This is a lightweight pure-Go renderer. For full-fidelity rendering
// (fonts, complex paths, transparency), a C-based engine like MuPDF is needed.
// This renderer is suitable for thumbnails, previews, and simple PDFs.
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	img, err := gopdf.RenderPageToImage(data, 0, gopdf.RenderOption{DPI: 150})
//	f, _ := os.Create("page0.png")
//	png.Encode(f, img)
func RenderPageToImage(pdfData []byte, pageIndex int, opt RenderOption) (image.Image, error) {
	opt.defaults()
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}
	return renderParsedPage(parser, pageIndex, opt)
}

// RenderAllPagesToImages renders all pages to images. The PDF is parsed
// once (RenderPageToImage re-parses per page, so prefer this for loops).
func RenderAllPagesToImages(pdfData []byte, opt RenderOption) ([]image.Image, error) {
	opt.defaults()
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, fmt.Errorf("parse PDF: %w", err)
	}

	images := make([]image.Image, len(parser.pages))
	for i := range parser.pages {
		img, err := renderParsedPage(parser, i, opt)
		if err != nil {
			return nil, fmt.Errorf("page %d: %w", i, err)
		}
		images[i] = img
	}
	return images, nil
}

// renderParsedPage renders one page of an already-parsed document.
//
// The page is rasterized at 2× the requested resolution and box-filtered
// down (supersampling): thin strokes (hairlines, $-bars, fraction rules)
// and curve edges survive at low DPI instead of vanishing.
func renderParsedPage(parser *rawPDFParser, pageIndex int, opt RenderOption) (image.Image, error) {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(parser.pages)-1)
	}
	page := parser.pages[pageIndex]
	mediaBox := page.mediaBox
	pageW := mediaBox[2] - mediaBox[0]
	pageH := mediaBox[3] - mediaBox[1]

	// supersample at low DPI where hairlines ($ bars, fraction rules)
	// would otherwise vanish; at >= 144 DPI lines already span multiple
	// pixels, so skip the 4x cost
	ss := 1
	if opt.DPI < 144 {
		ss = 2
	}
	scale := opt.DPI / 72.0 * float64(ss)
	imgW := int(math.Ceil(pageW * scale))
	imgH := int(math.Ceil(pageH * scale))
	if imgW < 1 {
		imgW = 1
	}
	if imgH < 1 {
		imgH = 1
	}

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	bg := img.Bounds()
	for y := bg.Min.Y; y < bg.Max.Y; y++ {
		for x := bg.Min.X; x < bg.Max.X; x++ {
			img.Set(x, y, opt.Background)
		}
	}

	stream := parser.getPageContentStream(pageIndex)
	if len(stream) > 0 {
		renderContentStream(img, stream, parser, page, scale, pageH)
	}

	// /CropBox: the visible area — crop the composed page to it (user
	// space y-up) before any rotation
	if page.hasCrop {
		scaleF := opt.DPI / 72.0 * float64(ss)
		cx0 := int((page.cropBox[0] - page.mediaBox[0]) * scaleF)
		cy0 := int((page.mediaBox[3] - page.cropBox[3]) * scaleF)
		cw := int((page.cropBox[2] - page.cropBox[0]) * scaleF)
		ch := int((page.cropBox[3] - page.cropBox[1]) * scaleF)
		if cw > 0 && ch > 0 && cw <= img.Bounds().Dx() && ch <= img.Bounds().Dy() {
			cropped := image.NewRGBA(image.Rect(0, 0, cw, ch))
			for y := 0; y < ch; y++ {
				for x := 0; x < cw; x++ {
					cropped.SetRGBA(x, y, img.RGBAAt(cx0+x, cy0+y))
				}
			}
			img = cropped
		}
	}

	// /Rotate: rotate the composed page — a rigid transform of the whole
	// bitmap is exactly equivalent to rotating the page before drawing.
	switch page.rotate {
	case 90, 180, 270:
		img = rotateImageRGBA(img, page.rotate)
	}

	// box-filter downsample by ss×
	if ss > 1 {
		// dims may have changed if the page was rotated above
		rb := img.Bounds()
		outW := rb.Dx() / ss
		outH := rb.Dy() / ss
		if outW < 1 {
			outW = 1
		}
		if outH < 1 {
			outH = 1
		}
		out := image.NewRGBA(image.Rect(0, 0, outW, outH))
		for y := 0; y < outH; y++ {
			for x := 0; x < outW; x++ {
				var r, g, b float64
				for dy := 0; dy < ss; dy++ {
					sy := y*ss + dy
					if sy >= imgH {
						sy = imgH - 1
					}
					for dx := 0; dx < ss; dx++ {
						sx := x*ss + dx
						if sx >= imgW {
							sx = imgW - 1
						}
						c := img.RGBAAt(sx, sy)
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
					}
				}
				n := float64(ss * ss)
				out.SetRGBA(x, y, color.RGBA{
					R: uint8(r/n + 0.5), G: uint8(g/n + 0.5), B: uint8(b/n + 0.5), A: 255,
				})
			}
		}
		return cropToClip(out, opt), nil
	}
	return cropToClip(img, opt), nil
}

// cropToClip restricts the rendered page to opt.Clip (visual points).
func cropToClip(img *image.RGBA, opt RenderOption) *image.RGBA {
	if opt.Clip == nil {
		return img
	}
	scale := opt.DPI / 72.0
	b := img.Bounds()
	x0 := int(opt.Clip.X * scale)
	y0 := int(opt.Clip.Y * scale)
	x1 := int((opt.Clip.X + opt.Clip.W) * scale)
	y1 := int((opt.Clip.Y + opt.Clip.H) * scale)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	if x1 > b.Max.X {
		x1 = b.Max.X
	}
	if y1 > b.Max.Y {
		y1 = b.Max.Y
	}
	if x1 <= x0 || y1 <= y0 {
		return img
	}
	out := image.NewRGBA(image.Rect(0, 0, x1-x0, y1-y0))
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			out.SetRGBA(x-x0, y-y0, img.RGBAAt(x, y))
		}
	}
	return out
}

// rotateImageRGBA returns img rotated clockwise by 90/180/270 degrees.
func rotateImageRGBA(img *image.RGBA, deg int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	switch deg {
	case 90, 270:
		out := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				c := img.RGBAAt(b.Min.X+x, b.Min.Y+y)
				if deg == 90 {
					out.SetRGBA(h-1-y, x, c)
				} else {
					out.SetRGBA(y, w-1-x, c)
				}
			}
		}
		return out
	case 180:
		out := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				out.SetRGBA(w-1-x, h-1-y, img.RGBAAt(b.Min.X+x, b.Min.Y+y))
			}
		}
		return out
	}
	return img
}

// debugRender enables pixmap_render diagnostics via GOPDF_DEBUG_RENDER.
var debugRender = os.Getenv("GOPDF_DEBUG_RENDER") != ""

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// renderContentStream interprets a PDF content stream and draws onto the image.
func renderContentStream(img *image.RGBA, stream []byte, parser *rawPDFParser, page rawPDFPage, scale, pageH float64) {
	tokens := tokenizeBytes(stream)

	var stack []float64
	strokeColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	fillColor := color.RGBA{R: 0, G: 0, B: 0, A: 255}

	// Current transformation matrix
	ctmA, ctmD := 1.0, 1.0
	ctmE, ctmF := 0.0, 0.0

	// path construction (m/l) in device pixels, stroked by S/s
	var pathPts [][2]float64
	lineWidth := 1.0

	// text state
	fontResources := resolveRenderFonts(parser, page)
	ts := &renderTextState{
		fill: fillColor,
	}
	inText := false

	for i := 0; i < len(tokens); i++ {
		tok := string(tokens[i])
		if v, err := strconv.ParseFloat(tok, 64); err == nil {
			stack = append(stack, v)
			continue
		}

		switch tok {
		case "BT":
			inText = true
			ts.tmX, ts.tmY = 0, 0
			ts.tmA, ts.tmB = 1, 0
			ts.lineA, ts.lineB = 1, 0
			ts.tlmX, ts.tlmY = 0, 0
		case "ET":
			inText = false
		case "Tf":
			if err := renderTfOp(ts, fontResources, tokens, i, stack); err != nil {
			}
		case "Td":
			if inText && len(stack) >= 2 {
				ts.tlmX += stack[len(stack)-2]
				ts.tlmY += stack[len(stack)-1]
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				ts.tmA, ts.tmB = ts.lineA, ts.lineB
				stack = stack[:len(stack)-2]
			}
		case "TD":
			if inText && len(stack) >= 2 {
				ts.leading = -stack[len(stack)-1]
				ts.tlmX += stack[len(stack)-2]
				ts.tlmY += stack[len(stack)-1]
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				ts.tmA, ts.tmB = ts.lineA, ts.lineB
				stack = stack[:len(stack)-2]
			}
		case "Tm":
			if inText && len(stack) >= 6 {
				ts.tmA = stack[len(stack)-6]
				ts.tmB = stack[len(stack)-5]
				ts.lineA, ts.lineB = ts.tmA, ts.tmB
				ts.tmX = stack[len(stack)-2]
				ts.tmY = stack[len(stack)-1]
				ts.tlmX, ts.tlmY = ts.tmX, ts.tmY
				stack = stack[:len(stack)-6]
			}
		case "T*":
			// next text line at the current leading
			if inText {
				ts.tlmY -= ts.leading
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				ts.tmA, ts.tmB = ts.lineA, ts.lineB
			}
		case "'":
			// next line + show
			if inText {
				ts.tlmY -= ts.leading
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				raw := renderTextOpBytes(tokens, i)
				renderDrawText(img, ts, raw, pageH, scale, fontResources)
			}
		case "\"":
			// set word/char spacing, next line + show
			if inText && len(stack) >= 3 {
				stack = stack[:len(stack)-3]
			}
			if inText {
				ts.tlmY -= ts.leading
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				raw := renderTextOpBytes(tokens, i)
				renderDrawText(img, ts, raw, pageH, scale, fontResources)
			}
		case "Tj":
			if inText {
				raw := renderTextOpBytes(tokens, i)
				renderDrawText(img, ts, raw, pageH, scale, fontResources)
			}
		case "TJ":
			if inText {
				if start := findArrayStart(tokens, i); start >= 0 {
					var run []byte
					kern := 0.0
					effSize := ts.effFontSize()
					flushRun := func() {
						if len(run) == 0 {
							return
						}
						penX := ts.tmX + kern
						penY := pageH - ts.tmY
						drawTextRun(img, ts.font, run, effSize*scale, penX*scale, penY*scale, ts.fill, ts.charSpacing*scale, ts.wordSpacing*scale)
						run = run[:0]
					}
					for _, item := range tokens[start+1 : i] {
						if isStringToken(item) {
							// strip the <...> or (...) delimiters
							inner := item[1 : len(item)-1]
							if len(inner) > 0 && inner[0] == '<' {
								continue
							}
							if item[0] == '<' {
								run = append(run, hexBytes(trimPDFWS(inner))...)
							} else {
								run = append(run, []byte(unescapePDFString(string(inner)))...)
							}
						} else if v, err := strconv.ParseFloat(string(item), 64); err == nil {
							// flush before the displacement so the next string
							// starts at the shifted position
							flushRun()
							kern += -v / 1000.0 * effSize
						}
					}
					flushRun()
				}
			}
		case "cm":
			if len(stack) >= 6 {
				ma := stack[len(stack)-6]
				md := stack[len(stack)-3]
				me := stack[len(stack)-2]
				mf := stack[len(stack)-1]
				stack = stack[:len(stack)-6]
				// compose: new = cm x CTM (b, c components unused here)
				ctmE = me*ctmA + mf*0 + ctmE
				ctmF = me*0 + mf*ctmD + ctmF
				ctmA *= ma
				ctmD *= md
			}

		case "re":
			// Rectangle: x y w h re — transformed through the CTM, painted
			// according to the following op (f fills, S strokes)
			if len(stack) >= 4 {
				rx := stack[len(stack)-4]
				ry := stack[len(stack)-3]
				rw := stack[len(stack)-2]
				rh := stack[len(stack)-1]
				stack = stack[:len(stack)-4]
				x1 := ctmA*rx + ctmE
				y1 := ctmD*ry + ctmF
				x2 := ctmA*(rx+rw) + ctmE
				y2 := ctmD*(ry+rh) + ctmF
				dx := min2(x1, x2) * scale
				dy := (pageH - max2(y1, y2)) * scale
				dw := math.Abs(x2-x1) * scale
				dh := math.Abs(y2-y1) * scale
				next := ""
				if i+1 < len(tokens) {
					next = string(tokens[i+1])
				}
				filled := next == "f" || next == "f*" || next == "B" || next == "b"
				stroked := next == "S" || next == "s" || next == "B" || next == "b"
				if filled {
					// paint the interior (cell/line backgrounds, shading)
					drawRectFill(img, dx, dy, dw, dh, fillColor)
				}
				if stroked {
					drawRectOnImage(img, dx, dy, dw, dh, 1.0, strokeColor)
				}
			}

		case "m":
			// moveto — start a subpath
			if len(stack) >= 2 {
				x := stack[len(stack)-2]
				y := stack[len(stack)-1]
				stack = stack[:len(stack)-2]
				pathPts = append(pathPts, [2]float64{ctmA*x + ctmE, ctmD*y + ctmF})
			}

		case "l":
			// lineto — extend the path
			if len(stack) >= 2 {
				x := stack[len(stack)-2]
				y := stack[len(stack)-1]
				stack = stack[:len(stack)-2]
				pathPts = append(pathPts, [2]float64{ctmA*x + ctmE, ctmD*y + ctmF})
			}

		case "RG":
			// Set stroke color RGB
			if len(stack) >= 3 {
				r := uint8(stack[len(stack)-3] * 255)
				g := uint8(stack[len(stack)-2] * 255)
				b := uint8(stack[len(stack)-1] * 255)
				strokeColor = color.RGBA{R: r, G: g, B: b, A: 255}
				stack = stack[:len(stack)-3]
			}

		case "rg":
			// Set fill color RGB
			if len(stack) >= 3 {
				r := uint8(stack[len(stack)-3] * 255)
				g := uint8(stack[len(stack)-2] * 255)
				b := uint8(stack[len(stack)-1] * 255)
				fillColor = color.RGBA{R: r, G: g, B: b, A: 255}
				stack = stack[:len(stack)-3]
			}

		case "G":
			// Set stroke gray
			if len(stack) >= 1 {
				g := uint8(stack[len(stack)-1] * 255)
				strokeColor = color.RGBA{R: g, G: g, B: g, A: 255}
				stack = stack[:len(stack)-1]
			}

		case "g":
			// Set fill gray
			if len(stack) >= 1 {
				g := uint8(stack[len(stack)-1] * 255)
				fillColor = color.RGBA{R: g, G: g, B: g, A: 255}
				stack = stack[:len(stack)-1]
			}

		case "Do":
			// Draw XObject (image)
			if i >= 1 && strings.HasPrefix(string(tokens[i-1]), "/") {
				name := string(tokens[i-1])
				renderXObject(img, parser, page, name, ctmA, ctmD, ctmE, ctmF, scale, pageH)
			}

		case "S", "s":
			// Stroke the constructed path (lines/polylines). Points are in
			// PDF space (y-up): convert to device pixels (y-down).
			if len(pathPts) >= 2 {
				dev := make([][2]float64, len(pathPts))
				for i, p := range pathPts {
					dev[i] = [2]float64{p[0] * scale, (pageH - p[1]) * scale}
				}
				if debugRender {
					fmt.Printf("DBG stroke n=%d pts=%v w=%.1f\n", len(dev), dev, lineWidth*scale)
				}
				strokePolyline(img, dev, lineWidth*scale, strokeColor)
			}
			pathPts = pathPts[:0]
			stack = stack[:0]

		case "f", "F", "f*":
			// Fill path
			pathPts = pathPts[:0]
			stack = stack[:0]

		case "B", "B*", "b", "b*":
			// Fill and stroke
			stack = stack[:0]

		case "n":
			// End path without fill/stroke
			stack = stack[:0]

		case "w":
			// Line width
			if len(stack) >= 1 {
				lineWidth = stack[len(stack)-1]
				if lineWidth <= 0 {
					lineWidth = 1
				}
				stack = stack[:len(stack)-1]
			}

		case "q":
			// Save graphics state
		case "Q":
			// Restore graphics state
			ctmA, ctmD = 1, 1
			ctmE, ctmF = 0, 0

		case "W", "W*":
			// Clipping — ignore for basic rendering

		default:
			if !strings.HasPrefix(tok, "/") {
				stack = stack[:0]
			}
		}
	}
}

// drawRectOnImage draws a rectangle outline on the image.
// strokePolyline draws connected segments with a simple thickness model:
// hairlines get a 1px Bresenham, wider widths get 2-3 px coverage.
func strokePolyline(img *image.RGBA, pts [][2]float64, wPx float64, c color.RGBA) {
	thick := int(wPx + 0.5)
	if thick < 1 {
		thick = 1
	}
	bounds := img.Bounds()
	for i := 0; i+1 < len(pts); i++ {
		x0, y0 := pts[i][0], pts[i][1]
		x1, y1 := pts[i+1][0], pts[i+1][1]
		drawThickLine(img, x0, y0, x1, y1, thick, c, bounds)
	}
}

// drawThickLine rasterizes one segment with integer thickness.
func drawThickLine(img *image.RGBA, x0, y0, x1, y1 float64, thick int, c color.RGBA, bounds image.Rectangle) {
	dx := x1 - x0
	dy := y1 - y0
	steps := int(math.Abs(dx))
	if int(math.Abs(dy)) > steps {
		steps = int(math.Abs(dy))
	}
	if steps < 1 {
		steps = 1
	}
	half := thick / 2
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		px := int(x0 + dx*t)
		py := int(y0 + dy*t)
		for oy := -half; oy < thick-half; oy++ {
			for ox := -half; ox < thick-half; ox++ {
				x, y := px+ox, py+oy
				if x >= 0 && x < bounds.Max.X && y >= 0 && y < bounds.Max.Y {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
}

// drawRectFill paints the interior of a rectangle (device-pixel coords).
func drawRectFill(img *image.RGBA, x, y, w, h float64, c color.RGBA) {
	ix := int(x)
	iy := int(y)
	iw := int(w)
	ih := int(h)
	bounds := img.Bounds()
	for py := iy; py < iy+ih; py++ {
		if py < 0 || py >= bounds.Max.Y {
			continue
		}
		for px := ix; px < ix+iw; px++ {
			if px < 0 || px >= bounds.Max.X {
				continue
			}
			img.SetRGBA(px, py, c)
		}
	}
}

func drawRectOnImage(img *image.RGBA, x, y, w, h, scale float64, c color.RGBA) {
	ix := int(x * scale)
	iy := int(y * scale)
	iw := int(w * scale)
	ih := int(h * scale)

	bounds := img.Bounds()

	// Draw horizontal lines
	for px := ix; px <= ix+iw && px < bounds.Max.X; px++ {
		if px >= 0 {
			if iy >= 0 && iy < bounds.Max.Y {
				img.SetRGBA(px, iy, c)
			}
			if iy+ih >= 0 && iy+ih < bounds.Max.Y {
				img.SetRGBA(px, iy+ih, c)
			}
		}
	}
	// Draw vertical lines
	for py := iy; py <= iy+ih && py < bounds.Max.Y; py++ {
		if py >= 0 {
			if ix >= 0 && ix < bounds.Max.X {
				img.SetRGBA(ix, py, c)
			}
			if ix+iw >= 0 && ix+iw < bounds.Max.X {
				img.SetRGBA(ix+iw, py, c)
			}
		}
	}
}

// renderXObject renders an image XObject onto the target image.
func renderXObject(img *image.RGBA, parser *rawPDFParser, page rawPDFPage, name string,
	ctmA, ctmD, ctmE, ctmF, scale, pageH float64) {

	objNum, ok := page.resources.xobjs[name]
	if !ok {
		return
	}
	obj, ok := parser.objects[objNum]
	if !ok {
		return
	}
	// Form XObjects may carry more text/graphics — render their stream
	// recursively with the form's own font resources merged in.
	if strings.Contains(obj.dict, "/Subtype /Form") || strings.Contains(obj.dict, "/Subtype/Form") {
		if obj.stream != nil {
			subPage := page
			subPage.resources.fonts = pageFontsWithForm(parser, page.resources.fonts, obj.dict)
			renderContentStream(img, obj.stream, parser, subPage, scale, pageH)
		}
		return
	}
	if !strings.Contains(obj.dict, "/Subtype /Image") &&
		!strings.Contains(obj.dict, "/Subtype/Image") {
		return
	}

	if obj.stream == nil {
		return
	}

	// Raw 8-bit sample bitmaps (DeviceRGB/DeviceGray — how GoPdf embeds PNG
	// inputs) cannot go through image.Decode; build the image directly and
	// fall back to the generic decoder for JPEG/PNG payload streams.
	srcImg := rawSampleImage(&obj, parser)
	if srcImg == nil {
		var err error
		srcImg, _, err = image.Decode(strings.NewReader(string(obj.stream)))
		if err != nil || srcImg == nil {
			return
		}
	}

	// Calculate destination rectangle
	dstX := int(ctmE * scale)
	dstY := int((pageH - ctmF - ctmD) * scale)
	dstW := int(ctmA * scale)
	dstH := int(ctmD * scale)

	if dstW <= 0 || dstH <= 0 {
		return
	}

	// Simple nearest-neighbor scaling
	srcBounds := srcImg.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	for py := 0; py < dstH; py++ {
		for px := 0; px < dstW; px++ {
			sx := srcBounds.Min.X + px*srcW/dstW
			sy := srcBounds.Min.Y + py*srcH/dstH
			if sx >= srcBounds.Max.X {
				sx = srcBounds.Max.X - 1
			}
			if sy >= srcBounds.Max.Y {
				sy = srcBounds.Max.Y - 1
			}
			dx := dstX + px
			dy := dstY + py
			if dx >= 0 && dx < img.Bounds().Max.X && dy >= 0 && dy < img.Bounds().Max.Y {
				// alpha-composite over the existing pixel (SMask
				// transparency must blend with the page, not replace it)
				sc := color.RGBAModel.Convert(srcImg.At(sx, sy)).(color.RGBA)
				if sc.A == 255 {
					img.SetRGBA(dx, dy, sc)
				} else if sc.A > 0 {
					d := img.RGBAAt(dx, dy)
					a := uint32(sc.A)
					inv := 255 - a
					out := color.RGBA{
						R: uint8((uint32(sc.R)*255 + uint32(d.R)*inv) / 255),
						G: uint8((uint32(sc.G)*255 + uint32(d.G)*inv) / 255),
						B: uint8((uint32(sc.B)*255 + uint32(d.B)*inv) / 255),
						A: 255,
					}
					img.SetRGBA(dx, dy, out)
				}
			}
		}
	}
}

// findArrayStart locates the opening bracket of the TJ array ending at opIdx.
func findArrayStart(tokens [][]byte, opIdx int) int {
	for j := opIdx - 1; j >= 0; j-- {
		if bytes.Equal(tokens[j], []byte("[")) {
			return j
		}
	}
	return -1
}

// rawSampleImage builds an image from an image XObject whose stream holds
// uncompressed 8-bit samples (DeviceRGB or DeviceGray). Returns nil when the
// dict/stream don't match that layout. An /SMask supplies the alpha channel.
func rawSampleImage(obj *rawPDFObject, parser *rawPDFParser) image.Image {
	w := int(extractNumValue(obj.dict, "/Width"))
	h := int(extractNumValue(obj.dict, "/Height"))
	bpc := int(extractNumValue(obj.dict, "/BitsPerComponent"))
	if w <= 0 || h <= 0 || bpc != 8 || obj.stream == nil || len(obj.stream) == 0 {
		return nil
	}
	channels := 0
	switch {
	case strings.Contains(obj.dict, "/DeviceRGB"):
		channels = 3
	case strings.Contains(obj.dict, "/DeviceGray"):
		channels = 1
	default:
		return nil
	}
	if len(obj.stream) < w*h*channels {
		return nil
	}
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	if channels == 3 {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				off := (y*w + x) * 3
				m.SetRGBA(x, y, color.RGBA{
					R: obj.stream[off], G: obj.stream[off+1], B: obj.stream[off+2], A: 255,
				})
			}
		}
	} else {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := obj.stream[y*w+x]
				m.SetRGBA(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
			}
		}
	}
	// soft mask: 8-bit gray samples -> alpha channel
	if smRef := extractRef(obj.dict, "/SMask"); smRef > 0 && parser != nil {
		if smObj, ok := parser.objects[smRef]; ok && smObj.stream != nil &&
			strings.Contains(smObj.dict, "/DeviceGray") {
			sw := int(extractNumValue(smObj.dict, "/Width"))
			sh := int(extractNumValue(smObj.dict, "/Height"))
			if sw > 0 && sh > 0 && len(smObj.stream) >= sw*sh {
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						sy := y * sh / h
						sx := x * sw / w
						c := m.RGBAAt(x, y)
						// image.RGBA is premultiplied — scale RGB by alpha
						a := uint32(smObj.stream[sy*sw+sx])
						c.A = uint8(a)
						c.R = uint8(uint16(c.R) * uint16(a) / 255)
						c.G = uint8(uint16(c.G) * uint16(a) / 255)
						c.B = uint8(uint16(c.B) * uint16(a) / 255)
						m.SetRGBA(x, y, c)
					}
				}
			}
		}
	}
	return m
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// renderTfOp applies a Tf operator: /Name size Tf — the name may sit one or
// several tokens back when intermediate numbers appear between operands.
func renderTfOp(ts *renderTextState, fontResources map[string]*renderFont, tokens [][]byte, i int, stack []float64) error {
	if len(fontResources) == 0 {
		return nil
	}
	var name []byte
	if i >= 1 && strings.HasPrefix(string(tokens[i-1]), "/") {
		name = tokens[i-1]
	} else if i >= 2 && strings.HasPrefix(string(tokens[i-2]), "/") {
		name = tokens[i-2]
	} else {
		return nil
	}
	var size float64
	if len(stack) >= 1 {
		size = stack[len(stack)-1]
	}
	renderTf(ts, fontResources, name, size)
	return nil
}

// RenderPageToFile renders a PDF page to a PNG file on disk.
func RenderPageToFile(pdfPath string, pageIndex int, outPath string, opt RenderOption) error {
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return err
	}
	img, err := RenderPageToImage(data, pageIndex, opt)
	if err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
