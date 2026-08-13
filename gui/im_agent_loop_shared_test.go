package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

// shouldUseSharedAgentLoopLive mirrors shouldUseSharedAgentLoop but uses the
// production mode resolver so unit tests are not blocked by the testing.Testing()
// gate (which keeps package RunAgentLoop tests on the legacy path).
func shouldUseSharedAgentLoopLive(h *IMMessageHandler, ctx *LoopContext, userID string, attachments []MessageAttachment) bool {
	mode := resolveSharedAgentLoopModeLive(h)
	eligible, reason := h.sharedAgentLoopEligibility(ctx, attachments)
	switch mode {
	case sharedAgentLoopOff:
		return false
	case sharedAgentLoopShadow:
		if eligible {
			recordSharedLoopSkip("shadow", reason)
		}
		return false
	case sharedAgentLoopOn:
		if !eligible {
			recordSharedLoopSkip("ineligible", reason)
			return false
		}
		if !sharedLoopCanaryAllowsFor(h, userID) {
			recordSharedLoopSkip("canary", "canary")
			return false
		}
		return true
	default:
		return false
	}
}

func TestSharedProjectToolResultUsesRuntimePolicyOwner(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	ctx := NewLoopContext("chat", 3, nil)
	ctx.Runtime.PolicyOwnerID = "remote:mobile"
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, loopCtx: ctx, userID: "desktop-user"}
	raw := strings.Repeat("raw-result\n", 2000)
	projected := cb.ProjectToolResult("bash", agent.ToolExecutionResult{Result: raw, Outcome: agent.ToolExecutionOutcomeOK})
	if !strings.Contains(projected, "[tool_result_handle]") {
		t.Fatalf("projection missing handle: %q", projected[max(0, len(projected)-300):])
	}
	if strings.Contains(projected, maclawpath.ToolResultsDir()) || strings.Contains(projected, "path:") {
		t.Fatalf("projection exposed local storage path: %q", projected[max(0, len(projected)-500):])
	}
	entries, err := os.ReadDir(filepath.Join(maclawpath.ToolResultsDir(), toolresult.SessionDirectoryName("remote:mobile")))
	if err != nil || len(entries) == 0 {
		t.Fatalf("projection was not stored under runtime policy owner: entries=%d err=%v", len(entries), err)
	}
}

func TestGUIAndCoreToolResultProjectionStayEquivalent(t *testing.T) {
	oldBase := maclawpath.BaseDir()
	maclawpath.SetBaseDir(t.TempDir())
	t.Cleanup(func() { maclawpath.SetBaseDir(oldBase) })
	raw := strings.Repeat("build output\n", 2000)
	guiProjection := truncateToolResultForToolWithSession("bash", "owner-a", raw)
	coreProjection := agent.TruncateToolResultForToolWithSession("bash", "owner-a", raw)
	previewOnly := func(s string) string {
		if i := strings.Index(s, "\n\n[tool_result_handle]\n"); i >= 0 {
			return s[:i]
		}
		return s
	}
	if previewOnly(guiProjection) != previewOnly(coreProjection) {
		t.Fatal("GUI and core projections diverged before handle metadata")
	}
	for label, projection := range map[string]string{"gui": guiProjection, "core": coreProjection} {
		if !strings.Contains(projection, "[tool_result_handle]") || !strings.Contains(projection, "read_tool_result") {
			t.Fatalf("%s projection lost read-back metadata: %q", label, projection[max(0, len(projection)-300):])
		}
		if len(projection) > agent.MaxToolResultLen {
			t.Fatalf("%s projection exceeded budget: %d", label, len(projection))
		}
	}
}

func TestShouldUseSharedAgentLoop_RequiresFlag(t *testing.T) {
	// Package tests force legacy via resolveSharedAgentLoopMode.
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if h.shouldUseSharedAgentLoop(ctx, "u1", nil) {
		t.Fatal("package tests must keep shared loop off by default")
	}
	// Production defaults still enable for new installs when env is unset.
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP")
	_ = os.Unsetenv("MACLAW_SHARED_AGENT_LOOP_SHADOW")
	if corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopOn {
			t.Fatal("expected production default on when no env/app config")
		}
	}
}

func TestShouldUseSharedAgentLoop_EnvEnable(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("expected true with env flag")
	}
}

func TestShouldUseSharedAgentLoop_Phase3AllowsBackground(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	bg := &LoopContext{Kind: LoopKindBackground}
	if !shouldUseSharedAgentLoopLive(h, bg, "u1", nil) {
		t.Fatal("background should use shared loop when enabled")
	}
	ok, reason := h.sharedAgentLoopEligibility(bg, nil)
	if !ok || reason != "background" {
		t.Fatalf("ok=%v reason=%q", ok, reason)
	}
}

func TestShouldUseSharedAgentLoop_Phase2AllowsLightAttachments(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if !shouldUseSharedAgentLoopLive(h, chat, "u1", []MessageAttachment{{Type: "image", FileName: "a.png", MimeType: "image/png", Size: 1024}}) {
		t.Fatal("light image attachments should be allowed")
	}
}

func TestShouldUseSharedAgentLoop_RejectsWorkflowByDefault(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow must not use shared loop without pilot env")
	}
	doc := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("workflow doc phase must never use shared loop")
	}
}

func TestShouldUseSharedAgentLoop_WorkflowPilot(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "1")
	h := &IMMessageHandler{}
	wf := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true}
	if !shouldUseSharedAgentLoopLive(h, wf, "u1", nil) {
		t.Fatal("workflow pilot should allow non-doc workflow")
	}
	// Doc phase still blocked.
	doc := &LoopContext{Kind: LoopKindChat, WorkflowAgentLoop: true, WorkflowDocPhase: true}
	if shouldUseSharedAgentLoopLive(h, doc, "u1", nil) {
		t.Fatal("doc phase must stay legacy even with pilot")
	}
}

func TestShouldUseSharedAgentLoop_ShadowNeverDiverts(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	h := &IMMessageHandler{}
	chat := &LoopContext{Kind: LoopKindChat}
	if shouldUseSharedAgentLoopLive(h, chat, "u1", nil) {
		t.Fatal("shadow mode must keep legacy path")
	}
	if resolveSharedAgentLoopModeLive(h) != sharedAgentLoopShadow {
		t.Fatal("mode should be shadow")
	}
	ok, _ := h.sharedAgentLoopEligibility(chat, nil)
	if !ok {
		t.Fatal("chat should be eligible even in shadow")
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentZero(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "0")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	before := processSharedLoopStats.skipCanary.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "any-user", nil) {
		t.Fatal("percent=0 must never divert")
	}
	if processSharedLoopStats.skipCanary.Load() <= before {
		t.Fatal("expected canary skip counter")
	}
}

func TestShouldUseSharedAgentLoop_IneligibleRecordsSkip(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "100")
	h := &IMMessageHandler{}
	// Workflow doc phase is never eligible.
	ctx := &LoopContext{Kind: LoopKindChat, WorkflowDocPhase: true}
	before := processSharedLoopStats.skipIneligible.Load()
	if shouldUseSharedAgentLoopLive(h, ctx, "u1", nil) {
		t.Fatal("doc phase must not use shared")
	}
	if processSharedLoopStats.skipIneligible.Load() <= before {
		t.Fatal("expected ineligible skip counter")
	}
	st := (&App{}).GetSharedAgentLoopStatus()
	if !strings.Contains(st.LastSkipReason, "workflow doc") && !strings.Contains(st.LastSkipReason, "ineligible") {
		// last may be ineligible:workflow doc phase
		if st.LastSkipReason == "" {
			t.Fatalf("last skip empty")
		}
	}
}

func TestShouldUseSharedAgentLoop_CanaryPercentSticky(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "1")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	h := &IMMessageHandler{}
	ctx := &LoopContext{Kind: LoopKindChat}
	// Stickiness: same user always same decision.
	a1 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	a2 := shouldUseSharedAgentLoopLive(h, ctx, "sticky-user-xyz", nil)
	if a1 != a2 {
		t.Fatal("canary must be sticky per user")
	}
	// Across many users some should pass and some fail at 50%.
	pass, fail := 0, 0
	for i := 0; i < 200; i++ {
		uid := "user-" + strings.Repeat("x", i%17) + string(rune('a'+i%26)) + string(rune('0'+i%10))
		if sharedLoopCanaryAllows(uid) {
			pass++
		} else {
			fail++
		}
	}
	if pass == 0 || fail == 0 {
		t.Fatalf("expected mix at 50%% canary, pass=%d fail=%d", pass, fail)
	}
}

func TestSharedLoopPercent_Bounds(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	if sharedLoopPercent() != 100 {
		t.Fatal("default 100")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "30")
	if sharedLoopPercent() != 30 {
		t.Fatal("30")
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "999")
	if sharedLoopPercent() != 100 {
		t.Fatal("cap 100")
	}
}

func TestSharedAgentLoopEnabled_EnvOff(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "0")
	if resolveSharedAgentLoopModeLive(&IMMessageHandler{}) != sharedAgentLoopOff {
		t.Fatal("env 0 should disable")
	}
	if sharedAgentLoopEnabled(&IMMessageHandler{}) {
		t.Fatal("package shouldUse path must stay off under tests")
	}
}

func TestAppConfigDefaults_SharedAgentLoopEnabled(t *testing.T) {
	if !corelib.AppConfigDefaults().SharedAgentLoopEnabled {
		t.Fatal("new installs should default SharedAgentLoopEnabled=true")
	}
}

func TestSharedAgentLoopCallbacks_RouteTurn(t *testing.T) {
	cb := &sharedAgentLoopCallbacks{
		llmCfg: corelib.MaclawLLMConfig{Model: "m1", ProviderName: "p1"},
		route:  modelRouteDecision{Task: "fast", Source: "aux", Model: "m1", Provider: "p1", Reason: "short"},
	}
	cfg, d, ok := cb.RouteTurn("hi")
	if !ok || cfg.Model != "m1" || d.Source != "aux" || !strings.Contains(d.Reason, "shared") {
		t.Fatalf("cfg=%+v d=%+v ok=%v", cfg, d, ok)
	}
}

func TestSharedAgentLoopCallbacks_UpgradeLightPromptKeepsCurrentAttachmentOutOfComputerUse(t *testing.T) {
	resetComputerUseSessionForTest(t)
	defs := []map[string]interface{}{
		toolDef("read_file", "read local file", nil, nil),
		toolDef("computer_observe", "observe desktop", nil, nil),
		toolDef("computer_click", "click desktop", nil, nil),
		toolDef("gui_click", "legacy click", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := NewLoopContext("shared-attachment-upgrade", 3, nil)
	ctx.Runtime.Execution = ExecutionProfile{
		Layer:         string(executionLayerLight),
		PromptProfile: "light",
	}
	// Simulate a desktop task immediately followed by an uploaded attachment.
	// The raw text has no staged attachment marker yet, so the callback field is
	// the signal that must survive the profile upgrade.
	markComputerUseSessionActive()
	cb := &sharedAgentLoopCallbacks{
		handler:          h,
		loopCtx:          ctx,
		userID:           "desktop-user:attachment-upgrade",
		userText:         "请阅读这个压缩包",
		hasLocalFileWork: true,
	}
	if !cb.UpgradeLightPromptToFull("need local file reader") {
		t.Fatal("light profile should upgrade to full")
	}
	names := toolNameSetForWorkflowFilterTest(cb.tools)
	if names["computer_observe"] || names["computer_click"] {
		t.Fatalf("attachment upgrade must not reintroduce Computer Use: %#v", names)
	}
	if !names["read_file"] || names["gui_click"] {
		t.Fatalf("attachment upgrade must preserve file tools but remove legacy desktop tools: %#v", names)
	}
	if computerUseSessionActive() {
		t.Fatal("attachment upgrade should clear the stale Computer Use session")
	}
}

func TestLocalFileWorkFenceSurvivesToolRecoveryAndAugmentation(t *testing.T) {
	defs := []map[string]interface{}{
		toolDef("read_file", "read local file", nil, nil),
		toolDef("computer_observe", "observe desktop", nil, nil),
		toolDef("computer_click", "click desktop", nil, nil),
	}
	h := &IMMessageHandler{toolDefGen: NewToolDefinitionGenerator(nil, defs)}
	ctx := NewLoopContext("local-file-recovery", 3, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true

	restored, _, _ := h.restoreToolsAfterSkillRecover("desktop-user:local-file-recovery", ctx, h.getTools(), agentLoopPhase{})
	restoredNames := toolNameSetForWorkflowFilterTest(restored)
	if restoredNames["computer_observe"] || restoredNames["computer_click"] || !restoredNames["read_file"] {
		t.Fatalf("recovery bypassed local-file Computer Use fence: %#v", restoredNames)
	}

	augmented, _ := h.finalizeInjectionAugmentedTools(ctx, "desktop-user:local-file-recovery", h.getTools())
	augmentedNames := toolNameSetForWorkflowFilterTest(augmented)
	if augmentedNames["computer_observe"] || augmentedNames["computer_click"] || !augmentedNames["read_file"] {
		t.Fatalf("augmentation bypassed local-file Computer Use fence: %#v", augmentedNames)
	}
}

func TestSharedAgentLoopCallbacks_RejectsStaleComputerUseCallForLocalFileWork(t *testing.T) {
	called := false
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name: "computer_click",
		Handler: func(args map[string]interface{}) string {
			called = true
			return "desktop clicked"
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx := NewLoopContext("shared-local-file-execution", 1, nil)
	ctx.ComputerUseBlockedForLocalFileWork = true
	cb := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{registry: registry},
		loopCtx:  ctx,
		userText: "请阅读附件",
	}
	if got := cb.ExecuteTool("computer_click", `{}`); !strings.Contains(got, "local attachment") {
		t.Fatalf("shared stale Computer Use call = %q, want local attachment rejection", got)
	}
	if called {
		t.Fatal("shared callback must not invoke Computer Use handler for local-file work")
	}
}

func TestSharedAgentLoopCallbacks_TransformConversationInjectsLiveSteer(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:shared-steer"
	h.accumulateInjection(userID, buildGuideLaunchInjection("switch to SQLite"))
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID}
	conversation := []interface{}{map[string]string{"role": "user", "content": "build a database"}}

	next := cb.TransformConversation(conversation)
	if len(next) != 2 {
		t.Fatalf("conversation len = %d, want 2", len(next))
	}
	msg, ok := next[1].(map[string]string)
	if !ok || msg["role"] != "user" || !strings.Contains(msg["content"], "switch to SQLite") {
		t.Fatalf("unexpected shared steer injection: %#v", next[1])
	}
	if _, ok := h.pendingInjection.Load(userID); ok {
		t.Fatal("shared loop transform should consume pending steer once")
	}
}

func TestSharedLoopDisplayReasoningUsesAllAssistantSummaries(t *testing.T) {
	delta := []agent.ConversationEntry{
		{Role: "user", Content: "weather"},
		{Role: "assistant", Content: "checking", ReasoningContent: "First summary."},
		{Role: "tool", Content: "weather result"},
		{Role: "assistant", Content: "answer", ReasoningContent: "Final display-safe summary."},
	}
	result := agent.LoopResult{HistoryDelta: delta, Reasoning: "First summary.\n\nFinal display-safe summary."}
	if got, want := sharedLoopDisplayReasoning(result), "First summary.\n\nFinal display-safe summary."; got != want {
		t.Fatalf("sharedLoopDisplayReasoning() = %q, want %q", got, want)
	}
}

func TestSharedLoopDisplayReasoningSkipsEmptyAssistantTurns(t *testing.T) {
	delta := []agent.ConversationEntry{
		{Role: "assistant", Content: "first", ReasoningContent: "usable summary"},
		{Role: "assistant", Content: "final", ReasoningContent: "  "},
	}
	if got, want := sharedLoopDisplayReasoning(agent.LoopResult{HistoryDelta: delta}), "usable summary"; got != want {
		t.Fatalf("sharedLoopDisplayReasoning() = %q, want %q", got, want)
	}
}

func TestSharedAgentLoopCallbacks_DetectsReplanRevision(t *testing.T) {
	ctx := NewLoopContext("shared-replan", 3, nil)
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userID: "desktop-user:shared-replan", loopCtx: ctx}
	cb.TransformConversation(nil)
	_, finish, err := cb.LLMRequestContext(0)
	if err != nil {
		t.Fatalf("LLMRequestContext: %v", err)
	}
	defer finish(nil)
	if cb.LLMReplanRequested() {
		t.Fatal("unexpected replan before steering")
	}
	ctx.RequestReplan()
	if !cb.LLMReplanRequested() {
		t.Fatal("shared callback did not observe live-steer replan")
	}
}

func TestSharedAgentLoopCallbacks_DetectsReplanBetweenTransformAndRequest(t *testing.T) {
	ctx := NewLoopContext("shared-replan-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{}, userID: "desktop-user:shared-replan-race", loopCtx: ctx}

	cb.TransformConversation(nil)
	ctx.RequestReplan() // steering lands after transform, before HTTP setup
	_, finish, err := cb.LLMRequestContext(0)
	if err != nil {
		t.Fatalf("LLMRequestContext: %v", err)
	}
	defer finish(nil)
	if !cb.LLMReplanRequested() {
		t.Fatal("replan in transform/request race window was lost")
	}
}

func TestSharedAgentLoopCallbacks_TransformConsumesExistingReplanRevision(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:shared-existing-replan"
	ctx := NewLoopContext("shared-existing-replan", 3, nil)
	h.accumulateInjection(userID, buildGuideLaunchInjection("prefer the existing API"))
	ctx.RequestReplan()
	cb := &sharedAgentLoopCallbacks{handler: h, userID: userID, loopCtx: ctx}

	next := cb.TransformConversation([]interface{}{map[string]string{"role": "user", "content": "refactor it"}})
	if len(next) != 2 {
		t.Fatalf("conversation len = %d, want injected steer", len(next))
	}
	if cb.LLMReplanRequested() {
		t.Fatal("revision already consumed by transform should not cancel its own replacement request")
	}
}

func TestSharedAgentLoopCallbacks_ForwardsNewLLMRound(t *testing.T) {
	var calls int
	cb := &sharedAgentLoopCallbacks{onNewRound: func() { calls++ }}
	cb.OnLLMNewRound()
	if calls != 1 {
		t.Fatalf("new-round callback calls = %d, want 1", calls)
	}
}

func TestSharedAgentLoopCallbacks_FinalizationRejectsLateAcceptedReplan(t *testing.T) {
	ctx := NewLoopContext("shared-finalize-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{loopCtx: ctx}
	cb.llmReplanRevision.Store(ctx.ReplanRevision())
	ctx.RequestReplan()
	if cb.TryFinalizeLLMResponse() {
		t.Fatal("final response committed despite a newer accepted steer")
	}
	cb.llmReplanRevision.Store(ctx.ReplanRevision())
	if !cb.TryFinalizeLLMResponse() {
		t.Fatal("final response should commit after the steer revision is consumed")
	}
	if ctx.AcceptingReplans() {
		t.Fatal("committed final response must close steer acceptance")
	}
}

func TestSharedAgentLoopCallbacks_LiveSteerCancelsContextAwareTool(t *testing.T) {
	registry := NewToolRegistry()
	started := make(chan struct{})
	if err := registry.Register(RegisteredTool{
		Name: "blocking_shared_tool",
		HandlerCtx: func(ctx context.Context, _ map[string]interface{}, _ coretool.ProgressCallback) string {
			close(started)
			<-ctx.Done()
			return "handler observed cancellation"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-replan", 3, nil)
	h := &IMMessageHandler{registry: registry}
	cb := &sharedAgentLoopCallbacks{handler: h, userID: "desktop-user:shared-tool-replan", loopCtx: ctx}
	cb.TransformConversation(nil)
	resultC := make(chan agent.ToolExecutionResult, 1)
	go func() { resultC <- cb.ExecuteToolStructured("blocking_shared_tool", `{}`) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("context-aware tool did not start")
	}

	ctx.RequestReplan()
	select {
	case result := <-resultC:
		if result.Outcome != agent.ToolExecutionOutcomeError {
			t.Fatalf("outcome = %q, want error; result=%q", result.Outcome, result.Result)
		}
		if !strings.Contains(result.Result, "tool execution interrupted") {
			t.Fatalf("missing interruption result: %q", result.Result)
		}
	case <-time.After(time.Second):
		t.Fatal("live steer did not cancel context-aware tool")
	}
	if !cb.LLMReplanRequested() {
		t.Fatal("cancelled tool did not leave shared loop ready to replan")
	}
}

func TestSharedAgentLoopCallbacks_DoesNotStartToolWhenSteerWinsStartRace(t *testing.T) {
	registry := NewToolRegistry()
	var calls int
	if err := registry.Register(RegisteredTool{
		Name: "must_not_start_after_steer",
		Handler: func(_ map[string]interface{}) string {
			calls++
			return "unexpected execution"
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-start-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		userID:  "desktop-user:shared-tool-start-race",
		loopCtx: ctx,
	}
	cb.TransformConversation(nil)
	ctx.RequestReplan()
	result := cb.ExecuteToolStructured("must_not_start_after_steer", `{}`)
	if calls != 0 {
		t.Fatalf("stale tool executed %d times", calls)
	}
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "tool execution interrupted") {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSharedAgentLoopCallbacks_SteerSuppressesPostToolFileMaterialization(t *testing.T) {
	registry := NewToolRegistry()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := registry.Register(RegisteredTool{
		Name: "stale_file_payload",
		Handler: func(_ map[string]interface{}) string {
			close(started)
			<-release
			return `[file_base64|c3RhbGU=|stale.txt|im]`
		},
	}); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	ctx := NewLoopContext("shared-tool-postprocess-race", 3, nil)
	cb := &sharedAgentLoopCallbacks{
		handler: &IMMessageHandler{registry: registry},
		userID:  "desktop-user:shared-tool-postprocess-race",
		loopCtx: ctx,
	}
	cb.TransformConversation(nil)
	resultC := make(chan agent.ToolExecutionResult, 1)
	go func() { resultC <- cb.ExecuteToolStructured("stale_file_payload", `{}`) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool did not start")
	}
	ctx.RequestReplan()
	close(release)
	select {
	case result := <-resultC:
		if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "tool execution interrupted") {
			t.Fatalf("unexpected result: %+v", result)
		}
		if len(cb.deliveredPaths) != 0 || cb.filesForwarded != 0 {
			t.Fatalf("stale file payload was materialized: paths=%v forwarded=%d", cb.deliveredPaths, cb.filesForwarded)
		}
	case <-time.After(time.Second):
		t.Fatal("tool did not return after release")
	}
}

func TestSharedAgentLoopPreToolCheckpointKeepsPersistedHistoryProviderValid(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	callback := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{memory: memory},
		userID:   "desktop-user:pre-tool-checkpoint",
		userText: "inspect the project",
		checkpointHistory: []agent.ConversationEntry{
			{Role: "user", Content: "inspect the project"},
		},
		checkpointRunID: "run-1",
	}
	delta := []agent.ConversationEntry{{
		Role: "assistant", Content: "", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	}}
	if err := callback.OnToolBatchStarting(delta, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain"}); err != nil {
		t.Fatalf("OnToolBatchStarting() error = %v", err)
	}
	history := memory.Load(callback.userID)
	if len(history) != 1 || history[0].Role != "user" {
		t.Fatalf("pre-tool checkpoint persisted unpaired tool declaration: %#v", history)
	}
	if task, _ := memory.ConsumeInFlightTask(callback.userID); task == "" {
		t.Fatal("pre-tool checkpoint did not persist recovery marker")
	}
}

func TestSharedPreToolCheckpointTracksPendingBatchUntilCommit(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	callback := &sharedAgentLoopCallbacks{
		handler:  &IMMessageHandler{memory: memory},
		userID:   "desktop-user:pending-checkpoint",
		userText: "update the project",
		checkpointHistory: []agent.ConversationEntry{
			{Role: "user", Content: "update the project"},
		},
		checkpointRunID: "run-1",
	}
	preTool := []agent.ConversationEntry{{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	}}
	if err := callback.OnToolBatchStarting(preTool, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain"}); err != nil {
		t.Fatalf("OnToolBatchStarting() error = %v", err)
	}
	if !callback.hasPendingToolBatch {
		t.Fatal("pre-tool checkpoint must mark the in-memory delta unsafe to save")
	}
	committed := append(append([]agent.ConversationEntry(nil), preTool...), agent.ConversationEntry{
		Role: "tool", Content: "written", ToolCallID: "call-1", ToolName: "write_file",
	})
	if err := callback.OnToolBatchCommitted(committed, agent.ToolBatchMetadata{Sequence: 1, LastToolName: "write_file", SideEffectState: "local_committed"}); err != nil {
		t.Fatalf("OnToolBatchCommitted() error = %v", err)
	}
	if callback.hasPendingToolBatch {
		t.Fatal("complete durable batch must allow normal terminal history saving")
	}
}

func TestInterruptedSharedLoopExitResponseDoesNotWritePartialHistory(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	h := &IMMessageHandler{memory: memory}
	const userID, runID = "desktop-user:partial-shared-exit", "run-1"
	durable := []agent.ConversationEntry{{Role: "user", Content: "update the project"}}
	if err := memory.PersistInFlightCheckpoint(userID, durable, "update the project", "/project", runID, agent.InFlightCheckpoint{
		Sequence: 1, LastToolName: "write_file", SideEffectState: "external_uncertain",
	}); err != nil {
		t.Fatalf("PersistInFlightCheckpoint() error = %v", err)
	}
	partial := append(append([]agent.ConversationEntry(nil), durable...), agent.ConversationEntry{
		Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}},
	})
	resp := h.interruptedSharedLoopExitResponse("update the project")
	if resp == nil || !strings.Contains(resp.Text, "interrupted") {
		t.Fatalf("response = %#v", resp)
	}
	if got := memory.Load(userID); len(got) != 1 || got[0].Role != "user" || got[0].Content != "update the project" {
		t.Fatalf("partial history overwrote durable provider-valid prefix: %#v", got)
	}
	if len(partial) != 2 { // Keep the test's partial shape explicit and intentional.
		t.Fatalf("partial history setup changed: %#v", partial)
	}
	if task, _ := memory.ConsumeInFlightTask(userID); task == "" {
		t.Fatal("interrupted exit must preserve the recovery marker")
	}
}

func TestShouldSaveSharedLoopTerminalHistoryRejectsPendingToolBatch(t *testing.T) {
	result := agent.LoopResult{HistoryDelta: []agent.ConversationEntry{
		{Role: "user", Content: "update"},
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "call-1", "name": "write_file"}}},
	}}
	if shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{hasPendingToolBatch: true}) {
		t.Fatal("partial tool batch must not be written by the generic terminal save")
	}
	if !shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{}) {
		t.Fatal("without a pending batch the complete terminal history remains saveable")
	}
	result.Error = "recovery_checkpoint_failed"
	if shouldSaveSharedLoopTerminalHistory(result, &sharedAgentLoopCallbacks{}) {
		t.Fatal("checkpoint failure must not fall back to an asynchronous history save")
	}
}

func TestInterruptedSharedLoopResultResponseKeepsRecoverySemantics(t *testing.T) {
	h := &IMMessageHandler{}
	callback := &sharedAgentLoopCallbacks{userID: "desktop-user:result-interrupt"}
	resp := h.interruptedSharedLoopResultResponse(
		"update the project", agent.LoopResult{Usage: agent.TurnUsage{InputTokens: 4, OutputTokens: 6}},
		"request-1", nil, nil, callback,
	)
	if resp == nil || !resp.HardExit || resp.Error != "" || resp.RequestID != "request-1" || resp.SessionKey != callback.userID {
		t.Fatalf("response = %#v", resp)
	}
	if !strings.Contains(resp.Text, "interrupted") || resp.InputTokens != 4 || resp.OutputTokens != 6 {
		t.Fatalf("response did not preserve recovery messaging/usage: %#v", resp)
	}
}

func TestSameConversationElementsDetectsSameLengthReplacement(t *testing.T) {
	first := map[string]interface{}{"role": "system", "content": "sys"}
	oldUser := map[string]string{"role": "user", "content": "old"}
	conversation := []interface{}{first, oldUser}
	alias := conversation[:]
	replacement := []interface{}{first, map[string]string{"role": "user", "content": "new"}}

	if !sameConversationElements(alias, conversation) {
		t.Fatal("slice aliases should compare as the same conversation")
	}
	if sameConversationElements(replacement, conversation) {
		t.Fatal("same-length replacement must be detected")
	}
}
