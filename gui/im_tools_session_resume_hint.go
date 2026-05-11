package main

import (
	"fmt"
	"strings"
)

// writeAutoResumeHint appends auto-resume instructions to the output buffer
// when a structured session exits with a ResumeContext. The reason parameter
// provides a context-specific description of why the session exited.
func writeAutoResumeHint(b *strings.Builder, rc *SessionResumeContext, reason string) {
	if rc == nil {
		b.WriteString("\n✅ 编程工具已正常退出。如需继续，请创建新会话。")
		return
	}
	if rc.ResumeCount >= 10 {
		b.WriteString("\n✅ 编程工具已退出。已自动续接 10 次，建议告知用户当前进度并询问是否继续。")
		return
	}
	b.WriteString(fmt.Sprintf("\n🔄 %s", reason))
	b.WriteString(fmt.Sprintf("\n📌 自动续接指令（第 %d 次续接）：", rc.ResumeCount+1))
	resumeSessionID := strings.TrimSpace(rc.ResumeSessionID)
	if resumeSessionID == "" {
		resumeSessionID = strings.TrimSpace(rc.ClaudeSessionID)
	}
	if resumeSessionID != "" {
		b.WriteString("\n1. 立即调用 create_session 创建新会话（使用相同的 tool 和 project_path，并传入 resume_session_id）")
		b.WriteString(fmt.Sprintf("\n   例如：create_session(tool=%q, project_path=%q, resume_session_id=%q)", rc.Tool, rc.ProjectPath, resumeSessionID))
	} else {
		b.WriteString("\n1. 立即调用 create_session 创建新会话（使用相同的 tool 和 project_path）")
	}
	b.WriteString("\n2. 用 send_and_observe 发送续接指令：")
	b.WriteString("\n   「请检查项目当前状态，继续完成之前未完成的任务。")
	if rc.OriginalTask != "" {
		b.WriteString(fmt.Sprintf("原始任务：%s。", rc.OriginalTask))
	}
	if rc.LastProgress != "" {
		b.WriteString(fmt.Sprintf("上次进度：%s。", rc.LastProgress))
	}
	if len(rc.CompletedFiles) > 0 {
		b.WriteString(fmt.Sprintf("已完成的文件：%s。", strings.Join(rc.CompletedFiles, ", ")))
	}
	b.WriteString("查看已有文件，补全缺失的部分，确保项目可以正常运行。」")
	b.WriteString("\n⚠️ 不要询问用户是否继续——直接创建新会话续接。不要自己用 write_file 写代码。")
}
