package httpapi

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func TestMobileBootstrapHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	rec := httptest.NewRecorder()

	MobileBootstrapHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMobileSearchHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/search", strings.NewReader(`{"query":"status"}`))
	rec := httptest.NewRecorder()

	MobileSearchHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileSearchFormatsResultsWithCitations(t *testing.T) {
	results := []websearch.SearchResult{
		{
			Title:   "Nginx logs guide",
			URL:     "https://example.test/nginx",
			Snippet: "Check error.log and access.log first.",
		},
		{
			Title:   "",
			URL:     "https://example.test/systemd",
			Snippet: "Use journalctl for service failures.",
		},
	}

	answer := mobileSearchAnswer("nginx 502", results)
	if !strings.Contains(answer, "nginx 502") {
		t.Fatalf("answer = %q, want query", answer)
	}
	if !strings.Contains(answer, "Nginx logs guide") {
		t.Fatalf("answer = %q, want result title", answer)
	}
	if !strings.Contains(answer, "Check error.log") {
		t.Fatalf("answer = %q, want result snippet", answer)
	}

	citations := mobileSearchCitations(results)
	if len(citations) != 2 {
		t.Fatalf("len(citations) = %d, want 2", len(citations))
	}
	if citations[0]["url"] != "https://example.test/nginx" {
		t.Fatalf("first citation = %#v, want nginx url", citations[0])
	}
	if citations[1]["title"] != "https://example.test/systemd" {
		t.Fatalf("second citation = %#v, want URL title fallback", citations[1])
	}
}

func TestMobileDocumentHandlersRequireViewerToken(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{
			name:    "draft",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/drafts",
			body:    `{"title":"notice"}`,
			handler: MobileDocumentDraftHandler(nil),
		},
		{
			name:    "upload",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/upload",
			body:    "",
			handler: MobileDocumentUploadHandler(nil),
		},
		{
			name:    "draft update",
			method:  http.MethodPatch,
			path:    "/api/mobile/documents/drafts/d1",
			body:    `{"title":"notice","markdown":"body"}`,
			handler: MobileDocumentDraftUpdateHandler(nil),
		},
		{
			name:    "draft process",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/drafts/d1/process",
			body:    `{"action":"summarize"}`,
			handler: MobileDocumentProcessHandler(nil),
		},
		{
			name:    "export",
			method:  http.MethodPost,
			path:    "/api/mobile/documents/export",
			body:    `{"draft_id":"d1","format":"pdf"}`,
			handler: MobileDocumentExportHandler(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestMobileSSHAnalyzeHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/analyze", strings.NewReader(`{"output":"panic"}`))
	rec := httptest.NewRecorder()

	MobileSSHAnalyzeHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeesHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeesHandler(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(rec.Body.String(), "UNAUTHORIZED") {
		t.Fatalf("body = %s, want UNAUTHORIZED", rec.Body.String())
	}
}

func TestMobileDigitalEmployeeTaskHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ops/tasks", strings.NewReader(`{"prompt":"check disk"}`))
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskClaimHandlerRequiresWorkerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/digital-employees/ops/tasks/claim", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskClaimHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskStatusHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/digital-employees/tasks/mobve_1", nil)
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskStatusHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskUpdateHandlerRequiresWorkerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodPatch, "/api/mobile/digital-employees/tasks/mobve_1", strings.NewReader(`{"status":"done","result":"ok"}`))
	rec := httptest.NewRecorder()

	MobileDigitalEmployeeTaskUpdateHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileDigitalEmployeeTaskPayloadIncludesRemoteWorkFields(t *testing.T) {
	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	payload := mobileDigitalEmployeeTaskPayload(mobileDigitalEmployeeTaskRecord{
		TaskID:     "mobve_1",
		EmployeeID: "ops",
		Prompt:     "check disk",
		Status:     "done",
		Result:     "disk ok",
		ClaimedBy:  "machine_1",
		CreatedAt:  now,
		UpdatedAt:  now,
	})

	if payload["prompt"] != "check disk" {
		t.Fatalf("prompt = %v, want check disk", payload["prompt"])
	}
	if payload["claimed_by"] != "machine_1" {
		t.Fatalf("claimed_by = %v, want machine_1", payload["claimed_by"])
	}
	if payload["status"] != "done" || payload["result"] != "disk ok" {
		t.Fatalf("payload = %#v, want final task status and result", payload)
	}
}

func TestMobileProcessDocumentMarkdown(t *testing.T) {
	markdown := "# Incident\n\nService returned 502 for 10 minutes.\n\nNginx was restarted."

	summary := mobileProcessDocumentMarkdown("summarize", markdown)
	if !strings.Contains(summary, "# Incident 摘要") {
		t.Fatalf("summary = %q, want summary title", summary)
	}
	if !strings.Contains(summary, "Service returned 502") {
		t.Fatalf("summary = %q, want first point", summary)
	}

	formatted := mobileProcessDocumentMarkdown("format", markdown)
	if !strings.Contains(formatted, "- Service returned 502") {
		t.Fatalf("formatted = %q, want bullet formatting", formatted)
	}
}

func TestMobileDocumentUploadHandlerRejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload", nil)
	rec := httptest.NewRecorder()

	MobileDocumentUploadHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestMobileDocumentUploadStatusHandlerRequiresViewerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/documents/upload/mobparse_1", nil)
	rec := httptest.NewRecorder()

	MobileDocumentUploadStatusHandler(nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMobileSSHAnalysisPayloadDetectsDiskFull(t *testing.T) {
	payload := mobileSSHAnalysisPayload("write failed: no space left on device")

	if payload["status"] != "ready" {
		t.Fatalf("status = %v, want ready", payload["status"])
	}
	if !strings.Contains(payload["summary"].(string), "磁盘空间不足") {
		t.Fatalf("summary = %v, want disk full summary", payload["summary"])
	}
	if !strings.Contains(payload["command_draft"].(string), "df -h") {
		t.Fatalf("command_draft = %v, want df -h", payload["command_draft"])
	}
}

func TestMobileUploadedTextDraftMarkdown(t *testing.T) {
	markdown, ok := mobileDraftMarkdownFromUpload("incident.log", []byte("panic: disk full"))
	if !ok {
		t.Fatal("mobileDraftMarkdownFromUpload returned ok=false")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "```text") {
		t.Fatalf("markdown = %q, want text fence", markdown)
	}
}

func TestMobileUploadedDOCXDraftMarkdown(t *testing.T) {
	data := mobileTestZip(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Incident report</w:t></w:r></w:p><w:p><w:r><w:t>Service recovered</w:t></w:r></w:p></w:body></w:document>`,
	})

	markdown, ok := mobileDraftMarkdownFromUpload("incident.docx", data)
	if !ok {
		t.Fatal("docx upload was not parsed")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "Service recovered") {
		t.Fatalf("markdown = %q, want body text", markdown)
	}
}

func TestMobileUploadedXLSXDraftMarkdown(t *testing.T) {
	data := mobileTestZip(t, map[string]string{
		"xl/sharedStrings.xml":     `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Host</t></si><si><t>Status</t></si><si><t>api-1</t></si><si><t>ok</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row><row><c t="s"><v>2</v></c><c t="s"><v>3</v></c></row></sheetData></worksheet>`,
	})

	markdown, ok := mobileDraftMarkdownFromUpload("servers.xlsx", data)
	if !ok {
		t.Fatal("xlsx upload was not parsed")
	}
	if !strings.Contains(markdown, "Host | Status") {
		t.Fatalf("markdown = %q, want header row", markdown)
	}
	if !strings.Contains(markdown, "api-1 | ok") {
		t.Fatalf("markdown = %q, want data row", markdown)
	}
}

func TestMobileUploadedPDFDraftMarkdown(t *testing.T) {
	data := mobileRenderDraftPDF(mobileDocumentDraftRecord{
		Title:    "Incident PDF",
		Markdown: "# Incident PDF\n\nService recovered after restart.",
	})

	markdown, ok := mobileDraftMarkdownFromUpload("incident.pdf", data)
	if !ok {
		t.Fatal("pdf upload was not parsed")
	}
	if !strings.Contains(markdown, "# incident") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "Service recovered after restart.") {
		t.Fatalf("markdown = %q, want extracted PDF text", markdown)
	}
}

func TestMobileUploadedImageDraftMarkdown(t *testing.T) {
	data := mobileTestPNG(640, 480)

	markdown := mobileDraftMarkdownFromImage("screenshot.png", data)
	if !strings.Contains(markdown, "# screenshot") {
		t.Fatalf("markdown = %q, want title", markdown)
	}
	if !strings.Contains(markdown, "等待 OCR") {
		t.Fatalf("markdown = %q, want OCR pending text", markdown)
	}
	if !strings.Contains(markdown, "640 x 480") {
		t.Fatalf("markdown = %q, want dimensions", markdown)
	}
}

func mobileTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func mobileTestPNG(width, height int) []byte {
	data := make([]byte, 24)
	copy(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	copy(data[12:], []byte{'I', 'H', 'D', 'R'})
	data[16] = byte(width >> 24)
	data[17] = byte(width >> 16)
	data[18] = byte(width >> 8)
	data[19] = byte(width)
	data[20] = byte(height >> 24)
	data[21] = byte(height >> 16)
	data[22] = byte(height >> 8)
	data[23] = byte(height)
	return data
}

func TestMobileDocumentExportPayloadReadyForPDF(t *testing.T) {
	payload := mobileDocumentExportPayload(mobileDocumentExportRecord{
		JobID:  "mobexp_1",
		Format: "pdf",
		Status: "ready",
	})

	if payload["download_url"] != "/api/mobile/documents/export/mobexp_1/download" {
		t.Fatalf("download_url = %v, want ready download URL", payload["download_url"])
	}
}

func TestMobileDocumentExportPayloadReadyForWord(t *testing.T) {
	payload := mobileDocumentExportPayload(mobileDocumentExportRecord{
		JobID:  "mobexp_2",
		Format: "word",
		Status: "ready",
	})

	if payload["download_url"] != "/api/mobile/documents/export/mobexp_2/download" {
		t.Fatalf("download_url = %v, want ready download URL", payload["download_url"])
	}
}

func TestMobileRenderDraftPDF(t *testing.T) {
	data := mobileRenderDraftPDF(mobileDocumentDraftRecord{
		Title:    "Incident Report",
		Markdown: "# Incident Report\n\nService recovered.\n\n- nginx restarted",
	})

	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("pdf header = %q, want %%PDF-1.4", data[:8])
	}
	if !bytes.Contains(data, []byte("xref")) {
		t.Fatal("pdf missing xref")
	}
	if !bytes.Contains(data, []byte("trailer")) {
		t.Fatal("pdf missing trailer")
	}
}

func TestMobileRenderDraftDOCX(t *testing.T) {
	data := mobileRenderDraftDOCX(mobileDocumentDraftRecord{
		Title:    "Incident Report",
		Markdown: "# Incident Report\n\nService recovered.\n\n- nginx restarted",
	})

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader returned error: %v", err)
	}
	var documentXML string
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open document.xml: %v", err)
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read document.xml: %v", err)
		}
		documentXML = string(raw)
		break
	}
	if documentXML == "" {
		t.Fatal("docx missing word/document.xml")
	}
	if !strings.Contains(documentXML, "Incident Report") {
		t.Fatalf("document.xml = %q, want title", documentXML)
	}
	if !strings.Contains(documentXML, "Service recovered.") {
		t.Fatalf("document.xml = %q, want body", documentXML)
	}
}

func TestMobileDocumentExportStatusHandlersRequireViewerToken(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
	}{
		{
			name:    "status",
			path:    "/api/mobile/documents/export/job-1",
			handler: MobileDocumentExportStatusHandler(nil),
		},
		{
			name:    "download",
			path:    "/api/mobile/documents/export/job-1/download",
			handler: MobileDocumentExportDownloadHandler(nil),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			tt.handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}
}
