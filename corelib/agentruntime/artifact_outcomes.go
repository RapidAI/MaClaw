package agentruntime

import (
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ResponseHasPDF identifies a PDF artifact using only transport-neutral
// metadata.  Hosts can use it to choose the same visible success projection
// without importing GUI response types.
func ResponseHasPDF(fileName, localPath, mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "application/pdf" || strings.HasPrefix(mime, "application/pdf;") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".pdf") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(localPath)), ".pdf")
}

// HostPublishedCurrentChannelFileNote is the tool result after a PDF is in
// the artifact broker. The host attaches that file to this chat when the
// model stops. The sentence must not name send_file: that name is also an
// external send, and a model call pauses the turn before the host attach.
const HostPublishedCurrentChannelFileNote = "PDF artifact published. The host attaches it to this chat. This is not a send."

// HostPublishedMailboxAttachmentNote is that same chat note plus the
// workspace path mailbox_send can attach. The chat card is still not the
// email. The sentence must not name send_file.
func HostPublishedMailboxAttachmentNote(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return HostPublishedCurrentChannelFileNote
	}
	// The path is the whole last line, with slashes. A period after it was
	// copied into the attachment argument. A Windows backslash is not a JSON
	// escape, so a literal copy of C:\Users never reached this send.
	path = filepath.ToSlash(path)
	return HostPublishedCurrentChannelFileNote + "\nCall mailbox_send with attachments set to:\n" + path
}

// HostOwnedGeneratePDFSucceeded recognizes the stable success marker emitted
// by the trusted PDF adapter.  Error/unknown markers always win, preventing a
// friendly-looking failure string from being treated as a committed effect.
func HostOwnedGeneratePDFSucceeded(result string) bool {
	trimmed := strings.TrimSpace(result)
	return strings.HasPrefix(trimmed, "PDF artifact published") &&
		!strings.Contains(trimmed, "[system rejected]") &&
		!strings.Contains(trimmed, "[system unknown]")
}

// KeepVisibleErrorAfterArtifactAttach marks errors that must remain visible
// even when a host managed to attach a file.  Cancellation, policy rejection,
// budget exhaustion, recovery failures, and a no-progress hard stop are not
// repaired by an artifact.
func KeepVisibleErrorAfterArtifactAttach(err string) bool {
	trimmed := strings.TrimSpace(err)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "cancelled" || strings.HasPrefix(lower, "cancelled ") {
		return true
	}
	for _, keep := range []string{
		"semantic_capability_unmet",
		"[system rejected]",
		"daily_llm_budget",
		"recovery_checkpoint_failed",
		"panicked",
		agent.ErrNoProgress,
	} {
		if strings.Contains(lower, keep) {
			return true
		}
	}
	return false
}

// ShouldClearStaleErrorAfterArtifactAttach is the inverse projection used by
// hosts after a successfully materialized artifact.
func ShouldClearStaleErrorAfterArtifactAttach(err string) bool {
	return strings.TrimSpace(err) != "" && !KeepVisibleErrorAfterArtifactAttach(err)
}
