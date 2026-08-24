package httpapi

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileTrustedAudioAttachmentPublishesOwnedRecording(t *testing.T) {
	dir := t.TempDir()
	raw := []byte("RIFF trusted-meeting-audio")
	if err := os.WriteFile(filepath.Join(dir, "recording.wav"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{
		ID: "rec-owned", OwnerID: "owner-a", TenantID: "tenant-a",
		Dir: dir, ContentType: "audio/wav", Status: "uploaded",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	got, ok := mobileTrustedAudioAttachment(rec)
	if !ok || got.Type != "audio" || got.FileName != "recording.wav" || got.MimeType != "audio/wav" {
		t.Fatalf("attachment=%#v ok=%v", got, ok)
	}
	if got.SourceMediaID != "mobile-recording:rec-owned" {
		t.Fatalf("source media id=%q", got.SourceMediaID)
	}
	if strings.Contains(got.Data, dir) || strings.Contains(got.FileName, "/") || strings.Contains(got.FileName, "\\") {
		t.Fatalf("attachment leaked a path: %#v", got)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("payload=%q err=%v", decoded, err)
	}
}

func TestMobileTrustedAudioAttachmentRejectsMissingOversizedAndForeign(t *testing.T) {
	if _, ok := mobileTrustedAudioAttachment(mobileMeetingRecording{ID: "empty", ContentType: "audio/wav"}); ok {
		t.Fatal("missing audio must not publish")
	}
	dir := t.TempDir()
	huge := make([]byte, agentservice.ReviewedHostAudioTranscribeMaxBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "recording.wav"), huge, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := mobileTrustedAudioAttachment(mobileMeetingRecording{
		ID: "huge", Dir: dir, ContentType: "audio/wav",
	}); ok {
		t.Fatal("oversized recording must fail closed")
	}

	okDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(okDir, "recording.wav"), []byte("RIFF ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	principal := &auth.ViewerPrincipal{UserID: "owner-a", TenantID: "tenant-a"}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items["rec-other"] = mobileMeetingRecording{
		ID: "rec-other", OwnerID: "owner-b", TenantID: "tenant-a",
		Dir: okDir, ContentType: "audio/wav",
	}
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, "rec-other")
		mobileMeetingRecordings.Unlock()
	})
	if _, ok := mobileTrustedAudioAttachmentForPrincipal(principal, "rec-other"); ok {
		t.Fatal("other owner's recording must not publish")
	}
	if atts := mobileTrustedAudioAttachments(principal, "rec-other"); len(atts) != 0 {
		t.Fatalf("attachments=%#v", atts)
	}
	if atts := mobileTrustedAgentAttachments(principal, "", "rec-other"); len(atts) != 0 {
		t.Fatalf("combined attachments=%#v", atts)
	}
}

func TestMobileTrustedAudioAttachmentFromOwnedAudioDraft(t *testing.T) {
	principal := &auth.ViewerPrincipal{UserID: "owner-a", TenantID: "tenant-a"}
	raw := []byte("RIFF draft-audio")
	mobileDocuments.Lock()
	mobileDocuments.drafts["draft-wav"] = mobileDocumentDraftRecord{
		ID: "draft-wav", OwnerID: principal.UserID, TenantID: principal.TenantID,
		Title: "clip", SourceFilename: "clip.wav", SourceContentType: "audio/wav",
		SourceBytes: raw, UpdatedAt: time.Now().UTC(),
	}
	mobileDocuments.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		delete(mobileDocuments.drafts, "draft-wav")
		mobileDocuments.Unlock()
	})
	if _, ok := mobileTrustedDocumentAttachmentForPrincipal(principal, "draft-wav"); ok {
		t.Fatal("audio draft must not publish as a document attachment")
	}
	atts := mobileTrustedDocumentAttachments(principal, "draft-wav")
	if len(atts) != 1 || atts[0].Type != "audio" || atts[0].MimeType != "audio/wav" {
		t.Fatalf("audio draft must publish as trusted audio, got %#v", atts)
	}
	if strings.Contains(atts[0].FileName, "/") || strings.Contains(atts[0].Data, "\\") {
		t.Fatalf("audio draft leaked a path: %#v", atts[0])
	}
	decoded, err := base64.StdEncoding.DecodeString(atts[0].Data)
	if err != nil || string(decoded) != string(raw) {
		t.Fatalf("payload=%q err=%v", decoded, err)
	}
}
