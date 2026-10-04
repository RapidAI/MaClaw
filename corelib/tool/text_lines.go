package tool

import "strings"

// SplitTextLines splits text into lines and keeps each line's own newline.
// strings.SplitAfter leaves a final empty segment after a trailing newline.
// Counting that segment makes a file whose real length is the page size look
// truncated, and the next cursor then names a line the compiler does not have.
// An empty string stays one empty segment so a 1-based cursor can still name it.
func SplitTextLines(content string) []string {
	lines := strings.SplitAfter(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
