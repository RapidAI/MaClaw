package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type outcomeExecutor struct{ res ExecuteResult }

func (e *outcomeExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	r := e.res
	r.Metadata = cloneMap(e.res.Metadata)
	return &r, nil
}

// The host's measured task outcome reaches the persisted run, the reloaded
// run, the assistant message and the terminal events; a not-done task is
// waiting_for_user with the run status unchanged; a happy final reply
// cannot hide it; unknown values are never passed on.
func TestTaskOutcomeIsTruthfulEndToEnd(t *testing.T) {
	cases := []struct {
		name, reply, source string
		hook                map[string]string
		outcome             string
		waiting             bool
		prefix              string
	}{
		{name: "ask_user without the goal met", reply: "请你确认一下", source: "ask_user", hook: map[string]string{TaskOutcomeKey: TaskOutcomeAwaitingUser}, outcome: TaskOutcomeAwaitingUser, waiting: true},
		{name: "blocked with a happy reply", reply: "已经全部发好了！", hook: map[string]string{TaskOutcomeKey: TaskOutcomeBlocked, TaskOutcomeNoticeKey: "这件没做成，我已经停下。"}, outcome: TaskOutcomeBlocked, waiting: true, prefix: "这件没做成，我已经停下。\n已经全部发好了！"},
		{name: "uncertain submission", reply: "发好了", hook: map[string]string{TaskOutcomeKey: TaskOutcomeUnconfirmed, TaskOutcomeNoticeKey: "提交后没能确认结果。"}, outcome: TaskOutcomeUnconfirmed, waiting: true, prefix: "提交后没能确认结果。\n发好了"},
		{name: "partial", reply: "好了", hook: map[string]string{TaskOutcomeKey: TaskOutcomePartial}, outcome: TaskOutcomePartial, waiting: true},
		{name: "not submitted", reply: "好了", hook: map[string]string{TaskOutcomeKey: TaskOutcomeNotSubmitted}, outcome: TaskOutcomeNotSubmitted, waiting: true},
		{name: "verified success", reply: "已发布：https://example.org/1", hook: map[string]string{TaskOutcomeKey: TaskOutcomeDone}, outcome: TaskOutcomeDone},
		{name: "background job still running", reply: "已经启动训练。", hook: map[string]string{TaskOutcomeKey: TaskOutcomeRunning, TaskOutcomeNoticeKey: "后台任务还在跑，我会定时查看进度并告诉你。"}, outcome: TaskOutcomeRunning, prefix: "后台任务还在跑，我会定时查看进度并告诉你。\n已经启动训练。"},
		{name: "question-only chat", reply: "你想发到哪个网站？", source: "ask_user", waiting: true},
		{name: "plain answer", reply: "今天是周二。"},
		{name: "unknown value dropped", reply: "ok", hook: map[string]string{TaskOutcomeKey: "kinda_done", TaskOutcomeNoticeKey: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ex := &outcomeExecutor{res: ExecuteResult{Content: tc.reply, OutputType: "text/plain"}}
			if tc.source != "" {
				ex.res.Metadata = map[string]string{metaResponseSource: tc.source}
			}
			store := NewMemoryStore()
			svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, ex)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = svc.Close() }()
			svc.AssistantMessageMetadataHook = func(ctx context.Context, p Principal, inst Instance, sess Session, run Run, msg Message, cfg corelib.AppConfig) map[string]string {
				return tc.hook
			}
			ctx := context.Background()
			tenant, _ := svc.CreateTenant(ctx, CreateTenantInput{Name: "T"})
			user, _ := svc.CreateUser(ctx, CreateUserInput{TenantID: tenant.ID, Name: "U"})
			p := Principal{TenantID: tenant.ID, UserID: user.ID}
			if _, err := svc.UpdateUserConfig(ctx, p, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1:9", MaclawLLMKey: "k", MaclawLLMModel: "m"}); err != nil {
				t.Fatal(err)
			}
			inst, err := svc.CreateInstance(ctx, p, CreateInstanceInput{Name: "I"})
			if err != nil {
				t.Fatal(err)
			}
			sess, _ := svc.CreateSession(ctx, p, inst.ID, CreateSessionInput{Title: "S"})
			run, msg, err := svc.PostMessage(ctx, p, inst.ID, sess.ID, PostMessageInput{Content: "把文章发到 CSDN"})
			if err != nil {
				t.Fatal(err)
			}
			check := func(where string, r *Run) {
				t.Helper()
				if r.Status != RunStatusSucceeded {
					t.Fatalf("%s: status %s (must stay compatible)", where, r.Status)
				}
				if r.Metadata[TaskOutcomeKey] != tc.outcome || r.WaitingForUser != tc.waiting {
					t.Fatalf("%s: outcome=%q waiting=%v", where, r.Metadata[TaskOutcomeKey], r.WaitingForUser)
				}
			}
			check("returned run", run)
			if msg.Metadata[TaskOutcomeKey] != tc.outcome || msg.Metadata[TaskOutcomeNoticeKey] != "" {
				t.Fatalf("message metadata %v", msg.Metadata)
			}
			if tc.prefix != "" && msg.Content != tc.prefix {
				t.Fatalf("content %q", msg.Content)
			}
			if tc.prefix == "" && msg.Content != tc.reply {
				t.Fatalf("content changed: %q", msg.Content)
			}
			// Reloaded from the store by a fresh service (restart).
			svc2, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, ex)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = svc2.Close() }()
			got, err := svc2.GetRun(ctx, p, inst.ID, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			check("reloaded run", got)
			events, err := svc2.ListRunEvents(ctx, p, run.ID, 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			var types []string
			for _, ev := range events {
				types = append(types, ev.Type)
				if ev.Type == "run.completed" || ev.Type == "assistant.message" || ev.Type == "ask_user" {
					if got, _ := ev.Payload[TaskOutcomeKey].(string); got != tc.outcome {
						t.Fatalf("%s payload outcome %q", ev.Type, got)
					}
				}
			}
			hasAsk := strings.Contains(strings.Join(types, ","), "ask_user")
			if hasAsk != tc.waiting {
				t.Fatalf("events %v (waiting=%v)", types, tc.waiting)
			}
		})
	}
}
