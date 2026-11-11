package agentservice

import "testing"

// papers 2026-11-11: a measured task's final reply never reached the person
// because clients show only send_to_user.
func TestApplyTaskOutcomeMakesTheReportTheSentence(t *testing.T) {
	run := &Run{}
	msg := &Message{Content: "桌面始终未能唤醒，没有截图。", Metadata: map[string]string{TaskOutcomeKey: TaskOutcomeBlocked, TaskOutcomeNoticeKey: "这件没做成。"}}
	applyTaskOutcome(run, msg)
	if got := msg.Metadata["send_to_user"]; got != "这件没做成。\n桌面始终未能唤醒，没有截图。" {
		t.Fatalf("send_to_user %q", got)
	}
	said := &Message{Content: "全文", Metadata: map[string]string{TaskOutcomeKey: TaskOutcomeDone, "send_to_user": "已发布：链接"}}
	applyTaskOutcome(&Run{}, said)
	if said.Metadata["send_to_user"] != "已发布：链接" {
		t.Fatal("a sentence the run said stays")
	}
	ask := &Message{Content: "选哪个？", Metadata: map[string]string{TaskOutcomeKey: TaskOutcomeAwaitingUser}}
	applyTaskOutcome(&Run{}, ask)
	if ask.Metadata["send_to_user"] != "" {
		t.Fatal("a question keeps its own path")
	}
}
