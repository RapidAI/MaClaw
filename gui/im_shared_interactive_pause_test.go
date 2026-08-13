package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSharedAskUserCheckpointConflictFailsClosed(t *testing.T) {
	memory := agent.NewConversationMemory()
	defer memory.Stop()
	const userID = "desktop-user:ask-checkpoint-conflict"
	memory.SetInFlightTaskForRun(userID, "newer task", "/project", "run-new")
	h := &IMMessageHandler{memory: memory}
	history := []agent.ConversationEntry{
		{Role: "user", Content: "choose an option"},
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "call-ask"}}},
	}
	previous := &pendingAskUserState{Question: "previous question", Timestamp: time.Now()}
	h.pendingAskUser.Store(userID, previous)
	out := h.handleAgentLoopAskUserToolResult(
		userID, "desktop", "choose an option",
		agent.AskUserResultMarker(&agent.AskUserRequest{Question: "Which option?", Options: []string{"A", "B"}}),
		false, "call-ask", nil, history, nil, nil, false,
	)
	if out.Response == nil {
		t.Fatal("setup must create an interactive ask_user response")
	}
	if err := h.persistSharedInteractivePause(userID, "run-old", out.History); err == nil {
		t.Fatal("expected run-scoped checkpoint conflict")
	}
	resp := h.sharedInteractivePausePersistenceFailureResponse(userID, "req-conflict", nil, nil, nil)
	if resp.ResponseSource == imResponseSourceAskUser.String() || resp.Error != "recovery_checkpoint_failed" {
		t.Fatalf("response = %#v, want non-interactive persistence failure", resp)
	}
	if len(resp.Actions) != 0 || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("failure response must have explanation and no actions: %#v", resp)
	}
	if got, ok := h.pendingAskUser.Load(userID); !ok || got != previous {
		t.Fatalf("failed ask-user finalization discarded prior pending state: %#v", got)
	}
	if task, _ := memory.ConsumeInFlightTask(userID); task != "newer task" {
		t.Fatalf("failed ask-user finalization changed newer marker: %q", task)
	}
}
