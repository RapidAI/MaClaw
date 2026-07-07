package httpapi

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// rankingCacheEntry holds pre-computed ranking data for a specific
// (tenantID, period, periodLabel) combination.
// Rows are sorted by TotalTokens descending (default dimension).
type rankingCacheEntry struct {
	Rows        []userRankingRow
	GeneratedAt time.Time
}

// tenantLister is the subset of store.TenantRepository used by the cache.
type tenantLister interface {
	List(ctx context.Context) ([]*store.Tenant, error)
}

// RankingCache pre-computes user rankings on a background timer so that
// HTTP handlers return instantly from cache instead of running expensive
// SQL aggregation queries on every request.
//
// Rankings are computed per-tenant: each tenant's ranking is independent.
// The background refresh covers all known tenants (from TenantRepository)
// so that any tenant's ranking page opens instantly.
//
// Thread safety: Get/GetOrCompute return a snapshot copy of the rows slice,
// safe to sort in-place without affecting other concurrent readers.
type RankingCache struct {
	sessions userUsageSummarizer
	users    store.UserRepository
	tenants  tenantLister // nil = only compute default tenant

	mu    sync.RWMutex
	cache map[string]*rankingCacheEntry // key = tenantID + "|" + period + "|" + label

	// inflight tracks on-demand computations in progress, keyed by cache key.
	// This prevents thundering herd per-key without globally serializing
	// unrelated keys (unlike a single computeMu).
	inflight   map[string]*inflightEntry
	inflightMu sync.Mutex

	refreshInterval time.Duration
	stopCh          chan struct{}
	stopped         chan struct{}
}

type inflightEntry struct {
	done chan struct{}       // closed when computation is complete
	result *rankingCacheEntry // nil if computation failed
}

// NewRankingCache creates a ranking cache that refreshes every interval.
// tenants may be nil (only default tenant is pre-computed; others computed on first request).
// Call Start() to begin background refresh and Stop() on shutdown.
func NewRankingCache(sessions userUsageSummarizer, users store.UserRepository, tenants tenantLister, interval time.Duration) *RankingCache {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	return &RankingCache{
		sessions:        sessions,
		users:           users,
		tenants:         tenants,
		cache:           make(map[string]*rankingCacheEntry),
		inflight:        make(map[string]*inflightEntry),
		refreshInterval: interval,
		stopCh:          make(chan struct{}),
		stopped:         make(chan struct{}),
	}
}

// Start begins background refresh. Non-blocking.
// The initial refresh runs asynchronously. Requests arriving before it completes
// will be handled by GetOrCompute (on-demand computation with caching).
func (rc *RankingCache) Start() {
	go rc.loop()
}

// Stop halts background refresh and waits for the goroutine to exit.
// Any in-flight refresh will be interrupted via stopCh (checked between tenants).
func (rc *RankingCache) Stop() {
	close(rc.stopCh)
	<-rc.stopped
}

func (rc *RankingCache) loop() {
	defer close(rc.stopped)
	// Initial refresh — warm up the cache before first ticker fires.
	rc.refresh()
	ticker := time.NewTicker(rc.refreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rc.stopCh:
			return
		case <-ticker.C:
			rc.refresh()
		}
	}
}

// refresh rebuilds rankings for all known tenants across current
// daily/weekly/monthly periods.
func (rc *RankingCache) refresh() {
	started := time.Now()
	now := started.UTC()

	// Derive context from stopCh so SQL queries are cancelled on shutdown.
	ctx, cancel := rc.contextFromStop()
	defer cancel()

	// Collect all tenant IDs to pre-compute.
	tenantIDs := rc.collectTenantIDs()

	periods := []struct {
		period string
		start  time.Time
		end    time.Time
		label  string
	}{
		{
			period: "daily",
			start:  time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC),
			end:    time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1),
			label:  now.Format("2006-01-02"),
		},
		{
			period: "monthly",
			start:  time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
			end:    time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0),
			label:  now.Format("2006-01"),
		},
		{
			period: "weekly",
			start:  weekStart(now),
			end:    weekStart(now).AddDate(0, 0, 7),
			label:  weekStart(now).Format("2006-01-02"),
		},
	}

	newCache := make(map[string]*rankingCacheEntry, len(tenantIDs)*len(periods))
	interrupted := false

	for _, tid := range tenantIDs {
		// Check for shutdown between tenants (early exit).
		select {
		case <-rc.stopCh:
			interrupted = true
			log.Printf("[ranking-cache] refresh interrupted by shutdown after %d/%d tenants", len(newCache)/len(periods), len(tenantIDs))
			goto commit
		default:
		}
		for _, p := range periods {
			entry := rc.computeEntry(ctx, tid, p.start, p.end, now)
			if entry != nil {
				key := tid + "|" + p.period + "|" + p.label
				newCache[key] = entry
			}
		}
	}

commit:
	// Write results even on partial completion — partial fresh data is
	// better than fully stale data during graceful shutdown.
	if len(newCache) > 0 {
		rc.mu.Lock()
		rc.cache = newCache
		rc.mu.Unlock()
	}

	elapsed := time.Since(started)
	if !interrupted {
		log.Printf("[ranking-cache] refreshed %d entries for %d tenants in %v", len(newCache), len(tenantIDs), elapsed.Round(time.Millisecond))
	}
}

// contextFromStop returns a context that is cancelled when stopCh is closed.
func (rc *RankingCache) contextFromStop() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-rc.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// collectTenantIDs returns all tenant IDs to pre-compute rankings for.
// Always includes DefaultTenantID. If TenantRepository is available,
// includes all tenants from the database.
func (rc *RankingCache) collectTenantIDs() []string {
	seen := map[string]struct{}{store.DefaultTenantID: {}}
	if rc.tenants != nil {
		tenants, err := rc.tenants.List(context.Background())
		if err != nil {
			log.Printf("[ranking-cache] failed to list tenants: %v (using default only)", err)
		} else {
			for _, t := range tenants {
				if t != nil && t.ID != "" {
					seen[t.ID] = struct{}{}
				}
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func weekStart(now time.Time) time.Time {
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return time.Date(now.Year(), now.Month(), now.Day()-(weekday-1), 0, 0, 0, 0, time.UTC)
}

func (rc *RankingCache) computeEntry(ctx context.Context, tenantID string, start, end, now time.Time) *rankingCacheEntry {
	if rc.sessions == nil {
		return nil
	}
	ctxTenant := store.WithTenant(ctx, tenantID)

	tokenRows, err := rc.sessions.SummarizeUserTokenUsage(ctxTenant, tenantID, start, end)
	if err != nil {
		if ctx.Err() == nil { // don't log if cancelled by shutdown
			log.Printf("[ranking-cache] SummarizeUserTokenUsage failed (tenant=%s): %v", tenantID, err)
		}
		return nil
	}
	durationRows, err := rc.sessions.SummarizeUserDurations(ctxTenant, tenantID, start, end, now)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("[ranking-cache] SummarizeUserDurations failed (tenant=%s): %v", tenantID, err)
		}
		return nil
	}

	merged := mergeUserRankingRows(ctxTenant, tenantID, rc.users, tokenRows, durationRows)
	assignUserRankingRanks(merged)
	// Store sorted by tokens (default dimension). Handlers copy and re-sort as needed.
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].TotalTokens > merged[j].TotalTokens
	})

	return &rankingCacheEntry{
		Rows:        merged,
		GeneratedAt: now,
	}
}

// Get retrieves a snapshot copy of cached ranking rows for the given parameters.
// Returns nil if cache miss. The returned slice is safe to sort in-place.
func (rc *RankingCache) Get(tenantID, period, label string) *rankingCacheEntry {
	key := tenantID + "|" + period + "|" + label
	rc.mu.RLock()
	entry := rc.cache[key]
	rc.mu.RUnlock()
	if entry == nil {
		return nil
	}
	return rc.copyEntry(entry)
}

// copyEntry returns a shallow copy of the entry with a copied rows slice.
func (rc *RankingCache) copyEntry(entry *rankingCacheEntry) *rankingCacheEntry {
	copied := make([]userRankingRow, len(entry.Rows))
	copy(copied, entry.Rows)
	return &rankingCacheEntry{
		Rows:        copied,
		GeneratedAt: entry.GeneratedAt,
	}
}

// GetOrCompute returns cached data if available, otherwise computes on-demand.
// Uses per-key deduplication to prevent thundering herd — if multiple goroutines
// request the same missing key, only one computes while others wait.
// The returned slice is a copy safe to sort in-place.
func (rc *RankingCache) GetOrCompute(ctx context.Context, tenantID, period, label string, start, end time.Time) *rankingCacheEntry {
	// Fast path: cache hit.
	entry := rc.Get(tenantID, period, label)
	if entry != nil {
		return entry
	}

	key := tenantID + "|" + period + "|" + label

	// Check if another goroutine is already computing this key.
	rc.inflightMu.Lock()
	if inf, ok := rc.inflight[key]; ok {
		// Another goroutine is computing — wait for it.
		rc.inflightMu.Unlock()
		select {
		case <-inf.done:
			if inf.result != nil {
				return rc.copyEntry(inf.result)
			}
			return nil
		case <-ctx.Done():
			return nil
		}
	}

	// We're the first — register inflight entry and compute.
	inf := &inflightEntry{done: make(chan struct{})}
	rc.inflight[key] = inf
	rc.inflightMu.Unlock()

	// Compute and store result.
	now := time.Now().UTC()
	computed := rc.computeEntry(ctx, tenantID, start, end, now)

	// Cache the result (even if nil — we'll just return nil).
	if computed != nil {
		rc.mu.Lock()
		rc.cache[key] = computed
		rc.mu.Unlock()
		inf.result = computed
	}

	// Unregister inflight and wake waiters.
	rc.inflightMu.Lock()
	delete(rc.inflight, key)
	rc.inflightMu.Unlock()
	close(inf.done)

	if computed == nil {
		return nil
	}
	return rc.copyEntry(computed)
}
