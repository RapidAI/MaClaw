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
	if strings.Contains(plain, "headless") {
		t.Fatalf("default launch must stay headed: %s", plain)
	}
	if !strings.Contains(plain, "--remote-debugging-port=9223") || !strings.Contains(plain, `--user-data-dir=C:\profile`) {
		t.Fatalf("launch args lost the debug port or profile: %s", plain)
	}
	EnableGUIEmbed(true)
	embedded := strings.Join(managedLaunchArgs(9223, `C:\profile`), " ")
	for _, want := range []string{"--headless=new", "--window-size=1280,800", "--force-device-scale-factor=1", "--remote-debugging-port=9223"} {
		if !strings.Contains(embedded, want) {
			t.Fatalf("embed launch missing %s in %s", want, embedded)
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
