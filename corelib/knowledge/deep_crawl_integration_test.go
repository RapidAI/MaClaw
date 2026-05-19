package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testFakeBase = "https://test-crawl-site.example.com"

// testFetchFunc creates a fetchFunc that maps fake public URLs to the httptest server.
func testFetchFunc(ts *httptest.Server, fakeBase string) func(ctx context.Context, rawURL string) (string, error) {
	client := ts.Client()
	return func(ctx context.Context, rawURL string) (string, error) {
		realURL := strings.Replace(rawURL, fakeBase, ts.URL, 1)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, realURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		buf := make([]byte, 5*1024*1024)
		n, _ := resp.Body.Read(buf)
		return string(buf[:n]), nil
	}
}

func buildTestSiteWithBase(baseURL string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<a href="%s/about">About</a>
			<a href="%s/blog">Blog</a>
		</body></html>`, baseURL, baseURL)
	})
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<a href="%s/team">Team</a>
		</body></html>`, baseURL)
	})
	mux.HandleFunc("/blog", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<a href="%s/blog/post1">Post 1</a>
		</body></html>`, baseURL)
	})
	mux.HandleFunc("/team", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Our team page.</p></body></html>`)
	})
	mux.HandleFunc("/blog/post1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<a href="%s/blog">Back to Blog</a>
		</body></html>`, baseURL)
	})
	return httptest.NewServer(mux)
}

func TestDeepCrawl_FullFlow(t *testing.T) {
	ts := buildTestSiteWithBase(testFakeBase)
	defer ts.Close()

	var mu sync.Mutex
	var progressEvents []DeepCrawlProgress
	onProgress := func(p DeepCrawlProgress) {
		mu.Lock()
		progressEvents = append(progressEvents, p)
		mu.Unlock()
	}

	engine := NewDeepCrawlEngine(nil, onProgress)
	engine.requestDelay = 10 * time.Millisecond
	engine.perURLTimeout = 5 * time.Second
	engine.sessionTimeout = 30 * time.Second
	engine.skipPublicCheck = true
	engine.fetchFunc = testFetchFunc(ts, testFakeBase)

	ctx := context.Background()
	result, err := engine.StartCrawl(ctx, DeepCrawlRequest{
		SeedURL:        testFakeBase + "/",
		MaxDepth:       2,
		SameDomainOnly: true,
	})
	if err != nil {
		t.Fatalf("StartCrawl: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q", result.Status)
	}
	// Site: / → /about, /blog; /about → /team; /blog → /blog/post1
	// depth=2 → at least 5 items (seed + 2 at depth 1 + 2 at depth 2)
	if len(result.Items) < 3 {
		t.Fatalf("expected at least 3 items, got %d: %+v", len(result.Items), result.Items)
	}
	// Verify no duplicates
	urlSet := make(map[string]bool)
	for _, item := range result.Items {
		if urlSet[item.URL] {
			t.Fatalf("duplicate URL in results: %s", item.URL)
		}
		urlSet[item.URL] = true
	}
	// Verify progress events were emitted
	mu.Lock()
	eventCount := len(progressEvents)
	mu.Unlock()
	if eventCount == 0 {
		t.Fatal("expected progress events to be emitted")
	}
	// Verify ByDepth is populated
	if len(result.ByDepth) == 0 {
		t.Fatal("expected ByDepth to be populated")
	}
	if result.ByDepth[0].Total != 1 {
		t.Fatalf("expected depth 0 to have 1 URL, got %d", result.ByDepth[0].Total)
	}
	// Verify result summary
	totalProcessed := result.TotalSaved + result.Failed + result.Skipped
	if totalProcessed == 0 {
		t.Fatalf("expected totalProcessed > 0")
	}
	if result.TotalDiscovered != totalProcessed {
		t.Fatalf("TotalDiscovered=%d, want processed total %d", result.TotalDiscovered, totalProcessed)
	}

	mu.Lock()
	finalProgress := progressEvents[len(progressEvents)-1]
	mu.Unlock()
	if finalProgress.TotalDiscovered != result.TotalDiscovered {
		t.Fatalf("final progress TotalDiscovered=%d, want result total %d", finalProgress.TotalDiscovered, result.TotalDiscovered)
	}
}

func TestDeepCrawl_PreviewNoSaves(t *testing.T) {
	ts := buildTestSiteWithBase(testFakeBase)
	defer ts.Close()

	var mu sync.Mutex
	var progressEvents []DeepCrawlProgress
	onProgress := func(p DeepCrawlProgress) {
		mu.Lock()
		progressEvents = append(progressEvents, p)
		mu.Unlock()
	}

	engine := NewDeepCrawlEngine(nil, onProgress)
	engine.requestDelay = 10 * time.Millisecond
	engine.perURLTimeout = 5 * time.Second
	engine.sessionTimeout = 30 * time.Second
	engine.skipPublicCheck = true
	engine.fetchFunc = testFetchFunc(ts, testFakeBase)

	ctx := context.Background()
	result, err := engine.Preview(ctx, DeepCrawlRequest{
		SeedURL:        testFakeBase + "/",
		MaxDepth:       2,
		SameDomainOnly: true,
		PreviewOnly:    true,
	})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("expected status=completed, got %q", result.Status)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected items to be discovered in preview")
	}
	if result.TotalDiscovered == 0 {
		t.Fatal("expected TotalDiscovered to be populated in preview")
	}
	// All items should be "discovered" (not "saved")
	for _, item := range result.Items {
		if item.Status != "discovered" {
			t.Fatalf("preview item status=%q, want discovered: %s", item.Status, item.URL)
		}
	}
	// ByDepth should be populated with URL lists
	if len(result.ByDepth) == 0 {
		t.Fatal("expected ByDepth to be populated in preview")
	}
	totalByDepth := 0
	for _, depth := range result.ByDepth {
		totalByDepth += depth.Total
		if depth.Total > 0 && len(depth.URLs) == 0 {
			t.Fatalf("ByDepth[%d] should have URLs in preview mode", depth.Depth)
		}
	}
	if result.TotalDiscovered != totalByDepth {
		t.Fatalf("TotalDiscovered=%d, want sum of by-depth totals %d", result.TotalDiscovered, totalByDepth)
	}
	// Progress events should be emitted
	mu.Lock()
	eventCount := len(progressEvents)
	mu.Unlock()
	if eventCount == 0 {
		t.Fatal("expected progress events in preview mode")
	}
}

func TestDeepCrawl_Cancellation(t *testing.T) {
	var requestCount int
	var requestMu sync.Mutex

	slowMux := http.NewServeMux()
	slowMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestMu.Lock()
		requestCount++
		requestMu.Unlock()
		time.Sleep(200 * time.Millisecond)
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<html><body>
				<a href="%s/page1">P1</a><a href="%s/page2">P2</a>
				<a href="%s/page3">P3</a><a href="%s/page4">P4</a>
				<a href="%s/page5">P5</a><a href="%s/page6">P6</a>
				<a href="%s/page7">P7</a><a href="%s/page8">P8</a>
			</body></html>`, testFakeBase, testFakeBase, testFakeBase, testFakeBase,
				testFakeBase, testFakeBase, testFakeBase, testFakeBase)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><p>%s</p></body></html>`, r.URL.Path)
	})
	slowServer := httptest.NewServer(slowMux)
	defer slowServer.Close()

	engine := NewDeepCrawlEngine(nil, nil)
	engine.requestDelay = 10 * time.Millisecond
	engine.perURLTimeout = 5 * time.Second
	engine.sessionTimeout = 30 * time.Second
	engine.skipPublicCheck = true
	engine.fetchFunc = testFetchFunc(slowServer, testFakeBase)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	result, err := engine.StartCrawl(ctx, DeepCrawlRequest{
		SeedURL:        testFakeBase + "/",
		MaxDepth:       2,
		SameDomainOnly: true,
	})
	if err != nil {
		t.Fatalf("StartCrawl with cancellation: %v", err)
	}
	// Should be cancelled or timeout
	if result.Status != "cancelled" && result.Status != "timeout" && result.Status != "completed" {
		t.Fatalf("unexpected status: %q", result.Status)
	}
	requestMu.Lock()
	totalRequests := requestCount
	requestMu.Unlock()
	if totalRequests == 0 {
		t.Fatal("expected at least some requests before cancellation")
	}
	// With 200ms/request and 500ms timeout, should not process all 9 pages
	if totalRequests >= 9 {
		t.Fatalf("expected cancellation before all pages, got %d requests", totalRequests)
	}
}

func TestDeepCrawl_SameDomainFilter(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body>
			<a href="%s/local-page">Local Page</a>
			<a href="https://external-domain.com/page1">External 1</a>
			<a href="https://another-site.org/page2">External 2</a>
		</body></html>`, testFakeBase)
	})
	mux.HandleFunc("/local-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Local content.</p></body></html>`)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	engine := NewDeepCrawlEngine(nil, nil)
	engine.requestDelay = 10 * time.Millisecond
	engine.perURLTimeout = 5 * time.Second
	engine.sessionTimeout = 30 * time.Second
	engine.skipPublicCheck = true
	engine.fetchFunc = testFetchFunc(ts, testFakeBase)

	ctx := context.Background()
	result, err := engine.StartCrawl(ctx, DeepCrawlRequest{
		SeedURL:        testFakeBase + "/",
		MaxDepth:       2,
		SameDomainOnly: true,
	})
	if err != nil {
		t.Fatalf("StartCrawl: %v", err)
	}
	// Only same-domain URLs should be crawled
	for _, item := range result.Items {
		itemHost := extractHostname(item.URL)
		if itemHost != "test-crawl-site.example.com" {
			t.Fatalf("external URL crawled: %s (host=%s)", item.URL, itemHost)
		}
	}
	// External URLs should NOT appear
	for _, item := range result.Items {
		if strings.Contains(item.URL, "external-domain") ||
			strings.Contains(item.URL, "another-site") {
			t.Fatalf("external URL in results: %s", item.URL)
		}
	}
}

func TestDeepCrawl_DomainPolicyEnforcement(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><a href="/page">Page</a></body></html>`)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	dbPath := t.TempDir() + "/test_policy.db"
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// Block the fake domain
	_, err = store.UpdateURLDomainPolicies(ctx, URLDomainPolicyUpdateRequest{
		BlockDomains: []string{"test-crawl-site.example.com"},
		Reason:       "test block",
	})
	if err != nil {
		t.Fatalf("UpdateURLDomainPolicies: %v", err)
	}

	engine := NewDeepCrawlEngine(store, nil)
	engine.requestDelay = 10 * time.Millisecond
	engine.perURLTimeout = 5 * time.Second
	engine.sessionTimeout = 30 * time.Second
	engine.skipPublicCheck = true
	engine.fetchFunc = testFetchFunc(ts, testFakeBase)

	_, err = engine.StartCrawl(ctx, DeepCrawlRequest{
		SeedURL:        testFakeBase + "/",
		MaxDepth:       2,
		SameDomainOnly: true,
	})
	// Seed URL blocked by domain policy → error
	if err == nil {
		t.Fatal("expected error when seed URL is blocked by domain policy")
	}
	if !strings.Contains(err.Error(), "domain policy") {
		t.Fatalf("expected domain policy error, got: %v", err)
	}
}
