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
	for _, k := range []string{"model", "intent_head", "tool_head", "interrupt_head"} {
		if !kinds[k] {
			t.Fatalf("missing %s asset", k)
		}
	}
	for _, a := range as {
		// guiapp/interrupt_relevance.go loads <models>/interrupt_heads/interrupt_relevance_qwen3go.json.
		if a.Kind == "interrupt_head" && (a.Subdir != "interrupt_heads" || a.Filename != "interrupt_relevance_qwen3go.json") {
			t.Fatalf("interrupt head install path %s/%s", a.Subdir, a.Filename)
		}
	}
	if Qwen3ModelSpec.Size != 639150592 || !strings.HasPrefix(Qwen3ModelSpec.URLs[0], "https://github.com/RapidAI/MaClaw/releases/download/Model_Release/") {
		t.Fatalf("Qwen3 spec %+v", Qwen3ModelSpec)
	}
	if !IsHeadKind("intent_head") || IsHeadKind("formula") || IsHeadKind("model") || IsHeadKind("layout") || IsHeadKind("table") {
		t.Fatal("IsHeadKind")
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

func TestLayoutAndTableReleaseFetch(t *testing.T) {
	want := map[string]ReleaseAsset{
		"PP-DocLayoutV3.onnx":      {Kind: "layout", Subdir: "layout", Size: 5088167, SHA256: "c0721928ff08741bb208ebed539c77170db5234a68cb7e546e6cc9bc172a695b"},
		"PP-DocLayoutV3.onnx.data": {Kind: "layout", Subdir: "layout", Size: 136774480, SHA256: "34df3e4b79d7bbbf82abce1b4f3cde3d540fa57ad42ec8905c352b97c408d437"},
		"unet.onnx":                {Kind: "table", Subdir: "table", Size: 8335007, SHA256: "0ea48d3a17e35ef5c2e498a5e799566073234d39b1079ca21d9f4fafe73c6d20"},
	}
	got := map[string]bool{}
	for _, asset := range ReleaseAssets() {
		spec, ok := want[asset.Filename]
		if !ok {
			continue
		}
		got[asset.Filename] = true
		if asset.Kind != spec.Kind || asset.Subdir != spec.Subdir || asset.Size != spec.Size || asset.SHA256 != spec.SHA256 {
			t.Fatalf("pin %+v", asset)
		}
		fetcher := NewReleaseAssetFetcher(t.TempDir(), asset, "https://hub.example")
		if fetcher.KeepExisting {
			t.Fatalf("%s kept an existing file", asset.Filename)
		}
		if len(fetcher.spec.URLs) != 2 || fetcher.spec.URLs[0] != ReleaseBaseURL+"/"+asset.Filename || fetcher.spec.URLs[1] != "https://hub.example/api/v1/models/"+asset.Filename {
			t.Fatalf("urls %v", fetcher.spec.URLs)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("pins %v", got)
	}
	if err := EnsureReleaseKind(context.Background(), t.TempDir(), "", "not-a-kind"); err == nil {
		t.Fatal("unknown kind")
	}
}

func TestHeadFetcherIgnoresEmbedModelURL(t *testing.T) {
	t.Setenv(ModelURLEnv, "https://example.invalid/custom.gguf")
	var head ReleaseAsset
	for _, a := range ReleaseAssets() {
		if a.Kind == "intent_head" {
			head = a
			break
		}
	}
	if head.Filename == "" {
		t.Fatal("no intent head in release assets")
	}
	f := NewReleaseAssetFetcher(t.TempDir(), head, "https://hub.example")
	if len(f.spec.URLs) != 2 {
		t.Fatalf("urls = %v", f.spec.URLs)
	}
	for _, u := range f.spec.URLs {
		if strings.Contains(u, "custom.gguf") || !strings.Contains(u, head.Filename) {
			t.Fatalf("head download followed the GGUF override: %v", f.spec.URLs)
		}
	}
	if !strings.Contains(f.spec.URLs[1], "https://hub.example/api/v1/models/") {
		t.Fatalf("hub mirror missing: %v", f.spec.URLs)
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
