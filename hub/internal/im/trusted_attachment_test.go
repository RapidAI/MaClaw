package im

import (
	"encoding/base64"
	"testing"
)

func TestCanonicalizeTrustedHostAttachmentSniffsPDF(t *testing.T) {
	got := CanonicalizeTrustedHostAttachment(MessageAttachment{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
	})
	if got.Type != "file" || got.MimeType != "application/pdf" || got.FileName != "document.pdf" {
		t.Fatalf("canonical pdf = %#v", got)
	}
}

func TestCanonicalizeTrustedHostAttachmentLeavesAMR(t *testing.T) {
	in := MessageAttachment{Type: "voice", FileName: "voice.amr", MimeType: "audio/amr", Data: base64.StdEncoding.EncodeToString([]byte("#!AMR\n"))}
	got := CanonicalizeTrustedHostAttachment(in)
	if got.MimeType != "audio/amr" || got.FileName != "voice.amr" {
		t.Fatalf("AMR must stay unchanged: %#v", got)
	}
}
