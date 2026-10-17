//go:build windows

package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
)

func TestPickWailsWindowPrefersVisibleFrame(t *testing.T) {
	tops := []recoverWinInfo{
		{hwnd: 1, class: "wailsWindow", visible: false, w: 10, h: 10},
		{hwnd: 2, class: "wailsWindow", visible: true},
		{hwnd: 3, class: "STATIC", visible: true, w: 100, h: 100},
		{hwnd: 4, class: "wailsWindow", visible: true, w: 800, h: 600},
	}
	if got := pickWailsWindow(tops).hwnd; got != 4 {
		t.Fatalf("visible hwnd = %#x", got)
	}
	if got := pickWailsWindow([]recoverWinInfo{{hwnd: 1, class: "wailsWindow", w: 10, h: 10}}).hwnd; got != 1 {
		t.Fatalf("hidden hwnd = %#x", got)
	}
	if pickWailsWindow(nil).hwnd != 0 {
		t.Fatal("empty scan returned a window")
	}
}

func TestTaskbarBadgeDiscUsesIconGreen(t *testing.T) {
	// Center of a 16px disc is fully covered: straight RGB(24, 132, 128).
	if got := taskbarBadgeDiscPixel(8, 8, 16); got != 0xFF188480 {
		t.Fatalf("center = %#08x", got)
	}
	if got := taskbarBadgeDiscPixel(0, 0, 16); got != 0 {
		t.Fatalf("corner = %#08x", got)
	}
	rim := taskbarBadgeDiscPixel(0, 8, 16)
	if alpha := rim >> 24; alpha == 0 || alpha == 255 {
		t.Fatalf("rim alpha = %d", alpha)
	}
}

func TestTaskbarBadgeComposePixelKeepsDiscAlpha(t *testing.T) {
	const disc = uint32(0x80133B43)
	if got := taskbarBadgeComposePixel(disc, disc&^0xFF000000); got != disc {
		t.Fatalf("cleared alpha = %#08x", got)
	}
	if got := taskbarBadgeComposePixel(disc, 0); got != disc {
		t.Fatalf("wiped pixel = %#08x", got)
	}
	if got := taskbarBadgeComposePixel(disc, 0x00FFFFFF); got != 0xFFFFFFFF {
		t.Fatalf("glyph = %#08x", got)
	}
}

func TestTaskbarBadgeHiddenWindowKeepsTheCount(t *testing.T) {
	now := time.Now()
	phase, result, hold := taskbarBadgeShowGate(2, false, taskbarBadgeShowIdle, time.Time{}, now)
	if !hold || result != taskbarBadgeWindowHidden || phase != taskbarBadgeShowIdle {
		t.Fatalf("hidden window = phase %d result %d hold %v", phase, result, hold)
	}
	if _, _, hold := taskbarBadgeShowGate(2, true, taskbarBadgeShowIdle, time.Time{}, now); hold {
		t.Fatal("visible window held the badge")
	}
	if _, _, hold := taskbarBadgeShowGate(0, false, taskbarBadgeShowWaitShow, now, now); hold {
		t.Fatal("clearing the badge waited for a hidden window")
	}

	armed := now
	phase, result, hold = taskbarBadgeShowGate(2, true, taskbarBadgeShowWaitHide, armed, armed.Add(100*time.Millisecond))
	if !hold || phase != taskbarBadgeShowWaitHide || result != taskbarBadgeWindowHidden {
		t.Fatal("paint landed before the hide was observed")
	}
	phase, _, hold = taskbarBadgeShowGate(2, false, phase, armed, armed.Add(200*time.Millisecond))
	if !hold || phase != taskbarBadgeShowWaitShow {
		t.Fatal("hide did not arm the show replay")
	}
	phase, _, hold = taskbarBadgeShowGate(2, false, phase, armed, armed.Add(300*time.Millisecond))
	if !hold || phase != taskbarBadgeShowWaitShow {
		t.Fatal("still-hidden window painted")
	}
	phase, _, hold = taskbarBadgeShowGate(2, true, phase, armed, armed.Add(400*time.Millisecond))
	if hold || phase != taskbarBadgeShowIdle {
		t.Fatal("shown window did not paint")
	}

	phase, _, hold = taskbarBadgeShowGate(2, true, taskbarBadgeShowWaitHide, armed, armed.Add(taskbarBadgeShowGrace))
	if hold || phase != taskbarBadgeShowIdle {
		t.Fatal("show that beat the hide check never painted")
	}
}

func TestTaskbarBadgeNoWindowStopsWhenNothingToClear(t *testing.T) {
	if _, result := taskbarBadgeNoWindowResult(0, 0); result != taskbarBadgeApplied {
		t.Fatal("empty clear kept waiting")
	}
	icon, result := taskbarBadgeNoWindowResult(2, 0)
	if result != taskbarBadgeNoWindow || icon != 0 {
		t.Fatalf("missing window result = %d icon %#x", result, icon)
	}
}

func TestTaskbarBadgeRegisterTabWaitsUntilLastAttempt(t *testing.T) {
	if taskbarBadgeRegisterTab(0) || taskbarBadgeRegisterTab(taskbarBadgeOverlayTries-1) {
		t.Fatal("AddTab ran before the last overlay attempt")
	}
	if !taskbarBadgeRegisterTab(taskbarBadgeOverlayTries) {
		t.Fatal("register attempt did not call AddTab")
	}
	if taskbarBadgeRegisterTab(taskbarBadgeOverlayTries + 1) {
		t.Fatal("AddTab repeated after the register attempt")
	}
}

func TestTaskbarBadgeMaskMarksTransparentCorners(t *testing.T) {
	const size = 8
	stride := ((size + 15) / 16) * 2
	mask := make([]byte, stride*size)
	taskbarBadgeFillMask(mask, size, func(x, y int) bool {
		return x == 3 && y == 4
	})
	// Top-left of the icon is transparent. CreateBitmap stores that row last.
	top := mask[(size-1)*stride]
	if top&0x80 == 0 {
		t.Fatalf("top-left mask = %#x", top)
	}
	// The one opaque pixel is image (3, 4). Bottom-up row for y=4 is index 3.
	opaqueRow := mask[3*stride]
	if opaqueRow&(0x80>>3) != 0 {
		t.Fatalf("opaque pixel was marked transparent: %#x", opaqueRow)
	}
}

func TestTaskbarBadgeDescriptionMatchesBrand(t *testing.T) {
	got := taskbarBadgeDescriptionText()
	want := brand.Current().DisplayName
	if want == "" {
		want = "MaClaw"
	}
	if got != want {
		t.Fatalf("description = %q, brand = %q", got, want)
	}
}

func TestTaskbarBadgeSubclassCallbackCompiles(t *testing.T) {
	if taskbarBadgeSubclassProcPtr() == 0 {
		t.Fatal("subclass callback was not created")
	}
}

func TestTaskbarBadgeShowWindowMessage(t *testing.T) {
	const created = uintptr(0xC000)
	if got := taskbarBadgeClassifyWindowMessage(taskbarBadgeWmShowWindow, 0, 0, created); got != taskbarBadgeNoticeHidden {
		t.Fatalf("hide = %d", got)
	}
	if got := taskbarBadgeClassifyWindowMessage(taskbarBadgeWmShowWindow, 1, 0, created); got != taskbarBadgeNoticeShown {
		t.Fatalf("show = %d", got)
	}
	// lParam other than 0 is a parent zoom or minimize notice, not ShowWindow.
	if got := taskbarBadgeClassifyWindowMessage(taskbarBadgeWmShowWindow, 0, 1, created); got != taskbarBadgeNoticeNone {
		t.Fatalf("parent notice = %d", got)
	}
	if got := taskbarBadgeClassifyWindowMessage(0x0005, 0, 0, created); got != taskbarBadgeNoticeNone {
		t.Fatalf("size = %d", got)
	}
	if got := taskbarBadgeClassifyWindowMessage(created, 0, 0, created); got != taskbarBadgeNoticeButton {
		t.Fatalf("button = %d", got)
	}
	if got := taskbarBadgeClassifyWindowMessage(0, 0, 0, 0); got != taskbarBadgeNoticeNone {
		t.Fatalf("unregistered button = %d", got)
	}
}

func TestTaskbarBadgeFollowUpRepaintsAfterHide(t *testing.T) {
	if got := taskbarBadgeFollowUpRepaints(true, 2); got != taskbarBadgeShowRepaintCount || got < 1 {
		t.Fatalf("follow-ups = %d", got)
	}
	if taskbarBadgeFollowUpRepaints(true, 0) != 0 {
		t.Fatal("clear scheduled follow-ups")
	}
	if taskbarBadgeFollowUpRepaints(false, 2) != 0 {
		t.Fatal("ordinary update scheduled follow-ups")
	}
}

func TestTaskbarBadgeOverlayVtableIndex(t *testing.T) {
	if taskbarBadgeVtableHrInit != 3 || taskbarBadgeVtableAddTab != 4 {
		t.Fatalf("HrInit=%d AddTab=%d", taskbarBadgeVtableHrInit, taskbarBadgeVtableAddTab)
	}
	// Index 11 is RegisterTab. SetOverlayIcon is nine ITaskbarList3 slots later.
	if taskbarBadgeVtableSetOverlayIcon != 18 {
		t.Fatalf("SetOverlayIcon index = %d", taskbarBadgeVtableSetOverlayIcon)
	}
}

func TestTaskbarBadgeCandidateOrderPrefersWailsFrame(t *testing.T) {
	got := taskbarBadgeCandidateOrder(0x11, 0x22)
	if len(got) != 2 || got[0] != 0x11 || got[1] != 0x22 {
		t.Fatalf("order = %#x", got)
	}
	got = taskbarBadgeCandidateOrder(0x11, 0x11)
	if len(got) != 1 || got[0] != 0x11 {
		t.Fatalf("dedupe = %#x", got)
	}
	got = taskbarBadgeCandidateOrder(0, 0x22)
	if len(got) != 1 || got[0] != 0x22 {
		t.Fatalf("fallback = %#x", got)
	}
	if got := taskbarBadgeRootOwner(0); got != 0 {
		t.Fatalf("empty root = %#x", got)
	}
}

// Regression test for the AI-assistant GUI crash: the badge renderer used to
// resolve SetBkMode/SetTextColor from user32.dll, where those GDI functions do
// not exist, so the first badge push panicked syscall.LazyProc and killed the
// whole process. Rendered icons must come back non-zero on every call now,
// and repeated renders must not lose the procs' lookup cache.

func TestTaskbarBadgeRenderIconCreatesIcon(t *testing.T) {
	for _, count := range []int{1, 2, 100} {
		icon := taskbarBadgeRenderIcon(count)
		if icon == 0 {
			t.Fatalf("taskbarBadgeRenderIcon(%d) returned 0", count)
		}
		procBadgeDestroyIcon.Call(icon)
	}
	icon := taskbarBadgeRenderIcon(0)
	if icon != 0 {
		procBadgeDestroyIcon.Call(icon)
		t.Fatalf("taskbarBadgeRenderIcon(0) should return 0 (no badge)")
	}
}
