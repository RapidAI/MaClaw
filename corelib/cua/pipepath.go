package cua

import "strings"

// pipePath normalizes a configured pipe address to the connectable form
// that CreateFileW wants. No prefix literal is used here: a leading
// separator is simply skipped, so the result is always exactly two
// separators followed by the name's path inside the pipe namespace. A
// configured value may be the connectable form, the short pipe-name form,
// or a bare name.
func pipePath(addr string) string {
	twoSep := string([]byte{92, 92})
	return twoSep + skipLeadingSeparators(strings.TrimSpace(addr))
}

// skipLeadingSeparators drops leading separators and slashes.
func skipLeadingSeparators(s string) string {
	i := 0
	for i < len(s) && (s[i] == 92 || s[i] == '/') {
		i++
	}
	return s[i:]
}
