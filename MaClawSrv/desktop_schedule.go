package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// runDesktopBotSchedule starts the bot turn for one due desktop-bot row.
// Tests replace it. Production calls SendMessage on that bot's instance.
var runDesktopBotSchedule = runDesktopBotScheduledTask

// persistDesktopBotSchedule writes the Intake decision onto the host clock.
// The GUI may keep a display copy; it does not start the turn.
func persistDesktopBotSchedule(p agentservice.Principal, instanceID string, in agentservice.BotIntakeInput, decision agentservice.BotIntake) {
	if decision.Kind != "schedule" || decision.Schedule == nil {
		return
	}
	if err := armDesktopBotSchedule(getSrvSchedulerManager(), p, instanceID, in.BotID, in.Content, decision); err != nil {
		log.Printf("[desktop-schedule] bot=%s instance=%s: %v", strings.TrimSpace(in.BotID), strings.TrimSpace(instanceID), err)
	}
}

// armDesktopBotSchedule creates, updates, or cancels one bot's timer.
// The name is the timer. A later decision that uses that name replaces the
// stored order with the decision's instruction and writes the clock the
// decision carries. The same stored order updates that row's clock.
// A different name is a new timer.
func armDesktopBotSchedule(mgr *scheduler.Manager, p agentservice.Principal, instanceID, botID, content string, decision agentservice.BotIntake) error {
	if mgr == nil {
		return fmt.Errorf("desktop bot clock is not running")
	}
	if decision.Schedule == nil {
		return fmt.Errorf("desktop bot schedule is missing")
	}
	botID = strings.TrimSpace(botID)
	instanceID = strings.TrimSpace(instanceID)
	if botID == "" || instanceID == "" {
		return fmt.Errorf("desktop bot schedule needs a bot and an instance")
	}
	mode := strings.ToLower(strings.TrimSpace(decision.Schedule.Mode))
	if mode == "" {
		mode = "create"
	}
	if mode == "cancel" {
		n, runningLeft := cancelDesktopBotSchedules(mgr, instanceID, botID, decision.Schedule.Name)
		log.Printf("[desktop-schedule] cancel bot=%s instance=%s n=%d running=%d", botID, instanceID, n, runningLeft)
		return nil
	}
	if mode != "create" {
		return fmt.Errorf("desktop bot schedule mode %q is unknown", mode)
	}
	givenName := strings.TrimSpace(decision.Schedule.Name)
	action := strings.TrimSpace(decision.Instruction)
	hour, minute, day := 0, 0, -1
	interval := decision.Schedule.IntervalMinutes
	if interval > 0 {
		hour, minute = 0, 0
		if decision.Schedule.Hour != nil && decision.Schedule.Minute != nil {
			hour = *decision.Schedule.Hour
			minute = *decision.Schedule.Minute
		}
	} else {
		if decision.Schedule.Hour != nil {
			hour = *decision.Schedule.Hour
		}
		if decision.Schedule.Minute != nil {
			minute = *decision.Schedule.Minute
		}
	}
	if decision.Schedule.DayOfWeek != nil {
		day = *decision.Schedule.DayOfWeek
	}
	claimUntaggedTimerByName(mgr, instanceID, botID, strings.TrimSpace(p.UserID), givenName)
	var mine []scheduler.ScheduledTask
	for _, existing := range mgr.List() {
		if existing.Status != "active" || existing.InstanceID != instanceID || existing.DesktopBotID != botID {
			continue
		}
		mine = append(mine, existing)
	}
	matchedID, matchedName, matchedAction := "", "", ""
	for _, existing := range mine {
		if givenName != "" && existing.Name == givenName {
			matchedID, matchedName, matchedAction = existing.ID, existing.Name, strings.TrimSpace(existing.Action)
			break
		}
	}
	if matchedID == "" && action != "" {
		for _, existing := range mine {
			if strings.TrimSpace(existing.Action) == action {
				matchedID, matchedName, matchedAction = existing.ID, existing.Name, strings.TrimSpace(existing.Action)
				break
			}
		}
	}
	if matchedID != "" {
		if action == "" {
			action = matchedAction
		}
		name := givenName
		if name == "" {
			name = matchedName
		}
		fields := map[string]interface{}{
			"name":   name,
			"action": action,
		}
		if interval > 0 {
			fields["interval_minutes"] = interval
			if decision.Schedule.Hour != nil && decision.Schedule.Minute != nil {
				fields["hour"] = *decision.Schedule.Hour
				fields["minute"] = *decision.Schedule.Minute
			}
		} else if decision.Schedule.Hour != nil && decision.Schedule.Minute != nil {
			fields["interval_minutes"] = 0
			fields["hour"] = hour
			fields["minute"] = minute
		}
		if decision.Schedule.DayOfWeek != nil {
			fields["day_of_week"] = day
		}
		if err := mgr.Update(matchedID, fields); err != nil {
			return err
		}
		log.Printf("[desktop-schedule] update id=%s bot=%s interval=%d hour=%d minute=%d", matchedID, botID, interval, hour, minute)
		return nil
	}
	if action == "" {
		action = strings.TrimSpace(content)
	}
	if action == "" {
		return fmt.Errorf("desktop bot schedule has no work order")
	}
	name := givenName
	if name == "" {
		name = clipScheduleName(action)
	}
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name:            name,
		Action:          action,
		DesktopBotID:    botID,
		DesktopBotOwner: strings.TrimSpace(p.UserID),
		InstanceID:      instanceID,
		OwnerTenantID:   strings.TrimSpace(p.TenantID),
		OwnerUserID:     strings.TrimSpace(p.UserID),
		// Reminder skips a slot missed while the process was down. A process
		// row would catch that slot up on the next start, so a deploy after
		// 08:00 would run every daily bot timer at once.
		TaskType:        scheduler.TaskTypeReminder,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       day,
		DayOfMonth:      -1,
		IntervalMinutes: interval,
		Status:          "active",
	})
	if err != nil {
		return err
	}
	log.Printf("[desktop-schedule] create id=%s bot=%s interval=%d hour=%d minute=%d", id, botID, interval, hour, minute)
	return nil
}

// adoptUntaggedBotSchedules gives a timer back to the desktop bot that owns
// its instance. manage_schedule used to store the row with only the instance,
// so the bot's schedule list could not see it.
func adoptUntaggedBotSchedules(svc *agentservice.Service, mgr *scheduler.Manager) {
	if svc == nil || mgr == nil {
		return
	}
	ctx := context.Background()
	for _, task := range mgr.List() {
		if strings.TrimSpace(task.DesktopBotID) != "" || strings.TrimSpace(task.InstanceID) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		p := agentservice.Principal{TenantID: strings.TrimSpace(task.OwnerTenantID), UserID: strings.TrimSpace(task.OwnerUserID)}
		if p.TenantID == "" {
			p.TenantID = "system"
		}
		inst, err := svc.GetInstance(ctx, p, task.InstanceID)
		if err != nil || inst == nil || strings.TrimSpace(inst.Metadata["hub_bot"]) != "1" {
			continue
		}
		botID := strings.TrimSpace(inst.Metadata["hub_desktop_bot"])
		if botID == "" {
			continue
		}
		if err := mgr.BindDesktopBot(task.ID, botID, task.OwnerUserID); err != nil {
			log.Printf("[desktop-schedule] adopt %s: %v", task.ID, err)
			continue
		}
		log.Printf("[desktop-schedule] adopt id=%s bot=%s", task.ID, botID)
	}
}

func desktopBotTimerLeftRunning() string {
	return "这条定时任务正在执行，没有取消，记录还在。同名的 create 会改这一条，不会另建一条。"
}

func desktopBotTimerRunsItself() string {
	return "这条定时任务由这个 bot 自己执行。到点的那一轮自己把结果发出去，不另建推送。同名的 create 改这一条。"
}

func desktopBotTimerStaysOpen() string {
	return "这条定时任务按周期一直重复，不设开始或结束日期，也不限每月几号。同名的 create 改这一条。"
}

// dropDesktopBotScheduleDelivery removes a channel push stored on a bot timer.
// The occurrence is that bot's own turn. A stored push is not sent, and the
// list line that advertised it is what made a later turn delete the timer
// and create another.
func dropDesktopBotScheduleDelivery(mgr *scheduler.Manager) {
	if mgr == nil {
		return
	}
	for _, task := range mgr.List() {
		if strings.TrimSpace(task.DesktopBotID) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		if task.Delivery != nil {
			if err := mgr.Update(task.ID, map[string]interface{}{"delivery": nil}); err != nil {
				log.Printf("[desktop-schedule] drop delivery %s: %v", task.ID, err)
			}
		}
		if scheduler.HasDeliveryWarning(task.LastResult) {
			if err := mgr.ClearDeliveryWarning(task.ID); err != nil {
				log.Printf("[desktop-schedule] drop delivery note %s: %v", task.ID, err)
			}
		}
		// A one-shot window is not part of the reply. The row then disappears
		// on that date, which is the same empty list as a cancelled timer.
		if task.StartDate != "" || task.EndDate != "" || task.DayOfMonth > 0 {
			fields := map[string]interface{}{}
			if task.StartDate != "" {
				fields["start_date"] = ""
			}
			if task.EndDate != "" {
				fields["end_date"] = ""
			}
			if task.DayOfMonth > 0 {
				fields["day_of_month"] = -1
			}
			if err := mgr.Update(task.ID, fields); err != nil {
				log.Printf("[desktop-schedule] drop window %s: %v", task.ID, err)
			}
		}
	}
}

func cancelDesktopBotSchedules(mgr *scheduler.Manager, instanceID, botID, name string) (removed, runningLeft int) {
	name = strings.TrimSpace(name)
	var idle []string
	for _, task := range mgr.List() {
		if task.InstanceID != instanceID || task.DesktopBotID != botID {
			continue
		}
		if name != "" && task.Name != name {
			continue
		}
		if mgr.Running(task.ID) {
			runningLeft++
			continue
		}
		idle = append(idle, task.ID)
	}
	for _, id := range idle {
		if err := mgr.ForceDelete(id); err == nil {
			removed++
		}
	}
	return removed, runningLeft
}

// desktopBotScheduleTool is bot_schedule. The model names the timer. The
// same name updates that row through armDesktopBotSchedule. A clock field
// the call leaves out stays on the row. A different name is a new timer.
func desktopBotScheduleTool(mgr *scheduler.Manager, p agentservice.Principal, instanceID, botID string, args map[string]interface{}) string {
	if mgr == nil {
		return "定时任务时钟没有启动"
	}
	instanceID = strings.TrimSpace(instanceID)
	botID = strings.TrimSpace(botID)
	if botID == "" || instanceID == "" {
		return "这一轮没有桌面 bot，不能登记定时任务"
	}
	action := strings.ToLower(strings.TrimSpace(scheduleArgString(args, "action")))
	if action == "" {
		action = "create"
	}
	switch action {
	case "list":
		return desktopBotScheduleList(mgr, instanceID, botID)
	case "cancel":
		name := clipScheduleText(scheduleArgString(args, "name"), 80)
		if name == "" {
			return "取消要给出这条定时任务的 name"
		}
		n, runningLeft := cancelDesktopBotSchedules(mgr, instanceID, botID, name)
		if runningLeft > 0 && n == 0 {
			return desktopBotTimerLeftRunning()
		}
		if n == 0 {
			return "没有名为 " + name + " 的定时任务"
		}
		if runningLeft > 0 {
			return "已取消 " + name + "。" + desktopBotTimerLeftRunning()
		}
		return "已取消 " + name
	case "create":
		return desktopBotScheduleCreate(mgr, p, instanceID, botID, args)
	default:
		return "action 只能是 create、list 或 cancel"
	}
}

func desktopBotScheduleCreate(mgr *scheduler.Manager, p agentservice.Principal, instanceID, botID string, args map[string]interface{}) string {
	name := clipScheduleText(scheduleArgString(args, "name"), 80)
	if name == "" {
		return "登记要给出这条定时任务的 name"
	}
	instruction := clipScheduleText(scheduleArgString(args, "instruction"), 4000)
	interval, intervalSet := scheduleArgInt(args, "interval_minutes")
	hour, hourSet := scheduleArgInt(args, "hour")
	minute, minuteSet := scheduleArgInt(args, "minute")
	day, daySet := scheduleArgInt(args, "day_of_week")
	if hourSet != minuteSet {
		return "hour 和 minute 要一起写，或者都不写"
	}
	if intervalSet && interval <= 0 {
		intervalSet = false
	}
	if intervalSet && interval > 7*24*60 {
		return "interval_minutes 不能超过 7 天"
	}
	if hourSet && (hour < 0 || hour > 23 || minute < 0 || minute > 59) {
		return "hour 必须在 0-23，minute 必须在 0-59"
	}
	if daySet && (day < 0 || day > 6) {
		daySet = false
	}
	exists := desktopBotHasTimer(mgr, instanceID, botID, name)
	if !intervalSet && !hourSet && !exists {
		return "新建定时任务要给出 interval_minutes，或同时给出 hour 和 minute"
	}
	if instruction == "" && !exists {
		return "新建定时任务要给出 instruction，写以后每一轮要做的整件事"
	}
	sched := &agentservice.BotIntakeSchedule{Name: name, Mode: "create"}
	if intervalSet {
		sched.IntervalMinutes = interval
	}
	if hourSet {
		sched.Hour = &hour
		sched.Minute = &minute
	}
	if daySet {
		sched.DayOfWeek = &day
	}
	if exists {
		if current := desktopBotTimerByName(mgr, instanceID, botID, name); current != nil && desktopBotScheduleUnchanged(current, instruction, sched) {
			return "已是 " + name + "，" + desktopBotScheduleWhen(*current) + "。" + clipScheduleText(current.Action, 200)
		}
	}
	decision := agentservice.BotIntake{Kind: "schedule", Instruction: instruction, Schedule: sched}
	if err := armDesktopBotSchedule(mgr, p, instanceID, botID, "", decision); err != nil {
		return err.Error()
	}
	task := desktopBotTimerByName(mgr, instanceID, botID, name)
	if task == nil {
		return "已登记 " + name
	}
	verb := "已登记"
	if exists {
		verb = "已更新"
	}
	return verb + " " + name + "，" + desktopBotScheduleWhen(*task) + "。" + clipScheduleText(task.Action, 200)
}

// desktopBotScheduleUnchanged reports a register call that repeats the stored
// order and clock. Writing it again would schedule the next run from now.
func desktopBotScheduleUnchanged(task *scheduler.ScheduledTask, instruction string, sched *agentservice.BotIntakeSchedule) bool {
	if task == nil || sched == nil {
		return false
	}
	if instruction != "" && instruction != strings.TrimSpace(task.Action) {
		return false
	}
	if sched.IntervalMinutes > 0 && sched.IntervalMinutes != task.IntervalMinutes {
		return false
	}
	if sched.Hour != nil && sched.Minute != nil {
		if sched.IntervalMinutes > 0 {
			if task.Hour != *sched.Hour || task.Minute != *sched.Minute {
				return false
			}
		} else if task.IntervalMinutes != 0 || task.Hour != *sched.Hour || task.Minute != *sched.Minute {
			return false
		}
	}
	if sched.DayOfWeek != nil && *sched.DayOfWeek != task.DayOfWeek {
		return false
	}
	return true
}

func clipScheduleText(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}

// claimUntaggedTimerByName gives this bot the one timer manage_schedule stored
// under name with only the instance. Other untagged rows on the instance,
// including a host task, stay untagged.
func claimUntaggedTimerByName(mgr *scheduler.Manager, instanceID, botID, owner, name string) {
	if mgr == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	botID = strings.TrimSpace(botID)
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if instanceID == "" || botID == "" || name == "" {
		return
	}
	for _, task := range mgr.List() {
		if task.InstanceID != instanceID || task.Name != name || strings.TrimSpace(task.DesktopBotID) != "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(task.Name), desktopLongJobTaskPrefix) {
			continue
		}
		rowOwner := strings.TrimSpace(task.OwnerUserID)
		if rowOwner != "" && owner != "" && rowOwner != owner {
			continue
		}
		_ = mgr.BindDesktopBot(task.ID, botID, owner)
	}
}

func desktopBotScheduleList(mgr *scheduler.Manager, instanceID, botID string) string {
	dropDesktopBotScheduleDelivery(mgr)
	var lines []string
	for _, task := range mgr.List() {
		if task.Status != "active" || task.InstanceID != instanceID || task.DesktopBotID != botID {
			continue
		}
		lines = append(lines, task.Name+"，"+desktopBotScheduleWhen(task)+"。"+clipScheduleText(task.Action, 200))
	}
	if len(lines) == 0 {
		return "当前没有定时任务"
	}
	return strings.Join(lines, "\n")
}

func desktopBotScheduleWhen(task scheduler.ScheduledTask) string {
	if task.IntervalMinutes > 0 {
		when := fmt.Sprintf("每 %d 分钟", task.IntervalMinutes)
		if task.Hour != 0 || task.Minute != 0 {
			when += fmt.Sprintf("，从 %02d:%02d 起", task.Hour, task.Minute)
		}
		if task.DayOfWeek >= 0 && task.DayOfWeek <= 6 {
			when += "，" + desktopBotWeekday(task.DayOfWeek)
		}
		return when
	}
	when := fmt.Sprintf("%02d:%02d", task.Hour, task.Minute)
	if task.DayOfWeek >= 0 && task.DayOfWeek <= 6 {
		return desktopBotWeekday(task.DayOfWeek) + " " + when
	}
	return "每天 " + when
}

func desktopBotWeekday(day int) string {
	switch day {
	case 0:
		return "周日"
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	default:
		return ""
	}
}

func desktopBotHasTimer(mgr *scheduler.Manager, instanceID, botID, name string) bool {
	return desktopBotTimerByName(mgr, instanceID, botID, name) != nil
}

func desktopBotTimerByName(mgr *scheduler.Manager, instanceID, botID, name string) *scheduler.ScheduledTask {
	name = strings.TrimSpace(name)
	if mgr == nil || name == "" {
		return nil
	}
	for _, task := range mgr.List() {
		if task.Status == "active" && task.InstanceID == instanceID && task.DesktopBotID == botID && task.Name == name {
			found := task
			return &found
		}
	}
	return nil
}

func scheduleArgString(args map[string]interface{}, key string) string {
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

func scheduleArgInt(args map[string]interface{}, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

// desktopIntakeTimers is what Intake sees, so a follow-up that names a city
// or a PDF updates the armed row instead of being read on its own.
func desktopIntakeTimers(instanceID, botID string) []agentservice.BotIntakeTimer {
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	botID = strings.TrimSpace(botID)
	var out []agentservice.BotIntakeTimer
	for _, task := range mgr.List() {
		if task.Status != "active" || task.InstanceID != instanceID || task.DesktopBotID != botID {
			continue
		}
		out = append(out, agentservice.BotIntakeTimer{
			Name:            task.Name,
			Action:          task.Action,
			IntervalMinutes: task.IntervalMinutes,
			Hour:            task.Hour,
			Minute:          task.Minute,
			DayOfWeek:       task.DayOfWeek,
		})
		if len(out) == 4 {
			break
		}
	}
	return out
}

func clipScheduleName(action string) string {
	r := []rune(strings.TrimSpace(action))
	if len(r) > 40 {
		r = r[:40]
	}
	return string(r)
}

// runDesktopBotScheduledTask is one due desktop-bot row: the same SendMessage
// path a person uses, on that bot's instance, with the quiet schedule prompt.
// It does not open the desktop. The first tool that needs a machine does.
func runDesktopBotScheduledTask(ctx context.Context, svc *agentservice.Service, task *scheduler.ScheduledTask) (string, error) {
	if svc == nil {
		return "", fmt.Errorf("scheduler service is unavailable")
	}
	if task == nil || strings.TrimSpace(task.DesktopBotID) == "" {
		return "", fmt.Errorf("desktop bot id is required")
	}
	instanceID := strings.TrimSpace(task.InstanceID)
	if instanceID == "" {
		return "", fmt.Errorf("desktop bot schedule has no instance")
	}
	principal := agentservice.Principal{
		TenantID: strings.TrimSpace(task.OwnerTenantID),
		UserID:   strings.TrimSpace(task.OwnerUserID),
	}
	if principal.TenantID == "" {
		principal.TenantID = "system"
	}
	in := desktopBotScheduleMessage(task)
	_, _, msg, err := svc.SendMessage(ctx, principal, instanceID, in)
	if err != nil {
		return "", scheduler.AnnotateRunErrWithContext(ctx, fmt.Errorf("post scheduled bot message: %w", err))
	}
	if msg != nil && strings.TrimSpace(msg.Content) != "" {
		return msg.Content, scheduler.AnnotateRunErrWithContext(ctx, nil)
	}
	return "", scheduler.AnnotateRunErrWithContext(ctx, nil)
}

// desktopBotScheduleMessage is one occurrence of a desktop-bot timer.
// ClientMessageID is stable for that due time, so a crash that fires the
// same slot again replays the run instead of starting a second one.
func desktopBotScheduleMessage(task *scheduler.ScheduledTask) agentservice.SendMessageInput {
	in := scheduledTaskMessageInput(task, scheduledTaskPrompt(task.Action))
	due := int64(0)
	if task != nil && task.NextRunAt != nil {
		due = task.NextRunAt.Unix()
	}
	id := ""
	bot := ""
	if task != nil {
		id = strings.TrimSpace(task.ID)
		bot = strings.TrimSpace(task.DesktopBotID)
	}
	in.ClientMessageID = fmt.Sprintf("sched-%s-%d", id, due)
	in.Metadata = map[string]string{
		"bot_id":          bot,
		"bot_phase":       "execute",
		"computer_helper": "1",
		"bot_origin":      "schedule",
		"bot_task":        strings.TrimSpace(task.Action),
	}
	return in
}
