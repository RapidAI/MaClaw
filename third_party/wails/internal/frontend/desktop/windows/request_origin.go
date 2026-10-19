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
