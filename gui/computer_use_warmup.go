package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const cuObserveHistoryMax = 16

var (
	cuWarmupOnce sync.Once
	cuWarmupMu   sync.Mutex
	cuLastCheck  map[string]interface{}

	cuLastErrorMu sync.Mutex
	cuLastError   map[string]interface{}

	cuLastObserveMu      sync.Mutex
	cuLastObserveMetrics map[string]interface{}
	cuObserveHistory     []map[string]interface{}

	cuLastE2EMu sync.Mutex
	cuLastE2E   map[string]interface{}
)

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// storeComputerUseLastObserveMetrics keeps the latest observe payload summary for operator UI.
func storeComputerUseLastObserveMetrics(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	// Drop heavy fields from the cached metrics snapshot.
	slim := make(map[string]interface{}, 12)
	for _, k := range []string{
		"at", "ok", "error", "guidance", "action", "stage",
		"element_count", "window_count", "yolo_count", "a11y_count", "ocr_count",
		"timing_ms", "total_ms",
	} {
		if v, ok := payload[k]; ok {
			slim[k] = v
		}
	}
	if meta, ok := payload["meta"]; ok {
		slim["meta"] = meta
	}
	if _, ok := slim["at"]; !ok {
		slim["at"] = time.Now().Format(time.RFC3339)
	}
	cuLastObserveMu.Lock()
	cuLastObserveMetrics = slim
	// Rolling history (newest last); in-place trim avoids realloc every overflow.
	entry := cloneStringAnyMap(slim)
	cuObserveHistory = append(cuObserveHistory, entry)
	if n := len(cuObserveHistory); n > cuObserveHistoryMax {
		copy(cuObserveHistory, cuObserveHistory[n-cuObserveHistoryMax:])
		cuObserveHistory = cuObserveHistory[:cuObserveHistoryMax]
	}
	cuLastObserveMu.Unlock()
}

// GetComputerUseLastObserveMetrics returns the last observe timing/counts snapshot.
func (a *App) GetComputerUseLastObserveMetrics() map[string]interface{} {
	cuLastObserveMu.Lock()
	defer cuLastObserveMu.Unlock()
	return cloneStringAnyMap(cuLastObserveMetrics)
}

// GetComputerUseObserveHistory returns the last N observe metrics plus summary stats.
func (a *App) GetComputerUseObserveHistory() map[string]interface{} {
	cuLastObserveMu.Lock()
	defer cuLastObserveMu.Unlock()
	items := make([]map[string]interface{}, 0, len(cuObserveHistory))
	var totals []int64
	okN := 0
	for _, it := range cuObserveHistory {
		items = append(items, cloneStringAnyMap(it))
		if ok, _ := it["ok"].(bool); ok {
			okN++
		}
		if ms := cuAnyToInt64(it["total_ms"]); ms > 0 {
			totals = append(totals, ms)
		}
	}
	summary := map[string]interface{}{
		"count":        len(items),
		"ok_count":     okN,
		"fail_count":   len(items) - okN,
		"avg_total_ms": int64(0),
		"min_total_ms": int64(0),
		"max_total_ms": int64(0),
	}
	if len(totals) > 0 {
		var sum int64
		min, max := totals[0], totals[0]
		for _, v := range totals {
			sum += v
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
		}
		summary["avg_total_ms"] = sum / int64(len(totals))
		summary["min_total_ms"] = min
		summary["max_total_ms"] = max
	}
	return map[string]interface{}{
		"items":   items,
		"summary": summary,
		"max":     cuObserveHistoryMax,
	}
}

func cuAnyToInt64(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case float32:
		return int64(t)
	default:
		return 0
	}
}

// recordComputerUseError stores the latest pipeline error for readiness UI and emits an event.
func recordComputerUseError(stage, errMsg, guidance, action string) {
	payload := map[string]interface{}{
		"at":       time.Now().Format(time.RFC3339),
		"stage":    stage,
		"error":    errMsg,
		"guidance": guidance,
		"action":   action,
		"unix":     time.Now().Unix(),
	}
	cuLastErrorMu.Lock()
	cuLastError = payload
	cuLastErrorMu.Unlock()
	emitComputerUseEvent(EventComputerUseError, payload)
}

func clearComputerUseError() {
	cuLastErrorMu.Lock()
	cuLastError = nil
	cuLastErrorMu.Unlock()
}

// GetComputerUseLastError returns the most recent observe/pipeline error, if any.
func (a *App) GetComputerUseLastError() map[string]interface{} {
	cuLastErrorMu.Lock()
	defer cuLastErrorMu.Unlock()
	if cuLastError == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(cuLastError))
	for k, v := range cuLastError {
		out[k] = v
	}
	return out
}

// backgroundWarmupComputerUse pre-starts Windows UIA sidecar (and optionally
// pings input simulator availability) so the first @computer task is snappier.
func (a *App) backgroundWarmupComputerUse() {
	cuWarmupOnce.Do(func() {
		go a.runComputerUseWarmup("startup")
		go a.backgroundAutoPruneComputerUseLogs()
	})
}

// backgroundAutoPruneComputerUseLogs applies saved prune policy on startup when enabled.
func (a *App) backgroundAutoPruneComputerUseLogs() {
	if a == nil || !a.computerUseLogAutoPruneEnabled() {
		return
	}
	// keep=0 and maxAgeDays=-1 → fully use config policy.
	r := a.PruneComputerUseLogArtifacts(0, -1)
	log.Printf("[computer-use] auto-prune ok=%v deleted=%v freed=%v keep=%v age_days=%v remove_errors=%v err=%v",
		r["ok"], r["deleted_n"], r["freed_bytes"], r["keep_newest"], r["max_age_days"],
		r["remove_error_n"], r["error"])
}

func (a *App) computerUseLogAutoPruneEnabled() bool {
	if a == nil {
		return false
	}
	cfg, err := a.LoadConfig()
	if err != nil || cfg.ComputerUseLogAutoPrune == nil {
		return false
	}
	return *cfg.ComputerUseLogAutoPrune
}

func (a *App) runComputerUseWarmup(reason string) {
	// Startup/idle warmups respect the product toggle; self-check always runs
	// so settings "Run self-check" still diagnoses a11y/input/YOLO when CU is off.
	force := reason == "self_check"
	if a != nil && !a.GetComputerUseEnabled() && !force {
		log.Printf("[computer-use] warmup skipped (%s): disabled", reason)
		return
	}
	start := time.Now()
	log.Printf("[computer-use] warmup begin reason=%s goos=%s", reason, runtime.GOOS)

	result := map[string]interface{}{
		"reason":   reason,
		"platform": runtime.GOOS,
		"at":       time.Now().Format(time.RFC3339),
		"enabled":  a == nil || a.GetComputerUseEnabled(),
	}

	// 1) Accessibility / UIA (Windows C# or PS sidecar).
	// Self-check already ran SelfCheckUIA (which warms); avoid a second cold enum.
	var uia map[string]interface{}
	if reason == "self_check" && accessibility.UIASidecarAlive() {
		uia = map[string]interface{}{
			"ok":      true,
			"backend": accessibility.UIASidecarBackend(),
			"alive":   true,
			"windows": 0,
			"ms":      0,
			"note":    "reused after SelfCheckUIA",
		}
	} else {
		uia = accessibility.WarmupUIA()
	}
	result["uia"] = uia

	// 2) Input simulator smoke (construct only; do not move the real cursor)
	inputOK := true
	inputErr := ""
	func() {
		defer func() {
			if r := recover(); r != nil {
				inputOK = false
				inputErr = "panic constructing input simulator"
			}
		}()
		_ = guiautomation.NewInputSimulator()
	}()
	result["input"] = map[string]interface{}{"ok": inputOK, "error": inputErr}

	// 3) YOLO path + optional weight load into memory
	yolo := map[string]interface{}{
		"enabled": a != nil && a.GetScreenParsingEnabled(),
		"exists":  false,
		"path":    "",
		"loaded":  false,
		"warm_ok": false,
		"warm_ms": int64(0),
	}
	if a != nil {
		info := a.CheckYOLOModel()
		yolo["exists"] = modelStatusExists(info)
		if p, ok := info["path"].(string); ok {
			yolo["path"] = p
		}
	}
	// Load weights when screen parsing is on and file exists (startup + self-check).
	// Skip in-memory load when product is disabled (self_check still reports path/exists).
	if en, _ := yolo["enabled"].(bool); en {
		if ex, _ := yolo["exists"].(bool); ex {
			load := true
			if reason == "self_check" && a != nil && !a.GetComputerUseEnabled() {
				load = false
				yolo["note"] = "weights present; not loaded because computer use disabled"
			}
			if load {
				warm := warmComputerUseYOLO()
				for k, v := range warm {
					yolo[k] = v
				}
			}
		}
	}
	result["yolo"] = yolo

	// 4) OCR sidecar (start only if already installed — no pip during warmup)
	ocr := warmComputerUseOCR()
	result["ocr"] = ocr

	// 5) OS permission / TCC posture (macOS Accessibility + Screen Recording)
	perms := accessibility.ProbeDesktopPermissions()
	result["permissions"] = perms

	ok := true
	if u, okm := uia["ok"].(bool); okm && !u {
		// On non-Windows Warmup is skipped with ok=true.
		if skipped, _ := uia["skipped"].(bool); !skipped {
			ok = false
		}
	}
	if !inputOK {
		ok = false
	}
	// macOS: missing Accessibility is a soft failure for full CU (YOLO still works if capture ok).
	if runtime.GOOS == "darwin" {
		if trusted, has := perms["accessibility_trusted"].(bool); has && !trusted {
			result["permission_warn"] = true
			// Do not hard-fail: pixel/YOLO path may still work after screen recording grant.
		}
		if pok, has := perms["ok"].(bool); has && !pok {
			result["permission_warn"] = true
		}
	}
	// YOLO warm failure is warn-only when weights exist but load fails.
	if wok, has := yolo["warm_ok"].(bool); has && !wok {
		if ex, _ := yolo["exists"].(bool); ex {
			result["yolo_warn"] = true
		}
	}
	result["ok"] = ok
	result["ms"] = time.Since(start).Milliseconds()

	cuWarmupMu.Lock()
	cuLastCheck = result
	cuWarmupMu.Unlock()

	log.Printf("[computer-use] warmup done ok=%v backend=%v yolo_loaded=%v ocr_ready=%v ms=%v",
		ok, uia["backend"], yolo["loaded"], ocr["ready"], result["ms"])

	if a != nil {
		a.emitEvent("computer-use:warmup", result)
		if UpdateComputerUseTray != nil {
			UpdateComputerUseTray()
		}
	}
}

// warmComputerUseYOLO ensures the shared CU YOLO parser exists and loads weights.
func warmComputerUseYOLO() map[string]interface{} {
	start := time.Now()
	out := map[string]interface{}{
		"warm_ok": false,
		"loaded":  false,
		"warm_ms": int64(0),
		"error":   "",
	}

	globalComputerUse.mu.Lock()
	parser := globalComputerUse.yolo
	if parser == nil {
		if p := findYOLOWeights(); p != "" {
			parser = guiautomation.NewYOLOScreenParser(p, 0.3, 0.5)
			parser.SetUnloadDelay(15 * time.Minute)
			globalComputerUse.yolo = parser
		}
	}
	globalComputerUse.mu.Unlock()

	if parser == nil {
		out["error"] = "no YOLO weights path"
		out["warm_ms"] = time.Since(start).Milliseconds()
		return out
	}
	// Already in memory (prior warmup / observe) — skip reload.
	if parser.Loaded() {
		out["warm_ok"] = true
		out["loaded"] = true
		out["warm_ms"] = time.Since(start).Milliseconds()
		out["note"] = "already loaded"
		return out
	}
	if err := parser.Warm(); err != nil {
		out["error"] = err.Error()
		out["warm_ms"] = time.Since(start).Milliseconds()
		return out
	}
	out["warm_ok"] = true
	out["loaded"] = parser.Loaded()
	out["warm_ms"] = time.Since(start).Milliseconds()
	return out
}

// warmComputerUseOCR starts RapidOCR if already installed (no pip install here).
func warmComputerUseOCR() map[string]interface{} {
	start := time.Now()
	out := map[string]interface{}{
		"installed": false,
		"ready":     false,
		"warm_ok":   false,
		"warm_ms":   int64(0),
		"error":     "",
		"skipped":   false,
	}

	globalComputerUse.mu.Lock()
	sc := globalComputerUse.ocrSidecar
	if sc == nil {
		// Tools may not be registered yet at early startup; create a probe sidecar.
		sc = browser.NewRapidOCRSidecar(func(msg string) { log.Printf("[computer-use] %s", msg) })
		globalComputerUse.ocrSidecar = sc
		if globalComputerUse.ocr == nil {
			globalComputerUse.ocr = &taskOCRFromBrowser{inner: sc}
		}
	}
	globalComputerUse.mu.Unlock()

	out["installed"] = sc.Installed()
	if !sc.Installed() {
		out["skipped"] = true
		out["error"] = "OCR not installed yet (will install on first observe)"
		out["warm_ms"] = time.Since(start).Milliseconds()
		return out
	}
	if sc.Ready() {
		out["warm_ok"] = true
		out["ready"] = true
		out["warm_ms"] = time.Since(start).Milliseconds()
		out["note"] = "already running"
		return out
	}
	if err := sc.Warm(); err != nil {
		out["error"] = err.Error()
		out["warm_ms"] = time.Since(start).Milliseconds()
		return out
	}
	out["warm_ok"] = true
	out["ready"] = sc.Ready()
	out["warm_ms"] = time.Since(start).Milliseconds()
	return out
}

// ComputerUseSelfCheck runs diagnostics (may compile C# sidecar) and returns
// a structured report for settings / doctor UI.
func (a *App) ComputerUseSelfCheck() map[string]interface{} {
	start := time.Now()
	report := map[string]interface{}{
		"at":       time.Now().Format(time.RFC3339),
		"enabled":  a != nil && a.GetComputerUseEnabled(),
		"platform": runtime.GOOS,
	}

	// Full UIA self-check (Windows may compile sidecar).
	uia := accessibility.SelfCheckUIA()
	report["uia"] = uia

	// Warmup covers permissions/YOLO/OCR once (avoids double ProbeDesktopPermissions).
	a.runComputerUseWarmup("self_check")

	cuWarmupMu.Lock()
	if cuLastCheck != nil {
		// shallow copy keys
		warm := make(map[string]interface{}, len(cuLastCheck))
		for k, v := range cuLastCheck {
			warm[k] = v
		}
		report["warmup"] = warm
		if perms, ok := warm["permissions"].(map[string]interface{}); ok {
			report["permissions"] = perms
		}
	}
	cuWarmupMu.Unlock()
	if report["permissions"] == nil {
		report["permissions"] = accessibility.ProbeDesktopPermissions()
	}

	// Merge overall ok
	ok := true
	warns := make([]string, 0, 4)
	enabled, _ := report["enabled"].(bool)
	// Desktop smoke is only useful when CU is enabled (moves screenshot path).
	var smoke map[string]interface{}
	if enabled {
		smoke = runComputerUseSmokeObserve(false)
	} else {
		smoke = map[string]interface{}{
			"ok": true, "skipped": true, "note": "computer use disabled",
		}
	}
	report["smoke"] = smoke

	if warm, _ := report["warmup"].(map[string]interface{}); warm != nil {
		if yolo, _ := warm["yolo"].(map[string]interface{}); yolo != nil {
			report["yolo"] = yolo
		}
		if ocr, _ := warm["ocr"].(map[string]interface{}); ocr != nil {
			report["ocr"] = ocr
		}
	}
	if !enabled {
		report["ok"] = true
		report["warnings"] = warns
		report["note"] = "computer use disabled; check still ran for diagnostics"
		report["ms"] = time.Since(start).Milliseconds()
		report["status"] = a.GetComputerUseStatus()
		attachComputerUseSelfCheckArtifacts(a, report)
		return report
	}
	if u, okm := uia["ok"].(bool); okm && !u {
		if skipped, _ := uia["skipped"].(bool); !skipped {
			ok = false
			warns = append(warns, "uia")
		}
	}
	if warm, _ := report["warmup"].(map[string]interface{}); warm != nil {
		if yw, _ := warm["yolo_warn"].(bool); yw {
			warns = append(warns, "yolo_load")
		}
		if pw, _ := warm["permission_warn"].(bool); pw {
			warns = append(warns, "permissions")
		}
	}
	if perms, _ := report["permissions"].(map[string]interface{}); perms != nil {
		if pok, has := perms["ok"].(bool); has && !pok {
			if skipped, _ := perms["skipped"].(bool); !skipped {
				warns = append(warns, "permissions")
			}
		}
	}
	if sok, has := smoke["ok"].(bool); has && !sok {
		// Soft warn only — screenshot can fail headless without meaning CU is misconfigured.
		warns = append(warns, "smoke")
	}
	report["ok"] = ok
	report["warnings"] = warns
	report["ms"] = time.Since(start).Milliseconds()
	report["status"] = a.GetComputerUseStatus()
	report["readiness"] = a.GetComputerUseReadiness()
	attachComputerUseSelfCheckArtifacts(a, report)
	return report
}

// attachComputerUseSelfCheckArtifacts inlines last E2E / log paths for settings UI.
func attachComputerUseSelfCheckArtifacts(a *App, report map[string]interface{}) {
	if report == nil {
		return
	}
	if e2e := a.GetComputerUseLastE2E(); len(e2e) > 0 {
		report["last_e2e"] = map[string]interface{}{
			"ok":               e2e["ok"],
			"interact":         e2e["interact"],
			"ms":               e2e["ms"],
			"error":            e2e["error"],
			"token_found":      e2e["token_found"],
			"type_ok":          e2e["type_ok"],
			"at":               e2e["at"],
			"diagnostics_path": e2e["diagnostics_path"],
			"history_csv_path": e2e["history_csv_path"],
			"focus_retry":      e2e["focus_retry"],
			"soft_fail":        e2e["soft_fail"],
			"skip_reason":      e2e["skip_reason"],
			"token_unconfirmed": e2e["token_unconfirmed"],
		}
		if path, _ := e2e["diagnostics_path"].(string); path != "" {
			report["diagnostics_path"] = path
		}
		if path, _ := e2e["history_csv_path"].(string); path != "" {
			report["history_csv_path"] = path
		}
	}
	if dir, err := computerUseLogsDir(); err == nil {
		report["logs_dir"] = dir
	}
}

// runComputerUseSmokeObserve captures the desktop and optionally parses elements.
// withVision: also run YOLO when weights are available (slower). OCR always off here.
func runComputerUseSmokeObserve(withVision bool) map[string]interface{} {
	start := time.Now()
	timing := map[string]int64{}
	out := map[string]interface{}{
		"ok":            false,
		"screenshot_ok": false,
		"element_count": 0,
		"window_count":  0,
		"yolo_count":    0,
		"a11y_count":    0,
		"width":         0,
		"height":        0,
		"error":         "",
		"guidance":      "",
		"action":        "",
		"ms":            int64(0),
		"timing_ms":     timing,
	}

	// Fast path: screenshot + a11y only (default smoke).
	tShot := time.Now()
	pngB64, err := captureDesktopScreenshot(-1)
	timing["screenshot"] = time.Since(tShot).Milliseconds()
	if err != nil {
		guide, action := cuScreenshotFailureGuidance(err)
		out["error"] = err.Error()
		out["guidance"] = guide
		out["action"] = action
		out["ms"] = time.Since(start).Milliseconds()
		timing["total"] = out["ms"].(int64)
		recordComputerUseError("smoke_screenshot", err.Error(), guide, action)
		storeComputerUseLastObserveMetrics(map[string]interface{}{
			"at":        time.Now().Format(time.RFC3339),
			"ok":        false,
			"error":     err.Error(),
			"guidance":  guide,
			"action":    action,
			"stage":     "smoke_screenshot",
			"timing_ms": timing,
			"total_ms":  out["ms"],
		})
		return out
	}
	out["screenshot_ok"] = true
	if w, h, ok := decodeImageSizeB64(pngB64); ok {
		out["width"] = w
		out["height"] = h
	}

	globalComputerUse.mu.Lock()
	bridge := globalComputerUse.bridge
	yolo := globalComputerUse.yolo
	globalComputerUse.mu.Unlock()
	if bridge == nil {
		bridge = accessibility.NewBridge()
	}

	tA11y := time.Now()
	windows := 0
	a11yN := 0
	if tops, err := bridge.EnumElements(""); err == nil {
		for _, el := range tops {
			if el.Name != "" {
				windows++
			}
		}
		// shallow a11y sample of first named window if any
		for _, el := range tops {
			if el.Name == "" {
				continue
			}
			if tree, err := bridge.EnumElements(el.Name); err == nil {
				var flat []taskengine.UIElement
				flattenA11y(&flat, tree, 0, 2)
				a11yN = len(flat)
			}
			break
		}
	}
	timing["a11y"] = time.Since(tA11y).Milliseconds()
	out["window_count"] = windows
	out["a11y_count"] = a11yN

	yoloN := 0
	tYolo := time.Now()
	if withVision && yolo != nil && yolo.IsAvailable() && computerUseYOLOAllowed() {
		if dets, err := yolo.Parse(pngB64); err == nil {
			yoloN = len(dets)
		} else {
			out["yolo_error"] = err.Error()
		}
	}
	timing["yolo"] = time.Since(tYolo).Milliseconds()
	out["yolo_count"] = yoloN
	out["element_count"] = a11yN + yoloN
	out["ok"] = true
	if windows == 0 && a11yN == 0 && yoloN == 0 {
		out["note"] = "screenshot ok but no UI elements detected (a11y empty; YOLO skipped or empty)"
	}
	out["ms"] = time.Since(start).Milliseconds()
	timing["total"] = out["ms"].(int64)
	clearComputerUseError()
	storeComputerUseLastObserveMetrics(map[string]interface{}{
		"at":            time.Now().Format(time.RFC3339),
		"ok":            true,
		"element_count": out["element_count"],
		"window_count":  windows,
		"yolo_count":    yoloN,
		"a11y_count":    a11yN,
		"ocr_count":     0,
		"timing_ms":     timing,
		"total_ms":      out["ms"],
		"stage":         "smoke",
		"meta": map[string]interface{}{
			"width":  out["width"],
			"height": out["height"],
		},
	})
	return out
}

// ComputerUseSmokeCheck runs a lightweight desktop observe smoke (for settings / tests).
func (a *App) ComputerUseSmokeCheck() map[string]interface{} {
	// Include YOLO if weights exist so operators can verify vision path.
	withVision := false
	if a != nil && a.GetScreenParsingEnabled() {
		if modelStatusExists(a.CheckYOLOModel()) {
			withVision = true
		}
	}
	return runComputerUseSmokeObserve(withVision)
}

// buildComputerUseDiagnostics assembles a support/debug bundle (no screenshots/base64).
// light=true skips SelfCheckUIA (may recompile C#) and live permission probes that
// restart sidecars — used for E2E-failure auto-export.
func (a *App) buildComputerUseDiagnostics(light bool) map[string]interface{} {
	diag := map[string]interface{}{
		"at":       time.Now().Format(time.RFC3339),
		"platform": runtime.GOOS,
		"goarch":   runtime.GOARCH,
		"version":  "computer-use-diag-v1",
		"light":    light,
	}
	if a != nil {
		diag["enabled"] = a.GetComputerUseEnabled()
		diag["screen_parsing"] = a.GetScreenParsingEnabled()
		diag["status"] = a.GetComputerUseStatus()
		// Readiness can re-probe permissions; skip on light path (snapshots suffice).
		if !light {
			diag["readiness"] = a.GetComputerUseReadiness()
		}
		diag["last_warmup"] = a.GetComputerUseLastWarmup()
		diag["last_error"] = a.GetComputerUseLastError()
		diag["last_observe"] = a.GetComputerUseLastObserveMetrics()
		diag["observe_history"] = a.GetComputerUseObserveHistory()
		diag["last_e2e"] = a.GetComputerUseLastE2E()
		diag["yolo"] = a.CheckYOLOModel()
		diag["log_prune_policy"] = a.GetComputerUseLogPrunePolicy()
	}
	if light {
		// Prefer cached warmup permissions; no live probe / SelfCheckUIA.
		if a != nil {
			if warm := a.GetComputerUseLastWarmup(); warm != nil {
				if p, ok := warm["permissions"].(map[string]interface{}); ok {
					diag["permissions"] = p
				}
				if u, ok := warm["uia"].(map[string]interface{}); ok {
					diag["uia"] = u
				}
			}
		}
		if diag["uia"] == nil {
			diag["uia"] = map[string]interface{}{
				"ok":      accessibility.UIASidecarAlive(),
				"backend": accessibility.UIASidecarBackend(),
				"alive":   accessibility.UIASidecarAlive(),
				"note":    "light diagnostics (no SelfCheckUIA)",
			}
		}
		if diag["permissions"] == nil {
			diag["permissions"] = map[string]interface{}{
				"ok": true, "skipped": true, "note": "light diagnostics",
			}
		}
	} else {
		diag["permissions"] = accessibility.ProbeDesktopPermissions()
		// SelfCheckUIA resets C# path cache and may re-resolve/compile. When a
		// sidecar is already healthy, a cheap status snapshot is enough for export.
		if accessibility.UIASidecarAlive() {
			diag["uia"] = map[string]interface{}{
				"ok":            true,
				"platform":      runtime.GOOS,
				"backend_after": accessibility.UIASidecarBackend(),
				"alive_after":   true,
				"note":          "sidecar already running; SelfCheckUIA skipped",
			}
		} else {
			diag["uia"] = accessibility.SelfCheckUIA()
		}
	}
	activated := computerUseSessionActive()
	globalComputerUse.mu.Lock()
	ocrReady := globalComputerUse.ocrSidecar != nil && globalComputerUse.ocrSidecar.Ready()
	ocrInstalled := globalComputerUse.ocrSidecar != nil && globalComputerUse.ocrSidecar.Installed()
	yoloLoaded := globalComputerUse.yolo != nil && globalComputerUse.yolo.Loaded()
	globalComputerUse.mu.Unlock()
	diag["runtime"] = map[string]interface{}{
		"activated":     activated,
		"ocr_ready":     ocrReady,
		"ocr_installed": ocrInstalled,
		"yolo_loaded":   yoloLoaded,
	}
	return diag
}

// storeComputerUseLastE2E keeps a slim copy of the latest E2E/smoke report for diagnostics.
func storeComputerUseLastE2E(report map[string]interface{}) {
	if report == nil {
		return
	}
	slim := cloneStringAnyMap(report)
	// Cap step message sizes in the cached copy (support both typed and []interface{} slices).
	if capped := cuCapE2ESteps(slim["steps"]); capped != nil {
		slim["steps"] = capped
	}
	if _, ok := slim["at"]; !ok {
		slim["at"] = time.Now().Format(time.RFC3339)
	}
	cuLastE2EMu.Lock()
	cuLastE2E = slim
	cuLastE2EMu.Unlock()
}

func cuCapE2ESteps(raw interface{}) []map[string]interface{} {
	var steps []map[string]interface{}
	switch v := raw.(type) {
	case []map[string]interface{}:
		steps = v
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				steps = append(steps, m)
			}
		}
	default:
		return nil
	}
	capped := make([]map[string]interface{}, 0, len(steps))
	for _, st := range steps {
		sc := cloneStringAnyMap(st)
		if msg, ok := sc["message"].(string); ok && len(msg) > 240 {
			sc["message"] = cuTruncateRunes(msg, 240)
		}
		capped = append(capped, sc)
	}
	return capped
}

// GetComputerUseLastE2E returns the last E2E smoke/interact report snapshot.
func (a *App) GetComputerUseLastE2E() map[string]interface{} {
	cuLastE2EMu.Lock()
	defer cuLastE2EMu.Unlock()
	return cloneStringAnyMap(cuLastE2E)
}

// ExportComputerUseDiagnostics writes a JSON diagnostics bundle.
// If a Wails save dialog is available, prompts the user; otherwise writes under MaclawBaseDir/logs.
func (a *App) ExportComputerUseDiagnostics() map[string]interface{} {
	return a.exportComputerUseDiagnostics(false, true)
}

// exportComputerUseDiagnostics is the shared implementation.
// silent=true always writes under logs (no save dialog) — used by E2E failure auto-export.
// withLiveSmoke runs a smoke snapshot into the bundle (skip for nested calls to avoid recursion/latency).
// Silent path uses light diagnostics (no SelfCheckUIA / live permission restart).
func (a *App) exportComputerUseDiagnostics(silent, withLiveSmoke bool) map[string]interface{} {
	start := time.Now()
	out := map[string]interface{}{
		"ok":     false,
		"path":   "",
		"error":  "",
		"ms":     int64(0),
		"silent": silent,
	}
	diag := a.buildComputerUseDiagnostics(silent /* light when silent */)
	var smoke map[string]interface{}
	if withLiveSmoke {
		smoke = runComputerUseSmokeObserve(false)
		diag["smoke"] = smoke
	} else {
		smoke = map[string]interface{}{"ok": nil, "skipped": true}
		diag["smoke"] = smoke
	}

	raw, err := json.MarshalIndent(diag, "", "  ")
	if err != nil {
		out["error"] = err.Error()
		out["ms"] = time.Since(start).Milliseconds()
		return out
	}

	path := ""
	if !silent && a != nil && a.hasWailsEventsContext() {
		sel, derr := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
			Title:           "Export Computer Use diagnostics",
			DefaultFilename: fmt.Sprintf("maclaw-computer-use-diag-%s.json", time.Now().Format("20060102-150405")),
			Filters: []wailsruntime.FileFilter{
				{DisplayName: "JSON", Pattern: "*.json"},
			},
		})
		if derr == nil && strings.TrimSpace(sel) != "" {
			path = sel
		}
	}
	if path == "" {
		dir := filepath.Join(corelib.MaclawBaseDir(), "logs")
		_ = os.MkdirAll(dir, 0o755)
		path = filepath.Join(dir, fmt.Sprintf("computer-use-diag-%s.json", time.Now().Format("20060102-150405")))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		out["error"] = err.Error()
		out["ms"] = time.Since(start).Milliseconds()
		return out
	}
	out["ok"] = true
	out["path"] = path
	out["bytes"] = len(raw)
	out["ms"] = time.Since(start).Milliseconds()
	out["summary"] = map[string]interface{}{
		"smoke_ok":        smoke["ok"],
		"history_count":   0,
		"readiness_ready": false,
	}
	if hist, ok := diag["observe_history"].(map[string]interface{}); ok {
		if sum, ok := hist["summary"].(map[string]interface{}); ok {
			out["summary"].(map[string]interface{})["history_count"] = sum["count"]
			out["summary"].(map[string]interface{})["avg_total_ms"] = sum["avg_total_ms"]
		}
	}
	if ready, ok := diag["readiness"].(map[string]interface{}); ok {
		out["summary"].(map[string]interface{})["readiness_ready"] = ready["ready"]
	}
	if e2e, ok := diag["last_e2e"].(map[string]interface{}); ok && len(e2e) > 0 {
		out["summary"].(map[string]interface{})["last_e2e_ok"] = e2e["ok"]
		out["summary"].(map[string]interface{})["last_e2e_interact"] = e2e["interact"]
		out["summary"].(map[string]interface{})["last_e2e_ms"] = e2e["ms"]
		if tf, ok := e2e["token_found"]; ok {
			out["summary"].(map[string]interface{})["last_e2e_token_found"] = tf
		}
	}
	return out
}

// ExportComputerUseObserveHistoryCSV writes observe timing history as CSV.
func (a *App) ExportComputerUseObserveHistoryCSV() map[string]interface{} {
	return a.exportComputerUseObserveHistoryCSV(false)
}

func (a *App) exportComputerUseObserveHistoryCSV(silent bool) map[string]interface{} {
	start := time.Now()
	out := map[string]interface{}{
		"ok":     false,
		"path":   "",
		"error":  "",
		"ms":     int64(0),
		"rows":   0,
		"silent": silent,
	}
	hist := a.GetComputerUseObserveHistory()
	items, _ := hist["items"].([]map[string]interface{})
	sum, _ := hist["summary"].(map[string]interface{})
	var b strings.Builder
	b.WriteString("at,ok,stage,total_ms,screenshot_ms,yolo_ms,a11y_ms,ocr_ms,commit_ms,element_count,window_count,yolo_count,a11y_count,ocr_count,error\n")
	for _, it := range items {
		tm, _ := it["timing_ms"].(map[string]int64)
		// timing may also arrive as map[string]interface{} from mixed stores
		if tm == nil {
			if generic, ok := it["timing_ms"].(map[string]interface{}); ok {
				tm = map[string]int64{}
				for k, v := range generic {
					tm[k] = cuAnyToInt64(v)
				}
			}
		}
		getTM := func(k string) int64 {
			if tm == nil {
				return 0
			}
			return tm[k]
		}
		errStr, _ := it["error"].(string)
		errStr = strings.ReplaceAll(errStr, "\"", "'")
		errStr = strings.ReplaceAll(errStr, "\n", " ")
		fmt.Fprintf(&b, "%s,%v,%s,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,\"%s\"\n",
			fmt.Sprint(it["at"]),
			it["ok"],
			fmt.Sprint(it["stage"]),
			cuAnyToInt64(it["total_ms"]),
			getTM("screenshot"),
			getTM("yolo"),
			getTM("a11y"),
			getTM("ocr"),
			getTM("commit"),
			cuAnyToInt64(it["element_count"]),
			cuAnyToInt64(it["window_count"]),
			cuAnyToInt64(it["yolo_count"]),
			cuAnyToInt64(it["a11y_count"]),
			cuAnyToInt64(it["ocr_count"]),
			errStr,
		)
	}
	// Summary footer (machine-readable + human comment).
	if sum != nil {
		fmt.Fprintf(&b, "# summary count=%v ok_count=%v fail_count=%v avg_total_ms=%v min_total_ms=%v max_total_ms=%v\n",
			sum["count"], sum["ok_count"], sum["fail_count"],
			sum["avg_total_ms"], sum["min_total_ms"], sum["max_total_ms"])
		// Extra data row for tools that only parse CSV rows.
		fmt.Fprintf(&b, "%s,%v,%s,%d,,,,,,,%d,,,,,,\n",
			time.Now().Format(time.RFC3339),
			true,
			"SUMMARY",
			cuAnyToInt64(sum["avg_total_ms"]),
			cuAnyToInt64(sum["count"]),
		)
	}
	raw := []byte(b.String())

	path := ""
	if !silent && a != nil && a.hasWailsEventsContext() {
		sel, derr := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
			Title:           "Export Computer Use observe history",
			DefaultFilename: fmt.Sprintf("maclaw-computer-use-history-%s.csv", time.Now().Format("20060102-150405")),
			Filters: []wailsruntime.FileFilter{
				{DisplayName: "CSV", Pattern: "*.csv"},
			},
		})
		if derr == nil && strings.TrimSpace(sel) != "" {
			path = sel
		}
	}
	if path == "" {
		dir := filepath.Join(corelib.MaclawBaseDir(), "logs")
		_ = os.MkdirAll(dir, 0o755)
		path = filepath.Join(dir, fmt.Sprintf("computer-use-history-%s.csv", time.Now().Format("20060102-150405")))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		out["error"] = err.Error()
		out["ms"] = time.Since(start).Milliseconds()
		return out
	}
	out["ok"] = true
	out["path"] = path
	out["bytes"] = len(raw)
	out["rows"] = len(items)
	out["ms"] = time.Since(start).Milliseconds()
	return out
}

type computerUseE2EOptions struct {
	// Interact enables focus → optional click(ref) → type unique token → re-observe.
	// Only runs when a target app was launched and focused successfully.
	Interact bool
}

// ComputerUseE2ESmoke runs a best-effort desktop loop:
// baseline smoke → launch a simple editor → focus → observe(window) → cleanup.
func (a *App) ComputerUseE2ESmoke() map[string]interface{} {
	return a.runComputerUseE2E(computerUseE2EOptions{Interact: false})
}

// ComputerUseE2EInteract extends smoke with a safe type-into-editor step (Notepad/TextEdit).
func (a *App) ComputerUseE2EInteract() map[string]interface{} {
	return a.runComputerUseE2E(computerUseE2EOptions{Interact: true})
}

func (a *App) runComputerUseE2E(opts computerUseE2EOptions) map[string]interface{} {
	start := time.Now()
	report := map[string]interface{}{
		"ok":       false,
		"platform": runtime.GOOS,
		"interact": opts.Interact,
		"steps":    []map[string]interface{}{},
		"error":    "",
		"ms":       int64(0),
	}
	addStep := func(name string, data map[string]interface{}) {
		steps, _ := report["steps"].([]map[string]interface{})
		entry := map[string]interface{}{"name": name}
		for k, v := range data {
			entry[k] = v
		}
		report["steps"] = append(steps, entry)
	}

	// Ensure control plane allows actions (previous Stop must not block interact).
	if sess := cuSession(); sess != nil {
		sess.ResetControl()
	}

	// 1) Baseline smoke (screenshot + a11y)
	base := runComputerUseSmokeObserve(false)
	addStep("baseline_smoke", map[string]interface{}{
		"ok": base["ok"], "screenshot_ok": base["screenshot_ok"],
		"window_count": base["window_count"], "ms": base["ms"],
		"error": base["error"], "guidance": base["guidance"],
	})
	if sok, _ := base["screenshot_ok"].(bool); !sok {
		report["error"] = "baseline screenshot failed"
		if g, _ := base["guidance"].(string); g != "" {
			report["error"] = g
		}
		report["ms"] = time.Since(start).Milliseconds()
		cuE2EAttachDiagnostics(a, report)
		storeComputerUseLastE2E(report)
		return report
	}

	// 2) Launch a simple desktop app (best-effort). If launch fails, still try to
	// focus an already-open editor (Notepad/TextEdit) so interact can proceed.
	appName, windowHint, cmd, cleanup := cuE2ELaunchTarget()
	launched := false
	focused := false
	var focusErr string
	addStep("launch_target", map[string]interface{}{
		"app": appName, "window_hint": windowHint, "cmd": cmd != nil,
	})
	if cmd != nil {
		if err := cmd.Start(); err != nil {
			addStep("launch_error", map[string]interface{}{"error": err.Error()})
		} else {
			launched = true
		}
	}
	// Poll focus: new windows can take >1s; also covers pre-existing editor windows.
	if windowHint != "" || appName != "" {
		hint := windowHint
		if hint == "" {
			hint = appName
		}
		// Brief settle before first focus attempt when we just launched.
		if launched {
			time.Sleep(500 * time.Millisecond)
		}
		windowHint, focused, focusErr = cuE2ETryFocusPoll(hint, 6, 400*time.Millisecond)
		if !focused && focusErr != "" {
			addStep("focus_error", map[string]interface{}{
				"error": focusErr, "hint": windowHint, "attempts": 6,
			})
		}
		if focused {
			// Treat focused existing window as usable for interact even if launch failed.
			if !launched {
				launched = true
				report["reused_existing_window"] = true
				addStep("focus_existing", map[string]interface{}{
					"ok": true, "window_hint": windowHint,
				})
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	addStep("focus", map[string]interface{}{
		"ok": focused, "window_hint": windowHint, "launched": launched, "error": focusErr,
	})

	// 3) Observe with window hint when available
	obs := computerUseObserve(-1, windowHint, false)
	addStep("observe_target", map[string]interface{}{
		"ok": obs.OK, "element_count": obs.ElementCount, "window_count": obs.WindowCount,
		"yolo": obs.YOLOCount, "a11y": obs.A11yCount, "total_ms": obs.TotalMs,
		"error": obs.Error, "guidance": obs.Guidance,
	})

	// 4) Optional interact: click best ref (if any) + type unique token into focused editor
	if opts.Interact && launched && focused {
		if sess := cuSession(); sess != nil {
			sess.ResetControl()
		}
		ref := cuE2EPickClickRef()
		if ref != "" {
			clickMsg := cuHandleClick(map[string]interface{}{"ref": ref})
			clickOK := !strings.Contains(strings.ToLower(clickMsg), "fail") &&
				!strings.Contains(strings.ToLower(clickMsg), "blocked") &&
				!strings.Contains(strings.ToLower(clickMsg), "stale")
			addStep("click_ref", map[string]interface{}{
				"ref": ref, "ok": clickOK, "message": cuTruncateRunes(clickMsg, 200),
			})
			// Click invalidates refs; type into the focused window without ref.
		}
		token := fmt.Sprintf("MacLawCU%d", time.Now().Unix()%100000)
		typeMsg := cuHandleType(map[string]interface{}{"text": token + " "})
		typeOK := strings.HasPrefix(typeMsg, "typed ")
		addStep("type_token", map[string]interface{}{
			"ok": typeOK, "token": token, "message": cuTruncateRunes(typeMsg, 200),
		})
		// Soft verify via OCR when available (may miss small text).
		obs2 := computerUseObserve(-1, windowHint, true)
		found := cuE2ETokenFound(obs2.Message, token)
		addStep("verify_observe", map[string]interface{}{
			"ok": obs2.OK, "token_found": found, "element_count": obs2.ElementCount,
			"ocr_count": obs2.OCRCount, "total_ms": obs2.TotalMs, "attempt": 1,
		})

		// If typed OK but OCR/token not found: re-focus (polled) + re-type once, re-verify.
		if typeOK && !found {
			retryHint, retryFocused, retryErr := cuE2ETryFocusPoll(windowHint, 4, 300*time.Millisecond)
			addStep("retry_focus", map[string]interface{}{
				"ok": retryFocused, "window_hint": retryHint, "error": retryErr,
			})
			if retryFocused {
				windowHint = retryHint
				time.Sleep(250 * time.Millisecond)
				if sess := cuSession(); sess != nil {
					sess.ResetControl()
				}
				// Prefer re-click a large region if available after fresh observe.
				_ = computerUseObserve(-1, windowHint, false)
				if ref2 := cuE2EPickClickRef(); ref2 != "" {
					_ = cuHandleClick(map[string]interface{}{"ref": ref2})
					addStep("retry_click_ref", map[string]interface{}{"ref": ref2})
				}
				typeMsg2 := cuHandleType(map[string]interface{}{"text": token + " "})
				typeOK2 := strings.HasPrefix(typeMsg2, "typed ")
				addStep("retry_type_token", map[string]interface{}{
					"ok": typeOK2, "token": token, "message": cuTruncateRunes(typeMsg2, 200),
				})
				if typeOK2 {
					typeOK = true
				}
				obs3 := computerUseObserve(-1, windowHint, true)
				found2 := cuE2ETokenFound(obs3.Message, token)
				addStep("retry_verify_observe", map[string]interface{}{
					"ok": obs3.OK, "token_found": found2, "element_count": obs3.ElementCount,
					"ocr_count": obs3.OCRCount, "total_ms": obs3.TotalMs, "attempt": 2,
				})
				found = found || found2
				report["focus_retry"] = true
			}
		}

		report["token"] = token
		report["token_found"] = found
		report["type_ok"] = typeOK
		if !typeOK {
			report["error"] = "type step failed: " + typeMsg
		}
		if typeOK && !found {
			report["token_unconfirmed"] = true
		}
	}

	// 5) Cleanup launched process if we started it
	if cleanup != nil {
		cleanup()
		addStep("cleanup", map[string]interface{}{"ok": true})
	}

	// Success criteria
	report["ok"] = true
	if !obs.OK {
		report["ok"] = false
		report["error"] = obs.Error
		if obs.Guidance != "" {
			report["error"] = obs.Guidance
		}
	}
	if opts.Interact {
		// Interact must not silently soft-pass when launch/focus/type never ran.
		if !launched {
			report["interact_skipped"] = true
			report["soft_fail"] = true
			report["skip_reason"] = "target_app_unavailable_or_launch_failed"
			if errStr, _ := report["error"].(string); errStr == "" {
				report["error"] = "interact: target editor failed to launch"
			}
			report["ok"] = false
		} else if !focused {
			report["interact_skipped"] = true
			report["soft_fail"] = true
			report["skip_reason"] = "focus_failed"
			if errStr, _ := report["error"].(string); errStr == "" {
				report["error"] = "interact: could not focus target window after retries"
			}
			report["ok"] = false
		} else if typeOK, has := report["type_ok"].(bool); has && !typeOK {
			report["ok"] = false
		} else if report["type_ok"] == nil && launched && focused {
			// Focused but interact block did not set type_ok (unexpected).
			report["soft_fail"] = true
			report["skip_reason"] = "interact_not_executed"
			report["error"] = "interact: type step did not run"
			report["ok"] = false
		}
	}
	report["ms"] = time.Since(start).Milliseconds()
	if ok, _ := report["ok"].(bool); !ok {
		cuE2EAttachDiagnostics(a, report)
	}
	storeComputerUseLastE2E(report)
	return report
}

// cuE2EAttachDiagnostics silently dumps diagnostics JSON + history CSV on E2E
// failure and records paths on the report (no save dialog).
func cuE2EAttachDiagnostics(a *App, report map[string]interface{}) {
	if report == nil {
		return
	}
	// Persist current report first so the JSON includes this E2E attempt.
	storeComputerUseLastE2E(report)
	// Skip live smoke inside auto-export to avoid extra desktop work on failure path.
	if a == nil {
		a = &App{}
	}
	exp := a.exportComputerUseDiagnostics(true, false)
	if ok, _ := exp["ok"].(bool); ok {
		report["diagnostics_path"] = exp["path"]
		report["diagnostics_ms"] = exp["ms"]
	} else if err, _ := exp["error"].(string); err != "" {
		report["diagnostics_error"] = err
	}
	csv := a.exportComputerUseObserveHistoryCSV(true)
	if ok, _ := csv["ok"].(bool); ok {
		report["history_csv_path"] = csv["path"]
		report["history_csv_rows"] = csv["rows"]
	} else if err, _ := csv["error"].(string); err != "" {
		report["history_csv_error"] = err
	}
	// Re-store so last_e2e includes artifact paths for readiness/UI.
	storeComputerUseLastE2E(report)
}

// computerUseLogsDir returns MaclawBaseDir/logs (created if needed).
func computerUseLogsDir() (string, error) {
	dir := filepath.Join(corelib.MaclawBaseDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// OpenComputerUseLogsFolder reveals the Computer Use logs directory in the file manager.
func (a *App) OpenComputerUseLogsFolder() map[string]interface{} {
	out := map[string]interface{}{"ok": false, "path": "", "error": ""}
	dir, err := computerUseLogsDir()
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["path"] = dir
	if a == nil {
		out["error"] = "app nil"
		return out
	}
	if err := a.OpenFileOrShowInFolder(dir); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	return out
}

// openComputerUseStoredPath opens a path from last E2E (diag/csv). Prefer
// validated logs-dir resolution; fall back to open-as-is for user Save dialogs.
func (a *App) openComputerUseStoredPath(path, missingNote string) map[string]interface{} {
	out := map[string]interface{}{"ok": false, "path": "", "error": ""}
	path = strings.TrimSpace(path)
	if path == "" {
		return a.OpenComputerUseLogsFolder()
	}
	// Prefer secure open when file lives under logs/.
	if resolved, err := resolveComputerUseLogArtifactPath(path); err == nil {
		path = resolved
	}
	out["path"] = path
	if a == nil {
		out["error"] = "app nil"
		return out
	}
	if err := a.OpenFileOrShowInFolder(path); err != nil {
		dir := filepath.Dir(path)
		if err2 := a.OpenFileOrShowInFolder(dir); err2 != nil {
			out["error"] = err.Error()
			return out
		}
		out["path"] = dir
		out["ok"] = true
		out["note"] = missingNote
		return out
	}
	out["ok"] = true
	return out
}

// OpenComputerUseLastDiagnostics opens the last E2E diagnostics JSON (or its folder).
func (a *App) OpenComputerUseLastDiagnostics() map[string]interface{} {
	e2e := a.GetComputerUseLastE2E()
	path, _ := e2e["diagnostics_path"].(string)
	return a.openComputerUseStoredPath(path, "diagnostics file missing; opened parent folder")
}

// OpenComputerUseLastHistoryCSV opens the last auto-exported history CSV path if any.
func (a *App) OpenComputerUseLastHistoryCSV() map[string]interface{} {
	e2e := a.GetComputerUseLastE2E()
	path, _ := e2e["history_csv_path"].(string)
	return a.openComputerUseStoredPath(path, "csv missing; opened parent folder")
}

// CopyComputerUsePath copies a path string to the system clipboard (Wails or OS fallback).
// which: "diagnostics" | "csv" | "logs" | "" (auto: diagnostics → csv → logs).
func (a *App) CopyComputerUsePath(which string) map[string]interface{} {
	out := map[string]interface{}{"ok": false, "path": "", "which": which, "error": ""}
	path := ""
	which = strings.ToLower(strings.TrimSpace(which))
	e2e := a.GetComputerUseLastE2E()
	switch which {
	case "diagnostics", "diag", "json":
		path, _ = e2e["diagnostics_path"].(string)
	case "csv", "history":
		path, _ = e2e["history_csv_path"].(string)
	case "logs", "folder", "dir":
		if d, err := computerUseLogsDir(); err == nil {
			path = d
		}
	default:
		if p, _ := e2e["diagnostics_path"].(string); strings.TrimSpace(p) != "" {
			path, which = p, "diagnostics"
		} else if p, _ := e2e["history_csv_path"].(string); strings.TrimSpace(p) != "" {
			path, which = p, "csv"
		} else if d, err := computerUseLogsDir(); err == nil {
			path, which = d, "logs"
		}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		out["error"] = "no path available"
		out["which"] = which
		return out
	}
	out["path"] = path
	out["which"] = which
	if err := a.clipboardSetText(path); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	return out
}

func (a *App) clipboardSetText(text string) error {
	if a != nil && a.ctx != nil {
		if err := wailsruntime.ClipboardSetText(a.ctx, text); err == nil {
			return nil
		}
	}
	// OS fallbacks
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "clip")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
		return fmt.Errorf("clipboard set failed")
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	default:
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
		cmd = exec.Command("xsel", "--clipboard", "--input")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
}

// cuLogFileItem is one CU diagnostic/history file under logs/.
type cuLogFileItem struct {
	name string
	path string
	size int64
	mod  time.Time
	kind string // "diag" | "csv"
}

// listComputerUseLogFileItems returns matching artifacts newest-first.
// limit <= 0 means no truncation (used by prune so oldest files are never skipped).
func listComputerUseLogFileItems(kind string, limit int) (files []cuLogFileItem, dir string, err error) {
	dir, err = computerUseLogsDir()
	if err != nil {
		return nil, "", err
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, dir, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !cuIsComputerUseLogArtifactName(name) {
			continue
		}
		fk := "diag"
		if strings.Contains(name, "history") && strings.HasSuffix(name, ".csv") {
			fk = "csv"
		}
		if kind == "diag" && fk != "diag" {
			continue
		}
		if kind == "csv" && fk != "csv" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, cuLogFileItem{
			name: name,
			path: filepath.Join(dir, name),
			size: info.Size(),
			mod:  info.ModTime(),
			kind: fk,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, dir, nil
}

// ListComputerUseLogArtifacts lists Computer Use diagnostic/history files under logs/.
// kind: "" | "all" | "diag" | "csv" — filter by filename prefix.
// limit is capped for UI; prune uses the unlimited internal lister.
func (a *App) ListComputerUseLogArtifacts(kind string, limit int) map[string]interface{} {
	out := map[string]interface{}{
		"ok":    false,
		"dir":   "",
		"items": []map[string]interface{}{},
		"error": "",
		"count": 0,
	}
	if limit <= 0 {
		limit = 40
	}
	if limit > 200 {
		limit = 200
	}
	files, dir, err := listComputerUseLogFileItems(kind, limit)
	if err != nil {
		out["error"] = err.Error()
		out["dir"] = dir
		return out
	}
	out["dir"] = dir
	items := make([]map[string]interface{}, 0, len(files))
	var totalBytes int64
	for _, f := range files {
		totalBytes += f.size
		items = append(items, map[string]interface{}{
			"name":     f.name,
			"path":     f.path,
			"size":     f.size,
			"kind":     f.kind,
			"mod_time": f.mod.Format(time.RFC3339),
			"mod_unix": f.mod.Unix(),
		})
	}
	out["items"] = items
	out["count"] = len(items)
	out["total_bytes"] = totalBytes
	out["ok"] = true
	return out
}

// GetComputerUseLogPrunePolicy returns keep-newest / max-age-days / auto-prune (with defaults).
func (a *App) GetComputerUseLogPrunePolicy() map[string]interface{} {
	keep, age := computerUseLogPrunePolicyFromApp(a)
	return map[string]interface{}{
		"keep_newest":  keep,
		"max_age_days": age,
		"auto_prune":   a != nil && a.computerUseLogAutoPruneEnabled(),
		"defaults": map[string]interface{}{
			"keep_newest":  10,
			"max_age_days": 0,
			"auto_prune":   false,
		},
	}
}

// SetComputerUseLogPrunePolicy persists prune policy via PatchConfigFields.
// autoPrune: 0=leave unchanged, 1=enable, 2=disable (avoids bool+optional over Wails).
func (a *App) SetComputerUseLogPrunePolicy(keepNewest, maxAgeDays, autoPrune int) map[string]interface{} {
	out := map[string]interface{}{
		"ok": false, "error": "",
		"keep_newest": keepNewest, "max_age_days": maxAgeDays,
		"auto_prune": a != nil && a.computerUseLogAutoPruneEnabled(),
	}
	if a == nil {
		out["error"] = "app nil"
		return out
	}
	if keepNewest < 0 {
		keepNewest = 0
	}
	if keepNewest > 100 {
		keepNewest = 100
	}
	if maxAgeDays < 0 {
		maxAgeDays = 0
	}
	if maxAgeDays > 3650 {
		maxAgeDays = 3650
	}
	patch := map[string]interface{}{
		"computer_use_log_keep_newest":  keepNewest,
		"computer_use_log_max_age_days": maxAgeDays,
	}
	if autoPrune == 1 {
		patch["computer_use_log_auto_prune"] = true
	} else if autoPrune == 2 {
		patch["computer_use_log_auto_prune"] = false
	}
	if _, err := a.PatchConfigFields(patch); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	out["keep_newest"] = keepNewest
	out["max_age_days"] = maxAgeDays
	out["auto_prune"] = a.computerUseLogAutoPruneEnabled()
	return out
}

// SetComputerUseLogAutoPrune toggles startup auto-prune only.
func (a *App) SetComputerUseLogAutoPrune(enabled bool) map[string]interface{} {
	out := map[string]interface{}{"ok": false, "auto_prune": enabled, "error": ""}
	if a == nil {
		out["error"] = "app nil"
		return out
	}
	if _, err := a.PatchConfigFields(map[string]interface{}{
		"computer_use_log_auto_prune": enabled,
	}); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	out["auto_prune"] = a.computerUseLogAutoPruneEnabled()
	return out
}

func computerUseLogPrunePolicyFromApp(a *App) (keepNewest, maxAgeDays int) {
	keepNewest, maxAgeDays = 10, 0
	if a == nil {
		return keepNewest, maxAgeDays
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return keepNewest, maxAgeDays
	}
	if cfg.ComputerUseLogKeepNewest != nil && *cfg.ComputerUseLogKeepNewest > 0 {
		keepNewest = *cfg.ComputerUseLogKeepNewest
	}
	if cfg.ComputerUseLogMaxAgeDays != nil && *cfg.ComputerUseLogMaxAgeDays > 0 {
		maxAgeDays = *cfg.ComputerUseLogMaxAgeDays
	}
	return keepNewest, maxAgeDays
}

// PruneComputerUseLogArtifacts deletes old Computer Use diag/csv files under logs/.
// keepNewest: number of newest files to keep per kind (0 = use config/default 10).
// maxAgeDays: <0 uses config; 0 means ignore age; >0 deletes older than N days.
// Enumerates ALL matching artifacts (not the UI list cap) so oldest files are deleted.
func (a *App) PruneComputerUseLogArtifacts(keepNewest int, maxAgeDays int) map[string]interface{} {
	cfgKeep, cfgAge := computerUseLogPrunePolicyFromApp(a)
	if keepNewest <= 0 {
		keepNewest = cfgKeep
	}
	if maxAgeDays < 0 {
		maxAgeDays = cfgAge
	}
	out := map[string]interface{}{
		"ok":              false,
		"deleted":         []string{},
		"kept":            0,
		"error":           "",
		"deleted_n":       0,
		"freed_bytes":     int64(0),
		"keep_newest":     keepNewest,
		"max_age_days":    maxAgeDays,
		"remove_errors":   []string{},
		"remove_error_n":  0,
	}
	if keepNewest <= 0 {
		keepNewest = 10
	}
	if keepNewest > 100 {
		keepNewest = 100
	}
	out["keep_newest"] = keepNewest
	out["max_age_days"] = maxAgeDays

	// Unlimited list so prune can reach the oldest files beyond UI's 200 cap.
	all, _, err := listComputerUseLogFileItems("all", 0)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	// Split by kind, already newest-first.
	var diag, csvs []cuLogFileItem
	for _, it := range all {
		switch it.kind {
		case "diag":
			diag = append(diag, it)
		case "csv":
			csvs = append(csvs, it)
		}
	}
	cutoff := time.Time{}
	if maxAgeDays > 0 {
		cutoff = time.Now().Add(-time.Duration(maxAgeDays) * 24 * time.Hour)
	}
	var deleted []string
	var removeErrors []string
	var freed int64
	pruneList := func(files []cuLogFileItem) {
		for i, it := range files {
			if it.path == "" {
				continue
			}
			drop := i >= keepNewest
			if !cutoff.IsZero() && it.mod.Before(cutoff) {
				drop = true
			}
			if !drop {
				continue
			}
			if err := os.Remove(it.path); err != nil {
				// Already gone is fine (race with manual delete).
				if os.IsNotExist(err) {
					deleted = append(deleted, it.path)
					freed += it.size
					continue
				}
				// Cap error list so a stuck volume does not explode the response.
				if len(removeErrors) < 20 {
					removeErrors = append(removeErrors, filepath.Base(it.path)+": "+err.Error())
				}
				continue
			}
			deleted = append(deleted, it.path)
			freed += it.size
		}
	}
	pruneList(diag)
	pruneList(csvs)
	out["deleted"] = deleted
	out["deleted_n"] = len(deleted)
	out["freed_bytes"] = freed
	out["remove_errors"] = removeErrors
	out["remove_error_n"] = len(removeErrors)
	kept := len(all) - len(deleted)
	if kept < 0 {
		kept = 0
	}
	out["kept"] = kept
	out["scanned"] = len(all)
	// Partial success is still ok; surface first remove error for operator UI.
	out["ok"] = true
	if n := len(removeErrors); n == 1 {
		out["error"] = removeErrors[0]
	} else if n > 1 {
		out["error"] = fmt.Sprintf("%s (+%d more)", removeErrors[0], n-1)
	}
	// Drop dead links from last E2E snapshot when prune removed those files.
	clearComputerUseE2EPathsIfDeleted(deleted)
	emitComputerUseLogsEvent("prune", out)
	return out
}

// cuSameComputerUseLogPath reports whether two paths refer to the same CU log file.
// Handles abs/rel forms and Windows case-insensitive paths (export vs Abs resolve).
func cuSameComputerUseLogPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	// Prefer validated resolve under logs/ when both are CU artifacts.
	if ra, errA := resolveComputerUseLogArtifactPath(a); errA == nil {
		if rb, errB := resolveComputerUseLogArtifactPath(b); errB == nil {
			if runtime.GOOS == "windows" {
				return strings.EqualFold(ra, rb)
			}
			return ra == rb
		}
	}
	aa, errA := filepath.Abs(filepath.Clean(a))
	bb, errB := filepath.Abs(filepath.Clean(b))
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}

// clearComputerUseE2EPathsIfDeleted removes diagnostics/csv paths from the last
// E2E snapshot when those files were deleted (single delete or prune).
func clearComputerUseE2EPathsIfDeleted(deleted []string) {
	if len(deleted) == 0 {
		return
	}
	cuLastE2EMu.Lock()
	if len(cuLastE2E) == 0 {
		cuLastE2EMu.Unlock()
		return
	}
	e2e := cloneStringAnyMap(cuLastE2E)
	cuLastE2EMu.Unlock()

	changed := false
	if p, _ := e2e["diagnostics_path"].(string); p != "" {
		for _, d := range deleted {
			if cuSameComputerUseLogPath(p, d) {
				delete(e2e, "diagnostics_path")
				changed = true
				break
			}
		}
	}
	if p, _ := e2e["history_csv_path"].(string); p != "" {
		for _, d := range deleted {
			if cuSameComputerUseLogPath(p, d) {
				delete(e2e, "history_csv_path")
				changed = true
				break
			}
		}
	}
	if changed {
		storeComputerUseLastE2E(e2e)
	}
}

// resolveComputerUseLogArtifactPath validates and resolves a path under logs/.
func resolveComputerUseLogArtifactPath(path string) (absFile string, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("empty path")
	}
	dir, err := computerUseLogsDir()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(path)
	// Allow basename-only: resolve under logs dir.
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(dir, filepath.Base(clean))
	}
	absFile, err = filepath.Abs(clean)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	// Must be inside logs dir
	rel, err := filepath.Rel(absDir, absFile)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path outside computer use logs directory")
	}
	base := filepath.Base(absFile)
	if !cuIsComputerUseLogArtifactName(base) {
		return "", fmt.Errorf("not a computer use log artifact")
	}
	return absFile, nil
}

// OpenComputerUseLogArtifact opens a single file under the Computer Use logs directory.
// Rejects path traversal / files outside logs/.
func (a *App) OpenComputerUseLogArtifact(path string) map[string]interface{} {
	out := map[string]interface{}{"ok": false, "path": "", "error": ""}
	absFile, err := resolveComputerUseLogArtifactPath(path)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	out["path"] = absFile
	if a == nil {
		out["error"] = "app nil"
		return out
	}
	if err := a.OpenFileOrShowInFolder(absFile); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	return out
}

// emitComputerUseLogsEvent broadcasts prune/delete lifecycle for UI refresh.
func emitComputerUseLogsEvent(op string, result map[string]interface{}) {
	payload := map[string]interface{}{
		"at": time.Now().Format(time.RFC3339),
		"op": op,
	}
	if result != nil {
		for _, k := range []string{
			"ok", "error", "deleted_n", "freed_bytes", "remove_error_n",
			"kept", "scanned", "path", "count",
		} {
			if v, has := result[k]; has {
				payload[k] = v
			}
		}
		if del, ok := result["deleted"].([]string); ok && len(del) > 0 {
			// Cap path list in events (UI only needs count for refresh).
			n := len(del)
			if n > 12 {
				n = 12
			}
			payload["deleted_sample"] = del[:n]
		}
	}
	emitComputerUseEvent(EventComputerUseLogs, payload)
}

// deleteComputerUseLogArtifactPath validates and removes one CU log file under logs/.
// IsNotExist is treated as success (already gone).
func deleteComputerUseLogArtifactPath(path string) (absFile string, freed int64, err error) {
	absFile, err = resolveComputerUseLogArtifactPath(path)
	if err != nil {
		return "", 0, err
	}
	st, err := os.Stat(absFile)
	if err != nil {
		if os.IsNotExist(err) {
			return absFile, 0, nil
		}
		return absFile, 0, err
	}
	size := st.Size()
	if err := os.Remove(absFile); err != nil {
		if os.IsNotExist(err) {
			return absFile, size, nil
		}
		return absFile, 0, err
	}
	return absFile, size, nil
}

// DeleteComputerUseLogArtifact deletes one validated CU log artifact under logs/.
func (a *App) DeleteComputerUseLogArtifact(path string) map[string]interface{} {
	out := map[string]interface{}{"ok": false, "path": "", "error": "", "freed_bytes": int64(0)}
	absFile, freed, err := deleteComputerUseLogArtifactPath(path)
	out["path"] = absFile
	if err != nil {
		out["error"] = err.Error()
		emitComputerUseLogsEvent("delete", out)
		return out
	}
	out["ok"] = true
	out["freed_bytes"] = freed
	out["deleted_n"] = 1
	// Clear last-E2E links so UI does not point at deleted files.
	clearComputerUseE2EPathsIfDeleted([]string{absFile})
	emitComputerUseLogsEvent("delete", out)
	return out
}

// BatchDeleteComputerUseLogArtifacts deletes multiple validated CU log artifacts.
// Invalid paths are reported in errors; valid deletions still proceed (partial ok).
// Cap: 100 paths per call.
func (a *App) BatchDeleteComputerUseLogArtifacts(paths []string) map[string]interface{} {
	out := map[string]interface{}{
		"ok":             false,
		"deleted":        []string{},
		"deleted_n":      0,
		"freed_bytes":    int64(0),
		"error":          "",
		"errors":         []string{},
		"error_n":        0,
		"requested":      len(paths),
		"skipped_empty":  0,
	}
	if len(paths) == 0 {
		out["error"] = "no paths"
		emitComputerUseLogsEvent("batch_delete", out)
		return out
	}
	if len(paths) > 100 {
		paths = paths[:100]
		out["truncated"] = true
	}
	var deleted []string
	var errs []string
	var freed int64
	seen := make(map[string]struct{}, len(paths))
	skippedEmpty := 0
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			skippedEmpty++
			continue
		}
		absFile, n, err := deleteComputerUseLogArtifactPath(p)
		if err != nil {
			if len(errs) < 20 {
				label := p
				if absFile != "" {
					label = filepath.Base(absFile)
				}
				errs = append(errs, label+": "+err.Error())
			}
			continue
		}
		if absFile == "" {
			continue
		}
		// Case-insensitive dedupe on Windows.
		key := absFile
		if runtime.GOOS == "windows" {
			key = strings.ToLower(absFile)
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		deleted = append(deleted, absFile)
		freed += n
	}
	out["skipped_empty"] = skippedEmpty
	out["deleted"] = deleted
	out["deleted_n"] = len(deleted)
	out["freed_bytes"] = freed
	out["errors"] = errs
	out["error_n"] = len(errs)
	out["ok"] = len(deleted) > 0 || len(errs) == 0
	if len(errs) == 1 {
		out["error"] = errs[0]
	} else if len(errs) > 1 {
		out["error"] = fmt.Sprintf("%s (+%d more)", errs[0], len(errs)-1)
	}
	if len(deleted) > 0 {
		clearComputerUseE2EPathsIfDeleted(deleted)
	}
	// All failed → not ok.
	if len(deleted) == 0 && len(errs) > 0 {
		out["ok"] = false
	}
	emitComputerUseLogsEvent("batch_delete", out)
	return out
}

func cuIsComputerUseLogArtifactName(name string) bool {
	switch {
	case strings.HasPrefix(name, "computer-use-diag-") && strings.HasSuffix(name, ".json"):
		return true
	case strings.HasPrefix(name, "maclaw-computer-use-diag-") && strings.HasSuffix(name, ".json"):
		return true
	case strings.HasPrefix(name, "computer-use-history-") && strings.HasSuffix(name, ".csv"):
		return true
	case strings.HasPrefix(name, "maclaw-computer-use-history-") && strings.HasSuffix(name, ".csv"):
		return true
	default:
		return false
	}
}

// cuE2ETryFocus attempts primary + localized window title fragments.
func cuE2ETryFocus(primary string) (matched string, ok bool, errMsg string) {
	var lastErr error
	for _, alt := range cuE2EWindowHints(primary) {
		if err := accessibility.FocusWindow(alt); err == nil {
			return alt, true, ""
		} else {
			lastErr = err
		}
	}
	if lastErr != nil {
		return primary, false, lastErr.Error()
	}
	return primary, false, "no matching window title"
}

// cuE2ETryFocusPoll retries focus with delay — new windows often need >1s.
// attempts is total tries (min 1). delay is wait between tries (not before first).
func cuE2ETryFocusPoll(primary string, attempts int, delay time.Duration) (matched string, ok bool, errMsg string) {
	if attempts < 1 {
		attempts = 1
	}
	matched = primary
	for i := 0; i < attempts; i++ {
		if i > 0 && delay > 0 {
			time.Sleep(delay)
		}
		m, ok, err := cuE2ETryFocus(matched)
		if ok {
			return m, true, ""
		}
		matched = m
		errMsg = err
	}
	return matched, false, errMsg
}

func cuE2ETokenFound(text, token string) bool {
	if token == "" {
		return false
	}
	if strings.Contains(text, token) {
		return true
	}
	return strings.Contains(strings.ToLower(text), "maclawcu")
}

// cuE2EReportRecent is true when the E2E snapshot has no timestamp or is within maxAge.
func cuE2EReportRecent(e2e map[string]interface{}, maxAge time.Duration) bool {
	if e2e == nil {
		return false
	}
	at, _ := e2e["at"].(string)
	if at == "" {
		return true
	}
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return true
	}
	return time.Since(ts) <= maxAge
}

// cuE2EPickClickRef chooses a large/interactable element from the last observe for a safe click.
func cuE2EPickClickRef() string {
	sess := cuSession()
	if sess == nil {
		return ""
	}
	last := sess.LastObserve()
	if last == nil || len(last.Elements) == 0 {
		return ""
	}
	bestRef := ""
	bestArea := 0
	for _, el := range last.Elements {
		// Prefer edit/document-like or large interactable regions (Notepad body via YOLO).
		area := el.BBox[2] * el.BBox[3]
		if area <= 0 {
			continue
		}
		src := strings.ToLower(el.Source + " " + el.Type + " " + el.Name)
		prefer := el.Interactable ||
			strings.Contains(src, "edit") ||
			strings.Contains(src, "document") ||
			strings.Contains(src, "text") ||
			strings.Contains(src, "yolo") ||
			strings.Contains(src, "interact")
		if !prefer && area < 80*80 {
			continue
		}
		score := area
		if prefer {
			score *= 2
		}
		if score > bestArea {
			bestArea = score
			bestRef = el.Ref
		}
	}
	return bestRef
}

func cuE2EWindowHints(primary string) []string {
	// Localized Notepad / TextEdit title fragments.
	alts := []string{primary, "Notepad", "记事本", "無標題", "Untitled", "TextEdit", "文本编辑"}
	out := make([]string, 0, len(alts))
	seen := map[string]bool{}
	for _, a := range alts {
		a = strings.TrimSpace(a)
		if a == "" || seen[a] {
			continue
		}
		seen[a] = true
		out = append(out, a)
	}
	return out
}

// cuE2ELaunchTarget picks a lightweight editor per OS. Returns nil cmd when unavailable.
func cuE2ELaunchTarget() (appName, windowHint string, cmd *exec.Cmd, cleanup func()) {
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad.exe")
		return "notepad", "Notepad", cmd, func() {
			if cmd != nil && cmd.Process != nil {
				_ = cmd.Process.Kill()
				_, _ = cmd.Process.Wait()
			}
		}
	case "darwin":
		// TextEdit is always present; open without creating a document dialog when possible.
		cmd = exec.Command("open", "-a", "TextEdit")
		return "TextEdit", "TextEdit", cmd, nil // open is fire-and-forget; do not kill TextEdit
	default:
		// Avoid launching random Linux editors in CI.
		return "", "", nil, nil
	}
}

// GetComputerUseLastWarmup returns the last warmup/self-check snapshot if any.
func (a *App) GetComputerUseLastWarmup() map[string]interface{} {
	cuWarmupMu.Lock()
	defer cuWarmupMu.Unlock()
	if cuLastCheck == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(cuLastCheck))
	for k, v := range cuLastCheck {
		out[k] = v
	}
	return out
}

// GetComputerUseReadiness returns actionable issues for the chat readiness banner.
// ready=true means no blocking/setup issues for a first Computer Use task.
func (a *App) GetComputerUseReadiness() map[string]interface{} {
	out := map[string]interface{}{
		"enabled":  a != nil && a.GetComputerUseEnabled(),
		"ready":    true,
		"platform": runtime.GOOS,
		"issues":   []map[string]interface{}{},
	}
	if a == nil || !a.GetComputerUseEnabled() {
		out["ready"] = true
		out["note"] = "computer use disabled"
		return out
	}

	issues := make([]map[string]interface{}, 0, 4)

	// OmniParser weights
	spOn := a.GetScreenParsingEnabled()
	yoloInfo := a.CheckYOLOModel()
	yoloExists := modelStatusExists(yoloInfo)
	if spOn && !yoloExists {
		path, _ := yoloInfo["path"].(string)
		issues = append(issues, map[string]interface{}{
			"id":       "yolo_missing",
			"severity": "warn",
			"message":  "OmniParser weights missing — observe will use a11y/OCR only",
			"action":   "download_yolo",
			"path":     path,
		})
	}

	// Permissions (prefer last warmup; live probe if empty)
	var perms map[string]interface{}
	if warm := a.GetComputerUseLastWarmup(); warm != nil {
		if p, ok := warm["permissions"].(map[string]interface{}); ok {
			perms = p
		}
	}
	if perms == nil {
		perms = accessibility.ProbeDesktopPermissions()
	}
	if runtime.GOOS == "darwin" {
		if trusted, ok := perms["accessibility_trusted"].(bool); ok && !trusted {
			issues = append(issues, map[string]interface{}{
				"id":       "accessibility",
				"severity": "warn",
				"message":  "macOS Accessibility permission not granted",
				"action":   "open_accessibility",
			})
		}
		if screen, ok := perms["screen_recording"].(bool); ok && !screen {
			issues = append(issues, map[string]interface{}{
				"id":       "screen_recording",
				"severity": "warn",
				"message":  "macOS Screen Recording permission not granted",
				"action":   "open_screen_recording",
			})
		}
	}

	// OCR optional info
	globalComputerUse.mu.Lock()
	sc := globalComputerUse.ocrSidecar
	globalComputerUse.mu.Unlock()
	if sc != nil && !sc.Installed() {
		issues = append(issues, map[string]interface{}{
			"id":       "ocr_not_installed",
			"severity": "info",
			"message":  "OCR engine not installed yet (auto-installs on first observe)",
			"action":   "",
		})
	}

	// Recent observe/smoke error (last 10 minutes)
	if last := a.GetComputerUseLastError(); len(last) > 0 {
		if unix, ok := last["unix"].(int64); ok && time.Now().Unix()-unix < 600 {
			action, _ := last["action"].(string)
			if action == "open_privacy" {
				action = "open_accessibility"
			}
			msg, _ := last["guidance"].(string)
			if msg == "" {
				msg, _ = last["error"].(string)
			}
			stage, _ := last["stage"].(string)
			issues = append(issues, map[string]interface{}{
				"id":       "last_error_" + stage,
				"severity": "warn",
				"message":  msg,
				"action":   action,
				"stage":    stage,
			})
		}
	}

	// Recent E2E failure (last 30 minutes) — operator/setup banner.
	if e2e := a.GetComputerUseLastE2E(); len(e2e) > 0 && cuE2EReportRecent(e2e, 30*time.Minute) {
		if ok, has := e2e["ok"].(bool); has && !ok {
			msg, _ := e2e["error"].(string)
			if msg == "" {
				msg = "Last Computer Use E2E check failed"
			}
			if path, _ := e2e["diagnostics_path"].(string); path != "" {
				msg = msg + " · diag: " + path
			}
			soft, _ := e2e["soft_fail"].(bool)
			skip, _ := e2e["skip_reason"].(string)
			id := "last_e2e_failed"
			severity := "warn"
			action := "export_diagnostics"
			// Soft interact skips (focus/launch) — re-run E2E+ is the primary fix.
			if soft {
				id = "last_e2e_soft_fail"
				if skip != "" {
					msg = msg + " · skip=" + skip
				}
				switch skip {
				case "focus_failed", "target_app_unavailable_or_launch_failed", "interact_not_executed":
					action = "run_e2e_interact"
				}
			}
			// Point at screen recording when baseline capture was the failure.
			if strings.Contains(strings.ToLower(msg), "screenshot") ||
				strings.Contains(strings.ToLower(msg), "screen recording") {
				if runtime.GOOS == "darwin" {
					action = "open_screen_recording"
				}
			}
			issues = append(issues, map[string]interface{}{
				"id":          id,
				"severity":    severity,
				"message":     msg,
				"action":      action,
				"path":        e2e["diagnostics_path"],
				"soft_fail":   soft,
				"skip_reason": skip,
			})
		} else if interact, _ := e2e["interact"].(bool); interact {
			// Soft: typed OK but OCR did not confirm token.
			typeOK, _ := e2e["type_ok"].(bool)
			found, hasFound := e2e["token_found"].(bool)
			unconfirmed, _ := e2e["token_unconfirmed"].(bool)
			if typeOK && ((hasFound && !found) || unconfirmed) {
				issues = append(issues, map[string]interface{}{
					"id":       "last_e2e_token_unverified",
					"severity": "info",
					"message":  "E2E typed token but OCR/text verify did not confirm it (often OK)",
					"action":   "run_e2e_interact",
				})
			}
		}
		out["last_e2e"] = map[string]interface{}{
			"ok":                e2e["ok"],
			"interact":          e2e["interact"],
			"ms":                e2e["ms"],
			"token_found":       e2e["token_found"],
			"type_ok":           e2e["type_ok"],
			"soft_fail":         e2e["soft_fail"],
			"skip_reason":       e2e["skip_reason"],
			"token_unconfirmed": e2e["token_unconfirmed"],
			"diagnostics_path":  e2e["diagnostics_path"],
			"history_csv_path":  e2e["history_csv_path"],
			"at":                e2e["at"],
		}
		if path, _ := e2e["diagnostics_path"].(string); path != "" {
			// Prefer opening the saved diagnostics over re-exporting.
			for i := range issues {
				id, _ := issues[i]["id"].(string)
				if id == "last_e2e_failed" || id == "last_e2e_soft_fail" {
					if act, _ := issues[i]["action"].(string); act == "export_diagnostics" {
						issues[i]["action"] = "open_diagnostics"
					}
				}
			}
		}
	}

	out["issues"] = issues
	// ready: no warn-level issues
	ready := true
	for _, iss := range issues {
		if sev, _ := iss["severity"].(string); sev == "warn" || sev == "error" {
			ready = false
			break
		}
	}
	out["ready"] = ready
	out["permissions"] = perms
	if yoloExists {
		out["yolo"] = map[string]interface{}{"exists": true, "path": yoloInfo["path"]}
	} else {
		out["yolo"] = map[string]interface{}{"exists": false, "path": yoloInfo["path"], "enabled": spOn}
	}
	return out
}

// OpenComputerUsePermissionSettings opens OS privacy UI for Computer Use setup.
// target: "accessibility" | "screen_recording" | "privacy" | "" (best default).
func (a *App) OpenComputerUsePermissionSettings(target string) map[string]interface{} {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		if runtime.GOOS == "darwin" {
			target = "accessibility"
		} else {
			target = "privacy"
		}
	}
	out := map[string]interface{}{
		"ok":      false,
		"target":  target,
		"platform": runtime.GOOS,
		"error":   "",
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		url := ""
		switch target {
		case "screen_recording", "screen":
			url = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"
		case "accessibility", "a11y", "privacy":
			url = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
		default:
			url = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
		}
		cmd = exec.Command("open", url)
		out["url"] = url
	case "windows":
		// No single TCC page for UIA; open Windows privacy overview as guidance.
		uri := "ms-settings:privacy"
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", uri)
		out["url"] = uri
	default:
		out["error"] = "opening system privacy settings is not supported on this platform"
		return out
	}
	if err := cmd.Start(); err != nil {
		out["error"] = err.Error()
		return out
	}
	out["ok"] = true
	return out
}

// OpenComputerUsePermissionSettingsDefault is a Wails-friendly zero-arg helper.
func (a *App) OpenComputerUsePermissionSettingsDefault() map[string]interface{} {
	return a.OpenComputerUsePermissionSettings("")
}
