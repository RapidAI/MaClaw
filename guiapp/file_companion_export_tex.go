package guiapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
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
	document, assets := fileCompanionRenderTeX(body)
	// Pictures land first. A .tex that names a file we failed to write would
	// still look like a finished export.
	dir := filepath.Dir(dest)
	for _, asset := range assets {
		name := filepath.Base(asset.name)
		if name == "" || name == "." || name != asset.name {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), asset.data, 0o644); err != nil {
			return "", fmt.Errorf("写入 LaTeX 失败: %w", err)
		}
	}
	if err := os.WriteFile(dest, []byte(document), 0o644); err != nil {
		return "", fmt.Errorf("写入 LaTeX 失败: %w", err)
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
	document, _ := fileCompanionRenderTeX(markdown)
	return document
}

// texAsset is one picture decoded out of a data URL. It is written beside the
// .tex file; \includegraphics cannot read a data URL.
type texAsset struct {
	name string
	data []byte
}

func fileCompanionRenderTeX(markdown string) (string, []texAsset) {
	var assets []texAsset
	body := strings.TrimSpace(fileCompanionMarkdownBodyToTeX(markdown, &assets))
	return fileCompanionTeXDocumentHead + body + "\n\\end{document}\n", assets
}

func fileCompanionMarkdownBodyToTeX(markdown string, assets *[]texAsset) string {
	lines := strings.Split(markdown, "\n")
	var doc texSink
	var math []string
	inMath := false
	mathClose := ""
	// region stays set until the matching closer. A sentence inside the block
	// is written as text, and a formula after that sentence is still math.
	region := false
	flush := func() {
		body := fileCompanionSanitizeMath(strings.TrimSpace(strings.Join(math, "\n")), false)
		math = nil
		inMath = false
		if body == "" {
			return
		}
		doc.block(fileCompanionTeXDisplay(body))
	}
	finish := func() {
		flush()
		region = false
		mathClose = ""
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		mathLine := fileCompanionTeXMathLine(trim)
		if region && !inMath && trim != "" {
			if fileCompanionDisplayMathWhole(mathLine) && fileCompanionDisplayMathCloses(mathLine, mathClose) {
				region = false
				mathClose = ""
			} else if fileCompanionDisplayMathCloses(mathLine, mathClose) {
				head := fileCompanionTeXCloserHead(mathLine, mathClose)
				if head != "" && fileCompanionTeXEndsDisplayMath(head, head) {
					finish()
					line, trim, mathLine = head, head, head
				} else {
					if head != "" {
						math = append(math, head)
					}
					finish()
					continue
				}
			} else if !fileCompanionTeXEndsDisplayMath(line, trim) && fileCompanionTeXFormulaLine(mathLine) && !fileCompanionDisplayMathWhole(mathLine) {
				inMath = true
				math = append(math, mathLine)
				continue
			}
		}
		if inMath {
			if fileCompanionDisplayMathWhole(mathLine) {
				closes := fileCompanionDisplayMathCloses(mathLine, mathClose)
				flush()
				if closes || !region {
					region = false
					mathClose = ""
				}
			} else if fileCompanionDisplayMathCloses(mathLine, mathClose) {
				head := fileCompanionTeXCloserHead(mathLine, mathClose)
				if head != "" && fileCompanionTeXEndsDisplayMath(head, head) {
					finish()
					line, trim, mathLine = head, head, head
				} else {
					if head != "" {
						math = append(math, head)
					}
					finish()
					continue
				}
			} else if trim != "" && (fileCompanionTeXEndsDisplayMath(line, trim) || !fileCompanionTeXFormulaLine(mathLine)) {
				flush()
				if !region {
					mathClose = ""
				}
			} else {
				if trim != "" {
					body := trim
					if mathLine != trim {
						body = mathLine
					}
					math = append(math, body)
				}
				continue
			}
		}
		if trim == "" {
			doc.blank()
			continue
		}
		if strings.Contains(line, fileCompanionErrOCRUncertain) || strings.Contains(line, "识别不确定") {
			doc.block(fileCompanionEscapeTeX(trim))
			continue
		}
		if img, ok := fileCompanionImageLineToTeX(trim, assets); ok {
			doc.block(img)
			continue
		}
		if inner, ok := fileCompanionTeXWholeInner(mathLine); ok {
			fileCompanionTeXWriteFormula(&doc, inner)
			continue
		}
		if closeTok, ok := fileCompanionDisplayMathOpens(mathLine); ok {
			rest := ""
			if len(mathLine) > 2 {
				rest = strings.TrimSpace(mathLine[2:])
			}
			// A $$ with no closer and no formula after it must not swallow the
			// rest of the answer. Chinese in math mode is missing from the page.
			realCloser := fileCompanionTeXRealCloserAhead(lines, i, closeTok)
			if !realCloser && !fileCompanionTeXEnderAhead(lines, i, closeTok) && !fileCompanionTeXFormulaAhead(lines, i, closeTok) {
				fileCompanionTeXWriteFormula(&doc, rest)
				continue
			}
			inMath = true
			region = realCloser
			mathClose = closeTok
			if rest == "" {
				continue
			}
			if fileCompanionDisplayMathCloses(rest, closeTok) {
				head := fileCompanionTeXCloserHead(rest, closeTok)
				if head != "" && fileCompanionTeXEndsDisplayMath(head, head) {
					finish()
					doc.block(fileCompanionInlineMarkdownToTeX(head))
					continue
				}
				if head != "" {
					math = append(math, head)
				}
				finish()
				continue
			}
			if fileCompanionTeXEndsDisplayMath(rest, rest) || !fileCompanionTeXFormulaLine(rest) {
				inMath = false
				if !region {
					mathClose = ""
				}
				doc.block(fileCompanionInlineMarkdownToTeX(rest))
				continue
			}
			math = append(math, rest)
			continue
		}
		if trim == "$$" || trim == `\[` || trim == `\]` {
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
			body := strings.ReplaceAll(strings.Join(code, "\n"), "\r", "")
			body = strings.ReplaceAll(body, `\end{verbatim}`, `\end{verbatim }`)
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
			var quoted []string
			for _, part := range parts {
				if part == "" {
					continue
				}
				quoted = append(quoted, fileCompanionInlineMarkdownToTeX(part))
			}
			doc.block("\\begin{quote}\n" + fileCompanionJoinProse(quoted) + "\n\\end{quote}")
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
		doc.proseLine(fileCompanionInlineMarkdownToTeX(trim))
	}
	if inMath {
		flush()
	}
	return doc.String()
}

// texSink emits paragraphs and keeps a list open across consecutive items.
// Consecutive prose lines are one paragraph. A blank line starts the next one.
// nl counts trailing newlines so a paragraph break does not copy the buffer.
type texSink struct {
	b     strings.Builder
	list  string
	prose []string
	nl    int
}

func (d *texSink) write(s string) {
	if s == "" {
		return
	}
	d.b.WriteString(s)
	d.nl = 0
	if strings.HasSuffix(s, "\n") {
		d.nl = 1
		if strings.HasSuffix(s, "\n\n") {
			d.nl = 2
		}
	}
}

func (d *texSink) breakBefore() {
	if d.b.Len() == 0 {
		return
	}
	for d.nl < 2 {
		d.b.WriteByte('\n')
		d.nl++
	}
}

func (d *texSink) flushProse() {
	if len(d.prose) == 0 {
		return
	}
	d.breakBefore()
	d.write(fileCompanionJoinProse(d.prose))
	d.write("\n")
	d.prose = nil
}

func (d *texSink) proseLine(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	d.closeList()
	d.prose = append(d.prose, s)
}

func (d *texSink) blank() {
	d.flushProse()
	d.closeList()
}

func (d *texSink) block(s string) {
	d.flushProse()
	d.closeList()
	if strings.TrimSpace(s) == "" {
		return
	}
	d.breakBefore()
	d.write(s)
	if d.nl == 0 {
		d.write("\n")
	}
}

func (d *texSink) item(kind, text string) {
	d.flushProse()
	if d.list != kind {
		d.closeList()
		d.breakBefore()
		d.write("\\begin{" + kind + "}\n")
		d.list = kind
	}
	if strings.HasPrefix(text, "[") {
		text = `\relax ` + text
	}
	d.write("\\item " + text + "\n")
}

func (d *texSink) closeList() {
	if d.list == "" {
		return
	}
	d.write("\\end{" + d.list + "}\n")
	d.list = ""
}

func (d *texSink) String() string {
	d.flushProse()
	d.closeList()
	return strings.TrimSpace(d.b.String())
}

// fileCompanionJoinProse keeps one paragraph. A newline in TeX is a space, so
// a CJK line would gain a gap; "%" eats that newline. Latin lines keep a space.
func fileCompanionJoinProse(lines []string) string {
	var b strings.Builder
	prev := ""
	for _, line := range lines {
		if line == "" {
			continue
		}
		if prev != "" {
			b.WriteString(fileCompanionProseBreak(prev, line))
		}
		b.WriteString(line)
		prev = line
	}
	return b.String()
}

func fileCompanionProseBreak(prev, next string) string {
	pr, _ := utf8.DecodeLastRuneInString(strings.TrimRight(prev, " "))
	nr, _ := utf8.DecodeRuneInString(strings.TrimLeft(next, " "))
	if fileCompanionCJKRune(pr) && fileCompanionCJKRune(nr) {
		return "%\n"
	}
	return " %\n"
}

func fileCompanionCJKRune(r rune) bool {
	if unicode.Is(unicode.Han, r) {
		return true
	}
	return (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
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

func fileCompanionImageLineToTeX(trim string, assets *[]texAsset) (string, bool) {
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
	switch {
	case strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://"):
		b.WriteString("\\url{")
		b.WriteString(fileCompanionTeXURL(path))
		b.WriteString("}")
		if strings.TrimSpace(alt) != "" {
			b.WriteString(`\\`)
		}
		b.WriteByte('\n')
	case strings.HasPrefix(path, "data:image/"):
		if assets == nil {
			if strings.TrimSpace(alt) == "" {
				return "", true
			}
			return fileCompanionInlineMarkdownToTeX(alt), true
		}
		ext, data, ok := fileCompanionDecodeDataImage(path)
		if !ok {
			if strings.TrimSpace(alt) == "" {
				return "", true
			}
			return fileCompanionInlineMarkdownToTeX(alt), true
		}
		name := fileCompanionStoreImage(assets, ext, data)
		b.WriteString(fileCompanionIncludeGraphics(name, strings.TrimSpace(alt) != ""))
		b.WriteByte('\n')
	default:
		name := strings.ReplaceAll(path, `\`, "/")
		if strings.ContainsAny(name, "%#\"{}") || strings.Contains(name, "\n") {
			if strings.TrimSpace(alt) == "" {
				return fileCompanionEscapeTeX(name), true
			}
			return fileCompanionInlineMarkdownToTeX(alt) + " (" + fileCompanionEscapeTeX(name) + ")", true
		}
		b.WriteString(fileCompanionIncludeGraphics(name, strings.TrimSpace(alt) != ""))
		b.WriteByte('\n')
	}
	if strings.TrimSpace(alt) != "" {
		b.WriteString("{\\small ")
		b.WriteString(fileCompanionInlineMarkdownToTeX(alt))
		b.WriteString("}\n")
	}
	b.WriteString("\\end{center}")
	return b.String(), true
}

func fileCompanionIncludeGraphics(name string, breakAfter bool) string {
	body := `\includegraphics[width=\linewidth]{` + name + `}`
	if breakAfter {
		body += `\\`
	}
	return `\IfFileExists{` + name + `}{` + body + `}{}`
}

func fileCompanionDecodeDataImage(raw string) (string, []byte, bool) {
	comma := strings.Index(raw, ",")
	if comma < 0 || !strings.Contains(strings.ToLower(raw[:comma]), ";base64") {
		return "", nil, false
	}
	payload := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, raw[comma+1:])
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return "", nil, false
		}
	}
	ext, ok := fileCompanionImageMagic(data)
	if !ok {
		return "", nil, false
	}
	return ext, data, true
}

// fileCompanionImageMagic is the graphicx formats XeLaTeX can read here.
// A data URL labeled png but holding SVG or text would make \includegraphics
// abort the compile, because \IfFileExists only checks that the file is there.
func fileCompanionImageMagic(data []byte) (string, bool) {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png", true
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpg", true
	}
	if len(data) >= 6 && (bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a"))) {
		return "gif", true
	}
	return "", false
}

// fileCompanionStoreImage names a picture from its bytes. Two exports into
// the same folder then share one file when the picture is the same, and do
// not overwrite each other when it is not.
func fileCompanionStoreImage(assets *[]texAsset, ext string, data []byte) string {
	sum := sha256.Sum256(data)
	name := fmt.Sprintf("companion-%x.%s", sum[:8], ext)
	for _, asset := range *assets {
		if asset.name == name {
			return name
		}
	}
	*assets = append(*assets, texAsset{name: name, data: data})
	return name
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
	if strings.HasPrefix(trim, "![") && strings.Contains(trim, "](") {
		return true
	}
	// A sentence inside $$ ... $$ is not a formula. Leaving it there puts
	// Chinese into math mode and the document does not compile. A formula
	// that writes 中文 inside \text still has a backslash and stays.
	return fileCompanionTeXHanProse(trim)
}

func fileCompanionTeXHanProse(trim string) bool {
	han := false
	for _, r := range trim {
		if unicode.Is(unicode.Han, r) {
			han = true
			break
		}
	}
	return han && !fileCompanionFormulaBody(trim)
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
		// hyperref reads this argument after changing catcodes, so escaping
		// "_" or "%" would put a backslash into the link target.
		return `\href{` + fileCompanionTeXURL(s.a) + `}{` + fileCompanionExpandTeXSlots(label, slots) + `}`
	case 'T':
		return s.a
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
		return fileCompanionMarkSlot(&slots, 'M', fileCompanionSanitizeMath(inner, true), "")
	})
	// Markdown \$ \% and the other escaped specials are already TeX. Escaping
	// the backslash as well would print it.
	line = fileCompanionTakeTeXEscapes(line, func(tex string) string {
		return fileCompanionMarkSlot(&slots, 'T', tex, "")
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
		if i+1 < len(line) && line[i] == '\\' && (line[i+1] == '[' || line[i+1] == '(') {
			closer := `\]`
			if line[i+1] == '(' {
				closer = `\)`
			}
			rest := line[i+2:]
			if end := strings.Index(rest, closer); end > 0 {
				inner := strings.TrimSpace(rest[:end])
				if inner != "" {
					b.WriteString(store(inner))
					i += 2 + end + len(closer)
					continue
				}
			}
		}
		// A backslash before $ is markdown for a literal dollar, not math.
		if line[i] != '$' || (i > 0 && line[i-1] == '\\') {
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
		end := fileCompanionUnescapedDollar(line, i+1)
		if end < 0 {
			b.WriteByte(line[i])
			i++
			continue
		}
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

func fileCompanionUnescapedDollar(line string, start int) int {
	for start < len(line) {
		rel := strings.IndexByte(line[start:], '$')
		if rel < 0 {
			return -1
		}
		at := start + rel
		if at > 0 && line[at-1] == '\\' {
			start = at + 1
			continue
		}
		return at
	}
	return -1
}

func fileCompanionTakeTeXEscapes(line string, store func(tex string) string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) && strings.ContainsRune("$&%#_{}", rune(line[i+1])) {
			b.WriteString(store(`\` + line[i+1:i+2]))
			i += 2
			continue
		}
		b.WriteByte(line[i])
		i++
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
	b.Grow(len(s))
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
		fileCompanionWriteEscapedRune(&b, r)
		i += size
	}
	return b.String()
}

func fileCompanionEscapeTeX(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		fileCompanionWriteEscapedRune(&b, r)
	}
	return b.String()
}

func fileCompanionWriteEscapedRune(b *strings.Builder, r rune) {
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

// fileCompanionTeXURL keeps a link readable by hyperref. Newlines and a brace
// would end the argument before hyperref sees them.
func fileCompanionTeXURL(url string) string {
	url = strings.ReplaceAll(url, "\r", "")
	url = strings.ReplaceAll(url, "\n", "")
	url = strings.ReplaceAll(url, "{", "")
	return strings.ReplaceAll(url, "}", "")
}

// fileCompanionSanitizeMath escapes the characters that still start a comment,
// a parameter, or math mode. Inline math also escapes "&", which is a column
// separator in a table even inside \(...\). Display math keeps "&" so an
// aligned formula still lines up. A character that is already escaped stays.
func fileCompanionSanitizeMath(s string, inline bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == '%' || r == '#' || r == '$' || (inline && r == '&')) && (i == 0 || s[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// fileCompanionTeXDisplay wraps a formula. align and equation are already
// display environments; wrapping them in \[ \] makes amsmath reject the file.
// A formula line before \begin{align} stays in \[ \]; the environment does not.
func fileCompanionTeXDisplay(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	if !fileCompanionTeXBlockHasOuterEnv(lines) {
		return `\[` + body + `\]`
	}
	var b strings.Builder
	var chunk []string
	write := func(s string) {
		if strings.TrimSpace(s) == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(s)
	}
	flushChunk := func() {
		part := strings.TrimSpace(strings.Join(chunk, "\n"))
		chunk = nil
		if part == "" {
			return
		}
		write(`\[` + part + `\]`)
	}
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		env, ok := fileCompanionTeXOuterEnvName(line)
		if !ok {
			if line != "" {
				chunk = append(chunk, line)
			}
			continue
		}
		flushChunk()
		block := []string{line}
		if !fileCompanionTeXHasEnvEnd(line, env) {
			for i+1 < len(lines) {
				i++
				next := strings.TrimSpace(lines[i])
				if next == "" {
					continue
				}
				block = append(block, next)
				if fileCompanionTeXHasEnvEnd(next, env) {
					break
				}
			}
		}
		write(strings.Join(block, "\n"))
	}
	flushChunk()
	return strings.TrimSpace(b.String())
}

func fileCompanionTeXBlockHasOuterEnv(lines []string) bool {
	for _, line := range lines {
		if _, ok := fileCompanionTeXOuterEnvName(strings.TrimSpace(line)); ok {
			return true
		}
	}
	return false
}

func fileCompanionTeXOuterEnvName(line string) (string, bool) {
	for _, env := range []string{"equation", "align", "alignat", "gather", "multline", "flalign", "displaymath", "eqnarray"} {
		needle := `\begin{` + env
		if !strings.HasPrefix(line, needle) {
			continue
		}
		after := line[len(needle):]
		if strings.HasPrefix(after, "*}") {
			return env + "*", true
		}
		if strings.HasPrefix(after, "}") {
			return env, true
		}
	}
	return "", false
}

func fileCompanionTeXHasEnvEnd(line, env string) bool {
	return strings.Contains(line, `\end{`+env+`}`)
}

func fileCompanionTeXWriteFormula(doc *texSink, inner string) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return
	}
	if fileCompanionTeXEndsDisplayMath(inner, inner) {
		doc.block(fileCompanionInlineMarkdownToTeX(inner))
		return
	}
	doc.block(fileCompanionTeXDisplay(fileCompanionSanitizeMath(inner, false)))
}

func fileCompanionTeXWholeInner(mathLine string) (string, bool) {
	if !fileCompanionDisplayMathWhole(mathLine) || len(mathLine) < 4 {
		return "", false
	}
	return strings.TrimSpace(mathLine[2 : len(mathLine)-2]), true
}

// fileCompanionTeXMathLine drops one list marker so "- $$" and "1. \[y\]"
// are the same formula the video notes already treat as display math.
func fileCompanionTeXMathLine(trim string) string {
	trim = fileCompanionDisplayMathLine(trim)
	if text, ok := fileCompanionUnorderedItem(trim); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := fileCompanionOrderedItem(trim); ok {
		return strings.TrimSpace(text)
	}
	return trim
}

func fileCompanionTeXCloserHead(mathLine, closeTok string) string {
	if closeTok == "" || len(mathLine) <= len(closeTok) {
		return ""
	}
	return strings.TrimSpace(mathLine[:len(mathLine)-len(closeTok)])
}

// fileCompanionTeXFormulaLine is a display-math row rather than a sentence.
// A long English word keeps "see (below)" out of math mode; \delta and
// E = mc^2 stay in.
func fileCompanionTeXFormulaLine(trim string) bool {
	trim = strings.TrimSpace(trim)
	if trim == "" || fileCompanionTeXRule(trim) || fileCompanionTeXHeading(trim) {
		return false
	}
	if strings.Contains(trim, fileCompanionErrOCRUncertain) || strings.HasPrefix(trim, "![") {
		return false
	}
	if fileCompanionTeXHanProse(trim) {
		return false
	}
	// A sentence that mentions 50% or {braces} is not a formula. Commands,
	// subscripts, and a bare 50% still are.
	if strings.ContainsAny(trim, "\\&=^_") {
		return true
	}
	if strings.ContainsAny(trim, "%#{}") && !fileCompanionTeXLongWord(trim) {
		return true
	}
	if strings.ContainsAny(trim, "+-*/()[]") && !fileCompanionTeXLongWord(trim) {
		return true
	}
	if strings.ContainsAny(trim, " \t") {
		return false
	}
	for _, r := range trim {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == ',' || r == ':' || r == '\'' {
			continue
		}
		return false
	}
	return true
}

func fileCompanionTeXLongWord(trim string) bool {
	n := 0
	for _, r := range trim {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			n++
			if n >= 4 {
				return true
			}
			continue
		}
		n = 0
	}
	return false
}

func fileCompanionTeXRealCloserAhead(lines []string, idx int, closeTok string) bool {
	for j := idx + 1; j < len(lines); j++ {
		trim := strings.TrimSpace(lines[j])
		if trim == "" {
			continue
		}
		mathLine := fileCompanionTeXMathLine(trim)
		if fileCompanionDisplayMathCloses(mathLine, closeTok) || fileCompanionDisplayMathWhole(mathLine) {
			return true
		}
	}
	return false
}

func fileCompanionTeXEnderAhead(lines []string, idx int, closeTok string) bool {
	for j := idx + 1; j < len(lines); j++ {
		trim := strings.TrimSpace(lines[j])
		if trim == "" {
			continue
		}
		mathLine := fileCompanionTeXMathLine(trim)
		if fileCompanionDisplayMathCloses(mathLine, closeTok) || fileCompanionDisplayMathWhole(mathLine) {
			return false
		}
		if fileCompanionTeXEndsDisplayMath(lines[j], trim) {
			return true
		}
	}
	return false
}

func fileCompanionTeXFormulaAhead(lines []string, idx int, closeTok string) bool {
	for j := idx + 1; j < len(lines); j++ {
		trim := strings.TrimSpace(lines[j])
		if trim == "" {
			continue
		}
		mathLine := fileCompanionTeXMathLine(trim)
		if fileCompanionDisplayMathCloses(mathLine, closeTok) || fileCompanionDisplayMathWhole(mathLine) || fileCompanionTeXEndsDisplayMath(lines[j], trim) {
			return false
		}
		return fileCompanionTeXFormulaLine(mathLine)
	}
	return false
}
