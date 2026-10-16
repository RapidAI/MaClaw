package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// newIdempotencyFixture builds one instance, one session and one token so a
// test can post to either message endpoint with the same body.
func newIdempotencyFixture(t *testing.T) (*HTTPServer, string, string, string) {
	t.Helper()
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), &countingExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	ctx := context.Background()
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(ctx, principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := svc.CreateSession(ctx, principal, inst.ID, agentservice.CreateSessionInput{Title: "s"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	t.Cleanup(server.Close)
	return server, inst.ID, sess.ID, token
}

// countingExecutor answers at once so a synchronous POST settles inside the
// request.
type countingExecutor struct {
	calls int
}

func (e *countingExecutor) Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	e.calls++
	return &agentservice.ExecuteResult{Content: "done:" + req.Message.Content}, nil
}

// runIDOf reads the run id a synchronous message response carries.
func runIDOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return body.Run.ID
}

// Both message endpoints must accept the per-turn idempotency key at the same
// JSON position and replay instead of executing twice. They used to disagree:
// PostMessageInput had no top-level client_message_id, so a caller that posted
// it there had the key dropped by the decoder, and every retry of a lost
// answer started another run while believing the turn was protected. That is
// the failure this guards, which is why it must assert on both routes at once
// — before the fix each route alone would have looked correct.
func TestBothMessageEndpointsHonorTopLevelClientMessageID(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(instanceID, sessionID string) string
		// pin carries whatever that route uses to keep the session stable.
		// The idempotency scope is (tenant, user, instance, session, message
		// key), so an unpinned session makes a second request resolve a new
		// one and replay has nothing to match. The session endpoint names its
		// session in the URL and therefore needs no extra field; posting one
		// there would be rejected, because this API refuses unknown fields.
		pin string
	}{
		{"convenience endpoint", func(instanceID, _ string) string {
			return "/api/v1/instances/" + instanceID + "/messages"
		}, `"client_session_key":"idem-session",`},
		{"session endpoint", func(instanceID, sessionID string) string {
			return "/api/v1/instances/" + instanceID + "/sessions/" + sessionID + "/messages"
		}, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, instID, sessID, token := newIdempotencyFixture(t)
			post := func(body string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, tc.path(instID, sessID), bytes.NewBufferString(body))
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				server.Handler().ServeHTTP(w, req)
				return w
			}
			first := post(`{"content":"hello",` + tc.pin + `"client_message_id":"turn-key-1"}`)
			if first.Code != http.StatusOK {
				t.Fatalf("first post status = %d body = %s", first.Code, first.Body.String())
			}
			firstRun := runIDOf(t, first)
			if firstRun == "" {
				t.Fatalf("no run id in %s", first.Body.String())
			}
			replay := post(`{"content":"hello",` + tc.pin + `"client_message_id":"turn-key-1"}`)
			if replay.Code != http.StatusOK {
				t.Fatalf("replay status = %d body = %s", replay.Code, replay.Body.String())
			}
			if got := runIDOf(t, replay); got != firstRun {
				t.Fatalf("top-level client_message_id did not replay: run %q -> %q", firstRun, got)
			}
		})
	}
}

// The metadata spelling the desktop path already used must keep working after
// the top-level field was added, on both routes.
func TestBothMessageEndpointsHonorMetadataClientMessageID(t *testing.T) {
	for _, tc := range []struct {
		name string
		path func(instanceID, sessionID string) string
		pin  string
	}{
		{"convenience endpoint", func(instanceID, _ string) string {
			return "/api/v1/instances/" + instanceID + "/messages"
		}, `"client_session_key":"idem-session",`},
		{"session endpoint", func(instanceID, sessionID string) string {
			return "/api/v1/instances/" + instanceID + "/sessions/" + sessionID + "/messages"
		}, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, instID, sessID, token := newIdempotencyFixture(t)
			post := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, tc.path(instID, sessID),
					bytes.NewBufferString(`{"content":"hello",`+tc.pin+`"metadata":{"client_message_id":"turn-key-meta"}}`))
				req.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				server.Handler().ServeHTTP(w, req)
				return w
			}
			first := post()
			if first.Code != http.StatusOK {
				t.Fatalf("first post status = %d body = %s", first.Code, first.Body.String())
			}
			replay := post()
			if replay.Code != http.StatusOK {
				t.Fatalf("replay status = %d body = %s", replay.Code, replay.Body.String())
			}
			if firstRun, got := runIDOf(t, first), runIDOf(t, replay); got != firstRun {
				t.Fatalf("metadata client_message_id did not replay: run %q -> %q", firstRun, got)
			}
		})
	}
}

// A key posted top-level must also participate in conflict detection against
// the Idempotency-Key header. Comparing only metadata used to let a top-level
// body value escape the check entirely, so header and body could disagree and
// the header silently won.
func TestIdempotencyHeaderMismatchRejectsTopLevelBodyKey(t *testing.T) {
	server, instID, sessID, token := newIdempotencyFixture(t)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/instances/"+instID+"/sessions/"+sessID+"/messages",
		bytes.NewBufferString(`{"content":"hello","client_message_id":"body-key"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", "header-key")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mismatched key status = %d body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "does not match client_message_id") {
		t.Fatalf("unexpected body = %s", w.Body.String())
	}
}
