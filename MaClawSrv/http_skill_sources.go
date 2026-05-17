package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func (s *HTTPServer) handleSkillSourcesAvailable(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": cskill.AllSkillSources,
		"description": map[string]string{
			"skillhub": "SkillHub/SkillMarket",
			"clawhub":  "ClawHub (community mirror)",
			"github":   "GitHub (open source)",
		},
	})
}

func (s *HTTPServer) handleSkillSourcesGetGlobal(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.skillSourceSvc.GetGlobal(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetGlobal(w http.ResponseWriter, r *http.Request) {
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetGlobal(r.Context(), &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_global_updated", "skill_source_policy", "global", skillSourceAuditMetadata(r, &cfg, ""))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesGetTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, err := s.skillSourceSvc.GetTenant(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetTenant(r.Context(), id, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_updated", "skill_source_policy", id, skillSourceAuditMetadata(r, &cfg, id))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesDeleteTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.skillSourceSvc.DeleteTenant(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_deleted", "skill_source_policy", id, map[string]string{"tenant_id": id, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesGetUser(w http.ResponseWriter, r *http.Request) {
	email := decodeEmail(r)
	cfg, err := s.skillSourceSvc.GetUser(r.Context(), email)
	if err != nil {
		writeError(w, err)
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetUser(w http.ResponseWriter, r *http.Request) {
	email := decodeEmail(r)
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetUser(r.Context(), email, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_user_updated", "skill_source_policy", email, skillSourceAuditMetadata(r, &cfg, email))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesDeleteUser(w http.ResponseWriter, r *http.Request) {
	email := decodeEmail(r)
	if err := s.skillSourceSvc.DeleteUser(r.Context(), email); err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_user_deleted", "skill_source_policy", email, map[string]string{"email": email, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesResolve(w http.ResponseWriter, r *http.Request) {
	email := decodeEmail(r)
	tenantID := r.URL.Query().Get("tenant_id")
	resolved := s.skillSourceSvc.ResolveForUser(r.Context(), email, tenantID)
	if resolved == nil {
		resolved = cskill.AllSkillSources
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":           email,
		"tenant_id":       tenantID,
		"allowed_sources": resolved,
	})
}

func skillSourceAuditMetadata(r *http.Request, cfg *cskill.SourceControlConfig, subject string) map[string]string {
	metadata := map[string]string{
		"enabled":         strconv.FormatBool(cfg.Enabled),
		"allowed_sources": strings.Join(cfg.AllowedSources, ","),
		"remote_ip":       requestClientIP(r),
	}
	if subject != "" {
		metadata["subject"] = subject
	}
	return metadata
}

func decodeEmail(r *http.Request) string {
	raw := r.PathValue("email")
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}
