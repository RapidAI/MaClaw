package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// TestLiveDesktopBotActions drives the bot's desktop tool against a real
// cloud desktop through the same InvokeTool path a bot turn uses. It only
// runs on a host configured for the desktop (MACLAW_HUB_URL and
// MACLAW_DESKTOP_API_TOKEN, as in MaClawSrv's .env) and only for the test
// user named by MACLAW_DESKTOP_LIVE_E2E_USER. Screenshots go to
// MACLAW_DESKTOP_LIVE_E2E_OUT.
func TestLiveDesktopBotActions(t *testing.T) {
	user := strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_LIVE_E2E_USER"))
	if user == "" {
		t.Skip("set MACLAW_DESKTOP_LIVE_E2E_USER to a test desktop user")
	}
	out := os.Getenv("MACLAW_DESKTOP_LIVE_E2E_OUT")
	if out == "" {
		out = t.TempDir()
	}
	_ = os.MkdirAll(out, 0o755)
	request := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant_default", UserID: "maclaw-service", InstanceID: "live-e2e-" + user},
		Input: agentservice.ExecuteRequest{
			Instance: agentservice.Instance{ID: "live-e2e-" + user, Metadata: map[string]string{"hub_bot": "1", "hub_user_id": user, "hub_tenant_id": "tenant_default"}},
			Message:  agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}},
		},
	}
	step := 0
	fails := 0
	call := func(label string, args map[string]any, want ...string) string {
		step++
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		started := time.Now()
		text, err := desktopRuntimeModule{}.InvokeTool(ctx, request, desktopToolName, args)
		plain, images := agentruntime.ExtractModelImages(text)
		for i, image := range images {
			ext := strings.TrimPrefix(image.MIME, "image/")
			raw, _ := base64.StdEncoding.DecodeString(image.Base64)
			name := fmt.Sprintf("%02d-%s-%d.%s", step, regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(label), "-"), i, ext)
			_ = os.WriteFile(filepath.Join(out, name), raw, 0o644)
		}
		status := "OK"
		if err != nil {
			status = "ERR " + err.Error()
		}
		for _, w := range want {
			if !strings.Contains(plain, w) {
				status = "MISSING " + w + " / " + status
			}
		}
		if status != "OK" {
			fails++
		}
		short := strings.ReplaceAll(plain, "\n", " | ")
		if len(short) > 420 {
			short = short[:420] + "..."
		}
		t.Logf("[%02d] %-28s %s (%d model image(s), %s)\n      %s", step, label, status, len(images), time.Since(started).Round(time.Millisecond), short)
		return plain
	}

	call("screen_info", map[string]any{"action": "screen_info"}, "width")
	call("screenshot scaled jpeg", map[string]any{"action": "screenshot", "max_width": 800, "format": "jpeg"}, "image 800x")
	call("app_open mousepad", map[string]any{"action": "app_open", "program": "mousepad"})
	call("wait window Mousepad", map[string]any{"action": "wait", "for": "window", "title": "Mousepad", "timeout": 15})
	call("window maximize", map[string]any{"action": "window", "do": "maximize", "title": "Mousepad"})
	call("window_list", map[string]any{"action": "window_list"}, "Mousepad")
	call("computer paste chinese", map[string]any{"action": "computer", "actions": []any{
		map[string]any{"action": "left_click", "coordinate": []any{400, 250}},
		map[string]any{"action": "paste", "text": "你好，桌面机器人\n第二行 line two\n第三行"},
	}}, "paste")
	call("computer double/drag/scroll/right", map[string]any{"action": "computer", "actions": []any{
		map[string]any{"action": "double_click", "coordinate": []any{60, 72}},
		map[string]any{"action": "left_click_drag", "start_coordinate": []any{20, 72}, "coordinate": []any{200, 100}},
		map[string]any{"action": "scroll", "coordinate": []any{400, 250}, "scroll_direction": "down", "scroll_amount": 2},
		map[string]any{"action": "right_click", "coordinate": []any{300, 250}},
	}})
	call("computer escape", map[string]any{"action": "computer", "actions": []any{map[string]any{"action": "key", "text": "Escape"}}})
	call("clipboard_set", map[string]any{"action": "clipboard_set", "text": "剪贴板 E2E ✓"})
	call("clipboard_get", map[string]any{"action": "clipboard_get"}, "剪贴板 E2E")
	call("app_open thunar", map[string]any{"action": "app_open", "program": "thunar"})
	call("wait window Thunar", map[string]any{"action": "wait", "for": "window", "title": "Thunar", "timeout": 15})
	probe := call("app_probe marks", map[string]any{"action": "app_probe", "marks": true}, "[1]")
	id := ""
	if m := regexp.MustCompile(`\[(\d+)\] menu "文件`).FindStringSubmatch(probe); m != nil {
		id = m[1]
	} else if m := regexp.MustCompile(`\[(\d+)\] (push button|menu)`).FindStringSubmatch(probe); m != nil {
		id = m[1]
	}
	if id == "" {
		t.Logf("no clickable element found in probe")
		fails++
	} else {
		n := 0
		fmt.Sscan(id, &n)
		call("computer click by id "+id, map[string]any{"action": "computer", "actions": []any{map[string]any{"action": "left_click", "id": n}}})
		call("screenshot after id click", map[string]any{"action": "screenshot", "max_width": 1024, "format": "png"})
		call("computer escape 2", map[string]any{"action": "computer", "actions": []any{map[string]any{"action": "key", "text": "Escape"}}})
	}
	call("wait change", map[string]any{"action": "wait", "for": "change", "timeout": 2})
	call("screenshot final webp", map[string]any{"action": "screenshot", "max_width": 1280, "format": "webp", "cursor": true})
	if fails > 0 {
		t.Errorf("%d step(s) failed", fails)
	}
}
