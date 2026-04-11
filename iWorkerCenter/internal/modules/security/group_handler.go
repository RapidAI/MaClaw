package security

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// GroupHandler provides HTTP endpoints for security group management.
type GroupHandler struct {
	repo *GroupRepo
}

// NewGroupHandler creates a GroupHandler.
func NewGroupHandler(repo *GroupRepo) *GroupHandler {
	return &GroupHandler{repo: repo}
}

// RegisterAdminRoutes registers group management routes.
func (h *GroupHandler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/security/groups", h.handleGroups)
	mux.HandleFunc("/admin/security/groups/", h.handleGroupByID)
	mux.HandleFunc("/admin/security/settings", h.handleSettings)
	mux.HandleFunc("/admin/security/settings/default-group", h.handleDefaultGroup)
}

func (h *GroupHandler) handleGroups(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		tree, err := h.repo.GetGroupTree(r.Context(), tid)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"tree": tree})
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			ParentID string `json:"parent_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			response.BadRequest(w, "INVALID_INPUT", "name is required")
			return
		}
		if req.ParentID == "" {
			response.BadRequest(w, "INVALID_INPUT", "parent_id is required")
			return
		}
		parent, err := h.repo.GetGroupByID(r.Context(), tid, req.ParentID)
		if err != nil || parent == nil {
			response.NotFound(w, "NOT_FOUND", "parent group not found")
			return
		}
		depth, err := h.repo.GetGroupDepth(r.Context(), tid, req.ParentID)
		if err == nil && depth+1 >= 10 {
			response.BadRequest(w, "DEPTH_EXCEEDED", "group tree depth exceeds maximum (10)")
			return
		}
		now := time.Now().UTC()
		g := &SecurityGroup{
			ID:        idgen.New("sgrp"),
			Name:      strings.TrimSpace(req.Name),
			ParentID:  req.ParentID,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := h.repo.CreateGroup(r.Context(), tid, g); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true, "group": g})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *GroupHandler) handleGroupByID(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	// Parse: /admin/security/groups/{id}[/members[/{email}]][/policy]
	rest := strings.TrimPrefix(r.URL.Path, "/admin/security/groups/")
	rest = strings.TrimRight(rest, "/")
	parts := strings.SplitN(rest, "/", 3)
	id := parts[0]
	if id == "" {
		response.BadRequest(w, "INVALID_INPUT", "id is required")
		return
	}

	if len(parts) >= 2 && parts[1] == "members" {
		if len(parts) == 3 {
			// DELETE /admin/security/groups/{id}/members/{email}
			email, _ := url.PathUnescape(parts[2])
			h.handleRemoveMember(w, r, tid, id, email)
			return
		}
		h.handleMembers(w, r, tid, id)
		return
	}
	if len(parts) >= 2 && parts[1] == "policy" {
		h.handlePolicy(w, r, tid, id)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			response.BadRequest(w, "INVALID_INPUT", "name is required")
			return
		}
		if err := h.repo.UpdateGroupName(r.Context(), tid, id, strings.TrimSpace(req.Name)); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true})
	case http.MethodDelete:
		if err := h.repo.DeleteGroup(r.Context(), tid, id); err != nil {
			if strings.Contains(err.Error(), "cannot delete root") {
				response.Error(w, http.StatusForbidden, "FORBIDDEN", "cannot delete root group")
				return
			}
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT or DELETE")
	}
}

func (h *GroupHandler) handleMembers(w http.ResponseWriter, r *http.Request, tenantID string, groupID string) {
	switch r.Method {
	case http.MethodGet:
		members, err := h.repo.ListGroupMembers(r.Context(), tenantID, groupID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		children, err := h.repo.GetGroupChildren(r.Context(), tenantID, groupID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"members": members, "children": children})
	case http.MethodPost:
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_JSON", "invalid request body")
			return
		}
		if strings.TrimSpace(req.Email) == "" {
			response.BadRequest(w, "INVALID_INPUT", "email is required")
			return
		}
		if err := h.repo.AssignUser(r.Context(), tenantID, strings.TrimSpace(req.Email), groupID); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *GroupHandler) handleRemoveMember(w http.ResponseWriter, r *http.Request, tenantID string, groupID, email string) {
	if r.Method != http.MethodDelete {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use DELETE")
		return
	}
	if err := h.repo.RemoveUser(r.Context(), tenantID, groupID, email); err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"ok": true})
}

func (h *GroupHandler) handlePolicy(w http.ResponseWriter, r *http.Request, tenantID string, groupID string) {
	switch r.Method {
	case http.MethodGet:
		policy, err := h.repo.GetGroupPolicy(r.Context(), tenantID, groupID)
		if err != nil {
			response.Internal(w, err.Error())
			return
		}
		// Build policy view with inheritance
		view := h.buildPolicyView(r, tenantID, groupID, policy)
		response.OK(w, view)
	case http.MethodPut:
		var req struct {
			Policy map[string]interface{} `json:"policy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_JSON", "invalid request body")
			return
		}
		if req.Policy == nil {
			response.BadRequest(w, "INVALID_INPUT", "policy is required")
			return
		}
		if err := h.repo.UpdateGroupPolicy(r.Context(), tenantID, groupID, req.Policy); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *GroupHandler) buildPolicyView(r *http.Request, tenantID string, groupID string, selfPolicy map[string]interface{}) GroupPolicyView {
	// Walk from root to this group, collecting policies
	chain := h.getAncestorChain(r, tenantID, groupID)
	view := GroupPolicyView{GroupID: groupID, Items: make(map[string]PolicyItemView)}

	type policyField struct {
		Key          string
		DefaultValue interface{}
	}
	fields := []policyField{
		{"file_outbound_enabled", true},
		{"image_outbound_enabled", true},
		{"gossip_enabled", true},
		{"guardrail_mode", "standard"},
		{"sandbox_mode", "none"},
		{"network_level", "full"},
		{"yolo_mode_allowed", true},
		{"smart_route_enabled", true},
	}

	for _, f := range fields {
		item := PolicyItemView{Value: f.DefaultValue, Source: "inherited", SourceGroup: "", SourceName: "默认"}
		// Walk chain from root to leaf
		for _, ancestor := range chain {
			if val, ok := ancestor.policy[f.Key]; ok {
				item = PolicyItemView{Value: val, Source: "inherited", SourceGroup: ancestor.id, SourceName: ancestor.name}
			}
		}
		// Check self
		if val, ok := selfPolicy[f.Key]; ok {
			item = PolicyItemView{Value: val, Source: "self", SourceGroup: groupID, SourceName: ""}
		}
		view.Items[f.Key] = item
	}
	return view
}

type ancestorInfo struct {
	id, name string
	policy   map[string]interface{}
}

func (h *GroupHandler) getAncestorChain(r *http.Request, tenantID string, groupID string) []ancestorInfo {
	// Walk up from groupID to root, then reverse
	var chain []ancestorInfo
	current := groupID
	for i := 0; i < 20; i++ {
		g, err := h.repo.GetGroupByID(r.Context(), tenantID, current)
		if err != nil || g == nil {
			break
		}
		if g.ParentID == "" {
			// root - get its policy and prepend
			p, _ := h.repo.GetGroupPolicy(r.Context(), tenantID, g.ID)
			chain = append([]ancestorInfo{{id: g.ID, name: g.Name, policy: p}}, chain...)
			break
		}
		p, _ := h.repo.GetGroupPolicy(r.Context(), tenantID, g.ID)
		chain = append([]ancestorInfo{{id: g.ID, name: g.Name, policy: p}}, chain...)
		current = g.ParentID
	}
	// Remove the target group itself from chain (it's handled separately)
	if len(chain) > 0 && chain[len(chain)-1].id == groupID {
		chain = chain[:len(chain)-1]
	}
	return chain
}

// --- Settings (stored as system_settings key-value) ---

const settingsKey = "iwc_security_settings"

func (h *GroupHandler) handleSettings(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	switch r.Method {
	case http.MethodGet:
		settings := h.loadSettings(tid)
		response.OK(w, settings)
	case http.MethodPut:
		var req SecuritySettings
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_JSON", "invalid request body")
			return
		}
		if err := h.saveSettings(tid, &req); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"ok": true})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *GroupHandler) handleDefaultGroup(w http.ResponseWriter, r *http.Request) {
	tid := tenant.TenantIDFromContext(r.Context())
	if r.Method != http.MethodPut {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}
	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_JSON", "invalid request body")
		return
	}
	if req.GroupID == "" {
		response.BadRequest(w, "INVALID_INPUT", "group_id is required")
		return
	}
	g, err := h.repo.GetGroupByID(r.Context(), tid, req.GroupID)
	if err != nil || g == nil {
		response.BadRequest(w, "NOT_FOUND", "group not found")
		return
	}
	settings := h.loadSettings(tid)
	settings.DefaultGroupID = req.GroupID
	if err := h.saveSettings(tid, settings); err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"ok": true})
}

func (h *GroupHandler) loadSettings(tenantID string) *SecuritySettings {
	data, err := h.repo.LoadSettings(tenantID, settingsKey)
	if err != nil {
		return &SecuritySettings{}
	}
	var s SecuritySettings
	if json.Unmarshal(data, &s) != nil {
		return &SecuritySettings{}
	}
	return &s
}

func (h *GroupHandler) saveSettings(tenantID string, s *SecuritySettings) error {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return h.repo.SaveSettings(tenantID, settingsKey, data)
}
