package main

import (
	"encoding/base64"
	"io"
	"os"
	"strings"
	"testing"

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

	content, err := (&VEMessageHandler{}).attachmentContent("disc-1", "", path)
	if err != nil {
		t.Fatalf("attachmentContent: %v", err)
	}
	if len(content) != veAttachmentContextMaxBytes {
		t.Fatalf("content len = %d, want %d", len(content), veAttachmentContextMaxBytes)
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
