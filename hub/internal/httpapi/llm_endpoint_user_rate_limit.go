package httpapi

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const llmEndpointUserLimiterShardCount = 64

// Opportunistic cleanup: drop idle buckets to bound memory.
const (
	llmEndpointUserBucketIdleTTL     = 10 * time.Minute
	llmEndpointUserBucketMaxPerShard = 4096
)

type llmEndpointUserLimiter struct {
	perMinute atomic.Int64
	burst     atomic.Int64
	maxWaitMS atomic.Int64
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
	MaxWaitMS int
}

type llmEndpointUserRateLimitResult struct {
	Allowed           bool
	Waited            time.Duration
	RetryAfterSeconds int64
	// Canceled is true when the request context ended while waiting for a token.
	Canceled bool
}

var globalLLMEndpointUserLimiter = newLLMEndpointUserLimiter()

func newLLMEndpointUserLimiter() *llmEndpointUserLimiter {
	l := &llmEndpointUserLimiter{}
	l.perMinute.Store(int64(im.DefaultLLMProviderUserRateLimitPerMinute))
	l.burst.Store(int64(im.DefaultLLMProviderUserRateLimitBurst))
	l.maxWaitMS.Store(int64(im.DefaultLLMProviderUserRateLimitMaxWaitMS))
	for i := range l.shards {
		l.shards[i].states = map[string]*llmEndpointUserBucket{}
	}
	return l
}

func applyLLMEndpointUserRateLimitConfig(reg *im.LLMProviderRegistry) {
	globalLLMEndpointUserLimiter.applyRegistry(reg)
}

func (l *llmEndpointUserLimiter) applyRegistry(reg *im.LLMProviderRegistry) {
	perMinute, burst, maxWaitMS := llmEndpointUserLimitFromRegistry(reg)
	l.perMinute.Store(int64(perMinute))
	l.burst.Store(int64(burst))
	l.maxWaitMS.Store(int64(maxWaitMS))
}

func llmEndpointUserLimitFromRegistry(reg *im.LLMProviderRegistry) (perMinute, burst, maxWaitMS int) {
	perMinute = im.DefaultLLMProviderUserRateLimitPerMinute
	burst = im.DefaultLLMProviderUserRateLimitBurst
	maxWaitMS = im.DefaultLLMProviderUserRateLimitMaxWaitMS
	if reg != nil {
		if reg.UserRateLimitPerMinute > 0 {
			perMinute = reg.UserRateLimitPerMinute
		}
		if reg.UserRateLimitBurst > 0 {
			burst = reg.UserRateLimitBurst
		}
		if reg.UserRateLimitMaxWaitMS > 0 {
			maxWaitMS = reg.UserRateLimitMaxWaitMS
		}
	}
	return perMinute, burst, clampLLMEndpointUserRateLimitMaxWaitMS(maxWaitMS)
}

func (l *llmEndpointUserLimiter) allow(email string) bool {
	return l.allowWithLimit(email, int(l.perMinute.Load()), int(l.burst.Load()))
}

func (l *llmEndpointUserLimiter) allowForRegistry(email string, reg *im.LLMProviderRegistry) bool {
	perMinute, burst, _ := llmEndpointUserLimitFromRegistry(reg)
	return l.allowWithLimit(email, perMinute, burst)
}

// acquireForRegistry waits (up to configured max wait) for a rate-limit token
// before rejecting. This turns short bursts into local queueing instead of 429s.
func (l *llmEndpointUserLimiter) acquireForRegistry(ctx context.Context, email string, reg *im.LLMProviderRegistry) llmEndpointUserRateLimitResult {
	perMinute, burst, maxWaitMS := llmEndpointUserLimitFromRegistry(reg)
	return l.acquireWithLimit(ctx, email, perMinute, burst, maxWaitMS)
}

func (l *llmEndpointUserLimiter) allowWithLimit(email string, perMinute, burst int) bool {
	return l.tryTakeToken(email, perMinute, burst).ok
}

type llmEndpointTokenAttempt struct {
	ok         bool
	retryAfter time.Duration
	tokens     float64
	disabled   bool
}

func (l *llmEndpointUserLimiter) tryTakeToken(email string, perMinute, burst int) llmEndpointTokenAttempt {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return llmEndpointTokenAttempt{ok: true, disabled: true}
	}
	if burst <= 0 || perMinute <= 0 {
		return llmEndpointTokenAttempt{ok: true, disabled: true}
	}
	shard := l.shard(email)
	shard.mu.Lock()
	defer shard.mu.Unlock()
	if shard.states == nil {
		shard.states = map[string]*llmEndpointUserBucket{}
	}
	now := time.Now()
	// Bound memory under long-lived hubs with many unique accounts.
	if len(shard.states) > llmEndpointUserBucketMaxPerShard {
		pruneIdleLLMEndpointUserBuckets(shard.states, now)
		if len(shard.states) > llmEndpointUserBucketMaxPerShard {
			evictOldestLLMEndpointUserBuckets(shard.states, len(shard.states)-llmEndpointUserBucketMaxPerShard)
		}
	}
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
		need := 1 - state.tokens
		rate := float64(perMinute) / 60.0
		if rate <= 0 {
			return llmEndpointTokenAttempt{ok: false, retryAfter: time.Second}
		}
		// Ceil to whole milliseconds so we never under-wait and spin.
		waitMS := math.Ceil(need / rate * 1000.0)
		if waitMS < 1 {
			waitMS = 1
		}
		return llmEndpointTokenAttempt{ok: false, retryAfter: time.Duration(waitMS) * time.Millisecond, tokens: state.tokens}
	}
	state.tokens--
	return llmEndpointTokenAttempt{ok: true, tokens: state.tokens}
}

func pruneIdleLLMEndpointUserBuckets(states map[string]*llmEndpointUserBucket, now time.Time) {
	for key, state := range states {
		if state == nil || now.Sub(state.last) > llmEndpointUserBucketIdleTTL {
			delete(states, key)
		}
	}
}

// evictOldestLLMEndpointUserBuckets drops the oldest n buckets when idle prune
// is not enough to stay under the per-shard cap (many concurrent unique users).
func evictOldestLLMEndpointUserBuckets(states map[string]*llmEndpointUserBucket, n int) {
	if n <= 0 || len(states) == 0 {
		return
	}
	type entry struct {
		key  string
		last time.Time
	}
	items := make([]entry, 0, len(states))
	for key, state := range states {
		last := time.Time{}
		if state != nil {
			last = state.last
		}
		items = append(items, entry{key: key, last: last})
	}
	// Partial selection sort is fine for rare overflow cleanup.
	for i := 0; i < n && i < len(items); i++ {
		oldest := i
		for j := i + 1; j < len(items); j++ {
			if items[j].last.Before(items[oldest].last) {
				oldest = j
			}
		}
		items[i], items[oldest] = items[oldest], items[i]
		delete(states, items[i].key)
	}
}

func clampLLMEndpointUserRateLimitMaxWaitMS(maxWaitMS int) int {
	if maxWaitMS <= 0 {
		maxWaitMS = im.DefaultLLMProviderUserRateLimitMaxWaitMS
	}
	if maxWaitMS > im.MaxLLMProviderUserRateLimitMaxWaitMS {
		return im.MaxLLMProviderUserRateLimitMaxWaitMS
	}
	return maxWaitMS
}

func (l *llmEndpointUserLimiter) acquireWithLimit(ctx context.Context, email string, perMinute, burst, maxWaitMS int) llmEndpointUserRateLimitResult {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	maxWaitMS = clampLLMEndpointUserRateLimitMaxWaitMS(maxWaitMS)
	deadline := start.Add(time.Duration(maxWaitMS) * time.Millisecond)

	for {
		// Fast path: context already done.
		if err := ctx.Err(); err != nil {
			return llmEndpointUserRateLimitResult{
				Allowed:           false,
				Waited:            time.Since(start),
				RetryAfterSeconds: 1,
				Canceled:          true,
			}
		}

		attempt := l.tryTakeToken(email, perMinute, burst)
		if attempt.ok || attempt.disabled {
			return llmEndpointUserRateLimitResult{Allowed: true, Waited: time.Since(start)}
		}

		now := time.Now()
		remainingBudget := deadline.Sub(now)
		if remainingBudget <= 0 {
			retrySec := int64(math.Ceil(attempt.retryAfter.Seconds()))
			if retrySec < 1 {
				retrySec = 1
			}
			return llmEndpointUserRateLimitResult{
				Allowed:           false,
				Waited:            time.Since(start),
				RetryAfterSeconds: retrySec,
			}
		}

		wait := attempt.retryAfter
		if wait <= 0 {
			wait = 50 * time.Millisecond
		}
		// Small jitter reduces thundering-herd wakeups when many requests wait
		// for the same refill instant.
		wait += time.Duration(rand.Intn(15)) * time.Millisecond
		if wait > remainingBudget {
			wait = remainingBudget
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return llmEndpointUserRateLimitResult{
				Allowed:           false,
				Waited:            time.Since(start),
				RetryAfterSeconds: 1,
				Canceled:          true,
			}
		case <-timer.C:
			// retry take
		}
	}
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
	return llmEndpointUserLimitSnapshot{
		PerMinute: int(l.perMinute.Load()),
		Burst:     int(l.burst.Load()),
		MaxWaitMS: int(l.maxWaitMS.Load()),
	}
}

func (l *llmEndpointUserLimiter) reset() {
	l.perMinute.Store(int64(im.DefaultLLMProviderUserRateLimitPerMinute))
	l.burst.Store(int64(im.DefaultLLMProviderUserRateLimitBurst))
	l.maxWaitMS.Store(int64(im.DefaultLLMProviderUserRateLimitMaxWaitMS))
	for i := range l.shards {
		l.shards[i].mu.Lock()
		l.shards[i].states = map[string]*llmEndpointUserBucket{}
		l.shards[i].mu.Unlock()
	}
}

// writeLLMEndpointUserRateLimited records access log + writes the denial response.
// Shared by chat/completions and responses handlers.
func writeLLMEndpointUserRateLimited(
	w http.ResponseWriter,
	system store.SystemSettingsRepository,
	email, clientIP, requestID string,
	startedAt time.Time,
	result llmEndpointUserRateLimitResult,
	extraMeta map[string]any,
) {
	retryAfter := result.RetryAfterSeconds
	if retryAfter < 1 {
		retryAfter = 1
	}
	status := http.StatusTooManyRequests
	code := "LLM_ENDPOINT_USER_RATE_LIMITED"
	msg := "user request rate exceeded; request waited in Hub queue but still timed out, please retry shortly"
	if result.Canceled {
		// Client/parent canceled while we were pacing — not a rate-limit exhaustion
		// signal for adaptive retry / scheduler pressure.
		status = http.StatusRequestTimeout
		code = "LLM_ENDPOINT_USER_RATE_LIMIT_WAIT_CANCELED"
		msg = "request canceled while waiting in Hub user rate-limit queue"
	} else {
		w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
	}
	meta := map[string]any{
		"request_id":           requestID,
		"elapsed_ms":           time.Since(startedAt).Milliseconds(),
		"failure_stage":        "hub_user_rate_limit",
		"retry_after_seconds":  retryAfter,
		"rate_limit_waited_ms": result.Waited.Milliseconds(),
	}
	if result.Canceled {
		meta["rate_limit_canceled"] = true
	}
	for k, v := range extraMeta {
		if strings.TrimSpace(k) == "" || v == nil {
			continue
		}
		meta[k] = v
	}
	enqueueLLMEndpointAccessLog(system, llmEndpointAccessLogEntry{
		Email:      email,
		ClientIP:   clientIP,
		StatusCode: status,
		ErrorCode:  code,
		CreatedAt:  time.Now().UTC(),
		Metadata:   meta,
	})
	fields := map[string]any{}
	if !result.Canceled {
		fields["retry_after_seconds"] = retryAfter
	}
	writeErrorWithFields(w, status, code, msg, fields)
}
