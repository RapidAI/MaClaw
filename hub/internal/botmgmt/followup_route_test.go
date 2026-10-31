package botmgmt

import (
	"testing"
	"time"
)

func TestFollowUpRule(t *testing.T) {
	for text, want := range map[string]string{
		"需要图文并茂":                 "follow",
		"标题改成秋天的第一杯奶茶":           "follow",
		"不要用表情":                  "follow",
		"帮我在知乎也发一篇":              "new",
		"https://x.com/a 看看这个":     "new",
		"停下":                     "new",
		"今天天气怎么样":                "",
	} {
		if got := followUpRule(text); got != want {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
}

func TestLiveDelegateSubscription(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	at := now.Add(-10 * time.Minute).Format(time.RFC3339)
	notes := []BotCrossNote{
		{ID: "d1", Owner: "u", From: "main", Target: "b1", Kind: "delegate", RunID: "run_1", CreatedAt: at},
		{ID: "d2", Owner: "u", From: "main", Target: "b2", Kind: "delegate", RunID: "run_2", CreatedAt: now.Add(-5 * time.Minute).Format(time.RFC3339)},
		{ID: "r2", Owner: "u", From: "b2", Target: "main", Kind: "report", ReplyTo: "d2", Topic: "done", CreatedAt: now.Format(time.RFC3339)},
		{ID: "d3", Owner: "u", From: "main", Target: "b3", Kind: "delegate", RunID: "run_3", CreatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339)},
	}
	sub, ok := liveDelegateSubscription(notes, "u", "main", now)
	if !ok || sub.ID != "d1" {
		t.Fatalf("got %+v %v, want d1 (d2 closed, d3 stale)", sub, ok)
	}
	if _, ok := liveDelegateSubscription(notes, "u", "b1", now); ok {
		t.Fatal("b1 subscribed nothing")
	}
}
