package httpapi

import (
	"net/http"
	"sync"
	"time"
)

// gossipRateLimiter provides per-key rate limiting for gossip public API endpoints.
// Uses a fixed-window counter approach with automatic cleanup of expired entries.
type gossipRateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rlEntry
	limit    int           // max requests per window
	window   time.Duration // sliding window duration
	lastGC   time.Time
	gcPeriod time.Duration
}

type rlEntry struct {
	count    int
	windowAt time.Time
}

func newGossipRateLimiter(limit int, window time.Duration) *gossipRateLimiter {
	return &gossipRateLimiter{
		entries:  make(map[string]*rlEntry),
		limit:    limit,
		window:   window,
		lastGC:   time.Now(),
		gcPeriod: 5 * time.Minute,
	}
}

// allow checks if the key is within rate limit. Returns true if allowed.
func (rl *gossipRateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Periodic cleanup of expired entries
	if now.Sub(rl.lastGC) > rl.gcPeriod {
		for k, e := range rl.entries {
			if now.Sub(e.windowAt) > rl.window {
				delete(rl.entries, k)
			}
		}
		rl.lastGC = now
	}

	e, ok := rl.entries[key]
	if !ok || now.Sub(e.windowAt) > rl.window {
		rl.entries[key] = &rlEntry{count: 1, windowAt: now}
		return true
	}
	if e.count >= rl.limit {
		return false
	}
	e.count++
	return true
}

// gossipRateLimitMiddleware wraps a handler with rate limiting by machine_id (from JSON body)
// or client IP as fallback. Returns 429 when limit exceeded.
func gossipRateLimitMiddleware(rl *gossipRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Use machine_id from query or X-Machine-ID header, fallback to client IP
		key := r.Header.Get("X-Machine-ID")
		if key == "" {
			key = clientIPFromRequest(r)
		}
		if !rl.allow(key) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests, please slow down")
			return
		}
		next(w, r)
	}
}
