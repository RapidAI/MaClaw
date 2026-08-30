package websearch

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func engineIDs(strategy corelib.WebSearchStrategy) []string {
	ids := make([]string, 0, len(strategy.Engines))
	for _, engine := range strategy.Engines {
		ids = append(ids, engine.ID)
	}
	return ids
}

func TestDefaultWebSearchStrategyPresets(t *testing.T) {
	mainland := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	international := DefaultWebSearchStrategy(corelib.WebSearchPresetInternational)
	if got := engineIDs(mainland)[:4]; !reflect.DeepEqual(got, []string{"bing_cn", "baidu", "duckduckgo", "google"}) {
		t.Fatalf("mainland order = %v", got)
	}
	if got := engineIDs(international)[:4]; !reflect.DeepEqual(got, []string{"google", "duckduckgo", "bing_cn", "baidu"}) {
		t.Fatalf("international order = %v", got)
	}
	if mainland.BrowserHumanAssistEnabled || international.BrowserHumanAssistEnabled {
		t.Fatal("human verification assistance must require explicit opt-in")
	}
	if mainland.BrowserFallbackEngineID != "bing_cn" {
		t.Fatalf("mainland fallback = %q, want bing_cn", mainland.BrowserFallbackEngineID)
	}
	if international.BrowserFallbackEngineID != "google" {
		t.Fatalf("international fallback = %q, want google", international.BrowserFallbackEngineID)
	}
}

func TestNormalizeWebSearchStrategyDropsRetiredMojeekEntry(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	strategy.Engines = append([]corelib.WebSearchEngineConfig{{ID: "mojeek", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportHTTPHTML}}, strategy.Engines...)
	normalized, err := NormalizeWebSearchStrategy(strategy)
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range normalized.Engines {
		if engine.ID == "mojeek" {
			t.Fatalf("retired Mojeek entry survived normalization: %#v", normalized.Engines)
		}
	}
}

func TestNormalizeWebSearchStrategyStillRejectsDuplicateActiveEnginesAroundRetiredEntry(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	strategy.Engines = append([]corelib.WebSearchEngineConfig{
		{ID: "bing_cn", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportHTTPHTML},
		{ID: "mojeek", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML},
	}, strategy.Engines...)
	if _, err := NormalizeWebSearchStrategy(strategy); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("NormalizeWebSearchStrategy() error = %v, want duplicate active engine", err)
	}
}

func TestMigrateLegacyWebSearchStrategyMovesCurrentAndKeepsKey(t *testing.T) {
	strategy := MigrateLegacyWebSearchStrategy(corelib.WebSearchStrategy{}, []corelib.WebSearchProvider{{Type: "brave", Key: "secret", BaseURL: "https://search.example/"}}, "brave")
	if strategy.Engines[0].ID != "brave" || !strategy.Engines[0].Enabled || strategy.Engines[0].APIKey != "secret" || strategy.Engines[0].BaseURL != "https://search.example" {
		t.Fatalf("legacy migration = %#v", strategy.Engines[0])
	}
}

func TestResetWebSearchStrategyKeepsAPISecrets(t *testing.T) {
	current := DefaultWebSearchStrategy(corelib.WebSearchPresetInternational)
	for i := range current.Engines {
		if current.Engines[i].ID == "brave" {
			current.Engines[i].APIKey = "secret"
			current.Engines[i].BaseURL = "https://search.example"
			current.Engines[i].Enabled = true
		}
	}
	reset := ResetWebSearchStrategy(current, corelib.WebSearchPresetMainland)
	for _, engine := range reset.Engines {
		if engine.ID == "brave" && (engine.APIKey != "secret" || engine.BaseURL != "https://search.example" || engine.Enabled) {
			t.Fatalf("reset lost API config: %#v", engine)
		}
	}
}

func TestNormalizeWebSearchStrategyRejectsEnabledAPIWithoutKey(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].Enabled = true
		}
	}
	if _, err := NormalizeWebSearchStrategy(strategy); err == nil {
		t.Fatal("expected missing API key error")
	}
}

func TestNormalizeWebSearchStrategyDisablesBackfilledEnginesAndFutureModes(t *testing.T) {
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModeAggregate,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEngineID: "bing_cn",
	}
	normalized, err := NormalizeWebSearchStrategy(strategy)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Mode != corelib.WebSearchModePriority {
		t.Fatalf("mode = %q, want priority", normalized.Mode)
	}
	for _, engine := range normalized.Engines[1:] {
		if engine.Enabled {
			t.Fatalf("backfilled engine unexpectedly enabled: %#v", engine)
		}
	}
}

func TestNormalizeWebSearchStrategyRejectsUnsafeBaseURLs(t *testing.T) {
	for _, raw := range []string{"file:///tmp/search", "https://user:secret@example.com/search", "not a URL"} {
		strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
		strategy.Engines[0].BaseURL = raw
		if _, err := NormalizeWebSearchStrategy(strategy); err == nil {
			t.Fatalf("NormalizeWebSearchStrategy(%q) error = nil", raw)
		}
	}
}

func TestStrategySearchBudgetOnlyExpandsForReachableBrowserWork(t *testing.T) {
	directOnly := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	directOnly.BrowserHumanAssistEnabled = true
	directOnly.BrowserFallbackEnabled = false
	for i := range directOnly.Engines {
		if directOnly.Engines[i].Transport == corelib.WebSearchTransportBrowser {
			directOnly.Engines[i].Enabled = false
		}
	}
	if got := strategySearchBudget(directOnly); got != strategySearchTimeout {
		t.Fatalf("direct-only budget = %s, want %s", got, strategySearchTimeout)
	}

	withBrowserEngine := directOnly
	for i := range withBrowserEngine.Engines {
		if withBrowserEngine.Engines[i].ID == "google" {
			withBrowserEngine.Engines[i].Enabled = true
		}
	}
	if got := strategySearchBudget(withBrowserEngine); got != 2*time.Minute {
		t.Fatalf("browser-engine budget = %s, want 2m", got)
	}

	withFallback := directOnly
	withFallback.BrowserFallbackEnabled = true
	if got := strategySearchBudget(withFallback); got != 2*time.Minute {
		t.Fatalf("browser-fallback budget = %s, want 2m", got)
	}
}

func TestProbeWebSearchEngineAllowsColdStartWithoutSlowingRuntimeFallback(t *testing.T) {
	oldRuntimeTimeout := strategyEngineTimeout
	oldProbeTimeout := strategyProbeEngineTimeout
	strategyEngineTimeout = 20 * time.Millisecond
	strategyProbeEngineTimeout = 250 * time.Millisecond
	t.Cleanup(func() {
		strategyEngineTimeout = oldRuntimeTimeout
		strategyProbeEngineTimeout = oldProbeTimeout
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(70 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Cold start","link":"https://example.com/cold","snippet":"ready"}]}`))
	}))
	defer server.Close()
	engine := corelib.WebSearchEngineConfig{
		ID: "serper", Enabled: true, Priority: 1,
		Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
	}

	runtimeStrategy := corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion, Preset: corelib.WebSearchPresetCustom,
		Mode: corelib.WebSearchModePriority, Engines: []corelib.WebSearchEngineConfig{engine},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	if _, err := SearchWithStrategyCtx(context.Background(), "golang", 3, runtimeStrategy); err == nil || !strings.Contains(err.Error(), "request timed out") {
		t.Fatalf("runtime error = %v, want short per-engine timeout", err)
	}

	response, err := ProbeWebSearchEngineCtx(context.Background(), "golang", 3, engine, false)
	if err != nil {
		t.Fatalf("ProbeWebSearchEngineCtx() error = %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Title != "Cold start" {
		t.Fatalf("probe results = %#v", response.Results)
	}
}

func TestProbeWebSearchEngineRespectsCancelledParent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	engine := corelib.WebSearchEngineConfig{
		ID: "serper", Enabled: true, Priority: 1,
		Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ProbeWebSearchEngineCtx(ctx, "golang", 3, engine, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ProbeWebSearchEngineCtx() error = %v, want context.Canceled", err)
	}
}

func TestProbeWebSearchEngineRetriesOneTransientFailure(t *testing.T) {
	oldRetryDelay := strategyRetryDelay
	strategyRetryDelay = 0
	t.Cleanup(func() { strategyRetryDelay = oldRetryDelay })

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Recovered probe","link":"https://example.com/probe","snippet":"ok"}]}`))
	}))
	defer server.Close()
	engine := corelib.WebSearchEngineConfig{
		ID: "serper", Enabled: true, Priority: 1,
		Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
	}

	response, err := ProbeWebSearchEngineCtx(context.Background(), "golang", 3, engine, false)
	if err != nil {
		t.Fatalf("ProbeWebSearchEngineCtx() error = %v", err)
	}
	if calls != 2 || len(response.Results) != 1 {
		t.Fatalf("calls=%d response=%#v", calls, response)
	}
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].RetryCount != 1 {
		t.Fatalf("diagnostics=%#v, want retry_count=1", response.Diagnostics)
	}
}

func TestSearchStrategyRetriesOneTransientDirectFailure(t *testing.T) {
	oldRuntimeTimeout := strategyEngineTimeout
	oldRetryDelay := strategyRetryDelay
	strategyEngineTimeout = 250 * time.Millisecond
	strategyRetryDelay = 0
	t.Cleanup(func() {
		strategyEngineTimeout = oldRuntimeTimeout
		strategyRetryDelay = oldRetryDelay
	})

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Recovered","link":"https://example.com/recovered","snippet":"ok"}]}`))
	}))
	defer server.Close()
	strategy := corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion, Preset: corelib.WebSearchPresetCustom,
		Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "serper", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportAPI,
			APIKey: "secret", BaseURL: server.URL,
		}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}

	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil {
		t.Fatalf("SearchWithStrategyCtx() error = %v", err)
	}
	if calls != 2 || len(response.Results) != 1 || response.Results[0].Title != "Recovered" {
		t.Fatalf("calls=%d response=%#v", calls, response)
	}
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].RetryCount != 1 {
		t.Fatalf("diagnostics=%#v, want retry_count=1", response.Diagnostics)
	}
	if !response.Degraded {
		t.Fatal("recovered runtime retry should mark the response degraded")
	}
}

func TestSearchStrategyStopsAfterOneRetry(t *testing.T) {
	oldRuntimeTimeout := strategyEngineTimeout
	oldRetryDelay := strategyRetryDelay
	strategyEngineTimeout = 250 * time.Millisecond
	strategyRetryDelay = 0
	t.Cleanup(func() {
		strategyEngineTimeout = oldRuntimeTimeout
		strategyRetryDelay = oldRetryDelay
	})

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	strategy := corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion, Preset: corelib.WebSearchPresetCustom,
		Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "serper", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportAPI,
			APIKey: "secret", BaseURL: server.URL,
		}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}

	if _, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy); err == nil {
		t.Fatal("SearchWithStrategyCtx() error = nil, want final transient failure")
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want initial attempt plus one retry", calls)
	}
}

func TestSearchStrategyDoesNotRetryStableFailuresOrBrowser(t *testing.T) {
	for _, tc := range []struct {
		name   string
		engine corelib.WebSearchEngineConfig
		err    error
	}{
		{name: "invalid key", engine: corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, err: errors.New("HTTP 401 invalid API key")},
		{name: "rate limited", engine: corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, err: errors.New("HTTP 429 too many requests")},
		{name: "blocked", engine: corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportHTTPHTML}, err: errors.New("captcha challenge")},
		{name: "browser timeout", engine: corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportBrowser}, err: context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if shouldRetrySearchEngine(tc.engine, tc.err) {
				t.Fatalf("shouldRetrySearchEngine(%v) = true", tc.err)
			}
		})
	}
	if !shouldRetrySearchEngine(corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should be retried for API engines")
	}
}

func TestSearchErrorClassificationUsesHTTPStatusPrefixNotResponseBodyNumbers(t *testing.T) {
	stableBody := errors.New(`Serper returned HTTP 400: {"message":"upstream 503 reference; account 40102"}`)
	if shouldRetrySearchEngine(corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, stableBody) {
		t.Fatal("HTTP 400 was retried because its response body mentioned 503")
	}
	if got := classifySearchError(stableBody); got != "error" {
		t.Fatalf("classification = %q, want error", got)
	}
	stableTimeoutBody := errors.New(`Serper returned HTTP 422: {"message":"temporary timeout while validating query"}`)
	if shouldRetrySearchEngine(corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, stableTimeoutBody) {
		t.Fatal("HTTP 422 was retried because its response body mentioned a temporary timeout")
	}

	rateLimitBody := errors.New(`Serper returned HTTP 429: {"request_id":"500-401"}`)
	if shouldRetrySearchEngine(corelib.WebSearchEngineConfig{Transport: corelib.WebSearchTransportAPI}, rateLimitBody) {
		t.Fatal("HTTP 429 should not be retried")
	}
	if got := classifySearchError(rateLimitBody); got != "rate_limited" {
		t.Fatalf("classification = %q, want rate_limited", got)
	}
}

func TestSearchStrategyEngineWithTimeoutZeroUsesParentDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Too late","link":"https://example.com/late"}]}`))
	}))
	defer server.Close()
	engine := corelib.WebSearchEngineConfig{
		ID: "serper", Enabled: true, Priority: 1,
		Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err, _ := searchStrategyEngineWithTimeout(ctx, "golang", 3, engine, false, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want parent deadline exceeded", err)
	}
}

func TestSearchWithStrategyReturnsContextCancellation(t *testing.T) {
	SetBrowserSearchProvider(func(ctx context.Context, _, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEngineID: "bing_cn",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := SearchWithStrategyCtx(ctx, "golang", 3, strategy)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCanonicalSearchResultURLRejectsUnsafeSchemesAndNormalizesDefaultPorts(t *testing.T) {
	if got := canonicalSearchResultURL("javascript://example.com/alert(1)"); got != "" {
		t.Fatalf("unsafe URL canonicalized to %q", got)
	}
	if got := canonicalSearchResultURL("https://user:secret@example.com/path"); got != "" {
		t.Fatalf("credential URL canonicalized to %q", got)
	}
	if got := canonicalSearchResultURL("HTTPS://Example.COM:443/path/?utm_source=test#part"); got != "https://example.com/path" {
		t.Fatalf("canonical URL = %q", got)
	}
}

func TestSearchWithStrategyUsesConfiguredDirectEngineBaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "golang" {
			t.Errorf("query = %q", got)
		}
		_, _ = fmt.Fprint(w, `<html><body><a class="result-link" href="https://example.com/result">Result</a><td class="result-snippet">Snippet</td></body></html>`)
	}))
	t.Cleanup(server.Close)
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "duckduckgo", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://example.com/result" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchWithStrategyPreservesBaseURLQueryParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("region"); got != "cn" {
			t.Errorf("region = %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "golang" {
			t.Errorf("query = %q", got)
		}
		_, _ = fmt.Fprint(w, `<html><body><a class="result-link" href="https://example.com/result">Result</a></body></html>`)
	}))
	t.Cleanup(server.Close)
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL + "/search?region=cn"}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestSearchWithStrategyUsesGoogleBrowserFirst(t *testing.T) {
	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		return []BrowserSearchHit{{Title: "Google result", URL: "https://example.com", Snippet: "ok"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEnabled: false, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google"}) || len(response.Results) != 1 {
		t.Fatalf("engines=%v results=%v", engines, response.Results)
	}
}

func TestSearchWithStrategyPassesHumanAssistPreferenceToBrowser(t *testing.T) {
	var got bool
	SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, humanAssist bool) ([]BrowserSearchHit, error) {
		got = humanAssist
		return []BrowserSearchHit{{Title: "Result", URL: "https://example.com"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEngineID: "bing_cn", BrowserHumanAssistEnabled: true, MinResultsBeforeHedge: 1}
	if _, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("human-assist preference was not passed to browser provider")
	}
}

func TestSearchWithStrategyRejectsInvalidEnabledEngineResults(t *testing.T) {
	SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		return []BrowserSearchHit{{Title: "Unsafe", URL: "javascript:alert(1)", Snippet: "bad"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEnabled: false, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err == nil || !strings.Contains(err.Error(), "google: no valid results") {
		t.Fatalf("response = %#v, error = %v, want no-valid-results failure", response, err)
	}
	if len(response.Diagnostics) != 1 || response.Diagnostics[0].Outcome != "no_results" || response.Diagnostics[0].ResultCount != 0 {
		t.Fatalf("diagnostics = %#v", response.Diagnostics)
	}
}

func TestSearchWithStrategyDoesNotRepeatBrowserFallbackAndRedactsError(t *testing.T) {
	calls := 0
	SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		calls++
		return nil, errors.New("HTTP 403 body echoed secret-token and query")
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "google", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "private query", 3, strategy)
	if err == nil {
		t.Fatal("expected search failure")
	}
	if calls != 1 {
		t.Fatalf("browser calls = %d, want 1", calls)
	}
	combined := err.Error()
	if len(response.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", response.Diagnostics)
	}
	combined += " " + response.Diagnostics[0].Detail
	if strings.Contains(combined, "secret-token") || strings.Contains(combined, "private query") {
		t.Fatalf("sensitive provider text leaked: %q", combined)
	}
}

func TestSearchWithStrategyUsesBrowserFallbackToTopUpSparseResults(t *testing.T) {
	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if engineID == "google" {
			return []BrowserSearchHit{{Title: "Sparse", URL: "https://one.example"}}, nil
		}
		return []BrowserSearchHit{
			{Title: "More 1", URL: "https://two.example"},
			{Title: "More 2", URL: "https://three.example"},
		}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 3,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google", "bing_cn"}) || len(response.Results) != 3 {
		t.Fatalf("engines=%v results=%#v", engines, response.Results)
	}
}

func TestSearchWithStrategyRejectsInvalidBrowserFallbackResults(t *testing.T) {
	SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		return []BrowserSearchHit{{Title: "Unsafe", URL: "javascript://example.com/alert(1)"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                []corelib.WebSearchEngineConfig{{ID: "google", Enabled: false, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err == nil {
		t.Fatal("expected invalid fallback result failure")
	}
	if len(response.Results) != 0 || len(response.Diagnostics) != 1 || response.Diagnostics[0].Outcome != "no_results" {
		t.Fatalf("response = %#v", response)
	}
}

func TestPublicNetworkSearchDisablesCredentialAndBrowserTransports(t *testing.T) {
	calledBrowser := false
	SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		calledBrowser = true
		return []BrowserSearchHit{{Title: "unexpected", URL: "https://example.com"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser},
			{ID: "brave", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportAPI, APIKey: "secret"},
		},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	_, err := SearchWithStrategyCtx(WithPublicNetworkOnly(context.Background()), "golang", 3, strategy)
	if err == nil || !strings.Contains(err.Error(), "no enabled") {
		t.Fatalf("public search error = %v, want no permitted engine", err)
	}
	if calledBrowser {
		t.Fatal("public search invoked the managed browser")
	}
}

func TestQueryFingerprintUsesProcessSalt(t *testing.T) {
	query := "sensitive search"
	plain := sha256.Sum256([]byte(query))
	if got := queryFingerprint(query); got == queryFingerprint("different") || got != queryFingerprint(query) {
		t.Fatalf("fingerprint is not stable and query-specific: %q", got)
	} else if got == fmt.Sprintf("%x", plain[:8]) {
		t.Fatalf("fingerprint is an unsalted SHA-256 prefix: %q", got)
	}
}

func TestSafeSearchErrorDetailClassifiesKeylessHTTP403AntiBotAsBlocked(t *testing.T) {
	err := errors.New("HTTP 403: Forbidden（目标站点存在反爬验证。请先完成人机验证后重试）")
	if got := SafeSearchErrorDetail(err); got != "request was blocked or challenged" {
		t.Fatalf("SafeSearchErrorDetail() = %q, want blocked diagnostic", got)
	}
}

func TestMergeSearchResultsCanonicalizesTrackingURLs(t *testing.T) {
	results := mergeSearchResults([]SearchResult{{Title: "A", URL: "https://Example.com/doc/?utm_source=x"}}, []SearchResult{{Title: "B", URL: "https://example.com/doc"}, {Title: "C", URL: "https://example.net/"}}, 8)
	if len(results) != 2 || results[1].Title != "C" {
		t.Fatalf("deduped results = %#v", results)
	}
}

func TestStrategySearchBudgetReservesBrowserFallbackWithoutHumanAssist(t *testing.T) {
	SetBrowserSearchProvider(func(context.Context, string, string, int, bool) ([]BrowserSearchHit, error) {
		return nil, errors.New("unused")
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	if got := strategySearchBudget(strategy); got != strategySearchTimeout+strategyBrowserTimeout {
		t.Fatalf("default budget = %s, want %s", got, strategySearchTimeout+strategyBrowserTimeout)
	}

	strategy.BrowserFallbackEnabled = false
	for i := range strategy.Engines {
		if strategy.Engines[i].Transport == corelib.WebSearchTransportBrowser {
			strategy.Engines[i].Enabled = false
		}
	}
	if got := strategySearchBudget(strategy); got != strategySearchTimeout {
		t.Fatalf("direct-only budget = %s, want %s", got, strategySearchTimeout)
	}
}

func overrideStrategyTimeouts(t *testing.T, search, browser, engine, retry time.Duration) {
	t.Helper()
	oldSearch, oldBrowser, oldEngine, oldRetry := strategySearchTimeout, strategyBrowserTimeout, strategyEngineTimeout, strategyRetryDelay
	strategySearchTimeout = search
	strategyBrowserTimeout = browser
	strategyEngineTimeout = engine
	strategyRetryDelay = retry
	t.Cleanup(func() {
		strategySearchTimeout = oldSearch
		strategyBrowserTimeout = oldBrowser
		strategyEngineTimeout = oldEngine
		strategyRetryDelay = oldRetry
	})
}

func TestSearchWithStrategyFallsBackToBrowserWhenHTTPEnginesTimeOut(t *testing.T) {
	overrideStrategyTimeouts(t, 80*time.Millisecond, 50*time.Millisecond, 40*time.Millisecond, 0)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, query string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if query != "openreview" {
			t.Errorf("query = %q", query)
		}
		return []BrowserSearchHit{{Title: "OpenReview", URL: "https://openreview.net/forum?id=abc", Snippet: "paper"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "serper", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	started := time.Now()
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google"}) {
		t.Fatalf("browser engines = %v, want [google] for an OpenReview query", engines)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://openreview.net/forum?id=abc" {
		t.Fatalf("results = %#v", response.Results)
	}
	if !response.Degraded {
		t.Fatal("browser fallback should mark the response degraded")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("search took %s, HTTP engines should fail fast into browser fallback", elapsed)
	}
}

func TestSearchWithStrategySkipsRemainingHTTPToReserveBrowserBudget(t *testing.T) {
	overrideStrategyTimeouts(t, 80*time.Millisecond, 80*time.Millisecond, 50*time.Millisecond, 0)

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
		time.Sleep(200 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	SetBrowserSearchProvider(func(context.Context, string, string, int, bool) ([]BrowserSearchHit, error) {
		return []BrowserSearchHit{{Title: "OpenReview", URL: "https://openreview.net/forum?id=1"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "serper", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL},
			{ID: "duckduckgo", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL},
		},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	skipped := false
	for _, attempt := range response.Diagnostics {
		if attempt.EngineID == "duckduckgo" && attempt.Outcome == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("duckduckgo was not skipped to reserve browser budget: %#v", response.Diagnostics)
	}
	if hits > 3 {
		t.Fatalf("HTTP hits = %d, want the first engine not to keep retrying into the browser reserve", hits)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://openreview.net/forum?id=1" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchWithStrategyUsesParentBudgetWhenInnerTimeoutExpires(t *testing.T) {
	overrideStrategyTimeouts(t, 40*time.Millisecond, 50*time.Millisecond, 80*time.Millisecond, 0)

	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(250 * time.Millisecond)
	}))
	t.Cleanup(server.Close)

	browserCalls := 0
	SetBrowserSearchProvider(func(ctx context.Context, engineID string, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		browserCalls++
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []BrowserSearchHit{{Title: "OpenReview", URL: "https://openreview.net/forum?id=parent"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "serper", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportAPI, APIKey: "secret", BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if browserCalls != 1 {
		t.Fatalf("browser calls = %d, want 1 after inner budget expired", browserCalls)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://openreview.net/forum?id=parent" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchQuerySiteHintRecognizesOpenReview(t *testing.T) {
	if got := searchQuerySiteHint("openreview iclr 2024"); got != "openreview" {
		t.Fatalf("hint = %q", got)
	}
	if got := searchQuerySiteHint("https://openreview.net/forum?id=abc"); got != "openreview" {
		t.Fatalf("domain hint = %q", got)
	}
	if got := searchQuerySiteHint("golang generics"); got != "" {
		t.Fatalf("generic query should have no site hint, got %q", got)
	}
	if got := searchQuerySiteHint("code review checklist"); got != "" {
		t.Fatalf("review token should not match openreview, got %q", got)
	}
	if got := searchQuerySiteHint("read the go.dev docs"); got != "" {
		t.Fatalf("short SLD must not become a site hint, got %q", got)
	}
	if hostCoversSiteHint("notopenreview.com", "openreview") {
		t.Fatal("substring hosts must not count as covering")
	}
	if !hostCoversSiteHint("openreview.net", "openreview") || !hostCoversSiteHint("ieeexplore.ieee.org", "ieee") {
		t.Fatal("expected host labels to cover the site hint")
	}
}

func TestSearchWithStrategyContinuesToBrowserWhenHTMLMissesNamedSite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>
<a class="result-link" href="https://en.wikipedia.org/wiki/Open_review">Open review</a>
<a class="result-link" href="https://example.com/a">Unrelated A</a>
<a class="result-link" href="https://example.com/b">Unrelated B</a>
</body></html>`)
	}))
	t.Cleanup(server.Close)

	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		return []BrowserSearchHit{{Title: "ICLR paper", URL: "https://openreview.net/forum?id=xyz", Snippet: "OpenReview"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 3,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview iclr 2024", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google"}) {
		t.Fatalf("browser engines = %v, want google fallback for a named site miss", engines)
	}
	if len(response.Results) == 0 || response.Results[0].URL != "https://openreview.net/forum?id=xyz" {
		t.Fatalf("OpenReview hit should be ranked first: %#v", response.Results)
	}
	if !response.Degraded {
		t.Fatal("HTML miss plus browser fallback should be degraded")
	}
}

func TestSearchWithStrategyDoesNotBrowserFallbackWhenGenericQueryIsCovered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>
<a class="result-link" href="https://go.dev/doc">Go docs</a>
<a class="result-link" href="https://example.com/a">A</a>
<a class="result-link" href="https://example.com/b">B</a>
</body></html>`)
	}))
	t.Cleanup(server.Close)

	called := false
	SetBrowserSearchProvider(func(context.Context, string, string, int, bool) ([]BrowserSearchHit, error) {
		called = true
		return []BrowserSearchHit{{Title: "unexpected", URL: "https://example.net"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 3,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "golang generics", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("generic covered HTML results should not invoke browser fallback")
	}
	if len(response.Results) != 3 {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchWithStrategySkipsRemainingHTMLAfterConsecutiveMisses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	SetBrowserSearchProvider(func(context.Context, string, string, int, bool) ([]BrowserSearchHit, error) {
		return []BrowserSearchHit{{Title: "OpenReview", URL: "https://openreview.net/forum?id=skip"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "duckduckgo", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL},
			{ID: "baidu", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL},
			{ID: "bing_cn", Enabled: true, Priority: 3, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL},
		},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	skipped := false
	for _, attempt := range response.Diagnostics {
		if attempt.EngineID == "bing_cn" && attempt.Transport == corelib.WebSearchTransportHTTPHTML && attempt.Outcome == "skipped" {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("remaining HTML engines should be skipped after a named-site miss: %#v", response.Diagnostics)
	}
	if len(response.Results) != 1 || response.Results[0].URL != "https://openreview.net/forum?id=skip" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchWithStrategyTriesSecondBrowserWhenNamedSiteStillMissing(t *testing.T) {
	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if engineID == "google" {
			return []BrowserSearchHit{
				{Title: "Open review", URL: "https://en.wikipedia.org/wiki/Open_review"},
				{Title: "A", URL: "https://example.com/a"},
				{Title: "B", URL: "https://example.com/b"},
			}, nil
		}
		return []BrowserSearchHit{{Title: "Paper", URL: "https://openreview.net/forum?id=2"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetInternational, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "google", MinResultsBeforeHedge: 3,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview iclr", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google", "bing_cn"}) {
		t.Fatalf("engines = %v, want google then bing_cn", engines)
	}
	if len(response.Results) == 0 || response.Results[0].URL != "https://openreview.net/forum?id=2" {
		t.Fatalf("OpenReview hit should be first: %#v", response.Results)
	}
}

func TestSearchWithStrategyFallbackRetriesSecondBrowserWhenFirstMissesNamedSite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if engineID == "google" {
			return []BrowserSearchHit{{Title: "Wiki", URL: "https://en.wikipedia.org/wiki/Open_review"}}, nil
		}
		return []BrowserSearchHit{{Title: "Paper", URL: "https://openreview.net/forum?id=3"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google", "bing_cn"}) {
		t.Fatalf("engines = %v, want google then bing_cn", engines)
	}
	if len(response.Results) == 0 || response.Results[0].URL != "https://openreview.net/forum?id=3" {
		t.Fatalf("OpenReview hit should be first: %#v", response.Results)
	}
}

func TestSearchWithStrategySkipsHTMLAfterBrowserMissesNamedSite(t *testing.T) {
	ddgHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ddgHits++
		_, _ = fmt.Fprint(w, `<html><body><a class="result-link" href="https://example.com/ddg">DDG</a></body></html>`)
	}))
	t.Cleanup(server.Close)

	var engines []string
	SetBrowserSearchProvider(func(_ context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if engineID == "google" {
			return []BrowserSearchHit{
				{Title: "Wiki", URL: "https://en.wikipedia.org/wiki/Open_review"},
				{Title: "A", URL: "https://example.com/a"},
				{Title: "B", URL: "https://example.com/b"},
			}, nil
		}
		return []BrowserSearchHit{{Title: "Paper", URL: "https://openreview.net/forum?id=4"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetInternational, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser},
			{ID: "duckduckgo", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL},
		},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "google", MinResultsBeforeHedge: 3,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if ddgHits != 0 {
		t.Fatalf("HTML engine was consulted after a browser named-site miss: hits=%d", ddgHits)
	}
	if !reflect.DeepEqual(engines, []string{"google", "bing_cn"}) {
		t.Fatalf("engines = %v, want google then bing_cn", engines)
	}
	if len(response.Results) == 0 || response.Results[0].URL != "https://openreview.net/forum?id=4" {
		t.Fatalf("results = %#v", response.Results)
	}
}

func TestSearchWithStrategySecondBrowserGetsFreshBudgetAfterFirst(t *testing.T) {
	overrideStrategyTimeouts(t, 30*time.Millisecond, 40*time.Millisecond, 80*time.Millisecond, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	var engines []string
	var bingBudget time.Duration
	SetBrowserSearchProvider(func(ctx context.Context, engineID, _ string, _ int, _ bool) ([]BrowserSearchHit, error) {
		engines = append(engines, engineID)
		if engineID == "google" {
			<-ctx.Done()
			return []BrowserSearchHit{{Title: "Wiki", URL: "https://en.wikipedia.org/wiki/Open_review"}}, nil
		}
		if deadline, ok := ctx.Deadline(); ok {
			bingBudget = time.Until(deadline)
		} else {
			bingBudget = time.Hour
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return []BrowserSearchHit{{Title: "Paper", URL: "https://openreview.net/forum?id=budget"}}, nil
	})
	t.Cleanup(func() { SetBrowserSearchProvider(nil) })

	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL,
		}},
		BrowserFallbackEnabled: true, BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	}
	response, err := SearchWithStrategyCtx(context.Background(), "openreview", 8, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(engines, []string{"google", "bing_cn"}) {
		t.Fatalf("engines = %v, want google then bing_cn", engines)
	}
	if bingBudget < 30*time.Millisecond {
		t.Fatalf("bing browser budget = %s, want a fresh attempt not inner-context crumbs", bingBudget)
	}
	if len(response.Results) == 0 || response.Results[0].URL != "https://openreview.net/forum?id=budget" {
		t.Fatalf("results = %#v", response.Results)
	}
}
