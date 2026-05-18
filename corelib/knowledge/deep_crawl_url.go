package knowledge

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// validateSeedURL checks that the seed URL starts with http:// or https://.
// Returns an error if the URL is invalid or uses an unsupported scheme.
func validateSeedURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("seed URL is required")
	}
	lower := strings.ToLower(rawURL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("seed URL must start with http:// or https://, got %q", rawURL)
	}
	_, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid seed URL: %w", err)
	}
	return nil
}

// normalizeURL normalizes a URL for deduplication purposes:
//   - Lowercase scheme and host
//   - Remove fragment (#...)
//   - Sort query parameters alphabetically
//   - Remove trailing slash from path (unless path is just "/")
func normalizeURL(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	// Lowercase scheme and host
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Remove fragment
	u.Fragment = ""
	u.RawFragment = ""

	// Sort query parameters alphabetically
	if u.RawQuery != "" {
		params := u.Query()
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sortedParts []string
		for _, k := range keys {
			values := params[k]
			sort.Strings(values)
			for _, v := range values {
				sortedParts = append(sortedParts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		u.RawQuery = strings.Join(sortedParts, "&")
	}

	// Remove trailing slash from path (unless path is just "/")
	if len(u.Path) > 1 && strings.HasSuffix(u.Path, "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
	}

	return u.String()
}

// isSameDomain compares the hostnames of two URLs (case-insensitive).
// Returns true if both URLs have the same hostname.
func isSameDomain(seedURL, candidateURL string) bool {
	seedHost := extractHostname(seedURL)
	candidateHost := extractHostname(candidateURL)
	if seedHost == "" || candidateHost == "" {
		return false
	}
	return strings.EqualFold(seedHost, candidateHost)
}

// extractHostname parses a URL and returns its lowercase hostname.
func extractHostname(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// extractTitleFromHTML extracts the <title> content from an HTML string.
// Returns empty string if no title tag is found.
func extractTitleFromHTML(html string) string {
	// Case-insensitive search for <title>...</title>
	lower := strings.ToLower(html)
	start := strings.Index(lower, "<title")
	if start < 0 {
		return ""
	}
	// Find the closing > of the opening tag
	closeTag := strings.Index(lower[start:], ">")
	if closeTag < 0 {
		return ""
	}
	contentStart := start + closeTag + 1
	end := strings.Index(lower[contentStart:], "</title>")
	if end < 0 {
		return ""
	}
	title := strings.TrimSpace(html[contentStart : contentStart+end])
	// Limit title length
	if len(title) > 200 {
		title = title[:200]
	}
	return title
}

// isHTMLContentType returns true if the Content-Type header indicates HTML or plain text
// content that is worth parsing for links. Returns true for empty content type (assume HTML).
func isHTMLContentType(ct string) bool {
	ct = strings.ToLower(ct)
	// Common HTML/text types that contain parseable links
	return strings.Contains(ct, "text/html") ||
		strings.Contains(ct, "application/xhtml") ||
		strings.Contains(ct, "text/plain") ||
		strings.Contains(ct, "text/xml") ||
		strings.Contains(ct, "application/xml")
}
