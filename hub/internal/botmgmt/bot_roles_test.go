package botmgmt

import (
	"sync"
	"testing"
	"time"
)

func TestWatcherReportsToRouterNotPerson(t *testing.T) {
	svc := NewService(&memSettings{})
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, time.Local)
	svc.Now = func() time.Time { return now }
	var mu sync.Mutex
	var told []string
	old := routerTell
	routerTell = func(_ *Service, tenant, user, bot, text, key string) error {
		mu.Lock()
		told = append(told, bot+"|"+key+"|"+text)
		mu.Unlock()
		return nil
	}
	defer func() { routerTell = old }()
	f := watchFinding{TenantID: "t", UserID: "alice", BotID: "bot_b", RunID: "run_1", Reason: "stalled"}
	svc.watcherReport(f)
	svc.watcherReport(f) // router dedupes
	svc.watcherReport(watchFinding{TenantID: "t", UserID: "alice", BotID: "bot_b", RunID: "run_1", Reason: "bogus"})
	mu.Lock()
	defer mu.Unlock()
	if len(told) != 1 || told[0][:6] != "bot_b|" {
		t.Fatalf("router should tell once as the owning bot: %v", told)
	}
}

func TestRouterWordsAttentionFromWorkerDetail(t *testing.T) {
	if got := routerWordFinding(watchFinding{Reason: "attention", Detail: "请扫码登录"}); got != "请扫码登录" {
		t.Fatal(got)
	}
	if routerWordFinding(watchFinding{Reason: "x"}) != "" {
		t.Fatal("unknown reason must stay quiet")
	}
}
