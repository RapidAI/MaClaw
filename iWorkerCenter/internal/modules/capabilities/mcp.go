package capabilities

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// MCPServer represents an enterprise MCP server installed and governed by iWorkerCenter.
type MCPServer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ServerType   string   `json:"server_type"`
	Endpoint     string   `json:"endpoint"`
	Command      string   `json:"command,omitempty"`
	Args         []string `json:"args"`
	EnvKeys      []string `json:"env_keys"`
	DepartmentID string   `json:"department_id"`
	RiskLevel    string   `json:"risk_level"`
	Status       string   `json:"status"`
	InstalledAt  string   `json:"installed_at"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

type mcpServerPayload struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ServerType   string   `json:"server_type"`
	Endpoint     string   `json:"endpoint"`
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	EnvKeys      []string `json:"env_keys"`
	DepartmentID string   `json:"department_id"`
	RiskLevel    string   `json:"risk_level"`
	Status       string   `json:"status"`
}

func (h *Handler) handleAdminMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listAdminMCPServers(w, r)
	case http.MethodPost:
		h.createMCPServer(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAdminMCPServerByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/mcp-servers/"), "/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "mcp server id is required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getMCPServer(w, r, id)
	case http.MethodPut:
		h.updateMCPServer(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) handleClientMCPServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	departmentID := strings.TrimSpace(r.URL.Query().Get("department_id"))
	servers, err := h.listRuntimeMCPServers(r.Context(), tenant.RequestTenantID(r), departmentID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"mcp_servers": servers})
}

func (h *Handler) listAdminMCPServers(w http.ResponseWriter, r *http.Request) {
	servers, err := h.listMCPServers(r.Context(), tenant.RequestTenantID(r), "", false)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"mcp_servers": servers})
}

func (h *Handler) getMCPServer(w http.ResponseWriter, r *http.Request, id string) {
	server, err := h.mcpServerByID(r.Context(), tenant.RequestTenantID(r), id)
	if err == sql.ErrNoRows {
		response.NotFound(w, "NOT_FOUND", "mcp server not found")
		return
	}
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, server)
}

func (h *Handler) createMCPServer(w http.ResponseWriter, r *http.Request) {
	var req mcpServerPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	server, err := normalizeMCPServerPayload(req)
	if err != nil {
		response.BadRequest(w, "INVALID_MCP_SERVER", err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	server.ID = idgen.New("mcp")
	server.InstalledAt = now
	server.CreatedAt = now
	server.UpdatedAt = now
	argsJSON, _ := json.Marshal(server.Args)
	envKeysJSON, _ := json.Marshal(server.EnvKeys)
	_, err = h.write.ExecContext(r.Context(), `INSERT INTO mcp_servers (tenant_id, id, name, description, server_type, endpoint, command, args_json, env_keys_json, department_id, risk_level, status, installed_at, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenant.RequestTenantID(r), server.ID, server.Name, server.Description, server.ServerType, server.Endpoint, server.Command, string(argsJSON), string(envKeysJSON), server.DepartmentID, server.RiskLevel, server.Status, server.InstalledAt, server.CreatedAt, server.UpdatedAt)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.Created(w, server)
}

func (h *Handler) updateMCPServer(w http.ResponseWriter, r *http.Request, id string) {
	var req mcpServerPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON body")
		return
	}
	server, err := normalizeMCPServerPayload(req)
	if err != nil {
		response.BadRequest(w, "INVALID_MCP_SERVER", err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	argsJSON, _ := json.Marshal(server.Args)
	envKeysJSON, _ := json.Marshal(server.EnvKeys)
	res, err := h.write.ExecContext(r.Context(), `UPDATE mcp_servers SET name=?, description=?, server_type=?, endpoint=?, command=?, args_json=?, env_keys_json=?, department_id=?, risk_level=?, status=?, updated_at=? WHERE tenant_id=? AND id=?`, server.Name, server.Description, server.ServerType, server.Endpoint, server.Command, string(argsJSON), string(envKeysJSON), server.DepartmentID, server.RiskLevel, server.Status, now, tenant.RequestTenantID(r), id)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		response.NotFound(w, "NOT_FOUND", "mcp server not found")
		return
	}
	server.ID = id
	server.UpdatedAt = now
	response.OK(w, server)
}

func normalizeMCPServerPayload(req mcpServerPayload) (MCPServer, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return MCPServer{}, fmt.Errorf("name is required")
	}
	serverType := strings.ToLower(strings.TrimSpace(req.ServerType))
	if serverType == "" {
		serverType = "http"
	}
	switch serverType {
	case "http", "sse", "stdio":
	default:
		return MCPServer{}, fmt.Errorf("server_type must be http, sse, or stdio")
	}
	endpoint := strings.TrimSpace(req.Endpoint)
	command := strings.TrimSpace(req.Command)
	if serverType == "stdio" && command == "" {
		return MCPServer{}, fmt.Errorf("command is required for stdio mcp servers")
	}
	if serverType != "stdio" && endpoint == "" {
		return MCPServer{}, fmt.Errorf("endpoint is required for network mcp servers")
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status == "" {
		status = "enabled"
	}
	if status != "enabled" && status != "disabled" {
		return MCPServer{}, fmt.Errorf("status must be enabled or disabled")
	}
	riskLevel := strings.TrimSpace(req.RiskLevel)
	if riskLevel == "" {
		riskLevel = "medium"
	}
	departmentID := strings.TrimSpace(req.DepartmentID)
	if departmentID == "" {
		departmentID = "all"
	}
	return MCPServer{Name: name, Description: strings.TrimSpace(req.Description), ServerType: serverType, Endpoint: endpoint, Command: command, Args: sanitizeStringList(req.Args), EnvKeys: sanitizeStringList(req.EnvKeys), DepartmentID: departmentID, RiskLevel: riskLevel, Status: status}, nil
}

func sanitizeStringList(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (h *Handler) listRuntimeMCPServers(ctx context.Context, tenantID, departmentID string) ([]MCPServer, error) {
	return h.listMCPServers(ctx, tenantID, departmentID, true)
}

func (h *Handler) listMCPServers(ctx context.Context, tenantID, departmentID string, enabledOnly bool) ([]MCPServer, error) {
	where := "tenant_id=?"
	args := []any{tenantID}
	if enabledOnly {
		where += " AND status='enabled'"
	}
	departmentID = strings.TrimSpace(departmentID)
	if departmentID != "" {
		where += " AND (department_id='' OR department_id='all' OR department_id=?)"
		args = append(args, departmentID)
	}
	rows, err := h.read.QueryContext(ctx, `SELECT id, name, description, server_type, endpoint, command, args_json, env_keys_json, department_id, risk_level, status, installed_at, created_at, updated_at FROM mcp_servers WHERE `+where+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	servers := []MCPServer{}
	for rows.Next() {
		server, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, rows.Err()
}

func (h *Handler) mcpServerByID(ctx context.Context, tenantID, id string) (MCPServer, error) {
	row := h.read.QueryRowContext(ctx, `SELECT id, name, description, server_type, endpoint, command, args_json, env_keys_json, department_id, risk_level, status, installed_at, created_at, updated_at FROM mcp_servers WHERE tenant_id=? AND id=?`, tenantID, id)
	return scanMCPServer(row)
}

type mcpScannable interface{ Scan(dest ...any) error }

func scanMCPServer(row mcpScannable) (MCPServer, error) {
	var server MCPServer
	var argsJSON, envKeysJSON string
	if err := row.Scan(&server.ID, &server.Name, &server.Description, &server.ServerType, &server.Endpoint, &server.Command, &argsJSON, &envKeysJSON, &server.DepartmentID, &server.RiskLevel, &server.Status, &server.InstalledAt, &server.CreatedAt, &server.UpdatedAt); err != nil {
		return MCPServer{}, err
	}
	_ = json.Unmarshal([]byte(argsJSON), &server.Args)
	_ = json.Unmarshal([]byte(envKeysJSON), &server.EnvKeys)
	if server.Args == nil {
		server.Args = []string{}
	}
	if server.EnvKeys == nil {
		server.EnvKeys = []string{}
	}
	return server, nil
}
