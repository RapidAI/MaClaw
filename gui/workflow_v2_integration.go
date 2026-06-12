// Package main contains the V2 workflow engine integration with the GUI layer.
package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
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
	log.Printf("[workflow-v2] engine ready: router=%v machine=%v store=%v", st.router != nil, st.machine != nil, st.store != nil)

	// On startup, dismiss any stale frontend workflow board state.
	// The frontend persists board state to ai_assistant_ui_state.json and restores it
	// on reload, but the backend workflow may have been cancelled or completed since then.
	// We emit a dismiss event so the frontend starts clean; if there's an active workflow,
	// the next user message will re-emit the correct phase_update via routeWithWorkflowV2.
	go func() {
		time.Sleep(500 * time.Millisecond) // Wait for Wails runtime to be ready
		emitWorkflowV2Event(a, "workflow:suggest_maximize_dismiss", nil)
		emitWorkflowV2Event(a, "workflow:phase_update", nil)
		log.Printf("[workflow-v2] startup: emitted board reset to clear stale frontend state")
	}()

	return st
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
	return &workflowV2State{
		router:   router,
		machine:  machine,
		store:    store,
		registry: registry,
	}
}

// routeWithWorkflowV2 is the V2 replacement for routeWorkflowIMMessage.
// Returns a workflowIMRouteResult compatible with the existing entry context.
func (h *IMMessageHandler) routeWithWorkflowV2(msg IMUserMessage, trimmed string) workflowIMRouteResult {
	wf := h.getWorkflowV2()
	if wf == nil {
		log.Printf("[workflow-v2] routeWithWorkflowV2: wf is nil, app=%v app.workflowV2=%v", h.app != nil, h.app != nil && h.app.workflowV2 != nil)
		return workflowIMRouteResult{}
	}

	// Lazily set the complexity function using available LLM (only once)
	if wf.router != nil && wf.router.GetComplexityFunc() == nil {
		wf.router.SetComplexityFunc(h.buildComplexityFunc())
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

	// Use UIC embedding-only classification (<100ms) as a semantic hint for the router.
	// This handles cases where BM25 text matching fails (user's message has paths,
	// framework names, etc. that dilute BM25 relevance) but the intent is clearly coding.
	var semanticHint string
	if uic := h.getUnifiedClassifier(); uic != nil {
		embResult := uic.ClassifyEmbeddingOnly(intent.MessageContext{Text: trimmed, UserID: msg.UserID})
		if embResult.Confidence >= 0.70 {
			semanticHint = string(embResult.Primary)
		}
	}

	result := wf.router.RouteWithHint(msg.UserID, trimmed, attachments, semanticHint)

	switch result.Target {
	case v2.RouteToAgentLoop:
		return workflowIMRouteResult{}

	case v2.RouteToDirectCoding:
		// Simple/medium task: skip SDD, go directly to SubAgent with user request as the task.
		log.Printf("[workflow-v2] RouteToDirectCoding: user=%s, direct SubAgent execution", msg.UserID)
		projectPath := result.ProjectPath
		if projectPath == "" {
			if h.app != nil {
				projectPath = strings.TrimSpace(h.app.GetCurrentProjectPath())
			}
		}
		if projectPath != "" {
			if cleaned := v2.TruncateToValidPathChars(projectPath); cleaned != "" {
				projectPath = cleaned
			}
		}
		if projectPath == "" {
			projectPath = "."
		}
		// Cancel any existing workflow
		if wf := h.getWorkflowV2(); wf != nil && wf.machine.GetActive(msg.UserID) != nil {
			wf.machine.Cancel(msg.UserID)
			emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		}
		h.pendingV2SubAgentExecution.Store(msg.UserID, true)
		h.pendingDirectCodingProjectPath.Store(msg.UserID, projectPath)
		h.workflowAgentLoopMarker.Store(msg.UserID, true)
		h.workflowOriginalRequest.Store(msg.UserID, msg.Text)
		return workflowIMRouteResult{
			WorkflowAgentLoop: true,
			WorkflowDocPhase:  false,
		}

	case v2.RouteToWorkflow:
		if result.HandleResult != nil {
			// Active workflow handled the message
			return h.handleWorkflowV2Action(msg, result.HandleResult)
		}
		// New workflow needs to be created
		return h.startNewWorkflowV2(msg, result)
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
	if wf.machine.GetActive(msg.UserID) != nil {
		wf.machine.Cancel(msg.UserID)
		emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", nil)
		log.Printf("[workflow-v2] cancelled previous workflow before starting new one: user=%s", msg.UserID)
	}

	projectPath := routeResult.ProjectPath
	if projectPath == "" {
		if h.app != nil {
			projectPath = strings.TrimSpace(h.app.GetCurrentProjectPath())
		}
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

	// Emit workflow started event
	h.emitWorkflowV2Progress(msg.UserID, state)

	// Run the first phase and return its output directly as the response.
	// The phase runs as a single agent loop call — loop ends, output returned.
	// User sees the document and the workflow waits for their next message.
	return h.runWorkflowV2Phase(msg.UserID, state, "")
}

func (h *IMMessageHandler) handleWorkflowV2Action(msg IMUserMessage, hr *v2.HandleResult) workflowIMRouteResult {
	switch hr.Action {
	case v2.ActionRunPhase:
		// Route based on ExecMode declared in template — no hardcoded phase IDs.
		if hr.Phase != nil {
			switch hr.Phase.ExecMode {
			case v2.ExecModeSubAgent:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=subagent for phase=%s, deferring SubAgent", hr.Phase.ID)
				if wf := h.getWorkflowV2(); wf != nil {
					if phase := hr.State.ActivePhase(); phase != nil {
						phase.Status = v2.PhaseExecuting
						wf.store.Save(hr.State)
					}
				}
				h.emitWorkflowV2Progress(msg.UserID, hr.State)
				h.pendingV2SubAgentExecution.Store(msg.UserID, true)
				h.workflowAgentLoopMarker.Store(msg.UserID, true)
				h.workflowOriginalRequest.Store(msg.UserID, "执行编码任务")
				return workflowIMRouteResult{
					WorkflowAgentLoop: true,
					WorkflowDocPhase:  false,
				}
			case v2.ExecModeAutoFromPrev:
				// Auto-complete from previous phase output — no execution needed.
				// This is used for phases like "verification" where the prior
				// phase (implementation) already produced the verification report.
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=auto_from_prev for phase=%s, auto-completing", hr.Phase.ID)
				// Already handled by RecordOutput auto-advance in the prior phase.
				// If we get here, the prior phase's auto-advance already completed this phase.
				// Just emit progress and return empty (workflow should be completed).
				if wf := h.getWorkflowV2(); wf != nil {
					if updatedState := wf.machine.GetActive(msg.UserID); updatedState != nil {
						h.emitWorkflowV2Progress(msg.UserID, updatedState)
					}
				}
				return workflowIMRouteResult{Response: &IMAgentResponse{Text: "✅ 工作流已完成"}}
			default:
				log.Printf("[workflow-v2] ActionRunPhase: ExecMode=default for phase=%s, running as agent loop", hr.Phase.ID)
			}
		}
		return h.runWorkflowV2Phase(msg.UserID, hr.State, "")

	case v2.ActionModify:
		return h.runWorkflowV2Phase(msg.UserID, hr.State, hr.ModifyHint)

	case v2.ActionConfirmed:
		// All phases complete (advanceLocked returns ActionConfirmed only when CurrentPhase >= len(Phases))
		h.emitWorkflowV2Progress(msg.UserID, hr.State)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "✅ 所有阶段已完成！工作流结束。"}}

	case v2.ActionCancelled:
		// Clear frontend workflow dashboard state.
		emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", nil)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "❌ 工作流已取消"}}

	case v2.ActionCancelAndExecute:
		// User wants to cancel the workflow but still execute the original task
		// directly (without the multi-phase process). Retrieve the original
		// request from state.Summary and let it fall through to the agent loop.
		originalRequest := ""
		if hr.State != nil {
			originalRequest = hr.State.Summary
		}
		log.Printf("[workflow-v2] ActionCancelAndExecute: user=%s original_len=%d", msg.UserID, len([]rune(originalRequest)))
		// Clear frontend workflow dashboard state.
		emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", nil)
		if originalRequest != "" {
			// Stash the original request so the agent loop processes it
			// instead of the "取消，直接处理" text.
			h.pendingCancelExecuteRequest.Store(msg.UserID, originalRequest)
		}
		// SkipNeedsConfirmGate ensures the message goes to normal agent loop
		// without being intercepted by any residual workflow gates.
		return workflowIMRouteResult{SkipNeedsConfirmGate: true}

	case v2.ActionPassThrough:
		return workflowIMRouteResult{SkipNeedsConfirmGate: true}
	}

	return workflowIMRouteResult{}
}

// runWorkflowV2Phase runs the current phase as a single agent loop invocation.
// The agent loop produces the phase document, then the response is returned to the user.
// The loop runs once to completion — output is captured post-loop by recordWorkflowV2Output.
func (h *IMMessageHandler) runWorkflowV2Phase(userID string, state *v2.WorkflowState, modifyHint string) workflowIMRouteResult {
	phase := state.ActivePhase()
	if phase == nil {
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "✅ 工作流已完成"}}
	}

	// Emit progress update so the frontend board always reflects the current phase.
	h.emitWorkflowV2Progress(userID, state)

	phasePrompt := v2.BuildPhasePrompt(state)
	if modifyHint != "" {
		phasePrompt += "\n\n## 用户修改意见\n\n" + modifyHint + "\n\n请根据以上修改意见重新生成本阶段文档。"
	}

	// Store phase prompt for the agent loop to consume.
	h.stashedPhasePrompt.Store(userID, phasePrompt)
	h.workflowAgentLoopMarker.Store(userID, true)

	// Clear conversation history for each doc phase.
	// Root cause of LLM self-repeating: history contains previous phase's
	// confirm/advance pattern ("user: 确认" → "assistant: 好的，进入下一阶段...").
	// LLM copies this pattern and self-confirms after generating the document.
	// Each phase should start with a clean slate — all context comes from phase prompt.
	if h.memory != nil {
		h.memory.Clear(userID)
	}

	// Set explicit userText for the agent loop so the LLM knows what to do.
	phaseUserText := fmt.Sprintf("请现在生成「%s」阶段的完整文档内容。不要引用或指向之前的对话，直接在本次回复中输出完整文档。", phase.Name)
	if modifyHint != "" {
		phaseUserText = fmt.Sprintf("请根据修改意见重新生成「%s」的完整文档。直接输出完整内容。", phase.Name)
	}
	h.workflowOriginalRequest.Store(userID, phaseUserText)

	log.Printf("[workflow-v2] running phase: user=%s type=%s phase=%s project=%s",
		userID, state.Type, phase.ID, state.ProjectPath)

	// WorkflowAgentLoop=true signals the agent loop to use the stashed prompt.
	// WorkflowDocPhase distinguishes doc phases (text-only output, no tool calls expected)
	// from execution phases (LLM should call tools like bash/write_file).
	isDocPhase := phase.ToolPolicy != v2.ToolPolicyFull
	return workflowIMRouteResult{
		WorkflowAgentLoop: true,
		WorkflowDocPhase:  isDocPhase,
	}
}

// getWorkflowV2 returns the V2 workflow state, or nil if not initialized.
func (h *IMMessageHandler) getWorkflowV2() *workflowV2State {
	if h == nil || h.app == nil {
		return nil
	}
	return h.app.workflowV2
}

// emitWorkflowV2Progress sends workflow phase update events to the frontend.
// Uses the same event name and data format as V1 so the frontend preview panel works.
func (h *IMMessageHandler) emitWorkflowV2Progress(userID string, state *v2.WorkflowState) {
	if h.app == nil || state == nil {
		return
	}

	// Build phases array for progress board
	phases := make([]map[string]interface{}, len(state.Phases))
	for i, p := range state.Phases {
		phases[i] = map[string]interface{}{
			"id":            p.ID,
			"name":          p.Name,
			"status":        string(p.Status),
			"needs_confirm": p.NeedsConfirm,
		}
	}

	// Build phase_outputs map
	phaseOutputs := make(map[string]interface{})
	for _, p := range state.Phases {
		if p.Output != "" {
			phaseOutputs[p.ID] = p.Output
		}
	}

	// Current phase ID
	currentPhaseID := ""
	if p := state.ActivePhase(); p != nil {
		currentPhaseID = p.ID
	}

	// Emit workflow:phase_update (the event name frontend listens to)
	emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
		"id":            state.ID,
		"status":        string(state.Status),
		"type":          state.Type,
		"current_phase": currentPhaseID,
		"phases":        phases,
		"phase_outputs": phaseOutputs,
		"project_path":  state.ProjectPath,
	})

	// Also emit suggest_maximize for desktop panel to auto-expand
	if state.Status == v2.StatusActive {
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize", map[string]interface{}{
			"workflow_type": state.Type,
		})
	} else {
		emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", nil)
	}
}

// recordWorkflowV2Output is called from the agent loop when substantial
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
	state := wf.machine.GetActive(userID)
	if state == nil {
		return
	}
	// Emit phase_update so the progress board reflects the new state (waiting_confirm)
	h.emitWorkflowV2Progress(userID, state)
	// Emit doc_update for the preview panel
	if state.IsWaitingConfirm() {
		phase := state.ActivePhase()
		if phase != nil {
			h.emitDocUpdateV2(userID, phase.ID, output)
		}
	}
}

// emitDocUpdateV2 sends document update to frontend preview panel.
func (h *IMMessageHandler) emitDocUpdateV2(userID, phaseID, content string) {
	if h.app == nil {
		return
	}
	wf := h.getWorkflowV2()
	projectPath := ""
	if wf != nil {
		if state := wf.machine.GetActive(userID); state != nil {
			projectPath = state.ProjectPath
		}
	}
	emitWorkflowV2Event(h.app, "workflow:doc_update", map[string]interface{}{
		"phase_id":     phaseID,
		"content":      content,
		"project_path": projectPath,
	})
}

// cancelWorkflowV2 cancels any active V2 workflow for the user.
func (h *IMMessageHandler) cancelWorkflowV2(userID string) {
	wf := h.getWorkflowV2()
	if wf == nil {
		return
	}
	wf.machine.Cancel(userID)
	// Emit null phase_update to reset frontend preview panel
	emitWorkflowV2Event(h.app, "workflow:phase_update", nil)
	emitWorkflowV2Event(h.app, "workflow:suggest_maximize_dismiss", nil)
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
// Tasks run synchronously for now (each task gets its own SubAgent call).
// Returns nil to fall through to normal agent loop if tasks can't be parsed.
func (h *IMMessageHandler) handleWorkflowV2ExecutionPhase(userID string, state *v2.WorkflowState) *IMAgentResponse {
	if state == nil || !state.IsExecutionPhase() {
		return nil
	}

	// Parse tasks from the task breakdown phase output
	tasksPhaseOutput := getPhaseOutput(state, "tasks")
	if tasksPhaseOutput == "" {
		// Also try loading fresh state from store in case of stale reference
		if wf := h.getWorkflowV2(); wf != nil {
			if fresh := wf.machine.GetActive(userID); fresh != nil {
				tasksPhaseOutput = getPhaseOutput(fresh, "tasks")
				if tasksPhaseOutput != "" {
					log.Printf("[workflow-v2] execution phase: tasks output was empty in passed state but found in fresh load (len=%d)", len(tasksPhaseOutput))
					state = fresh
				}
			}
		}
	}
	if tasksPhaseOutput == "" {
		log.Printf("[workflow-v2] execution phase but no task breakdown output (phase outputs: %v), falling back to agent loop", listPhaseIDs(state))
		return nil
	}

	tasks := v2.ParseTaskList(tasksPhaseOutput)
	if len(tasks) == 0 {
		log.Printf("[workflow-v2] no tasks parsed from breakdown (output_len=%d), falling back to agent loop. First 200 chars: %s",
			len(tasksPhaseOutput), truncateRunesV2(tasksPhaseOutput, 200))
		return nil
	}

	// Ensure project directory exists
	if err := v2.EnsureProjectDir(state.ProjectPath); err != nil {
		log.Printf("[workflow-v2] failed to ensure project dir %s: %v", state.ProjectPath, err)
	}

	reqCtx := truncateRunesV2(getPhaseOutput(state, "requirements"), 500)
	designCtx := truncateRunesV2(getPhaseOutput(state, "design"), 500)

	log.Printf("[workflow-v2] starting execution: %d tasks, project=%s", len(tasks), state.ProjectPath)

	// Mark phase as executing so subsequent messages pass through
	wf := h.getWorkflowV2()
	if wf != nil {
		if phase := state.ActivePhase(); phase != nil {
			phase.Status = v2.PhaseExecuting
			wf.store.Save(state)
		}
	}
	// Notify frontend that we've entered the execution phase.
	h.emitWorkflowV2Progress(userID, state)

	// Build SubAgent bridge: V2 TaskItem → V1 RunTaskWithSubAgent
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, onToken func(string), onProgress func(string)) *v2.TaskRunResult {
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
		v1Result := fn(h, cfg, httpClient, v1Task, config.ProjectPath, config.RequirementsCtx, config.DesignCtx, nil, nil, onToken, onProgress)
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

	// Run tasks synchronously — keeps the frontend spinner active until completion.
	// SubAgent execution may take several minutes for complex projects.
	runner := v2.NewTaskRunner(config, subAgentFn)
	runner.RunAll(context.Background(), tasks, nil, func(progress string) {
		log.Printf("[workflow-v2-exec] %s", progress)
		// Send progress to the frontend spinner area via the standard progress event.
		// The frontend's useAIAssistant hook listens to this and shows it below the spinner.
		if h.app != nil && h.app.ctx != nil {
			runtime.EventsEmit(h.app.ctx, "ai-assistant-progress", progress)
		}
	})
	report := runner.FinalReport()
	if wf := h.getWorkflowV2(); wf != nil {
		wf.machine.RecordOutput(userID, report)
		// Auto-complete next phase if it's ExecModeAutoFromPrev
		if updatedState := wf.machine.GetActive(userID); updatedState != nil {
			if nextPhase := updatedState.ActivePhase(); nextPhase != nil && nextPhase.ExecMode == v2.ExecModeAutoFromPrev {
				log.Printf("[workflow-v2] auto-completing phase=%s (ExecMode=auto_from_prev)", nextPhase.ID)
				wf.machine.RecordOutput(userID, report)
			}
		}
		if updatedState := wf.machine.GetActive(userID); updatedState != nil {
			h.emitWorkflowV2Progress(userID, updatedState)
		} else {
			emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
				"status": "completed",
			})
		}
	}
	log.Printf("[workflow-v2] execution complete: user=%s\n%s", userID, report)

	return &IMAgentResponse{
		Text: fmt.Sprintf("🚀 执行完成 %d 个编码任务\n项目路径：%s\n\n%s\n\n%s",
			len(tasks), state.ProjectPath, formatTaskListBrief(tasks), report),
	}
}

// handleWorkflowV2ExecutionPhaseWithProgress wraps handleWorkflowV2ExecutionPhase
// but uses the provided onProgress callback (which carries request_id context)
// instead of raw runtime.EventsEmit. This ensures progress events reach the frontend
// with the correct request_id for the active round.
func (h *IMMessageHandler) handleWorkflowV2ExecutionPhaseWithProgress(userID string, state *v2.WorkflowState, onProgress func(string), onToken func(string)) *IMAgentResponse {
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

	log.Printf("[workflow-v2] starting execution (with progress): %d tasks, project=%s", len(tasks), state.ProjectPath)

	h.emitWorkflowV2Progress(userID, state)

	// Send initial progress so frontend shows "coding started"
	if onProgress != nil {
		onProgress(fmt.Sprintf("🚀 开始编码执行：%d 个任务", len(tasks)))
	}

	// Wrap onToken to route SubAgent text output to the reasoning/thinking UI
	// (collapsible panel). Frontend uses \x01 prefix to distinguish reasoning
	// tokens from content tokens — they render in a collapsed "thinking" area.
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

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	subAgentFn := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, onToken func(string), onProgressFn func(string)) *v2.TaskRunResult {
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
		v1Result := fn(h, cfg, httpClient, v1Task, config.ProjectPath, config.RequirementsCtx, config.DesignCtx, nil, nil, onToken, onProgressFn)
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
	runner.RunAll(context.Background(), tasks, reasoningToken, func(progress string) {
		log.Printf("[workflow-v2-exec] %s", progress)
		// Use the onProgress callback which carries request_id context
		if onProgress != nil {
			onProgress(progress)
		}
	})
	report := runner.FinalReport()

	// Push the final report through regular onToken (NOT reasoning) so it appears
	// as visible content, not hidden in the collapsed thinking panel.
	if onToken != nil {
		onToken("\n\n" + report)
	}

	if wf := h.getWorkflowV2(); wf != nil {
		wf.machine.RecordOutput(userID, report)
		// If the next phase has ExecMode=auto_from_prev, auto-complete it with this report.
		// This eliminates the need for a separate execution step — the prior phase's
		// output IS the next phase's output (e.g. SubAgent report includes verification results).
		if updatedState := wf.machine.GetActive(userID); updatedState != nil {
			if nextPhase := updatedState.ActivePhase(); nextPhase != nil && nextPhase.ExecMode == v2.ExecModeAutoFromPrev {
				log.Printf("[workflow-v2] auto-completing phase=%s (ExecMode=auto_from_prev) with prior output", nextPhase.ID)
				wf.machine.RecordOutput(userID, report)
			}
		}
		if updatedState := wf.machine.GetActive(userID); updatedState != nil {
			h.emitWorkflowV2Progress(userID, updatedState)
		} else {
			emitWorkflowV2Event(h.app, "workflow:phase_update", map[string]interface{}{
				"status": "completed",
			})
		}
	}
	log.Printf("[workflow-v2] execution complete: user=%s\n%s", userID, report)

	return &IMAgentResponse{
		Text: fmt.Sprintf("🚀 执行完成 %d 个编码任务\n项目路径：%s\n\n%s\n\n%s",
			len(tasks), state.ProjectPath, formatTaskListBrief(tasks), report),
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
	var sb strings.Builder
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("- T%d: %s\n", t.Index, t.Title))
	}
	return sb.String()
}

// --- Helper: emit frontend event ---

func emitWorkflowV2Event(a *App, eventName string, data interface{}) {
	if a == nil || a.ctx == nil {
		log.Printf("[workflow-v2-event] %s (no ctx): %v", eventName, data)
		return
	}
	runtime.EventsEmit(a.ctx, eventName, data)
}

// runDirectCodingSubAgent executes a single coding task directly via SubAgent
// without going through the full SDD workflow. Used for simple/medium tasks.
func (h *IMMessageHandler) runDirectCodingSubAgent(userID, userText, projectPath string, onProgress func(string), onToken func(string)) *IMAgentResponse {
	if err := v2.EnsureProjectDir(projectPath); err != nil {
		log.Printf("[workflow-v2] direct coding: failed to ensure project dir %s: %v", projectPath, err)
	}

	// Create a single task from the user's request
	task := &v2.TaskItem{
		Index:       1,
		Title:       userText,
		Description: userText,
	}
	tasks := []*v2.TaskItem{task}

	log.Printf("[workflow-v2] direct coding: user=%s project=%s task=%q", userID, projectPath, truncateRunesV2(userText, 80))

	if onProgress != nil {
		onProgress("🚀 直接编码模式：开始执行")
	}

	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	subAgentFn := func(ctx context.Context, t *v2.TaskItem, config v2.TaskRunnerConfig, onTk func(string), onPr func(string)) *v2.TaskRunResult {
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
		v1Result := fn(h, cfg, httpClient, v1Task, config.ProjectPath, "", "", nil, nil, onTk, onPr)
		if v1Result == nil {
			return &v2.TaskRunResult{TaskIndex: t.Index, Title: t.Title, Status: v2.TaskFailed, Error: "SubAgent returned nil"}
		}
		status := v2.TaskFailed
		switch v1Result.Status {
		case TaskExecPassed:
			status = v2.TaskPassed
		case TaskExecSkipped:
			status = v2.TaskSkipped
		}
		return &v2.TaskRunResult{
			TaskIndex:     t.Index,
			Title:         t.Title,
			Status:        status,
			Summary:       v1Result.Summary,
			FilesCreated:  v1Result.FilesCreated,
			FilesModified: v1Result.FilesModified,
			Error:         v1Result.Error,
		}
	}

	config := v2.TaskRunnerConfig{
		ProjectPath: projectPath,
		MaxRetries:  2,
		TDDMode:     true,
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

	runner := v2.NewTaskRunner(config, subAgentFn)
	runner.RunAll(context.Background(), tasks, reasoningToken, func(progress string) {
		log.Printf("[workflow-v2-direct] %s", progress)
		if onProgress != nil {
			onProgress(progress)
		}
	})
	report := runner.FinalReport()

	// Push final report as visible content
	if onToken != nil {
		onToken("\n\n" + report)
	}

	log.Printf("[workflow-v2] direct coding complete: user=%s\n%s", userID, report)

	return &IMAgentResponse{
		Text: fmt.Sprintf("✅ 编码完成\n项目路径：%s\n\n%s", projectPath, report),
	}
}

// buildComplexityFunc returns a ComplexityFunc that uses the configured LLM
// to assess whether a coding task is simple (direct SubAgent) or complex (full SDD).
func (h *IMMessageHandler) buildComplexityFunc() v2.ComplexityFunc {
	return func(text string) v2.TaskComplexity {
		cfg := h.getMaclawLLMConfig()
		if cfg.Key == "" && cfg.URL == "" {
			// No LLM available — default to complex (safe)
			return v2.ComplexityComplex
		}

		systemPrompt := `You are a task classifier. Given a user request, respond with ONLY one word: "simple", "complex", or "none".

"simple" — This is a coding task that can be done directly without planning:
- Bug fixes, typo corrections in code
- Hello World, single-file programs
- Add one function, one API endpoint, one button
- Write a script or small utility (< 100 lines)
- Configuration file changes
- Code that does ONE thing (calculator, timer, converter)

"complex" — This is a coding task that needs requirements → design → task breakdown:
- Multi-module systems with 5+ components
- Games with rendering, physics, AI, audio subsystems
- Full applications needing architecture decisions
- Projects with database, auth, deployment needs
- 500+ lines across multiple interdependent files

"none" — This is NOT a coding task at all:
- Document generation (reports, summaries, translations)
- File operations (copy, move, convert formats)
- Information lookup or research
- Anything that doesn't involve writing/modifying source code

CRITICAL RULES:
- "hello world" or single-output programs → "simple"
- "修bug"/"fix bug" in actual code → "simple"
- "生成报告"/"generate report" even if it mentions "BUG" → "none" (it's document work, not coding)
- "写测试用例" without actual code context → "none"
- Games, apps, systems with multiple features → "complex"

Respond with ONLY one word: simple, complex, or none.`

		result := h.callLightweightLLM(cfg, systemPrompt, text, 5)
		result = strings.TrimSpace(strings.ToLower(result))

		switch {
		case result == "none" || strings.Contains(result, "none"):
			// Not a coding task — route to agent loop (normal non-coding handling)
			log.Printf("[workflow-v2] complexity assessment: NONE (not coding) for %q", truncateRunesV2(text, 60))
			return v2.ComplexityNone
		case result == "simple" || (strings.Contains(result, "simple") && !strings.Contains(result, "complex")):
			log.Printf("[workflow-v2] complexity assessment: SIMPLE for %q", truncateRunesV2(text, 60))
			return v2.ComplexitySimple
		case result == "complex" || strings.Contains(result, "complex"):
			log.Printf("[workflow-v2] complexity assessment: COMPLEX for %q", truncateRunesV2(text, 60))
			return v2.ComplexityComplex
		default:
			log.Printf("[workflow-v2] complexity assessment: AMBIGUOUS (%q) → defaulting to COMPLEX for %q", result, truncateRunesV2(text, 60))
			return v2.ComplexityComplex
		}
	}
}

// callLightweightLLM makes a quick non-streaming LLM call for classification.
// Returns the response text or empty string on failure.
func (h *IMMessageHandler) callLightweightLLM(cfg corelib.MaclawLLMConfig, systemPrompt, userText string, timeoutSec int) string {
	if h == nil || h.client == nil {
		return ""
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userText},
	}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "workflow-v2-lightweight"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, h.client, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		log.Printf("[workflow-v2] callLightweightLLM: request failed: %v", err)
		return ""
	}
	if resp == nil {
		return ""
	}
	return strings.TrimSpace(resp.Content)
}
