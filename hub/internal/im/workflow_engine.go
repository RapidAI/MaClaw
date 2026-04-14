package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// ---------------------------------------------------------------------------
// WorkflowEngine — core workflow execution engine (Task 4.1)
// ---------------------------------------------------------------------------

// WorkflowEngine coordinates template matching, intent understanding,
// and phase-by-phase workflow execution.
type WorkflowEngine struct {
	registry        *WorkflowRegistry
	understandingMgr *UnderstandingManager
	repo            store.WorkflowRepository
	configProvider  func() *HubLLMConfig
	breaker         *CircuitBreaker
	llmSem          *LLMSemaphore
	router          *MessageRouter
	spaceState      *spaceStateStore

	// In-memory cache of active workflows
	mu        sync.RWMutex
	workflows map[string]*WorkflowState // userID → active workflow
}

// NewWorkflowEngine creates a new WorkflowEngine with all dependencies.
func NewWorkflowEngine(
	registry *WorkflowRegistry,
	understandingMgr *UnderstandingManager,
	repo store.WorkflowRepository,
	configProvider func() *HubLLMConfig,
	breaker *CircuitBreaker,
	llmSem *LLMSemaphore,
	router *MessageRouter,
	spaceState *spaceStateStore,
) *WorkflowEngine {
	return &WorkflowEngine{
		registry:         registry,
		understandingMgr: understandingMgr,
		repo:             repo,
		configProvider:   configProvider,
		breaker:          breaker,
		llmSem:           llmSem,
		router:           router,
		spaceState:       spaceState,
		workflows:        make(map[string]*WorkflowState),
	}
}

// HasActiveWorkflow implements ActiveSessionChecker.
func (we *WorkflowEngine) HasActiveWorkflow(userID string) bool {
	return we.GetActiveWorkflow(userID) != nil
}

// HasActiveUnderstanding implements ActiveSessionChecker.
func (we *WorkflowEngine) HasActiveUnderstanding(userID string) bool {
	if we.understandingMgr == nil {
		return false
	}
	return we.understandingMgr.GetActiveSession(userID) != nil
}

// StartWorkflow creates a new workflow from a confirmed understanding session.
// It matches a template, creates the workflow state, persists it, generates
// an overview, and auto-executes the first phase.
func (we *WorkflowEngine) StartWorkflow(
	ctx context.Context,
	userID string,
	session *UnderstandingSession,
) (*GenericResponse, error) {
	// Check for existing active workflow
	if existing := we.GetActiveWorkflow(userID); existing != nil {
		return &GenericResponse{
			StatusCode: 409,
			StatusIcon: "⚠️",
			Title:      "已有活跃工作流",
			Body:       fmt.Sprintf("您当前有一个活跃的 %s 工作流，请先完成或取消（/workflow cancel）。", existing.Type),
		}, nil
	}

	// Match template
	tmpl := we.registry.Match(session.Intent.Category)
	if tmpl == nil {
		// Default to coding if no match
		tmpl = we.registry.Match(WorkflowCoding)
	}
	if tmpl == nil {
		return nil, fmt.Errorf("no workflow template found for category %s", session.Intent.Category)
	}

	now := time.Now()
	state := &WorkflowState{
		ID:           fmt.Sprintf("wf_%d", now.UnixNano()),
		UserID:       userID,
		Type:         tmpl.Type,
		TemplateRef:  tmpl.Type,
		Intent:       session.Intent,
		CurrentPhase: tmpl.Phases[0].ID,
		PhaseOutputs: make(map[string]string),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Store in memory
	we.mu.Lock()
	we.workflows[userID] = state
	we.mu.Unlock()

	// Persist
	we.persistWorkflow(ctx, state)

	// Enter workflow space state
	if we.spaceState != nil {
		we.spaceState.EnterWorkflow(userID, state.ID, string(state.Type))
	}

	// Clean up the understanding session
	if we.understandingMgr != nil {
		we.understandingMgr.RemoveSession(userID)
	}

	// Generate overview
	overview := we.formatWorkflowOverview(tmpl, state)

	// Auto-execute first phase
	firstPhase := &tmpl.Phases[0]
	phaseResp, err := we.executePhase(ctx, state, firstPhase)
	if err != nil {
		log.Printf("[WorkflowEngine] first phase execution error: %v", err)
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "📋",
			Title:      "工作流已创建",
			Body:       overview + "\n\n⚠️ 第一阶段执行出错，请重试或发送修改意见。",
		}, nil
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "📋",
		Title:      "工作流已创建",
		Body:       overview + "\n\n---\n\n" + phaseResp.Body,
	}, nil
}

// HandleWorkflowInput processes user input within an active workflow.
func (we *WorkflowEngine) HandleWorkflowInput(
	ctx context.Context,
	userID, text string,
) (*GenericResponse, error) {
	state := we.GetActiveWorkflow(userID)
	if state == nil {
		return nil, fmt.Errorf("no active workflow for user %s", userID)
	}

	// Off-topic detection
	offTopic := detectOffTopic(string(state.Type), text)
	switch offTopic {
	case OffTopicSimple:
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "💬",
			Title:      "快速回答",
			Body:       "这个问题与当前工作流无关，我先简单回答。\n\n（当前工作流仍在进行中，继续发送与工作流相关的内容即可。）",
		}, nil
	case OffTopicComplex:
		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "⚠️",
			Title:      "有活跃工作流",
			Body:       fmt.Sprintf("当前有活跃的 %s 工作流，建议先完成或发送 /workflow cancel 取消后再开始新任务。", state.Type),
		}, nil
	}

	// Identify user intent
	if isCancelTrigger(text) {
		return we.cancelWorkflowWithResponse(userID)
	}
	if isAdvanceTrigger(text) {
		return we.advancePhase(ctx, state)
	}
	if isSkipTrigger(text) {
		return we.skipPhase(ctx, state)
	}

	// Default: treat as modification request
	return we.modifyCurrentPhase(ctx, state, text)
}

// CancelWorkflow marks the workflow as cancelled and cleans up.
func (we *WorkflowEngine) CancelWorkflow(userID string) error {
	we.mu.Lock()
	state := we.workflows[userID]
	delete(we.workflows, userID)
	we.mu.Unlock()

	if state != nil && we.repo != nil {
		_ = we.repo.DeleteWorkflowState(context.Background(), state.ID)
	}

	// Exit workflow space state
	if we.spaceState != nil {
		we.spaceState.ExitWorkflow(userID)
	}

	return nil
}

func (we *WorkflowEngine) cancelWorkflowWithResponse(userID string) (*GenericResponse, error) {
	we.CancelWorkflow(userID)
	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🚫",
		Title:      "工作流已取消",
		Body:       "好的，已取消当前工作流，返回大厅。",
	}, nil
}

// GetActiveWorkflow returns the active workflow for a user.
func (we *WorkflowEngine) GetActiveWorkflow(userID string) *WorkflowState {
	we.mu.RLock()
	state := we.workflows[userID]
	we.mu.RUnlock()
	if state != nil {
		return state
	}

	// Fall back to SQLite
	if we.repo == nil {
		return nil
	}
	row, err := we.repo.GetActiveWorkflowState(context.Background(), userID)
	if err != nil || row == nil {
		return nil
	}

	s := &WorkflowState{
		ID:           row.ID,
		UserID:       row.UserID,
		Type:         WorkflowType(row.Type),
		TemplateRef:  WorkflowType(row.TemplateType),
		CurrentPhase: row.CurrentPhase,
		PhaseOutputs: make(map[string]string),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.IntentJSON != "" {
		_ = json.Unmarshal([]byte(row.IntentJSON), &s.Intent)
	}
	if row.PhaseOutputsJSON != "" {
		_ = json.Unmarshal([]byte(row.PhaseOutputsJSON), &s.PhaseOutputs)
	}

	we.mu.Lock()
	we.workflows[userID] = s
	we.mu.Unlock()

	return s
}

// formatWorkflowOverview generates a text overview of the workflow phases.
func (we *WorkflowEngine) formatWorkflowOverview(tmpl *WorkflowTemplate, state *WorkflowState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📋 **%s 工作流**\n\n", tmpl.Name)
	fmt.Fprintf(&b, "📝 %s\n\n", state.Intent.Summary)
	b.WriteString("阶段概览：\n")
	for i, phase := range tmpl.Phases {
		marker := "⬜"
		if phase.ID == state.CurrentPhase {
			marker = "▶️"
		}
		fmt.Fprintf(&b, "%s %d. %s — %s\n", marker, i+1, phase.Name, phase.Description)
	}
	return b.String()
}

// persistWorkflow saves the workflow state to SQLite.
func (we *WorkflowEngine) persistWorkflow(ctx context.Context, state *WorkflowState) {
	if we.repo == nil {
		return
	}

	intentJSON, _ := json.Marshal(state.Intent)
	outputsJSON, _ := json.Marshal(state.PhaseOutputs)

	row := &store.WorkflowStateRow{
		ID:               state.ID,
		UserID:           state.UserID,
		Type:             string(state.Type),
		TemplateType:     string(state.TemplateRef),
		IntentJSON:       string(intentJSON),
		CurrentPhase:     state.CurrentPhase,
		PhaseOutputsJSON: string(outputsJSON),
		CreatedAt:        state.CreatedAt,
		UpdatedAt:        state.UpdatedAt,
	}

	if err := we.repo.SaveWorkflowState(ctx, row); err != nil {
		log.Printf("[WorkflowEngine] persist error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase execution & quality gate (Task 4.2)
// ---------------------------------------------------------------------------

// CheckResult holds the result of a single checklist item evaluation.
type CheckResult struct {
	Item   string `json:"item"`
	Status string `json:"status"` // "pass", "warn", "fail"
	Detail string `json:"detail"`
}

// executePhase builds the LLM prompt, calls the LLM, runs the checklist,
// stores the output, and formats the response.
func (we *WorkflowEngine) executePhase(
	ctx context.Context,
	state *WorkflowState,
	phase *PhaseTemplate,
) (*GenericResponse, error) {
	// For device-routed phases, delegate to executeDevicePhase
	if phase.NeedsDevice {
		return we.executeDevicePhase(ctx, state, phase)
	}

	cfg := we.configProvider()
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("LLM not configured")
	}

	if !we.breaker.Allow() {
		return nil, fmt.Errorf("circuit breaker open")
	}

	// Build phase prompt
	prompt := we.buildPhasePrompt(state, phase)
	messages := []interface{}{
		map[string]string{"role": "system", "content": prompt},
		map[string]string{"role": "user", "content": fmt.Sprintf("请执行 %s 阶段，生成产出物。", phase.Name)},
	}

	llmCfg := cfg.ToMaclawLLMConfig()
	const phaseTimeout = 30 * time.Second
	callCtx, cancel := context.WithTimeout(ctx, phaseTimeout)
	defer cancel()

	if !we.llmSem.Acquire(callCtx) {
		return nil, fmt.Errorf("LLM semaphore timeout")
	}
	defer we.llmSem.Release()

	client := &http.Client{Timeout: phaseTimeout}
	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, client, phaseTimeout)
	if err != nil {
		we.breaker.RecordFailure()
		return nil, fmt.Errorf("phase LLM call failed: %w", err)
	}
	we.breaker.RecordSuccess()

	output := strings.TrimSpace(resp.Content)

	// Run checklist
	var checks []CheckResult
	if len(phase.Checklist) > 0 {
		checks = we.runChecklist(ctx, output, phase.Checklist)
	}

	// Store output
	state.PhaseOutputs[phase.ID] = output
	state.UpdatedAt = time.Now()
	we.persistWorkflow(ctx, state)

	// Format response
	body := formatPhaseOutput(output, checks, *phase)

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "📄",
		Title:      fmt.Sprintf("阶段: %s", phase.Name),
		Body:       body,
	}, nil
}

// buildPhasePrompt constructs the LLM prompt for a phase execution.
func (we *WorkflowEngine) buildPhasePrompt(state *WorkflowState, phase *PhaseTemplate) string {
	var b strings.Builder

	b.WriteString("你是一个专业的工作流执行助手。\n\n")
	fmt.Fprintf(&b, "## 任务概述\n%s\n\n", state.Intent.Summary)

	if len(state.Intent.Goals) > 0 {
		b.WriteString("## 目标\n")
		for _, g := range state.Intent.Goals {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}

	if len(state.Intent.Constraints) > 0 {
		b.WriteString("## 约束条件\n")
		for _, c := range state.Intent.Constraints {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	// Include previous phase outputs as context
	tmpl := we.registry.Match(state.TemplateRef)
	if tmpl != nil {
		for _, p := range tmpl.Phases {
			if p.ID == phase.ID {
				break
			}
			if output, ok := state.PhaseOutputs[p.ID]; ok {
				fmt.Fprintf(&b, "## 前序阶段: %s\n%s\n\n", p.Name, truncate(output, 2000))
			}
		}
	}

	fmt.Fprintf(&b, "## 当前阶段: %s\n%s\n\n", phase.Name, phase.Description)

	if phase.Prompt != "" {
		fmt.Fprintf(&b, "## 执行指令\n%s\n\n", phase.Prompt)
	}

	fmt.Fprintf(&b, "## 期望产出\n%s\n\n", phase.Deliverable)
	b.WriteString("请直接输出产出物内容，不要包含额外的解释或前言。")

	return b.String()
}

// runChecklist uses the LLM to evaluate each checklist item against the output.
func (we *WorkflowEngine) runChecklist(
	ctx context.Context,
	output string,
	checklist []string,
) []CheckResult {
	cfg := we.configProvider()
	if cfg == nil || !cfg.Enabled {
		// Return all items as unchecked
		var results []CheckResult
		for _, item := range checklist {
			results = append(results, CheckResult{Item: item, Status: "warn", Detail: "LLM 未配置，无法检查"})
		}
		return results
	}

	var checklistText strings.Builder
	for i, item := range checklist {
		fmt.Fprintf(&checklistText, "%d. %s\n", i+1, item)
	}

	prompt := fmt.Sprintf(`请对以下产出物进行质量检查。

产出物：
%s

检查清单：
%s

请以 JSON 数组格式返回检查结果，每项包含 item、status（pass/warn/fail）、detail 字段。
示例：[{"item":"检查项","status":"pass","detail":"符合要求"}]

仅返回 JSON 数组，不要其他内容。`, truncate(output, 3000), checklistText.String())

	messages := []interface{}{
		map[string]string{"role": "system", "content": "你是一个质量检查助手，严格按照检查清单评估产出物质量。"},
		map[string]string{"role": "user", "content": prompt},
	}

	llmCfg := cfg.ToMaclawLLMConfig()
	const checkTimeout = 15 * time.Second
	callCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	if !we.llmSem.Acquire(callCtx) {
		var results []CheckResult
		for _, item := range checklist {
			results = append(results, CheckResult{Item: item, Status: "warn", Detail: "检查超时"})
		}
		return results
	}
	defer we.llmSem.Release()

	client := &http.Client{Timeout: checkTimeout}
	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, client, checkTimeout)
	if err != nil {
		we.breaker.RecordFailure()
		var results []CheckResult
		for _, item := range checklist {
			results = append(results, CheckResult{Item: item, Status: "warn", Detail: "检查失败"})
		}
		return results
	}
	we.breaker.RecordSuccess()

	// Parse JSON result
	content := strings.TrimSpace(resp.Content)
	// Strip markdown code fences
	if strings.HasPrefix(content, "```") {
		lines := strings.Split(content, "\n")
		if len(lines) > 2 {
			end := len(lines) - 1
			for end > 0 && strings.TrimSpace(lines[end]) == "" {
				end--
			}
			if end > 0 && strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				content = strings.Join(lines[1:end], "\n")
			} else {
				content = strings.Join(lines[1:], "\n")
			}
		}
	}

	var results []CheckResult
	if err := json.Unmarshal([]byte(content), &results); err != nil {
		log.Printf("[WorkflowEngine] checklist parse error: %v", err)
		for _, item := range checklist {
			results = append(results, CheckResult{Item: item, Status: "warn", Detail: "解析失败"})
		}
	}

	return results
}

// formatPhaseOutput formats the phase output with checklist results and action hints.
func formatPhaseOutput(output string, checks []CheckResult, phase PhaseTemplate) string {
	var b strings.Builder

	b.WriteString(output)
	b.WriteString("\n\n")

	if len(checks) > 0 {
		b.WriteString("---\n📋 **质量检查**\n\n")
		for _, c := range checks {
			icon := "✅"
			switch c.Status {
			case "warn":
				icon = "⚠️"
			case "fail":
				icon = "❌"
			}
			fmt.Fprintf(&b, "%s %s", icon, c.Item)
			if c.Detail != "" {
				fmt.Fprintf(&b, " — %s", c.Detail)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Action hints
	b.WriteString("---\n")
	if phase.NeedsConfirm {
		b.WriteString("💡 说「下一步」继续，或直接提修改意见。")
	} else {
		b.WriteString("💡 说「下一步」继续，「跳过」跳过此阶段，或直接提修改意见。")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Phase advancement & user interaction (Task 4.3)
// ---------------------------------------------------------------------------

// isAdvanceTrigger returns true if the text is a "next step" trigger.
func isAdvanceTrigger(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	triggers := []string{
		"下一步", "确认", "继续", "next", "ok", "好的",
		"可以", "没问题", "通过",
	}
	for _, t := range triggers {
		if lower == t {
			return true
		}
	}
	return false
}

// isSkipTrigger returns true if the text is a "skip" trigger.
func isSkipTrigger(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return lower == "跳过" || lower == "skip"
}

// isCancelTrigger returns true if the text is a "cancel" trigger.
func isCancelTrigger(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	triggers := []string{"取消", "cancel", "算了", "不做了"}
	for _, t := range triggers {
		if lower == t {
			return true
		}
	}
	return false
}

// isModifyRequest returns true if the text is not a recognized trigger,
// meaning it should be treated as a modification request.
func isModifyRequest(text string) bool {
	return !isAdvanceTrigger(text) && !isSkipTrigger(text) && !isCancelTrigger(text)
}

// advancePhase moves to the next phase or completes the workflow.
func (we *WorkflowEngine) advancePhase(
	ctx context.Context,
	state *WorkflowState,
) (*GenericResponse, error) {
	tmpl := we.registry.Match(state.TemplateRef)
	if tmpl == nil {
		return nil, fmt.Errorf("template %s not found", state.TemplateRef)
	}

	// Find current phase index
	currentIdx := -1
	for i, p := range tmpl.Phases {
		if p.ID == state.CurrentPhase {
			currentIdx = i
			break
		}
	}

	if currentIdx < 0 {
		return nil, fmt.Errorf("current phase %s not found in template", state.CurrentPhase)
	}

	// Check if this is the last phase
	if currentIdx >= len(tmpl.Phases)-1 {
		// Workflow complete — persist final state before cleanup so outputs are saved.
		state.UpdatedAt = time.Now()
		we.persistWorkflow(ctx, state)

		// Clean up in-memory state and space state, but leave SQLite row
		// for historical reference (CleanupExpired will remove it after 7 days).
		we.mu.Lock()
		delete(we.workflows, state.UserID)
		we.mu.Unlock()
		if we.spaceState != nil {
			we.spaceState.ExitWorkflow(state.UserID)
		}

		return &GenericResponse{
			StatusCode: 200,
			StatusIcon: "🎉",
			Title:      "工作流完成",
			Body:       fmt.Sprintf("🎉 %s 工作流已全部完成！\n\n所有阶段的产出物已保存。", tmpl.Name),
		}, nil
	}

	// Move to next phase
	nextPhase := &tmpl.Phases[currentIdx+1]
	state.CurrentPhase = nextPhase.ID
	state.UpdatedAt = time.Now()

	we.mu.Lock()
	we.workflows[state.UserID] = state
	we.mu.Unlock()

	we.persistWorkflow(ctx, state)

	// Execute next phase
	return we.executePhase(ctx, state, nextPhase)
}

// modifyCurrentPhase rebuilds the current phase output with the user's
// modification request.
func (we *WorkflowEngine) modifyCurrentPhase(
	ctx context.Context,
	state *WorkflowState,
	text string,
) (*GenericResponse, error) {
	tmpl := we.registry.Match(state.TemplateRef)
	if tmpl == nil {
		return nil, fmt.Errorf("template %s not found", state.TemplateRef)
	}

	var currentPhase *PhaseTemplate
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == state.CurrentPhase {
			currentPhase = &tmpl.Phases[i]
			break
		}
	}
	if currentPhase == nil {
		return nil, fmt.Errorf("current phase %s not found", state.CurrentPhase)
	}

	cfg := we.configProvider()
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("LLM not configured")
	}

	// Build modification prompt
	currentOutput := state.PhaseOutputs[currentPhase.ID]
	prompt := fmt.Sprintf(`你是一个专业的工作流执行助手。

## 当前阶段: %s
%s

## 当前产出物
%s

## 用户修改请求
%s

请根据用户的修改请求，更新产出物。直接输出更新后的完整产出物，不要包含额外解释。`,
		currentPhase.Name, currentPhase.Description,
		truncate(currentOutput, 3000), text)

	messages := []interface{}{
		map[string]string{"role": "system", "content": prompt},
		map[string]string{"role": "user", "content": "请更新产出物。"},
	}

	llmCfg := cfg.ToMaclawLLMConfig()
	const modifyTimeout = 30 * time.Second
	callCtx, cancel := context.WithTimeout(ctx, modifyTimeout)
	defer cancel()

	if !we.llmSem.Acquire(callCtx) {
		return nil, fmt.Errorf("LLM semaphore timeout")
	}
	defer we.llmSem.Release()

	client := &http.Client{Timeout: modifyTimeout}
	resp, err := agent.DoSimpleLLMRequest(llmCfg, messages, client, modifyTimeout)
	if err != nil {
		we.breaker.RecordFailure()
		return nil, fmt.Errorf("modify LLM call failed: %w", err)
	}
	we.breaker.RecordSuccess()

	output := strings.TrimSpace(resp.Content)

	// Re-run checklist
	var checks []CheckResult
	if len(currentPhase.Checklist) > 0 {
		checks = we.runChecklist(ctx, output, currentPhase.Checklist)
	}

	// Update stored output
	state.PhaseOutputs[currentPhase.ID] = output
	state.UpdatedAt = time.Now()
	we.persistWorkflow(ctx, state)

	body := formatPhaseOutput(output, checks, *currentPhase)

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "✏️",
		Title:      fmt.Sprintf("已修改: %s", currentPhase.Name),
		Body:       body,
	}, nil
}

// skipPhase skips the current phase if it's skippable, then advances.
func (we *WorkflowEngine) skipPhase(
	ctx context.Context,
	state *WorkflowState,
) (*GenericResponse, error) {
	tmpl := we.registry.Match(state.TemplateRef)
	if tmpl == nil {
		return nil, fmt.Errorf("template %s not found", state.TemplateRef)
	}

	var currentPhase *PhaseTemplate
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == state.CurrentPhase {
			currentPhase = &tmpl.Phases[i]
			break
		}
	}
	if currentPhase == nil {
		return nil, fmt.Errorf("current phase %s not found", state.CurrentPhase)
	}

	if !currentPhase.CanSkip {
		return &GenericResponse{
			StatusCode: 400,
			StatusIcon: "⚠️",
			Title:      "不可跳过",
			Body:       fmt.Sprintf("阶段 %s 不可跳过，请完成后再继续。", currentPhase.Name),
		}, nil
	}

	// Mark as skipped and advance
	state.PhaseOutputs[currentPhase.ID] = "(已跳过)"
	return we.advancePhase(ctx, state)
}

// ---------------------------------------------------------------------------
// Device routing phase (Task 4.4)
// ---------------------------------------------------------------------------

// executeDevicePhase handles phases that need to be routed to a device.
func (we *WorkflowEngine) executeDevicePhase(
	ctx context.Context,
	state *WorkflowState,
	phase *PhaseTemplate,
) (*GenericResponse, error) {
	if we.router == nil {
		return nil, fmt.Errorf("message router not configured")
	}

	// Build task text from previous phase outputs + current phase description
	var taskText strings.Builder
	fmt.Fprintf(&taskText, "## 任务: %s\n\n", phase.Name)
	fmt.Fprintf(&taskText, "%s\n\n", phase.Description)
	fmt.Fprintf(&taskText, "## 需求摘要\n%s\n\n", state.Intent.Summary)

	// Include relevant previous phase outputs
	tmpl := we.registry.Match(state.TemplateRef)
	if tmpl != nil {
		for _, p := range tmpl.Phases {
			if p.ID == phase.ID {
				break
			}
			if output, ok := state.PhaseOutputs[p.ID]; ok && output != "(已跳过)" {
				fmt.Fprintf(&taskText, "## %s 产出\n%s\n\n", p.Name, truncate(output, 1500))
			}
		}
	}

	if phase.Prompt != "" {
		fmt.Fprintf(&taskText, "## 执行指令\n%s\n", phase.Prompt)
	}

	// Route to device using the existing router
	machines := we.router.devices.FindAllOnlineMachinesForUser(ctx, state.UserID)
	if len(machines) == 0 {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "📴",
			Title:      "设备不在线",
			Body:       "工作流需要设备执行此阶段，但当前没有在线设备。\n\n请启动 MaClaw 客户端后重试（说「继续」）。",
		}, nil
	}

	// Find a suitable machine (prefer LLM-configured ones)
	var targetID string
	for _, m := range machines {
		if m.LLMConfigured {
			targetID = m.MachineID
			break
		}
	}
	if targetID == "" {
		return &GenericResponse{
			StatusCode: 503,
			StatusIcon: "⚠️",
			Title:      "Agent 未就绪",
			Body:       "在线设备的 LLM 未配置，无法执行此阶段。",
		}, nil
	}

	// Route the task
	resp, err := we.router.routeToSingleMachine(ctx, state.UserID, "", "", taskText.String(), targetID, "")
	if err != nil {
		return resp, err
	}

	// Store the device response as phase output
	respBody := ""
	if resp != nil {
		respBody = resp.Body
		state.PhaseOutputs[phase.ID] = respBody
		state.UpdatedAt = time.Now()
		we.persistWorkflow(ctx, state)
	}

	return &GenericResponse{
		StatusCode: 200,
		StatusIcon: "🖥️",
		Title:      fmt.Sprintf("设备执行: %s", phase.Name),
		Body:       fmt.Sprintf("已将任务路由到设备执行。\n\n%s\n\n---\n💡 说「下一步」继续，或直接提修改意见。", respBody),
	}, nil
}
