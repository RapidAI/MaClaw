package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// BR-20261120-8A708E128485 — RunStatusPaused is a terminal run status the runs
// endpoint can return and accept as a filter. The OpenAPI status enum must
// advertise it: a spec-generated client that validates the enum otherwise
// rejects or drops a paused run, re-reading 待决策 as an unknown/failed state.
// The enum must cover every terminal RunStatus the store can persist, not just
// the ones that existed when the schema was first written.
func TestOpenAPIRunStatusEnumCoversTerminalStatuses(t *testing.T) {
	schema := openAPIQuerySchema("/api/v1/instances/{instanceId}/runs", "status")
	rawEnum, ok := schema["enum"].([]string)
	if !ok {
		t.Fatalf("runs status filter must declare a string enum, got %#v", schema)
	}
	enum := make(map[string]bool, len(rawEnum))
	for _, v := range rawEnum {
		enum[v] = true
	}
	// Every terminal status a stored run can hold must be a documented value,
	// so a client never sees an undeclared status on the wire.
	for _, status := range []agentservice.RunStatus{
		agentservice.RunStatusRunning,
		agentservice.RunStatusSucceeded,
		agentservice.RunStatusPaused,
		agentservice.RunStatusFailed,
		agentservice.RunStatusCancelled,
		agentservice.RunStatusInterrupted,
	} {
		if !enum[string(status)] {
			t.Fatalf("runs status enum missing stored status %q: %#v", status, rawEnum)
		}
	}
	// The new paused value is the regression this commit guards: it must be a
	// first-class filter value, not an afterthought a validator rejects.
	if !enum["paused"] {
		t.Fatalf("runs status enum must accept paused: %#v", rawEnum)
	}
}
