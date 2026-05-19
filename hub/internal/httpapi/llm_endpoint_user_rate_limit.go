package httpapi

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

const llmEndpointUserLimiterShardCount = 64

type llmEndpointUserLimiter struct {
	perMinute atomic.Int64
	burst     atomic.Int64
	shards    [llmEndpointUserLimiterShardCount]llmEndpointUserLimiterShard
}

type llmEndpointUserLimiterShard struct {
	mu     sync.Mutex
	states map[string]*llmEndpointUserBucket
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
	l := &llmEndpointUserLimiter{}
	l.perMinute.Store(int64(im.DefaultLLMProviderUserRateLimitPerMinute))
	l.burst.Store(int64(im.DefaultLLMProviderUserRateLimitBurst))
	for i := range l.shards {
		l.shards[i].states = map[string]*llmEndpointUserBucket{}
	}
	return l
}

func applyLLMEndpointUserRateLimitConfig(reg *im.LLMProviderRegistry) {
	globalLLMEndpointUserLimiter.applyRegistry(reg)
}

func (l *llmEndpointUserLimiter) applyRegistry(reg *im.LLMProviderRegistry) {
	perMinute, burst := llmEndpointUserLimitFromRegistry(reg)
	l.perMinute.Store(int64(perMinute))
	l.burst.Store(int64(burst))
}

func llmEndpointUserLimitFromRegistry(reg *im.LLMProviderRegistry) (int, int) {
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
	return perMinute, burst
}

func (l *llmEndpointUserLimiter) allow(email string) bool {
	return l.allowWithLimit(email, int(l.perMinute.Load()), int(l.burst.Load()))
}

func (l *llmEndpointUserLimiter) allowForRegistry(email string, reg *im.LLMProviderRegistry) bool {
	perMinute, burst := llmEndpointUserLimitFromRegistry(reg)
	return l.allowWithLimit(email, perMinute, burst)
}

func (l *llmEndpointUserLimiter) allowWithLimit(email string, perMinute, burst int) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return true
	}
	if burst <= 0 || perMinute <= 0 {
		return true
	}
	shard := l.shard(email)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.states == nil {
		shard.states = map[string]*llmEndpointUserBucket{}
	}
	now := time.Now()
	state := shard.states[email]
	if state == nil {
		state = &llmEndpointUserBucket{tokens: float64(burst), last: now}
		shard.states[email] = state
	}
	if state.last.IsZero() {
		state.last = now
	}
	elapsed := now.Sub(state.last).Seconds()
	if elapsed > 0 {
		state.tokens = math.Min(float64(burst), state.tokens+elapsed*(float64(perMinute)/60.0))
		state.last = now
	}
	if state.tokens < 1 {
		return false
	}
	state.tokens--
	return true
}

func (l *llmEndpointUserLimiter) shard(email string) *llmEndpointUserLimiterShard {
	var h uint32 = 2166136261
	for i := 0; i < len(email); i++ {
		h ^= uint32(email[i])
		h *= 16777619
	}
	return &l.shards[int(h%llmEndpointUserLimiterShardCount)]
}

func (l *llmEndpointUserLimiter) snapshot() llmEndpointUserLimitSnapshot {
	return llmEndpointUserLimitSnapshot{PerMinute: int(l.perMinute.Load()), Burst: int(l.burst.Load())}
}

func (l *llmEndpointUserLimiter) reset() {
	l.perMinute.Store(int64(im.DefaultLLMProviderUserRateLimitPerMinute))
	l.burst.Store(int64(im.DefaultLLMProviderUserRateLimitBurst))
	for i := range l.shards {
		l.shards[i].mu.Lock()
		l.shards[i].states = map[string]*llmEndpointUserBucket{}
		l.shards[i].mu.Unlock()
	}
}
