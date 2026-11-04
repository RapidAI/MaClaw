package intent

import "unicode"

// script_bypass.go — Cyrillic (Mongolian) messages bypass the local heads.
//
// The local intent / tool heads are trained on Chinese and do not meet their
// accuracy gates on Cyrillic Mongolian (r14 Phase B, STATUS_r14.md). Until a
// head passes those gates, a predominantly Cyrillic message skips the local
// intent head (L3 + head probe) and the first-party tool head and goes to the
// remote L3 tree LLM. Simplified / Traditional Chinese and English are unchanged.

// IsPredominantlyCyrillic reports whether at least half of the letters in
// text are Cyrillic (and there are at least two). Digits, punctuation,
// spaces and symbols are ignored; Han ideographs and Latin letters count as
// non-Cyrillic letters, so mixed zh/en text with a stray Cyrillic word is not
// affected.
func IsPredominantlyCyrillic(text string) bool {
	cyr, other := 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyr++
		case unicode.IsLetter(r):
			other++
		}
	}
	return cyr >= 2 && cyr >= other
}

// BypassLocalHeads reports whether text must skip the local intent and tool
// heads and go to the remote L3.
func BypassLocalHeads(text string) bool { return IsPredominantlyCyrillic(text) }
