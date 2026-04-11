package compute

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes HTTP endpoints for compute power management.
type Handler struct {
	syncMgr    *SyncManager
	sourceMgr  *SourceManager
	localStore *LocalStore
}

// NewHandler creates a compute Handler.
func NewHandler(syncMgr *SyncManager, sourceMgr *SourceManager, localStore *LocalStore) *Handler {
	return &Handler{
		syncMgr:    syncMgr,
		sourceMgr:  sourceMgr,
		localStore: localStore,
	}
}

// RegisterAdminRoutes registers all compute management routes on the mux.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/compute/source", h.handleSource)
	mux.HandleFunc("/admin/compute/providers", h.handleProviders)
	mux.HandleFunc("/admin/compute/sync", h.handleSync)
	mux.HandleFunc("/admin/compute/sync-status", h.handleSyncStatus)
	mux.HandleFunc("/admin/compute/local-providers", h.handleLocalProviders)
	mux.HandleFunc("/admin/compute/local-providers/", h.handleLocalProviderByID)
	mux.HandleFunc("/admin/compute/test", h.handleTest)
}

// --- /admin/compute/source ---

func (h *Handler) handleSource(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.getSource(w, r)
	case http.MethodPut:
		h.setSource(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) getSource(w http.ResponseWriter, _ *http.Request) {
	response.OK(w, map[string]any{
		"source":             h.sourceMgr.GetSource(),
		"compute_permission": h.syncMgr.GetComputePermission(),
	})
}

func (h *Handler) setSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if err := h.sourceMgr.SetSource(req.Source); err != nil {
		switch err {
		case ErrInvalidSource:
			response.BadRequest(w, "INVALID_SOURCE", err.Error())
		case ErrNoPermission:
			response.Error(w, http.StatusForbidden, "NO_PERMISSION", err.Error())
		default:
			response.Internal(w, err.Error())
		}
		return
	}
	response.OK(w, map[string]string{"source": h.sourceMgr.GetSource()})
}

// --- /admin/compute/providers ---

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	providers := h.sourceMgr.GetActiveProviders()
	if providers == nil {
		providers = []ComputeProvider{}
	}
	response.OK(w, map[string]any{
		"providers": providers,
		"source":    h.sourceMgr.GetSource(),
	})
}

// --- /admin/compute/sync ---

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	err := h.syncMgr.SyncNow()
	// Check force_sync after sync
	h.sourceMgr.CheckForceSync()
	if err != nil {
		response.OK(w, map[string]any{
			"status": "failure",
			"error":  err.Error(),
		})
		return
	}
	response.OK(w, map[string]any{
		"status":         "success",
		"provider_count": len(h.syncMgr.GetProviders()),
	})
}

// --- /admin/compute/sync-status ---

func (h *Handler) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	response.OK(w, h.syncMgr.GetSyncStatus())
}

// --- /admin/compute/local-providers ---

func (h *Handler) handleLocalProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listLocalProviders(w, r)
	case http.MethodPost:
		h.createLocalProvider(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) listLocalProviders(w http.ResponseWriter, _ *http.Request) {
	providers := h.localStore.ListProviders()
	if providers == nil {
		providers = []ComputeProvider{}
	}
	response.OK(w, map[string]any{"providers": providers})
}

func (h *Handler) createLocalProvider(w http.ResponseWriter, r *http.Request) {
	if !h.sourceMgr.IsLocalEditAllowed() {
		response.Error(w, http.StatusForbidden, "NOT_LOCAL_MODE", "local editing requires local mode with compute_permission")
		return
	}
	var p ComputeProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	p.ID = "" // force new ID generation
	if err := h.localStore.SaveProvider(p); err != nil {
		response.Internal(w, err.Error())
		return
	}
	// Return the full list so the caller sees the generated ID
	response.Created(w, map[string]any{"providers": h.localStore.ListProviders()})
}

// --- /admin/compute/local-providers/{id} ---

func (h *Handler) handleLocalProviderByID(w http.ResponseWriter, r *http.Request) {
	id := extractTrailingID(r.URL.Path, "/admin/compute/local-providers/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "provider id required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.updateLocalProvider(w, r, id)
	case http.MethodDelete:
		h.deleteLocalProvider(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT or DELETE")
	}
}

func (h *Handler) updateLocalProvider(w http.ResponseWriter, r *http.Request, id string) {
	if !h.sourceMgr.IsLocalEditAllowed() {
		response.Error(w, http.StatusForbidden, "NOT_LOCAL_MODE", "local editing requires local mode with compute_permission")
		return
	}
	var p ComputeProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	p.ID = id
	if err := h.localStore.SaveProvider(p); err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) deleteLocalProvider(w http.ResponseWriter, _ *http.Request, id string) {
	if !h.sourceMgr.IsLocalEditAllowed() {
		response.Error(w, http.StatusForbidden, "NOT_LOCAL_MODE", "local editing requires local mode with compute_permission")
		return
	}
	if err := h.localStore.DeleteProvider(id); err != nil {
		response.NotFound(w, "NOT_FOUND", err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- /admin/compute/test ---

func (h *Handler) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var p ComputeProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	result := TestComputeProvider(&p)
	response.OK(w, result)
}

// extractTrailingID extracts the ID segment after the given prefix.
func extractTrailingID(path, prefix string) string {
	rest := strings.TrimPrefix(path, prefix)
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	return parts[0]
}
