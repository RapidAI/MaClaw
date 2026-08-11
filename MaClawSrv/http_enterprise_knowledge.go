package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func (s *HTTPServer) requireEnterpriseSync(w http.ResponseWriter) bool {
	if s == nil || s.enterpriseSync == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "enterprise knowledge sync is not available"})
		return false
	}
	return true
}

// enterprisePurgeHTTPStatus maps purge errors to 404 when the library/meta is absent.
func enterprisePurgeHTTPStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "library not found") || strings.Contains(msg, "library_id required") {
		if strings.Contains(msg, "library_id required") {
			return http.StatusBadRequest
		}
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

// GET /api/v1/enterprise-knowledge/libraries
func (s *HTTPServer) handleEnterpriseKnowledgeListLibraries(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	libs, err := s.enterpriseSync.ListLibraries(p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"libraries": libs,
		"total":     len(libs),
	})
}

// GET /api/v1/enterprise-knowledge/sync/status
func (s *HTTPServer) handleEnterpriseKnowledgeSyncStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	st, err := s.enterpriseSync.UserStatus(r.Context(), p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/v1/enterprise-knowledge/sync/now
func (s *HTTPServer) handleEnterpriseKnowledgeSyncNow(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := s.enterpriseSync.SyncUser(ctx, p); err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, enterpriseknowledge.ErrSyncInProgress) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	st, _ := s.enterpriseSync.UserStatus(r.Context(), p)
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": st,
	})
}

// POST /api/v1/enterprise-knowledge/libraries/{libraryId}/user-sync
// body: { "enabled": true|false }
func (s *HTTPServer) handleEnterpriseKnowledgeSetUserSync(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	libraryID := strings.TrimSpace(r.PathValue("libraryId"))
	if libraryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "libraryId required"})
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := readJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if body.Enabled == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "enabled is required"})
		return
	}
	if err := s.enterpriseSync.SetUserSync(p, libraryID, *body.Enabled); err != nil {
		// library not found → 404 (same mapping as purge); other errors stay 400.
		writeJSON(w, enterprisePurgeHTTPStatus(err), map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	libs, _ := s.enterpriseSync.ListLibraries(p)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"library_id": libraryID,
		"enabled":   *body.Enabled,
		"libraries": libs,
	})
}

// GET /api/v1/admin/enterprise-knowledge/sync/status
func (s *HTTPServer) handleAdminEnterpriseKnowledgeSyncStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.enterpriseSync.Status())
}

// POST /api/v1/admin/enterprise-knowledge/sync/now
func (s *HTTPServer) handleAdminEnterpriseKnowledgeSyncNow(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	st, err := s.enterpriseSync.SyncAll(ctx)
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, enterpriseknowledge.ErrSyncInProgress) {
			code = http.StatusConflict
		}
		writeJSON(w, code, map[string]any{
			"error":  redactSupportBundleText(s.svc.DataRoot(), err.Error()),
			"status": st,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": st,
	})
}

// GET /api/v1/admin/enterprise-knowledge/tenants
// Query: tenant_id (optional), include_users=true|false
func (s *HTTPServer) handleAdminEnterpriseKnowledgeTenantProgress(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	includeUsers := strings.EqualFold(r.URL.Query().Get("include_users"), "true") || r.URL.Query().Get("include_users") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	rep, err := s.enterpriseSync.TenantProgress(ctx, tenantID, includeUsers)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// DELETE /api/v1/enterprise-knowledge/libraries/{libraryId}?confirm=true
func (s *HTTPServer) handleEnterpriseKnowledgePurgeLibrary(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	if !strings.EqualFold(r.URL.Query().Get("confirm"), "true") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	libraryID := strings.TrimSpace(r.PathValue("libraryId"))
	if libraryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "libraryId required"})
		return
	}
	if err := s.enterpriseSync.PurgeLibrary(p, libraryID); err != nil {
		writeJSON(w, enterprisePurgeHTTPStatus(err), map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	libs, _ := s.enterpriseSync.ListLibraries(p)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"purged":     libraryID,
		"libraries":  libs,
		"library_count": len(libs),
	})
}

// DELETE /api/v1/admin/enterprise-knowledge/tenants/{tenantId}/users/{userId}/libraries/{libraryId}?confirm=true
// Owner-only (matches Admin Web guard on data-ent-purge).
func (s *HTTPServer) handleAdminEnterpriseKnowledgePurgeLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.requireEnterpriseSync(w) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !strings.EqualFold(r.URL.Query().Get("confirm"), "true") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	tenantID := strings.TrimSpace(r.PathValue("tenantId"))
	userID := strings.TrimSpace(r.PathValue("userId"))
	libraryID := strings.TrimSpace(r.PathValue("libraryId"))
	if tenantID == "" || userID == "" || libraryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantId, userId, and libraryId are required"})
		return
	}
	p := agentservice.Principal{TenantID: tenantID, UserID: userID}
	if err := s.enterpriseSync.PurgeLibrary(p, libraryID); err != nil {
		writeJSON(w, enterprisePurgeHTTPStatus(err), map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	libs, _ := s.enterpriseSync.ListLibraries(p)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"tenant_id":     tenantID,
		"user_id":       userID,
		"purged":        libraryID,
		"libraries":     libs,
		"library_count": len(libs),
	})
}

// searchEnterpriseForPrincipal returns active enterprise hits for REST knowledge search merge.
func (s *HTTPServer) searchEnterpriseForPrincipal(ctx context.Context, p agentservice.Principal, query string) []knowledge.SearchResult {
	if s == nil || s.svc == nil || strings.TrimSpace(query) == "" {
		return nil
	}
	dataDir := s.svc.UserDataRoot(p)
	if dataDir == "" {
		return nil
	}
	hits, err := enterpriseknowledge.SearchActiveFromDataDir(ctx, dataDir, query, "")
	if err != nil {
		return nil
	}
	return hits
}

// mergeKnowledgeAPIResults is a thin alias used by HTTP knowledge search.
func mergeKnowledgeAPIResults(personal, enterprise []knowledge.SearchResult, limit int) []knowledge.SearchResult {
	return enterpriseknowledge.MergeSearchResults(personal, enterprise, limit, true)
}
