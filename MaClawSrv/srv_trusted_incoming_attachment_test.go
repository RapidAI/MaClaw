package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

func TestSrvWeixinTrustedIncomingAttachmentPublishesVoiceBytes(t *testing.T) {
	wav := testWAVBytes()
	att, ok := srvWeixinTrustedIncomingAttachment(weixin.IncomingMessage{
		FromUserID:   "wx-contact",
		ContextToken: "ctx-token-voice",
		MediaType:    "audio/wav",
		MediaData:    wav,
	})
	if !ok {
		t.Fatal("expected trusted WeChat audio attachment")
	}
	if att.Type != "audio" || att.MimeType != "audio/wav" || att.SourceMediaID != "weixin-media:ctx-token-voice" {
		t.Fatalf("attachment identity = %#v", att)
	}
	raw, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil || len(raw) != len(wav) {
		t.Fatalf("attachment data: %v len=%d", err, len(raw))
	}
	if strings.Contains(att.FileName, `\`) || strings.Contains(att.FileName, "/") {
		t.Fatalf("attachment leaked a path: %#v", att)
	}
}

func TestSrvWeixinTrustedIncomingAttachmentDefaultsVoiceToSilk(t *testing.T) {
	att, ok := srvWeixinTrustedIncomingAttachment(weixin.IncomingMessage{
		MediaType: "voice",
		MediaData: []byte("#!SILK_V3"),
	})
	if !ok {
		t.Fatal("expected WeChat voice to publish as silk")
	}
	if att.MimeType != "audio/silk" || att.FileName != "recording.silk" {
		t.Fatalf("silk default = %#v", att)
	}
}

func TestSrvIMTrustedIncomingAttachmentPublishesVoiceBytes(t *testing.T) {
	wav := testWAVBytes()
	att, ok := srvIMTrustedIncomingAttachment("telegram", srvIMIncomingMessage{
		Platform:      "telegram",
		ContactID:     "1001",
		MediaType:     "voice",
		MediaName:     "voice.wav",
		MimeType:      "audio/wav",
		MediaData:     wav,
		ClientEventID: "voice-event-1",
	})
	if !ok {
		t.Fatal("expected trusted IM audio attachment")
	}
	if att.Type != "audio" || att.MimeType != "audio/wav" || att.SourceMediaID != "telegram-media:voice-event-1" {
		t.Fatalf("attachment identity = %#v", att)
	}
	raw, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil || string(raw) != string(wav) {
		t.Fatalf("attachment data mismatch: %v", err)
	}
}

func TestSrvIMTrustedIncomingAttachmentUsesFormatHint(t *testing.T) {
	att, ok := srvIMTrustedIncomingAttachment("qq", srvIMIncomingMessage{
		MediaType: "voice",
		MediaName: "note.silk",
		MediaData: []byte("#!SILK_V3"),
	})
	if !ok || att.MimeType != "audio/silk" {
		t.Fatalf("format hint silk = ok=%v %#v", ok, att)
	}
}

func TestSrvTrustedIncomingHostAttachmentRejectsUnrecognizedAndOversize(t *testing.T) {
	if _, ok := srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName: "payload.bin",
		MimeType: "application/octet-stream",
		Data:     []byte("not-audio"),
	}); ok {
		t.Fatal("unrecognized bytes must not become a trusted attachment")
	}
	if _, ok := srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName: "huge.wav",
		MimeType: "audio/wav",
		Data:     make([]byte, agentservice.ReviewedHostAudioTranscribeMaxBytes+1),
	}); ok {
		t.Fatal("oversized audio must not publish")
	}
	if _, ok := srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		MediaType: "voice",
		Data:      []byte("raw-voice-without-hint"),
	}); ok {
		t.Fatal("generic IM voice without MIME/hint must fail closed")
	}
}

func TestSrvTrustedIncomingHostAttachmentPublishesDocument(t *testing.T) {
	att, ok := srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName:      "note.txt",
		MimeType:      "text/plain",
		SourceMediaID: "weixin-media:doc-1",
		Data:          []byte("hello"),
	})
	if !ok || att.Type != "file" || att.MimeType != "text/plain" || att.SourceMediaID != "weixin-media:doc-1" {
		t.Fatalf("document attachment = ok=%v %#v", ok, att)
	}
}

func TestSrvTrustedIncomingHostAttachmentSniffsUnnamedPDF(t *testing.T) {
	att, ok := srvTrustedIncomingHostAttachment(srvTrustedIncomingMedia{
		FileName:      "file",
		MimeType:      "application/octet-stream",
		SourceMediaID: "weixin-media:pdf-1",
		Data:          []byte("%PDF-1.4\n"),
	})
	if !ok || att.MimeType != "application/pdf" || att.FileName != "document.pdf" || att.SourceMediaID != "weixin-media:pdf-1" {
		t.Fatalf("unnamed pdf sniff = ok=%v %#v", ok, att)
	}
}

func TestThirdPartyTrustedHostAttachmentSniffsUnnamedPDF(t *testing.T) {
	att, ok := thirdPartyTrustedHostAttachment(coreim.ThirdPartyMediaReference{
		ID:       "media-pdf",
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
	}, []byte("%PDF-1.4\n"))
	if !ok || att.MimeType != "application/pdf" || att.FileName != "document.pdf" || att.SourceMediaID != "thirdparty-media:media-pdf" {
		t.Fatalf("unnamed pdf sniff = ok=%v %#v", ok, att)
	}
}

func TestSrvAudioMIMEFromFormatHint(t *testing.T) {
	got, ok := srvAudioMIMEFromFormatHint(audioconv.FormatWAV)
	if !ok || got != "audio/wav" {
		t.Fatalf("wav hint = %q ok=%v", got, ok)
	}
	if _, ok := srvAudioMIMEFromFormatHint("unknown"); ok {
		t.Fatal("unknown hint must not invent a MIME")
	}
}
