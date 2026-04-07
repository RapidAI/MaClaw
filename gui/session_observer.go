package main

import (
	"fmt"
	"strings"
	"time"
)

// SessionObserveOptions controls the send-and-observe polling behavior.
type SessionObserveOptions struct {
	TimeoutSeconds float64
	Lines          int
}

// SendAndObserveSession sends input to a remote session, waits for meaningful
// output or terminal/waiting state, then returns a formatted session snapshot.
// It is intentionally a thin shared helper so IM tools and other callers can
// reuse the same polling semantics without duplicating logic.
func SendAndObserveSession(manager *RemoteSessionManager, sessionID, text string, opts SessionObserveOptions, renderOutput func(map[string]interface{}) string) string {
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if manager == nil {
		return "会话管理器未初始化"
	}

	session, ok := manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}

	session.mu.RLock()
	baseLineCount := len(session.RawOutputLines)
	baseImageCount := len(session.OutputImages)
	session.mu.RUnlock()

	if err := manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}

	waitMs := []int{500, 500, 1000, 1000, 1500, 1500, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000, 3000}
	if ts := opts.TimeoutSeconds; ts > 0 {
		if ts > 120.0 {
			ts = 120.0
		}
		targetMs := int(ts * 1000)
		base := []int{500, 500, 1000, 1000, 1500, 1500, 2000}
		sum := 0
		for _, v := range base {
			sum += v
		}
		custom := make([]int, len(base))
		copy(custom, base)
		for sum < targetMs {
			custom = append(custom, 3000)
			sum += 3000
		}
		waitMs = custom
	}
	for _, ms := range waitMs {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		session.mu.RLock()
		newLines := len(session.RawOutputLines) - baseLineCount
		waiting := session.Summary.WaitingForUser
		status := session.Status
		session.mu.RUnlock()
		if newLines > 1 || waiting || status == SessionExited || status == SessionError {
			break
		}
	}

	session.mu.RLock()
	newImageCount := len(session.OutputImages) - baseImageCount
	session.mu.RUnlock()

	lines := opts.Lines
	if lines <= 0 {
		lines = 40
	}
	output := ""
	if renderOutput != nil {
		output = renderOutput(map[string]interface{}{
			"session_id": sessionID,
			"lines":      float64(lines),
		})
	} else {
		output = fmt.Sprintf("会话 %s 已发送输入。", sessionID)
	}
	if newImageCount > 0 {
		output += fmt.Sprintf("\n\n📷 会话产生了 %d 张图片，已自动通过 IM 发送给用户。", newImageCount)
	}
	return strings.TrimRight(output, "\n")
}
