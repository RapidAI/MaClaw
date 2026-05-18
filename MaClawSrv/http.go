package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

type configEnvelope struct {
	AppConfig corelib.AppConfig `json:"app_config"`
}

type importStateEnvelope struct {
	Data      agentservice.ExportServiceStateOutput `json:"data"`
	Overwrite bool                                  `json:"overwrite,omitempty"`
	DryRun    bool                                  `json:"dry_run,omitempty"`
}

type readinessCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

type readinessReport struct {
	Status      string           `json:"status"`
	GeneratedAt time.Time        `json:"generated_at"`
	DataRoot    string           `json:"data_root,omitempty"`
	Checks      []readinessCheck `json:"checks"`
}

type adminRiskEvent struct {
	ID           string            `json:"id"`
	Severity     string            `json:"severity"`
	Kind         string            `json:"kind"`
	Summary      string            `json:"summary"`
	Action       string            `json:"action,omitempty"`
	ResourceType string            `json:"resource_type,omitempty"`
	ResourceID   string            `json:"resource_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
}

type HTTPServer struct {
	svc            *agentservice.Service
	adminSecret    string
	mux            *http.ServeMux
	authLimiter    *authLimiter
	jobs           *asyncJobManager
	knowledgeMgr   *knowledgeStoreManager
	skillSourceSvc *cskill.SourceControlService
}

func NewHTTPServer(svc *agentservice.Service, adminSecret string, knowledgeMgr *knowledgeStoreManager, skillSourceSvc ...*cskill.SourceControlService) *HTTPServer {
	var sourceSvc *cskill.SourceControlService
	if len(skillSourceSvc) > 0 {
		sourceSvc = skillSourceSvc[0]
	}
	if sourceSvc == nil {
		sourceSvc = cskill.NewSourceControlService(newFileKVStore(filepath.Join(svc.DataRoot(), "skill_source_control.json")))
	}
	s := &HTTPServer{svc: svc, adminSecret: adminSecret, mux: http.NewServeMux(), authLimiter: newAuthLimiter(20, time.Minute), jobs: newAsyncJobManager(svc.DataRoot()), knowledgeMgr: knowledgeMgr, skillSourceSvc: sourceSvc}
	s.routes()
	s.startSandboxStartupDiagnoseIfEnabled()
	return s
}

type authFailureState struct {
	Count        int
	BlockedUntil time.Time
	LastFailure  time.Time
}

type authLimiter struct {
	mu                sync.Mutex
	limit             int
	window            time.Duration
	failureThreshold  int
	baseBlockDuration time.Duration
	maxBlockDuration  time.Duration
	buckets           map[string][]time.Time
	failures          map[string]authFailureState
}

func newAuthLimiter(limit int, window time.Duration) *authLimiter {
	return &authLimiter{
		limit:             limit,
		window:            window,
		failureThreshold:  5,
		baseBlockDuration: time.Minute,
		maxBlockDuration:  15 * time.Minute,
		buckets:           map[string][]time.Time{},
		failures:          map[string]authFailureState{},
	}
}

func (l *authLimiter) Allow(key string, now time.Time) bool {
	allowed, _ := l.AllowWithRetry(key, now)
	return allowed
}

func (l *authLimiter) AllowWithRetry(key string, now time.Time) (bool, time.Duration) {
	if l == nil || strings.TrimSpace(key) == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if retry := l.blockedRetryLocked(key, now); retry > 0 {
		return false, retry
	}
	cutoff := now.Add(-l.window)
	items := l.buckets[key][:0]
	for _, ts := range l.buckets[key] {
		if ts.After(cutoff) {
			items = append(items, ts)
		}
	}
	if len(items) >= l.limit {
		l.buckets[key] = items
		return false, l.window
	}
	l.buckets[key] = append(items, now)
	return true, 0
}

func (l *authLimiter) RegisterFailure(key string, now time.Time) time.Duration {
	if l == nil || strings.TrimSpace(key) == "" {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state := l.failures[key]
	if !state.LastFailure.IsZero() && now.Sub(state.LastFailure) > l.window {
		state = authFailureState{}
	}
	state.Count++
	state.LastFailure = now
	if state.Count >= l.failureThreshold {
		steps := state.Count - l.failureThreshold
		block := l.baseBlockDuration << steps
		if block > l.maxBlockDuration {
			block = l.maxBlockDuration
		}
		state.BlockedUntil = now.Add(block)
		l.failures[key] = state
		return block
	}
	l.failures[key] = state
	return 0
}

func (l *authLimiter) ResetFailures(key string) {
	if l == nil || strings.TrimSpace(key) == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func (l *authLimiter) blockedRetryLocked(key string, now time.Time) time.Duration {
	state, ok := l.failures[key]
	if !ok {
		return 0
	}
	if !state.BlockedUntil.After(now) {
		if !state.LastFailure.IsZero() && now.Sub(state.LastFailure) > l.window {
			delete(l.failures, key)
		} else {
			state.BlockedUntil = time.Time{}
			l.failures[key] = state
		}
		return 0
	}
	return time.Until(state.BlockedUntil)
}

const maxJSONBodyBytes int64 = 1 << 20

func (s *HTTPServer) Handler() http.Handler { return s.mux }

func (s *HTTPServer) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /livez", s.handleLive)
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /version", s.handleVersion)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /api/v1/openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /admin", s.handleAdminWeb)
	s.mux.HandleFunc("GET /admin/", s.handleAdminWeb)
	s.mux.HandleFunc("GET /api/v1/admin/bootstrap/status", s.withAdminSecurityHeaders(s.handleAdminBootstrapStatus))
	s.mux.HandleFunc("POST /api/v1/admin/bootstrap/initialize", s.withAdminSecurityHeaders(s.handleAdminBootstrapInitialize))
	s.mux.HandleFunc("POST /api/v1/admin/auth/login", s.withAdminSecurityHeaders(s.handleAdminAuthLogin))
	s.mux.HandleFunc("POST /api/v1/admin/auth/logout", s.withAdmin(s.handleAdminAuthLogout))
	s.mux.HandleFunc("GET /api/v1/admin/auth/me", s.withAdmin(s.handleAdminAuthMe))
	s.mux.HandleFunc("POST /api/v1/admin/auth/change-password", s.withAdmin(s.handleAdminAuthChangePassword))
	s.mux.HandleFunc("GET /api/v1/admin/auth/users", s.withAdmin(s.handleAdminAuthUsers))
	s.mux.HandleFunc("POST /api/v1/admin/auth/users", s.withAdmin(s.handleAdminAuthCreateUser))
	s.mux.HandleFunc("PATCH /api/v1/admin/auth/users/{adminUserId}", s.withAdmin(s.handleAdminAuthUpdateUser))
	s.mux.HandleFunc("GET /api/v1/admin/auth/sessions", s.withAdmin(s.handleAdminAuthSessions))
	s.mux.HandleFunc("DELETE /api/v1/admin/auth/sessions/{sessionId}", s.withAdmin(s.handleAdminAuthRevokeSession))
	s.mux.HandleFunc("GET /api/v1/admin/system/readiness", s.withAdmin(s.handleGetAdminReadiness))
	s.mux.HandleFunc("GET /api/v1/admin/overview", s.withAdmin(s.handleGetAdminOverview))
	s.mux.HandleFunc("GET /api/v1/admin/dashboard", s.withAdmin(s.handleGetAdminDashboard))
	s.mux.HandleFunc("GET /api/v1/admin/support-bundle", s.withAdmin(s.handleAdminSupportBundle))
	s.mux.HandleFunc("GET /api/v1/admin/insights", s.withAdmin(s.handleGetAdminInsights))
	s.mux.HandleFunc("GET /api/v1/admin/alerts", s.withAdmin(s.handleGetAdminAlerts))
	s.mux.HandleFunc("GET /api/v1/admin/security/summary", s.withAdmin(s.handleAdminSecuritySummary))
	s.mux.HandleFunc("GET /api/v1/admin/security/risk-events", s.withAdmin(s.handleAdminSecurityRiskEvents))
	s.mux.HandleFunc("GET /api/v1/admin/runtime/status", s.withAdmin(s.handleAdminRuntimeStatus))
	s.mux.HandleFunc("POST /api/v1/admin/runtime/gc", s.withAdmin(s.handleAdminRuntimeGC))
	s.mux.HandleFunc("GET /api/v1/admin/runtime/goroutines", s.withAdmin(s.handleAdminRuntimeGoroutines))
	s.mux.HandleFunc("GET /api/v1/admin/runtime/profiles/{profileName}", s.withAdmin(s.handleAdminRuntimeProfile))
	s.mux.HandleFunc("GET /api/v1/admin/scheduler/status", s.withAdmin(s.handleAdminSchedulerStatus))
	s.mux.HandleFunc("GET /api/v1/admin/jobs", s.withAdmin(s.handleAdminJobs))
	s.mux.HandleFunc("POST /api/v1/admin/jobs/{jobId}/cancel", s.withAdmin(s.handleAdminCancelJob))
	s.mux.HandleFunc("GET /api/v1/admin/logs/sources", s.withAdmin(s.handleAdminLogSources))
	s.mux.HandleFunc("GET /api/v1/admin/logs/errors/recent", s.withAdmin(s.handleAdminRecentLogErrors))
	s.mux.HandleFunc("POST /api/v1/admin/logs/search", s.withAdmin(s.handleAdminLogSearch))
	s.mux.HandleFunc("GET /api/v1/admin/logs/{sourceId}/download", s.withAdmin(s.handleAdminLogDownload))
	s.mux.HandleFunc("POST /api/v1/admin/logs/{sourceId}/rotate", s.withAdmin(s.handleAdminLogRotate))
	s.mux.HandleFunc("GET /api/v1/admin/logs/{sourceId}/tail", s.withAdmin(s.handleAdminLogRead))
	s.mux.HandleFunc("GET /api/v1/admin/logs/{sourceId}", s.withAdmin(s.handleAdminLogRead))
	s.mux.HandleFunc("GET /api/v1/admin/service-config/effective", s.withAdmin(s.handleGetAdminServiceConfigEffective))
	s.mux.HandleFunc("GET /api/v1/admin/service-config/schema", s.withAdmin(s.handleAdminServiceConfigSchema))
	s.mux.HandleFunc("GET /api/v1/admin/service-config/environment", s.withAdmin(s.handleAdminServiceConfigEnvironment))
	s.mux.HandleFunc("GET /api/v1/admin/service-config/diff", s.withAdmin(s.handleAdminServiceConfigDiff))
	s.mux.HandleFunc("GET /api/v1/admin/service-config/draft", s.withAdmin(s.handleAdminServiceConfigDraft))
	s.mux.HandleFunc("PATCH /api/v1/admin/service-config/draft", s.withAdmin(s.handleUpdateAdminServiceConfigDraft))
	s.mux.HandleFunc("DELETE /api/v1/admin/service-config/draft", s.withAdmin(s.handleClearAdminServiceConfigDraft))
	s.mux.HandleFunc("POST /api/v1/admin/service-config/validate", s.withAdmin(s.handleValidateAdminServiceConfig))
	s.mux.HandleFunc("POST /api/v1/admin/service-config/export-plan", s.withAdmin(s.handleExportAdminServiceConfigPlan))
	s.mux.HandleFunc("GET /api/v1/admin/i18n/locales", s.withAdmin(s.handleAdminI18NLocales))
	s.mux.HandleFunc("GET /api/v1/admin/i18n/messages", s.withAdmin(s.handleAdminI18NMessages))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/status", s.withAdmin(s.handleAdminSandboxStatus))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/config", s.withAdmin(s.handleAdminSandboxConfig))
	s.mux.HandleFunc("PUT /api/v1/admin/sandbox/config", s.withAdmin(s.handleUpdateAdminSandboxConfig))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/rollback", s.withAdmin(s.handleRollbackAdminSandboxConfig))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/switch", s.withAdmin(s.handleSwitchAdminSandbox))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/detect", s.withAdmin(s.handleAdminSandboxDetect))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/smoke-test", s.withAdmin(s.handleAdminSandboxSmokeTest))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/diagnose", s.withAdmin(s.handleAdminSandboxDiagnose))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/events", s.withAdmin(s.handleAdminSandboxEvents))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/support-bundle", s.withAdmin(s.handleAdminSandboxSupportBundle))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/profiles", s.withAdmin(s.handleAdminSandboxProfiles))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/profiles/{profileName}", s.withAdmin(s.handleAdminSandboxProfile))
	s.mux.HandleFunc("PUT /api/v1/admin/sandbox/profiles/{profileName}", s.withAdmin(s.handleUpdateAdminSandboxProfile))
	s.mux.HandleFunc("DELETE /api/v1/admin/sandbox/profiles/{profileName}", s.withAdmin(s.handleDeleteAdminSandboxProfile))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/profiles/{profileName}/validate", s.withAdmin(s.handleValidateAdminSandboxProfile))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/reports", s.withAdmin(s.handleAdminSandboxReports))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/reports/{reportId}", s.withAdmin(s.handleAdminSandboxReport))
	s.mux.HandleFunc("DELETE /api/v1/admin/sandbox/reports/{reportId}", s.withAdmin(s.handleDeleteAdminSandboxReport))
	s.mux.HandleFunc("GET /api/v1/admin/sandbox/install-plan", s.withAdmin(s.handleAdminSandboxInstallPlan))
	s.mux.HandleFunc("POST /api/v1/admin/sandbox/install", s.withAdmin(s.handleAdminSandboxInstall))
	s.mux.HandleFunc("GET /api/v1/admin/tenants", s.withAdmin(s.handleListTenants))
	s.mux.HandleFunc("GET /api/v1/admin/audit-events", s.withAdmin(s.handleListAuditEvents))
	s.mux.HandleFunc("GET /api/v1/admin/export", s.withAdmin(s.handleExportServiceState))
	s.mux.HandleFunc("POST /api/v1/admin/import", s.withAdmin(s.handleImportServiceState))
	s.mux.HandleFunc("GET /api/v1/admin/snapshots", s.withAdmin(s.handleListServiceSnapshots))
	s.mux.HandleFunc("POST /api/v1/admin/snapshots", s.withAdmin(s.handleCreateServiceSnapshot))
	s.mux.HandleFunc("POST /api/v1/admin/snapshots/prune", s.withAdmin(s.handlePruneServiceSnapshots))
	s.mux.HandleFunc("GET /api/v1/admin/snapshots/{snapshotId}", s.withAdmin(s.handleGetServiceSnapshot))
	s.mux.HandleFunc("POST /api/v1/admin/snapshots/{snapshotId}/restore", s.withAdmin(s.handleRestoreServiceSnapshot))
	s.mux.HandleFunc("DELETE /api/v1/admin/snapshots/{snapshotId}", s.withAdmin(s.handleDeleteServiceSnapshot))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/retire-plan", s.withAdmin(s.handleGetTenantRetirePlan))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}/retire-plan", s.withAdmin(s.handleGetUserRetirePlan))
	s.mux.HandleFunc("POST /api/v1/admin/tenants", s.withAdmin(s.handleCreateTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleGetTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/summary", s.withAdmin(s.handleGetTenantSummary))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/delete-check", s.withAdmin(s.handleGetTenantDeleteCheck))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleUpdateTenant))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/pause", s.withAdmin(s.handlePauseTenant))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/resume", s.withAdmin(s.handleResumeTenant))
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleDeleteTenant))
	s.mux.HandleFunc("GET /api/v1/admin/users", s.withAdmin(s.handleListAllUsers))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleCreateUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleGetUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}/delete-check", s.withAdmin(s.handleGetUserDeleteCheck))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleUpdateUser))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/pause", s.withAdmin(s.handlePauseUser))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/resume", s.withAdmin(s.handleResumeUser))
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleDeleteUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", s.withAdmin(s.handleListCredentials))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", s.withAdmin(s.handleCreateCredential))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", s.withAdmin(s.handleGetCredential))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", s.withAdmin(s.handleUpdateCredential))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-secret", s.withAdmin(s.handleRotateCredentialSecret))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}/rotate-key", s.withAdmin(s.handleRotateCredentialKey))
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials/{credentialId}", s.withAdmin(s.handleRevokeCredential))
	s.mux.HandleFunc("POST /api/v1/auth/token", s.handleIssueToken)
	s.mux.HandleFunc("GET /api/v1/me", s.withPrincipal(s.handleGetMe))
	s.mux.HandleFunc("GET /api/v1/config/schema", s.withPrincipal(s.handleGetConfigSchema))
	s.mux.HandleFunc("GET /api/v1/config", s.withPrincipal(s.handleGetConfig))
	s.mux.HandleFunc("PUT /api/v1/config", s.withPrincipal(s.handleUpdateConfig))
	s.mux.HandleFunc("POST /api/v1/config/validate", s.withPrincipal(s.handleValidateConfig))
	s.mux.HandleFunc("POST /api/v1/config/test", s.withPrincipal(s.handleTestConfig))
	s.mux.HandleFunc("GET /api/v1/usage/summary", s.withPrincipal(s.handleGetUsageSummary))
	s.mux.HandleFunc("GET /api/v1/mcp/servers", s.withPrincipal(s.handleListMCPServers))
	s.mux.HandleFunc("POST /api/v1/mcp/servers", s.withPrincipal(s.handleCreateMCPServer))
	s.mux.HandleFunc("GET /api/v1/mcp/servers/{serverId}", s.withPrincipal(s.handleGetMCPServer))
	s.mux.HandleFunc("PATCH /api/v1/mcp/servers/{serverId}", s.withPrincipal(s.handleUpdateMCPServer))
	s.mux.HandleFunc("DELETE /api/v1/mcp/servers/{serverId}", s.withPrincipal(s.handleDeleteMCPServer))
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverId}/start", s.withPrincipal(s.handleStartMCPServer))
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverId}/stop", s.withPrincipal(s.handleStopMCPServer))
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverId}/health-check", s.withPrincipal(s.handleCheckMCPServer))
	s.mux.HandleFunc("GET /api/v1/mcp/servers/{serverId}/tools", s.withPrincipal(s.handleGetMCPServerTools))
	s.mux.HandleFunc("GET /api/v1/skills", s.withPrincipal(s.handleListSkills))
	s.mux.HandleFunc("POST /api/v1/skills/search", s.withPrincipal(s.handleSearchSkills))
	s.mux.HandleFunc("POST /api/v1/skills/install", s.withPrincipal(s.handleInstallSkill))
	s.mux.HandleFunc("POST /api/v1/skills/import", s.withPrincipal(s.handleImportSkill))
	s.mux.HandleFunc("GET /api/v1/jobs", s.withPrincipal(s.handleListAsyncJobs))
	s.mux.HandleFunc("DELETE /api/v1/jobs", s.withPrincipal(s.handleDeleteAsyncJobs))
	s.mux.HandleFunc("GET /api/v1/jobs/{jobId}", s.withPrincipal(s.handleGetAsyncJob))
	s.mux.HandleFunc("POST /api/v1/jobs/{jobId}/cancel", s.withPrincipal(s.handleCancelAsyncJob))
	s.mux.HandleFunc("DELETE /api/v1/jobs/{jobId}", s.withPrincipal(s.handleDeleteAsyncJob))
	s.mux.HandleFunc("GET /api/v1/records", s.withPrincipal(s.handleListStructuredRecords))
	s.mux.HandleFunc("GET /api/v1/records/{collection}", s.withPrincipal(s.handleListStructuredRecords))
	s.mux.HandleFunc("POST /api/v1/records/{collection}", s.withPrincipal(s.handleCreateStructuredRecord))
	s.mux.HandleFunc("GET /api/v1/records/{collection}/{recordId}", s.withPrincipal(s.handleGetStructuredRecord))
	s.mux.HandleFunc("PATCH /api/v1/records/{collection}/{recordId}", s.withPrincipal(s.handleUpdateStructuredRecord))
	s.mux.HandleFunc("DELETE /api/v1/records/{collection}/{recordId}", s.withPrincipal(s.handleDeleteStructuredRecord))
	s.mux.HandleFunc("GET /api/v1/skill-uploads/{submissionId}", s.withPrincipal(s.handleGetSkillUploadStatus))
	s.mux.HandleFunc("GET /api/v1/skill-market/account", s.withPrincipal(s.handleGetSkillMarketAccount))
	s.mux.HandleFunc("GET /api/v1/skills/{skillName}", s.withPrincipal(s.handleGetSkill))
	s.mux.HandleFunc("DELETE /api/v1/skills/{skillName}", s.withPrincipal(s.handleDeleteSkill))
	s.mux.HandleFunc("GET /api/v1/skills/{skillName}/export", s.withPrincipal(s.handleExportSkill))
	s.mux.HandleFunc("POST /api/v1/skills/{skillName}/validate", s.withPrincipal(s.handleValidateSkill))
	s.mux.HandleFunc("POST /api/v1/skills/{skillName}/improve", s.withPrincipal(s.handleImproveSkill))
	s.mux.HandleFunc("POST /api/v1/skills/{skillName}/upload", s.withPrincipal(s.handleUploadSkill))
	s.mux.HandleFunc("GET /api/v1/instances", s.withPrincipal(s.handleListInstances))
	s.mux.HandleFunc("POST /api/v1/instances", s.withPrincipal(s.handleCreateInstance))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}", s.withPrincipal(s.handleGetInstance))
	s.mux.HandleFunc("PATCH /api/v1/instances/{instanceId}", s.withPrincipal(s.handleUpdateInstance))
	s.mux.HandleFunc("DELETE /api/v1/instances/{instanceId}", s.withPrincipal(s.handleDeleteInstance))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/capabilities", s.withPrincipal(s.handleGetInstanceCapabilities))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/stop", s.withPrincipal(s.handleStopInstance))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/resume", s.withPrincipal(s.handleResumeInstance))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/refresh-readiness", s.withPrincipal(s.handleRefreshInstanceReadiness))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/summary", s.withPrincipal(s.handleGetInstanceSummary))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/bootstrap", s.withPrincipal(s.handleGetInstanceBootstrap))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/messages", s.withPrincipal(s.handleSendMessage))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions", s.withPrincipal(s.handleListSessions))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions", s.withPrincipal(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions/{sessionId}", s.withPrincipal(s.handleGetSession))
	s.mux.HandleFunc("PATCH /api/v1/instances/{instanceId}/sessions/{sessionId}", s.withPrincipal(s.handleUpdateSession))
	s.mux.HandleFunc("DELETE /api/v1/instances/{instanceId}/sessions/{sessionId}", s.withPrincipal(s.handleDeleteSession))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions/{sessionId}/archive", s.withPrincipal(s.handleArchiveSession))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions/{sessionId}/restore", s.withPrincipal(s.handleRestoreSession))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages", s.withPrincipal(s.handleListMessages))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages", s.withPrincipal(s.handlePostMessage))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs", s.withPrincipal(s.handleListRuns))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs/{runId}", s.withPrincipal(s.handleGetRun))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs/{runId}/events", s.withPrincipal(s.handleStreamRunEvents))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/runs/{runId}/cancel", s.withPrincipal(s.handleCancelRun))

	// Knowledge base endpoints
	s.mux.HandleFunc("POST /api/v1/knowledge/import/file", s.withPrincipal(s.handleKnowledgeImportFile))
	s.mux.HandleFunc("POST /api/v1/knowledge/import/url", s.withPrincipal(s.handleKnowledgeImportURL))
	s.mux.HandleFunc("POST /api/v1/knowledge/import/text", s.withPrincipal(s.handleKnowledgeImportText))
	s.mux.HandleFunc("POST /api/v1/knowledge/import/directory", s.withPrincipal(s.handleKnowledgeImportDirectory))
	s.mux.HandleFunc("GET /api/v1/knowledge/import/jobs/{jobId}", s.withPrincipal(s.handleKnowledgeImportJobStatus))
	s.mux.HandleFunc("POST /api/v1/knowledge/search", s.withPrincipal(s.handleKnowledgeSearch))
	s.mux.HandleFunc("POST /api/v1/knowledge/context-pack", s.withPrincipal(s.handleKnowledgeContextPack))
	s.mux.HandleFunc("GET /api/v1/knowledge/sources", s.withPrincipal(s.handleKnowledgeListSources))
	s.mux.HandleFunc("GET /api/v1/knowledge/sources/{sourceId}", s.withPrincipal(s.handleKnowledgeGetSource))
	s.mux.HandleFunc("DELETE /api/v1/knowledge/sources/{sourceId}", s.withPrincipal(s.handleKnowledgeDeleteSource))
	s.mux.HandleFunc("PATCH /api/v1/knowledge/sources/{sourceId}", s.withPrincipal(s.handleKnowledgeUpdateSource))
	s.mux.HandleFunc("POST /api/v1/knowledge/sources/{sourceId}/disable", s.withPrincipal(s.handleKnowledgeDisableSource))
	s.mux.HandleFunc("POST /api/v1/knowledge/sources/{sourceId}/enable", s.withPrincipal(s.handleKnowledgeEnableSource))
	s.mux.HandleFunc("POST /api/v1/knowledge/sources/{sourceId}/refresh", s.withPrincipal(s.handleKnowledgeRefreshSource))
	s.mux.HandleFunc("GET /api/v1/knowledge/stats", s.withPrincipal(s.handleKnowledgeStats))
	s.mux.HandleFunc("GET /api/v1/knowledge/access", s.withPrincipal(s.handleKnowledgeAccessGetMe))
	s.mux.HandleFunc("DELETE /api/v1/knowledge", s.withPrincipal(s.handleKnowledgeClearAll))
	s.mux.HandleFunc("GET /api/v1/admin/knowledge/stats", s.withAdmin(s.handleAdminKnowledgeStats))
	s.mux.HandleFunc("GET /api/v1/admin/knowledge/sources", s.withAdmin(s.handleAdminKnowledgeListSources))
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{tenantId}/knowledge", s.withAdmin(s.handleAdminKnowledgeClearTenant))
	s.mux.HandleFunc("GET /api/v1/admin/knowledge-access/cross-tenant", s.withAdmin(s.handleAdminKnowledgeAccessGetCrossTenant))
	s.mux.HandleFunc("PUT /api/v1/admin/knowledge-access/cross-tenant", s.withAdmin(s.handleAdminKnowledgeAccessSetCrossTenant))
	s.mux.HandleFunc("GET /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleAdminKnowledgeAccessGetUser))
	s.mux.HandleFunc("PUT /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleAdminKnowledgeAccessSetUser))
	s.mux.HandleFunc("DELETE /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleAdminKnowledgeAccessDeleteUser))
	s.mux.HandleFunc("GET /api/v1/admin/knowledge-access/tenants/{tenantId}/users/{userId}/resolve", s.withAdmin(s.handleAdminKnowledgeAccessResolveUser))

	// Skill source control admin API (global / tenant / user).
	s.mux.HandleFunc("GET /api/v1/admin/skill-sources/available", s.withAdmin(s.handleSkillSourcesAvailable))
	s.mux.HandleFunc("GET /api/v1/admin/skill-sources/global", s.withAdmin(s.handleSkillSourcesGetGlobal))
	s.mux.HandleFunc("PUT /api/v1/admin/skill-sources/global", s.withAdmin(s.handleSkillSourcesSetGlobal))
	s.mux.HandleFunc("GET /api/v1/admin/skill-sources/tenant/{id}", s.withAdmin(s.handleSkillSourcesGetTenant))
	s.mux.HandleFunc("PUT /api/v1/admin/skill-sources/tenant/{id}", s.withAdmin(s.handleSkillSourcesSetTenant))
	s.mux.HandleFunc("DELETE /api/v1/admin/skill-sources/tenant/{id}", s.withAdmin(s.handleSkillSourcesDeleteTenant))
	s.mux.HandleFunc("GET /api/v1/admin/skill-sources/user/{email...}", s.withAdmin(s.handleSkillSourcesGetUser))
	s.mux.HandleFunc("PUT /api/v1/admin/skill-sources/user/{email...}", s.withAdmin(s.handleSkillSourcesSetUser))
	s.mux.HandleFunc("DELETE /api/v1/admin/skill-sources/user/{email...}", s.withAdmin(s.handleSkillSourcesDeleteUser))
	s.mux.HandleFunc("GET /api/v1/admin/skill-sources/resolve/{email...}", s.withAdmin(s.handleSkillSourcesResolve))
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *HTTPServer) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "alive"})
}
func (s *HTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	report := buildReadinessReport(s.svc.DataRoot(), s.jobs.filePath)
	if report.Status != "ready" {
		errMsg := "service not ready"
		for _, check := range report.Checks {
			if check.Status != "pass" && check.Error != "" {
				errMsg = check.Error
				break
			}
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": report.Status, "error": errMsg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": report.Status})
}

func (s *HTTPServer) handleGetAdminReadiness(w http.ResponseWriter, r *http.Request) {
	report := buildReadinessReport(s.svc.DataRoot(), s.jobs.filePath)
	status := http.StatusOK
	if report.Status != "ready" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, report)
}

func buildReadinessReport(dataRoot, jobsFilePath string) readinessReport {
	report := readinessReport{
		Status:      "ready",
		GeneratedAt: time.Now().UTC(),
		DataRoot:    dataRoot,
		Checks: []readinessCheck{
			checkPathExists("data_root_exists", dataRoot),
			checkPathDirectory("data_root_is_dir", dataRoot),
			checkDirectoryWritable("data_root_writable", dataRoot),
			checkDirectoryWritable("state_dir_writable", filepath.Join(dataRoot, "state")),
		},
	}
	if stringsTrim(jobsFilePath) != "" {
		report.Checks = append(report.Checks, checkDirectoryWritable("jobs_store_parent_writable", filepath.Dir(jobsFilePath)))
	}
	for _, check := range report.Checks {
		if check.Status != "pass" {
			report.Status = "not_ready"
			break
		}
	}
	return report
}

func checkReadyDataRoot(dataRoot string) error {
	report := buildReadinessReport(dataRoot, filepath.Join(dataRoot, "state", "jobs.json"))
	for _, check := range report.Checks {
		if check.Name == "data_root_exists" && check.Status != "pass" {
			return errors.New("data root unavailable")
		}
		if check.Name == "data_root_is_dir" && check.Status != "pass" {
			return errors.New("data root is not a directory")
		}
		if check.Name == "data_root_writable" && check.Status != "pass" {
			return errors.New("data root is not writable")
		}
	}
	return nil
}

func checkPathExists(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	if _, err := os.Stat(target); err != nil {
		check.Status = "fail"
		check.Error = "data root unavailable"
	}
	return check
}

func checkPathDirectory(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	info, err := os.Stat(target)
	if err != nil {
		check.Status = "fail"
		check.Error = "data root unavailable"
		return check
	}
	if !info.IsDir() {
		check.Status = "fail"
		check.Error = "data root is not a directory"
	}
	return check
}

func checkDirectoryWritable(name, target string) readinessCheck {
	check := readinessCheck{Name: name, Status: "pass", Path: target}
	if stringsTrim(target) == "" {
		check.Status = "fail"
		check.Error = "directory path is empty"
		return check
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		check.Status = "fail"
		check.Error = "directory is not writable"
		return check
	}
	f, err := os.CreateTemp(target, ".readyz-*")
	if err != nil {
		check.Status = "fail"
		check.Error = "directory is not writable"
		return check
	}
	nameOnDisk := f.Name()
	_ = f.Close()
	_ = os.Remove(nameOnDisk)
	return check
}
func (s *HTTPServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": serviceVersion, "commit": serviceCommit, "built_at": serviceBuiltAt})
}
func writeMetricsUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
}
func (s *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	overview, err := s.svc.GetAdminOverview(r.Context())
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	authFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	rateLimited, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_rate_limited"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminAuthFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.auth_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminOwnerDenied, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.owner_required_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminLoginFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.login_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminLoginRateLimited, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.login_rate_limited"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	adminPasswordChangeFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "admin.password_change_failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	runSucceeded, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.succeeded"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	runFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.failed"})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	alerts, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{})
	if err != nil {
		writeMetricsUnavailable(w)
		return
	}
	jobCounts := s.jobs.snapshotCounts()
	var b strings.Builder
	b.WriteString("# HELP maclaw_metrics_up Whether metrics collection succeeded\n")
	b.WriteString("# TYPE maclaw_metrics_up gauge\n")
	b.WriteString("maclaw_metrics_up 1\n")
	b.WriteString("# HELP maclaw_tenants_total Number of tenants\n")
	b.WriteString("# TYPE maclaw_tenants_total gauge\n")
	b.WriteString("maclaw_tenants_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Tenants), 10))
	b.WriteString("\n# HELP maclaw_users_total Number of users\n")
	b.WriteString("# TYPE maclaw_users_total gauge\n")
	b.WriteString("maclaw_users_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Users), 10))
	b.WriteString("\n# HELP maclaw_credentials_total Number of credentials\n")
	b.WriteString("# TYPE maclaw_credentials_total gauge\n")
	b.WriteString("maclaw_credentials_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Credentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_by_status Number of credentials by lifecycle status\n")
	b.WriteString("# TYPE maclaw_credentials_by_status gauge\n")
	b.WriteString("maclaw_credentials_by_status{status=\"active\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.ActiveCredentials), 10))
	b.WriteString("\nmaclaw_credentials_by_status{status=\"suspended\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.SuspendedCredentials), 10))
	b.WriteString("\nmaclaw_credentials_by_status{status=\"revoked\"} ")
	b.WriteString(strconv.FormatInt(int64(overview.RevokedCredentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_expired_total Number of credentials whose expires_at has passed\n")
	b.WriteString("# TYPE maclaw_credentials_expired_total gauge\n")
	b.WriteString("maclaw_credentials_expired_total ")
	b.WriteString(strconv.FormatInt(int64(overview.ExpiredCredentials), 10))
	b.WriteString("\n# HELP maclaw_credentials_expiring_total Number of credentials expiring within the default lookahead window\n")
	b.WriteString("# TYPE maclaw_credentials_expiring_total gauge\n")
	b.WriteString("maclaw_credentials_expiring_total ")
	b.WriteString(strconv.FormatInt(int64(overview.ExpiringCredentials), 10))
	b.WriteString("\n# HELP maclaw_instances_total Number of instances\n")
	b.WriteString("# TYPE maclaw_instances_total gauge\n")
	b.WriteString("maclaw_instances_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Instances), 10))
	b.WriteString("\n# HELP maclaw_sessions_total Number of sessions\n")
	b.WriteString("# TYPE maclaw_sessions_total gauge\n")
	b.WriteString("maclaw_sessions_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Sessions), 10))
	b.WriteString("\n# HELP maclaw_messages_total Number of messages\n")
	b.WriteString("# TYPE maclaw_messages_total gauge\n")
	b.WriteString("maclaw_messages_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Messages), 10))
	b.WriteString("\n# HELP maclaw_runs_total Number of runs\n")
	b.WriteString("# TYPE maclaw_runs_total gauge\n")
	b.WriteString("maclaw_runs_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Runs), 10))
	b.WriteString("\n# HELP maclaw_snapshots_total Number of persisted service snapshots\n")
	b.WriteString("# TYPE maclaw_snapshots_total gauge\n")
	b.WriteString("maclaw_snapshots_total ")
	b.WriteString(strconv.FormatInt(int64(overview.Snapshots), 10))
	b.WriteString("\n# HELP maclaw_snapshot_bytes_total Total bytes used by persisted service snapshots\n")
	b.WriteString("# TYPE maclaw_snapshot_bytes_total gauge\n")
	b.WriteString("maclaw_snapshot_bytes_total ")
	b.WriteString(strconv.FormatInt(overview.SnapshotBytes, 10))
	b.WriteString("\n# HELP maclaw_audit_events_total Number of audit events\n")
	b.WriteString("# TYPE maclaw_audit_events_total gauge\n")
	b.WriteString("maclaw_audit_events_total ")
	b.WriteString(strconv.FormatInt(int64(overview.AuditEvents), 10))
	b.WriteString("\n# HELP maclaw_auth_token_failed_total Number of failed token exchanges recorded in audit events\n")
	b.WriteString("# TYPE maclaw_auth_token_failed_total counter\n")
	b.WriteString("maclaw_auth_token_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(authFailed)), 10))
	b.WriteString("\n# HELP maclaw_auth_token_rate_limited_total Number of rate-limited token exchanges recorded in audit events\n")
	b.WriteString("# TYPE maclaw_auth_token_rate_limited_total counter\n")
	b.WriteString("maclaw_auth_token_rate_limited_total ")
	b.WriteString(strconv.FormatInt(int64(len(rateLimited)), 10))
	b.WriteString("\n# HELP maclaw_admin_auth_failed_total Number of failed Admin API authentications recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_auth_failed_total counter\n")
	b.WriteString("maclaw_admin_auth_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminAuthFailed)), 10))
	b.WriteString("\n# HELP maclaw_admin_owner_denied_total Number of Admin API owner-only authorization denials recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_owner_denied_total counter\n")
	b.WriteString("maclaw_admin_owner_denied_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminOwnerDenied)), 10))
	b.WriteString("\n# HELP maclaw_admin_login_failed_total Number of failed Admin Web logins recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_login_failed_total counter\n")
	b.WriteString("maclaw_admin_login_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminLoginFailed)), 10))
	b.WriteString("\n# HELP maclaw_admin_login_rate_limited_total Number of rate-limited Admin Web logins recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_login_rate_limited_total counter\n")
	b.WriteString("maclaw_admin_login_rate_limited_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminLoginRateLimited)), 10))
	b.WriteString("\n# HELP maclaw_admin_password_change_failed_total Number of failed Admin Web password changes recorded in audit events\n")
	b.WriteString("# TYPE maclaw_admin_password_change_failed_total counter\n")
	b.WriteString("maclaw_admin_password_change_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(adminPasswordChangeFailed)), 10))
	b.WriteString("\n# HELP maclaw_instances_unready_total Number of instances currently not ready\n")
	b.WriteString("# TYPE maclaw_instances_unready_total gauge\n")
	b.WriteString("maclaw_instances_unready_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.UnreadyInstances)), 10))
	b.WriteString("\n# HELP maclaw_runs_waiting_for_user_total Number of runs currently waiting for user input\n")
	b.WriteString("# TYPE maclaw_runs_waiting_for_user_total gauge\n")
	b.WriteString("maclaw_runs_waiting_for_user_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.WaitingRuns)), 10))
	b.WriteString("\n# HELP maclaw_runs_failed_total Number of runs currently surfaced as failed alerts\n")
	b.WriteString("# TYPE maclaw_runs_failed_total gauge\n")
	b.WriteString("maclaw_runs_failed_total ")
	b.WriteString(strconv.FormatInt(int64(len(alerts.FailedRuns)), 10))
	b.WriteString("\n# HELP maclaw_run_succeeded_events_total Number of succeeded run audit events\n")
	b.WriteString("# TYPE maclaw_run_succeeded_events_total counter\n")
	b.WriteString("maclaw_run_succeeded_events_total ")
	b.WriteString(strconv.FormatInt(int64(len(runSucceeded)), 10))
	b.WriteString("\n# HELP maclaw_run_failed_events_total Number of failed run audit events\n")
	b.WriteString("# TYPE maclaw_run_failed_events_total counter\n")
	b.WriteString("maclaw_run_failed_events_total ")
	b.WriteString(strconv.FormatInt(int64(len(runFailed)), 10))
	b.WriteString("\n# HELP maclaw_async_jobs_total Number of async jobs by lifecycle status\n")
	b.WriteString("# TYPE maclaw_async_jobs_total gauge\n")
	for _, status := range []asyncJobStatus{asyncJobStatusPending, asyncJobStatusRunning, asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled} {
		b.WriteString("maclaw_async_jobs_total{status=\"")
		b.WriteString(string(status))
		b.WriteString("\"} ")
		b.WriteString(strconv.FormatInt(int64(jobCounts[status]), 10))
		b.WriteString("\n")
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
func (s *HTTPServer) handleGetAdminOverview(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAdminOverview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetAdminDashboard(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetAdminDashboard(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	dashboard := redactAdminDashboardForAdminAPI(s.svc.DataRoot(), *out)
	writeJSON(w, http.StatusOK, dashboard)
}
func (s *HTTPServer) handleGetAdminInsights(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	inactiveForDays := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("inactive_for_days")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid inactive_for_days"})
			return
		}
		inactiveForDays = parsed
	}
	out, err := s.svc.GetAdminInsights(r.Context(), agentservice.AdminInsightsInput{InactiveForDays: inactiveForDays, Limit: limit})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetAdminAlerts(w http.ResponseWriter, r *http.Request) {
	var since *time.Time
	sinceRaw := strings.TrimSpace(r.URL.Query().Get("since"))
	if sinceRaw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, sinceRaw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid since"})
			return
		}
		since = &parsed
	}
	limit := 0
	limitRaw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if limitRaw != "" {
		parsed, err := strconv.Atoi(limitRaw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	expiryWindowDays := 0
	expiryWindowRaw := strings.TrimSpace(r.URL.Query().Get("credential_expiry_window_days"))
	if expiryWindowRaw != "" {
		parsed, err := strconv.Atoi(expiryWindowRaw)
		if err != nil || parsed < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credential_expiry_window_days"})
			return
		}
		expiryWindowDays = parsed
	}
	out, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{
		TenantID:                   strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:                     strings.TrimSpace(r.URL.Query().Get("user_id")),
		Kind:                       strings.TrimSpace(r.URL.Query().Get("kind")),
		Since:                      since,
		Limit:                      limit,
		CredentialExpiryWindowDays: expiryWindowDays,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleAdminSecuritySummary(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateOptionalTimeRange(since, until); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.loadAdminRiskEvents(r.Context(), since, until)
	if err != nil {
		writeError(w, err)
		return
	}
	counts := countRiskEventsBySeverity(items)
	kindCounts := countRiskEventsByKind(items)
	status := "ok"
	if counts["high"] > 0 {
		status = "critical"
	} else if counts["medium"] > 0 {
		status = "warn"
	}
	recent := items
	if len(recent) > 10 {
		recent = recent[:10]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"filters":      map[string]any{"since": since, "until": until},
		"status":       status,
		"total":        len(items),
		"counts":       counts,
		"kind_counts":  kindCounts,
		"recent":       recent,
	})
}

func (s *HTTPServer) handleAdminSecurityRiskEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateOptionalTimeRange(since, until); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	severity := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("severity")))
	if severity != "" && !isValidRiskSeverity(severity) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid severity"})
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = parsed
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	items, err := s.loadAdminRiskEvents(r.Context(), since, until)
	if err != nil {
		writeError(w, err)
		return
	}
	if severity != "" {
		items = filterRiskEventsBySeverity(items, severity)
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	if kind != "" {
		items = filterRiskEventsByKind(items, kind)
	}
	total := len(items)
	counts := countRiskEventsBySeverity(items)
	kindCounts := countRiskEventsByKind(items)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"generated_at": time.Now().UTC(), "filters": map[string]any{"severity": severity, "kind": kind, "since": since, "until": until, "limit": limit}, "items": items, "total": total, "counts": counts, "kind_counts": kindCounts})
}

func (s *HTTPServer) loadAdminRiskEvents(ctx context.Context, since, until *time.Time) ([]adminRiskEvent, error) {
	audit, err := s.svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Since: since, Until: until})
	if err != nil {
		return nil, err
	}
	items := buildAdminRiskEvents(s.svc.DataRoot(), audit)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func filterRiskEventsBySeverity(items []adminRiskEvent, severity string) []adminRiskEvent {
	filtered := items[:0]
	for _, item := range items {
		if item.Severity == severity {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func filterRiskEventsByKind(items []adminRiskEvent, kind string) []adminRiskEvent {
	filtered := items[:0]
	for _, item := range items {
		if item.Kind == kind {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func isValidRiskSeverity(severity string) bool {
	switch severity {
	case "high", "medium", "low":
		return true
	default:
		return false
	}
}

func buildAdminRiskEvents(dataRoot string, audit []agentservice.AuditEvent) []adminRiskEvent {
	items := []adminRiskEvent{}
	now := time.Now().UTC()
	mode, _ := effectiveSandboxMode(dataRoot)
	strict, _ := effectiveSandboxStrict(dataRoot)
	if mode == "none" {
		items = append(items, adminRiskEvent{ID: "config:sandbox:none", Severity: "high", Kind: "sandbox_disabled", Summary: "Sandbox mode is none; local execution is not protected.", ResourceType: "sandbox", ResourceID: "runtime", CreatedAt: now})
	} else if !strict {
		items = append(items, adminRiskEvent{ID: "config:sandbox:not_strict", Severity: "medium", Kind: "sandbox_not_strict", Summary: "Sandbox strict mode is disabled; execution may fall back when sandbox is unavailable.", ResourceType: "sandbox", ResourceID: "runtime", CreatedAt: now})
	}
	if adminEnvBool("MACLAW_ALLOW_INSECURE_HTTP", false) {
		items = append(items, adminRiskEvent{ID: "config:http:insecure", Severity: "high", Kind: "insecure_http", Summary: "Non-loopback plaintext HTTP is allowed by service config.", ResourceType: "service_config", ResourceID: "allow_insecure_http", CreatedAt: now})
	}
	for _, event := range audit {
		if risk, ok := riskEventFromAudit(dataRoot, event); ok {
			items = append(items, risk)
		}
	}
	return items
}

func riskEventFromAudit(dataRoot string, event agentservice.AuditEvent) (adminRiskEvent, bool) {
	severity := ""
	kind := ""
	summary := ""
	switch event.Action {
	case "auth.token_rate_limited", "admin.login_rate_limited":
		severity, kind, summary = "high", "auth_rate_limited", "Authentication rate limit was triggered."
	case "auth.token_failed", "admin.auth_failed", "admin.login_failed", "admin.bootstrap_failed", "admin.password_change_failed":
		severity, kind, summary = "medium", "auth_failed", "Authentication or admin credential validation failed."
	case "admin.owner_required_failed":
		severity, kind, summary = "medium", "admin_authorization_denied", "An Admin Web operator attempted an owner-only operation."
	case "admin.sandbox_diagnose_failed", "admin.sandbox_startup_diagnose_failed", "admin.sandbox_smoke_test_failed":
		severity, kind, summary = "high", "sandbox_failed", "Sandbox verification failed."
	case "admin.sandbox_install_failed":
		severity, kind, summary = "high", "sandbox_install_failed", "Sandbox installation failed."
	case "admin.service_state_exported":
		if event.Metadata["include_secrets"] == "true" {
			severity, kind, summary = "high", "service_state_secrets_exported", "Service state was exported with secrets included."
		} else {
			severity, kind, summary = "medium", "service_state_exported", "Service state was exported."
		}
	case "admin.service_state_imported":
		if event.Metadata["dry_run"] == "true" {
			severity, kind, summary = "medium", "service_state_import_planned", "Service state import dry-run was performed."
		} else {
			severity, kind, summary = "high", "service_state_imported", "Service state was imported into this server."
		}
	case "admin.snapshot_created":
		if event.Metadata["include_secrets"] == "true" {
			severity, kind, summary = "high", "snapshot_secrets_created", "A service snapshot was created with secrets included."
		} else {
			severity, kind, summary = "medium", "snapshot_created", "A service snapshot was created."
		}
	case "admin.snapshot_restored", "snapshot.restored":
		if event.Metadata["dry_run"] == "true" {
			severity, kind, summary = "medium", "snapshot_restore_planned", "Snapshot restore dry-run was performed."
		} else {
			severity, kind, summary = "high", "snapshot_restored", "A service snapshot was restored."
		}
	case "admin.credential_created":
		severity, kind, summary = "high", "credential_created", "An admin credential was created and one-time secrets may have been returned."
	case "admin.credential_key_rotated", "admin.credential_secret_rotated":
		severity, kind, summary = "high", "credential_rotated", "An admin credential key or secret was rotated."
	case "admin.credential_updated":
		severity, kind, summary = "medium", "credential_updated", "An admin credential was updated."
	case "admin.credential_revoked":
		severity, kind, summary = "medium", "credential_revoked", "An admin credential was revoked."
	case "admin.sandbox_config_updated", "admin.sandbox_config_rolled_back", "admin.sandbox_profile_updated", "admin.sandbox_profile_deleted", "admin.sandbox_report_deleted":
		severity, kind, summary = "medium", "sandbox_admin_changed", "Sandbox admin configuration or diagnostics were changed."
	case "admin.sandbox_install_started", "admin.sandbox_install_succeeded":
		severity, kind, summary = "high", "sandbox_install_changed", "Sandbox installation was requested or completed from Admin Web."
	case "admin.service_config_draft_updated", "admin.service_config_draft_cleared", "admin.service_config_export_plan":
		severity, kind, summary = "medium", "service_config_changed", "Service configuration draft or export plan was changed."
	case "admin.knowledge_access_cross_tenant_updated", "admin.knowledge_access_user_updated", "admin.knowledge_access_user_deleted", "admin.knowledge_tenant_cleared":
		severity, kind, summary = "medium", "knowledge_policy_changed", "Knowledge access policy or tenant knowledge data was changed."
	case "admin.skill_sources_global_updated", "admin.skill_sources_tenant_updated", "admin.skill_sources_tenant_deleted", "admin.skill_sources_user_updated", "admin.skill_sources_user_deleted":
		severity, kind, summary = "medium", "skill_source_policy_changed", "Skill source policy was changed."
	case "admin.support_bundle_downloaded", "admin.sandbox_support_bundle_downloaded":
		severity, kind, summary = "medium", "diagnostics_bundle_downloaded", "An admin downloaded a troubleshooting bundle."
	case "admin.logs_rotate":
		severity, kind, summary = "medium", "log_rotated", "An admin log source was rotated."
	case "admin.job_cancel":
		severity, kind, summary = "medium", "job_canceled", "An admin canceled a background job."
	case "admin.runtime_gc":
		severity, kind, summary = "low", "runtime_gc", "An admin triggered runtime garbage collection."
	}
	if severity == "" {
		return adminRiskEvent{}, false
	}
	return adminRiskEvent{ID: event.ID, Severity: severity, Kind: kind, Summary: summary, Action: event.Action, ResourceType: event.ResourceType, ResourceID: redactSupportBundleValue(dataRoot, event.ResourceID), Metadata: redactSupportBundleMetadata(dataRoot, event.Metadata), CreatedAt: event.CreatedAt}, true
}

func countRiskEventsBySeverity(items []adminRiskEvent) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		out[item.Severity]++
	}
	return out
}

func countRiskEventsByKind(items []adminRiskEvent) map[string]int {
	out := map[string]int{}
	for _, item := range items {
		out[item.Kind]++
	}
	return out
}

func (s *HTTPServer) handleListTenants(w http.ResponseWriter, r *http.Request) {
	status, ok := parseTenantStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{
		Status: status,
		Name:   strings.TrimSpace(r.URL.Query().Get("name")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateTenants(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{
		TenantID:     strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:       strings.TrimSpace(r.URL.Query().Get("user_id")),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
		ResourceID:   strings.TrimSpace(r.URL.Query().Get("resource_id")),
		ActorType:    strings.TrimSpace(r.URL.Query().Get("actor_type")),
		ActorTenant:  strings.TrimSpace(r.URL.Query().Get("actor_tenant_id")),
		ActorUser:    strings.TrimSpace(r.URL.Query().Get("actor_user_id")),
		Since:        since,
		Until:        until,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateAuditEvents(out, page)
	items = redactAuditEventsForAdminAPI(s.svc.DataRoot(), items)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleExportServiceState(w http.ResponseWriter, r *http.Request) {
	includeMessages, err := parseOptionalBoolQuery(r, "include_messages")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeRuns, err := parseOptionalBoolQuery(r, "include_runs")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeAudit, err := parseOptionalBoolQuery(r, "include_audit")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	includeSecrets, err := parseOptionalBoolQuery(r, "include_secrets")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, includeSecrets != nil && *includeSecrets) {
		return
	}
	out, err := s.svc.ExportServiceState(r.Context(), agentservice.ExportServiceStateInput{
		TenantID:        strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:          strings.TrimSpace(r.URL.Query().Get("user_id")),
		IncludeMessages: includeMessages == nil || *includeMessages,
		IncludeRuns:     includeRuns == nil || *includeRuns,
		IncludeAudit:    includeAudit == nil || *includeAudit,
		IncludeSecrets:  includeSecrets != nil && *includeSecrets,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.service_state_exported", "service_state", out.Scope, map[string]string{"tenant_id": out.TenantID, "user_id": out.UserID, "include_secrets": strconv.FormatBool(out.IncludeSecrets), "include_messages": strconv.FormatBool(out.IncludeMessages), "include_runs": strconv.FormatBool(out.IncludeRuns), "include_audit": strconv.FormatBool(out.IncludeAudit), "users": strconv.Itoa(len(out.Users)), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleImportServiceState(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	in, ok := decodeImportStateRequest(w, r)
	if !ok {
		return
	}
	if overwrite, err := parseOptionalBoolQuery(r, "overwrite"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if overwrite != nil {
		in.Overwrite = *overwrite
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	if !in.DryRun {
		if err := requireAdminConfirmation(r, "import operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.ImportServiceState(r.Context(), *in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.service_state_imported", "service_state", out.Scope, map[string]string{"tenant_id": out.TenantID, "user_id": out.UserID, "dry_run": strconv.FormatBool(out.DryRun), "overwrite": strconv.FormatBool(out.Overwrite), "tenants": strconv.Itoa(out.Tenants), "users": strconv.Itoa(out.Users), "credentials": strconv.Itoa(out.Credentials), "instances": strconv.Itoa(out.Instances), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleListServiceSnapshots(w http.ResponseWriter, r *http.Request) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.svc.ListServiceSnapshots(r.Context(), agentservice.ListServiceSnapshotsInput{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:   strings.TrimSpace(r.URL.Query().Get("user_id")),
		Scope:    strings.TrimSpace(r.URL.Query().Get("scope")),
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Since:    since,
		Until:    until,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	window, meta := paginateServiceSnapshots(items, page)
	writeJSON(w, http.StatusOK, listResponse(sanitizeServiceSnapshotsForAdminAPI(window), meta))
}

func (s *HTTPServer) handleCreateServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateServiceSnapshotInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.IncludeSecrets != nil && *in.IncludeSecrets {
		if err := requireAdminConfirmation(r, "secret snapshot operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.CreateServiceSnapshot(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.snapshot_created", "snapshot", out.Snapshot.ID, map[string]string{"scope": out.Snapshot.Scope, "tenant_id": out.Snapshot.TenantID, "user_id": out.Snapshot.UserID, "include_secrets": strconv.FormatBool(out.Snapshot.IncludeSecrets), "include_messages": strconv.FormatBool(out.Snapshot.IncludeMessages), "include_runs": strconv.FormatBool(out.Snapshot.IncludeRuns), "include_audit": strconv.FormatBool(out.Snapshot.IncludeAudit), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusCreated, sanitizeServiceSnapshotEnvelopeForAdminAPI(out))
}

func (s *HTTPServer) handlePruneServiceSnapshots(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.PruneServiceSnapshotsInput
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	if tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantID != "" {
		in.TenantID = tenantID
	}
	if userID := strings.TrimSpace(r.URL.Query().Get("user_id")); userID != "" {
		in.UserID = userID
	}
	if olderThan, err := parseOptionalTimeQuery(r, "older_than"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if olderThan != nil {
		in.OlderThan = olderThan
	}
	if keepLatestRaw := strings.TrimSpace(r.URL.Query().Get("keep_latest")); keepLatestRaw != "" {
		keepLatest, err := strconv.Atoi(keepLatestRaw)
		if err != nil || keepLatest < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "keep_latest must be greater than or equal to 0"})
			return
		}
		in.KeepLatest = keepLatest
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	out, err := s.svc.PruneServiceSnapshots(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sanitizePruneServiceSnapshotsOutputForAdminAPI(out))
}
func (s *HTTPServer) handleGetServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetServiceSnapshot(r.Context(), r.PathValue("snapshotId"))
	if err != nil {
		writeError(w, err)
		return
	}
	if out.Snapshot.IncludeSecrets && !s.requireAdminOwner(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, sanitizeServiceSnapshotEnvelopeForAdminAPI(out))
}

func (s *HTTPServer) handleRestoreServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RestoreServiceSnapshotInput
	if !decodeOptionalJSON(w, r, &in) {
		return
	}
	if overwrite, err := parseOptionalBoolQuery(r, "overwrite"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if overwrite != nil {
		in.Overwrite = *overwrite
	}
	if dryRun, err := parseOptionalBoolQuery(r, "dry_run"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	} else if dryRun != nil {
		in.DryRun = *dryRun
	}
	if !in.DryRun {
		if err := requireAdminConfirmation(r, "restore operations"); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	out, err := s.svc.RestoreServiceSnapshot(r.Context(), r.PathValue("snapshotId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.snapshot_restored", "snapshot", out.Snapshot.ID, map[string]string{"scope": out.Snapshot.Scope, "tenant_id": out.Snapshot.TenantID, "user_id": out.Snapshot.UserID, "dry_run": strconv.FormatBool(in.DryRun), "overwrite": strconv.FormatBool(in.Overwrite), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, sanitizeRestoreServiceSnapshotOutputForAdminAPI(out))
}
func (s *HTTPServer) handleDeleteServiceSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.DeleteServiceSnapshot(r.Context(), r.PathValue("snapshotId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sanitizeServiceSnapshotForAdminAPI(*out))
}
func (s *HTTPServer) handleGetTenantRetirePlan(w http.ResponseWriter, r *http.Request) {
	in, err := parseExportServiceStateInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, in.IncludeSecrets) {
		return
	}
	out, err := s.svc.GetTenantRetirePlan(r.Context(), r.PathValue("tenantId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleGetUserRetirePlan(w http.ResponseWriter, r *http.Request) {
	in, err := parseExportServiceStateInput(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !s.requireSecretExportAccess(w, r, in.IncludeSecrets) {
		return
	}
	out, err := s.svc.GetUserRetirePlan(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func sanitizeServiceSnapshotForAdminAPI(snapshot agentservice.ServiceSnapshot) agentservice.ServiceSnapshot {
	snapshot.Path = ""
	return snapshot
}

func sanitizeServiceSnapshotsForAdminAPI(items []agentservice.ServiceSnapshot) []agentservice.ServiceSnapshot {
	out := make([]agentservice.ServiceSnapshot, len(items))
	for i, item := range items {
		out[i] = sanitizeServiceSnapshotForAdminAPI(item)
	}
	return out
}

func sanitizeServiceSnapshotEnvelopeForAdminAPI(in *agentservice.ServiceSnapshotEnvelope) agentservice.ServiceSnapshotEnvelope {
	if in == nil {
		return agentservice.ServiceSnapshotEnvelope{}
	}
	out := *in
	out.Snapshot = sanitizeServiceSnapshotForAdminAPI(out.Snapshot)
	return out
}

func sanitizeRestoreServiceSnapshotOutputForAdminAPI(in *agentservice.RestoreServiceSnapshotOutput) agentservice.RestoreServiceSnapshotOutput {
	if in == nil {
		return agentservice.RestoreServiceSnapshotOutput{}
	}
	out := *in
	out.Snapshot = sanitizeServiceSnapshotForAdminAPI(out.Snapshot)
	return out
}

func sanitizePruneServiceSnapshotsOutputForAdminAPI(in *agentservice.PruneServiceSnapshotsOutput) agentservice.PruneServiceSnapshotsOutput {
	if in == nil {
		return agentservice.PruneServiceSnapshotsOutput{}
	}
	out := *in
	out.KeptSnapshots = sanitizeServiceSnapshotsForAdminAPI(out.KeptSnapshots)
	out.Snapshots = sanitizeServiceSnapshotsForAdminAPI(out.Snapshots)
	return out
}
func (s *HTTPServer) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateTenant(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_created", "tenant", out.ID, tenantAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}
func tenantAuditMetadata(r *http.Request, tenant *agentservice.Tenant) map[string]string {
	return map[string]string{
		"name":             tenant.Name,
		"status":           string(tenant.Status),
		"delete_protected": strconv.FormatBool(tenant.DeleteProtected),
		"remote_ip":        requestClientIP(r),
	}
}

func userAuditMetadata(r *http.Request, user *agentservice.User) map[string]string {
	return map[string]string{
		"tenant_id":        user.TenantID,
		"name":             user.Name,
		"email":            user.Email,
		"status":           string(user.Status),
		"delete_protected": strconv.FormatBool(user.DeleteProtected),
		"remote_ip":        requestClientIP(r),
	}
}

func credentialAuditMetadata(r *http.Request, cred *agentservice.Credential) map[string]string {
	metadata := map[string]string{
		"tenant_id":           cred.TenantID,
		"user_id":             cred.UserID,
		"name":                cred.Name,
		"status":              string(cred.Status),
		"api_key_prefix":      cred.APIKeyPrefix,
		"token_version":       strconv.Itoa(cred.TokenVersion),
		"has_expires_at":      strconv.FormatBool(cred.ExpiresAt != nil),
		"returned_api_key":    strconv.FormatBool(cred.APIKey != ""),
		"returned_api_secret": strconv.FormatBool(cred.APISecret != ""),
		"remote_ip":           requestClientIP(r),
	}
	if cred.ExpiresAt != nil {
		metadata["expires_at"] = cred.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return metadata
}
func (s *HTTPServer) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenant(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetTenantSummary(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenantSummary(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetTenantDeleteCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetTenantDeleteCheck(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateTenant(r.Context(), r.PathValue("tenantId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_updated", "tenant", out.ID, tenantAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handlePauseTenant(w http.ResponseWriter, r *http.Request) {
	s.updateTenantLifecycleStatus(w, r, agentservice.TenantStatusDisabled, "admin.tenant_paused")
}

func (s *HTTPServer) handleResumeTenant(w http.ResponseWriter, r *http.Request) {
	s.updateTenantLifecycleStatus(w, r, agentservice.TenantStatusActive, "admin.tenant_resumed")
}

func (s *HTTPServer) updateTenantLifecycleStatus(w http.ResponseWriter, r *http.Request, status agentservice.TenantStatus, action string) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.UpdateTenant(r.Context(), r.PathValue("tenantId"), agentservice.UpdateTenantInput{Status: &status})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), action, "tenant", out.ID, map[string]string{"status": string(out.Status), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tenantID := r.PathValue("tenantId")
	if err := s.svc.DeleteTenant(r.Context(), tenantID); err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.tenant_deleted", "tenant", tenantID, map[string]string{"remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleListAllUsers(w http.ResponseWriter, r *http.Request) {
	status, ok := parseUserStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListAllUsers(r.Context(), agentservice.ListAllUsersAdminInput{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		Status:   status,
		Name:     strings.TrimSpace(r.URL.Query().Get("name")),
		Email:    strings.TrimSpace(r.URL.Query().Get("email")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateUsers(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	status, ok := parseUserStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out, err := s.svc.ListUsers(r.Context(), r.PathValue("tenantId"), agentservice.ListUsersAdminInput{
		Status: status,
		Name:   strings.TrimSpace(r.URL.Query().Get("name")),
		Email:  strings.TrimSpace(r.URL.Query().Get("email")),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateUsers(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateUserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.TenantID = r.PathValue("tenantId")
	out, err := s.svc.CreateUser(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_created", "user", out.ID, userAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}
func (s *HTTPServer) handleGetUser(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetUserDeleteCheck(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetUserDeleteCheck(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateUserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_updated", "user", out.ID, userAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handlePauseUser(w http.ResponseWriter, r *http.Request) {
	s.updateUserLifecycleStatus(w, r, agentservice.UserStatusDisabled, "admin.user_paused")
}

func (s *HTTPServer) handleResumeUser(w http.ResponseWriter, r *http.Request) {
	s.updateUserLifecycleStatus(w, r, agentservice.UserStatusActive, "admin.user_resumed")
}

func (s *HTTPServer) updateUserLifecycleStatus(w http.ResponseWriter, r *http.Request, status agentservice.UserStatus, action string) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.UpdateUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), agentservice.UpdateUserInput{Status: &status})
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), action, "user", out.ID, map[string]string{"tenant_id": out.TenantID, "status": string(out.Status), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireDeleteConfirmation(r); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	tenantID := r.PathValue("tenantId")
	userID := r.PathValue("userId")
	if err := s.svc.DeleteUser(r.Context(), tenantID, userID); err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_deleted", "user", userID, map[string]string{"tenant_id": tenantID, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCredentials(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	out, err = filterCredentialsByQuery(out, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateCredentials(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCreateCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.CreateCredentialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.TenantID = r.PathValue("tenantId")
	in.UserID = r.PathValue("userId")
	out, err := s.svc.CreateCredential(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_created", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusCreated, out)
}
func (s *HTTPServer) handleGetCredential(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.GetCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.UpdateCredentialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_updated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRotateCredentialSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RotateCredentialSecretInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialSecret(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_secret_rotated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRotateCredentialKey(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in agentservice.RotateCredentialKeyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialAPIKey(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_key_rotated", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	out, err := s.svc.RevokeCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"))
	if err != nil {
		writeError(w, err)
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.credential_revoked", "credential", out.ID, credentialAuditMetadata(r, out))
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var in agentservice.IssueTokenInput
	if !decodeJSON(w, r, &in) {
		return
	}
	clientIP := requestClientIP(r)
	limitKey := clientIP + ":" + strings.TrimSpace(in.APIKey)
	now := time.Now()
	if allowed, retryAfter := s.authLimiter.AllowWithRetry(limitKey, now); !allowed {
		_ = s.svc.RecordTokenRateLimit(r.Context(), in.APIKey, clientIP)
		writeRateLimitError(w, retryAfter)
		return
	}
	out, err := s.svc.IssueToken(r.Context(), in)
	if err != nil {
		if errors.Is(err, agentservice.ErrUnauthorized) || errors.Is(err, agentservice.ErrForbidden) {
			blockFor := s.authLimiter.RegisterFailure(limitKey, now)
			_ = s.svc.RecordTokenAuthFailure(r.Context(), in.APIKey, clientIP, err.Error())
			if blockFor > 0 {
				writeRateLimitError(w, blockFor)
				return
			}
		}
		writeError(w, err)
		return
	}
	s.authLimiter.ResetFailures(limitKey)
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetMe(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMe(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetConfigSchema(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetParameterDefinitions(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *HTTPServer) handleGetConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{"app_config": corelib.AppConfig{}})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	// Accept both raw AppConfig JSON and {"app_config": {...}} envelope format.
	inPtr, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	if inPtr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty or invalid config body"})
		return
	}
	out, err := s.svc.UpdateUserConfig(r.Context(), p, *inPtr)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleValidateConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ValidateConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleTestConfig(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	candidate, ok := decodeOptionalAppConfig(w, r)
	if !ok {
		return
	}
	out, err := s.svc.TestConfigCandidate(r.Context(), p, candidate)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetUsageSummary(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetUsageSummary(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleListMCPServers(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListMCPServers(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateMCPServers(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCreateMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPServerCreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("mcp.create", p, func(ctx context.Context) (any, error) {
			return s.svc.CreateMCPServer(ctx, p, in)
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.CreateMCPServer(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *HTTPServer) handleGetMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPServerUpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("mcp.update", p, func(ctx context.Context) (any, error) {
			return s.svc.UpdateMCPServer(ctx, p, r.PathValue("serverId"), in)
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.UpdateMCPServer(r.Context(), p, r.PathValue("serverId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteMCPServer(r.Context(), p, r.PathValue("serverId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleStartMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("mcp.start", p, func(ctx context.Context) (any, error) {
			return s.svc.StartMCPServer(ctx, p, r.PathValue("serverId"))
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.StartMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleStopMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("mcp.stop", p, func(ctx context.Context) (any, error) {
			return s.svc.StopMCPServer(ctx, p, r.PathValue("serverId"))
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.StopMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleCheckMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("mcp.health_check", p, func(ctx context.Context) (any, error) {
			return s.svc.CheckMCPServer(ctx, p, r.PathValue("serverId"))
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.CheckMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetMCPServerTools(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetMCPServerTools(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *HTTPServer) handleListSkills(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListSkills(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parseSkillPageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateSkills(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleGetSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteSkill(r.Context(), p, r.PathValue("skillName")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleSearchSkills(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillSearchInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.SearchSkills(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *HTTPServer) handleInstallSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillInstallInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("skill.install", p, func(ctx context.Context) (any, error) {
			out, err := s.svc.InstallSkill(ctx, p, in)
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": out}, nil
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.InstallSkill(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": out})
}
func (s *HTTPServer) handleImportSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillImportInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("skill.import", p, func(ctx context.Context) (any, error) {
			out, err := s.svc.ImportSkillArchive(ctx, p, in)
			if err != nil {
				return nil, err
			}
			return map[string]any{"items": out}, nil
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.ImportSkillArchive(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": out})
}
func (s *HTTPServer) handleExportSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ExportSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleValidateSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ValidateSkill(r.Context(), p, r.PathValue("skillName"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleImproveSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillImproveInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.ImproveSkill(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUploadSkill(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SkillUploadInput
	if !decodeJSON(w, r, &in) {
		return
	}
	asyncMode, err := parseRequiredBoolLikeQuery(r, "async")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if asyncMode {
		job := s.jobs.createUserJob("skill.upload", p, func(ctx context.Context) (any, error) {
			return s.svc.UploadSkill(ctx, p, r.PathValue("skillName"), in)
		})
		writeJSON(w, http.StatusAccepted, job)
		return
	}
	out, err := s.svc.UploadSkill(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleListAsyncJobs(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	status, ok := parseAsyncJobStatus(r.URL.Query().Get("status"), false)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	out := s.jobs.listUserJobs(p, kind, status)
	items, meta := paginateAsyncJobs(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleDeleteAsyncJobs(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
	statusRaw := strings.TrimSpace(r.URL.Query().Get("status"))
	status, ok := parseAsyncJobStatus(statusRaw, true)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be succeeded, failed, or canceled"})
		return
	}
	before, err := parseOptionalTimeQuery(r, "before")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	deleteAll, err := parseOptionalBoolQuery(r, "all")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if (deleteAll == nil || !*deleteAll) && kind == "" && statusRaw == "" && before == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "specify kind, status, before, or all=true"})
		return
	}
	items := s.jobs.deleteUserJobs(p, kind, status, before)
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "deleted": len(items), "items": items})
}

func (s *HTTPServer) handleGetAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, ok := s.jobs.getUserJob(r.PathValue("jobId"), p)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) handleCancelAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, ok := s.jobs.cancelUserJob(r.PathValue("jobId"), p)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *HTTPServer) handleDeleteAsyncJob(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	job, found, deleted := s.jobs.deleteUserJob(r.PathValue("jobId"), p)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if !deleted {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "job is still active", "job": job})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "job": job})
}

func (s *HTTPServer) handleListStructuredRecords(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListStructuredRecords(r.Context(), p, agentservice.ListStructuredRecordsInput{
		Collection: r.PathValue("collection"),
		Tag:        strings.TrimSpace(r.URL.Query().Get("tag")),
		Q:          strings.TrimSpace(r.URL.Query().Get("q")),
		Limit:      page.Limit,
		Before:     formatOptionalCursorTime(page.Before),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	items, meta := recordsPageMeta(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleCreateStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateStructuredRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.Collection = r.PathValue("collection")
	out, err := s.svc.CreateStructuredRecord(r.Context(), p, in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *HTTPServer) handleGetStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleUpdateStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateStructuredRecordInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleDeleteStructuredRecord(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteStructuredRecord(r.Context(), p, r.PathValue("collection"), r.PathValue("recordId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *HTTPServer) handleGetSkillUploadStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkillUploadStatus(r.Context(), p, r.PathValue("submissionId"), strings.TrimSpace(r.URL.Query().Get("base_url")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetSkillMarketAccount(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSkillMarketAccount(r.Context(), p, strings.TrimSpace(r.URL.Query().Get("base_url")), strings.TrimSpace(r.URL.Query().Get("email")))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleListInstances(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListInstances(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateInstances(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCreateInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateInstanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateInstance(r.Context(), p, in)
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			validation, vErr := s.svc.ValidateUserConfig(r.Context(), p)
			if vErr == nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "config_validation": validation})
				return
			}
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *HTTPServer) handleGetInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateInstanceInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateInstance(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteInstance(r.Context(), p, r.PathValue("instanceId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleGetInstanceCapabilities(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceCapabilities(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleStopInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.StopInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleResumeInstance(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ResumeInstance(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) && out != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "instance": out, "config_validation": out.ConfigValidation})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRefreshInstanceReadiness(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.RefreshInstanceReadiness(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetInstanceSummary(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceSummary(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleGetInstanceBootstrap(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetInstanceBootstrap(r.Context(), p, r.PathValue("instanceId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleSendMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.SendMessageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	sess, run, msg, err := s.svc.SendMessage(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		if run != nil {
			status := http.StatusBadGateway
			if run.Status == agentservice.RunStatusCancelled {
				status = http.StatusConflict
			}
			writeJSON(w, status, map[string]any{"session": sess, "run": run, "message": msg, "error": err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "run": run, "message": msg})
}
func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	includeArchived, err := parseOptionalBoolQuery(r, "include_archived")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.ListSessions(r.Context(), p, r.PathValue("instanceId"), agentservice.ListSessionsInput{
		IncludeArchived: includeArchived != nil && *includeArchived,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateSessions(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCreateSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.CreateSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateSession(r.Context(), p, r.PathValue("instanceId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *HTTPServer) handleGetSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleUpdateSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.UpdateSessionInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := s.svc.DeleteSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleArchiveSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ArchiveSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRestoreSession(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.RestoreSession(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleListMessages(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	since, err := parseOptionalTimeQuery(r, "since")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	until, err := parseOptionalTimeQuery(r, "until")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	role, ok := parseMessageRole(r.URL.Query().Get("role"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid role"})
		return
	}
	out, err := s.svc.ListMessages(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), agentservice.ListMessagesInput{
		Role:  role,
		Since: since,
		Until: until,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateMessages(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handlePostMessage(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.PostMessageInput
	if !decodeJSON(w, r, &in) {
		return
	}
	run, msg, err := s.svc.PostMessage(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"), in)
	if err != nil {
		if run != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"run": run, "error": err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": run, "message": msg})
}
func (s *HTTPServer) handleGetRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.GetRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type runStreamSnapshot struct {
	Run              *agentservice.Run     `json:"run"`
	Session          *agentservice.Session `json:"session,omitempty"`
	AssistantMessage *agentservice.Message `json:"assistant_message,omitempty"`
}

type runStreamEnvelope struct {
	Type     string             `json:"type"`
	Snapshot *runStreamSnapshot `json:"snapshot,omitempty"`
}

func (s *HTTPServer) handleStreamRunEvents(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	instanceID := r.PathValue("instanceId")
	runID := r.PathValue("runId")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastPayload := ""
	sendSnapshot := func(eventType string, snap *runStreamSnapshot) bool {
		payload, err := json.Marshal(runStreamEnvelope{Type: eventType, Snapshot: snap})
		if err != nil {
			return false
		}
		if eventType == "snapshot" && string(payload) == lastPayload {
			return true
		}
		if eventType == "snapshot" {
			lastPayload = string(payload)
		}
		if _, err := w.Write([]byte("event: " + eventType + "\n")); err != nil {
			return false
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return false
		}
		if _, err := w.Write(payload); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	snapshot, err := s.loadRunStreamSnapshot(r.Context(), p, instanceID, runID)
	if err != nil {
		writeSSEError(w, flusher, err)
		return
	}
	if !sendSnapshot("snapshot", snapshot) {
		return
	}
	if snapshot.Run != nil && snapshot.Run.Status != agentservice.RunStatusRunning {
		_ = sendSnapshot("done", snapshot)
		return
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			snapshot, err := s.loadRunStreamSnapshot(r.Context(), p, instanceID, runID)
			if err != nil {
				writeSSEError(w, flusher, err)
				return
			}
			if !sendSnapshot("snapshot", snapshot) {
				return
			}
			if snapshot.Run != nil && snapshot.Run.Status != agentservice.RunStatusRunning {
				_ = sendSnapshot("done", snapshot)
				return
			}
		}
	}
}

func (s *HTTPServer) loadRunStreamSnapshot(ctx context.Context, p agentservice.Principal, instanceID, runID string) (*runStreamSnapshot, error) {
	run, err := s.svc.GetRun(ctx, p, instanceID, runID)
	if err != nil {
		return nil, err
	}
	snap := &runStreamSnapshot{Run: run}
	if run == nil || strings.TrimSpace(run.SessionID) == "" {
		return snap, nil
	}
	sess, err := s.svc.GetSession(ctx, p, instanceID, run.SessionID)
	if err != nil && !errors.Is(err, agentservice.ErrSessionNotFound) {
		return nil, err
	}
	if err == nil {
		snap.Session = sess
	}
	if strings.TrimSpace(run.AssistantMessageID) == "" {
		return snap, nil
	}
	messages, err := s.svc.ListMessages(ctx, p, instanceID, run.SessionID, agentservice.ListMessagesInput{})
	if err != nil {
		return nil, err
	}
	for i := range messages {
		if messages[i].ID == run.AssistantMessageID {
			msg := messages[i]
			snap.AssistantMessage = &msg
			break
		}
	}
	return snap, nil
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher, err error) {
	payload, _ := json.Marshal(map[string]string{"error": err.Error()})
	_, _ = w.Write([]byte("event: error\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
	flusher.Flush()
}
func (s *HTTPServer) handleListRuns(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	waitingForUser, err := parseOptionalBoolQuery(r, "waiting_for_user")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	status, ok := parseRunStatus(r.URL.Query().Get("status"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}
	responseSource, ok := parseRunResponseSource(r.URL.Query().Get("response_source"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid response_source"})
		return
	}
	out, err := s.svc.ListRuns(r.Context(), p, r.PathValue("instanceId"), agentservice.ListRunsInput{
		Status:         status,
		SessionID:      strings.TrimSpace(r.URL.Query().Get("session_id")),
		ResponseSource: responseSource,
		WaitingForUser: waitingForUser,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateRuns(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}
func (s *HTTPServer) handleCancelRun(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.CancelRun(r.Context(), p, r.PathValue("instanceId"), r.PathValue("runId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setAdminSecurityHeaders(w)
		provided := r.Header.Get("X-MaClaw-Admin-Secret")
		if s.adminSecretAuthorized(provided) {
			ctx := contextWithAdminAuditIdentity(r.Context(), adminAuditIdentity{AuthType: "admin_secret"})
			next(w, r.WithContext(ctx))
			return
		}
		session, user, err := getAdminSessionUser(s.svc.DataRoot(), provided, time.Now().UTC())
		if err != nil {
			_ = s.recordAdminAudit(r.Context(), "admin.auth_failed", "admin_auth", strings.TrimSpace(r.URL.Path), map[string]string{"method": r.Method, "remote_ip": requestClientIP(r)})
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin secret"})
			return
		}
		ctx := contextWithAdminAuditIdentity(r.Context(), adminAuditIdentity{AuthType: "admin_session", SessionID: session.ID, UserID: user.ID, Username: user.Username, Role: user.Role})
		next(w, r.WithContext(ctx))
	}
}
func (s *HTTPServer) withPrincipal(next func(http.ResponseWriter, *http.Request, agentservice.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(authz, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": agentservice.ErrUnauthorized.Error()})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
		if token == "" || strings.Contains(token, " ") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": agentservice.ErrUnauthorized.Error()})
			return
		}
		p, err := s.svc.Authenticate(token)
		if err != nil {
			writeError(w, err)
			return
		}
		next(w, r, *p)
	}
}
func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return false
	}
	return true
}
func (s *HTTPServer) requireSecretExportAccess(w http.ResponseWriter, r *http.Request, includeSecrets bool) bool {
	if !includeSecrets {
		return true
	}
	if !s.requireAdminOwner(w, r) {
		return false
	}
	if err := requireAdminConfirmation(r, "secret export operations"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func parseExportServiceStateInput(r *http.Request) (agentservice.ExportServiceStateInput, error) {
	includeMessages, err := parseOptionalBoolQuery(r, "include_messages")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeRuns, err := parseOptionalBoolQuery(r, "include_runs")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeAudit, err := parseOptionalBoolQuery(r, "include_audit")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	includeSecrets, err := parseOptionalBoolQuery(r, "include_secrets")
	if err != nil {
		return agentservice.ExportServiceStateInput{}, err
	}
	return agentservice.ExportServiceStateInput{
		IncludeMessages: includeMessages == nil || *includeMessages,
		IncludeRuns:     includeRuns == nil || *includeRuns,
		IncludeAudit:    includeAudit == nil || *includeAudit,
		IncludeSecrets:  includeSecrets != nil && *includeSecrets,
	}, nil
}

func decodeImportStateRequest(w http.ResponseWriter, r *http.Request) (*agentservice.ImportServiceStateRequest, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body is required"})
		return nil, false
	}
	var envelope importStateEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && (envelope.Data.Scope != "" || len(envelope.Data.Tenants) > 0 || len(envelope.Data.Users) > 0 || len(envelope.Data.AuditEvents) > 0) {
		return &agentservice.ImportServiceStateRequest{Data: envelope.Data, Overwrite: envelope.Overwrite, DryRun: envelope.DryRun}, true
	}
	var raw agentservice.ExportServiceStateOutput
	if err := json.Unmarshal(body, &raw); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return nil, false
	}
	return &agentservice.ImportServiceStateRequest{Data: raw}, true
}

func decodeOptionalAppConfig(w http.ResponseWriter, r *http.Request) (*corelib.AppConfig, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read request body"})
		return nil, false
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, true
	}

	var envelope configEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && !isZeroAppConfig(envelope.AppConfig) {
		return &envelope.AppConfig, true
	}

	var cfg corelib.AppConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return nil, false
	}
	return &cfg, true
}
func isZeroAppConfig(cfg corelib.AppConfig) bool {
	return reflect.DeepEqual(cfg, corelib.AppConfig{})
}

type pageQuery struct {
	Limit  int
	Before time.Time
}

type skillPageQuery struct {
	Limit  int
	Before string
}

type pageMeta struct {
	Limit      int
	HasMore    bool
	NextBefore string
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

func parseRequiredBoolLikeQuery(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New(key + " must be a boolean")
	}
	return v, nil
}

func requireDeleteConfirmation(r *http.Request) error {
	return requireAdminConfirmation(r, "delete operations")
}

func requireAdminConfirmation(r *http.Request, operation string) error {
	raw := strings.TrimSpace(r.URL.Query().Get("confirm"))
	if raw == "" {
		return errors.New("confirm=true is required for " + operation)
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return errors.New("confirm must be a boolean")
	}
	if !v {
		return errors.New("confirm=true is required for " + operation)
	}
	return nil
}
func parseOptionalBoolQuery(r *http.Request, key string) (*bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, errors.New(key + " must be a boolean")
	}
	return &v, nil
}

func parseOptionalTimeQuery(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, errors.New(key + " must be an RFC3339 timestamp")
	}
	return &v, nil
}

func validateOptionalTimeRange(since, until *time.Time) error {
	if since != nil && until != nil && since.After(*until) {
		return errors.New("since must be before or equal to until")
	}
	return nil
}
func filterCredentialsByQuery(items []agentservice.Credential, r *http.Request) ([]agentservice.Credential, error) {
	statusRaw := strings.TrimSpace(r.URL.Query().Get("status"))
	var status agentservice.CredentialStatus
	if statusRaw != "" {
		status = agentservice.CredentialStatus(statusRaw)
		switch status {
		case agentservice.CredentialStatusActive, agentservice.CredentialStatusSuspended, agentservice.CredentialStatusRevoked:
		default:
			return nil, errors.New("status must be active, suspended, or revoked")
		}
	}
	expired, err := parseOptionalBoolQuery(r, "expired")
	if err != nil {
		return nil, err
	}
	expiring, err := parseOptionalBoolQuery(r, "expiring")
	if err != nil {
		return nil, err
	}
	if statusRaw == "" && expired == nil && expiring == nil {
		return items, nil
	}
	now := time.Now().UTC()
	expiringCutoff := now.Add(7 * 24 * time.Hour)
	filtered := make([]agentservice.Credential, 0, len(items))
	for _, item := range items {
		if statusRaw != "" && item.Status != status {
			continue
		}
		isExpired := item.ExpiresAt != nil && !item.ExpiresAt.After(now)
		isExpiring := item.ExpiresAt != nil && item.ExpiresAt.After(now) && !item.ExpiresAt.After(expiringCutoff)
		if expired != nil && isExpired != *expired {
			continue
		}
		if expiring != nil && isExpiring != *expiring {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func parseAsyncJobStatus(raw string, terminalOnly bool) (asyncJobStatus, bool) {
	statusRaw := strings.TrimSpace(raw)
	if statusRaw == "" {
		return "", true
	}
	status := asyncJobStatus(statusRaw)
	if terminalOnly {
		switch status {
		case asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled:
			return status, true
		default:
			return "", false
		}
	}
	switch status {
	case asyncJobStatusPending, asyncJobStatusRunning, asyncJobStatusSucceeded, asyncJobStatusFailed, asyncJobStatusCanceled:
		return status, true
	default:
		return "", false
	}
}

func parseRunStatus(raw string) (agentservice.RunStatus, bool) {
	statusRaw := strings.TrimSpace(raw)
	if statusRaw == "" {
		return "", true
	}
	status := agentservice.RunStatus(statusRaw)
	switch status {
	case agentservice.RunStatusRunning, agentservice.RunStatusSucceeded, agentservice.RunStatusFailed, agentservice.RunStatusCancelled:
		return status, true
	default:
		return "", false
	}
}

func parseMessageRole(raw string) (agentservice.MessageRole, bool) {
	roleRaw := strings.TrimSpace(raw)
	if roleRaw == "" {
		return "", true
	}
	role := agentservice.MessageRole(roleRaw)
	switch role {
	case agentservice.MessageRoleUser, agentservice.MessageRoleAssistant, agentservice.MessageRoleSystem:
		return role, true
	default:
		return "", false
	}
}

func parseRunResponseSource(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	switch value {
	case "ask_user", "":
		return value, true
	default:
		return "", false
	}
}

func parseTenantStatus(raw string) (agentservice.TenantStatus, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	status := agentservice.TenantStatus(value)
	switch status {
	case agentservice.TenantStatusActive, agentservice.TenantStatusDisabled:
		return status, true
	default:
		return "", false
	}
}

func parseUserStatus(raw string) (agentservice.UserStatus, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	status := agentservice.UserStatus(value)
	switch status {
	case agentservice.UserStatusActive, agentservice.UserStatusDisabled:
		return status, true
	default:
		return "", false
	}
}

func parsePageLimit(r *http.Request) (int, error) {
	limit := defaultPageLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, errors.New("limit must be a positive integer")
		}
		if parsed > maxPageLimit {
			parsed = maxPageLimit
		}
		limit = parsed
	}
	return limit, nil
}

func parsePageQuery(r *http.Request) (pageQuery, error) {
	limit, err := parsePageLimit(r)
	if err != nil {
		return pageQuery{}, err
	}
	page := pageQuery{Limit: limit}
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return pageQuery{}, errors.New("before must be an RFC3339 timestamp")
		}
		page.Before = before
	}
	return page, nil
}

func parseSkillPageQuery(r *http.Request) (skillPageQuery, error) {
	limit, err := parsePageLimit(r)
	if err != nil {
		return skillPageQuery{}, err
	}
	return skillPageQuery{Limit: limit, Before: strings.TrimSpace(r.URL.Query().Get("before"))}, nil
}

func listResponse(items any, meta pageMeta) map[string]any {
	out := map[string]any{
		"items":    items,
		"limit":    meta.Limit,
		"has_more": meta.HasMore,
	}
	if meta.NextBefore != "" {
		out["next_before"] = meta.NextBefore
	}
	return out
}

func paginateInstances(items []agentservice.Instance, page pageQuery) ([]agentservice.Instance, pageMeta) {
	filtered := make([]agentservice.Instance, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateSessions(items []agentservice.Session, page pageQuery) ([]agentservice.Session, pageMeta) {
	filtered := make([]agentservice.Session, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateMessages(items []agentservice.Message, page pageQuery) ([]agentservice.Message, pageMeta) {
	filtered := make([]agentservice.Message, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateRuns(items []agentservice.Run, page pageQuery) ([]agentservice.Run, pageMeta) {
	filtered := make([]agentservice.Run, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.StartedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].StartedAt
	})
}

func recordsPageMeta(items []agentservice.StructuredRecord, page pageQuery) ([]agentservice.StructuredRecord, pageMeta) {
	meta := pageMeta{Limit: page.Limit, HasMore: len(items) == page.Limit}
	if meta.HasMore && len(items) > 0 {
		meta.NextBefore = items[len(items)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return items, meta
}

func paginateTenants(items []agentservice.Tenant, page pageQuery) ([]agentservice.Tenant, pageMeta) {
	filtered := make([]agentservice.Tenant, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateUsers(items []agentservice.User, page pageQuery) ([]agentservice.User, pageMeta) {
	filtered := make([]agentservice.User, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateCredentials(items []agentservice.Credential, page pageQuery) ([]agentservice.Credential, pageMeta) {
	filtered := make([]agentservice.Credential, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateServiceSnapshots(items []agentservice.ServiceSnapshot, page pageQuery) ([]agentservice.ServiceSnapshot, pageMeta) {
	filtered := make([]agentservice.ServiceSnapshot, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}
func paginateAsyncJobs(items []asyncJobRecord, page pageQuery) ([]asyncJobRecord, pageMeta) {
	filtered := make([]asyncJobRecord, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func paginateMCPServers(items []agentservice.MCPServerView, page pageQuery) ([]agentservice.MCPServerView, pageMeta) {
	filtered := make([]agentservice.MCPServerView, 0, len(items))
	for _, item := range items {
		createdAt, ok := parseCursorTime(item.CreatedAt)
		if !ok {
			continue
		}
		if page.Before.IsZero() || createdAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		cursor, _ := parseCursorTime(window[0].CreatedAt)
		return cursor
	})
}

func paginateSkills(items []corelib.NLSkillEntry, page skillPageQuery) ([]corelib.NLSkillEntry, pageMeta) {
	filtered := make([]corelib.NLSkillEntry, 0, len(items))
	before := strings.ToLower(strings.TrimSpace(page.Before))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		if before == "" || name < before {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	meta := pageMeta{Limit: page.Limit, HasMore: start > 0}
	if meta.HasMore && len(window) > 0 {
		meta.NextBefore = window[0].Name
	}
	return window, meta
}

func paginateAuditEvents(items []agentservice.AuditEvent, page pageQuery) ([]agentservice.AuditEvent, pageMeta) {
	filtered := make([]agentservice.AuditEvent, 0, len(items))
	for _, item := range items {
		if page.Before.IsZero() || item.CreatedAt.Before(page.Before) {
			filtered = append(filtered, item)
		}
	}
	start := 0
	if len(filtered) > page.Limit {
		start = len(filtered) - page.Limit
	}
	window := filtered[start:]
	return window, buildPageMeta(len(filtered), start, page.Limit, func() time.Time {
		if len(window) == 0 {
			return time.Time{}
		}
		return window[0].CreatedAt
	})
}

func parseCursorTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func formatOptionalCursorTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func buildPageMeta(total, start, limit int, cursor func() time.Time) pageMeta {
	meta := pageMeta{Limit: limit, HasMore: start > 0}
	if meta.HasMore {
		meta.NextBefore = cursor().Format(time.RFC3339Nano)
	}
	return meta
}

func writeRateLimitError(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{
		"error":               "too many token attempts",
		"retry_after_seconds": seconds,
	})
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, agentservice.ErrUnauthorized):
		code = http.StatusUnauthorized
	case errors.Is(err, agentservice.ErrForbidden):
		code = http.StatusForbidden
	case errors.Is(err, agentservice.ErrTenantNotFound), errors.Is(err, agentservice.ErrUserNotFound), errors.Is(err, agentservice.ErrCredentialNotFound), errors.Is(err, agentservice.ErrUserConfigNotFound), errors.Is(err, agentservice.ErrInstanceNotFound), errors.Is(err, agentservice.ErrSessionNotFound), errors.Is(err, agentservice.ErrRunNotFound), errors.Is(err, agentservice.ErrSnapshotNotFound), errors.Is(err, agentservice.ErrRecordNotFound):
		code = http.StatusNotFound
	case errors.Is(err, agentservice.ErrRunNotRunning), errors.Is(err, agentservice.ErrInstanceBusy), errors.Is(err, agentservice.ErrUserBusy), errors.Is(err, agentservice.ErrTenantBusy), errors.Is(err, agentservice.ErrDeleteProtected), errors.Is(err, agentservice.ErrSessionBusy), errors.Is(err, agentservice.ErrSessionArchived), errors.Is(err, agentservice.ErrAlreadyExists):
		code = http.StatusConflict
	case errors.Is(err, agentservice.ErrQuotaExceeded):
		code = http.StatusTooManyRequests
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func requestClientIP(r *http.Request) string {
	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	if remoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.TrimSpace(host)
	}
	return remoteAddr
}
