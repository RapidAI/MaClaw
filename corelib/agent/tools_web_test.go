package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
