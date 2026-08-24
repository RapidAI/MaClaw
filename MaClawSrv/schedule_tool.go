package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// newSrvManageScheduleHandler builds manage_schedule for CoreAgentExecutor.
func newSrvManageScheduleHandler(svc *agentservice.Service, mgr *scheduler.Manager) func(args map[string]interface{}) string {
	return func(args map[string]interface{}) string {
		if mgr == nil {
			return "定时任务管理器未初始化"
		}
		actionText := stringArg(args, "action")
		if ta := stringArg(args, "task_action"); ta != "" {
			normalized := normalizeSrvScheduleAction(ta)
			if normalized == "create" || normalized == "list" || normalized == "delete" || normalized == "update" || normalized == "list_targets" {
				args["task_action"] = actionText
				actionText = ta
			}
		}
		switch normalizeSrvScheduleAction(actionText) {
		case "create":
			return srvToolCreateScheduledTask(svc, mgr, args)
		case "list":
			return srvToolListScheduledTasks(mgr)
		case "delete":
			return srvToolDeleteScheduledTask(mgr, args)
		case "update":
			return srvToolUpdateScheduledTask(svc, mgr, args)
		case "list_targets":
			return srvToolListScheduleDeliveryTargets(svc, args)
		default:
			return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update/list_targets）", actionText)
		}
	}
}

func normalizeSrvScheduleAction(s string) string {
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

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func srvScheduleIntArg(args map[string]interface{}, key string, def int) int {
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
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		if f, err := n.Float64(); err == nil {
			return int(f)
		}
	case string:
		raw := strings.TrimSpace(n)
		if i, err := strconv.Atoi(raw); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return int(f)
		}
	}
	return def
}

func srvScheduleBoolArg(args map[string]interface{}, key string, def bool) bool {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case bool:
		return n
	case float64:
		return n != 0
	case int:
		return n != 0
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i != 0
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(n)) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return def
}

func srvToolListScheduleDeliveryTargets(svc *agentservice.Service, args map[string]interface{}) string {
	channel := scheduler.DefaultDeliveryChannel(firstNonEmpty(
		stringArg(args, "channel"),
		stringArg(args, "platform"),
	))
	query := firstNonEmpty(
		stringArg(args, "query"),
		stringArg(args, "group_name"),
		stringArg(args, "name"),
	)
	text, err := listSrvScheduleDeliveryTargets(svc, channel, query)
	if err != nil {
		return fmt.Sprintf("查询投递目标失败: %s", err.Error())
	}
	return text
}

func parseSrvScheduleDelivery(args map[string]interface{}) (*scheduler.TaskDelivery, error) {
	if args == nil {
		return nil, nil
	}
	if raw, ok := args["delivery"]; ok {
		if raw == nil {
			return nil, nil
		}
		if m, ok := raw.(map[string]interface{}); ok {
			if len(m) == 0 {
				return nil, nil
			}
			if en, ok := m["enabled"].(bool); ok && !en {
				return nil, nil
			}
		}
		d, err := scheduler.ParseDeliveryFromAny(raw)
		if err != nil {
			return nil, fmt.Errorf("delivery 配置无效: %w", err)
		}
		if d != nil {
			return d, nil
		}
	}
	groupID := firstNonEmpty(stringArg(args, "group_id"), stringArg(args, "delivery_group_id"))
	userID := firstNonEmpty(stringArg(args, "user_id"), stringArg(args, "delivery_user_id"))
	groupName := firstNonEmpty(stringArg(args, "group_name"), stringArg(args, "delivery_group_name"))
	if groupID == "" && userID == "" && groupName == "" {
		return nil, nil
	}
	channel := firstNonEmpty(stringArg(args, "channel"), stringArg(args, "delivery_channel"))
	if channel == "" {
		channel = scheduler.DeliveryChannelLansenger
	}
	d := &scheduler.TaskDelivery{Enabled: true, Channel: channel, On: scheduler.DeliveryOnSuccess}
	if _, ok := args["fail_on_error"]; ok {
		d.FailOnError = srvScheduleBoolArg(args, "fail_on_error", false)
	}
	if userID != "" && groupID == "" && groupName == "" {
		d.Targets = []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: userID}}
	} else {
		tg := scheduler.DeliveryTarget{
			Kind: scheduler.DeliveryKindGroup, GroupID: groupID, GroupName: groupName,
		}
		if mentions := stringArg(args, "mention_user_ids"); mentions != "" {
			for _, p := range strings.FieldsFunc(mentions, func(r rune) bool {
				return r == ',' || r == '，' || r == ';' || r == ' ' || r == '\n' || r == '\t'
			}) {
				p = strings.TrimSpace(p)
				if p != "" {
					tg.MentionUserIDs = append(tg.MentionUserIDs, p)
				}
			}
		}
		if srvScheduleBoolArg(args, "mention_all", false) {
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func parseAndResolveSrvDelivery(svc *agentservice.Service, args map[string]interface{}) (*scheduler.TaskDelivery, error) {
	d, err := parseSrvScheduleDelivery(args)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, nil
	}
	if err := resolveSrvScheduleDelivery(svc, d); err != nil {
		return nil, err
	}
	return d, nil
}

func srvToolCreateScheduledTask(svc *agentservice.Service, mgr *scheduler.Manager, args map[string]interface{}) string {
	name := stringArg(args, "name")
	taskAction := stringArg(args, "task_action")
	if taskAction == "" {
		if a := stringArg(args, "action"); a != "" {
			switch normalizeSrvScheduleAction(a) {
			case "create", "list", "delete", "update", "list_targets":
			default:
				taskAction = a
			}
		}
	}
	if name == "" || taskAction == "" {
		return "缺少 name 或 task_action 参数（task_action 为到点要执行的内容）"
	}
	intervalMin := srvScheduleIntArg(args, "interval_minutes", 0)
	hour := srvScheduleIntArg(args, "hour", -1)
	if intervalMin > 0 && hour < 0 {
		hour = 0
	}
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := srvScheduleIntArg(args, "minute", 0)
	if minute < 0 || minute > 59 {
		return "minute 必须在 0-59 之间"
	}
	dow := srvScheduleIntArg(args, "day_of_week", -1)
	dom := srvScheduleIntArg(args, "day_of_month", -1)

	t := scheduler.ScheduledTask{
		Name:            name,
		Action:          taskAction,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dow,
		DayOfMonth:      dom,
		IntervalMinutes: intervalMin,
		StartDate:       stringArg(args, "start_date"),
		EndDate:         stringArg(args, "end_date"),
		TaskType:        stringArg(args, "task_type"),
	}
	if d, err := parseAndResolveSrvDelivery(svc, args); err != nil {
		return err.Error()
	} else if d != nil {
		t.Delivery = d
	}

	id, err := mgr.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}
	if task := mgr.Get(id); task != nil {
		extra := ""
		if s := scheduler.SummarizeDelivery(task.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		if task.NextRunAt != nil {
			return fmt.Sprintf("定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s%s",
				id, name, taskAction, task.NextRunAt.Format("2006-01-02 15:04"), extra)
		}
		return fmt.Sprintf("定时任务已创建（ID: %s）%s", id, extra)
	}
	return fmt.Sprintf("定时任务已创建（ID: %s）", id)
}

func srvToolListScheduledTasks(mgr *scheduler.Manager) string {
	tasks := mgr.List()
	if len(tasks) == 0 {
		return "当前没有定时任务"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("定时任务列表（共 %d 个）：\n\n", len(tasks)))
	for i, t := range tasks {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   ID: %s\n   操作: %s\n   下次执行: %s\n   已执行: %d 次",
			i+1, t.Status, t.Name, t.ID, scheduler.TruncateStr(t.Action, 80), next, t.RunCount))
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			b.WriteString("\n   推送: " + s)
		}
		if scheduler.HasDeliveryWarning(t.LastResult) {
			b.WriteString("\n   投递: 警告")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func srvToolDeleteScheduledTask(mgr *scheduler.Manager, args map[string]interface{}) string {
	id := stringArg(args, "id")
	name := stringArg(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		err = mgr.Delete(id)
	} else {
		err = mgr.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	return "定时任务已删除"
}

func srvToolUpdateScheduledTask(svc *agentservice.Service, mgr *scheduler.Manager, args map[string]interface{}) string {
	id := stringArg(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	if ta := stringArg(args, "task_action"); ta != "" {
		args["action"] = ta
	} else if a := stringArg(args, "action"); a != "" && normalizeSrvScheduleAction(a) != a {
		delete(args, "action")
	}
	// Full replace or partial patch (fail_on_error) without wiping delivery.
	if err := applyDeliveryUpdateArgs(svc, mgr, id, args); err != nil {
		return err.Error()
	}
	if err := mgr.Update(id, args); err != nil {
		return fmt.Sprintf("更新失败: %s", err.Error())
	}
	if t := mgr.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		extra := ""
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		return fmt.Sprintf("定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s%s",
			t.ID, t.Name, t.Action, t.Hour, t.Minute, next, extra)
	}
	return "定时任务已更新"
}
