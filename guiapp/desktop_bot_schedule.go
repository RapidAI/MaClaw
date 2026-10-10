package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// desktopBotReportSink records a scheduled result. Production leaves it nil.
var desktopBotReportSink func(payload string)

// ArmDesktopBotSchedule creates or cancels timed work for one bot.
// intervalMinutes above zero repeats. Otherwise hour and minute are the clock.
// dayOfWeek is -1 for every day, or 0 through 6. mode is create or cancel.
// An empty name on cancel stops every timed job of this bot.
// The return is the task id, or the number of jobs cancelled.
func (a *App) ArmDesktopBotSchedule(botID, name, mode, action string, intervalMinutes, hour, minute, dayOfWeek int) (string, error) {
	if a == nil {
		return "", fmt.Errorf("scheduler unavailable")
	}
	owner, id, err := desktopBotIdentity(a.desktopBotAccountUserID(), botID)
	if err != nil {
		return "", err
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "create"
	}
	name = clipRunes(strings.TrimSpace(name), 80)
	action = strings.TrimSpace(action)
	mgr := a.scheduledTaskManagerForIMHandler()
	if mgr == nil {
		return "", fmt.Errorf("scheduler unavailable")
	}
	if mode == "cancel" {
		n := cancelDesktopBotSchedules(mgr, owner, id, name)
		if n == 0 {
			return "", fmt.Errorf("no desktop bot schedule matched")
		}
		log.Printf("[desktop-bot-schedule] cancel owner=%q bot=%q name_len=%d n=%d", owner, id, len([]rune(name)), n)
		a.noteDesktopBotSchedulesChanged()
		return strconv.Itoa(n), nil
	}
	if mode != "create" {
		return "", fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	if action == "" {
		return "", fmt.Errorf("desktop bot understanding omitted the work order")
	}
	if name == "" {
		name = clipRunes(action, 40)
	}
	if dayOfWeek < -1 || dayOfWeek > 6 {
		return "", fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	task := scheduler.ScheduledTask{
		Name:            name,
		Action:          action,
		DesktopBotID:    id,
		DesktopBotOwner: owner,
		TaskType:        scheduler.TaskTypeProcess,
		DayOfWeek:       dayOfWeek,
		IntervalMinutes: intervalMinutes,
		Hour:            hour,
		Minute:          minute,
	}
	if intervalMinutes > 0 {
		if intervalMinutes > 7*24*60 {
			return "", fmt.Errorf("desktop bot understanding omitted the schedule")
		}
		task.Hour = 0
		task.Minute = 0
	} else if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return "", fmt.Errorf("desktop bot understanding omitted the schedule")
	}
	for _, existing := range mgr.List() {
		if existing.Status != "active" || existing.DesktopBotID != id || existing.DesktopBotOwner != owner {
			continue
		}
		if strings.TrimSpace(existing.Action) != action {
			continue
		}
		if err := mgr.Update(existing.ID, map[string]interface{}{
			"name":             name,
			"hour":             task.Hour,
			"minute":           task.Minute,
			"day_of_week":      task.DayOfWeek,
			"interval_minutes": task.IntervalMinutes,
		}); err != nil {
			return "", err
		}
		log.Printf("[desktop-bot-schedule] update id=%s bot=%q interval=%d hour=%d minute=%d", existing.ID, id, task.IntervalMinutes, task.Hour, task.Minute)
		a.noteDesktopBotSchedulesChanged()
		return existing.ID, nil
	}
	taskID, err := mgr.Add(task)
	if err != nil {
		return "", err
	}
	log.Printf("[desktop-bot-schedule] create id=%s bot=%q interval=%d hour=%d minute=%d", taskID, id, task.IntervalMinutes, task.Hour, task.Minute)
	a.noteDesktopBotSchedulesChanged()
	return taskID, nil
}

func (a *App) noteDesktopBotSchedulesChanged() {
	if a == nil {
		return
	}
	a.emitEvent("scheduled-tasks-changed")
}

// DesktopBotScheduleInfo is one timed job of this bot, for the schedules tab.
type DesktopBotScheduleInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Action          string `json:"action"`
	IntervalMinutes int    `json:"interval_minutes"`
	Hour            int    `json:"hour"`
	Minute          int    `json:"minute"`
	DayOfWeek       int    `json:"day_of_week"`
	NextRunAt       string `json:"next_run_at,omitempty"`
	Status          string `json:"status,omitempty"`
}

// ListDesktopBotSchedules returns this bot's timed jobs. Other bots and host
// schedules stay off this list.
func (a *App) ListDesktopBotSchedules(botID string) ([]DesktopBotScheduleInfo, error) {
	if a == nil {
		return nil, fmt.Errorf("scheduler unavailable")
	}
	owner, id, err := desktopBotIdentity(a.desktopBotAccountUserID(), botID)
	if err != nil {
		return nil, err
	}
	mgr := a.scheduledTaskManagerForIMHandler()
	if mgr == nil {
		return []DesktopBotScheduleInfo{}, nil
	}
	out := make([]DesktopBotScheduleInfo, 0)
	for _, task := range mgr.List() {
		if task.DesktopBotID != id || task.DesktopBotOwner != owner {
			continue
		}
		info := DesktopBotScheduleInfo{
			ID:              task.ID,
			Name:            task.Name,
			Action:          task.Action,
			IntervalMinutes: task.IntervalMinutes,
			Hour:            task.Hour,
			Minute:          task.Minute,
			DayOfWeek:       task.DayOfWeek,
			Status:          task.Status,
		}
		if task.NextRunAt != nil && !task.NextRunAt.IsZero() {
			info.NextRunAt = task.NextRunAt.Local().Format(time.RFC3339)
		}
		out = append(out, info)
	}
	return out, nil
}

// DeleteDesktopBotSchedule removes one timed job of this bot. A job that is
// running is cancelled and taken off the list. Another bot's job is left as it is.
func (a *App) DeleteDesktopBotSchedule(botID, taskID string) error {
	if a == nil {
		return fmt.Errorf("scheduler unavailable")
	}
	owner, id, err := desktopBotIdentity(a.desktopBotAccountUserID(), botID)
	if err != nil {
		return err
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("no desktop bot schedule matched")
	}
	mgr := a.scheduledTaskManagerForIMHandler()
	if mgr == nil {
		return fmt.Errorf("scheduler unavailable")
	}
	matched := false
	for _, task := range mgr.List() {
		if task.ID != taskID {
			continue
		}
		if task.DesktopBotID != id || task.DesktopBotOwner != owner {
			return fmt.Errorf("no desktop bot schedule matched")
		}
		matched = true
		break
	}
	if !matched {
		return fmt.Errorf("no desktop bot schedule matched")
	}
	err = mgr.Delete(taskID)
	if err != nil && strings.Contains(err.Error(), "cancellation requested") {
		err = mgr.ForceDelete(taskID)
	}
	if err != nil {
		return err
	}
	log.Printf("[desktop-bot-schedule] delete id=%s bot=%q", taskID, id)
	a.noteDesktopBotSchedulesChanged()
	return nil
}

func cancelDesktopBotSchedules(mgr *scheduler.Manager, owner, botID, name string) int {
	if mgr == nil {
		return 0
	}
	var ids []string
	for _, task := range mgr.List() {
		if task.DesktopBotID != botID || task.DesktopBotOwner != owner {
			continue
		}
		if name != "" && task.Name != name {
			continue
		}
		ids = append(ids, task.ID)
	}
	n := 0
	for _, id := range ids {
		err := mgr.Delete(id)
		if err == nil || strings.Contains(err.Error(), "cancellation requested") {
			n++
		}
	}
	return n
}

// executeDesktopBotScheduledTask runs one occurrence on the bot and reports
// the result in that bot's chat. The schedule itself is already armed.
func (a *App) executeDesktopBotScheduledTask(ctx context.Context, task *scheduler.ScheduledTask) (string, error) {
	if task == nil || strings.TrimSpace(task.DesktopBotID) == "" {
		return "", fmt.Errorf("desktop bot id is required")
	}
	order := "This occurrence of a schedule is already set. Do the work below once and put the result in the reply. Do not create, change, or cancel a schedule.\n" + strings.TrimSpace(task.Action)
	result, err := a.relayDesktopBot(ctx, task.DesktopBotID, order, "execute")
	text := strings.TrimSpace(result.Text)
	if err != nil && text == "" {
		text = err.Error()
	}
	a.reportDesktopBotSchedule(task, text, result, err)
	log.Printf("[desktop-bot-schedule] run id=%s bot=%q text_len=%d err=%v", task.ID, task.DesktopBotID, len([]rune(text)), err)
	return text, err
}

func (a *App) reportDesktopBotSchedule(task *scheduler.ScheduledTask, text string, result desktopBotRelayResult, runErr error) {
	if a == nil || task == nil {
		return
	}
	botID := strings.TrimSpace(task.DesktopBotID)
	owner := strings.TrimSpace(task.DesktopBotOwner)
	if owner == "" {
		owner = "local"
	}
	requestID := fmt.Sprintf("desktop-bot-sched-%s-%d", strings.TrimSpace(task.ID), time.Now().UnixNano())
	shots := storeDesktopBotShots(botID, cleanDesktopBotImages(result.Images))
	var images []DesktopBotImage
	if len(shots) == 0 {
		images = cleanDesktopBotImages(result.Images)
	}
	saved, left := storeDesktopBotFiles(botID, cleanDesktopBotFiles(result.Files))
	payload := map[string]any{
		"request_id":      requestID,
		"session_key":     desktopBotLocalSessionKey(owner, botID),
		"bot_id":          botID,
		"owner_id":        owner,
		"schedule_report": true,
		"text":            text,
	}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	if len(shots) > 0 {
		payload["desktop_shot_paths"] = shots
	}
	if len(images) > 0 {
		payload["desktop_images"] = images
	}
	if len(saved) > 0 {
		payload["local_file_paths"] = saved
	}
	if len(left) > 0 {
		payload["desktop_files"] = left
	}
	if result.UserControl || strings.TrimSpace(result.AttentionReason) != "" {
		payload["desktop_handoff_url"] = result.NovncURL
		payload["desktop_user_control"] = result.UserControl
		payload["attention_reason"] = strings.TrimSpace(result.AttentionReason)
	}
	if question := strings.TrimSpace(result.AskUserQuestion); question != "" || strings.TrimSpace(result.AskUserSecretName) != "" {
		payload["ask_user_input_type"] = strings.TrimSpace(result.AskUserInputType)
		payload["ask_user_secret_name"] = strings.TrimSpace(result.AskUserSecretName)
		payload["ask_user_question"] = question
		payload["ask_user_options_json"] = strings.TrimSpace(result.AskUserOptionsJSON)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if desktopBotReportSink != nil {
		desktopBotReportSink(string(raw))
	}
	a.emitEvent("desktop-bot-report", string(raw))
}
