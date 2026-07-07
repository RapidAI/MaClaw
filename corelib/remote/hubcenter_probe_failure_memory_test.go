package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFailurePenalty_NoHistory(t *testing.T) {
	ResetFailureMemory()
	penalty := failurePenalty("https://example.com")
	if penalty != 0 {
		t.Fatalf("penalty = %d, want 0 for no history", penalty)
	}
}

func TestFailurePenalty_AllSuccesses(t *testing.T) {
	ResetFailureMemory()
	for i := 0; i < 5; i++ {
		RecordProbeResult("https://example.com", true)
	}
	penalty := failurePenalty("https://example.com")
	if penalty != 0 {
		t.Fatalf("penalty = %d, want 0 for all successes", penalty)
	}
}

func TestFailurePenalty_MixedResults(t *testing.T) {
	ResetFailureMemory()
	// 3 failures out of 5 probes → penalty = 3 * 3 = 9
	RecordProbeResult("https://unstable.com", true)
	RecordProbeResult("https://unstable.com", false)
	RecordProbeResult("https://unstable.com", false)
	RecordProbeResult("https://unstable.com", true)
	RecordProbeResult("https://unstable.com", false)

	penalty := failurePenalty("https://unstable.com")
	if penalty != 9 {
		t.Fatalf("penalty = %d, want 9 (3 failures × 3)", penalty)
	}
}

func TestFailurePenalty_CappedAtMax(t *testing.T) {
	ResetFailureMemory()
	// 10 consecutive failures → penalty = 10 * 3 = 30, capped at 15
	for i := 0; i < 10; i++ {
		RecordProbeResult("https://dead.com", false)
	}
	penalty := failurePenalty("https://dead.com")
	if penalty != maxFailurePenalty {
		t.Fatalf("penalty = %d, want %d (capped)", penalty, maxFailurePenalty)
	}
}

func TestFailurePenalty_WindowSize(t *testing.T) {
	ResetFailureMemory()
	// Record 10 failures, then 10 successes → window only keeps last 10 (all successes)
	for i := 0; i < 10; i++ {
		RecordProbeResult("https://recovered.com", false)
	}
	for i := 0; i < 10; i++ {
		RecordProbeResult("https://recovered.com", true)
	}
	penalty := failurePenalty("https://recovered.com")
	if penalty != 0 {
		t.Fatalf("penalty = %d, want 0 (recovered node, window slid past failures)", penalty)
	}
}

func TestSelectBestCenter_FailurePenaltyOverridesPreferred(t *testing.T) {
	InvalidateCenterCache()
	ResetFailureMemory()

	// Both servers have same quality score (90).
	stable := httptest.NewServer(qualityHandler(90, true))
	defer stable.Close()
	unstable := httptest.NewServer(qualityHandler(90, true))
	defer unstable.Close()

	// Simulate history: unstable had 3 recent failures.
	RecordProbeResult(unstable.URL, false)
	RecordProbeResult(unstable.URL, false)
	RecordProbeResult(unstable.URL, false)

	// unstable is preferred (+5), but has penalty (-9). Net = 90+5-9 = 86.
	// stable has no penalty. Net = 90.
	// stable should win.
	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{unstable.URL, stable.URL}, unstable.URL)

	if len(ordered) != 2 {
		t.Fatalf("got %d results, want 2", len(ordered))
	}
	if ordered[0] != stable.URL {
		t.Fatalf("first = %q, want %q (stable should win over penalized preferred)", ordered[0], stable.URL)
	}
}

func TestSelectBestCenter_PreferredStillWinsWithoutFailures(t *testing.T) {
	InvalidateCenterCache()
	ResetFailureMemory()

	// a=90, b=92. a is preferred (+5 → 95 > 92). No failures → a wins.
	a := httptest.NewServer(qualityHandler(90, true))
	defer a.Close()
	b := httptest.NewServer(qualityHandler(92, true))
	defer b.Close()

	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{a.URL, b.URL}, a.URL)
	if ordered[0] != a.URL {
		t.Fatalf("preferred without failures should still win: first = %q, want %q", ordered[0], a.URL)
	}
}

func TestSelectBestCenter_RecoveredNodeRegainsPosition(t *testing.T) {
	InvalidateCenterCache()
	ResetFailureMemory()

	stable := httptest.NewServer(qualityHandler(85, true))
	defer stable.Close()
	recovering := httptest.NewServer(qualityHandler(90, true))
	defer recovering.Close()

	// Recovering node had failures but then succeeded many times.
	RecordProbeResult(recovering.URL, false)
	RecordProbeResult(recovering.URL, false)
	RecordProbeResult(recovering.URL, false)
	// 7 successes push failures out of window (window=10, 3 failures + 7 successes = 10 total, 3 failures remain)
	for i := 0; i < 7; i++ {
		RecordProbeResult(recovering.URL, true)
	}
	// penalty = 3 * 3 = 9. recovering score: 90 - 9 = 81. stable = 85. stable still wins.
	InvalidateCenterCache()
	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{stable.URL, recovering.URL}, "")
	if ordered[0] != stable.URL {
		t.Fatalf("recovering node still penalized: first = %q, want %q", ordered[0], stable.URL)
	}

	// After more successes, failures slide out of window entirely.
	for i := 0; i < 7; i++ {
		RecordProbeResult(recovering.URL, true)
	}
	// Now window has 10 successes (last 10). penalty = 0. recovering = 90 > stable = 85.
	InvalidateCenterCache()
	ordered = SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{stable.URL, recovering.URL}, "")
	if ordered[0] != recovering.URL {
		t.Fatalf("fully recovered node should win: first = %q, want %q", ordered[0], recovering.URL)
	}
}

func TestProbeCenterQuality_RecordsFailureOnUnreachable(t *testing.T) {
	ResetFailureMemory()
	url := "http://127.0.0.1:1" // unreachable
	ProbeCenterQuality(context.Background(), http.DefaultClient, url)

	penalty := failurePenalty(url)
	if penalty != failurePenaltyPerFail {
		t.Fatalf("penalty after one failed probe = %d, want %d", penalty, failurePenaltyPerFail)
	}
}

func TestProbeCenterQuality_RecordsSuccessOnHealthy(t *testing.T) {
	ResetFailureMemory()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quality_score":  80,
			"routable":       true,
			"service_status": "healthy",
			"features":       map[string]any{"can_resolve": true},
		})
	}))
	defer srv.Close()

	ProbeCenterQuality(context.Background(), http.DefaultClient, srv.URL)

	penalty := failurePenalty(srv.URL)
	if penalty != 0 {
		t.Fatalf("penalty after successful probe = %d, want 0", penalty)
	}
}

func TestFailureMemory_IsolatedPerURL(t *testing.T) {
	ResetFailureMemory()
	RecordProbeResult("https://bad.com", false)
	RecordProbeResult("https://bad.com", false)
	RecordProbeResult("https://good.com", true)

	if p := failurePenalty("https://bad.com"); p != 6 {
		t.Fatalf("bad.com penalty = %d, want 6", p)
	}
	if p := failurePenalty("https://good.com"); p != 0 {
		t.Fatalf("good.com penalty = %d, want 0", p)
	}
}

func TestFailureMemory_EmptyURLIgnored(t *testing.T) {
	ResetFailureMemory()
	RecordProbeResult("", false)
	// Should not panic or create entries.
	if p := failurePenalty(""); p != 0 {
		t.Fatalf("empty URL penalty = %d, want 0", p)
	}
}

func TestFailureMemory_OldEntriesEvicted(t *testing.T) {
	ResetFailureMemory()
	url := "https://old-failures.com"

	// Manually inject old records.
	failureMemory.mu.Lock()
	if failureMemory.records == nil {
		failureMemory.records = make(map[string][]probeRecord)
	}
	oldTime := time.Now().Add(-failureMemoryMaxAge - time.Minute) // older than max age
	failureMemory.records[url] = []probeRecord{
		{ts: oldTime, success: false},
		{ts: oldTime, success: false},
		{ts: oldTime, success: false},
	}
	failureMemory.mu.Unlock()

	// Old records should be evicted on next RecordProbeResult.
	RecordProbeResult(url, true)

	// Penalty should be 0 (old failures evicted, only the new success remains).
	penalty := failurePenalty(url)
	if penalty != 0 {
		t.Fatalf("penalty with only old failures = %d, want 0", penalty)
	}
}

func TestFailureMemory_GCEvictsStaleURLs(t *testing.T) {
	ResetFailureMemory()

	// Fill up the map with stale URLs beyond the GC threshold.
	// Use a counter suffix to guarantee unique URLs.
	failureMemory.mu.Lock()
	if failureMemory.records == nil {
		failureMemory.records = make(map[string][]probeRecord)
	}
	oldTime := time.Now().Add(-failureMemoryMaxAge - time.Minute)
	for i := 0; i < failureMemoryMaxURLs+10; i++ {
		url := NormalizeHubCenterURL("https://stale-" + string(rune('a'+i/100)) + string(rune('a'+i%100/10)) + string(rune('0'+i%10)) + ".example.com")
		failureMemory.records[url] = []probeRecord{{ts: oldTime, success: false}}
	}
	countBefore := len(failureMemory.records)
	failureMemory.mu.Unlock()

	if countBefore <= failureMemoryMaxURLs {
		t.Fatalf("setup error: only %d URLs inserted, need > %d", countBefore, failureMemoryMaxURLs)
	}

	// Recording a new result should trigger GC and evict stale entries.
	RecordProbeResult("https://fresh.example.com", true)

	failureMemory.mu.RLock()
	countAfter := len(failureMemory.records)
	failureMemory.mu.RUnlock()

	// After GC, stale URLs should be evicted. Only fresh URL should remain.
	if countAfter > failureMemoryMaxURLs {
		t.Fatalf("after GC: %d URLs in map, expected <= %d", countAfter, failureMemoryMaxURLs)
	}
	if countAfter >= countBefore {
		t.Fatalf("GC did not evict: before=%d after=%d", countBefore, countAfter)
	}
	// The fresh URL must survive.
	if p := failurePenalty("https://fresh.example.com"); p != 0 {
		t.Fatalf("fresh URL penalty = %d, want 0", p)
	}
}
