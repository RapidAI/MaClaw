package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

func TestNewIMMessageHandlerStandalone_MinimalConfig(t *testing.T) {
	// Minimal config — only LLM config is truly required for the agent to function.
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{
				URL:   "http://localhost:8080/v1",
				Model: "test-model",
				Key:   "test-key",
			}
		},
	})
	defer h.memory.Stop()

	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.app != nil {
		t.Fatal("standalone handler should have nil app")
	}
	if h.registry == nil {
		t.Fatal("expected non-nil tool registry")
	}
	if h.memory == nil {
		t.Fatal("expected non-nil conversation memory")
	}
	if h.client == nil {
		t.Fatal("expected non-nil HTTP client")
	}
}

func TestNewIMMessageHandlerStandalone_AccessorsWork(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
		},
		MaxIterationsFunc: func() int { return 50 },
	})
	defer h.memory.Stop()

	// LLM config accessor
	cfg := h.getMaclawLLMConfig()
	if cfg.URL != "http://test" {
		t.Errorf("expected URL=http://test, got %q", cfg.URL)
	}

	// LLM configured check
	if !h.isMaclawLLMConfigured() {
		t.Error("expected LLM to be configured")
	}

	// Max iterations
	if n := h.getMaclawAgentMaxIterations(); n != 50 {
		t.Errorf("expected 50 iterations, got %d", n)
	}

	// Pro mode defaults to true
	if !h.isProMode() {
		t.Error("expected pro mode to default to true")
	}

	// Nil accessors should return nil gracefully (not panic)
	if h.getWorkflowEngine() != nil {
		t.Error("expected nil workflow engine")
	}
	if h.getUnifiedClassifier() != nil {
		t.Error("expected nil unified classifier")
	}
	if h.getSkillExecutor() != nil {
		t.Error("expected nil skill executor")
	}
	if h.getAuditLog() != nil {
		t.Error("expected nil audit log")
	}
	if h.getAgentNetClient() != nil {
		t.Error("expected nil agentnet client")
	}
}

func TestHandleMemoryStatusCommandUsesStandaloneMemoryStore(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(corememory.Entry{
		Content:  "project uses memory status command",
		Category: corememory.CategoryProjectKnowledge,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	h := &IMMessageHandler{memoryStore: store}
	resp := h.handleMemoryStatusCommand()
	if resp == nil {
		t.Fatal("expected response")
	}
	if strings.Contains(resp.Text, "未初始化") {
		t.Fatalf("expected standalone memory store to be used, got %q", resp.Text)
	}
	if !strings.Contains(resp.Text, "项目知识") || !strings.Contains(resp.Text, "1条") {
		t.Fatalf("expected memory status summary, got %q", resp.Text)
	}
}

func TestHandleIMMessageHelpIncludesMemoryCommand(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
		},
	})
	defer h.memory.Stop()

	resp := h.HandleIMMessage(IMUserMessage{UserID: "tui-user", Platform: "tui", Text: "/help", Lang: "zh"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.Contains(resp.Text, "/memory") || !strings.Contains(resp.Text, "记忆状态") {
		t.Fatalf("expected /help to include /memory, got %q", resp.Text)
	}
}

func TestNewIMMessageHandlerStandalone_ShortChitChat(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
		},
	})
	defer h.memory.Stop()

	// Short chit-chat should return a direct reply without calling LLM.
	resp := h.HandleIMMessage(IMUserMessage{
		UserID:   "tui-user",
		Platform: "tui",
		Text:     "没事",
		Lang:     "zh",
	})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.Text == "" {
		t.Fatal("expected non-empty text for chit-chat")
	}
}
