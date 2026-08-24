package computeruse

import "testing"

func TestWindowTitlesMatch(t *testing.T) {
	if !WindowTitlesMatch("Untitled - Notepad", "*Untitled - Notepad") {
		t.Fatal("notepad dirty title should match")
	}
	if WindowTitlesMatch("WeChat", "Slack") {
		t.Fatal("distinct apps must not match")
	}
	if !WindowTitlesMatch("", "anything") {
		t.Fatal("empty is unknown and must not invalidate")
	}
}
