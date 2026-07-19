package main

import (
	"encoding/json"
	"testing"
)

// onSessionSteer validates params and degrades gracefully when no GUI app is
// attached (accepted=false so ACP clients fall back to queueing).
func TestOnSessionSteerValidation(t *testing.T) {
	s := newACPHostSession(nil, "tok", nil)

	// missing text
	if _, rpcErr := s.onSessionSteer(mustJSON(t, map[string]any{"sessionId": "s1"})); rpcErr == nil {
		t.Fatal("expected params error for missing text")
	}
	// unknown session
	if _, rpcErr := s.onSessionSteer(mustJSON(t, map[string]any{"sessionId": "s1", "text": "hi"})); rpcErr == nil {
		t.Fatal("expected error for unknown sessionId")
	}

	// known session, no app attached → accepted=false (client queues instead)
	s.sessions["s1"] = &acpHostAgentSession{ID: "s1", UserID: "desktop-user"}
	res, rpcErr := s.onSessionSteer(mustJSON(t, map[string]any{"sessionId": "s1", "text": "hi"}))
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", res)
	}
	if accepted, _ := m["accepted"].(bool); accepted {
		t.Fatal("accepted must be false when no app is attached")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(raw)
}
