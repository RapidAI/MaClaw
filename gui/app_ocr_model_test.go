package main

import (
	"archive/zip"
	"bytes"
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

// withOCRModelsZipURL overrides the default GitHub release zip URL.
func withOCRModelsZipURL(t *testing.T, url string) {
	t.Helper()
	prev := ocrModelsZipURL
	ocrModelsZipURL = url
	t.Cleanup(func() { ocrModelsZipURL = prev })
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

// ocrModelServer serves the fake zip payload through a mock HTTP server.
func ocrModelServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv
}

// buildOCRModelsZip packs the 6 expected tier det/rec .onnx entries (with
// fake payloads) plus a non-.onnx entry that extraction must skip.
func buildOCRModelsZip(t *testing.T, payloadFor func(name string) []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range expectedOCRModelFilenames() {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payloadFor(name)); err != nil {
			t.Fatal(err)
		}
	}
	// Non-model entries shipped in the real bundle must be skipped.
	for _, name := range []string{"dict_ppocrv6_tiny.txt", "rec_inference.yml"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("ignored " + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// validOCRModelsZip returns a zip whose 6 .onnx entries all pass
// ocr.ModelFileStatus.
func validOCRModelsZip(t *testing.T) []byte {
	t.Helper()
	return buildOCRModelsZip(t, func(name string) []byte { return fakeONNXPayload(name) })
}

// serveOCRZip answers every request with the given zip bytes.
func serveOCRZip(t *testing.T, payload []byte) *httptest.Server {
	t.Helper()
	return ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	})
}

// assertOCRModelsExtracted fails unless all 6 tier files exist and validate.
func assertOCRModelsExtracted(t *testing.T, dir string) {
	t.Helper()
	for _, name := range expectedOCRModelFilenames() {
		if _, ok := ocr.ModelFileStatus(filepath.Join(dir, name)); !ok {
			t.Fatalf("%s missing/invalid after download", name)
		}
	}
}

// assertNoONNXFiles fails when any .onnx file is left in dir.
func assertNoONNXFiles(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".onnx") {
			t.Fatalf("partial model file left behind: %s", e.Name())
		}
	}
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

	srv := serveOCRZip(t, validOCRModelsZip(t))
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	if err := app.DownloadOCRModel(); err != nil {
		t.Fatalf("DownloadOCRModel: %v", err)
	}
	// All tiers are extracted so tier switching is instant.
	assertOCRModelsExtracted(t, dir)
	// The zip is a transport artifact and must be gone after extraction.
	if _, err := os.Stat(filepath.Join(dir, ocr.ModelsZipFilename)); !os.IsNotExist(err) {
		t.Fatalf("models zip not deleted after extraction (stat err = %v)", err)
	}
	if got := app.CheckOCRModel(); got["exists"] != true {
		t.Fatalf("CheckOCRModel after download = %#v", got)
	}
}

func TestDownloadOCRModelHubFallbackAfterGitHubFailures(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	var ghHits atomic.Int32
	gh := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		ghHits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	hub := serveOCRZip(t, validOCRModelsZip(t))
	withOCRModelsZipURL(t, gh.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, RemoteHubURL: hub.URL + "/"})
	if err := app.DownloadOCRModel(); err != nil {
		t.Fatalf("DownloadOCRModel with hub fallback: %v", err)
	}
	// 3 silent GitHub retries for the single zip before falling back to the hub.
	if got := ghHits.Load(); got != 3 {
		t.Fatalf("GitHub attempts = %d, want 3", got)
	}
	assertOCRModelsExtracted(t, dir)
}

func TestDownloadOCRModelBothSourcesFailLeavesNoFiles(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	gh := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	hub := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	withOCRModelsZipURL(t, gh.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, RemoteHubURL: hub.URL})
	err := app.DownloadOCRModel()
	if err == nil {
		t.Fatal("DownloadOCRModel succeeded with both sources failing")
	}
	assertNoONNXFiles(t, dir)
}

func TestDownloadOCRModelNoHubConfigured(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	gh := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	withOCRModelsZipURL(t, gh.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	if err := app.DownloadOCRModel(); err == nil {
		t.Fatal("DownloadOCRModel succeeded with GitHub down and no hub URL")
	}
}

func TestDownloadOCRModelCorruptZipLeavesNoFiles(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	// Server answers 200 with an HTML error page — not a valid zip.
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>502 Bad Gateway</body></html>"))
	})
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	err := app.DownloadOCRModel()
	if err == nil || !strings.Contains(err.Error(), "open OCR models zip") {
		t.Fatalf("DownloadOCRModel error = %v, want zip open failure", err)
	}
	assertNoONNXFiles(t, dir)
	// The corrupt zip must be deleted so the next attempt re-downloads.
	if _, statErr := os.Stat(filepath.Join(dir, ocr.ModelsZipFilename)); !os.IsNotExist(statErr) {
		t.Fatalf("corrupt zip not deleted (stat err = %v)", statErr)
	}
}

func TestDownloadOCRModelInvalidPayloadInZipRejectedAndDeleted(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	// A well-formed zip whose .onnx payloads fail ModelFileStatus (e.g. HTML).
	srv := serveOCRZip(t, buildOCRModelsZip(t, func(name string) []byte {
		return []byte("<html>not an onnx</html>")
	}))
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	err := app.DownloadOCRModel()
	if err == nil || !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("DownloadOCRModel error = %v, want validation failure", err)
	}
	assertNoONNXFiles(t, dir)
}

func TestDownloadOCRModelConcurrentCallersDeduped(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	release := make(chan struct{})
	var hits atomic.Int32
	payload := validOCRModelsZip(t)
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release // hold the first download so other callers overlap it
		_, _ = w.Write(payload)
	})
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	const callers = 4
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { errs <- app.DownloadOCRModel() }()
	}
	// Wait for the winning caller's request to arrive, give the losers a
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
	// Exactly one caller did the work: 1 zip request, no duplicates.
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hits = %d, want 1 (dedup via TryLock failed)", got)
	}
}

func TestDownloadOCRModelReturnsNilWhenLockHeld(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	var hits atomic.Int32
	payload := validOCRModelsZip(t)
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	})
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

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

func TestExtractOCRModelsZipRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../evil.onnx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(fakeONNXPayload("evil")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, ocr.ModelsZipFilename)
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	extractDir := filepath.Join(dir, "models")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err = extractOCRModelsZip(zipPath, extractDir)
	if err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("extractOCRModelsZip error = %v, want zip-slip rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "evil.onnx")); !os.IsNotExist(statErr) {
		t.Fatalf("zip-slip file escaped the models dir (stat err = %v)", statErr)
	}
}

// TestExtractOCRModelsZipRealBundle runs the extractor against the real
// release zip when it is available locally (end-to-end sanity check).
func TestExtractOCRModelsZipRealBundle(t *testing.T) {
	zipPath := os.Getenv("OCR_MODELS_ZIP_PATH")
	if zipPath == "" {
		zipPath = filepath.Join("..", ".tmp", "ocr-models", "ocr-models.zip")
	}
	if _, err := os.Stat(zipPath); err != nil {
		t.Skipf("real OCR models zip not available at %s", zipPath)
	}

	dir := t.TempDir()
	if err := extractOCRModelsZip(zipPath, dir); err != nil {
		t.Fatalf("extractOCRModelsZip(real bundle): %v", err)
	}
	for _, name := range expectedOCRModelFilenames() {
		size, ok := ocr.ModelFileStatus(filepath.Join(dir, name))
		if !ok {
			t.Fatalf("real bundle: %s missing/invalid after extraction", name)
		}
		if size < 1<<20 {
			t.Fatalf("real bundle: %s suspiciously small (%d bytes)", name, size)
		}
	}
	// Non-.onnx bundle entries must not be extracted.
	if _, err := os.Stat(filepath.Join(dir, "dict_ppocrv6_tiny.txt")); !os.IsNotExist(err) {
		t.Fatalf("non-onnx bundle entry was extracted (stat err = %v)", err)
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

func TestSetOCREnabledPersists(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)
	// Seed valid fake models so enabling does not kick a background download.
	tier := corelib.DefaultOCRModelTier
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{ocr.DetModelFilename(tier), ocr.RecModelFilename(tier)} {
		if err := os.WriteFile(filepath.Join(dir, name), fakeONNXPayload(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{}
	if err := app.SetOCREnabled(false); err != nil {
		t.Fatalf("SetOCREnabled(false) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCREnabled {
		t.Fatal("ocr_enabled=false not persisted")
	}
	if err := app.SetOCREnabled(true); err != nil {
		t.Fatalf("SetOCREnabled(true) error = %v", err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OCREnabled {
		t.Fatal("ocr_enabled=true not persisted")
	}
}

func TestPatchConfigFieldsOCRKeys(t *testing.T) {
	withOCRTestHome(t)
	app := &App{}
	cfg, err := app.PatchConfigFields(map[string]interface{}{
		"ocr_enabled":    false,
		"ocr_model_tier": "medium",
	})
	if err != nil {
		t.Fatalf("PatchConfigFields ocr keys error = %v", err)
	}
	if cfg.OCREnabled {
		t.Fatal("ocr_enabled=false not applied")
	}
	if cfg.OCRModelTier != "medium" {
		t.Fatalf("ocr_model_tier = %q, want medium", cfg.OCRModelTier)
	}
	cfg, err = app.PatchConfigFields(map[string]interface{}{"ocr_model_tier": "huge"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCRModelTier != corelib.DefaultOCRModelTier {
		t.Fatalf("junk tier normalized to %q, want %q", cfg.OCRModelTier, corelib.DefaultOCRModelTier)
	}
}

func TestSetOCRModelTierPersistsAndNormalizes(t *testing.T) {
	withOCRTestHome(t)
	withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	// OCR disabled: no background download is kicked on tier switch.
	app := &App{}
	if _, err := app.PatchConfigFields(map[string]interface{}{"ocr_enabled": false}); err != nil {
		t.Fatal(err)
	}

	if err := app.SetOCRModelTier("medium"); err != nil {
		t.Fatalf("SetOCRModelTier(medium) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCRModelTier != "medium" {
		t.Fatalf("ocr_model_tier = %q, want medium", cfg.OCRModelTier)
	}

	if err := app.SetOCRModelTier("huge"); err != nil {
		t.Fatalf("SetOCRModelTier(huge) error = %v", err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCRModelTier != corelib.DefaultOCRModelTier {
		t.Fatalf("junk tier persisted as %q, want normalized %q", cfg.OCRModelTier, corelib.DefaultOCRModelTier)
	}
}

// TestSetOCRModelTierInstantAfterZipExtraction verifies the zip flow's key
// property: because one download extracts every tier, switching tiers
// afterwards finds the files already present and kicks no new download.
func TestSetOCRModelTierInstantAfterZipExtraction(t *testing.T) {
	withOCRTestHome(t)
	dir := withOCRTestModelsDir(t)
	isolateSharedOCRProvider(t)

	var hits atomic.Int32
	srv := ocrModelServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(validOCRModelsZip(t))
	})
	withOCRModelsZipURL(t, srv.URL+"/"+ocr.ModelsZipFilename)

	// Fresh test home: defaults have ocr_enabled=true and the default tier
	// (small). Download once — the zip extracts all tiers.
	app := &App{}
	if err := app.DownloadOCRModel(); err != nil {
		t.Fatalf("DownloadOCRModel: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("server hits = %d, want 1", got)
	}

	if err := app.SetOCRModelTier("tiny"); err != nil {
		t.Fatalf("SetOCRModelTier(tiny) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OCRModelTier != "tiny" {
		t.Fatalf("ocr_model_tier = %q, want tiny", cfg.OCRModelTier)
	}
	// The tiny tier's files were extracted by the same zip download.
	_, detOK := ocr.ModelFileStatus(filepath.Join(dir, ocr.DetModelFilename("tiny")))
	_, recOK := ocr.ModelFileStatus(filepath.Join(dir, ocr.RecModelFilename("tiny")))
	if !detOK || !recOK {
		t.Fatal("tiny tier files missing after zip extraction")
	}
	if got := app.CheckOCRModel(); got["exists"] != true || got["tier"] != "tiny" {
		t.Fatalf("CheckOCRModel after tier switch = %#v", got)
	}
	// Give any (unexpectedly) kicked background download a moment to run.
	time.Sleep(200 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("tier switch kicked a new download: server hits = %d, want 1", got)
	}
}
