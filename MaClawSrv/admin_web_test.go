package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminWebServesEmbeddedShell(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "MaClawSrv") {
		t.Fatalf("admin shell = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	body := w.Body.String()
	if strings.Contains(body, "style=") || strings.Contains(body, "\uFFFD") {
		t.Fatalf("admin app asset contains CSP-hostile inline style or replacement char")
	}
	if w.Code != http.StatusOK || !strings.Contains(body, "/api/v1/admin/bootstrap/status") || !strings.Contains(body, "createTenant") || !strings.Contains(body, "delete-check") || !strings.Contains(body, "createCredential") || !strings.Contains(body, "rotate-secret") || !strings.Contains(body, "createSnapshot") || !strings.Contains(body, "/api/v1/admin/audit-events") || !strings.Contains(body, "/api/v1/admin/security/summary") || !strings.Contains(body, "/api/v1/admin/security/risk-events") || !strings.Contains(body, "riskEventsTable") || !strings.Contains(body, "riskDetailTarget") || !strings.Contains(body, "overviewOut") || !strings.Contains(body, "sandboxOut") || !strings.Contains(body, "t(\"view\")") || !strings.Contains(body, "t(\"empty\")") || !strings.Contains(body, "riskCountChips") || !strings.Contains(body, "riskKindCounts") || !strings.Contains(body, "riskSeverityCounts") || !strings.Contains(body, "bindRiskCountChips") || !strings.Contains(body, "data-risk-kind") || !strings.Contains(body, "data-risk-severity") || !strings.Contains(body, "loadRiskEventsFromFilters") || !strings.Contains(body, "validRiskTimeRange") || !strings.Contains(body, "validRiskLimit") || !strings.Contains(body, "invalidRiskLimit") || !strings.Contains(body, "generatedAt") || !strings.Contains(body, "risks.generated_at") || !strings.Contains(body, "risks.filters") || !strings.Contains(body, "limit=${limit}") || !strings.Contains(body, "min=\"1\"") || !strings.Contains(body, "max=\"500\"") || !strings.Contains(body, "since must be before or equal to until") || !strings.Contains(body, "riskSeverity") || !strings.Contains(body, "riskKind") || !strings.Contains(body, "riskKindOptions") || !strings.Contains(body, "riskKindOptions(risks.kind_counts)") || !strings.Contains(body, "riskEventsList") || !strings.Contains(body, "loadRisks") || !strings.Contains(body, "clearRiskFilters") || !strings.Contains(body, "setRiskTimePreset") || !strings.Contains(body, "data-risk-preset") || !strings.Contains(body, "toLocalDateTimeInput") || !strings.Contains(body, "${riskFilterSummary(security)}") || !strings.Contains(body, "riskFilterSummary") || !strings.Contains(body, "riskFilterStatus") || !strings.Contains(body, "all risks") || !strings.Contains(body, "goToRiskOps") || !strings.Contains(body, "pendingRiskFilter") || !strings.Contains(body, "applyPendingRiskFilter") || !strings.Contains(body, "/api/v1/admin/support-bundle") || !strings.Contains(body, "serviceSupportBundle") || !strings.Contains(body, "serviceSupportBundleDownload") || !strings.Contains(body, "/api/v1/admin/runtime/gc") || !strings.Contains(body, "runRuntimeGC") || !strings.Contains(body, "/api/v1/admin/runtime/goroutines") || !strings.Contains(body, "viewGoroutines") || !strings.Contains(body, "downloadGoroutines") || !strings.Contains(body, "/api/v1/admin/runtime/profiles/heap") || !strings.Contains(body, "viewHeapProfile") || !strings.Contains(body, "downloadHeapProfile") || !strings.Contains(body, "/api/v1/admin/jobs") || !strings.Contains(body, "last_sandbox_report") || !strings.Contains(body, "data-job-cancel") || !strings.Contains(body, "/api/v1/admin/logs/errors/recent") || !strings.Contains(body, "/api/v1/admin/logs/search") || !strings.Contains(body, "/download?${q}") || !strings.Contains(body, "downloadLog") || !strings.Contains(body, "rotateLog") || !strings.Contains(body, "/rotate?confirm=true") || !strings.Contains(body, "searchAllLogs") || !strings.Contains(body, "recentLogTable") || !strings.Contains(body, "logIncludeWarn") || !strings.Contains(body, "logQuery") || !strings.Contains(body, "logLineTable") || !strings.Contains(body, "sandboxReportsTable") || !strings.Contains(body, "sandboxEventsTable") || !strings.Contains(body, "loadSandboxEventsFromFilters") || !strings.Contains(body, "sandboxEventStatus") || !strings.Contains(body, "sandboxProfilesTable") || !strings.Contains(body, "data-sandbox-profile-delete") || !strings.Contains(body, "deleteSandboxProfileConfirm") || !strings.Contains(body, "/api/v1/admin/sandbox/profiles") || !strings.Contains(body, "/api/v1/admin/sandbox/events") || !strings.Contains(body, "/api/v1/admin/sandbox/support-bundle") || !strings.Contains(body, "sandboxSupportBundle") || !strings.Contains(body, "sandboxSupportBundleView") || !strings.Contains(body, "sandboxSupportBundleDownload") || !strings.Contains(body, "support-bundle?download=true") || !strings.Contains(body, "b.security_risks") || !strings.Contains(body, "b.redactions") || !strings.Contains(body, "b.recent_log_errors") || !strings.Contains(body, "t(\"redactions\")") || !strings.Contains(body, "t(\"dataRoot\")") || !strings.Contains(body, "t(\"overviewHint\")") || !strings.Contains(body, "t(\"sandboxHint\")") || !strings.Contains(body, "t(\"logsHint\")") || !strings.Contains(body, "t(\"sources\")") || !strings.Contains(body, "t(\"open\")") || !strings.Contains(body, "t(\"lines\")") || !strings.Contains(body, "t(\"createTenant\")") || !strings.Contains(body, "t(\"knowledgeAccess\")") || !strings.Contains(body, "t(\"skillSources\")") || !strings.Contains(body, "t(\"restartRequired\")") || !strings.Contains(body, "t(\"warnings\")") || !strings.Contains(body, "t(\"configHint\")") || !strings.Contains(body, "t(\"tenantsHint\")") || !strings.Contains(body, "t(\"accountsHint\")") || !strings.Contains(body, "t(\"knowledgeHint\")") || !strings.Contains(body, "t(\"opsHint\")") || !strings.Contains(body, "t(\"manualApply\")") || !strings.Contains(body, "t(\"willExecute\")") || !strings.Contains(body, "loadLocales") || !strings.Contains(body, "/api/v1/admin/i18n/locales") || !strings.Contains(body, "state.locales") || !strings.Contains(body, "out.default_locale") || !strings.Contains(body, "localeOptions") || !strings.Contains(body, "state.me?.admin?.locale") || !strings.Contains(body, "b.data_root_name") || !strings.Contains(body, "b.data_root_redacted") || !strings.Contains(body, "bundle.security_risks?.recent") || !strings.Contains(body, "riskFilterSummary(risks)") || !strings.Contains(body, "data-sandbox-report-delete") || !strings.Contains(body, "deleteSandboxReportConfirm") || !strings.Contains(body, "landlock") || !strings.Contains(body, "/api/v1/admin/service-config/environment") || !strings.Contains(body, "/api/v1/admin/service-config/diff") || !strings.Contains(body, "environment:envResp") || !strings.Contains(body, "diff:diffResp") || !strings.Contains(body, "clearCfgDraft") || !strings.Contains(body, "draft?confirm=true") || !strings.Contains(body, "buildConfigValues") || !strings.Contains(body, "cfgUse_") || !strings.Contains(body, "f.sensitive?\"\"") || !strings.Contains(body, "/api/v1/admin/knowledge-access/cross-tenant") || !strings.Contains(body, "/api/v1/admin/skill-sources/global") || !strings.Contains(body, "clearTenantKnowledge") || !strings.Contains(body, "/api/v1/admin/sandbox/install") || !strings.Contains(body, "installSandboxRun") || !strings.Contains(body, "/api/v1/admin/sandbox/switch") || !strings.Contains(body, "/api/v1/admin/sandbox/rollback") || !strings.Contains(body, "rollbackSandbox") || !strings.Contains(body, "sandboxRollbackConfirm") || !strings.Contains(body, "SNAPSHOT SECRETS") || !strings.Contains(body, "EXPORT SECRETS") || !strings.Contains(body, "IMPORT STATE") || !strings.Contains(body, "RESTORE SNAPSHOT") || !strings.Contains(body, "data-snapshot-restore-run") || !strings.Contains(body, "INSTALL SANDBOX") || !strings.Contains(body, "DISABLE SANDBOX") || !strings.Contains(body, "clearKnowledgeConfirm") || !strings.Contains(body, "deleteTenantConfirm") || !strings.Contains(body, "useSecret") || !strings.Contains(body, "auditActorUser") || !strings.Contains(body, "actor_user_id") || strings.Contains(body, "confirm_unsafe:mode===\"none\"") {
		t.Fatalf("admin app asset missing expected admin API wiring = %d body = %s", w.Code, body)
	}
}

func assertAdminSecurityHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	checks := map[string]string{
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}
	for name, want := range checks {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s header = %q, want %q", name, got, want)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	for _, part := range []string{"default-src 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, part) {
			t.Fatalf("Content-Security-Policy missing %q in %q", part, csp)
		}
	}
	if strings.Contains(csp, "'unsafe-inline'") {
		t.Fatalf("Content-Security-Policy should not allow inline script/style: %q", csp)
	}
}

func TestAdminWebIncludesTenantAuditShortcuts(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	for _, needle := range []string{"data-tenant-audit", "data-user-audit", "showTenantAudit", "/api/v1/admin/audit-events?${q}"} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing %s", needle)
		}
	}
}

func TestAdminWebIncludesOwnerRoleGuards(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	for _, needle := range []string{
		"function isOwner()",
		"state.me?.auth_type===\"admin_secret\"",
		"state.me?.admin?.role===\"owner\"",
		"function applyOwnerGuards()",
		"ownerOnly",
		"runRuntimeGC",
		"saveSandbox",
		"rollbackSandbox",
		"installSandboxRun",
		"createTenant",
		"createCredential",
		"runImport",
		"createSnapshot",
		"[data-job-cancel]",
		"[data-tenant-status]",
		"[data-user-delete]",
		"[data-snapshot-delete]",
		"/api/v1/admin/auth/change-password",
		"/api/v1/admin/auth/users",
		"/api/v1/admin/auth/sessions",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing owner guard marker %s", needle)
		}
	}
}

func TestAdminWebDownloadsUseAuthenticatedFetch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	if strings.Contains(body, "window.open") {
		t.Fatalf("admin web downloads must not use window.open because Admin API auth requires headers")
	}
	for _, needle := range []string{
		"async function downloadAdmin",
		"async function textAdmin",
		"fetch(path,{headers:headers(false)})",
		"URL.createObjectURL(blob)",
		"content-disposition",
		"downloadAdmin(\"/api/v1/admin/runtime/goroutines?debug=2&download=true\"",
		"downloadAdmin(\"/api/v1/admin/runtime/profiles/heap?debug=1&gc=true&download=true\"",
		"downloadAdmin(\"/api/v1/admin/support-bundle?download=true\"",
		"downloadAdmin(\"/api/v1/admin/sandbox/support-bundle?download=true\"",
		"downloadAdmin(`/api/v1/admin/logs/${encodeURIComponent(id)}/download?${q}`",
		"textAdmin(\"/api/v1/admin/runtime/goroutines?debug=1\")",
		"textAdmin(\"/api/v1/admin/runtime/profiles/heap?debug=1&gc=true\")",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("admin web missing authenticated download marker %s", needle)
		}
	}
}

func TestAdminWebAccessibilityContracts(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "root-admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	shell := w.Body.String()
	for _, needle := range []string{
		`class="skip-link"`,
		`<nav id="nav" class="nav" aria-label="Admin sections">`,
		`<main id="main" class="main" tabindex="-1">`,
	} {
		if !strings.Contains(shell, needle) {
			t.Fatalf("admin shell missing accessibility marker %s", needle)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/app.js", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	app := w.Body.String()
	for _, needle := range []string{
		`el.setAttribute("role","status")`,
		`el.setAttribute("aria-live","polite")`,
		`function enhanceA11y()`,
		`content.setAttribute("role","region")`,
		`th.setAttribute("scope","col")`,
		`badge.className=`,
	} {
		if !strings.Contains(app, needle) {
			t.Fatalf("admin app missing accessibility marker %s", needle)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/styles.css", nil)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	css := w.Body.String()
	for _, needle := range []string{".skip-link", ".badge-on::before", ".badge-off::before"} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing accessibility marker %s", needle)
		}
	}
	for _, needle := range []string{
		`@media (prefers-color-scheme: dark)`,
		`color-scheme: dark`,
		`--bg: #0e141a`,
		`.empty-state`,
	} {
		if !strings.Contains(css, needle) {
			t.Fatalf("admin css missing dark mode marker %s", needle)
		}
	}
}
