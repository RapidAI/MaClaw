package main

import "testing"

func TestClearAgentViewAdvancesLifecycleSequenceWithoutContext(t *testing.T) {
	app := &App{}
	before := app.agentViewSeq()
	if app.clearAgentViewWithPayload("workflow:form:requirements", map[string]interface{}{"workflow_id": "wf-1"}) {
		t.Fatal("clear should not emit without a Wails context")
	}
	if got := app.agentViewSeq(); got != before+1 {
		t.Fatalf("agentViewSeq = %d, want %d", got, before+1)
	}
}

func TestCloneAgentViewLifecyclePayloadPreservesSequence(t *testing.T) {
	payload := cloneAgentViewPayload(map[string]interface{}{"seq": int64(7), "view_id": "workflow:form:requirements"})
	if payload["seq"] != int64(7) {
		t.Fatalf("seq = %#v, want 7", payload["seq"])
	}
	if payload["view_id"] != "workflow:form:requirements" {
		t.Fatalf("view_id = %#v", payload["view_id"])
	}
}

func TestEmitAgentViewAdvancesLifecycleSequenceWithoutContext(t *testing.T) {
	app := &App{}
	before := app.agentViewSeq()
	if app.emitAgentView(map[string]interface{}{"type": "form", "title": "Test", "fields": []map[string]interface{}{}}) {
		t.Fatal("emit should not publish without a Wails context")
	}
	if got := app.agentViewSeq(); got != before+1 {
		t.Fatalf("agentViewSeq = %d, want %d", got, before+1)
	}
}

func TestDirectAgentViewLifecycleAdvancesSequenceWithoutContext(t *testing.T) {
	app := &App{}
	before := app.agentViewSeq()
	if app.emitAgentViewLifecycle(agentViewLifecycleSubmit, map[string]interface{}{"view_id": "workflow:form:requirements"}) {
		t.Fatal("lifecycle should not publish without a Wails context")
	}
	if got := app.agentViewSeq(); got != before+1 {
		t.Fatalf("agentViewSeq = %d, want %d", got, before+1)
	}
}

func TestAgentViewLifecyclePreservesCallerSequenceWithoutContext(t *testing.T) {
	app := &App{}
	if app.emitAgentViewLifecycle(agentViewLifecycleDismiss, map[string]interface{}{"seq": int64(7), "view_id": "workflow:form:requirements"}) {
		t.Fatal("lifecycle should not publish without a Wails context")
	}
	if got := app.agentViewSeq(); got != 0 {
		t.Fatalf("agentViewSeq = %d, want 0", got)
	}
}
