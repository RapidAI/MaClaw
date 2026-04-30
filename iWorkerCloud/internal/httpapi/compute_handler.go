package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

// ComputeHandler holds the dependencies for compute provider HTTP endpoints.
type ComputeHandler struct {
	store         *compute.ProviderStore
	tester        *compute.ProviderTester
	centerSvc     CenterAuthService
	usageStore    *compute.UsageStore
	costEngine    *compute.CostEngine
	forwardClient *http.Client
}

// CenterAuthService is the subset of centers.Service needed by ComputeHandler.
type CenterAuthService interface {
	Get(ctx context.Context, id string) (*store.Center, error)
	List(ctx context.Context) ([]*store.Center, error)
	AuthenticateCenter(ctx context.Context, id, secret string) (*store.Center, error)
}

// NewComputeHandler creates a new ComputeHandler.
func NewComputeHandler(store *compute.ProviderStore, centerSvc CenterAuthService) *ComputeHandler {
	return &ComputeHandler{
		store:         store,
		tester:        compute.NewProviderTester(),
		centerSvc:     centerSvc,
		forwardClient: &http.Client{Timeout: 120 * time.Second},
	}
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
		// Verify provider exists
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
		// Re-read to get the full record with updated timestamps
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
		// Re-read to return the updated state
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
// It sends a simple prompt to the provider and returns the connectivity result.
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
// Request body: {"enabled": true/false}
// When revoking (enabled=false), also sets force_sync=true for the center.
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

		// Set the permission.
		if err := h.store.SetComputePermission(ctx, centerID, body.Enabled); err != nil {
			writeError(w, http.StatusInternalServerError, "SET_PERMISSION_FAILED", err.Error())
			return
		}

		// When revoking permission, set force_sync so center reverts to cloud mode.
		if !body.Enabled {
			if err := h.store.SetForceSync(ctx, centerID, true); err != nil {
				writeError(w, http.StatusInternalServerError, "SET_FORCE_SYNC_FAILED", err.Error())
				return
			}
		}

		// Read back current values.
		perm, _ := h.store.GetComputePermission(ctx, centerID)
		fs, _ := h.store.GetForceSync(ctx, centerID)

		writeJSON(w, http.StatusOK, map[string]any{
			"compute_permission": perm,
			"force_sync":         fs,
		})
	}
}

// ListCenterPermissions handles GET /api/admin/compute/permissions.
// Returns all centers with their compute_permission status.
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
// Request body: {"compute_permission": true/false}
// Delegates to SetComputePermission logic.
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

func (h *ComputeHandler) ensureCenterExists(ctx context.Context, w http.ResponseWriter, centerID string) bool {
	if h.centerSvc == nil {
		return true
	}
	center, err := h.centerSvc.Get(ctx, centerID)
	if err != nil || center == nil {
		writeError(w, http.StatusNotFound, "CENTER_NOT_FOUND", "center not found")
		return false
	}
	return true
}

// AssignProviderToCenter handles POST /api/admin/centers/{id}/compute-providers.
// Request body: {"provider_id": "xxx"}
// Creates an assignment between a center and a provider.
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
		if !h.ensureCenterExists(ctx, w, centerID) {
			return
		}

		// Verify provider exists.
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
		if err := h.store.SetForceSync(ctx, centerID, true); err != nil {
			writeError(w, http.StatusInternalServerError, "SET_FORCE_SYNC_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "center_id": centerID, "provider_id": body.ProviderID, "force_sync": true})
	}
}

// UnassignProviderFromCenter handles DELETE /api/admin/centers/{id}/compute-providers/{provider_id}.
// Removes an assignment between a center and a provider.
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

		ctx := r.Context()
		if !h.ensureCenterExists(ctx, w, centerID) {
			return
		}

		if err := h.store.UnassignProvider(ctx, centerID, providerID); err != nil {
			writeError(w, http.StatusInternalServerError, "UNASSIGN_FAILED", err.Error())
			return
		}
		if err := h.store.SetForceSync(ctx, centerID, true); err != nil {
			writeError(w, http.StatusInternalServerError, "SET_FORCE_SYNC_FAILED", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "force_sync": true})
	}
}

// ListCenterAssignments handles GET /api/admin/centers/{id}/compute-providers.
// Returns the list of provider IDs assigned to a center.
func (h *ComputeHandler) ListCenterAssignments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		ctx := r.Context()
		if !h.ensureCenterExists(ctx, w, centerID) {
			return
		}

		ids, err := h.store.ListAssignments(ctx, centerID)
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
// Authenticates using the center's X-Center-Secret header.
// Returns the full provider list (with api_key) assigned to the center.
// If the center is disabled, returns 403 CENTER_DISABLED.
// If no specific assignments exist, returns all enabled providers.
// Response includes compute_permission and force_sync metadata fields.
func (h *ComputeHandler) CenterComputeProviders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if centerID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "center id is required")
			return
		}

		if _, ok := authenticateCenterRequest(w, r, h.centerSvc, centerID); !ok {
			return
		}

		ctx := r.Context()

		// Get assigned providers (falls back to all enabled if no assignments).
		providers, err := h.store.ListAssignedProviders(ctx, centerID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		if providers == nil {
			providers = []*compute.ComputeProvider{}
		}

		// Read real permission and force_sync values.
		perm, _ := h.store.GetComputePermission(ctx, centerID)
		forceSync, _ := h.store.GetForceSync(ctx, centerID)

		// NOTE: Do NOT mask api_key; centers need the full key for LLM requests.

		writeJSON(w, http.StatusOK, map[string]any{
			"providers":          providers,
			"compute_permission": perm,
			"force_sync":         forceSync,
		})

		// Clear force_sync after returning it as true, so it's a one-shot signal.
		if forceSync {
			_ = h.store.ClearForceSync(ctx, centerID)
		}
	}
}
