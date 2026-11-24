//go:build windows

package accessibility

import (
	"testing"
	"unsafe"
)

func TestGUIThreadInfoMatchesWindowsLayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 && unsafe.Sizeof(winGUIThreadInfo{}) != 72 {
		t.Fatalf("GUITHREADINFO size = %d", unsafe.Sizeof(winGUIThreadInfo{}))
	}
}

func TestWindowTextEmptyHandle(t *testing.T) {
	if windowText(0) != "" {
		t.Fatal("a missing window has no caption")
	}
}

func TestConsoleHostClassIsNotAnApplicationFrame(t *testing.T) {
	if !consoleHostClass("ConsoleWindowClass") || !consoleHostClass("PseudoConsoleWindow") {
		t.Fatal("a command prompt is not an application frame")
	}
	if consoleHostClass("Chrome_WidgetWin_0") || consoleHostClass("CASCADIA_HOSTING_WINDOW_CLASS") {
		t.Fatal("an application window and a terminal the person is using stay frames")
	}
}

func TestAppFrameEligibleSkipsToolAndCloakedWindows(t *testing.T) {
	if !appFrameEligible("微信", 0, false) {
		t.Fatal("a hidden main frame is still a frame")
	}
	if appFrameEligible("微信", wsExToolWindow, false) {
		t.Fatal("a tool window is not a frame")
	}
	if appFrameEligible("微信", 0, true) {
		t.Fatal("a cloaked window is not a frame")
	}
	if appFrameEligible("  ", 0, false) {
		t.Fatal("an empty title is not a frame")
	}
}

func TestHiddenFrameNeedsAppActivation(t *testing.T) {
	if !hiddenFrameNeedsAppActivation("蓝信", false, false) {
		t.Fatal("a declared single-instance tray app must use application activation")
	}
	for _, hint := range []string{"记事本", "微信", "unknown application"} {
		if hiddenFrameNeedsAppActivation(hint, false, false) {
			t.Fatalf("undeclared app %q would start another process", hint)
		}
	}
	for _, state := range [][2]bool{{true, false}, {false, true}, {true, true}} {
		if hiddenFrameNeedsAppActivation("蓝信", state[0], state[1]) {
			t.Fatalf("visible/iconic state %v must not start another activation", state)
		}
	}
}

func TestPickFrameUsesAHiddenFrame(t *testing.T) {
	hits := []visibleWindow{
		{title: "Program Manager", visible: true},
		{title: "dist - 文件资源管理器", visible: true},
		{title: "蓝信", visible: false},
	}
	got, ok := pickFrame("蓝信", hits)
	if !ok || got.title != "蓝信" || got.visible {
		t.Fatalf("hidden 蓝信 = %+v ok=%v", got, ok)
	}
	if _, ok := pickFrame("Program Manager", hits); !ok {
		t.Fatal("an ordinary visible title still matches")
	}
}
