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
	for _, action := range []string{"navigate", "click", "type", "press", "task_run", "app_run"} {
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
	typed := false
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:1", Display: ":99"}, nil
	}
	desktopAppRunner = func(string, ...string) (string, error) {
		typed = true
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
	if typed {
		t.Fatal("xdotool ran before the focused control was read")
	}
	// A dead CDP address must fail while attaching. Returning not_password_field
	// here would mean the type was refused without reading the focused control.
	if err == nil || !strings.Contains(err.Error(), "desktop browser") || strings.Contains(err.Error(), secret) {
		t.Fatalf("err=%v", err)
	}
	for _, key := range []string{"p", "Shift_L+plus", "Control_L+v"} {
		typed = false
		_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action": "app_run",
			"steps":  []any{map[string]any{"action": "key", "key": key}},
		})
		if typed {
			t.Fatalf("%s reached xdotool before the focused control was read", key)
		}
		if err == nil || !strings.Contains(err.Error(), "desktop browser") {
			t.Fatalf("key %s err=%v", key, err)
		}
	}
	typed = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Return"}},
	})
	if !typed {
		t.Fatalf("Return was held for a browser focus read: %v", err)
	}
	typed = false
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "app_run",
		"steps":  []any{map[string]any{"action": "key", "key": "Control_L"}},
	})
	if !typed {
		t.Fatalf("a modifier alone was held for a browser focus read: %v", err)
	}
	// A click can move into the password field. It has to be delivered, and
	// the focus read has to fail closed, before the following text is sent.
	var calls [][]string
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
		if len(calls) != 1 || !containsArg(calls[0], "click") || containsArg(calls[0], secret) || containsArg(calls[0], "p") {
			t.Fatalf("insert after click calls=%v", calls)
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
	if err != nil || len(calls) != 1 || !containsArg(calls[0], "click") || !containsArg(calls[0], "Return") {
		t.Fatalf("click+Return calls=%v err=%v", calls, err)
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
		for _, banned := range []string{"task_run", "app_run"} {
			if strings.Contains(text, banned) {
				t.Fatalf("plan copy still teaches %s: %s", banned, text)
			}
		}
	}
	encoded, err := json.Marshal(tools[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "task_run") || strings.Contains(string(encoded), "app_run") {
		t.Fatalf("plan parameters still teach mutation: %s", encoded)
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
