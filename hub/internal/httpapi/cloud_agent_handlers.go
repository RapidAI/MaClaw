package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudagent"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/remotecontrol"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func cloudAgentViewer(identity *auth.IdentityService) cloudagent.ViewerFunc {
	return func(r *http.Request) (*auth.ViewerPrincipal, error) {
		if identity == nil {
			return nil, auth.ErrInvalidUserCredentials
		}
		return authenticateViewerRequest(r, identity)
	}
}

func cloudAgentAdminSettings(svc *cloudagent.Service, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		var in cloudagent.AdminSettings
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID", "请同时填写开关和两个上限")
			return
		}
		view, err := svc.SaveAdminSettings(r.Context(), AdminTenantID(r.Context()), in)
		if err != nil {
			writeCloudAgentErr(w, err)
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "cloud_agent.settings.update", map[string]any{
			"cloud_agents_enabled": view.CloudAgentsEnabled,
			"max_instances":        view.MaxInstances,
			"max_concurrent_turns": view.MaxConcurrentTurns,
		})
		writeJSON(w, http.StatusOK, view)
	}
}

func cloudAgentAdminSettingsGet(svc *cloudagent.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		view, err := svc.AdminView(r.Context(), AdminTenantID(r.Context()))
		if err != nil {
			writeCloudAgentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func cloudAgentAdminUserLimits(svc *cloudagent.Service, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		var in cloudagent.UserLimits
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID", "限额无效")
			return
		}
		userID := strings.TrimSpace(r.PathValue("id"))
		if err := svc.SaveUserLimits(r.Context(), AdminTenantID(r.Context()), userID, in); err != nil {
			writeCloudAgentErr(w, err)
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "cloud_agent.user_limits.update", map[string]any{
			"user_id": userID,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func cloudAgentAdminAccessGet(svc *cloudagent.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		items, err := svc.ListAccess(r.Context(), AdminTenantID(r.Context()))
		if err != nil {
			writeCloudAgentErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func cloudAgentAdminAccessPut(svc *cloudagent.Service, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		var in cloudagent.AccessView
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID", "开通范围无效")
			return
		}
		saved, deleted, err := svc.SaveAccess(r.Context(), AdminTenantID(r.Context()), in)
		if err != nil {
			writeCloudAgentErr(w, err)
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "cloud_agent.access.update", map[string]any{
			"scope":     saved.Scope,
			"target_id": saved.TargetID,
			"enabled":   saved.Enabled,
			"deleted":   deleted,
		})
		if deleted {
			writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "scope": saved.Scope, "target_id": saved.TargetID})
			return
		}
		writeJSON(w, http.StatusOK, saved)
	}
}

func writeCloudAgentErr(w http.ResponseWriter, err error) {
	var failure *cloudagent.Failure
	if errors.As(err, &failure) {
		writeError(w, failure.HTTP, failure.Code, failure.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "CLOUD_AGENT", "云端管理暂时不可用")
}

// cloudAgentStream publishes one viewer event for a cloud grant.
// It does not call StreamGrant or Service.call. The envelope is the same
// remote_control.stream shape the desktop path uses.
func cloudAgentStream(rc *remotecontrol.Service, svc *cloudagent.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rc == nil || svc == nil {
			writeError(w, http.StatusServiceUnavailable, "OFFLINE", "云端运行时不可用")
			return
		}
		payload, err := io.ReadAll(io.LimitReader(r.Body, remoteControlStreamPayloadLimit+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID", "云端事件无效")
			return
		}
		if len(payload) == 0 || len(payload) > remoteControlStreamPayloadLimit {
			writeError(w, http.StatusRequestEntityTooLarge, "INVALID", "云端事件过大")
			return
		}
		var head struct {
			GrantID string `json:"grant_id"`
		}
		if err := json.Unmarshal(payload, &head); err != nil || strings.TrimSpace(head.GrantID) == "" {
			writeError(w, http.StatusBadRequest, "INVALID", "云端事件无效")
			return
		}
		grant, err := rc.ActiveCloudGrant(r.Context(), head.GrantID)
		if err != nil {
			writeRemoteControlError(w, err)
			return
		}
		srvTenant, _, srvNode, err := svc.InstanceOwner(r.Context(), grant.TargetAgentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到这个云端实例")
			return
		}
		// A node srv streams over the open network; the public connection
		// stays loopback-only.
		isNode, authed := svc.SrvAuth(r.Context(), srvTenant, srvNode, r.Header.Get("X-MaClaw-Admin-Secret"))
		if !isNode && !cloudagent.LoopbackAddr(r.RemoteAddr) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "云端事件只接受本机")
			return
		}
		if !authed {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "管理凭证无效")
			return
		}
		if !rc.AllowStream(grant.ID) {
			writeError(w, http.StatusTooManyRequests, "RATE", "云端事件过于频繁")
			return
		}
		if err := rc.SendController(grant.ControllerMachineID, remoteControlStreamEnvelope(grant.TargetMachineID, payload)); err != nil {
			writeRemoteControlError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

type cloudAgentOwnerKey struct{}

func withCloudAgentOwner(ctx context.Context, principal *auth.ViewerPrincipal) context.Context {
	return context.WithValue(ctx, cloudAgentOwnerKey{}, principal)
}

func cloudAgentOwnerPrincipal(r *http.Request) *auth.ViewerPrincipal {
	if r == nil {
		return nil
	}
	principal, _ := r.Context().Value(cloudAgentOwnerKey{}).(*auth.ViewerPrincipal)
	return principal
}

// cloudAgentLLMChat is the loopback official-model entry. It resolves the
// instance owner and then runs the existing responses handler.
func cloudAgentLLMChat(db *sql.DB, svc *cloudagent.Service, identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, promptCacheSources ...any) http.HandlerFunc {
	next := LLMV1ResponsesHandler(identity, system, securitySvc, promptCacheSources...)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		if svc == nil || db == nil {
			writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
			return
		}
		id := strings.TrimSpace(r.Header.Get("X-MaClaw-Cloud-Agent"))
		tenantID, userID, srvNode, err := svc.InstanceOwner(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到这个云端实例")
			return
		}
		isNode, authed := svc.SrvAuth(r.Context(), tenantID, srvNode, r.Header.Get("X-MaClaw-Admin-Secret"))
		if !isNode && !cloudagent.LoopbackAddr(r.RemoteAddr) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "云端运行时不可用")
			return
		}
		if !authed {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid admin secret")
			return
		}
		var email string
		err = db.QueryRowContext(r.Context(), `SELECT email FROM users WHERE id = ? AND tenant_id = ?`, userID, tenantID).Scan(&email)
		if err != nil || strings.TrimSpace(email) == "" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "没有找到实例主人")
			return
		}
		principal := &auth.ViewerPrincipal{TenantID: tenantID, UserID: userID, Email: email}
		next(w, r.WithContext(withCloudAgentOwner(r.Context(), principal)))
	}
}

// srvNodeView is the node summary both the user chooser and the admin
// bindings list share: name, a reachable address, and live link state.
func srvNodeView(node edgenode.Node) map[string]any {
	address := strings.TrimSpace(node.DirectBase)
	if address == "" {
		address = strings.TrimSpace(node.AdvertiseURL)
	}
	return map[string]any{
		"node_id": node.ID,
		"name":    strings.TrimSpace(node.Name),
		"address": address,
		"online":  node.Online,
	}
}

// cloudAgentUserSrv serves GET/PUT /api/v1/cloud-agents/srv. The signed-in
// user picks between the public maclawsrv and one of their own node srvs.
func cloudAgentUserSrv(reg *edgenode.Registry, broker *edgenode.Broker, bots *botmgmt.Service, viewer cloudagent.ViewerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if viewer == nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		principal, err := viewer(r)
		if err != nil || principal == nil || strings.TrimSpace(principal.UserID) == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		tenantID := strings.TrimSpace(principal.TenantID)
		userID := strings.TrimSpace(principal.UserID)
		// GET and PUT answer the same payload so a save never blanks the
		// client's node list between refreshes.
		payload := func() map[string]any {
			nodes := []map[string]any{}
			if reg != nil {
				if listed, err := reg.List(r.Context(), tenantID); err == nil {
					for _, node := range listed {
						if node.OwnerUserID != userID || !node.HasSrv {
							continue
						}
						if broker != nil {
							broker.ApplyLive(&node)
						}
						nodes = append(nodes, srvNodeView(node))
					}
				}
			}
			choice := ""
			if bots != nil {
				choice = bots.OwnerCloudNodeID(r.Context(), tenantID, userID)
			}
			return map[string]any{"node_id": choice, "nodes": nodes}
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, payload())
		case http.MethodPut:
			var in struct {
				NodeID string `json:"node_id"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID", "请求无效")
				return
			}
			nodeID := strings.TrimSpace(in.NodeID)
			if nodeID != "" {
				if reg == nil {
					writeError(w, http.StatusNotFound, "EDGE_NOT_FOUND", "edge node was not found")
					return
				}
				node, err := reg.Lookup(r.Context(), nodeID)
				if err != nil || node.TenantID != tenantID || node.OwnerUserID != userID || !node.HasSrv {
					writeError(w, http.StatusNotFound, "EDGE_NOT_FOUND", "edge node was not found")
					return
				}
			}
			if bots == nil {
				writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "云端管理暂时不可用")
				return
			}
			if err := bots.PinOwnerCloudNode(r.Context(), tenantID, userID, nodeID); err != nil {
				writeError(w, http.StatusBadGateway, "EDGE_NODES", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, payload())
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed")
		}
	}
}

// cloudAgentAdminSrvs serves GET /api/admin/cloud-agents/srvs: every
// maclawsrv a scope pin can name — the public system default, each
// registered srv link, and each node srv. The cloud-agent admin page loads
// it for its assignment pickers.
func cloudAgentAdminSrvs(reg *edgenode.Registry, broker *edgenode.Broker, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := AdminTenantID(r.Context())
		items := []map[string]any{}
		if bots != nil {
			if view, err := bots.View(r.Context(), tenantID); err == nil {
				items = append(items, map[string]any{
					"id":         "",
					"kind":       "public",
					"name":       "系统默认",
					"base_url":   view.BaseURL,
					"configured": view.AdminSecretSet,
				})
			}
			for _, link := range bots.SrvLinks(r.Context(), tenantID) {
				items = append(items, map[string]any{
					"id":         link.ID,
					"kind":       "registered",
					"name":       link.Name,
					"base_url":   link.BaseURL,
					"configured": link.AdminSecretSet,
				})
			}
		}
		if reg != nil {
			if nodes, err := reg.List(r.Context(), tenantID); err == nil {
				for _, node := range nodes {
					if !node.HasSrv {
						continue
					}
					if broker != nil {
						broker.ApplyLive(&node)
					}
					view := srvNodeView(node)
					view["id"] = view["node_id"]
					view["kind"] = "node"
					view["configured"] = true
					items = append(items, view)
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// cloudAgentAdminSrvBindings serves GET /api/admin/cloud-agents/srv-bindings:
// every user whose cloud agents are pinned to a private node srv. Users not
// listed run on the public maclawsrv.
func cloudAgentAdminSrvBindings(reg *edgenode.Registry, broker *edgenode.Broker, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items := []map[string]any{}
		if bots != nil {
			for userID, nodeID := range bots.CloudNodeBindings(r.Context(), AdminTenantID(r.Context())) {
				item := map[string]any{"user_id": userID, "node_id": nodeID, "scope": "private"}
				if reg != nil {
					if node, err := reg.Lookup(r.Context(), nodeID); err == nil {
						if broker != nil {
							broker.ApplyLive(&node)
						}
						for key, value := range srvNodeView(node) {
							item[key] = value
						}
					}
				}
				items = append(items, item)
			}
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i]["user_id"].(string) < items[j]["user_id"].(string)
		})
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
