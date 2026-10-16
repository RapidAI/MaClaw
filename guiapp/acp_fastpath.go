package guiapp

import (
	"strings"
	"unicode/utf8"
)

// isACPProgrammingRequestID reports Mode B programming-agent request IDs
// (session/prompt turns from VS Code via acp host).
func isACPProgrammingRequestID(requestID string) bool {
	return strings.HasPrefix(strings.TrimSpace(requestID), "acp-")
}

func isACPProgrammingMessage(msg IMUserMessage) bool {
	return isACPProgrammingRequestID(msg.RequestID)
}

// acpUserFacingText returns the bare user request when the body was wrapped by
// acpProgrammingUserText (so length / chit-chat / light routing stay accurate).
func acpUserFacingText(text string) string {
	return acpInnerUserRequest(text)
}

// acpPreferLightProfile: short free-form ACP turns should not pay for full
// agent tooling/prompt. Structural signals (paths, URLs, code fences) stay full.
func acpPreferLightProfile(msg IMUserMessage) bool {
	if !isACPProgrammingMessage(msg) {
		return false
	}
	text := acpUserFacingText(msg.Text)
	if text == "" || msg.IsBackground || len(msg.Attachments) > 0 {
		return false
	}
	// Bare 继续 after a markdown write still needs the full surface so leftover
	// / planner can HostKeep write_file. The ACP cwd wrapper currently forces
	// structural full first; this keeps 继续 off light if that gate is unwrapped.
	if semanticBareContinueQuery(text) {
		return false
	}
	if hasStructuralFullExecutionSignal(text) {
		return false
	}
	// Keep real programming asks on full; greetings and short chat go light.
	if utf8.RuneCountInString(text) > 80 {
		return false
	}
	return true
}
