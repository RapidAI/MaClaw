package enterpriseknowledge

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// MergeSearchResults appends enterprise hits after personal ones, de-duping by source id.
// limit <= 0 defaults to 20. Enterprise titles may be tagged with TagEnterprise.
func MergeSearchResults(personal, enterprise []knowledge.SearchResult, limit int, tagEnterprise bool) []knowledge.SearchResult {
	if limit <= 0 {
		limit = 20
	}
	seen := map[string]struct{}{}
	out := make([]knowledge.SearchResult, 0, len(personal)+len(enterprise))
	add := func(r knowledge.SearchResult, enterpriseHit bool) {
		if len(out) >= limit {
			return
		}
		if enterpriseHit && tagEnterprise {
			title := r.Source.Title
			if title == "" {
				title = r.Source.ID
			}
			if !strings.HasPrefix(title, "[企业]") {
				r.Source.Title = "[企业] " + title
			}
		}
		key := r.Source.ID
		if key == "" {
			key = r.Citation + "|" + r.Snippet
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
		}
		out = append(out, r)
	}
	for _, r := range personal {
		add(r, false)
	}
	for _, r := range enterprise {
		add(r, true)
	}
	return out
}
