package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

type staticTaskClassifier string

func (s staticTaskClassifier) Classify(_, _ string, _ int) (string, error) {
	return string(s), nil
}

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

func TestNewIMMessageHandlerStandalone_DefaultResponseHeaderTimeout(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
		},
	})
	defer h.memory.Stop()

	transport, ok := h.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport = %T, want *http.Transport", h.client.Transport)
	}
	want := time.Duration(corelib.DefaultLLMTimeoutSec) * time.Second
	if transport.ResponseHeaderTimeout != want {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, want)
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

	// Nil optional accessors should return nil gracefully (not panic).
	if h.getUnifiedClassifier() == nil {
		t.Error("expected default unified classifier")
	}
	if h.getSkillExecutor() != nil {
		t.Error("expected nil skill executor")
	}
	if h.getAuditLog() != nil {
		t.Error("expected nil audit log")
	}
}

func TestEnsureSSHManagerIsPerHandler(t *testing.T) {
	newHandler := func() *IMMessageHandler {
		h := NewIMMessageHandlerStandalone(StandaloneConfig{
			LLMConfigFunc: func() corelib.MaclawLLMConfig {
				return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
			},
		})
		t.Cleanup(h.memory.Stop)
		return h
	}

	h1 := newHandler()
	h2 := newHandler()
	m1 := h1.ensureSSHManager()
	m2 := h2.ensureSSHManager()

	if m1 == nil || m2 == nil {
		t.Fatal("expected both handlers to initialize SSH managers")
	}
	if m1 == m2 {
		t.Fatal("SSH manager should be scoped to each handler")
	}
	if h1.bgTaskMgr == nil || h2.bgTaskMgr == nil {
		t.Fatal("expected SSH background task managers to initialize with SSH managers")
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

func TestHandleIMMessageClearUsesCurrentChineseLanguage(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://test", Model: "m", Key: "k"}
		},
	})
	defer h.memory.Stop()
	h.app = &App{CurrentLanguage: "zh-Hans"}

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "/clear"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "\u5bf9\u8bdd\u5df2\u91cd\u7f6e\u3002" {
		t.Fatalf("/clear response = %q", resp.Text)
	}
	if !resp.ClearUI {
		t.Fatalf("expected ClearUI for /clear, got %+v", resp)
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

func TestHandleIMMessage_TaskContextSwitchSignalsClearUI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + strconvQuote("新任务已经开始") + `},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()
	h.taskContextManager = agent.NewTaskContextManager(agent.TaskContextConfig{
		MaxArchivedTasks:         10,
		ActiveConversationWindow: -time.Second,
		LLMTimeout:               2 * time.Second,
	}, staticTaskClassifier("new"))
	h.memory.Save("u-clear", []agent.ConversationEntry{
		{Role: "user", Content: "旧任务：推荐一个大模型"},
		{Role: "assistant", Content: "建议部署 Qwen3.5-122B-A10B"},
	})

	resp := h.HandleIMMessage(IMUserMessage{
		UserID:   "u-clear",
		Platform: "desktop",
		Text:     "请开始一个完全不同的新话题，介绍 OmniRoute 更新发布流程",
	})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if !resp.ClearUI {
		t.Fatalf("expected ClearUI after task context switch, got %+v", resp)
	}
}

func TestHandleIMMessage_PlainTextReplyUsesCurrentBoundQuestionContext(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "pending question") && !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"deploying recommended model"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-text"
	boundHistory := []agent.ConversationEntry{
		{Role: "user", Content: "install-server-task: install an inference server"},
		{Role: "assistant", Content: "Which model should I deploy? I recommend qwen-server-model."},
	}
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "Which model should I deploy? I recommend qwen-server-model.",
		History:   cloneConversationEntries(boundHistory),
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) {
		return question == "Which model should I deploy? I recommend qwen-server-model." && answer != "", nil
	}
	h.memory.Save(userID, cloneConversationEntries(boundHistory))

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u6309\u4f60\u7684\u5efa\u8bae\u90e8\u7f72"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if !strings.Contains(requestBody, "install-server-task") {
		t.Fatalf("expected current server-install context in LLM request, got %s", requestBody)
	}
}

func TestHandleIMMessage_PlainTextReplyAcceptsCurrentHistoryExtension(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "pending question") && !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"deploying recommended model"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-text-extended"
	boundHistory := []agent.ConversationEntry{
		{Role: "user", Content: "install-server-task: install an inference server"},
		{Role: "assistant", Content: "Which model should I deploy? I recommend qwen-server-model."},
	}
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "Which model should I deploy? I recommend qwen-server-model.",
		History:   cloneConversationEntries(boundHistory),
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return true, nil }
	currentHistory := append(cloneConversationEntries(boundHistory), agent.ConversationEntry{Role: "tool", Content: "post-question tool note"})
	h.memory.Save(userID, currentHistory)

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u6309\u4f60\u7684\u5efa\u8bae\u90e8\u7f72"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if resp.ClearUI {
		t.Fatalf("current history extension should not be restored or clear UI, got %+v", resp)
	}
	if !strings.Contains(requestBody, "install-server-task") || !strings.Contains(requestBody, "post-question tool note") {
		t.Fatalf("expected extended current context in LLM request, got %s", requestBody)
	}
}
func TestHandleIMMessage_PendingReplyRejectsPrefixWithLaterUserTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "pending question") && !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"running current game"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-prefix-later-user"
	boundHistory := []agent.ConversationEntry{
		{Role: "user", Content: "server-task: check A100 status"},
		{Role: "assistant", Content: "Should I check the A100 server now?"},
	}
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "Should I check the A100 server now?",
		History:   cloneConversationEntries(boundHistory),
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return true, nil }
	currentHistory := append(cloneConversationEntries(boundHistory),
		agent.ConversationEntry{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		agent.ConversationEntry{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	)
	h.memory.Save(userID, currentHistory)

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u8fd0\u884c\u4e0b"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if strings.Contains(requestBody, "Should I check the A100 server now?") {
		t.Fatalf("prefix stale pending reply leaked into LLM request: %s", requestBody)
	}
	if !strings.Contains(requestBody, "game-task: create snake2") || !strings.Contains(requestBody, "\u8fd0\u884c\u4e0b") {
		t.Fatalf("expected current game context and run request, got %s", requestBody)
	}
}
func TestHandleIMMessage_StalePlainTextReplyDoesNotRestoreOldTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "pending question") && !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"running current game"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-stale-pending-text"
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question: "Should I check the A100 server now?",
		History: []agent.ConversationEntry{
			{Role: "user", Content: "server-task: check A100 status"},
			{Role: "assistant", Content: "Should I check the A100 server now?"},
		},
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return true, nil }
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u8fd0\u884c\u4e0b"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if strings.Contains(requestBody, "server-task: check A100 status") {
		t.Fatalf("stale server task leaked into LLM request: %s", requestBody)
	}
	if !strings.Contains(requestBody, "game-task: create snake2") || !strings.Contains(requestBody, "\u8fd0\u884c\u4e0b") {
		t.Fatalf("expected current game context and run request, got %s", requestBody)
	}
}

func TestHandleIMMessage_StaleAskUserReplyDoesNotForceContinueOldTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"running current game"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-stale-ask-user"
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question: "Check A100 server status?",
		History: []agent.ConversationEntry{
			{Role: "user", Content: "server-task: check A100 status"},
			{Role: "assistant", Content: "Check A100 server status?"},
		},
		Timestamp: time.Now(),
	})
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u8fd0\u884c\u4e0b"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if strings.Contains(requestBody, "Check A100 server status?") || strings.Contains(requestBody, "server-task: check A100 status") {
		t.Fatalf("stale ask_user context leaked into LLM request: %s", requestBody)
	}
	if !strings.Contains(requestBody, "game-task: create snake2") || !strings.Contains(requestBody, "\u8fd0\u884c\u4e0b") {
		t.Fatalf("expected current game context and run request, got %s", requestBody)
	}
}

func TestHandleIMMessage_UnboundPlainTextReplyDoesNotRestoreOldTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "pending question") && !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"running current game"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-unbound-pending-text"
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "Should I check the A100 server now?",
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return true, nil }
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u8fd0\u884c\u4e0b"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if strings.Contains(requestBody, "Should I check the A100 server now?") {
		t.Fatalf("unbound pending reply leaked into LLM request: %s", requestBody)
	}
	if !strings.Contains(requestBody, "game-task: create snake2") || !strings.Contains(requestBody, "\u8fd0\u884c\u4e0b") {
		t.Fatalf("expected current game context and run request, got %s", requestBody)
	}
}

func TestHandleIMMessage_UnboundAskUserDoesNotSuppressShortChitChatWithCurrentTask(t *testing.T) {
	var llmCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"should not be called"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-unbound-ask-user-chitchat"
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question:  "Check A100 server status?",
		Timestamp: time.Now(),
	})
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u6ca1\u4e8b", Lang: "zh"})
	if resp == nil || resp.Error != "" || resp.Text == "" {
		t.Fatalf("expected direct chit-chat response, got %+v", resp)
	}
	if llmCalled.Load() {
		t.Fatal("unbound stale ask_user should not suppress short chit-chat and call LLM")
	}
}
func TestHandleIMMessage_StaleAskUserDoesNotSuppressShortChitChat(t *testing.T) {
	var llmCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalled.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"should not be called"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-stale-ask-user-chitchat"
	h.pendingAskUser.Store(userID, &pendingAskUserState{
		Question: "Check A100 server status?",
		History: []agent.ConversationEntry{
			{Role: "user", Content: "server-task: check A100 status"},
			{Role: "assistant", Content: "Check A100 server status?"},
		},
		Timestamp: time.Now(),
	})
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u6ca1\u4e8b", Lang: "zh"})
	if resp == nil || resp.Error != "" || resp.Text == "" {
		t.Fatalf("expected direct chit-chat response, got %+v", resp)
	}
	if llmCalled.Load() {
		t.Fatal("stale ask_user should not suppress short chit-chat and call LLM")
	}
}
func TestHandleIMMessage_PlainTextStartAnswerKeepsPreviousTaskContext(t *testing.T) {
	var requestBody string
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "pending question") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"answer"},"finish_reason":"stop"}]}`))
			return
		}
		if !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"starting implementation"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-start-answer"
	boundHistory := []agent.ConversationEntry{
		{Role: "user", Content: "flight-game-task: create a C++ graphical airplane shooting game in D:\\workprj\\test2"},
		{Role: "assistant", Content: "I will use C++ with a graphical UI. If this is okay, tell me to start."},
	}
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question:  "I will use C++ with a graphical UI. If this is okay, tell me to start.",
		History:   cloneConversationEntries(boundHistory),
		Timestamp: time.Now(),
	})
	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u5f00\u5de5"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if requestCount < 1 {
		t.Fatalf("expected agent request, got %d request(s)", requestCount)
	}
	if !strings.Contains(requestBody, "flight-game-task") {
		t.Fatalf("expected previous airplane game context in LLM request, got %s", requestBody)
	}
	if strings.Contains(requestBody, "今天要做什么项目") {
		t.Fatalf("assistant lost task context and asked for a new project: %s", requestBody)
	}
}

func TestPendingUserReplyIntentClassifiersDriveBinding(t *testing.T) {
	h := &IMMessageHandler{}
	h.pendingReplyPromptClassifier = func(assistantText string) (bool, error) {
		return strings.Contains(assistantText, "deploy"), nil
	}
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) {
		return strings.Contains(question, "deploy") && strings.Contains(answer, "recommendation"), nil
	}

	if !h.classifyPendingUserReplyPrompt("desktop-user", "Which model should I deploy? I recommend qwen-server-model.") {
		t.Fatal("expected classifier-approved assistant question to create pending reply state")
	}
	if h.classifyPendingUserReplyPrompt("desktop-user", "Task completed. Let me know if you need anything else.") {
		t.Fatal("classifier-rejected closing text must not create pending reply state")
	}
	if ok, classified := h.classifyPendingUserReplyAnswer("desktop-user", "Which model should I deploy?", "use your recommendation"); !classified || !ok {
		t.Fatal("expected classifier-approved answer to continue pending reply")
	}
	if ok, classified := h.classifyPendingUserReplyAnswer("desktop-user", "Which model should I deploy?", "deploy nginx to the server"); !classified || ok {
		t.Fatal("classifier-rejected fresh task must not be treated as a pending reply")
	}
}

func TestPendingUserReplyPromptCandidateFiltersClosingStatements(t *testing.T) {
	if looksLikePendingUserReplyPromptCandidate("Task completed. Let me know if you need anything else.") {
		t.Fatal("generic closing statement should not require pending reply classification")
	}
	if !looksLikePendingUserReplyPromptCandidate("Please confirm before I deploy this change.") {
		t.Fatal("explicit confirmation request should be a pending reply candidate")
	}
	if !looksLikePendingUserReplyPromptCandidate("Which model should I deploy?") {
		t.Fatal("question should be a pending reply candidate")
	}
}

func TestPendingUserReplySkipsBrowserDebugInstructions(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	defer h.memory.Stop()
	h.pendingReplyPromptClassifier = func(text string) (bool, error) {
		return true, nil
	}

	debugPrompt := "Browser: 伯伯，您当前的 Chrome 还没有开启远程调试功能。请在 Chrome 地址栏输入 chrome://inspect/#remote-debugging，然后勾选 Allow remote debugging。"
	if got := sanitizePendingUserReplyQuestion(debugPrompt); strings.Contains(got, "Browser:") {
		t.Fatalf("role prefix not sanitized: %q", got)
	}
	if h.classifyPendingUserReplyPrompt("desktop-user", debugPrompt) {
		t.Fatal("browser remote-debugging instructions must not become pending reply context")
	}
}

func TestBuildAgentLoopAssistantTurnStripsRolePrefixFromReasoning(t *testing.T) {
	h := NewIMMessageHandlerStandalone(StandaloneConfig{})
	defer h.memory.Stop()

	turn := h.buildAgentLoopAssistantTurn(nil, llm.Choice{Message: llm.Message{
		Role:             "assistant",
		Content:          "Visible answer.",
		ReasoningContent: "thinking kept\nBrowser: hidden browser instruction",
	}})

	if strings.Contains(turn.Reasoning, "Browser:") {
		t.Fatalf("role prefix leaked in reasoning: %q", turn.Reasoning)
	}
	if turn.Reasoning != "thinking kept" {
		t.Fatalf("reasoning = %q, want sanitized reasoning", turn.Reasoning)
	}
	if turn.HistoryEntry.ReasoningContent != "thinking kept" {
		t.Fatalf("history reasoning = %q, want sanitized reasoning", turn.HistoryEntry.ReasoningContent)
	}
}

func TestPendingUserReplyBindingSurvivesTranscriptReconciliation(t *testing.T) {
	question := "请查看并确认需求是否准确，或提出修改意见。确认后我将进入技术设计阶段。"
	pendingHistory := []agent.ConversationEntry{
		{Role: "user", Content: "在 d:\\workprj\\testprj 下开发一个打地鼠游戏。"},
		{Role: "assistant", Content: "# 打地鼠游戏 - 需求文档\n" + question},
	}
	currentHistory := []agent.ConversationEntry{
		{Role: "user", Content: "北京天气"},
		{Role: "assistant", Content: "天气结果"},
		{Role: "user", Content: "在 d:\\workprj\\testprj 下开发一个打地鼠游戏。"},
		{Role: "assistant", Content: "# 打地鼠游戏 - 需求文档\n" + question},
	}

	pending, fresh := pendingUserReplyForCurrentHistory(&pendingUserReplyState{
		Question:  question,
		History:   pendingHistory,
		Timestamp: time.Now(),
	}, currentHistory)
	if pending == nil || !fresh {
		t.Fatal("pending reply should remain bound after client transcript reconciliation prepends older entries")
	}
}

func TestPendingUserReplyBindingRejectsQuestionWithLaterUserMessage(t *testing.T) {
	question := "Should I check the A100 server now?"
	currentHistory := []agent.ConversationEntry{
		{Role: "user", Content: "server-task: check A100 status"},
		{Role: "assistant", Content: question},
		{Role: "user", Content: "game-task: create snake2 in D:\\workprj\\snake2"},
		{Role: "assistant", Content: "Run it with .\\build\\Release\\snake2.exe"},
	}

	_, fresh := pendingUserReplyForCurrentHistory(&pendingUserReplyState{
		Question:  question,
		History:   []agent.ConversationEntry{{Role: "user", Content: "server-task: check A100 status"}, {Role: "assistant", Content: question}},
		Timestamp: time.Now(),
	}, currentHistory)
	if fresh {
		t.Fatal("pending reply must not bind after a later user message starts another task")
	}
}

func TestPendingUserReplyBindingRejectsShortSubstringMatch(t *testing.T) {
	currentHistory := []agent.ConversationEntry{
		{Role: "user", Content: "current task"},
		{Role: "assistant", Content: "ongoing work is complete"},
	}

	_, fresh := pendingUserReplyForCurrentHistory(&pendingUserReplyState{
		Question:  "go",
		Timestamp: time.Now(),
	}, currentHistory)
	if fresh {
		t.Fatal("short pending question must not bind by substring match")
	}
}

func TestPendingUserReplyAnswerAmbiguousIntentKeepsPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"probably answering"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-ambiguous"
	pending := &pendingUserReplyState{
		Question:  "Which option should I use?",
		History:   []agent.ConversationEntry{{Role: "user", Content: "choose option"}},
		Timestamp: time.Now(),
	}
	h.pendingUserReply.Store(userID, pending)

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "go"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if _, ok := h.pendingUserReply.Load(userID); !ok {
		t.Fatal("ambiguous pending-answer classification should keep pending context")
	}
}

func TestHandleIMMessage_StalePlainTextReplyDoesNotSignalClearUI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-clear"
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question: "Which model should I deploy?",
		History: []agent.ConversationEntry{
			{Role: "user", Content: "install server"},
			{Role: "assistant", Content: "Which model should I deploy?"},
		},
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return true, nil }
	h.memory.Save(userID, []agent.ConversationEntry{{Role: "user", Content: "stale task"}})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "go ahead"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if resp.ClearUI {
		t.Fatalf("stale pending reply must not restore old context or clear UI, got %+v", resp)
	}
}

func TestHandleIMMessage_PendingTextReplyDoesNotCaptureFreshDeploymentTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "Assistant message:") {
			requestBody = string(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"starting nginx deployment"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	h := NewIMMessageHandlerStandalone(StandaloneConfig{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: server.URL, Model: "test-model", Protocol: "openai"}
		},
		MaxIterationsFunc: func() int { return 1 },
	})
	defer h.memory.Stop()

	userID := "u-pending-fresh-task"
	h.pendingUserReply.Store(userID, &pendingUserReplyState{
		Question: "Which model should I deploy?",
		History: []agent.ConversationEntry{
			{Role: "user", Content: "install-server-task: choose model"},
			{Role: "assistant", Content: "Which model should I deploy?"},
		},
		Timestamp: time.Now(),
	})
	h.pendingReplyAnswerClassifier = func(question, answer string) (bool, error) { return false, nil }
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "current task context"},
		{Role: "assistant", Content: "ready"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u90e8\u7f72 nginx \u5230\u670d\u52a1\u5668"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if strings.Contains(requestBody, "install-server-task: choose model") {
		t.Fatalf("pending question context captured a fresh deployment task: %s", requestBody)
	}
	if !strings.Contains(requestBody, "\u90e8\u7f72 nginx \u5230\u670d\u52a1\u5668") {
		t.Fatalf("expected fresh deployment request in LLM request, got %s", requestBody)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
