package docgen

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var markdownHorizontalRuleDashRe = regexp.MustCompile(`^-{3,}$`)
var markdownHorizontalRuleStarRe = regexp.MustCompile(`^\*{3,}$`)
var markdownOrderedListRe = regexp.MustCompile(`^\d+\.\s`)
var markdownImageRe = regexp.MustCompile(`^!\[(.*?)\]\((.*?)\)$`)
var markdownBareURLRe = regexp.MustCompile(`https?://[^\s<>\]（）)]+`)

// colorfulH2Colors are inline colors InsertHTMLBox paints. They rotate by
// section so a paper interpretation is not one near-black heading color.
var colorfulH2Colors = []string{
	"#1d4ed8",
	"#0f766e",
	"#7c3aed",
	"#c2410c",
	"#15803d",
	"#be123c",
	"#b45309",
	"#0369a1",
}
var inlineMathSegmentRe = regexp.MustCompile(`\$(.+?)\$`)
var inlineDisplayMathRe = regexp.MustCompile(`\$\$([^$\n]+)\$\$`)
var inlineCodeRe = regexp.MustCompile("`([^`]+)`")
var inlineStarBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
var inlineUnderscoreBoldRe = regexp.MustCompile(`__(.+?)__`)
var inlineItalicRe = regexp.MustCompile(`\*(.+?)\*`)
var inlineLinkRe = regexp.MustCompile(`(^|[^!])\[([^\]\n]+)\]\(([^)\s]+)\)`)
var sanitizeFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\s]+`)

// newLongestReplacer lists longer commands first. strings.NewReplacer keeps the
// first pair that matches at a position, so \le listed before \leftarrow turns
// the arrow into ≤ftarrow, and \to listed before \top turns it into →p.
func newLongestReplacer(pairs ...string) *strings.Replacer {
	type pair struct{ old, new string }
	items := make([]pair, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, pair{pairs[i], pairs[i+1]})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return len(items[i].old) > len(items[j].old)
	})
	ordered := make([]string, 0, len(pairs))
	for _, item := range items {
		ordered = append(ordered, item.old, item.new)
	}
	return strings.NewReplacer(ordered...)
}

var latexSymbolReplacer = newLongestReplacer(
	`\alpha`, "α",
	`\beta`, "β",
	`\gamma`, "γ",
	`\delta`, "δ",
	`\theta`, "θ",
	`\lambda`, "λ",
	`\mu`, "μ",
	`\pi`, "π",
	`\sigma`, "σ",
	`\phi`, "φ",
	`\omega`, "ω",
	`\epsilon`, "ε",
	`\varepsilon`, "ϵ",
	`\rho`, "ρ",
	`\eta`, "η",
	`\xi`, "ξ",
	`\zeta`, "ζ",
	`\psi`, "ψ",
	`\nu`, "ν",
	`\kappa`, "κ",
	`\tau`, "τ",
	`\Gamma`, "Γ",
	`\Delta`, "Δ",
	`\Theta`, "Θ",
	`\Lambda`, "Λ",
	`\Pi`, "Π",
	`\Sigma`, "Σ",
	`\Phi`, "Φ",
	`\Omega`, "Ω",
	`\cdots`, "⋯",
	`\times`, "×",
	`\cdot`, "·",
	`\oplus`, "⊕",
	`\otimes`, "⊗",
	`\ominus`, "⊖",
	`\pm`, "±",
	`\mp`, "∓",
	`\leq`, "≤",
	`\le`, "≤",
	`\geq`, "≥",
	`\ge`, "≥",
	`\leqslant`, "⩽",
	`\geqslant`, "⩾",
	`\nleq`, "≰",
	`\ngeq`, "≱",
	`\nleqslant`, "⩽̸",
	`\ngeqslant`, "⩾̸",
	`\ll`, "≪",
	`\gg`, "≫",
	`\lesssim`, "≲",
	`\gtrsim`, "≳",
	`\lessgtr`, "≶",
	`\gtrless`, "≷",
	`\lessapprox`, "⪅",
	`\gtrapprox`, "⪆",
	`\circeq`, "≗",
	`\eqcirc`, "≖",
	`\triangleq`, "≜",
	`\doteq`, "≐",
	`\fallingdotseq`, "≒",
	`\risingdotseq`, "≓",
	`\eqsim`, "≂",
	`\bumpeq`, "≏",
	`\Bumpeq`, "≎",
	`\neq`, "≠",
	`\approx`, "≈",
	`\simeq`, "≃",
	`\cong`, "≅",
	`\asymp`, "≍",
	`\backsim`, "∽",
	`\backsimeq`, "⋍",
	`\equiv`, "≡",
	`\propto`, "∝",
	`\sim`, "∼",
	`\perp`, "⊥",
	`\iff`, "⇔",
	`\implies`, "⇒",
	`\Longleftrightarrow`, "⇔",
	`\Longrightarrow`, "⇒",
	`\Rightarrow`, "⇒",
	`\Leftarrow`, "⇐",
	`\mapsto`, "↦",
	`\hookrightarrow`, "↪",
	`\hookleftarrow`, "↩",
	`\twoheadrightarrow`, "↠",
	`\twoheadleftarrow`, "↞",
	`\leftharpoonup`, "↼",
	`\rightharpoonup`, "⇀",
	`\leftharpoondown`, "↽",
	`\rightharpoondown`, "⇁",
	`\leftrightharpoons`, "⇋",
	`\rightleftharpoons`, "⇌",
	`\curvearrowleft`, "↶",
	`\curvearrowright`, "↷",
	`\circlearrowleft`, "↺",
	`\circlearrowright`, "↻",
	`\rightsquigarrow`, "⇝",
	`\leadsto`, "⇝",
	`\rightarrow`, "→",
	`\leftarrow`, "←",
	`\leftrightarrow`, "↔",
	`\upuparrows`, "⇈",
	`\downdownarrows`, "⇊",
	`\uparrow`, "↑",
	`\downarrow`, "↓",
	`\updownarrow`, "↕",
	`\nearrow`, "↗",
	`\searrow`, "↘",
	`\swarrow`, "↙",
	`\nwarrow`, "↖",
	`\gets`, "←",
	`\to`, "→",
	`\infty`, "∞",
	`\degree`, "°",
	`\partial`, "∂",
	`\nabla`, "∇",
	`\forall`, "∀",
	`\exists`, "∃",
	`\owns`, "∋",
	`\notin`, "∉",
	`\ni`, "∋",
	`\subseteq`, "⊆",
	`\supseteq`, "⊇",
	`\subsetneqq`, "⫋",
	`\subsetneq`, "⊊",
	`\supsetneqq`, "⫌",
	`\supsetneq`, "⊋",
	`\prec`, "≺",
	`\succ`, "≻",
	`\preceq`, "≼",
	`\succeq`, "≽",
	`\vartriangleleft`, "⊲",
	`\vartriangleright`, "⊳",
	`\blacktriangleleft`, "◂",
	`\blacktriangleright`, "▸",
	`\triangleleft`, "◃",
	`\triangleright`, "▹",
	`\trianglelefteq`, "⊴",
	`\trianglerighteq`, "⊵",
	`\sqsubseteq`, "⊑",
	`\sqsupseteq`, "⊒",
	`\sqsubset`, "⊏",
	`\sqsupset`, "⊐",
	`\Subset`, "⋐",
	`\Supset`, "⋑",
	`\eqslantless`, "⪕",
	`\eqslantgtr`, "⪖",
	`\precsim`, "≾",
	`\succsim`, "≿",
	`\curlyeqprec`, "⋞",
	`\curlyeqsucc`, "⋟",
	`\nprec`, "⊀",
	`\nsucc`, "⊁",
	`\npreceq`, "⪯̸",
	`\nsucceq`, "⪰̸",
	`\precapprox`, "⪷",
	`\succapprox`, "⪸",
	`\models`, "⊨",
	`\vdash`, "⊢",
	`\dashv`, "⊣",
	`\top`, "⊤",
	`\bot`, "⊥",
	`\setminus`, "∖",
	`\cup`, "∪",
	`\cap`, "∩",
	`\Join`, "⋈",
	`\bowtie`, "⋈",
	`\curlywedge`, "⋏",
	`\curlyvee`, "⋎",
	`\Cap`, "⋒",
	`\Cup`, "⋓",
	`\sqcap`, "⊓",
	`\sqcup`, "⊔",
	`\barwedge`, "⊼",
	`\intercal`, "⊺",
	`\divideontimes`, "⋇",
	`\ltimes`, "⋉",
	`\rtimes`, "⋊",
	`\leftthreetimes`, "⋋",
	`\rightthreetimes`, "⋌",
	`\doublebarwedge`, "⌆",
	`\veebar`, "⊻",
	`\circledast`, "⊛",
	`\circledcirc`, "⊚",
	`\circleddash`, "⊝",
	`\dotplus`, "∔",
	`\ldots`, "…",
	`\vdots`, "⋮",
	`\ddots`, "⋱",
	`\langle`, "⟨",
	`\rangle`, "⟩",
	`\lfloor`, "⌊",
	`\rfloor`, "⌋",
	`\lceil`, "⌈",
	`\rceil`, "⌉",
	`\shortmid`, "∣",
	`\mid`, "∣",
	`\nmid`, "∤",
	`\nparallel`, "∦",
	`\parallel`, "∥",
	`\angle`, "∠",
	`\triangle`, "△",
	`\smallsmile`, "⌣",
	`\smallfrown`, "⌢",
	`\bullet`, "•",
	`\circ`, "∘",
	`\star`, "⋆",
	`\dagger`, "†",
	`\ddagger`, "‡",
	`\flat`, "♭",
	`\natural`, "♮",
	`\sharp`, "♯",
	`\multimap`, "⊸",
	`\pitchfork`, "⋔",
	`\clubsuit`, "♣",
	`\diamondsuit`, "♢",
	`\heartsuit`, "♡",
	`\spadesuit`, "♠",
	`\coprod`, "∐",
	`\bigcup`, "⋃",
	`\bigcap`, "⋂",
	`\bigoplus`, "⨁",
	`\bigotimes`, "⨂",
	`\bigodot`, "⨀",
	`\sum`, "∑",
	`\prod`, "∏",
	`\bigvee`, "⋁",
	`\bigwedge`, "⋀",
	`\bigsqcup`, "⨆",
	`\iiint`, "∭",
	`\iint`, "∬",
	`\oint`, "∮",
	`\int`, "∫",
	`\sqrt`, "√",
	`\aleph`, "ℵ",
	`\hbar`, "ℏ",
	`\ell`, "ℓ",
	`\wp`, "℘",
	`\in`, "∈",
	`\Re`, "Re",
	`\Im`, "Im",
	`\emptyset`, "∅",
	`\varnothing`, "∅",
	`\subset`, "⊂",
	`\supset`, "⊃",
	`\land`, "∧",
	`\lor`, "∨",
	`\neg`, "¬",
	`\because`, "∵",
	`\therefore`, "∴",
)

var latexFunctionReplacer = strings.NewReplacer(
	`\sin`, "sin",
	`\cos`, "cos",
	`\tan`, "tan",
	`\log`, "log",
	`\ln`, "ln",
	`\exp`, "exp",
	`\min`, "min",
	`\max`, "max",
	`\argmin`, "argmin",
	`\argmax`, "argmax",
)

func markdownToHTML(md string) string {
	return markdownToHTMLWithScratch(nil, md, false)
}

// markdownToHTMLWithScratch renders Markdown for one render. scratch carries
// that render's home for decoded data-URL images, and may be nil for callers
// that have none (in-package HTML assertions), where a data-URL image then
// resolves to nothing rather than being written somewhere unmanaged.
func markdownToHTMLWithScratch(scratch *dataURLScratch, md string, colorful bool) string {
	return markdownToHTMLStyled(scratch, md, colorful, Style{})
}

func markdownToHTMLStyled(scratch *dataURLScratch, md string, colorful bool, style Style) string {
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	listType := ""
	h2 := 0
	sectionBody := ""
	closeList := func() {
		switch listType {
		case "ul":
			sb.WriteString("</ul>")
		case "ol":
			sb.WriteString("</ol>")
		default:
			return
		}
		// The marker is painted from the state that reaches <ul>/<ol>.
		// Those tags ignore their own style, so the ink has to sit on a parent.
		if style.ID != "" {
			sb.WriteString("</span>")
		}
		listType = ""
	}
	ensureList := func(nextType string) {
		if listType == nextType {
			return
		}
		if listType != "" {
			closeList()
		}
		if style.ID != "" {
			sb.WriteString(`<span style="color:` + cssHex(style.Colors.Ink) + `">`)
		}
		sb.WriteString("<" + nextType + ">")
		listType = nextType
	}

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			if listType != "" {
				closeList()
			}
			sb.WriteString("<br/>")
			continue
		}
		if markdownHorizontalRuleDashRe.MatchString(trimmed) || markdownHorizontalRuleStarRe.MatchString(trimmed) {
			if listType != "" {
				closeList()
			}
			sb.WriteString(styledRuleHTML(style))
			continue
		}
		if prefix, ok := markdownHeadingPrefix(trimmed); ok {
			if listType != "" {
				closeList()
			}
			size, color := markdownHeadingPresentation(prefix, colorful, h2)
			align := ""
			mark := ""
			if styledSize, styledColor, styledAlign := style.headingLook(prefix); styledSize != "" {
				size, color, align = styledSize, styledColor, styledAlign
				mark = headingMark(style, prefix)
			}
			body := markdownHeadingBody(trimmed, prefix)
			if prefix == "## " {
				if colorful {
					sectionBody = paperSectionBodyColor(strings.TrimSpace(body))
				}
				h2++
			}
			alignAttr := ""
			if align == "center" {
				alignAttr = "text-align:center; "
			}
			sb.WriteString(fmt.Sprintf(`<p style="%sfont-size:%s; color:%s"><b>%s%s</b></p>`, alignAttr, size, color, mark, renderInlineHTMLMode(body, colorful, style)))
			continue
		}
		if imageHTML, ok := markdownImageToHTML(scratch, trimmed, colorful, style); ok {
			if listType != "" {
				closeList()
			}
			sb.WriteString(imageHTML)
			continue
		}
		if tableHTML, nextIdx, ok := markdownTableToHTML(lines, i, style, colorful); ok {
			if listType != "" {
				closeList()
			}
			sb.WriteString(tableHTML)
			i = nextIdx - 1
			continue
		}
		if text, nextIdx, ok := markdownDisplayMathToHTML(lines, i, colorful); ok {
			if listType != "" {
				closeList()
			}
			switch {
			case colorful:
				sb.WriteString(fmt.Sprintf(`<p style="text-align:center; font-size:12pt; color:#6d28d9">%s</p>`, text))
			case style.ID != "":
				sb.WriteString(fmt.Sprintf(`<p style="text-align:center; color:%s">%s</p>`, cssHex(style.Colors.Ink), text))
			default:
				sb.WriteString(fmt.Sprintf(`<p style="text-align:center">%s</p>`, text))
			}
			i = nextIdx
			continue
		}
		if marker, ok := markdownFenceMarker(trimmed); ok {
			if listType != "" {
				closeList()
			}
			// Fenced code block — ``` or ~~~, closed by a matching fence.
			var codeLines []string
			i++ // skip the opening fence line
			for i < len(lines) {
				if isClosingFence(lines[i], marker) {
					break
				}
				codeLines = append(codeLines, lines[i])
				i++
			}
			// i now points to the closing ``` (or past end); outer loop will i++
			// Render each line with HTML escaping; convert spaces to &nbsp;
			// to preserve indentation since HTML collapses whitespace
			var codeHTML strings.Builder
			for ci, cl := range codeLines {
				if ci > 0 {
					codeHTML.WriteString("<br/>")
				}
				escaped := escapeHTML(cl)
				escaped = strings.ReplaceAll(escaped, " ", "&nbsp;")
				escaped = strings.ReplaceAll(escaped, "\t", "&nbsp;&nbsp;&nbsp;&nbsp;")
				codeHTML.WriteString(escaped)
			}
			codeColor := "#1f2937"
			if style.ID != "" {
				codeColor = cssHex(style.Colors.Ink)
			}
			sb.WriteString(fmt.Sprintf(`<p style="font-size:8.5pt; font-family:monospace; background-color:#f3f4f6; color:%s; padding:8px; border-radius:4px">%s</p>`, codeColor, codeHTML.String()))
			continue
		}
		if strings.HasPrefix(trimmed, ">") {
			if listType != "" {
				closeList()
			}
			// Collect consecutive blockquote lines
			var bqLines []string
			for i < len(lines) {
				bqTrimmed := strings.TrimSpace(lines[i])
				if strings.HasPrefix(bqTrimmed, "> ") {
					bqLines = append(bqLines, strings.TrimPrefix(bqTrimmed, "> "))
					i++
				} else if bqTrimmed == ">" {
					bqLines = append(bqLines, "")
					i++
				} else if strings.HasPrefix(bqTrimmed, ">") {
					bqLines = append(bqLines, strings.TrimPrefix(bqTrimmed, ">"))
					i++
				} else {
					break
				}
			}
			i-- // compensate for the outer loop's i++
			// Render each non-empty line separately, preserving line breaks
			var bqHTML strings.Builder
			for idx, line := range bqLines {
				if line == "" {
					if idx > 0 && idx < len(bqLines)-1 {
						bqHTML.WriteString("<br/>")
					}
					continue
				}
				if bqHTML.Len() > 0 {
					bqHTML.WriteString("<br/>")
				}
				bqHTML.WriteString(renderInlineHTMLMode(line, colorful, style))
			}
			quoteColor, barColor := "#4b5563", "#d1d5db"
			if style.ID != "" {
				quoteColor = cssHex(style.Colors.Quote)
				barColor = cssHex(style.Colors.Accent)
				sb.WriteString(fmt.Sprintf(`<p style="color:%s"><span style="color:%s">%s </span>%s</p>`, quoteColor, barColor, styleMarkGlyph, bqHTML.String()))
				continue
			}
			sb.WriteString(fmt.Sprintf(`<p style="border-left:3px solid %s; padding-left:10px; color:%s; margin-left:6px">%s</p>`, barColor, quoteColor, bqHTML.String()))
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			ensureList("ul")
			text := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			sb.WriteString(fmt.Sprintf("<li%s>%s</li>", paperBodyStyle(colorful, sectionBody, style), renderInlineHTMLMode(text, colorful, style)))
			continue
		}
		if markdownOrderedListRe.MatchString(trimmed) {
			ensureList("ol")
			text := trimmed
			if idx := strings.Index(text, ". "); idx > 0 {
				text = text[idx+2:]
			}
			sb.WriteString(fmt.Sprintf("<li%s>%s</li>", paperBodyStyle(colorful, sectionBody, style), renderInlineHTMLMode(text, colorful, style)))
			continue
		}
		if listType != "" {
			closeList()
		}
		if colorful && paperCaptionLine(trimmed) {
			caption := "#0f766e"
			if style.ID != "" {
				caption = cssHex(style.Colors.Meta)
			}
			sb.WriteString(fmt.Sprintf(`<p style="font-size:10pt; color:%s"><i>%s</i></p>`, caption, renderInlineHTMLMode(trimmed, true, style)))
			continue
		}
		if colorful {
			if mixed, ok := renderColorfulFormulaLine(trimmed, sectionBody, style); ok {
				sb.WriteString(mixed)
				continue
			}
		}
		sb.WriteString(fmt.Sprintf("<p%s>%s</p>", paperBodyStyle(colorful, sectionBody, style), renderInlineHTMLMode(trimmed, colorful, style)))
	}
	if listType != "" {
		closeList()
	}
	return breakInlineTails(sb.String())
}

// inlineTailBreakRun is the spaceless run length that starts cluster wrapping.
// A run of this many runes or more is split. GoPDF2 wraps on spaces only, and
// a run that does not fit the rest of the line moves to the next line as a
// whole. A colon sitting just after </b> or </span> is the first character of
// that run, so it starts the next line whenever the explanation is longer
// than the space left beside the label. A long run is broken into clusters so
// the sentence keeps filling the line. A shorter run only detaches that
// leading colon when it is glued to the next character. A colon already
// followed by a space is its own word, and wrapping it would drop the space.
const inlineTailBreakRun = 8

// breakInlineTails splits a long spaceless tail that follows inline markup so
// the PDF can wrap it one cluster at a time. Table cells flatten tags into one
// string and would show a space between spans, so they are left alone. Link
// text is left alone too: the annotation is one rectangle from the start of
// the <a> to the cursor after its children, and a wrap inside the label makes
// that width land on the wrong line.
func breakInlineTails(html string) string {
	var b strings.Builder
	b.Grow(len(html))
	pending := false
	cell := 0
	anchor := 0
	for i := 0; i < len(html); {
		if html[i] != '<' {
			j := strings.IndexByte(html[i:], '<')
			if j < 0 {
				j = len(html) - i
			}
			text := html[i : i+j]
			if pending && cell == 0 && anchor == 0 && strings.TrimSpace(text) != "" {
				b.WriteString(spanBreakLongRun(text))
				pending = false
			} else {
				b.WriteString(text)
				if strings.TrimSpace(text) != "" {
					pending = false
				}
			}
			i += j
			continue
		}
		j := strings.IndexByte(html[i:], '>')
		if j < 0 {
			b.WriteString(html[i:])
			break
		}
		tag := html[i : i+j+1]
		name, closing := htmlTagName(tag)
		switch name {
		case "td", "th":
			if closing {
				if cell > 0 {
					cell--
				}
			} else {
				cell++
			}
		case "a":
			if closing {
				if anchor > 0 {
					anchor--
				}
			} else {
				anchor++
			}
		}
		if closing && isInlineTailTag(name) {
			pending = true
		} else {
			pending = false
		}
		b.WriteString(tag)
		i += j + 1
	}
	return b.String()
}

func htmlTagName(tag string) (name string, closing bool) {
	s := strings.TrimSpace(tag)
	if len(s) < 3 || s[0] != '<' {
		return "", false
	}
	s = s[1:]
	if s[0] == '/' {
		closing = true
		s = s[1:]
	}
	s = strings.TrimLeft(s, " \t\r\n")
	end := 0
	for end < len(s) {
		c := s[end]
		if c == ' ' || c == '>' || c == '/' || c == '\t' || c == '\r' || c == '\n' {
			break
		}
		end++
	}
	return strings.ToLower(s[:end]), closing
}

func isInlineTailTag(name string) bool {
	switch name {
	case "b", "strong", "i", "em", "span", "a", "sub", "sup", "u", "s", "font", "strike", "del", "ins":
		return true
	default:
		return false
	}
}

func spanBreakLongRun(text string) string {
	if hasLongUnbrokenRun(text, inlineTailBreakRun) {
		groups := packInlineAtoms(glueInlinePunct(tokenizeInlineRun(text)))
		if len(groups) <= 1 {
			return text
		}
		var b strings.Builder
		b.Grow(len(text) + len(groups)*13)
		for _, g := range groups {
			if g == "" {
				continue
			}
			b.WriteString("<span>")
			b.WriteString(g)
			b.WriteString("</span>")
		}
		return b.String()
	}
	if !leadingClosingPunct(text) {
		return text
	}
	if peeled, ok := peelLeadingClosingPunct(glueInlinePunct(tokenizeInlineRun(text))); ok {
		return peeled
	}
	return text
}

// leadingClosingPunct reports a tail whose first visible marks cannot start a
// line and are glued to the following text, such as "：说明". A halfwidth
// colon that begins a token ("https://" or ":2510") is not that mark. A mark
// followed by whitespace is already its own word: GoPDF2 splits on spaces,
// and putting the mark in its own span would leave the space at the edge of
// the next text node, where it is trimmed away.
func leadingClosingPunct(s string) bool {
	i := 0
	for i < len(s) {
		if strings.HasPrefix(s[i:], "&nbsp;") {
			i += len("&nbsp;")
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		break
	}
	if i >= len(s) {
		return false
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r == ':' {
		next, _ := utf8.DecodeRuneInString(s[i+size:])
		if isLatinWordRune(next) {
			return false
		}
	} else if !isClosingPunctRune(r) {
		return false
	}
	i += size
	for i < len(s) {
		if strings.HasPrefix(s[i:], "&nbsp;") {
			return false
		}
		r, size = utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			return false
		}
		if r == ':' {
			next, _ := utf8.DecodeRuneInString(s[i+size:])
			if isLatinWordRune(next) {
				return false
			}
		}
		if !isClosingPunctRune(r) {
			return true
		}
		i += size
	}
	return false
}

// peelLeadingClosingPunct wraps a leading colon (or a run of closing marks)
// in its own span and leaves the short remainder as one word.
func peelLeadingClosingPunct(atoms []inlineAtom) (string, bool) {
	i := 0
	var lead strings.Builder
	for i < len(atoms) && atoms[i].space {
		lead.WriteString(atoms[i].text)
		i++
	}
	if i >= len(atoms) || !isBareClosingPunct(atoms[i].text) {
		return "", false
	}
	var rest strings.Builder
	for _, a := range atoms[i+1:] {
		rest.WriteString(a.text)
	}
	if rest.Len() == 0 {
		return "", false
	}
	var b strings.Builder
	b.Grow(lead.Len() + rest.Len() + len(atoms[i].text) + 13)
	b.WriteString(lead.String())
	b.WriteString("<span>")
	b.WriteString(atoms[i].text)
	b.WriteString("</span>")
	b.WriteString(rest.String())
	return b.String(), true
}

func isBareClosingPunct(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if !isClosingPunctRune(r) {
			return false
		}
	}
	return true
}

func hasLongUnbrokenRun(s string, n int) bool {
	count := 0
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "&nbsp;") {
			count = 0
			i += len("&nbsp;")
			continue
		}
		if s[i] == '&' {
			if semi := strings.IndexByte(s[i:], ';'); semi > 0 && semi <= 8 {
				count++
				if count >= n {
					return true
				}
				i += semi + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if isInlineSpace(r) {
			count = 0
		} else {
			count++
			if count >= n {
				return true
			}
		}
		i += size
	}
	return false
}

type inlineAtom struct {
	text  string
	space bool
	open  bool
}

func tokenizeInlineRun(s string) []inlineAtom {
	var atoms []inlineAtom
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "&nbsp;") {
			j := i + len("&nbsp;")
			for j < len(s) && (isInlineSpacePrefix(s[j:]) || strings.HasPrefix(s[j:], "&nbsp;")) {
				if strings.HasPrefix(s[j:], "&nbsp;") {
					j += len("&nbsp;")
					continue
				}
				_, size := utf8.DecodeRuneInString(s[j:])
				j += size
			}
			atoms = append(atoms, inlineAtom{text: s[i:j], space: true})
			i = j
			continue
		}
		if s[i] == '&' {
			if semi := strings.IndexByte(s[i:], ';'); semi > 0 && semi <= 8 {
				atoms = append(atoms, inlineAtom{text: s[i : i+semi+1]})
				i += semi + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if isInlineSpace(r) {
			j := i + size
			for j < len(s) {
				if strings.HasPrefix(s[j:], "&nbsp;") {
					break
				}
				rr, sz := utf8.DecodeRuneInString(s[j:])
				if !isInlineSpace(rr) {
					break
				}
				j += sz
			}
			atoms = append(atoms, inlineAtom{text: s[i:j], space: true})
			i = j
			continue
		}
		if isLatinWordRune(r) {
			j := i + size
			for j < len(s) {
				if s[j] == '&' {
					break
				}
				rr, sz := utf8.DecodeRuneInString(s[j:])
				if !isLatinWordRune(rr) {
					break
				}
				j += sz
			}
			atoms = append(atoms, inlineAtom{text: s[i:j]})
			i = j
			continue
		}
		atoms = append(atoms, inlineAtom{text: s[i : i+size]})
		i += size
	}
	return atoms
}

func isInlineSpacePrefix(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return isInlineSpace(r)
}

func isInlineSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u00a0'
}

func isLatinWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-+/#@%':", r)
}

func isOpeningPunctToken(token string) bool {
	r, size := utf8.DecodeRuneInString(token)
	return size == len(token) && strings.ContainsRune("（(【［〔〈《「『“‘", r)
}

func isClosingPunctToken(token string) bool {
	r, size := utf8.DecodeRuneInString(token)
	return size == len(token) && isClosingPunctRune(r)
}

func isClosingPunctRune(r rune) bool {
	return strings.ContainsRune("：:，,。．、；;！!？?）)】］〕〉》」』”’…·～~％%—–－", r)
}

func glueInlinePunct(atoms []inlineAtom) []inlineAtom {
	out := make([]inlineAtom, 0, len(atoms))
	for _, a := range atoms {
		if a.space {
			out = append(out, a)
			continue
		}
		if isOpeningPunctToken(a.text) {
			a.open = true
			out = append(out, a)
			continue
		}
		if len(out) > 0 && out[len(out)-1].open && !out[len(out)-1].space {
			out[len(out)-1].text += a.text
			out[len(out)-1].open = false
			continue
		}
		if isClosingPunctToken(a.text) && len(out) > 0 && !out[len(out)-1].space {
			out[len(out)-1].text += a.text
			continue
		}
		out = append(out, a)
	}
	return out
}

func packInlineAtoms(atoms []inlineAtom) []string {
	var groups []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		groups = append(groups, cur.String())
		cur.Reset()
	}
	for i, a := range atoms {
		if a.space {
			cur.WriteString(a.text)
			continue
		}
		cur.WriteString(a.text)
		if i+1 < len(atoms) && atoms[i+1].space {
			continue
		}
		flush()
	}
	flush()
	return groups
}

func markdownImageToHTML(scratch *dataURLScratch, line string, colorful bool, style Style) (string, bool) {
	matches := markdownImageRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return "", false
	}
	alt := strings.TrimSpace(matches[1])
	rawPath := strings.TrimSpace(matches[2])
	imagePath, ok := resolveMarkdownImagePath(scratch, rawPath)
	captionColor := "#666"
	if style.ID != "" {
		captionColor = cssHex(style.Colors.Meta)
	} else if colorful {
		captionColor = "#0f766e"
	}
	if ok {
		caption := ""
		if alt != "" {
			caption = fmt.Sprintf(`<p style="font-size:9pt; color:%s"><i>%s</i></p>`, captionColor, inlineMD(escapeHTML(alt)))
		}
		width, height := fittedMarkdownImageSize(imagePath)
		return fmt.Sprintf(`<p><img src="%s" width="%d" height="%d"/></p>%s`, escapeHTMLAttr(imagePath), width, height, caption), true
	}
	label := fmt.Sprintf("<p%s><b>图片：</b>%s</p>", paperBodyStyle(false, "", style), inlineMD(escapeHTML(fallbackText(alt, "未命名图片"))))
	if isRemoteURL(rawPath) {
		return label + fmt.Sprintf(`<p style="font-size:9pt; color:%s"><i>暂不支持远程图片：%s</i></p>`, captionColor, inlineMD(escapeHTML(rawPath))), true
	}
	return label + fmt.Sprintf(`<p style="font-size:9pt; color:%s"><i>图片未找到：%s</i></p>`, captionColor, inlineMD(escapeHTML(rawPath))), true
}

// fittedMarkdownImageSize keeps a chart inside the area above the footer on
// both A4 and B5. Width stays at most 480pt. Height stays at most 600pt.
//
// Results are cached per render: pagination measures each candidate split,
// so a 12-image document used to decode the same files over 100 times. File
// identity is part of the cache key (see imageSizeCacheKey), so a file that
// changes between renders is re-read instead of served stale.
func fittedMarkdownImageSize(path string) (int, int) {
	cached := decodeMarkdownImageSizeWithCache(path, globalMarkdownImageSizeCache)
	return cached.w, cached.h
}

// markdownImageSize is the fitted size of one image, in points.
type markdownImageSize struct {
	w, h int
}

// globalMarkdownImageSizeCache is shared by the render helpers that call
// fittedMarkdownImageSize directly without a per-render map. It is keyed by
// file identity (see imageSizeCacheKey) so a replaced file invalidates itself.
var globalMarkdownImageSizeCache = newMarkdownImageSizeCache()

// decodeMarkdownImageSizeWithCache reads image dimensions through the given
// cache, falling back to the maximum chart size when the file cannot be read
// or decoded.
func decodeMarkdownImageSizeWithCache(path string, cache *markdownImageSizeCache) markdownImageSize {
	const maxW = 480.0
	const maxH = 600.0
	fallback := markdownImageSize{w: int(maxW), h: int(maxH)}

	key, ok := imageSizeCacheKey(path)
	if !ok {
		return fallback
	}
	if got, ok := cache.get(key); ok {
		return got
	}

	got := fallback
	if cfg, ok := decodeImageSize(path); ok {
		w := maxW
		h := float64(cfg.height) * w / float64(cfg.width)
		if h > maxH {
			h = maxH
			w = float64(cfg.width) * h / float64(cfg.height)
		}
		if w < 1 {
			w = 1
		}
		if h < 1 {
			h = 1
		}
		got = markdownImageSize{w: int(w + 0.5), h: int(h + 0.5)}
	}
	cache.put(key, got)
	return got
}

// imageSize is the natural pixel size of an image file.
type imageSize struct {
	width, height int
}

func decodeImageSize(path string) (imageSize, bool) {
	f, err := os.Open(path)
	if err != nil {
		return imageSize{}, false
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return imageSize{}, false
	}
	return imageSize{width: cfg.Width, height: cfg.Height}, true
}

// resolveMarkdownImagePath turns one Markdown target into a file the renderer
// can open. scratch supplies the render-scoped directory for images that
// arrive as bytes; without it, only real filesystem paths can resolve.
func resolveMarkdownImagePath(scratch *dataURLScratch, rawPath string) (string, bool) {
	path := strings.TrimSpace(rawPath)
	// CommonMark wraps a destination that contains spaces in <...>.
	// The capture includes those brackets; the file does not.
	if len(path) >= 2 && path[0] == '<' && path[len(path)-1] == '>' {
		path = strings.TrimSpace(path[1 : len(path)-1])
	}
	if path == "" || isRemoteURL(path) {
		return "", false
	}
	// A data URL carries the image itself. Decoding it to a file lets every
	// later stage — measuring, embedding — stay exactly as it is for a path.
	if isDataURLImage(path) {
		if scratch == nil {
			return "", false
		}
		return scratch.resolve(path)
	}
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		path = strings.TrimPrefix(path, "file://")
		path = strings.TrimPrefix(path, "/")
	}
	path = filepath.FromSlash(path)
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
		return "", false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	absPath := filepath.Join(cwd, path)
	if _, err := os.Stat(absPath); err == nil {
		return absPath, true
	}
	return "", false
}

func isRemoteURL(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func markdownTableToHTML(lines []string, start int, style Style, colorful bool) (string, int, bool) {
	if start+1 >= len(lines) {
		return "", start, false
	}
	headerLine := strings.TrimSpace(lines[start])
	separatorLine := strings.TrimSpace(lines[start+1])
	if !looksLikeMarkdownTableRow(headerLine) || !isMarkdownTableSeparator(separatorLine) {
		return "", start, false
	}
	headers := parseMarkdownTableRow(headerLine)
	if len(headers) == 0 {
		return "", start, false
	}
	var sb strings.Builder
	sb.WriteString(`<table><thead><tr>`)
	for _, header := range headers {
		if style.ID != "" {
			sb.WriteString(fmt.Sprintf(`<td style="color:%s; text-align:center"><b>%s</b></td>`, cssHex(style.Colors.Navy), tableCellHTML(header, colorful)))
			continue
		}
		sb.WriteString(fmt.Sprintf(`<th>%s</th>`, tableCellHTML(header, colorful)))
	}
	sb.WriteString(`</tr></thead><tbody>`)
	next := start + 2
	rowCount := 0
	for next < len(lines) {
		rowLine := strings.TrimSpace(lines[next])
		if !looksLikeMarkdownTableRow(rowLine) {
			break
		}
		cells := parseMarkdownTableRow(rowLine)
		if len(cells) == 0 {
			break
		}
		rowCount++
		sb.WriteString(`<tr>`)
		for i := range headers {
			value := ""
			if i < len(cells) {
				value = cells[i]
			}
			if style.ID != "" {
				sb.WriteString(fmt.Sprintf(`<td style="color:%s">%s</td>`, cssHex(style.Colors.Ink), tableCellHTML(value, colorful)))
			} else {
				sb.WriteString(fmt.Sprintf(`<td>%s</td>`, tableCellHTML(value, colorful)))
			}
		}
		sb.WriteString(`</tr>`)
		next++
	}
	if rowCount == 0 {
		return "", start, false
	}
	sb.WriteString(`</tbody></table>`)
	return sb.String(), next, true
}

func looksLikeMarkdownTableRow(line string) bool {
	return strings.Count(strings.TrimSpace(line), "|") >= 2
}

func isMarkdownTableSeparator(line string) bool {
	cells := parseMarkdownTableRow(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" {
			return false
		}
		for _, r := range cell {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

func parseMarkdownTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	parts := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func isMarkdownTableBlock(block string) bool {
	lines := strings.Split(strings.TrimSpace(block), "\n")
	if len(lines) < 3 {
		return false
	}
	if !looksLikeMarkdownTableRow(strings.TrimSpace(lines[0])) || !isMarkdownTableSeparator(strings.TrimSpace(lines[1])) {
		return false
	}
	for _, line := range lines[2:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !looksLikeMarkdownTableRow(line) {
			return false
		}
	}
	return true
}

// MarkdownDisplayMathSpan is the line range one display-math block covers.
// A single-line "$$...$$" covers itself. A closed block runs through the
// line that ends in "$$". An opener with no closer covers only that opener:
// the lines after it are ordinary text. Callers that decide whether a fence
// marker is real have to use this, because a "```" inside the block is math.
func MarkdownDisplayMathSpan(lines []string, idx int) (end int, ok bool) {
	if idx < 0 || idx >= len(lines) {
		return idx, false
	}
	trimmed := strings.TrimSpace(lines[idx])
	if !strings.HasPrefix(trimmed, "$$") {
		return idx, false
	}
	if strings.Count(trimmed, "$$") >= 2 && strings.HasSuffix(trimmed, "$$") && len(trimmed) > 4 {
		return idx, true
	}
	for next := idx + 1; next < len(lines); next++ {
		if strings.HasSuffix(strings.TrimSpace(lines[next]), "$$") {
			return next, true
		}
	}
	return idx, true
}

func markdownDisplayMathToHTML(lines []string, idx int, colorful bool) (string, int, bool) {
	end, ok := MarkdownDisplayMathSpan(lines, idx)
	if !ok {
		return "", idx, false
	}
	render := func(text string) string {
		if colorful {
			text = paperPrepareMath(text)
		}
		return transformMathText(text)
	}
	trimmed := strings.TrimSpace(lines[idx])
	if end == idx {
		if strings.Count(trimmed, "$$") >= 2 && strings.HasSuffix(trimmed, "$$") && len(trimmed) > 4 {
			inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "$$"), "$$"))
			return render(inner), idx, true
		}
		return render(strings.TrimSpace(strings.TrimPrefix(trimmed, "$$"))), idx, true
	}
	var block []string
	first := strings.TrimSpace(strings.TrimPrefix(trimmed, "$$"))
	if first != "" {
		block = append(block, first)
	}
	for next := idx + 1; next <= end; next++ {
		part := strings.TrimSpace(lines[next])
		if next == end {
			part = strings.TrimSpace(strings.TrimSuffix(part, "$$"))
			if part != "" {
				block = append(block, part)
			}
			break
		}
		block = append(block, part)
	}
	return render(strings.Join(block, " ")), end, true
}

func renderInlineHTML(text string) string {
	return renderInlineHTMLMode(text, false, Style{})
}

func renderInlineHTMLMode(text string, colorful bool, style Style) string {
	// Step 1: Extract inline code spans before any other processing.
	// This prevents math ($...$) and bold/italic from parsing inside code.
	var codePlaceholders []string
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1]
		idx := len(codePlaceholders)
		// Pre-render the code span with its content HTML-escaped
		rendered := fmt.Sprintf(`<span style="font-family:monospace; background-color:#f3f4f6; padding:1px 3px; font-size:9pt">%s</span>`, escapeHTML(inner))
		codePlaceholders = append(codePlaceholders, rendered)
		return fmt.Sprintf("\x1aCODE%d\x1a", idx) // Use SUB character as sentinel
	})

	var urlPlaceholders []string
	if colorful {
		text = paperNormalizeLatexLine(text)
		text = markdownBareURLRe.ReplaceAllStringFunc(text, func(match string) string {
			target := strings.TrimRight(match, ".,);，。")
			if target == "" {
				return match
			}
			idx := len(urlPlaceholders)
			urlPlaceholders = append(urlPlaceholders, anchorHTML(escapeHTMLAttr(target), escapeHTML(target), style))
			return fmt.Sprintf("\x1aURL%d\x1a%s", idx, match[len(target):])
		})
	}

	// Step 2: Process math segments on the remaining text (no code spans)
	text = renderMathSegments(text, colorful)

	// Step 3: HTML-escape the text (protects against injection)
	text = escapeHTML(text)
	if colorful {
		text = paperPrepareMath(text)
	}

	// Step 4: Restore math-generated HTML tags (<sub>, <sup>, <span>)
	text = restoreMathHTML(text)

	// Step 5: Process bold and italic
	text = applyInlineEmphasis(text, style)

	// Step 6: Restore code span placeholders
	for idx, rendered := range codePlaceholders {
		text = strings.Replace(text, fmt.Sprintf("\x1aCODE%d\x1a", idx), rendered, 1)
	}
	for idx, rendered := range urlPlaceholders {
		text = strings.Replace(text, fmt.Sprintf("\x1aURL%d\x1a", idx), rendered, 1)
	}
	if colorful {
		markColor := "#c2410c"
		if style.ID != "" {
			markColor = cssHex(style.Colors.Accent)
		}
		text = strings.ReplaceAll(text, "（可能）", `<span style="color:`+markColor+`"><b>（可能）</b></span>`)
	}

	return text
}

func paperBodyStyle(colorful bool, color string, style Style) string {
	if style.ID != "" {
		return ` style="color:` + cssHex(style.Colors.Ink) + `"`
	}
	if !colorful || color == "" {
		return ""
	}
	return ` style="color:` + color + `"`
}

func paperSectionBodyColor(title string) string {
	switch {
	case strings.Contains(title, "方法本质"):
		return "#0f766e"
	case strings.Contains(title, "原理图"):
		return "#5b21b6"
	case strings.Contains(title, "实验"):
		return "#9a3412"
	case strings.Contains(title, "数据集"):
		return "#166534"
	case strings.Contains(title, "开源"):
		return "#075985"
	case strings.Contains(title, "质量"):
		return "#9f1239"
	case strings.Contains(title, "网上核对"):
		return "#b45309"
	case strings.Contains(title, "方法原理"):
		return "#1e40af"
	default:
		return "#1e3a5f"
	}
}

func paperCaptionLine(line string) bool {
	line = strings.TrimLeft(line, "*_ ")
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "figure") || strings.HasPrefix(lower, "fig.") || strings.HasPrefix(lower, "fig ") {
		return true
	}
	if !strings.HasPrefix(line, "图") {
		return false
	}
	rest := strings.TrimLeft(strings.TrimPrefix(line, "图"), " ")
	if rest == "" {
		return false
	}
	r := []rune(rest)[0]
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || r == '：' || r == ':'
}

// The base is the whole token. A tail match would turn softmax_i into s + oftmax_i.
// memory_ehr stays plain because the mark after the underscore is more than one letter.
var paperWordSubRe = regexp.MustCompile(`(^|[^A-Za-z0-9α-ωΑ-Ω])([A-Za-zα-ωΑ-Ω]{1,12})_([A-Za-zα-ωΑ-Ω])([^A-Za-z0-9α-ωΑ-Ω]|$)`)
var paperDigitSubRe = regexp.MustCompile(`(^|[^A-Za-z0-9α-ωΑ-Ω])([A-Za-zα-ωΑ-Ω]{1,12})_([0-9]{1,2})([^0-9]|$)`)
var paperBraceSubRe = regexp.MustCompile(`(^|[^A-Za-z0-9α-ωΑ-Ω])([A-Za-zα-ωΑ-Ω]{1,12})_\{([^{}]+)\}`)

func paperInlineSubscript(text string) string {
	text = paperWordSubRe.ReplaceAllString(text, `$1$2<sub>$3</sub>$4`)
	return paperDigitSubRe.ReplaceAllString(text, `$1$2<sub>$3</sub>$4`)
}

func paperPrepareMath(text string) string {
	text = paperBraceSubRe.ReplaceAllString(text, `$1$2<sub>$3</sub>`)
	return paperInlineSubscript(text)
}

func renderColorfulFormulaLine(line, bodyColor string, style Style) (string, bool) {
	// Recognition looks for → and ≠. A line written with \to or \neq stays
	// ordinary prose unless those commands are glyphs first.
	line = paperNormalizeLatexLine(line)
	parts := attachPaperFormulaPunct(splitPaperFormula(line))
	found := false
	for _, part := range parts {
		if part.formula {
			found = true
			break
		}
	}
	if !found {
		return "", false
	}
	var b strings.Builder
	for _, part := range parts {
		text := strings.TrimSpace(part.text)
		if text == "" {
			continue
		}
		if part.formula {
			b.WriteString(fmt.Sprintf(`<p style="text-align:center; font-size:12pt; color:#6d28d9">%s</p>`, renderInlineHTMLMode(text, true, style)))
			continue
		}
		b.WriteString(fmt.Sprintf("<p%s>%s</p>", paperBodyStyle(true, bodyColor, style), renderInlineHTMLMode(text, true, style)))
	}
	return b.String(), true
}

type paperFormulaPart struct {
	text    string
	formula bool
}

func splitPaperFormula(line string) []paperFormulaPart {
	rs := []rune(line)
	var parts []paperFormulaPart
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		parts = append(parts, paperFormulaPart{text: text.String()})
		text.Reset()
	}
	i := 0
	for i < len(rs) {
		if !paperFormulaRune(rs[i]) {
			text.WriteRune(rs[i])
			i++
			continue
		}
		j := i + 1
		for j < len(rs) && paperFormulaRune(rs[j]) {
			j++
		}
		chunk := string(rs[i:j])
		trimmed := strings.TrimLeft(chunk, " \t:")
		lead := chunk[:len(chunk)-len(trimmed)]
		span := strings.TrimRight(trimmed, " \t:")
		trail := trimmed[len(span):]
		if paperFormulaSpanOK(span) {
			text.WriteString(lead)
			flush()
			parts = append(parts, paperFormulaPart{text: span, formula: true})
			text.WriteString(trail)
		} else {
			text.WriteString(chunk)
		}
		i = j
	}
	flush()
	return parts
}

func attachPaperFormulaPunct(parts []paperFormulaPart) []paperFormulaPart {
	for i := 1; i < len(parts); i++ {
		if !parts[i-1].formula || parts[i].formula {
			continue
		}
		punct, rest := peelPaperLeadPunct(parts[i].text)
		if punct == "" {
			continue
		}
		parts[i-1].text += punct
		parts[i].text = rest
	}
	return parts
}

func peelPaperLeadPunct(s string) (string, string) {
	rs := []rune(s)
	i := 0
	for i < len(rs) && (rs[i] == ' ' || rs[i] == '\t') {
		i++
	}
	start := i
	for i < len(rs) && strings.ContainsRune("。；！？", rs[i]) {
		i++
	}
	if i == start {
		return "", s
	}
	return string(rs[start:i]), string(rs[i:])
}

func paperFormulaRune(r rune) bool {
	if r >= 0x4E00 && r <= 0x9FFF {
		return false
	}
	switch r {
	case '。', '；', '！', '？', '，', '、', '（', '）', '：', '「', '」', '“', '”':
		return false
	default:
		return true
	}
}

func paperFormulaSpanOK(span string) bool {
	span = strings.TrimSpace(span)
	if len([]rune(span)) < 8 {
		return false
	}
	if strings.Contains(span, "http://") || strings.Contains(span, "https://") {
		return false
	}
	if !strings.Contains(span, "=") && !strings.Contains(span, "→") && !strings.Contains(span, "≠") {
		return false
	}
	// _{...} is a subscript mark. Counting only q_j left W_{\theta}(x_{t}) inline.
	marks := len(paperWordSubRe.FindAllString(span, -1)) + len(paperDigitSubRe.FindAllString(span, -1)) + len(paperBraceSubRe.FindAllString(span, -1))
	return marks >= 2
}

func paperNormalizeLatexLine(text string) string {
	text = stripLatexDelimiters(text)
	text = strings.ReplaceAll(text, `\{`, `{`)
	text = strings.ReplaceAll(text, `\}`, `}`)
	return latexSymbolReplacer.Replace(text)
}

func stripLatexDelimiters(text string) string {
	// Longer forms first. \left| is a prefix of \left\|.
	for _, pair := range []struct{ old, new string }{
		{`\left\|`, `|`}, {`\right\|`, `|`},
		{`\left\{`, `{`}, {`\left\}`, `}`}, {`\right\{`, `{`}, {`\right\}`, `}`},
		{`\left\[`, `[`}, {`\left\]`, `]`}, {`\right\[`, `[`}, {`\right\]`, `]`},
		{`\left(`, `(`}, {`\left[`, `[`}, {`\left|`, `|`}, {`\left.`, ``},
		{`\right)`, `)`}, {`\right]`, `]`}, {`\right|`, `|`}, {`\right.`, ``},
	} {
		text = strings.ReplaceAll(text, pair.old, pair.new)
	}
	return text
}

// markdownHeadingBody is the words of one heading. A closing run of hashes
// is not part of the title when a space sits in front of it and only spaces
// follow it. A hash that belongs to the word, as in "C#", stays.
func markdownHeadingBody(trimmed, prefix string) string {
	text := strings.TrimPrefix(trimmed, prefix)
	end := len(text)
	for end > 0 && text[end-1] == ' ' {
		end--
	}
	hash := end
	for hash > 0 && text[hash-1] == '#' {
		hash--
	}
	if hash < end && hash > 0 && text[hash-1] == ' ' {
		return strings.TrimRight(text[:hash], " ")
	}
	return text
}

func markdownHeadingPrefix(trimmed string) (string, bool) {
	for _, prefix := range []string{"##### ", "#### ", "### ", "## ", "# "} {
		if strings.HasPrefix(trimmed, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func markdownHeadingPresentation(prefix string, colorful bool, h2Index int) (size, color string) {
	switch prefix {
	case "##### ":
		size, color = "8.5pt", "#4b5563"
		if colorful {
			color = "#c2410c"
		}
	case "#### ":
		size, color = "9.5pt", "#374151"
		if colorful {
			color = "#7c3aed"
		}
	case "### ":
		size, color = "11pt", "#2c3e50"
		if colorful {
			color = "#0f766e"
		}
	case "## ":
		size, color = "13pt", "#1a1a2e"
		if colorful {
			size = "14pt"
			color = colorfulH2Colors[h2Index%len(colorfulH2Colors)]
		}
	default:
		size, color = "15pt", "#0f3460"
		if colorful {
			color = "#1d4ed8"
		}
	}
	return size, color
}

func renderOneMath(inner string, colorful bool) string {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return ""
	}
	if colorful {
		inner = paperPrepareMath(inner)
	}
	return transformMathText(inner)
}

func renderMathSegments(text string, colorful bool) string {
	// A $$ pair in a sentence is one formula. Replacing it first stops the
	// single-dollar scan from taking the inner dollars apart.
	text = inlineDisplayMathRe.ReplaceAllStringFunc(text, func(segment string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(segment, "$$"), "$$")
		rendered := renderOneMath(inner, colorful)
		if rendered == "" {
			return segment
		}
		return rendered
	})
	return inlineMathSegmentRe.ReplaceAllStringFunc(text, func(segment string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(segment, "$"), "$")
		return renderOneMath(inner, colorful)
	})
}

// PresentFormula turns FormulaNet LaTeX into the HTML the scan writer paints
// inside the formula's original box. It is the same math transform a paper
// PDF already uses for commands, fractions, and subscripts.
func PresentFormula(latex string) string {
	return transformMathText(strings.TrimSpace(latex))
}

func transformMathText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = stripLatexDelimiters(text)
	text = strings.ReplaceAll(text, `\{`, `{`)
	text = strings.ReplaceAll(text, `\}`, `}`)
	text = strings.ReplaceAll(text, `\[`, `[`)
	text = strings.ReplaceAll(text, `\]`, `]`)
	text = strings.ReplaceAll(text, `\|`, `|`)
	text = regexp.MustCompile(`\\sqrt\[([^\[\]]+)\]\{([^{}]+)\}`).ReplaceAllString(text, `root($1, $2)`)
	text = regexp.MustCompile(`\\pmod\{([^{}]+)\}`).ReplaceAllString(text, `(mod $1)`)
	for _, pair := range []struct{ old, new string }{
		{`\Longleftrightarrow`, `⇔`}, {`\Longrightarrow`, `⇒`}, {`\leftrightarrow`, `↔`}, {`\leftarrow`, `←`}, {`\gets`, `←`},
		{`\hookrightarrow`, `↪`}, {`\hookleftarrow`, `↩`}, {`\twoheadrightarrow`, `↠`}, {`\twoheadleftarrow`, `↞`},
		{`\leftharpoonup`, `↼`}, {`\rightharpoonup`, `⇀`}, {`\leftharpoondown`, `↽`}, {`\rightharpoondown`, `⇁`},
		{`\leftrightharpoons`, `⇋`}, {`\rightleftharpoons`, `⇌`}, {`\curvearrowleft`, `↶`}, {`\curvearrowright`, `↷`},
		{`\circlearrowleft`, `↺`}, {`\circlearrowright`, `↻`}, {`\rightsquigarrow`, `⇝`}, {`\leadsto`, `⇝`},
		{`\updownarrow`, `↕`}, {`\nearrow`, `↗`}, {`\searrow`, `↘`}, {`\swarrow`, `↙`}, {`\nwarrow`, `↖`}, {`\mapsto`, `↦`},
		{`\setminus`, `∖`}, {`\oplus`, `⊕`}, {`\otimes`, `⊗`}, {`\ominus`, `⊖`}, {`\propto`, `∝`}, {`\perp`, `⊥`},
		{`\ni`, `∋`}, {`\models`, `⊨`}, {`\top`, `⊤`}, {`\preceq`, `≼`}, {`\succeq`, `≽`}, {`\trianglelefteq`, `⊴`}, {`\trianglerighteq`, `⊵`},
		{`\leqslant`, `⩽`}, {`\geqslant`, `⩾`}, {`\nleqslant`, `⩽̸`}, {`\ngeqslant`, `⩾̸`}, {`\nleq`, `≰`}, {`\ngeq`, `≱`},
		{`\precsim`, `≾`}, {`\succsim`, `≿`}, {`\curlyeqprec`, `⋞`}, {`\curlyeqsucc`, `⋟`}, {`\npreceq`, `⪯̸`}, {`\nsucceq`, `⪰̸`},
		{`\precapprox`, `⪷`}, {`\succapprox`, `⪸`}, {`\leftthreetimes`, `⋋`}, {`\rightthreetimes`, `⋌`}, {`\lesssim`, `≲`}, {`\gtrsim`, `≳`},
		{`\lessgtr`, `≶`}, {`\gtrless`, `≷`}, {`\lessapprox`, `⪅`}, {`\gtrapprox`, `⪆`}, {`\circeq`, `≗`}, {`\eqcirc`, `≖`},
		{`\triangleq`, `≜`}, {`\doteq`, `≐`}, {`\fallingdotseq`, `≒`}, {`\risingdotseq`, `≓`}, {`\eqsim`, `≂`}, {`\bumpeq`, `≏`}, {`\Bumpeq`, `≎`},
		{`\blacktriangleleft`, `◂`}, {`\blacktriangleright`, `▸`}, {`\vartriangleleft`, `⊲`}, {`\vartriangleright`, `⊳`}, {`\multimap`, `⊸`}, {`\pitchfork`, `⋔`},
		{`\backsimeq`, `⋍`}, {`\backsim`, `∽`}, {`\triangleleft`, `◃`}, {`\triangleright`, `▹`}, {`\angle`, `∠`}, {`\triangle`, `△`},
		{`\not\subseteq`, `⊈`}, {`\not\supseteq`, `⊉`}, {`\nsubseteq`, `⊈`}, {`\nsupseteq`, `⊉`}, {`\not\subset`, `⊄`}, {`\not\supset`, `⊅`},
		{`\nsubset`, `⊄`}, {`\nsupset`, `⊅`}, {`\not\leq`, `≰`}, {`\not\geq`, `≱`}, {`\not\equiv`, `≢`}, {`\not\sim`, `≁`}, {`\not\cong`, `≇`},
		{`\nsim`, `≁`}, {`\ncong`, `≇`}, {`\not\approx`, `≉`}, {`\not=`, `≠`},
	} {
		text = strings.ReplaceAll(text, pair.old, pair.new)
	}
	for _, re := range []struct{ pattern, repl string }{
		{`\\bmod`, `mod`}, {`\\mod`, `mod`}, {`\\limsup`, `limsup`}, {`\\liminf`, `liminf`}, {`\\gcd`, `gcd`}, {`\\det`, `det`}, {`\\dim`, `dim`}, {`\\ker`, `ker`}, {`\\Pr`, `Pr`}, {`\\mathbb\{E\}`, `bb(E)`},
	} {
		text = regexp.MustCompile(re.pattern).ReplaceAllString(text, re.repl)
	}
	text = strings.ReplaceAll(text, `\sup_`, `sup_`)
	text = strings.ReplaceAll(text, `\sup{`, `sup{`)
	text = strings.ReplaceAll(text, `\sup `, `sup `)
	text = strings.ReplaceAll(text, `\inf_`, `inf_`)
	text = strings.ReplaceAll(text, `\inf{`, `inf{`)
	text = strings.ReplaceAll(text, `\inf `, `inf `)
	text = latexSymbolReplacer.Replace(text)
	text = latexFunctionReplacer.Replace(text)
	for _, re := range []struct{ pattern, repl string }{
		{`\\mathbb\{([^{}]+)\}`, `bb($1)`}, {`\\mathbf\{([^{}]+)\}`, `bf($1)`}, {`\\mathrm\{([^{}]+)\}`, `$1`}, {`\\mathcal\{([^{}]+)\}`, `cal($1)`},
		{`\\textbf\{([^{}]+)\}`, `bold($1)`}, {`\\textit\{([^{}]+)\}`, `italic($1)`}, {`\\emph\{([^{}]+)\}`, `emph($1)`}, {`\\text\{([^{}]+)\}`, `$1`},
	} {
		text = regexp.MustCompile(re.pattern).ReplaceAllString(text, re.repl)
	}
	for _, env := range []struct {
		pattern string
		fn      func(string) string
	}{
		{`\\begin\{cases\}([\s\S]*?)\\end\{cases\}`, transformCasesEnvironment},
		{`\\begin\{aligned\}([\s\S]*?)\\end\{aligned\}`, transformAlignedEnvironment},
		{`\\begin\{align\*?\}([\s\S]*?)\\end\{align\*?\}`, transformAlignedEnvironment},
		{`\\begin\{eqnarray\}([\s\S]*?)\\end\{eqnarray\}`, transformAlignedEnvironment},
		{`\\begin\{eqnarray\*\}([\s\S]*?)\\end\{eqnarray\*\}`, transformAlignedEnvironment},
		{`\\begin\{array\}(?:\{[^{}]*\})?([\s\S]*?)\\end\{array\}`, transformArrayEnvironment},
		{`\\begin\{vmatrix\}([\s\S]*?)\\end\{vmatrix\}`, transformDeterminantEnvironment},
		{`\\begin\{Vmatrix\}([\s\S]*?)\\end\{Vmatrix\}`, transformNormEnvironment},
		{`\\begin\{bmatrix\}([\s\S]*?)\\end\{bmatrix\}`, transformMatrixEnvironment},
		{`\\begin\{matrix\}([\s\S]*?)\\end\{matrix\}`, transformPlainMatrixEnvironment},
		{`\\begin\{smallmatrix\}([\s\S]*?)\\end\{smallmatrix\}`, transformSmallMatrixEnvironment},
		{`\\begin\{pmatrix\}([\s\S]*?)\\end\{pmatrix\}`, transformParenMatrixEnvironment},
		{`\\begin\{Bmatrix\}([\s\S]*?)\\end\{Bmatrix\}`, transformBraceMatrixEnvironment},
	} {
		text = regexp.MustCompile(env.pattern).ReplaceAllStringFunc(text, env.fn)
	}
	text = replaceFractions(text)
	for _, re := range []struct{ pattern, repl string }{
		{`\\overbrace\{([^{}]+)\}(?:\^\{([^{}]+)\})?`, `overbrace($1, $2)`},
		{`\\underbrace\{([^{}]+)\}(?:_\{([^{}]+)\})?`, `underbrace($1, $2)`},
		{`\\overparen\{([^{}]+)\}`, `overparen($1)`}, {`\\underparen\{([^{}]+)\}`, `underparen($1)`}, {`\\binom\{([^{}]+)\}\{([^{}]+)\}`, `binom($1, $2)`},
		{`\\overline\{([^{}]+)\}`, `overline($1)`}, {`\\underline\{([^{}]+)\}`, `underline($1)`}, {`\\hat\{([^{}]+)\}`, `hat($1)`}, {`\\tilde\{([^{}]+)\}`, `tilde($1)`},
		{`\\ddot\{([^{}]+)\}`, `ddot($1)`}, {`\\dot\{([^{}]+)\}`, `dot($1)`}, {`\\cancel\{([^{}]+)\}`, `cancel($1)`}, {`\\boxed\{([^{}]+)\}`, `boxed($1)`},
		{`\\overset\{([^{}]+)\}\{([^{}]+)\}`, `overset($1, $2)`}, {`\\underset\{([^{}]+)\}\{([^{}]+)\}`, `underset($1, $2)`},
		{`\\operatorname\*\{([^{}]+)\}`, `$1`}, {`\\operatorname\{([^{}]+)\}`, `$1`}, {`\\xmapsto\{([^{}]+)\}`, `xmapsto($1)`},
		{`\\xleftrightarrow\{([^{}]+)\}`, `xleftrightarrow($1)`}, {`\\xrightarrow\{([^{}]+)\}`, `xrightarrow($1)`}, {`\\xleftarrow\{([^{}]+)\}`, `xleftarrow($1)`},
		{`\{([^{}]+)\\choose([^{}]+)\}`, `choose($1, $2)`}, {`\\overleftrightarrow\{([^{}]+)\}`, `overleftrightarrow($1)`}, {`\\underrightarrow\{([^{}]+)\}`, `underrightarrow($1)`},
		{`\\underleftarrow\{([^{}]+)\}`, `underleftarrow($1)`}, {`\\overrightarrow\{([^{}]+)\}`, `overrightarrow($1)`}, {`\\overleftarrow\{([^{}]+)\}`, `overleftarrow($1)`},
		{`\\bar\{([^{}]+)\}`, `bar($1)`}, {`\\vec\{([^{}]+)\}`, `vec($1)`}, {`\\sqrt\[([^\[\]]+)\]\{([^{}]+)\}`, `root($1, $2)`}, {`\\sqrt\{([^{}]+)\}`, `√($1)`},
		{`\\(?:d|t)?frac\{([^{}]+)\}\{([^{}]+)\}`, `($1)/($2)`}, {`\\(?:big|Big|bigg|Bigg)(l|r|m)?`, ``}, {`\\(?:displaystyle|textstyle|scriptstyle|scriptscriptstyle)`, ``},
	} {
		text = regexp.MustCompile(re.pattern).ReplaceAllString(text, re.repl)
	}
	for _, pair := range []struct{ old, new string }{{`\,`, " "}, {`\!`, ""}, {`\;`, " "}, {`\:`, " "}, {`\quad`, " "}, {`\qquad`, " "}} {
		text = strings.ReplaceAll(text, pair.old, pair.new)
	}
	for _, re := range []struct{ pattern, repl string }{
		{`(argmin|argmax|min|max)_\{([^{}]+)\}`, `$1<sub>$2</sub>`}, {`(argmin|argmax|min|max)_([A-Za-z0-9α-ωΑ-Ω]+)`, `$1<sub>$2</sub>`}, {`\\lim_\{([^{}]+)\}`, `lim<sub>$1</sub>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_\{([^{}]+)\}\^\{([^{}]+)\}`, `$1<sub>$2</sub><sup>$3</sup>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_([A-Za-z0-9α-ωΑ-Ω]+)\^([A-Za-z0-9α-ωΑ-Ω+\-])`, `$1<sub>$2</sub><sup>$3</sup>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_\{([^{}]+)\}\^([A-Za-z0-9α-ωΑ-Ω+\-])`, `$1<sub>$2</sub><sup>$3</sup>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_([A-Za-z0-9α-ωΑ-Ω]+)\^\{([^{}]+)\}`, `$1<sub>$2</sub><sup>$3</sup>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_\{([^{}]+)\}`, `$1<sub>$2</sub>`}, {`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)_([A-Za-z0-9α-ωΑ-Ω]+)`, `$1<sub>$2</sub>`},
		{`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)\^\{([^{}]+)\}`, `$1<sup>$2</sup>`}, {`(∑|∏|∐|⋃|⋂|⨁|⨂|⨀|⋁|⋀|⨆|∭|∬|∮|∫)\^([A-Za-z0-9α-ωΑ-Ω+\-])`, `$1<sup>$2</sup>`},
		{`\|\|([^|\s][^|]*?[^|\s]|[^|\s])\|\|`, `norm($1)`}, {`\|([^|\s][^|]*?[^|\s]|[^|\s])\|`, `abs($1)`}, {`([A-Za-z0-9α-ωΑ-Ω∑∏∫√\)\]\}])\^\{([^{}]+)\}`, `$1<sup>$2</sup>`},
		{`([A-Za-z0-9α-ωΑ-Ω∑∏∫√\)\]\}])_\{([^{}]+)\}`, `$1<sub>$2</sub>`}, {`([A-Za-z0-9α-ωΑ-Ω∑∏∫√\)\]\}])\^([A-Za-z0-9α-ωΑ-Ω+\-])`, `$1<sup>$2</sup>`}, {`([A-Za-z0-9α-ωΑ-Ω∑∏∫√])_([A-Za-z0-9α-ωΑ-Ω])`, `$1<sub>$2</sub>`},
	} {
		text = regexp.MustCompile(re.pattern).ReplaceAllString(text, re.repl)
	}
	for _, pair := range []struct{ old, new string }{{`&=`, `=`}, {` , `, `, `}, {`\\,`, " "}, {`\\`, `\`}, {`{`, ``}, {`}`, ``}} {
		text = strings.ReplaceAll(text, pair.old, pair.new)
	}
	text = regexp.MustCompile(`\s{2,}`).ReplaceAllString(text, " ")
	text = escapeHTML(text)
	text = strings.ReplaceAll(text, "&lt;span style=\"white-space:nowrap\"&gt;", `<span style="white-space:nowrap">`)
	return restoreMathHTML(strings.TrimSpace(text))
}

func replaceFractions(text string) string {
	fractionRe := regexp.MustCompile(`\\(?:d|t)?frac\{([^{}]+)\}\{([^{}]+)\}`)
	for {
		next := fractionRe.ReplaceAllString(text, `<span style="white-space:nowrap">($1)/($2)</span>`)
		if next == text {
			return text
		}
		text = next
	}
}

func transformMatrixEnvironment(expr string) string {
	return formatMatrixRows(stripMathEnvironment(expr, "bmatrix", "pmatrix"), "[", "]")
}
func transformPlainMatrixEnvironment(expr string) string {
	return "matrix" + formatMatrixRows(stripMathEnvironment(expr, "matrix"), "[", "]")
}
func transformSmallMatrixEnvironment(expr string) string {
	return "smallmatrix" + formatMatrixRows(stripMathEnvironment(expr, "smallmatrix"), "[", "]")
}
func transformParenMatrixEnvironment(expr string) string {
	return formatMatrixRows(stripMathEnvironment(expr, "pmatrix"), "(", ")")
}
func transformBraceMatrixEnvironment(expr string) string {
	return "brace" + formatMatrixRows(stripMathEnvironment(expr, "Bmatrix"), "[", "]")
}
func transformDeterminantEnvironment(expr string) string {
	return "det" + formatMatrixRows(stripMathEnvironment(expr, "vmatrix"), "[", "]")
}
func transformNormEnvironment(expr string) string {
	return "norm" + formatMatrixRows(stripMathEnvironment(expr, "Vmatrix"), "[", "]")
}

func transformArrayEnvironment(expr string) string {
	content := stripMathEnvironmentWithSpec(expr, "array")
	rows := splitMathRows(content)
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := splitMathCells(row)
		if len(cells) == 0 {
			continue
		}
		parts = append(parts, strings.Join(transformMathCellSlice(cells), " | "))
	}
	return "array[" + strings.Join(parts, "; ") + "]"
}

func transformCasesEnvironment(expr string) string {
	content := stripMathEnvironment(expr, "cases")
	rows := splitMathRows(content)
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := splitMathCells(row)
		if len(cells) == 0 {
			continue
		}
		if len(cells) == 1 {
			parts = append(parts, transformMathText(cells[0]))
			continue
		}
		parts = append(parts, transformMathText(cells[0])+", "+strings.Join(transformMathCellSlice(cells[1:]), ", "))
	}
	return "cases(" + strings.Join(parts, "; ") + ")"
}

func transformAlignedEnvironment(expr string) string {
	content := stripMathEnvironment(expr, "aligned", "align", "align*", "eqnarray", "eqnarray*")
	rows := splitMathRows(content)
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := splitMathCells(row)
		if len(cells) == 0 {
			continue
		}
		parts = append(parts, strings.Join(transformMathCellSlice(cells), " "))
	}
	return strings.Join(parts, " ; ")
}

func stripMathEnvironment(expr string, names ...string) string {
	content := strings.TrimSpace(expr)
	for _, name := range names {
		content = strings.TrimPrefix(content, `\begin{`+name+`}`)
		content = strings.TrimSuffix(content, `\end{`+name+`}`)
	}
	return strings.TrimSpace(content)
}

func stripMathEnvironmentWithSpec(expr, name string) string {
	content := strings.TrimSpace(expr)
	prefix := regexp.MustCompile(`^\\begin\{` + regexp.QuoteMeta(name) + `\}(?:\{[^{}]*\})?`)
	content = prefix.ReplaceAllString(content, "")
	content = strings.TrimSuffix(content, `\end{`+name+`}`)
	return strings.TrimSpace(content)
}

func formatMatrixRows(content, open, close string) string {
	rows := splitMathRows(content)
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		cells := splitMathCells(row)
		if len(cells) == 0 {
			continue
		}
		parts = append(parts, open+strings.Join(transformMathCellSlice(cells), ", ")+close)
	}
	return open + strings.Join(parts, "; ") + close
}

func splitMathRows(content string) []string {
	rows := strings.Split(content, `\\`)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		row = strings.TrimSpace(row)
		if row != "" {
			out = append(out, row)
		}
	}
	return out
}

func splitMathCells(row string) []string {
	parts := strings.Split(row, `&`)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func transformMathCellSlice(cells []string) []string {
	out := make([]string, 0, len(cells))
	for _, cell := range cells {
		out = append(out, strings.TrimSpace(transformMathText(cell)))
	}
	return out
}

func restoreMathHTML(text string) string {
	for _, pair := range []struct{ old, new string }{
		{"&amp;lt;", "&lt;"}, {"&amp;gt;", "&gt;"}, {"&amp;le;", "&le;"}, {"&amp;ge;", "&ge;"},
		{"&lt;sup&gt;", "<sup>"}, {"&lt;/sup&gt;", "</sup>"}, {"&lt;sub&gt;", "<sub>"}, {"&lt;/sub&gt;", "</sub>"},
		{"&lt;span style=&#34;white-space:nowrap&#34;&gt;", `<span style="white-space:nowrap">`}, {"&lt;/span&gt;", "</span>"},
	} {
		text = strings.ReplaceAll(text, pair.old, pair.new)
	}
	return text
}

// tableCellHTML renders one table cell.
//
// A cell is the body's inline pipeline without the parts a cell cannot hold:
// a cell is one color, so a link inside it does not take the link color. Math
// is the exception — a table of formulas is a table of formulas, not a table
// of dollar signs.
//
// The order matches the body: code first, then math, then escaping, then
// emphasis. A formula inside a code span stays code.
func tableCellHTML(value string, colorful bool) string {
	var codes []string
	text := inlineCodeRe.ReplaceAllStringFunc(value, func(match string) string {
		inner := match[1 : len(match)-1]
		codes = append(codes, fmt.Sprintf(`<span style="font-family:monospace; background-color:#f3f4f6; padding:1px 3px; font-size:9pt">%s</span>`, escapeHTML(inner)))
		return fmt.Sprintf("\x1aC%d\x1a", len(codes)-1)
	})
	text = renderMathSegments(text, colorful)
	text = escapeHTML(text)
	text = restoreMathHTML(text)
	text = applyInlineEmphasis(text, Style{})
	for i, rendered := range codes {
		text = strings.Replace(text, fmt.Sprintf("\x1aC%d\x1a", i), rendered, 1)
	}
	return text
}

func inlineMD(text string) string {
	// Code is taken out before emphasis. Otherwise ** inside `code` is seen
	// as bold and the span tag gets cut in half.
	var codes []string
	text = inlineCodeRe.ReplaceAllStringFunc(text, func(match string) string {
		inner := match[1 : len(match)-1]
		codes = append(codes, fmt.Sprintf(`<span style="font-family:monospace; background-color:#f3f4f6; padding:1px 3px; font-size:9pt">%s</span>`, inner))
		return fmt.Sprintf("\x1aC%d\x1a", len(codes)-1)
	})
	text = applyInlineEmphasis(text, Style{})
	for i, rendered := range codes {
		text = strings.Replace(text, fmt.Sprintf("\x1aC%d\x1a", i), rendered, 1)
	}
	return text
}

// applyInlineEmphasis turns **bold**, __bold__, and *italic* into tags.
//
// Star bold runs before underscore bold, so __**粗**__ does not keep the
// stars. A single "_" is left alone: file_name and Qwen2.5 are not emphasis.
func applyInlineEmphasis(text string, style Style) string {
	text = inlineStarBoldRe.ReplaceAllString(text, "<b>$1</b>")
	text = inlineUnderscoreBoldRe.ReplaceAllString(text, "<b>$1</b>")
	text = inlineItalicRe.ReplaceAllString(text, "<i>$1</i>")
	return inlineLinkRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := inlineLinkRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		return parts[1] + anchorHTML(parts[3], parts[2], style)
	})
}

// anchorHTML keeps the link annotation. GoPDF2 paints every <a> in pure blue
// and ignores the anchor's own color; a child span replaces that blue.
// Table cells use one color for the whole cell, so they do not take this span.
func anchorHTML(href, label string, style Style) string {
	href = strings.ReplaceAll(href, `"`, "&quot;")
	if style.ID != "" {
		label = `<span style="color:` + cssHex(style.Colors.Link) + `">` + label + `</span>`
	}
	return `<a href="` + href + `">` + label + `</a>`
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func escapeHTMLAttr(s string) string {
	s = escapeHTML(s)
	return strings.ReplaceAll(s, `"`, "&quot;")
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = sanitizeFileNameRe.ReplaceAllString(name, "_")
	if len(name) > 30 {
		name = name[:30]
	}
	return name
}
