package gopdf

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Pre-compiled regexes for text extraction — avoids recompilation per call.
var (
	reExtractHexPairs = regexp.MustCompile(`<([0-9a-fA-F]+)>`)
	reBfChar          = regexp.MustCompile(`(?s)beginbfchar\s*(.*?)\s*endbfchar`)
	reBfRange         = regexp.MustCompile(`(?s)beginbfrange\s*(.*?)\s*endbfrange`)
)

// ============================================================
// Text extraction from existing PDF files
// ============================================================

// ExtractTextFromPage extracts text from a specific page (0-based) of
// the given PDF data. Returns a list of ExtractedText items with
// position, font, and text content.
//
// This is a pure-Go PDF content stream parser that handles the most
// common text operators: BT/ET, Tf, Td, TD, Tm, T*, Tj, TJ, ', ".
//
// Example:
//
//	data, _ := os.ReadFile("input.pdf")
//	texts, _ := gopdf.ExtractTextFromPage(data, 0)
//	for _, t := range texts {
//	    fmt.Printf("(%0.f,%0.f) %s\n", t.X, t.Y, t.Text)
//	}
func ExtractTextFromPage(pdfData []byte, pageIndex int) ([]ExtractedText, error) {
	// Prefer the built-in raw parser: it tracks text-position advances and
	// supports object streams, predefined CMaps and legacy CJK encodings.
	texts, rawErr := extractTextRawPage(pdfData, pageIndex)
	if rawErr == nil && len(texts) > 0 {
		return texts, nil
	}
	// Fallback to the ledongthuc/pdf backend.
	result, v2Err := extractTextV2FromPage(pdfData, pageIndex)
	if v2Err == nil {
		return result, nil
	}
	if rawErr != nil {
		return nil, rawErr
	}
	return result, nil
}

// extractTextRawPage runs the raw-parser extraction for one page.
func extractTextRawPage(pdfData []byte, pageIndex int) ([]ExtractedText, error) {
	parser, err := newRawPDFParser(pdfData)
	if err != nil {
		return nil, err
	}
	if pageIndex < 0 || pageIndex >= len(parser.pages) {
		return nil, fmt.Errorf("page index %d out of range (0..%d)", pageIndex, len(parser.pages)-1)
	}
	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return nil, nil
	}
	page := parser.pages[pageIndex]
	fonts := buildFontMap(parser, page)
	return parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		visited:  map[int]bool{},
	}, stream), nil
}

// ExtractTextFromAllPages extracts text from all pages.
func ExtractTextFromAllPages(pdfData []byte) (map[int][]ExtractedText, error) {
	// Raw parser first (position tracking, ObjStm, CJK encodings).
	parser, err := newRawPDFParser(pdfData)
	if err == nil && len(parser.pages) > 0 {
		result := make(map[int][]ExtractedText, len(parser.pages))
		any := false
		for i := range parser.pages {
			stream := parser.getPageContentStream(i)
			if len(stream) == 0 {
				continue
			}
			page := parser.pages[i]
			fonts := buildFontMap(parser, page)
			texts := parseTextOperatorsCtx(&extractionContext{
				parser:   parser,
				fonts:    fonts,
				xobjs:    page.resources.xobjs,
				mediaBox: page.mediaBox,
				visited:  map[int]bool{},
			}, stream)
			if len(texts) > 0 {
				result[i] = texts
				any = true
			}
		}
		if any {
			return result, nil
		}
	}
	// Fallback to ledongthuc/pdf.
	if result, err := extractTextV2FromAllPages(pdfData); err == nil && len(result) > 0 {
		return result, nil
	}
	if err == nil {
		return map[int][]ExtractedText{}, nil
	}
	return nil, err
}

// ExtractPageText extracts all text from a page as a single string.
// Convenience wrapper around ExtractTextFromPage.
func ExtractPageText(pdfData []byte, pageIndex int) (string, error) {
	// Try robust ledongthuc/pdf backend first.
	if result, err := extractPageTextV2(pdfData, pageIndex); err == nil && result != "" {
		return result, nil
	}
	// Fallback to raw parser.
	texts, err := ExtractTextFromPage(pdfData, pageIndex)
	if err != nil {
		return "", err
	}
	return joinExtractedPlain(texts), nil
}

// joinExtractedPlain is the line break ExtractPageText uses: a new line when
// the baseline moves, a space when the next run stays on that baseline.
func joinExtractedPlain(texts []ExtractedText) string {
	var sb strings.Builder
	lastY := math.MaxFloat64
	for _, t := range texts {
		if math.Abs(t.Y-lastY) > 2 && lastY != math.MaxFloat64 {
			sb.WriteByte('\n')
		} else if sb.Len() > 0 && lastY != math.MaxFloat64 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.Text)
		lastY = t.Y
	}
	return sb.String()
}

// rawPagePlainText reads one page from an already-open raw parser.
// pageIndex is 0-based. A page with no content stream is empty.
func rawPagePlainText(parser *rawPDFParser, pageIndex int) string {
	if parser == nil || pageIndex < 0 || pageIndex >= len(parser.pages) {
		return ""
	}
	stream := parser.getPageContentStream(pageIndex)
	if len(stream) == 0 {
		return ""
	}
	page := parser.pages[pageIndex]
	fonts := buildFontMap(parser, page)
	return joinExtractedPlain(parseTextOperatorsCtx(&extractionContext{
		parser:   parser,
		fonts:    fonts,
		xobjs:    page.resources.xobjs,
		mediaBox: page.mediaBox,
		visited:  map[int]bool{},
	}, stream))
}

// fontInfo holds decoded font information for text extraction.
type fontInfo struct {
	name          string // resource name like /F1
	baseFont      string
	encoding      string // /Encoding value (name form)
	baseEncoding  string // BaseEncoding or the name form for dict encodings
	differences   map[byte]string
	toUni         codeToUnicode // CMap: character code -> unicode text
	predefCMap    string        // Type0 predefined CMap name (e.g. UniGB-UCS2-H)
	cmapFamily    int           // classification of predefCMap
	isType0       bool
	codeSize      int // 1 for simple fonts, 2 for Type0/CID fonts
	isSymbolicZap bool
	// widths maps character codes (simple fonts) or CIDs (Type0) to glyph
	// widths in 1/1000 em, used to advance the text position after show ops.
	widths map[int]float64
	// defaultWidth is the fallback width (1/1000 em) for codes not in widths.
	defaultWidth float64
}

func buildFontMap(parser *rawPDFParser, page rawPDFPage) map[string]*fontInfo {
	fonts := make(map[string]*fontInfo, len(page.resources.fonts))
	for name, objNum := range page.resources.fonts {
		fi := &fontInfo{
			name:        name,
			cmapFamily:  cmapFamilyUnknown,
			differences: make(map[byte]string),
		}
		obj, ok := parser.objects[objNum]
		if ok {
			fi.baseFont = extractName(obj.dict, "/BaseFont")
			fi.encoding = extractName(obj.dict, "/Encoding")
			fi.isType0 = strings.Contains(obj.dict, "/Type0")
			// Type0 fonts always use 2-byte codes.
			if fi.isType0 {
				fi.codeSize = 2
			} else {
				fi.codeSize = 1
			}

			// Symbol and ZapfDingbats use special custom encodings.
			base := fi.baseFont
			fi.isSymbolicZap = strings.Contains(base, "Symbol") || strings.Contains(base, "Dingbat")

			// Glyph widths: /Widths [..] with /FirstChar /LastChar (simple
			// fonts), /W [..] (CID fonts), /MissingWidth fallback.
			fi.widths, fi.defaultWidth = parseFontWidths(parser, obj.dict)

			// /Encoding may be a name (/WinAnsiEncoding) or a dict
			// (<< /BaseEncoding /WinAnsiEncoding /Differences [...] >>).
			if fi.encoding != "" {
				fi.baseEncoding = fi.encoding
				if fi.isType0 {
					fi.predefCMap = fi.encoding
					fi.cmapFamily = predefinedCMapFamily(fi.encoding)
				}
			} else if encRef := extractRef(obj.dict, "/Encoding"); encRef > 0 {
				if encObj, ok2 := parser.objects[encRef]; ok2 {
					fi.baseEncoding = extractName(encObj.dict, "/BaseEncoding")
					fi.differences = parseDifferences(encObj.dict)
				}
			} else if strings.Contains(obj.dict, "/Differences") {
				fi.differences = parseDifferences(obj.dict)
			}
			if fi.baseEncoding == "" && strings.Contains(obj.dict, "/BaseEncoding") {
				fi.baseEncoding = extractName(obj.dict, "/BaseEncoding")
			}

			// Try to parse /ToUnicode CMap
			toUniRef := extractRef(obj.dict, "/ToUnicode")
			if toUniRef > 0 {
				if cmapObj, ok2 := parser.objects[toUniRef]; ok2 && cmapObj.stream != nil {
					fi.toUni = parseCMap(cmapObj.stream)
				}
			}

			// Type0 descendant fonts carry /Encoding with the predefined CMap
			// and /CIDSystemInfo with the registry-ordering (e.g. Adobe-GB1).
			if fi.isType0 {
				if fi.predefCMap == "" {
					fi.predefCMap = fi.encoding
					fi.cmapFamily = predefinedCMapFamily(fi.predefCMap)
				}
				if fi.cmapFamily == cmapFamilyUnknown {
					// Infer from CIDSystemInfo ordering.
					ordering := extractName(obj.dict, "/Ordering")
					fi.cmapFamily = cmapFamilyFromOrdering(ordering)
				}
			}
		}
		fonts[name] = fi
	}
	return fonts
}

// cmapFamilyFromOrdering maps a CIDSystemInfo Ordering to a legacy byte encoding.
func cmapFamilyFromOrdering(ordering string) int {
	switch ordering {
	case "GB1", "GB-EUC-H", "GB-EUC-V":
		return cmapFamilyGBK
	case "CNS1":
		return cmapFamilyBig5
	case "Korea1":
		return cmapFamilyEUCKR
	case "Japan1", "Japan2":
		return cmapFamilySJIS
	default:
		return cmapFamilyUnknown
	}
}

// extractName extracts a /Name value from a dict string.
// Uses string search instead of regex for performance.
func extractName(dict, key string) string {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return ""
	}
	rest := dict[idx+len(key):]
	// Skip whitespace
	i := 0
	for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\r' || rest[i] == '\n') {
		i++
	}
	if i >= len(rest) || rest[i] != '/' {
		return ""
	}
	i++ // skip '/'
	start := i
	for i < len(rest) && rest[i] != ' ' && rest[i] != '\t' && rest[i] != '\r' &&
		rest[i] != '\n' && rest[i] != '/' && rest[i] != '>' && rest[i] != '[' {
		i++
	}
	if i > start {
		return decodePDFNameEscapes(rest[start:i])
	}
	return ""
}

// decodePDFNameEscapes decodes #XX hexadecimal escapes in PDF names.
func decodePDFNameEscapes(s string) string {
	if !strings.Contains(s, "#") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '#' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			sb.WriteByte(hexVal(s[i+1])<<4 | hexVal(s[i+2]))
			i += 2
		} else {
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

// parseCMap parses a ToUnicode CMap stream to build a code->text mapping.
// Handles beginbfchar (including multi-rune destinations for ligatures and
// UTF-16 surrogate pairs) and beginbfrange (range and explicit-list forms).
func parseCMap(data []byte) codeToUnicode {
	m := make(codeToUnicode, 64)
	s := string(data)

	// Parse beginbfchar ... endbfchar sections
	for _, match := range reBfChar.FindAllStringSubmatch(s, -1) {
		lines := strings.Split(strings.TrimSpace(match[1]), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			parts := extractHexPairs(line)
			if len(parts) >= 2 {
				code := parseHex16(parts[0])
				dst := decodeUTF16HexDestination(parts[1:])
				if dst != "" {
					m[code] = dst
				}
			}
		}
	}

	// Parse beginbfrange ... endbfrange sections
	for _, match := range reBfRange.FindAllStringSubmatch(s, -1) {
		lines := strings.Split(strings.TrimSpace(match[1]), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			parts := extractHexPairs(line)
			if len(parts) >= 3 {
				start := int(parseHex16(parts[0]))
				end := int(parseHex16(parts[1]))
				uniStart := int(parseHex16(parts[2]))
				// Guard against malformed ranges. A uint16 loop variable would
				// overflow on end==0xFFFF and loop forever; use int and validate.
				if end < start || end-start > 0xFFFF {
					continue
				}
				if len(parts) == 3 {
					for code := start; code <= end; code++ {
						m[uint16(code)] = string(rune(uniStart + (code - start)))
					}
				} else {
					// <start> <end> [<dst0> <dst1> ...] — explicit destination list
					for code := start; code <= end && code-start < len(parts)-2; code++ {
						dst := decodeUTF16HexDestination([]string{parts[2+code-start]})
						if dst != "" {
							m[uint16(code)] = dst
						}
					}
				}
			}
		}
	}

	return m
}

// decodeUTF16HexDestination decodes one or more 4-hex-digit UTF-16BE units
// (the destination of a bfchar entry) into a string, joining surrogates.
func decodeUTF16HexDestination(units []string) string {
	var data []byte
	for _, u := range units {
		switch len(u) {
		case 4:
			data = append(data, byte(parseHex16(u)>>8), byte(parseHex16(u)))
		case 2:
			// single-byte code destination
			data = append(data, byte(parseHex16(u)))
		default:
			// uneven — best effort
			for i := 0; i+1 < len(u); i += 2 {
				data = append(data, byte(parseHex16(u[i:i+2])))
			}
		}
	}
	if len(data) == 0 {
		return ""
	}
	if len(data)%2 == 0 {
		runes := utf16BeRunes(data)
		out := make([]rune, 0, len(runes))
		for _, r := range runes {
			if r != 0xFFFD {
				out = append(out, r)
			}
		}
		if len(out) > 0 {
			return string(out)
		}
	}
	return string(data)
}

func extractHexPairs(line string) []string {
	matches := reExtractHexPairs.FindAllStringSubmatch(line, -1)
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		result = append(result, m[1])
	}
	return result
}

func parseHex16(s string) uint16 {
	v, _ := strconv.ParseUint(s, 16, 32)
	return uint16(v)
}

// parseFontWidths parses glyph widths from a font dict.
// Simple fonts: /FirstChar, /LastChar, /Widths [w0 w1 ...].
// CID fonts: descendant font's /W array and /DW default width.
// Returns the width map (1/1000 em) and the default width.
func parseFontWidths(parser *rawPDFParser, dict string) (map[int]float64, float64) {
	widths := make(map[int]float64, 96)
	defaultWidth := 500.0

	// CID font: /DescendantFonts [ N 0 R ] — read /W and /DW from descendant.
	if descRef := extractRef(dict, "/DescendantFonts"); descRef > 0 {
		if descObj, ok := parser.objects[descRef]; ok {
			if dw := extractNumValue(descObj.dict, "/DW"); dw > 0 {
				defaultWidth = dw
			}
			parseWArray(descObj.dict, widths)
		}
	}

	// Simple font widths.
	first := int(extractNumValue(dict, "/FirstChar"))
	last := int(extractNumValue(dict, "/LastChar"))
	if last >= first && last-first < 65536 {
		if wArr := extractNumberArray(dict, "/Widths"); len(wArr) > 0 {
			for i, w := range wArr {
				code := first + i
				if code >= 0 && code <= 0xFFFF {
					widths[code] = w
				}
			}
		}
	}
	if mw := extractNumValue(dict, "/MissingWidth"); mw != 0 {
		defaultWidth = mw
	}
	return widths, defaultWidth
}

// parseWArray parses a CID font /W array: [c [w1 w2 ...] | c1 c2 w | c[w]].
func parseWArray(dict string, out map[int]float64) {
	idx := strings.Index(dict, "/W")
	if idx < 0 {
		return
	}
	rest := dict[idx+2:]
	start := strings.Index(rest, "[")
	if start < 0 {
		return
	}
	end := strings.LastIndex(rest, "]")
	if end <= start {
		return
	}
	arr := rest[start+1 : end]
	// Compact form "15[500] 23[780]" (no space between CID and width).
	for _, m := range reCompactW.FindAllStringSubmatch(arr, -1) {
		c, _ := strconv.Atoi(m[1])
		w, _ := strconv.ParseFloat(m[2], 64)
		out[c] = w
	}
	// Strip compact pairs so the general parser doesn't see them.
	arr = reCompactW.ReplaceAllString(arr, " ")
	fields := strings.Fields(arr)
	i := 0
	for i < len(fields) {
		c1, err1 := strconv.Atoi(strings.TrimPrefix(fields[i], "/"))
		if err1 != nil {
			i++
			continue
		}
		if i+1 >= len(fields) {
			break
		}
		if fields[i+1] == "[" {
			// c [w1 w2 ...]
			j := i + 2
			c := c1
			for j < len(fields) && fields[j] != "]" {
				if w, err := strconv.ParseFloat(fields[j], 64); err == nil {
					out[c] = w
					c++
				}
				j++
			}
			i = j + 1
		} else if i+2 < len(fields) {
			// c1 c2 w
			if c2, err := strconv.Atoi(fields[i+1]); err == nil {
				if w, err2 := strconv.ParseFloat(fields[i+2], 64); err2 == nil {
					if c2 >= c1 && c2-c1 < 65536 {
						for c := c1; c <= c2; c++ {
							out[c] = w
						}
					}
				}
			}
			i += 3
		} else {
			i++
		}
	}
}

var reCompactW = regexp.MustCompile(`(\d+)\[(-?[\d.]+)\]`)

// extractNumValue extracts a float value for a key from a dict string.
func extractNumValue(dict, key string) float64 {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimLeft(dict[idx+len(key):], " \t\r\n")
	end := 0
	for end < len(rest) && (rest[end] >= '0' && rest[end] <= '9' || rest[end] == '.' || rest[end] == '-' || rest[end] == '+') {
		end++
	}
	if end == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(rest[:end], 64)
	if err != nil {
		return 0
	}
	return v
}

// extractNumberArray extracts an array of numbers for a key from a dict string.
func extractNumberArray(dict, key string) []float64 {
	idx := strings.Index(dict, key)
	if idx < 0 {
		return nil
	}
	rest := dict[idx+len(key):]
	start := strings.Index(rest, "[")
	if start < 0 {
		return nil
	}
	end := strings.Index(rest[start:], "]")
	if end < 0 {
		return nil
	}
	arr := rest[start+1 : start+end]
	fields := strings.Fields(arr)
	out := make([]float64, 0, len(fields))
	for _, f := range fields {
		if v, err := strconv.ParseFloat(f, 64); err == nil {
			out = append(out, v)
		} else {
			out = append(out, 0)
		}
	}
	return out
}

// glyphAdvance returns the advance width (in text space units) of a decoded
// byte code, using font widths when available (heuristic fallback 500/1000 em).
func glyphAdvance(fi *fontInfo, code int, fontSize float64) float64 {
	w := 500.0
	if fi != nil {
		if fi.defaultWidth > 0 {
			w = fi.defaultWidth
		}
		if v, ok := fi.widths[code]; ok && v > 0 {
			w = v
		}
	}
	return w / 1000.0 * fontSize
}

// trimPDFWS trims PDF whitespace from both ends of b.
func trimPDFWS(b []byte) []byte {
	start := 0
	for start < len(b) && isPDFSpace(b[start]) {
		start++
	}
	end := len(b)
	for end > start && isPDFSpace(b[end-1]) {
		end--
	}
	return b[start:end]
}

// fastParseFloat parses a PDF number token without allocating. Handles
// [-]digits[.digits]; anything else (exponents) falls back to strconv.
func fastParseFloat(b []byte) (float64, bool) {
	i := 0
	neg := false
	if i < len(b) && (b[i] == '-' || b[i] == '+') {
		neg = b[i] == '-'
		i++
	}
	intPart := 0
	digits := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		intPart = intPart*10 + int(b[i]-'0')
		i++
		digits++
	}
	frac := 0.0
	if i < len(b) && b[i] == '.' {
		i++
		scale := 0.1
		fdigits := 0
		for i < len(b) && b[i] >= '0' && b[i] <= '9' {
			frac += float64(b[i]-'0') * scale
			scale *= 0.1
			i++
			fdigits++
		}
		if digits == 0 && fdigits == 0 {
			return 0, false
		}
	} else if digits == 0 {
		return 0, false
	}
	if i != len(b) {
		// exponent or trailing junk — fall back to the full parser
		v, err := strconv.ParseFloat(string(b), 64)
		return v, err == nil
	}
	v := float64(intPart) + frac
	if neg {
		v = -v
	}
	return v, true
}

// shownAdvance computes the horizontal advance of a shown string token.
func shownAdvance(rawToken []byte, fi *fontInfo, fontSize float64) float64 {
	if fontSize <= 0 || len(rawToken) < 2 {
		return 0
	}
	var data []byte
	if rawToken[0] == '<' && rawToken[len(rawToken)-1] == '>' {
		data = hexBytes(trimPDFWS(rawToken[1 : len(rawToken)-1]))
	} else if rawToken[0] == '(' && rawToken[len(rawToken)-1] == ')' {
		data = []byte(unescapePDFString(string(rawToken[1 : len(rawToken)-1])))
	} else {
		return 0
	}
	step := 1
	if fi != nil && fi.codeSize == 2 {
		step = 2
	}
	total := 0.0
	for i := 0; i < len(data); i += step {
		var code int
		if step == 2 && i+1 < len(data) {
			code = int(data[i])<<8 | int(data[i+1])
		} else {
			code = int(data[i])
		}
		total += glyphAdvance(fi, code, fontSize)
	}
	return total
}

// shownWidth returns the shown advance only when the font carries real glyph
// width data; otherwise 0 (callers fall back to position-derived estimates).
func shownWidth(rawToken []byte, fi *fontInfo, fontSize float64) float64 {
	if fi == nil || len(fi.widths) == 0 {
		return 0
	}
	return shownAdvance(rawToken, fi, fontSize)
}

// tjWidth returns the TJ array advance when the font carries real widths.
func tjWidth(arr [][]byte, fi *fontInfo, fontSize float64) float64 {
	if fi == nil || len(fi.widths) == 0 {
		return 0
	}
	total := 0.0
	for _, item := range arr {
		if isStringToken(item) {
			total += shownAdvance(item, fi, fontSize)
		} else if v, ok := fastParseFloat(item); ok {
			total -= v / 1000.0 * fontSize
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

// parseTextOperators parses PDF content stream text operators.
// extractionContext carries what parseTextOperators needs beyond a single
// stream: the parser (to resolve Form XObjects), page resources, media box,
// recursion depth and visited-object tracking (anti-loop).
type extractionContext struct {
	parser   *rawPDFParser
	fonts    map[string]*fontInfo
	xobjs    map[string]int // resource name -> object number
	mediaBox [4]float64
	offX     float64 // Do translation offset already applied within this ctx
	offY     float64
	depth    int
	visited  map[int]bool
}

const maxFormDepth = 4

// isFormXObject reports whether an object dict describes a Form XObject.
func isFormXObject(dict string) bool {
	return strings.Contains(dict, "/Subtype /Form") || strings.Contains(dict, "/Subtype/Form")
}

// cachedFormFonts resolves a form's fonts once per parser instance.
func cachedFormFonts(parser *rawPDFParser, objNum int, dict string) map[string]*fontInfo {
	if m, ok := parser.fontCache[objNum]; ok {
		return m
	}
	m := buildFontMapFromDict(parser, dict)
	parser.fontCache[objNum] = m
	return m
}

// cachedFormXObjects resolves a form's nested XObjects once per instance.
func cachedFormXObjects(parser *rawPDFParser, objNum int, dict string) map[string]int {
	if m, ok := parser.xobjCache[objNum]; ok {
		return m
	}
	m := buildXObjMapFromDict(parser, dict)
	parser.xobjCache[objNum] = m
	return m
}

// buildFontMapFromDict builds the font map for resources referenced by an
// arbitrary dict (e.g. a Form XObject), mirroring the page-level logic.
func buildFontMapFromDict(parser *rawPDFParser, dict string) map[string]*fontInfo {
	res := rawPDFResources{
		fonts: make(map[string]int),
		xobjs: make(map[string]int),
	}
	resDict := dict
	if resRef := extractRef(dict, "/Resources"); resRef > 0 {
		if obj, ok := parser.objects[resRef]; ok {
			resDict = obj.dict
		}
	}
	parser.extractNamedRefs(resDict, "/Font", res.fonts)
	return buildFontMap(parser, rawPDFPage{resources: res})
}

// buildXObjMapFromDict builds the XObject name map for a dict's resources.
func buildXObjMapFromDict(parser *rawPDFParser, dict string) map[string]int {
	res := make(map[string]int)
	resDict := dict
	if resRef := extractRef(dict, "/Resources"); resRef > 0 {
		if obj, ok := parser.objects[resRef]; ok {
			resDict = obj.dict
		}
	}
	parser.extractNamedRefs(resDict, "/XObject", res)
	return res
}

// parseTextOperators parses a standalone stream (kept for callers that have
// no parser context).
func parseTextOperators(stream []byte, fonts map[string]*fontInfo, mediaBox [4]float64) []ExtractedText {
	return parseTextOperatorsCtx(&extractionContext{
		fonts:    fonts,
		mediaBox: mediaBox,
		visited:  map[int]bool{},
	}, stream)
}

// parseTextOperatorsCtx parses text operators, descending into Form XObjects.
func parseTextOperatorsCtx(ctx *extractionContext, stream []byte) []ExtractedText {
	tokens := tokenizeBytes(stream)
	// Pre-allocate with estimated capacity based on stream size.
	results := make([]ExtractedText, 0, len(stream)/100+16)

	// Text state
	var (
		inText   bool
		curFont  *fontInfo
		fontSize float64
		// Text matrix components
		tmX, tmY float64
		// Line matrix
		tlmX, tlmY float64
		// CTM (simplified — only tracking translation)
		ctmA, ctmD float64 = 1, 1
		ctmE, ctmF float64
		tl         float64 // text leading
	)
	pageHeight := ctx.mediaBox[3] - ctx.mediaBox[1]

	// Graphics-state stack for q/Q. A cm inside a q...Q group must not leak
	// into text positioned after the group — images are drawn with a
	// scaling cm (e.g. "200 0 0 100 72 642 cm /Im Do Q") and losing the Q
	// restore placed every later fragment at that matrix.
	ctmStack := make([][4]float64, 0, 8) // a, d, e, f
	pushCTM := func() {
		ctmStack = append(ctmStack, [4]float64{ctmA, ctmD, ctmE, ctmF})
	}
	popCTM := func() {
		if len(ctmStack) == 0 {
			return
		}
		top := ctmStack[len(ctmStack)-1]
		ctmStack = ctmStack[:len(ctmStack)-1]
		ctmA, ctmD, ctmE, ctmF = top[0], top[1], top[2], top[3]
	}

	stack := make([]float64, 0, 64)
	var scratch [8]float64 // reused by popN — operator arity is at most 6 (Tm/cm)
	popN := func(n int) []float64 {
		if len(stack) < n {
			stack = stack[:0]
			return scratch[:n]
		}
		copy(scratch[:n], stack[len(stack)-n:])
		stack = stack[:len(stack)-n]
		return scratch[:n]
	}

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		// Try to parse as number (allocation-free fast path)
		if v, ok := fastParseFloat(tok); ok {
			stack = append(stack, v)
			continue
		}

		switch string(tok) {
		case "BT":
			inText = true
			tmX, tmY = 0, 0
			tlmX, tlmY = 0, 0
		case "ET":
			inText = false
			stack = stack[:0]
		case "Tf":
			if !inText {
				continue
			}
			args := popN(1)
			fontSize = args[0]
			// font name is the token before the size number
			// We need to look back in the original tokens
			if i >= 2 {
				if bytes.HasPrefix(tokens[i-2], []byte("/")) {
					if f, ok := ctx.fonts[string(tokens[i-2])]; ok {
						curFont = f
					}
				}
			}
		case "Td":
			if !inText {
				continue
			}
			args := popN(2)
			tlmX += args[0]
			tlmY += args[1]
			tmX, tmY = tlmX, tlmY
		case "TD":
			if !inText {
				continue
			}
			args := popN(2)
			tl = -args[1]
			tlmX += args[0]
			tlmY += args[1]
			tmX, tmY = tlmX, tlmY
		case "Tm":
			if !inText {
				continue
			}
			args := popN(6)
			// args = [a b c d e f]
			tmX = args[4]
			tmY = args[5]
			tlmX, tlmY = tmX, tmY
			if args[0] != 0 {
				fontSize = math.Abs(args[0])
			}
		case "T*":
			if !inText {
				continue
			}
			tlmX += 0
			tlmY -= tl
			tmX, tmY = tlmX, tlmY
		case "TL":
			args := popN(1)
			tl = args[0]
		case "Tj":
			if !inText {
				continue
			}
			// String is the previous token (in parentheses or hex)
			if i >= 1 {
				raw := findStringBefore(tokens, i)
				text := decodeTextString(string(raw), curFont)
				if text != "" {
					x := ctx.offX + ctmA*tmX + ctmE
					y := (pageHeight - ctx.offY) - (ctmD*tmY + ctmF)
					results = append(results, ExtractedText{
						Text:     text,
						X:        x,
						Y:        y,
						FontName: fontDisplayName(curFont),
						FontSize: fontSize,
					})
				}
				// Advance the text position by the glyph widths shown.
				tmX += shownAdvance(raw, curFont, fontSize)
			}
		case "TJ":
			if !inText {
				continue
			}
			// TJ takes an array: [(string) num (string) ...]. Large positive
			// adjustments (≥ ~0.2 em) carry inter-word spaces in kerned fonts.
			arr := findArrayBefore(tokens, i)
			var combined strings.Builder
			advance := 0.0
			for _, item := range arr {
				if isStringToken(item) {
					combined.WriteString(decodeTextString(string(item), curFont))
					advance += shownAdvance(item, curFont, fontSize)
				} else if v, ok := fastParseFloat(item); ok {
					// TJ numbers displace position by tx = -v/1000 em
					// (spec 9.4.3): negative values move right, widening the
					// gap — those carry inter-word spaces in kerned fonts.
					advance -= v / 1000.0 * fontSize
					if v <= -200 && fontSize > 0 {
						// wide negative kern ≈ a space glyph
						if combined.Len() > 0 && !strings.HasSuffix(combined.String(), " ") {
							combined.WriteByte(' ')
						}
					}
				}
			}
			text := combined.String()
			if text != "" {
				x := ctx.offX + ctmA*tmX + ctmE
				y := (pageHeight - ctx.offY) - (ctmD*tmY + ctmF)
				results = append(results, ExtractedText{
					Text:     text,
					X:        x,
					Y:        y,
					FontName: fontDisplayName(curFont),
					FontSize: fontSize,
					Width:    tjWidth(arr, curFont, fontSize),
				})
			}
			tmX += advance
		case "'":
			// Move to next line and show string
			if !inText {
				continue
			}
			tlmY -= tl
			tmX, tmY = tlmX, tlmY
			if i >= 1 {
				raw := findStringBefore(tokens, i)
				text := decodeTextString(string(raw), curFont)
				if text != "" {
					x := ctx.offX + ctmA*tmX + ctmE
					y := (pageHeight - ctx.offY) - (ctmD*tmY + ctmF)
					results = append(results, ExtractedText{
						Text: text, X: x, Y: y,
						FontName: fontDisplayName(curFont), FontSize: fontSize,
						Width: shownWidth(raw, curFont, fontSize),
					})
				}
				tmX += shownAdvance(raw, curFont, fontSize)
			}
		case "\"":
			// aw ac string " — set word/char spacing, move to next line, show
			if !inText {
				continue
			}
			popN(2) // aw, ac
			tlmY -= tl
			tmX, tmY = tlmX, tlmY
			if i >= 1 {
				raw := findStringBefore(tokens, i)
				text := decodeTextString(string(raw), curFont)
				if text != "" {
					x := ctx.offX + ctmA*tmX + ctmE
					y := (pageHeight - ctx.offY) - (ctmD*tmY + ctmF)
					results = append(results, ExtractedText{
						Text: text, X: x, Y: y,
						FontName: fontDisplayName(curFont), FontSize: fontSize,
						Width: shownWidth(raw, curFont, fontSize),
					})
				}
				tmX += shownAdvance(raw, curFont, fontSize)
			}
		case "cm":
			args := popN(6)
			// CTM := cm × CTM (spec 4.2.3): a point already expressed in the
			// old ctm is appended by this cm, so translations and scales
			// compound instead of replacing each other.
			ctmA = args[0] * ctmA
			ctmD = args[3] * ctmD
			ctmE = args[0]*ctmE + args[4]
			ctmF = args[3]*ctmF + args[5]
		case "q":
			pushCTM()
		case "Q":
			popCTM()
		case "Do":
			// Form XObjects may carry more text (stamps, letterheads,
			// figure annotations). Recurse into /Subtype /Form streams with
			// the Do translation applied and the form's own fonts.
			if ctx.parser == nil || ctx.depth >= maxFormDepth {
				continue
			}
			if i < 1 || !bytes.HasPrefix(tokens[i-1], []byte("/")) {
				continue
			}
			name := string(tokens[i-1])
			objNum, ok := ctx.xobjs[name]
			if !ok || ctx.visited[objNum] {
				continue
			}
			obj, found := ctx.parser.objects[objNum]
			if !found || obj.stream == nil || !isFormXObject(obj.dict) {
				continue
			}
			pageH := ctx.mediaBox[3] - ctx.mediaBox[1] // runs are per page height
			runsForPage := ctx.parser.formRuns[objNum]
			if runsForPage == nil {
				runsForPage = make(map[float64][]ExtractedText)
				ctx.parser.formRuns[objNum] = runsForPage
			}
			baseRuns, ok := runsForPage[pageH]
			if !ok {
				subVisited := make(map[int]bool, len(ctx.visited)+1)
				for k, v := range ctx.visited {
					subVisited[k] = v
				}
				subVisited[objNum] = true
				sub := &extractionContext{
					parser:   ctx.parser,
					fonts:    cachedFormFonts(ctx.parser, objNum, obj.dict),
					xobjs:    cachedFormXObjects(ctx.parser, objNum, obj.dict),
					mediaBox: ctx.mediaBox,
					visited:  subVisited,
				}
				baseRuns = parseTextOperatorsCtx(sub, obj.stream)
				runsForPage[pageH] = baseRuns
			}
			// translate the cached un-offset runs by the cumulative Do offset
			offX := ctx.offX + ctmE
			offY := ctx.offY + ctmF
			for _, r := range baseRuns {
				r.X += offX
				r.Y -= offY
				results = append(results, r)
			}
		}
	}
	return results
}

// tokenize splits a PDF content stream into tokens.
//
// maxTokens bounds the token count as a safety measure against pathological
// streams; tokenization stops when the limit is reached (partial results are
// still usable). Use 0 for no limit.
// tokenize splits a content stream into string tokens (convenience wrapper
// over tokenizeBytes for callers outside the text path).
func tokenize(data []byte) []string {
	raw := tokenizeBytes(data)
	tokens := make([]string, len(raw))
	for i, b := range raw {
		tokens[i] = string(b)
	}
	return tokens
}

// interned delimiter tokens — appended without allocation
var (
	tokDictOpen   = []byte("<<")
	tokDictClose  = []byte(">>")
	tokArrayOpen  = []byte("[")
	tokArrayClose = []byte("]")
)

// tokenizeBytes splits a PDF content stream into zero-copy byte tokens.
func tokenizeBytes(data []byte) [][]byte {
	const maxTokens = 4_000_000
	// Pre-allocate with estimated capacity.
	tokens := make([][]byte, 0, len(data)/4+16)
	i := 0
	n := len(data)
	for i < n {
		// skip whitespace
		if data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n' || data[i] == '\f' || data[i] == 0 {
			i++
			continue
		}
		// comment
		if data[i] == '%' {
			for i < n && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			continue
		}
		// string literal (...)
		if data[i] == '(' {
			depth := 1
			start := i
			i++
			for i < n && depth > 0 {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == '(' {
					depth++
				} else if data[i] == ')' {
					depth--
				}
				i++
			}
			tokens = append(tokens, data[start:i])
			continue
		}
		// hex string <...>
		if data[i] == '<' && (i+1 >= n || data[i+1] != '<') {
			start := i
			i++
			for i < n && data[i] != '>' {
				i++
			}
			if i < n {
				i++ // skip >
			}
			tokens = append(tokens, data[start:i])
			continue
		}
		// dict << >>
		if data[i] == '<' && i+1 < n && data[i+1] == '<' {
			tokens = append(tokens, tokDictOpen)
			i += 2
			continue
		}
		if data[i] == '>' && i+1 < n && data[i+1] == '>' {
			tokens = append(tokens, tokDictClose)
			i += 2
			continue
		}
		// array delimiters
		if data[i] == '[' {
			tokens = append(tokens, tokArrayOpen)
			i++
			continue
		}
		if data[i] == ']' {
			tokens = append(tokens, tokArrayClose)
			i++
			continue
		}
		// regular token (name, number, operator)
		start := i
		for i < n && data[i] != ' ' && data[i] != '\t' && data[i] != '\r' &&
			data[i] != '\n' && data[i] != '\f' && data[i] != 0 &&
			data[i] != '(' && data[i] != ')' &&
			data[i] != '<' && data[i] != '>' && data[i] != '[' &&
			data[i] != ']' && data[i] != '%' {
			i++
		}
		if i > start {
			tokens = append(tokens, data[start:i])
		} else {
			// Unmatched delimiter (e.g. a stray ')' or '>'): skip it.
			// Without this the loop never advances and extraction hangs.
			i++
		}
		if len(tokens) >= maxTokens {
			break
		}
	}
	return tokens
}

func isStringToken(s []byte) bool {
	return (bytes.HasPrefix(s, []byte("(")) && bytes.HasSuffix(s, []byte(")"))) ||
		(bytes.HasPrefix(s, []byte("<")) && bytes.HasSuffix(s, []byte(">")) && !bytes.HasPrefix(s, tokDictOpen))
}

func findStringBefore(tokens [][]byte, opIdx int) []byte {
	for j := opIdx - 1; j >= 0; j-- {
		if isStringToken(tokens[j]) {
			return tokens[j]
		}
		// stop if we hit another operator
		if _, ok := fastParseFloat(tokens[j]); !ok && !bytes.Equal(tokens[j], tokArrayOpen) && !bytes.Equal(tokens[j], tokArrayClose) {
			break
		}
	}
	return nil
}

func findArrayBefore(tokens [][]byte, opIdx int) [][]byte {
	// Walk backwards to find matching [ ... ]
	end := -1
	start := -1
	for j := opIdx - 1; j >= 0; j-- {
		if bytes.Equal(tokens[j], tokArrayClose) {
			end = j
		}
		if bytes.Equal(tokens[j], tokArrayOpen) {
			start = j
			break
		}
	}
	if start >= 0 && end > start {
		return tokens[start+1 : end]
	}
	return nil
}

func fontDisplayName(fi *fontInfo) string {
	if fi == nil {
		return ""
	}
	if fi.baseFont != "" {
		return fi.baseFont
	}
	return fi.name
}

// decodeTextString decodes a PDF string token to a Go string, using the
// font's ToUnicode CMap, /Differences glyph names, base encodings, predefined
// CMaps, and legacy CJK decoders as available.
func decodeTextString(raw string, fi *fontInfo) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		// Hex string
		hex := raw[1 : len(raw)-1]
		hex = strings.ReplaceAll(hex, " ", "")
		hex = strings.ReplaceAll(hex, "\n", "")
		hex = strings.ReplaceAll(hex, "\r", "")
		return decodeStringBytes(hexBytes([]byte(hex)), fi)
	}
	if strings.HasPrefix(raw, "(") && strings.HasSuffix(raw, ")") {
		inner := raw[1 : len(raw)-1]
		inner = unescapePDFString(inner)
		return decodeStringBytes([]byte(inner), fi)
	}
	return raw
}

// hexToBytes converts a hex string (whitespace removed) to bytes.
func hexToBytes(hex string) []byte {
	if len(hex)%2 == 1 {
		hex += "0"
	}
	out := make([]byte, len(hex)/2)
	for i := 0; i+1 < len(hex); i += 2 {
		hi := hexVal(hex[i])
		lo := hexVal(hex[i+1])
		out[i/2] = byte(hi<<4 | lo)
	}
	return out
}

// hexBytes converts a hex byte slice (whitespace removed) to bytes.
// The input is copied, never mutated — callers pass views into the stream.
func hexBytes(hex []byte) []byte {
	clean := make([]byte, 0, len(hex)+1)
	for _, c := range hex {
		if !isPDFSpace(c) {
			clean = append(clean, c)
		}
	}
	if len(clean)%2 == 1 {
		clean = append(clean, '0')
	}
	out := make([]byte, len(clean)/2)
	for i := 0; i+1 < len(clean); i += 2 {
		hi := hexVal(clean[i])
		lo := hexVal(clean[i+1])
		out[i/2] = byte(hi<<4 | lo)
	}
	return out
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// decodeStringBytes decodes raw string bytes (from hex or literal strings)
// using the font's ToUnicode CMap, encoding tables, predefined CMaps and
// legacy CJK decoders as available.
func decodeStringBytes(data []byte, fi *fontInfo) string {
	// BOMs inside the string — PDF 2.0 text strings may be UTF-16BE
	// (FE FF) or UTF-8 (EF BB BF).
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return decodeUTF16BE(data[2:])
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return string(data[3:])
	}
	if fi == nil {
		return string(data)
	}
	if fi.toUni != nil {
		if fi.codeSize == 2 {
			return decodeCodesWithCMap(data, 2, fi.toUni)
		}
		return decodeCodesWithCMap(data, 1, fi.toUni)
	}
	if fi.isType0 {
		// Identity-style CID fonts without ToUnicode: the CIDs often equal
		// Unicode for UTF-16-built docs; otherwise fall through to encoding.
		if len(data)%2 == 0 {
			runes := utf16BeRunes(data)
			if isPlausibleText(runes) {
				return string(runes)
			}
		}
		if fi.cmapFamily != cmapFamilyUnknown {
			return decodeCJKBytes(fi.cmapFamily, data)
		}
		return string(data)
	}
	// Simple fonts: 1-byte codes.
	if len(fi.differences) > 0 {
		return decodeCodesWithDifferences(data, fi)
	}
	if fi.isSymbolicZap {
		// Symbol/ZapfDingbats: codes map to U+F020..U+F0FF display range.
		var sb strings.Builder
		for _, b := range data {
			sb.WriteRune(0xF000 + rune(b))
		}
		return sb.String()
	}
	if fi.baseEncoding != "" {
		var sb strings.Builder
		for _, b := range data {
			if r, ok := decodeBaseEncodingByte(b, fi.baseEncoding); ok {
				sb.WriteRune(r)
			} else {
				sb.WriteByte(b)
			}
		}
		out := sb.String()
		if alt, ok2 := tryDecodingWithAltEncoders(data, fi); ok2 {
			return alt
		}
		return out
	}
	return string(data)
}

// decodeCodesWithCMap maps byte codes (codeSize bytes each) through the CMap.
func decodeCodesWithCMap(data []byte, codeSize int, cmap codeToUnicode) string {
	var sb strings.Builder
	step := codeSize
	if step < 1 {
		step = 1
	}
	for i := 0; i < len(data); i += step {
		var code uint16
		if step == 2 && i+1 < len(data) {
			code = uint16(data[i])<<8 | uint16(data[i+1])
		} else {
			code = uint16(data[i])
		}
		if s, ok := cmap[code]; ok {
			sb.WriteString(s)
		} else if code >= 0x20 && code < 0x7F {
			sb.WriteByte(byte(code))
		}
	}
	return sb.String()
}

// decodeCodesWithDifferences maps 1-byte codes through the Differences array.
func decodeCodesWithDifferences(data []byte, fi *fontInfo) string {
	var sb strings.Builder
	for _, b := range data {
		if name, ok := fi.differences[b]; ok {
			if r, ok2 := decodeGlyphName(name); ok2 {
				sb.WriteRune(r)
				continue
			}
		}
		if r, ok := decodeBaseEncodingByte(b, fi.baseEncoding); ok && r >= 0x20 {
			sb.WriteRune(r)
		} else if b >= 0x20 && b < 0x7F {
			sb.WriteByte(b)
		}
	}
	return sb.String()
}

// isPlausibleText reports whether the runes look like real text rather than
// random CID values (heuristic: mostly printable).
func isPlausibleText(runes []rune) bool {
	if len(runes) == 0 {
		return false
	}
	printable := 0
	for _, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' || (r >= 0x20 && r != 0xFFFD) {
			printable++
		}
	}
	return printable*10 >= len(runes)*7
}

func decodeHexUTF16BE(hex string) string {
	if len(hex)%4 != 0 {
		// fall back to latin
		return decodeHexLatin(hex)
	}
	codes := make([]uint16, 0, len(hex)/4)
	for i := 0; i+3 < len(hex); i += 4 {
		codes = append(codes, parseHex16(hex[i:i+4]))
	}
	return string(utf16.Decode(codes))
}

func decodeHexLatin(hex string) string {
	var sb strings.Builder
	for i := 0; i+1 < len(hex); i += 2 {
		b := parseHex16(hex[i : i+2])
		sb.WriteByte(byte(b))
	}
	return sb.String()
}

func decodeUTF16BE(data []byte) string {
	if len(data)%2 != 0 {
		data = append(data, 0)
	}
	codes := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		codes = append(codes, uint16(data[i])<<8|uint16(data[i+1]))
	}
	return string(utf16.Decode(codes))
}

func unescapePDFString(s string) string {
	var buf bytes.Buffer
	i := 0
	for i < len(s) {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				buf.WriteByte('\n')
			case 'r':
				buf.WriteByte('\r')
			case 't':
				buf.WriteByte('\t')
			case 'b':
				buf.WriteByte('\b')
			case 'f':
				buf.WriteByte('\f')
			case '(':
				buf.WriteByte('(')
			case ')':
				buf.WriteByte(')')
			case '\\':
				buf.WriteByte('\\')
			default:
				// octal
				if s[i] >= '0' && s[i] <= '7' {
					oct := string(s[i])
					i++
					for j := 0; j < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; j++ {
						oct += string(s[i])
						i++
					}
					v, _ := strconv.ParseUint(oct, 8, 8)
					buf.WriteByte(byte(v))
					continue
				}
				buf.WriteByte(s[i])
			}
		} else {
			buf.WriteByte(s[i])
		}
		i++
	}
	return buf.String()
}
