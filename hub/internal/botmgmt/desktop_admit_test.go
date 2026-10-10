package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdmittedDesktopRunFinishesAfterTheCallerReturns(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		name := "reports the reply when the run finishes"
		if handoff {
			name = "keeps the handoff until the run finishes"
		}
		t.Run(name, func(t *testing.T) {
			var gate sync.Mutex
			ready := false
			var sawPrefer bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
					_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
					gate.Lock()
					goNow := ready
					gate.Unlock()
					if !goNow {
						_, _ = w.Write([]byte(`{"id":"run_admitted","status":"running"}`))
						return
					}
					if handoff {
						_, _ = w.Write([]byte(`{"id":"run_admitted","status":"succeeded","desktop_ready":true,"desktop_handoff":true,"attention_reason":"payment_confirm","message":{"content":"需要支付","attachments":[{"type":"image","mime_type":"image/png","data":"aGVsbG8="}]}}`))
						return
					}
					_, _ = w.Write([]byte(`{"id":"run_admitted","status":"succeeded","desktop_ready":true,"message":{"content":"页面已打开","metadata":{"ask_user_question":"下一步？"}}}`))
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
					writeInstanceSettings(w, r)
				case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
					if r.Header.Get("Prefer") != "respond-async" {
						http.Error(w, "prefer", http.StatusBadRequest)
						return
					}
					sawPrefer = true
					raw, _ := io.ReadAll(r.Body)
					if !strings.Contains(string(raw), `"bot_phase":"execute"`) {
						http.Error(w, "phase", http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusAccepted)
					_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_admitted","status":"running"},"status_url":"/api/v1/instances/inst_alice/runs/run_admitted"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
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
			svc.admitReadyGrace = 30 * time.Millisecond
			desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
			svc.Desktop = desk

			started := time.Now()
			admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "按这个安排执行", "execute")
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("admission waited %s", time.Since(started))
			}
			if !admission.Accepted || admission.Settled || admission.RunID != "run_admitted" {
				t.Fatalf("admission = %#v", admission)
			}
			if !sawPrefer {
				t.Fatal("message was not admitted with Prefer: respond-async")
			}
			if desk.stop != 0 || desk.open != 1 {
				t.Fatalf("desktop at admission open=%d stop=%d", desk.open, desk.stop)
			}
			if _, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID); err != nil || done {
				t.Fatalf("result at admission done=%v err=%v", done, err)
			}

			gate.Lock()
			ready = true
			gate.Unlock()
			deadline := time.Now().Add(2 * time.Second)
			var reply Reply
			for {
				got, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if done {
					reply = got
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("run result did not arrive")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if desk.stop != 0 {
				t.Fatalf("finished reply stopped the desktop from Hub, stop=%d", desk.stop)
			}
			if handoff {
				if !reply.Handoff || reply.AttentionReason != "payment_confirm" || reply.Text != "需要支付" || len(reply.Images) != 1 {
					t.Fatalf("handoff reply = %#v", reply)
				}
				_, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
				if err != nil || !control {
					t.Fatalf("watch control=%v err=%v", control, err)
				}
				return
			}
			if reply.Handoff || reply.Text != "页面已打开" || reply.AskUserQuestion != "下一步？" {
				t.Fatalf("reply = %#v", reply)
			}
		})
	}
}

func TestAdmittedDesktopFailureSettlesWhenTheRunFails(t *testing.T) {
	var gate sync.Mutex
	ready := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			gate.Lock()
			goNow := ready
			gate.Unlock()
			if !goNow {
				_, _ = w.Write([]byte(`{"id":"run_fail","status":"running"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"run_fail","status":"failed","desktop_ready":true,"error":"browser broke"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_fail","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
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
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute"); err != nil {
		t.Fatal(err)
	}
	if desk.stop != 0 {
		t.Fatalf("admission stop=%d", desk.stop)
	}
	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_fail")
		if done {
			if err == nil || !strings.Contains(err.Error(), "browser broke") {
				t.Fatalf("done err=%v", err)
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("failure did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if desk.stop != 1 {
		t.Fatalf("failed run stop=%d, want 1", desk.stop)
	}
}
