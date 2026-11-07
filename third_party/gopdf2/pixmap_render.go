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
func renderParsedPage(parser *rawPDFParser, pageIndex int, opt RenderOption) (image.Image, error) {
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(parser.pages)-1)
	}
	page := parser.pages[pageIndex]
	mediaBox := page.mediaBox
	pageW := mediaBox[2] - mediaBox[0]
	pageH := mediaBox[3] - mediaBox[1]

	scale := opt.DPI / 72.0
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
	return img, nil
}

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
				stack = stack[:len(stack)-2]
			}
		case "TD":
			if inText && len(stack) >= 2 {
				ts.leading = -stack[len(stack)-1]
				ts.tlmX += stack[len(stack)-2]
				ts.tlmY += stack[len(stack)-1]
				ts.tmX, ts.tmY = ts.tlmX, ts.tlmY
				stack = stack[:len(stack)-2]
			}
		case "Tm":
			if inText && len(stack) >= 6 {
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
					flushRun := func() {
						if len(run) == 0 {
							return
						}
						penX := ts.tmX + kern
						penY := pageH - ts.tmY
						drawTextRun(img, ts.font, run, ts.fontSize, penX*scale, penY*scale, ts.fill, ts.charSpacing*scale, ts.wordSpacing*scale)
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
							kern += -v / 1000.0 * ts.fontSize
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
				col := strokeColor
				next := ""
				if i+1 < len(tokens) {
					next = string(tokens[i+1])
				}
				if next == "f" || next == "f*" || next == "B" {
					col = fillColor
				}
				drawRectOnImage(img, dx, dy, dw, dh, 1.0, col)
			}

		case "m":
			// moveto — just consume coordinates
			if len(stack) >= 2 {
				stack = stack[:len(stack)-2]
			}

		case "l":
			// lineto — draw line from last point
			if len(stack) >= 2 {
				stack = stack[:len(stack)-2]
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
			// Stroke path — already handled inline
			stack = stack[:0]

		case "f", "F", "f*":
			// Fill path
			stack = stack[:0]

		case "B", "B*", "b", "b*":
			// Fill and stroke
			stack = stack[:0]

		case "n":
			// End path without fill/stroke
			stack = stack[:0]

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

	filter := extractFilterValue(obj.dict)
	if obj.stream == nil {
		return
	}

	var srcImg image.Image
	var err error

	switch filter {
	case "DCTDecode":
		srcImg, _, err = image.Decode(strings.NewReader(string(obj.stream)))
	default:
		// Try generic decode
		srcImg, _, err = image.Decode(strings.NewReader(string(obj.stream)))
	}
	if err != nil || srcImg == nil {
		return
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
				img.Set(dx, dy, srcImg.At(sx, sy))
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
