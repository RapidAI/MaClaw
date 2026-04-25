package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestOpenAPIDocumentIsAvailable(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("openapi status = %d body = %s", w.Code, w.Body.String())
	}
	var doc struct {
		OpenAPI    string         `json:"openapi"`
		Paths      map[string]any `json:"paths"`
		Components struct {
			SecuritySchemes map[string]any `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}
	if doc.OpenAPI == "" {
		t.Fatalf("missing openapi version: %#v", doc)
	}
	if _, ok := doc.Paths["/api/v1/instances"]; !ok {
		t.Fatalf("expected instance path in openapi doc")
	}
	if _, ok := doc.Components.SecuritySchemes["bearerAuth"]; !ok {
		t.Fatalf("expected bearerAuth security scheme")
	}
}

type blockingExecutor struct {
	started chan string
	release chan struct{}
}

func (e *blockingExecutor) Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	if e.started != nil {
		select {
		case e.started <- req.Message.ID:
		default:
		}
	}
	if e.release == nil {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
		return &agentservice.ExecuteResult{Content: "released", OutputType: "text/plain"}, nil
	}
}

func (e *blockingExecutor) DescribeCapabilities(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.AgentCapabilities, error) {
	_ = ctx
	_ = req
	return &agentservice.AgentCapabilities{Executor: "blocking", SupportsSessions: true}, nil
}

func TestGetAdminAlerts(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	readyInst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Ready Instance"})
	if err != nil {
		t.Fatalf("CreateInstance ready: %v", err)
	}
	unreadyInst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Unready Instance"})
	if err != nil {
		t.Fatalf("CreateInstance unready: %v", err)
	}
	unreadyInst.Status = agentservice.InstanceStatusStopped
	unreadyInst.Ready = false
	unreadyInst.ReadyReason = "config validation failed"
	unreadyInst.UpdatedAt = time.Now().UTC()
	if err := store.SaveInstance(*unreadyInst); err != nil {
		t.Fatalf("SaveInstance unready: %v", err)
	}
	waitingRun := agentservice.Run{
		ID:             "run_wait",
		TenantID:       tenant.ID,
		UserID:         user.ID,
		InstanceID:     readyInst.ID,
		SessionID:      "sess_wait",
		Status:         agentservice.RunStatusSucceeded,
		ResponseSource: "ask_user",
		WaitingForUser: true,
		StartedAt:      time.Now().UTC().Add(-time.Minute),
	}
	failedRun := agentservice.Run{
		ID:         "run_fail",
		TenantID:   tenant.ID,
		UserID:     user.ID,
		InstanceID: readyInst.ID,
		SessionID:  "sess_fail",
		Status:     agentservice.RunStatusFailed,
		Error:      "boom",
		StartedAt:  time.Now().UTC().Add(-2 * time.Minute),
	}
	if err := store.SaveRun(waitingRun); err != nil {
		t.Fatalf("SaveRun waiting: %v", err)
	}
	if err := store.SaveRun(failedRun); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/alerts?kind=failed_run&limit=1", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("alerts status = %d body = %s", w.Code, w.Body.String())
	}

	var alerts agentservice.AdminAlerts
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	if len(alerts.Items) != 1 {
		t.Fatalf("expected exactly one normalized alert item, got %#v", alerts.Items)
	}
	if alerts.Items[0].Kind != "failed_run" || alerts.Items[0].RunID != failedRun.ID {
		t.Fatalf("unexpected normalized alert item: %#v", alerts.Items[0])
	}
	if len(alerts.UnreadyInstances) != 0 {
		t.Fatalf("expected unready instances filtered out, got %#v", alerts.UnreadyInstances)
	}
	if len(alerts.WaitingRuns) != 0 {
		t.Fatalf("expected waiting runs filtered out, got %#v", alerts.WaitingRuns)
	}
	if len(alerts.FailedRuns) != 1 || alerts.FailedRuns[0].ID != failedRun.ID {
		t.Fatalf("expected failed run retained in legacy list, got %#v", alerts.FailedRuns)
	}
	if alerts.GeneratedAt.IsZero() {
		t.Fatalf("expected generated_at: %#v", alerts)
	}
}

func TestGetAdminDashboard(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Hour)
	sess := agentservice.Session{ID: "sess_dash", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_dash_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(-2 * time.Hour)}); err != nil {
		t.Fatalf("SaveMessage recent: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_dash_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "older", CreatedAt: now.Add(-48 * time.Hour)}); err != nil {
		t.Fatalf("SaveMessage older: %v", err)
	}
	completed := now.Add(-90 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_dash_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(-90 * time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun recent: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_dash_1", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "dashboard.opened", ResourceType: "system", ResourceID: "dashboard", CreatedAt: now.Add(-30 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/dashboard", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body = %s", w.Code, w.Body.String())
	}
	var dashboard agentservice.AdminDashboard
	if err := json.NewDecoder(w.Body).Decode(&dashboard); err != nil {
		t.Fatalf("decode dashboard: %v", err)
	}
	if dashboard.Overview.Tenants != 1 || dashboard.Overview.Users != 1 {
		t.Fatalf("unexpected overview: %#v", dashboard.Overview)
	}
	foundDashboardAudit := false
	for _, event := range dashboard.RecentAuditEvents {
		if event.Action == "dashboard.opened" {
			foundDashboardAudit = true
			break
		}
	}
	if len(dashboard.RecentAuditEvents) == 0 || !foundDashboardAudit {
		t.Fatalf("unexpected recent audits: %#v", dashboard.RecentAuditEvents)
	}
	if len(dashboard.Last24Hours) != 24 || len(dashboard.Last7Days) != 7 {
		t.Fatalf("unexpected trend lengths: %#v", dashboard)
	}
	recentHourHasMessage := false
	recentDayHasMessage := false
	for _, point := range dashboard.Last24Hours {
		if point.Messages > 0 || point.Runs > 0 || point.AuditEvents > 0 {
			recentHourHasMessage = true
			break
		}
	}
	for _, point := range dashboard.Last7Days {
		if point.Messages > 0 || point.Runs > 0 || point.AuditEvents > 0 {
			recentDayHasMessage = true
			break
		}
	}
	if !recentHourHasMessage || !recentDayHasMessage || dashboard.GeneratedAt.IsZero() {
		t.Fatalf("unexpected dashboard trend payload: %#v", dashboard)
	}
}

func TestGetAdminOverview(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess := agentservice.Session{ID: "sess_overview", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_overview_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	completed := now.Add(2 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_overview_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := store.SaveAuditEvent(agentservice.AuditEvent{ID: "audit_custom", TenantID: tenant.ID, UserID: user.ID, ActorType: "admin", Action: "overview.checked", ResourceType: "system", ResourceID: "overview", CreatedAt: now.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("SaveAuditEvent: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/overview", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("overview status = %d body = %s", w.Code, w.Body.String())
	}
	var overview agentservice.AdminOverview
	if err := json.NewDecoder(w.Body).Decode(&overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview.Tenants != 1 || overview.ActiveTenants != 1 || overview.Users != 1 || overview.ActiveUsers != 1 {
		t.Fatalf("unexpected tenant/user counts: %#v", overview)
	}
	if overview.Instances != 1 || overview.ReadyInstances != 1 || overview.Sessions != 1 || overview.Messages != 1 || overview.Runs != 1 {
		t.Fatalf("unexpected activity counts: %#v", overview)
	}
	if overview.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || overview.AuditEvents == 0 {
		t.Fatalf("unexpected run/audit counts: %#v", overview)
	}
	if overview.LastActivityAt == nil || overview.LastAuditAt == nil {
		t.Fatalf("expected last activity and last audit timestamps: %#v", overview)
	}
}

func TestAdminCanListTenantsAndUsers(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants status = %d body = %s", w.Code, w.Body.String())
	}
	var tenants struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tenants); err != nil {
		t.Fatalf("decode tenants: %v", err)
	}
	if len(tenants.Items) != 1 || tenants.Items[0].ID != tenant.ID {
		t.Fatalf("tenants = %#v", tenants.Items)
	}
	if tenants.Limit != defaultPageLimit || tenants.HasMore || tenants.NextBefore != "" {
		t.Fatalf("unexpected tenant page meta: %#v", tenants)
	}

	req = httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users status = %d body = %s", w.Code, w.Body.String())
	}
	var users struct {
		Items      []agentservice.User `json:"items"`
		Limit      int                 `json:"limit"`
		HasMore    bool                `json:"has_more"`
		NextBefore string              `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users.Items) != 1 || users.Items[0].ID != user.ID {
		t.Fatalf("users = %#v", users.Items)
	}
	if users.Limit != defaultPageLimit || users.HasMore || users.NextBefore != "" {
		t.Fatalf("unexpected user page meta: %#v", users)
	}
}

func TestAdminListTenantsSupportsFilters(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	alpha, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Alpha Team"})
	if err != nil {
		t.Fatalf("CreateTenant alpha: %v", err)
	}
	beta, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Beta Team"})
	if err != nil {
		t.Fatalf("CreateTenant beta: %v", err)
	}
	disabled := agentservice.TenantStatusDisabled
	if _, err := svc.UpdateTenant(context.Background(), beta.ID, agentservice.UpdateTenantInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateTenant beta: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?status=disabled", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants by status = %d body = %s", w.Code, w.Body.String())
	}
	var byStatus struct {
		Items []agentservice.Tenant `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byStatus); err != nil {
		t.Fatalf("decode byStatus: %v", err)
	}
	if len(byStatus.Items) != 1 || byStatus.Items[0].ID != beta.ID {
		t.Fatalf("unexpected tenants by status: %#v", byStatus.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?name=alpha", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list tenants by name = %d body = %s", w.Code, w.Body.String())
	}
	var byName struct {
		Items []agentservice.Tenant `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byName); err != nil {
		t.Fatalf("decode byName: %v", err)
	}
	if len(byName.Items) != 1 || byName.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected tenants by name: %#v", byName.Items)
	}
}

func TestAdminListUsersSupportsFilters(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	alpha, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alpha User", Email: "alpha@example.com"})
	if err != nil {
		t.Fatalf("CreateUser alpha: %v", err)
	}
	beta, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Beta User", Email: "beta@example.com"})
	if err != nil {
		t.Fatalf("CreateUser beta: %v", err)
	}
	disabled := agentservice.UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, beta.ID, agentservice.UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser beta: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?status=disabled", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by status = %d body = %s", w.Code, w.Body.String())
	}
	var byStatus struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byStatus); err != nil {
		t.Fatalf("decode byStatus: %v", err)
	}
	if len(byStatus.Items) != 1 || byStatus.Items[0].ID != beta.ID {
		t.Fatalf("unexpected users by status: %#v", byStatus.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?name=alpha", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by name = %d body = %s", w.Code, w.Body.String())
	}
	var byName struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byName); err != nil {
		t.Fatalf("decode byName: %v", err)
	}
	if len(byName.Items) != 1 || byName.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected users by name: %#v", byName.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/users?email=alpha@example.com", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list users by email = %d body = %s", w.Code, w.Body.String())
	}
	var byEmail struct {
		Items []agentservice.User `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&byEmail); err != nil {
		t.Fatalf("decode byEmail: %v", err)
	}
	if len(byEmail.Items) != 1 || byEmail.Items[0].ID != alpha.ID {
		t.Fatalf("unexpected users by email: %#v", byEmail.Items)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "# TYPE maclaw_metrics_up gauge") ||
		!strings.Contains(body, "maclaw_metrics_up 1") ||
		!strings.Contains(body, "maclaw_tenants_total ") ||
		!strings.Contains(body, "maclaw_users_total ") ||
		!strings.Contains(body, "maclaw_audit_events_total ") {
		t.Fatalf("unexpected metrics body: %s", body)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("unexpected metrics content type: %s", got)
	}
}
func TestAdminCanListAndRevokeCredentials(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cred, err := svc.CreateCredential(context.Background(), agentservice.CreateCredentialInput{TenantID: tenant.ID, UserID: user.ID, Name: "API", APIKey: "key", APISecret: "secret"})
	if err != nil {
		t.Fatalf("CreateCredential: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list credentials status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items      []agentservice.Credential `json:"items"`
		Limit      int                       `json:"limit"`
		HasMore    bool                      `json:"has_more"`
		NextBefore string                    `json:"next_before"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != cred.ID || listed.Items[0].Status != agentservice.CredentialStatusActive {
		t.Fatalf("credentials = %#v", listed.Items)
	}
	if listed.Limit != defaultPageLimit || listed.HasMore || listed.NextBefore != "" {
		t.Fatalf("unexpected credential page meta: %#v", listed)
	}

	req = httptest.NewRequest("DELETE", "/api/v1/admin/tenants/"+tenant.ID+"/users/"+user.ID+"/credentials/"+cred.ID, bytes.NewReader(nil))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke credential status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.IssueToken(context.Background(), agentservice.IssueTokenInput{APIKey: "key", APISecret: "secret"}); err == nil {
		t.Fatalf("expected revoked credential to reject token issuance")
	}
}

func TestAdminPaginationForTenantsUsersAndCredentials(t *testing.T) {
	ctx := context.Background()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	createdTenants := make([]agentservice.Tenant, 0, 3)
	for i := 1; i <= 3; i++ {
		tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: fmt.Sprintf("Tenant %d", i)})
		if err != nil {
			t.Fatalf("CreateTenant %d: %v", i, err)
		}
		createdTenants = append(createdTenants, *tenant)
		time.Sleep(2 * time.Millisecond)
	}

	targetTenant := createdTenants[2]
	createdUsers := make([]agentservice.User, 0, 3)
	for i := 1; i <= 3; i++ {
		user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: targetTenant.ID, Name: fmt.Sprintf("User %d", i)})
		if err != nil {
			t.Fatalf("CreateUser %d: %v", i, err)
		}
		createdUsers = append(createdUsers, *user)
		time.Sleep(2 * time.Millisecond)
	}

	targetUser := createdUsers[2]
	createdCredentials := make([]agentservice.Credential, 0, 3)
	for i := 1; i <= 3; i++ {
		cred, err := svc.CreateCredential(ctx, agentservice.CreateCredentialInput{
			TenantID:  targetTenant.ID,
			UserID:    targetUser.ID,
			Name:      fmt.Sprintf("Cred %d", i),
			APIKey:    fmt.Sprintf("key-%d", i),
			APISecret: fmt.Sprintf("secret-%d", i),
		})
		if err != nil {
			t.Fatalf("CreateCredential %d: %v", i, err)
		}
		createdCredentials = append(createdCredentials, *cred)
		time.Sleep(2 * time.Millisecond)
	}

	server := NewHTTPServer(svc, "admin-secret")

	var tenantsPage struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tenantsPage); err != nil {
		t.Fatalf("decode tenant page: %v", err)
	}
	if tenantsPage.Limit != 2 || !tenantsPage.HasMore || tenantsPage.NextBefore == "" {
		t.Fatalf("unexpected tenant page meta: %#v", tenantsPage)
	}
	if len(tenantsPage.Items) != 2 || tenantsPage.Items[0].ID != createdTenants[1].ID || tenantsPage.Items[1].ID != createdTenants[2].ID {
		t.Fatalf("unexpected tenant page items: %#v", tenantsPage.Items)
	}

	var tenantTail struct {
		Items      []agentservice.Tenant `json:"items"`
		Limit      int                   `json:"limit"`
		HasMore    bool                  `json:"has_more"`
		NextBefore string                `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants?limit=2&before="+url.QueryEscape(tenantsPage.NextBefore), nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tenantTail); err != nil {
		t.Fatalf("decode tenant tail: %v", err)
	}
	if tenantTail.HasMore || tenantTail.NextBefore != "" || len(tenantTail.Items) != 1 || tenantTail.Items[0].ID != createdTenants[0].ID {
		t.Fatalf("unexpected tenant tail: %#v", tenantTail)
	}

	var usersPage struct {
		Items      []agentservice.User `json:"items"`
		Limit      int                 `json:"limit"`
		HasMore    bool                `json:"has_more"`
		NextBefore string              `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+targetTenant.ID+"/users?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("user page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&usersPage); err != nil {
		t.Fatalf("decode user page: %v", err)
	}
	if usersPage.Limit != 2 || !usersPage.HasMore || usersPage.NextBefore == "" {
		t.Fatalf("unexpected user page meta: %#v", usersPage)
	}
	if len(usersPage.Items) != 2 || usersPage.Items[0].ID != createdUsers[1].ID || usersPage.Items[1].ID != createdUsers[2].ID {
		t.Fatalf("unexpected user page items: %#v", usersPage.Items)
	}

	var credentialsPage struct {
		Items      []agentservice.Credential `json:"items"`
		Limit      int                       `json:"limit"`
		HasMore    bool                      `json:"has_more"`
		NextBefore string                    `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+targetTenant.ID+"/users/"+targetUser.ID+"/credentials?limit=2", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("credential page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&credentialsPage); err != nil {
		t.Fatalf("decode credential page: %v", err)
	}
	if credentialsPage.Limit != 2 || !credentialsPage.HasMore || credentialsPage.NextBefore == "" {
		t.Fatalf("unexpected credential page meta: %#v", credentialsPage)
	}
	if len(credentialsPage.Items) != 2 || credentialsPage.Items[0].ID != createdCredentials[1].ID || credentialsPage.Items[1].ID != createdCredentials[2].ID {
		t.Fatalf("unexpected credential page items: %#v", credentialsPage.Items)
	}
}

func TestAdminCanListAuditEvents(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/admin/audit-events?tenant_id="+tenant.ID+"&action=user.created", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list audit events status = %d body = %s", w.Code, w.Body.String())
	}
	var events struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&events); err != nil {
		t.Fatalf("decode audit events: %v", err)
	}
	if len(events.Items) != 1 || events.Items[0].Action != "user.created" || events.Items[0].UserID != user.ID {
		t.Fatalf("audit events = %#v", events.Items)
	}
	if events.Items[0].ActorType != "admin" || events.Items[0].ResourceType != "user" {
		t.Fatalf("unexpected audit event = %#v", events.Items[0])
	}
}

func TestListRunsFiltersByStatus(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	_, err = svc.UpdateUserConfig(context.Background(), principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_1", Status: agentservice.RunStatusFailed, StartedAt: now.Add(1 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_2", Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun succeeded: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs?status=failed", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", w.Code, w.Body.String())
	}
	var runs struct {
		Items []agentservice.Run `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs.Items) != 1 || runs.Items[0].ID != "run_1" || runs.Items[0].Status != agentservice.RunStatusFailed {
		t.Fatalf("runs = %#v", runs.Items)
	}
}

func TestListRunsFiltersByResponseSourceAndWaitingForUser(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	_, err = svc.UpdateUserConfig(context.Background(), principal, testLLMConfig())
	if err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_wait", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_1", Status: agentservice.RunStatusSucceeded, ResponseSource: "ask_user", WaitingForUser: true, StartedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveRun wait: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_done", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: "sess_2", Status: agentservice.RunStatusSucceeded, ResponseSource: "assistant", WaitingForUser: false, StartedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun done: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/runs?response_source=ask_user&waiting_for_user=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list runs status = %d body = %s", w.Code, w.Body.String())
	}
	var runs struct {
		Items []agentservice.Run `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&runs); err != nil {
		t.Fatalf("decode runs: %v", err)
	}
	if len(runs.Items) != 1 || runs.Items[0].ID != "run_wait" {
		t.Fatalf("runs = %#v", runs.Items)
	}
}

func TestListMessagesFiltersByRoleAndSince(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	base := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	if err := store.SaveMessage(agentservice.Message{ID: "msg_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: base.Add(1 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_1: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: base.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_2: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_3", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "followup", CreatedAt: base.Add(3 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage msg_3: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages?role=assistant&since=2026-04-24T10:02:00Z", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body = %s", w.Code, w.Body.String())
	}
	var messages struct {
		Items []agentservice.Message `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(messages.Items) != 2 || messages.Items[0].ID != "msg_2" || messages.Items[1].ID != "msg_3" {
		t.Fatalf("messages = %#v", messages.Items)
	}
}

func TestGetInstanceSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess1 := agentservice.Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", Metadata: map[string]string{"pending_ask_user": "true"}, CreatedAt: now, UpdatedAt: now}
	sess2ArchivedAt := now.Add(2 * time.Minute)
	sess2 := agentservice.Session{ID: "sess_2", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", Archived: true, ArchivedAt: &sess2ArchivedAt, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(3 * time.Minute)}
	if err := store.SaveSession(sess1); err != nil {
		t.Fatalf("SaveSession sess1: %v", err)
	}
	if err := store.SaveSession(sess2); err != nil {
		t.Fatalf("SaveSession sess2: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(4 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_assistant", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}
	completed := now.Add(7 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_wait", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess1.ID, Status: agentservice.RunStatusSucceeded, WaitingForUser: true, StartedAt: now.Add(6 * time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun wait: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_failed", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess2.ID, Status: agentservice.RunStatusFailed, StartedAt: now.Add(8 * time.Minute)}); err != nil {
		t.Fatalf("SaveRun failed: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.InstanceSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.InstanceID != inst.ID || summary.Sessions != 2 || summary.ArchivedSessions != 1 || summary.WaitingSessions != 1 {
		t.Fatalf("unexpected session summary: %#v", summary)
	}
	if summary.Messages != 2 || summary.UserMessages != 1 || summary.AssistantMessages != 1 {
		t.Fatalf("unexpected message summary: %#v", summary)
	}
	if summary.Runs != 2 || summary.WaitingRuns != 1 || summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.RunsByStatus[agentservice.RunStatusFailed] != 1 {
		t.Fatalf("unexpected run summary: %#v", summary)
	}
	if summary.LastActivityAt == nil {
		t.Fatalf("expected last activity")
	}
}

func TestGetTenantSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user1, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User One", Email: "one@example.com"})
	if err != nil {
		t.Fatalf("CreateUser user1: %v", err)
	}
	user2, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User Two", Email: "two@example.com"})
	if err != nil {
		t.Fatalf("CreateUser user2: %v", err)
	}
	tenantInstanceLimit := 5
	tenantMessageLimit := 10
	if _, err := svc.UpdateTenant(context.Background(), tenant.ID, agentservice.UpdateTenantInput{MaxInstances: &tenantInstanceLimit, MaxMessages: &tenantMessageLimit}); err != nil {
		t.Fatalf("UpdateTenant quota: %v", err)
	}
	user1SessionLimit := 3
	user1MessageLimit := 4
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user1.ID, agentservice.UpdateUserInput{MaxSessions: &user1SessionLimit, MaxMessages: &user1MessageLimit}); err != nil {
		t.Fatalf("UpdateUser user1 quota: %v", err)
	}
	disabled := agentservice.UserStatusDisabled
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user2.ID, agentservice.UpdateUserInput{Status: &disabled}); err != nil {
		t.Fatalf("UpdateUser user2: %v", err)
	}
	principal1 := agentservice.Principal{TenantID: tenant.ID, UserID: user1.ID}
	principal2 := agentservice.Principal{TenantID: tenant.ID, UserID: user2.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal1, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig user1: %v", err)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), principal2, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig user2: %v", err)
	}
	inst1, err := svc.CreateInstance(context.Background(), principal1, agentservice.CreateInstanceInput{Name: "Instance One"})
	if err != nil {
		t.Fatalf("CreateInstance user1: %v", err)
	}
	inst2, err := svc.CreateInstance(context.Background(), principal2, agentservice.CreateInstanceInput{Name: "Instance Two"})
	if err != nil {
		t.Fatalf("CreateInstance user2: %v", err)
	}
	if _, err := svc.StopInstance(context.Background(), principal2, inst2.ID); err != nil {
		t.Fatalf("StopInstance user2: %v", err)
	}
	base := time.Date(2026, 4, 25, 9, 0, 0, 0, time.UTC)
	for _, sess := range []agentservice.Session{
		{ID: "sess_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, AgentID: "default", CreatedAt: base, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "sess_2", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, AgentID: "default", CreatedAt: base.Add(3 * time.Minute), UpdatedAt: base.Add(4 * time.Minute)},
	} {
		if err := store.SaveSession(sess); err != nil {
			t.Fatalf("SaveSession %s: %v", sess.ID, err)
		}
	}
	for _, msg := range []agentservice.Message{
		{ID: "msg_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: base.Add(5 * time.Minute)},
		{ID: "msg_2", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: base.Add(6 * time.Minute)},
		{ID: "msg_3", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, SessionID: "sess_2", Role: agentservice.MessageRoleUser, Content: "ping", CreatedAt: base.Add(7 * time.Minute)},
	} {
		if err := store.SaveMessage(msg); err != nil {
			t.Fatalf("SaveMessage %s: %v", msg.ID, err)
		}
	}
	completed := base.Add(9 * time.Minute)
	for _, run := range []agentservice.Run{
		{ID: "run_1", TenantID: tenant.ID, UserID: user1.ID, InstanceID: inst1.ID, SessionID: "sess_1", Status: agentservice.RunStatusSucceeded, StartedAt: base.Add(6 * time.Minute), CompletedAt: &completed},
		{ID: "run_2", TenantID: tenant.ID, UserID: user2.ID, InstanceID: inst2.ID, SessionID: "sess_2", Status: agentservice.RunStatusFailed, StartedAt: base.Add(8 * time.Minute)},
	} {
		if err := store.SaveRun(run); err != nil {
			t.Fatalf("SaveRun %s: %v", run.ID, err)
		}
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tenants/"+tenant.ID+"/summary", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tenant summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.TenantSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode tenant summary: %v", err)
	}
	if summary.TenantID != tenant.ID || summary.Users != 2 || summary.ActiveUsers != 1 || summary.DisabledUsers != 1 {
		t.Fatalf("unexpected tenant user summary: %#v", summary)
	}
	if summary.Instances != 2 || summary.ReadyInstances != 1 || summary.StoppedInstances != 1 {
		t.Fatalf("unexpected instance totals: %#v", summary)
	}
	if summary.Sessions != 2 || summary.Messages != 3 || summary.UserMessages != 2 || summary.AssistantMessages != 1 || summary.Runs != 2 {
		t.Fatalf("unexpected activity totals: %#v", summary)
	}
	if summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.RunsByStatus[agentservice.RunStatusFailed] != 1 {
		t.Fatalf("unexpected run statuses: %#v", summary)
	}
	if len(summary.UserSummaries) != 2 || summary.LastActivityAt == nil {
		t.Fatalf("unexpected user breakdown: %#v", summary)
	}
	if summary.Quota.MaxInstances != 5 || summary.QuotaUsage.Instances.Limit != 5 || summary.QuotaUsage.Instances.Used != 2 {
		t.Fatalf("unexpected tenant quota snapshot: %#v", summary.QuotaUsage)
	}
	if summary.QuotaUsage.Instances.Remaining == nil || *summary.QuotaUsage.Instances.Remaining != 3 {
		t.Fatalf("unexpected tenant remaining instances: %#v", summary.QuotaUsage.Instances)
	}
	var user1Summary *agentservice.TenantUserSummary
	for i := range summary.UserSummaries {
		if summary.UserSummaries[i].UserID == user1.ID {
			user1Summary = &summary.UserSummaries[i]
			break
		}
	}
	if user1Summary == nil {
		t.Fatalf("missing user1 summary: %#v", summary.UserSummaries)
	}
	if user1Summary.EffectiveQuota.MaxSessions != 3 || user1Summary.QuotaUsage.Sessions.Limit != 3 || user1Summary.QuotaUsage.Sessions.Used != 1 {
		t.Fatalf("unexpected user1 quota usage: %#v", user1Summary)
	}
	if user1Summary.QuotaUsage.Sessions.Remaining == nil || *user1Summary.QuotaUsage.Sessions.Remaining != 2 {
		t.Fatalf("unexpected user1 remaining sessions: %#v", user1Summary.QuotaUsage.Sessions)
	}
	if user1Summary.EffectiveQuota.MaxMessages != 4 || user1Summary.QuotaUsage.Messages.Remaining == nil || *user1Summary.QuotaUsage.Messages.Remaining != 2 {
		t.Fatalf("unexpected user1 message quota usage: %#v", user1Summary)
	}
}

func TestCreateInstanceReturnsTooManyRequestsWhenQuotaExceeded(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	one := 1
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user.ID, agentservice.UpdateUserInput{MaxInstances: &one}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	body := `{"name":"first"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create instance status = %d body = %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances", bytes.NewBufferString(`{"name":"second"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second create instance status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestGetUsageSummary(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	instanceLimit := 3
	sessionLimit := 4
	messageLimit := 5
	runLimit := 6
	if _, err := svc.UpdateUser(context.Background(), tenant.ID, user.ID, agentservice.UpdateUserInput{MaxInstances: &instanceLimit, MaxSessions: &sessionLimit, MaxMessages: &messageLimit, MaxRuns: &runLimit}); err != nil {
		t.Fatalf("UpdateUser quota: %v", err)
	}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	now := time.Now().UTC()
	sess := agentservice.Session{ID: "sess_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, AgentID: "default", CreatedAt: now, UpdatedAt: now}
	if err := store.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_user", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now.Add(time.Minute)}); err != nil {
		t.Fatalf("SaveMessage user: %v", err)
	}
	if err := store.SaveMessage(agentservice.Message{ID: "msg_assistant", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleAssistant, Content: "hi", CreatedAt: now.Add(2 * time.Minute)}); err != nil {
		t.Fatalf("SaveMessage assistant: %v", err)
	}
	completed := now.Add(3 * time.Minute)
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now.Add(time.Minute), CompletedAt: &completed}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/usage/summary", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("usage summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary agentservice.UsageSummary
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.Instances != 1 || summary.Sessions != 1 || summary.Messages != 2 || summary.UserMessages != 1 || summary.AssistantMessages != 1 || summary.Runs != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.RunsByStatus[agentservice.RunStatusSucceeded] != 1 || summary.LastActivityAt == nil {
		t.Fatalf("summary run status/last activity = %#v", summary)
	}
	if summary.Quota.MaxInstances != 3 || summary.Quota.MaxMessages != 5 || summary.QuotaUsage.Messages.Limit != 5 || summary.QuotaUsage.Messages.Used != 2 {
		t.Fatalf("unexpected usage quota snapshot: %#v", summary)
	}
	if summary.QuotaUsage.Messages.Remaining == nil || *summary.QuotaUsage.Messages.Remaining != 3 {
		t.Fatalf("unexpected usage message remaining: %#v", summary.QuotaUsage.Messages)
	}
	if summary.QuotaUsage.Runs.Remaining == nil || *summary.QuotaUsage.Runs.Remaining != 5 {
		t.Fatalf("unexpected usage run remaining: %#v", summary.QuotaUsage.Runs)
	}
}

func TestPaginateMessagesReturnsNewestWindowChronologically(t *testing.T) {
	base := time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC)
	items := []agentservice.Message{
		{ID: "msg_1", CreatedAt: base.Add(1 * time.Minute)},
		{ID: "msg_2", CreatedAt: base.Add(2 * time.Minute)},
		{ID: "msg_3", CreatedAt: base.Add(3 * time.Minute)},
	}

	page, err := parsePageQuery(httptest.NewRequest("GET", "/messages?limit=2", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	got, meta := paginateMessages(items, page)

	if len(got) != 2 || got[0].ID != "msg_2" || got[1].ID != "msg_3" {
		t.Fatalf("unexpected page: %#v", got)
	}
	if !meta.HasMore || meta.NextBefore != got[0].CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected meta: %#v", meta)
	}
}

func TestParsePageQueryCapsLimitAndValidatesBefore(t *testing.T) {
	page, err := parsePageQuery(httptest.NewRequest("GET", "/instances?limit=999", nil))
	if err != nil {
		t.Fatalf("parsePageQuery: %v", err)
	}
	if page.Limit != maxPageLimit {
		t.Fatalf("limit = %d, want %d", page.Limit, maxPageLimit)
	}

	if _, err := parsePageQuery(httptest.NewRequest("GET", "/instances?before=not-time", nil)); err == nil {
		t.Fatalf("expected invalid before error")
	}
}

func testLLMConfig() corelib.AppConfig {
	return corelib.AppConfig{
		MaclawLLMUrl:   "https://llm.example/v1",
		MaclawLLMKey:   "test-key",
		MaclawLLMModel: "test-model",
	}
}

func TestRequestClientIPStripsSourcePort(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/auth/token", nil)
	req.RemoteAddr = "203.0.113.10:54321"
	if got := requestClientIP(req); got != "203.0.113.10" {
		t.Fatalf("requestClientIP = %q", got)
	}
}

func TestTokenRateLimitUsesClientIPNotSourcePort(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	server.authLimiter = newAuthLimiter(1, time.Minute)

	body := []byte(`{"api_key":"missing","api_secret":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("first token attempt status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader(body))
	req.RemoteAddr = "198.51.100.20:10099"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second token attempt status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestFailedTokenAttemptCreatesAuditEvent(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"missing-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "203.0.113.9:40123"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("token failure status = %d body = %s", w.Code, w.Body.String())
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "auth.token_failed"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 auth.token_failed event, got %#v", events)
	}
	if events[0].Metadata["remote_ip"] != "203.0.113.9" {
		t.Fatalf("unexpected remote_ip metadata = %#v", events[0].Metadata)
	}
	if events[0].Metadata["api_key_prefix"] != "missin" {
		t.Fatalf("unexpected api_key_prefix metadata = %#v", events[0].Metadata)
	}
}

func TestTokenFailureThresholdTriggersTemporaryLock(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	server.authLimiter = newAuthLimiter(100, time.Minute)

	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
		req.RemoteAddr = "198.51.100.50:50000"
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d body = %s", i+1, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest("POST", "/api/v1/auth/token", bytes.NewReader([]byte(`{"api_key":"lock-key","api_secret":"wrong"}`)))
	req.RemoteAddr = "198.51.100.50:50001"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("threshold attempt status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Fatalf("expected Retry-After header")
	}
}

func TestGetInstanceCapabilities(t *testing.T) {
	executor := &agentservice.CoreAgentExecutor{}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret")
	req := httptest.NewRequest("GET", "/api/v1/instances/"+inst.ID+"/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get capabilities status = %d body = %s", w.Code, w.Body.String())
	}
	var caps agentservice.AgentCapabilities
	if err := json.NewDecoder(w.Body).Decode(&caps); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if caps.Executor != "core_agent" || caps.SupportsSSH || !caps.SupportsAskUser {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	if len(caps.Tools) == 0 {
		t.Fatalf("expected tools in capabilities")
	}
}

func TestListSkillsSupportsNameCursorPagination(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	skillsRoot := filepath.Join(dataRoot, "tenants", tenant.ID, "users", user.ID, "skills")
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		dir := filepath.Join(skillsRoot, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "KNOWLEDGE.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	server := NewHTTPServer(svc, "admin-secret")
	var page struct {
		Items      []corelib.NLSkillEntry `json:"items"`
		Limit      int                    `json:"limit"`
		HasMore    bool                   `json:"has_more"`
		NextBefore string                 `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/skills?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list skills page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode skills page: %v", err)
	}
	if page.Limit != 2 || !page.HasMore || page.NextBefore != "bravo" {
		t.Fatalf("unexpected skills page meta: %#v", page)
	}
	if len(page.Items) != 2 || page.Items[0].Name != "bravo" || page.Items[1].Name != "charlie" {
		t.Fatalf("unexpected skills page items: %#v", page.Items)
	}

	var tail struct {
		Items      []corelib.NLSkillEntry `json:"items"`
		Limit      int                    `json:"limit"`
		HasMore    bool                   `json:"has_more"`
		NextBefore string                 `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/skills?limit=2&before="+url.QueryEscape(page.NextBefore), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list skills tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tail); err != nil {
		t.Fatalf("decode skills tail: %v", err)
	}
	if tail.HasMore || tail.NextBefore != "" || len(tail.Items) != 1 || tail.Items[0].Name != "alpha" {
		t.Fatalf("unexpected skills tail: %#v", tail)
	}
}

func TestMCPRemoteServerCRUDAndTools(t *testing.T) {
	tenantID, userID, token, server := newMCPAuthenticatedServer(t)
	_ = tenantID
	_ = userID

	remoteMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode remote MCP request: %v", err)
		}
		if req["method"] == "initialize" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "session-1")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26","capabilities":{}}}`))
			return
		}
		if req["method"] != "tools/list" {
			t.Fatalf("unexpected remote MCP method: %#v", req["method"])
		}
		if got := r.Header.Get("Authorization"); got != "Bearer remote-secret" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Mcp-Session-Id"); got != "session-1" {
			t.Fatalf("session id = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search_docs","description":"Search docs","inputSchema":{"type":"object"}}]}}`))
	}))
	defer remoteMCP.Close()

	body := fmt.Sprintf(`{"kind":"remote","name":"Docs MCP","endpoint_url":%q,"auth_type":"bearer","auth_secret":"remote-secret"}`, remoteMCP.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created MCP server: %v", err)
	}
	if created.Kind != "remote" || created.Name != "Docs MCP" || created.EndpointURL != remoteMCP.URL {
		t.Fatalf("unexpected created remote MCP server: %#v", created)
	}
	if created.HasAuthSecret != true {
		t.Fatalf("expected auth secret marker on created remote MCP server: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/health-check", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP health status = %d body = %s", w.Code, w.Body.String())
	}
	var checked agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&checked); err != nil {
		t.Fatalf("decode checked MCP server: %v", err)
	}
	if checked.HealthStatus != "healthy" || !checked.Running || len(checked.Tools) != 1 {
		t.Fatalf("unexpected checked MCP server: %#v", checked)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("remote MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode remote MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "search_docs" {
		t.Fatalf("unexpected remote MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/mcp/servers/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete remote MCP status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestListMCPServersSupportsPagination(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	created := make([]agentservice.MCPServerView, 0, 3)
	for i := 1; i <= 3; i++ {
		body := fmt.Sprintf(`{"kind":"local","name":%q,"command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, fmt.Sprintf("Local %d", i), os.Args[0])
		req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create mcp %d status = %d body = %s", i, w.Code, w.Body.String())
		}
		var item agentservice.MCPServerView
		if err := json.NewDecoder(w.Body).Decode(&item); err != nil {
			t.Fatalf("decode mcp %d: %v", i, err)
		}
		created = append(created, item)
		time.Sleep(1100 * time.Millisecond)
	}

	var page struct {
		Items      []agentservice.MCPServerView `json:"items"`
		Limit      int                          `json:"limit"`
		HasMore    bool                         `json:"has_more"`
		NextBefore string                       `json:"next_before"`
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mcp page status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&page); err != nil {
		t.Fatalf("decode mcp page: %v", err)
	}
	if page.Limit != 2 || !page.HasMore || page.NextBefore == "" {
		t.Fatalf("unexpected mcp page meta: %#v", page)
	}
	if len(page.Items) != 2 || page.Items[0].ID != created[1].ID || page.Items[1].ID != created[2].ID {
		t.Fatalf("unexpected mcp page items: %#v", page.Items)
	}

	var tail struct {
		Items      []agentservice.MCPServerView `json:"items"`
		Limit      int                          `json:"limit"`
		HasMore    bool                         `json:"has_more"`
		NextBefore string                       `json:"next_before"`
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers?limit=2&before="+url.QueryEscape(page.NextBefore), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list mcp tail status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&tail); err != nil {
		t.Fatalf("decode mcp tail: %v", err)
	}
	if tail.HasMore || tail.NextBefore != "" || len(tail.Items) != 1 || tail.Items[0].ID != created[0].ID {
		t.Fatalf("unexpected mcp tail: %#v", tail)
	}
}

func TestMCPLocalServerStartAndStop(t *testing.T) {
	_, _, token, server := newMCPAuthenticatedServer(t)

	cmd := os.Args[0]
	body := fmt.Sprintf(`{"kind":"local","name":"Local Echo","command":%q,"args":["-test.run=TestLocalMCPHelperProcess","--"],"env":{"GO_WANT_LOCAL_MCP_HELPER":"1"}}`, cmd)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var created agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode local MCP server: %v", err)
	}
	if created.Kind != "local" || created.Command != cmd {
		t.Fatalf("unexpected local MCP create result: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/start", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var started agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&started); err != nil {
		t.Fatalf("decode started local MCP server: %v", err)
	}
	if !started.Running || started.HealthStatus != "running" {
		t.Fatalf("unexpected started local MCP server: %#v", started)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/mcp/servers/"+created.ID+"/tools", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("local MCP tools status = %d body = %s", w.Code, w.Body.String())
	}
	var tools struct {
		Items []agentservice.MCPToolView `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode local MCP tools: %v", err)
	}
	if len(tools.Items) != 1 || tools.Items[0].Name != "echo" {
		t.Fatalf("unexpected local MCP tools: %#v", tools.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/mcp/servers/"+created.ID+"/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stop local MCP status = %d body = %s", w.Code, w.Body.String())
	}
	var stopped agentservice.MCPServerView
	if err := json.NewDecoder(w.Body).Decode(&stopped); err != nil {
		t.Fatalf("decode stopped local MCP server: %v", err)
	}
	if stopped.Running || stopped.HealthStatus != "stopped" {
		t.Fatalf("unexpected stopped local MCP server: %#v", stopped)
	}
}

func newMCPAuthenticatedServer(t *testing.T) (string, string, string, *HTTPServer) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	return tenant.ID, user.ID, token, NewHTTPServer(svc, "admin-secret")
}

func TestLocalMCPHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_LOCAL_MCP_HELPER") != "1" {
		return
	}
	defer os.Exit(0)
	reader := bufio.NewReader(os.Stdin)
	writer := os.Stdout
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		if method == "notifications/initialized" {
			continue
		}
		id := int(req["id"].(float64))
		switch method {
		case "initialize":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{}}}\n", id)
		case "tools/list":
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"echo\",\"description\":\"Echo text\",\"inputSchema\":{\"type\":\"object\"}}]}}\n", id)
		default:
			_, _ = fmt.Fprintf(writer, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{}}\n", id)
		}
	}
}

func TestCancelRunEndpointCancelsRunningExecution(t *testing.T) {
	store := agentservice.NewMemoryStore()
	executor := &blockingExecutor{started: make(chan string, 1)}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	resultCh := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		body := bytes.NewBufferString(`{"content":"please block"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", body)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		resultCh <- rec
	}()

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for executor to start")
	}

	var runID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := store.ListRuns(tenant.ID, user.ID, inst.ID)
		if err == nil && len(runs) > 0 {
			runID = runs[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runID == "" {
		t.Fatalf("expected run to be persisted")
	}

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/runs/"+runID+"/cancel", nil)
	cancelReq.Header.Set("Authorization", "Bearer "+token)
	cancelRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel run status = %d body = %s", cancelRec.Code, cancelRec.Body.String())
	}
	var cancelled agentservice.Run
	if err := json.NewDecoder(cancelRec.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancelled run: %v", err)
	}
	if cancelled.Status != agentservice.RunStatusCancelled {
		t.Fatalf("expected cancelled run, got %#v", cancelled)
	}

	select {
	case rec := <-resultCh:
		if rec.Code != http.StatusConflict {
			t.Fatalf("send message status = %d body = %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for send request to finish after cancel")
	}

	storedRun, err := store.GetRun(tenant.ID, user.ID, inst.ID, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if storedRun.Status != agentservice.RunStatusCancelled {
		t.Fatalf("stored run = %#v", storedRun)
	}
}

func TestUpdateInstanceEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Old Name", Description: "old desc", Metadata: map[string]string{"tier": "dev"}})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	body := bytes.NewBufferString(`{"name":"Renamed Instance","description":"new desc","metadata":{"tier":"prod","region":"cn"}}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/instances/"+inst.ID, body)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update instance status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.Instance
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated instance: %v", err)
	}
	if updated.Name != "Renamed Instance" || updated.Description != "new desc" {
		t.Fatalf("unexpected instance update: %#v", updated)
	}
	if len(updated.Metadata) != 2 || updated.Metadata["tier"] != "prod" || updated.Metadata["region"] != "cn" {
		t.Fatalf("unexpected metadata: %#v", updated.Metadata)
	}
}

func TestUpdateSessionEndpoint(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Old", Metadata: map[string]string{"a": "1"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	body := bytes.NewBufferString(`{"title":"Renamed","metadata":{"env":"prod","region":"cn"}}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID, body)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update session status = %d body = %s", w.Code, w.Body.String())
	}
	var updated agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated session: %v", err)
	}
	if updated.Title != "Renamed" {
		t.Fatalf("unexpected title: %#v", updated)
	}
	if len(updated.Metadata) != 2 || updated.Metadata["env"] != "prod" || updated.Metadata["region"] != "cn" {
		t.Fatalf("unexpected metadata: %#v", updated.Metadata)
	}
}

func TestArchiveSessionLifecycle(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/archive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("archive session status = %d body = %s", w.Code, w.Body.String())
	}
	var archived agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archived session: %v", err)
	}
	if !archived.Archived || archived.ArchivedAt == nil {
		t.Fatalf("expected archived session, got %#v", archived)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list sessions status = %d body = %s", w.Code, w.Body.String())
	}
	var listed struct {
		Items []agentservice.Session `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("expected archived session hidden from default list, got %#v", listed.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/sessions?include_archived=true", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list archived sessions status = %d body = %s", w.Code, w.Body.String())
	}
	listed = struct {
		Items []agentservice.Session `json:"items"`
	}{}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode archived sessions: %v", err)
	}
	if len(listed.Items) != 1 || !listed.Items[0].Archived {
		t.Fatalf("expected archived session in explicit list, got %#v", listed.Items)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/messages", bytes.NewBufferString(`{"content":"hello"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("post to archived session status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID+"/restore", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("restore session status = %d body = %s", w.Code, w.Body.String())
	}
	var restored agentservice.Session
	if err := json.NewDecoder(w.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restored session: %v", err)
	}
	if restored.Archived || restored.ArchivedAt != nil {
		t.Fatalf("expected restored session, got %#v", restored)
	}
}

func TestDeleteSessionRemovesMessagesAndRuns(t *testing.T) {
	store := agentservice.NewMemoryStore()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveMessage(agentservice.Message{ID: "msg_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Role: agentservice.MessageRoleUser, Content: "hello", CreatedAt: now}); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID+"/sessions/"+sess.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete session status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetSession(context.Background(), principal, inst.ID, sess.ID); err == nil {
		t.Fatalf("expected deleted session to be missing")
	}
	msgs, err := store.ListMessages(sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected session messages deleted, got %#v", msgs)
	}
	if _, err := store.GetRun(tenant.ID, user.ID, inst.ID, "run_1"); err == nil {
		t.Fatalf("expected session runs deleted")
	}
}

func TestDeleteInstanceRemovesRuntimeAndChildren(t *testing.T) {
	store := agentservice.NewMemoryStore()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, store, agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, agentservice.CreateSessionInput{Title: "Demo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	now := time.Now().UTC()
	if err := store.SaveRun(agentservice.Run{ID: "run_1", TenantID: tenant.ID, UserID: user.ID, InstanceID: inst.ID, SessionID: sess.ID, Status: agentservice.RunStatusSucceeded, StartedAt: now}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/instances/"+inst.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete instance status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := svc.GetInstance(context.Background(), principal, inst.ID); err == nil {
		t.Fatalf("expected deleted instance to be missing")
	}
	if _, err := svc.GetSession(context.Background(), principal, inst.ID, sess.ID); err == nil {
		t.Fatalf("expected child session to be removed")
	}
	if _, err := store.GetRun(tenant.ID, user.ID, inst.ID, "run_1"); err == nil {
		t.Fatalf("expected child run to be removed")
	}
	if _, err := os.Stat(inst.RuntimeDir); !os.IsNotExist(err) {
		t.Fatalf("expected runtime dir removed, stat err = %v", err)
	}
}

func TestRunEventsStreamPublishesRunningAndDoneSnapshots(t *testing.T) {
	executor := &blockingExecutor{started: make(chan string, 1), release: make(chan struct{})}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret")
	httpSrv := httptest.NewServer(server.Handler())
	defer httpSrv.Close()

	resultCh := make(chan error, 1)
	go func() {
		body := bytes.NewBufferString(`{"content":"please stream"}`)
		req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/api/v1/instances/"+inst.ID+"/messages", body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resultCh <- err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			resultCh <- fmt.Errorf("send status=%d", resp.StatusCode)
			return
		}
		resultCh <- nil
	}()

	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for executor start")
	}

	var runID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := svc.ListRuns(context.Background(), principal, inst.ID, agentservice.ListRunsInput{})
		if err == nil && len(runs) > 0 {
			runID = runs[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runID == "" {
		t.Fatalf("expected run id")
	}

	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+"/api/v1/instances/"+inst.ID+"/runs/"+runID+"/events", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("unexpected content type: %s", got)
	}

	reader := bufio.NewReader(resp.Body)
	seenRunning := false
	seenDone := false
	for !seenDone {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("ReadString: %v", err)
		}
		if strings.HasPrefix(line, "data: ") {
			var envelope struct {
				Type     string `json:"type"`
				Snapshot struct {
					Run struct {
						Status string `json:"status"`
					} `json:"run"`
					AssistantMessage *struct {
						Content string `json:"content"`
					} `json:"assistant_message,omitempty"`
				} `json:"snapshot"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &envelope); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if envelope.Type == "snapshot" && envelope.Snapshot.Run.Status == string(agentservice.RunStatusRunning) {
				if !seenRunning {
					seenRunning = true
					close(executor.release)
				}
			}
			if envelope.Type == "done" {
				seenDone = true
				if envelope.Snapshot.Run.Status != string(agentservice.RunStatusSucceeded) {
					t.Fatalf("expected succeeded done event, got %#v", envelope)
				}
				if envelope.Snapshot.AssistantMessage == nil || envelope.Snapshot.AssistantMessage.Content != "released" {
					t.Fatalf("expected final assistant message, got %#v", envelope)
				}
			}
		}
	}
	if !seenRunning {
		t.Fatalf("expected running snapshot before done")
	}
	if err := <-resultCh; err != nil {
		t.Fatalf("send request failed: %v", err)
	}
}
