//go:build windows

package accessibility

import "testing"

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
