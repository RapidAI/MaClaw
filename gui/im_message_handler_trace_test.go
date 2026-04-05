package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type loopTraceRequest struct {
	Messages []map[string]interface{} `json:"messages"`
}

func TestRunAgentLoop_TrialReflect_ClearsRepeatGuardAfterSuccess(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var (
		mu       sync.Mutex
		requests []loopTraceRequest
		callNum  int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req loopTraceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		callNum++
		currentCall := callNum
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch currentCall {
		case 1, 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"npm test\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"success after retry\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.TrialReflectEnabled = true
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
	cfg.MaclawAgentMaxIterations = 5
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	bashCalls := 0
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			bashCalls++
			if bashCalls == 1 {
				return "error: test failed"
			}
			return "success: completed"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trial retry loop", "desktop", "u1", "/project")
	loopCtx := NewLoopContext("chat-trial-retry", 5, server.Client())
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID

	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "run retry loop", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "success after retry" {
		t.Fatalf("resp.Text = %q, want success after retry", resp.Text)
	}
	loopCtx.SetState("completed")

	finalResp := h.finalizeTraceResult(loopCtx, resp, resp.Text, "")
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if finalResp.TraceEventCount != len(view.Events) {
		t.Fatalf("TraceEventCount = %d, want %d", finalResp.TraceEventCount, len(view.Events))
	}
	if finalResp.EvidenceCount != len(view.Evidence) {
		t.Fatalf("EvidenceCount = %d, want %d", finalResp.EvidenceCount, len(view.Evidence))
	}
	if finalResp.TrialReflectStatus != "recovered_success" {
		t.Fatalf("TrialReflectStatus = %q, want recovered_success", finalResp.TrialReflectStatus)
	}
	if finalResp.TrialReflectFailures != 1 {
		t.Fatalf("TrialReflectFailures = %d, want 1", finalResp.TrialReflectFailures)
	}
	if !strings.Contains(finalResp.TrialReflectSummary, "recovered after failure") {
		t.Fatalf("TrialReflectSummary = %q, want recovered after failure", finalResp.TrialReflectSummary)
	}

	repeatGuardCount := 0
	succeededObservationCount := 0
	failedObservationCount := 0
	for _, ev := range view.Evidence {
		switch {
		case ev.Category == "repeat_guard":
			repeatGuardCount++
		case ev.SourceKind == "trial_reflect" && ev.Category == "succeeded" && ev.Summary == "trial observation":
			succeededObservationCount++
		case ev.SourceKind == "trial_reflect" && ev.Category == "failed" && ev.Summary == "trial observation":
			failedObservationCount++
		}
	}
	if repeatGuardCount != 1 {
		t.Fatalf("repeat_guard evidence count = %d, want 1", repeatGuardCount)
	}
	if failedObservationCount != 1 {
		t.Fatalf("failed trial observation count = %d, want 1", failedObservationCount)
	}
	if succeededObservationCount != 1 {
		t.Fatalf("succeeded trial observation count = %d, want 1", succeededObservationCount)
	}

	observedSuccess := false
	for _, evt := range view.Events {
		if evt.Kind == "trial.observed" && strings.Contains(evt.Summary, "bash=succeeded") {
			observedSuccess = true
			break
		}
	}
	if !observedSuccess {
		t.Fatal("expected a succeeded trial.observed event after retry")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("LLM request count = %d, want 3", len(requests))
	}
	thirdMessages := requests[2].Messages
	foundSuccessReflectionNote := false
	for _, msg := range thirdMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "bash=succeeded") {
			foundSuccessReflectionNote = true
			break
		}
	}
	if !foundSuccessReflectionNote {
		t.Fatal("expected third LLM request to include success reflection note")
	}
}

func TestRunAgentLoop_TrialReflect_RestartsFailureCycleAfterSuccess(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var (
		mu       sync.Mutex
		requests []loopTraceRequest
		callNum  int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req loopTraceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		callNum++
		currentCall := callNum
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		switch currentCall {
		case 1, 2, 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"npm test\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"final after second failure\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.TrialReflectEnabled = true
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
	cfg.MaclawAgentMaxIterations = 6
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	bashCalls := 0
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			bashCalls++
			switch bashCalls {
			case 1:
				return "error: first failure"
			case 2:
				return "success: completed"
			case 3:
				return "error: second failure"
			default:
				return "success: unexpected"
			}
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trial cycle loop", "desktop", "u1", "/project")
	loopCtx := NewLoopContext("chat-trial-cycle", 6, server.Client())
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID

	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "run failure cycle loop", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "final after second failure" {
		t.Fatalf("resp.Text = %q, want final after second failure", resp.Text)
	}
	loopCtx.SetState("completed")

	finalResp := h.finalizeTraceResult(loopCtx, resp, resp.Text, "")
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if finalResp.TraceEventCount != len(view.Events) {
		t.Fatalf("TraceEventCount = %d, want %d", finalResp.TraceEventCount, len(view.Events))
	}
	if finalResp.EvidenceCount != len(view.Evidence) {
		t.Fatalf("EvidenceCount = %d, want %d", finalResp.EvidenceCount, len(view.Evidence))
	}
	if finalResp.TrialReflectStatus != "recovered_success" {
		t.Fatalf("TrialReflectStatus = %q, want recovered_success", finalResp.TrialReflectStatus)
	}
	if finalResp.TrialReflectFailures != 2 {
		t.Fatalf("TrialReflectFailures = %d, want 2", finalResp.TrialReflectFailures)
	}
	if !strings.Contains(finalResp.TrialReflectSummary, "recovered after failure") {
		t.Fatalf("TrialReflectSummary = %q, want recovered after failure", finalResp.TrialReflectSummary)
	}

	repeatGuardCount := 0
	failedObservationCount := 0
	succeededObservationCount := 0
	for _, ev := range view.Evidence {
		switch {
		case ev.Category == "repeat_guard":
			repeatGuardCount++
		case ev.SourceKind == "trial_reflect" && ev.Category == "failed" && ev.Summary == "trial observation":
			failedObservationCount++
		case ev.SourceKind == "trial_reflect" && ev.Category == "succeeded" && ev.Summary == "trial observation":
			succeededObservationCount++
		}
	}
	if repeatGuardCount != 2 {
		t.Fatalf("repeat_guard evidence count = %d, want 2", repeatGuardCount)
	}
	if failedObservationCount != 2 {
		t.Fatalf("failed trial observation count = %d, want 2", failedObservationCount)
	}
	if succeededObservationCount != 1 {
		t.Fatalf("succeeded trial observation count = %d, want 1", succeededObservationCount)
	}

	observedFailedCount := 0
	observedSucceededCount := 0
	for _, evt := range view.Events {
		if evt.Kind != "trial.observed" {
			continue
		}
		if strings.Contains(evt.Summary, "bash=failed") {
			observedFailedCount++
		}
		if strings.Contains(evt.Summary, "bash=succeeded") {
			observedSucceededCount++
		}
	}
	if observedFailedCount != 2 {
		t.Fatalf("failed trial.observed count = %d, want 2", observedFailedCount)
	}
	if observedSucceededCount != 1 {
		t.Fatalf("succeeded trial.observed count = %d, want 1", observedSucceededCount)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("LLM request count = %d, want 4", len(requests))
	}
	fourthMessages := requests[3].Messages
	foundSecondFailureReflectionNote := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "bash=failed") {
			foundSecondFailureReflectionNote = true
			break
		}
	}
	if !foundSecondFailureReflectionNote {
		t.Fatal("expected fourth LLM request to include second failure reflection note")
	}
}

func TestRunAgentLoop_EstimatesTokenUsageWhenStreamOmitsUsage(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"estimated usage reply\"},\"finish_reason\":null}]}\n\n"))
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

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	loopCtx := NewLoopContext("chat-estimated-usage", 2, server.Client())

	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "estimate token usage", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "estimated usage reply" {
		t.Fatalf("resp.Text = %q, want estimated usage reply", resp.Text)
	}
	if resp.InputTokens <= 0 {
		t.Fatalf("InputTokens = %d, want > 0", resp.InputTokens)
	}
	if resp.OutputTokens <= 0 {
		t.Fatalf("OutputTokens = %d, want > 0", resp.OutputTokens)
	}
	if resp.TotalTokens != resp.InputTokens+resp.OutputTokens {
		t.Fatalf("TotalTokens = %d, want %d", resp.TotalTokens, resp.InputTokens+resp.OutputTokens)
	}
	if len(resp.Fields) < 3 {
		t.Fatalf("fields len = %d, want at least 3", len(resp.Fields))
	}

	stat := app.GetLLMTokenUsage("Custom1")
	if stat.InputTokens != int64(resp.InputTokens) {
		t.Fatalf("stored InputTokens = %d, want %d", stat.InputTokens, resp.InputTokens)
	}
	if stat.OutputTokens != int64(resp.OutputTokens) {
		t.Fatalf("stored OutputTokens = %d, want %d", stat.OutputTokens, resp.OutputTokens)
	}
	if stat.TotalTokens != int64(resp.TotalTokens) {
		t.Fatalf("stored TotalTokens = %d, want %d", stat.TotalTokens, resp.TotalTokens)
	}
	if resp.Fields[0].Label != "Input tokens" || resp.Fields[1].Label != "Output tokens" || resp.Fields[2].Label != "Total tokens" {
		t.Fatalf("unexpected token field labels: %+v", resp.Fields)
	}
}

func TestDeriveLLMTokenUsage_FallsBackPerMissingSide(t *testing.T) {
	conversation := []interface{}{
		map[string]interface{}{"role": "user", "content": "请帮我总结这个页面的问题并给出修复建议"},
	}

	resp := &llmResponse{
		Choices: []llmChoice{{
			Message: llmMessage{Content: "这是模型返回的分析结果"},
		}},
		Usage: &llmUsage{
			PromptTokens: 0,
			OutputTokens: 23,
		},
	}

	input, output := deriveLLMTokenUsage(resp, conversation)
	if input <= 0 {
		t.Fatalf("input = %d, want > 0", input)
	}
	if output != 23 {
		t.Fatalf("output = %d, want 23", output)
	}

	resp2 := &llmResponse{
		Choices: []llmChoice{{
			Message: llmMessage{Content: "这是模型返回的分析结果"},
		}},
		Usage: &llmUsage{
			PromptTokens: 18,
			CompletionTokens: 0,
		},
	}
	input2, output2 := deriveLLMTokenUsage(resp2, conversation)
	if input2 != 18 {
		t.Fatalf("input2 = %d, want 18", input2)
	}
	if output2 <= 0 {
		t.Fatalf("output2 = %d, want > 0", output2)
	}
}

func TestRunAgentLoop_StreamDoneFiresForToolOnlyAndBonusRounds(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	callNum := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"echo status\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"bonus status\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":6,\"completion_tokens\":3,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", callNum)
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.UIMode = "pro"
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
	cfg.MaclawAgentMaxIterations = 1
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	manager := &RemoteSessionManager{
		app: app,
		sessions: map[string]*RemoteSession{
			"sess-active": {ID: "sess-active", Status: SessionRunning},
		},
	}
	h := NewIMMessageHandler(app, manager)
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			return "status ok"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	streamDoneCount := 0
	newRoundCount := 0
	loopCtx := NewLoopContext("chat-stream-done", 1, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "check session status", nil, nil, nil, func() {
		newRoundCount++
	}, func() {
		streamDoneCount++
	}, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("runAgentLoop error = %q", resp.Error)
	}
	if resp.Text != "bonus status" {
		t.Fatalf("resp.Text = %q, want bonus status", resp.Text)
	}
	if resp.Deferred != false {
		t.Fatalf("resp.Deferred = %v, want false", resp.Deferred)
	}
	if streamDoneCount != 2 {
		t.Fatalf("streamDoneCount = %d, want 2", streamDoneCount)
	}
	if newRoundCount != 1 {
		t.Fatalf("newRoundCount = %d, want 1", newRoundCount)
	}
	if callNum != 2 {
		t.Fatalf("LLM call count = %d, want 2", callNum)
	}
}

func TestFinalizeTraceResult_PersistsTrialReflectSummaryAndRefreshesCounts(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "finalize summary", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-finalize-summary", 3, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=failed command=npm test"})
	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=succeeded command=npm test"})
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "adaptive_retry", Category: "args", Summary: "retry decision", ContentSnippet: "invalid parameter"})
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "trial_reflect", Category: "repeat_guard", Summary: "avoid repeating failed actions", ContentSnippet: "bash"})

	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if resp.TrialReflectStatus != "recovered_success" {
		t.Fatalf("TrialReflectStatus = %q, want recovered_success", resp.TrialReflectStatus)
	}
	if resp.TrialReflectFailures != 1 {
		t.Fatalf("TrialReflectFailures = %d, want 1", resp.TrialReflectFailures)
	}
	if resp.EvidenceCount != len(view.Evidence) {
		t.Fatalf("EvidenceCount = %d, want %d", resp.EvidenceCount, len(view.Evidence))
	}
	if resp.TraceEventCount != len(view.Events) {
		t.Fatalf("TraceEventCount = %d, want %d", resp.TraceEventCount, len(view.Events))
	}
	foundSummaryEvidence := false
	for _, ev := range view.Evidence {
		if ev.SourceKind == "trial_reflect_summary" && ev.Category == "decision" && strings.Contains(ev.ContentSnippet, "recovered after failure") {
			foundSummaryEvidence = true
			break
		}
	}
	if !foundSummaryEvidence {
		t.Fatal("expected persisted trial_reflect_summary evidence")
	}
}

func TestFinalizeTraceResult_DoesNotPersistBenignTrialReflectSummary(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "finalize benign summary", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-finalize-benign", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=succeeded command=npm test"})

	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	if resp.TrialReflectStatus != "success" {
		t.Fatalf("TrialReflectStatus = %q, want success", resp.TrialReflectStatus)
	}
	if resp.TrialReflectFailures != 0 {
		t.Fatalf("TrialReflectFailures = %d, want 0", resp.TrialReflectFailures)
	}
	for _, ev := range view.Evidence {
		if ev.SourceKind == "trial_reflect_summary" {
			t.Fatalf("unexpected persisted trial_reflect_summary evidence: %#v", ev)
		}
	}
	if resp.EvidenceCount != len(view.Evidence) {
		t.Fatalf("EvidenceCount = %d, want %d", resp.EvidenceCount, len(view.Evidence))
	}
}

func TestBuildTraceEvidencePrompt_IncludesPersistedTrialReflectSummary(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Name: "project", Path: "/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trace recall", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-trace-recall", 3, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=failed command=npm test"})
	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=succeeded command=npm test"})
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "adaptive_retry", Category: "args", Summary: "retry decision", ContentSnippet: "invalid parameter"})
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "trial_reflect", Category: "repeat_guard", Summary: "avoid repeating failed actions", ContentSnippet: "bash"})

	h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	prompt := h.buildTraceEvidencePrompt("why did npm test fail before it recovered?")
	if !strings.Contains(prompt, "## 最近执行证据") {
		t.Fatalf("prompt = %q, want evidence header", prompt)
	}
	if !strings.Contains(prompt, "repeat guard avoided duplicate failed actions") {
		t.Fatalf("prompt = %q, want persisted trial-reflect strategy note", prompt)
	}
	if !strings.Contains(prompt, "recovered after failure") {
		t.Fatalf("prompt = %q, want recovered after failure detail", prompt)
	}
	if !strings.Contains(prompt, "retry decision") {
		t.Fatalf("prompt = %q, want retry decision evidence", prompt)
	}
}

func TestBuildTraceEvidencePrompt_SkipsBenignTrialReflectSummary(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Projects = []ProjectConfig{{Id: "p1", Name: "project", Path: "/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trace recall benign", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-trace-recall-benign", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "trial.observed", Title: "Trial outcome", Summary: "bash=succeeded command=npm test"})

	h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	prompt := h.buildTraceEvidencePrompt("npm test success")
	if strings.Contains(prompt, "trial-reflect summary") {
		t.Fatalf("prompt = %q, did not expect benign trial-reflect summary evidence", prompt)
	}
	if !strings.Contains(prompt, "Trial outcome") && prompt != "" {
		t.Fatalf("prompt = %q, want only normal evidence lines or empty", prompt)
	}
}

func TestFinalizeTraceResult_AddsVisibleFallbackForEmptyFailedRun(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "empty failed run", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-empty-failed", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("failed")

	h.traceService.AppendEvent(run.RunID, TraceEvent{Kind: "tool.failed", Title: "PDF generation failed", Summary: "未生成 PDF 文件"})

	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{}, "", "pdf generation failed")
	if resp.Text == "" {
		t.Fatal("expected fallback text for empty failed run")
	}
	if !strings.Contains(resp.Text, "任务未完成可交付结果") {
		t.Fatalf("resp.Text = %q, want failed fallback prefix", resp.Text)
	}
	if resp.RunID != run.RunID {
		t.Fatalf("RunID = %q, want %q", resp.RunID, run.RunID)
	}
	if resp.JobID != run.JobID {
		t.Fatalf("JobID = %q, want %q", resp.JobID, run.JobID)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Command != "__view_trace__ "+run.RunID {
		t.Fatalf("Actions = %#v, want trace action", resp.Actions)
	}
	if resp.TraceEventCount != 1 {
		t.Fatalf("TraceEventCount = %d, want 1", resp.TraceEventCount)
	}
	if resp.EvidenceCount != 0 {
		t.Fatalf("EvidenceCount = %d, want 0", resp.EvidenceCount)
	}
}

func TestFinalizeTraceResult_PreservesVisibleFileOnlyResult(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "file result", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-file-result", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{LocalFilePath: "/tmp/review.pdf", FileName: "review.pdf"}, "pdf ready", "")
	if resp.Text != "" {
		t.Fatalf("resp.Text = %q, want empty when file result is already visible", resp.Text)
	}
	if resp.LocalFilePath != "/tmp/review.pdf" {
		t.Fatalf("LocalFilePath = %q, want /tmp/review.pdf", resp.LocalFilePath)
	}
	if len(resp.Actions) != 0 {
		t.Fatalf("Actions = %#v, want none", resp.Actions)
	}
}

func TestRunAgentLoop_PromiseOnlyPDFReplyTriggersAnotherRound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好的，我马上基于已有数据生成论文综述 PDF。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"{\\\"path\\\":\\\"review.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", callNum)
		}
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	if err := h.registry.Register(RegisteredTool{
		Name:        "send_file",
		Description: "test file sender",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			return "[file_base64|review.pdf|application/pdf]JVBERi0xLjQKfake"
		},
	}); err != nil {
		t.Fatalf("Register send_file tool: %v", err)
	}

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "生成 pdf 发我"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("LocalFilePath empty, resp=%+v", resp)
	}
	if callNum < 2 {
		t.Fatalf("expected extra round after promise-only reply, callNum=%d", callNum)
	}
	view, ok := h.traceService.GetTrace(resp.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundNudge := false
	for _, evt := range view.Events {
		if evt.Kind == "delivery.nudged" {
			foundNudge = true
			break
		}
	}
	if !foundNudge {
		t.Fatalf("events = %#v, want delivery.nudged event", view.Events)
	}
}

func TestRunAgentLoop_PromiseOnlyDocumentReplyTriggersAnotherRound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好的，我马上整理一份报告并发给你。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"{\\\"path\\\":\\\"report.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", callNum)
		}
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	if err := h.registry.Register(RegisteredTool{
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

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "整理一份报告发我"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("LocalFilePath empty, resp=%+v", resp)
	}
	if callNum < 2 {
		t.Fatalf("expected extra round after promise-only report reply, callNum=%d", callNum)
	}
}

func TestRunAgentLoop_AutoExtendsLongChatBeforeHardLimit(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1, 2, 3, 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-web\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"hugging face daily papers\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 5:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"final summary after auto extend\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", callNum)
		}
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
	cfg.MaclawAgentMaxIterations = 300
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

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetDriftDetector(NewDriftDetector(8, 0.8))
	if err := h.registry.Register(RegisteredTool{
		Name:        "web_search",
		Description: "test web search",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			q, _ := args["query"].(string)
			return "results for " + q
		},
	}); err != nil {
		t.Fatalf("Register web_search tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-auto-extend", 300, server.Client())
	loopCtx.SetMaxIterations(4)
	job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "long task", "desktop", "desktop-user", "/project")
	loopCtx.JobID = job.JobID
	loopCtx.RunID = run.RunID

	resp := h.HandleIMMessageWithExistingLoop(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "长任务继续跑完"}, loopCtx, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "final summary after auto extend" {
		t.Fatalf("resp.Text = %q, want final summary after auto extend", resp.Text)
	}
	if loopCtx.MaxIterations() <= 4 {
		t.Fatalf("MaxIterations = %d, want auto extension beyond 4", loopCtx.MaxIterations())
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundExtended := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.extended" {
			foundExtended = true
			break
		}
	}
	if !foundExtended {
		t.Fatalf("events = %#v, want loop.extended event", view.Events)
	}
}

func TestRunAgentLoop_LongChainUsesGraceRoundsToFinishFileDelivery(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-web1\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"hugging face daily papers day 1\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-web2\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"hugging face daily papers day 2\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-web3\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"hugging face daily papers day 3\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"{\\\"path\\\":\\\"review.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			t.Fatalf("unexpected LLM call %d", callNum)
		}
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
	cfg.MaclawAgentMaxIterations = 3
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

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	if err := h.registry.Register(RegisteredTool{
		Name:        "web_search",
		Description: "test web search",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			q, _ := args["query"].(string)
			return "results for " + q
		},
	}); err != nil {
		t.Fatalf("Register web_search tool: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "send_file",
		Description: "test file sender",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress ProgressCallback) string {
			return "[file_base64|review.pdf|application/pdf]JVBERi0xLjQKfake"
		},
	}); err != nil {
		t.Fatalf("Register send_file tool: %v", err)
	}

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "搜索 hugging face daily papers 做综述，生成pdf"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("LocalFilePath empty, resp=%+v", resp)
	}
	if strings.Contains(resp.Text, "已达到最大推理轮次") {
		t.Fatalf("resp.Text = %q, did not expect generic iteration limit message", resp.Text)
	}
	if !strings.Contains(resp.TraceSummary, "文件 review.pdf 已准备好") {
		t.Fatalf("TraceSummary = %q, want readable file delivery summary", resp.TraceSummary)
	}
}
