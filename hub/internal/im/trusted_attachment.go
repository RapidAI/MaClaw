package im

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// CanonicalizeTrustedHostAttachment upgrades a downloaded IM attachment to
// the reviewed audio/document allowlist when filename, MIME, or byte
// signatures are enough. Unrecognized types are returned unchanged.
func CanonicalizeTrustedHostAttachment(att MessageAttachment) MessageAttachment {
	converted := agent.MessageAttachment{
		Type:          att.Type,
		FileName:      att.FileName,
		MimeType:      att.MimeType,
		Data:          att.Data,
		Size:          att.Size,
		SourceMediaID: att.SourceMediaID,
	}
	out, ok := agentservice.ReviewedHostCanonicalizeTrustedAttachment(converted)
	if !ok {
		return att
	}
	return MessageAttachment{
		Type:          out.Type,
		FileName:      out.FileName,
		MimeType:      out.MimeType,
		Data:          out.Data,
		Size:          out.Size,
		SourceMediaID: out.SourceMediaID,
	}
}
