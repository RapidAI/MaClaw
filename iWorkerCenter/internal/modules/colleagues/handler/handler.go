package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/domain"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/service"
	roleDomain "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	roleSvc "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for colleague management.
type Handler struct {
	svc      *service.ColleagueService
	roleRepo *roleRepo.RoleRepo
	roleSvc  *roleSvc.RoleService
}

// New creates a Handler.
func New(svc *service.ColleagueService, rr *roleRepo.RoleRepo, rs *roleSvc.RoleService) *Handler {
	return &Handler{svc: svc, roleRepo: rr, roleSvc: rs}
}

// RegisterAdminRoutes registers admin-facing routes on the given mux.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/colleagues", h.handleAdminColleagues)
	mux.HandleFunc("/admin/colleagues/", h.handleAdminColleagueByID)
}

// RegisterClientRoutes registers client-facing routes (for DiWorker).
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/colleagues", h.handleClientColleagues)
}

func (h *Handler) handleAdminColleagues(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listColleagues(w, r)
	case http.MethodPost:
		h.createColleague(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAdminColleagueByID(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	id := extractID(path, "/admin/colleagues/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "colleague id is required")
		return
	}

	// /admin/colleagues/{id}/status
	if strings.HasSuffix(path, "/status") {
		id = strings.TrimSuffix(id, "/status")
		if r.Method == http.MethodPost {
			h.setStatus(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	// /admin/colleagues/{id}/assign-role
	if strings.HasSuffix(path, "/assign-role") {
		id = strings.TrimSuffix(id, "/assign-role")
		if r.Method == http.MethodPost {
			h.assignRole(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	// /admin/colleagues/{id}/role-history
	if strings.HasSuffix(path, "/role-history") {
		id = strings.TrimSuffix(id, "/role-history")
		if r.Method == http.MethodGet {
			h.roleHistory(w, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getColleague(w, id)
	case http.MethodPut:
		h.updateColleague(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleClientColleagues(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}

	// optional filter by role_id
	roleID := r.URL.Query().Get("role_id")
	var items []*domain.Colleague
	var err error
	if roleID != "" {
		items, err = h.svc.ListByRoleID(roleID)
	} else {
		items, err = h.svc.ListActive()
	}
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"colleagues": h.toClientDTOs(items)})
}

func (h *Handler) listColleagues(w http.ResponseWriter, r *http.Request) {
	// optional filter by role_id
	roleID := r.URL.Query().Get("role_id")
	var items []*domain.Colleague
	var err error
	if roleID != "" {
		items, err = h.svc.ListByRoleID(roleID)
	} else {
		items, err = h.svc.List()
	}
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"colleagues": h.toAdminDTOs(items)})
}

func (h *Handler) createColleague(w http.ResponseWriter, r *http.Request) {
	var req service.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	c, err := h.svc.Create(req)
	if err != nil {
		response.BadRequest(w, "CREATE_FAILED", err.Error())
		return
	}
	response.Created(w, h.toAdminDTO(c))
}

func (h *Handler) getColleague(w http.ResponseWriter, id string) {
	c, err := h.svc.GetByID(id)
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "colleague not found")
		return
	}
	response.OK(w, h.toAdminDTO(c))
}

func (h *Handler) updateColleague(w http.ResponseWriter, r *http.Request, id string) {
	var req service.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	c, err := h.svc.Update(id, req)
	if err != nil {
		response.BadRequest(w, "UPDATE_FAILED", err.Error())
		return
	}
	response.OK(w, h.toAdminDTO(c))
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	if err := h.svc.SetStatus(id, req.Status); err != nil {
		response.BadRequest(w, "STATUS_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) assignRole(w http.ResponseWriter, r *http.Request, id string) {
	var req service.AssignRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	if err := h.svc.AssignRole(id, req); err != nil {
		response.BadRequest(w, "ASSIGN_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) roleHistory(w http.ResponseWriter, colleagueID string) {
	logs, err := h.roleSvc.GetAssignmentHistory(colleagueID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	type logDTO struct {
		ID          string `json:"id"`
		ColleagueID string `json:"colleague_id"`
		OldRoleID   string `json:"old_role_id"`
		NewRoleID   string `json:"new_role_id"`
		Reason      string `json:"reason"`
		AssignedAt  string `json:"assigned_at"`
	}
	dtos := make([]logDTO, 0, len(logs))
	for _, l := range logs {
		dtos = append(dtos, logDTO{
			ID: l.ID, ColleagueID: l.ColleagueID,
			OldRoleID: l.OldRoleID, NewRoleID: l.NewRoleID,
			Reason: l.Reason, AssignedAt: l.AssignedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	response.OK(w, map[string]any{"logs": dtos})
}

// --- DTOs with role info ---

type adminDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	RoleID      string   `json:"role_id"`
	RoleName    string   `json:"role_name"`
	RoleCode    string   `json:"role_code"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type clientDTO struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Avatar      string   `json:"avatar"`
	RoleID      string   `json:"role_id"`
	RoleName    string   `json:"role_name"`
	RoleCode    string   `json:"role_code"`
	Description string   `json:"description"`
	Strengths   []string `json:"strengths"`
	Tasks       []string `json:"tasks"`
}

func (h *Handler) resolveRole(roleID string) *roleDomain.Role {
	if roleID == "" {
		return nil
	}
	r, _ := h.roleRepo.GetByID(roleID)
	return r
}

func (h *Handler) toAdminDTO(c *domain.Colleague) adminDTO {
	role := h.resolveRole(c.RoleID)
	dto := adminDTO{
		ID: c.ID, Name: c.Name, Avatar: c.Avatar,
		RoleID: c.RoleID, Description: c.Description,
		Strengths: c.Strengths, Tasks: c.Tasks, Status: c.Status,
		CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if role != nil {
		dto.RoleName = role.Name
		dto.RoleCode = role.Code
	}
	return dto
}

func (h *Handler) toAdminDTOs(items []*domain.Colleague) []adminDTO {
	dtos := make([]adminDTO, 0, len(items))
	for _, c := range items {
		dtos = append(dtos, h.toAdminDTO(c))
	}
	return dtos
}

func (h *Handler) toClientDTO(c *domain.Colleague) clientDTO {
	role := h.resolveRole(c.RoleID)
	dto := clientDTO{
		ID: c.ID, Name: c.Name, Avatar: c.Avatar,
		RoleID: c.RoleID, Description: c.Description,
		Strengths: c.Strengths, Tasks: c.Tasks,
	}
	if role != nil {
		dto.RoleName = role.Name
		dto.RoleCode = role.Code
	}
	return dto
}

func (h *Handler) toClientDTOs(items []*domain.Colleague) []clientDTO {
	dtos := make([]clientDTO, 0, len(items))
	for _, c := range items {
		dtos = append(dtos, h.toClientDTO(c))
	}
	return dtos
}

func extractID(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
