package websearch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// disableBingBaidu mocks Bing and Baidu endpoints to return 503 so that
// existing tests exercise the DDG/Mojeek fallback paths they were designed for.
// Returns a cleanup function that restores the original URLs.
func disableBingBaidu(t *testing.T) func() {
	t.Helper()
	origBing := defaultBingSearchURL
	origBaidu := defaultBaiduSearchURL
	origLastGood := lastGoodEndpointName

	unavailableServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable in test", http.StatusServiceUnavailable)
	}))
	defaultBingSearchURL = unavailableServer.URL
	defaultBaiduSearchURL = unavailableServer.URL
	lastGoodEndpointMu.Lock()
	lastGoodEndpointName = ""
	lastGoodEndpointMu.Unlock()

	return func() {
		defaultBingSearchURL = origBing
		defaultBaiduSearchURL = origBaidu
		lastGoodEndpointMu.Lock()
		lastGoodEndpointName = origLastGood
		lastGoodEndpointMu.Unlock()
		unavailableServer.Close()
	}
}

func TestSearchWithProvider_Brave(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "brave-key" {
			t.Fatalf("X-Subscription-Token = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"web":{"results":[{"title":"Brave Result","url":"https://example.com/brave","description":"brave snippet"}]}}`))
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "brave", Key: "brave-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Brave Result" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestTestProvider_BraveRejectsBadKeyWithoutFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Subscription-Token"); got != "bad-key" {
			t.Fatalf("X-Subscription-Token = %q, want bad-key", got)
		}
		http.Error(w, "invalid subscription token", http.StatusUnauthorized)
	}))
	defer server.Close()

	err := TestProvider(context.Background(), corelib.WebSearchProvider{Type: "brave", Key: "bad-key", BaseURL: server.URL})
	if err == nil {
		t.Fatal("TestProvider() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "Brave returned HTTP 401") {
		t.Fatalf("TestProvider() error = %v, want Brave HTTP 401", err)
	}
}

func TestTestProvider_DuckDuckGoHTTP202ChallengeFailsClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`<html><body><div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.</div><form id="challenge-form"></form></body></html>`))
	}))
	defer server.Close()

	err := TestProvider(context.Background(), corelib.WebSearchProvider{Type: "duckduckgo", BaseURL: server.URL})
	if err == nil {
		t.Fatal("TestProvider() error = nil, want challenge failure")
	}
	// The anti-bot chain must surface the challenge clearly (and escalate
	// through browser headers / fingerprint before giving up) rather than
	// returning the anomaly page as if it were search results.
	if !strings.Contains(err.Error(), "challenge") {
		t.Fatalf("TestProvider() error = %v, want challenge error", err)
	}
}

func TestSearchWithProvider_Serper(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-KEY"); got != "serper-key" {
			t.Fatalf("X-API-KEY = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Serper Result","link":"https://example.com/serper","snippet":"serper snippet"}]}`))
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "serper", Key: "serper-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Serper Result" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchWithProvider_DuckDuckGo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/ddg">Duck Result</a><a class="result__snippet">duck snippet</a></body></html>`))
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "duckduckgo", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Duck Result" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchWithProvider_FallbackOnMissingKey(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/fallback">Fallback Result</a><a class="result__snippet">fallback snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = server.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		server.Close()
	}()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "brave"})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Fallback Result" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchWithProvider_ProviderErrorFallsBackToDirectSearch(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/direct-fallback">Direct Fallback</a><a class="result__snippet">direct fallback snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		legacyServer.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "serper", Key: "bad", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Direct Fallback" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchWithProvider_DuckDuckGoHTTP202FallsBackToDirectSearch(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	mojeekURL := defaultMojeekSearchURL
	legacyCalled := false
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalled = true
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/ddg-direct">DDG Direct</a><a class="result__snippet">ddg direct snippet</a></body></html>`))
	}))
	mojeekServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result-title" href="https://example.com/mojeek-direct">Mojeek Direct</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defaultMojeekSearchURL = mojeekServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		defaultMojeekSearchURL = mojeekURL
		legacyServer.Close()
		mojeekServer.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "accepted but not ready", http.StatusAccepted)
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "duckduckgo", BaseURL: server.URL})
	if err == nil {
		if legacyCalled {
			t.Fatal("same failure-domain fallback was called")
		}
		if len(results) != 1 || results[0].Title != "Mojeek Direct" {
			t.Fatalf("unexpected results: %+v", results)
		}
		return
	}
	t.Fatalf("SearchWithProvider() error = %v", err)
}

func TestSearchWithProvider_DirectFallbackUsesAlternateFailureDomain(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	mojeekURL := defaultMojeekSearchURL
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "accepted but not ready", http.StatusAccepted)
	}))
	mojeekServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result-title" href="https://example.com/alternate">Alternate Direct</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defaultMojeekSearchURL = mojeekServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		defaultMojeekSearchURL = mojeekURL
		legacyServer.Close()
		mojeekServer.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "serper", Key: "bad", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if len(results) != 1 || results[0].Title != "Alternate Direct" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestDirectFallbackChainTimesOutSlowEndpointAndTriesNext(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	mojeekURL := defaultMojeekSearchURL
	oldDirectEndpointTimeout := directEndpointSearchTimeout
	directEndpointSearchTimeout = 20 * time.Millisecond

	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/slow">Slow Direct</a></body></html>`))
	}))
	mojeekServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result-title" href="https://example.com/fast">Fast Direct</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defaultMojeekSearchURL = mojeekServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		defaultMojeekSearchURL = mojeekURL
		directEndpointSearchTimeout = oldDirectEndpointTimeout
		legacyServer.Close()
		mojeekServer.Close()
	}()

	start := time.Now()
	results, err := Search("golang", 5)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if elapsed >= 80*time.Millisecond {
		t.Fatalf("Search() took %s, want slow endpoint timeout plus next endpoint", elapsed)
	}
	if len(results) != 1 || results[0].Title != "Fast Direct" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestFallbackDirectSearchRespectsParentContextAfterProviderError(t *testing.T) {
	legacyURL := defaultLegacySearchURL
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/canceled-context">Canceled Context</a><a class="result__snippet">canceled context snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		legacyServer.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fallbackDirectSearch(ctx, "golang", 5, corelib.WebSearchProvider{Type: "serper"}, context.Canceled, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
	}
}

func TestSearchWithProvider_ProviderTimeoutStillFallsBackWithinGlobalBudget(t *testing.T) {
	cleanup := disableBingBaidu(t)
	defer cleanup()
	legacyURL := defaultLegacySearchURL
	oldSearchTimeout := searchTimeout
	oldProviderSearchTimeout := providerSearchTimeout
	oldFallbackSearchTimeout := fallbackSearchTimeout

	searchTimeout = 250 * time.Millisecond
	providerSearchTimeout = 20 * time.Millisecond
	fallbackSearchTimeout = 150 * time.Millisecond

	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/timeout-direct">Timeout Direct</a><a class="result__snippet">timeout direct snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		searchTimeout = oldSearchTimeout
		providerSearchTimeout = oldProviderSearchTimeout
		fallbackSearchTimeout = oldFallbackSearchTimeout
		legacyServer.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Slow Result","link":"https://example.com/slow","snippet":"slow snippet"}]}`))
	}))
	defer server.Close()

	start := time.Now()
	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "serper", Key: "slow", BaseURL: server.URL})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	if elapsed >= 80*time.Millisecond {
		t.Fatalf("SearchWithProvider() took %s, want provider timeout plus quick fallback", elapsed)
	}
	if len(results) != 1 || results[0].Title != "Timeout Direct" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	_, err := SearchWithProvider("   ", 5, corelib.WebSearchProvider{Type: "duckduckgo"})
	if err == nil || err.Error() != "query is empty" {
		t.Fatalf("err = %v, want query is empty", err)
	}
}

func TestSearchDirectLegacy_UsesOverrideURL(t *testing.T) {
	legacyURL := defaultLegacySearchURL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/direct">Direct Result</a><a class="result__snippet">direct snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = server.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		server.Close()
	}()

	results, err := searchDirectLegacy(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("searchDirectLegacy() error = %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/direct" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestParseMojeekResultsHandlesUppercaseAnchors(t *testing.T) {
	html := `<html><body><div id="results"><A CLASS="result-title" HREF="https://example.com/upper">Upper Result</A></div></body></html>`
	results := parseMojeekResults(html, 5)
	if len(results) != 1 || results[0].Title != "Upper Result" || results[0].URL != "https://example.com/upper" {
		t.Fatalf("parseMojeekResults() = %+v", results)
	}
}

func TestParseMojeekResultsKeepsExternalSearchPathResults(t *testing.T) {
	html := `<html><body><div id="results"><a class="result-title" href="https://docs.example.com/search/reference">Search Docs</a></div></body></html>`
	results := parseMojeekResults(html, 5)
	if len(results) != 1 || results[0].URL != "https://docs.example.com/search/reference" {
		t.Fatalf("parseMojeekResults() = %+v, want external /search result kept", results)
	}
}

func TestParseMojeekResultsFiltersProviderSearchChrome(t *testing.T) {
	html := `<html><body><div id="results"><a class="result-title" href="https://www.mojeek.com/search?q=next">Next Page</a><a class="result-title" href="https://example.com/result">Real Result</a></div></body></html>`
	results := parseMojeekResults(html, 5)
	if len(results) != 1 || results[0].URL != "https://example.com/result" {
		t.Fatalf("parseMojeekResults() = %+v, want only real result", results)
	}
}

func TestExtractAttrRequiresAttributeBoundary(t *testing.T) {
	anchor := `<a data-href="https://example.com/wrong" href="https://example.com/right">Result</a>`
	if got := extractAttr(anchor, "href"); got != "https://example.com/right" {
		t.Fatalf("extractAttr() = %q, want right href", got)
	}
}

func TestPrepareSearchCapsMaxResults(t *testing.T) {
	query, maxResults, _, cancel, err := prepareSearch(context.Background(), "golang", 99)
	if err != nil {
		t.Fatalf("prepareSearch() error = %v", err)
	}
	defer cancel()
	if query != "golang" || maxResults != 20 {
		t.Fatalf("query=%q maxResults=%d", query, maxResults)
	}
}

func TestApplyContentWindowUsesRuneOffsets(t *testing.T) {
	result := &FetchResult{Content: "\u7532\u4e59\u4e19\u4e01\u620a"}
	applyContentWindow(result, 1, 2)
	if result.Content != "\u4e59\u4e19" {
		t.Fatalf("Content = %q", result.Content)
	}
	if result.TotalChars != 5 {
		t.Fatalf("TotalChars = %d", result.TotalChars)
	}
	if !result.Truncated || !result.HasMore {
		t.Fatalf("Truncated=%t HasMore=%t", result.Truncated, result.HasMore)
	}
	if result.NextOffset != 3 {
		t.Fatalf("NextOffset = %d", result.NextOffset)
	}
}

func TestFetchFTPCtxReturnsCancelledBeforeDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchFTPCtx(ctx, "ftp://127.0.0.1/file.txt", &FetchOptions{TimeoutS: 30})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchFTPCtx error = %v, want context.Canceled", err)
	}
}

func TestFetchWithChromeCtxReturnsCancelledBeforeLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchWithChromeCtx(ctx, "https://example.com", &FetchOptions{RenderJS: true, TimeoutS: 30})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("fetchWithChromeCtx error = %v, want context.Canceled", err)
	}
}

func TestCloseConnOnContextDoneClosesConn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	left, right := net.Pipe()
	defer right.Close()

	stop := closeConnOnContextDone(ctx, left)
	cancel()
	buf := make([]byte, 1)
	_, err := left.Read(buf)
	stop()
	stop()
	if err == nil {
		t.Fatal("left.Read succeeded after context cancellation, want closed connection error")
	}
}

func TestFetchSupportsContentWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><head><title>Long Page</title></head><body><main>ABCDEFGHIJ</main></body></html>`))
	}))
	defer server.Close()

	result, err := Fetch(server.URL, &FetchOptions{Offset: 2, MaxChars: 4})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Title != "Long Page" {
		t.Fatalf("Title = %q", result.Title)
	}
	if result.Content != "CDEF" {
		t.Fatalf("Content = %q", result.Content)
	}
	if result.TotalChars != 10 {
		t.Fatalf("TotalChars = %d", result.TotalChars)
	}
	if !result.Truncated || !result.HasMore {
		t.Fatalf("Truncated=%t HasMore=%t", result.Truncated, result.HasMore)
	}
	if result.NextOffset != 6 {
		t.Fatalf("NextOffset = %d", result.NextOffset)
	}
}
