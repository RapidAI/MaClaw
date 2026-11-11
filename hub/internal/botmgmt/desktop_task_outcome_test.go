package botmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A finished MaClawSrv run keeps status "succeeded"; its measured
// task_outcome decides what the person and other bots are told. Live
// settle, the GUI delivery, the status question and a Hub restart (pin
// reload) all carry it; an older MaClawSrv without the field keeps the old
// behaviour; an unknown value is ignored; a cancelled run stays cancelled.
func TestDesktopRunTaskOutcomeReachesEveryReader(t *testing.T) {
	cases := []struct {
		name, status, content string
		msgMeta, runMeta      map[string]string
		want                  string
		notDone               bool
	}{
		{name: "done", status: "succeeded", content: "已发布：https://example.org/1", msgMeta: map[string]string{"task_outcome": "done"}, want: "done"},
		{name: "partial", status: "succeeded", content: "这件还没完全做成。\n发好了", msgMeta: map[string]string{"task_outcome": "partial"}, want: "partial", notDone: true},
		{name: "blocked happy reply", status: "succeeded", content: "已经全部发好了！", msgMeta: map[string]string{"task_outcome": "blocked", "send_to_user": "已经全部发好了！"}, want: "blocked", notDone: true},
		{name: "not_submitted", status: "succeeded", content: "好了", msgMeta: map[string]string{"task_outcome": "not_submitted"}, want: "not_submitted", notDone: true},
		{name: "unconfirmed from run metadata after a restart", status: "succeeded", content: "发好了", runMeta: map[string]string{"task_outcome": "unconfirmed"}, want: "unconfirmed", notDone: true},
		{name: "awaiting_user", status: "succeeded", content: "要发到哪个账号？", msgMeta: map[string]string{"task_outcome": "awaiting_user", "response_source": "ask_user", "ask_user_question": "要发到哪个账号？"}, want: "awaiting_user", notDone: true},
		{name: "running", status: "succeeded", content: "后台任务还在跑，我会定时查看进度并告诉你。\n已经启动训练。", msgMeta: map[string]string{"task_outcome": "running"}, want: "running", notDone: true},
		{name: "ordinary question", status: "succeeded", content: "你想发到哪个网站？", msgMeta: map[string]string{"response_source": "ask_user", "ask_user_question": "你想发到哪个网站？"}},
		{name: "older srv", status: "succeeded", content: "完成了"},
		{name: "unknown value", status: "succeeded", content: "完成了", msgMeta: map[string]string{"task_outcome": "kinda"}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runID := fmt.Sprintf("run_outcome_%d", i)
			run := map[string]any{"id": runID, "status": tc.status, "desktop_ready": true, "metadata": tc.runMeta, "message": map[string]any{"content": tc.content, "metadata": tc.msgMeta}}
			if tc.runMeta != nil {
				// a reply rebuilt after a srv restart: message without outcome
				run["message"] = map[string]any{"content": tc.content}
			}
			runJSON, _ := json.Marshal(run)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
					_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
				case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
					_, _ = w.Write(runJSON)
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
					writeInstanceSettings(w, r)
				case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
					w.WriteHeader(http.StatusOK)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
					w.WriteHeader(http.StatusAccepted)
					_, _ = fmt.Fprintf(w, `{"async":true,"run":{"id":%q,"status":"running"}}`, runID)
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()
			settings := &memSettings{}
			raw, _ := json.Marshal(record{
				BaseURL: srv.URL, AccessToken: "t", AdminSecret: "a",
				Principals: []ownerPrincipal{{HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "k", APISecret: "s"}},
				Bots:       []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
				Grants:     []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
			})
			if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
				t.Fatal(err)
			}
			newSvc := func() *Service {
				svc := NewService(settings)
				svc.HTTP = srv.Client()
				svc.admitPollInterval = 10 * time.Millisecond
				svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
				return svc
			}
			svc := newSvc()
			ctx := context.Background()
			if _, err := svc.AdmitDesktopMessage(ctx, "tenant-a", "alice", "bot_alice", "把文章发到 CSDN", "execute", "cm-"+runID); err != nil {
				t.Fatal(err)
			}
			var reply Reply
			deadline := time.Now().Add(3 * time.Second)
			for {
				r, done, err := svc.DesktopRunResult(ctx, "tenant-a", "alice", "bot_alice", runID)
				if done {
					if err != nil {
						t.Fatal(err)
					}
					reply = r
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("run did not settle")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if reply.TaskOutcome != tc.want {
				t.Fatalf("live reply outcome %q, want %q", reply.TaskOutcome, tc.want)
			}
			if tc.msgMeta["send_to_user"] != "" {
				line := desktopOutcomeStatusLine(tc.want, false, false)
				if !strings.HasPrefix(reply.SendToUser, line+"\n") || !strings.Contains(reply.SendToUser, tc.msgMeta["send_to_user"]) {
					t.Fatalf("spoken sentence hides the outcome: %q", reply.SendToUser)
				}
			}
			check := func(where string, s *Service) {
				t.Helper()
				text, items, err := s.DesktopTaskQuery(ctx, "tenant-a", "alice", "bot_alice", "", "zh")
				if err != nil || len(items) != 1 {
					t.Fatalf("%s: %v %v", where, items, err)
				}
				if items[0].TaskOutcome != tc.want {
					t.Fatalf("%s: status outcome %q", where, items[0].TaskOutcome)
				}
				saysDone := strings.Contains(text, "已经做完")
				if tc.notDone == saysDone {
					t.Fatalf("%s: status question answered %q (not done=%v)", where, text, tc.notDone)
				}
				dels, err := s.PendingDesktopDeliveries(ctx, "tenant-a", "alice")
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range dels {
					if d.RunID == runID && d.Reply.TaskOutcome != tc.want {
						t.Fatalf("%s: delivery outcome %q", where, d.Reply.TaskOutcome)
					}
				}
			}
			check("live", svc)
			check("after a Hub restart", newSvc())
		})
	}
}

func TestDelegatedRunEndHonoursTaskOutcome(t *testing.T) {
	for _, tc := range []struct {
		outcome, topic string
	}{{"done", "done"}, {"blocked", "blocked"}, {"unconfirmed", "blocked"}, {"partial", "blocked"}, {"not_submitted", "blocked"}, {"awaiting_user", "blocked"}, {"", "done"}, {"kinda", "done"}} {
		svc := primaryFixture(t, []Bot{
			{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
			{ID: "bot_pub", Name: "发布员", OwnerUserID: "alice", InstanceID: "inst_pub", CreatedAt: "2026-02-01T00:00:00Z"},
		})
		ctx := context.Background()
		d, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", "bot_main", BotCrossNote{Kind: "delegate", Target: "bot_pub", Text: "发文章", Order: "把文章发到 CSDN"})
		if err != nil {
			t.Fatal(err)
		}
		if _, claimed, err := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_pub", d.ID); err != nil || !claimed {
			t.Fatalf("claim %v %v", claimed, err)
		}
		if _, err := svc.NoteBotCrossRun(ctx, "tenant-a", "alice", "bot_pub", d.ID, "run_7"); err != nil {
			t.Fatal(err)
		}
		svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_7", "succeeded", Reply{Text: "已经全部发好了", TaskOutcome: desktopTaskOutcome(tc.outcome)}, nil)
		items, err := svc.BotMailbox(ctx, "tenant-a", "alice", "main", nil, false)
		if err != nil || len(items) != 1 || items[0].Topic != tc.topic {
			t.Fatalf("outcome %q: %#v %v", tc.outcome, items, err)
		}
	}
}

func TestDelegatedRunEndLeavesRunningJobOpen(t *testing.T) {
	svc := primaryFixture(t, []Bot{
		{ID: "bot_main", Name: "主", OwnerUserID: "alice", InstanceID: "inst_main", CreatedAt: "2026-01-01T00:00:00Z"},
		{ID: "bot_pub", Name: "发布员", OwnerUserID: "alice", InstanceID: "inst_pub", CreatedAt: "2026-02-01T00:00:00Z"},
	})
	ctx := context.Background()
	d, err := svc.PostBotCrossTalk(ctx, "tenant-a", "alice", "bot_main", BotCrossNote{Kind: "delegate", Target: "bot_pub", Text: "训练", Order: "训练分类器"})
	if err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := svc.ClaimBotCrossStart(ctx, "tenant-a", "alice", "bot_pub", d.ID); err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if _, err := svc.NoteBotCrossRun(ctx, "tenant-a", "alice", "bot_pub", d.ID, "run_7"); err != nil {
		t.Fatal(err)
	}
	svc.reportDelegateRunEnd("tenant-a", "alice", "bot_pub", "run_7", "succeeded", Reply{Text: "已经启动训练。", TaskOutcome: desktopTaskOutcome("running")}, nil)
	items, err := svc.BotMailbox(ctx, "tenant-a", "alice", "main", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Topic == "done" || it.Topic == "blocked" {
			t.Fatalf("running handoff reported as %s: %+v", it.Topic, it)
		}
	}
}
