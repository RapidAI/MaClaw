package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotEventCases(t *testing.T) {
	t.Setenv(desktopHubURLEnv, "")
	t.Setenv(desktopHubTokenEnv, "")

	var sessionCalls int
	var fileCalls int
	previousSession := desktopRemoteSession
	previousFile := desktopRemoteFile
	previousShot := desktopRemoteScreenshot
	previousOpen := desktopOpenBrowser
	previousFocus := desktopFocusVisiblePage
	previousPages := desktopOpenPages
	shot := desktopTestPNG(t, 8, 8, false)
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		sessionCalls++
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		fileCalls++
		return "ok", nil
	}
	desktopRemoteScreenshot = func(context.Context, string, string, string) ([]byte, error) {
		return shot, nil
	}
	desktopOpenBrowser = func(*desktopBinding, agentruntime.Scope, string, string) (*browser.BrowserAgentSession, error) {
		return &browser.BrowserAgentSession{}, nil
	}
	desktopFocusVisiblePage = func(*browser.BrowserAgentSession) error { return nil }
	desktopOpenPages = func(string, string) string { return "" }
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteFile = previousFile
		desktopRemoteScreenshot = previousShot
		desktopOpenBrowser = previousOpen
		desktopFocusVisiblePage = previousFocus
		desktopOpenPages = previousPages
	})

	t.Run("first_tool_ensures_once", func(t *testing.T) {
		scope := agentruntime.Scope{TenantID: "tenant-event", UserID: "alice", InstanceID: "event-bot", RunID: "round-shot"}
		sessionCalls = 0
		if _, err := operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("first shot calls=%d err=%v", sessionCalls, err)
		}
		if _, err := operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("second shot calls=%d err=%v", sessionCalls, err)
		}

		plan := context.WithValue(context.Background(), desktopPhaseKey{}, "plan")
		_, err := operateDesktop(plan, scope, map[string]any{"action": "file_write", "path": "/home/desktop/notes.txt", "content": "x"})
		if err == nil || !strings.Contains(err.Error(), desktopbot.PlanPhaseNextStep) || sessionCalls != 1 {
			t.Fatalf("plan woke the machine calls=%d err=%v", sessionCalls, err)
		}
		_, err = operateDesktop(context.Background(), scope, map[string]any{
			"action": "file_write", "path": "/home/desktop/notes.txt", "content": strings.Repeat("a", desktop.FileContentMax+1),
		})
		if err == nil || !strings.Contains(err.Error(), desktopbot.FileTooLarge) || sessionCalls != 1 {
			t.Fatalf("oversize woke the machine calls=%d err=%v", sessionCalls, err)
		}

		scope.RunID = "round-skill"
		before := sessionCalls
		text, err := operateDesktop(context.Background(), scope, map[string]any{"action": "skill_list"})
		if err != nil || sessionCalls != before || !strings.Contains(text, "browser skills") {
			t.Fatalf("skill_list calls=%d text=%q err=%v", sessionCalls, text, err)
		}

		// The file call is the wake when no session stub is installed.
		desktopRemoteSession = nil
		sessionCalls = 0
		fileCalls = 0
		scope.RunID = "round-file"
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "file_read", "path": "/home/desktop/notes.txt"}); err != nil || sessionCalls != 0 || fileCalls != 1 {
			t.Fatalf("file wake calls=%d files=%d err=%v", sessionCalls, fileCalls, err)
		}
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "file_read", "path": "/home/desktop/notes.txt"}); err != nil || sessionCalls != 0 || fileCalls != 2 {
			t.Fatalf("second file calls=%d files=%d err=%v", sessionCalls, fileCalls, err)
		}
		desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
			sessionCalls++
			return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
		}
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("shot after file calls=%d err=%v", sessionCalls, err)
		}

		// A session stub and a file in the same round share one Ensure.
		sessionCalls = 0
		fileCalls = 0
		scope.RunID = "round-both"
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "file_read", "path": "/home/desktop/notes.txt"}); err != nil || sessionCalls != 1 || fileCalls != 1 {
			t.Fatalf("shared file calls=%d files=%d err=%v", sessionCalls, fileCalls, err)
		}
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("shared shot calls=%d err=%v", sessionCalls, err)
		}

		sessionCalls = 0
		loose := agentruntime.Scope{TenantID: "tenant-event", UserID: "alice", InstanceID: "event-bot"}
		if _, err = operateDesktop(context.Background(), loose, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("unkeyed first calls=%d err=%v", sessionCalls, err)
		}
		if _, err = operateDesktop(context.Background(), loose, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 2 {
			t.Fatalf("unkeyed second calls=%d err=%v", sessionCalls, err)
		}
	})

	t.Run("round_end_drops_binding", func(t *testing.T) {
		const tenantID, userID, runID = "tenant-bind", "alice-bind", "run-bind"
		key, err := desktop.UserKey(tenantID, userID)
		if err != nil {
			t.Fatal(err)
		}
		desktopBindings.Store(key, &desktopBinding{session: &browser.BrowserAgentSession{}, addr: "http://10.0.0.8:19020"})
		desktopRoundWakes.Store(runID, &desktopRoundWake{woken: true, have: true, endpoint: desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}})
		finishDesktopRound(tenantID, userID, runID)
		if _, ok := desktopBindings.Load(key); ok {
			t.Fatal("browser session stayed after the round")
		}
		if _, ok := desktopRoundWakes.Load(runID); ok {
			t.Fatal("wake stayed after the round")
		}

		sessionCalls = 0
		scope := agentruntime.Scope{TenantID: tenantID, UserID: userID, InstanceID: "bind-bot", RunID: "run-after"}
		if _, err = operateDesktop(context.Background(), scope, map[string]any{"action": "screenshot"}); err != nil || sessionCalls != 1 {
			t.Fatalf("next round calls=%d err=%v", sessionCalls, err)
		}
		finishDesktopRound(tenantID, userID, "run-after")
		if _, ok := desktopBindings.Load(key); ok {
			t.Fatal("the next round left a browser session")
		}
	})
}
