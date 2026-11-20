package botmgmt

import "testing"

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

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
