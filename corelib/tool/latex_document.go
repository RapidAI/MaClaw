package tool

import "strings"

// LatexDocument reports a document-class command at the start of a line.
// A comment or a sentence that mentions the command is not a main file.
// Project-kind detection and the TeX preview resolver both use this rule so
// a directory is not classified from a different definition than the one
// that decides which file is actually compiled.
func LatexDocument(body string) bool {
	for _, line := range strings.Split(LatexCode(body), "\n") {
		if lineStartsLatexDocument(line) {
			return true
		}
	}
	return false
}

// LatexCode drops TeX comments and a leading UTF-8 BOM. A % escaped by an
// odd number of backslashes is kept. Callers that scan for \input or
// \includegraphics must use this, because a commented reference is not a
// dependency and treating it as one can compile a document before the
// figure it includes.
func LatexCode(body string) string {
	body = strings.TrimPrefix(body, "\uFEFF")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = texCodePrefix(strings.TrimRight(line, "\r"))
	}
	return strings.Join(lines, "\n")
}

func texCodePrefix(line string) string {
	var b strings.Builder
	backslashes := 0
	for _, r := range line {
		if r == '%' && backslashes%2 == 0 {
			break
		}
		b.WriteRune(r)
		if r == '\\' {
			backslashes++
		} else {
			backslashes = 0
		}
	}
	return strings.TrimSpace(b.String())
}

func lineStartsLatexDocument(code string) bool {
	return strings.HasPrefix(code, `\documentclass`) || strings.HasPrefix(code, `\documentstyle`)
}
