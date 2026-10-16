package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdmitUserJobDeduplicatesWithoutPersistingSensitiveRequest(t *testing.T) {
	root := t.TempDir()
	mgr := newAsyncJobManager(root)
	t.Cleanup(mgr.close)
	server := &HTTPServer{jobs: mgr}
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/migration/export", nil)
	req.Header.Set("Idempotency-Key", "migration-client-key")
	requestIdentity := struct {
		Password string `json:"password"`
	}{Password: "migration-password-secret"}
	release := make(chan struct{})
	var calls atomic.Uint32
	run := func(ctx context.Context) (any, error) {
		calls.Add(1)
		if _, ok := agentruntime.JobIdempotencyIdentityFromContext(ctx); !ok {
			return nil, errors.New("missing idempotency identity")
		}
		<-release
		return nil, nil
	}
	first, err := server.admitUserJob(req, principal, "migration.export", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, requestIdentity, run)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := server.admitUserJob(req, principal, "migration.export", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, requestIdentity, run)
	if err != nil || replay.ID != first.ID || !replay.IdempotentReplay {
		t.Fatalf("replay = %#v err=%v, first=%#v", replay, err, first)
	}
	conflictingRequest := struct {
		Password string `json:"password"`
	}{Password: "different-password-secret"}
	if conflict, err := server.admitUserJob(req, principal, "migration.export", agentruntime.JobRecoveryPolicyReconcile, agentruntime.JobRetryPolicy{}, conflictingRequest, run); conflict != nil || !errors.Is(err, agentruntime.ErrJobIdempotencyConflict) {
		t.Fatalf("conflicting request = %#v err=%v", conflict, err)
	}
	payload, err := json.Marshal(loadPersistedAsyncJobsForTest(t, mgr))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"migration-client-key", "migration-password-secret", "different-password-secret"} {
		if strings.Contains(string(payload), secret) {
			t.Fatalf("persisted Job exposed sensitive admission input %q: %s", secret, payload)
		}
	}
	close(release)
}

func TestAdmitUserJobRejectsAmbiguousWhitespaceKey(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	server := &HTTPServer{jobs: mgr}
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	for _, key := range []string{"   ", " client-key", "client-key "} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", nil)
		req.Header.Set("Idempotency-Key", key)
		job, err := server.admitUserJob(req, principal, "test.whitespace", agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, map[string]bool{"ok": true}, func(context.Context) (any, error) {
			return nil, nil
		})
		if job != nil || !errors.Is(err, agentruntime.ErrInvalidJobIdempotency) {
			t.Fatalf("key %q admitted ambiguous identity: job=%#v err=%v", key, job, err)
		}
	}
}

func TestAdmitUserJobRejectsEmptyOrRepeatedIdempotencyHeader(t *testing.T) {
	mgr := newAsyncJobManager(t.TempDir())
	t.Cleanup(mgr.close)
	server := &HTTPServer{jobs: mgr}
	principal := agentservice.Principal{TenantID: "tenant", UserID: "user"}
	for _, values := range [][]string{{""}, {"one", "two"}} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", nil)
		req.Header["Idempotency-Key"] = values
		if _, err := server.admitUserJob(req, principal, "test.header", agentruntime.JobRecoveryPolicyFail, agentruntime.JobRetryPolicy{}, map[string]bool{"ok": true}, func(context.Context) (any, error) { return nil, nil }); !errors.Is(err, agentruntime.ErrInvalidJobIdempotency) {
			t.Fatalf("header values %#v error=%v, want invalid idempotency", values, err)
		}
	}
}
