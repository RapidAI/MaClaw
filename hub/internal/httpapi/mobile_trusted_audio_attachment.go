package httpapi

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileTrustedAudioAttachment publishes one viewer-owned meeting recording
// as a host-trusted current-turn audio attachment. The model never sees Dir,
// filesystem paths, or GUI asr names. Missing, oversized, or unrecognized
// audio fails closed instead of inventing a transcript or path tool.
func mobileTrustedAudioAttachment(rec mobileMeetingRecording) (agent.MessageAttachment, bool) {
	mime, fileName, ok := mobileTrustedAudioMIME(rec.ContentType)
	if !ok {
		return agent.MessageAttachment{}, false
	}
	if strings.TrimSpace(rec.Dir) == "" {
		return agent.MessageAttachment{}, false
	}
	path := filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType))
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return agent.MessageAttachment{}, false
	}
	if info.Size() > int64(agentservice.ReviewedHostAudioTranscribeMaxBytes) {
		return agent.MessageAttachment{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > agentservice.ReviewedHostAudioTranscribeMaxBytes {
		return agent.MessageAttachment{}, false
	}
	return agent.MessageAttachment{
		Type:          "audio",
		FileName:      fileName,
		MimeType:      mime,
		Data:          base64.StdEncoding.EncodeToString(raw),
		Size:          int64(len(raw)),
		SourceMediaID: "mobile-recording:" + strings.TrimSpace(rec.ID),
	}, true
}

func mobileTrustedAudioAttachmentForPrincipal(principal *auth.ViewerPrincipal, recordingID string) (agent.MessageAttachment, bool) {
	rec, ok := mobileLookupOwnedRecording(principal, recordingID)
	if !ok {
		return agent.MessageAttachment{}, false
	}
	return mobileTrustedAudioAttachment(rec)
}

func mobileTrustedAudioAttachments(principal *auth.ViewerPrincipal, recordingID string) []agent.MessageAttachment {
	attachment, ok := mobileTrustedAudioAttachmentForPrincipal(principal, recordingID)
	if !ok {
		return nil
	}
	return []agent.MessageAttachment{attachment}
}

func mobileDraftLooksLikeTrustedAudio(draft mobileDocumentDraftRecord) bool {
	att, ok := mobileCanonicalizeOwnedOriginal(draft)
	return ok && att.Type == "audio"
}

func mobileTrustedAudioAttachmentFromDraft(draft mobileDocumentDraftRecord) (agent.MessageAttachment, bool) {
	att, ok := mobileCanonicalizeOwnedOriginal(draft)
	if !ok || att.Type != "audio" {
		return agent.MessageAttachment{}, false
	}
	return att, true
}

func mobileCanonicalizeOwnedOriginal(draft mobileDocumentDraftRecord) (agent.MessageAttachment, bool) {
	if !mobileDraftHasOriginal(draft) {
		return agent.MessageAttachment{}, false
	}
	raw := mobileDraftLoadSourceBytes(&draft)
	if len(raw) == 0 {
		return agent.MessageAttachment{}, false
	}
	name := strings.TrimSpace(draft.SourceFilename)
	if name == "" {
		name = "file"
	}
	mime := strings.TrimSpace(draft.SourceContentType)
	if mime == "" {
		mime = "application/octet-stream"
	}
	return agentservice.ReviewedHostCanonicalizeTrustedAttachment(agent.MessageAttachment{
		Type:          "file",
		FileName:      name,
		MimeType:      mime,
		Data:          base64.StdEncoding.EncodeToString(raw),
		SourceMediaID: "mobile-draft:" + strings.TrimSpace(draft.ID),
	})
}

func mobileTrustedAgentAttachments(principal *auth.ViewerPrincipal, documentID, recordingID string) []agent.MessageAttachment {
	out := mobileTrustedDocumentAttachments(principal, documentID)
	return append(out, mobileTrustedAudioAttachments(principal, recordingID)...)
}

func mobileLookupOwnedRecording(principal *auth.ViewerPrincipal, recordingID string) (mobileMeetingRecording, bool) {
	id := strings.TrimSpace(recordingID)
	if id == "" || principal == nil {
		return mobileMeetingRecording{}, false
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		return mobileMeetingRecording{}, false
	}
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok := mobileMeetingRecordings.items[id]
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, rec.TenantID) {
		return mobileMeetingRecording{}, false
	}
	return rec, true
}

func mobileTrustedAudioMIME(contentType string) (mime, fileName string, ok bool) {
	switch mobileMeetingRecordingContentType(contentType) {
	case "audio/wav":
		return "audio/wav", "recording.wav", true
	case "audio/mp4", "audio/aac":
		return "audio/mp4", "recording.m4a", true
	default:
		return "", "", false
	}
}
