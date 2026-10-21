package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// toolGoal handles the goal management tool. The shared loop dispatch passes
// the turn owner from the call context; this entry remains for callers that
// only have lastUserID.
// Model-facing actions: create, plan, step, complete, get, fail
// System-only transitions: pause, resume (also used by /goal slash commands).
func (h *IMMessageHandler) toolGoal(args map[string]interface{}) string {
	return h.toolGoalForUser(h.lastUserID, args)
}

// toolGoalForUser is the session-scoped goal tool entry point (pure coding tabs
// use desktop-user:<projectPath> as the store key).
func (h *IMMessageHandler) toolGoalForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))

	switch action {
	case "create":
		return h.goalCreateForUser(userID, args)
	case "plan":
		return h.goalPlanForUser(userID, args)
	case "step":
		return h.goalStepForUser(userID, args)
	case "complete":
		return h.goalCompleteForUser(userID, args)
	case "fail":
		return h.goalFailForUser(userID, args)
	case "get":
		return h.goalGetForUser(userID)
	case "pause":
		return h.goalPauseForUser(userID)
	case "resume":
		return h.goalResumeForUser(userID)
	case "cancel", "clear":
		return h.goalClearForUser(userID)
	default:
		return fmt.Sprintf("未知 goal action: %s（模型可用: create/plan/step/complete/fail/get）", action)
	}
}

// goalExecutionOwner is the session that owns the goal store row for this
// tool call. A mailbox owner is the turn that dispatched the call. lastUserID
// remains the fallback for callers that do not carry a turn context.
func goalExecutionOwner(ctx context.Context, lastUserID string) string {
	if ctx != nil {
		if id, ok := ctx.Value(mailboxOwnerContextKey{}).(string); ok {
			if id = strings.TrimSpace(id); id != "" {
				return id
			}
		}
	}
	return strings.TrimSpace(lastUserID)
}

func (h *IMMessageHandler) resolveGoalUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID != "" {
		return userID
	}
	if h != nil {
		return strings.TrimSpace(h.lastUserID)
	}
	return ""
}

func (h *IMMessageHandler) goalCreate(args map[string]interface{}) string {
	return h.goalCreateForUser(h.lastUserID, args)
}

func (h *IMMessageHandler) goalCreateForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()

	// Check for existing active goal
	existing := store.Get(userID)
	if existing != nil && !existing.IsTerminal() {
		return fmt.Sprintf("已有活跃目标（%s）：%s\n如需替换请先取消当前目标（goal action=cancel），或等待当前目标完成。",
			existing.Status, existing.Objective)
	}

	objective, _ := args["objective"].(string)
	if objective == "" {
		return "错误: 缺少 objective 参数（目标描述）"
	}

	var opts []goal.SetOption

	if budget, ok := getIntArg(args, "token_budget"); ok && budget > 0 {
		opts = append(opts, goal.WithTokenBudget(budget))
	}
	if maxTurns, ok := getIntArg(args, "max_turns"); ok && maxTurns > 0 {
		opts = append(opts, goal.WithMaxTurns(maxTurns))
	}
	if criteria := getStringSliceArg(args, "acceptance_criteria"); len(criteria) > 0 {
		opts = append(opts, goal.WithAcceptanceCriteria(criteria))
	}
	if projPath, _ := args["project_path"].(string); projPath != "" {
		opts = append(opts, goal.WithProjectPath(projPath))
	}

	g, err := store.Set(userID, objective, opts...)
	if err != nil {
		return fmt.Sprintf("创建目标失败: %v", err)
	}

	// Pure coding workbench: mirror objective into durable session plan so the
	// multi-turn coding banner stays aligned with /goal.
	h.syncCodingWorkbenchSessionPlanFromGoal(userID, objective)

	// Schedule first continuation turn
	if h.app != nil && h.app.goalContinuation != nil {
		// Note: don't scheduleDelayed here — the post-loop side effects
		// (maybeScheduleGoalContinuation) will handle it after this agent loop
		// finishes. Scheduling here would create a timer that gets immediately
		// replaced by the post-loop schedule anyway.
		h.app.goalContinuation.emitGoalStateChanged(userID, g)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("目标已创建\n"))
	b.WriteString(fmt.Sprintf("目标: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("Goal ID: %s\n", g.GoalID))
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf("Token 预算: %d\n", g.TokenBudget))
	}
	b.WriteString(fmt.Sprintf("最大轮次: %d\n", g.MaxTurns))
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("验收标准:\n")
		for _, c := range g.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("  • %s\n", c))
		}
	}
	b.WriteString("\n系统将自动持续推进此目标，直到目标达成或预算耗尽。")
	return b.String()
}

func (h *IMMessageHandler) goalComplete(args map[string]interface{}) string {
	return h.goalCompleteForUser(h.lastUserID, args)
}

func (h *IMMessageHandler) goalCompleteForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("目标已处于终态（%s），无需再次完成。", g.Status)
	}

	summary, _ := args["summary"].(string)
	summary = strings.TrimSpace(summary)
	if refusal := goal.CompletionRefusal(g, summary); refusal != "" {
		return refusal
	}

	store.UpdateStatus(userID, g.GoalID, goal.StatusComplete, summary)
	h.cancelGoalContinuation(userID)

	// Notify frontend of terminal state
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(userID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}

	updated := store.Get(userID)
	if updated == nil {
		updated = g
	}
	report := goal.FormatCompletionReport(updated, summary)
	if updated.TokenBudget > 0 {
		report += fmt.Sprintf("\nToken 使用: %d / %d", updated.TokensUsed, updated.TokenBudget)
	}
	report += fmt.Sprintf("\n总轮次: %d | 耗时: %ds", updated.TurnsUsed, updated.TimeUsedSeconds)
	return report
}

func (h *IMMessageHandler) goalPlanForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "错误: 当前没有活跃目标"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("错误: 目标已处于终态（%s）", g.Status)
	}
	understanding, _ := args["understanding"].(string)
	steps, err := goal.ParseSteps(args["steps"])
	if err != nil {
		return "错误: " + err.Error()
	}
	criteria := getStringSliceArg(args, "acceptance_criteria")
	if len(criteria) == 0 {
		criteria = goal.ParseStringList(args["acceptance_criteria"])
	}
	if err := store.SavePlan(userID, g.GoalID, understanding, steps, criteria); err != nil {
		return "错误: " + err.Error()
	}
	updated := store.Get(userID)
	h.syncCodingWorkbenchSessionPlanFromGoal(userID, g.Objective)
	if h.app != nil && h.app.goalContinuation != nil && updated != nil {
		h.app.goalContinuation.emitGoalStateChanged(userID, updated)
	}
	return formatGoalPlanSavedText(updated)
}

func (h *IMMessageHandler) goalStepForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "错误: 当前没有活跃目标"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("错误: 目标已处于终态（%s）", g.Status)
	}
	if g.EffectivePhase() != goal.PhaseExecute {
		return "错误: 目标仍在规划阶段，请先调用 goal(action=\"plan\")"
	}
	index, ok := getIntArg(args, "index")
	if !ok || index <= 0 {
		return "错误: 缺少 index 参数"
	}
	statusRaw, _ := args["status"].(string)
	result, _ := args["result"].(string)
	if strings.TrimSpace(result) == "" {
		result, _ = args["reason"].(string)
	}
	if refusal := g.StepReportRefusal(index); refusal != "" {
		return "错误: " + refusal
	}
	if err := store.UpdateStep(userID, g.GoalID, index, goal.StepStatus(statusRaw), result); err != nil {
		return "错误: " + err.Error()
	}
	updated := store.Get(userID)
	if updated == nil {
		return "错误: 步骤更新后目标丢失"
	}
	for _, step := range updated.Steps {
		if step.Index != index {
			continue
		}
		if step.Status == goal.StepFailed {
			reason := fmt.Sprintf("步骤 %d 失败：%s", index, step.Result)
			h.goalFailForUser(userID, map[string]interface{}{"reason": reason})
			return "错误: " + reason
		}
		if h.app != nil && h.app.goalContinuation != nil {
			h.app.goalContinuation.emitGoalStateChanged(userID, updated)
		}
		if step.Status == goal.StepProgress {
			return fmt.Sprintf("步骤 %d 仍在进行：%s\n下一轮继续这一步，不要开始下一步。", index, step.Title)
		}
		reply := fmt.Sprintf("步骤 %d 已完成: %s", index, result)
		if next, ok := updated.CurrentStep(); ok {
			reply += fmt.Sprintf("\n下一步将单独执行步骤 %d：%s。本轮不要开始它。", next.Index, next.Title)
		}
		return reply
	}
	return fmt.Sprintf("步骤 %d 已完成: %s", index, result)
}

func (h *IMMessageHandler) goalFail(args map[string]interface{}) string {
	return h.goalFailForUser(h.lastUserID, args)
}

func (h *IMMessageHandler) goalFailForUser(userID string, args map[string]interface{}) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("目标已处于终态（%s）。", g.Status)
	}

	reason, _ := args["reason"].(string)
	if reason == "" {
		reason = "目标无法完成"
	}

	store.UpdateStatus(userID, g.GoalID, goal.StatusFailed, reason)
	h.cancelGoalContinuation(userID)

	// Notify frontend of terminal state
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(userID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}

	return fmt.Sprintf("目标失败\n目标: %s\n原因: %s\nToken 使用: %d | 总轮次: %d",
		g.Objective, reason, g.TokensUsed, g.TurnsUsed)
}

func (h *IMMessageHandler) goalGet() string {
	return h.goalGetForUser(h.lastUserID)
}

func (h *IMMessageHandler) goalGetForUser(userID string) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "当前没有目标。使用 goal(action=\"create\", objective=\"...\") 创建目标。"
	}

	statusMark := map[goal.Status]string{
		goal.StatusActive:      "[>>]",
		goal.StatusPaused:      "[||]",
		goal.StatusBudgetLimit: "[!!]",
		goal.StatusComplete:    "[OK]",
		goal.StatusFailed:      "[ERR]",
	}

	var b strings.Builder
	icon := statusMark[g.Status]
	if icon == "" {
		icon = "[?]"
	}
	b.WriteString(fmt.Sprintf("%s 当前目标 [%s]\n", icon, g.Status))
	b.WriteString(fmt.Sprintf("目标: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("Goal ID: %s\n", g.GoalID))
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf("Token: %d / %d\n", g.TokensUsed, g.TokenBudget))
	} else {
		b.WriteString(fmt.Sprintf("Token 已用: %d（无限制）\n", g.TokensUsed))
	}
	b.WriteString(fmt.Sprintf("轮次: %d / %d\n", g.TurnsUsed, g.MaxTurns))
	b.WriteString(fmt.Sprintf("耗时: %ds\n", g.TimeUsedSeconds))
	if g.Understanding != "" {
		b.WriteString(fmt.Sprintf("理解: %s\n", g.Understanding))
	}
	b.WriteString(fmt.Sprintf("阶段: %s\n", g.EffectivePhase()))
	if len(g.Steps) > 0 {
		b.WriteString("步骤:\n")
		for _, step := range g.Steps {
			b.WriteString(fmt.Sprintf("  %d. [%s] %s", step.Index, step.Status, step.Title))
			if step.Result != "" {
				b.WriteString(" — " + step.Result)
			}
			b.WriteString("\n")
		}
	}
	if len(g.AcceptanceCriteria) > 0 {
		b.WriteString("验收标准:\n")
		for _, c := range g.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("  • %s\n", c))
		}
	}
	if g.Summary != "" {
		b.WriteString(fmt.Sprintf("备注: %s\n", g.Summary))
	}
	return b.String()
}

func (h *IMMessageHandler) goalPause() string {
	return h.goalPauseForUser(h.lastUserID)
}

func (h *IMMessageHandler) goalPauseForUser(userID string) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.Status != goal.StatusActive {
		return fmt.Sprintf("目标不在活跃状态（当前: %s），无法暂停。", g.Status)
	}
	store.Pause(userID, g.GoalID)
	// The queued continuation must stop with the goal. Cancel after the
	// status flip so a callback that already claimed the timer sees paused
	// and does not dispatch. CancelPending also refunds an unsent plan
	// correction and retires the timer generation.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.CancelPending(userID)
		updated := store.Get(userID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}
	return fmt.Sprintf("目标已暂停: %s\n使用 goal(action=\"resume\") 或发送 /goal resume 恢复。", g.Objective)
}

func (h *IMMessageHandler) goalResume() string {
	return h.goalResumeForUser(h.lastUserID)
}

func (h *IMMessageHandler) goalResumeForUser(userID string) string {
	userID = h.resolveGoalUserID(userID)
	store := h.getGoalStore()
	g := store.Get(userID)
	if g == nil {
		return "当前没有目标。"
	}
	if g.Status != goal.StatusPaused {
		return fmt.Sprintf("目标不在暂停状态（当前: %s），无法恢复。", g.Status)
	}
	store.Resume(userID, g.GoalID)
	// Notify frontend
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(userID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(userID, updated)
		}
	}
	return fmt.Sprintf("目标已恢复: %s\n系统将继续自动推进。", g.Objective)
}

func (h *IMMessageHandler) goalClear() string {
	return h.goalClearForUser(h.lastUserID)
}

func (h *IMMessageHandler) goalClearForUser(userID string) string {
	userID = h.resolveGoalUserID(userID)
	// Drop the queued turn before the row disappears. A leftover timer keeps
	// the run window open and can still claim a generation after clear.
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.CancelPending(userID)
	}
	store := h.getGoalStore()
	cleared := store.Clear(userID)
	if h.app != nil && h.app.goalContinuation != nil && h.app.goalContinuation.store != nil && h.app.goalContinuation.store != store {
		if h.app.goalContinuation.store.Clear(userID) {
			cleared = true
		}
	}
	if h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.emitGoalCleared(userID)
	}
	if cleared {
		return "目标已清除。"
	}
	return "当前没有目标。"
}

func (h *IMMessageHandler) activeContinuationWorkingStateGoal(userID string) string {
	if h == nil {
		return ""
	}
	store := h.getGoalStore()
	if store == nil {
		return ""
	}
	g := store.Get(h.resolveGoalUserID(userID))
	if g == nil || !g.ShouldContinue() {
		return ""
	}
	return strings.TrimSpace(g.Objective)
}

// getGoalStore returns the goal store, lazily initializing if needed.
func (h *IMMessageHandler) getGoalStore() *goal.Store {
	if h.goalStore != nil {
		return h.goalStore
	}
	// Try to get from app's continuation engine
	if h.app != nil && h.app.goalContinuation != nil {
		h.goalStore = h.app.goalContinuation.store
		return h.goalStore
	}
	// Fallback: create standalone store
	dataDir := ""
	if h.app != nil {
		dataDir = h.app.GetDataDir()
		if dataDir != "" {
			dataDir = dataDir + "/goals"
		}
	}
	h.goalStore = goal.NewStore(dataDir)
	return h.goalStore
}

// --- Helpers ---

func (h *IMMessageHandler) cancelGoalContinuation(userID string) {
	if h == nil || h.app == nil || h.app.goalContinuation == nil {
		return
	}
	h.app.goalContinuation.CancelPending(userID)
}

func formatGoalPlanSavedText(g *goal.Goal) string {
	if g == nil {
		return "计划已保存"
	}
	var b strings.Builder
	b.WriteString("计划已保存\n")
	b.WriteString("理解: " + g.Understanding + "\n")
	b.WriteString("步骤:\n")
	for _, step := range g.Steps {
		b.WriteString(fmt.Sprintf("  %d. %s\n", step.Index, step.Title))
	}
	b.WriteString("验收标准:\n")
	for _, criterion := range g.AcceptanceCriteria {
		b.WriteString("  • " + criterion + "\n")
	}
	b.WriteString("下一轮只执行第 1 步。")
	return b.String()
}

func getIntArg(args map[string]interface{}, key string) (int, bool) {
	raw, ok := args[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	}
	return 0, false
}

func getStringSliceArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		var parsed []string
		if json.Unmarshal([]byte(v), &parsed) == nil {
			return parsed
		}
		// Single value as one criterion.
		if strings.TrimSpace(v) != "" {
			return []string{strings.TrimSpace(v)}
		}
	}
	return nil
}
