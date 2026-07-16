package accessibility

import "testing"

func TestFocusWindowEmptyTitle(t *testing.T) {
	if err := FocusWindow(""); err == nil {
		t.Fatal("expected error for empty title")
	}
	if err := FocusWindow("   "); err == nil {
		t.Fatal("expected error for whitespace title")
	}
}
