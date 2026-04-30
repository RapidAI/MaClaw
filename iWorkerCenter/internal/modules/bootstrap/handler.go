package bootstrap

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/bootstrap/status", h.handleStatus)
	mux.HandleFunc("/admin/bootstrap/draft-plan", h.handleDraftPlan)
	mux.HandleFunc("/admin/bootstrap/validate-plan", h.handleValidatePlan)
	mux.HandleFunc("/admin/bootstrap/apply-plan", h.handleApplyPlan)
	mux.HandleFunc("/admin/bootstrap/start-first-wave", h.handleStartFirstWave)
	mux.HandleFunc("/admin/bootstrap/runs/", h.handleRunByID)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	response.OK(w, h.svc.Status(tenant.RequestTenantID(r)))
}

func (h *Handler) handleDraftPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req Plan
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	plan, issues, err := h.svc.DraftPlan(tenant.RequestTenantID(r), req)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, map[string]any{"plan": plan, "validation_issues": issues, "suggested_first_wave": BuildFirstWave(plan)})
}

func (h *Handler) handleValidatePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req Plan
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	plan := NormalizePlan(tenant.RequestTenantID(r), req)
	issues := ValidatePlan(plan)
	response.OK(w, map[string]any{"plan": plan, "ready_to_start": noBlockingIssues(issues), "validation_issues": issues})
}

func (h *Handler) handleApplyPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req Plan
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	plan, issues, assets, err := h.svc.ApplyPlan(tenant.RequestTenantID(r), req)
	if err != nil {
		response.BadRequest(w, "PLAN_NOT_READY", err.Error())
		return
	}
	response.OK(w, map[string]any{"plan": plan, "validation_issues": issues, "suggested_first_wave": BuildFirstWave(plan), "applied_assets": assets})
}

func (h *Handler) handleStartFirstWave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	run, err := h.svc.StartFirstWave(tenant.RequestTenantID(r))
	if err != nil {
		response.BadRequest(w, "FIRST_WAVE_NOT_READY", err.Error())
		return
	}
	response.Created(w, map[string]any{"run": run})
}

func (h *Handler) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	runID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/bootstrap/runs/"), "/")
	if runID == "" {
		response.BadRequest(w, "MISSING_RUN_ID", "run id is required")
		return
	}
	run, ok := h.svc.GetRun(tenant.RequestTenantID(r), runID)
	if !ok {
		response.NotFound(w, "RUN_NOT_FOUND", "bootstrap run not found")
		return
	}
	response.OK(w, map[string]any{"run": run})
}
