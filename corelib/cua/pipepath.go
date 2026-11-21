package cua

import "strings"

// pipeName returns the name of a Windows pipe from a configured address.
// Every accepted form names the same pipe: the full form
// (two separators, a dot, the word pipe, a separator, then the name), the
// short form that starts with the word pipe and a separator, or the bare
// name. A value whose leading doubled separator collapsed to one is the
// same as the short form.
//
// The comparison is byte by byte on positions, never on a separator
// literal joined with the word: a separator can be lost in transport, and
// the word is the only stable marker.
func pipeName(addr string) string {
	sep := string([]byte{92})
	s := strings.TrimSpace(addr)
	for i := 0; i+len("pipe") < len(s); i++ {
		if s[i] != 'p' || s[i+1] != 'i' || s[i+2] != 'p' || s[i+3] != 'e' {
			continue
		}
		if s[i+len("pipe")] != sep[0] {
			continue
		}
		return strings.TrimLeft(s[i+len("pipe")+1:], sep+"/")
	}
	return strings.TrimLeft(s, sep+"/")
}
