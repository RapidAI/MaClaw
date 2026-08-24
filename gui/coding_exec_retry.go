package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// codingExecCheckpoint is resume/retry state for coding implementation after
// cancel or partial failure. Kept in-process and mirrored to disk (no secrets).
type codingExecCheckpoint struct {
	UserID     string `json:"user_id,omitempty"`
	WorkflowID string `json:"workflow_id,omitempty"`
	// WorkflowPhaseID binds a completed Runtime projection to the exact active
	// phase that produced it. Startup repair must never advance a different
	// implementation phase merely because it finds a completed opaque task ID.
	WorkflowPhaseID string             `json:"workflow_phase_id,omitempty"`
	IsRemote        bool               `json:"is_remote"`
	Tasks           []*v2.TaskItem     `json:"tasks"`
	Results         []v2.TaskRunResult `json:"results"`
	RequirementsCtx string             `json:"requirements_ctx,omitempty"`
	DesignCtx       string             `json:"design_ctx,omitempty"`
	ProjectPath     string             `json:"project_path,omitempty"`
	// Remote SSH (non-secret)
	RemoteSessionID string `json:"remote_session_id,omitempty"`
	RemoteWorkDir   string `json:"remote_work_dir,omitempty"`
	RemoteHost      string `json:"remote_host,omitempty"`
	RemoteUser      string `json:"remote_user,omitempty"`
	RemotePort      int    `json:"remote_port,omitempty"`
	Cancelled       bool   `json:"cancelled,omitempty"`
	// ProjectionPending marks a completed Ledger execution whose Workflow V2
	// phase output has not yet been durably projected. It is intentionally a
	// presentation-only marker: recovery may retry only RecordOutput, never an
	// executor, model call, tool invocation, or old command.
	ProjectionPending bool      `json:"projection_pending,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
}

const (
	codingExecRetryActionFailed = "failed" // 重试失败
	codingExecRetryActionResume = "resume" // 继续执行（失败+取消跳过）
	// codingExecRetryActionReviewChildren starts a deliberately fresh parent
	// attempt after Runtime has durably delivered all read-only child results.
	// It is not a retry and must never reuse the old parent transcript.
	codingExecRetryActionReviewChildren = "review_children"

	codingExecCheckpointFileName = ".coding_exec_checkpoint.json"
	codingExecCheckpointMaxAge   = 7 * 24 * time.Hour
)

func (h *IMMessageHandler) storeCodingExecCheckpoint(userID string, cp codingExecCheckpoint) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	// Stabilize task list against later parser/mutation of shared pointers.
	cp.Tasks = cloneV2TaskItems(cp.Tasks)
	if len(cp.Results) > 0 {
		cp.Results = append([]v2.TaskRunResult(nil), cp.Results...)
	}
	cp.UserID = userID
	cp.UpdatedAt = time.Now()
	h.codingExecCheckpoint.Store(userID, cp)
	persistCodingExecCheckpointToDisk(userID, cp)
}

func (h *IMMessageHandler) loadCodingExecCheckpoint(userID string) (codingExecCheckpoint, bool) {
	if h == nil {
		return codingExecCheckpoint{}, false
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return codingExecCheckpoint{}, false
	}
	raw, ok := h.codingExecCheckpoint.Load(userID)
	if ok {
		cp, ok := raw.(codingExecCheckpoint)
		if ok && len(cp.Tasks) > 0 {
			if !codingExecCheckpointStillValid(cp) {
				h.clearCodingExecCheckpoint(userID)
				return codingExecCheckpoint{}, false
			}
			return cp, true
		}
	}
	// Memory miss (e.g. process restart): try durable disk snapshot.
	cp, ok := loadCodingExecCheckpointFromDisk(userID, "")
	if !ok || len(cp.Tasks) == 0 {
		return codingExecCheckpoint{}, false
	}
	if !codingExecCheckpointStillValid(cp) {
		// Drop pending markers + all candidate disk paths (memory may be empty).
		projectPath := cp.ProjectPath
		h.clearCodingExecCheckpoint(userID)
		if projectPath != "" {
			deleteCodingExecCheckpointFromDisk(userID, projectPath)
		}
		return codingExecCheckpoint{}, false
	}
	// Re-hydrate session map so subsequent loads are cheap.
	h.codingExecCheckpoint.Store(userID, cp)
	return cp, true
}

func (h *IMMessageHandler) clearCodingExecCheckpoint(userID string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	projectPath := ""
	if raw, ok := h.codingExecCheckpoint.Load(userID); ok {
		if cp, ok := raw.(codingExecCheckpoint); ok {
			projectPath = cp.ProjectPath
		}
	}
	// Prefer session-owner project path so project-local disk files are removed
	// even when the in-memory checkpoint is already gone.
	if projectPath == "" {
		projectPath = projectPathFromSessionOwnerID(userID)
	}
	h.codingExecCheckpoint.Delete(userID)
	h.pendingCodingExecRetryAction.Delete(userID)
	deleteCodingExecCheckpointFromDisk(userID, projectPath)
}

// codingExecCurrentLang returns the UI language (same source as main shell).
func codingExecCurrentLang() string {
	lang, _ := agentViewCurrentLang.Load().(string)
	return lang
}

// codingExecText localizes user-facing coding-exec copy to match App language.
func codingExecText(en, zhHans, zhHant string) string {
	return unfinishedSlotText(codingExecCurrentLang(), en, zhHans, zhHant)
}

// isCodingExecCancelError reports whether a task error denotes user/system cancel.
func isCodingExecCancelError(err string) bool {
	e := strings.ToLower(strings.TrimSpace(err))
	if e == "" {
		return false
	}
	// Prefer whole-token cancel markers so we don't match unrelated text like
	// "cancellation fee" from non-runner sources when possible.
	if e == "cancelled" || e == "canceled" || e == "cancel" {
		return true
	}
	if strings.Contains(e, "cancelled") || strings.Contains(e, "canceled") {
		return true
	}
	// Chinese cancel markers (TaskRunner normally uses English "cancelled").
	return strings.Contains(err, "取消")
}

// parseCodingExecRetryCommand returns retry action for user text, or "".
// Accepts en / zh-Hans / zh-Hant phrases so action buttons stay parseable.
func parseCodingExecRetryCommand(text string) string {
	t := strings.TrimSpace(strings.ToLower(text))
	if t == "" {
		return ""
	}
	// Normalize full-width and common phrases.
	t = strings.ReplaceAll(t, "　", " ")
	raw := strings.TrimSpace(text)

	// Failed-only
	failedHints := []string{
		"重试失败", "重试失败任务", "重跑失败", "重跑失败任务",
		"重試失敗", "重試失敗任務", "重跑失敗", "重跑失敗任務",
		"retry failed", "retry failures", "retry failed tasks",
	}
	for _, h := range failedHints {
		if strings.EqualFold(raw, h) || t == strings.ToLower(h) {
			return codingExecRetryActionFailed
		}
	}
	// Loose phrase match only for short, command-like messages (avoid hijacking
	// long chat that merely mentions "retry" and "fail").
	if utf8.RuneCountInString(raw) <= 24 &&
		(strings.Contains(raw, "重试") || strings.Contains(raw, "重試") || strings.Contains(t, "retry")) &&
		(strings.Contains(raw, "失败") || strings.Contains(raw, "失敗") || strings.Contains(t, "fail")) {
		return codingExecRetryActionFailed
	}

	// Resume incomplete (failed + cancelled skips)
	resumeHints := []string{
		"继续执行", "继续编码", "继续远程编码", "继续任务",
		"繼續執行", "繼續編碼", "繼續遠程編碼", "繼續遠端編碼", "繼續任務",
		"resume coding", "continue execution", "continue coding",
	}
	for _, h := range resumeHints {
		if strings.EqualFold(raw, h) || t == strings.ToLower(h) {
			return codingExecRetryActionResume
		}
	}

	// Review a durable Runtime child handoff. Keep this command specific: plain
	// "continue" remains owned by ordinary workflow confirmation.
	childReviewHints := []string{
		"review child results", "review children", "continue after child results",
		"审阅子任务结果", "审查子任务结果", "查看子任务结果", "继续处理子任务结果",
		"審閱子任務結果", "審查子任務結果", "查看子任務結果", "繼續處理子任務結果",
	}
	for _, h := range childReviewHints {
		if strings.EqualFold(raw, h) || t == strings.ToLower(h) {
			return codingExecRetryActionReviewChildren
		}
	}
	return ""
}

// Localized command strings used by action buttons (must remain parseable).
func codingExecCmdRetryFailed() string {
	return codingExecText("retry failed", "重试失败", "重試失敗")
}

func codingExecCmdResume() string {
	return codingExecText("continue coding", "继续执行", "繼續執行")
}

func codingExecCmdReviewChildResults() string {
	return codingExecText("review child results", "审阅子任务结果", "審閱子任務結果")
}

func codingExecCmdCancelWorkflow() string {
	return codingExecText("cancel workflow", "取消工作流", "取消工作流程")
}

// codingExecStatusLabel localizes TaskRunStatus for user-facing reports.
func codingExecStatusLabel(status v2.TaskRunStatus) string {
	switch status {
	case v2.TaskPassed:
		return codingExecText("passed", "通过", "通過")
	case v2.TaskFailed:
		return codingExecText("failed", "失败", "失敗")
	case v2.TaskSkipped:
		return codingExecText("skipped", "跳过", "跳過")
	default:
		if s := strings.TrimSpace(string(status)); s != "" {
			return s
		}
		return codingExecText("unknown", "未知", "未知")
	}
}

func incompleteTaskItemsFromResults(tasks []*v2.TaskItem, results []v2.TaskRunResult) []*v2.TaskItem {
	if len(tasks) == 0 {
		return nil
	}
	byIndex := make(map[int]v2.TaskRunResult, len(results))
	for _, r := range results {
		byIndex[r.TaskIndex] = r
	}
	var out []*v2.TaskItem
	for _, t := range tasks {
		if t == nil {
			continue
		}
		r, ok := byIndex[t.Index]
		if !ok || r.Status == "" {
			// Never ran (e.g. cancel before start of this slot).
			out = append(out, t)
			continue
		}
		switch r.Status {
		case v2.TaskFailed:
			out = append(out, t)
		case v2.TaskSkipped:
			// Resume cancelled work, but not permanent "dependency not met" skips.
			if isCodingExecCancelError(r.Error) {
				out = append(out, t)
			}
		}
	}
	return out
}

// childHandoffTaskItemsFromResults returns only Runtime-marked handoffs. A
// generic skipped task is never eligible: it may be a dependency skip,
// cancellation, or approval boundary rather than a delivered child result.
func childHandoffTaskItemsFromResults(tasks []*v2.TaskItem, results []v2.TaskRunResult) []*v2.TaskItem {
	if len(tasks) == 0 || len(results) == 0 {
		return nil
	}
	byIndex := make(map[int]v2.TaskRunResult, len(results))
	for _, result := range results {
		byIndex[result.TaskIndex] = result
	}
	var out []*v2.TaskItem
	for _, task := range tasks {
		if task == nil {
			continue
		}
		result, ok := byIndex[task.Index]
		if ok && result.RuntimeHandoff && strings.TrimSpace(result.RuntimeTaskID) != "" {
			out = append(out, task)
		}
	}
	return out
}

// tasksForSubsetRerun clones tasks and clears DependsOn so TaskRunner can re-run a
// subset. Dependencies were already satisfied when those tasks first executed
// (or were ready when cancelled); keeping DependsOn would skip them because the
// dependency tasks are not in the subset result set.
func tasksForSubsetRerun(tasks []*v2.TaskItem) []*v2.TaskItem {
	out := cloneV2TaskItems(tasks)
	for _, t := range out {
		if t != nil {
			t.DependsOn = nil
		}
	}
	return out
}

func countTaskRunStatuses(results []v2.TaskRunResult) (passed, failed, skipped int) {
	for _, r := range results {
		switch r.Status {
		case v2.TaskPassed:
			passed++
		case v2.TaskFailed:
			failed++
		case v2.TaskSkipped:
			skipped++
		}
	}
	return
}

// allCodingTasksPassed reports whether every non-nil task has a matching
// TaskPassed result. Used instead of "failed==0 && skipped==0" so a cancel
// that races after the last task still advances the workflow, and so empty
// results never look like success when tasks remain.
func allCodingTasksPassed(tasks []*v2.TaskItem, results []v2.TaskRunResult) bool {
	if len(tasks) == 0 {
		return false
	}
	byIndex := make(map[int]v2.TaskRunStatus, len(results))
	for _, r := range results {
		byIndex[r.TaskIndex] = r.Status
	}
	any := false
	for _, t := range tasks {
		if t == nil {
			continue
		}
		any = true
		if byIndex[t.Index] != v2.TaskPassed {
			return false
		}
	}
	return any
}

func codingExecResumeGuidance(cp codingExecCheckpoint, isRemote bool) string {
	_ = isRemote
	var b strings.Builder
	if cp.Cancelled {
		b.WriteString(codingExecText(
			"Coding execution paused (cancelled by user).\n",
			"编码执行已暂停（用户取消）。\n",
			"編碼執行已暫停（使用者取消）。\n",
		))
	} else {
		b.WriteString(codingExecText(
			"Coding execution did not fully pass.\n",
			"编码执行未全部通过。\n",
			"編碼執行未全部通過。\n",
		))
	}
	b.WriteString(codingExecText("You can:\n", "你可以：\n", "你可以：\n"))
	fmt.Fprintf(&b, codingExecText(
		"- Send **%s** — re-run only failed tasks\n",
		"- 发送 **%s** — 仅重跑失败的任务\n",
		"- 傳送 **%s** — 僅重跑失敗的任務\n",
	), codingExecCmdRetryFailed())
	fmt.Fprintf(&b, codingExecText(
		"- Send **%s** — re-run failed and skipped/incomplete tasks\n",
		"- 发送 **%s** — 重跑失败与已跳过/未完成的任务\n",
		"- 傳送 **%s** — 重跑失敗與已跳過/未完成的任務\n",
	), codingExecCmdResume())
	if len(childHandoffTaskItemsFromResults(cp.Tasks, cp.Results)) > 0 {
		fmt.Fprintf(&b, codingExecText(
			"- Send **%s** — start a fresh review using delivered child findings\n",
			"- 发送 **%s** — 使用已交付的子任务结论启动新的审阅\n",
			"- 傳送 **%s** — 使用已交付的子任務結論啟動新的審閱\n",
		), codingExecCmdReviewChildResults())
	}
	fmt.Fprintf(&b, codingExecText(
		"- Send **%s** — end the coding workflow\n",
		"- 发送 **%s** — 结束当前编程工作流\n",
		"- 傳送 **%s** — 結束目前程式設計工作流\n",
	), codingExecCmdCancelWorkflow())
	return b.String()
}

func formatCodingExecCompletedUserText(report string) string {
	return strings.TrimSpace(report)
}

func formatCodingExecIncompleteUserText(cp codingExecCheckpoint, isRemote bool, report string) string {
	guidance := strings.TrimSpace(codingExecResumeGuidance(cp, isRemote))
	report = strings.TrimSpace(report)
	switch {
	case guidance == "":
		return report
	case report == "":
		return guidance
	default:
		return guidance + "\n\n" + report
	}
}

func formatCodingExecProjectionPendingUserText(report string) string {
	note := strings.TrimSpace(codingExecText(
		"Coding execution completed and was saved safely. The workflow phase update is pending and will be retried as metadata only; no coding task will be run again.",
		"编码执行已安全完成并持久化。工作流阶段更新待补齐，将仅重试元数据写入，不会再次执行编码任务。",
		"編碼執行已安全完成並持久化。工作流程階段更新待補齊，將僅重試中繼資料寫入，不會再次執行編碼任務。",
	))
	report = strings.TrimSpace(report)
	if report == "" {
		return note
	}
	return note + "\n\n" + report
}

// codingExecResumeActions builds clickable chat actions for partial/cancelled runs.
func codingExecResumeActions(cp codingExecCheckpoint) []IMResponseAction {
	failedN := len(failedTaskItemsFromResults(cp.Tasks, cp.Results))
	incompleteN := len(incompleteTaskItemsFromResults(cp.Tasks, cp.Results))
	childHandoffN := len(childHandoffTaskItemsFromResults(cp.Tasks, cp.Results))
	var actions []IMResponseAction
	if failedN > 0 {
		cmd := codingExecCmdRetryFailed()
		actions = append(actions, IMResponseAction{
			Label:   cmd,
			Command: cmd,
			Style:   "primary",
		})
	}
	if incompleteN > 0 {
		style := "default"
		if failedN == 0 {
			style = "primary"
		}
		cmd := codingExecCmdResume()
		actions = append(actions, IMResponseAction{
			Label:   cmd,
			Command: cmd,
			Style:   style,
		})
	}
	if childHandoffN > 0 {
		cmd := codingExecCmdReviewChildResults()
		actions = append(actions, IMResponseAction{
			Label:   cmd,
			Command: cmd,
			Style:   "primary",
		})
	}
	cmdCancel := codingExecCmdCancelWorkflow()
	actions = append(actions, IMResponseAction{
		Label:   cmdCancel,
		Command: cmdCancel,
		Style:   "danger",
	})
	return actions
}

// tryQueueCodingExecRetryCommand handles 重试失败 / 继续执行 while a checkpoint exists.
// Returns nil when the message is not a retry command OR there is no checkpoint
// (so ordinary chat is not stolen by "重试失败" in unrelated sessions).
func (h *IMMessageHandler) tryQueueCodingExecRetryCommand(userID, text string) *workflowIMRouteResult {
	if h == nil {
		return nil
	}
	action := parseCodingExecRetryCommand(text)
	if action == "" {
		return nil
	}
	cp, ok := h.loadCodingExecCheckpoint(userID)
	if !ok || len(cp.Tasks) == 0 {
		// Only claim the phrase when a checkpoint exists; otherwise let chat handle it.
		return nil
	}
	if !h.codingExecCheckpointUsable(userID, cp) {
		// Stale after workflow cancel/restart/new run — drop and leave chat free.
		log.Printf("[coding-exec-retry] dropping unusable checkpoint user=%s workflow=%s", userID, cp.WorkflowID)
		h.clearCodingExecCheckpoint(userID)
		return nil
	}
	var targets []*v2.TaskItem
	switch action {
	case codingExecRetryActionFailed:
		targets = failedTaskItemsFromResults(cp.Tasks, cp.Results)
		if len(targets) == 0 {
			cmdResume := codingExecCmdResume()
			cmdCancel := codingExecCmdCancelWorkflow()
			return &workflowIMRouteResult{
				Response: &IMAgentResponse{
					Text: codingExecText(
						fmt.Sprintf("No failed tasks to retry.\nIf tasks were cancelled/skipped, send **%s**.", cmdResume),
						fmt.Sprintf("没有失败任务可重试。\n若有被取消/跳过的任务，请发送 **%s**。", cmdResume),
						fmt.Sprintf("沒有失敗任務可重試。\n若有被取消/跳過的任務，請傳送 **%s**。", cmdResume),
					),
					Actions: []IMResponseAction{
						{Label: cmdResume, Command: cmdResume, Style: "primary"},
						{Label: cmdCancel, Command: cmdCancel, Style: "danger"},
					},
				},
			}
		}
	case codingExecRetryActionResume:
		targets = incompleteTaskItemsFromResults(cp.Tasks, cp.Results)
		if len(targets) == 0 {
			cmdCancel := codingExecCmdCancelWorkflow()
			return &workflowIMRouteResult{
				Response: &IMAgentResponse{
					Text: codingExecText(
						fmt.Sprintf("No incomplete tasks. If everything passed, send “continue” to advance acceptance, or **%s** to end.", cmdCancel),
						fmt.Sprintf("没有未完成的任务。若全部已通过，可发送「继续」推进验收，或「%s」结束。", cmdCancel),
						fmt.Sprintf("沒有未完成的任務。若全部已通過，可傳送「繼續」推進驗收，或「%s」結束。", cmdCancel),
					),
					Actions: []IMResponseAction{
						{Label: cmdCancel, Command: cmdCancel, Style: "danger"},
					},
				},
			}
		}
	case codingExecRetryActionReviewChildren:
		targets = childHandoffTaskItemsFromResults(cp.Tasks, cp.Results)
		if len(targets) == 0 {
			return &workflowIMRouteResult{Response: &IMAgentResponse{Text: codingExecText(
				"No Runtime child-result handoff is ready for review.",
				"没有可审阅的 Runtime 子任务结果交接。",
				"沒有可審閱的 Runtime 子任務結果交接。",
			)}}
		}
		// Continue to the normal execution path. It revalidates the durable
		// Runtime handoff before starting any parent attempt.
	default:
		return nil
	}

	// Prevent pure sticky coding from intercepting this SubAgent turn.
	h.clearPendingPureCodingTemplateExecution(userID)
	h.pendingCodingExecRetryAction.Store(userID, action)
	h.pendingV2SubAgentExecution.Store(userID, true)
	h.workflowAgentLoopMarker.Store(userID, true)
	log.Printf("[coding-exec-retry] queued action=%s user=%s targets=%d remote=%v", action, userID, len(targets), cp.IsRemote)

	// Do not set Response — entry_context returns early on Response and would
	// skip the agent loop / progress stream. Only arm markers for SubAgent path.
	return &workflowIMRouteResult{
		WorkflowAgentLoop: true,
		WorkflowDocPhase:  false,
	}
}

// cloneV2TaskItems shallow-copies v2 task structs so the checkpoint is stable.
func cloneV2TaskItems(tasks []*v2.TaskItem) []*v2.TaskItem {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]*v2.TaskItem, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		cp := *t
		if len(t.Files) > 0 {
			cp.Files = append([]string(nil), t.Files...)
		}
		if len(t.DependsOn) > 0 {
			cp.DependsOn = append([]int(nil), t.DependsOn...)
		}
		out = append(out, &cp)
	}
	return out
}

// --- Durable checkpoint (disk) ---

func codingExecCheckpointFilePath(userID, projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath != "" {
		return filepath.Join(projectPath, codingExecCheckpointFileName)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return ""
	}
	sum := sha1.Sum([]byte(userID))
	return filepath.Join(corelib.MaclawBaseDir(), "data", "coding_exec", hex.EncodeToString(sum[:])+".json")
}

func codingExecCheckpointStillValid(cp codingExecCheckpoint) bool {
	if len(cp.Tasks) == 0 {
		return false
	}
	if cp.UpdatedAt.IsZero() {
		return true
	}
	return time.Since(cp.UpdatedAt) <= codingExecCheckpointMaxAge
}

// codingExecCheckpointMatchesActive reports whether a checkpoint still belongs
// to the user's active coding implementation phase. Stale checkpoints (wrong
// workflow id, non-coding type, non-execution phase) must not claim chat phrases.
func codingExecCheckpointMatchesActive(cp codingExecCheckpoint, state *v2.WorkflowState) bool {
	if len(cp.Tasks) == 0 || !codingExecCheckpointStillValid(cp) {
		return false
	}
	if state == nil {
		// After cancel/complete there is no active workflow — checkpoint is stale.
		// Legacy checkpoints without WorkflowID also require an active execution phase.
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(state.Type), "coding") {
		return false
	}
	if !state.IsExecutionPhase() {
		return false
	}
	if id := strings.TrimSpace(cp.WorkflowID); id != "" && id != strings.TrimSpace(state.ID) {
		return false
	}
	return true
}

// codingExecCheckpointUsable loads the active workflow (when available) and
// checks age + workflow/phase binding. When no workflow engine is wired (unit
// tests with a bare handler), only age/task validity is enforced.
func (h *IMMessageHandler) codingExecCheckpointUsable(userID string, cp codingExecCheckpoint) bool {
	if len(cp.Tasks) == 0 || !codingExecCheckpointStillValid(cp) {
		return false
	}
	if h == nil {
		return true
	}
	wf := h.getWorkflowV2()
	if wf == nil || wf.machine == nil {
		return true
	}
	state := wf.machine.GetActive(strings.TrimSpace(userID))
	return codingExecCheckpointMatchesActive(cp, state)
}

// repairCompletedCodingWorkflowProjections is invoked during GUI startup for
// the narrow crash window after the Ledger committed a successful coding run
// but before Workflow V2 saved its phase output. It deliberately knows
// nothing about subagents, tools, SSH sessions, or executor callbacks.
func (a *App) repairCompletedCodingWorkflowProjections(wf *workflowV2State) {
	if a == nil || wf == nil || wf.machine == nil {
		return
	}
	store := a.ensureCodingRuntimeStore()
	if store == nil {
		log.Printf("[coding-runtime] skip completed workflow projection repair: ledger unavailable")
		return
	}
	userIDs, err := wf.machine.ListAllStoredUserIDs()
	if err != nil {
		log.Printf("[coding-runtime] list workflows for completed projection repair failed: %v", err)
		return
	}
	for _, userID := range userIDs {
		state, err := wf.store.Load(userID)
		if err != nil || state == nil {
			continue
		}
		cp, ok := loadCodingExecCheckpointFromDisk(userID, state.ProjectPath)
		if !ok || !cp.ProjectionPending || !codingExecCheckpointStillValid(cp) || !codingExecCheckpointMatchesActive(cp, state) || !allCodingTasksPassed(cp.Tasks, cp.Results) || !completedCodingRuntimeResults(store, state, cp) {
			continue
		}
		report := formatTaskRunResultsReportEx(cp.Results, false)
		if err := wf.machine.RecordOutput(userID, report); err != nil {
			log.Printf("[coding-runtime] completed workflow projection still pending: user=%s workflow=%s err=%v", userID, state.ID, err)
			continue
		}
		deleteCodingExecCheckpointFromDisk(userID, cp.ProjectPath)
		log.Printf("[coding-runtime] repaired completed workflow projection without executor replay: user=%s workflow=%s", userID, state.ID)
	}
}

// completedCodingRuntimeResults proves that the presentation checkpoint is
// backed by terminal Ledger facts for this exact active workflow, phase, mode,
// and workspace. A missing/mismatched ID fails closed: startup leaves the
// marker for recovery/diagnosis instead of using a stale UI result to advance
// a different workflow phase or project.
func completedCodingRuntimeResults(store codingruntime.Store, state *v2.WorkflowState, cp codingExecCheckpoint) bool {
	if store == nil || state == nil || len(cp.Results) == 0 {
		return false
	}
	phase := state.ActivePhase()
	if phase == nil || strings.TrimSpace(cp.WorkflowID) != strings.TrimSpace(state.ID) || strings.TrimSpace(cp.WorkflowPhaseID) == "" || strings.TrimSpace(cp.WorkflowPhaseID) != strings.TrimSpace(phase.ID) {
		return false
	}
	mode, projectRef := "local", strings.TrimSpace(cp.ProjectPath)
	if cp.IsRemote {
		mode, projectRef = "remote", strings.TrimSpace(cp.RemoteWorkDir)
	}
	if projectRef == "" {
		return false
	}
	for _, result := range cp.Results {
		taskID := strings.TrimSpace(result.RuntimeTaskID)
		if taskID == "" {
			return false
		}
		task, err := store.GetTask(taskID)
		if err != nil || task == nil || task.Status != codingruntime.TaskCompleted || strings.TrimSpace(task.WorkflowID) != strings.TrimSpace(state.ID) || strings.TrimSpace(task.PhaseID) != strings.TrimSpace(phase.ID) || !strings.EqualFold(strings.TrimSpace(task.Mode), mode) || strings.TrimSpace(task.ProjectRef) != projectRef {
			return false
		}
	}
	return true
}

func persistCodingExecCheckpointToDisk(userID string, cp codingExecCheckpoint) {
	path := codingExecCheckpointFilePath(userID, cp.ProjectPath)
	if path == "" {
		return
	}
	// Never write secrets — checkpoint only has non-secret remote coords.
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		log.Printf("[coding-exec-retry] marshal checkpoint failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("[coding-exec-retry] mkdir checkpoint dir failed: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("[coding-exec-retry] write checkpoint failed: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		// Windows: rename over existing can fail; remove destination then retry.
		// Do not delete tmp first — otherwise the retry has nothing to rename.
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			log.Printf("[coding-exec-retry] rename checkpoint failed: %v", err2)
			_ = os.Remove(tmp)
		}
	}
}

func loadCodingExecCheckpointFromDisk(userID, projectPathHint string) (codingExecCheckpoint, bool) {
	// Prefer project-local file when known; also try user-keyed fallback + session owner path.
	seen := map[string]bool{}
	var candidates []string
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}
	add(codingExecCheckpointFilePath(userID, projectPathHint))
	add(codingExecCheckpointFilePath(userID, projectPathFromSessionOwnerID(userID)))
	add(codingExecCheckpointFilePath(userID, ""))

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		var cp codingExecCheckpoint
		if err := json.Unmarshal(data, &cp); err != nil {
			log.Printf("[coding-exec-retry] corrupt checkpoint %s: %v", path, err)
			continue
		}
		if strings.TrimSpace(cp.UserID) != "" && strings.TrimSpace(cp.UserID) != strings.TrimSpace(userID) {
			continue
		}
		if len(cp.Tasks) == 0 {
			continue
		}
		return cp, true
	}
	return codingExecCheckpoint{}, false
}

func deleteCodingExecCheckpointFromDisk(userID, projectPath string) {
	seen := map[string]bool{}
	for _, path := range []string{
		codingExecCheckpointFilePath(userID, projectPath),
		codingExecCheckpointFilePath(userID, ""),
	} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		_ = os.Remove(path)
	}
	if projectPath == "" {
		if p := projectPathFromSessionOwnerID(userID); p != "" {
			_ = os.Remove(codingExecCheckpointFilePath(userID, p))
		}
	}
}
