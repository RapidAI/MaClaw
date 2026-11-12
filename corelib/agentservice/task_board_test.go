package agentservice

import (
	"context"
	"strings"
	"testing"
)

func TestBotOfClientSessionKey(t *testing.T) {
	cases := map[string]string{
		"bot_p": "bot_p", "bot_p#t-k": "bot_p", "bot_p#xdel-1": "bot_p",
		"telegram:c1": "", "schedule:x": "", "discussion-1": "", "bot_p#misc": "",
	}
	for in, want := range cases {
		if got := botOfClientSessionKey(in); got != want {
			t.Fatalf("%q -> %q want %q", in, got, want)
		}
	}
}

func TestThreadTurnSeesSiblingBoardNotTranscripts(t *testing.T) {
	exec := &captureExecutor{}
	svc, principal, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()
	send := func(key, cmid, content string) {
		t.Helper()
		if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{ClientSessionKey: key, ClientMessageID: cmid, Content: content}); err != nil {
			t.Fatal(err)
		}
	}
	send("bot_p#t-a", "a1", "整理上周订单报表")
	send("bot_p#t-a", "a2", "SIBLING-DETAIL 把第三列改成金额")
	send("other_bot_x", "x1", "unrelated other bot")
	send("bot_q#t-z", "z1", "OTHER-BOT task")
	send("bot_p#t-b", "b1", "给供应商写邮件")
	hist := exec.req.History
	if len(hist) < 2 || hist[0].Metadata["task_board"] != "1" {
		t.Fatalf("no board at the front: %#v", hist)
	}
	board := hist[0].Content
	if !strings.Contains(board, "整理上周订单报表") || !strings.Contains(board, "succeeded") || !strings.Contains(board, "result: ok") {
		t.Fatalf("board = %q", board)
	}
	if strings.Contains(board, "OTHER-BOT") || strings.Contains(board, "unrelated") {
		t.Fatalf("another bot leaked: %q", board)
	}
	for _, msg := range hist {
		if strings.Contains(msg.Content, "SIBLING-DETAIL") {
			t.Fatalf("sibling transcript interleaved: %#v", hist)
		}
	}
	// Not written into the thread.
	listed, _ := svc.ListMessages(context.Background(), principal, inst.ID, exec.req.Session.ID, ListMessagesInput{})
	for _, msg := range listed {
		if msg.Metadata["task_board"] == "1" || strings.Contains(msg.Content, "Task board") {
			t.Fatalf("board stored: %#v", listed)
		}
	}
}

func TestTaskBoardIsBounded(t *testing.T) {
	var entries []taskBoardEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, taskBoardEntry{title: strings.Repeat("任", 80), status: "succeeded", result: strings.Repeat("果", 240)})
	}
	out := renderTaskBoard(entries)
	if n := len([]rune(out)); n > taskBoardMaxRunes {
		t.Fatalf("board %d runes", n)
	}
	exec := &captureExecutor{}
	svc, principal, inst := setupCaptureAgentService(t, exec)
	defer svc.Close()
	for i := 0; i < 10; i++ {
		key := "bot_p#t-" + string(rune('a'+i))
		if _, _, _, err := svc.SendMessage(context.Background(), principal, inst.ID, SendMessageInput{ClientSessionKey: key, ClientMessageID: key, Content: "task " + key}); err != nil {
			t.Fatal(err)
		}
	}
	got := svc.taskBoardFor(principal, inst.ID, exec.req.Session, svc.now())
	if len(got) != taskBoardMaxEntries {
		t.Fatalf("entries = %d", len(got))
	}
}
