package main

// workflow_v2_init.go initializes the V2 workflow engine for the TUI.
//
// V2 is the sole workflow engine for TUI runtime operations:
// - StateMachine (state transitions + persistence)
// - TemplateRegistry (19 builtin templates)
// - SQLiteStore (persistence) or MemoryStore (fallback)
// - WorkflowRouter (message routing decisions)
//
// The deprecated V1 WorkflowEngine field is retained only for test backward
// compatibility. Production code never uses it (initWorkflowEngine removed).

import (
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// tuiWorkflowV2State holds the V2 workflow engine components for TUI.
type tuiWorkflowV2State struct {
	router        *v2.WorkflowRouter
	machine       *v2.StateMachine
	store         v2.WorkflowStore
	registry      *v2.TemplateRegistry
	understanding *v2.IntentUnderstandingManager // shared with V1, needed for TUI workflow interception
	filter        *v2.QuickFilter               // shared with V1, needed for TUI workflow interception
}

// initWorkflowV2TUI creates and wires the V2 workflow engine for the TUI.
// It uses SQLiteStore for persistence (with MemoryStore fallback), registers
// all 19 builtin templates, and optionally wires an LLM-based confirm classifier.
func (app *TUIApp) initWorkflowV2TUI() *tuiWorkflowV2State {
	// Determine storage backend
	var store v2.WorkflowStore
	dataDir := commands.ResolveDataDir()
	if dataDir != "" {
		dbPath := filepath.Join(dataDir, "workflow_v2.db")
		sqlStore, err := v2.NewSQLiteStore(dbPath)
		if err != nil {
			log.Printf("[TUI-workflow-v2] SQLite store init failed: %v, using memory store", err)
			store = v2.NewMemoryStore()
		} else {
			log.Printf("[TUI-workflow-v2] initialized SQLite store: %s", dbPath)
			store = sqlStore
		}
	} else {
		store = v2.NewMemoryStore()
	}

	// Create registry and register all builtin templates
	registry := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(registry)

	// Create state machine
	machine := v2.NewStateMachine(store, registry)

	// Wire LLM-based confirm classifier if LLM is configured
	machine.SetConfirmClassifier(app.tuiWorkflowV2ConfirmClassifier)

	// Create router (no LLM confirm function — keyword matching is sufficient for TUI)
	router := v2.NewWorkflowRouter(machine, registry, nil)

	log.Printf("[TUI-workflow-v2] engine ready: router=%v machine=%v store=%T",
		router != nil, machine != nil, store)

	// Create IntentUnderstandingManager for V2 workflow routing.
	// Reuse the V1 persistence store for understanding sessions (in-memory no-op).
	v1Store := &tuiWorkflowStore{}
	llmCaller := &tuiWorkflowLLMCaller{
		app:    app,
		client: &http.Client{},
	}
	// Use the V1 registry for understanding (it expects *v2.WorkflowRegistry).
	v1Registry := v2.NewWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v1Store, llmCaller, v1Registry)
	understanding.SetLanguage(app.workflowLang())

	// Create QuickFilter for message classification.
	// Use a V2-backed WorkflowChecker shim so the filter detects V2 active workflows.
	checker := &tuiV2WorkflowChecker{machine: machine, understanding: understanding}
	filter := v2.NewQuickFilter(checker)

	return &tuiWorkflowV2State{
		router:        router,
		machine:       machine,
		store:         store,
		registry:      registry,
		understanding: understanding,
		filter:        filter,
	}
}

// tuiWorkflowV2ConfirmClassifier uses LLM to classify user intent during
// workflow confirmation. Falls back to keyword matching when LLM is unavailable.
// Uses a goroutine + channel to avoid blocking the Bubble Tea main goroutine
// for the full LLM response time (up to 5s timeout, keyword fallback on timeout).
func (app *TUIApp) tuiWorkflowV2ConfirmClassifier(phaseContext, userText string) string {
	if app.llmConfig.URL == "" || app.llmConfig.Model == "" {
		return v2.ClassifyConfirmIntentKeyword(userText)
	}

	messages := []interface{}{
		map[string]string{
			"role":    "system",
			"content": v2.ConfirmClassifierSystemPrompt,
		},
		map[string]string{
			"role":    "user",
			"content": v2.BuildConfirmClassifierUserPrompt(phaseContext, userText),
		},
	}

	type llmResult struct {
		content string
		err     error
	}
	ch := make(chan llmResult, 1)
	go func() {
		client := &http.Client{Timeout: 8 * time.Second}
		resp, err := agent.DoSimpleLLMRequest(app.llmConfig, messages, client, 5*time.Second)
		if err != nil || resp == nil {
			ch <- llmResult{err: err}
		} else {
			ch <- llmResult{content: resp.Content}
		}
	}()

	// Wait up to 5s for LLM. If it takes longer, fall back to keywords
	// to avoid blocking the TUI event loop.
	select {
	case result := <-ch:
		if result.err != nil {
			log.Printf("[TUI-workflow-v2] confirm classifier LLM failed: %v, falling back to keywords", result.err)
			return v2.ClassifyConfirmIntentKeyword(userText)
		}
		intent := v2.ParseConfirmClassifierResponse(result.content)
		if intent == "" {
			return v2.ClassifyConfirmIntentKeyword(userText)
		}
		log.Printf("[TUI-workflow-v2] confirm classifier: text=%q → %q", userText, intent)
		return intent
	case <-time.After(5 * time.Second):
		log.Printf("[TUI-workflow-v2] confirm classifier LLM timeout (5s), falling back to keywords")
		return v2.ClassifyConfirmIntentKeyword(userText)
	}
}

// getWorkflowV2TUI returns the V2 workflow state if available and enabled.
func (app *TUIApp) getWorkflowV2TUI() *tuiWorkflowV2State {
	if !app.appConfig.IsWorkflowEnabled() {
		return nil
	}
	return app.workflowV2
}

// tuiV2WorkflowChecker implements v2.WorkflowChecker backed by V2 StateMachine.
// This lets QuickFilter detect V2 active workflows for routing decisions.
type tuiV2WorkflowChecker struct {
	machine       *v2.StateMachine
	understanding *v2.IntentUnderstandingManager
}

func (c *tuiV2WorkflowChecker) HasActiveWorkflow(userID string) bool {
	if c.machine == nil {
		return false
	}
	return c.machine.GetActive(userID) != nil
}

func (c *tuiV2WorkflowChecker) HasActiveUnderstanding(userID string) bool {
	if c.understanding == nil {
		return false
	}
	return c.understanding.HasActiveSession(userID)
}


// mapV2ToolPolicyToV1 converts V2 ToolPolicy to V1 ToolFilterPolicy.
func mapV2ToolPolicyToV1(policy v2.ToolPolicy) v2.ToolFilterPolicy {
	switch policy {
	case v2.ToolPolicyDocOnly:
		return v2.ToolFilterDocOnly
	case v2.ToolPolicyFull:
		return v2.ToolFilterFull
	default:
		return v2.ToolFilterNone
	}
}

// currentWorkflowToolFilterV2 returns the tool filter policy from V2 state.
// Falls back to ToolFilterNone if no active v2.
func (app *TUIApp) currentWorkflowToolFilterV2() v2.ToolFilterPolicy {
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return v2.ToolFilterNone
	}
	state := wf.machine.GetActive("tui-user")
	if state == nil {
		return v2.ToolFilterNone
	}
	phase := state.ActivePhase()
	if phase == nil {
		return v2.ToolFilterNone
	}
	return mapV2ToolPolicyToV1(phase.ToolPolicy)
}

// isWorkflowV2Active returns true if there's an active V2 v2.
func (app *TUIApp) isWorkflowV2Active() bool {
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return false
	}
	return wf.machine.GetActive("tui-user") != nil
}


// routeWithV2Router uses the V2 Router to handle messages for active V2 workflows.
// Returns a non-empty string if the message was handled by V2 (the string is the
// response to show the user). Returns empty string to fall through to V1 engine.
//
// This handles the case where a V2 workflow was created (via machine.Create) and
// the user sends confirmation/modification/cancellation messages. The V2 Router
// delegates to StateMachine.HandleInput which uses the LLM confirm classifier.
func (app *TUIApp) routeWithV2Router(userID, text string) string {
	wf := app.getWorkflowV2TUI()
	if wf == nil {
		return ""
	}

	// Only route to V2 if there's an active V2 workflow for this user.
	state := wf.machine.GetActive(userID)
	if state == nil {
		return ""
	}

	// Use the V2 Router to get a routing decision.
	result := wf.router.Route(userID, text, nil)
	if result == nil || result.Target == v2.RouteToAgentLoop {
		return "" // pass through — V2 Router says this isn't for the workflow
	}

	// Handle the result from V2 StateMachine.
	if result.HandleResult != nil {
		return app.handleV2HandleResult(userID, result.HandleResult, state)
	}

	// New workflow route (shouldn't happen if we already have active state, but handle gracefully)
	return ""
}

// handleV2HandleResult translates a V2 HandleResult into TUI behavior.
// Maps V2 actions to the TUI's phase prompt stashing / text response pattern.
func (app *TUIApp) handleV2HandleResult(userID string, hr *v2.HandleResult, state *v2.WorkflowState) string {
	switch hr.Action {
	case v2.ActionRunPhase:
		// Phase needs execution — stash phase prompt for agent loop.
		if hr.Phase != nil && hr.State != nil {
			phasePrompt := v2.BuildPhasePrompt(hr.State)
			if hr.ModifyHint != "" {
				phasePrompt += "\n\n用户修改意见：" + hr.ModifyHint
			}
			app.workflowMu.Lock()
			app.pendingPhasePrompt = phasePrompt
			app.workflowAgentLoop = true
			app.workflowMu.Unlock()
			log.Printf("[TUI-workflow-v2] ActionRunPhase: phase=%s, stashed prompt", hr.Phase.ID)
		}
		return "" // fall through to agent loop with stashed phase prompt

	case v2.ActionConfirmed:
		// User confirmed. If there's a next phase, stash its prompt.
		if hr.State != nil {
			nextPhase := hr.State.ActivePhase()
			if nextPhase != nil {
				phasePrompt := v2.BuildPhasePrompt(hr.State)
				app.workflowMu.Lock()
				app.pendingPhasePrompt = phasePrompt
				app.workflowAgentLoop = true
				app.workflowMu.Unlock()
				log.Printf("[TUI-workflow-v2] ActionConfirmed: advanced to phase=%s", nextPhase.ID)
				return "" // fall through to agent loop
			}
		}
		// Workflow completed.
		app.workflowMu.Lock()
		app.workflowAgentLoop = false
		app.pendingPhasePrompt = ""
		app.workflowMu.Unlock()
		log.Printf("[TUI-workflow-v2] ActionConfirmed: workflow completed")
		return "✅ 工作流已完成"

	case v2.ActionModify:
		// User wants to modify — re-run phase with hint.
		if hr.Phase != nil && hr.State != nil {
			phasePrompt := v2.BuildPhasePrompt(hr.State)
			if hr.ModifyHint != "" {
				phasePrompt += "\n\n用户修改意见：" + hr.ModifyHint
			}
			app.workflowMu.Lock()
			app.pendingPhasePrompt = phasePrompt
			app.workflowAgentLoop = true
			app.workflowMu.Unlock()
			log.Printf("[TUI-workflow-v2] ActionModify: phase=%s hint=%s", hr.Phase.ID, truncateTUI(hr.ModifyHint, 40))
		}
		return "" // fall through to agent loop with modified prompt

	case v2.ActionCancelled:
		app.workflowMu.Lock()
		app.workflowAgentLoop = false
		app.pendingPhasePrompt = ""
		app.workflowMu.Unlock()
		log.Printf("[TUI-workflow-v2] ActionCancelled")
		return "❌ 工作流已取消"

	case v2.ActionPassThrough:
		return "" // not workflow-related, pass to agent loop

	case v2.ActionCancelAndExecute:
		app.workflowMu.Lock()
		app.workflowAgentLoop = false
		app.pendingPhasePrompt = ""
		app.workflowMu.Unlock()
		log.Printf("[TUI-workflow-v2] ActionCancelAndExecute")
		return "" // fall through to agent loop for direct execution
	}

	return ""
}
