package main

// workflow_integration.go adds workflow engine support to the TUI.
//
// This is the foundation for TUI workflow integration. It initializes the
// workflow engine with the same registry and templates as the GUI, and adds
// the interception point in handleChatSend. The intent understanding LLM
// caller is wired to the same LLM config as the agent loop.
//
// Current limitations vs GUI:
// - No doc preview panel (TUI is text-only — documents shown inline)
// - No lightweight LLM confirm classifier (uses keyword matching only)
// - No SteeringWorkflowDetector (steering rules still work via agent loop)
// - No SubAgent (TUI uses direct mode via agent.RunLoop)
//
// These can be added incrementally without changing the architecture.

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// TUIWorkflowCallbacks implements workflow.EngineCallbacks for the TUI.
// In TUI mode, events are logged but not emitted to a frontend — the
// workflow state is communicated through inline text in the chat.
type TUIWorkflowCallbacks struct {
	app *TUIApp
}

func (c *TUIWorkflowCallbacks) SendTextToUser(userID, text string) error {
	log.Printf("[TUI-workflow] text: %s", truncateTUI(text, 80))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	log.Printf("[TUI-workflow] phase update: phase=%s index=%d", state.CurrentPhase, state.PhaseIndex)
	return nil
}

func (c *TUIWorkflowCallbacks) EmitDocUpdate(userID, phaseID, content string) error {
	log.Printf("[TUI-workflow] doc update: phase=%s len=%d", phaseID, len(content))
	return nil
}

func (c *TUIWorkflowCallbacks) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	log.Printf("[TUI-workflow] gate result: phase=%s", phaseID)
	return nil
}

// tuiWorkflowStore implements workflow.PersistenceStore (in-memory no-op).
// TUI sessions are typically short-lived; workflow state doesn't need
// to survive restarts (the conversation history does, via ConversationMemory).
type tuiWorkflowStore struct{}

func (s *tuiWorkflowStore) SaveWorkflowState(state *workflow.WorkflowState) error   { return nil }
func (s *tuiWorkflowStore) LoadWorkflowState(userID string) (*workflow.WorkflowState, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteWorkflowState(id string) error                     { return nil }
func (s *tuiWorkflowStore) ListActiveWorkflows() ([]*workflow.WorkflowState, error)  { return nil, nil }
func (s *tuiWorkflowStore) SaveUnderstandingSession(session *workflow.UnderstandingSession) error {
	return nil
}
func (s *tuiWorkflowStore) LoadUnderstandingSession(userID string) (*workflow.UnderstandingSession, error) {
	return nil, nil
}
func (s *tuiWorkflowStore) DeleteUnderstandingSession(userID string) error           { return nil }
func (s *tuiWorkflowStore) CleanupExpired(olderThan time.Duration) error             { return nil }

// initWorkflowEngine creates and wires the workflow engine for the TUI.
// The engine uses the same registry (19 templates) as the GUI.
// Intent understanding is initialized without an LLM caller — the TUI
// relies on QuickFilter's keyword/BM25 layers for workflow detection.
// When those layers can't determine the workflow type, the message falls
// through to the normal agent loop (no multi-round LLM clarification).
func (app *TUIApp) initWorkflowEngine() *workflow.WorkflowEngine {
	registry := workflow.NewWorkflowRegistry()
	store := &tuiWorkflowStore{}
	callbacks := &TUIWorkflowCallbacks{app: app}

	// Pass nil for IntentUnderstandingManager — the TUI doesn't have a
	// lightweight LLM caller for multi-round intent clarification.
	// QuickFilter's Layer 1 (keywords) and Layer 1.5 (registry matching)
	// handle most cases. Layer 2 (BM25) provides semantic fallback.
	// Messages that need Layer 3 (LLM) fall through to the normal agent loop.
	engine := workflow.NewWorkflowEngine(registry, nil, store, callbacks)

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
	filter := engine.GetFilter()
	if filter == nil {
		return ""
	}

	classification := filter.Classify(userID, text)
	log.Printf("[TUI-workflow] classify: %v text=%q", classification, truncateTUI(text, 60))

	switch classification {
	case workflow.FilterActiveWorkflow:
		return app.handleActiveWorkflowTUI(text)

	case workflow.FilterNeedsUnderstanding:
		// Without an LLM caller, we can't do multi-round intent clarification.
		// Try to match a workflow template directly from keywords/BM25.
		return app.tryDirectWorkflowStart(text)

	case workflow.FilterSmallTalk, workflow.FilterSimpleDirective:
		return "" // pass through to normal agent loop
	}

	return ""
}

func (app *TUIApp) handleActiveWorkflowTUI(text string) string {
	userID := "tui-user"
	resp, err := app.workflowEngine.HandleInput(userID, text)
	if err != nil {
		log.Printf("[TUI-workflow] HandleInput error: %v", err)
		return fmt.Sprintf("工作流处理出错: %v", err)
	}
	if resp == nil {
		return ""
	}

	if resp.PendingConfirm {
		// TUI uses keyword matching for confirm detection.
		// The engine's isOnlyConfirmWord already handles this correctly.
		// If we reach PendingConfirm, the engine didn't match a pure confirm
		// word — treat as modify/other and fall through to agent loop.
		return ""
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

	// RunAgentLoop=true — stash the phase prompt for the agent loop.
	if resp.PhasePrompt != "" {
		app.workflowMu.Lock()
		app.pendingPhasePrompt = resp.PhasePrompt
		app.workflowAgentLoop = true
		app.workflowMu.Unlock()
	}
	if resp.Advance && resp.Text != "" {
		// Phase advanced — return the transition text. The next agent loop
		// call will pick up the stashed phase prompt.
		return resp.Text
	}
	return "" // fall through to agent loop with stashed phase prompt
}

// tryDirectWorkflowStart attempts to start a workflow directly from
// keyword/BM25 matching, without multi-round LLM clarification.
func (app *TUIApp) tryDirectWorkflowStart(text string) string {
	userID := "tui-user"
	registry := app.workflowEngine.GetRegistry()
	if registry == nil {
		return ""
	}

	// Try strong keyword match first.
	if wfType, matched := registry.MatchTemplateByStrongKeyword(text); matched {
		return app.startWorkflowDirect(userID, text, wfType)
	}

	// Try BM25 semantic match.
	if wfType := registry.BestTemplateType(text); wfType != "" {
		return app.startWorkflowDirect(userID, text, wfType)
	}

	// Can't determine workflow type — fall through to normal agent loop.
	return ""
}

func (app *TUIApp) startWorkflowDirect(userID, text string, wfType workflow.WorkflowType) string {
	intent := workflow.StructuredIntent{
		Category:   wfType,
		Summary:    text,
		Goals:      []string{text},
		Confidence: 0.8,
	}
	state, err := app.workflowEngine.StartWorkflow(userID, intent)
	if err != nil {
		log.Printf("[TUI-workflow] StartWorkflow error: %v", err)
		return ""
	}

	overview := fmt.Sprintf("🚀 工作流已启动：%s\n📋 当前阶段：%s", state.Type, state.CurrentPhase)
	if req := app.workflowEngine.GetInputRequirement(userID); req != nil {
		overview += "\n\n📎 " + req.Description
	}

	// The first phase needs the agent loop to generate content.
	// Stash the phase prompt.
	tmpl := app.workflowEngine.GetRegistry().Match(wfType)
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

// truncateTUI truncates a string for logging.
func truncateTUI(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
