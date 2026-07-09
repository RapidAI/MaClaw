package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageRecord represents token usage from a single API call.
type UsageRecord struct {
	Timestamp        time.Time `json:"ts"`
	PromptTokens     int64     `json:"p"`
	CompletionTokens int64     `json:"c"`
	TotalTokens      int64     `json:"t"`
	CacheHit         bool      `json:"h,omitempty"` // true if this was served from cache (saved tokens)
}

// TokenStatsSummary is the per-period aggregated result returned to the frontend.
type TokenStatsSummary struct {
	Period string `json:"period"` // "today", "week", "month", "all"

	// Actual tokens consumed (sent to upstream API, excluding cache hits).
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`

	// Total tokens if cache didn't exist (actual + saved by cache).
	PromptTokensBeforeCache     int64 `json:"prompt_before_cache"`
	CompletionTokensBeforeCache int64 `json:"completion_before_cache"`
	TotalTokensBeforeCache      int64 `json:"total_before_cache"`

	// Counts.
	RequestCount int `json:"request_count"`
	CacheHits    int `json:"cache_hits"`
	CacheMisses  int `json:"cache_misses"`

	// Derived: cache saving percentage (0-100).
	CacheSavingPct float64 `json:"cache_saving_pct"`
}

// UsageStore persists token usage records and provides time-period queries.
// Records are kept in memory (up to maxRecords) and flushed to disk periodically.
type UsageStore struct {
	mu       sync.Mutex
	records  []UsageRecord
	path     string
	dirty    bool
	flushCh  chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
}

const maxUsageRecords = 100_000 // ~30 days at ~2000 requests/day

func NewUsageStore(dir string) *UsageStore {
	s := &UsageStore{
		path:    filepath.Join(dir, "usage_stats.json"),
		flushCh: make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
	}
	s.loadFromDisk()
	go s.flushLoop()
	return s
}

// Record adds a usage entry for an actual upstream API call (cache miss).
func (s *UsageStore) Record(prompt, completion, total int64) {
	s.mu.Lock()
	s.records = append(s.records, UsageRecord{
		Timestamp:        time.Now(),
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CacheHit:         false,
	})
	s.trimLocked()
	s.mu.Unlock()
	s.signalFlush()
}

// RecordCacheHit adds a usage entry for a request that was served from cache.
// The token values represent what WOULD have been consumed without cache.
func (s *UsageStore) RecordCacheHit(prompt, completion, total int64) {
	s.mu.Lock()
	s.records = append(s.records, UsageRecord{
		Timestamp:        time.Now(),
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
		CacheHit:         true,
	})
	s.trimLocked()
	s.mu.Unlock()
	s.signalFlush()
}

// trimLocked evicts oldest records if over limit. Must be called with mu held.
func (s *UsageStore) trimLocked() {
	if len(s.records) > maxUsageRecords {
		s.records = s.records[len(s.records)-maxUsageRecords:]
	}
	s.dirty = true
}

func (s *UsageStore) signalFlush() {
	select {
	case s.flushCh <- struct{}{}:
	default:
	}
}

// Query returns aggregated token stats for the given period.
func (s *UsageStore) Query(period string) TokenStatsSummary {
	now := time.Now()
	var cutoff time.Time
	switch period {
	case "today":
		y, m, d := now.Date()
		cutoff = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "week":
		y, m, d := now.Date()
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7
		}
		cutoff = time.Date(y, m, d-weekday+1, 0, 0, 0, 0, now.Location())
	case "month":
		y, m, _ := now.Date()
		cutoff = time.Date(y, m, 1, 0, 0, 0, 0, now.Location())
	default: // "all"
		cutoff = time.Time{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	summary := TokenStatsSummary{Period: period}
	for i := len(s.records) - 1; i >= 0; i-- {
		r := s.records[i]
		if !cutoff.IsZero() && r.Timestamp.Before(cutoff) {
			break
		}
		summary.RequestCount++
		// "Before cache" = all requests (hit + miss) — what would be consumed without cache.
		summary.PromptTokensBeforeCache += r.PromptTokens
		summary.CompletionTokensBeforeCache += r.CompletionTokens
		summary.TotalTokensBeforeCache += r.TotalTokens
		if r.CacheHit {
			summary.CacheHits++
		} else {
			summary.CacheMisses++
			// "Actual" = only cache misses — what was actually sent to upstream.
			summary.PromptTokens += r.PromptTokens
			summary.CompletionTokens += r.CompletionTokens
			summary.TotalTokens += r.TotalTokens
		}
	}
	// Calculate saving percentage.
	if summary.TotalTokensBeforeCache > 0 {
		saved := summary.TotalTokensBeforeCache - summary.TotalTokens
		summary.CacheSavingPct = float64(saved) / float64(summary.TotalTokensBeforeCache) * 100
	}
	return summary
}

// Stop flushes remaining data and stops the background goroutine.
func (s *UsageStore) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.flush()
	})
}

func (s *UsageStore) flushLoop() {
	const debounce = 5 * time.Second
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.flushCh:
			timer := time.NewTimer(debounce)
		drainLoop:
			for {
				select {
				case <-s.flushCh:
				case <-timer.C:
					break drainLoop
				case <-s.stopCh:
					timer.Stop()
					return
				}
			}
			s.flush()
		case <-ticker.C:
			s.flush()
		case <-s.stopCh:
			return
		}
	}
}

func (s *UsageStore) flush() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	cutoff := time.Now().AddDate(0, 0, -30)
	n := 0
	for _, r := range s.records {
		if !r.Timestamp.Before(cutoff) {
			s.records[n] = r
			n++
		}
	}
	s.records = s.records[:n]
	snapshot := make([]UsageRecord, len(s.records))
	copy(snapshot, s.records)
	s.dirty = false
	s.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	dir := filepath.Dir(s.path)
	_ = os.MkdirAll(dir, 0o700)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		_ = os.WriteFile(s.path, data, 0o600)
	}
}

func (s *UsageStore) loadFromDisk() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var records []UsageRecord
	if json.Unmarshal(data, &records) == nil {
		s.records = records
	}
}
