package main

import (
	"sync"
	"time"
)

// surveySessionHint tracks users likely mid-survey so free-text replies can
// reach Hub without probing every short chat message.
// Hints are best-effort (TTL); missing hints only skip free-text probes.
type surveySessionHint struct {
	mu    sync.Mutex
	until map[string]time.Time // rate key -> approx expiry
}

func newSurveySessionHint() *surveySessionHint {
	return &surveySessionHint{until: map[string]time.Time{}}
}

func (h *surveySessionHint) active(key string, now time.Time) bool {
	if h == nil || key == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	exp, ok := h.until[key]
	if !ok {
		return false
	}
	if !exp.After(now) {
		delete(h.until, key)
		return false
	}
	return true
}

func (h *surveySessionHint) mark(key string, now time.Time, ttl time.Duration) {
	if h == nil || key == "" {
		return
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.until == nil {
		h.until = map[string]time.Time{}
	}
	h.until[key] = now.Add(ttl)
	// light prune
	if len(h.until) > 4096 {
		for k, exp := range h.until {
			if !exp.After(now) {
				delete(h.until, k)
			}
		}
	}
}

func (h *surveySessionHint) clear(key string) {
	if h == nil || key == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.until, key)
}
