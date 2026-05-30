package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

func TestAttachmentContentRejectsOversizedLocalContextFile(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/big.txt"
	if err := writeZeroFile(path, veAttachmentContextMaxBytes+1); err != nil {
		t.Fatalf("writeZeroFile: %v", err)
	}

	got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-1", a2a.GroupDiscussionMessage{
		FileAttachments: []a2a.FileAttachment{{LocalPath: path, Filename: "big.txt", MimeType: "text/plain"}},
	})
	if got != "" {
		t.Fatalf("oversized local attachment context = %q, want empty", got)
	}
}

func TestAttachmentContentReadsLocalContextFileAtLimit(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/small.txt"
	if err := writeZeroFile(path, veAttachmentContextMaxBytes); err != nil {
		t.Fatalf("writeZeroFile: %v", err)
	}

	content, err := (&VEMessageHandler{}).attachmentContent("disc-1", "", path, "small.txt")
	if err != nil {
		t.Fatalf("attachmentContent: %v", err)
	}
	if len(content.Data) != veAttachmentContextMaxBytes {
		t.Fatalf("content len = %d, want %d", len(content.Data), veAttachmentContextMaxBytes)
	}
	if content.LocalPath != path {
		t.Fatalf("content local path = %q, want %q", content.LocalPath, path)
	}
}

func TestReadVEAttachmentContextContentRejectsOversizedReader(t *testing.T) {
	_, err := readVEAttachmentContextContent(strings.NewReader(strings.Repeat("x", veAttachmentContextMaxBytes+1)))
	if err == nil {
		t.Fatal("expected oversized reader to be rejected")
	}
}

func TestDecodeTextAttachmentRejectsOversizedEncodedContent(t *testing.T) {
	oversizedEncoded := strings.Repeat("A", base64.StdEncoding.EncodedLen(veAttachmentContextMaxBytes)+1)
	if _, err := decodeTextAttachment(a2a.TextAttachment{Filename: "big.txt", Content: oversizedEncoded}); err == nil {
		t.Fatal("expected oversized encoded text attachment to be rejected")
	}
}

func TestRemoteAttachmentIsSavedAndPathIncludedForAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ve/files/download/file-1":
			_, _ = w.Write([]byte("remote note"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldClient := veFileRelayHTTPClient
	veFileRelayHTTPClient = server.Client()
	defer func() { veFileRelayHTTPClient = oldClient }()

	app := &App{testHomeDir: t.TempDir()}
	if err := app.SaveConfig(corelib.AppConfig{RemoteHubURL: server.URL, RemoteMachineToken: "token-1"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	handler := NewVEMessageHandler(app)
	got := handler.ProcessMessageAttachmentsForSession("disc-remote", a2a.GroupDiscussionMessage{
		FileAttachments: []a2a.FileAttachment{{FileURL: "/api/ve/files/download/file-1", Filename: "note.txt", MimeType: "text/plain"}},
	})

	if !strings.Contains(got, "remote note") {
		t.Fatalf("context missing downloaded content: %q", got)
	}
	if !strings.Contains(got, "Saved path:") {
		t.Fatalf("context missing saved path: %q", got)
	}
	attachmentDir := app.groupDiscussionAttachmentRoot("disc-remote")
	entries, err := os.ReadDir(attachmentDir)
	if err != nil {
		t.Fatalf("read attachment dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("saved attachments = %d, want 1", len(entries))
	}
	savedPath := filepath.Join(attachmentDir, entries[0].Name())
	if !strings.Contains(got, savedPath) {
		t.Fatalf("context saved path %q missing from %q", savedPath, got)
	}
	data, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read saved attachment: %v", err)
	}
	if string(data) != "remote note" {
		t.Fatalf("saved content = %q", string(data))
	}
}

func TestProcessMessageAttachmentsCapsTotalContextItems(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{}
	for i := 0; i < veAttachmentContextMaxCount+5; i++ {
		msg.TextAttachments = append(msg.TextAttachments, a2a.TextAttachment{
			Filename: "note-" + string(rune('a'+(i%26))) + ".txt",
			Content:  base64.StdEncoding.EncodeToString([]byte("note")),
		})
	}
	got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-1", msg)
	if count := strings.Count(got, "File:"); count != veAttachmentContextMaxCount {
		t.Fatalf("attachment context count = %d, want %d", count, veAttachmentContextMaxCount)
	}
}

func TestProcessMessageAttachmentsCapsFailedAttempts(t *testing.T) {
	msg := a2a.GroupDiscussionMessage{}
	for i := 0; i < veAttachmentContextMaxCount; i++ {
		msg.TextAttachments = append(msg.TextAttachments, a2a.TextAttachment{
			Filename: "bad.txt",
			Content:  "not-base64",
		})
	}
	msg.TextAttachments = append(msg.TextAttachments, a2a.TextAttachment{
		Filename: "after-limit.txt",
		Content:  base64.StdEncoding.EncodeToString([]byte("should not be processed")),
	})

	got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-1", msg)
	if strings.Contains(got, "after-limit.txt") {
		t.Fatalf("processed attachment after attempt limit: %s", got)
	}
}

func writeZeroFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.CopyN(f, zeroReader{}, size)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
