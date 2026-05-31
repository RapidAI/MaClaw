package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// newManageScheduleHandler creates the TUI-side manage_schedule tool handler.
func newManageScheduleHandler(app *TUIApp) agent.ToolHandler {
	return func(args map[string]interface{}) string {
		actionText := stringVal(args, "action")
		// The tool definition has both "action" (CRUD verb) and "task_action"
		// (what the scheduled task should do). When both are present, "action"
		// is the CRUD verb. When LLM mistakenly puts the CRUD verb in
		// "task_action", swap them.
		if ta := stringVal(args, "task_action"); ta != "" {
			normalized := normalizeScheduleAction(ta)
			if normalized == "create" || normalized == "list" || normalized == "delete" || normalized == "update" {
				// LLM put the CRUD verb in task_action — swap.
				args["task_action"] = actionText
				actionText = ta
			}
		}
		action := normalizeScheduleAction(actionText)
		switch action {
		case "create":
			return app.toolCreateScheduledTask(args)
		case "list":
			return app.toolListScheduledTasks()
		case "delete":
			return app.toolDeleteScheduledTask(args)
		case "update":
			return app.toolUpdateScheduledTask(args)
		default:
			return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update）", action)
		}
	}
}

func normalizeScheduleAction(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "create", "add", "new":
		return "create"
	case "list", "ls", "show", "all":
		return "list"
	case "delete", "del", "remove", "rm":
		return "delete"
	case "update", "edit", "modify":
		return "update"
	default:
		return s
	}
}

func (app *TUIApp) toolCreateScheduledTask(args map[string]interface{}) string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	// The task's action content comes from "task_action" (preferred) or "action".
	taskAction := stringVal(args, "task_action")
	if taskAction == "" {
		taskAction = stringVal(args, "action")
	}
	if name == "" || taskAction == "" {
		return "缺少 name 或 task_action 参数"
	}
	hour := -1
	if v, ok := args["hour"].(float64); ok {
		hour = int(v)
	}
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := 0
	if v, ok := args["minute"].(float64); ok {
		minute = int(v)
	}
	dow := -1
	if v, ok := args["day_of_week"].(float64); ok {
		dow = int(v)
	}
	dom := -1
	if v, ok := args["day_of_month"].(float64); ok {
		dom = int(v)
	}
	intervalMin := 0
	if v, ok := args["interval_minutes"].(float64); ok {
		intervalMin = int(v)
	}

	t := scheduler.ScheduledTask{
		Name:            name,
		Action:          taskAction,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dow,
		DayOfMonth:      dom,
		IntervalMinutes: intervalMin,
		StartDate:       stringVal(args, "start_date"),
		EndDate:         stringVal(args, "end_date"),
		TaskType:        stringVal(args, "task_type"),
	}

	id, err := app.scheduledTaskManager.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}

	if task := app.scheduledTaskManager.Get(id); task != nil && task.NextRunAt != nil {
		return fmt.Sprintf("✅ 定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s", id, name, taskAction, task.NextRunAt.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("✅ 定时任务已创建（ID: %s）", id)
}

func (app *TUIApp) toolListScheduledTasks() string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	tasks := app.scheduledTaskManager.List()
	if len(tasks) == 0 {
		return "当前没有定时任务"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 定时任务列表（共 %d 个）：\n\n", len(tasks)))
	for i, t := range tasks {
		status := t.Status
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   ID: %s\n   操作: %s\n   下次执行: %s\n   已执行: %d 次\n\n",
			i+1, status, t.Name, t.ID, scheduler.TruncateStr(t.Action, 80), next, t.RunCount))
	}
	return b.String()
}

func (app *TUIApp) toolDeleteScheduledTask(args map[string]interface{}) string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	name := stringVal(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		err = app.scheduledTaskManager.Delete(id)
	} else {
		err = app.scheduledTaskManager.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	return "✅ 定时任务已删除"
}

func (app *TUIApp) toolUpdateScheduledTask(args map[string]interface{}) string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	err := app.scheduledTaskManager.Update(id, args)
	if err != nil {
		return fmt.Sprintf("更新失败: %s", err.Error())
	}
	if t := app.scheduledTaskManager.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("✅ 定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next)
	}
	return "✅ 定时任务已更新"
}

// buildTUIScheduledTaskExecutor creates a TaskExecutor for the TUI that uses
// agent.RunLoop to execute scheduled tasks in the background.
func (app *TUIApp) buildScheduledTaskExecutor() scheduler.TaskExecutor {
	return func(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
		log.Printf("[TUI-ScheduledTask] firing task %s: %s", task.Name, scheduler.TruncateStr(task.Action, 100))

		actionText := fmt.Sprintf("[自动执行定时任务] 这是系统自动触发的定时任务，必须在一次执行中完成，不会有用户交互。请直接执行以下操作并返回结果：\n%s", task.Action)

		// Use a dedicated callbacks instance (no UI streaming for background tasks).
		cb := &tuiSchedulerCallbacks{app: app, ctx: ctx}
		result := agent.RunLoop(cb, actionText, nil, nil)

		if result.Error != "" {
			log.Printf("[TUI-ScheduledTask] task %s failed: %s", task.Name, result.Error)
			return result.Text, fmt.Errorf("%s", result.Error)
		}
		log.Printf("[TUI-ScheduledTask] task %s completed: %s", task.Name, scheduler.TruncateStr(result.Text, 200))
		return result.Text, nil
	}
}

// tuiSchedulerCallbacks implements agent.LoopCallbacks for background scheduled
// task execution. It's a minimal implementation that doesn't stream to the UI.
type tuiSchedulerCallbacks struct {
	app *TUIApp
	ctx context.Context
}

func (c *tuiSchedulerCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *tuiSchedulerCallbacks) GetMaxIterations() int {
	// Scheduled tasks may need many iterations. Use the configured value
	// (or default 300) — same as the main agent loop.
	return config.EffectiveMaxIterations(c.app.appConfig.MaclawAgentMaxIterations)
}

func (c *tuiSchedulerCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	deps := c.app.buildSystemPromptDeps()
	return agent.BuildSystemPrompt(deps, userText, isFirstTurn)
}

func (c *tuiSchedulerCallbacks) BuildTools(userText string) []map[string]interface{} {
	return c.app.toolRegistry.BuildDefinitions()
}

func (c *tuiSchedulerCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := parseJSON(argsJSON, &args); err != nil {
		return fmt.Sprintf("参数解析失败: %s", err.Error())
	}
	return c.app.toolRegistry.Execute(name, args)
}

func (c *tuiSchedulerCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.app == nil {
		return true
	}
	return c.app.isWorkflowToolAllowedTUI(name)
}

func (c *tuiSchedulerCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.app == nil {
		return true, ""
	}
	return c.app.isWorkflowToolCallAllowedTUI(name, argsJSON)
}

func (c *tuiSchedulerCallbacks) OnToken(delta string) {
	// No UI streaming for background tasks.
}

func (c *tuiSchedulerCallbacks) OnProgress(text string) {
	log.Printf("[TUI-ScheduledTask] progress: %s", text)
}

func (c *tuiSchedulerCallbacks) OnToolCall(name string) {
	log.Printf("[TUI-ScheduledTask] tool call: %s", name)
}

func (c *tuiSchedulerCallbacks) OnToolResult(name string) {}

func (c *tuiSchedulerCallbacks) ShouldStop() bool {
	return c.ctx.Err() != nil
}

// stringVal extracts a string value from args map.
func stringVal(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// parseJSON is a helper to unmarshal JSON args.
func parseJSON(data string, v interface{}) error {
	if strings.TrimSpace(data) == "" {
		return nil
	}
	return json.Unmarshal([]byte(data), v)
}
