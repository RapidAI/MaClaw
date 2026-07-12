package agent

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/goal"
)

// ToolGoal handles the goal management tool for the TUI/pipe agent loop.
// Actions: create, complete, fail, get
//
// Note: pause/resume are system-controlled and not exposed to the model.
// The GUI has its own richer toolGoal handler that also handles pause/resume
// and integrates with the continuation engine. This implementation is the
// minimal portable version for corelib consumers (TUI, pipe mode).
//
// UserID scoping: TUI is single-user, so all goal operations use a fixed
// "default" userID. The GUI's im_tool_goal.go uses h.lastUserID for proper
// multi-user isolation. This mirrors how ToolTask uses a single shared
// task.Store without user scoping in TUI.
func ToolGoal(store *goal.Store, args map[string]interface{}) string {
	if store == nil {
		return "目标管理器未初始化"
	}

	action, _ := args["action"].(string)
	action = strings.TrimSpace(strings.ToLower(action))

	switch action {
	case "create":
		return toolGoalCreate(store, args)
	case "complete":
		return toolGoalComplete(store, args)
	case "fail":
		return toolGoalFail(store, args)
	case "get":
		return toolGoalGet(store)
	default:
		return fmt.Sprintf("未知 goal action: %s（支持: create/complete/fail/get）", action)
	}
}

func toolGoalCreate(store *goal.Store, args map[string]interface{}) string {
	objective, _ := args["objective"].(string)
	if objective == "" {
		return "错误: 缺少 objective 参数"
	}

	var opts []goal.SetOption
	if budget, ok := intFromArg(args, "token_budget"); ok && budget > 0 {
		opts = append(opts, goal.WithTokenBudget(budget))
	}
	if maxTurns, ok := intFromArg(args, "max_turns"); ok && maxTurns > 0 {
		opts = append(opts, goal.WithMaxTurns(maxTurns))
	}
	if criteria := stringSliceFromArg(args, "acceptance_criteria"); len(criteria) > 0 {
		opts = append(opts, goal.WithAcceptanceCriteria(criteria))
	}

	g, err := store.Set("default", objective, opts...)
	if err != nil {
		return fmt.Sprintf("创建目标失败: %v", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("目标已创建: %s\n", g.Objective))
	b.WriteString(fmt.Sprintf("Goal ID: %s\n", g.GoalID))
	b.WriteString(fmt.Sprintf("最大轮次: %d", g.MaxTurns))
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf(" | Token 预算: %d", g.TokenBudget))
	}
	return b.String()
}

func toolGoalComplete(store *goal.Store, args map[string]interface{}) string {
	g := store.Get("default")
	if g == nil {
		return "当前没有活跃目标。"
	}
	if g.IsTerminal() {
		return fmt.Sprintf("目标已处于终态（%s）。", g.Status)
	}
	summary, _ := args["summary"].(string)
	if summary == "" {
		summary = "目标已完成"
	}
	store.UpdateStatus("default", g.GoalID, goal.StatusComplete, summary)
	return fmt.Sprintf("目标已完成: %s\n总结: %s\n轮次: %d", g.Objective, summary, g.TurnsUsed)
}

func toolGoalFail(store *goal.Store, args map[string]interface{}) string {
	g := store.Get("default")
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
	store.UpdateStatus("default", g.GoalID, goal.StatusFailed, reason)
	return fmt.Sprintf("目标失败: %s\n原因: %s", g.Objective, reason)
}

func toolGoalGet(store *goal.Store) string {
	g := store.Get("default")
	if g == nil {
		return "当前没有目标。使用 goal(action=\"create\", objective=\"...\") 创建目标。"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("目标 [%s]: %s\n", g.Status, g.Objective))
	if g.TokenBudget > 0 {
		b.WriteString(fmt.Sprintf("Token: %d/%d\n", g.TokensUsed, g.TokenBudget))
	}
	b.WriteString(fmt.Sprintf("轮次: %d/%d | 耗时: %ds", g.TurnsUsed, g.MaxTurns, g.TimeUsedSeconds))
	if g.Summary != "" {
		b.WriteString(fmt.Sprintf("\n备注: %s", g.Summary))
	}
	return b.String()
}

// --- helpers (avoid importing encoding/json for simple arg extraction) ---

func intFromArg(args map[string]interface{}, key string) (int, bool) {
	raw, ok := args[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

func stringSliceFromArg(args map[string]interface{}, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []string
	for _, item := range arr {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
