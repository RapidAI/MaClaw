package guiapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBotNoticeModelLineTimeoutAndFallback(t *testing.T) {
	orig := botNoticeWriter
	prev := botNoticeApp.Load()
	t.Cleanup(func() { botNoticeWriter = orig; botNoticeApp.Store(prev) })
	botNoticeApp.Store(nil)
	if got := botNotice("t", "notice", "g", "", "zh-Hans", "收到。", time.Second); got != "收到。" {
		t.Fatalf("no app=%q", got)
	}
	botNoticeApp.Store(&App{})
	var seen DesktopBotAckRequest
	botNoticeWriter = func(_ context.Context, _ *App, req DesktopBotAckRequest) (string, error) {
		seen = req
		return " 目标暂停了，发 /goal resume 我就接着做。 ", nil
	}
	if got := botNotice("t", "notice", "include /goal resume", "", "zh-Hans", "x", time.Second); got != "目标暂停了，发 /goal resume 我就接着做。" {
		t.Fatalf("line=%q", got)
	}
	if seen.Kind != "notice" || seen.Instruction != "include /goal resume" {
		t.Fatalf("req=%#v", seen)
	}
	botNoticeWriter = func(context.Context, *App, DesktopBotAckRequest) (string, error) { return "", errors.New("all failed") }
	if got := botNotice("t", "notice", "", "", "", "停了。", time.Second); got != "停了。" {
		t.Fatalf("fail=%q", got)
	}
	botNoticeWriter = func(ctx context.Context, _ *App, _ DesktopBotAckRequest) (string, error) {
		<-ctx.Done()
		return "late", nil
	}
	start := time.Now()
	if got := botNotice("t", "notice", "", "", "", "接着做", 50*time.Millisecond); got != "接着做" || time.Since(start) > time.Second {
		t.Fatalf("timeout=%q after %s", got, time.Since(start))
	}
}
