package main

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const llmEndpointNetworkFailureTTL = 30 * time.Second

type llmEndpointFailureGate struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]llmEndpointFailureEntry
}

type llmEndpointFailureEntry struct {
	expiresAt time.Time
	reason    string
}

func newLLMEndpointFailureGate(ttl time.Duration) *llmEndpointFailureGate {
	if ttl <= 0 {
		ttl = llmEndpointNetworkFailureTTL
	}
	return &llmEndpointFailureGate{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]llmEndpointFailureEntry),
	}
}

func (g *llmEndpointFailureGate) shouldSkip(cfg corelib.MaclawLLMConfig) (string, bool) {
	if g == nil {
		return "", false
	}
	key := llmEndpointFailureKey(cfg)
	if key == "" {
		return "", false
	}
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[key]
	if !ok {
		return "", false
	}
	if !entry.expiresAt.After(now) {
		delete(g.entries, key)
		return "", false
	}
	log.Printf("[LLM Endpoint Gate] skip lightweight call key=%s remaining=%s reason=%q", key, entry.expiresAt.Sub(now).Round(time.Millisecond), truncateForLogGUI(entry.reason, 160))
	return entry.reason, true
}

func (g *llmEndpointFailureGate) observe(cfg corelib.MaclawLLMConfig, err error) {
	if g == nil {
		return
	}
	key := llmEndpointFailureKey(cfg)
	if key == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err == nil {
		if _, existed := g.entries[key]; existed {
			log.Printf("[LLM Endpoint Gate] clear key=%s after successful LLM result", key)
			delete(g.entries, key)
		}
		return
	}
	if classifyLLMRetryError(err) != llmRetryErrorNetwork {
		return
	}
	g.entries[key] = llmEndpointFailureEntry{
		expiresAt: g.now().Add(g.ttl),
		reason:    err.Error(),
	}
	log.Printf("[LLM Endpoint Gate] record network failure key=%s ttl=%s reason=%q", key, g.ttl, truncateForLogGUI(err.Error(), 160))
}

func (a *App) getLLMEndpointFailureGate() *llmEndpointFailureGate {
	if a == nil {
		return nil
	}
	a.llmEndpointFailuresOnce.Do(func() {
		a.llmEndpointFailures = newLLMEndpointFailureGate(llmEndpointNetworkFailureTTL)
	})
	return a.llmEndpointFailures
}

func (a *App) shouldSkipLightweightLLM(cfg corelib.MaclawLLMConfig) (string, bool) {
	gate := a.getLLMEndpointFailureGate()
	if gate == nil {
		return "", false
	}
	return gate.shouldSkip(cfg)
}

func (a *App) observeLLMEndpointResult(cfg corelib.MaclawLLMConfig, err error) {
	if gate := a.getLLMEndpointFailureGate(); gate != nil {
		gate.observe(cfg, err)
	}
}

func llmEndpointFailureKey(cfg corelib.MaclawLLMConfig) string {
	rawURL := strings.TrimSpace(cfg.URL)
	model := strings.TrimSpace(cfg.Model)
	if rawURL == "" || model == "" {
		return ""
	}
	normalizedURL := strings.ToLower(strings.TrimRight(rawURL, "/"))
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		normalizedURL = strings.ToLower(parsed.Scheme + "://" + parsed.Host + path)
	}
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI))
	provider := strings.ToLower(strings.TrimSpace(cfg.ProviderName))
	return fmt.Sprintf("%s|protocol=%s|wire=%s|provider=%s|model=%s", normalizedURL, protocol, wireAPI, provider, strings.ToLower(model))
}
