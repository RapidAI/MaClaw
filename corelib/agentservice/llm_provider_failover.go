package agentservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// Provider-level failover cooldowns shared by every turn in this process, so
// a rate-limited or unreachable endpoint is skipped by the next turn too,
// until its Retry-After (or the default) has passed.
const defaultProviderFailoverCooldown = 60 * time.Second

var providerFailoverCooldowns sync.Map // key -> time.Time

var providerFailoverNow = time.Now

func providerCooldownKey(url, model string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/") + "|" + strings.TrimSpace(model)
}

func providerFailoverCooldown(err error, now time.Time) time.Duration {
	var se *llm.HTTPStatusError
	if errors.As(err, &se) {
		if wait := llm.HubRetryWait(se.Body, now); wait > 0 {
			if wait > time.Hour {
				wait = time.Hour
			}
			return wait
		}
	}
	return defaultProviderFailoverCooldown
}

// FailoverLLMProvider marks the current provider unavailable and returns the
// next configured provider that is not cooling down. A 429 keeps its
// Retry-After; transport and gateway failures use the default cooldown.
// Providers are tried in their configured order, skipping the failed one.
func (c *coreAgentCallbacks) FailoverLLMProvider(_ context.Context, current corelib.MaclawLLMConfig, err error) (corelib.MaclawLLMConfig, bool) {
	if c == nil {
		return corelib.MaclawLLMConfig{}, false
	}
	now := providerFailoverNow()
	curKey := providerCooldownKey(current.URL, current.Model)
	providerFailoverCooldowns.Store(curKey, now.Add(providerFailoverCooldown(err, now)))
	for _, p := range c.appCfg.MaclawLLMProviders {
		if corelib.MaclawLLMProviderNameEqual(p.Name, current.ProviderName) {
			continue
		}
		cfg := c.appCfg
		cfg.MaclawLLMCurrentProvider = p.Name
		next, rerr := ResolveLLMConfig(cfg)
		if rerr != nil || strings.TrimSpace(next.URL) == "" || strings.TrimSpace(next.Key) == "" || strings.TrimSpace(next.Model) == "" {
			continue
		}
		key := providerCooldownKey(next.URL, next.Model)
		if key == curKey {
			continue
		}
		if until, ok := providerFailoverCooldowns.Load(key); ok && now.Before(until.(time.Time)) {
			continue
		}
		next = applyHubBotLLMServiceGroup(next, c.instance.Metadata)
		c.llmCfg = next
		return next, true
	}
	return corelib.MaclawLLMConfig{}, false
}
