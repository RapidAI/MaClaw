package guiapp

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/reposcanner"
)

func TestResolveRepoOverviewCacheIncludesBudgetAndStaysBounded(t *testing.T) {
	var calls atomic.Int32
	resolver := NewSessionContextResolver(nil)
	resolver.scanOverview = func(_ string, cfg reposcanner.ScanConfig) (*reposcanner.ScanResult, error) {
		calls.Add(1)
		return &reposcanner.ScanResult{CompressedMD: fmt.Sprintf("budget-%d", cfg.TokenBudget)}, nil
	}

	if got := resolver.ResolveRepoOverview("/project", 100); got != "budget-100" {
		t.Fatalf("first overview = %q", got)
	}
	if got := resolver.ResolveRepoOverview("/project", 200); got != "budget-200" {
		t.Fatalf("second budget overview = %q", got)
	}
	if got := resolver.ResolveRepoOverview("/project", 100); got != "budget-100" {
		t.Fatalf("cached overview = %q", got)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("scan calls = %d, want 2", got)
	}

	for i := 0; i < maxCachedRepoOverviews; i++ {
		resolver.cacheOverview(overviewCacheKey{projectPath: "/project", tokenBudget: 1000 + i}, "cached")
	}
	resolver.overviewMu.Lock()
	size := len(resolver.overviewCache)
	resolver.overviewMu.Unlock()
	if size != maxCachedRepoOverviews {
		t.Fatalf("cache size = %d, want %d", size, maxCachedRepoOverviews)
	}
	if _, ok := resolver.cachedOverview(overviewCacheKey{projectPath: "/project", tokenBudget: 100}); ok {
		t.Fatal("least recently used budget was not evicted")
	}
}

func TestResolveRepoOverviewSingleflightDoesNotHoldCacheLockDuringScan(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	var calls atomic.Int32
	resolver := NewSessionContextResolver(nil)
	resolver.scanOverview = func(_ string, cfg reposcanner.ScanConfig) (*reposcanner.ScanResult, error) {
		calls.Add(1)
		if cfg.TokenBudget == 100 {
			startOnce.Do(func() { close(started) })
			<-release
			return &reposcanner.ScanResult{CompressedMD: "slow"}, nil
		}
		return &reposcanner.ScanResult{CompressedMD: "fast"}, nil
	}

	const waiters = 8
	results := make(chan string, waiters)
	for i := 0; i < waiters; i++ {
		go func() { results <- resolver.ResolveRepoOverview("/project", 100) }()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow scan did not start")
	}

	fastResult := make(chan string, 1)
	go func() { fastResult <- resolver.ResolveRepoOverview("/project", 200) }()
	select {
	case got := <-fastResult:
		if got != "fast" {
			t.Fatalf("independent overview = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("independent scan blocked behind repository I/O")
	}
	time.Sleep(30 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("scans before release = %d, want one per key", got)
	}

	close(release)
	for i := 0; i < waiters; i++ {
		select {
		case got := <-results:
			if got != "slow" {
				t.Fatalf("shared overview = %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("shared scan did not finish")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("total scans = %d, want 2", got)
	}
}
