package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// desktopBotBusySink records schedule runs that are on the shared desktop.
// Tests read it; production also emits desktop-bot-busy for the Bot page.
var desktopBotBusySink func(botID string, busy bool)

// desktopBotBusyCount is how many schedule occurrences of one owner and bot
// are inside relay right now. The Bot page reads it on open, because the
// busy event fired while that page was closed.
var (
	desktopBotBusyMu    sync.Mutex
	desktopBotBusySeq   int64
	desktopBotBusyCount = map[string]int{}
)

func desktopBotBusyKey(owner, botID string) string {
	return owner + "\x00" + botID
}

func noteDesktopBotBusyCount(owner, botID string, busy bool) (int64, int) {
	desktopBotBusyMu.Lock()
	defer desktopBotBusyMu.Unlock()
	desktopBotBusySeq++
	key := desktopBotBusyKey(owner, botID)
	count := desktopBotBusyCount[key]
	if busy {
		count++
	} else if count > 0 {
		count--
	}
	if count <= 0 {
		delete(desktopBotBusyCount, key)
		count = 0
	} else {
		desktopBotBusyCount[key] = count
	}
	return desktopBotBusySeq, count
}

// DesktopBotBusyMark is one bot whose schedule still holds the shared desktop.
type DesktopBotBusyMark struct {
	OwnerID string `json:"owner_id"`
	BotID   string `json:"bot_id"`
	Count   int    `json:"count"`
}

// DesktopBotBusySnapshot is the schedule holds for one account.
// Seq moves forward on every hold change. A page that already applied a
// newer event ignores an older snapshot. Runs are chat turns still on the
// keyboard, including ones whose bubble has aged off the pending list.
type DesktopBotBusySnapshot struct {
	Seq   int64                `json:"seq"`
	Marks []DesktopBotBusyMark `json:"marks"`
	Runs  []string             `json:"runs"`
}

// GetDesktopBotBusy lists schedule holds for owner. An empty owner lists every
// account, which is what a signed-out page uses to keep the keyboard closed.
func (a *App) GetDesktopBotBusy(owner string) DesktopBotBusySnapshot {
	owner = strings.TrimSpace(owner)
	desktopBotBusyMu.Lock()
	out := DesktopBotBusySnapshot{Seq: desktopBotBusySeq, Marks: []DesktopBotBusyMark{}}
	for key, count := range desktopBotBusyCount {
		if count <= 0 {
			continue
		}
		markOwner, botID, ok := strings.Cut(key, "\x00")
		if !ok || botID == "" || (owner != "" && markOwner != owner) {
			continue
		}
		out.Marks = append(out.Marks, DesktopBotBusyMark{OwnerID: markOwner, BotID: botID, Count: count})
	}
	sort.Slice(out.Marks, func(i, j int) bool {
		if out.Marks[i].BotID == out.Marks[j].BotID {
			return out.Marks[i].OwnerID < out.Marks[j].OwnerID
		}
		return out.Marks[i].BotID < out.Marks[j].BotID
	})
	desktopBotBusyMu.Unlock()
	out.Runs = desktopDrivingRuns(owner)
	return out
}

func desktopRunOwner(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	split := strings.LastIndex(sessionKey, ":")
	if split <= 0 {
		return ""
	}
	return strings.TrimSpace(sessionKey[:split])
}

// desktopDrivingRuns lists bots whose cloud-desktop turn is still on the
// keyboard. A turn that stopped to ask the person is not driving. An empty
// owner keeps a turn whose session did not name an account, so that keyboard
// stays closed for whoever has the page open.
func desktopDrivingRuns(owner string) []string {
	owner = strings.TrimSpace(owner)
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	seen := map[string]struct{}{}
	for _, run := range activeDesktopRuns {
		if run == nil || run.WaitingForUser {
			continue
		}
		botID := strings.TrimSpace(run.BotID)
		if botID == "" {
			continue
		}
		runOwner := desktopRunOwner(run.SessionKey)
		if owner != "" && runOwner != "" && runOwner != owner {
			continue
		}
		seen[botID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for botID := range seen {
		out = append(out, botID)
	}
	sort.Strings(out)
	return out
}

func desktopRunDriving(botID string) (bool, string) {
	botID = strings.TrimSpace(botID)
	activeDesktopRunsMu.Lock()
	defer activeDesktopRunsMu.Unlock()
	owner := ""
	driving := false
	prefix := botID + desktopRunSeparator
	for key, run := range activeDesktopRuns {
		if run == nil || !strings.HasPrefix(key, prefix) || run.WaitingForUser {
			continue
		}
		driving = true
		if owner == "" {
			owner = desktopRunOwner(run.SessionKey)
		}
	}
	return driving, owner
}

// publishDesktopRunDriving tells the Bot page whether this bot's turn still
// has the shared keyboard. The page may have dropped the chat bubble.
func (a *App) publishDesktopRunDriving(botID string) {
	botID = strings.TrimSpace(botID)
	if a == nil || botID == "" {
		return
	}
	driving, owner := desktopRunDriving(botID)
	raw, err := json.Marshal(map[string]any{
		"bot_id":      botID,
		"owner_id":    owner,
		"run_driving": driving,
	})
	if err != nil {
		return
	}
	a.emitEvent("desktop-bot-busy", string(raw))
}

func (a *App) noteDesktopBotBusy(botID, owner string, busy bool) {
	botID = strings.TrimSpace(botID)
	if botID == "" {
		return
	}
	owner = strings.TrimSpace(owner)
	seq, count := noteDesktopBotBusyCount(owner, botID, busy)
	if desktopBotBusySink != nil {
		desktopBotBusySink(botID, busy)
	}
	if a == nil {
		return
	}
	raw, err := json.Marshal(map[string]any{
		"bot_id":   botID,
		"owner_id": owner,
		"busy":     busy,
		"count":    count,
		"seq":      seq,
	})
	if err != nil {
		return
	}
	a.emitEvent("desktop-bot-busy", string(raw))
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
	// The chat has no pending bubble for a schedule, but the run still uses
	// the shared desktop. The Bot page locks takeover until this returns.
	// A login handoff is the end of that hold: the person has to take the keys.
	owner := task.DesktopBotOwner
	held := true
	releaseDesktop := func() {
		if !held {
			return
		}
		held = false
		a.noteDesktopBotBusy(task.DesktopBotID, owner, false)
	}
	a.noteDesktopBotBusy(task.DesktopBotID, owner, true)
	defer releaseDesktop()
	// A scheduled occurrence has no client request behind it, so there is no
	// request id to name the run by. setActiveDesktopRunID skips empty ones,
	// which leaves this turn unaddressable for supplements — it has no
	// conversation waiting to add anything.
	result, err := a.relayDesktopBotWhenFree(ctx, task.DesktopBotID, order, "execute", "", nil)
	if errors.Is(err, errDesktopOccurrenceSkipped) {
		log.Printf("[desktop-bot-schedule] skip id=%s bot=%q reason=busy_timeout", task.ID, task.DesktopBotID)
		return "", nil
	}
	if result.UserControl || strings.TrimSpace(result.AttentionReason) != "" {
		releaseDesktop()
	}
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
