// Package main contains the V2 workflow engine integration with the GUI layer.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// workflowV2Router is the V2 workflow router instance.
// Set during App initialization when workflow_engine_version == "v2".
type workflowV2State struct {
	router   *v2.WorkflowRouter
	machine  *v2.StateMachine
	store    v2.WorkflowStore
	registry *v2.TemplateRegistry
}

// initWorkflowV2 initializes the V2 workflow engine.
func (a *App) initWorkflowV2() *workflowV2State {
	log.Printf("[workflow-v2] initWorkflowV2 called")
	dbPath := filepath.Join(a.getMaclawBaseDir(), "workflow_v2.db")
	store, err := v2.NewSQLiteStore(dbPath)
	if err != nil {
		log.Printf("[workflow-v2] failed to init SQLite store: %v, falling back to memory", err)
		memStore := v2.NewMemoryStore()
		return a.buildWorkflowV2StateWithLLM(memStore)
	}
	log.Printf("[workflow-v2] initialized with SQLite store at %s", dbPath)
	return a.buildWorkflowV2StateWithLLM(store)
}

func (a *App) buildWorkflowV2StateWithLLM(store v2.WorkflowStore) *workflowV2State {
	st := buildWorkflowV2State(store)
	// Wire LLM-based confirm classifier.
	st.machine.SetConfirmClassifier(a.workflowV2ConfirmClassifier)
	if strings.TrimSpace(a.testHomeDir) != "" {
		st.machine.SetAllowTempTestPaths(true)
	}
	log.Printf("[workflow-v2] engine ready: router=%v machine=%v store=%v", st.router != nil, st.machine != nil, st.store != nil)

	// Self-heal: cancel stale active workflows that were left over from a previous
	// session (e.g. app crash, forced quit, or update restart). Without this, the
	// stale active state causes routing conflicts when the user starts a new workflow.
	staleCancelled := a.cancelStaleWorkflowsOnStartup(st.machine)

	// On startup, dismiss any stale frontend workflow board state.
	// The frontend persists board state to ai_assistant_ui_state.json and restores it
	// on reload, but the backend workflow may have been cancelled or completed since then.
	// We emit a dismiss event so the frontend starts clean; if there's an active workflow,
	// the next user message will re-emit the correct phase_update via routeWithWorkflowV2.
	//
	// Skip the delayed emit if we already cancelled a stale workflow above — the state
	// is already clean, and the delayed emit could race with a new workflow the user
	// starts within the 500ms window.
	if !staleCancelled {
		go func() {
			time.Sleep(500 * time.Millisecond) // Wait for Wails runtime to be ready
			emitWorkflowV2Event(a, "workflow:suggest_maximize_dismiss", nil)
			emitWorkflowV2Event(a, "workflow:phase_update", nil)
			log.Printf("[workflow-v2] startup: emitted board reset to clear stale frontend state")
		}()
	}

	return st
}

// suspendStaleWorkflowsOnStartup marks any active workflow from a previous session
// as suspended. Returns true if a workflow was suspended.
//
// Mechanism: App restart is a session boundary. Workflows depend on continuous
// interactive confirm/modify loops that are broken when the app closes. Without
// suspension, the router's Step 1 ("active workflow takes priority") hijacks the
// user's first message, routing it to the stale workflow's current phase instead
// of treating it as a fresh request.
//
// Suspension (vs cancellation): The workflow state and all completed phase outputs
// are preserved. The user can resume by saying "继续" (which the confirm classifier
// recognizes), or send an unrelated message which passes through to the agent loop.
// This preserves the user's work (requirements, design docs, etc.) while preventing
// automatic execution hijacking.
func (a *App) cancelStaleWorkflowsOnStartup(machine *v2.StateMachine) bool {
	if machine == nil {
		return false
	}

	// List all user IDs with stored workflow state (desktop-user + project tabs).
	userIDs, err := machine.ListAllStoredUserIDs()
	if err != nil {
		log.Printf("[workflow-v2] startup: failed to list stored workflows: %v", err)
		userIDs = []string{"desktop-user"}
	}

	suspended := false
	for _, userID := range userIDs {
		state := machine.GetActive(userID)
		if state == nil {
			continue
		}
		if state.Suspended {
			// Already suspended from a previous startup — still counts as "handled"
			suspended = true
			continue
		}
		if err := machine.SuspendWorkflow(userID); err != nil {
			log.Printf("[workflow-v2] startup: failed to suspend workflow %s (user=%s): %v", state.ID, userID, err)
			// Fallback: try to cancel or delete to prevent hijacking
			if cancelErr := machine.Cancel(userID); cancelErr != nil {
				_ = machine.DeleteState(userID)
			}
			suspended = true
			continue
		}
		log.Printf("[workflow-v2] startup: suspended workflow %s (user=%s, type=%s, phase=%d, age=%s)",
			state.ID, userID, state.Type, state.CurrentPhase, time.Since(state.UpdatedAt).Truncate(time.Second))
		suspended = true
	}

	if suspended {
		go func() {
			time.Sleep(500 * time.Millisecond)
			emitWorkflowV2Event(a, "workflow:suggest_maximize_dismiss", nil)
			emitWorkflowV2Event(a, "workflow:phase_update", nil)
		}()
	}
	return suspended
}

// workflowV2ConfirmClassifier uses LLM to classify user intent during workflow confirmation.
// Retries once on transient errors (503/timeout) before falling back to keyword matching.
func (a *App) workflowV2ConfirmClassifier(phaseContext, userText string) string {
	hubClient := a.hubClient()
	if hubClient == nil {
		return v2.ClassifyConfirmIntentKeyword(userText)
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return v2.ClassifyConfirmIntentKeyword(userText)
	}

	req := LLMClassifyRequest{
		SystemPrompt:      v2.ConfirmClassifierSystemPrompt,
		UserMessage:       v2.BuildConfirmClassifierUserPrompt(phaseContext, userText),
		TimeoutSec:        12,
		Tag:               "workflow-confirm-v2",
		PreferLightweight: true,
	}

	// Try up to 2 times (initial + 1 retry) with a short backoff.
	var result *LLMClassifyResult
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		result, err = handler.LLMClassify(ctx, req)
		cancel()
		if err == nil {
			break
		}
		if attempt == 0 {
			log.Printf("[workflow-v2] confirm classifier attempt 1 failed: %v, retrying in 2s", err)
			time.Sleep(2 * time.Second)
		}
	}
	if err != nil {
		log.Printf("[workflow-v2] confirm classifier LLM failed after retry: %v, falling back to keywords", err)
		return v2.ClassifyConfirmIntentKeyword(userText)
	}
	intent := v2.ParseConfirmClassifierResponse(result.Text)
	log.Printf("[workflow-v2] confirm classifier: text=%q → intent=%q (latency=%s)", userText, intent, result.Latency.Round(time.Millisecond))
	if intent == "" {
		return v2.ClassifyConfirmIntentKeyword(userText)
	}
	return intent
}

func buildWorkflowV2State(store v2.WorkflowStore) *workflowV2State {
	registry := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(registry)
	machine := v2.NewStateMachine(store, registry)
	router := v2.NewWorkflowRouter(machine, registry, nil) // no LLM confirm for now
	// Populate the template-keyword-driven explicit hint registry so that
	// inferExplicitWorkflowHint works for ALL templates, not just PPT.
	SetExplicitHintTemplates(registry)
	return &workflowV2State{
		router:   router,
		machine:  machine,
		store:    store,
		registry: registry,
	}
}

// intentLabelToTemplateType maps UIC intent labels that don't directly
// correspond to template registry type names. This bridges the vocabulary gap
// between the intent classifier (which uses domain labels like "office") and
// the template registry (which uses specific type names like "presentation_design").
//
// Only workflow-candidate labels need mapping here. Non-workflow labels (ssh,
// search, etc.) are passed through for veto purposes and don't need mapping.
//
// TODO: When more mappings accumulate, move to IntentDefinition.MappedWorkflowType
// field in corelib/intent/definitions.go so it's part of the unified intent data.
func intentLabelToTemplateType(label string) string {
	switch label {
	case "office":
		return "presentation_design"
	default:
		return ""
	}
}

// workflowHintExclusionPatterns are verbs/patterns that indicate the user is
// operating on an EXISTING artifact (open, read, send, delete) rather than
// requesting to CREATE a new one. This is a closed set — there are only a few
// ways to reference an existing file. In contrast, creation patterns are open-ended
// ("做"/"搞"/"弄"/"帮我来个"/...) and cannot be enumerated.
//
// Design principle: exclude the finite set of non-creation actions rather than
// enumerate the infinite set of creation actions.
var workflowHintExclusionPatterns = []string{
	// File operations — user is acting on an existing artifact
	"打开", "读取", "查看", "看看", "阅读", "浏览", "预览",
	"发送", "发给", "转发", "分享",
	"删除", "移除", "清理",
	"截图", "截屏",
	// Modification — ambiguous (could be "modify existing" vs "create new"),
	// conservatively excluded to avoid false-positive workflow triggers
	"修改", "编辑", "更新",
	// Content processing — when combined with a template keyword (e.g. "总结这个PPT"),
	// usually means operating on existing material, not creating from scratch.
	// Ambiguous cases (e.g. "做竞品分析总结") fall through to UIC for accurate classification.
	"总结", "翻译", "转换",
	// English equivalents
	"open", "read", "view", "preview", "send", "forward", "delete", "remove",
	"edit", "modify", "update", "summarize", "translate", "convert", "screenshot",
}

// inferExplicitWorkflowHint detects explicit template signals from user text by
// matching template Keywords. This is a fast pre-router heuristic (~0ms) that
// provides a high-confidence hint before the full UIC classification (~7s).
//
// Logic: if the message contains a strong template keyword AND does NOT contain
// an exclusion pattern (file operations, read/send/delete), return that template
// type as hint. The exclusion set is finite and stable; the creation intent set
// is open and need not be enumerated.
//
// Returns the template type string if matched, empty otherwise.
func inferExplicitWorkflowHint(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" || utf8.RuneCountInString(lower) < 3 {
		return ""
	}
	// Step 1: Match a strong template keyword.
	reg := getExplicitHintRegistry()
	if reg == nil {
		return ""
	}
	matched := reg.matchStrongKeyword(lower)
	if matched == "" {
		return ""
	}
	// Step 2: Check exclusion — if the message looks like a file operation on
	// an existing artifact rather than a creation request, don't hint.
	for _, excl := range workflowHintExclusionPatterns {
		if containsWorkflowHintExclusion(lower, excl) {
			return ""
		}
	}
	return matched
}

// explicitHintRegistry caches strong keywords from templates for fast matching.
// The entries slice is populated once at init time and never modified after,
// so no mutex is needed for reads.
type explicitHintRegistry struct {
	entries []explicitHintEntry
}

type explicitHintEntry struct {
	keyword           string // lowercased
	workflowType      string
	needsWordBoundary bool // true for short ASCII keywords that would substring-match common words
}

var globalExplicitHintRegistry *explicitHintRegistry
var globalExplicitHintRegistryOnce sync.Once

// getExplicitHintRegistry returns the hint registry, lazy-initializing it from
// builtin templates if SetExplicitHintTemplates was not called (e.g. in tests).
func getExplicitHintRegistry() *explicitHintRegistry {
	globalExplicitHintRegistryOnce.Do(func() {
		if globalExplicitHintRegistry == nil {
			// Lazy init from builtin templates for test and fallback scenarios.
			registry := v2.NewTemplateRegistry()
			v2.RegisterBuiltinTemplates(registry)
			SetExplicitHintTemplates(registry)
		}
	})
	return globalExplicitHintRegistry
}

// SetExplicitHintTemplates populates the hint registry from template keywords.
// Called once during initialization when the TemplateRegistry is ready.
func SetExplicitHintTemplates(templates *v2.TemplateRegistry) {
	if templates == nil {
		return
	}
	reg := &explicitHintRegistry{}
	for _, typ := range templates.AllTypes() {
		tmpl := templates.Get(typ)
		if tmpl == nil {
			continue
		}
		// SemanticOnly templates are explicitly designed to NOT be activated
		// via keyword matching — they require IUM LLM classification.
		if tmpl.SemanticOnly {
			continue
		}
		for _, kw := range tmpl.Keywords {
			kwLower := strings.ToLower(strings.TrimSpace(kw))
			if kwLower == "" {
				continue
			}
			// Only include "strong" keywords: >=3 CJK chars, or uppercase
			// abbreviations (PPT, SWOT, PRD), or multi-word phrases.
			if isStrongExplicitKeyword(kwLower) {
				reg.entries = append(reg.entries, explicitHintEntry{
					keyword:           kwLower,
					workflowType:      tmpl.Type,
					needsWordBoundary: needsWordBoundaryMatch(kwLower),
				})
			}
		}
	}
	globalExplicitHintRegistry = reg
}

func isStrongExplicitKeyword(kw string) bool {
	if kw == "powerpoint" {
		return true
	}
	// Uppercase abbreviations (original case was lowered, check if short and ASCII)
	if len(kw) >= 2 && len(kw) <= 6 && isASCIIAlpha(kw) {
		return true // PPT, SWOT, PRD etc
	}
	// CJK-heavy strings (>=3 runes that are CJK)
	cjkCount := 0
	for _, r := range kw {
		if r >= 0x4E00 && r <= 0x9FFF {
			cjkCount++
		}
	}
	if cjkCount >= 3 {
		return true // 幻灯片, 演示文稿, 商业计划, 竞品分析, etc.
	}
	// Multi-word English phrases
	if strings.Contains(kw, " ") && len(kw) >= 8 {
		return true // "slide deck", "business plan", etc.
	}
	return false
}

func isASCIIAlpha(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// needsWordBoundaryMatch returns true for short ASCII keywords that could
// accidentally substring-match common English words (e.g. "dd" in "add",
// "bp" in "subproject"). CJK keywords don't need this because CJK characters
// are inherently word-boundary-separated in text.
func needsWordBoundaryMatch(kw string) bool {
	// Only pure-ASCII short keywords need word boundary.
	// CJK keywords (幻灯片, 演示文稿) don't — they're self-delimiting.
	if !isASCIIAlpha(kw) {
		return false
	}
	return len(kw) <= 4 // "dd"(2), "bp"(2), "ppt"(3), "prd"(3), "qa"(2), "swot"(4)
}

func (r *explicitHintRegistry) matchStrongKeyword(lowerText string) string {
	for _, entry := range r.entries {
		if entry.needsWordBoundary {
			// Short ASCII keywords require word-boundary matching to avoid
			// false positives like "dd" matching "add" or "bp" matching "subproject".
			if containsWordBoundary(lowerText, entry.keyword) {
				return entry.workflowType
			}
		} else {
			if strings.Contains(lowerText, entry.keyword) {
				return entry.workflowType
			}
		}
	}
	return ""
}

// containsWordBoundary checks if keyword appears in text at a word boundary.
// A word boundary is: start/end of string, space, punctuation, or CJK character.
// The keyword is guaranteed to be pure ASCII (from isASCIIAlpha + needsWordBoundary),
// so strings.Index byte positions correctly delimit the keyword. We only need
// proper UTF-8 decoding for the boundary characters adjacent to the keyword.
func containsWorkflowHintExclusion(lowerText, exclusion string) bool {
	exclusion = strings.ToLower(strings.TrimSpace(exclusion))
	if exclusion == "" {
		return false
	}
	if isASCIIAlpha(exclusion) {
		return containsWordBoundary(lowerText, exclusion)
	}
	return strings.Contains(lowerText, exclusion)
}
func containsWordBoundary(text, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(text[idx:], keyword)
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		endPos := absPos + len(keyword)

		// Check left boundary: decode the rune ending at absPos.
		leftOK := absPos == 0
		if !leftOK {
			r, _ := utf8.DecodeLastRuneInString(text[:absPos])
			leftOK = r != utf8.RuneError && isWordBoundaryChar(r)
		}

		// Check right boundary: decode the rune starting at endPos.
		rightOK := endPos >= len(text)
		if !rightOK {
			r, _ := utf8.DecodeRuneInString(text[endPos:])
			rightOK = isWordBoundaryChar(r)
		}

		if leftOK && rightOK {
			return true
		}
		idx = absPos + 1
		if idx >= len(text) {
			return false
		}
	}
}

func isWordBoundaryChar(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' ||
		r == '!' || r == '?' || r == ';' || r == ':' || r == '"' || r == '\'' ||
		r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' ||
		r == '/' || r == '-' || r == '_' || unicode.IsPunct(r) || unicode.IsSpace(r) ||
		(r >= 0x4E00 && r <= 0x9FFF) || // CJK char is always a boundary
		(r >= 0x3000 && r <= 0x303F) // CJK punctuation
}

// routeWithWorkflowV2 is the V2 replacement for routeWorkflowIMMessage.
// Returns a workflowIMRouteResult compatible with the existing entry context.
func (h *IMMessageHandler) routeWithWorkflowV2(msg IMUserMessage, trimmed string) workflowIMRouteResult {
	wf := h.getWorkflowV2()
	if wf == nil {
		log.Printf("[workflow-v2] routeWithWorkflowV2: wf is nil, app=%v app.workflowV2=%v", h.app != nil, h.app != nil && h.app.workflowV2 != nil)
		return workflowIMRouteResult{}
	}

	// --- Handle experiment orchestrator control commands ---
	if resp := h.handleExperimentOrchestratorCommand(msg.UserID, trimmed); resp != nil {
		return workflowIMRouteResult{Response: resp}
	}

	// --- Deliver pending experiment notifications ---
	if notif, ok := h.pendingExperimentNotification.LoadAndDelete(msg.UserID); ok {
		if notifStr, ok := notif.(string); ok && notifStr != "" {
			// Prepend the notification to whatever response will be generated
			// For now, deliver it immediately as a standalone response
			return workflowIMRouteResult{Response: &IMAgentResponse{Text: notifStr}}
		}
	}

	// Coding implementation resume: 重试失败 / 继续执行 (before review / router).
	if retryRoute := h.tryQueueCodingExecRetryCommand(msg.UserID, trimmed); retryRoute != nil {
		return *retryRoute
	}

	// Review replies must be handled before generic router matching. Otherwise
	// short confirms like "继续推进" or implementation requests at the coding
	// task-breakdown gate can be misrouted as a fresh task or a new workflow.
	if h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.IsAwaitingReview(msg.UserID) {
		if resp := h.handleWorkflowReview(h.app.workflowEngine, msg.UserID, trimmed, msg.Platform); resp != nil {
			return workflowIMRouteResult{Response: resp}
		}
		if marker, ok := h.workflowAgentLoopMarker.Load(msg.UserID); ok {
			if enabled, _ := marker.(bool); enabled {
				if _, promptOK := h.stashedPhasePrompt.Load(msg.UserID); !promptOK {
					if phasePrompt := h.app.workflowEngine.BuildPhasePrompt(msg.UserID); strings.TrimSpace(phasePrompt) != "" {
						h.stashedPhasePrompt.Store(msg.UserID, phasePrompt)
					} else if wf.machine != nil {
						if state := wf.machine.GetActive(msg.UserID); state != nil {
							if phasePrompt := v2.BuildPhasePrompt(state); strings.TrimSpace(phasePrompt) != "" {
								h.stashedPhasePrompt.Store(msg.UserID, phasePrompt)
							}
						}
					}
				}
				return workflowIMRouteResult{WorkflowAgentLoop: true}
			}
		}
	}

	// --- Handle pending coding complexity choice ---
	if choice := h.handleCodingComplexityCommand(msg, trimmed); choice != nil {
		return *choice
	}
	if h.hasPendingTemplateSubAgentExecution(msg.UserID) {
		log.Printf("[workflow-v2] template SubAgent mode awaiting user task: user=%s", msg.UserID)
		return workflowIMRouteResult{WorkflowAgentLoop: true}
	}

	log.Printf("[workflow-v2] routing: user=%s text_len=%d", msg.UserID, len([]rune(trimmed)))

	// VE group executor messages should not trigger workflow creation.
	// They execute specific tasks delegated by the group conversation —
	// workflow routing is for user-initiated tasks only.
	// Background tasks (scheduled, auto-picked) also bypass workflow.
	if msg.Platform == "ve_group_executor" || msg.IsBackground {
		return workflowIMRouteResult{}
	}

	var attachments []v2.Attachment
	for _, a := range msg.Attachments {
		attachments = append(attachments, v2.Attachment{Type: a.Type, Name: a.FileName})
	}

	// Workflows are started only from the workflow panel. Ordinary messages must
	// never be classified or matched as a new workflow; this path only continues
	// an already active workflow. Panel launches are handled above as explicit
	// workflow choice commands.
	if wf.machine == nil || wf.machine.GetActive(msg.UserID) == nil {
		return workflowIMRouteResult{}
	}

	result := wf.router.Route(msg.UserID, trimmed, attachments)

	switch result.Target {
	case v2.RouteToAgentLoop:
		log.Printf("[workflow-v2] route_result: user=%s target=agent_loop", msg.UserID)
		return workflowIMRouteResult{}

	case v2.RouteToWorkflow:
		if result.HandleResult != nil {
			// Active workflow handled the message
			return h.handleWorkflowV2Action(msg, result.HandleResult)
		}
		// A message unrelated to the active workflow may match a template in the
		// generic router. Do not turn that into an implicit workflow launch.
		return workflowIMRouteResult{}
	}

	return workflowIMRouteResult{}
}

func (h *IMMessageHandler) startNewWorkflowV2(msg IMUserMessage, routeResult *v2.RouteResult) workflowIMRouteResult {
	wf := h.getWorkflowV2()
	if wf == nil {
		return workflowIMRouteResult{}
	}

	// Cancel any existing workflow and reset the frontend dashboard before starting a new one.
	// Without this, the old workflow's phase progress (completed checkmarks) bleeds into the new panel.
	if prevState := wf.machine.GetActive(msg.UserID); prevState != nil {
		wf.machine.Cancel(msg.UserID)
		h.workflowV2Adapters.Delete(msg.UserID)
		h.clearCodingExecCheckpoint(msg.UserID)
		emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
			"id":             prevState.ID,
			"status":         string(v2.StatusCancelled),
			"type":           prevState.Type,
			"project_path":   workflowEventProjectPath(prevState),
			"event_scope_id": h.app.getEventScopeID(prevState.UserID),
		})
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", map[string]interface{}{
			"event_scope_id": h.app.getEventScopeID(prevState.UserID),
		})
		log.Printf("[workflow-v2] cancelled previous workflow before starting new one: user=%s", msg.UserID)
	} else {
		// No active workflow, but a durable checkpoint may still linger from a prior run.
		h.clearCodingExecCheckpoint(msg.UserID)
	}

	// Resolve working directory with the same chain as ProjectDirBar / tools /
	// system prompt. Never prefer config.Projects (GetCurrentProjectPath) over
	// the user-visible top-bar working directory — that mismatch made agents
	// list home or unrelated trees while the UI showed another path.
	projectPath := strings.TrimSpace(routeResult.ProjectPath)
	if projectPath == "" || projectPath == "." {
		projectPath = h.workflowStartProjectPathForOwner(msg.UserID)
	}
	// Ensure project path is ASCII-safe for SubAgent (LLMs struggle with Chinese paths)
	if projectPath != "" {
		cleaned := v2.TruncateToValidPathChars(projectPath)
		if cleaned != "" {
			projectPath = cleaned
		}
	}
	if projectPath == "" {
		projectPath = "."
	}

	state, err := wf.machine.Create(msg.UserID, routeResult.WorkflowType, projectPath, msg.Text)
	if err != nil {
		// If project path was rejected (temp/test directory), retry with "." for non-coding workflows
		if routeResult.WorkflowType != "coding" && projectPath != "." {
			log.Printf("[workflow-v2] Create failed with path %q, retrying with '.': %v", projectPath, err)
			state, err = wf.machine.Create(msg.UserID, routeResult.WorkflowType, ".", msg.Text)
		}
		if err != nil {
			log.Printf("[workflow-v2] Create failed: user=%s type=%s err=%v", msg.UserID, routeResult.WorkflowType, err)
			return workflowIMRouteResult{}
		}
	}

	log.Printf("[workflow-v2] started: user=%s type=%s project=%s id=%s", msg.UserID, state.Type, state.ProjectPath, state.ID)

	// Run the first phase and return its output directly as the response.
	// The phase runs as a single agent loop call — loop ends, output returned.
	// User sees the document and the workflow waits for their next message.
	// Note: emitWorkflowV2Progress is called inside runWorkflowV2Phase (both
	// the form path and the agent-loop path), so no need to call it here.
	return h.runWorkflowV2Phase(msg.UserID, state, "")
}

func (h *IMMessageHandler) handleWorkflowV2Action(msg IMUserMessage, hr *v2.HandleResult) workflowIMRouteResult {
	if hr == nil {
		return workflowIMRouteResult{}
	}
	switch hr.Action {
	case v2.ActionShowForm:
		if hr.Phase != nil && hr.Phase.InputSchema != nil {
			prefilled := h.prefillWorkflowFormFields(msg.UserID, hr.Phase, msg.Text)
			hr.PrefilledData = prefilled
			h.emitWorkflowV2PhaseForm(msg.UserID, hr.State, hr.Phase, prefilled)
			h.emitWorkflowV2Progress(msg.UserID, hr.State)
			return workflowIMRouteResult{Response: &IMAgentResponse{Text: "请在右侧任务面板填写信息后提交。"}}
		}
		return h.runWorkflowV2Phase(msg.UserID, hr.State, "")
	case v2.ActionRunPhase:
		if hr.Phase != nil {
			switch hr.Phase.ExecMode {
			case v2.ExecModeSubAgent:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=subagent for phase=%s", hr.Phase.ID)
				if wf := h.getWorkflowV2(); wf != nil {
					if err := wf.machine.MarkPhaseExecuting(msg.UserID); err != nil {
						log.Printf("[workflow-v2] MarkPhaseExecuting failed: %v", err)
					}
					if fresh := wf.machine.GetActive(msg.UserID); fresh != nil {
						hr.State = fresh
					}
				}
				h.emitWorkflowV2Progress(msg.UserID, hr.State)
				// Pure sticky coding must not steal the workflow implementation turn.
				h.clearPendingPureCodingTemplateExecution(msg.UserID)
				h.pendingV2SubAgentExecution.Store(msg.UserID, true)
				h.workflowOriginalRequest.Store(msg.UserID, "执行编码任务")
				h.workflowAgentLoopMarker.Store(msg.UserID, true)
				return workflowIMRouteResult{WorkflowAgentLoop: true, WorkflowDocPhase: false}
			case v2.ExecModeRemoteSubAgent:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=remote_subagent for phase=%s", hr.Phase.ID)
				if resp := h.launchRemoteExperimentOrchestrator(msg.UserID, hr.State); resp != nil {
					return workflowIMRouteResult{Response: resp}
				}
				log.Printf("[workflow-v2] RemoteExperimentOrchestrator launch failed, falling back to agent loop")
			case v2.ExecModeAutoFromPrev:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=auto_from_prev for phase=%s, auto-completing", hr.Phase.ID)
				if wf := h.getWorkflowV2(); wf != nil {
					if updatedState := wf.machine.GetActive(msg.UserID); updatedState != nil {
						h.emitWorkflowV2Progress(msg.UserID, updatedState)
					}
				}
				return workflowIMRouteResult{Response: &IMAgentResponse{Text: "工作流已完成"}}
			default:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=default for phase=%s, running as agent loop", hr.Phase.ID)
			}
		}
		return h.runWorkflowV2Phase(msg.UserID, hr.State, "")
	case v2.ActionModify:
		return h.runWorkflowV2Phase(msg.UserID, hr.State, hr.ModifyHint)
	case v2.ActionConfirmed:
		h.emitWorkflowV2Progress(msg.UserID, hr.State)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "所有阶段已完成！工作流结束。"}}
	case v2.ActionCancelled:
		h.workflowV2Adapters.Delete(msg.UserID)
		h.clearCodingExecCheckpoint(msg.UserID)
		if hr.State != nil {
			emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{"id": hr.State.ID, "status": string(v2.StatusCancelled), "type": hr.State.Type, "project_path": workflowEventProjectPath(hr.State), "event_scope_id": h.app.getEventScopeID(hr.State.UserID)})
		} else {
			emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		}
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", map[string]interface{}{"event_scope_id": h.app.getEventScopeID(msg.UserID)})
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "工作流已取消"}}
	case v2.ActionCancelAndExecute:
		h.workflowV2Adapters.Delete(msg.UserID)
		h.clearCodingExecCheckpoint(msg.UserID)
		originalRequest := ""
		if hr.State != nil {
			originalRequest = hr.State.Summary
		}
		log.Printf("[workflow-v2] ActionCancelAndExecute: user=%s original_len=%d", msg.UserID, len([]rune(originalRequest)))
		if hr.State != nil {
			emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{"id": hr.State.ID, "status": string(v2.StatusCancelled), "type": hr.State.Type, "project_path": workflowEventProjectPath(hr.State), "event_scope_id": h.app.getEventScopeID(hr.State.UserID)})
		} else {
			emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		}
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", map[string]interface{}{"event_scope_id": h.app.getEventScopeID(msg.UserID)})
		if originalRequest != "" {
			h.pendingCancelExecuteRequest.Store(msg.UserID, originalRequest)
		}
		return workflowIMRouteResult{SkipNeedsConfirmGate: true}
	case v2.ActionPassThrough:
		return workflowIMRouteResult{SkipNeedsConfirmGate: true}
	}
	return workflowIMRouteResult{}
}

func (h *IMMessageHandler) runWorkflowV2Phase(userID string, state *v2.WorkflowState, modifyHint string) workflowIMRouteResult {
	if state == nil {
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "工作流阶段无法启动：工作流状态不存在", Error: "workflow state is nil"}}
	}
	// Templates may gain explicit data dependencies after a workflow has already
	// been persisted. Backfill only missing metadata so resumed workflows receive
	// the same safe handoff semantics as newly created ones.
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil && wf.machine.GetRegistry() != nil {
		if tmpl := wf.machine.GetRegistry().Get(state.Type); v2.BackfillPhaseDependenciesFromTemplate(state, tmpl) {
			if err := wf.store.Save(state); err != nil {
				log.Printf("[workflow-v2] dependency metadata backfill persistence failed: user=%s err=%v", userID, err)
			}
		}
	}
	phase := state.ActivePhase()
	if phase == nil {
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "工作流已完成"}}
	}
	if phase.InputSchema != nil && phase.FormData == nil {
		prefilled := h.prefillWorkflowFormFields(userID, phase, "")
		h.emitWorkflowV2PhaseForm(userID, state, phase, prefilled)
		h.emitWorkflowV2Progress(userID, state)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "请在右侧面板填写信息后提交。"}}
	}
	if missing := v2.MissingFullDependencies(state); len(missing) > 0 {
		errText := fmt.Sprintf("阶段 %s 缺少已确认的前序产出物：%s", phase.ID, strings.Join(missing, "、"))
		log.Printf("[workflow-v2] phase blocked by missing dependencies: user=%s type=%s phase=%s missing=%v", userID, state.Type, phase.ID, missing)
		h.emitWorkflowV2Progress(userID, state)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: errText + "。请恢复或重新执行缺失阶段后再继续。", Error: errText}}
	}
	if err := ensureWorkflowV2PhaseWorkDir(state); err != nil {
		log.Printf("[workflow-v2] phase workdir unavailable: user=%s type=%s phase=%s project=%q err=%v", userID, state.Type, phase.ID, state.ProjectPath, err)
		h.emitWorkflowV2Progress(userID, state)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: fmt.Sprintf("工作流阶段无法启动：%s", err.Error()), Error: err.Error()}}
	}
	h.emitWorkflowV2Progress(userID, state)
	phasePrompt := v2.BuildPhasePrompt(state)
	if modifyHint != "" {
		if workflowV2IsArtifactPhase(phase) {
			phasePrompt += "\n\n## 用户修改意见\n\n" + modifyHint + "\n\n请根据以上修改意见重新生成最终产物文件，不要改为输出文档或需求澄清。"
		} else {
			phasePrompt += "\n\n## 用户修改意见\n\n" + modifyHint + "\n\n请根据以上修改意见重新生成本阶段文档。"
		}
	}
	log.Printf("[workflow-v2] stashedPhasePrompt.Store: key=%q len=%d", userID, len(phasePrompt))
	h.stashedPhasePrompt.Store(userID, phasePrompt)
	h.workflowAgentLoopMarker.Store(userID, true)
	if h.memory != nil {
		h.memory.Clear(userID)
	}
	phaseUserText := workflowV2PhaseUserRequest(phase)
	if modifyHint != "" {
		if workflowV2IsArtifactPhase(phase) {
			phaseUserText += "\n\n用户修改意见：" + modifyHint + "。直接重新生成并发送最终文件。"
		} else {
			phaseUserText = fmt.Sprintf("请根据修改意见重新生成「%s」的完整文档。直接输出完整内容。", phase.Name)
		}
	} else if !workflowV2IsArtifactPhase(phase) && phase.FormData != nil && len(phase.FormData) > 0 {
		phaseUserText = buildFormDataInlinedUserText(phase)
	}
	h.workflowOriginalRequest.Store(userID, phaseUserText)
	log.Printf("[workflow-v2] running phase: user=%s type=%s phase=%s project=%s", userID, state.Type, phase.ID, state.ProjectPath)
	return workflowIMRouteResult{WorkflowAgentLoop: true, WorkflowDocPhase: phase.NeedsConfirm, WorkflowPhaseID: phase.ID, PhasePrompt: phasePrompt}
}

func workflowV2IsArtifactPhase(phase *v2.Phase) bool {
	return phase != nil && (phase.Kind == v2.PhaseKindArtifactGeneration || phase.MutationScope == v2.MutationScopeArtifact)
}

func workflowV2PhaseUserRequest(phase *v2.Phase) string {
	if phase == nil {
		return "请执行当前工作流阶段。"
	}
	if workflowV2IsArtifactPhase(phase) {
		return fmt.Sprintf("立即执行「%s」产物生成：系统提示中已提供所有前序阶段的权威内容，直接据此生成并发送最终文件。不要询问主题、受众、页数或要点；不要搜索项目目录、PDF、记忆或历史对话来补充上下文；不要输出计划或 Markdown 文档。必须调用适当的生成工具（PPT 阶段优先运行 pptx-generator），成功后发送最终 .pptx 文件。", phase.Name)
	}
	return fmt.Sprintf("请现在生成「%s」阶段的完整文档内容。不要引用或指向之前的对话，直接在本次回复中输出完整文档。", phase.Name)
}
func ensureWorkflowV2PhaseWorkDir(state *v2.WorkflowState) error {
	if state == nil {
		return nil
	}
	projectPath, created, err := ensureAbsoluteDirectoryPath(state.ProjectPath, "workflow project path")
	state.ProjectPath = projectPath
	if err != nil {
		return err
	}
	if projectPath == "" {
		return nil
	}
	if created {
		log.Printf("[workflow-v2] created phase workdir type=%s phase=%s project=%q", state.Type, activeWorkflowV2PhaseID(state), projectPath)
	}
	return nil
}

func activeWorkflowV2PhaseID(state *v2.WorkflowState) string {
	if state == nil {
		return ""
	}
	if phase := state.ActivePhase(); phase != nil {
		return phase.ID
	}
	return ""
}

// buildFormDataInlinedUserText constructs a user message that directly embeds
// the FormData key-value pairs. This is the mechanism-level fix for models that
// ignore system prompt sections: by putting the actual data in the user message
// (highest compliance weight), the model physically sees it as "the user's request"
// rather than a background instruction it can skip.
//
// The system prompt still has the full FormData section (for structured field
// labels, variant info, and parsing guidance). The user message provides a
// redundant but authoritative copy that weak models cannot miss.
func buildFormDataInlinedUserText(phase *v2.Phase) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("我已通过表单提交了「%s」阶段的全部输入信息，具体如下：\n\n", phase.Name))
	sb.WriteString(v2.RenderFormDataFields(phase, false))
	sb.WriteString("\n请直接基于以上信息生成本阶段完整文档。")
	sb.WriteString("如果信息中包含文件路径，请直接用 read_file 或 bash 读取该文件内容后再生成文档。")
	return sb.String()
}

// getWorkflowV2 returns the V2 workflow state, or nil if not initialized.
func (h *IMMessageHandler) getWorkflowV2() *workflowV2State {
	if h == nil || h.app == nil {
		return nil
	}
	return h.app.workflowV2
}

// emitWorkflowV2PhaseUpdateEvent builds and emits the workflow:phase_update event payload.
// Extracted so it can be reused by both emitWorkflowV2Progress (which also emits
// suggest_maximize) and emitWorkflowV2ProgressPayloadOnly (which does not).
func (h *IMMessageHandler) emitWorkflowV2PhaseUpdateEvent(state *v2.WorkflowState) {
	if h.app == nil || state == nil {
		return
	}
	activePhase := state.ActivePhase()
	activePhaseID := ""
	if activePhase != nil {
		activePhaseID = activePhase.ID
	}
	awaitingForm := activePhase != nil && activePhase.InputSchema != nil && activePhase.FormData == nil

	phases := make([]map[string]interface{}, len(state.Phases))
	for i, p := range state.Phases {
		expectsDoc := p.NeedsConfirm
		if p.ID == activePhaseID && awaitingForm {
			expectsDoc = false
		}
		phases[i] = map[string]interface{}{
			"id":               p.ID,
			"name":             p.Name,
			"status":           string(p.Status),
			"needs_confirm":    p.NeedsConfirm,
			"expects_document": expectsDoc,
		}
	}

	phaseOutputs := make(map[string]interface{})
	for _, p := range state.Phases {
		if p.Output != "" {
			phaseOutputs[p.ID] = p.Output
		}
	}

	payload := map[string]interface{}{
		"id":             state.ID,
		"status":         string(state.Status),
		"type":           state.Type,
		"current_phase":  activePhaseID,
		"phases":         phases,
		"phase_outputs":  phaseOutputs,
		"project_path":   workflowEventProjectPath(state),
		"event_scope_id": h.app.getEventScopeID(state.UserID),
	}
	if awaitingForm {
		payload["awaiting_form"] = true
	}
	emitWorkflowV2Event(h.app, "workflow:phase_update", payload)
}

// emitWorkflowV2ProgressPayloadOnly emits only the workflow:phase_update event
// without suggest_maximize side effects. Used for tab-switch refresh where the
// panel state is already restored and doesn't need maximize suggestions.
func (h *IMMessageHandler) emitWorkflowV2ProgressPayloadOnly(userID string, state *v2.WorkflowState) {
	if adapter := h.workflowV2GUIAdapter(userID); adapter != nil {
		// Keep V2 on the same persistence/instance-namespacing path as legacy
		// workflows. Previously V2 only emitted an event, so its documents were
		// never made available to the adapter's durable workflow store.
		_ = adapter.EmitPhaseUpdate(userID, mapV2StateToV1(state))
		if state != nil && state.Status != v2.StatusActive {
			h.workflowV2Adapters.Delete(userID)
		}
		return
	}
	h.emitWorkflowV2PhaseUpdateEvent(state)
}

// emitWorkflowV2Progress sends workflow phase update events to the frontend.
// Uses the same event name and data format so the frontend preview panel works.
func (h *IMMessageHandler) emitWorkflowV2Progress(userID string, state *v2.WorkflowState) {
	if h.app == nil || state == nil {
		return
	}
	if adapter := h.workflowV2GUIAdapter(userID); adapter != nil {
		_ = adapter.EmitPhaseUpdate(userID, mapV2StateToV1(state))
	} else {
		h.emitWorkflowV2PhaseUpdateEvent(state)
	}

	// Also emit suggest_maximize for desktop panel to auto-expand.
	// But NOT when the active phase is waiting for form input — there's no
	// document content to preview yet, and opening the workflow doc panel would
	// obscure the AgentView form that the user needs to fill in.
	activePhase := state.ActivePhase()
	awaitingForm := activePhase != nil && activePhase.InputSchema != nil && activePhase.FormData == nil
	if state.Status == v2.StatusActive {
		if !awaitingForm {
			emitWorkflowV2Event(h.app, "workflow:suggest_maximize", map[string]interface{}{
				"workflow_type":  state.Type,
				"event_scope_id": h.app.getEventScopeID(state.UserID),
			})
		}
	} else {
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", map[string]interface{}{
			"event_scope_id": h.app.getEventScopeID(state.UserID),
		})
		// The per-owner adapter retains instance metadata for persistence. Once
		// the workflow is terminal, release it so a later task cannot inherit
		// stale state and long-running IM sessions do not retain adapters forever.
		h.workflowV2Adapters.Delete(userID)
	}
}

// emitWorkflowV2PhaseForm builds and emits an AG UI form from the phase's InputSchema.
// The form appears in the right-side task panel (AgentTaskPanel).
// prefilled contains auto-collected default values with provenance tracking.
func (h *IMMessageHandler) emitWorkflowV2PhaseForm(userID string, state *v2.WorkflowState, phase *v2.Phase, prefilled map[string]*v2.PrefilledValue) {
	if h == nil || h.app == nil || phase == nil || phase.InputSchema == nil {
		return
	}
	schema := phase.InputSchema
	workflowID := ""
	if state != nil {
		workflowID = state.ID
	}

	fields := make([]map[string]interface{}, 0, len(schema.Fields)+3)
	for _, f := range schema.Fields {
		field := map[string]interface{}{
			"name":  f.Name,
			"label": f.Label,
			"type":  f.Type,
		}
		if f.Required {
			field["required"] = true
		}
		if f.Sensitive {
			field["sensitive"] = true
		}
		if f.Description != "" {
			field["description"] = f.Description
		}
		if f.Placeholder != "" {
			field["placeholder"] = f.Placeholder
		}
		if len(f.Options) > 0 {
			opts := make([]map[string]string, len(f.Options))
			for i, o := range f.Options {
				opts[i] = map[string]string{"label": o.Label, "value": o.Value}
			}
			field["options"] = opts
		}
		// Apply prefilled value (from memory/knowledge/context) or static default
		if pv, ok := prefilled[f.Name]; ok && pv != nil && pv.Value != nil {
			field["value"] = pv.Value
			field["prefill_source"] = pv.Source
			if pv.SourceDetail != "" {
				field["prefill_detail"] = pv.SourceDetail
			}
			if pv.NeedsConfirm {
				field["prefill_needs_confirm"] = true
			}
		} else if f.Default != nil {
			field["value"] = f.Default
		}
		fields = append(fields, field)
	}

	// Hidden routing fields
	fields = append(fields, map[string]interface{}{"name": "_workflow_phase", "type": "hidden", "value": phase.ID})
	fields = append(fields, map[string]interface{}{"name": "_workflow_user_id", "type": "hidden", "value": userID})
	fields = append(fields, map[string]interface{}{"name": "_workflow_id", "type": "hidden", "value": workflowID})
	if scopeID := strings.TrimSpace(h.app.getEventScopeID(userID)); scopeID != "" {
		fields = append(fields, map[string]interface{}{"name": workflowFormEventScopeField, "type": "hidden", "value": scopeID})
	}

	viewID := "workflow:form:" + phase.ID
	view := map[string]interface{}{
		"type":        "form",
		"id":          viewID,
		"title":       schema.Title,
		"description": schema.Description,
		"fields":      fields,
		"submitLabel": "提交",
		"meta": map[string]interface{}{
			"source":   "workflow_v2.phase_form",
			"phase_id": phase.ID,
		},
	}
	// Declare resume upload capability — frontend renders file upload entry at form top
	if schema.AcceptsResume {
		view["accepts_resume"] = true
	}
	// Declare supplementary documents upload capability
	if schema.AcceptsSupplementary != nil {
		suppConfig := map[string]interface{}{
			"label":       schema.AcceptsSupplementary.Label,
			"description": schema.AcceptsSupplementary.Description,
		}
		if schema.AcceptsSupplementary.MaxFiles > 0 {
			suppConfig["max_files"] = schema.AcceptsSupplementary.MaxFiles
		}
		if len(schema.AcceptsSupplementary.AcceptedTypes) > 0 {
			suppConfig["accepted_types"] = schema.AcceptsSupplementary.AcceptedTypes
		}
		view["accepts_supplementary"] = suppConfig
	}
	// Pass through variants (mutually exclusive field groups) if defined.
	// Prefill values are applied to variant fields the same way as common fields —
	// this is critical for forms where ALL user-facing fields live inside variants
	// (e.g. academic application forms: schema.Fields=[] and all fields are in manual_mode variant).
	if len(schema.Variants) > 0 {
		variants := make([]map[string]interface{}, 0, len(schema.Variants))
		for _, v := range schema.Variants {
			variantFields := make([]map[string]interface{}, 0, len(v.Fields))
			for _, f := range v.Fields {
				vf := map[string]interface{}{
					"name":  f.Name,
					"label": f.Label,
					"type":  f.Type,
				}
				if f.Required {
					vf["required"] = true
				}
				if f.Sensitive {
					vf["sensitive"] = true
				}
				if f.Description != "" {
					vf["description"] = f.Description
				}
				if f.Placeholder != "" {
					vf["placeholder"] = f.Placeholder
				}
				if len(f.Options) > 0 {
					opts := make([]map[string]string, len(f.Options))
					for i, o := range f.Options {
						opts[i] = map[string]string{"label": o.Label, "value": o.Value}
					}
					vf["options"] = opts
				}
				// Apply prefilled value (from memory/knowledge) or static default.
				if pv, ok := prefilled[f.Name]; ok && pv != nil && pv.Value != nil {
					vf["value"] = pv.Value
					vf["prefill_source"] = pv.Source
					if pv.SourceDetail != "" {
						vf["prefill_detail"] = pv.SourceDetail
					}
					if pv.NeedsConfirm {
						vf["prefill_needs_confirm"] = true
					}
				} else if f.Default != nil {
					vf["value"] = f.Default
				}
				variantFields = append(variantFields, vf)
			}
			variants = append(variants, map[string]interface{}{
				"id":     v.ID,
				"label":  v.Label,
				"fields": variantFields,
			})
		}
		view["variants"] = variants
	}
	// Coding remote variant: inject SSH host options after variants exist.
	if h.app != nil {
		injectSSHProfileOptionsIntoAgentView(view, h.app.sshHostEntries())
	}
	h.app.emitAgentView(view)
	log.Printf("[workflow-v2] emitted AG UI form: phase=%s fields=%d variants=%d prefilled=%d", phase.ID, len(schema.Fields), len(schema.Variants), len(prefilled))
}

// emitWorkflowV2FormWithPrefill re-emits the AG UI form in manual_mode with
// pre-filled values from resume parsing. The form is switched to manual_mode
// so all fields are visible, and extracted values are set as default values.
// The user reviews, fills any gaps (e.g. missing required fields), then submits.
func (h *IMMessageHandler) emitWorkflowV2FormWithPrefill(userID, phaseID string, schema *v2.PhaseInputSchema, prefilled map[string]*v2.PrefilledValue) {
	if h == nil || h.app == nil || schema == nil {
		return
	}

	wf := h.getWorkflowV2()
	if wf == nil {
		return
	}
	state := wf.machine.GetActive(userID)
	workflowID := ""
	if state != nil {
		workflowID = state.ID
	}

	// Find the manual_mode variant
	var manualVariant *v2.PhaseInputVariant
	for i := range schema.Variants {
		if schema.Variants[i].ID == "manual_mode" {
			manualVariant = &schema.Variants[i]
			break
		}
	}
	if manualVariant == nil {
		log.Printf("[workflow-v2] emitWorkflowV2FormWithPrefill: no manual_mode variant found")
		return
	}

	// Build the form fields from manual_mode variant with prefilled values injected
	fields := make([]map[string]interface{}, 0, len(manualVariant.Fields)+3)
	for _, f := range manualVariant.Fields {
		field := map[string]interface{}{
			"name":  f.Name,
			"label": f.Label,
			"type":  f.Type,
		}
		if f.Required {
			field["required"] = true
		}
		if f.Description != "" {
			field["description"] = f.Description
		}
		if f.Placeholder != "" {
			field["placeholder"] = f.Placeholder
		}
		if len(f.Options) > 0 {
			opts := make([]map[string]string, len(f.Options))
			for i, o := range f.Options {
				opts[i] = map[string]string{"label": o.Label, "value": o.Value}
			}
			field["options"] = opts
		}
		// Inject prefilled value from resume extraction
		if pv, ok := prefilled[f.Name]; ok && pv != nil && pv.Value != nil {
			field["value"] = pv.Value
			field["prefill_source"] = pv.Source
			if pv.SourceDetail != "" {
				field["prefill_detail"] = pv.SourceDetail
			}
		} else if f.Default != nil {
			field["value"] = f.Default
		}
		fields = append(fields, field)
	}

	// Hidden routing fields — force manual_mode
	fields = append(fields, map[string]interface{}{"name": "_workflow_phase", "type": "hidden", "value": phaseID})
	fields = append(fields, map[string]interface{}{"name": "_workflow_user_id", "type": "hidden", "value": userID})
	fields = append(fields, map[string]interface{}{"name": "_workflow_id", "type": "hidden", "value": workflowID})
	if scopeID := strings.TrimSpace(h.app.getEventScopeID(userID)); scopeID != "" {
		fields = append(fields, map[string]interface{}{"name": workflowFormEventScopeField, "type": "hidden", "value": scopeID})
	}
	fields = append(fields, map[string]interface{}{"name": "_agent_view_variant", "type": "hidden", "value": "manual_mode"})

	viewID := "workflow:form:" + phaseID
	description := fmt.Sprintf("已从简历中提取 %d 个字段（绿色标记）。请核对信息，补充未提取到的字段后提交。", len(prefilled))

	view := map[string]interface{}{
		"type":        "form",
		"id":          viewID,
		"title":       schema.Title,
		"description": description,
		"fields":      fields,
		"submitLabel": "确认提交",
		"meta": map[string]interface{}{
			"source":        "workflow_v2.resume_prefill",
			"phase_id":      phaseID,
			"resume_mode":   true,
			"prefill_count": len(prefilled),
		},
	}
	// No variants — we force manual_mode as a flat form for user review
	h.app.emitAgentView(view)
	log.Printf("[workflow-v2] re-emitted form with %d prefilled fields for user review: phase=%s", len(prefilled), phaseID)
}

// handleWorkflowV2FormSubmit processes the user's AG UI form submission for a
// workflow v2 phase. It stores the form data in the state machine, dismisses
// the form panel, and auto-triggers phase execution via an async message
// ("继续") through the normal message path. The agent loop starts without
// requiring the user to manually type "继续".
func (h *IMMessageHandler) handleWorkflowV2FormSubmit(userID, phaseID string, data map[string]interface{}, requestID string) *IMAgentResponse {
	wf := h.getWorkflowV2()
	if wf == nil {
		return &IMAgentResponse{Text: "工作流未初始化", Error: "no workflow v2 state"}
	}

	// Strip hidden routing fields from form data before storing.
	// Preserve _agent_view_variant (needed by BuildPhasePrompt to identify active variant).
	cleanData := make(map[string]interface{}, len(data))
	for k, v := range data {
		if k == "" {
			continue
		}
		if k[0] == '_' && k != "_agent_view_variant" {
			continue
		}
		cleanData[k] = v
	}

	// --- Resume mode handling ---
	// If user selected "resume_mode" variant and provided a file path,
	// parse the resume and RE-EMIT the form in manual_mode with pre-filled values.
	// The user reviews extracted fields, fills any gaps, then submits again.
	if activeVariant, _ := cleanData["_agent_view_variant"].(string); activeVariant == "resume_mode" {
		resumePath, _ := cleanData["resume_file"].(string)
		if resumePath == "" {
			return &IMAgentResponse{Text: "请选择简历文件", Error: "resume_file is empty"}
		}

		// Get the phase schema for field extraction
		state := wf.machine.GetActive(userID)
		var phaseSchema *v2.PhaseInputSchema
		if state != nil {
			if phase := state.ActivePhase(); phase != nil {
				phaseSchema = phase.InputSchema
			}
		}
		if phaseSchema == nil || len(phaseSchema.Variants) == 0 {
			return &IMAgentResponse{Text: "工作流配置错误：找不到表单字段定义", Error: "no input schema with variants"}
		}

		// Extract text from resume file
		resumeText, err := extractTextFromFile(resumePath)
		if err != nil {
			return &IMAgentResponse{Text: fmt.Sprintf("读取简历文件失败: %v", err), Error: err.Error()}
		}
		resumeText = sanitizeExtractedText(resumeText)
		if strings.TrimSpace(resumeText) == "" {
			return &IMAgentResponse{Text: "简历文件内容为空", Error: "empty resume text"}
		}

		// Parse resume using LLM
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		caller := &appResumeLLMCaller{app: h.app}
		result, err := v2.ParseResumeForSchema(ctx, v2.ResumeParseRequest{
			ResumeText: resumeText,
			Schema:     phaseSchema,
		}, caller)
		if err != nil {
			errMsg := "简历解析失败: " + err.Error()
			log.Printf("[workflow-v2] resume parse failed: user=%s phase=%s err=%v", userID, phaseID, err)
			return &IMAgentResponse{Text: errMsg, Error: errMsg}
		}

		prefilled := v2.ResumeParseResultToPrefilled(result, phaseSchema)
		if len(prefilled) == 0 {
			return &IMAgentResponse{Text: "未能从简历中提取到有效信息，请切换到手动填写模式", Error: "no fields extracted"}
		}

		log.Printf("[workflow-v2] resume_mode: extracted %d fields, re-emitting form in manual_mode for user review",
			len(prefilled))

		// Sediment to memory asynchronously for future recall
		go func() {
			formData := make(map[string]interface{}, len(prefilled))
			for name, pv := range prefilled {
				formData[name] = pv.Value
			}
			activeState := wf.machine.GetActive(userID)
			h.sedimentFormDataToMemory(userID, phaseID, formData, activeState)
		}()

		// Re-emit the form in manual_mode with pre-filled values from resume.
		// The user reviews extracted fields, fills any missing required fields, then submits again.
		h.emitWorkflowV2FormWithPrefill(userID, phaseID, phaseSchema, prefilled)

		return &IMAgentResponse{
			Text:      fmt.Sprintf("已从简历中提取 %d 个字段，请在右侧面板核对信息并补充未提取到的字段后提交。", len(prefilled)),
			KeepPanel: true, // prevent frontend from auto-dismissing the AG view panel
		}
	}

	// Coding remote variant: resolve SSH profile / new connection → host/user/port.
	if h.app != nil {
		if err := resolveCodingWorkflowRemoteFormData(h.app.sshHostEntries(), cleanData); err != nil {
			return &IMAgentResponse{Text: err.Error(), Error: err.Error()}
		}
	}

	// Session-only remote creds (password/key) + sticky non-secret meta +
	// left-sidebar remote_coding_dev task (tags only; no password).
	if formDataTrimString(cleanData, "_agent_view_variant") == workflowFormExecRemote {
		creds := captureCodingWorkflowRemoteCreds(cleanData)
		h.storeCodingWorkflowRemoteCreds(userID, creds)
		h.bindCodingWorkflowRemoteSticky(userID, creds)
		// Strip secrets before durable FormData / task sync.
		delete(cleanData, workflowFormSSHPasswordField)
		// Sync after secret strip; may write remote_task_path into cleanData.
		stateForSync := wf.machine.GetActive(userID)
		h.syncCodingWorkflowRemoteTask(userID, cleanData, stateForSync)
	}

	// Local project_path validation (remote uses remote_workdir, not local paths).
	if pp := strings.TrimSpace(fmt.Sprint(cleanData[workflowFormProjectPathField])); pp != "" && pp != "<nil>" {
		if normalized, _, err := normalizeWorkflowProjectPath(pp); err != nil {
			return &IMAgentResponse{Text: fmt.Sprintf("项目路径无效: %v", err), Error: err.Error()}
		} else {
			cleanData[workflowFormProjectPathField] = normalized
		}
	}

	if err := wf.machine.SubmitForm(userID, cleanData); err != nil {
		log.Printf("[workflow-v2] SubmitForm failed: user=%s phase=%s err=%v", userID, phaseID, err)
		return &IMAgentResponse{Text: "表单提交失败: " + err.Error(), Error: err.Error()}
	}

	log.Printf("[workflow-v2] form submitted: user=%s phase=%s fields=%d", userID, phaseID, len(cleanData))

	// Emit progress so the frontend dashboard updates.
	state := wf.machine.GetActive(userID)
	if state != nil {
		h.emitWorkflowV2Progress(userID, state)
	}

	// Sediment confirmed form data to long-term memory for future prefill reuse.
	// Only factual user information is persisted (name, institution, etc.) — not
	// task-specific creative content. Runs async to not block the response.
	go h.sedimentFormDataToMemory(userID, phaseID, cleanData, state)

	// Dismiss the AG UI form panel after successful submission.
	if h.app != nil {
		h.app.emitAgentViewLifecycle("dismiss", map[string]interface{}{
			"view_id":          "workflow:form:" + phaseID,
			"workflow_phase":   phaseID,
			"workflow_user_id": userID,
		})
	}

	// Build echo text summarizing what the user submitted.
	echoText := buildFormSubmissionEcho(state, cleanData)

	// Auto-trigger phase execution immediately after form submission.
	// No need to require user to type "继续" — the form was the data collection step,
	// execution should begin automatically once data is available.
	if state != nil {
		phase := state.ActivePhase()
		if phase != nil && phase.FormData != nil {
			autoContinueText := "继续"

			// Emit echo as an immediate streaming token so the frontend shows it
			// while the async agent loop starts. In deferred mode, response.Text
			// is not rendered by the frontend — only streaming events are.
			if h.app != nil && h.app.ctx != nil && requestID != "" && echoText != "" {
				h.app.emitStreamingToken(requestID, userID, echoText+"\n\n")
			}

			// Form submission is a synchronous Wails binding call — we cannot run
			// the agent loop inline. Dispatch an async message through the normal
			// message path so that the workflow routing kicks in (ActionRunPhase),
			// sets the marker, and the agent loop actually starts.
			//
			// Return Deferred=true with the same RequestID so the frontend keeps
			// the streaming round open and associates async tokens/response with it.
			if h.app != nil {
				go func() {
					log.Printf("[workflow-v2] form auto-continue: dispatching agent loop for user=%s requestID=%s text_len=%d", userID, requestID, len([]rune(autoContinueText)))
					if _, err := h.app.continueAIAssistantWorkflowMessage(userID, autoContinueText, requestID); err != nil {
						log.Printf("[workflow-v2] form auto-continue failed: user=%s err=%v", userID, err)
						// Emit a final response to resolve the frontend's deferred round.
						// Without this, the round stays in "requesting" state with spinner forever.
						h.app.emitAIAssistantResponse(requestID, &IMAgentResponse{
							Text:       echoText + "\n\n自动执行失败，请发送「继续」手动触发。",
							SessionKey: userID,
						})
					}
				}()
			}
			return &IMAgentResponse{
				Text:      echoText,
				RequestID: requestID,
				Deferred:  true,
			}
		}
	}

	return &IMAgentResponse{
		Text: echoText + "\n\n发送「继续」开始生成文档。",
	}
}

// document output is produced during a workflow phase.
func (h *IMMessageHandler) recordWorkflowV2Output(userID, output string) {
	wf := h.getWorkflowV2()
	if wf == nil {
		return
	}
	if err := wf.machine.RecordOutput(userID, output); err != nil {
		log.Printf("[workflow-v2] RecordOutput failed: user=%s err=%v", userID, err)
		return
	}
	// Load state after RecordOutput. Use store.Load directly instead of GetActive
	// because RecordOutput may have completed the workflow (Status=Completed),
	// and GetActive filters out non-active workflows. We still need to emit
	// progress and doc_update events for the completed state.
	state, _ := wf.store.Load(userID)
	if state == nil {
		return
	}
	if h.app != nil && h.app.workflowEngine != nil {
		h.app.workflowEngine.StoreActiveState(userID, mapV2StateToV1(state))
	}
	// Emit phase_update so the progress board reflects the new state.
	h.emitWorkflowV2Progress(userID, state)
	// Emit doc_update for the phase that just produced output. This is essential
	// for the frontend's docUpdatePhaseIDsRef tracking — once a phase receives
	// doc_update, subsequent phase_update events won't overwrite its content.
	// The phase that just produced output depends on NeedsConfirm:
	//   - NeedsConfirm=true: RecordOutput sets WaitingConfirm, ActivePhase() still has output
	//   - NeedsConfirm=false: RecordOutput auto-advances CurrentPhase, completed phase is at CurrentPhase-1
	if phase := state.ActivePhase(); phase != nil && phase.Output != "" {
		// NeedsConfirm=true path: current phase has output and is WaitingConfirm
		h.emitDocUpdateV2(userID, phase.ID, phase.Output)
	} else if state.CurrentPhase > 0 && state.CurrentPhase <= len(state.Phases) {
		// NeedsConfirm=false path (or workflow completed): look at the just-completed phase
		prevPhase := &state.Phases[state.CurrentPhase-1]
		if prevPhase.Output != "" {
			h.emitDocUpdateV2(userID, prevPhase.ID, prevPhase.Output)
		}
	}
}

// emitDocUpdateV2 sends document update to frontend preview panel.
// Content is sanitized to remove role-prefix hallucinations before emission.
func (h *IMMessageHandler) emitDocUpdateV2(userID, phaseID, content string) {
	if h.app == nil {
		return
	}
	content = stripRolePrefixHallucination(content)
	wf := h.getWorkflowV2()
	projectPath := ""
	workflowID := ""
	if wf != nil {
		// Use store.Load instead of GetActive — GetActive filters out completed
		// workflows, but we still need project_path and workflow_id for event
		// routing when emitting doc_update for the final phase of a completed workflow.
		if state, _ := wf.store.Load(userID); state != nil {
			projectPath = workflowEventProjectPath(state)
			workflowID = state.ID
		}
	}
	if adapter := h.workflowV2GUIAdapter(userID); adapter != nil {
		// Persist before publishing the UI update. This is intentionally shared
		// with the legacy path so a completed V2 phase always has a durable
		// Markdown representation in addition to its authoritative SQLite output.
		_ = adapter.EmitDocUpdate(userID, phaseID, content)
		return
	}
	emitWorkflowV2Event(h.app, "workflow:doc_update", map[string]interface{}{
		"phase_id":       phaseID,
		"content":        content,
		"project_path":   projectPath,
		"workflow_id":    workflowID,
		"event_scope_id": h.app.getEventScopeID(userID),
	})
}

func (h *IMMessageHandler) workflowV2GUIAdapter(userID string) *GUIWorkflowAdapter {
	if h == nil || h.app == nil || h.app.workflowEngine == nil {
		return nil
	}
	engine := h.app.workflowEngine
	if adapter, ok := engine.GetCallbacks().(*GUIWorkflowAdapter); ok {
		return adapter
	}
	// V2 workflows bypass the legacy engine's normal start path. Keep an
	// adapter dedicated to each workflow owner instead of replacing another
	// host callback or sharing mutable instance state between users.
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	if adapter, ok := h.workflowV2Adapters.Load(userID); ok {
		return adapter.(*GUIWorkflowAdapter)
	}
	adapter := NewGUIWorkflowAdapter(h.app, engine)
	actual, _ := h.workflowV2Adapters.LoadOrStore(userID, adapter)
	return actual.(*GUIWorkflowAdapter)
}

// cancelWorkflowV2 cancels any active V2 workflow for the user.
func (h *IMMessageHandler) cancelWorkflowV2(userID string) {
	wf := h.getWorkflowV2()
	if wf == nil {
		return
	}
	// Read state before Cancel() so we can emit a targeted reset event.
	state := wf.machine.GetActive(userID)
	wf.machine.Cancel(userID)
	h.clearCodingExecCheckpoint(userID)
	// Emit targeted reset: include workflow_id and project_path so only
	// the matching tab clears its preview panel.
	if state != nil {
		emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
			"id":             state.ID,
			"status":         string(v2.StatusCancelled),
			"type":           state.Type,
			"project_path":   workflowEventProjectPath(state),
			"event_scope_id": h.app.getEventScopeID(state.UserID),
		})
	} else {
		emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
	}
	emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", map[string]interface{}{
		"event_scope_id": h.app.getEventScopeID(userID),
	})
}

// isWorkflowV2Active returns true if user has an active V2 workflow.
func (h *IMMessageHandler) isWorkflowV2Active(userID string) bool {
	wf := h.getWorkflowV2()
	if wf == nil {
		return false
	}
	return wf.machine.GetActive(userID) != nil
}

// handleWorkflowV2ExecutionPhase handles the implementation phase using TaskRunner + SubAgent.
// Delegates to the progress-aware path so cancel / failed-retry / checkpoint stay consistent.
func (h *IMMessageHandler) handleWorkflowV2ExecutionPhase(userID string, state *v2.WorkflowState) *IMAgentResponse {
	return h.handleWorkflowV2ExecutionPhaseWithProgress(userID, state, nil, nil, nil)
}

// handleWorkflowV2ExecutionPhaseWithProgress wraps handleWorkflowV2ExecutionPhase
// but uses the provided onProgress callback (which carries request_id context)
// instead of raw runtime.EventsEmit. This ensures progress events reach the frontend
// with the correct request_id for the active round.
func (h *IMMessageHandler) handleWorkflowV2ExecutionPhaseWithProgress(userID string, state *v2.WorkflowState, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	// Manual 重试失败 / 继续执行 — consume before the phase gate so a stale
	// marker is never left orphaned when the phase is no longer executable.
	if raw, ok := h.pendingCodingExecRetryAction.LoadAndDelete(userID); ok {
		if action, _ := raw.(string); action != "" {
			if state == nil || !state.IsExecutionPhase() {
				return &IMAgentResponse{Text: codingExecText(
					"Not in the coding execution phase — cannot retry. Please re-enter coding execution in the programming workflow.",
					"当前不在编码执行阶段，无法重试。请重新进入编程工作流的编码执行阶段。",
					"目前不在編碼執行階段，無法重試。請重新進入程式設計工作流的編碼執行階段。",
				)}
			}
			if resp := h.runCodingExecFromCheckpoint(userID, action, state, onProgress, onToken, parentLoop); resp != nil {
				return resp
			}
		}
	}

	if state == nil || !state.IsExecutionPhase() {
		return nil
	}

	// Coding workflow remote variant: execute tasks via RemoteCodingSubAgent/SSH.
	if isCodingWorkflowRemoteExecution(state) {
		return h.handleWorkflowV2RemoteExecutionPhaseWithProgress(userID, state, onProgress, onToken, parentLoop)
	}

	return h.handleWorkflowV2LocalExecutionPhaseWithProgress(userID, state, onProgress, onToken, parentLoop)
}

// handleWorkflowV2LocalExecutionPhaseWithProgress runs local CodingSubAgent tasks
// with cancel support, one automatic failed-only retry, and resume checkpoint.
func (h *IMMessageHandler) handleWorkflowV2LocalExecutionPhaseWithProgress(userID string, state *v2.WorkflowState, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	if state == nil || !state.IsExecutionPhase() {
		return nil
	}

	tasksPhaseOutput := getPhaseOutput(state, "tasks")
	if tasksPhaseOutput == "" {
		if wf := h.getWorkflowV2(); wf != nil {
			if fresh := wf.machine.GetActive(userID); fresh != nil {
				tasksPhaseOutput = getPhaseOutput(fresh, "tasks")
				if tasksPhaseOutput != "" {
					state = fresh
				}
			}
		}
	}
	if tasksPhaseOutput == "" {
		log.Printf("[workflow-v2] execution phase (with progress) but no task breakdown output, falling back")
		return nil
	}

	tasks := v2.ParseTaskList(tasksPhaseOutput)
	if len(tasks) == 0 {
		log.Printf("[workflow-v2] no tasks parsed from breakdown (output_len=%d), falling back", len(tasksPhaseOutput))
		return nil
	}

	if err := v2.EnsureProjectDir(state.ProjectPath); err != nil {
		log.Printf("[workflow-v2] failed to ensure project dir %s: %v", state.ProjectPath, err)
	}

	reqCtx := truncateRunesV2(getPhaseOutput(state, "requirements"), 500)
	designCtx := truncateRunesV2(getPhaseOutput(state, "design"), 500)
	totalTasks := len(tasks)

	log.Printf("[workflow-v2] starting local execution: %d tasks, project=%s", totalTasks, state.ProjectPath)

	if wf := h.getWorkflowV2(); wf != nil {
		if err := wf.machine.MarkPhaseExecuting(userID); err != nil {
			log.Printf("[workflow-v2] MarkPhaseExecuting failed: %v", err)
		}
		if fresh := wf.machine.GetActive(userID); fresh != nil {
			state = fresh
		}
	}
	h.emitWorkflowV2Progress(userID, state)

	emitProgress := func(msg string) {
		if onProgress != nil && strings.TrimSpace(msg) != "" {
			onProgress(msg)
		}
	}
	emitProgress(fmt.Sprintf(codingExecText(
		"① Local coding execution: %d tasks @ %s",
		"① 本机编码执行：共 %d 个任务 @ %s",
		"① 本機編碼執行：共 %d 個任務 @ %s",
	), totalTasks, state.ProjectPath))

	loopCtx := parentLoop
	ownLoop := false
	if loopCtx == nil {
		loopCtx = NewLoopContext("workflow-local-coding-exec", h.getMaclawAgentMaxIterations(), h.client)
		ownLoop = true
	}
	ensureLoopCtxUserID(loopCtx, userID)
	runCtx, cancelRun := loopCtx.Context()
	defer cancelRun()
	if ownLoop {
		defer func() {
			loopCtx.Cancel()
			loopCtx.Done()
		}()
	}

	reasoningToken := onToken
	if onToken != nil {
		reasoningToken = func(delta string) {
			if strings.HasPrefix(delta, "Browser:") || strings.HasPrefix(delta, "Browser：") {
				delta = strings.TrimPrefix(strings.TrimPrefix(delta, "Browser:"), "Browser：")
				delta = strings.TrimLeft(delta, " ")
			}
			onToken("\x01" + delta)
		}
	}

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, tokenCB func(string), progressCB func(string)) *v2.TaskRunResult {
		if task == nil {
			return &v2.TaskRunResult{Status: v2.TaskFailed, Error: "nil task"}
		}
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if progressCB != nil {
			progressCB(fmt.Sprintf(codingExecText("② Local task %d/%d: %s", "② 本机任务 %d/%d：%s", "② 本機任務 %d/%d：%s"), task.Index, totalTasks, task.Title))
		}
		v1Task := &TaskItem{
			Index:       task.Index,
			Title:       task.Title,
			Description: task.Description,
			Files:       task.Files,
			DependsOn:   task.DependsOn,
		}
		var fn subAgentTaskFunc
		if runTaskWithSubAgent != nil {
			fn = runTaskWithSubAgent
		} else {
			fn = RunTaskWithSubAgent
		}
		v1Result := fn(h, cfg, httpClient, v1Task, config.ProjectPath, config.RequirementsCtx, config.DesignCtx, nil, loopCtx, tokenCB, progressCB)
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if v1Result == nil {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskFailed, Error: "SubAgent returned nil"}
		}
		status := v2.TaskFailed
		switch v1Result.Status {
		case TaskExecPassed:
			status = v2.TaskPassed
		case TaskExecSkipped:
			status = v2.TaskSkipped
		}
		if progressCB != nil {
			switch status {
			case v2.TaskPassed:
				progressCB(fmt.Sprintf(codingExecText("✓ Local task %d/%d done: %s", "✓ 本机任务 %d/%d 完成：%s", "✓ 本機任務 %d/%d 完成：%s"), task.Index, totalTasks, task.Title))
			case v2.TaskSkipped:
				progressCB(fmt.Sprintf(codingExecText("⊘ Local task %d/%d skipped: %s", "⊘ 本机任务 %d/%d 跳过：%s", "⊘ 本機任務 %d/%d 跳過：%s"), task.Index, totalTasks, task.Title))
			default:
				progressCB(fmt.Sprintf(codingExecText("❌ Local task %d/%d failed: %s", "❌ 本机任务 %d/%d 失败：%s", "❌ 本機任務 %d/%d 失敗：%s"), task.Index, totalTasks, task.Title))
			}
		}
		return &v2.TaskRunResult{
			TaskIndex:     task.Index,
			Title:         task.Title,
			Status:        status,
			Summary:       v1Result.Summary,
			FilesCreated:  v1Result.FilesCreated,
			FilesModified: v1Result.FilesModified,
			Error:         v1Result.Error,
		}
	}

	config := v2.TaskRunnerConfig{
		ProjectPath:     state.ProjectPath,
		RequirementsCtx: reqCtx,
		DesignCtx:       designCtx,
		MaxRetries:      2,
		TDDMode:         true,
	}

	runner := v2.NewTaskRunner(config, subAgentFn)
	allResults := runner.RunAll(runCtx, tasks, reasoningToken, func(progress string) {
		log.Printf("[workflow-v2-exec] %s", progress)
		emitProgress(progress)
	})

	allResults = autoRetryFailedCodingTasks(runCtx, loopCtx, config, subAgentFn, tasks, allResults, reasoningToken, emitProgress, "local")

	cancelled := runCtx.Err() != nil || loopCtx.IsCancelled()
	report := formatTaskRunResultsReportEx(allResults, cancelled)
	if onToken != nil {
		onToken("\n\n" + report)
	}

	return h.finalizeCodingImplementation(userID, state, tasks, allResults, report, cancelled, false, codingExecCheckpoint{
		IsRemote:        false,
		Tasks:           tasks,
		Results:         allResults,
		RequirementsCtx: reqCtx,
		DesignCtx:       designCtx,
		ProjectPath:     state.ProjectPath,
		Cancelled:       cancelled,
	}, onProgress)
}

// handleWorkflowV2RemoteExecutionPhaseWithProgress runs coding workflow
// implementation tasks on a remote host via SSH + RemoteCodingSubAgent.
// parentLoop (when non-nil) shares cancellation with the AI assistant turn so
// the user can stop remote execution via the normal cancel control.
func (h *IMMessageHandler) handleWorkflowV2RemoteExecutionPhaseWithProgress(userID string, state *v2.WorkflowState, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	if state == nil || !state.IsExecutionPhase() {
		return nil
	}

	tasksPhaseOutput := getPhaseOutput(state, "tasks")
	if tasksPhaseOutput == "" {
		if wf := h.getWorkflowV2(); wf != nil {
			if fresh := wf.machine.GetActive(userID); fresh != nil {
				tasksPhaseOutput = getPhaseOutput(fresh, "tasks")
				if tasksPhaseOutput != "" {
					state = fresh
				}
			}
		}
	}
	if tasksPhaseOutput == "" {
		log.Printf("[workflow-v2-remote-exec] no task breakdown output, falling back")
		return nil
	}
	tasks := v2.ParseTaskList(tasksPhaseOutput)
	if len(tasks) == 0 {
		log.Printf("[workflow-v2-remote-exec] no tasks parsed, falling back")
		return nil
	}

	host, user, _, port, _ := codingWorkflowRemoteEnvFromState(state)
	if port <= 0 {
		port = 22
	}
	totalTasks := len(tasks)
	emitRemoteProgress := func(msg string) {
		if onProgress != nil && strings.TrimSpace(msg) != "" {
			onProgress(msg)
		}
	}

	// Cancellation: prefer the assistant turn's LoopContext so Stop cancels SSH work.
	loopCtx := parentLoop
	ownLoop := false
	if loopCtx == nil {
		loopCtx = NewLoopContext("workflow-remote-coding-exec", h.getMaclawAgentMaxIterations(), h.client)
		ownLoop = true
	}
	ensureLoopCtxUserID(loopCtx, userID)
	runCtx, cancelRun := loopCtx.Context()
	defer cancelRun()
	if ownLoop {
		defer func() {
			loopCtx.Cancel()
			loopCtx.Done()
		}()
	}

	if loopCtx.IsCancelled() || runCtx.Err() != nil {
		return &IMAgentResponse{Text: codingExecText(
			"Remote coding cancelled",
			"远程编码已取消",
			"遠端編碼已取消",
		)}
	}

	emitRemoteProgress(fmt.Sprintf(codingExecText(
		"① Connecting to remote %s@%s:%d …",
		"① 正在连接远程服务器 %s@%s:%d …",
		"① 正在連線遠端伺服器 %s@%s:%d …",
	), user, host, port))

	sessionID, remoteWorkDir, errMsg := h.ensureCodingWorkflowRemoteSSHSession(userID, state)
	if errMsg != "" {
		emitRemoteProgress(codingExecText("❌ Remote connection failed: ", "❌ 远程连接失败: ", "❌ 遠端連線失敗: ") + errMsg)
		return &IMAgentResponse{Text: errMsg, Error: errMsg}
	}
	if loopCtx.IsCancelled() || runCtx.Err() != nil {
		emitRemoteProgress(codingExecText(
			"⏹ Cancelled after SSH connect — no coding tasks started",
			"⏹ 远程连接后已取消，未开始编码任务",
			"⏹ 遠端連線後已取消，未開始編碼任務",
		))
		return &IMAgentResponse{Text: codingExecText(
			"Remote coding cancelled (SSH connected but no tasks ran)",
			"远程编码已取消（SSH 已连接但未执行任务）",
			"遠端編碼已取消（SSH 已連線但未執行任務）",
		)}
	}
	emitRemoteProgress(fmt.Sprintf(codingExecText(
		"② SSH connected %s@%s:%d, work dir %s, %d coding tasks",
		"② SSH 已连接 %s@%s:%d，工作目录 %s，共 %d 个编码任务",
		"② SSH 已連線 %s@%s:%d，工作目錄 %s，共 %d 個編碼任務",
	), user, host, port, remoteWorkDir, totalTasks))

	// Mark phase executing (locked path)
	if wf := h.getWorkflowV2(); wf != nil {
		if err := wf.machine.MarkPhaseExecuting(userID); err != nil {
			log.Printf("[workflow-v2-remote-exec] MarkPhaseExecuting failed: %v", err)
		}
		if fresh := wf.machine.GetActive(userID); fresh != nil {
			state = fresh
		}
	}
	h.emitWorkflowV2Progress(userID, state)

	reqCtx := truncateRunesV2(getPhaseOutput(state, "requirements"), 500)
	designCtx := truncateRunesV2(getPhaseOutput(state, "design"), 500)
	log.Printf("[workflow-v2-remote-exec] starting: %d tasks session=%s workdir=%s", totalTasks, sessionID, remoteWorkDir)

	reasoningToken := onToken
	if onToken != nil {
		reasoningToken = func(delta string) {
			if strings.HasPrefix(delta, "Browser:") || strings.HasPrefix(delta, "Browser：") {
				delta = strings.TrimPrefix(strings.TrimPrefix(delta, "Browser:"), "Browser：")
				delta = strings.TrimLeft(delta, " ")
			}
			onToken("\x01" + delta)
		}
	}

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client

	// 1-based ordinal for user-facing progress (TaskItem.Index may already be 1-based).
	taskOrdinal := func(task *v2.TaskItem, fallback int) int {
		if task != nil && task.Index > 0 {
			return task.Index
		}
		return fallback
	}

	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, tokenCB func(string), progressCB func(string)) *v2.TaskRunResult {
		if task == nil {
			return &v2.TaskRunResult{Status: v2.TaskFailed, Error: "nil task"}
		}
		ord := taskOrdinal(task, 0)
		title := strings.TrimSpace(task.Title)
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			if progressCB != nil {
				progressCB(codingExecRemoteTaskProgress("cancel", ord, totalTasks, title, ""))
			}
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if progressCB != nil {
			progressCB(codingExecRemoteTaskProgress("start", ord, totalTasks, title, ""))
		}

		desc, taskContext := buildCodingRemoteTaskPrompt(task, config.RequirementsCtx, config.DesignCtx)

		// Nest agent progress under the current task banner.
		nestedProgress := progressCB
		if progressCB != nil {
			nestedProgress = func(msg string) {
				msg = strings.TrimSpace(msg)
				if msg == "" {
					return
				}
				progressCB(fmt.Sprintf("   · T%d %s", ord, msg))
			}
		}

		agent := NewRemoteCodingSubAgent(h, cfg, httpClient, sessionID, remoteWorkDir, remoteWorkDir, loopCtx)
		agent.SetCallbacks(tokenCB, nestedProgress)
		agent.SetSourcePreviewEnabled(true)
		if h.app != nil {
			codingKB := h.app.ensureCodingKnowledgeStore()
			generalKB := getAutoRecallStoreForApp(h.app, false)
			agent.SetKnowledgeStores(codingKB, generalKB)
		}
		result := agent.ExecuteTask(desc, taskContext)
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			if progressCB != nil {
				progressCB(codingExecRemoteTaskProgress("cancel", ord, totalTasks, title, ""))
			}
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if result == nil {
			if progressCB != nil {
				progressCB(codingExecRemoteTaskProgress("fail_nil", ord, totalTasks, title, ""))
			}
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: title, Status: v2.TaskFailed, Error: "RemoteCodingSubAgent returned nil"}
		}
		status := mapRemoteCodingSubAgentStatus(result.Status)
		if progressCB != nil {
			switch status {
			case v2.TaskPassed:
				progressCB(codingExecRemoteTaskProgress("pass", ord, totalTasks, title, ""))
			case v2.TaskSkipped:
				if isCodingExecCancelError(result.Error) || strings.Contains(strings.ToLower(result.Status), "cancel") {
					progressCB(codingExecRemoteTaskProgress("cancel", ord, totalTasks, title, ""))
				} else {
					progressCB(codingExecRemoteTaskProgress("skip", ord, totalTasks, title, ""))
				}
			default:
				errBrief := strings.TrimSpace(result.Error)
				if errBrief == "" {
					errBrief = result.Status
				}
				progressCB(codingExecRemoteTaskProgress("fail", ord, totalTasks, title, truncateRunesV2(errBrief, 80)))
			}
		}
		return &v2.TaskRunResult{
			TaskIndex:     task.Index,
			Title:         title,
			Status:        status,
			Summary:       result.Summary,
			FilesCreated:  result.FilesCreated,
			FilesModified: result.FilesModified,
			Error:         result.Error,
		}
	}

	// ProjectPath for TaskRunner is local workspace for docs; remote agent uses remoteWorkDir.
	config := v2.TaskRunnerConfig{
		ProjectPath:     state.ProjectPath,
		RequirementsCtx: reqCtx,
		DesignCtx:       designCtx,
		MaxRetries:      2,
		TDDMode:         true,
	}
	runner := v2.NewTaskRunner(config, subAgentFn)
	allResults := runner.RunAll(runCtx, tasks, reasoningToken, func(progress string) {
		log.Printf("[workflow-v2-remote-exec] %s", progress)
		emitRemoteProgress(progress)
	})

	allResults = autoRetryFailedCodingTasks(runCtx, loopCtx, config, subAgentFn, tasks, allResults, reasoningToken, emitRemoteProgress, "remote")

	cancelled := runCtx.Err() != nil || loopCtx.IsCancelled()
	report := formatTaskRunResultsReportEx(allResults, cancelled)
	emitRemoteProgress(fmt.Sprintf(codingExecText(
		"④ Remote coding phase finished (%d tasks), summarizing…",
		"④ 远程编码阶段结束（%d 个任务），正在汇总…",
		"④ 遠端編碼階段結束（%d 個任務），正在彙總…",
	), totalTasks))
	if onToken != nil {
		onToken("\n\n" + report)
	}

	cp := codingExecCheckpoint{
		IsRemote:        true,
		Tasks:           tasks,
		Results:         allResults,
		RequirementsCtx: reqCtx,
		DesignCtx:       designCtx,
		ProjectPath:     state.ProjectPath,
		RemoteSessionID: sessionID,
		RemoteWorkDir:   remoteWorkDir,
		RemoteHost:      host,
		RemoteUser:      user,
		RemotePort:      port,
		Cancelled:       cancelled,
	}
	resp := h.finalizeCodingImplementation(userID, state, tasks, allResults, report, cancelled, true, cp, onProgress)
	// On full success, re-arm pure remote coding for continued chat
	// (including cancel-after-last-task races where all results already Passed).
	if resp != nil && allCodingTasksPassed(tasks, allResults) && sessionID != "" {
		remoteCtx := remoteCodingTemplateContext{
			SessionID:  sessionID,
			WorkDir:    remoteWorkDir,
			ProjectDir: remoteWorkDir,
		}
		h.rearmStickyRemoteCodingEnvironment(userID, remoteCtx)
		if taskPath := remoteTaskPathFromWorkflowState(state); taskPath != "" {
			h.bindCodingWorkflowRemoteTaskSticky(userID, taskPath, host, user, remoteWorkDir, port)
			taskOwner := projectSessionOwnerID(taskPath)
			if taskOwner != "" && taskOwner != userID {
				h.rearmStickyRemoteCodingEnvironment(taskOwner, remoteCtx)
				h.bindCodingWorkflowRemoteTaskSticky(taskOwner, taskPath, host, user, remoteWorkDir, port)
			}
		}
	}
	return resp
}

// finalizeCodingImplementation records success (advance) or saves checkpoint for resume.
func (h *IMMessageHandler) finalizeCodingImplementation(
	userID string,
	state *v2.WorkflowState,
	tasks []*v2.TaskItem,
	results []v2.TaskRunResult,
	report string,
	cancelled bool,
	isRemote bool,
	cp codingExecCheckpoint,
	onProgress func(string),
) *IMAgentResponse {
	passed, failed, skipped := countTaskRunStatuses(results)
	// Advance only when every task has a Passed result. A cancel that races
	// after the last task still advances; empty/partial results never do.
	allOK := allCodingTasksPassed(tasks, results)

	if allOK {
		h.clearCodingExecCheckpoint(userID)
		if wf := h.getWorkflowV2(); wf != nil {
			_ = wf.machine.RecordOutput(userID, report)
			if updatedState := wf.machine.GetActive(userID); updatedState != nil {
				if nextPhase := updatedState.ActivePhase(); nextPhase != nil && nextPhase.ExecMode == v2.ExecModeAutoFromPrev {
					if onProgress != nil {
						onProgress(codingExecText(
							"⑤ Auto-advancing to acceptance…",
							"⑤ 自动进入验收阶段…",
							"⑤ 自動進入驗收階段…",
						))
					}
					log.Printf("[workflow-v2-exec] auto-completing phase=%s", nextPhase.ID)
					_ = wf.machine.RecordOutput(userID, report)
				}
			}
			if updatedState := wf.machine.GetActive(userID); updatedState != nil {
				h.emitWorkflowV2Progress(userID, updatedState)
			} else if state != nil {
				emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
					"id":             state.ID,
					"status":         "completed",
					"type":           state.Type,
					"project_path":   workflowEventProjectPath(state),
					"event_scope_id": h.app.getEventScopeID(userID),
				})
			}
		}
		if onProgress != nil {
			if isRemote {
				onProgress(codingExecText(
					"✅ Workflow remote execution completed",
					"✅ 工作流远程执行全部完成",
					"✅ 工作流遠端執行全部完成",
				))
			} else {
				onProgress(codingExecText(
					"✅ Workflow local execution completed",
					"✅ 工作流本机执行全部完成",
					"✅ 工作流本機執行全部完成",
				))
			}
		}
		var b strings.Builder
		if isRemote {
			fmt.Fprintf(&b, codingExecText(
				"Remote execution finished %d coding tasks\n",
				"远程执行完成 %d 个编码任务\n",
				"遠端執行完成 %d 個編碼任務\n",
			), len(tasks))
			if cp.RemoteHost != "" {
				fmt.Fprintf(&b, codingExecText(
					"Remote dir: %s@%s:%d %s\n",
					"远程目录：%s@%s:%d %s\n",
					"遠端目錄：%s@%s:%d %s\n",
				), cp.RemoteUser, cp.RemoteHost, cp.RemotePort, cp.RemoteWorkDir)
			}
		} else {
			fmt.Fprintf(&b, codingExecText(
				"Local execution finished %d coding tasks\n",
				"本机执行完成 %d 个编码任务\n",
				"本機執行完成 %d 個編碼任務\n",
			), len(tasks))
		}
		if state != nil {
			fmt.Fprintf(&b, codingExecText(
				"Local workflow path: %s\n",
				"本机工作流路径：%s\n",
				"本機工作流路徑：%s\n",
			), state.ProjectPath)
		}
		if p := remoteTaskPathFromWorkflowState(state); p != "" {
			fmt.Fprintf(&b, codingExecText(
				"Task record: %s\n",
				"任务管理记录：%s\n",
				"任務管理記錄：%s\n",
			), p)
		}
		b.WriteString(codingExecText(
			"\nWorkflow advanced to the next phase or completed.\n\n",
			"\n工作流已进入下一阶段或已完成。\n\n",
			"\n工作流已進入下一階段或已完成。\n\n",
		))
		b.WriteString(formatTaskListBrief(tasks))
		b.WriteString("\n\n")
		b.WriteString(report)
		return &IMAgentResponse{Text: b.String()}
	}

	// Partial / cancelled: keep phase open and store checkpoint.
	cp.Results = results
	cp.Cancelled = cancelled
	cp.IsRemote = isRemote
	if state != nil {
		cp.WorkflowID = state.ID
		if strings.TrimSpace(cp.ProjectPath) == "" {
			cp.ProjectPath = state.ProjectPath
		}
	}
	h.storeCodingExecCheckpoint(userID, cp)
	if wf := h.getWorkflowV2(); wf != nil {
		if err := wf.machine.SaveExecutionProgress(userID, report); err != nil {
			log.Printf("[workflow-v2-exec] SaveExecutionProgress failed: %v", err)
		}
		if updated := wf.machine.GetActive(userID); updated != nil {
			h.emitWorkflowV2Progress(userID, updated)
		}
	}
	if onProgress != nil {
		if cancelled {
			onProgress(codingExecText(
				"⏹ Coding paused — workflow stays on coding execution (retry failed / continue)",
				"⏹ 编码已暂停，工作流停在「编码执行」— 可 重试失败 / 继续执行",
				"⏹ 編碼已暫停，工作流停在「編碼執行」— 可 重試失敗 / 繼續執行",
			))
		} else if failed > 0 {
			onProgress(fmt.Sprintf(codingExecText(
				"⚠ %d failed tasks remain — workflow stays on coding execution",
				"⚠ 仍有 %d 个失败任务，工作流停在「编码执行」",
				"⚠ 仍有 %d 個失敗任務，工作流停在「編碼執行」",
			), failed))
		} else {
			onProgress(fmt.Sprintf(codingExecText(
				"⚠ Coding incomplete (%d skipped) — workflow stays on coding execution",
				"⚠ 编码未全部完成（跳过 %d），工作流停在「编码执行」",
				"⚠ 編碼未全部完成（跳過 %d），工作流停在「編碼執行」",
			), skipped))
		}
	}
	var b strings.Builder
	b.WriteString(codingExecResumeGuidance(cp, isRemote))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, codingExecText(
		"Stats: passed %d · failed %d · skipped %d\n\n",
		"统计：通过 %d · 失败 %d · 跳过 %d\n\n",
		"統計：通過 %d · 失敗 %d · 跳過 %d\n\n",
	), passed, failed, skipped)
	b.WriteString(formatTaskListBrief(tasks))
	b.WriteString("\n\n")
	b.WriteString(report)
	return &IMAgentResponse{
		Text:    b.String(),
		Actions: codingExecResumeActions(cp),
	}
}

// runCodingExecFromCheckpoint re-runs failed or incomplete tasks from a checkpoint.
func (h *IMMessageHandler) runCodingExecFromCheckpoint(userID, action string, state *v2.WorkflowState, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	cp, ok := h.loadCodingExecCheckpoint(userID)
	if !ok || len(cp.Tasks) == 0 {
		return &IMAgentResponse{Text: codingExecText(
			"No coding checkpoint to retry. Please re-enter the coding execution phase.",
			"没有可重试的编码检查点。请重新进入编码执行阶段。",
			"沒有可重試的編碼檢查點。請重新進入編碼執行階段。",
		)}
	}
	// Prefer live state path/env.
	if state == nil {
		if wf := h.getWorkflowV2(); wf != nil {
			state = wf.machine.GetActive(userID)
		}
	}
	if !codingExecCheckpointMatchesActive(cp, state) {
		h.clearCodingExecCheckpoint(userID)
		return &IMAgentResponse{Text: codingExecText(
			"The coding checkpoint expired or does not match the current workflow and was cleared. Please re-enter coding execution.",
			"编码检查点已过期或与当前工作流不匹配，已清除。请重新进入编码执行阶段。",
			"編碼檢查點已過期或與目前工作流不符，已清除。請重新進入編碼執行階段。",
		)}
	}
	var targets []*v2.TaskItem
	switch action {
	case codingExecRetryActionFailed:
		targets = failedTaskItemsFromResults(cp.Tasks, cp.Results)
	case codingExecRetryActionResume:
		targets = incompleteTaskItemsFromResults(cp.Tasks, cp.Results)
	default:
		return &IMAgentResponse{Text: codingExecText(
			fmt.Sprintf("Unknown retry action. Send “%s” or “%s”.", codingExecCmdRetryFailed(), codingExecCmdResume()),
			fmt.Sprintf("未知重试动作。请发送「%s」或「%s」。", codingExecCmdRetryFailed(), codingExecCmdResume()),
			fmt.Sprintf("未知重試動作。請傳送「%s」或「%s」。", codingExecCmdRetryFailed(), codingExecCmdResume()),
		)}
	}
	if len(targets) == 0 {
		return &IMAgentResponse{Text: codingExecText(
			"No tasks need retry.",
			"没有需要重试的任务。",
			"沒有需要重試的任務。",
		)}
	}
	// Subset re-run must not re-evaluate DependsOn against an incomplete result set.
	targets = tasksForSubsetRerun(targets)
	if onProgress != nil {
		label := codingExecCmdRetryFailed()
		if action == codingExecRetryActionResume {
			label = codingExecCmdResume()
		}
		onProgress(fmt.Sprintf(codingExecText(
			"▶ %s: %d tasks…",
			"▶ %s：%d 个任务…",
			"▶ %s：%d 個任務…",
		), label, len(targets)))
	}
	// Keep phase status consistent with first-run execution.
	if wf := h.getWorkflowV2(); wf != nil {
		if err := wf.machine.MarkPhaseExecuting(userID); err != nil {
			log.Printf("[coding-exec-retry] MarkPhaseExecuting failed: %v", err)
		}
		if fresh := wf.machine.GetActive(userID); fresh != nil {
			state = fresh
		}
	}

	if cp.IsRemote || isCodingWorkflowRemoteExecution(state) {
		return h.runCodingExecTargetsRemote(userID, state, cp, targets, onProgress, onToken, parentLoop)
	}
	return h.runCodingExecTargetsLocal(userID, state, cp, targets, onProgress, onToken, parentLoop)
}

func (h *IMMessageHandler) runCodingExecTargetsLocal(userID string, state *v2.WorkflowState, cp codingExecCheckpoint, targets []*v2.TaskItem, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	projectPath := cp.ProjectPath
	if state != nil && state.ProjectPath != "" {
		projectPath = state.ProjectPath
	}
	loopCtx := parentLoop
	if loopCtx == nil {
		loopCtx = NewLoopContext("workflow-local-coding-retry", h.getMaclawAgentMaxIterations(), h.client)
		defer func() { loopCtx.Cancel(); loopCtx.Done() }()
	}
	ensureLoopCtxUserID(loopCtx, userID)
	runCtx, cancelRun := loopCtx.Context()
	defer cancelRun()

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	total := len(targets)
	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, tokenCB func(string), progressCB func(string)) *v2.TaskRunResult {
		if task == nil {
			return &v2.TaskRunResult{Status: v2.TaskFailed, Error: "nil task"}
		}
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if progressCB != nil {
			progressCB(fmt.Sprintf(codingExecText("Local retry %d/%d: %s", "本机重试 %d/%d：%s", "本機重試 %d/%d：%s"), task.Index, total, task.Title))
		}
		v1Task := &TaskItem{Index: task.Index, Title: task.Title, Description: task.Description, Files: task.Files, DependsOn: task.DependsOn}
		fn := RunTaskWithSubAgent
		if runTaskWithSubAgent != nil {
			fn = runTaskWithSubAgent
		}
		v1Result := fn(h, cfg, httpClient, v1Task, config.ProjectPath, config.RequirementsCtx, config.DesignCtx, nil, loopCtx, tokenCB, progressCB)
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if v1Result == nil {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskFailed, Error: "SubAgent returned nil"}
		}
		status := v2.TaskFailed
		switch v1Result.Status {
		case TaskExecPassed:
			status = v2.TaskPassed
		case TaskExecSkipped:
			status = v2.TaskSkipped
		}
		return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: status, Summary: v1Result.Summary, FilesCreated: v1Result.FilesCreated, FilesModified: v1Result.FilesModified, Error: v1Result.Error}
	}
	config := v2.TaskRunnerConfig{ProjectPath: projectPath, RequirementsCtx: cp.RequirementsCtx, DesignCtx: cp.DesignCtx, MaxRetries: 1, TDDMode: true}
	runner := v2.NewTaskRunner(config, subAgentFn)
	retryResults := runner.RunAll(runCtx, targets, onToken, onProgress)
	merged := mergeTaskRunResultsByIndex(cp.Results, retryResults)
	cancelled := runCtx.Err() != nil || loopCtx.IsCancelled()
	report := formatTaskRunResultsReportEx(merged, cancelled)
	if onToken != nil {
		onToken("\n\n" + report)
	}
	cp.Results = merged
	cp.Cancelled = cancelled
	return h.finalizeCodingImplementation(userID, state, cp.Tasks, merged, report, cancelled, false, cp, onProgress)
}

func (h *IMMessageHandler) runCodingExecTargetsRemote(userID string, state *v2.WorkflowState, cp codingExecCheckpoint, targets []*v2.TaskItem, onProgress func(string), onToken func(string), parentLoop *LoopContext) *IMAgentResponse {
	// Refresh SSH session if needed.
	sessionID := cp.RemoteSessionID
	workDir := cp.RemoteWorkDir
	host, user, port := cp.RemoteHost, cp.RemoteUser, cp.RemotePort
	if state != nil {
		if h2, u2, w2, p2, ok := codingWorkflowRemoteEnvFromState(state); ok {
			host, user, workDir, port = h2, u2, w2, p2
		}
	}
	if port <= 0 {
		port = 22
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf(codingExecText(
			"① Checking remote connection %s@%s:%d before retry…",
			"① 重试前检查远程连接 %s@%s:%d …",
			"① 重試前檢查遠端連線 %s@%s:%d …",
		), user, host, port))
	}
	if sid, wd, errMsg := h.ensureCodingWorkflowRemoteSSHSession(userID, state); errMsg == "" {
		sessionID, workDir = sid, wd
	} else if sessionID == "" {
		return &IMAgentResponse{Text: codingExecText(
			"Remote retry failed: cannot connect SSH — ",
			"远程重试失败：无法连接 SSH — ",
			"遠端重試失敗：無法連線 SSH — ",
		) + errMsg, Error: errMsg}
	}

	loopCtx := parentLoop
	if loopCtx == nil {
		loopCtx = NewLoopContext("workflow-remote-coding-retry", h.getMaclawAgentMaxIterations(), h.client)
		defer func() { loopCtx.Cancel(); loopCtx.Done() }()
	}
	ensureLoopCtxUserID(loopCtx, userID)
	runCtx, cancelRun := loopCtx.Context()
	defer cancelRun()

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	total := len(targets)
	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, tokenCB func(string), progressCB func(string)) *v2.TaskRunResult {
		if task == nil {
			return &v2.TaskRunResult{Status: v2.TaskFailed, Error: "nil task"}
		}
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if progressCB != nil {
			progressCB(fmt.Sprintf(codingExecText("Remote retry %d/%d: %s", "远程重试 %d/%d：%s", "遠端重試 %d/%d：%s"), task.Index, total, task.Title))
		}
		desc, taskContext := buildCodingRemoteTaskPrompt(task, config.RequirementsCtx, config.DesignCtx)
		agent := NewRemoteCodingSubAgent(h, cfg, httpClient, sessionID, workDir, workDir, loopCtx)
		agent.SetCallbacks(tokenCB, progressCB)
		agent.SetSourcePreviewEnabled(true)
		if h.app != nil {
			codingKB := h.app.ensureCodingKnowledgeStore()
			generalKB := getAutoRecallStoreForApp(h.app, false)
			agent.SetKnowledgeStores(codingKB, generalKB)
		}
		result := agent.ExecuteTask(desc, taskContext)
		if ctx.Err() != nil || loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if result == nil {
			return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskFailed, Error: "RemoteCodingSubAgent returned nil"}
		}
		status := mapRemoteCodingSubAgentStatus(result.Status)
		return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: status, Summary: result.Summary, FilesCreated: result.FilesCreated, FilesModified: result.FilesModified, Error: result.Error}
	}
	config := v2.TaskRunnerConfig{ProjectPath: cp.ProjectPath, RequirementsCtx: cp.RequirementsCtx, DesignCtx: cp.DesignCtx, MaxRetries: 1, TDDMode: true}
	runner := v2.NewTaskRunner(config, subAgentFn)
	retryResults := runner.RunAll(runCtx, targets, onToken, onProgress)
	merged := mergeTaskRunResultsByIndex(cp.Results, retryResults)
	cancelled := runCtx.Err() != nil || loopCtx.IsCancelled()
	report := formatTaskRunResultsReportEx(merged, cancelled)
	if onToken != nil {
		onToken("\n\n" + report)
	}
	cp.Results = merged
	cp.RemoteSessionID = sessionID
	cp.RemoteWorkDir = workDir
	cp.RemoteHost = host
	cp.RemoteUser = user
	cp.RemotePort = port
	cp.Cancelled = cancelled
	cp.IsRemote = true
	return h.finalizeCodingImplementation(userID, state, cp.Tasks, merged, report, cancelled, true, cp, onProgress)
}


// buildCodingRemoteTaskPrompt builds the remote SubAgent task description and
// requirements/design context (shared by first-run and checkpoint retry paths).
func buildCodingRemoteTaskPrompt(task *v2.TaskItem, reqCtx, designCtx string) (desc, taskContext string) {
	if task != nil {
		desc = strings.TrimSpace(task.Title)
		if d := strings.TrimSpace(task.Description); d != "" {
			if desc != "" {
				desc = desc + "\n\n" + d
			} else {
				desc = d
			}
		}
		if len(task.Files) > 0 {
			desc += "\n\n相关文件: " + strings.Join(task.Files, ", ")
		}
	}
	var parts []string
	if strings.TrimSpace(reqCtx) != "" {
		parts = append(parts, "需求摘要:\n"+reqCtx)
	}
	if strings.TrimSpace(designCtx) != "" {
		parts = append(parts, "设计摘要:\n"+designCtx)
	}
	return desc, strings.Join(parts, "\n\n")
}


// autoRetryFailedCodingTasks runs one automatic failed-only subset pass shared by
// local and remote first-run coding execution. DependsOn is stripped so TaskRunner
// does not false-skip subset tasks.
func autoRetryFailedCodingTasks(
	runCtx context.Context,
	loopCtx *LoopContext,
	config v2.TaskRunnerConfig,
	subAgentFn v2.SubAgentFunc,
	tasks []*v2.TaskItem,
	allResults []v2.TaskRunResult,
	onToken func(string),
	onProgress func(string),
	envLabel string,
) []v2.TaskRunResult {
	cancelled := runCtx != nil && runCtx.Err() != nil
	if !cancelled && loopCtx != nil && loopCtx.IsCancelled() {
		cancelled = true
	}
	if cancelled {
		if onProgress != nil {
			// envLabel is "local" or "remote" (stable key).
			switch envLabel {
			case "remote":
				onProgress(codingExecText(
					"⏹ Remote coding cancelled — skip automatic failed retry",
					"⏹ 远程编码已取消，跳过自动失败重试",
					"⏹ 遠端編碼已取消，跳過自動失敗重試",
				))
			default:
				onProgress(codingExecText(
					"⏹ Local coding cancelled — skip automatic failed retry",
					"⏹ 本机编码已取消，跳过自动失败重试",
					"⏹ 本機編碼已取消，跳過自動失敗重試",
				))
			}
		}
		return allResults
	}
	failedTasks := tasksForSubsetRerun(failedTaskItemsFromResults(tasks, allResults))
	if len(failedTasks) == 0 {
		return allResults
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf(codingExecText(
			"↻ Found %d failed tasks — retrying failures only…",
			"↻ 发现 %d 个失败任务，正在仅重试失败项…",
			"↻ 發現 %d 個失敗任務，正在僅重試失敗項…",
		), len(failedTasks)))
	}
	retryConfig := config
	retryConfig.MaxRetries = 1
	retryRunner := v2.NewTaskRunner(retryConfig, subAgentFn)
	retryResults := retryRunner.RunAll(runCtx, failedTasks, onToken, func(progress string) {
		if onProgress != nil {
			onProgress("↻ " + progress)
		}
	})
	merged := mergeTaskRunResultsByIndex(allResults, retryResults)
	if onProgress != nil {
		onProgress(fmt.Sprintf(codingExecText(
			"↻ Failed-task retry finished (%d items)",
			"↻ 失败项重试结束（%d 项）",
			"↻ 失敗項重試結束（%d 項）",
		), len(failedTasks)))
	}
	return merged
}

// mapRemoteCodingSubAgentStatus normalizes RemoteCodingSubAgent status strings.
func mapRemoteCodingSubAgentStatus(status string) v2.TaskRunStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed", "success", "ok", "completed", "done":
		return v2.TaskPassed
	case "skipped", "cancelled", "canceled":
		return v2.TaskSkipped
	default:
		return v2.TaskFailed
	}
}

// failedTaskItemsFromResults returns original task items whose matching result failed.
func failedTaskItemsFromResults(tasks []*v2.TaskItem, results []v2.TaskRunResult) []*v2.TaskItem {
	if len(tasks) == 0 || len(results) == 0 {
		return nil
	}
	byIndex := make(map[int]v2.TaskRunStatus, len(results))
	for _, r := range results {
		byIndex[r.TaskIndex] = r.Status
	}
	var failed []*v2.TaskItem
	for _, t := range tasks {
		if t == nil {
			continue
		}
		if byIndex[t.Index] == v2.TaskFailed {
			failed = append(failed, t)
		}
	}
	return failed
}

// mergeTaskRunResultsByIndex replaces results with the same TaskIndex from updates.
func mergeTaskRunResultsByIndex(base, updates []v2.TaskRunResult) []v2.TaskRunResult {
	if len(updates) == 0 {
		return base
	}
	out := append([]v2.TaskRunResult(nil), base...)
	for _, u := range updates {
		replaced := false
		for i := range out {
			if out[i].TaskIndex == u.TaskIndex {
				out[i] = u
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, u)
		}
	}
	return out
}

// formatTaskRunResultsReport builds a summary report from task run results.
func formatTaskRunResultsReport(results []v2.TaskRunResult) string {
	return formatTaskRunResultsReportEx(results, false)
}

// formatTaskRunResultsReportEx builds a summary report; cancelled uses a single title
// (avoids prepending a second "## …" header).
func formatTaskRunResultsReportEx(results []v2.TaskRunResult, cancelled bool) string {
	var sb strings.Builder
	if cancelled {
		sb.WriteString(codingExecText("## Execution report (cancelled)\n\n", "## 执行报告（已取消）\n\n", "## 執行報告（已取消）\n\n"))
	} else {
		sb.WriteString(codingExecText("## Execution report\n\n", "## 执行报告\n\n", "## 執行報告\n\n"))
	}
	passed, failed, skipped := 0, 0, 0
	for _, result := range results {
		switch result.Status {
		case v2.TaskPassed:
			passed++
		case v2.TaskFailed:
			failed++
		case v2.TaskSkipped:
			skipped++
		}
	}
	sb.WriteString(fmt.Sprintf(codingExecText(
		"- Passed: %d\n- Failed: %d\n- Skipped: %d\n\n",
		"- 通过: %d\n- 失败: %d\n- 跳过: %d\n\n",
		"- 通過: %d\n- 失敗: %d\n- 跳過: %d\n\n",
	), passed, failed, skipped))
	for _, result := range results {
		mark := "·"
		switch result.Status {
		case v2.TaskPassed:
			mark = "✓"
		case v2.TaskFailed:
			mark = "✗"
		case v2.TaskSkipped:
			mark = "⊘"
		}
		line := fmt.Sprintf("%s T%d %s — %s", mark, result.TaskIndex, result.Title, codingExecStatusLabel(result.Status))
		if result.Error != "" {
			line += " (" + result.Error + ")"
		}
		sb.WriteString(line + "\n")
		if s := strings.TrimSpace(result.Summary); s != "" {
			sb.WriteString("  " + truncateRunesV2(s, 200) + "\n")
		}
	}
	return sb.String()
}

// codingExecRemoteTaskProgress localizes remote first-run task progress banners.
func codingExecRemoteTaskProgress(kind string, ord, total int, title, detail string) string {
	switch kind {
	case "start":
		if strings.TrimSpace(title) != "" {
			return fmt.Sprintf(codingExecText(
				"③ Remote task %d/%d: %s",
				"③ 远程任务 %d/%d：%s",
				"③ 遠端任務 %d/%d：%s",
			), ord, total, title)
		}
		return fmt.Sprintf(codingExecText(
			"③ Remote task %d/%d",
			"③ 远程任务 %d/%d",
			"③ 遠端任務 %d/%d",
		), ord, total)
	case "cancel":
		return fmt.Sprintf(codingExecText(
			"⏹ Remote task %d/%d cancelled: %s",
			"⏹ 远程任务 %d/%d 已取消：%s",
			"⏹ 遠端任務 %d/%d 已取消：%s",
		), ord, total, title)
	case "pass":
		return fmt.Sprintf(codingExecText(
			"✓ Remote task %d/%d done: %s",
			"✓ 远程任务 %d/%d 完成：%s",
			"✓ 遠端任務 %d/%d 完成：%s",
		), ord, total, title)
	case "skip":
		return fmt.Sprintf(codingExecText(
			"⊘ Remote task %d/%d skipped: %s",
			"⊘ 远程任务 %d/%d 已跳过：%s",
			"⊘ 遠端任務 %d/%d 已跳過：%s",
		), ord, total, title)
	case "fail_nil":
		return fmt.Sprintf(codingExecText(
			"❌ Remote task %d/%d failed: SubAgent returned nothing",
			"❌ 远程任务 %d/%d 失败：SubAgent 无返回",
			"❌ 遠端任務 %d/%d 失敗：SubAgent 無返回",
		), ord, total)
	case "fail":
		return fmt.Sprintf(codingExecText(
			"❌ Remote task %d/%d failed: %s — %s",
			"❌ 远程任务 %d/%d 失败：%s — %s",
			"❌ 遠端任務 %d/%d 失敗：%s — %s",
		), ord, total, title, detail)
	default:
		return title
	}
}

func getPhaseOutput(state *v2.WorkflowState, phaseID string) string {
	if state == nil {
		return ""
	}
	for _, p := range state.Phases {
		if p.ID == phaseID {
			return p.Output
		}
	}
	return ""
}

func listPhaseIDs(state *v2.WorkflowState) []string {
	if state == nil {
		return nil
	}
	ids := make([]string, len(state.Phases))
	for i, p := range state.Phases {
		ids[i] = fmt.Sprintf("%s(status=%s,output_len=%d)", p.ID, p.Status, len(p.Output))
	}
	return ids
}

func truncateRunesV2(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func formatTaskListBrief(tasks []*v2.TaskItem) string {
	return formatTaskListBriefWithCurrent(tasks, 0)
}

// formatTaskListBriefWithCurrent renders a titles-only plan outline.
// When currentIdx > 0, that step is marked as the active plan step.
// Tasks with Index<=0 are renumbered by stable 1-based position among non-nil tasks.
func formatTaskListBriefWithCurrent(tasks []*v2.TaskItem, currentIdx int) string {
	var sb strings.Builder
	pos := 0
	for _, t := range tasks {
		if t == nil {
			continue
		}
		pos++
		idx := t.Index
		if idx <= 0 {
			idx = pos
		}
		title := strings.TrimSpace(t.Title)
		if title == "" {
			title = "(untitled)"
		}
		if currentIdx > 0 && idx == currentIdx {
			sb.WriteString(fmt.Sprintf("- T%d: %s  ← current (do only this)\n", idx, title))
		} else {
			sb.WriteString(fmt.Sprintf("- T%d: %s\n", idx, title))
		}
	}
	return sb.String()
}

// formatRemotePlanStepCarrySummary builds a short prior-step note for the next
// remote plan step. Full agent summaries often claim "全部完成"; that must not
// license later steps to no-op.
func formatRemotePlanStepCarrySummary(stepIndex int, title, status, sum string) string {
	title = strings.TrimSpace(title)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "unknown"
	}
	body := strings.TrimSpace(sum)
	if body != "" {
		body = truncateRunesV2(body, 400)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"Prior plan step T%d (%s) status=%s. Context only — still execute the CURRENT step fully; ignore any claim that the whole project is already done.",
		stepIndex, title, status,
	))
	if body != "" {
		b.WriteString("\n")
		b.WriteString(body)
	}
	return b.String()
}

// setRemotePlanLastSummary records prior-step context for the next remote step.
// When planned, uses a sanitized carry note so "全部完成" claims do not leak.
func setRemotePlanLastSummary(sessionMem *stickyCodingWorkbenchMemory, planned bool, stepIndex int, title, status, sum string) {
	if sessionMem == nil {
		return
	}
	sum = strings.TrimSpace(sum)
	if sum == "" {
		return
	}
	if planned {
		sessionMem.LastSummary = formatRemotePlanStepCarrySummary(stepIndex, title, status, sum)
		return
	}
	sessionMem.LastSummary = truncateRunesV2(sum, 800)
}

// --- Helper: emit frontend event ---

func emitWorkflowV2Event(a *App, eventName string, data interface{}) {
	if a == nil || a.ctx == nil {
		log.Printf("[workflow-v2-event] %s (no ctx): %v", eventName, data)
		return
	}
	a.emitEvent(eventName, data)
}

// workflowEventProjectPath resolves the project_path for frontend event routing.
// state.ProjectPath may have been truncated by TruncateToValidPathChars (strips
// non-ASCII for SubAgent), making it differ from the frontend tab's projectPath.
// This recovers the original tab path from state.UserID ("desktop-user:<path>").
func workflowEventProjectPath(state *v2.WorkflowState) string {
	if state == nil {
		return ""
	}
	if tabPath := projectPathFromSessionOwnerID(state.UserID); tabPath != "" {
		return tabPath
	}
	return state.ProjectPath
}

// runCodingTemplateSubAgent executes the pure-coding workbench turn.
// Complex requests may be auto-planned into multiple ordered TaskItems and
// executed sequentially via TaskRunner; simple requests stay single-task.
// Plan mode "approve" pauses after planning until /plan approve.
func (h *IMMessageHandler) runCodingTemplateSubAgent(userID, userText, projectPath string, loopCtx *LoopContext, onProgress func(string), onToken func(string)) *IMAgentResponse {
	ensureLoopCtxUserID(loopCtx, userID)
	// Register after cancel check, before planning, so guide-launch during plan
	// LLM uses pendingInjection (not the 30s pre-loop bag). Plan-approve early
	// return still cleans up via defer; clearNonGuidePendingInjection keeps guides.
	cleanupPureCodingRuntime := func() {}
	defer func() { cleanupPureCodingRuntime() }()
	if err := v2.EnsureProjectDir(projectPath); err != nil {
		log.Printf("[workflow-v2] pure coding: failed to ensure project dir %s: %v", projectPath, err)
	}

	// Load AGENTS.md / CLAUDE.md into sticky for this turn.
	h.ensureStickyProjectInstructions(userID, projectPath)

	// Multi-turn session memory: carry prior summaries/files into this turn.
	sessionMem := h.getStickyCodingWorkbenchMemory(userID)
	prevOutputs := sessionMem.prevOutputs()
	reqCtx := ""
	if sessionMem.TurnCount > 0 {
		reqCtx = fmt.Sprintf("Continue full coding workbench session (turn %d). Prefer incremental changes on prior work.", sessionMem.TurnCount+1)
	}

	if loopCtx != nil && loopCtx.IsCancelled() {
		return &IMAgentResponse{Text: "编码任务已取消", TraceEventCount: 0}
	}
	cleanupPureCodingRuntime = h.beginPureCodingRuntime(loopCtx, userID, userText)

	// One-shot approved plan from /plan approve.
	var tasks []*v2.TaskItem
	var planMarkdown string
	var planned bool
	recordUserText := userText
	if approved, ok := h.takeStickyApprovedCodingPlan(userID); ok {
		tasks = approved.Tasks
		planMarkdown = approved.Markdown
		planned = len(tasks) >= codingWorkbenchPlanMinTasks
		if s := strings.TrimSpace(approved.UserText); s != "" {
			recordUserText = s
		}
		// Strip approve marker from display text.
		userText = recordUserText
		if onProgress != nil {
			onProgress(fmt.Sprintf("执行已批准计划（%d 步）…", len(tasks)))
		}
		if onToken != nil && planMarkdown != "" {
			onToken("\n\n## 按已批准计划执行\n\n" + planMarkdown + "\n\n---\n\n")
		}
		h.persistCodingWorkbenchPlans(userID, planMarkdown, "")
		h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	} else {
		// Strip accidental approve marker if present without sticky plan.
		if strings.HasPrefix(strings.TrimSpace(userText), codingPlanApproveExecuteMarker) {
			userText = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userText), codingPlanApproveExecuteMarker))
			recordUserText = userText
		}
		hooks := loadCodingWorkbenchHooks(projectPath)
		if prePlan := runCodingWorkbenchHookPhase(projectPath, hooks, "pre_plan"); prePlan.Report != "" {
			log.Printf("[coding-hooks] pre_plan: %s", truncateRunesV2(prePlan.Report, 200))
		}
		// Note: pre_plan fail_on_error does not abort planning (user still needs a plan);
		// step-level fail_on_error is enforced on pre_step / pre_verify.
		tasks, planMarkdown, planned = h.resolveCodingWorkbenchTasks(userID, userText, projectPath, sessionMem, onProgress, onToken)
		// Approve mode: resolve already stored pending; return without executing.
		if planned && normalizeCodingPlanMode(sessionMem.PlanMode) == codingPlanModeApprove {
			if _, hasPending := h.loadStickyPendingCodingPlan(userID); hasPending {
				text := formatPendingPlanApprovalText(planMarkdown, len(tasks))
				if onToken != nil {
					onToken("\n\n" + text)
				}
				return &IMAgentResponse{
					Text:    text,
					Actions: codingPlanApproveActions(),
				}
			}
		}
	}
	if planned && planMarkdown != "" {
		// Refresh prevOutputs after plan was persisted so steps see the plan.
		sessionMem = h.getStickyCodingWorkbenchMemory(userID)
		prevOutputs = sessionMem.prevOutputs()
		reqCtx = strings.TrimSpace(reqCtx + "\n\nA multi-step execution plan was generated for this request. Complete the CURRENT step fully, then stop; later steps run as separate tasks.")
	}
	if len(tasks) == 0 {
		tasks = []*v2.TaskItem{{Index: 1, Title: truncateRunesV2(userText, 80), Description: userText}}
		planned = false
	}

	log.Printf("[workflow-v2] pure coding: user=%s project=%s task=%q sticky_turn=%d prev_outputs=%d planned=%v steps=%d",
		userID, projectPath, truncateRunesV2(userText, 80), sessionMem.TurnCount, len(prevOutputs), planned, len(tasks))

	if onProgress != nil {
		if planned {
			onProgress(fmt.Sprintf("全功能编程工作台：按计划执行 %d 步", len(tasks)))
		} else if sessionMem.TurnCount > 0 {
			onProgress(fmt.Sprintf("全功能编程工作台：继续第 %d 轮执行", sessionMem.TurnCount+1))
		} else {
			onProgress("全功能编程工作台：开始执行")
		}
	}

	hooks := loadCodingWorkbenchHooks(projectPath)
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	worktreeMode := h.getStickyCodingWorktreeMode(userID)
	// One preview session for the whole pure-coding turn so multi-step writes
	// accumulate in the right-hand source panel.
	// Frontend auto_open session_start keeps existing tabs (does not wipe the map).
	// Preview content is filled by: arm restore (sticky), mid-turn write_file events,
	// and end-of-turn emitCodingWorkbenchSourcePreview — no turn-start full re-read
	// (avoids double disk I/O and forceOpen race with session_start ordering).
	codeSessionID := newCodingSubAgentCodeSessionID("coding-workbench", userID)
	if loopCtx != nil {
		loopCtx.codeSessionID = codeSessionID
	}
	// Route with tab project path (managed task dir), not exec/working_dir.
	// Frontend shouldAcceptCodeEventForProject filters on activeTab.projectPath.
	previewRoutePath := codePreviewRouteProjectPath(userID, projectPath)
	emitCodingSubAgentCodeSessionStart(h.app, codeSessionID, previewRoutePath)
	defer emitCodingSubAgentCodeSessionEnd(h.app, codeSessionID, previewRoutePath)
	// Multi-step plans already encode explore→implement→verify; TDD doubling is too heavy.
	// Parallel waves: explore-only steps with softened deps can fan out (MaxParallel=3).
	// Write steps may isolate into git worktrees when mode is auto/always.
	maxParallel := 1
	if planned && len(tasks) >= 2 {
		maxParallel = 3
	}
	var lastCodingResult *CodingSubAgentResult
	totalToolCalls, totalIters := 0, 0
	totalInTok, totalOutTok := 0, 0
	totalCost := 0.0
	var usageMu sync.Mutex
	var mergeMu sync.Mutex // serialize worktree cherry-picks into main tree
	var attachOnce sync.Once
	var mergedModified, mergedCreated []string
	subAgentFn := func(ctx context.Context, t *v2.TaskItem, config v2.TaskRunnerConfig, onTk func(string), onPr func(string)) *v2.TaskRunResult {
		if t == nil {
			return &v2.TaskRunResult{Status: v2.TaskFailed, Error: "nil task"}
		}
		if loopCtx != nil && loopCtx.IsCancelled() {
			return &v2.TaskRunResult{TaskIndex: t.Index, Title: t.Title, Status: v2.TaskSkipped, Error: "cancelled"}
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				return &v2.TaskRunResult{TaskIndex: t.Index, Title: t.Title, Status: v2.TaskSkipped, Error: "cancelled"}
			default:
			}
		}
		h.updateStickyCodingStepStatus(userID, t.Index, codingStepRunning, "")
		if preStepRes := runCodingWorkbenchHookPhase(projectPath, hooks, "pre_step"); preStepRes.Report != "" || preStepRes.Failed {
			if preStepRes.Report != "" {
				log.Printf("[coding-hooks] pre_step T%d: %s", t.Index, truncateRunesV2(preStepRes.Report, 160))
			}
			if codingHookShouldAbort(hooks, preStepRes) {
				sum := "pre_step hook failed (fail_on_error)\n" + preStepRes.Report
				h.updateStickyCodingStepStatus(userID, t.Index, codingStepFailed, sum)
				return &v2.TaskRunResult{
					TaskIndex: t.Index,
					Title:     t.Title,
					Status:    v2.TaskFailed,
					Summary:   sum,
					Error:     "pre_step hook failed",
				}
			}
		}
		stepReqCtx := reqCtx
		if planned {
			stepReqCtx = strings.TrimSpace(reqCtx + fmt.Sprintf(
				"\n\nYou are executing plan step T%d/%d: %s\nFocus on this step only; do not skip ahead.",
				t.Index, len(tasks), strings.TrimSpace(t.Title),
			))
		}

		// Optional git worktree isolation for write-capable steps.
		stepProject := config.ProjectPath
		if stepProject == "" {
			stepProject = projectPath
		}
		var wt *codingWorkbenchWorktree
		waveSize := config.WaveSize
		useWT := shouldUseCodingWorktree(worktreeMode, planned, t.Title, t.Description, maxParallel, waveSize, t.DependsOn)
		if useWT {
			created, wtErr := createCodingWorkbenchWorktree(projectPath, t.Index, t.Title)
			if wtErr != nil {
				log.Printf("[coding-worktree] create failed T%d: %v — falling back to main tree", t.Index, wtErr)
				if onPr != nil {
					onPr(fmt.Sprintf("T%d worktree 创建失败，回退主目录: %v", t.Index, wtErr))
				}
			} else if created != nil {
				wt = created
				stepProject = wt.ProjectPath
				h.rememberCodingWorktree(userID, wt, "created")
				if onPr != nil {
					onPr(fmt.Sprintf("T%d 在 git worktree 中执行: %s", t.Index, wt.Branch))
				}
				stepReqCtx = strings.TrimSpace(stepReqCtx +
					"\n\nYou are in an isolated git worktree. Edit only under this project path. Changes will be cherry-picked back to the main tree after the step.")
			}
		}

		v1Task := &TaskItem{
			Index:       t.Index,
			Title:       t.Title,
			Description: t.Description,
			Files:       t.Files,
			DependsOn:   t.DependsOn,
		}
		var fn subAgentTaskFunc
		if runTaskWithSubAgent != nil {
			fn = runTaskWithSubAgent
		} else {
			fn = RunTaskWithSubAgent
		}
		// Snapshot prevOutputs under lock so parallel waves don't race the slice.
		usageMu.Lock()
		stepPrev := append([]string(nil), prevOutputs...)
		usageMu.Unlock()
		v1Result := fn(h, cfg, httpClient, v1Task, stepProject, stepReqCtx, "", stepPrev, loopCtx, onTk, onPr)

		// Merge worktree back (or cleanup on failure).
		if wt != nil {
			if v1Result != nil && v1Result.Status == TaskExecPassed {
				mergeMu.Lock()
				mergedOK, mergeSum, mergeErr := wt.mergeBack(projectPath)
				mergeMu.Unlock()
				if mergeErr != nil {
					log.Printf("[coding-worktree] merge failed T%d: %v", t.Index, mergeErr)
					h.rememberCodingWorktree(userID, wt, "merge-failed")
					h.recordLocalWorktreeConflict(userID, wt, projectPath, mergeErr.Error())
					// Keep worktree for inspection + /worktree adopt.
					wt.cleanup(true)
					hint := mergeErr.Error() + "\n\n可用 `/worktree conflicts` 查看，`/worktree adopt <id>` 强制合并。"
					if v1Result.Summary != "" {
						v1Result.Summary = v1Result.Summary + "\n\nworktree merge failed: " + hint
					} else {
						v1Result.Summary = "worktree merge failed: " + hint
					}
					v1Result.Status = TaskExecFailed
					v1Result.Error = mergeErr.Error()
				} else {
					if mergeSum != "" {
						h.rememberCodingWorktree(userID, wt, mergeSum)
						if onPr != nil {
							onPr(fmt.Sprintf("T%d %s", t.Index, mergeSum))
						}
						if mergedOK {
							if v1Result.Summary != "" {
								v1Result.Summary = v1Result.Summary + "\n\n" + mergeSum
							} else {
								v1Result.Summary = mergeSum
							}
						}
					}
					// Remap file paths from worktree → main before sticky merge.
					v1Result.FilesModified = remapWorktreePaths(v1Result.FilesModified, wt.ProjectPath, projectPath)
					v1Result.FilesCreated = remapWorktreePaths(v1Result.FilesCreated, wt.ProjectPath, projectPath)
					wt.cleanup(false)
				}
			} else {
				// Failed/skipped step: drop worktree branch.
				wt.cleanup(false)
			}
		}

		if v1Result != nil {
			// Push step file changes into the right-hand preview immediately
			// (after worktree path remap so events target the main project).
			emitCodingSubAgentCodeFileEvents(h.app, codeSessionID, projectPath, v1Result.FilesModified, v1Result.FilesCreated, previewRoutePath)
			usageMu.Lock()
			lastCodingResult = v1Result
			totalToolCalls += v1Result.ToolCalls
			totalIters += v1Result.Iterations
			totalInTok += v1Result.InputTokens
			totalOutTok += v1Result.OutputTokens
			totalCost += v1Result.EstCostRMB
			if v1Result.RouteModel != "" || v1Result.RouteSource != "" {
				h.recordStickyCodingRoute(userID, v1Result.RouteModel, v1Result.RouteSource, v1Result.RouteTask, v1Result.RouteReason)
			}
			mergedModified = append(mergedModified, v1Result.FilesModified...)
			mergedCreated = append(mergedCreated, v1Result.FilesCreated...)
			// Later steps see earlier step summaries in continuity context.
			if sum := strings.TrimSpace(v1Result.Summary); sum != "" {
				prevOutputs = append(prevOutputs, fmt.Sprintf("Completed T%d (%s):\n%s", t.Index, t.Title, truncateRunesV2(sum, 600)))
				if len(prevOutputs) > stickyCodingMemoryPrevOutputsMax {
					prevOutputs = prevOutputs[len(prevOutputs)-stickyCodingMemoryPrevOutputsMax:]
				}
			}
			usageMu.Unlock()
			// Attachments only needed on the first step of a multi-step plan.
			attachOnce.Do(func() {
				if loopCtx != nil {
					loopCtx.CodingAttachments = nil
				}
			})
		}
		if v1Result == nil {
			h.updateStickyCodingStepStatus(userID, t.Index, codingStepFailed, "SubAgent returned nil")
			return &v2.TaskRunResult{TaskIndex: t.Index, Title: t.Title, Status: v2.TaskFailed, Error: "SubAgent returned nil"}
		}
		status := v2.TaskFailed
		stepStatus := codingStepFailed
		switch v1Result.Status {
		case TaskExecPassed:
			status = v2.TaskPassed
			stepStatus = codingStepPassed
		case TaskExecSkipped:
			status = v2.TaskSkipped
			stepStatus = codingStepSkipped
		}
		summary := v1Result.Summary
		// Step verification gate after successful implement/verify-style steps.
		// Always verify on the main project path (post-merge).
		if status == v2.TaskPassed && planned && stepNeedsVerifyGate(t.Title, t.Description, t.Index, len(tasks)) {
			preV := runCodingWorkbenchHookPhase(projectPath, hooks, "pre_verify")
			if preV.Report != "" {
				log.Printf("[coding-hooks] pre_verify T%d: %s", t.Index, truncateRunesV2(preV.Report, 160))
			}
			if codingHookShouldAbort(hooks, preV) {
				status = v2.TaskFailed
				stepStatus = codingStepVerifyFail
				gateSum := "pre_verify hook failed (fail_on_error)\n" + preV.Report
				if summary != "" {
					summary = summary + "\n\n" + gateSum
				} else {
					summary = gateSum
				}
				h.updateStickyCodingStepStatus(userID, t.Index, stepStatus, summary)
				_ = runCodingWorkbenchHookPhase(projectPath, hooks, "post_step")
				return &v2.TaskRunResult{
					TaskIndex:     t.Index,
					Title:         t.Title,
					Status:        status,
					Summary:       summary,
					FilesCreated:  v1Result.FilesCreated,
					FilesModified: v1Result.FilesModified,
					Error:         "pre_verify hook failed",
				}
			}
			ok, vcmd, vout, skipped := runCodingWorkbenchStepVerify(ctx, projectPath)
			gateSum := codingWorkbenchStepGateSummary(ok, vcmd, vout, skipped)
			if !skipped {
				if onProgress != nil {
					if ok {
						onProgress(fmt.Sprintf("T%d 步级验证通过: %s", t.Index, vcmd))
					} else {
						onProgress(fmt.Sprintf("T%d 步级验证失败: %s", t.Index, vcmd))
					}
				}
				if onTk != nil {
					onTk("\n\n" + gateSum + "\n")
				}
				postV := runCodingWorkbenchHookPhase(projectPath, hooks, "post_verify")
				if postV.Report != "" {
					log.Printf("[coding-hooks] post_verify T%d: %s", t.Index, truncateRunesV2(postV.Report, 160))
				}
				if !ok {
					status = v2.TaskFailed
					stepStatus = codingStepVerifyFail
					if summary != "" {
						summary = summary + "\n\n" + gateSum
					} else {
						summary = gateSum
					}
					h.updateStickyCodingStepVerify(userID, t.Index, vcmd, false, gateSum)
					_ = runCodingWorkbenchHookPhase(projectPath, hooks, "post_step")
					return &v2.TaskRunResult{
						TaskIndex:     t.Index,
						Title:         t.Title,
						Status:        status,
						Summary:       summary,
						FilesCreated:  v1Result.FilesCreated,
						FilesModified: v1Result.FilesModified,
						Error:         "step verification failed: " + vcmd,
					}
				}
				h.updateStickyCodingStepVerify(userID, t.Index, vcmd, true, gateSum)
				if summary != "" {
					summary = summary + "\n\n" + gateSum
				} else {
					summary = gateSum
				}
			} else {
				h.updateStickyCodingStepStatus(userID, t.Index, stepStatus, summary)
			}
		} else {
			h.updateStickyCodingStepStatus(userID, t.Index, stepStatus, summary)
		}
		_ = runCodingWorkbenchHookPhase(projectPath, hooks, "post_step")
		return &v2.TaskRunResult{
			TaskIndex:     t.Index,
			Title:         t.Title,
			Status:        status,
			Summary:       summary,
			FilesCreated:  v1Result.FilesCreated,
			FilesModified: v1Result.FilesModified,
			Error:         v1Result.Error,
		}
	}

	// Single-task turns used to always enable TDD. That is correct for
	// "fix the login bug" but wrong for operational pings like
	// "运行下生成的游戏" — those must not enter red/green test generation.
	tddMode := shouldEnableCodingTDD(recordUserText, planned, len(tasks))
	operational := looksLikeCodingOperationalRequest(recordUserText)
	if tddMode {
		log.Printf("[workflow-v2] pure coding: TDD enabled for single implement task user=%s", userID)
	}
	if operational {
		log.Printf("[workflow-v2] pure coding: operational run/build path user=%s", userID)
	}
	maxRetries := 2
	if operational {
		// Run/demo follow-ups should fail fast rather than thrash implement retries.
		maxRetries = 0
	}
	config := v2.TaskRunnerConfig{
		ProjectPath: projectPath,
		MaxRetries:  maxRetries,
		TDDMode:     tddMode,
		MaxParallel: maxParallel,
	}

	// Route SubAgent thinking to reasoning panel (collapsed)
	reasoningToken := onToken
	if onToken != nil {
		reasoningToken = func(delta string) {
			// Strip Browser: role prefix hallucination from reasoning output
			if strings.HasPrefix(delta, "Browser:") || strings.HasPrefix(delta, "Browser：") {
				delta = strings.TrimPrefix(strings.TrimPrefix(delta, "Browser:"), "Browser：")
				delta = strings.TrimLeft(delta, " ")
			}
			onToken("\x01" + delta)
		}
	}

	runCtx := context.Background()
	var cancelRun context.CancelFunc
	if loopCtx != nil {
		runCtx, cancelRun = loopCtx.Context()
		if cancelRun != nil {
			defer cancelRun()
		}
	}
	// Codex/Claude Code-style checklist once multi-step plan is armed.
	if planned {
		memSteps := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
		if len(memSteps) == 0 {
			memSteps = codingWorkbenchStepsFromTasks(tasks, codingStepPending)
		}
		if checklist := formatCodingStepsChecklist(memSteps); checklist != "" {
			if onToken != nil {
				onToken("\n\n" + checklist + "\n\n")
			}
			if onProgress != nil {
				onProgress(checklist)
			}
		}
	}
	runner := v2.NewTaskRunner(config, subAgentFn)
	runResults := runner.RunAll(runCtx, tasks, reasoningToken, func(progress string) {
		log.Printf("[workflow-v2-template] %s", progress)
		if onProgress != nil {
			onProgress(progress)
		}
	})
	// TaskRunner may skip later steps without calling SubAgent — mirror into sticky Todo.
	for _, rr := range runResults {
		if rr.Status == v2.TaskSkipped {
			h.updateStickyCodingStepStatus(userID, rr.TaskIndex, codingStepSkipped, rr.Error)
		}
	}
	report := runner.FinalReport()
	if lastCodingResult != nil {
		// Aggregate file audit across multi-step runs for sticky memory.
		lastCodingResult.FilesModified = uniqueSortedSubAgentStrings(append(lastCodingResult.FilesModified, mergedModified...))
		lastCodingResult.FilesCreated = uniqueSortedSubAgentStrings(append(lastCodingResult.FilesCreated, mergedCreated...))
		lastCodingResult.ToolCalls = totalToolCalls
		lastCodingResult.Iterations = totalIters
	}
	// Always refresh source preview after the turn: write_file mid-run events
	// can be missed when the agent only uses bash, or when a later step runs
	// against already-generated code without further edits.
	// lastCodingResult already aggregates multi-step file lists above.
	previewModified := mergedModified
	previewCreated := mergedCreated
	if lastCodingResult != nil {
		previewModified = lastCodingResult.FilesModified
		previewCreated = lastCodingResult.FilesCreated
	}
	stickyFiles := uniqueSortedSubAgentStrings(append(append([]string{}, sessionMem.FilesModified...), sessionMem.FilesCreated...))
	emittedPreview := emitCodingWorkbenchSourcePreview(h.app, codeSessionID, projectPath, previewModified, previewCreated, stickyFiles, true, true, previewRoutePath)
	h.recordStickyLocalCodingTurn(userID, projectPath, recordUserText, lastCodingResult)
	// Bash-only turns: no tool audit and no prior sticky → end-of-turn used a
	// project scan. Persist those paths so re-arm can restore the preview.
	// Skip when write_file already audited files or sticky already had history
	// (avoids a second RMW that only re-writes the same sticky fill).
	if shouldStickyMergePreviewScan(previewModified, previewCreated, stickyFiles, emittedPreview) {
		h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
			if mem == nil {
				return
			}
			mem.FilesModified = uniqueSortedSubAgentStrings(append(mem.FilesModified, emittedPreview...))
			if len(mem.FilesModified) > 40 {
				mem.FilesModified = mem.FilesModified[len(mem.FilesModified)-40:]
			}
		})
	}
	h.accumulateStickyCodingUsage(userID, totalInTok, totalOutTok, totalCost)
	if postTurn := runCodingWorkbenchHookPhase(projectPath, hooks, "post_turn"); postTurn.Report != "" {
		log.Printf("[coding-hooks] post_turn: %s", truncateRunesV2(postTurn.Report, 200))
	}

	// Push final report as visible content
	if onToken != nil {
		onToken("\n\n" + report)
	}

	log.Printf("[workflow-v2] pure coding complete: user=%s planned=%v steps=%d tokens_in=%d out=%d\n%s",
		userID, planned, len(tasks), totalInTok, totalOutTok, report)

	// Populate TraceEventCount so /goal continuation does not false-pause after
	// two pure-coding turns (no-tool suppression uses TraceEventCount as proxy).
	header := codingWorkbenchRunHeader(planned, len(tasks), runResults)
	memAfter := h.getStickyCodingWorkbenchMemory(userID)
	costLine := formatCodingSessionCostLine(memAfter)
	body := fmt.Sprintf("%s\n项目路径：%s\n\n%s", header, projectPath, report)
	if costLine != "" {
		body = body + "\n\n" + costLine
	}
	if m := strings.TrimSpace(memAfter.LastRouteModel); m != "" {
		body = body + fmt.Sprintf("\n路由: %s", m)
		if s := strings.TrimSpace(memAfter.LastRouteSource); s != "" {
			body = body + fmt.Sprintf(" (%s)", s)
		}
	}
	resp := &IMAgentResponse{Text: body}
	h.applyCodingUsageToResponse(userID, resp, totalInTok, totalOutTok, totalCost)
	if memAfter.LastRouteModel != "" {
		resp.RouteModel = memAfter.LastRouteModel
		resp.RouteSource = memAfter.LastRouteSource
		resp.RouteTask = memAfter.LastRouteTask
		resp.RouteReason = memAfter.LastRouteReason
	}
	if totalToolCalls > 0 || totalIters > 0 {
		resp.TraceEventCount = codingSubAgentActivityTraceCount(totalToolCalls, totalIters)
	} else if lastCodingResult != nil {
		resp.TraceEventCount = codingSubAgentActivityTraceCount(lastCodingResult.ToolCalls, lastCodingResult.Iterations)
	} else if strings.TrimSpace(report) != "" {
		resp.TraceEventCount = 1
	}
	return resp
}

// buildRemoteCodingPlanStepText builds the user prompt for one remote pure-coding
// plan step. When planned, it mirrors local pure-coding constraints: complete only
// the CURRENT step, then stop; later steps run as separate RemoteCodingSubAgent runs.
// Full multi-step implementation details are replaced with a titles-only outline so
// the model is not encouraged to implement T2–Tn during T1.
//
// stepIdx should be the 1-based plan index (prefer step.Index). stepTotal is the
// plan length used for "Tn/N" display.
// lastSummary should already be carry-sanitized for planned multi-step runs
// (see formatRemotePlanStepCarrySummary).
func buildRemoteCodingPlanStepText(
	step *v2.TaskItem,
	stepIdx, stepTotal int,
	planned bool,
	userText string,
	tasks []*v2.TaskItem,
	turnCount int,
	sessionPlan string,
	lastSummary string,
	filesModified []string,
) string {
	userText = strings.TrimSpace(userText)
	if step != nil && planned {
		if step.Index > 0 {
			stepIdx = step.Index
		}
		if stepTotal < 1 {
			stepTotal = 0
			for _, t := range tasks {
				if t != nil {
					stepTotal++
				}
			}
		}
		if stepTotal < 1 {
			stepTotal = stepIdx
		}
	}

	var cont strings.Builder
	// Typical remote step prompt is 1–2KB; pre-size to avoid mid-build growth.
	cont.Grow(1280)

	if step != nil && planned {
		title := strings.TrimSpace(step.Title)
		cont.WriteString(fmt.Sprintf("[Plan step T%d/%d] %s\n\n", stepIdx, stepTotal, title))
		if d := strings.TrimSpace(step.Description); d != "" {
			cont.WriteString(d)
			cont.WriteString("\n\n")
		}
		// Parity with local pure-coding stepReqCtx (same file, local path).
		cont.WriteString(fmt.Sprintf(
			"You are executing plan step T%d/%d: %s\n"+
				"Focus on this step only; do not skip ahead.\n"+
				"Complete the CURRENT step fully, then stop; later steps run as separate tasks.\n"+
				"Do not implement, create files for, verify, or report on work that belongs to later plan steps.\n"+
				"If you use todo_write, only subdivide THIS step — do not re-list the whole plan as todos.\n",
			stepIdx, stepTotal, title,
		))
		// Overall goal is context only; keep short so it does not dominate the step brief.
		if userText != "" {
			cont.WriteString("\n## Overall user request (context only — not a license to finish later steps)\n")
			cont.WriteString(truncateRunesV2(userText, 600))
		}
	} else {
		cont.WriteString(userText)
	}
	if turnCount > 0 || planned {
		cont.WriteString("\n\n[Session continuity")
		if turnCount > 0 {
			cont.WriteString(fmt.Sprintf(" turn %d", turnCount+1))
		}
		cont.WriteString("]")
	}
	// Multi-step planned: titles-only outline with current marker (never full later-step recipes).
	// Unplanned: session goal + prior summary only — never re-inject sticky full ExecutionPlan.
	if planned {
		outline := strings.TrimSpace(formatTaskListBriefWithCurrent(tasks, stepIdx))
		if outline == "" && step != nil {
			outline = fmt.Sprintf("- T%d: %s  ← current (do only this)", stepIdx, strings.TrimSpace(step.Title))
		}
		if outline != "" {
			cont.WriteString("\nPlan outline (titles only; later steps are separate remote tasks — do not execute them now):\n")
			cont.WriteString(outline)
		}
	} else if plan := strings.TrimSpace(sessionPlan); plan != "" {
		cont.WriteString("\nSession plan / overall goal:\n")
		cont.WriteString(truncateRunesV2(plan, 800))
	}
	if sum := strings.TrimSpace(lastSummary); sum != "" {
		cont.WriteString("\nPrevious turn summary:\n")
		// Carry summaries are pre-truncated for planned runs; still cap hard here.
		cont.WriteString(truncateRunesV2(sum, 600))
	}
	if len(filesModified) > 0 {
		files := uniqueSortedSubAgentStrings(filesModified)
		if len(files) > 12 {
			files = files[:12]
		}
		cont.WriteString("\nFiles modified earlier: ")
		cont.WriteString(strings.Join(files, ", "))
	}
	return cont.String()
}

func (h *IMMessageHandler) runRemoteCodingTemplateSubAgent(userID, userText string, remoteCtx remoteCodingTemplateContext, loopCtx *LoopContext, onProgress func(string), onToken func(string)) *IMAgentResponse {
	ensureLoopCtxUserID(loopCtx, userID)
	userText = strings.TrimSpace(userText)
	if userText == "" {
		userText = "执行远程编程任务"
	}
	// SSH/cancel preflight first; register before planning (see below).
	cleanupPureCodingRuntime := func() {}
	defer func() { cleanupPureCodingRuntime() }()
	// SSHSessionManager only retains live sessions in memory. A sticky remote
	// workbench, however, is persisted and can therefore outlive that manager
	// (for example after a timeout removes the session or after app restart).
	// Recreate a missing session from the stored non-secret coordinates before
	// reporting a project-directory error. sshConnect will use a configured key
	// or SSH agent when available; passwords deliberately remain non-persistent.
	var reconnectErr string
	remoteCtx, reconnectErr = h.recoverStickyRemoteCodingSSHSession(userID, remoteCtx)
	if reconnectErr != "" {
		return &IMAgentResponse{Text: reconnectErr}
	}
	if strings.TrimSpace(remoteCtx.SessionID) == "" {
		return &IMAgentResponse{Text: "远程编程无法启动：缺少 SSH 会话。请先连接远程服务器。"}
	}
	if remoteCtx.ProjectDir == "" {
		remoteCtx.ProjectDir = "."
	}
	if remoteCtx.WorkDir == "" {
		remoteCtx.WorkDir = remoteCtx.ProjectDir
	}

	if loopCtx != nil && loopCtx.IsCancelled() {
		return &IMAgentResponse{Text: "远程编程任务已取消"}
	}
	// After SSH is healthy: register before planning so guide-launch mid-plan
	// lands in pendingInjection. Plan-approve pause cleans up via defer.
	if loopCtx == nil {
		loopCtx = NewLoopContext("template-remote-coding-subagent", h.getMaclawAgentMaxIterations(), h.client)
		defer func() {
			loopCtx.Cancel()
			loopCtx.Done()
		}()
		ensureLoopCtxUserID(loopCtx, userID)
	}
	cleanupPureCodingRuntime = h.beginPureCodingRuntime(loopCtx, userID, userText)
	sessionMem := h.getStickyCodingWorkbenchMemory(userID)
	// Remote may not have local AGENTS.md; still try project dir if mirrored locally.
	// Hooks also load from the local task project path (.maclaw/hooks.json), not the SSH tree.
	localHooksPath := strings.TrimSpace(sessionMem.ProjectPath)
	if p := localHooksPath; p != "" {
		h.ensureStickyProjectInstructions(userID, p)
		sessionMem = h.getStickyCodingWorkbenchMemory(userID)
	}
	hooks := loadCodingWorkbenchHooks(localHooksPath)

	var tasks []*v2.TaskItem
	var planMarkdown string
	var planned bool
	recordUserText := userText
	if approved, ok := h.takeStickyApprovedCodingPlan(userID); ok {
		tasks = approved.Tasks
		planMarkdown = approved.Markdown
		planned = len(tasks) >= codingWorkbenchPlanMinTasks
		if s := strings.TrimSpace(approved.UserText); s != "" {
			recordUserText = s
			userText = s
		}
		h.persistCodingWorkbenchPlans(userID, planMarkdown, "")
		h.setStickyCodingStepStatuses(userID, codingWorkbenchStepsFromTasks(tasks, codingStepPending))
	} else {
		if strings.HasPrefix(strings.TrimSpace(userText), codingPlanApproveExecuteMarker) {
			userText = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userText), codingPlanApproveExecuteMarker))
			recordUserText = userText
		}
		if prePlan := runCodingWorkbenchHookPhase(localHooksPath, hooks, "pre_plan"); prePlan.Report != "" {
			log.Printf("[coding-hooks] remote pre_plan: %s", truncateRunesV2(prePlan.Report, 200))
		}
		tasks, planMarkdown, planned = h.resolveCodingWorkbenchTasks(userID, userText, remoteCtx.ProjectDir, sessionMem, onProgress, onToken)
		if planned && normalizeCodingPlanMode(sessionMem.PlanMode) == codingPlanModeApprove {
			if _, hasPending := h.loadStickyPendingCodingPlan(userID); hasPending {
				text := formatPendingPlanApprovalText(planMarkdown, len(tasks))
				return &IMAgentResponse{Text: text, Actions: codingPlanApproveActions()}
			}
		}
	}
	if planned {
		sessionMem = h.getStickyCodingWorkbenchMemory(userID)
	}
	if len(tasks) == 0 {
		tasks = []*v2.TaskItem{{Index: 1, Title: truncateRunesV2(userText, 80), Description: userText}}
		planned = false
	}

	buildRemoteTaskText := func(step *v2.TaskItem, stepIdx, stepTotal int) string {
		return buildRemoteCodingPlanStepText(
			step,
			stepIdx,
			stepTotal,
			planned,
			userText,
			tasks,
			sessionMem.TurnCount,
			sessionMem.SessionPlan,
			sessionMem.LastSummary,
			sessionMem.FilesModified,
		)
	}

	log.Printf("[workflow-v2] pure remote coding: user=%s session=%s project=%s task=%q sticky_turn=%d planned=%v steps=%d",
		userID, remoteCtx.SessionID, remoteCtx.ProjectDir, truncateRunesV2(userText, 80), sessionMem.TurnCount, planned, len(tasks))
	if onProgress != nil {
		if planned {
			onProgress(fmt.Sprintf("全功能远程编程：按计划执行 %d 步（SSH %s）", len(tasks), remoteCtx.SessionID))
		} else if sessionMem.TurnCount > 0 {
			onProgress(fmt.Sprintf("全功能远程编程：继续第 %d 轮（SSH %s）", sessionMem.TurnCount+1, remoteCtx.SessionID))
		} else {
			onProgress(fmt.Sprintf("全功能远程编程：使用 SSH 会话 %s 开始执行", remoteCtx.SessionID))
		}
	}
	// Codex/Claude Code-style checklist banner once the multi-step plan is armed.
	if planned {
		memSteps := h.getStickyCodingWorkbenchMemory(userID).StepStatuses
		if len(memSteps) == 0 {
			memSteps = codingWorkbenchStepsFromTasks(tasks, codingStepPending)
		}
		if checklist := formatCodingStepsChecklist(memSteps); checklist != "" {
			if onToken != nil {
				onToken("\n\n" + checklist + "\n\n")
			}
			if onProgress != nil {
				onProgress(checklist)
			}
		}
	}

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	runner := remoteCodingTemplateRunner
	if runner == nil {
		runner = defaultRemoteCodingTemplateRunner
	}

	var result *RemoteCodingSubAgentResult
	totalTools, totalIters := 0, 0
	totalInTok, totalOutTok := 0, 0
	totalCost := 0.0
	var reportParts []string
	for i, step := range tasks {
		if step == nil {
			continue
		}
		if loopCtx != nil && loopCtx.IsCancelled() {
			break
		}
		if planned && onProgress != nil {
			onProgress(fmt.Sprintf("T%d/%d: %s", step.Index, len(tasks), step.Title))
		}
		if planned && onToken != nil {
			onToken(fmt.Sprintf("\n\n---\n### T%d: %s\n\n", step.Index, step.Title))
		}
		h.updateStickyCodingStepStatus(userID, step.Index, codingStepRunning, "")
		if preStepRes := runCodingWorkbenchHookPhase(localHooksPath, hooks, "pre_step"); preStepRes.Report != "" || preStepRes.Failed {
			if preStepRes.Report != "" {
				log.Printf("[coding-hooks] remote pre_step T%d: %s", step.Index, truncateRunesV2(preStepRes.Report, 160))
			}
			if codingHookShouldAbort(hooks, preStepRes) {
				sum := "pre_step hook failed (fail_on_error)\n" + preStepRes.Report
				h.updateStickyCodingStepStatus(userID, step.Index, codingStepFailed, sum)
				reportParts = append(reportParts, fmt.Sprintf("### T%d: %s\n状态: failed\n%s", step.Index, step.Title, truncateRunesV2(sum, 800)))
				result = &RemoteCodingSubAgentResult{Status: "failed", Error: "pre_step hook failed", Summary: sum}
				h.markRemainingCodingStepsSkipped(userID, step.Index, "skipped: prior step failed")
				break
			}
		}
		stepIdx := i + 1
		if step.Index > 0 {
			stepIdx = step.Index
		}
		taskText := buildRemoteTaskText(step, stepIdx, len(tasks))

		// Remote write isolation (temp dir copy) when worktree mode allows.
		stepRemoteCtx := remoteCtx
		var isolate *remoteCodingIsolate
		wtMode := h.getStickyCodingWorktreeMode(userID)
		if shouldUseRemoteCodingIsolate(wtMode, planned, step.Title, step.Description, step.DependsOn) {
			allowCopy := normalizeCodingWorktreeMode(wtMode) == codingWorktreeModeAlways
			iso, isoErr := createRemoteCodingIsolate(h, remoteCtx.SessionID, remoteCtx.ProjectDir, step.Index, allowCopy)
			if isoErr != nil {
				log.Printf("[remote-isolate] T%d create failed: %v — using main remote dir", step.Index, isoErr)
				if onProgress != nil && allowCopy {
					onProgress(fmt.Sprintf("T%d 远程隔离目录创建失败，使用主目录: %v", step.Index, isoErr))
				}
			} else if iso != nil {
				isolate = iso
				stepRemoteCtx.ProjectDir = iso.IsolateDir
				stepRemoteCtx.WorkDir = iso.IsolateDir
				if onProgress != nil {
					onProgress(fmt.Sprintf("T%d 在远程隔离目录执行: %s", step.Index, iso.IsolateDir))
				}
			}
		}

		stepResult := runner(h, cfg, httpClient, stepRemoteCtx, loopCtx, taskText, onProgress, onToken)
		if isolate != nil {
			if stepResult != nil && stepResult.Status == "success" {
				if mergeSum, mergeErr := isolate.mergeBack(h); mergeErr != nil {
					log.Printf("[remote-isolate] T%d merge failed: %v", step.Index, mergeErr)
					h.storeStickyCodingConflict(userID, codingWorkbenchConflict{
						StepIndex:   step.Index,
						Path:        isolate.IsolateDir,
						MainProject: remoteCtx.ProjectDir,
						Error:       mergeErr.Error(),
						Kind:        "remote_isolate",
					})
					// Local project path (task root) for hooks.json — not remote isolate dir.
					h.fireCodingOnConflictHook(userID, "")
					hint := mergeErr.Error() + "\n\n可用 `/worktree conflicts` 查看，`/worktree discard <id>` 丢弃远程隔离目录。"
					if stepResult.Summary != "" {
						stepResult.Summary += "\n\nremote isolate merge failed: " + hint
					} else {
						stepResult.Summary = "remote isolate merge failed: " + hint
					}
					stepResult.Status = "failed"
					stepResult.Error = mergeErr.Error()
					// keep isolate for inspection
				} else {
					if onProgress != nil && mergeSum != "" {
						onProgress(mergeSum)
					}
					if mergeSum != "" {
						if stepResult.Summary != "" {
							stepResult.Summary += "\n\n" + mergeSum
						} else {
							stepResult.Summary = mergeSum
						}
					}
					isolate.cleanup(h)
				}
			} else {
				isolate.cleanup(h)
			}
		}
		// Attachments only on first remote step.
		if loopCtx != nil && len(loopCtx.CodingAttachments) > 0 {
			loopCtx.CodingAttachments = nil
		}

		if stepResult != nil {
			result = stepResult
			totalTools += stepResult.ToolCalls
			totalIters += stepResult.Iterations
			totalInTok += stepResult.InputTokens
			totalOutTok += stepResult.OutputTokens
			totalCost += stepResult.EstCostRMB
			if stepResult.RouteModel != "" || stepResult.RouteSource != "" {
				h.recordStickyCodingRoute(userID, stepResult.RouteModel, stepResult.RouteSource, stepResult.RouteTask, stepResult.RouteReason)
			}
			sum := strings.TrimSpace(stepResult.Summary)
			if sum == "" {
				sum = strings.TrimSpace(stepResult.Error)
			}
			stepStatus := codingStepFailed
			if stepResult.Status == "success" {
				stepStatus = codingStepPassed
			}
			// Remote step verification gate (parity with local pure-coding).
			if stepResult.Status == "success" && planned &&
				stepNeedsVerifyGate(step.Title, step.Description, step.Index, len(tasks)) {
				preV := runCodingWorkbenchHookPhase(localHooksPath, hooks, "pre_verify")
				if preV.Report != "" {
					log.Printf("[coding-hooks] remote pre_verify T%d: %s", step.Index, truncateRunesV2(preV.Report, 160))
				}
				if codingHookShouldAbort(hooks, preV) {
					stepStatus = codingStepVerifyFail
					stepResult.Status = "failed"
					gateSum := "pre_verify hook failed (fail_on_error)\n" + preV.Report
					if sum != "" {
						sum = sum + "\n\n" + gateSum
					} else {
						sum = gateSum
					}
					h.updateStickyCodingStepStatus(userID, step.Index, stepStatus, sum)
					reportParts = append(reportParts, fmt.Sprintf("### T%d: %s\n状态: verify_failed\n%s", step.Index, step.Title, truncateRunesV2(sum, 800)))
					setRemotePlanLastSummary(&sessionMem, planned, step.Index, step.Title, "verify_failed", sum)
					_ = runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_step")
					h.markRemainingCodingStepsSkipped(userID, step.Index, "skipped: prior step failed")
					break
				}
				ok, vcmd, vout, skipped := runCodingWorkbenchRemoteStepVerify(h, remoteCtx.SessionID, remoteCtx.ProjectDir)
				gateSum := codingWorkbenchStepGateSummary(ok, vcmd, vout, skipped)
				if !skipped {
					if onProgress != nil {
						if ok {
							onProgress(fmt.Sprintf("T%d 远程步级验证通过: %s", step.Index, vcmd))
						} else {
							onProgress(fmt.Sprintf("T%d 远程步级验证失败: %s", step.Index, vcmd))
						}
					}
					if onToken != nil {
						onToken("\n\n" + gateSum + "\n")
					}
					if postV := runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_verify"); postV.Report != "" {
						log.Printf("[coding-hooks] remote post_verify T%d: %s", step.Index, truncateRunesV2(postV.Report, 160))
					}
					if !ok {
						stepStatus = codingStepVerifyFail
						stepResult.Status = "failed"
						if sum != "" {
							sum = sum + "\n\n" + gateSum
						} else {
							sum = gateSum
						}
						h.updateStickyCodingStepVerify(userID, step.Index, vcmd, false, gateSum)
						if sum != "" {
							reportParts = append(reportParts, fmt.Sprintf("### T%d: %s\n状态: verify_failed\n%s", step.Index, step.Title, truncateRunesV2(sum, 800)))
							setRemotePlanLastSummary(&sessionMem, planned, step.Index, step.Title, "verify_failed", sum)
						}
						_ = runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_step")
						h.markRemainingCodingStepsSkipped(userID, step.Index, "skipped: prior step failed")
						break
					}
					h.updateStickyCodingStepVerify(userID, step.Index, vcmd, true, gateSum)
					if sum != "" {
						sum = sum + "\n\n" + gateSum
					} else {
						sum = gateSum
					}
				} else {
					h.updateStickyCodingStepStatus(userID, step.Index, stepStatus, sum)
				}
			} else {
				h.updateStickyCodingStepStatus(userID, step.Index, stepStatus, sum)
			}
			_ = runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_step")
			if sum != "" {
				reportParts = append(reportParts, fmt.Sprintf("### T%d: %s\n状态: %s\n%s", step.Index, step.Title, stepResult.Status, truncateRunesV2(sum, 800)))
				// Feed prior step outcome into subsequent steps (sanitized when multi-step).
				setRemotePlanLastSummary(&sessionMem, planned, step.Index, step.Title, stepResult.Status, sum)
			}
			// Stream a one-line checkmark update (Claude Code-style).
			if planned && onProgress != nil {
				mark := "✗"
				if stepResult.Status == "success" {
					mark = "☑"
				}
				onProgress(fmt.Sprintf("%s T%d %s", mark, step.Index, strings.TrimSpace(step.Title)))
			}
			// Sequential plans: stop on first hard failure (matches TaskRunner depends_on skip).
			if stepResult.Status != "success" {
				h.markRemainingCodingStepsSkipped(userID, step.Index, "skipped: prior step failed")
				break
			}
		} else {
			h.updateStickyCodingStepStatus(userID, step.Index, codingStepFailed, "nil result")
			_ = runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_step")
			h.markRemainingCodingStepsSkipped(userID, step.Index, "skipped: prior step failed")
			break
		}
	}

	h.recordStickyRemoteCodingTurn(userID, recordUserText, result)
	h.accumulateStickyCodingUsage(userID, totalInTok, totalOutTok, totalCost)
	if postTurn := runCodingWorkbenchHookPhase(localHooksPath, hooks, "post_turn"); postTurn.Report != "" {
		log.Printf("[coding-hooks] remote post_turn: %s", truncateRunesV2(postTurn.Report, 200))
	}
	if result == nil {
		return &IMAgentResponse{Text: "远程编程执行失败：RemoteCodingSubAgent 没有返回结果。"}
	}

	memAfter := h.getStickyCodingWorkbenchMemory(userID)
	passedSteps, failedSteps, skippedSteps := countCodingWorkbenchStepOutcomes(memAfter.StepStatuses)
	totalSteps := len(tasks)
	if planned && totalSteps == 0 {
		totalSteps = len(memAfter.StepStatuses)
	}
	statusText := formatRemoteCodingPlanStatusText(planned, result.Status, totalSteps, passedSteps, failedSteps, skippedSteps)
	summary := strings.TrimSpace(result.Summary)
	if planned && len(reportParts) > 0 {
		summary = strings.Join(reportParts, "\n\n")
	}
	if summary == "" {
		summary = strings.TrimSpace(result.Error)
	}
	if result.Status != "success" && strings.TrimSpace(result.Error) != "" && !strings.Contains(summary, result.Error) {
		summary = strings.TrimSpace(summary) + "\n\n失败原因：" + compactSubAgentErrorSummary(result.Error)
	}
	if summary == "" {
		summary = fmt.Sprintf("状态：%s", result.Status)
	}
	// Always append a final checklist so users are not misled by a step agent
	// saying "waiting for Tn" when the orchestrator actually stopped.
	if planned {
		checklist := formatCodingStepsChecklist(memAfter.StepStatuses)
		incompleteNote := formatRemoteCodingPlanIncompleteNote(totalSteps, passedSteps, failedSteps, skippedSteps)
		if checklist != "" {
			summary = strings.TrimSpace(summary) + "\n\n" + checklist
		}
		if incompleteNote != "" {
			summary = strings.TrimSpace(summary) + "\n\n" + incompleteNote
		}
		if onToken != nil {
			// Stream the final plan outcome so the chat surface is not only the
			// last subagent "waiting for next step" narrative.
			streamBody := statusText
			if checklist != "" {
				streamBody += "\n\n" + checklist
			}
			onToken("\n\n---\n### 计划执行结果\n\n" + streamBody + "\n")
		}
	}
	costLine := formatCodingSessionCostLine(memAfter)
	text := fmt.Sprintf("%s\nSSH 会话：%s\n远程项目目录：%s\n\n%s", statusText, remoteCtx.SessionID, remoteCtx.ProjectDir, summary)
	if costLine != "" {
		text = text + "\n\n" + costLine
	}
	if m := strings.TrimSpace(memAfter.LastRouteModel); m != "" {
		text = text + fmt.Sprintf("\n路由: %s", m)
		if s := strings.TrimSpace(memAfter.LastRouteSource); s != "" {
			text = text + fmt.Sprintf(" (%s)", s)
		}
	}
	resp := &IMAgentResponse{
		Text:            text,
		TraceEventCount: codingSubAgentActivityTraceCount(totalTools, totalIters),
	}
	h.applyCodingUsageToResponse(userID, resp, totalInTok, totalOutTok, totalCost)
	if memAfter.LastRouteModel != "" {
		resp.RouteModel = memAfter.LastRouteModel
		resp.RouteSource = memAfter.LastRouteSource
		resp.RouteTask = memAfter.LastRouteTask
		resp.RouteReason = memAfter.LastRouteReason
	}
	return resp
}

// recoverStickyRemoteCodingSSHSession creates a fresh managed SSH session when
// the sticky session ID is no longer present. It intentionally uses only
// persisted non-secret coordinates and any configured key/agent auth; a missing
// password must be supplied through the normal connection UI instead of being
// stored in a workbench file.
func (h *IMMessageHandler) recoverStickyRemoteCodingSSHSession(userID string, remoteCtx remoteCodingTemplateContext) (remoteCodingTemplateContext, string) {
	if h == nil || strings.TrimSpace(remoteCtx.SessionID) == "" {
		return remoteCtx, ""
	}
	if _, ok := h.ensureSSHManager().Get(remoteCtx.SessionID); ok {
		return remoteCtx, ""
	}

	mem := h.getStickyCodingWorkbenchMemory(userID)
	// Only recover a session that actually belongs to this persisted sticky
	// workbench. Ad-hoc/template contexts may intentionally supply a session ID
	// through a test or an external runtime and must retain their existing
	// execution path instead of being replaced with a reconnect prompt.
	if mem.Kind != "remote" || strings.TrimSpace(mem.RemoteSessionID) != strings.TrimSpace(remoteCtx.SessionID) {
		return remoteCtx, ""
	}
	host := strings.TrimSpace(mem.RemoteHost)
	user := strings.TrimSpace(mem.RemoteUser)
	port := mem.RemotePort
	if host == "" || user == "" {
		return remoteCtx, fmt.Sprintf("远程 SSH 会话 %s 已失效，且没有可用于自动重建的主机信息。请重新连接后继续。", remoteCtx.SessionID)
	}
	if port <= 0 || port >= 65536 {
		port = 22
	}

	args := map[string]interface{}{
		"host": host,
		"user": user,
		"port": float64(port),
	}
	// Reuse non-secret configured authentication metadata when this host has a
	// profile. Password and passphrase are intentionally not copied here.
	for _, entry := range h.loadSSHHosts() {
		entryPort := entry.Port
		if entryPort <= 0 {
			entryPort = 22
		}
		if strings.EqualFold(strings.TrimSpace(entry.Host), host) && strings.EqualFold(strings.TrimSpace(entry.User), user) && entryPort == port {
			if value := strings.TrimSpace(entry.Label); value != "" {
				args["label"] = value
			}
			if value := strings.TrimSpace(entry.AuthMethod); value != "" {
				args["auth_method"] = value
			}
			if value := strings.TrimSpace(entry.KeyPath); value != "" {
				args["key_path"] = value
			}
			break
		}
	}

	connectResult := h.sshConnect(args)
	newSessionID := extractSSHSessionIDFromConnectResult(connectResult)
	if newSessionID == "" {
		return remoteCtx, fmt.Sprintf("远程 SSH 会话 %s 已失效，已尝试自动新建 %s@%s:%d，但未能建立连接。请在远程连接面板重新认证后继续。\n\n%s", remoteCtx.SessionID, user, host, port, truncateRunesV2(connectResult, 500))
	}

	oldSessionID := remoteCtx.SessionID
	remoteCtx.SessionID = newSessionID
	h.bindStickyRemoteCodingContext(userID, remoteCtx, host, user, port)
	log.Printf("[workflow-v2] recreated missing sticky SSH session old=%s new=%s host=%s", oldSessionID, newSessionID, fmt.Sprintf("%s@%s:%d", user, host, port))
	return remoteCtx, ""
}

// codingSubAgentActivityTraceCount maps SubAgent work into IMAgentResponse.TraceEventCount
// so goal-continuation no-tool suppression treats pure coding turns as active work.
func codingSubAgentActivityTraceCount(toolCalls, iterations int) int {
	if toolCalls > 0 {
		return toolCalls
	}
	if iterations > 0 {
		return iterations
	}
	return 0
}

type remoteCodingTemplateRunnerFunc func(*IMMessageHandler, corelib.MaclawLLMConfig, *http.Client, remoteCodingTemplateContext, *LoopContext, string, func(string), func(string)) *RemoteCodingSubAgentResult

var remoteCodingTemplateRunner remoteCodingTemplateRunnerFunc

func defaultRemoteCodingTemplateRunner(h *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, remoteCtx remoteCodingTemplateContext, loopCtx *LoopContext, userText string, onProgress func(string), onToken func(string)) *RemoteCodingSubAgentResult {
	subAgent := NewRemoteCodingSubAgent(h, cfg, httpClient, remoteCtx.SessionID, remoteCtx.WorkDir, remoteCtx.ProjectDir, loopCtx)
	subAgent.SetSourcePreviewEnabled(true)
	// Wire approval BEFORE SetCallbacks so we never briefly install a
	// prompt-everything state that could race with the first tool call.
	// Input-box "完全控制" (full) skips path + high-risk prompts.
	if h != nil && h.app != nil {
		globalFull := h.app.isSubAgentFullAccessGranted()
		userID := ""
		if loopCtx != nil {
			userID = loopCtx.UserID
		}
		fullAccess := h.stickyCodingEffectiveFullAccess(userID, globalFull)
		subAgent.SetHighRiskApprovalCallback(buildRemoteHighRiskApprovalCallback(h, loopCtx, onProgress), fullAccess)
		// When not already full, overlay sticky path trust / high-risk / allow_dir.
		if !fullAccess && subAgent.highRiskApproval != nil && userID != "" {
			h.applyStickyRemoteCodingPermissions(userID, subAgent.highRiskApproval)
		}
		subAgent.SetKnowledgeStores(h.app.ensureCodingKnowledgeStore(), getAutoRecallStoreForApp(h.app, false))
	}
	// SetCallbacks after approval config: highRiskApprovalExplicit blocks overwrite.
	subAgent.SetCallbacks(onToken, onProgress)
	return subAgent.ExecuteTask(userText, "")
}

// callLightweightLLM makes a quick non-streaming LLM call for lightweight
// workflow classification helpers.
func (h *IMMessageHandler) callLightweightLLM(cfg corelib.MaclawLLMConfig, systemPrompt, userText string, timeoutSec int) string {
	if h == nil || h.client == nil {
		return ""
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userText},
	}

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "workflow-v2-lightweight"})
		resp, err := doSimpleLLMRequest(ctx, cfg, messages, h.client, time.Duration(timeoutSec)*time.Second)
		if err != nil {
			log.Printf("[workflow-v2] callLightweightLLM: attempt %d/%d failed: %v", attempt, maxAttempts, err)
			if attempt < maxAttempts {
				time.Sleep(2 * time.Second)
				continue
			}
			return ""
		}
		if resp == nil {
			if attempt < maxAttempts {
				time.Sleep(2 * time.Second)
				continue
			}
			return ""
		}
		content := strings.TrimSpace(resp.Content)
		if content == "" && attempt < maxAttempts {
			// Model returned empty content — retry
			time.Sleep(2 * time.Second)
			continue
		}
		return content
	}
	return ""
}

// --- Workflow Confirm Choice (user decides whether to enter workflow) ---

const (
	workflowChoiceCommandPrefix = "__workflow_choice__"
	workflowChoiceComplex       = "complex" // Full SDD / structured workflow
	workflowChoiceSkip          = "skip"    // Not a workflow task, use normal agent loop
	workflowChoiceDirect        = "direct"  // Non-coding direct handling, use normal agent loop
)

// pendingWorkflowChoice stores the original route result while waiting for user choice.
type pendingWorkflowChoice struct {
	Msg         IMUserMessage
	RouteResult *v2.RouteResult
	ChoiceID    string
}

type remoteCodingTemplateContext struct {
	SessionID  string
	WorkDir    string
	ProjectDir string
}

func scrubActivePhaseSensitiveFormData(state *v2.WorkflowState) bool {
	if state == nil {
		return false
	}
	phase := state.ActivePhase()
	if phase == nil || phase.FormData == nil {
		return false
	}
	sensitiveNames := map[string]bool{}
	if phase.InputSchema != nil {
		collect := func(fields []v2.PhaseInputField) {
			for _, field := range fields {
				if field.Sensitive && strings.TrimSpace(field.Name) != "" {
					sensitiveNames[field.Name] = true
				}
			}
		}
		collect(phase.InputSchema.Fields)
		for _, variant := range phase.InputSchema.Variants {
			collect(variant.Fields)
		}
	}
	changed := false
	for key := range phase.FormData {
		lower := strings.ToLower(strings.TrimSpace(key))
		if sensitiveNames[key] || strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") {
			delete(phase.FormData, key)
			changed = true
		}
	}
	return changed
}

func (h *IMMessageHandler) hasPendingTemplateSubAgentExecution(userID string) bool {
	if h == nil {
		return false
	}
	if _, pending := h.pendingV2SubAgentExecution.Load(userID); !pending {
		return false
	}
	if _, ok := h.pendingTemplateCodingProjectPath.Load(userID); ok {
		return true
	}
	if _, ok := h.pendingTemplateRemoteCoding.Load(userID); ok {
		return true
	}
	return false
}

func buildWorkflowChoiceCommand(choice, choiceID string) string {
	return workflowChoiceCommandPrefix + " " + choice + " " + choiceID
}

func parseWorkflowChoiceCommand(text string) (choice, choiceID string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || fields[0] != workflowChoiceCommandPrefix {
		return "", "", false
	}
	return fields[1], fields[2], true
}

func inferRemoteProjectDirFromSSHSession(initialCommand string) string {
	cmd := strings.TrimSpace(initialCommand)
	for _, separator := range []string{"&&", ";", "\n"} {
		cmd = strings.ReplaceAll(cmd, separator, "\n")
	}
	for _, part := range strings.Split(cmd, "\n") {
		if dir := remoteProjectDirFromCDCommand(part); dir != "" {
			return dir
		}
	}
	return ""
}

func remoteProjectDirFromCDCommand(commandPart string) string {
	part := strings.TrimSpace(commandPart)
	if part == "cd" || !strings.HasPrefix(part, "cd") {
		return ""
	}
	rest := strings.TrimPrefix(part, "cd")
	if rest == "" || rest == part {
		return ""
	}
	if rest[0] != ' ' && rest[0] != '\t' {
		return ""
	}
	dir := strings.TrimSpace(rest)
	for {
		switch {
		case strings.HasPrefix(dir, "-- "):
			dir = strings.TrimSpace(strings.TrimPrefix(dir, "--"))
		case strings.HasPrefix(dir, "-P "):
			dir = strings.TrimSpace(strings.TrimPrefix(dir, "-P"))
		case strings.HasPrefix(dir, "-L "):
			dir = strings.TrimSpace(strings.TrimPrefix(dir, "-L"))
		default:
			goto parseTarget
		}
	}

parseTarget:
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "\"") || strings.HasPrefix(dir, "'") {
		quote := dir[:1]
		dir = strings.TrimPrefix(dir, quote)
		if idx := strings.Index(dir, quote); idx >= 0 {
			dir = dir[:idx]
		}
	} else if fields := strings.Fields(dir); len(fields) > 0 {
		dir = fields[0]
	}
	return strings.TrimSpace(dir)
}

// askWorkflowConfirmChoice presents the user with a choice before entering a workflow.
// For coding: full SDD workflow vs skip (pure coding is created via task management).
// For other templates: enter workflow / handle directly.
func (h *IMMessageHandler) askWorkflowConfirmChoice(msg IMUserMessage, result *v2.RouteResult) workflowIMRouteResult {
	choiceID := fmt.Sprintf("wc_%d", time.Now().UnixMilli())

	// Store pending state
	h.pendingWorkflowChoice.Store(msg.UserID, &pendingWorkflowChoice{
		Msg:         msg,
		RouteResult: result,
		ChoiceID:    choiceID,
	})

	var text string
	var actions []IMResponseAction

	if result.WorkflowType == "coding" {
		text = "识别到这可能是一个**编程开发任务**，请选择处理方式：\n\n" +
			"**1. 完整开发流程 SDD（推荐用于中大型项目）**\n" +
			"系统引导完成：需求文档 → 技术设计 → 任务拆分 → 逐任务编码 → 验收\n" +
			"耗时较长，但能显著提升代码质量和可维护性：\n" +
			"• 需求先行，避免返工和需求遗漏\n" +
			"• 架构设计确保模块解耦、接口清晰\n" +
			"• 任务拆分让每个子任务可独立验证，降低集成风险\n" +
			"适合：多模块系统、游戏、完整应用、需要多人协作或长期维护的项目\n\n" +
			"**2. 这不是编程工作流任务**\n" +
			"不走编程工作流；小改动/快速编码请用任务管理中的「纯编程」环境\n" +
			"适合：翻译、整理文档、搜索资料，或已通过任务管理进入纯编程工作台的场景"
		actions = []IMResponseAction{
			{Label: "完整开发流程", Command: buildWorkflowChoiceCommand(workflowChoiceComplex, choiceID), Style: "primary"},
			{Label: "不是编程工作流", Command: buildWorkflowChoiceCommand(workflowChoiceSkip, choiceID), Style: "secondary"},
		}
	} else {
		templateName := result.WorkflowType
		if wf := h.getWorkflowV2(); wf != nil {
			if tmpl := wf.registry.Get(result.WorkflowType); tmpl != nil && tmpl.Name != "" {
				templateName = tmpl.Name
			}
		}

		// When a close runner-up exists, show disambiguation panel with both options.
		if result.RunnerUp != "" {
			runnerUpName := result.RunnerUp
			if wf := h.getWorkflowV2(); wf != nil {
				if tmpl := wf.registry.Get(result.RunnerUp); tmpl != nil && tmpl.Name != "" {
					runnerUpName = tmpl.Name
				}
			}
			text = fmt.Sprintf("识别到多个可能匹配的工作流，请选择：\n\n"+
				"**1. %s**\n"+
				"**2. %s**\n"+
				"**3. 直接处理**（不走工作流）", templateName, runnerUpName)
			// Store runner-up in pending choice for retrieval when user clicks option 2
			actions = []IMResponseAction{
				{Label: fmt.Sprintf("%s", templateName), Command: buildWorkflowChoiceCommand(workflowChoiceComplex, choiceID), Style: "primary"},
				{Label: fmt.Sprintf("%s", runnerUpName), Command: buildWorkflowChoiceCommand("alt_"+result.RunnerUp, choiceID), Style: "secondary"},
				{Label: "直接处理", Command: buildWorkflowChoiceCommand(workflowChoiceDirect, choiceID), Style: "secondary"},
			}
		} else {
			text = fmt.Sprintf("识别到这可能适合使用**%s**工作流，请选择处理方式：\n\n"+
				"**1. 进入%s工作流（推荐）**\n"+
				"系统按阶段引导完成，每个阶段产出结构化文档，完成后再进入下一阶段\n\n"+
				"**2. 直接处理**\n"+
				"不走工作流，当作普通任务由 AI 自由发挥完成", templateName, templateName)
			actions = []IMResponseAction{
				{Label: fmt.Sprintf("进入%s工作流", templateName), Command: buildWorkflowChoiceCommand(workflowChoiceComplex, choiceID), Style: "primary"},
				{Label: "直接处理", Command: buildWorkflowChoiceCommand(workflowChoiceDirect, choiceID), Style: "secondary"},
			}
		}
	}

	log.Printf("[workflow-v2] askWorkflowConfirmChoice: user=%s type=%s choiceID=%s", msg.UserID, result.WorkflowType, choiceID)

	return workflowIMRouteResult{
		Response: &IMAgentResponse{
			Text:    text,
			Actions: actions,
		},
	}
}

// handleCodingComplexityCommand intercepts structured choice commands from button clicks.
// Returns nil if the message is not a choice command.
// Also clears stale pending state when user sends a non-command message.
func (h *IMMessageHandler) handleCodingComplexityCommand(msg IMUserMessage, trimmed string) *workflowIMRouteResult {
	choice, choiceID, ok := parseWorkflowChoiceCommand(trimmed)
	if !ok {
		// Not a choice command — clear any stale pending state so the new message
		// flows through normal routing without interference.
		h.pendingWorkflowChoice.Delete(msg.UserID)
		return nil
	}

	// Consume pending choice
	raw, loaded := h.pendingWorkflowChoice.LoadAndDelete(msg.UserID)
	if !loaded {
		// No pending choice. Check if a workflow is already active for this user
		// (common scenario: user double-clicks the button, first click consumed
		// the pending and started the workflow, second click arrives here).
		if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
			if active := wf.machine.GetActive(msg.UserID); active != nil {
				// Workflow is running — re-emit the form if the current phase has one,
				// or return a helpful message pointing the user to the panel.
				if phase := active.ActivePhase(); phase != nil && phase.InputSchema != nil && phase.FormData == nil {
					prefilled := h.prefillWorkflowFormFields(msg.UserID, phase, "")
					h.emitWorkflowV2PhaseForm(msg.UserID, active, phase, prefilled)
					result := workflowIMRouteResult{
						Response: &IMAgentResponse{Text: "请在右侧任务面板填写信息后提交。"},
					}
					return &result
				}
				result := workflowIMRouteResult{
					Response: &IMAgentResponse{Text: "工作流已在进行中，请在右侧面板操作或直接输入内容继续。"},
				}
				return &result
			}
		}
		// Truly stale/expired button click — no active workflow either.
		result := workflowIMRouteResult{
			Response: &IMAgentResponse{Text: "选择已过期，请重新发送任务。"},
		}
		return &result
	}
	pending := raw.(*pendingWorkflowChoice)

	// Validate choiceID matches the stored pending — reject stale button clicks
	// from a previous prompt that was superseded by a new one.
	if pending.ChoiceID != choiceID {
		result := workflowIMRouteResult{
			Response: &IMAgentResponse{Text: "选择已过期，请重新发送任务。"},
		}
		return &result
	}

	switch choice {
	case workflowChoiceComplex:
		// Full SDD workflow (or full structured workflow for non-coding)
		log.Printf("[workflow-v2] user chose: COMPLEX (full workflow) type=%s", pending.RouteResult.WorkflowType)
		result := h.startNewWorkflowV2(pending.Msg, pending.RouteResult)
		return &result

	case workflowChoiceSkip:
		// Not a workflow task — route to normal agent loop with original message.
		// ReplayText tells the entry layer to substitute the button command with
		// the user's original task text for agent loop processing.
		log.Printf("[workflow-v2] user chose: SKIP (normal agent loop)")
		result := workflowIMRouteResult{
			ReplayText: pending.Msg.Text,
		}
		return &result

	case workflowChoiceDirect:
		log.Printf("[workflow-v2] user chose: DIRECT (normal agent loop) type=%s", pending.RouteResult.WorkflowType)
		result := workflowIMRouteResult{
			ReplayText: pending.Msg.Text,
		}
		return &result

	default:
		// Handle alt_<type> choices from disambiguation panel
		if strings.HasPrefix(choice, "alt_") {
			altType := strings.TrimPrefix(choice, "alt_")
			log.Printf("[workflow-v2] user chose: ALT template %s (was %s)", altType, pending.RouteResult.WorkflowType)
			// Override the route result with the user's chosen template
			pending.RouteResult.WorkflowType = altType
			result := h.startNewWorkflowV2(pending.Msg, pending.RouteResult)
			return &result
		}
		// Retired one-shot workflow buttons (coding_subagent / remote_coding_subagent / simple)
		// may still appear in older chat history. Point users at the supported paths.
		if choice == "coding_subagent" || choice == "remote_coding_subagent" || choice == "simple" {
			log.Printf("[workflow-v2] user chose retired template choice %q", choice)
			result := workflowIMRouteResult{
				Response: &IMAgentResponse{Text: "该入口已下线。请用任务管理创建「纯编程」环境，或重新发送任务并选择「完整开发流程」。"},
			}
			return &result
		}
		result := workflowIMRouteResult{
			Response: &IMAgentResponse{Text: "无效选择，请重新发送任务。"},
		}
		return &result
	}
}
