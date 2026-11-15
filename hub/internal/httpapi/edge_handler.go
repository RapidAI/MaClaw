package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/gorilla/websocket"
)

// edgeRegistry and edgeBroker are set by the router when HUB_EDGE_NODES is
// on. Nil means the feature is off and authorizeDesktopAPI ignores node tokens.
var (
	edgeRegistry *edgenode.Registry
	edgeBroker   *edgenode.Broker
)

var edgeUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  64 << 10,
	WriteBufferSize: 64 << 10,
}

// GetEdgeNodesAdminHandler GET /api/admin/bots/nodes
func GetEdgeNodesAdminHandler(reg *edgenode.Registry, broker *edgenode.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		nodes, err := reg.List(r.Context(), botTenantID(r))
		if err != nil {
			writeError(w, http.StatusBadGateway, "EDGE_NODES", err.Error())
			return
		}
		for i := range nodes {
			if broker != nil {
				nodes[i].Online = broker.Online(nodes[i].ID)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

// DeleteEdgeNodeAdminHandler DELETE /api/admin/bots/nodes/{id}
func DeleteEdgeNodeAdminHandler(reg *edgenode.Registry, broker *edgenode.Broker, pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		nodeID := strings.TrimSpace(r.PathValue("id"))
		node, err := reg.Lookup(r.Context(), nodeID)
		if err != nil || node.TenantID != botTenantID(r) {
			writeError(w, http.StatusNotFound, "EDGE_NOT_FOUND", "edge node was not found")
			return
		}
		if err := detachEdgeServers(r.Context(), pool, node.TenantID, node.ID); err != nil {
			writeError(w, http.StatusConflict, "EDGE_IN_USE", err.Error())
			return
		}
		if err := reg.Remove(r.Context(), node.ID); err != nil {
			writeError(w, http.StatusBadGateway, "EDGE_NODES", err.Error())
			return
		}
		if broker != nil {
			broker.Drop(node.ID)
		}
		dropCDPTickets(node.ID)
		writeJSON(w, http.StatusOK, map[string]any{"deleted": node.ID})
	}
}

func detachEdgeServers(ctx context.Context, pool *desktoppool.Pool, tenantID, nodeID string) error {
	if pool == nil || strings.TrimSpace(nodeID) == "" {
		return nil
	}
	view, err := pool.ViewAuthorized(ctx, tenantID, nil)
	if err != nil {
		return err
	}
	assigned := map[string]bool{}
	for _, item := range view.Assignments {
		assigned[item.ServerID] = true
	}
	var ids []string
	for _, server := range view.Servers {
		if server.NodeID != nodeID {
			continue
		}
		if assigned[server.ID] {
			return errors.New("cancel the assignment before deleting this node")
		}
		ids = append(ids, server.ID)
	}
	for _, id := range ids {
		if err := pool.DeleteServer(ctx, tenantID, id); err != nil {
			return err
		}
	}
	return nil
}

// PostEdgeCodeAdminHandler POST /api/admin/bots/nodes/codes
func PostEdgeCodeAdminHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		code, err := reg.IssueCode(r.Context(), botTenantID(r), in.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"code":    code.Code,
			"node_id": code.NodeID,
			"until":   code.Until,
		})
	}
}

// PostEdgeEnrollHandler POST /api/v1/edge/enroll
func PostEdgeEnrollHandler(reg *edgenode.Registry, pool *desktoppool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		var in struct {
			Code         string `json:"code"`
			DesktopdKey  string `json:"desktopd_key"`
			SrvAdmin     string `json:"srv_admin_secret"`
			Version      string `json:"version"`
			HasDesktopd  bool   `json:"has_desktopd"`
			HasSrv       bool   `json:"has_srv"`
			AdvertiseURL string `json:"advertise_url"`
			DirectBase   string `json:"direct_base"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid enrollment")
			return
		}
		if in.HasDesktopd && strings.TrimSpace(in.DesktopdKey) == "" {
			writeError(w, http.StatusBadRequest, "EDGE_ENROLL", "desktopd key is required")
			return
		}
		if in.HasDesktopd && pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		if _, err := edgenode.ParseDirectBase(in.DirectBase); err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_ENROLL", err.Error())
			return
		}
		out, err := reg.Enroll(r.Context(), in.Code, in.DesktopdKey, in.SrvAdmin, in.Version, in.HasDesktopd, in.HasSrv, in.AdvertiseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_ENROLL", err.Error())
			return
		}
		if in.HasDesktopd {
			node, authErr := reg.Authenticate(r.Context(), out.NodeID, out.NodeSecret)
			var createErr error
			switch {
			case authErr != nil:
				createErr = authErr
			case node.OwnerUserID != "":
				createErr = attachOwnedNode(r.Context(), pool, node, in.DesktopdKey)
			default:
				_, createErr = pool.CreateServer(r.Context(), node.TenantID, desktoppool.Server{
					Name:        node.Name,
					BaseURL:     desktoppool.NodeBaseURL(node.ID),
					AccessToken: strings.TrimSpace(in.DesktopdKey),
					NodeID:      node.ID,
				})
			}
			if createErr != nil {
				_ = reg.Remove(r.Context(), out.NodeID)
				writeError(w, http.StatusBadGateway, "EDGE_ENROLL", "desktop service was not recorded; issue a new code")
				return
			}
		}
		if strings.TrimSpace(in.DirectBase) != "" {
			_ = reg.SetDirectBase(r.Context(), out.NodeID, in.DirectBase)
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

// GetEdgeLinkHandler GET /api/v1/edge/link
// The node authenticates with its node secret, not the desktop API token.
func GetEdgeLinkHandler(reg *edgenode.Registry, broker *edgenode.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || broker == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		nodeID := strings.TrimSpace(r.Header.Get("X-Node-Id"))
		secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		node, err := reg.Authenticate(r.Context(), nodeID, strings.TrimSpace(secret))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "EDGE_AUTH", "node secret is required")
			return
		}
		ws, err := edgeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = broker.Attach(r.Context(), node, localrelay.NewWSConn(ws))
	}
}
