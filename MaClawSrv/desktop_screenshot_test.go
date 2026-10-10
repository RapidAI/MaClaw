package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func desktopTestPNG(t *testing.T, width, height int, noisy bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewSource(1))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.RGBA{R: 30, G: 60, B: 90, A: 255}
			if noisy {
				c = color.RGBA{R: uint8(rng.Intn(256)), G: uint8(rng.Intn(256)), B: uint8(rng.Intn(256)), A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDesktopPageListIncludesEveryTab(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[
			{"id":"yt","type":"page","title":"A long YouTube title","url":"https://www.youtube.com/watch?v=secret"},
			{"id":"fb","type":"page","title":"(20+) Facebook","url":"https://www.facebook.com/"},
			{"id":"worker","type":"worker","title":"skip","url":"https://example.com/worker"}
		]`)
	}))
	defer srv.Close()

	text := listDesktopOpenPages(srv.URL, "fb")
	if !strings.Contains(text, "Open browser pages (2)") || !strings.Contains(text, "YouTube") || !strings.Contains(text, "https://www.youtube.com/watch") {
		t.Fatalf("text=%q", text)
	}
	if !strings.Contains(text, "- attached (20+) Facebook — https://www.facebook.com/") || strings.Contains(text, "secret") || strings.Contains(text, "worker") {
		t.Fatalf("text=%q", text)
	}
}

func TestDesktopPagesStayVisibleWhenTheAttachedTabTimesOut(t *testing.T) {
	previous := desktopOpenPages
	desktopOpenPages = func(addr, attached string) string {
		if addr != "http://desktop" || attached != "tab-b" {
			t.Fatalf("addr=%s attached=%s", addr, attached)
		}
		return "Open browser pages (2), including tabs behind the front window:\n- YouTube — https://www.youtube.com/watch\n- attached (20+) Facebook — https://www.facebook.com/"
	}
	t.Cleanup(func() { desktopOpenPages = previous })

	text, err := withDesktopPages("", fmt.Errorf("cdp timeout: Runtime.evaluate (id=3)"), "http://desktop", "tab-b")
	if err != nil || !strings.Contains(text, "YouTube") || !strings.Contains(text, "cdp timeout") || strings.Contains(text, "://secret") {
		t.Fatalf("text=%q err=%v", text, err)
	}

	shot := agentruntime.AttachModelImage("Desktop screenshot of display :20.", "image/png", base64.StdEncoding.EncodeToString(desktopTestPNG(t, 8, 8, false)))
	combined, err := withDesktopPages(shot, nil, "http://desktop", "tab-b")
	plain, images := agentruntime.ExtractModelImages(combined)
	if err != nil || len(images) != 1 || !strings.Contains(plain, "Desktop screenshot") || !strings.Contains(plain, "Facebook") {
		t.Fatalf("plain=%q images=%d err=%v", plain, len(images), err)
	}
}

func TestScreenshotRaisesTheLoadedPageWithoutAResume(t *testing.T) {
	shot := desktopTestPNG(t, 8, 8, false)
	scope := agentruntime.Scope{TenantID: "tenant-shot-raise", UserID: "alice", InstanceID: "shot-raise"}
	previousSession := desktopRemoteSession
	previousShot := desktopRemoteScreenshot
	previousOpen := desktopOpenBrowser
	previousFocus := desktopFocusVisiblePage
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		return shot, nil
	}
	raised := 0
	desktopOpenBrowser = func(*desktopBinding, agentruntime.Scope, string, string) (*browser.BrowserAgentSession, error) {
		return &browser.BrowserAgentSession{}, nil
	}
	desktopFocusVisiblePage = func(sess *browser.BrowserAgentSession) error {
		if sess == nil {
			t.Fatal("screenshot raised a nil browser")
		}
		raised++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteScreenshot = previousShot
		desktopOpenBrowser = previousOpen
		desktopFocusVisiblePage = previousFocus
	})

	out, err := operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"})
	text, images := agentruntime.ExtractModelImages(out)
	if err != nil || raised != 1 || !strings.Contains(text, "8x8") || len(images) != 1 {
		t.Fatalf("raised=%d text=%q images=%d err=%v", raised, text, len(images), err)
	}
}

func TestDesktopScreenshotAttachesTheImageForTheModel(t *testing.T) {
	shot := desktopTestPNG(t, 1440, 900, false)
	var gotUser, gotDisplay string
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(_ context.Context, _, userID, display string) ([]byte, error) {
		gotUser, gotDisplay = userID, display
		return shot, nil
	}
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "inst-shot"}
	ctx, slot := withDesktopShotSlot(context.Background())
	out, err := desktopScreenshot(ctx, scope, ":20")
	if err != nil {
		t.Fatal(err)
	}
	text, images := agentruntime.ExtractModelImages(out)
	if gotUser != "alice" || gotDisplay != ":20" {
		t.Fatalf("user=%q display=%q", gotUser, gotDisplay)
	}
	if !strings.Contains(text, "1440x900") || !strings.Contains(text, "image/png") || strings.Contains(text, base64.StdEncoding.EncodeToString(shot)[:40]) {
		t.Fatalf("text=%q", text)
	}
	if len(images) != 1 || images[0].MIME != "image/png" {
		t.Fatalf("images=%d", len(images))
	}
	decoded, err := base64.StdEncoding.DecodeString(images[0].Base64)
	if err != nil || !bytes.Equal(decoded, shot) {
		t.Fatal("attached image is not the screenshot")
	}
	if !strings.Contains(text, "shown in the chat") {
		t.Fatalf("text=%q", text)
	}
	reply := &agentservice.Message{}
	if n := attachDesktopShot(reply, slot); n != 1 || len(reply.Attachments) != 1 || reply.Attachments[0].MimeType != "image/png" {
		t.Fatalf("reply shot=%d %#v", n, reply.Attachments)
	}
	shown, err := base64.StdEncoding.DecodeString(reply.Attachments[0].Data)
	if err != nil || !bytes.Equal(shown, shot) {
		t.Fatal("reply image is not the screenshot")
	}
	if n := attachDesktopShot(reply, slot); n != 0 || len(reply.Attachments) != 1 {
		t.Fatal("screenshot stayed attached after the reply took it")
	}
}

func TestDesktopShotReplacesThePreviousOne(t *testing.T) {
	first := desktopTestPNG(t, 4, 4, false)
	second := desktopTestPNG(t, 5, 4, false)
	ctx, slot := withDesktopShotSlot(context.Background())
	if !noteDesktopShot(ctx, "image/png", base64.StdEncoding.EncodeToString(first)) || !noteDesktopShot(ctx, "image/png", base64.StdEncoding.EncodeToString(second)) {
		t.Fatal("screenshot was not kept")
	}
	msg := &agentservice.Message{}
	if attachDesktopShot(msg, slot) != 1 {
		t.Fatal("missing screenshot")
	}
	decoded, err := base64.StdEncoding.DecodeString(msg.Attachments[0].Data)
	if err != nil || !bytes.Equal(decoded, second) {
		t.Fatal("latest screenshot was not kept")
	}
	if noteDesktopShot(ctx, "text/html", base64.StdEncoding.EncodeToString(first)) || noteDesktopShot(context.Background(), "image/png", base64.StdEncoding.EncodeToString(first)) {
		t.Fatal("invalid screenshot was stored")
	}
	otherCtx, other := withDesktopShotSlot(context.Background())
	if !noteDesktopShot(otherCtx, "image/png", base64.StdEncoding.EncodeToString(first)) {
		t.Fatal("other turn did not keep its screenshot")
	}
	if attachDesktopShot(&agentservice.Message{}, slot) != 0 {
		t.Fatal("this turn took the other turn's screenshot")
	}
	if attachDesktopShot(&agentservice.Message{}, other) != 1 {
		t.Fatal("other turn lost its screenshot")
	}
}

func TestAttachDesktopShotLeavesTheStoredMessageAlone(t *testing.T) {
	png := desktopTestPNG(t, 4, 3, false)
	ctx, slot := withDesktopShotSlot(context.Background())
	if !noteDesktopShot(ctx, "image/png", base64.StdEncoding.EncodeToString(png)) {
		t.Fatal("screenshot was not kept")
	}
	msg := &agentservice.Message{Content: "看桌面"}
	if n := attachDesktopShot(msg, slot); n != 1 || len(msg.Attachments) != 1 || msg.Attachments[0].MimeType != "image/png" {
		t.Fatalf("attached=%d %#v", n, msg.Attachments)
	}
	if n := attachDesktopShot(msg, slot); n != 0 || len(msg.Attachments) != 1 {
		t.Fatalf("second attach=%d %#v", n, msg.Attachments)
	}
	ctx, slot = withDesktopShotSlot(context.Background())
	if !noteDesktopShot(ctx, "image/png", base64.StdEncoding.EncodeToString(png)) {
		t.Fatal("screenshot was not kept")
	}
	if n := attachDesktopShot(nil, slot); n != 0 {
		t.Fatal("nil message kept the screenshot")
	}
	if n := attachDesktopShot(&agentservice.Message{}, slot); n != 0 {
		t.Fatal("screenshot leaked onto the next reply")
	}
}

func TestLargeDesktopScreenshotIsSentAsJPEG(t *testing.T) {
	shot := desktopTestPNG(t, 1000, 700, true)
	if base64.StdEncoding.EncodedLen(len(shot)) <= desktopScreenshotMaxBase64 {
		t.Skip("test image is not large enough")
	}
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) { return shot, nil }
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	ctx, slot := withDesktopShotSlot(context.Background())
	out, err := desktopScreenshot(ctx, agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20")
	if err != nil {
		t.Fatal(err)
	}
	text, images := agentruntime.ExtractModelImages(out)
	if len(images) != 1 || images[0].MIME != "image/jpeg" || !strings.Contains(text, "1000x700") {
		t.Fatalf("text=%q images=%d", text, len(images))
	}
	raw, err := base64.StdEncoding.DecodeString(images[0].Base64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 1000 || cfg.Height != 700 {
		t.Fatalf("jpeg=%+v err=%v", cfg, err)
	}
	reply := &agentservice.Message{}
	n := attachDesktopShot(reply, slot)
	if len(images[0].Base64) <= desktopScreenshotMaxBase64 {
		if n != 1 || reply.Attachments[0].Data != images[0].Base64 || !strings.Contains(text, "shown in the chat") {
			t.Fatal("fitting screenshot was not shown")
		}
		return
	}
	if n != 0 || strings.Contains(text, "shown in the chat") {
		t.Fatal("oversized screenshot was placed on the reply")
	}
}

func TestDesktopScreenshotRejectsNonImages(t *testing.T) {
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) { return []byte("<html>"), nil }
	t.Cleanup(func() { desktopRemoteScreenshot = previous })
	if _, err := desktopScreenshot(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20"); err == nil {
		t.Fatal("non-PNG screenshot was accepted")
	}
}

func TestLocalDesktopScreenshotUsesTheDisplay(t *testing.T) {
	shot := desktopTestPNG(t, 8, 6, false)
	var seen string
	previous := desktopLocalScreenshotRunner
	desktopLocalScreenshotRunner = func(display string) (string, error) {
		seen = display
		return base64.StdEncoding.EncodeToString(shot) + "\n", nil
	}
	t.Cleanup(func() { desktopLocalScreenshotRunner = previous })
	data, err := desktopLocalScreenshot(":20")
	if err != nil || !bytes.Equal(data, shot) || seen != ":20" {
		t.Fatalf("seen=%q err=%v", seen, err)
	}
	if _, err := desktopLocalScreenshot(":20;id"); err == nil {
		t.Fatal("invalid display was accepted")
	}
}

func TestHubClientFetchesTheScreenshot(t *testing.T) {
	shot := desktopTestPNG(t, 8, 6, false)
	var path, auth, user string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		user = in["user_id"]
		_ = json.NewEncoder(w).Encode(map[string]any{"mime": "image/png", "image_base64": base64.StdEncoding.EncodeToString(shot)})
	}))
	defer srv.Close()
	client := &desktopHubClient{baseURL: srv.URL, token: "hub-token", http: srv.Client()}
	data, err := client.Screenshot(context.Background(), "tenant", "alice", ":20")
	if err != nil || !bytes.Equal(data, shot) {
		t.Fatalf("err=%v", err)
	}
	if path != "/api/v1/desktop-services/screenshot" || auth != "Bearer hub-token" || user != "alice" {
		t.Fatalf("path=%q auth=%q user=%q", path, auth, user)
	}
	var savedName string
	saveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		savedName = in["name"]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mime": "image/png", "image_base64": base64.StdEncoding.EncodeToString(shot),
			"saved_path": "/home/desktop/Desktop/baidu_screenshot.png",
		})
	}))
	defer saveSrv.Close()
	saver := &desktopHubClient{baseURL: saveSrv.URL, token: "hub-token", http: saveSrv.Client()}
	data, saved, err := saver.screenshot(context.Background(), "tenant", "alice", ":20", "baidu_screenshot.png")
	if err != nil || !bytes.Equal(data, shot) || saved != "/home/desktop/Desktop/baidu_screenshot.png" || savedName != "baidu_screenshot.png" {
		t.Fatalf("saved=%q name=%q err=%v", saved, savedName, err)
	}
	data, saved, err = saver.screenshot(context.Background(), "tenant", "alice", ":20", "other.png")
	if err != nil || saved != "" || !bytes.Equal(data, shot) {
		t.Fatalf("mismatched path saved=%q err=%v", saved, err)
	}
}

func TestDesktopToolAdvertisesScreenshot(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Instance: agentservice.Instance{Metadata: map[string]string{"hub_bot": "1"}}},
	})
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	raw, _ := json.Marshal(tools[0])
	if !strings.Contains(tools[0].Description, "action=screenshot") || !strings.Contains(string(raw), "screenshot") {
		t.Fatalf("tool does not offer screenshot: %s", raw)
	}
	if !strings.Contains(tools[0].Description, "Do not click the browser window with pixels") || !strings.Contains(tools[0].Description, "no element ref") || !strings.Contains(tools[0].Description, "probe again without it") {
		t.Fatalf("tool description: %s", tools[0].Description)
	}
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	})
	if err != nil || !strings.Contains(prompt, "Do not pixel-click the browser") || !strings.Contains(prompt, "no element ref") || !strings.Contains(prompt, "element ref, not a pixel click") || !strings.Contains(prompt, "probe again without it") || !strings.Contains(prompt, "Do not invent a path") {
		t.Fatalf("prompt=%q err=%v", prompt, err)
	}
	if !strings.Contains(tools[0].Description, "/home/desktop/Desktop") {
		t.Fatalf("tool description: %s", tools[0].Description)
	}
}

func TestAppRunAttachesSettledScreenshot(t *testing.T) {
	const width, height = 13, 9
	shot := desktopTestPNG(t, width, height, false)
	scope := agentruntime.Scope{TenantID: "tenant-shot-run", UserID: "alice", InstanceID: "shot-bot"}
	previousSession := desktopRemoteSession
	previousApp := desktopRemoteApp
	previousShot := desktopRemoteScreenshot
	previousWait := desktopAppWait
	previousPages := desktopOpenPages
	var order []string
	var waited time.Duration
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		order = append(order, "app:"+strings.Join(args, " "))
		return "clicked", nil
	}
	desktopAppWait = func(_ context.Context, d time.Duration) {
		waited = d
		order = append(order, "wait")
	}
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		order = append(order, "shot")
		return shot, nil
	}
	desktopOpenPages = func(string, string) string { return "" }
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteApp = previousApp
		desktopRemoteScreenshot = previousShot
		desktopAppWait = previousWait
		desktopOpenPages = previousPages
	})

	ctx, slot := withDesktopShotSlot(context.Background())
	text, err := operateDesktop(ctx, scope, map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "click", "x": 4, "y": 5}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if waited < 400*time.Millisecond || waited > 600*time.Millisecond {
		t.Fatalf("settle %s", waited)
	}
	if len(order) != 6 || !strings.HasPrefix(order[0], "app:getactivewindow") || strings.Contains(order[0], "windowactivate") || !strings.HasPrefix(order[1], "app:keyup") || order[2] != "app:"+strings.Join(desktopWhiskerDismiss, " ") || !strings.Contains(order[3], "mousemove") || strings.Contains(order[3], "keyup") || order[4] != "wait" || order[5] != "shot" {
		t.Fatalf("order=%v", order)
	}
	for _, side := range []string{"Alt_L", "Alt_R", "Control_L", "Control_R", "Shift_L", "Shift_R", "Super_L", "Super_R"} {
		if !strings.Contains(order[1], side) {
			t.Fatalf("release missing %s: %s", side, order[1])
		}
	}
	if !strings.Contains(order[1], "--delay 0") || strings.Contains(order[1], "mousemove") {
		t.Fatalf("release was not its own call: %s", order[1])
	}
	plain, images := agentruntime.ExtractModelImages(text)
	if len(images) != 1 || !strings.Contains(plain, "clicked") || !strings.Contains(plain, "13x9") || !strings.Contains(plain, "app_run click uses these pixel coordinates") {
		t.Fatalf("plain=%q images=%d", plain, len(images))
	}
	raw, err := base64.StdEncoding.DecodeString(images[0].Base64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != width || cfg.Height != height {
		t.Fatalf("png=%+v err=%v", cfg, err)
	}
	reply := &agentservice.Message{}
	if n := attachDesktopShot(reply, slot); n != 1 || len(reply.Attachments) != 1 {
		t.Fatalf("attach=%d", n)
	}
	if n := attachDesktopShot(&agentservice.Message{}, slot); n != 0 {
		t.Fatal("the screenshot stayed on the first request")
	}

	ctx2, slot2 := withDesktopShotSlot(context.Background())
	order = nil
	text, err = operateDesktop(ctx2, scope, map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "focus", "name": "Notes"}},
	})
	if err != nil || strings.Contains(text, "13x9") || len(order) != 1 || !strings.HasPrefix(order[0], "app:") || strings.Contains(order[0], "keyup") {
		t.Fatalf("focus text=%q err=%v order=%v", text, err, order)
	}
	if _, images = agentruntime.ExtractModelImages(text); len(images) != 0 {
		t.Fatal("focus-only app_run attached an image")
	}
	if attachDesktopShot(&agentservice.Message{}, slot2) != 0 {
		t.Fatal("a later request received the first screenshot")
	}

	order = nil
	text, err = operateDesktop(ctx2, scope, map[string]any{"action": "app_list"})
	if err != nil || len(order) != 1 || strings.Contains(order[0], "shot") || strings.Contains(order[0], "keyup") {
		t.Fatalf("app_list text=%q err=%v order=%v", text, err, order)
	}

	order = nil
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		order = append(order, "shot")
		return nil, fmt.Errorf("display asleep")
	}
	text, err = operateDesktop(ctx2, scope, map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Return"}},
	})
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || plain != "clicked" || len(images) != 0 || len(order) != 6 || !strings.HasPrefix(order[0], "app:getactivewindow") || !strings.HasPrefix(order[1], "app:keyup") || order[2] != "app:"+strings.Join(desktopWhiskerDismiss, " ") || !strings.Contains(order[3], "key Return") || strings.Contains(order[3], "keyup") || strings.Contains(order[0], "windowactivate") || order[4] != "wait" || order[5] != "shot" {
		t.Fatalf("failed shot text=%q err=%v images=%d order=%v", text, err, len(images), order)
	}

	order = nil
	waited = 0
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctxCancel, _ := withDesktopShotSlot(base)
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		order = append(order, "app:"+strings.Join(args, " "))
		for _, arg := range args {
			if arg == "click" {
				cancel()
				break
			}
		}
		return "clicked", nil
	}
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		order = append(order, "shot")
		return shot, nil
	}
	text, err = operateDesktop(ctxCancel, scope, map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "click", "x": 4, "y": 5}},
	})
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || !strings.Contains(plain, "clicked") || len(images) != 0 || waited != 0 || len(order) != 4 || !strings.HasPrefix(order[0], "app:getactivewindow") || !strings.HasPrefix(order[1], "app:keyup") || order[2] != "app:"+strings.Join(desktopWhiskerDismiss, " ") || !strings.Contains(order[3], "mousemove") {
		t.Fatalf("cancelled settle text=%q err=%v images=%d waited=%s order=%v", text, err, len(images), waited, order)
	}

	live, cancelLive := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancelLive()
	}()
	started := time.Now()
	previousWait(live, time.Second)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("settle ignored cancel: %s", elapsed)
	}
}

func TestAppRunWaitsWhilePersonVerifies(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-person-verify", UserID: "alice", InstanceID: "bot-b"}
	key := desktopRunKey(scope.TenantID, scope.UserID)
	desktopPersonInstance.Store(key, "bot-a")
	calls := 0
	previous := desktopRemoteApp
	desktopRemoteApp = func(context.Context, string, string, string, []string) (string, error) {
		calls++
		return "ok", nil
	}
	t.Cleanup(func() {
		desktopRemoteApp = previous
		desktopPersonInstance.Delete(key)
	})
	_, err := operateDesktop(context.Background(), scope, map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Return"}},
	})
	if err == nil || !strings.Contains(err.Error(), "登录") || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	_, err = operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant-click-ref", UserID: "alice"}, map[string]any{"action": "click"})
	if err == nil || !strings.Contains(err.Error(), "ref or text") || calls != 0 {
		t.Fatalf("click err=%v calls=%d", err, calls)
	}
}

func TestProbeWithoutRefsAttachesDesktopImage(t *testing.T) {
	const width, height = 11, 7
	shot := desktopTestPNG(t, width, height, false)
	scope := agentruntime.Scope{TenantID: "tenant-probe-shot", UserID: "alice", InstanceID: "probe-bot"}
	shots := 0
	previous := desktopRemoteScreenshot
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		shots++
		return shot, nil
	}
	t.Cleanup(func() { desktopRemoteScreenshot = previous })

	ctx, slot := withDesktopShotSlot(context.Background())
	text, err := desktopObservationText(ctx, scope, ":20", "", &browser.BrowserObservation{
		Display: "a canvas with no controls",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, images := agentruntime.ExtractModelImages(text)
	if shots != 1 || len(images) != 1 || !strings.Contains(plain, "No element ref was found") || !strings.Contains(plain, "11x7") || !strings.Contains(plain, "Do not pixel-click the browser") || strings.Contains(plain, "app_run click uses these pixel coordinates") {
		t.Fatalf("plain=%q images=%d shots=%d", plain, len(images), shots)
	}
	if strings.Index(plain, "Do not pixel-click the browser.") < strings.Index(plain, "11x7") {
		t.Fatalf("pixel-click ban was not the last instruction: %q", plain)
	}
	raw, err := base64.StdEncoding.DecodeString(images[0].Base64)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != width || cfg.Height != height {
		t.Fatalf("png=%+v err=%v", cfg, err)
	}
	if attachDesktopShot(&agentservice.Message{}, slot) != 1 {
		t.Fatal("empty probe did not keep the image on this request")
	}

	text, err = desktopObservationText(context.Background(), scope, ":20", "Save", &browser.BrowserObservation{
		Display:  "a button",
		Snapshot: browser.BrowserSnapshot{Refs: []browser.BrowserElementRef{{Ref: "e1", Role: "button", Name: "Save"}}},
	}, nil)
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || shots != 1 || len(images) != 0 || strings.Contains(plain, "No element ref") || plain != "a button" {
		t.Fatalf("ref plain=%q images=%d shots=%d err=%v", plain, len(images), shots, err)
	}

	text, err = desktopObservationText(context.Background(), scope, ":20", "missing-label", &browser.BrowserObservation{
		Display: "a form",
	}, nil)
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || shots != 1 || len(images) != 0 || !strings.Contains(plain, "No element ref matched that filter") || !strings.Contains(plain, "Probe again without a filter") || !strings.Contains(plain, "Do not pixel-click the browser") {
		t.Fatalf("filter plain=%q images=%d shots=%d err=%v", plain, len(images), shots, err)
	}

	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		shots++
		return nil, fmt.Errorf("display asleep")
	}
	text, err = desktopObservationText(context.Background(), scope, ":20", "  ", &browser.BrowserObservation{
		Display: "a canvas",
	}, nil)
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || shots != 2 || len(images) != 0 || !strings.Contains(plain, "No element ref was found") || !strings.Contains(plain, "could not be captured") || !strings.Contains(plain, "Do not pixel-click the browser") || strings.Contains(plain, "matched that filter") {
		t.Fatalf("shot error plain=%q images=%d shots=%d err=%v", plain, len(images), shots, err)
	}

	done, cancel := context.WithCancel(context.Background())
	cancel()
	text, err = desktopObservationText(done, scope, ":20", "", &browser.BrowserObservation{
		Display: "a canvas",
	}, nil)
	plain, images = agentruntime.ExtractModelImages(text)
	if err != nil || shots != 2 || len(images) != 0 || !strings.Contains(plain, "No element ref was found") || strings.Contains(plain, "could not be captured") || !strings.Contains(plain, "Do not pixel-click the browser") {
		t.Fatalf("cancelled probe plain=%q images=%d shots=%d err=%v", plain, len(images), shots, err)
	}
}

func TestDesktopInstructionStaysAfterThePageList(t *testing.T) {
	shot := desktopTestPNG(t, 11, 7, false)
	previousShot := desktopRemoteScreenshot
	previousPages := desktopOpenPages
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		return shot, nil
	}
	const pages = "Open browser pages (1), including tabs behind the front window:\n- attached Canvas — https://example.com/canvas"
	desktopOpenPages = func(string, string) string { return pages }
	t.Cleanup(func() {
		desktopRemoteScreenshot = previousShot
		desktopOpenPages = previousPages
	})
	scope := agentruntime.Scope{TenantID: "tenant-pages-tail", UserID: "alice"}
	ctx, _ := withDesktopShotSlot(context.Background())
	obsText, err := desktopObservationText(ctx, scope, ":20", "", &browser.BrowserObservation{
		Display: "a canvas with no controls",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	shown, err := withDesktopPages(obsText, nil, "http://desktop", "tab")
	plain, images := agentruntime.ExtractModelImages(shown)
	banAt := strings.LastIndex(plain, "Do not pixel-click the browser.")
	if err != nil || len(images) != 1 || banAt < 0 || banAt < strings.Index(plain, "Open browser pages") || banAt < strings.Index(plain, "11x7") || strings.Contains(plain, "app_run click uses these pixel coordinates") {
		t.Fatalf("probe plain=%q images=%d err=%v", plain, len(images), err)
	}

	filtered, err := desktopObservationText(context.Background(), scope, ":20", "missing", &browser.BrowserObservation{
		Display: "a form",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	shown, err = withDesktopPages(filtered, nil, "http://desktop", "tab")
	plain, images = agentruntime.ExtractModelImages(shown)
	banAt = strings.LastIndex(plain, "Do not pixel-click the browser.")
	filterAt := strings.LastIndex(plain, "Probe again without a filter.")
	if err != nil || len(images) != 0 || banAt < strings.Index(plain, "Open browser pages") || filterAt < 0 || filterAt > banAt {
		t.Fatalf("filter plain=%q err=%v", plain, err)
	}

	ctx2, _ := withDesktopShotSlot(context.Background())
	caption, err := desktopScreenshot(ctx2, scope, ":20")
	if err != nil {
		t.Fatal(err)
	}
	shown, err = withDesktopPages(caption, nil, "http://desktop", "tab")
	plain, images = agentruntime.ExtractModelImages(shown)
	clickAt := strings.LastIndex(plain, "app_run click uses these pixel coordinates.")
	if err != nil || len(images) != 1 || clickAt < strings.Index(plain, "Open browser pages") || strings.Contains(plain, "Do not pixel-click the browser.") {
		t.Fatalf("shot plain=%q images=%d err=%v", plain, len(images), err)
	}
}

func TestScreenshotSaveNamesOnlyTheWrittenFile(t *testing.T) {
	shot := desktopTestPNG(t, 1440, 900, false)
	scope := agentruntime.Scope{TenantID: "tenant-save", UserID: "alice", InstanceID: "save-bot"}
	previousSession := desktopRemoteSession
	previousShot := desktopRemoteScreenshot
	previousSaved := desktopRemoteSavedPath
	previousOpen := desktopOpenBrowser
	previousFocus := desktopFocusVisiblePage
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) { return shot, nil }
	desktopRemoteSavedPath = func(name string) string { return "/home/desktop/Desktop/" + name }
	desktopOpenBrowser = func(*desktopBinding, agentruntime.Scope, string, string) (*browser.BrowserAgentSession, error) {
		return nil, nil
	}
	desktopFocusVisiblePage = func(*browser.BrowserAgentSession) error { return nil }
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteScreenshot = previousShot
		desktopRemoteSavedPath = previousSaved
		desktopOpenBrowser = previousOpen
		desktopFocusVisiblePage = previousFocus
	})
	ctx, slot := withDesktopShotSlot(context.Background())
	ctx = context.WithValue(ctx, desktopPhaseKey{}, "execute")
	out, err := operateDesktop(ctx, scope, map[string]any{"action": "screenshot", "name": "baidu_screenshot.png"})
	text, images := agentruntime.ExtractModelImages(out)
	if err != nil || len(images) != 1 || !strings.Contains(text, "Saved /home/desktop/Desktop/baidu_screenshot.png") || strings.Contains(text, "was not written") {
		t.Fatalf("text=%q images=%d err=%v", text, len(images), err)
	}
	if desktopSavedPath(slot) != "/home/desktop/Desktop/baidu_screenshot.png" {
		t.Fatal(desktopSavedPath(slot))
	}
	reply := &agentservice.Message{Content: "图片已成功保存到桌面。\n文件路径：~/Desktop/baidu_screenshot.png"}
	publishDesktopReply(reply, slot)
	if !strings.Contains(reply.Content, "/home/desktop/Desktop/baidu_screenshot.png") || strings.Contains(reply.Content, "~/Desktop/") {
		t.Fatalf("reply=%q", reply.Content)
	}

	desktopRemoteSavedPath = nil
	missed, err := operateDesktop(ctx, scope, map[string]any{"action": "screenshot", "name": "baidu_screenshot.png"})
	missedText, _ := agentruntime.ExtractModelImages(missed)
	if err != nil || !strings.Contains(missedText, "was not written") || strings.Contains(missedText, "Saved ") {
		t.Fatalf("missed=%q err=%v", missedText, err)
	}
	claimed := &agentservice.Message{Content: "图片已成功保存到桌面。\n• 文件路径：~/Desktop/baidu_screenshot.png\n• 分辨率：1440×900"}
	publishDesktopReply(claimed, nil)
	if strings.Contains(claimed.Content, "~/Desktop/") || strings.Contains(claimed.Content, "保存到桌面") || !strings.Contains(claimed.Content, "截图没有写到桌面文件夹") || !strings.Contains(claimed.Content, "1440×900") {
		t.Fatalf("claimed=%q", claimed.Content)
	}
	plan := context.WithValue(ctx, desktopPhaseKey{}, "plan")
	if _, err := operateDesktop(plan, scope, map[string]any{"action": "screenshot", "name": "baidu_screenshot.png"}); err == nil || !strings.Contains(err.Error(), "plan phase blocks saving") {
		t.Fatal(err)
	}
	if _, err := operateDesktop(ctx, scope, map[string]any{"action": "screenshot", "name": "../secret.png"}); err == nil {
		t.Fatal("a path name was accepted")
	}
}
