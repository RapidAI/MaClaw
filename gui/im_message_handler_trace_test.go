package main

import (
	"encoding/base64"
	"encoding/json"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const testOnePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+yF9kAAAAASUVORK5CYII="

type loopTraceRequest struct {
	Messages []map[string]interface{} `json:"messages"`
}

func traceTrialObservedEvent(toolName string, outcome toolOutcome) TraceEvent {
	return TraceEvent{
		Kind:    "trial.observed",
		Title:   "Trial outcome",
		Summary: "command=npm test",
		ToolOutcomes: []TraceToolObservation{{
			ToolName: toolName,
			Outcome:  outcome.String(),
		}},
	}
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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

	resp := &llm.Response{
		Choices: []llm.Choice{{
			Message: llm.Message{Content: "这是模型返回的分析结果"},
		}},
		Usage: &llm.Usage{
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

	resp2 := &llm.Response{
		Choices: []llm.Choice{{
			Message: llm.Message{Content: "这是模型返回的分析结果"},
		}},
		Usage: &llm.Usage{
			PromptTokens:     18,
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

func TestRunAgentLoop_OrientSkillPreferenceInjectsRunSkillGuidance(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var (
		mu       sync.Mutex
		requests []loopTraceRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req loopTraceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我会优先使用现有技能完成这个任务。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "hf_daily_papers_report",
		Description: "把 HuggingFace Daily Papers 整理成报告并生成文件",
		Triggers:    []string{"daily papers", "综述", "报告", "pdf"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo skill"}}},
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "manual",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	loopCtx := NewLoopContext("chat-skill-prefer", 2, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成huggingface daily papers的综述，包括方法原理、关键创新、评论等。", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("expected at least one LLM request")
	}
	foundSkillGuidance := false
	for _, msg := range requests[0].Messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "manage_skill") && strings.Contains(content, "hf_daily_papers_report") {
			foundSkillGuidance = true
			break
		}
	}
	if !foundSkillGuidance {
		t.Fatalf("first request messages = %#v, want injected manage_skill guidance", requests[0].Messages)
	}
}

func TestRunAgentLoop_SkillFailureInjectsFallbackGuidance(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我会先用现有 Skill 试一下。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-run-skill\",\"type\":\"function\",\"function\":{\"name\":\"run_skill\",\"arguments\":\"{\\\"name\\\":\\\"hf_daily_papers_report\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"这个 Skill 失败了，我改用别的工具继续完成。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我会根据失败原因切换其他工具。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "hf_daily_papers_report",
		Description: "把 HuggingFace Daily Papers 整理成报告并生成文件",
		Triggers:    []string{"daily papers", "综述", "报告", "pdf"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo skill"}}},
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "manual",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "run_skill",
		Description: "run local skill",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "Skill 执行失败: skill execution stopped at step 1: command failed（run_id=run-2024）"
		},
	}); err != nil {
		t.Fatalf("Register run_skill tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-skill-fallback", 4, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "skill fallback", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成huggingface daily papers的综述，包括方法原理、关键创新、评论等。", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 4 {
		t.Fatalf("LLM request count = %d, want at least 4", len(requests))
	}
	fourthMessages := requests[3].Messages
	foundRecoverPrompt := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "[Recover 阶段]") && strings.Contains(content, "hf_daily_papers_report") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("fourth request messages = %#v, want recover guidance", fourthMessages)
	}
	foundConcreteRunID := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, `get_skill_run(run_id="run-2024")`) {
			foundConcreteRunID = true
			break
		}
	}
	if !foundConcreteRunID {
		t.Fatalf("fourth request messages = %#v, want concrete run_id guidance", fourthMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecoverEvent := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "skill_failed") {
			foundRecoverEvent = true
			break
		}
	}
	if !foundRecoverEvent {
		t.Fatalf("events = %#v, want recover trace event", view.Events)
	}
}

func TestRunAgentLoop_RunningSkillUsesConcreteRunIDGuidanceOnNextRound(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先启动这个 Skill。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-run-skill\",\"type\":\"function\",\"function\":{\"name\":\"run_skill\",\"arguments\":\"{\\\"name\\\":\\\"hf_daily_papers_report\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先等一下状态。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"继续观察 skill 状态。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "hf_daily_papers_report",
		Description: "把 HuggingFace Daily Papers 整理成报告并生成文件",
		Triggers:    []string{"daily papers", "综述", "报告", "pdf"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo skill"}}},
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "manual",
	}}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "run_skill",
		Description: "run local skill",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "✅ Skill 已启动\n## 运行信息\n- run_id: run-555\n- skill: hf_daily_papers_report\n- status: running\n## 下一步\n- 使用 get_skill_run(run_id) 继续观察执行进度。"
		},
	}); err != nil {
		t.Fatalf("Register run_skill tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-skill-running-guidance", 4, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成huggingface daily papers的综述，包括方法原理、关键创新、评论等。", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 4 {
		t.Fatalf("LLM request count = %d, want at least 4", len(requests))
	}
	fourthMessages := requests[3].Messages
	foundConcreteRunID := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, `get_skill_run(run_id="run-555")`) {
			foundConcreteRunID = true
			break
		}
	}
	if !foundConcreteRunID {
		t.Fatalf("fourth request messages = %#v, want concrete running run_id guidance", fourthMessages)
	}
}

func TestRunAgentLoop_DriftDetectionEntersRecoverPhase(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-web\",\"type\":\"function\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"hugging face daily papers\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4, 5:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"recover after drift\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetDriftDetector(NewDriftDetector(8, 0.8))
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "web_search",
		Description: "test web search",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "results for hugging face daily papers"
		},
	}); err != nil {
		t.Fatalf("Register web_search tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-drift-recover", 5, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "drift recover", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "搜索 hugging face daily papers 并整理结果", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "recover after drift" {
		t.Fatalf("resp.Text = %q, want recover after drift", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 4 {
		t.Fatalf("LLM request count = %d, want at least 4", len(requests))
	}
	fourthMessages := requests[3].Messages
	foundRecoverPrompt := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "[Recover 阶段]") && strings.Contains(content, "漂移") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("fourth request messages = %#v, want drift recover guidance", fourthMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecoverEvent := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "drift_detected") {
			foundRecoverEvent = true
			break
		}
	}
	if !foundRecoverEvent {
		t.Fatalf("events = %#v, want recover trace event", view.Events)
	}
}

func TestRunAgentLoop_TrialFailureEntersRecoverPhase(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"npm test\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我将按调整后的方案继续。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "error: npm test failed"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-trial-recover", 3, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trial recover", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "执行测试并修复失败", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 2 {
		t.Fatalf("LLM request count = %d, want at least 2", len(requests))
	}
	secondMessages := requests[1].Messages
	foundRecoverPrompt := false
	for _, msg := range secondMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "[Recover 阶段]") && strings.Contains(content, "失败观察") && strings.Contains(content, "bash=failed") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("second request messages = %#v, want trial recover guidance", secondMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecoverEvent := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "trial_failed") {
			foundRecoverEvent = true
			break
		}
	}
	if !foundRecoverEvent {
		t.Fatalf("events = %#v, want trial recover trace event", view.Events)
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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

func TestRunAgentLoop_NonDebugStillReportsBaseToolStageProgress(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-send-file\",\"type\":\"function\",\"function\":{\"name\":\"send_file\",\"arguments\":\"{\\\"path\\\":\\\"report.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawDebugToolCalls = false
	cfg.MaclawAgentMaxIterations = 2
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "send_file",
		Description: "test file sender",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			if onProgress != nil {
				onProgress("internal debug-only progress")
			}
			return "[file_base64|report.pdf|application/pdf]JVBERi0xLjQKfake"
		},
	}); err != nil {
		t.Fatalf("Register send_file tool: %v", err)
	}

	var progress []string
	loopCtx := NewLoopContext("chat-nondebug-progress", 2, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "发送文件", nil, func(msg string) {
		progress = append(progress, msg)
	}, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("expected LocalFilePath, got %+v", resp)
	}
	if len(progress) == 0 {
		t.Fatal("expected visible progress messages")
	}
	foundBaseStage := false
	for _, msg := range progress {
		if strings.Contains(msg, "正在整理并发送文件") {
			foundBaseStage = true
		}
		if strings.Contains(msg, "internal debug-only progress") {
			t.Fatalf("unexpected verbose internal tool progress in user-facing mode: %q", msg)
		}
	}
	if !foundBaseStage {
		t.Fatalf("progress = %#v, want visible base tool stage progress", progress)
	}
}

func TestRunAgentLoop_AttachesIntermediateScreenshotAndQRCodeURL(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "application/json")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_bash","type":"function","function":{"name":"bash","arguments":"{}"}},{"id":"call_screenshot","type":"function","function":{"name":"screenshot","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"扫码登录即可。"},"finish_reason":"stop"}]}`))
		}
	}))
	defer server.Close()

	app := &App{testHomeDir: tempHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
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
	cfg.MaclawAgentMaxIterations = 3
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	app.gateIntentClassifier = NewGateIntentClassifier(nil)

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.SetToolRegistry(NewToolRegistry())
	qrURL := "https://liteapp.weixin.qq.com/q/7GiQu1?qrcode=abc123&bot_type=3"
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "fake bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "URL: " + qrURL + "\n<terminal qr omitted>"
		},
	}); err != nil {
		t.Fatalf("Register bash tool: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "screenshot",
		Description: "fake screenshot",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "[screenshot_base64]" + testOnePixelPNGBase64
		},
	}); err != nil {
		t.Fatalf("Register screenshot tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-qr-screenshot", 3, server.Client())
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "部署 cc-connect 并给我二维码", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if !strings.Contains(resp.Text, qrURL) {
		t.Fatalf("response text missing QR URL: %q", resp.Text)
	}
	if resp.LocalFilePath == "" || resp.ThumbnailBase64 == "" {
		t.Fatalf("expected local screenshot preview, got %+v", resp)
	}
	if _, err := os.Stat(resp.LocalFilePath); err != nil {
		t.Fatalf("screenshot file not saved: %v", err)
	}
	if _, err := base64.StdEncoding.DecodeString(resp.ThumbnailBase64); err != nil {
		t.Fatalf("thumbnail is not base64: %v", err)
	}
}

func TestRunAgentLoop_NoToolStallEntersRecoverPhase(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先想想怎么做。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我再整理一下步骤。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"现在开始实际执行。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	loopCtx := NewLoopContext("chat-no-tool-stall", 4, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "stall recover", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "帮我整理结果并完成交付", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "现在开始实际执行。" {
		t.Fatalf("resp.Text = %q, want final third response", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 3 {
		t.Fatalf("LLM request count = %d, want at least 3", len(requests))
	}
	thirdMessages := requests[2].Messages
	foundRecoverPrompt := false
	for _, msg := range thirdMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "[Recover]") && strings.Contains(content, "No real tool was called for 2 consecutive rounds") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("third request messages = %#v, want no-tool stall recover guidance", thirdMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecoverEvent := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "no_tool_stall") {
			foundRecoverEvent = true
			break
		}
	}
	if !foundRecoverEvent {
		t.Fatalf("events = %#v, want no-tool stall recover trace event", view.Events)
	}
}

func TestRunAgentLoop_PendingSkillRunNoToolFragmentStaysInRecover(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-run-skill-1\",\"type\":\"function\",\"function\":{\"name\":\"run_skill\",\"arguments\":\"{\\\"name\\\":\\\"hf_daily_papers_report\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"继续添加第7-8节和参考文献：\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我已改为继续观察 skill 状态，并将在确认后继续执行。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = 5
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "run_skill",
		Description: "run local skill",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "✅ Skill 已启动\n## 运行信息\n- run_id: run-1775734674900-1\n- status: running\n## 下一步\n- 使用 get_skill_run(run_id) 继续观察执行进度。"
		},
	}); err != nil {
		t.Fatalf("Register run_skill tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-pending-run-fragment", 5, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "pending skill run fragment", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "继续完成文档交付", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text == "继续添加第7-8节和参考文献：" {
		t.Fatalf("resp.Text = %q, want recover to continue instead of finishing on fragment", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("LLM request count = %d, want 4", len(requests))
	}
	fourthMessages := requests[3].Messages
	foundRecoverPrompt := false
	for _, msg := range fourthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, `get_skill_run(run_id="run-1775734674900-1")`) {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("fourth request messages = %#v, want pending run recover guidance", fourthMessages)
	}
}

func TestHandleIMMessage_ResumeSlotUsesBoundResumeContext(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	h.memory.UpsertUnfinishedSlot("desktop-user", &agent.UnfinishedTaskSlot{
		SlotID:       "slot-old",
		UserID:       "desktop-user",
		ProjectPath:  "/project",
		Status:       "pending_resume",
		LastTask:     "继续未完成工作",
		ResumePrompt: "这里是旧任务恢复提示",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	if !h.memory.BindUnfinishedSlot("desktop-user", "slot-old") {
		t.Fatal("expected bindUnfinishedSlot to succeed")
	}

	prompt := h.buildResumeTraceContext("desktop-user", "fallback task")
	if !strings.Contains(prompt, "显式恢复未完成任务") {
		t.Fatalf("prompt = %q, want resume header", prompt)
	}
	if !strings.Contains(prompt, "继续未完成工作") {
		t.Fatalf("prompt = %q, want last task", prompt)
	}
	if !strings.Contains(prompt, "这里是旧任务恢复提示") {
		t.Fatalf("prompt = %q, want resume prompt", prompt)
	}
}

func TestHandleIMMessage_ResumeSlotBindsContextWithoutStartingSession(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	t.Setenv("AppData", filepath.Join(tempHome, "AppData", "Roaming"))

	projectDir := filepath.Join(tempHome, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(projectDir): %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"继续处理这个未完成任务。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
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
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Path: projectDir}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	provider := &fakeProviderAdapter{cmd: CommandSpec{Command: "claude.exe"}}
	manager := NewRemoteSessionManager(app)
	manager.providerFactory = func(tool string) (ProviderAdapter, error) {
		return provider, nil
	}
	manager.executionFactory = func(spec LaunchSpec) (ExecutionStrategy, error) {
		return &fakeExecutionStrategy{handle: newFakeExecutionHandle(203)}, nil
	}
	app.remoteSessions = manager
	app.sessionStarter = NewCodingSessionStarter(app)

	h := NewIMMessageHandler(app, manager)
	h.memory.UpsertUnfinishedSlot("desktop-user", &agent.UnfinishedTaskSlot{
		SlotID:       "slot-old",
		UserID:       "desktop-user",
		ProjectPath:  projectDir,
		Status:       "pending_resume",
		LastTask:     "继续未完成工作",
		Summary:      "继续未完成工作",
		ResumePrompt: "这里是旧任务恢复提示",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "继续这个未完成任务", ResumeSlotID: "slot-old"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if strings.Contains(resp.Text, "已启动未完成任务恢复会话") {
		t.Fatalf("resp.Text = %q, should not start recovery session", resp.Text)
	}
	if provider.lastSpec.Tool != "" {
		t.Fatalf("provider last spec = %#v, want zero value because no session should start", provider.lastSpec)
	}
	bound := h.memory.ActiveUnfinishedSlot("desktop-user")
	if bound == nil || bound.SlotID != "slot-old" {
		t.Fatalf("bound slot = %#v, want slot-old", bound)
	}
}

func TestHandleIMMessage_NewTaskAfterIncompleteRunClearsOldContext(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"知识库文件已处理完成。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawLLMUrl = server.URL
	cfg.MaclawLLMModel = "test-model"
	cfg.MaclawLLMProtocol = "openai"
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	userID := "desktop-user"
	h.memory.Save(userID, []agent.ConversationEntry{
		{Role: "user", Content: "搜索 huggingface daily papers，生成每日论文综述，生成pdf发我"},
		{Role: "assistant", Content: "(已达到最大推理轮次，请继续发送消息以完成任务)"},
	})
	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:       "slot-stale",
		UserID:       userID,
		Status:       "pending_resume",
		LastTask:     "继续 Daily Paper",
		Summary:      "还差最后一轮整理",
		ResumePrompt: "这里是旧任务恢复提示",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	})
	h.memory.BindUnfinishedSlot(userID, "slot-stale")

	resp := h.HandleIMMessage(IMUserMessage{UserID: userID, Platform: "desktop", Text: "现在帮我把桌面上的 AI 编程评测报告放入知识库", StartNewTask: true})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "知识库文件已处理完成。" {
		t.Fatalf("resp.Text = %q, want fresh task response", resp.Text)
	}
	if slot := h.memory.GetUnfinishedSlot(userID); slot != nil {
		t.Fatalf("unfinished slot = %#v, want nil after StartNewTask", slot)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) == 0 {
		t.Fatal("expected at least one LLM request")
	}
	firstMessages := requests[0].Messages
	for _, msg := range firstMessages {
		content, _ := msg["content"].(string)
		if strings.Contains(content, "Daily Paper") || strings.Contains(content, "daily papers") {
			t.Fatalf("unexpected old incomplete task context leaked into new task request: %#v", firstMessages)
		}
		if strings.Contains(content, "最近执行证据") {
			t.Fatalf("unexpected old trace evidence leaked into fresh task request: %#v", firstMessages)
		}
		if strings.Contains(content, "这里是旧任务恢复提示") {
			t.Fatalf("unexpected unfinished slot resume prompt leaked into fresh task request: %#v", firstMessages)
		}
	}
}

func TestHandleIMMessage_DismissRecoverableSessionSuppressesResumeContext(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := NewApp()
	app.testHomeDir = tempHome
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Name: "project", Path: "D:/work/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	manager := NewRemoteSessionManager(app)
	session := &RemoteSession{
		ID:          "sess-dismiss",
		ProjectPath: "D:/work/project",
		Tool:        "claude",
		Status:      SessionExited,
		ResumeContext: &SessionResumeContext{
			ProjectPath:     "D:/work/project",
			Tool:            "claude",
			ResumeSessionID: "resume-123",
		},
	}
	manager.sessions[session.ID] = session
	h := NewIMMessageHandler(app, manager)
	defer h.memory.Stop()

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "忽略这个恢复会话", DismissRecoverableSessionID: "sess-dismiss"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	if session.ResumeContext != nil {
		t.Fatalf("ResumeContext = %#v, want nil", session.ResumeContext)
	}
}

func TestFinalizeTraceResult_PersistsRecoveredTrialReflectSummary(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "finalize recovered summary", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-finalize-recovered", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeFailed))
	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeSucceeded))
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

	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeSucceeded))

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
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Name: "project", Path: "/project"}}
	cfg.CurrentProject = "p1"
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.memory.UpsertUnfinishedSlot("desktop-user", &agent.UnfinishedTaskSlot{
		SlotID:      "slot-trace-recall",
		UserID:      "desktop-user",
		Status:      "pending_resume",
		LastTask:    "排查 npm test 为什么先失败后恢复",
		ProjectPath: "/project",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})
	h.memory.BindUnfinishedSlot("desktop-user", "slot-trace-recall")
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trace recall", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-trace-recall", 3, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeFailed))
	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeSucceeded))
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "adaptive_retry", Category: "args", Summary: "retry decision", ContentSnippet: "invalid parameter"})
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{SourceKind: "trial_reflect", Category: "repeat_guard", Summary: "avoid repeating failed actions", ContentSnippet: "bash"})

	h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	prompt := h.buildTraceEvidencePrompt("desktop-user", "why did npm test fail before it recovered?")
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
	cfg.Projects = []corelib.ProjectConfig{{Id: "p1", Name: "project", Path: "/project"}}
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

	h.traceService.AppendEvent(run.RunID, traceTrialObservedEvent("bash", toolOutcomeSucceeded))

	h.finalizeTraceResult(ctx, &IMAgentResponse{Text: "done"}, "done", "")
	prompt := h.buildTraceEvidencePrompt("desktop-user", "npm test success")
	if strings.Contains(prompt, "trial-reflect summary") {
		t.Fatalf("prompt = %q, did not expect benign trial-reflect summary evidence", prompt)
	}
	if !strings.Contains(prompt, "Trial outcome") && prompt != "" {
		t.Fatalf("prompt = %q, want only normal evidence lines or empty", prompt)
	}
}

func TestBuildTraceEvidencePrompt_SkipsFreshTaskWithoutActiveSlot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trace recall", "desktop", "u1", "/project")
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "recovered after failure",
		ProjectPath:    "/project",
	})

	prompt := h.buildTraceEvidencePrompt("desktop-user", "现在帮我整理新的知识库文档")
	if prompt != "" {
		t.Fatalf("prompt = %q, want empty for fresh task without active slot", prompt)
	}
}

func TestBuildTraceEvidencePrompt_UsesActiveSlot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	app := &App{testHomeDir: tempHome}
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.memory.UpsertUnfinishedSlot("desktop-user", &agent.UnfinishedTaskSlot{
		SlotID:      "slot-trace",
		UserID:      "desktop-user",
		Status:      "pending_resume",
		LastTask:    "继续 Daily Paper",
		ProjectPath: "/project",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	})
	h.memory.BindUnfinishedSlot("desktop-user", "slot-trace")
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "trace recall", "desktop", "u1", "/project")
	h.traceService.AppendEvidence(run.RunID, EvidenceRecord{
		SourceKind:     "trial_reflect_summary",
		Category:       "decision",
		Summary:        "trial-reflect summary",
		ContentSnippet: "recovered after failure",
		ProjectPath:    "/project",
	})

	prompt := h.buildTraceEvidencePrompt("desktop-user", "继续 Daily Paper")
	if !strings.Contains(prompt, "最近执行证据") {
		t.Fatalf("prompt = %q, want evidence header when active slot exists", prompt)
	}
}

func TestHandleIMMessage_ShortChitChatReturnsDirectReply(t *testing.T) {
	h := NewIMMessageHandler(&App{}, &RemoteSessionManager{app: &App{}, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "没事", Lang: "zh"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "好，没问题。我在这，有需要随时叫我。" {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
}

func TestHandleIMMessage_EnglishShortChitChatReturnsDirectReply(t *testing.T) {
	h := NewIMMessageHandler(&App{}, &RemoteSessionManager{app: &App{}, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "nothing", Lang: "en"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "No problem. I'm here if you need anything." {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
}

func TestHandleIMMessageWithProgressAndStream_ShortChitChatWithPunctuationReturnsDirectReply(t *testing.T) {
	h := NewIMMessageHandler(&App{}, &RemoteSessionManager{app: &App{}, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "没事。", Lang: "zh"}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "好，没问题。我在这，有需要随时叫我。" {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
	if resp.TraceSummary != "" {
		t.Fatalf("TraceSummary = %q, want empty", resp.TraceSummary)
	}
}

func TestHandleIMMessageWithProgressAndStream_EnglishShortChitChatWithPunctuationReturnsDirectReply(t *testing.T) {
	h := NewIMMessageHandler(&App{}, &RemoteSessionManager{app: &App{}, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "nothing...", Lang: "en"}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "No problem. I'm here if you need anything." {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
}

func TestHandleIMMessageWithProgressAndStream_OkShortChitChatWithPunctuationReturnsDirectReply(t *testing.T) {
	h := NewIMMessageHandler(&App{}, &RemoteSessionManager{app: &App{}, sessions: map[string]*RemoteSession{}})
	resp := h.HandleIMMessageWithProgressAndStream(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "okay.", Lang: "en"}, nil, nil, nil, nil)
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Text != "Okay. I'm here if you need anything." {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
}

func TestBuildEmptyResultFallback_SuppressesConversationalEchoSummaryWithPunctuation(t *testing.T) {
	got := buildEmptyResultFallback(TraceRunStatusCompleted, "结果：没事。")
	if got != "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。" {
		t.Fatalf("buildEmptyResultFallback() = %q", got)
	}
}

func TestBuildEmptyResultFallback_SuppressesPromptLikeSummary(t *testing.T) {
	got := buildEmptyResultFallback(TraceRunStatusCompleted, "请帮我生成一个 PDF，并保存在当前工作目录")
	if got != "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。" {
		t.Fatalf("buildEmptyResultFallback() = %q", got)
	}
}

func TestBuildEmptyResultFallback_SuppressesConversationalEchoSummary(t *testing.T) {
	got := buildEmptyResultFallback(TraceRunStatusCompleted, "没事")
	if got != "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。" {
		t.Fatalf("buildEmptyResultFallback() = %q", got)
	}
}

func TestBuildEmptyResultFallback_SuppressesEnglishConversationalEchoSummary(t *testing.T) {
	got := buildEmptyResultFallback(TraceRunStatusCompleted, "nothing")
	if got != "任务已结束，但没有生成可展示的结果。可查看 Trace 了解详情。" {
		t.Fatalf("buildEmptyResultFallback() = %q", got)
	}
}

func TestBuildEmptyResultFallback_PreservesExecutionSummary(t *testing.T) {
	got := buildEmptyResultFallback(TraceRunStatusCompleted, "文件 review.pdf 已准备好，但未返回正文摘要")
	if got != "任务已结束，但没有生成可展示的结果。文件 review.pdf 已准备好，但未返回正文摘要" {
		t.Fatalf("buildEmptyResultFallback() = %q", got)
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
	if resp.TraceStatus != string(TraceRunStatusFailed) {
		t.Fatalf("TraceStatus = %q, want %q", resp.TraceStatus, string(TraceRunStatusFailed))
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

func TestFinalizeTraceResult_UsesConfirmedResumeFallbackForEmptyCompletedRun(t *testing.T) {
	h := &IMMessageHandler{traceService: NewAITraceService()}
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "confirmed empty run", "desktop", "u1", "/project")
	ctx := NewLoopContext("chat-confirmed-empty", 2, nil)
	ctx.RunID = run.RunID
	ctx.JobID = run.JobID
	ctx.SetState("completed")

	resp := h.finalizeTraceResult(ctx, &IMAgentResponse{ConfirmedResume: true}, "", "")
	if resp.TraceStatus != string(TraceRunStatusCompleted) {
		t.Fatalf("TraceStatus = %q, want %q", resp.TraceStatus, string(TraceRunStatusCompleted))
	}
	if resp.Text != "已确认并开始执行任务。当前暂无可展示结果，可查看 Trace 了解进展。" {
		t.Fatalf("resp.Text = %q", resp.Text)
	}
	if len(resp.Actions) != 1 || resp.Actions[0].Command != "__view_trace__ "+run.RunID {
		t.Fatalf("Actions = %#v, want trace action", resp.Actions)
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

func TestLooksLikePromiseOnlyDeliverableReply(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "promise only chinese", text: "好的，我马上整理一份报告并发给你。", want: true},
		{name: "completed summary chinese", text: "已完成，以下是总结：本周趋势聚焦多模态与小模型。", want: false},
		{name: "completed summary english", text: "Here is the summary of the report.", want: false},
		{name: "results below english", text: "Completed. Results below: multimodal agents and compact models.", want: false},
		{name: "organized for user chinese", text: "我已经为你整理好了，结果如下：重点是 Agent 与多模态。", want: false},
		{name: "summary first then continue generate", text: "我会先给你总结，再继续生成 PDF 文件。", want: true},
		{name: "completed but still promises send", text: "报告已生成，马上发你。", want: true},
		{name: "english future send promise", text: "I will prepare the document and send you the PDF shortly.", want: true},
		{name: "continuation fragment chinese", text: "继续添加第7-8节和参考文献：", want: true},
		{name: "explicit failure chinese", text: "无法继续添加第7-8节，原因是源文件缺失。", want: false},
		{name: "self intro chinese", text: "我是安妮，平时我会帮你查资料、写文档、做整理、跑工具、处理文件和各种电脑任务。", want: false},
		{name: "self intro english", text: "I'm Annie, your AI assistant. I can help you write documents, organize files, and run tools.", want: false},
		{name: "capability listing chinese", text: "我可以帮你整理文档、生成报告、处理文件等任务。", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikePromiseOnlyDeliverableReply(tc.text); got != tc.want {
				t.Fatalf("looksLikePromiseOnlyDeliverableReply(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestRunAgentLoop_CompletedSummaryReplyDoesNotTriggerDeliverableRecover(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1, 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已完成，以下是总结：2025 年 AI 趋势集中在多模态、Agent 与小型化部署。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()

	resp := h.HandleIMMessage(IMUserMessage{UserID: "desktop-user", Platform: "desktop", Text: "生成 pdf 发我"})
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "已完成，以下是总结") {
		t.Fatalf("resp.Text = %q, want completed summary text", resp.Text)
	}
	if callNum < 1 {
		t.Fatalf("expected at least one LLM call, callNum=%d", callNum)
	}
	view, ok := h.traceService.GetTrace(resp.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	for _, evt := range view.Events {
		if evt.Kind == "delivery.nudged" {
			t.Fatalf("events = %#v, did not expect delivery.nudged", view.Events)
		}
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "deliverable_pending") {
			t.Fatalf("events = %#v, did not expect deliverable_pending recover", view.Events)
		}
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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
	foundRecover := false
	var sawSendFile bool
	for _, evt := range view.Events {
		if evt.Kind == "delivery.nudged" {
			foundNudge = true
		}
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "deliverable_pending") {
			foundRecover = true
		}
		if evt.Kind == "tool.executed" && evt.Title == "send_file" {
			sawSendFile = true
		}
	}
	if !foundNudge && !sawSendFile {
		t.Fatalf("events = %#v, want delivery.nudged event or direct send_file execution", view.Events)
	}
	if !foundRecover && !sawSendFile {
		t.Fatalf("events = %#v, want deliverable recover event or direct send_file execution", view.Events)
	}
}

func TestRunAgentLoop_ListSkillsEmptyStateDoesNotEnterTrialFailedRecover(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-list-skills\",\"type\":\"function\",\"function\":{\"name\":\"list_skills\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我看过本地技能了，目前还没有已安装项，我继续换其他路径完成。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "list_skills",
		Description: "list skills",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "本地没有已注册的 Skill。\n\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n"
		},
	}); err != nil {
		t.Fatalf("Register list_skills tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-list-skills-empty", 4, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "list skills empty state", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "先看下有没有本地 skill", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "继续换其他路径完成") {
		t.Fatalf("resp.Text = %q, want final assistant text", resp.Text)
	}

	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "trial_failed") {
			t.Fatalf("events = %#v, did not expect trial_failed recover", view.Events)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("LLM request count = %d, want 2", len(requests))
	}
}

func TestRunAgentLoop_EmptyAssistantReplyTriggersRecoverPhase(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已恢复，以下是可展示结果。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	h.traceService = NewAITraceService()
	loopCtx := NewLoopContext("chat-empty-final", 4, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "empty final recover", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "帮我整理结果并完成交付", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "已恢复，以下是可展示结果。" {
		t.Fatalf("resp.Text = %q, want recovered text", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 2 {
		t.Fatalf("LLM request count = %d, want at least 2", len(requests))
	}
	secondMessages := requests[1].Messages
	foundRecoverPrompt := false
	for _, msg := range secondMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "[Recover 阶段]") && strings.Contains(content, "没有返回任何可展示结果") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("second request messages = %#v, want empty-result recover guidance", secondMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecoverEvent := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "empty_final_response") {
			foundRecoverEvent = true
			break
		}
	}
	if !foundRecoverEvent {
		t.Fatalf("events = %#v, want empty_final_response recover trace event", view.Events)
	}
}

func TestRunAgentLoop_RepeatedPromiseOnlyRepliesEscalateToNoToolStallRecover(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好的，我马上继续生成综述 PDF 并发给你。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我已经停止空转，改用真实工具继续。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = 5
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	loopCtx := NewLoopContext("chat-promise-stall", 5, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "promise recover escalation", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成 pdf 发我", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "改用真实工具继续") {
		t.Fatalf("resp.Text = %q, want escalated response", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("LLM request count = %d, want 3", len(requests))
	}
	thirdMessages := requests[2].Messages
	foundRecoverPrompt := false
	for _, msg := range thirdMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "No real tool was called for 2 consecutive rounds") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("third request messages = %#v, want no_tool_stall recover guidance", thirdMessages)
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundRecover := false
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "no_tool_stall") {
			foundRecover = true
			break
		}
	}
	if !foundRecover {
		t.Fatalf("events = %#v, want no_tool_stall recover event", view.Events)
	}
}

func TestRunAgentLoop_EmptyAssistantAfterSkillFailureEscalatesToNoToolStallRecover(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先用现有 Skill 试一下。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-run-skill\",\"type\":\"function\",\"function\":{\"name\":\"run_skill\",\"arguments\":\"{\\\"name\\\":\\\"hf_daily_papers_report\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"继续添加第7-8节和参考文献：\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 5:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我已切换到其他真实工具继续处理。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.NLSkills = []corelib.NLSkillEntry{{
		Name:        "hf_daily_papers_report",
		Description: "把 HuggingFace Daily Papers 整理成报告并生成文件",
		Triggers:    []string{"daily papers", "综述", "报告", "pdf"},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo skill"}}},
		Status:      "active",
		CreatedAt:   time.Now().Format(time.RFC3339),
		Source:      "manual",
	}}
	cfg.MaclawAgentMaxIterations = 5
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	if err := h.registry.Register(RegisteredTool{
		Name:        "run_skill",
		Description: "run local skill",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			return "Skill 执行失败: skill execution stopped at step 1: command failed（run_id=run-2024）"
		},
	}); err != nil {
		t.Fatalf("Register run_skill tool: %v", err)
	}

	loopCtx := NewLoopContext("chat-skill-empty", 5, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "skill failure empty follow-up", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成huggingface daily papers的综述，包括方法原理、关键创新、评论等。", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "切换到其他真实工具继续处理") {
		t.Fatalf("resp.Text = %q, want escalated response", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 5 {
		t.Fatalf("LLM request count = %d, want 5", len(requests))
	}
	fifthMessages := requests[4].Messages
	foundRecoverPrompt := false
	for _, msg := range fifthMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "No real tool was called for") {
			foundRecoverPrompt = true
			break
		}
	}
	if !foundRecoverPrompt {
		t.Fatalf("fifth request messages = %#v, want no_tool_stall guidance after skill failure", fifthMessages)
	}
}

func TestRunAgentLoop_ReasoningFallbackDoesNotTriggerEmptyRecover(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"这是 reasoning fallback 的结果\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = 3
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	loopCtx := NewLoopContext("chat-reasoning-fallback", 3, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "reasoning fallback", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "给我总结结果", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "这是 reasoning fallback 的结果" {
		t.Fatalf("resp.Text = %q, want reasoning fallback text", resp.Text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 1 {
		t.Fatalf("LLM request count = %d, want at least 1", len(requests))
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	for _, evt := range view.Events {
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "empty_final_response") {
			t.Fatalf("events = %#v, did not expect empty_final_response recover", view.Events)
		}
	}
}

func TestRunAgentLoop_RepeatedEmptyAssistantRepliesReenterRecover(t *testing.T) {
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
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":1,\"total_tokens\":9}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已恢复，补充了最终结果。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = config.MinAgentIterations
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	loopCtx := NewLoopContext("chat-empty-fallback", config.MinAgentIterations, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "empty final repeated recover", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "帮我整理结果并完成交付", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "已恢复，补充了最终结果。" {
		t.Fatalf("resp.Text = %q, want recovered text", resp.Text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("LLM request count = %d, want 3", len(requests))
	}
	for _, idx := range []int{1, 2} {
		foundRecoverPrompt := false
		for _, msg := range requests[idx].Messages {
			role, _ := msg["role"].(string)
			content, _ := msg["content"].(string)
			if role == "system" && strings.Contains(content, "没有返回任何可展示结果") {
				foundRecoverPrompt = true
				break
			}
		}
		if !foundRecoverPrompt {
			t.Fatalf("request[%d] messages = %#v, want empty-result recover guidance", idx, requests[idx].Messages)
		}
	}
}

func TestRunAgentLoop_RemoteSkillSearchPromptAppearsWhenNoLocalSkillMatches(t *testing.T) {
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
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"我先检查一下可复用能力。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-search-install\",\"type\":\"function\",\"function\":{\"name\":\"search_and_install_skill\",\"arguments\":\"{\\\"query\\\":\\\"daily papers pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"已搜索并安装相关 Skill，接下来继续执行。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = 4
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	h.SetToolRegistry(NewToolRegistry())
	h.SetSkillSearchInstallHandler(func(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
		return searchAndInstallSkillResult{Text: "Installed Skill: hf_daily_papers_report", Success: true}
	})

	loopCtx := NewLoopContext("chat-remote-skill-search", 4, server.Client())
	_, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "remote skill search", "desktop", "u1", "/project")
	loopCtx.RunID = run.RunID
	loopCtx.JobID = run.JobID
	resp := h.runAgentLoop(loopCtx, "u1", "system", nil, "生成 huggingface daily papers 的 pdf 综述", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if !strings.Contains(resp.Text, "已搜索并安装相关 Skill") {
		t.Fatalf("resp.Text = %q, want remote search completion text", resp.Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) < 2 {
		t.Fatalf("LLM request count = %d, want at least 2", len(requests))
	}
	secondMessages := requests[1].Messages
	foundRemotePrompt := false
	for _, msg := range secondMessages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" && strings.Contains(content, "Search/install a reusable Skill first") && strings.Contains(content, "Only switch to craft_tool or bash") {
			foundRemotePrompt = true
			break
		}
	}
	if !foundRemotePrompt {
		t.Fatalf("second request messages = %#v, want remote skill search guidance", secondMessages)
	}
}

func TestRunAgentLoop_PromiseOnlyPDFCraftTimeoutFallsBackToBashAndDeliversFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好的，我马上继续生成综述 PDF 并发给你。\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-craft\",\"type\":\"function\",\"function\":{\"name\":\"craft_tool\",\"arguments\":\"{\\\"task\\\":\\\"生成 PDF 综述文章并发给我\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-bash\",\"type\":\"function\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"python render_pdf.py --input review.md --output review.pdf\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawDebugToolCalls = false
	cfg.MaclawAgentMaxIterations = 6
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	if err := h.registry.Register(RegisteredTool{
		Name:        "craft_tool",
		Description: "test craft tool",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			if onProgress != nil {
				onProgress("internal craft progress")
			}
			return "📝 脚本语言: python\n📁 脚本路径: /tmp/review.py\n\n[error] timeout after 180s\n\n⚠️ 执行出错: timeout after 180s\n脚本已保存，你可以手动修改后重新执行。"
		},
	}); err != nil {
		t.Fatalf("Register craft_tool: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "bash",
		Description: "test bash",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			if onProgress != nil {
				onProgress("internal bash progress")
			}
			return "review.pdf generated"
		},
	}); err != nil {
		t.Fatalf("Register bash: %v", err)
	}
	if err := h.registry.Register(RegisteredTool{
		Name:        "send_file",
		Description: "test file sender",
		Category:    ToolCategoryBuiltin,
		Status:      RegToolAvailable,
		Source:      "test",
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
			if onProgress != nil {
				onProgress("internal send_file progress")
			}
			return "[file_base64|review.pdf|application/pdf]JVBERi0xLjQKfake"
		},
	}); err != nil {
		t.Fatalf("Register send_file: %v", err)
	}

	job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "生成 pdf 发我", "desktop", "desktop-user", "/project")
	loopCtx := NewLoopContext("chat-pdf-fallback", 6, server.Client())
	loopCtx.JobID = job.JobID
	loopCtx.RunID = run.RunID

	var progress []string
	resp := h.runAgentLoop(loopCtx, "desktop-user", "system", nil, "生成 pdf 发我", nil, func(msg string) {
		progress = append(progress, msg)
	}, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.LocalFilePath == "" {
		t.Fatalf("LocalFilePath empty, resp=%+v", resp)
	}
	if callNum != 4 {
		t.Fatalf("LLM call count = %d, want 4", callNum)
	}
	wantProgress := []string{
		"🛠️ 正在生成并执行脚本，准备继续完成交付...",
		"🖥️ 正在执行命令处理文件，请稍候...",
		"📤 正在整理并发送生成的文件...",
	}
	for _, want := range wantProgress {
		found := false
		for _, msg := range progress {
			if strings.Contains(msg, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("progress = %#v, want %q", progress, want)
		}
	}
	for _, msg := range progress {
		if strings.Contains(msg, "internal craft progress") || strings.Contains(msg, "internal bash progress") || strings.Contains(msg, "internal send_file progress") {
			t.Fatalf("unexpected verbose internal tool progress in user-facing mode: %q", msg)
		}
		if strings.Contains(msg, "脚本路径:") || strings.Contains(msg, "[stderr]") {
			t.Fatalf("unexpected raw tool output leaked to user-facing progress: %q", msg)
		}
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	foundNudge := false
	foundRecover := false
	var sawSendFile bool
	for _, evt := range view.Events {
		if evt.Kind == "delivery.nudged" {
			foundNudge = true
		}
		if evt.Kind == "loop.recover_entered" && strings.Contains(evt.Summary, "deliverable_pending") {
			foundRecover = true
		}
		if evt.Kind == "tool.executed" && evt.Title == "send_file" {
			sawSendFile = true
		}
	}
	if !foundNudge && !sawSendFile {
		t.Fatalf("events = %#v, want delivery.nudged event or direct send_file execution", view.Events)
	}
	if !foundRecover && !sawSendFile {
		t.Fatalf("events = %#v, want deliverable recover event or direct send_file execution", view.Events)
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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
		HandlerProg: func(args map[string]interface{}, onProgress tool.ProgressCallback) string {
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

func TestRunAgentLoop_MarkdownWorkflowUsesWriteThenEditInTrace(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("USERPROFILE", tempHome)
	targetPath := tempHome + "/review.md"

	var callNum int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callNum++
		w.Header().Set("Content-Type", "text/event-stream")
		switch callNum {
		case 1:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-write\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"~/review.md\\\",\\\"content\\\":\\\"# Daily Review\\\\nFirst draft\\\"}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 2:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-edit\",\"type\":\"function\",\"function\":{\"name\":\"edit_file\",\"arguments\":\"{\\\"path\\\":\\\"~/review.md\\\",\\\"old_string\\\":\\\"First draft\\\",\\\"new_string\\\":\\\"Final draft\\\",\\\"replace_all\\\":false}\"}}]},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		case 3, 4:
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"markdown ready\"},\"finish_reason\":null}],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":4,\"total_tokens\":12}}\n\n"))
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
	cfg.MaclawAgentMaxIterations = 4
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
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.traceService = NewAITraceService()
	job, run := h.traceService.StartJobRun(TraceJobKindAIAssistant, "整理 markdown 文档", "desktop", "desktop-user", tempHome)
	loopCtx := NewLoopContext("chat-markdown-trace", 4, server.Client())
	loopCtx.JobID = job.JobID
	loopCtx.RunID = run.RunID

	resp := h.runAgentLoop(loopCtx, "desktop-user", "system", nil, "把内容整理成 markdown 文件，并按意见继续修改", nil, nil, nil, nil, nil, 0, "desktop")
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != "" {
		t.Fatalf("resp.Error = %q", resp.Error)
	}
	if resp.Text != "markdown ready" {
		t.Fatalf("resp.Text = %q, want markdown ready", resp.Text)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", targetPath, err)
	}
	if string(data) != "# Daily Review\nFinal draft" {
		t.Fatalf("file content = %q, want %q", string(data), "# Daily Review\nFinal draft")
	}
	view, ok := h.traceService.GetTrace(run.RunID)
	if !ok {
		t.Fatal("expected trace view")
	}
	var sawWrite, sawEdit, sawCraft bool
	for _, evt := range view.Events {
		if evt.Kind != "tool.executed" {
			continue
		}
		switch evt.Title {
		case "write_file":
			sawWrite = true
		case "edit_file":
			sawEdit = true
		case "craft_tool":
			sawCraft = true
		}
	}
	if !sawWrite || !sawEdit {
		t.Fatalf("events = %#v, want write_file and edit_file tool.executed events", view.Events)
	}
	if callNum < 3 {
		t.Fatalf("expected at least 3 LLM calls, got %d", callNum)
	}
	if sawCraft {
		t.Fatalf("events = %#v, did not expect craft_tool for markdown workflow", view.Events)
	}
}
