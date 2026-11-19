package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// srvScheduleCaller is the agent instance that is managing tasks. An empty
// InstanceID is the legacy unscoped caller used by tests and admin tools.
type srvScheduleCaller struct {
	InstanceID   string
	TenantID     string
	UserID       string
	DesktopBotID string
}

// newSrvManageScheduleHandler builds manage_schedule for CoreAgentExecutor.
func newSrvManageScheduleHandler(svc *agentservice.Service, mgr *scheduler.Manager) func(args map[string]interface{}) string {
	return newSrvManageScheduleHandlerForCaller(svc, mgr, srvScheduleCaller{})
}

func newSrvManageScheduleHandlerForCaller(svc *agentservice.Service, mgr *scheduler.Manager, caller srvScheduleCaller) func(args map[string]interface{}) string {
	caller.InstanceID = strings.TrimSpace(caller.InstanceID)
	caller.TenantID = strings.TrimSpace(caller.TenantID)
	caller.UserID = strings.TrimSpace(caller.UserID)
	return func(args map[string]interface{}) string {
		if mgr == nil {
			return "定时任务管理器未初始化"
		}
		call := caller
		if args != nil {
			delete(args, "instance_id")
			delete(args, "owner_tenant_id")
			delete(args, "owner_user_id")
			// The turn stamps its own bot. An omitted field must not clear a
			// caller that already named one.
			if id := stringArg(args, "desktop_bot_id"); id != "" {
				call.DesktopBotID = id
			}
			delete(args, "desktop_bot_id")
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
			return srvToolCreateScheduledTask(svc, mgr, call, args)
		case "list":
			return srvToolListScheduledTasks(mgr, call)
		case "delete":
			return srvToolDeleteScheduledTask(mgr, call, args)
		case "update":
			return srvToolUpdateScheduledTask(svc, mgr, call, args)
		case "list_targets":
			return srvToolListScheduleDeliveryTargets(svc, args)
		default:
			return fmt.Sprintf("未知 manage_schedule action: %s（支持: create/list/delete/update/list_targets）", actionText)
		}
	}
}

func srvScheduleVerb(s string) bool {
	switch normalizeSrvScheduleAction(s) {
	case "create", "list", "delete", "update", "list_targets":
		return true
	default:
		return false
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

func srvToolCreateScheduledTask(svc *agentservice.Service, mgr *scheduler.Manager, caller srvScheduleCaller, args map[string]interface{}) string {
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
	// A desktop bot already has this name. The call is an edit of that row:
	// a missing clock or a missing instruction stays as stored. Rejecting it
	// here is what made the model delete the running timer and create another.
	if strings.TrimSpace(caller.DesktopBotID) != "" && name != "" {
		claimUntaggedTimerByName(mgr, caller.InstanceID, caller.DesktopBotID, caller.UserID, name)
		if existing := srvDesktopBotTimerByName(mgr, caller, name); existing != nil {
			return srvUpdateDesktopBotTimer(svc, mgr, existing, taskAction, args)
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
		InstanceID:      caller.InstanceID,
		OwnerTenantID:   caller.TenantID,
		OwnerUserID:     caller.UserID,
		DesktopBotID:    strings.TrimSpace(caller.DesktopBotID),
		DesktopBotOwner: strings.TrimSpace(caller.UserID),
	}
	openNote := ""
	if t.DesktopBotID != "" {
		// A process row catches up a slot missed while the process was down.
		// A bot timer must not do that, whatever task_type the call sent.
		t.TaskType = scheduler.TaskTypeReminder
		// start/end and a day of the month are a host one-shot. On a bot
		// timer they are not in the reply, then the row vanishes on that date.
		if t.StartDate != "" || t.EndDate != "" || t.DayOfMonth > 0 {
			openNote = "\n" + desktopBotTimerStaysOpen()
		}
		t.StartDate = ""
		t.EndDate = ""
		t.DayOfMonth = -1
	}
	if t.DesktopBotID == "" {
		t.DesktopBotOwner = ""
	}
	deliveryNote := ""
	if t.DesktopBotID != "" {
		// This row fires as the bot. A channel push is not a second output,
		// and rejecting the call here makes the model build a host task instead.
		if scheduler.ArgsTouchDelivery(args) {
			deliveryNote = "\n" + desktopBotTimerRunsItself()
		}
		deliveryNote += openNote
	} else if d, err := parseAndResolveSrvDelivery(svc, args); err != nil {
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
			return fmt.Sprintf("定时任务已创建\nID: %s\n名称: %s\n操作: %s\n%s\n下次执行: %s%s%s",
				id, name, taskAction, desktopBotScheduleWhen(*task), scheduleClockText(task.NextRunAt), extra, deliveryNote)
		}
		return fmt.Sprintf("定时任务已创建（ID: %s）%s%s", id, extra, deliveryNote)
	}
	return fmt.Sprintf("定时任务已创建（ID: %s）%s", id, deliveryNote)
}

// srvDesktopBotTimerByName is this bot's row for name. claimUntaggedTimerByName
// has already given it a same-name row that was stored without a bot.
func srvDesktopBotTimerByName(mgr *scheduler.Manager, caller srvScheduleCaller, name string) *scheduler.ScheduledTask {
	name = strings.TrimSpace(name)
	botID := strings.TrimSpace(caller.DesktopBotID)
	if mgr == nil || botID == "" || name == "" {
		return nil
	}
	for _, task := range mgr.List() {
		if task.Name != name || task.InstanceID != strings.TrimSpace(caller.InstanceID) || task.DesktopBotID != botID {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		cp := task
		return &cp
	}
	return nil
}

// srvUpdateDesktopBotTimer writes the same row. A second create for this
// name is not a new timer, and it does not stop the occurrence already running.
func srvUpdateDesktopBotTimer(_ *agentservice.Service, mgr *scheduler.Manager, existing *scheduler.ScheduledTask, taskAction string, args map[string]interface{}) string {
	fields := map[string]interface{}{}
	if taskAction != "" {
		fields["action"] = taskAction
	}
	if _, ok := args["interval_minutes"]; ok {
		if n := srvScheduleIntArg(args, "interval_minutes", 0); n > 0 {
			fields["interval_minutes"] = n
		}
	}
	if _, hourOK := args["hour"]; hourOK {
		if _, minuteOK := args["minute"]; minuteOK {
			fields["hour"] = srvScheduleIntArg(args, "hour", 0)
			fields["minute"] = srvScheduleIntArg(args, "minute", 0)
		}
	}
	if _, ok := args["day_of_week"]; ok {
		fields["day_of_week"] = srvScheduleIntArg(args, "day_of_week", -1)
	}
	staysOpen := false
	if existing.StartDate != "" || stringArg(args, "start_date") != "" {
		fields["start_date"] = ""
		staysOpen = true
	}
	if existing.EndDate != "" || stringArg(args, "end_date") != "" {
		fields["end_date"] = ""
		staysOpen = true
	}
	if existing.DayOfMonth > 0 {
		fields["day_of_month"] = -1
		staysOpen = true
	} else if _, ok := args["day_of_month"]; ok && srvScheduleIntArg(args, "day_of_month", -1) > 0 {
		fields["day_of_month"] = -1
		staysOpen = true
	}
	if v := stringArg(args, "name"); v != "" {
		fields["name"] = v
	}
	runsItself := existing.Delivery != nil || scheduler.ArgsTouchDelivery(args)
	if runsItself {
		fields["delivery"] = nil
	}
	if err := mgr.Update(existing.ID, fields); err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}
	task := mgr.Get(existing.ID)
	if task == nil {
		return "定时任务已更新\nID: " + existing.ID
	}
	text := srvScheduleUpdatedText(task)
	if runsItself {
		text += "\n" + desktopBotTimerRunsItself()
	}
	if staysOpen {
		text += "\n" + desktopBotTimerStaysOpen()
	}
	return text
}

// scheduleClockText is the next fire on this machine's clock. The stored
// instant is UTC. Printing that wall time with no zone made a 22:31 run
// look like 14:31, and the model rewrote the timer to correct it.
func scheduleClockText(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	local := t.In(time.Local)
	return local.Format("2006-01-02 15:04 -07:00")
}

func srvScheduleUpdatedText(task *scheduler.ScheduledTask) string {
	next := scheduleClockText(task.NextRunAt)
	extra := ""
	if s := scheduler.SummarizeDelivery(task.Delivery); s != "" {
		extra = "\n推送: " + s
	}
	return fmt.Sprintf("定时任务已更新\nID: %s\n名称: %s\n操作: %s\n%s\n下次执行: %s%s",
		task.ID, task.Name, task.Action, desktopBotScheduleWhen(*task), next, extra)
}

func srvScheduleTasksForCaller(tasks []scheduler.ScheduledTask, caller srvScheduleCaller) []scheduler.ScheduledTask {
	instanceID := strings.TrimSpace(caller.InstanceID)
	botID := strings.TrimSpace(caller.DesktopBotID)
	if instanceID == "" && botID == "" {
		return tasks
	}
	out := make([]scheduler.ScheduledTask, 0, len(tasks))
	for _, task := range tasks {
		if instanceID != "" && task.InstanceID != instanceID {
			continue
		}
		if botID != "" && task.DesktopBotID != botID {
			continue
		}
		if botID != "" && strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		out = append(out, task)
	}
	return out
}

func srvScheduleTaskVisible(task *scheduler.ScheduledTask, caller srvScheduleCaller) bool {
	if task == nil {
		return false
	}
	instanceID := strings.TrimSpace(caller.InstanceID)
	if instanceID != "" && task.InstanceID != instanceID {
		return false
	}
	botID := strings.TrimSpace(caller.DesktopBotID)
	if botID != "" && task.DesktopBotID != botID {
		return false
	}
	return true
}

func srvToolListScheduledTasks(mgr *scheduler.Manager, caller srvScheduleCaller) string {
	if strings.TrimSpace(caller.DesktopBotID) != "" {
		dropDesktopBotScheduleDelivery(mgr)
	}
	tasks := srvScheduleTasksForCaller(mgr.List(), caller)
	if len(tasks) == 0 {
		return "当前没有定时任务"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("定时任务列表（共 %d 个）：\n\n", len(tasks)))
	for i, t := range tasks {
		next := scheduleClockText(t.NextRunAt)
		b.WriteString(fmt.Sprintf("%d. [%s] %s\n   ID: %s\n   操作: %s\n   时间: %s\n   下次执行: %s\n   已执行: %d 次",
			i+1, t.Status, t.Name, t.ID, scheduler.TruncateStr(t.Action, 80), desktopBotScheduleWhen(t), next, t.RunCount))
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			b.WriteString("\n   推送: " + s)
		}
		if strings.TrimSpace(caller.DesktopBotID) == "" && scheduler.HasDeliveryWarning(t.LastResult) {
			b.WriteString("\n   投递: 警告")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func srvToolDeleteScheduledTask(mgr *scheduler.Manager, caller srvScheduleCaller, args map[string]interface{}) string {
	id := stringArg(args, "id")
	name := stringArg(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		task := mgr.Get(id)
		if !srvScheduleTaskVisible(task, caller) {
			return "删除失败: scheduler: task not found"
		}
		if held := srvDesktopBotLeaveRunning(mgr, caller, task); held != "" {
			return held
		}
		err = mgr.Delete(id)
	} else {
		err = srvDeleteScheduledTaskByName(mgr, name, caller)
	}
	if err != nil {
		if strings.Contains(err.Error(), "没有取消") {
			return err.Error()
		}
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	return "定时任务已删除"
}

func srvDeleteScheduledTaskByName(mgr *scheduler.Manager, name string, caller srvScheduleCaller) error {
	for _, task := range mgr.List() {
		if task.Name != name || !srvScheduleTaskVisible(&task, caller) {
			continue
		}
		if held := srvDesktopBotLeaveRunning(mgr, caller, &task); held != "" {
			return fmt.Errorf("%s", held)
		}
		return mgr.Delete(task.ID)
	}
	return fmt.Errorf("scheduler: task named %q not found", name)
}

// srvDesktopBotLeaveRunning refuses to stop a desktop bot timer that is
// already firing. Manager.Delete cancels that run and still returns an error,
// which is how a replace attempt dropped the weather timer mid-occurrence.
func srvDesktopBotLeaveRunning(mgr *scheduler.Manager, caller srvScheduleCaller, task *scheduler.ScheduledTask) string {
	if strings.TrimSpace(caller.DesktopBotID) == "" || task == nil || mgr == nil || !mgr.Running(task.ID) {
		return ""
	}
	return desktopBotTimerLeftRunning()
}

func srvToolUpdateScheduledTask(svc *agentservice.Service, mgr *scheduler.Manager, caller srvScheduleCaller, args map[string]interface{}) string {
	id := stringArg(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	task := mgr.Get(id)
	if !srvScheduleTaskVisible(task, caller) {
		return "更新失败: scheduler: task not found"
	}
	if strings.TrimSpace(caller.DesktopBotID) != "" {
		taskAction := stringArg(args, "task_action")
		if taskAction == "" {
			if a := stringArg(args, "action"); a != "" && !srvScheduleVerb(a) {
				taskAction = a
			}
		}
		return srvUpdateDesktopBotTimer(svc, mgr, task, taskAction, args)
	}
	if ta := stringArg(args, "task_action"); ta != "" {
		args["action"] = ta
	} else if srvScheduleVerb(stringArg(args, "action")) {
		// action=update is the tool verb. Leaving it in the patch stores the
		// word "update" as the order for the next run.
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
		next := scheduleClockText(t.NextRunAt)
		extra := ""
		if s := scheduler.SummarizeDelivery(t.Delivery); s != "" {
			extra = "\n推送: " + s
		}
		return fmt.Sprintf("定时任务已更新\nID: %s\n名称: %s\n操作: %s\n%s\n下次执行: %s%s",
			t.ID, t.Name, t.Action, desktopBotScheduleWhen(*t), next, extra)
	}
	return "定时任务已更新"
}
