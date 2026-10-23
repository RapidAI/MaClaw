package guiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
	gopdf2 "github.com/VantageDataChat/GoPDF2"
)

func TestAttachmentOpeningPreviewCountContinuesWhileSparse(t *testing.T) {
	cover := "封面"
	dense := strings.Repeat("正文", 50)
	if got := attachmentOpeningPreviewCount([]string{cover, dense, "后面不再带上"}); got != 2 {
		t.Fatalf("count = %d, want 2 (sparse cover, then the first full page)", got)
	}
	if got := attachmentOpeningPreviewCount([]string{cover, "仍少", dense}); got != 3 {
		t.Fatalf("count = %d, want 3", got)
	}
	if got := attachmentOpeningPreviewCount([]string{dense, cover}); got != 1 {
		t.Fatalf("count = %d, want 1 when the first page already has text", got)
	}
	if got := attachmentOpeningPreviewCount(nil); got != 0 {
		t.Fatalf("count = %d, want 0", got)
	}
}

func TestAttachmentOpeningPreviewCountCapsSparseRun(t *testing.T) {
	pages := make([]string, attachmentPreviewMaxPages+3)
	for i := range pages {
		pages[i] = "x"
	}
	if got := attachmentOpeningPreviewCount(pages); got != attachmentPreviewMaxPages {
		t.Fatalf("count = %d, want cap %d", got, attachmentPreviewMaxPages)
	}
}

func TestSlidePlainTextJoinsRunsAndCells(t *testing.T) {
	slide := pptx.Slide{
		Shapes: []pptx.Shape{
			{Text: &pptx.TextBody{Paragraphs: []pptx.Paragraph{{Runs: []pptx.TextRun{{Text: "标题"}}}}}},
			{Table: &pptx.TableData{Rows: []pptx.TableRow{{Cells: []pptx.TableCell{{Text: "光电流"}}}}}},
		},
	}
	got := slidePlainText(slide)
	if !strings.Contains(got, "标题") || !strings.Contains(got, "光电流") {
		t.Fatalf("slide text = %q", got)
	}
	if !attachmentPreviewPageIsSparse("标题") {
		t.Fatal("a title-only slide should count as little text")
	}
}

func TestAIAssistantAttachmentOpeningPreviewPDFIncludesNextPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensor.pdf")
	dense := strings.Repeat("Photocurrent sample row. ", 6)
	writeOpeningPreviewPDF(t, path, []string{"Cover", dense})

	got, err := (*App)(nil).AIAssistantAttachmentOpeningPreview(path)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if got.Kind != "pdf" || !strings.Contains(got.PreviewURL, taskResultPreviewHTTPPath) {
		t.Fatalf("preview = %+v", got)
	}
	if len(got.Pages) != 2 {
		t.Fatalf("pages = %d, want the sparse cover and the next page", len(got.Pages))
	}
	if !strings.Contains(got.Pages[0].Text, "Cover") || !strings.Contains(got.Pages[1].Text, "Photocurrent") {
		t.Fatalf("page text = %#v", []string{got.Pages[0].Text, got.Pages[1].Text})
	}
	if got.Pages[0].Index != 1 || got.Pages[1].Index != 2 {
		t.Fatalf("indexes = %d,%d", got.Pages[0].Index, got.Pages[1].Index)
	}
	if got.Pages[0].Width <= 0 || got.Pages[0].Height <= 0 {
		t.Fatalf("page size = %v x %v", got.Pages[0].Width, got.Pages[0].Height)
	}
	token := strings.TrimPrefix(got.PreviewURL, taskResultPreviewHTTPPath+"?t=")
	taskResultPreviewMu.Lock()
	lease := taskResultPreviewLeases[token]
	taskResultPreviewMu.Unlock()
	if time.Until(lease.expires) < time.Hour {
		t.Fatalf("attachment preview lease expires too soon: %v", time.Until(lease.expires))
	}
	if _, ok := lookupTaskResultPreviewPath(token); !ok {
		t.Fatal("expected the preview lease to stay readable")
	}
	taskResultPreviewMu.Lock()
	after := taskResultPreviewLeases[token]
	taskResultPreviewMu.Unlock()
	if after.expires.Before(lease.expires) {
		t.Fatal("serving the preview shortened the attachment lease")
	}
}

func TestAIAssistantAttachmentOpeningPreviewPDFStopsOnFirstFullPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "full.pdf")
	dense := strings.Repeat("Photocurrent sample row. ", 6)
	writeOpeningPreviewPDF(t, path, []string{dense, "This later page stays out of the preview."})

	got, err := (*App)(nil).AIAssistantAttachmentOpeningPreview(path)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(got.Pages) != 1 {
		t.Fatalf("pages = %d, want only the first full page", len(got.Pages))
	}
	if strings.Contains(got.Pages[0].Text, "later page") {
		t.Fatalf("first page text included the second page: %q", got.Pages[0].Text)
	}
}

func TestAttachmentPreviewSizeLimitUsesShotBufferForPDF(t *testing.T) {
	const wantPDF int64 = 128 << 20
	if attachmentPreviewPDFMaxBytes != wantPDF {
		t.Fatalf("shot buffer = %d, want %d", attachmentPreviewPDFMaxBytes, wantPDF)
	}
	if attachmentPreviewPDFMaxBytes <= agent.MaxOfficeReadFileBytes {
		t.Fatalf("shot buffer %d must sit above the text cap %d", attachmentPreviewPDFMaxBytes, agent.MaxOfficeReadFileBytes)
	}
	if attachmentPreviewPDFMaxBytes >= taskResultPreviewMediaMaxBytes {
		t.Fatalf("shot buffer %d must stay under the viewer range cap %d", attachmentPreviewPDFMaxBytes, taskResultPreviewMediaMaxBytes)
	}
	if got := attachmentPreviewSizeLimit(`C:\docs\scan.pdf`); got != attachmentPreviewPDFMaxBytes {
		t.Fatalf("pdf limit = %d, want the shot buffer %d", got, attachmentPreviewPDFMaxBytes)
	}
	if got := attachmentPreviewSizeLimit(`C:\docs\scan.PDF`); got != attachmentPreviewPDFMaxBytes {
		t.Fatalf("PDF limit = %d, want the shot buffer %d", got, attachmentPreviewPDFMaxBytes)
	}
	if got := attachmentPreviewSizeLimit(`C:\docs\note.docx`); got != agent.MaxOfficeReadFileBytes {
		t.Fatalf("docx limit = %d, want the text cap %d", got, agent.MaxOfficeReadFileBytes)
	}
	if got := attachmentPreviewSizeLimit(`C:\docs\deck.pptx`); got != agent.MaxOfficeReadFileBytes {
		t.Fatalf("pptx limit = %d, want the text cap %d", got, agent.MaxOfficeReadFileBytes)
	}
}

func TestAIAssistantAttachmentOpeningPreviewPDFAboveTextCapStillLeasesPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := (*App)(nil).AIAssistantAttachmentOpeningPreview(path)
	if err != nil {
		t.Fatalf("preview above the text cap: %v", err)
	}
	if got.Kind != "pdf" || !strings.Contains(got.PreviewURL, taskResultPreviewHTTPPath) {
		t.Fatalf("preview = %+v", got)
	}
	if len(got.Pages) != 1 || got.Pages[0].Index != 1 {
		t.Fatalf("pages = %+v, want page 1 only", got.Pages)
	}
	if strings.TrimSpace(got.Pages[0].Text) != "" {
		t.Fatalf("text = %q, a file past the text cap must not be parsed", got.Pages[0].Text)
	}
}

func TestAIAssistantAttachmentOpeningPreviewPDFAboveShotBufferRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(attachmentPreviewPDFMaxBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = (*App)(nil).AIAssistantAttachmentOpeningPreview(path)
	if err == nil || !strings.Contains(err.Error(), "文件过大") {
		t.Fatalf("oversized pdf error = %v", err)
	}
}

func TestAIAssistantAttachmentOpeningPreviewRejectsOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (*App)(nil).AIAssistantAttachmentOpeningPreview(path); err == nil || !strings.Contains(err.Error(), "无法预览") {
		t.Fatalf("txt error = %v", err)
	}
	if _, err := (*App)(nil).AIAssistantAttachmentOpeningPreview(filepath.Join(t.TempDir(), "missing.pdf")); err == nil {
		t.Fatal("missing pdf should fail")
	}
}

func writeOpeningPreviewPDF(t *testing.T, path string, pages []string) {
	t.Helper()
	font := `C:\Windows\Fonts\arial.ttf`
	if _, err := os.Stat(font); err != nil {
		t.Skip("arial.ttf not available")
	}
	pdf := &gopdf2.GoPdf{}
	pdf.Start(gopdf2.Config{PageSize: *gopdf2.PageSizeA4})
	if err := pdf.AddTTFFont("arial", font); err != nil {
		t.Fatal(err)
	}
	for _, page := range pages {
		pdf.AddPage()
		if err := pdf.SetFont("arial", "", 12); err != nil {
			t.Fatal(err)
		}
		y := 72.0
		for _, line := range wrapPreviewPDFLines(page) {
			pdf.SetXY(48, y)
			if err := pdf.Cell(&gopdf2.Rect{W: 500, H: 18}, line); err != nil {
				t.Fatal(err)
			}
			y += 20
			if y > 760 {
				break
			}
		}
	}
	if err := pdf.WritePdf(path); err != nil {
		t.Fatal(err)
	}
}

func wrapPreviewPDFLines(page string) []string {
	page = strings.TrimSpace(page)
	if page == "" {
		return []string{""}
	}
	const width = 48
	var lines []string
	for len(page) > 0 {
		if len(page) <= width {
			lines = append(lines, page)
			break
		}
		lines = append(lines, page[:width])
		page = page[width:]
	}
	return lines
}
