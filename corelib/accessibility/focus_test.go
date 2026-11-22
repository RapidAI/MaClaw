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

func TestKeyboardTextTarget(t *testing.T) {
	cases := []struct {
		name string
		in   KeyboardTextTarget
		ok   bool
	}{
		{name: "no window", ok: false},
		{name: "foreground only", in: KeyboardTextTarget{Foreground: 1}, ok: false},
		{name: "caret on a browser frame", in: KeyboardTextTarget{Foreground: 1, Focus: 1, Caret: 9, FocusClass: "Chrome_WidgetWin_0"}, ok: true},
		{name: "browser frame", in: KeyboardTextTarget{Foreground: 1, Focus: 1, FocusClass: "Chrome_WidgetWin_0"}, ok: false},
		{name: "nested browser frame", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "Chrome_WidgetWin_1"}, ok: false},
		{name: "render widget", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "Chrome_RenderWidgetHostHWND"}, ok: true},
		{name: "edit", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "Edit"}, ok: true},
		{name: "qt window", in: KeyboardTextTarget{Foreground: 1, Focus: 1, FocusClass: "Qt51514QWindowIcon"}, ok: true},
		{name: "button", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "Button"}, ok: false},
		{name: "desktop", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "#32769"}, ok: false},
		{name: "menu", in: KeyboardTextTarget{Foreground: 1, Focus: 2, FocusClass: "#32768"}, ok: false},
		{name: "unread class", in: KeyboardTextTarget{Foreground: 1, Focus: 2}, ok: true},
	}
	for _, tc := range cases {
		if got := tc.in.AcceptsText(); got != tc.ok {
			t.Fatalf("%s: got %v", tc.name, got)
		}
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
