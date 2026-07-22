package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestRuntimeContextFromIMMessageSeparatesChannelSessionAndActor(t *testing.T) {
	desktop := runtimeContextFromIMMessage(IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "hi"})
	weixin := runtimeContextFromIMMessage(IMUserMessage{UserID: "o9cq802UzUN9ln7xyVX8S3V93w5g@im.wechat", Platform: "weixin", Text: "hi"})
	background := runtimeContextFromIMMessage(IMUserMessage{UserID: "scheduled_task", IsBackground: true, Text: "sync"})
	discussion := runtimeContextFromIMMessage(IMUserMessage{UserID: "ve-group-executor:session-1", Platform: "ve_group_executor", Text: "hi"})

	if desktop.Source.Channel != "desktop" || desktop.Actor.ActorID != "main-ai" {
		t.Fatalf("desktop runtime = %+v", desktop)
	}
	if weixin.Source.Channel != "im" || weixin.Source.Provider != "weixin" || weixin.Actor.ActorID != "main-ai" {
		t.Fatalf("weixin runtime = %+v", weixin)
	}
	if background.Source.Channel != "system" || background.Actor.ActorID != "system" {
		t.Fatalf("background runtime = %+v", background)
	}
	if discussion.Source.Channel != "discussion" || discussion.Actor.ActorID != "digital-employee" {
		t.Fatalf("discussion runtime = %+v", discussion)
	}
	if desktop.Conversation.SessionKey == weixin.Conversation.SessionKey {
		t.Fatalf("desktop and weixin must not share session key: %q", desktop.Conversation.SessionKey)
	}
	if !strings.Contains(weixin.LockKey, weixin.Conversation.SessionKey) || !strings.Contains(weixin.LockKey, weixin.Actor.ActorID) {
		t.Fatalf("lock key should include session and actor, got %q", weixin.LockKey)
	}
}

func TestToolPolicyTraceIncludesRuntimeOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.traceService = NewAITraceService()
	userID := "runtime-policy-trace-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	msg := IMUserMessage{UserID: userID, Platform: desktopPlatform, Text: "write code"}
	loopCtx := handler.prepareIMLoopContext(nil, msg, nil, false, false)
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context: loopCtx,
		UserID:  userID,
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.txt","content":"x"}`,
		}},
	})
	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("write_file should be workflow-policy rejected, got %+v", result)
	}
	view, ok := handler.traceService.GetTrace(loopCtx.RunID)
	if !ok {
		t.Fatalf("missing trace for run %q", loopCtx.RunID)
	}
	joined := ""
	for _, event := range view.Events {
		joined += event.Kind + " " + event.Summary + "\n"
	}
	for _, want := range []string{"request.accepted", "tool.policy_denied", "policy_owner=" + userID, "session_key=" + loopCtx.Runtime.Conversation.SessionKey} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q in:\n%s", want, joined)
		}
	}
}

func TestRuntimeContextWithEmptyPolicyOwnerDoesNotUseLegacyFallback(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "runtime-desktop-doc-only-owner"
	if _, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.lastUserID = desktopID

	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Runtime = RuntimeContext{
		RequestID: "req-explicit-empty-owner",
		Source:    RuntimeSourceRef{Channel: "im", Provider: "weixin"},
		Actor:     RuntimeActorRef{ActorID: "main-ai", ActorType: "main_ai"},
		Conversation: RuntimeConversationRef{
			ConversationID: "weixin-user",
			SessionKey:     "im:weixin:weixin-user:main-ai",
		},
		PolicyOwnerID: "",
		LockKey:       "im:weixin:weixin-user:main-ai:main-ai",
	}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context:          loopCtx,
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.txt","content":"x"}`,
		}},
	})
	if result.FailureKind == toolFailurePolicyRejected && strings.Contains(result.Text, "workflow tool policy") {
		t.Fatalf("explicit runtime owner must not fall back to desktop workflow policy, got %+v", result)
	}
}

func TestRuntimePolicyOwnerWithoutRequestIDStillDrivesWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	ownerID := "remote:runtime-owner-without-request-id"
	if _, err := handler.app.workflowEngine.StartWorkflow(ownerID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(ownerID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Runtime = RuntimeContext{PolicyOwnerID: ownerID}
	// write_file remains blocked in doc-only phases (bash is allowed for doc parsing).
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:  "weixin-user",
		Context: loopCtx,
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.md","content":"x"}`,
		}},
	})
	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("runtime policy owner without request id should drive workflow policy, got %+v", result)
	}
}

func TestPrepareIMLoopContextPreservesRuntimeOwnerWithoutRequestID(t *testing.T) {
	handler := &IMMessageHandler{}
	provided := NewLoopContext("chat", 1, nil)
	provided.Runtime = RuntimeContext{PolicyOwnerID: "remote:provided-owner"}

	got := handler.prepareIMLoopContext(provided, IMUserMessage{UserID: desktopUserID, Platform: desktopPlatform, Text: "hi"}, nil, false, false)
	if got.Runtime.RequestID == "" {
		t.Fatal("prepareIMLoopContext should create request id")
	}
	if got.Runtime.PolicyOwnerID != "remote:provided-owner" || got.Runtime.WorkflowOwnerID != "remote:provided-owner" {
		t.Fatalf("runtime owner = (%q,%q), want provided owner", got.Runtime.PolicyOwnerID, got.Runtime.WorkflowOwnerID)
	}
	if got.Runtime.Source.Channel != "desktop" || got.Runtime.Conversation.SessionKey == "" {
		t.Fatalf("runtime source/session not normalized: %+v", got.Runtime)
	}
}

func TestRunSkillUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := desktopUserID
	if _, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.lastUserID = desktopID
	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Runtime = RuntimeContext{
		RequestID:     "req-skill-owner",
		PolicyOwnerID: "remote:mobile",
		Conversation:  RuntimeConversationRef{SessionKey: "im:weixin:mobile-user:main-ai"},
	}
	handler.currentLoopCtx = loopCtx

	text := handler.toolRunSkill(context.Background(), map[string]interface{}{"name": "missing-skill"}, nil)
	if strings.Contains(text, "not allowed by the current workflow tool policy") {
		t.Fatalf("run_skill should use runtime owner, not desktop workflow policy: %s", text)
	}
}

func TestRunSkillWithoutRuntimeOwnerDoesNotBypassWorkflowOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.currentLoopCtx = &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-run-skill-owner"}}

	text := handler.toolRunSkill(context.Background(), map[string]interface{}{"name": "missing-skill"}, nil)
	if !strings.Contains(text, "runtime owner is missing") {
		t.Fatalf("run_skill without runtime owner should fail closed, got %q", text)
	}
}

func TestDelegateCodingWorkflowWithoutRuntimeOwnerDoesNotFallbackToDesktop(t *testing.T) {
	handler := &IMMessageHandler{currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-delegate-owner"}}}

	text := handler.toolDelegateTask(map[string]interface{}{
		"agent":   "coding_workflow",
		"request": "change code",
	})
	if !strings.Contains(text, "runtime owner is missing") {
		t.Fatalf("delegate_task(coding_workflow) without runtime owner should fail closed, got %q", text)
	}
}

func TestAutoSkillInstallUsesExplicitPolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := desktopUserID
	if _, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	result := handler.installAndExecuteSkill(context.Background(), &SkillSearchResult{
		ID:     "https://github.com/acme/demo-skill",
		Name:   "demo-skill",
		Status: skillSearchSourceGitHub,
	}, "demo", "weixin", desktopID, "remote:mobile", func(string) {})
	if strings.Contains(result.Text, "not allowed by the current workflow tool policy") {
		t.Fatalf("auto skill install should use explicit policy owner, not desktop workflow policy: %s", result.Text)
	}
}

func TestSystemPromptRuntimeUserIDPrefersLoopOwner(t *testing.T) {
	handler := &IMMessageHandler{lastUserID: desktopUserID}
	loopCtx := &LoopContext{UserID: "weixin-user", Runtime: RuntimeContext{RequestID: "req-prompt", PolicyOwnerID: "remote:mobile"}}
	if got := handler.promptRuntimeUserID(loopCtx); got != "remote:mobile" {
		t.Fatalf("prompt runtime user = %q, want runtime owner", got)
	}
}

func TestSystemPromptRuntimeUserIDEmptyEnvelopeDoesNotFallbackToDesktop(t *testing.T) {
	handler := &IMMessageHandler{lastUserID: desktopUserID}
	loopCtx := &LoopContext{UserID: "weixin-user", Runtime: RuntimeContext{RequestID: "req-empty-prompt-owner"}}
	if got := handler.promptRuntimeUserID(loopCtx); got != "" {
		t.Fatalf("prompt runtime user = %q, want isolated empty owner", got)
	}
	handler.currentLoopCtx = loopCtx
	if got := handler.promptRuntimeUserID(nil); got != "" {
		t.Fatalf("global prompt runtime user = %q, want isolated empty owner", got)
	}
}

func TestCompressContextUsesRuntimeOwner(t *testing.T) {
	handler := &IMMessageHandler{
		lastUserID:     desktopUserID,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-compress", PolicyOwnerID: "remote:mobile"}},
	}

	text := handler.toolCompressContext(map[string]interface{}{"summary": "mobile summary"})
	if strings.Contains(text, "缺少 owner") {
		t.Fatalf("compress_context rejected runtime owner: %s", text)
	}
	if _, ok := handler.pendingContextCompression.Load("remote:mobile"); !ok {
		t.Fatal("compress_context should store pending compression under runtime owner")
	}
	if _, ok := handler.pendingContextCompression.Load(desktopUserID); ok {
		t.Fatal("compress_context must not store pending compression under lastUserID")
	}
}

func TestToolRuntimePolicyOwnerArgOverridesGlobalLoop(t *testing.T) {
	handler := &IMMessageHandler{
		lastUserID:     desktopUserID,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: desktopUserID}},
	}

	text := handler.toolCompressContext(map[string]interface{}{
		"summary":                        "mobile summary",
		registeredToolPolicyOwnerIDField: "remote:mobile",
	})
	if strings.Contains(text, "缺少 owner") {
		t.Fatalf("compress_context rejected explicit tool owner: %s", text)
	}
	if _, ok := handler.pendingContextCompression.Load("remote:mobile"); !ok {
		t.Fatal("tool hidden runtime owner should override global loop owner")
	}
	if _, ok := handler.pendingContextCompression.Load(desktopUserID); ok {
		t.Fatal("tool hidden runtime owner must not store under global desktop owner")
	}
}

func TestToolRuntimePolicyOwnerArgDoesNotLeakToGenericTool(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	seenOwnerArg := false
	if err := handler.registry.Register(RegisteredTool{
		Name: "generic_capture_tool",
		Handler: func(args map[string]interface{}) string {
			_, seenOwnerArg = args[registeredToolPolicyOwnerIDField]
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result := handler.executeToolDetailedWithPolicyUserText("remote:mobile", "generic_capture_tool", `{}`, "", nil)
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenOwnerArg {
		t.Fatal("runtime owner hidden arg must not leak into generic/external tool args")
	}
}

func TestRegisteredToolRuntimeOwnerMetadataCarriesOwner(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	var seenOwner string
	if err := handler.registry.Register(RegisteredTool{
		Name:                  "custom_owner_tool",
		RuntimePolicyOwnerArg: true,
		Handler: func(args map[string]interface{}) string {
			seenOwner = consumeRuntimePolicyOwnerIDFromToolArgs(args)
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result := handler.executeToolDetailedWithRuntimeState("remote:mobile", true, "", "custom_owner_tool", `{}`, "", nil)
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenOwner != "remote:mobile" {
		t.Fatalf("metadata owner = %q, want remote:mobile", seenOwner)
	}
}

func TestRegisteredToolRuntimePlatformMetadataCarriesPlatform(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	var seenPlatform string
	if err := handler.registry.Register(RegisteredTool{
		Name:               "custom_platform_tool",
		RuntimePlatformArg: true,
		Handler: func(args map[string]interface{}) string {
			seenPlatform = consumeRuntimePlatformFromToolArgs(args)
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result := handler.executeToolDetailedWithRuntimeState("", false, "weixin", "custom_platform_tool", `{}`, "", nil)
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenPlatform != "weixin" {
		t.Fatalf("metadata platform = %q, want weixin", seenPlatform)
	}
}

func TestBuiltinIMManagementToolsCarryRuntimePlatform(t *testing.T) {
	handler := &IMMessageHandler{app: &App{}, registry: NewToolRegistry()}
	registerBuiltinTools(handler.registry, handler)
	for _, name := range []string{"manage_schedule", "im_message"} {
		registered, ok := handler.registry.Get(name)
		if !ok || registered == nil {
			t.Fatalf("builtin %q is not registered", name)
		}
		if !registered.RuntimePlatformArg {
			t.Fatalf("builtin %q must accept runtime platform metadata", name)
		}
	}
}

func TestOwnerAwareToolEmptyRuntimeOwnerFailsClosedBeforeHandler(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	called := false
	if err := handler.registry.Register(RegisteredTool{
		Name: "memory",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result := handler.executeToolDetailedWithRuntimeState("", true, "", "memory", `{}`, "", nil)
	if called {
		t.Fatal("owner-aware handler should not run when runtime owner is explicitly empty")
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected || !strings.Contains(result.Text, "runtime owner is missing") {
		t.Fatalf("empty runtime owner result = %+v, want fail closed", result)
	}
}

func TestRuntimeOwnerAwareToolListIncludesMergedPrimaryTools(t *testing.T) {
	for _, name := range []string{
		"bash",
		"manage_skill",
		"run_skill",
		"install_skill_hub",
		"search_and_install_skill",
		"memory",
		"compress_context",
		"delegate_task",
		"agent_status",
		"async_wait",
		"set_max_iterations",
		"group_discussion",
		"screenshot",
		"call_mcp_tool",
		"browser",
		"browser_session_start",
		"browser_connect",
		"tts",
	} {
		if !toolAcceptsRuntimePolicyOwnerArg(name) {
			t.Fatalf("%s must accept hidden runtime owner args", name)
		}
	}
}

func TestRuntimePlatformAwareToolListIncludesPlatformTools(t *testing.T) {
	for _, name := range []string{
		"manage_skill",
		"install_skill_hub",
		"search_and_install_skill",
		"screenshot",
		"tts",
	} {
		if !toolAcceptsRuntimePlatformArg(name) {
			t.Fatalf("%s must accept hidden runtime platform args", name)
		}
	}
}

func TestToolRegistryPopulatesDefaultRuntimeMetadata(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{Name: "browser"}); err != nil {
		t.Fatalf("Register browser failed: %v", err)
	}
	if err := registry.Register(RegisteredTool{Name: "tts"}); err != nil {
		t.Fatalf("Register tts failed: %v", err)
	}
	browserTool, ok := registry.Get("browser")
	if !ok || browserTool == nil || !browserTool.RuntimePolicyOwnerArg {
		t.Fatalf("browser runtime owner metadata = %#v", browserTool)
	}
	ttsTool, ok := registry.Get("tts")
	if !ok || ttsTool == nil || !ttsTool.RuntimePolicyOwnerArg || !ttsTool.RuntimePlatformArg {
		t.Fatalf("tts runtime metadata = %#v", ttsTool)
	}
}

func TestRegisteredToolRuntimeMetadataIsInternalOnly(t *testing.T) {
	data, err := json.Marshal(RegisteredTool{
		Name:                  "custom_owner_tool",
		RuntimePolicyOwnerArg: true,
		RuntimePlatformArg:    true,
	})
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	raw := string(data)
	if strings.Contains(raw, "runtime_policy_owner") || strings.Contains(raw, "runtime_platform") {
		t.Fatalf("runtime metadata leaked into JSON: %s", raw)
	}
}

func TestAgentLoopToolCarriesRuntimePlatformToOwnerAwareInstallTool(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	var seenOwner, seenPlatform string
	if err := handler.registry.Register(RegisteredTool{
		Name: "install_skill_hub",
		Handler: func(args map[string]interface{}) string {
			seenPlatform = consumeRuntimePlatformFromToolArgs(args)
			seenOwner = consumeRuntimePolicyOwnerIDFromToolArgs(args)
			if _, ok := args[registeredToolRuntimePlatformField]; ok {
				t.Fatal("runtime platform field should be consumed before downstream install execution")
			}
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Platform = "weixin"
	loopCtx.Runtime = RuntimeContext{RequestID: "req-install-platform", PolicyOwnerID: "remote:mobile"}
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		Context: loopCtx,
		ToolCall: llm.ToolCall{ID: "call_install", Function: llm.ToolCallFunction{
			Name:      "install_skill_hub",
			Arguments: `{}`,
		}},
	})
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenOwner != "remote:mobile" || seenPlatform != "weixin" {
		t.Fatalf("runtime fields = owner %q platform %q, want remote:mobile/weixin", seenOwner, seenPlatform)
	}
}

func TestSearchAndInstallSkillCarriesRuntimePlatform(t *testing.T) {
	handler := &IMMessageHandler{}
	var seenOwner, seenPlatform string
	handler.skillSearchInstallHandler = func(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
		seenPlatform = consumeRuntimePlatformFromToolArgs(args)
		seenOwner = consumeRuntimePolicyOwnerIDFromToolArgs(args)
		if _, ok := args[registeredToolRuntimePlatformField]; ok {
			t.Fatal("runtime platform field should be consumed before downstream search/install execution")
		}
		return searchAndInstallSkillResult{Text: "ok", Success: true}
	}

	result := handler.executeToolDetailedWithRuntime("remote:mobile", "weixin", "search_and_install_skill", `{"query":"demo"}`, "", nil)
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenOwner != "remote:mobile" || seenPlatform != "weixin" {
		t.Fatalf("runtime fields = owner %q platform %q, want remote:mobile/weixin", seenOwner, seenPlatform)
	}
}

func TestManageSkillCarriesRuntimeOwnerAndPlatform(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	var seenOwner, seenPlatform string
	if err := handler.registry.Register(RegisteredTool{
		Name: "manage_skill",
		Handler: func(args map[string]interface{}) string {
			seenOwner = consumeRuntimePolicyOwnerIDFromToolArgs(args)
			seenPlatform = consumeRuntimePlatformFromToolArgs(args)
			if _, ok := args[registeredToolPolicyOwnerIDField]; ok {
				t.Fatal("runtime owner field should be consumed by manage_skill handler")
			}
			if _, ok := args[registeredToolRuntimePlatformField]; ok {
				t.Fatal("runtime platform field should be consumed by manage_skill handler")
			}
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	result := handler.executeToolDetailedWithRuntime("remote:mobile", "weixin", "manage_skill", `{"action":"run","name":"demo"}`, "", nil)
	if result.Text != "ok" {
		t.Fatalf("tool result = %+v, want ok", result)
	}
	if seenOwner != "remote:mobile" || seenPlatform != "weixin" {
		t.Fatalf("runtime fields = owner %q platform %q, want remote:mobile/weixin", seenOwner, seenPlatform)
	}
}

func TestManageSkillEmptyRuntimeOwnerFailsClosedAtMergedEntry(t *testing.T) {
	handler := &IMMessageHandler{}
	got := handler.toolManageSkill(context.Background(), map[string]interface{}{
		"action":                         "list",
		registeredToolPolicyOwnerIDField: "",
	}, nil)
	if !strings.Contains(got, "runtime owner is missing") {
		t.Fatalf("manage_skill empty runtime owner = %q, want fail closed", got)
	}
}

func TestBonusRoundToolExecutionUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := desktopUserID
	if _, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.lastUserID = desktopID
	handler.registry = NewToolRegistry()
	if err := handler.registry.Register(RegisteredTool{
		Name: "generic_capture_tool",
		Handler: func(args map[string]interface{}) string {
			return "ok"
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	loopCtx := NewLoopContext("chat", 1, nil)
	loopCtx.Runtime = RuntimeContext{RequestID: "req-bonus", PolicyOwnerID: "remote:mobile"}
	result := handler.executeBonusRoundTool(llm.ToolCall{Function: llm.ToolCallFunction{
		Name:      "generic_capture_tool",
		Arguments: `{}`,
	}}, nil, nil, nil, "weixin-user", loopCtx)
	if result.Text != "ok" {
		t.Fatalf("bonus-round tool should use runtime owner, not desktop workflow policy, got %+v", result)
	}
}

func TestMemoryContextHintUsesExplicitOwner(t *testing.T) {
	handler := &IMMessageHandler{memory: agent.NewConversationMemory()}
	handler.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: "desktop secret context"}})
	handler.memory.Save("remote:mobile", []agent.ConversationEntry{{Role: "user", Content: "mobile selected context"}})

	hint := handler.buildMemoryContextHintForUser("remote:mobile")
	if !strings.Contains(hint, "mobile selected context") {
		t.Fatalf("memory context hint = %q, want explicit owner history", hint)
	}
	if strings.Contains(hint, "desktop secret context") {
		t.Fatalf("memory context hint leaked desktop history: %q", hint)
	}
}

func TestMemoryContextHintWithoutRuntimeOwnerDoesNotFallbackToDesktop(t *testing.T) {
	handler := &IMMessageHandler{
		memory:         agent.NewConversationMemory(),
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-memory-owner"}},
	}
	handler.memory.Save(desktopUserID, []agent.ConversationEntry{{Role: "user", Content: "desktop secret context"}})

	if hint := handler.buildMemoryContextHint(); hint != "" {
		t.Fatalf("memory context hint without runtime owner should not fall back to desktop, got %q", hint)
	}
}

func TestMemoryContextHintWithoutHiddenOwnerDoesNotInheritCurrentRuntimeOwner(t *testing.T) {
	handler := &IMMessageHandler{
		memory: agent.NewConversationMemory(),
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{
			RequestID:     "req-remote-memory",
			PolicyOwnerID: "remote:mobile",
		}},
	}
	handler.memory.Save("remote:mobile", []agent.ConversationEntry{{Role: "user", Content: "remote secret context"}})

	if hint := handler.buildMemoryContextHint(); hint != "" {
		t.Fatalf("memory context hint without hidden owner should not inherit current runtime owner, got %q", hint)
	}
}

func TestToolMemoryWithoutRuntimeOwnerDoesNotFallbackToDesktop(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(store.Stop)
	handler := &IMMessageHandler{
		memoryStore:    store,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-empty-memory-owner"}},
	}

	got := handler.toolMemory(map[string]interface{}{"action": "save", "content": "desktop leak"})
	if !strings.Contains(got, "owner is missing") {
		t.Fatalf("memory tool should reject isolated runtime without owner, got %q", got)
	}
	if entries := store.List("", ""); len(entries) != 0 {
		t.Fatalf("memory tool wrote entries without owner: %#v", entries)
	}
}

func TestToolMemoryWithoutHiddenOwnerDoesNotInheritCurrentRuntimeOwner(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(store.Stop)
	handler := &IMMessageHandler{
		memoryStore: store,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{
			RequestID:     "req-remote-memory",
			PolicyOwnerID: "remote:mobile",
		}},
	}

	got := handler.toolMemory(map[string]interface{}{"action": "save", "content": "remote leak"})
	if !strings.Contains(got, "owner is missing") {
		t.Fatalf("memory tool should reject missing hidden owner instead of inheriting current runtime, got %q", got)
	}
	if entries := store.List("", ""); len(entries) != 0 {
		t.Fatalf("memory tool wrote entries without hidden owner: %#v", entries)
	}
}

func TestToolMemoryEmptyHiddenRuntimeOwnerDoesNotFallbackToDesktop(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(store.Stop)
	handler := &IMMessageHandler{memoryStore: store}

	got := handler.toolMemory(map[string]interface{}{
		"action":                         "save",
		"content":                        "desktop leak",
		registeredToolPolicyOwnerIDField: "",
	})
	if !strings.Contains(got, "owner is missing") {
		t.Fatalf("memory tool should reject empty hidden runtime owner, got %q", got)
	}
	if entries := store.List("", ""); len(entries) != 0 {
		t.Fatalf("memory tool wrote entries with empty hidden owner: %#v", entries)
	}
}

func TestSituationReportWithoutRuntimeOwnerDoesNotExposeDesktopArtifacts(t *testing.T) {
	store, err := corememory.NewStore(filepath.Join(t.TempDir(), "memories.json"))
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(store.Stop)
	if err := store.Save(corememory.Entry{
		Title:     "Desktop Artifact",
		Content:   "desktop artifact content",
		Category:  corememory.CategoryTaskArtifact,
		OwnerID:   desktopUserID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	handler := &IMMessageHandler{memoryStore: store}

	if report := handler.buildSituationReport(""); strings.Contains(report, "Desktop Artifact") {
		t.Fatalf("situation report without owner leaked desktop artifact: %q", report)
	}
}
