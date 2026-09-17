package guiapp

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	// llmEndpointNetworkFailureTTL is the ban TTL for the main agent-stream
	// category.
	llmEndpointNetworkFailureTTL = 30 * time.Second
	// llmEndpointLightweightFailureTTL is the ban TTL for lightweight
	// categories (classification etc.). A wrong ban here degrades every
	// ambiguous turn, so keep the window short.
	llmEndpointLightweightFailureTTL = 10 * time.Second
	// llmEndpointMainStreamSuccessGrace is how much residual ban a lightweight
	// category keeps after a successful main-stream call on the same endpoint
	// prefix. Not a full clear: a different model behind the same prefix can
	// still be unhealthy, so leave a small buffer.
	llmEndpointMainStreamSuccessGrace = 2 * time.Second
)

// Endpoint call categories observed by the failure gate. Bans are tracked per
// category so a network failure in one category does not block another.
const (
	llmEndpointCategoryLightweightClassify = "lightweight-classify"
	llmEndpointCategoryUICTree             = "uic-tree"
	llmEndpointCategoryMainStream          = "main-stream"
)

type llmEndpointFailureGate struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]llmEndpointFailureEntry
}

type llmEndpointFailureEntry struct {
	expiresAt time.Time
	reason    string
	category  string
	prefix    string
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

// llmEndpointFailureBanTTL returns the ban TTL for a call category.
func llmEndpointFailureBanTTL(category string) time.Duration {
	switch category {
	case llmEndpointCategoryLightweightClassify, llmEndpointCategoryUICTree:
		return llmEndpointLightweightFailureTTL
	default:
		return llmEndpointNetworkFailureTTL
	}
}

func isLightweightEndpointCategory(category string) bool {
	return category == llmEndpointCategoryLightweightClassify || category == llmEndpointCategoryUICTree
}

func (g *llmEndpointFailureGate) shouldSkip(cfg corelib.MaclawLLMConfig, category string) (string, bool) {
	if g == nil {
		return "", false
	}
	key := llmEndpointFailureKey(cfg, category)
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
	log.Printf("[LLM Endpoint Gate] skip %s call key=%s remaining=%s reason=%q", category, key, entry.expiresAt.Sub(now).Round(time.Millisecond), truncateForLogGUI(entry.reason, 160))
	return entry.reason, true
}

// observe records the outcome of an endpoint call. budgetFired reports whether
// the caller's own context/deadline fired (ctx.Err() != nil at the call site):
// such an error says nothing about endpoint health and must never trip a ban —
// this is what turned one 2s budget timeout into a 30s ban on every
// lightweight call (2026-08-25 incident). User cancellation (ctx.Canceled)
// carries the same "not endpoint evidence" semantics. A successful result is
// always evidence: it clears the category's own ban, and a main-stream OR
// lightweight success additionally shortens every live lightweight ban on the
// same endpoint prefix to a 2s grace window — a recovered slow endpoint becomes
// usable by all lightweight categories immediately instead of after their TTL.
func (g *llmEndpointFailureGate) observe(cfg corelib.MaclawLLMConfig, category string, budgetFired bool, err error) {
	if g == nil {
		return
	}
	key := llmEndpointFailureKey(cfg, category)
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
		if category == llmEndpointCategoryMainStream || isLightweightEndpointCategory(category) {
			g.shortenLightweightBansLocked(llmEndpointPrefix(cfg), g.now().Add(llmEndpointMainStreamSuccessGrace))
		}
		return
	}
	if budgetFired {
		log.Printf("[LLM Endpoint Gate] ignore %s failure with caller budget/cancel fired key=%s reason=%q", category, key, truncateForLogGUI(err.Error(), 160))
		return
	}
	if classifyLLMRetryError(err) != llmRetryErrorNetwork {
		return
	}
	ttl := llmEndpointFailureBanTTL(category)
	g.entries[key] = llmEndpointFailureEntry{
		expiresAt: g.now().Add(ttl),
		reason:    err.Error(),
		category:  category,
		prefix:    llmEndpointPrefix(cfg),
	}
	log.Printf("[LLM Endpoint Gate] record %s network failure key=%s ttl=%s reason=%q", category, key, ttl, truncateForLogGUI(err.Error(), 160))
}

// observeLightweightSuccess records a successful lightweight call — including
// a detached slow-response read that finished in the background or was adopted
// (P0-3 positive health signal). It is deliberately the same code path as
// observe(cfg, category, false, nil): the category's own ban is cleared and
// every other live lightweight ban on the same endpoint prefix is shortened to
// the 2s grace window, exactly like a foreground lightweight success.
func (g *llmEndpointFailureGate) observeLightweightSuccess(cfg corelib.MaclawLLMConfig, category string) {
	g.observe(cfg, category, false, nil)
}

// shortenLightweightBansLocked caps every live lightweight ban under prefix at
// newExpiry. Caller must hold g.mu.
func (g *llmEndpointFailureGate) shortenLightweightBansLocked(prefix string, newExpiry time.Time) {
	for key, entry := range g.entries {
		if entry.prefix != prefix || !isLightweightEndpointCategory(entry.category) {
			continue
		}
		if !entry.expiresAt.After(newExpiry) {
			continue
		}
		entry.expiresAt = newExpiry
		g.entries[key] = entry
		log.Printf("[LLM Endpoint Gate] shorten %s ban key=%s to %s after main-stream success", entry.category, key, newExpiry.Sub(g.now()).Round(time.Millisecond))
	}
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

func (a *App) shouldSkipLightweightLLM(cfg corelib.MaclawLLMConfig, category string) (string, bool) {
	gate := a.getLLMEndpointFailureGate()
	if gate == nil {
		return "", false
	}
	return gate.shouldSkip(cfg, category)
}

func (a *App) observeLLMEndpointResult(cfg corelib.MaclawLLMConfig, category string, budgetFired bool, err error) {
	if gate := a.getLLMEndpointFailureGate(); gate != nil {
		gate.observe(cfg, category, budgetFired, err)
	}
}

func (a *App) observeLLMEndpointLightweightSuccess(cfg corelib.MaclawLLMConfig, category string) {
	if gate := a.getLLMEndpointFailureGate(); gate != nil {
		gate.observeLightweightSuccess(cfg, category)
	}
}

// llmEndpointPrefix identifies an endpoint ignoring model/wire differences:
// URL + protocol + provider. Main-stream success on one model is health
// evidence for the whole prefix, so lightweight bans are shortened per prefix.
func llmEndpointPrefix(cfg corelib.MaclawLLMConfig) string {
	rawURL := strings.TrimSpace(cfg.URL)
	if rawURL == "" {
		return ""
	}
	normalizedURL := strings.ToLower(strings.TrimRight(rawURL, "/"))
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
		path := strings.TrimRight(parsed.EscapedPath(), "/")
		normalizedURL = strings.ToLower(parsed.Scheme + "://" + parsed.Host + path)
	}
	protocol := strings.ToLower(strings.TrimSpace(cfg.Protocol))
	provider := strings.ToLower(strings.TrimSpace(cfg.ProviderName))
	return fmt.Sprintf("%s|protocol=%s|provider=%s", normalizedURL, protocol, provider)
}

func llmEndpointFailureKey(cfg corelib.MaclawLLMConfig, category string) string {
	if strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	prefix := llmEndpointPrefix(cfg)
	if prefix == "" {
		return ""
	}
	wireAPI := strings.ToLower(strings.TrimSpace(cfg.WireAPI))
	return fmt.Sprintf("%s|wire=%s|model=%s|category=%s", prefix, wireAPI, strings.ToLower(strings.TrimSpace(cfg.Model)), category)
}
