package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/edgenode"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// GetEdgeNodesPlatformHandler GET /api/admin/platform/edge-nodes
// Global admins see every tenant's nodes as metadata. Secrets stay in the
// tenant record and are not copied here.
func GetEdgeNodesPlatformHandler(reg *edgenode.Registry, broker *edgenode.Broker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		nodes, err := reg.ListAll(r.Context())
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

// GetEdgePolicyPlatformHandler GET /api/admin/platform/tenants/{tenantId}/edge-policy
func GetEdgePolicyPlatformHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		policy, err := reg.Policy(r.Context(), r.PathValue("tenantId"))
		if err != nil {
			writeError(w, http.StatusBadGateway, "EDGE_NODES", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}

// PutEdgePolicyPlatformHandler PUT /api/admin/platform/tenants/{tenantId}/edge-policy
func PutEdgePolicyPlatformHandler(reg *edgenode.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reg == nil || !edgenode.Enabled() {
			writeError(w, http.StatusNotFound, "EDGE_DISABLED", "edge nodes are disabled")
			return
		}
		var in edgenode.Policy
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "invalid edge policy")
			return
		}
		tenantID := store.NormalizeTenantID(strings.TrimSpace(r.PathValue("tenantId")))
		if tenantID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", "tenant is required")
			return
		}
		if err := reg.SetPolicy(r.Context(), tenantID, in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BOT_SETTINGS", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, in)
	}
}
