package main

import (
	"net/http"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

type openAPIRoute struct {
	Method               string
	Path                 string
	Summary              string
	Description          string
	Tag                  string
	Security             []map[string][]string
	QueryParams          []string
	HeaderParams         []string
	ResponseContent      string
	ResponseContentTypes []string
	RequestContentTypes  []string
	AdminRole            string
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
	{Method: http.MethodPost, Path: "/api/v1/admin/auth/reveal-admin-secret", Summary: "Reveal root admin secret", Description: "Returns MACLAW_ADMIN_SECRET for external platform configuration after an active owner account session re-enters its password. Root admin-secret authentication is not accepted for this endpoint, and the reveal is audited.", Tag: "admin-auth", Security: adminSecurity()},
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
	{Method: http.MethodGet, Path: "/api/platform/runtime/report", Summary: "Platform runtime report", Description: "Returns VE Platform-compatible MaClawSrv runtime health, users, instances, and summary counters.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/virtual-employees", Summary: "Create VE Platform runtime", Description: "Creates or reuses the MaClawSrv tenant, user, user LLM config, SSH host labels, and instance for one VE Platform virtual employee.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/virtual-employees/{employeeId}/config", Summary: "Update VE Platform user config", Description: "Updates the shared MaClawSrv user IM and third-party access settings for one VE Platform virtual employee without changing LLM or instance state.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/platform/virtual-employees/{employeeId}", Summary: "Delete VE Platform runtime", Description: "Deletes the MaClawSrv instance for one VE Platform virtual employee, and removes the managed runtime user when no other instances remain.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/source-users/runtime-status", Summary: "Batch source-user runtime status", Description: "Returns per-source-user web assistant instance counts and shared user config validation for VE Platform tenant user rows.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/platform/source-users/{sourceUserId}/runtime-status", Summary: "Source-user runtime status", Description: "Returns web assistant readiness, instance counts, and shared user config validation for one VE Platform source user.", Tag: "platform", Security: adminSecurity(), QueryParams: []string{"tenant_id"}},
	{Method: http.MethodGet, Path: "/api/platform/source-users/{sourceUserId}/assistant-instances", Summary: "List source-user assistant instances", Description: "Lists MaClawSrv web assistant instances for one VE Platform source user. All instances share the source user's config, tools, knowledge, memory, and security settings.", Tag: "platform", Security: adminSecurity(), QueryParams: []string{"tenant_id"}},
	{Method: http.MethodPost, Path: "/api/platform/source-users/{sourceUserId}/assistant-instances", Summary: "Create source-user assistant instance", Description: "Creates another web assistant instance under the same MaClawSrv user for one VE Platform source user. Optional ssh_hosts updates the user's shared SSH host labels; an empty array clears them.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/source-users/{sourceUserId}/assistant-link", Summary: "Create source-user assistant launch link", Description: "Creates a short-lived one-time launch URL for a source user's web AI assistant, optionally bound to an existing assistant instance. Optional ssh_hosts updates the user's shared SSH host labels; an empty array clears them.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/source-users/{sourceUserId}/knowledge-link", Summary: "Create source-user knowledge launch link", Description: "Creates a short-lived one-time launch URL for the source user's MaClawSrv knowledge base page.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/source-users/{sourceUserId}/settings-link", Summary: "Create source-user settings launch link", Description: "Creates a short-lived one-time launch URL for the source user's shared system settings page. Optional ssh_hosts updates the user's shared SSH host labels; an empty array clears them.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/virtual-employees/{employeeId}/knowledge/imports", Summary: "Import platform knowledge", Description: "Accepts a VE Platform knowledge import handoff for a virtual employee runtime.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/virtual-employees/{employeeId}/migrations/imports", Summary: "Import platform migration", Description: "Accepts a VE Platform migration handoff for a virtual employee runtime.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/sync/jobs/{jobId}/run", Summary: "Run platform sync job", Description: "Runs a VE Platform sync job handoff and returns conflict and cursor metadata.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/platform/sync/conflicts/{conflictId}/resolve", Summary: "Resolve platform sync conflict", Description: "Applies a VE Platform conflict resolution handoff to the virtual employee runtime.", Tag: "platform", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/security/summary", Summary: "Admin security summary", Description: "Returns current admin security posture, generated_at, applied filters, risk severity counts, risk kind counts, and recent risk events derived from audit entries and service posture. Risk events derived from audit entries redact sensitive metadata and path-like resource ids. When both since and until are provided, since must be before or equal to until.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"since", "until"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/security/risk-events", Summary: "Admin security risk events", Description: "Returns security-relevant risk events with generated_at, applied filters, severity counts, and kind counts derived from audit entries and service posture, including auth failures, sandbox failures, disabled sandbox, and insecure HTTP posture. Risk events derived from audit entries redact sensitive metadata and path-like resource ids. The severity filter accepts high, medium, or low case-insensitively; kind filters by stable risk kind case-insensitively; when both since and until are provided, since must be before or equal to until.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"severity", "kind", "since", "until", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/status", Summary: "Admin runtime status", Description: "Returns process, readiness, scheduler, job, sandbox, and log source status for Admin Web runtime pages.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/runtime/gc", Summary: "Run runtime GC", Description: "Forces Go garbage collection and returns before/after memory counters for Admin Web diagnostics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/goroutines", Summary: "Runtime goroutine dump", Description: "Returns a redacted text/plain Go goroutine profile for deadlock and leak diagnostics. Set download=true to include an attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"debug", "download"}, ResponseContent: "text/plain"},
	{Method: http.MethodGet, Path: "/api/v1/admin/runtime/profiles/{profileName}", Summary: "Runtime text profile", Description: "Returns a redacted text/plain Go runtime profile for heap, allocs, block, mutex, or threadcreate diagnostics. Only debug=1 or debug=2 text output is supported; heap/allocs may set gc=true first; download=true adds an attachment filename.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"debug", "gc", "download"}, ResponseContent: "text/plain"},
	{Method: http.MethodGet, Path: "/api/v1/admin/scheduler/status", Summary: "Scheduler status", Description: "Returns scheduler enablement and persisted scheduled task rollups, including delivery push metrics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/scheduler/delivery-targets", Summary: "Scheduler delivery targets", Description: "Lists IM push destinations for a channel (lansenger groups, weixin/telegram/qq self/last peers). Query: channel, query.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"channel", "query", "q"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/scheduler/delivery-audit", Summary: "Scheduler delivery audit", Description: "Recent IM push attempts (newest first). Query: limit.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/scheduler/tasks", Summary: "List scheduled tasks", Description: "Live task list from the running scheduler manager.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/scheduler/tasks", Summary: "Create scheduled task", Description: "Creates a scheduled task (owner admin). Supports delivery object or shorthand group_id/group_name/user_id/channel.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/admin/scheduler/tasks/{taskId}", Summary: "Update scheduled task", Description: "Partial update of a scheduled task (owner admin).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/scheduler/tasks/{taskId}", Summary: "Delete scheduled task", Description: "Deletes a scheduled task (owner admin).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/scheduler/tasks/{taskId}/trigger", Summary: "Trigger scheduled task", Description: "Runs a task immediately (owner admin).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/scheduler/tasks/{taskId}/pause", Summary: "Pause scheduled task", Description: "Pauses a task (owner admin).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/scheduler/tasks/{taskId}/resume", Summary: "Resume scheduled task", Description: "Resumes a paused task (owner admin).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/jobs", Summary: "List admin jobs", Description: "Lists async jobs across tenants for Admin Web operations diagnostics.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "user_id", "kind", "status", "limit"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/jobs/{jobId}", Summary: "Get admin job", Description: "Returns one async job across tenants for Admin Web progress polling.", Tag: "admin", Security: adminSecurity()},
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
	{Method: http.MethodGet, Path: "/api/v1/admin/client-config/schema", Summary: "Client config schema", Description: "Returns user-client configuration metadata for Admin Web default client settings.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/client-config/default", Summary: "Shared client config", Description: "Returns the redacted shared MaClawSrv client configuration applied to all users at runtime.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/client-config/default", Summary: "Update shared client config", Description: "Validates and persists the shared MaClawSrv client configuration applied to all users at runtime. Redacted sensitive placeholders preserve existing shared secrets when available.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/client-config/default/validate", Summary: "Validate shared client config", Description: "Validates a submitted shared MaClawSrv client configuration without persisting it.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/ai-models/status", Summary: "Admin AI model status", Description: "Returns server-wide shared AI model readiness for embedding, ASR, and TTS using the shared default client configuration, including full local model paths for administrator verification.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/ai-models/{model}/download", Summary: "Admin download AI model", Description: "Starts or resumes a server-wide shared AI model download for embedding, ASR, or TTS. OminiParser is intentionally unsupported in MaClawSrv.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/ai-models/embedding/embed", Summary: "Admin test embedding model", Description: "Uses the shared server-wide embedding model to embed supplied text and returns the full vector, dimension, and L2 norm for admin verification.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/ai-models/asr/transcribe", Summary: "Admin test ASR model", Description: "Uses the shared server-wide ASR model to transcribe uploaded audio for admin verification. WAV, OGG/Opus, Silk, and MP3 inputs are normalized to 16kHz mono WAV before ASR using native Go decoders. M4A and AAC are accepted as uploads but currently rejected with a native-decoder-not-supported error because no native decoder is wired.", Tag: "admin", Security: adminSecurity(), HeaderParams: []string{"X-MaClaw-Audio-Format"}, RequestContentTypes: []string{"application/json", "audio/wav", "audio/ogg", "audio/opus", "audio/mpeg", "audio/mp4", "audio/aac", "application/octet-stream"}},
	{Method: http.MethodPost, Path: "/api/v1/admin/ai-models/tts/synthesize", Summary: "Admin test TTS model", Description: "Uses the shared server-wide TTS model to synthesize supplied text and returns a downloadable WAV file for admin verification.", Tag: "admin", Security: adminSecurity(), ResponseContentTypes: []string{"audio/wav"}},
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
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/config/schema", Summary: "User config schema", Description: "Returns all AppConfig fields that can be edited for one user.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/config", Summary: "Get user config", Description: "Returns one user's sanitized assistant configuration.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/config", Summary: "Update user config", Description: "Updates one user's assistant configuration and refreshes instance readiness so changes take effect.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/config/validate", Summary: "Validate user config", Description: "Validates a candidate configuration for one user without saving it.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/config/test", Summary: "Test user config", Description: "Tests a candidate configuration for one user without saving it.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/dynamic-capabilities/mcp/{serverId}/{toolName}", Summary: "Publish observed MCP capability contract", Description: "Owner-only control-plane endpoint. Requires confirm=true and binds a reviewed capability declaration to the exact ready MCP schema observed by the service; provider metadata and caller-supplied binding digests are not accepted.", Tag: "admin", Security: adminSecurity(), AdminRole: "owner"},
	{Method: http.MethodPost, Path: "/api/v1/admin/tenants/{tenantId}/users/{userId}/dynamic-capabilities/skills/{stableId}", Summary: "Publish observed Skill capability contract", Description: "Owner-only control-plane endpoint. Requires confirm=true and binds a reviewed capability declaration to the installed Skill stable identity, version, and content observed by the service; caller-supplied binding digests are not accepted.", Tag: "admin", Security: adminSecurity(), AdminRole: "owner"},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge/stats", Summary: "Admin knowledge stats", Description: "Returns service-wide knowledge store statistics for Admin Web diagnostics.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge/sources", Summary: "List all knowledge sources", Description: "Lists knowledge sources across tenants and users for Admin Web inspection.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/public-knowledge-libraries", Summary: "List public knowledge libraries", Description: "Lists named public knowledge libraries that users may be granted access to.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/public-knowledge-libraries", Summary: "Create public knowledge library", Description: "Creates a named public knowledge library in a tenant.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/public-knowledge-libraries/{libraryId}", Summary: "Delete public knowledge library", Description: "Deletes a public knowledge library and its sources.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/public-knowledge-libraries/{libraryId}/sources", Summary: "List public knowledge library sources", Description: "Lists knowledge sources inside one named public knowledge library.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/text", Summary: "Import public knowledge text", Description: "Imports text into a named public knowledge library.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/file", Summary: "Import public knowledge files", Description: "Imports one or more documents or ZIP/RAR document archives into a named public knowledge library as an async job.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/urls", Summary: "Import public knowledge URLs", Description: "Imports URLs into a named public knowledge library, optionally crawling to a configured depth.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/tenants/{tenantId}/knowledge", Summary: "Clear tenant knowledge", Description: "Deletes knowledge sources for one tenant after confirm=true is explicitly provided. This is an irreversible admin cleanup operation.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/cross-tenant", Summary: "Knowledge cross-tenant access", Description: "Returns whether administrators may configure cross-tenant readable knowledge scopes.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/knowledge-access/cross-tenant", Summary: "Update knowledge cross-tenant access", Description: "Enables or disables admin configuration of cross-tenant readable knowledge scopes.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Get knowledge access", Description: "Returns the configured additional readable knowledge scopes for one user. The user's own scope is implicit.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Update knowledge access", Description: "Configures additional readable knowledge scopes for one user. Same-tenant scopes are allowed by default; cross-tenant scopes require the global cross-tenant switch.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", Summary: "Delete knowledge access", Description: "Deletes the configured additional readable knowledge scopes for one user.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}", Summary: "Attach public knowledge library", Description: "Grants one user read access to a named public knowledge library.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}", Summary: "Detach public knowledge library", Description: "Removes one user's read access to a named public knowledge library.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/resolve", Summary: "Resolve knowledge access", Description: "Returns the effective readable knowledge scopes for one user, including the implicit self scope and cross-tenant filtering.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/available", Summary: "Available skill sources", Description: "Returns the canonical list of skill source identifiers that can be allowed or blocked by admin policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/global", Summary: "Get global skill source policy", Description: "Returns the global service-wide skill source allow policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/global", Summary: "Update global skill source policy", Description: "Updates the global service-wide skill source allow policy.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Get tenant skill source policy", Description: "Returns one tenant-level skill source override.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Update tenant skill source policy", Description: "Updates one tenant-level skill source override.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/skill-sources/tenant/{id}", Summary: "Delete tenant skill source policy", Description: "Deletes one tenant-level skill source override so the global policy applies.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}", Summary: "Get tenant user skill source policy", Description: "Returns one tenant-scoped user skill source override by runtime user ID.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}", Summary: "Update tenant user skill source policy", Description: "Updates one tenant-scoped user skill source override by runtime user ID.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}", Summary: "Delete tenant user skill source policy", Description: "Deletes one tenant-scoped user skill source override so tenant or global policy applies.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}/resolve", Summary: "Resolve tenant user skill source policy", Description: "Returns effective allowed skill sources for one runtime user after tenant-user, tenant, and global policy are applied.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/auth/token", Summary: "Issue bearer token", Description: "Exchanges tenant user API key and secret for a bearer token.", Tag: "auth"},
	{Method: http.MethodPost, Path: "/api/v1/web/exchange", Summary: "Exchange web launch token", Description: "Consumes a one-time VE Platform web launch token and returns a short-lived bearer token for the user web app.", Tag: "auth"},
	{Method: http.MethodGet, Path: "/api/v1/me", Summary: "Current principal", Tag: "auth", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config/schema", Summary: "Config schema", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/config", Summary: "Get config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/config", Summary: "Update config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/validate", Summary: "Validate config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/config/test", Summary: "Test config", Tag: "config", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/ai-models/status", Summary: "AI model status", Description: "Returns shared server-side AI model readiness for embedding, ASR, and TTS without exposing local model paths.", Tag: "ai-models", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/ai-models/{model}/download", Summary: "Download AI model", Description: "Starts or resumes a server-wide shared AI model download for embedding, ASR, or TTS. OminiParser is intentionally unsupported in MaClawSrv.", Tag: "ai-models", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/ai-models/asr/transcribe", Summary: "Transcribe audio", Description: "Uses the shared ASR model to transcribe uploaded audio for the authenticated user's agent composer. WAV, OGG/Opus, Silk, and MP3 inputs are normalized to 16kHz mono WAV before ASR using native Go decoders. M4A and AAC are accepted as uploads but currently rejected with a native-decoder-not-supported error because no native decoder is wired. For application/octet-stream, send X-MaClaw-Audio-Format with wav, ogg, opus, silk, mp3, m4a, or aac when magic-byte detection is not enough. The model is lazy-loaded once per server process.", Tag: "ai-models", Security: bearerSecurity(), HeaderParams: []string{"X-MaClaw-Audio-Format"}, RequestContentTypes: []string{"application/json", "audio/wav", "audio/ogg", "audio/opus", "audio/mpeg", "audio/mp4", "audio/aac", "application/octet-stream"}},
	{Method: http.MethodPost, Path: "/api/v1/ai-models/tts/synthesize", Summary: "Synthesize assistant speech", Description: "Uses the shared TTS model to synthesize text as audio for assistant response playback. Request format=mp3 to receive audio/mpeg; otherwise WAV is returned. IM auto voice replies synthesize WAV first, then convert to an MP3 file with the built-in pure Go shine-mp3 encoder using the MaClaw GUI-compatible flow.", Tag: "ai-models", Security: bearerSecurity(), ResponseContentTypes: []string{"audio/wav", "audio/mpeg"}},
	{Method: http.MethodPost, Path: "/api/v1/im/weixin/qr/start", Summary: "Start WeChat QR binding", Description: "Starts an iLink/personal WeChat QR binding flow for the authenticated MaClawSrv user only and returns the original QR image URL, same-origin QR image proxy URL, plus opaque QR token.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/im/weixin/qr/image", Summary: "Render WeChat QR image", Description: "Renders a WeChat QR payload as a same-origin PNG image. Legacy validated remote image proxying remains available for url query values.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"value", "url"}},
	{Method: http.MethodPost, Path: "/api/v1/im/weixin/qr/poll", Summary: "Poll WeChat QR binding", Description: "Polls one iLink/personal WeChat QR binding token for the authenticated MaClawSrv user only. On confirmed login, saves WeChat credentials into that user's isolated config.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/im/qqbot/qr/start", Summary: "Start QQ Bot QR binding", Description: "Starts a QQ Bot scan-bind flow for the authenticated MaClawSrv user only and returns the connect URL, same-origin QR image URL, plus opaque QR token.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/im/qqbot/qr/image", Summary: "Render QQ Bot QR image", Description: "Renders a QQ Bot bind URL as a same-origin PNG image.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"value"}},
	{Method: http.MethodPost, Path: "/api/v1/im/qqbot/qr/poll", Summary: "Poll QQ Bot QR binding", Description: "Polls one QQ Bot scan-bind token for the authenticated MaClawSrv user only. On confirmed login, saves AppID and AppSecret into that user's isolated config.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/memory", Summary: "List memory", Description: "Lists current user's long-term memory entries with optional category, text search, limit, and offset pagination. Protected self-identity entries are read-only.", Tag: "memory", Security: bearerSecurity(), QueryParams: []string{"category", "q", "limit", "offset"}},
	{Method: http.MethodPost, Path: "/api/v1/memory", Summary: "Create memory", Description: "Creates a manual long-term memory entry for current user. Protected self-identity memory cannot be created here.", Tag: "memory", Security: bearerSecurity()},
	{Method: http.MethodPut, Path: "/api/v1/memory/{id}", Summary: "Update memory", Description: "Updates one current-user memory entry by id. Protected self-identity memory is read-only.", Tag: "memory", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/memory/{id}", Summary: "Delete memory", Description: "Deletes one current-user memory entry by id. Protected self-identity memory is read-only.", Tag: "memory", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/text", Summary: "Import knowledge text", Description: "Saves user-provided text into the current user's knowledge base.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/file", Summary: "Import knowledge files", Description: "Uploads one or more files and imports them into the current user's knowledge base as an async job.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/image", Summary: "Import knowledge image", Description: "Uploads one image, indexes its OCR/visual description for knowledge search, and returns source_ids, asset_ids, and authenticated media URLs for immediate display.", Tag: "knowledge", Security: bearerSecurity(), RequestContentTypes: []string{"multipart/form-data"}},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/capabilities", Summary: "Knowledge retrieval capabilities", Description: "Returns the available knowledge retrieval contract, including image-search modes, indexed evidence, endpoint, and agent tool name. The current built-in route supports text-to-image retrieval through OCR/caption/context; image-to-image is explicitly unavailable until a shared multimodal encoder is configured.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/url", Summary: "Import knowledge URL", Description: "Imports one URL into the current user's knowledge base.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/urls", Summary: "Import knowledge URLs", Description: "Imports one or more URLs, optionally crawling discovered links up to max_depth.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/export", Summary: "Export knowledge package", Description: "Exports all or selected current-user own knowledge metadata into an editable MaClaw knowledge JSON package. A description is required for future Hub sharing.", Tag: "knowledge", Security: bearerSecurity(), ResponseContentTypes: []string{"application/json"}},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/package", Summary: "Import knowledge package", Description: "Imports a MaClaw editable knowledge JSON package into the current user's own knowledge base. URL entries are refetched; text entries require a content field.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/import/share", Summary: "Import shared knowledge", Description: "Imports shared knowledge by knowledge ID or by a human-readable, agent-importable share link after the host resolves the Hub and permissions. Private, tenant, or user-list shares can pass hub_token or authorization in the JSON body.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/import/batches", Summary: "List own knowledge import batches", Description: "Lists import batches for the current user's own knowledge base with page and limit pagination.", Tag: "knowledge", Security: bearerSecurity(), QueryParams: []string{"page", "limit"}},
	{Method: http.MethodDelete, Path: "/api/v1/knowledge/import/batches/{batchId}", Summary: "Delete own knowledge import batch", Description: "Deletes one current-user knowledge import batch, including sources imported by that batch.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/import/jobs/{jobId}", Summary: "Knowledge import job", Description: "Returns status and result for a user knowledge import job.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/search", Summary: "Search knowledge", Description: "Searches the current user's own knowledge plus effective readable knowledge scopes, and merges Hub-synced enterprise digital-asset hits (access_state=active only) from the user's local enterprise cache. Tenant and owner are enforced from the authenticated principal.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/knowledge/images/search", Summary: "Search knowledge images", Description: "Searches image OCR, vision captions, filenames, and document context across the authenticated user's readable knowledge scopes. Returns only image nodes with authenticated thumbnail, preview, and original URLs. This is text-to-image retrieval; image-to-image retrieval is not available until a shared multimodal embedding model is configured.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/images/{assetId}/thumbnail", Summary: "Get knowledge image thumbnail", Description: "Returns an authenticated thumbnail for a standalone or document-embedded knowledge image asset.", Tag: "knowledge", Security: bearerSecurity(), ResponseContentTypes: []string{"image/jpeg"}},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/images/{assetId}/preview", Summary: "Get knowledge image preview", Description: "Returns an authenticated preview for a standalone or document-embedded knowledge image asset.", Tag: "knowledge", Security: bearerSecurity(), ResponseContentTypes: []string{"image/jpeg"}},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/images/{assetId}", Summary: "Get knowledge image original", Description: "Returns an authenticated original knowledge image asset.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/knowledge/access", Summary: "Effective knowledge access", Description: "Returns the current user's effective readable knowledge bases. Own knowledge is included by default. Each scope includes scope_type (self, public, or user) and display fields such as tenant_name and owner_name when available; other users' email addresses are not exposed.", Tag: "knowledge", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/knowledge", Summary: "Clear own knowledge", Description: "Clears all sources in the current user's own knowledge base. Requires confirm=true and administrator password or Admin Secret in the request body.", Tag: "knowledge", Security: bearerSecurity(), QueryParams: []string{"confirm"}},
	// Enterprise digital assets (Hub→local one-way cache per user data dir).
	{Method: http.MethodGet, Path: "/api/v1/enterprise-knowledge/libraries", Summary: "List enterprise libraries", Description: "Lists non-revoked Hub-synced enterprise digital-asset libraries cached for the authenticated user, including access_state and local user_sync_enabled preference.", Tag: "enterprise-knowledge", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/enterprise-knowledge/sync/status", Summary: "Enterprise sync status", Description: "Returns the user's enterprise library list, Hub credential posture (hub_configured / hub_url without secrets), and process-wide coordinator status.", Tag: "enterprise-knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/enterprise-knowledge/sync/now", Summary: "Sync enterprise libraries now", Description: "Runs one Hub→local pull cycle for the authenticated user. Requires RemoteHubURL and RemoteViewerToken on the user config. Returns 409 when a cycle is already in progress.", Tag: "enterprise-knowledge", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/enterprise-knowledge/libraries/{libraryId}/user-sync", Summary: "Set enterprise library user sync", Description: "Enables or disables Hub pull for one enterprise library on this device. Body: {\"enabled\": true|false}. Does not delete local cache.", Tag: "enterprise-knowledge", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/enterprise-knowledge/libraries/{libraryId}", Summary: "Purge enterprise library cache", Description: "Deletes the local enterprise digital-asset cache for one library for the authenticated user. Requires confirm=true. Irreversible until the next Hub sync re-pulls content.", Tag: "enterprise-knowledge", Security: bearerSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/admin/enterprise-knowledge/sync/status", Summary: "Admin enterprise sync coordinator status", Description: "Returns process-wide enterprise digital-asset sync coordinator status (enabled, running, last run, last errors, device id).", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/admin/enterprise-knowledge/sync/now", Summary: "Admin enterprise sync all users", Description: "Runs one Hub→local enterprise sync cycle for all active users with Hub credentials. Returns 409 when a cycle is already in progress. Disable with MACLAW_ENTERPRISE_SYNC_DISABLED=true.", Tag: "admin", Security: adminSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/admin/enterprise-knowledge/tenants", Summary: "Enterprise sync progress by tenant", Description: "Rolls up per-tenant enterprise cache state: hub-configured users, library counts, active/sync_disabled/revoked, and optional per-user detail. Query: tenant_id, include_users=true.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"tenant_id", "include_users"}},
	{Method: http.MethodDelete, Path: "/api/v1/admin/enterprise-knowledge/tenants/{tenantId}/users/{userId}/libraries/{libraryId}", Summary: "Admin purge user enterprise library", Description: "Deletes one enterprise library local cache for a tenant user. Requires confirm=true. Owner role or root admin secret required.", Tag: "admin", Security: adminSecurity(), QueryParams: []string{"confirm"}},
	{Method: http.MethodGet, Path: "/api/v1/usage/summary", Summary: "Usage summary", Tag: "usage", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/mcp/servers", Summary: "List MCP servers", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/mcp/market", Summary: "Search MCP marketplace", Tag: "mcp", Security: bearerSecurity(), QueryParams: []string{"q"}},
	{Method: http.MethodPost, Path: "/api/v1/mcp/market/install", Summary: "Install MCP capability", Tag: "mcp", Security: bearerSecurity()},
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
	{Method: http.MethodGet, Path: "/api/v1/im/weixin/status", Summary: "WeChat runtime status", Description: "Returns this user's WeChat binding and actual MaClawSrv gateway runtime state.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/im/weixin/restart", Summary: "Restart WeChat runtime", Description: "Restarts this user's bound WeChat gateway runtime after saving IM settings.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/im-gateway/v1/health", Summary: "Third-party IM gateway health", Description: "Returns MaClawSrv third-party IM gateway protocol health.", Tag: "messages"},
	{Method: http.MethodPost, Path: "/api/v1/device-pairings", Summary: "Create hardware-device pairing code", Description: "Creates a one-time six-digit code, valid for thirty minutes, for a screenless hardware client. The code is never a bearer credential.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/hardware-devices", Summary: "List hardware devices", Description: "Lists authenticated user's paired hardware device assistant bindings, effective TTS voices, and runtime status.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/hardware-devices/tts-voices", Summary: "List hardware TTS voices", Description: "Lists bundled voices available for per-device TTS selection.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/hardware-devices/experts", Summary: "List hardware AI experts", Description: "Lists server-side AI expert snapshots selectable by the authenticated user's hardware devices.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/hardware-devices/experts", Summary: "Upsert hardware AI expert", Description: "Stores a server-side expert snapshot for a user's hardware devices.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/hardware-devices/experts/{expertId}", Summary: "Delete hardware AI expert", Description: "Deletes a selectable expert snapshot. Devices still bound to it become degraded until reconfigured.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/hardware-devices/{deviceId}", Summary: "Get hardware device", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPatch, Path: "/api/v1/hardware-devices/{deviceId}/agent-binding", Summary: "Update hardware assistant binding", Description: "Sets general/expert assistant mode, initial prompt, and a device-specific TTS voice.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodDelete, Path: "/api/v1/hardware-devices/{deviceId}", Summary: "Delete hardware device", Description: "Removes a device agent binding and its dedicated assistant instance.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/device-gateway/v1/pair", Summary: "Pair hardware device by code", Description: "Consumes a six-digit one-time pairing code and returns the user's third-party gateway credential. Intended only for devices that can enter the code directly.", Tag: "messages"},
	{Method: http.MethodPost, Path: "/api/device-gateway/v1/pair/voice", Summary: "Pair hardware device by spoken code", Description: "Consumes WAV audio of a six-digit one-time pairing code and returns the user's third-party gateway credential. Requires HTTPS and is rate limited by source IP.", Tag: "messages"},
	{Method: http.MethodPost, Path: "/api/im-gateway/v1/handshake", Summary: "Third-party IM gateway handshake", Description: "Authenticates a third-party client with the user's unique gateway bearer token and returns protocol capabilities.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/im-gateway/v1/incoming", Summary: "Third-party IM incoming message", Description: "Accepts third-party IM user messages and routes them to the MaClawSrv user selected by the gateway bearer token. message.type supports text, image, file, voice, and audio; media clients should first request server-owned upload/download URLs through /api/im-gateway/v1/media/upload-url, upload bytes to upload.url, then reference the returned media id or object in message.id or message.attachments[]. The client must not provide its own download URL for the server to fetch.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/im-gateway/v1/outgoing", Summary: "Third-party IM outgoing poll", Description: "Long-polls assistant replies for a third-party IM client authenticated by the gateway bearer token.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"clientId", "cursor", "limit", "timeout"}},
	{Method: http.MethodPost, Path: "/api/im-gateway/v1/ack", Summary: "Third-party IM delivery ack", Description: "Acknowledges delivery of third-party IM outgoing messages for the user selected by the gateway bearer token.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/im-gateway/v1/tool-result", Summary: "Third-party IM client tool result", Description: "Accepts completion results for tool_call or tool_plan messages executed by a third-party client. /ack only means the client received a tool request; this endpoint reports execution success, error, rejection, cancellation, or timeout so the agent can see the result.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/im-gateway/v1/media/upload-url", Summary: "Third-party IM media upload URL", Description: "Creates a server-owned media object and returns upload/download URLs for image, file, and voice attachments. Requires the user's third-party gateway bearer token.", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPut, Path: "/api/im-gateway/v1/media/{mediaId}/upload", Summary: "Third-party IM media upload", Description: "Uploads attachment bytes to a previously issued server-owned media upload URL. This endpoint is protected by the mediaToken embedded in the upload URL or supplied as X-Media-Token.", Tag: "messages"},
	{Method: http.MethodGet, Path: "/api/im-gateway/v1/media/{mediaId}", Summary: "Third-party IM media download", Description: "Downloads bytes from a server-owned third-party media URL. This endpoint is protected by the mediaToken embedded in the download URL or supplied as X-Media-Token.", Tag: "messages"},
	{Method: http.MethodGet, Path: "/api/v1/im-audit/contacts", Summary: "List IM contacts", Description: "Lists IM contacts visible to the authenticated MaClawSrv user only, optionally filtered by platform.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"platform"}},
	{Method: http.MethodGet, Path: "/api/v1/im-audit/messages", Summary: "List IM history", Description: "Lists IM monitor/history messages visible to the authenticated MaClawSrv user only. Filters are applied within that tenant/user scope.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"platform", "contact", "q", "role", "since", "until", "limit", "before"}},
	{Method: http.MethodGet, Path: "/api/v1/im-audit/stats", Summary: "IM history stats", Description: "Returns IM monitor/history counts visible to the authenticated MaClawSrv user only.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"platform", "contact", "q", "role", "since", "until"}},
	{Method: http.MethodGet, Path: "/api/v1/im-audit/export.csv", Summary: "Export IM history CSV", Description: "Exports filtered IM monitor/history messages for the authenticated MaClawSrv user only.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"platform", "contact", "q", "role", "since", "until"}},
	{Method: http.MethodDelete, Path: "/api/v1/im-audit/messages", Summary: "Clean IM history", Description: "Deletes filtered IM monitor/history messages before the supplied timestamp for the authenticated MaClawSrv user only. Requires confirm=true.", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"platform", "contact", "q", "role", "since", "until", "before", "confirm"}},
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
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/coding-runtime/{taskId}/recovery", Summary: "Review coding runtime recovery", Description: "Performs a fresh read-only workspace probe for an interrupted coding attempt. Remote recovery requires the same configured pinned target and an already-verified live SSH session; this endpoint never reconnects or replays commands.", Tag: "coding-runtime", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/coding-runtime/{taskId}/recovery", Summary: "Confirm coding runtime recovery", Description: "Records a human recovery decision after a matching fresh review. Confirmation queues the stable task but never reruns a model, tool call, or previous command.", Tag: "coding-runtime", Security: bearerSecurity()},
	{Method: http.MethodGet, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/messages", Summary: "List messages", Tag: "messages", Security: bearerSecurity(), QueryParams: []string{"role", "since", "until", "limit", "before"}},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/messages", Summary: "Post message", Tag: "messages", Security: bearerSecurity()},
	{Method: http.MethodPost, Path: "/api/v1/instances/{instanceId}/sessions/{sessionId}/coding-runtime/remote", Summary: "Start remote coding runtime", Description: "Starts an explicit remote Workflow implementation attempt. The request supplies only a configured SSH host label and absolute remote work directory; MaClawSrv resolves the canonical host/user/port and pinned host key from the authenticated user's configuration. A previously verified live SSH session is required; no connection or replay is attempted.", Tag: "coding-runtime", Security: bearerSecurity()},
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
		http.MethodPost + " /api/v1/admin/auth/reveal-admin-secret",
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
		http.MethodPut + " /api/v1/admin/client-config/default",
		http.MethodPost + " /api/v1/admin/ai-models/{model}/download",
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
		http.MethodPut + " /api/v1/admin/tenants/{tenantId}/users/{userId}/config",
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
		http.MethodPost + " /api/v1/admin/public-knowledge-libraries",
		http.MethodDelete + " /api/v1/admin/public-knowledge-libraries/{libraryId}",
		http.MethodPost + " /api/v1/admin/public-knowledge-libraries/{libraryId}/import/text",
		http.MethodPost + " /api/v1/admin/public-knowledge-libraries/{libraryId}/import/file",
		http.MethodPost + " /api/v1/admin/public-knowledge-libraries/{libraryId}/import/urls",
		http.MethodDelete + " /api/v1/admin/tenants/{tenantId}/knowledge",
		http.MethodPut + " /api/v1/admin/knowledge-access/cross-tenant",
		http.MethodPut + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}",
		http.MethodDelete + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}",
		http.MethodPost + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}",
		http.MethodDelete + " /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/public-libraries/{libraryId}",
		http.MethodPut + " /api/v1/admin/skill-sources/global",
		http.MethodPut + " /api/v1/admin/skill-sources/tenant/{id}",
		http.MethodDelete + " /api/v1/admin/skill-sources/tenant/{id}",
		http.MethodPut + " /api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}",
		http.MethodDelete + " /api/v1/admin/skill-sources/tenants/{tenantId}/users/{userId}",
		http.MethodDelete + " /api/v1/admin/enterprise-knowledge/tenants/{tenantId}/users/{userId}/libraries/{libraryId}":
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

func routeResponseContentTypes(route openAPIRoute) []string {
	if len(route.ResponseContentTypes) > 0 {
		return route.ResponseContentTypes
	}
	if strings.TrimSpace(route.ResponseContent) != "" {
		return []string{route.ResponseContent}
	}
	return nil
}

func routeRequestContentTypes(route openAPIRoute) []string {
	if len(route.RequestContentTypes) > 0 {
		return route.RequestContentTypes
	}
	return []string{"application/json"}
}

func openAPIContentSchema(contentType string) map[string]any {
	if strings.HasPrefix(contentType, "audio/") || contentType == "application/octet-stream" {
		return map[string]any{"type": "string", "format": "binary"}
	}
	return map[string]any{"type": "string"}
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
		if responseTypes := routeResponseContentTypes(route); len(responseTypes) > 0 {
			content := map[string]any{}
			for _, contentType := range responseTypes {
				content[contentType] = map[string]any{
					"schema": openAPIContentSchema(contentType),
				}
			}
			responses["200"] = map[string]any{
				"description": "Successful response",
				"content":     content,
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
		if params := buildOpenAPIParameters(route.Path, route.QueryParams, route.HeaderParams); len(params) > 0 {
			op["parameters"] = params
		}
		if route.Method == http.MethodPost || route.Method == http.MethodPut || route.Method == http.MethodPatch {
			contentTypes := routeRequestContentTypes(route)
			content := map[string]any{}
			for _, contentType := range contentTypes {
				content[contentType] = map[string]any{
					"schema": map[string]any{"type": "object"},
				}
				if strings.HasPrefix(contentType, "audio/") || contentType == "application/octet-stream" {
					content[contentType] = map[string]any{
						"schema": map[string]any{"type": "string", "format": "binary"},
					}
				}
			}
			op["requestBody"] = map[string]any{
				"required": false,
				"content":  content,
			}
		}
		if body := openAPIFileImportRequestBody(route.Path); body != nil {
			op["requestBody"] = body
		}
		if body := openAPIMemoryRequestBody(route.Method, route.Path); body != nil {
			op["requestBody"] = body
		}
		if body := openAPIKnowledgeClearRequestBody(route.Method, route.Path); body != nil {
			op["requestBody"] = body
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

func buildOpenAPIParameters(path string, queryParams, headerParams []string) []map[string]any {
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
	for _, name := range headerParams {
		params = append(params, map[string]any{
			"name":        name,
			"in":          "header",
			"required":    false,
			"schema":      openAPIHeaderSchema(path, name),
			"description": openAPIHeaderDescription(path, name),
		})
	}
	return params
}

func openAPIHeaderSchema(path, name string) map[string]any {
	if isOpenAPIASRTranscribePath(path) && strings.EqualFold(name, "X-MaClaw-Audio-Format") {
		return map[string]any{"type": "string", "enum": []string{"wav", "ogg", "opus", "silk", "mp3", "m4a", "aac"}}
	}
	return map[string]any{"type": "string"}
}

func openAPIHeaderDescription(path, name string) string {
	if isOpenAPIASRTranscribePath(path) && strings.EqualFold(name, "X-MaClaw-Audio-Format") {
		return "Optional source audio format hint. M4A/AAC are accepted but return native-decoder-not-supported until a native decoder is wired."
	}
	return ""
}

func isOpenAPIASRTranscribePath(path string) bool {
	switch path {
	case "/api/v1/ai-models/asr/transcribe", "/api/v1/admin/ai-models/asr/transcribe":
		return true
	default:
		return false
	}
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
	case "category":
		if path == "/api/v1/memory" {
			return map[string]any{"type": "string", "enum": []string{"self_identity", "user_fact", "preference", "project_knowledge", "instruction", "conversation_summary", "session_checkpoint", "task_artifact", "profile"}}
		}
	case "before", "since", "until":
		if path != "/api/v1/skills" {
			return map[string]any{"type": "string", "format": "date-time"}
		}
	case "q", "tag":
		return map[string]any{"type": "string"}
	case "offset":
		return map[string]any{"type": "integer", "minimum": 0}
	case "limit":
		if path == "/api/v1/memory" {
			return map[string]any{"type": "integer", "minimum": 1, "maximum": 200}
		}
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

func openAPIFileImportRequestBody(path string) map[string]any {
	if path != "/api/v1/knowledge/import/file" && path != "/api/v1/admin/public-knowledge-libraries/{libraryId}/import/file" {
		return nil
	}
	properties := map[string]any{
		"file": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type":   "string",
				"format": "binary",
			},
			"minItems": 1,
			"maxItems": maxKnowledgeUploadFiles,
		},
		"topic_hint": map[string]any{"type": "string"},
		"labels":     map[string]any{"type": "string"},
	}
	if path == "/api/v1/knowledge/import/file" {
		properties["title"] = map[string]any{"type": "string"}
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"multipart/form-data": map[string]any{
				"schema": map[string]any{
					"type":       "object",
					"required":   []string{"file"},
					"properties": properties,
				},
			},
		},
	}
}

func openAPIMemoryRequestBody(method, path string) map[string]any {
	if !((method == http.MethodPost && path == "/api/v1/memory") || (method == http.MethodPut && path == "/api/v1/memory/{id}")) {
		return nil
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{
					"type":     "object",
					"required": []string{"content"},
					"properties": map[string]any{
						"content":  map[string]any{"type": "string", "minLength": 1, "maxLength": agentservice.UserMemoryMaxContentRunes},
						"category": map[string]any{"type": "string", "enum": []string{"user_fact", "preference", "project_knowledge", "instruction", "conversation_summary", "session_checkpoint", "task_artifact", "profile"}},
						"tags": map[string]any{
							"type":     "array",
							"maxItems": agentservice.UserMemoryMaxTags,
							"items":    map[string]any{"type": "string", "maxLength": agentservice.UserMemoryMaxTagRunes},
						},
					},
				},
			},
		},
	}
}

func openAPIKnowledgeClearRequestBody(method, path string) map[string]any {
	if method != http.MethodDelete || path != "/api/v1/knowledge" {
		return nil
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"admin_password": map[string]any{"type": "string", "description": "Administrator password. For the user web single credential field, Admin Secret is also accepted here."},
						"password":       map[string]any{"type": "string", "description": "Compatibility alias for admin_password."},
						"admin_secret":   map[string]any{"type": "string", "description": "Root Admin Secret from MACLAW_ADMIN_SECRET."},
					},
					"anyOf": []map[string]any{
						{"required": []string{"admin_password"}},
						{"required": []string{"password"}},
						{"required": []string{"admin_secret"}},
					},
				},
			},
		},
	}
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
			return "Stable risk kind, such as auth_failed, web_launch_token_rejected, sandbox_disabled, sandbox_failed, or insecure_http. Matching is case-insensitive."
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
		if path == "/api/v1/memory" {
			return "Case-insensitive text search over current-user memory content and tags."
		}
		return "Case-insensitive text search over structured record title, tags, collection, and JSON data."
	case "category":
		if path == "/api/v1/memory" {
			return "Filters long-term memory by canonical memory category."
		}
	case "offset":
		return "Zero-based pagination offset."
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
