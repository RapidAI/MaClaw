package agentservice

import (
	"context"
	"encoding/json"
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
	"github.com/RapidAI/CodeClaw/corelib/skill"
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

func TestCoreAgentExecutorReturnsPromptBundleMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "ok",
				},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	executor := &CoreAgentExecutor{HTTPClient: server.Client()}
	result, err := executor.Execute(context.Background(), ExecuteRequest{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   server.URL,
			MaclawLLMKey:   "test-key",
			MaclawLLMModel: "test-model",
		},
		Principal: Principal{TenantID: "tenant-a", UserID: "user-a"},
		DataDir:   t.TempDir(),
		Instance:  Instance{Workspace: t.TempDir()},
		Session:   Session{ID: "session-a", AgentID: "agent-a"},
		Message:   Message{ID: "message-a", Content: "hello"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, key := range []string{"prompt_tokens_stable", "prompt_tokens_session", "prompt_tokens_retrieved", "prompt_tokens_total", "prompt_stable_cache_key"} {
		if result.Metadata[key] == "" {
			t.Fatalf("expected metadata %s, got %#v", key, result.Metadata)
		}
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

type maintenancePlanSkillProvider struct{}

func (maintenancePlanSkillProvider) ListSkills(context.Context, Principal) []SkillToolEntry {
	return nil
}

func (maintenancePlanSkillProvider) RunSkill(context.Context, Principal, string, map[string]interface{}) (string, error) {
	return "", nil
}

func (maintenancePlanSkillProvider) SearchSkills(context.Context, Principal, string) ([]SkillSearchResult, error) {
	return nil, nil
}

func (maintenancePlanSkillProvider) BuildSkillMaintenancePlan(context.Context, Principal, skill.SkillMaintenancePlanOptions) (skill.SkillMaintenancePlan, error) {
	return skill.BuildSkillMaintenancePlan([]corelib.NLSkillEntry{{
		Name:         "fragile-skill",
		UsageCount:   3,
		FailureCount: 3,
		SuccessCount: 0,
	}}, skill.SkillMaintenancePlanOptions{MinFailureRuns: 3, MaxActions: 5}), nil
}

func TestCoreAgentManageSkillMaintenancePlanIsReadOnly(t *testing.T) {
	cb := &coreAgentCallbacks{ctx: context.Background(), skillProvider: maintenancePlanSkillProvider{}}
	out := cb.executeManageSkill(map[string]interface{}{"action": "maintenance_plan", "max_actions": float64(5)})
	if out.Outcome != agent.ToolExecutionOutcomeOK {
		t.Fatalf("Outcome = %q, result = %s", out.Outcome, out.Result)
	}
	var payload struct {
		OK                    bool `json:"ok"`
		NonExecuting          bool `json:"non_executing"`
		Boundary              string
		MaintenancePlanStatus string                     `json:"maintenance_plan_status"`
		Plan                  skill.SkillMaintenancePlan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out.Result), &payload); err != nil {
		t.Fatalf("unmarshal maintenance plan: %v\n%s", err, out.Result)
	}
	if !payload.OK || !payload.NonExecuting || payload.MaintenancePlanStatus != "local_skill_maintenance_plan_no_llm" || !strings.Contains(payload.Boundary, "read-only skill maintenance plan") {
		t.Fatalf("expected read-only maintenance payload: %#v", payload)
	}
	if len(payload.Plan.Actions) == 0 || payload.Plan.Actions[0].Action != skill.MaintenanceActionMarkNeedsReview {
		t.Fatalf("expected review action: %#v", payload.Plan.Actions)
	}
}

func TestCoreAgentManageSkillToolDefIncludesMaintenancePlan(t *testing.T) {
	cb := &coreAgentCallbacks{skillProvider: maintenancePlanSkillProvider{}}
	def := cb.manageSkillToolDef()
	raw, _ := json.Marshal(def)
	text := string(raw)
	for _, want := range []string{"maintenance_plan", "execute_maintenance_plan", "max_actions", "stale_after_days", "min_failure_runs", "duplicate_similarity", "dry_run", "confirm", "approved_actions", "allow_duplicate_retire"} {
		if !strings.Contains(text, want) {
			t.Fatalf("manage_skill tool definition missing %q: %s", want, text)
		}
	}
}
