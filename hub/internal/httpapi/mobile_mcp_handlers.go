package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MobileAgentMCPHandler lists or replaces remote MCP servers for the signed-in
// mobile user. Config is stored in agentservice UserConfig (same as MaClawSrv).
//
//	GET  /api/mobile/agent/mcp
//	PUT  /api/mobile/agent/mcp
func MobileAgentMCPHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		// EnsurePrincipal and MCP config updates write the full-agent runtime.
		// Hold the lifecycle lock through them so unbind cannot delete the user
		// halfway through and have this request recreate it afterwards.
		mobileKnowledgePurgeState.RLock()
		defer mobileKnowledgePurgeState.RUnlock()
		if !mobileOwnerWriteAllowedLocked(principal.TenantID, mobilePrincipalOwnerID(principal)) {
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

		switch r.Method {
		case http.MethodGet:
			cfg, err := svc.GetUserConfig(r.Context(), p)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CONFIG_LOAD_FAILED", "failed to load agent config")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"mcp_servers":       mobileMCPServersPublic(cfg.AppConfig.MCPServers),
				"local_mcp_servers": mobileLocalMCPServersPublic(cfg.AppConfig.LocalMCPServers),
				// Local stdio MCP on multi-tenant Hub is high-risk; exposed for
				// visibility but mobile clients should prefer remote MCP.
				"local_mcp_allowed": false,
			})
		case http.MethodPut:
			var req struct {
				MCPServers []corelib.MCPServerEntry `json:"mcp_servers"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			if err := mobileValidateMCPServers(req.MCPServers); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_MCP", err.Error())
				return
			}
			current, err := svc.GetUserConfig(r.Context(), p)
			if err != nil {
				// EnsurePrincipal should have created config; fall back to empty.
				current = &agentservice.UserConfig{TenantID: p.TenantID, UserID: p.UserID, AppConfig: corelib.AppConfig{}}
			}
			next := current.AppConfig
			next.MCPServers = mobileNormalizeMCPServers(req.MCPServers)
			// Do not accept local MCP mutation from mobile (host security).
			// Preserve existing LocalMCPServers from server-side config only.
			updated, err := svc.UpdateUserConfig(r.Context(), p, next)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "CONFIG_SAVE_FAILED", "failed to save MCP config")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status":      "saved",
				"mcp_servers": mobileMCPServersPublic(updated.AppConfig.MCPServers),
			})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
		}
	}
}

func mobileAgentPrincipal(principal *auth.ViewerPrincipal) agentservice.Principal {
	tenantID := "default"
	userID := ""
	if principal != nil {
		if strings.TrimSpace(principal.TenantID) != "" {
			tenantID = strings.TrimSpace(principal.TenantID)
		}
		userID = strings.TrimSpace(principal.UserID)
		if userID == "" {
			userID = strings.TrimSpace(principal.Email)
		}
	}
	return agentservice.Principal{TenantID: tenantID, UserID: userID, Roles: []string{"mobile"}}
}

func mobileValidateMCPServers(servers []corelib.MCPServerEntry) error {
	if len(servers) > 32 {
		return errString("at most 32 MCP servers allowed")
	}
	seen := map[string]struct{}{}
	for _, s := range servers {
		id := strings.TrimSpace(s.ID)
		if id == "" {
			return errString("mcp server id is required")
		}
		if _, ok := seen[id]; ok {
			return errString("duplicate mcp server id: " + id)
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(s.EndpointURL) == "" {
			return errString("mcp server endpoint_url is required for " + id)
		}
		ep := strings.ToLower(strings.TrimSpace(s.EndpointURL))
		if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
			return errString("mcp endpoint must be http(s) for " + id)
		}
	}
	return nil
}

func mobileNormalizeMCPServers(in []corelib.MCPServerEntry) []corelib.MCPServerEntry {
	if len(in) == 0 {
		return []corelib.MCPServerEntry{}
	}
	out := make([]corelib.MCPServerEntry, 0, len(in))
	for _, s := range in {
		s.ID = strings.TrimSpace(s.ID)
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			s.Name = s.ID
		}
		s.EndpointURL = strings.TrimSpace(s.EndpointURL)
		s.AuthType = strings.TrimSpace(s.AuthType)
		if s.AuthType == "" {
			s.AuthType = "none"
		}
		out = append(out, s)
	}
	return out
}

func mobileMCPServersPublic(servers []corelib.MCPServerEntry) []map[string]any {
	out := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		secret := strings.TrimSpace(s.AuthSecret)
		item := map[string]any{
			"id":           s.ID,
			"name":         s.Name,
			"endpoint_url": s.EndpointURL,
			"auth_type":    s.AuthType,
			// Never echo secrets; only whether configured (masked or present).
			"has_auth_secret": secret != "",
		}
		out = append(out, item)
	}
	return out
}

func mobileLocalMCPServersPublic(servers []corelib.LocalMCPServerEntry) []map[string]any {
	out := make([]map[string]any, 0, len(servers))
	for _, s := range servers {
		out = append(out, map[string]any{
			"id":       s.ID,
			"name":     s.Name,
			"command":  s.Command,
			"disabled": s.Disabled,
		})
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }
