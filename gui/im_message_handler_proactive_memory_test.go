package main

import (
	pathpkg "path/filepath"
	"strings"
	"testing"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func newTestIMHandlerWithMemoryStore(t *testing.T) *IMMessageHandler {
	t.Helper()
	tmpDir := t.TempDir()
	memPath := pathpkg.Join(tmpDir, "memories.json")
	ms, err := NewMemoryStore(memPath)
	if err != nil {
		t.Fatalf("NewMemoryStore: %v", err)
	}
	t.Cleanup(ms.Stop)

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.memoryStore = ms
	return h
}

func assertContainsAll(t *testing.T, text string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Errorf("missing %q", part)
		}
	}
}

func assertContainsNone(t *testing.T, text string, parts []string) {
	t.Helper()
	for _, part := range parts {
		if strings.Contains(text, part) {
			t.Errorf("unexpectedly contained %q", part)
		}
	}
}

func TestSystemPrompt_FirstTurn_ContainsProactiveMemoryInstruction(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	prompt := h.buildSystemPromptWithMemory("hello", true)

	assertContainsAll(t, prompt, []string{
		corememory.PromptSectionUserMemory,
		"如需更多记忆，可通过 " + corememory.PromptActionRecallColon,
		corememory.BuildIMMemoryGuidePrompt(),
	})
}

func TestSystemPrompt_NonFirstTurn_NoProactiveMemoryInstruction(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	prompt := h.buildSystemPromptWithMemory("hello", false)

	assertContainsNone(t, prompt, []string{
		corememory.PromptSectionMemoryGuide,
		corememory.PromptActionSaveColon,
		corememory.PromptSaveCategorySummary,
	})
}

func TestSystemPrompt_ClearHistory_RestoresFirstTurnProactiveInstruction(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	h.memory = newConversationMemory()
	userID := "desktop-user"

	h.memory.save(userID, []conversationEntry{{Role: "user", Content: "hello"}})
	promptBeforeClear := h.buildSystemPromptWithMemory("follow up", len(h.memory.load(userID)) == 0)
	assertContainsNone(t, promptBeforeClear, []string{"主动调用 " + corememory.PromptActionSaveColon})

	h.memory.clear(userID)
	promptAfterClear := h.buildSystemPromptWithMemory("new topic", len(h.memory.load(userID)) == 0)
	assertContainsAll(t, promptAfterClear, []string{
		corememory.PromptSectionUserMemory,
		corememory.BuildIMMemoryGuidePrompt(),
	})
}

func TestSystemPrompt_TrialReflectEnabled_InProMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.TrialReflectEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.app = app
	prompt := h.buildSystemPrompt()

	assertContainsAll(t, prompt, []string{
		"## 试错并反思模式",
		"先提出当前最有可能成立的假设",
		"不要机械重复同样的失败动作",
	})
}

func TestSystemPrompt_TrialReflectDisabled_OutsideProMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "lite"
	cfg.TrialReflectEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.app = app
	prompt := h.buildSystemPrompt()

	assertContainsNone(t, prompt, []string{
		"## 试错并反思模式",
		"不要机械重复同样的失败动作",
	})
}

func TestSystemPrompt_NoMemoryStore_NoProactiveInstruction(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})
	prompt := h.buildSystemPrompt()
	assertContainsNone(t, prompt, []string{corememory.PromptSectionProactiveMemory})
}
