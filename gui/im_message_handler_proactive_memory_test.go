package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	pathpkg "path/filepath"
	"strings"
	"testing"

	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func newTestIMHandlerWithMemoryStore(t *testing.T) *IMMessageHandler {
	t.Helper()
	tmpDir := t.TempDir()
	memPath := pathpkg.Join(tmpDir, "memories.json")
	ms, err := corememory.NewStore(memPath)
	if err != nil {
		t.Fatalf("corememory.NewStore: %v", err)
	}
	t.Cleanup(ms.Stop)

	h := newTestIMHandler(map[string]*RemoteSession{})
	h.memoryStore = ms
	return h
}

func TestSystemPrompt_IncludesMISDynamicAgentViewRules(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})
	prompt := h.buildSystemPrompt()

	assertContainsAll(t, prompt, []string{
		"## MIS Dynamic AgentView",
		"mis_data(action=\"resolve_intent\"",
		"mis_data(action=\"list_agent_transactions\")",
		"right-side AgentView",
		"directly operable UI",
		"Standard skills remain immutable",
	})
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
		"濡傞渶鏇村璁板繂锛屽彲閫氳繃 " + corememory.PromptActionRecallColon,
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
	h.memory = agent.NewConversationMemory()
	userID := "desktop-user"

	h.memory.Save(userID, []agent.ConversationEntry{{Role: "user", Content: "hello"}})
	promptBeforeClear := h.buildSystemPromptWithMemory("follow up", len(h.memory.Load(userID)) == 0)
	assertContainsNone(t, promptBeforeClear, []string{"涓诲姩璋冪敤 " + corememory.PromptActionSaveColon})

	h.memory.Clear(userID)
	promptAfterClear := h.buildSystemPromptWithMemory("new topic", len(h.memory.Load(userID)) == 0)
	assertContainsAll(t, promptAfterClear, []string{
		corememory.PromptSectionUserMemory,
		corememory.BuildIMMemoryGuidePrompt(),
	})
}
func TestSystemPrompt_ProactiveRecallIncludesSourceHint(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	h.lastUserID = desktopUserID
	sourcePath := `D:\workprj\snake\requirements.md`
	if err := h.memoryStore.Save(corememory.Entry{
		Content:    "Snake game requirements include keyboard controls and scoring rules",
		Category:   corememory.CategoryTaskArtifact,
		Tags:       []string{"snake", "requirements"},
		SourceType: "workflow_output",
		SourceURL:  sourcePath,
	}); err != nil {
		t.Fatal(err)
	}

	prompt := h.buildSystemPromptWithMemory("snake requirements keyboard scoring", false)
	if !strings.Contains(prompt, "source: "+sourcePath) {
		t.Fatalf("expected proactive recall source hint, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "full: read_file") {
		t.Fatalf("expected proactive recall drill-down hint, got:\n%s", prompt)
	}
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
		"## 璇曢敊骞跺弽鎬濇ā寮?",
		"鍏堟彁鍑哄綋鍓嶆渶鏈夊彲鑳芥垚绔嬬殑鍋囪",
		"涓嶈鏈烘閲嶅鍚屾牱鐨勫け璐ュ姩浣?",
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
		"## 璇曢敊骞跺弽鎬濇ā寮?",
		"涓嶈鏈烘閲嶅鍚屾牱鐨勫け璐ュ姩浣?",
	})
}

func TestSystemPrompt_NoMemoryStore_NoProactiveInstruction(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})
	prompt := h.buildSystemPrompt()
	assertContainsNone(t, prompt, []string{corememory.PromptSectionProactiveMemory})
}

func TestSystemPrompt_IncludesKnowledgeBaseTriggerRules(t *testing.T) {
	h := newTestIMHandler(map[string]*RemoteSession{})
	prompt := h.buildSystemPrompt()

	assertContainsAll(t, prompt, []string{
		"## 鐭ヨ瘑搴撳鑴戣鍒?",
		"knowledge_save_url",
		"knowledge_import_files",
		"knowledge_import_directory",
		"knowledge_save_text",
		"knowledge_context_pack",
		"涓嶈鍥犱负鐢ㄦ埛鍙槸璁╀綘",
	})
}

func TestSystemPrompt_ProactiveRecallIncludesSceneIndex(t *testing.T) {
	store, err := corememory.NewStore(pathpkg.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()
	if err := store.Save(corememory.Entry{
		Title:      "Requirements",
		Content:    "# Requirements\nBuild scene prompt support",
		Category:   corememory.CategoryTaskArtifact,
		Tags:       []string{"/home/user/project", "workflow:coding"},
		SourceType: "workflow_output_ref",
		SourceURL:  "/home/user/project/memory_refs/requirements.md",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	h := &IMMessageHandler{memoryStore: store}
	var b strings.Builder
	h.appendProactiveRecall(&b, "scene prompt", false)
	out := b.String()
	if !strings.Contains(out, "[Scene Index]") || !strings.Contains(out, "Requirements") || !strings.Contains(out, "full: read_file") {
		t.Fatalf("expected scene index in prompt, got:\n%s", out)
	}
}
