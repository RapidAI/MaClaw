package main

import (
	"context"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A run whose person asked to see the result as a picture ("截屏发我") must
// end with a picture on its reply. The bot used to take its shots with bash
// (import/scrot into ~/Desktop/...), never called deliver or screenshot at
// the end, and the reply arrived with no image (snake game, Nov 11 10:03).
// Before the round's desktop is released, the run end looks at the bot's own
// screen once and puts that picture on the reply. When that also fails the
// reply says so, with the cause, instead of implying it was sent.

// desktopFinalShotTimeout bounds the one capture at the run's end.
var desktopFinalShotTimeout = 30 * time.Second

func noteDesktopRequest(slot *desktopShotSlot, request string) {
	if slot == nil {
		return
	}
	slot.mu.Lock()
	slot.request = strings.TrimSpace(request)
	slot.mu.Unlock()
}

// desktopPictureAsked reports a request that wants an image of the result.
func desktopPictureAsked(request string) bool {
	return desktopShotAskedRe.MatchString(request)
}

// desktopSlotHasPicture reports a screenshot or a delivered image file.
func desktopSlotHasPicture(slot *desktopShotSlot) bool {
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.ready && slot.shot.Data != "" {
		return true
	}
	for _, f := range slot.files {
		if strings.HasPrefix(strings.ToLower(f.MimeType), "image/") {
			return true
		}
	}
	return false
}

// ensureDesktopPictureDelivered runs at the run's completion, while the
// round's desktop is still bound. It never wakes a computer: only a round
// that already had its screen open is looked at.
// A run that ends waiting on the person (a question, a confirmation) is
// not its result yet and is left alone.
func ensureDesktopPictureDelivered(ctx context.Context, runID, outcome string) {
	slot := desktopShotSlotFrom(ctx)
	if slot == nil || outcome == "awaiting_user" {
		return
	}
	slot.mu.Lock()
	request := slot.request
	slot.mu.Unlock()
	if !desktopPictureAsked(request) || desktopSlotHasPicture(slot) {
		return
	}
	missing := desktopFinalShot(ctx, runID)
	if missing == "" {
		missing = "-"
	}
	slot.mu.Lock()
	slot.shotMissing = missing
	slot.mu.Unlock()
}

// desktopFinalShot captures the round's display onto the slot. It returns
// "" on success, or the cause in words for the person.
func desktopFinalShot(ctx context.Context, runID string) string {
	value, ok := desktopRoundWakes.Load(strings.TrimSpace(runID))
	if !ok {
		return "这次没有打开过桌面，没有画面可截"
	}
	wake, _ := value.(*desktopRoundWake)
	if wake == nil {
		return "这次没有打开过桌面，没有画面可截"
	}
	wake.mu.Lock()
	endpoint, have, scope := wake.endpoint, wake.have, wake.scope
	wake.mu.Unlock()
	if !have || strings.TrimSpace(endpoint.Display) == "" {
		return "这次没有打开过桌面，没有画面可截"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	shotCtx, cancel := context.WithTimeout(ctx, desktopFinalShotTimeout)
	defer cancel()
	if _, err := desktopCaptureFinal(shotCtx, scope, endpoint.Display); err != nil {
		return "结束时截屏失败（" + desktopRuneHead(err.Error(), 80) + "）"
	}
	if slot := desktopShotSlotFrom(ctx); slot != nil && !desktopSlotHasPicture(slot) {
		return "截到的画面太大，放不进这条回复"
	}
	return ""
}

// desktopCaptureFinal is a test seam over desktopCapture.
var desktopCaptureFinal = func(ctx context.Context, scope agentruntime.Scope, display string) (string, error) {
	return desktopCapture(ctx, scope, display, false, "")
}

// desktopPictureNote is the honest line a reply gets when a picture was
// asked for and none rides on it.
func desktopPictureNote(slot *desktopShotSlot, msg *agentservice.Message, images, files int) {
	if slot == nil || msg == nil || images > 0 {
		return
	}
	slot.mu.Lock()
	request, missing := slot.request, slot.shotMissing
	slot.mu.Unlock()
	// Only a run end that checked (and found no picture to send) speaks.
	if !desktopPictureAsked(request) || missing == "" || missing == "-" {
		return
	}
	if files > 0 {
		for _, a := range msg.Attachments {
			if strings.HasPrefix(strings.ToLower(a.MimeType), "image/") {
				return
			}
		}
	}
	note := "截图没能发到这里：" + missing + "。"
	if strings.Contains(msg.Content, note) {
		return
	}
	msg.Content = strings.TrimSpace(strings.TrimSpace(msg.Content) + "\n" + note)
}
