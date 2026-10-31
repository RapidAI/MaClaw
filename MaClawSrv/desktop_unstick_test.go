package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopStepTimeoutUnsticksHungTab(t *testing.T) {
	t.Setenv(desktopStepTimeoutEnv, "0.05")
	oldOp, oldUn := operateDesktopFunc, desktopUnstickFunc
	defer func() { operateDesktopFunc, desktopUnstickFunc = oldOp, oldUn }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		time.Sleep(time.Second)
		return "", nil
	}
	var asked string
	desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
		asked = url
		return map[string]any{"unstick": map[string]any{"done": true, "closed": url, "reopened": url}}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u1", "bot_625eda1bbe86a157")}
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "navigate", "url": "https://zhuanlan.zhihu.com/write"})
	if err == nil || !strings.Contains(err.Error(), "closed and opened again") || asked != "https://zhuanlan.zhihu.com/write" {
		t.Fatalf("err=%v asked=%q", err, asked)
	}
	asked = "none"
	_, err = operateDesktopWithTimeout(context.Background(), agentruntime.Scope{TenantID: "t", UserID: "u1"}, map[string]any{"action": "screenshot"})
	if asked != "none" || !strings.Contains(err.Error(), "take a screenshot") {
		t.Fatalf("shared desktop must not unstick: err=%v asked=%q", err, asked)
	}
}

func TestDesktopStepTimeoutDropsStuckBinding(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u2", "bot_625eda1bbe86a157")}
	key, err := desktopUserKey(scope)
	if err != nil {
		t.Fatal(err)
	}
	b := desktopBindingFor(key)
	b.mu.Lock()
	desktopDropStuckBinding(scope)
	if desktopBindingFor(key) == b {
		t.Fatal("stuck binding kept")
	}
	b.mu.Unlock()
	free := desktopBindingFor(key)
	desktopDropStuckBinding(scope)
	if desktopBindingFor(key) != free {
		t.Fatal("free binding dropped")
	}
}

// A step that returns on its own with the page silent (cdp timeout) also
// reopens the tab, at most once a minute.
func TestDesktopCdpTimeoutUnsticksOncePerMinute(t *testing.T) {
	oldOp, oldUn := operateDesktopFunc, desktopUnstickFunc
	defer func() { operateDesktopFunc, desktopUnstickFunc = oldOp, oldUn }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		return "", errString("publish step observe failed: page did not answer: cdp timeout: Runtime.evaluate (id=39)")
	}
	calls := 0
	desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
		calls++
		return map[string]any{"unstick": map[string]any{"done": true, "closed": "https://a.test/x", "reopened": "https://a.test/x"}}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u3", "bot_625eda1bbe86a157")}
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "publish"})
	if err == nil || !strings.Contains(err.Error(), "closed and opened again") || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	_, _ = operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "probe"})
	if calls != 1 {
		t.Fatalf("unstick not rate limited: %d", calls)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// A slow step on a page the bot filled must not close the tab: 10-31 the
// Xiaohongshu editor with title, body and images reopened empty.
func TestDesktopStepTimeoutKeepsFilledPage(t *testing.T) {
	t.Setenv(desktopStepTimeoutEnv, "0.05")
	oldOp, oldUn := operateDesktopFunc, desktopUnstickFunc
	defer func() { operateDesktopFunc, desktopUnstickFunc = oldOp, oldUn }()
	slow := false
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if slow {
			time.Sleep(time.Second)
		}
		return "ok", nil
	}
	asked := 0
	desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
		asked++
		return map[string]any{"unstick": map[string]any{"done": true, "closed": "x", "reopened": "x"}}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u9", "bot_625eda1bbe86a157"), RunID: "run_form"}
	if _, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "rich_fill"}); err != nil {
		t.Fatal(err)
	}
	slow = true
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "screenshot"})
	if asked != 0 || err == nil || !strings.Contains(err.Error(), "NOT closed or reloaded") {
		t.Fatalf("filled page was reopened: asked=%d err=%v", asked, err)
	}
	slow = false
	operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "navigate", "url": "https://a.test/"})
	slow = true
	if _, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "screenshot"}); asked != 1 {
		t.Fatalf("after navigate a hung tab is reopened again: asked=%d err=%v", asked, err)
	}
}

// 10-31: "#话题" typed into the Xiaohongshu body left the topic dropdown
// over 发布. A fill with # or @ tells the bot to close that menu first.
func TestDesktopFillWithHashtagWarnsAboutTheMenu(t *testing.T) {
	oldOp := operateDesktopFunc
	defer func() { operateDesktopFunc = oldOp }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		return "has=yes", nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: "u1"}
	out, _ := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "rich_fill", "text": "秋天喝茶 #乌龙茶 #日常"})
	if !strings.Contains(out, "Escape") {
		t.Fatalf("no menu note: %q", out)
	}
	out, _ = operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "batch", "plan": []any{map[string]any{"action": "rich_fill", "text": "hi @小明"}}})
	if !strings.Contains(out, "Escape") {
		t.Fatalf("batch: %q", out)
	}
	for _, text := range []string{"第1步 C# 语言", "mail a@b.com", "普通正文"} {
		out, _ = operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "rich_fill", "text": text})
		if strings.Contains(out, "Escape") {
			t.Fatalf("%q: false note", text)
		}
	}
}

// A slow step on a live tab is load: nothing is reopened, the bot waits.
func TestDesktopStepTimeoutWaitsOnALiveSlowPage(t *testing.T) {
	t.Setenv(desktopStepTimeoutEnv, "0.05")
	oldOp, oldUn := operateDesktopFunc, desktopUnstickFunc
	defer func() { operateDesktopFunc, desktopUnstickFunc = oldOp, oldUn }()
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		time.Sleep(time.Second)
		return "", nil
	}
	desktopUnstickFunc = func(ctx context.Context, scope agentruntime.Scope, url string) (map[string]any, error) {
		return map[string]any{"unstick": map[string]any{"done": false, "why": "alive_slow"}}, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: desktop.BotDesktopUser("u1", "bot_live_slow")}
	_, err := operateDesktopWithTimeout(context.Background(), scope, map[string]any{"action": "click", "ref": "e3"})
	if err == nil || !strings.Contains(err.Error(), "alive but the desktop is slow") || strings.Contains(err.Error(), "opened again") {
		t.Fatalf("err=%v", err)
	}
}

// The takeover reason shown to the person no longer asks them to press 交还.
func TestDesktopTakeoverReasonDropsHandBackAsk(t *testing.T) {
	for in, want := range map[string]string{
		"标题、正文、4 张图已填好，请点页面右下角红色「发布」按钮，完成后按交还": "标题、正文、4 张图已填好，请点页面右下角红色「发布」按钮",
		"请扫码登录小红书，登录后点交还。":                     "请扫码登录小红书",
	} {
		if got := strings.TrimSpace(desktopHandBackAskRe.ReplaceAllString(in, "")); got != want {
			t.Errorf("%q -> %q", in, got)
		}
	}
}
