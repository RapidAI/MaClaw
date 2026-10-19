package guiapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// The companion export renders one reply as Markdown, so the PDF keeps the
// headings, lists, tables, and code fences the answer came in. Nothing here
// touches the file the reply is about; the open file only names the export.

const fileCompanionExportBrand = "MaClaw 伴读"
const fileCompanionExportFooter = "由 MaClaw 伴读导出"

// Overridable so tests can pick a destination without a desktop dialog.
var fileCompanionSavePDFDialog = func(a *App, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出为 PDF",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "PDF (*.pdf)", Pattern: "*.pdf"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// FileCompanionExportPDF writes one companion reply to a PDF at the path the
// reader picks. An empty first result means the dialog was cancelled, which is
// not an error. The Markdown answer goes in unchanged; the PDF layout is the
// same one the paper interpretation uses.
func (a *App) FileCompanionExportPDF(path, markdown string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	grant, _, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", errFileCompanionNotGranted
	}
	body := strings.TrimSpace(markdown)
	if body == "" {
		return "", errors.New("没有可导出的内容")
	}
	source := strings.TrimSpace(grant.Canonical)
	dest, err := fileCompanionSavePDFDialog(a, fileCompanionExportFileName(source))
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(dest), ".pdf") {
		dest += ".pdf"
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if info, statErr := os.Stat(source); statErr == nil && !info.IsDir() && sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	written, err := docgen.GenerateToFile(docgen.Spec{
		Title:          fileCompanionPaperSourceName(source),
		Brand:          fileCompanionExportBrand,
		Content:        body,
		FooterHint:     fileCompanionExportFooter,
		FileNamePrefix: "companion",
		PaperSize:      "A4",
	}, dest)
	if err != nil {
		return "", err
	}
	logFileCompanion("export pdf")
	return written, nil
}

// FileCompanionExportWebPDF writes one web-tab answer. It does not call fileCompanionGrant.
func (a *App) FileCompanionExportWebPDF(sessionID, markdown string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	if a.fileCompanionWebFrame(sessionID) == "" {
		return "", errors.New("web session is not open")
	}
	return a.fileCompanionWriteExportPDF("网页-回答.pdf", "网页", "", markdown, "companion", "", false)
}

func (a *App) fileCompanionWriteExportPDF(defaultName, title, subtitle, markdown, prefix, source string, refuseOverwrite bool) (string, error) {
	body := strings.TrimSpace(markdown)
	if body == "" {
		return "", errors.New("没有可导出的内容")
	}
	dest, err := fileCompanionSavePDFDialog(a, defaultName)
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(dest), ".pdf") {
		dest += ".pdf"
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	if info, statErr := os.Stat(dest); statErr == nil && info.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if refuseOverwrite && strings.TrimSpace(source) != "" {
		if info, statErr := os.Stat(source); statErr == nil && !info.IsDir() && sameFileCompanionExportPath(source, dest) {
			return "", errors.New(fileCompanionErrCannotOverwrite)
		}
	}
	written, err := docgen.GenerateToFile(docgen.Spec{
		Title:          title,
		Subtitle:       subtitle,
		Brand:          fileCompanionExportBrand,
		Content:        body,
		FooterHint:     fileCompanionExportFooter,
		FileNamePrefix: prefix,
		PaperSize:      "A4",
	}, dest)
	if err != nil {
		return "", err
	}
	return written, nil
}

// Default name for the save dialog: "<document>-回答.pdf", kept inside the
// length and character rules the paper result already follows.
func fileCompanionExportFileName(source string) string {
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, fileCompanionPaperSourceName(source))
	base = strings.Trim(base, " .")
	rs := []rune(base)
	if len(rs) > 60 {
		base = strings.TrimRight(string(rs[:60]), " .")
	}
	if strings.TrimSpace(base) == "" {
		base = "document"
	}
	return base + "-回答.pdf"
}

func sameFileCompanionExportPath(source, dest string) bool {
	source = filepath.Clean(source)
	dest = filepath.Clean(dest)
	if strings.EqualFold(source, dest) {
		return true
	}
	si, err := os.Stat(source)
	if err != nil {
		return false
	}
	di, err := os.Stat(dest)
	if err != nil {
		return false
	}
	return os.SameFile(si, di)
}
