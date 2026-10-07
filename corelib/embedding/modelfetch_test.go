package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fakeModel(n int) ([]byte, ModelSpec) {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i/251)
	}
	s := sha256.Sum256(b)
	return b, ModelSpec{Filename: "m.gguf", Size: int64(n), SHA256: hex.EncodeToString(s[:])}
}

// rangeServer serves body with optional Range support and records requests.
type rangeServer struct {
	body      []byte
	noRange   bool
	failAfter int64 // cut the connection after this many bytes on the first request (0 = never)
	reqs      atomic.Int32
	rangeHdrs []string
	mu        sync.Mutex
	slow      time.Duration
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.reqs.Add(1)
	s.mu.Lock()
	s.rangeHdrs = append(s.rangeHdrs, r.Header.Get("Range"))
	s.mu.Unlock()
	if s.slow > 0 {
		time.Sleep(s.slow)
	}
	start := int64(0)
	if h := r.Header.Get("Range"); h != "" && !s.noRange {
		v := strings.TrimSuffix(strings.TrimPrefix(h, "bytes="), "-")
		start, _ = strconv.ParseInt(v, 10, 64)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.body)-1, len(s.body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)-int(start)))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		w.WriteHeader(http.StatusOK)
	}
	data := s.body[start:]
	if n == 1 && s.failAfter > 0 && s.failAfter < int64(len(data)) {
		_, _ = w.Write(data[:s.failAfter])
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				_ = c.Close()
			}
		}
		return
	}
	_, _ = w.Write(data)
}

func newTestFetcher(t *testing.T, spec ModelSpec, urls ...string) *ModelFetcher {
	t.Helper()
	t.Setenv(ModelURLEnv, "")
	spec.URLs = urls
	f := NewModelFetcher(t.TempDir(), spec)
	f.Retries = 2
	f.Backoff = time.Millisecond
	return f
}

func TestModelFetchMissingEverywhereFailsCleanly(t *testing.T) {
	_, spec := fakeModel(1000)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL+"/a", srv.URL+"/b")
	if got := f.Status().String(); got != "local model missing" {
		t.Fatalf("initial status %q", got)
	}
	_, err := f.Ensure(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	st := f.Status()
	if st.State != FetchFailed || !strings.HasPrefix(st.String(), "local model unavailable") {
		t.Fatalf("status %+v %q", st, st.String())
	}
	if _, err := os.Stat(f.Path()); !os.IsNotExist(err) {
		t.Fatal("no final file may exist after a failed download")
	}
}

func TestModelFetchStartNeverBlocksAndHotReady(t *testing.T) {
	body, spec := fakeModel(300_000)
	rs := &rangeServer{body: body, slow: 200 * time.Millisecond}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	var progress []int
	var pmu sync.Mutex
	f.OnProgress = func(s FetchStatus) {
		if s.State == FetchDownloading {
			pmu.Lock()
			progress = append(progress, s.Percent())
			pmu.Unlock()
		}
	}
	ready := make(chan string, 4)
	t0 := time.Now()
	f.Start(context.Background(), func(p string) { ready <- p })
	f.Start(context.Background(), func(p string) { ready <- p }) // single-flight: second caller just subscribes
	if d := time.Since(t0); d > 100*time.Millisecond {
		t.Fatalf("Start blocked for %v", d)
	}
	for i := 0; i < 2; i++ {
		select {
		case p := <-ready:
			if p != f.Path() {
				t.Fatalf("ready path %q", p)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("not ready")
		}
	}
	if n := rs.reqs.Load(); n != 1 {
		t.Fatalf("single-flight broken: %d requests", n)
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
	pmu.Lock()
	defer pmu.Unlock()
	if len(progress) == 0 || progress[len(progress)-1] != 100 {
		t.Fatalf("progress %v", progress)
	}
	// Already ready: Start fires the callback immediately without a request.
	f.Start(context.Background(), func(p string) { ready <- p })
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("ready callback not fired for an already-ready model")
	}
	if n := rs.reqs.Load(); n != 1 {
		t.Fatalf("re-downloaded a ready model: %d requests", n)
	}
}

func TestModelFetchResumesPartialWithRange(t *testing.T) {
	body, spec := fakeModel(200_000)
	rs := &rangeServer{body: body}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	if err := os.WriteFile(f.PartPath(), body[:123_456], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rs.rangeHdrs[0] != "bytes=123456-" {
		t.Fatalf("range header %q", rs.rangeHdrs[0])
	}
	if _, err := os.Stat(f.PartPath()); !os.IsNotExist(err) {
		t.Fatal(".part should be renamed away")
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
}

func TestModelFetchConnectionDropThenResume(t *testing.T) {
	body, spec := fakeModel(500_000)
	rs := &rangeServer{body: body, failAfter: 100_000}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(rs.rangeHdrs) < 2 || !strings.HasPrefix(rs.rangeHdrs[1], "bytes=") || rs.rangeHdrs[1] == "bytes=0-" {
		t.Fatalf("second request did not resume: %v", rs.rangeHdrs)
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
}

func TestModelFetchServerIgnoresRangeRestarts(t *testing.T) {
	body, spec := fakeModel(100_000)
	rs := &rangeServer{body: body, noRange: true}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	_ = os.WriteFile(f.PartPath(), []byte("garbage-prefix"), 0o644)
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
}

func TestModelFetchCorruptFinalIsReplaced(t *testing.T) {
	body, spec := fakeModel(50_000)
	rs := &rangeServer{body: body}
	srv := httptest.NewServer(rs)
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	bad := append([]byte(nil), body...)
	bad[100] ^= 0xff // same size, wrong hash
	if err := os.WriteFile(f.Path(), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModelFile(f.Path(), spec); !errors.Is(err, ErrModelCorrupt) {
		t.Fatalf("want ErrModelCorrupt, got %v", err)
	}
	if _, err := f.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := VerifyModelFile(f.Path(), spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Path() + ".corrupt"); err != nil {
		t.Fatal("corrupt file should be kept aside for diagnosis")
	}
}

func TestModelFetchCorruptDownloadNeverInstalled(t *testing.T) {
	body, spec := fakeModel(40_000)
	body[7] ^= 0x55 // server serves wrong bytes
	srv := httptest.NewServer(&rangeServer{body: body})
	defer srv.Close()
	f := newTestFetcher(t, spec, srv.URL)
	_, err := f.Ensure(context.Background())
	if !errors.Is(err, ErrModelCorrupt) {
		t.Fatalf("want ErrModelCorrupt, got %v", err)
	}
	if _, err := os.Stat(f.Path()); !os.IsNotExist(err) {
		t.Fatal("corrupt download must not be installed")
	}
	if _, err := os.Stat(f.PartPath()); !os.IsNotExist(err) {
		t.Fatal("corrupt .part must be discarded")
	}
}

func TestVerifyModelFileWrongSizeAndMarker(t *testing.T) {
	body, spec := fakeModel(10_000)
	p := filepath.Join(t.TempDir(), "m.gguf")
	_ = os.WriteFile(p, body[:9_999], 0o644)
	if err := VerifyModelFile(p, spec); !errors.Is(err, ErrModelCorrupt) {
		t.Fatalf("short file: %v", err)
	}
	_ = os.WriteFile(p, body, 0o644)
	if err := VerifyModelFile(p, spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".verified"); err != nil {
		t.Fatal("marker not written")
	}
}

func TestQwen3ModelSpecMatchesLocalFile(t *testing.T) {
	dir := os.Getenv("MACLAW_TEST_MODELS_DIR")
	if dir == "" {
		t.Skip("MACLAW_TEST_MODELS_DIR not set")
	}
	p := filepath.Join(dir, Qwen3ModelFilename)
	if _, err := os.Stat(p); err != nil {
		t.Skip(err)
	}
	tmp := filepath.Join(t.TempDir(), Qwen3ModelFilename)
	if err := os.Symlink(p, tmp); err != nil {
		t.Skip(err)
	}
	// Symlink target is regular; Stat follows it.
	if err := VerifyModelFile(tmp, Qwen3ModelSpec); err != nil {
		t.Fatal(err)
	}
}

func TestModelFetcherEnsureSharedAndForget(t *testing.T) {
	body := []byte("qwen3-model-bytes")
	sum := sha256.Sum256(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	dir := t.TempDir()
	f := NewModelFetcher(dir, ModelSpec{Filename: "m.gguf", URLs: []string{srv.URL + "/m.gguf"}, Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])})
	f.Backoff = 0
	fired := make(chan string, 1)
	f.Start(context.Background(), func(p string) { fired <- p })
	p, err := f.EnsureShared(context.Background())
	if err != nil || p != f.Path() {
		t.Fatalf("EnsureShared = %q, %v", p, err)
	}
	<-fired
	if f.Running() {
		t.Fatal("not running after EnsureShared")
	}
	if err := f.Forget(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.Path()); !os.IsNotExist(err) {
		t.Fatalf("file must be removed: %v", err)
	}
	if st := f.Status(); st.State != FetchMissing {
		t.Fatalf("state after Forget = %v", st.State)
	}
	if _, err := f.EnsureShared(context.Background()); err != nil {
		t.Fatalf("redownload: %v", err)
	}
	if err := VerifyModelFile(f.Path(), f.Spec()); err != nil {
		t.Fatal(err)
	}
}
