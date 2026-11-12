package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func seedBotThreads(t *testing.T, svc *Service, p Principal, inst string) {
	t.Helper()
	for _, m := range []struct{ key, cmid, content string }{
		{"bot_p#t-a", "a1", "整理上周订单报表"},
		{"bot_p#t-b", "b1", "给供应商写催货邮件"},
		{"bot_p", "m1", "你好"},
		{"bot_q#t-z", "z1", "另一个 bot 的事"},
	} {
		if _, _, _, err := svc.SendMessage(context.Background(), p, inst, SendMessageInput{ClientSessionKey: m.key, ClientMessageID: m.cmid, Content: m.content}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRouteBotMessageByContent(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	seedBotThreads(t, svc, p, inst.ID)
	var prompt string
	route, err := svc.RouteBotMessage(context.Background(), p, inst.ID, BotRouteInput{BotID: "bot_p", Content: "把报表金额改成两位小数"},
		func(_ context.Context, _ string, user string) (string, error) {
			prompt = user
			for i, line := range strings.Split(user, "\n") {
				_ = i
				if strings.Contains(line, "整理上周订单报表") {
					return `{"task":` + line[:1] + `,"confidence":0.9,"reason":"report"}`, nil
				}
			}
			return `{"task":0,"confidence":1}`, nil
		})
	if err != nil || route.Session != "bot_p#t-a" || route.Via != "content" || route.Title != "整理上周订单报表" {
		t.Fatalf("route = %+v %v", route, err)
	}
	if strings.Contains(prompt, "另一个 bot") || strings.Contains(prompt, "你好") {
		t.Fatalf("candidates leaked main or another bot: %s", prompt)
	}
}

func TestRouteBotMessageUnsureAndTimeoutGoToMain(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	seedBotThreads(t, svc, p, inst.ID)
	for _, raw := range []string{`{"task":1,"confidence":0.5}`, `{"task":0,"confidence":0.99}`, `{"task":9,"confidence":0.99}`, `nope`} {
		raw := raw
		route, _ := svc.RouteBotMessage(context.Background(), p, inst.ID, BotRouteInput{BotID: "bot_p", Content: "查天气"},
			func(context.Context, string, string) (string, error) { return raw, nil })
		if route.Session != "bot_p" || route.Via != "main" {
			t.Fatalf("%s -> %+v", raw, route)
		}
	}
	prev := BotRouteBudget
	BotRouteBudget = 80 * time.Millisecond
	defer func() { BotRouteBudget = prev }()
	start := time.Now()
	route, _ := svc.RouteBotMessage(context.Background(), p, inst.ID, BotRouteInput{BotID: "bot_p", Content: "报表"},
		func(ctx context.Context, _, _ string) (string, error) {
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond)
			return `{"task":1,"confidence":1}`, nil
		})
	if route.Via != "main" || route.Reason != "timeout" || time.Since(start) > time.Second {
		t.Fatalf("timeout -> %+v after %v", route, time.Since(start))
	}
}

func TestRouteBotMessageStickyWinsWithTitle(t *testing.T) {
	svc, p, inst := setupCaptureAgentService(t, &captureExecutor{})
	defer svc.Close()
	seedBotThreads(t, svc, p, inst.ID)
	route, err := svc.RouteBotMessage(context.Background(), p, inst.ID, BotRouteInput{BotID: "bot_p", Content: "x", StickySession: "bot_p#t-b"},
		func(context.Context, string, string) (string, error) {
			t.Fatal("classifier called for sticky")
			return "", nil
		})
	if err != nil || route.Session != "bot_p#t-b" || route.Via != "sticky" || route.Title != "给供应商写催货邮件" {
		t.Fatalf("route = %+v %v", route, err)
	}
	if _, err := svc.RouteBotMessage(context.Background(), p, inst.ID, BotRouteInput{BotID: "telegram:1"}, nil); err != ErrBotThreadsBadBot {
		t.Fatalf("bad bot err = %v", err)
	}
}

// The registry is a view over the persisted conversations: a new Service
// on the same store (an srv restart) sees the same tasks, and another user
// sees none of them.
func TestBotThreadsSurviveRestartAndStayWithOwner(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state", "store.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewService(Config{DataRoot: root, TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store, &captureExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, _ := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "T"})
	user, _ := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "U"})
	other, _ := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "O"})
	p := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), p, corelib.AppConfig{MaclawLLMUrl: "https://llm.example/v1", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatal(err)
	}
	inst, err := svc.CreateInstance(context.Background(), p, CreateInstanceInput{Name: "I"})
	if err != nil {
		t.Fatal(err)
	}
	seedBotThreads(t, svc, p, inst.ID)
	svc.Close()

	store2, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc2, err := NewService(Config{DataRoot: root, TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, store2, &captureExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc2.Close()
	items, err := svc2.ListBotThreads(p, inst.ID, "bot_p", BotThreadQuery{ThreadsOnly: true, ExcludeRunning: true})
	if err != nil || len(items) != 2 {
		t.Fatalf("after restart: %+v %v", items, err)
	}
	for _, it := range items {
		if it.Status != "succeeded" || it.Result == "" || it.UserID != user.ID || it.UpdatedAt.IsZero() {
			t.Fatalf("thread row = %+v", it)
		}
	}
	if _, err := svc2.ListBotThreads(Principal{TenantID: tenant.ID, UserID: other.ID}, inst.ID, "bot_p", BotThreadQuery{}); err == nil {
		t.Fatal("another user read this user's bot threads")
	}
	// Bounded: never more than the retention cap.
	if got := svc2.botThreads(p, inst.ID, "bot_p", BotThreadQuery{Limit: 1000, MaxAge: 365 * 24 * time.Hour}, time.Now().Add(8*24*time.Hour)); len(got) != 0 {
		t.Fatalf("rows older than retention returned: %+v", got)
	}
}
