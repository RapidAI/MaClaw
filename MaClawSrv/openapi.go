package main

import (
	"net/http"
	"sort"
	"strings"
)

type openAPIRoute struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Tag         string
	Security    []map[string][]string
	QueryParams []string
}

var openAPIRoutes = []openAPIRoute{
	{Method: http.MethodGet, Path: "/health", Summary: "Service health", Description: "Returns a basic health status for load balancers and uptime checks.", Tag: "system"},
	{Method: http.MethodGet, Path: "/livez", Summary: "Liveness probe", Description: "Returns process liveness for container orchestration and uptime checks.", Tag: "system"},
	{Method: http.MethodGet, Path: "/readyz", Summary: "Readiness probe", Description: "Returns service readiness for load balancers and container orchestration.", Tag: "system"},
	{Method: http.MethodGet, Path: "/version", Summary: "Service version", Description: "Returns build and version metadata for the current MaClawSrv binary.", Tag: "system"}, {Method: http.MethodGet, Path: "/metrics", Summary: "Prometheus metrics", Description: "Returns Prometheus text metrics for service-wide counters.", Tag: "system"},
	{Method: http.MethodGet, Path: "/openapi.json", Summary: "OpenAPI document", Description: "Returns the machine-readable OpenAPI description for MaClawSrv.", Tag: "system"},
	{Method: http.MethodGet, Path: "/api/v1/openapi.json", Summary: "OpenAPI document", Description: "Returns the machine-readable OpenAPI description for MaClawSrv.", Tag: "system"},
	{Method: http.MethodGet, Path: "/api/v1/admin/system/readiness", Summary: "Admin readiness details", Description: "Returns admin-only detailed readiness checks for the service data root and writable state paths.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/overview", Summary: "Admin overview", Description: "Returns control-plane aggregate counts for tenants, users, activity, and audit events.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/dashboard", Summary: "Admin dashboard", Description: "Returns overview, recent audit events, and recent 24h/7d activity trends for admin homepages.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/insights", Summary: "Admin insights", Description: "Returns top tenants, inactive users, and quota-pressure insights for operator consoles.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"inactive_for_days", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/alerts", Summary: "Admin alerts", Description: "Returns unready instances, waiting runs, and failed runs for operator alert panels.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "kind", "since", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants", Summary: "List tenants", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "name", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants", Summary: "Create tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Get tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/summary", Summary: "Get tenant summary", Description: "Returns aggregate tenant usage plus per-user rollups for control-plane dashboards.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Update tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Delete tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/users", Summary: "List users across tenants", Description: "Returns users across all tenants or within one tenant when tenant_id is provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "status", "name", "email", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users", Summary: "List users", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "name", "email", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users", Summary: "Create user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Get user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Update user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Delete user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", Summary: "List credentials", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", Summary: "Create credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Get credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Update credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret", Summary: "Rotate credential secret", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key", Summary: "Rotate credential API key", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Revoke credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/audit-events", Summary: "List audit events", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "action", "resource_type", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/export", Summary: "Export service state", Description: "Exports service, tenant, or user state for backup, migration, or inspection. Sensitive values are omitted unless include_secrets=true.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "include_messages", "include_runs", "include_audit", "include_secrets"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/import", Summary: "Import service state", Description: "Imports previously exported service, tenant, or user state. Imported instance paths are remapped into the current data root, and dry_run mode returns conflicts, warnings, and per-resource plan actions without mutating state.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"overwrite", "dry_run"}},
	{Method: http.MethodPost, Path: "/api/v1/auth/token", Summary: "Issue bearer token", Description: "Exchanges tenant user API key and secret for a bearer token.", Tag: "auth"},
	{Method: http.MethodGet, Path: "/api/v1/me", Summary: "Current principal", Tag: "auth", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config/schema", Summary: "Config schema", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config", Summary: "Get config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/config", Summary: "Update config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/validate", Summary: "Validate config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/test", Summary: "Test config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/usage/summary", Summary: "Usage summary", Tag: "usage", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/mcp/servers", Summary: "List MCP servers", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/mcp/servers", Summary: "Create MCP server", Description: "Creates an MCP server synchronously or starts an async job when async=true.", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodGet, Path: "/api/v1/mcp/servers/{serverId}", Summary: "Get MCP server", Tag: "mcp", Security: bearerSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/mcp/servers/{serverId}", Summary: "Update MCP server", Description: "Updates an MCP server synchronously or starts an async job when async=true.", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodDelete, Path: "/api/v1/mcp/servers/{serverId}", Summary: "Delete MCP server", Tag: "mcp", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/mcp/servers/{serverId}/start", Summary: "Start MCP server", Description: "Starts an MCP server synchronously or starts an async job when async=true.", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodPost, Path: "/api/v1/mcp/servers/{serverId}/stop", Summary: "Stop MCP server", Description: "Stops an MCP server synchronously or starts an async job when async=true.", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodPost, Path: "/api/v1/mcp/servers/{serverId}/health-check", Summary: "Check MCP server", Description: "Checks MCP server health synchronously or starts an async job when async=true.", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodGet, Path: "/api/v1/mcp/servers/{serverId}/tools", Summary: "List MCP tools", Tag: "mcp", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/skills", Summary: "List skills", Tag: "skills", Security: bearerSecurity(), QueryParams: []string{"limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/skills/search", Summary: "Search skills", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/skills/install", Summary: "Install skill", Description: "Installs a skill synchronously or starts an async job when async=true.", Tag: "skills", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodPost, Path: "/api/v1/skills/import", Summary: "Import skill", Description: "Imports a skill archive synchronously or starts an async job when async=true.", Tag: "skills", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodGet, Path: "/api/v1/jobs", Summary: "List async jobs", Description: "Lists async jobs owned by the current tenant/user.", Tag: "jobs", Security: bearerSecurity(), QueryParams: []string{"status", "kind", "limit", "before"}},
	{Method: http.MethodDelete, Path: "/api/v1/jobs", Summary: "Delete async jobs", Description: "Deletes completed, failed, or canceled async jobs owned by the current tenant/user, filtered by kind, status, before, or all=true.", Tag: "jobs", Security: bearerSecurity(), QueryParams: []string{"kind", "status", "before", "all"}},
	{Method: http.MethodGet, Path: "/api/v1/jobs/{jobId}", Summary: "Get async job", Description: "Returns the current state of an async user-scoped job started by async skill or MCP operations.", Tag: "jobs", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/jobs/{jobId}/cancel", Summary: "Cancel async job", Description: "Requests cancellation for a pending or running async job owned by the current tenant/user.", Tag: "jobs", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/jobs/{jobId}", Summary: "Delete async job", Description: "Deletes a completed, failed, or canceled async job owned by the current tenant/user.", Tag: "jobs", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/skill-uploads/{submissionId}", Summary: "Skill upload status", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/skill-market/account", Summary: "Skill market account", Description: "Returns author account profile from the configured skill market by email and optional base_url.", Tag: "skills", Security: bearerSecurity(), QueryParams: []string{"email", "base_url"}},
	{Method: http.MethodGet, Path: "/api/v1/skills/{skillName}", Summary: "Get skill", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/skills/{skillName}", Summary: "Delete skill", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/skills/{skillName}/export", Summary: "Export skill", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/skills/{skillName}/validate", Summary: "Validate skill", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/skills/{skillName}/improve", Summary: "Improve skill", Tag: "skills", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/skills/{skillName}/upload", Summary: "Upload skill", Description: "Uploads a skill synchronously or starts an async job when async=true.", Tag: "skills", Security: bearerSecurity(), QueryParams: []string{"async"}},
	{Method: http.MethodGet, Path: "/api/v1/instances", Summary: "List instances", Tag: "instances", Security: bearerSecurity(), QueryParams: []string{"limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/instances", Summary: "Create instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}", Summary: "Get instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/instances/{instanceId}", Summary: "Update instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/instances/{instanceId}", Summary: "Delete instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/capabilities", Summary: "Instance capabilities", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/stop", Summary: "Stop instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/resume", Summary: "Resume instance", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/refresh-readiness", Summary: "Refresh readiness", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/summary", Summary: "Instance summary", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/bootstrap", Summary: "Instance bootstrap", Tag: "instances", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/messages", Summary: "Send message", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/sessions", Summary: "List sessions", Tag: "sessions", Security: bearerSecurity(), QueryParams: []string{"include_archived", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions", Summary: "Create session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}", Summary: "Get session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}", Summary: "Update session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}", Summary: "Delete session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/archive", Summary: "Archive session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/restore", Summary: "Restore session", Tag: "sessions", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/messages", Summary: "List messages", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"role", "since", "until", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/messages", Summary: "Post message", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/runs", Summary: "List runs", Description: "Lists runs for one instance. status accepts running, succeeded, failed, cancelled. response_source currently accepts ask_user when filtering waiting-for-user flows.", Tag: "runs", Security: bearerSecurity(), QueryParams: []string{"status", "session_id", "response_source", "waiting_for_user", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/runs/{runId}", Summary: "Get run", Tag: "runs", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/runs/{runId}/events", Summary: "Stream run events", Description: "Returns a server-sent events stream for run snapshots and terminal updates.", Tag: "runs", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/runs/{runId}/cancel", Summary: "Cancel run", Tag: "runs", Security: bearerSecurity()},
}

func adminSecurity() []map[string][]string {
	return []map[string][]string{{"adminSecret": {}}}
}

func bearerSecurity() []map[string][]string {
	return []map[string][]string{{"bearerAuth": {}}}
}

func buildOpenAPISpec() map[string]any {
	paths := map[string]map[string]any{}
	tags := map[string]struct{}{}
	for _, route := range openAPIRoutes {
		if route.Tag != "" {
			tags[route.Tag] = struct{}{}
		}
		if _, ok := paths[route.Path]; !ok {
			paths[route.Path] = map[string]any{}
		}
		op := map[string]any{
			"summary":     route.Summary,
			"operationId": operationID(route.Method, route.Path),
			"responses": map[string]any{
				"200": map[string]any{"description": "Successful response"},
				"400": map[string]any{"description": "Bad request"},
				"401": map[string]any{"description": "Unauthorized"},
				"500": map[string]any{"description": "Internal server error"},
			},
		}
		if route.Description != "" {
			op["description"] = route.Description
		}
		if route.Tag != "" {
			op["tags"] = []string{route.Tag}
		}
		if len(route.Security) > 0 {
			op["security"] = route.Security
		}
		if params := buildOpenAPIParameters(route.Path, route.QueryParams); len(params) > 0 {
			op["parameters"] = params
		}
		if route.Method == http.MethodPost || route.Method == http.MethodPut || route.Method == http.MethodPatch {
			op["requestBody"] = map[string]any{
				"required": false,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": map[string]any{"type": "object"},
					},
				},
			}
		}
		if route.Path == "/api/v1/instances/{instanceId}/runs/{runId}/events" {
			op["responses"] = map[string]any{
				"200": map[string]any{
					"description": "Server-sent events stream",
					"content": map[string]any{
						"text/event-stream": map[string]any{
							"schema": map[string]any{"type": "string"},
						},
					},
				},
				"401": map[string]any{"description": "Unauthorized"},
				"404": map[string]any{"description": "Run not found"},
			}
		}
		if route.Path == "/openapi.json" || route.Path == "/api/v1/openapi.json" {
			op["responses"] = map[string]any{
				"200": map[string]any{
					"description": "OpenAPI document",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"type": "object"},
						},
					},
				},
			}
		}
		paths[route.Path][strings.ToLower(route.Method)] = op
	}

	tagList := make([]map[string]string, 0, len(tags))
	for tag := range tags {
		tagList = append(tagList, map[string]string{"name": tag})
	}
	sort.Slice(tagList, func(i, j int) bool { return tagList[i]["name"] < tagList[j]["name"] })

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "MaClawSrv API",
			"version":     "1.0.0",
			"description": "REST API for multi-tenant Maclaw agent runtime management and usage.",
		},
		"servers": []map[string]string{{"url": "/", "description": "Current MaClawSrv host"}},
		"tags":    tagList,
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"adminSecret": map[string]any{
					"type": "apiKey",
					"in":   "header",
					"name": "X-MaClaw-Admin-Secret",
				},
				"bearerAuth": map[string]any{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "JWT",
				},
			},
		},
	}
}

func buildOpenAPIParameters(path string, queryParams []string) []map[string]any {
	params := []map[string]any{}
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
			params = append(params, map[string]any{
				"name":     name,
				"in":       "path",
				"required": true,
				"schema":   map[string]any{"type": "string"},
			})
		}
	}
	for _, name := range queryParams {
		params = append(params, map[string]any{
			"name":        name,
			"in":          "query",
			"required":    false,
			"schema":      openAPIQuerySchema(path, name),
			"description": openAPIQueryDescription(path, name),
		})
	}
	return params
}

func openAPIQuerySchema(path, name string) map[string]any {
	switch name {
	case "include_archived", "waiting_for_user", "include_messages", "include_runs", "include_audit", "include_secrets", "overwrite", "dry_run", "all":
		return map[string]any{"type": "boolean"}
	case "async":
		return map[string]any{"type": "boolean"}
	case "status":
		switch path {
		case "/api/v1/admin/tenants":
			return map[string]any{"type": "string", "enum": []string{"active", "disabled"}}
		case "/api/v1/admin/tenants/{tenantId}/users":
			return map[string]any{"type": "string", "enum": []string{"active", "disabled"}}
		case "/api/v1/jobs":
			return map[string]any{"type": "string", "enum": []string{"pending", "running", "succeeded", "failed", "canceled"}}
		case "/api/v1/instances/{instanceId}/runs":
			return map[string]any{"type": "string", "enum": []string{"running", "succeeded", "failed", "cancelled"}}
		}
	case "role":
		return map[string]any{"type": "string", "enum": []string{"user", "assistant", "system"}}
	case "response_source":
		return map[string]any{"type": "string", "enum": []string{"ask_user"}}
	case "before", "since", "until":
		if path != "/api/v1/skills" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
	case "limit":
		if path == "/api/v1/admin/insights" {
			return map[string]any{"type": "integer", "minimum": 0, "maximum": 50}
		}
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 500}
	case "inactive_for_days":
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 3650}
	}
	return map[string]any{"type": "string"}
}

func openAPIQueryDescription(path, name string) string {
	switch name {
	case "before":
		if path == "/api/v1/skills" {
			return "Case-insensitive skill-name cursor."
		}
		return "RFC3339 timestamp cursor."
	case "status":
		if path == "/api/v1/jobs" {
			return "Bulk delete only accepts terminal statuses."
		}
	case "response_source":
		return "Currently only ask_user is supported for filtering."
	}
	return ""
}

func operationID(method, path string) string {
	cleaned := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_").Replace(strings.Trim(path, "/"))
	if cleaned == "" {
		cleaned = "root"
	}
	return strings.ToLower(method) + "_" + cleaned
}

func (s *HTTPServer) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildOpenAPISpec())
}
