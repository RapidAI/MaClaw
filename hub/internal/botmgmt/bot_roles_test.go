package botmgmt

import (
	"strings"
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
	if got := routerWordFinding(watchFinding{Reason: "attention", Detail: "请扫码登录"}, false); got != "请扫码登录" {
		t.Fatal(got)
	}
	if routerWordFinding(watchFinding{Reason: "x"}, false) != "" {
		t.Fatal("unknown reason must stay quiet")
	}
}

// A watcher fact about a computerless bot never points at a screen.
func TestRouterWordsOffTheDesktopForNoDesktopBots(t *testing.T) {
	cases := []struct {
		reason  string
		detail  string
		want    string
		wantAlt string
	}{
		{"no_receipt", "", "这件做完了。", ""},
		{"attention", "", "这件需要你看一眼。", ""},
		{"attention", "请扫码登录", "请扫码登录", ""},
		{"failed", "", "这件我没做成，中途出了问题。你说一声我再试一次。", ""},
	}
	for _, tc := range cases {
		got := routerWordFinding(watchFinding{Reason: tc.reason, Detail: tc.detail}, true)
		if got != tc.want && got != tc.wantAlt {
			t.Fatalf("%s: %q", tc.reason, got)
		}
		if strings.Contains(got, "桌面") {
			t.Fatalf("%s names a desktop: %q", tc.reason, got)
		}
	}
	if got := routerWordFinding(watchFinding{Reason: "no_receipt"}, false); !strings.Contains(got, "桌面") {
		t.Fatalf("desktop bot lost its wording: %q", got)
	}
}
