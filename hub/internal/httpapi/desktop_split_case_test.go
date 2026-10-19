package httpapi

import (
	"strings"
	"testing"
)

func TestDesktopBotSplitCases(t *testing.T) {
	t.Run("tool_rpc_has_no_transcript", func(t *testing.T) {
		for _, key := range []string{"messages", "memory", "send_to_user", "conversation", "history", "transcript"} {
			raw := []byte(`{"run_id":"run-1","tool_call_id":"call-1","tenant":"tenant","user":"alice","action":"click","args":{"` + key + `":"hello"}}`)
			if _, err := parseDesktopToolCall(raw); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("accepted %s: %v", key, err)
			}
			top := []byte(`{"run_id":"run-1","tool_call_id":"call-1","tenant":"tenant","user":"alice","action":"click","` + key + `":[]}`)
			if _, err := parseDesktopToolCall(top); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("accepted top-level %s: %v", key, err)
			}
		}
		if _, err := parseDesktopToolCall([]byte(`{"run_id":"run-1","tool_call_id":"call-1","tenant":"tenant","user":"alice","action":"chat"}`)); err == nil {
			t.Fatal("chat is a desktop action")
		}
		allowed := map[string]bool{
			"screenshot": true, "file": true, "bash": true, "http": true,
			"install": true, "open": true, "app": true, "ctl": true,
			"session": true, "stop": true,
		}
		for action, route := range desktopToolRoutes {
			if !allowed[route] {
				t.Fatalf("%s -> %s is not an existing desktopd call", action, route)
			}
			switch action {
			case "chat", "messages", "model", "send_to_user", "conversation", "history", "transcript":
				t.Fatalf("action %s is a model loop", action)
			}
			switch route {
			case "chat", "messages", "model":
				t.Fatalf("route %s is a model loop", route)
			}
		}
		if desktopToolRoutes["script"] != "ctl" {
			t.Fatalf("script -> %s", desktopToolRoutes["script"])
		}
		if desktopToolRoutes["bash"] != "bash" || desktopToolRoutes["click"] != "ctl" || desktopToolRoutes["screenshot"] != "screenshot" {
			t.Fatalf("routes = %#v", desktopToolRoutes)
		}
		if _, err := desktopToolRoute("model"); err == nil {
			t.Fatal("model is a desktop route")
		}
	})
}
