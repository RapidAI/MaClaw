package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/compute"
)

// CenterInfo is a minimal representation of a center for compute handler use.
// The hub does not have a store.Center type, so we define this locally.
type CenterInfo struct {
	ID          string
	CompanyName string
	Status      string
}

// CenterAuthService is the subset of center service needed by ComputeHandler.
type CenterAuthService interface {
	Get(ctx context.Context, id string) (*CenterInfo, error)
	List(ctx context.Context) ([]*CenterInfo, error)
	AuthenticateCenter(ctx context.Context, id, secret string) (*CenterInfo, error)
}

// ComputeHandler holds the dependencies for compute provider HTTP endpoints.
type ComputeHandler struct {
	store     *compute.ProviderStore
	tester    *compute.ProviderTester
	centerSvc CenterAuthService
}

// NewComputeHandler creates a new ComputeHandler.
func NewComputeHandler(store *compute.ProviderStore, centerSvc CenterAuthService) *ComputeHandler {
	return &ComputeHandler{store: store, tester: compute.NewProviderTester(), centerSvc: centerSvc}
}

// CreateProvider handles POST /api/admin/compute/providers.
func (h *ComputeHandler) CreateProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var p compute.ComputeProvider
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := compute.ValidateProvider(&p); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
			return
		}
		if err := h.store.CreateProvider(r.Context(), &p); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		// Mask api_key in response
		p.HasAPIKey = p.APIKey != ""
		p.APIKey = ""
		writeJSON(w, http.StatusCreated, p)
	}
}

// ListProviders handles GET /api/admin/compute/providers.
// Returns all providers with api_key masked (replaced by has_api_key).
func (h *ComputeHandler) ListProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providers, err := h.store.ListProviders(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		if providers == nil {
			providers = []*compute.ComputeProvider{}
		}
		for _, p := range providers {
			p.HasAPIKey = p.APIKey != ""
			p.APIKey = ""
		}
		writeJSON(w, http.StatusOK, providers)
	}
}

// GetProvider handles GET /api/admin/compute/providers/{id}.
// Returns a single provider with api_key masked.
func (h *ComputeHandler) GetProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		p, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}
		p.HasAPIKey = p.APIKey != ""
		p.APIKey = ""
		writeJSON(w, http.StatusOK, p)
	}
}

// UpdateProvider handles PUT /api/admin/compute/providers/{id}.
func (h *ComputeHandler) UpdateProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		existing, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}
		var p compute.ComputeProvider
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		p.ID = id
		if err := compute.ValidateProvider(&p); err != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error())
			return
		}
		if err := h.store.UpdateProvider(r.Context(), &p); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		updated, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		updated.HasAPIKey = updated.APIKey != ""
		updated.APIKey = ""
		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteProvider handles DELETE /api/admin/compute/providers/{id}.
func (h *ComputeHandler) DeleteProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		existing, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}
		if err := h.store.DeleteProvider(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ToggleProvider handles POST /api/admin/compute/providers/{id}/toggle.
func (h *ComputeHandler) ToggleProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		existing, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}
		if err := h.store.ToggleProvider(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "TOGGLE_FAILED", err.Error())
			return
		}
		updated, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		updated.HasAPIKey = updated.APIKey != ""
		updated.APIKey = ""
		writeJSON(w, http.StatusOK, updated)
	}
}

// TestProvider handles POST /api/admin/compute/providers/{id}/test.
func (h *ComputeHandler) TestProvider() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "id is required")
			return
		}
		p, err := h.store.GetProvider(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}
		result := h.tester.Test(p)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         result.Success,
			"latency_ms": result.Latency.Milliseconds(),
			"error":      result.Error,
			"model":      result.Model,
		})
	}
}

// SetComputePermission handles PUT /api/admin/centers/{id}/compute-permission.
func (h *ComputeHandler) SetComputePermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		ctx := r.Context()

		if err := h.store.SetComputePermission(ctx, centerID, body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "SET_PERMISSION_FAILED", err.Error())
			return
		}

		if !body.Enabled {
			if err := h.store.SetForceSync(ctx, centerID, true); err != nil {
				writeError(w, http.StatusInternalServerError, "SET_FORCE_SYNC_FAILED", err.Error())
				return
			}
		}

		perm, _ := h.store.GetComputePermission(ctx, centerID)
		fs, _ := h.store.GetForceSync(ctx, centerID)

		writeJSON(w, http.StatusOK, map[string]any{
			"compute_permission": perm,
			"force_sync":         fs,
		})
	}
}

// ListCenterPermissions handles GET /api/admin/compute/permissions.
func (h *ComputeHandler) ListCenterPermissions() http.HandlerFunc {
	type centerPermission struct {
		CenterID          string `json:"center_id"`
		CompanyName       string `json:"company_name"`
		ComputePermission bool   `json:"compute_permission"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		centers, err := h.centerSvc.List(ctx)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		result := make([]centerPermission, 0, len(centers))
		for _, c := range centers {
			perm, _ := h.store.GetComputePermission(ctx, c.ID)
			result = append(result, centerPermission{
				CenterID:          c.ID,
				CompanyName:       c.CompanyName,
				ComputePermission: perm,
			})
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// ToggleCenterPermission handles POST /api/admin/compute/permissions/{id}.
func (h *ComputeHandler) ToggleCenterPermission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		var body struct {
			ComputePermission bool `json:"compute_permission"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}

		ctx := r.Context()

		if err := h.store.SetComputePermission(ctx, centerID, body.ComputePermission); err != nil {
			writeError(w, http.StatusInternalServerError, "SET_PERMISSION_FAILED", err.Error())
			return
		}

		if !body.ComputePermission {
			if err := h.store.SetForceSync(ctx, centerID, true); err != nil {
				writeError(w, http.StatusInternalServerError, "SET_FORCE_SYNC_FAILED", err.Error())
				return
			}
		}

		perm, _ := h.store.GetComputePermission(ctx, centerID)
		fs, _ := h.store.GetForceSync(ctx, centerID)

		writeJSON(w, http.StatusOK, map[string]any{
			"compute_permission": perm,
			"force_sync":         fs,
		})
	}
}

// AssignProviderToCenter handles POST /api/admin/centers/{id}/compute-providers.
func (h *ComputeHandler) AssignProviderToCenter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		var body struct {
			ProviderID string `json:"provider_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if body.ProviderID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "provider_id is required")
			return
		}

		ctx := r.Context()

		p, err := h.store.GetProvider(ctx, body.ProviderID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "GET_FAILED", err.Error())
			return
		}
		if p == nil {
			writeError(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "provider not found")
			return
		}

		if err := h.store.AssignProvider(ctx, centerID, body.ProviderID); err != nil {
			writeError(w, http.StatusInternalServerError, "ASSIGN_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "center_id": centerID, "provider_id": body.ProviderID})
	}
}

// UnassignProviderFromCenter handles DELETE /api/admin/centers/{id}/compute-providers/{provider_id}.
func (h *ComputeHandler) UnassignProviderFromCenter() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}
		providerID := r.PathValue("provider_id")
		if providerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "provider_id is required")
			return
		}

		if err := h.store.UnassignProvider(r.Context(), centerID, providerID); err != nil {
			writeError(w, http.StatusInternalServerError, "UNASSIGN_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ListCenterAssignments handles GET /api/admin/centers/{id}/compute-providers.
func (h *ComputeHandler) ListCenterAssignments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		ids, err := h.store.ListAssignments(r.Context(), centerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		if ids == nil {
			ids = []string{}
		}

		writeJSON(w, http.StatusOK, map[string]any{"assignments": ids})
	}
}

// CenterComputeProviders handles GET /api/centers/{id}/compute-providers.
// Authenticates using the center's secret (query param ?secret= or X-Center-Secret header).
// Returns the full provider list (with api_key) assigned to the center.
func (h *ComputeHandler) CenterComputeProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		secret := r.URL.Query().Get("secret")
		if secret == "" {
			secret = r.Header.Get("X-Center-Secret")
		}
		if secret == "" {
			writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "missing center secret")
			return
		}

		center, err := h.centerSvc.AuthenticateCenter(r.Context(), centerID, secret)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_FAILED", "invalid center credentials")
			return
		}

		if center.Status == "disabled" {
			writeError(w, http.StatusForbidden, "CENTER_DISABLED", "center is disabled")
			return
		}

		ctx := r.Context()

		providers, err := h.store.ListAssignedProviders(ctx, centerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		if providers == nil {
			providers = []*compute.ComputeProvider{}
		}

		perm, _ := h.store.GetComputePermission(ctx, centerID)
		forceSync, _ := h.store.GetForceSync(ctx, centerID)

		writeJSON(w, http.StatusOK, map[string]any{
			"providers":          providers,
			"compute_permission": perm,
			"force_sync":         forceSync,
		})

		if forceSync {
			_ = h.store.ClearForceSync(ctx, centerID)
		}
	}
}
