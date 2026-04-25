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
		return result
	}
	resp, err := client.Do(req)
	if err != nil {
		return result
	}
	defer resp.Body.Close()
	result.RTTMs = time.Since(start).Milliseconds()
	if result.RTTMs <= 0 {
		result.RTTMs = 1
	}

	if resp.StatusCode != http.StatusOK {
		return result
	}
	var payload centerQualityResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return result
	}
	result.Reachable = true
	result.Routable = payload.Routable
	result.QualityScore = payload.QualityScore
	result.CanResolve = payload.Features.CanResolve
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
