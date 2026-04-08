package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type trajectoryTestRequest struct {
	Messages []map[string]interface{} `json:"messages"`
}

func TestRunAgentLoop_TrajectoryLoggingRecordsConversationAndTools(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var mu sync.Mutex
	callNum := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req trajectoryTestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		callNum++
		currentCall := callNum
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		switch currentCall {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"echo hi\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", currentCall)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
	cfg.LLMTrajectoryLogging = true
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
	cfg.MaclawAgentMaxIterations = 4
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			return "tool ok"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-trajectory", 4, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", nil, "run trajectory", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "done" {
		t.Fatalf("resp.Text = %q, want done", resp.Text)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil {
		t.Fatalf("ReadDir trajectories: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("trajectory file count = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile trajectory: %v", err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatalf("unmarshal trajectory: %v", err)
	}
	if session.SessionID != "chat-trajectory" {
		t.Fatalf("SessionID = %q, want chat-trajectory", session.SessionID)
	}
	if session.Provider != "Custom1" {
		t.Fatalf("Provider = %q, want Custom1", session.Provider)
	}
	if session.Model != "test-model" {
		t.Fatalf("Model = %q, want test-model", session.Model)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, entry := range session.Entries {
		roles = append(roles, entry.Role)
	}
	wantRoles := []string{"system", "user", "assistant", "tool", "tool_result", "assistant"}
	if strings.Join(roles, ",") != strings.Join(wantRoles, ",") {
		t.Fatalf("roles = %v, want %v", roles, wantRoles)
	}
	if session.Entries[2].Role != "assistant" || session.Entries[2].Reasoning != "" {
		t.Fatalf("unexpected first assistant entry: %+v", session.Entries[2])
	}
	if session.Entries[2].ToolCalls == nil {
		t.Fatalf("expected assistant tool_calls to be recorded")
	}
	toolPayload, ok := session.Entries[3].Content.(map[string]interface{})
	if !ok {
		t.Fatalf("tool entry content type = %T, want map[string]interface{}", session.Entries[3].Content)
	}
	if toolPayload["name"] != "bash" {
		t.Fatalf("tool name = %v, want bash", toolPayload["name"])
	}
	if session.Entries[3].ToolCallID != "call-bash" {
		t.Fatalf("tool entry ToolCallID = %q, want call-bash", session.Entries[3].ToolCallID)
	}
	if session.Entries[4].ToolCallID != "call-bash" {
		t.Fatalf("tool_result ToolCallID = %q, want call-bash", session.Entries[4].ToolCallID)
	}
	if content, ok := session.Entries[4].Content.(string); !ok || content != "tool ok" {
		t.Fatalf("tool_result content = %#v, want \"tool ok\"", session.Entries[4].Content)
	}
	if content, ok := session.Entries[5].Content.(string); !ok || content != "done" {
		t.Fatalf("final assistant content = %#v, want \"done\"", session.Entries[5].Content)
	}
}
