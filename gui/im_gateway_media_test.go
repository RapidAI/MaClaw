package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestGuessMimeFromMediaRecognizesAllSixOfficeFormats(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "legacy.doc", want: "application/msword"},
		{name: "modern.docx", want: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{name: "legacy.xls", want: "application/vnd.ms-excel"},
		{name: "modern.xlsx", want: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{name: "legacy.ppt", want: "application/vnd.ms-powerpoint"},
		{name: "modern.pptx", want: "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := guessMimeFromMedia(imMediaFile.String(), tt.name); got != tt.want {
				t.Fatalf("guessMimeFromMedia(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestBuildTrustedHostMediaAttachmentPublishesDocumentAndAudio(t *testing.T) {
	doc, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		FileName:      "brief.pdf",
		MimeType:      "application/pdf",
		SourceMediaID: "weixin-media:ctx-1",
		Data:          []byte("%PDF-1.4"),
	})
	if !ok || doc.Type != "file" || doc.MimeType != "application/pdf" || doc.SourceMediaID != "weixin-media:ctx-1" {
		t.Fatalf("trusted document = ok=%v %#v", ok, doc)
	}
	raw, err := base64.StdEncoding.DecodeString(doc.Data)
	if err != nil || string(raw) != "%PDF-1.4" {
		t.Fatalf("document bytes: %v %q", err, raw)
	}

	audio, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		MediaType:        "voice",
		Data:             []byte("#!SILK_V3"),
		DefaultAudioMIME: "audio/silk",
	})
	if !ok || audio.Type != "audio" || audio.MimeType != "audio/silk" || audio.FileName != "recording.silk" {
		t.Fatalf("trusted weixin voice = ok=%v %#v", ok, audio)
	}

	unnamed, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     []byte("%PDF-1.4\n"),
	})
	if !ok || unnamed.MimeType != "application/pdf" || unnamed.FileName != "document.pdf" {
		t.Fatalf("unnamed pdf sniff = ok=%v %#v", ok, unnamed)
	}
}

func TestBuildTrustedHostMediaAttachmentRejectsUnrecognizedAndOversize(t *testing.T) {
	if _, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		FileName: "archive.zip",
		MimeType: "application/zip",
		Data:     []byte("PK"),
	}); ok {
		t.Fatal("zip must not become a trusted attachment")
	}
	if _, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		MediaType: "voice",
		Data:      []byte("raw-voice"),
	}); ok {
		t.Fatal("voice without MIME/hint must fail closed")
	}
	if _, ok := buildTrustedHostMediaAttachment(trustedHostMediaInput{
		FileName: "huge.wav",
		MimeType: "audio/wav",
		Data:     make([]byte, agentservice.ReviewedHostAudioTranscribeMaxBytes+1),
	}); ok {
		t.Fatal("oversized audio must not publish")
	}
}

func TestAppendTrustedHostMediaOrStageDoesNotLeakPath(t *testing.T) {
	text, atts := appendTrustedHostMediaOrStage("", nil, trustedHostMediaInput{
		MediaType: "file",
		FileName:  "note.txt",
		MimeType:  "text/plain",
		Data:      []byte("hello"),
	}, func() (string, error) {
		t.Fatal("trusted document must not stage a temp path")
		return `C:\evil\note.txt`, nil
	})
	if len(atts) != 1 || strings.Contains(text, `C:\evil`) || strings.Contains(text, "[file_base64") {
		t.Fatalf("trusted stage bypass: text=%q atts=%#v", text, atts)
	}
	if text != "[收到文件]" {
		t.Fatalf("empty caption = %q", text)
	}

	text, atts = appendTrustedHostMediaOrStage("see this", nil, trustedHostMediaInput{
		FileName: "archive.zip",
		MimeType: "application/zip",
		Data:     []byte("PK"),
	}, func() (string, error) {
		return "/tmp/archive.zip", nil
	})
	if len(atts) != 0 || !strings.Contains(text, "/tmp/archive.zip") {
		t.Fatalf("unrecognized media should keep staged path: text=%q atts=%#v", text, atts)
	}
}

func TestCanonicalizeIncomingIMAttachmentsSniffsPDF(t *testing.T) {
	atts := canonicalizeIncomingIMAttachments([]MessageAttachment{{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
	}})
	if len(atts) != 1 || atts[0].MimeType != "application/pdf" || atts[0].FileName != "document.pdf" {
		t.Fatalf("hub inbound pdf = %#v", atts)
	}
}
