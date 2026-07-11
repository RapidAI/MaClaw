package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileAgentMCPHealthHandler probes configured remote MCP servers via the
// shared agentservice readiness path (same as agent tool discovery).
//
//	POST /api/mobile/agent/mcp/health
func MobileAgentMCPHealthHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		_, svc, err := mobileEnsureCoreAgent()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "AGENT_UNAVAILABLE", "mobile agent runtime is unavailable")
			return
		}
		p := mobileAgentPrincipal(principal)
		if err := svc.EnsurePrincipal(r.Context(), p, principal.Email, p.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "AGENT_PRINCIPAL", "failed to ensure agent principal")
			return
		}

		// Probe through the same bridge the agent uses (EnsureReady + tool list).
		toolCountByServer := map[string]int{}
		if mobileMCPBridge != nil {
			tools := mobileMCPBridge.ListAvailableTools(r.Context(), p)
			for _, t := range tools {
				toolCountByServer[t.ServerID]++
			}
		}

		views, err := svc.ListMCPServers(r.Context(), p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "MCP_LIST_FAILED", "failed to list MCP servers")
			return
		}

		servers := make([]map[string]any, 0, len(views))
		healthy := 0
		for _, v := range views {
			// Mobile only cares about remote MCP for health UX.
			if strings.EqualFold(v.Kind, "local") {
				continue
			}
			status := strings.TrimSpace(string(v.HealthStatus))
			if status == "" {
				status = "unknown"
			}
			if status == "healthy" || status == "slow" {
				healthy++
			}
			item := map[string]any{
				"id":            v.ID,
				"name":          v.Name,
				"kind":          v.Kind,
				"endpoint_url":  v.EndpointURL,
				"health_status": status,
				"running":       v.Running,
				"tool_count":    len(v.Tools),
				"fail_count":    v.FailCount,
			}
			if n, ok := toolCountByServer[v.ID]; ok && n > len(v.Tools) {
				item["tool_count"] = n
			}
			if v.LastCheckAt != nil {
				item["last_check_at"] = v.LastCheckAt.UTC().Format(time.RFC3339)
			}
			servers = append(servers, item)
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":          "ok",
			"probed_at":       time.Now().UTC().Format(time.RFC3339),
			"server_count":    len(servers),
			"healthy_count":   healthy,
			"available_tools": sumToolCounts(servers),
			"servers":         servers,
		})
	}
}

func sumToolCounts(servers []map[string]any) int {
	total := 0
	for _, s := range servers {
		switch n := s["tool_count"].(type) {
		case int:
			total += n
		case float64:
			total += int(n)
		}
	}
	return total
}

