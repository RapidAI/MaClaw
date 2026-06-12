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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tool"
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
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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

func TestRunAgentLoop_TrajectoryLoggingRecordsEmptyFinalRecoverFlow(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"recovered summary\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
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
	loopCtx := NewLoopContext("chat-empty-trajectory", 4, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", nil, "run trajectory", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "recovered summary" {
		t.Fatalf("resp.Text = %q, want recovered summary", resp.Text)
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
	roles := make([]string, 0, len(session.Entries))
	for _, entry := range session.Entries {
		roles = append(roles, entry.Role)
	}
	wantRoles := []string{"system", "user", "assistant", "system", "assistant"}
	if strings.Join(roles, ",") != strings.Join(wantRoles, ",") {
		t.Fatalf("roles = %v, want %v", roles, wantRoles)
	}
	if content, ok := session.Entries[2].Content.(string); !ok || content != "" {
		t.Fatalf("first assistant content = %#v, want empty string", session.Entries[2].Content)
	}
	if content, ok := session.Entries[3].Content.(string); !ok || !strings.Contains(content, "no visible result") {
		t.Fatalf("recover system content = %#v, want empty-result recover guidance", session.Entries[3].Content)
	}
	if content, ok := session.Entries[4].Content.(string); !ok || content != "recovered summary" {
		t.Fatalf("final assistant content = %#v, want recovered summary", session.Entries[4].Content)
	}
}

func TestRunAgentLoop_TrajectoryLoggingRecordsPendingSkillRunRecoverReplay(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-run-skill\",\"type\":\"function\",\"function\":{\"name\":\"run_skill\",\"arguments\":\"{\\\"skill_name\\\":\\\"long_writer\\\",\\\"input\\\":\\\"draft report\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"total_tokens\":20}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"继续添加第7-8节和参考文献：\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":8,\"total_tokens\":17}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-get-skill-run\",\"type\":\"function\",\"function\":{\"name\":\"get_skill_run\",\"arguments\":\"{\\\"run_id\\\":\\\"run-1775734674900-1\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 5:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"第7-8节和参考文献已补充完成。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":8,\"total_tokens\":18}}\n\n"))
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
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:          "Custom1",
		URL:           server.URL,
		Model:         "test-model",
		Protocol:      "openai",
		IsCustom:      true,
		AuthType:      "none",
		ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 8
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "run_skill",
		Description: "test run skill",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return `{"run_id":"run-1775734674900-1","status":"running","skill_name":"long_writer"}`
		},
	}); err != nil {
		t.Fatalf("Register run_skill tool: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "get_skill_run",
		Description: "test get skill run",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return `{"run_id":"run-1775734674900-1","status":"success","result":"Part 3 (sections 4-6) appended successfully"}`
		},
	}); err != nil {
		t.Fatalf("Register get_skill_run tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-pending-skill-trajectory", 8, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", nil, "run trajectory", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "第7-8节和参考文献已补充完成。" {
		t.Fatalf("resp.Text = %q, want final completed content", resp.Text)
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

	roles := make([]string, 0, len(session.Entries))
	for _, entry := range session.Entries {
		roles = append(roles, entry.Role)
	}
	wantRoles := []string{"system", "user", "assistant", "tool", "tool_result", "assistant", "system", "assistant", "system", "assistant", "tool", "tool_result", "assistant"}
	if strings.Join(roles, ",") != strings.Join(wantRoles, ",") {
		t.Fatalf("roles = %v, want %v", roles, wantRoles)
	}

	if content, ok := session.Entries[5].Content.(string); !ok || content != "" {
		t.Fatalf("empty assistant content = %#v, want empty string", session.Entries[5].Content)
	}
	if content, ok := session.Entries[7].Content.(string); !ok || content != "继续添加第7-8节和参考文献：" {
		t.Fatalf("continuation fragment content = %#v, want trajectory fragment", session.Entries[7].Content)
	}
	if content, ok := session.Entries[12].Content.(string); !ok || content != "第7-8节和参考文献已补充完成。" {
		t.Fatalf("final assistant content = %#v, want completed content", session.Entries[12].Content)
	}

	recoverContents := make([]string, 0, 2)
	for _, idx := range []int{6, 8} {
		content, ok := session.Entries[idx].Content.(string)
		if !ok {
			t.Fatalf("recover entry %d content type = %T, want string", idx, session.Entries[idx].Content)
		}
		recoverContents = append(recoverContents, content)
	}
	if !strings.Contains(recoverContents[0], `get_skill_run(run_id="run-1775734674900-1")`) {
		t.Fatalf("first recover content = %q, want pending skill run guidance", recoverContents[0])
	}
	if !strings.Contains(recoverContents[1], `get_skill_run(run_id="run-1775734674900-1")`) {
		t.Fatalf("second recover content = %q, want pending skill run guidance", recoverContents[1])
	}

	toolPayload, ok := session.Entries[10].Content.(map[string]interface{})
	if !ok {
		t.Fatalf("tool entry content type = %T, want map[string]interface{}", session.Entries[10].Content)
	}
	if toolPayload["name"] != "get_skill_run" {
		t.Fatalf("tool name = %v, want get_skill_run", toolPayload["name"])
	}
	if session.Entries[10].ToolCallID != "call-get-skill-run" {
		t.Fatalf("tool entry ToolCallID = %q, want call-get-skill-run", session.Entries[10].ToolCallID)
	}
	if session.Entries[11].ToolCallID != "call-get-skill-run" {
		t.Fatalf("tool_result ToolCallID = %q, want call-get-skill-run", session.Entries[11].ToolCallID)
	}
	if content, ok := session.Entries[11].Content.(string); !ok || !strings.Contains(content, "Part 3 (sections 4-6) appended successfully") {
		t.Fatalf("tool_result content = %#v, want appended-successfully status", session.Entries[11].Content)
	}
}
