package collaboration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for collaboration tasks.
type Handler struct {
	svc       *Service
	auditRepo *audit.Repo
}

// NewHandler creates a Handler.
func NewHandler(svc *Service, auditRepo *audit.Repo) *Handler {
	return &Handler{svc: svc, auditRepo: auditRepo}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/collaborations", h.handleList)
	mux.HandleFunc("/admin/collaborations/", h.handleByID)
	mux.HandleFunc("/admin/collaborations-settings", h.handleSettings)
	mux.HandleFunc("/admin/collaborations-settings/actions", h.handleRoleAction)
}

// RegisterClientRoutes registers client-facing routes.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/collaborations", h.handleClientList)
	mux.HandleFunc("/runtime/collaboration/create", h.handleCreate)
	mux.HandleFunc("/runtime/collaboration/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("/runtime/collaboration/", h.handleTransition)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		tasks, err := h.svc.ListAll(tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"tasks": toTaskDTOs(tasks)})
	case http.MethodPost:
		var req CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		task, err := h.svc.Create(tid, req)
		if err != nil {
			response.BadRequest(w, "CREATE_FAILED", err.Error())
			return
		}
		h.recordTaskCreationAudit(tid, req, task)
		response.Created(w, toTaskDTO(task))
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	switch r.Method {
	case http.MethodGet:
		overview, err := h.svc.GetRoutingOverview(tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, overview)
	case http.MethodPost:
		var req RoutingSettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		if err := h.svc.SaveRoutingSettings(tid, req); err != nil {
			response.BadRequest(w, "SAVE_FAILED", err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "ok"})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleRoleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	var req struct {
		RoleCode string `json:"role_code"`
		Action   string `json:"action"`
		ActorID  string `json:"actor_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	beforeSnapshot := h.describeRoleSnapshot(tid, req.RoleCode)
	settings, err := h.svc.ExecuteRoleRoutingAction(tid, req.RoleCode, req.Action)
	if err != nil {
		response.BadRequest(w, "ACTION_FAILED", err.Error())
		return
	}
	afterSnapshot := h.describeRoleSnapshot(tid, req.RoleCode)
	if h.auditRepo != nil {
		actorID := strings.TrimSpace(req.ActorID)
		if actorID == "" {
			actorID = "board_console"
		}
		_ = h.auditRepo.Insert(tid, &audit.ProxyLog{
			ProviderID: "iworkercenter",
			Model:      "routing_control",
			WorkType:   "role_routing_action",
			CostTier:   "internal",
			Status:     "ok",
			Summary:    fmt.Sprintf("%s executed %s on %s", actorID, strings.TrimSpace(req.Action), strings.TrimSpace(req.RoleCode)),
			ErrorMsg:   fmt.Sprintf("before: %s\nafter: %s", beforeSnapshot, afterSnapshot),
		})
	}
	response.OK(w, map[string]any{"status": "ok", "settings": settings})
}

func (h *Handler) describeRoleSnapshot(tenantID, roleCode string) string {
	roleCode = strings.TrimSpace(roleCode)
	if roleCode == "" || h.svc == nil || h.svc.colleagueRp == nil {
		return "snapshot unavailable"
	}
	settings, err := h.svc.GetRoutingSettings(tenantID)
	if err != nil {
		return "snapshot unavailable"
	}
	colleagues, err := h.svc.colleagueRp.ListByRoleCode(tenantID, roleCode)
	if err != nil || len(colleagues) == 0 {
		return fmt.Sprintf("role=%s no_active_colleagues", roleCode)
	}
	overview := BuildRoutingOverview(settings, colleagues, time.Now())
	primary := strings.TrimSpace(settings.PrimaryColleagueByRole[roleCode])
	strategy := strings.TrimSpace(settings.RoleStrategies[roleCode])
	if strategy == "" {
		strategy = settings.DefaultStrategy
	}
	return fmt.Sprintf(
		"role=%s strategy=%s primary=%s active=%d standby=%d unhealthy=%d",
		roleCode,
		strategy,
		primary,
		overview.ActiveCount,
		overview.StandbyCount,
		overview.UnhealthyCount,
	)
}

func (h *Handler) handleClientList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.RequestTenantID(r)
	colleagueID := r.URL.Query().Get("colleague_id")
	if colleagueID == "" {
		tasks, _ := h.svc.ListAll(tid)
		response.OK(w, map[string]any{"tasks": toTaskDTOs(tasks)})
		return
	}
	tasks, err := h.svc.ListByColleague(tid, colleagueID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"tasks": toTaskDTOs(tasks)})
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	task, err := h.svc.Create(tid, req)
	if err != nil {
		response.BadRequest(w, "CREATE_FAILED", err.Error())
		return
	}
	h.recordTaskCreationAudit(tid, req, task)
	response.Created(w, toTaskDTO(task))
}

func (h *Handler) recordTaskCreationAudit(tenantID string, req CreateRequest, task *Task) {
	if h.auditRepo == nil || task == nil {
		return
	}
	sourceType := strings.TrimSpace(req.SourceType)
	if sourceType == "" {
		return
	}
	if sourceType == "executive_skill" {
		summary := fmt.Sprintf("Task created from executive skill %s", strings.TrimSpace(req.SourceSkillTitle))
		detail := fmt.Sprintf(
			"task_id: %s | role_code: %s | skill_id: %s | focus: %s | task: %s",
			task.ID,
			strings.TrimSpace(req.SourceFocusRoleCode),
			strings.TrimSpace(req.SourceSkillID),
			strings.TrimSpace(req.SourceFocusTitle),
			task.Title,
		)
		_ = h.auditRepo.Insert(tenantID, &audit.ProxyLog{
			ProviderID: "iworkercenter",
			Model:      "executive_action",
			WorkType:   "executive_action_task",
			CostTier:   "internal",
			Status:     "ok",
			Summary:    summary,
			ErrorMsg:   detail,
		})
	}
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	var req struct {
		ColleagueID string `json:"colleague_id"`
		ObservedAt  string `json:"observed_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	observedAt := time.Now()
	if strings.TrimSpace(req.ObservedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ObservedAt)); err == nil {
			observedAt = parsed
		}
	}
	if err := h.svc.RecordHeartbeat(tid, req.ColleagueID, observedAt); err != nil {
		response.BadRequest(w, "HEARTBEAT_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	rest := strings.TrimPrefix(r.URL.Path, "/admin/collaborations/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	id := parts[0]
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "task id required")
		return
	}

	if len(parts) == 2 && parts[1] == "events" {
		events, err := h.svc.ListEvents(tid, id)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"events": toEventDTOs(events)})
		return
	}

	task, err := h.svc.GetByID(tid, id)
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "task not found")
		return
	}
	response.OK(w, toTaskDTO(task))
}

func (h *Handler) handleTransition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tid := tenant.RequestTenantID(r)
	rest := strings.TrimPrefix(r.URL.Path, "/runtime/collaboration/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[0] == "create" || parts[0] == "heartbeat" {
		response.BadRequest(w, "INVALID_PATH", "expected /runtime/collaboration/{id}/{action}")
		return
	}
	id, action := parts[0], parts[1]

	var body struct {
		ActorID string `json:"actor_id"`
		Result  string `json:"result"`
		Note    string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	statusMap := map[string]string{
		"accept":   StatusAccepted,
		"start":    StatusInProgress,
		"complete": StatusCompleted,
		"reject":   StatusRejected,
	}
	newStatus, ok := statusMap[action]
	if !ok {
		response.BadRequest(w, "INVALID_ACTION", "valid actions: accept, start, complete, reject")
		return
	}

	if err := h.svc.Transition(tid, id, newStatus, body.ActorID, body.Result, body.Note); err != nil {
		response.BadRequest(w, "TRANSITION_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

type taskDTO struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	FromColleagueID string `json:"from_colleague_id"`
	ToColleagueID   string `json:"to_colleague_id"`
	ToRoleCode      string `json:"to_role_code"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	Result          string `json:"result"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type eventDTO struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Event     string `json:"event"`
	ActorID   string `json:"actor_id"`
	Note      string `json:"note"`
	CreatedAt string `json:"created_at"`
}

func toTaskDTO(t *Task) taskDTO {
	return taskDTO{
		ID: t.ID, Title: t.Title, Description: t.Description,
		FromColleagueID: t.FromColleagueID, ToColleagueID: t.ToColleagueID,
		ToRoleCode: t.ToRoleCode, Status: t.Status, Priority: t.Priority,
		Result:    t.Result,
		CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: t.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func toTaskDTOs(tasks []*Task) []taskDTO {
	dtos := make([]taskDTO, 0, len(tasks))
	for _, t := range tasks {
		dtos = append(dtos, toTaskDTO(t))
	}
	return dtos
}

func toEventDTOs(events []*TaskEvent) []eventDTO {
	dtos := make([]eventDTO, 0, len(events))
	for _, e := range events {
		dtos = append(dtos, eventDTO{
			ID: e.ID, TaskID: e.TaskID, Event: e.Event,
			ActorID: e.ActorID, Note: e.Note,
			CreatedAt: e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return dtos
}
