package docgen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownHorizontalRuleDashRe = regexp.MustCompile(`^-{3,}$`)
var markdownHorizontalRuleStarRe = regexp.MustCompile(`^\*{3,}$`)
var markdownOrderedListRe = regexp.MustCompile(`^\d+\.\s`)
var markdownImageRe = regexp.MustCompile(`^!\[(.*?)\]\((.*?)\)$`)
var inlineMathSegmentRe = regexp.MustCompile(`\$(.+?)\$`)
var inlineBoldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)
var inlineItalicRe = regexp.MustCompile(`\*(.+?)\*`)
var sanitizeFileNameRe = regexp.MustCompile(`[<>:"/\\|?*\s]+`)

var latexSymbolReplacer = strings.NewReplacer(
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
	lines := strings.Split(md, "\n")
	var sb strings.Builder
	listType := ""
	closeList := func() {
		switch listType {
		case "ul":
			sb.WriteString("</ul>")
		case "ol":
			sb.WriteString("</ol>")
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
			sb.WriteString("<hr/>")
			continue
		}
		if strings.HasPrefix(trimmed, "##### ") {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="font-size:8.5pt; color:#4b5563"><b>%s</b></p>`, renderInlineHTML(strings.TrimPrefix(trimmed, "##### "))))
			continue
		}
		if strings.HasPrefix(trimmed, "#### ") {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="font-size:9.5pt; color:#374151"><b>%s</b></p>`, renderInlineHTML(strings.TrimPrefix(trimmed, "#### "))))
			continue
		}
		if strings.HasPrefix(trimmed, "### ") {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="font-size:11pt; color:#2c3e50"><b>%s</b></p>`, renderInlineHTML(strings.TrimPrefix(trimmed, "### "))))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="font-size:13pt; color:#1a1a2e"><b>%s</b></p>`, renderInlineHTML(strings.TrimPrefix(trimmed, "## "))))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="font-size:15pt; color:#0f3460"><b>%s</b></p>`, renderInlineHTML(strings.TrimPrefix(trimmed, "# "))))
			continue
		}
		if imageHTML, ok := markdownImageToHTML(trimmed); ok {
			if listType != "" { closeList() }
			sb.WriteString(imageHTML)
			continue
		}
		if tableHTML, nextIdx, ok := markdownTableToHTML(lines, i); ok {
			if listType != "" { closeList() }
			sb.WriteString(tableHTML)
			i = nextIdx - 1
			continue
		}
		if text, nextIdx, ok := markdownDisplayMathToHTML(lines, i); ok {
			if listType != "" { closeList() }
			sb.WriteString(fmt.Sprintf(`<p style="text-align:center">%s</p>`, text))
			i = nextIdx
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			ensureList("ul")
			text := strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
			sb.WriteString(fmt.Sprintf("<li>%s</li>", renderInlineHTML(text)))
			continue
		}
		if markdownOrderedListRe.MatchString(trimmed) {
			ensureList("ol")
			text := trimmed
			if idx := strings.Index(text, ". "); idx > 0 {
				text = text[idx+2:]
			}
			sb.WriteString(fmt.Sprintf("<li>%s</li>", renderInlineHTML(text)))
			continue
		}
		if listType != "" {
			closeList()
		}
		sb.WriteString(fmt.Sprintf("<p>%s</p>", renderInlineHTML(trimmed)))
	}
	if listType != "" {
		closeList()
	}
	return sb.String()
}

func markdownImageToHTML(line string) (string, bool) {
	matches := markdownImageRe.FindStringSubmatch(strings.TrimSpace(line))
	if len(matches) != 3 {
		return "", false
	}
	alt := strings.TrimSpace(matches[1])
	rawPath := strings.TrimSpace(matches[2])
	imagePath, ok := resolveMarkdownImagePath(rawPath)
	if ok {
		caption := ""
		if alt != "" {
			caption = fmt.Sprintf(`<p style="font-size:9pt; color:#666"><i>%s</i></p>`, inlineMD(escapeHTML(alt)))
		}
		return fmt.Sprintf(`<p><img src="%s" width="480"/></p>%s`, escapeHTMLAttr(imagePath), caption), true
	}
	if isRemoteURL(rawPath) {
		return fmt.Sprintf(`<p><b>图片：</b>%s</p><p style="font-size:9pt; color:#666"><i>暂不支持远程图片：%s</i></p>`, inlineMD(escapeHTML(fallbackText(alt, "未命名图片"))), inlineMD(escapeHTML(rawPath))), true
	}
	return fmt.Sprintf(`<p><b>图片：</b>%s</p><p style="font-size:9pt; color:#666"><i>图片未找到：%s</i></p>`, inlineMD(escapeHTML(fallbackText(alt, "未命名图片"))), inlineMD(escapeHTML(rawPath))), true
}

func resolveMarkdownImagePath(rawPath string) (string, bool) {
	path := strings.TrimSpace(rawPath)
	if path == "" || isRemoteURL(path) {
		return "", false
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

func markdownTableToHTML(lines []string, start int) (string, int, bool) {
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
		sb.WriteString(fmt.Sprintf(`<th>%s</th>`, inlineMD(escapeHTML(header))))
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
			sb.WriteString(fmt.Sprintf(`<td>%s</td>`, inlineMD(escapeHTML(value))))
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

func markdownDisplayMathToHTML(lines []string, idx int) (string, int, bool) {
	trimmed := strings.TrimSpace(lines[idx])
	if !strings.HasPrefix(trimmed, "$$") {
		return "", idx, false
	}
	if strings.Count(trimmed, "$$") >= 2 && strings.HasSuffix(trimmed, "$$") && len(trimmed) > 4 {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "$$"), "$$"))
		return transformMathText(inner), idx, true
	}
	var block []string
	first := strings.TrimSpace(strings.TrimPrefix(trimmed, "$$"))
	if first != "" {
		block = append(block, first)
	}
	for next := idx + 1; next < len(lines); next++ {
		part := strings.TrimSpace(lines[next])
		if strings.HasSuffix(part, "$$") {
			part = strings.TrimSpace(strings.TrimSuffix(part, "$$"))
			if part != "" {
				block = append(block, part)
			}
			return transformMathText(strings.Join(block, " ")), next, true
		}
		block = append(block, part)
	}
	return transformMathText(strings.TrimSpace(strings.TrimPrefix(trimmed, "$$"))), idx, true
}

func renderInlineHTML(text string) string {
	text = renderMathSegments(text)
	text = escapeHTML(text)
	text = restoreMathHTML(text)
	return inlineMD(text)
}

func renderMathSegments(text string) string {
	return inlineMathSegmentRe.ReplaceAllStringFunc(text, func(segment string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(segment, "$"), "$")
		return transformMathText(inner)
	})
}

func transformMathText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, `\left(`, `(`)
	text = strings.ReplaceAll(text, `\left[`, `[`)
	text = strings.ReplaceAll(text, `\left|`, `|`)
	text = strings.ReplaceAll(text, `\left.`, ``)
	text = strings.ReplaceAll(text, `\left\{`, `{`)
	text = strings.ReplaceAll(text, `\left\}`, `}`)
	text = strings.ReplaceAll(text, `\left\[`, `[`)
	text = strings.ReplaceAll(text, `\left\]`, `]`)
	text = strings.ReplaceAll(text, `\left\|`, `|`)
	text = strings.ReplaceAll(text, `\right)`, `)`)
	text = strings.ReplaceAll(text, `\right]`, `]`)
	text = strings.ReplaceAll(text, `\right|`, `|`)
	text = strings.ReplaceAll(text, `\right.`, ``)
	text = strings.ReplaceAll(text, `\right\{`, `{`)
	text = strings.ReplaceAll(text, `\right\}`, `}`)
	text = strings.ReplaceAll(text, `\right\[`, `[`)
	text = strings.ReplaceAll(text, `\right\]`, `]`)
	text = strings.ReplaceAll(text, `\right\|`, `|`)
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
	for _, env := range []struct{ pattern string; fn func(string) string }{
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

func transformMatrixEnvironment(expr string) string { return formatMatrixRows(stripMathEnvironment(expr, "bmatrix", "pmatrix"), "[", "]") }
func transformPlainMatrixEnvironment(expr string) string { return "matrix" + formatMatrixRows(stripMathEnvironment(expr, "matrix"), "[", "]") }
func transformSmallMatrixEnvironment(expr string) string { return "smallmatrix" + formatMatrixRows(stripMathEnvironment(expr, "smallmatrix"), "[", "]") }
func transformParenMatrixEnvironment(expr string) string { return formatMatrixRows(stripMathEnvironment(expr, "pmatrix"), "(", ")") }
func transformBraceMatrixEnvironment(expr string) string { return "brace" + formatMatrixRows(stripMathEnvironment(expr, "Bmatrix"), "[", "]") }
func transformDeterminantEnvironment(expr string) string { return "det" + formatMatrixRows(stripMathEnvironment(expr, "vmatrix"), "[", "]") }
func transformNormEnvironment(expr string) string { return "norm" + formatMatrixRows(stripMathEnvironment(expr, "Vmatrix"), "[", "]") }

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

func inlineMD(text string) string {
	text = inlineBoldRe.ReplaceAllString(text, "<b>$1</b>")
	text = inlineItalicRe.ReplaceAllString(text, "<i>$1</i>")
	return text
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
