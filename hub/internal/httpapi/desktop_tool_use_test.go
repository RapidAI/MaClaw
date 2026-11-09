package httpapi

import (
	"errors"
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
