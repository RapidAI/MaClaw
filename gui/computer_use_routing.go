package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/browser"
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

const computerUseLocalAttachmentMarker = "\n[附件: 当前轮已收到，待本地解析]"

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
	LocalFileWork     bool
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
	// A current local attachment is already staged for the agent and should be
	// handled through the file/Office tools.  In particular, exposing desktop
	// tools here makes a model try to open PowerShell or Explorer merely to
	// unpack/read a ZIP, even though no GUI interaction is needed.  This also
	// ends a stale sticky desktop session so it cannot leak into a fresh
	// document-processing turn.  An explicit @computer / "computer use" request
	// above remains the intentional opt-in override.
	if in.LocalFileWork {
		return computerUseActivationDecision{
			Active: false, ClearSticky: in.Sticky, Reason: "local_file_work",
		}
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
// Repeated prompt/tool preparation calls may clear a stale sticky state, which
// is idempotent; all other gate outcomes are read-only.
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
		Explicit:      hasExplicitComputerUseRequest(userText),
		LocalFileWork: hasCurrentLocalFileWork(userText),
		Sticky:        sticky,
		StickyAge:     stickyAge,
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

// hasExplicitComputerUseRequest recognizes an intentional desktop-control
// request only in the user-authored portion of a turn. Attachment staging and
// document extraction are appended to user content later in the pipeline; a
// document that merely discusses "@computer" or "computer use" must never be
// able to grant desktop-control authority to itself.
func hasExplicitComputerUseRequest(text string) bool {
	if cut := currentLocalFileWorkMarkerOffset(text); cut >= 0 {
		text = text[:cut]
	}
	return computeruse.HasExplicitTrigger(text)
}

// currentLocalFileWorkMarkerOffset returns the first control/content boundary
// introduced for the current turn's local file work. The marker itself and
// everything following it may include untrusted attachment text.
func currentLocalFileWorkMarkerOffset(text string) int {
	markers := []string{
		"[附件:",
		"[用户选择的本地文件路径]",
		"--- auto_extract: begin ",
	}
	first := -1
	for _, marker := range markers {
		if index := strings.Index(text, marker); index >= 0 && (first < 0 || index < first) {
			first = index
		}
	}
	return first
}

// hasCurrentLocalFileWork recognizes files supplied in the current turn. It
// deliberately includes archives: ZIPs are often skill packages or document
// bundles, but they still belong to local file handling rather than desktop
// control. Historical attachment annotations are excluded because a follow-up
// can legitimately continue an earlier Computer Use task.
func hasCurrentLocalFileWork(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if strings.Contains(text, "[用户选择的本地文件路径]") ||
		strings.Contains(text, "--- auto_extract: begin ") {
		return true
	}

	const currentAttachmentPrefix = "[附件:"
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, currentAttachmentPrefix) {
			continue
		}
		// The host writes this prefix only after staging the attachment. Do not
		// infer its kind from an extension: archives without one and arbitrary
		// binary uploads still need local file handling, not desktop control.
		return true
	}
	return false
}

// computerUseRoutingText reserves Computer Use for an attachment turn before
// the attachment has been staged and described in user content. The marker is
// control-plane-only: callers use it for prompt/tool routing, never as the
// visible user message. Explicit desktop intent remains authoritative.
func computerUseRoutingText(text string, attachments []MessageAttachment) string {
	return computerUseRoutingTextForLocalFileWork(text, len(attachments) > 0)
}

// computerUseRoutingTextForLocalFileWork adds the control-plane attachment
// marker when the caller knows that this turn has local file work, even if the
// raw user text has not yet been enriched with staged attachment details.
// Do not add it twice: a staged attachment/auto-extract marker already carries
// the same routing signal. Explicit desktop intent remains authoritative.
func computerUseRoutingTextForLocalFileWork(text string, hasLocalFileWork bool) string {
	if !hasLocalFileWork || hasExplicitComputerUseRequest(text) || hasCurrentLocalFileWork(text) {
		return text
	}
	return text + computerUseLocalAttachmentMarker
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
			intent.LabelSearch, intent.LabelDocumentDelivery, intent.LabelDocumentOpen, intent.LabelCoding:
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
		intent.LabelNonCoding, intent.LabelDocumentDelivery, intent.LabelDocumentOpen,
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
	return computerUsePlaybookSectionFor(active, "")
}

func computerUsePlaybookSectionFor(active bool, owner string) string {
	if !active {
		return ""
	}
	var b strings.Builder
	if computerUseLLMSupportsVision() {
		b.WriteString("\n## Computer Use（桌面操控 · 视觉模型看截图）\n")
		b.WriteString(computeruse.PlaybookVision())
	} else {
		b.WriteString("\n## Computer Use（桌面操控 · 文本模型优先）\n")
		b.WriteString(computeruse.Playbook())
	}
	b.WriteString("\n")
	if extra := computerUseContractPlaybookExtra(owner); extra != "" {
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}

func browserPlaybookSection(active bool) string {
	if !active {
		return ""
	}
	return "\n## Browser Use\n" + browser.Playbook() + "\n"
}

func (h *IMMessageHandler) shouldActivateBrowser(userText string) bool {
	if h == nil {
		return false
	}
	uic := h.getUnifiedClassifier()
	if uic == nil {
		return false
	}
	res := uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: userText})
	return res.Primary == intent.LabelBrowser
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
	computerUseVisionFn = func() bool {
		return app.GetMaclawLLMConfig().SupportsVision
	}
	computerUseCaptionConfigFn = func() (corelib.MaclawLLMConfig, bool) {
		cfg := app.GetCaptionLLMConfig()
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return corelib.MaclawLLMConfig{}, false
		}
		if id := strings.TrimSpace(cfg.ProviderID); id != "" {
			if err := app.ensureOAuthTokenForProvider(id); err != nil {
				log.Printf("[computer-use] caption oauth: %v", err)
				return corelib.MaclawLLMConfig{}, false
			}
			if err := app.ensureCodeGenTokenForProvider(id); err != nil {
				log.Printf("[computer-use] caption token: %v", err)
				return corelib.MaclawLLMConfig{}, false
			}
			cfg = app.GetCaptionLLMConfig()
			if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
				return corelib.MaclawLLMConfig{}, false
			}
		}
		return cfg, true
	}
	computerUseEventEmitter = func(name string, data interface{}) {
		app.emitEvent(name, data)
	}
}

// Deprecated name kept as alias for call sites.
func bindComputerUseYOLOGate(app *App) { bindComputerUseApp(app) }

func computerUsePlaybookOneLiner() string {
	if computerUseLLMSupportsVision() {
		return "vision: computer_observe attaches a screenshot → computer_click x,y in image pixels → re-observe"
	}
	return "text-primary: computer_observe → computer_click(ref=eN) → re-observe; no pixel guessing; screenshots not sent to LLM"
}
