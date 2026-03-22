package im

import "github.com/RapidAI/CodeClaw/corelib/textutil"

// stripMarkdown delegates to the shared corelib implementation.
func stripMarkdown(text string) string {
	return textutil.StripMarkdown(text)
}
