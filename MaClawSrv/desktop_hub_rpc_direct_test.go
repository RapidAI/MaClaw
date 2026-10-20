package main

import "testing"

// Screen actions must not take the Hub one-call shortcut: their guards
// (window raise, paste guard, captcha read, browser session, display) run
// on the worker's guarded path.
func TestDesktopHubRPCDirectKeepsGuardedActionsLocal(t *testing.T) {
	for _, action := range []string{"app_open", "app_run", "computer", "click", "type", "navigate", "script", "probe", "screenshot", "skill_run", "app_list"} {
		if desktopHubRPCDirect(action) {
			t.Fatalf("%s took the Hub shortcut and would skip its guards", action)
		}
	}
	for _, action := range []string{"file_read", "file_write", "bash", "install"} {
		if !desktopHubRPCDirect(action) {
			t.Fatalf("%s should stay a single Hub call", action)
		}
	}
}
