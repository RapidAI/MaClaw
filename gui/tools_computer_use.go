package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// computerUseRuntime holds shared CU state for the desktop agent tool handlers.
type computerUseRuntime struct {
	mu         sync.Mutex
	session    *computeruse.Session
	bridge     accessibility.Bridge
	input      guiautomation.InputSimulator
	yolo       *guiautomation.YOLOScreenParser
	ocr        taskengine.OCRProvider
	ocrSidecar *browser.NativeOCRProvider // underlying engine for Warm(); may be nil
	logger     func(string)
	// activated is sticky session state after a successful computer_* observe/action.
	// It expires after computerUseStickyTTL unless refreshed by further CU activity.
	activated   bool
	activatedAt time.Time
	// lastFreshOpenRequestID is the request that last opened the CU gate via a
	// fresh (explicit/semantic) decision. A stopped in-flight turn re-gates with
	// the same request ID while cancel is still taking effect; the ID lets
	// liftComputerUseStopForFreshRequest tell that re-gate apart from a genuine
	// new task and keep the operator's Stop blocking.
	lastFreshOpenRequestID string
}

var globalComputerUse = &computerUseRuntime{}

// taskOCRFromBrowser adapts browser.OCRProvider to taskengine.OCRProvider.
type taskOCRFromBrowser struct {
	inner browser.OCRProvider
}

func (a *taskOCRFromBrowser) Recognize(pngBase64 string) ([]taskengine.OCRResult, error) {
	if a == nil || a.inner == nil {
		return nil, fmt.Errorf("ocr unavailable")
	}
	raw, err := a.inner.Recognize(pngBase64)
	if err != nil {
		return nil, err
	}
	out := make([]taskengine.OCRResult, len(raw))
	for i, r := range raw {
		out[i] = taskengine.OCRResult{
			Text:       r.Text,
			Confidence: r.Confidence,
			BBox:       r.BBox,
		}
	}
	return out, nil
}

func (a *taskOCRFromBrowser) IsAvailable() bool {
	return a != nil && a.inner != nil && a.inner.IsAvailable()
}

// registerComputerUseTools registers text-primary Computer Use tools.
// OmniParser (YOLO) + OCR are the local "eyes" for non-multimodal models.
func registerComputerUseTools(registry *ToolRegistry) {
	if registry == nil {
		return
	}

	logger := func(msg string) { log.Printf("[computer-use] %s", msg) }

	// Snapshot existing runtime (may already be warmed by startup).
	globalComputerUse.mu.Lock()
	yolo := globalComputerUse.yolo
	ocrSidecar := globalComputerUse.ocrSidecar
	bridge := globalComputerUse.bridge
	inputSim := globalComputerUse.input
	sess := globalComputerUse.session
	globalComputerUse.mu.Unlock()

	// Slow construction outside the lock.
	if bridge == nil {
		bridge = accessibility.NewBridge()
	}
	if inputSim == nil {
		inputSim = guiautomation.NewInputSimulator()
	}
	if yolo == nil {
		if p := findYOLOWeights(); p != "" {
			yolo = guiautomation.NewYOLOScreenParser(p, 0.3, 0.5)
			yolo.SetUnloadDelay(15 * time.Minute)
			logger("OmniParser YOLO configured: " + p)
		} else {
			logger("OmniParser weights not found — observe will rely on a11y/OCR only")
		}
	}
	if ocrSidecar == nil {
		ocrSidecar = sharedNativeOCRProvider()
	}
	if sess == nil {
		sess = computeruse.NewSession(computeruse.DefaultConfig())
	}

	// Publish under lock; prefer any instance another goroutine installed while we
	// constructed (warmup vs register race) so a warmed model is never discarded.
	globalComputerUse.mu.Lock()
	if globalComputerUse.yolo != nil {
		yolo = globalComputerUse.yolo
		logger("reusing warmed OmniParser YOLO instance")
	} else {
		globalComputerUse.yolo = yolo
	}
	if globalComputerUse.ocrSidecar != nil {
		ocrSidecar = globalComputerUse.ocrSidecar
		logger("reusing warmed OCR sidecar instance")
	} else {
		globalComputerUse.ocrSidecar = ocrSidecar
	}
	if globalComputerUse.bridge != nil {
		bridge = globalComputerUse.bridge
	} else {
		globalComputerUse.bridge = bridge
	}
	if globalComputerUse.input != nil {
		inputSim = globalComputerUse.input
	} else {
		globalComputerUse.input = inputSim
	}
	if globalComputerUse.session != nil {
		sess = globalComputerUse.session
	} else {
		globalComputerUse.session = sess
	}
	ocr := &taskOCRFromBrowser{inner: ocrSidecar}
	globalComputerUse.ocr = ocr
	globalComputerUse.logger = logger
	globalComputerUse.mu.Unlock()

	// --- computer_observe ---
	registry.Register(RegisteredTool{
		Name: "computer_observe",
		Description: "Observe the desktop as structured TEXT for Computer Use (text-primary). " +
			"Uses local OmniParser (YOLO) + OCR + accessibility. Does NOT return screenshots/base64 — " +
			"safe for text-only models. Returns eN elements; click with computer_click ref=eN. " +
			"Defaults to primary monitor (screen_index=0). Use screen_index=-1 only when you need all monitors stitched.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "observe", "omniparser"},
		Priority: 8,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"window": map[string]interface{}{"type": "string", "description": "Optional window title substring to bias a11y tree"},
			"screen_index": map[string]interface{}{
				"type":        "integer",
				"description": "Monitor index: 0=primary (default), 1=second, …; -1=all monitors stitched (slow, OCR may degrade on huge desktops)",
			},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleObserve(args)
		},
	})

	// --- computer_click ---
	registry.Register(RegisteredTool{
		Name: "computer_click",
		Description: "Click a desktop UI element. Prefer ref=eN from computer_observe. " +
			"button=left|right (default left), count=1|2 (2=double-click). " +
			"Raw x,y only if session allows pixel click (disabled by default for text models).",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "click"},
		Priority: 8,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"ref":    map[string]interface{}{"type": "string", "description": "Element ref from last computer_observe, e.g. e3"},
			"x":      map[string]interface{}{"type": "integer", "description": "Raw X (discouraged; needs allow_pixel_click)"},
			"y":      map[string]interface{}{"type": "integer", "description": "Raw Y (discouraged)"},
			"button": map[string]interface{}{"type": "string", "description": "left (default) or right"},
			"count":  map[string]interface{}{"type": "integer", "description": "1=single (default), 2=double-click"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleClick(args)
		},
	})

	// --- computer_type ---
	registry.Register(RegisteredTool{
		Name: "computer_type",
		Description: "Type text into the desktop. Optional ref=eN clicks that element first. " +
			"Re-observe after typing.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "type"},
		Priority: 8,
		Status:   RegToolAvailable,
		Required: []string{"text"},
		InputSchema: map[string]interface{}{
			"text": map[string]interface{}{"type": "string", "description": "Text to type"},
			"ref":  map[string]interface{}{"type": "string", "description": "Optional element ref to focus first"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleType(args)
		},
	})

	// --- computer_key ---
	registry.Register(RegisteredTool{
		Name:        "computer_key",
		Description: "Press a key or key combination (e.g. enter, ctrl+c, alt+tab). Keys space-separated or as list via keys param.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "keyboard"},
		Priority:    8,
		Status:      RegToolAvailable,
		Required:    []string{"keys"},
		InputSchema: map[string]interface{}{
			"keys": map[string]interface{}{"type": "string", "description": "Keys separated by space or +, e.g. \"ctrl c\" or \"enter\""},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleKey(args)
		},
	})

	// --- computer_scroll ---
	registry.Register(RegisteredTool{
		Name:        "computer_scroll",
		Description: "Scroll at a position. Prefer ref=eN for location; else x,y if pixel click allowed. delta_y positive scrolls down.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "scroll"},
		Priority:    7,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"ref":     map[string]interface{}{"type": "string", "description": "Element ref to scroll over"},
			"x":       map[string]interface{}{"type": "integer"},
			"y":       map[string]interface{}{"type": "integer"},
			"delta_x": map[string]interface{}{"type": "integer", "description": "Horizontal scroll delta"},
			"delta_y": map[string]interface{}{"type": "integer", "description": "Vertical scroll delta (positive=down)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleScroll(args)
		},
	})

	// --- computer_wait ---
	registry.Register(RegisteredTool{
		Name:        "computer_wait",
		Description: "Wait for the UI to settle (screenshot stability) or a fixed duration in ms.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "wait"},
		Priority:    6,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"ms":         map[string]interface{}{"type": "integer", "description": "Fixed wait milliseconds (default 500)"},
			"stable":     map[string]interface{}{"type": "boolean", "description": "If true, wait until visual stability (up to timeout_ms)"},
			"timeout_ms": map[string]interface{}{"type": "integer", "description": "Max wait for stable mode (default 3000)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleWait(args)
		},
	})

	// --- computer_focus ---
	registry.Register(RegisteredTool{
		Name: "computer_focus",
		Description: "Bring a desktop window to the foreground by title substring " +
			"(e.g. Notepad, 记事本). Call before click/type when the target app is not frontmost.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "focus", "window"},
		Priority: 8,
		Status:   RegToolAvailable,
		Required: []string{"window"},
		InputSchema: map[string]interface{}{
			"window": map[string]interface{}{"type": "string", "description": "Window title substring to activate"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleFocus(args)
		},
	})

	// --- computer_done ---
	registry.Register(RegisteredTool{
		Name:        "computer_done",
		Description: "End the Computer Use interaction and summarize result for the user.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop"},
		Priority:    5,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"summary": map[string]interface{}{"type": "string", "description": "What was accomplished or why stopped"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			if msg := cuGuardDisabled("computer_done"); msg != "" {
				return msg
			}
			summary := guiStrArg(args, "summary", "done")
			rt := globalComputerUse
			rt.mu.Lock()
			sess := rt.session
			rt.mu.Unlock()
			steps := 0
			if sess != nil {
				sess.RecordAction("done", summary, true, "", false)
				if p := sess.Policy(); p != nil {
					steps = p.StepCount()
				}
			}
			// End sticky injection so the next unrelated chat does not keep CU tools.
			clearComputerUseSessionActive()
			emitComputerUseDoneControl(steps)
			return fmt.Sprintf("computer_done: %s", summary)
		},
	})

	// --- computer_playbook (optional helper so agents can re-read rules) ---
	registry.Register(RegisteredTool{
		Name:        "computer_playbook",
		Description: "Return Computer Use operating rules for text-only models.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui"},
		Priority:    3,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{},
		Source:      "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			if msg := cuGuardDisabled("computer_playbook"); msg != "" {
				return msg
			}
			return computeruse.Playbook()
		},
	})

	// --- computer_find ---
	registry.Register(RegisteredTool{
		Name: "computer_find",
		Description: "Find on-screen text or UI elements by keyword (e.g. a contact name in an IM app). " +
			"Runs a fresh observe, then searches element labels AND raw OCR text; matches become " +
			"clickable eN refs — even for text no YOLO/a11y element covers. " +
			"Use this before clicking anything you cannot see a ref for.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "find", "search"},
		Priority: 8,
		Status:   RegToolAvailable,
		Required: []string{"query"},
		InputSchema: map[string]interface{}{
			"query":  map[string]interface{}{"type": "string", "description": "Text to find on screen, e.g. a contact name"},
			"window": map[string]interface{}{"type": "string", "description": "Optional window title substring to bias a11y tree"},
			"limit":  map[string]interface{}{"type": "integer", "description": "Max matches (default 10)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleFind(args)
		},
	})

	log.Printf("[computer-use] tools registered (text-primary, OmniParser=%v OCR=available-on-demand)", yolo != nil)
}

func cuSession() *computeruse.Session {
	globalComputerUse.mu.Lock()
	defer globalComputerUse.mu.Unlock()
	if globalComputerUse.session == nil {
		globalComputerUse.session = computeruse.NewSession(computeruse.DefaultConfig())
	}
	// Attribute elements to their owning window at observe time (click policy).
	globalComputerUse.session.SetWindowResolver(accessibility.WindowTitleAtPoint)
	// Re-apply the target-app allowlist on every access so config changes take
	// effect without restarting the session. A config read error keeps the
	// previous allowlist (fail-closed).
	if computerUseTargetAppsFn != nil {
		if apps, ok := computerUseTargetAppsFn(); ok {
			globalComputerUse.session.SetTargetApps(apps)
		}
	}
	return globalComputerUse.session
}

// cuGuardDisabled returns a user-facing message when Computer Use is disabled
// in settings (computer_use_enabled=false), or "" when the tool may run.
func cuGuardDisabled(tool string) string {
	if computerUseToolsEnabled() {
		return ""
	}
	return fmt.Sprintf("%s: Computer Use is disabled (computer_use_enabled=false). Enable it in Settings → Computer Use first.", tool)
}

// cuPolicyWindowTitle resolves the window a click at (x,y) will actually land
// in, falling back to the foreground window title (what type/key target).
func cuPolicyWindowTitle(x, y int) string {
	if t := accessibility.WindowTitleAtPoint(x, y); t != "" {
		return t
	}
	return accessibility.ForegroundWindowTitle()
}

func cuHandleObserve(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		return msg
	}
	// Default to primary monitor. Full multi-monitor stitch (-1) is explicit only —
	// huge virtual desktops crash/degrade OCR and leave all YOLO labels empty.
	screenIdx := guiIntArg(args, "screen_index", 0)
	windowHint := guiStrArg(args, "window", "")
	res := computerUseObserve(screenIdx, windowHint, true)
	if !res.OK {
		return res.Message
	}
	return res.Message
}

// cuHandleFind searches element labels and raw OCR lines for the query,
// reusing a just-taken observation when possible. OCR hits not covered by any
// element are appended to the session ref table so they can be clicked like
// normal refs.
func cuHandleFind(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_find"); msg != "" {
		return msg
	}
	query := guiStrArg(args, "query", "")
	if strings.TrimSpace(query) == "" {
		return "computer_find: query is required"
	}
	windowHint := guiStrArg(args, "window", "")
	limit := guiIntArg(args, "limit", 10)

	sess := cuSession()
	// Reuse a just-taken observation (the playbook flow is observe → find) to
	// skip a second screenshot+YOLO+OCR cycle. Only without a window bias and
	// only while refs are still valid.
	obs := sess.LastObserve()
	tookObserve := false
	if obs == nil || windowHint != "" || !sess.RefsValid() || time.Since(obs.ObservedAt) > 2*time.Second {
		res := computerUseObserve(0, windowHint, true)
		if !res.OK {
			return res.Message
		}
		tookObserve = true
		obs = sess.LastObserve()
	}
	matches := computeruse.FindMatches(obs, query, limit)
	if len(matches) == 0 {
		return fmt.Sprintf("computer_find: no match for %q. Suggestions: pass window=... to include accessibility elements; "+
			"use the app's own search box; or computer_scroll and find again.", query)
	}
	// Assign refs to synthesized (unmarked) OCR matches.
	var synth []computeruse.MarkedElement
	for _, m := range matches {
		if m.Ref == "" {
			synth = append(synth, m)
		}
	}
	assigned := sess.AppendElements(synth)
	if len(assigned) < len(synth) {
		// Refs went stale between observe and append (e.g. operator stop) — report
		// coordinates as guidance instead of dangling refs.
		var b strings.Builder
		fmt.Fprintf(&b, "computer_find %q: %d match(es), but refs could not be assigned — re-run computer_find. Approximate centers:\n", query, len(matches))
		for _, m := range matches {
			fmt.Fprintf(&b, "  %q center=%d,%d src=%s\n", m.Name, m.CenterX, m.CenterY, m.Source)
		}
		return b.String()
	}
	var b strings.Builder
	if tookObserve {
		fmt.Fprintf(&b, "computer_find %q: %d match(es) (fresh observe). Click with computer_click ref=eN.\n", query, len(matches))
	} else {
		fmt.Fprintf(&b, "computer_find %q: %d match(es) (reused last observe). Click with computer_click ref=eN.\n", query, len(matches))
	}
	si := 0
	for _, m := range matches {
		ref := m.Ref
		if ref == "" && si < len(assigned) {
			ref = assigned[si]
			si++
		}
		name := m.Name
		if name == "" {
			name = "(no label)"
		}
		fmt.Fprintf(&b, "  %s [%s] %q center=%d,%d src=%s\n", ref, m.Type, name, m.CenterX, m.CenterY, m.Source)
	}
	return b.String()
}

// computerUseObserveResult is the structured outcome of a desktop observe.
type computerUseObserveResult struct {
	OK           bool
	Message      string // TextForModel or error+guidance for the agent
	Error        string
	Guidance     string
	Action       string
	ElementCount int
	WindowCount  int
	YOLOCount    int
	A11yCount    int
	OCRCount     int
	OCRFailed    bool
	OCRError     string
	ScreenshotOK bool
	Width        int
	Height       int
	// TimingMs is per-stage latency for operator UI / diagnostics.
	TimingMs map[string]int64
	TotalMs  int64
}

// computerUseObserve runs screenshot → YOLO/a11y/OCR → SoM commit.
// withOCR enables OCR (slower); smoke checks may set it false.
func computerUseObserve(screenIdx int, windowHint string, withOCR bool) computerUseObserveResult {
	rt := globalComputerUse
	out := computerUseObserveResult{
		TimingMs: map[string]int64{},
	}
	totalStart := time.Now()

	t0 := time.Now()
	pngB64, err := captureDesktopScreenshot(screenIdx)
	out.TimingMs["screenshot"] = time.Since(t0).Milliseconds()
	if err != nil {
		guide, action := cuScreenshotFailureGuidance(err)
		out.Error = err.Error()
		out.Guidance = guide
		out.Action = action
		out.TotalMs = time.Since(totalStart).Milliseconds()
		out.Message = fmt.Sprintf("computer_observe failed: screenshot: %v\nGuidance: %s", err, guide)
		recordComputerUseError("screenshot", err.Error(), guide, action)
		payload := map[string]interface{}{
			"at":        time.Now().Format(time.RFC3339),
			"ok":        false,
			"error":     err.Error(),
			"guidance":  guide,
			"action":    action,
			"stage":     "screenshot",
			"timing_ms": out.TimingMs,
			"total_ms":  out.TotalMs,
		}
		storeComputerUseLastObserveMetrics(payload)
		emitComputerUseEvent(EventComputerUseObserve, payload)
		return out
	}
	out.ScreenshotOK = true
	if w, h, ok := decodeImageSizeB64(pngB64); ok {
		out.Width, out.Height = w, h
	}
	if screenIdx < 0 && int64(out.Width)*int64(out.Height) > 8_000_000 {
		log.Printf("[computer-use] large stitched capture %dx%d (screen_index=-1); prefer screen_index=0 for reliable OCR", out.Width, out.Height)
	}

	meta := computeruse.ScreenMeta{
		ScaleFactor: 1.0,
		ScreenIndex: screenIdx,
		Width:       out.Width,
		Height:      out.Height,
	}

	var elements []taskengine.UIElement
	rt.mu.Lock()
	yolo := rt.yolo
	ocr := rt.ocr
	bridge := rt.bridge
	rt.mu.Unlock()

	yoloN, a11yN := 0, 0
	useYOLO := yolo != nil && yolo.IsAvailable()
	if useYOLO && !computerUseYOLOAllowed() {
		useYOLO = false
	}
	tYolo := time.Now()
	if useYOLO {
		if dets, err := yolo.Parse(pngB64); err == nil {
			yoloN = len(dets)
			elements = append(elements, dets...)
		} else {
			log.Printf("[computer-use] YOLO parse: %v", err)
		}
	}
	out.TimingMs["yolo"] = time.Since(tYolo).Milliseconds()

	var windows []string
	tA11y := time.Now()
	if bridge != nil {
		if windowHint != "" {
			if tree, err := bridge.EnumElements(windowHint); err == nil {
				before := len(elements)
				flattenA11y(&elements, tree, 0, 3)
				a11yN = len(elements) - before
			}
		} else {
			if tops, err := bridge.EnumElements(""); err == nil {
				for _, el := range tops {
					if el.Name != "" {
						windows = append(windows, el.Name)
					}
				}
			}
		}
	}
	out.TimingMs["a11y"] = time.Since(tA11y).Milliseconds()

	var ocrResults []taskengine.OCRResult
	tOCR := time.Now()
	if withOCR && ocr != nil {
		// Always attempt Recognize: IsAvailable is advisory (install-on-demand).
		res, ocrErr := ocr.Recognize(pngB64)
		if ocrErr != nil {
			log.Printf("[computer-use] OCR: %v", ocrErr)
			out.OCRFailed = true
			out.OCRError = ocrErr.Error()
		} else {
			ocrResults = res
		}
	}
	out.TimingMs["ocr"] = time.Since(tOCR).Milliseconds()

	tCommit := time.Now()
	sess := cuSession()
	obs := sess.CommitObserve(meta, windows, elements, ocrResults, pngB64)
	out.TimingMs["commit"] = time.Since(tCommit).Milliseconds()
	markComputerUseSessionActive()
	clearComputerUseError()

	out.OK = true
	out.Message = appendOCRFailureHint(obs.TextForModel, out.OCRFailed, out.OCRError, screenIdx, out.Width, out.Height)
	out.ElementCount = len(obs.Elements)
	out.WindowCount = len(obs.Windows)
	out.YOLOCount = yoloN
	out.A11yCount = a11yN
	out.OCRCount = len(ocrResults)
	out.TotalMs = time.Since(totalStart).Milliseconds()
	out.TimingMs["total"] = out.TotalMs

	payload := map[string]interface{}{
		"at":            obs.ObservedAt.Format(time.RFC3339),
		"ok":            true,
		"element_count": len(obs.Elements),
		"window_count":  len(obs.Windows),
		"windows":       obs.Windows,
		"ocr_excerpt":   cuTruncateRunes(obs.OCRExcerpt, 400),
		"elements":      summarizeMarksForUI(obs.Elements, 40),
		"meta":          obs.Meta,
		"text_preview":  cuTruncateRunes(out.Message, 1200),
		"yolo_count":    yoloN,
		"a11y_count":    a11yN,
		"ocr_count":     len(ocrResults),
		"ocr_failed":    out.OCRFailed,
		"ocr_error":     out.OCRError,
		"timing_ms":     out.TimingMs,
		"total_ms":      out.TotalMs,
	}
	storeComputerUseLastObserveMetrics(payload)
	emitComputerUseEvent(EventComputerUseObserve, payload)
	return out
}

// appendOCRFailureHint surfaces OCR failures in the model-facing observe text so
// agents stop treating unlabeled YOLO boxes as trustworthy UI labels.
func appendOCRFailureHint(text string, failed bool, errMsg string, screenIdx, width, height int) string {
	if !failed {
		return text
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(text, "\n"))
	b.WriteByte('\n')
	b.WriteString("ocr_failed=true")
	if errMsg != "" {
		// Keep one line: strip embedded newlines so logs/parsers stay stable.
		b.WriteString(" ocr_error=")
		b.WriteString(strings.ReplaceAll(strings.ReplaceAll(errMsg, "\r", " "), "\n", " "))
	}
	b.WriteByte('\n')
	b.WriteString("hint: OCR failed — element labels may be empty. Prefer screen_index=0 (primary monitor). ")
	b.WriteString("Avoid screen_index=-1 (all monitors stitched) on large desktops. ")
	b.WriteString("Retry computer_observe with screen_index=0, or launch apps via shell (e.g. start winword) instead of blind clicks.")
	if screenIdx < 0 {
		b.WriteString(" Current capture used screen_index=-1")
		if width > 0 && height > 0 {
			fmt.Fprintf(&b, " (%dx%d)", width, height)
		}
		b.WriteByte('.')
	}
	b.WriteByte('\n')
	return b.String()
}

func cuScreenshotFailureGuidance(err error) (guidance, action string) {
	if err == nil {
		return "", ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "screen recording"), strings.Contains(msg, "permission not granted"):
		if runtime.GOOS == "darwin" {
			return "Grant Screen Recording in System Settings → Privacy & Security, then retry computer_observe.", "open_screen_recording"
		}
		return "Screen capture permission denied or unavailable. Ensure a desktop session is active.", "open_privacy"
	case strings.Contains(msg, "no graphical display"), strings.Contains(msg, "display"):
		return "No graphical display detected (headless/RDP without desktop?). Connect an interactive desktop session.", ""
	case strings.Contains(msg, "blank"):
		return "Screenshot is blank — wake the display or unlock the screen, then retry.", ""
	default:
		if runtime.GOOS == "darwin" {
			return "Screenshot failed. Check Screen Recording permission and retry computer_observe.", "open_screen_recording"
		}
		return "Screenshot failed. Check display access and retry computer_observe.", ""
	}
}

func summarizeMarksForUI(els []computeruse.MarkedElement, max int) []map[string]interface{} {
	if max <= 0 || max > len(els) {
		max = len(els)
	}
	out := make([]map[string]interface{}, 0, max)
	for i := 0; i < max; i++ {
		el := els[i]
		out = append(out, map[string]interface{}{
			"ref":    el.Ref,
			"name":   el.Name,
			"type":   el.Type,
			"center": []int{el.CenterX, el.CenterY},
			"bbox":   el.BBox,
			"conf":   el.Confidence,
			"source": el.Source,
		})
	}
	return out
}

func cuTruncateRunes(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func emitComputerUseActionUI(action, detail string, ok bool, errMsg string) {
	emitComputerUseEvent(EventComputerUseAction, map[string]interface{}{
		"at":     time.Now().Format(time.RFC3339),
		"action": action,
		"detail": detail,
		"ok":     ok,
		"error":  errMsg,
	})
}

// cuPostActionSettle gives the UI a brief moment to update before the next observe.
func cuPostActionSettle() {
	time.Sleep(180 * time.Millisecond)
}

func cuHandleFocus(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_focus"); msg != "" {
		return msg
	}
	window := guiStrArg(args, "window", "")
	if window == "" {
		return "computer_focus: missing window (title substring)"
	}
	sess := cuSession()
	detail := fmt.Sprintf("window=%q", window)
	if err := sess.BeginAction("focus", detail); err != nil {
		emitComputerUseActionUI("focus", detail, false, err.Error())
		return fmt.Sprintf("computer_focus: %v", err)
	}
	if err := accessibility.FocusWindow(window); err != nil {
		sess.RecordAction("focus", detail, false, err.Error(), false)
		emitComputerUseActionUI("focus", detail, false, err.Error())
		return fmt.Sprintf("computer_focus failed: %v", err)
	}
	sess.RecordAction("focus", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("focus", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("focused window matching %q — call computer_observe again", window)
}

func flattenA11y(dst *[]taskengine.UIElement, nodes []accessibility.Element, depth, maxDepth int) {
	if depth >= maxDepth {
		return
	}
	for _, n := range nodes {
		interact := n.Role == "button" || n.Role == "Button" ||
			strings.Contains(strings.ToLower(n.Role), "edit") ||
			strings.Contains(strings.ToLower(n.Role), "menu") ||
			strings.Contains(strings.ToLower(n.Role), "link") ||
			strings.Contains(strings.ToLower(n.Role), "check") ||
			strings.Contains(strings.ToLower(n.Role), "tab") ||
			n.Role == "ListItem"
		if n.Name != "" || n.Value != "" || interact {
			*dst = append(*dst, taskengine.UIElement{
				Type:         n.Role,
				Name:         n.Name,
				Value:        n.Value,
				BBox:         [4]int{n.Bounds.X, n.Bounds.Y, n.Bounds.Width, n.Bounds.Height},
				Interactable: interact || n.Name != "",
				Confidence:   1.0,
				Source:       "accessibility",
			})
		}
		if len(n.Children) > 0 {
			flattenA11y(dst, n.Children, depth+1, maxDepth)
		}
	}
}

func cuHandleClick(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_click"); msg != "" {
		return msg
	}
	rt := globalComputerUse
	rt.mu.Lock()
	input := rt.input
	rt.mu.Unlock()
	if input == nil {
		return "computer_click: input simulator unavailable"
	}
	sess := cuSession()
	ref := guiStrArg(args, "ref", "")
	button := strings.ToLower(strings.TrimSpace(guiStrArg(args, "button", "left")))
	if button == "" {
		button = "left"
	}
	count := guiIntArg(args, "count", 1)
	if count < 1 {
		count = 1
	}
	if count > 2 {
		count = 2
	}
	var x, y int
	var detail string
	var policyTitle string
	if ref != "" {
		cx, cy, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			return fmt.Sprintf("computer_click: %v", err)
		}
		x, y = cx, cy
		// Prefer the element's owning-window attribution from observe time.
		policyTitle = el.Window
		detail = fmt.Sprintf("ref=%s name=%q at (%d,%d) button=%s count=%d", el.Ref, el.Name, x, y, button, count)
	} else {
		if !sess.AllowPixelClick() {
			return "computer_click: pixel x,y disabled for text-primary mode; call computer_observe and pass ref=eN"
		}
		x = guiIntArg(args, "x", 0)
		y = guiIntArg(args, "y", 0)
		detail = fmt.Sprintf("pixel (%d,%d) button=%s count=%d", x, y, button, count)
	}
	if policyTitle == "" {
		policyTitle = cuPolicyWindowTitle(x, y)
	}
	// Policy before BeginAction: a blocked click must not consume step budget.
	if err := sess.CheckClickPolicy(x, y, policyTitle); err != nil {
		sess.RecordAction("click", detail, false, err.Error(), false)
		emitComputerUseActionUI("click", detail, false, err.Error())
		return fmt.Sprintf("computer_click blocked: %v", err)
	}
	if err := sess.BeginAction("click", detail); err != nil {
		emitComputerUseActionUI("click", detail, false, err.Error())
		return fmt.Sprintf("computer_click: %v", err)
	}
	var err error
	switch {
	case button == "right":
		err = input.RightClick(x, y)
	case count >= 2:
		err = input.DoubleClick(x, y)
	default:
		err = input.Click(x, y)
	}
	if err != nil {
		sess.RecordAction("click", detail, false, err.Error(), false)
		emitComputerUseActionUI("click", detail, false, err.Error())
		return fmt.Sprintf("computer_click failed: %v", err)
	}
	sess.RecordAction("click", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("click", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("clicked %s — call computer_observe again (refs are now stale)", detail)
}

func cuHandleType(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_type"); msg != "" {
		return msg
	}
	rt := globalComputerUse
	rt.mu.Lock()
	input := rt.input
	rt.mu.Unlock()
	if input == nil {
		return "computer_type: input simulator unavailable"
	}
	text := guiStrArg(args, "text", "")
	if text == "" {
		return "computer_type: missing text"
	}
	sess := cuSession()
	ref := guiStrArg(args, "ref", "")
	detail := fmt.Sprintf("chars=%d", len([]rune(text)))
	// Typing sends keystrokes to whatever window is foreground — gate it with
	// the same blocked-window/target-app policy as clicks. Policy before
	// BeginAction so a blocked type must not consume step budget.
	if err := sess.CheckClickPolicy(0, 0, accessibility.ForegroundWindowTitle()); err != nil {
		sess.RecordAction("type", detail, false, err.Error(), false)
		emitComputerUseActionUI("type", detail, false, err.Error())
		return fmt.Sprintf("computer_type blocked: %v", err)
	}
	if err := sess.BeginAction("type", detail); err != nil {
		return fmt.Sprintf("computer_type: %v", err)
	}
	if ref != "" {
		x, y, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			return fmt.Sprintf("computer_type: %v", err)
		}
		detail = fmt.Sprintf("ref=%s name=%q chars=%d", el.Ref, el.Name, len([]rune(text)))
		// Gate the focus click itself: the element may live in a blocked window
		// that is not the current foreground.
		focusTitle := el.Window
		if focusTitle == "" {
			focusTitle = cuPolicyWindowTitle(x, y)
		}
		if err := sess.CheckClickPolicy(x, y, focusTitle); err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			emitComputerUseActionUI("type", detail, false, err.Error())
			return fmt.Sprintf("computer_type blocked: %v", err)
		}
		if err := input.Click(x, y); err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			return fmt.Sprintf("computer_type focus click failed: %v", err)
		}
		time.Sleep(80 * time.Millisecond)
		// Re-check: the focus click may have foregrounded a blocked window.
		if err := sess.CheckClickPolicy(x, y, cuPolicyWindowTitle(x, y)); err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			emitComputerUseActionUI("type", detail, false, err.Error())
			return fmt.Sprintf("computer_type blocked: %v", err)
		}
	}
	if err := input.Type(text); err != nil {
		sess.RecordAction("type", detail, false, err.Error(), false)
		emitComputerUseActionUI("type", detail, false, err.Error())
		return fmt.Sprintf("computer_type failed: %v", err)
	}
	sess.RecordAction("type", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("type", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("typed %s — call computer_observe again", detail)
}

func cuHandleKey(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_key"); msg != "" {
		return msg
	}
	rt := globalComputerUse
	rt.mu.Lock()
	input := rt.input
	rt.mu.Unlock()
	if input == nil {
		return "computer_key: input simulator unavailable"
	}
	raw := guiStrArg(args, "keys", "")
	if raw == "" {
		return "computer_key: missing keys"
	}
	keys := splitKeyCombo(raw)
	sess := cuSession()
	detail := strings.Join(keys, "+")
	// Keys go to the foreground window (enter/space can confirm dialogs) — gate
	// with the same blocked-window/target-app policy as clicks. Policy before
	// BeginAction so a blocked key press must not consume step budget.
	if err := sess.CheckClickPolicy(0, 0, accessibility.ForegroundWindowTitle()); err != nil {
		sess.RecordAction("key", detail, false, err.Error(), false)
		emitComputerUseActionUI("key", detail, false, err.Error())
		return fmt.Sprintf("computer_key blocked: %v", err)
	}
	if err := sess.BeginAction("key", detail); err != nil {
		return fmt.Sprintf("computer_key: %v", err)
	}
	if err := input.KeyCombo(keys...); err != nil {
		sess.RecordAction("key", detail, false, err.Error(), false)
		emitComputerUseActionUI("key", detail, false, err.Error())
		return fmt.Sprintf("computer_key failed: %v", err)
	}
	sess.RecordAction("key", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("key", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("pressed %s — call computer_observe again", detail)
}

func splitKeyCombo(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "+", " ")
	parts := strings.Fields(raw)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cuHandleScroll(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_scroll"); msg != "" {
		return msg
	}
	rt := globalComputerUse
	rt.mu.Lock()
	input := rt.input
	rt.mu.Unlock()
	if input == nil {
		return "computer_scroll: input simulator unavailable"
	}
	sess := cuSession()
	dx := guiIntArg(args, "delta_x", 0)
	dy := guiIntArg(args, "delta_y", 0)
	if dx == 0 && dy == 0 {
		dy = 3
	}
	ref := guiStrArg(args, "ref", "")
	var x, y int
	if ref != "" {
		cx, cy, _, err := sess.ResolveClickRef(ref)
		if err != nil {
			return fmt.Sprintf("computer_scroll: %v", err)
		}
		x, y = cx, cy
	} else if sess.AllowPixelClick() {
		x = guiIntArg(args, "x", 0)
		y = guiIntArg(args, "y", 0)
	} else {
		// Default to screen center of last observe meta if available.
		if last := sess.LastObserve(); last != nil && last.Meta.Width > 0 {
			x = last.Meta.Width / 2
			y = last.Meta.Height / 2
		} else {
			return "computer_scroll: provide ref=eN from computer_observe (or enable pixel click)"
		}
	}
	detail := fmt.Sprintf("at (%d,%d) delta=%d,%d", x, y, dx, dy)
	if err := sess.BeginAction("scroll", detail); err != nil {
		return fmt.Sprintf("computer_scroll: %v", err)
	}
	if err := input.Scroll(x, y, dx, dy); err != nil {
		sess.RecordAction("scroll", detail, false, err.Error(), false)
		emitComputerUseActionUI("scroll", detail, false, err.Error())
		return fmt.Sprintf("computer_scroll failed: %v", err)
	}
	sess.RecordAction("scroll", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("scroll", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("scrolled %s — call computer_observe again", detail)
}

func cuHandleWait(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_wait"); msg != "" {
		return msg
	}
	ms := guiIntArg(args, "ms", 500)
	stable := false
	if v, ok := args["stable"].(bool); ok {
		stable = v
	}
	if stable {
		timeoutMs := guiIntArg(args, "timeout_ms", 3000)
		obs := guiautomation.NewGUIStateObserver(nil, nil, func() (string, error) {
			return captureDesktopScreenshot(-1)
		}, nil)
		if err := obs.WaitForStable(time.Duration(timeoutMs) * time.Millisecond); err != nil {
			return fmt.Sprintf("computer_wait stable: %v", err)
		}
		return fmt.Sprintf("computer_wait: UI considered stable (timeout %dms)", timeoutMs)
	}
	if ms < 0 {
		ms = 0
	}
	if ms > 30000 {
		ms = 30000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return fmt.Sprintf("computer_wait: slept %dms", ms)
}

func decodeImageSizeB64(b64 string) (w, h int, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return 0, 0, false
		}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}
