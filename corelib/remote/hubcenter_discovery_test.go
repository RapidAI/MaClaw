package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discoveryServer(score int, routable bool, urls []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/client/quality":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"quality_score":  score,
				"routable":       routable,
				"service_status": "healthy",
				"features":       map[string]any{"can_resolve": true},
			})
		case "/api/client/hubcenters":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":          true,
				"urls":        urls,
				"count":       len(urls),
				"ttl_seconds": 300,
				"nodes":       []map[string]any{},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFetchHubCenterDiscovery_UsesURLList(t *testing.T) {
	srv := discoveryServer(95, true, []string{"https://hubs.maclaw.top", "https://hubs2.maclaw.top"})
	defer srv.Close()

	view, err := FetchHubCenterDiscovery(context.Background(), http.DefaultClient, srv.URL)
	if err != nil {
		t.Fatalf("FetchHubCenterDiscovery() error = %v", err)
	}
	if view == nil {
		t.Fatal("view is nil")
	}
	if len(view.URLs) != 2 {
		t.Fatalf("urls = %v", view.URLs)
	}
	if view.Count != 2 {
		t.Fatalf("count = %d", view.Count)
	}
}

func TestDiscoverHubCenterURLs_MergesSeedsAndDiscoveredURLs(t *testing.T) {
	InvalidateCenterCache()
	primary := discoveryServer(99, true, []string{"https://hubs.maclaw.top", "https://hubs2.maclaw.top"})
	defer primary.Close()
	secondary := discoveryServer(80, true, nil)
	defer secondary.Close()

	ordered := DiscoverHubCenterURLs(context.Background(), http.DefaultClient, []string{secondary.URL, primary.URL}, "")
	if len(ordered) != 4 {
		t.Fatalf("ordered len = %d, ordered=%v", len(ordered), ordered)
	}
	if ordered[0] != primary.URL {
		t.Fatalf("first = %q, want %q", ordered[0], primary.URL)
	}
	if ordered[1] != secondary.URL {
		t.Fatalf("second = %q, want %q", ordered[1], secondary.URL)
	}
	discovered := map[string]bool{}
	for _, url := range ordered[2:] {
		discovered[url] = true
	}
	if !discovered["https://hubs.maclaw.top"] || !discovered["https://hubs2.maclaw.top"] {
		t.Fatalf("unexpected discovered urls = %v", ordered)
	}
}

func TestSelectBestCenter_CacheIsScopedPerURLSet(t *testing.T) {
	InvalidateCenterCache()
	ResetFailureMemory()
	a := discoveryServer(90, true, nil)
	defer a.Close()
	b := discoveryServer(80, true, nil)
	defer b.Close()
	c := discoveryServer(99, true, nil)
	defer c.Close()

	first := SelectBestCenter(context.Background(), http.DefaultClient, []string{a.URL, b.URL}, "")
	if len(first) != 2 || first[0] != a.URL {
		t.Fatalf("first result = %v", first)
	}
	second := SelectBestCenter(context.Background(), http.DefaultClient, []string{c.URL}, "")
	if len(second) != 1 || second[0] != c.URL {
		t.Fatalf("second result = %v", second)
	}
}
