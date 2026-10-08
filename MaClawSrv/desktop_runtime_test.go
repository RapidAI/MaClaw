package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
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
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), agentruntime.TurnRequest{})
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
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), agentruntime.TurnRequest{})
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
	var calls int
	var argv []string
	previous := desktopRemoteApp
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls++
		argv = append([]string(nil), args...)
		return "ok", nil
	}
	t.Cleanup(func() { desktopRemoteApp = previous })
	text, err := operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Notes"},
			map[string]any{"action": "type", "text": "hi"},
			map[string]any{"action": "key", "key": "Return"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || text != "ok" {
		t.Fatalf("calls=%d text=%q argv=%v", calls, text, argv)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "windowactivate") || !strings.Contains(joined, "type") || !strings.Contains(joined, "key") {
		t.Fatalf("argv=%v", argv)
	}
	// A focus read has to see the window switch before the type is sent.
	var guarded [][]string
	reads := 0
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		guarded = append(guarded, append([]string(nil), args...))
		return "ok", nil
	}
	text, err = operateDesktopApp(context.Background(), agentruntime.Scope{TenantID: "tenant", UserID: "alice"}, ":20", "app_run", map[string]any{
		"steps": []any{
			map[string]any{"action": "focus", "name": "Notes"},
			map[string]any{"action": "type", "text": "hi"},
			map[string]any{"action": "key", "key": "Return"},
		},
	}, func() error {
		reads++
		return nil
	})
	if err != nil || text != "ok" || reads != 1 || len(guarded) != 3 {
		t.Fatalf("reads=%d text=%q err=%v calls=%v", reads, text, err, guarded)
	}
	if !strings.Contains(strings.Join(guarded[0], " "), "windowactivate") || strings.Contains(strings.Join(guarded[0], " "), "type") {
		t.Fatalf("focus was not delivered alone: %v", guarded[0])
	}
	if strings.Join(guarded[1], " ") != "type --delay 20 hi" {
		t.Fatalf("type call=%v", guarded[1])
	}
	if strings.Join(guarded[2], " ") != "key Return" {
		t.Fatalf("return call=%v", guarded[2])
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
