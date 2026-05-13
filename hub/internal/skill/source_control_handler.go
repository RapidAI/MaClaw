package skill

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// RegisterRoutes registers the skill source control REST API routes.
// All routes require admin authentication (caller wraps with RequireAdmin).
//
// Endpoints:
//
//	GET    /api/admin/skill-sources/available          — list valid source identifiers
//	GET    /api/admin/skill-sources/global             — get global config
//	PUT    /api/admin/skill-sources/global             — set global config
//	GET    /api/admin/skill-sources/tenant/{id}        — get tenant config
//	PUT    /api/admin/skill-sources/tenant/{id}        — set tenant config
//	DELETE /api/admin/skill-sources/tenant/{id}        — delete tenant config
//	GET    /api/admin/skill-sources/user/{email}       — get user config
//	PUT    /api/admin/skill-sources/user/{email}       — set user config
//	DELETE /api/admin/skill-sources/user/{email}       — delete user config
//	GET    /api/admin/skill-sources/resolve/{email}    — resolve effective sources for user
func RegisterRoutes(mux *http.ServeMux, svc *SourceControlService, adminWrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/admin/skill-sources/available", adminWrap(handleAvailableSources()))
	mux.HandleFunc("GET /api/admin/skill-sources/global", adminWrap(handleGetGlobal(svc)))
	mux.HandleFunc("PUT /api/admin/skill-sources/global", adminWrap(handleSetGlobal(svc)))
	mux.HandleFunc("GET /api/admin/skill-sources/tenant/{id}", adminWrap(handleGetTenant(svc)))
	mux.HandleFunc("PUT /api/admin/skill-sources/tenant/{id}", adminWrap(handleSetTenant(svc)))
	mux.HandleFunc("DELETE /api/admin/skill-sources/tenant/{id}", adminWrap(handleDeleteTenant(svc)))
	mux.HandleFunc("GET /api/admin/skill-sources/user/{email...}", adminWrap(handleGetUser(svc)))
	mux.HandleFunc("PUT /api/admin/skill-sources/user/{email...}", adminWrap(handleSetUser(svc)))
	mux.HandleFunc("DELETE /api/admin/skill-sources/user/{email...}", adminWrap(handleDeleteUser(svc)))
	mux.HandleFunc("GET /api/admin/skill-sources/resolve/{email...}", adminWrap(handleResolve(svc)))
}

// --- Handlers ---

func handleAvailableSources() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"sources": AllSources,
			"description": map[string]string{
				"skillhub": "SkillHub/SkillMarket (官方技能市场)",
				"clawhub":  "ClawHub (社区技能镜像)",
				"github":   "GitHub (开源技能仓库)",
			},
		})
	}
}

func handleGetGlobal(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := svc.GetGlobal(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		if cfg == nil {
			cfg = &SourceControlConfig{Enabled: false, AllowedSources: nil}
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func handleSetGlobal(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

func handleGetTenant(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "tenant id is required")
			return
		}
		cfg, err := svc.GetTenant(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		if cfg == nil {
			cfg = &SourceControlConfig{Enabled: false, AllowedSources: nil}
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func handleSetTenant(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
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

func handleDeleteTenant(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
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

func handleGetUser(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := decodePathEmail(r)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		cfg, err := svc.GetUser(r.Context(), email)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LOAD_FAILED", err.Error())
			return
		}
		if cfg == nil {
			cfg = &SourceControlConfig{Enabled: false, AllowedSources: nil}
		}
		writeJSON(w, http.StatusOK, cfg)
	}
}

func handleSetUser(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := decodePathEmail(r)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		var cfg SourceControlConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		if err := svc.SetUser(r.Context(), email, &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "SET_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleDeleteUser(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := decodePathEmail(r)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		if err := svc.DeleteUser(r.Context(), email); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func handleResolve(svc *SourceControlService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := decodePathEmail(r)
		if email == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "email is required")
			return
		}
		tenantID := r.URL.Query().Get("tenant_id")
		resolved := svc.ResolveForUser(r.Context(), email, tenantID)
		if resolved == nil {
			resolved = AllSources // explicit "all" for API clarity
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"email":           email,
			"tenant_id":       tenantID,
			"allowed_sources": resolved,
		})
	}
}

// --- Helpers ---

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

// decodePathEmail extracts and URL-decodes the email from the path.
// Uses {email...} wildcard pattern to capture the full email including dots.
func decodePathEmail(r *http.Request) string {
	raw := r.PathValue("email")
	if raw == "" {
		return ""
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}
