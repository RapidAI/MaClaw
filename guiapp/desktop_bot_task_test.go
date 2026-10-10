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
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "按这个安排执行", "execute")
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != "页面已打开" || calls != 5 {
			t.Fatalf("text=%q calls=%d", got.Text, calls)
		}
	})

	t.Run("stops when an http error uses the unreachable sentence", func(t *testing.T) {
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_http", "status": "running"})
				return http.StatusAccepted, nil
			}
			return http.StatusBadGateway, fmt.Errorf("MaClawSrv 不可达")
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute")
		if err == nil || !strings.Contains(err.Error(), "不可达") || calls != 2 {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})

	t.Run("stops on a real hub error", func(t *testing.T) {
		var calls int
		desktopBotExchangeOverride = func(_ context.Context, method, _ string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
			calls++
			if method == http.MethodPost {
				fillDesktopBotDest(t, dest, map[string]any{"accepted": true, "run_id": "run_err", "status": "running"})
				return http.StatusAccepted, nil
			}
			return http.StatusBadGateway, fmt.Errorf("bot service is unavailable, contact the administrator")
		}
		t.Cleanup(func() { desktopBotExchangeOverride = nil })
		_, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "execute")
		if err == nil || !strings.Contains(err.Error(), "unavailable") || calls != 2 {
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
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "打开", "plan")
		if err != nil || got.Text != "做完了" || calls != 3 {
			t.Fatalf("text=%q err=%v calls=%d", got.Text, err, calls)
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
		got, err := (&App{}).relayDesktopBot(context.Background(), "bot_1", "看一下", "plan")
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
		_, err := (&App{}).relayDesktopBot(ctx, "bot_1", "打开", "execute")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestWatchDesktopBotRequiresABotID(t *testing.T) {
	if _, err := (&App{}).WatchDesktopBot(" ", 0); err == nil {
		t.Fatal("empty bot id should be rejected")
	}
}
