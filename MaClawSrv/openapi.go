package main

import (
	"net/http"
	"sort"
	"strings"
)

type openAPIRoute struct {
	Method          string
	Path            string
	Summary         string
	Description     string
	Tag             string
	Security        []map[string][]string
	QueryParams     []string
	ResponseContent string
	AdminRole       string
}

var openAPIRoutes = []openAPIRoute{
	{Method: http.MethodGet, Path: "/health", Summary: "Service health", Description: "Returns a basic health status for load balancers and uptime checks.", Tag: "system"},
	{Method: http.MethodGet, Path: "/livez", Summary: "Liveness probe", Description: "Returns process liveness for container orchestration and uptime checks.", Tag: "system"},
	{Method: http.MethodGet, Path: "/readyz", Summary: "Readiness probe", Description: "Returns service readiness for load balancers and container orchestration.", Tag: "system"},
	{Method: http.MethodGet, Path: "/version", Summary: "Service version", Description: "Returns build and version metadata for the current MaClawSrv binary.", Tag: "system"},
	{Method: http.MethodGet, Path: "/metrics", Summary: "Prometheus metrics", Description: "Returns Prometheus text metrics for service-wide counters including credential lifecycle gauges and persisted snapshot storage gauges.", Tag: "system", ResponseContent: "text/plain"},
	{Method: http.MethodGet, Path: "/openapi.json", Summary: "OpenAPI document", Description: "Returns the machine-readable OpenAPI description for MaClawSrv.", Tag: "system"},
	{Method: http.MethodGet, Path: "/api/v1/openapi.json", Summary: "OpenAPI document", Description: "Returns the machine-readable OpenAPI description for MaClawSrv.", Tag: "system"},
	{Method: http.MethodGet, Path: "/api/v1/admin/bootstrap/status", Summary: "Admin bootstrap status", Description: "Returns non-sensitive Admin Web setup state and password policy.", Tag: "admin-bootstrap"},
	{Method: http.MethodPost, Path: "/api/v1/admin/bootstrap/initialize", Summary: "Initialize admin owner", Description: "Creates the first Admin Web owner account when bootstrap setup is required.", Tag: "admin-bootstrap"},
	{Method: http.MethodPost, Path: "/api/v1/admin/auth/login", Summary: "Admin login", Description: "Authenticates an Admin Web account and returns an admin session token used as the admin secret header.", Tag: "admin-auth"},
	{Method: http.MethodPost, Path: "/api/v1/admin/auth/logout", Summary: "Admin logout", Description: "Revokes the current Admin Web session token.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/auth/me", Summary: "Current admin", Description: "Returns the current Admin Web session account when authenticated with an admin session token.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/auth/change-password", Summary: "Change admin password", Description: "Changes the current Admin Web account password. Requires an admin session token and revokes other sessions for the account.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/auth/users", Summary: "List admin users", Description: "Lists Admin Web operator accounts for owner-level administration.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/auth/users", Summary: "Create admin user", Description: "Creates an Admin Web owner or operator account. Requires an active owner or root admin secret.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/auth/users/{adminUserId}", Summary: "Update admin user", Description: "Updates an Admin Web operator account status, display name, locale, or password. Suspending the last active owner requires confirm_unsafe=true; password resets revoke active sessions for that account.", Tag: "admin-auth", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/auth/sessions", Summary: "List admin sessions", Description: "Lists Admin Web sessions for owner-level account diagnostics and revocation.", Tag: "admin-auth", Security: adminSecurity(), QueryParams: []string{"user_id"}},
	{Method: http.MethodDelete, Path: "/api/v1/admin/auth/sessions/{sessionId}", Summary: "Revoke admin session", Description: "Revokes one Admin Web session after confirm=true is explicitly provided.", Tag: "admin-auth", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/system/readiness", Summary: "Admin readiness details", Description: "Returns admin-only detailed readiness checks for the service data root and writable state paths.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/overview", Summary: "Admin overview", Description: "Returns control-plane aggregate counts for tenants, users, activity, audit events, credentials, and persisted snapshots.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/dashboard", Summary: "Admin dashboard", Description: "Returns overview, redacted recent audit events, and recent 24h/7d activity trends for admin homepages. Audit metadata and path-like resource ids are redacted before response serialization.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/support-bundle", Summary: "Admin support bundle", Description: "Returns a redacted service-level troubleshooting bundle with runtime status, dashboard, service config posture, sandbox summary, recent logs, recent audit events, and risk summary. Set download=true to include a JSON attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"download"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/insights", Summary: "Admin insights", Description: "Returns top tenants, inactive users, and quota-pressure insights for operator consoles.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"inactive_for_days", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/alerts", Summary: "Admin alerts", Description: "Returns unready instances, waiting runs, failed runs, and credential expiry alerts for operator panels.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "kind", "since", "limit", "credential_expiry_window_days"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/security/summary", Summary: "Admin security summary", Description: "Returns current admin security posture, generated_at, applied filters, risk severity counts, risk kind counts, and recent risk events derived from audit entries and service posture. Risk events derived from audit entries redact sensitive metadata and path-like resource ids. When both since and until are provided, since must be before or equal to until.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"since", "until"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/security/risk-events", Summary: "Admin security risk events", Description: "Returns security-relevant risk events with generated_at, applied filters, severity counts, and kind counts derived from audit entries and service posture, including auth failures, sandbox failures, disabled sandbox, and insecure HTTP posture. Risk events derived from audit entries redact sensitive metadata and path-like resource ids. The severity filter accepts high, medium, or low case-insensitively; kind filters by stable risk kind case-insensitively; when both since and until are provided, since must be before or equal to until.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"severity", "kind", "since", "until", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/status", Summary: "Admin runtime status", Description: "Returns process, readiness, scheduler, job, sandbox, and log source status for Admin Web runtime pages.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/runtime/gc", Summary: "Run runtime GC", Description: "Forces Go garbage collection and returns before/after memory counters for Admin Web diagnostics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/goroutines", Summary: "Runtime goroutine dump", Description: "Returns a redacted text/plain Go goroutine profile for deadlock and leak diagnostics. Set download=true to include an attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"debug", "download"}, ResponseContent: "text/plain"},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/profiles/{profileName}", Summary: "Runtime text profile", Description: "Returns a redacted text/plain Go runtime profile for heap, allocs, block, mutex, or threadcreate diagnostics. Only debug=1 or debug=2 text output is supported; heap/allocs may set gc=true first; download=true adds an attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"debug", "gc", "download"}, ResponseContent: "text/plain"},
	{Method: http.MethodGet, Path: "/api/v1/admin/scheduler/status", Summary: "Scheduler status", Description: "Returns scheduler enablement and persisted scheduled task rollups.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/jobs", Summary: "List admin jobs", Description: "Lists async jobs across tenants for Admin Web operations diagnostics.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "kind", "status", "limit"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/jobs/{jobId}/cancel", Summary: "Cancel admin job", Description: "Cancels a pending or running async job across tenants.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/logs/sources", Summary: "List log sources", Description: "Lists service log files available for Admin Web diagnostics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/logs/errors/recent", Summary: "Recent log errors", Description: "Returns a bounded, redacted list of recent error log lines across configured service log sources. Set include_warn=true to include warnings.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"limit", "include_warn"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/logs/search", Summary: "Search logs", Description: "Searches configured service log sources with bounded tail, limit, source, level, and q filters. Returned lines are redacted.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/logs/{sourceId}", Summary: "Read log source", Description: "Returns a bounded, redacted tail of a configured service log source.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tail", "level", "q"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/logs/{sourceId}/tail", Summary: "Tail log source", Description: "Compatibility alias for reading a bounded, redacted tail of a configured service log source.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tail", "level", "q"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/logs/{sourceId}/download", Summary: "Download redacted log source", Description: "Downloads a bounded text/plain redacted tail of a configured service log source.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tail", "level", "q"}, ResponseContent: "text/plain"},
	{Method: http.MethodPost, Path: "/api/v1/admin/logs/{sourceId}/rotate", Summary: "Rotate log source", Description: "Renames the current log file to a timestamped sibling and creates a new empty file. Requires confirm=true.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/service-config/effective", Summary: "Effective service config", Description: "Returns redacted effective MaClawSrv service configuration for Admin Web bootstrap and settings pages.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/service-config/schema", Summary: "Service config schema", Description: "Returns writable and read-only service-level configuration metadata for Admin Web settings.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/service-config/environment", Summary: "Service config environment", Description: "Returns redacted managed environment variable posture for service-level configuration diagnostics, including configured flags, sources, defaults, sensitivity, and restart/runtime metadata.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/service-config/diff", Summary: "Service config draft diff", Description: "Compares the persisted service config draft against current process environment and returns redacted changed keys plus validation metadata.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/service-config/draft", Summary: "Service config draft", Description: "Returns the persisted service configuration draft and validation result with sensitive values and local paths redacted for Admin Web display.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/service-config/draft", Summary: "Update service config draft", Description: "Persists a service configuration draft after validation. Redacted sensitive and path placeholders preserve existing draft or environment values when available. Restart-required fields are staged, not applied to the current process.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/service-config/draft", Summary: "Clear service config draft", Description: "Clears the persisted service configuration draft after confirm=true, returning to environment/default values for future validation/export plans.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/service-config/validate", Summary: "Validate service config", Description: "Validates submitted service configuration values or the current draft and returns a redacted environment application plan.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/service-config/export-plan", Summary: "Export service config plan", Description: "Builds .env and systemd drop-in content from submitted service config values or the current draft. JSON plan fields are redacted for display; manual apply text preserves non-sensitive paths. This endpoint never writes host service files.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/i18n/locales", Summary: "Admin locales", Description: "Returns Admin Web supported locales, labels, and the configured default locale after normalizing aliases such as zh_CN, zh, en_US, and en.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/i18n/messages", Summary: "Admin locale messages", Description: "Returns Admin Web message strings for the requested locale. Locale aliases such as zh_CN, zh, en_US, and en are normalized; unsupported locales return 400 with enabled_locales.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"locale"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/status", Summary: "Sandbox status", Description: "Returns sandbox mode, detected capabilities, backend availability, and effective fallback decision.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/config", Summary: "Sandbox config", Description: "Returns runtime sandbox mode and strictness overrides used by Admin Web.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/sandbox/config", Summary: "Update sandbox config", Description: "Updates runtime sandbox mode or strictness override for fast admin troubleshooting. Switching to none requires confirm_unsafe=true.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/rollback", Summary: "Rollback sandbox config", Description: "Clears runtime sandbox mode and strictness overrides so environment/default settings apply again.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/switch", Summary: "Switch sandbox backend", Description: "Switches the runtime sandbox backend and returns a lightweight diagnose report for immediate administrator verification. Switching to none requires confirm_unsafe=true.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/detect", Summary: "Detect sandbox", Description: "Refreshes sandbox capability and backend detection, including optional backend smoke checks.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/smoke-test", Summary: "Sandbox smoke test", Description: "Runs a lightweight sandbox diagnose probe without persisting a report.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/diagnose", Summary: "Diagnose sandbox", Description: "Runs sandbox verification checks and persists a report by default for administrator review.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/events", Summary: "List sandbox events", Description: "Lists sandbox-related admin audit events for troubleshooting and timeline views.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "backend", "since", "until", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/support-bundle", Summary: "Sandbox support bundle", Description: "Returns a redacted troubleshooting bundle with sandbox status, runtime config, recent reports, sandbox audit events, profiles, install guidance, log sources, recent redacted log errors, redacted data root metadata, explicit redactions list, and a security_risks summary with generated_at, filters, severity counts, kind counts, and recent risk events. Set download=true to include a JSON attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"download"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/profiles", Summary: "List sandbox profiles", Description: "Lists persisted sandbox profile definitions for Admin Web policy editing.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/profiles/{profileName}", Summary: "Get sandbox profile", Description: "Returns one sandbox profile and validation result.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/sandbox/profiles/{profileName}", Summary: "Update sandbox profile", Description: "Validates and persists one sandbox profile definition.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/sandbox/profiles/{profileName}", Summary: "Delete sandbox profile", Description: "Deletes one sandbox profile after confirm=true is explicitly provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/profiles/{profileName}/validate", Summary: "Validate sandbox profile", Description: "Validates a sandbox profile payload without persisting it.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/reports", Summary: "List sandbox reports", Description: "Lists recent persisted sandbox diagnose reports.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/reports/{reportId}", Summary: "Get sandbox report", Description: "Returns one persisted sandbox diagnose report.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/sandbox/reports/{reportId}", Summary: "Delete sandbox report", Description: "Deletes one persisted sandbox diagnose report after confirm=true is explicitly provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/sandbox/install-plan", Summary: "Sandbox install plan", Description: "Returns host-specific install guidance for a sandbox backend. This endpoint does not execute installation commands.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"backend"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/sandbox/install", Summary: "Sandbox install", Description: "Returns a sandbox install plan by default. Command execution requires owner authorization, confirm=true, mode=run, Linux, and MACLAW_SANDBOX_INSTALL_POLICY=run.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants", Summary: "List tenants", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "name", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants", Summary: "Create tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Get tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/summary", Summary: "Get tenant summary", Description: "Returns aggregate tenant usage plus per-user rollups for control-plane dashboards.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/delete-check", Summary: "Tenant delete precheck", Description: "Returns the impact summary and blockers before deleting a tenant.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Update tenant", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/pause", Summary: "Pause tenant", Description: "Disables a tenant without deleting data, so the tenant can be resumed later.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/resume", Summary: "Resume tenant", Description: "Reactivates a paused tenant.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}", Summary: "Delete tenant", Description: "Deletes a tenant after confirm=true is explicitly provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/users", Summary: "List users across tenants", Description: "Returns users across all tenants or within one tenant when tenant_id is provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "status", "name", "email", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users", Summary: "List users", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "name", "email", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users", Summary: "Create user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Get user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check", Summary: "User delete precheck", Description: "Returns the impact summary and blockers before deleting a user.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Update user", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/pause", Summary: "Pause user", Description: "Disables a tenant user without deleting data, so the user can be resumed later.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/resume", Summary: "Resume user", Description: "Reactivates a paused tenant user.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}", Summary: "Delete user", Description: "Deletes a user after confirm=true is explicitly provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", Summary: "List credentials", Description: "Lists credentials with optional status, expired, and expiring filters.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"status", "expired", "expiring", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", Summary: "Create credential", Description: "Creates a credential. api_key, api_secret, and expires_at are optional; omitted key/secret values are generated and returned only in the create response.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Get credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Update credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret", Summary: "Rotate credential secret", Description: "Rotates the credential secret. api_secret is optional; when omitted, a generated secret is returned only in this response.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key", Summary: "Rotate credential API key", Description: "Rotates the credential API key. api_key is optional; when omitted, a generated key is returned only in this response.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", Summary: "Revoke credential", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/audit-events", Summary: "List audit events", Description: "Lists audit events for Admin Web filtering. Returned audit metadata and path-like resource ids are redacted before response serialization; filters are applied before redaction.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "action", "resource_type", "resource_id", "actor_type", "actor_tenant_id", "actor_user_id", "since", "until", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/export", Summary: "Export service state", Description: "Exports service, tenant, or user state for backup, migration, or inspection. Sensitive values are omitted unless include_secrets=true; exported audit events always redact sensitive metadata and path-like resource ids.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "include_messages", "include_runs", "include_audit", "include_secrets", "confirm"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/import", Summary: "Import service state", Description: "Imports previously exported service, tenant, or user state. Imported instance paths are remapped into the current data root, and dry_run mode returns conflicts, warnings, and per-resource plan actions without mutating state.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"overwrite", "dry_run", "confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/snapshots", Summary: "List service snapshots", Description: "Lists persisted export snapshots saved under the service data root.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "scope", "name", "since", "until", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/snapshots", Summary: "Create service snapshot", Description: "Creates a persisted service, tenant, or user snapshot by reusing the export pipeline and writing a private JSON file under the service data root. Snapshots that include secrets require confirm=true.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/snapshots/prune", Summary: "Prune service snapshots", Description: "Deletes old persisted snapshots by tenant/user scope, older_than cutoff, and keep_latest retention. Supports dry_run previews.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "older_than", "keep_latest", "dry_run"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/snapshots/{snapshotId}", Summary: "Get service snapshot", Description: "Returns snapshot metadata and the exported state payload. Snapshots that include secrets require owner authorization.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/snapshots/{snapshotId}/restore", Summary: "Restore service snapshot", Description: "Restores a persisted snapshot through the same import pipeline used by /api/v1/admin/import. Supports dry_run and overwrite in either query string or JSON body.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"overwrite", "dry_run", "confirm"}},
	{Method: http.MethodDelete, Path: "/api/v1/admin/snapshots/{snapshotId}", Summary: "Delete service snapshot", Description: "Deletes a persisted snapshot after confirm=true is explicitly provided.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/retire-plan", Summary: "Tenant retire plan", Description: "Returns tenant delete precheck plus a scoped export payload for export-before-delete flows.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"include_messages", "include_runs", "include_audit", "include_secrets"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan", Summary: "User retire plan", Description: "Returns user delete precheck plus a scoped export payload for export-before-delete flows.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"include_messages", "include_runs", "include_audit", "include_secrets"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge/stats", Summary: "Admin knowledge stats", Description: "Returns service-wide knowledge store statistics for Admin Web diagnostics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge/sources", Summary: "List all knowledge sources", Description: "Lists knowledge sources across tenants and users for Admin Web inspection.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/knowledge", Summary: "Clear tenant knowledge", Description: "Deletes knowledge sources for one tenant after confirm=true is explicitly provided. This is an irreversible admin cleanup operation.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/cross-tenant", Summary: "Knowledge cross-tenant access", Description: "Returns whether administrators may configure cross-tenant readable knowledge scopes.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/knowledge-access/cross-tenant", Summary: "Update knowledge cross-tenant access", Description: "Enables or disables admin configuration of cross-tenant readable knowledge scopes.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Get knowledge access", Description: "Returns the configured additional readable knowledge scopes for one user. The user's own scope is implicit.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Update knowledge access", Description: "Configures additional readable knowledge scopes for one user. Same-tenant scopes are allowed by default; cross-tenant scopes require the global cross-tenant switch.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Delete knowledge access", Description: "Deletes the configured additional readable knowledge scopes for one user.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/resolve", Summary: "Resolve knowledge access", Description: "Returns the effective readable knowledge scopes for one user, including the implicit self scope and cross-tenant filtering.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/available", Summary: "Available skill sources", Description: "Returns the canonical list of skill source identifiers that can be allowed or blocked by admin policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/global", Summary: "Get global skill source policy", Description: "Returns the global service-wide skill source allow policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/global", Summary: "Update global skill source policy", Description: "Updates the global service-wide skill source allow policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Get tenant skill source policy", Description: "Returns one tenant-level skill source override.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Update tenant skill source policy", Description: "Updates one tenant-level skill source override.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Delete tenant skill source policy", Description: "Deletes one tenant-level skill source override so the global policy applies.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/user/{email...}", Summary: "Get user skill source policy", Description: "Returns one user-level skill source override by email.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/user/{email...}", Summary: "Update user skill source policy", Description: "Updates one user-level skill source override by email.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/skill-sources/user/{email...}", Summary: "Delete user skill source policy", Description: "Deletes one user-level skill source override so tenant or global policy applies.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/resolve/{email...}", Summary: "Resolve skill source policy", Description: "Returns effective allowed skill sources for one user after user, tenant, and global policy are applied.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id"}},
	{Method: http.MethodPost, Path: "/api/v1/auth/token", Summary: "Issue bearer token", Description: "Exchanges tenant user API key and secret for a bearer token.", Tag: "auth"},
	{Method: http.MethodGet, Path: "/api/v1/me", Summary: "Current principal", Tag: "auth", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config/schema", Summary: "Config schema", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config", Summary: "Get config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/config", Summary: "Update config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/validate", Summary: "Validate config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/test", Summary: "Test config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/access", Summary: "Effective knowledge access", Description: "Returns the current user's effective readable knowledge scopes. Own knowledge is included by default.", Tag: "knowledge", Security: bearerSecurity()},
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
	{Method: http.MethodGet, Path: "/api/v1/records", Summary: "List structured records", Description: "Lists user-scoped flexible JSON records across collections with optional tag and text filters.", Tag: "records", Security: bearerSecurity(), QueryParams: []string{"tag", "q", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/records/{collection}", Summary: "List collection records", Description: "Lists flexible JSON records from one collection, such as finance or hr.", Tag: "records", Security: bearerSecurity(), QueryParams: []string{"tag", "q", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/records/{collection}", Summary: "Create structured record", Description: "Creates a flexible JSON record. The data field accepts any JSON object so callers can store finance, HR, or other structured information without a fixed schema.", Tag: "records", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/records/{collection}/{recordId}", Summary: "Get structured record", Tag: "records", Security: bearerSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/records/{collection}/{recordId}", Summary: "Update structured record", Description: "Updates title, tags, or replaces the flexible JSON data object.", Tag: "records", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/records/{collection}/{recordId}", Summary: "Delete structured record", Tag: "records", Security: bearerSecurity()},
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

func routeOpenAPIAdminRole(route openAPIRoute) string {
	if route.AdminRole != "" {
		return route.AdminRole
	}
	if isOwnerOpenAPIRoute(route.Method, route.Path) {
		return "owner"
	}
	if strings.HasPrefix(route.Path, "/api/v1/admin/") && len(route.Security) > 0 {
		return "operator"
	}
	return ""
}

func appendOpenAPIAdminRoleDescription(description, role string) string {
	if role != "owner" {
		return description
	}
	const note = "Requires an Admin Web owner role or the root admin secret."
	if strings.Contains(description, note) {
		return description
	}
	if strings.TrimSpace(description) == "" {
		return note
	}
	return description + " " + note
}

func isOwnerOpenAPIRoute(method, path string) bool {
	switch method + " " + path {
	case
		http.MethodGet + " /api/v1/admin/auth/users",
		http.MethodPost + " /api/v1/admin/auth/users",
		http.MethodPatch + " /api/v1/admin/auth/users/{adminUserId}",
		http.MethodGet + " /api/v1/admin/auth/sessions",
		http.MethodDelete + " /api/v1/admin/auth/sessions/{sessionId}",
		http.MethodPost + " /api/v1/admin/runtime/gc",
		http.MethodPost + " /api/v1/admin/jobs/{jobId}/cancel",
		http.MethodPost + " /api/v1/admin/logs/{sourceId}/rotate",
		http.MethodPatch + " /api/v1/admin/service-config/draft",
		http.MethodDelete + " /api/v1/admin/service-config/draft",
		http.MethodPost + " /api/v1/admin/service-config/export-plan",
		http.MethodPut + " /api/v1/admin/sandbox/config",
		http.MethodPost + " /api/v1/admin/sandbox/rollback",
		http.MethodPost + " /api/v1/admin/sandbox/switch",
		http.MethodPut + " /api/v1/admin/sandbox/profiles/{profileName}",
		http.MethodDelete + " /api/v1/admin/sandbox/profiles/{profileName}",
		http.MethodDelete + " /api/v1/admin/sandbox/reports/{reportId}",
		http.MethodPost + " /api/v1/admin/sandbox/install",
		http.MethodPost + " /api/v1/admin/tenants",
		http.MethodPatch + " /api/v1/admin/tenants/{tenantId}",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/pause",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/resume",
		http.MethodDelete + " /api/v1/admin/tenants/{tenantId}",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users",
		http.MethodPatch + " /api/v1/admin/tenants/{tenantId}/users/{userId}",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/pause",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/resume",
		http.MethodDelete + " /api/v1/admin/tenants/{tenantId}/users/{userId}",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials",
		http.MethodPatch + " /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret",
		http.MethodPost + " /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key",
		http.MethodDelete + " /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}",
		http.MethodPost + " /api/v1/admin/import",
		http.MethodPost + " /api/v1/admin/snapshots",
		http.MethodPost + " /api/v1/admin/snapshots/prune",
		http.MethodPost + " /api/v1/admin/snapshots/{snapshotId}/restore",
		http.MethodDelete + " /api/v1/admin/snapshots/{snapshotId}",
		http.MethodDelete + " /api/v1/admin/tenants/{tenantId}/knowledge",
		http.MethodPut + " /api/v1/admin/knowledge-access/cross-tenant",
		http.MethodPut + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}",
		http.MethodDelete + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}",
		http.MethodPut + " /api/v1/admin/skill-sources/global",
		http.MethodPut + " /api/v1/admin/skill-sources/tenant/{id}",
		http.MethodDelete + " /api/v1/admin/skill-sources/tenant/{id}",
		http.MethodPut + " /api/v1/admin/skill-sources/user/{email...}",
		http.MethodDelete + " /api/v1/admin/skill-sources/user/{email...}":
		return true
	default:
		return false
	}
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
		responses := map[string]any{
			"200": map[string]any{"description": "Successful response"},
			"400": map[string]any{"description": "Bad request"},
			"401": map[string]any{"description": "Unauthorized"},
			"500": map[string]any{"description": "Internal server error"},
		}
		if route.ResponseContent != "" {
			responses["200"] = map[string]any{
				"description": "Successful response",
				"content": map[string]any{
					route.ResponseContent: map[string]any{
						"schema": map[string]any{"type": "string"},
					},
				},
			}
		}
		op := map[string]any{
			"summary":     route.Summary,
			"operationId": operationID(route.Method, route.Path),
			"responses":   responses,
		}
		role := routeOpenAPIAdminRole(route)
		if role != "" {
			op["x-maclaw-admin-role"] = role
			if role == "owner" {
				responses["403"] = map[string]any{"description": "Forbidden: admin owner role required"}
			}
		}
		description := appendOpenAPIAdminRoleDescription(route.Description, role)
		if description != "" {
			op["description"] = description
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
	case "include_archived", "waiting_for_user", "include_messages", "include_runs", "include_audit", "include_secrets", "overwrite", "dry_run", "all", "confirm", "expired", "expiring":
		return map[string]any{"type": "boolean"}
	case "async":
		return map[string]any{"type": "boolean"}
	case "status":
		switch path {
		case "/api/v1/admin/tenants":
			return map[string]any{"type": "string", "enum": []string{"active", "disabled"}}
		case "/api/v1/admin/tenants/{tenantId}/users":
			return map[string]any{"type": "string", "enum": []string{"active", "disabled"}}
		case "/api/v1/admin/tenants/{tenantId}/users/{userId}/credentials":
			return map[string]any{"type": "string", "enum": []string{"active", "suspended", "revoked"}}
		case "/api/v1/admin/snapshots":
			return map[string]any{"type": "string", "enum": []string{"service", "tenant", "user"}}
		case "/api/v1/jobs":
			return map[string]any{"type": "string", "enum": []string{"pending", "running", "succeeded", "failed", "canceled"}}
		case "/api/v1/instances/{instanceId}/runs":
			return map[string]any{"type": "string", "enum": []string{"running", "succeeded", "failed", "cancelled"}}
		}
	case "severity":
		if path == "/api/v1/admin/security/risk-events" {
			return map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}}
		}
	case "role":
		return map[string]any{"type": "string", "enum": []string{"user", "assistant", "system"}}
	case "response_source":
		return map[string]any{"type": "string", "enum": []string{"ask_user"}}
	case "before", "since", "until":
		if path != "/api/v1/skills" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
	case "q", "tag":
		return map[string]any{"type": "string"}
	case "limit":
		if path == "/api/v1/admin/insights" {
			return map[string]any{"type": "integer", "minimum": 0, "maximum": 50}
		}
		return map[string]any{"type": "integer", "minimum": 1, "maximum": 500}
	case "inactive_for_days":
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 3650}
	case "credential_expiry_window_days":
		return map[string]any{"type": "integer", "minimum": 0, "maximum": 365}
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
	case "since":
		if path == "/api/v1/admin/security/summary" || path == "/api/v1/admin/security/risk-events" {
			return "RFC3339 lower bound. When until is also provided, since must be before or equal to until."
		}
	case "until":
		if path == "/api/v1/admin/security/summary" || path == "/api/v1/admin/security/risk-events" {
			return "RFC3339 upper bound. When since is also provided, until must be after or equal to since."
		}
	case "kind":
		if path == "/api/v1/admin/security/risk-events" {
			return "Stable risk kind, such as auth_failed, sandbox_disabled, sandbox_failed, or insecure_http. Matching is case-insensitive."
		}
	case "status":
		if path == "/api/v1/jobs" {
			return "Bulk delete only accepts terminal statuses."
		}
	case "resource_id":
		return "Filters audit events for a specific resource id, such as a credential, run, user, or tenant id."
	case "actor_type":
		return "Filters audit events by actor type, such as admin, user, credential, system, or anonymous."
	case "actor_user_id":
		return "Filters audit events by actor user id. For Admin Web sessions, this is the admin user id."
	case "actor_tenant_id":
		return "Filters audit events by actor tenant id for tenant-scoped user actors."
	case "scope":
		if path == "/api/v1/admin/snapshots" {
			return "Filters snapshots by scope: service, tenant, or user."
		}
	case "response_source":
		return "Currently only ask_user is supported for filtering."
	case "expired":
		return "When true, only credentials whose expires_at has passed are returned; when false, expired credentials are excluded."
	case "expiring":
		return "When true, only credentials expiring within the default 7-day window are returned; when false, those credentials are excluded."
	case "credential_expiry_window_days":
		return "Number of days ahead to include credential_expiring alerts. Defaults to 7 and is capped at 365."
	case "tag":
		return "Filters flexible structured records by tag."
	case "q":
		return "Case-insensitive text search over structured record title, tags, collection, and JSON data."
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
