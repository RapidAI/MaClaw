package guiapp

import (
	"strings"
	"testing"
)

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

	t.Run("empty completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{})
		if len(*notifs) != 1 || (*notifs)[0].msg != "任务完成了" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("completion names the task", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:       "done",
			SessionKey: desktopUserID + `:\Users\me\.maclaw\data\tasks\项目是什么-1791813952406498200`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "项目是什么任务完成了" || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("workspace subdirectory keeps the task name", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:       "done",
			SessionKey: desktopUserID + `:\Users\me\.maclaw\data\tasks\项目是什么-1791813952406498200\workspace`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "项目是什么任务完成了" || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("directory under tasks without an id is not a task", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:       "done",
			SessionKey: desktopUserID + `:\Users\me\.maclaw\data\tasks\notes`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务完成" || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("named completion with an empty reply repeats the sentence", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			SessionKey: desktopUserID + `:\Users\me\.maclaw\data\tasks\项目是什么-1791813952406498200`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "项目是什么任务完成了" || (*notifs)[0].msg != "项目是什么任务完成了" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("project folder is not a task name", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:       "done",
			SessionKey: desktopUserID + `:F:\test-prog`,
		})
		if len(*notifs) != 1 || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("name already ending in 任务", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "done", trayTaskName: "整理任务"})
		if len(*notifs) != 1 || (*notifs)[0].title != "整理任务完成了" || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("recording session is not completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:           "请开始录音",
			trayTaskName:   "周会",
			ResponseSource: imResponseSourceRecordAudio.String(),
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "请开始录音" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 1 {
			t.Fatalf("expected flash, got %d", *flashes)
		}
	})

	t.Run("completion prefers an explicit label", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "done", trayTaskName: "打开报表"})
		if len(*notifs) != 1 || (*notifs)[0].title != "打开报表任务完成了" || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("long task name stays inside the balloon", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "done", trayTaskName: strings.Repeat("项", 50)})
		want := strings.Repeat("项", 40) + "…任务完成了"
		if len(*notifs) != 1 || (*notifs)[0].title != want || (*notifs)[0].msg != "done" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("bot question is not completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:            "请选择",
			trayTaskName:    "打开报表",
			AskUserQuestion: "用哪个账号？",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "用哪个账号？" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		if *flashes != 1 {
			t.Fatalf("expected flash, got %d", *flashes)
		}
	})

	t.Run("desktop handoff is not completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:               "请在浏览器里登录",
			trayTaskName:       "打开报表",
			DesktopUserControl: true,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || (*notifs)[0].msg != "请在浏览器里登录" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("recovery review is not completion", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			Text:                  "请确认恢复",
			trayTaskName:          "项目是什么",
			CodingRuntimeRecovery: &CodingRuntimeRecoveryReview{},
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || (*notifs)[0].msg != "请确认恢复" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
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

func TestTrayTaskLabelFromCommandUsesFirstLine(t *testing.T) {
	got := trayTaskCompletedMessage(trayTaskLabelFromCommand("\n  打开报表\n第二行不用"))
	if got != "打开报表任务完成了" {
		t.Fatalf("message = %q", got)
	}
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
