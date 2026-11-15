package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
)

// edgeSelfDesktopEnabled is HUB_EDGE_NODES plus HUB_EDGE_SELF_DESKTOP not
// "off": users may register their own intranet desktopd from the GUI.
func edgeSelfDesktopEnabled() bool {
	if !edgenode.Enabled() {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("HUB_EDGE_SELF_DESKTOP")), "off")
}

// selfDesktopNode is one desktopd the caller registered. AccessToken is that
// desktopd's own API token: the machine is the caller's, so it is shown to
// them (and only them) for backup.
type selfDesktopNode struct {
	ServerID    string `json:"server_id"`
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	Online      bool   `json:"online"`
	Active      bool   `json:"active"`
	AccessToken string `json:"access_token,omitempty"`
}

type selfDesktopView struct {
	Enabled bool              `json:"enabled"`
	Nodes   []selfDesktopNode `json:"nodes"`
}

func selfDesktopList(ctx context.Context, pool *desktoppool.Pool, broker *edgenode.Broker, tenantID, userID string) (selfDesktopView, error) {
	servers, active, err := pool.OwnedServers(ctx, tenantID, userID)
	if err != nil {
		return selfDesktopView{}, err
	}
	view := selfDesktopView{Enabled: true, Nodes: []selfDesktopNode{}}
	for _, server := range servers {
		view.Nodes = append(view.Nodes, selfDesktopNode{
			ServerID:    server.ID,
			NodeID:      server.NodeID,
			Name:        server.Name,
			Online:      broker.Online(server.NodeID),
			Active:      server.ID == active,
			AccessToken: server.AccessToken,
		})
	}
	return view, nil
}

// GetSelfDesktopNodesHandler GET /api/v1/bots/desktop-node
func GetSelfDesktopNodesHandler(pool *desktoppool.Pool, reg *edgenode.Registry, broker *edgenode.Broker, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if pool == nil || reg == nil || !edgeSelfDesktopEnabled() {
			writeJSON(w, http.StatusOK, selfDesktopView{Nodes: []selfDesktopNode{}})
			return
		}
		view, err := selfDesktopList(r.Context(), pool, broker, principal.TenantID, principal.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// PostSelfDesktopCodeHandler POST /api/v1/bots/desktop-node/code {"name"}
// issues a one-time code that registers a desktopd for the caller only.
func PostSelfDesktopCodeHandler(reg *edgenode.Registry, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if reg == nil || !edgeSelfDesktopEnabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "desktop registration is disabled on this hub")
			return
		}
		var in struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid desktop registration")
			return
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "My desktop"
		}
		code, err := reg.IssueOwnedCode(r.Context(), principal.TenantID, principal.UserID, name)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"code": code.Code, "node_id": code.NodeID, "until": code.Until})
	}
}

// PutSelfDesktopActiveHandler PUT /api/v1/bots/desktop-node/{server_id}/active
func PutSelfDesktopActiveHandler(pool *desktoppool.Pool, reg *edgenode.Registry, broker *edgenode.Broker, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if pool == nil || reg == nil || !edgeSelfDesktopEnabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "desktop registration is disabled on this hub")
			return
		}
		before := pool.ResolvedServerID(r.Context(), principal.TenantID, principal.UserID)
		serverID := strings.TrimSpace(r.PathValue("server_id"))
		if err := pool.ActivateOwnedServer(r.Context(), principal.TenantID, principal.UserID, serverID); err != nil {
			writeDesktopError(w, err)
			return
		}
		if before != serverID {
			pool.StopUserOnLater(principal.TenantID, before, principal.UserID)
		}
		view, err := selfDesktopList(r.Context(), pool, broker, principal.TenantID, principal.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// DeleteSelfDesktopNodeHandler DELETE /api/v1/bots/desktop-node/{server_id}
func DeleteSelfDesktopNodeHandler(pool *desktoppool.Pool, reg *edgenode.Registry, broker *edgenode.Broker, identity botAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := botMachine(w, r, identity)
		if !ok {
			return
		}
		if pool == nil || reg == nil || !edgeSelfDesktopEnabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "desktop registration is disabled on this hub")
			return
		}
		nodeID, err := pool.DetachOwnedServer(r.Context(), principal.TenantID, principal.UserID, r.PathValue("server_id"))
		if err != nil && !errors.Is(err, desktoppool.ErrNotFound) {
			writeDesktopError(w, err)
			return
		}
		if nodeID != "" {
			retireEdgeNodes(r.Context(), reg, broker, principal.TenantID, []string{nodeID})
		}
		view, err := selfDesktopList(r.Context(), pool, broker, principal.TenantID, principal.UserID)
		if err != nil {
			writeDesktopError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// retireEdgeNodes removes nodes of this tenant and closes their links.
func retireEdgeNodes(ctx context.Context, reg *edgenode.Registry, broker *edgenode.Broker, tenantID string, nodes []string) {
	for _, id := range nodes {
		node, err := reg.Lookup(ctx, id)
		if err != nil || edgeTenantMismatch(node.TenantID, tenantID) || edgeTenantMismatch(tenantID, node.TenantID) {
			continue
		}
		_ = reg.Remove(ctx, id)
		if broker != nil {
			broker.Drop(id)
		}
	}
}

// attachOwnedNode records an enrolled personal node as one more desktop of
// its owner and makes it the active one. The owner's desktop on the service
// they used before is stopped in the background, as an assignment move does.
func attachOwnedNode(ctx context.Context, pool *desktoppool.Pool, node edgenode.Node, key string) error {
	before := pool.ResolvedServerID(ctx, node.TenantID, node.OwnerUserID)
	view, err := pool.AttachOwnedServer(ctx, node.TenantID, node.OwnerUserID, desktoppool.Server{
		Name:        node.Name,
		BaseURL:     desktoppool.NodeBaseURL(node.ID),
		AccessToken: strings.TrimSpace(key),
		NodeID:      node.ID,
	})
	if err != nil {
		return err
	}
	if before != view.ID {
		pool.StopUserOnLater(node.TenantID, before, node.OwnerUserID)
	}
	return nil
}
