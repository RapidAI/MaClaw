package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	if len(views) != 1 || !strings.Contains(views[0], `"user_control":false`) || strings.Contains(views[0], `"user_control":true`) {
		t.Fatalf("first quiet poll did not release a latched handoff: %v", views)
	}
	views = nil
	prev = app.announceDesktopView(requestID, sessionKey, prev, desktopViewSnap{url: "https://hub.example/other"})
	if len(views) != 0 {
		t.Fatalf("a later desktop address alone was announced: %v", views)
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

func TestFinishedReplyCarriesTheDesktopScreenshot(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	app := &App{}
	var got *IMAgentResponse
	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		return desktopBotRelayResult{
			Text: "桌面截图已生成",
			Images: []DesktopBotImage{
				{MIME: "text/html", Data: payload},
				{MIME: "image/png", Data: payload},
			},
		}, nil
	}
	t.Cleanup(func() {
		corelib.SetMaclawBaseDir(prev)
		desktopBotRelayOverride = nil
		desktopBotResultSink = nil
	})
	app.finishDesktopBotTask("desktop-bot-shot", "alice:bot_1", "bot_1", "看一下环境", "plan")
	if got == nil || got.Text != "桌面截图已生成" || len(got.DesktopImages) != 0 || len(got.DesktopShotPaths) != 1 {
		t.Fatalf("reply=%#v", got)
	}
	path := got.DesktopShotPaths[0]
	wantDir := filepath.Join(corelib.MaclawDataDir(), "bot-files", "bot_1")
	if !filepath.IsAbs(path) || filepath.Clean(filepath.Dir(path)) != filepath.Clean(wantDir) || filepath.Base(path) != "screenshot.png" {
		t.Fatalf("path=%s dir=%s", path, wantDir)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "png-bytes" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	shot, err := app.ReadDesktopBotShot(path)
	if err != nil || shot == nil || shot.MIME != "image/png" || shot.Data != payload {
		t.Fatalf("shot=%#v err=%v", shot, err)
	}
	outside := filepath.Join(base, "secret.png")
	if err := os.WriteFile(outside, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ReadDesktopBotShot(outside); err == nil {
		t.Fatal("a screenshot outside bot-files was returned")
	}
	raw, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(raw), `"desktop_shot_paths"`) || strings.Contains(string(raw), payload) {
		t.Fatalf("event=%s err=%v", raw, err)
	}
}

func TestFinishedReplyCarriesTheDocument(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	app := &App{}
	var got *IMAgentResponse
	payload := base64.StdEncoding.EncodeToString([]byte("word-bytes"))
	desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		return desktopBotRelayResult{
			Text: "文件已放在这条回复里",
			Files: []DesktopBotFile{
				{Name: "../secret.docx", MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: payload},
				{Name: "自我描述.docx", MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Data: payload},
			},
		}, nil
	}
	t.Cleanup(func() {
		corelib.SetMaclawBaseDir(prev)
		desktopBotRelayOverride = nil
		desktopBotResultSink = nil
	})
	app.finishDesktopBotTask("desktop-bot-file", "alice:bot_1", "bot_1", "生成文档", "execute")
	if got == nil || got.Text != "文件已放在这条回复里" || len(got.DesktopFiles) != 0 || len(got.LocalFilePaths) != 1 {
		t.Fatalf("reply=%#v", got)
	}
	path := got.LocalFilePaths[0]
	wantDir := filepath.Join(corelib.MaclawDataDir(), "bot-files", "bot_1")
	if !filepath.IsAbs(path) || filepath.Clean(filepath.Dir(path)) != filepath.Clean(wantDir) || filepath.Base(path) != "自我描述.docx" {
		t.Fatalf("path=%s dir=%s", path, wantDir)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "word-bytes" {
		t.Fatalf("body=%q err=%v", body, err)
	}
	again, left := storeDesktopBotFiles("bot_1", []DesktopBotFile{{
		Name: "自我描述.docx",
		MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Data: payload,
	}})
	if len(left) != 0 || len(again) != 1 || filepath.Base(again[0]) == "自我描述.docx" || filepath.Ext(again[0]) != ".docx" {
		t.Fatalf("collision=%v left=%v", again, left)
	}
	raw, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(raw), `"local_file_paths"`) || strings.Contains(string(raw), payload) {
		t.Fatalf("event=%s err=%v", raw, err)
	}
}

func TestDocumentStaysDownloadableWhenItCannotBeSaved(t *testing.T) {
	base := t.TempDir()
	prev := corelib.MaclawBaseDir()
	corelib.SetMaclawBaseDir(base)
	t.Cleanup(func() { corelib.SetMaclawBaseDir(prev) })
	if err := os.MkdirAll(corelib.MaclawDataDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corelib.MaclawDataDir(), "bot-files"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString([]byte("word-bytes"))
	file := DesktopBotFile{
		Name: "自我描述.docx",
		MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Data: payload,
	}
	paths, left := storeDesktopBotFiles("bot_1", []DesktopBotFile{file})
	if len(paths) != 0 || len(left) != 1 || left[0].Data != payload {
		t.Fatalf("paths=%v left=%#v", paths, left)
	}
	escaped, kept := storeDesktopBotFiles("../bot", []DesktopBotFile{file})
	if escaped != nil || len(kept) != 1 {
		t.Fatalf("escaped=%v kept=%v", escaped, kept)
	}
}

func TestDesktopBotMessageResponseHasRoomForAScreenshot(t *testing.T) {
	if desktopBotResponseLimit("/api/v1/bots/bot_1/messages") < 1<<20+1_200_000 {
		t.Fatal("message response cannot carry a screenshot")
	}
	if desktopBotResponseLimit("/api/v1/bots/bot_1/runs/run_1") < 1<<20+1_200_000 {
		t.Fatal("admitted run response cannot carry a screenshot")
	}
	if desktopBotResponseLimit("/api/v1/bots/bot_1") != 1<<20 {
		t.Fatal("non-message response limit changed")
	}
	if cleanDesktopBotImages([]DesktopBotImage{{MIME: "image/png", Data: "iVB=Rw0KGgo="}}) != nil {
		t.Fatal("broken padding was kept")
	}
}

func fillDesktopBotDest(t *testing.T, dest any, payload any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatal(err)
	}
}

func TestCancelDesktopBotRun(t *testing.T) {
	t.Cleanup(func() { desktopBotExchangeOverride = nil })

	t.Run("posts the hub cancel route", func(t *testing.T) {
		var method, path string
		desktopBotExchangeOverride = func(_ context.Context, gotMethod, gotPath string, body any, dest any, timeout time.Duration, _ map[string]string) (int, error) {
			method, path = gotMethod, gotPath
			if timeout != 20*time.Second {
				t.Fatalf("timeout = %s", timeout)
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != "{}" {
				t.Fatalf("body = %s", raw)
			}
			if dest != nil {
				t.Fatal("cancel has no response body to decode")
			}
			return http.StatusOK, nil
		}
		if err := (&App{}).CancelDesktopBotRun("bot_1", "run_1"); err != nil {
			t.Fatal(err)
		}
		if method != http.MethodPost || path != "/api/v1/bots/bot_1/runs/run_1/cancel" {
			t.Fatalf("call = %s %s", method, path)
		}
	})

	t.Run("rejects an empty id before the hub call", func(t *testing.T) {
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			t.Fatalf("hub was called: %s %s", method, path)
			return 0, nil
		}
		if err := (&App{}).CancelDesktopBotRun("bot_1", " "); err == nil {
			t.Fatal("empty run id was accepted")
		}
		if err := (&App{}).CancelDesktopBotRun(" ", "run_1"); err == nil {
			t.Fatal("empty bot id was accepted")
		}
	})

	t.Run("returns the hub reason", func(t *testing.T) {
		desktopBotExchangeOverride = func(_ context.Context, _, _ string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			return http.StatusConflict, fmt.Errorf("这台云桌面还有一件在做。")
		}
		err := (&App{}).CancelDesktopBotRun("bot_1", "run_1")
		if err == nil || !strings.Contains(err.Error(), "这台云桌面还有一件在做。") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDesktopBotAdmitCancelStopsTheRunHubNames(t *testing.T) {
	resetActiveDesktopRuns(t)
	resetDesktopAdmitCancel()
	t.Cleanup(func() {
		desktopBotExchangeOverride = nil
		resetDesktopAdmitCancel()
		resetActiveDesktopRuns(t)
	})

	t.Run("cancels when the admit names the run and leaves the next run alone", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		app := &App{}
		app.registerActiveDesktopRun("bot_1", "turn-stop", "alice:bot_1", "打开示例网站", "execute")
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		var paths []string
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			paths = append(paths, method+" "+path)
			if strings.Contains(path, "/cancel") {
				return http.StatusOK, nil
			}
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-late", "status": "running"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "已停", "status": "cancelled"})
			return http.StatusOK, nil
		}
		got, err := app.relayDesktopBot(context.Background(), "bot_1", "打开示例网站", "execute", "turn-stop")
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != "已停" {
			t.Fatalf("text=%q paths=%v", got.Text, paths)
		}
		cancel := http.MethodPost + " /api/v1/bots/bot_1/runs/run-late/cancel"
		found := false
		for _, path := range paths {
			if path == cancel {
				found = true
			}
		}
		if !found {
			t.Fatalf("cancel missing: %v", paths)
		}
		paths = nil
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			paths = append(paths, method+" "+path)
			if strings.Contains(path, "/cancel") {
				t.Fatalf("next run was cancelled: %s", path)
			}
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-next", "status": "running"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "开了", "status": "succeeded"})
			return http.StatusOK, nil
		}
		got, err = app.relayDesktopBot(context.Background(), "bot_1", "下一件", "execute", "turn-next")
		if err != nil || got.Text != "开了" {
			t.Fatalf("text=%q err=%v paths=%v", got.Text, err, paths)
		}
	})

	t.Run("drops the mark when the admit never names a run", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		app := &App{}
		app.registerActiveDesktopRun("bot_1", "turn-fail", "alice:bot_1", "打开示例网站", "execute")
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		desktopBotExchangeOverride = func(_ context.Context, _ string, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			if strings.Contains(path, "/cancel") {
				t.Fatalf("failed admit cancelled: %s", path)
			}
			return http.StatusBadRequest, fmt.Errorf("拒绝")
		}
		if _, err := app.relayDesktopBot(context.Background(), "bot_1", "打开示例网站", "execute", "turn-fail"); err == nil {
			t.Fatal("expected the admit to fail")
		}
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			if strings.Contains(path, "/cancel") {
				t.Fatalf("next run was cancelled: %s", path)
			}
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-after", "status": "running"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "开了", "status": "succeeded"})
			return http.StatusOK, nil
		}
		got, err := app.relayDesktopBot(context.Background(), "bot_1", "下一件", "execute", "turn-after")
		if err != nil || got.Text != "开了" {
			t.Fatalf("text=%q err=%v", got.Text, err)
		}
	})

	t.Run("cancels a run that was named before the stop", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		app := &App{}
		app.registerActiveDesktopRun("bot_1", "req-1", "alice:bot_1", "打开示例网站", "execute")
		app.setActiveDesktopRunID("bot_1", "req-1", "run-late")
		var path string
		desktopBotExchangeOverride = func(_ context.Context, _ string, gotPath string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			path = gotPath
			return http.StatusOK, nil
		}
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		if path != "/api/v1/bots/bot_1/runs/run-late/cancel" {
			t.Fatalf("path=%s", path)
		}
		path = ""
		if err := app.nameAdmittedDesktopRun("bot_1", "req-2", "run-next"); err != nil {
			t.Fatal(err)
		}
		if path != "" {
			t.Fatalf("next run cancelled via %s", path)
		}
	})

	t.Run("keeps the stop when another admit of the same bot ends unnamed", func(t *testing.T) {
		resetDesktopAdmitCancel()
		resetActiveDesktopRuns(t)
		app := &App{}
		started := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		letGo := func() { releaseOnce.Do(func() { close(release) }) }
		t.Cleanup(letGo)
		var startOnce sync.Once
		var mu sync.Mutex
		var paths []string
		record := func(method, path string) {
			mu.Lock()
			paths = append(paths, method+" "+path)
			mu.Unlock()
		}
		snapshot := func() []string {
			mu.Lock()
			defer mu.Unlock()
			out := make([]string, len(paths))
			copy(out, paths)
			return out
		}
		desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			record(method, path)
			if strings.Contains(path, "/cancel") {
				return http.StatusOK, nil
			}
			if method != http.MethodPost {
				if dest == nil {
					return http.StatusOK, nil
				}
				raw, err := json.Marshal(map[string]any{"text": "已停", "status": "cancelled"})
				if err != nil {
					return 0, err
				}
				if err := json.Unmarshal(raw, dest); err != nil {
					return 0, err
				}
				return http.StatusOK, nil
			}
			raw, err := json.Marshal(body)
			if err != nil {
				return 0, err
			}
			var posted struct {
				ClientMessageID string `json:"client_message_id"`
			}
			if err := json.Unmarshal(raw, &posted); err != nil {
				return 0, err
			}
			if posted.ClientMessageID != "turn-a" {
				return http.StatusBadRequest, fmt.Errorf("拒绝")
			}
			startOnce.Do(func() { close(started) })
			<-release
			if dest == nil {
				return 0, fmt.Errorf("admit dest is nil")
			}
			raw, err = json.Marshal(map[string]any{"accepted": true, "run_id": "run-a", "status": "running"})
			if err != nil {
				return 0, err
			}
			if err := json.Unmarshal(raw, dest); err != nil {
				return 0, err
			}
			return http.StatusAccepted, nil
		}
		done := make(chan error, 1)
		go func() {
			got, err := app.relayDesktopBot(context.Background(), "bot_1", "打开示例网站", "execute", "turn-a")
			if err != nil {
				done <- err
				return
			}
			if got.Text != "已停" {
				done <- fmt.Errorf("text=%q", got.Text)
				return
			}
			done <- nil
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("admit did not start")
		}
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		if _, err := app.relayDesktopBot(context.Background(), "bot_1", "别的", "execute", ""); err == nil {
			t.Fatal("expected the other admit to fail")
		}
		for _, path := range snapshot() {
			if strings.Contains(path, "/cancel") {
				t.Fatalf("unnamed admit cancelled: %v", snapshot())
			}
		}
		letGo()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%v paths=%v", err, snapshot())
			}
		case <-time.After(5 * time.Second):
			t.Fatal("admit did not finish")
		}
		cancel := http.MethodPost + " /api/v1/bots/bot_1/runs/run-a/cancel"
		found := 0
		for _, path := range snapshot() {
			if !strings.Contains(path, "/cancel") {
				continue
			}
			found++
			if path != cancel {
				t.Fatalf("cancel path = %s", path)
			}
		}
		if found != 1 {
			t.Fatalf("cancels=%d paths=%v", found, snapshot())
		}
	})

	t.Run("keeps the stop across a busy refusal of the same request", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		prev := desktopBotBusyPoll
		desktopBotBusyPoll = 5 * time.Millisecond
		t.Cleanup(func() { desktopBotBusyPoll = prev })
		app := &App{}
		app.registerActiveDesktopRun("bot_1", "turn-busy", "alice:bot_1", "打开示例网站", "execute")
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		posts := 0
		var paths []string
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			paths = append(paths, method+" "+path)
			if strings.Contains(path, "/cancel") {
				return http.StatusOK, nil
			}
			if method == http.MethodPost && strings.Contains(path, "/messages") {
				posts++
				if posts == 1 {
					return http.StatusConflict, fmt.Errorf("%s", desktopBotBusyMessage)
				}
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-retry", "status": "running"})
				return http.StatusAccepted, nil
			}
			if method == http.MethodGet && strings.Contains(path, "/tasks") {
				fillDesktopBotDest(t, dest, map[string]any{"items": []any{}})
				return http.StatusOK, nil
			}
			if method == http.MethodGet && strings.Contains(path, "/runs/") {
				fillDesktopBotDest(t, dest, map[string]any{"text": "已停", "status": "cancelled"})
				return http.StatusOK, nil
			}
			return 0, fmt.Errorf("no bot list")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		got, err := app.relayDesktopBotWhenFree(ctx, "bot_1", "打开示例网站", "execute", "turn-busy", nil)
		if err != nil || got.Text != "已停" {
			t.Fatalf("text=%q err=%v paths=%v", got.Text, err, paths)
		}
		cancelPath := http.MethodPost + " /api/v1/bots/bot_1/runs/run-retry/cancel"
		found := 0
		for _, path := range paths {
			if !strings.Contains(path, "/cancel") {
				continue
			}
			found++
			if path != cancelPath {
				t.Fatalf("cancel path = %s", path)
			}
		}
		if found != 1 || posts != 2 {
			t.Fatalf("cancels=%d posts=%d paths=%v", found, posts, paths)
		}
	})

	t.Run("keeps a registered turn's stop when another request starts first", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		app := &App{}
		app.registerActiveDesktopRun("bot_1", "req-chat", "alice:bot_1", "打开示例网站", "execute")
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		desktopBotExchangeOverride = func(_ context.Context, _, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			if strings.Contains(path, "/cancel") {
				t.Fatalf("other request cancelled: %s", path)
			}
			return http.StatusBadRequest, fmt.Errorf("拒绝")
		}
		if _, err := app.relayDesktopBot(context.Background(), "bot_1", "别的", "execute", ""); err == nil {
			t.Fatal("expected the other admit to fail")
		}
		cancels := 0
		desktopBotExchangeOverride = func(_ context.Context, _, path string, _ any, _ any, _ time.Duration, _ map[string]string) (int, error) {
			if strings.Contains(path, "/cancel") {
				cancels++
				return http.StatusOK, nil
			}
			t.Fatalf("unexpected %s", path)
			return 0, nil
		}
		if err := app.nameAdmittedDesktopRun("bot_1", "req-chat", "run-chat"); err != nil {
			t.Fatal(err)
		}
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		if cancels != 1 {
			t.Fatalf("cancels=%d", cancels)
		}
	})

	t.Run("does not stop a later admit when nothing was underway", func(t *testing.T) {
		resetActiveDesktopRuns(t)
		resetDesktopAdmitCancel()
		app := &App{}
		if err := app.ArmDesktopBotAdmitCancel("bot_1"); err != nil {
			t.Fatal(err)
		}
		desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			if strings.Contains(path, "/cancel") {
				t.Fatalf("later admit cancelled: %s", path)
			}
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run-later", "status": "running"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "开了", "status": "succeeded"})
			return http.StatusOK, nil
		}
		got, err := app.relayDesktopBot(context.Background(), "bot_1", "下一件", "execute", "turn-later")
		if err != nil || got.Text != "开了" {
			t.Fatalf("text=%q err=%v", got.Text, err)
		}
	})
}

func TestDesktopBotAdmissionPoll(t *testing.T) {
	previous := desktopBotPollInterval
	desktopBotPollInterval = time.Millisecond
	t.Cleanup(func() {
		desktopBotPollInterval = previous
		desktopBotExchangeOverride = nil
	})

	t.Run("retries a dropped read and then reports the text", func(t *testing.T) {
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, path string, body any, dest any, timeout time.Duration, headers map[string]string) (int, error) {
			calls++
			switch calls {
			case 1:
				if method != http.MethodPost || !strings.HasSuffix(path, "/messages") {
					t.Fatalf("admit call = %s %s", method, path)
				}
				if headers["Prefer"] != "respond-async" {
					t.Fatalf("prefer = %q", headers["Prefer"])
				}
				if timeout != 30*time.Minute {
					t.Fatalf("admit timeout = %s", timeout)
				}
				raw, _ := json.Marshal(body)
				if !strings.Contains(string(raw), `"phase":"execute"`) {
					t.Fatalf("body = %s", raw)
				}
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_1", "status": "running"})
				return http.StatusAccepted, nil
			case 2:
				if timeout != 20*time.Second || !strings.Contains(path, "/runs/run_1") {
					t.Fatalf("poll = %s timeout %s", path, timeout)
				}
				return 0, fmt.Errorf("MaClawSrv 不可达")
			case 3:
				return 0, fmt.Errorf("MaClawSrv 没有在时限内返回结果")
			case 4:
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running"})
				return http.StatusAccepted, nil
			default:
				fillDesktopBotDest(t, dest, map[string]any{"text": "页面已打开", "status": "succeeded"})
				return http.StatusOK, nil
			}
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "按这个安排执行", "execute", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != "页面已打开" || calls != 5 {
			t.Fatalf("text=%q calls=%d", got.Text, calls)
		}
	})

	t.Run("retries a gateway 502 and then reports the text", func(t *testing.T) {
		// A 502 is the proxy in front of Hub failing to answer. The run is
		// still driving the desktop, so the read is tried again. A turn that
		// had been running for forty minutes used to die on exactly this.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_502", "status": "running"})
				return http.StatusAccepted, nil
			}
			switch calls {
			case 2, 3, 4:
				return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
			default:
				fillDesktopBotDest(t, dest, map[string]any{"text": "页面已打开", "status": "succeeded"})
				return http.StatusOK, nil
			}
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "")
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != "页面已打开" || calls != 5 {
			t.Fatalf("text=%q calls=%d", got.Text, calls)
		}
	})

	t.Run("stops on a real hub error", func(t *testing.T) {
		// A 404 is an answer: the server looked and the run is not there.
		// Only that ends the turn, never a status the server did not mean.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_err", "status": "running"})
				return http.StatusAccepted, nil
			}
			return http.StatusNotFound, fmt.Errorf("bot not found")
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "")
		if err == nil || !strings.Contains(err.Error(), "bot not found") || calls != 2 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})

	t.Run("retries one poll timeout", func(t *testing.T) {
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"run_id": "run_slow", "status": "running"})
				return http.StatusAccepted, nil
			}
			if calls == 2 {
				return 0, context.DeadlineExceeded
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "plan", "")
		if err != nil || got.Text != "做完了" || calls != 3 {
			t.Fatalf("text=%q err=%v calls=%d", got.Text, err, calls)
		}
	})

	t.Run("resends a lost admission answer with the same key", func(t *testing.T) {
		// Hub answered 502 but named the key it received, which proves it
		// forwards the key to MaClawSrv. The same key is sent again: a
		// repeated key replays the run instead of starting a second one.
		var bodies []string
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, body any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			raw, _ := json.Marshal(body)
			bodies = append(bodies, string(raw))
			if method == http.MethodPost {
				if len(bodies) == 1 {
					fillDesktopBotDest(t, dest, map[string]any{"client_message_id": "turn-1"})
					return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
				}
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_retry", "status": "running", "client_message_id": "turn-1"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "做完了", "status": "succeeded"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "turn-1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != "做完了" || len(bodies) != 3 {
			t.Fatalf("text=%q sends=%d", got.Text, len(bodies))
		}
		if !strings.Contains(bodies[0], `"client_message_id":"turn-1"`) || bodies[0] != bodies[1] {
			t.Fatalf("resend changed the turn: %s vs %s", bodies[0], bodies[1])
		}
	})

	t.Run("does not resend an admission the hub never keyed", func(t *testing.T) {
		// A 502 with no key echo may be a Hub that drops the key. Resending
		// against that Hub could start the work twice, so the failure stands
		// until one answer proves the echo.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "turn-2")
		if err == nil || !strings.Contains(err.Error(), "unavailable") || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})

	t.Run("does not treat an empty echo as proof of a callerless turn", func(t *testing.T) {
		// A scheduled occurrence has no caller behind it and sends no key.
		// Hub mints a fresh key per call and the failure carries no echo, so
		// "no key" matched "no key" would look proven — and every resend
		// would start a new run instead of replaying one. The absence of a
		// key must never count as the round-trip that licenses a retry.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "到期任务", "execute", "")
		if err == nil || !strings.Contains(err.Error(), "unavailable") || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})

	t.Run("does not resend a rejected admission", func(t *testing.T) {
		// A 400 is an answer the server meant. Sending it again only
		// repeats the rejection.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"client_message_id": "turn-3"})
				return http.StatusBadRequest, fmt.Errorf("invalid bot settings")
			}
			fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "turn-3")
		if err == nil || !strings.Contains(err.Error(), "invalid bot settings") || calls != 1 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})

	t.Run("does not reuse a key echo from a different hub", func(t *testing.T) {
		// Two different Hubs answer within one session, which happens after
		// a redeploy or a rollback. The first echoes the key; the second is
		// an older build that drops it. Permission to resend must come from
		// the answer in hand, never from what an earlier Hub did — otherwise
		// a retry against the old Hub starts the work a second time.
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method != http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
				return http.StatusOK, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"client_message_id": "turn-echo"})
			return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		// First turn: this Hub proves the echo but still fails.
		if _, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "turn-echo"); err == nil {
			t.Fatal("expected the 502 to stand")
		}
		callsAfterFirst := calls

		// Same process, different Hub behind it: no echo this time.
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method != http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"text": "做完了"})
				return http.StatusOK, nil
			}
			return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
		}
		if _, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute", "turn-new"); err == nil {
			t.Fatal("expected the 502 to stand")
		}
		if sent := calls - callsAfterFirst; sent != 1 {
			t.Fatalf("retried %d times against a hub that dropped the key", sent)
		}
	})

	t.Run("keeps a synchronous reply on the admitting call", func(t *testing.T) {
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, _ string, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			fillDesktopBotDest(t, dest, map[string]any{"text": "同步回复"})
			return http.StatusOK, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "看一下", "plan", "")
		if err != nil || got.Text != "同步回复" || calls != 1 {
			t.Fatalf("text=%q err=%v calls=%d", got.Text, err, calls)
		}
	})

	t.Run("stops polling when the turn is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"run_id": "run_cancel", "status": "running"})
				return http.StatusAccepted, nil
			}
			cancel()
			fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running"})
			return http.StatusAccepted, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(ctx, "bot_1", "打开", "execute", "")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("hands every run read a context the turn can cancel", func(t *testing.T) {
		// The read used to hang off context.Background, so cancelling the
		// turn could not cut a poll short — abandoning a bot talking to a
		// slow Hub kept reading it for the full read window. The context
		// reaching each GET must be the turn's own, not a fresh root.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// The snapshot has to be taken the instant the turn is cancelled.
		// Reading it afterwards is useless: by then the cancellation has
		// propagated on its own and even a context rooted at Background
		// looks cancelled.
		reached := false
		desktopBotExchangeOverride = func(callCtx context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			if method == http.MethodGet {
				cancel()
				reached = callCtx.Err() != nil
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "status": "running"})
				return http.StatusAccepted, nil
			}
			fillDesktopBotDest(t, dest, map[string]any{"run_id": "run_ctx", "status": "running"})
			return http.StatusAccepted, nil
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		if _, err := (&App{}).relayDesktopBot(ctx, "bot_1", "打开", "execute", ""); !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
		if !reached {
			t.Fatal("the run read had its own context, so cancelling the turn cannot cut it short")
		}
	})
}

func TestWatchDesktopBotRequiresABotID(t *testing.T) {
	if _, err := (&App{}).WatchDesktopBot(" ", 0); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
}

func TestDesktopBotPollAgainKeepsARunThroughAProxyBlink(t *testing.T) {
	// The run lives on MaClawSrv. A read that never got an answer cannot
	// have changed it, so nothing but a rejection the server meant ends the
	// turn.
	cases := []struct {
		name   string
		status int
		err    error
		again  bool
	}{
		{"dropped connection", 0, fmt.Errorf("MaClawSrv 不可达"), true},
		{"deadline", 0, context.DeadlineExceeded, true},
		{"gateway 502", http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator"), true},
		{"unavailable 503", http.StatusServiceUnavailable, fmt.Errorf("MaClawSrv 不可达"), true},
		{"gateway timeout 504", http.StatusGatewayTimeout, fmt.Errorf("MaClawSrv 不可达"), true},
		{"not found 404", http.StatusNotFound, fmt.Errorf("bot not found"), false},
		{"bad request 400", http.StatusBadRequest, fmt.Errorf("invalid bot settings"), false},
		{"no error", http.StatusOK, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desktopBotPollAgain(tc.status, tc.err); got != tc.again {
				t.Fatalf("desktopBotPollAgain(%d, %v) = %v, want %v", tc.status, tc.err, got, tc.again)
			}
		})
	}
}

func TestDesktopBotPollStretchCapsAndResets(t *testing.T) {
	previous := desktopBotPollInterval
	desktopBotPollInterval = time.Second
	t.Cleanup(func() { desktopBotPollInterval = previous })
	if got := desktopBotPollStretch(0); got != time.Second {
		t.Fatalf("first failure wait = %s", got)
	}
	if got := desktopBotPollStretch(2); got != 4*time.Second {
		t.Fatalf("third failure wait = %s", got)
	}
	if got := desktopBotPollStretch(50); got != desktopBotPollStretchMax {
		t.Fatalf("capped wait = %s, want %s", got, desktopBotPollStretchMax)
	}
	// A read that answers resets the count, so a slow run never pays it.
	if got := desktopBotPollStretch(0); got != time.Second {
		t.Fatalf("reset wait = %s", got)
	}
}

func TestFinishDesktopBotTaskWaitsOutABusyDesktop(t *testing.T) {
	prev := desktopBotBusyPoll
	desktopBotBusyPoll = 5 * time.Millisecond
	app := &App{}
	var got *IMAgentResponse
	var queued []string
	relays := 0
	t.Cleanup(func() {
		desktopBotBusyPoll = prev
		desktopBotRelayOverride = nil
		desktopBotExchangeOverride = nil
		desktopBotResultSink = nil
		desktopBotQueuedSink = nil
	})
	desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
	desktopBotQueuedSink = func(payload string) { queued = append(queued, payload) }
	desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
		relays++
		if relays == 1 {
			return desktopBotRelayResult{}, fmt.Errorf("%s", desktopBotBusyMessage)
		}
		return desktopBotRelayResult{Text: "页面打开了。"}, nil
	}
	polls := 0
	desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
		if method == http.MethodGet && strings.Contains(path, "/tasks") {
			polls++
			payload := map[string]any{
				"items": []map[string]any{{"run_id": "run-1", "bot_id": "bot_1", "status": "running", "task": "早报"}},
			}
			if polls >= 2 {
				payload = map[string]any{"items": []any{}}
			}
			fillDesktopBotDest(t, dest, payload)
			return http.StatusOK, nil
		}
		return 0, fmt.Errorf("no bot list")
	}
	app.finishDesktopBotTask("desktop-bot-busy", "alice:bot_1", "bot_1", "打开示例网站", "execute")
	if relays != 2 || got == nil || got.Text != "页面打开了。" || got.Error != "" {
		t.Fatalf("relays=%d reply=%#v", relays, got)
	}
	if strings.Contains(got.Error, desktopBotBusyMessage) || strings.Contains(got.Text, desktopBotBusyMessage) {
		t.Fatalf("busy sentence became the reply: %#v", got)
	}
	if len(queued) != 1 || !strings.Contains(queued[0], `"waiting":true`) || !strings.Contains(queued[0], `"bot_id":"bot_1"`) || !strings.Contains(queued[0], `"session_key":"alice:bot_1"`) {
		t.Fatalf("queued = %v", queued)
	}
}
