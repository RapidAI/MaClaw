package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestNotReadyMessageDoesNotOpenTheDesktop(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Bot", AllowInvalidConfig: true})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatalf("Issue token: %v", err)
	}
	opened := 0
	previous := desktopRemoteSession
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		opened++
		return desktopEndpoint{}, nil
	}
	t.Cleanup(func() { desktopRemoteSession = previous })

	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewBufferString(`{"content":"hello","client_session_key":"bot-1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "instance is not ready: user LLM configuration is incomplete") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if opened != 0 {
		t.Fatalf("desktop opens = %d", opened)
	}
	runs, err := svc.ListRuns(context.Background(), principal, inst.ID, agentservice.ListRunsInput{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %d", len(runs))
	}
	sessions, err := svc.ListSessions(context.Background(), principal, inst.ID, agentservice.ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions = %d", len(sessions))
	}

	sessionReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/sessions/sess-missing/messages", bytes.NewBufferString(`{"content":"hello"}`))
	sessionReq.Header.Set("Authorization", "Bearer "+token)
	sessionRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusBadRequest || !strings.Contains(sessionRec.Body.String(), "instance is not ready: user LLM configuration is incomplete") {
		t.Fatalf("session status = %d body = %s", sessionRec.Code, sessionRec.Body.String())
	}

	asyncReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewBufferString(`{"content":"hello","client_session_key":"bot-async"}`))
	asyncReq.Header.Set("Authorization", "Bearer "+token)
	asyncReq.Header.Set("Prefer", "respond-async")
	asyncRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(asyncRec, asyncReq)
	if asyncRec.Code != http.StatusBadRequest || !strings.Contains(asyncRec.Body.String(), "instance is not ready: user LLM configuration is incomplete") {
		t.Fatalf("async status = %d body = %s", asyncRec.Code, asyncRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/api/v1/instances/inst_missing/messages", bytes.NewBufferString(`{"content":"hello"}`))
	missingReq.Header.Set("Authorization", "Bearer "+token)
	missingRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound || !strings.Contains(missingRec.Body.String(), "instance not found") {
		t.Fatalf("missing status = %d body = %s", missingRec.Code, missingRec.Body.String())
	}
	if opened != 0 {
		t.Fatalf("desktop opens = %d", opened)
	}
}

func TestNotReadyScheduledTaskDoesNotOpenTheDesktop(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Bot", AllowInvalidConfig: true})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	opened := 0
	previous := desktopRemoteSession
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		opened++
		return desktopEndpoint{}, nil
	}
	t.Cleanup(func() { desktopRemoteSession = previous })

	_, err = runSrvScheduledTaskAction(context.Background(), svc, nil, principal, &scheduler.ScheduledTask{
		ID:            "task-1",
		InstanceID:    inst.ID,
		OwnerTenantID: tenant.ID,
		OwnerUserID:   user.ID,
		Action:        "hello",
	})
	if err == nil || !strings.Contains(err.Error(), "instance is not ready: user LLM configuration is incomplete") {
		t.Fatalf("err = %v", err)
	}
	if opened != 0 {
		t.Fatalf("desktop opens = %d", opened)
	}
	runs, err := svc.ListRuns(context.Background(), principal, inst.ID, agentservice.ListRunsInput{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %d", len(runs))
	}
}

func TestReadyScheduledTaskOpensTheDesktopForTheRun(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alice"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Bot"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	opened := 0
	previousSession := desktopRemoteSession
	previousStop := desktopRemoteStop
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		opened++
		return desktopEndpoint{}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error { return nil }
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteStop = previousStop
	})

	task := &scheduler.ScheduledTask{
		ID:            "task-ready",
		InstanceID:    inst.ID,
		OwnerTenantID: tenant.ID,
		OwnerUserID:   user.ID,
		Action:        "hello",
	}
	result, err := runSrvScheduledTaskAction(context.Background(), svc, nil, principal, task)
	if err != nil || !strings.Contains(result, "hello") {
		t.Fatalf("result=%q err=%v", result, err)
	}
	result, err = runSrvScheduledTaskAction(context.Background(), svc, nil, principal, task)
	if err != nil || !strings.Contains(result, "hello") {
		t.Fatalf("second result=%q err=%v", result, err)
	}
	if opened != 2 {
		t.Fatalf("desktop opens = %d", opened)
	}
	runs, err := svc.ListRuns(context.Background(), principal, inst.ID, agentservice.ListRunsInput{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %d", len(runs))
	}
	sessions, err := svc.ListSessions(context.Background(), principal, inst.ID, agentservice.ListSessionsInput{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d", len(sessions))
	}
}
