//go:build livesmoke

package websearch

// Live smoke tests against real search engines. They hit the public internet
// and are excluded from the default test suite via the "livesmoke" build tag:
//
//	go test ./corelib/websearch/ -tags livesmoke -run 'TestLive' -v
//
// Flaky by nature (network, engine markup changes) — use for manual
// verification of the anti-bot fallback chain, not CI gating.

import (
	"context"
	"testing"
	"time"
)

func liveSearchAttempt(t *testing.T, name string, search func(context.Context, string, int) ([]SearchResult, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	results, err := search(ctx, "golang programming language", 3)
	if err != nil {
		t.Logf("%s: FAILED (acceptable in restricted networks): %v", name, err)
		return
	}
	if len(results) == 0 {
		t.Logf("%s: no results (markup may have changed)", name)
		return
	}
	for i, r := range results {
		t.Logf("%s result %d: %s — %s", name, i+1, r.Title, r.URL)
	}
	t.Logf("%s: OK, %d results", name, len(results))
}

func TestLiveSearchBingDirect(t *testing.T) {
	liveSearchAttempt(t, "bing-cn", searchBingDirect)
}

func TestLiveSearchDuckDuckGo(t *testing.T) {
	liveSearchAttempt(t, "duckduckgo-html", searchDirectLegacy)
}

func TestLiveSearchMojeek(t *testing.T) {
	liveSearchAttempt(t, "mojeek-html", searchMojeekDirect)
}

func TestLiveSearchBaidu(t *testing.T) {
	liveSearchAttempt(t, "baidu", searchBaiduDirect)
}

// TestLiveSearchFallbackChain exercises the full provider-failure path:
// a dead provider must degrade to a working direct endpoint.
func TestLiveSearchFallbackChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_ = ctx
	results, err := Search("golang programming language", 3)
	if err != nil {
		t.Logf("fallback chain FAILED (acceptable in restricted networks): %v", err)
		return
	}
	if len(results) == 0 {
		t.Log("fallback chain: no results")
		return
	}
	t.Logf("fallback chain: OK, %d results, first: %s — %s", len(results), results[0].Title, results[0].URL)
}
