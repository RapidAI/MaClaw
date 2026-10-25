package formula

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func TestReleasePinsMatchManifest(t *testing.T) {
	got := map[string]modelAsset{}
	for _, a := range modelAssets {
		got[a.Filename] = a
	}
	n := 0
	for _, a := range embedding.ReleaseAssets() {
		if a.Kind != "formula" {
			continue
		}
		n++
		m, ok := got[a.Filename]
		if !ok || m.Size != a.Size || !strings.EqualFold(m.SHA256, a.SHA256) || a.Subdir != "formula" {
			t.Fatalf("manifest %+v local %+v", a, m)
		}
		if a.Spec().URLs[0] != ReleaseBaseURL+"/"+a.Filename {
			t.Fatalf("url %s", a.Spec().URLs[0])
		}
	}
	if n != len(modelAssets) {
		t.Fatalf("formula assets %d, manifest %d", len(modelAssets), n)
	}
}

func TestDefaultModelDir(t *testing.T) {
	old := maclawpath.BaseDir()
	dir := t.TempDir()
	maclawpath.SetBaseDir(dir)
	t.Cleanup(func() { maclawpath.SetBaseDir(old) })
	got, err := DefaultModelDir()
	if err != nil || got != filepath.Join(dir, "models", "formula") {
		t.Fatalf("dir %s err %v", got, err)
	}
}

func TestSourceURLsGitHubThenHub(t *testing.T) {
	t.Setenv(hubURLEnv, "https://from-env.example")
	got := sourceURLs(ReleaseBaseURL, "", fileEncoder)
	if len(got) != 2 || got[0] != ReleaseBaseURL+"/"+fileEncoder || got[1] != "https://from-env.example/api/v1/models/"+fileEncoder {
		t.Fatalf("%v", got)
	}
	got = sourceURLs(ReleaseBaseURL, "https://arg.example/", fileEncoder)
	if got[1] != "https://arg.example/api/v1/models/"+fileEncoder {
		t.Fatalf("arg hub %v", got)
	}
}

func TestModelFilePrefersReleaseName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, legacyEncoder), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := modelFile(dir, fileEncoder, legacyEncoder); filepath.Base(got) != legacyEncoder {
		t.Fatalf("legacy %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, fileEncoder), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := modelFile(dir, fileEncoder, legacyEncoder); filepath.Base(got) != fileEncoder {
		t.Fatalf("release %s", got)
	}
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fakeAsset(body []byte) modelAsset {
	return modelAsset{Filename: "formulanet-encoder.onnx", Size: int64(len(body)), SHA256: shaHex(body)}
}

func testOpt(base string) fetchOpt {
	opt := defaultFetchOpt()
	opt.retries = 1
	opt.backoff = 0
	opt.releaseBase = base
	return opt
}

func TestFilesPresentAcceptsPinnedSize(t *testing.T) {
	dir := t.TempDir()
	body := []byte("formula-bytes")
	asset := fakeAsset(body)
	asset.Legacy = "encoder_model.onnx"
	if size, ok := filesPresent(dir, []modelAsset{asset}); ok || size != 0 {
		t.Fatalf("missing file size %d ok %v", size, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, asset.Legacy), body, 0o644); err != nil {
		t.Fatal(err)
	}
	size, ok := filesPresent(dir, []modelAsset{asset})
	if !ok || size != int64(len(body)) {
		t.Fatalf("legacy present size %d ok %v", size, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, asset.Filename), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	size, ok = filesPresent(dir, []modelAsset{asset})
	if !ok || size != int64(len(body)) {
		t.Fatalf("short release stub hid legacy file, size %d ok %v", size, ok)
	}
	if got := pinnedFile(dir, asset); filepath.Base(got) != asset.Legacy {
		t.Fatalf("pinned %s", got)
	}
	shortOnly := t.TempDir()
	if err := os.WriteFile(filepath.Join(shortOnly, asset.Filename), []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := filesPresent(shortOnly, []modelAsset{asset}); ok {
		t.Fatal("short release alone should not count")
	}
}

func TestDownloadReportsFinalByteCount(t *testing.T) {
	body := []byte("formula-bytes-progress")
	asset := fakeAsset(body)
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer gs.Close()
	dir := t.TempDir()
	var last int64
	opt := testOpt(gs.URL)
	opt.onBytes = func(n int64) { last = n }
	if err := ensureOne(context.Background(), dir, "", asset, opt); err != nil {
		t.Fatal(err)
	}
	if last != int64(len(body)) {
		t.Fatalf("reported %d, want %d", last, len(body))
	}
}

func TestEnsureGitHubOKSkipsHub(t *testing.T) {
	body := []byte("formula-bytes-github")
	asset := fakeAsset(body)
	hubHits := 0
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+asset.Filename {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer gs.Close()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hubHits++ }))
	defer hs.Close()
	dir := t.TempDir()
	if err := ensureOne(context.Background(), dir, hs.URL, asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	if hubHits != 0 {
		t.Fatalf("hub hits %d", hubHits)
	}
	got, err := os.ReadFile(filepath.Join(dir, asset.Filename))
	if err != nil || string(got) != string(body) {
		t.Fatalf("installed %q err %v", got, err)
	}
	// A verified file is not downloaded again.
	if err := ensureOne(context.Background(), dir, hs.URL, asset, testOpt(gs.URL)); err != nil || hubHits != 0 {
		t.Fatalf("second ensure err %v hub hits %d", err, hubHits)
	}
}

func TestEnsureGitHubFailsThenHubOK(t *testing.T) {
	body := []byte("formula-bytes-from-hub")
	asset := fakeAsset(body)
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unreachable", http.StatusServiceUnavailable)
	}))
	defer gs.Close()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models/"+asset.Filename {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer hs.Close()
	dir := t.TempDir()
	if err := ensureOne(context.Background(), dir, hs.URL, asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, asset.Filename))
	if err != nil || string(got) != string(body) {
		t.Fatalf("installed %q err %v", got, err)
	}
}

func TestEnsureShaMismatchThenHub(t *testing.T) {
	body := []byte("formula-bytes-correct!!")
	asset := fakeAsset(body)
	bad := append([]byte(nil), body...)
	bad[0] ^= 1
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bad)
	}))
	defer gs.Close()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer hs.Close()
	dir := t.TempDir()
	if err := ensureOne(context.Background(), dir, hs.URL, asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, asset.Filename))
	if string(got) != string(body) {
		t.Fatalf("installed %q", got)
	}
}

func TestEnsureBothFailLeavesNothing(t *testing.T) {
	asset := fakeAsset([]byte("formula-bytes"))
	down := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	})
	gs := httptest.NewServer(down)
	defer gs.Close()
	hs := httptest.NewServer(down)
	defer hs.Close()
	dir := t.TempDir()
	err := ensureOne(context.Background(), dir, hs.URL, asset, testOpt(gs.URL))
	if err == nil || !strings.Contains(err.Error(), "download") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, asset.Filename)); !os.IsNotExist(statErr) {
		t.Fatal("final file installed")
	}
}

func TestDownloadResumesWithRange(t *testing.T) {
	body := []byte("abcdefghijklmnopqrstuvwxyz012345")
	asset := fakeAsset(body)
	var ranges []string
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rng := r.Header.Get("Range")
		ranges = append(ranges, rng)
		if rng == "" {
			_, _ = w.Write(body[:10])
			return
		}
		if rng != "bytes=10-" {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 10-31/32")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[10:])
	}))
	defer gs.Close()
	dir := t.TempDir()
	opt := testOpt(gs.URL)
	// First response is a short body, so the attempt fails and keeps .part.
	if err := ensureOne(context.Background(), dir, "", asset, opt); err == nil {
		t.Fatal("short github body must not install")
	}
	part := filepath.Join(dir, asset.Filename+".part")
	if st, err := os.Stat(part); err != nil || st.Size() != 10 {
		t.Fatalf("part %v", err)
	}
	opt.retries = 1
	if err := ensureOne(context.Background(), dir, "", asset, opt); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, asset.Filename))
	if string(got) != string(body) {
		t.Fatalf("installed %q ranges %v", got, ranges)
	}
	if len(ranges) < 2 || ranges[1] != "bytes=10-" {
		t.Fatalf("ranges %v", ranges)
	}
}

func TestVerifyLoadedMissing(t *testing.T) {
	if err := VerifyLoaded(t.TempDir()); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestVerifyFileHashesCorruptBytesOnce(t *testing.T) {
	body := bytes.Repeat([]byte("x"), 64)
	asset := fakeAsset(body)
	path := filepath.Join(t.TempDir(), asset.Filename)
	if err := os.WriteFile(path, bytes.Repeat([]byte("y"), len(body)), 0o644); err != nil {
		t.Fatal(err)
	}
	before := atomic.LoadInt64(&verifyHashReads)
	if err := verifyFile(path, asset); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if err := verifyFile(path, asset); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&verifyHashReads) - before; got != 1 {
		t.Fatalf("hash reads %d, want 1", got)
	}
	if err := os.Chtimes(path, time.Now().Add(2*time.Hour), time.Now().Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, asset); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&verifyHashReads) - before; got != 2 {
		t.Fatalf("hash reads after mtime change %d, want 2", got)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, asset); err != nil {
		t.Fatal(err)
	}
	if err := verifyFile(path, asset); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(&verifyHashReads) - before; got != 3 {
		t.Fatalf("hash reads after repair %d, want 3", got)
	}
}

func TestEnsureVerifiedLegacySkipsDownload(t *testing.T) {
	body := []byte("formula-bytes-legacy")
	asset := fakeAsset(body)
	asset.Legacy = "encoder_model.onnx"
	hits := 0
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "should not download", http.StatusInternalServerError)
	}))
	defer gs.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, asset.Legacy), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOne(context.Background(), dir, "", asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("download hits %d", hits)
	}
	if _, err := os.Stat(filepath.Join(dir, asset.Filename)); !os.IsNotExist(err) {
		t.Fatalf("release file err %v", err)
	}
}

func TestEnsureCorruptReleaseKeepsVerifiedLegacy(t *testing.T) {
	body := []byte("formula-bytes-legacy-ok")
	asset := fakeAsset(body)
	asset.Legacy = "encoder_model.onnx"
	bad := append([]byte(nil), body...)
	bad[0] ^= 1
	hits := 0
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.Error(w, "should not download", http.StatusInternalServerError)
	}))
	defer gs.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, asset.Legacy), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asset.Filename), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOne(context.Background(), dir, "", asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	if hits != 0 {
		t.Fatalf("download hits %d", hits)
	}
	if _, err := os.Stat(filepath.Join(dir, asset.Filename)); !os.IsNotExist(err) {
		t.Fatal("corrupt release file still in place")
	}
	got, err := os.ReadFile(filepath.Join(dir, asset.Legacy))
	if err != nil || string(got) != string(body) {
		t.Fatalf("legacy %q err %v", got, err)
	}
	if got := pinnedFile(dir, asset); filepath.Base(got) != asset.Legacy {
		t.Fatalf("pinned %s", got)
	}
}

func TestEnsureSameSizeCorruptReleaseReplaced(t *testing.T) {
	body := []byte("formula-bytes-correct!!")
	asset := fakeAsset(body)
	bad := append([]byte(nil), body...)
	bad[0] ^= 1
	gs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer gs.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, asset.Filename), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureOne(context.Background(), dir, "", asset, testOpt(gs.URL)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, asset.Filename))
	if err != nil || string(got) != string(body) {
		t.Fatalf("installed %q err %v", got, err)
	}
}
