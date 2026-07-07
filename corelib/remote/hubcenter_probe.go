package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Failure Memory ─────────────────────────────────────────────────────────
// Tracks recent probe failures per HubCenter URL. Nodes with high failure rates
// get a score penalty that counteracts the preferred +5 bonus, preventing
// unstable nodes from being permanently locked in as the preferred choice.

const (
	failureMemoryWindow   = 10               // number of recent probe results to remember
	failureMemoryMaxAge   = 30 * time.Minute // discard entries older than this
	failurePenaltyPerFail = 3                // score penalty per failure in the window
	maxFailurePenalty     = 15               // cap: even fully broken nodes don't get -∞
	failureMemoryMaxURLs  = 50               // GC trigger: evict stale URLs when map exceeds this
)

type probeRecord struct {
	ts      time.Time
	success bool
}

var failureMemory struct {
	mu      sync.RWMutex
	records map[string][]probeRecord // key: normalized URL
}

// RecordProbeResult records a probe outcome (success/failure) for a HubCenter URL.
func RecordProbeResult(url string, success bool) {
	url = NormalizeHubCenterURL(url)
	if url == "" {
		return
	}
	failureMemory.mu.Lock()
	defer failureMemory.mu.Unlock()
	if failureMemory.records == nil {
		failureMemory.records = make(map[string][]probeRecord)
	}
	now := time.Now()
	records := failureMemory.records[url]
	// Build fresh slice (new allocation to avoid aliasing the stored slice).
	cutoff := now.Add(-failureMemoryMaxAge)
	fresh := make([]probeRecord, 0, len(records)+1)
	for _, r := range records {
		if r.ts.After(cutoff) {
			fresh = append(fresh, r)
		}
	}
	// Append new record, cap at window size.
	fresh = append(fresh, probeRecord{ts: now, success: success})
	if len(fresh) > failureMemoryWindow {
		fresh = fresh[len(fresh)-failureMemoryWindow:]
	}
	failureMemory.records[url] = fresh

	// Periodic GC: when map grows large, evict URLs with no recent records.
	// This prevents unbounded growth from transient discovery URLs.
	if len(failureMemory.records) > failureMemoryMaxURLs {
		for k, recs := range failureMemory.records {
			if len(recs) == 0 {
				delete(failureMemory.records, k)
				continue
			}
			// Evict if newest record is older than maxAge.
			if recs[len(recs)-1].ts.Before(cutoff) {
				delete(failureMemory.records, k)
			}
		}
	}
}

// failurePenalty returns the score penalty for a URL based on recent failures.
// Returns 0 if no failure history.
func failurePenalty(url string) int {
	url = NormalizeHubCenterURL(url)
	if url == "" {
		return 0
	}
	failureMemory.mu.RLock()
	defer failureMemory.mu.RUnlock()
	records := failureMemory.records[url]
	if len(records) == 0 {
		return 0
	}
	cutoff := time.Now().Add(-failureMemoryMaxAge)
	failures := 0
	for _, r := range records {
		if r.ts.After(cutoff) && !r.success {
			failures++
		}
	}
	penalty := failures * failurePenaltyPerFail
	if penalty > maxFailurePenalty {
		penalty = maxFailurePenalty
	}
	return penalty
}

// ResetFailureMemory clears all failure history. Useful for tests.
func ResetFailureMemory() {
	failureMemory.mu.Lock()
	defer failureMemory.mu.Unlock()
	failureMemory.records = nil
}

// CenterQuality holds the quality probe result for a single HubCenter node.
type CenterQuality struct {
	Reachable    bool
	Routable     bool
	QualityScore int
	CanResolve   bool
	RTTMs        int64
}

// centerQualityResponse mirrors the JSON returned by GET /api/client/quality.
type centerQualityResponse struct {
	QualityScore  int    `json:"quality_score"`
	Routable      bool   `json:"routable"`
	ServiceStatus string `json:"service_status"`
	Features      struct {
		CanResolve bool `json:"can_resolve"`
	} `json:"features"`
}

// ProbeCenterQuality probes a single HubCenter node's quality.
// Timeout is 3 seconds. Mirrors hub/internal/center/service.go probeCenterQuality.
// Records the probe outcome into failure memory for future penalty calculations.
func ProbeCenterQuality(ctx context.Context, client *http.Client, baseURL string) CenterQuality {
	var result CenterQuality
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	baseURL = NormalizeHubCenterURL(baseURL)
	if baseURL == "" {
		return result
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, baseURL+"/api/client/quality", nil)
	if err != nil {
		RecordProbeResult(baseURL, false)
		return result
	}
	resp, err := client.Do(req)
	if err != nil {
		RecordProbeResult(baseURL, false)
		return result
	}
	defer resp.Body.Close()
	result.RTTMs = time.Since(start).Milliseconds()
	if result.RTTMs <= 0 {
		result.RTTMs = 1
	}

	if resp.StatusCode != http.StatusOK {
		RecordProbeResult(baseURL, false)
		return result
	}
	var payload centerQualityResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		RecordProbeResult(baseURL, false)
		return result
	}
	result.Reachable = true
	result.Routable = payload.Routable
	result.QualityScore = payload.QualityScore
	result.CanResolve = payload.Features.CanResolve
	RecordProbeResult(baseURL, true)
	return result
}

type rankedCenter struct {
	URL       string
	Quality   CenterQuality
	Preferred bool
}

// SelectBestCenter concurrently probes all HubCenter URLs and returns them
// sorted by quality (best first). preferred is the last successfully used URL
// and gets a +5 score bonus. Sorting matches hub/internal/center/service.go
// orderedCenterBaseURLs.
func SelectBestCenter(ctx context.Context, client *http.Client, urls []string, preferred string) []string {
	urls = NormalizeHubCenterURLs(urls)
	if len(urls) == 0 {
		return nil
	}
	if len(urls) == 1 {
		return urls
	}

	preferred = NormalizeHubCenterURL(preferred)

	if cached := loadCenterCache(urls, preferred); cached != nil {
		return cached
	}

	items := make([]rankedCenter, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			items[idx] = rankedCenter{
				URL:       url,
				Quality:   ProbeCenterQuality(ctx, client, url),
				Preferred: url == preferred,
			}
		}(i, u)
	}
	wg.Wait()

	// Snapshot failure penalties before sorting to ensure consistent comparisons.
	// Without this, concurrent RecordProbeResult calls could change penalties
	// between comparisons, violating sort's transitivity requirement.
	penalties := make(map[string]int, len(items))
	for _, item := range items {
		penalties[item.URL] = failurePenalty(item.URL)
	}

	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Quality.Reachable != b.Quality.Reachable {
			return a.Quality.Reachable
		}
		if a.Quality.Routable != b.Quality.Routable {
			return a.Quality.Routable
		}
		scoreA := a.Quality.QualityScore
		scoreB := b.Quality.QualityScore
		if a.Preferred {
			scoreA += 5
		}
		if b.Preferred {
			scoreB += 5
		}
		// Apply failure memory penalty: unstable nodes get penalized even if preferred.
		// Uses pre-snapshotted penalties for sort consistency.
		scoreA -= penalties[a.URL]
		scoreB -= penalties[b.URL]
		if scoreA != scoreB {
			return scoreA > scoreB
		}
		return a.URL < b.URL
	})

	ordered := make([]string, len(items))
	for i, item := range items {
		ordered[i] = item.URL
	}

	storeCenterCache(urls, preferred, ordered)
	return ordered
}

var centerCache struct {
	mu      sync.Mutex
	entries map[string]centerCacheEntry
}

type centerCacheEntry struct {
	results []string
	ts      time.Time
}

const centerCacheTTL = 15 * time.Second

func loadCenterCache(urls []string, preferred string) []string {
	key := centerCacheKey(urls, preferred)
	centerCache.mu.Lock()
	defer centerCache.mu.Unlock()
	if centerCache.entries == nil {
		return nil
	}
	entry, ok := centerCache.entries[key]
	if !ok || entry.results == nil || time.Since(entry.ts) > centerCacheTTL {
		return nil
	}
	cp := make([]string, len(entry.results))
	copy(cp, entry.results)
	return cp
}

func storeCenterCache(urls []string, preferred string, results []string) {
	key := centerCacheKey(urls, preferred)
	centerCache.mu.Lock()
	defer centerCache.mu.Unlock()
	if centerCache.entries == nil {
		centerCache.entries = make(map[string]centerCacheEntry)
	}
	cp := make([]string, len(results))
	copy(cp, results)
	centerCache.entries[key] = centerCacheEntry{results: cp, ts: time.Now()}
}

func centerCacheKey(urls []string, preferred string) string {
	copyURLs := append([]string(nil), NormalizeHubCenterURLs(urls)...)
	sort.Strings(copyURLs)
	return strings.Join(copyURLs, "|") + "#" + NormalizeHubCenterURL(preferred)
}

// InvalidateCenterCache clears the cached quality probe results.
// Useful for tests and when the user explicitly changes the HubCenter URL.
func InvalidateCenterCache() {
	centerCache.mu.Lock()
	defer centerCache.mu.Unlock()
	centerCache.entries = nil
}
