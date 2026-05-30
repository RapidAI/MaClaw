package skill

import (
	"encoding/json"
	"net/http"
	"strings"
)

type TenantAccessResolver func(r *http.Request) (tenantID string, global bool)

// RegisterRoutes registers the skill source control REST API routes.
// All routes require admin authentication (caller wraps with RequireAdmin).
// User-level source policy is scoped by runtime tenant_id + user_id.
func RegisterRoutes(mux *http.ServeMux, svc *SourceControlService, adminWrap func(http.HandlerFunc) http.HandlerFunc, accessOpt ...TenantAccessResolver) {
	var access TenantAccessResolver
	if len(accessOpt) > 0 {
		access = accessOpt[0]
	}
	mux.HandleFunc("GET /api/admin/skill-sources/available", adminWrap(handleAvailableSources()))
	mux.HandleFunc("GET /api/admin/skill-sources/global", adminWrap(handleGetGlobal(svc, access)))
	mux.HandleFunc("PUT /api/admin/skill-sources/global", adminWrap(handleSetGlobal(svc, access)))
	mux.HandleFunc("GET /api/admin/skill-sources/tenant/{id}", adminWrap(handleGetTenant(svc, access)))
	mux.HandleFunc("PUT /api/admin/skill-sources/tenant/{id}", adminWrap(handleSetTenant(svc, access)))
	mux.HandleFunc("DELETE /api/admin/skill-sources/tenant/{id}", adminWrap(handleDeleteTenant(svc, access)))
	mux.HandleFunc("GET /api/admin/skill-sources/tenants/{tenantId}/users/{userId}", adminWrap(handleGetTenantUser(svc, access)))
	mux.HandleFunc("PUT /api/admin/skill-sources/tenants/{tenantId}/users/{userId}", adminWrap(handleSetTenantUser(svc, access)))
	mux.HandleFunc("DELETE /api/admin/skill-sources/tenants/{tenantId}/users/{userId}", adminWrap(handleDeleteTenantUser(svc, access)))
	mux.HandleFunc("GET /api/admin/skill-sources/tenants/{tenantId}/users/{userId}/resolve", adminWrap(handleResolveTenantUser(svc, access)))
}

func handleAvailableSources() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"sources": AllSources,
			"description": map[string]string{
				"skillhub":       "SkillHub/SkillMarket",
				"clawhub":        "ClawHub",
				"github":         "GitHub",
				"enterprise_hub": "Enterprise Hub",
				"local":          "Local zip/import upload",
			},
		})
	}
}

func handleGetGlobal(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, global := resolveTenantAccess(r, access); !global {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "global skill source config requires global admin")
			return
		}
		cfg, err := svc.GetGlobal(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, defaultSourceControlConfig(cfg))
	}
}

func handleSetGlobal(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, global := resolveTenantAccess(r, access); !global {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "global skill source config requires global admin")
			return
		}
		var cfg SourceControlConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := svc.SetGlobal(r.Context(), &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "SET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGetTenant(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := tenantIDFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
			return
		}
		cfg, err := svc.GetTenant(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, defaultSourceControlConfig(cfg))
	}
}

func handleSetTenant(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := tenantIDFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
			return
		}
		var cfg SourceControlConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := svc.SetTenant(r.Context(), id, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "SET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleDeleteTenant(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := tenantIDFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
			return
		}
		if err := svc.DeleteTenant(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleGetTenantUser(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, userID, ok := tenantUserIDsFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		cfg, err := svc.GetTenantUser(r.Context(), tenantID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, defaultSourceControlConfig(cfg))
	}
}

func handleSetTenantUser(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, userID, ok := tenantUserIDsFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		var cfg SourceControlConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := svc.SetTenantUser(r.Context(), tenantID, userID, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "SET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleDeleteTenantUser(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, userID, ok := tenantUserIDsFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		if err := svc.DeleteTenantUser(r.Context(), tenantID, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleResolveTenantUser(svc *SourceControlService, access TenantAccessResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID, userID, ok := tenantUserIDsFromPathOrAccess(w, r, access)
		if !ok {
			return
		}
		resolved := svc.ResolveForUser(r.Context(), userID, tenantID)
		if resolved == nil {
			resolved = AllSources
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id":       tenantID,
			"user_id":         userID,
			"allowed_sources": resolved,
		})
	}
}

func resolveTenantAccess(r *http.Request, access TenantAccessResolver) (string, bool) {
	if access == nil {
		return "", true
	}
	tenantID, global := access(r)
	return strings.TrimSpace(tenantID), global
}

func tenantIDFromPathOrAccess(w http.ResponseWriter, r *http.Request, access TenantAccessResolver) (string, bool) {
	pathTenantID := strings.TrimSpace(r.PathValue("id"))
	tenantID, global := resolveTenantAccess(r, access)
	if global {
		return pathTenantID, true
	}
	if pathTenantID != "" && pathTenantID != tenantID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "tenant admin can only manage own tenant")
		return "", false
	}
	return tenantID, true
}

func tenantUserIDsFromPathOrAccess(w http.ResponseWriter, r *http.Request, access TenantAccessResolver) (string, string, bool) {
	pathTenantID := strings.TrimSpace(r.PathValue("tenantId"))
	userID := strings.TrimSpace(r.PathValue("userId"))
	if userID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "user id is required")
		return "", "", false
	}
	tenantID, global := resolveTenantAccess(r, access)
	if global {
		if pathTenantID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
			return "", "", false
		}
		return pathTenantID, userID, true
	}
	if pathTenantID != "" && pathTenantID != tenantID {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "tenant admin can only manage own tenant")
		return "", "", false
	}
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
		return "", "", false
	}
	return tenantID, userID, true
}

func defaultSourceControlConfig(cfg *SourceControlConfig) *SourceControlConfig {
	if cfg == nil {
		return &SourceControlConfig{Enabled: false, AllowedSources: nil}
	}
	return cfg
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":   code,
		"message": message,
	})
}
