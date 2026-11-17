package computeruse

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

func TestDesktopHandoffWaitsUntilThePersonFinishes(t *testing.T) {
	t.Cleanup(func() {
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	var got DesktopHandoffNotice
	SetDesktopHandoffNotifier(func(n DesktopHandoffNotice) {
		if n.Active {
			got = n
		}
	})
	done := make(chan error, 1)
	go func() {
		done <- OfferDesktopHandoff("secret", "请输入支付密码")
	}()
	time.Sleep(30 * time.Millisecond)
	if !got.Active || got.Reason != "secret" || got.Title != "请输入支付密码" {
		t.Fatalf("notice %+v", got)
	}
	if strings.Contains(got.Title, "123456") {
		t.Fatal(got.Title)
	}
	if err := ResolveDesktopHandoff(true); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrDesktopHandoffDone) || strings.Contains(err.Error(), "123456") {
			t.Fatalf("done err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handoff did not return")
	}
}

func TestDesktopHandoffCancelDoesNotResumeWhenSuppressed(t *testing.T) {
	t.Cleanup(func() {
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	var last DesktopHandoffNotice
	SetDesktopHandoffNotifier(func(n DesktopHandoffNotice) { last = n })
	done := make(chan error, 1)
	go func() {
		done <- OfferDesktopHandoff("checkout", "立即支付")
	}()
	time.Sleep(30 * time.Millisecond)
	SetDesktopHandoffResume(false)
	if err := ResolveDesktopHandoff(false); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrDesktopHandoffCancelled) {
			t.Fatalf("cancel err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not return")
	}
	time.Sleep(20 * time.Millisecond)
	if last.Active || last.Resume {
		t.Fatalf("end notice %+v", last)
	}
}

func TestDesktopHandoffOffDoesNotWait(t *testing.T) {
	SetDesktopHandoffNotifier(nil)
	if err := OfferDesktopHandoff("secret", "密码"); !errors.Is(err, ErrDesktopHandoffOff) {
		t.Fatal(err)
	}
	if DesktopHandoffEnabled() {
		t.Fatal("notifier nil must disable the wait")
	}
}

func TestDesktopHandoffTimeout(t *testing.T) {
	old := desktopHandoffWaitCap
	desktopHandoffWaitCap = 40 * time.Millisecond
	t.Cleanup(func() {
		desktopHandoffWaitCap = old
		SetDesktopHandoffNotifier(nil)
		_ = ResolveDesktopHandoff(false)
	})
	SetDesktopHandoffNotifier(func(DesktopHandoffNotice) {})
	err := OfferDesktopHandoff("secret", "验证码")
	if !errors.Is(err, ErrDesktopHandoffTimeout) {
		t.Fatal(err)
	}
}

func TestElementNameAtScreenPrefersTheSmallerControl(t *testing.T) {
	s := NewSession(DefaultConfig())
	s.CommitObserve(ScreenMeta{Width: 200, Height: 100, ScaleFactor: 1}, nil, []taskengine.UIElement{
		{Name: "页面", BBox: [4]int{0, 0, 200, 100}, Source: "yolo"},
		{Name: "立即支付", BBox: [4]int{10, 10, 40, 20}, Source: "yolo"},
	}, nil, "")
	if got := s.ElementNameAtScreen(30, 20); got != "立即支付" {
		t.Fatalf("pay hit %q", got)
	}
	if got := s.ElementNameAtScreen(180, 80); got != "页面" {
		t.Fatalf("page hit %q", got)
	}
	if got := s.ElementNameAtScreen(400, 400); got != "" {
		t.Fatalf("miss %q", got)
	}
}
