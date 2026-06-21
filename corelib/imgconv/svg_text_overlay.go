package imgconv

import (
	"encoding/xml"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// svgTextElement represents a parsed <text> or <tspan> element from SVG.
type svgTextElement struct {
	X        float64
	Y        float64
	FontSize float64 // in SVG user units
	Anchor   string  // start, middle, end
	Bold     bool
	Fill     color.RGBA // text color (default black)
	Content  string
}

// --- Parsing helpers ---

// parseCoordinate parses a simple float coordinate value (no unit stripping).
func parseCoordinate(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// parseFontSizeValue extracts a numeric font-size from a string that may
// contain units like "px", "pt", or "em".
func parseFontSizeValue(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	s = strings.TrimSuffix(s, "pt")
	s = strings.TrimSuffix(s, "em")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

var styleFontSizeRe = regexp.MustCompile(`font-size\s*:\s*([0-9.]+(?:px|pt|em)?)`)
var styleFontWeightRe = regexp.MustCompile(`font-weight\s*:\s*(\w+)`)
var styleFillRe = regexp.MustCompile(`(?:^|;)\s*fill\s*:\s*([^;]+)`)

func parseFontSizeFromStyle(style string) float64 {
	m := styleFontSizeRe.FindStringSubmatch(style)
	if len(m) < 2 {
		return 0
	}
	return parseFontSizeValue(m[1])
}

func isBoldWeight(w string) bool {
	w = strings.ToLower(strings.TrimSpace(w))
	return w == "bold" || w == "700" || w == "800" || w == "900"
}

// parseHexColor parses #RGB, #RRGGBB, or #RRGGBBAA hex color strings.
func parseHexColor(s string) (color.RGBA, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "#") {
		return color.RGBA{}, false
	}
	s = s[1:]
	switch len(s) {
	case 3:
		r, _ := strconv.ParseUint(string(s[0])+string(s[0]), 16, 8)
		g, _ := strconv.ParseUint(string(s[1])+string(s[1]), 16, 8)
		b, _ := strconv.ParseUint(string(s[2])+string(s[2]), 16, 8)
		return color.RGBA{uint8(r), uint8(g), uint8(b), 255}, true
	case 6:
		r, _ := strconv.ParseUint(s[0:2], 16, 8)
		g, _ := strconv.ParseUint(s[2:4], 16, 8)
		b, _ := strconv.ParseUint(s[4:6], 16, 8)
		return color.RGBA{uint8(r), uint8(g), uint8(b), 255}, true
	case 8:
		r, _ := strconv.ParseUint(s[0:2], 16, 8)
		g, _ := strconv.ParseUint(s[2:4], 16, 8)
		b, _ := strconv.ParseUint(s[4:6], 16, 8)
		a, _ := strconv.ParseUint(s[6:8], 16, 8)
		return color.RGBA{uint8(r), uint8(g), uint8(b), uint8(a)}, true
	}
	return color.RGBA{}, false
}

// parseFillColor tries to parse a fill attribute value.
// Returns black as default. For "none", returns alpha=0 (skip rendering).
func parseFillColor(s string) color.RGBA {
	s = strings.TrimSpace(s)
	if s == "" || s == "currentColor" {
		return color.RGBA{0, 0, 0, 255} // default black
	}
	if s == "none" {
		return color.RGBA{0, 0, 0, 0} // transparent = skip
	}
	if c, ok := parseHexColor(s); ok {
		return c
	}
	// Common named colors used in SVG
	switch strings.ToLower(s) {
	case "black":
		return color.RGBA{0, 0, 0, 255}
	case "white":
		return color.RGBA{255, 255, 255, 255}
	case "red":
		return color.RGBA{255, 0, 0, 255}
	case "blue":
		return color.RGBA{0, 0, 255, 255}
	case "green":
		return color.RGBA{0, 128, 0, 255}
	}
	return color.RGBA{0, 0, 0, 255}
}

// --- SVG Text Extraction ---

// extractSVGTextElements parses SVG XML and extracts all text fragments.
func extractSVGTextElements(r io.Reader) ([]svgTextElement, error) {
	decoder := xml.NewDecoder(r)
	var texts []svgTextElement

	type textState struct {
		baseX, baseY float64
		fontSize     float64
		anchor       string
		bold         bool
		fill         color.RGBA
	}
	var stack []textState
	var curSpan *svgTextElement
	var textBuf strings.Builder
	inText := false
	defaultFill := color.RGBA{0, 0, 0, 255}

	flushSpan := func() {
		if curSpan != nil {
			content := strings.TrimSpace(textBuf.String())
			if content != "" {
				curSpan.Content = content
				texts = append(texts, *curSpan)
			}
			curSpan = nil
			textBuf.Reset()
		}
	}

	parseTextAttrs := func(attrs []xml.Attr, base *textState) {
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "x":
				base.baseX = parseCoordinate(attr.Value)
			case "y":
				base.baseY = parseCoordinate(attr.Value)
			case "font-size":
				if v := parseFontSizeValue(attr.Value); v > 0 {
					base.fontSize = v
				}
			case "text-anchor":
				base.anchor = attr.Value
			case "font-weight":
				base.bold = isBoldWeight(attr.Value)
			case "fill":
				base.fill = parseFillColor(attr.Value)
			case "style":
				if v := parseFontSizeFromStyle(attr.Value); v > 0 {
					base.fontSize = v
				}
				if m := styleFontWeightRe.FindStringSubmatch(attr.Value); len(m) >= 2 {
					base.bold = isBoldWeight(m[1])
				}
				if m := styleFillRe.FindStringSubmatch(attr.Value); len(m) >= 2 {
					base.fill = parseFillColor(m[1])
				}
			}
		}
	}

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "text" {
				inText = true
				st := textState{fontSize: 14, anchor: "start", fill: defaultFill}
				parseTextAttrs(t.Attr, &st)
				stack = append(stack, st)
				curSpan = &svgTextElement{
					X: st.baseX, Y: st.baseY,
					FontSize: st.fontSize, Anchor: st.anchor,
					Bold: st.bold, Fill: st.fill,
				}
				textBuf.Reset()

			} else if t.Name.Local == "tspan" && inText {
				flushSpan()
				parent := stack[len(stack)-1]
				st := textState{
					baseX: parent.baseX, baseY: parent.baseY,
					fontSize: parent.fontSize, anchor: parent.anchor,
					bold: parent.bold, fill: parent.fill,
				}
				parseTextAttrs(t.Attr, &st)
				curSpan = &svgTextElement{
					X: st.baseX, Y: st.baseY,
					FontSize: st.fontSize, Anchor: st.anchor,
					Bold: st.bold, Fill: st.fill,
				}
				textBuf.Reset()
			}

		case xml.CharData:
			if inText && curSpan != nil {
				textBuf.Write(t)
			}

		case xml.EndElement:
			if t.Name.Local == "tspan" && inText {
				flushSpan()
			} else if t.Name.Local == "text" {
				flushSpan()
				inText = false
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	return texts, nil
}

// --- Font Loading ---

var (
	fontOnce       sync.Once
	cachedRegular  *opentype.Font
	cachedBold     *opentype.Font
)

func systemFontPaths() []string {
	switch runtime.GOOS {
	case "windows":
		winDir := os.Getenv("WINDIR")
		if winDir == "" {
			winDir = `C:\Windows`
		}
		fontsDir := filepath.Join(winDir, "Fonts")
		return []string{
			filepath.Join(fontsDir, "arial.ttf"),
			filepath.Join(fontsDir, "segoeui.ttf"),
			filepath.Join(fontsDir, "consola.ttf"),
		}
	case "darwin":
		return []string{
			"/System/Library/Fonts/Supplemental/Arial.ttf",
			"/System/Library/Fonts/Helvetica.ttc",
			"/Library/Fonts/Arial.ttf",
		}
	default:
		return []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
			"/usr/share/fonts/TTF/DejaVuSans.ttf",
			"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf",
			"/usr/share/fonts/liberation-sans/LiberationSans-Regular.ttf",
			"/usr/share/fonts/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
			"/usr/share/fonts/google-noto/NotoSans-Regular.ttf",
		}
	}
}

func loadFonts() {
	fontOnce.Do(func() {
		// Try system font for regular weight
		for _, p := range systemFontPaths() {
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			f, err := opentype.Parse(data)
			if err != nil {
				continue
			}
			cachedRegular = f
			break
		}
		// Fallback to embedded Go fonts (guaranteed cross-platform)
		if cachedRegular == nil {
			if f, err := opentype.Parse(goregular.TTF); err == nil {
				cachedRegular = f
			}
		}
		// Bold: always use embedded Go Bold (simple, avoids hunting for bold system font)
		if f, err := opentype.Parse(gobold.TTF); err == nil {
			cachedBold = f
		}
	})
}

// --- Text Rendering ---

// faceKey is used to cache font.Face instances by size+weight to avoid
// creating duplicate faces (which leak resources if not closed).
type faceKey struct {
	size int // pixelSize rounded to int (sufficient precision for raster)
	bold bool
}

// overlayTextOnImage draws extracted SVG text elements onto the rendered image.
// scaleX/scaleY map SVG user coordinates to pixel coordinates.
func overlayTextOnImage(img *image.RGBA, texts []svgTextElement, scaleX, scaleY float64) {
	loadFonts()

	// Pre-allocate common black color source (most SVG text is black)
	defaultBlack := color.RGBA{0, 0, 0, 255}
	blackSrc := image.NewUniform(defaultBlack)

	// Cache faces by (size, bold) to avoid repeated allocation and resource leak.
	// All faces are closed at function exit.
	faceCache := make(map[faceKey]font.Face)
	defer func() {
		for _, f := range faceCache {
			if closer, ok := f.(interface{ Close() error }); ok {
				closer.Close()
			}
		}
	}()

	getFace := func(pixelSize float64, bold bool) font.Face {
		key := faceKey{size: int(math.Round(pixelSize)), bold: bold}
		if f, ok := faceCache[key]; ok {
			return f
		}
		fnt := cachedRegular
		if bold && cachedBold != nil {
			fnt = cachedBold
		}
		if fnt == nil {
			f := font.Face(basicfont.Face7x13)
			faceCache[key] = f
			return f
		}
		face, err := opentype.NewFace(fnt, &opentype.FaceOptions{
			Size:    pixelSize,
			DPI:     72,
			Hinting: font.HintingFull,
		})
		if err != nil {
			f := font.Face(basicfont.Face7x13)
			faceCache[key] = f
			return f
		}
		faceCache[key] = face
		return face
	}

	for _, te := range texts {
		if te.Content == "" {
			continue
		}
		// Skip text with fill="none" (transparent, not meant to be rendered)
		if te.Fill.A == 0 {
			continue
		}

		// Target pixel font size
		pixelSize := te.FontSize * scaleY
		if pixelSize < 8 {
			pixelSize = 8
		}
		if pixelSize > 200 {
			pixelSize = 200
		}

		// Scale SVG coordinates to pixel coordinates
		px := int(math.Round(te.X * scaleX))
		py := int(math.Round(te.Y * scaleY))

		face := getFace(pixelSize, te.Bold)

		// Reuse color source for the common case (black)
		src := blackSrc
		if te.Fill != defaultBlack {
			src = image.NewUniform(te.Fill)
		}

		drawer := &font.Drawer{
			Dst:  img,
			Src:  src,
			Face: face,
		}

		// Measure text width for anchor adjustment
		advance := drawer.MeasureString(te.Content)
		textWidthPx := advance.Ceil()

		switch te.Anchor {
		case "middle":
			px -= textWidthPx / 2
		case "end":
			px -= textWidthPx
		}

		drawer.Dot = fixed.P(px, py)
		drawer.DrawString(te.Content)
	}
}
