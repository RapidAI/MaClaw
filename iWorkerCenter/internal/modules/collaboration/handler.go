package collaboration

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for collaboration tasks.
type Handler struct {
	svc *Service
}

// NewHandler creates a Handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/collaborations", h.handleList)
	mux.HandleFunc("/admin/collaborations/", h.handleByID)
}

// RegisterClientRoutes registers client-facing routes.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/collaborations", h.handleClientList)
	mux.HandleFunc("/runtime/collaboration/create", h.handleCreate)
	mux.HandleFunc("/runtime/collaboration/", h.handleTransition)
}

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.TenantIDFromContext(r.Context())
	tasks, err := h.svc.ListAll(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"tasks": toTaskDTOs(tasks)})
}

func (h *Handler) handleClientList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.TenantIDFromContext(r.Context())
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
	tid := tenant.TenantIDFromContext(r.Context())
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
	response.Created(w, toTaskDTO(task))
}

func (h *Handler) handleByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	// /admin/collaborations/{id}
	// /admin/collaborations/{id}/events
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
	tid := tenant.TenantIDFromContext(r.Context())
	// /runtime/collaboration/{id}/{action}
	rest := strings.TrimPrefix(r.URL.Path, "/runtime/collaboration/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 2 || parts[0] == "" || parts[0] == "create" {
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

// --- DTOs ---

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
		Result: t.Result,
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
