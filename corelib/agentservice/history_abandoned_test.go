package agentservice

import "testing"

func TestDropAbandonedUserTurnsKeepsOnlyAnsweredHistory(t *testing.T) {
	h := []Message{
		{ID: "u1", Role: MessageRoleUser, Content: "茶"},
		{ID: "a1", Role: MessageRoleAssistant, Content: "红茶"},
		{ID: "u2", Role: MessageRoleUser, Content: "查一下今天天气"},
		{ID: "u3", Role: MessageRoleUser, Content: "推荐一首歌"},
	}
	runs := []Run{
		{ID: "r1", UserMessageID: "u1", Status: RunStatusSucceeded, AssistantMessageID: "a1"},
		{ID: "r2", UserMessageID: "u2", Status: RunStatusCancelled},
		{ID: "r3", UserMessageID: "u3", Status: RunStatusRunning},
	}
	got := dropAbandonedUserTurns(h, runs, "u3")
	if len(got) != 3 || got[2].ID != "u3" || got[1].ID != "a1" {
		t.Fatalf("cancelled turn must not reach the model: %+v", got)
	}
	// A cancelled turn that was later retried and answered stays.
	runs = append(runs, Run{ID: "r4", UserMessageID: "u2", Status: RunStatusSucceeded})
	if got := dropAbandonedUserTurns(h, runs, "u3"); len(got) != 4 {
		t.Fatalf("answered retry dropped: %+v", got)
	}
	// The current message is never dropped, even if an earlier attempt failed.
	runs = []Run{{ID: "r5", UserMessageID: "u3", Status: RunStatusFailed}}
	if got := dropAbandonedUserTurns(h, runs, "u3"); len(got) != 4 {
		t.Fatalf("current turn dropped: %+v", got)
	}
}
