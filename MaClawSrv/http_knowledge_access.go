package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type knowledgeAccessScopeView struct {
	TenantID   string `json:"tenant_id"`
	OwnerID    string `json:"owner_id"`
	Name       string `json:"name,omitempty"`
	ScopeType  string `json:"scope_type"`
	TenantName string `json:"tenant_name,omitempty"`
	OwnerName  string `json:"owner_name,omitempty"`
}

type knowledgeAccessResolvedView struct {
	TenantID           string                     `json:"tenant_id"`
	UserID             string                     `json:"user_id"`
	CrossTenantEnabled bool                       `json:"cross_tenant_enabled"`
	Scopes             []knowledgeAccessScopeView `json:"scopes"`
}

func (s *HTTPServer) handleKnowledgeAccessGetMe(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := strings.TrimSpace(p.TenantID)
	userID := strings.TrimSpace(p.UserID)
	resolved := knowledgeAccessResolved{TenantID: tenantID, UserID: userID, Scopes: []knowledgeScope{{TenantID: tenantID, OwnerID: userID, Name: "self"}}}
	if access := s.knowledgeMgr.Access(); access != nil {
		resolved = access.ResolveResponse(r.Context(), tenantID, userID)
	}
	writeJSON(w, http.StatusOK, s.knowledgeAccessResolvedView(r.Context(), resolved, tenantID, userID))
}

func (s *HTTPServer) knowledgeAccessResolvedView(ctx context.Context, resolved knowledgeAccessResolved, selfTenantID, selfUserID string) knowledgeAccessResolvedView {
	view := knowledgeAccessResolvedView{
		TenantID:           resolved.TenantID,
		UserID:             resolved.UserID,
		CrossTenantEnabled: resolved.CrossTenantEnabled,
		Scopes:             make([]knowledgeAccessScopeView, 0, len(resolved.Scopes)),
	}
	for _, scope := range resolved.Scopes {
		view.Scopes = append(view.Scopes, s.knowledgeAccessScopeView(ctx, scope, selfTenantID, selfUserID))
	}
	return view
}

func (s *HTTPServer) knowledgeAccessScopeView(ctx context.Context, scope knowledgeScope, selfTenantID, selfUserID string) knowledgeAccessScopeView {
	tenantID := strings.TrimSpace(scope.TenantID)
	ownerID := strings.TrimSpace(scope.OwnerID)
	name := strings.TrimSpace(scope.Name)
	out := knowledgeAccessScopeView{TenantID: tenantID, OwnerID: ownerID, Name: name, ScopeType: "user"}
	if s.svc != nil {
		if tenant, err := s.svc.GetTenant(ctx, tenantID); err == nil && tenant != nil {
			out.TenantName = strings.TrimSpace(tenant.Name)
		}
	}
	if isPublicKnowledgeScope(scope) {
		out.ScopeType = "public"
		if access := s.knowledgeMgr.Access(); access != nil {
			libraryID := strings.TrimPrefix(ownerID, publicKnowledgeOwnerPrefix)
			if library, ok, err := access.GetPublicLibrary(ctx, libraryID); err == nil && ok {
				if out.Name == "" {
					out.Name = strings.TrimSpace(library.Name)
				}
				out.OwnerName = strings.TrimSpace(library.Name)
			}
		}
		return out
	}
	if tenantID == strings.TrimSpace(selfTenantID) && ownerID == strings.TrimSpace(selfUserID) {
		out.ScopeType = "self"
	}
	if s.svc != nil {
		if user, err := s.svc.GetUser(ctx, tenantID, ownerID); err == nil && user != nil {
			out.OwnerName = strings.TrimSpace(user.Name)
		}
	}
	return out
}

func (s *HTTPServer) handleAdminKnowledgeAccessGetCrossTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	cfg, err := s.knowledgeMgr.Access().GetCrossTenant(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminKnowledgeAccessSetCrossTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	var cfg knowledgeCrossTenantConfig
	if err := readJSONBody(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if err := s.knowledgeMgr.Access().SetCrossTenant(r.Context(), cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
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
	if !s.requireExistingKnowledgeUser(w, r, tenantID, userID) {
		return
	}
	cfg, err := s.knowledgeMgr.Access().GetUser(r.Context(), tenantID, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if cfg == nil {
		cfg = &knowledgeAccessConfig{Enabled: false}
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminKnowledgeAccessSetUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingKnowledgeUser(w, r, tenantID, userID) {
		return
	}
	var cfg knowledgeAccessConfig
	if err := readJSONBody(r, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if err := normalizeKnowledgeAccessConfig(tenantID, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !s.requireExistingKnowledgeScopeUsers(w, r, cfg.ReadScopes) {
		return
	}
	if err := s.knowledgeMgr.Access().SetUser(r.Context(), tenantID, userID, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
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
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingKnowledgeUser(w, r, tenantID, userID) {
		return
	}
	if err := s.knowledgeMgr.Access().DeleteUser(r.Context(), tenantID, userID); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.knowledge_access_user_deleted", "knowledge_access", tenantID+"/"+userID, map[string]string{
		"tenant_id": tenantID,
		"user_id":   userID,
		"remote_ip": requestClientIP(r),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *HTTPServer) handleAdminKnowledgeAccessAttachPublicLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingKnowledgeUser(w, r, tenantID, userID) {
		return
	}
	library, ok, err := s.knowledgeMgr.Access().GetPublicLibrary(r.Context(), r.PathValue("libraryId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "public knowledge library not found"})
		return
	}
	cfg, err := s.knowledgeMgr.Access().GetUser(r.Context(), tenantID, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if cfg == nil {
		cfg = &knowledgeAccessConfig{}
	}
	cfg.Enabled = true
	if !containsKnowledgeScope(cfg.ReadScopes, library.TenantID, library.OwnerID) {
		cfg.ReadScopes = append(cfg.ReadScopes, knowledgeScope{TenantID: library.TenantID, OwnerID: library.OwnerID, Name: library.Name})
	}
	if err := s.knowledgeMgr.Access().SetUser(r.Context(), tenantID, userID, cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	s.recordPublicKnowledgeAccessAudit(r, "admin.knowledge_access_public_library_attached", tenantID, userID, library, cfg)
	writeJSON(w, http.StatusOK, cfg)
}

func (s *HTTPServer) handleAdminKnowledgeAccessDetachPublicLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if !s.requireKnowledge(w) {
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if !s.requireExistingKnowledgeUser(w, r, tenantID, userID) {
		return
	}
	library, ok, err := s.knowledgeMgr.Access().GetPublicLibrary(r.Context(), r.PathValue("libraryId"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "public knowledge library not found"})
		return
	}
	cfg, err := s.knowledgeMgr.Access().GetUser(r.Context(), tenantID, userID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	if cfg == nil {
		cfg = &knowledgeAccessConfig{}
	}
	cfg.ReadScopes = removeKnowledgeScope(cfg.ReadScopes, library.TenantID, library.OwnerID)
	if err := s.knowledgeMgr.Access().SetUser(r.Context(), tenantID, userID, cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	s.recordPublicKnowledgeAccessAudit(r, "admin.knowledge_access_public_library_detached", tenantID, userID, library, cfg)
	writeJSON(w, http.StatusOK, cfg)
}

func removeKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) []knowledgeScope {
	tenantID = strings.TrimSpace(tenantID)
	ownerID = strings.TrimSpace(ownerID)
	next := scopes[:0]
	for _, scope := range scopes {
		if strings.TrimSpace(scope.TenantID) == tenantID && strings.TrimSpace(scope.OwnerID) == ownerID {
			continue
		}
		next = append(next, scope)
	}
	return next
}

func containsKnowledgeScope(scopes []knowledgeScope, tenantID, ownerID string) bool {
	tenantID = strings.TrimSpace(tenantID)
	ownerID = strings.TrimSpace(ownerID)
	for _, scope := range scopes {
		if strings.TrimSpace(scope.TenantID) == tenantID && strings.TrimSpace(scope.OwnerID) == ownerID {
			return true
		}
	}
	return false
}

func (s *HTTPServer) recordPublicKnowledgeAccessAudit(r *http.Request, action, tenantID, userID string, library publicKnowledgeLibrary, cfg *knowledgeAccessConfig) {
	_ = s.recordAdminAudit(r.Context(), action, "knowledge_access", tenantID+"/"+userID, map[string]string{
		"tenant_id":    tenantID,
		"user_id":      userID,
		"library_id":   library.ID,
		"library_name": library.Name,
		"owner_id":     library.OwnerID,
		"scope_count":  strconv.Itoa(len(cfg.ReadScopes)),
		"remote_ip":    requestClientIP(r),
	})
}

func (s *HTTPServer) requireExistingKnowledgeUser(w http.ResponseWriter, r *http.Request, tenantID, userID string) bool {
	return s.requireExistingTenantUser(w, r, tenantID, userID)
}

func (s *HTTPServer) requireExistingKnowledgeScopeUsers(w http.ResponseWriter, r *http.Request, scopes []knowledgeScope) bool {
	for _, scope := range scopes {
		if isPublicKnowledgeScope(scope) {
			continue
		}
		if !s.requireExistingKnowledgeUser(w, r, scope.TenantID, scope.OwnerID) {
			return false
		}
	}
	return true
}

func (s *HTTPServer) handleAdminKnowledgeAccessResolveUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireKnowledge(w) {
		return
	}
	if !s.requireExistingKnowledgeUser(w, r, r.PathValue("tenantId"), r.PathValue("userId")) {
		return
	}
	writeJSON(w, http.StatusOK, s.knowledgeMgr.Access().ResolveResponse(r.Context(), r.PathValue("tenantId"), r.PathValue("userId")))
}
