package database

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// HostReadToolNames is the read surface a host grant may keep. It is not a
// model-history pin: BM25 and leftover ranking cannot drop these names when
// HostReadGrant is true.
var HostReadToolNames = []string{"database", "database_query"}

var genericHostTypeTokens = map[string]struct{}{
	"mysql": {}, "postgres": {}, "postgresql": {}, "sqlserver": {},
	"access": {}, "excel": {}, "sqlite": {}, "oracle": {},
	"sql": {}, "database": {}, "schema": {}, "table": {}, "query": {}, "data": {},
}

// HostReadGrant reports whether host-owned data-source facts require the SQL
// read surface for this message. Empty hostTokens means the host has no
// configured source, so this is not a grant (propose_profile still competes
// as a normal candidate). Git/knowledge 仓库 wording fails closed.
func HostReadGrant(message string, hostTokens []string) bool {
	if len(hostTokens) == 0 {
		return false
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return false
	}
	folded := strings.ToLower(msg)
	if looksLikeGitOrKnowledgeInspect(folded) {
		return false
	}
	if hostTokenHit(folded, hostTokens) {
		return true
	}
	return looksLikeCatalogInspect(folded)
}

// HostReadGrant reports the manager-scoped grant: kill switch, shutdown, and
// empty profile sets fail closed.
func (m *Manager) HostReadGrant(message string) bool {
	if m == nil || !m.ToolEnabled() {
		return false
	}
	return HostReadGrant(message, m.SearchTokens())
}

func looksLikeGitOrKnowledgeInspect(msg string) bool {
	if strings.Contains(msg, "知识库") || strings.Contains(msg, "暂存") ||
		strings.Contains(msg, "代码仓库") {
		return true
	}
	if strings.Contains(msg, "仓库") && !strings.Contains(msg, "数据仓库") {
		return true
	}
	for _, phrase := range []string{
		"knowledge base", "git status", "git diff", "git commit", "uncommitted",
	} {
		if tokenBoundedHit(msg, phrase) {
			return true
		}
	}
	return tokenBoundedHit(msg, "git")
}

func looksLikeCatalogInspect(msg string) bool {
	if strings.Contains(msg, "查看数据库") || strings.Contains(msg, "数据仓库") {
		return true
	}
	runes := []rune(msg)
	if containsSQLBiaoJieGou(runes) || inspectBareObject(runes) {
		return true
	}
	for _, marker := range []string{
		"inspect schema", "show tables", "list tables", "list databases",
		"information_schema",
	} {
		if tokenBoundedHit(msg, marker) {
			return true
		}
	}
	return (strings.Contains(msg, "查看") || strings.Contains(msg, "列出")) &&
		strings.Contains(msg, "数据库")
}

func containsSQLBiaoJieGou(runes []rune) bool {
	target := []rune("表结构")
	for i := 0; i+len(target) <= len(runes); i++ {
		if !runeSliceEqual(runes[i:i+len(target)], target) {
			continue
		}
		if i == 0 {
			return true
		}
		prev := runes[i-1]
		if unicode.Is(unicode.Han, prev) && !chineseClauseParticle(prev) && prev != '看' && prev != '出' {
			continue
		}
		return true
	}
	return false
}

// inspectBareObject is 查看/列出 immediately followed by a standalone 库/表
// ("查看库", "列出 表", "查看表结构"). It must not fire on 查看库存 / 列出表格.
func inspectBareObject(runes []rune) bool {
	for _, verb := range [][]rune{[]rune("查看"), []rune("列出")} {
		verbLen := len(verb)
		for i := 0; i+verbLen < len(runes); i++ {
			if !runeSliceEqual(runes[i:i+verbLen], verb) {
				continue
			}
			j := i + verbLen
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			if j >= len(runes) {
				continue
			}
			obj := runes[j]
			if (obj == '库' || obj == '表') && hanObjectStandalone(runes, j) {
				return true
			}
		}
	}
	return false
}

func hanObjectStandalone(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) {
		return false
	}
	if i+1 >= len(runes) {
		return true
	}
	if runes[i] == '表' && i+2 < len(runes) && runes[i+1] == '结' && runes[i+2] == '构' {
		return true
	}
	next := runes[i+1]
	if unicode.Is(unicode.Han, next) && !chineseClauseParticle(next) {
		return false
	}
	return true
}

func chineseClauseParticle(r rune) bool {
	switch r {
	case '的', '了', '吗', '呢', '吧', '啊', '呀', '嘛', '么', '和', '与', '及', '或', '并',
		'里', '中', '内', '上', '下':
		return true
	}
	return false
}

func runeSliceEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hostTokenHit(msg string, hostTokens []string) bool {
	for _, raw := range hostTokens {
		token := strings.ToLower(strings.TrimSpace(raw))
		if !distinctiveHostToken(token) {
			continue
		}
		if !tokenBoundedHit(msg, token) {
			continue
		}
		if weakHostToken(token) && !weakTokenContext(msg, token) && !isolatedHanCatalogMention(msg, token) {
			continue
		}
		return true
	}
	return false
}

func distinctiveHostToken(token string) bool {
	if token == "" {
		return false
	}
	if _, generic := genericHostTypeTokens[token]; generic {
		return false
	}
	letters, han := tokenLetterCounts(token)
	if han > 0 {
		return letters >= 2
	}
	return letters >= 3
}

func tokenLetterCounts(token string) (letters, han int) {
	for _, r := range token {
		switch {
		case unicode.Is(unicode.Han, r):
			han++
			letters++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			letters++
		}
	}
	return letters, han
}

// weakHostToken is too common to grant on mention alone: addresses, short
// English names (test/user/app), and two-character Han catalog names (订单).
func weakHostToken(token string) bool {
	if strings.Contains(token, ".") {
		return true
	}
	letters, han := tokenLetterCounts(token)
	if han > 0 {
		return han == 2 && letters == 2
	}
	return letters <= 5
}

func weakTokenContext(msg, token string) bool {
	if strings.Contains(msg, "查看") || strings.Contains(msg, "列出") ||
		strings.Contains(msg, "数据库") || strings.Contains(msg, "数据仓库") {
		return true
	}
	if looksLikeCatalogInspect(msg) || tokenBoundedHit(msg, "sql") {
		return true
	}
	if strings.Contains(token, ".") && tokenBoundedHit(msg, "inspect") {
		return true
	}
	if tokenHasHan(token) && strings.Contains(msg, "查") {
		return true
	}
	return false
}

func isolatedHanCatalogMention(msg, token string) bool {
	if !tokenHasHan(token) {
		return false
	}
	s := strings.TrimSpace(msg)
	return s == token || s == token+"库"
}

func tokenHasHan(token string) bool {
	for _, r := range token {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func tokenBoundedHit(msg, token string) bool {
	if token == "" {
		return false
	}
	checkHanTail := tokenHasHan(token)
	from := 0
	for {
		idx := strings.Index(msg[from:], token)
		if idx < 0 {
			return false
		}
		at := from + idx
		beforeOK := at == 0 || !asciiAlnum(rune(msg[at-1]))
		after := at + len(token)
		afterOK := after >= len(msg) || !asciiAlnum(rune(msg[after]))
		if beforeOK && afterOK && (!checkHanTail || hanTailStandalone(msg, after)) {
			return true
		}
		from = at + 1
	}
}

func hanTailStandalone(msg string, after int) bool {
	if after >= len(msg) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(msg[after:])
	if r == utf8.RuneError {
		return true
	}
	return !unicode.Is(unicode.Han, r) || chineseClauseParticle(r)
}

func asciiAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
