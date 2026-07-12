package main

import (
	"fmt"
	"strings"
)

func appendNoOutputSessionHint(b *strings.Builder, sessionID string, facts sessionOutputHintFacts) {
	if facts.Status.IsRunning() {
		b.WriteString(fmt.Sprintf("\n旧外部编程会话 %q 已就绪但暂无输出。agent 新编程任务已改走内部 CodingSubAgent，不再续接外部会话。", sessionID))
	} else if facts.Status.IsStarting() {
		b.WriteString("\n会话正在启动中，请稍后再次调用 get_session_output 检查状态（最多再检查 1 次）。")
	}
}

func appendBusySessionHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if !facts.Status.IsBusy() {
		return
	}
	if facts.HasAPIRetry {
		b.WriteString("\n上游 API 限流/暂时不可用，编程工具正在自动重试中。这是正常现象，请耐心等待（可能需要 30-60 秒）。")
		b.WriteString("\n不要放弃或创建新会话——API 重试通常会成功。稍后再调用 get_session_output 检查进度。")
		return
	}
	switch facts.StallState {
	case StallStateSuspected:
		b.WriteString("\n编程工具输出暂停，系统正在尝试恢复，请稍后再检查")
	case StallStateStuck:
		b.WriteString("\n编程工具可能已卡住，建议发送具体指令或终止会话")
	default:
		b.WriteString("\n编程工具正在工作中，请等待后再检查进度")
	}
}

func appendWaitingInputSessionHint(b *strings.Builder, sessionID string, facts sessionOutputHintFacts) {
	if !facts.Status.IsWaitingInput() {
		return
	}
	if facts.HasRecentTransientAPI && facts.CompletionLevel != CompletionCompleted {
		b.WriteString("\n编程工具遇到 API 错误后恢复，任务可能未完成。")
		b.WriteString(fmt.Sprintf("\n旧外部会话 %q 不再由 agent 自动续接；新编程任务请走内部 CodingSubAgent。", sessionID))
		return
	}
	switch facts.CompletionLevel {
	case CompletionCompleted:
		b.WriteString("\n任务似乎已完成，可以查看结果")
	case CompletionIncomplete:
		if facts.StructuredSession {
			if facts.AutoContinueCount >= 10 {
				b.WriteString("\n已自动续接 10 次，建议告知用户当前进度并询问是否继续。")
			} else {
				b.WriteString("\n编程工具因 token/turn 限制暂停，任务未完成。")
				b.WriteString(fmt.Sprintf("\n旧外部会话 %q 不再由 agent 自动续接；新编程任务请走内部 CodingSubAgent。", sessionID))
			}
		} else {
			b.WriteString("\n任务似乎未完成。agent 新编程任务请走内部 CodingSubAgent。")
		}
	default:
		if facts.StructuredSession {
			b.WriteString(fmt.Sprintf("\n旧外部编程会话 %q 已暂停。agent 新编程任务请走内部 CodingSubAgent。", sessionID))
		}
	}
}

func appendTerminalSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.HasNonZeroTerminalExit() {
		if facts.StructuredSession && *facts.ExitCode == 1 {
			writeAutoResumeHint(b, facts.ResumeContext, "编程工具因 token 耗尽正常退出，但任务可能未完成。")
		} else if facts.StructuredSession && *facts.ExitCode > 1 {
			appendStructuredSessionExitHint(b, facts)
		} else {
			appendPTYSessionExitHint(b, facts)
		}
	}
	if facts.HasStructuredNormalExitWithPossibleUnfinishedWork() {
		writeAutoResumeHint(b, facts.ResumeContext, "编程工具已正常退出（可能达到 max-turns 限制），任务可能未完成。")
	}
}

func appendStructuredSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.ExitCode == nil {
		return
	}
	resumeCount := facts.ResumeCount()
	if facts.FatalSessionError {
		b.WriteString(fmt.Sprintf("\n编程工具遇到不可恢复的错误退出（退出码 %d）。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的 API Key 配置和安装。", facts.Tool))
		b.WriteString("\n不要重试——这是配置问题，重试不会解决。")
	} else if resumeCount < 3 {
		b.WriteString(fmt.Sprintf("\n编程工具异常退出（退出码 %d），可能是 API 限流、网络波动或临时服务故障。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n不再自动创建外部会话重试（原计划第 %d/3 次）。新编程任务请走内部 CodingSubAgent。", resumeCount+1))
	} else {
		b.WriteString(fmt.Sprintf("\n编程工具已连续失败 %d 次（退出码 %d）。", resumeCount, *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请将错误信息告知用户，建议检查 %s 的上游 API 状态或稍后再试。", facts.Tool))
	}
}

func appendPTYSessionExitHint(b *strings.Builder, facts sessionOutputHintFacts) {
	if facts.ExitCode == nil {
		return
	}
	if facts.FatalSessionError {
		b.WriteString(fmt.Sprintf("\n会话遇到不可恢复的错误退出（退出码 %d）。", *facts.ExitCode))
		b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", facts.Tool))
	} else {
		b.WriteString(fmt.Sprintf("\n会话已失败退出（退出码 %d），可能是临时错误。", *facts.ExitCode))
		b.WriteString("\n不再建议创建外部会话重试；新编程任务请走内部 CodingSubAgent。")
	}
}
