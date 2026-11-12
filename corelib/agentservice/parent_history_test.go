package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParentClientSessionKey(t *testing.T) {
	if got := parentClientSessionKey("bot_p#t-k-b"); got != "bot_p" {
		t.Fatalf("thread = %q", got)
	}
	if got := parentClientSessionKey("bot_p#xdel-1"); got != "bot_p" {
		t.Fatalf("delegate = %q", got)
	}
	if got := parentClientSessionKey("bot_p#xwake-2"); got != "bot_p" {
		t.Fatalf("wake = %q", got)
	}
	if got := parentClientSessionKey("bot_p"); got != "" {
		t.Fatalf("main = %q", got)
	}
	if got := parentClientSessionKey("telegram:contact-1"); got != "" {
		t.Fatalf("im key = %q", got)
	}
	if got := parentClientSessionKey("other#misc"); got != "" {
		t.Fatalf("unknown hash = %q", got)
	}
}

func TestBoundRecentConversationKeepsTheLastTurns(t *testing.T) {
	items := []Message{
		{Role: MessageRoleSystem, Content: "sys"},
		{Role: MessageRoleUser, Content: "one"},
		{Role: MessageRoleAssistant, Content: "a1"},
		{Role: MessageRoleUser, Content: "two"},
		{Role: MessageRoleAssistant, Content: "a2"},
		{Role: MessageRoleUser, Content: "three"},
		{Role: MessageRoleAssistant, Content: "a3"},
	}
	got := boundRecentConversation(items, 4, 8000)
	if len(got) != 4 || got[0].Content != "two" || got[3].Content != "a3" {
		t.Fatalf("got %#v", got)
	}
	// A tight rune cap drops the oldest turn as a pair.
	got = boundRecentConversation(items, 16, 12)
	if len(got) == 0 || got[0].Content == "one" || conversationRunes(got) > 12 {
		t.Fatalf("capped %#v runes=%d", got, conversationRunes(got))
	}
}

func historyContents(items []Message) []string {
	out := make([]string, 0, len(items))
	for _, msg := range items {
		out = append(out, msg.Content)
	}
	return out
}

func TestThreadRunSeesRecentMainHistory(t *testing.T) {
	exec := &captureExecutor{}
	svc, principal, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()

	if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "k-main-1",
		Content:          "先看一下上周的订单",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "k-main-2",
		Content:          "把缺货的标出来",
	}); err != nil {
		t.Fatal(err)
	}
	mainHistory := append([]string{}, historyContents(exec.req.History)...)
	if !containsAll(mainHistory, "先看一下上周的订单", "把缺货的标出来") {
		t.Fatalf("main history = %v", mainHistory)
	}

	if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		ClientSessionKey: "bot_p#t-k-b",
		ClientMessageID:  "k-b",
		Content:          "同步把退货也列出来",
	}); err != nil {
		t.Fatal(err)
	}
	got := historyContents(exec.req.History)
	if !containsAll(got, "先看一下上周的订单", "把缺货的标出来", "同步把退货也列出来") {
		t.Fatalf("thread history = %v", got)
	}
	if exec.req.History[0].Metadata["parent_context"] != "1" {
		t.Fatal("parent messages should be marked as context")
	}
	// The thread's own store does not receive a copy of main.
	thread := exec.req.Session
	listed, err := svc.ListMessages(context.Background(), principal, inst.ID, thread.ID, ListMessagesInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range listed {
		if strings.Contains(msg.Content, "先看一下上周的订单") || strings.Contains(msg.Content, "把缺货的标出来") {
			t.Fatalf("main history was written into the thread: %#v", listed)
		}
	}

	// A later turn in main (a sibling after this thread started) must not
	// interleave into a follow-up on the thread.
	if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		ClientSessionKey: "bot_p",
		ClientMessageID:  "k-main-3",
		Content:          "sibling work in main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{
		ClientSessionKey: "bot_p#t-k-b",
		ClientMessageID:  "k-b-2",
		Content:          "把退货按日期排",
	}); err != nil {
		t.Fatal(err)
	}
	got = historyContents(exec.req.History)
	if containsAll(got, "sibling work in main") {
		t.Fatalf("sibling main turn leaked into the thread: %v", got)
	}
	if !containsAll(got, "先看一下上周的订单", "把缺货的标出来", "同步把退货也列出来", "把退货按日期排") {
		t.Fatalf("follow-up history = %v", got)
	}
}

func containsAll(items []string, want ...string) bool {
	have := map[string]bool{}
	for _, item := range items {
		have[item] = true
	}
	for _, w := range want {
		if !have[w] {
			return false
		}
	}
	return true
}

func TestMessagesBeforeExcludesLaterTurns(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)
	items := []Message{
		{Content: "old", CreatedAt: t0.Add(-time.Minute)},
		{Content: "same", CreatedAt: t0},
		{Content: "new", CreatedAt: t0.Add(time.Second)},
	}
	got := messagesBefore(items, t0)
	if len(got) != 1 || got[0].Content != "old" {
		t.Fatalf("got %#v", got)
	}
}
