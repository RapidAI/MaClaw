package main

import (
	"net/http"
	"strconv"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func (s *HTTPServer) handleKnowledgeAccessGetMe(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.knowledgeMgr.Access().ResolveResponse(r.Context(), p.TenantID, p.UserID))
}

func (s *HTTPServer) handleAdminKnowledgeAccessGetCrossTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	cfg, err := s.knowledgeMgr.Access().GetCrossTenant(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminKnowledgeAccessSetCrossTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	var cfg knowledgeCrossTenantConfig
	if err := readJSONBody(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.knowledgeMgr.Access().SetCrossTenant(r.Context(), cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_access_cross_tenant_updated", "knowledge_access", "cross_tenant", map[string]string{
		"enabled":   strconv.FormatBool(cfg.Enabled),
		"remote_ip": requestClientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessGetUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	cfg, err := s.knowledgeMgr.Access().GetUser(r.Context(), tenantID, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if cfg == nil {
		cfg = &knowledgeAccessConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminKnowledgeAccessSetUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	var cfg knowledgeAccessConfig
	if err := readJSONBody(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.knowledgeMgr.Access().SetUser(r.Context(), tenantID, userID, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_access_user_updated", "knowledge_access", tenantID+"/"+userID, map[string]string{
		"tenant_id":    tenantID,
		"user_id":      userID,
		"enabled":      strconv.FormatBool(cfg.Enabled),
		"scope_count":  strconv.Itoa(len(cfg.ReadScopes)),
		"remote_ip":    requestClientIP(r),
		"cross_tenant": strconv.FormatBool(knowledgeAccessConfigHasCrossTenantScope(tenantID, &cfg)),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if err := s.knowledgeMgr.Access().DeleteUser(r.Context(), tenantID, userID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_access_user_deleted", "knowledge_access", tenantID+"/"+userID, map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
		"remote_ip": requestClientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessResolveUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.knowledgeMgr.Access().ResolveResponse(r.Context(), r.PathValue("tenantId"), r.PathValue("userId")))
}
