package main

// workflow_integration.go adds workflow engine support to the TUI.
//
// This is the foundation for TUI workflow integration. It initializes the
// workflow engine with the same registry and templates as the GUI, and adds
// the interception point in handleChatSend. The intent understanding LLM
// caller is wired to the same LLM config as the agent loop.
//
// Current limitations vs GUI:
// - No doc preview panel (TUI is text-only; documents are shown inline)
// - No SteeringWorkflowDetector (steering rules still work via agent loop)
// - No SubAgent (TUI uses direct mode via agent.RunLoop)
//
// These can be added incrementally without changing the architecture.

import (
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
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

var tuiQuotedPathPattern = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)

type tuiPendingWorkflowStart struct {
	OriginalText string
	StartReply   string
	Intent       workflow.StructuredIntent
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

// TUIWorkflowCallbacks implements workflow.EngineCallbacks for the TUI.
// In TUI mode, events are logged but not emitted to a frontend; the
// workflow state is communicated through inline text in the chat.
//
// registry is the same WorkflowRegistry the engine holds. It lets
// EmitPhaseUpdate derive dashboard phase metadata through the single
// source-of-truth deriver (workflow.PhaseMetadata) the GUI adapter uses,
// keeping the TUI in parity rather than maintaining a separate phase list.
type TUIWorkflowCallbacks struct {
	app      *TUIApp
	registry *workflow.WorkflowRegistry
}

func (c *TUIWorkflowCallbacks) SendTextToUser(userID, text string) error {
	log.Printf("[TUI-workflow] text: %s", truncateTUI(text, 80))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	if state == nil {
		return nil
	}
	// Derive dashboard phase metadata through the same single source-of-truth
	// deriver the GUI adapter uses (workflow.PhaseMetadata), rather than
	// maintaining a separate phase list. TUI is text-only: there is no doc
	// preview board, so the metadata is logged structurally for parity.
	var phases []workflow.PhaseMeta
	if c.registry != nil {
		phases = workflow.PhaseMetadata(c.registry.Match(state.Type))
	}
	log.Printf("[TUI-workflow] phase update: type=%s phase=%s index=%d phases=%s",
		state.Type, state.CurrentPhase, state.PhaseIndex, formatTUIPhaseMeta(phases))
	return nil
}

// formatTUIPhaseMeta renders derived PhaseMeta as a compact structural log line.
// It mirrors what the GUI adapter attaches to workflow:phase_update so the TUI
// stays in parity with the dashboard's phase ordering, labels, and flags.
func formatTUIPhaseMeta(phases []workflow.PhaseMeta) string {
	if len(phases) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(phases))
	for _, p := range phases {
		parts = append(parts, fmt.Sprintf("%d:%s(%s doc=%t skip=%t confirm=%t)",
			p.Index, p.ID, p.Name, p.ExpectsDocument, p.CanSkip, p.NeedsConfirm))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (c *TUIWorkflowCallbacks) EmitDocUpdate(userID, phaseID, content string) error {
	log.Printf("[TUI-workflow] doc update: phase=%s len=%d", phaseID, len(content))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	log.Printf("[TUI-workflow] gate result: phase=%s", phaseID)
	return nil
}

func (c *TUIWorkflowCallbacks) GetLang() string {
	if c.app != nil {
		return c.app.appConfig.Language
	}
	return ""
}

// tuiWorkflowStore implements workflow.PersistenceStore (in-memory no-op).
// TUI sessions are typically short-lived; workflow state doesn't need
// to survive restarts (the conversation history does, via ConversationMemory).
type tuiWorkflowStore struct{}

func (s *tuiWorkflowStore) SaveWorkflowState(state *workflow.WorkflowState) error { return nil }
func (s *tuiWorkflowStore) LoadWorkflowState(userID string) (*workflow.WorkflowState, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteWorkflowState(id string) error                     { return nil }
func (s *tuiWorkflowStore) ListActiveWorkflows() ([]*workflow.WorkflowState, error) { return nil, nil }
func (s *tuiWorkflowStore) SaveUnderstandingSession(session *workflow.UnderstandingSession) error {
	return nil
}
func (s *tuiWorkflowStore) LoadUnderstandingSession(userID string) (*workflow.UnderstandingSession, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteUnderstandingSession(userID string) error { return nil }
func (s *tuiWorkflowStore) CleanupExpired(olderThan time.Duration) error   { return nil }

// initWorkflowEngine creates and wires the workflow engine for the TUI.
// The engine uses the same registry (19 templates) as the GUI. New workflow
// starts go through IntentUnderstandingManager, not template keyword matching.
func (app *TUIApp) initWorkflowEngine() *workflow.WorkflowEngine {
	registry := workflow.NewWorkflowRegistry()
	store := &tuiWorkflowStore{}
	callbacks := &TUIWorkflowCallbacks{app: app, registry: registry}
	llmCaller := &tuiWorkflowLLMCaller{
		app:    app,
		client: &http.Client{},
	}
	understanding := workflow.NewIntentUnderstandingManager(store, llmCaller, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, store, callbacks)

	return engine
}

// getWorkflowEngine returns the workflow engine if available and enabled, or nil.
// This is the TUI's single enforcement point for the "workflow enabled" config
// toggle. All workflow consumers should go through this method.
func (app *TUIApp) getWorkflowEngine() *workflow.WorkflowEngine {
	if !app.appConfig.IsWorkflowEnabled() {
		return nil
	}
	return app.workflowEngine
}

// handleWorkflowInterception checks if the message should be handled by the
// workflow engine. Returns a non-empty string if the message was fully handled
// (the string is the response to show the user). Returns empty string if the
// message should proceed to the normal agent loop.
func (app *TUIApp) handleWorkflowInterception(text string) string {
	engine := app.getWorkflowEngine()
	if engine == nil {
		return ""
	}

	userID := "tui-user"
	if pendingResp := app.handlePendingWorkflowStartTUI(userID, text); pendingResp != "" {
		return pendingResp
	}

	filter := engine.GetFilter()
	if filter == nil {
		return ""
	}

	classification := filter.Classify(userID, text)
	log.Printf("[TUI-workflow] classify: %v text=%q", classification, truncateTUI(text, 60))

	switch classification {
	case workflow.FilterActiveWorkflow:
		return app.handleActiveWorkflowTUI(text)

	case workflow.FilterActiveUnderstanding:
		return app.handleActiveUnderstandingTUI(text)

	case workflow.FilterNeedsUnderstanding:
		if shouldBypassTUIWorkflowUnderstanding(text) {
			return ""
		}
		return app.handleNeedsUnderstandingTUI(text)

	case workflow.FilterSimpleDirective:
		return "" // pass through to normal agent loop
	}

	return ""
}

func (app *TUIApp) handleActiveWorkflowTUI(text string) string {
	userID := "tui-user"
	text = app.expandWorkflowAttachmentInput(userID, text)
	resp, err := app.workflowEngine.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] HandleInput error: %v", err)
		return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
	}
	if resp == nil {
		return ""
	}

	if resp.PendingReview || resp.PendingConfirm {
		return app.handleWorkflowReviewTUI(userID, text)
	}

	if resp.ShowForm && resp.FormSchema != nil {
		if err := app.workflowEngine.SkipPhaseForm(userID); err != nil {
			log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
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

	// RunAgentLoop=true: stash the phase prompt for the agent loop.
	if resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	if resp.Advance && resp.Text != "" {
		// Phase advanced: return the transition text. The next agent loop
		// call will pick up the stashed phase prompt.
		return resp.Text
	}
	return "" // fall through to agent loop with stashed phase prompt
}

func (app *TUIApp) handleWorkflowReviewTUI(userID, text string) string {
	intent := workflow.ReviewIntentOther
	rawIntent := ""
	needlePrediction, needleReady := app.predictNeedleWorkflowReview(text)
	if needleReady && needlePrediction != nil {
		intent = workflow.ParseReviewIntent(needlePrediction.Name)
		rawIntent = needlePrediction.Name
	} else if strings.TrimSpace(app.llmConfig.URL) != "" && strings.TrimSpace(app.llmConfig.Model) != "" {
		raw, err := app.classifyWorkflowReviewIntentTUI(userID, text)
		if err != nil {
			log.Printf("[TUI-workflow] review intent classification failed: %v", err)
		} else {
			rawIntent = raw
			intent = workflow.ParseReviewIntent(raw)
		}
	}

	resp, err := app.workflowEngine.ApplyReviewIntent(userID, intent, text)
	if err != nil {
		log.Printf("[TUI-workflow] ApplyReviewIntent error: intent=%s err=%v", intent, err)
		app.logNeedleWorkflowReviewEvent(userID, text, rawIntent, needlePrediction, string(intent), false, err.Error())
		return app.workflowReviewBarrierText(userID)
	}
	app.logNeedleWorkflowReviewEvent(userID, text, rawIntent, needlePrediction, string(intent), true, "")
	if intent == workflow.ReviewIntentSwitchTask {
		return app.handleWorkflowInterception(text)
	}
	return app.handleWorkflowResponseTUI(userID, resp)
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
- "cancel": abandon the current workflow.
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

func (app *TUIApp) handleWorkflowResponseTUI(userID string, resp *workflow.WorkflowResponse) string {
	if resp == nil {
		return ""
	}
	if resp.ShowForm && resp.FormSchema != nil {
		if err := app.workflowEngine.SkipPhaseForm(userID); err != nil {
			log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
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

func (app *TUIApp) applyWorkflowAutoAdvanceTUI(userID string, resp *workflow.WorkflowResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Text != "" {
		log.Printf("[TUI-workflow] auto-advance: %s", truncateTUI(resp.Text, 80))
	}
	if resp.ShowForm && resp.FormSchema != nil {
		if err := app.workflowEngine.SkipPhaseForm(userID); err != nil {
			log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
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
	ws := app.workflowEngine.GetActiveWorkflow(userID)
	if ws == nil {
		return ""
	}
	phaseName := ws.CurrentPhase
	if tmpl := app.workflowEngine.GetRegistry().Match(ws.Type); tmpl != nil && ws.PhaseIndex < len(tmpl.Phases) {
		phaseName = tmpl.Phases[ws.PhaseIndex].Name
	}
	return i18n.Tf(i18n.MsgWorkflowAwaitingReview, app.workflowLang(), phaseName)
}

func (app *TUIApp) handleActiveUnderstandingTUI(text string) string {
	userID := "tui-user"
	understanding := app.workflowEngine.GetUnderstanding()
	if understanding == nil {
		return ""
	}

	reply, ready, cancelled, intent, err := understanding.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding HandleInput error: %v", err)
		// The understanding LLM failed: clean up the broken session and fall
		// through to the normal agent loop (return empty string).
		understanding.CancelSession(userID)
		return ""
	}
	if cancelled {
		return i18n.T(i18n.MsgWorkflowCancelled, app.workflowLang())
	}
	if ready && intent != nil {
		if intent.Category == workflow.WorkflowNone || intent.Category == "" {
			return ""
		}
		state, err := app.workflowEngine.StartWorkflowWithOptions(userID, *intent, workflow.WorkflowStartOptions{ProjectPath: tuiWorkflowProjectPath()})
		if err != nil {
			log.Printf("[TUI-workflow] StartWorkflow error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowStartError, app.workflowLang(), err)
		}
		return app.buildWorkflowStartOverview(userID, state, reply)
	}
	return reply
}

func (app *TUIApp) handleNeedsUnderstandingTUI(text string) string {
	userID := "tui-user"
	if strings.TrimSpace(app.llmConfig.URL) == "" || strings.TrimSpace(app.llmConfig.Model) == "" {
		return ""
	}
	understanding := app.workflowEngine.GetUnderstanding()
	if understanding == nil {
		return ""
	}

	result, err := understanding.Start(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] understanding Start error: %v", err)
		return ""
	}
	if result == nil || result.Rejected {
		return ""
	}
	if result.Ready && result.Intent != nil {
		if result.Intent.Category == workflow.WorkflowNone || result.Intent.Category == "" {
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
		state, err := app.workflowEngine.StartWorkflowWithOptions(userID, pending.Intent, workflow.WorkflowStartOptions{ProjectPath: tuiWorkflowProjectPath()})
		if err != nil {
			log.Printf("[TUI-workflow] pending StartWorkflow error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowStartError, app.workflowLang(), err)
		}
		app.workflowMu.Lock()
		if app.pendingWorkflowStart == pending {
			app.pendingWorkflowStart = nil
		}
		app.workflowMu.Unlock()
		return app.buildWorkflowStartOverview(userID, state, pending.StartReply)
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

func (app *TUIApp) buildWorkflowStartOverview(userID string, state *workflow.WorkflowState, prefix string) string {
	lang := app.workflowLang()
	overview := i18n.Tf(i18n.MsgWorkflowStarted, lang, state.Type, state.CurrentPhase)
	if req := app.workflowEngine.GetInputRequirement(userID); req != nil {
		overview += i18n.Tf(i18n.MsgWorkflowInputRequired, lang, req.Description)
		if len(req.FileTypes) > 0 {
			overview += i18n.Tf(i18n.MsgWorkflowInputFormats, lang, strings.Join(req.FileTypes, ", "))
		}
		if req.AcceptText {
			overview += i18n.T(i18n.MsgWorkflowInputPasteHint, lang)
		}
		if strings.TrimSpace(prefix) != "" {
			overview = strings.TrimSpace(prefix) + "\n\n" + overview
		}
		return overview
	}
	if strings.TrimSpace(prefix) != "" {
		overview = strings.TrimSpace(prefix) + "\n\n" + overview
	}

	// TUI is text-only. If the first phase declares structured input, ask for it
	// as numbered text and wait for the next user message before starting the
	// agent loop. This preserves the form-first contract without a side panel.
	tmpl := app.workflowEngine.GetRegistry().Match(state.Type)
	if tmpl != nil && len(tmpl.Phases) > 0 && tmpl.Phases[0].InputSchema != nil {
		if err := app.workflowEngine.SkipPhaseForm(userID); err != nil {
			log.Printf("[TUI-workflow] SkipPhaseForm error: %v", err)
			return i18n.Tf(i18n.MsgWorkflowHandleError, app.workflowLang(), err)
		}
		if guidance := buildTUIPhaseInputGuidance(tmpl.Phases[0].InputSchema); guidance != "" {
			return strings.TrimSpace(overview + "\n\n" + guidance)
		}
	}

	// The first phase needs the agent loop to generate content. Stash the phase prompt.
	if tmpl != nil && len(tmpl.Phases) > 0 {
		phasePrompt := workflow.BuildPhaseSystemPrompt(state, &tmpl.Phases[0], app.workflowEngine.GetRegistry())
		if phasePrompt != "" {
			app.workflowMu.Lock()
			app.pendingPhasePrompt = phasePrompt
			app.workflowAgentLoop = true
			app.workflowMu.Unlock()
		}
	}

	return overview
}

func buildTUIPhaseInputGuidance(schema *workflow.PhaseInputSchema) string {
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

// expandWorkflowAttachmentInput turns a pasted/dropped local path into explicit
// workflow context. TUI has no file-picker, so the user's attachment gesture is
// a text path in the chat input.
func (app *TUIApp) expandWorkflowAttachmentInput(userID, text string) string {
	if app == nil || app.workflowEngine == nil || app.workflowEngine.GetInputRequirement(userID) == nil {
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
