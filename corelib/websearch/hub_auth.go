package websearch

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

// HubAuthTokenFromConfig returns the already-registered MaClaw Hub credential
// used for other hub.maclaw.top APIs. RapidSearch reuses this token instead of
// storing a dedicated proxy API key.
func HubAuthTokenFromConfig(cfg corelib.AppConfig) string {
	for _, token := range []string{cfg.RemoteViewerToken, cfg.SkillMarketSessionToken, cfg.RemoteMachineToken} {
		if trimmed := strings.TrimSpace(token); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// ApplyConfigHubAuth copies the GUI-registered hub token onto the MaClaw Hub
// RapidSearch engine when that engine has no explicit key. The token is not
// persisted by this helper; callers should apply it only on the runtime copy.
func ApplyConfigHubAuth(strategy corelib.WebSearchStrategy, cfg corelib.AppConfig) corelib.WebSearchStrategy {
	return ApplyHubAuth(strategy, HubAuthTokenFromConfig(cfg))
}

// ApplyHubAuth attaches a runtime bearer token to the MaClaw Hub engine.
func ApplyHubAuth(strategy corelib.WebSearchStrategy, token string) corelib.WebSearchStrategy {
	token = strings.TrimSpace(token)
	if token == "" {
		return strategy
	}
	engines := append([]corelib.WebSearchEngineConfig(nil), strategy.Engines...)
	changed := false
	for i := range engines {
		if engines[i].ID != WebSearchEngineMaclawHub {
			continue
		}
		if strings.TrimSpace(engines[i].APIKey) == "" {
			engines[i].APIKey = token
			changed = true
		}
		break
	}
	if !changed {
		return strategy
	}
	strategy.Engines = engines
	return strategy
}
