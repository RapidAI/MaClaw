package guiapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotInstancesStayOnOneUser(t *testing.T) {
	user1, first, err := desktopBotIdentity("alice", "bot-1")
	if err != nil {
		t.Fatal(err)
	}
	user2, second, err := desktopBotIdentity("alice", "bot-2")
	if err != nil {
		t.Fatal(err)
	}
	if user1 != "alice" || user2 != "alice" {
		t.Fatalf("users = %q %q", user1, user2)
	}
	if first == second || first != "bot-1" || second != "bot-2" {
		t.Fatalf("instances = %q %q", first, second)
	}
	if desktopBotLocalSessionKey(user1, first) == desktopBotLocalSessionKey(user2, second) {
		t.Fatal("two instances resolved to one local session")
	}
	if _, _, err := desktopBotIdentity("alice", ""); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
	if _, _, err := desktopBotIdentity("alice", "bad id"); err == nil {
		t.Fatal("bot id with a space should be rejected")
	}
}

func TestUnreachableBotStaysAFailure(t *testing.T) {
	if _, ok := (&App{}).desktopTimeoutHandoff("bot_1", fmt.Errorf("MaClawSrv 不可达")); ok {
		t.Fatal("an unreachable instance was turned into a login handoff")
	}
	timeout := fmt.Errorf("MaClawSrv 没有在时限内返回结果")
	if !desktopTimeoutHandsOff(timeout, "https://hub.example/vnc", true) {
		t.Fatal("a login handoff that outlived the reply was not shown")
	}
	if desktopTimeoutHandsOff(timeout, "https://hub.example/vnc", false) {
		t.Fatal("a slow task was given the keyboard")
	}
	if desktopTimeoutHandsOff(fmt.Errorf("MaClawSrv 不可达"), "https://hub.example/vnc", true) {
		t.Fatal("an unreachable instance was turned into a login handoff")
	}
	if !keepLoginDesktopAfterFailure(fmt.Errorf("instance returned no result"), "https://hub.example/vnc", true) {
		t.Fatal("a failed continuation hid the logged-in desktop")
	}
	if keepLoginDesktopAfterFailure(fmt.Errorf("MaClawSrv 不可达"), "https://hub.example/vnc", true) {
		t.Fatal("an unreachable instance kept a login desktop")
	}
	if keepLoginDesktopAfterFailure(fmt.Errorf("instance returned no result"), "https://hub.example/vnc", false) {
		t.Fatal("a normal failure was shown as a login desktop")
	}
	if !desktopCommandTimedOut(fmt.Errorf("MaClawSrv 没有在时限内返回结果")) {
		t.Fatal("timeout was not recognized")
	}
	if desktopCommandTimedOut(fmt.Errorf("MaClawSrv 不可达")) {
		t.Fatal("unreachable was treated as a timeout")
	}
}

func TestSendDesktopBotTaskReachesTheInstance(t *testing.T) {
	got := make(chan string, 1)
	desktopBotRelayOverride = func(_ context.Context, botID, text, phase string) (desktopBotRelayResult, error) {
		got <- botID + ":" + text + ":" + phase
		return desktopBotRelayResult{Text: "done"}, nil
	}
	t.Cleanup(func() { desktopBotRelayOverride = nil })
	resp, err := (&App{}).SendDesktopBotTask("bot_1", "open site", "plan")
	if err != nil || resp == nil || !resp.Deferred || resp.RequestID == "" {
		t.Fatalf("resp=%#v err=%v", resp, err)
	}
	select {
	case body := <-got:
		if body != "bot_1:open site:plan" {
			t.Fatalf("relayed %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message was not sent to the instance")
	}
}

func TestDesktopURLAloneDoesNotAnnounceAView(t *testing.T) {
	var views []string
	desktopBotViewSink = func(payload string) { views = append(views, payload) }
	t.Cleanup(func() { desktopBotViewSink = nil })
	app := &App{}
	const requestID = "desktop-bot-1"
	const sessionKey = "alice:bot_1"
	urlOnly := desktopViewSnap{url: "https://hub.example/vnc"}

	prev := app.announceDesktopView(requestID, sessionKey, desktopViewSnap{}, urlOnly)
	prev = app.announceDesktopView(requestID, sessionKey, prev, desktopViewSnap{url: "https://hub.example/other"})
	if len(views) != 0 {
		t.Fatalf("a desktop address alone was announced: %v", views)
	}
	if prev.url != "https://hub.example/other" {
		t.Fatalf("quiet poll did not keep the latest address: %#v", prev)
	}

	prev = app.announceDesktopView(requestID, sessionKey, prev, desktopViewSnap{url: "https://hub.example/other", control: true})
	if len(views) != 1 || !strings.Contains(views[0], `"user_control":true`) || !strings.Contains(views[0], "https://hub.example/other") {
		t.Fatalf("rising keyboard handoff was not announced: %v", views)
	}

	prev = app.announceDesktopView(requestID, sessionKey, prev, desktopViewSnap{url: "https://hub.example/other", control: false})
	if len(views) != 2 || !strings.Contains(views[1], `"user_control":false`) {
		t.Fatalf("falling keyboard handoff was not announced: %v", views)
	}

	views = nil
	app.announceDesktopView(requestID, sessionKey, desktopViewSnap{}, desktopViewSnap{url: "https://hub.example/pay", reason: "payment_confirm"})
	if len(views) != 1 || !strings.Contains(views[0], `"attention_reason":"payment_confirm"`) || strings.Contains(views[0], `"user_control":true`) {
		t.Fatalf("attention reason was not announced on its own: %v", views)
	}
}

func TestTimeoutAttachesTheDesktopOnlyWhenThePersonHasIt(t *testing.T) {
	app := &App{}
	var got *IMAgentResponse
	var views []string
	desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
	desktopBotViewSink = func(payload string) { views = append(views, payload) }
	t.Cleanup(func() {
		desktopBotRelayOverride = nil
		desktopBotWatchOverride = nil
		desktopBotResultSink = nil
		desktopBotViewSink = nil
	})
	timeoutErr := fmt.Errorf("MaClawSrv 没有在时限内返回结果")
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		return desktopBotRelayResult{}, timeoutErr
	}

	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		return "https://hub.example/vnc", false, "", nil
	}
	app.finishDesktopBotTask("desktop-bot-timeout", "alice:bot_1", "bot_1", "open site", "execute")
	if got == nil || got.DesktopHandoffURL != "" || got.DesktopUserControl || got.Error == "" {
		t.Fatalf("timeout without the keyboard attached a desktop: %#v", got)
	}
	if len(views) != 1 || !strings.Contains(views[0], `"cleared":true`) || strings.Contains(views[0], "hub.example") {
		t.Fatalf("timeout without the keyboard did not clear the view: %v", views)
	}

	got = nil
	views = nil
	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		return "https://hub.example/vnc", true, "login_wall", nil
	}
	app.finishDesktopBotTask("desktop-bot-timeout-hand", "alice:bot_1", "bot_1", "open site", "execute")
	if got == nil || got.DesktopHandoffURL != "https://hub.example/vnc" || !got.DesktopUserControl || got.Error != "" {
		t.Fatalf("timeout with the keyboard was not a handoff: %#v", got)
	}
	const loginQuestion = "这一步需要你在当前桌面的浏览器里完成登录或验证。登录状态会留在这个浏览器里，完成后这个 bot 会接着操作。"
	if got.Text != loginQuestion {
		t.Fatalf("timeout handoff text = %q", got.Text)
	}
	for _, view := range views {
		if strings.Contains(view, `"cleared":true`) {
			t.Fatalf("timeout handoff cleared the desktop: %v", views)
		}
	}

	got = nil
	views = nil
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		return desktopBotRelayResult{}, fmt.Errorf("MaClawSrv 不可达")
	}
	desktopBotWatchOverride = func(context.Context, string) (string, bool, string, error) {
		return "https://hub.example/vnc", true, "login_wall", nil
	}
	app.finishDesktopBotTask("desktop-bot-down", "alice:bot_1", "bot_1", "open site", "execute")
	if got == nil || got.DesktopHandoffURL != "" || got.DesktopUserControl || !strings.Contains(got.Error, "不可达") {
		t.Fatalf("unreachable instance became a handoff: %#v", got)
	}
	if len(views) != 1 || !strings.Contains(views[0], `"cleared":true`) {
		t.Fatalf("unreachable send left a view up: %v", views)
	}
}

func TestDesktopAddressOnAFinishedReplyIsNotAHandoff(t *testing.T) {
	app := &App{}
	var got *IMAgentResponse
	var views []string
	desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
	desktopBotViewSink = func(payload string) { views = append(views, payload) }
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		return desktopBotRelayResult{Text: "首页标题是欢迎。", NovncURL: "https://hub.example/vnc"}, nil
	}
	t.Cleanup(func() {
		desktopBotRelayOverride = nil
		desktopBotResultSink = nil
		desktopBotViewSink = nil
	})
	app.finishDesktopBotTask("desktop-bot-done", "alice:bot_1", "bot_1", "看看首页", "plan")
	if got == nil || got.Text != "首页标题是欢迎。" || got.DesktopHandoffURL != "" || got.DesktopUserControl || got.DesktopAttentionReason != "" {
		t.Fatalf("finished reply announced a desktop address: %#v", got)
	}
	if len(views) != 0 {
		t.Fatalf("finished reply emitted a view: %v", views)
	}
}

func TestWatchDesktopBotRequiresABotID(t *testing.T) {
	if _, err := (&App{}).WatchDesktopBot(" "); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
}
