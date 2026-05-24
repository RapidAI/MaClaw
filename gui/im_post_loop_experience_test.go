package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestAgentLoopTerminalExperienceEventSuccessWithTrace(t *testing.T) {
	ctx := NewLoopContext("loop-1", 3, nil)
	ctx.RunID = "trace-1"
	ctx.SetLoopState(LoopStateCompleted)

	event, ok := agentLoopTerminalExperienceEvent(ctx, &IMAgentResponse{Text: "done"})
	if !ok {
		t.Fatal("expected terminal experience event")
	}
	if event.EventType != lifecycle.EventTaskSucceeded || event.Outcome != "success" {
		t.Fatalf("unexpected event outcome: %+v", event)
	}
	if event.TraceID != "trace-1" || event.TaskID != "loop-1" {
		t.Fatalf("unexpected event context: %+v", event)
	}
}

func TestAgentLoopTerminalExperienceEventFailureWithTrace(t *testing.T) {
	ctx := NewLoopContext("loop-2", 3, nil)
	ctx.RunID = "trace-2"

	event, ok := agentLoopTerminalExperienceEvent(ctx, &IMAgentResponse{Error: "boom"})
	if !ok {
		t.Fatal("expected terminal experience event")
	}
	if event.EventType != lifecycle.EventTaskFailed || event.Outcome != "failure" || event.ErrorClass != "agent_loop_error" {
		t.Fatalf("unexpected event outcome: %+v", event)
	}
	if event.Reason != "boom" || event.TraceID != "trace-2" || event.TaskID != "loop-2" {
		t.Fatalf("unexpected event metadata: %+v", event)
	}
}

func TestRecordWorkflowPhaseCompletedExperienceUsesLoopTrace(t *testing.T) {
	app := &App{}
	h := &IMMessageHandler{app: app}
	ctx := NewLoopContext("loop-3", 3, nil)
	ctx.RunID = "trace-3"

	h.recordWorkflowPhaseCompletedExperience(IMUserMessage{Text: "draft requirements"}, ctx, "requirements")

	events := app.experienceEvents.List()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != lifecycle.EventWorkflowPhaseCompleted || event.Outcome != "success" || event.Reason != "requirements" {
		t.Fatalf("unexpected workflow event: %+v", event)
	}
	if event.TraceID != "trace-3" || event.TaskID != "loop-3" || event.Query != "draft requirements" {
		t.Fatalf("unexpected workflow event context: %+v", event)
	}
}

func TestRecordWorkflowReviewFeedbackExperienceUsesStoredPhaseTrace(t *testing.T) {
	app := &App{}
	h := &IMMessageHandler{app: app}
	h.workflowReviewExperienceContext.Store("u1", workflowReviewExperienceContext{
		EventContext: lifecycle.EventContext{TraceID: "trace-review", TaskID: "loop-review"},
		PhaseID:      "requirements",
	})

	h.recordWorkflowReviewFeedbackExperience("u1", workflow.ReviewIntentSupplement, "add auth details")

	events := app.experienceEvents.List()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	event := events[0]
	if event.EventType != lifecycle.EventUserFeedbackReceived || event.Outcome != "supplement" || event.Reason != "requirements" {
		t.Fatalf("unexpected feedback event: %+v", event)
	}
	if event.TraceID != "trace-review" || event.TaskID != "loop-review" || event.Query != "add auth details" {
		t.Fatalf("unexpected feedback event context: %+v", event)
	}
	if _, ok := h.workflowReviewExperienceContext.Load("u1"); !ok {
		t.Fatal("supplement should keep review context until regenerated phase output replaces it")
	}

	h.recordWorkflowReviewFeedbackExperience("u1", workflow.ReviewIntentConfirm, "ok")
	if _, ok := h.workflowReviewExperienceContext.Load("u1"); ok {
		t.Fatal("confirm should clear stored review context")
	}
}
