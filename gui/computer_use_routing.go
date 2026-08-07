package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// computerUseEnabledFromConfig returns whether Computer Use product surface is on.
// Defaults to true when unset. Screen parsing (YOLO) can still be off independently.
func computerUseEnabledFromConfig(cfg *corelib.AppConfig) bool {
	if cfg == nil || cfg.ComputerUseEnabled == nil {
		return true
	}
	return *cfg.ComputerUseEnabled
}

func (h *IMMessageHandler) computerUseEnabled() bool {
	if h == nil || h.app == nil {
		return true
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return true
	}
	return computerUseEnabledFromConfig(&cfg)
}

// computerUseStickyTTL is the hard cap after the last successful computer_* action.
// Root cause of "random reactivation": sticky was process-lifetime.
const computerUseStickyTTL = 8 * time.Minute

// computerUseStickyDegradedTTL is used when intent cannot be re-evaluated (nil/degraded
// classifier). Without reclassification, long sticky lets models keep calling
// computer_observe on pure chat — keep that window short.
const computerUseStickyDegradedTTL = 2 * time.Minute

// computerUseIntentMinConfidence is the minimum UIC embedding confidence for
// LabelComputerUse to open the CU surface on a fresh turn.
// Raised from 0.50 to cut false opens on office/document phrasing.
const computerUseIntentMinConfidence = 0.65

// computerUseIntentCompetingConfidence is required when office/browser/non_coding
// also appear as secondary intents (common false-positive pattern: "写 word 简历").
const computerUseIntentCompetingConfidence = 0.78

// computerUseStickyReleaseConfidence is the minimum non-CU primary confidence that
// ends sticky session mid-conversation (e.g. user switched to pure chat/coding).
const computerUseStickyReleaseConfidence = 0.55

// clearComputerUseStickyLocked clears sticky flags. Caller must hold globalComputerUse.mu.
func clearComputerUseStickyLocked() (changed bool) {
	if !globalComputerUse.activated && globalComputerUse.activatedAt.IsZero() {
		return false
	}
	globalComputerUse.activated = false
	globalComputerUse.activatedAt = time.Time{}
	return true
}

func notifyComputerUseTrayIf(changed bool) {
	if changed && UpdateComputerUseTray != nil {
		UpdateComputerUseTray()
	}
}

// computerUseStickyState returns whether sticky is still valid (hard TTL) and its age.
// Lazy expiry refreshes the tray when the hard TTL elapses.
func computerUseStickyState() (active bool, age time.Duration) {
	globalComputerUse.mu.Lock()
	if !globalComputerUse.activated {
		globalComputerUse.mu.Unlock()
		return false, 0
	}
	if globalComputerUse.activatedAt.IsZero() {
		changed := clearComputerUseStickyLocked()
		globalComputerUse.mu.Unlock()
		notifyComputerUseTrayIf(changed)
		return false, 0
	}
	age = time.Since(globalComputerUse.activatedAt)
	if age < 0 {
		// Clock skew / time adjustment — treat as expired rather than sticky forever.
		changed := clearComputerUseStickyLocked()
		globalComputerUse.mu.Unlock()
		notifyComputerUseTrayIf(changed)
		return false, 0
	}
	if age > computerUseStickyTTL {
		changed := clearComputerUseStickyLocked()
		globalComputerUse.mu.Unlock()
		notifyComputerUseTrayIf(changed)
		return false, 0
	}
	globalComputerUse.mu.Unlock()
	return true, age
}

// computerUseSessionActive is true after a recent successful computer_* action.
func computerUseSessionActive() bool {
	active, _ := computerUseStickyState()
	return active
}

func markComputerUseSessionActive() {
	globalComputerUse.mu.Lock()
	was := globalComputerUse.activated
	globalComputerUse.activated = true
	globalComputerUse.activatedAt = time.Now()
	globalComputerUse.mu.Unlock()
	// Tray only needs a refresh when entering Active from inactive.
	notifyComputerUseTrayIf(!was)
}

// clearComputerUseSessionActive ends sticky CU injection (done / stop / reset /
// non-CU turn / TTL). Safe to call when already inactive.
func clearComputerUseSessionActive() {
	globalComputerUse.mu.Lock()
	changed := clearComputerUseStickyLocked()
	globalComputerUse.mu.Unlock()
	notifyComputerUseTrayIf(changed)
}

// computerUseActivationInput is the pure input to the CU gate (testable without UIC).
type computerUseActivationInput struct {
	Explicit          bool
	Sticky            bool
	StickyAge         time.Duration
	HasClassification bool
	Classification    intent.ClassificationResult
}

// computerUseActivationDecision is the pure gate result.
type computerUseActivationDecision struct {
	Active      bool
	ClearSticky bool
	Reason      string
}

// decideComputerUseActivation is the pure root-cause gate:
//
//  1. Explicit @computer / "computer use" → open
//  2. Confident LabelComputerUse → open
//  3. Sticky from recent computer_* activity → open only while the user has not
//     clearly left desktop control; shortened when classification is unavailable
func decideComputerUseActivation(in computerUseActivationInput) computerUseActivationDecision {
	if in.Explicit {
		return computerUseActivationDecision{Active: true, Reason: "explicit_trigger"}
	}

	if in.HasClassification && computerUseIntentActivated(in.Classification) {
		return computerUseActivationDecision{Active: true, Reason: "semantic_computer_use"}
	}

	if !in.Sticky {
		return computerUseActivationDecision{Active: false, Reason: "inactive"}
	}

	// Sticky path: cannot reclassify → short degraded TTL only.
	if !in.HasClassification || in.Classification.Degraded {
		if in.StickyAge > computerUseStickyDegradedTTL {
			return computerUseActivationDecision{
				Active: false, ClearSticky: true, Reason: "sticky_degraded_ttl",
			}
		}
		return computerUseActivationDecision{Active: true, Reason: "sticky_degraded"}
	}

	if computerUseStickyShouldRelease(in.Classification) {
		return computerUseActivationDecision{
			Active: false, ClearSticky: true, Reason: "sticky_released",
		}
	}
	return computerUseActivationDecision{Active: true, Reason: "sticky"}
}

// shouldActivateComputerUse decides playbook + tool injection for this turn.
// Pure: repeated calls within a turn (prompt build + per-iteration tool
// filtering) must not mutate session control state.
func (h *IMMessageHandler) shouldActivateComputerUse(userText string) bool {
	active, _ := h.gateComputerUse(userText)
	return active
}

// gateComputerUse evaluates the CU gate. fresh reports an explicit/semantic
// open (a new desktop task) as opposed to sticky continuation of an existing
// session.
func (h *IMMessageHandler) gateComputerUse(userText string) (active, fresh bool) {
	if h == nil || !h.computerUseEnabled() {
		return false, false
	}

	sticky, stickyAge := computerUseStickyState()
	in := computerUseActivationInput{
		Explicit:  computeruse.HasExplicitTrigger(userText),
		Sticky:    sticky,
		StickyAge: stickyAge,
	}
	if uic := h.getUnifiedClassifier(); uic != nil {
		in.HasClassification = true
		in.Classification = uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: userText})
	}

	d := decideComputerUseActivation(in)
	if d.ClearSticky {
		clearComputerUseSessionActive()
	}
	// Log only state changes / opens — not the common inactive path.
	if d.Active || d.ClearSticky {
		log.Printf("[computer-use] gate active=%v clear=%v reason=%s sticky=%v age=%s",
			d.Active, d.ClearSticky, d.Reason, sticky, stickyAge.Round(time.Millisecond))
	}
	fresh = d.Active && d.Reason != "sticky" && d.Reason != "sticky_degraded"
	return d.Active, fresh
}

// liftComputerUseStopForFreshRequest lifts a stale hard-stop when a brand-new
// CU task activates, so the operator console can hide after Stop without
// leaving the session permanently blocked. Pause is left untouched.
//
// Safety: the lift happens at most once per request ID. A stopped in-flight
// turn whose cancel is still taking effect re-gates with the SAME request ID
// (the same user text reclassifies as explicit/semantic) — that re-gate must
// not resurrect the turn the operator just stopped.
func liftComputerUseStopForFreshRequest(requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		// Without a request identity a re-gated in-flight turn is
		// indistinguishable from a new task — stay safe and keep the stop.
		return
	}
	globalComputerUse.mu.Lock()
	if globalComputerUse.lastFreshOpenRequestID == requestID {
		globalComputerUse.mu.Unlock()
		return
	}
	globalComputerUse.lastFreshOpenRequestID = requestID
	globalComputerUse.mu.Unlock()

	sess := cuSession()
	if sess == nil {
		return
	}
	if _, stopped := sess.ControlState(); !stopped {
		return
	}
	sess.ResetControl()
	log.Printf("[computer-use] cleared stopped state for fresh task activation")
	// Keep the tray submenu in sync — nothing else refreshes it until the next
	// sticky transition, so without this the tray would show "stopped" for the
	// whole new task.
	if UpdateComputerUseTray != nil {
		UpdateComputerUseTray()
	}
}

// computerUseIntentActivated is the pure decision on a UIC result: the gate
// opens only for a confident, non-degraded computer_use primary intent.
// Competing secondaries (office/browser/content) require a clearer win.
func computerUseIntentActivated(res intent.ClassificationResult) bool {
	if res.Degraded {
		return false
	}
	if res.Primary != intent.LabelComputerUse {
		return false
	}
	if res.Confidence < computerUseIntentMinConfidence {
		return false
	}
	if computerUseHasCompetingSecondary(res) && res.Confidence < computerUseIntentCompetingConfidence {
		return false
	}
	return true
}

func computerUseHasCompetingSecondary(res intent.ClassificationResult) bool {
	for _, s := range res.Secondary {
		switch s {
		case intent.LabelOffice, intent.LabelBrowser, intent.LabelNonCoding,
			intent.LabelSearch, intent.LabelDocumentDelivery, intent.LabelCoding:
			return true
		}
	}
	return false
}

// computerUseStickyShouldRelease ends sticky injection only when the user has
// clearly left desktop control. Office is intentionally NOT a release label:
// mid-task follow-ups ("第二段改成简介") often classify as office and must keep
// computer_* tools. Fresh-turn office false opens are blocked by
// computerUseIntentActivated + competing secondary threshold instead.
func computerUseStickyShouldRelease(res intent.ClassificationResult) bool {
	if res.Degraded {
		return false
	}
	if res.Confidence < computerUseStickyReleaseConfidence {
		return false
	}
	switch res.Primary {
	case intent.LabelComputerUse,
		intent.LabelContinuation,
		intent.LabelUnknown,
		intent.LabelAmbiguous,
		intent.LabelOffice,
		intent.LabelCurrentTime:
		// Stay sticky: multi-step desktop / soft follow-ups / time checks / uncertain.
		return false
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance,
		intent.LabelSearch, intent.LabelBrowser, intent.LabelSSH,
		intent.LabelKnowledgeWrite, intent.LabelLiveData,
		intent.LabelNonCoding, intent.LabelDocumentDelivery,
		intent.LabelBusinessData, intent.LabelWorkflowTask:
		return true
	default:
		// Unknown future labels: release when confidence is already high enough.
		return true
	}
}

// ensureComputerUseTools forces CU tools into the routed list when active.
// Also drops competing legacy gui_click/type/screenshot to steer text models
// toward ref-based computer_* tools.
func ensureComputerUseTools(tools, allTools []map[string]interface{}, active bool) []map[string]interface{} {
	if !active {
		return tools
	}
	byName := make(map[string]map[string]interface{}, len(allTools))
	for _, t := range allTools {
		n := extractToolName(t)
		if n != "" {
			byName[n] = t
		}
	}
	out := make([]map[string]interface{}, 0, len(tools)+len(computeruse.ToolNames))
	// Prefer computer tools first in the list for model attention.
	seen := make(map[string]bool, len(computeruse.ToolNames))
	for _, name := range computeruse.ToolNames {
		if def, ok := byName[name]; ok {
			out = append(out, def)
			seen[name] = true
		}
	}
	for _, t := range tools {
		name := extractToolName(t)
		if name == "" {
			out = append(out, t)
			continue
		}
		if seen[name] || computeruse.IsComputerUseTool(name) {
			continue
		}
		if computeruse.LegacyGUICompeteTools[name] {
			continue // demote raw coordinate tools while CU is active
		}
		out = append(out, t)
	}
	return out
}

// computerUsePlaybookSection returns system-prompt text when CU is active.
func computerUsePlaybookSection(active bool) string {
	if !active {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Computer Use（桌面操控 · 文本模型优先）\n")
	b.WriteString(computeruse.Playbook())
	b.WriteString("\n")
	return b.String()
}

// computerUseYOLOAllowed is set from App.GetScreenParsingEnabled when tools register.
// Defaults to true so headless tests still exercise YOLO when weights exist.
var computerUseYOLOAllowedFn = func() bool { return true }

// computerUseEnabledFn gates computer_* tool EXECUTION (not just activation).
// Defaults to true so headless tests exercise the handlers.
var computerUseEnabledFn = func() bool { return true }

// computerUseTargetAppsFn supplies the TargetApps click allowlist from config.
// ok=false means the config could not be read — callers must keep the previous
// allowlist rather than silently dropping it (fail-closed).
var computerUseTargetAppsFn func() (apps []string, ok bool)

// computerUseToolsEnabled reports whether computer_* handlers may run.
func computerUseToolsEnabled() bool {
	if computerUseEnabledFn == nil {
		return true
	}
	return computerUseEnabledFn()
}

// computerUseEventEmitter pushes operator-preview events to the UI when bound.
var computerUseEventEmitter func(name string, data interface{})

func computerUseYOLOAllowed() bool {
	if computerUseYOLOAllowedFn == nil {
		return true
	}
	return computerUseYOLOAllowedFn()
}

func emitComputerUseEvent(name string, data interface{}) {
	if computerUseEventEmitter == nil {
		return
	}
	computerUseEventEmitter(name, data)
}

// emitComputerUseDoneControl notifies UI that sticky CU session ended via computer_done.
func emitComputerUseDoneControl(steps int) {
	emitComputerUseEvent(EventComputerUseControl, map[string]interface{}{
		"at":      time.Now().Format(time.RFC3339),
		"action":  "done",
		"paused":  false,
		"stopped": false,
		"steps":   steps,
	})
}

// bindComputerUseApp wires YOLO gate + UI event emission for Computer Use.
func bindComputerUseApp(app *App) {
	if app == nil {
		return
	}
	computerUseYOLOAllowedFn = func() bool {
		return app.GetScreenParsingEnabled()
	}
	computerUseEnabledFn = func() bool {
		return app.GetComputerUseEnabled()
	}
	computerUseTargetAppsFn = func() ([]string, bool) {
		cfg, err := app.LoadConfig()
		if err != nil {
			return nil, false
		}
		return cfg.ComputerUseTargetApps, true
	}
	computerUseEventEmitter = func(name string, data interface{}) {
		app.emitEvent(name, data)
	}
}

// Deprecated name kept as alias for call sites.
func bindComputerUseYOLOGate(app *App) { bindComputerUseApp(app) }

func computerUsePlaybookOneLiner() string {
	return "text-primary: computer_observe → computer_click(ref=eN) → re-observe; no pixel guessing; screenshots not sent to LLM"
}
