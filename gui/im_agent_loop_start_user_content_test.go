package main

import "testing"

func TestAgentLoopStartStateCarriesPreparedUserContent(t *testing.T) {
	state := agentLoopStartState{UserContent: "prepared voice transcript"}
	if got, ok := state.UserContent.(string); !ok || got != "prepared voice transcript" {
		t.Fatalf("UserContent = %#v", state.UserContent)
	}
}
