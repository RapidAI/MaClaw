package guiapp

import (
	"encoding/json"
	"strings"
	"testing"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// The welcome-page wizard (workflow = "none") hands off its first message with
// the no_workflow_interception transport flag. Routing must pass the message
// straight to the agent loop even when a workflow is active for the owner, and
// messages without the flag must keep the exact previous behavior.
func TestRouteWithWorkflowV2_NoWorkflowInterceptionBypassesActiveWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-no-workflow-interception"
	workflowType := v2.WorkflowType("no_workflow_interception")
	if err := engine.GetRegistry().Register(&v2.TemplateSpec{
		Type:        workflowType,
		Name:        "no workflow interception",
		Description: "test template",
		Phases: []v2.PhaseSpec{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: v2.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: v2.ToolFilterFull, Kind: v2.PhaseKindExecution, MutationScope: v2.MutationScopeProject},
		},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := engine.StartWorkflow(userID, v2.StructuredIntent{
		Category: workflowType,
		Summary:  "build something",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if handler.getWorkflowV2().machine.GetActive(userID) == nil {
		t.Fatal("workflow should be active for the test owner")
	}

	text := "帮我继续这个工作流任务"
	flagged := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:                 userID,
		Text:                   text,
		Platform:               "desktop",
		NoWorkflowInterception: true,
	}, text)
	if flagged.Response != nil || flagged.WorkflowAgentLoop || flagged.WorkflowDocPhase || flagged.SkipNeedsConfirmGate {
		t.Fatalf("flagged message must bypass workflow routing entirely, got %#v", flagged)
	}

	// Control: the same message without the flag keeps the previous behavior
	// (the active workflow consumes it).
	unflagged := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   userID,
		Text:     text,
		Platform: "desktop",
	}, text)
	if unflagged.Response == nil && !unflagged.WorkflowAgentLoop && !unflagged.WorkflowDocPhase && !unflagged.SkipNeedsConfirmGate {
		t.Fatalf("unflagged message must keep previous routing behavior, got empty result %#v", unflagged)
	}

	// Control 2: no active workflow and no flag → ordinary passthrough
	// (routeWithWorkflowV2 is only entered when a workflow state exists, but a
	// direct call must still return the empty result for unrelated messages).
	other := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   "test-no-workflow-interception-unrelated",
		Text:     text,
		Platform: "desktop",
	}, text)
	if other.Response != nil || other.WorkflowAgentLoop || other.WorkflowDocPhase || other.SkipNeedsConfirmGate {
		t.Fatalf("unrelated unflagged message must pass through untouched, got %#v", other)
	}
}

// The bypass is intentionally total for the flagged handoff message: even an
// explicit workflow choice command (`__workflow_choice__ ...`) is not
// consumed — the wizard's「无」selection is authoritative, so the command
// text falls through to the agent loop as plain task text and the pending
// choice is left intact. Without the flag the same command is consumed.
func TestRouteWithWorkflowV2_NoWorkflowInterceptionBypassesExplicitChoiceCommand(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-no-workflow-interception-explicit"
	handler.pendingWorkflowChoice.Store(userID, &pendingWorkflowChoice{
		Msg:         IMUserMessage{UserID: userID, Text: "original task", Platform: "desktop"},
		RouteResult: &v2.RouteResult{WorkflowType: "some_template"},
		ChoiceID:    "choice-1",
	})

	command := buildWorkflowChoiceCommand(workflowChoiceSkip, "choice-1")
	flagged := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:                 userID,
		Text:                   command,
		Platform:               "desktop",
		NoWorkflowInterception: true,
	}, command)
	if flagged.Response != nil || flagged.WorkflowAgentLoop || flagged.WorkflowDocPhase || flagged.SkipNeedsConfirmGate || flagged.ReplayText != "" {
		t.Fatalf("flagged choice command must bypass routing entirely, got %#v", flagged)
	}
	if _, stillPending := handler.pendingWorkflowChoice.Load(userID); !stillPending {
		t.Fatal("flagged choice command must not consume the pending workflow choice")
	}

	unflagged := handler.routeWithWorkflowV2(IMUserMessage{
		UserID:   userID,
		Text:     command,
		Platform: "desktop",
	}, command)
	if unflagged.ReplayText != "original task" {
		t.Fatalf("unflagged choice command must be consumed (replay original task), got %#v", unflagged)
	}
	if _, stillPending := handler.pendingWorkflowChoice.Load(userID); stillPending {
		t.Fatal("unflagged choice command must consume the pending workflow choice")
	}
}

// The Wails request field is wire-visible under the documented JSON key so the
// frontend payload maps 1:1.
func TestAIAssistantSendRequest_NoWorkflowInterceptionJSONKey(t *testing.T) {
	raw, err := json.Marshal(AIAssistantSendRequest{Text: "x", NoWorkflowInterception: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"no_workflow_interception":true`) {
		t.Fatalf("expected no_workflow_interception in payload, got %s", raw)
	}
	zero, err := json.Marshal(AIAssistantSendRequest{Text: "x"})
	if err != nil {
		t.Fatalf("marshal zero: %v", err)
	}
	if strings.Contains(string(zero), "no_workflow_interception") {
		t.Fatalf("zero value must stay wire-absent (additive), got %s", zero)
	}
}
