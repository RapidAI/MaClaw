package llm

import (
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/kimicode"
	"github.com/RapidAI/CodeClaw/corelib/trae"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

// ApplyProviderAuthHeaders adds provider-specific authorization headers after
// the standard Bearer token has been attached. Grok Build authenticates
// first-party user OAuth tokens with this explicit client marker; ordinary xAI
// API keys must not receive it.
func ApplyProviderAuthHeaders(req *http.Request, cfg corelib.MaclawLLMConfig) {
	if req == nil {
		return
	}
	workbuddy.ApplyHeaders(req.Header, cfg)
	kimicode.ApplyHeaders(req.Header, cfg)
	trae.ApplyHeaders(req.Header, cfg.TraeMachineID, cfg.TraeDeviceID, "")
	if !strings.EqualFold(strings.TrimSpace(cfg.ProviderName), "xAI-Grok") ||
		!strings.EqualFold(strings.TrimSpace(cfg.AuthType), "oauth") {
		return
	}
	req.Header.Set("X-XAI-Token-Auth", "xai-grok-cli")
}
