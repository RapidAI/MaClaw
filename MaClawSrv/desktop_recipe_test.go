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

func TestDesktopRecipeIgnoresStepsAfterFirstWrite(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-rec2", RunID: "run_rec_3"}
	desktopRecipeBegin(scope, "summarize papers and post")
	big := strings.Repeat("row\n", 200)
	desktopRecipeObserve(scope, "bash", map[string]any{"command": "fetch-material"}, big, nil)
	desktopRecipeObserve(scope, "rich_fill", map[string]any{}, "ok", nil)
	desktopRecipeObserve(scope, "script", map[string]any{"script": "readBackPage()"}, big, nil)
	desktopRecipeValidate(scope)
	got := desktopRecipeText(agentruntime.Scope{TenantID: "t", UserID: "u-rec2"}, "summarize papers and post")
	if !strings.Contains(got, "fetch-material") || strings.Contains(got, "readBackPage") {
		t.Fatalf("got %q", got)
	}
}

func TestDesktopRecipeOldVersionNotOffered(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-recv", RunID: "run_recv_1"}
	desktopRecipeBegin(scope, "做10.18的 Hugging Face Daily Papers 摘要，发到知乎上")
	desktopRecipeObserve(scope, "bash", map[string]any{"action": "bash", "command": "curl -s https://example.org/a"}, strings.Repeat("paper line\n", 60), nil)
	desktopRecipeValidate(scope)
	desktopExperienceMu.Lock()
	path := desktopExperiencePath(scope)
	f := desktopExperienceLoad(path)
	for i := range f.Recipes {
		f.Recipes[i].V = 0
	}
	desktopExperienceSave(path, f)
	desktopExperienceMu.Unlock()
	if got := desktopRecipeText(scope, "做10.20的 Hugging Face Daily Papers 摘要，发到知乎上"); got != "" {
		t.Fatalf("v0 recipe offered: %q", got)
	}
}

func TestDesktopRecipeWritingScriptFreezes(t *testing.T) {
	desktopExperienceRoot = t.TempDir()
	t.Cleanup(func() { desktopExperienceRoot = "" })
	scope := agentruntime.Scope{TenantID: "t", UserID: "u-recw", RunID: "run_recw_1"}
	desktopRecipeBegin(scope, "做10.18的 Hugging Face Daily Papers 摘要，发到知乎上")
	big := strings.Repeat("paper line\n", 60)
	desktopRecipeObserve(scope, "bash", map[string]any{"action": "bash", "command": "curl -s https://example.org/gather"}, big, nil)
	desktopRecipeObserve(scope, "script", map[string]any{"action": "script", "script": "await type('textarea', T); return document.body.innerText"}, big, nil)
	desktopRecipeObserve(scope, "script", map[string]any{"action": "script", "script": "return document.body.innerText"}, big, nil)
	desktopRecipeValidate(scope)
	if got := desktopRecipeText(scope, "做10.20的 Hugging Face Daily Papers 摘要，发到知乎上"); !strings.Contains(got, "example.org/gather") {
		t.Fatalf("want gathering bash, got %q", got)
	}
}

func TestRecipeSimilarityRejectsOtherTaskSameTopic(t *testing.T) {
	zh := desktopRecipeIntent("做10.18的 Hugging Face Daily Papers 摘要，发到知乎上")
	xhs := desktopRecipeIntent("做一张简单的封面图，在小红书发一篇图文笔记，主题是 Hugging Face Daily Papers 10.18 的三篇亮点论文速览（标题+三段短正文）。发布后打开笔记确认，并把链接给我。")
	if s := desktopRecipeSimilarity(zh, xhs); s >= desktopRecipeMatchMin {
		t.Fatalf("cross-task score %.2f", s)
	}
	if s := desktopRecipeSimilarity(zh, desktopRecipeIntent("做10.17的 Hugging Face Daily Papers 摘要，发到知乎上")); s < desktopRecipeMatchMin {
		t.Fatalf("same task score %.2f", s)
	}
}
