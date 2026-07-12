package views

import "github.com/RapidAI/CodeClaw/corelib/textutil"

// PrepareChatBodyForDisplay strips line-leading decorative pictographs outside
// fenced code blocks. Mid-sentence pictographs are preserved. Display-only.
// Shared implementation lives in corelib/textutil.
func PrepareChatBodyForDisplay(text string) string {
	return textutil.PrepareChatBodyForDisplay(text)
}
