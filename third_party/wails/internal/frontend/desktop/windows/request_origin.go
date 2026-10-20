//go:build windows

package windows

import (
	"net/url"
	"strings"
)

// requestIsAppOrigin reports whether raw belongs to the app page.
// processRequest may rewrite headers only for that origin. The dev server
// uses the appended user agent to select desktop IPC. A third-party document,
// including a YouTube embed, must keep the Referer and User-Agent the browser
// built from the iframe referrer policy. Rewriting those headers is what
// leaves the embedded player on the poster.
func requestIsAppOrigin(start *url.URL, raw string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return false
	}
	return appOriginMatches(start, parsed)
}

func appOriginMatches(start, parsed *url.URL) bool {
	if start == nil || parsed == nil {
		return false
	}
	return strings.EqualFold(parsed.Scheme, start.Scheme) && strings.EqualFold(parsed.Host, start.Host)
}

// appOriginRequestFilter is the WebResourceRequested URI glob for start.
// This handler serves that origin and appends the dev-server user-agent token
// only there. "*" delivers every main-frame request. Registering "*" through
// AddWebResourceRequestedFilterWithRequestSourceKinds also delivers a
// cross-origin iframe document. Reading or rewriting those headers drops the
// Referer an embedded player requires.
func appOriginRequestFilter(start *url.URL) string {
	if start == nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(start.Scheme))
	host := strings.ToLower(strings.TrimSpace(start.Host))
	if scheme == "" || host == "" {
		return ""
	}
	return scheme + "://" + host + "/*"
}
