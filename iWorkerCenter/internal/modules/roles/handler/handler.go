package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/domain"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/service"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for role management.
type Handler struct {
	svc *service.RoleService
}

// New creates a Handler.
func New(svc *service.RoleService) *Handler {
	return &Handler{svc: svc}
}

// RegisterAdminRoutes registers admin-facing routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/roles", h.handleAdminRoles)
	mux.HandleFunc("/admin/roles/", h.handleAdminRoleByID)
}

// RegisterClientRoutes registers client-facing routes (for DiWorker).
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/roles", h.handleClientRoles)
}

func (h *Handler) handleAdminRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listRoles(w, r)
	case http.MethodPost:
		h.createRole(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAdminRoleByID(w http.ResponseWriter, r *http.Request) {
	id := extractID(r.URL.Path, "/admin/roles/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "role id is required")
		return
	}

	// /admin/roles/{id}/status
	if strings.HasSuffix(r.URL.Path, "/status") {
		id = strings.TrimSuffix(id, "/status")
		if r.Method == http.MethodPost {
			h.setStatus(w, r, id)
			return
		}
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getRole(w, r, id)
	case http.MethodPut:
		h.updateRole(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleClientRoles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	tid := tenant.RequestTenantID(r)
	items, err := h.svc.ListActive(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"roles": toClientDTOs(items)})
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	items, err := h.svc.List(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"roles": toAdminDTOs(items)})
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	tid := tenant.RequestTenantID(r)
	var req service.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	role, err := h.svc.Create(tid, req)
	if err != nil {
		response.BadRequest(w, "CREATE_FAILED", err.Error())
		return
	}
	response.Created(w, toAdminDTO(role))
}

func (h *Handler) getRole(w http.ResponseWriter, r *http.Request, id string) {
	tid := tenant.RequestTenantID(r)
	role, err := h.svc.GetByID(tid, id)
	if err != nil {
		response.NotFound(w, "NOT_FOUND", "role not found")
		return
	}
	response.OK(w, toAdminDTO(role))
}

func (h *Handler) updateRole(w http.ResponseWriter, r *http.Request, id string) {
	tid := tenant.RequestTenantID(r)
	var req service.UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	role, err := h.svc.Update(tid, id, req)
	if err != nil {
		response.BadRequest(w, "UPDATE_FAILED", err.Error())
		return
	}
	response.OK(w, toAdminDTO(role))
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, id string) {
	tid := tenant.RequestTenantID(r)
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	if err := h.svc.SetStatus(tid, id, req.Status); err != nil {
		response.BadRequest(w, "STATUS_FAILED", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- DTOs ---

type adminDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
	Status           string   `json:"status"`
	SortOrder        int      `json:"sort_order"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
}

type clientDTO struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Code             string   `json:"code"`
	Description      string   `json:"description"`
	DefaultStrengths []string `json:"default_strengths"`
	ApplicableTasks  []string `json:"applicable_tasks"`
}

func toAdminDTO(r *domain.Role) adminDTO {
	return adminDTO{
		ID: r.ID, Name: r.Name, Code: r.Code, Description: r.Description,
		DefaultStrengths: r.DefaultStrengths, ApplicableTasks: r.ApplicableTasks,
		Status: r.Status, SortOrder: r.SortOrder,
		CreatedAt: r.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt: r.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func toAdminDTOs(items []*domain.Role) []adminDTO {
	dtos := make([]adminDTO, 0, len(items))
	for _, r := range items {
		dtos = append(dtos, toAdminDTO(r))
	}
	return dtos
}

func toClientDTO(r *domain.Role) clientDTO {
	return clientDTO{
		ID: r.ID, Name: r.Name, Code: r.Code, Description: r.Description,
		DefaultStrengths: r.DefaultStrengths, ApplicableTasks: r.ApplicableTasks,
	}
}

func toClientDTOs(items []*domain.Role) []clientDTO {
	dtos := make([]clientDTO, 0, len(items))
	for _, r := range items {
		dtos = append(dtos, toClientDTO(r))
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
