package agentruntime

import (
	"slices"
	"strings"
)

// SkillDocMatchScore scores how strongly a skill (by name and triggers) is
// mentioned in the user message. Trigger hits win; otherwise bounded alias
// occurrences score 1. The raw name is never substring-matched:
// "handbook-pdf" contains "book-pdf".
func SkillDocMatchScore(name string, triggers []string, msgLower string) int {
	score := CountTriggerMatches(triggers, msgLower)
	if score > 0 {
		return score
	}
	for _, alias := range SkillDocMatchAliases(name, triggers) {
		if SkillDocPhraseOccurs(msgLower, alias) {
			return 1
		}
	}
	return 0
}

// SkillDocMatchAliases derives bounded match aliases from the skill name and
// its triggers. Aliases shorter than 6 compact runes are dropped to keep
// generic words from hijacking the match.
func SkillDocMatchAliases(name string, triggers []string) []string {
	seen := make(map[string]struct{}, 4)
	out := make([]string, 0, 2)
	add := func(alias string) {
		alias = strings.ToLower(strings.TrimSpace(alias))
		if compactSkillDocLen(alias) < 6 {
			return
		}
		if _, ok := seen[alias]; ok {
			return
		}
		seen[alias] = struct{}{}
		out = append(out, alias)
	}
	if n := strings.ToLower(strings.TrimSpace(name)); n != "" {
		if i := strings.IndexAny(n, ":："); i > 0 {
			add(n[:i])
			add(n[i+1:])
		} else {
			add(n)
		}
	}
	for _, trigger := range triggers {
		parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(trigger)), isSkillDocSeparator)
		if len(parts) < 2 {
			continue
		}
		add(strings.Join(parts[len(parts)-2:], "-"))
	}
	return out
}

// SkillDocPhraseOccurs reports whether phrase occurs in msgLower as a bounded
// phrase: separator variants (hyphen/space/dash/underscore, including
// fullwidth forms) are interchangeable, ASCII word characters may not glue to
// either side, and hits inside filesystem path operands do not count.
func SkillDocPhraseOccurs(msgLower, phrase string) bool {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(phrase)), isSkillDocSeparator)
	if len(parts) == 0 {
		return false
	}
	msg := []rune(strings.ToLower(msgLower))
	first := []rune(parts[0])
	if len(first) == 0 {
		return false
	}
	for i := 0; i+len(first) <= len(msg); i++ {
		if !slices.Equal(msg[i:i+len(first)], first) {
			continue
		}
		if i > 0 && isSkillDocASCIIWord(msg[i-1]) {
			continue
		}
		pos := i + len(first)
		ok := true
		for _, part := range parts[1:] {
			sepAt := pos
			for pos < len(msg) && isSkillDocSeparator(msg[pos]) {
				pos++
			}
			if pos == sepAt {
				ok = false
				break
			}
			pr := []rune(part)
			if pos+len(pr) > len(msg) || !slices.Equal(msg[pos:pos+len(pr)], pr) {
				ok = false
				break
			}
			pos += len(pr)
		}
		if ok && (pos == len(msg) || !isSkillDocASCIIWord(msg[pos])) {
			if SkillDocMatchLooksLikePathOperand(msg, i, pos) {
				continue
			}
			return true
		}
	}
	return false
}

// SkillDocMatchLooksLikePathOperand reports a skill-name hit that sits inside a
// filesystem path token. Identity matching is for naming a skill, not for a
// file whose basename happens to contain the same characters.
func SkillDocMatchLooksLikePathOperand(msg []rune, start, end int) bool {
	if start < 0 || end > len(msg) || start >= end {
		return false
	}
	lo, hi := start, end
	for lo > 0 && !isSkillDocTokenSpace(msg[lo-1]) {
		lo--
	}
	for hi < len(msg) && !isSkillDocTokenSpace(msg[hi]) {
		hi++
	}
	token := string(msg[lo:hi])
	if strings.ContainsAny(token, `/\`) {
		return true
	}
	if len(token) >= 2 && ((token[0] >= 'a' && token[0] <= 'z') || (token[0] >= 'A' && token[0] <= 'Z')) && token[1] == ':' {
		return true
	}
	// Basename with a version/extension suffix: 人工智能数学基础-v2.2.3.pdf
	if end < len(msg) {
		switch msg[end] {
		case '-', '_', '.':
			if end+1 < len(msg) && isSkillDocASCIIWord(msg[end+1]) {
				return true
			}
		}
	}
	if start > 0 {
		switch msg[start-1] {
		case '-', '_', '.':
			if start >= 2 && isSkillDocASCIIWord(msg[start-2]) {
				return true
			}
		}
	}
	// CJK compound filename: 复制品.pdf must not count as naming a skill 复制.
	// A verb glued to an ASCII name (读取config.json) stays a real mention.
	if end < len(msg) && !isSkillDocTokenSpace(msg[end]) && !isSkillDocASCIIWord(msg[end]) && !isSkillDocSeparator(msg[end]) && tokenHasASCIIFileExtension(token) {
		return true
	}
	return false
}

// CountTriggerMatches counts how many of the skill's triggers match the user
// message as bounded phrases (hyphen/space/dash variants allowed). Returns 0
// if none match. Substring matching would treat trigger "git" as a hit inside
// "github".
func CountTriggerMatches(triggers []string, msgLower string) int {
	count := 0
	for _, t := range triggers {
		if t == "" {
			continue
		}
		if SkillDocPhraseOccurs(msgLower, t) {
			count++
		}
	}
	return count
}

func tokenHasASCIIFileExtension(token string) bool {
	dot := strings.LastIndexByte(token, '.')
	if dot < 0 || dot+1 >= len(token) {
		return false
	}
	ext := token[dot+1:]
	if len(ext) == 0 || len(ext) > 8 {
		return false
	}
	hasLetter := false
	for i := 0; i < len(ext); i++ {
		c := ext[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			hasLetter = true
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return hasLetter
}

func isSkillDocTokenSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func isSkillDocSeparator(r rune) bool {
	switch r {
	case '-', '_', ' ', '\t', '\n', '\r',
		'\u00a0', '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212', '\uff0d':
		return true
	default:
		return false
	}
}

func isSkillDocASCIIWord(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func compactSkillDocLen(s string) int {
	n := 0
	for _, r := range s {
		if isSkillDocSeparator(r) {
			continue
		}
		n++
	}
	return n
}
