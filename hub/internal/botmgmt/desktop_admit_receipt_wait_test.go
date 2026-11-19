package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newReceiptWaitService(t *testing.T, srv *httptest.Server) *Service {
	t.Helper()
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.admitPollInterval = 10 * time.Millisecond
	svc.admitReadyGrace = 20 * time.Millisecond
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	return svc
}

// receiptWaitServer serves the pending body until ready is set. onPending
// runs once, when the first not-ready terminal body is served.
func receiptWaitServer(t *testing.T, pending, readyBody string, ready *atomic.Bool, onPending func()) *httptest.Server {
	t.Helper()
	var gate sync.Mutex
	var notified bool
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			goNow := ready != nil && ready.Load()
			first := !notified && !goNow
			if first {
				notified = true
			}
			gate.Unlock()
			if first && onPending != nil {
				onPending()
			}
			if !goNow {
				_, _ = w.Write([]byte(pending))
				return
			}
			_, _ = w.Write([]byte(readyBody))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_pending","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func waitDesktopResult(t *testing.T, svc *Service, runID string) (Reply, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		reply, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", runID)
		if done {
			return reply, err
		}
		if time.Now().After(deadline) {
			t.Fatal("run result did not arrive")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPendingReceiptIsNotDone(t *testing.T) {
	const pending = `{"id":"run_pending","status":"succeeded","desktop_ready":false,"metadata":{"receipt_pending":"1","task_outcome":"done"}}`
	const pendingSentence = `{"id":"run_pending","status":"succeeded","desktop_ready":false,"metadata":{"receipt_pending":"1","task_outcome":"done"},"message":{"metadata":{"send_to_user":"已经发布了"}}}`
	cases := []struct {
		name     string
		body     string
		clientID string
	}{
		{name: "empty body, nobody waiting", body: pending, clientID: ""},
		{name: "empty body, person waiting", body: pending, clientID: "turn-pending"},
		{name: "pre-receipt done sentence", body: pendingSentence, clientID: "turn-pending"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := receiptWaitServer(t, tc.body, "", nil, nil)
			defer srv.Close()
			svc := newReceiptWaitService(t, srv)
			svc.admitReceiptGrace = 30 * time.Millisecond

			admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "发一篇帖子", "execute", tc.clientID)
			if err != nil {
				t.Fatal(err)
			}
			reply, runErr := waitDesktopResult(t, svc, admission.RunID)
			if reply.TaskOutcome == "done" {
				t.Fatalf("pending receipt settled as done: %#v err=%v", reply, runErr)
			}
			if runErr == nil && reply.TaskOutcome != "unverified" {
				t.Fatalf("pending receipt was a quiet success: %#v", reply)
			}
			if strings.TrimSpace(reply.SendToUser) != "" && !strings.Contains(reply.SendToUser, desktopUnverifiedSentence) {
				t.Fatalf("sentence omitted the unverified line: %q", reply.SendToUser)
			}
		})
	}
}

func TestPendingReceiptWaitsUntilReady(t *testing.T) {
	const pending = `{"id":"run_pending","status":"succeeded","desktop_ready":false,"metadata":{"receipt_pending":"1","task_outcome":"done"},"message":{"metadata":{"send_to_user":"已经发布了"}}}`
	const readyBody = `{"id":"run_pending","status":"succeeded","desktop_ready":true,"metadata":{"task_outcome":"partial"},"message":{"content":"页面上还没有帖子","metadata":{"task_outcome":"partial","send_to_user":"页面上还没有帖子"}}}`
	pendingSeen := make(chan struct{})
	var ready atomic.Bool
	var once sync.Once
	srv := receiptWaitServer(t, pending, readyBody, &ready, func() {
		once.Do(func() { close(pendingSeen) })
	})
	defer srv.Close()
	svc := newReceiptWaitService(t, srv)

	admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "发一篇帖子", "execute", "turn-pending")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-pendingSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("srv was not read while the receipt was pending")
	}
	// Past the short grace (20ms). receipt_pending must still be open.
	time.Sleep(50 * time.Millisecond)
	if _, done, _ := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID); done {
		t.Fatal("settled during receipt_pending, before desktop_ready")
	}
	ready.Store(true)

	reply, runErr := waitDesktopResult(t, svc, admission.RunID)
	if runErr != nil {
		t.Fatal(runErr)
	}
	if reply.TaskOutcome != "partial" || !strings.Contains(reply.SendToUser, "页面上还没有帖子") || !strings.Contains(reply.SendToUser, "只做完了一部分") {
		t.Fatalf("ready receipt = %#v", reply)
	}
}
