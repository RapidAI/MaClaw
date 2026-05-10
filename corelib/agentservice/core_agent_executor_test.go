package agentservice

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
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
