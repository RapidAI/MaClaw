package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// holdingExecutor blocks inside the run. The admitting request returns first.
type holdingExecutor struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	handoff bool
}

func (e *holdingExecutor) Execute(ctx context.Context, req agentservice.ExecuteRequest) (*agentservice.ExecuteResult, error) {
	if !noteDesktopShot(ctx, "image/png", "aGVsbG8=") {
		return nil, context.Canceled
	}
	if e.handoff {
		noteDesktopHandoff(req.Principal.TenantID, req.Principal.UserID, req.Instance.ID)
		noteDesktopAttention(req.Principal.TenantID, req.Principal.UserID, req.Instance.ID, "payment_confirm")
	}
	e.once.Do(func() { close(e.started) })
	select {
	case <-e.release:
		return &agentservice.ExecuteResult{Content: "桌面操作完成"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAsyncDesktopTurnOutlivesTheAdmittingRequest(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		name := "stops when the run finishes"
		if handoff {
			name = "keeps a handoff after the caller is gone"
		}
		t.Run(name, func(t *testing.T) {
			executor := &holdingExecutor{
				started: make(chan struct{}),
				release: make(chan struct{}),
				handoff: handoff,
			}
			var releaseOnce sync.Once
			releaseRun := func() { releaseOnce.Do(func() { close(executor.release) }) }
			stops := 0
			previousStop := desktopRemoteStop
			desktopRemoteStop = func(context.Context, string, string) error {
				stops++
				return nil
			}
			t.Cleanup(func() {
				releaseRun()
				desktopRemoteStop = previousStop
			})

			svc, err := agentservice.NewService(agentservice.Config{
				DataRoot:    t.TempDir(),
				TokenSecret: "test-token-secret-0123456789012345",
			}, agentservice.NewMemoryStore(), executor)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
			if err != nil {
				t.Fatal(err)
			}
			user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
			if err != nil {
				t.Fatal(err)
			}
			principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
			if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
				t.Fatal(err)
			}
			inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
			if err != nil {
				t.Fatal(err)
			}
			token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
			if err != nil {
				t.Fatal(err)
			}
			server := NewHTTPServer(svc, "admin-secret", nil)
			t.Cleanup(server.Close)
			runKey := desktopRunKey(tenant.ID, user.ID)
			t.Cleanup(func() {
				desktopRunsMu.Lock()
				delete(desktopRuns, runKey)
				delete(desktopHolds, runKey)
				desktopRunsMu.Unlock()
				desktopTurnResults.Delete("")
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewBufferString(`{"content":"打开页面","client_session_key":"bot_async"}`))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Prefer", "respond-async")
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusAccepted {
				t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
			}
			var admitted struct {
				Async bool `json:"async"`
				Run   struct {
					ID string `json:"id"`
				} `json:"run"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &admitted); err != nil {
				t.Fatal(err)
			}
			if !admitted.Async || admitted.Run.ID == "" {
				t.Fatalf("admission = %s", w.Body.String())
			}
			select {
			case <-executor.started:
			case <-time.After(2 * time.Second):
				t.Fatal("desktop run did not start after admission")
			}
			desktopRunsMu.Lock()
			held := desktopRuns[runKey]
			desktopRunsMu.Unlock()
			if held < 1 {
				t.Fatalf("desktop run count = %d after the caller returned", held)
			}
			if stops != 0 {
				t.Fatalf("desktop stopped at admission, stops=%d", stops)
			}

			releaseRun()
			deadline := time.Now().Add(2 * time.Second)
			var body desktopRunAPI
			for {
				read := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
				read.Header.Set("Authorization", "Bearer "+token)
				got := httptest.NewRecorder()
				server.Handler().ServeHTTP(got, read)
				if got.Code != http.StatusOK {
					t.Fatalf("run status = %d body = %s", got.Code, got.Body.String())
				}
				if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.DesktopReady && body.Status == agentservice.RunStatusSucceeded {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("run did not finish after the caller left: %#v", body.Run)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if body.Message == nil || body.Message.Content != "桌面操作完成" {
				t.Fatalf("message = %#v", body.Message)
			}
			if body.DesktopHandoff != handoff || (handoff && body.AttentionReason != "payment_confirm") {
				t.Fatalf("handoff=%v attention=%q", body.DesktopHandoff, body.AttentionReason)
			}
			if body.Message == nil || len(body.Message.Attachments) != 1 || body.Message.Attachments[0].Data != "aGVsbG8=" {
				t.Fatalf("attachments = %#v", body.Message)
			}
			desktopRunsMu.Lock()
			left := desktopRuns[runKey]
			desktopRunsMu.Unlock()
			if left != 0 {
				t.Fatalf("desktop run count = %d after finish", left)
			}
			if handoff && stops != 0 {
				t.Fatalf("handoff stopped the desktop, stops=%d", stops)
			}
			if !handoff && stops != 1 {
				t.Fatalf("finished run stops=%d, want 1", stops)
			}
			t.Cleanup(func() { desktopTurnResults.Delete(admitted.Run.ID) })
		})
	}
}

// A second Prefer with the same Idempotency-Key occupies again and finishes
// immediately. That finish must not take the handoff or publish a result
// while the original executor is still running.
func TestAsyncReplayDoesNotStealTheDesktopTurn(t *testing.T) {
	executor := &holdingExecutor{
		started: make(chan struct{}),
		release: make(chan struct{}),
		handoff: true,
	}
	var releaseOnce sync.Once
	releaseRun := func() { releaseOnce.Do(func() { close(executor.release) }) }
	stops := 0
	previousStop := desktopRemoteStop
	desktopRemoteStop = func(context.Context, string, string) error {
		stops++
		return nil
	}
	t.Cleanup(func() {
		releaseRun()
		desktopRemoteStop = previousStop
	})

	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
	}, agentservice.NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, testLLMConfig()); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principal)
	if err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	t.Cleanup(server.Close)
	runKey := desktopRunKey(tenant.ID, user.ID)
	handoffKey := desktopHandoffKey(tenant.ID, user.ID, inst.ID)
	t.Cleanup(func() {
		desktopRunsMu.Lock()
		delete(desktopRuns, runKey)
		delete(desktopHolds, runKey)
		desktopRunsMu.Unlock()
		desktopHandoff.Delete(handoffKey)
		desktopAttention.Delete(handoffKey)
	})

	body := `{"content":"打开页面","client_session_key":"bot_replay","client_message_id":"turn-replay"}`
	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/"+inst.ID+"/messages", bytes.NewBufferString(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Prefer", "respond-async")
		req.Header.Set("Idempotency-Key", "turn-replay")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		return w
	}
	first := post()
	if first.Code != http.StatusAccepted {
		t.Fatalf("status = %d body = %s", first.Code, first.Body.String())
	}
	var admitted struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &admitted); err != nil {
		t.Fatal(err)
	}
	if admitted.Run.ID == "" {
		t.Fatalf("admission = %s", first.Body.String())
	}
	t.Cleanup(func() { desktopTurnResults.Delete(admitted.Run.ID) })
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("desktop run did not start")
	}

	replay := post()
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay status = %d body = %s", replay.Code, replay.Body.String())
	}
	var replayed struct {
		Run struct {
			ID string `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Run.ID != admitted.Run.ID {
		t.Fatalf("replay run = %s, want %s", replayed.Run.ID, admitted.Run.ID)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		desktopRunsMu.Lock()
		held := desktopRuns[runKey]
		desktopRunsMu.Unlock()
		_, handoffHeld := desktopHandoff.Load(handoffKey)
		attention, _ := desktopAttention.Load(handoffKey)
		reason, _ := attention.(string)
		if held == 1 && handoffHeld && reason == "payment_confirm" && stops == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after replay held=%d handoff=%v attention=%q stops=%d", held, handoffHeld, reason, stops)
		}
		time.Sleep(10 * time.Millisecond)
	}
	read := httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
	read.Header.Set("Authorization", "Bearer "+token)
	got := httptest.NewRecorder()
	server.Handler().ServeHTTP(got, read)
	var early desktopRunAPI
	if err := json.Unmarshal(got.Body.Bytes(), &early); err != nil {
		t.Fatal(err)
	}
	if early.DesktopReady {
		t.Fatalf("replay published a result: %#v", early)
	}

	releaseRun()
	deadline = time.Now().Add(2 * time.Second)
	var done desktopRunAPI
	for {
		read = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+inst.ID+"/runs/"+admitted.Run.ID, nil)
		read.Header.Set("Authorization", "Bearer "+token)
		got = httptest.NewRecorder()
		server.Handler().ServeHTTP(got, read)
		if err := json.Unmarshal(got.Body.Bytes(), &done); err != nil {
			t.Fatal(err)
		}
		if done.DesktopReady && done.Status == agentservice.RunStatusSucceeded {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("owner run did not finish: %#v", done.Run)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if done.Message == nil || done.Message.Content != "桌面操作完成" || !done.DesktopHandoff || done.AttentionReason != "payment_confirm" {
		t.Fatalf("result = %#v", done)
	}
	if len(done.Message.Attachments) != 1 || done.Message.Attachments[0].Data != "aGVsbG8=" {
		t.Fatalf("attachments = %#v", done.Message.Attachments)
	}
	desktopRunsMu.Lock()
	left := desktopRuns[runKey]
	desktopRunsMu.Unlock()
	if left != 0 || stops != 0 {
		t.Fatalf("left=%d stops=%d", left, stops)
	}
}
