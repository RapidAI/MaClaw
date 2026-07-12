package memory

import (
	"regexp"
	"strings"
	"unicode"
)

// ExpandResult contains the results of query expansion.
type ExpandResult struct {
	Entities    []string // extracted entity phrases for independent BM25 queries
	QueryTokens []string // tokenized words for tag cross-matching
}

// maxEntities is the maximum number of extracted entities.
const maxEntities = 5

// maxQueryTokens is the maximum number of query tokens.
const maxQueryTokens = 20

// Chinese stopwords — common verbs, auxiliaries, pronouns that add noise.
var chineseStopwords = map[string]bool{
	"的": true, "了": true, "吗": true, "呢": true, "把": true,
	"给": true, "在": true, "是": true, "有": true, "我": true,
	"你": true, "他": true, "她": true, "它": true, "们": true,
	"帮": true, "看": true, "用": true, "下": true, "一下": true,
	"这": true, "那": true, "个": true, "些": true, "什么": true,
	"怎么": true, "如何": true, "可以": true, "能": true, "要": true,
	"会": true, "就": true, "也": true, "都": true, "还": true,
	"和": true, "与": true, "或": true, "但": true, "不": true,
	"没": true, "很": true, "太": true, "最": true, "更": true,
	"上": true, "去": true, "来": true, "到": true, "从": true,
	"为": true, "被": true, "让": true, "对": true, "向": true,
	"做": true, "搞": true, "弄": true, "整": true, "跑": true,
	"连": true, "打开": true, "关闭": true, "启动": true, "停止": true,
	"检查": true, "查看": true, "确认": true, "登录": true, "连接": true,
}

// English stopwords — common words that shouldn't be extracted as entities.
var englishStopwords = map[string]bool{
	"the": true, "this": true, "that": true, "with": true, "from": true,
	"have": true, "been": true, "will": true, "would": true, "could": true,
	"should": true, "about": true, "after": true, "before": true, "between": true,
	"into": true, "through": true, "during": true, "each": true, "some": true,
	"other": true, "than": true, "then": true, "when": true, "where": true,
	"which": true, "while": true, "also": true, "just": true, "only": true,
	"very": true, "what": true, "your": true, "their": true, "there": true,
	"here": true, "more": true, "most": true, "such": true, "like": true,
	"over": true, "under": true, "these": true, "those": true, "them": true,
}

// Entity extraction patterns ordered by specificity.
// Priority principle: patterns that produce HIGH-SIGNAL entities for tag matching
// should come first, because maxEntities=5 cap means later patterns may be skipped.
// Named concepts (compounds, acronyms) > specific identifiers (IP, domain) > paths.
var entityPatterns = []*regexp.Regexp{
	// Quoted content: "xxx" or 「xxx」 or 『xxx』
	regexp.MustCompile(`["「『]([^"」』]{2,30})["」』]`),
	// English word + Chinese noun compound: "api服务器", "gpu服务器", "ssh连接", "web页面"
	// These mixed-language compounds are common in Chinese tech conversations and
	// must be extracted as a single entity for accurate BM25/tag matching.
	// This pattern covers both pure-alpha ("api服务器") and alphanumeric ("api2服务器")
	// identifiers — [a-zA-Z][a-zA-Z0-9]+ requires ≥2 alphanumeric chars total,
	// excluding single-letter prefixes ("A机器", "V开头") from over-matching.
	regexp.MustCompile(`(?i)([a-zA-Z][a-zA-Z0-9]+)(` + `[` + chineseRange + `]{2,})`),
	// Uppercase acronym (2-6 chars): "GPU", "SSH", "API", "CUDA"
	regexp.MustCompile(`\b([A-Z]{2,6})\b`),
	// IP address
	regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`),
	// Domain name
	regexp.MustCompile(`\b([a-zA-Z0-9][-a-zA-Z0-9]*\.[a-zA-Z][-a-zA-Z0-9.]*[a-zA-Z])\b`),
	// Number + Chinese noun: "4090服务器", "4090 服务器"
	regexp.MustCompile(`(\d{2,})\s*([` + chineseRange + `]{2,6})`),
	// Chinese noun + number: "服务器4090"
	regexp.MustCompile(`([` + chineseRange + `]{2,6})\s*(\d{2,})`),
	// English proper noun (2+ capitalized words): "Claude API", "Visual Studio"
	regexp.MustCompile(`\b([A-Z][a-zA-Z]+(?:\s+[A-Z][a-zA-Z]+)+)\b`),
	// File path (Unix) — lower priority: long plan texts often contain example paths
	// that fill up maxEntities before more useful named concepts are extracted.
	regexp.MustCompile(`(/[a-zA-Z0-9_][-a-zA-Z0-9_./]*)`),
	// File path (Windows)
	regexp.MustCompile(`([A-Z]:\\[-a-zA-Z0-9_\\./]+)`),
	// English technical term (≥4 chars, may contain dots/hyphens): "deploy.sh", "nginx", "pytest"
	regexp.MustCompile(`\b([a-zA-Z][-a-zA-Z0-9_.]{2,}[a-zA-Z0-9])\b`),
}

const chineseRange = `\x{4e00}-\x{9fff}`

// chineseNounPattern matches sequences of ≥3 Chinese characters.
var chineseNounPattern = regexp.MustCompile(`([` + chineseRange + `]{3,})`)

// mixedLangCompoundRe matches English/alphanumeric word + Chinese noun compounds.
// Pre-compiled at package level to avoid per-call regexp compilation overhead.
var mixedLangCompoundRe = regexp.MustCompile(`(?i)([a-zA-Z][a-zA-Z0-9]+)([` + chineseRange + `]{2,})`)

// ExpandQuery extracts key entities and tokens from a user message.
// Pure rule-based, no LLM dependency, < 5ms latency.
func ExpandQuery(userMessage string) ExpandResult {
	if strings.TrimSpace(userMessage) == "" {
		return ExpandResult{}
	}

	var entities []string
	seen := make(map[string]bool)

	addEntity := func(s string) {
		s = strings.TrimSpace(s)
		if len([]rune(s)) < 2 {
			return
		}
		lower := strings.ToLower(s)
		if seen[lower] {
			return
		}
		// Skip if it's a pure stopword (Chinese or English)
		if chineseStopwords[s] || englishStopwords[lower] {
			return
		}
		seen[lower] = true
		entities = append(entities, s)
	}

	// Phase 1: Pattern-based entity extraction
	for _, pat := range entityPatterns {
		matches := pat.FindAllStringSubmatch(userMessage, -1)
		for _, m := range matches {
			if len(m) > 2 {
				g1 := strings.TrimSpace(m[1])
				g2 := strings.TrimSpace(m[2])

				// For English+Chinese compound patterns, the Chinese part may
				// over-match (e.g. "api服务器资源状" captures "服务器资源状").
				// Most Chinese nouns are 2-3 characters (服务器, 连接, 页面, 数据库).
				// Trim to first 3 Chinese chars to form a tight compound entity.
				// Also add the 2-char variant as a fallback (连接, 页面, 服务).
				g2Runes := []rune(g2)
				if len(g2Runes) > 3 && isAllChinese(g2Runes) {
					g2 = string(g2Runes[:3])
				}

				combined := g1 + g2
				addEntity(combined)
				// Add 2-char compound variant for better tag matching coverage,
				// but only for English+Chinese compounds (not Number+Chinese).
				if len(g1) > 0 && ((g1[0] >= 'A' && g1[0] <= 'Z') || (g1[0] >= 'a' && g1[0] <= 'z')) &&
					len([]rune(g2)) > 2 && isAllChinese([]rune(g2)) {
					addEntity(g1 + string([]rune(g2)[:2]))
				}
				// Also add individual parts if meaningful
				if len([]rune(g1)) >= 2 {
					addEntity(g1)
				}
				if len([]rune(g2)) >= 2 {
					addEntity(g2)
				}
			} else if len(m) > 1 {
				addEntity(m[1])
			}
		}
		if len(entities) >= maxEntities {
			break
		}
	}

	// Phase 2: Chinese compound noun extraction (if we haven't hit the limit)
	// Split Chinese text on stopwords, then extract remaining segments.
	if len(entities) < maxEntities {
		cnSegments := extractChineseNouns(userMessage)
		for _, cn := range cnSegments {
			addEntity(cn)
			if len(entities) >= maxEntities {
				break
			}
		}
	}

	// Cap entities
	if len(entities) > maxEntities {
		entities = entities[:maxEntities]
	}

	// Phase 3: Tokenization for tag cross-matching
	tokens := tokenizeForTagMatch(userMessage)

	return ExpandResult{
		Entities:    entities,
		QueryTokens: tokens,
	}
}

// tokenizeForTagMatch splits a user message into meaningful tokens suitable
// for tag cross-matching. Only produces complete semantic units — no sliding
// window n-gram fragments.
//
// Design principle: every token produced must be a meaningful word or phrase
// that a human would recognize as a concept. "北京" OK, "京天" ERR.
func tokenizeForTagMatch(msg string) []string {
	seen := make(map[string]bool)
	var tokens []string

	add := func(s string) {
		s = strings.TrimSpace(strings.ToLower(s))
		if len([]rune(s)) < 2 {
			return
		}
		if seen[s] || chineseStopwords[s] || englishStopwords[s] {
			return
		}
		seen[s] = true
		tokens = append(tokens, s)
	}

	// Phase 0: Extract English+Chinese compound tokens before boundary splitting.
	// These mixed-language compounds (e.g. "api服务器", "gpu服务器") are common
	// in Chinese tech text and must be preserved as single tokens for tag matching.
	for _, m := range mixedLangCompoundRe.FindAllStringSubmatch(msg, -1) {
		if len(m) > 2 {
			g1 := strings.ToLower(m[1])
			g2 := m[2]
			// Trim long Chinese part to first 3 chars (most nouns are 2-3 chars)
			g2Runes := []rune(g2)
			if len(g2Runes) > 3 && isAllChinese(g2Runes) {
				g2 = string(g2Runes[:3])
			}
			add(g1 + g2) // compound: "api服务器"
		}
	}

	// Split on whitespace and punctuation first
	parts := splitOnBoundaries(msg)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Check if it's purely Chinese, purely ASCII, or mixed
		runes := []rune(part)
		if isAllChinese(runes) {
			if len(runes) > 3 {
				// Split on stopwords to extract meaningful noun phrases.
				// Only add the sub-phrases (not the raw segment which may
				// contain trailing/internal stopwords like "的", "了").
				subPhrases := splitOnChineseStopwords(part)
				if len(subPhrases) == 1 && subPhrases[0] == part {
					// No stopwords found — the segment is a clean noun phrase.
					add(part)
				} else {
					// Stopwords found — add only the clean sub-phrases.
					for _, sp := range subPhrases {
						if len([]rune(sp)) >= 2 {
							add(sp)
						}
					}
				}
			} else {
				// Short segment (2-3 chars) — add as-is. These are typically
				// single Chinese words that passed boundary splitting.
				add(part)
			}
		} else {
			// ASCII or mixed: add as-is
			add(part)
		}

		if len(tokens) >= maxQueryTokens {
			break
		}
	}

	if len(tokens) > maxQueryTokens {
		tokens = tokens[:maxQueryTokens]
	}
	return tokens
}

// splitOnBoundaries splits text on whitespace, punctuation, and
// Chinese/ASCII boundaries.
func splitOnBoundaries(s string) []string {
	var parts []string
	var current []rune
	var lastType int // 0=unknown, 1=chinese, 2=ascii/digit

	flush := func() {
		if len(current) > 0 {
			parts = append(parts, string(current))
			current = nil
		}
	}

	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSpace(r) || r == '\uFF0C' || r == '\u3002' ||
			r == '\u3001' || r == '\uFF1B' || r == '\uFF1A' || r == '\u201C' || r == '\u201D' ||
			r == '\u300C' || r == '\u300D' || r == '\uFF08' || r == '\uFF09' {
			flush()
			lastType = 0
			continue
		}

		curType := 2 // ascii/digit
		if isChinese(r) {
			curType = 1
		}

		if lastType != 0 && curType != lastType {
			flush()
		}
		current = append(current, r)
		lastType = curType
	}
	flush()
	return parts
}

func isChinese(r rune) bool {
	return r >= 0x4e00 && r <= 0x9fff
}

func isAllChinese(runes []rune) bool {
	for _, r := range runes {
		if !isChinese(r) {
			return false
		}
	}
	return len(runes) > 0
}

// extractChineseNouns extracts meaningful Chinese noun phrases by splitting
// continuous Chinese character sequences on stopwords.
func extractChineseNouns(msg string) []string {
	// First, find all continuous Chinese character sequences.
	cnMatches := chineseNounPattern.FindAllString(msg, -1)
	var results []string

	for _, segment := range cnMatches {
		// Split this segment on stopwords to get noun phrases.
		nouns := splitOnChineseStopwords(segment)
		for _, n := range nouns {
			if len([]rune(n)) >= 2 && !chineseStopwords[n] {
				results = append(results, n)
			}
		}
	}
	return results
}

// ClassifyComplexity determines how deep into the temporal hierarchy a recall
// should go based on query length, entity count, and reasoning signal keywords.
// Pure rule-based, no LLM dependency.
func ClassifyComplexity(query string, entities []string, _ []Entry) QueryComplexity {
	runeLen := len([]rune(query))
	entityCount := len(entities)

	complexKeywords := []string{
		"为什么", "对比", "分析", "趋势", "历史", "变化", "演变", "总结", "比较",
		"why", "compare", "analyze", "trend", "history", "evolve", "summarize", "pattern",
	}
	habitKeywords := []string{
		"usually", "always", "often", "typically", "习惯", "一般", "经常", "通常", "偏好",
	}
	lq := strings.ToLower(query)
	signalCount := 0
	for _, kw := range complexKeywords {
		if strings.Contains(lq, kw) {
			signalCount++
		}
	}
	habitCount := 0
	for _, kw := range habitKeywords {
		if strings.Contains(lq, kw) {
			habitCount++
		}
	}

	if runeLen >= 40 && entityCount >= 3 && signalCount >= 2 {
		return ComplexityComplex
	}
	if runeLen <= 20 && entityCount <= 1 && signalCount == 0 && habitCount == 0 {
		return ComplexitySimple
	}
	if signalCount >= 1 || entityCount >= 2 || habitCount >= 1 {
		return ComplexityHybrid
	}
	return ComplexitySimple
}

// splitOnChineseStopwords splits a Chinese string by removing stopword
// characters from the boundaries and splitting on internal stopwords.
func splitOnChineseStopwords(s string) []string {
	runes := []rune(s)
	var results []string
	var current []rune

	i := 0
	for i < len(runes) {
		// Try 2-char stopword first, then 1-char
		if i+1 < len(runes) {
			twoChar := string(runes[i : i+2])
			if chineseStopwords[twoChar] {
				if len(current) > 0 {
					results = append(results, string(current))
					current = nil
				}
				i += 2
				continue
			}
		}
		oneChar := string(runes[i : i+1])
		if chineseStopwords[oneChar] {
			if len(current) > 0 {
				results = append(results, string(current))
				current = nil
			}
			i++
			continue
		}
		current = append(current, runes[i])
		i++
	}
	if len(current) > 0 {
		results = append(results, string(current))
	}
	return results
}
