package httpapi

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

type llmEndpointUserLimiter struct {
	mu        sync.Mutex
	perMinute int
	burst     int
	states    map[string]*llmEndpointUserBucket
}

type llmEndpointUserBucket struct {
	tokens float64
	last   time.Time
}

type llmEndpointUserLimitSnapshot struct {
	PerMinute int
	Burst     int
}

var globalLLMEndpointUserLimiter = newLLMEndpointUserLimiter()

func newLLMEndpointUserLimiter() *llmEndpointUserLimiter {
	return &llmEndpointUserLimiter{
		perMinute: im.DefaultLLMProviderUserRateLimitPerMinute,
		burst:     im.DefaultLLMProviderUserRateLimitBurst,
		states:    map[string]*llmEndpointUserBucket{},
	}
}

func applyLLMEndpointUserRateLimitConfig(reg *im.LLMProviderRegistry) {
	globalLLMEndpointUserLimiter.applyRegistry(reg)
}

func (l *llmEndpointUserLimiter) applyRegistry(reg *im.LLMProviderRegistry) {
	perMinute := im.DefaultLLMProviderUserRateLimitPerMinute
	burst := im.DefaultLLMProviderUserRateLimitBurst
	if reg != nil {
		if reg.UserRateLimitPerMinute > 0 {
			perMinute = reg.UserRateLimitPerMinute
		}
		if reg.UserRateLimitBurst > 0 {
			burst = reg.UserRateLimitBurst
		}
	}
	l.mu.Lock()
	l.perMinute = perMinute
	l.burst = burst
	if l.states == nil {
		l.states = map[string]*llmEndpointUserBucket{}
	}
	l.mu.Unlock()
}

func (l *llmEndpointUserLimiter) allow(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.burst <= 0 || l.perMinute <= 0 {
		return true
	}
	if l.states == nil {
		l.states = map[string]*llmEndpointUserBucket{}
	}
	now := time.Now()
	state := l.states[email]
	if state == nil {
		state = &llmEndpointUserBucket{tokens: float64(l.burst), last: now}
		l.states[email] = state
	}
	if state.last.IsZero() {
		state.last = now
	}
	elapsed := now.Sub(state.last).Seconds()
	if elapsed > 0 {
		state.tokens = math.Min(float64(l.burst), state.tokens+elapsed*(float64(l.perMinute)/60.0))
		state.last = now
	}
	if state.tokens < 1 {
		return false
	}
	state.tokens--
	return true
}

func (l *llmEndpointUserLimiter) snapshot() llmEndpointUserLimitSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return llmEndpointUserLimitSnapshot{PerMinute: l.perMinute, Burst: l.burst}
}

func (l *llmEndpointUserLimiter) reset() {
	l.mu.Lock()
	l.perMinute = im.DefaultLLMProviderUserRateLimitPerMinute
	l.burst = im.DefaultLLMProviderUserRateLimitBurst
	l.states = map[string]*llmEndpointUserBucket{}
	l.mu.Unlock()
}
