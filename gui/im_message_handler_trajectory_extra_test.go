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
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestRunAgentLoopShared_TrajectoryLoggingRecordsConversationAndTools(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"done via shared\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
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
		Name: "bash", Description: "test bash", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "tool ok"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-shared-trajectory", 4, server.Client())
	loopCtx.Kind = LoopKindChat
	resp := h.runAgentLoopShared(loopCtx, "u1", "system prompt", nil, "run shared trajectory", nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoopShared error = %q", resp.Error)
	}
	if resp.Text != "done via shared" {
		t.Fatalf("resp.Text = %q, want done via shared", resp.Text)
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
	if session.Kind != "shared" {
		t.Fatalf("Kind = %q, want shared", session.Kind)
	}
	if session.Status != "success" {
		t.Fatalf("Status = %q, want success", session.Status)
	}
	if session.Iterations < 2 {
		t.Fatalf("Iterations = %d, want >= 2", session.Iterations)
	}
	if session.ToolCallCount < 1 {
		t.Fatalf("ToolCallCount = %d, want >= 1", session.ToolCallCount)
	}
	if session.InputTokens <= 0 || session.OutputTokens <= 0 {
		t.Fatalf("tokens = %d/%d, want positive loop totals from HistoryDelta usage", session.InputTokens, session.OutputTokens)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, entry := range session.Entries {
		roles = append(roles, entry.Role)
	}
	wantRoles := []string{"system", "user", "assistant", "tool", "tool_result", "assistant"}
	if strings.Join(roles, ",") != strings.Join(wantRoles, ",") {
		t.Fatalf("roles = %v, want %v", roles, wantRoles)
	}
	if session.Entries[2].FinishReason != "tool_calls" {
		t.Fatalf("first assistant finish_reason = %q, want tool_calls", session.Entries[2].FinishReason)
	}
	if session.Entries[2].Iteration != 1 {
		t.Fatalf("first assistant iteration = %d, want 1", session.Entries[2].Iteration)
	}
	if session.Entries[4].Role != "tool_result" || session.Entries[4].ToolName != "bash" {
		t.Fatalf("tool_result = %+v", session.Entries[4])
	}
	if session.Entries[4].ToolOutcome != "succeeded" {
		t.Fatalf("tool_result outcome = %q, want succeeded", session.Entries[4].ToolOutcome)
	}
	if content, ok := session.Entries[5].Content.(string); !ok || content != "done via shared" {
		t.Fatalf("final assistant content = %#v", session.Entries[5].Content)
	}
	if session.Entries[5].FinishReason != "stop" {
		t.Fatalf("final assistant finish_reason = %q, want stop", session.Entries[5].FinishReason)
	}
	if session.Entries[5].Iteration != 2 {
		t.Fatalf("final assistant iteration = %d, want 2", session.Entries[5].Iteration)
	}
}

func TestRunAgentLoop_TrajectoryLoggingRecordsPriorHistory(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
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
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 2
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())

	history := []agent.ConversationEntry{
		{Role: "user", Content: "prior question"},
		{Role: "assistant", Content: "prior answer"},
	}
	loopCtx := NewLoopContext("chat-history-trajectory", 2, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", history, "new question", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil || resp.Error != "" {
		t.Fatalf("resp=%+v", resp)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, _ := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, e := range session.Entries {
		roles = append(roles, e.Role)
	}
	wantPrefix := []string{"system", "user", "assistant", "user", "assistant"}
	if len(roles) < len(wantPrefix) {
		t.Fatalf("roles = %v, want prefix %v", roles, wantPrefix)
	}
	for i, w := range wantPrefix {
		if roles[i] != w {
			t.Fatalf("roles = %v, want prefix %v", roles, wantPrefix)
		}
	}
	if content, ok := session.Entries[1].Content.(string); !ok || content != "prior question" {
		t.Fatalf("prior user = %#v", session.Entries[1].Content)
	}
	if content, ok := session.Entries[2].Content.(string); !ok || content != "prior answer" {
		t.Fatalf("prior assistant = %#v", session.Entries[2].Content)
	}
}

func TestRunAgentLoop_TrajectoryAskUserClosesSiblingTools(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// ask_user first; bash sibling must be closed as paused when the loop early-returns.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-ask\",\"type\":\"function\",\"function\":{\"name\":\"ask_user\",\"arguments\":\"{\\\"question\\\":\\\"OK?\\\",\\\"input_type\\\":\\\"confirm\\\"}\"}},{\"index\":1,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"echo x\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\n"))
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
	cfg.LLMTrajectoryLogging = true
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 3
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name: "ask_user", Description: "ask", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolAskUser(args)
		},
	}); err != nil {
		t.Fatalf("Register ask_user: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name: "bash", Description: "bash", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			t.Fatal("sibling bash must not run after ask_user pause")
			return ""
		},
	}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}

	loopCtx := NewLoopContext("chat-main-ask-traj", 3, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", nil, "ask me", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.ResponseSource != imResponseSourceAskUser.String() {
		t.Fatalf("ResponseSource=%q want ask_user", resp.ResponseSource)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Kind != "main" {
		t.Fatalf("Kind=%q", session.Kind)
	}
	if session.Status != "paused" {
		t.Fatalf("Status=%q want paused", session.Status)
	}
	var askOK, bashClosed bool
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call-ask":
			askOK = true
			if e.ToolName != "ask_user" || e.ToolOutcome != "paused" {
				t.Fatalf("ask_user result = %+v", e)
			}
		case "call-bash":
			bashClosed = true
			if e.ToolOutcome != "paused" {
				t.Fatalf("sibling close = %+v", e)
			}
		}
	}
	if !askOK || !bashClosed {
		t.Fatalf("pairing incomplete ask=%v bash=%v entries=%+v", askOK, bashClosed, session.Entries)
	}
}

func TestRunAgentLoop_TrajectoryCancelClosesUnpairedTools(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"echo a\\\"}\"}},{\"index\":1,\"id\":\"call-b\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"x.go\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
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
	cfg.LLMTrajectoryLogging = true
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 4
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	loopCtx := NewLoopContext("chat-main-cancel-traj", 4, server.Client())
	if err := h.registry.Register(RegisteredTool{
		Name: "bash", Description: "test bash", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			loopCtx.Cancel()
			return "a ok"
		},
	}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name: "read_file", Description: "test read", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			t.Fatal("second tool should not run after cancel")
			return ""
		},
	}); err != nil {
		t.Fatalf("Register read_file: %v", err)
	}

	resp := h.runAgentLoop(loopCtx, "u1", "system prompt", nil, "cancel mid batch", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Text)), "task cancelled") &&
		!strings.Contains(strings.ToLower(resp.Error), "cancel") {
		t.Fatalf("expected cancel response, got text=%q err=%q", resp.Text, resp.Error)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Kind != "main" {
		t.Fatalf("Kind=%q want main", session.Kind)
	}
	if session.Status != "cancelled" {
		t.Fatalf("Status=%q want cancelled", session.Status)
	}
	var foundA, foundB bool
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call-a":
			foundA = true
		case "call-b":
			foundB = true
			if e.ToolOutcome != "cancelled" && e.Content != "cancelled" {
				t.Fatalf("unpaired close = %+v", e)
			}
		}
	}
	if !foundA || !foundB {
		t.Fatalf("pairing incomplete foundA=%v foundB=%v entries=%+v", foundA, foundB, session.Entries)
	}
}

func TestRunAgentLoopShared_TrajectoryCancelClosesUnpairedTools(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// One assistant round with two parallel tool calls; cancel after the first runs.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-a\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"echo a\\\"}\"}},{\"index\":1,\"id\":\"call-b\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"x.go\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":6,\"total_tokens\":18}}\n\n"))
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
	cfg.LLMTrajectoryLogging = true
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 4
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	loopCtx := NewLoopContext("chat-shared-cancel-traj", 4, server.Client())
	loopCtx.Kind = LoopKindChat
	if err := h.registry.Register(RegisteredTool{
		Name: "bash", Description: "test bash", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			// Cancel after first tool so the second tool_call never executes.
			loopCtx.Cancel()
			return "a ok"
		},
	}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name: "read_file", Description: "test read", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			t.Fatal("second tool should not run after cancel")
			return ""
		},
	}); err != nil {
		t.Fatalf("Register read_file: %v", err)
	}

	resp := h.runAgentLoopShared(loopCtx, "u1", "system prompt", nil, "cancel mid batch", nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	// Legacy cancel uses Text "Task cancelled..." rather than Error.
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Text)), "task cancelled") &&
		!strings.Contains(strings.ToLower(resp.Error), "cancel") {
		t.Fatalf("expected cancel response, got text=%q err=%q", resp.Text, resp.Error)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Kind != "shared" {
		t.Fatalf("Kind=%q", session.Kind)
	}
	if session.Status != "cancelled" {
		t.Fatalf("Status=%q want cancelled", session.Status)
	}
	// Expect synthetic close for call-b.
	var foundA, foundB bool
	for _, e := range session.Entries {
		if e.Role != "tool_result" {
			continue
		}
		switch e.ToolCallID {
		case "call-a":
			foundA = true
		case "call-b":
			foundB = true
			if e.ToolOutcome != "cancelled" && e.Content != "cancelled" {
				t.Fatalf("unpaired close = %+v", e)
			}
		}
	}
	if !foundA {
		t.Fatalf("missing result for call-a: %+v", session.Entries)
	}
	if !foundB {
		t.Fatalf("missing synthetic cancelled result for call-b: %+v", session.Entries)
	}
}

func TestRunAgentLoopShared_TrajectoryAskUserPausedPairsResult(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-ask\",\"type\":\"function\",\"function\":{\"name\":\"ask_user\",\"arguments\":\"{\\\"question\\\":\\\"Pick one?\\\",\\\"options\\\":[\\\"A\\\",\\\"B\\\"],\\\"input_type\\\":\\\"choice\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4,\"total_tokens\":13}}\n\n"))
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
	cfg.LLMTrajectoryLogging = true
	cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name: "Custom1", URL: server.URL, Model: "test-model", Protocol: "openai",
		IsCustom: true, AuthType: "none", ContextLength: 16000,
	}}
	cfg.MaclawLLMCurrentProvider = "Custom1"
	cfg.MaclawAgentMaxIterations = 3
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetTrajectoryRecorderFactory(app.buildTrajectoryRecorderFactory())
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name: "ask_user", Description: "ask", Category: ToolCategoryBuiltin,
		Status: RegToolAvailable, Source: "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return h.toolAskUser(args)
		},
	}); err != nil {
		t.Fatalf("Register ask_user: %v", err)
	}

	loopCtx := NewLoopContext("chat-shared-ask-traj", 3, server.Client())
	loopCtx.Kind = LoopKindChat
	resp := h.runAgentLoopShared(loopCtx, "u1", "system prompt", nil, "please ask me", nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("error=%q", resp.Error)
	}
	if resp.ResponseSource != imResponseSourceAskUser.String() {
		t.Fatalf("ResponseSource=%q want ask_user", resp.ResponseSource)
	}

	trajDir := filepath.Join(tempHome, ".maclaw", "trajectories")
	entries, err := os.ReadDir(trajDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("traj files err=%v n=%d", err, len(entries))
	}
	data, err := os.ReadFile(filepath.Join(trajDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var session TrajectorySession
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	if session.Kind != "shared" {
		t.Fatalf("Kind=%q", session.Kind)
	}
	if session.Status != "paused" {
		t.Fatalf("Status=%q want paused", session.Status)
	}
	roles := make([]string, 0, len(session.Entries))
	for _, e := range session.Entries {
		roles = append(roles, e.Role)
	}
	// system + user + assistant + expanded tool + early-stop tool_result
	want := []string{"system", "user", "assistant", "tool", "tool_result"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles=%v want %v", roles, want)
	}
	last := session.Entries[len(session.Entries)-1]
	if last.ToolCallID != "call-ask" || last.ToolName != "ask_user" {
		t.Fatalf("early-stop result = %+v", last)
	}
	if last.ToolOutcome != "paused" {
		t.Fatalf("ToolOutcome=%q want paused", last.ToolOutcome)
	}
	if session.Entries[2].FinishReason != "tool_calls" {
		t.Fatalf("assistant finish_reason=%q", session.Entries[2].FinishReason)
	}
}
