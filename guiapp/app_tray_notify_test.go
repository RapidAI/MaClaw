package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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

	t.Run("completion shows markdown as plain text", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "## 完成\n\n**已更新** `main.go`，见 [说明](https://example.com)。\n\n- 第一项\n- [x] 第二项\n\n| 文件 | 状态 |\n| --- | --- |\n| a.go | 完成 |"})
		want := "完成 已更新 main.go，见 说明。 第一项 第二项 文件 状态 a.go 完成"
		if len(*notifs) != 1 || (*notifs)[0].title != "任务完成" || (*notifs)[0].msg != want {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("completion keeps identifiers and drops fenced code", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Text: "调用 __init__。\n\n```go\nfmt.Println(\"hi\")\n```"})
		if len(*notifs) != 1 || (*notifs)[0].msg != "调用 __init__。" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("ask and confirm strip markup", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{AskUserQuestion: "用**哪个**目录？"})
		if len(*notifs) != 1 || (*notifs)[0].msg != "用哪个目录？" {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{Confirmation: &IMResponseConfirmation{Summary: "确认**删除** `a.go`？"}})
		if len(*notifs) != 1 || (*notifs)[0].msg != "确认删除 a.go？" {
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

	t.Run("desktop bot prose is not a balloon", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:    "desktop-bot-1",
			Text:         "无变化",
			trayTaskName: "打开首页",
		})
		if len(*notifs) != 0 || *flashes != 0 {
			t.Fatalf("prose became a balloon: %+v flashes=%d", *notifs, *flashes)
		}
	})

	t.Run("desktop bot balloon is the sentence", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:    "desktop-bot-1",
			Text:         "无变化",
			SendToUser:   "首页没打开。",
			trayTaskName: "打开首页",
		})
		if len(*notifs) != 1 || (*notifs)[0].msg != "首页没打开。" || strings.Contains((*notifs)[0].title, "无变化") || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot filler error stays quiet", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID: "desktop-bot-1",
			Text:      "无变化",
			Error:     "run ended without a result",
		})
		if len(*notifs) != 0 || *flashes != 0 {
			t.Fatalf("filler became a balloon: %+v flashes=%d", *notifs, *flashes)
		}
	})

	t.Run("desktop bot real error still notifies", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID: "desktop-bot-1",
			Text:      "无变化",
			Error:     "browser broke",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].msg != "browser broke" || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot ask does not quote prose", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:       "desktop-bot-1",
			Text:            "无变化",
			AskUserQuestion: "用哪个账号？",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "用哪个账号？" || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot failure that still asks is a reply", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:         "desktop-bot-1",
			Text:              "安装过程写在这里",
			Error:             "browser broke",
			AskUserQuestion:   "请输入密码。",
			AskUserSecretName: "SITE_PASSWORD",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "请输入密码。" || strings.Contains((*notifs)[0].msg, "安装过程") || strings.Contains((*notifs)[0].msg, "browser broke") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder question does not hide the error", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:       "desktop-bot-1",
			Text:            "安装过程写在这里",
			Error:           "browser broke",
			AskUserQuestion: "无变化",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].msg != "browser broke" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "安装过程") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder question still asks for the secret", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:         "desktop-bot-1",
			Text:              "安装过程写在这里",
			Error:             "browser broke",
			AskUserQuestion:   "无变化",
			AskUserSecretName: "SITE_PASSWORD",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "需要提供 SITE_PASSWORD" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "browser broke") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder question with choices still waits", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			Error:              "browser broke",
			AskUserQuestion:    "无变化",
			AskUserOptionsJSON: `["继续","停下"]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "任务等待回复，请返回窗口查看" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "browser broke") || strings.Contains((*notifs)[0].msg, "安装过程") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder question with choices and no error still waits", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "无变化",
			AskUserQuestion:    "无变化",
			AskUserOptionsJSON: `["继续","停下"]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "任务等待回复，请返回窗口查看" || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot choices without a question still wait", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			AskUserOptionsJSON: `["继续","停下"]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "任务等待回复，请返回窗口查看" || strings.Contains((*notifs)[0].msg, "安装过程") || strings.Contains((*notifs)[0].msg, "继续") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder labels without a question stay an error", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			Error:              "browser broke",
			AskUserOptionsJSON: `["无变化"]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].msg != "browser broke" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "安装过程") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder choice labels do not hide the error", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			Error:              "browser broke",
			AskUserQuestion:    "无变化",
			AskUserOptionsJSON: `["无变化", " "]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].msg != "browser broke" || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder choice labels leave a handoff as confirm", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:              "desktop-bot-1",
			Text:                   "安装过程写在这里",
			Error:                  "browser broke",
			AskUserQuestion:        "无变化",
			AskUserOptionsJSON:     `["无变化"]`,
			DesktopUserControl:     true,
			DesktopAttentionReason: "login_wall",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "browser broke") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder choice labels still ask for the secret", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			Error:              "browser broke",
			AskUserQuestion:    "无变化",
			AskUserSecretName:  "SITE_PASSWORD",
			AskUserOptionsJSON: `["无变化"]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "等待回复" || (*notifs)[0].msg != "需要提供 SITE_PASSWORD" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "browser broke") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot empty choices do not hide the error", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:          "desktop-bot-1",
			Text:               "安装过程写在这里",
			Error:              "browser broke",
			AskUserQuestion:    "无变化",
			AskUserOptionsJSON: `["", " "]`,
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" || (*notifs)[0].msg != "browser broke" || strings.Contains((*notifs)[0].msg, "无变化") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot placeholder question leaves a handoff as confirm", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:              "desktop-bot-1",
			Text:                   "安装过程写在这里",
			Error:                  "browser broke",
			AskUserQuestion:        "无变化",
			DesktopUserControl:     true,
			DesktopAttentionReason: "login_wall",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || strings.Contains((*notifs)[0].msg, "无变化") || strings.Contains((*notifs)[0].msg, "browser broke") || strings.Contains((*notifs)[0].msg, "安装过程") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})

	t.Run("desktop bot failure that hands off the desktop asks them back", func(t *testing.T) {
		reset()
		a.notifyTaskResponseTray(&IMAgentResponse{
			RequestID:              "desktop-bot-1",
			Text:                   "安装过程写在这里",
			Error:                  "browser broke",
			DesktopUserControl:     true,
			DesktopAttentionReason: "login_wall",
		})
		if len(*notifs) != 1 || (*notifs)[0].title != "需要确认" || (*notifs)[0].msg != "任务等待确认，请返回窗口处理" || strings.Contains((*notifs)[0].msg, "安装过程") {
			t.Fatalf("unexpected notifications: %+v", *notifs)
		}
	})
}

func TestTrayBalloonPlain(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "prose around a fence",
			in:   "已改好。\n\n```go\nfmt.Println(\"hi\")\n```\n请看日志。",
			want: "已改好。 请看日志。",
		},
		{
			name: "code only keeps the first line",
			in:   "```go\nfmt.Println(\"hi\")\nfmt.Println(\"skip\")\n```",
			want: "fmt.Println(\"hi\")",
		},
		{
			name: "identifiers stay",
			in:   "调用 __init__，然后看 arr[x]",
			want: "调用 __init__，然后看 arr[x]",
		},
		{
			name: "checkbox is only a list marker",
			in:   "- [x] 完成\n下一行 arr[x] = 1",
			want: "完成 下一行 arr[x] = 1",
		},
		{
			name: "one pipe stays",
			in:   "保留 a | b 这个写法",
			want: "保留 a | b 这个写法",
		},
		{
			name: "phone and negative stay",
			in:   "-1 是起点\n+86 13800000000",
			want: "-1 是起点 +86 13800000000",
		},
		{
			name: "quote heading and break",
			in:   "  ## 标题\n> 引用<br>下一行",
			want: "标题 引用 下一行",
		},
		{
			name: "link image and autolink",
			in:   "见图 ![界面](https://example.com/a.png)，打开 <https://example.com/a>",
			want: "见图 界面，打开 https://example.com/a",
		},
		{
			name: "delivery warning stays after the prose",
			in:   "**完成**了整理。\n\n[投递警告] push fail",
			want: "完成了整理。\n\n[投递警告] push fail",
		},
		{
			name: "markdown fence is the answer",
			in:   "```markdown\n## 完成\n已更新三个文件。\n```",
			want: "完成 已更新三个文件。",
		},
		{
			name: "markdown fence keeps prose and drops inner code",
			in:   "```md\n已完成。\n\n```go\nfmt.Println(\"hi\")\n```\n请看日志。\n```",
			want: "已完成。 请看日志。",
		},
		{
			name: "tilde rule does not hide the next line",
			in:   "上面\n~~~\n下面",
			want: "上面 下面",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trayBalloonPlain(tc.in); got != tc.want {
				t.Fatalf("got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestTrayNotifyBodyKeepsWarningInsideBalloon(t *testing.T) {
	got := trayNotifyBody("日报", strings.Repeat("字", 300)+"\n\n[投递警告] push fail")
	if !strings.HasPrefix(got, "日报: ") || !strings.Contains(got, "\n\n[投递警告] push fail") {
		t.Fatalf("body = %q", got)
	}
	if n := utf16Units(got); n > trayInfoUnits {
		t.Fatalf("units = %d, balloon holds %d", n, trayInfoUnits)
	}
}

func TestTrayNotifyBodyCountsUTF16(t *testing.T) {
	got := trayNotifyBody("日报", strings.Repeat("😀", 200)+"\n\n[投递警告] push fail")
	if !strings.HasPrefix(got, "日报: ") || !strings.Contains(got, "\n\n[投递警告] push fail") {
		t.Fatalf("body = %q", got)
	}
	if n := utf16Units(got); n > trayInfoUnits {
		t.Fatalf("units = %d, balloon holds %d", n, trayInfoUnits)
	}
}

func TestTrayTitleFitsUTF16(t *testing.T) {
	got := trayTaskCompletedMessage(strings.Repeat("😀", 80))
	if n := utf16Units(got); n > trayTitleUnits {
		t.Fatalf("units = %d, title holds %d (%q)", n, trayTitleUnits, got)
	}
	if !strings.HasSuffix(got, "任务完成了") {
		t.Fatalf("suffix lost: %q", got)
	}
}

func TestShortenTrayTaskNameKeepsLiteralText(t *testing.T) {
	if got := trayTaskCompletedMessage("1. 报表"); got != "1. 报表任务完成了" {
		t.Fatalf("numbered = %q", got)
	}
	if got := trayTaskCompletedMessage("a | b"); got != "a | b任务完成了" {
		t.Fatalf("pipe = %q", got)
	}
	if got := trayTaskCompletedMessage("**周报**"); got != "周报任务完成了" {
		t.Fatalf("bold = %q", got)
	}
	if got := trayTaskCompletedMessage("## 周报"); got != "周报任务完成了" {
		t.Fatalf("heading = %q", got)
	}
}

func TestTrayTaskLabelFromCommandUsesFirstLine(t *testing.T) {
	got := trayTaskCompletedMessage(trayTaskLabelFromCommand("\n  打开报表\n第二行不用"))
	if got != "打开报表任务完成了" {
		t.Fatalf("message = %q", got)
	}
}

func TestNotifyTaskResponseTrayCompletionWhileWindowVisible(t *testing.T) {
	notifs, flashes, restore := stubTrayNotifyEnv(t, true)
	defer restore()
	// ctx is nil, so the minimised fallback cannot fire either.
	a := &App{}
	a.notifyTaskResponseTray(&IMAgentResponse{Text: "done"})
	if len(*notifs) != 1 || (*notifs)[0].title != "任务完成" || (*notifs)[0].msg != "done" {
		t.Fatalf("expected a completion balloon while the window is visible, got %+v", *notifs)
	}
	*notifs = nil

	a.notifyTaskResponseTray(&IMAgentResponse{
		Text:       "done",
		SessionKey: desktopUserID + `:\Users\me\.maclaw\data\tasks\项目是什么-1791813952406498200`,
	})
	if len(*notifs) != 1 || (*notifs)[0].title != "项目是什么任务完成了" || (*notifs)[0].msg != "done" {
		t.Fatalf("expected a named-task balloon while the window is visible, got %+v", *notifs)
	}
	if *flashes != 0 {
		t.Fatalf("completion should not flash, got %d", *flashes)
	}

	*notifs = nil
	a.notifyTaskResponseTray(&IMAgentResponse{Error: "boom"})
	if len(*notifs) != 0 {
		t.Fatalf("errors stay quiet while the window is visible, got %+v", *notifs)
	}
}

func TestNotifyTaskResponseTrayCompletionSwitchOff(t *testing.T) {
	notifs, _, restore := stubTrayNotifyEnv(t, false)
	defer restore()
	off := false
	a := &App{}
	a.configSnap.Store(&corelib.AppConfig{TrayTaskCompletionNotify: &off})

	a.notifyTaskResponseTray(&IMAgentResponse{Text: "done"})
	if len(*notifs) != 0 {
		t.Fatalf("expected no completion balloon when the switch is off, got %+v", *notifs)
	}

	a.notifyTaskResponseTray(&IMAgentResponse{Error: "boom"})
	if len(*notifs) != 1 || (*notifs)[0].title != "任务异常" {
		t.Fatalf("errors still notify while away, got %+v", *notifs)
	}
}
