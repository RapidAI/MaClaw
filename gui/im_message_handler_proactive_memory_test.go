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

func TestSystemPrompt_FirstTurn_DoesNotDumpWarehouseFacts(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	if err := h.memoryStore.Save(corememory.Entry{
		Content:  "User's production SSH host is old-warehouse.example",
		Category: corememory.CategoryUserFact,
	}); err != nil {
		t.Fatal(err)
	}
	prompt := h.buildSystemPromptWithMemory("hello", true)
	if strings.Contains(prompt, "old-warehouse.example") {
		t.Fatalf("first-turn prompt dumped warehouse text:\n%s", prompt)
	}
	assertContainsAll(t, prompt, []string{
		corememory.PromptSectionUserMemory,
		corememory.DefaultRecallHintForPrompt(),
		corememory.CatalogOnlyWorkingSetFooter(),
	})
}

func TestSystemPrompt_FirstTurn_ContainsProactiveMemoryInstruction(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	prompt := h.buildSystemPromptWithMemory("hello", true)

	assertContainsAll(t, prompt, []string{
		corememory.PromptSectionUserMemory,
		corememory.DefaultRecallHintForPrompt(),
		corememory.BuildIMMemoryGuidePrompt(),
	})
}

func TestSystemPrompt_NonFirstTurn_KeepsSessionStableMemoryGuide(t *testing.T) {
	h := newTestIMHandlerWithMemoryStore(t)
	// Build first-turn prompt so the frozen static memory snapshot is populated.
	first := h.buildSystemPromptWithMemory("hello", true)
	// Subsequent turns reuse the same session-stable snapshot (including the
	// memory management guide) so the LLM KV prefix for that block stays stable.
	prompt := h.buildSystemPromptWithMemory("hello again", false)

	assertContainsAll(t, first, []string{
		corememory.PromptSectionMemoryGuide,
		corememory.PromptActionSaveColon,
		corememory.PromptSaveCategorySummary,
	})
	assertContainsAll(t, prompt, []string{
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
	assertContainsNone(t, promptBeforeClear, []string{"娑撹濮╃拫鍐暏 " + corememory.PromptActionSaveColon})

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
	if strings.Contains(prompt, sourcePath) || strings.Contains(prompt, "keyboard controls and scoring rules") {
		t.Fatalf("catalog-only prompt must not dump recalled warehouse text, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, corememory.CatalogOnlyWorkingSetFooter()) {
		t.Fatalf("expected catalog-only working-set footer, got:\n%s", prompt)
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
	app.configCache = cfg
	app.configCacheValid = true

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
		"## 鐠囨洟鏁婇獮璺哄冀閹繃膩瀵?",
		"娑撳秷顩﹂張鐑橆潾闁插秴顦查崥灞剧壉閻ㄥ嫬銇戠拹銉ュЗ娴?",
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
		agent.PromptKnowledgeBaseRules,
		"knowledge_save_url",
		"knowledge_import_files",
		"knowledge_import_directory",
		"knowledge_save_text",
		"knowledge_context_pack",
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
	if strings.Contains(out, "[Scene Index]") || strings.Contains(out, "Build scene prompt support") {
		t.Fatalf("catalog-only recall must not dump scene bodies, got:\n%s", out)
	}
	if !strings.Contains(out, corememory.CatalogOnlyWorkingSetFooter()) {
		t.Fatalf("expected catalog-only working-set footer, got:\n%s", out)
	}
}
