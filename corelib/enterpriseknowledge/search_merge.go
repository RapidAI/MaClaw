package enterpriseknowledge

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// enterpriseLibraryFallbackID marks a merged enterprise hit whose search result
// did not carry a real library id.
const enterpriseLibraryFallbackID = "enterprise"

// MergeSearchResults appends enterprise hits after personal ones.
// Each personal evidence locator is kept, so a card, a fact, and a table row
// from one document all remain. An enterprise source already covered by those
// rows is omitted, and every other enterprise source contributes its first hit.
// When the combined rows exceed limit, the page takes one hit from each source
// before extra evidence from a source already on the page. limit <= 0 defaults
// to 20. When tagEnterprise is set, a hit without a library id is marked with
// the fallback id "enterprise". Titles are not rewritten.
func MergeSearchResults(personal, enterprise []knowledge.SearchResult, limit int, tagEnterprise bool) []knowledge.SearchResult {
	if limit <= 0 {
		limit = 20
	}
	seenEvidence := map[string]struct{}{}
	seenSources := map[string]struct{}{}
	personalKept := make([]knowledge.SearchResult, 0, len(personal))
	for _, r := range personal {
		if key := personalEvidenceKey(r); key != "" {
			if _, ok := seenEvidence[key]; ok {
				continue
			}
			seenEvidence[key] = struct{}{}
		}
		if sourceKey := mergedSourceKey(r); sourceKey != "" {
			seenSources[sourceKey] = struct{}{}
		}
		personalKept = append(personalKept, r)
	}
	enterpriseKept := make([]knowledge.SearchResult, 0, len(enterprise))
	for _, r := range enterprise {
		if sourceKey := mergedSourceKey(r); sourceKey != "" {
			if _, ok := seenSources[sourceKey]; ok {
				continue
			}
			seenSources[sourceKey] = struct{}{}
		}
		if tagEnterprise && strings.TrimSpace(r.LibraryID) == "" {
			r.LibraryID = enterpriseLibraryFallbackID
		}
		enterpriseKept = append(enterpriseKept, r)
	}
	return fitMergedPage(personalKept, enterpriseKept, limit)
}

// fitMergedPage keeps every row when they fit. A tight page shows each source
// once before it shows another fact or row from a source already included.
func fitMergedPage(personal, enterprise []knowledge.SearchResult, limit int) []knowledge.SearchResult {
	if len(personal)+len(enterprise) <= limit {
		out := make([]knowledge.SearchResult, 0, len(personal)+len(enterprise))
		out = append(out, personal...)
		out = append(out, enterprise...)
		return out
	}
	seen := map[string]struct{}{}
	primary := make([]knowledge.SearchResult, 0, limit)
	extra := make([]knowledge.SearchResult, 0)
	for _, r := range personal {
		sourceKey := mergedSourceKey(r)
		if sourceKey == "" {
			primary = append(primary, r)
			continue
		}
		if _, ok := seen[sourceKey]; ok {
			extra = append(extra, r)
			continue
		}
		seen[sourceKey] = struct{}{}
		primary = append(primary, r)
	}
	primary = append(primary, enterprise...)
	out := make([]knowledge.SearchResult, 0, limit)
	for _, row := range [][]knowledge.SearchResult{primary, extra} {
		for _, r := range row {
			if len(out) >= limit {
				return out
			}
			out = append(out, r)
		}
	}
	return out
}

// personalEvidenceKey distinguishes facts, cards, nodes, and table rows that
// share a source. Hits with no stored id fall back to the citation or snippet.
func personalEvidenceKey(r knowledge.SearchResult) string {
	locator := knowledge.SearchResultLocator(r)
	if _, id, ok := strings.Cut(locator, ":"); ok && strings.TrimSpace(id) != "" {
		if sourceID := strings.TrimSpace(r.Source.ID); sourceID != "" {
			return sourceID + "\x00" + locator
		}
		return locator
	}
	if sourceID := strings.TrimSpace(r.Source.ID); sourceID != "" {
		if extra := unstructuredMergeKey(r); extra != "" {
			return sourceID + "\x00" + extra
		}
		return sourceID
	}
	return unstructuredMergeKey(r)
}

func mergedSourceKey(r knowledge.SearchResult) string {
	if id := strings.TrimSpace(r.Source.ID); id != "" {
		return id
	}
	return unstructuredMergeKey(r)
}

func unstructuredMergeKey(r knowledge.SearchResult) string {
	citation := strings.TrimSpace(r.Citation)
	snippet := strings.TrimSpace(r.Snippet)
	if citation == "" && snippet == "" {
		return ""
	}
	return citation + "|" + snippet
}
