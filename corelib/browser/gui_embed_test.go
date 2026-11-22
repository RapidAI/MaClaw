package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManagedLaunchArgsHeadlessOnlyWhenGUIEmbed(t *testing.T) {
	t.Cleanup(func() { EnableGUIEmbed(false) })
	EnableGUIEmbed(false)
	plain := strings.Join(managedLaunchArgs(9223, `C:\profile`), " ")
	assertHeadedManagedLaunch(t, plain)
	if !strings.Contains(plain, "--remote-debugging-port=9223") || !strings.Contains(plain, `--user-data-dir=C:\profile`) {
		t.Fatalf("launch args lost the debug port or profile: %s", plain)
	}
	EnableGUIEmbed(true)
	embedded := strings.Join(managedLaunchArgs(9223, `C:\profile`), " ")
	assertHeadedManagedLaunch(t, embedded)
	for _, want := range []string{"--window-size=1280,800", "--remote-debugging-port=9223"} {
		if !strings.Contains(embedded, want) {
			t.Fatalf("embed launch missing %s in %s", want, embedded)
		}
	}
	if strings.Contains(embedded, "force-device-scale-factor") {
		t.Fatalf("embed launch must keep the OS scale: %s", embedded)
	}
	if strings.Contains(embedded, nativeOcclusionFeature) || strings.Contains(embedded, "--window-position=") {
		t.Fatalf("a profile this process does not own must stay on the desktop: %s", embedded)
	}
	owned := strings.Join(managedLaunchArgs(9223, persistentProfileDir()), " ")
	assertHeadedManagedLaunch(t, owned)
	if !strings.Contains(owned, "--window-size=1280,800") || !strings.Contains(owned, "--disable-features="+nativeOcclusionFeature) || !strings.Contains(owned, "--window-position=") {
		t.Fatalf("owned preview launch must park off-screen without going headless: %s", owned)
	}
	if managedWindowShouldPark(`C:\profile`) {
		t.Fatal("foreign profile must not be parked")
	}
	if !managedWindowShouldPark(persistentProfileDir()) || !managedWindowShouldPark(debugProfileDir()) {
		t.Fatal("owned profiles must be parked while the preview is on")
	}
}

func TestCommandLineDisablesNativeOcclusion(t *testing.T) {
	if !commandLineDisablesNativeOcclusion(`chrome.exe --disable-features=CalculateNativeWinOcclusion --user-data-dir=C:\p`) {
		t.Fatal("single feature")
	}
	if !commandLineDisablesNativeOcclusion(`chrome.exe --disable-features=Foo,CalculateNativeWinOcclusion,Bar`) {
		t.Fatal("feature list")
	}
	if commandLineDisablesNativeOcclusion(`chrome.exe --user-data-dir=C:\CalculateNativeWinOcclusion --remote-debugging-port=9222`) {
		t.Fatal("profile path is not the switch")
	}
	if commandLineDisablesNativeOcclusion(`chrome.exe --disable-features=CalculateNativeWinOcclusionExtra`) {
		t.Fatal("prefix of the feature name")
	}
	if commandLinesDisableNativeOcclusion(nil) {
		t.Fatal("no process is not proof")
	}
	lines := []string{
		`chrome.exe --disable-features=CalculateNativeWinOcclusion`,
		`chrome.exe --window-size=1280,800`,
	}
	if commandLinesDisableNativeOcclusion(lines) {
		t.Fatal("one process without the switch must keep the relaunch")
	}
}

func TestCommandLineHasProfile(t *testing.T) {
	dir := `C:\Users\ma139\.maclaw\browser-profile`
	if !commandLineHasProfile(`chrome.exe --user-data-dir=`+dir+` --remote-debugging-port=1`, dir) {
		t.Fatal("exact profile")
	}
	if !commandLineHasProfile(`chrome.exe --user-data-dir="`+dir+`"`, dir) {
		t.Fatal("quoted profile")
	}
	if commandLineHasProfile(`chrome.exe --user-data-dir=`+dir+`-extra`, dir) {
		t.Fatal("longer directory shares only a prefix")
	}
}

func TestParkedOriginClearsVirtualScreen(t *testing.T) {
	const vx, vy, vw, vh = -1368, 0, 3288, 1080
	for _, width := range []int{200, 1280, 3840, 9000} {
		x := parkedWindowLeft(vx, width)
		if rectIntersectsVirtualScreen(x, vy, x+width, vy+800, vx, vy, vw, vh) {
			t.Fatalf("width %d at %d still meets the virtual screen", width, x)
		}
	}
	x, y := parkedOriginForScreen(vx, vy)
	if rectIntersectsVirtualScreen(x, y, x+1280, y+800, vx, vy, vw, vh) {
		t.Fatalf("launch position still meets the virtual screen at %d,%d", x, y)
	}
	if !rectIntersectsVirtualScreen(10, 10, 1290, 810, vx, vy, vw, vh) {
		t.Fatal("on-screen window must still count")
	}
	if rectIntersectsVirtualScreen(10, 10, 10, 810, vx, vy, vw, vh) {
		t.Fatal("zero-area rectangle")
	}
}

func TestParkRestoreTarget(t *testing.T) {
	if got := parkRestoreTarget(5, 9, 7, 3, true); got != 9 {
		t.Fatalf("remembered window %d", got)
	}
	if got := parkRestoreTarget(5, 0, 7, 3, true); got != 7 {
		t.Fatalf("window behind %d", got)
	}
	if got := parkRestoreTarget(5, 5, 7, 3, true); got != 7 {
		t.Fatalf("remembered is the browser %d", got)
	}
	if got := parkRestoreTarget(5, 0, 0, 3, true); got != 3 {
		t.Fatalf("own window %d", got)
	}
	if got := parkRestoreTarget(5, 0, 5, 3, true); got != 3 {
		t.Fatalf("window behind is the browser %d", got)
	}
	if got := parkRestoreTarget(5, 9, 7, 3, false); got != 0 {
		t.Fatalf("foreign foreground %d", got)
	}
	if got := parkRestoreTarget(0, 9, 7, 3, true); got != 0 {
		t.Fatalf("no foreground %d", got)
	}
}

func TestParkFocusCandidateSkipsToolWindows(t *testing.T) {
	if !parkFocusCandidate(0, true, false, true, false) {
		t.Fatal("ordinary window")
	}
	if parkFocusCandidate(0x80, true, false, true, false) {
		t.Fatal("tool window")
	}
	if parkFocusCandidate(0x08000000, true, false, true, false) {
		t.Fatal("no-activate window")
	}
	if parkFocusCandidate(0, true, true, true, false) {
		t.Fatal("our browser")
	}
	if parkFocusCandidate(0, true, false, true, true) {
		t.Fatal("minimized window")
	}
	if parkFocusCandidate(0, false, false, true, false) || parkFocusCandidate(0, true, false, false, false) {
		t.Fatal("hidden or empty window")
	}
}

func TestShownParkWidthUsesTheSizeThatWillBeShown(t *testing.T) {
	if got := shownParkWidth(160, 9000, true); got != 9000 {
		t.Fatalf("minimized window must use its restored width, got %d", got)
	}
	if got := shownParkWidth(3288, 1280, true); got != 1280 {
		t.Fatalf("maximized window must use its restored width, got %d", got)
	}
	if got := shownParkWidth(160, 100, true); got != 1280 {
		t.Fatalf("missing restored width, got %d", got)
	}
	if got := shownParkWidth(1920, 0, false); got != 1920 {
		t.Fatalf("shown width, got %d", got)
	}
	if got := shownParkWidth(0, 1280, false); got != 1280 {
		t.Fatalf("unknown shown width, got %d", got)
	}
	const vx = -1368
	wide := shownParkWidth(160, 9000, true)
	x := parkedWindowLeft(vx, wide)
	if rectIntersectsVirtualScreen(x, 0, x+wide, 800, vx, 0, 3288, 1080) {
		t.Fatalf("restored %d-wide window at %d still meets the virtual screen", wide, x)
	}
}

func TestParkPollWaits(t *testing.T) {
	if parkPollWait(0, true, true) != time.Second {
		t.Fatal("no watched profile must not snapshot")
	}
	if parkPollWait(1, true, false) != 15*time.Millisecond {
		t.Fatal("fresh window")
	}
	if parkPollWait(1, false, true) != 15*time.Millisecond {
		t.Fatal("window still on a monitor")
	}
	if parkPollWait(1, false, false) != 400*time.Millisecond {
		t.Fatal("parked window")
	}
	if parkPIDCacheAge(false, false) != 2*time.Second {
		t.Fatal("idle process scan")
	}
	if parkPIDCacheAge(true, false) != 80*time.Millisecond {
		t.Fatal("fresh process scan")
	}
	if parkPIDCacheAge(false, true) != 80*time.Millisecond {
		t.Fatal("busy process scan")
	}
	if parkWatchExpired(time.Time{}, time.Now()) {
		t.Fatal("the first miss only starts the timer")
	}
	start := time.Now()
	if parkWatchExpired(start, start.Add(29*time.Second)) {
		t.Fatal("a slow startup is still inside the timer")
	}
	if !parkWatchExpired(start, start.Add(parkProcessGoneAfter)) {
		t.Fatal("a process gone for the full interval must drop")
	}
}

func assertHeadedManagedLaunch(t *testing.T, args string) {
	t.Helper()
	if strings.Contains(strings.ToLower(args), "headless") {
		t.Fatalf("managed launch must stay headed: %s", args)
	}
	if strings.Contains(args, "--enable-automation") {
		t.Fatalf("managed launch must not set the automation switch: %s", args)
	}
	if strings.Contains(args, "--hide-scrollbars") {
		t.Fatalf("managed launch must keep normal scrollbars: %s", args)
	}
	if !strings.Contains(args, "--disable-blink-features=AutomationControlled") {
		t.Fatalf("managed launch must leave navigator.webdriver false: %s", args)
	}
	if strings.Contains(args, "--remote-allow-origins=*") {
		t.Fatalf("debugger port must not accept every origin: %s", args)
	}
	if !strings.Contains(args, "--remote-allow-origins=http://127.0.0.1:9223,http://localhost:9223") {
		t.Fatalf("debugger port must allow only the loopback dial origin: %s", args)
	}
}

func TestPageDrivingOmitsRuntimeEnable(t *testing.T) {
	for _, method := range append(append([]string{}, pageDrivingMethods()...), frameDrivingMethods()...) {
		if method == "Runtime.enable" || strings.Contains(method, "Runtime.enable") {
			t.Fatalf("driving domain subscribes the page to the debugger: %s", method)
		}
	}
	joined := strings.Join(pageDrivingMethods(), " ")
	frames := strings.Join(frameDrivingMethods(), " ")
	if !strings.Contains(joined, "Page.enable") {
		t.Fatalf("page driving lost Page.enable in %s", joined)
	}
	for _, banned := range []string{"Runtime.enable", "Log.enable", "Network.enable"} {
		if strings.Contains(joined, banned) || strings.Contains(frames, banned) {
			t.Fatalf("driving domain %s is visible to the page: %s", banned, joined+" "+frames)
		}
	}
}

func TestMapViewportFraction(t *testing.T) {
	x, y, ok := MapViewportFraction(0.25, 0.5, 1280, 800)
	if !ok || x != 320 || y != 400 {
		t.Fatalf("got %v,%v ok=%v", x, y, ok)
	}
	if _, _, ok := MapViewportFraction(0.5, 0.5, 0, 800); ok {
		t.Fatal("zero width must fail")
	}
	x, y, ok = MapViewportFraction(-1, 2, 100, 100)
	if !ok || x != 0 || y != 100 {
		t.Fatalf("clamp got %v,%v", x, y)
	}
}

func TestParseViewportInput(t *testing.T) {
	in, err := ParseViewportInput(`{"kind":"down","x":0.5,"y":0.25,"button":"left","click_count":1}`)
	if err != nil || in.Kind != "down" || in.X != 0.5 || in.Button != "left" {
		t.Fatalf("parse %+v err=%v", in, err)
	}
	if _, err := ParseViewportInput(`{"x":1}`); err == nil {
		t.Fatal("missing kind must fail")
	}
}

func TestUserHoldReleaseTellsCallerToProbe(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(false, "")
	sess := &BrowserAgentSession{stopCh: make(chan struct{})}
	if err := sess.blockIfUserHolds(); err != nil {
		t.Fatalf("idle hold: %v", err)
	}
	SetUserHoldsBrowser(true, "preview")
	done := make(chan error, 1)
	go func() {
		done <- sess.yieldToUserHold(context.Background())
	}()
	time.Sleep(20 * time.Millisecond)
	SetUserHoldsBrowser(false, "")
	select {
	case err := <-done:
		if !errors.Is(err, errUserReleased) || !strings.Contains(err.Error(), "probe") {
			t.Fatalf("release err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("yield did not return after release")
	}
}

func TestUserHoldStopsWhenSessionStops(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(true, "preview")
	stop := make(chan struct{})
	sess := &BrowserAgentSession{stopCh: stop}
	done := make(chan error, 1)
	go func() {
		done <- sess.blockIfUserHolds()
	}()
	time.Sleep(20 * time.Millisecond)
	close(stop)
	select {
	case err := <-done:
		if !errors.Is(err, errUserHoldStopped) {
			t.Fatalf("stop err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("yield ignored session stop")
	}
}

func TestCancelledHoldLeaseSurvivesANewerHold(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(false, "")

	cancelled := browserUserHold.set(true, "captcha", false)
	CancelUserHold()
	confirm := browserUserHold.set(true, "confirm", false)
	if confirm == cancelled {
		t.Fatal("newer hold reused the cancelled lease")
	}
	err := waitArmedHold(cancelled, nil)
	if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
		t.Fatalf("cancel rewritten as a hand-back: %v", err)
	}

	SetUserHoldsBrowser(false, "")
	err = waitArmedHold(confirm, nil)
	if !errors.Is(err, errUserReleased) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("hand-back err=%v", err)
	}
	later := browserUserHold.set(true, "captcha", false)
	CancelUserHold()
	if later == confirm {
		t.Fatal("later hold reused the hand-back lease")
	}
	err = waitArmedHold(confirm, nil)
	if !errors.Is(err, errUserReleased) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("hand-back rewritten as a cancel: %v", err)
	}
	err = waitArmedHold(later, nil)
	if !errors.Is(err, errUserCancelled) || strings.Contains(err.Error(), "probe") {
		t.Fatalf("later cancel err=%v", err)
	}
}

func TestDismissHandsBackOnlyTheHoldItSaw(t *testing.T) {
	t.Cleanup(func() { SetUserHoldsBrowser(false, "") })
	SetUserHoldsBrowser(false, "")
	lease := browserUserHold.set(true, "captcha", false)
	DismissBrowserPreview()
	err := waitArmedHold(lease, nil)
	if !errors.Is(err, errUserReleased) || !strings.Contains(err.Error(), "probe") {
		t.Fatalf("plain dismiss err=%v", err)
	}
	if held, _ := UserHoldsBrowser(); held {
		t.Fatal("plain dismiss left the hold up")
	}
}

func TestIsPreviewHandBack(t *testing.T) {
	if !IsPreviewHandBack(errUserReleased) || !IsPreviewHandBack(ErrPreviewHandBack) {
		t.Fatal("hand-back")
	}
	wrapped := errors.Join(ErrPreviewHandBack, errors.New("after"))
	if !IsPreviewHandBack(wrapped) {
		t.Fatal("wrapped hand-back")
	}
	if IsPreviewHandBack(errUserCancelled) || IsPreviewHandBack(errUserHoldTimeout) || IsPreviewHandBack(errUserHoldStopped) || IsPreviewHandBack(nil) {
		t.Fatal("cancel, timeout, stop, and nil are not hand-backs")
	}
}

func TestCaptchaAskNotifiesStage(t *testing.T) {
	var got BrowserStageNotice
	SetBrowserStageNotifier(func(n BrowserStageNotice) { got = n })
	t.Cleanup(func() { SetBrowserStageNotifier(nil) })
	s := &BrowserAgentSession{ID: "browser-session-x"}
	res := s.captchaAskResult("click")
	if res == nil || res.Status != "ask" || res.Detail != "captcha_widget" {
		t.Fatalf("ask result %#v", res)
	}
	if got.Kind != "captcha" || got.SessionID != "browser-session-x" || got.Reason != "captcha_widget" {
		t.Fatalf("notice %#v", got)
	}
}
