package main

// workflow_v2_init.go initializes the V2 workflow engine for the TUI.
//
// V2 is the sole workflow engine for TUI runtime operations:
// - StateMachine (state transitions + persistence)
// - TemplateRegistry (19 builtin templates)
// - SQLiteStore (persistence) or MemoryStore (fallback)
// - WorkflowRouter (message routing decisions)
//
// The WorkflowEngine field is retained only for test backward
// compatibility. Production code never uses it (initWorkflowEngine removed).

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingagent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// tuiWorkflowV2State holds the V2 workflow engine components for TUI.
type tuiWorkflowV2State struct {
	router        *v2.WorkflowRouter
	machine       *v2.StateMachine
	store         v2.WorkflowStore
	registry      *v2.TemplateRegistry
	understanding *v2.IntentUnderstandingManager // needed for TUI workflow interception
	filter        *v2.QuickFilter                // needed for TUI workflow interception
}

// initWorkflowV2TUI creates and wires the V2 workflow engine for the TUI.
// It uses SQLiteStore for persistence (with MemoryStore fallback), registers
// all 19 builtin templates, and optionally wires an LLM-based confirm classifier.
func (app *TUIApp) initWorkflowV2TUI() *tuiWorkflowV2State {
	// Determine storage backend
	var store v2.WorkflowStore
	dataDir := commands.ResolveDataDir()
	app.initCodingRuntimeStoreTUI(dataDir)
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
	// Reuse the persistence store for understanding sessions (in-memory no-op).
	v1Store := &tuiWorkflowStore{}
	llmCaller := &tuiWorkflowLLMCaller{
		app:    app,
		client: &http.Client{},
	}
	// Use the WorkflowRegistry for understanding.
	v1Registry := v2.NewWorkflowRegistry()
	understanding := v2.NewIntentUnderstandingManager(v1Store, llmCaller, v1Registry)
	understanding.SetLanguage(app.workflowLang())

	// Create QuickFilter for message classification.
	// Use a V2-backed WorkflowChecker shim so the filter detects V2 active workflows.
	checker := &tuiV2WorkflowChecker{machine: machine, understanding: understanding}
	filter := v2.NewQuickFilter(checker)
	workflowState := &tuiWorkflowV2State{
		router:        router,
		machine:       machine,
		store:         store,
		registry:      registry,
		understanding: understanding,
		filter:        filter,
	}
	// The Ledger commits before a host projects the bounded completion into
	// Workflow V2. Repair that narrow crash window during initialization; this
	// reads only durable state and never invokes a model, tool, or executor.
	app.repairCompletedTUIWorkflowCodingProjections(workflowState)

	return workflowState
}

// initCodingRuntimeStoreTUI initializes the shared, host-neutral execution
// ledger. Startup only expires abandoned leases; it never resumes an executor
// or replays prior tool calls.
func (app *TUIApp) initCodingRuntimeStoreTUI(dataDir string) {
	if app == nil || dataDir == "" || app.codingRuntimeStore != nil {
		return
	}
	store, err := codingruntime.NewSQLiteStore(filepath.Join(dataDir, "coding_runtime.db"))
	if err != nil {
		log.Printf("[TUI-coding-runtime] SQLite store init failed: %v", err)
		return
	}
	if expired, err := store.ExpireLeases(time.Now().UTC()); err != nil {
		log.Printf("[TUI-coding-runtime] expire stale leases failed: %v", err)
	} else if len(expired) > 0 {
		log.Printf("[TUI-coding-runtime] marked %d stale attempt(s) interrupted; read-only recovery is required", len(expired))
	}
	if interrupted, err := store.InterruptUnstartedChildren(time.Now().UTC()); err != nil {
		log.Printf("[TUI-coding-runtime] reconcile unstarted child tasks failed: %v", err)
	} else if len(interrupted) > 0 {
		log.Printf("[TUI-coding-runtime] marked %d waiting parent attempt(s) interrupted; child dispatch is not replayed", len(interrupted))
	}
	app.codingRuntimeStore = store
}

// runTUIWorkflowCodingAttempt is the TUI's explicit bridge from a Workflow V2
// implementation phase to the durable coding runtime. Ordinary chat, document
// planning, and artifact phases deliberately stay on their existing direct
// RunLoop path. The TUI remains a serial local host: it does not declare an
// isolated writer or opt into parallel writer admission.
func (app *TUIApp) runTUIWorkflowCodingAttempt(ctx context.Context, cb *tuiCallbacks, state *v2.WorkflowState, phase *v2.Phase, userText string, history []agent.ConversationEntry) (agent.LoopResult, *codingruntime.Task, *codingruntime.Attempt, error) {
	if app == nil || cb == nil || state == nil || phase == nil {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("TUI coding runtime requires app, callbacks, workflow state, and phase")
	}
	if app.codingRuntimeStore == nil {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("TUI coding runtime ledger is unavailable")
	}
	if !tuiWorkflowPhaseUsesCodingRuntime(state, phase) {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("workflow phase %q is not a local coding-runtime execution phase", phase.ID)
	}
	projectPath := strings.TrimSpace(state.ProjectPath)
	if projectPath == "" {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("workflow coding phase requires a project path")
	}
	// A Workflow V2 implementation phase is a write-capable task. Do not let
	// a bare model completion advance it: corelib must observe a changed local
	// Git workspace before it can be recorded as completed.
	policy := codingruntime.PolicySnapshot{ProjectRoot: projectPath, Mode: "local", FinalWorkspaceGateRequired: true}
	digest, err := codingruntime.PolicyDigest(policy)
	if err != nil {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("freeze TUI coding policy: %w", err)
	}
	policy.Digest = digest
	if existing, getErr := app.codingRuntimeStore.GetTask(tuiWorkflowRuntimeTaskID(state.ID, phase.ID)); getErr == nil {
		// The Ledger is the source of truth for executor completion. A process
		// can die after Runner commits the terminal Attempt but before this host
		// projects it into Workflow V2. On restart, repair only that projection:
		// do not invoke a model, tool, or executor a second time.
		if existing.Status == codingruntime.TaskCompleted {
			return tuiCompletedCodingRuntimeProjection(*existing), existing, nil, nil
		}
		if existing.Status == codingruntime.TaskInterrupted {
			return agent.LoopResult{}, existing, nil, codingruntime.ErrRecoveryRequired
		}
		if existing.Status != codingruntime.TaskQueued && existing.Status != codingruntime.TaskWaitingApproval {
			return agent.LoopResult{}, existing, nil, fmt.Errorf("TUI coding runtime task is not ready for a new attempt: %s", existing.Status)
		}
	} else if getErr != codingruntime.ErrNotFound {
		return agent.LoopResult{}, nil, nil, fmt.Errorf("load TUI coding runtime task: %w", getErr)
	}
	var loopResult agent.LoopResult
	executor := codingagent.LoopExecutor{Run: func(runCtx context.Context, request codingruntime.ExecutionRequest) agent.LoopResult {
		// tuiCallbacks already owns the UI's cancellation and tool-policy
		// boundary. RunLoop's ShouldStop observes the same cancel channel; the
		// runner context is checked by LoopExecutor before and after this call.
		cb.bindRuntimeTask(app.codingRuntimeStore, request.Attempt)
		cb.runtimeMu.Lock()
		cb.runtimeChildApp = app
		cb.childExecutions = &codingruntime.ChildExecutionRegistry{}
		cb.runtimeMu.Unlock()
		defer cb.clearRuntimeTask()
		loopResult = codingagent.Run(cb, userText, userText, history, nil, nil)
		return loopResult
	}}
	taskID := tuiWorkflowRuntimeTaskID(state.ID, phase.ID)
	runner := codingruntime.Runner{
		Store:           app.codingRuntimeStore,
		LeaseOwner:      "tui:workflow:" + state.ID,
		LeaseDuration:   15 * time.Minute,
		WorkspaceProber: codingruntime.NewLocalGitWorkspaceProber(projectPath),
	}
	task, attempt, err := runner.Run(ctx, codingruntime.Task{
		TaskID:        taskID,
		WorkflowID:    state.ID,
		PhaseID:       phase.ID,
		OwnerID:       "tui-user",
		ProjectRef:    projectPath,
		Mode:          "local",
		RequestedWork: tuiWorkflowRequestedWork(state, phase, userText),
		PolicyDigest:  digest,
	}, policy, executor)
	return loopResult, task, attempt, err
}

// tuiCompletedCodingRuntimeProjection is intentionally a bounded synthetic
// loop result. It exists solely to let the caller project a durably completed
// attempt into Workflow V2 after a crash. It contains no prior model response,
// command, tool argument, or replay plan, and must never be fed back to an
// executor as continuation context.
func tuiCompletedCodingRuntimeProjection(task codingruntime.Task) agent.LoopResult {
	return agent.LoopResult{Text: fmt.Sprintf("Coding runtime task %s completed durably. Workflow projection was repaired without replaying the executor.", task.TaskID)}
}

// repairCompletedTUIWorkflowCodingProjections restores only a missing
// Workflow V2 projection after a completed Ledger task survived a process
// exit. The deterministic task ID ties the active execution phase to one
// durable task, so no mutable prompt, command, transcript, or executor state
// is needed (or consulted) for repair.
func (app *TUIApp) repairCompletedTUIWorkflowCodingProjections(wf *tuiWorkflowV2State) {
	if app == nil || app.codingRuntimeStore == nil || wf == nil || wf.machine == nil {
		return
	}
	userIDs, err := wf.machine.ListAllStoredUserIDs()
	if err != nil {
		log.Printf("[TUI-coding-runtime] list workflows for completed projection repair failed: %v", err)
		return
	}
	for _, userID := range userIDs {
		state := wf.machine.GetActive(strings.TrimSpace(userID))
		phase := (*v2.Phase)(nil)
		if state != nil {
			phase = state.ActivePhase()
		}
		if !tuiWorkflowPhaseUsesCodingRuntime(state, phase) {
			continue
		}
		taskID := tuiWorkflowRuntimeTaskID(state.ID, phase.ID)
		task, getErr := app.codingRuntimeStore.GetTask(taskID)
		if getErr != nil || task == nil || task.Status != codingruntime.TaskCompleted || task.WorkflowID != state.ID || task.PhaseID != phase.ID || strings.TrimSpace(task.ProjectRef) != strings.TrimSpace(state.ProjectPath) {
			continue
		}
		projection := tuiCompletedCodingRuntimeProjection(*task)
		if err := wf.machine.RecordOutput(strings.TrimSpace(userID), projection.Text); err != nil {
			log.Printf("[TUI-coding-runtime] completed workflow projection still pending: user=%s workflow=%s err=%v", userID, state.ID, err)
			continue
		}
		log.Printf("[TUI-coding-runtime] repaired completed workflow projection without executor replay: user=%s workflow=%s", userID, state.ID)
	}
}

// prepareTUIWorkflowCodingRecovery exposes the core recovery protocol to the
// TUI command layer. It is deliberately read-only: it prepares a plan, probes
// the local Git workspace, and returns an explanation without allocating an
// attempt or calling a model/tool executor.
func (app *TUIApp) prepareTUIWorkflowCodingRecovery(ctx context.Context, taskID string) (*codingruntime.RecoveryPlan, string, error) {
	if app == nil || app.codingRuntimeStore == nil {
		return nil, "", fmt.Errorf("TUI coding runtime ledger is unavailable")
	}
	service := codingruntime.RecoveryService{Store: app.codingRuntimeStore}
	plan, err := service.PrepareRecoveryForTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, "", err
	}
	if strings.ToLower(strings.TrimSpace(plan.Task.Mode)) != "local" || strings.TrimSpace(plan.Task.ProjectRef) == "" {
		return nil, "", fmt.Errorf("TUI recovery supports only a declared local coding workspace")
	}
	plan, err = service.ProbeWorkspace(ctx, plan, codingruntime.NewLocalGitWorkspaceProber(plan.Task.ProjectRef))
	if err != nil {
		return nil, "", err
	}
	summary, err := service.PresentRecoveryDiff(plan)
	if err != nil {
		return nil, "", err
	}
	return plan, summary, nil
}

// confirmTUIWorkflowCodingRecovery records a human confirmation after a fresh
// read-only probe. A confirmation only queues the stable task for a future,
// explicit new attempt; it never invokes the old executor or reuses commands.
func (app *TUIApp) confirmTUIWorkflowCodingRecovery(ctx context.Context, taskID string, confirmed bool) (string, error) {
	plan, summary, err := app.prepareTUIWorkflowCodingRecovery(ctx, taskID)
	if err != nil {
		return "", err
	}
	service := codingruntime.RecoveryService{Store: app.codingRuntimeStore}
	if err := service.ConfirmContinuation(plan, plan.Interrupted.Policy, confirmed); err != nil {
		return "", err
	}
	if !confirmed {
		return summary + "; continuation was declined; no executor was run", nil
	}
	return summary + "; continuation was confirmed; task is queued but no executor was run", nil
}

func tuiWorkflowPhaseUsesCodingRuntime(state *v2.WorkflowState, phase *v2.Phase) bool {
	if state == nil || phase == nil {
		return false
	}
	// A confirmable phase produces reviewable output and must return through the
	// Workflow state machine before any write-capable runtime is admitted. This
	// is intentionally checked here (not only in template definitions): stored
	// workflows can outlive a template change, and a malformed/custom phase must
	// fail closed instead of bypassing the confirmation boundary.
	return state.Type == string(v2.WorkflowCoding) && !phase.NeedsConfirm && phase.Kind == v2.PhaseKindExecution && phase.ExecMode == v2.ExecModeSubAgent
}

func tuiWorkflowRuntimeTaskID(workflowID, phaseID string) string {
	// Workflow IDs and phase IDs originate from the local state machine. Hash
	// them nevertheless so the runtime ID stays bounded and never absorbs user
	// prompt text or filesystem paths.
	sum := sha256.Sum256([]byte(strings.TrimSpace(workflowID) + "\n" + strings.TrimSpace(phaseID)))
	return fmt.Sprintf("tui-coding-%x", sum[:16])
}

func tuiWorkflowRequestedWork(state *v2.WorkflowState, phase *v2.Phase, userText string) string {
	parts := []string{strings.TrimSpace(state.Summary), strings.TrimSpace(phase.Name), strings.TrimSpace(userText)}
	return strings.TrimSpace(strings.Join(parts, "\n"))
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

// getWorkflowV2TUI returns the V2 workflow state when available.
func (app *TUIApp) getWorkflowV2TUI() *tuiWorkflowV2State {
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

// mapToolPolicyToFilterPolicy converts native ToolPolicy to ToolFilterPolicy alias.
func mapToolPolicyToFilterPolicy(policy v2.ToolPolicy) v2.ToolFilterPolicy {
	switch policy {
	case v2.ToolPolicyDocOnly:
		return v2.ToolFilterDocOnly
	case v2.ToolPolicyPlanning:
		return v2.ToolFilterPlanning
	case v2.ToolPolicyOpsControlled:
		return v2.ToolFilterOpsControlled
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
	return mapToolPolicyToFilterPolicy(phase.ToolPolicy)
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
// response to show the user). Returns empty string to fall through to engine.
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

	// TUI is text-only: when a phase is waiting for form input, accept a numbered
	// reply as form submission before the router re-emits ActionShowForm.
	if submitted := app.trySubmitTUIFormFromText(userID, text); submitted {
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

// trySubmitTUIFormFromText maps a numbered chat reply onto the active phase form.
// Returns true when the form was submitted and the agent loop was armed.
func (app *TUIApp) trySubmitTUIFormFromText(userID, text string) bool {
	wf := app.getWorkflowV2TUI()
	if wf == nil || wf.machine == nil {
		return false
	}
	state := wf.machine.GetActive(userID)
	if state == nil {
		return false
	}
	phase := state.ActivePhase()
	if phase == nil || phase.InputSchema == nil || phase.FormData != nil {
		return false
	}
	formData := parseTUINumberedFormReply(text, phase.InputSchema)
	if len(formData) == 0 {
		return false
	}
	if err := wf.machine.SubmitForm(userID, formData); err != nil {
		log.Printf("[TUI-workflow-v2] SubmitForm from text failed: %v", err)
		return false
	}
	state = wf.machine.GetActive(userID)
	if state == nil {
		return false
	}
	phasePrompt := v2.BuildPhasePrompt(state)
	app.workflowMu.Lock()
	app.pendingPhasePrompt = phasePrompt
	app.workflowAgentLoop = phasePrompt != ""
	app.workflowMu.Unlock()
	log.Printf("[TUI-workflow-v2] form submitted via text; agent loop armed=%v", phasePrompt != "")
	return true
}

// handleV2HandleResult translates a V2 HandleResult into TUI behavior.
// Maps V2 actions to the TUI's phase prompt stashing / text response pattern.
func (app *TUIApp) handleV2HandleResult(userID string, hr *v2.HandleResult, state *v2.WorkflowState) string {
	switch hr.Action {
	case v2.ActionRunPhase:
		// Phase needs execution — stash phase prompt for agent loop.
		// If the phase still needs form input, surface text guidance instead.
		if hr.Phase != nil && hr.Phase.InputSchema != nil && hr.Phase.FormData == nil {
			return app.tuiFormGuidanceReply(hr.State, hr.Phase)
		}
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

	case v2.ActionShowForm:
		return app.tuiFormGuidanceReply(hr.State, hr.Phase)

	case v2.ActionConfirmed:
		// User confirmed. If there's a next phase, stash its prompt (or form guidance).
		if hr.State != nil {
			nextPhase := hr.State.ActivePhase()
			if nextPhase != nil && nextPhase.InputSchema != nil && nextPhase.FormData == nil {
				return app.tuiFormGuidanceReply(hr.State, nextPhase)
			}
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
		return "工作流已完成"

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
		return "工作流已取消"

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

// tuiFormGuidanceReply renders numbered text guidance for a phase InputSchema.
// TUI has no side-panel form UI, so form-gated phases collect details in chat.
func (app *TUIApp) tuiFormGuidanceReply(state *v2.WorkflowState, phase *v2.Phase) string {
	app.workflowMu.Lock()
	app.workflowAgentLoop = false
	app.pendingPhasePrompt = ""
	app.workflowMu.Unlock()
	if phase == nil || phase.InputSchema == nil {
		return ""
	}
	return buildTUIPhaseInputGuidanceNative(phase.InputSchema)
}
