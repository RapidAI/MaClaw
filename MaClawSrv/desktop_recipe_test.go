package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDesktopRecipeStoredOnlyAfterValidationAndInjectedForSameIntent(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-rec", RunID: "run_rec_1"}
	desktopRecipeBegin(scope, "做10.18的 Hugging Face Daily Papers 摘要，发到知乎上")
	big := strings.Repeat("paper line\n", 60)
	cmd := map[string]any{"action": "bash", "command": "curl -s https://example.org/api?date=2026-10-16 | jq ."}
	desktopRecipeObserve(scope, "bash", map[string]any{"action": "bash", "command": "ls"}, "x", nil) // too small
	desktopRecipeObserve(scope, "bash", cmd, big, nil)
	desktopRecipeObserve(scope, "bash", map[string]any{"action": "bash", "command": "bad"}, big, errors.New("exit 1"))
	// Not validated yet: nothing injected.
	if got := desktopRecipeText(scope, "做10.20的 Hugging Face Daily Papers 摘要，发到知乎上"); got != "" {
		t.Fatalf("unvalidated recipe injected: %q", got)
	}
	desktopRecipeValidate(scope)
	got := desktopRecipeText(agentruntime.Scope{TenantID: "t", UserID: "u-rec", RunID: "run_rec_2"}, "做10.20的 Hugging Face Daily Papers 摘要，发到知乎上")
	if !strings.Contains(got, "curl -s https://example.org/api") || !strings.Contains(got, "FIRST") {
		t.Fatalf("recipe not injected: %q", got)
	}
	if other := desktopRecipeText(scope, "帮我订一张明天去上海的火车票"); other != "" {
		t.Fatalf("unrelated task got recipe: %q", other)
	}
	if x := desktopRecipeText(agentruntime.Scope{TenantID: "t", UserID: "someone-else"}, "做10.20的 Hugging Face Daily Papers 摘要，发到知乎上"); x != "" {
		t.Fatalf("leaked across users")
	}
}

func TestDesktopRecipeIntentNormalizesDigits(t *testing.T) {
	if desktopRecipeIntent("做10.18的摘要") != desktopRecipeIntent("做9.2的摘要") {
		// digit counts differ; similarity must still be high
		if s := desktopRecipeSimilarity(desktopRecipeIntent("做10.18的摘要"), desktopRecipeIntent("做9.2的摘要")); s < desktopRecipeMatchMin {
			t.Fatalf("similarity %.2f", s)
		}
	}
}
