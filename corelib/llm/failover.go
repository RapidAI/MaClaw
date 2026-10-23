package llm

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// FailoverReason classifies why a failover was triggered.
type FailoverReason string

const (
	FailoverRateLimit   FailoverReason = "rate_limit"   // 429
	FailoverServerError FailoverReason = "server_error" // 5xx
	FailoverAuthError   FailoverReason = "auth_error"   // 401/403
	FailoverTimeout     FailoverReason = "timeout"
	FailoverNetwork     FailoverReason = "network"
)

// FailoverProvider holds a fallback LLM provider configuration.
type FailoverProvider struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol,omitempty"`
}

// FailoverChain manages an ordered list of fallback providers.
// Thread-safe for concurrent access.
type FailoverChain struct {
	mu        sync.RWMutex
	providers []FailoverProvider
	cooldowns map[string]time.Time // provider name → cooldown expiry
}

// NewFailoverChain creates a chain from the given providers.
func NewFailoverChain(providers []FailoverProvider) *FailoverChain {
	return &FailoverChain{
		providers: providers,
		cooldowns: make(map[string]time.Time),
	}
}

// IsEmpty returns true if no fallback providers are configured.
func (fc *FailoverChain) IsEmpty() bool {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.providers) == 0
}

// NextAvailable returns the first provider not in cooldown, or nil.
func (fc *FailoverChain) NextAvailable() *FailoverProvider {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	now := time.Now()
	for i := range fc.providers {
		name := fc.providers[i].Name
		if expiry, ok := fc.cooldowns[name]; ok && now.Before(expiry) {
			continue
		}
		p := fc.providers[i] // copy
		return &p
	}
	return nil
}

// MarkFailed puts a provider into cooldown. Duration depends on reason:
//   - rate_limit: 60s
//   - server_error: 30s
//   - auth_error: 300s (likely permanent, long cooldown)
//   - timeout/network: 15s
func (fc *FailoverChain) MarkFailed(name string, reason FailoverReason) {
	fc.MarkFailedWithCooldown(name, reason, 0)
}

// MarkFailedWithCooldown is MarkFailed with an explicit cooldown. A positive
// cooldown (e.g. from Retry-After) wins over the reason's default. Cap at 1h.
func (fc *FailoverChain) MarkFailedWithCooldown(name string, reason FailoverReason, cooldown time.Duration) {
	if cooldown <= 0 {
		switch reason {
		case FailoverRateLimit:
			cooldown = 60 * time.Second
		case FailoverServerError:
			cooldown = 30 * time.Second
		case FailoverAuthError:
			cooldown = 300 * time.Second
		default:
			cooldown = 15 * time.Second
		}
	}
	if cooldown > time.Hour {
		cooldown = time.Hour
	}
	fc.mu.Lock()
	fc.cooldowns[name] = time.Now().Add(cooldown)
	fc.mu.Unlock()
	log.Printf("[Failover] provider %q marked failed (%s), cooldown %v", name, reason, cooldown)
}

// MarkFailedFromError classifies err and, for an HTTP 429 with a Retry-After
// body field, uses that wait as the cooldown.
func (fc *FailoverChain) MarkFailedFromError(name string, err error) {
	reason := ClassifyError(err)
	if reason == "" {
		reason = FailoverNetwork
	}
	var wait time.Duration
	var se *HTTPStatusError
	if errors.As(err, &se) && se != nil {
		wait = HubRetryWait(se.Body, time.Now())
	}
	fc.MarkFailedWithCooldown(name, reason, wait)
}

// MarkSuccess clears cooldown for a provider.
func (fc *FailoverChain) MarkSuccess(name string) {
	fc.mu.Lock()
	delete(fc.cooldowns, name)
	fc.mu.Unlock()
}

// ClassifyHTTPError maps an HTTP status code to a FailoverReason.
func ClassifyHTTPError(statusCode int) FailoverReason {
	switch {
	case statusCode == http.StatusTooManyRequests:
		return FailoverRateLimit
	case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
		return FailoverAuthError
	case statusCode >= 500:
		return FailoverServerError
	default:
		return FailoverNetwork
	}
}

// ClassifyError maps an error string to a FailoverReason.
func ClassifyError(err error) FailoverReason {
	if err == nil {
		return ""
	}
	return classifyFailoverErrorMarker(err).Reason()
}

// ShouldFailover returns true if the error warrants trying a fallback provider.
func ShouldFailover(err error) bool {
	reason := ClassifyError(err)
	return reason == FailoverRateLimit ||
		reason == FailoverServerError ||
		reason == FailoverTimeout ||
		reason == FailoverNetwork
}

// FormatFailoverLog returns a human-readable log message for a failover event.
func FormatFailoverLog(fromProvider, toProvider string, reason FailoverReason, err error) string {
	return fmt.Sprintf("[Failover] %s → %s (reason=%s, error=%v)", fromProvider, toProvider, reason, err)
}
