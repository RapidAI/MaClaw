package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSendAIAssistantMessage_PromiseOnlyDeliverableReplyCompletesInSameRequest(t *testing.T) {
	t.Skip("runtime.EventsEmit requires a real Wails lifecycle context; handler/runtime behavior is covered by lower-level regressions")
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var (
		mu      sync.Mutex
		callNum int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callNum++
		currentCall := callNum
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch currentCall {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好的，我马上整理一份报告并发给你。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"{\\\"path\\\":\\\"report.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", currentCall)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	app.ctx = context.Background()
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	manager := NewRemoteSessionManager(app)
	app.remoteSessions = manager
	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	client.configureIMHandler = func(handler *IMMessageHandler) {
		handler.SetTraceService(NewAITraceService())
		if err := handler.registry.Register(RegisteredTool{
			Name:        "send_file",
			Description: "test file sender",
			Category:    ToolCategoryBuiltin,
			Status:      RegToolAvailable,
			Source:      "test",
			HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
				return "[file_base64|report.pdf|application/pdf]JVBERi0xLjQKfake"
			},
		}); err != nil {
			t.Fatalf("Register send_file tool: %v", err)
		}
	}

	resp, err := app.SendAIAssistantMessage(AIAssistantSendRequest{Text: "整理一份报告发我"})
	if err != nil {
		t.Fatalf("SendAIAssistantMessage: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("LocalFilePath empty, resp=%+v", resp)
	}
	if resp.TraceSummary == "" {
		t.Fatal("expected trace summary")
	}
	if callNum < 2 {
		t.Fatalf("expected promise-only deliverable reply to finish in same binding request, callNum=%d", callNum)
	}

	if _, err := json.Marshal(resp); err != nil {
		t.Fatalf("marshal resp: %v", err)
	}
}

func TestSendAIAssistantMessage_RejectsOversizedToolArguments_OpenAI(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	oversized := strings.Repeat("a", guiMaxToolArgumentsBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"" + oversized + "\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
	cfg.MaclawLLMProviders = []MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 2
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTraceService(NewAITraceService())
	loopCtx := NewLoopContext("chat-tool-overlimit", 2, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "发送文件", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.Contains(resp.Error, "tool arguments too large") {
		t.Fatalf("expected oversized tool args error, got %+v", resp)
	}
}

func TestSendAIAssistantMessage_RejectsOversizedToolArguments_Anthropic(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	oversized := strings.Repeat("a", guiMaxToolArgumentsBytes+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8}}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call-send-file\",\"name\":\"send_file\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"" + oversized + "\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":4}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "anthropic"
	cfg.MaclawLLMProviders = []MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "anthropic",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 2
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTraceService(NewAITraceService())
	loopCtx := NewLoopContext("chat-tool-overlimit-anthropic", 2, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "发送文件", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.Contains(resp.Error, "tool arguments too large") {
		t.Fatalf("expected oversized tool args error, got %+v", resp)
	}
}
