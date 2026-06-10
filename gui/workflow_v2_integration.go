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
	log.Printf("[workflow-v2] routing: user=%s text_len=%d", msg.UserID, len([]rune(trimmed)))

	var attachments []v2.Attachment
	for _, a := range msg.Attachments {
		attachments = append(attachments, v2.Attachment{Type: a.Type, Name: a.FileName})
	}

	result := wf.router.Route(msg.UserID, trimmed, attachments)

	switch result.Target {
	case v2.RouteToAgentLoop:
		return workflowIMRouteResult{}

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
	if projectPath == "" {
		projectPath = "."
	}

	state, err := wf.machine.Create(msg.UserID, routeResult.WorkflowType, projectPath, msg.Text)
	if err != nil {
		log.Printf("[workflow-v2] Create failed: user=%s type=%s err=%v", msg.UserID, routeResult.WorkflowType, err)
		return workflowIMRouteResult{}
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
		// Check if we're entering an execution phase (e.g. after advancing from task_breakdown).
		if hr.Phase != nil && hr.Phase.ToolPolicy == v2.ToolPolicyFull && hr.State.Type == "coding" {
			log.Printf("[workflow-v2] ActionRunPhase: entering execution phase for user=%s, invoking SubAgent", msg.UserID)
			execResp := h.handleWorkflowV2ExecutionPhase(msg.UserID, hr.State)
			if execResp != nil {
				return workflowIMRouteResult{Response: execResp}
			}
			log.Printf("[workflow-v2] ActionRunPhase: SubAgent returned nil (task parse failed), falling back to agent loop")
			// SubAgent couldn't parse tasks — fall through to normal agent loop with full tools.
			if wf := h.getWorkflowV2(); wf != nil {
				if phase := hr.State.ActivePhase(); phase != nil {
					phase.Status = v2.PhaseExecuting
					wf.store.Save(hr.State)
				}
			}
		} else if hr.Phase != nil {
			log.Printf("[workflow-v2] ActionRunPhase: phase=%s toolPolicy=%s type=%s", hr.Phase.ID, hr.Phase.ToolPolicy, hr.State.Type)
		}
		return h.runWorkflowV2Phase(msg.UserID, hr.State, "")

	case v2.ActionModify:
		return h.runWorkflowV2Phase(msg.UserID, hr.State, hr.ModifyHint)

	case v2.ActionConfirmed:
		// All phases complete (advanceLocked returns ActionConfirmed only when CurrentPhase >= len(Phases))
		h.emitWorkflowV2Progress(msg.UserID, hr.State)
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "✅ 所有阶段已完成！工作流结束。"}}

	case v2.ActionCancelled:
		return workflowIMRouteResult{Response: &IMAgentResponse{Text: "❌ 工作流已取消"}}

	case v2.ActionPassThrough:
		return workflowIMRouteResult{SkipNeedsConfirmGate: true}
	}

	return workflowIMRouteResult{}
}

// runWorkflowV2Phase runs the current phase as a single agent loop invocation.
// The agent loop will produce the phase document, then the response is returned to the user.
// No V1 NeedsConfirm gate is needed — the loop runs once and exits.
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
