//go:build windows

package windows

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"

	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/win32"
	"github.com/wailsapp/wails/v2/internal/system/operatingsystem"

	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc"
	"github.com/wailsapp/wails/v2/internal/frontend/desktop/windows/winc/w32"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/options"
	winoptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

type Window struct {
	winc.Form
	frontendOptions                          *options.App
	applicationMenu                          *menu.Menu
	minWidth, minHeight, maxWidth, maxHeight int
	versionInfo                              *operatingsystem.WindowsVersionInfo
	isDarkMode                               bool
	isActive                                 bool
	hasBeenShown                             bool

	// Theme
	theme        winoptions.Theme
	themeChanged bool

	framelessWithDecorations bool

	OnSuspend func()
	OnResume  func()

	chromium *edge.Chromium

	// cursorRestoreArmed is set while a delayed cursor restore is waiting.
	// Maximise and fullscreen can post several size messages; one follow-up
	// is enough to catch a hide that lands after the first restore.
	cursorRestoreArmed atomic.Bool

	// cursorWatchArmed is set while a sustained-watch tick is queued. A shown
	// window re-arms it on every tick, so a hide that lands after the early
	// posts still gets answered; the chain stops when the window is hidden.
	cursorWatchArmed atomic.Bool

	// cursorHiddenStreak counts consecutive watch ticks that found the
	// cursor hidden or NULL over this window; it bounds how many recovery
	// jogs one detection episode may spend. The position fields remember
	// the last pointer sample in that state so a pointer still moving while
	// invisible — the stuck signature — keeps the jogs going. cursorJogNanos
	// is the last jog's timestamp; the burst of ticks a jog posts must not
	// consume the budget. Touched only on the window procedure thread.
	cursorHiddenStreak int
	cursorHiddenSeen   bool
	cursorHiddenX      int
	cursorHiddenY      int
	cursorJogNanos     int64

	// isMinimizing indicates whether the window is currently being minimized
	// 标识窗口是否处于最小化状态,用于解决最小化/恢复时的闪屏问题
	// This flag is used to prevent unnecessary redraws during minimize/restore transitions for frameless windows
	// 此标志用于防止无边框窗口在最小化/恢复过程中的不必要重绘
	// Reference: https://github.com/wailsapp/wails/issues/3951
	isMinimizing bool

	// contentObscured is set from the window procedure before DefWindowProc
	// hides or minimizes. The player can drop its fullscreen element inside
	// that call, while Win32 still reports the window visible and the page
	// has not yet flipped visibilityState. Place reads it from a binding
	// goroutine, so it is atomic.
	contentObscured atomic.Bool
	// visibilityGen advances every time contentObscured changes. The page
	// notice is posted after this procedure returns, and a notice that is no
	// longer the latest generation is dropped.
	visibilityGen atomic.Uint64
	// onContentVisibility reports that change. shown is the new state.
	// The callback runs on the window procedure thread and must not wait for
	// the UI thread. A nil callback leaves the flag as the only record.
	onContentVisibility func(shown bool, gen uint64)
	// releaseFullscreenOnShow asks WndProc to run afterShowFullscreen once
	// DefWindowProc for this show message has returned. The fullscreen exit
	// that belongs to a hide may run inside that call, or after it returns.
	releaseFullscreenOnShow bool
	// afterShowFullscreen runs after that DefWindowProc. It drops a hide-owned
	// fullscreen exit only when a live ContainsFullScreenElement read says
	// the element is still up, so a later reader exit still stops the note.
	afterShowFullscreen func()
}

// WM_SYSCOMMAND stores internal bits in the low nibble. 0xFFF0 is the command.
const (
	scMinimize = 0xF020
	scRestore  = 0xF120
)

// wmKeepCursorVisible is posted after a size, show, or controller-bounds
// change that can hide the pointer, and again by the sustained watch while
// the window is shown. Two distinct WebView2 failures are answered on each
// tick: a ShowCursor(FALSE) whose matching ShowCursor(TRUE) is skipped (the
// display count stays negative), and a hidden/NULL cursor the browser
// process leaks across controllers (the count is fine; the cursor object is
// gone — answered by jogging the controllers, not by ShowCursor). The
// posted handler runs after the triggering message returns, on the UI
// thread.
const wmKeepCursorVisible = w32.WM_APP + 73

// cursorWatchInterval paces the sustained check. A one-shot retry loses to a
// hide that lands after it, and the stuck hidden cursor does not recover on
// its own — the user's manual window restore is what clears it, so the watch
// replays that signal once a second until the window is hidden or the
// cursor is back.
const cursorWatchInterval = time.Second

type obscureUpdate int

const (
	obscureKeep obscureUpdate = iota
	obscureNow
	obscureClear
	obscureClearIfShown
)

// contentObscuredUpdate classifies a window message that is about to be
// dispatched. Hide and minimize are recorded before DefWindowProc so a
// fullscreen exit nested inside that call is still the window going away.
func contentObscuredUpdate(msg uint32, wparam uintptr) obscureUpdate {
	switch msg {
	case w32.WM_SHOWWINDOW:
		if wparam == 0 {
			return obscureNow
		}
		return obscureClear
	case w32.WM_SYSCOMMAND:
		switch wparam & 0xFFF0 {
		case scMinimize:
			return obscureNow
		case scRestore:
			return obscureClear
		}
	case w32.WM_SIZE:
		switch wparam {
		case uintptr(w32.SIZE_MINIMIZED):
			return obscureNow
		case uintptr(w32.SIZE_RESTORED), uintptr(w32.SIZE_MAXIMIZED):
			return obscureClearIfShown
		}
	}
	return obscureKeep
}

func (w *Window) noteContentObscured(msg uint32, wparam uintptr) {
	if w == nil {
		return
	}
	prev := w.contentObscured.Load()
	switch contentObscuredUpdate(msg, wparam) {
	case obscureNow:
		w.contentObscured.Store(true)
	case obscureClear:
		w.contentObscured.Store(false)
	case obscureClearIfShown:
		// A restored or maximized size also arrives for an ordinary resize.
		// Clear only once Win32 already shows the window, so a hide that has
		// not cleared WS_VISIBLE yet keeps the flag.
		if w.Handle() != 0 && !w.IsMinimised() && w.IsVisible() {
			w.contentObscured.Store(false)
		}
	}
	next := w.contentObscured.Load()
	if prev == next || w.onContentVisibility == nil {
		return
	}
	// The page hears the same transition Place already reads. Posting the
	// script from this call would run it before DefWindowProc returns.
	w.onContentVisibility(!next, w.visibilityGen.Add(1))
}

// contentShown is false when this window is hiding, minimized, or already not
// visible. A nil window or a window with no handle stays "shown": there is no
// Win32 hide to attribute a fullscreen exit to.
func (w *Window) contentShown() bool {
	if w == nil {
		return true
	}
	if w.contentObscured.Load() {
		return false
	}
	if w.Handle() == 0 {
		return true
	}
	return !w.IsMinimised() && w.IsVisible()
}

// cursorRestoreLogNanos throttles the recovery log. The sustained watch calls
// the restore every second while a stuck state holds; without a cap a count
// that keeps getting re-hidden would write a line on every tick.
var cursorRestoreLogNanos atomic.Int64

const cursorRestoreLogInterval = int64(15 * time.Second)

// restoreCursorDisplayCount raises a cursor display count that WebView2 left
// below zero. Runtime 152-156 hide the pointer for the Windows "Hide pointer
// while typing" setting, but can skip the matching ShowCursor(TRUE). The
// count then stays negative across lock/unlock and window hide/show: the
// pointer is invisible over this window while clicks and typing still work.
// There is no API to read the count, so the probe increment is undone when
// the cursor was already visible; a negative count is raised to zero. The
// caller must be the window procedure: the count belongs to this UI thread,
// which is also where WebView2 updates the cursor.
func restoreCursorDisplayCount() {
	if original := restoreCursorDisplayCountWith(w32.ShowCursor); original < 0 {
		now := time.Now().UnixNano()
		if last := cursorRestoreLogNanos.Load(); now-last < cursorRestoreLogInterval || !cursorRestoreLogNanos.CompareAndSwap(last, now) {
			return
		}
		log.Printf("[cursor] display count was %d; raised toward zero", original)
	}
}

// cursorRestoreOnSize reports a size change that can leave the pointer hidden:
// OS maximise, or fullscreen (which sizes the popup without WS_MAXIMIZE).
// Minimise is excluded so a hidden window does not touch the cursor count.
// The restore back to normal is covered by the watch's in-flight tick and by
// WebView2's own paired show on the resize back.
func cursorRestoreOnSize(msg uint32, wparam uintptr, fullscreen bool) bool {
	if msg != w32.WM_SIZE || wparam == uintptr(w32.SIZE_MINIMIZED) {
		return false
	}
	return wparam == uintptr(w32.SIZE_MAXIMIZED) || fullscreen
}

// cursorRestoreOnShow reports a show message that can also surface a hidden
// pointer: the stuck count survives hide/show, and a re-shown window gets no
// activation of its own.
func cursorRestoreOnShow(msg uint32, wparam uintptr) bool {
	return msg == w32.WM_SHOWWINDOW && wparam != 0
}

// postCursorRestore queues restoreCursorDisplayCount on the UI thread, after
// the transition that may have hidden the pointer. Invoke would run inline
// on that thread and lose the race to WebView2's hide.
func (w *Window) postCursorRestore() {
	if w == nil || w.Handle() == 0 {
		return
	}
	hwnd := w.Handle()
	w32.PostMessage(hwnd, wmKeepCursorVisible, 0, 0)
	// WebView2 sometimes hides the pointer again on a message posted after
	// the size change. One later restore covers that. A further call while
	// this wait is in flight does not start another, and ShowCursor on an
	// already-visible count is undone.
	if !w.cursorRestoreArmed.CompareAndSwap(false, true) {
		return
	}
	time.AfterFunc(200*time.Millisecond, func() {
		w.cursorRestoreArmed.Store(false)
		w32.PostMessage(hwnd, wmKeepCursorVisible, 0, 0)
	})
}

// cursorWatchActive reports whether the cursor watch should keep ticking.
// The WebView2 hidden-cursor leak is not confined to the maximized or
// fullscreen state — a companion with the second controller mounted can
// carry a NULL cursor in a normal window too — so the watch runs whenever
// the window is shown. A hidden or minimized window drops it; the next show
// posts a restore of its own.
func (w *Window) cursorWatchActive() bool {
	return w != nil && w.Handle() != 0 && w.contentShown()
}

// scheduleCursorWatch re-arms the watch while the window is shown. The timer
// posts back onto this window's procedure, so each tick runs on the thread
// that owns the cursor count; when the window is hidden the handler stops
// re-arming and the chain ends on the next tick.
func (w *Window) scheduleCursorWatch() {
	if !w.cursorWatchActive() {
		return
	}
	if !w.cursorWatchArmed.CompareAndSwap(false, true) {
		return
	}
	hwnd := w.Handle()
	time.AfterFunc(cursorWatchInterval, func() {
		w.cursorWatchArmed.Store(false)
		w32.PostMessage(hwnd, wmKeepCursorVisible, 0, 0)
	})
}

// cursorJogLogNanos throttles the hidden-cursor log separately from the
// display-count log: a stuck WebView2 cursor is detected on every tick, so
// without a cap each detection would write a line.
var cursorJogLogNanos atomic.Int64

// cursorJogBudget bounds the jogs for one detection episode. A stuck state
// either clears on the first jogs or does not; a legitimate hide (typing,
// a page's cursor:none) must not be jogged every second forever.
const cursorJogBudget = 3

// cursorJogMinInterval bounds the jog cadence. A jog itself posts restore
// ticks through the player's parentMoved, and those extra handler runs must
// not burn the episode budget in a millisecond burst — the jogs are meant
// to be spread across the seconds the stuck state takes to settle.
const cursorJogMinInterval = int64(900 * time.Millisecond)

// checkWebViewCursorHidden detects a hidden or NULL cursor while the pointer
// is over this window — the state the WebView2 browser process can leak
// across controllers — and jogs the controllers into re-evaluating it. The
// display count stays healthy in that state, so ShowCursor cannot reach it;
// SetCursor from this process cannot either, because the cursor belongs to
// the thread that owns the window under the pointer (the msedgewebview2
// renderer). After the first few jogs the watch re-jogs only when the
// pointer moved while invisible — the signature of the stuck state — so a
// stationary legitimate hide is left alone. Runs on the window procedure
// thread.
func (w *Window) checkWebViewCursorHidden() {
	var info w32.CURSORINFO
	if !w32.GetCursorInfo(&info) {
		w.clearCursorHidden()
		return
	}
	nullCursor := info.HCursor == 0
	hidden := info.Flags&w32.CURSOR_SHOWING == 0
	if !nullCursor && !hidden {
		w.clearCursorHidden()
		return
	}
	x, y := int(info.PtScreenPos.X), int(info.PtScreenPos.Y)
	if !w.pointOverWindow(x, y) {
		w.clearCursorHidden()
		return
	}
	moved := w.cursorHiddenSeen && (w.cursorHiddenX != x || w.cursorHiddenY != y)
	w.cursorHiddenSeen = true
	w.cursorHiddenX, w.cursorHiddenY = x, y
	now := time.Now().UnixNano()
	if !cursorJogShould(w.cursorHiddenStreak, moved) || now-w.cursorJogNanos < cursorJogMinInterval {
		return
	}
	w.cursorHiddenStreak++
	w.cursorJogNanos = now
	if last := cursorJogLogNanos.Load(); now-last >= cursorRestoreLogInterval && cursorJogLogNanos.CompareAndSwap(last, now) {
		log.Printf("[cursor] hidden over window at %d,%d (null=%v); jogging webviews", x, y, nullCursor)
	}
	w.jogWebViewCursor()
}

// cursorJogShould reports whether a hidden-cursor detection tick should jog
// the controllers. Within the episode budget every detection jogs; past it,
// only a pointer that moved while still invisible — the stuck signature —
// earns another jog.
func cursorJogShould(streak int, moved bool) bool {
	return streak < cursorJogBudget || moved
}

// clearCursorHidden resets the hidden-cursor episode so the next detection
// starts a fresh jog budget.
func (w *Window) clearCursorHidden() {
	w.cursorHiddenStreak = 0
	w.cursorHiddenSeen = false
	w.cursorJogNanos = 0
}

// pointOverWindow reports whether a screen point lands on this window's
// tree — the app's own WebView surfaces included.
func (w *Window) pointOverWindow(x, y int) bool {
	hit := w32.WindowFromPoint(w32.POINT{X: int32(x), Y: int32(y)})
	return hit != 0 && w32.GetAncestor(hit, w32.GA_ROOT) == w.Handle()
}

// jogWebViewCursor replays what a manual window restore runs on the WebView2
// controllers — Frontend.refitWebViewOnSize's Notify + Resize sequence,
// which is the action that clears the browser process's stuck hidden cursor
// for the user. The controllers only re-evaluate their state; a legitimate
// hide (typing, a page's cursor:none) is re-applied, never forced visible.
// Resizing the app WebView raises its window over the player surface, so the
// player is restacked after it.
func (w *Window) jogWebViewCursor() {
	if w.chromium != nil {
		_ = w.chromium.NotifyParentWindowPositionChanged()
		w.chromium.Resize()
	}
	notifyCompanionPlayerParentMoved()
	restackCompanionPlayer()
}

// restoreCursorDisplayCountWith is restoreCursorDisplayCount with the Win32
// call injected, so the probe-and-undo sequence is testable. showCursor must
// increment on true and decrement on false, returning the new count. The
// return value is the count before the probe, so the caller can log a real
// recovery without changing the helper's behavior.
// cursorRestoreMaxShow bounds the recovery loop. A hide repeated on every
// mouse message can push the deficit far below -32 before the first restore,
// so the bound is generous; it exists only to keep a non-incrementing
// ShowCursor from spinning forever.
const cursorRestoreMaxShow = 4096

func restoreCursorDisplayCountWith(showCursor func(bool) int32) int32 {
	count := showCursor(true)
	original := count - 1
	if count >= 0 {
		if count > 0 {
			showCursor(false)
		}
		return original
	}
	// Repeated hides can leave the count more than one below zero.
	for i := 0; i < cursorRestoreMaxShow && count < 0; i++ {
		count = showCursor(true)
	}
	return original
}

func NewWindow(parent winc.Controller, appoptions *options.App, versionInfo *operatingsystem.WindowsVersionInfo, chromium *edge.Chromium) *Window {
	windowsOptions := appoptions.Windows

	result := &Window{
		frontendOptions: appoptions,
		minHeight:       appoptions.MinHeight,
		minWidth:        appoptions.MinWidth,
		maxHeight:       appoptions.MaxHeight,
		maxWidth:        appoptions.MaxWidth,
		versionInfo:     versionInfo,
		isActive:        true,
		themeChanged:    true,
		chromium:        chromium,

		framelessWithDecorations: appoptions.Frameless && (windowsOptions == nil || !windowsOptions.DisableFramelessWindowDecorations),
	}
	result.SetIsForm(true)

	var exStyle int
	if windowsOptions != nil {
		exStyle = w32.WS_EX_CONTROLPARENT | w32.WS_EX_APPWINDOW
		if windowsOptions.WindowIsTranslucent {
			exStyle |= w32.WS_EX_NOREDIRECTIONBITMAP
		}
	}
	if appoptions.AlwaysOnTop {
		exStyle |= w32.WS_EX_TOPMOST
	}

	var dwStyle = w32.WS_OVERLAPPEDWINDOW

	windowClassName := "wailsWindow"
	if windowsOptions != nil && windowsOptions.WindowClassName != "" {
		windowClassName = windowsOptions.WindowClassName
	}

	winc.RegClassOnlyOnce(windowClassName)
	handle := winc.CreateWindow(windowClassName, parent, uint(exStyle), uint(dwStyle))
	result.SetHandle(handle)
	// Lock/unlock does not deactivate this window, so the unlock notification
	// is what restores a cursor count WebView2 left below zero.
	registerSessionNotification(uintptr(handle))
	winc.RegMsgHandler(result)
	result.SetParent(parent)

	loadIcon := true
	if windowsOptions != nil && windowsOptions.DisableWindowIcon == true {
		loadIcon = false
	}
	if loadIcon {
		if ico, err := winc.NewIconFromResource(winc.GetAppInstance(), uint16(winc.AppIconID)); err == nil {
			result.SetIcon(0, ico)
		}
	}

	if appoptions.BackgroundColour != nil {
		win32.SetBackgroundColour(result.Handle(), appoptions.BackgroundColour.R, appoptions.BackgroundColour.G, appoptions.BackgroundColour.B)
	}

	if windowsOptions != nil {
		result.theme = windowsOptions.Theme
	} else {
		result.theme = winoptions.SystemDefault
	}

	result.SetSize(appoptions.Width, appoptions.Height)
	result.SetText(appoptions.Title)
	result.EnableSizable(!appoptions.DisableResize)
	if !appoptions.Fullscreen {
		result.EnableMaxButton(!appoptions.DisableResize)
		result.SetMinSize(appoptions.MinWidth, appoptions.MinHeight)
		result.SetMaxSize(appoptions.MaxWidth, appoptions.MaxHeight)
	}

	result.UpdateTheme()

	if windowsOptions != nil {
		result.OnSuspend = windowsOptions.OnSuspend
		result.OnResume = windowsOptions.OnResume
		if windowsOptions.WindowIsTranslucent {
			if !win32.SupportsBackdropTypes() {
				result.SetTranslucentBackground()
			} else {
				win32.EnableTranslucency(result.Handle(), win32.BackdropType(windowsOptions.BackdropType))
			}
		}

		if windowsOptions.ContentProtection {
			w32.SetWindowDisplayAffinity(result.Handle(), w32.WDA_EXCLUDEFROMCAPTURE)
		}

		if windowsOptions.DisableWindowIcon {
			result.DisableIcon()
		}
	}

	// Dlg forces display of focus rectangles, as soon as the user starts to type.
	w32.SendMessage(result.Handle(), w32.WM_CHANGEUISTATE, w32.UIS_INITIALIZE, 0)

	result.SetFont(winc.DefaultFont)

	if appoptions.Menu != nil {
		result.SetApplicationMenu(appoptions.Menu)
	}

	return result
}

func (w *Window) Fullscreen() {
	if w.Form.IsFullScreen() {
		return
	}
	if w.framelessWithDecorations {
		win32.ExtendFrameIntoClientArea(w.Handle(), false)
	}
	w.Form.SetMaxSize(0, 0)
	w.Form.SetMinSize(0, 0)
	w.Form.Fullscreen()
	w.postCursorRestore()
}

func (w *Window) UnFullscreen() {
	if !w.Form.IsFullScreen() {
		return
	}
	if w.framelessWithDecorations {
		win32.ExtendFrameIntoClientArea(w.Handle(), true)
	}
	w.Form.UnFullscreen()
	w.SetMinSize(w.minWidth, w.minHeight)
	w.SetMaxSize(w.maxWidth, w.maxHeight)
	w.postCursorRestore()
}

func (w *Window) Restore() {
	if w.Form.IsFullScreen() {
		w.UnFullscreen()
	} else {
		w.Form.Restore()
	}
}

func (w *Window) SetMinSize(minWidth int, minHeight int) {
	w.minWidth = minWidth
	w.minHeight = minHeight
	w.Form.SetMinSize(minWidth, minHeight)
}

func (w *Window) SetMaxSize(maxWidth int, maxHeight int) {
	w.maxWidth = maxWidth
	w.maxHeight = maxHeight
	w.Form.SetMaxSize(maxWidth, maxHeight)
}

func (w *Window) IsVisible() bool {
	return win32.IsVisible(w.Handle())
}

func (w *Window) WndProc(msg uint32, wparam, lparam uintptr) uintptr {
	// After this call returns, including DefWindowProc. A maximise,
	// fullscreen, or show transition can hide the pointer inside that call.
	defer func() {
		if cursorRestoreOnSize(msg, wparam, w.Form.IsFullScreen()) || cursorRestoreOnShow(msg, wparam) {
			w.postCursorRestore()
		}
	}()
	// Before DefWindowProc. ShowWindow and SC_MINIMIZE nest the player's
	// fullscreen exit inside this call, ahead of the page visibility event.
	w.noteContentObscured(msg, wparam)
	// Captured after the show callback set it. A nested procedure must not
	// take this message's check, and an early return still runs it.
	releaseFullscreen := w.releaseFullscreenOnShow
	w.releaseFullscreenOnShow = false
	defer func() {
		if releaseFullscreen && w.afterShowFullscreen != nil {
			w.afterShowFullscreen()
		}
	}()

	switch msg {
	case wmKeepCursorVisible:
		restoreCursorDisplayCount()
		w.checkWebViewCursorHidden()
		w.scheduleCursorWatch()
		return 0
	case win32.WM_POWERBROADCAST:
		switch wparam {
		case win32.PBT_APMSUSPEND:
			if w.OnSuspend != nil {
				w.OnSuspend()
			}
		case win32.PBT_APMRESUMEAUTOMATIC:
			if w.OnResume != nil {
				w.OnResume()
			}
			restoreCursorDisplayCount()
		}
	case w32.WM_SETTINGCHANGE:
		settingChanged := w32.UTF16PtrToString((*uint16)(unsafe.Pointer(lparam)))
		if settingChanged == "ImmersiveColorSet" {
			w.themeChanged = true
			w.UpdateTheme()
		}
		return 0
	case w32.WM_NCLBUTTONDOWN:
		w32.SetFocus(w.Handle())
	case w32.WM_MOVE, w32.WM_MOVING:
		w.chromium.NotifyParentWindowPositionChanged()
		notifyCompanionPlayerParentMoved()
	case w32.WM_ACTIVATE:
		//if !w.frontendOptions.Frameless {
		w.themeChanged = true
		if int(wparam) == w32.WA_INACTIVE {
			w.isActive = false
			w.UpdateTheme()
		} else {
			w.isActive = true
			w.UpdateTheme()
			// A lock screen or another app can take activation while WebView2
			// has the cursor hidden. Regaining activation is the first moment
			// the pointer can be restored without fighting an active page.
			restoreCursorDisplayCount()
			//}
		}

	case w32.WM_SETFOCUS:
		restoreCursorDisplayCount()

	case wmWtsSessionChange:
		if wparam == wtsSessionUnlock {
			restoreCursorDisplayCount()
		}

	case w32.WM_DESTROY:
		unregisterSessionNotification(uintptr(w.Handle()))

	case 0x02E0: //w32.WM_DPICHANGED
		newWindowSize := (*w32.RECT)(unsafe.Pointer(lparam))
		w32.SetWindowPos(w.Handle(),
			uintptr(0),
			int(newWindowSize.Left),
			int(newWindowSize.Top),
			int(newWindowSize.Right-newWindowSize.Left),
			int(newWindowSize.Bottom-newWindowSize.Top),
			w32.SWP_NOZORDER|w32.SWP_NOACTIVATE)
	}

	if w.frontendOptions.Frameless {
		switch msg {
		case w32.WM_ACTIVATE:
			// If we want to have a frameless window but with the default frame decorations, extend the DWM client area.
			// This Option is not affected by returning 0 in WM_NCCALCSIZE.
			// As a result we have hidden the titlebar but still have the default window frame styling.
			// See: https://docs.microsoft.com/en-us/windows/win32/api/dwmapi/nf-dwmapi-dwmextendframeintoclientarea#remarks
			if w.framelessWithDecorations {
				win32.ExtendFrameIntoClientArea(w.Handle(), true)
			}
		case w32.WM_NCCALCSIZE:
			// Disable the standard frame by allowing the client area to take the full
			// window size.
			// See: https://docs.microsoft.com/en-us/windows/win32/winmsg/wm-nccalcsize#remarks
			// This hides the titlebar and also disables the resizing from user interaction because the standard frame is not
			// shown. We still need the WS_THICKFRAME style to enable resizing from the frontend.
			if wparam != 0 {
				rgrc := (*w32.RECT)(unsafe.Pointer(lparam))
				if w.Form.IsFullScreen() {
					// In Full-Screen mode we don't need to adjust anything
					w.SetPadding(edge.Rect{})
				} else if w.IsMaximised() {
					// If the window is maximized we must adjust the client area to the work area of the monitor. Otherwise
					// some content goes beyond the visible part of the monitor.
					// Make sure to use the provided RECT to get the monitor, because during maximizig there might be
					// a wrong monitor returned in multi screen mode when using MonitorFromWindow.
					// See: https://github.com/MicrosoftEdge/WebView2Feedback/issues/2549
					monitor := w32.MonitorFromRect(rgrc, w32.MONITOR_DEFAULTTONULL)

					var monitorInfo w32.MONITORINFO
					monitorInfo.CbSize = uint32(unsafe.Sizeof(monitorInfo))
					if monitor != 0 && w32.GetMonitorInfo(monitor, &monitorInfo) {
						*rgrc = monitorInfo.RcWork

						maxWidth := w.frontendOptions.MaxWidth
						maxHeight := w.frontendOptions.MaxHeight
						if maxWidth > 0 || maxHeight > 0 {
							var dpiX, dpiY uint
							w32.GetDPIForMonitor(monitor, w32.MDT_EFFECTIVE_DPI, &dpiX, &dpiY)

							maxWidth := int32(winc.ScaleWithDPI(maxWidth, dpiX))
							if maxWidth > 0 && rgrc.Right-rgrc.Left > maxWidth {
								rgrc.Right = rgrc.Left + maxWidth
							}

							maxHeight := int32(winc.ScaleWithDPI(maxHeight, dpiY))
							if maxHeight > 0 && rgrc.Bottom-rgrc.Top > maxHeight {
								rgrc.Bottom = rgrc.Top + maxHeight
							}
						}
					}
					w.SetPadding(edge.Rect{})
				} else {
					// This is needed to workaround the resize flickering in frameless mode with WindowDecorations
					// See: https://stackoverflow.com/a/6558508
					// The workaround originally suggests to decrese the bottom 1px, but that seems to bring up a thin
					// white line on some Windows-Versions, due to DrawBackground using also this reduces ClientSize.
					// Increasing the bottom also worksaround the flickering but we would loose 1px of the WebView content
					// therefore let's pad the content with 1px at the bottom.
					rgrc.Bottom += 1
					w.SetPadding(edge.Rect{Bottom: 1})
				}
				return 0
			}
		}
	}
	return w.Form.WndProc(msg, wparam, lparam)
}

func (w *Window) IsMaximised() bool {
	return win32.IsWindowMaximised(w.Handle())
}

func (w *Window) IsMinimised() bool {
	return win32.IsWindowMinimised(w.Handle())
}

func (w *Window) IsNormal() bool {
	return win32.IsWindowNormal(w.Handle())
}

func (w *Window) IsFullScreen() bool {
	return win32.IsWindowFullScreen(w.Handle())
}

func (w *Window) SetTheme(theme winoptions.Theme) {
	w.theme = theme
	w.themeChanged = true
	w.Invoke(func() {
		w.UpdateTheme()
	})
}

func invokeSync[T any](cba *Window, fn func() (T, error)) (res T, err error) {
	var wg sync.WaitGroup
	wg.Add(1)
	cba.Invoke(func() {
		res, err = fn()
		wg.Done()
	})
	wg.Wait()
	return res, err
}

// SetPadding is a filter that wraps chromium.SetPadding to prevent unnecessary redraws during minimize/restore
// 包装了chromium.SetPadding的过滤器,用于防止窗口最小化/恢复过程中的不必要重绘
// This fixes window flickering when minimizing/restoring frameless windows
// 这修复了无边框窗口在最小化/恢复时的闪烁问题
// Reference: https://github.com/wailsapp/wails/issues/3951
func (w *Window) SetPadding(padding edge.Rect) {
	// Skip SetPadding if window is being minimized to prevent flickering
	// 如果窗口正在最小化,跳过设置padding以防止闪烁
	if w.isMinimizing {
		return
	}
	w.chromium.SetPadding(padding)
}
