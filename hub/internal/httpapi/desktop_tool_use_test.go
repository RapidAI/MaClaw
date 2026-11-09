package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
)

// Only steps that drive the screen count as the bot using the desktop.
// Headless bash/file/http/install and failed steps do not; stop forgets.
func TestNoteDesktopToolUseCountsScreenStepsOnly(t *testing.T) {
	pool := desktoppool.New(nil, nil)
	call := func(action string) desktopToolCall {
		return desktopToolCall{RunID: "run_1", ToolCallID: "tc", Tenant: "tenant-a", User: "alice", Action: action}
	}
	for action, route := range desktopToolRoutes {
		switch route {
		case "bash", "file", "http", "install":
			noteDesktopToolUse(pool, call(action), nil)
			if at, _ := pool.BotGUIUse("tenant-a", "alice"); !at.IsZero() {
				t.Fatalf("headless %s (%s) counted as desktop use", action, route)
			}
		}
	}
	noteDesktopToolUse(pool, call("click"), errors.New("failed"))
	if at, _ := pool.BotGUIUse("tenant-a", "alice"); !at.IsZero() {
		t.Fatal("a failed step counted as desktop use")
	}
	for _, action := range []string{"screenshot", "computer", "app_open", "click", "type", "navigate"} {
		pool.ForgetBotGUIUse("tenant-a", "alice")
		noteDesktopToolUse(pool, call(action), nil)
		if at, _ := pool.BotGUIUse("tenant-a", "alice"); at.IsZero() {
			t.Fatalf("screen step %s did not count as desktop use", action)
		}
	}
	for action, route := range desktopToolRoutes {
		if route == "stop" {
			noteDesktopToolUse(pool, call(action), nil)
			if at, _ := pool.BotGUIUse("tenant-a", "alice"); !at.IsZero() {
				t.Fatal("stop kept the desktop-use record")
			}
			break
		}
	}
}

// MaClawSrv's production screen path goes through Hub's session, app, ctl
// and screenshot routes (browser work then goes straight over CDP). Each
// successful one counts as the bot on the screen; shell work does not, and
// a stop forgets the record.
func TestDesktopServiceScreenRoutesNoteBotUse(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "desktop-api-token")
	docker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/desktops/ensure":
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case "/v1/desktops/session":
			_, _ = w.Write([]byte(`{"cdp_url":"http://dockerd.example:9222","display":":1","novnc_url":"http://dockerd.example:6081/vnc.html"}`))
		case "/v1/desktops/ctl":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/v1/desktops/bash":
			_, _ = w.Write([]byte(`{"output":"ok"}`))
		case "/v1/desktops/stop":
			_, _ = w.Write([]byte(`{"status":"stopped"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(docker.Close)
	pool := desktopPoolFake(t, newDesktopSettingsMem(), docker.URL)
	post := func(h http.HandlerFunc, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer desktop-api-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if code := post(PostDesktopBashHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","command":"ls"}`); code != http.StatusOK {
		t.Fatalf("bash status=%d", code)
	}
	if at, _ := pool.BotGUIUse("tenant-a", "alice"); !at.IsZero() {
		t.Fatal("a shell step counted as the bot on the screen")
	}
	if code := post(PostDesktopSessionHandler(pool, nil), `{"tenant_id":"tenant-a","user_id":"alice"}`); code != http.StatusOK {
		t.Fatalf("session status=%d", code)
	}
	at, novnc := pool.BotGUIUse("tenant-a", "alice")
	if at.IsZero() || novnc != "http://dockerd.example:6081/vnc.html" {
		t.Fatalf("session wake not noted: at=%v novnc=%q", at, novnc)
	}
	pool.ForgetBotGUIUse("tenant-a", "alice")
	if code := post(PostDesktopCtlHandler(pool), `{"tenant_id":"tenant-a","user_id":"alice","request":{"op":"windows"}}`); code != http.StatusOK {
		t.Fatalf("ctl status=%d", code)
	}
	if at, _ := pool.BotGUIUse("tenant-a", "alice"); at.IsZero() {
		t.Fatal("ctl step not noted")
	}
	if _, err := pool.StopDesktop(context.Background(), "tenant-a", "alice"); err != nil {
		t.Fatal(err)
	}
	if at, _ := pool.BotGUIUse("tenant-a", "alice"); !at.IsZero() {
		t.Fatal("stop kept the bot-on-screen record")
	}
}
