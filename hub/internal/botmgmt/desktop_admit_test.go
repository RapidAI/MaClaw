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
			admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "按这个安排执行", "execute", "")
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
			if desk.stop != 0 || desk.open != 0 {
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
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", ""); err != nil {
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
	if desk.open != 0 || desk.stop != 0 {
		t.Fatalf("failed run opened or stopped a desktop it did not start open=%d stop=%d", desk.open, desk.stop)
	}
}

func TestAdmittedFailedRunKeepsSendToUser(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"id":"run_fail","status":"failed","desktop_ready":true,"error":"browser broke","message":{"content":"无变化","metadata":{"send_to_user":"首页没打开。","ask_user_question":"下一步？","ask_user_secret_name":"SITE_PASSWORD"}}}`))
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
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "execute", ""); err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	var reply Reply
	for {
		got, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_fail")
		if done {
			if err == nil || !strings.Contains(err.Error(), "browser broke") {
				t.Fatalf("done err=%v", err)
			}
			reply = got
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
	if reply.SendToUser != "首页没打开。" || reply.Text != "" || strings.Contains(reply.Text, "无变化") || reply.AskUserQuestion != "下一步？" || reply.AskUserSecretName != "SITE_PASSWORD" {
		t.Fatalf("reply = %#v", reply)
	}
	pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Reply.SendToUser != "首页没打开。" || strings.Contains(pending[0].Reply.Text, "无变化") || pending[0].Reply.AskUserQuestion != "下一步？" || pending[0].Status != "failed" {
		t.Fatalf("pending = %#v", pending)
	}
	shown, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "没有完成") || !strings.Contains(shown, "首页没打开。") || strings.Contains(shown, "无变化") || strings.Contains(shown, "browser broke") {
		t.Fatalf("status = %q", shown)
	}
}

func TestAdmittedFailedRunKeepsHandoff(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"id":"run_fail","status":"failed","desktop_ready":true,"error":"browser broke","desktop_handoff":true,"attention_reason":"login_wall","message":{"content":"安装过程写在这里","metadata":{"send_to_user":"这一步需要你登录。","ask_user_question":"请输入密码。","ask_user_secret_name":"SITE_PASSWORD"}}}`))
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
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "execute", ""); err != nil {
		t.Fatal(err)
	}
	gate.Lock()
	ready = true
	gate.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	var reply Reply
	for {
		got, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_fail")
		if done {
			if err == nil || !strings.Contains(err.Error(), "browser broke") {
				t.Fatalf("done err=%v", err)
			}
			reply = got
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
	if desk.stop != 0 {
		t.Fatalf("failed handoff stopped the desktop, stop=%d", desk.stop)
	}
	if !reply.Handoff || reply.AttentionReason != "login_wall" || reply.SendToUser != "这一步需要你登录。" || reply.AskUserQuestion != "请输入密码。" || reply.AskUserSecretName != "SITE_PASSWORD" || reply.Text != "" || strings.Contains(reply.Text, "安装过程") {
		t.Fatalf("reply = %#v", reply)
	}
	if !strings.HasPrefix(reply.NovncURL, "/api/v1/desktop-handoff/") || strings.Contains(reply.NovncURL, "dockerd.example") {
		t.Fatalf("novnc = %q", reply.NovncURL)
	}
	_, control, err := svc.DesktopWatch(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil || !control {
		t.Fatalf("watch control=%v err=%v", control, err)
	}
	pending, err := svc.PendingDesktopDeliveries(context.Background(), "tenant-a", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !pending[0].Reply.Handoff || pending[0].Reply.AttentionReason != "login_wall" || pending[0].Reply.SendToUser != "这一步需要你登录。" || strings.Contains(pending[0].Reply.Text, "安装过程") || pending[0].Status != "failed" {
		t.Fatalf("pending = %#v", pending)
	}
	shown, _, err := svc.DesktopTaskQuery(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "zh-Hans")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "没有完成") || !strings.Contains(shown, "这一步需要你登录。") || strings.Contains(shown, "安装过程") || strings.Contains(shown, "browser broke") {
		t.Fatalf("status = %q", shown)
	}
}

// TestDesktopRunSupplementsReportsTheRecordedOutcome checks the read side of
// the supplement contract: the final states of one run's supplements come back
// verbatim, addressed through the bot's own instance, and an unknown bot is
// reported as not found rather than proxied.
func TestDesktopRunSupplementsReportsTheRecordedOutcome(t *testing.T) {
	var gotPath string
	tokenCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			tokenCalls++
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/runs/run_42/supplements"):
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"items":[{"id":"sup-1","run_id":"run_42","state":"missed","content":"只要前5条"}]}`))
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

	items, err := svc.DesktopRunSupplements(context.Background(), "tenant-a", "alice", "bot_alice", "run_42")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "sup-1" || items[0].State != "missed" || items[0].Content != "只要前5条" {
		t.Fatalf("items = %#v", items)
	}
	if want := "/api/v1/instances/inst_alice/runs/run_42/supplements"; !strings.HasSuffix(gotPath, want) {
		t.Fatalf("asked %q, want suffix %q", gotPath, want)
	}
	if tokenCalls == 0 {
		t.Fatal("the proxy did not authenticate as the owner")
	}

	// Another bot's record is not this caller's, so it is not found — the
	// proxy must not widen the question past the named bot.
	if _, err := svc.DesktopRunSupplements(context.Background(), "tenant-a", "alice", "bot_other", "run_42"); err == nil {
		t.Fatal("an unknown bot answered")
	}
}
