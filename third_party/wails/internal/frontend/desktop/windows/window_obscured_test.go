//go:build windows

package windows

import (
	"testing"

	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc/w32"
	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
)

func TestContentObscuredUpdate(t *testing.T) {
	if contentObscuredUpdate(w32.WM_SHOWWINDOW, 0) != obscureNow {
		t.Fatal("WM_SHOWWINDOW hide")
	}
	if contentObscuredUpdate(w32.WM_SHOWWINDOW, 1) != obscureClear {
		t.Fatal("WM_SHOWWINDOW show")
	}
	if contentObscuredUpdate(w32.WM_SYSCOMMAND, scMinimize) != obscureNow {
		t.Fatal("SC_MINIMIZE")
	}
	if contentObscuredUpdate(w32.WM_SYSCOMMAND, scMinimize|0x4) != obscureNow {
		t.Fatal("SC_MINIMIZE with internal bits")
	}
	if contentObscuredUpdate(w32.WM_SYSCOMMAND, scRestore) != obscureClear {
		t.Fatal("SC_RESTORE")
	}
	if contentObscuredUpdate(w32.WM_SYSCOMMAND, 0xF060) != obscureKeep {
		t.Fatal("SC_CLOSE is not a hide")
	}
	if contentObscuredUpdate(w32.WM_SIZE, uintptr(w32.SIZE_MINIMIZED)) != obscureNow {
		t.Fatal("SIZE_MINIMIZED")
	}
	if contentObscuredUpdate(w32.WM_SIZE, uintptr(w32.SIZE_RESTORED)) != obscureClearIfShown {
		t.Fatal("SIZE_RESTORED")
	}
	if contentObscuredUpdate(w32.WM_SIZE, uintptr(w32.SIZE_MAXIMIZED)) != obscureClearIfShown {
		t.Fatal("SIZE_MAXIMIZED")
	}
	if contentObscuredUpdate(w32.WM_MOVE, 0) != obscureKeep {
		t.Fatal("WM_MOVE")
	}

	w := &Window{}
	w.contentObscured.Store(true)
	var notices []bool
	w.onContentVisibility = func(shown bool, _ uint64) {
		notices = append(notices, shown)
	}
	if w.contentShown() {
		t.Fatal("obscured window is not shown")
	}
	w.noteContentObscured(w32.WM_SIZE, uintptr(w32.SIZE_RESTORED))
	if !w.contentObscured.Load() {
		t.Fatal("a restored size with no hwnd must not clear the hide")
	}
	if len(notices) != 0 {
		t.Fatal("a size that keeps the hide must not notify the page")
	}
	w.noteContentObscured(w32.WM_SHOWWINDOW, 0)
	if len(notices) != 0 {
		t.Fatal("hiding an already hidden window must not notify again")
	}
	w.noteContentObscured(w32.WM_SHOWWINDOW, 1)
	if w.contentObscured.Load() {
		t.Fatal("WM_SHOWWINDOW show clears the hide")
	}
	if len(notices) != 1 || !notices[0] {
		t.Fatalf("show notice = %v", notices)
	}
	w.noteContentObscured(w32.WM_SHOWWINDOW, 0)
	if len(notices) != 2 || notices[1] {
		t.Fatalf("hide notice = %v", notices)
	}
	w.noteContentObscured(w32.WM_SHOWWINDOW, 1)
	if w.contentObscured.Load() || !w.contentShown() {
		t.Fatal("a window with no handle and no hide flag stays shown")
	}
	var none *Window
	if !none.contentShown() {
		t.Fatal("nil window stays shown")
	}
}

func TestShowLeavesTheHideMarkWhenFullscreenCannotBeRead(t *testing.T) {
	prevPlayer := companionPlayer
	prevView := maclawcapture.Current()
	companionPlayer = nil
	maclawcapture.Bind(nil)
	t.Cleanup(func() {
		companionPlayer = prevPlayer
		maclawcapture.Bind(prevView)
		maclawplayer.ResetShowPlacement()
	})
	maclawplayer.ResetShowPlacement()
	maclawplayer.NoteFullscreenHidden()
	clearFullscreenHideIfStillUp()
	maclawplayer.SetFullscreenSource(true, false)
	visible, hideExit := maclawplayer.FullscreenNotifyVisible(false, true)
	if !visible || !hideExit {
		t.Fatal("a show with no live fullscreen read cleared the hide exit")
	}
}
