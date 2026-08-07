package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
)

func TestGetComputerUseLastWarmupEmpty(t *testing.T) {
	// Isolate package globals for this test process.
	cuWarmupMu.Lock()
	prev := cuLastCheck
	cuLastCheck = nil
	cuWarmupMu.Unlock()
	t.Cleanup(func() {
		cuWarmupMu.Lock()
		cuLastCheck = prev
		cuWarmupMu.Unlock()
	})

	a := &App{}
	out := a.GetComputerUseLastWarmup()
	if out == nil {
		t.Fatal("expected non-nil map")
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %#v", out)
	}
}

func TestRunComputerUseWarmupStoresSnapshot(t *testing.T) {
	a := &App{}
	// Ensure CU toggle does not skip (default config usually enables; force via self_check).
	a.runComputerUseWarmup("self_check")

	out := a.GetComputerUseLastWarmup()
	if out == nil || len(out) == 0 {
		t.Fatal("expected warmup snapshot")
	}
	if reason, _ := out["reason"].(string); reason != "self_check" {
		t.Fatalf("reason=%v", out["reason"])
	}
	if plat, _ := out["platform"].(string); plat != runtime.GOOS {
		t.Fatalf("platform=%v want %s", out["platform"], runtime.GOOS)
	}
	if _, ok := out["ok"].(bool); !ok {
		t.Fatalf("missing ok: %#v", out)
	}
	if _, ok := out["uia"].(map[string]interface{}); !ok {
		t.Fatalf("missing uia map: %#v", out)
	}
	if _, ok := out["input"].(map[string]interface{}); !ok {
		t.Fatalf("missing input map: %#v", out)
	}
	t.Logf("warmup snapshot: ok=%v ms=%v uia=%v yolo=%v",
		out["ok"], out["ms"], out["uia"], out["yolo"])
}

func TestComputerUseSelfCheckReportShape(t *testing.T) {
	a := &App{}
	rep := a.ComputerUseSelfCheck()
	if rep == nil {
		t.Fatal("nil report")
	}
	for _, key := range []string{"at", "enabled", "platform", "uia", "ok", "ms", "permissions"} {
		if _, ok := rep[key]; !ok {
			t.Errorf("missing key %q in %#v", key, rep)
		}
	}
	if plat, _ := rep["platform"].(string); plat != runtime.GOOS {
		t.Fatalf("platform=%v", rep["platform"])
	}
	// warmup nested after self_check path
	if warm, ok := rep["warmup"].(map[string]interface{}); ok {
		if _, has := warm["ok"]; !has {
			t.Errorf("warmup missing ok: %#v", warm)
		}
		if yolo, ok := warm["yolo"].(map[string]interface{}); ok {
			t.Logf("yolo snapshot: %#v", yolo)
		}
		if perms, ok := warm["permissions"].(map[string]interface{}); ok {
			t.Logf("warmup permissions: %#v", perms)
		}
	}
	if _, ok := rep["warnings"]; !ok {
		t.Error("missing warnings slice")
	}
	if _, ok := rep["readiness"]; !ok {
		t.Error("missing readiness")
	}
	t.Logf("selfcheck ok=%v ms=%v warnings=%v uia=%v readiness=%v", rep["ok"], rep["ms"], rep["warnings"], rep["uia"], rep["readiness"])
}

func TestCuScreenshotFailureGuidance(t *testing.T) {
	g, a := cuScreenshotFailureGuidance(fmt.Errorf("screen recording permission not granted"))
	if g == "" {
		t.Fatal("expected guidance")
	}
	if runtime.GOOS == "darwin" && a != "open_screen_recording" {
		t.Fatalf("action=%q", a)
	}
	g2, _ := cuScreenshotFailureGuidance(fmt.Errorf("no graphical display: headless"))
	if g2 == "" {
		t.Fatal("expected display guidance")
	}
}

func TestRecordAndClearComputerUseError(t *testing.T) {
	clearComputerUseError()
	a := &App{}
	if len(a.GetComputerUseLastError()) != 0 {
		t.Fatal("expected empty")
	}
	recordComputerUseError("screenshot", "boom", "fix it", "open_screen_recording")
	last := a.GetComputerUseLastError()
	if last["error"] != "boom" || last["stage"] != "screenshot" {
		t.Fatalf("%#v", last)
	}
	clearComputerUseError()
	if len(a.GetComputerUseLastError()) != 0 {
		t.Fatal("expected cleared")
	}
}

func TestComputerUseSmokeCheckShape(t *testing.T) {
	a := &App{}
	// May fail in CI without display; still must return structured map.
	out := a.ComputerUseSmokeCheck()
	if out == nil {
		t.Fatal("nil")
	}
	for _, key := range []string{"ok", "screenshot_ok", "ms", "timing_ms"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing %q in %#v", key, out)
		}
	}
	// Metrics cache should be populated after smoke (ok or fail).
	m := a.GetComputerUseLastObserveMetrics()
	if len(m) == 0 {
		t.Fatal("expected last observe metrics after smoke")
	}
	if _, ok := m["timing_ms"]; !ok {
		t.Fatalf("metrics missing timing_ms: %#v", m)
	}
	st := a.GetComputerUseStatus()
	if st["last_observe"] == nil {
		t.Fatalf("status missing last_observe: %#v", st)
	}
	t.Logf("smoke: %#v metrics=%#v", out, m)
}

func TestStoreComputerUseLastObserveMetricsSlim(t *testing.T) {
	// Reset history for isolation.
	cuLastObserveMu.Lock()
	cuObserveHistory = nil
	cuLastObserveMetrics = nil
	cuLastObserveMu.Unlock()

	storeComputerUseLastObserveMetrics(map[string]interface{}{
		"ok":            true,
		"element_count": 3,
		"timing_ms":     map[string]int64{"screenshot": 10, "total": 40},
		"total_ms":      int64(40),
		"elements":      []map[string]interface{}{{"ref": "e0"}}, // heavy — should be dropped
		"text_preview":  "secret-ish",
	})
	a := &App{}
	m := a.GetComputerUseLastObserveMetrics()
	if m["element_count"] != 3 {
		t.Fatalf("%#v", m)
	}
	if _, ok := m["elements"]; ok {
		t.Fatal("elements should not be stored in slim metrics")
	}
	if _, ok := m["text_preview"]; ok {
		t.Fatal("text_preview should not be stored")
	}
	hist := a.GetComputerUseObserveHistory()
	sum, _ := hist["summary"].(map[string]interface{})
	if sum == nil || sum["count"] != 1 {
		t.Fatalf("history summary: %#v", hist)
	}
	if sum["avg_total_ms"] != int64(40) && sum["avg_total_ms"] != 40 {
		// accept int or int64
		if cuAnyToInt64(sum["avg_total_ms"]) != 40 {
			t.Fatalf("avg=%v", sum["avg_total_ms"])
		}
	}
}

func TestObserveHistoryRollingCap(t *testing.T) {
	cuLastObserveMu.Lock()
	cuObserveHistory = nil
	cuLastObserveMetrics = nil
	cuLastObserveMu.Unlock()

	for i := 0; i < cuObserveHistoryMax+5; i++ {
		storeComputerUseLastObserveMetrics(map[string]interface{}{
			"ok":       true,
			"total_ms": int64(100 + i),
			"stage":    "test",
		})
	}
	a := &App{}
	hist := a.GetComputerUseObserveHistory()
	items, _ := hist["items"].([]map[string]interface{})
	if len(items) != cuObserveHistoryMax {
		t.Fatalf("len=%d want %d", len(items), cuObserveHistoryMax)
	}
	sum, _ := hist["summary"].(map[string]interface{})
	if cuAnyToInt64(sum["count"]) != int64(cuObserveHistoryMax) {
		t.Fatalf("count=%v", sum["count"])
	}
}

func TestExportComputerUseDiagnosticsWritesFile(t *testing.T) {
	a := &App{}
	// Seed last E2E so diagnostics include last_e2e field.
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": true, "interact": false, "ms": int64(12), "at": "test",
	})
	// No Wails ctx → writes under MaclawBaseDir/logs
	out := a.ExportComputerUseDiagnostics()
	if out == nil {
		t.Fatal("nil")
	}
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("export failed: %#v", out)
	}
	path, _ := out["path"].(string)
	if path == "" {
		t.Fatal("empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 20 || data[0] != '{' {
		t.Fatalf("unexpected file content len=%d", len(data))
	}
	if !strings.Contains(string(data), "last_e2e") {
		t.Fatal("diagnostics JSON missing last_e2e")
	}
	sum, _ := out["summary"].(map[string]interface{})
	if sum == nil || sum["last_e2e_ok"] != true {
		t.Fatalf("summary last_e2e: %#v", sum)
	}
	t.Logf("exported %s (%d bytes)", path, len(data))
	_ = os.Remove(path)
}

func TestBuildComputerUseDiagnosticsLight(t *testing.T) {
	a := &App{}
	// Seed warmup snapshot used by light path.
	cuWarmupMu.Lock()
	cuLastCheck = map[string]interface{}{
		"permissions": map[string]interface{}{"ok": true, "platform": "test"},
		"uia":         map[string]interface{}{"ok": true, "backend": "csharp"},
	}
	cuWarmupMu.Unlock()
	diag := a.buildComputerUseDiagnostics(true)
	if diag["light"] != true {
		t.Fatalf("light flag: %#v", diag["light"])
	}
	// Light must not force readiness (can re-probe).
	if _, has := diag["readiness"]; has {
		t.Fatal("light diag should skip readiness")
	}
	if perms, _ := diag["permissions"].(map[string]interface{}); perms["ok"] != true {
		t.Fatalf("permissions: %#v", diag["permissions"])
	}
	// Silent export path should succeed with light build.
	out := a.exportComputerUseDiagnostics(true, false)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("%#v", out)
	}
	if p, _ := out["path"].(string); p != "" {
		_ = os.Remove(p)
	}
}

func TestExportComputerUseObserveHistoryCSV(t *testing.T) {
	cuLastObserveMu.Lock()
	cuObserveHistory = nil
	cuLastObserveMetrics = nil
	cuLastObserveMu.Unlock()
	storeComputerUseLastObserveMetrics(map[string]interface{}{
		"ok": true, "total_ms": int64(42), "stage": "smoke",
		"timing_ms":     map[string]int64{"screenshot": 10, "a11y": 5, "total": 42},
		"element_count": 2,
	})
	a := &App{}
	out := a.ExportComputerUseObserveHistoryCSV()
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("%#v", out)
	}
	path, _ := out["path"].(string)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "total_ms") || !strings.Contains(text, "42") {
		t.Fatalf("csv content: %s", text)
	}
	if !strings.Contains(text, "# summary") || !strings.Contains(text, "SUMMARY") {
		t.Fatalf("missing summary footer: %s", text)
	}
	if out["rows"] != 1 && cuAnyToInt64(out["rows"]) != 1 {
		t.Fatalf("rows=%v", out["rows"])
	}
	_ = os.Remove(path)
}

func TestGetComputerUseReadinessIncludesLastE2E(t *testing.T) {
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "error": "baseline screenshot failed",
		"at":               time.Now().Format(time.RFC3339),
		"diagnostics_path": "/tmp/fake-diag.json",
	})
	a := &App{}
	r := a.GetComputerUseReadiness()
	// CU may be enabled by default; look for last_e2e_failed in issues.
	found := false
	issues, _ := r["issues"].([]map[string]interface{})
	for _, iss := range issues {
		if iss["id"] == "last_e2e_failed" {
			found = true
			if !strings.Contains(fmt.Sprint(iss["message"]), "diag") &&
				!strings.Contains(fmt.Sprint(iss["message"]), "baseline") {
				t.Fatalf("message: %#v", iss["message"])
			}
		}
	}
	if !found && r["enabled"] == true {
		// enabled defaults true when config missing
		t.Fatalf("expected last_e2e_failed issue: %#v", r["issues"])
	}
}

func TestGetComputerUseReadinessSoftFailInteract(t *testing.T) {
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "interact": true, "soft_fail": true,
		"skip_reason": "focus_failed",
		"error":       "interact: could not focus target window after retries",
		"at":          time.Now().Format(time.RFC3339),
	})
	a := &App{}
	// Force enabled path: readiness returns early when disabled.
	// Default config often enables CU; if disabled, skip assertion.
	r := a.GetComputerUseReadiness()
	if r["enabled"] != true {
		t.Skip("computer use disabled in config")
	}
	found := false
	issues, _ := r["issues"].([]map[string]interface{})
	for _, iss := range issues {
		if iss["id"] == "last_e2e_soft_fail" {
			found = true
			if iss["action"] != "run_e2e_interact" {
				t.Fatalf("action want run_e2e_interact: %#v", iss)
			}
			if !strings.Contains(fmt.Sprint(iss["message"]), "focus") {
				t.Fatalf("message: %#v", iss["message"])
			}
		}
	}
	if !found {
		t.Fatalf("expected last_e2e_soft_fail: %#v", r["issues"])
	}
	le, _ := r["last_e2e"].(map[string]interface{})
	if le["soft_fail"] != true || le["skip_reason"] != "focus_failed" {
		t.Fatalf("last_e2e: %#v", le)
	}
}

func TestCuE2EAttachDiagnosticsSetsPath(t *testing.T) {
	a := &App{}
	rep := map[string]interface{}{
		"ok": false, "error": "test fail", "at": time.Now().Format(time.RFC3339),
	}
	cuE2EAttachDiagnostics(a, rep)
	path, _ := rep["diagnostics_path"].(string)
	if path == "" {
		t.Fatalf("expected diagnostics_path: %#v", rep)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	csvPath, _ := rep["history_csv_path"].(string)
	if csvPath == "" {
		t.Fatalf("expected history_csv_path: %#v", rep)
	}
	if _, err := os.Stat(csvPath); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	_ = os.Remove(csvPath)
	last := a.GetComputerUseLastE2E()
	if last["ok"] != false {
		t.Fatalf("last e2e: %#v", last)
	}
}

func TestComputerUseSelfCheckIncludesLastE2EPaths(t *testing.T) {
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "error": "x", "at": time.Now().Format(time.RFC3339),
		"diagnostics_path": "/tmp/diag.json",
		"history_csv_path": "/tmp/hist.csv",
		"interact":         true,
		"ms":               int64(9),
	})
	a := &App{}
	rep := a.ComputerUseSelfCheck()
	if rep["last_e2e"] == nil {
		t.Fatalf("missing last_e2e: %#v", rep)
	}
	if rep["diagnostics_path"] != "/tmp/diag.json" {
		t.Fatalf("diagnostics_path=%v", rep["diagnostics_path"])
	}
	if rep["history_csv_path"] != "/tmp/hist.csv" {
		t.Fatalf("history_csv_path=%v", rep["history_csv_path"])
	}
	if rep["logs_dir"] == nil || rep["logs_dir"] == "" {
		t.Fatalf("logs_dir missing: %#v", rep["logs_dir"])
	}
}

func TestOpenComputerUseLogsFolderShape(t *testing.T) {
	a := &App{}
	// May fail to open UI in headless CI; path must still resolve.
	out := a.OpenComputerUseLogsFolder()
	if out["path"] == nil || out["path"] == "" {
		t.Fatalf("%#v", out)
	}
	if _, err := os.Stat(fmt.Sprint(out["path"])); err != nil {
		t.Fatal(err)
	}
	t.Logf("logs folder: %#v", out)
}

func TestListAndPruneComputerUseLogArtifacts(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	// Seed a few fake artifacts.
	p1 := filepath.Join(dir, "computer-use-diag-test-old.json")
	p2 := filepath.Join(dir, "computer-use-history-test-old.csv")
	_ = os.WriteFile(p1, []byte(`{"ok":false}`), 0o600)
	_ = os.WriteFile(p2, []byte("at,ok\n"), 0o600)
	// Make them old
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(p1, old, old)
	_ = os.Chtimes(p2, old, old)
	t.Cleanup(func() {
		_ = os.Remove(p1)
		_ = os.Remove(p2)
	})

	a := &App{}
	list := a.ListComputerUseLogArtifacts("all", 50)
	if ok, _ := list["ok"].(bool); !ok {
		t.Fatalf("%#v", list)
	}
	if cuAnyToInt64(list["count"]) < 1 {
		t.Fatalf("expected items: %#v", list)
	}

	// keepNewest=0 becomes 10; use maxAgeDays=1 to drop 48h-old files.
	pr := a.PruneComputerUseLogArtifacts(50, 1)
	if ok, _ := pr["ok"].(bool); !ok {
		t.Fatalf("%#v", pr)
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Fatalf("expected old diag deleted, err=%v", err)
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Fatalf("expected old csv deleted, err=%v", err)
	}
	t.Logf("prune: deleted_n=%v freed=%v scanned=%v", pr["deleted_n"], pr["freed_bytes"], pr["scanned"])
}

func TestPruneDeletesBeyondUIListCap(t *testing.T) {
	// Ensure prune does not only look at the newest 200 listed files.
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	const n = 25
	paths := make([]string, 0, n)
	base := time.Now().Add(-72 * time.Hour)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, fmt.Sprintf("computer-use-diag-prune-cap-%03d.json", i))
		if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		// Older index = older mtime so they sort after newest when keep=5.
		mt := base.Add(time.Duration(i) * time.Minute)
		_ = os.Chtimes(p, mt, mt)
		paths = append(paths, p)
	}
	t.Cleanup(func() {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	})
	a := &App{}
	// Keep 5 newest of this kind among ALL diags; age unlimited.
	pr := a.PruneComputerUseLogArtifacts(5, 0)
	if ok, _ := pr["ok"].(bool); !ok {
		t.Fatalf("%#v", pr)
	}
	// At least the oldest of our batch (index 0) should be gone if we had >=5 newer
	// files overall; assert scanned >= n and some deletion occurred.
	if cuAnyToInt64(pr["scanned"]) < int64(n) {
		// Other CU files may exist; scanned should still include all on disk.
		t.Fatalf("scanned=%v want >= %d (prune must not cap list)", pr["scanned"], n)
	}
	// Oldest seeded file must not remain among keep=5 of its kind if we only had our files,
	// but other diags may exist. Safer: verify unlimited scan via listComputerUseLogFileItems.
	all, _, err := listComputerUseLogFileItems("diag", 0)
	if err != nil {
		t.Fatal(err)
	}
	ui, _, err := listComputerUseLogFileItems("diag", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < len(ui) {
		t.Fatalf("unlimited list smaller than limited: all=%d ui=%d", len(all), len(ui))
	}
	// After prune keep=5, at most 5 of our batch might remain if they were the newest overall;
	// count remaining of our prefix.
	remain := 0
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			remain++
		}
	}
	if remain > 5 {
		t.Fatalf("kept %d of seeded diag files, want <=5 after keep=5 prune", remain)
	}
	t.Logf("prune-cap: remain=%d deleted_n=%v scanned=%v", remain, pr["deleted_n"], pr["scanned"])
}

func TestRegisterComputerUseToolsReusesWarmYOLO(t *testing.T) {
	// Simulate warmup installing a YOLO parser, then register should keep it.
	prevYolo := globalComputerUse.yolo
	prevOCR := globalComputerUse.ocrSidecar
	t.Cleanup(func() {
		globalComputerUse.mu.Lock()
		globalComputerUse.yolo = prevYolo
		globalComputerUse.ocrSidecar = prevOCR
		globalComputerUse.mu.Unlock()
	})
	// Only meaningful when weights exist.
	p := findYOLOWeights()
	if p == "" {
		t.Skip("no YOLO weights")
	}
	warm := guiautomation.NewYOLOScreenParser(p, 0.3, 0.5)
	warm.SetUnloadDelay(15 * time.Minute)
	globalComputerUse.mu.Lock()
	globalComputerUse.yolo = warm
	globalComputerUse.ocrSidecar = browser.NewNativeOCRProvider("", "", nil)
	globalComputerUse.mu.Unlock()

	reg := NewToolRegistry()
	registerComputerUseTools(reg)
	globalComputerUse.mu.Lock()
	got := globalComputerUse.yolo
	globalComputerUse.mu.Unlock()
	if got != warm {
		t.Fatal("registerComputerUseTools replaced warmed YOLO instance")
	}
}

func TestCopyComputerUsePathLogs(t *testing.T) {
	a := &App{}
	out := a.CopyComputerUsePath("logs")
	// Clipboard may fail in some CI; path must be set when ok or error is clipboard-related.
	if out["path"] == nil || out["path"] == "" {
		t.Fatalf("%#v", out)
	}
	if _, err := os.Stat(fmt.Sprint(out["path"])); err != nil {
		t.Fatal(err)
	}
	t.Logf("copy logs: %#v", out)
}

func TestComputerUseLogPrunePolicyDefaults(t *testing.T) {
	a := &App{}
	pol := a.GetComputerUseLogPrunePolicy()
	if cuAnyToInt64(pol["keep_newest"]) <= 0 {
		t.Fatalf("%#v", pol)
	}
	// max age may be 0
	t.Logf("policy: %#v", pol)
}

func TestOpenComputerUseLogArtifactRejectsOutside(t *testing.T) {
	a := &App{}
	out := a.OpenComputerUseLogArtifact("C:\\Windows\\System32\\drivers\\etc\\hosts")
	if ok, _ := out["ok"].(bool); ok {
		t.Fatalf("should reject outside path: %#v", out)
	}
	// Basename not matching artifact pattern
	out2 := a.OpenComputerUseLogArtifact("readme.txt")
	if ok, _ := out2["ok"].(bool); ok {
		t.Fatalf("should reject non-artifact: %#v", out2)
	}
}

func TestOpenComputerUseLogArtifactAllowsUnderLogs(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "computer-use-diag-open-test.json")
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })
	a := &App{}
	out := a.OpenComputerUseLogArtifact(p)
	// Opening may fail without desktop shell; path validation should pass.
	if out["path"] != p && filepath.Base(fmt.Sprint(out["path"])) != filepath.Base(p) {
		// If ok=false due to open, path should still be set after validation
		if out["error"] != nil && strings.Contains(fmt.Sprint(out["error"]), "outside") {
			t.Fatalf("unexpected outside error: %#v", out)
		}
	}
	t.Logf("open artifact: %#v", out)
}

func TestCuIsComputerUseLogArtifactName(t *testing.T) {
	if !cuIsComputerUseLogArtifactName("computer-use-diag-x.json") {
		t.Fatal("diag")
	}
	if !cuIsComputerUseLogArtifactName("computer-use-history-x.csv") {
		t.Fatal("csv")
	}
	if cuIsComputerUseLogArtifactName("other.log") {
		t.Fatal("other")
	}
}

func TestDeleteComputerUseLogArtifact(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "computer-use-diag-delete-test.json")
	if err := os.WriteFile(p, []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "diagnostics_path": p, "at": time.Now().Format(time.RFC3339),
	})
	a := &App{}
	out := a.DeleteComputerUseLogArtifact(p)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("%#v", out)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("file should be gone: %v", err)
	}
	last := a.GetComputerUseLastE2E()
	if _, has := last["diagnostics_path"]; has {
		t.Fatalf("diagnostics_path should be cleared: %#v", last)
	}
	// Reject outside
	bad := a.DeleteComputerUseLogArtifact("C:\\Windows\\win.ini")
	if ok, _ := bad["ok"].(bool); ok {
		t.Fatalf("should reject: %#v", bad)
	}
}

func TestBatchDeleteComputerUseLogArtifacts(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(dir, "computer-use-diag-batch-1.json")
	p2 := filepath.Join(dir, "computer-use-history-batch-2.csv")
	if err := os.WriteFile(p1, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(p1)
		_ = os.Remove(p2)
	})
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "diagnostics_path": p1, "history_csv_path": p2,
		"at": time.Now().Format(time.RFC3339),
	})
	a := &App{}
	// Mix: valid basename, valid full path, invalid outside, empty.
	out := a.BatchDeleteComputerUseLogArtifacts([]string{
		filepath.Base(p1),
		p2,
		`C:\Windows\win.ini`,
		"",
		p1, // dedupe
	})
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("expected partial ok: %#v", out)
	}
	if n, _ := out["deleted_n"].(int); n < 2 {
		// deleted_n may be int or from JSON-like map as something else — check len(deleted)
		if del, _ := out["deleted"].([]string); len(del) < 2 {
			t.Fatalf("want >=2 deleted, got %#v", out)
		}
	}
	if _, err := os.Stat(p1); !os.IsNotExist(err) {
		t.Fatalf("p1 should be gone: %v", err)
	}
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Fatalf("p2 should be gone: %v", err)
	}
	if en, _ := out["error_n"].(int); en < 1 {
		if errs, _ := out["errors"].([]string); len(errs) < 1 {
			t.Fatalf("want outside path error: %#v", out)
		}
	}
	last := a.GetComputerUseLastE2E()
	if _, has := last["diagnostics_path"]; has {
		t.Fatalf("diagnostics_path should clear: %#v", last)
	}
	if _, has := last["history_csv_path"]; has {
		t.Fatalf("history_csv_path should clear: %#v", last)
	}
	// Empty call
	empty := a.BatchDeleteComputerUseLogArtifacts(nil)
	if ok, _ := empty["ok"].(bool); ok {
		t.Fatalf("empty should fail: %#v", empty)
	}
}

func TestCuSameComputerUseLogPath(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	name := "computer-use-diag-same-path-test.json"
	full := filepath.Join(dir, name)
	if !cuSameComputerUseLogPath(full, name) {
		t.Fatalf("basename should match full under logs: full=%q name=%q", full, name)
	}
	if !cuSameComputerUseLogPath(full, full) {
		t.Fatal("identical paths")
	}
	if cuSameComputerUseLogPath(full, filepath.Join(dir, "computer-use-diag-other.json")) {
		t.Fatal("different files must not match")
	}
	if cuSameComputerUseLogPath("", full) {
		t.Fatal("empty")
	}
}

func TestPruneClearsLastE2EPaths(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	// Create a single diag and mark it as last E2E artifact, then prune with keep=0→default 10
	// by using keep=1 after flooding... simpler: keepNewest=0 uses default 10; create 1 file
	// and prune with keepNewest=0 which becomes 10 — won't delete. Use keepNewest=1 with 2 files
	// and set e2e to the older one.
	oldP := filepath.Join(dir, "computer-use-diag-prune-e2e-old.json")
	newP := filepath.Join(dir, "computer-use-diag-prune-e2e-new.json")
	if err := os.WriteFile(oldP, []byte(`{"old":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure old is older for newest-first sort.
	oldTime := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(oldP, oldTime, oldTime)
	if err := os.WriteFile(newP, []byte(`{"new":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(oldP)
		_ = os.Remove(newP)
	})
	storeComputerUseLastE2E(map[string]interface{}{
		"ok": false, "diagnostics_path": oldP, "at": time.Now().Format(time.RFC3339),
	})
	a := &App{}
	// keep only 1 newest diag → old should be deleted and e2e path cleared.
	out := a.PruneComputerUseLogArtifacts(1, 0)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("%#v", out)
	}
	if _, err := os.Stat(oldP); !os.IsNotExist(err) {
		t.Fatalf("old should be pruned: %v", err)
	}
	last := a.GetComputerUseLastE2E()
	if p, has := last["diagnostics_path"]; has {
		t.Fatalf("diagnostics_path should be cleared after prune, still %v", p)
	}
}

func TestBackgroundAutoPruneDisabledByDefault(t *testing.T) {
	a := &App{}
	if a.computerUseLogAutoPruneEnabled() {
		t.Fatal("auto prune should default off")
	}
}

func TestListComputerUseLogArtifactsKindFilter(t *testing.T) {
	dir, err := computerUseLogsDir()
	if err != nil {
		t.Fatal(err)
	}
	pDiag := filepath.Join(dir, "computer-use-diag-filter-test.json")
	pCSV := filepath.Join(dir, "computer-use-history-filter-test.csv")
	_ = os.WriteFile(pDiag, []byte(`{}`), 0o600)
	_ = os.WriteFile(pCSV, []byte("a\n"), 0o600)
	t.Cleanup(func() {
		_ = os.Remove(pDiag)
		_ = os.Remove(pCSV)
	})
	a := &App{}
	onlyDiag := a.ListComputerUseLogArtifacts("diag", 50)
	items, _ := onlyDiag["items"].([]map[string]interface{})
	for _, it := range items {
		if it["kind"] != "diag" {
			t.Fatalf("expected only diag: %#v", it)
		}
	}
	onlyCSV := a.ListComputerUseLogArtifacts("csv", 50)
	items2, _ := onlyCSV["items"].([]map[string]interface{})
	for _, it := range items2 {
		if it["kind"] != "csv" {
			t.Fatalf("expected only csv: %#v", it)
		}
	}
}

func TestCuE2ETokenFound(t *testing.T) {
	if !cuE2ETokenFound("hello MacLawCU123 world", "MacLawCU123") {
		t.Fatal("exact")
	}
	if !cuE2ETokenFound("maclawcu999", "MacLawCU1") {
		t.Fatal("prefix lower")
	}
	if cuE2ETokenFound("nothing", "MacLawCU1") {
		t.Fatal("should miss")
	}
}

func TestComputerUseE2ESmokeShape(t *testing.T) {
	a := &App{}
	// On machines without a display, baseline fails but structure must be present.
	// On interactive Windows it may launch Notepad briefly.
	out := a.ComputerUseE2ESmoke()
	if out == nil {
		t.Fatal("nil")
	}
	for _, key := range []string{"ok", "platform", "steps", "ms", "interact"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing %q: %#v", key, out)
		}
	}
	if out["interact"] != false {
		t.Fatalf("smoke should set interact=false, got %#v", out["interact"])
	}
	steps, _ := out["steps"].([]map[string]interface{})
	if len(steps) < 1 {
		t.Fatalf("expected at least baseline step: %#v", out)
	}
	t.Logf("e2e: ok=%v steps=%d ms=%v err=%v", out["ok"], len(steps), out["ms"], out["error"])
}

func TestComputerUseE2EInteractShape(t *testing.T) {
	// Types into a real Notepad/TextEdit — opt-in only.
	if testing.Short() || os.Getenv("MACLAW_CU_E2E") != "1" {
		t.Skip("set MACLAW_CU_E2E=1 to run interactive E2E (types into editor)")
	}
	a := &App{}
	out := a.ComputerUseE2EInteract()
	if out == nil {
		t.Fatal("nil")
	}
	if out["interact"] != true {
		t.Fatalf("interact flag: %#v", out["interact"])
	}
	steps, _ := out["steps"].([]map[string]interface{})
	if len(steps) < 1 {
		t.Fatalf("no steps: %#v", out)
	}
	// Full interact path: must not silent-pass when type never ran.
	if ok, _ := out["ok"].(bool); ok {
		if _, has := out["type_ok"]; !has {
			t.Fatalf("ok=true requires type_ok on interact: %#v", out)
		}
	} else {
		// soft_fail / skip_reason should explain focus or launch failure
		if out["soft_fail"] != true && out["error"] == "" {
			t.Fatalf("failed interact should set soft_fail or error: %#v", out)
		}
	}
	t.Logf("e2e interact: ok=%v soft_fail=%v skip=%v token=%v found=%v err=%v",
		out["ok"], out["soft_fail"], out["skip_reason"], out["token"], out["token_found"], out["error"])
}

func TestComputerUseE2EInteractDoesNotSilentPass(t *testing.T) {
	// On Windows/macOS this launches an editor — gate with env.
	// On Linux (no launch target) it always runs and must soft-fail, not silent-pass.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if testing.Short() || os.Getenv("MACLAW_CU_E2E") != "1" {
			t.Skip("set MACLAW_CU_E2E=1 to run interactive E2E (types into editor)")
		}
	}
	a := &App{}
	out := a.ComputerUseE2EInteract()
	if out == nil {
		t.Fatal("nil")
	}
	if out["interact"] != true {
		t.Fatalf("interact=%v", out["interact"])
	}
	for _, key := range []string{"ok", "platform", "steps", "ms", "interact"} {
		if _, ok := out[key]; !ok {
			t.Errorf("missing %q", key)
		}
	}
	// If type never happened, ok must be false (no silent soft-pass).
	if out["type_ok"] == nil {
		if ok, _ := out["ok"].(bool); ok {
			t.Fatalf("interact without type must not be ok: %#v", out)
		}
		if out["soft_fail"] != true && out["interact_skipped"] != true {
			// baseline screenshot failure sets ok=false without soft_fail — also fine
			if err, _ := out["error"].(string); err == "" {
				t.Fatalf("expected soft_fail/skip or error: %#v", out)
			}
		}
	}
	t.Logf("interact structure: ok=%v soft_fail=%v skip=%v type_ok=%v err=%v",
		out["ok"], out["soft_fail"], out["skip_reason"], out["type_ok"], out["error"])
}

func TestCuE2ETryFocusPollAttempts(t *testing.T) {
	// Completes attempts without panicking; success depends on desktop state.
	// Use 2 short attempts so the test stays fast.
	matched, ok, errMsg := cuE2ETryFocusPoll("__maclaw_cu_no_such_window_xyz__", 2, 5*time.Millisecond)
	if matched == "" {
		t.Fatal("matched hint should remain non-empty")
	}
	if ok && errMsg != "" {
		t.Fatalf("ok with error: matched=%q err=%q", matched, errMsg)
	}
	if !ok && errMsg == "" {
		// Some backends return empty err on miss — still a valid soft miss.
		t.Log("focus miss with empty error (backend-dependent)")
	}
	t.Logf("focus poll: ok=%v matched=%q err=%q", ok, matched, errMsg)
}

func TestCuE2EPickClickRefEmpty(t *testing.T) {
	// Fresh session with no observe → empty ref.
	globalComputerUse.mu.Lock()
	globalComputerUse.session = computeruse.NewSession(computeruse.DefaultConfig())
	globalComputerUse.mu.Unlock()
	if ref := cuE2EPickClickRef(); ref != "" {
		t.Fatalf("expected empty ref, got %q", ref)
	}
}

func TestCuE2EWindowHints(t *testing.T) {
	h := cuE2EWindowHints("Notepad")
	if len(h) < 2 {
		t.Fatalf("%v", h)
	}
}

func TestGetComputerUseReadinessShape(t *testing.T) {
	a := &App{}
	r := a.GetComputerUseReadiness()
	if r == nil {
		t.Fatal("nil")
	}
	for _, key := range []string{"enabled", "ready", "platform", "issues"} {
		if _, ok := r[key]; !ok {
			t.Errorf("missing %q in %#v", key, r)
		}
	}
	if _, ok := r["issues"].([]map[string]interface{}); !ok {
		// Go may serialize as []interface{} when empty via map assignment — accept both.
		if _, ok2 := r["issues"].([]interface{}); !ok2 {
			// issues is []map[string]interface{} in Go return
			if issues, ok3 := r["issues"].([]map[string]interface{}); ok3 {
				t.Logf("issues=%d", len(issues))
			} else {
				t.Logf("issues type=%T val=%#v", r["issues"], r["issues"])
			}
		}
	}
	t.Logf("readiness: %#v", r)
}

func TestOpenComputerUsePermissionSettingsUnsupportedTargetStillShapes(t *testing.T) {
	// On windows/darwin this would open OS UI — only assert API shape on other GOOS,
	// and on win/darwin just validate the return map keys via a no-op path by
	// checking the helper builds a non-nil map (call Default only if we can't open).
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		// Avoid launching system Settings during unit tests.
		t.Skip("skip opening OS settings UI in automated tests")
	}
	a := &App{}
	out := a.OpenComputerUsePermissionSettings("privacy")
	if out == nil {
		t.Fatal("nil")
	}
	if _, ok := out["ok"].(bool); !ok {
		t.Fatalf("missing ok: %#v", out)
	}
	if out["ok"] != false {
		t.Fatalf("expected ok=false on unsupported platform, got %#v", out)
	}
}
