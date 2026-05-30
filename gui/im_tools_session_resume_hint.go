package main

import (
	"fmt"
	"strings"
)

// writeAutoResumeHint appends guidance when a legacy structured session exits
// with a ResumeContext. New agent coding work must continue through the
// internal CodingSubAgent, not by creating another external session.
func writeAutoResumeHint(b *strings.Builder, rc *SessionResumeContext, reason string) {
	if rc == nil {
		b.WriteString("\n" + avTr(
			"External coding session exited. Continue any remaining coding work through internal CodingSubAgent; do not create another external coding session.",
			"外部编程会话已退出。剩余编程工作请继续走内部 CodingSubAgent；不要再创建新的外部编程会话。",
		))
		return
	}
	if rc.ResumeCount >= 10 {
		b.WriteString("\n" + avTr(
			"External coding session exited after repeated resume attempts. Summarize current progress and continue remaining coding work through internal CodingSubAgent.",
			"外部编程会话多次续接后已退出。请总结当前进度，并继续通过内部 CodingSubAgent 完成剩余编程工作。",
		))
		return
	}

	b.WriteString(fmt.Sprintf("\n%s", strings.TrimSpace(reason)))
	b.WriteString(fmt.Sprintf("\n"+avTr(
		"Legacy resume context available (attempt %d). Do not use external session tools for new agent work; route unfinished coding tasks through internal CodingSubAgent.",
		"已有旧外部会话续接上下文（第 %d 次）。新的 agent 编程任务不要再使用外部会话工具；未完成的编程任务请走内部 CodingSubAgent。",
	), rc.ResumeCount+1))
	if rc.OriginalTask != "" {
		b.WriteString(fmt.Sprintf("\n"+avTr("Original task: %s", "原始任务：%s"), rc.OriginalTask))
	}
	if rc.LastProgress != "" {
		b.WriteString(fmt.Sprintf("\n"+avTr("Last progress: %s", "最近进度：%s"), rc.LastProgress))
	}
	if len(rc.CompletedFiles) > 0 {
		b.WriteString(fmt.Sprintf("\n"+avTr("Completed files: %s", "已完成文件：%s"), strings.Join(rc.CompletedFiles, ", ")))
	}
	resumeSessionID := strings.TrimSpace(rc.ResumeSessionID)
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(rc.ClaudeSessionID)
	}
	if resumeSessionID != "" {
		b.WriteString(fmt.Sprintf("\n"+avTr("Legacy resume_session_id for audit only: %s", "旧 resume_session_id 仅用于审计：%s"), resumeSessionID))
	}
}
