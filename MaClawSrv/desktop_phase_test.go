package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestPlanPhaseDoesNotMutateOrHandOffLogin(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-phase", UserID: "alice", InstanceID: "phase-bot"}
	t.Cleanup(func() {
		desktopHandoff.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
		desktopAttention.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
	})
	plan := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "plan"}}},
	}
	mod := desktopRuntimeModule{}
	for _, action := range []string{"navigate", "click", "type", "press", "task_run", "app_run", "app_open", "install", "deliver", "file_write", "file_edit", "bash"} {
		_, err := mod.InvokeTool(context.Background(), plan, "desktop", map[string]any{
			"action": action,
			"url":    "https://example.com",
			"ref":    "e1",
			"text":   "hello",
			"key":    "Enter",
		})
		if err == nil || !strings.Contains(err.Error(), "plan phase blocks "+action) {
			t.Fatalf("%s err=%v", action, err)
		}
	}
	probe, err := finishDesktopActionPhase(scope, "page flags: login_wall", nil, "plan")
	if err != nil || probe != "page flags: login_wall" {
		t.Fatalf("plan probe=%q err=%v", probe, err)
	}
	if takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) || takeDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID) != "" {
		t.Fatal("a plan probe recorded a login handoff")
	}
	login, err := finishDesktopActionPhase(scope, "page flags: login_wall", nil, "execute")
	if err != nil {
		t.Fatal(err)
	}
	asked, ok := agent.ParseAskUserResult(login)
	if !ok || asked.Question != desktopLoginQuestion {
		t.Fatalf("execute login=%q", login)
	}
	if !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("execute did not record the login handoff")
	}
	if reason := takeDesktopAttention(scope.TenantID, scope.UserID, scope.InstanceID); reason != "login_wall" {
		t.Fatalf("attention=%q", reason)
	}
}

func TestPaymentConfirmHandsOffOnlyDuringExecution(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-pay", UserID: "alice", InstanceID: "pay-bot"}
	t.Cleanup(func() {
		desktopHandoff.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
		desktopAttention.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
	})
	for _, text := range []string{"确认支付", "价格 99 元", "nav 支付", "page flags: canvas"} {
		if reason := desktopAttentionReason(text); reason != "" {
			t.Fatalf("%q attention=%q", text, reason)
		}
	}
	planned, err := finishDesktopActionPhase(scope, "page flags: payment_confirm", nil, "plan")
	if err != nil || planned != "page flags: payment_confirm" || takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatalf("plan payment=%q err=%v", planned, err)
	}
	paid, err := finishDesktopActionPhase(scope, "page flags: payment_confirm", nil, "execute")
	if err != nil {
		t.Fatal(err)
	}
	asked, ok := agent.ParseAskUserResult(paid)
	if !ok || asked.Question != "这一步需要你在当前桌面的浏览器里完成支付。完成后这个 bot 会接着操作。" {
		t.Fatalf("payment ask=%q", paid)
	}
	if !takeDesktopHandoff(scope.TenantID, scope.UserID, scope.InstanceID) {
		t.Fatal("card payment did not hand off during execution")
	}
	consent, err := finishDesktopActionPhase(scope, "page flags: consent_dialog", nil, "execute")
	asked, ok = agent.ParseAskUserResult(consent)
	if err != nil || !ok || asked.Question != "这一步需要你在当前桌面的浏览器里确认这项同意。完成后这个 bot 会接着操作。" {
		t.Fatalf("consent=%q err=%v", consent, err)
	}
}

func TestModelTypeIntoAPasswordFieldIsRefused(t *testing.T) {
	const secret = "s3cret-value"
	previous := desktopFocusedInputType
	previousSession := desktopRemoteSession
	desktopFocusedInputType = func(agentruntime.Scope) string { return "password" }
	reached := false
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		reached = true
		return desktopEndpoint{}, fmt.Errorf("desktop session was reached")
	}
	t.Cleanup(func() {
		desktopFocusedInputType = previous
		desktopRemoteSession = previousSession
	})
	scope := agentruntime.Scope{TenantID: "tenant-secret", UserID: "alice", InstanceID: "secret-bot"}
	turn := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "type",
		"text":   secret,
	})
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) || reached {
		t.Fatalf("active type err=%v reached=%v", err, reached)
	}
	reached = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "type",
		"text":   secret,
		"ref":    "e4",
	})
	if !reached || err == nil || err.Error() == "not_password_field" || strings.Contains(err.Error(), secret) {
		t.Fatalf("ref type was refused with the focused password: err=%v reached=%v", err, reached)
	}
	reached = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "task_run",
		"steps": []any{
			map[string]any{"action": "type", "params": map[string]any{"text": secret}},
		},
	})
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) || reached {
		t.Fatalf("task_run active type err=%v reached=%v", err, reached)
	}
	reached = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "task_run",
		"steps": []any{
			map[string]any{"action": "click", "params": map[string]any{"ref": "e2"}},
			map[string]any{"action": "type", "params": map[string]any{"text": secret, "ref": "e4"}},
		},
	})
	if !reached || err == nil || err.Error() == "not_password_field" || strings.Contains(err.Error(), secret) {
		t.Fatalf("task_run ref type was refused with the focused password: err=%v reached=%v", err, reached)
	}
	reached = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps": []any{
			map[string]any{"action": "type", "text": secret},
		},
	})
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) {
		t.Fatalf("app_run err=%v", err)
	}
	for _, key := range []string{"p", "Shift_L+a", "plus", "Control_L+v"} {
		_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action": "press",
			"key":    key,
		})
		if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) {
			t.Fatalf("press %s err=%v", key, err)
		}
	}
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "task_run",
		"steps": []any{
			map[string]any{"action": "press", "params": map[string]any{"key": "p"}},
		},
	})
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) || reached {
		t.Fatalf("task_run press err=%v reached=%v", err, reached)
	}
}

func TestDesktopInsertsIntoFocusedControl(t *testing.T) {
	active := map[string]any{"action": "type", "text": "hi"}
	if !desktopInsertsIntoFocusedControl("type", active) {
		t.Fatal("typing into the focused control was allowed through")
	}
	if desktopInsertsIntoFocusedControl("type", map[string]any{"action": "type", "text": "hi", "ref": "e4"}) {
		t.Fatal("a named ref was treated as the focused control")
	}
	if !desktopInsertsIntoFocusedControl("press", map[string]any{"key": "p"}) {
		t.Fatal("a character press was allowed through")
	}
	if desktopInsertsIntoFocusedControl("press", map[string]any{"key": "Return"}) {
		t.Fatal("Return was treated as text")
	}
	steps := []any{
		map[string]any{"action": "click", "x": 1, "y": 2},
		map[string]any{"action": "type", "text": "hi"},
	}
	if desktopInsertsIntoFocusedControl("app_run", map[string]any{"steps": steps}) {
		t.Fatal("a click before the text was treated as the current focus")
	}
	if !desktopInsertsIntoFocusedControl("app_run", map[string]any{"steps": []any{map[string]any{"action": "type", "text": "hi"}}}) {
		t.Fatal("an app type with no prior click was allowed through")
	}
	task := []any{map[string]any{"action": "type", "params": map[string]any{"text": "hi", "ref": "e4"}}}
	if desktopInsertsIntoFocusedControl("task_run", map[string]any{"steps": task}) {
		t.Fatal("a task type into a ref was treated as the focused control")
	}
	bare := []any{map[string]any{"action": "type", "params": map[string]any{"text": "hi"}}}
	if !desktopInsertsIntoFocusedControl("task_run", map[string]any{"steps": bare}) {
		t.Fatal("a task type into the focused control was allowed through")
	}
}

func TestAppRunTypeAttachesTheBrowserBeforeXdotool(t *testing.T) {
	const secret = "s3cret-value"
	previousSession := desktopRemoteSession
	previousRunner := desktopAppRunner
	previousFocus := desktopFocusedInputType
	desktopFocusedInputType = desktopLiveFocusedInputType
	var calls [][]string
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:1", Display: ":99"}, nil
	}
	desktopAppRunner = func(_ string, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "", fmt.Errorf("xdotool should not run")
	}
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopAppRunner = previousRunner
		desktopFocusedInputType = previousFocus
	})
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-app-focus", UserID: "alice", InstanceID: "app-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "type", "text": secret}},
	})
	// The focused window is recorded before the password read. The secret
	// is not sent until that read succeeds.
	if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(desktopActiveWindowRead, " ") || containsArg(calls[0], secret) {
		t.Fatalf("type before read calls=%v", calls)
	}
	// A dead CDP address must fail while attaching. Returning not_password_field
	// here would mean the type was refused without reading the focused control.
	if err == nil || !strings.Contains(err.Error(), "desktop browser") || strings.Contains(err.Error(), secret) {
		t.Fatalf("err=%v", err)
	}
	for _, key := range []string{"p", "Shift_L+plus", "Control_L+v"} {
		calls = nil
		_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action": "app_run",
			"steps":  []any{map[string]any{"action": "key", "key": key}},
		})
		if len(calls) != 1 || calls[0][0] != "getactivewindow" || containsArg(calls[0], key) {
			t.Fatalf("%s reached xdotool before the focused control was read: %v", key, calls)
		}
		if err == nil || !strings.Contains(err.Error(), "desktop browser") {
			t.Fatalf("key %s err=%v", key, err)
		}
	}
	calls = nil
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Return"}},
	})
	if len(calls) != 4 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || calls[3][0] != "key" || !containsArg(calls[3], "Return") || containsArg(calls[0], "Return") {
		t.Fatalf("Return was held for a browser focus read: %v err=%v", calls, err)
	}
	calls = nil
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Control_L"}},
	})
	if len(calls) != 4 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || !containsArg(calls[3], "Control_L") || containsArg(calls[0], "Control_L") {
		t.Fatalf("a modifier alone was held for a browser focus read: %v err=%v", calls, err)
	}
	// A click can move into the password field. It has to be delivered, and
	// the focus read has to fail closed, before the following text is sent.
	desktopAppRunner = func(_ string, args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "clicked", nil
	}
	for _, insert := range []map[string]any{
		{"action": "type", "text": secret},
		{"action": "key", "key": "p"},
	} {
		calls = nil
		_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action": "app_run",
			"steps": []any{
				map[string]any{"action": "click", "x": 20, "y": 30},
				insert,
			},
		})
		if err == nil || !strings.Contains(err.Error(), "desktop browser") || strings.Contains(err.Error(), secret) {
			t.Fatalf("insert after click err=%v", err)
		}
		if len(calls) != 5 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || containsArg(calls[1], "click") || !containsArg(calls[3], "click") || containsArg(calls[3], "keyup") || strings.Join(calls[4], " ") != strings.Join(desktopActiveWindowRead, " ") {
			t.Fatalf("insert after click calls=%v", calls)
		}
		for _, call := range calls {
			if containsArg(call, secret) || containsArg(call, "p") || (len(call) > 0 && call[0] == "windowactivate") {
				t.Fatalf("insert after click calls=%v", calls)
			}
		}
	}
	calls = nil
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps": []any{
			map[string]any{"action": "click", "x": 20, "y": 30},
			map[string]any{"action": "key", "key": "Return"},
		},
	})
	if err != nil || len(calls) != 5 || calls[0][0] != "getactivewindow" || calls[1][0] != "keyup" || strings.Join(calls[2], " ") != strings.Join(desktopWhiskerDismiss, " ") || !containsArg(calls[1], "Alt_L") || !containsArg(calls[1], "Alt_R") || !containsArg(calls[1], "Control_L") || !containsArg(calls[1], "Control_R") || !containsArg(calls[1], "Shift_L") || !containsArg(calls[1], "Shift_R") || !containsArg(calls[1], "Super_L") || !containsArg(calls[1], "Super_R") || !containsArg(calls[1], "--delay") || !containsArg(calls[1], "0") || containsArg(calls[1], "click") || containsArg(calls[3], "keyup") || !containsArg(calls[3], "click") || containsArg(calls[3], "Return") || calls[4][0] != "key" || containsArg(calls[4], "keyup") || !containsArg(calls[4], "Return") {
		t.Fatalf("click+Return calls=%v err=%v", calls, err)
	}
	for _, call := range calls {
		if len(call) > 0 && call[0] == "windowactivate" {
			t.Fatalf("non-numeric window id was activated: %v", calls)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestPlanCopyListsOnlyInspection(t *testing.T) {
	t.Setenv(desktopCDPEnv, "http://127.0.0.1:9222/")
	plan := agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "plan"}}},
	}
	tools, err := desktopRuntimeModule{}.Tools(context.Background(), plan)
	if err != nil || len(tools) != 1 {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	prompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{tools[0].Description, prompt} {
		for _, allowed := range []string{"probe", "screenshot", "app_list"} {
			if !strings.Contains(text, allowed) {
				t.Fatalf("plan copy missing %s: %s", allowed, text)
			}
		}
		for _, banned := range []string{"task_run", "app_run", "app_open"} {
			if strings.Contains(text, banned) {
				t.Fatalf("plan copy still teaches %s: %s", banned, text)
			}
		}
	}
	encoded, err := json.Marshal(tools[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "task_run") || strings.Contains(string(encoded), "app_run") || strings.Contains(string(encoded), "deliver") {
		t.Fatalf("plan parameters still teach mutation: %s", encoded)
	}
	if !strings.Contains(prompt, "one arrangement") || !strings.Contains(prompt, "Do not ask the user") || !strings.Contains(prompt, "say it is done") || !strings.Contains(prompt, "does not start it") || strings.Contains(prompt, "action=deliver") {
		t.Fatalf("plan copy does not keep the arrangement to one confirmation: %s", prompt)
	}
	if !strings.Contains(prompt, "Do not paste the document") || !strings.Contains(prompt, "copy it into Word") || !strings.Contains(prompt, "task order") {
		t.Fatalf("plan copy still lets a document become chat text: %s", prompt)
	}
	if !strings.Contains(tools[0].Description, "Do not paste the document") {
		t.Fatalf("plan tool lost the document rule: %s", tools[0].Description)
	}
	execute := agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	execTools, err := desktopRuntimeModule{}.Tools(context.Background(), execute)
	if err != nil || len(execTools) != 1 || !strings.Contains(execTools[0].Description, "action=deliver") {
		t.Fatalf("execute copy lost deliver: %v", execTools)
	}
	execPrompt, err := desktopRuntimeModule{}.ContributePrompt(context.Background(), execute)
	if err != nil || !strings.Contains(execPrompt, "action=deliver") || !strings.Contains(execPrompt, "Do not ask the user") || !strings.Contains(execPrompt, "Do not paste the document") || !strings.Contains(execPrompt, "copy it into Word") || !strings.Contains(execPrompt, "report what happened") || !strings.Contains(execPrompt, "another confirmation") || !strings.Contains(execPrompt, "app_open") || !strings.Contains(execPrompt, "exit status 1") || !strings.Contains(execPrompt, "not open") || strings.Contains(execPrompt, "sh -c") {
		t.Fatalf("execute prompt=%s err=%v", execPrompt, err)
	}
	if !strings.Contains(execTools[0].Description, "app_open") || !strings.Contains(execTools[0].Description, "exit status 1") || !strings.Contains(execTools[0].Description, "not a shell") {
		t.Fatalf("execute tool=%s", execTools[0].Description)
	}
	if strings.Contains(execPrompt, ".docx or .txt") || strings.Contains(execTools[0].Description, ".docx or .txt") {
		t.Fatal("deliver copy still limits the file type")
	}
	if !strings.Contains(execPrompt, "including a pdf") || !strings.Contains(execPrompt, "uses path") || !strings.Contains(execTools[0].Description, "including a pdf") {
		t.Fatal("deliver copy does not read the desktop file")
	}
}

func TestExecuteInstallRunsAptPackages(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-install", UserID: "alice", InstanceID: "install-bot"}
	previousSession := desktopRemoteSession
	previousInstall := desktopRemoteInstall
	var got []string
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://10.0.0.8:19020", Display: ":20"}, nil
	}
	desktopRemoteInstall = func(_ context.Context, tenantID, userID string, packages []string) (string, error) {
		if tenantID != scope.TenantID || userID != scope.UserID {
			t.Fatalf("account %s/%s", tenantID, userID)
		}
		got = append([]string(nil), packages...)
		return "installed: libreoffice", nil
	}
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteInstall = previousInstall
	})
	turn := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	text, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":   "install",
		"packages": []any{"libreoffice", "libreoffice"},
	})
	if err != nil || text != "installed: libreoffice" || len(got) != 1 || got[0] != "libreoffice" {
		t.Fatalf("text=%q err=%v packages=%v", text, err, got)
	}
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action":   "install",
		"packages": []any{"libreoffice;rm -rf /"},
	})
	if err == nil || !strings.Contains(err.Error(), "package") || len(got) != 1 {
		t.Fatalf("bad package err=%v packages=%v", err, got)
	}
}

func TestSecretFillReportsACodeWithoutTheValue(t *testing.T) {
	const secret = "s3cret-value"
	scope := agentruntime.Scope{TenantID: "tenant-fill", UserID: "alice", InstanceID: "fill-bot"}
	previousFocus := desktopSecretFocus
	previousSend := desktopSecretSend
	t.Cleanup(func() {
		desktopSecretFocus = previousFocus
		desktopSecretSend = previousSend
	})
	var method string
	var text string
	desktopSecretFocus = func(agentruntime.Scope) (bool, string) { return true, "password" }
	desktopSecretSend = func(_ agentruntime.Scope, name string, params map[string]any) error {
		method = name
		text, _ = params["text"].(string)
		return nil
	}
	if err := applyDesktopSecretFill(scope, secret); err != nil {
		t.Fatal(err)
	}
	if method != "Input.insertText" || text != secret {
		t.Fatalf("method=%s text=%q", method, text)
	}
	desktopSecretFocus = func(agentruntime.Scope) (bool, string) { return true, "cross-origin" }
	err := applyDesktopSecretFill(scope, secret)
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) {
		t.Fatalf("cross-origin err=%v", err)
	}
	desktopSecretFocus = func(agentruntime.Scope) (bool, string) { return false, "password" }
	err = applyDesktopSecretFill(scope, secret)
	if err == nil || err.Error() != "no_focus" || strings.Contains(err.Error(), secret) {
		t.Fatalf("focus err=%v", err)
	}
	desktopSecretFocus = func(agentruntime.Scope) (bool, string) { return true, "password" }
	desktopSecretSend = func(agentruntime.Scope, string, map[string]any) error { return fillErrorCode("boom " + secret) }
	err = applyDesktopSecretFill(scope, secret)
	if err == nil || err.Error() != "unavailable" || strings.Contains(err.Error(), secret) {
		t.Fatalf("send err=%v", err)
	}
}
