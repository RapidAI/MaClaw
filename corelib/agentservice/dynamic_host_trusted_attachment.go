package agentservice

import (
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ReviewedHostCanonicalizeTrustedAttachment upgrades a host-owned attachment
// to the closed audio/document allowlist using filename, MIME, or byte
// signatures. It does not invent AMR support, does not treat zip/OLE as a
// named Office family, and does not read user text.
func ReviewedHostCanonicalizeTrustedAttachment(att agent.MessageAttachment) (agent.MessageAttachment, bool) {
	raw, err := decodeReviewedHostAttachmentBytes(att.Data)
	if err != nil || len(raw) == 0 {
		return agent.MessageAttachment{}, false
	}
	if mime, ok := ReviewedHostTrustedAudioMIME(att); ok {
		return reviewedHostCanonicalAudioAttachment(att, mime, raw)
	}
	if mime, ok := ReviewedHostTrustedDocumentMIME(att.FileName, att.MimeType); ok {
		return reviewedHostCanonicalDocumentAttachment(att, mime, raw)
	}
	if mime, name, ok := reviewedHostSniffTrustedAudio(raw); ok {
		att.FileName = reviewedHostFallbackAttachmentName(att.FileName, name)
		return reviewedHostCanonicalAudioAttachment(att, mime, raw)
	}
	if mime, name, ok := reviewedHostSniffTrustedDocument(raw); ok {
		att.FileName = reviewedHostFallbackAttachmentName(att.FileName, name)
		return reviewedHostCanonicalDocumentAttachment(att, mime, raw)
	}
	if mime, ok := ReviewedHostTrustedImageMIME(att.FileName, att.MimeType); ok {
		return reviewedHostCanonicalImageAttachment(att, mime, raw)
	}
	if mime, name, ok := reviewedHostSniffTrustedImage(raw); ok {
		att.FileName = reviewedHostFallbackAttachmentName(att.FileName, name)
		return reviewedHostCanonicalImageAttachment(att, mime, raw)
	}
	return agent.MessageAttachment{}, false
}

// CanonicalizeReviewedHostMessageAttachments upgrades host-owned attachments
// that can be recognized from filename, MIME, or closed byte signatures.
// Unrecognized items are left unchanged so later size and bind checks stay
// fail-closed.
func CanonicalizeReviewedHostMessageAttachments(attachments []agent.MessageAttachment) []agent.MessageAttachment {
	if len(attachments) == 0 {
		return attachments
	}
	out := append([]agent.MessageAttachment(nil), attachments...)
	for i, att := range out {
		if canon, ok := ReviewedHostCanonicalizeTrustedAttachment(att); ok {
			out[i] = canon
		}
	}
	return out
}

func reviewedHostCanonicalAudioAttachment(att agent.MessageAttachment, mime string, raw []byte) (agent.MessageAttachment, bool) {
	if len(raw) > ReviewedHostAudioTranscribeMaxBytes {
		return agent.MessageAttachment{}, false
	}
	att.Type = "audio"
	att.MimeType = mime
	att.Size = int64(len(raw))
	if reviewedHostGenericAttachmentName(att.FileName) {
		att.FileName = reviewedHostAudioFallbackName(mime)
	} else {
		att.FileName = filepath.Base(strings.TrimSpace(att.FileName))
	}
	return att, true
}

func reviewedHostCanonicalImageAttachment(att agent.MessageAttachment, mime string, raw []byte) (agent.MessageAttachment, bool) {
	if int64(len(raw)) > reviewedHostImageDeliverMaxBytes {
		return agent.MessageAttachment{}, false
	}
	att.Type = "image"
	att.MimeType = mime
	att.Size = int64(len(raw))
	if reviewedHostGenericAttachmentName(att.FileName) {
		att.FileName = reviewedHostImageFallbackName(mime)
	} else {
		att.FileName = filepath.Base(strings.TrimSpace(att.FileName))
	}
	return att, true
}

func reviewedHostCanonicalDocumentAttachment(att agent.MessageAttachment, mime string, raw []byte) (agent.MessageAttachment, bool) {
	if int64(len(raw)) > agent.MaxOfficeReadFileBytes {
		return agent.MessageAttachment{}, false
	}
	att.Type = "file"
	att.MimeType = mime
	att.Size = int64(len(raw))
	if reviewedHostGenericAttachmentName(att.FileName) {
		att.FileName = reviewedHostDocumentFallbackName(mime)
	} else {
		att.FileName = filepath.Base(strings.TrimSpace(att.FileName))
	}
	return att, true
}

func reviewedHostSniffTrustedAudio(raw []byte) (mime, name string, ok bool) {
	if len(raw) >= 12 && string(raw[:4]) == "RIFF" && string(raw[8:12]) == "WAVE" {
		return "audio/wav", "recording.wav", true
	}
	if len(raw) >= 4 && string(raw[:4]) == "OggS" {
		return "audio/ogg", "recording.ogg", true
	}
	if reviewedHostLooksLikeSilk(raw) {
		return "audio/silk", "recording.silk", true
	}
	return "", "", false
}

func reviewedHostLooksLikeSilk(raw []byte) bool {
	if len(raw) >= 10 && string(raw[:10]) == "#!SILK_V3\n" {
		return true
	}
	if len(raw) >= 9 && string(raw[:9]) == "#!SILK_V3" {
		return true
	}
	return len(raw) >= 10 && raw[0] == 0x02 && string(raw[1:10]) == "#!SILK_V3"
}

func reviewedHostSniffTrustedDocument(raw []byte) (mime, name string, ok bool) {
	if len(raw) >= 4 && string(raw[:4]) == "%PDF" {
		return "application/pdf", "document.pdf", true
	}
	return "", "", false
}

func reviewedHostGenericAttachmentName(name string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	switch base {
	case "", ".", "file", "attachment", "document", "recording", "voice", "audio", "media", "image", "photo", "picture", "screenshot":
		return true
	}
	switch filepath.Ext(base) {
	case ".bin", ".dat", ".amr":
		return base == "file.bin" || base == "attachment.bin" || base == "voice.amr" || base == "file.dat"
	}
	return false
}

func reviewedHostFallbackAttachmentName(current, fallback string) string {
	if reviewedHostGenericAttachmentName(current) {
		return fallback
	}
	return filepath.Base(strings.TrimSpace(current))
}

func reviewedHostAudioFallbackName(mime string) string {
	switch mime {
	case "audio/wav":
		return "recording.wav"
	case "audio/mpeg":
		return "recording.mp3"
	case "audio/ogg":
		return "recording.ogg"
	case "audio/opus":
		return "recording.opus"
	case "audio/silk":
		return "recording.silk"
	case "audio/mp4":
		return "recording.m4a"
	case "audio/webm":
		return "recording.webm"
	default:
		return "recording"
	}
}

func reviewedHostImageFallbackName(mime string) string {
	switch mime {
	case "image/jpeg":
		return "photo.jpg"
	case "image/gif":
		return "photo.gif"
	case "image/webp":
		return "photo.webp"
	default:
		return "photo.png"
	}
}

func reviewedHostDocumentFallbackName(mime string) string {
	switch mime {
	case "application/pdf":
		return "document.pdf"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "document.docx"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "document.xlsx"
	case "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "document.pptx"
	default:
		return "document.txt"
	}
}
