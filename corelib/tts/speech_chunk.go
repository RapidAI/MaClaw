package tts

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Speech chunk limits for Kokoro (PLBert max_position_embeddings = 512).
// Chinese text expands to multiple phoneme tokens per character, so per-chunk
// rune budgets must stay well under 512 after G2P.
const (
	// DefaultSpeechChunkRunes is the preferred max characters per synthesis segment.
	DefaultSpeechChunkRunes = 100
	// MaxSafeSpeechChunkRunes hard-caps a single model call even if callers pass a larger budget.
	MaxSafeSpeechChunkRunes = 120
	// MaxSpeechPhonemeTokens leaves headroom under the 512-token context window.
	MaxSpeechPhonemeTokens = 450
	// phonemeJoinSlackTokens covers context-sensitive G2P when packing unit estimates
	// (per-unit content sum can slightly under-count vs. joined text).
	phonemeJoinSlackTokens = 4
	// SpeechChunkSilenceMs is inserted between concatenated long-form segments.
	SpeechChunkSilenceMs = 200
	// AutoSpeechMaxRunes caps non-tool auto voice summaries (IM auto-readback).
	AutoSpeechMaxRunes = 180
	// MaxLongFormSpeechRunes soft-caps explicit long-form reads (platform/payload safety).
	// Explicit tts tool may still synthesize multi-minute audio via chunking.
	MaxLongFormSpeechRunes = 8000
)

// SplitSpeechChunks cleans text and packs it into semantic segments suitable for TTS.
// maxRunes is a soft per-chunk character budget (clamped to MaxSafeSpeechChunkRunes).
// Packing also respects MaxSpeechPhonemeTokens so Kokoro will not silently truncate.
func SplitSpeechChunks(text string, maxRunes int) []string {
	return PrepareSpeechChunks(text, 0, maxRunes)
}

// PrepareSpeechChunks cleans once, optionally soft-caps total length, then semantic-chunks.
// totalCapRunes <= 0 means no total cap. Prefer this over CapSpeechText+SplitSpeechChunks
// to avoid double CleanForSpeech.
func PrepareSpeechChunks(text string, totalCapRunes, maxChunkRunes int) []string {
	cleaned := CleanForSpeech(text)
	if cleaned == "" {
		return nil
	}
	if totalCapRunes > 0 && utf8.RuneCountInString(cleaned) > totalCapRunes {
		cleaned = TruncateRunesSmart(cleaned, totalCapRunes)
	}
	return splitCleanedSpeechChunks(cleaned, maxChunkRunes)
}

// splitCleanedSpeechChunks packs already-cleaned text. Prefer SplitSpeechChunks / PrepareSpeechChunks.
func splitCleanedSpeechChunks(cleaned string, maxRunes int) []string {
	maxRunes = normalizeSpeechChunkRunes(maxRunes)
	if cleaned == "" {
		return nil
	}
	if fitsSpeechChunk(cleaned, maxRunes) {
		return []string{cleaned}
	}

	units := splitSemanticUnits(cleaned)
	if len(units) == 0 {
		return ensureSpeechChunksFit(hardSplitRunes(cleaned, maxRunes), maxRunes)
	}

	// Precompute per-unit budgets so packing does not re-run G2P on growing candidates.
	type unitBudget struct {
		text  string
		runes int
		ph    int
	}
	budgets := make([]unitBudget, 0, len(units))
	for _, u := range units {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		budgets = append(budgets, unitBudget{
			text:  u,
			runes: utf8.RuneCountInString(u),
			// Content-only phoneme estimate (no BOS/EOS) so packing can sum units.
			ph: estimatePhonemeContentTokens(u),
		})
	}
	if len(budgets) == 0 {
		return ensureSpeechChunksFit(hardSplitRunes(cleaned, maxRunes), maxRunes)
	}

	var chunks []string
	var cur strings.Builder
	curRunes, curPh := 0, 0 // curPh = content tokens only; frame (+2) applied in fits
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
		curRunes, curPh = 0, 0
	}

	for _, u := range budgets {
		// Oversized atomic unit: clause-split / hard-cut.
		if u.runes > maxRunes || phonemeFrameTokens(u.ph) > MaxSpeechPhonemeTokens {
			flush()
			chunks = append(chunks, splitOversizedUnit(u.text, maxRunes)...)
			continue
		}

		joinSpace := 0
		joinPh := 0
		if cur.Len() > 0 && needsSpeechJoinSpace(lastNonSpaceRune(cur.String()), firstNonSpaceRune(u.text)) {
			joinSpace = 1
			joinPh = 1 // space is a Kokoro vocab token
		}
		nextRunes := curRunes + joinSpace + u.runes
		// Budget check uses slack; stored curPh stays content-only for future sums.
		nextPhBudget := curPh + joinPh + u.ph
		if cur.Len() > 0 {
			nextPhBudget += phonemeJoinSlackTokens
		}
		if cur.Len() > 0 && (nextRunes > maxRunes || phonemeFrameTokens(nextPhBudget) > MaxSpeechPhonemeTokens) {
			flush()
			joinSpace, joinPh = 0, 0
			nextRunes = u.runes
		}
		if cur.Len() > 0 && joinSpace > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(u.text)
		curRunes = nextRunes
		// Store content-only (no slack) so slack is not compounded every pack step.
		if curPh == 0 {
			curPh = u.ph
		} else {
			curPh = curPh + joinPh + u.ph
		}
	}
	flush()
	if len(chunks) == 0 {
		return ensureSpeechChunksFit(hardSplitRunes(cleaned, maxRunes), maxRunes)
	}
	return ensureSpeechChunksFit(chunks, maxRunes)
}

// ensureSpeechChunksFit re-checks each packed chunk against the real G2P budget and
// hard-splits any that still overflow (guards context-sensitive under-estimates).
func ensureSpeechChunksFit(chunks []string, maxRunes int) []string {
	maxRunes = normalizeSpeechChunkRunes(maxRunes)
	if len(chunks) == 0 {
		return nil
	}
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if fitsSpeechChunk(c, maxRunes) {
			out = append(out, c)
			continue
		}
		// Prefer clause re-split before hard rune cut.
		parts := splitOversizedUnit(c, maxRunes)
		if len(parts) == 0 {
			parts = hardSplitRunes(c, maxRunes)
		}
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if fitsSpeechChunk(p, maxRunes) {
				out = append(out, p)
				continue
			}
			// Last resort: hard split again (handles pathological dense text).
			for _, h := range hardSplitRunes(p, maxRunes) {
				if h = strings.TrimSpace(h); h == "" {
					continue
				}
				// Drop only if still impossible (e.g. empty after clean); single runes always accepted.
				if fitsSpeechChunk(h, maxRunes) || utf8.RuneCountInString(h) <= 1 {
					out = append(out, h)
				} else {
					// Force rune-level cut so model never sees a multi-rune overflow chunk.
					for _, r := range []rune(h) {
						out = append(out, string(r))
					}
				}
			}
		}
	}
	return out
}

func normalizeSpeechChunkRunes(maxRunes int) int {
	if maxRunes <= 0 {
		return DefaultSpeechChunkRunes
	}
	if maxRunes > MaxSafeSpeechChunkRunes {
		return MaxSafeSpeechChunkRunes
	}
	return maxRunes
}

func fitsSpeechChunk(text string, maxRunes int) bool {
	if text == "" {
		return true
	}
	if utf8.RuneCountInString(text) > maxRunes {
		return false
	}
	return estimatePhonemeTokens(text) <= MaxSpeechPhonemeTokens
}

// estimatePhonemeContentTokens counts phoneme runes only (no BOS/EOS).
// Safe slight overestimate: unknown vocab symbols may be counted but skipped by the model.
func estimatePhonemeContentTokens(text string) int {
	ph := KokoroTextToPhonemes(text)
	if ph == "" {
		return 0
	}
	return utf8.RuneCountInString(ph)
}

// estimatePhonemeTokens approximates full Kokoro token count (BOS + content + EOS).
func estimatePhonemeTokens(text string) int {
	return phonemeFrameTokens(estimatePhonemeContentTokens(text))
}

func phonemeFrameTokens(contentTokens int) int {
	if contentTokens < 0 {
		contentTokens = 0
	}
	return contentTokens + 2 // BOS + EOS
}

func needsSpeechJoinSpace(leftLast, rightFirst rune) bool {
	if leftLast == 0 || rightFirst == 0 {
		return false
	}
	return isLatinLetter(leftLast) && isLatinLetter(rightFirst)
}

func lastNonSpaceRune(s string) rune {
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if r == utf8.RuneError && size == 1 {
			i--
			continue
		}
		i -= size
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return r
		}
	}
	return 0
}

func firstNonSpaceRune(s string) rune {
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return r
		}
		i += size
	}
	return 0
}

func isLatinLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// splitSemanticUnits breaks text into paragraphs → sentences.
func splitSemanticUnits(text string) []string {
	var units []string
	paras := splitKeepSeparators(text, func(r rune) bool {
		return r == '\n' || r == '\r'
	}, true)
	for _, p := range paras {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for _, s := range splitBySentenceEnders(p) {
			s = strings.TrimSpace(s)
			if s != "" {
				units = append(units, s)
			}
		}
	}
	return units
}

func splitBySentenceEnders(text string) []string {
	var out []string
	var cur []rune
	runes := []rune(text)
	for i, r := range runes {
		cur = append(cur, r)
		if !isSentenceEnder(r) {
			continue
		}
		// Avoid splitting decimals / versions: "4.3", "go 1.22"
		if r == '.' && isDecimalDot(runes, i) {
			continue
		}
		// Avoid common single-letter abbreviations: "U.S." mid-token is rare; keep simple.
		out = append(out, string(cur))
		cur = cur[:0]
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func isSentenceEnder(r rune) bool {
	switch r {
	case '。', '！', '？', '；', '!', '?', ';':
		return true
	case '.':
		return true
	default:
		return false
	}
}

func isDecimalDot(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) || runes[i] != '.' {
		return false
	}
	if i+1 < len(runes) && unicode.IsDigit(runes[i+1]) {
		return true
	}
	return false
}

func splitOversizedUnit(unit string, maxRunes int) []string {
	clauses := splitKeepSeparators(unit, func(r rune) bool {
		switch r {
		case '，', ',', '、', '：', ':', '—', '–':
			return true
		default:
			return false
		}
	}, false)

	var out []string
	var cur strings.Builder
	curRunes, curPh := 0, 0
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			out = append(out, s)
		}
		cur.Reset()
		curRunes, curPh = 0, 0
	}

	for _, c := range clauses {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		cr := utf8.RuneCountInString(c)
		cp := estimatePhonemeContentTokens(c)
		if cr > maxRunes || phonemeFrameTokens(cp) > MaxSpeechPhonemeTokens {
			flush()
			out = append(out, hardSplitRunes(c, maxRunes)...)
			continue
		}
		budgetPh := curPh + cp
		if cur.Len() > 0 {
			budgetPh += phonemeJoinSlackTokens
		}
		if cur.Len() > 0 && (curRunes+cr > maxRunes || phonemeFrameTokens(budgetPh) > MaxSpeechPhonemeTokens) {
			flush()
		}
		cur.WriteString(c)
		curRunes += cr
		curPh += cp
	}
	flush()
	if len(out) == 0 {
		return hardSplitRunes(unit, maxRunes)
	}
	return out
}

// splitKeepSeparators splits on delimiter runes. If dropSep, separators are discarded;
// otherwise each separator is appended to the preceding segment.
func splitKeepSeparators(text string, isSep func(rune) bool, dropSep bool) []string {
	var out []string
	var cur []rune
	for _, r := range text {
		if isSep(r) {
			if dropSep {
				if len(cur) > 0 {
					out = append(out, string(cur))
					cur = cur[:0]
				}
				continue
			}
			cur = append(cur, r)
			out = append(out, string(cur))
			cur = cur[:0]
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		out = append(out, string(cur))
	}
	return out
}

func hardSplitRunes(text string, maxRunes int) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = DefaultSpeechChunkRunes
	}
	var out []string
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		// Prefer cutting near a space or weak punctuation inside the window.
		if n < len(runes) {
			best := n
			for i := n - 1; i > n/2; i-- {
				switch runes[i] {
				case ' ', '\t', '，', ',', '、', '：', ':', '；', ';':
					best = i + 1
					n = best
					goto cut
				}
			}
		cut:
		}
		// Shrink until phoneme budget fits (down to a single rune if needed).
		for n > 1 {
			part := strings.TrimSpace(string(runes[:n]))
			if part != "" && fitsSpeechChunk(part, maxRunes) {
				break
			}
			next := n * 3 / 4
			if next >= n {
				next = n - 1
			}
			if next < 1 {
				next = 1
			}
			n = next
		}
		if n > len(runes) {
			n = len(runes)
		}
		if n < 1 {
			n = 1
		}
		part := strings.TrimSpace(string(runes[:n]))
		if part != "" {
			out = append(out, part)
		}
		// Always advance by at least n runes (even if trim emptied the window).
		runes = runes[n:]
	}
	return out
}

// CapSpeechText cleans text and soft-caps total spoken length at sentence boundaries
// when possible. Used for IM auto-summary and as a pathological-input guard on long-form
// reads (MaxLongFormSpeechRunes). Explicit multi-chunk synthesis still applies under the cap.
func CapSpeechText(text string, maxRunes int) string {
	cleaned := CleanForSpeech(text)
	if cleaned == "" || maxRunes <= 0 {
		return cleaned
	}
	if utf8.RuneCountInString(cleaned) <= maxRunes {
		return cleaned
	}
	return TruncateRunesSmart(cleaned, maxRunes)
}
