package main

import (
	"context"
	"fmt"
	"log"
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
		n := cancelDesktopBotSchedules(mgr, instanceID, botID, decision.Schedule.Name)
		log.Printf("[desktop-schedule] cancel bot=%s instance=%s n=%d", botID, instanceID, n)
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
			fields["hour"] = 0
			fields["minute"] = 0
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

func cancelDesktopBotSchedules(mgr *scheduler.Manager, instanceID, botID, name string) int {
	name = strings.TrimSpace(name)
	var ids []string
	for _, task := range mgr.List() {
		if task.InstanceID != instanceID || task.DesktopBotID != botID {
			continue
		}
		if name != "" && task.Name != name {
			continue
		}
		ids = append(ids, task.ID)
	}
	n := 0
	for _, id := range ids {
		if err := mgr.ForceDelete(id); err == nil {
			n++
		}
	}
	return n
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
	}
	return in
}
