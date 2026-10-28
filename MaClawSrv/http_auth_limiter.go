package main

import (
	"strings"
	"sync"
	"time"
)

const (
	authLimiterMaxKeys         = 10000
	authLimiterCleanupInterval = time.Minute
)

type authFailureState struct {
	Count        int
	BlockedUntil time.Time
	LastFailure  time.Time
}

type authLimiter struct {
	mu                sync.Mutex
	limit             int
	window            time.Duration
	failureThreshold  int
	baseBlockDuration time.Duration
	maxBlockDuration  time.Duration
	buckets           map[string][]time.Time
	failures          map[string]authFailureState
}

func newAuthLimiter(limit int, window time.Duration) *authLimiter {
	l := &authLimiter{
		limit:             limit,
		window:            window,
		failureThreshold:  5,
		baseBlockDuration: time.Minute,
		maxBlockDuration:  15 * time.Minute,
		buckets:           map[string][]time.Time{},
		failures:          map[string]authFailureState{},
	}
	go l.cleanupLoop()
	return l
}

func (l *authLimiter) Allow(key string, now time.Time) bool {
	allowed, _ := l.AllowWithRetry(key, now)
	return allowed
}

func (l *authLimiter) AllowWithRetry(key string, now time.Time) (bool, time.Duration) {
	if l == nil || strings.TrimSpace(key) == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if retry := l.blockedRetryLocked(key, now); retry > 0 {
		return false, retry
	}
	if !l.canRetainKeyLocked(key, now) {
		return false, l.window
	}
	cutoff := now.Add(-l.window)
	items := l.buckets[key][:0]
	for _, ts := range l.buckets[key] {
		if ts.After(cutoff) {
			items = append(items, ts)
		}
	}
	if len(items) >= l.limit {
		l.buckets[key] = items
		return false, l.window
	}
	l.buckets[key] = append(items, now)
	return true, 0
}

func (l *authLimiter) RegisterFailure(key string, now time.Time) time.Duration {
	if l == nil || strings.TrimSpace(key) == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.canRetainKeyLocked(key, now) {
		return l.maxBlockDuration
	}
	state := l.failures[key]
	if !state.LastFailure.IsZero() && now.Sub(state.LastFailure) > l.window {
		state = authFailureState{}
	}
	state.Count++
	state.LastFailure = now
	if state.Count >= l.failureThreshold {
		steps := state.Count - l.failureThreshold
		block := l.baseBlockDuration << steps
		if block > l.maxBlockDuration {
			block = l.maxBlockDuration
		}
		state.BlockedUntil = now.Add(block)
		l.failures[key] = state
		return block
	}
	l.failures[key] = state
	return 0
}

func (l *authLimiter) ResetFailures(key string) {
	if l == nil || strings.TrimSpace(key) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func (l *authLimiter) cleanupLoop() {
	ticker := time.NewTicker(authLimiterCleanupInterval)
	defer ticker.Stop()
	for now := range ticker.C {
		l.mu.Lock()
		l.pruneLocked(now)
		l.mu.Unlock()
	}
}

func (l *authLimiter) canRetainKeyLocked(key string, now time.Time) bool {
	if _, ok := l.buckets[key]; ok {
		return true
	}
	if _, ok := l.failures[key]; ok {
		return true
	}
	if !l.atCapacityLocked() {
		return true
	}
	l.pruneLocked(now)
	return !l.atCapacityLocked()
}

func (l *authLimiter) atCapacityLocked() bool {
	if len(l.buckets)+len(l.failures) < authLimiterMaxKeys {
		return false
	}
	count := len(l.buckets)
	for key := range l.failures {
		if _, ok := l.buckets[key]; !ok {
			count++
			if count >= authLimiterMaxKeys {
				return true
			}
		}
	}
	return count >= authLimiterMaxKeys
}

func (l *authLimiter) pruneLocked(now time.Time) {
	cutoff := now.Add(-l.window)
	for key, timestamps := range l.buckets {
		items := timestamps[:0]
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				items = append(items, ts)
			}
		}
		if len(items) == 0 {
			delete(l.buckets, key)
		} else {
			l.buckets[key] = items
		}
	}
	for key, state := range l.failures {
		blocked := state.BlockedUntil.After(now)
		expired := state.LastFailure.IsZero() || now.Sub(state.LastFailure) > l.window
		if !blocked && expired {
			delete(l.failures, key)
		}
	}
}

func (l *authLimiter) blockedRetryLocked(key string, now time.Time) time.Duration {
	state, ok := l.failures[key]
	if !ok {
		return 0
	}
	if !state.BlockedUntil.After(now) {
		if !state.LastFailure.IsZero() && now.Sub(state.LastFailure) > l.window {
			delete(l.failures, key)
		} else {
			state.BlockedUntil = time.Time{}
			l.failures[key] = state
		}
		return 0
	}
	return time.Until(state.BlockedUntil)
}
