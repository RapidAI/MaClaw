package guiapp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/formula"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

func withFormulaModelDir(t *testing.T) {
	t.Helper()
	prev := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(prev) })
}

func withFormulaReleaseDown(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	prev := formula.DownloadReleaseBase
	formula.DownloadReleaseBase = srv.URL
	t.Cleanup(func() { formula.DownloadReleaseBase = prev })
}

func resetSharedFormula(t *testing.T) {
	t.Helper()
	sharedFormula.mu.Lock()
	sharedFormula.eng = nil
	sharedFormula.dir = ""
	sharedFormula.mu.Unlock()
	t.Cleanup(func() {
		sharedFormula.mu.Lock()
		sharedFormula.eng = nil
		sharedFormula.dir = ""
		sharedFormula.mu.Unlock()
	})
}

func TestCheckFormulaModelMissing(t *testing.T) {
	withFormulaModelDir(t)
	app := newOCRTestApp(corelib.AppConfig{FormulaEnabled: true})
	got := app.CheckFormulaModel()
	if got["exists"] != false || got["size"] != int64(0) {
		t.Fatalf("CheckFormulaModel = %#v", got)
	}
}

func TestSetFormulaEnabledPersists(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	withFormulaReleaseDown(t)
	resetSharedFormula(t)

	app := &App{}
	if err := app.SetFormulaEnabled(false); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FormulaEnabled {
		t.Fatal("formula_enabled=false not persisted")
	}
	if err := app.SetFormulaEnabled(true); err != nil {
		t.Fatal(err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.FormulaEnabled {
		t.Fatal("formula_enabled=true not persisted")
	}
}

func TestOCRRecognizeFormulaDisabled(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	resetSharedFormula(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, FormulaEnabled: false})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{"mode": "formula", "image_path": "formula.png"})
	if !strings.Contains(out, "formula_enabled=false") {
		t.Fatalf("disabled formula message = %q", out)
	}
}

func TestOCRRecognizeFormulaModelMissing(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	withFormulaReleaseDown(t)
	resetSharedFormula(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, FormulaEnabled: true})
	tool := ocrToolForTest(t, app)
	imgPath := filepath.Join("..", "corelib", "ocr", "testdata", "en.png")
	out := tool.Handler(map[string]interface{}{"mode": "formula", "image_path": imgPath})
	if !strings.Contains(out, "not present yet") {
		t.Fatalf("missing formula message = %q", out)
	}
}

func TestOCRRecognizeFormulaIgnoresTextOCRToggle(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	withFormulaReleaseDown(t)
	resetSharedFormula(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: false, FormulaEnabled: true})
	tool := ocrToolForTest(t, app)
	imgPath := filepath.Join("..", "corelib", "ocr", "testdata", "en.png")
	out := tool.Handler(map[string]interface{}{"mode": "formula", "image_path": imgPath})
	if strings.Contains(out, "ocr_enabled=false") || !strings.Contains(out, "not present yet") {
		t.Fatalf("formula with text OCR off = %q", out)
	}
}

func TestOCRRecognizeFormulaRejectsPDFOutput(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	resetSharedFormula(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, FormulaEnabled: true})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{
		"mode":        "formula",
		"image_path":  "formula.png",
		"output_path": "formula.pdf",
	})
	if !strings.Contains(out, "output_path 要写成 .txt") {
		t.Fatalf("pdf output message = %q", out)
	}
}

func TestOCRRecognizeFormulaRejectsPDF(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	resetSharedFormula(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true, FormulaEnabled: true})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{"mode": "latex", "image_path": "scan.pdf"})
	if !strings.Contains(out, "不接受 PDF") {
		t.Fatalf("pdf formula message = %q", out)
	}
}

func TestFormulaReadyUsesCacheAndRefetchesMissingFiles(t *testing.T) {
	withOCRTestHome(t)
	withFormulaModelDir(t)
	resetSharedFormula(t)
	t.Setenv("MACLAW_HUB_URL", "")
	prevBackoff := formula.DownloadBackoff
	formula.DownloadBackoff = 0
	t.Cleanup(func() { formula.DownloadBackoff = prevBackoff })

	dir, err := formula.DefaultModelDir()
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	prev := formula.DownloadReleaseBase
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(func() {
		srv.Close()
		formula.DownloadReleaseBase = prev
	})
	formula.DownloadReleaseBase = srv.URL

	planted := &formula.Engine{}
	sharedFormula.mu.Lock()
	sharedFormula.eng = planted
	sharedFormula.dir = dir
	sharedFormula.mu.Unlock()

	app := newOCRTestApp(corelib.AppConfig{FormulaEnabled: true})
	eng, err := app.ensureFormulaReady()
	if err != nil || eng != planted {
		t.Fatalf("ready eng=%v err=%v", eng, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hits.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("cached engine suppressed the refetch")
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(5 * time.Second)
	for !formulaDownloadMu.TryLock() {
		if time.Now().After(deadline) {
			t.Fatal("refetch did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	formulaDownloadMu.Unlock()
}

func TestDownloadFormulaDisabledAfterReadyDoesNotOpen(t *testing.T) {
	withFormulaModelDir(t)
	resetSharedFormula(t)
	dir, err := formula.DefaultModelDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	plantVerifiedFormulaStubs(t, dir)

	var calls int
	var gotPct int
	var gotErr string
	prev := formulaProgressSink
	formulaProgressSink = func(pct int, _ int64, _ int64, errMsg string) {
		calls++
		gotPct = pct
		gotErr = errMsg
	}
	t.Cleanup(func() { formulaProgressSink = prev })

	app := newOCRTestApp(corelib.AppConfig{FormulaEnabled: false})
	if err := app.downloadFormulaModel(false); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotPct != 100 || gotErr != "" {
		t.Fatalf("progress calls=%d pct=%d err=%q", calls, gotPct, gotErr)
	}
	if sharedFormula.current(dir) != nil {
		t.Fatal("disabled formula loaded the engine")
	}
}

func plantVerifiedFormulaStubs(t *testing.T, dir string) {
	t.Helper()
	// Sizes and digests match corelib/formula modelAssets. The marker skips
	// the hash, so the bodies stay sparse.
	stubs := []struct {
		name string
		size int64
		sha  string
	}{
		{"formulanet-encoder.onnx", 54168561, "fbd69cf63cf833db1e2ef40013d859b560671c1253278441a01bde4516b624ae"},
		{"formulanet-decoder.onnx", 25872372, "00f07add62d74c78750cfda60ce8f1cff3d1774f913ca3f412e2e72f4189d7d4"},
		{"formulanet-decoder-past.onnx", 20337816, "cf6838e89f96e33cd81ec8eb885bf78953506e340ff0485aaea13a784af2e804"},
		{"formulanet-tokenizer.json", 17318, "1240f9d178e1ad2a0076fe95ba62e332871c702accdd5ce3ae3ef33ffd6c3a1e"},
	}
	for _, a := range stubs {
		path := filepath.Join(dir, a.name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(a.size); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		marker := fmt.Sprintf("%d %d %s\n", fi.Size(), fi.ModTime().UnixNano(), a.sha)
		if err := os.WriteFile(path+".verified", []byte(marker), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := formula.VerifyLoaded(dir); err != nil {
		t.Fatalf("stubs: %v", err)
	}
}

func TestOCRRecognizeRejectsUnknownMode(t *testing.T) {
	withOCRTestHome(t)
	app := newOCRTestApp(corelib.AppConfig{OCREnabled: true})
	tool := ocrToolForTest(t, app)
	out := tool.Handler(map[string]interface{}{"mode": "cloud", "image_path": "a.png"})
	if !strings.Contains(out, "mode must be text or formula") {
		t.Fatalf("bad mode message = %q", out)
	}
}
