package main

import (
	"fmt"
	"strings"
)

type sessionOutputSnapshot struct {
	Status   SessionStatus
	Summary  SessionSummary
	RawLines []string
}

func snapshotSessionOutput(session *RemoteSession) sessionOutputSnapshot {
	if session == nil {
		return sessionOutputSnapshot{}
	}
	session.mu.RLock()
	defer session.mu.RUnlock()

	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	return sessionOutputSnapshot{
		Status:   session.Status,
		Summary:  session.Summary,
		RawLines: rawLines,
	}
}

func renderSessionOutput(sessionID string, maxLines int, snapshot sessionOutputSnapshot, hintFacts sessionOutputHintFacts) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("会话 %s 状态: %s\n", sessionID, snapshot.Status))
	appendSessionSummaryOutput(&b, snapshot.Summary)
	appendSessionRawOutput(&b, sessionID, maxLines, snapshot.RawLines, hintFacts)
	appendBusySessionHint(&b, hintFacts)
	appendWaitingInputSessionHint(&b, sessionID, hintFacts)
	appendTerminalSessionExitHint(&b, hintFacts)
	return b.String()
}

func appendSessionSummaryOutput(b *strings.Builder, summary SessionSummary) {
	if summary.CurrentTask != "" {
		b.WriteString(fmt.Sprintf("当前任务: %s\n", summary.CurrentTask))
	}
	if summary.ProgressSummary != "" {
		b.WriteString(fmt.Sprintf("进度: %s\n", summary.ProgressSummary))
	}
	if summary.LastResult != "" {
		b.WriteString(fmt.Sprintf("最近结果: %s\n", summary.LastResult))
	}
	if summary.LastCommand != "" {
		b.WriteString(fmt.Sprintf("最近命令: %s\n", summary.LastCommand))
	}
	if summary.WaitingForUser {
		b.WriteString("⚠️ 会话正在等待用户输入\n")
	}
	if summary.SuggestedAction != "" {
		b.WriteString(fmt.Sprintf("建议操作: %s\n", summary.SuggestedAction))
	}
}

func appendSessionRawOutput(b *strings.Builder, sessionID string, maxLines int, rawLines []string, hintFacts sessionOutputHintFacts) {
	if len(rawLines) == 0 {
		b.WriteString("\n(暂无输出)\n")
		appendNoOutputSessionHint(b, sessionID, hintFacts)
		return
	}

	start := 0
	if maxLines > 0 && len(rawLines) > maxLines {
		start = len(rawLines) - maxLines
	}
	b.WriteString(fmt.Sprintf("\n--- 最近 %d 行输出 ---\n", len(rawLines)-start))
	for _, line := range rawLines[start:] {
		b.WriteString(line)
		b.WriteString("\n")
	}
}
