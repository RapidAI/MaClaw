package main

import (
	"sync"
	"time"
)

// surveyUserRateLimit implements design §9 simple per-user throttle (~2 msg/s).
type surveyUserRateLimit struct {
	mu sync.Mutex
	// recent holds timestamps of accepted attempts per rate key.
	recent map[string][]time.Time
	// maxAccepted is how many attempts may succeed inside window (default 2).
	maxAccepted int
	// window is the sliding window duration (default 1s).
	window time.Duration
	// maxKeys caps map growth (evict idle keys when exceeded).
	maxKeys int
}

func newSurveyUserRateLimit() *surveyUserRateLimit {
	return &surveyUserRateLimit{
		recent:      map[string][]time.Time{},
		maxAccepted: 2,
		window:      time.Second,
		maxKeys:     4096,
	}
}

func (l *surveyUserRateLimit) params() (max int, win time.Duration) {
	max = l.maxAccepted
	if max <= 0 {
		max = 2
	}
	win = l.window
	if win <= 0 {
		win = time.Second
	}
	return max, win
}

// wouldAllow reports whether allow would succeed without recording.
func (l *surveyUserRateLimit) wouldAllow(key string, now time.Time) bool {
	if l == nil || key == "" {
		return true
	}
	max, win := l.params()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.recent == nil {
		return true
	}
	return l.countRecent(key, now, win) < max
}

// allow returns true if this attempt is under the rate limit and records it.
func (l *surveyUserRateLimit) allow(key string, now time.Time) bool {
	if l == nil {
		return true
	}
	if key == "" {
		return true
	}
	max, win := l.params()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.recent == nil {
		l.recent = map[string][]time.Time{}
	}
	n := l.countRecent(key, now, win)
	if n >= max {
		return false
	}
	l.recent[key] = append(l.pruneKey(key, now, win), now)
	l.evictIfNeeded(now, win)
	return true
}

// record counts a successful survey handle without denying (used after Hub handled=true
// for speculative session replies that skipped pre-call allow).
func (l *surveyUserRateLimit) record(key string, now time.Time) {
	if l == nil || key == "" {
		return
	}
	_, win := l.params()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.recent == nil {
		l.recent = map[string][]time.Time{}
	}
	l.recent[key] = append(l.pruneKey(key, now, win), now)
	l.evictIfNeeded(now, win)
}

func (l *surveyUserRateLimit) countRecent(key string, now time.Time, win time.Duration) int {
	kept := l.pruneKey(key, now, win)
	l.recent[key] = kept
	return len(kept)
}

func (l *surveyUserRateLimit) pruneKey(key string, now time.Time, win time.Duration) []time.Time {
	if l.recent == nil {
		return nil
	}
	prev := l.recent[key]
	cutoff := now.Add(-win)
	kept := prev[:0]
	for _, t := range prev {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.recent, key)
		return nil
	}
	return kept
}

func (l *surveyUserRateLimit) evictIfNeeded(now time.Time, win time.Duration) {
	maxKeys := l.maxKeys
	if maxKeys <= 0 {
		maxKeys = 4096
	}
	if len(l.recent) <= maxKeys {
		return
	}
	// Drop keys with no timestamps in window (and empty leftovers).
	for k := range l.recent {
		l.pruneKey(k, now, win)
		if len(l.recent) <= maxKeys {
			return
		}
	}
	// Still over: drop arbitrary keys until under cap.
	for k := range l.recent {
		delete(l.recent, k)
		if len(l.recent) <= maxKeys {
			return
		}
	}
}

// surveyRateKey builds a stable throttle key for a platform user.
func surveyRateKey(platform, userID string) string {
	if platform == "" {
		platform = "lansenger"
	}
	return platform + ":" + userID
}
