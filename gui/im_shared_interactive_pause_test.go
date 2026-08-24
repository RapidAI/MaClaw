package main

import (
	"fmt"
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

func TestAskUserToolResultKeepsResumeTaskID(t *testing.T) {
	h := &IMMessageHandler{}
	history := []agent.ConversationEntry{
		{Role: "assistant", ToolCalls: []map[string]string{{"id": "call-ask"}}},
	}
	out := h.handleAgentLoopAskUserToolResult(
		"user-1", "desktop", "publish",
		agent.AskUserResultMarker(&agent.AskUserRequest{
			Question: "solve captcha",
			Context:  "resume_task_id=bt-9 challenge",
			Options:  []string{"Continue"},
		}),
		false, "call-ask", nil, history, nil, nil, false,
	)
	if len(out.History) == 0 {
		t.Fatal("expected history tool result")
	}
	got := out.History[len(out.History)-1].Content
	if !strings.Contains(fmt.Sprint(got), "resume_task_id=bt-9") {
		t.Fatalf("history dropped resume_task_id: %#v", got)
	}
}

func TestConsumePendingAskUserAnswerKeepsContext(t *testing.T) {
	h := &IMMessageHandler{}
	entries := []agent.ConversationEntry{{Role: "user", Content: "publish"}}
	h.pendingAskUser.Store("user-1", &pendingAskUserState{
		Question:  "solve captcha",
		Context:   "resume_task_id=bt-9",
		History:   entries,
		Timestamp: time.Now(),
	})
	got, _, ok := h.consumePendingAskUserAnswer("user-1", "continue", entries)
	if !ok {
		t.Fatal("expected pending ask answer")
	}
	if !strings.Contains(got, "resume_task_id=bt-9") {
		t.Fatalf("continue hint dropped resume_task_id: %q", got)
	}
}
