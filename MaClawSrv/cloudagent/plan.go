package cloudagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// The coding workbench plan mirrors the desktop flow: one planner call judges
// whether a request needs a step list, the plan waits on a confirmation card,
// and the approved run reports progress through update_plan_step. The record
// lives in the book so a watch snapshot (and a srv restart) restores it.

type planStep struct {
	Index       int    `json:"index"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Summary     string `json:"summary,omitempty"`
	UpdatedUnix int64  `json:"updated_unix,omitempty"`
}

const (
	planStatePending  = "pending"  // gate card is up, request is parked
	planStateApproved = "approved" // user confirmed; next turn runs the request
	planStateSkipped  = "skipped"  // user asked to run without the plan
	planStateRejected = "rejected" // user cancelled; next turn only says so
	planStateRunning  = "executing"
	planStateDone     = "done"
	planStateFailed   = "failed"
)

type planRecord struct {
	State       string     `json:"state"`
	Request     string     `json:"request"`
	Restatement string     `json:"restatement"`
	Plan        string     `json:"plan"`
	Steps       []planStep `json:"steps"`
	Revision    int        `json:"revision"`
}

// --- book persistence ---

func (b *book) planLoad(taskID string) (*planRecord, error) {
	var rec planRecord
	var steps string
	err := b.db.QueryRow(`SELECT state, request, restatement, plan, steps, revision FROM plan_state WHERE task_id = ?`, taskID).
		Scan(&rec.State, &rec.Request, &rec.Restatement, &rec.Plan, &steps, &rec.Revision)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(steps), &rec.Steps)
	return &rec, nil
}

func (b *book) planLoadOrNil(taskID string) *planRecord {
	rec, err := b.planLoad(taskID)
	if err != nil {
		return nil
	}
	return rec
}

func (b *book) planSave(taskID string, rec *planRecord) error {
	steps, err := json.Marshal(rec.Steps)
	if err != nil {
		return err
	}
	_, err = b.db.Exec(`INSERT INTO plan_state (task_id, state, request, restatement, plan, steps, revision, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET state = excluded.state, request = excluded.request,
			restatement = excluded.restatement, plan = excluded.plan, steps = excluded.steps,
			revision = excluded.revision, updated_at = excluded.updated_at`,
		taskID, rec.State, rec.Request, rec.Restatement, rec.Plan, string(steps), rec.Revision, formatTS(time.Now()))
	return err
}

func (b *book) planClear(taskID string) error {
	_, err := b.db.Exec(`DELETE FROM plan_state WHERE task_id = ?`, taskID)
	return err
}

// planPendingTask returns the task whose plan gate is still open, newest
// first. A bare send carries no task id, and the pending gate is exactly
// the state where no turn is running — so the running-task fallback in
// sendCoding cannot find it.
func (b *book) planPendingTask() string {
	var taskID string
	_ = b.db.QueryRow(`SELECT task_id FROM plan_state WHERE state = ? ORDER BY updated_at DESC LIMIT 1`,
		planStatePending).Scan(&taskID)
	return taskID
}

// --- plan answers ---

const (
	planAnswerApprove = "approve"
	planAnswerSkip    = "skip"
	planAnswerReject  = "reject"
)

// parsePlanAnswer recognizes the gate card's commands and their typed
// equivalents. Words the commit gate owns (仅这一次/本任务内允许/拒绝) are
// checked elsewhere; 拒绝 doubles as a plan rejection only while a plan is
// actually parked, which is the only caller of this.
func parsePlanAnswer(text string) string {
	norm := strings.Join(strings.Fields(strings.TrimSpace(text)), "")
	norm = strings.TrimRight(norm, "。.!！?？")
	switch norm {
	case "按计划开始", "按计划执行", "按计划实施", "按计划做", "开始执行", "开始吧", "按这个计划":
		return planAnswerApprove
	case "不用计划，直接做", "不用计划,直接做", "不用计划直接做", "不要计划，直接做", "不要计划直接做",
		"跳过计划", "跳过计划，直接做", "跳过计划直接做", "直接做", "直接开始", "不需要计划", "不需要计划，直接做":
		return planAnswerSkip
	case "取消这个计划", "取消计划", "取消", "不要做了", "不用做了", "拒绝":
		return planAnswerReject
	}
	return ""
}

// --- planner (one non-streaming model call per unplanned coding turn) ---

const planPrompt = `你是编程任务的规划器。只输出一个 JSON 对象，不要输出其它内容：
{"restatement":"一句话概括用户要做什么","needs_plan":true,"steps":[{"title":"短标题","summary":"这一步做什么"}]}
规则：
- needs_plan 仅当请求需要多步代码改动、新建项目或跨文件修改时才为 true；回答问题、查看代码、运行已有程序、单点小修改一律为 false。
- needs_plan 为 true 时给出 2 到 6 个步骤，title 不超过 15 个字。
- restatement 用一句话说出要做什么，不要描述过程。`

type planDecision struct {
	Restatement string
	NeedsPlan   bool
	Steps       []planStep
}

// planDecisionFor asks the model once whether this request warrants a plan.
// A failed call fails open (no gate) — the turn then runs directly, the same
// fallback the desktop uses when its classifier is unavailable.
func (rt *Runtime) planDecisionFor(ctx context.Context, inst *instance, cfg corelib.MaclawLLMConfig, text string) planDecision {
	messages := []interface{}{
		map[string]any{"role": "system", "content": planPrompt},
		map[string]any{"role": "user", "content": text},
	}
	resp, err := rt.llmOnce(withInstance(ctx, inst.meta.ID), cfg, messages)
	if err != nil || resp == nil {
		return planDecision{}
	}
	return parsePlanDecision(responseText(resp))
}

// llmOnce is the non-streaming counterpart of the loop's protocol dispatch.
func (rt *Runtime) llmOnce(ctx context.Context, cfg corelib.MaclawLLMConfig, messages []interface{}) (*llm.Response, error) {
	if cfg.IsResponsesAPI() || cfg.IsResponsesWebSocket() {
		return llm.DoResponsesAPIRequest(ctx, cfg, messages, rt.httpClient)
	}
	if strings.EqualFold(cfg.Protocol, "anthropic") {
		return llm.DoAnthropicRequest(ctx, cfg, messages, nil, rt.httpClient)
	}
	return llm.DoOpenAIRequest(ctx, cfg, messages, nil, rt.httpClient)
}

func responseText(resp *llm.Response) string {
	if resp == nil {
		return ""
	}
	for _, choice := range resp.Choices {
		if text := strings.TrimSpace(choice.Message.Content); text != "" {
			return text
		}
	}
	return ""
}

// parsePlanDecision tolerates the model wrapping the object in prose or a
// code fence: it takes the outermost {…} span and accepts a few key aliases.
func parsePlanDecision(text string) planDecision {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return planDecision{}
	}
	var raw struct {
		Restatement   string `json:"restatement"`
		Understanding string `json:"understanding"`
		NeedsPlan     *bool  `json:"needs_plan"`
		Steps         []struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &raw); err != nil {
		return planDecision{}
	}
	decision := planDecision{Restatement: strings.TrimSpace(raw.Restatement)}
	if decision.Restatement == "" {
		decision.Restatement = strings.TrimSpace(raw.Understanding)
	}
	for i, step := range raw.Steps {
		title := strings.TrimSpace(step.Title)
		if title == "" || i >= 6 {
			continue
		}
		decision.Steps = append(decision.Steps, planStep{
			Index:   len(decision.Steps) + 1,
			Title:   title,
			Status:  "pending",
			Summary: strings.TrimSpace(step.Summary),
		})
	}
	if raw.NeedsPlan != nil {
		decision.NeedsPlan = *raw.NeedsPlan && len(decision.Steps) > 0
	} else {
		decision.NeedsPlan = len(decision.Steps) > 0
	}
	return decision
}

func planTextOf(steps []planStep) string {
	var lines []string
	for _, step := range steps {
		lines = append(lines, fmt.Sprintf("T%d %s", step.Index, step.Title))
	}
	return strings.Join(lines, "\n")
}

// --- workbench publication ---

// workbenchPayload is the RemoteWorkbenchState the frontend checklist reads.
// A nil record still emits the empty shape so a cleared plan retracts the bar.
func workbenchPayload(rec *planRecord) map[string]any {
	steps := []map[string]any{}
	if rec != nil {
		for _, step := range rec.Steps {
			row := map[string]any{
				"index":   step.Index,
				"title":   step.Title,
				"status":  step.Status,
				"summary": step.Summary,
			}
			if step.UpdatedUnix > 0 {
				row["updated_unix"] = step.UpdatedUnix
			}
			steps = append(steps, row)
		}
	}
	payload := map[string]any{
		"execution_plan":          "",
		"requirement_restatement": "",
		"pending_approval":        false,
		"steps_revision":          0,
		"step_statuses":           steps,
	}
	if rec == nil {
		return payload
	}
	payload["execution_plan"] = rec.Plan
	payload["requirement_restatement"] = rec.Restatement
	payload["pending_approval"] = rec.State == planStatePending
	payload["steps_revision"] = rec.Revision
	switch rec.State {
	case planStateRunning:
		payload["execution_status"] = "running"
	case planStateFailed:
		payload["execution_status"] = "failed"
	}
	return payload
}

func (rt *Runtime) publishWorkbench(inst *instance, taskID, requestID string, rec *planRecord) {
	rt.publish(inst, taskID, requestID, "workbench_steps", "", map[string]any{"workbench": workbenchPayload(rec)})
}

// workbenchSnapshot is the watch-reply copy — rebuilt from the book so it
// also survives a srv restart, which lanes do not.
func (rt *Runtime) workbenchSnapshot(inst *instance, taskID string) map[string]any {
	if inst == nil || inst.book == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	rec := inst.book.planLoadOrNil(taskID)
	if rec == nil {
		return nil
	}
	return workbenchPayload(rec)
}

// --- gate ---

// planTerminal is the confirmation card parked on the gate response. Its
// commands are the exact strings parsePlanAnswer recognizes.
func planTerminal(taskID string) map[string]any {
	return map[string]any{
		"text":         "我先把这次任务拆成了执行计划，确认后开始实施。",
		"confirmation": map[string]any{"id": "plan-approval;" + taskID},
		"actions": []map[string]string{
			{"label": "开始执行", "command": "按计划开始"},
			{"label": "跳过计划", "command": "不用计划，直接做"},
			{"label": "取消", "command": "取消这个计划"},
		},
	}
}

// routePlanAnswer applies a gate answer before the turn is begun. It returns
// true when the text was a plan decision and the record moved on; the caller
// then runs (or refuses) rec.Request inside the new turn.
func (rt *Runtime) routePlanAnswer(inst *instance, taskID, text string) {
	if inst == nil || inst.book == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	rec := inst.book.planLoadOrNil(taskID)
	if rec == nil || rec.State != planStatePending {
		return
	}
	switch parsePlanAnswer(text) {
	case planAnswerApprove:
		rec.State = planStateApproved
	case planAnswerSkip:
		rec.State = planStateSkipped
	case planAnswerReject:
		rec.State = planStateRejected
	default:
		// A plain message while the gate is open drops the parked plan — the
		// new request plans for itself inside its own turn. Publish the empty
		// payload here: when that request needs no plan, no workbench_steps
		// frame follows, and without this retract the viewer would hold the
		// open gate card forever.
		_ = inst.book.planClear(taskID)
		rt.publishWorkbench(inst, taskID, "", nil)
		return
	}
	_ = inst.book.planSave(taskID, rec)
	// The decided payload already closes the gate on screen —
	// pending_approval flips false — instead of waiting for the turn's own
	// publish a moment later.
	rt.publishWorkbench(inst, taskID, "", rec)
}

// --- update_plan_step ---

func normalizePlanStepStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "in_progress", "started":
		return "running"
	case "passed", "done", "completed", "success":
		return "passed"
	case "failed", "error", "verify_failed":
		return "failed"
	case "skipped", "cancelled", "canceled":
		return "skipped"
	}
	return ""
}

func (rt *Runtime) execUpdatePlanStep(inst *instance, taskID, requestID, argsJSON string) (string, bool) {
	var body struct {
		Index   int    `json:"index"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &body); err != nil || body.Index <= 0 {
		return `请用 {"index":<步骤号>,"status":"running|passed|failed|skipped"}`, true
	}
	status := normalizePlanStepStatus(body.Status)
	if status == "" {
		return "status 只能是 running、passed、failed 或 skipped", true
	}
	rec := inst.book.planLoadOrNil(taskID)
	if rec == nil || rec.State != planStateRunning {
		return "当前没有正在执行的计划", true
	}
	now := time.Now().Unix()
	found := false
	for i := range rec.Steps {
		if rec.Steps[i].Index != body.Index {
			continue
		}
		if status == "running" {
			// One step runs at a time: starting a later one settles the
			// previous running row instead of leaving two spinners.
			for j := range rec.Steps[:i] {
				if rec.Steps[j].Status == "running" {
					rec.Steps[j].Status = "passed"
					rec.Steps[j].UpdatedUnix = now
				}
			}
		}
		rec.Steps[i].Status = status
		if summary := strings.TrimSpace(body.Summary); summary != "" {
			rec.Steps[i].Summary = summary
		}
		rec.Steps[i].UpdatedUnix = now
		found = true
		break
	}
	if !found {
		return fmt.Sprintf("计划里没有 T%d", body.Index), true
	}
	rec.Revision++
	if err := inst.book.planSave(taskID, rec); err != nil {
		return err.Error(), true
	}
	rt.publishWorkbench(inst, taskID, requestID, rec)
	return fmt.Sprintf("已更新 T%d → %s", body.Index, status), false
}

// settlePlan closes the cursor the turn left: a successful reply passes the
// running step, a failed reply marks it failed. Called once the model's turn
// is over so no row spins forever.
func (rt *Runtime) settlePlan(inst *instance, taskID, requestID string, asError bool) {
	if inst == nil || inst.book == nil {
		return
	}
	rec := inst.book.planLoadOrNil(taskID)
	if rec == nil || rec.State != planStateRunning {
		return
	}
	now := time.Now().Unix()
	for i := range rec.Steps {
		switch {
		case rec.Steps[i].Status == "running":
			if asError {
				rec.Steps[i].Status = "failed"
			} else {
				rec.Steps[i].Status = "passed"
			}
		case rec.Steps[i].Status == "pending" && !asError:
			// A finished turn must not leave a stale pending row in a done
			// plan: the model ended the work without touching it, which is a
			// skip, not a step still to come.
			rec.Steps[i].Status = "skipped"
		default:
			continue
		}
		rec.Steps[i].UpdatedUnix = now
	}
	if asError {
		rec.State = planStateFailed
	} else {
		rec.State = planStateDone
	}
	rec.Revision++
	if err := inst.book.planSave(taskID, rec); err != nil {
		return
	}
	rt.publishWorkbench(inst, taskID, requestID, rec)
}
