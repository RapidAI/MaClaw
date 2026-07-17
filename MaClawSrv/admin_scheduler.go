package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// handleAdminSchedulerTasks lists live tasks from the running manager (or file fallback via status).
func (s *HTTPServer) handleAdminSchedulerTasks(w http.ResponseWriter, r *http.Request) {
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "scheduler not running (set MACLAW_ENABLE_SCHEDULER=true and restart)",
		})
		return
	}
	tasks := mgr.List()
	// Redact sensitive paths in list responses for admin UI.
	root := ""
	if s.svc != nil {
		root = s.svc.DataRoot()
	}
	out := make([]scheduler.ScheduledTask, len(tasks))
	copy(out, tasks)
	for i := range out {
		out[i].Name = redactSupportBundleText(root, out[i].Name)
		out[i].Action = redactSupportBundleText(root, out[i].Action)
		out[i].LastResult = redactSupportBundleText(root, out[i].LastResult)
		out[i].LastError = redactSupportBundleText(root, out[i].LastError)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": out,
		"count": len(out),
	})
}

type adminSchedulerTaskBody struct {
	Name            string                 `json:"name"`
	Action          string                 `json:"action"`
	TaskAction      string                 `json:"task_action"`
	Hour            *int                   `json:"hour"`
	Minute          *int                   `json:"minute"`
	DayOfWeek       *int                   `json:"day_of_week"`
	DayOfMonth      *int                   `json:"day_of_month"`
	IntervalMinutes *int                   `json:"interval_minutes"`
	StartDate       *string                `json:"start_date"`
	EndDate         *string                `json:"end_date"`
	TaskType        *string                `json:"task_type"`
	Delivery        json.RawMessage        `json:"delivery"`
	// Shorthand delivery fields (same as agent tool).
	Channel        string `json:"channel"`
	GroupID        string `json:"group_id"`
	GroupName      string `json:"group_name"`
	UserID         string `json:"user_id"`
	FailOnError    *bool  `json:"fail_on_error"`
	MentionAll     *bool  `json:"mention_all"`
	MentionUserIDs string `json:"mention_user_ids"`
}

func (s *HTTPServer) handleAdminSchedulerCreateTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	var body adminSchedulerTaskBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	name := strings.TrimSpace(body.Name)
	action := strings.TrimSpace(body.TaskAction)
	if action == "" {
		action = strings.TrimSpace(body.Action)
	}
	if name == "" || action == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and action/task_action are required"})
		return
	}
	hour := 9
	if body.Hour != nil {
		hour = *body.Hour
	}
	if hour < 0 || hour > 23 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "hour must be 0-23"})
		return
	}
	minute := 0
	if body.Minute != nil {
		minute = *body.Minute
	}
	if minute < 0 || minute > 59 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "minute must be 0-59"})
		return
	}
	dow, dom := -1, -1
	if body.DayOfWeek != nil {
		dow = *body.DayOfWeek
	}
	if body.DayOfMonth != nil {
		dom = *body.DayOfMonth
	}
	interval := 0
	if body.IntervalMinutes != nil {
		interval = *body.IntervalMinutes
	}
	t := scheduler.ScheduledTask{
		Name:            name,
		Action:          action,
		Hour:            hour,
		Minute:          minute,
		DayOfWeek:       dow,
		DayOfMonth:      dom,
		IntervalMinutes: interval,
	}
	if body.StartDate != nil {
		t.StartDate = strings.TrimSpace(*body.StartDate)
	}
	if body.EndDate != nil {
		t.EndDate = strings.TrimSpace(*body.EndDate)
	}
	if body.TaskType != nil {
		t.TaskType = strings.TrimSpace(*body.TaskType)
	}
	d, err := adminParseDelivery(s, body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	t.Delivery = d
	id, err := mgr.Add(t)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_create", "scheduled_task", id, map[string]string{
		"name": name, "remote_ip": requestClientIP(r),
	})
	got := mgr.Get(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": id, "task": got})
}

func (s *HTTPServer) handleAdminSchedulerUpdateTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	id := strings.TrimSpace(r.PathValue("taskId"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing task id"})
		return
	}
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	// Normalize task_action → action for Manager.Update.
	if ta, ok := body["task_action"].(string); ok && strings.TrimSpace(ta) != "" {
		body["action"] = strings.TrimSpace(ta)
	}
	if err := applyDeliveryUpdateArgs(s.svc, mgr, id, body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := mgr.Update(id, body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_update", "scheduled_task", id, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]interface{}{"task": mgr.Get(id)})
}

func (s *HTTPServer) handleAdminSchedulerDeleteTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	id := strings.TrimSpace(r.PathValue("taskId"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing task id"})
		return
	}
	if err := mgr.Delete(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_delete", "scheduled_task", id, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func (s *HTTPServer) handleAdminSchedulerTriggerTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	id := strings.TrimSpace(r.PathValue("taskId"))
	if err := mgr.TriggerNow(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_trigger", "scheduled_task", id, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "triggered", "id": id})
}

func (s *HTTPServer) handleAdminSchedulerPauseTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	id := strings.TrimSpace(r.PathValue("taskId"))
	if err := mgr.Pause(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_pause", "scheduled_task", id, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]interface{}{"task": mgr.Get(id)})
}

func (s *HTTPServer) handleAdminSchedulerResumeTask(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduler not running"})
		return
	}
	id := strings.TrimSpace(r.PathValue("taskId"))
	if err := mgr.Resume(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.scheduler_resume", "scheduled_task", id, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]interface{}{"task": mgr.Get(id)})
}

func (s *HTTPServer) handleAdminSchedulerDeliveryAudit(w http.ResponseWriter, r *http.Request) {
	root := ""
	if s.svc != nil {
		root = s.svc.DataRoot()
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := parsePositiveInt(raw, 100); err == nil {
			limit = n
		}
	}
	items := listDeliveryAudit(root, limit)
	// Light redaction on error strings.
	for i := range items {
		items[i].Error = redactSupportBundleText(root, items[i].Error)
		items[i].TaskName = redactSupportBundleText(root, items[i].TaskName)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items, "count": len(items)})
}

func adminParseDelivery(s *HTTPServer, body adminSchedulerTaskBody) (*scheduler.TaskDelivery, error) {
	args := map[string]interface{}{}
	if len(body.Delivery) > 0 && string(body.Delivery) != "null" {
		var raw interface{}
		if err := json.Unmarshal(body.Delivery, &raw); err != nil {
			return nil, fmt.Errorf("invalid delivery: %w", err)
		}
		args["delivery"] = raw
	}
	if body.Channel != "" {
		args["channel"] = body.Channel
	}
	if body.GroupID != "" {
		args["group_id"] = body.GroupID
	}
	if body.GroupName != "" {
		args["group_name"] = body.GroupName
	}
	if body.UserID != "" {
		args["user_id"] = body.UserID
	}
	if body.FailOnError != nil {
		args["fail_on_error"] = *body.FailOnError
	}
	if body.MentionAll != nil {
		args["mention_all"] = *body.MentionAll
	}
	if body.MentionUserIDs != "" {
		args["mention_user_ids"] = body.MentionUserIDs
	}
	var svc *agentservice.Service
	if s != nil {
		svc = s.svc
	}
	return parseAndResolveSrvDelivery(svc, args)
}

func strMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// applyDeliveryUpdateArgs mutates args so Manager.Update receives a coherent delivery.
func applyDeliveryUpdateArgs(svc *agentservice.Service, mgr *scheduler.Manager, id string, args map[string]interface{}) error {
	if args == nil || mgr == nil {
		return nil
	}
	var current *scheduler.TaskDelivery
	if t := mgr.Get(id); t != nil {
		current = t.Delivery
	}
	return scheduler.PrepareDeliveryForUpdate(current, args, func(a map[string]interface{}) (*scheduler.TaskDelivery, error) {
		return parseAndResolveSrvDelivery(svc, a)
	})
}

// adminBodyReplacesDelivery / adminBodyTouchesDelivery kept as thin aliases for tests.
func adminBodyReplacesDelivery(body map[string]interface{}) bool {
	return scheduler.ArgsReplaceDelivery(body)
}

func adminBodyTouchesDelivery(body map[string]interface{}) bool {
	return scheduler.ArgsTouchDelivery(body)
}

func parsePositiveInt(raw string, def int) (int, error) {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &n); err != nil {
		return def, err
	}
	if n <= 0 {
		return def, fmt.Errorf("non-positive")
	}
	if n > deliveryAuditMaxRead {
		n = deliveryAuditMaxRead
	}
	return n, nil
}
