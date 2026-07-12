package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// toolGoal handles the goal management tool.
// Model-facing actions: create, complete, get, fail
// System-only transitions: pause, resume (not exposed to model but handled here
// for internal /goal slash command dispatch).
func (h *IMMessageHandler) toolGoal(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))

	switch action {
	case "create":
		return h.goalCreate(args)
	case "complete":
		return h.goalComplete(args)
	case "fail":
		return h.goalFail(args)
	case "get":
		return h.goalGet()
	case "pause":
		return h.goalPause()
	case "resume":
		return h.goalResume()
	case "cancel", "clear":
		return h.goalClear()
	default:
		return fmt.Sprintf("未知 goal action: %s（模型可用: create/complete/fail/get）", action)
	}
}

func (h *IMMessageHandler) goalCreate(args map[string]interface{}) string {
	store := h.getGoalStore()

	// Check for existing active goal
	existing := store.Get(h.lastUserID)
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

	g, err := store.Set(h.lastUserID, objective, opts...)
	if err != nil {
		return fmt.Sprintf("创建目标失败: %v", err)
	}

	// Schedule first continuation turn
	if h.app != nil && h.app.goalContinuation != nil {
		// Note: don't scheduleDelayed here — the post-loop side effects
		// (maybeScheduleGoalContinuation) will handle it after this agent loop
		// finishes. Scheduling here would create a timer that gets immediately
		// replaced by the post-loop schedule anyway.
		h.app.goalContinuation.emitGoalStateChanged(h.lastUserID, g)
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
	store := h.getGoalStore()
	g := store.Get(h.lastUserID)
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("目标已处于终态（%s），无需再次完成。", g.Status)
	}

	summary, _ := args["summary"].(string)
	if summary == "" {
		summary = "目标已完成"
	}

	store.UpdateStatus(h.lastUserID, g.GoalID, goal.StatusComplete, summary)

	// Notify frontend of terminal state
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(h.lastUserID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(h.lastUserID, updated)
		}
	}

	var b strings.Builder
	b.WriteString("目标已完成\n")
	b.WriteString(fmt.Sprintf("目标: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("总结: %s\n", summary))
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf("Token 使用: %d / %d\n", g.TokensUsed, g.TokenBudget))
	}
	b.WriteString(fmt.Sprintf("总轮次: %d | 耗时: %ds", g.TurnsUsed, g.TimeUsedSeconds))
	return b.String()
}

func (h *IMMessageHandler) goalFail(args map[string]interface{}) string {
	store := h.getGoalStore()
	g := store.Get(h.lastUserID)
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

	store.UpdateStatus(h.lastUserID, g.GoalID, goal.StatusFailed, reason)

	// Notify frontend of terminal state
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(h.lastUserID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(h.lastUserID, updated)
		}
	}

	return fmt.Sprintf("目标失败\n目标: %s\n原因: %s\nToken 使用: %d | 总轮次: %d",
		g.Objective, reason, g.TokensUsed, g.TurnsUsed)
}

func (h *IMMessageHandler) goalGet() string {
	store := h.getGoalStore()
	g := store.Get(h.lastUserID)
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
	store := h.getGoalStore()
	g := store.Get(h.lastUserID)
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.Status != goal.StatusActive {
		return fmt.Sprintf("目标不在活跃状态（当前: %s），无法暂停。", g.Status)
	}
	store.Pause(h.lastUserID, g.GoalID)
	// Notify frontend
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(h.lastUserID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(h.lastUserID, updated)
		}
	}
	return fmt.Sprintf("目标已暂停: %s\n使用 goal(action=\"resume\") 或发送 /goal resume 恢复。", g.Objective)
}

func (h *IMMessageHandler) goalResume() string {
	store := h.getGoalStore()
	g := store.Get(h.lastUserID)
	if g == nil {
		return "当前没有目标。"
	}
	if g.Status != goal.StatusPaused {
		return fmt.Sprintf("目标不在暂停状态（当前: %s），无法恢复。", g.Status)
	}
	store.Resume(h.lastUserID, g.GoalID)
	// Notify frontend
	if h.app != nil && h.app.goalContinuation != nil {
		updated := store.Get(h.lastUserID)
		if updated != nil {
			h.app.goalContinuation.emitGoalStateChanged(h.lastUserID, updated)
		}
	}
	return fmt.Sprintf("目标已恢复: %s\n系统将继续自动推进。", g.Objective)
}

func (h *IMMessageHandler) goalClear() string {
	store := h.getGoalStore()
	if store.Clear(h.lastUserID) {
		return "目标已清除。"
	}
	return "当前没有目标。"
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
		// Single value
		if v != "" {
			return []string{v}
		}
	}
	return nil
}
