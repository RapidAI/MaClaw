package agentruntime

import "github.com/RapidAI/CodeClaw/corelib/agent"

// SharedLoopMaxAttachments bounds one shared-runtime turn before routing and
// prompt construction. The limit is deliberately separate from transport
// validation limits, which may be stricter for a given channel.
const SharedLoopMaxAttachments = 8

// SharedLoopMaxAttachmentBytes is the soft aggregate payload cap used by the
// shared runtime. Data is normally base64 encoded at the transport boundary,
// so callers without an explicit Size use the conservative decoded estimate.
const SharedLoopMaxAttachmentBytes int64 = 25 * 1024 * 1024

// AttachmentsWithinRuntimeLimit gates attachment shape consistently across
// GUI, headless and future hosts. Empty metadata is tolerated for compatibility
// with transports that stage bytes out-of-band; concrete size limits still
// apply whenever Size or Data is present.
func AttachmentsWithinRuntimeLimit(attachments []agent.MessageAttachment) bool {
	if len(attachments) == 0 {
		return true
	}
	if len(attachments) > SharedLoopMaxAttachments {
		return false
	}
	var total int64
	for i := range attachments {
		attachment := &attachments[i]
		if attachment.Size > 0 {
			total += attachment.Size
		} else if attachment.Data != "" {
			// Rough base64 decoded-size estimate. Exact padding is immaterial for
			// this soft admission gate and avoids decoding attacker-controlled data.
			total += int64(len(attachment.Data)) * 3 / 4
		}
	}
	return total <= SharedLoopMaxAttachmentBytes
}

// AttachmentsWithinSharedLoopLimit is retained as a source-compatible alias
// for callers that still describe the original GUI path explicitly.
func AttachmentsWithinSharedLoopLimit(attachments []agent.MessageAttachment) bool {
	return AttachmentsWithinRuntimeLimit(attachments)
}
