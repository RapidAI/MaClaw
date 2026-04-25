package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func qualityHandler(score int, routable bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client/quality" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"quality_score":  score,
			"routable":       routable,
			"service_status": "healthy",
			"features":       map[string]any{"can_resolve": true},
		})
	}
}

func TestProbeCenterQuality_Healthy(t *testing.T) {
	srv := httptest.NewServer(qualityHandler(95, true))
	defer srv.Close()

	q := ProbeCenterQuality(context.Background(), http.DefaultClient, srv.URL)
	if !q.Reachable {
		t.Fatal("expected reachable")
	}
	if !q.Routable {
		t.Fatal("expected routable")
	}
	if q.QualityScore != 95 {
		t.Fatalf("QualityScore = %d, want 95", q.QualityScore)
	}
	if q.RTTMs <= 0 {
		t.Fatal("expected positive RTT")
	}
}

func TestProbeCenterQuality_Unreachable(t *testing.T) {
	q := ProbeCenterQuality(context.Background(), http.DefaultClient, "http://127.0.0.1:1")
	if q.Reachable {
		t.Fatal("expected unreachable")
	}
}

func TestSelectBestCenter_SortsByQuality(t *testing.T) {
	InvalidateCenterCache()

	low := httptest.NewServer(qualityHandler(60, true))
	defer low.Close()
	high := httptest.NewServer(qualityHandler(98, true))
	defer high.Close()
	mid := httptest.NewServer(qualityHandler(80, true))
	defer mid.Close()

	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{low.URL, high.URL, mid.URL}, "")

	if len(ordered) != 3 {
		t.Fatalf("got %d results, want 3", len(ordered))
	}
	if ordered[0] != high.URL {
		t.Fatalf("first = %q, want %q (highest quality)", ordered[0], high.URL)
	}
	if ordered[1] != mid.URL {
		t.Fatalf("second = %q, want %q", ordered[1], mid.URL)
	}
}

func TestSelectBestCenter_PreferredBonus(t *testing.T) {
	InvalidateCenterCache()

	a := httptest.NewServer(qualityHandler(90, true))
	defer a.Close()
	b := httptest.NewServer(qualityHandler(92, true))
	defer b.Close()

	// Without preferred, b wins (92 > 90).
	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{a.URL, b.URL}, "")
	if ordered[0] != b.URL {
		t.Fatalf("without preferred: first = %q, want %q", ordered[0], b.URL)
	}

	// With a as preferred, a gets +5 → 95 > 92 → a wins.
	InvalidateCenterCache()
	ordered = SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{a.URL, b.URL}, a.URL)
	if ordered[0] != a.URL {
		t.Fatalf("with preferred: first = %q, want %q", ordered[0], a.URL)
	}
}

func TestSelectBestCenter_UnreachableNodesSortedLast(t *testing.T) {
	InvalidateCenterCache()

	good := httptest.NewServer(qualityHandler(80, true))
	defer good.Close()

	ordered := SelectBestCenter(context.Background(), http.DefaultClient,
		[]string{"http://127.0.0.1:1", good.URL}, "")

	if len(ordered) != 2 {
		t.Fatalf("got %d results, want 2", len(ordered))
	}
	if ordered[0] != good.URL {
		t.Fatalf("first = %q, want reachable node %q", ordered[0], good.URL)
	}
}

func TestSelectBestCenter_CacheTTL(t *testing.T) {
	InvalidateCenterCache()

	srv := httptest.NewServer(qualityHandler(99, true))
	defer srv.Close()

	urls := []string{srv.URL}
	ordered1 := SelectBestCenter(context.Background(), http.DefaultClient, urls, "")
	if len(ordered1) != 1 || ordered1[0] != srv.URL {
		t.Fatalf("first call: got %v", ordered1)
	}

	// Second call should use cache (even if server is now down).
	srv.Close()
	ordered2 := SelectBestCenter(context.Background(), http.DefaultClient, urls, "")
	if len(ordered2) != 1 || ordered2[0] != srv.URL {
		t.Fatalf("cached call: got %v", ordered2)
	}
}

func TestSelectBestCenter_SingleURL(t *testing.T) {
	InvalidateCenterCache()
	ordered := SelectBestCenter(context.Background(), http.DefaultClient, []string{"https://example.com"}, "")
	if len(ordered) != 1 || ordered[0] != "https://example.com" {
		t.Fatalf("single URL: got %v", ordered)
	}
}

func TestSelectBestCenter_Empty(t *testing.T) {
	ordered := SelectBestCenter(context.Background(), http.DefaultClient, nil, "")
	if ordered != nil {
		t.Fatalf("empty: got %v", ordered)
	}
}
