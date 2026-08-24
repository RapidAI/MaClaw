package httpapi

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileTrustedDocumentAttachment publishes one viewer-owned draft as a
// host-trusted current-turn attachment. The model never sees SourcePath,
// blob IDs, or raw filesystem locations. Oversized originals fail closed
// instead of being silently replaced by extracted text.
func mobileTrustedDocumentAttachment(draft mobileDocumentDraftRecord) (agent.MessageAttachment, bool) {
	if mobileDraftLooksLikeTrustedAudio(draft) {
		return agent.MessageAttachment{}, false
	}
	if mobileDraftHasOriginal(draft) {
		att, ok := mobileCanonicalizeOwnedOriginal(draft)
		if !ok || att.Type == "audio" {
			return agent.MessageAttachment{}, false
		}
		return att, true
	}
	text := strings.TrimSpace(mobileDraftWorkingText(draft))
	if text == "" {
		return agent.MessageAttachment{}, false
	}
	raw := []byte(text)
	if int64(len(raw)) > agent.MaxOfficeReadFileBytes {
		return agent.MessageAttachment{}, false
	}
	name := strings.TrimSpace(draft.Title)
	if name == "" {
		name = strings.TrimSpace(draft.ID)
	}
	if name == "" {
		name = "document"
	}
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	return agent.MessageAttachment{
		Type:          "file",
		FileName:      filepath.Base(name),
		MimeType:      "text/markdown",
		Data:          base64.StdEncoding.EncodeToString(raw),
		Size:          int64(len(raw)),
		SourceMediaID: "mobile-draft:" + strings.TrimSpace(draft.ID),
	}, true
}

func mobileTrustedDocumentAttachmentForPrincipal(principal *auth.ViewerPrincipal, documentID string) (agent.MessageAttachment, bool) {
	draft, ok := mobileLookupOwnedDraft(principal, documentID)
	if !ok {
		return agent.MessageAttachment{}, false
	}
	return mobileTrustedDocumentAttachment(draft)
}

func mobileTrustedDocumentAttachments(principal *auth.ViewerPrincipal, documentID string) []agent.MessageAttachment {
	draft, ok := mobileLookupOwnedDraft(principal, documentID)
	if !ok {
		return nil
	}
	return mobileTrustedDraftAttachments(draft)
}

func mobileTrustedDraftAttachments(draft mobileDocumentDraftRecord) []agent.MessageAttachment {
	if attachment, ok := mobileTrustedAudioAttachmentFromDraft(draft); ok {
		return []agent.MessageAttachment{attachment}
	}
	if attachment, ok := mobileTrustedDocumentAttachment(draft); ok {
		return []agent.MessageAttachment{attachment}
	}
	return nil
}

func mobileTrustedDocumentMIME(fileName string) string {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))) {
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls", ".xlsx", ".csv":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt", ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".md", ".markdown":
		return "text/markdown"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".txt", ".log":
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}
