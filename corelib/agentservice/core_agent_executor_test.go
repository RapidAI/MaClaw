package agentservice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type captureExecutor struct {
	req ExecuteRequest
}

func (e *captureExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	_ = ctx
	e.req = req
	return &ExecuteResult{Content: "ok", OutputType: "text/plain"}, nil
}

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
	req     ExecuteRequest
}

func (e *blockingExecutor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	e.req = req
	close(e.started)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-e.release:
		return &ExecuteResult{Content: "async done", OutputType: "text/plain"}, nil
	}
}

type fakeMCPToolProvider struct {
	entries  []MCPToolEntry
	calls    []string
	lastArgs map[string]interface{}
}

func (p *fakeMCPToolProvider) ListAvailableTools(context.Context, Principal) []MCPToolEntry {
	return p.entries
}

func (p *fakeMCPToolProvider) CallTool(ctx context.Context, principal Principal, serverID, toolName string, arguments map[string]interface{}) (string, error) {
	_ = ctx
	_ = principal
	p.calls = append(p.calls, serverID+"/"+toolName)
	p.lastArgs = arguments
	switch toolName {
	case "prepare_skill_input_data":
		return `{"samples":[{"id":"sample:sample_1#0","question":"prepared expert sample question","category":"expert_sample"}],"composed_attacks":[{"id":"composed_attack:attack_1#0","question":"prepared composed attack text","category":"composed_attack"}],"count":2}`, nil
	case "register_skill_payload_dataset":
		return `{"payload_handles":["redteam_payload_1"],"payload_count":1}`, nil
	case "execute_redteam_evaluation_batch":
		return `{"report_id":"redteam_report_1","status":"succeeded"}`, nil
	}
	return `{"items":[{"source_type":"skill","source_ref":"ccbos-classical-chinese-skill","name":"CCBOS classical Chinese jailbreak","summary":"Classical-Chinese jailbreak payload generator."}]}`, nil
}

func TestPostMessagePropagatesCapabilityContextToExecutor(t *testing.T) {
	executor := &captureExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://example.invalid/v1", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session", Metadata: map[string]string{"agent_profile": "redteam_evaluation_v1"}})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	capCtx := &RuntimeCapabilityContext{
		AgentProfile: "redteam_evaluation_v1",
		Cards: []CapabilityCard{{
			SourceType: "skill",
			SourceRef:  "ccbos-classical-chinese-skill",
			Name:       "CCBOS classical Chinese jailbreak",
			RiskTypes:  []string{"jailbreak"},
			Languages:  []string{"classical_chinese"},
		}},
	}
	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "use classical Chinese jailbreak", CapabilityContext: capCtx}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if executor.req.CapabilityContext == nil || len(executor.req.CapabilityContext.Cards) != 1 {
		t.Fatalf("capability context was not propagated: %#v", executor.req.CapabilityContext)
	}
	if got := executor.req.CapabilityContext.Cards[0].SourceRef; got != "ccbos-classical-chinese-skill" {
		t.Fatalf("capability ref = %q", got)
	}
}

func TestPostMessageAsyncReturnsRunningRunAndCompletesInBackground(t *testing.T) {
	executor := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	svc, principal, inst, sess := newTestServiceInstanceSession(t, executor, "http://llm.example.test/v1", nil)

	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "confirm"})
	if err != nil {
		t.Fatalf("PostMessageAsync: %v", err)
	}
	if run == nil || run.Status != RunStatusRunning || run.SessionID != sess.ID {
		t.Fatalf("async run = %#v", run)
	}
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start in background")
	}

	close(executor.release)
	var final *Run
	for i := 0; i < 30; i++ {
		got, err := svc.GetRun(context.Background(), principal, inst.ID, run.ID)
		if err != nil {
			t.Fatalf("GetRun: %v", err)
		}
		if got.Status == RunStatusSucceeded {
			final = got
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final == nil || final.AssistantMessageID == "" {
		t.Fatalf("async run did not complete with assistant message: %#v", final)
	}
	messages, err := svc.ListMessages(context.Background(), principal, inst.ID, sess.ID, ListMessagesInput{})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 || messages[1].Role != MessageRoleAssistant || messages[1].Content != "async done" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestPostMessageAsyncInjectsRunIDIntoExecutorMetadata(t *testing.T) {
	executor := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	svc, principal, inst, sess := newTestServiceInstanceSession(t, executor, "http://llm.example.test/v1", nil)

	run, err := svc.PostMessageAsync(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content: "confirm",
		Metadata: map[string]string{
			"evaluation_action": "confirm_plan",
		},
	})
	if err != nil {
		t.Fatalf("PostMessageAsync: %v", err)
	}
	select {
	case <-executor.started:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not start in background")
	}
	close(executor.release)
	if got := executor.req.Message.Metadata["run_id"]; got != run.ID {
		t.Fatalf("executor run_id metadata = %q, want %q; metadata=%#v", got, run.ID, executor.req.Message.Metadata)
	}
}

func TestRedteamProfileCanUseMCPToolAndReturnFinalAnswer(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req struct {
			Tools    []map[string]interface{} `json:"tools"`
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode llm request: %v", err)
		}
		if callCount == 1 {
			if !requestHasTool(req.Tools, "search_redteam_capabilities") {
				t.Fatalf("first LLM request missing MCP search tool: %#v", req.Tools)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_search",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "search_redteam_capabilities",
								"arguments": `{"query":"classical Chinese jailbreak","limit":5}`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})
			return
		}
		if !requestHasToolResult(req.Messages, "call_search", "ccbos-classical-chinese-skill") {
			t.Fatalf("second LLM request missing MCP tool result: %#v", req.Messages)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message":       map[string]interface{}{"role": "assistant", "content": "I found CCBOS and can draft a red-team plan."},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	provider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "search_redteam_capabilities",
		Description: "Search safe red-team capability cards.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
				"limit": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"query"},
		},
	}}}
	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetMCPToolProvider(provider)
	svc, principal, inst, sess := newTestServiceInstanceSession(t, executor, server.URL, map[string]string{"agent_profile": "redteam_evaluation_v1"})

	_, msg, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content:  "Please inspect available red-team capabilities for classical Chinese jailbreak testing.",
		Metadata: map[string]string{"agent_profile": "redteam_evaluation_v1"},
	})
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if msg == nil || !strings.Contains(msg.Content, "CCBOS") {
		t.Fatalf("unexpected final message: %#v", msg)
	}
	if len(provider.calls) != 1 || provider.calls[0] != "mcp_redteam/search_redteam_capabilities" {
		t.Fatalf("MCP calls = %#v", provider.calls)
	}
}

func TestRedteamProfileGreetingUsesFastReply(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	result, err := executor.Execute(context.Background(), redteamFastReplyRequest(t, server.URL, "你好"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("fast reply should not call LLM, got %d calls", llmCalls)
	}
	if result.Metadata["response_source"] != "chat" || result.Metadata["redteam_fast_path"] != "true" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if !strings.Contains(result.Content, "大模型安全评估") || strings.Contains(result.Content, "红队测试") || strings.Contains(result.Content, "智能体") {
		t.Fatalf("unexpected greeting content: %q", result.Content)
	}
}

func TestRedteamProfileCapabilityQuestionUsesFastReply(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	result, err := executor.Execute(context.Background(), redteamFastReplyRequest(t, server.URL, "你能干什么？"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("fast reply should not call LLM, got %d calls", llmCalls)
	}
	if !strings.Contains(result.Content, "专家样本") || !strings.Contains(result.Content, "固定中文 PDF 报告") {
		t.Fatalf("unexpected capability content: %q", result.Content)
	}
}

func TestRedteamProfileSkillQuestionUsesFastReplyWithInstalledSkillSummary(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetSkillToolProvider(fakeSkillToolProvider{})
	result, err := executor.Execute(context.Background(), redteamFastReplyRequest(t, server.URL, "你有什么skill？"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("skill fast reply should not call LLM, got %d calls", llmCalls)
	}
	if result.Metadata["redteam_fast_path"] != "true" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if !strings.Contains(result.Content, "ccbos-classical-chinese-skill") || !strings.Contains(result.Content, "Classical Chinese jailbreak probes") {
		t.Fatalf("skill summary should use installed skill provider data, got %q", result.Content)
	}
}

func TestRedteamProfileSpecificSkillQuestionUsesDetailReply(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetSkillToolProvider(fakeSkillToolProvider{})
	result, err := executor.Execute(context.Background(), redteamFastReplyRequest(t, server.URL, "CCBOS 文言文越狱 Skill 有什么功能？"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("specific skill detail should use installed metadata without LLM, got %d calls", llmCalls)
	}
	if result.Metadata["redteam_fast_path"] != "true" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if strings.Contains(result.Content, "当前租户已安装以下 Skill") {
		t.Fatalf("specific skill detail should not use generic inventory reply: %q", result.Content)
	}
	if !strings.Contains(result.Content, "ccbos-classical-chinese-skill") ||
		!strings.Contains(result.Content, "Classical Chinese jailbreak probes") ||
		!strings.Contains(result.Content, "确认执行") {
		t.Fatalf("specific skill detail should explain the matched skill and execution boundary, got %q", result.Content)
	}
}

func TestRedteamProfileExplicitInstalledSkillRequestUsesFastPlanWithoutLLMLoop(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetSkillToolProvider(fakeSkillToolProvider{})
	req := redteamFastReplyRequest(t, server.URL, "请对当前被测模型进行文言文越狱测试，优先使用已安装的 CCBOS 文言文改写 Skill，测试 3 条。")
	req.Message.Metadata = map[string]string{
		"agent_profile":                "redteam_evaluation_v1",
		"current_target_configured":    "true",
		"current_target_name":          "默认被测模型",
		"current_target_provider":      "openai",
		"current_target_model":         "deepseek-chat",
		"current_target_health_status": "unknown",
	}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("explicit installed Skill request should not enter LLM loop, got %d calls", llmCalls)
	}
	if result.OutputType != outputTypePlanConfirm || result.Metadata[metaResponseSource] != string(responseSourcePlanConfirm) {
		t.Fatalf("result metadata/output = %#v output=%s", result.Metadata, result.OutputType)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &body); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, result.Content)
	}
	if body["response_source"] != string(responseSourcePlanConfirm) || body["requires_confirmation"] != true {
		t.Fatalf("plan body = %#v", body)
	}
	if intFromInterface(body["test_count"]) != 3 {
		t.Fatalf("test_count = %#v, want 3", body["test_count"])
	}
	if !strings.Contains(fmt.Sprint(body["selected_skills"]), "ccbos-classical-chinese-skill") ||
		!strings.Contains(fmt.Sprint(body["selected_capability_refs"]), "skillhub:ccbos-classical-chinese-skill") {
		t.Fatalf("plan should select installed CCBOS Skill: %#v", body)
	}
}

func TestRedteamProfileNormalizesPlanConfirmJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role": "assistant",
					"content": "Here is the plan:\n```json\n" +
						`{"target_summary":"default target","risk_types":["jailbreak"],"selected_capability_refs":["skillhub:ccbos-classical-chinese-skill"],"selected_skills":["ccbos-classical-chinese-skill"],"selection_reasons":"use the installed CCBOS Skill","test_count":5,"selection_strategy":"maclaw_selected","requires_confirmation":true}` +
						"\n```",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	result, err := executor.Execute(context.Background(), redteamFastReplyRequest(t, server.URL, "run ccbos skill"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Metadata[metaResponseSource] != "plan_confirm" || result.Metadata["evaluation_event_type"] != "plan_confirm" {
		t.Fatalf("metadata = %#v, want plan_confirm", result.Metadata)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &body); err != nil {
		t.Fatalf("normalized content is not JSON: %v\n%s", err, result.Content)
	}
	if body["response_source"] != "plan_confirm" || body["requires_confirmation"] != true {
		t.Fatalf("normalized body = %#v", body)
	}
	if strings.Contains(result.Content, "```") || strings.Contains(result.Content, "Here is") {
		t.Fatalf("normalized plan should not include prose/fences: %q", result.Content)
	}
}

func redteamFastReplyRequest(t *testing.T, llmURL, content string) ExecuteRequest {
	t.Helper()
	dataDir := t.TempDir()
	return ExecuteRequest{
		Principal: Principal{TenantID: "tenant_1", UserID: "user_1"},
		Instance: Instance{
			ID:        "inst_1",
			TenantID:  "tenant_1",
			UserID:    "user_1",
			Workspace: dataDir,
		},
		Session: Session{
			ID:       "sess_1",
			AgentID:  "agent_1",
			Metadata: map[string]string{"agent_profile": "redteam_evaluation_v1"},
		},
		Message: Message{
			ID:       "msg_1",
			Content:  content,
			Metadata: map[string]string{"agent_profile": "redteam_evaluation_v1"},
		},
		DataDir: dataDir,
		Config: corelib.AppConfig{
			MaclawLLMUrl:   llmURL,
			MaclawLLMKey:   "test-key",
			MaclawLLMModel: "test-model",
		},
	}
}

func newTestServiceInstanceSession(t *testing.T, executor Executor, llmURL string, sessionMetadata map[string]string) (*Service, Principal, Instance, Session) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: llmURL, MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session", Metadata: sessionMetadata})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return svc, principal, *inst, *sess
}

func requestHasTool(tools []map[string]interface{}, name string) bool {
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		if got, _ := fn["name"].(string); got == name {
			return true
		}
	}
	return false
}

func requestHasToolResult(messages []map[string]interface{}, toolCallID, contains string) bool {
	for _, msg := range messages {
		if role, _ := msg["role"].(string); role != "tool" {
			continue
		}
		if got, _ := msg["tool_call_id"].(string); got != toolCallID {
			continue
		}
		content, _ := msg["content"].(string)
		return strings.Contains(content, contains)
	}
	return false
}

func TestRedteamProfilePromptSteersScopeAndCapabilityCards(t *testing.T) {
	cb := &coreAgentCallbacks{
		appCfg:       corelib.AppConfig{MaclawRoleName: "Tenant configured MaClaw role"},
		agentProfile: "redteam_evaluation_v1",
		capabilityContext: &RuntimeCapabilityContext{Cards: []CapabilityCard{{
			SourceType: "skill",
			SourceRef:  "ccbos-classical-chinese-skill",
			Name:       "CCBOS",
			Summary:    "Classical-Chinese jailbreak payload generator.",
			UseWhen:    "The user asks for classical Chinese jailbreak testing.",
		}}},
	}
	prompt := cb.BuildSystemPrompt("你好，你能做什么", true)
	for _, want := range []string{"Tenant configured MaClaw role", "large-model security evaluation", "Current supported capabilities", "expert samples", "plan_confirm", "ccbos-classical-chinese-skill"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{"large-model/agent", "red-team evaluation agent", "general-purpose assistant"} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("prompt should be scoped to current capabilities and omit %q:\n%s", notWant, prompt)
		}
	}
}

func TestRedteamProfilePromptRequiresWorkbenchIdentityAndAutonomousSearch(t *testing.T) {
	cb := &coreAgentCallbacks{
		ctx:           context.Background(),
		agentProfile:  "redteam_evaluation_v1",
		skillProvider: fakeSkillToolProvider{},
		messageMetadata: map[string]string{
			"current_target_configured": "true",
			"current_target_name":       "Default target",
			"current_target_provider":   "openai-compatible",
			"current_target_model":      "demo-model",
		},
	}
	prompt := cb.BuildSystemPrompt("你好，你能干什么", true)
	for _, want := range []string{
		"large-model security evaluation assistant",
		"greetings",
		"capability questions",
		"search_platform_redteam_capabilities",
		"manage_skill tool with action=\"list\" or action=\"search\"",
		"exact source_ref values",
		"Do not invent expert data names or placeholder refs",
		"Respect explicit user preferences for data forms and sampling strategy",
		"MUST include at least one sample:<uuid> ref and at least one template:<uuid> ref",
		"selection_strategy",
		"single JSON object only",
		"Do not add Markdown or prose",
		"suggest a reasonable test_count",
		"execute_redteam_evaluation_batch",
		"Do not fan out confirmed evaluations into per-payload call_evaluation_target",
		"judge_attack_result",
		"source of truth",
		"compiles the fixed Chinese report",
		"Do not rely on memory to define your role",
		"Current supported capabilities",
		"Installed Skills available to this tenant",
		"ccbos-classical-chinese-skill",
		"Installed Skill summaries are authoritative enough for planning",
		"selected_skills",
		"canonical Skill name",
		"do not answer with only a Skill capability explanation",
		"produce plan_confirm",
		"If no matching Skill is installed or available from the configured Hub",
		"current tested model",
		"当前被测模型",
		"Default target",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{"model/agent", "red-team abilities", "agent-tool-abuse", "general-purpose assistant", "Before producing plan_confirm for a Skill-backed request, call manage_skill"} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("prompt should describe only currently supported capabilities and omit %q:\n%s", notWant, prompt)
		}
	}
}

func TestCoreAgentExecutorSupportsAskUserFlow(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var resp map[string]interface{}
		if callCount == 1 {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]interface{}{{
							"id":   "call_ask_1",
							"type": "function",
							"function": map[string]interface{}{
								"name":      "ask_user",
								"arguments": `{"question":"Choose one","options":["A","B"],"input_type":"choice"}`,
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			}
		} else {
			resp = map[string]interface{}{
				"choices": []map[string]interface{}{{
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Acknowledged your answer.",
					},
					"finish_reason": "stop",
				}},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: server.URL, MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	run, msg, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "Help me choose"})
	if err != nil {
		t.Fatalf("PostMessage ask_user: %v", err)
	}
	if msg == nil || msg.Metadata[metaResponseSource] != "ask_user" {
		t.Fatalf("expected ask_user message metadata, got %#v", msg)
	}
	if run == nil || run.ResponseSource != "ask_user" || !run.WaitingForUser || run.DurationMs <= 0 {
		t.Fatalf("expected enriched ask_user run, got %#v", run)
	}
	sess, err = svc.GetSession(context.Background(), principal, inst.ID, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after ask_user: %v", err)
	}
	if sess.Metadata[sessionMetaPendingAskUser] != "true" {
		t.Fatalf("expected pending ask_user metadata, got %#v", sess.Metadata)
	}
	if !sess.WaitingForUser || sess.PendingAsk == nil || sess.PendingAsk.Question != "Choose one" || len(sess.PendingAsk.Options) != 2 {
		t.Fatalf("expected enriched pending ask state, got %#v", sess)
	}
	if sess.LastMessageAt == nil {
		t.Fatalf("expected last_message_at to be populated")
	}
	if _, msg, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "1"}); err != nil {
		t.Fatalf("PostMessage answer: %v", err)
	} else if msg == nil || msg.Content != "Acknowledged your answer." {
		t.Fatalf("unexpected final message: %#v", msg)
	}
	sess, err = svc.GetSession(context.Background(), principal, inst.ID, sess.ID)
	if err != nil {
		t.Fatalf("GetSession after answer: %v", err)
	}
	if sess.Metadata[sessionMetaPendingAskUser] != "" {
		t.Fatalf("expected pending ask_user metadata to clear, got %#v", sess.Metadata)
	}
}

type noOpKnowledgeStore struct{}

func (noOpKnowledgeStore) Search(context.Context, knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	return nil, nil
}
func (noOpKnowledgeStore) ContextPack(context.Context, knowledge.ContextPackOptions) (knowledge.ContextPackResult, error) {
	return knowledge.ContextPackResult{}, nil
}
func (noOpKnowledgeStore) SaveURL(context.Context, knowledge.URLSaveRequest) (knowledge.Source, error) {
	return knowledge.Source{}, nil
}
func (noOpKnowledgeStore) SaveText(context.Context, knowledge.TextSaveRequest) (knowledge.Source, error) {
	return knowledge.Source{}, nil
}
func (noOpKnowledgeStore) ScanDirectory(context.Context, knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	return knowledge.DirectoryImportResult{}, nil
}
func (noOpKnowledgeStore) ScanFiles(context.Context, knowledge.DirectoryImportRequest, []string) (knowledge.DirectoryImportResult, error) {
	return knowledge.DirectoryImportResult{}, nil
}
func (noOpKnowledgeStore) ImportDirectory(context.Context, knowledge.DirectoryImportRequest) (knowledge.DirectoryImportResult, error) {
	return knowledge.DirectoryImportResult{}, nil
}
func (noOpKnowledgeStore) ImportFiles(context.Context, knowledge.DirectoryImportRequest, []string) (knowledge.DirectoryImportResult, error) {
	return knowledge.DirectoryImportResult{}, nil
}
func (noOpKnowledgeStore) Stats(context.Context) (knowledge.Stats, error) {
	return knowledge.Stats{}, nil
}

func TestCoreAgentBuildSystemPromptIncludesKnowledgeRulesWhenStoreConfigured(t *testing.T) {
	cb := &coreAgentCallbacks{knowledgeStore: noOpKnowledgeStore{}}
	prompt := cb.BuildSystemPrompt("what is in my docs?", true)
	if !strings.Contains(prompt, agent.PromptKnowledgeBaseRules) {
		t.Fatalf("expected knowledge base rules in core agent prompt")
	}
	if !strings.Contains(prompt, "knowledge_import_files") || !strings.Contains(prompt, "knowledge_import_directory") {
		t.Fatalf("expected knowledge import tool guidance in core agent prompt")
	}
}

func TestCoreAgentExecutorMemoryUsesCorelibStoreFactory(t *testing.T) {
	dataDir := t.TempDir()
	legacyPath := filepath.Join(dataDir, "agent_memory.json")
	now := time.Now().UTC()
	legacy := []memory.Entry{{ID: "srv-legacy-1", Content: "legacy server memory", Category: memory.CategoryUserFact, CreatedAt: now, UpdatedAt: now, Strength: 1}}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(legacyPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	executor := &CoreAgentExecutor{}
	store, err := executor.resourcesForUser("tenant-a", "user-a", dataDir)
	if err != nil {
		t.Fatalf("resourcesForUser: %v", err)
	}
	defer store.Stop()

	if got := store.List(memory.CategoryUserFact, "legacy server"); len(got) != 1 {
		t.Fatalf("expected migrated legacy agent memory, got %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "memory", "memories.json")); err != nil {
		t.Fatalf("expected MaClawSrv memory to use corelib canonical memory directory: %v", err)
	}
}

func TestCoreAgentExecutorMemoryToolUsesPrincipalBoundary(t *testing.T) {
	store, err := memory.NewStoreWithMode(t.TempDir(), memory.StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	ownerA := memoryOwnerIDForPrincipal(Principal{TenantID: "tenant-a", UserID: "user-a"})
	ownerB := memoryOwnerIDForPrincipal(Principal{TenantID: "tenant-a", UserID: "user-b"})
	if err := store.Save(memory.Entry{Content: "private api endpoint belongs to owner alpha", Category: memory.CategoryProjectKnowledge, OwnerID: ownerA, Status: memory.StatusActive}); err != nil {
		t.Fatalf("Save owner A: %v", err)
	}
	if err := store.Save(memory.Entry{Content: "private api endpoint belongs to owner beta", Category: memory.CategoryProjectKnowledge, OwnerID: ownerB, Status: memory.StatusActive}); err != nil {
		t.Fatalf("Save owner B: %v", err)
	}

	cb := &coreAgentCallbacks{
		memory:    store,
		principal: Principal{TenantID: "tenant-a", UserID: "user-a"},
		workspace: `D:\workprj\alpha`,
		userText:  "remember the current user context",
	}
	out := cb.ExecuteTool("memory", `{"action":"recall","query":"private api endpoint","mode":"hybrid","limit":10}`)
	if !strings.Contains(out, "owner alpha") || strings.Contains(out, "owner beta") {
		t.Fatalf("MaClawSrv memory recall must be scoped to principal owner, got:\n%s", out)
	}

	out = cb.ExecuteTool("memory", `{"action":"save","content":"Project API endpoint is https://api.owner-a.example.com and test command is pnpm test","category":"project_knowledge"}`)
	if !strings.Contains(out, "Memory saved:") {
		t.Fatalf("expected owner-scoped memory save, got: %s", out)
	}
	entries := store.Search(memory.CategoryProjectKnowledge, "api.owner-a.example.com", 5)
	if len(entries) != 1 || entries[0].OwnerID != ownerA {
		t.Fatalf("saved MaClawSrv memory must carry principal owner %q, got %+v", ownerA, entries)
	}
}

func TestPostMessagePropagatesToolPolicyMetadata(t *testing.T) {
	executor := &captureExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1/test", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{
		Title:    "Session",
		Metadata: map[string]string{"tool_policy": string(workflow.ToolFilterDocOnly)},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content:  "run controlled operation",
		Metadata: map[string]string{"tool_policy": string(workflow.ToolFilterOpsControlled)},
	}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if executor.req.ToolPolicy != workflow.ToolFilterOpsControlled {
		t.Fatalf("ToolPolicy = %q, want %q", executor.req.ToolPolicy, workflow.ToolFilterOpsControlled)
	}
}

func TestPostMessageFallsBackToSessionToolPolicyMetadata(t *testing.T) {
	executor := &captureExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1/test", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{
		Title:    "Session",
		Metadata: map[string]string{"tool_policy": string(workflow.ToolFilterOpsControlled)},
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{Content: "run controlled operation"}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if executor.req.ToolPolicy != workflow.ToolFilterOpsControlled {
		t.Fatalf("ToolPolicy = %q, want %q", executor.req.ToolPolicy, workflow.ToolFilterOpsControlled)
	}
}

func TestPostMessagePropagatesOpsApprovedCommandsMetadata(t *testing.T) {
	executor := &captureExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1/test", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	policyText := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    target: /srv/app
    command: "systemctl restart nginx"
`
	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content: "run controlled operation",
		Metadata: map[string]string{
			"tool_policy":            string(workflow.ToolFilterOpsControlled),
			"ops_execution_approved": "true",
			"ops_approval_digest":    workflow.OpsApprovalDigest(policyText),
			"ops_approved_commands":  policyText,
		},
	}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if len(executor.req.OpsApprovedCommands) != 1 {
		t.Fatalf("OpsApprovedCommands len = %d, want 1: %#v", len(executor.req.OpsApprovedCommands), executor.req.OpsApprovedCommands)
	}
	if executor.req.OpsApprovedCommands[0].Command != "systemctl restart nginx" {
		t.Fatalf("unexpected approved commands: %#v", executor.req.OpsApprovedCommands)
	}
	if executor.req.OpsApprovedCommands[0].Target != "/srv/app" {
		t.Fatalf("approved command target = %q, want /srv/app", executor.req.OpsApprovedCommands[0].Target)
	}
}

func TestOpsApprovedCommandsFromMetadataRequiresApprovalForApprovalRequired(t *testing.T) {
	policyText := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	metadata := map[string]string{
		"ops_approved_commands": policyText,
	}
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 0 {
		t.Fatalf("approval_required without approval flag should not propagate commands: %#v", got)
	}

	metadata["ops_execution_approved"] = "approved"
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 0 {
		t.Fatalf("approval_required without digest should not propagate commands: %#v", got)
	}

	metadata["ops_approval_digest"] = "bad-digest"
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 0 {
		t.Fatalf("approval_required with mismatched digest should not propagate commands: %#v", got)
	}

	metadata["ops_approval_digest"] = workflow.OpsApprovalDigest(policyText)
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 1 {
		t.Fatalf("approval_required with approval flag and digest should propagate commands: %#v", got)
	}
}

func TestOpsApprovedCommandsFromMetadataAllowsAutoExecuteWithoutApprovalFlag(t *testing.T) {
	got := opsApprovedCommandsFromMetadata(map[string]string{
		"ops_approved_commands": `
decision: auto_execute
risk_level: L1
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl status nginx"
`,
	}, nil)
	if len(got) != 1 {
		t.Fatalf("auto_execute should propagate commands without approval flag: %#v", got)
	}
}

func TestOpsApprovedCommandsFromMetadataRequiresDoubleApprovalWhenPolicyRequiresDouble(t *testing.T) {
	policyText := `
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	metadata := map[string]string{
		"ops_approved_commands":        policyText,
		"ops_approval_digest":          workflow.OpsApprovalDigest(policyText),
		"ops_execution_approval_level": "single",
	}
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 0 {
		t.Fatalf("single approval should not satisfy double-required policy: %#v", got)
	}

	metadata["ops_execution_approval_level"] = "double"
	if got := opsApprovedCommandsFromMetadata(metadata, nil); len(got) != 1 {
		t.Fatalf("double approval should satisfy double-required policy: %#v", got)
	}
}

func TestOpsApprovedCommandsFromMetadataPreservesPolicyStrengthMetadata(t *testing.T) {
	policyText := `
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: ssh
    action: close_all
    command: "all"
`
	metadata := map[string]string{
		"ops_approved_commands":        policyText,
		"ops_approval_digest":          workflow.OpsApprovalDigest(policyText),
		"ops_execution_approval_level": "double",
	}
	got := opsApprovedCommandsFromMetadata(metadata, nil)
	if len(got) != 1 {
		t.Fatalf("double-approved close_all policy should propagate one command: %#v", got)
	}
	if got[0].RiskLevel != workflow.OpsRiskLevelL3 || got[0].ApprovalRequirement != workflow.OpsApprovalRequirementDouble {
		t.Fatalf("approved command lost policy strength metadata: %#v", got[0])
	}
}

func TestOpsApprovedCommandsFromMetadataCombinesSessionPolicyWithMessageApproval(t *testing.T) {
	policyText := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	messageMetadata := map[string]string{
		"ops_execution_approved": "true",
		"ops_approval_digest":    workflow.OpsApprovalDigest(policyText),
	}
	sessionMetadata := map[string]string{
		"ops_approved_commands": policyText,
	}
	if got := opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata); len(got) != 1 {
		t.Fatalf("message approval should satisfy session policy when digest matches: %#v", got)
	}
}

func TestOpsApprovedCommandsFromMetadataMessagePolicyOverridesSessionPolicy(t *testing.T) {
	sessionPolicy := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	messagePolicy := `
decision: deny
risk_level: L4
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	messageMetadata := map[string]string{
		"ops_approved_commands":  messagePolicy,
		"ops_execution_approved": "true",
		"ops_approval_digest":    workflow.OpsApprovalDigest(messagePolicy),
	}
	sessionMetadata := map[string]string{
		"ops_approved_commands":  sessionPolicy,
		"ops_execution_approved": "true",
		"ops_approval_digest":    workflow.OpsApprovalDigest(sessionPolicy),
	}
	if got := opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata); len(got) != 0 {
		t.Fatalf("message policy should override stale session policy: %#v", got)
	}
}

func TestOpsApprovedCommandsFromMetadataRequiresMessageApprovalForMessagePolicy(t *testing.T) {
	sessionPolicy := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	messagePolicy := `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart mysql"
`
	messageMetadata := map[string]string{
		"ops_approved_commands": messagePolicy,
		"ops_approval_digest":   workflow.OpsApprovalDigest(messagePolicy),
	}
	sessionMetadata := map[string]string{
		"ops_approved_commands":  sessionPolicy,
		"ops_execution_approved": "true",
		"ops_approval_digest":    workflow.OpsApprovalDigest(sessionPolicy),
	}
	if got := opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata); len(got) != 0 {
		t.Fatalf("message policy should not inherit stale session approval: %#v", got)
	}

	messageMetadata["ops_execution_approved"] = "true"
	if got := opsApprovedCommandsFromMetadata(messageMetadata, sessionMetadata); len(got) != 1 || got[0].Command != "systemctl restart mysql" {
		t.Fatalf("message policy should use message-scoped approval and digest: %#v", got)
	}
}

func TestPostMessageDoesNotPropagateDeniedOpsApprovedCommandsMetadata(t *testing.T) {
	executor := &captureExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "01234567890123456789012345678901", TokenTTL: time.Hour}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(context.Background(), principal, corelib.AppConfig{MaclawLLMUrl: "http://127.0.0.1/test", MaclawLLMKey: "test-key", MaclawLLMModel: "test-model"}); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	inst, err := svc.CreateInstance(context.Background(), principal, CreateInstanceInput{Name: "Instance"})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	sess, err := svc.CreateSession(context.Background(), principal, inst.ID, CreateSessionInput{Title: "Session"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, _, err := svc.PostMessage(context.Background(), principal, inst.ID, sess.ID, PostMessageInput{
		Content: "run controlled operation",
		Metadata: map[string]string{
			"tool_policy": string(workflow.ToolFilterOpsControlled),
			"ops_approved_commands": `
decision: deny
risk_level: L4
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`,
		},
	}); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	if len(executor.req.OpsApprovedCommands) != 0 {
		t.Fatalf("denied policy should not propagate commands: %#v", executor.req.OpsApprovedCommands)
	}
}

func TestEnsureBashWorkingDirUsesInstanceWorkspace(t *testing.T) {
	args := ensureBashWorkingDir(map[string]interface{}{"command": "pwd"}, "/tmp/workspace")
	if got := args["working_dir"]; got != "/tmp/workspace" {
		t.Fatalf("expected working_dir to default to workspace, got %#v", got)
	}
}

func TestEnsureBashWorkingDirPreservesExplicitDir(t *testing.T) {
	args := ensureBashWorkingDir(map[string]interface{}{"command": "pwd", "working_dir": "/tmp/custom"}, "/tmp/workspace")
	if got := args["working_dir"]; got != "/tmp/custom" {
		t.Fatalf("expected explicit working_dir to be preserved, got %#v", got)
	}
}

func TestCoreAgentKnowledgeImportToolsAcceptStructuredStringSlices(t *testing.T) {
	fileA := filepath.Join(t.TempDir(), "a.md")
	fileB := filepath.Join(t.TempDir(), "b.md")
	req := buildDirectoryImportRequest(map[string]interface{}{
		"include_exts": []string{".md,.txt"},
		"labels":       []interface{}{"alpha; beta"},
		"max_file_mb":  2,
	}, "tenant_a", "user_a")
	if len(req.IncludeExts) != 2 || req.IncludeExts[0] != ".md" || req.IncludeExts[1] != ".txt" {
		t.Fatalf("unexpected include_exts: %#v", req.IncludeExts)
	}
	if len(req.Labels) != 2 || req.Labels[0] != "alpha" || req.Labels[1] != "beta" {
		t.Fatalf("unexpected labels: %#v", req.Labels)
	}
	if req.MaxFileBytes != 2*1024*1024 {
		t.Fatalf("unexpected MaxFileBytes: %d", req.MaxFileBytes)
	}

	paths := toStringSlice([]string{fileA + "\n" + fileB})
	if len(paths) != 2 || paths[0] != fileA || paths[1] != fileB {
		t.Fatalf("unexpected file paths: %#v", paths)
	}
}

func TestCoreAgentKnowledgeImportToolsExecuteAgainstStore(t *testing.T) {
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	root := t.TempDir()
	filePath := filepath.Join(root, "note.md")
	if err := os.WriteFile(filePath, []byte("# Note\n\nAgent service import smoke test."), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}

	cb := &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: store,
		principal:      Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:      root,
	}
	tools := cb.BuildTools("")
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tooldef.Name(tool)] = true
	}
	if !seen["knowledge_import_directory"] || !seen["knowledge_import_files"] {
		t.Fatalf("expected knowledge import tools in %#v", seen)
	}

	importFilesProps := map[string]interface{}{}
	for _, tool := range tools {
		if tooldef.Name(tool) != "knowledge_import_files" {
			continue
		}
		fn, _ := tool["function"].(map[string]interface{})
		params, _ := fn["parameters"].(map[string]interface{})
		importFilesProps, _ = params["properties"].(map[string]interface{})
	}
	for _, prop := range []string{"root_path", "labels", "exclude_globs", "distill_mode", "auto_labels"} {
		if _, ok := importFilesProps[prop]; !ok {
			t.Fatalf("knowledge_import_files schema missing %s in %#v", prop, importFilesProps)
		}
	}

	scanArgs, err := json.Marshal(map[string]interface{}{
		"action":       "scan",
		"file_paths":   []string{filePath},
		"include_exts": []string{".md"},
		"max_file_mb":  1,
	})
	if err != nil {
		t.Fatalf("marshal scan args: %v", err)
	}
	scan := cb.ExecuteToolStructured("knowledge_import_files", string(scanArgs))
	if scan.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(scan.Result, "scanned") {
		t.Fatalf("unexpected scan result: outcome=%s result=%s", scan.Outcome, scan.Result)
	}

	importArgs, err := json.Marshal(map[string]interface{}{
		"action":       "import",
		"root_path":    root,
		"include_exts": []string{".md"},
		"max_file_mb":  1,
	})
	if err != nil {
		t.Fatalf("marshal import args: %v", err)
	}
	imported := cb.ExecuteToolStructured("knowledge_import_directory", string(importArgs))
	if imported.Outcome != agent.ToolExecutionOutcomeOK || !strings.Contains(imported.Result, "imported=1") {
		t.Fatalf("unexpected import result: outcome=%s result=%s", imported.Outcome, imported.Result)
	}
}

func TestCoreAgentKnowledgeImportRejectsPathsOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	cb := &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: noOpKnowledgeStore{},
		principal:      Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:      workspace,
	}
	args, err := json.Marshal(map[string]interface{}{
		"action":     "scan",
		"file_paths": []string{outside},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out := cb.ExecuteToolStructured("knowledge_import_files", string(args))
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "outside workspace") {
		t.Fatalf("expected workspace boundary rejection, got outcome=%s result=%s", out.Outcome, out.Result)
	}
}

func TestCoreAgentKnowledgeImportFilesPreservesPunctuationInArrayPaths(t *testing.T) {
	workspace := t.TempDir()
	filePath := filepath.Join(workspace, "note;v1,final.md")
	if err := os.WriteFile(filePath, []byte("punctuated path"), 0o644); err != nil {
		t.Fatalf("write punctuated file: %v", err)
	}

	cb := &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: noOpKnowledgeStore{},
		principal:      Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:      workspace,
	}
	args, err := json.Marshal(map[string]interface{}{
		"action":     "scan",
		"file_paths": []string{filePath},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out := cb.ExecuteToolStructured("knowledge_import_files", string(args))
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("expected punctuated array path to stay intact, got outcome=%s result=%s", out.Outcome, out.Result)
	}
}

func TestCoreAgentKnowledgeImportFilesRejectsFilesOutsideExplicitRoot(t *testing.T) {
	workspace := t.TempDir()
	root := filepath.Join(workspace, "docs")
	other := filepath.Join(workspace, "other")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	outsideRoot := filepath.Join(other, "note.md")
	if err := os.WriteFile(outsideRoot, []byte("outside explicit root"), 0o644); err != nil {
		t.Fatalf("write outside root: %v", err)
	}

	cb := &coreAgentCallbacks{
		ctx:            context.Background(),
		knowledgeStore: noOpKnowledgeStore{},
		principal:      Principal{TenantID: "tenant_a", UserID: "user_a"},
		workspace:      workspace,
	}
	args, err := json.Marshal(map[string]interface{}{
		"action":     "scan",
		"root_path":  root,
		"file_paths": []string{outsideRoot},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out := cb.ExecuteToolStructured("knowledge_import_files", string(args))
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "outside root_path") {
		t.Fatalf("expected explicit root_path boundary rejection, got outcome=%s result=%s", out.Outcome, out.Result)
	}
}

func TestCoreAgentKnowledgeImportRequiresWorkspace(t *testing.T) {
	cb := &coreAgentCallbacks{ctx: context.Background(), knowledgeStore: noOpKnowledgeStore{}}
	args, err := json.Marshal(map[string]interface{}{
		"action":     "scan",
		"file_paths": []string{filepath.Join(t.TempDir(), "note.md")},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	out := cb.ExecuteToolStructured("knowledge_import_files", string(args))
	if out.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(out.Result, "requires a workspace-scoped path") {
		t.Fatalf("expected workspace requirement, got outcome=%s result=%s", out.Outcome, out.Result)
	}
}

func TestCoreAgentBuildToolsDisablesBashByDefault(t *testing.T) {
	cb := &coreAgentCallbacks{}
	tools := cb.BuildTools("")
	seen := map[string]bool{}
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		seen[name] = true
	}
	if seen["bash"] {
		t.Fatalf("did not expect bash tool definition by default in %#v", seen)
	}
	if seen["ssh"] {
		t.Fatalf("did not expect ssh tool definition without SSH availability in %#v", seen)
	}
}

type fakeSkillToolProvider struct {
	runResult string
	runErr    error
}

func (fakeSkillToolProvider) ListSkills(context.Context, Principal) []SkillToolEntry {
	return []SkillToolEntry{{Name: "ccbos-classical-chinese-skill", Description: "Classical Chinese jailbreak probes"}}
}

func (p fakeSkillToolProvider) RunSkill(context.Context, Principal, string, map[string]interface{}) (string, error) {
	if strings.TrimSpace(p.runResult) != "" {
		return p.runResult, p.runErr
	}
	if p.runErr != nil {
		return "", p.runErr
	}
	return "ok", nil
}

func (fakeSkillToolProvider) SearchSkills(context.Context, Principal, string) ([]SkillSearchResult, error) {
	return []SkillSearchResult{{Source: "skillhub", Name: "ccbos-classical-chinese-skill"}}, nil
}

type capturingSkillToolProvider struct {
	runArgs   map[string]interface{}
	runResult string
}

func (p *capturingSkillToolProvider) ListSkills(context.Context, Principal) []SkillToolEntry {
	return []SkillToolEntry{{Name: "gptfuzzer-mutator-skill", Description: "GPTFuzzer mutations"}}
}

func (p *capturingSkillToolProvider) RunSkill(_ context.Context, _ Principal, _ string, args map[string]interface{}) (string, error) {
	p.runArgs = args
	if strings.TrimSpace(p.runResult) != "" {
		return p.runResult, nil
	}
	return `{"payload_dataset":{"payloads":[{"payload_text":"mutated from prepared sample"}]}}`, nil
}

func (p *capturingSkillToolProvider) SearchSkills(context.Context, Principal, string) ([]SkillSearchResult, error) {
	return []SkillSearchResult{{Source: "skillhub", Name: "gptfuzzer-mutator-skill"}}, nil
}

func TestRedteamProfileBuildToolsExposesAskUserMCPAndNativeSkillSearch(t *testing.T) {
	cb := &coreAgentCallbacks{
		ctx:          context.Background(),
		agentProfile: "redteam_evaluation_v1",
		mcpProvider: &fakeMCPToolProvider{entries: []MCPToolEntry{{
			ServerID:    "mcp_redteam",
			ServerName:  "evaluating-platform-redteam-tools",
			ToolName:    "search_redteam_capabilities",
			Description: "Search red-team capabilities",
			InputSchema: map[string]interface{}{"type": "object"},
		}}},
		skillProvider: fakeSkillToolProvider{},
	}

	tools := cb.BuildTools("")
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tooldef.Name(tool)] = true
	}

	if !seen["ask_user"] || !seen["search_redteam_capabilities"] || !seen["manage_skill"] {
		t.Fatalf("expected ask_user, red-team MCP search, and native manage_skill tool, got %#v", seen)
	}
	for _, forbidden := range []string{"read_file", "write_file", "edit_file", "web_search", "knowledge_search", "bash"} {
		if seen[forbidden] {
			t.Fatalf("redteam profile should not expose %s in %#v", forbidden, seen)
		}
	}
}

func TestRedteamProfileToolCapabilitiesOnlyDescribeAskUser(t *testing.T) {
	cb := &coreAgentCallbacks{agentProfile: "redteam_evaluation_v1"}

	caps := cb.toolCapabilities()
	seen := map[string]bool{}
	for _, cap := range caps {
		seen[cap.Name] = true
	}

	if !seen["ask_user"] {
		t.Fatalf("expected ask_user capability in %#v", seen)
	}
	for _, forbidden := range []string{"read_file", "write_file", "web_search", "knowledge_search", "manage_skill", "task"} {
		if seen[forbidden] {
			t.Fatalf("redteam profile prompt should not describe %s in %#v", forbidden, seen)
		}
	}
}

func TestRedteamProfileManageSkillRunRequiresConfirmedSelectedSkill(t *testing.T) {
	cb := &coreAgentCallbacks{
		ctx:           context.Background(),
		agentProfile:  "redteam_evaluation_v1",
		skillProvider: fakeSkillToolProvider{},
	}
	args := `{"action":"run","name":"ccbos-classical-chinese-skill"}`
	blocked := cb.ExecuteToolStructured("manage_skill", args)
	if blocked.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(blocked.Result, "accepted plan_confirm") {
		t.Fatalf("blocked run = %#v", blocked)
	}

	cb.messageMetadata = map[string]string{
		"evaluation_action":         "confirm_plan",
		"selected_skill_names_json": `["ccbos-classical-chinese-skill"]`,
	}
	allowed := cb.ExecuteToolStructured("manage_skill", args)
	if allowed.Outcome != agent.ToolExecutionOutcomeOK || allowed.Result != "ok" {
		t.Fatalf("allowed run = %#v", allowed)
	}
}

func TestRedteamProfileBatchToolRequiresSelectedSkillRunFirst(t *testing.T) {
	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "execute_redteam_evaluation_batch",
		Description: "Execute confirmed red-team evaluation batch.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	cb := &coreAgentCallbacks{
		ctx:           context.Background(),
		agentProfile:  "redteam_evaluation_v1",
		mcpProvider:   mcpProvider,
		skillProvider: fakeSkillToolProvider{runResult: `{"payload_dataset":{"payloads":[{"payload_text":"rewritten from selected Skill"}]}}`},
		messageMetadata: map[string]string{
			"evaluation_action":         "confirm_plan",
			"selected_skill_names_json": `["ccbos-classical-chinese-skill"]`,
			"run_id":                    "run_1",
			"session_id":                "sess_1",
		},
	}

	blocked := cb.ExecuteToolStructured("execute_redteam_evaluation_batch", `{"run_id":"run_1","session_id":"sess_1","test_count":5}`)
	if blocked.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(blocked.Result, "manage_skill(action=\"run\")") {
		t.Fatalf("batch should be blocked until selected Skill runs, got %#v", blocked)
	}
	if len(mcpProvider.calls) != 0 {
		t.Fatalf("batch MCP should not be called before selected Skill run: %#v", mcpProvider.calls)
	}

	run := cb.ExecuteToolStructured("manage_skill", `{"action":"run","name":"ccbos-classical-chinese-skill","args":{"questions":["demo"]}}`)
	if run.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("selected Skill run = %#v", run)
	}
	if len(mcpProvider.calls) != 1 || mcpProvider.calls[0] != "mcp_redteam/register_skill_payload_dataset" {
		t.Fatalf("selected Skill output should be auto-registered, calls=%#v run=%#v", mcpProvider.calls, run)
	}
	if mcpProvider.lastArgs["run_id"] != "run_1" || mcpProvider.lastArgs["session_id"] != "sess_1" {
		t.Fatalf("register should inherit run/session ids from confirmed metadata, got %#v", mcpProvider.lastArgs)
	}
	data, _ := json.Marshal(mcpProvider.lastArgs["payload_dataset"])
	if !strings.Contains(string(data), "rewritten from selected Skill") || strings.Contains(string(data), `"rewritten"`) && !strings.Contains(string(data), "from selected Skill") {
		t.Fatalf("register must use trusted Skill output dataset, got %s", data)
	}

	allowed := cb.ExecuteToolStructured("execute_redteam_evaluation_batch", `{"run_id":"run_1","session_id":"sess_1","test_count":5,"selected_skills":["ccbos-classical-chinese-skill"]}`)
	if allowed.Outcome != agent.ToolExecutionOutcomeOK || len(mcpProvider.calls) != 2 || mcpProvider.calls[1] != "mcp_redteam/execute_redteam_evaluation_batch" {
		t.Fatalf("batch after auto-registered Skill handles = %#v calls=%#v", allowed, mcpProvider.calls)
	}
	if got := mcpProvider.lastArgs["payload_handles"]; !strings.Contains(fmt.Sprint(got), "redteam_payload_1") {
		t.Fatalf("batch should receive auto-registered payload handles, args=%#v", mcpProvider.lastArgs)
	}
}

func TestRedteamConfirmedSelectedSkillExecutesBatchWithoutLLMLoop(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "execute_redteam_evaluation_batch",
		Description: "Execute confirmed red-team evaluation batch.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetMCPToolProvider(mcpProvider)
	executor.SetSkillToolProvider(fakeSkillToolProvider{runResult: `{"payload_dataset":{"payloads":[{"payload_text":"rewritten from selected Skill"}]}}`})
	req := redteamFastReplyRequest(t, server.URL, "confirm execution")
	req.Message.Metadata = map[string]string{
		"agent_profile":                  "redteam_evaluation_v1",
		"evaluation_action":              "confirm_plan",
		"selected_skill_names_json":      `["ccbos-classical-chinese-skill"]`,
		"run_id":                         "run_1",
		"session_id":                     "sess_1",
		"test_count":                     "2",
		"evaluation_execution_grant":     "grant_1",
		"evaluation_execution_grant_exp": "9999999999",
	}
	req.History = []Message{{
		ID:      "plan_1",
		Role:    MessageRoleAssistant,
		Content: `{"response_source":"plan_confirm","target_summary":"default target","risk_types":["jailbreak"],"selected_skills":["ccbos-classical-chinese-skill"],"selected_capability_refs":["skillhub:ccbos-classical-chinese-skill"],"selection_strategy":"maclaw_selected","test_count":2,"requires_confirmation":true}`,
	}}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("confirmed Skill execution should not enter LLM loop, got %d calls", llmCalls)
	}
	if result.Metadata["evaluation_event_type"] != "report" || !strings.Contains(result.Content, "redteam_report_1") {
		t.Fatalf("result = %#v", result)
	}
	if len(mcpProvider.calls) != 2 || mcpProvider.calls[0] != "mcp_redteam/register_skill_payload_dataset" || mcpProvider.calls[1] != "mcp_redteam/execute_redteam_evaluation_batch" {
		t.Fatalf("MCP calls = %#v", mcpProvider.calls)
	}
	if got := mcpProvider.lastArgs["payload_handles"]; !strings.Contains(fmt.Sprint(got), "redteam_payload_1") {
		t.Fatalf("batch should receive registered Skill payload handles, args=%#v", mcpProvider.lastArgs)
	}
	if got := fmt.Sprint(mcpProvider.lastArgs["test_count"]); got != "2" {
		t.Fatalf("batch test_count = %s, want 2; args=%#v", got, mcpProvider.lastArgs)
	}
}

func TestRedteamConfirmedSelectedSkillPassesOptimizedBatchArgs(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "execute_redteam_evaluation_batch",
		Description: "Execute confirmed red-team evaluation batch.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	skillProvider := &capturingSkillToolProvider{runResult: `{"payload_dataset":{"payloads":[{"payload_text":"rewritten from selected Skill"}]}}`}
	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetMCPToolProvider(mcpProvider)
	executor.SetSkillToolProvider(skillProvider)
	req := redteamFastReplyRequest(t, server.URL, "confirm execution")
	req.Message.Metadata = map[string]string{
		"agent_profile":                  "redteam_evaluation_v1",
		"evaluation_action":              "confirm_plan",
		"selected_skill_names_json":      `["ccbos-classical-chinese-skill"]`,
		"run_id":                         "run_1",
		"session_id":                     "sess_1",
		"test_count":                     "20",
		"evaluation_execution_grant":     "grant_1",
		"evaluation_execution_grant_exp": "9999999999",
	}
	req.History = []Message{{
		ID:      "plan_1",
		Role:    MessageRoleAssistant,
		Content: `{"response_source":"plan_confirm","target_summary":"default target","risk_types":["jailbreak"],"selected_skills":["ccbos-classical-chinese-skill"],"selected_capability_refs":["skillhub:ccbos-classical-chinese-skill"],"selection_strategy":"maclaw_selected","test_count":20,"requires_confirmation":true}`,
	}}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("confirmed Skill execution should not enter LLM loop, got %d calls", llmCalls)
	}
	if result.Metadata["evaluation_event_type"] != "report" {
		t.Fatalf("result = %#v", result)
	}
	nested, ok := skillProvider.runArgs["args"].(map[string]interface{})
	if !ok {
		t.Fatalf("skill args = %#v", skillProvider.runArgs)
	}
	if got := fmt.Sprint(nested["batch_size"]); got != "5" {
		t.Fatalf("batch_size = %s, want 5; args=%#v", got, nested)
	}
	if got := fmt.Sprint(nested["batch_concurrency"]); got != "5" {
		t.Fatalf("batch_concurrency = %s, want 5; args=%#v", got, nested)
	}
	if got := fmt.Sprint(nested["test_count"]); got != "20" {
		t.Fatalf("test_count = %s, want 20; args=%#v", got, nested)
	}
}

func TestRedteamManageSkillRunAddsBatchArgsInConfirmedAgentLoop(t *testing.T) {
	skillProvider := &capturingSkillToolProvider{runResult: `{"payload_dataset":{"payloads":[{"payload_text":"rewritten from selected Skill"}]}}`}
	cb := &coreAgentCallbacks{
		ctx:          context.Background(),
		agentProfile: "redteam_evaluation_v1",
		messageMetadata: map[string]string{
			"evaluation_action":              "confirm_plan",
			"selected_skill_names_json":      `["ccbos-classical-chinese-skill"]`,
			"test_count":                     "20",
			"current_target_model":           "deepseek-chat",
			"evaluation_execution_grant":     "grant_1",
			"evaluation_execution_grant_exp": "9999999999",
		},
		skillProvider: skillProvider,
	}

	result := cb.executeManageSkill(map[string]interface{}{
		"action": "run",
		"name":   "ccbos-classical-chinese-skill",
		"args": map[string]interface{}{
			"questions":         []interface{}{"demo"},
			"batch_size":        1,
			"batch_concurrency": 1,
		},
	})
	if result.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("manage_skill run = %#v", result)
	}
	nested, ok := skillProvider.runArgs["args"].(map[string]interface{})
	if !ok {
		t.Fatalf("skill args = %#v", skillProvider.runArgs)
	}
	if got := fmt.Sprint(nested["test_count"]); got != "20" {
		t.Fatalf("test_count = %s, want 20; args=%#v", got, nested)
	}
	if got := fmt.Sprint(nested["batch_size"]); got != "5" {
		t.Fatalf("batch_size = %s, want 5; args=%#v", got, nested)
	}
	if got := fmt.Sprint(nested["batch_concurrency"]); got != "5" {
		t.Fatalf("batch_concurrency = %s, want 5; args=%#v", got, nested)
	}
	if _, ok := nested["questions"]; !ok {
		t.Fatalf("existing Skill args should be preserved: %#v", nested)
	}
}

func TestRedteamConfirmedSelectedSkillRunsWithPreparedExpertSamples(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "prepare_skill_input_data",
		Description: "Prepare selected expert samples for Skill input.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "execute_redteam_evaluation_batch",
		Description: "Execute confirmed red-team evaluation batch.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	skillProvider := &capturingSkillToolProvider{}
	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetMCPToolProvider(mcpProvider)
	executor.SetSkillToolProvider(skillProvider)
	req := redteamFastReplyRequest(t, server.URL, "confirm execution")
	req.Message.Metadata = map[string]string{
		"agent_profile":                  "redteam_evaluation_v1",
		"evaluation_action":              "confirm_plan",
		"selected_skill_names_json":      `["gptfuzzer-mutator-skill"]`,
		"selected_capability_refs_json":  `["skillhub:gptfuzzer-mutator-skill","sample:sample_1","composed_attack:attack_1"]`,
		"run_id":                         "run_1",
		"session_id":                     "sess_1",
		"test_count":                     "3",
		"evaluation_execution_grant":     "grant_1",
		"evaluation_execution_grant_exp": "9999999999",
	}
	req.History = []Message{{
		ID:      "plan_1",
		Role:    MessageRoleAssistant,
		Content: `{"response_source":"plan_confirm","target_summary":"default target","risk_types":["jailbreak"],"selected_skills":["gptfuzzer-mutator-skill"],"selected_capability_refs":["skillhub:gptfuzzer-mutator-skill","sample:sample_1","composed_attack:attack_1"],"selection_strategy":"maclaw_selected","test_count":3,"requires_confirmation":true}`,
	}}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("confirmed Skill execution should not enter LLM loop, got %d calls", llmCalls)
	}
	if result.Metadata["evaluation_event_type"] != "report" {
		t.Fatalf("result = %#v", result)
	}
	if len(mcpProvider.calls) != 3 || mcpProvider.calls[0] != "mcp_redteam/prepare_skill_input_data" || mcpProvider.calls[1] != "mcp_redteam/register_skill_payload_dataset" || mcpProvider.calls[2] != "mcp_redteam/execute_redteam_evaluation_batch" {
		t.Fatalf("MCP calls = %#v", mcpProvider.calls)
	}
	nested, ok := skillProvider.runArgs["args"].(map[string]interface{})
	if !ok {
		t.Fatalf("skill args = %#v", skillProvider.runArgs)
	}
	raw, _ := json.Marshal(nested)
	text := string(raw)
	for _, want := range []string{"prepared expert sample question", "prepared composed attack text", `"test_count":3`} {
		if !strings.Contains(text, want) {
			t.Fatalf("skill input %s missing %q; args=%s", text, want, text)
		}
	}
}

func TestRedteamConfirmedSelectedSkillUsesPayloadDatasetFromFailedSkillStep(t *testing.T) {
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		http.Error(w, "unexpected llm call", http.StatusInternalServerError)
	}))
	defer server.Close()

	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}, {
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "execute_redteam_evaluation_batch",
		Description: "Execute confirmed red-team evaluation batch.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	executor.SetMCPToolProvider(mcpProvider)
	executor.SetSkillToolProvider(fakeSkillToolProvider{
		runResult: `{"payload_dataset":{"payloads":[{"payload_text":"usable rewritten payload from killed step"}]}}`,
		runErr:    fmt.Errorf("step 1 failed: signal: killed"),
	})
	req := redteamFastReplyRequest(t, server.URL, "confirm execution")
	req.Message.Metadata = map[string]string{
		"agent_profile":                  "redteam_evaluation_v1",
		"evaluation_action":              "confirm_plan",
		"selected_skill_names_json":      `["ccbos-classical-chinese-skill"]`,
		"run_id":                         "run_1",
		"session_id":                     "sess_1",
		"test_count":                     "5",
		"evaluation_execution_grant":     "grant_1",
		"evaluation_execution_grant_exp": "9999999999",
	}
	req.History = []Message{{
		ID:      "plan_1",
		Role:    MessageRoleAssistant,
		Content: `{"response_source":"plan_confirm","target_summary":"default target","risk_types":["jailbreak"],"selected_skills":["ccbos-classical-chinese-skill"],"selected_capability_refs":["skillhub:ccbos-classical-chinese-skill"],"selection_strategy":"maclaw_selected","test_count":5,"requires_confirmation":true}`,
	}}

	result, err := executor.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if llmCalls != 0 {
		t.Fatalf("confirmed Skill execution should not enter LLM loop, got %d calls", llmCalls)
	}
	if result.Metadata["evaluation_event_type"] != "report" || !strings.Contains(result.Content, "redteam_report_1") {
		t.Fatalf("result = %#v", result)
	}
	if len(mcpProvider.calls) != 2 || mcpProvider.calls[0] != "mcp_redteam/register_skill_payload_dataset" || mcpProvider.calls[1] != "mcp_redteam/execute_redteam_evaluation_batch" {
		t.Fatalf("MCP calls = %#v", mcpProvider.calls)
	}
	data, _ := json.Marshal(mcpProvider.lastArgs["payload_handles"])
	if !strings.Contains(string(data), "redteam_payload_1") {
		t.Fatalf("batch should receive registered payload handles, args=%#v", mcpProvider.lastArgs)
	}
}

func TestSkillRunTimeoutUsesSkillGlobalTimeoutWithSafeDefault(t *testing.T) {
	if got := skillRunTimeout(&corelib.NLSkillEntry{}); got != 300*time.Second {
		t.Fatalf("default skill timeout = %s, want 300s", got)
	}
	if got := skillRunTimeout(&corelib.NLSkillEntry{GlobalTimeout: 600}); got != 600*time.Second {
		t.Fatalf("global skill timeout = %s, want 600s", got)
	}
	if got := skillRunTimeout(&corelib.NLSkillEntry{GlobalTimeout: -1}); got != 300*time.Second {
		t.Fatalf("invalid global skill timeout = %s, want default 300s", got)
	}
}

func TestRedteamProfileRegisterSkillPayloadRequiresActualSkillOutput(t *testing.T) {
	mcpProvider := &fakeMCPToolProvider{entries: []MCPToolEntry{{
		ServerID:    "mcp_redteam",
		ServerName:  "evaluating-platform-redteam-tools",
		ToolName:    "register_skill_payload_dataset",
		Description: "Register Skill output payloads.",
		InputSchema: map[string]interface{}{"type": "object"},
	}}}
	cb := &coreAgentCallbacks{
		ctx:           context.Background(),
		agentProfile:  "redteam_evaluation_v1",
		mcpProvider:   mcpProvider,
		skillProvider: fakeSkillToolProvider{runResult: "ok"},
		messageMetadata: map[string]string{
			"evaluation_action":         "confirm_plan",
			"selected_skill_names_json": `["ccbos-classical-chinese-skill"]`,
		},
	}

	run := cb.ExecuteToolStructured("manage_skill", `{"action":"run","name":"ccbos-classical-chinese-skill","args":{"questions":["demo"]}}`)
	if run.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("selected Skill run = %#v", run)
	}

	fabricated := cb.ExecuteToolStructured("register_skill_payload_dataset", `{"run_id":"run_1","session_id":"sess_1","skill_name":"ccbos-classical-chinese-skill","payload_dataset":{"payloads":[{"payload_text":"fabricated fallback"}]}}`)
	if fabricated.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(fabricated.Result, "did not return payload_dataset") {
		t.Fatalf("fabricated register should be blocked, got %#v", fabricated)
	}
	if len(mcpProvider.calls) != 0 {
		t.Fatalf("register MCP should not be called with fabricated payloads: %#v", mcpProvider.calls)
	}
}

func TestCoreAgentBuildToolsIncludesBashWhenEnabled(t *testing.T) {
	cb := &coreAgentCallbacks{allowLocalBash: true, localBashTrustedSingleUser: true, localBashTenantID: "tenant_a", localBashUserID: "user_a", principal: Principal{TenantID: "tenant_a", UserID: "user_a"}}
	tools := cb.BuildTools("")
	seen := map[string]bool{}
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		seen[name] = true
	}
	if !seen["bash"] {
		t.Fatalf("expected bash tool definition in %#v", seen)
	}
}

func TestCoreAgentToolPolicyFiltersExposedTools(t *testing.T) {
	cb := &coreAgentCallbacks{
		allowLocalBash:             true,
		localBashTrustedSingleUser: true,
		localBashTenantID:          "tenant_a",
		localBashUserID:            "user_a",
		principal:                  Principal{TenantID: "tenant_a", UserID: "user_a"},
		toolPolicy:                 workflow.ToolFilterOpsControlled,
	}
	tools := agent.FilterToolDefinitionsByAuthorizer(cb, cb.BuildTools(""))
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tooldef.Name(tool)] = true
	}
	if !seen["bash"] {
		t.Fatalf("expected bash to remain under ops policy, got %#v", seen)
	}
	if seen["task"] || seen["ask_user"] {
		t.Fatalf("expected non-ops tools to be filtered by ops policy, got %#v", seen)
	}
}

func TestCoreAgentToolPolicyBlocksExecution(t *testing.T) {
	cb := &coreAgentCallbacks{toolPolicy: workflow.ToolFilterOpsControlled}
	if cb.IsToolAllowed("bash") != true {
		t.Fatal("expected bash to be allowed by ops policy")
	}
	if cb.IsToolAllowed("task") {
		t.Fatal("expected task to be blocked by ops policy")
	}
}

func TestCoreAgentToolPolicyBlocksHighRiskCommandArguments(t *testing.T) {
	cb := &coreAgentCallbacks{toolPolicy: workflow.ToolFilterOpsControlled}
	allowed, reason := cb.IsToolCallAllowed("bash", `{"command":"rm -rf / --no-preserve-root"}`)
	if allowed {
		t.Fatal("expected high-risk bash command to be blocked")
	}
	if !strings.Contains(reason, "reviewed runbook") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
	result := cb.ExecuteToolStructured("bash", `{"command":"rm -rf / --no-preserve-root"}`)
	if result.Outcome != agent.ToolExecutionOutcomeError {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, agent.ToolExecutionOutcomeError)
	}
	if !strings.Contains(result.Result, "reviewed runbook") {
		t.Fatalf("unexpected result: %q", result.Result)
	}
}

func TestCoreAgentToolPolicyBlocksMutatingCommandWithoutApprovedManifest(t *testing.T) {
	cb := &coreAgentCallbacks{toolPolicy: workflow.ToolFilterOpsControlled}
	allowed, reason := cb.IsToolCallAllowed("bash", `{"command":"systemctl restart nginx"}`)
	if allowed {
		t.Fatal("expected mutating command without approved manifest to be blocked")
	}
	if !strings.Contains(reason, "allowed_commands") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
	allowed, reason = cb.IsToolCallAllowed("bash", `{"command":"systemctl status nginx"}`)
	if !allowed {
		t.Fatalf("expected read-only command without manifest to pass, got %q", reason)
	}
}

func TestCoreAgentToolPolicyBlocksCommandOutsideApprovedManifest(t *testing.T) {
	cb := &coreAgentCallbacks{
		toolPolicy: workflow.ToolFilterOpsControlled,
		opsApprovedCommands: []workflow.OpsApprovedCommand{
			{Tool: "bash", Command: "systemctl restart nginx"},
		},
	}
	allowed, reason := cb.IsToolCallAllowed("bash", `{"command":"systemctl restart mysql"}`)
	if allowed {
		t.Fatal("expected command outside approved manifest to be blocked")
	}
	if !strings.Contains(reason, "approved risk-policy") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
	allowed, reason = cb.IsToolCallAllowed("bash", `{"command":"systemctl   restart   nginx"}`)
	if !allowed {
		t.Fatalf("expected approved command to pass, got %q", reason)
	}
}

func TestCoreAgentToolPolicyBlocksSSHUploadWithoutApprovedManifest(t *testing.T) {
	cb := &coreAgentCallbacks{toolPolicy: workflow.ToolFilterOpsControlled}
	allowed, reason := cb.IsToolCallAllowed("ssh", `{"action":"upload","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected ssh upload without approved manifest to be blocked")
	}
	if !strings.Contains(reason, "allowed_commands") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}

	cb.opsApprovedCommands = []workflow.OpsApprovedCommand{{Tool: "ssh", Action: "upload", Target: "prod-session", Command: "apply.sh -> /tmp/apply.sh"}}
	allowed, reason = cb.IsToolCallAllowed("ssh", `{"action":"upload","session_id":"prod-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if !allowed {
		t.Fatalf("expected approved ssh upload to pass, got %q", reason)
	}
	allowed, reason = cb.IsToolCallAllowed("ssh", `{"action":"upload","session_id":"prod-session","local_path":"other.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected upload outside manifest to be blocked")
	}
	allowed, reason = cb.IsToolCallAllowed("ssh", `{"action":"upload","session_id":"staging-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`)
	if allowed {
		t.Fatal("expected upload on unapproved target to be blocked")
	}
}

func TestCoreAgentDescribeCapabilitiesShowsDisabledBash(t *testing.T) {
	executor := &CoreAgentExecutor{}
	caps, err := executor.DescribeCapabilities(context.Background(), ExecuteRequest{Instance: Instance{Workspace: "/tmp/workspace"}})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if caps == nil || caps.Executor != "core_agent" || caps.SupportsSSH || !caps.SupportsAskUser || caps.SupportsLocalBash {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
	var bash *AgentToolCapability
	for i := range caps.Tools {
		if caps.Tools[i].Name == "bash" {
			bash = &caps.Tools[i]
			break
		}
	}
	if bash == nil {
		t.Fatalf("expected bash capability in %#v", caps.Tools)
	}
	if bash.Enabled || bash.DisabledReason == "" {
		t.Fatalf("expected disabled bash capability, got %#v", bash)
	}
}

func TestCoreAgentDescribeCapabilitiesEnablesBashWhenAllowed(t *testing.T) {
	executor := &CoreAgentExecutor{AllowLocalBash: true, LocalBashTrustedSingleUser: true, LocalBashTenantID: "tenant_a", LocalBashUserID: "user_a"}
	caps, err := executor.DescribeCapabilities(context.Background(), ExecuteRequest{Principal: Principal{TenantID: "tenant_a", UserID: "user_a"}, Instance: Instance{Workspace: "/tmp/workspace"}})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	var bash *AgentToolCapability
	for i := range caps.Tools {
		if caps.Tools[i].Name == "bash" {
			bash = &caps.Tools[i]
			break
		}
	}
	if bash == nil || !bash.Enabled {
		t.Fatalf("expected enabled bash capability, got %#v", bash)
	}
}

func TestCoreAgentDescribeCapabilitiesShowsSSHUnavailableByDefault(t *testing.T) {
	executor := &CoreAgentExecutor{}
	caps, err := executor.DescribeCapabilities(context.Background(), ExecuteRequest{Instance: Instance{Workspace: "/tmp/workspace"}})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if caps.SupportsSSH {
		t.Fatalf("expected ssh to be unavailable by default, got %#v", caps)
	}
	for i := range caps.Tools {
		if caps.Tools[i].Name == "ssh" && (caps.Tools[i].Enabled || caps.Tools[i].DisabledReason == "") {
			t.Fatalf("expected disabled ssh capability, got %#v", caps.Tools[i])
		}
	}
}

func TestCoreAgentDescribeCapabilitiesEnablesSSHWhenHostsConfigured(t *testing.T) {
	executor := &CoreAgentExecutor{}
	caps, err := executor.DescribeCapabilities(context.Background(), ExecuteRequest{Config: corelib.AppConfig{SSHHosts: []corelib.SSHHostEntry{{Label: "prod", Host: "example.com", User: "root"}}}, Instance: Instance{Workspace: "/tmp/workspace"}})
	if err != nil {
		t.Fatalf("DescribeCapabilities: %v", err)
	}
	if !caps.SupportsSSH {
		t.Fatalf("expected ssh support when hosts are configured, got %#v", caps)
	}
	foundEnabled := false
	for i := range caps.Tools {
		if caps.Tools[i].Name == "ssh" && caps.Tools[i].Enabled {
			foundEnabled = true
		}
	}
	if !foundEnabled {
		t.Fatalf("expected enabled ssh capability, got %#v", caps.Tools)
	}
}

func TestCoreAgentValidateSSHArgsRequiresLabelByDefault(t *testing.T) {
	cb := &coreAgentCallbacks{workspace: t.TempDir()}
	_, err := cb.validateSSHArgs(map[string]interface{}{"action": "connect", "host": "example.com", "user": "root"})
	if err == nil || !strings.Contains(err.Error(), "configured label") {
		t.Fatalf("expected label requirement error, got %v", err)
	}
}

func TestCoreAgentExecuteSSHReturnsUnavailableWhenNotConfigured(t *testing.T) {
	cb := &coreAgentCallbacks{}
	out := cb.ExecuteTool("ssh", `{"action":"connect","label":"prod"}`)
	if !strings.Contains(out, "ssh is unavailable") {
		t.Fatalf("expected ssh unavailable error, got %q", out)
	}
}

func TestCoreAgentValidateSSHArgsRejectsDirectOverrideWhenUsingLabel(t *testing.T) {
	cb := &coreAgentCallbacks{workspace: t.TempDir()}
	_, err := cb.validateSSHArgs(map[string]interface{}{"action": "connect", "label": "prod", "host": "example.com"})
	if err == nil || !strings.Contains(err.Error(), "overriding host") {
		t.Fatalf("expected override rejection, got %v", err)
	}
}

func TestCoreAgentValidateSSHArgsRejectsFileTransferByDefault(t *testing.T) {
	cb := &coreAgentCallbacks{workspace: t.TempDir()}
	_, err := cb.validateSSHArgs(map[string]interface{}{"action": "upload", "local_path": cb.workspace, "remote_path": "/tmp/x"})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected file transfer disabled error, got %v", err)
	}
}

func TestCoreAgentValidateSSHArgsRestrictsFileTransferToWorkspace(t *testing.T) {
	workspace := t.TempDir()
	cb := &coreAgentCallbacks{workspace: workspace, allowSSHFileTransfer: true}
	_, err := cb.validateSSHArgs(map[string]interface{}{"action": "upload", "local_path": filepath.Join(workspace, "..", "escape.txt"), "remote_path": "/tmp/x"})
	if err == nil || !strings.Contains(err.Error(), "instance workspace") {
		t.Fatalf("expected workspace restriction error, got %v", err)
	}
}

func TestCoreAgentBashRequiresTrustedSingleUserMode(t *testing.T) {
	cb := &coreAgentCallbacks{allowLocalBash: true, principal: Principal{TenantID: "tenant_a", UserID: "user_a"}}
	if cb.canUseLocalBash() {
		t.Fatalf("expected local bash to stay disabled without trusted single-user mode")
	}
	if !strings.Contains(cb.localBashDeniedReason(), "MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER") {
		t.Fatalf("unexpected denied reason: %q", cb.localBashDeniedReason())
	}
}

func TestCoreAgentBashRequiresScopedTenantAndUser(t *testing.T) {
	cb := &coreAgentCallbacks{allowLocalBash: true, localBashTrustedSingleUser: true, principal: Principal{TenantID: "tenant_a", UserID: "user_a"}}
	if cb.canUseLocalBash() {
		t.Fatalf("expected local bash to stay disabled without explicit scope")
	}
	if !strings.Contains(cb.localBashDeniedReason(), "MACLAW_LOCAL_BASH_TENANT_ID") {
		t.Fatalf("unexpected denied reason: %q", cb.localBashDeniedReason())
	}
}

func TestCoreAgentBashRespectsScopedPrincipal(t *testing.T) {
	cb := &coreAgentCallbacks{
		allowLocalBash:             true,
		localBashTrustedSingleUser: true,
		localBashTenantID:          "tenant_a",
		localBashUserID:            "user_a",
		principal:                  Principal{TenantID: "tenant_a", UserID: "user_a"},
	}
	if !cb.canUseLocalBash() {
		t.Fatalf("expected scoped local bash to be enabled for matching principal")
	}
	cb.principal = Principal{TenantID: "tenant_a", UserID: "user_b"}
	if cb.canUseLocalBash() {
		t.Fatalf("expected scoped local bash to reject non-matching user")
	}
}
