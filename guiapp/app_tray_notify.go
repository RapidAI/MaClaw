package guiapp

import (
	"log"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// notifyTaskResponseTray pops a system tray balloon notification when an
// interactive agent task reaches a terminal state: failed, awaiting user
// confirmation, waiting for an ask_user reply, or completed. A named task says
// "xx任务完成了" as the balloon title. The name is an explicit label or the
// tasks/<slug>-<id> directory, including when the session is a subdirectory.
// The reply stays in the body. A turn that is still asking, recording, or
// waiting for the person is not called complete.
// It only fires while the main window is hidden or minimised, so users
// actively working in the window are not disturbed. User-initiated
// cancellations are skipped.
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
	case trayResponseWaitsForReply(resp):
		if FlashAndBeep != nil {
			FlashAndBeep()
		}
		msg := truncateRunes(strings.TrimSpace(resp.AskUserQuestion), 200)
		if msg == "" {
			msg = body
		}
		if msg == "" {
			if secret := strings.TrimSpace(resp.AskUserSecretName); secret != "" {
				msg = truncateRunes("需要提供 "+secret, 200)
			}
		}
		if msg == "" {
			msg = "任务等待回复，请返回窗口查看"
		}
		ShowNotification("等待回复", msg, 2)
	case trayResponseNeedsConfirm(resp):
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
	default:
		msg := body
		title := "任务完成"
		if name := trayTaskCompletionName(resp); name != "" {
			title = trayTaskCompletedMessage(name)
			if msg == "" {
				// Windows hides a balloon whose body is empty.
				msg = title
			}
		} else if msg == "" {
			msg = "任务完成了"
		}
		ShowNotification(title, msg, 1)
	}
}

// trayResponseWaitsForReply is a turn that stopped to ask this person something.
// The reply text is not a finished task, even when ResponseSource was not set.
func trayResponseWaitsForReply(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	if strings.TrimSpace(resp.AskUserQuestion) != "" || strings.TrimSpace(resp.AskUserSecretName) != "" {
		return true
	}
	switch canonicalIMResponseSourceKind(resp.ResponseSource) {
	case imResponseSourceAskUser, imResponseSourceRecordAudio:
		return true
	default:
		return false
	}
}

// trayResponseNeedsConfirm is a turn the person still has to accept or take over.
func trayResponseNeedsConfirm(resp *IMAgentResponse) bool {
	if resp == nil {
		return false
	}
	return resp.Confirmation != nil || resp.UnfinishedTask != nil || resp.UnfinishedSlot != nil ||
		resp.CodingRuntimeRecovery != nil || resp.DesktopUserControl ||
		strings.TrimSpace(resp.DesktopAttentionReason) != ""
}

// trayTaskCompletionName is the label in "xx任务完成了". An explicit tray label
// wins. A session path contributes a name only when it is a managed task directory.
func trayTaskCompletionName(resp *IMAgentResponse) string {
	if resp == nil {
		return ""
	}
	if name := strings.TrimSpace(resp.trayTaskName); name != "" {
		return name
	}
	return managedTaskTitleFromSessionPath(projectPathFromSessionOwnerID(strings.TrimSpace(resp.SessionKey)))
}

// managedTaskTitleFromSessionPath reads the tasks/<slug>-<id> segment.
// A workspace subdirectory stays on that task. A project folder, or a
// directory that only happens to sit under data/tasks, is not a task name.
func managedTaskTitleFromSessionPath(path string) string {
	n := filepath.ToSlash(normalizeProjectSessionPath(path))
	if n == "" {
		return ""
	}
	const marker = "/data/tasks/"
	i := strings.Index(strings.ToLower(n), marker)
	if i < 0 {
		return ""
	}
	rest := n[i+len(marker):]
	slug, _, _ := strings.Cut(rest, "/")
	if !looksLikeManagedTaskDirName(slug) {
		return ""
	}
	return stripTaskDirTimestampSuffix(slug)
}

func trayTaskLabelFromCommand(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func shortenTrayTaskName(name string) string {
	name = strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
	const maxRunes = 40
	runes := []rune(name)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "…"
	}
	return name
}

func trayTaskCompletedMessage(name string) string {
	name = shortenTrayTaskName(name)
	if name == "" {
		return "任务完成了"
	}
	if strings.HasSuffix(name, "任务") {
		return name + "完成了"
	}
	return name + "任务完成了"
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
