package httpapi

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileTrustedDocumentAttachmentPublishesOwnedOriginal(t *testing.T) {
	draft := mobileDocumentDraftRecord{
		ID: "d-orig", Title: "notes", SourceFilename: "notes.txt",
		SourceContentType: "text/plain", SourceBytes: []byte("hello trusted original"),
		UpdatedAt: time.Now().UTC(),
	}
	got, ok := mobileTrustedDocumentAttachment(draft)
	if !ok || got.FileName != "notes.txt" || got.MimeType != "text/plain" {
		t.Fatalf("attachment=%#v ok=%v", got, ok)
	}
	if got.SourceMediaID != "mobile-draft:d-orig" {
		t.Fatalf("source media id=%q", got.SourceMediaID)
	}
	if strings.Contains(got.Data, "\\") || strings.Contains(got.FileName, "/") {
		t.Fatalf("attachment leaked a path: %#v", got)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil || string(raw) != "hello trusted original" {
		t.Fatalf("payload=%q err=%v", raw, err)
	}
}

func TestMobileTrustedDocumentAttachmentPublishesTextDraftWithoutOriginal(t *testing.T) {
	draft := mobileDocumentDraftRecord{
		ID: "d-text", Title: "周报", Markdown: "# 标题\n\n正文",
		UpdatedAt: time.Now().UTC(),
	}
	got, ok := mobileTrustedDocumentAttachment(draft)
	if !ok || got.FileName != "周报.md" || got.MimeType != "text/markdown" {
		t.Fatalf("attachment=%#v ok=%v", got, ok)
	}
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil || !strings.Contains(string(raw), "正文") {
		t.Fatalf("payload=%q err=%v", raw, err)
	}
}

func TestMobileTrustedDocumentAttachmentRejectsEmptyAndOversized(t *testing.T) {
	if _, ok := mobileTrustedDocumentAttachment(mobileDocumentDraftRecord{ID: "empty"}); ok {
		t.Fatal("empty draft must not publish")
	}
	huge := make([]byte, agent.MaxOfficeReadFileBytes+1)
	if _, ok := mobileTrustedDocumentAttachment(mobileDocumentDraftRecord{
		ID: "huge", SourceFilename: "big.pdf", SourceBytes: huge,
	}); ok {
		t.Fatal("oversized original must fail closed")
	}
	if _, ok := mobileTrustedDocumentAttachment(mobileDocumentDraftRecord{
		ID: "bin", SourceFilename: "payload.bin", SourceContentType: "application/octet-stream",
		SourceBytes: []byte("not a document"),
	}); ok {
		t.Fatal("unrecognized original must not publish as a document attachment")
	}
	if _, ok := mobileTrustedDocumentAttachment(mobileDocumentDraftRecord{
		ID: "wav-huge", SourceFilename: "clip.wav", SourceContentType: "audio/wav",
		SourceBytes: make([]byte, agentservice.ReviewedHostAudioTranscribeMaxBytes+1),
	}); ok {
		t.Fatal("oversized audio original must not fall through to document publish")
	}
}

func TestMobileTrustedDocumentAttachmentForPrincipalRejectsOtherOwner(t *testing.T) {
	principal := &auth.ViewerPrincipal{UserID: "owner-a", TenantID: "tenant-a"}
	mobileDocuments.Lock()
	mobileDocuments.drafts["doc-other"] = mobileDocumentDraftRecord{
		ID: "doc-other", OwnerID: "owner-b", TenantID: "tenant-a",
		Markdown: "secret", UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "doc-other")
		mobileDocuments.Unlock()
	})
	if _, ok := mobileTrustedDocumentAttachmentForPrincipal(principal, "doc-other"); ok {
		t.Fatal("other owner's draft must not publish")
	}
	if atts := mobileTrustedDocumentAttachments(principal, "doc-other"); len(atts) != 0 {
		t.Fatalf("attachments=%#v", atts)
	}
}

func TestMobileTrustedDocumentAttachmentSniffsUnnamedPDF(t *testing.T) {
	got, ok := mobileTrustedDocumentAttachment(mobileDocumentDraftRecord{
		ID: "d-pdf", SourceFilename: "", SourceContentType: "application/octet-stream",
		SourceBytes: []byte("%PDF-1.4\n"),
	})
	if !ok || got.MimeType != "application/pdf" || got.FileName != "document.pdf" {
		t.Fatalf("unnamed pdf draft = ok=%v %#v", ok, got)
	}
	if got.SourceMediaID != "mobile-draft:d-pdf" {
		t.Fatalf("source media id=%q", got.SourceMediaID)
	}
}
