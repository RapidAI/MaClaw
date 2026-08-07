package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/ocr"
)

// fakeONNXPayload returns bytes that satisfy ocr.ModelFileStatus's header
// check (ONNX field-1 varint tag 0x08 + plausible ir_version).
func fakeONNXPayload(tag string) []byte {
	return append([]byte{0x08, 0x08}, []byte("fake onnx "+tag)...)
}

// withOCRTestHome points the per-user config/home dir at a temp dir so the
// download flow's config persistence never touches the real user config.
func withOCRTestHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("HOME", tmp)
}

// withOCRTestModelsDir redirects the shared models directory into a temp dir
// (same seam the diarization tests use) and returns the models dir.
func withOCRTestModelsDir(t *testing.T) string {
	t.Helper()
	previous := embedding.BaseDirFunc.Load()
	base := t.TempDir()
	embedding.BaseDirFunc.Store(func() string { return base })
	t.Cleanup(func() { embedding.BaseDirFunc.Store(previous) })
	return filepath.Join(base, "models")
}

// withOCRModelURLs overrides the default HuggingFace URL resolvers.
func withOCRModelURLs(t *testing.T, detURL, recURL string) {
	t.Helper()
	prevDet, prevRec := ocrDetModelURL, ocrRecModelURL
	ocrDetModelURL = func(string) string { return detURL }
	ocrRecModelURL = func(string) string { return recURL }
	t.Cleanup(func() { ocrDetModelURL, ocrRecModelURL = prevDet, prevRec })
}

// isolateSharedOCRProvider replaces the process-wide provider singleton with
// a fresh one for the duration of the test.
func isolateSharedOCRProvider(t *testing.T) {
	t.Helper()
	sharedOCROnce = sync.Once{}
	sharedOCRProvider = nil
	t.Cleanup(func() {
		if sharedOCRProvider != nil {
			sharedOCRProvider.Close()
		}
		sharedOCROnce = sync.Once{}
		sharedOCRProvider = nil
	})
}

func newOCRTestApp(cfg corelib.AppConfig) *App {
	return &App{configCacheValid: true, configCache: cfg}
}

// ocrModelServer serves the fake det/rec payloads at /det and /rec.
func ocrModelServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheckOCRModel(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})

	if got := app.CheckOCRModel(); got["exists"] != false {
		t.Fatalf("missing model status = %#v", got)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	detPath := filepath.Join(dir, ocr.DetModelFilename(corelib.DefaultOCRModelTier))
	recPath := filepath.Join(dir, ocr.RecModelFilename(corelib.DefaultOCRModelTier))

	// Corrupt det file alone is not enough.
	if err := os.WriteFile(detPath, []byte("<html>error</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := app.CheckOCRModel(); got["exists"] != false {
		t.Fatalf("corrupt det status = %#v", got)
	}
	// Valid det but missing rec is still not ready.
	if err := os.WriteFile(detPath, fakeONNXPayload("det"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := app.CheckOCRModel(); got["exists"] != false {
		t.Fatalf("det-only status = %#v", got)
	}
	if err := os.WriteFile(recPath, fakeONNXPayload("rec"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := app.CheckOCRModel()
	wantSize := int64(len(fakeONNXPayload("det")) + len(fakeONNXPayload("rec")))
	if got["exists"] != true || got["size"] != wantSize || got["model"] != "ppocrv6_small" {
		t.Fatalf("ready model status = %#v", got)
	}
}

func TestDownloadOCRModelHappyPath(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/det":
			_, _ = w.Write(fakeONNXPayload("det"))
		case "/rec":
			_, _ = w.Write(fakeONNXPayload("rec"))
		default:
			http.NotFound(w, r)
		}
	})
	withOCRModelURLs(t, srv.URL+"/det", srv.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	if err := app.DownloadOCRModel(); err != nil {
		t.Fatalf("DownloadOCRModel: %v", err)
	}
	for _, name := range []string{ocr.DetModelFilename("small"), ocr.RecModelFilename("small")} {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, name)); !ok {
			t.Fatalf("%s missing/invalid after download", name)
		}
	}
	if got := app.CheckOCRModel(); got["exists"] != true {
		t.Fatalf("CheckOCRModel after download = %#v", got)
	}
}

func TestDownloadOCRModelHubFallbackAfterHuggingFaceFailures(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	var hfHits atomic.Int32
	hf := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hfHits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	hub := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch filepath.Base(r.URL.Path) {
		case ocr.DetModelFilename("small"):
			_, _ = w.Write(fakeONNXPayload("det"))
		case ocr.RecModelFilename("small"):
			_, _ = w.Write(fakeONNXPayload("rec"))
		default:
			http.NotFound(w, r)
		}
	})
	withOCRModelURLs(t, hf.URL+"/det", hf.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, RemoteHubURL: hub.URL + "/"})
	if err := app.DownloadOCRModel(); err != nil {
		t.Fatalf("DownloadOCRModel with hub fallback: %v", err)
	}
	// 3 silent HF retries per file before falling back to the hub.
	if got := hfHits.Load(); got != 6 {
		t.Fatalf("HuggingFace attempts = %d, want 6 (3 per file)", got)
	}
	for _, name := range []string{ocr.DetModelFilename("small"), ocr.RecModelFilename("small")} {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, name)); !ok {
			t.Fatalf("%s missing/invalid after hub fallback", name)
		}
	}
}

func TestDownloadOCRModelBothSourcesFailLeavesNoFiles(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	hf := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	hub := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	withOCRModelURLs(t, hf.URL+"/det", hf.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, RemoteHubURL: hub.URL})
	err := app.DownloadOCRModel()
	if err == nil {
		t.Fatal("DownloadOCRModel succeeded with both sources failing")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".onnx") {
			t.Fatalf("partial model file left behind: %s", e.Name())
		}
	}
}

func TestDownloadOCRModelNoHubConfigured(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	hf := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	withOCRModelURLs(t, hf.URL+"/det", hf.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	if err := app.DownloadOCRModel(); err == nil {
		t.Fatal("DownloadOCRModel succeeded with HF down and no hub URL")
	}
}

func TestDownloadOCRModelCorruptPayloadRejectedAndDeleted(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	// Server answers 200 with an HTML error page — must fail ModelFileStatus.
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	})
	withOCRModelURLs(t, srv.URL+"/det", srv.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	err := app.DownloadOCRModel()
	if err == nil || !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("DownloadOCRModel error = %v, want validation failure", err)
	}
	detPath := filepath.Join(dir, ocr.DetModelFilename("small"))
	if _, statErr := os.Stat(detPath); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt det file not deleted (stat err = %v)", statErr)
	}
}

func TestDownloadOCRModelConcurrentCallersDeduped(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	release := make(chan struct{})
	var hits atomic.Int32
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release // hold the first download so other callers overlap it
		if r.URL.Path == "/det" {
			_, _ = w.Write(fakeONNXPayload("det"))
		} else {
			_, _ = w.Write(fakeONNXPayload("rec"))
		}
	})
	withOCRModelURLs(t, srv.URL+"/det", srv.URL+"/rec")

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	const callers = 4
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { errs <- app.DownloadOCRModel() }()
	}
	// Wait for the winning caller's first request to arrive, give the losers a
	// moment to bounce off TryLock, then unblock the download.
	for hits.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent DownloadOCRModel: %v", err)
		}
	}
	// Exactly one caller did the work: 1 det + 1 rec request, no duplicates.
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want 2 (dedup via TryLock failed)", got)
	}
}

func TestDownloadOCRModelReturnsNilWhenLockHeld(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	var hits atomic.Int32
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(fakeONNXPayload("x"))
	})
	withOCRModelURLs(t, srv.URL+"/det", srv.URL+"/rec")

	ocrDownloadMu.Lock()
	defer ocrDownloadMu.Unlock()
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	done := make(chan error, 1)
	go func() { done <- app.DownloadOCRModel() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DownloadOCRModel with held lock = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DownloadOCRModel blocked on ocrDownloadMu instead of TryLock-bailing")
	}
	if hits.Load() != 0 {
		t.Fatalf("server hit %d times while lock was held", hits.Load())
	}
}

func TestEnsureOCRModelFiles(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	// OCR disabled in config so the kicked-off background preload is a no-op.
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: false})

	detPath, recPath, ok := app.ensureOCRModelFiles()
	if ok {
		t.Fatal("ensureOCRModelFiles ok=true with no model files")
	}
	if detPath != filepath.Join(dir, ocr.DetModelFilename("small")) ||
		recPath != filepath.Join(dir, ocr.RecModelFilename("small")) {
		t.Fatalf("paths = %q, %q; want small tier in %s", detPath, recPath, dir)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(detPath, fakeONNXPayload("det"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, fakeONNXPayload("rec"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := app.ensureOCRModelFiles(); !ok {
		t.Fatal("ensureOCRModelFiles ok=false with both model files present")
	}
}

func TestOCRModelTierFallsBackToDefault(t *testing.T) {
	app := newOCRTestApp(corelib.AppConfig{OCRModelTier: "huge"})
	if got := app.ocrModelTier(); got != corelib.DefaultOCRModelTier {
		t.Fatalf("ocrModelTier() = %q, want %q", got, corelib.DefaultOCRModelTier)
	}
	app = newOCRTestApp(corelib.AppConfig{OCRModelTier: "tiny"})
	if got := app.ocrModelTier(); got != "tiny" {
		t.Fatalf("ocrModelTier() = %q, want tiny", got)
	}
}

func TestOCRDownloadEmitsNoEventWithoutWailsContext(t *testing.T) {
	// emitOCRProgress must be safe on a bare App (no Wails runtime ctx).
	app := &App{}
	app.emitOCRProgress(50, 1, 2, "")
	app.emitOCRProgress(0, 0, 0, fmt.Sprintf("err"))
}
