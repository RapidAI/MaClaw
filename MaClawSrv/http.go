package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
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

type HTTPServer struct {
	svc         *agentservice.Service
	adminSecret string
	mux         *http.ServeMux
	authLimiter *authLimiter
	jobs        *asyncJobManager
}

func NewHTTPServer(svc *agentservice.Service, adminSecret string) *HTTPServer {
	s := &HTTPServer{svc: svc, adminSecret: adminSecret, mux: http.NewServeMux(), authLimiter: newAuthLimiter(20, time.Minute), jobs: newAsyncJobManager(svc.DataRoot())}
	s.routes()
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
	s.mux.HandleFunc("GET /api/v1/admin/system/readiness", s.withAdmin(s.handleGetAdminReadiness))
	s.mux.HandleFunc("GET /api/v1/admin/overview", s.withAdmin(s.handleGetAdminOverview))
	s.mux.HandleFunc("GET /api/v1/admin/dashboard", s.withAdmin(s.handleGetAdminDashboard))
	s.mux.HandleFunc("GET /api/v1/admin/insights", s.withAdmin(s.handleGetAdminInsights))
	s.mux.HandleFunc("GET /api/v1/admin/alerts", s.withAdmin(s.handleGetAdminAlerts))
	s.mux.HandleFunc("GET /api/v1/admin/tenants", s.withAdmin(s.handleListTenants))
	s.mux.HandleFunc("GET /api/v1/admin/audit-events", s.withAdmin(s.handleListAuditEvents))
	s.mux.HandleFunc("GET /api/v1/admin/export", s.withAdmin(s.handleExportServiceState))
	s.mux.HandleFunc("POST /api/v1/admin/import", s.withAdmin(s.handleImportServiceState))
	s.mux.HandleFunc("POST /api/v1/admin/tenants", s.withAdmin(s.handleCreateTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleGetTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/summary", s.withAdmin(s.handleGetTenantSummary))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleUpdateTenant))
	s.mux.HandleFunc("DELETE /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleDeleteTenant))
	s.mux.HandleFunc("GET /api/v1/admin/users", s.withAdmin(s.handleListAllUsers))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleCreateUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleGetUser))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleUpdateUser))
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
func (s *HTTPServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	overview, err := s.svc.GetAdminOverview(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
		return
	}
	authFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
		return
	}
	rateLimited, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "auth.token_rate_limited"})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
		return
	}
	runSucceeded, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.succeeded"})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
		return
	}
	runFailed, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{Action: "run.failed"})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
		return
	}
	alerts, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{})
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("# MaClawSrv metrics unavailable\n# TYPE maclaw_metrics_up gauge\nmaclaw_metrics_up 0\n"))
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
	writeJSON(w, http.StatusOK, out)
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
	out, err := s.svc.GetAdminAlerts(r.Context(), agentservice.AdminAlertsInput{
		TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:   strings.TrimSpace(r.URL.Query().Get("user_id")),
		Kind:     strings.TrimSpace(r.URL.Query().Get("kind")),
		Since:    since,
		Limit:    limit,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
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
	out, err := s.svc.ListAuditEvents(r.Context(), agentservice.ListAuditEventsInput{
		TenantID:     strings.TrimSpace(r.URL.Query().Get("tenant_id")),
		UserID:       strings.TrimSpace(r.URL.Query().Get("user_id")),
		Action:       strings.TrimSpace(r.URL.Query().Get("action")),
		ResourceType: strings.TrimSpace(r.URL.Query().Get("resource_type")),
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
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleImportServiceState(w http.ResponseWriter, r *http.Request) {
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
	out, err := s.svc.ImportServiceState(r.Context(), *in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var in agentservice.CreateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.CreateTenant(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
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
func (s *HTTPServer) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	var in agentservice.UpdateTenantInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateTenant(r.Context(), r.PathValue("tenantId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteTenant(r.Context(), r.PathValue("tenantId")); err != nil {
		writeError(w, err)
		return
	}
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
func (s *HTTPServer) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var in agentservice.UpdateUserInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.DeleteUser(r.Context(), r.PathValue("tenantId"), r.PathValue("userId")); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
func (s *HTTPServer) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCredentials(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
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
	var in agentservice.UpdateCredentialInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRotateCredentialSecret(w http.ResponseWriter, r *http.Request) {
	var in agentservice.RotateCredentialSecretInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialSecret(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRotateCredentialKey(w http.ResponseWriter, r *http.Request) {
	var in agentservice.RotateCredentialKeyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.RotateCredentialAPIKey(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.RevokeCredential(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"), r.PathValue("credentialId"))
	if err != nil {
		writeError(w, err)
		return
	}
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
	var in corelib.AppConfig
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.svc.UpdateUserConfig(r.Context(), p, in)
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
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
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
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
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
		provided := r.Header.Get("X-MaClaw-Admin-Secret")
		if s.adminSecret == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(s.adminSecret)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin secret"})
			return
		}
		next(w, r)
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
	case errors.Is(err, agentservice.ErrTenantNotFound), errors.Is(err, agentservice.ErrUserNotFound), errors.Is(err, agentservice.ErrCredentialNotFound), errors.Is(err, agentservice.ErrUserConfigNotFound), errors.Is(err, agentservice.ErrInstanceNotFound), errors.Is(err, agentservice.ErrSessionNotFound), errors.Is(err, agentservice.ErrRunNotFound):
		code = http.StatusNotFound
	case errors.Is(err, agentservice.ErrRunNotRunning), errors.Is(err, agentservice.ErrInstanceBusy), errors.Is(err, agentservice.ErrUserBusy), errors.Is(err, agentservice.ErrTenantBusy), errors.Is(err, agentservice.ErrSessionBusy), errors.Is(err, agentservice.ErrSessionArchived), errors.Is(err, agentservice.ErrAlreadyExists):
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
