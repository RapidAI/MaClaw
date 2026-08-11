package main

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestExtractTextFromFileUsesSharedOfficeRouteForDOCX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.docx")
	writeWorkflowResumeDOCX(t, path, "OfficeRead shared document extraction")

	text, err := extractTextFromFile(path)
	if err != nil {
		t.Fatalf("extract workflow DOCX: %v", err)
	}
	if !strings.Contains(text, "OfficeRead shared document extraction") {
		t.Fatalf("workflow extraction lost DOCX body: %q", text)
	}
}

func TestExtractTextFromFileRejectsOversizedBeforeAnyParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-resume.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(agent.MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = extractTextFromFile(path)
	if err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("oversized workflow file error = %v", err)
	}
}

func TestExtractTextFromFileSanitizesOfficeParserDetail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "customer-private-resume.docx")
	if err := os.WriteFile(path, []byte("not an Office document"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := extractTextFromFile(path)
	if err == nil || err.Error() != "文档文本提取被安全、版本或资源策略拒绝" {
		t.Fatalf("unexpected workflow error = %v", err)
	}
	if strings.Contains(err.Error(), filepath.Base(path)) || strings.Contains(err.Error(), "ZIP") {
		t.Fatalf("workflow parser error leaked file or parser detail: %v", err)
	}
}

func TestExtractTextFromFileRejectsContainerDisguisedAsPlainText(t *testing.T) {
	for _, ext := range []string{".txt", ".unknown"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised"+ext)
			if err := os.WriteFile(path, []byte("PK\x03\x04not a valid Office archive"), 0o600); err != nil {
				t.Fatal(err)
			}
			text, err := extractTextFromFile(path)
			if err == nil || err.Error() != "文档文本提取被安全、版本或资源策略拒绝" || text != "" {
				t.Fatalf("disguised container result = text=%q err=%v", text, err)
			}
		})
	}
}

func TestExtractTextFromFileDoesNotFallbackForEncryptedPDF(t *testing.T) {
	if !officeExtractionMustStayClosed(agent.ErrOfficeReadEncryptedContainer) {
		t.Fatal("encrypted Office error must block workflow PDF fallback")
	}
	if officeExtractionMustStayClosed(errors.New("ordinary PDF parser failure")) {
		t.Fatal("ordinary PDF parser failure must retain the existing quality fallback")
	}
}

func TestLimitWorkflowExtractedTextUsesOfficeReadBoundary(t *testing.T) {
	input := strings.Repeat("文", agent.MaxOfficeReadTextRunes+1)
	if got := len([]rune(limitWorkflowExtractedText(input))); got != agent.MaxOfficeReadTextRunes {
		t.Fatalf("workflow text limit = %d, want %d", got, agent.MaxOfficeReadTextRunes)
	}
}

func writeWorkflowResumeDOCX(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	document, err := archive.Create("word/document.xml")
	if err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := document.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`)); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
