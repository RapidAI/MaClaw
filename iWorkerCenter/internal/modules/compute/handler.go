package compute

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

const maxComputeJSONBodyBytes = 64 << 10

func decodeComputeJSON(body io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxComputeJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxComputeJSONBodyBytes {
		return errors.New("compute json body exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("compute json body contains trailing data")
		}
		return err
	}
	return nil
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
	syncStatus := h.syncMgr.GetSyncStatus()
	activeProviders, effectiveSource, fallbackActive := h.sourceMgr.GetActiveProviderSnapshot()
	response.OK(w, map[string]any{
		"source":                h.sourceMgr.GetSource(),
		"effective_source":      effectiveSource,
		"fallback_active":       fallbackActive,
		"active_provider_count": len(activeProviders),
		"compute_permission":    h.syncMgr.GetComputePermission(),
		"sync_status":           syncStatus,
		"last_sync_at":          syncStatus.LastSyncAt,
		"provider_count":        syncStatus.ProviderCount,
	})
}

func (h *Handler) setSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Source string `json:"source"`
	}
	if err := decodeComputeJSON(r.Body, &req); err != nil {
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
	if h.sourceMgr.GetSource() == "local" {
		if err := h.applyLocalProvidersSnapshot(); err != nil {
			response.Internal(w, err.Error())
			return
		}
	}
	h.writeSourceSnapshot(w, http.StatusOK)
}

// --- /admin/compute/providers ---

func (h *Handler) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	providers, effectiveSource, fallbackActive := h.sourceMgr.GetActiveProviderSnapshot()
	if providers == nil {
		providers = []ComputeProvider{}
	}
	response.OK(w, map[string]any{
		"providers":        providers,
		"source":           h.sourceMgr.GetSource(),
		"effective_source": effectiveSource,
		"fallback_active":  fallbackActive,
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
	syncStatus := h.syncMgr.GetSyncStatus()
	if err != nil {
		if syncStatus.Status == "" {
			syncStatus.Status = "failure"
			if errors.Is(err, ErrWaitingForCredentials) {
				syncStatus.Status = "waiting_for_credentials"
			}
		}
		if syncStatus.Error == "" {
			syncStatus.Error = err.Error()
		}
		response.OK(w, map[string]any{
			"status":         syncStatus.Status,
			"error":          syncStatus.Error,
			"last_sync_at":   syncStatus.LastSyncAt,
			"provider_count": syncStatus.ProviderCount,
		})
		return
	}
	response.OK(w, map[string]any{
		"status":         syncStatus.Status,
		"last_sync_at":   syncStatus.LastSyncAt,
		"provider_count": syncStatus.ProviderCount,
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
	if err := decodeComputeJSON(r.Body, &p); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	p.ID = "" // force new ID generation
	if err := h.localStore.SaveProvider(p); err != nil {
		response.Internal(w, err.Error())
		return
	}
	providers, err := h.applyLocalProvidersSnapshotAndList()
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, map[string]any{"providers": providers, "active_provider_count": len(providers), "source": h.sourceMgr.GetSource()})
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
	if err := decodeComputeJSON(r.Body, &p); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	p.ID = id
	if err := h.localStore.SaveProvider(p); err != nil {
		response.Internal(w, err.Error())
		return
	}
	providers, err := h.applyLocalProvidersSnapshotAndList()
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"status": "ok", "providers": providers, "active_provider_count": len(providers), "source": h.sourceMgr.GetSource()})
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
	providers, err := h.applyLocalProvidersSnapshotAndList()
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"status": "ok", "providers": providers, "active_provider_count": len(providers), "source": h.sourceMgr.GetSource()})
}

// --- /admin/compute/test ---

func (h *Handler) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var p ComputeProvider
	if err := decodeComputeJSON(r.Body, &p); err != nil {
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

func (h *Handler) applyLocalProvidersSnapshot() error {
	if h == nil || h.localStore == nil || h.sourceMgr == nil {
		return nil
	}
	return h.sourceMgr.SetLocalProviders(h.localStore.ListProviders())
}

func (h *Handler) applyLocalProvidersSnapshotAndList() ([]ComputeProvider, error) {
	if h == nil || h.localStore == nil {
		return []ComputeProvider{}, nil
	}
	providers := h.localStore.ListProviders()
	if providers == nil {
		providers = []ComputeProvider{}
	}
	if h.sourceMgr != nil {
		if err := h.sourceMgr.SetLocalProviders(providers); err != nil {
			return nil, err
		}
	}
	return providers, nil
}

func (h *Handler) writeSourceSnapshot(w http.ResponseWriter, status int) {
	syncStatus := h.syncMgr.GetSyncStatus()
	activeProviders, effectiveSource, fallbackActive := h.sourceMgr.GetActiveProviderSnapshot()
	response.JSON(w, status, map[string]any{
		"source":                h.sourceMgr.GetSource(),
		"effective_source":      effectiveSource,
		"fallback_active":       fallbackActive,
		"active_provider_count": len(activeProviders),
		"compute_permission":    h.syncMgr.GetComputePermission(),
		"sync_status":           syncStatus,
		"last_sync_at":          syncStatus.LastSyncAt,
		"provider_count":        syncStatus.ProviderCount,
	})
}
