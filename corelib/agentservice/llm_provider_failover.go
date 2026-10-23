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

// Provider-level 429 cooldowns shared by every turn in this process, so a
// rate-limited key is skipped by the next turn too, until its Retry-After
// (or the default) has passed.
const defaultProviderRateLimitCooldown = 60 * time.Second

var providerRateLimitCooldowns sync.Map // key -> time.Time

var providerFailoverNow = time.Now

func providerCooldownKey(url, model string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/") + "|" + strings.TrimSpace(model)
}

func providerRateLimitCooldown(err error, now time.Time) time.Duration {
	var se *llm.HTTPStatusError
	if errors.As(err, &se) {
		if wait := llm.HubRetryWait(se.Body, now); wait > 0 {
			if wait > time.Hour {
				wait = time.Hour
			}
			return wait
		}
	}
	return defaultProviderRateLimitCooldown
}

// FailoverLLMProvider marks the current provider rate-limited and returns the
// next configured provider that is not cooling down. Providers are tried in
// their configured order, skipping the one that just failed.
func (c *coreAgentCallbacks) FailoverLLMProvider(_ context.Context, current corelib.MaclawLLMConfig, err error) (corelib.MaclawLLMConfig, bool) {
	if c == nil {
		return corelib.MaclawLLMConfig{}, false
	}
	now := providerFailoverNow()
	curKey := providerCooldownKey(current.URL, current.Model)
	providerRateLimitCooldowns.Store(curKey, now.Add(providerRateLimitCooldown(err, now)))
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
		if until, ok := providerRateLimitCooldowns.Load(key); ok && now.Before(until.(time.Time)) {
			continue
		}
		next = applyHubBotLLMServiceGroup(next, c.instance.Metadata)
		c.llmCfg = next
		return next, true
	}
	return corelib.MaclawLLMConfig{}, false
}
