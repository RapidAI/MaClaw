package knowledge

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// chunkerVersion identifies the policy used to create document nodes. Keeping
// it with each node makes a future re-chunk deterministic and auditable.
const chunkerVersion = "multilingual-token-v1"

const (
	targetTextNodeTokens = 700
	chunkOverlapTokens   = 80
)

type textLanguage struct {
	language   string
	script     string
	confidence float64
}

// normalizeKnowledgeText gives equivalent Unicode spellings one canonical form
// before parsing, hashing, embedding, or indexing. It intentionally does not
// lowercase text: case can be significant for code, identifiers, and names.
func normalizeKnowledgeText(text string) string {
	text = norm.NFKC.String(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if r == '\u200b' || r == '\ufeff' || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(text)
}

// detectKnowledgeLanguage is deliberately script-first. It is deterministic,
// offline, and never claims a language when mixed text is ambiguous. This is
// sufficient to choose a safe lexical analysis path without sending document
// contents to an external language-identification service.
func detectKnowledgeLanguage(text string) textLanguage {
	counts := map[string]int{}
	total := 0
	hasKana := false
	for _, r := range text {
		switch {
		case isHanRune(r):
			counts["Han"]++
			total++
		case isHiraganaRune(r) || isKatakanaRune(r):
			counts["Japanese"]++
			total++
			hasKana = true
		case isHangulRune(r):
			counts["Korean"]++
			total++
		case isThaiRune(r):
			counts["Thai"]++
			total++
		case isArabicRune(r):
			counts["Arabic"]++
			total++
		case unicode.IsLetter(r):
			counts["Latin"]++
			total++
		}
	}
	if total == 0 {
		return textLanguage{language: "und", script: "unknown"}
	}
	// Japanese normally mixes Han with kana. A Han majority must not cause it
	// to be indexed as Chinese merely because a document contains more kanji.
	if hasKana && float64(counts["Japanese"])/float64(total) >= 0.08 {
		return textLanguage{language: "ja", script: "Jpan", confidence: float64(counts["Japanese"]+counts["Han"]) / float64(total)}
	}
	best, n := "Latin", 0
	for script, count := range counts {
		if count > n {
			best, n = script, count
		}
	}
	confidence := float64(n) / float64(total)
	if confidence < 0.65 {
		return textLanguage{language: "und", script: "mixed", confidence: confidence}
	}
	switch best {
	case "Japanese":
		return textLanguage{language: "ja", script: "Jpan", confidence: confidence}
	case "Korean":
		return textLanguage{language: "ko", script: "Kore", confidence: confidence}
	case "Thai":
		return textLanguage{language: "th", script: "Thai", confidence: confidence}
	case "Arabic":
		return textLanguage{language: "ar", script: "Arab", confidence: confidence}
	case "Han":
		return textLanguage{language: "zh", script: "Hans", confidence: confidence}
	default:
		return textLanguage{language: "und", script: "Latn", confidence: confidence}
	}
}

func isHanRune(r rune) bool      { return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) }
func isHiraganaRune(r rune) bool { return r >= 0x3040 && r <= 0x309F }
func isKatakanaRune(r rune) bool { return (r >= 0x30A0 && r <= 0x30FF) || (r >= 0x31F0 && r <= 0x31FF) }
func isHangulRune(r rune) bool   { return (r >= 0xAC00 && r <= 0xD7AF) || (r >= 0x1100 && r <= 0x11FF) }
func isThaiRune(r rune) bool     { return r >= 0x0E00 && r <= 0x0E7F }
func isArabicRune(r rune) bool {
	return (r >= 0x0600 && r <= 0x06FF) || (r >= 0x0750 && r <= 0x077F) || (r >= 0x08A0 && r <= 0x08FF)
}
func isNoSpaceScriptRune(r rune) bool {
	return isHanRune(r) || isHiraganaRune(r) || isKatakanaRune(r) || isHangulRune(r) || isThaiRune(r)
}

func annotateMultilingualNodes(nodes []DocumentNode) []DocumentNode {
	prepared := annotateMultilingualNodeMetadata(nodes)
	return splitNodesToTokenBudget(prepared)
}

// annotateMultilingualNodeMetadata is the non-expanding form used by low-level
// SQL insertion paths. Public ingest paths call annotateMultilingualNodes so
// they also receive token-bounded chunks before card distillation.
func annotateMultilingualNodeMetadata(nodes []DocumentNode) []DocumentNode {
	prepared := make([]DocumentNode, 0, len(nodes))
	for i := range nodes {
		if nodes[i].Metadata != nil && nodes[i].Metadata["chunker_version"] == chunkerVersion {
			prepared = append(prepared, nodes[i])
			continue
		}
		nodes[i].Text = normalizeKnowledgeText(nodes[i].Text)
		nodes[i].Title = normalizeKnowledgeText(nodes[i].Title)
		if nodes[i].Metadata == nil {
			nodes[i].Metadata = make(map[string]string)
		}
		info := detectKnowledgeLanguage(nodes[i].Text + "\n" + nodes[i].Title)
		nodes[i].Metadata["language"] = info.language
		nodes[i].Metadata["script"] = info.script
		nodes[i].Metadata["language_confidence"] = formatLanguageConfidence(info.confidence)
		nodes[i].Metadata["chunker_version"] = chunkerVersion
		nodes[i].TokenCount = estimateTokens(nodes[i].Text)
		prepared = append(prepared, nodes[i])
	}
	return prepared
}

// splitNodesToTokenBudget turns an overlong parser node into bounded child
// nodes. It preserves source/page provenance and keeps a small trailing-token
// overlap so an answer spanning a boundary remains retrievable. Parsers still
// own semantic boundaries (headings, pages, spreadsheet rows); this is only a
// safety net for unusually large sections/pages.
func splitNodesToTokenBudget(nodes []DocumentNode) []DocumentNode {
	out := make([]DocumentNode, 0, len(nodes))
	for _, node := range nodes {
		if estimateTokens(node.Text) <= targetTextNodeTokens {
			out = append(out, node)
			continue
		}
		parts := splitTextByTokenBudget(node.Text, targetTextNodeTokens, chunkOverlapTokens)
		if len(parts) < 2 {
			out = append(out, node)
			continue
		}
		parentID := node.ID
		// Retain a lightweight structural parent. Children keep their original
		// citation/provenance while ContextPack can recover the parser heading.
		// Empty text prevents the parent from competing with its own chunks.
		parent := node
		parent.Text = ""
		parent.TokenCount = 0
		parent.Metadata = cloneNodeMetadata(node.Metadata)
		parent.Metadata["chunk_parent"] = "true"
		out = append(out, parent)
		for i, part := range parts {
			child := node
			child.ID = NewID("kdn")
			child.ParentID = parentID
			child.Text = part
			child.Title = fmt.Sprintf("%s part %d", fallbackText(node.Title, "Document"), i+1)
			child.TokenCount = estimateTokens(part)
			child.Metadata = cloneNodeMetadata(node.Metadata)
			child.Metadata["chunk_index"] = fmt.Sprint(i + 1)
			child.Metadata["chunk_count"] = fmt.Sprint(len(parts))
			child.Metadata["parent_node_id"] = parentID
			out = append(out, child)
		}
	}
	return out
}

func cloneNodeMetadata(meta map[string]string) map[string]string {
	copy := make(map[string]string, len(meta)+3)
	for k, v := range meta {
		copy[k] = v
	}
	return copy
}

func splitTextByTokenBudget(text string, budget, overlap int) []string {
	text = strings.TrimSpace(text)
	if text == "" || budget <= 0 || estimateTokens(text) <= budget {
		return []string{text}
	}
	units := textSplitUnits(text)
	if len(units) == 0 {
		return []string{text}
	}
	parts := make([]string, 0, 2)
	start := 0
	for start < len(units) {
		end := start
		used := 0
		for end < len(units) {
			cost := estimateTokens(units[end])
			if end > start && used+cost > budget {
				break
			}
			used += cost
			end++
		}
		if end == start { // A single unbroken token/unit.
			end++
		}
		parts = append(parts, strings.TrimSpace(strings.Join(units[start:end], "")))
		if end >= len(units) {
			break
		}
		nextStart := end
		if overlap > 0 {
			shared := 0
			for nextStart > start {
				cost := estimateTokens(units[nextStart-1])
				if shared+cost > overlap {
					break
				}
				shared += cost
				nextStart--
			}
		}
		if nextStart == start { // Guarantee progress for tiny budgets.
			nextStart = end
		}
		start = nextStart
	}
	return parts
}

func textSplitUnits(text string) []string {
	runes := []rune(text)
	units := make([]string, 0, len(runes)/4)
	for i := 0; i < len(runes); {
		start := i
		r := runes[i]
		if isNoSpaceScriptRune(r) {
			i++
		} else {
			i++
			for i < len(runes) && !unicode.IsSpace(runes[i]) && !isNoSpaceScriptRune(runes[i]) {
				i++
			}
			for i < len(runes) && unicode.IsSpace(runes[i]) {
				i++
			}
		}
		units = append(units, string(runes[start:i]))
	}
	return units
}

func formatLanguageConfidence(v float64) string {
	if v <= 0 {
		return "0"
	}
	if v >= 1 {
		return "1"
	}
	// Metadata is only for observability; two decimals avoids unstable noise.
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
}
