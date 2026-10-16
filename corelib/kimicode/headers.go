package kimicode

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ApplyHeaders adds Kimi Code device identity to a managed API request.
// API-key calls to the same host keep their own client identity.
func ApplyHeaders(h http.Header, cfg corelib.MaclawLLMConfig) {
	if h == nil || !Matches(cfg) {
		return
	}
	ApplyHTTPHeaders(h)
}

// Matches reports whether cfg is a Kimi Code OAuth session.
func Matches(cfg corelib.MaclawLLMConfig) bool {
	if !strings.EqualFold(strings.TrimSpace(cfg.AuthType), "oauth") {
		return false
	}
	if IsProviderName(cfg.ProviderName) {
		return true
	}
	return IsCodingEndpoint(cfg.URL)
}
