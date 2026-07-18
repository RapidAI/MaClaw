package main

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
	text = strings.TrimSpace(text)
	const marker = "User request:\n"
	if i := strings.LastIndex(text, marker); i >= 0 {
		return strings.TrimSpace(text[i+len(marker):])
	}
	return text
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
	if hasStructuralFullExecutionSignal(text) {
		return false
	}
	// Keep real programming asks on full; greetings and short chat go light.
	if utf8.RuneCountInString(text) > 80 {
		return false
	}
	return true
}
