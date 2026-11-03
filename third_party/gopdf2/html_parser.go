package gopdf

import (
	"fmt"
	"strconv"
	"strings"
)

// htmlNodeType represents the type of an HTML node.
type htmlNodeType int

const (
	htmlNodeText    htmlNodeType = iota // plain text
	htmlNodeElement                     // an HTML element like <b>, <p>, etc.
)

// htmlNode represents a parsed HTML node (either text or element).
type htmlNode struct {
	Type      htmlNodeType
	Tag       string // lowercase tag name, e.g. "b", "p", "br", "img"
	Text      string // text content (for text nodes)
	Attrs     map[string]string
	Children  []*htmlNode
	SelfClose bool // e.g. <br/>, <img ... />
}

// parseHTML parses a simplified HTML string into a tree of htmlNode.
// Supported tags: b, i, u, s, br, p, div, span, font, img, h1-h6, ul, ol, li,
// a, hr, sub, sup, center, blockquote, pre, code, table/thead/tbody/tfoot/tr/
// th/td/caption/colgroup/col, style (raw text), script (raw text, ignored).
func parseHTML(html string) []*htmlNode {
	p := &htmlParser{input: html}
	return p.parseNodes()
}

type htmlParser struct {
	input string
	pos   int
}

func (p *htmlParser) parseNodes() []*htmlNode {
	var nodes []*htmlNode
	for p.pos < len(p.input) {
		if p.input[p.pos] == '<' {
			// check for closing tag
			if p.pos+1 < len(p.input) && p.input[p.pos+1] == '/' {
				// closing tag — return to parent
				return nodes
			}
			// check for comment <!-- ... -->
			if p.pos+3 < len(p.input) && p.input[p.pos:p.pos+4] == "<!--" {
				p.skipComment()
				continue
			}
			node := p.parseElement()
			if node != nil {
				nodes = append(nodes, node)
			}
		} else {
			node := p.parseText()
			if node != nil {
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

func (p *htmlParser) skipComment() {
	p.pos += 4 // skip "<!--"
	for p.pos < len(p.input)-2 {
		if p.input[p.pos:p.pos+3] == "-->" {
			p.pos += 3
			return
		}
		p.pos++
	}
	p.pos = len(p.input)
}

func (p *htmlParser) parseText() *htmlNode {
	start := p.pos
	for p.pos < len(p.input) && p.input[p.pos] != '<' {
		p.pos++
	}
	text := p.input[start:p.pos]
	text = decodeHTMLEntities(text)
	if len(text) == 0 {
		return nil
	}
	return &htmlNode{Type: htmlNodeText, Text: text}
}

func (p *htmlParser) parseElement() *htmlNode {
	if p.pos >= len(p.input) || p.input[p.pos] != '<' {
		return nil
	}
	p.pos++ // skip '<'
	p.skipWhitespace()

	tag := p.readTagName()
	if tag == "" {
		return nil
	}

	attrs := p.parseAttributes()
	p.skipWhitespace()

	selfClose := false
	if p.pos < len(p.input) && p.input[p.pos] == '/' {
		selfClose = true
		p.pos++
	}
	if p.pos < len(p.input) && p.input[p.pos] == '>' {
		p.pos++
	}

	node := &htmlNode{
		Type:      htmlNodeElement,
		Tag:       strings.ToLower(tag),
		Attrs:     attrs,
		SelfClose: selfClose,
	}

	// self-closing or void elements
	if selfClose || isVoidElement(node.Tag) {
		return node
	}

	// <script> and <style> contain raw text (markup inside must not be
	// parsed): consume everything up to the matching close tag verbatim
	if node.Tag == "script" || node.Tag == "style" {
		closeTag := "</" + node.Tag
		rest := strings.ToLower(p.input[p.pos:])
		idx := strings.Index(rest, closeTag)
		if idx >= 0 {
			node.Children = []*htmlNode{{Type: htmlNodeText, Text: p.input[p.pos : p.pos+idx]}}
			p.pos += idx
		} else {
			// unterminated: consume the rest as raw text
			node.Children = []*htmlNode{{Type: htmlNodeText, Text: p.input[p.pos:]}}
			p.pos = len(p.input)
			return node
		}
		p.consumeClosingTag(node.Tag)
		return node
	}

	// parse children until closing tag
	node.Children = p.parseNodes()

	// consume closing tag </tag>
	p.consumeClosingTag(node.Tag)

	return node
}

func (p *htmlParser) readTagName() string {
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '>' || ch == '/' || ch == '\t' || ch == '\n' || ch == '\r' {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *htmlParser) parseAttributes() map[string]string {
	attrs := make(map[string]string)
	for {
		p.skipWhitespace()
		if p.pos >= len(p.input) {
			break
		}
		ch := p.input[p.pos]
		if ch == '>' || ch == '/' {
			break
		}
		key := p.readAttrName()
		if key == "" {
			p.pos++
			continue
		}
		p.skipWhitespace()
		value := ""
		if p.pos < len(p.input) && p.input[p.pos] == '=' {
			p.pos++ // skip '='
			p.skipWhitespace()
			value = p.readAttrValue()
		}
		attrs[strings.ToLower(key)] = value
	}
	return attrs
}

func (p *htmlParser) readAttrName() string {
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == '=' || ch == ' ' || ch == '>' || ch == '/' || ch == '\t' || ch == '\n' {
			break
		}
		p.pos++
	}
	return p.input[start:p.pos]
}

func (p *htmlParser) readAttrValue() string {
	if p.pos >= len(p.input) {
		return ""
	}
	ch := p.input[p.pos]
	if ch == '"' || ch == '\'' {
		quote := ch
		p.pos++
		start := p.pos
		for p.pos < len(p.input) && p.input[p.pos] != quote {
			p.pos++
		}
		val := p.input[start:p.pos]
		if p.pos < len(p.input) {
			p.pos++ // skip closing quote
		}
		return decodeHTMLEntities(val)
	}
	// unquoted value
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch == ' ' || ch == '>' || ch == '/' || ch == '\t' || ch == '\n' {
			break
		}
		p.pos++
	}
	return decodeHTMLEntities(p.input[start:p.pos])
}

func (p *htmlParser) consumeClosingTag(tag string) {
	// look for </tag>
	if p.pos+1 < len(p.input) && p.input[p.pos] == '<' && p.input[p.pos+1] == '/' {
		p.pos += 2
		p.skipWhitespace()
		p.readTagName() // consume tag name
		p.skipWhitespace()
		if p.pos < len(p.input) && p.input[p.pos] == '>' {
			p.pos++
		}
	}
}

func (p *htmlParser) skipWhitespace() {
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if ch != ' ' && ch != '\t' && ch != '\n' && ch != '\r' {
			break
		}
		p.pos++
	}
}

// isVoidElement reports tags that never have children.
func isVoidElement(tag string) bool {
	switch tag {
	case "br", "hr", "img", "col", "input", "meta", "link", "area", "base", "wbr":
		return true
	}
	return false
}

// isTransparentContainer reports elements whose children should be rendered
// inline without layout effects of their own (head metadata is dropped).
func isTransparentContainer(tag string) bool {
	switch tag {
	case "html", "body", "head", "header", "footer", "main", "article", "section", "nav", "aside":
		return true
	}
	return false
}

// decodeHTMLEntities decodes named and numeric HTML entities.
func decodeHTMLEntities(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '&' {
			// numeric: &#123; or &#x1F600;
			if i+2 < len(s) && s[i+1] == '#' {
				end := strings.IndexByte(s[i:], ';')
				if end > 2 && end <= 10 {
					numStr := s[i+2 : i+end]
					var cp int
					var ok bool
					if len(numStr) > 1 && (numStr[0] == 'x' || numStr[0] == 'X') {
						v, err := strconv.ParseUint(numStr[1:], 16, 32)
						cp, ok = int(v), err == nil
					} else {
						v, err := strconv.Atoi(numStr)
						cp, ok = v, err == nil
					}
					if ok && cp > 0 && cp <= 0x10FFFF && !(cp >= 0xD800 && cp <= 0xDFFF) {
						sb.WriteRune(rune(cp))
						i += end + 1
						continue
					}
				}
			}
			// named
			if end := strings.IndexByte(s[i:], ';'); end > 1 && end <= 32 {
				name := s[i+1 : i+end]
				if r, ok := htmlNamedEntities[name]; ok {
					sb.WriteRune(r)
					i += end + 1
					continue
				}
			}
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

// htmlNamedEntities covers common named entities beyond the XML five.
var htmlNamedEntities = map[string]rune{
	"amp":     '&',
	"lt":      '<',
	"gt":      '>',
	"quot":    '"',
	"apos":    '\'',
	"nbsp":    0x00A0,
	"iexcl":   0x00A1,
	"cent":    0x00A2,
	"pound":   0x00A3,
	"yen":     0x00A5,
	"brvbar":  0x00A6,
	"sect":    0x00A7,
	"uml":     0x00A8,
	"copy":    0x00A9,
	"ordf":    0x00AA,
	"laquo":   0x00AB,
	"not":     0x00AC,
	"shy":     0x00AD,
	"reg":     0x00AE,
	"macr":    0x00AF,
	"deg":     0x00B0,
	"plusmn":  0x00B1,
	"sup2":    0x00B2,
	"sup3":    0x00B3,
	"acute":   0x00B4,
	"micro":   0x00B5,
	"para":    0x00B6,
	"middot":  0x00B7,
	"cedil":   0x00B8,
	"sup1":    0x00B9,
	"ordm":    0x00BA,
	"raquo":   0x00BB,
	"frac14":  0x00BC,
	"frac12":  0x00BD,
	"frac34":  0x00BE,
	"iquest":  0x00BF,
	"Agrave":  0x00C0,
	"Aacute":  0x00C1,
	"Acirc":   0x00C2,
	"Atilde":  0x00C3,
	"Auml":    0x00C4,
	"Aring":   0x00C5,
	"AElig":   0x00C6,
	"Ccedil":  0x00C7,
	"Egrave":  0x00C8,
	"Eacute":  0x00C9,
	"Ecirc":   0x00CA,
	"Euml":    0x00CB,
	"Igrave":  0x00CC,
	"Iacute":  0x00CD,
	"Icirc":   0x00CE,
	"Iuml":    0x00CF,
	"ETH":     0x00D0,
	"Ntilde":  0x00D1,
	"Ograve":  0x00D2,
	"Oacute":  0x00D3,
	"Ocirc":   0x00D4,
	"Otilde":  0x00D5,
	"Ouml":    0x00D6,
	"times":   0x00D7,
	"Oslash":  0x00D8,
	"Ugrave":  0x00D9,
	"Uacute":  0x00DA,
	"Ucirc":   0x00DB,
	"Uuml":    0x00DC,
	"Yacute":  0x00DD,
	"THORN":   0x00DE,
	"szlig":   0x00DF,
	"agrave":  0x00E0,
	"aacute":  0x00E1,
	"acirc":   0x00E2,
	"atilde":  0x00E3,
	"auml":    0x00E4,
	"aring":   0x00E5,
	"aelig":   0x00E6,
	"ccedil":  0x00E7,
	"egrave":  0x00E8,
	"eacute":  0x00E9,
	"ecirc":   0x00EA,
	"euml":    0x00EB,
	"igrave":  0x00EC,
	"iacute":  0x00ED,
	"icirc":   0x00EE,
	"iuml":    0x00EF,
	"eth":     0x00F0,
	"ntilde":  0x00F1,
	"ograve":  0x00F2,
	"oacute":  0x00F3,
	"ocirc":   0x00F4,
	"otilde":  0x00F5,
	"ouml":    0x00F6,
	"divide":  0x00F7,
	"oslash":  0x00F8,
	"ugrave":  0x00F9,
	"uacute":  0x00FA,
	"ucirc":   0x00FB,
	"uuml":    0x00FC,
	"yacute":  0x00FD,
	"thorn":   0x00FE,
	"yuml":    0x00FF,
	"OElig":   0x0152,
	"oelig":   0x0153,
	"Scaron":  0x0160,
	"scaron":  0x0161,
	"Yuml":    0x0178,
	"fnof":    0x0192,
	"circ":    0x02C6,
	"tilde":   0x02DC,
	"Alpha":   0x0391,
	"Beta":    0x0392,
	"Gamma":   0x0393,
	"Delta":   0x0394,
	"Epsilon": 0x0395,
	"Zeta":    0x0396,
	"Eta":     0x0397,
	"Theta":   0x0398,
	"Iota":    0x0399,
	"Kappa":   0x039A,
	"Lambda":  0x039B,
	"Mu":      0x039C,
	"Nu":      0x039D,
	"Xi":      0x039E,
	"Omicron": 0x039F,
	"Pi":      0x03A0,
	"Rho":     0x03A1,
	"Sigma":   0x03A3,
	"Tau":     0x03A4,
	"Upsilon": 0x03A5,
	"Phi":     0x03A6,
	"Chi":     0x03A7,
	"Psi":     0x03A8,
	"Omega":   0x03A9,
	"alpha":   0x03B1,
	"beta":    0x03B2,
	"gamma":   0x03B3,
	"delta":   0x03B4,
	"epsilon": 0x03B5,
	"zeta":    0x03B6,
	"eta":     0x03B7,
	"theta":   0x03B8,
	"iota":    0x03B9,
	"kappa":   0x03BA,
	"lambda":  0x03BB,
	"mu":      0x03BC,
	"nu":      0x03BD,
	"xi":      0x03BE,
	"omicron": 0x03BF,
	"pi":      0x03C0,
	"rho":     0x03C1,
	"sigmaf":  0x03C2,
	"sigma":   0x03C3,
	"tau":     0x03C4,
	"upsilon": 0x03C5,
	"phi":     0x03C6,
	"chi":     0x03C7,
	"psi":     0x03C8,
	"omega":   0x03C9,
	"ensp":    0x2002,
	"emsp":    0x2003,
	"thinsp":  0x2009,
	"ndash":   0x2013,
	"mdash":   0x2014,
	"lsquo":   0x2018,
	"rsquo":   0x2019,
	"sbquo":   0x201A,
	"ldquo":   0x201C,
	"rdquo":   0x201D,
	"bdquo":   0x201E,
	"dagger":  0x2020,
	"Dagger":  0x2021,
	"bull":    0x2022,
	"hellip":  0x2026,
	"permil":  0x2030,
	"prime":   0x2032,
	"Prime":   0x2033,
	"lsaquo":  0x2039,
	"rsaquo":  0x203A,
	"oline":   0x203E,
	"euro":    0x20AC,
	"trade":   0x2122,
	"larr":    0x2190,
	"uarr":    0x2191,
	"rarr":    0x2192,
	"darr":    0x2193,
	"harr":    0x2194,
	"minus":   0x2212,
	"lowast":  0x2217,
	"radic":   0x221A,
	"prop":    0x221D,
	"infin":   0x221E,
	"ang":     0x2220,
	"and":     0x2227,
	"or":      0x2228,
	"cap":     0x2229,
	"cup":     0x222A,
	"int":     0x222B,
	"there4":  0x2234,
	"sim":     0x223C,
	"cong":    0x2245,
	"asymp":   0x2248,
	"ne":      0x2260,
	"equiv":   0x2261,
	"le":      0x2264,
	"ge":      0x2265,
	"sub":     0x2282,
	"sup":     0x2283,
	"nsub":    0x2284,
	"sube":    0x2286,
	"supe":    0x2287,
	"oplus":   0x2295,
	"otimes":  0x2297,
	"perp":    0x22A5,
	"sdot":    0x22C5,
	"lceil":   0x2308,
	"rceil":   0x2309,
	"lfloor":  0x230A,
	"rfloor":  0x230B,
	"loz":     0x25CA,
	"spades":  0x2660,
	"clubs":   0x2663,
	"hearts":  0x2665,
	"diams":   0x2666,
	"star":    0x2605,
	"check":   0x2713,
	"cross":   0x2717,
}

// parseInlineStyle parses a CSS-like inline style string into a map.
// e.g. "color: red; font-size: 14px" => {"color":"red", "font-size":"14px"}
func parseInlineStyle(style string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(style, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.TrimSpace(part[idx+1:])
		result[strings.ToLower(key)] = val
	}
	return result
}

// parseCSSColor parses a color string and returns r, g, b values.
// Supports: #RGB, #RRGGBB, rgb(r,g,b), and named colors.
func parseCSSColor(color string) (uint8, uint8, uint8, bool) {
	color = strings.TrimSpace(strings.ToLower(color))

	// named colors
	if rgb, ok := cssNamedColors[color]; ok {
		return rgb[0], rgb[1], rgb[2], true
	}

	// hex colors
	if strings.HasPrefix(color, "#") {
		hex := color[1:]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		if len(hex) == 6 {
			r, err1 := strconv.ParseUint(hex[0:2], 16, 8)
			g, err2 := strconv.ParseUint(hex[2:4], 16, 8)
			b, err3 := strconv.ParseUint(hex[4:6], 16, 8)
			if err1 == nil && err2 == nil && err3 == nil {
				return uint8(r), uint8(g), uint8(b), true
			}
		}
		return 0, 0, 0, false
	}

	// rgb(r, g, b) and rgba(r, g, b, a) — alpha is ignored (PDF text has no
	// alpha in this code path)
	isRGBA := strings.HasPrefix(color, "rgba(") && strings.HasSuffix(color, ")")
	if isRGBA || (strings.HasPrefix(color, "rgb(") && strings.HasSuffix(color, ")")) {
		prefixLen := 4
		if isRGBA {
			prefixLen = 5
		}
		inner := color[prefixLen : len(color)-1]
		parts := strings.Split(inner, ",")
		if len(parts) == 3 || (isRGBA && len(parts) == 4) {
			r, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			g, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
			b, err3 := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err1 == nil && err2 == nil && err3 == nil {
				return uint8(r), uint8(g), uint8(b), true
			}
		}
	}

	return 0, 0, 0, false
}

// parseFontSize parses a CSS font-size value and returns the size in points.
// Supports: "12pt", "16px", "1.5em", plain numbers, and named sizes.
func parseFontSize(val string, currentSize float64) (float64, bool) {
	val = strings.TrimSpace(strings.ToLower(val))

	// named sizes
	switch val {
	case "xx-small":
		return 6, true
	case "x-small":
		return 7.5, true
	case "small":
		return 10, true
	case "medium":
		return 12, true
	case "large":
		return 14, true
	case "x-large":
		return 18, true
	case "xx-large":
		return 24, true
	}

	if strings.HasSuffix(val, "pt") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "pt"), 64)
		if err == nil {
			return f, true
		}
	}
	if strings.HasSuffix(val, "px") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "px"), 64)
		if err == nil {
			return f * 0.75, true // px to pt
		}
	}
	if strings.HasSuffix(val, "em") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "em"), 64)
		if err == nil {
			return currentSize * f, true
		}
	}
	if strings.HasSuffix(val, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64)
		if err == nil {
			return currentSize * f / 100.0, true
		}
	}

	// plain number (treat as pt)
	f, err := strconv.ParseFloat(val, 64)
	if err == nil {
		return f, true
	}

	return 0, false
}

// parseDimension parses a CSS dimension value (e.g. "100px", "50%", "3em") and returns points.
func parseDimension(val string, relativeTo float64) (float64, bool) {
	val = strings.TrimSpace(strings.ToLower(val))
	if strings.HasSuffix(val, "px") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "px"), 64)
		if err == nil {
			return f * 0.75, true
		}
	}
	if strings.HasSuffix(val, "pt") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "pt"), 64)
		if err == nil {
			return f, true
		}
	}
	if strings.HasSuffix(val, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "%"), 64)
		if err == nil {
			return relativeTo * f / 100.0, true
		}
	}
	if strings.HasSuffix(val, "em") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(val, "em"), 64)
		if err == nil {
			return f * 12, true // assume 12pt base
		}
	}
	// plain number
	f, err := strconv.ParseFloat(val, 64)
	if err == nil {
		return f, true
	}
	return 0, false
}

// parseFontSizeAttr parses the HTML <font size="..."> attribute (1-7 scale).
func parseFontSizeAttr(val string) (float64, bool) {
	val = strings.TrimSpace(val)
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}
	// HTML font size 1-7 mapping to pt
	sizes := map[int]float64{
		1: 8, 2: 10, 3: 12, 4: 14, 5: 18, 6: 24, 7: 36,
	}
	if sz, ok := sizes[n]; ok {
		return sz, true
	}
	return 12, true
}

// headingFontSize returns the font size for h1-h6 tags.
func headingFontSize(tag string) float64 {
	switch tag {
	case "h1":
		return 24
	case "h2":
		return 20
	case "h3":
		return 16
	case "h4":
		return 14
	case "h5":
		return 12
	case "h6":
		return 10
	}
	return 12
}

// isBlockElement returns true if the tag is a block-level element.
func isBlockElement(tag string) bool {
	switch tag {
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "li", "hr", "center", "blockquote", "table", "thead", "tbody", "tfoot", "tr":
		return true
	}
	return false
}

var cssNamedColors = map[string][3]uint8{
	"black":   {0, 0, 0},
	"white":   {255, 255, 255},
	"red":     {255, 0, 0},
	"green":   {0, 128, 0},
	"blue":    {0, 0, 255},
	"yellow":  {255, 255, 0},
	"cyan":    {0, 255, 255},
	"magenta": {255, 0, 255},
	"gray":    {128, 128, 128},
	"grey":    {128, 128, 128},
	"orange":  {255, 165, 0},
	"purple":  {128, 0, 128},
	"brown":   {165, 42, 42},
	"pink":    {255, 192, 203},
	"navy":    {0, 0, 128},
	"teal":    {0, 128, 128},
	"olive":   {128, 128, 0},
	"maroon":  {128, 0, 0},
	"silver":  {192, 192, 192},
	"lime":    {0, 255, 0},
	"aqua":    {0, 255, 255},
	"fuchsia": {255, 0, 255},
}

// htmlFontSizeToFloat converts an HTML <font size="N"> attribute to a float64 font size.
func htmlFontSizeToFloat(sizeStr string) float64 {
	sz, ok := parseFontSizeAttr(sizeStr)
	if !ok {
		return 12
	}
	return sz
}

// debugHTMLTree prints the HTML tree for debugging (not used in production).
func debugHTMLTree(nodes []*htmlNode, indent int) string {
	var sb strings.Builder
	prefix := strings.Repeat("  ", indent)
	for _, n := range nodes {
		if n.Type == htmlNodeText {
			sb.WriteString(fmt.Sprintf("%sTEXT: %q\n", prefix, n.Text))
		} else {
			sb.WriteString(fmt.Sprintf("%s<%s", prefix, n.Tag))
			for k, v := range n.Attrs {
				sb.WriteString(fmt.Sprintf(" %s=%q", k, v))
			}
			if n.SelfClose {
				sb.WriteString(" />\n")
			} else {
				sb.WriteString(">\n")
				sb.WriteString(debugHTMLTree(n.Children, indent+1))
				sb.WriteString(fmt.Sprintf("%s</%s>\n", prefix, n.Tag))
			}
		}
	}
	return sb.String()
}
