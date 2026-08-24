package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostScheduleProviderID     = "core-schedule"
	reviewedHostScheduleImplementation = "local"
	reviewedHostScheduleAdapterName    = "host_schedule_administer_local"
	reviewedHostScheduleNameMax        = 200
	reviewedHostScheduleActionMax      = 2000
)

type reviewedHostScheduleAdministrator interface {
	AdministerReviewedHostSchedule(ctx context.Context, principal Principal, args reviewedHostScheduleArgs) (string, error)
}

type reviewedHostScheduleArgs struct {
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

func reviewedHostScheduleInvocationSchema() map[string]interface{} {
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

func reviewedHostScheduleContractDigest() string {
	return coretool.SchemaDigest([]byte("schedule.administer.local:v1:host-schedule-administer"))
}

func (a reviewedHostScheduleArgs) hasCreateFields() bool {
	return a.Name != "" && a.TaskAction != "" && a.HasHour
}

func (a reviewedHostScheduleArgs) hasUpdateExtras() bool {
	return a.Name != "" || a.TaskAction != "" || a.HasHour || a.HasMinute || a.HasDayOfWeek || a.HasDayOfMonth || a.HasInterval || a.Status != "" || a.StartDate != "" || a.EndDate != ""
}

func reviewedHostScheduleDispatch(a reviewedHostScheduleArgs) (string, bool) {
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

func reviewedHostScheduleStatus(raw string) (string, bool) {
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

func reviewedHostScheduleInt(raw interface{}) (int, bool) {
	switch n := raw.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// ProjectReviewedHostScheduleProvider projects the host-owned local schedule
// record. It is not a Skill/MCP discovery entry and must not import the GUI
// manage_schedule or schedule_administer action catalog. Field presence
// decides create/update/delete/list. action, channel, destination,
// group_name, delivery, list_targets, and run are rejected. This is not
// schedule.dispatch.channel, schedule.manage.local, or task.track.local.
// The host process observes the schedule store and does not start a fire
// executor, so the handler result is the local completion receipt.
func ProjectReviewedHostScheduleProvider(admin reviewedHostScheduleAdministrator) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if admin == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host schedule administrator is unavailable")
	}
	parameters := reviewedHostScheduleInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host schedule schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostScheduleContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-schedule-name-action-hour-or-id-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostScheduleAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostScheduleProviderID,
			ImplementationID: reviewedHostScheduleImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityScheduleAdminister,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectLocalMutation},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostSchedule(admin)}, nil
}

func AttachReviewedHostScheduleProvider(catalog DynamicSemanticCatalog, admin reviewedHostScheduleAdministrator) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostScheduleProvider(admin)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostSchedule(admin reviewedHostScheduleAdministrator) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if admin == nil {
			return "", fmt.Errorf("host_schedule_unavailable")
		}
		parsed, err := parseReviewedHostScheduleArgs(args)
		if err != nil {
			return "", err
		}
		if _, ok := reviewedHostScheduleDispatch(parsed); !ok {
			return "", fmt.Errorf("host_schedule_field_presence_rejected")
		}
		return admin.AdministerReviewedHostSchedule(ctx, principal, parsed)
	}
}

func parseReviewedHostScheduleArgs(args map[string]interface{}) (reviewedHostScheduleArgs, error) {
	var parsed reviewedHostScheduleArgs
	if len(args) > 11 {
		return parsed, fmt.Errorf("host_schedule_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "name", "task_action", "id", "status", "start_date", "end_date":
			value, ok := raw.(string)
			if !ok {
				return parsed, fmt.Errorf("host_schedule_arguments_rejected")
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
			n, ok := reviewedHostScheduleInt(raw)
			if !ok {
				return parsed, fmt.Errorf("host_schedule_arguments_rejected")
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
			return parsed, fmt.Errorf("host_schedule_arguments_rejected")
		}
	}
	return parsed, nil
}

func (c *coreAgentCallbacks) AdministerReviewedHostSchedule(ctx context.Context, principal Principal, args reviewedHostScheduleArgs) (string, error) {
	if c == nil || c.schedules == nil {
		return "", fmt.Errorf("host_schedule_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_schedule_principal_mismatch")
	}
	op, ok := reviewedHostScheduleDispatch(args)
	if !ok {
		return "", fmt.Errorf("host_schedule_field_presence_rejected")
	}
	if !reviewedHostTemplateTokenOK(args.Name, reviewedHostScheduleNameMax) {
		return "", fmt.Errorf("host_schedule_name_rejected")
	}
	if !reviewedHostTemplateTokenOK(args.TaskAction, reviewedHostScheduleActionMax) {
		return "", fmt.Errorf("host_schedule_task_action_rejected")
	}
	if args.HasHour && (args.Hour < 0 || args.Hour > 23) {
		return "", fmt.Errorf("host_schedule_hour_rejected")
	}
	if args.HasMinute && (args.Minute < 0 || args.Minute > 59) {
		return "", fmt.Errorf("host_schedule_minute_rejected")
	}
	if args.HasDayOfWeek && (args.DayOfWeek < -1 || args.DayOfWeek > 6) {
		return "", fmt.Errorf("host_schedule_day_of_week_rejected")
	}
	if args.HasDayOfMonth && args.DayOfMonth != -1 && (args.DayOfMonth < 1 || args.DayOfMonth > 31) {
		return "", fmt.Errorf("host_schedule_day_of_month_rejected")
	}
	if args.HasInterval && args.IntervalMinutes < 0 {
		return "", fmt.Errorf("host_schedule_interval_rejected")
	}
	status, statusOK := reviewedHostScheduleStatus(args.Status)
	if !statusOK {
		return "", fmt.Errorf("host_schedule_status_rejected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
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
		id, err := c.schedules.Add(task)
		if err != nil {
			return "", err
		}
		created := c.schedules.Get(id)
		if created == nil {
			return "", fmt.Errorf("host_schedule_create_failed")
		}
		if created.Delivery != nil {
			_ = c.schedules.Delete(id)
			return "", fmt.Errorf("host_schedule_delivery_rejected")
		}
		c.rememberAdministeredScheduleID(id)
		return reviewedHostScheduleProjection("created", created), nil
	case "update":
		current := c.schedules.Get(args.ID)
		if current == nil {
			return "", fmt.Errorf("host_schedule_not_found")
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
			if err := c.schedules.Update(args.ID, patch); err != nil {
				return "", err
			}
		}
		if status == "paused" {
			if err := c.schedules.Pause(args.ID); err != nil {
				return "", err
			}
		}
		if status == "active" {
			if err := c.schedules.Resume(args.ID); err != nil {
				return "", err
			}
		}
		updated := c.schedules.Get(args.ID)
		if updated == nil {
			return "", fmt.Errorf("host_schedule_update_failed")
		}
		c.rememberAdministeredScheduleID(args.ID)
		return reviewedHostScheduleProjection("updated", updated), nil
	case "delete":
		if c.scheduleDispatchBindings != nil {
			c.scheduleDispatchBindings.Delete(args.ID)
		}
		if err := c.schedules.Delete(args.ID); err != nil {
			if strings.Contains(err.Error(), "not found") {
				return "", fmt.Errorf("host_schedule_not_found")
			}
			return "", err
		}
		return "定时任务已删除。", nil
	default:
		listed := c.schedules.List()
		if len(listed) == 0 {
			return "当前没有定时任务。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个定时任务:\n", len(listed))
		for _, item := range listed {
			fmt.Fprintf(&b, "- %s\n", reviewedHostScheduleLine(item))
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func reviewedHostScheduleProjection(kind string, task *scheduler.ScheduledTask) string {
	if task == nil {
		return "当前没有定时任务。"
	}
	line := reviewedHostScheduleLine(*task)
	switch kind {
	case "created":
		return "定时任务已创建: " + line
	case "updated":
		return "定时任务已更新: " + line
	default:
		return line
	}
}

func reviewedHostScheduleLine(task scheduler.ScheduledTask) string {
	next := "-"
	if task.NextRunAt != nil {
		next = task.NextRunAt.Format("2006-01-02 15:04")
	}
	return fmt.Sprintf("%s [%s] ID=%s 时间=%02d:%02d 操作=%s 下次=%s",
		task.Name, task.Status, task.ID, task.Hour, task.Minute, task.Action, next)
}
