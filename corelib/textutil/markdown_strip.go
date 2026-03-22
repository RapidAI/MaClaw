// Package textutil provides shared text processing utilities used by both
// the GUI client and the Hub server.
package textutil

import (
	"regexp"
	"strings"
)

// StripMarkdown removes common Markdown formatting from text so it reads
// cleanly on IM platforms that don't render Markdown (WeChat, QQ Bot plain
// text, etc.).
//
// Handled patterns:
//   - Bold: **text** / __text__  → text
//   - Italic: *text* / _text_    → text
//   - Strikethrough: ~~text~~    → text
//   - Inline code: `code`        → code
//   - Code blocks: ```...```     → (content preserved, fences removed)
//   - Headers: # / ## / ###      → text (prefix removed)
//   - Links: [text](url)         → text
//   - Images: ![alt](url)        → alt
//   - Blockquotes: > text        → text
//   - Horizontal rules: --- / ***→ (removed)
func StripMarkdown(text string) string {
	if text == "" {
		return text
	}

	// Remove fenced code block markers (keep content).
	text = reCodeBlockFence.ReplaceAllString(text, "")

	// Remove inline code backticks.
	text = reInlineCode.ReplaceAllString(text, "$1")

	// Images: ![alt](url) → alt
	text = reImage.ReplaceAllString(text, "$1")

	// Links: [text](url) → text
	text = reLink.ReplaceAllString(text, "$1")

	// Bold/italic combos: ***text*** or ___text___
	text = reBoldItalic.ReplaceAllString(text, "$1")

	// Bold: **text** or __text__
	text = reBoldStar.ReplaceAllString(text, "$1")
	text = reBoldUnderscore.ReplaceAllString(text, "$1")

	// Italic: *text* or _text_
	text = reItalicStar.ReplaceAllString(text, "$1")
	text = reItalicUnderscore.ReplaceAllString(text, "${1}${2}${3}")

	// Strikethrough: ~~text~~
	text = reStrikethrough.ReplaceAllString(text, "$1")

	// Process line-level patterns.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Horizontal rules: ---, ***, ___
		if reHorizontalRule.MatchString(trimmed) {
			lines[i] = ""
			continue
		}

		// Headers: # text → text
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = reHeader.ReplaceAllString(line, "$1")
			continue
		}

		// Blockquotes: > text → text
		if strings.HasPrefix(trimmed, ">") {
			lines[i] = reBlockquote.ReplaceAllString(line, "$1")
			continue
		}
	}

	return strings.Join(lines, "\n")
}

var (
	reCodeBlockFence   = regexp.MustCompile("(?m)^```[a-zA-Z]*\\s*$")
	reInlineCode       = regexp.MustCompile("`([^`]+)`")
	reImage            = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`)
	reLink             = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	reBoldItalic       = regexp.MustCompile(`(?:\*{3}|___)(.+?)(?:\*{3}|___)`)
	reBoldStar         = regexp.MustCompile(`\*{2}(.+?)\*{2}`)
	reBoldUnderscore   = regexp.MustCompile(`__(.+?)__`)
	reItalicStar       = regexp.MustCompile(`\*([^*]+)\*`)
	reItalicUnderscore = regexp.MustCompile(`(^|[\s(])_([^_]+)_([\s).,;:!?]|$)`)
	reStrikethrough    = regexp.MustCompile(`~~(.+?)~~`)
	reHeader           = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	reBlockquote       = regexp.MustCompile(`^>\s?(.*)$`)
	reHorizontalRule   = regexp.MustCompile(`^[-*_]{3,}\s*$`)
)
