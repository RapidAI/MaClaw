package guiapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// fileCompanionSaveTeXDialog is the save dialog for a LaTeX fragment.
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

// FileCompanionExportTeX writes one answer as a LaTeX fragment.
// The default PDF click does not call this.
func (a *App) FileCompanionExportTeX(path, markdown string) (string, error) {
	return a.fileCompanionExportTeX(path, markdown, "回答.tex")
}

// FileCompanionExportChatTeX writes the whole chat as a LaTeX fragment.
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

// FileCompanionExportWebChatTeX writes a web tab's chat as a LaTeX fragment.
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
	fragment := fileCompanionMarkdownToTeX(body)
	if err := os.WriteFile(dest, []byte(fragment), 0o644); err != nil {
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

// fileCompanionMarkdownToTeX converts dollar math to LaTeX delimiters.
// A display formula is one \[...\] block, including a $$ or \[ block that
// spans lines. Lines marked 识别不确定 and picture lines are copied unchanged.
// The fragment has no document class and no document environment.
func fileCompanionMarkdownToTeX(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
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
		out = append(out, `\[`+body+`\]`)
	}
	for _, line := range lines {
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
		if strings.Contains(line, fileCompanionErrOCRUncertain) || strings.Contains(line, "识别不确定") {
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(trim, "![") && strings.Contains(trim, "](") {
			out = append(out, line)
			continue
		}
		if fileCompanionDisplayMathWhole(trim) {
			fileCompanionAppendTeXDisplay(&out, trim[2:len(trim)-2])
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
		out = append(out, fileCompanionInlineDollarToTeX(line))
	}
	if inMath {
		flush()
	}
	return strings.Join(out, "\n")
}

// fileCompanionTeXEndsDisplayMath reports a line that must not stay inside a
// display block: a heading, a chapter stamp, an uncertain line, or a picture.
func fileCompanionTeXEndsDisplayMath(line, trim string) bool {
	if strings.HasPrefix(trim, "## ") {
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

func fileCompanionAppendTeXDisplay(out *[]string, inner string) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return
	}
	*out = append(*out, `\[`+inner+`\]`)
}

func fileCompanionInlineDollarToTeX(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '$' {
			b.WriteByte(line[i])
			i++
			continue
		}
		// $$...$$ inside a sentence is one formula. A whole-line display
		// block is assembled before this scanner runs.
		if i+1 < len(line) && line[i+1] == '$' {
			rest := line[i+2:]
			end := strings.Index(rest, "$$")
			if end > 0 {
				inner := strings.TrimSpace(rest[:end])
				if inner != "" {
					b.WriteString(`\(`)
					b.WriteString(inner)
					b.WriteString(`\)`)
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
		inner := line[i+1 : end]
		if strings.Contains(inner, "\n") || inner == "" {
			b.WriteByte(line[i])
			i++
			continue
		}
		b.WriteString(`\(`)
		b.WriteString(inner)
		b.WriteString(`\)`)
		i = end + 1
	}
	return b.String()
}
