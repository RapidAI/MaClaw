package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func stubDesktopGround(t *testing.T, cfg corelib.MaclawLLMConfig, ok bool) *int {
	t.Helper()
	shots := 0
	oldCfg, oldShot := desktopGrounderConfig, desktopGroundShot
	desktopGrounderConfig = func() (corelib.MaclawLLMConfig, bool) { return cfg, ok }
	desktopGroundShot = func(context.Context, agentruntime.Scope, string, desktop.ShotOptions) (desktop.Shot, error) {
		shots++
		return desktop.Shot{Data: []byte("png"), MIME: "image/png", ImageWidth: 1280, ImageHeight: 720, Scale: 1}, nil
	}
	t.Cleanup(func() { desktopGrounderConfig, desktopGroundShot = oldCfg, oldShot })
	return &shots
}

func TestGroundDesktopStepsLeavesPlainStepsAlone(t *testing.T) {
	shots := stubDesktopGround(t, corelib.MaclawLLMConfig{}, false)
	raw := []any{map[string]any{"action": "left_click", "coordinate": []any{1, 2}, "target": "ignored"}}
	out, notes, err := groundDesktopSteps(context.Background(), agentruntime.Scope{RunID: "g1"}, ":1", raw)
	if err != nil || len(notes) != 0 || *shots != 0 {
		t.Fatalf("err=%v notes=%v shots=%d", err, notes, *shots)
	}
	if out.([]any)[0].(map[string]any)["target"] != "ignored" {
		t.Fatal("steps with coordinates must pass through")
	}
}

func TestGroundDesktopStepsWithoutGrounderFallsBack(t *testing.T) {
	stubDesktopGround(t, corelib.MaclawLLMConfig{}, false)
	raw := []any{map[string]any{"action": "left_click", "target": "OK"}}
	_, _, err := groundDesktopSteps(context.Background(), agentruntime.Scope{RunID: "g2"}, ":1", raw)
	if err == nil || !strings.Contains(err.Error(), "give x,y") {
		t.Fatalf("err=%v", err)
	}
}

func TestGroundDesktopStepsOnlyFirstStep(t *testing.T) {
	stubDesktopGround(t, corelib.MaclawLLMConfig{URL: "http://x", Model: "m"}, true)
	raw := []any{map[string]any{"action": "key", "text": "Return"}, map[string]any{"type": "click", "target": "OK"}}
	if _, _, err := groundDesktopSteps(context.Background(), agentruntime.Scope{RunID: "g3"}, ":1", raw); err == nil {
		t.Fatal("a later targeted step must be refused")
	}
}

func TestGroundDesktopStepsUsesGrounder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"x\":300,\"y\":200}"}}]}`))
	}))
	defer srv.Close()
	shots := stubDesktopGround(t, corelib.MaclawLLMConfig{URL: srv.URL, Model: "qwen3-vl", Protocol: "openai"}, true)
	raw := []any{map[string]any{"action": "left_click", "target": "确定"}, map[string]any{"action": "type", "text": "hi"}}
	out, notes, err := groundDesktopSteps(context.Background(), agentruntime.Scope{RunID: "g4"}, ":1", raw)
	if err != nil {
		t.Fatal(err)
	}
	step := out.([]any)[0].(map[string]any)
	if step["x"] != 300 || step["y"] != 200 || step["target"] != nil {
		t.Fatalf("step=%v", step)
	}
	if raw[0].(map[string]any)["target"] != "确定" {
		t.Fatal("caller's step must not be mutated")
	}
	if *shots != 1 || len(notes) != 1 || !strings.Contains(notes[0], "(300,200)") {
		t.Fatalf("shots=%d notes=%v", *shots, notes)
	}
}

func TestResolveDesktopGrounderUsesHubRoleWhenEnvMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"x\":11,\"y\":22}"}}]}`))
	}))
	defer srv.Close()
	stubDesktopGround(t, corelib.MaclawLLMConfig{}, false)
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "DeepSeek",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "sk-ds", Model: "deepseek-chat", Protocol: "openai"},
			{Name: "Grounder", URL: srv.URL, Key: "sk-g", Model: "qwen3-vl", Protocol: "openai"},
		},
		MaclawHubBotRoles: corelib.MaclawHubBotRoles{
			Grounder: corelib.MaclawHubBotRole{Mode: corelib.MaclawHubBotRolePin, Provider: "Grounder"},
		},
	}
	ctx := context.WithValue(context.Background(), desktopAppConfigKey{}, cfg)
	got, ok := resolveDesktopGrounder(ctx)
	if !ok || got.ProviderName != "Grounder" || got.Model != "qwen3-vl" {
		t.Fatalf("role grounder = %+v ok=%v", got, ok)
	}
	raw := []any{map[string]any{"action": "left_click", "target": "OK"}}
	out, notes, err := groundDesktopSteps(ctx, agentruntime.Scope{RunID: "g-role"}, ":1", raw)
	if err != nil {
		t.Fatal(err)
	}
	step := out.([]any)[0].(map[string]any)
	if step["x"] != 11 || step["y"] != 22 {
		t.Fatalf("step=%v notes=%v", step, notes)
	}
}
