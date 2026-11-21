package botmgmt

import (
	"strings"
	"testing"
)

func TestDesktopQueuedAckNamesTheCommand(t *testing.T) {
	// A normal command is quoted and clipped with an ellipsis only when cut.
	got := desktopQueuedAck("北京天气，生成pdf")
	want := "收到「北京天气，生成pdf」，先排着，忙完就接着做。"
	if got != want {
		t.Fatalf("short command = %q, want %q", got, want)
	}

	// A long command is clipped to 24 runes and gets a trailing ellipsis.
	long := desktopQueuedAck("查一下最近一周的北京天气，然后生成一份带图表的pdf报告发到群里")
	if len([]rune(long)) <= 0 {
		t.Fatalf("long command produced empty ack: %q", long)
	}
	if !containsRune(long, '…') {
		t.Fatalf("clipped command missing ellipsis: %q", long)
	}

	// Empty content falls back to the neutral line.
	if got := desktopQueuedAck("   "); got != "收到，这条先排着，忙完就接着做。" {
		t.Fatalf("empty command = %q", got)
	}
}

// TestDesktopStartedAckDoesNotQueue guards the admit reply: this turn has
// already started, so its first line must not talk about waiting in a line.
// A failure reply right after it would otherwise contradict it.
func TestDesktopStartedAckDoesNotQueue(t *testing.T) {
	got := desktopStartedAck("北京天气，生成pdf")
	want := "收到「北京天气，生成pdf」，这就开始做。"
	if got != want {
		t.Fatalf("started ack = %q, want %q", got, want)
	}
	for _, phrase := range []string{"排着", "忙完"} {
		if containsText(got, phrase) {
			t.Fatalf("started ack still queues (%s): %q", phrase, got)
		}
	}
	long := desktopStartedAck("查一下最近一周的北京天气，然后生成一份带图表的pdf报告发到群里")
	if !containsRune(long, '…') {
		t.Fatalf("clipped command missing ellipsis: %q", long)
	}
	if got := desktopStartedAck("   "); got != "收到，这就开始做。" {
		t.Fatalf("empty command = %q", got)
	}
}

func containsText(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && strings.Contains(s, sub)
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
