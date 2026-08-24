package agentservice

import (
	"encoding/base64"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func reviewedHostTestPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
}

func TestReviewedHostCanonicalizeTrustedAttachmentSniffsPDFAndWAV(t *testing.T) {
	pdf, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
	})
	if !ok || pdf.Type != "file" || pdf.MimeType != "application/pdf" || pdf.FileName != "document.pdf" {
		t.Fatalf("pdf sniff = ok=%v %#v", ok, pdf)
	}

	wav := reviewedHostTestWAV()
	audio, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString(wav),
	})
	if !ok || audio.Type != "audio" || audio.MimeType != "audio/wav" || audio.FileName != "recording.wav" {
		t.Fatalf("wav sniff = ok=%v %#v", ok, audio)
	}
}

func TestReviewedHostCanonicalizeTrustedAttachmentKeepsNamedDocument(t *testing.T) {
	att, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "file",
		FileName: "brief.pdf",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")),
	})
	if !ok || att.FileName != "brief.pdf" || att.MimeType != "application/pdf" {
		t.Fatalf("named pdf = ok=%v %#v", ok, att)
	}
}

func TestReviewedHostCanonicalizeTrustedAttachmentRejectsZipAndAMR(t *testing.T) {
	if _, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("PK\x03\x04zip")),
	}); ok {
		t.Fatal("unnamed zip must not become a trusted office document")
	}
	if _, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "voice",
		FileName: "voice.amr",
		MimeType: "audio/amr",
		Data:     base64.StdEncoding.EncodeToString([]byte("#!AMR\npayload")),
	}); ok {
		t.Fatal("AMR must stay outside the trusted speech allowlist")
	}
}

func TestReviewedHostCanonicalizeTrustedAttachmentSniffsSilkNotAMRLabel(t *testing.T) {
	att, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "voice",
		FileName: "voice.amr",
		MimeType: "audio/amr",
		Data:     base64.StdEncoding.EncodeToString([]byte("#!SILK_V3\npayload")),
	})
	if !ok || att.MimeType != "audio/silk" || att.FileName != "recording.silk" {
		t.Fatalf("silk under an AMR label = ok=%v %#v", ok, att)
	}
}

func TestCanonicalizeReviewedHostMessageAttachmentsUpgradesUnnamedPDF(t *testing.T) {
	got := CanonicalizeReviewedHostMessageAttachments([]agent.MessageAttachment{{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n")),
	}, {
		Type:     "file",
		FileName: "archive.zip",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString([]byte("PK\x03\x04zip")),
	}})
	if len(got) != 2 || got[0].MimeType != "application/pdf" || got[0].FileName != "document.pdf" {
		t.Fatalf("named pdf upgrade = %#v", got)
	}
	if got[1].MimeType != "application/octet-stream" || got[1].FileName != "archive.zip" {
		t.Fatalf("zip must stay unrecognized: %#v", got[1])
	}
}

func TestReviewedHostCanonicalizeTrustedAttachmentSniffsPNG(t *testing.T) {
	att, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "file",
		FileName: "file",
		MimeType: "application/octet-stream",
		Data:     base64.StdEncoding.EncodeToString(reviewedHostTestPNG()),
	})
	if !ok || att.Type != "image" || att.MimeType != "image/png" || att.FileName != "photo.png" {
		t.Fatalf("png sniff = ok=%v %#v", ok, att)
	}
	named, ok := ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:     "image",
		FileName: "shot.png",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(reviewedHostTestPNG()),
	})
	if !ok || named.FileName != "shot.png" || named.MimeType != "image/png" {
		t.Fatalf("named png = ok=%v %#v", ok, named)
	}
}
