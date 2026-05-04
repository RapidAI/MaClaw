package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
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

func TestHandleIMMessage_PlainTextReplyRestoresBoundQuestionContext(t *testing.T) {
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
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "omniroute stale current task"},
		{Role: "assistant", Content: "continuing OmniRoute update"},
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "\u6309\u4f60\u7684\u5efa\u8bae\u90e8\u7f72"})
	if resp == nil || resp.Error != "" {
		t.Fatalf("expected successful response, got %+v", resp)
	}
	if !strings.Contains(requestBody, "install-server-task") {
		t.Fatalf("expected restored server-install context in LLM request, got %s", requestBody)
	}
	if strings.Contains(requestBody, "omniroute stale current task") {
		t.Fatalf("stale OmniRoute context leaked into LLM request: %s", requestBody)
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
	if requestCount < 2 {
		t.Fatalf("expected pending-answer classifier plus agent request, got %d request(s)", requestCount)
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

	if !h.classifyPendingUserReplyPrompt("Which model should I deploy? I recommend qwen-server-model.") {
		t.Fatal("expected classifier-approved assistant question to create pending reply state")
	}
	if h.classifyPendingUserReplyPrompt("Task completed. Let me know if you need anything else.") {
		t.Fatal("classifier-rejected closing text must not create pending reply state")
	}
	if ok, classified := h.classifyPendingUserReplyAnswer("Which model should I deploy?", "use your recommendation"); !classified || !ok {
		t.Fatal("expected classifier-approved answer to continue pending reply")
	}
	if ok, classified := h.classifyPendingUserReplyAnswer("Which model should I deploy?", "deploy nginx to the server"); !classified || ok {
		t.Fatal("classifier-rejected fresh task must not be treated as a pending reply")
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

func TestHandleIMMessage_PlainTextReplyRestoreSignalsClearUI(t *testing.T) {
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
	if !resp.ClearUI {
		t.Fatalf("expected ClearUI when restoring pending reply context, got %+v", resp)
	}
}

func TestHandleIMMessage_PendingTextReplyDoesNotCaptureFreshDeploymentTask(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBody = string(body)
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
