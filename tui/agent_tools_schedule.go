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
	"github.com/RapidAI/CodeClaw/corelib/llm"
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
		case "list_targets":
			return app.toolListScheduleDeliveryTargets(args)
		default:
			return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update/list_targets）", action)
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
	case "list_targets", "list_groups", "list_im_targets", "list_delivery_targets":
		return "list_targets"
	default:
		return s
	}
}

func (app *TUIApp) toolCreateScheduledTask(args map[string]interface{}) string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	// Prefer task_action; never treat CRUD verb "action" as work content.
	taskAction := stringVal(args, "task_action")
	if taskAction == "" {
		if a := stringVal(args, "action"); a != "" {
			switch normalizeScheduleAction(a) {
			case "create", "list", "delete", "update", "list_targets":
				// leftover CRUD verb — ignore
			default:
				taskAction = a
			}
		}
	}
	if name == "" || taskAction == "" {
		return "缺少 name 或 task_action 参数（task_action 为到点要执行的内容）"
	}
	hour := tuiScheduleIntArg(args, "hour", -1)
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := tuiScheduleIntArg(args, "minute", 0)
	dow := tuiScheduleIntArg(args, "day_of_week", -1)
	dom := tuiScheduleIntArg(args, "day_of_month", -1)
	intervalMin := tuiScheduleIntArg(args, "interval_minutes", 0)

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
	if d, err := parseTUIScheduleDelivery(args); err != nil {
		return err.Error()
	} else if d != nil {
		if err := app.resolveScheduleDelivery(d); err != nil {
			return err.Error()
		}
		t.Delivery = d
	}

	id, err := app.scheduledTaskManager.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}

	if task := app.scheduledTaskManager.Get(id); task != nil {
		extra := ""
		if s := scheduler.SummarizeDelivery(task.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		if task.NextRunAt != nil {
			return fmt.Sprintf("定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s%s", id, name, taskAction, task.NextRunAt.Format("2006-01-02 15:04"), extra)
		}
		return fmt.Sprintf("定时任务已创建（ID: %s）%s", id, extra)
	}
	return fmt.Sprintf("定时任务已创建（ID: %s）", id)
}

func (app *TUIApp) toolListScheduleDeliveryTargets(args map[string]interface{}) string {
	channel := stringVal(args, "channel")
	if channel == "" {
		channel = stringVal(args, "platform")
	}
	query := stringVal(args, "query")
	if query == "" {
		query = stringVal(args, "group_name")
	}
	if query == "" {
		query = stringVal(args, "name")
	}
	text, err := app.listScheduleDeliveryTargets(channel, query)
	if err != nil {
		return fmt.Sprintf("查询投递目标失败: %s", err.Error())
	}
	return text
}

// parseTUIScheduleDelivery supports delivery object or group_id/user_id/group_name shorthand.
// group_name is resolved via the channel catalog (same as desktop).
func parseTUIScheduleDelivery(args map[string]interface{}) (*scheduler.TaskDelivery, error) {
	if args == nil {
		return nil, nil
	}
	if raw, ok := args["delivery"]; ok {
		if raw == nil {
			return nil, nil
		}
		d, err := scheduler.ParseDeliveryFromAny(raw)
		if err != nil {
			return nil, fmt.Errorf("delivery 配置无效: %w", err)
		}
		return d, nil
	}
	groupID := strings.TrimSpace(stringVal(args, "group_id"))
	if groupID == "" {
		groupID = strings.TrimSpace(stringVal(args, "delivery_group_id"))
	}
	userID := strings.TrimSpace(stringVal(args, "user_id"))
	if userID == "" {
		userID = strings.TrimSpace(stringVal(args, "delivery_user_id"))
	}
	groupName := strings.TrimSpace(stringVal(args, "group_name"))
	if groupName == "" {
		groupName = strings.TrimSpace(stringVal(args, "delivery_group_name"))
	}
	if groupID == "" && userID == "" && groupName == "" {
		return nil, nil
	}
	channel := strings.TrimSpace(stringVal(args, "channel"))
	if channel == "" {
		channel = strings.TrimSpace(stringVal(args, "delivery_channel"))
	}
	if channel == "" {
		channel = scheduler.DeliveryChannelLansenger
	}
	d := &scheduler.TaskDelivery{Enabled: true, Channel: channel, On: scheduler.DeliveryOnSuccess}
	if b, ok := args["fail_on_error"].(bool); ok {
		d.FailOnError = b
	}
	if userID != "" && groupID == "" && groupName == "" {
		d.Targets = []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: userID}}
	} else {
		tg := scheduler.DeliveryTarget{
			Kind: scheduler.DeliveryKindGroup, GroupID: groupID, GroupName: groupName,
		}
		if mentions := stringVal(args, "mention_user_ids"); mentions != "" {
			for _, p := range strings.FieldsFunc(mentions, func(r rune) bool {
				return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\n' || r == '\t'
			}) {
				p = strings.TrimSpace(p)
				if p != "" {
					tg.MentionUserIDs = append(tg.MentionUserIDs, p)
				}
			}
		}
		if b, ok := args["mention_all"].(bool); ok && b {
			tg.MentionAll = true
		}
		d.Targets = []scheduler.DeliveryTarget{tg}
	}
	d.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return d, nil
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
	b.WriteString(fmt.Sprintf("定时任务列表（共 %d 个）：\n\n", len(tasks)))
	for i, t := range tasks {
		status := t.Status
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   ID: %s\n   操作: %s\n   下次执行: %s\n   已执行: %d 次",
			i+1, status, t.Name, t.ID, scheduler.TruncateStr(t.Action, 80), next, t.RunCount))
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			b.WriteString("\n   推送: " + s)
		}
		b.WriteString("\n\n")
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
	return "定时任务已删除"
}

func (app *TUIApp) toolUpdateScheduledTask(args map[string]interface{}) string {
	if app.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	if ta := stringVal(args, "task_action"); ta != "" {
		args["action"] = ta
	} else if a := stringVal(args, "action"); a != "" && normalizeScheduleAction(a) != a {
		// action is a CRUD verb (update/create/…) — do not persist as work content.
		delete(args, "action")
	}
	var current *scheduler.TaskDelivery
	if cur := app.scheduledTaskManager.Get(id); cur != nil {
		current = cur.Delivery
	}
	if err := scheduler.PrepareDeliveryForUpdate(current, args, func(a map[string]interface{}) (*scheduler.TaskDelivery, error) {
		d, err := parseTUIScheduleDelivery(a)
		if err != nil {
			return nil, err
		}
		if d == nil {
			return nil, nil
		}
		if err := app.resolveScheduleDelivery(d); err != nil {
			return nil, err
		}
		return d, nil
	}); err != nil {
		return err.Error()
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
		extra := ""
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		return fmt.Sprintf("定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s%s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next, extra)
	}
	return "定时任务已更新"
}

func tuiScheduleIntArg(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return def
	}
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

		var runErr error
		if result.Error != "" {
			runErr = fmt.Errorf("%s", result.Error)
			log.Printf("[TUI-ScheduledTask] task %s failed: %s", task.Name, result.Error)
		} else {
			log.Printf("[TUI-ScheduledTask] task %s completed: %s", task.Name, scheduler.TruncateStr(result.Text, 200))
		}
		// Wrap with ctx abort so ShouldDeliver can push partial text on timeout.
		runErr = scheduler.AnnotateRunErrWithContext(ctx, runErr)

		resultText := result.Text
		// Structured channel delivery (same model as desktop GUI / MaClawSrv).
		// Also attempt on timeout when partial text exists.
		if delErr := app.deliverScheduledTaskResult(task, resultText, runErr); delErr != nil {
			log.Printf("[TUI-ScheduledTask] delivery failed: %v", delErr)
			resultText, runErr = scheduler.MergeDeliveryOutcome(task.Delivery, resultText, runErr, delErr)
		}
		if runErr != nil {
			return resultText, runErr
		}
		return resultText, nil
	}
}

// tuiSchedulerCallbacks implements agent.LoopCallbacks for background scheduled
// task execution. It's a minimal implementation that doesn't stream to the UI.
type tuiSchedulerCallbacks struct {
	app       *TUIApp
	ctx       context.Context
	activeLLM tuiActiveLLM
}

func (c *tuiSchedulerCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.activeLLM.get(c.app.llmConfig)
}

func (c *tuiSchedulerCallbacks) RouteTurn(userText string) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	if c == nil || c.app == nil {
		return corelib.MaclawLLMConfig{}, agent.RouteDecision{}, false
	}
	cfg, d, ok := c.app.routeTurn(userText, llm.ClassifyHints{})
	if ok {
		c.activeLLM.set(cfg)
	}
	return cfg, d, ok
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
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	args["_ctx"] = ctx
	return c.app.toolRegistry.ExecuteCtx(ctx, name, args)
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

func (c *tuiSchedulerCallbacks) EarlyStop() (bool, string, string) {
	if c == nil || c.app == nil {
		return false, "", ""
	}
	return c.app.earlyStopBudget()
}

func (c *tuiSchedulerCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	if c == nil || c.app == nil {
		return
	}
	c.app.recordLLMCost(model, inputTokens, outputTokens)
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
