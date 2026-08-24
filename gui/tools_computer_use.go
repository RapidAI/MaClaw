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
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// computerUseRuntime holds shared CU state for the desktop agent tool handlers.
type computerUseRuntime struct {
	mu          sync.Mutex
	session     *computeruse.Session
	sessions    map[string]*computeruse.Session
	sessionUsed map[string]time.Time
	activeOwner string
	bridge      accessibility.Bridge
	input       guiautomation.InputSimulator
	yolo        *guiautomation.YOLOScreenParser
	ocr         taskengine.OCRProvider
	ocrSidecar  *browser.NativeOCRProvider // underlying engine for Warm(); may be nil
	logger      func(string)
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
	// turnVision / turnVisionKnown are set from the current agent-loop LLM
	// config so observe can skip OmniParser when that model accepts images.
	turnVision      bool
	turnVisionKnown bool
	// pendingModelImage is a PNG (base64) captured by computer_observe in
	// vision mode; the agent loop attaches it as a model-facing screenshot.
	pendingModelImage string
	// taskStates holds the slim per-owner CU MEA contract (P0). Empty
	// Acceptance means computer_done keeps today's self-reported completion.
	taskStates map[string]*computerUseTaskState
	// horizonClaimOnly keyed by CU owner: computer_done returns a claim and
	// must not complete the outer LongHorizon TaskState.
	horizonClaimOnly map[string]bool
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

func cuHandleDone(summary string) string {
	text, _ := cuDoneResult(summary)
	return text
}

// cuDoneResult reports whether the completion was accepted, alongside the text
// describing it. A long-horizon claim and an audit rejection are both refusals
// to complete, and separating them from an accepted completion by reading the
// prose is how a refused completion ends up recorded as a finished task.
func cuDoneResult(summary string) (string, bool) {
	sess, owner := cuSessionAndOwner()
	if horizonComputerUseClaimOnly(owner) {
		if sess != nil {
			sess.RecordAction("done", summary, true, "horizon claim", false)
		}
		return fmt.Sprintf("computer_done claim: %s (does not complete the long-horizon task)", summary), false
	}
	state := snapshotComputerUseTaskState(owner)
	if state != nil && len(state.Acceptance) > 0 {
		passed, reason := applyComputerUseAudit(sess, state)
		if !passed {
			updateComputerUseTaskAudit(owner, computerUseAuditFailed, true)
			if sess != nil {
				sess.RecordAction("done", summary, false, reason, false)
			}
			return fmt.Sprintf("computer_done rejected: %s; call computer_observe and retry", reason), false
		}
		updateComputerUseTaskAudit(owner, computerUseAuditPassed, false)
	} else if state != nil {
		updateComputerUseTaskAudit(owner, computerUseAuditSkipped, false)
	}
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
	return fmt.Sprintf("computer_done: %s", summary), true
}

// registerComputerUseTools registers text-primary Computer Use tools.
// OmniParser (YOLO) + OCR are the local "eyes" for non-multimodal models.
// app may be nil (tests); then observation uses the local OCR engine only.
func registerComputerUseTools(registry *ToolRegistry, app *App) {
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
	if globalComputerUse.sessions == nil {
		globalComputerUse.sessions = make(map[string]*computeruse.Session)
	}
	if globalComputerUse.sessions[computerUseDefaultOwner] == nil {
		globalComputerUse.sessions[computerUseDefaultOwner] = sess
	}
	ocr := &taskOCRFromBrowser{inner: newVisionFirstOCRProvider(app, ocrSidecar)}
	globalComputerUse.ocr = ocr
	globalComputerUse.logger = logger
	globalComputerUse.mu.Unlock()

	// --- computer_observe ---
	registry.Register(RegisteredTool{
		Name: "computer_observe",
		Description: "Observe the desktop for Computer Use. " +
			"If the chat model supports vision, a screenshot is attached — look at the image and click x,y. " +
			"If the model is text-only, local OmniParser (YOLO) + OCR + accessibility return eN element refs (no image). " +
			"Defaults to a crop of the focused window (or window= title). Pass screen_index=0 for the primary monitor, or screen_index=-1 for all monitors stitched.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "observe", "omniparser"},
		Priority: 8,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"window": map[string]interface{}{"type": "string", "description": "Optional window title substring; omitted crops the foreground window"},
			"screen_index": map[string]interface{}{
				"type":        "integer",
				"description": "When set, capture that monitor instead of the focused window: 0=primary, 1=second, …; -1=all monitors stitched (slow, OCR may degrade on huge desktops)",
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
		Description: "Click a desktop UI element. After computer_observe: vision models pass x,y in screenshot pixels; " +
			"text-only models pass ref=eN. button=left|right (default left), count=1|2 (2=double-click).",
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

	registry.Register(RegisteredTool{
		Name:        "computer_select",
		Description: "Select a list, tab, or tree item (UIA SelectionItem / AXPress). Prefer ref=eN from computer_observe.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "select"},
		Priority:    7,
		Status:      RegToolAvailable,
		Required:    []string{"ref"},
		InputSchema: map[string]interface{}{
			"ref": map[string]interface{}{"type": "string", "description": "Element ref from last computer_observe"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleSelect(args)
		},
	})

	registry.Register(RegisteredTool{
		Name:        "computer_scroll_into_view",
		Description: "Scroll a container so ref=eN is visible (UIA ScrollItem / AXScrollToVisible), then you can click it.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "scroll"},
		Priority:    7,
		Status:      RegToolAvailable,
		Required:    []string{"ref"},
		InputSchema: map[string]interface{}{
			"ref": map[string]interface{}{"type": "string", "description": "Element ref to reveal"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleScrollIntoView(args)
		},
	})

	registry.Register(RegisteredTool{
		Name:        "computer_drag",
		Description: "Drag from one on-screen point to another. Prefer from_ref/to_ref; pixel points only when vision/pixel click is allowed.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "drag"},
		Priority:    6,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"from_ref": map[string]interface{}{"type": "string"},
			"to_ref":   map[string]interface{}{"type": "string"},
			"from_x":   map[string]interface{}{"type": "integer"},
			"from_y":   map[string]interface{}{"type": "integer"},
			"to_x":     map[string]interface{}{"type": "integer"},
			"to_y":     map[string]interface{}{"type": "integer"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleDrag(args)
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
			return cuHandleDone(guiStrArg(args, "summary", "done"))
		},
	})

	// --- computer_playbook (optional helper so agents can re-read rules) ---
	registry.Register(RegisteredTool{
		Name:        "computer_playbook",
		Description: "Return Computer Use operating rules for the active perception mode (vision screenshot vs text-primary OmniParser).",
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
			mode := cuSession().Config().Mode
			if computerUseLLMSupportsVision() {
				mode = computeruse.ObserveVisionAssist
			}
			return computeruse.PlaybookFor(mode)
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
			"window": map[string]interface{}{"type": "string", "description": "Optional window title substring; omitted uses the foreground window for a11y"},
			"limit":  map[string]interface{}{"type": "integer", "description": "Max matches (default 10)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleFind(args)
		},
	})

	log.Printf("[computer-use] tools registered (text-primary, OmniParser=%v OCR=available-on-demand)", yolo != nil)

	// Catalog registration only: the computer-use family drives external
	// desktop effects without a trusted receipt boundary, so no intent rule
	// maps LabelComputerUse to this capability and managed routing stays
	// disabled for it. Every computer_* entry is a complete user-facing entry
	// point (there is no merged dispatcher), so each one declares the shared
	// outcome contract.
	for _, name := range []string{
		"computer_observe", "computer_click", "computer_type", "computer_key",
		"computer_scroll", "computer_select", "computer_scroll_into_view",
		"computer_drag", "computer_wait", "computer_focus", "computer_done",
		"computer_playbook", "computer_find",
	} {
		annotateSemanticTool(registry, name, []tool.CapabilityProvision{{
			Capability: tool.CapabilityComputerControlDesktop, Quality: 1,
		}}, []tool.EffectClass{tool.EffectExternalEffect})
	}
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

// cuObserveResult returns the observation text together with whether the
// observation actually succeeded.
//
// cuHandleObserve flattens the two into one string because the legacy tool
// surface has nowhere to carry the flag, which leaves a failed observation
// indistinguishable from a successful one. A caller that can report a failure
// must not have to read the prose to discover there was one.
func cuObserveResult(args map[string]interface{}) (string, bool) {
	if msg := cuGuardDisabled("computer_observe"); msg != "" {
		return msg, false
	}
	// Default: crop the focused window. An explicit screen_index captures that
	// monitor (or -1 = all monitors). Huge virtual desktops degrade OCR.
	screenIdx, screenSet := guiIntArgPresent(args, "screen_index", 0)
	windowHint := guiStrArg(args, "window", "")
	cropFocused := !screenSet && screenIdx >= 0
	res := computerUseObserve(screenIdx, windowHint, true, cropFocused)
	return res.Message, res.OK
}

func cuHandleObserve(args map[string]interface{}) string {
	text, _ := cuObserveResult(args)
	return text
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
	if computerUseLLMSupportsVision() {
		return "computer_find: the chat model can see the screenshot from computer_observe. " +
			"Look at that image and computer_click x,y; do not use OmniParser/OCR find."
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
		res := computerUseObserve(0, windowHint, true, true)
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
	CaptionCount int
	// TimingMs is per-stage latency for operator UI / diagnostics.
	TimingMs map[string]int64
	TotalMs  int64
}

// computerUseObserve runs screenshot → YOLO/a11y/OCR → SoM commit.
// withOCR enables OCR (slower); smoke checks may set it false.
// cropFocused captures the focused (or window=) window instead of a full monitor.
func computerUseObserve(screenIdx int, windowHint string, withOCR, cropFocused bool) computerUseObserveResult {
	return computerUseObserveCaption(screenIdx, windowHint, withOCR, cropFocused, true)
}

// computerUseObserveCaption is the observe implementation. Diagnostics/E2E
// pass caption=false so unlabeled-box HTTP does not inflate self-check time.
func computerUseObserveCaption(screenIdx int, windowHint string, withOCR, cropFocused, caption bool) computerUseObserveResult {
	rt := globalComputerUse
	out := computerUseObserveResult{
		TimingMs: map[string]int64{},
	}
	totalStart := time.Now()

	t0 := time.Now()
	cap, err := captureComputerUseScreen(screenIdx, windowHint, cropFocused)
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
	pngB64 := cap.PNG
	meta := cap.Meta
	out.ScreenshotOK = true
	out.Width, out.Height = cap.Width, cap.Height
	if out.Width == 0 || out.Height == 0 {
		if w, h, ok := decodeImageSizeB64(pngB64); ok {
			out.Width, out.Height = w, h
			if meta.Width == 0 {
				meta.Width = w
			}
			if meta.Height == 0 {
				meta.Height = h
			}
		}
	}
	if screenIdx < 0 && int64(out.Width)*int64(out.Height) > 8_000_000 {
		log.Printf("[computer-use] large stitched capture %dx%d (screen_index=-1); prefer screen_index=0 for reliable OCR", out.Width, out.Height)
	}

	vision := computerUseLLMSupportsVision()
	sess := cuSession()
	sess.ApplyPerceptionMode(vision)
	if !vision {
		setPendingComputerUseModelImage("")
	}

	rt.mu.Lock()
	yolo := rt.yolo
	ocr := rt.ocr
	bridge := rt.bridge
	rt.mu.Unlock()

	yoloN, a11yN := 0, 0
	useYOLO := !vision && yolo != nil && yolo.IsAvailable()
	if useYOLO && !computerUseYOLOAllowed() {
		useYOLO = false
	}
	tYolo := time.Now()
	var yoloEls []taskengine.UIElement
	if useYOLO {
		if dets, err := yolo.Parse(pngB64); err == nil {
			yoloN = len(dets)
			yoloEls = dets
		} else {
			log.Printf("[computer-use] YOLO parse: %v", err)
		}
	}
	out.TimingMs["yolo"] = time.Since(tYolo).Milliseconds()

	var windows []string
	var a11yEls []taskengine.UIElement
	tA11y := time.Now()
	if bridge != nil {
		if tops, err := bridge.EnumElements(""); err == nil {
			for _, el := range tops {
				if el.Name != "" {
					windows = append(windows, el.Name)
				}
			}
		}
		hint := strings.TrimSpace(windowHint)
		if hint == "" {
			hint = accessibility.ForegroundWindowTitle()
		}
		if hint != "" {
			if tree, err := bridge.EnumElements(hint); err == nil {
				flattenA11y(&a11yEls, tree, 0, 5, meta)
				a11yN = len(a11yEls)
			}
		}
	}
	out.TimingMs["a11y"] = time.Since(tA11y).Milliseconds()

	elements, _ := guiautomation.NewCompositeScreenParser(
		&guiautomation.StaticScreenParser{Els: yoloEls},
		&guiautomation.StaticScreenParser{Els: a11yEls},
	).Parse(pngB64)
	if elements == nil {
		elements = []taskengine.UIElement{}
	}

	var ocrResults []taskengine.OCRResult
	tOCR := time.Now()
	if !vision && withOCR && ocr != nil {
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
	obs := sess.CommitObserve(meta, windows, elements, ocrResults, pngB64)
	out.TimingMs["commit"] = time.Since(tCommit).Milliseconds()
	if vision {
		annotated := annotateSoMOverlay(pngB64, obs.Elements)
		modelPNG, vw, vh := prepareVisionScreenshot(annotated)
		meta.VisionWidth = vw
		meta.VisionHeight = vh
		obs.Meta.VisionWidth = vw
		obs.Meta.VisionHeight = vh
		obs.TextForModel = computeruse.RenderVisionObserve(obs)
		setPendingComputerUseModelImage(modelPNG)
	} else if caption {
		tCap := time.Now()
		working := append([]computeruse.MarkedElement(nil), obs.Elements...)
		captionN := applyComputerUseCaptions(pngB64, working)
		out.TimingMs["caption"] = time.Since(tCap).Milliseconds()
		if captionN > 0 {
			sess.ReplaceLastElements(working)
			if last := sess.LastObserve(); last != nil {
				obs = last
			}
		}
		out.CaptionCount = captionN
	}
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
		"caption_count": out.CaptionCount,
		"perception":    obs.Mode,
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

// cuPostActionSettle waits for the UI to settle after an action.
// Windows and macOS poll a short visual idle wait (native capture is cheap).
// Linux sleeps briefly — X11 screenshots are too slow to poll after every click.
func cuPostActionSettle() {
	time.Sleep(80 * time.Millisecond)
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return
	}
	obs := guiautomation.NewGUIStateObserver(nil, nil, func() (string, error) {
		return captureSettleScreenshot()
	}, nil)
	_ = obs.WaitForIdle(300*time.Millisecond, 80*time.Millisecond)
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

func flattenA11y(dst *[]taskengine.UIElement, nodes []accessibility.Element, depth, maxDepth int, meta computeruse.ScreenMeta) {
	if depth >= maxDepth {
		return
	}
	for _, n := range nodes {
		interact := a11yRoleInteractable(n.Role)
		if n.Name != "" || n.Value != "" || interact {
			x, y := computeruse.MapScreenToCapture(meta, n.Bounds.X, n.Bounds.Y)
			w, h := computeruse.ScaleSize(meta, n.Bounds.Width, n.Bounds.Height)
			*dst = append(*dst, taskengine.UIElement{
				Type:         n.Role,
				Name:         n.Name,
				Value:        n.Value,
				BBox:         [4]int{x, y, w, h},
				Interactable: interact || n.Name != "",
				Confidence:   1.0,
				Source:       "accessibility",
				Handle:       n.AutomationID,
				Patterns:     append([]string(nil), n.Patterns...),
			})
		}
		if len(n.Children) > 0 {
			flattenA11y(dst, n.Children, depth+1, maxDepth, meta)
		}
	}
}

func a11yRoleInteractable(role string) bool {
	r := strings.ToLower(role)
	switch {
	case strings.Contains(r, "button"),
		strings.Contains(r, "edit"),
		strings.Contains(r, "menu"),
		strings.Contains(r, "link"),
		strings.Contains(r, "check"),
		strings.Contains(r, "tab"),
		strings.Contains(r, "listitem"),
		strings.Contains(r, "treeitem"),
		strings.Contains(r, "combo"),
		strings.Contains(r, "radio"),
		strings.Contains(r, "hyperlink"),
		strings.Contains(r, "slider"),
		strings.Contains(r, "spinner"),
		strings.Contains(r, "document"),
		r == "textfield",
		r == "edit":
		return true
	default:
		return false
	}
}

func screenMetaFromCapture(screenIdx, imageW, imageH int) computeruse.ScreenMeta {
	meta := computeruse.ScreenMeta{
		ScaleFactor: 1.0,
		ScreenIndex: screenIdx,
		Width:       imageW,
		Height:      imageH,
	}
	displays, err := remote.EnumDisplays()
	if err != nil || len(displays) == 0 {
		return meta
	}
	if screenIdx >= 0 && screenIdx < len(displays) {
		d := displays[screenIdx]
		if screenshotMatchesDisplay(imageW, imageH, d.Width, d.Height) {
			computeruse.ApplyDisplayGeometry(&meta, d.X, d.Y, d.Width, d.Height, imageW, imageH)
			return meta
		}
	}
	minX, minY := displays[0].X, displays[0].Y
	maxX, maxY := displays[0].X+displays[0].Width, displays[0].Y+displays[0].Height
	for _, d := range displays[1:] {
		if d.X < minX {
			minX = d.X
		}
		if d.Y < minY {
			minY = d.Y
		}
		if d.X+d.Width > maxX {
			maxX = d.X + d.Width
		}
		if d.Y+d.Height > maxY {
			maxY = d.Y + d.Height
		}
	}
	computeruse.ApplyDisplayGeometry(&meta, minX, minY, maxX-minX, maxY-minY, imageW, imageH)
	return meta
}

func screenshotMatchesDisplay(imageW, imageH, logicalW, logicalH int) bool {
	if imageW <= 0 || imageH <= 0 || logicalW <= 0 || logicalH <= 0 {
		return false
	}
	for _, s := range []int{1, 2} {
		if absInt(imageW-logicalW*s) <= 8 && absInt(imageH-logicalH*s) <= 8 {
			return true
		}
	}
	return false
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func cuTrySemanticClick(mark *computeruse.MarkedElement, screenX, screenY int) bool {
	if mark == nil || !cuMarkSemantic(mark) {
		return false
	}
	rt := globalComputerUse
	rt.mu.Lock()
	bridge := rt.bridge
	rt.mu.Unlock()
	if bridge == nil {
		return false
	}
	el := cuMarkToA11y(mark, screenX, screenY)
	return bridge.ClickElement(el) == nil
}

func cuTrySemanticType(mark *computeruse.MarkedElement, screenX, screenY int, text string) bool {
	if mark == nil || !cuMarkSemantic(mark) {
		return false
	}
	rt := globalComputerUse
	rt.mu.Lock()
	bridge := rt.bridge
	rt.mu.Unlock()
	if bridge == nil {
		return false
	}
	el := cuMarkToA11y(mark, screenX, screenY)
	return bridge.TypeInElement(el, text) == nil
}

func cuMarkSemantic(mark *computeruse.MarkedElement) bool {
	if mark.Source == "accessibility" || mark.Handle != "" {
		return true
	}
	for _, p := range mark.Patterns {
		switch strings.ToLower(p) {
		case "invoke", "value", "toggle", "select", "expand":
			return true
		}
	}
	return false
}

func cuMarkToA11y(mark *computeruse.MarkedElement, screenX, screenY int) *accessibility.Element {
	return &accessibility.Element{
		Role:         mark.Type,
		Name:         mark.Name,
		Value:        mark.Value,
		AutomationID: mark.Handle,
		Patterns:     append([]string(nil), mark.Patterns...),
		Bounds: accessibility.Rect{
			X:      screenX,
			Y:      screenY,
			Width:  0,
			Height: 0,
		},
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
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_click: " + msg
	}
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
	var mark *computeruse.MarkedElement
	if ref != "" {
		cx, cy, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			return fmt.Sprintf("computer_click: %v", err)
		}
		x, y = cx, cy
		mark = el
		// Prefer the element's owning-window attribution from observe time.
		policyTitle = el.Window
		detail = fmt.Sprintf("ref=%s name=%q at (%d,%d) button=%s count=%d", el.Ref, el.Name, x, y, button, count)
	} else {
		if !sess.AllowPixelClick() {
			return "computer_click: pixel x,y disabled for text-primary mode; call computer_observe and pass ref=eN"
		}
		x = guiIntArg(args, "x", 0)
		y = guiIntArg(args, "y", 0)
		x, y = sess.MapVisionClick(x, y)
		x, y = sess.MapCaptureClick(x, y)
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
	strategy := "pixel"
	switch {
	case button == "right":
		err = input.RightClick(x, y)
	case count >= 2:
		err = input.DoubleClick(x, y)
	default:
		if mark != nil && cuTrySemanticClick(mark, x, y) {
			strategy = "a11y"
		} else {
			err = input.Click(x, y)
		}
	}
	if err != nil {
		sess.RecordAction("click", detail, false, err.Error(), false)
		emitComputerUseActionUI("click", detail, false, err.Error())
		return fmt.Sprintf("computer_click failed: %v", err)
	}
	sess.RecordAction("click", detail+" via "+strategy, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("click", detail+" via "+strategy, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("clicked %s via %s — call computer_observe again (refs are now stale)", detail, strategy)
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
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_type: " + msg
	}
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
	strategy := "pixel"
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
		if cuTrySemanticType(el, x, y, text) {
			strategy = "a11y"
		} else {
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
	}
	if strategy != "a11y" {
		if err := input.Type(text); err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			emitComputerUseActionUI("type", detail, false, err.Error())
			return fmt.Sprintf("computer_type failed: %v", err)
		}
	}
	sess.RecordAction("type", detail+" via "+strategy, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("type", detail+" via "+strategy, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("typed %s via %s — call computer_observe again", detail, strategy)
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
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_key: " + msg
	}
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
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_scroll: " + msg
	}
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
		x, y = sess.MapVisionClick(x, y)
		x, y = sess.MapCaptureClick(x, y)
	} else {
		// Default to screen center of last observe meta if available.
		if last := sess.LastObserve(); last != nil && last.Meta.Width > 0 {
			x = last.Meta.Width / 2
			y = last.Meta.Height / 2
			x, y = sess.MapCaptureClick(x, y)
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

func cuHandleSelect(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_select"); msg != "" {
		return msg
	}
	sess := cuSession()
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_select: " + msg
	}
	ref := guiStrArg(args, "ref", "")
	if ref == "" {
		return "computer_select: missing ref"
	}
	x, y, mark, err := sess.ResolveClickRef(ref)
	if err != nil {
		return fmt.Sprintf("computer_select: %v", err)
	}
	detail := fmt.Sprintf("ref=%s name=%q", mark.Ref, mark.Name)
	if err := sess.CheckClickPolicy(x, y, cuPolicyWindowTitle(x, y)); err != nil {
		return fmt.Sprintf("computer_select blocked: %v", err)
	}
	if err := sess.BeginAction("select", detail); err != nil {
		return fmt.Sprintf("computer_select: %v", err)
	}
	rt := globalComputerUse
	rt.mu.Lock()
	bridge := rt.bridge
	input := rt.input
	rt.mu.Unlock()
	strategy := "pixel"
	if bridge != nil && bridge.SelectElement(cuMarkToA11y(mark, x, y)) == nil {
		strategy = "a11y"
	} else if input != nil {
		if err := input.Click(x, y); err != nil {
			sess.RecordAction("select", detail, false, err.Error(), false)
			return fmt.Sprintf("computer_select failed: %v", err)
		}
	} else {
		sess.RecordAction("select", detail, false, "no actuator", false)
		return "computer_select: input simulator unavailable"
	}
	sess.RecordAction("select", detail+" via "+strategy, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("select", detail+" via "+strategy, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("selected %s via %s — call computer_observe again", detail, strategy)
}

func cuHandleScrollIntoView(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_scroll_into_view"); msg != "" {
		return msg
	}
	sess := cuSession()
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_scroll_into_view: " + msg
	}
	ref := guiStrArg(args, "ref", "")
	if ref == "" {
		return "computer_scroll_into_view: missing ref"
	}
	x, y, mark, err := sess.ResolveClickRef(ref)
	if err != nil {
		return fmt.Sprintf("computer_scroll_into_view: %v", err)
	}
	detail := fmt.Sprintf("ref=%s name=%q", mark.Ref, mark.Name)
	if err := sess.BeginAction("scroll_into_view", detail); err != nil {
		return fmt.Sprintf("computer_scroll_into_view: %v", err)
	}
	rt := globalComputerUse
	rt.mu.Lock()
	bridge := rt.bridge
	rt.mu.Unlock()
	if bridge == nil || bridge.ScrollElementIntoView(cuMarkToA11y(mark, x, y)) != nil {
		sess.RecordAction("scroll_into_view", detail, false, "a11y scroll_into_view unavailable", false)
		return "computer_scroll_into_view: accessibility scroll failed — try computer_scroll then re-observe"
	}
	sess.RecordAction("scroll_into_view", detail+" via a11y", true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("scroll_into_view", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("scrolled %s into view — call computer_observe again", detail)
}

func cuHandleDrag(args map[string]interface{}) string {
	if msg := cuGuardDisabled("computer_drag"); msg != "" {
		return msg
	}
	rt := globalComputerUse
	rt.mu.Lock()
	input := rt.input
	rt.mu.Unlock()
	if input == nil {
		return "computer_drag: input simulator unavailable"
	}
	sess := cuSession()
	if msg := cuRequireSameWindow(sess); msg != "" {
		return "computer_drag: " + msg
	}
	fromX, fromY, err := cuResolveDragPoint(sess, args, "from_ref", "from_x", "from_y")
	if err != nil {
		return "computer_drag: " + err.Error()
	}
	toX, toY, err := cuResolveDragPoint(sess, args, "to_ref", "to_x", "to_y")
	if err != nil {
		return "computer_drag: " + err.Error()
	}
	detail := fmt.Sprintf("(%d,%d)->(%d,%d)", fromX, fromY, toX, toY)
	if err := sess.CheckClickPolicy(fromX, fromY, cuPolicyWindowTitle(fromX, fromY)); err != nil {
		return fmt.Sprintf("computer_drag blocked: %v", err)
	}
	if err := sess.BeginAction("drag", detail); err != nil {
		return fmt.Sprintf("computer_drag: %v", err)
	}
	if err := input.DragDrop(fromX, fromY, toX, toY); err != nil {
		sess.RecordAction("drag", detail, false, err.Error(), false)
		return fmt.Sprintf("computer_drag failed: %v", err)
	}
	sess.RecordAction("drag", detail, true, "", true)
	markComputerUseSessionActive()
	emitComputerUseActionUI("drag", detail, true, "")
	cuPostActionSettle()
	return fmt.Sprintf("dragged %s — call computer_observe again", detail)
}

func cuResolveDragPoint(sess *computeruse.Session, args map[string]interface{}, refKey, xKey, yKey string) (int, int, error) {
	if ref := guiStrArg(args, refKey, ""); ref != "" {
		x, y, _, err := sess.ResolveClickRef(ref)
		return x, y, err
	}
	if !sess.AllowPixelClick() {
		return 0, 0, fmt.Errorf("provide %s (or enable pixel click for %s/%s)", refKey, xKey, yKey)
	}
	x := guiIntArg(args, xKey, 0)
	y := guiIntArg(args, yKey, 0)
	x, y = sess.MapVisionClick(x, y)
	x, y = sess.MapCaptureClick(x, y)
	return x, y, nil
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
