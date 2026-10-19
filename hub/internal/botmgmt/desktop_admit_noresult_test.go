package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAdmittedEmptyRunDoesNotBlameTheServer pins the wording a bot sees when a
// run reaches a terminal state having produced nothing.
//
// This is the path that produced "我这边暂时连不上我的服务器": a run cancelled
// by an exhausted provider quota came back with no text and no recorded reason,
// and the follow-up wrapped that as "maclawsrv request failed". Nothing had
// failed to reach MaClawSrv — the container had pushed the commits and the
// release — so the bot was handed a false diagnosis and the reader re-sent work
// that was already done. The message must not claim a server failure.
func TestAdmittedEmptyRunDoesNotBlameTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			// Terminal, empty, and no recorded reason — exactly the shape a
			// quota cutoff used to leave behind.
			_, _ = w.Write([]byte(`{"id":"run_empty","status":"cancelled","desktop_ready":true}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_empty","status":"running"}}`))
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
	deadline := time.Now().Add(2 * time.Second)
	var settleErr error
	for {
		_, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_empty")
		if done {
			settleErr = err
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("empty run did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if settleErr == nil {
		t.Fatal("an empty terminal run must still surface an error")
	}
	if strings.Contains(settleErr.Error(), "maclawsrv") {
		t.Fatalf("empty run blamed the server: %v", settleErr)
	}
	if !strings.Contains(settleErr.Error(), "cancelled") {
		t.Fatalf("empty run error=%v should name the run's terminal state", settleErr)
	}
}

func TestDesktopTerminalMissTreatsFillerAsEmpty(t *testing.T) {
	miss := func(status, text, recorded string, handoff bool) error {
		return desktopTerminalMiss("run_1", status, text, recorded, handoff)
	}
	for _, text := range []string{"", "无变化", "(无输出)", "（无输出）", "no change", "后台没有返回结果。"} {
		err := miss("failed", text, "", false)
		if err == nil || !strings.Contains(err.Error(), "failed") || !strings.Contains(err.Error(), "without a result") || strings.Contains(err.Error(), "maclawsrv") {
			t.Fatalf("text %q error=%v", text, err)
		}
	}
	if err := miss("cancelled", "无变化", "", false); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancelled filler error=%v", err)
	}
	if err := miss("failed", "无变化", "", true); err != nil {
		t.Fatalf("handoff is a result: %v", err)
	}
	if err := miss("failed", "无变化", "browser broke", false); err == nil || err.Error() != "browser broke" {
		t.Fatalf("recorded reason=%v", err)
	}
	if err := miss("failed", "页面打不开", "", false); err != nil {
		t.Fatalf("real prose stays on the record: %v", err)
	}
	if err := miss("succeeded", "无变化", "", false); err != nil {
		t.Fatalf("quiet success=%v", err)
	}
}

// A failed run whose only words are 「无变化」 used to settle as a success.
// That cleared a login the person was still holding and left the desktop up.
// The phrase is not a result, so the ending matches an empty body.
func TestAdmittedFillerRunSettlesLikeAnEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
			_, _ = w.Write([]byte(`{"id":"run_filler","status":"failed","desktop_ready":true,"message":{"content":"无变化"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_filler","status":"running"}}`))
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
	if _, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开", "execute", ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var reply Reply
	var settleErr error
	for {
		got, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", "run_filler")
		if done {
			reply, settleErr = got, err
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("filler run did not settle")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if settleErr == nil || !strings.Contains(settleErr.Error(), "without a result") || !strings.Contains(settleErr.Error(), "failed") || strings.Contains(settleErr.Error(), "maclawsrv") {
		t.Fatalf("filler run error=%v", settleErr)
	}
	if reply.Text != "" || reply.SendToUser != "" || strings.Contains(reply.Text, "无变化") {
		t.Fatalf("filler stayed on the reply: %+v", reply)
	}
}
