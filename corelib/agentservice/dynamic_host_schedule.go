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

// reviewedHostScheduleArgs is the shared admission record. The GUI aliases
// the same struct, so a field cannot exist on only one surface.
type reviewedHostScheduleArgs = scheduler.AdmitFields

func reviewedHostScheduleInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]interface{}{"type": "string"},
			"task_action": map[string]interface{}{"type": "string"},
			"id":          map[string]interface{}{"type": "string"},
			"status": map[string]interface{}{
				"type":        "string",
				"enum":        coretool.ScheduleStatusOptions(),
				"description": "Record state, not an operation. On create this is the initial state: omit it or send active for a running record, or paused to store it paused. An existing record changes status only together with its id.",
			},
			"hour":             map[string]interface{}{"type": "integer"},
			"minute":           map[string]interface{}{"type": "integer"},
			"day_of_week":      map[string]interface{}{"type": "integer"},
			"day_of_month":     map[string]interface{}{"type": "integer"},
			"interval_minutes": map[string]interface{}{"type": "integer"},
			"start_date":       map[string]interface{}{"type": "string", "description": "Inclusive first day, YYYY-MM-DD."},
			"end_date":         map[string]interface{}{"type": "string", "description": "Inclusive last day, YYYY-MM-DD. The next day is past the window. A paused record keeps a passed end date until it is made active."},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostScheduleContractDigest() string {
	return coretool.SchemaDigest([]byte("schedule.administer.local:v1:host-schedule-administer"))
}

func reviewedHostScheduleDispatch(a reviewedHostScheduleArgs) (string, bool) {
	return scheduler.AdmitOperation(a)
}

// reviewedHostScheduleStatus maps what the model sent to the canonical value
// the record stores. The vocabulary, its aliases, and the empty-is-unset
// reading all live in the shared schedule contract: this host and the GUI
// surface both serve schedule.administer.local, and two copies of this answer
// would drift.
func reviewedHostScheduleStatus(raw string) (string, bool) {
	return coretool.ScheduleStatusCanonical(raw)
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
		if _, err := screenReviewedHostSchedule(parsed); err != nil {
			return "", err
		}
		return admin.AdministerReviewedHostSchedule(ctx, principal, parsed)
	}
}

func parseReviewedHostScheduleArgs(args map[string]interface{}) (reviewedHostScheduleArgs, error) {
	parsed, err := scheduler.ParseAdmit(args)
	if err != nil {
		return parsed, fmt.Errorf("host_schedule_arguments_rejected")
	}
	return parsed, nil
}

func screenReviewedHostSchedule(args reviewedHostScheduleArgs) (string, error) {
	op, field, ok := scheduler.AdmitScreen(args)
	if field != "" {
		return "", fmt.Errorf("host_schedule_%s_rejected", field)
	}
	if !ok {
		return "", fmt.Errorf("host_schedule_field_presence_rejected")
	}
	return op, nil
}

func (c *coreAgentCallbacks) AdministerReviewedHostSchedule(ctx context.Context, principal Principal, args reviewedHostScheduleArgs) (string, error) {
	if c == nil || c.schedules == nil {
		return "", fmt.Errorf("host_schedule_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_schedule_principal_mismatch")
	}
	op, err := screenReviewedHostSchedule(args)
	if err != nil {
		return "", err
	}
	if !reviewedHostTemplateTokenOK(args.Name, reviewedHostScheduleNameMax) {
		return "", fmt.Errorf("host_schedule_name_rejected")
	}
	if !reviewedHostTemplateTokenOK(args.TaskAction, reviewedHostScheduleActionMax) {
		return "", fmt.Errorf("host_schedule_task_action_rejected")
	}
	status, statusOK := reviewedHostScheduleStatus(args.Status)
	if !statusOK {
		return "", fmt.Errorf("host_schedule_status_rejected (%s)", coretool.ScheduleStatusHint())
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
		task := scheduler.AdmitTask(args)
		task.Status = status
		task.InstanceID = strings.TrimSpace(c.instance.ID)
		task.OwnerTenantID = strings.TrimSpace(c.principal.TenantID)
		task.OwnerUserID = strings.TrimSpace(c.principal.UserID)
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
		if _, ok := c.visibleSchedule(args.ID); !ok {
			return "", fmt.Errorf("host_schedule_not_found")
		}
		if err := c.schedules.UpdateState(args.ID, scheduler.AdmitPatch(args), status); err != nil {
			return "", err
		}
		updated := c.schedules.Get(args.ID)
		if updated == nil {
			return "", fmt.Errorf("host_schedule_update_failed")
		}
		c.rememberAdministeredScheduleID(args.ID)
		return reviewedHostScheduleProjection("updated", updated), nil
	case "delete":
		if _, ok := c.visibleSchedule(args.ID); !ok {
			return "", fmt.Errorf("host_schedule_not_found")
		}
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
		listed := c.schedulesForCaller()
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

func (c *coreAgentCallbacks) schedulesForCaller() []scheduler.ScheduledTask {
	if c == nil || c.schedules == nil {
		return nil
	}
	listed := c.schedules.List()
	instanceID := strings.TrimSpace(c.instance.ID)
	if instanceID == "" {
		return listed
	}
	out := make([]scheduler.ScheduledTask, 0, len(listed))
	for _, item := range listed {
		if item.InstanceID == instanceID {
			out = append(out, item)
		}
	}
	return out
}

func (c *coreAgentCallbacks) visibleSchedule(id string) (*scheduler.ScheduledTask, bool) {
	if c == nil || c.schedules == nil {
		return nil, false
	}
	task := c.schedules.Get(id)
	if task == nil {
		return nil, false
	}
	instanceID := strings.TrimSpace(c.instance.ID)
	if instanceID != "" && task.InstanceID != instanceID {
		return nil, false
	}
	return task, true
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
	return fmt.Sprintf("%s [%s] ID=%s 时间=%s 操作=%s 下次=%s",
		task.Name, task.Status, task.ID, scheduler.FormatSchedule(task), task.Action, next)
}
