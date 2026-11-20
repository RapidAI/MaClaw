package accessibility

import "testing"

func TestWindowOwnedBySelfRejectsZero(t *testing.T) {
	if WindowOwnedBySelf(0) {
		t.Fatal("zero window id is not this process")
	}
	if NamedWindowID("") != 0 || NamedWindowID("   ") != 0 {
		t.Fatal("empty title has no window id")
	}
}

func TestFocusWindowEmptyTitle(t *testing.T) {
	if err := FocusWindow(""); err == nil {
		t.Fatal("expected error for empty title")
	}
	if err := FocusWindow("   "); err == nil {
		t.Fatal("expected error for whitespace title")
	}
}
