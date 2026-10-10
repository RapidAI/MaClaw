package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopModuleHiddenWithoutEndpoint(t *testing.T) {
	t.Setenv(desktopCDPEnv, "")
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools=%v, want none", tools)
	}
	if got := srvDesktopRuntimeModules(); got != nil {
		t.Fatalf("modules=%v, want nil", got)
	}
}

func TestDesktopModuleAdvertisesToolWhenConfigured(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	if desktopCDPAddr() != "http://127.0.0.1:9222" {
		t.Fatalf("addr=%q", desktopCDPAddr())
	}
	plain, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{})
	if err != nil || len(plain) != 0 {
		t.Fatalf("digital employee tools=%v err=%v", plain, err)
	}
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Instance: agentservice.Instance{Metadata: map[string]string{"hub_bot": "1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != desktopToolName {
		t.Fatalf("tools=%v", tools)
	}
	modules := srvDesktopRuntimeModules()
	if len(modules) != 1 || modules[0].Descriptor().ModuleID != desktopRuntimeModuleID || !modules[0].Descriptor().HeadlessSupport {
		t.Fatalf("modules=%v", modules)
	}
}

func TestDesktopPromptContinuesOnTheLoggedInSite(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	plain, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), agentruntime.TurnRequest{})
	if err != nil || plain != "" {
		t.Fatalf("digital employee prompt=%q err=%v", plain, err)
	}
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "original task") || !strings.Contains(prompt, "this site") || !strings.Contains(prompt, "If this site opens a window, continue there") {
		t.Fatalf("prompt does not continue on the logged-in site: %s", prompt)
	}
	if strings.Contains(prompt, "same-page sequence") {
		t.Fatal("prompt keeps the agent on one page after login")
	}
}

func TestOperateDesktopLiveNavigate(t *testing.T) {
	if os.Getenv("MACLAW_DESKTOP_LIVE") != "1" {
		t.Skip("set MACLAW_DESKTOP_LIVE=1 to drive the configured desktop")
	}
	if desktopCDPAddr() == "" {
		t.Fatal("MACLAW_DESKTOP_CDP is empty")
	}
	restore := stubLiveDesktop()
	defer restore()
	text, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "live", UserID: "live"}, map[string]any{
		"action": "navigate",
		"url":    "https://example.org",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "example.org") {
		t.Fatalf("navigate result=%q", text)
	}
}

func TestOperateDesktopLiveTaskRun(t *testing.T) {
	if os.Getenv("MACLAW_DESKTOP_LIVE") != "1" {
		t.Skip("set MACLAW_DESKTOP_LIVE=1 to drive the configured desktop")
	}
	if desktopCDPAddr() == "" {
		t.Fatal("MACLAW_DESKTOP_CDP is empty")
	}
	restore := stubLiveDesktop()
	defer restore()
	text, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "live", UserID: "live"}, map[string]any{
		"action": "task_run",
		"steps": []any{
			map[string]any{"action": "navigate", "params": map[string]any{"url": "https://example.org"}},
			map[string]any{"action": "wait", "params": map[string]any{"ms": "500"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "example.org") && !strings.Contains(lower, "status=completed") {
		t.Fatalf("task_run result=%q", text)
	}
}

func TestDesktopBrowserStepsRejectsPixelsAndEmptyBatch(t *testing.T) {
	if _, err := desktopBrowserSteps(nil); err == nil {
		t.Fatal("nil steps unexpectedly accepted")
	}
	_, err := desktopBrowserSteps([]any{map[string]any{"action": "click_at", "params": map[string]any{"x": "1"}}})
	if err == nil {
		t.Fatal("pixel step unexpectedly accepted")
	}
	steps, err := desktopBrowserSteps([]any{
		map[string]any{"action": "click", "params": map[string]any{"ref": "e3"}},
		map[string]any{"action": "type", "params": map[string]any{"text": "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 || steps[0].Params["ref"] != "e3" || steps[1].Params["text"] != "hello" {
		t.Fatalf("steps=%#v", steps)
	}
}

func TestDesktopAppStepsStayOnXdotoolArgs(t *testing.T) {
	steps, err := desktopAppSteps([]any{
		map[string]any{"action": "focus", "name": "gedit"},
		map[string]any{"action": "key", "key": "ctrl+s"},
		map[string]any{"action": "click", "x": 12, "y": 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if steps[0][0] != "search" || steps[1][1] != "ctrl+s" || steps[2][2] != "12" {
		t.Fatalf("steps=%#v", steps)
	}
	typed, err := desktopAppSteps([]any{
		map[string]any{"action": "type", "text": "hello"},
		map[string]any{"action": "key", "key": "Return"},
	})
	if err != nil {
		t.Fatal(err)
	}
	argv := strings.Join(desktopAppArgv(typed), " ")
	if argv != "type --delay 20 hello key Return" {
		t.Fatalf("argv=%s", argv)
	}
	if _, err := desktopAppSteps([]any{map[string]any{"action": "key", "key": "rm -rf"}}); err == nil {
		t.Fatal("unsafe key unexpectedly accepted")
	}
}

func TestOperateDesktopRejectsIncompleteActions(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")
	cases := []map[string]any{
		{"action": "fly"},
		{"action": "navigate"},
		{"action": "click"},
		{"action": "type"},
		{"action": "press"},
	}
	for _, args := range cases {
		if _, err := operateDesktop(context.Background(), agentruntime.Scope{}, args); err == nil {
			t.Fatalf("args=%v unexpectedly succeeded", args)
		}
	}
	t.Setenv(desktopCDPEnv, "")
	if _, err := operateDesktop(context.Background(), agentruntime.Scope{UserID: "user"}, map[string]any{"action": "probe"}); err == nil {
		t.Fatal("missing endpoint unexpectedly succeeded")
	}
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")
	if _, err := operateDesktop(context.Background(), agentruntime.Scope{}, map[string]any{"action": "probe"}); err == nil {
		t.Fatal("missing user unexpectedly succeeded")
	}
}

func TestDesktopUsersGetSeparateDesktops(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")
	previous := desktopEnsureFn
	desktopEnsureFn = func(userKey string) (desktopEndpoint, error) {
		switch userKey {
		case mustDesktopUserKey(t, "tenant", "alice"):
			return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
		case mustDesktopUserKey(t, "tenant", "bob"):
			return desktopEndpoint{CDP: "http://10.0.0.8:19021", Display: ":21"}, nil
		default:
			t.Fatalf("unexpected user key %s", userKey)
			return desktopEndpoint{}, nil
		}
	}
	t.Cleanup(func() { desktopEnsureFn = previous })
	var seen []string
	desktopAppRunner = func(display string, _ ...string) (string, error) {
		seen = append(seen, display)
		return "Chromium", nil
	}
	t.Cleanup(func() { desktopAppRunner = nil })
	alice := agentruntime.Scope{TenantID: "tenant", UserID: "alice"}
	bob := agentruntime.Scope{TenantID: "tenant", UserID: "bob"}
	if _, err := operateDesktop(context.Background(), alice, map[string]any{"action": "app_list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := operateDesktop(context.Background(), alice, map[string]any{"action": "app_list"}); err != nil {
		t.Fatal(err)
	}
	if _, err := operateDesktop(context.Background(), bob, map[string]any{"action": "app_list"}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[0] != ":20" || seen[1] != ":20" || seen[2] != ":21" {
		t.Fatalf("displays=%v", seen)
	}
	if mustDesktopUserKey(t, "tenant", "alice") == mustDesktopUserKey(t, "tenant", "bob") {
		t.Fatal("different users resolved to one desktop")
	}
	if mustDesktopUserKey(t, "other", "alice") == mustDesktopUserKey(t, "tenant", "alice") {
		t.Fatal("same user id in another tenant resolved to the same desktop")
	}
}

func TestBotAgentInstancesShareOneCloudDesktop(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9")
	previous := desktopEnsureFn
	var keys []string
	desktopEnsureFn = func(userKey string) (desktopEndpoint, error) {
		keys = append(keys, userKey)
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	t.Cleanup(func() { desktopEnsureFn = previous })
	desktopAppRunner = func(display string, _ ...string) (string, error) {
		return display, nil
	}
	t.Cleanup(func() { desktopAppRunner = nil })
	scopes := []agentruntime.Scope{
		{TenantID: "tenant", UserID: "alice", InstanceID: "bot-1"},
		{TenantID: "tenant", UserID: "alice", InstanceID: "bot-2"},
		{TenantID: "tenant", UserID: "alice", InstanceID: "inst_duty"},
	}
	for _, scope := range scopes {
		if _, err := operateDesktop(context.Background(), scope, map[string]any{"action": "app_list"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) != 3 || keys[0] != keys[1] || keys[1] != keys[2] {
		t.Fatalf("bot instances did not share one desktop: %v", keys)
	}
	bob := mustDesktopUserKey(t, "tenant", "bob")
	if bob == keys[0] {
		t.Fatal("another user's bot resolved to the same desktop")
	}
}

func TestCaptchaPausesTheDesktopTurn(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-a"}
	t.Cleanup(func() {
		desktopHandoff.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
		desktopResumeFocus.Delete(desktopResumeKey(scope))
		desktopResumeUser.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopLoginDocument.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopRunsMu.Lock()
		delete(desktopHolds, desktopRunKey(scope.TenantID, scope.UserID))
		desktopRunsMu.Unlock()
	})
	text, err := finishDesktopAction(scope, "status=ask\nhandoff=captcha", fmt.Errorf("still running"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.ParseAskUserResult(text); !ok {
		t.Fatalf("turn did not pause: %q", text)
	}
	if takeDesktopHandoff(scope.TenantID, scope.UserID, "bot-b") {
		t.Fatal("another bot took the login handoff")
	}
	if !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("handoff was not reported")
	}
	login, err := finishDesktopAction(scope, "probed page Sign in (https://ex/login), interactive elements: 2; page flags: login_wall", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.ParseAskUserResult(login); !ok || !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("login page did not hand the desktop to the user")
	}
	mentioned, err := finishDesktopAction(scope, "the article explains how to login", nil)
	if err != nil || mentioned != "the article explains how to login" {
		t.Fatalf("mentioned=%q err=%v", mentioned, err)
	}
	longPage := strings.Repeat("button ", 4000) + "page flags: login_wall"
	trimmed := trimDesktopText(longPage)
	if !strings.Contains(trimmed, "page flags: login_wall") {
		t.Fatal("long observation dropped the login flag")
	}
	longLogin, err := finishDesktopAction(scope, trimmed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.ParseAskUserResult(longLogin); !ok || !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("long login page did not hand the desktop to the user")
	}
	navigated, err := desktopActionText(&browser.BrowserActionResult{
		Display: "navigated to https://ex/login",
		Data:    map[string]interface{}{"page_flags": browser.BrowserPageFlags{LoginWall: true}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	navigatedHand, err := finishDesktopAction(scope, navigated, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := agent.ParseAskUserResult(navigatedHand); !ok || !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("navigate onto a login page did not hand the desktop to the user")
	}
	other := scope
	other.InstanceID = "bot-b"
	if consumeDesktopResume(other) {
		t.Fatal("another bot of this user moved the page during login")
	}
	if !consumeDesktopResume(scope) || consumeDesktopResume(scope) {
		t.Fatal("the bot that handed off the desktop did not follow the logged-in page")
	}
	plain, err := finishDesktopAction(scope, "status=ok", nil)
	if err != nil || plain != "status=ok" {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
}

func TestScheduledTaskDoesNotHoldTheDesktopForLogin(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "sched"}
	t.Cleanup(func() {
		desktopUnattendedInstances.Delete(scope.InstanceID)
		desktopHandoff.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
		desktopRunsMu.Lock()
		delete(desktopHolds, desktopRunKey(scope.TenantID, scope.UserID))
		desktopRunsMu.Unlock()
	})
	end := markDesktopUnattended(scope.InstanceID)
	defer end()
	text, err := finishDesktopAction(scope, "page flags: login_wall", nil)
	if err == nil || !strings.Contains(text, "自动任务") {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("scheduled task handed the desktop to a person")
	}
}

func TestOtherBotDoesNotDriveTheDesktopDuringLogin(t *testing.T) {
	key := desktopRunKey("tenant", "alice")
	desktopPersonInstance.Store(key, "bot-a")
	t.Cleanup(func() { desktopPersonInstance.Delete(key) })
	_, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-b"}, map[string]any{"action": "probe"})
	if err == nil || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("err=%v", err)
	}
}

func TestDeletedBotReleasesTheSharedDesktop(t *testing.T) {
	key := desktopRunKey("tenant", "alice")
	desktopPersonInstance.Store(key, "bot-a")
	desktopRunsMu.Lock()
	desktopHolds[key] = 1
	desktopRunsMu.Unlock()
	previousSession := desktopRemoteSession
	previousApp := desktopRemoteApp
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteApp = func(context.Context, string, string, string, []string) (string, error) {
		return "Notes", nil
	}
	t.Cleanup(func() {
		desktopPersonInstance.Delete(key)
		desktopRunsMu.Lock()
		delete(desktopHolds, key)
		desktopRunsMu.Unlock()
		desktopRemoteSession = previousSession
		desktopRemoteApp = previousApp
	})
	releaseDesktopPerson("bot-a")
	text, err := operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-b"}, map[string]any{"action": "app_list"})
	if err != nil || text != "Notes" {
		t.Fatalf("deleted bot still blocked the shared desktop: text=%q err=%v", text, err)
	}
	desktopPersonInstance.Store(key, "bot-b")
	releaseDesktopPerson("bot-a")
	_, err = operateDesktop(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-c"}, map[string]any{"action": "probe"})
	if err == nil || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("a different bot's login was cleared: %v", err)
	}
}

func TestDeletedBotStopsTheDesktopWhenNothingElseIsUsingIt(t *testing.T) {
	key := desktopRunKey("tenant", "alice")
	var stopped int
	previousStop := desktopRemoteStop
	desktopRemoteStop = func(context.Context, string, string) error {
		stopped++
		return nil
	}
	t.Cleanup(func() {
		desktopRemoteStop = previousStop
		desktopPersonInstance.Delete(key)
		desktopRunsMu.Lock()
		delete(desktopRuns, key)
		delete(desktopHolds, key)
		desktopRunsMu.Unlock()
	})
	desktopPersonInstance.Store(key, "bot-a")
	desktopRunsMu.Lock()
	desktopRuns[key] = 1
	desktopRunsMu.Unlock()
	releaseDesktopPerson("bot-a")
	if stopped != 0 {
		t.Fatalf("deleting the login bot stopped a desktop another run is using: %d", stopped)
	}
	desktopPersonInstance.Store(key, "bot-a")
	desktopRunsMu.Lock()
	desktopRuns[key] = 0
	desktopRunsMu.Unlock()
	releaseDesktopPerson("bot-a")
	if stopped != 1 {
		t.Fatalf("an idle login desktop stayed up after its bot was deleted: %d", stopped)
	}
}

func TestAppRunUsesOneXdotoolCall(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Notes"},
			map[string]any{"action": "type", "text": "int main() {\nreturn 0;\n}"},
			map[string]any{"action": "key", "key": "Return"},
		},
	}, nil)
	if err != nil || text != "ok" {
		t.Fatalf("calls=%d text=%q err=%v argv=%v", len(calls), text, err, calls)
	}
	want := []string{
		"type --delay 20 --args 1 -- int main() { key Return",
		"type --delay 20 --args 1 -- return 0; key Return",
		"type --delay 20 --args 1 -- }",
		"key Return",
	}
	// keyup is its own call. The Whisker Menu is unmapped next, then focus.
	// Nothing sits between focus and the first type: a keyup there can
	// move the window. keyup is not a prefix on the type argv, because
	// xdotool stops the rest of one process when a keysym fails.
	if len(calls) != len(want)+3 || strings.Join(calls[0], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[1], " ") != strings.Join(desktopWhiskerDismiss, " ") {
		t.Fatalf("calls=%v", calls)
	}
	focus := strings.Join(calls[2], " ")
	if strings.Contains(focus, "type") || strings.Contains(focus, "keyup") || !strings.Contains(focus, "windowactivate --sync") {
		t.Fatalf("focus shared a command with type: %v", calls[2])
	}
	for i, line := range want {
		got := strings.Join(calls[i+3], " ")
		if got != line || strings.Contains(got, "search") || strings.Contains(got, "keyup") {
			t.Fatalf("call %d=%v", i+3, calls[i+3])
		}
	}
	// The password read raises the browser, so focus is sent after the
	// read and type follows with an empty window stack.
	var guarded [][]string
	reads := 0
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		guarded = append(guarded, append([]string(nil), args...))
		return "ok", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Notes"},
			map[string]any{"action": "type", "text": "hi\nthere"},
			map[string]any{"action": "key", "key": "Return"},
		},
	}, func() error {
		reads++
		if len(guarded) != 0 {
			return fmt.Errorf("focus ran before the password read")
		}
		return nil
	})
	if err != nil || text != "ok" || reads != 1 || len(guarded) != 6 {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, guarded)
	}
	if strings.Join(guarded[0], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(guarded[1], " ") != strings.Join(desktopWhiskerDismiss, " ") {
		t.Fatalf("release=%v", guarded[:2])
	}
	focus = strings.Join(guarded[2], " ")
	if strings.Contains(focus, "type") || strings.Contains(focus, "keyup") || !strings.Contains(focus, "windowactivate --sync") {
		t.Fatalf("focus was chained with type: %v", guarded[2])
	}
	if strings.Join(guarded[3], " ") != "type --delay 20 --args 1 -- hi key Return" || strings.Join(guarded[4], " ") != "type --delay 20 --args 1 -- there" || strings.Join(guarded[5], " ") != "key Return" {
		t.Fatalf("keys=%v", guarded)
	}
	guarded = nil
	reads = 0
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 10, "y": 20},
			map[string]any{"action": "type", "text": "hi"},
		},
	}, func() error {
		reads++
		// The fake answers "ok", which is not a window id. The click records
		// the window before the modifier release, unmaps the menu, then the
		// text step records the window again before the read. Nothing is typed yet.
		if reads == 1 && (len(guarded) != 5 || guarded[0][0] != "getactivewindow" || guarded[1][0] != "keyup" || strings.Join(guarded[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || !strings.Contains(strings.Join(guarded[3], " "), "click") || strings.Join(guarded[4], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Contains(strings.Join(guarded[3], " "), "type")) {
			return fmt.Errorf("click was not delivered before the read: %v", guarded)
		}
		return nil
	})
	if err != nil || text != "ok" || reads != 1 || len(guarded) != 6 {
		t.Fatalf("click reads=%d text=%q err=%v calls=%v", reads, text, err, guarded)
	}
	if strings.Join(guarded[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(guarded[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(guarded[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(guarded[3], " ") != "mousemove --sync 10 20 click 1" || strings.Join(guarded[4], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(guarded[5], " ") != "type --delay 20 --args 1 -- hi" {
		t.Fatalf("click then type=%v", guarded)
	}
	for _, call := range guarded {
		if len(call) > 0 && call[0] == "windowactivate" {
			t.Fatalf("non-numeric window id was activated: %v", guarded)
		}
	}
	calls = nil
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "type", "text": "hello\n    return 0;\n   \n-Wall"},
		},
	}, nil)
	if err != nil || text != "ok" || len(calls) != 7 {
		t.Fatalf("blank line calls=%d text=%q err=%v argv=%v", len(calls), text, err, calls)
	}
	if calls[0][0] != "getactivewindow" || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "type --delay 20 --args 1 -- hello key Return" || strings.Join(calls[5], " ") != "key Return" {
		t.Fatalf("blank line=%v", calls)
	}
	if calls[4][len(calls[4])-3] != "    return 0;" || calls[4][len(calls[4])-2] != "key" {
		t.Fatalf("indent=%v", calls[4])
	}
	if got := calls[6]; len(got) != 7 || got[0] != "type" || got[5] != "--" || got[6] != "-Wall" {
		t.Fatalf("dash line=%v", calls[6])
	}
}

func TestAppRunClickThenTypeRestoresTheClickedWindow(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609\n", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		if len(args) > 0 && args[0] == "type" {
			return "", nil
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	reads := 0
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 10, "y": 20},
			map[string]any{"action": "key", "key": "Return"},
			map[string]any{"action": "type", "text": "hi\nthere"},
		},
	}, func() error {
		reads++
		// The click restores its window before the click. The text step
		// records the window after that click and before this read. The
		// post-click activate has not run yet.
		if reads == 1 && (len(calls) != 7 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || !strings.Contains(strings.Join(calls[4], " "), "click") || calls[5][0] != "key" || calls[6][0] != "getactivewindow") {
			return fmt.Errorf("window was read after the browser could raise: %v", calls)
		}
		activates := 0
		for _, call := range calls {
			if len(call) > 0 && call[0] == "type" {
				return fmt.Errorf("text ran before the password read: %v", calls)
			}
			if len(call) > 0 && call[0] == "windowactivate" {
				activates++
			}
		}
		if activates != 1 {
			return fmt.Errorf("post-click activate ran before the read: %v", calls)
		}
		return nil
	})
	if err != nil || text != "ok" || reads != 1 || len(calls) != 10 {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "mousemove --sync 10 20 click 1" || strings.Join(calls[5], " ") != "key Return" || strings.Join(calls[6], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[7], " ") != "windowactivate --sync 8388609" || strings.Join(calls[8], " ") != "type --delay 20 --args 1 -- hi key Return" || strings.Join(calls[9], " ") != "type --delay 20 --args 1 -- there" {
		t.Fatalf("restore=%v", calls)
	}
	// The next text step reads again. The first line may have moved the
	// focus, and this step's password read can raise Chromium once more.
	calls = nil
	reads = 0
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 4, "y": 5},
			map[string]any{"action": "type", "text": "a"},
			map[string]any{"action": "key", "key": "b"},
		},
	}, func() error {
		reads++
		if reads == 2 {
			if len(calls) != 9 || calls[7][0] != "type" || calls[8][0] != "getactivewindow" {
				return fmt.Errorf("second read saw the wrong window: %v", calls)
			}
		}
		return nil
	})
	if err != nil || text != "ok" || reads != 2 || len(calls) != 11 {
		t.Fatalf("second text reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "mousemove --sync 4 5 click 1" || strings.Join(calls[5], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[6], " ") != "windowactivate --sync 8388609" || strings.Join(calls[7], " ") != "type --delay 20 --args 1 -- a" || strings.Join(calls[8], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[9], " ") != "windowactivate --sync 8388609" || strings.Join(calls[10], " ") != "key b" {
		t.Fatalf("second text=%v", calls)
	}
	// A missing id does not cancel the type. The click already landed.
	calls = nil
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "", fmt.Errorf("exit status 1")
		}
		return "typed", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 1, "y": 2},
			map[string]any{"action": "type", "text": "hi"},
		},
	}, func() error { return nil })
	if err != nil || text != "typed" || len(calls) != 6 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || !strings.Contains(strings.Join(calls[3], " "), "click") || calls[4][0] != "getactivewindow" || calls[5][0] != "type" {
		t.Fatalf("text=%q err=%v calls=%v", text, err, calls)
	}
	for _, call := range calls {
		if len(call) > 0 && call[0] == "windowactivate" {
			t.Fatalf("failed window read was activated: %v", calls)
		}
	}
}

func TestAppRunTypeWithoutAClickRestoresTheFocusedWindow(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609\n", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		if len(args) > 0 && args[0] == "type" {
			return "", nil
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	reads := 0
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "type", "text": "pwd"},
		},
	}, func() error {
		reads++
		if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") {
			return fmt.Errorf("window was read after the browser could raise: %v", calls)
		}
		return nil
	})
	if err != nil || text != "" || reads != 1 || len(calls) != 5 {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "type --delay 20 --args 1 -- pwd" {
		t.Fatalf("type only=%v", calls)
	}
}

func TestDesktopWindowIsBrowser(t *testing.T) {
	for _, class := range []string{"Chromium", "chromium", "google-chrome", "Firefox", "microsoft-edge"} {
		if !desktopWindowIsBrowser(class) {
			t.Fatalf("%s was not a browser", class)
		}
	}
	for _, class := range []string{"", "Xfce4-terminal", "xfce4-terminal", "XTerm", "Mousepad", "Terminal"} {
		if desktopWindowIsBrowser(class) {
			t.Fatalf("%s was treated as a browser", class)
		}
	}
}

// A dead browser debug socket used to cancel every app_run type, including
// a command aimed at the terminal the person is watching. The class of that
// window decides. The command and its Enter still go to xdotool.
func TestAppRunTypesIntoATerminalWhenTheBrowserSocketIsDown(t *testing.T) {
	const command = "echo hi"
	browserDown := fmt.Errorf("desktop browser: discover targets: unexpected HTTP 500")
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "getactivewindow" && args[1] == "getwindowname" {
			return "8388609\nTerminal 终端 -", nil
		}
		if len(args) >= 2 && args[0] == "getactivewindow" && args[1] == "getwindowclassname" {
			return "Xfce4-terminal", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		if len(args) >= 4 && args[0] == "search" && args[3] == desktopWhiskerMenuName {
			return "", fmt.Errorf("exit status 1")
		}
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	reads := 0
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Terminal 终端 -"},
			map[string]any{"action": "type", "text": command + "\n"},
		},
	}, func() error {
		reads++
		if len(calls) != 0 {
			return fmt.Errorf("xdotool ran before the browser read: %v", calls)
		}
		return browserDown
	})
	if err != nil || reads != 1 || text != "" {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if len(calls) != 5 || strings.Join(calls[0], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[1], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[2], " ") != "search --onlyvisible --name Terminal 终端 - windowactivate --sync" || strings.Join(calls[3], " ") != strings.Join(desktopWindowClassRead, " ") || strings.Join(calls[4], " ") != "type --delay 20 --args 1 -- echo hi key Return" {
		t.Fatalf("named terminal=%v", calls)
	}
	calls = nil
	reads = 0
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "type", "text": "pwd\n"},
		},
	}, func() error {
		reads++
		if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") {
			return fmt.Errorf("window was read after the browser could raise: %v", calls)
		}
		return browserDown
	})
	if err != nil || reads != 1 || text != "" || len(calls) != 6 {
		t.Fatalf("focused reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != strings.Join(desktopWindowClassRead, " ") || strings.Join(calls[5], " ") != "type --delay 20 --args 1 -- pwd key Return" {
		t.Fatalf("focused terminal=%v", calls)
	}
	// A password field in the browser is not the terminal. The command still runs.
	calls = nil
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Terminal 终端 -"},
			map[string]any{"action": "type", "text": command + "\n"},
		},
	}, func() error { return fmt.Errorf("not_password_field") })
	if err != nil || text != "" || !containsArg(calls[len(calls)-1], command) || !containsArg(calls[len(calls)-1], "Return") {
		t.Fatalf("password behind the terminal text=%q err=%v calls=%v", text, err, calls)
	}
}

func TestAppRunKeepsABrowserTypeClosedWhenTheBrowserSocketIsDown(t *testing.T) {
	const secret = "s3cret-value"
	browserDown := fmt.Errorf("desktop browser: discover targets: unexpected HTTP 500")
	var calls [][]string
	class := "Chromium"
	readWindow := true
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "getactivewindow" && args[1] == "getwindowname" {
			if !readWindow {
				return "", fmt.Errorf("exit status 1")
			}
			return "100\n百度一下，你就知道 - Chromium", nil
		}
		if len(args) >= 2 && args[0] == "getactivewindow" && args[1] == "getwindowclassname" {
			if class == "" {
				return "", fmt.Errorf("exit status 1")
			}
			return class, nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "100", nil
		}
		if len(args) >= 4 && args[0] == "search" && args[3] == desktopWhiskerMenuName {
			return "", fmt.Errorf("exit status 1")
		}
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	refuse := func(steps []any) {
		t.Helper()
		calls = nil
		_, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
			"steps": steps,
		}, func() error { return browserDown })
		if err == nil || !strings.Contains(err.Error(), "desktop browser") || strings.Contains(err.Error(), secret) {
			t.Fatalf("err=%v calls=%v", err, calls)
		}
		for _, call := range calls {
			if containsArg(call, secret) || (len(call) > 0 && call[0] == "type") {
				t.Fatalf("text reached xdotool: %v", calls)
			}
		}
	}
	refuse([]any{
		map[string]any{"action": "focus", "name": "Chromium"},
		map[string]any{"action": "type", "text": secret},
	})
	calls = nil
	_, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Chromium"},
			map[string]any{"action": "type", "text": secret},
		},
	}, func() error { return fmt.Errorf("not_password_field") })
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) {
		t.Fatalf("password field err=%v", err)
	}
	for _, call := range calls {
		if containsArg(call, secret) || (len(call) > 0 && call[0] == "type") {
			t.Fatalf("password field was typed: %v", calls)
		}
	}
	refuse([]any{map[string]any{"action": "type", "text": secret}})
	// The window was focused, and its class could not be read.
	class = ""
	refuse([]any{
		map[string]any{"action": "focus", "name": "Terminal"},
		map[string]any{"action": "type", "text": secret},
	})
	// No window id and no named focus: do not ask for a class, and do not type.
	class = "Xfce4-terminal"
	readWindow = false
	calls = nil
	_, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{map[string]any{"action": "type", "text": secret}},
	}, func() error { return browserDown })
	if err == nil || !strings.Contains(err.Error(), "desktop browser") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unread window err=%v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || containsArg(calls[0], secret) {
		t.Fatalf("unread window calls=%v", calls)
	}
}

func TestAppRunSkipsWindowRestoreWhenTheBrowserStaysPut(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609\n", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	reads := 0
	text, err := desktopAppRun(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 10, "y": 20},
			map[string]any{"action": "type", "text": "hi"},
		},
	}, func() error {
		reads++
		// The browser will not raise, so the text step does not record
		// again. The click still records before Super keyup and activates
		// before the click, or the menu takes it.
		if len(calls) != 5 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || !strings.Contains(strings.Join(calls[4], " "), "click") {
			return fmt.Errorf("click was not delivered before the read: %v", calls)
		}
		return nil
	}, func() bool { return false })
	if err != nil || text != "ok" || reads != 1 || len(calls) != 6 {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "mousemove --sync 10 20 click 1" || strings.Join(calls[5], " ") != "type --delay 20 --args 1 -- hi" {
		t.Fatalf("stayed=%v", calls)
	}
	// No click has released modifiers yet. Super keyup opens the menu, so
	// the focused window is recorded before that release and put back after.
	calls = nil
	reads = 0
	text, err = desktopAppRun(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "type", "text": "pwd"},
			map[string]any{"action": "key", "key": "b"},
		},
	}, func() error {
		reads++
		if reads == 1 && (len(calls) != 1 || calls[0][0] != "getactivewindow") {
			return fmt.Errorf("release ran before the window was recorded: %v", calls)
		}
		return nil
	}, func() bool { return false })
	if err != nil || text != "ok" || reads != 2 || len(calls) != 6 {
		t.Fatalf("release reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "type --delay 20 --args 1 -- pwd" || strings.Join(calls[5], " ") != "key b" {
		t.Fatalf("release=%v", calls)
	}
	// The first read can raise Chromium. The next line is on the browser
	// that read just attached, so it must not activate the earlier window.
	calls = nil
	reads = 0
	raises := true
	text, err = desktopAppRun(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 4, "y": 5},
			map[string]any{"action": "type", "text": "a"},
			map[string]any{"action": "key", "key": "b"},
		},
	}, func() error {
		reads++
		if reads == 1 {
			raises = false
		}
		return nil
	}, func() bool { return raises })
	if err != nil || text != "ok" || reads != 2 || len(calls) != 9 {
		t.Fatalf("second reads=%d text=%q err=%v calls=%v", reads, text, err, calls)
	}
	if strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "mousemove --sync 4 5 click 1" || strings.Join(calls[5], " ") != strings.Join(desktopActiveWindowRead, " ") || strings.Join(calls[6], " ") != "windowactivate --sync 8388609" || strings.Join(calls[7], " ") != "type --delay 20 --args 1 -- a" || strings.Join(calls[8], " ") != "key b" {
		t.Fatalf("second=%v", calls)
	}
}

func TestDesktopAppBrowserRaisesOnlyWhenTheReadWillCoverTheWindow(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-raise", UserID: "alice", InstanceID: "raise-bot"}
	if !desktopAppBrowserRaises(nil, scope, "http://cdp") {
		t.Fatal("nil binding did not raise")
	}
	if !desktopAppBrowserRaises(&desktopBinding{}, scope, "http://cdp") {
		t.Fatal("missing session did not raise")
	}
	session := &browser.BrowserAgentSession{}
	binding := &desktopBinding{session: session, addr: "http://cdp"}
	if !desktopAppBrowserRaises(binding, scope, "http://other") {
		t.Fatal("different browser did not raise")
	}
	previous := desktopSessionConnected
	desktopSessionConnected = func(got *browser.BrowserAgentSession) bool {
		return got == session
	}
	t.Cleanup(func() { desktopSessionConnected = previous })
	if desktopAppBrowserRaises(binding, scope, "http://cdp") {
		t.Fatal("connected browser with no resume raised")
	}
	resumeKey := desktopResumeKey(scope)
	desktopResumeFocus.Store(resumeKey, true)
	t.Cleanup(func() { desktopResumeFocus.Delete(resumeKey) })
	if !desktopAppBrowserRaises(binding, scope, "http://cdp") {
		t.Fatal("login resume did not raise")
	}
	desktopSessionConnected = func(*browser.BrowserAgentSession) bool { return false }
	if !desktopAppBrowserRaises(binding, scope, "http://cdp") {
		t.Fatal("dead connection did not raise")
	}
}

func TestAppRunClickThenTypeStopsWhenTheWindowCannotBeRestored(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "cannot activate", fmt.Errorf("exit status 1")
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 10, "y": 20},
			map[string]any{"action": "type", "text": "secret-line"},
		},
	}, func() error { return nil })
	if err == nil || strings.Contains(text, "secret-line") || strings.Contains(err.Error(), "secret-line") {
		t.Fatalf("text=%q err=%v", text, err)
	}
	if len(calls) != 4 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" {
		t.Fatalf("calls=%v", calls)
	}
	for _, call := range calls {
		if len(call) > 0 && (call[0] == "type" || call[0] == "mousemove") {
			t.Fatalf("clicked or typed into the menu: %v", calls)
		}
	}
}

func TestAppRunKeyupFailureStillClicks(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && (args[0] == "keyup" || (args[0] == "search" && containsArg(args, desktopWhiskerMenuName))) {
			return "xdo_send_keysequence_window reported an error for string 'Super_R'", fmt.Errorf("exit status 1")
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 8, "y": 9},
			map[string]any{"action": "type", "text": "hello\nworld"},
		},
	}, nil)
	if err != nil || text != "ok" || len(calls) != 6 {
		t.Fatalf("text=%q err=%v calls=%v", text, err, calls)
	}
	if calls[0][0] != "getactivewindow" || strings.Join(calls[1], " ") != strings.Join(desktopModifierRelease, " ") || !containsArg(calls[1], "Alt_L") || !containsArg(calls[1], "Super_R") || !containsArg(calls[1], "--delay") || !containsArg(calls[1], "0") || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") {
		t.Fatalf("release=%v", calls[:3])
	}
	if strings.Join(calls[3], " ") != "mousemove --sync 8 9 click 1" || strings.Join(calls[4], " ") != "type --delay 20 --args 1 -- hello key Return" || strings.Join(calls[5], " ") != "type --delay 20 --args 1 -- world" {
		t.Fatalf("input=%v", calls[3:])
	}
	for _, call := range calls {
		if len(call) > 0 && call[0] == "windowactivate" {
			t.Fatalf("non-numeric window id was activated: %v", calls)
		}
	}
	keyups := 0
	for _, call := range calls {
		if len(call) > 0 && call[0] == "keyup" {
			keyups++
		}
	}
	if keyups != 1 {
		t.Fatalf("keyups=%d calls=%v", keyups, calls)
	}
}

func TestAppRunClosesTheWhiskerMenuInsteadOfRestoringIt(t *testing.T) {
	var calls [][]string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "12345\nWhisker Menu\n", nil
		}
		if len(args) > 0 && args[0] == "search" && containsArg(args, desktopWhiskerMenuName) {
			return "12345\n", nil
		}
		if len(args) > 0 && args[0] == "key" && containsArg(args, "Escape") {
			return "escape failed", fmt.Errorf("exit status 1")
		}
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 3, "y": 4},
		},
	}, nil)
	if err != nil || text != "ok" || strings.Contains(text, "escape failed") || strings.Contains(text, "12345") {
		t.Fatalf("text=%q err=%v calls=%v", text, err, calls)
	}
	if len(calls) != 5 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "key Escape" || strings.Join(calls[4], " ") != "mousemove --sync 3 4 click 1" {
		t.Fatalf("menu=%v", calls)
	}
	for _, call := range calls {
		if len(call) > 0 && call[0] == "windowactivate" {
			t.Fatalf("whisker menu was activated: %v", calls)
		}
		if len(call) > 0 && call[0] == "search" && containsArg(call, "Escape") {
			t.Fatalf("escape was sent with search: %v", calls)
		}
	}

	calls = nil
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609\nTerminal\n", nil
		}
		if len(args) > 0 && args[0] == "search" && containsArg(args, desktopWhiskerMenuName) {
			return "", fmt.Errorf("exit status 1")
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		if len(args) > 0 && args[0] == "key" && containsArg(args, "Escape") {
			return "escaped", nil
		}
		return "landed", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 6, "y": 7},
		},
	}, nil)
	if err != nil || text != "landed" || strings.Contains(text, "escaped") {
		t.Fatalf("text=%q err=%v calls=%v", text, err, calls)
	}
	if len(calls) != 5 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "windowactivate --sync 8388609" || strings.Join(calls[4], " ") != "mousemove --sync 6 7 click 1" {
		t.Fatalf("terminal=%v", calls)
	}
	for _, call := range calls {
		if containsArg(call, "Escape") {
			t.Fatalf("escape sent when the menu was closed: %v", calls)
		}
	}

	calls = nil
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) > 0 && args[0] == "getactivewindow" {
			return "8388609\n", nil
		}
		if len(args) > 0 && args[0] == "search" && containsArg(args, desktopWhiskerMenuName) {
			return "777\n", nil
		}
		if len(args) > 0 && args[0] == "key" && containsArg(args, "Escape") {
			return "nope", nil
		}
		if len(args) > 0 && args[0] == "windowactivate" {
			return "8388609", nil
		}
		return "landed", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "click", "x": 1, "y": 2},
		},
	}, nil)
	if err != nil || text != "landed" || strings.Contains(text, "nope") || strings.Contains(text, "777") {
		t.Fatalf("text=%q err=%v calls=%v", text, err, calls)
	}
	if len(calls) != 6 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || strings.Join(calls[3], " ") != "key Escape" || strings.Join(calls[4], " ") != "windowactivate --sync 8388609" || strings.Join(calls[5], " ") != "mousemove --sync 1 2 click 1" {
		t.Fatalf("opened=%v", calls)
	}
}

func TestAppOpenStartsTheProgramAndFocusExplainsAMissingWindow(t *testing.T) {
	previousSession := desktopRemoteSession
	previousOpen := desktopRemoteOpen
	previousApp := desktopRemoteApp
	var gotProgram string
	var gotArgs []string
	var gotDisplay string
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:1", Display: ":20"}, nil
	}
	desktopRemoteOpen = func(_ context.Context, _, _, display, program string, args []string) (string, error) {
		gotDisplay = display
		gotProgram = program
		gotArgs = append([]string(nil), args...)
		return "opened " + program, nil
	}
	desktopRemoteApp = func(context.Context, string, string, string, []string) (string, error) {
		return "exit status 1", fmt.Errorf("desktop app command failed: exit status 1")
	}
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteOpen = previousOpen
		desktopRemoteApp = previousApp
	})
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-open", UserID: "alice", InstanceID: "open-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	text, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":  "app_open",
		"program": "xterm",
		"args":    []any{"-geometry", "80x24"},
	})
	if err != nil || text != "opened xterm" || gotProgram != "xterm" || gotDisplay != ":20" || strings.Join(gotArgs, " ") != "-geometry 80x24" {
		t.Fatalf("text=%q program=%s display=%s args=%v err=%v", text, gotProgram, gotDisplay, gotArgs, err)
	}
	gotProgram = ""
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":  "app_open",
		"program": "bash",
		"args":    []any{"-c", "echo hi"},
	})
	if err == nil || !strings.Contains(err.Error(), "app_open") || gotProgram != "" {
		t.Fatalf("shell err=%v program=%s", err, gotProgram)
	}
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":  "app_open",
		"program": "/usr/bin/chromium",
	})
	if err == nil || !strings.Contains(err.Error(), "browser") || gotProgram != "" {
		t.Fatalf("browser err=%v program=%s", err, gotProgram)
	}
	_, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant-open", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{map[string]any{"action": "focus", "name": "xterm"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "not open") || !strings.Contains(err.Error(), "app_open") || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("focus err=%v", err)
	}
}

func TestContinuationStaysOnTheLoggedInPage(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant", UserID: "alice", InstanceID: "bot-a"}
	prev := desktopKeepLoggedInDocument
	var kept []bool
	desktopKeepLoggedInDocument = func(_ *browser.BrowserAgentSession, keep bool) {
		kept = append(kept, keep)
	}
	key := mustDesktopUserKey(t, scope.TenantID, scope.UserID)
	t.Cleanup(func() {
		desktopKeepLoggedInDocument = prev
		desktopResumeFocus.Delete(desktopResumeKey(scope))
		desktopResumeUser.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		desktopLoginDocument.Delete(desktopRunKey(scope.TenantID, scope.UserID))
		binding := desktopBindingFor(key)
		binding.mu.Lock()
		binding.session = nil
		binding.mu.Unlock()
	})
	noteDesktopResume(scope)
	// The handoff run ends before the person finishes logging in.
	releaseLoggedInDocument(scope.TenantID, scope.UserID)
	if len(kept) != 0 {
		t.Fatalf("ending the handoff dropped the logged-in page: %v", kept)
	}
	followDesktopAfterPerson(scope, &browser.BrowserAgentSession{})
	if len(kept) != 1 || !kept[0] {
		t.Fatalf("continuation left the logged-in page: %v", kept)
	}
	followDesktopAfterPerson(scope, &browser.BrowserAgentSession{})
	if len(kept) != 2 || !kept[1] {
		t.Fatalf("reconnecting left the logged-in page: %v", kept)
	}
	binding := desktopBindingFor(key)
	binding.mu.Lock()
	binding.session = &browser.BrowserAgentSession{}
	binding.mu.Unlock()
	releaseLoggedInDocument(scope.TenantID, scope.UserID)
	if len(kept) != 3 || kept[2] {
		t.Fatalf("the next task would still be stuck on the old page: %v", kept)
	}
	followDesktopAfterPerson(scope, &browser.BrowserAgentSession{})
	if len(kept) != 3 {
		t.Fatalf("a later task stayed locked to the old page: %v", kept)
	}
}

func TestOtherRunDoesNotDropTheLoggedInPage(t *testing.T) {
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://docker.example:1", Display: ":20"}, nil
	}
	desktopRemoteStop = func(context.Context, string, string) error { return nil }
	prev := desktopKeepLoggedInDocument
	var kept []bool
	desktopKeepLoggedInDocument = func(_ *browser.BrowserAgentSession, keep bool) {
		kept = append(kept, keep)
	}
	key := mustDesktopUserKey(t, "tenant", "alice")
	t.Cleanup(func() {
		desktopKeepLoggedInDocument = prev
		desktopRemoteSession = nil
		desktopRemoteStop = nil
		desktopRunsMu.Lock()
		desktopRuns = map[string]int{}
		desktopHolds = map[string]int{}
		desktopRunsMu.Unlock()
		binding := desktopBindingFor(key)
		binding.mu.Lock()
		binding.session = nil
		binding.mu.Unlock()
	})
	binding := desktopBindingFor(key)
	binding.mu.Lock()
	binding.session = &browser.BrowserAgentSession{}
	binding.mu.Unlock()
	desktopRunsMu.Lock()
	desktopRuns = map[string]int{}
	desktopHolds = map[string]int{}
	desktopRunsMu.Unlock()
	releaseA := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-a")
	releaseB := occupyUserDesktop(context.Background(), "tenant", "alice", "bot-b")
	releaseA()
	if len(kept) != 0 {
		t.Fatalf("another run cleared the logged-in page: %v", kept)
	}
	releaseB()
	if len(kept) != 1 || kept[0] {
		t.Fatalf("finished run left the navigation lock: %v", kept)
	}
}

func mustDesktopUserKey(t *testing.T, tenantID, userID string) string {
	t.Helper()
	key, err := desktopUserKey(agentruntime.Scope{TenantID: tenantID, UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func stubLiveDesktop() func() {
	previous := desktopEnsureFn
	desktopEnsureFn = func(string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: desktopCDPAddr(), Display: ":1"}, nil
	}
	return func() { desktopEnsureFn = previous }
}
