package textutil

import "testing"

func TestSanitizeVisibleChatText(t *testing.T) {
	pua := string(rune(0xEB90))
	soh := string(rune(1))
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello kate", "hello kate"},
		{"reasoning sentinel between tokens", soh + "user" + soh + " (" + soh + "initiator m" + soh + "group" + soh + ")", "user (initiator mgroup)"},
		{"leading sentinel only", soh + "analyzing greeting", "analyzing greeting"},
		{"pua tofu between words", "I am " + pua + "Kate" + pua + " Miss", "I am Kate Miss"},
		{"mixed sentinel and pua", soh + "user" + pua + "hello", "userhello"},
		{"keeps newlines and tabs", "line1\n\tline2", "line1\n\tline2"},
		{"strips nul and c1", "visible" + string(rune(0)) + "text" + string(rune(0x85)) + "more", "visibletextmore"},
		{"strips replacement char", "bad" + string(rune(0xFFFD)) + "char", "badchar"},
		{"sentinel only", soh, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeVisibleChatText(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeVisibleChatText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestVisibleChatStreamDeltaDropsReasoningLane(t *testing.T) {
	soh := string(rune(1))
	pua := string(rune(0xEB90))
	if got := VisibleChatStreamDelta(soh + "user"); got != "" {
		t.Fatalf("reasoning lane = %q, want empty", got)
	}
	if got := VisibleChatStreamDelta("hello " + pua + "kate"); got != "hello kate" {
		t.Fatalf("visible pua = %q", got)
	}
	if got := VisibleChatStreamDelta("plain"); got != "plain" {
		t.Fatalf("plain rewritten: %q", got)
	}
}

func TestSanitizeVisibleChatTextIdempotent(t *testing.T) {
	in := string(rune(1)) + "user" + string(rune(0xEB90)) + "hello"
	once := SanitizeVisibleChatText(in)
	twice := SanitizeVisibleChatText(once)
	if once != twice {
		t.Fatalf("not idempotent: once=%q twice=%q", once, twice)
	}
	if once != "userhello" {
		t.Fatalf("unexpected once=%q", once)
	}
}

func TestSanitizeVisibleChatTextFastPathCleanText(t *testing.T) {
	in := "plain text\n### Heading"
	got := SanitizeVisibleChatText(in)
	if got != in {
		t.Fatalf("clean text rewritten: %q", got)
	}
}

func TestSanitizeVisibleChatTextKeepsAssembledLeadingSentinel(t *testing.T) {
	assembled := SanitizeVisibleChatText("\x01visible answer")
	if assembled != "visible answer" {
		t.Fatalf("assembled = %q, want kept after stripping sentinel", assembled)
	}
	if got := VisibleChatStreamDelta("\x01visible answer"); got != "" {
		t.Fatalf("stream delta = %q, want empty", got)
	}
}

func TestFirstVisibleChatText(t *testing.T) {
	if got := FirstVisibleChatText("\x01", "hello"); got != "hello" {
		t.Fatalf("fallback = %q", got)
	}
	if got := FirstVisibleChatText("\x01visible", "other"); got != "visible" {
		t.Fatalf("prefer first = %q", got)
	}
	if got := FirstVisibleChatText("\x01", "\x00"); got != "" {
		t.Fatalf("all empty = %q", got)
	}
	if got := FirstVisibleChatText("", "\x01", "keep"); got != "keep" {
		t.Fatalf("skip empty = %q", got)
	}
}
