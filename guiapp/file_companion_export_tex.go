package guiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// fileCompanionSaveTeXDialog is the save dialog for a LaTeX document.
// The PDF buttons keep their own dialog. Tests replace this.
var fileCompanionSaveTeXDialog = func(a *App, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 LaTeX",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "LaTeX (*.tex)", Pattern: "*.tex"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// FileCompanionExportTeX writes one answer as a LaTeX document.
// The default PDF click does not call this.
func (a *App) FileCompanionExportTeX(path, markdown string) (string, error) {
	return a.fileCompanionExportTeX(path, markdown, "回答.tex")
}

// FileCompanionExportChatTeX writes the whole chat as a LaTeX document.
func (a *App) FileCompanionExportChatTeX(path string, turns []CompanionChatTurn) (string, error) {
	body, turnCount := fileCompanionChatDocument(turns)
	if turnCount == 0 {
		return "", errors.New("没有可导出的内容")
	}
	return a.fileCompanionExportTeX(path, body, "对话.tex")
}

// FileCompanionExportWebTeX writes one web-tab answer. It does not call fileCompanionGrant.
func (a *App) FileCompanionExportWebTeX(sessionID, markdown string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	if a.fileCompanionWebFrame(sessionID) == "" {
		return "", errors.New("web session is not open")
	}
	return a.fileCompanionWriteTeX(markdown, "回答.tex", "", false)
}

// FileCompanionExportWebChatTeX writes a web tab's chat as a LaTeX document.
func (a *App) FileCompanionExportWebChatTeX(sessionID string, turns []CompanionChatTurn) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	if a.fileCompanionWebFrame(sessionID) == "" {
		return "", errors.New("web session is not open")
	}
	body, turnCount := fileCompanionChatDocument(turns)
	if turnCount == 0 {
		return "", errors.New("没有可导出的内容")
	}
	return a.fileCompanionWriteTeX(body, "对话.tex", "", false)
}

func (a *App) fileCompanionExportTeX(path, markdown, defaultName string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	grant, _, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", errFileCompanionNotGranted
	}
	return a.fileCompanionWriteTeX(markdown, defaultName, strings.TrimSpace(grant.Canonical), true)
}

func (a *App) fileCompanionWriteTeX(markdown, defaultName, source string, refuseOverwrite bool) (string, error) {
	body := strings.TrimSpace(markdown)
	if body == "" {
		return "", errors.New("没有可导出的内容")
	}
	dest, err := fileCompanionSaveTeXDialog(a, defaultName)
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	chosen := dest
	if !strings.EqualFold(filepath.Ext(dest), ".tex") {
		dest += ".tex"
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	// The dialog can hand back the open file before ".tex" is appended.
	// notes.md must be refused, not written as notes.md.tex.
	if refuseOverwrite && fileCompanionExportHitsOpenFile(source, chosen, dest) {
		return "", errors.New(fileCompanionErrCannotOverwrite)
	}
	document := fileCompanionMarkdownToTeX(body)
	if err := os.WriteFile(dest, []byte(document), 0o644); err != nil {
		// Same wrapper as a PDF that cannot be written, so a full disk or a
		// refused folder is told apart from a generic failure.
		return "", fmt.Errorf("写入 PDF 失败: %w", err)
	}
	return dest, nil
}

// fileCompanionExportHitsOpenFile is true when the dialog returned the open
// file, including the path from before a missing ".tex" suffix is appended.
func fileCompanionExportHitsOpenFile(source, chosen, dest string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	info, err := os.Stat(source)
	if err != nil || info.IsDir() {
		return false
	}
	if strings.TrimSpace(chosen) != "" && sameFileCompanionExportPath(source, chosen) {
		return true
	}
	return strings.TrimSpace(dest) != "" && sameFileCompanionExportPath(source, dest)
}

// fileCompanionTeXDocumentHead is a document a reader can compile.
// Companion answers are Chinese, so the class is ctexart and the engine is
// XeLaTeX or LuaLaTeX. amsmath keeps aligned formulas; graphicx, booktabs,
// and hyperref cover pictures, tables, and links.
const fileCompanionTeXDocumentHead = `% 请使用 XeLaTeX 或 LuaLaTeX 编译
\documentclass{ctexart}
\usepackage{amsmath,amssymb}
\usepackage{graphicx}
\usepackage{booktabs}
\usepackage{hyperref}
\hypersetup{hidelinks}
\begin{document}
`

// fileCompanionMarkdownToTeX turns one Markdown answer into a LaTeX document.
// Dollar math becomes \(...\) and \[...\]. Headings, emphasis, lists, tables,
// code fences, quotes, and pictures become LaTeX commands. A line marked
// 识别不确定 stays ordinary text: its dollars are not turned into a formula.
func fileCompanionMarkdownToTeX(markdown string) string {
	body := strings.TrimSpace(fileCompanionMarkdownBodyToTeX(markdown))
	return fileCompanionTeXDocumentHead + body + "\n\\end{document}\n"
}

func fileCompanionMarkdownBodyToTeX(markdown string) string {
	lines := strings.Split(markdown, "\n")
	var doc texSink
	var math []string
	inMath := false
	mathClose := ""
	flush := func() {
		body := strings.TrimSpace(strings.Join(math, "\n"))
		math = nil
		inMath = false
		mathClose = ""
		if body == "" {
			return
		}
		doc.block(`\[` + body + `\]`)
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		if inMath {
			if fileCompanionDisplayMathWhole(trim) {
				flush()
			} else if fileCompanionDisplayMathCloses(trim, mathClose) {
				if trim != mathClose {
					head := strings.TrimSpace(trim[:len(trim)-len(mathClose)])
					if head != "" {
						math = append(math, head)
					}
				}
				flush()
				continue
			} else if fileCompanionTeXEndsDisplayMath(line, trim) {
				flush()
			} else {
				if trim != "" {
					math = append(math, trim)
				}
				continue
			}
		}
		if trim == "" {
			doc.closeList()
			continue
		}
		if strings.Contains(line, fileCompanionErrOCRUncertain) || strings.Contains(line, "识别不确定") {
			doc.block(fileCompanionEscapeTeX(trim))
			continue
		}
		if img, ok := fileCompanionImageLineToTeX(trim); ok {
			doc.block(img)
			continue
		}
		if fileCompanionDisplayMathWhole(trim) {
			inner := strings.TrimSpace(trim[2 : len(trim)-2])
			if inner != "" {
				doc.block(`\[` + inner + `\]`)
			}
			continue
		}
		if closeTok, ok := fileCompanionDisplayMathOpens(trim); ok {
			rest := ""
			if len(trim) > 2 {
				rest = strings.TrimSpace(trim[2:])
			}
			inMath = true
			mathClose = closeTok
			if rest == "" {
				continue
			}
			if fileCompanionDisplayMathCloses(rest, closeTok) {
				if rest != closeTok {
					head := strings.TrimSpace(rest[:len(rest)-len(closeTok)])
					if head != "" {
						math = append(math, head)
					}
				}
				flush()
				continue
			}
			math = append(math, rest)
			continue
		}
		if heading, ok := fileCompanionHeadingToTeX(trim); ok {
			doc.block(heading)
			continue
		}
		if fileCompanionTeXRule(trim) {
			doc.block(`\noindent\rule{\linewidth}{0.4pt}`)
			continue
		}
		if marker, _, ok := fileCompanionSplitFence(trim); ok {
			var code []string
			j := i + 1
			for j < len(lines) && !fileCompanionTeXFenceCloses(lines[j], marker) {
				code = append(code, lines[j])
				j++
			}
			body := strings.ReplaceAll(strings.Join(code, "\n"), `\end{verbatim}`, `\end{verbatim }`)
			doc.block("\\begin{verbatim}\n" + body + "\n\\end{verbatim}")
			if j < len(lines) {
				i = j
			} else {
				i = len(lines)
			}
			continue
		}
		if strings.HasPrefix(trim, ">") {
			var parts []string
			j := i
			for j < len(lines) {
				part := strings.TrimSpace(lines[j])
				if !strings.HasPrefix(part, ">") {
					break
				}
				parts = append(parts, strings.TrimSpace(strings.TrimPrefix(part, ">")))
				j++
			}
			var b strings.Builder
			b.WriteString("\\begin{quote}\n")
			wrote := false
			for _, part := range parts {
				if part == "" {
					continue
				}
				if wrote {
					b.WriteString("\n\n")
				}
				wrote = true
				b.WriteString(fileCompanionInlineMarkdownToTeX(part))
			}
			b.WriteString("\n\\end{quote}")
			doc.block(b.String())
			i = j - 1
			continue
		}
		if table, next, ok := fileCompanionTableToTeX(lines, i); ok {
			doc.block(table)
			i = next - 1
			continue
		}
		if text, ok := fileCompanionUnorderedItem(trim); ok {
			doc.item("itemize", fileCompanionInlineMarkdownToTeX(text))
			continue
		}
		if text, ok := fileCompanionOrderedItem(trim); ok {
			doc.item("enumerate", fileCompanionInlineMarkdownToTeX(text))
			continue
		}
		doc.block(fileCompanionInlineMarkdownToTeX(trim))
	}
	if inMath {
		flush()
	}
	doc.closeList()
	return doc.String()
}

// texSink emits paragraphs and keeps a list open across consecutive items.
type texSink struct {
	b    strings.Builder
	list string
}

func (d *texSink) block(s string) {
	d.closeList()
	if strings.TrimSpace(s) == "" {
		return
	}
	if d.b.Len() > 0 {
		d.b.WriteByte('\n')
	}
	d.b.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		d.b.WriteByte('\n')
	}
}

func (d *texSink) item(kind, text string) {
	if d.list != kind {
		d.closeList()
		if d.b.Len() > 0 && !strings.HasSuffix(d.b.String(), "\n\n") {
			if !strings.HasSuffix(d.b.String(), "\n") {
				d.b.WriteByte('\n')
			}
			d.b.WriteByte('\n')
		}
		d.b.WriteString("\\begin{" + kind + "}\n")
		d.list = kind
	}
	if strings.HasPrefix(text, "[") {
		text = `\relax ` + text
	}
	d.b.WriteString("\\item " + text + "\n")
}

func (d *texSink) closeList() {
	if d.list == "" {
		return
	}
	d.b.WriteString("\\end{" + d.list + "}\n")
	d.list = ""
}

func (d *texSink) String() string {
	return strings.TrimSpace(d.b.String())
}

func fileCompanionTeXFenceCloses(line, opener string) bool {
	marker, rest, ok := fileCompanionSplitFence(strings.TrimSpace(line))
	if !ok || opener == "" {
		return false
	}
	return marker[0] == opener[0] && len(marker) >= len(opener) && strings.TrimSpace(rest) == ""
}

func fileCompanionTeXRule(trim string) bool {
	if len(trim) < 3 {
		return false
	}
	first, _ := utf8.DecodeRuneInString(trim)
	if first != '-' && first != '*' && first != '_' {
		return false
	}
	for _, r := range trim {
		if r != first {
			return false
		}
	}
	return true
}

func fileCompanionHeadingToTeX(trim string) (string, bool) {
	n := 0
	for n < len(trim) && trim[n] == '#' {
		n++
	}
	if n < 1 || n > 6 || n >= len(trim) || trim[n] != ' ' {
		return "", false
	}
	body := fileCompanionTeXHeadingBody(trim[n+1:])
	if strings.TrimSpace(body) == "" {
		return "", false
	}
	cmd := []string{"", "section", "subsection", "subsubsection", "paragraph", "subparagraph", "subparagraph"}[n]
	return fmt.Sprintf("\\%s*{%s}", cmd, fileCompanionInlineMarkdownToTeX(body)), true
}

func fileCompanionTeXHeadingBody(text string) string {
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
	return strings.TrimSpace(text)
}

func fileCompanionUnorderedItem(trim string) (string, bool) {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trim, prefix) {
			return strings.TrimPrefix(trim, prefix), true
		}
	}
	return "", false
}

func fileCompanionOrderedItem(trim string) (string, bool) {
	i := 0
	for i < len(trim) && trim[i] >= '0' && trim[i] <= '9' {
		i++
	}
	if i == 0 || i+2 > len(trim) || trim[i] != '.' || trim[i+1] != ' ' {
		return "", false
	}
	return trim[i+2:], true
}

func fileCompanionImageLineToTeX(trim string) (string, bool) {
	if !strings.HasPrefix(trim, "![") {
		return "", false
	}
	rel := strings.Index(trim, "](")
	if rel < 2 || !strings.HasSuffix(trim, ")") {
		return "", false
	}
	alt := trim[2:rel]
	path := strings.TrimSpace(trim[rel+2 : len(trim)-1])
	if path == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("\\begin{center}\n")
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		b.WriteString("\\url{")
		b.WriteString(path)
		b.WriteString("}\n")
	} else {
		b.WriteString("\\includegraphics[width=\\linewidth]{")
		b.WriteString(fileCompanionTeXFilename(strings.ReplaceAll(path, `\`, "/")))
		b.WriteString("}\n")
	}
	if strings.TrimSpace(alt) != "" {
		b.WriteString("{\\small ")
		b.WriteString(fileCompanionInlineMarkdownToTeX(alt))
		b.WriteString("}\n")
	}
	b.WriteString("\\end{center}")
	return b.String(), true
}

func fileCompanionTeXFilename(path string) string {
	path = strings.ReplaceAll(path, "%", `\%`)
	path = strings.ReplaceAll(path, "#", `\#`)
	if strings.ContainsAny(path, " {}") {
		return `"` + path + `"`
	}
	return path
}

func fileCompanionTableToTeX(lines []string, start int) (string, int, bool) {
	if start+1 >= len(lines) {
		return "", start, false
	}
	headerLine := strings.TrimSpace(lines[start])
	sepLine := strings.TrimSpace(lines[start+1])
	if !fileCompanionTableRow(headerLine) || !fileCompanionTableSep(sepLine) {
		return "", start, false
	}
	headers := fileCompanionTableCells(headerLine)
	if len(headers) == 0 {
		return "", start, false
	}
	var rows [][]string
	next := start + 2
	for next < len(lines) {
		rowLine := strings.TrimSpace(lines[next])
		if !fileCompanionTableRow(rowLine) {
			break
		}
		cells := fileCompanionTableCells(rowLine)
		if len(cells) == 0 {
			break
		}
		rows = append(rows, cells)
		next++
	}
	if len(rows) == 0 {
		return "", start, false
	}
	cols := len(headers)
	var b strings.Builder
	b.WriteString("\\begin{center}\n\\begin{tabular}{")
	for i := 0; i < cols; i++ {
		b.WriteByte('l')
	}
	b.WriteString("}\n\\toprule\n")
	b.WriteString(fileCompanionTableTeXRow(headers, cols))
	b.WriteString(" \\\\\n\\midrule\n")
	for _, row := range rows {
		b.WriteString(fileCompanionTableTeXRow(row, cols))
		b.WriteString(" \\\\\n")
	}
	b.WriteString("\\bottomrule\n\\end{tabular}\n\\end{center}")
	return b.String(), next, true
}

func fileCompanionTableTeXRow(cells []string, cols int) string {
	parts := make([]string, cols)
	for i := 0; i < cols; i++ {
		if i < len(cells) {
			parts[i] = fileCompanionInlineMarkdownToTeX(cells[i])
		}
	}
	return strings.Join(parts, " & ")
}

func fileCompanionTableRow(line string) bool {
	return strings.Count(line, "|") >= 2
}

func fileCompanionTableSep(line string) bool {
	cells := fileCompanionTableCells(line)
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

func fileCompanionTableCells(line string) []string {
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

// fileCompanionTeXEndsDisplayMath reports a line that must not stay inside a
// display block: a heading, a chapter stamp, an uncertain line, or a picture.
func fileCompanionTeXEndsDisplayMath(line, trim string) bool {
	if fileCompanionTeXHeading(trim) {
		return true
	}
	if _, _, prose, ok := fileCompanionCanonicalStampLine(trim); ok && !fileCompanionFormulaBody(prose) {
		return true
	}
	if strings.Contains(line, fileCompanionErrOCRUncertain) || strings.Contains(line, "识别不确定") {
		return true
	}
	return strings.HasPrefix(trim, "![") && strings.Contains(trim, "](")
}

func fileCompanionTeXHeading(trim string) bool {
	n := 0
	for n < len(trim) && trim[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(trim) && trim[n] == ' '
}

var (
	fileCompanionCodeRe      = regexp.MustCompile("`([^`]+)`")
	fileCompanionBoldRe      = regexp.MustCompile(`\*\*(.+?)\*\*`)
	fileCompanionUnderBoldRe = regexp.MustCompile(`__(.+?)__`)
	fileCompanionItalicRe    = regexp.MustCompile(`\*(.+?)\*`)
	fileCompanionLinkRe      = regexp.MustCompile(`(^|[^!])\[([^\]\n]+)\]\(([^)\s]+)\)`)
)

type texSlot struct {
	kind byte
	a, b string
}

func (s texSlot) render(slots []texSlot) string {
	switch s.kind {
	case 'C':
		return `\texttt{` + fileCompanionEscapeTeX(s.a) + `}`
	case 'M':
		return `\(` + s.a + `\)`
	case 'B':
		return `\textbf{` + fileCompanionExpandTeXSlots(s.a, slots) + `}`
	case 'I':
		return `\emph{` + fileCompanionExpandTeXSlots(s.a, slots) + `}`
	case 'L':
		label := s.b
		if strings.TrimSpace(label) == "" {
			label = s.a
		}
		return `\href{` + fileCompanionEscapeTeX(s.a) + `}{` + fileCompanionExpandTeXSlots(label, slots) + `}`
	default:
		return ""
	}
}

func fileCompanionMarkSlot(slots *[]texSlot, kind byte, a, b string) string {
	*slots = append(*slots, texSlot{kind: kind, a: a, b: b})
	return fmt.Sprintf("\x00%d\x00", len(*slots)-1)
}

// fileCompanionInlineMarkdownToTeX converts one line of prose.
// Code and math are taken out before emphasis, so a star inside `code` or
// $x^*$ is not a Markdown marker. What remains is escaped for text mode.
func fileCompanionInlineMarkdownToTeX(line string) string {
	var slots []texSlot
	line = fileCompanionCodeRe.ReplaceAllStringFunc(line, func(m string) string {
		return fileCompanionMarkSlot(&slots, 'C', m[1:len(m)-1], "")
	})
	line = fileCompanionTakeInlineMath(line, func(inner string) string {
		return fileCompanionMarkSlot(&slots, 'M', inner, "")
	})
	line = fileCompanionMarkEmphasis(line, &slots)
	return fileCompanionExpandTeXSlots(line, slots)
}

func fileCompanionMarkEmphasis(line string, slots *[]texSlot) string {
	line = fileCompanionMarkWrapped(line, slots, fileCompanionBoldRe, 'B', 2)
	line = fileCompanionMarkWrapped(line, slots, fileCompanionUnderBoldRe, 'B', 2)
	line = fileCompanionMarkWrapped(line, slots, fileCompanionItalicRe, 'I', 1)
	return fileCompanionMarkLinks(line, slots)
}

func fileCompanionMarkWrapped(line string, slots *[]texSlot, re *regexp.Regexp, kind byte, edge int) string {
	return re.ReplaceAllStringFunc(line, func(m string) string {
		inner := m[edge : len(m)-edge]
		if kind == 'B' {
			inner = fileCompanionMarkWrapped(inner, slots, fileCompanionItalicRe, 'I', 1)
			inner = fileCompanionMarkLinks(inner, slots)
		}
		if kind == 'I' {
			inner = fileCompanionMarkLinks(inner, slots)
		}
		return fileCompanionMarkSlot(slots, kind, inner, "")
	})
}

func fileCompanionMarkLinks(line string, slots *[]texSlot) string {
	return fileCompanionLinkRe.ReplaceAllStringFunc(line, func(m string) string {
		parts := fileCompanionLinkRe.FindStringSubmatch(m)
		if len(parts) != 4 {
			return m
		}
		return parts[1] + fileCompanionMarkSlot(slots, 'L', parts[3], parts[2])
	})
}

// fileCompanionTakeInlineMath lifts $...$ and $$...$$ out of a sentence.
// A whole-line display block is assembled before this scanner runs.
func fileCompanionTakeInlineMath(line string, store func(inner string) string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '$' {
			b.WriteByte(line[i])
			i++
			continue
		}
		if i+1 < len(line) && line[i+1] == '$' {
			rest := line[i+2:]
			end := strings.Index(rest, "$$")
			if end > 0 {
				inner := strings.TrimSpace(rest[:end])
				if inner != "" {
					b.WriteString(store(inner))
					i += 2 + end + 2
					continue
				}
			}
		}
		end := strings.IndexByte(line[i+1:], '$')
		if end < 0 {
			b.WriteByte(line[i])
			i++
			continue
		}
		end = i + 1 + end
		inner := strings.TrimSpace(line[i+1 : end])
		if inner == "" {
			b.WriteByte(line[i])
			i++
			continue
		}
		b.WriteString(store(inner))
		i = end + 1
	}
	return b.String()
}

func fileCompanionExpandTeXSlots(s string, slots []texSlot) string {
	s = fileCompanionEscapeTeXKeepSlots(s)
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0 {
			j := strings.IndexByte(s[i+1:], 0)
			if j >= 0 {
				if n, err := strconv.Atoi(s[i+1 : i+1+j]); err == nil && n >= 0 && n < len(slots) {
					b.WriteString(slots[n].render(slots))
					i += j + 2
					continue
				}
				b.WriteString(s[i : i+2+j])
				i += j + 2
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func fileCompanionEscapeTeXKeepSlots(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0 {
			j := strings.IndexByte(s[i+1:], 0)
			if j >= 0 {
				b.WriteString(s[i : i+2+j])
				i += j + 2
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		b.WriteString(fileCompanionEscapeTeX(string(r)))
		i += size
	}
	return b.String()
}

func fileCompanionEscapeTeX(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\textbackslash{}`)
		case '{', '}', '$', '&', '%', '#', '_':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '~':
			b.WriteString(`\textasciitilde{}`)
		case '^':
			b.WriteString(`\textasciicircum{}`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
