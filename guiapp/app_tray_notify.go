package guiapp

import (
	"log"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifyTaskResponseTray pops a system tray balloon notification when an
// interactive agent task reaches a terminal state: failed, awaiting user
// confirmation, waiting for an ask_user reply, or completed. It only fires
// while the main window is hidden or minimised, so users actively working in
// the window are not disturbed. User-initiated cancellations are skipped.
// It must never panic: callers treat a panic in this emit path as a task
// failure and would surface a bogus error response.
func (a *App) notifyTaskResponseTray(resp *IMAgentResponse) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tray-notify] panic suppressed: %v", r)
		}
	}()
	if resp == nil || ShowNotification == nil || !a.shouldTrayNotify() {
		return
	}
	source := canonicalIMResponseSourceKind(resp.ResponseSource)
	if source == imResponseSourceCancel {
		// User-initiated cancellation; the user already knows.
		return
	}
	body := truncateRunes(strings.TrimSpace(resp.Text), 200)
	switch {
	case strings.TrimSpace(resp.Error) != "":
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		ShowNotification("任务异常", truncateRunes(strings.TrimSpace(resp.Error), 200), 3)
	case resp.Confirmation != nil || resp.UnfinishedTask != nil || resp.UnfinishedSlot != nil:
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		msg := body
		if resp.Confirmation != nil {
			msg = truncateRunes(strings.TrimSpace(resp.Confirmation.Summary), 200)
			if msg == "" {
				msg = body
			}
		}
		if msg == "" {
			msg = "任务等待确认，请返回窗口处理"
		}
		ShowNotification("需要确认", msg, 2)
	case source == imResponseSourceAskUser:
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		if body == "" {
			body = "任务等待回复，请返回窗口查看"
		}
		ShowNotification("等待回复", body, 2)
	default:
		if body == "" {
			body = "任务已执行完毕"
		}
		ShowNotification("任务完成", body, 1)
	}
}

func (a *App) shouldTrayNotify() bool {
	if IsMainWindowVisible != nil && !IsMainWindowVisible() {
		return true
	}
	if a != nil && a.ctx != nil {
		return runtime.WindowIsMinimised(a.ctx)
	}
	return false
}
