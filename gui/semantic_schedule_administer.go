package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedScheduleAdapter        = "semantic_administer_trusted_schedule"
	semanticTrustedScheduleImplementation = "trusted-schedule-administer-v1"
	semanticTrustedScheduleNameMax        = 200
	semanticTrustedScheduleActionMax      = 2000
	semanticTrustedScheduleTimeout        = 10 * time.Second
)

type semanticTrustedScheduleArgs struct {
	Name            string
	TaskAction      string
	ID              string
	Status          string
	StartDate       string
	EndDate         string
	Hour            int
	Minute          int
	DayOfWeek       int
	DayOfMonth      int
	IntervalMinutes int
	HasHour         bool
	HasMinute       bool
	HasDayOfWeek    bool
	HasDayOfMonth   bool
	HasInterval     bool
}

// schedule.manage.local is unpublished alongside schedule.administer.local.
// No intent rule maps to it, and the record editing and channel firing it used
// to combine are now separate trusted capabilities. Leaving it published only
// kept the legacy multiplexer's schema — a model-writable user_id, group_id and
// open delivery object — reachable in the managed catalog.
func semanticUnpublishedLegacyScheduleProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		switch provision.Capability {
		case tool.CapabilityScheduleAdministerLocal, tool.CapabilityScheduleManageLocal:
			return true
		}
	}
	return false
}

func semanticTrustedScheduleDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedScheduleAdapter,
			"description": "Read or update host-local scheduled task records. Field presence decides create, update, delete, or list. This does not dispatch or fire.",
			"parameters":  semanticTrustedScheduleInvocationSchema(),
		},
	}
}

func semanticTrustedScheduleInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":             map[string]interface{}{"type": "string"},
			"task_action":      map[string]interface{}{"type": "string"},
			"id":               map[string]interface{}{"type": "string"},
			"status":           map[string]interface{}{"type": "string"},
			"hour":             map[string]interface{}{"type": "integer"},
			"minute":           map[string]interface{}{"type": "integer"},
			"day_of_week":      map[string]interface{}{"type": "integer"},
			"day_of_month":     map[string]interface{}{"type": "integer"},
			"interval_minutes": map[string]interface{}{"type": "integer"},
			"start_date":       map[string]interface{}{"type": "string"},
			"end_date":         map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedScheduleArgsAllowed(args map[string]interface{}) (semanticTrustedScheduleArgs, error) {
	var parsed semanticTrustedScheduleArgs
	if len(args) > 11 {
		return parsed, fmt.Errorf("trusted_schedule_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "name", "task_action", "id", "status", "start_date", "end_date":
			value, ok := raw.(string)
			if !ok {
				return parsed, fmt.Errorf("trusted_schedule_arguments_rejected")
			}
			value = strings.TrimSpace(value)
			switch key {
			case "name":
				parsed.Name = value
			case "task_action":
				parsed.TaskAction = value
			case "id":
				parsed.ID = value
			case "status":
				parsed.Status = value
			case "start_date":
				parsed.StartDate = value
			case "end_date":
				parsed.EndDate = value
			}
		case "hour", "minute", "day_of_week", "day_of_month", "interval_minutes":
			n, ok := semanticTrustedConfigInt(raw)
			if !ok {
				return parsed, fmt.Errorf("trusted_schedule_arguments_rejected")
			}
			switch key {
			case "hour":
				parsed.Hour, parsed.HasHour = n, true
			case "minute":
				parsed.Minute, parsed.HasMinute = n, true
			case "day_of_week":
				parsed.DayOfWeek, parsed.HasDayOfWeek = n, true
			case "day_of_month":
				parsed.DayOfMonth, parsed.HasDayOfMonth = n, true
			case "interval_minutes":
				parsed.IntervalMinutes, parsed.HasInterval = n, true
			}
		default:
			return parsed, fmt.Errorf("trusted_schedule_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedScheduleDispatch(parsed); !ok {
		return parsed, fmt.Errorf("trusted_schedule_field_presence_rejected")
	}
	return parsed, nil
}

func (a semanticTrustedScheduleArgs) hasCreateFields() bool {
	return a.Name != "" && a.TaskAction != "" && a.HasHour
}

func (a semanticTrustedScheduleArgs) hasUpdateExtras() bool {
	return a.Name != "" || a.TaskAction != "" || a.HasHour || a.HasMinute || a.HasDayOfWeek || a.HasDayOfMonth || a.HasInterval || a.Status != "" || a.StartDate != "" || a.EndDate != ""
}

func semanticTrustedScheduleDispatch(a semanticTrustedScheduleArgs) (string, bool) {
	if a.ID == "" {
		if a.hasCreateFields() && a.Status == "" {
			return "create", true
		}
		if a.Name == "" && a.TaskAction == "" && !a.HasHour && !a.hasUpdateExtras() {
			return "list", true
		}
		return "", false
	}
	if a.hasUpdateExtras() {
		return "update", true
	}
	return "delete", true
}

func semanticTrustedScheduleStatus(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "paused", "pause":
		return "paused", true
	case "active", "resume":
		return "active", true
	case "":
		return "", true
	default:
		return "", false
	}
}

func (h *IMMessageHandler) administerTrustedSchedule(principalID string, args semanticTrustedScheduleArgs) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_schedule_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_schedule_principal_required")
	}
	if h.semanticTrustedSchedule != nil {
		return h.semanticTrustedSchedule(principalID, args)
	}
	op, ok := semanticTrustedScheduleDispatch(args)
	if !ok {
		return "", fmt.Errorf("trusted_schedule_field_presence_rejected")
	}
	if !semanticTrustedTemplateTokenOK(args.Name, semanticTrustedScheduleNameMax) {
		return "", fmt.Errorf("trusted_schedule_name_rejected")
	}
	if !semanticTrustedTemplateTokenOK(args.TaskAction, semanticTrustedScheduleActionMax) {
		return "", fmt.Errorf("trusted_schedule_task_action_rejected")
	}
	if args.HasHour && (args.Hour < 0 || args.Hour > 23) {
		return "", fmt.Errorf("trusted_schedule_hour_rejected")
	}
	if args.HasMinute && (args.Minute < 0 || args.Minute > 59) {
		return "", fmt.Errorf("trusted_schedule_minute_rejected")
	}
	if args.HasDayOfWeek && (args.DayOfWeek < -1 || args.DayOfWeek > 6) {
		return "", fmt.Errorf("trusted_schedule_day_of_week_rejected")
	}
	if args.HasDayOfMonth && args.DayOfMonth != -1 && (args.DayOfMonth < 1 || args.DayOfMonth > 31) {
		return "", fmt.Errorf("trusted_schedule_day_of_month_rejected")
	}
	if args.HasInterval && args.IntervalMinutes < 0 {
		return "", fmt.Errorf("trusted_schedule_interval_rejected")
	}
	status, statusOK := semanticTrustedScheduleStatus(args.Status)
	if !statusOK {
		return "", fmt.Errorf("trusted_schedule_status_rejected")
	}
	manager := h.scheduledTaskManagerForTool()
	if manager == nil {
		return "", fmt.Errorf("trusted_schedule_unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedScheduleTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	switch op {
	case "create":
		task := scheduler.ScheduledTask{
			Name:            args.Name,
			Action:          args.TaskAction,
			Hour:            args.Hour,
			Minute:          0,
			DayOfWeek:       -1,
			DayOfMonth:      -1,
			IntervalMinutes: 0,
			StartDate:       args.StartDate,
			EndDate:         args.EndDate,
		}
		if args.HasMinute {
			task.Minute = args.Minute
		}
		if args.HasDayOfWeek {
			task.DayOfWeek = args.DayOfWeek
		}
		if args.HasDayOfMonth {
			task.DayOfMonth = args.DayOfMonth
		}
		if args.HasInterval {
			task.IntervalMinutes = args.IntervalMinutes
		}
		id, err := manager.Add(task)
		if err != nil {
			return "", err
		}
		created := manager.Get(id)
		if created == nil {
			return "", fmt.Errorf("trusted_schedule_create_failed")
		}
		if created.Delivery != nil {
			_ = manager.Delete(id)
			return "", fmt.Errorf("trusted_schedule_delivery_rejected")
		}
		h.rememberAdministeredTaskID(id)
		h.emitAppEvent("scheduled-tasks-changed")
		return semanticTrustedScheduleProjection("created", created), nil
	case "update":
		current := manager.Get(args.ID)
		if current == nil {
			return "", fmt.Errorf("trusted_schedule_not_found")
		}
		patch := map[string]interface{}{}
		if args.Name != "" {
			patch["name"] = args.Name
		}
		if args.TaskAction != "" {
			patch["action"] = args.TaskAction
		}
		if args.HasHour {
			patch["hour"] = args.Hour
		}
		if args.HasMinute {
			patch["minute"] = args.Minute
		}
		if args.HasDayOfWeek {
			patch["day_of_week"] = args.DayOfWeek
		}
		if args.HasDayOfMonth {
			patch["day_of_month"] = args.DayOfMonth
		}
		if args.HasInterval {
			patch["interval_minutes"] = args.IntervalMinutes
		}
		if args.StartDate != "" {
			patch["start_date"] = args.StartDate
		}
		if args.EndDate != "" {
			patch["end_date"] = args.EndDate
		}
		if len(patch) > 0 {
			if err := manager.Update(args.ID, patch); err != nil {
				return "", err
			}
		}
		if status == "paused" {
			if err := manager.Pause(args.ID); err != nil {
				return "", err
			}
		}
		if status == "active" {
			if err := manager.Resume(args.ID); err != nil {
				return "", err
			}
		}
		updated := manager.Get(args.ID)
		if updated == nil {
			return "", fmt.Errorf("trusted_schedule_update_failed")
		}
		h.rememberAdministeredTaskID(args.ID)
		h.emitAppEvent("scheduled-tasks-changed")
		return semanticTrustedScheduleProjection("updated", updated), nil
	case "delete":
		if err := manager.Delete(args.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return "", fmt.Errorf("trusted_schedule_not_found")
			}
			return "", err
		}
		h.emitAppEvent("scheduled-tasks-changed")
		return "定时任务已删除。", nil
	default:
		listed := manager.List()
		if len(listed) == 0 {
			return "当前没有定时任务。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个定时任务:\n", len(listed))
		for _, item := range listed {
			fmt.Fprintf(&b, "- %s\n", semanticTrustedScheduleLine(item))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func semanticTrustedScheduleProjection(kind string, task *scheduler.ScheduledTask) string {
	if task == nil {
		return "当前没有定时任务。"
	}
	line := semanticTrustedScheduleLine(*task)
	switch kind {
	case "created":
		return "定时任务已创建: " + line
	case "updated":
		return "定时任务已更新: " + line
	default:
		return line
	}
}

func semanticTrustedScheduleLine(task scheduler.ScheduledTask) string {
	next := "-"
	if task.NextRunAt != nil {
		next = task.NextRunAt.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%s [%s] ID=%s 时间=%02d:%02d 操作=%s 下次=%s",
		task.Name, task.Status, task.ID, task.Hour, task.Minute, task.Action, next)
}

func semanticTrustedScheduleResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_schedule_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_schedule_empty")
	}
	return text, nil
}
