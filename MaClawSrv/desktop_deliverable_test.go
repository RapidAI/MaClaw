package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const d98TinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func d98Round(t *testing.T, runID, display string) {
	t.Helper()
	w := &desktopRoundWake{have: display != "", woken: true, endpoint: desktopEndpoint{Display: display}, scope: agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_snake", RunID: runID}}
	desktopRoundWakes.Store(runID, w)
	t.Cleanup(func() { desktopRoundWakes.Delete(runID) })
}

func d98Capture(t *testing.T, fn func(context.Context, agentruntime.Scope, string) (string, error)) {
	t.Helper()
	prev := desktopCaptureFinal
	desktopCaptureFinal = fn
	t.Cleanup(func() { desktopCaptureFinal = prev })
}

// Snake game, Nov 11: "截屏发我", shots only taken with bash, reply had none.
func TestD98AskedPictureIsCapturedAtRunEnd(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "开发一个贪吃蛇游戏，然后运行后，截屏发我")
	d98Round(t, "run_d98a", ":44")
	var gotUser, gotDisplay string
	d98Capture(t, func(c context.Context, s agentruntime.Scope, d string) (string, error) {
		gotUser, gotDisplay = s.UserID, d
		noteDesktopShot(c, "image/png", d98TinyPNG)
		return "ok", nil
	})
	ensureDesktopPictureDelivered(ctx, "run_d98a", "done")
	if gotDisplay != ":44" || gotUser != "u::desk::bot_snake" {
		t.Fatalf("captured %q on %q, want the bot's own display", gotUser, gotDisplay)
	}
	msg := &agentservice.Message{Content: "游戏做好了，文件在 snake_game 里。"}
	images, files := publishDesktopReply(msg, slot)
	desktopPictureNote(slot, msg, images, files)
	if images != 1 || len(msg.Attachments) != 1 || msg.Attachments[0].MimeType != "image/png" {
		t.Fatalf("images=%d attachments=%+v", images, msg.Attachments)
	}
	if strings.Contains(msg.Content, "截图没能") {
		t.Fatalf("note added although the picture rides: %q", msg.Content)
	}
}

func TestD98AlreadyHasPictureTakesNoShot(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "截图给我看看")
	noteDesktopShot(ctx, "image/png", d98TinyPNG)
	d98Round(t, "run_d98b", ":44")
	d98Capture(t, func(context.Context, agentruntime.Scope, string) (string, error) {
		t.Fatal("captured again")
		return "", nil
	})
	ensureDesktopPictureDelivered(ctx, "run_d98b", "done")
}

func TestD98UnaskedOrWaitingTakesNoShot(t *testing.T) {
	d98Round(t, "run_d98c", ":44")
	d98Capture(t, func(context.Context, agentruntime.Scope, string) (string, error) {
		t.Fatal("captured without being asked")
		return "", nil
	})
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "把这篇论文发到知乎")
	ensureDesktopPictureDelivered(ctx, "run_d98c", "done")
	ctx2, slot2 := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot2, "截屏发我")
	ensureDesktopPictureDelivered(ctx2, "run_d98c", "awaiting_user")
	msg := &agentservice.Message{Content: "可以吗？"}
	desktopPictureNote(slot2, msg, 0, 0)
	if msg.Content != "可以吗？" {
		t.Fatalf("waiting reply changed: %q", msg.Content)
	}
}

func TestD98FailedCaptureIsSaidHonestly(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "运行后截屏发我")
	d98Round(t, "run_d98d", ":44")
	d98Capture(t, func(context.Context, agentruntime.Scope, string) (string, error) {
		return "", errors.New("desktop is asleep")
	})
	ensureDesktopPictureDelivered(ctx, "run_d98d", "done")
	msg := &agentservice.Message{Content: "游戏做好了，截图已发给你。"}
	images, files := publishDesktopReply(msg, slot)
	desktopPictureNote(slot, msg, images, files)
	if images != 0 || !strings.Contains(msg.Content, "截图没能发到这里") || !strings.Contains(msg.Content, "desktop is asleep") {
		t.Fatalf("reply %q images=%d", msg.Content, images)
	}
}

func TestD98NoRoundSaysNoScreen(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "截屏发我")
	ensureDesktopPictureDelivered(ctx, "run_d98_none", "done")
	msg := &agentservice.Message{Content: "好了"}
	desktopPictureNote(slot, msg, 0, 0)
	if !strings.Contains(msg.Content, "没有打开过桌面") {
		t.Fatalf("reply %q", msg.Content)
	}
}

func TestD98DeliveredImageFileCounts(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	noteDesktopRequest(slot, "截屏发我")
	if err := noteDesktopFile(ctx, "play.png", "image/png", []byte("\x89PNG\r\n\x1a\nxx")); err != nil {
		t.Fatal(err)
	}
	d98Round(t, "run_d98e", ":44")
	d98Capture(t, func(context.Context, agentruntime.Scope, string) (string, error) {
		t.Fatal("captured although an image file was delivered")
		return "", nil
	})
	ensureDesktopPictureDelivered(ctx, "run_d98e", "done")
}

// Snake, Nov 11 10:38: four deliver calls, one file reached the chat.
func TestD98EveryDeliveredFileRides(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	for _, n := range []string{"a.png", "b.png", "c.png", "b.png"} {
		if err := noteDesktopFile(ctx, n, "image/png", []byte("\x89PNG\r\n\x1a\n"+n)); err != nil {
			t.Fatal(err)
		}
	}
	msg := &agentservice.Message{Content: "好了"}
	if n := attachDesktopFile(msg, slot); n != 3 || len(msg.Attachments) != 3 {
		t.Fatalf("attached %d: %+v", n, msg.Attachments)
	}
	if attachDesktopFile(&agentservice.Message{}, slot) != 0 {
		t.Fatal("slot not emptied")
	}
}

func TestD98DeliveredFilesShareOneBudget(t *testing.T) {
	ctx, _ := withDesktopShotSlot(context.Background())
	big := make([]byte, 400_000)
	if err := noteDesktopFile(ctx, "a.pdf", "application/pdf", big); err != nil {
		t.Fatal(err)
	}
	if err := noteDesktopFile(ctx, "b.pdf", "application/pdf", big); err == nil || !strings.Contains(err.Error(), "together with") {
		t.Fatalf("err = %v", err)
	}
}
