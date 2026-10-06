package llm

import (
	"errors"
	"net/http"
	"strings"
)

// permanentForbiddenMarkers names 403 responses that backoff cannot fix.
// Everything else with status 403 — in particular our own Hub gateway's
// "authorization denied" pre-dispatch denial, which in practice is usually a
// transient HA authorization-sync gap rather than a real revocation — is
// worth retrying with exponential backoff.
var permanentForbiddenMarkers = []string{
	// Provider content-policy blocks: repeating the same request cannot help.
	"content_policy",
	"content policy",
	"content-policy",
	"blocked by content",
	"content management",
	"sensitive content",
	"内容审核",
	"内容过滤",
	"敏感内容",
	// Region / entitlement locks that only an explicit change can lift.
	"unsupported_country",
	"unsupported country",
	"country_region",
	"not available in your region",
	"region is not supported",
	"not supported in your region",
	"no active model service entitlement",
	"model service entitlement",
	"permission_error",
}

// IsPermanentForbiddenError reports a 403 denial that retrying cannot fix:
// content-policy blocks, region locks, and explicit permanent entitlement or
// permission errors. Callers that want reliability against transient gateway
// 403s should treat status-403 errors as retryable unless this reports true.
func IsPermanentForbiddenError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) || statusErr == nil {
		return false
	}
	if statusErr.StatusCode != http.StatusForbidden {
		return false
	}
	text := strings.ToLower(string(statusErr.Body))
	if strings.TrimSpace(text) == "" {
		// Body-free status error (HTTPStatusError.Error() is deliberately
		// body-free): nothing permanent is provable, so allow the backoff.
		return false
	}
	for _, marker := range permanentForbiddenMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
