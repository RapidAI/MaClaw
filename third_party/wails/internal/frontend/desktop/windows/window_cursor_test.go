//go:build windows

package windows

import (
	"testing"

	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc/w32"
)

// TestRestoreCursorDisplayCount checks the probe-and-undo sequence. Win32 has
// no cursor-count read, so a hidden count must end at zero with one show per
// decrement, and an already visible count must be left exactly as it was.
func TestRestoreCursorDisplayCount(t *testing.T) {
	cases := []struct {
		name         string
		start        int32
		wantCount    int32
		wantOriginal int32
		wantCalls    []bool
	}{
		{name: "hidden once", start: -1, wantCount: 0, wantOriginal: -1, wantCalls: []bool{true}},
		{name: "hidden three times", start: -3, wantCount: 0, wantOriginal: -3, wantCalls: []bool{true, true, true}},
		{name: "visible at zero", start: 0, wantCount: 0, wantOriginal: 0, wantCalls: []bool{true, false}},
		{name: "visible above zero", start: 2, wantCount: 2, wantOriginal: 2, wantCalls: []bool{true, false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			count := tc.start
			var calls []bool
			original := restoreCursorDisplayCountWith(func(show bool) int32 {
				calls = append(calls, show)
				if show {
					count++
				} else {
					count--
				}
				return count
			})
			if original != tc.wantOriginal {
				t.Fatalf("original = %d, want %d", original, tc.wantOriginal)
			}
			if count != tc.wantCount {
				t.Fatalf("count = %d, want %d", count, tc.wantCount)
			}
			if len(calls) != len(tc.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
			}
			for i := range calls {
				if calls[i] != tc.wantCalls[i] {
					t.Fatalf("calls = %v, want %v", calls, tc.wantCalls)
				}
			}
		})
	}
}

func TestCursorRestoreOnSize(t *testing.T) {
	const (
		sizeRestored  = 0
		sizeMinimized = 1
		sizeMaximized = 2
	)
	cases := []struct {
		name       string
		msg        uint32
		wparam     uintptr
		fullscreen bool
		want       bool
	}{
		{name: "maximize", msg: w32.WM_SIZE, wparam: sizeMaximized, want: true},
		{name: "fullscreen restore size", msg: w32.WM_SIZE, wparam: sizeRestored, fullscreen: true, want: true},
		{name: "ordinary resize", msg: w32.WM_SIZE, wparam: sizeRestored, want: false},
		{name: "minimize while fullscreen flag remains", msg: w32.WM_SIZE, wparam: sizeMinimized, fullscreen: true, want: false},
		{name: "other message", msg: w32.WM_MOVE, wparam: sizeMaximized, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cursorRestoreOnSize(tc.msg, tc.wparam, tc.fullscreen); got != tc.want {
				t.Fatalf("cursorRestoreOnSize() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCursorRestoreOnShow(t *testing.T) {
	cases := []struct {
		name   string
		msg    uint32
		wparam uintptr
		want   bool
	}{
		{name: "shown", msg: w32.WM_SHOWWINDOW, wparam: 1, want: true},
		{name: "hidden", msg: w32.WM_SHOWWINDOW, wparam: 0, want: false},
		{name: "other message", msg: w32.WM_SIZE, wparam: 1, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cursorRestoreOnShow(tc.msg, tc.wparam); got != tc.want {
				t.Fatalf("cursorRestoreOnShow() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCursorWatchWithoutHandle keeps the watch helpers safe on a window that
// was never realized: no handle means no risky state and no queued tick.
func TestCursorWatchWithoutHandle(t *testing.T) {
	var nilWindow *Window
	if nilWindow.cursorWatchActive() {
		t.Fatal("nil window must not report an active watch")
	}
	w := &Window{}
	if w.cursorWatchActive() {
		t.Fatal("a handle-less window must not report an active watch")
	}
	w.scheduleCursorWatch()
	if w.cursorWatchArmed.Load() {
		t.Fatal("an inactive watch must not arm a tick")
	}
}

// TestRestoreCursorDisplayCountStopsAtTheBound keeps an absurdly negative
// count from spinning forever. The helper gives up and leaves the count as it
// found it plus the bounded number of increments.
func TestRestoreCursorDisplayCountStopsAtTheBound(t *testing.T) {
	count := int32(-cursorRestoreMaxShow - 1000)
	calls := 0
	original := restoreCursorDisplayCountWith(func(show bool) int32 {
		calls++
		if show {
			count++
		} else {
			count--
		}
		return count
	})
	if original != -cursorRestoreMaxShow-1000 {
		t.Fatalf("original = %d, want %d", original, -cursorRestoreMaxShow-1000)
	}
	if calls > cursorRestoreMaxShow+1 {
		t.Fatalf("calls = %d, want at most %d", calls, cursorRestoreMaxShow+1)
	}
	if count >= 0 {
		t.Fatalf("count = %d, want it still hidden after the bound", count)
	}
}

// TestRestoreCursorDisplayCountDeepDeficit makes sure a deficit deepened by a
// hide on every mouse message is still raised all the way back to zero.
func TestRestoreCursorDisplayCountDeepDeficit(t *testing.T) {
	count := int32(-700)
	original := restoreCursorDisplayCountWith(func(show bool) int32 {
		if show {
			count++
		} else {
			count--
		}
		return count
	})
	if original != -700 {
		t.Fatalf("original = %d, want -700", original)
	}
	if count != 0 {
		t.Fatalf("count = %d, want 0", count)
	}
}

// TestCursorJogShould pins the episode policy: jogs are spent inside the
// budget, then only a pointer moving while still invisible earns more.
func TestCursorJogShould(t *testing.T) {
	cases := []struct {
		name   string
		streak int
		moved  bool
		want   bool
	}{
		{name: "first detection", streak: 0, moved: false, want: true},
		{name: "inside budget", streak: cursorJogBudget - 1, moved: false, want: true},
		{name: "budget spent, still", streak: cursorJogBudget, moved: false, want: false},
		{name: "past budget, still", streak: cursorJogBudget + 9, moved: false, want: false},
		{name: "budget spent, moving", streak: cursorJogBudget, moved: true, want: true},
		{name: "past budget, moving", streak: cursorJogBudget + 9, moved: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cursorJogShould(tc.streak, tc.moved); got != tc.want {
				t.Fatalf("cursorJogShould(%d, %v) = %v, want %v", tc.streak, tc.moved, got, tc.want)
			}
		})
	}
}

// TestIsSizingCursorHandle keeps the stuck-fullscreen-cursor detector limited
// to the shared resize cursors; an arrow or a null cursor must take their own
// paths instead.
func TestIsSizingCursorHandle(t *testing.T) {
	handles := map[uint16]w32.HCURSOR{
		w32.IDC_SIZENWSE: 0x1000d,
		w32.IDC_SIZENESW: 0x1000f,
		w32.IDC_SIZEWE:   0x10011,
		w32.IDC_SIZENS:   0x10013,
	}
	load := func(_ w32.HINSTANCE, id uint16) w32.HCURSOR {
		return handles[id]
	}
	for _, cursor := range []w32.HCURSOR{0x1000d, 0x1000f, 0x10011, 0x10013} {
		if !isSizingCursorHandleWith(cursor, load) {
			t.Fatalf("cursor %#x must be detected as a sizing cursor", cursor)
		}
	}
	for _, cursor := range []w32.HCURSOR{0, 0x10003, 0x10015, 0xdead} {
		if isSizingCursorHandleWith(cursor, load) {
			t.Fatalf("cursor %#x must not be detected as a sizing cursor", cursor)
		}
	}
}

// TestCursorMouseMoveLParam pins the signed low-word packing used for posted
// WM_MOUSEMOVE messages; negative client coordinates must wrap, not clamp.
func TestCursorMouseMoveLParam(t *testing.T) {
	if got, want := cursorMouseMoveLParam(0x1234, 0x5678), uintptr(0x1234|0x5678<<16); got != want {
		t.Fatalf("positive lparam = %#x, want %#x", got, want)
	}
	if got, want := cursorMouseMoveLParam(-1, -2), uintptr(0xffff|0xfffe<<16); got != want {
		t.Fatalf("negative lparam = %#x, want %#x", got, want)
	}
}

// TestCursorMouseMoveWParam pins the modifier replication: a synthetic move
// must carry the buttons and modifiers the user is actually holding, or a
// mid-drag page would see a false button-up state.
func TestCursorMouseMoveWParam(t *testing.T) {
	downSet := map[int]bool{}
	keyState := func(vk int) uint16 {
		if downSet[vk] {
			return 0x8000
		}
		return 0
	}
	if got := cursorMouseMoveWParamWith(keyState); got != 0 {
		t.Fatalf("no keys = %#x, want 0", got)
	}
	downSet[w32.VK_LBUTTON] = true
	if got := cursorMouseMoveWParamWith(keyState); got != mkLButton {
		t.Fatalf("lbutton = %#x, want %#x", got, mkLButton)
	}
	downSet[w32.VK_SHIFT] = true
	downSet[w32.VK_CONTROL] = true
	if got := cursorMouseMoveWParamWith(keyState); got != mkLButton|mkShift|mkControl {
		t.Fatalf("lbutton+shift+control = %#x, want %#x", got, mkLButton|mkShift|mkControl)
	}
	downSet[w32.VK_XBUTTON2] = true
	if got := cursorMouseMoveWParamWith(keyState); got != mkLButton|mkShift|mkControl|mkXButton2 {
		t.Fatalf("with xbutton2 = %#x, want %#x", got, mkLButton|mkShift|mkControl|mkXButton2)
	}
}
