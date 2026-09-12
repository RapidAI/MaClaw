package guiapp

import "testing"

type trayNotifRecord struct {
	title string
	msg   string
	icon  uint32
}

// stubTrayNotifyEnv replaces the tray notification globals and returns the
// captured notifications plus a restore func.
func stubTrayNotifyEnv(t *testing.T, windowVisible bool) (*[]trayNotifRecord, *int, func()) {
	t.Helper()
	origShow, origFlash, origVisible := ShowNotification, FlashAndBeep, IsMainWindowVisible
	notifs := &[]trayNotifRecord{}
	flashes := new(int)
	ShowNotification = func(title, message string, iconFlag uint32) {
		*notifs = append(*notifs, trayNotifRecord{title, message, iconFlag})
	}
	FlashAndBeep = func() { *flashes++ }
	IsMainWindowVisible = func() bool { return windowVisible }
	return notifs, flashes, func() {
		ShowNotification, FlashAndBeep, IsMainWindowVisible = origShow, origFlash, origVisible
	}
}

func TestNotifyTaskResponseTrayRouting(t *testing.T) {
	notifs, flashes, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	a := &App{}

	reset := func() {
		*notifs = nil
		*flashes = 0
	}

	t.Run("error", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Error: "boom"})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].icon != 3 || (*notifs)[0].msg != "boom" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 1 {
			t.Fatalf("expected flash, got %d", *flashes)
		}
	})

	t.Run("confirmation", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Confirmation: &IMResponseConfirmation{Summary: "确认删除？"}})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || (*notifs)[0].icon != 2 || (*notifs)[0].msg != "确认删除？" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 1 {
			t.Fatalf("expected flash, got %d", *flashes)
		}
	})

	t.Run("unfinished slot waits for user", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "有中断任务", UnfinishedSlot: &IMResponseUnfinishedTask{}})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || (*notifs)[0].icon != 2 {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("ask user", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "选择哪个目录？", ResponseSource: imResponseSourceAskUser.String()})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].icon != 2 || (*notifs)[0].msg != "选择哪个目录？" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 1 {
			t.Fatalf("expected flash, got %d", *flashes)
		}
	})

	t.Run("cancel is silent", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "已取消", ResponseSource: imResponseSourceCancel.String()})
		if len(*notifs) != 0 {
			t.Fatalf("expected no notification for cancel, got %+v", *notifs)
		}
	})

	t.Run("completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "done"})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务完成" || (*notifs)[0].icon != 1 || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 0 {
			t.Fatalf("completion should not flash, got %d", *flashes)
		}
	})

	t.Run("nil response is silent", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(nil)
		if len(*notifs) != 0 {
			t.Fatalf("expected no notification for nil resp, got %+v", *notifs)
		}
	})
}

func TestNotifyTaskResponseTraySilentWhileWindowVisible(t *testing.T) {
	notifs, _, restore := stubTrayNotifyEnv(t, true)
	defer restore()
	// ctx is nil, so the minimised fallback cannot fire either.
	a := &App{}
	a.notifyTaskResponseTray(&IMAgentResponse{Text: "done"})
	if len(*notifs) != 0 {
		t.Fatalf("expected no notification while window visible, got %+v", *notifs)
	}
}
