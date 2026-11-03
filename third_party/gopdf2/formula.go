package gopdf

import (
	"strings"
)

// ============================================================
// Formula rendering — a practical LaTeX-lite subset.
//
// Supported syntax:
//   - superscripts and subscripts:  x^2  a_i  x^{n+1}  a_{ij}
//   - fractions:                    \frac{a}{b}
//   - square roots:                 \sqrt{x}
//   - Greek letters:                \alpha \beta \gamma ... \Omega
//   - common operators/symbols:     \sum \int \prod \times \cdot \div
//                                   \pm \leq \geq \neq \approx \infty
//                                   \rightarrow \leftarrow \in \partial
//   - function names:               \sin \cos \tan \log \ln \lim \exp
//   - plain parentheses and braces (escaped \{ \})
//
// Example:
//
//	pdf.SetFont("lib", "", 12)
//	pdf.DrawFormula(50, 100, `E = mc^2`, 12, nil)
//	pdf.DrawFormula(50, 130, `\int_0^\infty e^{-x^2} dx = \frac{\sqrt{\pi}}{2}`, 12, nil)
// ============================================================

// FormulaOption configures formula rendering.
type FormulaOption struct {
	// FontFamily must already be registered via AddTTFFont. Defaults to the
	// currently selected font when empty.
	FontFamily string
	// Color of the formula glyphs (r, g, b). Defaults to current text color.
	Color *[3]uint8
	// Italic renders variables in italic (classic math style) when a matching
	// italic font family is registered. Default false.
	Italic bool
}

// formulaToken is a parsed formula element.
type formulaToken struct {
	kind     byte // 't' text, '^' sup, '_' sub, 'f' frac, 'q' sqrt
	text     string
	children []*formulaToken // for sup/sub/frac numerator+denominator
}

// latexSymbols maps LaTeX control names to Unicode text.
var latexSymbols = map[string]string{
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε",
	"varepsilon": "ε", "zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ",
	"iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
	"pi": "π", "rho": "ρ", "sigma": "σ", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "ϕ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ",
	"Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	"sum": "∑", "prod": "∏", "int": "∫", "iint": "∬", "oint": "∮",
	"times": "×", "cdot": "·", "div": "÷", "pm": "±", "mp": "∓",
	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠",
	"approx": "≈", "equiv": "≡", "sim": "∼", "propto": "∝",
	"infty": "∞", "partial": "∂", "nabla": "∇", "hbar": "ℏ",
	"in": "∈", "notin": "∉", "subset": "⊂", "supset": "⊃", "subseteq": "⊆",
	"cup": "∪", "cap": "∩", "emptyset": "∅", "forall": "∀", "exists": "∃",
	"rightarrow": "→", "to": "→", "leftarrow": "←", "Rightarrow": "⇒",
	"Leftarrow": "⇐", "leftrightarrow": "↔", "mapsto": "↦",
	"ldots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱",
	"prime": "′", "degree": "°", "circ": "∘", "bullet": "•",
	"langle": "⟨", "rangle": "⟩", "perp": "⊥", "parallel": "∥",
	"angle": "∠", "triangle": "△", "square": "□", "star": "★",
	"sqrt": "√", "ln": "ln", "log": "log", "sin": "sin", "cos": "cos",
	"tan": "tan", "cot": "cot", "sec": "sec", "csc": "csc", "exp": "exp",
	"lim": "lim", "max": "max", "min": "min", "arg": "arg", "deg": "deg",
	"left": "", "right": "", ",": " ", ";": " ", ":": " ", " ": " ", "quad": "  ",
}

// parseFormula parses a LaTeX-lite string into tokens.
func parseFormula(s string) []*formulaToken {
	var tokens []*formulaToken
	i := 0
	n := len(s)

	parseGroup := func() (string, int) {
		// s[i] == '{'; returns content and new index past '}'
		depth := 0
		start := i + 1
		j := i
		for j < n {
			if s[j] == '{' {
				depth++
			} else if s[j] == '}' {
				depth--
				if depth == 0 {
					return s[start:j], j + 1
				}
			}
			j++
		}
		return s[start:], n
	}

	readArg := func() []*formulaToken {
		for i < n && s[i] == ' ' {
			i++
		}
		if i < n && s[i] == '{' {
			content, ni := parseGroup()
			i = ni
			return parseFormula(content)
		}
		// single token (char or command)
		if i < n && s[i] == '\\' {
			j := i + 1
			for j < n && isAlphaByte(s[j]) {
				j++
			}
			if j > i+1 {
				tok := &formulaToken{kind: 't', text: latexCommand(s[i+1 : j])}
				i = j
				return []*formulaToken{tok}
			}
		}
		if i < n {
			tok := &formulaToken{kind: 't', text: string(s[i])}
			i++
			return []*formulaToken{tok}
		}
		return nil
	}

	for i < n {
		ch := s[i]
		switch ch {
		case '\\':
			j := i + 1
			for j < n && isAlphaByte(s[j]) {
				j++
			}
			name := s[i+1 : j]
			if j == i+1 && j < n {
				// escaped symbol like \{ \} \\ \,
				name = string(s[j])
				j++
			}
			i = j
			switch name {
			case "binom":
				// binomial coefficient: ( n over k ) with big parens
				top := readArg()
				bot := readArg()
				tok := &formulaToken{kind: 'b'}
				tok.children = append(tok.children, top...)
				tok.children = append(tok.children, &formulaToken{kind: '|'})
				tok.children = append(tok.children, bot...)
				tokens = append(tokens, tok)
			case "frac":
				num := readArg()
				den := readArg()
				tok := &formulaToken{kind: 'f'}
				tok.children = append(tok.children, num...)
				tok.children = append(tok.children, &formulaToken{kind: '|'}) // separator marker
				tok.children = append(tok.children, den...)
				tokens = append(tokens, tok)
			case "sqrt":
				arg := readArg()
				tok := &formulaToken{kind: 'q', children: arg}
				tokens = append(tokens, tok)
			case "left", "right":
				// consume the delimiter character
				if i < n && s[i] != '\\' {
					delim := s[i]
					i++
					if delim != '.' {
						tokens = append(tokens, &formulaToken{kind: 't', text: string(mapLatexDelim(delim))})
					}
				} else if i < n {
					// \left\{ etc.
					j := i + 1
					for j < n && isAlphaByte(s[j]) {
						j++
					}
					name2 := s[i+1 : j]
					i = j
					tokens = append(tokens, &formulaToken{kind: 't', text: mapLatexDelimName(name2)})
				}
			default:
				tokens = append(tokens, &formulaToken{kind: 't', text: latexCommand(name)})
			}
		case '^':
			i++
			arg := readArg()
			tokens = append(tokens, &formulaToken{kind: '^', children: arg})
		case '_':
			i++
			arg := readArg()
			tokens = append(tokens, &formulaToken{kind: '_', children: arg})
		case '{':
			content, ni := parseGroup()
			i = ni
			tokens = append(tokens, parseFormula(content)...)
		case ' ', '\t':
			i++
			// keep light spacing between words
			if len(tokens) > 0 {
				tokens = append(tokens, &formulaToken{kind: 't', text: " "})
			}
		default:
			// accumulate plain text until a special char
			j := i
			for j < n && !strings.ContainsRune("\\^_{} ", rune(s[j])) {
				j++
			}
			tokens = append(tokens, &formulaToken{kind: 't', text: s[i:j]})
			i = j
		}
	}
	return tokens
}

func isAlphaByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// latexCommand resolves a LaTeX control name to display text.
func latexCommand(name string) string {
	if v, ok := latexSymbols[name]; ok {
		return v
	}
	// unknown command: render the name verbatim
	return name
}

func mapLatexDelim(d byte) string {
	switch d {
	case '<':
		return "⟨"
	case '>':
		return "⟩"
	default:
		return string(d)
	}
}

func mapLatexDelimName(name string) string {
	switch name {
	case "lbrace":
		return "{"
	case "rbrace":
		return "}"
	case "langle":
		return "⟨"
	case "rangle":
		return "⟩"
	case "lVert", "lvert":
		return "|"
	}
	return latexCommand(name)
}

// formulaMetrics carries measured layout for a token subtree.
type formulaMetrics struct {
	width float64
	// ascent/descent above/below the main baseline, in units
	ascent  float64
	descent float64
}

// measureFormulaLayout measures a token list at the given base font size.
func (gp *GoPdf) measureFormulaLayout(tokens []*formulaToken, fontSize float64) formulaMetrics {
	m := formulaMetrics{}
	scriptSize := fontSize * 0.7
	for _, tok := range tokens {
		switch tok.kind {
		case 't':
			w, _ := gp.MeasureTextWidth(tok.text)
			if gp.curr.FontSize > 0 {
				w = w * fontSize / gp.curr.FontSize
			}
			m.width += w
		case '^', '_':
			sm := gp.measureFormulaLayout(tok.children, scriptSize)
			m.width += sm.width
			if tok.kind == '^' && sm.ascent+fontSize*0.35 > m.ascent {
				m.ascent = sm.ascent + fontSize*0.35
			}
			if tok.kind == '_' && sm.descent+fontSize*0.25 > m.descent {
				m.descent = sm.descent + fontSize*0.25
			}
		case 'f':
			var num, den []*formulaToken
			seenSep := false
			for _, c := range tok.children {
				if c.kind == '|' {
					seenSep = true
					continue
				}
				if seenSep {
					den = append(den, c)
				} else {
					num = append(num, c)
				}
			}
			nm := gp.measureFormulaLayout(num, scriptSize)
			dm := gp.measureFormulaLayout(den, scriptSize)
			w := nm.width
			if dm.width > w {
				w = dm.width
			}
			m.width += w + fontSize*0.15
			asc := fontSize*0.55 + nm.descent
			desc := fontSize*0.55 + dm.ascent
			if asc > m.ascent {
				m.ascent = asc
			}
			if desc > m.descent {
				m.descent = desc
			}
		case 'q':
			sm := gp.measureFormulaLayout(tok.children, fontSize)
			m.width += sm.width + fontSize*0.6
			if sm.ascent+fontSize*0.2 > m.ascent {
				m.ascent = sm.ascent + fontSize*0.2
			}
		case 'b':
			var top, bot []*formulaToken
			seenSep := false
			for _, c := range tok.children {
				if c.kind == '|' {
					seenSep = true
					continue
				}
				if seenSep {
					bot = append(bot, c)
				} else {
					top = append(top, c)
				}
			}
			tm := gp.measureFormulaLayout(top, scriptSize)
			bm := gp.measureFormulaLayout(bot, scriptSize)
			w := tm.width
			if bm.width > w {
				w = bm.width
			}
			m.width += w + fontSize*0.6
			asc := fontSize*0.55 + tm.descent
			desc := fontSize*0.55 + bm.ascent
			if asc > m.ascent {
				m.ascent = asc
			}
			if desc > m.descent {
				m.descent = desc
			}
		}
	}
	return m
}

// MeasureFormula returns the width and height of a formula string at fontSize.
func (gp *GoPdf) MeasureFormula(latex string, fontSize float64) (float64, float64, error) {
	tokens := parseFormula(latex)
	m := gp.measureFormulaLayout(tokens, fontSize)
	return m.width, m.ascent + m.descent + fontSize*0.3, nil
}

// DrawFormula renders a LaTeX-lite formula with the top-left of its bounding
// box at (x, y). Returns the height of the rendered formula.
//
// The current font (SetFont) is used; a font with good Unicode math symbol
// coverage (e.g. STIX, DejaVu, Liberation) is recommended.
func (gp *GoPdf) DrawFormula(x, y float64, latex string, fontSize float64, opt *FormulaOption) (float64, error) {
	if opt == nil {
		opt = &FormulaOption{}
	}
	tokens := parseFormula(latex)
	m := gp.measureFormulaLayout(tokens, fontSize)

	prevX, prevY := gp.GetX(), gp.GetY()
	prevSize := gp.curr.FontSize
	defer func() {
		gp.SetXY(prevX, prevY)
		// script levels leave a smaller font size active — restore it so
		// subsequent drawing continues at the caller's size
		gp.curr.FontSize = prevSize
	}()

	rise := fontSize * 0.3 // top padding
	baseline := y + rise + m.ascent

	// apply options
	if opt.Color != nil {
		gp.SetTextColor(opt.Color[0], opt.Color[1], opt.Color[2])
		if rgb, ok := gp.curr.textColor().(cacheContentTextColorRGB); ok {
			defer gp.SetTextColor(rgb.r, rgb.g, rgb.b)
		}
	}
	if opt.FontFamily != "" {
		style := Regular
		if opt.Italic {
			style = Italic
		}
		if err := gp.SetFontWithStyle(opt.FontFamily, style, fontSize); err != nil {
			return 0, err
		}
	} else if opt.Italic {
		// keep the current selection, add italic style
		gp.curr.FontStyle |= Italic
	}

	err := gp.renderFormulaTokens(tokens, x, baseline, fontSize)
	if err != nil {
		return 0, err
	}
	return m.ascent + m.descent + fontSize*0.3, nil
}

// renderFormulaTokens draws tokens with baseline at (x, baselineY).
func (gp *GoPdf) renderFormulaTokens(tokens []*formulaToken, x, baselineY, fontSize float64) error {
	scriptSize := fontSize * 0.7
	cur := x
	for _, tok := range tokens {
		switch tok.kind {
		case 't':
			// Activate this level's font size (scripts render smaller).
			// The content writer reads curr.FontSize at append time.
			gp.curr.FontSize = fontSize
			w, _ := gp.MeasureTextWidth(tok.text)
			gp.SetXY(cur, baselineY-fontSize)
			rect := &Rect{W: w + 2, H: fontSize * 1.3}
			if err := gp.CellWithOption(rect, tok.text, CellOption{Align: Left | Top}); err != nil {
				return err
			}
			cur += w
		case '^':
			sm := gp.measureFormulaLayout(tok.children, scriptSize)
			if err := gp.renderFormulaTokens(tok.children, cur, baselineY-fontSize*0.35, scriptSize); err != nil {
				return err
			}
			gp.curr.FontSize = fontSize
			cur += sm.width
		case '_':
			sm := gp.measureFormulaLayout(tok.children, scriptSize)
			if err := gp.renderFormulaTokens(tok.children, cur, baselineY+fontSize*0.25, scriptSize); err != nil {
				return err
			}
			gp.curr.FontSize = fontSize
			cur += sm.width
		case 'f':
			var num, den []*formulaToken
			seenSep := false
			for _, c := range tok.children {
				if c.kind == '|' {
					seenSep = true
					continue
				}
				if seenSep {
					den = append(den, c)
				} else {
					num = append(num, c)
				}
			}
			nm := gp.measureFormulaLayout(num, scriptSize)
			dm := gp.measureFormulaLayout(den, scriptSize)
			w := nm.width
			if dm.width > w {
				w = dm.width
			}
			// numerator: its baseline sits above the bar
			numBaseline := baselineY - fontSize*0.55
			if err := gp.renderFormulaTokens(num, cur+(w-nm.width)/2, numBaseline, scriptSize); err != nil {
				return err
			}
			// bar
			barY := baselineY - fontSize*0.12
			gp.SetLineWidth(0.6)
			gp.Line(cur, barY, cur+w, barY)
			// denominator
			denBaseline := baselineY + fontSize*0.55
			if err := gp.renderFormulaTokens(den, cur+(w-dm.width)/2, denBaseline, scriptSize); err != nil {
				return err
			}
			cur += w + fontSize*0.15
		case 'q':
			sm := gp.measureFormulaLayout(tok.children, fontSize)
			// radical glyph
			wR, _ := gp.MeasureTextWidth("√")
			if gp.curr.FontSize > 0 {
				wR = wR * fontSize / gp.curr.FontSize
			}
			gp.SetXY(cur, baselineY-fontSize)
			rect := &Rect{W: wR + 2, H: fontSize * 1.3}
			if err := gp.CellWithOption(rect, "√", CellOption{Align: Left | Top}); err != nil {
				return err
			}
			cur += wR * 0.6
			if err := gp.renderFormulaTokens(tok.children, cur, baselineY, fontSize); err != nil {
				return err
			}
			// overline across the content
			gp.SetLineWidth(0.6)
			gp.Line(cur, baselineY-sm.ascent-fontSize*0.15, cur+sm.width, baselineY-sm.ascent-fontSize*0.15)
			cur += sm.width + wR*0.4
		case 'b':
			var top, bot []*formulaToken
			seenSep := false
			for _, c := range tok.children {
				if c.kind == '|' {
					seenSep = true
					continue
				}
				if seenSep {
					bot = append(bot, c)
				} else {
					top = append(top, c)
				}
			}
			tm := gp.measureFormulaLayout(top, scriptSize)
			bm := gp.measureFormulaLayout(bot, scriptSize)
			w := tm.width
			if bm.width > w {
				w = bm.width
			}
			// parentheses scaled to the stack height
			parenH := fontSize * 1.5
			parenTop := baselineY - fontSize*0.85
			drawBigParen := func(px float64, open bool) {
				gp.SetLineWidth(0.7)
				half := parenH / 2
				midX := px
				if open {
					gp.Line(px+fontSize*0.12, parenTop, midX, parenTop+half)
					gp.Line(midX, parenTop+half, px+fontSize*0.12, parenTop+parenH)
				} else {
					gp.Line(px, parenTop, px+fontSize*0.12, parenTop+half)
					gp.Line(px+fontSize*0.12, parenTop+half, px, parenTop+parenH)
				}
			}
			drawBigParen(cur, true)
			inner := cur + fontSize*0.25
			topBaseline := baselineY - fontSize*0.35
			if err := gp.renderFormulaTokens(top, inner+(w-tm.width)/2, topBaseline, scriptSize); err != nil {
				return err
			}
			botBaseline := baselineY + fontSize*0.45
			if err := gp.renderFormulaTokens(bot, inner+(w-bm.width)/2, botBaseline, scriptSize); err != nil {
				return err
			}
			drawBigParen(inner+w+fontSize*0.05, false)
			gp.curr.FontSize = fontSize
			cur += w + fontSize*0.6
		}
	}
	return nil
}
