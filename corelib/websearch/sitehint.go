package websearch

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// Named research venues that HTML scrapers (especially Bing CN / Baidu) often
// replace with unrelated local results. When the query names one of these, a
// result set that never hits the matching host is treated as a miss so the
// browser fallback can still run.
var wellKnownSearchSites = []string{
	"openreview",
	"arxiv",
	"paperswithcode",
	"huggingface",
	"semanticscholar",
	"acm.org",
	"ieee.org",
	"ssrn",
	"neurips",
}

var siteHintDomainPattern = regexp.MustCompile(`(?i)\b(?:https?://)?(?:www\.)?([a-z0-9-]+\.(?:com|net|org|io|ai|edu|gov|info|dev|app|co|ac\.[a-z]{2}|edu\.[a-z]{2}))\b`)

func defaultBrowserFallbackEngineID(preset string) string {
	if normalizePreset(preset) == corelib.WebSearchPresetInternational {
		return "google"
	}
	return "bing_cn"
}

func remainingBrowserFallbackIDs(strategy corelib.WebSearchStrategy, attempted map[string]bool, hint string) []string {
	if !strategy.BrowserFallbackEnabled {
		return nil
	}
	configured := strategy.BrowserFallbackEngineID
	if configured == "" {
		configured = defaultBrowserFallbackEngineID(strategy.Preset)
	}
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || attempted[id] || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if hint != "" && !attempted["google"] {
		add("google")
	}
	add(configured)
	if hint != "" {
		add("google")
		add("bing_cn")
	}
	return ids
}

func searchQuerySiteHint(query string) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return ""
	}
	if m := siteHintDomainPattern.FindStringSubmatch(q); len(m) == 2 {
		host := strings.TrimPrefix(strings.ToLower(m[1]), "www.")
		parts := strings.Split(host, ".")
		if sld := registrableSiteLabel(parts); sld != "" {
			return sld
		}
	}
	for _, name := range wellKnownSearchSites {
		token := name
		if i := strings.IndexByte(name, '.'); i > 0 {
			token = name[:i]
		}
		if hasSiteToken(q, token) {
			return token
		}
	}
	return ""
}

func registrableSiteLabel(parts []string) string {
	if len(parts) < 2 {
		return ""
	}
	sld := parts[len(parts)-2]
	// One/two-letter labels ("go.dev", "ai.com") match too many unrelated hosts.
	if len(sld) < 3 {
		return ""
	}
	return sld
}

func htmlMissSkipThreshold(hint string) int {
	if hint != "" {
		return 1
	}
	return 2
}

func hasSiteToken(query, token string) bool {
	if token == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(query[start:], token)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isSiteTokenChar(query[idx-1])
		after := idx + len(token)
		afterOK := after == len(query) || !isSiteTokenChar(query[after])
		if beforeOK && afterOK {
			return true
		}
		start = idx + 1
	}
}

func isSiteTokenChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func resultCoversSiteHint(result SearchResult, hint string) bool {
	if hint == "" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(result.URL))
	if err != nil {
		return false
	}
	return hostCoversSiteHint(u.Hostname(), hint)
}

func hostCoversSiteHint(host, hint string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	hint = strings.ToLower(strings.TrimSpace(hint))
	if host == "" || hint == "" {
		return false
	}
	host = strings.TrimPrefix(host, "www.")
	if host == hint || strings.HasPrefix(host, hint+".") {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if label == hint {
			return true
		}
	}
	return false
}

func resultsCoverSiteHint(results []SearchResult, hint string) bool {
	if hint == "" {
		return true
	}
	for _, result := range results {
		if resultCoversSiteHint(result, hint) {
			return true
		}
	}
	return false
}
