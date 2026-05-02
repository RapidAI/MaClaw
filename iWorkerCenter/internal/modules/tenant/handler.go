package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Handler provides HTTP endpoints for tenant management.
type Handler struct {
	svc *TenantService
}

func NewHandler(svc *TenantService) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers public tenant routes. Cloud-side tenant provisioning is intentionally not exposed.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/tenant-status", h.handleTenantStatus)
	mux.HandleFunc("/auth/tenants", h.handleListTenants)
	mux.HandleFunc("/auth/setup-tenant", h.handleSetupTenant)
}

// RegisterAdminRoutes registers tenant-related admin routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/system/tenant-mode", h.handleTenantMode)
	mux.HandleFunc("/admin/cloud/config", h.handleCloudConfig)
	mux.HandleFunc("/admin/cloud/status", h.handleCloudStatus)
	mux.HandleFunc("/admin/cloud/register", h.handleCloudRegister)
	mux.HandleFunc("/admin/cloud/license", h.handleCloudLicense)
}

// RegisterCloudRoutes registers Cloud-to-Center tenant management routes.
func (h *Handler) RegisterCloudRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/cloud/tenants", h.handleCloudTenants)
	mux.HandleFunc("/api/cloud/tenants/", h.handleCloudTenantByID)
}

// handleTenantStatus returns {count, needs_setup}.
func (h *Handler) handleTenantStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	count, err := h.svc.TenantCount(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":       count,
		"needs_setup": count == 0,
	})
}

// handleListTenants returns active tenants for login page selection.
func (h *Handler) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tenants, err := h.svc.ListActiveTenants(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	type item struct {
		ID          string `json:"id"`
		CompanyName string `json:"company_name"`
	}
	items := make([]item, len(tenants))
	for i, t := range tenants {
		items[i] = item{ID: t.ID, CompanyName: t.CompanyName}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": items})
}

// handleSetupTenant handles first-time tenant creation.
func (h *Handler) handleSetupTenant(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req CreateTenantRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
		return
	}

	t, err := h.svc.SetupFirstTenant(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrSetupAlreadyDone) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "initial setup already completed"})
			return
		}
		if errors.Is(err, ErrCompanyExists) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "company already exists"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	// Use a background context because the request context is cancelled after response.
	go h.svc.RegisterToCloud(context.Background(), t.ID)

	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": t.ID,
		"status":    t.Status,
		"message":   "tenant created",
	})
}

func (h *Handler) handleCloudConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.svc.CloudConfig(r.Context()))
	case http.MethodPut:
		var req UpdateCloudConfigRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
			return
		}
		cfg, err := h.svc.UpdateCloudConfig(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func (h *Handler) handleCloudStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := h.svc.CloudStatus(r.Context(), RequestTenantID(r))
	if err != nil {
		switch {
		case errors.Is(err, ErrTenantNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "tenant not found"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *Handler) handleCloudRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req RegisterCenterRequest
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	}
	resp, err := h.svc.RegisterTenantToCloud(r.Context(), RequestTenantID(r), req)
	if err != nil {
		switch {
		case errors.Is(err, ErrCloudNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "iWorkerCloud not configured"})
		case errors.Is(err, ErrTenantNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "tenant not found"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"center_id": resp.CenterID, "status": resp.Status, "message": resp.Message})
}
func (h *Handler) handleCloudLicense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	license, err := h.svc.FetchCloudLicense(r.Context(), RequestTenantID(r))
	if err != nil {
		switch {
		case errors.Is(err, ErrCloudNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "iWorkerCloud not configured"})
		case errors.Is(err, ErrTenantNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "tenant not found"})
		case errors.Is(err, ErrCloudCredentialsMissing):
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": "tenant cloud credentials missing"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, license)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *Handler) handleTenantMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		settings, err := h.svc.MultiTenantSettings(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, settings)
	case http.MethodPut:
		var req struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
			return
		}
		settings, err := h.svc.UpdateMultiTenantSettings(r.Context(), req.Mode)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, settings)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) requireCloudTenantManagement(w http.ResponseWriter, r *http.Request) bool {
	if err := h.svc.AuthenticateCloudCenter(r.Context(), r.Header.Get("X-Center-ID"), r.Header.Get("X-Center-Secret")); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid center credentials"})
		return false
	}
	if err := h.svc.RequireMultiTenantEnabled(r.Context()); err != nil {
		status := http.StatusForbidden
		if !errors.Is(err, ErrMultiTenantDisabled) {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return false
	}
	return true
}

func (h *Handler) handleCloudTenants(w http.ResponseWriter, r *http.Request) {
	if !h.requireCloudTenantManagement(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		tenants, err := h.svc.ListTenants(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tenants": tenants})
	case http.MethodPost:
		var req CreateTenantRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
			return
		}
		tenant, err := h.svc.CreateTenant(r.Context(), req)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrCompanyExists) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, tenant)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleCloudTenantByID(w http.ResponseWriter, r *http.Request) {
	if !h.requireCloudTenantManagement(w, r) {
		return
	}
	tenantID := strings.TrimPrefix(r.URL.Path, "/api/cloud/tenants/")
	if strings.TrimSpace(tenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tenant id is required"})
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req UpdateTenantRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON"})
			return
		}
		tenant, err := h.svc.UpdateTenant(r.Context(), tenantID, req)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrTenantNotFound) {
				status = http.StatusNotFound
			} else if errors.Is(err, ErrCompanyExists) {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, tenant)
	case http.MethodDelete:
		if err := h.svc.DeleteTenant(r.Context(), tenantID); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, ErrTenantNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
