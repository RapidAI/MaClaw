package repomaint

import (
	"sync"
	"time"
)

// limiter is a per-process fixed window. The key is the hook token, not the
// source IP. Counters reset when the process restarts.
type limiter struct {
	mu        sync.Mutex
	perHook   int
	globalMax int
	global    bucket
	byToken   map[string]bucket
}

type bucket struct {
	window int64
	count  int
}

func newLimiter() *limiter {
	return &limiter{
		perHook:   defaultHookRatePerMin,
		globalMax: defaultGlobalRatePerMin,
		byToken:   map[string]bucket{},
	}
}

func (l *limiter) set(perHook, global int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if perHook > 0 {
		l.perHook = perHook
	}
	if global > 0 {
		l.globalMax = global
	}
}

// allow counts one request for a known hook token. Bad signatures are counted
// by the caller invoking this before signature verification.
func (l *limiter) allow(token string) bool {
	window := time.Now().Unix() / 60
	l.mu.Lock()
	defer l.mu.Unlock()
	if !bump(&l.global, window, l.globalMax) {
		return false
	}
	b := l.byToken[token]
	if !bump(&b, window, l.perHook) {
		l.byToken[token] = b
		return false
	}
	l.byToken[token] = b
	return true
}

func bump(b *bucket, window int64, max int) bool {
	if b.window != window {
		b.window = window
		b.count = 0
	}
	if b.count >= max {
		return false
	}
	b.count++
	return true
}
