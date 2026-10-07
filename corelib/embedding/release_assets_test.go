package embedding

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReleaseAssetsTable(t *testing.T) {
	as := ReleaseAssets()
	kinds := map[string]bool{}
	for _, a := range as {
		kinds[a.Kind] = true
		if a.Size <= 0 || len(a.SHA256) != 64 || a.Filename == "" {
			t.Fatalf("bad asset %+v", a)
		}
		if got := a.Spec().URLs[0]; got != ReleaseBaseURL+"/"+a.Filename {
			t.Fatalf("url %s", got)
		}
	}
	for _, k := range []string{"model", "intent_head", "tool_head"} {
		if !kinds[k] {
			t.Fatalf("missing %s asset", k)
		}
	}
	if Qwen3ModelSpec.Size != 639150592 || !strings.HasPrefix(Qwen3ModelSpec.URLs[0], "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/") {
		t.Fatalf("Qwen3 spec %+v", Qwen3ModelSpec)
	}
	s := Qwen3ModelSpec.WithHubFallback("https://hub.example/")
	if len(s.URLs) != 2 || s.URLs[1] != "https://hub.example/api/v1/models/"+Qwen3ModelFilename {
		t.Fatalf("hub fallback %v", s.URLs)
	}
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) logf(f string, a ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	l.mu.Unlock()
}
func (l *logSink) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// github = first URL, hub = second URL (WithHubFallback order).
func multiSourceFetcher(t *testing.T, spec ModelSpec, github, hub http.Handler) (*ModelFetcher, *logSink, *httptest.Server, *httptest.Server) {
	gs := httptest.NewServer(github)
	hs := httptest.NewServer(hub)
	t.Cleanup(gs.Close)
	t.Cleanup(hs.Close)
	spec.URLs = []string{gs.URL + "/Model_Release/" + spec.Filename}
	spec = spec.WithHubFallback(hs.URL)
	t.Setenv(ModelURLEnv, "")
	f := NewModelFetcher(t.TempDir(), spec)
	f.Retries, f.Backoff = 1, time.Millisecond
	ls := &logSink{}
	f.Logf = ls.logf
	return f, ls, gs, hs
}

func TestFetchGitHubOK(t *testing.T) {
	body, spec := fakeModel(30_000)
	hubHits := 0
	f, ls, gs, _ := multiSourceFetcher(t, spec, &rangeServer{body: body}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hubHits++ }))
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Status().Source, gs.URL) || hubHits != 0 || !ls.has("verified from "+gs.URL) {
		t.Fatalf("source %q hub hits %d logs %v", f.Status().Source, hubHits, ls.lines)
	}
}

func TestFetchGitHubFailsThenHubOK(t *testing.T) {
	body, spec := fakeModel(30_000)
	f, ls, _, hs := multiSourceFetcher(t, spec, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusServiceUnavailable)
	}), &rangeServer{body: body})
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Status().Source, hs.URL+"/api/v1/models/") || !ls.has("trying next source") {
		t.Fatalf("source %q logs %v", f.Status().Source, ls.lines)
	}
}

func TestFetchShaMismatchThenNextSource(t *testing.T) {
	body, spec := fakeModel(30_000)
	bad := append([]byte(nil), body...)
	bad[1234] ^= 1
	f, ls, _, hs := multiSourceFetcher(t, spec, &rangeServer{body: bad}, &rangeServer{body: body})
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.Status().Source, hs.URL) || !ls.has("failed verification") {
		t.Fatalf("source %q logs %v", f.Status().Source, ls.lines)
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
}

// Both sources down: Ensure fails, nothing is installed, status says
// unavailable — callers stay on L1 rules / BM25 (no embedder attached).
func TestFetchBothFailGraceful(t *testing.T) {
	_, spec := fakeModel(30_000)
	down := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 502) })
	f, _, _, _ := multiSourceFetcher(t, spec, down, http.NotFoundHandler())
	ready := make(chan string, 1)
	f.Start(context.Background(), func(p string) { ready <- p })
	deadline := time.Now().Add(5 * time.Second)
	for f.Status().State != FetchFailed && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := f.Status(); st.State != FetchFailed || !strings.HasPrefix(st.String(), "local model unavailable") {
		t.Fatalf("status %+v", st)
	}
	select {
	case <-ready:
		t.Fatal("onReady must not fire when every source failed")
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := os.Stat(f.Path()); !os.IsNotExist(err) {
		t.Fatal("nothing may be installed")
	}
}

func TestHeadAssetKeepsLocalFile(t *testing.T) {
	dir := t.TempDir()
	var head ReleaseAsset
	for _, a := range ReleaseAssets() {
		if a.Kind == "intent_head" {
			head = a
		}
	}
	p := filepath.Join(dir, head.Subdir, head.Filename)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(`{"recalibrated":true}`), 0o644)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	f := NewReleaseAssetFetcher(dir, head, srv.URL)
	f.Retries = 1
	got, err := f.Ensure(context.Background())
	if err != nil || got != p || hits != 0 {
		t.Fatalf("got %q err %v hits %d", got, err, hits)
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{"recalibrated":true}` {
		t.Fatal("local head overwritten")
	}
}
