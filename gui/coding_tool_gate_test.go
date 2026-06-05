package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestCodingGate_BlocklistAndAllowlist(t *testing.T) {
	expectedCoding := []string{
		"create_session", "bash", "write_file", "edit_file", "edit_lines",
		"craft_tool", "send_and_observe", "control_session",
		"browser",
		"gui_record_start", "gui_record_stop", "gui_observe", "gui_verify",
	}
	for _, name := range expectedCoding {
		if !isCodingTool(name) {
			t.Fatalf("expected %s to be blocked", name)
		}
	}
	if !isCodingTool("browser_navigate") || !isCodingTool(" browser_click ") {
		t.Fatal("all legacy browser_* tool names must be blocked by prefix")
	}
	if !isCodingTool("browser_task_replay") || !isCodingTool("browser_record_start") {
		t.Fatal("disabled legacy browser_* tool names must be blocked by prefix")
	}
	if len(codingToolBlocklist) != len(expectedCoding) {
		t.Fatalf("coding tool blocklist size = %d, want %d", len(codingToolBlocklist), len(expectedCoding))
	}

	expectedDelivery := []string{"generate_pdf", "office", "send_file", "memory", "open", "set_nickname", "manage_config", "ask_user", "task"}
	for _, name := range expectedDelivery {
		if isCodingTool(name) {
			t.Fatalf("expected %s to be allowed", name)
		}
		if !deliveryToolAllowlist[name] {
			t.Fatalf("delivery allowlist missing %s", name)
		}
	}
	if len(deliveryToolAllowlist) != len(expectedDelivery) {
		t.Fatalf("delivery tool allowlist size = %d, want %d", len(deliveryToolAllowlist), len(expectedDelivery))
	}
}

func TestCodingGate_DirectModeSessionBlocklist(t *testing.T) {
	blocked := []string{"create_session", "send_and_observe", "control_session", "get_session_output", "get_session_events", "interrupt_session", "kill_session", "list_sessions", "send_input"}
	for _, name := range blocked {
		if !isDirectModeBlockedTool(name) {
			t.Fatalf("expected direct mode to block %s", name)
		}
	}
	for _, name := range []string{"bash", "write_file", "edit_file", "send_file"} {
		if isDirectModeBlockedTool(name) {
			t.Fatalf("expected direct mode to allow %s", name)
		}
	}
}

func TestCodingGate_WithoutClassifiers_UsesOrdinaryAgentPath(t *testing.T) {
	cfg := newCodingToolGateConfigWithClassifier("build a web app", LoopKindNormal, nil, nil, "user")
	if cfg.active {
		t.Fatalf("classifier absence must not activate workflow gate")
	}
	if !strings.Contains(cfg.reason, "ordinary agent") {
		t.Fatalf("expected ordinary-agent reason, got %q", cfg.reason)
	}
}

func TestCodingGate_BackgroundBypassesGate(t *testing.T) {
	cfg := newCodingToolGateConfigWithClassifier("build a web app", LoopKindBackground, nil, nil, "user")
	if cfg.active {
		t.Fatalf("background loop should bypass gate")
	}
}

func TestCodingGate_WithoutClassifiers_DoesNotInferWorkflow(t *testing.T) {
	for _, text := range []string{
		"translate this paragraph",
		"ssh to the server and inspect logs",
		"deploy to production",
		"build a todo app",
		"",
	} {
		cfg := newCodingToolGateConfigWithClassifier(text, LoopKindNormal, nil, nil, "user")
		if cfg.active {
			t.Fatalf("text=%q: classifier absence must not activate workflow gate", text)
		}
	}
}

func TestCodingGate_NoKeywordBugFixBypassWithoutClassifier(t *testing.T) {
	cfg := newCodingToolGateConfigWithClassifier("fix the crash", LoopKindNormal, nil, nil, "user")
	if cfg.active {
		t.Fatalf("without a classifier, bug-fix text must not infer workflow gate")
	}
	if cfg.bugFix {
		t.Fatalf("without a classifier, bugFix must not be inferred from text")
	}
}

func TestCodingGate_InconclusiveGICUsesOrdinaryAgentPath(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	cfg := newCodingToolGateConfigWithClassifier("fix the crash", LoopKindNormal, gic, nil, "user")
	if cfg.active {
		t.Fatalf("inconclusive GIC result must not activate workflow gate")
	}
	if cfg.bugFix {
		t.Fatalf("inconclusive GIC result must not infer bugFix")
	}
	if !strings.Contains(cfg.reason, "ordinary agent") {
		t.Fatalf("expected inconclusive classifier reason, got %q", cfg.reason)
	}
}

func TestCodingGate_MapAcceptedNewProjectActivates(t *testing.T) {
	cfg := mapGateIntentToConfig(GateIntentResult{
		Intent:     GateIntentNewProject,
		Confidence: 0.60,
		Layer:      3,
	}, false)
	if !cfg.active || cfg.intent != intentCoding {
		t.Fatalf("accepted new_project must activate coding gate, got %#v", cfg)
	}
}

func TestCodingGate_MapUnknownUsesOrdinaryAgentPath(t *testing.T) {
	cfg := mapGateIntentToConfig(GateIntentResult{
		Intent:     GateIntentUnknown,
		Confidence: 0.99,
		Layer:      3,
	}, false)
	if cfg.active || cfg.intent != intentAmbiguous {
		t.Fatalf("unknown classifier result must use ordinary agent path, got %#v", cfg)
	}
}

func TestCodingGate_DegradedUICUsesOrdinaryAgentPath(t *testing.T) {
	uic := intent.New(intent.Config{Embedder: embedding.NoopEmbedder{}})
	cfg := newCodingToolGateConfigWithClassifier("fix the crash", LoopKindNormal, nil, uic, "user")
	if cfg.active {
		t.Fatalf("degraded UIC result must not activate workflow gate")
	}
	if !strings.Contains(cfg.reason, "ordinary agent") {
		t.Fatalf("expected inconclusive classifier reason, got %q", cfg.reason)
	}
}

func TestCodingGate_ReusesRuntimeSemanticIntent(t *testing.T) {
	ctx := NewLoopContext("chat", 300, nil)
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary:    intent.LabelLiveData,
		Confidence: 0.95,
		Layer:      2,
		Reason:     "test semantic live data",
	}
	h := &IMMessageHandler{}
	cfg, _, _ := h.prepareAgentLoopCodingGate("user", "weather", ctx, nil)
	if cfg.active {
		t.Fatalf("live_data semantic intent must not activate coding gate")
	}
	if cfg.intent != intentNonCoding {
		t.Fatalf("gate intent = %v, want non_coding", cfg.intent)
	}
	if !strings.Contains(cfg.reason, "non_coding") {
		t.Fatalf("expected reused semantic gate reason, got %q", cfg.reason)
	}
}

func TestCodingGate_BackgroundBypassesReusedSemanticIntent(t *testing.T) {
	ctx := NewLoopContext("background", 300, nil)
	ctx.Kind = LoopKindBackground
	ctx.Runtime.SemanticIntent = &intent.ClassificationResult{
		Primary:          intent.LabelCoding,
		Confidence:       0.99,
		Layer:            3,
		CreationOriented: true,
	}
	h := &IMMessageHandler{}
	cfg, _, _ := h.prepareAgentLoopCodingGate("user", "build app", ctx, nil)
	if cfg.active || cfg.intent != intentUnknown {
		t.Fatalf("background loop should bypass semantic gate, got %#v", cfg)
	}
	if !strings.Contains(cfg.reason, "background") {
		t.Fatalf("expected background reason, got %q", cfg.reason)
	}
}

func TestCodingGate_ApplyPartitionsTools(t *testing.T) {
	calls := []llm.ToolCall{
		toolCallNamed("create_session"),
		toolCallNamed("generate_pdf"),
		toolCallNamed("send_file"),
		toolCallNamed("bash"),
		toolCallNamed("write_file"),
		toolCallNamed("memory"),
	}

	got := applyCodingToolGate(calls)
	if !got.applied {
		t.Fatal("expected gate to strip coding tools")
	}
	if len(got.stripped) != 3 {
		t.Fatalf("stripped = %d, want 3", len(got.stripped))
	}
	if len(got.remaining) != 3 {
		t.Fatalf("remaining = %d, want 3", len(got.remaining))
	}
	if got.remaining[0].Function.Name != "generate_pdf" || got.remaining[1].Function.Name != "send_file" || got.remaining[2].Function.Name != "memory" {
		t.Fatalf("remaining order not preserved: %#v", got.remaining)
	}
}

func TestCodingGate_ApplyNoopsForEmptyAndDeliveryOnly(t *testing.T) {
	if got := applyCodingToolGate(nil); got.applied || len(got.stripped) != 0 || len(got.remaining) != 0 {
		t.Fatalf("empty tool list should be a no-op: %#v", got)
	}
	calls := []llm.ToolCall{toolCallNamed("generate_pdf"), toolCallNamed("send_file"), toolCallNamed("memory")}
	got := applyCodingToolGate(calls)
	if got.applied {
		t.Fatalf("delivery-only tool list should not apply gate: %#v", got)
	}
	if len(got.remaining) != len(calls) {
		t.Fatalf("remaining = %d, want %d", len(got.remaining), len(calls))
	}
}

func TestCodingGate_ApplyStripsAllCodingTools(t *testing.T) {
	calls := []llm.ToolCall{toolCallNamed("bash"), toolCallNamed("write_file"), toolCallNamed("create_session")}
	got := applyCodingToolGate(calls)
	if !got.applied {
		t.Fatal("expected gate to strip all coding tools")
	}
	if len(got.stripped) != len(calls) || len(got.remaining) != 0 {
		t.Fatalf("unexpected partition: %#v", got)
	}
}

func TestCodingGate_WorkflowAgentLoopBypassesGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-coding-gate-workflow-loop-bypass"
	engine := handler.app.workflowEngine

	_, err := engine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.95,
		Ready:      true,
	}, workflow.WorkflowStartOptions{ProjectPath: "/proj"})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	workflowLoopCtx := &LoopContext{WorkflowAgentLoop: true}
	if handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, workflowLoopCtx) {
		t.Fatal("form-blocked workflow phase must not bypass coding gate before phase prompt is available")
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm(initial) failed: %v", err)
	}
	if !handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, workflowLoopCtx) {
		t.Fatal("workflow agent loop must let workflow tool policy own doc phases")
	}
	if handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, &LoopContext{}) {
		t.Fatal("active workflow must not bypass coding gate outside a workflow agent loop")
	}

	for _, label := range []string{"requirements", "technical design", "task breakdown"} {
		if _, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, substantialWorkflowDoc(label)); err != nil {
			t.Fatalf("SavePhaseOutputAndMaybeAdvance(%s) failed: %v", label, err)
		}
		if _, err := engine.ApplyReviewIntent(userID, workflow.ReviewIntentConfirm, ""); err != nil {
			t.Fatalf("ApplyReviewIntent(%s) failed: %v", label, err)
		}
	}

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingImplementation {
		t.Fatalf("expected implementation phase, got %#v", ws)
	}
	if !handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, workflowLoopCtx) {
		t.Fatal("execution phase must bypass the three-phase coding gate")
	}
	if handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, &LoopContext{}) {
		t.Fatal("execution phase must not bypass coding gate outside a workflow agent loop")
	}

	cfg := codingToolGateConfig{active: true, intent: intentCoding, reason: "test active coding gate"}
	tools := []map[string]interface{}{toolDef("bash", "bash", nil, nil), toolDef("send_file", "send", nil, nil)}
	filtered, _ := handler.applyInitialCodingToolGate(tools, cfg, handler.shouldBypassCodingGateForWorkflowAgentLoop(userID, workflowLoopCtx), func() bool { return false })
	if len(filtered) != len(tools) {
		t.Fatalf("execution phase must preserve coding tools, got %d want %d", len(filtered), len(tools))
	}
}

func TestCodingGateWorkflowAgentLoopUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-coding-gate-workflow-owner"
	remoteOwnerID := "remote:mobile-coding-gate-owner"
	engine := handler.app.workflowEngine
	_, err := engine.StartWorkflowWithOptions(desktopID, workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build desktop project",
		Goals:      []string{"build desktop project"},
		Confidence: 0.95,
		Ready:      true,
	}, workflow.WorkflowStartOptions{ProjectPath: "/desktop"})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions desktop failed: %v", err)
	}
	if err := engine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm desktop failed: %v", err)
	}
	handler.lastUserID = desktopID
	ctx := &LoopContext{WorkflowAgentLoop: true, Runtime: RuntimeContext{RequestID: "req-coding-gate", PolicyOwnerID: remoteOwnerID}}

	if handler.shouldBypassCodingGateForWorkflowAgentLoop(desktopID, ctx) {
		t.Fatal("workflow coding gate must not bypass from desktop workflow when runtime owner has no workflow")
	}

	_, err = engine.StartWorkflowWithOptions(remoteOwnerID, workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build remote project",
		Goals:      []string{"build remote project"},
		Confidence: 0.95,
		Ready:      true,
	}, workflow.WorkflowStartOptions{ProjectPath: "/remote"})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions remote failed: %v", err)
	}
	if err := engine.SkipPhaseForm(remoteOwnerID); err != nil {
		t.Fatalf("SkipPhaseForm remote failed: %v", err)
	}
	if !handler.shouldBypassCodingGateForWorkflowAgentLoop(desktopID, ctx) {
		t.Fatal("workflow coding gate should follow runtime owner workflow")
	}
}

func substantialWorkflowDoc(label string) string {
	return "# " + label + "\n\n" +
		"This phase deliverable contains enough structure to pass the minimum workflow quality gate.\n" +
		"It intentionally avoids phase-specific keywords so the test verifies state-machine behavior rather than text matching.\n" +
		"- item one\n- item two\n- item three\n"
}

func toolCallNamed(name string) llm.ToolCall {
	return llm.ToolCall{
		ID:   "call_" + name,
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      name,
			Arguments: "{}",
		},
	}
}

func TestExecuteAgentLoopRejectsCodingSessionToolBeforeArgValidation(t *testing.T) {
	handler := &IMMessageHandler{registry: NewToolRegistry()}
	registerBuiltinTools(handler.registry, handler)

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		SkipWorkflowGate: true,
		ToolCall:         toolCallNamed(" send_and_observe "),
	})

	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("send_and_observe should be policy-rejected before parameter validation, got %#v", result)
	}
	if strings.Contains(result.Text, "requires parameter") {
		t.Fatalf("coding-session tool reached missing-parameter validation: %q", result.Text)
	}
}
