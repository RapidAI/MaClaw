package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestDefaultWebSearchStrategyIncludesMaclawHubDisabled(t *testing.T) {
	for _, preset := range []string{corelib.WebSearchPresetMainland, corelib.WebSearchPresetInternational} {
		strategy := DefaultWebSearchStrategy(preset)
		var found *corelib.WebSearchEngineConfig
		enabledFree := map[string]bool{}
		for i := range strategy.Engines {
			engine := strategy.Engines[i]
			if engine.ID == WebSearchEngineMaclawHub {
				copy := engine
				found = &copy
			}
			if engine.Enabled && !builtinWebSearchEngines[engine.ID].NeedsKey {
				enabledFree[engine.ID] = true
			}
		}
		if found == nil {
			t.Fatalf("preset %s missing %s", preset, WebSearchEngineMaclawHub)
		}
		if found.Enabled {
			t.Fatalf("preset %s enabled MaClaw Hub by default: %#v", preset, found)
		}
		if found.Transport != corelib.WebSearchTransportAPI {
			t.Fatalf("transport = %q", found.Transport)
		}
		if found.BaseURL != defaultMaclawHubSearchURL {
			t.Fatalf("base URL = %q", found.BaseURL)
		}
		if found.APIKey != "" {
			t.Fatalf("default strategy stored a hub token: %q", found.APIKey)
		}
		for _, id := range []string{"bing_cn", "baidu", "duckduckgo", "google"} {
			if !enabledFree[id] {
				t.Fatalf("preset %s unexpectedly disabled default engine %s", preset, id)
			}
		}
	}
}

func TestNormalizeWebSearchStrategyBackfillsMaclawHubDisabled(t *testing.T) {
	strategy := corelib.WebSearchStrategy{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEngineID: "bing_cn",
	}
	normalized, err := NormalizeWebSearchStrategy(strategy)
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range normalized.Engines {
		if engine.ID == WebSearchEngineMaclawHub {
			if engine.Enabled {
				t.Fatalf("backfilled MaClaw Hub was enabled: %#v", engine)
			}
			return
		}
	}
	t.Fatal("NormalizeWebSearchStrategy did not backfill MaClaw Hub")
}

func TestNormalizeWebSearchStrategyAllowsEnabledMaclawHubWithoutKey(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == WebSearchEngineMaclawHub {
			strategy.Engines[i].Enabled = true
		}
	}
	normalized, err := NormalizeWebSearchStrategy(strategy)
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range normalized.Engines {
		if engine.ID == WebSearchEngineMaclawHub && !engine.Enabled {
			t.Fatalf("enabled MaClaw Hub was disabled during normalize: %#v", engine)
		}
	}
}

func TestApplyConfigHubAuthUsesRegisteredHubToken(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	next := ApplyConfigHubAuth(strategy, corelib.AppConfig{RemoteViewerToken: " viewer-token "})
	for _, engine := range next.Engines {
		if engine.ID == WebSearchEngineMaclawHub {
			if engine.APIKey != "viewer-token" {
				t.Fatalf("APIKey = %q, want viewer-token", engine.APIKey)
			}
			return
		}
	}
	t.Fatal("MaClaw Hub missing after ApplyConfigHubAuth")
}

func TestApplyConfigHubAuthPrefersViewerThenSessionThenMachine(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	if got := HubAuthTokenFromConfig(corelib.AppConfig{
		RemoteViewerToken: "viewer", SkillMarketSessionToken: "session", RemoteMachineToken: "machine",
	}); got != "viewer" {
		t.Fatalf("token = %q, want viewer", got)
	}
	if got := HubAuthTokenFromConfig(corelib.AppConfig{
		SkillMarketSessionToken: "session", RemoteMachineToken: "machine",
	}); got != "session" {
		t.Fatalf("token = %q, want session", got)
	}
	if got := HubAuthTokenFromConfig(corelib.AppConfig{RemoteMachineToken: "machine"}); got != "machine" {
		t.Fatalf("token = %q, want machine", got)
	}
	next := ApplyHubAuth(strategy, "explicit")
	for _, engine := range next.Engines {
		if engine.ID == WebSearchEngineMaclawHub && engine.APIKey != "explicit" {
			t.Fatalf("explicit token not applied: %#v", engine)
		}
	}
	withKey := ApplyHubAuth(next, "replacement")
	for _, engine := range withKey.Engines {
		if engine.ID == WebSearchEngineMaclawHub && engine.APIKey != "explicit" {
			t.Fatalf("existing key was overwritten: %#v", engine)
		}
	}
}

func TestSearchMaclawHubPostsJSONAndMapsResults(t *testing.T) {
	var gotAuth, gotQuery, gotEngine string
	var gotLimit int
	var gotContent, gotFallback bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Query    string `json:"query"`
			Engine   string `json:"engine"`
			Limit    int    `json:"limit"`
			Content  bool   `json:"content"`
			Fallback bool   `json:"fallback"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("payload = %s err = %v", body, err)
		}
		gotQuery, gotEngine, gotLimit = payload.Query, payload.Engine, payload.Limit
		gotContent, gotFallback = payload.Content, payload.Fallback
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"golang","engine":"google","results":[{"rank":1,"title":"Go","url":"https://go.dev","snippet":"The Go site","content":"full page","relevance":0.9}],"count":1,"took_ms":1234}`))
	}))
	defer server.Close()

	results, err := searchMaclawHub(context.Background(), corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub, Key: "hub-token", BaseURL: server.URL,
	}, "golang programming", 7)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer hub-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotQuery != "golang programming" || gotEngine != "auto" || gotLimit != 7 || !gotContent || !gotFallback {
		t.Fatalf("request query=%q engine=%q limit=%d content=%t fallback=%t", gotQuery, gotEngine, gotLimit, gotContent, gotFallback)
	}
	if len(results) != 1 || results[0].Title != "Go" || results[0].URL != "https://go.dev" || results[0].Snippet != "The Go site" {
		t.Fatalf("results = %#v", results)
	}
}

func TestSearchMaclawHubUsesContentWhenSnippetMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"","url":"https://example.com/page","content":"` + strings.Repeat("很长的正文", 80) + `"}]}`))
	}))
	defer server.Close()

	results, err := searchMaclawHub(context.Background(), corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub, Key: "hub-token", BaseURL: server.URL,
	}, "example", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Title != "https://example.com/page" {
		t.Fatalf("title fallback = %q", results[0].Title)
	}
	if !strings.HasSuffix(results[0].Snippet, "…") || len([]rune(results[0].Snippet)) != 301 {
		t.Fatalf("snippet was not truncated from content: %q", results[0].Snippet)
	}
}

func TestSearchMaclawHubRequiresToken(t *testing.T) {
	_, err := searchMaclawHub(context.Background(), corelib.WebSearchProvider{Type: WebSearchEngineMaclawHub}, "golang", 3)
	if err == nil || !strings.Contains(err.Error(), "signed-in hub account") {
		t.Fatalf("error = %v, want signed-in hub account", err)
	}
}

func TestSearchMaclawHubUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := searchMaclawHub(context.Background(), corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub, Key: "bad-token", BaseURL: server.URL,
	}, "golang", 3)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error = %v, want HTTP 401", err)
	}
	if classifySearchError(err) != "invalid_key" {
		t.Fatalf("classify = %q", classifySearchError(err))
	}
	if strings.Contains(err.Error(), "missing bearer token") {
		t.Fatalf("provider body leaked: %v", err)
	}
}

func TestSearchMaclawHubSkipsInvalidResultURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Bad","url":"javascript:alert(1)"},{"title":"Good","url":"https://example.com/ok","snippet":"safe"}]}`))
	}))
	defer server.Close()

	results, err := searchMaclawHub(context.Background(), corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub, Key: "hub-token", BaseURL: server.URL,
	}, "golang", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com/ok" {
		t.Fatalf("results = %#v", results)
	}
}

func TestMaclawHubSearchURLUsesCanonicalSearchPath(t *testing.T) {
	got, err := maclawHubSearchURL("https://hub.maclaw.top/searchproxy")
	if err != nil || got != defaultMaclawHubSearchURL {
		t.Fatalf("canonical base = %q err = %v", got, err)
	}
	got, err = maclawHubSearchURL("https://hub.maclaw.top/searchproxy/search")
	if err != nil || got != defaultMaclawHubSearchURL {
		t.Fatalf("search path = %q err = %v", got, err)
	}
	got, err = maclawHubSearchURL("http://127.0.0.1:9")
	if err != nil || got != "http://127.0.0.1:9" {
		t.Fatalf("test endpoint rewritten: %q err = %v", got, err)
	}
}

func TestSafeSearchErrorDetailForMissingHubLogin(t *testing.T) {
	err := fmt.Errorf("MaClaw Hub search requires a signed-in hub account")
	if got := SafeSearchErrorDetail(err); got != "sign in to MaClaw Hub" {
		t.Fatalf("SafeSearchErrorDetail() = %q", got)
	}
}

func TestSafeSearchErrorDetailForHubUnauthorizedNeverMentionsCredentials(t *testing.T) {
	err := fmt.Errorf("MaClaw Hub search returned HTTP 401")
	got := SafeSearchErrorDetail(err)
	if got != "MaClaw Hub search is unavailable" {
		t.Fatalf("SafeSearchErrorDetail() = %q", got)
	}
	if strings.Contains(strings.ToLower(got), "credential") || strings.Contains(strings.ToLower(got), "api key") || strings.Contains(strings.ToLower(got), "token") {
		t.Fatalf("hub error mentioned secrets: %q", got)
	}
}

func TestSearchWithStrategyUsesMaclawHubAndHubAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer viewer-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Hub Result","url":"https://example.com/hub","snippet":"from hub"}]}`))
	}))
	defer server.Close()

	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		strategy.Engines[i].Enabled = strategy.Engines[i].ID == WebSearchEngineMaclawHub
		if strategy.Engines[i].ID == WebSearchEngineMaclawHub {
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	strategy.BrowserFallbackEnabled = false
	strategy = ApplyConfigHubAuth(strategy, corelib.AppConfig{RemoteViewerToken: "viewer-token"})

	response, err := SearchWithStrategyCtx(context.Background(), "golang", 3, strategy)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].Title != "Hub Result" {
		t.Fatalf("response = %#v", response)
	}
}

func TestStrategySearchBudgetExpandsForMaclawHub(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	if got := strategySearchBudget(strategy); got != strategySearchTimeout {
		t.Fatalf("disabled hub budget = %s, want %s", got, strategySearchTimeout)
	}
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == WebSearchEngineMaclawHub {
			strategy.Engines[i].Enabled = true
		}
	}
	if got := strategySearchBudget(strategy); got != strategyMaclawHubTimeout {
		t.Fatalf("enabled hub budget = %s, want %s", got, strategyMaclawHubTimeout)
	}
}

func TestLiveMaclawHubSearchSmoke(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("MACLAW_HUB_TOKEN"))
	if token == "" {
		t.Skip("skipping live RapidSearch smoke test: set MACLAW_HUB_TOKEN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	results, err := searchMaclawHub(ctx, corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub, Key: token, BaseURL: defaultMaclawHubSearchURL,
	}, "golang programming language", 3)
	if err != nil {
		if strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "network is unreachable") {
			t.Skipf("skipping live RapidSearch smoke test: network unavailable: %v", err)
		}
		t.Fatalf("live RapidSearch failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("live RapidSearch returned no results")
	}
	t.Logf("live RapidSearch: %d results, first %s — %s", len(results), results[0].Title, results[0].URL)
}
