package llm

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestBindOpenCodeSessionUsesTraceAndFirstUserMessage(t *testing.T) {
	ctx := WithRequestTrace(context.Background(), RequestTrace{OwnerID: "user-1", LoopID: "chat"})
	cfg := bindOpenCodeSession(ctx, corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/go/v1",
		Model: "glm-5.3-flash",
	}, []any{map[string]any{"role": "user", "content": "fix login"}})
	want := corelib.StableOpenCodeSessionID("user-1", "chat", "fix login")
	if cfg.SessionID != want {
		t.Fatalf("SessionID = %q, want %q", cfg.SessionID, want)
	}
	again := bindOpenCodeSession(ctx, corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/go/v1",
		Model: "glm-5.3-flash",
	}, []any{
		map[string]any{"role": "user", "content": "fix login"},
		map[string]any{"role": "assistant", "content": "ok"},
		map[string]any{"role": "user", "content": "add tests"},
	})
	if again.SessionID != cfg.SessionID {
		t.Fatalf("session id changed across turns: %q vs %q", again.SessionID, cfg.SessionID)
	}

	typed := bindOpenCodeSession(ctx, corelib.MaclawLLMConfig{
		URL:   "https://opencode.ai/zen/go/v1",
		Model: "glm-5.3-flash",
	}, []interface{}{map[string]string{"role": "user", "content": "fix login"}})
	if typed.SessionID != want {
		t.Fatalf("map[string]string SessionID = %q, want %q", typed.SessionID, want)
	}
}
