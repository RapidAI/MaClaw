package websearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

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
	legacyURL := defaultLegacySearchURL
	legacyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a class="result__a" href="https://example.com/ddg-direct">DDG Direct</a><a class="result__snippet">ddg direct snippet</a></body></html>`))
	}))
	defaultLegacySearchURL = legacyServer.URL
	defer func() {
		defaultLegacySearchURL = legacyURL
		legacyServer.Close()
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "accepted but not ready", http.StatusAccepted)
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "duckduckgo", BaseURL: server.URL})
	if err == nil {
		if len(results) != 1 || results[0].Title != "DDG Direct" {
			t.Fatalf("unexpected results: %+v", results)
		}
		return
	}
	t.Fatalf("SearchWithProvider() error = %v", err)
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

	providerCanceled := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		select {
		case providerCanceled <- struct{}{}:
		default:
		}
	}))
	defer server.Close()

	results, err := SearchWithProvider("golang", 5, corelib.WebSearchProvider{Type: "serper", Key: "slow", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("SearchWithProvider() error = %v", err)
	}
	select {
	case <-providerCanceled:
	default:
		t.Fatal("provider request was not canceled before fallback")
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

func TestPrepareSearchCapsMaxResults(t *testing.T) {
	query, maxResults, _, cancel, err := prepareSearch("golang", 99)
	if err != nil {
		t.Fatalf("prepareSearch() error = %v", err)
	}
	defer cancel()
	if query != "golang" || maxResults != 20 {
		t.Fatalf("query=%q maxResults=%d", query, maxResults)
	}
}

func TestApplyContentWindowUsesRuneOffsets(t *testing.T) {
	result := &FetchResult{Content: "甲乙丙丁戊"}
	applyContentWindow(result, 1, 2)
	if result.Content != "乙丙" {
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
