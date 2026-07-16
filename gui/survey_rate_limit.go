package main

import (
	"sync"
	"time"
)

// surveyUserRateLimit implements design §9 simple per-user throttle (~2 msg/s).
// Pure decision: allow(key, now) → true if under limit within window.
type surveyUserRateLimit struct {
	mu      sync.Mutex
	// recent holds timestamps of accepted attempts per rate key.
	recent map[string][]time.Time
	// maxAccepted is how many attempts may succeed inside window (default 2).
	maxAccepted int
	// window is the sliding window duration (default 1s).
	window time.Duration
}

func newSurveyUserRateLimit() *surveyUserRateLimit {
	return &surveyUserRateLimit{
		recent:      map[string][]time.Time{},
		maxAccepted: 2,
		window:      time.Second,
	}
}

// allow returns true if this attempt is under the rate limit and records it.
// key should be stable per user (e.g. platform + userID).
func (l *surveyUserRateLimit) allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	if key == "" {
		return true
	}
	max := l.maxAccepted
	if max <= 0 {
		max = 2
	}
	win := l.window
	if win <= 0 {
		win = time.Second
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.recent == nil {
		l.recent = map[string][]time.Time{}
	}
	cutoff := now.Add(-win)
	prev := l.recent[key]
	kept := prev[:0]
	for _, t := range prev {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= max {
		l.recent[key] = kept
		return false
	}
	l.recent[key] = append(kept, now)
	return true
}

// surveyRateKey builds a stable throttle key for a platform user.
func surveyRateKey(platform, userID string) string {
	if platform == "" {
		platform = "lansenger"
	}
	return platform + ":" + userID
}
