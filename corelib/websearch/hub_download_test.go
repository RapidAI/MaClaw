package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMaclawHubDownloadURLUsesCanonicalDownloadPath(t *testing.T) {
	got, err := maclawHubDownloadURL("")
	if err != nil || got != defaultMaclawHubDownloadURL {
		t.Fatalf("empty = %q err = %v", got, err)
	}
	got, err = maclawHubDownloadURL("https://hub.maclaw.top/searchproxy")
	if err != nil || got != defaultMaclawHubDownloadURL {
		t.Fatalf("searchproxy = %q err = %v", got, err)
	}
	got, err = maclawHubDownloadURL("https://hub.maclaw.top/searchproxy/search")
	if err != nil || got != defaultMaclawHubDownloadURL {
		t.Fatalf("search = %q err = %v", got, err)
	}
	got, err = maclawHubDownloadURL("https://hub.maclaw.top/searchproxy/download")
	if err != nil || got != defaultMaclawHubDownloadURL {
		t.Fatalf("download = %q err = %v", got, err)
	}
	got, err = maclawHubDownloadURL("http://127.0.0.1:9")
	if err != nil || got != "http://127.0.0.1:9/download" {
		t.Fatalf("test endpoint = %q err = %v", got, err)
	}
}

func TestHubDownloadFromStrategyRequiresEnabledEngineAndToken(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	ch, err := HubDownloadFromStrategy(strategy)
	if err != nil || ch != nil {
		t.Fatalf("default strategy channel = %#v err = %v", ch, err)
	}

	for i := range strategy.Engines {
		if strategy.Engines[i].ID == WebSearchEngineMaclawHub {
			strategy.Engines[i].Enabled = true
		}
	}
	ch, err = HubDownloadFromStrategy(strategy)
	if err == nil || !strings.Contains(err.Error(), "signed-in hub account") {
		t.Fatalf("enabled without token: ch=%#v err=%v", ch, err)
	}

	strategy = ApplyHubAuth(strategy, "hub-token")
	ch, err = HubDownloadFromStrategy(strategy)
	if err != nil || ch == nil || ch.Token != "hub-token" {
		t.Fatalf("enabled with token: %#v err=%v", ch, err)
	}
}

func TestApplyHubDownloadSkippedForPublicNetworkOnly(t *testing.T) {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == WebSearchEngineMaclawHub {
			strategy.Engines[i].Enabled = true
			strategy.Engines[i].APIKey = "hub-token"
		}
	}
	opts := &FetchOptions{PublicNetworkOnly: true}
	if err := ApplyHubDownload(opts, strategy); err != nil {
		t.Fatal(err)
	}
	if opts.HubDownload != nil {
		t.Fatalf("public-network caller kept hub channel: %#v", opts.HubDownload)
	}
}

func TestFetchRoutesThroughHubDownloadWhenEnabled(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		http.Error(w, "direct download must not happen", http.StatusTeapot)
	}))
	defer target.Close()

	var gotAuth, gotMethod, gotPath, gotQueryURL, gotRange string
	var gotPostURL string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQueryURL = r.URL.Query().Get("url")
		gotRange = r.Header.Get("Range")
		if r.Method == http.MethodPost {
			var payload struct {
				URL string `json:"url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				gotPostURL = payload.URL
			}
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-hub"))
	}))
	defer proxy.Close()

	savePath := filepath.Join(t.TempDir(), "paper.pdf")
	strategy := hubDownloadStrategy(proxy.URL, "hub-token", true)
	opts := &FetchOptions{SavePath: savePath, Headers: map[string]string{"Range": "bytes=0-7"}}
	if err := ApplyHubDownload(opts, strategy); err != nil {
		t.Fatal(err)
	}
	result, err := FetchCtx(context.Background(), target.URL+"/paper.pdf", opts)
	if err != nil {
		t.Fatal(err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("target received %d direct GETs", targetHits.Load())
	}
	if gotAuth != "Bearer hub-token" || gotMethod != http.MethodGet || gotPath != "/download" {
		t.Fatalf("proxy request auth=%q method=%q path=%q", gotAuth, gotMethod, gotPath)
	}
	if gotQueryURL != target.URL+"/paper.pdf" {
		t.Fatalf("proxy url query = %q", gotQueryURL)
	}
	if gotRange != "bytes=0-7" {
		t.Fatalf("Range = %q", gotRange)
	}
	if gotPostURL != "" {
		t.Fatalf("unexpected POST: %q", gotPostURL)
	}
	body, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "%PDF-hub" {
		t.Fatalf("saved %q", body)
	}
	if result.SavedTo != savePath || result.BytesRead != 8 {
		t.Fatalf("result = %#v", result)
	}
	if result.URL != target.URL+"/paper.pdf" {
		t.Fatalf("result URL leaked proxy path: %q", result.URL)
	}
}

func TestFetchDownloadsDirectlyWhenHubDisabled(t *testing.T) {
	var targetHits, proxyHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-direct"))
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		proxyHits.Add(1)
	}))
	defer proxy.Close()

	savePath := filepath.Join(t.TempDir(), "paper.pdf")
	strategy := hubDownloadStrategy(proxy.URL, "hub-token", false)
	opts := &FetchOptions{SavePath: savePath}
	if err := ApplyHubDownload(opts, strategy); err != nil {
		t.Fatal(err)
	}
	result, err := FetchCtx(context.Background(), target.URL+"/paper.pdf", opts)
	if err != nil {
		t.Fatal(err)
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("proxy received %d requests while hub was off", proxyHits.Load())
	}
	if targetHits.Load() != 1 {
		t.Fatalf("target hits = %d", targetHits.Load())
	}
	body, err := os.ReadFile(result.SavedTo)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "%PDF-direct" {
		t.Fatalf("saved %q", body)
	}
}

func TestFetchHubDownloadMissingTokenDoesNotHitTarget(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHits.Add(1)
	}))
	defer target.Close()

	strategy := hubDownloadStrategy("https://hub.example/searchproxy", "", true)
	opts := &FetchOptions{SavePath: filepath.Join(t.TempDir(), "x.bin")}
	err := ApplyHubDownload(opts, strategy)
	if err == nil || !strings.Contains(err.Error(), "signed-in hub account") {
		t.Fatalf("error = %v", err)
	}
	_, fetchErr := FetchWithStrategyCtx(context.Background(), target.URL+"/x.bin", opts, strategy)
	if fetchErr == nil || !strings.Contains(fetchErr.Error(), "signed-in hub account") {
		t.Fatalf("fetch error = %v", fetchErr)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("target hits = %d", targetHits.Load())
	}
	if _, statErr := os.Stat(opts.SavePath); !os.IsNotExist(statErr) {
		t.Fatalf("401/missing-token wrote a file: %v", statErr)
	}
}

func TestFetchHubDownloadUnauthorizedIsNotAFile(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHits.Add(1)
	}))
	defer target.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid hub session"}`))
	}))
	defer proxy.Close()

	savePath := filepath.Join(t.TempDir(), "secret.bin")
	opts := &FetchOptions{
		SavePath:    savePath,
		HubDownload: &HubDownloadChannel{Token: "bad-token", BaseURL: proxy.URL},
	}
	_, err := FetchCtx(context.Background(), target.URL+"/secret.bin", opts)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "invalid hub session") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "bad-token") {
		t.Fatalf("token leaked: %v", err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("target hits = %d", targetHits.Load())
	}
	if _, statErr := os.Stat(savePath); !os.IsNotExist(statErr) {
		t.Fatalf("401 was written as a file: %v", statErr)
	}
}

func TestFetchHubDownloadRetriesTransientThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"upstream busy"}`))
			return
		}
		_, _ = w.Write([]byte("ok-body"))
	}))
	defer proxy.Close()

	savePath := filepath.Join(t.TempDir(), "ok.bin")
	opts := &FetchOptions{
		SavePath:    savePath,
		HubDownload: &HubDownloadChannel{Token: "hub-token", BaseURL: proxy.URL},
	}
	result, err := FetchCtx(context.Background(), "https://example.com/ok.bin", opts)
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
	body, err := os.ReadFile(result.SavedTo)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok-body" {
		t.Fatalf("saved %q", body)
	}
}

func TestFetchHubDownloadFallsBackToPOST(t *testing.T) {
	var gotPostURL string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Error(w, "use POST", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		gotPostURL = payload.URL
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("from-post"))
	}))
	defer proxy.Close()

	opts := &FetchOptions{
		HubDownload: &HubDownloadChannel{Token: "hub-token", BaseURL: proxy.URL},
		MaxChars:    100,
	}
	result, err := FetchCtx(context.Background(), "https://example.com/page", opts)
	if err != nil {
		t.Fatal(err)
	}
	if gotPostURL != "https://example.com/page" {
		t.Fatalf("POST url = %q", gotPostURL)
	}
	if !strings.Contains(result.Content, "from-post") {
		t.Fatalf("content = %q", result.Content)
	}
}

func TestFetchWithProviderMaclawHubRoutesDownload(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetHits.Add(1)
	}))
	defer target.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer provider-token" {
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("provider-body"))
	}))
	defer proxy.Close()

	savePath := filepath.Join(t.TempDir(), "p.bin")
	result, err := FetchWithProviderCtx(context.Background(), target.URL+"/p.bin", &FetchOptions{SavePath: savePath}, corelib.WebSearchProvider{
		Type:    WebSearchEngineMaclawHub,
		Key:     "provider-token",
		BaseURL: proxy.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if targetHits.Load() != 0 {
		t.Fatalf("target hits = %d", targetHits.Load())
	}
	body, err := os.ReadFile(result.SavedTo)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "provider-body" {
		t.Fatalf("saved %q", body)
	}
}

func TestFetchWithProviderMaclawHubRequiresToken(t *testing.T) {
	_, err := FetchWithProviderCtx(context.Background(), "https://example.com/x", &FetchOptions{}, corelib.WebSearchProvider{
		Type: WebSearchEngineMaclawHub,
	})
	if err == nil || !strings.Contains(err.Error(), "signed-in hub account") {
		t.Fatalf("error = %v", err)
	}
}

func hubDownloadStrategy(baseURL, token string, enabled bool) corelib.WebSearchStrategy {
	strategy := DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID != WebSearchEngineMaclawHub {
			continue
		}
		strategy.Engines[i].Enabled = enabled
		strategy.Engines[i].BaseURL = baseURL
		strategy.Engines[i].APIKey = token
	}
	return strategy
}
