package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func (s *HTTPServer) handleSkillSourcesAvailable(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": cskill.AllSkillSources,
		"description": map[string]string{
			"skillhub":       "SkillHub/SkillMarket",
			"clawhub":        "ClawHub (community mirror)",
			"github":         "GitHub (open source)",
			"enterprise_hub": "Enterprise Hub",
			"local":          "Local zip/import upload",
		},
	})
}

func (s *HTTPServer) handleSkillSourcesGetGlobal(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.skillSourceSvc.GetGlobal(r.Context())
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetGlobal(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetGlobal(r.Context(), &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_global_updated", "skill_source_policy", "global", skillSourceAuditMetadata(r, &cfg, ""))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesGetTenant(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cfg, err := s.skillSourceSvc.GetTenant(r.Context(), id)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	id := r.PathValue("id")
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetTenant(r.Context(), id, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_updated", "skill_source_policy", id, skillSourceAuditMetadata(r, &cfg, id))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.skillSourceSvc.DeleteTenant(r.Context(), id); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_deleted", "skill_source_policy", id, map[string]string{"tenant_id": id, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesGetTenantUser(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingTenantUser(w, r, tenantID, userID) {
		return
	}
	cfg, err := s.skillSourceSvc.GetTenantUser(r.Context(), tenantID, userID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if cfg == nil {
		cfg = &cskill.SourceControlConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleSkillSourcesSetTenantUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingTenantUser(w, r, tenantID, userID) {
		return
	}
	var cfg cskill.SourceControlConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if err := s.skillSourceSvc.SetTenantUser(r.Context(), tenantID, userID, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_user_updated", "skill_source_policy", tenantID+":"+userID, skillSourceAuditMetadata(r, &cfg, tenantID+":"+userID))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesDeleteTenantUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingTenantUser(w, r, tenantID, userID) {
		return
	}
	if err := s.skillSourceSvc.DeleteTenantUser(r.Context(), tenantID, userID); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.skill_sources_tenant_user_deleted", "skill_source_policy", tenantID+":"+userID, map[string]string{"tenant_id": tenantID, "user_id": userID, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleSkillSourcesResolveTenantUser(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingTenantUser(w, r, tenantID, userID) {
		return
	}
	resolved := s.skillSourceSvc.ResolveForUser(r.Context(), userID, tenantID)
	if resolved == nil {
		resolved = cskill.AllSkillSources
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":       tenantID,
		"user_id":         userID,
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
