package cloudagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// The V2 workflow engine runs inside the instance so a cloud task gets the
// same phase/form/confirm flow a local task does. Each instance holds one
// SQLite store; workflows are keyed by task id, so several tasks on one
// instance can each carry their own flow.
type workflowHolder struct {
	machine *v2.StateMachine
	store   *v2.SQLiteStore
}

func workflowStorePath(dir string) string {
	return filepath.Join(dir, "workflow_v2.db")
}

// workflowMachine opens (or returns) the instance's workflow machine. The
// confirm classifier rides the instance's own LLM config, so official and
// custom providers both answer the review gate.
func (rt *Runtime) workflowMachine(inst *instance) (*v2.StateMachine, error) {
	inst.wfMu.Lock()
	defer inst.wfMu.Unlock()
	if inst.workflow != nil {
		return inst.workflow.machine, nil
	}
	store, err := v2.NewSQLiteStore(workflowStorePath(inst.dir))
	if err != nil {
		return nil, err
	}
	registry := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(registry)
	machine := v2.NewStateMachine(store, registry)
	machine.SetConfirmClassifier(rt.workflowConfirmClassifier(inst))
	if testing.Testing() {
		// Test instances live under t.TempDir — the desktop relaxes the same
		// Create guard behind its test-home flag.
		machine.SetAllowTempTestPaths(true)
	}
	// A persisted active flow predates this process: like the desktop it must
	// not hijack the next message — suspend it until the user asks to resume.
	if ids, err := machine.ListAllStoredUserIDs(); err == nil {
		for _, userID := range ids {
			_ = machine.SuspendWorkflow(userID)
		}
	}
	inst.workflow = &workflowHolder{machine: machine, store: store}
	return inst.workflow.machine, nil
}

// workflowClose drops the machine and its store (instance delete / runtime
// shutdown).
func (rt *Runtime) workflowClose(inst *instance) {
	inst.wfMu.Lock()
	defer inst.wfMu.Unlock()
	if inst.workflow != nil && inst.workflow.store != nil {
		_ = inst.workflow.store.Close()
	}
	inst.workflow = nil
}

// workflowCreate runs for cloud_create_task's workflow_template_id. Params
// that fail validation only defer the slot to the first-message form gate,
// matching the desktop's SubmitForm-at-create contract.
func (rt *Runtime) workflowCreate(inst *instance, taskID, templateID string, params map[string]any, summary string) error {
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		return err
	}
	root, err := jailRoot(inst.dir, inst.meta.Kind)
	if err != nil {
		root = filepath.Join(inst.dir, "workspace")
	}
	state, err := machine.Create(taskID, strings.TrimSpace(templateID), root, summary)
	if err != nil {
		return err
	}
	if len(params) > 0 {
		if err := machine.SubmitForm(taskID, params); err != nil {
			slog.Warn("cloudagent workflow param submit failed",
				slog.String("instance", inst.meta.ID), slog.String("task", taskID),
				slog.String("type", templateID), slog.Any("err", err))
		}
	}
	slog.Info("cloudagent workflow created",
		slog.String("instance", inst.meta.ID), slog.String("task", taskID),
		slog.String("type", templateID), slog.String("workflow", state.ID))
	rt.publishWorkflowState(inst, taskID, workflowLatest(machine, taskID))
	return nil
}

// workflowConfirmClassifier answers the review-gate intent on the instance's
// model. The classify turn must not stream: its host drops token/progress
// publishes and exposes no tools.
func (rt *Runtime) workflowConfirmClassifier(inst *instance) v2.ConfirmClassifier {
	return func(phaseContext, userText string) string {
		cfg := rt.maclawCfg(inst)
		if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		host := &workflowClassifyHost{agentHost: &agentHost{
			rt:      rt,
			inst:    inst,
			turn:    ctx,
			cfg:     cfg,
			started: time.Now(),
		}}
		result := agentruntime.RunAgentTurn(host, v2.BuildConfirmClassifierUserPrompt(phaseContext, userText), nil, rt.httpClient)
		return v2.ParseConfirmClassifierResponse(result.Text)
	}
}

// workflowClassifyHost reuses the agent kernel for the one-shot intent call
// while silencing every viewer-visible callback.
type workflowClassifyHost struct {
	*agentHost
}

func (h *workflowClassifyHost) BuildSystemPrompt(string, bool) string {
	return v2.ConfirmClassifierSystemPrompt
}
func (h *workflowClassifyHost) BuildTools(string) []map[string]interface{} { return nil }
func (h *workflowClassifyHost) OnToken(string)                             {}
func (h *workflowClassifyHost) OnProgress(string)                          {}

// turnRoute is what a message becomes after workflow routing: either a model
// input (normal send, cancel-and-execute, or a phase run with its prompt) or
// a notice answered without an agent loop (form gate, review gate, terminal).
type turnRoute struct {
	modelText   string
	phasePrompt string
	notice      string
	phaseRan    bool
}

// workflowRoute is the cloud mirror of the desktop's routeWithWorkflowV2 +
// handleWorkflowV2Action. HandleInput decides; this maps the action onto this
// instance's turn model: phases run through the same agent loop with the
// phase prompt injected, gates answer directly.
func (rt *Runtime) workflowRoute(inst *instance, taskID, text string) *turnRoute {
	route := &turnRoute{modelText: text}
	// Same guard as workflowSnapshot: until the store file exists no workflow
	// can be active, so ordinary chat never pays for opening it.
	inst.wfMu.Lock()
	loaded := inst.workflow != nil
	inst.wfMu.Unlock()
	if !loaded {
		if _, err := os.Stat(workflowStorePath(inst.dir)); err != nil {
			return route
		}
	}
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		slog.Warn("cloudagent workflow machine unavailable",
			slog.String("instance", inst.meta.ID), slog.Any("err", err))
		return route
	}
	// A transient SQLITE_BUSY on the shared workflow db must not fall through
	// to the model — a review-gate "确认" would then run as an ordinary
	// question while the phase still waits. One retry covers the flake the
	// store's busy_timeout can still lose to a checkpoint race.
	var hr *v2.HandleResult
	for attempt := 0; attempt < 2; attempt++ {
		hr, err = machine.HandleInput(taskID, text)
		if err == nil {
			break
		}
		if attempt == 0 {
			time.Sleep(150 * time.Millisecond)
		}
	}
	if err != nil {
		slog.Warn("cloudagent workflow HandleInput failed",
			slog.String("instance", inst.meta.ID), slog.String("task", taskID), slog.Any("err", err))
		return route
	}
	if hr == nil {
		return route
	}
	phaseName := func() string {
		if hr.Phase != nil && strings.TrimSpace(hr.Phase.Name) != "" {
			return hr.Phase.Name
		}
		if hr.State != nil {
			if p := hr.State.ActivePhase(); p != nil && strings.TrimSpace(p.Name) != "" {
				return p.Name
			}
		}
		return ""
	}
	switch hr.Action {
	case v2.ActionRunPhase, v2.ActionModify:
		phase := hr.Phase
		if phase == nil {
			rt.publishWorkflowState(inst, taskID, hr.State)
			route.notice = "工作流已完成"
			return route
		}
		// Same gate as the desktop: a phase with an unsubmitted form shows the
		// form instead of running — advanceLocked returns RunPhase for the next
		// phase too, so this check lives here, not only on ShowForm.
		if phase.InputSchema != nil && phase.FormData == nil {
			rt.publishWorkflowState(inst, taskID, hr.State)
			route.notice = fmt.Sprintf("工作流已进入「%s」阶段，请在下方工作流面板填写输入信息后提交。", phaseName())
			return route
		}
		route.phasePrompt = v2.BuildPhasePrompt(hr.State)
		if strings.TrimSpace(hr.ModifyHint) != "" {
			// The desktop appends the feedback to the phase prompt for BOTH
			// phase kinds — the user-slot text alone drops it on doc phases.
			if workflowIsArtifactPhase(phase) {
				route.phasePrompt += "\n\n## 用户修改意见\n\n" + hr.ModifyHint + "\n\n请根据以上修改意见重新生成最终产物文件，不要改为输出文档或需求澄清。"
			} else {
				route.phasePrompt += "\n\n## 用户修改意见\n\n" + hr.ModifyHint + "\n\n请根据以上修改意见重新生成本阶段文档。"
			}
		}
		route.modelText = workflowPhaseUserText(phase, hr.ModifyHint)
		route.phaseRan = true
		rt.publishWorkflowState(inst, taskID, hr.State)
		return route
	case v2.ActionShowForm:
		rt.publishWorkflowState(inst, taskID, hr.State)
		name := phaseName()
		if name == "" {
			name = "当前"
		}
		route.notice = fmt.Sprintf("工作流已进入「%s」阶段，请在下方工作流面板填写输入信息后提交。", name)
		return route
	case v2.ActionConfirmed:
		rt.publishWorkflowState(inst, taskID, hr.State)
		route.notice = "所有阶段已完成！工作流结束。"
		return route
	case v2.ActionCancelled:
		rt.publishWorkflowState(inst, taskID, hr.State)
		route.notice = "工作流已取消"
		return route
	case v2.ActionCancelAndExecute:
		rt.publishWorkflowState(inst, taskID, hr.State)
		if hr.State != nil && strings.TrimSpace(hr.State.Summary) != "" {
			route.modelText = hr.State.Summary
		}
		return route
	case v2.ActionReviewPending:
		rt.publishWorkflowState(inst, taskID, hr.State)
		name := phaseName()
		if name == "" {
			name = "当前"
		}
		route.notice = fmt.Sprintf("「%s」阶段的产出正在等待你的处理：回复「确认」进入下一阶段，回复修改意见重新生成，或回复「取消」结束工作流。", name)
		return route
	}
	return route
}

// workflowPhaseUserText mirrors the desktop's workflowV2PhaseUserRequest +
// buildFormDataInlinedUserText: the phase instruction rides the user slot so
// weak models cannot skip it, and submitted form fields are inlined for the
// same reason.
func workflowPhaseUserText(phase *v2.Phase, modifyHint string) string {
	if phase == nil {
		return "请执行当前工作流阶段。"
	}
	artifact := workflowIsArtifactPhase(phase)
	if modifyHint != "" {
		if artifact {
			return fmt.Sprintf("立即执行「%s」产物生成：系统提示中已提供所有前序阶段的权威内容，直接据此生成并发送最终文件。不要询问主题、受众、页数或要点；不要搜索项目目录、PDF、记忆或历史对话来补充上下文；不要输出计划或 Markdown 文档。必须调用适当的生成工具（PPT 阶段优先运行 pptx-generator），成功后发送最终 .pptx 文件。\n\n用户修改意见：%s。直接重新生成并发送最终文件。", phase.Name, modifyHint)
		}
		return fmt.Sprintf("请根据修改意见重新生成「%s」的完整文档。直接输出完整内容。", phase.Name)
	}
	if artifact {
		return fmt.Sprintf("立即执行「%s」产物生成：系统提示中已提供所有前序阶段的权威内容，直接据此生成并发送最终文件。不要询问主题、受众、页数或要点；不要搜索项目目录、PDF、记忆或历史对话来补充上下文；不要输出计划或 Markdown 文档。必须调用适当的生成工具（PPT 阶段优先运行 pptx-generator），成功后发送最终 .pptx 文件。", phase.Name)
	}
	if phase.FormData != nil && len(phase.FormData) > 0 {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("我已通过表单提交了「%s」阶段的全部输入信息，具体如下：\n\n", phase.Name))
		sb.WriteString(v2.RenderFormDataFields(phase, false))
		sb.WriteString("\n请直接基于以上信息生成本阶段完整文档。")
		sb.WriteString("如果信息中包含文件路径，请直接用 read_file 或 bash 读取该文件内容后再生成文档。")
		return sb.String()
	}
	return fmt.Sprintf("请现在生成「%s」阶段的完整文档内容。不要引用或指向之前的对话，直接在本次回复中输出完整文档。", phase.Name)
}

func workflowIsArtifactPhase(phase *v2.Phase) bool {
	return phase != nil && (phase.Kind == v2.PhaseKindArtifactGeneration || phase.MutationScope == v2.MutationScopeArtifact)
}

// workflowRecordOutput lands the phase turn's answer on the machine. A
// failed or cancelled turn keeps the phase Running so the next message
// re-runs it, which is the same gate the desktop leaves behind.
func (rt *Runtime) workflowRecordOutput(inst *instance, taskID, output string) {
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		return
	}
	if err := machine.RecordOutput(taskID, output); err != nil {
		slog.Warn("cloudagent workflow RecordOutput failed",
			slog.String("instance", inst.meta.ID), slog.String("task", taskID), slog.Any("err", err))
		return
	}
	rt.publishWorkflowState(inst, taskID, workflowLatest(machine, taskID))
}

// submitWorkflowForm backs cloud_submit_workflow_form: store the values on
// the gated phase, publish the new state, then continue through the normal
// send path so queueing, routing and the phase prompt all apply — the same
// boundary the desktop crosses by auto-sending "继续" after a submit.
func (rt *Runtime) submitWorkflowForm(ctx context.Context, inst *instance, payload turnBody) (json.RawMessage, error) {
	taskID := strings.TrimSpace(payload.TaskID)
	if taskID == "" {
		return nil, errors.New("缺少任务")
	}
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		return nil, err
	}
	values := payload.Values
	if len(values) == 0 {
		return nil, errors.New("表单内容为空")
	}
	cleaned := make(map[string]any, len(values))
	for key, value := range values {
		if strings.HasPrefix(key, "_") && key != "_agent_view_variant" {
			continue
		}
		cleaned[key] = value
	}
	if err := machine.SubmitForm(taskID, cleaned); err != nil {
		return nil, err
	}
	rt.publishWorkflowState(inst, taskID, workflowLatest(machine, taskID))
	next := payload
	next.Text = "继续"
	return rt.send(ctx, inst, next)
}

// workflowLatest loads the task's workflow regardless of status. GetActive
// filters to StatusActive, which would blank the panel the moment a flow
// completes or is cancelled — the desktop keeps that board visible.
func workflowLatest(machine *v2.StateMachine, taskID string) *v2.WorkflowState {
	state, _ := machine.GetStore().Load(taskID)
	return state
}

// workflowSnapshotOf serializes a workflow for the wire — the same field
// names the desktop's workflow:phase_update payload carries, plus the active
// phase's input schema so the viewer can render the form gate.
func workflowSnapshotOf(state *v2.WorkflowState) map[string]any {
	if state == nil {
		return nil
	}
	active := state.ActivePhase()
	activeID := ""
	awaitingForm := false
	if active != nil {
		activeID = active.ID
		awaitingForm = active.InputSchema != nil && active.FormData == nil && state.Status == v2.StatusActive
	}
	phases := make([]map[string]any, len(state.Phases))
	for i, p := range state.Phases {
		expectsDoc := p.NeedsConfirm
		if p.ID == activeID && awaitingForm {
			expectsDoc = false
		}
		phases[i] = map[string]any{
			"id":               p.ID,
			"name":             p.Name,
			"status":           string(p.Status),
			"needs_confirm":    p.NeedsConfirm,
			"expects_document": expectsDoc,
		}
	}
	payload := map[string]any{
		"id":            state.ID,
		"status":        string(state.Status),
		"type":          state.Type,
		"current_phase": activeID,
		"phases":        phases,
		"project_path":  state.ProjectPath,
	}
	if state.Suspended {
		payload["suspended"] = true
	}
	if awaitingForm {
		payload["awaiting_form"] = true
		payload["form_schema"] = active.InputSchema
	}
	if active != nil && active.Status == v2.PhaseWaitingConfirm {
		payload["pending_review"] = true
	}
	return payload
}

// workflowSnapshot answers this task's workflow for the watch snapshot. A
// task that never ran a workflow does not pay for opening the store — the
// file only exists after the first workflow-bearing op created it.
func (rt *Runtime) workflowSnapshot(inst *instance, taskID string) map[string]any {
	inst.wfMu.Lock()
	loaded := inst.workflow != nil
	inst.wfMu.Unlock()
	if !loaded {
		if _, err := os.Stat(workflowStorePath(inst.dir)); err != nil {
			return nil
		}
	}
	machine, err := rt.workflowMachine(inst)
	if err != nil {
		return nil
	}
	return workflowSnapshotOf(workflowLatest(machine, taskID))
}

// publishWorkflowState streams the task's workflow snapshot. requestID can be
// empty — the frame is task state, not part of a round, and an empty request
// id keeps the reducer from treating it as one.
func (rt *Runtime) publishWorkflowState(inst *instance, taskID string, state *v2.WorkflowState) {
	rt.publish(inst, taskID, "", "workflow_state", "", map[string]any{"workflow": workflowSnapshotOf(state)})
}

// send dispatches a synthetic send through the same path a user message
// takes — queueing behind a running turn included. The form submit's
// continuation and any future host-driven resume share it.
func (rt *Runtime) send(ctx context.Context, inst *instance, payload turnBody) (json.RawMessage, error) {
	if inst.meta.Kind == KindCoding {
		return rt.sendCoding(ctx, inst, payload)
	}
	return rt.sendSession(inst, payload)
}
