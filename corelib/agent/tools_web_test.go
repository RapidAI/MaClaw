package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestToolWebSearchWithStrategyCtxUsesConfiguredEngineOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "strategy regression" {
			t.Errorf("query = %q", got)
		}
		_, _ = fmt.Fprint(w, `<html><body><a class="result-link" href="https://example.com/result">Strategy result</a><td class="result-snippet">from configured engine</td></body></html>`)
	}))
	t.Cleanup(server.Close)

	strategy := corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion,
		Preset:  corelib.WebSearchPresetCustom,
		Mode:    corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{
			ID: "duckduckgo", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: server.URL,
		}},
		BrowserFallbackEnabled:  false,
		BrowserFallbackEngineID: "bing_cn",
		MinResultsBeforeHedge:   1,
	}

	got := ToolWebSearchWithStrategyCtx(context.Background(), strategy, map[string]interface{}{
		"query":       "strategy regression",
		"max_results": float64(3),
	})
	if !strings.Contains(got, "Strategy result") || !strings.Contains(got, "https://example.com/result") {
		t.Fatalf("result = %q", got)
	}
}

func TestToolWebSearchWithStrategyCtxHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := ToolWebSearchWithStrategyCtx(ctx, corelib.WebSearchStrategy{}, map[string]interface{}{"query": "cancelled"})
	if !strings.Contains(got, "context canceled") {
		t.Fatalf("result = %q, want cancellation", got)
	}
}

func TestToolWebFetchFormatHTMLReturnsRawBody(t *testing.T) {
	const page = `<html><head><meta name="citation_pdf_url" content="https://example.com/paper.pdf"></head><body><article><p>Readable body</p></article></body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, page)
	}))
	t.Cleanup(server.Close)

	got := ToolWebFetchCtx(context.Background(), map[string]interface{}{"url": server.URL, "format": "html", "timeout": float64(3)})
	var decoded struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("web_fetch response is not JSON: %q", got)
	}
	if !strings.Contains(decoded.Content, `<meta name="citation_pdf_url"`) || !strings.Contains(decoded.Content, `<article>`) {
		t.Fatalf("html format did not keep raw body: %q", decoded.Content)
	}

	got = ToolWebFetchCtx(context.Background(), map[string]interface{}{"url": server.URL, "format": "text", "timeout": float64(3)})
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("web_fetch text response is not JSON: %q", got)
	}
	if strings.Contains(decoded.Content, `<article>`) || !strings.Contains(decoded.Content, "Readable body") {
		t.Fatalf("text format did not use readable extraction: %q", decoded.Content)
	}
}

func TestToolWebFetchHonorsShortTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		_, _ = fmt.Fprint(w, "slow body")
	}))
	t.Cleanup(server.Close)

	got := ToolWebFetchCtx(context.Background(), map[string]interface{}{"url": server.URL, "timeout": float64(1)})
	if strings.Contains(got, "slow body") {
		t.Fatalf("1s web_fetch timeout was inflated until the slow response arrived: %q", got)
	}
}
