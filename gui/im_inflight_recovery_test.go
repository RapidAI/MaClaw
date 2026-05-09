package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestImplicitInFlightRecoveryDecisionStartsNewForOrdinaryInput(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_recovery",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "check whether omniroute supports websocket"},
		"check whether omniroute supports websocket",
		slot,
		explicitTaskSlotDecision{},
	)
	if !decision.StartNewTask {
		t.Fatal("expected ordinary input to start a new task instead of binding recovery slot")
	}
	if decision.DismissSlotID != slot.SlotID {
		t.Fatalf("DismissSlotID = %q, want %q", decision.DismissSlotID, slot.SlotID)
	}
	if decision.ResumeSlotID != "" {
		t.Fatalf("ResumeSlotID = %q, want empty", decision.ResumeSlotID)
	}
}

func TestImplicitInFlightRecoveryDecisionResumesOnlyForExplicitResume(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "continue this"},
		"continue this",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.ResumeSlotID != slot.SlotID {
		t.Fatalf("ResumeSlotID = %q, want %q", decision.ResumeSlotID, slot.SlotID)
	}
	if decision.StartNewTask {
		t.Fatal("did not expect explicit resume input to start a new task")
	}
}

func TestImplicitInFlightRecoveryDecisionResumesForChineseContinue(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "\u7ee7\u7eed"},
		"\u7ee7\u7eed",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.ResumeSlotID != slot.SlotID {
		t.Fatalf("ResumeSlotID = %q, want %q", decision.ResumeSlotID, slot.SlotID)
	}
	if decision.StartNewTask {
		t.Fatal("did not expect Chinese resume input to start a new task")
	}
}

func TestImplicitInFlightRecoveryDecisionDoesNotOverrideExplicitAction(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_recovery",
	}
	original := explicitTaskSlotDecision{ResumeSlotID: "slot-explicit"}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "new request"},
		"new request",
		slot,
		original,
	)
	if decision.ResumeSlotID != original.ResumeSlotID || decision.StartNewTask {
		t.Fatalf("decision = %#v, want explicit resume preserved", decision)
	}
}

func TestImplicitInFlightRecoveryDecisionIgnoresOtherSlotSources(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-max-rounds",
		Source: "max_rounds",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user", Text: "check whether omniroute supports websocket"},
		"check whether omniroute supports websocket",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.StartNewTask || decision.DismissSlotID != "" || decision.ResumeSlotID != "" {
		t.Fatalf("decision = %#v, want ordinary unfinished slot untouched", decision)
	}
}

func TestImplicitInFlightRecoveryDecisionKeepsEmptyInputNeutral(t *testing.T) {
	slot := &agent.UnfinishedTaskSlot{
		SlotID: "slot-recovery",
		Source: "in_flight_lease_expired",
	}
	decision := applyImplicitInFlightRecoveryDecision(
		IMUserMessage{UserID: "desktop-user"},
		"",
		slot,
		explicitTaskSlotDecision{},
	)
	if decision.StartNewTask || decision.DismissSlotID != "" || decision.ResumeSlotID != "" {
		t.Fatalf("decision = %#v, want empty input to leave recovery hint available", decision)
	}
}

func TestShouldRecoverInFlightMarkerWaitsForActiveLoop(t *testing.T) {
	msg := IMUserMessage{UserID: "desktop-user", Text: "new request"}
	if shouldRecoverInFlightMarker(msg, nil, &LoopContext{ID: "active-loop"}) {
		t.Fatal("did not expect in-flight marker recovery while an agent loop is still active")
	}
}

func TestShouldRecoverInFlightMarkerOnlyForForegroundWithoutSlot(t *testing.T) {
	msg := IMUserMessage{UserID: "desktop-user", Text: "new request"}
	if !shouldRecoverInFlightMarker(msg, nil, nil) {
		t.Fatal("expected foreground message with no active slot or loop to recover marker")
	}
	if shouldRecoverInFlightMarker(IMUserMessage{UserID: "desktop-user", IsBackground: true}, nil, nil) {
		t.Fatal("did not expect background message to recover foreground marker")
	}
	if shouldRecoverInFlightMarker(msg, &agent.UnfinishedTaskSlot{SlotID: "existing"}, nil) {
		t.Fatal("did not expect recovery when an unfinished slot already exists")
	}
}
