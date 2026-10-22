package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestPauseResumeRunEndpoints(t *testing.T) {
	store := agentservice.NewMemoryStore()
	executor := &blockingExecutor{started: make(chan string, 1)}
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, store, executor)
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "T"})
	user, _ := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "U"})
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	inst, _ := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "I"})
	token, _, _ := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	server := NewHTTPServer(svc, "admin-secret", nil)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		var b *bytes.Buffer
		if body != "" {
			b = bytes.NewBufferString(body)
		} else {
			b = &bytes.Buffer{}
		}
		req := httptest.NewRequest(method, path, b)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	go do(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", `{"content":"please block"}`)
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor not started")
	}
	var runID string
	for i := 0; i < 200 && runID == ""; i++ {
		if runs, _ := store.ListRuns(tenant.ID, user.ID, inst.ID); len(runs) > 0 {
			runID = runs[0].ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	base := "/api/v1/instances/" + inst.ID + "/runs/" + runID
	rec := do(http.MethodPost, base+"/pause", `{"reason":"look","timeout_sec":60}`)
	var st agentservice.RunPauseState
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if rec.Code != 200 || !st.Paused || st.TimeoutSec != 60 {
		t.Fatalf("pause %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, base+"/pause", "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"paused":true`)) {
		t.Fatalf("get %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/resume", "")
	if rec.Code != 200 || !bytes.Contains(rec.Body.Bytes(), []byte(`"resumed"`)) {
		t.Fatalf("resume %d %s", rec.Code, rec.Body.String())
	}
	if rec = do(http.MethodPost, base+"/pause", `{bad`); rec.Code != 400 {
		t.Fatalf("bad body %d", rec.Code)
	}
	_ = do(http.MethodPost, base+"/cancel", "")
	if rec = do(http.MethodPost, base+"/pause", ""); rec.Code < 400 {
		t.Fatalf("pause cancelled run %d", rec.Code)
	}
}
