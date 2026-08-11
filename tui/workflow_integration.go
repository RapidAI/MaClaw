package main

// workflow_integration.go adds V2 workflow engine support to the TUI.
//
// The V2 engine (StateMachine + Router + SQLiteStore) is the sole runtime
// engine. initWorkflowEngine() and getWorkflowEngine() have been removed.
// The intent understanding LLM caller is wired to the same LLM config as
// the agent loop.
//
// Current limitations vs GUI:
// - No doc preview panel (TUI is text-only; documents are shown inline)
// - No SteeringWorkflowDetector (steering rules still work via agent loop)
// - No isolated GUI-style SubAgent UI. Explicit coding implementation phases
//   use a serial host adapter backed by corelib/codingruntime; other phases
//   continue to use the direct agent.RunLoop path.
//
// These can be added incrementally without changing the architecture.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

var tuiQuotedPathPattern = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)

type tuiPendingWorkflowStart struct {
	OriginalText string
	StartReply   string
	Intent       v2.StructuredIntent
}

type tuiWorkflowLLMCaller struct {
	app    *TUIApp
	client *http.Client
}

func (c *tuiWorkflowLLMCaller) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	if c == nil || c.app == nil {
		return "", fmt.Errorf("TUI workflow LLM caller is not initialized")
	}
	client := c.client
	if client == nil {
		client = &http.Client{Timeout: timeout + 5*time.Second}
	}
	resp, err := agent.DoSimpleLLMRequest(c.app.llmConfig, messages, client, timeout)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	return resp.Content, nil
}

// TUIWorkflowCallbacks implements v2.EngineCallbacks for the TUI.
// In TUI mode, events are logged but not emitted to a frontend; the
// workflow state is communicated through inline text in the chat.
//
// registry is the same WorkflowRegistry the engine holds. It lets
// EmitPhaseUpdate derive dashboard phase metadata through the single
// source-of-truth deriver (v2.PhaseMetadata) the GUI adapter uses,
// keeping the TUI in parity rather than maintaining a separate phase list.
type TUIWorkflowCallbacks struct {
	app      *TUIApp
	registry *v2.WorkflowRegistry
}

func (c *TUIWorkflowCallbacks) SendTextToUser(userID, text string) error {
	log.Printf("[TUI-workflow] text: %s", truncateTUI(text, 80))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitPhaseUpdate(userID string, state *v2.EngineState) error {
	if state == nil {
		return nil
	}
	// Derive dashboard phase metadata through the same single source-of-truth
	// deriver the GUI adapter uses (v2.PhaseMetadata), rather than
	// maintaining a separate phase list. TUI is text-only: there is no doc
	// preview board, so the metadata is logged structurally for parity.
	var phases []v2.PhaseMeta
	if c.registry != nil {
		phases = v2.PhaseMetadata(c.registry.Match(state.Type))
	}
	log.Printf("[TUI-workflow] phase update: type=%s phase=%s index=%d phases=%s",
		state.Type, state.CurrentPhase, state.PhaseIndex, formatTUIPhaseMeta(phases))
	return nil
}

// formatTUIPhaseMeta renders derived PhaseMeta as a compact structural log line.
// It mirrors what the GUI adapter attaches to workflow:phase_update so the TUI
// stays in parity with the dashboard's phase ordering, labels, and flags.
func formatTUIPhaseMeta(phases []v2.PhaseMeta) string {
	if len(phases) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(phases))
	for _, p := range phases {
		parts = append(parts, fmt.Sprintf("%d:%s(%s doc=%t skip=%t confirm=%t kind=%s policy=%s scope=%s orch=%t)",
			p.Index, p.ID, p.Name, p.ExpectsDocument, p.CanSkip, p.NeedsConfirm,
			p.Kind, p.ToolPolicy, p.MutationScope, p.ActivatesOrchestrator))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (c *TUIWorkflowCallbacks) EmitDocUpdate(userID, phaseID, content string) error {
	log.Printf("[TUI-workflow] doc update: phase=%s len=%d", phaseID, len(content))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitGateResult(userID, phaseID string, result *v2.QualityGateResult) error {
	log.Printf("[TUI-workflow] gate result: phase=%s", phaseID)
	return nil
}

func (c *TUIWorkflowCallbacks) GetLang() string {
	if c.app != nil {
		return c.app.appConfig.Language
	}
	return ""
}

// tuiWorkflowStore implements v2.PersistenceStore (in-memory no-op).
// TUI sessions are typically short-lived; workflow state doesn't need
// to survive restarts (the conversation history does, via ConversationMemory).
type tuiWorkflowStore struct{}

func (s *tuiWorkflowStore) SaveWorkflowState(state *v2.EngineState) error { return nil }
func (s *tuiWorkflowStore) LoadWorkflowState(userID string) (*v2.EngineState, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteWorkflowState(id string) error             { return nil }
func (s *tuiWorkflowStore) ListActiveWorkflows() ([]*v2.EngineState, error) { return nil, nil }
func (s *tuiWorkflowStore) SaveUnderstandingSession(session *v2.UnderstandingSession) error {
	return nil
}
func (s *tuiWorkflowStore) LoadUnderstandingSession(userID string) (*v2.UnderstandingSession, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteUnderstandingSession(userID string) error { return nil }
func (s *tuiWorkflowStore) CleanupExpired(olderThan time.Duration) error   { return nil }

// handleWorkflowInterception checks if the message should be handled by the
// workflow engine. Returns a non-empty string if the message was fully handled
// (the string is the response to show the user). Returns empty string if the
// message should proceed to the normal agent loop.
func (app *TUIApp) handleWorkflowInterception(text string) string {
	// V2 path: use V2 filter and understanding directly.
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return ""
	}

	userID := "tui-user"
	if pendingResp := app.handlePendingWorkflowStartTUI(userID, text); pendingResp != "" {
		return pendingResp
	}

	// V2 Router: check if the message should be routed to an active V2 v2.
	// This provides V2 persistence and LLM confirm classification.
	if result := app.routeWithV2Router(userID, text); result != "" {
		return result
	}

	filter := wf.filter
	if filter == nil {
		return ""
	}

	classification := filter.Classify(userID, text)
	log.Printf("[TUI-workflow] classify: %v text=%q", classification, truncateTUI(text, 60))

	switch classification {
	case v2.FilterActiveWorkflow:
		return app.handleActiveWorkflowTUI(text)

	case v2.FilterActiveUnderstanding:
		return app.handleActiveUnderstandingTUI(text)

	case v2.FilterNeedsUnderstanding:
		if shouldBypassTUIWorkflowUnderstanding(text) {
			return ""
		}
		return app.handleNeedsUnderstandingTUI(text)

	case v2.FilterSimpleDirective:
		return "" // pass through to normal agent loop
	}

	return ""
}

func (app *TUIApp) handleActiveWorkflowTUI(text string) string {
	userID := "tui-user"
	text = app.expandWorkflowAttachmentInput(userID, text)

	// V2 path: use StateMachine.HandleInput directly.
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		log.Printf("[TUI-workflow] handleActiveWorkflowTUI: V2 state unavailable, passing through")
		return ""
	}

	state := wf.machine.GetActive(userID)
	if state == nil {
		return ""
	}

	// V2 doesn't use form gates — TUI is text-only, no side panel forms.

	hr, err := wf.machine.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] V2 HandleInput error: %v", err)
		return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
	}
	if hr == nil {
		return ""
	}

	// Reload state after HandleInput (may have been mutated).
	state = wf.machine.GetActive(userID)

	return app.handleV2HandleResult(userID, hr, state)
}

func (app *TUIApp) handleWorkflowReviewTUI(userID, text string) string {
	// V2 path: use StateMachine.ApplyReviewIntent directly.
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		log.Printf("[TUI-workflow] handleWorkflowReviewTUI: V2 state unavailable")
		return app.workflowReviewBarrierText(userID)
	}

	intent := "other"
	rawIntent := ""
	needlePrediction, needleReady := app.predictNeedleWorkflowReview(text)
	if needleReady && needlePrediction != nil {
		intent = needlePrediction.Name
		rawIntent = needlePrediction.Name
	} else if strings.TrimSpace(app.llmConfig.URL) != "" && strings.TrimSpace(app.llmConfig.Model) != "" {
		raw, err := app.classifyWorkflowReviewIntentTUI(userID, text)
		if err != nil {
			log.Printf("[TUI-workflow] review intent classification failed: %v", err)
			// Fall back to keyword classification when the LLM is unreachable.
			if kw := v2.ClassifyConfirmIntentKeyword(text); kw != "" && kw != "unrelated" {
				intent = mapConfirmKeywordToReviewIntent(kw)
				rawIntent = intent
			}
		} else {
			rawIntent = raw
			intent = raw
		}
	} else if kw := v2.ClassifyConfirmIntentKeyword(text); kw != "" && kw != "unrelated" {
		// Offline / no-LLM: use the same keyword fallback as confirm classifier.
		intent = mapConfirmKeywordToReviewIntent(kw)
		rawIntent = intent
	}

	// V2 ApplyReviewIntent accepts string intents: "confirm", "skip", "cancel", "switch_task", "supplement", "other"
	hr, err := wf.machine.ApplyReviewIntent(userID, intent, text)
	if err != nil {
		log.Printf("[TUI-workflow] V2 ApplyReviewIntent error: intent=%s err=%v", intent, err)
		app.logNeedleWorkflowReviewEvent(userID, text, rawIntent, needlePrediction, intent, false, err.Error())
		return app.workflowReviewBarrierText(userID)
	}
	app.logNeedleWorkflowReviewEvent(userID, text, rawIntent, needlePrediction, intent, true, "")

	if hr == nil {
		return ""
	}

	// Handle switch_task: cancel workflow and re-route the message.
	if strings.ToLower(strings.TrimSpace(intent)) == "switch_task" && hr.Action == v2.ActionCancelAndExecute {
		app.workflowMu.Lock()
		app.workflowAgentLoop = false
		app.pendingPhasePrompt = ""
		app.workflowMu.Unlock()
		return app.handleWorkflowInterception(text)
	}

	// Reload state and delegate to handleV2HandleResult.
	state := wf.machine.GetActive(userID)
	return app.handleV2HandleResult(userID, hr, state)
}

func (app *TUIApp) classifyWorkflowReviewIntentTUI(userID, text string) (string, error) {
	messages := []interface{}{
		map[string]string{
			"role": "system",
			"content": `You are a user intent classifier for a workflow review step.

The user has just seen a phase deliverable and the workflow must wait for an explicit review decision before continuing.

Classify the user's response into exactly one category. Reply with ONLY the category word:
- "confirm": approve and continue to the next phase.
- "supplement": provide additions, corrections, questions, or requested changes for the current phase deliverable.
- "skip": skip the current phase if the workflow template allows it.
- "cancel": abandon the current v2.
- "switch_task": abandon this workflow and start a clearly different task.
- "other": unrelated request that should not advance or execute tools while review is pending.`,
		},
		map[string]string{
			"role":    "user",
			"content": text,
		},
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := agent.DoSimpleLLMRequest(app.llmConfig, messages, client, 10*time.Second)
	if err != nil || resp == nil {
		return "", err
	}
	return resp.Content, nil
}

func (app *TUIApp) handleWorkflowResponseTUI(userID string, resp *v2.WorkflowResponse) string {
	if resp == nil {
		return ""
	}
	// V2 path: skip form gates (TUI has no side panel).
	if resp.ShowForm && resp.FormSchema != nil {
		wf := app.getWorkflowV2TUI()
		if wf != nil {
			if err := wf.machine.SkipPhaseForm(userID); err != nil {
				log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
				return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
			}
		}
		return buildTUIPhaseInputGuidance(resp.FormSchema)
	}
	if !resp.RunAgentLoop {
		if resp.Complete {
			app.workflowMu.Lock()
			app.workflowAgentLoop = false
			app.pendingPhasePrompt = ""
			app.workflowMu.Unlock()
		}
		return resp.Text
	}
	if resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	return ""
}

func (app *TUIApp) applyWorkflowAutoAdvanceTUI(userID string, resp *v2.WorkflowResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Text != "" {
		log.Printf("[TUI-workflow] auto-advance: %s", truncateTUI(resp.Text, 80))
	}
	if resp.ShowForm && resp.FormSchema != nil {
		wf := app.getWorkflowV2TUI()
		if wf != nil {
			if err := wf.machine.SkipPhaseForm(userID); err != nil {
				log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
				return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
			}
		}
		guidance := buildTUIPhaseInputGuidance(resp.FormSchema)
		if strings.TrimSpace(resp.Text) != "" {
			guidance = strings.TrimSpace(resp.Text) + "\n\n" + guidance
		}
		return strings.TrimSpace(guidance)
	}
	if resp.Complete {
		app.workflowMu.Lock()
		app.workflowAgentLoop = false
		app.pendingPhasePrompt = ""
		app.workflowMu.Unlock()
		return resp.Text
	}
	if resp.RunAgentLoop && resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	return resp.Text
}

func (app *TUIApp) workflowReviewBarrierText(userID string) string {
	// V2 path: read active workflow from V2 StateMachine.
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return ""
	}
	state := wf.machine.GetActive(userID)
	if state == nil {
		return ""
	}
	phaseName := ""
	phase := state.ActivePhase()
	if phase != nil {
		phaseName = phase.Name
	}
	if phaseName == "" {
		phaseName = fmt.Sprintf("phase-%d", state.CurrentPhase)
	}
	return i18n.Tf(i18n.MsgWorkflowAwaitingReview, app.workflowLang(), phaseName)
}

func (app *TUIApp) handleActiveUnderstandingTUI(text string) string {
	userID := "tui-user"
	// V2 path: understanding is on tuiWorkflowV2State.
	wf := app.getWorkflowV2TUI()
	if wf == nil || wf.understanding == nil {
		return ""
	}
	understanding := wf.understanding
	understanding.SetUserLanguage(userID, app.workflowLang())

	reply, ready, cancelled, intent, err := understanding.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding HandleInput error: %v", err)
		if errors.Is(err, v2.ErrIntentUnderstandingContractBreach) {
			return ""
		}
		return i18n.T(i18n.MsgWorkflowUnderstandError, app.workflowLang())
	}
	if cancelled {
		return i18n.T(i18n.MsgWorkflowCancelled, app.workflowLang())
	}
	if ready && intent != nil {
		if intent.Category == v2.WorkflowNone || intent.Category == "" {
			return ""
		}
		// V2 path: use machine.Create to start the v2.
		state, err := wf.machine.Create(userID, string(intent.Category), tuiWorkflowProjectPath(), intent.Summary)
		if err != nil {
			log.Printf("[TUI-workflow] V2 Create error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowStartError, app.workflowLang(), err)
		}
		return app.buildWorkflowStartOverviewV2(userID, state, reply)
	}
	return reply
}

func (app *TUIApp) handleNeedsUnderstandingTUI(text string) string {
	userID := "tui-user"
	if strings.TrimSpace(app.llmConfig.URL) == "" || strings.TrimSpace(app.llmConfig.Model) == "" {
		return ""
	}
	// V2 path: understanding is on tuiWorkflowV2State.
	wf := app.getWorkflowV2TUI()
	if wf == nil || wf.understanding == nil {
		return ""
	}
	understanding := wf.understanding
	understanding.SetUserLanguage(userID, app.workflowLang())

	result, err := understanding.Start(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding Start error: %v", err)
		return ""
	}
	if result == nil || result.Rejected {
		return ""
	}
	if result.Ready && result.Intent != nil {
		if result.Intent.Category == v2.WorkflowNone || result.Intent.Category == "" {
			return ""
		}
		app.workflowMu.Lock()
		app.pendingWorkflowStart = &tuiPendingWorkflowStart{
			OriginalText: text,
			StartReply:   result.Reply,
			Intent:       *result.Intent,
		}
		app.workflowMu.Unlock()
		return strings.TrimSpace(result.Reply)
	}
	return result.Reply
}

func (app *TUIApp) handlePendingWorkflowStartTUI(userID, text string) string {
	app.workflowMu.Lock()
	pending := app.pendingWorkflowStart
	app.workflowMu.Unlock()
	if pending == nil {
		return ""
	}

	trimmed := strings.ToLower(strings.TrimSpace(text))
	switch {
	case isTUIWorkflowStartCancelCommand(trimmed):
		app.workflowMu.Lock()
		if app.pendingWorkflowStart == pending {
			app.pendingWorkflowStart = nil
		}
		app.workflowMu.Unlock()
		return i18n.T(i18n.MsgWorkflowCancelled, app.workflowLang())
	case isTUIWorkflowStartConfirmCommand(trimmed):
		// V2 path: use machine.Create to start the v2.
		wf := app.getWorkflowV2TUI()
		if wf == nil {
			return i18n.Tf(i18n.MsgWorkflowStartError, app.workflowLang(), fmt.Errorf("V2 engine unavailable"))
		}
		state, err := wf.machine.Create(userID, string(pending.Intent.Category), tuiWorkflowProjectPath(), pending.Intent.Summary)
		if err != nil {
			log.Printf("[TUI-workflow] pending V2 Create error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowStartError, app.workflowLang(), err)
		}
		app.workflowMu.Lock()
		if app.pendingWorkflowStart == pending {
			app.pendingWorkflowStart = nil
		}
		app.workflowMu.Unlock()
		return app.buildWorkflowStartOverviewV2(userID, state, pending.StartReply)
	default:
		// Treat substantive text as a correction to the proposed workflow start.
		// It must go back through intent understanding so the pending intent cannot
		// silently absorb changed scope or constraints.
		app.workflowMu.Lock()
		if app.pendingWorkflowStart == pending {
			app.pendingWorkflowStart = nil
		}
		app.workflowMu.Unlock()
		return app.handleNeedsUnderstandingTUI(text)
	}
}

func isTUIWorkflowStartCancelCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "no", "n", "cancel", "stop", "abort", "quit", "exit", "\u53d6\u6d88", "\u505c\u6b62", "\u4e0d\u5f00\u59cb", "\u5148\u4e0d\u5f00\u59cb":
		return true
	default:
		return false
	}
}

func isTUIWorkflowStartConfirmCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "start", "confirm", "yes", "y", "go", "ok", "continue", "\u786e\u8ba4", "\u5f00\u59cb", "\u540c\u610f", "\u7ee7\u7eed":
		return true
	default:
		return false
	}
}

// mapConfirmKeywordToReviewIntent maps StateMachine confirm-keyword labels onto
// ApplyReviewIntent categories used by the review barrier.
func mapConfirmKeywordToReviewIntent(kw string) string {
	switch strings.ToLower(strings.TrimSpace(kw)) {
	case "confirm":
		return "confirm"
	case "modify":
		return "supplement"
	case "cancel":
		return "cancel"
	case "cancel_execute":
		return "switch_task"
	default:
		return "other"
	}
}
func tuiWorkflowProjectPath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cwd)
}

func shouldBypassTUIWorkflowUnderstanding(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "" || utf8.RuneCountInString(trimmed) <= 1
}

// workflowLang returns the language for workflow user-facing messages.
func (app *TUIApp) workflowLang() string {
	return app.appConfig.Language
}

// buildWorkflowStartOverviewV2 builds the start overview using V2 APIs.
func (app *TUIApp) buildWorkflowStartOverviewV2(userID string, state *v2.WorkflowState, prefix string) string {
	lang := app.workflowLang()
	phaseName := ""
	phase := state.ActivePhase()
	if phase != nil {
		phaseName = phase.Name
	}
	overview := i18n.Tf(i18n.MsgWorkflowStarted, lang, state.Type, phaseName)
	if strings.TrimSpace(prefix) != "" {
		overview = strings.TrimSpace(prefix) + "\n\n" + overview
	}

	// TUI has no form panel: wait for numbered text details when the first phase
	// declares an InputSchema, instead of arming the agent loop immediately.
	if phase != nil && phase.InputSchema != nil && phase.FormData == nil {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = ""
		app.workflowAgentLoop = false
		app.workflowMu.Unlock()
		if guidance := buildTUIPhaseInputGuidanceNative(phase.InputSchema); guidance != "" {
			return strings.TrimSpace(overview + "\n\n" + guidance)
		}
	}

	// Build and stash the phase prompt for the first phase.
	phasePrompt := v2.BuildPhasePrompt(state)
	if phasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = phasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}

	return overview
}

func buildTUIPhaseInputGuidance(schema *v2.PhaseInputSchemaSpec) string {
	if schema == nil || len(schema.Fields) == 0 {
		return ""
	}
	var b strings.Builder
	if strings.TrimSpace(schema.Title) != "" {
		b.WriteString(schema.Title)
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(schema.Description) != "" {
		b.WriteString(schema.Description)
		b.WriteString("\n\n")
	}
	b.WriteString("Please provide the following workflow details in a numbered reply. Fields marked * are required.\n")
	for i, field := range schema.Fields {
		marker := " "
		if field.Required {
			marker = "*"
		}
		fmt.Fprintf(&b, "%s%d. %s", marker, i+1, field.Label)
		if len(field.Options) > 0 {
			labels := make([]string, 0, len(field.Options))
			for _, opt := range field.Options {
				labels = append(labels, opt.Label)
			}
			fmt.Fprintf(&b, " (%s)", strings.Join(labels, " / "))
		}
		if strings.TrimSpace(field.Placeholder) != "" {
			fmt.Fprintf(&b, " - %s", field.Placeholder)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func buildTUIPhaseInputGuidanceNative(schema *v2.PhaseInputSchema) string {
	if schema == nil {
		return ""
	}
	return buildTUIPhaseInputGuidance(v2.PhaseInputSchemaToSpec(schema))
}

// parseTUINumberedFormReply maps a numbered chat reply onto schema field names.
// Lines like "1. value" / "2) value" / bare sequential lines are accepted.
func parseTUINumberedFormReply(text string, schema *v2.PhaseInputSchema) map[string]interface{} {
	if schema == nil || len(schema.Fields) == 0 {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip leading "1." / "1)" / "1、" markers.
		trimmed := line
		for i, r := range line {
			if r >= '0' && r <= '9' {
				continue
			}
			rest := strings.TrimSpace(line[i:])
			rest = strings.TrimLeft(rest, ".)、:：-")
			rest = strings.TrimSpace(rest)
			if rest != "" {
				trimmed = rest
			} else if i > 0 {
				// Pure number line — ignore.
				trimmed = ""
			}
			break
		}
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	if len(values) == 0 {
		// Single-paragraph free text: put it into the first required textarea/text field.
		values = []string{text}
	}
	out := make(map[string]interface{}, len(schema.Fields))
	for i, field := range schema.Fields {
		if i >= len(values) {
			break
		}
		if strings.TrimSpace(values[i]) == "" {
			continue
		}
		out[field.Name] = values[i]
	}
	if len(out) == 0 {
		return nil
	}
	// Ensure required fields are present when enough values were supplied.
	for _, field := range schema.Fields {
		if !field.Required {
			continue
		}
		if v, ok := out[field.Name]; !ok || strings.TrimSpace(fmt.Sprint(v)) == "" {
			// Incomplete required set — only accept if user clearly numbered enough lines.
			if len(values) < countRequiredTUIFormFields(schema) {
				return nil
			}
		}
	}
	return out
}

func countRequiredTUIFormFields(schema *v2.PhaseInputSchema) int {
	if schema == nil {
		return 0
	}
	n := 0
	for _, field := range schema.Fields {
		if field.Required {
			n++
		}
	}
	return n
}

// expandWorkflowAttachmentInput turns a pasted/dropped local path into explicit
// workflow context. TUI has no file-picker, so the user's attachment gesture is
// a text path in the chat input.
func (app *TUIApp) expandWorkflowAttachmentInput(userID, text string) string {
	// Check if V2 has an active workflow expecting input.
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return text
	}
	state := wf.machine.GetActive(userID)
	if state == nil {
		return text
	}
	path := firstExistingWorkflowPath(text)
	if path == "" {
		return text
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return fmt.Sprintf("The user provided this local file path as workflow attachment context: %s\n\nPrefer reading this file with read_file or available document tools before analyzing the current workflow phase. Original user input: %s", abs, text)
}

func firstExistingWorkflowPath(text string) string {
	for _, candidate := range workflowPathCandidates(text) {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.Trim(candidate, "\"'")
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func workflowPathCandidates(text string) []string {
	var out []string
	for _, m := range tuiQuotedPathPattern.FindAllStringSubmatch(text, -1) {
		for i := 1; i < len(m); i++ {
			if strings.TrimSpace(m[i]) != "" {
				out = append(out, m[i])
			}
		}
	}
	fields := strings.Fields(text)
	for i := range fields {
		if looksLikePath(fields[i]) {
			out = append(out, strings.TrimRight(fields[i], ",.;:"))
		}
	}
	return out
}

func looksLikePath(s string) bool {
	s = strings.Trim(s, "\"'")
	return strings.Contains(s, `:\`) || strings.Contains(s, `:/`) || strings.HasPrefix(s, `\\`) || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../")
}

// truncateTUI truncates a string for logging.
func truncateTUI(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
