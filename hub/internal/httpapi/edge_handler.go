package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/localrelay"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
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
				broker.ApplyLive(&nodes[i])
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

// DeleteEdgeNodeAdminHandler DELETE /api/admin/bots/nodes/{id}
func DeleteEdgeNodeAdminHandler(reg *edgenode.Registry, broker *edgenode.Broker, pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
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
		if bots != nil {
			_ = bots.ForgetNodeSrv(r.Context(), node.TenantID, node.ID)
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
		issuer := ""
		if admin := AdminFromContext(r.Context()); admin != nil {
			issuer = strings.TrimSpace(admin.Username)
			if issuer == "" {
				issuer = strings.TrimSpace(admin.DisplayName)
			}
			if issuer == "" {
				issuer = strings.TrimSpace(admin.ID)
			}
		}
		code, err := reg.IssueCodeBy(r.Context(), botTenantID(r), in.Name, issuer)
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

// PostEdgeNodeSettingsAdminHandler POST /api/admin/bots/nodes/{id}/settings
// patches a registered node's options: name renames it and owner_user_id is
// the account that has the desktops to itself ("" turns the node shared).
// Both are applied to the node and to its desktop services so resolution
// and the admin lists stay consistent. On an exclusive change the srv pins
// follow too: the previous owner's pin to this node is dropped and the new
// owner is pinned, so their next bots land on this node's srv.
func PostEdgeNodeSettingsAdminHandler(reg *edgenode.Registry, pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		var in struct {
			Name        string `json:"name"`
			OwnerUserID string `json:"owner_user_id"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid bot settings")
			return
		}
		nodeID := strings.TrimSpace(r.PathValue("id"))
		node, err := reg.Lookup(r.Context(), nodeID)
		if err != nil || node.TenantID != botTenantID(r) {
			writeError(w, http.StatusNotFound, "EDGE_NOT_FOUND", "edge node was not found")
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		in.OwnerUserID = strings.TrimSpace(in.OwnerUserID)
		if in.Name == "" || len([]rune(in.Name)) > 80 {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "node name is required and must stay under 80 characters")
			return
		}
		if in.OwnerUserID != "" && !desktop.ValidUserID(in.OwnerUserID) {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "owner_user_id is invalid")
			return
		}
		prevOwner := strings.TrimSpace(node.OwnerUserID)
		prevName := node.Name
		if err := reg.UpdateNode(r.Context(), node.ID, in.Name, &in.OwnerUserID); err != nil {
			writeError(w, http.StatusBadGateway, "EDGE_NODES", err.Error())
			return
		}
		revertOwner := func() {
			_ = reg.UpdateNode(r.Context(), node.ID, prevName, &prevOwner)
			if pool != nil {
				_ = pool.SetNodeServersOwner(r.Context(), node.TenantID, node.ID, prevOwner)
			}
		}
		if pool != nil {
			if err := pool.SetNodeServersOwner(r.Context(), node.TenantID, node.ID, in.OwnerUserID); err != nil {
				_ = reg.UpdateNode(r.Context(), node.ID, prevName, &prevOwner)
				writeDesktopError(w, err)
				return
			}
		}
		if bots != nil && node.HasSrv && in.OwnerUserID != "" && in.OwnerUserID != prevOwner {
			// Pin the new owner before dropping anyone else. A failed pin
			// puts the previous owner back, so bots do not lose the node
			// while the new account never receives it. A desktop without
			// an srv link still changes owner.
			if err := bots.PinOwnerNode(r.Context(), node.TenantID, in.OwnerUserID, node.ID); err != nil && !errors.Is(err, botmgmt.ErrSrv) {
				revertOwner()
				writeError(w, http.StatusBadGateway, "NODE_PIN", "the user was not pinned to this node's srv")
				return
			}
			// An exclusive desktop strands every other user's pin, not only
			// the previous owner's (a shared node may have pinned several).
			_ = bots.UnpinNodeExcept(r.Context(), node.TenantID, node.ID, in.OwnerUserID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": node.ID})
	}
}

// PostEdgeRotateCodeAdminHandler POST /api/admin/bots/nodes/{id}/code
// The code rotates that node's secret. It does not register another node.
func PostEdgeRotateCodeAdminHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		code, err := reg.IssueRotateCode(r.Context(), botTenantID(r), r.PathValue("id"))
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, edgenode.ErrNotFound) || errors.Is(err, edgenode.ErrTenant) {
				status = http.StatusNotFound
			}
			writeError(w, status, "EDGE_ENROLL", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"code":    code.Code,
			"node_id": code.NodeID,
			"until":   code.Until,
		})
	}
}

// refreshRotatedDesktop writes the new desktopd key onto the server this node
// already has. A missing server is created. A second server is not.
func refreshRotatedDesktop(ctx context.Context, pool *desktoppool.Pool, tenantID, nodeID, name, ownerUserID, key string, ramBytes uint64, logicalCPUs int) error {
	if pool == nil {
		return errors.New("desktop service store is unavailable")
	}
	view, err := pool.View(ctx, tenantID)
	if err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	for _, server := range view.Servers {
		if server.NodeID != nodeID {
			continue
		}
		_, err = pool.UpdateServer(ctx, tenantID, server.ID, desktoppool.Server{
			Name: server.Name, BaseURL: server.BaseURL, AccessToken: key,
			Image: server.Image, Memory: server.Memory, CPUs: server.CPUs, ShmSize: server.ShmSize,
			NodeID: server.NodeID, OwnerUserID: server.OwnerID, Intranet: server.Intranet,
			ResourcesAuto: server.ResourcesAuto, ResourcesPinned: server.ResourcesPinned,
		}, true)
		return err
	}
	if strings.TrimSpace(name) == "" {
		name = nodeID
	}
	ownerUserID = strings.TrimSpace(ownerUserID)
	server, err := desktoppool.PrepareOwnedResources(desktoppool.Server{
		Name: name, BaseURL: desktoppool.NodeBaseURL(nodeID), AccessToken: key, NodeID: nodeID,
	}, ramBytes, logicalCPUs)
	if err != nil {
		return err
	}
	if ownerUserID != "" {
		_, err = pool.AttachOwnedServer(ctx, tenantID, ownerUserID, server)
		return err
	}
	_, err = pool.CreateServer(ctx, tenantID, server)
	return err
}

// ensureEdgeDesktopRecorded repairs the pool row of a node whose enrollment
// succeeded but whose desktop service record was lost (or predates the pool).
// The node already proved this desktopd key at enrollment, so a reconnect may
// restore the record without asking the user to install again.
func ensureEdgeDesktopRecorded(ctx context.Context, pool *desktoppool.Pool, node edgenode.Node) error {
	if pool == nil || !node.HasDesktopd || strings.TrimSpace(node.DesktopdKey) == "" {
		return nil
	}
	if err := refreshRotatedDesktop(ctx, pool, node.TenantID, node.ID, node.Name, node.OwnerUserID, node.DesktopdKey, 0, 0); err != nil {
		return err
	}
	if strings.TrimSpace(node.OwnerUserID) != "" {
		return pool.SetNodeServersOwner(ctx, node.TenantID, node.ID, node.OwnerUserID)
	}
	return nil
}

// abortEdgeEnroll undoes a registration whose srv connection was not stored.
// The code is already spent. A new node is removed with the desktop service
// it just created. A rotation is put back, including the previous desktop key.
func abortEdgeEnroll(ctx context.Context, reg *edgenode.Registry, pool *desktoppool.Pool, bots *botmgmt.Service, out edgenode.EnrollResult, hadDesktop bool) {
	if out.Rotated {
		_ = reg.Restore(ctx, out.Previous)
		if hadDesktop && strings.TrimSpace(out.Previous.DesktopdKey) != "" {
			_ = refreshRotatedDesktop(ctx, pool, out.TenantID, out.NodeID, out.Previous.Name, out.Previous.OwnerUserID, out.Previous.DesktopdKey, 0, 0)
		}
		return
	}
	if bots != nil {
		_ = bots.ForgetNodeSrv(ctx, out.TenantID, out.NodeID)
	}
	if hadDesktop && pool != nil {
		view, err := pool.View(ctx, out.TenantID)
		if err == nil {
			for _, server := range view.Servers {
				if server.NodeID == out.NodeID {
					_ = pool.DeleteServer(ctx, out.TenantID, server.ID)
				}
			}
		}
	}
	_ = reg.Remove(ctx, out.NodeID)
}

// PostEdgeEnrollHandler POST /api/v1/edge/enroll
func PostEdgeEnrollHandler(reg *edgenode.Registry, pool *desktoppool.Pool, bots *botmgmt.Service) http.HandlerFunc {
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
			HostRAMBytes uint64 `json:"host_ram_bytes"`
			HostCPUs     int    `json:"host_cpus"`
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
		if out.Rotated && in.HasDesktopd {
			if err := refreshRotatedDesktop(r.Context(), pool, out.TenantID, out.NodeID, out.Previous.Name, out.Previous.OwnerUserID, in.DesktopdKey, in.HostRAMBytes, in.HostCPUs); err != nil {
				_ = reg.Restore(r.Context(), out.Previous)
				msg := "desktop service was not recorded; issue a new code"
				if errors.Is(err, desktop.ErrHostTooSmall) {
					msg = err.Error()
				}
				writeError(w, http.StatusBadGateway, "EDGE_ENROLL", msg)
				return
			}
		} else if in.HasDesktopd {
			node, authErr := reg.Authenticate(r.Context(), out.NodeID, out.NodeSecret)
			var createErr error
			switch {
			case authErr != nil:
				createErr = authErr
			case node.OwnerUserID != "":
				createErr = attachOwnedNode(r.Context(), pool, node, in.DesktopdKey, in.HostRAMBytes, in.HostCPUs)
			default:
				server, fitErr := desktoppool.PrepareOwnedResources(desktoppool.Server{
					Name:        node.Name,
					BaseURL:     desktoppool.NodeBaseURL(node.ID),
					AccessToken: strings.TrimSpace(in.DesktopdKey),
					NodeID:      node.ID,
				}, in.HostRAMBytes, in.HostCPUs)
				if fitErr != nil {
					createErr = fitErr
				} else {
					_, createErr = pool.CreateServer(r.Context(), node.TenantID, server)
				}
			}
			if createErr != nil {
				_ = reg.Remove(r.Context(), out.NodeID)
				msg := "desktop service was not recorded; issue a new code"
				if errors.Is(createErr, desktop.ErrHostTooSmall) {
					msg = createErr.Error()
				}
				writeError(w, http.StatusBadGateway, "EDGE_ENROLL", msg)
				return
			}
		}
		if strings.TrimSpace(in.DirectBase) != "" {
			_ = reg.SetDirectBase(r.Context(), out.NodeID, in.DirectBase)
		}
		if in.HasSrv {
			// The node said it has a srv. Leaving it registered without that
			// connection would accept bots onto a machine Hub cannot reach.
			node, lookupErr := reg.Lookup(r.Context(), out.NodeID)
			attachErr := lookupErr
			if attachErr == nil {
				if bots == nil {
					attachErr = errors.New("srv connection was not recorded")
				} else {
					attachErr = bots.AttachNodeSrv(r.Context(), node.TenantID, out.NodeID, "http://srv."+out.NodeID+".node.invalid", in.SrvAdmin)
				}
			}
			// Only a node that is both srv and desktop can be the default home
			// of the owner's new bots. A srv-only node remains usable for cloud
			// agents, but pinning it here would make every new desktop bot a
			// computerless one while still advertising a computer.
			if attachErr == nil && bots != nil && in.HasDesktopd && strings.TrimSpace(node.OwnerUserID) != "" {
				attachErr = bots.PinOwnerNode(r.Context(), node.TenantID, node.OwnerUserID, out.NodeID)
			}
			if attachErr != nil {
				abortEdgeEnroll(r.Context(), reg, pool, bots, out, in.HasDesktopd)
				writeError(w, http.StatusBadGateway, "EDGE_ENROLL", "srv connection was not recorded; issue a new code")
				return
			}
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

// GetEdgeLinkHandler GET /api/v1/edge/link
// The node authenticates with its node secret, not the desktop API token.
func GetEdgeLinkHandler(reg *edgenode.Registry, broker *edgenode.Broker, bots *botmgmt.Service, pool *desktoppool.Pool) http.HandlerFunc {
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
		if err := ensureEdgeDesktopRecorded(r.Context(), pool, node); err != nil {
			log.Printf("[edge] restore desktop record node=%s: %v", node.ID, err)
		}
		ws, err := edgeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if node.HasSrv && bots != nil {
			tenantID, nodeID := node.TenantID, node.ID
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_ = bots.ProvisionNodeConnection(ctx, tenantID, nodeID)
			}()
		}
		_ = broker.Attach(r.Context(), node, localrelay.NewWSConn(ws))
	}
}

// PostEdgeCapacityHandler POST /api/v1/edge/capacity
// The node reports the machine it is running on. An installed desktop is
// sized from that. A cloud desktop is left on the size set for it.
func PostEdgeCapacityHandler(pool *desktoppool.Pool, reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || pool == nil || !edgenode.Enabled() {
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
		var in struct {
			RAMBytes uint64 `json:"ram_bytes"`
			CPUs     int    `json:"cpus"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_CAPACITY", "invalid host capacity")
			return
		}
		if err := pool.FitNodeResources(r.Context(), node.TenantID, node.ID, in.RAMBytes, in.CPUs); err != nil {
			if errors.Is(err, desktop.ErrHostTooSmall) {
				writeError(w, http.StatusBadRequest, "EDGE_CAPACITY", err.Error())
				return
			}
			writeError(w, http.StatusBadGateway, "EDGE_CAPACITY", "desktop size was not stored")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// PostEdgeResourcesHandler POST /api/v1/edge/resources
// An operator typed a desktop size on this machine. It replaces the size
// chosen at install, and later measurements leave it in place.
func PostEdgeResourcesHandler(pool *desktoppool.Pool, reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || pool == nil || !edgenode.Enabled() {
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
		var in struct {
			Memory      string `json:"memory"`
			CPUs        string `json:"cpus"`
			ShmSize     string `json:"shm_size"`
			RestoreAuto bool   `json:"restore_auto"`
			RAMBytes    uint64 `json:"ram_bytes"`
			HostCPUs    int    `json:"host_cpus"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_RESOURCES", "invalid desktop size")
			return
		}
		if in.RestoreAuto {
			if in.RAMBytes > 0 {
				if _, err := desktop.FitHostResources(in.RAMBytes, in.HostCPUs); err != nil {
					if errors.Is(err, desktop.ErrHostTooSmall) {
						writeError(w, http.StatusBadRequest, "EDGE_RESOURCES", err.Error())
						return
					}
					writeError(w, http.StatusBadGateway, "EDGE_RESOURCES", "desktop size was not stored")
					return
				}
			}
			if err := pool.AllowNodeRefit(r.Context(), node.TenantID, node.ID); err != nil {
				writeError(w, http.StatusBadGateway, "EDGE_RESOURCES", "desktop size was not stored")
				return
			}
			if in.RAMBytes > 0 {
				if err := pool.FitNodeResources(r.Context(), node.TenantID, node.ID, in.RAMBytes, in.HostCPUs); err != nil {
					writeError(w, http.StatusBadGateway, "EDGE_RESOURCES", "desktop size was not stored")
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if err := pool.SetNodeResources(r.Context(), node.TenantID, node.ID, in.Memory, in.CPUs, in.ShmSize); err != nil {
			if errors.Is(err, desktoppool.ErrDesktopNotRecorded) {
				writeError(w, http.StatusConflict, "EDGE_RESOURCES", "desktop was not recorded")
				return
			}
			if errors.Is(err, desktop.ErrInvalid) {
				writeError(w, http.StatusBadRequest, "EDGE_RESOURCES", "desktop size is invalid")
				return
			}
			writeError(w, http.StatusBadGateway, "EDGE_RESOURCES", "desktop size was not stored")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// PostEdgeDirectBaseHandler POST /api/v1/edge/direct-base
// The node replaces the intranet address a GUI probes. An empty address
// clears it. This does not issue a new node secret.
func PostEdgeDirectBaseHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
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
		var in struct {
			DirectBase string `json:"direct_base"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_DIRECT", "direct base must be an http url")
			return
		}
		if err := reg.SetDirectBase(r.Context(), node.ID, in.DirectBase); err != nil {
			writeError(w, http.StatusBadRequest, "EDGE_DIRECT", "direct base must be an http url")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
