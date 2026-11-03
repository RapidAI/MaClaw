//go:build windows

package embedding

import (
	"os"
	"path/filepath"
	"strings"
)

// expandInfString turns the indirect device-name form the Enum keys store into
// the text a user recognises.
//
// Drivers registered through an INF keep the name as "@oem21.inf,%ipumcdmdriver%;NPU
// Compute Accelerator Device": a pointer at an INF string table, the key, and a
// fallback description. Showing that raw to the user (in the About badge and the
// acceleration diagnostics) is noise, so resolve the key out of the INF and fall
// back to the trailing description when the lookup fails. Anything that is not
// in the indirect form is returned unchanged.
func expandInfString(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "@") {
		return s
	}
	body := s[1:]
	// Split "@file,key;fallback" into its three parts. The fallback is optional
	// and may itself contain commas, so it is split off first.
	fallback := ""
	if i := strings.Index(body, ";"); i >= 0 {
		fallback = strings.TrimSpace(body[i+1:])
		body = body[:i]
	}
	file, key := body, ""
	if i := strings.LastIndex(body, ","); i >= 0 {
		file = body[:i]
		key = strings.TrimSpace(body[i+1:])
	}
	if key != "" {
		if v := lookupInfString(file, key); v != "" {
			return v
		}
	}
	if fallback != "" {
		return fallback
	}
	return s
}

// lookupInfString reads one key out of an INF [Strings] section. INF files are
// UTF-16 when they contain non-ASCII text and plain ASCII otherwise, so both
// encodings are tried. A missing or unreadable file is not an error: the caller
// has a fallback description to use.
func lookupInfString(file, key string) string {
	name := strings.TrimSpace(file)
	if name == "" {
		return ""
	}
	path := name
	if !filepath.IsAbs(path) {
		// The Enum keys name the INF by its oemNN.inf filename, which lives in
		// the Windows INF directory.
		path = filepath.Join(os.Getenv("SystemRoot"), "INF", name)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := decodeInfText(raw)
	// Parse only the [Strings] section: the same key can appear elsewhere in the
	// file with a different meaning.
	inStrings := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inStrings = strings.EqualFold(strings.Trim(line, "[]"), "Strings")
			continue
		}
		if !inStrings {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), key) {
			continue
		}
		v = strings.TrimSpace(v)
		// INF strings are quoted when they contain leading/trailing spaces.
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		return strings.TrimSpace(v)
	}
	return ""
}

// decodeInfText converts INF bytes to a Go string. UTF-16 LE with a BOM is the
// common case for localized INFs; everything else is treated as UTF-8, which is
// byte-compatible with the ASCII INFs that only carry Latin text.
func decodeInfText(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		u := make([]uint16, 0, (len(raw)-2)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			u = append(u, uint16(raw[i])|uint16(raw[i+1])<<8)
		}
		return string(utf16Decode(u))
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return string(raw[3:])
	}
	return string(raw)
}

// utf16Decode is a minimal UTF-16 (with surrogate pairs) decoder, so this file
// does not pull in an encoding package just to read a device name.
func utf16Decode(u []uint16) []rune {
	out := make([]rune, 0, len(u))
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c >= 0xD800 && c < 0xDC00 && i+1 < len(u) {
			if lo := u[i+1]; lo >= 0xDC00 && lo < 0xE000 {
				out = append(out, rune(c-0xD800)<<10|rune(lo-0xDC00)+0x10000)
				i++
				continue
			}
		}
		out = append(out, rune(c))
	}
	return out
}
