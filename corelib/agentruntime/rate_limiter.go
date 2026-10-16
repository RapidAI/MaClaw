package agentruntime

import (
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// TenantTokenBucket is a small, transport-neutral token-bucket limiter used
// at the shared Agent turn admission boundary.  It deliberately accepts an
// explicit clock value so GUI, srv and tests observe identical deterministic
// semantics and hosts can keep request handling non-blocking.
//
// A zero or negative rate disables the limiter. The bucket map is bounded to
// prevent a stream of tenant ids from becoming an in-memory cardinality DoS;
// when full, the least-recently-used bucket is evicted before admitting the
// new tenant. A deployment that needs strict cross-process fairness should
// provide a distributed admission implementation at the host boundary while
// retaining this contract for local/offline hosts.
type TenantTokenBucket struct {
	rate        float64
	burst       float64
	tenantLimit int

	mu      sync.Mutex
	buckets map[string]tokenBucketState
	evicted atomic.Uint64
}

// TokenBucketConfig is a transport-safe description of the local admission
// policy. It contains no tenant state and can be embedded in capability
// snapshots consumed by GUI, srv and TUI.
type TokenBucketConfig struct {
	Enabled     bool    `json:"enabled"`
	Rate        float64 `json:"rate"`
	Burst       int     `json:"burst"`
	TenantLimit int     `json:"tenant_limit"`
}

type tokenBucketState struct {
	tokens float64
	last   time.Time
}

const defaultTenantTokenBucketLimit = 1024

// NewTenantTokenBucket creates a bounded per-tenant limiter. rate is tokens
// per second and burst is the maximum number of immediately admitted turns.
// When burst is non-positive but rate is enabled, a one-token burst is used.
// When tenantLimit is non-positive a conservative default is used.
func NewTenantTokenBucket(rate float64, burst, tenantLimit int) *TenantTokenBucket {
	if tenantLimit <= 0 {
		tenantLimit = defaultTenantTokenBucketLimit
	}
	if burst <= 0 && rate > 0 {
		burst = 1
	}
	if burst < 0 {
		burst = 0
	}
	return &TenantTokenBucket{
		rate:        rate,
		burst:       float64(burst),
		tenantLimit: tenantLimit,
		buckets:     make(map[string]tokenBucketState),
	}
}

// Enabled reports whether this limiter can reject a request.
func (l *TenantTokenBucket) Enabled() bool {
	return l != nil && l.rate > 0 && l.burst > 0
}

// Config returns the immutable limiter configuration. A nil limiter is
// represented as disabled.
func (l *TenantTokenBucket) Config() TokenBucketConfig {
	if l == nil {
		return TokenBucketConfig{}
	}
	return TokenBucketConfig{Enabled: l.Enabled(), Rate: l.rate, Burst: int(l.burst), TenantLimit: l.tenantLimit}
}

// Allow consumes one token for tenantID. Empty tenant ids are never limited.
// RetryAfter is zero when the request is admitted or the limiter is disabled.
func (l *TenantTokenBucket) Allow(tenantID string, now time.Time) (allowed bool, retryAfter time.Duration) {
	allowed, retryAfter, _ = l.AllowWithStatus(tenantID, now)
	return allowed, retryAfter
}

// AllowWithStatus is Allow plus a cardinality signal for hosts that want to
// expose a scalar diagnostic when an older tenant bucket was evicted.
func (l *TenantTokenBucket) AllowWithStatus(tenantID string, now time.Time) (allowed bool, retryAfter time.Duration, evicted bool) {
	if l == nil || !l.Enabled() || strings.TrimSpace(tenantID) == "" {
		return true, 0, false
	}
	key := TenantMetricsHash(tenantID)
	if key == "" {
		return true, 0, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.tenantLimit {
			l.evictOldestLocked()
			evicted = true
		}
		state = tokenBucketState{tokens: l.burst, last: now}
	}
	if !state.last.IsZero() {
		elapsed := now.Sub(state.last).Seconds()
		if elapsed > 0 {
			state.tokens = math.Min(l.burst, state.tokens+elapsed*l.rate)
		}
	}
	state.last = now
	if state.tokens < 1 {
		waitSeconds := (1 - state.tokens) / l.rate
		if waitSeconds < 0 {
			waitSeconds = 0
		}
		// Round up to avoid returning a Retry-After value that is shorter
		// than the actual refill interval due to duration truncation.
		retryAfter = time.Duration(math.Ceil(waitSeconds * float64(time.Second)))
		if retryAfter <= 0 {
			retryAfter = time.Nanosecond
		}
		l.buckets[key] = state
		return false, retryAfter, evicted
	}
	state.tokens--
	l.buckets[key] = state
	return true, 0, evicted
}

func (l *TenantTokenBucket) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, state := range l.buckets {
		if oldestKey == "" || state.last.Before(oldest) || (state.last.Equal(oldest) && key < oldestKey) {
			oldestKey, oldest = key, state.last
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
		l.evicted.Add(1)
	}
}

// ConsumeTokenBucket applies one-token admission to a persisted bucket.
// Memory and SQLite limiters share this refill math so local and shared-file
// hosts cannot drift.
func ConsumeTokenBucket(tokens, burst, rate float64, last, now time.Time) (next float64, allowed bool, retryAfter time.Duration) {
	if rate <= 0 || burst <= 0 {
		return tokens, true, 0
	}
	if now.IsZero() {
		now = time.Now()
	}
	if last.IsZero() {
		tokens = burst
	} else {
		elapsed := now.Sub(last).Seconds()
		if elapsed > 0 {
			tokens = math.Min(burst, tokens+elapsed*rate)
		}
	}
	if tokens < 1 {
		waitSeconds := (1 - tokens) / rate
		if waitSeconds < 0 {
			waitSeconds = 0
		}
		retryAfter = time.Duration(math.Ceil(waitSeconds * float64(time.Second)))
		if retryAfter <= 0 {
			retryAfter = time.Nanosecond
		}
		return tokens, false, retryAfter
	}
	return tokens - 1, true, 0
}

// EvictedTenants returns the number of bucket evictions caused by the bounded
// tenant map. It is a scalar and therefore cannot create unbounded labels.
func (l *TenantTokenBucket) EvictedTenants() uint64 {
	if l == nil {
		return 0
	}
	return l.evicted.Load()
}
