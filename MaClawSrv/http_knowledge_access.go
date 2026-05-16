package main

import (
	"net/http"

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
	var cfg knowledgeAccessConfig
	if err := readJSONBody(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.knowledgeMgr.Access().SetUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	if err := s.knowledgeMgr.Access().DeleteUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId")); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessResolveUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.knowledgeMgr.Access().ResolveResponse(r.Context(), r.PathValue("tenantId"), r.PathValue("userId")))
}
