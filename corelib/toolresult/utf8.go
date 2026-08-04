package toolresult

import "unicode/utf8"

// utf8Prefix returns at most limit bytes without splitting a UTF-8 rune.
func utf8Prefix(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	end := limit
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// utf8Suffix returns at most limit trailing bytes without splitting a rune.
func utf8Suffix(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	start := len(s) - limit
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func utf8PrefixWithSuffix(s, suffix string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	if len(suffix) >= limit {
		return utf8Prefix(suffix, limit)
	}
	return utf8Prefix(s, limit-len(suffix)) + suffix
}
