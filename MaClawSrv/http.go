package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
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

type HTTPServer struct {
	svc         *agentservice.Service
	adminSecret string
	mux         *http.ServeMux
	authLimiter *authLimiter
}

func NewHTTPServer(svc *agentservice.Service, adminSecret string) *HTTPServer {
	s := &HTTPServer{svc: svc, adminSecret: adminSecret, mux: http.NewServeMux(), authLimiter: newAuthLimiter(20, time.Minute)}
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
	s.mux.HandleFunc("GET /api/v1/admin/tenants", s.withAdmin(s.handleListTenants))
	s.mux.HandleFunc("GET /api/v1/admin/audit-events", s.withAdmin(s.handleListAuditEvents))
	s.mux.HandleFunc("POST /api/v1/admin/tenants", s.withAdmin(s.handleCreateTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleGetTenant))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}", s.withAdmin(s.handleUpdateTenant))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleListUsers))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users", s.withAdmin(s.handleCreateUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleGetUser))
	s.mux.HandleFunc("PATCH /api/v1/admin/tenants/{tenantId}/users/{userId}", s.withAdmin(s.handleUpdateUser))
	s.mux.HandleFunc("GET /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", s.withAdmin(s.handleListCredentials))
	s.mux.HandleFunc("POST /api/v1/admin/tenants/{tenantId}/users/{userId}/credentials", s.withAdmin(s.handleCreateCredential))
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
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/capabilities", s.withPrincipal(s.handleGetInstanceCapabilities))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/stop", s.withPrincipal(s.handleStopInstance))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/resume", s.withPrincipal(s.handleResumeInstance))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/refresh-readiness", s.withPrincipal(s.handleRefreshInstanceReadiness))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/bootstrap", s.withPrincipal(s.handleGetInstanceBootstrap))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/messages", s.withPrincipal(s.handleSendMessage))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions", s.withPrincipal(s.handleListSessions))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions", s.withPrincipal(s.handleCreateSession))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions/{sessionId}", s.withPrincipal(s.handleGetSession))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/sessions/{sessionId}/messages", s.withPrincipal(s.handleListMessages))
	s.mux.HandleFunc("POST /api/v1/instances/{instanceId}/sessions/{sessionId}/messages", s.withPrincipal(s.handlePostMessage))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs", s.withPrincipal(s.handleListRuns))
	s.mux.HandleFunc("GET /api/v1/instances/{instanceId}/runs/{runId}", s.withPrincipal(s.handleGetRun))
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (s *HTTPServer) handleListTenants(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListTenants(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
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
func (s *HTTPServer) handleListUsers(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListUsers(r.Context(), r.PathValue("tenantId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
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
func (s *HTTPServer) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	out, err := s.svc.ListCredentials(r.Context(), r.PathValue("tenantId"), r.PathValue("userId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
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
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}
func (s *HTTPServer) handleCreateMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in agentservice.MCPServerCreateInput
	if !decodeJSON(w, r, &in) {
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
	out, err := s.svc.StartMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleStopMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.StopMCPServer(r.Context(), p, r.PathValue("serverId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *HTTPServer) handleCheckMCPServer(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
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
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
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
	out, err := s.svc.UploadSkill(r.Context(), p, r.PathValue("skillName"), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
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
			writeJSON(w, http.StatusBadGateway, map[string]any{"session": sess, "run": run, "message": msg, "error": err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "run": run, "message": msg})
}
func (s *HTTPServer) handleListSessions(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListSessions(r.Context(), p, r.PathValue("instanceId"))
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
func (s *HTTPServer) handleListMessages(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListMessages(r.Context(), p, r.PathValue("instanceId"), r.PathValue("sessionId"))
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
func (s *HTTPServer) handleListRuns(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListRuns(r.Context(), p, r.PathValue("instanceId"), agentservice.ListRunsInput{
		Status:    agentservice.RunStatus(strings.TrimSpace(r.URL.Query().Get("status"))),
		SessionID: strings.TrimSpace(r.URL.Query().Get("session_id")),
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

type pageMeta struct {
	Limit      int
	HasMore    bool
	NextBefore string
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 500
)

func parsePageQuery(r *http.Request) (pageQuery, error) {
	q := r.URL.Query()
	page := pageQuery{Limit: defaultPageLimit}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return pageQuery{}, errors.New("limit must be a positive integer")
		}
		if limit > maxPageLimit {
			limit = maxPageLimit
		}
		page.Limit = limit
	}

	if raw := strings.TrimSpace(q.Get("before")); raw != "" {
		before, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return pageQuery{}, errors.New("before must be an RFC3339 timestamp")
		}
		page.Before = before
	}
	return page, nil
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
