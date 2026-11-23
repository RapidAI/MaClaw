package guiapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"log"
	"runtime"
	"strings"
	"sync"
	"testing"
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
	// activatedOwner is the owner that last marked the sticky session.
	// The rim and the activity bar are one global session, so a finished
	// loop may retire it only while this is still that loop's owner.
	activatedOwner string
	// frameEpoch increments on every mark. A drop that observed an older
	// epoch must not hide a rim a newer mark has already published.
	frameEpoch uint64
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
	// lastModelImage is the newest screenshot sent to the model; the
	// optional grounding model reads it for computer_click target.
	lastModelImage string
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
// Input denial and a spent step budget do not accept the summary. The audit
// still decides. A failed audit on a terminal attempt names that reason and
// does not ask for another observe, and it leaves the latch and the acceptance
// in place. A passing audit, or a task with no acceptance, still completes.
// That completion does not clear the latch. It marks the attempt finished
// so the loop does not start another model round. The next task clears both.
func cuDoneResult(summary string) (string, bool) {
	sess, owner := cuSessionAndOwner()
	if horizonComputerUseClaimOnly(owner) {
		if sess != nil {
			sess.RecordAction("done", summary, true, "horizon claim", false)
		}
		return fmt.Sprintf("computer_done claim: %s (does not complete the long-horizon task)", summary), false
	}
	terminal, terminalReason := cuAttemptTerminal(sess)
	state := snapshotComputerUseTaskState(owner)
	if state != nil && len(state.Acceptance) > 0 {
		passed, reason := applyComputerUseAudit(sess, state)
		if !passed {
			updateComputerUseTaskAudit(owner, computerUseAuditFailed, true)
			if sess != nil {
				sess.RecordAction("done", summary, false, reason, false)
			}
			if terminal {
				return fmt.Sprintf("computer_done rejected: %s; %s Do not observe again.", reason, terminalReason), false
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
	// The summary is the report. Later tools and the next model round stop.
	// The rim comes down only when this owner still holds it. Another
	// owner's session stays, and done is not emitted for them.
	markComputerUseTaskCompleted(owner, summary)
	if _, released := releaseComputerUseSessionForOwner(owner); released {
		emitComputerUseDoneControl(steps)
	}
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
			"Defaults to a crop of the focused window (or window= title). Pass window=<app title> to observe one app. Pass screen_index=0 for the primary monitor. Do not pass screen_index=-1 on a large desktop: a stitch over 8 megapixels is refused and the foreground window is captured instead.",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "observe", "omniparser"},
		Priority: 8,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"window": map[string]interface{}{"type": "string", "description": "Optional window title substring; omitted crops the foreground window"},
			"screen_index": map[string]interface{}{
				"type":        "integer",
				"description": "When set, capture that monitor instead of the focused window: 0=primary, 1=second, …. -1 stitches all monitors and is refused when the virtual desktop is larger than 8 megapixels.",
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
			"text-only models pass ref=eN. When a grounding model is configured, target (the element described in words) may replace x,y. " +
			"button=left|right (default left), count=1|2 (2=double-click).",
		Category: ToolCategoryBuiltin,
		Tags:     []string{"computer", "gui", "desktop", "click"},
		Priority: 8,
		Status:   RegToolAvailable,
		InputSchema: map[string]interface{}{
			"ref":    map[string]interface{}{"type": "string", "description": "Element ref from last computer_observe, e.g. e3"},
			"x":      map[string]interface{}{"type": "integer", "description": "Raw X (discouraged; needs allow_pixel_click)"},
			"y":      map[string]interface{}{"type": "integer", "description": "Raw Y (discouraged)"},
			"target": map[string]interface{}{"type": "string", "description": "Optional: the element to click described in words (e.g. \"the blue Send button\"); a grounding model finds it in the last screenshot. Ignored when ref or x,y is given."},
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
			"A successful type already includes a fresh foreground-window crop.",
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

	// --- computer_verify ---
	// Deterministic assertions through the Cua Driver: no model turn is
	// spent reading the screen back. Served by tools_computer_verify.go.
	registry.Register(RegisteredTool{
		Name:        "computer_verify",
		Description: cuVerifyDescription,
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "verify"},
		Priority:    6,
		Status:      RegToolAvailable,
		Required:    []string{"pid", "window_id", "expect"},
		InputSchema: map[string]interface{}{
			"pid":            map[string]interface{}{"type": "integer", "description": "Target process id"},
			"window_id":      map[string]interface{}{"type": "integer", "description": "Target window id"},
			"expect":         map[string]interface{}{"type": "array", "description": "Checks: {window:{exists|bounds}}, {element:{selector:{role,label_contains}, enabled, selected, value_equals}}"},
			"stable_samples": map[string]interface{}{"type": "integer", "description": "Consecutive agreeing samples (default 1)"},
			"timeout":        map[string]interface{}{"type": "string", "description": "Poll budget, e.g. 2s (default 2s)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleVerify(args)
		},
	})

	// --- computer_wait_until ---
	// A wait that costs no model turns: the driver polls, not the agent.
	registry.Register(RegisteredTool{
		Name:        "computer_wait_until",
		Description: cuWaitUntilDescription,
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"computer", "gui", "desktop", "wait"},
		Priority:    6,
		Status:      RegToolAvailable,
		InputSchema: map[string]interface{}{
			"title":     map[string]interface{}{"type": "string", "description": "Window title substring to wait for"},
			"app":       map[string]interface{}{"type": "string", "description": "App or process name instead of a title"},
			"gone":      map[string]interface{}{"type": "boolean", "description": "Wait for the window to disappear"},
			"on_screen": map[string]interface{}{"type": "boolean", "description": "Only count windows that are not minimized (default true)"},
			"timeout":   map[string]interface{}{"type": "string", "description": "How long to wait, e.g. 20s (default 20s, max 120s)"},
		},
		Source: "builtin:computer_use",
		Handler: func(args map[string]interface{}) string {
			return cuHandleWaitUntil(args)
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
			cuKeepPerceptionWarm()
			return computeruse.PlaybookFor(mode)
		},
	})

	// --- computer_find ---
	registry.Register(RegisteredTool{
		Name: "computer_find",
		Description: "Find on-screen text or UI elements by keyword (e.g. a contact name in an IM app). " +
			"Searches element labels AND raw OCR text in both vision and text modes; matches become " +
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
	// desktop effects, so it is served on managed turns exclusively by the
	// trusted host adapter (intent rule LabelComputerUse →
	// computer.control.desktop, published only when a desktop runtime is
	// enabled; a confident turn without one HostRejects as unmet). Every
	// computer_* entry is a complete user-facing entry point (there is no
	// merged dispatcher), so each one declares the shared outcome contract.
	for _, name := range []string{
		"computer_observe", "computer_click", "computer_type", "computer_key",
		"computer_scroll", "computer_select", "computer_scroll_into_view",
		"computer_drag", "computer_wait", "computer_focus", "computer_done",
		"computer_verify", "computer_wait_until",
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
	if msg := computerUseCompletedToolRefusal(tool); msg != "" {
		return msg
	}
	if tool != "computer_done" && tool != "computer_playbook" {
		if msg := cuRefuseAfterDesktopMessageSent(tool); msg != "" {
			return msg
		}
		if msg := cuAttemptFinished(tool); msg != "" {
			return msg
		}
	}
	if computerUseToolsEnabled() {
		return ""
	}
	return fmt.Sprintf("%s: Computer Use is disabled (computer_use_enabled=false). Enable it in Settings → Computer Use first.", tool)
}

// cuAttemptTerminal reports that this attempt can no longer inject.
// The reason does not name an observe tool. The managed surface rewrites
// that name into another observe call.
func cuAttemptTerminal(sess *computeruse.Session) (bool, string) {
	if sess == nil {
		return false, ""
	}
	if sess.InputDenied() {
		return true, "the foreground window refused input."
	}
	if max, exhausted := sess.StepsExhausted(); exhausted {
		return true, fmt.Sprintf("computer use max steps exceeded (%d).", max)
	}
	return false, ""
}

// cuAttemptFinished stops every computer tool except computer_done and
// computer_playbook once this attempt can no longer inject. The stale-label
// check says to observe again; that is how a spent budget or a refused
// input channel keeps the model looping. This answer is the same terminal
// line, and it does not spend another step.
func cuAttemptFinished(tool string) string {
	sess := cuSession()
	if sess == nil {
		return ""
	}
	if sess.InputDenied() {
		return fmt.Sprintf("%s: the foreground window refused input. Call computer_done.", tool)
	}
	if max, exhausted := sess.StepsExhausted(); exhausted {
		return fmt.Sprintf("%s: computer use max steps exceeded (%d). Call computer_done.", tool, max)
	}
	return ""
}

// cuInputDenied latches privilege isolation and tells the model to finish.
// The OS error stays in the audit. The tool result does not ask for another observe.
func cuInputDenied(sess *computeruse.Session, tool string, err error) string {
	if err == nil || !errors.Is(err, guiautomation.ErrInputDenied) {
		return ""
	}
	if sess != nil {
		sess.NoteInputDenied()
	}
	return fmt.Sprintf("%s: the foreground window refused input. Call computer_done.", tool)
}

// computerUsePolicyWindowHook overrides the live window under a point in tests.
// Production leaves it nil.
var computerUsePolicyWindowHook func(x, y int) string

// computerUseNamedWindowFn overrides the window computer_focus would activate.
// Production leaves it nil and resolves the visible window.
var computerUseNamedWindowFn func(hint string) (title string, ok bool)

// computerUseForegroundTitleFn and computerUseForegroundIDFn override the
// live foreground window in tests. Production leaves them nil.
var computerUseForegroundTitleFn func() string
var computerUseForegroundIDFn func() uint64

// computerUseFocusWindowFn overrides bringing a window to the foreground.
// Production leaves it nil.
var computerUseFocusWindowFn func(title string) error

// computerUseWindowAtPointFn overrides the window under a click in tests.
// Production leaves it nil.
var computerUseWindowAtPointFn func(x, y int) (id uint64, title string)

// computerUseAcceptsTextFn overrides the foreground keyboard-focus probe.
// Production leaves it nil and reads the GUI thread. Tests set it so they
// do not call SetForegroundWindow or depend on the desktop caret.
var computerUseAcceptsTextFn func() bool

// computerUseNoTextFocus is the tool result when keystrokes would not be
// inserted. The model has to click a text field before typing again.
const computerUseNoTextFocus = "keyboard focus is not a text control; computer_click the text field, then computer_type"

func computerUseKeyboardAcceptsText() bool {
	if computerUseAcceptsTextFn != nil {
		return computerUseAcceptsTextFn()
	}
	return accessibility.ForegroundAcceptsText()
}

// computerUseRaisedNote is the window just brought back from the background.
// The next tool result consumes it.
var computerUseRaisedNote string

// computerUseExplicitFocusTitle is the window computer_focus just activated
// when the following refresh did not replace the crop. A click that lands on
// a covering window must not set this: the halo follows the pointer, and that
// is not an intentional switch. Empty means the last crop is still the app.
var computerUseExplicitFocusTitle string

// cuFocusPolicyTitle is the window title computer_focus is about to activate.
// The request stands in when that window is not visible yet, so a security
// surface is refused before it appears.
func cuFocusPolicyTitle(hint string) string {
	hint = strings.TrimSpace(hint)
	if computerUseNamedWindowFn != nil {
		if title, ok := computerUseNamedWindowFn(hint); ok {
			if title = strings.TrimSpace(title); title != "" {
				return title
			}
		}
		return hint
	}
	if bounds, ok := accessibility.NamedWindowBounds(hint); ok {
		if title := strings.TrimSpace(bounds.Title); title != "" {
			return title
		}
	}
	return hint
}

func cuForegroundTitle() string {
	if computerUseForegroundTitleFn != nil {
		return computerUseForegroundTitleFn()
	}
	return accessibility.ForegroundWindowTitle()
}

func cuForegroundID() uint64 {
	if computerUseForegroundIDFn != nil {
		return computerUseForegroundIDFn()
	}
	return accessibility.ForegroundWindowID()
}

func cuFocusWindow(title string) error {
	if computerUseFocusWindowFn != nil {
		return computerUseFocusWindowFn(title)
	}
	return accessibility.FocusWindow(title)
}

func cuAppendRaisedNote(prefix string) string {
	note := strings.TrimSpace(computerUseRaisedNote)
	computerUseRaisedNote = ""
	if note == "" {
		return prefix
	}
	return prefix + fmt.Sprintf("\nraised %q to the foreground.", note)
}

// cuRestoreOperatedApp brings title forward when another window is covering
// the app being driven. It does not cover a security prompt, and it does not
// pull a stale crop back over a window computer_focus just activated.
// honorHalo keeps that intentional switch. A halo that followed a click onto
// the covering window is not that switch. The return is true only when the
// foreground title now matches title.
func cuRestoreOperatedApp(sess *computeruse.Session, title string, honorHalo bool) bool {
	title = strings.TrimSpace(title)
	if sess == nil || title == "" {
		return false
	}
	if paused, stopped := sess.ControlState(); paused || stopped {
		return false
	}
	fg := strings.TrimSpace(cuForegroundTitle())
	if computeruse.TitleBlocked(fg) || computeruse.TitleBlocked(cuFocusPolicyTitle(title)) {
		return false
	}
	if honorHalo && computerUseExplicitFocusHolds(title) {
		return false
	}
	if err := cuFocusWindow(title); err != nil {
		log.Printf("[computer-use] raise window %q: %v", title, err)
		return false
	}
	if !computeruse.CropIsForeground(title, cuForegroundTitle()) {
		return false
	}
	computerUseRaisedNote = title
	log.Printf("[computer-use] raised background window %q", title)
	noteComputerUseNamedWindow(title)
	return true
}

// computerUseExplicitFocusHolds reports that computer_focus activated a
// different app and the crop has not caught up yet. The same title does not
// hold: that window should still be raised when it later goes to the back.
func computerUseExplicitFocusHolds(crop string) bool {
	chosen := strings.TrimSpace(computerUseExplicitFocusTitle)
	if chosen == "" {
		return false
	}
	return !computeruse.WindowTitlesMatch(chosen, crop)
}

func clearComputerUseExplicitFocus() {
	computerUseExplicitFocusTitle = ""
}

func cuObservedCropTitle() string {
	last := cuSession().LastObserve()
	if last == nil {
		return ""
	}
	return strings.TrimSpace(last.Meta.CropTitle)
}

func cuWindowAtPoint(x, y int) (uint64, string) {
	if computerUseWindowAtPointFn != nil {
		return computerUseWindowAtPointFn(x, y)
	}
	return accessibility.WindowIDAtPoint(x, y), accessibility.WindowTitleAtPoint(x, y)
}

// cuPointMatchesOperatedCrop is false when title is a different app than the
// crop. An empty title is unknown and matches, same as WindowTitlesMatch.
func cuPointMatchesOperatedCrop(title string) bool {
	crop := cuObservedCropTitle()
	title = strings.TrimSpace(title)
	if crop == "" || title == "" {
		return true
	}
	return computeruse.WindowTitlesMatch(crop, title)
}

// noteComputerUseForegroundIfCrop moves the halo only when the foreground is
// still the operated crop. A type that leaves another app in front must not
// retarget the next raise.
func noteComputerUseForegroundIfCrop() {
	if !cuPointMatchesOperatedCrop(cuForegroundTitle()) {
		return
	}
	noteComputerUseOperatedWindow(cuForegroundID())
}

// cuPolicyWindowTitle resolves the window a click at (x,y) will actually land
// in, falling back to the foreground window title (what type/key target).
func cuPolicyWindowTitle(x, y int) string {
	if computerUsePolicyWindowHook != nil {
		return computerUsePolicyWindowHook(x, y)
	}
	if t := accessibility.WindowTitleAtPoint(x, y); t != "" {
		return t
	}
	return accessibility.ForegroundWindowTitle()
}

func cuPolicyWindowID(x, y int) uint64 {
	if id := accessibility.WindowIDAtPoint(x, y); id != 0 {
		return id
	}
	return accessibility.ForegroundWindowID()
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
	vision := computerUseLLMSupportsVision()
	windowHint := guiStrArg(args, "window", "")
	limit := guiIntArg(args, "limit", 10)

	sess := cuSession()
	// Reuse a just-taken observation (the playbook flow is observe → find) to
	// skip a second screenshot+YOLO+OCR cycle. Only without a window bias and
	// only while refs are still valid.
	obs := sess.LastObserve()
	reusable := obs != nil && windowHint == "" && sess.RefsValid() && time.Since(obs.ObservedAt) <= 2*time.Second
	var matches []computeruse.MarkedElement
	if reusable {
		matches = computeruse.FindMatches(obs, query, limit)
	}
	tookObserve := false
	needsOCR := reusable && vision && len(matches) == 0 && len(obs.OCRLines) == 0
	if !reusable || needsOCR {
		res := computerUseObserve(0, windowHint, true, true, vision)
		if !res.OK {
			return res.Message
		}
		tookObserve = true
		obs = sess.LastObserve()
		matches = computeruse.FindMatches(obs, query, limit)
	}
	if len(matches) == 0 {
		title := ""
		if obs != nil {
			title = obs.Meta.CropTitle
		}
		msg := fmt.Sprintf("computer_find: no match for %q. Suggestions: pass window=... to include accessibility elements; "+
			"use the app's own search box; or computer_scroll and find again.", query)
		return msg + computerUseRememberFind(title, windowHint, false)
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
		title := ""
		if obs != nil {
			title = obs.Meta.CropTitle
		}
		return b.String() + computerUseRememberFind(title, windowHint, true)
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
	title := ""
	if obs != nil {
		title = obs.Meta.CropTitle
	}
	return b.String() + computerUseRememberFind(title, windowHint, true)
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
func computerUseObserve(screenIdx int, windowHint string, withOCR, cropFocused bool, forceOCR ...bool) computerUseObserveResult {
	return computerUseObserveCaption(screenIdx, windowHint, withOCR, cropFocused, true, forceOCR...)
}

// computerUseObserveCaption is the observe implementation. Diagnostics/E2E
// pass caption=false so unlabeled-box HTTP does not inflate self-check time.
// forceOCR is used by computer_find on vision turns; ordinary vision observes
// stay screenshot-fast and leave OCR to the asynchronous label prefetch.
func computerUseObserveCaption(screenIdx int, windowHint string, withOCR, cropFocused, caption bool, forceOCR ...bool) (out computerUseObserveResult) {
	stitchLine := ""
	refusedStitch := false
	refusedW, refusedH := 0, 0
	if screenIdx < 0 {
		if w, h, px, ok := computerUseVirtualStitchSize(); ok && refuseStitchedCapture(screenIdx, px) {
			log.Printf("[computer-use] refused screen_index=-1 stitch %dx%d (%d px); capturing the foreground window", w, h, px)
			stitchLine = computerUseRememberExperience("", computeruse.NoteStitchRefused, false)
			refusedStitch = true
			refusedW, refusedH = w, h
			screenIdx = 0
			cropFocused = true
		}
	}
	cuKeepPerceptionWarm()
	rt := globalComputerUse
	out = computerUseObserveResult{
		TimingMs: map[string]int64{},
	}
	defer func() {
		out.Message = cuAppendRaisedNote(out.Message)
	}()
	totalStart := time.Now()

	if cropFocused && screenIdx >= 0 {
		explicitHint := strings.TrimSpace(windowHint)
		raiseHint := explicitHint
		if raiseHint == "" {
			raiseHint = cuObservedCropTitle()
			// computer_focus already chose the foreground. Shoot that window
			// so the crop catches up, instead of pulling the previous app
			// back over it.
			if raiseHint != "" && computerUseExplicitFocusHolds(raiseHint) {
				raiseHint = ""
			}
		}
		if raiseHint != "" && !computeruse.CropIsForeground(raiseHint, cuForegroundTitle()) {
			if !cuRestoreOperatedApp(cuSession(), raiseHint, explicitHint == "") {
				now := cuForegroundTitle()
				out.Error = "foreground"
				out.Message = fmt.Sprintf("computer_observe: could not bring %q to the foreground (now %q). Call computer_observe again.", raiseHint, now)
				return out
			}
		}
	}

	t0 := time.Now()
	cap, err := captureComputerUseScreen(screenIdx, windowHint, cropFocused)
	out.TimingMs["screenshot"] = time.Since(t0).Milliseconds()
	if err != nil {
		guide, action := cuScreenshotFailureGuidance(err)
		out.Error = err.Error()
		out.Guidance = guide
		out.Action = action
		out.TotalMs = time.Since(totalStart).Milliseconds()
		out.Message = computerUseAppendExperience(computerUseStitchNote(refusedStitch, refusedW, refusedH, false, computerUseCapture{})+fmt.Sprintf("computer_observe failed: screenshot: %v\nGuidance: %s", err, guide), stitchLine)
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
		// This crop is not sent as an image. Drop the previous vision
		// picture so a later target click cannot place points on it.
		clearComputerUseModelImages()
	}

	rt.mu.Lock()
	yolo := rt.yolo
	ocr := rt.ocr
	ocrSidecar := rt.ocrSidecar
	bridge := rt.bridge
	rt.mu.Unlock()
	ocrOnVision := len(forceOCR) > 0 && forceOCR[0]
	if ocrOnVision && ocrSidecar != nil {
		ocr = &taskOCRFromBrowser{inner: &configGatedOCRProvider{inner: ocrSidecar}}
	}

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
				if el.Name != "" && !computeruse.ExecutablePathTitle(el.Name) {
					windows = append(windows, el.Name)
				}
			}
		}
		hint := strings.TrimSpace(windowHint)
		if hint == "" {
			hint = accessibility.ForegroundWindowTitle()
		} else if best, ok := computeruse.BestWindowTitle(hint, windows); ok {
			// Resolve 记事本/微信/chrome to the real top-level title before UIA
			// walks the tree, so a shorter lookalike window is not enumerated.
			hint = best
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
	if withOCR && ocr != nil && (!vision || ocrOnVision) {
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
	computerUseTraceObserve()
	out.Message = computerUseAppendExperience(computerUseStitchNote(refusedStitch, refusedW, refusedH, true, cap)+appendOCRFailureHint(obs.TextForModel, out.OCRFailed, out.OCRError, screenIdx, out.Width, out.Height), stitchLine)
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
	noteComputerUseNamedWindow(windowHint)
	clearComputerUseExplicitFocus()
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

// computerUseAfterActionRefresh appends a crop of the window just operated.
// Tests replace it. The default skips the real desktop while the test binary
// is running so handoff tests stay fast. The window argument is the action's
// own title when the action named one.
var computerUseAfterActionRefresh = func(prefix, window string) string {
	if testing.Testing() {
		return prefix
	}
	return cuRefreshForeground(prefix, window)
}

// computerUseRefreshCapture takes the post-action crop. Tests replace it so
// a unit test never screenshots the desktop.
var computerUseRefreshCapture = captureComputerUseRefresh

func cuFinishAction(prefix string, window ...string) string {
	prefix = cuAppendRaisedNote(prefix)
	hint := ""
	if len(window) > 0 {
		hint = window[0]
	}
	refresh := computerUseAfterActionRefresh
	if refresh == nil {
		return prefix
	}
	return refresh(prefix, hint)
}

func computerUseStitchNote(refused bool, w, h int, captured bool, cap computerUseCapture) string {
	if !refused {
		return ""
	}
	return computerUseStitchRefusalNote(w, h, captured, cap)
}

// cuOperatedWindow is the window this action addressed. An explicit title
// wins. Otherwise a foreground that belongs to another app is not the crop
// the action was checked against: a click that lands on a covering window
// must not make the next refresh operate that window.
func cuOperatedWindow(explicit string) string {
	if title := strings.TrimSpace(explicit); title != "" {
		return title
	}
	crop := cuObservedCropTitle()
	fg := strings.TrimSpace(cuForegroundTitle())
	if crop != "" && fg != "" && !computeruse.WindowTitlesMatch(crop, fg) {
		return crop
	}
	if fg != "" {
		return fg
	}
	return crop
}

// captureComputerUseRefresh reuses the settle shot when it is already the
// operated window. Otherwise it captures that window once. It does not run
// YOLO, accessibility, or a full computer_observe.
func captureComputerUseRefresh(window string) (computerUseCapture, error) {
	if shot, ok := takeComputerUseSettle(window); ok {
		meta := shot.meta
		w, h := meta.Width, meta.Height
		if iw, ih, ok := decodeImageSizeB64(shot.png); ok && iw > 0 && ih > 0 {
			meta = computeruse.RefreshGeometryFromImage(meta, iw, ih)
			w, h = iw, ih
		}
		return computerUseCapture{PNG: shot.png, Meta: meta, Width: w, Height: h}, nil
	}
	return captureComputerUseScreen(0, window, true)
}

// cuRefreshForeground captures the window just operated and describes that
// crop. A monitor fallback names the screen that was actually captured.
func cuRefreshForeground(prefix, windowHint string) (out string) {
	defer func() {
		out = cuAppendRaisedNote(out)
	}()
	window := cuOperatedWindow(windowHint)
	if window != "" && !computeruse.CropIsForeground(window, cuForegroundTitle()) {
		// The shot is the window rectangle. If this app is still behind
		// another window, that rectangle is the covering window's pixels.
		if !cuRestoreOperatedApp(cuSession(), window, strings.TrimSpace(windowHint) == "") {
			cuSession().InvalidateLabels()
			cuCancelLabelPrefetch()
			clearComputerUseModelImages()
			line := computerUseRememberExperience(computeruse.ExperienceAppID(window), computeruse.NoteRefreshFailed, false)
			return computerUseAppendExperience(prefix+"\nrefresh failed; call computer_observe again", line)
		}
		// Settle ran before this raise, so its pixels are the covering window.
		clearComputerUseSettle()
	}
	cap, err := computerUseRefreshCapture(window)
	if err != nil || strings.TrimSpace(cap.PNG) == "" {
		cuSession().InvalidateLabels()
		cuCancelLabelPrefetch()
		// The previous picture is not the screen we failed to capture.
		clearComputerUseModelImages()
		line := computerUseRememberExperience(computeruse.ExperienceAppID(window), computeruse.NoteRefreshFailed, false)
		return computerUseAppendExperience(prefix+"\nrefresh failed; call computer_observe again", line)
	}
	if cap.Width <= 0 || cap.Height <= 0 {
		if w, h, ok := decodeImageSizeB64(cap.PNG); ok {
			cap.Width, cap.Height = w, h
		}
	}
	if cap.Meta.Width <= 0 {
		cap.Meta.Width = cap.Width
	}
	if cap.Meta.Height <= 0 {
		cap.Meta.Height = cap.Height
	}
	noteComputerUseNamedWindow(window)
	return cuDescribeRefresh(prefix, cap)
}

func cuDescribeRefresh(prefix string, cap computerUseCapture) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('\n')
	title := strings.TrimSpace(cap.Meta.CropTitle)
	if title == "" {
		fmt.Fprintf(&b, "refresh: window crop failed. This image is a full monitor, not the foreground window. Captured screen_index=%d", cap.Meta.ScreenIndex)
		if cap.Width > 0 && cap.Height > 0 {
			fmt.Fprintf(&b, " (%dx%d)", cap.Width, cap.Height)
		}
		b.WriteByte('.')
		if list := computerUseDisplayList(); list != "" {
			b.WriteString(" Displays: ")
			b.WriteString(list)
			b.WriteByte('.')
		}
		b.WriteString(" Pass window=<app title> and call computer_observe again before the next click, scroll, or drag.\n")
		cuSession().InvalidateLabels()
		cuCancelLabelPrefetch()
		clearComputerUseModelImages()
		return b.String()
	} else {
		fmt.Fprintf(&b, "refresh: crop of %q", title)
		if cap.Width > 0 && cap.Height > 0 {
			fmt.Fprintf(&b, " (%dx%d)", cap.Width, cap.Height)
		}
		b.WriteString(". This is the window just operated.\n")
		clearComputerUseExplicitFocus()
	}
	if computerUseLLMSupportsVision() {
		modelPNG, vw, vh := prepareVisionScreenshot(cap.PNG)
		meta := cap.Meta
		if meta.ScaleFactor <= 0 {
			meta.ScaleFactor = 1
		}
		meta.VisionWidth, meta.VisionHeight = vw, vh
		cuSession().ApplyRefreshCapture(meta, cap.PNG)
		// Read labels while the model looks at this crop. The next pixel
		// click or Enter reuses the result instead of starting another OCR.
		cuPrefetchCropLabels(cap.PNG)
		setPendingComputerUseModelImage(modelPNG)
		if vw > 0 && vh > 0 {
			fmt.Fprintf(&b, "image=%dx%d (click x,y in this image; mapped to screen)\n", vw, vh)
		}
		b.WriteString("screenshot: attached in the following message. Look at the image.\n")
		return b.String()
	}
	// Text mode does not send this crop. The previous vision picture
	// belongs to another image, so a target click must not use it.
	clearComputerUseModelImages()
	rt := globalComputerUse
	rt.mu.Lock()
	ocr := rt.ocr
	rt.mu.Unlock()
	if ocr == nil {
		cuSession().ApplyRefreshCapture(cap.Meta, cap.PNG)
		b.WriteString("No new element list on this crop. Call computer_observe for refs.\n")
		return b.String()
	}
	res, err := ocr.Recognize(cap.PNG)
	if err != nil || !computeruse.OCRLinesUsable(res) {
		cuSession().ApplyRefreshCapture(cap.Meta, cap.PNG)
		b.WriteString("refresh: OCR returned no text. Call computer_observe for refs.\n")
		return b.String()
	}
	els := computeruse.OCRClickElements(res, 24)
	obs := cuSession().CommitObserve(cap.Meta, nil, els, res, cap.PNG)
	if obs != nil {
		fmt.Fprintf(&b, "ocr_refs=%d\n", len(obs.Elements))
		shown := len(obs.Elements)
		if shown > 24 {
			shown = 24
		}
		for i := 0; i < shown; i++ {
			el := obs.Elements[i]
			fmt.Fprintf(&b, "  %s %q\n", el.Ref, el.Name)
		}
		if obs.OCRExcerpt != "" {
			b.WriteString("ocr_excerpt: ")
			b.WriteString(obs.OCRExcerpt)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// cuKeepPerceptionWarm restarts the idle-unload timer for models that are
// already loaded. It does not load YOLO or OCR. A vision turn skips both,
// and an idle unload between warmup and the first observe would otherwise
// drop them five minutes after warmup.
func cuKeepPerceptionWarm() {
	rt := globalComputerUse
	rt.mu.Lock()
	yolo := rt.yolo
	ocr := rt.ocrSidecar
	rt.mu.Unlock()
	if yolo != nil && yolo.Loaded() {
		_ = yolo.Warm()
	}
	if ocr != nil {
		ocr.Touch()
	}
}

// computerUseLocalOCR is the already-loaded PP-OCR engine. Label checks must
// not call the vision-LLM OCR wrapper: that sends the crop through another
// chat completion, and that model is allowed to return boxes of 0,0,0,0.
type computerUseLocalOCR interface {
	Ready() bool
	Recognize(pngBase64 string) ([]browser.OCRResult, error)
}

// readLoadedCropLabels reads a crop with the local engine when it is already
// in memory. A nil local engine uses fallback, which tests install. A local
// engine that is not loaded does not fall through to the vision wrapper and
// does not cold-load.
func readLoadedCropLabels(local computerUseLocalOCR, fallback taskengine.OCRProvider, png string) ([]taskengine.OCRResult, bool) {
	if local != nil {
		if !local.Ready() {
			return nil, false
		}
		got, err := local.Recognize(png)
		if err != nil || len(got) == 0 {
			return nil, false
		}
		raw := make([]taskengine.OCRResult, len(got))
		for i, r := range got {
			raw[i] = taskengine.OCRResult{Text: r.Text, Confidence: r.Confidence, BBox: r.BBox}
		}
		return boxedOCRLines(raw)
	}
	if fallback == nil || !fallback.IsAvailable() {
		return nil, false
	}
	res, err := fallback.Recognize(png)
	if err != nil || len(res) == 0 {
		return nil, false
	}
	return boxedOCRLines(res)
}

// boxedOCRLines keeps lines that can be hit-tested. A zero box must not count
// as a restored label: Enter would then see an empty surface and go through.
func boxedOCRLines(lines []taskengine.OCRResult) ([]taskengine.OCRResult, bool) {
	out := make([]taskengine.OCRResult, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.Text) == "" || line.BBox[2] <= 0 || line.BBox[3] <= 0 {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// computerUseCropLabelRead overrides the crop label read in tests.
// Production leaves it nil.
var computerUseCropLabelRead func(png string) ([]taskengine.OCRResult, bool)

// cropLabelPrefetch is a local OCR of the vision refresh crop. It runs while
// the model reads that image, so the next pixel click or Enter does not start
// a second read. A newer refresh or a failed capture bumps gen and drops it.
type cropLabelPrefetch struct {
	mu    sync.Mutex
	gen   uint64
	png   string
	ready chan struct{}
	lines []taskengine.OCRResult
	ran   bool
}

var cropLabels cropLabelPrefetch

func cuCancelLabelPrefetch() {
	cropLabels.mu.Lock()
	cropLabels.gen++
	cropLabels.png = ""
	cropLabels.lines = nil
	cropLabels.ran = false
	cropLabels.ready = nil
	cropLabels.mu.Unlock()
}

func cuPrefetchCropLabels(png string) {
	png = strings.TrimSpace(png)
	if png == "" {
		cuCancelLabelPrefetch()
		return
	}
	ch := make(chan struct{})
	cropLabels.mu.Lock()
	cropLabels.gen++
	gen := cropLabels.gen
	cropLabels.png = png
	cropLabels.lines = nil
	cropLabels.ran = false
	cropLabels.ready = ch
	cropLabels.mu.Unlock()
	// Capture the test hook now. Clearing it while this read runs must not
	// fall through to the vision wrapper.
	read := computerUseCropLabelRead
	go func() {
		defer close(ch)
		var lines []taskengine.OCRResult
		var ok bool
		if read != nil {
			lines, ok = read(png)
		} else {
			lines, ok = cuReadCropLabels(png)
		}
		cropLabels.mu.Lock()
		if cropLabels.gen == gen {
			if ok {
				cropLabels.lines = lines
			}
			cropLabels.ran = true
			cropLabels.ready = nil
		}
		cropLabels.mu.Unlock()
	}()
}

// cuConsumePrefetch waits for the read started for this exact crop.
// covered is false when no read was started for png, so the caller reads it.
func cuConsumePrefetch(png string) (lines []taskengine.OCRResult, covered bool) {
	cropLabels.mu.Lock()
	if cropLabels.png != png {
		cropLabels.mu.Unlock()
		return nil, false
	}
	gen := cropLabels.gen
	ch := cropLabels.ready
	if ch == nil {
		lines, covered = cropLabels.lines, cropLabels.ran
		cropLabels.mu.Unlock()
		return lines, covered
	}
	cropLabels.mu.Unlock()
	<-ch
	cropLabels.mu.Lock()
	defer cropLabels.mu.Unlock()
	if cropLabels.gen != gen || cropLabels.png != png {
		return nil, false
	}
	return cropLabels.lines, cropLabels.ran
}

func cuReadCropLabels(png string) ([]taskengine.OCRResult, bool) {
	if computerUseCropLabelRead != nil {
		return computerUseCropLabelRead(png)
	}
	rt := globalComputerUse
	rt.mu.Lock()
	ocr := rt.ocr
	var local computerUseLocalOCR
	if rt.ocrSidecar != nil {
		local = rt.ocrSidecar
	}
	rt.mu.Unlock()
	return readLoadedCropLabels(local, ocr, png)
}

// cuEnsureCurrentLabels reads the stored crop once when pay labels are stale.
// It uses the loaded local OCR engine, not the vision-LLM wrapper. A monitor
// fallback is not read. A vision refresh may already have started the read.
// This does not count as computer_observe.
func cuEnsureCurrentLabels(sess *computeruse.Session) {
	if sess == nil || sess.LabelsCurrent() {
		return
	}
	png, ok := sess.StaleCropScreenshot()
	if !ok {
		return
	}
	if lines, covered := cuConsumePrefetch(png); covered {
		if len(lines) > 0 {
			sess.AdoptOCRLabels(lines)
		}
		return
	}
	res, ok := cuReadCropLabels(png)
	if !ok {
		return
	}
	sess.AdoptOCRLabels(res)
}

// cuPostActionSettle waits for the UI to settle after an action.
// Windows and macOS poll a short visual idle wait (native capture is cheap).
// Linux sleeps briefly — X11 screenshots are too slow to poll after every click.
func cuPostActionSettle() {
	// The refresh hook is already skipped under test. Settle must not capture
	// the desktop either.
	if testing.Testing() {
		return
	}
	time.Sleep(80 * time.Millisecond)
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return
	}
	obs := guiautomation.NewGUIStateObserver(nil, nil, func() (string, error) {
		return captureSettleScreenshot()
	}, nil)
	_ = obs.WaitForIdle(300*time.Millisecond, 80*time.Millisecond)
}

// cuPersonHandoff waits while the person finishes a pay or secret step.
// The fallback is the immediate refusal used when the desktop has no handoff UI.
// Neither string contains the text the model tried to type.
func cuPersonHandoff(reason, title, fallback string) string {
	err := computeruse.OfferDesktopHandoff(reason, title)
	if err == nil || errors.Is(err, computeruse.ErrDesktopHandoffOff) {
		return fallback
	}
	return err.Error()
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
	if computeruse.ExecutablePathTitle(window) {
		msg := fmt.Sprintf("no application window matching %q", window)
		sess.RecordAction("focus", detail, false, msg, false)
		emitComputerUseActionUI("focus", detail, false, msg)
		return "computer_focus failed: " + msg
	}
	// The title we would activate, or the request when that window is not
	// visible yet. A security surface must not be brought forward.
	if err := sess.CheckClickPolicy(0, 0, cuFocusPolicyTitle(window)); err != nil {
		sess.RecordAction("focus", detail, false, err.Error(), false)
		emitComputerUseActionUI("focus", detail, false, err.Error())
		return fmt.Sprintf("computer_focus blocked: %v", err)
	}
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
	computerUseExplicitFocusTitle = strings.TrimSpace(window)
	noteComputerUseNamedWindow(window)
	emitComputerUseActionUI("focus", detail, true, "")
	computerUseTraceFocus()
	cuPostActionSettle()
	return cuFinishAction(fmt.Sprintf("focused window matching %q", window), window)
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
	r := strings.ToLower(strings.TrimSpace(role))
	r = strings.TrimPrefix(r, "controltype.")
	switch r {
	case "button", "splitbutton", "edit", "textfield", "textarea",
		"menu", "menuitem", "menubar",
		"link", "hyperlink",
		"checkbox", "tab", "tabitem",
		"listitem", "treeitem", "dataitem",
		"combobox", "combo",
		"radio", "radiobutton",
		"slider", "spinner",
		"document":
		return true
	default:
		// Exact roles only. Substring checks treated Table as a tab and let
		// static text or panes become click targets.
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

// cuNoteClickTarget remembers the control a click landed on so a later
// Enter or untargeted type knows which field has the caret.
func cuNoteClickTarget(sess *computeruse.Session, mark *computeruse.MarkedElement, x, y int, window string, windowID uint64) {
	if sess == nil {
		return
	}
	name := ""
	kind := ""
	// A ref already knows its window from observe. Do not replace an empty
	// one with the live title under the point: that point can sit in another
	// window while the control itself was not attributed.
	win := ""
	id := uint64(0)
	if mark != nil {
		name = mark.Name
		kind = mark.Type
		win = mark.Window
		id = mark.WindowID
	} else {
		win = window
		id = windowID
	}
	if strings.TrimSpace(name) == "" {
		name = sess.ElementNameAtScreen(x, y)
		kind = ""
	}
	if strings.TrimSpace(name) == "" {
		name = sess.OCRTextAtScreen(x, y)
		kind = ""
	}
	sess.NoteActuatedID(name, win, kind, id)
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
	target := strings.TrimSpace(guiStrArg(args, "target", ""))
	// An explicit pay name needs no point. Hand off before grounding or a
	// label read, including when this crop's labels are already stale.
	if refusal := browser.DesktopPayClickRefusal(target); refusal != "" {
		detail := fmt.Sprintf("target=%q", target)
		sess.RecordAction("click", detail, false, "checkout", false)
		emitComputerUseActionUI("click", detail, false, "checkout")
		return "computer_click blocked: " + cuPersonHandoff("checkout", target, refusal)
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
		// The stored crop cannot be labeled again (monitor fallback or a
		// failed capture). Refuse before a grounding model places the target
		// on the previous screenshot.
		if cuStalePointerGeometry(sess) {
			return cuStaleLabelRefusal(sess, "computer_click", "click")
		}
		x = guiIntArg(args, "x", 0)
		y = guiIntArg(args, "y", 0)
		if target != "" && !cuHasArg(args, "x") && !cuHasArg(args, "y") {
			gx, gy, note := cuGroundTarget(target)
			if note != "" {
				return "computer_click: " + note
			}
			x, y = gx, gy
		}
		x, y = sess.MapVisionClick(x, y)
		x, y = sess.MapCaptureClick(x, y)
		detail = fmt.Sprintf("pixel (%d,%d) button=%s count=%d", x, y, button, count)
	}
	if policyTitle == "" {
		policyTitle = cuPolicyWindowTitle(x, y)
	}
	// The ref's own name is checked before the label read. The target
	// argument was already refused above, before grounding.
	payNames := make([]string, 0, 2)
	if mark != nil && strings.TrimSpace(mark.Name) != "" {
		payNames = append(payNames, mark.Name)
	}
	if target != "" {
		payNames = append(payNames, target)
	}
	for _, payName := range payNames {
		refusal := browser.DesktopPayClickRefusal(payName)
		if refusal == "" {
			continue
		}
		sess.RecordAction("click", detail, false, "checkout", false)
		emitComputerUseActionUI("click", detail, false, "checkout")
		return "computer_click blocked: " + cuPersonHandoff("checkout", payName, refusal)
	}
	if ref == "" {
		cuEnsureCurrentLabels(sess)
	}
	// The previous screen's boxes were dropped. A pixel click with no new
	// labels cannot see a pay button that appeared after the last action.
	if ref == "" && !sess.LabelsCurrent() {
		return cuStaleLabelRefusal(sess, "computer_click", "click")
	}
	hits := make([]string, 0, 2)
	if hit := sess.ElementNameAtScreen(x, y); hit != "" {
		hits = append(hits, hit)
	}
	if hit := sess.OCRTextAtScreen(x, y); hit != "" {
		hits = append(hits, hit)
	}
	for _, payName := range hits {
		refusal := browser.DesktopPayClickRefusal(payName)
		if refusal == "" {
			continue
		}
		sess.RecordAction("click", detail, false, "checkout", false)
		emitComputerUseActionUI("click", detail, false, "checkout")
		return "computer_click blocked: " + cuPersonHandoff("checkout", payName, refusal)
	}
	// Policy before BeginAction: a blocked click must not consume step budget.
	// The stored window and the live window under the point are both checked.
	if msg := cuPointerWindowRefusal(sess, "computer_click", "click", detail, cuDragEnd{x: x, y: y, mark: mark}); msg != "" {
		return msg
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_click: " + msg
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
		if msg := cuInputDenied(sess, "computer_click", err); msg != "" {
			return msg
		}
		return fmt.Sprintf("computer_click failed: %v", err)
	}
	sess.RecordAction("click", detail+" via "+strategy, true, "", true)
	noteComputerUsePointWindow(x, y)
	policyID := uint64(0)
	if mark == nil {
		policyID = cuPolicyWindowID(x, y)
	}
	cuNoteClickTarget(sess, mark, x, y, policyTitle, policyID)
	markComputerUseSessionActive()
	emitComputerUseActionUI("click", detail+" via "+strategy, true, "")
	cuPostActionSettle()
	msg := cuFinishAction(fmt.Sprintf("clicked %s via %s", detail, strategy))
	if button == "left" && count == 1 && noteDesktopMessageClickSubmitted(append(payNames, hits...)...) {
		log.Printf("[computer-use] desktop message submitted by Send click; further actions stop")
		msg += "\n" + desktopMessageStopText
	}
	return msg
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
	foreground := accessibility.ForegroundWindowTitle()
	if err := sess.CheckClickPolicy(0, 0, foreground); err != nil {
		sess.RecordAction("type", detail, false, err.Error(), false)
		emitComputerUseActionUI("type", detail, false, err.Error())
		return fmt.Sprintf("computer_type blocked: %v", err)
	}
	if crop, stale := sess.ObserveDocumentStale(foreground, accessibility.ForegroundWindowID()); stale {
		sess.RecordAction("type", "stale-document", false, "stale-document", false)
		emitComputerUseActionUI("type", "stale-document", false, "stale-document")
		return fmt.Sprintf("computer_type: foreground document changed (observed %q, now %q); call computer_observe again", crop, foreground)
	}
	var typeEl *computeruse.MarkedElement
	var typeX, typeY int
	if ref != "" {
		x, y, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			return fmt.Sprintf("computer_type: %v", err)
		}
		typeEl, typeX, typeY = el, x, y
		detail = fmt.Sprintf("ref=%s name=%q chars=%d", el.Ref, el.Name, len([]rune(text)))
		if refusal := browser.DesktopSecretTypeRefusal(el.Name); refusal != "" {
			sess.RecordAction("type", detail, false, "secret", false)
			emitComputerUseActionUI("type", detail, false, "secret")
			return "computer_type blocked: " + cuPersonHandoff("secret", el.Name, refusal)
		}
		// The focus click is a click, and a semantic type actuates the same
		// control. A pay control must not be invoked by either path.
		if refusal := browser.DesktopPayClickRefusal(el.Name); refusal != "" {
			sess.RecordAction("type", detail, false, "checkout", false)
			emitComputerUseActionUI("type", detail, false, "checkout")
			return "computer_type blocked: " + cuPersonHandoff("checkout", el.Name, refusal)
		}
		for _, hit := range []string{sess.ElementNameAtScreen(x, y), sess.OCRTextAtScreen(x, y)} {
			refusal := browser.DesktopPayClickRefusal(hit)
			if refusal == "" {
				continue
			}
			sess.RecordAction("type", detail, false, "checkout", false)
			emitComputerUseActionUI("type", detail, false, "checkout")
			return "computer_type blocked: " + cuPersonHandoff("checkout", hit, refusal)
		}
		// The focus click and a semantic type both use this point. A stored
		// window must not hide the live window, and the check is before
		// BeginAction so a block does not spend a step.
		if msg := cuPointerWindowRefusal(sess, "computer_type", "type", detail, cuDragEnd{x: x, y: y, mark: el}); msg != "" {
			return msg
		}
	} else {
		// Untargeted typing goes to the caret. The last control the agent
		// clicked is that caret when the window title is not itself a dialog.
		if name, _ := sess.ActuatedForKey(foreground, accessibility.ForegroundWindowID()); name != "" {
			if refusal := browser.DesktopSecretTypeRefusal(name); refusal != "" {
				sess.RecordAction("type", "secret", false, "secret", false)
				emitComputerUseActionUI("type", "secret", false, "secret")
				return "computer_type blocked: " + cuPersonHandoff("secret", name, refusal)
			}
		}
		if computeruse.DesktopHandoffEnabled() {
			if refusal := browser.DesktopSecretSurfaceRefusal(foreground); refusal != "" {
				sess.RecordAction("type", "secret", false, "secret", false)
				emitComputerUseActionUI("type", "secret", false, "secret")
				return "computer_type blocked: " + cuPersonHandoff("secret", foreground, refusal)
			}
		}
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_type: " + msg
	}
	// Untargeted typing pastes at the caret. The foreground window title is
	// not that caret: a browser frame and a thread with no focus window drop
	// the paste, and SendInput still returns success. Refuse before the step
	// is spent, and do not send the keys.
	if ref == "" && !computerUseKeyboardAcceptsText() {
		sess.RecordAction("type", detail, false, "no-text-focus", false)
		emitComputerUseActionUI("type", detail, false, "no-text-focus")
		return "computer_type: " + computerUseNoTextFocus
	}
	if err := sess.BeginAction("type", detail); err != nil {
		return fmt.Sprintf("computer_type: %v", err)
	}
	strategy := "pixel"
	if typeEl != nil {
		x, y, el := typeX, typeY, typeEl
		if cuTrySemanticType(el, x, y, text) {
			strategy = "a11y"
		} else {
			if err := input.Click(x, y); err != nil {
				sess.RecordAction("type", detail, false, err.Error(), false)
				if msg := cuInputDenied(sess, "computer_type", err); msg != "" {
					return msg
				}
				return fmt.Sprintf("computer_type focus click failed: %v", err)
			}
			time.Sleep(80 * time.Millisecond)
			// The click can foreground a blocked window that was not under the point before.
			if err := sess.CheckClickPolicy(x, y, cuPolicyWindowTitle(x, y)); err != nil {
				sess.RecordAction("type", detail, false, err.Error(), false)
				emitComputerUseActionUI("type", detail, false, err.Error())
				return fmt.Sprintf("computer_type blocked: %v", err)
			}
		}
	}
	if strategy != "a11y" {
		// A ref click was supposed to move keyboard focus onto the control.
		// Type only after that focus can insert text. A ValuePattern write
		// already finished above and does not use this path.
		if !computerUseKeyboardAcceptsText() {
			sess.RecordAction("type", detail, false, "no-text-focus", false)
			emitComputerUseActionUI("type", detail, false, "no-text-focus")
			return "computer_type: " + computerUseNoTextFocus
		}
		if err := input.Type(text); err != nil {
			sess.RecordAction("type", detail, false, err.Error(), false)
			emitComputerUseActionUI("type", detail, false, err.Error())
			if msg := cuInputDenied(sess, "computer_type", err); msg != "" {
				return msg
			}
			return fmt.Sprintf("computer_type failed: %v", err)
		}
	}
	sess.RecordAction("type", detail+" via "+strategy, true, "", true)
	messageTyped := noteDesktopMessageTyped(text)
	noteComputerUseForegroundIfCrop()
	if typeEl != nil {
		sess.NoteActuatedID(typeEl.Name, typeEl.Window, typeEl.Type, typeEl.WindowID)
	}
	markComputerUseSessionActive()
	emitComputerUseActionUI("type", detail+" via "+strategy, true, "")
	computerUseTraceType()
	cuPostActionSettle()
	msg := cuFinishAction(fmt.Sprintf("typed %s via %s", detail, strategy))
	if messageTyped {
		msg += "\n" + desktopMessageTypedStopText
	}
	if computerUseLLMSupportsVision() {
		msg = computerUseAppendExperience(msg, computerUseRememberVisionSearch(cuOperatedWindow("")))
	}
	return msg
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
	foreground := accessibility.ForegroundWindowTitle()
	if err := sess.CheckClickPolicy(0, 0, foreground); err != nil {
		sess.RecordAction("key", detail, false, err.Error(), false)
		emitComputerUseActionUI("key", detail, false, err.Error())
		return fmt.Sprintf("computer_key blocked: %v", err)
	}
	if computeruse.DesktopHandoffEnabled() {
		if refusal := browser.DesktopSecretSurfaceRefusal(foreground); refusal != "" {
			sess.RecordAction("key", "secret", false, "secret", false)
			emitComputerUseActionUI("key", "secret", false, "secret")
			return "computer_key blocked: " + cuPersonHandoff("secret", foreground, refusal)
		}
	}
	fgID := accessibility.ForegroundWindowID()
	// The same window can navigate. Enter and Space must not use the previous
	// page's labels, which often have no pay button yet.
	if browser.DesktopSubmitKey(detail) {
		if crop, stale := sess.ObserveDocumentStale(foreground, fgID); stale {
			sess.RecordAction("key", "stale-document", false, "stale-document", false)
			emitComputerUseActionUI("key", "stale-document", false, "stale-document")
			return fmt.Sprintf("computer_key: foreground document changed (observed %q, now %q); call computer_observe again", crop, foreground)
		}
		focused, kind := sess.ActuatedForKey(foreground, fgID)
		if reason, title, refusal := browser.DesktopPayKeyRefusal(detail, focused, kind, nil); refusal != "" {
			sess.RecordAction("key", reason, false, reason, false)
			emitComputerUseActionUI("key", reason, false, reason)
			return "computer_key blocked: " + cuPersonHandoff(reason, title, refusal)
		}
		cuEnsureCurrentLabels(sess)
		if !sess.LabelsCurrent() {
			return cuStaleLabelRefusal(sess, "computer_key", "key")
		}
	}
	focused, kind := sess.ActuatedForKey(foreground, fgID)
	if reason, title, refusal := browser.DesktopPayKeyRefusal(detail, focused, kind, sess.KeySurfaceLabelsFor(foreground, fgID)); refusal != "" {
		sess.RecordAction("key", reason, false, reason, false)
		emitComputerUseActionUI("key", reason, false, reason)
		return "computer_key blocked: " + cuPersonHandoff(reason, title, refusal)
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_key: " + msg
	}
	// Paste is typing. Enter and arrows still go to the window.
	if keysInsertText(keys) && !computerUseKeyboardAcceptsText() {
		sess.RecordAction("key", detail, false, "no-text-focus", false)
		emitComputerUseActionUI("key", detail, false, "no-text-focus")
		return "computer_key: " + computerUseNoTextFocus
	}
	if err := sess.BeginAction("key", detail); err != nil {
		return fmt.Sprintf("computer_key: %v", err)
	}
	if err := input.KeyCombo(keys...); err != nil {
		sess.RecordAction("key", detail, false, err.Error(), false)
		emitComputerUseActionUI("key", detail, false, err.Error())
		if msg := cuInputDenied(sess, "computer_key", err); msg != "" {
			return msg
		}
		return fmt.Sprintf("computer_key failed: %v", err)
	}
	sess.RecordAction("key", detail, true, "", true)
	noteComputerUseForegroundIfCrop()
	markComputerUseSessionActive()
	emitComputerUseActionUI("key", detail, true, "")
	cuPostActionSettle()
	msg := cuFinishAction(fmt.Sprintf("pressed %s", detail))
	if noteDesktopMessageSubmitted(detail) {
		log.Printf("[computer-use] desktop message submitted; further actions stop")
		msg += "\n" + desktopMessageStopText
	}
	return msg
}

// keysInsertText reports a paste chord. Ctrl+V and Ctrl+Shift+V paste.
// Shift+Insert pastes. Enter, Ctrl+Enter, and Ctrl+A do not.
func keysInsertText(keys []string) bool {
	if len(keys) == 0 || len(keys) > 3 {
		return false
	}
	var v, insert, ctrl, shift, other bool
	for _, k := range keys {
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "v":
			v = true
		case "insert":
			insert = true
		case "ctrl", "control":
			ctrl = true
		case "shift":
			shift = true
		default:
			other = true
		}
	}
	if other {
		return false
	}
	if v && ctrl {
		return true
	}
	return insert && shift && !ctrl && !v
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
	var end cuDragEnd
	if ref != "" {
		cx, cy, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			return fmt.Sprintf("computer_scroll: %v", err)
		}
		end = cuDragEnd{x: cx, y: cy, mark: el}
	} else if cuStalePointerGeometry(sess) {
		// Pixel scroll and the text-mode center both map through the stored
		// crop. A monitor fallback keeps the previous geometry and drops the
		// picture, so the wheel would land on the wrong screen.
		return cuStaleLabelRefusal(sess, "computer_scroll", "scroll")
	} else if sess.AllowPixelClick() {
		x := guiIntArg(args, "x", 0)
		y := guiIntArg(args, "y", 0)
		x, y = sess.MapVisionClick(x, y)
		x, y = sess.MapCaptureClick(x, y)
		end = cuDragEnd{x: x, y: y, pixel: true}
	} else {
		// Default to screen center of last observe meta if available.
		if last := sess.LastObserve(); last != nil && last.Meta.Width > 0 {
			x := last.Meta.Width / 2
			y := last.Meta.Height / 2
			x, y = sess.MapCaptureClick(x, y)
			end = cuDragEnd{x: x, y: y}
		} else {
			return "computer_scroll: provide ref=eN from computer_observe (or enable pixel click)"
		}
	}
	detail := fmt.Sprintf("at (%d,%d) delta=%d,%d", end.x, end.y, dx, dy)
	if msg := cuPointerWindowRefusal(sess, "computer_scroll", "scroll", detail, end); msg != "" {
		return msg
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_scroll: " + msg
	}
	if err := sess.BeginAction("scroll", detail); err != nil {
		return fmt.Sprintf("computer_scroll: %v", err)
	}
	if err := input.Scroll(end.x, end.y, dx, dy); err != nil {
		sess.RecordAction("scroll", detail, false, err.Error(), false)
		emitComputerUseActionUI("scroll", detail, false, err.Error())
		if msg := cuInputDenied(sess, "computer_scroll", err); msg != "" {
			return msg
		}
		return fmt.Sprintf("computer_scroll failed: %v", err)
	}
	sess.RecordAction("scroll", detail, true, "", true)
	noteComputerUsePointWindow(end.x, end.y)
	markComputerUseSessionActive()
	emitComputerUseActionUI("scroll", detail, true, "")
	cuPostActionSettle()
	return cuFinishAction(fmt.Sprintf("scrolled %s", detail))
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
	// Select activates the ref, and the pixel fallback is a click. A pay
	// control must not be invoked by either path.
	if refusal := browser.DesktopPayClickRefusal(mark.Name); refusal != "" {
		sess.RecordAction("select", detail, false, "checkout", false)
		emitComputerUseActionUI("select", detail, false, "checkout")
		return "computer_select blocked: " + cuPersonHandoff("checkout", mark.Name, refusal)
	}
	for _, hit := range []string{sess.ElementNameAtScreen(x, y), sess.OCRTextAtScreen(x, y)} {
		refusal := browser.DesktopPayClickRefusal(hit)
		if refusal == "" {
			continue
		}
		sess.RecordAction("select", detail, false, "checkout", false)
		emitComputerUseActionUI("select", detail, false, "checkout")
		return "computer_select blocked: " + cuPersonHandoff("checkout", hit, refusal)
	}
	if msg := cuPointerWindowRefusal(sess, "computer_select", "select", detail, cuDragEnd{x: x, y: y, mark: mark}); msg != "" {
		return msg
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_select: " + msg
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
			if msg := cuInputDenied(sess, "computer_select", err); msg != "" {
				return msg
			}
			return fmt.Sprintf("computer_select failed: %v", err)
		}
	} else {
		sess.RecordAction("select", detail, false, "no actuator", false)
		return "computer_select: input simulator unavailable"
	}
	sess.RecordAction("select", detail+" via "+strategy, true, "", true)
	noteComputerUsePointWindow(x, y)
	markComputerUseSessionActive()
	emitComputerUseActionUI("select", detail+" via "+strategy, true, "")
	cuPostActionSettle()
	return cuFinishAction(fmt.Sprintf("selected %s via %s", detail, strategy))
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
	if msg := cuPointerWindowRefusal(sess, "computer_scroll_into_view", "scroll_into_view", detail, cuDragEnd{x: x, y: y, mark: mark}); msg != "" {
		return msg
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_scroll_into_view: " + msg
	}
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
	noteComputerUsePointWindow(x, y)
	markComputerUseSessionActive()
	emitComputerUseActionUI("scroll_into_view", detail, true, "")
	cuPostActionSettle()
	return cuFinishAction(fmt.Sprintf("scrolled %s into view", detail))
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
	// A pixel end is mapped through the stored crop. That crop is not the
	// screen a failed capture just described.
	if (guiStrArg(args, "from_ref", "") == "" || guiStrArg(args, "to_ref", "") == "") && cuStalePointerGeometry(sess) {
		return cuStaleLabelRefusal(sess, "computer_drag", "drag")
	}
	from, err := cuResolveDragPoint(sess, args, "from_ref", "from_x", "from_y")
	if err != nil {
		return "computer_drag: " + err.Error()
	}
	to, err := cuResolveDragPoint(sess, args, "to_ref", "to_x", "to_y")
	if err != nil {
		return "computer_drag: " + err.Error()
	}
	detail := fmt.Sprintf("(%d,%d)->(%d,%d)", from.x, from.y, to.x, to.y)
	if msg := cuDragPayRefusal(sess, from, to); msg != "" {
		return msg
	}
	if msg := cuPointerWindowRefusal(sess, "computer_drag", "drag", detail, from, to); msg != "" {
		return msg
	}
	if msg := cuReassertOperatedWindow(sess); msg != "" {
		return "computer_drag: " + msg
	}
	if err := sess.BeginAction("drag", detail); err != nil {
		return fmt.Sprintf("computer_drag: %v", err)
	}
	if err := input.DragDrop(from.x, from.y, to.x, to.y); err != nil {
		sess.RecordAction("drag", detail, false, err.Error(), false)
		if msg := cuInputDenied(sess, "computer_drag", err); msg != "" {
			return msg
		}
		return fmt.Sprintf("computer_drag failed: %v", err)
	}
	sess.RecordAction("drag", detail, true, "", true)
	noteComputerUsePointWindow(from.x, from.y)
	markComputerUseSessionActive()
	emitComputerUseActionUI("drag", detail, true, "")
	cuPostActionSettle()
	return cuFinishAction(fmt.Sprintf("dragged %s", detail))
}

// cuDragEnd is one end of a drag. pixel is false when the point came from a ref.
type cuDragEnd struct {
	x, y  int
	mark  *computeruse.MarkedElement
	pixel bool
}

// cuStalePointerGeometry reports that the stored screen is not a crop we can
// still label. A click, scroll, or drag mapped through it would use the
// previous window, or a full monitor the caption already said is not the
// foreground window. A titled crop still on file is current enough to map;
// the caller may read its labels.
func cuStalePointerGeometry(sess *computeruse.Session) bool {
	if sess == nil || sess.LabelsCurrent() {
		return false
	}
	_, ok := sess.StaleCropScreenshot()
	return !ok
}

// cuPointerWindowTitles are the windows a pointer action can hit. The
// observe-time window and the live window under the point are both included.
// A safe stored title must not hide a security surface the pointer will land
// on, and a live title must not hide a control already attributed to one.
func cuPointerWindowTitles(end cuDragEnd) []string {
	seen := map[string]struct{}{}
	var titles []string
	add := func(title string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		titles = append(titles, title)
	}
	if end.mark != nil {
		add(end.mark.Window)
	}
	add(cuPolicyWindowTitle(end.x, end.y))
	if len(titles) == 0 {
		return []string{""}
	}
	return titles
}

// cuPointerWindowRefusal applies the click window policy at every point the
// pointer will use. Either window being blocked refuses the action.
func cuPointerWindowRefusal(sess *computeruse.Session, tool, action, detail string, ends ...cuDragEnd) string {
	for _, end := range ends {
		for _, title := range cuPointerWindowTitles(end) {
			if err := sess.CheckClickPolicy(end.x, end.y, title); err != nil {
				if strings.TrimSpace(detail) == "" {
					detail = fmt.Sprintf("(%d,%d)", end.x, end.y)
				}
				sess.RecordAction(action, detail, false, err.Error(), false)
				emitComputerUseActionUI(action, detail, false, err.Error())
				return fmt.Sprintf("%s blocked: %v", tool, err)
			}
		}
	}
	return ""
}

func cuStaleLabelRefusal(sess *computeruse.Session, tool, action string) string {
	sess.RecordAction(action, "stale-labels", false, "stale-labels", false)
	emitComputerUseActionUI(action, "stale-labels", false, "stale-labels")
	return tool + ": control labels are from before the last action; call computer_observe again"
}

// cuDragPayRefusal applies the click pay gate to both ends. A pixel drag
// whose crop can still be labeled reads that crop first, the same way a
// pixel click does. A ref name is checked even when the point is not read.
func cuDragPayRefusal(sess *computeruse.Session, ends ...cuDragEnd) string {
	pixel := false
	for _, end := range ends {
		if end.pixel {
			pixel = true
			break
		}
	}
	if pixel && !sess.LabelsCurrent() {
		cuEnsureCurrentLabels(sess)
		if !sess.LabelsCurrent() {
			return cuStaleLabelRefusal(sess, "computer_drag", "drag")
		}
	}
	for _, end := range ends {
		names := make([]string, 0, 3)
		if end.mark != nil {
			if name := strings.TrimSpace(end.mark.Name); name != "" {
				names = append(names, name)
			}
		}
		if hit := sess.ElementNameAtScreen(end.x, end.y); hit != "" {
			names = append(names, hit)
		}
		if hit := sess.OCRTextAtScreen(end.x, end.y); hit != "" {
			names = append(names, hit)
		}
		for _, name := range names {
			refusal := browser.DesktopPayClickRefusal(name)
			if refusal == "" {
				continue
			}
			detail := fmt.Sprintf("(%d,%d)", end.x, end.y)
			sess.RecordAction("drag", detail, false, "checkout", false)
			emitComputerUseActionUI("drag", detail, false, "checkout")
			return "computer_drag blocked: " + cuPersonHandoff("checkout", name, refusal)
		}
	}
	return ""
}

func cuResolveDragPoint(sess *computeruse.Session, args map[string]interface{}, refKey, xKey, yKey string) (cuDragEnd, error) {
	if ref := guiStrArg(args, refKey, ""); ref != "" {
		x, y, el, err := sess.ResolveClickRef(ref)
		if err != nil {
			return cuDragEnd{}, err
		}
		return cuDragEnd{x: x, y: y, mark: el}, nil
	}
	if !sess.AllowPixelClick() {
		return cuDragEnd{}, fmt.Errorf("provide %s (or enable pixel click for %s/%s)", refKey, xKey, yKey)
	}
	x := guiIntArg(args, xKey, 0)
	y := guiIntArg(args, yKey, 0)
	x, y = sess.MapVisionClick(x, y)
	x, y = sess.MapCaptureClick(x, y)
	return cuDragEnd{x: x, y: y, pixel: true}, nil
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
			return captureSettleScreenshot()
		}, nil)
		if err := obs.WaitForStable(time.Duration(timeoutMs) * time.Millisecond); err != nil {
			return fmt.Sprintf("computer_wait stable: %v", err)
		}
		return cuFinishAction(fmt.Sprintf("computer_wait: UI considered stable (timeout %dms)", timeoutMs))
	}
	if ms < 0 {
		ms = 0
	}
	if ms > 30000 {
		ms = 30000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	return cuFinishAction(fmt.Sprintf("computer_wait: slept %dms", ms))
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
