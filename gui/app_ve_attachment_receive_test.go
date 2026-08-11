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

func TestProcessMessageAttachmentsKeepsAllOfficeFormatsOutOfInlineContext(t *testing.T) {
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "contract"+ext)
			const body = "office-binary-content-must-not-be-injected"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-office", a2a.GroupDiscussionMessage{
				FileAttachments: []a2a.FileAttachment{{LocalPath: path, Filename: filepath.Base(path), MimeType: mimeTypeForFile(path)}},
			})
			if !strings.Contains(got, "[Office document: "+filepath.Base(path)) || !strings.Contains(got, "Saved path: "+path) {
				t.Fatalf("Office attachment context lacks its controlled reference: %q", got)
			}
			if strings.Contains(got, body) {
				t.Fatalf("Office attachment body leaked into agent context: %q", got)
			}
		})
	}
}

func TestProcessMessageAttachmentsKeepsMislabelledOfficeFormatsOutOfInlineContext(t *testing.T) {
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mislabelled"+ext)
			const body = "untrusted-office-bytes-must-not-be-injected"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}

			got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-office", a2a.GroupDiscussionMessage{
				FileAttachments: []a2a.FileAttachment{{LocalPath: path, Filename: filepath.Base(path), MimeType: "text/plain"}},
			})
			if !strings.Contains(got, "[Office document: "+filepath.Base(path)) || !strings.Contains(got, "Saved path: "+path) {
				t.Fatalf("mislabelled Office attachment lacks its controlled reference: %q", got)
			}
			if strings.Contains(got, body) {
				t.Fatalf("mislabelled Office attachment body leaked into agent context: %q", got)
			}
		})
	}
}

func TestProcessMessageAttachmentsKeepsMislabelledPDFOutOfInlineContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mislabelled.pdf")
	const body = "%PDF-1.7\nuntrusted-pdf-bytes-must-not-be-injected"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-pdf", a2a.GroupDiscussionMessage{
		FileAttachments: []a2a.FileAttachment{{LocalPath: path, Filename: filepath.Base(path), MimeType: "text/plain"}},
	})
	if !strings.Contains(got, "[PDF document: "+filepath.Base(path)) || !strings.Contains(got, "Saved path: "+path) {
		t.Fatalf("mislabelled PDF lacks its controlled reference: %q", got)
	}
	if strings.Contains(got, body) {
		t.Fatalf("mislabelled PDF body leaked into agent context: %q", got)
	}
}

func TestProcessMessageAttachmentsKeepsOfficeTextAttachmentsOutOfInlineContext(t *testing.T) {
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"} {
		t.Run(ext, func(t *testing.T) {
			const body = "office-content-must-never-be-injected-from-text-attachment"
			app := &App{testHomeDir: t.TempDir()}
			filename := "misrouted" + ext
			got := NewVEMessageHandler(app).ProcessMessageAttachmentsForSession("disc-inline-office", a2a.GroupDiscussionMessage{
				TextAttachments: []a2a.TextAttachment{{
					Filename: filename,
					MimeType: "text/plain",
					Content:  base64.RawStdEncoding.EncodeToString([]byte(body)),
				}},
			})
			if strings.Contains(got, body) {
				t.Fatalf("Office text attachment body leaked into agent context: %q", got)
			}
			if !strings.Contains(got, "[Binary document: "+filename) {
				t.Fatalf("Office text attachment lacks binary placeholder: %q", got)
			}
			attachmentDir := app.groupDiscussionAttachmentRoot("disc-inline-office")
			entries, err := os.ReadDir(attachmentDir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("persisted text attachment entries = %v, %v; want one", entries, err)
			}
			localPath := filepath.Join(attachmentDir, entries[0].Name())
			if !strings.Contains(got, "Saved path: "+localPath) {
				t.Fatalf("binary placeholder lacks persisted local path %q: %q", localPath, got)
			}
			stored, err := os.ReadFile(localPath)
			if err != nil || string(stored) != body {
				t.Fatalf("persisted binary attachment = %q, %v; want original content", stored, err)
			}
		})
	}
}

func TestProcessMessageAttachmentsKeepsSignatureRoutedTextAttachmentsOutOfInlineContext(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  []byte
	}{
		{name: "pdf", filename: "notes.txt", content: []byte("%PDF-1.7\\nbytes must not become prompt text")},
		{name: "ole", filename: "notes.txt", content: append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, []byte("office bytes must not become prompt text")...)},
		{name: "zip", filename: "notes.txt", content: append([]byte{'P', 'K', 3, 4}, []byte("office bytes must not become prompt text")...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := &App{testHomeDir: t.TempDir()}
			got := NewVEMessageHandler(app).ProcessMessageAttachmentsForSession("disc-inline-signature", a2a.GroupDiscussionMessage{
				TextAttachments: []a2a.TextAttachment{{
					Filename: tc.filename,
					MimeType: "text/plain",
					Content:  base64.StdEncoding.EncodeToString(tc.content),
				}},
			})
			if strings.Contains(got, "must not become prompt text") {
				t.Fatalf("signature-routed text attachment body leaked into agent context: %q", got)
			}
			if !strings.Contains(got, "[Binary document: "+tc.filename) || !strings.Contains(got, "Saved path:") {
				t.Fatalf("signature-routed text attachment lacks controlled reference: %q", got)
			}
		})
	}
}

func TestProcessMessageAttachmentsKeepsMislabelledOfficeImageAttachmentsOutOfImageContext(t *testing.T) {
	for _, ext := range []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "mislabelled-image"+ext)
			const body = "office image attachment bytes must not enter image context"
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-image-office", a2a.GroupDiscussionMessage{
				ImageAttachments: []a2a.ImageAttachment{{LocalPath: path, Filename: filepath.Base(path), MimeType: "image/png"}},
			})
			if strings.Contains(got, "Image: "+filepath.Base(path)) || strings.Contains(got, body) {
				t.Fatalf("mislabelled Office image attachment retained image context: %q", got)
			}
			if !strings.Contains(got, "[Binary document: "+filepath.Base(path)) || !strings.Contains(got, "Saved path: "+path) {
				t.Fatalf("mislabelled Office image attachment lacks controlled reference: %q", got)
			}
		})
	}
}

func TestProcessMessageAttachmentsNormalizesMislabelledOfficeImageAttachmentPath(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	sourcePath := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(sourcePath, []byte("office bytes must use a document suffix"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := NewVEMessageHandler(app).ProcessMessageAttachmentsForSession("disc-image-office-name", a2a.GroupDiscussionMessage{
		ImageAttachments: []a2a.ImageAttachment{{
			LocalPath: sourcePath,
			Filename:  "cover.png",
			MimeType:  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		}},
	})
	if !strings.Contains(got, "[Binary document: cover.png") || !strings.Contains(got, "Saved path:") {
		t.Fatalf("mislabelled Office image attachment lacks controlled reference: %q", got)
	}
	if strings.Contains(got, "Saved path: "+sourcePath) {
		t.Fatalf("mislabelled Office image attachment retained non-document source path: %q", got)
	}
	if !strings.Contains(got, ".docx") {
		t.Fatalf("mislabelled Office image attachment lacks normalized Office path: %q", got)
	}
}

func TestProcessMessageAttachmentsPreservesValidInlineTextAttachment(t *testing.T) {
	const body = "# Safe note\\nNormal UTF-8 text remains available to the agent."
	got := (&VEMessageHandler{}).ProcessMessageAttachmentsForSession("disc-inline-text", a2a.GroupDiscussionMessage{
		TextAttachments: []a2a.TextAttachment{{
			Filename: "note.txt",
			MimeType: "text/plain",
			Content:  base64.RawURLEncoding.EncodeToString([]byte(body)),
		}},
	})
	if !strings.Contains(got, body) {
		t.Fatalf("valid inline text was not retained: %q", got)
	}
}

func TestProcessMessageAttachmentsKeepsOpaqueBinaryTextAttachmentOutOfInlineContext(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	const visibleTail = "opaque binary payload must not become prompt text"
	payload := append([]byte{0x00, 0x01, 0x02}, []byte(visibleTail)...)
	got := NewVEMessageHandler(app).ProcessMessageAttachmentsForSession("disc-inline-binary", a2a.GroupDiscussionMessage{
		TextAttachments: []a2a.TextAttachment{{
			Filename: "payload.txt",
			MimeType: "text/plain",
			Content:  base64.StdEncoding.EncodeToString(payload),
		}},
	})
	if strings.Contains(got, visibleTail) {
		t.Fatalf("opaque binary text attachment leaked into agent context: %q", got)
	}
	if !strings.Contains(got, "[Binary attachment: payload.txt") || !strings.Contains(got, "Saved path:") {
		t.Fatalf("opaque binary text attachment lacks controlled reference: %q", got)
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
