package main

import "testing"

func TestAgentActivityIsScopedToAssistantOwner(t *testing.T) {
	store := NewAgentActivityStore()
	store.Update(&AgentActivity{Source: "gui", OwnerID: "desktop-user:D:/one", Task: "project one"})
	store.Update(&AgentActivity{Source: "gui", OwnerID: "desktop-user:D:/two", Task: "project two"})

	store.ClearForOwner("gui", "desktop-user:D:/one")
	if len(store.items) != 1 {
		t.Fatalf("activities after clearing owner one = %#v, want only owner two", store.items)
	}
	if activity := store.items[agentActivityKey("gui", "desktop-user:D:/two")]; activity == nil || activity.Task != "project two" {
		t.Fatalf("owner two activity = %#v, want preserved project two", activity)
	}
}

func TestStartAgentLoopActivityDoesNotInjectOtherSessionTask(t *testing.T) {
	store := NewAgentActivityStore()
	store.Update(&AgentActivity{Source: "im", OwnerID: "im:other", Task: "secret other task"})
	h := &IMMessageHandler{agentActivity: store}
	_, cleanup, prompt := h.startAgentLoopActivity("desktop-user:D:/one", "desktop", "project one", 5)
	defer cleanup()
	if prompt != "" {
		t.Fatalf("cross-session prompt = %q, want empty", prompt)
	}
	if activity := store.items[agentActivityKey("im", "im:other")]; activity == nil || activity.Task != "secret other task" {
		t.Fatalf("other owner activity = %#v, want preserved status only", activity)
	}
}
